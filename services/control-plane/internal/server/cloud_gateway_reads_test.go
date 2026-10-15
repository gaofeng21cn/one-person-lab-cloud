package server

import (
	"google.golang.org/grpc/codes"
	"net/http"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
	"strings"
	"testing"
	"time"
)

func TestCloudGatewayHistoryUsesLiveUserWithoutLegacyAccount(t *testing.T) {
	at := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	gateway := &sourceTruthGatewayClient{customerFactsSub2API: &customerFactsSub2API{testSub2APIClient: &testSub2APIClient{charges: map[string]int64{}}, history: map[int64][]clients.Sub2APIBalanceHistoryEntry{901: {{Type: "balance", ValueUSDMicros: -9223372036854775808, Status: "used", UsedAt: &at, CreatedAt: at}}}}}
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore(), controlplane.NewService(&customerFactsLedger{}, &fakeFabricClient{}, gateway))
	alice := f.gateway.bind("personal-gateway-alice", "cloud-alice", "901")
	bob := f.gateway.bind("personal-gateway-bob", "cloud-bob", "902")
	base := "/internal/cloud/gateway/balance-history"
	got := f.request(t, "GET", base+"?page=1&pageSize=20", alice, "")
	if got.code != 200 || !strings.Contains(string(got.body), `"valueUsdMicros":"-9223372036854775808"`) {
		t.Fatalf("history=%d %s", got.code, got.body)
	}
	other := f.request(t, "GET", base, bob, "")
	if other.code != 200 || strings.Contains(string(other.body), "9223372036854775808") {
		t.Fatalf("other history=%d %s", other.code, other.body)
	}
	endpoint := f.request(t, "GET", "/internal/cloud/gateway/endpoint", alice, "")
	if endpoint.code != 200 || !strings.Contains(string(endpoint.body), "https://gateway.example.test/v1") {
		t.Fatalf("endpoint=%d %s", endpoint.code, endpoint.body)
	}
	for _, path := range []string{base + "?userId=902", base + "?page=1&page=2", base + "?pageSize=101", base + "?page=1000001", "/internal/cloud/gateway/endpoint?tenantId=x"} {
		if denied := f.request(t, "GET", path, alice, ""); denied.code != http.StatusBadRequest {
			t.Fatalf("query=%d %s", denied.code, denied.body)
		}
	}
	f.gateway.fail(alice, codes.Unauthenticated)
	if expired := f.request(t, "GET", base, alice, ""); expired.code != 401 {
		t.Fatalf("expired=%d", expired.code)
	}
	f.gateway.fail(alice, codes.Unavailable)
	if missing := f.request(t, "GET", base, alice, ""); missing.code != 503 {
		t.Fatalf("unavailable=%d", missing.code)
	}
}
