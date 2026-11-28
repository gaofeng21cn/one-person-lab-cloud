// Command server runs the Console BFF: the browser-facing same-origin REST
// surface that aggregates Cloud owner DTOs. It owns no business database and no
// business write.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/apps/console-bff/module"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := module.Run(ctx, owneridentity.OSModuleEnv()); err != nil {
		log.Fatal(err)
	}
}
