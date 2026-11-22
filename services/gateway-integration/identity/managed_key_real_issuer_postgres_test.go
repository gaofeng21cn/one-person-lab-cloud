package identity_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/gateway-integration/identity"
)

// realIssuerGatewayStub serves the Gateway routes the real Workspace key
// issuer uses on top of the shared wallet fixture: the visible Group list, the
// key creation and the model catalog the issued Key reads for itself.
func realIssuerGatewayStub(t *testing.T, catalog []map[string]any) *gatewayWalletStub {
	t.Helper()
	return &gatewayWalletStub{extra: map[string]http.HandlerFunc{
		"/api/v1/groups/available": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": []map[string]any{{"id": 9, "name": "Codex", "status": "active"}}})
		},
		"/api/v1/keys": func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Name    string `json:"name"`
				GroupID int64  `json:"group_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"id": 4242, "user_id": 101, "key": "sk-created-once", "name": body.Name, "group_id": body.GroupID, "status": "active",
			}})
		},
		"/v1/models": func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": catalog})
		},
	}}
}

// realIssuerCoordination wires the owner to the real Gateway Workspace key
// issuer. The tenant's payer is the subject this fixture logged in as, so the
// issuer receives the delegated credential it requires.
func realIssuerCoordination(t *testing.T, stub *gatewayWalletStub, secrets identity.SecretStore) (*identity.Service, api.GatewayCoordinationClient) {
	t.Helper()
	service, tenantDB, cookie, console, workspace := coordinationSystem(t, stub)
	issueGrant(t, tenantDB, "grant-workspace", "op-1", "ws-1", "createworkspace")
	if _, err := console.BindWallet(t.Context(), &api.WalletBindingCommand{Context: platformCaller("bind", cookie), TargetTenantId: "tenant-coord", BillingSub2ApiUserId: "101", AuthorizationReceiptId: "auth-receipt-1"}); err != nil {
		t.Fatalf("bind wallet: %v", err)
	}
	issuer, err := identity.NewGatewayWorkspaceKeyIssuer(service.Gateway, "Codex")
	if err != nil {
		t.Fatal(err)
	}
	service.WorkspaceKeyGroup = "Codex"
	service.KeyIssuer = issuer
	service.SecretStore = secrets
	return service, workspace
}

// TestManagedKeyCommandConfirmsADeclaredSelectionThroughTheRealIssuer proves the
// customer path that stalled: a non-empty declared model selection, issued
// through the real Gateway issuer, is confirmed against the issued Key's own
// catalog, reaches the Secret and binding delivery, and replays as the one
// original command instead of minting a second Key.
func TestManagedKeyCommandConfirmsADeclaredSelectionThroughTheRealIssuer(t *testing.T) {
	stub := realIssuerGatewayStub(t, []map[string]any{{"id": "model-a"}, {"id": "model-b"}})
	secrets := &managedKeySecretStore{}
	service, workspace := realIssuerCoordination(t, stub, secrets)
	command := managedKeyCommand("managed-key-real-issuer", "model-a", "model-b")
	first, err := workspace.CreateManagedKey(t.Context(), command)
	if err != nil {
		t.Fatalf("create managed key through the real issuer: %v", err)
	}
	if first.GetKeyBindingId() == "" || first.GetFingerprint() == "" || first.GetSecretDeliveryReference() == "" {
		t.Fatalf("incomplete managed key binding: %v", first)
	}
	var recordedModels string
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT model_ids::text FROM gateway.key_bindings WHERE workspace_id = 'ws-1'`).Scan(&recordedModels); err != nil {
		t.Fatal(err)
	}
	if recordedModels != "{model-a,model-b}" {
		t.Fatalf("binding model scope=%s want the confirmed declared selection", recordedModels)
	}
	if secrets.writeCount() != 1 {
		t.Fatalf("secret writes=%d want one delivery", secrets.writeCount())
	}
	// The original command replays as the one original key.
	replay, err := workspace.CreateManagedKey(t.Context(), command)
	if err != nil || replay.GetKeyBindingId() != first.GetKeyBindingId() || secrets.writeCount() != 1 {
		t.Fatalf("replay err=%v binding=%v writes=%d", err, replay, secrets.writeCount())
	}
}

// TestManagedKeyCommandRefusesADeclaredModelTheKeyCannotUse proves the
// confirmation is a real check and not a copy of the requested models: a model
// the issued Key may not use is refused, nothing is delivered, and the refusal
// is recorded for the original command instead of being re-issued.
func TestManagedKeyCommandRefusesADeclaredModelTheKeyCannotUse(t *testing.T) {
	stub := realIssuerGatewayStub(t, []map[string]any{{"id": "model-a"}})
	secrets := &managedKeySecretStore{}
	service, workspace := realIssuerCoordination(t, stub, secrets)
	command := managedKeyCommand("managed-key-real-refusal", "model-a", "model-z")
	if _, err := workspace.CreateManagedKey(t.Context(), command); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("err=%v want the refused issuance", err)
	}
	var bindings int
	if err := service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.key_bindings WHERE workspace_id = 'ws-1'`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 0 || secrets.writeCount() != 0 {
		t.Fatalf("a refused issuance delivered a key: bindings=%d writes=%d", bindings, secrets.writeCount())
	}
	var body string
	if err := service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT response_body::text FROM gateway.idempotency_records
		WHERE operation_name = 'CreateManagedKey' AND idempotency_key = $1`, "opl-test:managed-key-real-refusal").Scan(&body); err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		Outcome   string `json:"outcome"`
		ErrorCode string `json:"errorCode"`
	}
	if json.Unmarshal([]byte(body), &recorded) != nil || recorded.Outcome != "rejected" || !strings.Contains(recorded.ErrorCode, "PermissionDenied") {
		t.Fatalf("recorded answer=%s", body)
	}
	// The recorded refusal answers the original command; it is never re-issued.
	if _, err := workspace.CreateManagedKey(t.Context(), command); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("replay err=%v want the recorded refusal", err)
	}
}
