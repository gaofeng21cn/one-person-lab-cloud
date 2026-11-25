package http

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"opl-cloud/services/fabric/internal/fabric"
)

// The reconciliation completes one original prepaid CBS whose terminate was
// dispatched but never confirmed. Only the operator scope may authorize it, and
// the signed capability must name the exact body it authorizes.
func TestStoragePrepaidTerminationReconciliationCapabilityAndDecoder(t *testing.T) {
	valid := `{"accountId":"acct-alpha","workspaceId":"ws-alpha","destroyOperationId":"original-destroy","authorizationReference":"terminate-reconcile-1","confirmation":"RECONCILE_ORIGINAL_PREPAID_TERMINATION"}`
	for _, tc := range []struct {
		name, body, caller, action, key string
		want                            int
	}{
		{"ordinary destroy cannot authorize reconciliation", valid, "control-plane", "destroy_storage_volume", "terminate-reconcile-1", http.StatusForbidden},
		{"workspace authority cannot authorize reconciliation", valid, "workspace", "reconcile_prepaid_storage_termination", "terminate-reconcile-1", http.StatusForbidden},
		{"mismatched authorization reference", valid, "operator", "reconcile_prepaid_storage_termination", "other-intent", http.StatusBadRequest},
		{"unknown body field", bytes.NewBufferString(valid[:len(valid)-1]+`,"extra":true}`).String(), "operator", "reconcile_prepaid_storage_termination", "terminate-reconcile-1", http.StatusBadRequest},
		{"trailing JSON", valid + `{}`, "operator", "reconcile_prepaid_storage_termination", "terminate-reconcile-1", http.StatusForbidden},
		{"invalid JSON", `{`, "operator", "reconcile_prepaid_storage_termination", "terminate-reconcile-1", http.StatusForbidden},
		{"exact capability reaches owner", valid, "operator", "reconcile_prepaid_storage_termination", "terminate-reconcile-1", http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := fabric.NewMemoryOperationStore()
			body := []byte(tc.body)
			claims := fabricCapabilityClaimsForTest{Version: 1, Caller: tc.caller, AccountID: "acct-alpha", WorkspaceID: "ws-alpha", ResourceKind: "storage_volume", ResourceID: "storage-alpha", Action: tc.action, OperationID: tc.key, ExpiresAt: time.Now().Add(time.Minute).Unix()}
			capabilityBody := body
			if tc.name == "trailing JSON" || tc.name == "invalid JSON" {
				capabilityBody = []byte(valid)
			}
			server := NewServerWithAuth(fabric.NewServiceWithOperationStore(testProvider{}, store), ServerAuthConfig{ControlPlaneToken: "internal-secret", CapabilityKey: testFabricCapabilityKey})
			req := httptest.NewRequest(http.MethodPost, "/fabric/storage-volumes/storage-alpha/reconcile-prepaid-termination", bytes.NewReader(body))
			req.Header.Set("Authorization", "Bearer internal-secret")
			req.Header.Set("Idempotency-Key", tc.key)
			req.Header.Set(fabricCapabilityHeader, fabricCapabilityForTest(t, claims, capabilityBody))
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			rows, err := store.List(t.Context())
			if err != nil || len(rows) != 0 {
				t.Fatalf("refusal mutated journal: %v %v", rows, err)
			}
		})
	}
}
