package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/postgresmigrate"
	"opl-cloud/services/ledger/eventconsumer"
	ledgerhttp "opl-cloud/services/ledger/internal/http"
	"opl-cloud/services/ledger/internal/ledger"
)

func main() {
	addr := os.Getenv("LEDGER_ADDR")
	if addr == "" {
		addr = ":8081"
	}

	databaseURL, err := storeDatabaseURL(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	token, err := internalServiceToken(os.Getenv)
	if err != nil {
		log.Fatal(err)
	}
	capabilityKey, err := ledgerCapabilityKey(os.Getenv, token)
	if err != nil {
		log.Fatal(err)
	}
	store := ledger.Store(ledger.NewMemoryStore())
	var domainDB *sql.DB
	if databaseURL != "" {
		db, err := sql.Open("postgres", databaseURL)
		if err != nil {
			log.Fatal(err)
		}
		domainDB = db
		postgresStore := ledger.NewPostgresStore(db)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := postgresStore.Install(ctx); err != nil {
			log.Fatal(err)
		}
		store = postgresStore
	}

	if os.Getenv("OPL_LEDGER_ADDR") != "" {
		config, err := ownerservice.LoadConfig(os.Getenv, ownerservice.OwnerLedger, ":8186")
		if err != nil {
			log.Fatal(err)
		}
		consumer, err := eventconsumer.New(domainDB)
		if err != nil {
			log.Fatal(err)
		}
		server, err := consumer.NewGRPC(config)
		if err != nil {
			log.Fatal(err)
		}
		listener, err := net.Listen("tcp", config.Addr)
		if err != nil {
			log.Fatal(err)
		}
		defer server.Stop()
		go func() {
			if err := server.Serve(listener); err != nil {
				log.Fatal("Ledger domain listener stopped")
			}
		}()
	}

	handler := ledgerhttp.NewServerWithAuth(store, token, capabilityKey)
	log.Printf("ledger listening on %s", addr)
	if err := newHTTPServer(addr, handler).ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func ledgerCapabilityKey(getenv func(string) string, transportToken string) (string, error) {
	key := strings.TrimSpace(getenv("OPL_LEDGER_CAPABILITY_KEY"))
	if (getenv("NODE_ENV") == "production" || transportToken != "" || key != "") && len(key) < 32 {
		return "", errors.New("OPL_LEDGER_CAPABILITY_KEY must contain at least 32 characters when Ledger transport is configured")
	}
	if key != "" && key == transportToken {
		return "", errors.New("Ledger capability key must be distinct from its transport token")
	}
	return key, nil
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute,
	}
}

func internalServiceToken(getenv func(string) string) (string, error) {
	token := getenv("OPL_INTERNAL_SERVICE_TOKEN")
	if getenv("NODE_ENV") == "production" && token == "" {
		return "", errors.New("OPL_INTERNAL_SERVICE_TOKEN is required in production")
	}
	return token, nil
}

func storeDatabaseURL(getenv func(string) string) (string, error) {
	databaseURL := getenv("DATABASE_URL")
	if getenv("NODE_ENV") == "production" && databaseURL == "" {
		return "", errors.New("DATABASE_URL is required for production Ledger persistence")
	}
	if databaseURL != "" {
		if err := postgresmigrate.ValidateTLS(databaseURL); err != nil && !localPostgresTestDatabaseURL(getenv, databaseURL) {
			return "", err
		}
	}
	return databaseURL, nil
}

func localPostgresTestDatabaseURL(getenv func(string) string, databaseURL string) bool {
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
