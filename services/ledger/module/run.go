package module

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/postgresmigrate"
	"opl-cloud/services/ledger/eventconsumer"
	ledgerhttp "opl-cloud/services/ledger/internal/http"
	"opl-cloud/services/ledger/internal/ledger"
)

// Run executes the Ledger module (HTTP + optional gRPC coordination listener) until ctx is canceled.
func Run(ctx context.Context, env ownerservice.ModuleEnv) error {
	addr := env.Env("LEDGER_ADDR")
	if addr == "" {
		addr = ":8081"
	}

	databaseURL, err := StoreDatabaseURL(env.Env)
	if err != nil {
		return fmt.Errorf("ledger startup failed stage=database_url: %w", err)
	}
	token, err := InternalServiceToken(env.Env)
	if err != nil {
		return fmt.Errorf("ledger startup failed stage=service_token: %w", err)
	}
	capabilityKey, err := LedgerCapabilityKey(env.Env, token)
	if err != nil {
		return fmt.Errorf("ledger startup failed stage=capability_key: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	store := ledger.Store(ledger.NewMemoryStore())
	var domainDB *sql.DB
	if databaseURL != "" {
		pool, db, err := postgresmigrate.OpenPool(runCtx, databaseURL, nil)
		if err != nil {
			return fmt.Errorf("ledger startup failed stage=open_pool: %w", err)
		}
		defer func() {
			_ = db.Close()
			pool.Close()
		}()
		domainDB = db
		postgresStore := ledger.NewPostgresStore(db)
		migrateCtx, migrateCancel := context.WithTimeout(runCtx, 30*time.Second)
		err = postgresStore.Install(migrateCtx)
		migrateCancel()
		if err != nil {
			return fmt.Errorf("ledger startup failed stage=install_migrations: %w", err)
		}
		store = postgresStore
	}

	if env.Env("OPL_LEDGER_ADDR") != "" {
		config, err := ownerservice.LoadConfig(env.Env, ownerservice.OwnerLedger, ":8186")
		if err != nil {
			return fmt.Errorf("ledger startup failed stage=load_config: %w", err)
		}
		config.TLS.InProc = env.InProc
		consumer, err := eventconsumer.New(domainDB)
		if err != nil {
			return fmt.Errorf("ledger startup failed stage=eventconsumer_new: %w", err)
		}
		closeCoordination, err := consumer.ConfigureCoordination(config, env.Env)
		if err != nil {
			return fmt.Errorf("ledger startup failed stage=eventconsumer_coordination: %w", err)
		}
		defer closeCoordination()
		server, err := consumer.NewGRPC(config)
		if err != nil {
			return fmt.Errorf("ledger startup failed stage=eventconsumer_grpc: %w", err)
		}
		var inprocLn net.Listener
		if env.InProc != nil {
			inprocLn, err = env.InProc.Listen(owneridentity.Ledger.Service())
			if err != nil {
				return fmt.Errorf("ledger startup failed stage=inproc_listen: %w", err)
			}
			go func() {
				_ = server.Serve(inprocLn)
			}()
		}
		listener, err := net.Listen("tcp", config.Addr)
		if err != nil {
			if inprocLn != nil {
				_ = inprocLn.Close()
			}
			return fmt.Errorf("ledger startup failed stage=grpc_listen: %w", err)
		}
		defer func() {
			server.GracefulStop()
			if inprocLn != nil {
				_ = inprocLn.Close()
			}
		}()
		go func() {
			if err := server.Serve(listener); err != nil && runCtx.Err() == nil {
				log.Printf("ledger grpc stopped: %v", err)
				cancel()
			}
		}()
	}

	httpListener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("ledger startup failed stage=http_listen: %w", err)
	}
	handler := ledgerhttp.NewServerWithAuth(store, token, capabilityKey)
	httpServer := NewHTTPServer(addr, handler)
	errCh := make(chan error, 1)
	go func() {
		log.Printf("ledger listening on %s", addr)
		err := httpServer.Serve(httpListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			cancel()
			errCh <- fmt.Errorf("ledger startup failed stage=http_listen: %w", err)
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

// LedgerCapabilityKey validates OPL_LEDGER_CAPABILITY_KEY against the transport token.
func LedgerCapabilityKey(getenv func(string) string, transportToken string) (string, error) {
	key := strings.TrimSpace(getenv("OPL_LEDGER_CAPABILITY_KEY"))
	if (getenv("NODE_ENV") == "production" || transportToken != "" || key != "") && len(key) < 32 {
		return "", errors.New("OPL_LEDGER_CAPABILITY_KEY must contain at least 32 characters when Ledger transport is configured")
	}
	if key != "" && key == transportToken {
		return "", errors.New("Ledger capability key must be distinct from its transport token")
	}
	return key, nil
}

// NewHTTPServer builds the bounded HTTP server for Ledger APIs.
func NewHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute,
	}
}

// InternalServiceToken validates OPL_INTERNAL_SERVICE_TOKEN.
func InternalServiceToken(getenv func(string) string) (string, error) {
	token := getenv("OPL_INTERNAL_SERVICE_TOKEN")
	if getenv("NODE_ENV") == "production" && token == "" {
		return "", errors.New("OPL_INTERNAL_SERVICE_TOKEN is required in production")
	}
	return token, nil
}

// StoreDatabaseURL validates Ledger's PostgreSQL DSN.
func StoreDatabaseURL(getenv func(string) string) (string, error) {
	databaseURL := strings.TrimSpace(getenv("DATABASE_URL"))
	if getenv("NODE_ENV") == "production" && databaseURL == "" {
		return "", errors.New("DATABASE_URL is required for production Ledger persistence")
	}
	if databaseURL != "" {
		if err := postgresmigrate.ValidateTLS(databaseURL); err != nil && !LocalPostgresTestDatabaseURL(getenv, databaseURL) {
			return "", err
		}
	}
	return databaseURL, nil
}

// LocalPostgresTestDatabaseURL accepts local loopback DB URLs strictly under test opt-in.
func LocalPostgresTestDatabaseURL(getenv func(string) string, databaseURL string) bool {
	if getenv("NODE_ENV") == "production" || getenv("OPL_POSTGRES_TESTS") != "1" {
		return false
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" || parsed.Host == "" || parsed.Fragment != "" {
		return false
	}
	query := parsed.Query()
	if len(query["sslmode"]) != 1 || query.Get("sslmode") != "disable" || query.Has("host") {
		return false
	}
	address := net.ParseIP(parsed.Hostname())
	return address != nil && address.IsLoopback()
}
