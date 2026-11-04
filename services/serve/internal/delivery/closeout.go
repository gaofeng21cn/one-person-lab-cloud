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
func refuseRetiredRuntime(ctx context.Context, tx rowQueryer, runtimeID string) error {
	var retiring bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1 AND action='terminate')`, runtimeID).Scan(&retiring); err != nil {
		return dbError(err)
	}
	if retiring {
		return status.Error(codes.FailedPrecondition, "the original Workspace runtime is retiring")
	}
	return nil
}

// A Workspace deletion keeps its original reservation identity, even after Serve
// replaces the application. Freeze every descendant under the same owner lock:
// no caller needs to copy Serve's current selection into Workspace state.
type runtimeRetirement struct {
	deploy  *persistedRuntime
	started bool
}

func (s *Service) prepareWorkspaceRetirement(ctx context.Context, command *api.RuntimeStopCommand) ([]runtimeRetirement, error) {
	var workspace string
	if err := s.DB.QueryRowContext(ctx, `SELECT workspace_id FROM serve.agent_runtime_instances WHERE id=$1 AND deployment_id=$2`, command.GetRuntimeInstanceId(), command.GetDeploymentId()).Scan(&workspace); err != nil {
		return nil, dbError(err)
	}
	if command.GetWorkspaceId() != "" && command.GetWorkspaceId() != workspace {
		return nil, status.Error(codes.FailedPrecondition, "retirement identifies another Workspace")
	}
	if err := s.authorize(ctx, command.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME, workspace); err != nil {
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
	if err = s.authorizeWorkspaceAt(ctx, tx, command.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RETIRERUNTIME, workspace, false); err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `WITH RECURSIVE lineage(id) AS (
  SELECT id FROM serve.agent_deployments WHERE id=$2 AND workspace_id=$1 AND previous_deployment_id IS NULL
  UNION
  SELECT d.id FROM serve.agent_deployments d JOIN lineage p ON d.previous_deployment_id=p.id WHERE d.workspace_id=$1
 )
 SELECT d.id,d.runtime_instance_id,d.operation_id,COALESCE(o.tenant_id,''),o.actor_id,o.request_id,
 r.workspace_id,r.fabric_resource_set_id,r.data_attachment_contract,
 EXISTS(SELECT 1 FROM lineage WHERE id=d.id),
 EXISTS(SELECT 1 FROM serve.agent_runtime_actions WHERE runtime_instance_id=r.id AND action='start')
 FROM serve.agent_deployments d
 JOIN serve.agent_runtime_instances r ON r.id=d.runtime_instance_id AND r.deployment_id=d.id
 JOIN serve.operations o ON o.id=d.operation_id
 WHERE d.workspace_id=$1 ORDER BY (d.id=$2),d.execution_epoch DESC,d.id`, workspace, command.GetDeploymentId())
	if err != nil {
		return nil, dbError(err)
	}
	var targets []runtimeRetirement
	var original *persistedRuntime
	for rows.Next() {
		deploy := &persistedRuntime{command: &api.RuntimeDeployCommand{Context: command.GetContext(), WorkspaceId: workspace}}
		var runtimeWorkspace string
		var attachment []byte
		var inLineage, started bool
		if err = rows.Scan(&deploy.command.DeploymentId, &deploy.command.RuntimeInstanceId, &deploy.operationID, &deploy.tenantID, &deploy.actorID, &deploy.requestID, &runtimeWorkspace, &deploy.command.ResourceSetId, &attachment, &inLineage, &started); err != nil {
			rows.Close()
			return nil, dbError(err)
		}
		var binding struct {
			AttachmentID string `json:"attachmentId"`
		}
		if !inLineage || runtimeWorkspace != workspace || deploy.tenantID != command.GetContext().GetScope().GetTenant().GetTenantId() || json.Unmarshal(attachment, &binding) != nil || binding.AttachmentID == "" {
			rows.Close()
			return nil, status.Error(codes.FailedPrecondition, "Workspace delivery does not belong to the original retirement lineage")
		}
		deploy.command.DataAttachmentId = binding.AttachmentID
		if deploy.command.GetDeploymentId() == command.GetDeploymentId() && deploy.command.GetRuntimeInstanceId() == command.GetRuntimeInstanceId() {
			original = deploy
		}
		targets = append(targets, runtimeRetirement{deploy: deploy, started: started})
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, dbError(err)
	}
	rows.Close()
	if original == nil {
		return nil, status.Error(codes.FailedPrecondition, "original retirement reservation is unavailable")
	}
	if command.GetRetainedDataAttachmentId() != "" && original.command.GetDataAttachmentId() != command.GetRetainedDataAttachmentId() {
		return nil, status.Error(codes.FailedPrecondition, "retirement does not identify the reserved attachment")
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM serve.agent_deployments WHERE workspace_id=$1`, workspace).Scan(&count); err != nil {
		return nil, dbError(err)
	}
	if count != len(targets) {
		return nil, status.Error(codes.FailedPrecondition, "Workspace retirement has an unresolved deployment identity")
	}
	for _, target := range targets {
		if target.deploy.command.GetResourceSetId() != original.command.GetResourceSetId() || target.deploy.command.GetDataAttachmentId() != original.command.GetDataAttachmentId() {
			return nil, status.Error(codes.FailedPrecondition, "retirement lineage differs from the original resources")
		}
		exact := proto.Clone(command).(*api.RuntimeStopCommand)
		exact.RuntimeInstanceId, exact.DeploymentId = target.deploy.command.GetRuntimeInstanceId(), target.deploy.command.GetDeploymentId()
		if err = s.recordRuntimeRetirement(ctx, tx, exact, target.deploy); err != nil {
			return nil, err
		}
	}
	// Validate the selected route before clearing it or touching a provider. A
	// different delivery is a conflict, never evidence of application absence.
	if err = lockRouteBinding(ctx, tx, workspace); err != nil {
		return nil, dbError(err)
	}
	binding, err := ensureRouteBinding(ctx, tx, workspace)
	if err != nil {
		return nil, err
	}
	if binding.TargetExecutionResource.Valid {
		var selected *persistedRuntime
		for _, target := range targets {
			if binding.TargetExecutionResource.String == target.deploy.command.GetRuntimeInstanceId() && binding.TargetRuntimeInstance.String == target.deploy.command.GetRuntimeInstanceId() && binding.TargetDeployment.String == target.deploy.command.GetDeploymentId() {
				selected = target.deploy
			}
		}
		if selected == nil {
			return nil, status.Error(codes.FailedPrecondition, "the Workspace route selects another original runtime")
		}
		if _, err = commitRouteSwitchTx(ctx, tx, routeSwitchPlan{
			workspaceID: workspace, operationID: stableID("op_", "runtime_retire", original.command.GetRuntimeInstanceId(), original.operationID),
			actionKind: "retire", executionEpoch: binding.AcceptedExecutionEpoch + 1,
			expectedRouteGeneration: binding.RouteGeneration, expectedRouteRevision: binding.RouteRevision.String,
		}); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	// Decode every frozen start before provider effects. The original operation is
	// completed last, so a failed successor cannot be reported as deletion success.
	for i := range targets {
		if targets[i].started {
			expected := targets[i].deploy
			recorded, err := s.persistedRuntimeCommand(ctx, expected.command.GetRuntimeInstanceId(), expected.command.GetDeploymentId())
			if err != nil {
				return nil, err
			}
			if recorded.command.GetWorkspaceId() != workspace || recorded.command.GetResourceSetId() != expected.command.GetResourceSetId() || recorded.command.GetDataAttachmentId() != expected.command.GetDataAttachmentId() {
				return nil, status.Error(codes.DataLoss, "recorded runtime command differs from its retirement resources")
			}
			recorded.command.Context = command.GetContext()
			targets[i].deploy = recorded
		}
	}
	return targets, nil
}

func (s *Service) recordRuntimeRetirement(ctx context.Context, tx *sql.Tx, command *api.RuntimeStopCommand, deploy *persistedRuntime) error {
	operationID := stableID("op_", "runtime_retire", command.GetRuntimeInstanceId(), deploy.operationID)
	var kind, resource, requestID string
	err := tx.QueryRowContext(ctx, `SELECT kind,resource_id,request_id FROM serve.operations WHERE id=$1`, operationID).Scan(&kind, &resource, &requestID)
	if err == nil {
		if kind != "runtime_retire" || resource != command.GetRuntimeInstanceId() || requestID != deploy.requestID {
			return status.Error(codes.AlreadyExists, "runtime lifecycle operation identity mismatch")
		}
	} else if errors.Is(err, sql.ErrNoRows) {
		accepted, _ := json.Marshal(map[string]any{"runtimeInstanceId": command.GetRuntimeInstanceId(), "deploymentId": command.GetDeploymentId(), "action": "runtime_retire", "identity": []string(nil)})
		if _, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operationID, TenantID: deploy.tenantID, ActorID: deploy.actorID, Kind: "runtime_retire", ResourceID: command.GetRuntimeInstanceId(), Stage: "retirement", RequestID: deploy.requestID, AcceptedInput: accepted}); err != nil {
			return dbError(err)
		}
	} else {
		return dbError(err)
	}
	snapshot, _ := json.Marshal(map[string]string{"desired": "absent", "retainedDataAttachmentId": command.GetRetainedDataAttachmentId()})
	actionID := stableID("act_", "terminate", command.GetRuntimeInstanceId(), deploy.operationID)
	if _, err = tx.ExecContext(ctx, `INSERT INTO serve.agent_runtime_actions(id,runtime_instance_id,command_id,action,expected_deployment_id,input_snapshot,observation_result) VALUES($1,$2,$1,'terminate',$3,$4,'unknown') ON CONFLICT(command_id) DO NOTHING`, actionID, command.GetRuntimeInstanceId(), command.GetDeploymentId(), snapshot); err != nil {
		return dbError(err)
	}
	var identical bool
	if err = tx.QueryRowContext(ctx, `SELECT input_snapshot=$2::jsonb FROM serve.agent_runtime_actions WHERE command_id=$1`, actionID, snapshot).Scan(&identical); err != nil {
		return dbError(err)
	}
	if !identical {
		return status.Error(codes.AlreadyExists, "runtime retirement differs from the original command")
	}
	return nil
}

func refuseClosedWorkspace(ctx context.Context, tx rowQueryer, workspace string) error {
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
