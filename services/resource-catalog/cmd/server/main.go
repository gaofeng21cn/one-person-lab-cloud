// Command server starts the Resource Catalog owner process.
//
// It opens this owner's own database, installs this owner's own migrations,
// registers the shared owner Operation readback group, and reports SERVING only
// when its declared dependencies and product groups are actually ready. The
// process holds the instance's approved provider/billing profile because the
// catalog never lets a customer choose a provider.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"google.golang.org/grpc"

	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/resource-catalog/catalog"
	"opl-cloud/services/resource-catalog/migrations"
)

func main() {
	config, err := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerResourceCatalog, ":8186")
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	source, err := migrations.Source()
	if err != nil {
		log.Fatal(err)
	}

	bootstrap, err := ownerservice.Start(ctx, config, source, func(server *ownerservice.Server, database *ownerservice.Database) error {
		if database == nil {
			return ownerservice.ErrHandlersNotImplemented
		}
		profile, err := loadProfile(os.Getenv("OPL_RESOURCE_CATALOG_PROFILE_JSON"))
		if err != nil {
			return err
		}
		authorizer, _, err := ownerservice.AuthorizerFromConfig(config)
		if err != nil {
			return err
		}
		service, err := catalog.New(database.DB(), authorizer, profile)
		if err != nil {
			return err
		}
		if address := strings.TrimSpace(os.Getenv("OPL_LEDGER_ADDR")); address != "" {
			conn, err := dialPeer(config, owneridentity.Ledger, address)
			if err != nil {
				return err
			}
			if err := server.TrackCloser(conn); err != nil {
				return err
			}
			service.Ledger = api.NewDomainInboxClient(conn)
		}
		if err := service.Register(server); err != nil {
			return err
		}
		go service.RunPolicyDelivery(ctx)
		return nil
	})
	if err != nil {
		log.Fatal(err)
	}
	defer bootstrap.Close()

	server := bootstrap.Server
	go func() {
		<-ctx.Done()
		server.Stop()
	}()
	if err := server.Serve(); err != nil {
		log.Fatal(err)
	}
}

// loadProfile admits the instance's approved provider profile. It is required:
// without an approved provider, region, billing mode and provider capability
// version the owner refuses to start rather than publishing plans it cannot
// price. The profile is a machine contract, so an unknown field is rejected.
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

func dialPeer(config ownerservice.Config, target owneridentity.Owner, address string) (*grpc.ClientConn, error) {
	if address == "" {
		return nil, fmt.Errorf("%s address is required", target)
	}
	options, err := config.TLS.DialOptions(config.Owner.Service(), target.Service(), os.Getenv("OPL_"+strings.ToUpper(target.String())+"_TOKEN"))
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(address, options...)
}
