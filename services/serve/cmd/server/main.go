// Command server starts the Serve owner process.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/serve/module"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "probe-workspace-access" {
		os.Exit(module.RunWorkspaceAccessProbe(os.Args[2:]))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := module.Run(ctx, ownerservice.OSModuleEnv()); err != nil {
		log.Fatal(err)
	}
}
