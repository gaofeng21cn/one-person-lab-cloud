package module

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"google.golang.org/grpc"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/gatewaymigrations"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
)

// Run executes the Gateway Integration & CloudIdentity deployment unit until ctx is canceled.
func Run(ctx context.Context, env ownerservice.ModuleEnv) error {
	config, e := ownerservice.LoadConfig(env.Env, ownerservice.OwnerTenant, ":8187")
	if e != nil {
		return e
	}
	config.TLS.InProc = env.InProc

	gatewayDSN := strings.TrimSpace(env.Env("OPL_GATEWAY_DATABASE_URL"))
	if gatewayDSN == "" {
		return fmt.Errorf("OPL_GATEWAY_DATABASE_URL is required for durable User identity and OCI bindings")
	}
	var directory *identity.GatewayDirectory
	if email, password := strings.TrimSpace(env.Env("OPL_GATEWAY_DIRECTORY_EMAIL")), env.Env("OPL_GATEWAY_DIRECTORY_PASSWORD"); email != "" || password != "" {
		directory = &identity.GatewayDirectory{Email: email, Password: password}
	}
	gateway, e := identity.NewGatewayWithDirectory(env.Env("OPL_SUB2API_URL"), directory)
	if e != nil {
		return e
	}
	source, e := migrations.Source()
	if e != nil {
		return e
	}
	boot, e := ownerservice.Start(ctx, config, source, func(server *ownerservice.Server, db *ownerservice.Database) error {
		if db == nil {
			return ownerservice.ErrHandlersNotImplemented
		}
		admins := []string{}
		if v := strings.TrimSpace(env.Env("OPL_PLATFORM_ADMIN_SUBJECTS")); v != "" {
			admins = strings.Split(v, ",")
		}
		invitationTTL, e := time.ParseDuration(strings.TrimSpace(env.Env("OPL_INVITATION_TTL")))
		if e != nil {
			return fmt.Errorf("OPL_INVITATION_TTL must be an explicit Go duration such as 168h: %w", e)
		}
		s, e := identity.New(db.DB(), gateway, []byte(env.Env("OPL_SESSION_SIGNING_KEY")), admins, invitationTTL)
		if e != nil {
			return e
		}
		registryHost, registryNamespace := strings.TrimSpace(env.Env("OPL_WORKSPACE_REGISTRY_HOST")), strings.TrimSpace(env.Env("OPL_WORKSPACE_REGISTRY_NAMESPACE"))
		if (registryHost == "") != (registryNamespace == "") {
			return fmt.Errorf("OPL_WORKSPACE_REGISTRY_HOST and OPL_WORKSPACE_REGISTRY_NAMESPACE must be supplied together")
		}
		s.ConfigureRegistry(registryHost, registryNamespace)

		gatewayDB, e := ownerservice.OpenDatabase(ctx, owneridentity.Gateway, gatewayDSN)
		if e != nil {
			return e
		}
		gatewaySource, e := gatewaymigrations.Source()
		if e != nil {
			gatewayDB.Close()
			return e
		}
		if e = gatewayDB.Migrate(ctx, gatewaySource); e != nil {
			gatewayDB.Close()
			return e
		}
		if e = server.TrackCloser(gatewayDB); e != nil {
			gatewayDB.Close()
			return e
		}
		store, e := identity.NewGatewayStore(gatewayDB.DB())
		if e != nil {
			return e
		}
		if e = server.AddReadinessCheck("gateway_database", gatewayDB.Ready); e != nil {
			return e
		}
		s.GatewayStore = store
		// The historical OCI destinations live in the frozen Tenant table. This
		// one-time pass moves them into the Gateway owner's single destination
		// mapping, which is the only place a destination is written from now on.
		// An unavailable owner store is different from a reported row: the pass
		// cannot be trusted to have completed, so it fails the startup the same
		// way the owner refuses to serve with an incomplete handover.
		summary, handoverErr := s.HandoverRepositoryBindings(ctx)
		slog.Info("repository binding handover",
			"scanned", summary.Scanned, "moved", summary.Moved, "already", summary.Already,
			"released", summary.Released, "released_unheld", summary.ReleasedUnheld,
			"unattributable", summary.Unattributable,
			"conflicting", summary.Conflicting, "failed", summary.Failed,
			"moved_refs", summary.MovedRefs)
		if handoverErr != nil {
			slog.Error("repository binding handover failed", "error", handoverErr.Error())
			return handoverErr
		}
		// A row the owner could not attribute or serve is a destination nobody
		// holds. The runtime fallback read is retired, so the frozen table keeps
		// that destination as provenance only and a new actor could take it and
		// reach another Tenant's OCI artifacts. The owner therefore refuses to
		// serve while a residual row exists; the operator resolves the named
		// rows and the service starts on the next attempt.
		if residual := summary.ResidualError(); residual != nil {
			slog.Error("repository binding handover needs an operator",
				"unattributable", summary.UnattributableRefs,
				"conflicting", summary.ConflictingRefs,
				"failed", summary.FailedRefs,
				"released_unheld", summary.ReleasedUnheldRefs)
			return residual
		}

		group := strings.TrimSpace(env.Env("OPL_GATEWAY_WORKSPACE_KEY_GROUP"))
		if group == "" {
			group = "Codex"
		}
		issuer, err := identity.NewGatewayWorkspaceKeyIssuer(s.Gateway, group)
		if err != nil {
			return err
		}
		s.KeyIssuer = issuer
		s.WorkspaceKeyGroup = group
		if fabricAddr, fabricToken := strings.TrimSpace(env.Env("OPL_FABRIC_ADDR")), strings.TrimSpace(env.Env("OPL_FABRIC_TOKEN")); fabricAddr != "" && fabricToken != "" {
			options, err := config.TLS.DialOptions(owneridentity.Tenant.Service(), owneridentity.Fabric.Service(), fabricToken)
			if err != nil {
				return err
			}
			conn, err := grpc.NewClient(fabricAddr, options...)
			if err != nil {
				return err
			}
			if err = server.TrackCloser(conn); err != nil {
				conn.Close()
				return err
			}
			s.SecretStore = identity.NewFabricManagedSecretStore(api.NewFabricCoordinationClient(conn))
		}
		for _, peer := range []struct {
			target  owneridentity.Owner
			address string
			token   string
			assign  func(api.OwnerCommitReadbackClient)
		}{
			{owneridentity.Build, "OPL_BUILD_ADDR", "OPL_BUILD_TOKEN", func(c api.OwnerCommitReadbackClient) { s.BuildCommit = c }},
			{owneridentity.Workspace, "OPL_WORKSPACE_ADDR", "OPL_WORKSPACE_TOKEN", func(c api.OwnerCommitReadbackClient) { s.WorkspaceCommit = c }},
			{owneridentity.Serve, "OPL_SERVE_ADDR", "OPL_SERVE_TOKEN", func(c api.OwnerCommitReadbackClient) { s.ServeCommit = c }},
		} {
			address := strings.TrimSpace(env.Env(peer.address))
			if address == "" {
				continue
			}
			options, e := config.TLS.DialOptions(owneridentity.Tenant.Service(), peer.target.Service(), env.Env(peer.token))
			if e != nil {
				return e
			}
			conn, e := grpc.NewClient(address, options...)
			if e != nil {
				return e
			}
			if e = server.TrackCloser(conn); e != nil {
				conn.Close()
				return e
			}
			peer.assign(api.NewOwnerCommitReadbackClient(conn))
		}
		return s.Register(server)
	})
	if e != nil {
		return e
	}
	defer boot.Close()
	return ownerservice.ServeContext(ctx, boot.Server)
}
