package fabric

import (
	"context"
	"errors"
	"strings"
)

// workspaceRuntimeReadEngine owns the non-secret Runtime status read model.
// Runtime mutations and Gateway Secret reads remain separate capabilities
// because they have different write authority and failure models.
type workspaceRuntimeReadEngine struct {
	provider   runtimeResourceReader
	operations WorkspaceRuntimeReadStore
}

func newWorkspaceRuntimeReadEngine(provider runtimeResourceReader, operations WorkspaceRuntimeReadStore) *workspaceRuntimeReadEngine {
	return &workspaceRuntimeReadEngine{provider: provider, operations: operations}
}

func (e *workspaceRuntimeReadEngine) providerStatus(ctx context.Context, workspaceID string) (WorkspaceRuntime, error) {
	return e.provider.WorkspaceRuntimeStatus(ctx, workspaceID)
}

// readStatusWithOwner is used only by callers that already hold an
// authorization boundary, such as credential reveal. Public status uses
// Status, which redacts the provider password.
func (e *workspaceRuntimeReadEngine) readStatusWithOwner(ctx context.Context, workspaceID string) (WorkspaceRuntime, FabricOperation, error) {
	runtime, err := e.providerStatus(ctx, workspaceID)
	if err != nil {
		return runtime, FabricOperation{}, err
	}
	if runtime.Status != "running" && runtime.Status != "unready" {
		return runtime, FabricOperation{}, nil
	}
	matches, err := e.operations.WorkspaceRuntimeIdentityCandidates(ctx, workspaceID)
	if err != nil {
		return runtime, FabricOperation{}, err
	}
	var created WorkspaceRuntime
	if runtime.WorkspaceID != workspaceID || len(matches) != 1 || matches[0].ID == "" || matches[0].CreatedAt.IsZero() || !decodeOperationResource(matches[0], &created) ||
		created.WorkspaceID != workspaceID || strings.TrimSpace(created.ID) == "" || strings.TrimSpace(created.OperationID) == "" ||
		runtime.ID != "" && runtime.ID != created.ID || runtime.OperationID != "" && runtime.OperationID != created.OperationID ||
		!legacyLocalDockerRuntimeReadbackMatches(matches[0], runtime, created) {
		return runtime, FabricOperation{}, ErrLaunchStageBindingConflict
	}
	runtime.ID, runtime.OperationID = created.ID, created.OperationID
	return runtime, matches[0], nil
}

func legacyLocalDockerRuntimeReadbackMatches(operation FabricOperation, live, created WorkspaceRuntime) bool {
	if operation.Provider != "local-docker" || operation.Status != "failed" || operation.ErrorCode != "local_docker_runtime_readback_mismatch" {
		return true
	}
	return live.ServiceName == created.ServiceName && live.ImageID == created.ImageID &&
		live.Access.Username == created.Access.Username && live.Access.CredentialStatus == created.Access.CredentialStatus &&
		live.Access.CredentialVersion == created.Access.CredentialVersion && live.Access.SecretRef == created.Access.SecretRef
}

func (e *workspaceRuntimeReadEngine) Status(ctx context.Context, workspaceID string) (WorkspaceRuntime, error) {
	runtime, _, err := e.readStatusWithOwner(ctx, workspaceID)
	runtime.Access.Password = ""
	return runtime, err
}

func (e *workspaceRuntimeReadEngine) Observe(ctx context.Context, workspaceID string) WorkspaceRuntimeObservation {
	runtime, err := e.Status(ctx, workspaceID)
	return workspaceRuntimeOwnerObservation(workspaceID, runtime, err)
}

func workspaceRuntimeOwnerObservation(workspaceID string, runtime WorkspaceRuntime, err error) WorkspaceRuntimeObservation {
	observation := WorkspaceRuntimeObservation{SchemaVersion: WorkspaceOwnerObservationSchemaVersion, State: WorkspaceOwnerObservationError, WorkspaceID: workspaceID}
	runtime.Access.Password = ""
	switch {
	case strings.TrimSpace(workspaceID) == "":
		return observation
	case errors.Is(err, ErrWorkspaceLaunchResourceAbsent):
		observation.State = WorkspaceOwnerObservationAbsent
		return observation
	case errors.Is(err, ErrLaunchStageBindingConflict):
		observation.State = WorkspaceOwnerObservationConflict
		return observation
	case err != nil:
		return observation
	case runtime.WorkspaceID != workspaceID || strings.TrimSpace(runtime.ID) == "":
		observation.State = WorkspaceOwnerObservationConflict
		return observation
	}
	switch runtime.Status {
	case "running":
		if runtime.Ready {
			observation.State = WorkspaceOwnerObservationReady
		} else {
			observation.State = WorkspaceOwnerObservationPending
		}
	case "unready", "pending", "provisioning", "creating", "destroying":
		if runtime.Ready {
			return observation
		}
		observation.State = WorkspaceOwnerObservationPending
	default:
		return observation
	}
	observation.Runtime = &runtime
	return observation
}
