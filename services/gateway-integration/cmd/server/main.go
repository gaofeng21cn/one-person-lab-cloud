// Command server runs the gateway integration unit: CloudIdentity (`tenant`) and
// Gateway Integration (`gateway`) share this Go module, process and deployment
// unit, while each owner keeps its own database, database roles and connection
// pool. The internal typed gRPC surface is the v2.26 OwnerOperations,
// OwnerCommitReadback and DomainInbox contract.
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
	gatewaystore "opl-cloud/services/gateway-integration/internal/gateway/store"
	tenantstore "opl-cloud/services/gateway-integration/internal/tenant/store"
	"opl-cloud/services/gateway-integration/internal/transport"
	"opl-cloud/services/internal/postgresmigrate"
)

func main() {
	// The v2.26 specification fixes the two owner databases and roles but not the
	// environment variable names, so this unit names one variable per owner
	// database. Two separate URLs keep the two pools, and therefore the two data
	// owners, explicitly separate.
	tenantURL := os.Getenv("OPL_TENANT_DATABASE_URL")
	if tenantURL == "" {
		log.Fatal("OPL_TENANT_DATABASE_URL is required for the CloudIdentity (tenant) database")
	}
	gatewayURL := os.Getenv("OPL_GATEWAY_DATABASE_URL")
	if gatewayURL == "" {
		log.Fatal("OPL_GATEWAY_DATABASE_URL is required for the Gateway Integration (gateway) database")
	}
	if err := postgresmigrate.ValidateTLS(tenantURL); err != nil {
		log.Fatalf("OPL_TENANT_DATABASE_URL is not accepted: %v", err)
	}
	if err := postgresmigrate.ValidateTLS(gatewayURL); err != nil {
		log.Fatalf("OPL_GATEWAY_DATABASE_URL is not accepted: %v", err)
	}

	grpcAddr := envOr("GATEWAY_INTEGRATION_ADDR", ":8096")
	httpAddr := envOr("GATEWAY_INTEGRATION_HTTP_ADDR", ":8196")

	tenantDB, err := sql.Open("postgres", tenantURL)
	if err != nil {
		log.Fatalf("open tenant database: %v", err)
	}
	defer tenantDB.Close()
	gatewayDB, err := sql.Open("postgres", gatewayURL)
	if err != nil {
		log.Fatalf("open gateway database: %v", err)
	}
	defer gatewayDB.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Each owner schema is installed on its own connection; the DDL guards its own
	// database name, so a swapped URL fails instead of creating the wrong schema.
	tenantStore := tenantstore.New(tenantDB)
	if err := tenantStore.Install(ctx); err != nil {
		log.Fatalf("install tenant schema: %v", err)
	}
	gatewayStore := gatewaystore.New(gatewayDB)
	if err := gatewayStore.Install(ctx); err != nil {
		log.Fatalf("install gateway schema: %v", err)
	}

	listener, err := net.Listen("tcp", grpcAddr)
	if err != nil {
		log.Fatalf("listen on %s: %v", grpcAddr, err)
	}
	grpcServer := grpc.NewServer()
	server := transport.NewServer(tenantStore, gatewayStore)
	v226.RegisterOwnerOperationsServer(grpcServer, server)
	v226.RegisterOwnerCommitReadbackServer(grpcServer, server)
	v226.RegisterDomainInboxServer(grpcServer, server)

	healthServer := &http.Server{
		Addr:              httpAddr,
		Handler:           healthHandler(tenantStore, gatewayStore),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("gateway integration gRPC listening on %s", grpcAddr)
		if err := grpcServer.Serve(listener); err != nil {
			log.Fatalf("serve gateway integration gRPC: %v", err)
		}
	}()
	go func() {
		log.Printf("gateway integration health listening on %s", httpAddr)
		if err := healthServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve gateway integration health: %v", err)
		}
	}()

	<-ctx.Done()
	grpcServer.GracefulStop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := healthServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown gateway integration health: %v", err)
	}
}

// ownerReadiness is one data owner's readiness check. This unit is ready only
// when BOTH of its owners are ready, so the handler takes exactly those two
// checks instead of one shared health abstraction.
type ownerReadiness interface {
	Ready(ctx context.Context) error
}

// healthHandler reports liveness and readiness for the whole deployment unit.
// Readiness fails closed while EITHER owner's database is unreachable: one owner
// being reachable never reports the unit ready.
func healthHandler(tenant, gateway ownerReadiness) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := tenant.Ready(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"not_ready"}`))
			return
		}
		if err := gateway.Ready(r.Context()); err != nil {
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
