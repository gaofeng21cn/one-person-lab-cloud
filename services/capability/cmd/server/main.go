// Command server starts the Capability owner process.
//
// It opens this owner's own database, installs this owner's own migrations,
// registers the shared owner Operation readback group, and reports SERVING only
// when its declared dependencies and product groups are actually ready.
// Product handlers and the restricted object data plane start only when their
// explicit owner configuration is available.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/capability/catalog"

	"opl-cloud/services/capability/migrations"
	"opl-cloud/services/internal/ownerservice"
)

func main() {
	config, err := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerCapability, ":8181")
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
		root, publicURL, schemaPath, schemaDigest, signing := os.Getenv("OPL_CAPABILITY_OBJECT_ROOT"), os.Getenv("OPL_CAPABILITY_OBJECT_URL"), os.Getenv("OPL_CAPABILITY_PACKAGE_SCHEMA_PATH"), os.Getenv("OPL_CAPABILITY_PACKAGE_SCHEMA_DIGEST"), os.Getenv("OPL_CAPABILITY_OBJECT_SIGNING_KEY")
		if root == "" || publicURL == "" || schemaPath == "" || schemaDigest == "" || len(signing) < 32 {
			return ownerservice.ErrHandlersNotImplemented
		}
		objects, err := catalog.NewObjects(root, publicURL, []byte(signing), catalog.UploadPolicy{MaxBytes: 1 << 30, PartBytes: 16 << 20, MaxExpandedBytes: 4 << 30, MaxFiles: 100000, TTL: 24 * time.Hour, ManifestPath: "manifest.json", SchemaPath: schemaPath, SchemaDigest: schemaDigest})
		if err != nil {
			return err
		}
		tls := owneridentity.TLSFromEnv(os.Getenv)
		runtimeConn, err := dialPeer(config, tls, owneridentity.RuntimeControl, os.Getenv("OPL_RUNTIME_CONTROL_ADDR"))
		if err != nil {
			return err
		}
		if err := server.TrackCloser(runtimeConn); err != nil {
			return err
		}
		buildConn, err := dialPeer(config, tls, owneridentity.Build, os.Getenv("OPL_BUILD_ADDR"))
		if err != nil {
			return err
		}
		if err := server.TrackCloser(buildConn); err != nil {
			return err
		}
		authorizer, _, err := ownerservice.AuthorizerFromConfig(config)
		if err != nil {
			return err
		}
		service, err := catalog.New(database.DB(), authorizer.Authorize, objects)
		if err != nil {
			return err
		}
		service.Runtime = api.NewRuntimeControlProductServiceClient(runtimeConn)
		service.Build = api.NewBuildCoordinationClient(buildConn)
		service.Usage = api.NewClaimUsageReadbackClient(buildConn)
		if err := service.Register(server); err != nil {
			return err
		}
		handler, err := service.DataHandler(os.Getenv("OPL_CAPABILITY_OBJECT_TOKEN"))
		if err != nil {
			return err
		}
		address := os.Getenv("OPL_CAPABILITY_OBJECT_LISTEN_ADDR")
		if address == "" {
			return fmt.Errorf("Capability object listener is required")
		}
		listener, err := net.Listen("tcp", address)
		if err != nil {
			return err
		}
		dataServer := &http.Server{Handler: handler, ReadHeaderTimeout: 10 * time.Second}
		if err := server.TrackCloser(dataServer); err != nil {
			listener.Close()
			return err
		}
		go func() {
			if err := dataServer.Serve(listener); err != nil && err != http.ErrServerClosed {
				log.Print("Capability data plane stopped unexpectedly")
				stop()
			}
		}()
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
