package httpapi

import (
	"net/http"
	"net/http/httptest"
	"opl-cloud/apps/console-bff/internal/clients"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
	"strings"
	"testing"
)

func TestPersonalGatewayBFFPreservesSubjectPaginationAndMoney(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get(contracts.CloudSessionHeader) != owneridentity.SessionReference("session-1") || r.Header.Get("X-Actor-Id") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("unexpected identity headers")
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/internal/cloud/gateway/endpoint" {
			w.Write([]byte(`{"source":"sub2api","available":true,"status":"available","fetchedAt":"2026-10-16T00:00:00Z","data":{"baseUrl":"https://gateway.example.test/v1"}}`))
			return
		}
		if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("pageSize") != "1" {
			t.Errorf("pagination=%s", r.URL)
		}
		w.Write([]byte(`{"source":"sub2api","available":true,"status":"available","fetchedAt":"2026-10-16T00:00:00Z","data":{"items":[{"type":"balance","valueUsdMicros":"-9223372036854775808","status":"used","usedAt":null,"createdAt":"2026-10-16T00:00:00Z"}],"total":2,"page":2,"pageSize":1,"pages":2}}`))
	}))
	defer upstream.Close()
	reader, err := clients.Dial(clients.Config{ControlPlaneURL: upstream.URL, ControlPlaneToken: "personal-gateway-test-token-00001"})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	identity := allowedIdentity()
	identity.session.TenantId = nil
	handler := NewServer(reader, identity).Handler()
	for _, path := range []string{"/api/v2/gateway/balance-history?page=2&pageSize=1", "/api/v2/gateway/endpoint"} {
		r := sessionRequest("GET", path)
		r.Header.Set("X-Actor-Id", "someone-else")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("read=%d %s", w.Code, w.Body.String())
		}
		if strings.Contains(path, "balance-history") && !strings.Contains(w.Body.String(), `"valueUsdMicros":"-9223372036854775808"`) {
			t.Fatal("money precision lost")
		}
	}
	before := calls
	for _, path := range []string{"/api/v2/gateway/balance-history?userId=other", "/api/v2/gateway/balance-history?page=1&page=2", "/api/v2/gateway/balance-history?pageSize=101", "/api/v2/gateway/endpoint?tenantId=other"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, sessionRequest("GET", path))
		if w.Code != 400 {
			t.Fatalf("query=%d", w.Code)
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v2/gateway/endpoint", nil))
	if w.Code != 401 || calls != before {
		t.Fatalf("guard=%d calls=%d", w.Code, calls)
	}
}
