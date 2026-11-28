package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/services/build/module"
	"opl-cloud/services/internal/ownerservice"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := module.Run(ctx, ownerservice.OSModuleEnv()); err != nil {
		log.Fatal(err)
	}
}
