// Command server starts the Workspace owner process.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/workspace/module"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "retry-initial-runtime" {
		os.Exit(module.RunInitialRuntimeRetryCLI(os.Args[2:], os.Stdin, os.Stdout))
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := module.Run(ctx, ownerservice.OSModuleEnv()); err != nil {
		log.Fatal(err)
	}
}
