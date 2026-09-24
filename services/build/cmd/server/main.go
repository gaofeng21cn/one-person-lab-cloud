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
	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	buildservice "opl-cloud/services/build/internal/build"

	"opl-cloud/services/build/migrations"
	"opl-cloud/services/internal/ownerservice"
)

func main() {
	config, err := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerBuild, ":8182")
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
		tls := owneridentity.TLSFromEnv(os.Getenv)
		capabilityConn, err := dialPeer(config, tls, owneridentity.Capability, os.Getenv("OPL_CAPABILITY_ADDR"))
		if err != nil {
			return err
		}
		ledgerConn, err := dialPeer(config, tls, owneridentity.Ledger, os.Getenv("OPL_LEDGER_ADDR"))
		if err != nil {
			return err
		}
		identityConn, err := dialPeer(config, tls, owneridentity.Tenant, config.CloudIdentityAddr)
		if err != nil {
			return err
		}
		runner := &buildservice.Runner{Builder: os.Getenv("OPL_BUILD_BUILDER"), RegistryPrefix: os.Getenv("OPL_BUILD_REGISTRY_PREFIX"), StorageURL: os.Getenv("OPL_CAPABILITY_OBJECT_URL"), StorageToken: os.Getenv("OPL_CAPABILITY_OBJECT_TOKEN"), RegistryToken: os.Getenv("OPL_BUILD_REGISTRY_TOKEN"), DockerConfig: os.Getenv("OPL_BUILD_DOCKER_CONFIG"), WorkDir: os.Getenv("OPL_BUILD_WORKDIR"), MaxPackageBytes: 1 << 30, MaxExpandedBytes: 4 << 30, MaxFiles: 100000, Timeout: 30 * time.Minute, AllowHTTP: os.Getenv("NODE_ENV") != "production"}
		if err := runner.Validate(); err != nil {
			return err
		}
		service, err := buildservice.New(database.Store(), ownerservice.NewAuthorizer(config.Owner, api.NewCloudIdentityAuthorizationClient(identityConn)), api.NewCapabilityCoordinationClient(capabilityConn), api.NewCapabilityProductServiceClient(capabilityConn), map[string]api.DomainInboxClient{"capability": api.NewDomainInboxClient(capabilityConn), "ledger": api.NewDomainInboxClient(ledgerConn)}, api.NewCloudIdentityAuthorizationClient(identityConn), runner)
		if err != nil {
			return err
		}
		if err := service.Register(server); err != nil {
			return err
		}
		go service.Run(ctx)
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
	token := os.Getenv("OPL_" + strings.ToUpper(target.String()) + "_TOKEN")
	options, err := tls.DialOptions(config.Owner.Service(), target.Service(), token)
	if err != nil {
		return nil, err
	}
	return grpc.NewClient(address, options...)
}
