// Command server runs the Console BFF: the browser-facing same-origin REST
// surface that aggregates Cloud owner DTOs. It owns no business database and no
// business write.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"opl-cloud/apps/console-bff/internal/clients"
	"opl-cloud/apps/console-bff/internal/httpapi"
)

func main() {
	addr := os.Getenv("OPL_BFF_ADDR")
	if addr == "" {
		addr = ":8190"
	}

	config := clients.ConfigFromEnv(os.Getenv)
	clients, err := clients.Dial(config)
	if err != nil {
		log.Fatalf("dial domain owners: %v", err)
	}
	defer clients.Close()

	handler := httpapi.NewServer(clients).Handler()
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("console-bff listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve console-bff: %v", err)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown console-bff: %v", err)
	}
}
