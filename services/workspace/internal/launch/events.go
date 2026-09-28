package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

func (s *Service) Deliver(ctx context.Context, request *api.DeliverEventRequest) (*api.InboxAck, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || (peer != owneridentity.Serve.Service() && peer != owneridentity.Ledger.Service()) {
		return nil, status.Error(codes.Unauthenticated, "verified Serve or Ledger peer required")
	}
	event := request.GetEvent()
	if event == nil || request.GetAuthenticatedProducer() != string(peer) || event.GetOwner() != string(peer) || event.GetScope() != "tenant" || event.GetTenantId() == "" || event.GetRequestId() == "" || event.GetEventId() == "" || event.GetAggregateVersion() < 1 || event.GetSchemaVersion() != 1 {
		return nil, status.Error(codes.InvalidArgument, "invalid Workspace domain event")
	}
	payload, aggregateType, err := workspaceEventPayload(event)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if event.GetEventType() == "serve.agent_readiness_observed.v1" && event.GetAggregateId() != event.GetRuntimeReadinessObserved().GetRuntimeInstanceId() {
		return nil, status.Error(codes.InvalidArgument, "Serve readiness aggregate identity mismatch")
	}
	if event.GetEventType() == "ledger.receipt_recorded.v1" && event.GetAggregateId() != event.GetReceiptRecorded().GetReceiptId() {
		return nil, status.Error(codes.InvalidArgument, "Ledger receipt aggregate identity mismatch")
	}
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()
	result, err := s.Store.DeliverInbox(ctx, tx, ownerstore.InboundEvent{
		ID: "in_workspace_" + string(peer) + "_" + event.GetEventId(), SourceOwner: string(peer), SourceEventID: event.GetEventId(), EventType: event.GetEventType(), SchemaVersion: event.GetSchemaVersion(), AggregateType: aggregateType, AggregateID: event.GetAggregateId(), AggregateRevision: event.GetAggregateVersion(), Payload: payload,
	}, time.Now().UTC())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid Workspace domain event")
	}
	if result.Decision == ownerstore.InboxConflict {
		return nil, status.Error(codes.AlreadyExists, "Workspace event identity conflicts")
	}
	if result.Decision == ownerstore.InboxDuplicate {
		if err = tx.Commit(); err != nil {
			return nil, dbError(err)
		}
		return &api.InboxAck{EventId: event.GetEventId(), Consumer: "workspace", Committed: true, Duplicate: true, AppliedAggregateVersion: event.GetAggregateVersion()}, nil
	}
	var resourceID string
	switch event.GetEventType() {
	case "serve.agent_readiness_observed.v1":
		resourceID, err = s.applyServeReadinessEvent(ctx, tx, event.GetTenantId(), event.GetRuntimeReadinessObserved())
	case "ledger.receipt_recorded.v1":
		resourceID, err = s.applyLedgerReceiptEvent(ctx, tx, event.GetTenantId(), event.GetReceiptRecorded())
	default:
		err = status.Error(codes.InvalidArgument, "unsupported Workspace domain event")
	}
	if err != nil {
		return nil, err
	}
	if err = s.Store.MarkInboxProcessed(ctx, tx, string(peer), event.GetEventId(), resourceID, "", time.Now().UTC()); err != nil {
		return nil, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return &api.InboxAck{EventId: event.GetEventId(), Consumer: "workspace", Committed: true, AppliedAggregateVersion: event.GetAggregateVersion()}, nil
}

func workspaceEventPayload(event *api.EventEnvelope) ([]byte, string, error) {
	var message proto.Message
	var aggregateType string
	switch event.GetEventType() {
	case "serve.agent_readiness_observed.v1":
		message, aggregateType = event.GetRuntimeReadinessObserved(), "runtime_instance"
	case "ledger.receipt_recorded.v1":
		message, aggregateType = event.GetReceiptRecorded(), "receipt"
	default:
		return nil, "", errors.New("unsupported Workspace domain event")
	}
	if message == nil {
		return nil, "", errors.New("Workspace domain event payload is required")
	}
	payload, err := protojson.Marshal(message)
	return payload, aggregateType, err
}

func (s *Service) applyServeReadinessEvent(ctx context.Context, tx *sql.Tx, tenant string, payload *api.RuntimeReadinessObservedEvent) (string, error) {
	if payload == nil || payload.GetRuntimeInstanceId() == "" || payload.GetWorkspaceId() == "" || payload.GetDeploymentId() == "" || (payload.GetOutcome() != "confirmed" && payload.GetOutcome() != "rejected" && payload.GetOutcome() != "unknown") || (payload.GetOutcome() == "confirmed" && (!payload.GetApplicationAvailable() || payload.GetReceiptId() == "")) {
		return "", status.Error(codes.InvalidArgument, "invalid Serve readiness evidence")
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,tenant_id,resource_id,status,observation_result,result FROM workspace.operations WHERE kind='create_workspace' AND resource_id=$1 ORDER BY updated_at DESC,id DESC FOR UPDATE`, payload.GetWorkspaceId())
	if err != nil {
		return "", dbError(err)
	}
	defer rows.Close()
	for rows.Next() {
		var opID, tenantID, resourceID, state, observation string
		var raw []byte
		if err = rows.Scan(&opID, &tenantID, &resourceID, &state, &observation, &raw); err != nil {
			return "", dbError(err)
		}
		if tenantID != "" && tenantID != tenant {
			continue
		}
		var result orderResult
		if len(raw) == 0 || json.Unmarshal(raw, &result) != nil || len(result.RuntimeCommand) == 0 {
			continue
		}
		command := &api.RuntimeDeployCommand{}
		if protojson.Unmarshal(result.RuntimeCommand, command) != nil || command.GetWorkspaceId() != payload.GetWorkspaceId() || command.GetDeploymentId() != payload.GetDeploymentId() || command.GetRuntimeInstanceId() != payload.GetRuntimeInstanceId() {
			continue
		}
		stage, nextState, nextObservation, code := "receipt", "awaiting_confirmation", "confirmed", "DEPENDENCY_UNAVAILABLE"
		if payload.GetOutcome() == "rejected" {
			stage, nextState, nextObservation, code = "runtime", "needs_attention", "rejected", "RUNTIME_REJECTED"
		} else if payload.GetOutcome() == "unknown" {
			stage, nextState, nextObservation = "runtime", "awaiting_confirmation", "unknown"
		}
		_, err = tx.ExecContext(ctx, `UPDATE workspace.operations SET status=$2,stage=$3,observation_result=$4,error_code=$5,updated_at=now() WHERE id=$1 AND status NOT IN ('succeeded','failed','cancelled')`, opID, nextState, stage, nextObservation, code)
		if err != nil {
			return "", dbError(err)
		}
		return resourceID, nil
	}
	if err = rows.Err(); err != nil {
		return "", dbError(err)
	}
	return "", status.Error(codes.FailedPrecondition, "Workspace runtime command for Serve readiness was not found")
}

func (s *Service) applyLedgerReceiptEvent(ctx context.Context, tx *sql.Tx, tenant string, payload *api.ReceiptRecordedEvent) (string, error) {
	if payload == nil || payload.GetReceiptId() == "" || payload.GetSourceOwner() != "workspace" || !strings.HasSuffix(payload.GetOwnerEvidenceReference(), ":deployment") || !validEvidenceDigest(payload.GetEvidenceDigest()) {
		return "", status.Error(codes.InvalidArgument, "invalid Ledger receipt evidence")
	}
	opID := strings.TrimSuffix(payload.GetOwnerEvidenceReference(), ":deployment")
	var tenantID, resourceID, state string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT tenant_id,resource_id,status,result FROM workspace.operations WHERE id=$1 AND kind='create_workspace' FOR UPDATE`, opID).Scan(&tenantID, &resourceID, &state, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", status.Error(codes.FailedPrecondition, "Workspace operation for Ledger receipt was not found")
	}
	if err != nil {
		return "", dbError(err)
	}
	if tenantID != tenant {
		return "", status.Error(codes.PermissionDenied, "Ledger receipt belongs to another tenant")
	}
	var result orderResult
	if json.Unmarshal(raw, &result) != nil || len(result.RuntimeCommand) == 0 || len(result.RuntimeReadback) == 0 {
		return "", status.Error(codes.FailedPrecondition, "Workspace runtime readback is not ready for Ledger receipt")
	}
	command := &api.RuntimeDeployCommand{}
	readback := &api.RuntimeReadback{}
	if protojson.Unmarshal(result.RuntimeCommand, command) != nil || protojson.Unmarshal(result.RuntimeReadback, readback) != nil || command.GetWorkspaceId() != resourceID || validateRuntimeReadback(command, readback) != nil {
		return "", status.Error(codes.FailedPrecondition, "Workspace runtime identity is not ready for Ledger receipt")
	}
	stored := &api.Receipt{}
	if len(result.DeploymentReceipt) > 0 && protojson.Unmarshal(result.DeploymentReceipt, stored) == nil && stored.GetId() == payload.GetReceiptId() && stored.GetOperationId() == opID && stored.GetKind() == api.ReceiptKindEnum_RECEIPT_KIND_ENUM_DEPLOYMENT && stored.GetOutcome() == api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED {
		resultRaw, marshalErr := json.Marshal(result)
		if marshalErr != nil {
			return "", status.Error(codes.Internal, "Workspace result cannot be encoded")
		}
		_, err = tx.ExecContext(ctx, `UPDATE workspace.operations SET result=$2,status='succeeded',stage='succeeded',observation_result='confirmed',error_code=NULL,completed_at=COALESCE(completed_at,now()),updated_at=now() WHERE id=$1 AND status NOT IN ('succeeded','failed','cancelled')`, opID, resultRaw)
		if err != nil {
			return "", dbError(err)
		}
		_, err = tx.ExecContext(ctx, `UPDATE workspace.workspaces SET status='active',version=version+1,updated_at=now() WHERE id=$1 AND active_operation_id=$2 AND status NOT IN ('deleted','deleting')`, resourceID, opID)
		if err != nil {
			return "", dbError(err)
		}
		return resourceID, nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE workspace.operations SET status=CASE WHEN status='needs_attention' THEN status ELSE 'awaiting_confirmation' END,stage='receipt',observation_result='unknown',error_code='DEPENDENCY_UNAVAILABLE',updated_at=now() WHERE id=$1 AND status NOT IN ('succeeded','failed','cancelled')`, opID)
	if err != nil {
		return "", dbError(err)
	}
	return resourceID, nil
}

func validEvidenceDigest(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, r := range value[len("sha256:"):] {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
