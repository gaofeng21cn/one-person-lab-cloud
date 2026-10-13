package identity_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/gateway-integration/identity"
)

// revocationAssembly builds the exact issuer the deployed Gateway wires: the
// production main binds *Sub2APIWorkspaceKeyIssuer to the KeyIssuer field, so a
// failing revoke is only meaningful against that concrete type. The Sub2API it
// points at answers the one approved issuance route and refuses every other
// route, which is how the approved service contract actually reads today: this
// Cloud has no revocation endpoint to call.
func revocationAssembly(t *testing.T) (identity.ManagedKeyIssuer, *atomic.Int32) {
	t.Helper()
	var issuanceCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/service/workspace-keys" || r.Method != http.MethodPost {
			t.Errorf("the gateway contacted an unapproved Sub2API route: %s %s", r.Method, r.URL.Path)
			http.Error(w, "unapproved route", http.StatusNotFound)
			return
		}
		issuanceCalls.Add(1)
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "message": "success",
			"data": map[string]any{"key_id": 4242, "api_key": "sk-managed", "group_id": 9, "model_ids": []string{"model-a"}, "replayed": false}})
	}))
	t.Cleanup(server.Close)
	issuer, err := identity.NewSub2APIWorkspaceKeyIssuer(server.URL, strings.Repeat("s", 32), "workspace-approved")
	if err != nil {
		t.Fatal(err)
	}
	return issuer, &issuanceCalls
}

// TestRevokeManagedKeyRecordsAnUnconfirmedRevocationWhenTheAuthorityCannotRevoke
// is the control for issue #821. The production issuer refuses every revocation,
// so RevokeManagedKey always fails; the failure used to leave the binding
// byte-identical to one nobody ever asked to revoke. A caller therefore could not
// tell "revoked" from "revoke was requested and did not happen". After the fix
// the binding records that a revocation was attempted and left unconfirmed, while
// still reporting the key as not revoked.
func TestRevokeManagedKeyRecordsAnUnconfirmedRevocationWhenTheAuthorityCannotRevoke(t *testing.T) {
	issuer, issuanceCalls := revocationAssembly(t)
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	issued, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	if issuanceCalls.Load() != 1 {
		t.Fatalf("issuance calls=%d want 1: the production issuer must stay a live client", issuanceCalls.Load())
	}

	_, err = workspace.RevokeManagedKey(t.Context(), &api.ManagedKeyRevoke{
		Context:       grantedCaller("revoke", "grant-workspace"),
		KeyBindingId:  issued.GetKeyBindingId(),
		WorkspaceId:   "ws-1",
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("revoke error=%v want Unimplemented: this owner has no approved revocation route and must keep saying so", err)
	}
	if issuanceCalls.Load() != 1 {
		t.Fatalf("issuance calls=%d want 1: a revocation must never contact Sub2API", issuanceCalls.Load())
	}

	binding, readErr := service.GatewayStore.ReadManagedKeyBinding(t.Context(), issued.GetKeyBindingId())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !binding.RevokedAt.IsZero() {
		t.Fatalf("a refused revocation marked the key revoked at %s", binding.RevokedAt)
	}
	// THE CONTROL: the refusal must be observable on the binding itself.
	if binding.ObservationResult == "confirmed" {
		t.Fatal("a refused revocation left no trace: observation_result is still 'confirmed', so the binding is indistinguishable from one never asked to revoke")
	}
	if binding.ObservationResult != "rejected" {
		t.Fatalf("observation_result=%q want 'rejected': Unimplemented is a definite refusal, not an unknown outcome", binding.ObservationResult)
	}

	// The recorded partial state must be findable, not only reachable by id.
	pending, listErr := service.GatewayStore.ListManagedKeyBindingsWithUnconfirmedRevocation(t.Context(), "tenant-coord")
	if listErr != nil {
		t.Fatal(listErr)
	}
	found := false
	for _, entry := range pending {
		if entry.ID == issued.GetKeyBindingId() {
			found = true
			if entry.ObservationResult != "rejected" || !entry.RevokedAt.IsZero() {
				t.Fatalf("listed binding=%+v", entry)
			}
		}
	}
	if !found {
		t.Fatalf("the unconfirmed revocation was recorded but not listed: %+v", pending)
	}
}

// TestRevokeManagedKeyStillRefusesAfterRecording guards the discipline that
// matters most here. Making the partial state observable must not turn an
// absent capability into a success: the recorded refusal is a trace, never a
// receipt. This fails loudly if anyone later rewrites the Unimplemented return as
// a nil error to make the recorded state look tidy.
func TestRevokeManagedKeyStillRefusesAfterRecording(t *testing.T) {
	issuer, _ := revocationAssembly(t)
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	issued, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	operation, err := workspace.RevokeManagedKey(t.Context(), &api.ManagedKeyRevoke{
		Context:      grantedCaller("revoke", "grant-workspace"),
		KeyBindingId: issued.GetKeyBindingId(),
		WorkspaceId:  "ws-1",
	})
	if err == nil {
		t.Fatalf("a revoke with no approved route reported success: %v", operation)
	}
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("revoke error=%v want Unimplemented", err)
	}
	if operation != nil {
		t.Fatalf("a refused revoke returned a successful Operation: %v", operation)
	}
	binding, readErr := service.GatewayStore.ReadManagedKeyBinding(t.Context(), issued.GetKeyBindingId())
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !binding.RevokedAt.IsZero() {
		t.Fatalf("the key was recorded revoked without an approved revocation route: %s", binding.RevokedAt)
	}
	// The public read model must not claim a terminal revoked status either.
	if status := binding.GatewayKeyContract().GetStatus(); status == api.GatewayKeyStatusEnum_GATEWAY_KEY_STATUS_ENUM_REVOKED {
		t.Fatalf("the read model reports the key revoked although nothing revoked it: %v", status)
	}
}

// TestMarkManagedKeyRevokedSupersedesAnEarlierUnconfirmedRevocation proves the
// recorded partial state is not terminal. Once an approved revocation route
// exists and confirms, the binding reaches the confirmed terminal state exactly
// as a first-time confirmation would.
func TestMarkManagedKeyRevokedSupersedesAnEarlierUnconfirmedRevocation(t *testing.T) {
	ctx := t.Context()
	service, workspace := managedKeyCoordination(t, &managedKeyIssuerStub{}, &managedKeySecretStore{})
	issued, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	if _, err = service.GatewayStore.MarkManagedKeyRevocationUnconfirmed(ctx, issued.GetKeyBindingId(), "rejected"); err != nil {
		t.Fatal(err)
	}
	revoked, err := service.GatewayStore.MarkManagedKeyRevoked(ctx, issued.GetKeyBindingId())
	if err != nil {
		t.Fatal(err)
	}
	if revoked.RevokedAt.IsZero() || revoked.ObservationResult != "confirmed" {
		t.Fatalf("an earlier unconfirmed revocation blocked a later confirmed one: %+v", revoked)
	}
	// A confirmed revocation must not be re-opened by a later unconfirmed record.
	if _, err = service.GatewayStore.MarkManagedKeyRevocationUnconfirmed(ctx, issued.GetKeyBindingId(), "unknown"); err != nil {
		t.Fatal(err)
	}
	after, err := service.GatewayStore.ReadManagedKeyBinding(ctx, issued.GetKeyBindingId())
	if err != nil {
		t.Fatal(err)
	}
	if after.ObservationResult != "confirmed" || after.RevokedAt.IsZero() {
		t.Fatalf("a confirmed revocation was downgraded by a later unconfirmed record: %+v", after)
	}
}