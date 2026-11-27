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
		amount := int64(9007199254740993)
		receipt := contracts.LegacyBillingReceipt{ReceiptID: "original", Type: "billing.workspace_purchased.v1", Currency: "USD", ChargeUSDMicros: &amount, Components: json.RawMessage(`{"original":9007199254740991,"compute":{"chargeUsdMicros":9007199254740993}}`), RefundCode: "original-refund-code", AccountID: "original-account", LaunchReceiptID: "original-launch", ResourceStatus: json.RawMessage(`{"workspaceStatus":"absent"}`), StageEvidence: json.RawMessage(`[{"stage":"confirmed"}]`)}
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
		if strings.Contains(path, "receipts") && !strings.Contains(w.Body.String(), `"chargeUsdMicros":"9007199254740993"`) {
			t.Fatal("original money changed")
		}
		if strings.Contains(path, "receipts") {
			var envelope map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			data := envelope["data"].(map[string]any)
			if page, ok := data["receipts"].([]any); ok {
				data = page[0].(map[string]any)
			}
			if data["chargeUsdMicros"] != "9007199254740993" || data["components"].(map[string]any)["compute"].(map[string]any)["chargeUsdMicros"] != "9007199254740993" {
				t.Fatalf("browser JSON loses micros: %s", w.Body.String())
			}
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

func TestLegacyBillingBFFPreservesAccountNotFoundReason(t *testing.T) {
	const token = "historical-billing-test-token-000001"
	var ownerStatus int
	var ownerBody string
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ownerStatus)
		_, _ = w.Write([]byte(ownerBody))
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

	ownerStatus = http.StatusNotFound
	ownerBody = `{"error":"legacy_billing_account_not_found"}`
	for _, path := range []string{"/api/v2/legacy-billing/receipts?limit=3", "/api/v2/legacy-billing/workspace-settlements"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, sessionRequest("GET", path))
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d body=%s", path, w.Code, w.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["code"] != "NOT_FOUND" || payload["message"] != "legacy_billing_account_not_found" {
			t.Fatalf("%s payload=%v", path, payload)
		}
	}

	ownerStatus = http.StatusNotFound
	ownerBody = `{"error":"billing_receipt_not_found"}`
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, sessionRequest("GET", "/api/v2/legacy-billing/receipts/missing"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("receipt not found status=%d body=%s", w.Code, w.Body.String())
	}
	var missingPayload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &missingPayload); err != nil {
		t.Fatal(err)
	}
	if missingPayload["code"] != "NOT_FOUND" || missingPayload["message"] == "legacy_billing_account_not_found" {
		t.Fatalf("missing receipt leaked account reason: %v", missingPayload)
	}

	ownerStatus = http.StatusConflict
	ownerBody = `{"error":"legacy_billing_identity_lineage_invalid"}`
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, sessionRequest("GET", "/api/v2/legacy-billing/receipts?limit=3"))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("conflict status=%d body=%s", w.Code, w.Body.String())
	}

	ownerStatus = http.StatusServiceUnavailable
	ownerBody = `{"status":"unavailable"}`
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, sessionRequest("GET", "/api/v2/legacy-billing/workspace-settlements"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d body=%s", w.Code, w.Body.String())
	}
}
