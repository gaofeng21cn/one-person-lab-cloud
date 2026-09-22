package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"opl-cloud/services/workspace/internal/store"
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
