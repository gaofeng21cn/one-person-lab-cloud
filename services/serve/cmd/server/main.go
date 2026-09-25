// Command server starts the Serve owner process.
//
// It opens this owner's own database, installs this owner's own migrations,
// registers the shared owner Operation readback group, and reports SERVING only
// when its declared dependencies and product groups are actually ready.
//
// Serve currently implements its own product read surface: the current Agent
// deployment, the delivery history, and the current Agent's access facts, all
// read from Serve's own database. The delivery write path (Reserve/Deploy and
// route switching) is not implemented yet, so Serve registers only the groups it
// can actually answer rather than claiming the rest.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/serve/internal/delivery"
	"opl-cloud/services/serve/migrations"
)

func main() {
	config, err := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerServe, ":8185")
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
		authorizer, _, err := ownerservice.AuthorizerFromConfig(config)
		if err != nil {
			return err
		}
		service, err := delivery.New(database.DB(), authorizer.Authorize)
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
