package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
	fabrichttp "opl-cloud/services/fabric/internal/http"
	"opl-cloud/services/internal/postgresmigrate"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	addr := os.Getenv("FABRIC_ADDR")
	if addr == "" {
		addr = ":8082"
	}

	databaseURL, err := operationStoreDatabaseURL(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	authConfig, err := fabricServerAuthFromEnv(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	operationStore := fabric.OperationStore(fabric.NewMemoryOperationStore())
	if databaseURL != "" {
		store, err := fabric.NewPostgresOperationStore(databaseURL)
		if err != nil {
			log.Fatal(err)
		}
		operationStore = store
	}
	provider, err := selectedProvider(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	fabricService := fabric.NewServiceWithOperationStore(provider, operationStore)
	handler := fabrichttp.NewServerWithAuth(fabricService, authConfig)
	var dispatcher coordination.LocalResourceDispatcher
	if local, ok := provider.(*fabric.LocalDockerProvider); ok && databaseURL != "" {
		dispatcher = coordination.NewLocalDispatcher(fabricService, local)
	}
	owner, err := coordination.Start(ctx, os.Getenv, dispatcher)
	if err != nil {
		log.Fatal(err)
	}
	if owner != nil {
		defer owner.Close()
		go func() {
			<-ctx.Done()
			owner.Server.Stop()
		}()
		go func() {
			if err := owner.Server.Serve(); err != nil && ctx.Err() == nil {
				log.Printf("Fabric coordination stopped: %v", err)
				stop()
			}
		}()
	}
	httpServer := newHTTPServer(addr, handler)
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(shutdown)
	}()
	log.Printf("fabric listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func selectedProvider(getenv func(string) string) (fabric.Provider, error) {
	name := strings.TrimSpace(getenv("OPL_FABRIC_PROVIDER"))
	if name == "" {
		return nil, errors.New("OPL_FABRIC_PROVIDER is required")
	}
	switch name {
	case "local-docker":
		return fabric.NewLocalDockerProvider(), nil
	case "tencent-tke":
		return fabric.NewTencentProvider(), nil
	default:
		return nil, errors.New("OPL_FABRIC_PROVIDER must be local-docker or tencent-tke")
	}
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 10 * time.Minute, IdleTimeout: 2 * time.Minute,
	}
}

func fabricServerAuthFromEnv(getenv func(string) string) (fabrichttp.ServerAuthConfig, error) {
	config := fabrichttp.ServerAuthConfig{
		ControlPlaneToken:  strings.TrimSpace(getenv("OPL_INTERNAL_SERVICE_TOKEN")),
		RunnerToken:        strings.TrimSpace(getenv("OPL_FABRIC_RUNNER_SERVICE_TOKEN")),
		CapabilityKey:      strings.TrimSpace(getenv("OPL_FABRIC_CAPABILITY_KEY")),
		ServeToken:         strings.TrimSpace(getenv("OPL_FABRIC_SERVE_SERVICE_TOKEN")),
		ServeCapabilityKey: strings.TrimSpace(getenv("OPL_FABRIC_SERVE_CAPABILITY_KEY")),
	}
	configured := 0
	for _, value := range []string{config.ControlPlaneToken, config.RunnerToken, config.CapabilityKey} {
		if value != "" {
			configured++
		}
	}
	if getenv("NODE_ENV") == "production" || configured > 0 {
		missing := make([]string, 0, 3)
		if config.ControlPlaneToken == "" {
			missing = append(missing, "OPL_INTERNAL_SERVICE_TOKEN")
		}
		if config.RunnerToken == "" {
			missing = append(missing, "OPL_FABRIC_RUNNER_SERVICE_TOKEN")
		}
		if len(config.CapabilityKey) < 32 {
			missing = append(missing, "OPL_FABRIC_CAPABILITY_KEY (32+ characters)")
		}
		if len(missing) > 0 {
			return fabrichttp.ServerAuthConfig{}, fmt.Errorf("missing required Fabric authorization configuration: %s", strings.Join(missing, ", "))
		}
	}
	if config.ControlPlaneToken != "" && (config.ControlPlaneToken == config.RunnerToken || config.ControlPlaneToken == config.CapabilityKey || config.RunnerToken == config.CapabilityKey) {
		return fabrichttp.ServerAuthConfig{}, errors.New("Fabric transport, runner, and capability credentials must be distinct")
	}
	if config.ServeToken != "" || config.ServeCapabilityKey != "" {
		if len(config.ServeToken) < 32 || len(config.ServeCapabilityKey) < 32 {
			return fabrichttp.ServerAuthConfig{}, errors.New("Serve transport and capability credentials must both contain 32+ characters")
		}
		for _, v := range []string{config.ControlPlaneToken, config.RunnerToken, config.CapabilityKey} {
			if config.ServeToken == v || config.ServeCapabilityKey == v {
				return fabrichttp.ServerAuthConfig{}, errors.New("Serve credentials must be distinct from other Fabric credentials")
			}
		}
		if config.ServeToken == config.ServeCapabilityKey {
			return fabrichttp.ServerAuthConfig{}, errors.New("Serve transport and capability credentials must be distinct")
		}
	}
	return config, nil
}

func operationStoreDatabaseURL(getenv func(string) string) (string, error) {
	databaseURL := getenv("DATABASE_URL")
	if getenv("NODE_ENV") == "production" && databaseURL == "" {
		return "", errors.New("DATABASE_URL is required for production Fabric persistence")
	}
	if databaseURL != "" {
		if err := postgresmigrate.ValidateTLS(databaseURL); err != nil {
			return "", err
		}
	}
	return databaseURL, nil
}
