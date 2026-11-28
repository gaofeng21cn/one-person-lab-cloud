package module

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	buildservice "opl-cloud/services/build/internal/build"
	"opl-cloud/services/build/migrations"
	"opl-cloud/services/internal/ownerservice"
)

// Run executes the Build owner module until ctx is canceled.
func Run(ctx context.Context, env ownerservice.ModuleEnv) error {
	config, err := ownerservice.LoadConfig(env.Env, ownerservice.OwnerBuild, ":8182")
	if err != nil {
		return err
	}
	config.TLS.InProc = env.InProc

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	source, err := migrations.Source()
	if err != nil {
		return err
	}

	bootstrap, err := ownerservice.Start(runCtx, config, source, func(server *ownerservice.Server, database *ownerservice.Database) error {
		if database == nil {
			return ownerservice.ErrHandlersNotImplemented
		}
		tls := config.TLS
		capabilityConn, err := dialPeer(config, env.Env, tls, owneridentity.Capability, env.Env("OPL_CAPABILITY_ADDR"))
		if err != nil {
			return err
		}
		if err := server.TrackCloser(capabilityConn); err != nil {
			capabilityConn.Close()
			return err
		}
		ledgerConn, err := dialPeer(config, env.Env, tls, owneridentity.Ledger, env.Env("OPL_LEDGER_ADDR"))
		if err != nil {
			return err
		}
		if err := server.TrackCloser(ledgerConn); err != nil {
			ledgerConn.Close()
			return err
		}
		identityConn, err := dialPeer(config, env.Env, tls, owneridentity.Tenant, config.CloudIdentityAddr)
		if err != nil {
			return err
		}
		if err := server.TrackCloser(identityConn); err != nil {
			identityConn.Close()
			return err
		}
		runner := &buildservice.Runner{
			Builder:           env.Env("OPL_BUILD_BUILDER"),
			RegistryPrefix:    env.Env("OPL_BUILD_REGISTRY_PREFIX"),
			StorageURL:        env.Env("OPL_CAPABILITY_OBJECT_URL"),
			StorageToken:      env.Env("OPL_CAPABILITY_OBJECT_TOKEN"),
			RegistryToken:     env.Env("OPL_BUILD_REGISTRY_TOKEN"),
			DockerConfig:      env.Env("OPL_BUILD_DOCKER_CONFIG"),
			WorkDir:           env.Env("OPL_BUILD_WORKDIR"),
			MaxPackageBytes:   1 << 30,
			MaxExpandedBytes:  4 << 30,
			MaxFiles:          100000,
			Timeout:           30 * time.Minute,
			PathEnv:           env.Env("PATH"),
			AllowHTTP:         env.Env("NODE_ENV") != "production",
			InsecureHTTPHosts: buildservice.ParseInsecureHTTPHosts(env.Env("OPL_BUILD_INSECURE_HTTP_HOSTS")),
		}
		if err := runner.Validate(); err != nil {
			return err
		}
		service, err := buildservice.New(
			database.Store(),
			ownerservice.NewAuthorizer(config.Owner, api.NewCloudIdentityAuthorizationClient(identityConn)),
			api.NewCapabilityCoordinationClient(capabilityConn),
			api.NewCapabilityProductServiceClient(capabilityConn),
			map[string]api.DomainInboxClient{
				"capability": api.NewDomainInboxClient(capabilityConn),
				"ledger":     api.NewDomainInboxClient(ledgerConn),
			},
			api.NewCloudIdentityAuthorizationClient(identityConn),
			api.NewGatewayCoordinationClient(identityConn),
			runner,
		)
		if err != nil {
			return err
		}
		if err := service.Register(server); err != nil {
			return err
		}
		go service.Run(runCtx)
		return nil
	})
	if err != nil {
		return err
	}
	defer bootstrap.Close()
	return ownerservice.ServeContext(runCtx, bootstrap.Server)
}

func dialPeer(config ownerservice.Config, getenv func(string) string, tls owneridentity.TLSConfig, target owneridentity.Owner, address string) (*grpc.ClientConn, error) {
	if address == "" {
		return nil, fmt.Errorf("%s address is required", target)
	}
	token := getenv("OPL_" + strings.ToUpper(target.String()) + "_TOKEN")
	options, err := tls.DialOptions(config.Owner.Service(), target.Service(), token)
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(address, options...)
}
