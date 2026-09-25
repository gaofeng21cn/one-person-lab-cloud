package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// A pass is shorter than its SQL lease. Every persisted checkpoint is fenced
// by the same token and expiry; a late worker cannot overwrite its successor.
const resumeTimeout = 45 * time.Second

func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.RunOnce(ctx); err != nil && ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (s *Service) RunOnce(ctx context.Context) error {
	rows, err := s.Store.DB().QueryContext(ctx, `SELECT o.id FROM workspace.operations o
		WHERE o.kind='create_workspace' AND o.status IN ('accepted','running','awaiting_confirmation','needs_attention')
		AND (o.worker_lease_until IS NULL OR o.worker_lease_until<now())
		AND NOT EXISTS (SELECT 1 FROM workspace.saga_steps st WHERE st.operation_id=o.id AND st.next_attempt_at>now())
		ORDER BY o.updated_at,o.id LIMIT 20`)
	if err != nil {
		return dbError(err)
	}
	var ids []string
	for rows.Next() {
		var value string
		if err = rows.Scan(&value); err != nil {
			rows.Close()
			return dbError(err)
		}
		ids = append(ids, value)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return dbError(err)
	}
	var failures []error
	for _, operationID := range ids {
		if err = s.Resume(ctx, operationID); err != nil {
			failures = append(failures, err)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	return errors.Join(failures...)
}

func continuation(op ownerstore.Operation, grant, step string) *api.CallContext {
	return &api.CallContext{
		RequestId: op.RequestID + ":" + step, IdempotencyKey: op.ID + ":" + step,
		ActorId: op.ActorID, AcceptedOperationGrantId: proto.String(grant),
		Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: op.TenantID}}},
	}
}

var continuationActions = []api.AuthorizationActionEnum{
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEACCEPTEDOBLIGATION,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDREFERENCE,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_APPENDRECEIPT,
	api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT,
}

// Resume continues the committed original order. Repeating a side effect after
// response loss reuses its immutable command and key at the receiving owner.
func (s *Service) Resume(ctx context.Context, operationID string) error {
	ctx, cancel := context.WithTimeout(ctx, resumeTimeout)
	defer cancel()
	op, err := s.Store.ReadOperation(ctx, operationID)
	if err != nil {
		return dbError(err)
	}
	if op.Terminal() {
		return nil
	}
	if op.Kind != "create_workspace" {
		return status.Error(codes.FailedPrecondition, "operation is not a Workspace launch")
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
	a, result, err := decodeOrder(op)
	if err != nil {
		return err
	}
	offer := &api.QuoteAcceptance{}
	if protojson.Unmarshal(a.Quote, offer) != nil || offer.GetQuote().GetId() == "" || offer.GetResourcePlan() == nil {
		return status.Error(codes.DataLoss, "stored Workspace quote is invalid")
	}
	if result.GrantID == "" {
		e, err := evidence(op)
		if err != nil {
			return err
		}
		request := &api.AcceptedOperationGrantRequest{AuthorizationContextId: a.AuthorizationContextID, OwnerCommitEvidence: e, AllowedActions: continuationActions}
		if err = s.beginStep(ctx, op, token, "grant", 0, "tenant", "identity_verification", request); err != nil {
			return err
		}
		grant, err := s.Identity.IssueAcceptedOperationGrant(ctx, request)
		if err != nil {
			return s.failedCall(ctx, op, token, "grant", "identity_verification", result, err)
		}
		if grant.GetId() == "" || grant.GetAcceptedOperationId() != op.ID || grant.GetResourceId() != op.ResourceID || grant.GetAcceptedOperationOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || grant.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE || grant.GetActorId() != op.ActorID || !proto.Equal(grant.GetScope(), e.Scope) {
			return s.failedCall(ctx, op, token, "grant", "identity_verification", result, status.Error(codes.DataLoss, "accepted grant does not identify the Workspace order"))
		}
		result.GrantID = grant.Id
		if err = s.checkpoint(ctx, op, token, "grant", "quote_binding", "running", "confirmed", grant.Id, "", result); err != nil {
			return err
		}
	}
	accepted := &api.QuoteAcceptance{}
	if len(result.Acceptance) == 0 {
		request := &api.AcceptQuoteRequest{Context: continuation(op, result.GrantID, "accept_quote"), QuoteId: offer.Quote.Id, WorkspaceId: op.ResourceID, ObligationId: op.ID}
		if err = s.beginStep(ctx, op, token, "accept_quote", 1, "resource_catalog", "quote_binding", request); err != nil {
			return err
		}
		accepted, err = s.Catalog.AcceptQuote(ctx, request)
		if err != nil {
			return s.failedCall(ctx, op, token, "accept_quote", "quote_binding", result, err)
		}
		if err = validateAcceptance(op, offer, accepted); err != nil {
			return s.failedCall(ctx, op, token, "accept_quote", "quote_binding", result, err)
		}
		result.Acceptance = wire(accepted)
		if err = s.checkpoint(ctx, op, token, "accept_quote", "resource_preflight", "running", "confirmed", accepted.AcceptanceId, "", result); err != nil {
			return err
		}
	} else if protojson.Unmarshal(result.Acceptance, accepted) != nil {
		return status.Error(codes.DataLoss, "stored Workspace acceptance is invalid")
	}
	if err = validateAcceptance(op, offer, accepted); err != nil {
		return err
	}
	if result.FabricOperationID == "" || result.ResourceSetID == "" {
		if err = s.ensureResources(ctx, op, token, accepted, "", &result); err != nil {
			return err
		}
	}
	// Persist Fabric's original intent even when Ledger is not configured or is
	// temporarily unavailable. Only an explicitly free Local quote may proceed
	// through zero-charge evidence; an ordinary money quote remains unchanged.
	if localNoCharge(accepted) && s.Ledger != nil {
		receipt, err := s.zeroChargeReceipt(ctx, op, token, accepted, &result)
		if err != nil {
			return err
		}
		if err = s.ensureResources(ctx, op, token, accepted, receipt.Id, &result); err != nil {
			if code := resourcePermissionError(err); code != "" && result.FabricOperationID != "" && result.ResourceSetID != "" {
				return s.readResourceCloseout(ctx, op, token, code, &result)
			}
			return err
		}
	}
	request := &api.ResourceReadbackRequest{Context: continuation(op, result.GrantID, "read_resources"), ResourceSetId: result.ResourceSetID}
	if err = s.beginStep(ctx, op, token, "read_resources", 4, "fabric", "readback", request); err != nil {
		return err
	}
	readback, err := s.Fabric.ReadResources(ctx, request)
	if err != nil {
		return s.failedCall(ctx, op, token, "read_resources", "readback", result, err)
	}
	if readback.GetResourceSetId() != result.ResourceSetID || readback.GetWorkspaceId() != op.ResourceID || readback.GetOutcome() == api.Observation_OBSERVATION_UNSPECIFIED {
		return s.failedCall(ctx, op, token, "read_resources", "readback", result, status.Error(codes.DataLoss, "Fabric readback does not identify the original resources"))
	}
	result.ResourceReadback = wire(readback)
	switch readback.Outcome {
	case api.Observation_OBSERVATION_CONFIRMED:
		if err = s.checkpoint(ctx, op, token, "read_resources", "runtime", "awaiting_confirmation", "confirmed", result.ResourceSetID, "DEPENDENCY_UNAVAILABLE", result); err != nil {
			return err
		}
		return s.resumeRuntime(ctx, op, token, accepted, readback, &result)
	case api.Observation_OBSERVATION_REJECTED:
		return s.checkpoint(ctx, op, token, "read_resources", "readback", "failed", "rejected", result.ResourceSetID, readback.ErrorCode, result)
	default:
		return s.checkpoint(ctx, op, token, "read_resources", "readback", "awaiting_confirmation", "unknown", result.ResourceSetID, "DEPENDENCY_UNAVAILABLE", result)
	}
}

// A denied continuation cannot start new work, but the bounded grant can still
// observe the original resource obligation. Keep the denial on the operation
// even when that separate, read-only owner observation succeeds.
func (s *Service) readResourceCloseout(ctx context.Context, op ownerstore.Operation, token, permissionCode string, result *orderResult) error {
	request := &api.ResourceReadbackRequest{Context: continuation(op, result.GrantID, "read_resources"), ResourceSetId: result.ResourceSetID}
	if err := s.beginStep(ctx, op, token, "read_resources", 4, "fabric", "original_action_readback", request); err != nil {
		return err
	}
	readback, err := s.Fabric.ReadResources(ctx, request)
	stepObservation, stepCode := "unknown", "DEPENDENCY_UNAVAILABLE"
	if err == nil {
		if readback.GetResourceSetId() != result.ResourceSetID || readback.GetWorkspaceId() != op.ResourceID ||
			(readback.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED && readback.GetOutcome() != api.Observation_OBSERVATION_REJECTED && readback.GetOutcome() != api.Observation_OBSERVATION_UNKNOWN) {
			err = status.Error(codes.DataLoss, "Fabric closeout readback differs from the original resources")
		} else {
			result.ResourceReadback = wire(readback)
			stepCode = readback.ErrorCode
			switch readback.Outcome {
			case api.Observation_OBSERVATION_CONFIRMED:
				stepObservation = "confirmed"
			case api.Observation_OBSERVATION_REJECTED:
				stepObservation = "rejected"
			}
		}
	} else if code := resourcePermissionError(err); code != "" {
		stepObservation, stepCode = "rejected", code
	}
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if saveErr := s.checkpointOutcome(writeCtx, op, token, "read_resources", "original_action_readback", "needs_attention", "rejected", stepObservation, result.ResourceSetID, permissionCode, stepCode, *result); saveErr != nil {
		return saveErr
	}
	return err
}

func resourcePermissionError(err error) string {
	switch status.Code(err) {
	case codes.PermissionDenied:
		return "FORBIDDEN"
	case codes.Unauthenticated:
		return "UNAUTHENTICATED"
	default:
		return ""
	}
}

func (s *Service) ensureResources(ctx context.Context, op ownerstore.Operation, token string, accepted *api.QuoteAcceptance, receiptID string, result *orderResult) error {
	request := &api.EnsureResourcesCommand{Context: continuation(op, result.GrantID, "ensure_resources"), WorkspaceId: op.ResourceID, ObligationId: op.ID, Plan: accepted.ResourcePlan, QuoteAcceptance: accepted, ConfirmedChargeReceiptId: receiptID}
	if err := s.beginStep(ctx, op, token, "ensure_resources", 2, "fabric", "resource_preflight", request); err != nil {
		return err
	}
	resourceOperation, err := s.Fabric.EnsureResources(ctx, request)
	if err != nil {
		return s.failedCall(ctx, op, token, "ensure_resources", "resource_preflight", *result, err)
	}
	if resourceOperation.GetOperationId() == "" || resourceOperation.GetResourceId() == "" || resourceOperation.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC || resourceOperation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RESOURCE_PROVISION ||
		(result.FabricOperationID != "" && resourceOperation.OperationId != result.FabricOperationID) || (result.ResourceSetID != "" && resourceOperation.ResourceId != result.ResourceSetID) {
		return s.failedCall(ctx, op, token, "ensure_resources", "resource_preflight", *result, status.Error(codes.DataLoss, "Fabric returned a different resource operation"))
	}
	result.FabricOperationID, result.ResourceSetID = resourceOperation.OperationId, resourceOperation.ResourceId
	return s.checkpoint(ctx, op, token, "ensure_resources", "readback", "awaiting_confirmation", "confirmed", resourceOperation.OperationId, "", *result)
}

func localNoCharge(accepted *api.QuoteAcceptance) bool {
	return accepted.GetQuote() != nil && accepted.GetResourcePlan().GetProvider() == "local-docker" && accepted.GetResourcePlan().GetBillingMode() == "LOCAL_NO_CHARGE" && accepted.GetQuote().GetTotalUsdMicros() == 0
}

func (s *Service) zeroChargeReceipt(ctx context.Context, op ownerstore.Operation, token string, accepted *api.QuoteAcceptance, result *orderResult) (*api.Receipt, error) {
	commit, err := evidence(op)
	if err != nil {
		return nil, err
	}
	if len(result.ZeroChargeReceipt) > 0 {
		stored := &api.LocalNoChargeReceiptEvidence{}
		if protojson.Unmarshal(result.ZeroChargeReceipt, stored) != nil || validateZeroChargeEvidence(op, accepted, commit, stored, nil) != nil {
			return nil, status.Error(codes.DataLoss, "stored Local zero-charge evidence is invalid")
		}
		return stored.Receipt, nil
	}
	request := &api.AppendReceiptRequest{
		Context:        continuation(op, result.GrantID, "zero_charge_receipt"),
		Receipt:        &api.Receipt{Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(op.ID), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, EvidenceSummary: "Accepted Local deployment quote has no charge."},
		EvidenceDigest: accepted.SnapshotDigest, OwnerEvidenceReference: op.ID, QuoteAcceptance: accepted, OwnerCommitEvidence: commit,
	}
	if err = s.beginStep(ctx, op, token, "zero_charge_receipt", 3, "ledger", "receipt", request); err != nil {
		return nil, err
	}
	appended, err := s.Ledger.AppendReceipt(ctx, request)
	if err != nil {
		return nil, s.failedCall(ctx, op, token, "zero_charge_receipt", "receipt", *result, err)
	}
	readback, err := s.Ledger.ReadLocalNoChargeReceipt(ctx, &api.GetReceiptByReferenceRequest{Context: continuation(op, result.GrantID, "read_zero_charge_receipt"), Owner: "workspace", OwnerEvidenceReference: op.ID})
	if err != nil {
		return nil, s.failedCall(ctx, op, token, "zero_charge_receipt", "receipt", *result, err)
	}
	if err = validateZeroChargeEvidence(op, accepted, commit, readback, appended); err != nil {
		return nil, s.failedCall(ctx, op, token, "zero_charge_receipt", "receipt", *result, err)
	}
	result.ZeroChargeReceipt = wire(readback)
	if err = s.checkpoint(ctx, op, token, "zero_charge_receipt", "resource_preflight", "running", "confirmed", readback.Receipt.Id, "", *result); err != nil {
		return nil, err
	}
	return readback.Receipt, nil
}

func validateZeroChargeEvidence(op ownerstore.Operation, accepted *api.QuoteAcceptance, commit *api.OwnerCommitEvidence, readback *api.LocalNoChargeReceiptEvidence, appended *api.Receipt) error {
	r := readback.GetReceipt()
	if !localNoCharge(accepted) || r.GetId() == "" || r.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE || r.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || r.GetOperationId() != op.ID || r.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || r.GetCreatedAt() == nil || r.CreatedAt.CheckValid() != nil || readback.GetEvidenceDigest() != accepted.GetSnapshotDigest() || !proto.Equal(readback.GetQuoteAcceptance(), accepted) || !proto.Equal(readback.GetOwnerCommitEvidence(), commit) || (appended != nil && !proto.Equal(appended, r)) {
		return status.Error(codes.DataLoss, "Ledger zero-charge evidence differs from the accepted Workspace order")
	}
	return nil
}

func validateAcceptance(op ownerstore.Operation, offer, accepted *api.QuoteAcceptance) error {
	expected := proto.Clone(offer.GetQuote()).(*api.Quote)
	expected.Status = api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED
	if accepted.GetAcceptanceId() == "" || accepted.GetSnapshotDigest() == "" || accepted.GetObligationId() != op.ID || accepted.GetWorkspaceId() != op.ResourceID || !proto.Equal(expected, accepted.GetQuote()) || !proto.Equal(offer.GetResourcePlan(), accepted.GetResourcePlan()) {
		return status.Error(codes.DataLoss, "Catalog acceptance differs from the committed Workspace order")
	}
	return nil
}

func (s *Service) beginStep(ctx context.Context, op ownerstore.Operation, token, step string, sequence int, owner, stage string, request proto.Message) error {
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workspace.operations SET status='running',stage=$3,observation_result='unknown',started_at=COALESCE(started_at,now()),updated_at=now()
		WHERE id=$1 AND worker_lease_token=$2 AND worker_lease_until>now() AND status NOT IN ('succeeded','failed','cancelled')`, op.ID, token, stage)
	if err = fenced(res, err); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO workspace.saga_steps(id,operation_id,step_key,sequence,target_owner,command_id,idempotency_key,input_snapshot,observation_result,attempt_count)
		VALUES($1,$2,$3,$4,$5,$6,$6,$7,'unknown',1) ON CONFLICT(operation_id,step_key) DO UPDATE SET attempt_count=workspace.saga_steps.attempt_count+1,observation_result='unknown',confirmed_at=NULL,next_attempt_at=NULL,updated_at=now()`, id("step_"), op.ID, step, sequence, owner, op.ID+":"+step, []byte(wire(request)))
	if err != nil {
		return dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return dbError(err)
	}
	return nil
}

func fenced(result sql.Result, err error) error {
	if err != nil {
		return dbError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return dbError(err)
	}
	if n != 1 {
		return status.Error(codes.Aborted, "Workspace recovery lease was lost")
	}
	return nil
}

func (s *Service) checkpoint(ctx context.Context, op ownerstore.Operation, token, step, stage, state, observation, ownerRef, code string, result orderResult) error {
	return s.checkpointOutcome(ctx, op, token, step, stage, state, observation, observation, ownerRef, code, code, result)
}

func (s *Service) checkpointOutcome(ctx context.Context, op ownerstore.Operation, token, step, stage, state, operationObservation, stepObservation, ownerRef, operationCode, stepCode string, result orderResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		return status.Error(codes.Internal, "Workspace result cannot be encoded")
	}
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workspace.operations SET result=$3,status=$4,stage=$5,observation_result=$6,error_code=NULLIF($7,''),updated_at=now(),completed_at=CASE WHEN $4='failed' THEN now() ELSE completed_at END
		WHERE id=$1 AND worker_lease_token=$2 AND worker_lease_until>now() AND status NOT IN ('succeeded','failed','cancelled')`, op.ID, token, raw, state, stage, operationObservation, operationCode)
	if err = fenced(res, err); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE workspace.saga_steps SET observation_result=$3,owner_result_ref=NULLIF($4,''),error_code=NULLIF($5,''),confirmed_at=CASE WHEN $3='confirmed' THEN now() ELSE NULL END,next_attempt_at=CASE WHEN $3='unknown' THEN now()+interval '5 seconds' ELSE NULL END,updated_at=now() WHERE operation_id=$1 AND step_key=$2`, op.ID, step, stepObservation, ownerRef, stepCode)
	if err != nil {
		return dbError(err)
	}
	if state == "failed" {
		_, err = tx.ExecContext(ctx, `UPDATE workspace.workspaces SET status='failed',version=version+1,updated_at=now() WHERE id=$1 AND active_operation_id=$2`, op.ResourceID, op.ID)
		if err != nil {
			return dbError(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return dbError(err)
	}
	return nil
}

func (s *Service) failedCall(ctx context.Context, op ownerstore.Operation, token, step, stage string, result orderResult, cause error) error {
	state, observation, code := callFailure(step, cause)
	// A timed-out caller still leaves a durable unknown outcome for the worker.
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.checkpoint(writeCtx, op, token, step, stage, state, observation, "", code, result); err != nil {
		return err
	}
	return cause
}

func callFailure(step string, cause error) (state, observation, code string) {
	state, observation, code = "awaiting_confirmation", "unknown", "DEPENDENCY_UNAVAILABLE"
	if step == "ensure_resources" {
		if denied := resourcePermissionError(cause); denied != "" {
			return "needs_attention", "rejected", denied
		}
	}
	if status.Code(cause) == codes.DataLoss {
		return "needs_attention", observation, code
	}
	// Only Catalog's explicit refusal before resources are requested is a
	// terminal launch rejection. Transport loss never proves rejection.
	if step == "accept_quote" {
		switch status.Code(cause) {
		case codes.InvalidArgument, codes.FailedPrecondition, codes.AlreadyExists, codes.NotFound:
			return "failed", "rejected", "QUOTE_MISMATCH"
		}
	}
	return state, observation, code
}
