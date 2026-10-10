package identity_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
)

// TestManagedKeyCommandResolvesTheDeclaredDefaultScope proves the accepted
// default-App launch - which declares no model selection - issues through the
// authority's approved scope instead of being refused locally: the declared list
// travels empty, the authority's resolved concrete list is recorded with the
// binding and the original command, the recorded fingerprint is Fabric's exact
// format, and a replay returns the same key without a second issuance.
func TestManagedKeyCommandResolvesTheDeclaredDefaultScope(t *testing.T) {
	issuer := &managedKeyIssuerStub{}
	secrets := &managedKeySecretStore{}
	service, workspace := managedKeyCoordination(t, issuer, secrets)
	service.WorkspaceKeyGroup = "workspace-approved-group"
	command := &api.ManagedKeyCommand{Context: grantedCaller("managed-key-default", "grant-workspace"), WorkspaceId: "ws-1", TargetRuntimeInstanceId: "runtime-1"}
	first, err := workspace.CreateManagedKey(t.Context(), command)
	if err != nil {
		t.Fatalf("create default-scope managed key: %v", err)
	}
	if first.GetKeyBindingId() == "" || first.GetFingerprint() == "" || first.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef("ws-1") {
		t.Fatalf("incomplete managed key binding: %v", first)
	}
	request := issuer.lastRequest
	if request == nil || len(request.ModelIDs) != 0 || request.WorkspaceID != "ws-1" || request.Subject != "77" || request.GroupName != "workspace-approved-group" || request.IdempotencyKey != "opl-test:managed-key-default" || request.LaunchOperationID != "opl-test:managed-key-default" {
		t.Fatalf("declared issuance request=%+v", request)
	}
	if request.ExactName != workspaceKeyExactNameForTest("ws-1") {
		t.Fatalf("exact name=%q want the legacy reserved-name convention", request.ExactName)
	}
	// The resolved scope is durable on the binding and in the original command
	// answer; the binding readback carries the fingerprint Serve checks.
	var recordedModels string
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT model_ids::text FROM gateway.key_bindings WHERE workspace_id = 'ws-1'`).Scan(&recordedModels); err != nil {
		t.Fatal(err)
	}
	if recordedModels != "{default-model}" {
		t.Fatalf("binding model scope=%s want the resolved concrete list", recordedModels)
	}
	var answer string
	if err = service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT response_body::text FROM gateway.idempotency_records WHERE idempotency_key = 'opl-test:managed-key-default'`).Scan(&answer); err != nil {
		t.Fatal(err)
	}
	var recorded struct {
		ResolvedModelIDs []string `json:"resolvedModelIds"`
	}
	if json.Unmarshal([]byte(answer), &recorded) != nil || strings.Join(recorded.ResolvedModelIDs, ",") != "default-model" || strings.Contains(answer, "raw-managed-key") {
		t.Fatalf("recorded answer=%s", answer)
	}
	// The passed fingerprint is exactly Fabric's provider format.
	raw := "raw-managed-key-ws-1"
	digest := sha256.Sum256([]byte(raw))
	if want := "sha256:" + hex.EncodeToString(digest[:]); secrets.lastFingerprint != want || first.GetFingerprint() != want {
		t.Fatalf("fingerprint=%q readback=%q want %q", secrets.lastFingerprint, first.GetFingerprint(), want)
	}
	if secrets.lastExternalKeyID == "" || secrets.lastCallContextMissing || secrets.lastCall == nil ||
		secrets.lastCall.GetIdempotencyKey() != "opl-test:managed-key-default" || secrets.lastCall.GetAcceptedOperationGrantId() != "grant-workspace" {
		t.Fatalf("Secret write delivery context=%+v", secrets.lastCall)
	}
	// A replay of the same declared default scope returns the recorded binding.
	replay, err := workspace.CreateManagedKey(t.Context(), &api.ManagedKeyCommand{Context: grantedCaller("managed-key-default", "grant-workspace"), WorkspaceId: "ws-1", TargetRuntimeInstanceId: "runtime-1"})
	if err != nil {
		t.Fatalf("replay default-scope managed key: %v", err)
	}
	if replay.GetKeyBindingId() != first.GetKeyBindingId() || issuer.issued() != 1 || secrets.writeCount() != 1 {
		t.Fatalf("replay re-issued: issuances=%d writes=%d", issuer.issued(), secrets.writeCount())
	}
	// The same idempotency key with a different declared scope is a different
	// command and stays refused instead of silently re-resolving.
	_, err = workspace.CreateManagedKey(t.Context(), &api.ManagedKeyCommand{Context: grantedCaller("managed-key-default", "grant-workspace"), WorkspaceId: "ws-1", ModelIds: []string{"model-a"}, TargetRuntimeInstanceId: "runtime-1"})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("different declared scope err=%v want conflict", err)
	}
}

// TestManagedKeyCommandRejectsAnUnrecordableResolvedScope proves a resolved scope
// that violates the bounded contract fails closed: nothing is recorded as an
// approved allowlist and the original command stays unresolved without a second
// issuance.
func TestManagedKeyCommandRejectsAnUnrecordableResolvedScope(t *testing.T) {
	overlong := make([]string, 0, 65)
	for index := 0; index < 65; index++ {
		overlong = append(overlong, fmt.Sprintf("model-%d", index))
	}
	issuer := &managedKeyIssuerStub{}
	secrets := &managedKeySecretStore{}
	service, workspace := managedKeyCoordination(t, issuer, secrets)
	for index, testCase := range []struct {
		name     string
		resolved []string
	}{
		{"wildcard", []string{"*"}},
		{"too many", overlong},
		{"invalid char", []string{"two words"}},
	} {
		issuer.resolvedModels = testCase.resolved
		step := fmt.Sprintf("managed-key-invalid-%d", index)
		command := &api.ManagedKeyCommand{Context: grantedCaller(step, "grant-workspace"), WorkspaceId: "ws-1", TargetRuntimeInstanceId: "runtime-1"}
		if _, err := workspace.CreateManagedKey(t.Context(), command); status.Code(err) != codes.Unavailable {
			t.Fatalf("%s err=%v want unavailable", testCase.name, err)
		}
		var bindings int
		if err := service.GatewayStore.DB().QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.key_bindings WHERE workspace_id = 'ws-1'`).Scan(&bindings); err != nil {
			t.Fatal(err)
		}
		if bindings != 0 || secrets.writeCount() != 0 {
			t.Fatalf("%s recorded an unapproved scope: bindings=%d writes=%d", testCase.name, bindings, secrets.writeCount())
		}
		// The unresolved command is never re-dispatched.
		issued := issuer.issued()
		if _, err := workspace.CreateManagedKey(t.Context(), command); status.Code(err) != codes.Unavailable || issuer.issued() != issued {
			t.Fatalf("%s re-issued: issuances=%d err=%v", testCase.name, issuer.issued(), err)
		}
	}
}

// workspaceKeyExactNameForTest derives the legacy reserved-name convention
// independently of the implementation: "opl-workspace-" plus the first 12 hex of
// the Workspace id digest.
func workspaceKeyExactNameForTest(workspaceID string) string {
	digest := sha256.Sum256([]byte(workspaceID))
	return "opl-workspace-" + hex.EncodeToString(digest[:])[:12]
}
