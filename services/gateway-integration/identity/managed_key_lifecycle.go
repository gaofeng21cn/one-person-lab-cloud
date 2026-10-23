package identity

// This file owns the reversible lifecycle of one Workspace-managed Gateway key:
// disable, re-enable and the authoritative state readback. Gateway Integration is
// the only Cloud owner that may coordinate this transition with the external key
// authority; Workspace names the exact opaque binding it stored and decides when a
// lifecycle event happens, but never writes the key's state.
//
// The local `disabled_at` column is written only from an external readback that
// confirmed the exact key in the exact desired state. A refused or unobservable
// provider answer therefore never becomes a local "disabled" claim, and a retry
// starts from a fresh external read instead of repeating a mutation.

import (
	"context"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// DisableManagedKey reversibly disables the exact bound key. It is distinct from
// RevokeManagedKey: the key remains owned by the same binding and can be enabled
// again, while a revoked key is terminal.
func (s *Service) DisableManagedKey(ctx context.Context, r *api.ManagedKeyLifecycleCommand) (*api.ManagedKeyLifecycleReadback, error) {
	if r.GetOriginalCreateIdempotencyKey() != "" {
		return s.closeWorkspaceManagedKey(ctx, r)
	}
	return s.setManagedKeyLifecycle(ctx, r, api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED)
}

// EnableManagedKey restores the exact same bound key. It never issues or
// substitutes a key and never revives a revoked one.
func (s *Service) EnableManagedKey(ctx context.Context, r *api.ManagedKeyLifecycleCommand) (*api.ManagedKeyLifecycleReadback, error) {
	binding, _, err := s.managedKeyLifecycleBinding(ctx, r)
	if err != nil {
		return nil, err
	}
	guard, err := s.GatewayStore.guardWorkspaceKey(ctx, binding.TenantID, binding.WorkspaceID)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Workspace key guard unavailable")
	}
	defer guard.Rollback()
	closing, err := s.GatewayStore.workspaceKeyClosing(ctx, binding.TenantID, binding.WorkspaceID)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "Workspace closeout readback unavailable")
	}
	if closing {
		return nil, status.Error(codes.FailedPrecondition, "Workspace Key enablement is closed")
	}
	return s.setManagedKeyLifecycle(ctx, r, api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE)
}

// ReadManagedKeyLifecycle reports the state the external authority confirms for
// the exact bound key together with this owner's recorded lifecycle version. A
// local binding read alone is not reported as external state.
func (s *Service) ReadManagedKeyLifecycle(ctx context.Context, r *api.ManagedKeyLifecycleCommand) (*api.ManagedKeyLifecycleReadback, error) {
	key, subject, err := s.managedKeyLifecycleBinding(ctx, r)
	if err != nil {
		return nil, err
	}
	if s.KeyIssuer == nil {
		return nil, status.Error(codes.FailedPrecondition, "no approved Gateway key issuer is configured")
	}
	state, err := s.KeyIssuer.ReadWorkspaceKeyState(ctx, subject, key.ExternalKeyID)
	if err != nil {
		return nil, err
	}
	out, err := managedKeyLifecycleReadback(key, state)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// setManagedKeyLifecycle applies one reversible transition to the exact binding.
//
// The external key state is read before any mutation so a replay whose first
// response was lost settles from the authority instead of repeating the effect.
// Only a fresh readback that confirms the desired state commits the local column;
// a provider refusal, an unavailable provider or a mismatch leaves the recorded
// state untouched and reaches the caller unchanged.
func (s *Service) setManagedKeyLifecycle(ctx context.Context, r *api.ManagedKeyLifecycleCommand, desired api.ManagedKeyLifecycleState) (*api.ManagedKeyLifecycleReadback, error) {
	key, subject, err := s.managedKeyLifecycleBinding(ctx, r)
	if err != nil {
		return nil, err
	}
	if !key.RevokedAt.IsZero() {
		return nil, status.Error(codes.FailedPrecondition, "a revoked Gateway key has no reversible lifecycle state")
	}
	if s.KeyIssuer == nil {
		return nil, status.Error(codes.FailedPrecondition, "no approved Gateway key issuer is configured")
	}
	externalDesired, err := externalManagedKeyState(desired)
	if err != nil {
		return nil, err
	}
	observed, err := s.KeyIssuer.ReadWorkspaceKeyState(ctx, subject, key.ExternalKeyID)
	if err != nil {
		return nil, err
	}
	if observed == ManagedKeyExternalRevoked {
		return nil, status.Error(codes.FailedPrecondition, "the external Gateway key is revoked and cannot be re-enabled")
	}
	if observed != externalDesired {
		if err := s.KeyIssuer.SetWorkspaceKeyState(ctx, subject, key.ExternalKeyID, externalDesired); err != nil {
			return nil, err
		}
		observed, err = s.KeyIssuer.ReadWorkspaceKeyState(ctx, subject, key.ExternalKeyID)
		if err != nil {
			// The mutation may have taken effect; the answer is unknown rather than a
			// refusal, so no local state is written and the caller must recover.
			return nil, status.Error(codes.Unavailable, "the Gateway key lifecycle change was requested but its state could not be read back")
		}
		if observed != externalDesired {
			return nil, status.Error(codes.FailedPrecondition, "the external Gateway key did not confirm the requested lifecycle state")
		}
	}
	confirmedAt := time.Now().UTC()
	var updated ManagedKeyBinding
	if desired == api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED {
		updated, err = s.GatewayStore.MarkManagedKeyDisabled(ctx, key.ID, confirmedAt)
	} else {
		updated, err = s.GatewayStore.MarkManagedKeyEnabled(ctx, key.ID, confirmedAt)
	}
	if errors.Is(err, ErrManagedKeyLifecycleConflict) {
		return nil, status.Error(codes.FailedPrecondition, "the Gateway key binding cannot accept the requested lifecycle transition")
	}
	if err != nil {
		// The authority confirmed the effect but this owner could not record it, so
		// the binding's local lifecycle fact stays unconfirmed rather than claimed.
		return nil, status.Error(codes.Unavailable, "the Gateway key lifecycle change was confirmed but could not be recorded")
	}
	return managedKeyLifecycleReadback(updated, observed)
}

// managedKeyLifecycleBinding validates one lifecycle command and resolves the
// exact stored binding plus its bound external subject. Ownership is checked
// before any external effect: a binding from another tenant or Workspace is a
// permission refusal, never a target.
func (s *Service) managedKeyLifecycleBinding(ctx context.Context, r *api.ManagedKeyLifecycleCommand) (ManagedKeyBinding, string, error) {
	if err := s.coordinationCaller(ctx, owneridentity.Workspace.Service()); err != nil {
		return ManagedKeyBinding{}, "", err
	}
	if s.GatewayStore == nil {
		return ManagedKeyBinding{}, "", status.Error(codes.Unavailable, "Gateway key persistence is not configured")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return ManagedKeyBinding{}, "", err
	}
	if strings.TrimSpace(r.GetKeyBindingId()) == "" || strings.TrimSpace(r.GetWorkspaceId()) == "" {
		return ManagedKeyBinding{}, "", status.Error(codes.InvalidArgument, "key binding and workspace are required")
	}
	tenantID := r.GetContext().GetScope().GetTenant().GetTenantId()
	if tenantID == "" {
		return ManagedKeyBinding{}, "", status.Error(codes.PermissionDenied, "a tenant-scoped key operation is required")
	}
	if err := s.authorizeManagedKey(ctx, r.GetContext(), tenantID, r.GetWorkspaceId()); err != nil {
		return ManagedKeyBinding{}, "", err
	}
	key, err := s.GatewayStore.ReadManagedKeyBinding(ctx, r.GetKeyBindingId())
	if err != nil {
		return ManagedKeyBinding{}, "", status.Error(codes.NotFound, "Gateway key binding not found")
	}
	if key.TenantID != tenantID || key.WorkspaceID != r.GetWorkspaceId() {
		return ManagedKeyBinding{}, "", status.Error(codes.PermissionDenied, "key binding belongs to another tenant or workspace")
	}
	if key.Purpose != "workspace_managed" {
		return ManagedKeyBinding{}, "", status.Error(codes.FailedPrecondition, "only a Workspace-managed key has a reversible lifecycle")
	}
	binding, err := s.GatewayStore.ReadActiveWalletBinding(ctx, tenantID)
	if err != nil {
		return ManagedKeyBinding{}, "", status.Error(codes.FailedPrecondition, "the tenant has no active wallet binding")
	}
	if strings.TrimSpace(binding.BillingSub2APIUserID) == "" {
		return ManagedKeyBinding{}, "", status.Error(codes.FailedPrecondition, "the tenant wallet binding names no external subject")
	}
	return key, binding.BillingSub2APIUserID, nil
}

func externalManagedKeyState(state api.ManagedKeyLifecycleState) (ManagedKeyExternalState, error) {
	switch state {
	case api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE:
		return ManagedKeyExternalActive, nil
	case api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED:
		return ManagedKeyExternalDisabled, nil
	default:
		return "", status.Error(codes.InvalidArgument, "a reversible lifecycle state is required")
	}
}

func managedKeyLifecycleReadback(key ManagedKeyBinding, state ManagedKeyExternalState) (*api.ManagedKeyLifecycleReadback, error) {
	var mapped api.ManagedKeyLifecycleState
	switch state {
	case ManagedKeyExternalActive:
		mapped = api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE
	case ManagedKeyExternalDisabled:
		mapped = api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED
	case ManagedKeyExternalRevoked:
		mapped = api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_REVOKED
	default:
		return nil, status.Error(codes.Unavailable, "the external Gateway key state could not be interpreted")
	}
	updated := key.CreatedAt
	if !key.LifecycleUpdatedAt.IsZero() {
		updated = key.LifecycleUpdatedAt
	}
	return &api.ManagedKeyLifecycleReadback{
		KeyBindingId: key.ID, WorkspaceId: key.WorkspaceID, State: mapped,
		LifecycleVersion: key.LifecycleVersion, Outcome: api.Observation_OBSERVATION_CONFIRMED,
		UpdatedAt: stampOf(updated),
	}, nil
}
