package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

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

// runtimeStartSnapshot is Serve's owner-local execution record. The command
// remains the admitted cross-owner input; the credential version is frozen here
// because its lifecycle belongs to Serve, not to a Gateway Secret generation.
type runtimeStartSnapshot struct {
	Command           json.RawMessage `json:"command"`
	CredentialVersion string          `json:"credentialVersion,omitempty"`
}

func decodeRuntimeStartSnapshot(raw []byte) (*api.RuntimeDeployCommand, string, error) {
	var snapshot runtimeStartSnapshot
	if json.Unmarshal(raw, &snapshot) != nil {
		return nil, "", status.Error(codes.DataLoss, "stored runtime start snapshot is invalid")
	}
	legacy := len(snapshot.Command) == 0
	commandRaw := snapshot.Command
	if legacy {
		// Retained starts stored the command directly and used the confirmed
		// Gateway version for derivation. Preserve those exact credentials.
		commandRaw = raw
	}
	command := &api.RuntimeDeployCommand{}
	if protojson.Unmarshal(commandRaw, command) != nil {
		return nil, "", status.Error(codes.DataLoss, "stored runtime start command is invalid")
	}
	revision, err := deploymentRevision(command)
	if err != nil {
		return nil, "", status.Error(codes.DataLoss, "stored runtime application revision is invalid")
	}
	if legacy && contracts.WorkspaceApplicationCredentialKind(revision, contracts.WorkspaceApplicationCredentialGatewayKey) {
		if _, err := managedKeyRuntimeBindings(command); err != nil {
			return nil, "", err
		}
		snapshot.CredentialVersion = command.GetManagedKeyBinding().GetSecretVersion()
	}
	if !legacy && contracts.WorkspaceApplicationRequiresPlatformCredentials(revision) != (strings.TrimSpace(snapshot.CredentialVersion) != "") {
		return nil, "", status.Error(codes.DataLoss, "stored runtime credential version does not match its declaration")
	}
	return command, snapshot.CredentialVersion, nil
}

// initialRuntimeCredentialVersionTx resolves only Serve's selected lineage. A
// failed or unrelated candidate never supplies credentials. The first operation
// establishes an identity once; successors freeze the inherited identity.
func initialRuntimeCredentialVersionTx(ctx context.Context, tx *sql.Tx, command *api.RuntimeDeployCommand) (string, error) {
	revision, err := deploymentRevision(command)
	if err != nil || !contracts.WorkspaceApplicationRequiresPlatformCredentials(revision) {
		return "", err
	}
	var previous, operationID string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(previous_deployment_id,''),operation_id FROM serve.agent_deployments WHERE id=$1 AND workspace_id=$2`, command.GetDeploymentId(), command.GetWorkspaceId()).Scan(&previous, &operationID); err != nil {
		return "", dbError(err)
	}
	visited := map[string]bool{command.GetDeploymentId(): true}
	for previous != "" {
		if visited[previous] {
			return "", status.Error(codes.DataLoss, "runtime credential lineage contains a cycle")
		}
		visited[previous] = true
		deploymentID := previous
		var runtimeID string
		var activated bool
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(previous_deployment_id,''),COALESCE(runtime_instance_id,''),activated_at IS NOT NULL FROM serve.agent_deployments WHERE id=$1 AND workspace_id=$2`, deploymentID, command.GetWorkspaceId()).Scan(&previous, &runtimeID, &activated); err != nil {
			return "", dbError(err)
		}
		if !activated {
			return "", status.Error(codes.DataLoss, "runtime credential predecessor was never selected")
		}
		var raw []byte
		if err := tx.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE command_id=$1`, stableID("start_", deploymentID)).Scan(&raw); err != nil {
			return "", dbError(err)
		}
		prior, version, err := decodeRuntimeStartSnapshot(raw)
		if err != nil {
			return "", err
		}
		if prior.GetWorkspaceId() != command.GetWorkspaceId() || prior.GetDeploymentId() != deploymentID || prior.GetRuntimeInstanceId() != runtimeID {
			return "", status.Error(codes.DataLoss, "runtime credential predecessor identity is invalid")
		}
		priorRevision, err := deploymentRevision(prior)
		if err != nil {
			return "", err
		}
		if priorRevision.ApplicationID == revision.ApplicationID && version != "" {
			return version, nil
		}
		if contracts.WorkspaceApplicationRequiresPlatformCredentials(priorRevision) && version == "" {
			return "", status.Error(codes.DataLoss, "selected runtime has no recorded credential identity")
		}
	}
	return stableID("credential_", command.GetWorkspaceId(), revision.ApplicationID, operationID), nil
}

// runtimeCredentialVersion reads the same frozen identity after a restart, for
// apply, observation and lifecycle. A reload may change its Gateway binding but
// never silently replaces the platform password/session generation.
func runtimeCredentialVersion(ctx context.Context, q rowQueryer, command *api.RuntimeDeployCommand) (string, error) {
	revision, err := deploymentRevision(command)
	if err != nil || !contracts.WorkspaceApplicationRequiresPlatformCredentials(revision) {
		return "", err
	}
	var raw []byte
	err = q.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE command_id=$1 AND runtime_instance_id=$2 AND expected_deployment_id=$3`, stableID("start_", command.GetDeploymentId()), command.GetRuntimeInstanceId(), command.GetDeploymentId()).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", status.Error(codes.FailedPrecondition, "runtime credential identity is not frozen")
	}
	if err != nil {
		return "", dbError(err)
	}
	frozen, version, err := decodeRuntimeStartSnapshot(raw)
	if err != nil {
		return "", err
	}
	if frozen.GetWorkspaceId() != command.GetWorkspaceId() || frozen.GetRuntimeInstanceId() != command.GetRuntimeInstanceId() || frozen.GetDeploymentId() != command.GetDeploymentId() || frozen.GetDeploymentDescriptorDigest() != command.GetDeploymentDescriptorDigest() {
		return "", status.Error(codes.DataLoss, "runtime credential identity differs from its frozen deployment")
	}
	if strings.TrimSpace(version) == "" {
		return "", status.Error(codes.FailedPrecondition, "runtime credential identity is not frozen")
	}
	return version, nil
}
