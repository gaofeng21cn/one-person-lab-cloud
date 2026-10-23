package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Called under the runtime's owner lock before admitting or executing a start.
// An unconfirmed termination also fences starts: its external effect is unknown.
func refuseRetiredRuntime(ctx context.Context, tx *sql.Tx, runtimeID string) error {
	var retiring bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1 AND action='terminate')`, runtimeID).Scan(&retiring); err != nil {
		return dbError(err)
	}
	if retiring {
		return status.Error(codes.FailedPrecondition, "the original Workspace runtime is retiring")
	}
	return nil
}

// Retire through the same conditional route writer as activation. A replay of
// an already cleared route has no new generation or route effect.
func (s *Service) retireRuntimeRoute(ctx context.Context, deploy *persistedRuntime) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	workspace := deploy.command.GetWorkspaceId()
	if err = lockWorkspace(ctx, tx, workspace); err != nil {
		return dbError(err)
	}
	if err = lockRouteBinding(ctx, tx, workspace); err != nil {
		return dbError(err)
	}
	binding, err := ensureRouteBinding(ctx, tx, workspace)
	if err != nil {
		return err
	}
	if !binding.TargetExecutionResource.Valid {
		return nil
	}
	if binding.TargetRuntimeInstance.Valid && binding.TargetRuntimeInstance.String != deploy.command.GetRuntimeInstanceId() {
		return status.Error(codes.FailedPrecondition, "the Workspace route selects another original runtime")
	}
	_, err = commitRouteSwitchTx(ctx, tx, routeSwitchPlan{
		workspaceID: workspace, operationID: stableID("op_", "runtime_retire", deploy.command.GetRuntimeInstanceId(), deploy.operationID),
		actionKind: "retire", executionEpoch: binding.AcceptedExecutionEpoch + 1,
		expectedRouteGeneration: binding.RouteGeneration, expectedRouteRevision: binding.RouteRevision.String,
	})
	if err != nil {
		return err
	}
	return dbError(tx.Commit())
}

// Freeze the exact reservation before resolving its optional start command. The
// workspace lock drains in-flight delivery; the durable terminate action then
// prevents late Deploy, including a start whose caller lost its response.
func (s *Service) prepareRuntimeRetirement(ctx context.Context, command *api.RuntimeStopCommand) (*persistedRuntime, bool, error) {
	var workspace string
	if err := s.DB.QueryRowContext(ctx, `SELECT workspace_id FROM serve.agent_runtime_instances WHERE id=$1 AND deployment_id=$2`, command.GetRuntimeInstanceId(), command.GetDeploymentId()).Scan(&workspace); err != nil {
		return nil, false, dbError(err)
	}
	if err := s.authorize(ctx, command.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME, workspace); err != nil {
		return nil, false, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, false, dbError(err)
	}
	defer tx.Rollback()
	if err = lockWorkspace(ctx, tx, workspace); err != nil {
		return nil, false, dbError(err)
	}
	if err = s.authorizeWorkspaceAt(ctx, tx, command.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME, workspace, false); err != nil {
		return nil, false, err
	}
	deploy := &persistedRuntime{command: &api.RuntimeDeployCommand{Context: command.GetContext(), WorkspaceId: workspace, RuntimeInstanceId: command.GetRuntimeInstanceId(), DeploymentId: command.GetDeploymentId()}}
	var attachment []byte
	if err = tx.QueryRowContext(ctx, `SELECT d.operation_id,COALESCE(o.tenant_id,''),o.actor_id,o.request_id,r.data_attachment_contract FROM serve.agent_runtime_instances r JOIN serve.agent_deployments d ON d.id=r.deployment_id JOIN serve.operations o ON o.id=d.operation_id WHERE r.id=$1 AND d.id=$2`, command.GetRuntimeInstanceId(), command.GetDeploymentId()).Scan(&deploy.operationID, &deploy.tenantID, &deploy.actorID, &deploy.requestID, &attachment); err != nil {
		return nil, false, dbError(err)
	}
	var binding struct {
		AttachmentID string `json:"attachmentId"`
	}
	if json.Unmarshal(attachment, &binding) != nil || (command.GetRetainedDataAttachmentId() != "" && binding.AttachmentID != command.GetRetainedDataAttachmentId()) {
		return nil, false, status.Error(codes.FailedPrecondition, "retirement does not identify the reserved attachment")
	}
	operationID := stableID("op_", "runtime_retire", command.GetRuntimeInstanceId(), deploy.operationID)
	var kind, resource, requestID string
	err = tx.QueryRowContext(ctx, `SELECT kind,resource_id,request_id FROM serve.operations WHERE id=$1`, operationID).Scan(&kind, &resource, &requestID)
	if err == nil {
		if kind != "runtime_retire" || resource != command.GetRuntimeInstanceId() || requestID != deploy.requestID {
			return nil, false, status.Error(codes.AlreadyExists, "runtime lifecycle operation identity mismatch")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		accepted, _ := json.Marshal(map[string]any{"runtimeInstanceId": command.GetRuntimeInstanceId(), "deploymentId": command.GetDeploymentId(), "action": "runtime_retire", "identity": []string(nil)})
		if _, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operationID, TenantID: deploy.tenantID, ActorID: deploy.actorID, Kind: "runtime_retire", ResourceID: command.GetRuntimeInstanceId(), Stage: "retirement", RequestID: deploy.requestID, AcceptedInput: accepted}); err != nil {
			return nil, false, dbError(err)
		}
	} else {
		return nil, false, dbError(err)
	}
	snapshot, _ := json.Marshal(map[string]string{"desired": "absent", "retainedDataAttachmentId": command.GetRetainedDataAttachmentId()})
	actionID := stableID("act_", "terminate", command.GetRuntimeInstanceId(), deploy.operationID)
	if _, err = tx.ExecContext(ctx, `INSERT INTO serve.agent_runtime_actions(id,runtime_instance_id,command_id,action,expected_deployment_id,input_snapshot,observation_result) VALUES($1,$2,$1,'terminate',$3,$4,'unknown') ON CONFLICT(command_id) DO NOTHING`, actionID, command.GetRuntimeInstanceId(), command.GetDeploymentId(), snapshot); err != nil {
		return nil, false, dbError(err)
	}
	var identical bool
	if err = tx.QueryRowContext(ctx, `SELECT input_snapshot=$2::jsonb FROM serve.agent_runtime_actions WHERE command_id=$1`, actionID, snapshot).Scan(&identical); err != nil {
		return nil, false, dbError(err)
	}
	if !identical {
		return nil, false, status.Error(codes.AlreadyExists, "runtime retirement differs from the original command")
	}
	var started bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1 AND action='start')`, command.GetRuntimeInstanceId()).Scan(&started); err != nil {
		return nil, false, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, false, dbError(err)
	}
	if !started {
		return deploy, false, nil
	}
	deploy, err = s.persistedRuntimeCommand(ctx, command.GetRuntimeInstanceId(), command.GetDeploymentId())
	return deploy, true, err
}

func refuseClosedWorkspace(ctx context.Context, tx *sql.Tx, workspace string) error {
	var closing bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.operations o LEFT JOIN serve.agent_runtime_instances r ON r.id=o.resource_id WHERE o.kind='runtime_retire' AND (o.resource_id=$1 OR r.workspace_id=$1))`, workspace).Scan(&closing); err != nil {
		return dbError(err)
	}
	if closing {
		return status.Error(codes.FailedPrecondition, "Workspace delivery is closed")
	}
	return nil
}

func (s *Service) originalReservation(ctx context.Context, q rowQueryer, tenant, workspace, key string) (string, string, error) {
	var deployment string
	var count int
	err := q.QueryRowContext(ctx, `SELECT resource_id,COUNT(*) OVER() FROM serve.idempotency_records WHERE tenant_scope=$1 AND operation_name='ReserveRuntime' AND idempotency_key=$2`, tenant, key).Scan(&deployment, &count)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	if err != nil {
		return "", "", dbError(err)
	}
	if count != 1 || deployment == "" {
		return "", "", status.Error(codes.DataLoss, "original runtime reservation is ambiguous")
	}
	var runtime, storedWorkspace, storedTenant string
	if err = q.QueryRowContext(ctx, `SELECT d.runtime_instance_id,d.workspace_id,COALESCE(o.tenant_id,'') FROM serve.agent_deployments d JOIN serve.operations o ON o.id=d.operation_id WHERE d.id=$1`, deployment).Scan(&runtime, &storedWorkspace, &storedTenant); err != nil {
		return "", "", dbError(err)
	}
	if storedWorkspace != workspace || storedTenant != tenant {
		return "", "", status.Error(codes.PermissionDenied, "original reservation belongs to another Workspace")
	}
	return runtime, deployment, nil
}

func (s *Service) retireOriginalReservation(ctx context.Context, request *api.RuntimeStopCommand) (*api.Operation, error) {
	if err := ownerservice.ValidateCallContext(ctx, request.GetContext()); err != nil {
		return nil, err
	}
	tenant, workspace := request.GetContext().GetScope().GetTenant().GetTenantId(), request.GetWorkspaceId()
	if tenant == "" || workspace == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant and original Workspace required")
	}
	if err := s.authorize(ctx, request.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME, workspace); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()
	if err = lockWorkspace(ctx, tx, workspace); err != nil {
		return nil, dbError(err)
	}
	if err = s.authorizeWorkspaceAt(ctx, tx, request.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME, workspace, false); err != nil {
		return nil, err
	}
	runtime, deployment, err := s.originalReservation(ctx, tx, tenant, workspace, request.GetOriginalReservationIdempotencyKey())
	if err != nil {
		return nil, err
	}
	if runtime != "" {
		tx.Rollback()
		resolved := proto.Clone(request).(*api.RuntimeStopCommand)
		resolved.RuntimeInstanceId, resolved.DeploymentId = runtime, deployment
		return s.Retire(ctx, resolved)
	}
	var hasDelivery bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.agent_deployments WHERE workspace_id=$1)`, workspace).Scan(&hasDelivery); err != nil {
		return nil, dbError(err)
	}
	if hasDelivery {
		return nil, status.Error(codes.FailedPrecondition, "Workspace delivery exists without this original reservation")
	}
	operationID := stableID("op_", "runtime_retire", tenant, workspace)
	var original string
	err = tx.QueryRowContext(ctx, `SELECT accepted_input->>'originalReservationIdempotencyKey' FROM serve.operations WHERE id=$1`, operationID).Scan(&original)
	if err == nil {
		if original != request.GetOriginalReservationIdempotencyKey() {
			return nil, status.Error(codes.FailedPrecondition, "runtime closeout identifies another original reservation")
		}
		tx.Rollback()
		return s.operationByID(ctx, operationID)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, dbError(err)
	}
	command := proto.Clone(request).(*api.RuntimeStopCommand)
	command.Context = nil
	body, err := protojson.Marshal(command)
	if err != nil {
		return nil, err
	}
	if _, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operationID, TenantID: tenant, ActorID: request.GetContext().GetActorId(), Kind: "runtime_retire", ResourceID: workspace, Stage: "retirement", RequestID: request.GetContext().GetRequestId(), AcceptedInput: body}); err != nil {
		return nil, dbError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE serve.operations SET status='succeeded',observation_result='confirmed',completed_at=now(),updated_at=now() WHERE id=$1`, operationID); err != nil {
		return nil, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return s.operationByID(ctx, operationID)
}

func (s *Service) readOriginalReservation(ctx context.Context, request *api.RuntimeReadbackRequest) (*api.RuntimeReadback, error) {
	if err := ownerservice.ValidateCallContext(ctx, request.GetContext()); err != nil {
		return nil, err
	}
	tenant, workspace := request.GetContext().GetScope().GetTenant().GetTenantId(), request.GetWorkspaceId()
	if tenant == "" || workspace == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant and original Workspace required")
	}
	if err := s.authorize(ctx, request.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME, workspace); err != nil {
		return nil, err
	}
	runtime, deployment, err := s.originalReservation(ctx, s.DB, tenant, workspace, request.GetOriginalReservationIdempotencyKey())
	if err != nil {
		return nil, err
	}
	if runtime != "" {
		if (request.GetRuntimeInstanceId() != "" && request.GetRuntimeInstanceId() != runtime) || (request.GetDeploymentId() != "" && request.GetDeploymentId() != deployment) {
			return nil, status.Error(codes.FailedPrecondition, "original runtime identity differs")
		}
		return s.runtimeReadback(ctx, runtime, deployment)
	}
	var closed bool
	if err = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.operations WHERE tenant_id=$1 AND resource_id=$2 AND kind='runtime_retire' AND accepted_input->>'originalReservationIdempotencyKey'=$3 AND status='succeeded' AND observation_result='confirmed') AND NOT EXISTS(SELECT 1 FROM serve.agent_deployments WHERE workspace_id=$2)`, tenant, workspace, request.GetOriginalReservationIdempotencyKey()).Scan(&closed); err != nil {
		return nil, dbError(err)
	}
	if !closed {
		return nil, status.Error(codes.NotFound, "original reservation has no confirmed closeout")
	}
	return &api.RuntimeReadback{WorkspaceId: workspace, State: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED, Outcome: api.Observation_OBSERVATION_CONFIRMED, NeverDeliveredConfirmed: true, ObservedAt: timestamppb.Now()}, nil
}
