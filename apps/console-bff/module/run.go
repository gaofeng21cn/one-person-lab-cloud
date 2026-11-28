package module

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"opl-cloud/apps/console-bff/internal/clients"
	"opl-cloud/apps/console-bff/internal/httpapi"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// Run executes the Console BFF HTTP/MCP gateway module until ctx is canceled.
func Run(ctx context.Context, env owneridentity.ModuleEnv) error {
	addr := env.Env("OPL_BFF_ADDR")
	if addr == "" {
		addr = ":8190"
	}

	config := clients.ConfigFromEnv(env.Env)
	config.TLS.InProc = env.InProc
	if config.CloudIdentityAddr == "" {
		return errors.New("OPL_CLOUD_IDENTITY_URL is required: browser session and authorization are resolved through CloudIdentity")
	}
	peerClients, err := clients.Dial(config)
	if err != nil {
		return fmt.Errorf("dial domain owners: %w", err)
	}
	defer peerClients.Close()

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen console-bff %s: %w", addr, err)
	}

	handler := httpapi.NewServerWithEnv(peerClients, peerClients, env.Env).Handler()
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      3 * time.Minute,
		IdleTimeout:       2 * time.Minute,
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("console-bff listening on %s", addr)
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel()
			errCh <- fmt.Errorf("serve console-bff: %w", err)
			return
		}
		errCh <- nil
	}()

	<-runCtx.Done()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = server.Shutdown(shutdownCtx)
	return <-errCh
}
