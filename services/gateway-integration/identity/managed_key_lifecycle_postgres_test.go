package identity_test

// These tests drive the reversible managed-key lifecycle through the owner's real
// coordination surface and its real PostgreSQL state. The external authority is a
// scripted issuer, so the transition, the readback requirement and the local record
// are proven without inventing a provider contract.

import (
	"context"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/gateway-integration/identity"
)

// lifecycleIssuer is the one external authority in these tests: it mints keys and
// keeps the state a real authority would report, while scripting refusals and lost
// answers. It records the exact subject, key and desired state it was asked for.
type lifecycleIssuer struct {
	*managedKeyIssuerStub
	mu               sync.Mutex
	states           map[string]identity.ManagedKeyExternalState
	setCalls         int
	readCalls        int
	lastSubject      string
	lastExternalKey  string
	lastDesiredState identity.ManagedKeyExternalState
	setErr           error
	readErr          error
	loseReadAfterSet bool
	mutated          bool
}

func newLifecycleIssuer() *lifecycleIssuer {
	return &lifecycleIssuer{managedKeyIssuerStub: &managedKeyIssuerStub{}, states: map[string]identity.ManagedKeyExternalState{}}
}

func (i *lifecycleIssuer) SetWorkspaceKeyState(_ context.Context, subject, externalKeyID string, state identity.ManagedKeyExternalState) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.setCalls++
	i.lastSubject, i.lastExternalKey, i.lastDesiredState = subject, externalKeyID, state
	if i.setErr != nil {
		return i.setErr
	}
	i.states[externalKeyID] = state
	i.mutated = true
	return nil
}

func (i *lifecycleIssuer) ReadWorkspaceKeyState(_ context.Context, subject, externalKeyID string) (identity.ManagedKeyExternalState, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.readCalls++
	i.lastSubject = subject
	if i.readErr != nil {
		return "", i.readErr
	}
	if i.loseReadAfterSet && i.mutated {
		return "", status.Error(codes.Unavailable, "the Gateway state readback was lost")
	}
	state, ok := i.states[externalKeyID]
	if !ok {
		return "", status.Error(codes.NotFound, "unknown external key")
	}
	return state, nil
}

func lifecycleCommand(step, bindingID string) *api.ManagedKeyLifecycleCommand {
	return &api.ManagedKeyLifecycleCommand{Context: grantedCaller(step, "grant-workspace"), KeyBindingId: bindingID, WorkspaceId: "ws-1"}
}

// TestManagedKeyLifecycleDisablesEnablesAndReadsBackTheExactKey proves the
// reversible state machine: one exact binding is disabled and enabled through the
// owner, the external subject is the bound wallet subject, and every confirmed
// transition advances the recorded lifecycle version while a replay does not
// repeat the external effect.
func TestManagedKeyLifecycleDisablesEnablesAndReadsBackTheExactKey(t *testing.T) {
	issuer := newLifecycleIssuer()
	externalKeyID := managedKeyExternalIDForTest("ws-1", []string{"model-a"})
	issuer.states[externalKeyID] = identity.ManagedKeyExternalActive
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	created, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	bindingID := created.GetKeyBindingId()

	disabled, err := workspace.DisableManagedKey(t.Context(), lifecycleCommand("disable-key", bindingID))
	if err != nil {
		t.Fatalf("disable managed key: %v", err)
	}
	if disabled.GetKeyBindingId() != bindingID || disabled.GetState() != api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED ||
		disabled.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || disabled.GetLifecycleVersion() != 1 {
		t.Fatalf("disable readback=%v", disabled)
	}
	issuer.mu.Lock()
	if issuer.setCalls != 1 || issuer.lastSubject != "77" || issuer.lastExternalKey != externalKeyID || issuer.lastDesiredState != identity.ManagedKeyExternalDisabled {
		t.Fatalf("external disable calls=%d subject=%q key=%q state=%q", issuer.setCalls, issuer.lastSubject, issuer.lastExternalKey, issuer.lastDesiredState)
	}
	issuer.mu.Unlock()

	// A replay of the same intent settles from the authority without a second
	// disable or a new lifecycle version.
	replay, err := workspace.DisableManagedKey(t.Context(), lifecycleCommand("disable-key", bindingID))
	if err != nil {
		t.Fatalf("replay disable: %v", err)
	}
	if replay.GetState() != api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED || replay.GetLifecycleVersion() != 1 {
		t.Fatalf("replayed disable readback=%v", replay)
	}
	issuer.mu.Lock()
	if issuer.setCalls != 1 {
		t.Fatalf("replayed disable caused %d external transitions", issuer.setCalls)
	}
	issuer.mu.Unlock()

	enabled, err := workspace.EnableManagedKey(t.Context(), lifecycleCommand("enable-key", bindingID))
	if err != nil {
		t.Fatalf("enable managed key: %v", err)
	}
	if enabled.GetState() != api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE || enabled.GetLifecycleVersion() != 2 ||
		enabled.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED {
		t.Fatalf("enable readback=%v", enabled)
	}
	read, err := workspace.ReadManagedKeyLifecycle(t.Context(), lifecycleCommand("read-key", bindingID))
	if err != nil {
		t.Fatalf("read managed key lifecycle: %v", err)
	}
	if read.GetState() != api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE || read.GetLifecycleVersion() != 2 {
		t.Fatalf("lifecycle readback=%v", read)
	}
	// The owner's own record agrees with the confirmed transitions.
	stored, err := service.GatewayStore.ReadManagedKeyBinding(t.Context(), bindingID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.LifecycleVersion != 2 || !stored.DisabledAt.IsZero() {
		t.Fatalf("stored lifecycle version=%d disabledAt=%v", stored.LifecycleVersion, stored.DisabledAt)
	}
}

// TestManagedKeyLifecycleRefusalAndLostReadbackNeverBecomeLocalState proves a
// refused disable and an unobservable readback leave the stored binding active.
// The key must stay visibly live when the authority did not confirm the change.
func TestManagedKeyLifecycleRefusalAndLostReadbackNeverBecomeLocalState(t *testing.T) {
	issuer := newLifecycleIssuer()
	externalKeyID := managedKeyExternalIDForTest("ws-1", []string{"model-a"})
	issuer.states[externalKeyID] = identity.ManagedKeyExternalActive
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	created, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	bindingID := created.GetKeyBindingId()

	issuer.mu.Lock()
	issuer.setErr = status.Error(codes.Unimplemented, "no approved Sub2API service key disable/enable endpoint is configured")
	issuer.mu.Unlock()
	if _, err = workspace.DisableManagedKey(t.Context(), lifecycleCommand("disable-refused", bindingID)); status.Code(err) != codes.Unimplemented {
		t.Fatalf("refused disable err=%v", err)
	}
	stored, err := service.GatewayStore.ReadManagedKeyBinding(t.Context(), bindingID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.DisabledAt.IsZero() || stored.LifecycleVersion != 0 {
		t.Fatalf("refused disable recorded disabledAt=%v version=%d", stored.DisabledAt, stored.LifecycleVersion)
	}

	// The authority applies the change but its confirming readback is lost. The
	// owner must not record a disabled key it could not read back.
	issuer.mu.Lock()
	issuer.setErr = nil
	issuer.loseReadAfterSet = true
	issuer.mu.Unlock()
	if _, err = workspace.DisableManagedKey(t.Context(), lifecycleCommand("disable-lost-readback", bindingID)); status.Code(err) != codes.Unavailable {
		t.Fatalf("unconfirmed disable err=%v", err)
	}
	stored, err = service.GatewayStore.ReadManagedKeyBinding(t.Context(), bindingID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.DisabledAt.IsZero() || stored.LifecycleVersion != 0 {
		t.Fatalf("unconfirmed disable recorded disabledAt=%v version=%d", stored.DisabledAt, stored.LifecycleVersion)
	}
}

// TestManagedKeyLifecycleCannotReviveARevokedKey proves revocation stays terminal:
// the reversible route refuses it instead of re-enabling a retired credential.
func TestManagedKeyLifecycleCannotReviveARevokedKey(t *testing.T) {
	issuer := newLifecycleIssuer()
	issuer.states[managedKeyExternalIDForTest("ws-1", []string{"model-a"})] = identity.ManagedKeyExternalActive
	service, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	created, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("managed-key", "model-a"))
	if err != nil {
		t.Fatalf("create managed key: %v", err)
	}
	if _, err = workspace.RevokeManagedKey(t.Context(), &api.ManagedKeyRevoke{Context: grantedCaller("revoke-key", "grant-workspace"), KeyBindingId: created.GetKeyBindingId(), WorkspaceId: "ws-1"}); err != nil {
		t.Fatalf("revoke managed key: %v", err)
	}
	if _, err = workspace.EnableManagedKey(t.Context(), lifecycleCommand("enable-revoked", created.GetKeyBindingId())); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("enable revoked key err=%v, want FailedPrecondition", err)
	}
	stored, err := service.GatewayStore.ReadManagedKeyBinding(t.Context(), created.GetKeyBindingId())
	if err != nil {
		t.Fatal(err)
	}
	if stored.RevokedAt.IsZero() || !stored.DisabledAt.IsZero() {
		t.Fatalf("revoked binding state revokedAt=%v disabledAt=%v", stored.RevokedAt, stored.DisabledAt)
	}
}
