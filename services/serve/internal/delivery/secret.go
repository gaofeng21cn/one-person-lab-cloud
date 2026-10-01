package delivery

import (
	"context"
	"encoding/json"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

// ReasonManagedKeyUnavailable reports that a default App declares the platform
// Gateway credential but Serve cannot obtain a confirmed managed key + Fabric
// Secret binding for it. Serve refuses rather than starting an Agent that can
// never reach its model.
const ReasonManagedKeyUnavailable = "managed_key_unavailable"

// deploymentRevision decodes the immutable application revision the deployment
// descriptor carries, rejecting an unreadable or invalid one.
func deploymentRevision(r *api.RuntimeDeployCommand) (contracts.WorkspaceApplicationRevision, error) {
	var zero contracts.WorkspaceApplicationRevision
	if r == nil || r.GetDeploymentDescriptor() == nil {
		return zero, status.Error(codes.InvalidArgument, "deployment descriptor is required")
	}
	raw, err := publicjson.Marshal(r.GetDeploymentDescriptor().GetApplicationRevision())
	if err != nil {
		return zero, err
	}
	var revision contracts.WorkspaceApplicationRevision
	if json.Unmarshal(raw, &revision) != nil || contracts.ValidateWorkspaceApplicationRevision(revision) != nil {
		return zero, status.Error(codes.InvalidArgument, "invalid application revision")
	}
	return revision, nil
}

// resolveManagedKeyBinding mints (or replays) the Workspace-managed Gateway key
// for this exact runtime and binds its approved-store Secret through Fabric, then
// records the result on the deploy command. It mutates only the caller's copy of
// the command; the frozen snapshot the caller persists afterwards carries the
// binding so a recovery replay issues the same identities.
//
// The raw key never enters this process: Gateway writes it to the approved Secret
// store and returns an opaque delivery reference; Fabric confirms that reference
// and returns the exact version Serve's execution boundary must inject.
func (s *Service) resolveManagedKeyBinding(ctx context.Context, r *api.RuntimeDeployCommand) error {
	revision, err := deploymentRevision(r)
	if err != nil {
		return err
	}
	credential, declared := contracts.WorkspaceApplicationDeclaredCredential(revision, contracts.WorkspaceApplicationCredentialGatewayKey)
	if !declared {
		return nil
	}
	if r.GetManagedKeyBinding() != nil && strings.TrimSpace(r.GetManagedKeyBinding().GetSecretBindingId()) != "" {
		// A recovered command already carries its original binding.
		return nil
	}
	if s.Gateway == nil {
		return status.Error(codes.Unavailable, "the Gateway managed-key authority is not configured")
	}
	if s.Resources == nil {
		return status.Error(codes.Unavailable, "Fabric Secret binding is not configured")
	}
	modelIDs := make([]string, 0, len(r.GetModelSelections()))
	for _, selection := range r.GetModelSelections() {
		if id := strings.TrimSpace(selection.GetModelId()); id != "" {
			modelIDs = append(modelIDs, id)
		}
	}
	if len(modelIDs) == 0 {
		return status.Error(codes.FailedPrecondition, "the default App declares a Gateway credential but names no model")
	}
	mint := &api.ManagedKeyCommand{Context: nextOwnerCall(r.GetContext()), WorkspaceId: r.GetWorkspaceId(), ModelIds: modelIDs, TargetRuntimeInstanceId: r.GetRuntimeInstanceId()}
	key, err := s.Gateway.CreateManagedKey(ctx, mint)
	if err != nil {
		return err
	}
	if strings.TrimSpace(key.GetKeyBindingId()) == "" || strings.TrimSpace(key.GetSecretDeliveryReference()) == "" || strings.TrimSpace(key.GetFingerprint()) == "" || key.GetWorkspaceId() != r.GetWorkspaceId() || key.GetTargetRuntimeInstanceId() != r.GetRuntimeInstanceId() {
		return status.Error(codes.FailedPrecondition, "the Gateway returned a managed key that differs from the original runtime")
	}
	bind := &api.SecretBindingCommand{Context: nextOwnerCall(r.GetContext()), WorkspaceId: r.GetWorkspaceId(), RuntimeInstanceId: r.GetRuntimeInstanceId(), KeyBindingId: key.GetKeyBindingId(), SecretDeliveryReference: key.GetSecretDeliveryReference(), TargetSlot: credential.Name, Fingerprint: key.GetFingerprint()}
	bound, err := s.Resources.BindSecret(ctx, bind)
	if err != nil {
		return err
	}
	if bound.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || strings.TrimSpace(bound.GetSecretBindingId()) == "" || strings.TrimSpace(bound.GetVersion()) == "" || bound.GetFingerprint() != key.GetFingerprint() || bound.GetRuntimeInstanceId() != r.GetRuntimeInstanceId() {
		return status.Error(codes.FailedPrecondition, "Fabric did not confirm the exact Gateway Secret binding")
	}
	r.ManagedKeyBinding = &api.RuntimeManagedKeyBinding{KeyBindingId: key.GetKeyBindingId(), SecretDeliveryReference: key.GetSecretDeliveryReference(), Fingerprint: key.GetFingerprint(), TargetSlot: credential.Name, SecretBindingId: bound.GetSecretBindingId(), SecretVersion: bound.GetVersion()}
	return nil
}

// confirmedReloadBinding validates the opaque Gateway/Fabric managed-key binding one
// model-configuration reload carries. The binding only exists for a runtime whose
// frozen revision declares the installation Gateway credential, because that revision
// is the one Serve injects a credential into; such a reload must present the exact
// confirmed generation its owners produced, and Serve never mints or restates a
// Gateway key. A revision that declares no such credential receives no credential, so
// a binding presented for it is not trusted either.
func confirmedReloadBinding(r *api.RuntimeDeployCommand, binding *api.RuntimeManagedKeyBinding) (*api.RuntimeManagedKeyBinding, error) {
	revision, err := deploymentRevision(r)
	if err != nil {
		return nil, err
	}
	credential, declared := contracts.WorkspaceApplicationDeclaredCredential(revision, contracts.WorkspaceApplicationCredentialGatewayKey)
	if !declared {
		if binding != nil {
			return nil, status.Errorf(codes.FailedPrecondition, "%s: the runtime declares no Gateway credential, so no managed key binding applies", ReasonManagedKeyUnavailable)
		}
		return nil, nil
	}
	if binding == nil ||
		strings.TrimSpace(binding.GetKeyBindingId()) == "" ||
		strings.TrimSpace(binding.GetSecretBindingId()) == "" ||
		strings.TrimSpace(binding.GetSecretVersion()) == "" ||
		strings.TrimSpace(binding.GetFingerprint()) == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: a model configuration reload requires the confirmed Gateway key binding", ReasonManagedKeyUnavailable)
	}
	// The binding must name the exact delivery Serve injects: the installation's
	// Workspace Gateway Secret for this Workspace, delivered into the slot this
	// revision declares. A binding for another delivery or slot is a different fact.
	if binding.GetTargetSlot() != credential.Name || binding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(r.GetWorkspaceId()) {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: the managed key binding does not name this Workspace's Gateway Secret delivery", ReasonManagedKeyUnavailable)
	}
	return binding, nil
}

// managedKeyRuntimeBindings builds the runtime Secret bindings the execution
// boundary injects from the confirmed managed-key binding. The Secret reference is
// the installation's deterministic Workspace Gateway Secret; the version is
// Fabric's own confirmed version, so the injected binding names exactly the store
// content the Gateway wrote.
func managedKeyRuntimeBindings(r *api.RuntimeDeployCommand) ([]contracts.WorkspaceApplicationRuntimeSecretBinding, error) {
	revision, err := deploymentRevision(r)
	if err != nil {
		return nil, err
	}
	credential, declared := contracts.WorkspaceApplicationDeclaredCredential(revision, contracts.WorkspaceApplicationCredentialGatewayKey)
	if !declared {
		return nil, nil
	}
	binding := r.GetManagedKeyBinding()
	if binding == nil || strings.TrimSpace(binding.GetSecretBindingId()) == "" || strings.TrimSpace(binding.GetSecretVersion()) == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: the default App declares the Gateway credential but no confirmed Secret binding was resolved", ReasonManagedKeyUnavailable)
	}
	return []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: credential.Name, SecretRef: contracts.WorkspaceGatewaySecretRef(r.GetWorkspaceId()), Version: binding.GetSecretVersion(), Key: contracts.WorkspaceApplicationGatewayKeyField}}, nil
}
