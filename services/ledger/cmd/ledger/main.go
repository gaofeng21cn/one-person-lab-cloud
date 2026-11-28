package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/ledger/module"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := module.Run(ctx, ownerservice.OSModuleEnv()); err != nil {
		log.Fatal(err)
	}
}

func ledgerCapabilityKey(getenv func(string) string, transportToken string) (string, error) {
	return module.LedgerCapabilityKey(getenv, transportToken)
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return module.NewHTTPServer(addr, handler)
}

func internalServiceToken(getenv func(string) string) (string, error) {
	return module.InternalServiceToken(getenv)
}

func storeDatabaseURL(getenv func(string) string) (string, error) {
	return module.StoreDatabaseURL(getenv)
}

func localPostgresTestDatabaseURL(getenv func(string) string, databaseURL string) bool {
	return module.LocalPostgresTestDatabaseURL(getenv, databaseURL)
}
