// Command server starts the RuntimeControl owner process.
//
// It opens this owner's own database, installs this owner's own migrations,
// registers the shared owner Operation readback group, and reports SERVING only
// when its declared dependencies and product groups are actually ready. The
// RuntimeControlProductService domain handlers are not implemented yet, so this process reports
// NOT_SERVING with that exact reason instead of claiming readiness it does not
// have or exiting.
package main

import (
	"context"
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
	"opl-cloud/services/runtime-control/catalog"
	"opl-cloud/services/runtime-control/migrations"
)

func main() {
	config, err := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerRuntimeControl, ":8183")
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
		schemaPath, schemaDigest := os.Getenv("OPL_RUNTIME_PUBLISHER_SCHEMA_PATH"), os.Getenv("OPL_RUNTIME_PUBLISHER_SCHEMA_DIGEST")
		if schemaPath == "" || schemaDigest == "" {
			return ownerservice.ErrHandlersNotImplemented
		}
		tls := owneridentity.TLSFromEnv(os.Getenv)
		capabilityConn, err := dialPeer(config, tls, owneridentity.Capability, os.Getenv("OPL_CAPABILITY_ADDR"))
		if err != nil {
			return err
		}
		if err := server.TrackCloser(capabilityConn); err != nil {
			return err
		}
		authorizer, _, err := ownerservice.AuthorizerFromConfig(config)
		if err != nil {
			return err
		}
		service, err := catalog.New(database.DB(), authorizer, api.NewCapabilityProductServiceClient(capabilityConn), schemaPath, schemaDigest)
		if err != nil {
			return err
		}
		return service.Register(server)
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

func dialPeer(config ownerservice.Config, tls owneridentity.TLSConfig, target owneridentity.Owner, address string) (*grpc.ClientConn, error) {
	if address == "" {
		return nil, fmt.Errorf("%s address is required", target)
	}
	options, err := tls.DialOptions(config.Owner.Service(), target.Service(), os.Getenv("OPL_"+strings.ToUpper(target.String())+"_TOKEN"))
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(address, options...)
}
