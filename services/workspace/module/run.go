package module

import (
	"context"
	"fmt"
	"log"
	"strings"

	"google.golang.org/grpc"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/workspace/internal/launch"
	"opl-cloud/services/workspace/migrations"
)

// Run executes the Workspace owner module until ctx is canceled.
func Run(ctx context.Context, env ownerservice.ModuleEnv) error {
	config, err := ownerservice.LoadConfig(env.Env, ownerservice.OwnerWorkspace, ":8184")
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
		if err := server.RequireProductGroups("WorkspaceProductService"); err != nil {
			return err
		}
		if database == nil {
			return ownerservice.ErrHandlersNotImplemented
		}
		authorizer, identityConn, err := ownerservice.AuthorizerFromConfig(config)
		if err != nil {
			return err
		}
		if identityConn == nil {
			return fmt.Errorf("CloudIdentity address is required for Workspace launch")
		}
		if err = server.TrackCloser(identityConn); err != nil {
			identityConn.Close()
			return err
		}
		catalogConn, err := dialPeer(config, env.Env, server, owneridentity.ResourceCatalog, env.Env("OPL_RESOURCE_CATALOG_ADDR"))
		if err != nil {
			return err
		}
		fabricConn, err := dialPeer(config, env.Env, server, owneridentity.Fabric, env.Env("OPL_FABRIC_ADDR"))
		if err != nil {
			return err
		}
		identityClient := api.NewCloudIdentityAuthorizationClient(identityConn)
		service, err := launch.New(database.DB(), authorizer, api.NewCatalogCoordinationClient(catalogConn), api.NewFabricCoordinationClient(fabricConn), identityClient)
		if err != nil {
			return err
		}
		service.PurchaseEligibility, err = launch.NewCloudIdentityPurchaseEligibilityReader(identityClient)
		if err != nil {
			return err
		}
		if address := strings.TrimSpace(env.Env("OPL_LEDGER_ADDR")); address != "" {
			ledgerConn, err := dialPeer(config, env.Env, server, owneridentity.Ledger, address)
			if err != nil {
				return err
			}
			service.Ledger = api.NewLedgerCoordinationClient(ledgerConn)
		}
		if address := strings.TrimSpace(env.Env("OPL_CAPABILITY_ADDR")); address != "" {
			conn, err := dialPeer(config, env.Env, server, owneridentity.Capability, address)
			if err != nil {
				return err
			}
			service.Capability = api.NewCapabilityProductServiceClient(conn)
		}
		if address := strings.TrimSpace(env.Env("OPL_RUNTIME_CONTROL_ADDR")); address != "" {
			conn, err := dialPeer(config, env.Env, server, owneridentity.RuntimeControl, address)
			if err != nil {
				return err
			}
			service.RuntimeReleases = api.NewRuntimeControlProductServiceClient(conn)
		}
		if address := strings.TrimSpace(env.Env("OPL_SERVE_ADDR")); address != "" {
			conn, err := dialPeer(config, env.Env, server, owneridentity.Serve, address)
			if err != nil {
				return err
			}
			service.Serve = api.NewServeAgentCoordinationClient(conn)
			service.ServeProduct = api.NewServeProductServiceClient(conn)
			service.ServeCredentials = api.NewServeRuntimeAdapterClient(conn)
		}
		if address := strings.TrimSpace(env.Env("OPL_GATEWAY_ADDR")); address != "" {
			conn, err := dialPeer(config, env.Env, server, owneridentity.Gateway, address)
			if err != nil {
				return err
			}
			service.Gateway = api.NewGatewayCoordinationClient(conn)
		}
		if err = service.Register(server); err != nil {
			return err
		}
		go func() {
			if err := service.Run(runCtx); err != nil && runCtx.Err() == nil {
				log.Printf("Workspace recovery stopped: %v", err)
			}
		}()
		return nil
	})
	if err != nil {
		return err
	}
	defer bootstrap.Close()
	return ownerservice.ServeContext(runCtx, bootstrap.Server)
}

func dialPeer(config ownerservice.Config, getenv func(string) string, server *ownerservice.Server, target owneridentity.Owner, address string) (*grpc.ClientConn, error) {
	if strings.TrimSpace(address) == "" {
		return nil, fmt.Errorf("%s address is required", target)
	}
	options, err := config.TLS.DialOptions(config.Owner.Service(), owneridentity.DeploymentUnitTransportPrincipal(target), getenv("OPL_"+strings.ToUpper(target.String())+"_TOKEN"))
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		return nil, err
	}
	if err = server.TrackCloser(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
