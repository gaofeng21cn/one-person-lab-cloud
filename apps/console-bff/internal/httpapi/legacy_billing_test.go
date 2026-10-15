package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opl-cloud/apps/console-bff/internal/clients"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func TestLegacyBillingBFFUsesSessionOnlyAndPreservesOriginalMoney(t *testing.T) {
	const token = "historical-billing-test-token-000001"
	calls := 0
	ownerStatus := 200
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != "GET" || r.Header.Get(owneridentity.PeerHeader) != "console_bff" || r.Header.Get(owneridentity.TokenHeader) != token || r.Header.Get(contracts.CloudSessionHeader) != owneridentity.SessionReference("session-1") || r.Header.Get("Cookie") != "" || r.Header.Get("X-Actor-Id") != "" || r.Header.Get("X-Tenant-Id") != "" {
			t.Errorf("unexpected authority: %s %v", r.Method, r.Header)
		}
		w.WriteHeader(ownerStatus)
		if ownerStatus != 200 {
			return
		}
		amount := int64(9007199254740991)
		receipt := contracts.LegacyBillingReceipt{ReceiptID: "original", Type: "billing.workspace_purchased.v1", Currency: "USD", ChargeUSDMicros: &amount, Components: json.RawMessage(`{"original":9007199254740991}`), RefundCode: "original-refund-code", AccountID: "original-account", LaunchReceiptID: "original-launch", ResourceStatus: json.RawMessage(`{"workspaceStatus":"absent"}`), StageEvidence: json.RawMessage(`[{"stage":"confirmed"}]`)}
		switch r.URL.Path {
		case "/internal/cloud/legacy-billing/receipts":
			if r.URL.Query().Get("cursor") != "opaque" || r.URL.Query().Get("limit") != "1" {
				t.Errorf("pagination=%s", r.URL)
			}
			_ = json.NewEncoder(w).Encode(contracts.LegacyBillingSource[contracts.LegacyBillingPage]{Source: "ledger", Status: "available", Available: true, FetchedAt: "2026-10-16T00:00:00Z", Data: &contracts.LegacyBillingPage{Receipts: []contracts.LegacyBillingReceipt{receipt}, NextCursor: "next", HasMore: true}})
		case "/internal/cloud/legacy-billing/receipts/original":
			_ = json.NewEncoder(w).Encode(contracts.LegacyBillingSource[contracts.LegacyBillingReceipt]{Source: "ledger", Status: "available", Available: true, FetchedAt: "2026-10-16T00:00:00Z", Data: &receipt})
		case "/internal/cloud/legacy-billing/workspace-settlements":
			_ = json.NewEncoder(w).Encode(contracts.LegacyBillingSource[contracts.LegacySettlementTrend]{Source: "control_plane", Status: "available", Available: true, FetchedAt: "2026-10-16T00:00:00Z", Data: &contracts.LegacySettlementTrend{Days: []contracts.LegacySettlementDay{}, Timezone: "Asia/Shanghai", UnconfirmedCount: 1, Complete: false}})
		default:
			t.Errorf("unexpected path %s", r.URL)
		}
	}))
	defer owner.Close()
	readers, err := clients.Dial(clients.Config{ControlPlaneURL: owner.URL, ControlPlaneToken: token})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	identity := allowedIdentity()
	identity.session.TenantId = nil
	handler := NewServer(readers, identity).Handler()
	for _, path := range []string{"/api/v2/legacy-billing/receipts?cursor=opaque&limit=1", "/api/v2/legacy-billing/receipts/original", "/api/v2/legacy-billing/workspace-settlements"} {
		r := sessionRequest("GET", path)
		r.Header.Set("X-Actor-Id", "another")
		r.Header.Set("X-Tenant-Id", "another")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s=%d %s", path, w.Code, w.Body.String())
		}
		if strings.Contains(path, "receipts") && !strings.Contains(w.Body.String(), `9007199254740991`) {
			t.Fatal("original money changed")
		}
		if strings.Contains(path, "receipts") {
			for _, original := range []string{"original-refund-code", "original-account", "original-launch", `"workspaceStatus":"absent"`, `"stage":"confirmed"`} {
				if !strings.Contains(w.Body.String(), original) {
					t.Fatalf("original receipt field lost: %s", original)
				}
			}
		}
		if strings.Contains(path, "settlements") && (!strings.Contains(w.Body.String(), `"complete":false`) || !strings.Contains(w.Body.String(), `"unconfirmedCount":1`)) {
			t.Fatal("unknown settlement hidden")
		}
	}
	before := calls
	for _, path := range []string{"/api/v2/legacy-billing/receipts?accountId=another", "/api/v2/legacy-billing/receipts?limit=0", "/api/v2/legacy-billing/receipts?limit=1&limit=2", "/api/v2/legacy-billing/workspace-settlements?tenantId=another"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, sessionRequest("GET", path))
		if w.Code != 400 {
			t.Fatalf("selector=%d %s", w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/api/v2/legacy-billing/receipts", nil))
	if w.Code != 401 || calls != before {
		t.Fatalf("session guard=%d calls=%d", w.Code, calls)
	}
	ownerStatus = 503
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, sessionRequest("GET", "/api/v2/legacy-billing/receipts?cursor=opaque&limit=1"))
	if w.Code != 503 {
		t.Fatalf("unavailable=%d %s", w.Code, w.Body.String())
	}
}
