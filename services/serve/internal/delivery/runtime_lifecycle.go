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

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// persistedRuntime is the exact original delivery a lifecycle command resolves from
// Serve's own store. It carries the frozen command so Stop/Reload/Credentials act
// on the same runtime the order reserved, never a newly derived one.
type persistedRuntime struct {
	command             *api.RuntimeDeployCommand
	operationID         string
	tenantID            string
	actorID             string
	requestID           string
	accountID           string
	appliedModelVersion int64
}

// persistedRuntimeCommand resolves the frozen original RuntimeDeployCommand for one
// runtime instance. The command was stored in serve.agent_runtime_actions at
// Reserve/Deploy time, so this is the identical command every continuation replays.
func (s *Service) persistedRuntimeCommand(ctx context.Context, runtimeID, deploymentID string) (*persistedRuntime, error) {
	var deployment, workspace, opID, tenantID, actorID, requestID, accountID string
	var appliedVersion int64
	err := s.DB.QueryRowContext(ctx, `
		SELECT d.id, d.workspace_id, d.operation_id, COALESCE(o.tenant_id,''), o.actor_id, o.request_id,
		       COALESCE(r.fabric_resource_set_id,''), r.applied_model_configuration_version
		FROM serve.agent_runtime_instances r
		JOIN serve.agent_deployments d ON d.id = r.deployment_id
		JOIN serve.operations o ON o.id = d.operation_id
		WHERE r.id = $1 AND ($2 = '' OR d.id = $2)`, runtimeID, deploymentID).
		Scan(&deployment, &workspace, &opID, &tenantID, &actorID, &requestID, &accountID, &appliedVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "runtime instance not found")
	}
	if err != nil {
		return nil, dbError(err)
	}
	var raw []byte
	err = s.DB.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE command_id=$1`, stableID("start_", deployment)).Scan(&raw)
	if err != nil {
		return nil, dbError(err)
	}
	command := &api.RuntimeDeployCommand{}
	if protojson.Unmarshal(raw, command) != nil || command.GetDeploymentId() != deployment || command.GetRuntimeInstanceId() != runtimeID {
		return nil, status.Error(codes.DataLoss, "stored runtime command is invalid")
	}
	_ = workspace
	return &persistedRuntime{command: command, operationID: opID, tenantID: tenantID, actorID: actorID, requestID: requestID, accountID: accountID, appliedModelVersion: appliedVersion}, nil
}

// confirmedRuntimeBinding resolves the exact Fabric execution binding the command
// reserved, so lifecycle and credentials reach the same provider object.
func (s *Service) confirmedRuntimeBinding(ctx context.Context, command *api.RuntimeDeployCommand) (*api.ResourceExecutionBinding, error) {
	if s.Resources == nil {
		return nil, status.Error(codes.Unavailable, "Fabric readback is not configured")
	}
	resources, err := s.Resources.ReadResources(ctx, &api.ResourceReadbackRequest{Context: nextOwnerCall(command.GetContext()), ResourceSetId: command.GetResourceSetId()})
	if err != nil {
		return nil, err
	}
	return confirmedBinding(command, resources)
}

// runtimeLifecycleOperation records one lifecycle action as an owner Operation in
// Serve's own store and returns that row exactly as committed. The action
// identity is deterministic, so a resumed command resumes its original operation
// instead of writing a second one; a row that exists with different input is
// refused rather than reported as this caller's own.
//
// The reported state is read back from the owner row. A lifecycle action never
// reports success here: applying a desired state and confirming it is a separate,
// observed fact, and the operation stays accepted until the provider readback
// records it.
func (s *Service) runtimeLifecycleOperation(ctx context.Context, deploy *persistedRuntime, kind, stage, nextStatus string) (*api.Operation, error) {
	opID := stableID("op_", kind, deploy.command.GetRuntimeInstanceId(), deploy.operationID)
	runtimeID := deploy.command.GetRuntimeInstanceId()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer func() { _ = tx.Rollback() }()
	accepted, _ := json.Marshal(map[string]any{"runtimeInstanceId": runtimeID, "deploymentId": deploy.command.GetDeploymentId(), "action": kind})
	if _, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: opID, TenantID: deploy.tenantID, ActorID: deploy.actorID, Kind: kind, ResourceID: runtimeID, Stage: strings.ToLower(stage), RequestID: deploy.requestID, AcceptedInput: accepted}); err != nil {
		// The action identity is already recorded. It is this caller's own
		// operation only when it names the same runtime, kind and request.
		existing, readErr := s.Store.ReadOperation(ctx, opID)
		if readErr != nil || existing.Kind != kind || existing.ResourceID != runtimeID || existing.RequestID != deploy.requestID {
			return nil, status.Error(codes.AlreadyExists, "runtime lifecycle operation identity mismatch")
		}
		return s.operationByID(ctx, opID)
	}
	// The recorded status is the fact this action actually established: a state
	// the provider confirmed may complete, while an action whose applied result is
	// still unread waits for confirmation instead of claiming success.
	if _, err = tx.ExecContext(ctx, `UPDATE serve.operations SET status=$2,stage=$3,completed_at=CASE WHEN $2='succeeded' THEN COALESCE(completed_at,now()) ELSE completed_at END,updated_at=now() WHERE id=$1`, opID, nextStatus, stage); err != nil {
		return nil, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return s.operationByID(ctx, opID)
}

// applyRecordedLifecycle persists one lifecycle intent for the exact persisted
// runtime before the provider call and reports whether the provider applied it. A
// retry of the same action resumes the recorded intent instead of re-issuing the
// call, and a retry presenting different input is refused rather than silently
// re-targeting the runtime. The provider call is repeated only when the earlier
// outcome was never confirmed, which is the one case where it is unknown.
func (s *Service) applyRecordedLifecycle(ctx context.Context, deploy *persistedRuntime, action string, snapshot map[string]string, apply func() error) error {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return status.Error(codes.Internal, "cannot record the runtime lifecycle action")
	}
	id := stableID("act_", action, deploy.command.GetRuntimeInstanceId(), deploy.operationID)
	confirmed, err := s.recordRuntimeAction(ctx, id, deploy.command.GetRuntimeInstanceId(), action, deploy.command.GetDeploymentId(), raw)
	if err != nil {
		return err
	}
	if confirmed {
		return nil
	}
	if err = apply(); err != nil {
		return err
	}
	return s.confirmRuntimeAction(ctx, id, action)
}
