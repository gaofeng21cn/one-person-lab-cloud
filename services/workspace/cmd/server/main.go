// Command server runs the workspace owner: its own Go module, process, database, and
// internal typed gRPC surface.
package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"google.golang.org/grpc"

	v226 "opl-cloud/packages/contracts/go/v226"
	"opl-cloud/services/internal/postgresmigrate"
	"opl-cloud/services/workspace/internal/store"
	"opl-cloud/services/workspace/internal/transport"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required for the workspace owner database")
	}
	if err := postgresmigrate.ValidateTLS(databaseURL); err != nil {
		log.Fatalf("workspace DATABASE_URL is not accepted: %v", err)
	}

	grpcAddr := envOr("WORKSPACE_ADDR", ":8093")
	httpAddr := envOr("WORKSPACE_HTTP_ADDR", ":8193")

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("open workspace database: %v", err)
	}
	defer db.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	workspaceStore := store.New(db)
	if err := workspaceStore.Install(ctx); err != nil {
		log.Fatalf("install workspace schema: %v", err)
	}

	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("listen on %s: %v", grpcAddr, err)
	}
	grpcServer := grpc.NewServer()
	server := transport.NewServer(workspaceStore)
	v226.RegisterOwnerOperationsServer(grpcServer, server)
	v226.RegisterOwnerCommitReadbackServer(grpcServer, server)
	v226.RegisterDomainInboxServer(grpcServer, server)

	healthServer := &http.Server{
		Addr:              httpAddr,
		Handler:           healthHandler(workspaceStore),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("workspace owner gRPC listening on %s", grpcAddr)
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("serve workspace gRPC: %v", err)
		}
	}()
	go func() {
		log.Printf("workspace owner health listening on %s", httpAddr)
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve workspace health: %v", err)
		}
	}()

	<-ctx.Done()
	grpcServer.GracefulStop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown workspace health: %v", err)
	}
}

// healthHandler reports liveness and readiness. Readiness fails closed when the
// owner's database is not reachable; an unavailable dependency is never
// reported as ready.
func healthHandler(workspaceStore *store.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := workspaceStore.Ready(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"not_ready"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})
	return mux
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
