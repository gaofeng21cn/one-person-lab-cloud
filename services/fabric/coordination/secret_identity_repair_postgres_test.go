package coordination_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// TestBindSecretRefusesChangedFingerprintButReplaysExactOriginal pins the exact
// input identity of an initial Secret bind: the same runtime, slot, Gateway key
// binding and Secret replay the confirmed readback, the optional fingerprint
// omission replays the owner-authoritative readback, and a supplied fingerprint
// that differs from the confirmed binding is refused as a different Secret
// identity without touching the provider or the active binding.
func TestBindSecretRefusesChangedFingerprintButReplaysExactOriginal(t *testing.T) {
	db := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	surface := startFabricSurface(t, db)
	dispatcher := &rebindDispatcher{}
	surface.service.Dispatcher = dispatcher
	t.Cleanup(func() { surface.service.Dispatcher = nil })

	const workspace, tenant, setID = "workspace-bind-fingerprint", "tenant", "rset_bind_fingerprint"
	seedConfirmedExecutionResource(t, ctx, db, setID, workspace, tenant)
	serve := surface.client(t, owneridentity.Serve.Service())
	const original = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const changed = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	call := func(key string) *api.CallContext {
		return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, IdempotencyKey: key, SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
	}
	bind := func(key, fingerprint string) (*api.SecretBindingReadback, error) {
		return serve.BindSecret(ctx, &api.SecretBindingCommand{Context: call(key), WorkspaceId: workspace, RuntimeInstanceId: "rt-bind", KeyBindingId: "key-original", SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: fingerprint})
	}

	first, err := bind("bind-first", original)
	if err != nil || first.GetFingerprint() != original || dispatcher.calls != 1 {
		t.Fatalf("initial bind=%+v err=%v calls=%d", first, err, dispatcher.calls)
	}
	// Exact original retry: same readback, no second provider confirmation.
	replayed, err := bind("bind-exact", original)
	if err != nil || !proto.Equal(first, replayed) || dispatcher.calls != 1 {
		t.Fatalf("exact replay=%+v err=%v calls=%d", replayed, err, dispatcher.calls)
	}
	// Omitted fingerprint: the optional field replays the owner-authoritative
	// confirmed readback instead of asserting a new identity.
	omitted, err := bind("bind-omitted", "")
	if err != nil || !proto.Equal(first, omitted) || dispatcher.calls != 1 {
		t.Fatalf("omitted fingerprint replay=%+v err=%v calls=%d", omitted, err, dispatcher.calls)
	}
	// A supplied fingerprint that differs from the confirmed binding is a
	// different Secret identity and must be refused before any provider call.
	if _, err = bind("bind-changed", changed); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("changed fingerprint err=%v want failed precondition", err)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("changed fingerprint reached the provider: calls=%d", dispatcher.calls)
	}
	count, active := activeSecretBindingCount(t, ctx, db, setID, "gateway")
	if count != 1 || active != first.GetSecretBindingId() {
		t.Fatalf("changed fingerprint mutated the active binding: active=%q count=%d want %q", active, count, first.GetSecretBindingId())
	}
	var storedFingerprint string
	if err = db.QueryRowContext(ctx, `SELECT fingerprint FROM fabric.secret_bindings WHERE id=$1`, first.GetSecretBindingId()).Scan(&storedFingerprint); err != nil {
		t.Fatal(err)
	}
	if storedFingerprint != original {
		t.Fatalf("stored fingerprint=%q want original", storedFingerprint)
	}
}

// TestRebindSecretRefusesDifferentGatewayBindingOnIdenticalContent pins the
// exact Gateway binding identity of a replacement: identical Secret content does
// not let a different Gateway key binding claim the predecessor as confirmed.
// The refusal happens before any provider confirmation, the one active binding
// is unchanged, and the genuine replacement path with the real new binding still
// executes with provider confirmation and predecessor retirement.
func TestRebindSecretRefusesDifferentGatewayBindingOnIdenticalContent(t *testing.T) {
	db := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	surface := startFabricSurface(t, db)
	dispatcher := &rebindDispatcher{}
	surface.service.Dispatcher = dispatcher
	t.Cleanup(func() { surface.service.Dispatcher = nil })

	const workspace, tenant, setID = "workspace-rebind-identity", "tenant", "rset_rebind_identity"
	seedConfirmedExecutionResource(t, ctx, db, setID, workspace, tenant)
	serve := surface.client(t, owneridentity.Serve.Service())
	const firstFingerprint = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const secondFingerprint = "sha256:1111111111111111111111111111111111111111111111111111111111111111"
	call := func(key string) *api.CallContext {
		return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, IdempotencyKey: key, SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
	}
	rebind := func(key, predecessorID, keyBindingID, fingerprint string) (*api.SecretBindingRebindReadback, error) {
		return serve.RebindSecret(ctx, &api.SecretBindingRebindCommand{Context: call(key), WorkspaceId: workspace, RuntimeInstanceId: "rt-rebind", ExpectedCurrentSecretBindingId: predecessorID, KeyBindingId: keyBindingID, SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: fingerprint})
	}

	first, err := serve.BindSecret(ctx, &api.SecretBindingCommand{Context: call("rebind-bind"), WorkspaceId: workspace, RuntimeInstanceId: "rt-rebind", KeyBindingId: "key-original", SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: firstFingerprint})
	if err != nil || first.GetSecretBindingId() == "" || dispatcher.calls != 1 {
		t.Fatalf("initial bind=%+v err=%v calls=%d", first, err, dispatcher.calls)
	}
	predecessor := first.GetSecretBindingId()
	// key-other with identical ref/fingerprint: the predecessor may not relabel
	// another Gateway binding identity as confirmed.
	if _, err = rebind("rebind-other-identity", predecessor, "key-other", firstFingerprint); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("foreign Gateway binding err=%v want failed precondition", err)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("foreign Gateway binding reached the provider: calls=%d", dispatcher.calls)
	}
	count, active := activeSecretBindingCount(t, ctx, db, setID, "gateway")
	if count != 1 || active != predecessor {
		t.Fatalf("foreign Gateway binding mutated state: active=%q count=%d want %q", active, count, predecessor)
	}
	// Exact self-replacement (the original Gateway binding, same content) remains
	// the legitimate idempotent replay.
	self, err := rebind("rebind-self", predecessor, "key-original", firstFingerprint)
	if err != nil || self.GetSecretBindingId() != predecessor || self.GetPreviousSecretBindingId() != predecessor || dispatcher.calls != 1 {
		t.Fatalf("exact self replacement=%+v err=%v calls=%d", self, err, dispatcher.calls)
	}
	// The genuine replacement named by its real new Gateway binding and a new
	// Secret identity still executes with provider confirmation.
	replacement, err := rebind("rebind-real", predecessor, "key-other", secondFingerprint)
	if err != nil || replacement.GetSecretBindingId() == predecessor || replacement.GetPreviousSecretBindingId() != predecessor || dispatcher.calls != 2 {
		t.Fatalf("genuine replacement=%+v err=%v calls=%d", replacement, err, dispatcher.calls)
	}
	count, active = activeSecretBindingCount(t, ctx, db, setID, "gateway")
	if count != 1 || active != replacement.GetSecretBindingId() {
		t.Fatalf("active=%q count=%d want replacement %q", active, count, replacement.GetSecretBindingId())
	}
	// The identical replacement command replays its stored readback without a
	// second retirement or a second provider confirmation.
	replayed, err := rebind("rebind-real", predecessor, "key-other", secondFingerprint)
	if err != nil || !proto.Equal(replacement, replayed) || dispatcher.calls != 2 {
		t.Fatalf("replacement replay=%+v err=%v calls=%d", replayed, err, dispatcher.calls)
	}
	// The successor answers the exact new Gateway binding as itself.
	successor, err := rebind("rebind-real-successor", replacement.GetSecretBindingId(), "key-other", secondFingerprint)
	if err != nil || successor.GetSecretBindingId() != replacement.GetSecretBindingId() || successor.GetPreviousSecretBindingId() != replacement.GetSecretBindingId() || dispatcher.calls != 2 {
		t.Fatalf("successor self replay=%+v err=%v calls=%d", successor, err, dispatcher.calls)
	}
	// A stale Gateway binding identity on the new active binding is refused.
	if _, err = rebind("rebind-stale-identity", replacement.GetSecretBindingId(), "key-original", secondFingerprint); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale Gateway binding err=%v want failed precondition", err)
	}
	count, active = activeSecretBindingCount(t, ctx, db, setID, "gateway")
	if count != 1 || active != replacement.GetSecretBindingId() {
		t.Fatalf("refused stale identity mutated state: active=%q count=%d", active, count)
	}
}
