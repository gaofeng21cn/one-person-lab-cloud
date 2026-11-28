package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"opl-cloud/services/fabric/internal/fabric"
	fabrichttp "opl-cloud/services/fabric/internal/http"
	"opl-cloud/services/fabric/module"
	"opl-cloud/services/internal/ownerservice"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := module.Run(ctx, ownerservice.OSModuleEnv()); err != nil {
		log.Fatal(err)
	}
}

func selectedProvider(getenv func(string) string) (fabric.Provider, error) {
	return module.SelectedProvider(ownerservice.ModuleEnv{Getenv: getenv})
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return module.NewHTTPServer(addr, handler)
}

func fabricServerAuthFromEnv(getenv func(string) string) (fabrichttp.ServerAuthConfig, error) {
	return module.FabricServerAuthFromEnv(getenv)
}

func operationStoreDatabaseURL(getenv func(string) string) (string, error) {
	return module.OperationStoreDatabaseURL(getenv)
}
