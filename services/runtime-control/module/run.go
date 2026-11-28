package module

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/runtime-control/catalog"
	"opl-cloud/services/runtime-control/migrations"
)

// Run executes the RuntimeControl owner module until ctx is canceled.
func Run(ctx context.Context, env ownerservice.ModuleEnv) error {
	config, err := ownerservice.LoadConfig(env.Env, ownerservice.OwnerRuntimeControl, ":8183")
	if err != nil {
		return err
	}
	config.TLS.InProc = env.InProc

	source, err := migrations.Source()
	if err != nil {
		return err
	}

	bootstrap, err := ownerservice.Start(ctx, config, source, func(server *ownerservice.Server, database *ownerservice.Database) error {
		if database == nil {
			return ownerservice.ErrHandlersNotImplemented
		}
		schemaPath, schemaDigest := env.Env("OPL_RUNTIME_PUBLISHER_SCHEMA_PATH"), env.Env("OPL_RUNTIME_PUBLISHER_SCHEMA_DIGEST")
		if schemaPath == "" || schemaDigest == "" {
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
		authorizer, idConn, err := ownerservice.AuthorizerFromConfig(config)
		if err != nil {
			return err
		}
		if idConn != nil {
			if err := server.TrackCloser(idConn); err != nil {
				idConn.Close()
				return err
			}
		}
		service, err := catalog.New(database.DB(), authorizer, api.NewCapabilityProductServiceClient(capabilityConn), schemaPath, schemaDigest)
		if err != nil {
			return err
		}
		return service.Register(server)
	})
	if err != nil {
		return err
	}
	defer bootstrap.Close()
	return ownerservice.ServeContext(ctx, bootstrap.Server)
}

func dialPeer(config ownerservice.Config, getenv func(string) string, tls owneridentity.TLSConfig, target owneridentity.Owner, address string) (*grpc.ClientConn, error) {
	if address == "" {
		return nil, fmt.Errorf("%s address is required", target)
	}
	options, err := tls.DialOptions(config.Owner.Service(), owneridentity.DeploymentUnitTransportPrincipal(target), getenv("OPL_"+strings.ToUpper(target.String())+"_TOKEN"))
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(address, options...)
}
