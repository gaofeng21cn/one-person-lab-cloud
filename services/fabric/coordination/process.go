package coordination

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/ownermigrations"
	"opl-cloud/services/internal/ownerservice"
)

// Start configures the target gRPC surface in the existing Fabric process.
// Its database is explicitly separate from the legacy operation-store setting;
// absence disables this surface without changing the legacy HTTP lifecycle.
func Start(ctx context.Context, getenv func(string) string, dispatchers ...LocalResourceDispatcher) (*ownerservice.Bootstrap, error) {
	if strings.TrimSpace(getenv("OPL_FABRIC_DATABASE_URL")) == "" {
		return nil, nil
	}
	ownerEnv := func(key string) string {
		if key == "DATABASE_URL" {
			return getenv("OPL_FABRIC_DATABASE_URL")
		}
		return getenv(key)
	}
	config, err := ownerservice.LoadConfig(ownerEnv, ownerservice.OwnerFabric, ":8187")
	if err != nil {
		return nil, err
	}
	catalogAddr := strings.TrimSpace(getenv("OPL_RESOURCE_CATALOG_ADDR"))
	if catalogAddr == "" {
		return nil, errors.New("OPL_RESOURCE_CATALOG_ADDR is required for Fabric resource acceptance")
	}
	source, err := ownermigrations.Source()
	if err != nil {
		return nil, err
	}
	return ownerservice.Start(ctx, config, source, func(server *ownerservice.Server, database *ownerservice.Database) error {
		if database == nil {
			return ownerservice.ErrHandlersNotImplemented
		}
		authorizer, identityConn, err := ownerservice.AuthorizerFromConfig(config)
		if err != nil {
			return err
		}
		if identityConn != nil {
			if err = server.TrackCloser(identityConn); err != nil {
				identityConn.Close()
				return err
			}
		}
		options, err := config.TLS.DialOptions(owneridentity.Fabric.Service(), owneridentity.ResourceCatalog.Service(), getenv("OPL_RESOURCE_CATALOG_TOKEN"))
		if err != nil {
			return err
		}
		conn, err := grpc.NewClient(catalogAddr, options...)
		if err != nil {
			return err
		}
		if err = server.TrackCloser(conn); err != nil {
			conn.Close()
			return err
		}
		service, err := New(database.DB(), authorizer.Authorize, api.NewCatalogCoordinationClient(conn))
		if err != nil {
			return err
		}
		if len(dispatchers) == 1 && dispatchers[0] != nil {
			ledgerAddr := strings.TrimSpace(getenv("OPL_LEDGER_ADDR"))
			if ledgerAddr == "" {
				return errors.New("OPL_LEDGER_ADDR is required for local resource dispatch")
			}
			options, err := config.TLS.DialOptions(owneridentity.Fabric.Service(), owneridentity.Ledger.Service(), getenv("OPL_LEDGER_TOKEN"))
			if err != nil {
				return err
			}
			ledger, err := grpc.NewClient(ledgerAddr, options...)
			if err != nil {
				return err
			}
			if err = server.TrackCloser(ledger); err != nil {
				ledger.Close()
				return err
			}
			service.Dispatcher, service.Ledger = dispatchers[0], api.NewLedgerCoordinationClient(ledger)
		}
		return service.Register(server)
	})
}
