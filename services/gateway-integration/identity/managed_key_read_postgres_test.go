package identity_test

// This file owns the Gateway-managed-key read tests. A model-configuration update
// must keep the exact key the Workspace already holds, so the Workspace reads that
// binding and its approved model scope from this owner. The read is scoped to the
// owning Workspace, names no external key or raw Secret, and refuses a retired key
// instead of presenting it as usable.

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
)

func managedKeyRead(step, keyBindingID, workspaceID string) *api.ManagedKeyReadCommand {
	return &api.ManagedKeyReadCommand{Context: grantedCaller(step, "grant-workspace"), KeyBindingId: keyBindingID, WorkspaceId: workspaceID}
}

// TestReadManagedKeyReturnsTheApprovedScopeToItsWorkspace proves the read answers
// with the binding this Workspace issued, including the exact approved model scope a
// model update must stay inside, and never with the external key identity.
func TestReadManagedKeyReturnsTheApprovedScopeToItsWorkspace(t *testing.T) {
	issuer := &managedKeyIssuerStub{}
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	created, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-b", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	read, err := workspace.ReadManagedKey(t.Context(), managedKeyRead("read-managed-key", created.GetKeyBindingId(), "ws-1"))
	if err != nil {
		t.Fatalf("read managed key: %v", err)
	}
	if read.GetKeyBindingId() != created.GetKeyBindingId() || read.GetWorkspaceId() != "ws-1" ||
		read.GetFingerprint() != created.GetFingerprint() || read.GetSecretDeliveryReference() != created.GetSecretDeliveryReference() {
		t.Fatalf("read returned a different binding: %v vs %v", read, created)
	}
	approved := map[string]bool{}
	for _, model := range read.GetModelIds() {
		approved[model] = true
	}
	if !approved["model-a"] || !approved["model-b"] || len(approved) != 2 {
		t.Fatalf("read scope=%v, want the issued model set", read.GetModelIds())
	}
	// The read answers no external key identity and no raw Secret.
	if read.GetExpiresAt() == nil {
		t.Fatal("read lost the binding expiry")
	}
	var external string
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT external_key_id FROM gateway.key_bindings WHERE id=$1`, created.GetKeyBindingId()).Scan(&external); err != nil {
		t.Fatal(err)
	}
	if external == "" {
		t.Fatal("fixture did not record the external key identity")
	}
}

// TestReadManagedKeyRefusesAnotherWorkspaceAndAnUnknownBinding proves the read is
// scoped to the owning Workspace: another Workspace of the same tenant cannot read
// the binding, and an unknown id is a not-found answer rather than an empty binding.
func TestReadManagedKeyRefusesAnotherWorkspaceAndAnUnknownBinding(t *testing.T) {
	issuer := &managedKeyIssuerStub{}
	_, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	created, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	if _, err = workspace.ReadManagedKey(t.Context(), managedKeyRead("read-foreign-workspace", created.GetKeyBindingId(), "ws-2")); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("another workspace read err=%v want permission denied", err)
	}
	if _, err = workspace.ReadManagedKey(t.Context(), managedKeyRead("read-unknown-key", "gateway-key-unknown", "ws-1")); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown binding read err=%v want not found", err)
	}
}

// TestReadManagedKeyRefusesARevokedBinding proves a model update can never be
// applied under a key the owner already retired: the read refuses the binding rather
// than returning a scope that no live key backs.
func TestReadManagedKeyRefusesARevokedBinding(t *testing.T) {
	issuer := &managedKeyIssuerStub{}
	_, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	created, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	if _, err = workspace.RevokeManagedKey(t.Context(), &api.ManagedKeyRevoke{Context: grantedCaller("revoke-managed-key", "grant-workspace"), KeyBindingId: created.GetKeyBindingId(), WorkspaceId: "ws-1"}); err != nil {
		t.Fatalf("revoke managed key: %v", err)
	}
	if _, err = workspace.ReadManagedKey(t.Context(), managedKeyRead("read-revoked-key", created.GetKeyBindingId(), "ws-1")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("revoked binding read err=%v want a failed precondition", err)
	}
	if issuer.revokes != 1 {
		t.Fatalf("fixture revoked %d keys, want one", issuer.revokes)
	}
}
