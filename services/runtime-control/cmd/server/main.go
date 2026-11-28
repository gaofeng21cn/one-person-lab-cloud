// Command server starts the RuntimeControl owner process.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/runtime-control/module"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := module.Run(ctx, ownerservice.OSModuleEnv()); err != nil {
		log.Fatal(err)
	}
}
