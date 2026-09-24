// Command server starts the Workspace owner process.
//
// It opens this owner's own database, installs this owner's own migrations,
// registers the shared owner Operation readback group, and reports SERVING only
// when its declared dependencies and product groups are actually ready. The
// WorkspaceProductService domain handlers are not implemented yet, so this process reports
// NOT_SERVING with that exact reason instead of claiming readiness it does not
// have or exiting.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/services/internal/ownerservice"
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
		_ = database
		return ownerservice.ErrHandlersNotImplemented
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
