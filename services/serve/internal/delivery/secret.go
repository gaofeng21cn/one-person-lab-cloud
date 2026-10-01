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

// resolveManagedKeyBinding confirms the Workspace-issued managed key binding a launch
// command carries. The launch key is issued and bound by the Workspace owner (F08
// workspace->gateway CreateManagedKey, then workspace->fabric BindSecret) and travels
// on the deploy command as an opaque RuntimeManagedKeyBinding. Serve never mints a
// Gateway key: a runtime that declares the installation Gateway credential must
// present the complete binding its owner produced, and one that declares no such
// credential must present none.
func (s *Service) resolveManagedKeyBinding(_ context.Context, r *api.RuntimeDeployCommand) error {
	revision, err := deploymentRevision(r)
	if err != nil {
		return err
	}
	credential, declared := contracts.WorkspaceApplicationDeclaredCredential(revision, contracts.WorkspaceApplicationCredentialGatewayKey)
	if !declared {
		if r.GetManagedKeyBinding() != nil {
			return status.Errorf(codes.FailedPrecondition, "%s: the runtime declares no Gateway credential, so no managed key binding applies", ReasonManagedKeyUnavailable)
		}
		return nil
	}
	binding := r.GetManagedKeyBinding()
	if binding == nil || strings.TrimSpace(binding.GetKeyBindingId()) == "" || strings.TrimSpace(binding.GetSecretBindingId()) == "" ||
		strings.TrimSpace(binding.GetSecretVersion()) == "" || strings.TrimSpace(binding.GetFingerprint()) == "" {
		return status.Errorf(codes.FailedPrecondition, "%s: a launch that declares a Gateway credential requires the Workspace-issued managed key binding", ReasonManagedKeyUnavailable)
	}
	if binding.GetTargetSlot() != credential.Name || binding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(r.GetWorkspaceId()) {
		return status.Errorf(codes.FailedPrecondition, "%s: the managed key binding does not name this Workspace's Gateway Secret delivery", ReasonManagedKeyUnavailable)
	}
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
