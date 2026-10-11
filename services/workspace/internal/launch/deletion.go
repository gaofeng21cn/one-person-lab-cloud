package launch

// This file owns the Workspace deletion: one durable deletion intent per
// Workspace that retires the exact original runtime, removes the original
// provider resources, records the confirmed deletion evidence and settles the
// original charge through the wallet authority. The owners stay separate on
// purpose - Serve proves the application runtime is gone, Fabric proves the
// provider resources are absent, Ledger records the deletion evidence and
// Gateway is the only writer that may move the refund. Workspace coordinates
// and never fabricates another owner's fact: a stage advances only on that
// owner's own confirmed readback, and "not found in a list" is never absence.

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
	"google.golang.org/protobuf/types/known/timestamppb"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// deletionOrder is immutable accepted deletion intent plus the frozen original
// execution and payment facts the deletion must act on. Every cross-owner call is
// derived from these recorded facts, so a resume never re-derives a runtime, a
// resource set, a period or a charge from current state that may have moved.
type deletionOrder struct {
	Request                json.RawMessage `json:"request"`
	AuthorizationContextID string          `json:"authorizationContextId"`
	InputDigest            string          `json:"inputDigest"`
	WorkspaceName          string          `json:"workspaceName"`
	WorkspaceVersion       int64           `json:"workspaceVersion"`

	LaunchOperationID string `json:"launchOperationId"`
	GrantID           string `json:"grantId"`
	RuntimeInstanceID string `json:"runtimeInstanceId"`
	DeploymentID      string `json:"deploymentId"`
	DataAttachmentID  string `json:"dataAttachmentId"`
	ResourceSetID     string `json:"resourceSetId"`

	SubscriptionPeriodID            string `json:"subscriptionPeriodId"`
	PeriodStart                     string `json:"periodStart"`
	OriginalChargeWalletOperationID string `json:"originalChargeWalletOperationId"`
	OriginalChargeReceiptID         string `json:"originalChargeReceiptId"`
	OriginalChargeUSDMicros         int64  `json:"originalChargeUsdMicros"`
	RefundPolicyVersionID           string `json:"refundPolicyVersionId"`
}

// deletionResult records the exact identities this deletion already obtained from
// their owners. A resume replays these identities instead of issuing a second
// retire, delete, receipt or refund.
type deletionResult struct {
	// GrantID is the accepted-operation grant of this deletion. The deletion is
	// its own accepted Workspace obligation with its own action set, so it is not
	// bound to the actor or the action surface of the original launch.
	GrantID             string          `json:"grantId,omitempty"`
	RetirementOperation json.RawMessage `json:"retirementOperation,omitempty"`
	DeleteOperation     json.RawMessage `json:"deleteOperation,omitempty"`
	ResourceReadback    json.RawMessage `json:"resourceReadback,omitempty"`
	DeletionReceipt     json.RawMessage `json:"deletionReceipt,omitempty"`
	RefundCommand       json.RawMessage `json:"refundCommand,omitempty"`
	WalletRefund        json.RawMessage `json:"walletRefund,omitempty"`
	// RefundStatus is this operation's own settlement outcome: not_applicable,
	// requested, confirmed or rejected. It never stands in for the Gateway fact.
	RefundStatus          string `json:"refundStatus,omitempty"`
	RefundAmountUSDMicros int64  `json:"refundAmountUsdMicros,omitempty"`
	DeletedAt             string `json:"deletedAt,omitempty"`
}

func decodeDeletion(op ownerstore.Operation) (deletionOrder, deletionResult, error) {
	var order deletionOrder
	var result deletionResult
	if json.Unmarshal(op.AcceptedInput, &order) != nil || json.Unmarshal(op.Result, &result) != nil {
		return order, result, status.Error(codes.DataLoss, "stored Workspace deletion is invalid")
	}
	return order, result, nil
}

// DeleteWorkspace accepts one deletion intent. The confirmation must name the
// Workspace exactly, the data-destruction acknowledgement is mandatory, and the
// intent is accepted only after re-reading the current permission. The accepted
// input freezes the original launch, provider and payment facts, and the ordered
// whole runs behind the durable operation the caller polls.
func (s *Service) DeleteWorkspace(ctx context.Context, r *api.DeleteWorkspaceRpcRequest) (*api.Operation, error) {
	c, b := r.GetContext(), r.GetBody()
	if err := ownerservice.ValidateCallContext(ctx, c); err != nil {
		return nil, err
	}
	tid := c.GetScope().GetTenant().GetTenantId()
	workspaceID := strings.TrimSpace(r.GetWorkspaceId())
	if tid == "" || workspaceID == "" || b == nil || c.GetIdempotencyKey() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant, workspace, body and idempotency key are required")
	}
	var name string
	var version int64
	err := s.Store.DB().QueryRowContext(ctx, `SELECT name,status,version FROM workspace.workspaces WHERE id=$1 AND tenant_id=$2`, workspaceID, tid).Scan(&name, new(string), &version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "Workspace not found")
	}
	if err != nil {
		return nil, dbError(err)
	}
	// A confirmation name that is not this Workspace's own name, or a missing
	// acknowledgement of data destruction, is refused before any deletion intent is
	// recorded.
	if b.GetConfirmationName() != name {
		return nil, status.Error(codes.FailedPrecondition, "the confirmation name must equal the Workspace name")
	}
	if !b.GetAcknowledgeDataDestruction() {
		return nil, status.Error(codes.FailedPrecondition, "data destruction acknowledgement is required")
	}
	if err = s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE, workspaceResource(workspaceID), tid); err != nil {
		return nil, err
	}
	normalized, err := proto.MarshalOptions{Deterministic: true}.Marshal(b)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid deletion input")
	}
	idem := ownerstore.IdempotencyInput{ID: id("idem_"), TenantScope: tid, ActorScope: c.ActorId, OperationName: "deleteWorkspace", IdempotencyKey: c.IdempotencyKey, RequestSHA256: ownerstore.HashRequestBody(normalized), ResponseStatus: 202}
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tid+"/"+c.ActorId+"/deleteWorkspace/"+c.IdempotencyKey); err != nil {
		return nil, dbError(err)
	}
	replay, found, err := s.Store.LookupIdempotency(ctx, tx, idem)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "idempotency key has different input")
	}
	if err != nil {
		return nil, dbError(err)
	}
	if found {
		tx.Rollback()
		op, err := s.Store.ReadOperation(ctx, replay.OperationID)
		if err != nil {
			return nil, dbError(err)
		}
		return deletionOperation(op)
	}
	// Re-read the Workspace under the command lock so the accepted intent and the
	// one-deletion rule are decided against the current row, not a pre-lock read.
	var currentName, currentState string
	var currentVersion int64
	err = tx.QueryRowContext(ctx, `SELECT name,status,version FROM workspace.workspaces WHERE id=$1 AND tenant_id=$2`, workspaceID, tid).Scan(&currentName, &currentState, &currentVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.NotFound, "Workspace not found")
	}
	if err != nil {
		return nil, dbError(err)
	}
	if b.GetConfirmationName() != currentName {
		return nil, status.Error(codes.FailedPrecondition, "the confirmation name must equal the Workspace name")
	}
	if !b.GetAcknowledgeDataDestruction() {
		return nil, status.Error(codes.FailedPrecondition, "data destruction acknowledgement is required")
	}
	// One Workspace has at most one deletion. A repeated intent - even under a new
	// idempotency key - is the same deletion and returns its original operation
	// instead of minting a second cleanup. An already deleted Workspace returns the
	// deletion that removed it, so a retry is idempotent rather than an error.
	var existingID, existingState string
	err = tx.QueryRowContext(ctx, `SELECT id,status FROM workspace.operations
		WHERE resource_id=$1 AND kind='delete_workspace'
		ORDER BY CASE WHEN status NOT IN ('succeeded','failed','cancelled') THEN 0 ELSE 1 END, created_at,id LIMIT 1`, workspaceID).Scan(&existingID, &existingState)
	if err == nil && existingState != "failed" && existingState != "cancelled" {
		if currentState != "deleted" || existingState == "succeeded" {
			tx.Rollback()
			op, err := s.Store.ReadOperation(ctx, existingID)
			if err != nil {
				return nil, dbError(err)
			}
			return deletionOperation(op)
		}
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, dbError(err)
	}
	if currentState == "deleted" {
		return nil, status.Error(codes.FailedPrecondition, "the Workspace is already deleted")
	}
	name, version = currentName, currentVersion
	// Revalidate after the command lock. A request queued behind another
	// transaction must not commit using a pre-lock permission.
	decisionRequest := &api.AuthorizationRequest{Scope: c.Scope, ActorId: c.ActorId, SessionId: c.SessionId, AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE, Resource: workspaceResource(workspaceID), RequestId: c.RequestId}
	decision, err := s.Identity.AuthorizeAction(ctx, decisionRequest)
	if err != nil {
		return nil, err
	}
	if err = owneridentity.ValidateDecision(decisionRequest, decision, time.Now()); err != nil || decision.GetAuthorizationContextId() == "" {
		return nil, status.Error(codes.PermissionDenied, "fresh Workspace authorization required")
	}
	order, err := s.freezeDeletion(ctx, tx, workspaceID, name, version, decision.GetAuthorizationContextId())
	if err != nil {
		return nil, err
	}
	order.Request = wire(b)
	raw, err := json.Marshal(order)
	if err != nil {
		return nil, status.Error(codes.Internal, "Workspace deletion intent cannot be encoded")
	}
	oid := id("op_")
	op, err := s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: oid, TenantID: tid, ActorID: c.ActorId, Kind: "delete_workspace", ResourceID: workspaceID, Stage: "retirement", RequestID: c.RequestId, AcceptedInput: raw})
	if err != nil {
		return nil, dbError(err)
	}
	// PostgreSQL now() is transaction-start time, which precedes the fresh
	// post-lock decision. Record the actual acceptance instant for commit proof.
	if err = tx.QueryRowContext(ctx, `UPDATE workspace.operations SET created_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1 RETURNING created_at,updated_at`, oid).Scan(&op.CreatedAt, &op.UpdatedAt); err != nil {
		return nil, dbError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE workspace.workspaces SET status='deleting',version=version+1,updated_at=now() WHERE id=$1 AND status<>'deleted'`, workspaceID); err != nil {
		return nil, dbError(err)
	}
	idem.ResourceID, idem.OperationID = workspaceID, oid
	acceptedOperation, err := deletionOperation(op)
	if err != nil {
		return nil, err
	}
	idem.ResponseBody = wire(acceptedOperation)
	if err = s.Store.RecordIdempotency(ctx, tx, idem); err != nil {
		return nil, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	// The durable intent precedes every cross-owner side effect. A lost response
	// leaves the original deletion recoverable, never a second cleanup.
	_ = s.Resume(ctx, oid)
	op, err = s.Store.ReadOperation(ctx, oid)
	if err != nil {
		return nil, dbError(err)
	}
	return deletionOperation(op)
}

// freezeDeletion binds the exact original execution and payment facts the
// deletion will act on. The launch must have delivered a runtime and a confirmed
// resource set, and a paid order must still carry its original confirmed charge;
// an order whose funding evidence is missing is refused instead of being deleted
// without a refundable original.
func (s *Service) freezeDeletion(ctx context.Context, tx *sql.Tx, workspaceID, name string, version int64, authorizationContextID string) (deletionOrder, error) {
	var launchID string
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT id,COALESCE(result,'{}'::jsonb) FROM workspace.operations
		WHERE resource_id=$1 AND kind='create_workspace' AND status='succeeded'
		ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID).Scan(&launchID, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return deletionOrder{}, status.Error(codes.FailedPrecondition, "the Workspace has no delivered launch to delete")
	}
	if err != nil {
		return deletionOrder{}, dbError(err)
	}
	var launched orderResult
	if json.Unmarshal(raw, &launched) != nil {
		return deletionOrder{}, status.Error(codes.DataLoss, "stored Workspace launch result is invalid")
	}
	if strings.TrimSpace(launched.GrantID) == "" || strings.TrimSpace(launched.ResourceSetID) == "" || len(launched.RuntimeCommand) == 0 {
		return deletionOrder{}, status.Error(codes.FailedPrecondition, "the Workspace has no delivered runtime to delete")
	}
	command := &api.RuntimeDeployCommand{}
	if protojson.Unmarshal(launched.RuntimeCommand, command) != nil || strings.TrimSpace(command.GetRuntimeInstanceId()) == "" || strings.TrimSpace(command.GetDeploymentId()) == "" || command.GetWorkspaceId() != workspaceID {
		return deletionOrder{}, status.Error(codes.DataLoss, "stored Workspace runtime command is invalid")
	}
	dataAttachmentID := ""
	if len(launched.ResourceReadback) > 0 {
		readback := &api.ResourceReadback{}
		if protojson.Unmarshal(launched.ResourceReadback, readback) != nil || readback.GetWorkspaceId() != workspaceID {
			return deletionOrder{}, status.Error(codes.DataLoss, "stored Workspace resource readback is invalid")
		}
		dataAttachmentID = readback.GetExecutionResources().GetDataAttachmentId()
	}
	var periodID string
	var periodStart time.Time
	var chargeOperation, chargeReceipt sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT p.id,p.period_start,p.charge_wallet_operation_id,p.charge_receipt_id,p.accepted_quote_snapshot
		FROM workspace.subscriptions s JOIN workspace.subscription_periods p ON p.subscription_id=s.id
		WHERE s.workspace_id=$1 ORDER BY p.period_start,p.id LIMIT 1`, workspaceID).Scan(&periodID, &periodStart, &chargeOperation, &chargeReceipt, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return deletionOrder{}, status.Error(codes.FailedPrecondition, "the Workspace has no confirmed paid period to settle")
	}
	if err != nil {
		return deletionOrder{}, dbError(err)
	}
	accepted := &api.QuoteAcceptance{}
	if protojson.Unmarshal(raw, accepted) != nil || accepted.GetQuote().GetId() == "" {
		return deletionOrder{}, status.Error(codes.DataLoss, "stored subscription period evidence is invalid")
	}
	amount := accepted.GetQuote().GetTotalUsdMicros()
	if amount > 0 && (!chargeOperation.Valid || !chargeReceipt.Valid || strings.TrimSpace(chargeOperation.String) == "" || strings.TrimSpace(chargeReceipt.String) == "") {
		return deletionOrder{}, status.Error(codes.FailedPrecondition, "the original charge evidence is incomplete")
	}
	order := deletionOrder{
		AuthorizationContextID: authorizationContextID,
		WorkspaceName:          name, WorkspaceVersion: version,
		LaunchOperationID: launchID, GrantID: launched.GrantID,
		RuntimeInstanceID: command.GetRuntimeInstanceId(), DeploymentID: command.GetDeploymentId(), DataAttachmentID: dataAttachmentID,
		ResourceSetID:        launched.ResourceSetID,
		SubscriptionPeriodID: periodID, PeriodStart: periodStart.UTC().Format(time.RFC3339Nano),
		OriginalChargeWalletOperationID: chargeOperation.String, OriginalChargeReceiptID: chargeReceipt.String,
		OriginalChargeUSDMicros: amount, RefundPolicyVersionID: contracts.WorkspaceRefundPolicyVersion,
	}
	material, err := json.Marshal(struct {
		Request json.RawMessage `json:"request"`
		Launch  string          `json:"launchOperationId"`
		Charge  string          `json:"chargeWalletOperationId"`
	}{Request: wire(&api.DeleteWorkspaceRequest{ConfirmationName: name, AcknowledgeDataDestruction: true}), Launch: launchID, Charge: chargeOperation.String})
	if err != nil {
		return deletionOrder{}, status.Error(codes.Internal, "Workspace deletion intent cannot be encoded")
	}
	order.InputDigest = "sha256:" + ownerstore.HashRequestBody(material)
	return order, nil
}

// GetWorkspaceDeletion is the owner readback of the deletion and its refund. The
// three facts stay separate: resources, data and money each report their own
// owner-confirmed state, so a deleted Workspace is never presented as refunded.
func (s *Service) GetWorkspaceDeletion(ctx context.Context, r *api.GetWorkspaceDeletionRpcRequest) (*api.WorkspaceDeletion, error) {
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	workspaceID := strings.TrimSpace(r.GetWorkspaceId())
	if workspaceID == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace id is required")
	}
	var tenant string
	if err := s.Store.DB().QueryRowContext(ctx, `SELECT tenant_id FROM workspace.workspaces WHERE id=$1`, workspaceID).Scan(&tenant); err != nil {
		return nil, dbError(err)
	}
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEDELETION, workspaceResource(workspaceID), tenant); err != nil {
		return nil, err
	}
	out := &api.WorkspaceDeletion{WorkspaceId: workspaceID, ResourceDeletionStatus: api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_PENDING, DataDeletionStatus: api.WorkspaceDeletionDataDeletionStatusEnum_WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_PENDING, RefundStatus: api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_UNKNOWN}
	var id, state, stage, observation string
	var raw []byte
	var opUpdated time.Time
	err := s.Store.DB().QueryRowContext(ctx, `SELECT id,status,stage,COALESCE(observation_result,''),COALESCE(result,'{}'::jsonb),updated_at
		FROM workspace.operations WHERE resource_id=$1 AND kind='delete_workspace' ORDER BY created_at DESC,id DESC LIMIT 1`, workspaceID).Scan(&id, &state, &stage, &observation, &raw, &opUpdated)
	if errors.Is(err, sql.ErrNoRows) {
		// The deletion readback answers about one accepted deletion. With no
		// deletion accepted yet, every required field would be invented, so the read
		// reports the absence itself and the caller shows the deletion entry point.
		return nil, status.Error(codes.NotFound, "the Workspace has no accepted deletion")
	}
	if err != nil {
		return nil, dbError(err)
	}
	var result deletionResult
	if json.Unmarshal(raw, &result) != nil {
		return nil, status.Error(codes.DataLoss, "stored Workspace deletion result is invalid")
	}
	// resourceFact distinguishes a confirmed absence from a resource Fabric
	// confirmed is still present, and from an unanswered readback. The three are
	// different customer-visible states, so a present resource never reports as an
	// unknown provider and an unanswered one never reports as progress.
	resourceFact := "none"
	if len(result.ResourceReadback) > 0 {
		readback := &api.ResourceReadback{}
		if protojson.Unmarshal(result.ResourceReadback, readback) != nil || readback.GetWorkspaceId() != workspaceID {
			return nil, status.Error(codes.DataLoss, "stored Workspace deletion readback is invalid")
		}
		switch {
		case readback.GetOutcome() == api.Observation_OBSERVATION_CONFIRMED && readback.GetAbsenceConfirmed():
			resourceFact = "absent"
		case readback.GetOutcome() == api.Observation_OBSERVATION_CONFIRMED:
			resourceFact = "present"
		default:
			resourceFact = "unknown"
		}
	}
	resource, data := deletionStageStatus(state, observation, resourceFact)
	out.OperationId = id
	out.ResourceDeletionStatus, out.DataDeletionStatus = resource, data
	switch result.RefundStatus {
	case "confirmed":
		out.RefundStatus = api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED
	case "rejected":
		out.RefundStatus = api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_REJECTED
	case "requested":
		out.RefundStatus = api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_REQUESTED
	case "not_applicable":
		out.RefundStatus = api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_NOT_APPLICABLE
	}
	if len(result.WalletRefund) > 0 {
		refund := &api.WalletOperation{}
		if protojson.Unmarshal(result.WalletRefund, refund) != nil {
			return nil, status.Error(codes.DataLoss, "stored Workspace refund is invalid")
		}
		if refund.GetId() != "" {
			out.RefundOperationId = proto.String(refund.GetId())
		}
	}
	out.UpdatedAt = timestamppb.New(opUpdated)
	return out, nil
}

// deletionStageStatus maps one deletion row onto the customer-visible resource and
// data facts. Money is answered separately, and an unanswered provider can never
// be presented as a confirmed deletion.
func deletionStageStatus(state, observation, resourceFact string) (api.WorkspaceDeletionResourceDeletionStatusEnum, api.WorkspaceDeletionDataDeletionStatusEnum) {
	switch {
	case state == "succeeded", resourceFact == "absent":
		return api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_CONFIRMED, api.WorkspaceDeletionDataDeletionStatusEnum_WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_CONFIRMED
	case observation == "rejected":
		return api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_REJECTED, api.WorkspaceDeletionDataDeletionStatusEnum_WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_REJECTED
	case resourceFact == "unknown", resourceFact == "none" && observation == "unknown":
		return api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_UNKNOWN, api.WorkspaceDeletionDataDeletionStatusEnum_WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_UNKNOWN
	default:
		return api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_PENDING, api.WorkspaceDeletionDataDeletionStatusEnum_WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_PENDING
	}
}

// deletionOperation projects one owner-local delete_workspace row as the typed
// Operation the caller polls.
func deletionOperation(o ownerstore.Operation) (*api.Operation, error) {
	stage, ok := api.OperationStageEnum_value["OPERATION_STAGE_ENUM_"+strings.ToUpper(o.Stage)]
	if !ok || stage == 0 {
		return nil, status.Error(codes.Internal, "stored Workspace deletion stage is invalid")
	}
	state, ok := api.OperationStatusEnum_value["OPERATION_STATUS_ENUM_"+strings.ToUpper(o.Status)]
	if !ok || state == 0 {
		return nil, status.Error(codes.Internal, "stored Workspace deletion status is invalid")
	}
	out := &api.Operation{OperationId: o.ID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_DELETE_WORKSPACE, ResourceId: o.ResourceID, Status: api.OperationStatusEnum(state), Stage: api.OperationStageEnum(stage), RequestId: o.RequestID, CreatedAt: timestamppb.New(o.CreatedAt), UpdatedAt: timestamppb.New(o.UpdatedAt)}
	if o.Observation != "" {
		observation, ok := api.OperationObservationResultEnum_value["OPERATION_OBSERVATION_RESULT_ENUM_"+strings.ToUpper(o.Observation)]
		if !ok || observation == 0 {
			return nil, status.Error(codes.Internal, "stored Workspace deletion observation is invalid")
		}
		v := api.OperationObservationResultEnum(observation)
		out.ObservationResult = &v
	}
	if !o.Terminal() {
		out.PollAfterSeconds = proto.Int32(5)
	}
	return out, nil
}

// deletionEvidence is this deletion's own owner commit: the accepted confirmation
// input, the original launch and charge it froze, and the delete action it
// accepted. Ledger re-reads it before it records the confirmed deletion.
func deletionEvidence(op ownerstore.Operation) (*api.OwnerCommitEvidence, error) {
	order, _, err := decodeDeletion(op)
	if err != nil {
		return nil, err
	}
	return &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: op.ID, ResourceId: op.ResourceID, AcceptedInputDigest: order.InputDigest, CommittedVersion: 1, AcceptedAt: timestamppb.New(op.CreatedAt), AuthorizationContextId: order.AuthorizationContextID, ActorId: op.ActorID, Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: op.TenantID}}}, AcceptedAction: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE, AuthorizationResource: workspaceResource(op.ResourceID), ContinuationResources: []*api.AuthorizationResource{workspaceResource(op.ResourceID)}}, nil
}

// resumeDeletion continues one committed deletion under its own worker lease. Each
// step runs against the frozen original identities and advances only on the owning
// provider's confirmed readback.
func (s *Service) resumeDeletion(ctx context.Context, operationID string) error {
	ctx, cancel := context.WithTimeout(ctx, resumeTimeout)
	defer cancel()
	op, err := s.Store.ReadOperation(ctx, operationID)
	if err != nil {
		return dbError(err)
	}
	if op.Terminal() {
		return nil
	}
	if op.Kind != "delete_workspace" {
		return status.Error(codes.FailedPrecondition, "operation is not a Workspace deletion")
	}
	token := id("lease_")
	var leased string
	err = s.Store.DB().QueryRowContext(ctx, `UPDATE workspace.operations SET worker_lease_token=$2,worker_lease_until=now()+interval '60 seconds'
		WHERE id=$1 AND status NOT IN ('succeeded','failed','cancelled') AND (worker_lease_until IS NULL OR worker_lease_until<now()) RETURNING id`, operationID, token).Scan(&leased)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return dbError(err)
	}
	defer func() {
		releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer releaseCancel()
		_, _ = s.Store.DB().ExecContext(releaseCtx, `UPDATE workspace.operations SET worker_lease_token=NULL,worker_lease_until=NULL WHERE id=$1 AND worker_lease_token=$2`, operationID, token)
	}()
	op, err = s.Store.ReadOperation(ctx, operationID)
	if err != nil {
		return dbError(err)
	}
	order, result, err := decodeDeletion(op)
	if err != nil {
		return err
	}
	if err = s.ensureDeletionGrant(ctx, op, token, &result); err != nil {
		return err
	}
	retired, err := s.retireDeletedRuntime(ctx, op, token, order, &result)
	if err != nil || !retired {
		return err
	}
	absent, err := s.confirmDeletedResources(ctx, op, token, order, &result)
	if err != nil || !absent {
		return err
	}
	receiptID, err := s.recordDeletionReceipt(ctx, op, token, order, &result)
	if err != nil || receiptID == "" {
		return err
	}
	if err = s.refundConfirmedDeletion(ctx, op, token, order, &result, receiptID); err != nil {
		return err
	}
	return s.completeDeletion(ctx, op, token, &result)
}

// ensureDeletionGrant issues the accepted-operation grant this deletion owns. The
// deletion's own owner commit names DELETEWORKSPACE, so CloudIdentity binds the
// grant to the actor and scope that accepted this deletion rather than to the
// original order's. A replay re-reads the recorded grant instead of issuing a
// second one, and a grant that does not identify the deletion is refused rather
// than used.
func (s *Service) ensureDeletionGrant(ctx context.Context, op ownerstore.Operation, token string, result *deletionResult) error {
	if result.GrantID != "" {
		return nil
	}
	if s.Identity == nil {
		return status.Error(codes.Unavailable, "Workspace deletion requires the CloudIdentity authorization owner")
	}
	commit, err := deletionEvidence(op)
	if err != nil {
		return err
	}
	request := &api.AcceptedOperationGrantRequest{AuthorizationContextId: commit.GetAuthorizationContextId(), OwnerCommitEvidence: commit, AllowedActions: deletionContinuationActions}
	if err = s.beginStep(ctx, op, token, "grant", 0, "tenant", "identity_verification", request); err != nil {
		return err
	}
	grant, err := s.Identity.IssueAcceptedOperationGrant(ctx, request)
	if err != nil {
		return s.failedDeletionCall(ctx, op, token, "grant", "identity_verification", *result, err)
	}
	if grant.GetId() == "" || grant.GetAcceptedOperationId() != op.ID || grant.GetResourceId() != op.ResourceID ||
		grant.GetAcceptedOperationOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		grant.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE ||
		grant.GetActorId() != op.ActorID || !proto.Equal(grant.GetScope(), commit.GetScope()) {
		return s.failedDeletionCall(ctx, op, token, "grant", "identity_verification", *result, status.Error(codes.DataLoss, "accepted grant does not identify the Workspace deletion"))
	}
	result.GrantID = grant.GetId()
	return s.checkpointDeletion(ctx, op, token, "grant", "retirement", "running", "confirmed", grant.GetId(), "", *result)
}

// failedDeletionCall records the outcome of one failed deletion step. A
// transport loss leaves an unknown provider outcome the worker must resolve
// rather than a fabricated success.
func (s *Service) failedDeletionCall(ctx context.Context, op ownerstore.Operation, token, step, stage string, result deletionResult, cause error) error {
	state, observation, code := callFailure(step, cause)
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.checkpointDeletion(writeCtx, op, token, step, stage, state, observation, "", code, result); err != nil {
		return err
	}
	return cause
}

// checkpointDeletion persists one deletion result under the same worker lease as
// every other step.
func (s *Service) checkpointDeletion(ctx context.Context, op ownerstore.Operation, token, step, stage, state, observation, ownerRef, code string, result deletionResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return status.Error(codes.Internal, "Workspace deletion result cannot be encoded")
	}
	return s.checkpointBytes(ctx, op, token, step, stage, state, observation, observation, ownerRef, code, code, raw)
}

// retireDeletedRuntime stops the one runtime this deletion froze. Serve's own
// operation is the confirmation, and a replay re-reads the recorded operation
// instead of issuing a second stop.
func (s *Service) retireDeletedRuntime(ctx context.Context, op ownerstore.Operation, token string, order deletionOrder, result *deletionResult) (bool, error) {
	if len(result.RetirementOperation) > 0 {
		stored := &api.Operation{}
		if protojson.Unmarshal(result.RetirementOperation, stored) != nil {
			return false, status.Error(codes.DataLoss, "stored runtime retirement is invalid")
		}
		return true, validateRetirement(op, order, stored)
	}
	if s.Serve == nil {
		// Retiring the application runtime belongs to Serve. Without that owner the
		// deletion cannot retire anything, so it waits here instead of advancing
		// Fabric to release resources the application still uses.
		if err := s.checkpointDeletion(ctx, op, token, "retire_runtime", "retirement", "awaiting_confirmation", "unknown", "", "DEPENDENCY_UNAVAILABLE", *result); err != nil {
			return false, err
		}
		return false, nil
	}
	request := &api.RuntimeStopCommand{Context: continuation(op, result.GrantID, "retire_runtime"), RuntimeInstanceId: order.RuntimeInstanceID, DeploymentId: order.DeploymentID, RetainedDataAttachmentId: order.DataAttachmentID}
	if err := s.beginStep(ctx, op, token, "retire_runtime", 0, "serve", "retirement", request); err != nil {
		return false, err
	}
	retired, err := s.Serve.Retire(ctx, request)
	if err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "retire_runtime", "retirement", *result, err)
	}
	if err = validateRetirement(op, order, retired); err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "retire_runtime", "retirement", *result, err)
	}
	result.RetirementOperation = wire(retired)
	if err = s.checkpointDeletion(ctx, op, token, "retire_runtime", "retirement", "running", "confirmed", retired.GetOperationId(), "", *result); err != nil {
		return false, err
	}
	return true, nil
}

// validateRetirement binds Serve's retire operation to the exact frozen runtime
// and deployment. A different runtime, a different owner or an unconfirmed
// operation is not this deletion's retirement.
func validateRetirement(op ownerstore.Operation, order deletionOrder, retired *api.Operation) error {
	if retired.GetOperationId() == "" || retired.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE || retired.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_RETIRE ||
		retired.GetResourceId() != order.RuntimeInstanceID || retired.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED ||
		retired.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		return status.Error(codes.DataLoss, "Serve did not confirm the original runtime retirement")
	}
	return nil
}

// confirmDeletedResources removes the frozen original resource set and then proves
// absence from Fabric's own readback. The delete request alone never advances the
// deletion: only a readback that confirms every original resource absent does.
func (s *Service) confirmDeletedResources(ctx context.Context, op ownerstore.Operation, token string, order deletionOrder, result *deletionResult) (bool, error) {
	if s.Fabric == nil {
		return false, nil
	}
	if len(result.DeleteOperation) == 0 {
		// The resource set's identity is frozen when the deletion is accepted; its
		// version is not. Fabric compares the version it is given with the one it
		// holds now, so the deletion reads it immediately before releasing rather
		// than sending a version observed when the order was launched. A set that
		// changed since then is still the same frozen identity, and a stale version
		// would otherwise refuse the deletion forever instead of describing it.
		observed, err := s.Fabric.ReadResources(ctx, &api.ResourceReadbackRequest{Context: continuation(op, result.GrantID, "read_resources_before_delete"), ResourceSetId: order.ResourceSetID})
		if err != nil {
			return false, s.failedDeletionCall(ctx, op, token, "delete_resources", "attachment_deletion", *result, err)
		}
		if observed.GetResourceSetId() != order.ResourceSetID || observed.GetWorkspaceId() != op.ResourceID {
			return false, s.failedDeletionCall(ctx, op, token, "delete_resources", "attachment_deletion", *result, status.Error(codes.DataLoss, "Fabric readback does not identify the original resources"))
		}
		request := &api.MutateResourcesCommand{Context: continuation(op, result.GrantID, "delete_resources"), WorkspaceId: op.ResourceID, ResourceSetId: order.ResourceSetID, ExpectedResourceVersion: observed.GetResourceVersion()}
		if err := s.beginStep(ctx, op, token, "delete_resources", 1, "fabric", "attachment_deletion", request); err != nil {
			return false, err
		}
		deleted, err := s.Fabric.DeleteResources(ctx, request)
		if err != nil {
			return false, s.failedDeletionCall(ctx, op, token, "delete_resources", "attachment_deletion", *result, err)
		}
		if err = validateResourceDeletion(op, order, deleted); err != nil {
			return false, s.failedDeletionCall(ctx, op, token, "delete_resources", "attachment_deletion", *result, err)
		}
		result.DeleteOperation = wire(deleted)
		// The delete request is recorded but absence is not proven yet, so the step
		// stays unknown until Fabric's own readback confirms it.
		if err = s.checkpointDeletion(ctx, op, token, "delete_resources", "attachment_deletion", "running", "unknown", deleted.GetOperationId(), "DEPENDENCY_UNAVAILABLE", *result); err != nil {
			return false, err
		}
	}
	request := &api.ResourceReadbackRequest{Context: continuation(op, result.GrantID, "read_resources"), ResourceSetId: order.ResourceSetID}
	if err := s.beginStep(ctx, op, token, "read_resources", 2, "fabric", "storage_deletion", request); err != nil {
		return false, err
	}
	readback, err := s.Fabric.ReadResources(ctx, request)
	if err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "read_resources", "storage_deletion", *result, err)
	}
	if readback.GetResourceSetId() != order.ResourceSetID || readback.GetWorkspaceId() != op.ResourceID {
		return false, s.failedDeletionCall(ctx, op, token, "read_resources", "storage_deletion", *result, status.Error(codes.DataLoss, "Fabric readback does not identify the original resources"))
	}
	result.ResourceReadback = wire(readback)
	switch {
	case readback.GetOutcome() == api.Observation_OBSERVATION_REJECTED:
		return false, s.checkpointDeletion(ctx, op, token, "read_resources", "storage_deletion", "needs_attention", "rejected", order.ResourceSetID, readback.GetErrorCode(), *result)
	case readback.GetOutcome() == api.Observation_OBSERVATION_CONFIRMED && readback.GetAbsenceConfirmed():
		// The deletion instant is frozen the moment absence is confirmed, before
		// any refund is computed, so a retry of the refund settles the same hour
		// bucket instead of re-deriving a different amount.
		if result.DeletedAt == "" {
			result.DeletedAt = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if err = s.checkpointDeletion(ctx, op, token, "read_resources", "compute_deletion", "running", "confirmed", order.ResourceSetID, "", *result); err != nil {
			return false, err
		}
		return true, nil
	default:
		// A present or unanswered resource is a wait, never an absence.
		if err = s.checkpointDeletion(ctx, op, token, "read_resources", "storage_deletion", "awaiting_confirmation", "unknown", order.ResourceSetID, "DEPENDENCY_UNAVAILABLE", *result); err != nil {
			return false, err
		}
		return false, nil
	}
}

// validateResourceDeletion binds Fabric's delete operation to the original
// resource set. The operation is an intent; absence is proven separately.
func validateResourceDeletion(op ownerstore.Operation, order deletionOrder, deleted *api.Operation) error {
	if deleted.GetOperationId() == "" || deleted.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC {
		return status.Error(codes.DataLoss, "Fabric did not accept the original resource deletion")
	}
	if deleted.GetResourceId() != order.ResourceSetID && deleted.GetResourceId() != op.ResourceID {
		return status.Error(codes.DataLoss, "Fabric deletion names a different resource set")
	}
	if deleted.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_FAILED || deleted.GetObservationResult() == api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED {
		return status.Error(codes.FailedPrecondition, "Fabric refused the original resource deletion")
	}
	return nil
}

// recordDeletionReceipt records the confirmed deletion evidence in Ledger and
// reads it back by the original owner reference, so the refund is issued against
// evidence that really exists.
func (s *Service) recordDeletionReceipt(ctx context.Context, op ownerstore.Operation, token string, order deletionOrder, result *deletionResult) (string, error) {
	if len(result.DeletionReceipt) > 0 {
		stored := &api.Receipt{}
		if protojson.Unmarshal(result.DeletionReceipt, stored) != nil {
			return "", status.Error(codes.DataLoss, "stored deletion receipt is invalid")
		}
		return stored.GetId(), validateDeletionReceipt(op, stored)
	}
	if s.Ledger == nil {
		// The confirmed deletion is recorded by its evidence owner before any refund
		// is issued. Without Ledger the absence evidence stays pending rather than
		// settling money against a receipt that does not exist.
		if err := s.checkpointDeletion(ctx, op, token, "deletion_receipt", "deletion_evidence", "awaiting_confirmation", "unknown", "", "DEPENDENCY_UNAVAILABLE", *result); err != nil {
			return "", err
		}
		return "", nil
	}
	commit, err := deletionEvidence(op)
	if err != nil {
		return "", err
	}
	request := &api.AppendReceiptRequest{
		Context:                continuation(op, result.GrantID, "deletion_receipt"),
		Receipt:                &api.Receipt{Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(op.ID), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, EvidenceSummary: "Workspace deletion confirmed: the original application runtime and provider resources are absent."},
		EvidenceDigest:         order.InputDigest,
		OwnerEvidenceReference: op.ID,
		OwnerCommitEvidence:    commit,
	}
	if err = s.beginStep(ctx, op, token, "deletion_receipt", 3, "ledger", "deletion_evidence", request); err != nil {
		return "", err
	}
	appended, err := s.Ledger.AppendReceipt(ctx, request)
	if err != nil {
		return "", s.failedDeletionCall(ctx, op, token, "deletion_receipt", "deletion_evidence", *result, err)
	}
	if err = validateDeletionReceipt(op, appended); err != nil {
		return "", s.failedDeletionCall(ctx, op, token, "deletion_receipt", "deletion_evidence", *result, err)
	}
	readback, err := s.Ledger.ReadReceiptByReference(ctx, &api.GetReceiptByReferenceRequest{Context: continuation(op, result.GrantID, "read_deletion_receipt"), Owner: "workspace", OwnerEvidenceReference: op.ID})
	if err != nil {
		return "", s.failedDeletionCall(ctx, op, token, "read_deletion_receipt", "deletion_evidence", *result, err)
	}
	if !proto.Equal(appended, readback) {
		return "", s.failedDeletionCall(ctx, op, token, "read_deletion_receipt", "deletion_evidence", *result, status.Error(codes.DataLoss, "Ledger returned a different deletion receipt"))
	}
	result.DeletionReceipt = wire(readback)
	if err = s.checkpointDeletion(ctx, op, token, "deletion_receipt", "deletion_evidence", "running", "confirmed", readback.GetId(), "", *result); err != nil {
		return "", err
	}
	return readback.GetId(), nil
}

// validateDeletionReceipt binds the recorded evidence to this deletion.
func validateDeletionReceipt(op ownerstore.Operation, receipt *api.Receipt) error {
	if receipt.GetId() == "" || receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		receipt.GetOperationId() != op.ID || receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetCreatedAt() == nil || receipt.CreatedAt.CheckValid() != nil {
		return status.Error(codes.DataLoss, "Ledger deletion receipt does not identify the original deletion")
	}
	return nil
}

// refundConfirmedDeletion settles the original platform charge for the confirmed
// deletion. The amount is the approved platform policy computed from the frozen
// original charge and period, and an unknown refund is read back by its original
// command instead of being re-issued. A deletion with nothing to refund records
// that explicitly instead of inventing a zero-value wallet action.
func (s *Service) refundConfirmedDeletion(ctx context.Context, op ownerstore.Operation, token string, order deletionOrder, result *deletionResult, receiptID string) error {
	if result.DeletedAt == "" {
		result.DeletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if strings.TrimSpace(receiptID) == "" {
		// A settlement always names the confirmed deletion evidence it is issued
		// against, so an absent receipt is a state error rather than an entitlement.
		return status.Error(codes.DataLoss, "Workspace deletion reached settlement without its confirmed deletion receipt")
	}
	if order.OriginalChargeUSDMicros <= 0 {
		// A Local no-charge order has no original platform charge to settle, so its
		// deletion has no refund entitlement.
		result.RefundStatus, result.RefundAmountUSDMicros = "not_applicable", 0
		return s.checkpointDeletion(ctx, op, token, "refund_confirmed_deletion", "refund", "running", "confirmed", "", "", *result)
	}
	if s.Gateway == nil {
		// The wallet authority is not configured on this deployment, so the original
		// charge cannot be settled yet. The deletion stays awaiting its funding owner
		// instead of reporting a refund that was never issued.
		if err := s.checkpointDeletion(ctx, op, token, "refund_confirmed_deletion", "refund", "awaiting_confirmation", "unknown", "", "DEPENDENCY_UNAVAILABLE", *result); err != nil {
			return err
		}
		return nil
	}
	fulfilledAt, err := time.Parse(time.RFC3339Nano, order.PeriodStart)
	if err != nil {
		return status.Error(codes.DataLoss, "stored original period start is invalid")
	}
	deletedAt, err := time.Parse(time.RFC3339Nano, result.DeletedAt)
	if err != nil {
		return status.Error(codes.DataLoss, "stored deletion time is invalid")
	}
	amount, err := contracts.PlatformWorkspaceDeleteRefundMicros(order.OriginalChargeUSDMicros, fulfilledAt, deletedAt)
	if err != nil {
		return s.failedDeletionCall(ctx, op, token, "refund_confirmed_deletion", "refund", *result, err)
	}
	if amount == 0 {
		result.RefundStatus, result.RefundAmountUSDMicros = "not_applicable", 0
		return s.checkpointDeletion(ctx, op, token, "refund_confirmed_deletion", "refund", "running", "confirmed", "", "", *result)
	}
	command := &api.WalletRefundCommand{Context: continuation(op, result.GrantID, "refund_confirmed_deletion"), WorkspaceId: op.ResourceID, OriginalWalletOperationId: order.OriginalChargeWalletOperationID, AmountUsdMicros: amount, RefundPolicyVersionId: order.RefundPolicyVersionID, ConfirmedDeletionReceiptId: receiptID, SubscriptionPeriodId: order.SubscriptionPeriodID}
	if len(result.WalletRefund) > 0 {
		stored := &api.WalletOperation{}
		if protojson.Unmarshal(result.WalletRefund, stored) != nil {
			return status.Error(codes.DataLoss, "stored Workspace refund is invalid")
		}
		if err = validateWalletRefund(op, amount, stored); err != nil {
			return status.Error(codes.DataLoss, "stored Workspace refund does not identify the original settlement")
		}
		if len(result.RefundCommand) > 0 {
			original := &api.WalletRefundCommand{}
			if protojson.Unmarshal(result.RefundCommand, original) != nil || !proto.Equal(original, command) {
				return status.Error(codes.DataLoss, "stored refund command differs from its original execution")
			}
		}
		if walletRefundSettled(stored.GetStatus()) {
			return s.settleRefund(ctx, op, token, order, result, stored)
		}
		// The wallet owner recorded this refund without a terminal answer, so the
		// original action may already have moved money. Its own recorded answer is
		// read back by the original command instead of issuing a second refund, and
		// only that readback may settle the deletion.
		if err = s.beginStep(ctx, op, token, "read_refund_confirmed_deletion", 4, "gateway", "refund", &api.WalletReadbackRequest{Context: command.GetContext(), OriginalIdempotencyKey: command.GetContext().GetIdempotencyKey()}); err != nil {
			return err
		}
		observed, readErr := s.Gateway.ReadWalletAction(ctx, &api.WalletReadbackRequest{Context: command.GetContext(), OriginalIdempotencyKey: command.GetContext().GetIdempotencyKey()})
		if readErr != nil {
			return s.failedDeletionCall(ctx, op, token, "read_refund_confirmed_deletion", "refund", *result, readErr)
		}
		if err = validateWalletRefund(op, amount, observed); err != nil {
			return s.failedDeletionCall(ctx, op, token, "read_refund_confirmed_deletion", "refund", *result, err)
		}
		result.WalletRefund = wire(observed)
		return s.settleRefund(ctx, op, token, order, result, observed)
	}
	result.RefundCommand = wire(command)
	if err = s.beginStep(ctx, op, token, "refund_confirmed_deletion", 4, "gateway", "refund", command); err != nil {
		return err
	}
	refunded, err := s.Gateway.Refund(ctx, command)
	if err != nil {
		return s.failedDeletionCall(ctx, op, token, "refund_confirmed_deletion", "refund", *result, err)
	}
	if err = validateWalletRefund(op, amount, refunded); err != nil {
		return s.failedDeletionCall(ctx, op, token, "refund_confirmed_deletion", "refund", *result, err)
	}
	result.WalletRefund = wire(refunded)
	return s.settleRefund(ctx, op, token, order, result, refunded)
}

// settleRefund records what the wallet authority answered for the original
// charge. Only a confirmed refund completes the deletion; a refusal is explicit,
// and any other answer stays an unknown settlement a later pass reads back.
func (s *Service) settleRefund(ctx context.Context, op ownerstore.Operation, token string, order deletionOrder, result *deletionResult, refunded *api.WalletOperation) error {
	result.RefundAmountUSDMicros = refunded.GetAmountUsdMicros()
	switch refunded.GetStatus() {
	case api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED:
		result.RefundStatus = "confirmed"
		return s.checkpointDeletion(ctx, op, token, "refund_confirmed_deletion", "refund", "running", "confirmed", refunded.GetId(), "", *result)
	case api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REJECTED:
		result.RefundStatus = "rejected"
		if !validErrorCode(refunded.GetErrorCode()) {
			return s.failedDeletionCall(ctx, op, token, "refund_confirmed_deletion", "refund", *result, status.Error(codes.DataLoss, "rejected refund names no cause"))
		}
		return s.checkpointDeletion(ctx, op, token, "refund_confirmed_deletion", "refund", "needs_attention", "rejected", refunded.GetId(), errorCodeText(refunded.GetErrorCode()), *result)
	default:
		result.RefundStatus = "requested"
		return s.checkpointDeletion(ctx, op, token, "refund_confirmed_deletion", "refund", "awaiting_confirmation", "unknown", refunded.GetId(), "DEPENDENCY_UNAVAILABLE", *result)
	}
}

// walletRefundSettled reports whether a wallet answer is terminal for this
// settlement. A request or unknown answer is the wallet owner's own recorded
// intent and is resolved by readback, never by a second refund.
func walletRefundSettled(status api.WalletOperationStatusEnum) bool {
	return status == api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED || status == api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REJECTED
}

// validateWalletRefund binds one refund to the original charge amount and this
// Workspace.
func validateWalletRefund(op ownerstore.Operation, amount int64, refunded *api.WalletOperation) error {
	if refunded.GetId() == "" || refunded.GetWorkspaceId() != op.ResourceID || refunded.GetKind() != api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND || refunded.GetAmountUsdMicros() != amount {
		return status.Error(codes.DataLoss, "wallet refund does not identify the original deletion settlement")
	}
	return nil
}

// completeDeletion marks the Workspace deleted only after the resource absence
// and the refund settlement are both resolved. The operation and the Workspace
// row are written in one owner transaction, so no reader sees a succeeded
// deletion whose Workspace is still active.
func (s *Service) completeDeletion(ctx context.Context, op ownerstore.Operation, token string, result *deletionResult) error {
	if result.RefundStatus != "confirmed" && result.RefundStatus != "not_applicable" {
		return nil
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return status.Error(codes.Internal, "Workspace deletion result cannot be encoded")
	}
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workspace.operations SET result=$3,status='succeeded',stage='succeeded',observation_result='confirmed',error_code=NULL,updated_at=now(),completed_at=now()
		WHERE id=$1 AND worker_lease_token=$2 AND worker_lease_until>now() AND status NOT IN ('succeeded','failed','cancelled')`, op.ID, token, raw)
	if err = fenced(res, err); err != nil {
		return err
	}
	res, err = tx.ExecContext(ctx, `UPDATE workspace.workspaces SET status='deleted',deleted_at=COALESCE(deleted_at,now()),version=version+1,updated_at=now() WHERE id=$1 AND status<>'deleted'`, op.ResourceID)
	if err = fenced(res, err); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return dbError(err)
	}
	return nil
}
