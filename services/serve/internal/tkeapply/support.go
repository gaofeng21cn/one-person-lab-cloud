package tkeapply

import (
	"errors"
	"fmt"

	contracts "opl-cloud/packages/contracts/go"
)

// tencentWorkspaceFSGroupPolicy is the volume-ownership policy a Workspace's
// prepaid claim is mounted with. A workspace volume is owned by the application's
// own group, so the claim is adjusted once on mount rather than on every restart.
const tencentWorkspaceFSGroupPolicy = "OnRootMismatch"

// ErrWorkspaceLaunchPending reports that the workload is applied but not yet
// available. It is the executor's honest progress signal: an object that exists is
// not an application that serves.
var ErrWorkspaceLaunchPending = errors.New("workspace_application_runtime_pending")

// applicationCredentialSourceContextKey and applicationCredentialSource name the
// provenance proof a legacy schema-0 deployment needed for its credential. Only the
// historical runtime owner ever recorded that proof, so this executor never holds
// one: a legacy input that asks for a source-proven credential fails closed
// instead of accepting an unproven version.
type applicationCredentialSourceContextKey struct{}
type applicationCredentialSource struct {
	RuntimeOperationID, Version, OperationKey string
}

// validateWorkspaceApplicationConfiguration refuses an input whose configuration,
// credential reference or data binding the shared contract says is incomplete.
func validateWorkspaceApplicationConfiguration(input WorkspaceApplicationRuntimeInput) error {
	return contracts.ValidateWorkspaceApplicationRuntimeConfiguration(input)
}

// workspaceApplicationRequiresDerivedCredentials reports whether the revision
// asks the installation to derive a credential for it. Deriving needs the
// installation seed and the credential version, so the executor resolves those
// only for an application that declared the requirement.
func workspaceApplicationRequiresDerivedCredentials(revision contracts.WorkspaceApplicationRevision) bool {
	return contracts.WorkspaceApplicationCredentialKind(revision, contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword) ||
		contracts.WorkspaceApplicationCredentialKind(revision, contracts.WorkspaceApplicationCredentialWorkspaceSessionSecret)
}

// workspaceApplicationGatewayBinding resolves the Secret binding named by the
// revision's declared Gateway credential. The credential's name is the
// application's choice; the platform only requires that the binding points at the
// Workspace's own Gateway secret.
func workspaceApplicationGatewayBinding(input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeSecretBinding, error) {
	credential, declared := contracts.WorkspaceApplicationDeclaredCredential(input.Revision, contracts.WorkspaceApplicationCredentialGatewayKey)
	if !declared {
		return contracts.WorkspaceApplicationRuntimeSecretBinding{}, errors.New("workspace_application_credentials_unavailable")
	}
	for _, binding := range input.SecretBindings {
		if binding.Name == credential.Name && binding.Key == contracts.WorkspaceApplicationGatewayKeyField && binding.SecretRef == gatewaySecretName(input.WorkspaceID) {
			return binding, nil
		}
	}
	return contracts.WorkspaceApplicationRuntimeSecretBinding{}, fmt.Errorf("workspace_application_gateway_binding_required")
}
