// Command server starts the Capability owner process.
//
// It opens this owner's own database, installs this owner's own migrations,
// registers the shared owner Operation readback group, and reports SERVING only
// when its declared dependencies and product groups are actually ready.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/services/capability/module"
	"opl-cloud/services/internal/ownerservice"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := module.Run(ctx, ownerservice.OSModuleEnv()); err != nil {
		log.Fatal(err)
	}
}
