//go:build !linux

package coordination_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
)

type dockerAPIFixture struct {
	server *httptest.Server
	mu     sync.Mutex
	calls  []string
}

// newDockerInfoAPIFixture starts an in-process Docker Engine HTTP stand-in. It
// answers only API version negotiation (/_ping) and the provider's read-only
// /info check, refuses every other endpoint with HTTP 500, and records every
// request it receives.
func newDockerInfoAPIFixture(t *testing.T) *dockerAPIFixture {
	t.Helper()
	fixture := &dockerAPIFixture{}
	fixture.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		fixture.calls = append(fixture.calls, r.Method+" "+r.URL.Path)
		fixture.mu.Unlock()
		switch {
		case (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path == "/_ping":
			w.Header().Set("Api-Version", "1.47")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
		case r.Method == http.MethodGet && (r.URL.Path == "/info" || strings.HasSuffix(r.URL.Path, "/info")):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ServerVersion":"29.0.0-test-fixture"}`))
		default:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("fixture_refused_unexpected_request\n"))
		}
	}))
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *dockerAPIFixture) resetCalls() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = nil
}

func (f *dockerAPIFixture) snapshotCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// The real provider must reject an unsupported host before creating a network;
// Docker Desktop's Linux daemon is not the Fabric host's project-quota backend.
func TestLocalDispatcherRejectsUnsupportedHostQuota(t *testing.T) {
	t.Setenv("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON", `{"schemaVersion":1,"profileId":"profile","capabilityVersion":"v1","region":"local","accountBindings":[{"tenantId":"tenant","accountId":"account"}],"packages":[{"id":"package","name":"Package","available":true,"compute":{"id":"compute-sku","server":"2c4g","cpu":2,"memoryGb":4,"instanceType":"local-2c4g"},"storage":{"id":"storage-sku","sizeGb":10,"quotaPolicy":"linux-project"}}]}`)

	fixture := newDockerInfoAPIFixture(t)
	// Point the Moby Go SDK client at the scratch HTTP stand-in instead of any
	// local Docker daemon socket.
	t.Setenv("DOCKER_HOST", fixture.server.URL)
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	// The unmeasured control phase proves the fixture fails closed on endpoints
	// it was not built for; reset recorded requests before the measured phase.
	assertDockerFixtureRefusesUnexpectedRequest(t, fixture)
	fixture.resetCalls()

	provider := fabric.NewLocalDockerProvider()
	service := fabric.NewService(provider)
	dispatcher := coordination.NewLocalDispatcher(service, provider)
	_, err := dispatcher.EnsureResources(context.Background(), coordination.ResourceIntent{TenantID: "tenant", WorkspaceID: "quota-guard-never-created", OperationID: "quota-guard", ComputeID: "quota-compute-never-created", StorageID: "quota-storage-never-created", Plan: &api.ResourcePlanSnapshot{Provider: "local-docker", ProviderProfileId: "profile", ProviderCapabilityVersion: "v1", Region: "local", BillingMode: "LOCAL_NO_CHARGE", ProviderComputeSkuId: "compute-sku", ProviderStorageSkuId: "storage-sku", Vcpus: 2, MemoryMib: 4096, CapacityGib: 10}})
	if err == nil || !strings.Contains(err.Error(), "local_docker_storage_quota_unavailable") {
		t.Fatalf("host quota guard=%v", err)
	}
	if _, found := service.GetComputeAllocation(context.Background(), "quota-compute-never-created"); found {
		t.Fatal("compute was dispatched before quota admission")
	}
	calls := fixture.snapshotCalls()
	infoCalls := 0
	for _, call := range calls {
		switch {
		case call == "GET /_ping" || call == "HEAD /_ping":
			continue
		case call == "GET /info" || (strings.HasPrefix(call, "GET /v") && strings.HasSuffix(call, "/info")):
			infoCalls++
		default:
			t.Fatalf("unexpected docker API call=%q all=%q", call, calls)
		}
	}
	if infoCalls != 1 {
		t.Fatalf("docker fixture calls=%q, want exactly one /info call", calls)
	}
}

func assertDockerFixtureRefusesUnexpectedRequest(t *testing.T, fixture *dockerAPIFixture) {
	t.Helper()
	resp, err := http.Get(fixture.server.URL + "/containers/json")
	if err != nil {
		t.Fatalf("control request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read control body: %v", err)
	}
	if resp.StatusCode != http.StatusInternalServerError || !strings.Contains(string(body), "fixture_refused_unexpected_request") {
		t.Fatalf("docker fixture accepted unexpected request: status=%d body=%q", resp.StatusCode, body)
	}
	calls := fixture.snapshotCalls()
	if len(calls) != 1 || calls[0] != "GET /containers/json" {
		t.Fatalf("refused docker fixture calls=%q", calls)
	}
}
