package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/packages/contracts/go/owneridentity"
	controlplanemodule "opl-cloud/services/control-plane/module"
)

var (
	ledgerCapabilityKeyFromEnv   = controlplanemodule.LedgerCapabilityKeyFromEnv
	newHTTPServer                = controlplanemodule.NewHTTPServer
	sub2APIConfigFromEnv         = controlplanemodule.Sub2APIConfigFromEnv
	internalServiceTokensFromEnv = controlplanemodule.InternalServiceTokensFromEnv
	fabricCapabilityKeyFromEnv   = controlplanemodule.FabricCapabilityKeyFromEnv
)

func controlPlaneAddr() string {
	return controlplanemodule.ControlPlaneAddr(os.Getenv)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := controlplanemodule.Run(ctx, owneridentity.OSModuleEnv()); err != nil {
		log.Fatal(err)
	}
}
