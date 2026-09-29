package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
)

func main() {
	config, e := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerTenant, ":8187")
	if e != nil {
		log.Fatal(e)
	}
	// The Gateway directory identity is optional and all-or-nothing: it is the
	// service's own administrative read-only identity used to resolve a member's
	// display name and to confirm an invited subject exists. Without it those two
	// facts stay unresolved rather than fabricated.
	var directory *identity.GatewayDirectory
	if email, password := strings.TrimSpace(os.Getenv("OPL_GATEWAY_DIRECTORY_EMAIL")), os.Getenv("OPL_GATEWAY_DIRECTORY_PASSWORD"); email != "" || password != "" {
		directory = &identity.GatewayDirectory{Email: email, Password: password}
	}
	gateway, e := identity.NewGatewayWithDirectory(os.Getenv("OPL_SUB2API_URL"), directory)
	if e != nil {
		log.Fatal(e)
	}
	source, e := migrations.Source()
	if e != nil {
		log.Fatal(e)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	boot, e := ownerservice.Start(ctx, config, source, func(server *ownerservice.Server, db *ownerservice.Database) error {
		if db == nil {
			return ownerservice.ErrHandlersNotImplemented
		}
		admins := []string{}
		if v := strings.TrimSpace(os.Getenv("OPL_PLATFORM_ADMIN_SUBJECTS")); v != "" {
			admins = strings.Split(v, ",")
		}
		invitationTTL, e := time.ParseDuration(strings.TrimSpace(os.Getenv("OPL_INVITATION_TTL")))
		if e != nil {
			return fmt.Errorf("OPL_INVITATION_TTL must be an explicit Go duration such as 168h: %w", e)
		}
		s, e := identity.New(db.DB(), gateway, []byte(os.Getenv("OPL_SESSION_SIGNING_KEY")), admins, invitationTTL)
		if e != nil {
			return e
		}
		registryHost, registryNamespace := strings.TrimSpace(os.Getenv("OPL_WORKSPACE_REGISTRY_HOST")), strings.TrimSpace(os.Getenv("OPL_WORKSPACE_REGISTRY_NAMESPACE"))
		if (registryHost == "") != (registryNamespace == "") {
			return fmt.Errorf("OPL_WORKSPACE_REGISTRY_HOST and OPL_WORKSPACE_REGISTRY_NAMESPACE must be supplied together")
		}
		s.ConfigureRegistry(registryHost, registryNamespace)
		if err := s.BackfillTenantRepositoryBindings(ctx); err != nil {
			return err
		}
		// Owner-commit readback for grant issuance is wired only for an owner whose
		// address this deployment actually configures. An absent address is a
		// deployment that has not placed that owner yet, and grpc.NewClient accepts
		// an empty target without error, so dialing it would install a client whose
		// failure surfaces much later as an unrelated transport error. Build and
		// Workspace are treated identically: no address, no client, and the grant
		// path refuses. This owner is deployed before Build in the unit, so Build
		// absence is a normal configuration rather than a startup defect.
		for _, peer := range []struct {
			target  owneridentity.Owner
			address string
			token   string
			assign  func(api.OwnerCommitReadbackClient)
		}{
			{owneridentity.Build, "OPL_BUILD_ADDR", "OPL_BUILD_TOKEN", func(c api.OwnerCommitReadbackClient) { s.BuildCommit = c }},
			{owneridentity.Workspace, "OPL_WORKSPACE_ADDR", "OPL_WORKSPACE_TOKEN", func(c api.OwnerCommitReadbackClient) { s.WorkspaceCommit = c }},
		} {
			address := strings.TrimSpace(os.Getenv(peer.address))
			if address == "" {
				continue
			}
			options, e := config.TLS.DialOptions(owneridentity.Tenant.Service(), peer.target.Service(), os.Getenv(peer.token))
			if e != nil {
				return e
			}
			conn, e := grpc.NewClient(address, options...)
			if e != nil {
				return e
			}
			if e = server.TrackCloser(conn); e != nil {
				return e
			}
			peer.assign(api.NewOwnerCommitReadbackClient(conn))
		}
		return s.Register(server)
	})
	if e != nil {
		log.Fatal(e)
	}
	defer boot.Close()
	go func() { <-ctx.Done(); boot.Server.Stop() }()
	if e = boot.Server.Serve(); e != nil {
		log.Fatal(e)
	}
}
