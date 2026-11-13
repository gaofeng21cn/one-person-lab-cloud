package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func TestHealthzReportsBakedProductRevisionInsteadOfDeploymentClaim(t *testing.T) {
	previous := contracts.ProductSourceSHA
	t.Cleanup(func() { contracts.ProductSourceSHA = previous })
	t.Setenv(contracts.ProductSourceSHAEnv, strings.Repeat("b", 40))
	server := NewServer(newTestService(fakeLedgerClient{}, &fakeFabricClient{}))
	for _, baked := range []string{strings.Repeat("a", 40), ""} {
		contracts.ProductSourceSHA = baked
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/healthz", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("health status=%d, body=%s", recorder.Code, recorder.Body.String())
		}
		var body map[string]string
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["status"] != "ok" || body["productSourceSha"] != baked {
			t.Fatalf("health response=%v, want baked revision %q", body, baked)
		}
	}
}
