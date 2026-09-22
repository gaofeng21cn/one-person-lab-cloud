package main

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"opl-cloud/services/capability/internal/store"
)

func TestHealthIsAlwaysLive(t *testing.T) {
	handler := healthHandler(store.New(nil))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("healthz content type = %q", got)
	}
}

// Readiness must fail closed: an owner whose database is unavailable is never
// reported as ready.
func TestReadinessFailsClosedWithoutADatabase(t *testing.T) {
	handler := healthHandler(store.New(nil))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if body := recorder.Body.String(); body != `{"status":"not_ready"}` {
		t.Fatalf("readyz body = %s", body)
	}
}

// Readiness must also be able to report ready: a probe that answered not-ready
// unconditionally would satisfy the fail-closed test while being useless. The
// probe driver answers the driver-level ping, so this exercises the ready
// branch without a database server.
func TestReadinessReportsReadyWhenTheDatabaseAnswers(t *testing.T) {
	sql.Register("capability_readyz_test", pingDriver{})
	db, err := sql.Open("capability_readyz_test", "capability")
	if err != nil {
		t.Fatalf("open probe database: %v", err)
	}
	defer db.Close()

	handler := healthHandler(store.New(db))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("readyz status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if body := recorder.Body.String(); body != `{"status":"ready"}` {
		t.Fatalf("readyz body = %s", body)
	}
}

// pingDriver is a database/sql driver whose connection answers readiness
// probes. It prepares no statements and begins no transactions.
type pingDriver struct{}

func (pingDriver) Open(string) (driver.Conn, error) { return pingConn{}, nil }

type pingConn struct{}

func (pingConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("ping driver prepares no statements")
}

func (pingConn) Close() error { return nil }

func (pingConn) Begin() (driver.Tx, error) {
	return nil, errors.New("ping driver begins no transactions")
}

func (pingConn) Ping(context.Context) error { return nil }
