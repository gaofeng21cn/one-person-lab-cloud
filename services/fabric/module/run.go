package module

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
	fabrichttp "opl-cloud/services/fabric/internal/http"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/postgresmigrate"
)

// Run executes the Fabric module (HTTP + optional coordination gRPC listener) until ctx is canceled.
func Run(ctx context.Context, env ownerservice.ModuleEnv) error {
	addr := env.Env("FABRIC_ADDR")
	if addr == "" {
		addr = ":8082"
	}

	databaseURL, err := OperationStoreDatabaseURL(env.Env)
	if err != nil {
		return err
	}
	authConfig, err := FabricServerAuthFromEnv(env.Env)
	if err != nil {
		return err
	}
	operationStore := fabric.OperationStore(fabric.NewMemoryOperationStore())
	if databaseURL != "" {
		store, err := fabric.NewPostgresOperationStore(databaseURL)
		if err != nil {
			return err
		}
		operationStore = store
	}
	provider, err := SelectedProvider(env)
	if err != nil {
		return err
	}
	fabricService := fabric.NewServiceWithOperationStore(provider, operationStore)
	handler := fabrichttp.NewServerWithAuth(fabricService, authConfig)

	var dispatcher coordination.ResourceDispatcher
	if databaseURL != "" {
		switch typed := provider.(type) {
		case *fabric.LocalDockerProvider:
			dispatcher = coordination.NewLocalDispatcher(fabricService, typed)
		case *fabric.TencentProvider:
			dispatcher = coordination.NewTencentDispatcher(fabricService, typed)
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	owner, err := coordination.StartWithEnv(runCtx, env, dispatcher)
	if err != nil {
		return err
	}
	if owner != nil {
		defer owner.Close()
		defer owner.Server.Stop()
		go func() {
			if err := owner.Server.Serve(); err != nil && runCtx.Err() == nil {
				log.Printf("Fabric coordination stopped: %v", err)
				cancel()
			}
		}()
	}

	httpListener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	httpServer := NewHTTPServer(addr, handler)
	errCh := make(chan error, 1)
	go func() {
		log.Printf("fabric listening on %s", addr)
		err := httpServer.Serve(httpListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel()
			errCh <- err
			return
		}
		errCh <- nil
	}()

	<-runCtx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = httpServer.Shutdown(shutdownCtx)
	return <-errCh
}

// SelectedProvider resolves the configured Fabric provider adapter using env.
func SelectedProvider(env ownerservice.ModuleEnv) (fabric.Provider, error) {
	name := strings.TrimSpace(env.Env("OPL_FABRIC_PROVIDER"))
	if name == "" {
		return nil, errors.New("OPL_FABRIC_PROVIDER is required")
	}
	switch name {
	case "local-docker":
		return fabric.NewLocalDockerProviderWithEnv(env.Env, env.Lookup), nil
	case "tencent-tke":
		return fabric.NewTencentProviderWithEnv(env.Env), nil
	default:
		return nil, errors.New("OPL_FABRIC_PROVIDER must be local-docker or tencent-tke")
	}
}

// NewHTTPServer builds the bounded Fabric HTTP server.
func NewHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 10 * time.Minute, IdleTimeout: 2 * time.Minute,
	}
}

// FabricServerAuthFromEnv validates Fabric's HTTP bearer and capability credentials.
func FabricServerAuthFromEnv(getenv func(string) string) (fabrichttp.ServerAuthConfig, error) {
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

// OperationStoreDatabaseURL validates Fabric's operation-store DSN.
func OperationStoreDatabaseURL(getenv func(string) string) (string, error) {
	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
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
