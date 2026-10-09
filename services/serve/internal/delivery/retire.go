package delivery

// Retire is the Serve coordination entry point behind the Workspace deletion
// chain: the Workspace owner retires the deleted Workspace's application runtime
// before Fabric destroys the resources it runs on.
//
// The rules here are the owner boundary, not a policy layer:
//
//   - The declared runtime/deployment identity is a precondition. Serve resolves
//     the frozen command of exactly that runtime and refuses a substitute instead
//     of selecting another runtime for the Workspace.
//   - A retire is exactly-once per runtime/deployment. The recorded retire action
//     is the durable intent a replay resumes, and the reported operation is read
//     back from Serve's own store, so a lost response resolves by reading the
//     recorded result rather than issuing a second stop.
//   - Absence is reported only from Serve's own readback. A runtime Serve already
//     recorded terminated completes the same terminal operation against that owner
//     fact; an unknown outcome stays unknown.
//   - Only the application lifecycle state is applied. Retained data, its
//     attachment, disks, claims, secrets and every Fabric-owned fact stay with
//     their own owner: the command's retained_data_attachment_id is deliberately
//     neither read nor mutated, and this action never reaches Fabric's resource
//     mutation surface.

import (
	"context"
	"encoding/json"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// Retire stops the exact application runtime a deletion declared. It is idempotent
// per runtime/deployment: a replay reads back the recorded retire result, and the
// operation returned is always one Serve has committed.
func (s *Service) Retire(ctx context.Context, command *api.RuntimeStopCommand) (*api.Operation, error) {
	if err := requirePeer(ctx, owneridentity.Workspace); err != nil {
		return nil, err
	}
	runtimeID := strings.TrimSpace(command.GetRuntimeInstanceId())
	deploymentID := strings.TrimSpace(command.GetDeploymentId())
	if runtimeID == "" || deploymentID == "" {
		return nil, status.Error(codes.InvalidArgument, "runtime instance and deployment are required")
	}
	// The frozen original command proves the declared runtime exists and belongs to
	// the declared deployment; anything else is refused rather than resolved.
	deploy, err := s.persistedRuntimeCommand(ctx, runtimeID, deploymentID)
	if err != nil {
		return nil, err
	}
	// The Workspace owner's own call context is the admission input, and Serve's
	// persisted delivery decides whose Workspace this retirement may act on.
	if err = s.authorize(ctx, command.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME, deploy.command.GetWorkspaceId()); err != nil {
		return nil, err
	}
	return s.retireRuntime(ctx, deploy)
}

// retireRuntime applies the one recorded retirement of the exact persisted runtime.
// The action identity names the runtime and the deployment operation it belongs to,
// so a replay of the same deletion resumes its own action while a replay that
// changes the runtime, the deployment or the desired state is refused. The stop is
// recorded under the schema's own stop vocabulary and the desired lifecycle state
// it applies; the retained data attachment is not part of that identity because
// this action never acts on it: the caller keeps its lifecycle.
func (s *Service) retireRuntime(ctx context.Context, deploy *persistedRuntime) (*api.Operation, error) {
	runtimeID := deploy.command.GetRuntimeInstanceId()
	deploymentID := deploy.command.GetDeploymentId()
	snapshot, err := json.Marshal(map[string]string{"action": "stop", "desired": "suspended", "runtimeInstanceId": runtimeID, "deploymentId": deploymentID})
	if err != nil {
		return nil, status.Error(codes.Internal, "cannot record the runtime retirement")
	}
	id := stableID("act_", "retire", runtimeID, deploy.operationID)
	confirmed, err := s.recordRuntimeAction(ctx, id, runtimeID, "stop", deploymentID, snapshot)
	if err != nil {
		return nil, err
	}
	if !confirmed {
		if err = s.applyRetirement(ctx, deploy, id); err != nil {
			return nil, err
		}
	}
	operation, err := s.runtimeLifecycleOperation(ctx, deploy, "runtime_retire", "retirement", "succeeded")
	if err != nil {
		return nil, err
	}
	// The retirement completes only against a confirmed fact — the
	// provider-applied suspension or Serve's own readback that the runtime is gone
	// — so the reported operation carries that confirmation instead of an unread
	// outcome.
	if _, err = s.DB.ExecContext(ctx, `UPDATE serve.operations SET observation_result='confirmed',updated_at=now() WHERE id=$1`, operation.GetOperationId()); err != nil {
		return nil, dbError(err)
	}
	return s.operationByID(ctx, operation.GetOperationId())
}

// applyRetirement applies one unconfirmed retire intent. A runtime Serve recorded
// as terminated is already confirmed absent by its own readback, so that action is
// confirmed with the absence evidence and nothing is stopped again. Every other
// runtime is stopped through the execution boundary for the exact confirmed Fabric
// binding, and the action is confirmed only with the provider's own applied fact.
func (s *Service) applyRetirement(ctx context.Context, deploy *persistedRuntime, actionID string) error {
	runtimeID := deploy.command.GetRuntimeInstanceId()
	deploymentID := deploy.command.GetDeploymentId()
	var state string
	if err := s.DB.QueryRowContext(ctx, `SELECT status FROM serve.agent_runtime_instances WHERE id=$1 AND deployment_id=$2`, runtimeID, deploymentID).Scan(&state); err != nil {
		return dbError(err)
	}
	if state == "terminated" {
		return s.confirmRuntimeAction(ctx, actionID, "absent:"+deploymentID)
	}
	if s.Runtime == nil {
		return status.Error(codes.Unavailable, "runtime execution adapter is not configured")
	}
	target, err := s.confirmedRuntimeTarget(ctx, deploy.command)
	if err != nil {
		return err
	}
	if err = s.Runtime.Lifecycle(ctx, deploy.command, target, "suspended"); err != nil {
		return err
	}
	if err = s.confirmRuntimeAction(ctx, actionID, "suspended:"+deploymentID); err != nil {
		return err
	}
	return s.setRuntimeInstanceState(ctx, runtimeID, "stopped")
}
