package main

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	_ "github.com/lib/pq"

	"opl-cloud/services/resource-catalog/internal/store"
)

// Readiness fails closed: an unreachable owner database is reported as
// not-ready with 503, never as healthy.
func TestReadyzFailsClosedWhenTheOwnerDatabaseIsUnreachable(t *testing.T) {
	db, err := sql.Open("postgres", "connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	handler := healthHandler(store.New(db))

	ready := httptest.NewRecorder()
	handler.ServeHTTP(ready, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if ready.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", ready.Code, http.StatusServiceUnavailable)
	}
	if body := ready.Body.String(); body != `{"status":"not_ready"}` {
		t.Fatalf("readyz body = %s", body)
	}

	// Liveness reports the process itself; it does not claim dependency health.
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", health.Code, http.StatusOK)
	}
	if body := health.Body.String(); body != `{"status":"ok"}` {
		t.Fatalf("healthz body = %s", body)
	}
}
