package module

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/resource-catalog/catalog"
	"opl-cloud/services/resource-catalog/migrations"
)

// Run executes the Resource Catalog owner module until ctx is canceled.
func Run(ctx context.Context, env ownerservice.ModuleEnv) error {
	config, err := ownerservice.LoadConfig(env.Env, ownerservice.OwnerResourceCatalog, ":8186")
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
		profile, err := loadProfile(env.Env("OPL_RESOURCE_CATALOG_PROFILE_JSON"))
		if err != nil {
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
		service, err := catalog.New(database.DB(), authorizer, profile)
		if err != nil {
			return err
		}
		if address := strings.TrimSpace(env.Env("OPL_LEDGER_ADDR")); address != "" {
			conn, err := dialPeer(config, env.Env, owneridentity.Ledger, address)
			if err != nil {
				return err
			}
			if err := server.TrackCloser(conn); err != nil {
				conn.Close()
				return err
			}
			service.Ledger = api.NewDomainInboxClient(conn)
		}
		if err := service.Register(server); err != nil {
			return err
		}
		go service.RunPolicyDelivery(runCtx)
		return nil
	})
	if err != nil {
		return err
	}
	defer bootstrap.Close()
	return ownerservice.ServeContext(runCtx, bootstrap.Server)
}

func loadProfile(raw string) (catalog.Profile, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return catalog.Profile{}, fmt.Errorf("OPL_RESOURCE_CATALOG_PROFILE_JSON is required")
	}
	var document struct {
		Provider                  string `json:"provider"`
		Region                    string `json:"region"`
		BillingMode               string `json:"billingMode"`
		ProviderCapabilityVersion string `json:"providerCapabilityVersion"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&document); err != nil {
		return catalog.Profile{}, fmt.Errorf("OPL_RESOURCE_CATALOG_PROFILE_JSON is invalid: %w", err)
	}
	return catalog.Profile{
		Provider:                  document.Provider,
		Region:                    document.Region,
		BillingMode:               document.BillingMode,
		ProviderCapabilityVersion: document.ProviderCapabilityVersion,
	}, nil
}

func dialPeer(config ownerservice.Config, getenv func(string) string, target owneridentity.Owner, address string) (*grpc.ClientConn, error) {
	if address == "" {
		return nil, fmt.Errorf("%s address is required", target)
	}
	options, err := config.TLS.DialOptions(config.Owner.Service(), target.Service(), getenv("OPL_"+strings.ToUpper(target.String())+"_TOKEN"))
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(address, options...)
}
