package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	gatewaystore "opl-cloud/services/gateway-integration/internal/gateway/store"
	tenantstore "opl-cloud/services/gateway-integration/internal/tenant/store"
)

// readinessOwner stands in for one owner database without opening a connection,
// so both directions of the two-owner readiness rule can be exercised.
type readinessOwner struct{ err error }

func (o readinessOwner) Ready(context.Context) error { return o.err }

var errUnreachable = errors.New("owner database unreachable")

func TestHealthIsAlwaysLive(t *testing.T) {
	handler := healthHandler(readinessOwner{err: errUnreachable}, readinessOwner{err: errUnreachable})
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
// reported as ready, and one reachable owner is not enough for the unit.
func TestReadinessRequiresBothOwners(t *testing.T) {
	cases := map[string]struct {
		tenant  error
		gateway error
		want    int
	}{
		"both owners reachable": {want: http.StatusOK},
		"tenant unreachable":    {tenant: errUnreachable, want: http.StatusServiceUnavailable},
		"gateway unreachable":   {gateway: errUnreachable, want: http.StatusServiceUnavailable},
		"both unreachable":      {tenant: errUnreachable, gateway: errUnreachable, want: http.StatusServiceUnavailable},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			handler := healthHandler(readinessOwner{err: testCase.tenant}, readinessOwner{err: testCase.gateway})
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if recorder.Code != testCase.want {
				t.Fatalf("readyz status = %d, want %d", recorder.Code, testCase.want)
			}
			if testCase.want == http.StatusServiceUnavailable && recorder.Body.String() != `{"status":"not_ready"}` {
				t.Fatalf("readyz body = %s", recorder.Body.String())
			}
		})
	}
}

func TestReadinessFailsClosedWithoutADatabase(t *testing.T) {
	handler := healthHandler(tenantstore.New(nil), gatewaystore.New(nil))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if body := recorder.Body.String(); body != `{"status":"not_ready"}` {
		t.Fatalf("readyz body = %s", body)
	}
}

func TestEnvOrDefaultsNameBothOwnersAndPorts(t *testing.T) {
	t.Setenv("GATEWAY_INTEGRATION_ADDR", "")
	t.Setenv("GATEWAY_INTEGRATION_HTTP_ADDR", "")
	if got := envOr("GATEWAY_INTEGRATION_ADDR", ":8096"); got != ":8096" {
		t.Fatalf("default gRPC address = %q", got)
	}
	if got := envOr("GATEWAY_INTEGRATION_HTTP_ADDR", ":8196"); got != ":8196" {
		t.Fatalf("default health address = %q", got)
	}
	t.Setenv("GATEWAY_INTEGRATION_ADDR", ":9000")
	if got := envOr("GATEWAY_INTEGRATION_ADDR", ":8096"); got != ":9000" {
		t.Fatalf("configured gRPC address = %q", got)
	}
}
