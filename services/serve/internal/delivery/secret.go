package delivery

import (
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
