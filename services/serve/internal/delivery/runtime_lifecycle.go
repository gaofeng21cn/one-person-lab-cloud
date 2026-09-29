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
// Serve's own store. It is idempotent on the action identity, so a resumed command
// returns the same operation instead of writing a second one.
func (s *Service) runtimeLifecycleOperation(ctx context.Context, deploy *persistedRuntime, kind, stage string) *api.Operation {
	opID := stableID("op_", kind, deploy.command.GetRuntimeInstanceId(), deploy.operationID)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return &api.Operation{OperationId: opID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE, ResourceId: deploy.command.GetRuntimeInstanceId(), Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_RUNNING, Stage: api.OperationStageEnum(api.OperationStageEnum_value["OPERATION_STAGE_ENUM_RUNTIME"])}
	}
	defer tx.Rollback()
	accepted, _ := json.Marshal(map[string]any{"runtimeInstanceId": deploy.command.GetRuntimeInstanceId(), "deploymentId": deploy.command.GetDeploymentId(), "action": kind})
	if _, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: opID, TenantID: deploy.tenantID, ActorID: deploy.actorID, Kind: kind, ResourceID: deploy.command.GetRuntimeInstanceId(), Stage: strings.ToLower(stage), RequestID: deploy.requestID, AcceptedInput: accepted}); err != nil {
		return &api.Operation{OperationId: opID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE, ResourceId: deploy.command.GetRuntimeInstanceId(), Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_RUNNING, Stage: api.OperationStageEnum(api.OperationStageEnum_value["OPERATION_STAGE_ENUM_RUNTIME"])}
	}
	_ = tx.Commit()
	return &api.Operation{OperationId: opID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE, Kind: api.OperationKindEnum(api.OperationKindEnum_value["OPERATION_KIND_ENUM_"+strings.ToUpper(kind)]), ResourceId: deploy.command.GetRuntimeInstanceId(), Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, Stage: api.OperationStageEnum(api.OperationStageEnum_value["OPERATION_STAGE_ENUM_RUNTIME"])}
}
