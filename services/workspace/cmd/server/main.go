// Command server starts the Workspace owner process.
//
// It owns accepted launch orders and resumes their original Catalog and Fabric
// commands from durable state in the same Workspace process.
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
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/workspace/internal/launch"
	"opl-cloud/services/workspace/migrations"
)

func main() {
	config, err := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerWorkspace, ":8184")
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
		catalogConn, err := dialPeer(config, server, owneridentity.ResourceCatalog, os.Getenv("OPL_RESOURCE_CATALOG_ADDR"))
		if err != nil {
			return err
		}
		fabricConn, err := dialPeer(config, server, owneridentity.Fabric, os.Getenv("OPL_FABRIC_ADDR"))
		if err != nil {
			return err
		}
		service, err := launch.New(database.DB(), authorizer, api.NewCatalogCoordinationClient(catalogConn), api.NewFabricCoordinationClient(fabricConn), api.NewCloudIdentityAuthorizationClient(identityConn))
		if err != nil {
			return err
		}
		if address := strings.TrimSpace(os.Getenv("OPL_LEDGER_ADDR")); address != "" {
			ledgerConn, err := dialPeer(config, server, owneridentity.Ledger, address)
			if err != nil {
				return err
			}
			service.Ledger = api.NewLedgerCoordinationClient(ledgerConn)
		}
		if address := strings.TrimSpace(os.Getenv("OPL_CAPABILITY_ADDR")); address != "" {
			conn, err := dialPeer(config, server, owneridentity.Capability, address)
			if err != nil {
				return err
			}
			service.Capability = api.NewCapabilityProductServiceClient(conn)
		}
		if address := strings.TrimSpace(os.Getenv("OPL_SERVE_ADDR")); address != "" {
			conn, err := dialPeer(config, server, owneridentity.Serve, address)
			if err != nil {
				return err
			}
			service.Serve = api.NewServeAgentCoordinationClient(conn)
		}
		if err = service.Register(server); err != nil {
			return err
		}
		go func() {
			if err := service.Run(ctx); err != nil && ctx.Err() == nil {
				log.Printf("Workspace recovery stopped: %v", err)
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

func dialPeer(config ownerservice.Config, server *ownerservice.Server, target owneridentity.Owner, address string) (*grpc.ClientConn, error) {
	if strings.TrimSpace(address) == "" {
		return nil, fmt.Errorf("%s address is required", target)
	}
	options, err := config.TLS.DialOptions(config.Owner.Service(), target.Service(), os.Getenv("OPL_"+strings.ToUpper(target.String())+"_TOKEN"))
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
