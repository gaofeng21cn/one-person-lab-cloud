package launch

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	api "opl-cloud/packages/contracts/go/api"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"opl-cloud/services/internal/ownerstore"
)

// An unfinished create has no delivered subscription period. Accept its closing
// intent without inventing one; downstream absence and funding remain unresolved
// until their original owners answer through the existing deletion operation.
func freezeUnfinishedDeletion(workspaceID, name string, version int64, authorizationContextID, launchID string, raw []byte) (deletionOrder, error) {
	var result orderResult
	if json.Unmarshal(raw, &result) != nil {
		return deletionOrder{}, status.Error(codes.DataLoss, "stored Workspace launch result is invalid")
	}
	material, err := json.Marshal(struct{ Workspace, Launch, Name string }{workspaceID, launchID, name})
	if err != nil {
		return deletionOrder{}, err
	}
	order := deletionOrder{
		AuthorizationContextID: authorizationContextID, WorkspaceName: name, WorkspaceVersion: version,
		LaunchOperationID: launchID, Unfinished: true, GrantID: result.GrantID, ResourceSetID: result.ResourceSetID,
		InputDigest: "sha256:" + ownerstore.HashRequestBody(material),
	}
	if len(result.RuntimeCommand) > 0 {
		command := &api.RuntimeDeployCommand{}
		if protojson.Unmarshal(result.RuntimeCommand, command) != nil || command.GetWorkspaceId() != workspaceID || command.GetRuntimeInstanceId() == "" || command.GetDeploymentId() == "" {
			return deletionOrder{}, status.Error(codes.DataLoss, "stored original runtime command is invalid")
		}
		order.RuntimeInstanceID, order.DeploymentID = command.GetRuntimeInstanceId(), command.GetDeploymentId()
	}
	if len(result.ResourceReadback) > 0 {
		readback := &api.ResourceReadback{}
		if protojson.Unmarshal(result.ResourceReadback, readback) != nil || readback.GetWorkspaceId() != workspaceID || readback.GetResourceSetId() != order.ResourceSetID {
			return deletionOrder{}, status.Error(codes.DataLoss, "stored original resource readback is invalid")
		}
		order.DataAttachmentID = readback.GetExecutionResources().GetDataAttachmentId()
	}
	return order, nil
}

func (s *Service) resolveUnfinishedRefund(ctx context.Context, op ownerstore.Operation, token string, order *deletionOrder, result *deletionResult) (bool, error) {
	original, err := s.Store.ReadOperation(ctx, order.LaunchOperationID)
	if err != nil {
		return false, dbError(err)
	}
	if original.Kind != "create_workspace" || original.TenantID != op.TenantID || original.ResourceID != op.ResourceID {
		return false, status.Error(codes.DataLoss, "closing intent does not identify its original order")
	}
	acceptedInput, progress, err := decodeOrder(original)
	if err != nil {
		return false, err
	}
	offer, accepted := &api.QuoteAcceptance{}, &api.QuoteAcceptance{}
	if protojson.Unmarshal(acceptedInput.Quote, offer) != nil {
		return false, status.Error(codes.DataLoss, "original quote snapshot is invalid")
	}
	hasAcceptance := len(progress.Acceptance) > 0
	if hasAcceptance && (protojson.Unmarshal(progress.Acceptance, accepted) != nil || validateAcceptance(original, offer, accepted) != nil) {
		return false, status.Error(codes.DataLoss, "original accepted quote is invalid")
	}
	if hasAcceptance && localNoCharge(accepted) {
		order.OriginalChargeUSDMicros = 0
		return true, nil
	}
	if s.Gateway == nil || s.Ledger == nil || s.Serve == nil {
		return false, s.checkpointDeletion(ctx, op, token, "read_original_charge", "refund", "awaiting_confirmation", "unknown", original.ID, "DEPENDENCY_UNAVAILABLE", result)
	}
	// Gateway's fenced read proves whether the original charge was dispatched.
	// Private Ledger evidence still uses the original accepted order's identity.
	charge, err := s.Gateway.ReadWalletAction(ctx, &api.WalletReadbackRequest{Context: continuation(op, result.GrantID, "read_original_charge"), OriginalIdempotencyKey: original.ID + ":wallet_charge", WorkspaceId: op.ResourceID, ConfirmClosedAbsence: true})
	if status.Code(err) == codes.NotFound {
		order.OriginalChargeUSDMicros = 0
		return true, nil
	}
	if err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "read_original_charge", "refund", *result, err)
	}
	if !hasAcceptance {
		return false, status.Error(codes.DataLoss, "original charge exists without its accepted quote")
	}
	if err = validateWalletCharge(original, accepted, charge); err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "read_original_charge", "refund", *result, err)
	}
	if charge.GetStatus() == api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REJECTED {
		if !validErrorCode(charge.GetErrorCode()) {
			return false, status.Error(codes.DataLoss, "rejected original charge has no owner cause")
		}
		order.OriginalChargeUSDMicros = 0
		return true, nil
	}
	if charge.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED || charge.GetReceiptId() == "" {
		return false, s.checkpointDeletion(ctx, op, token, "read_original_charge", "refund", "awaiting_confirmation", "unknown", charge.GetId(), "DEPENDENCY_UNAVAILABLE", result)
	}
	readFunding := &api.GetReceiptByReferenceRequest{Context: continuation(original, progress.GrantID, "read_wallet_action_receipt"), Owner: "workspace", OwnerEvidenceReference: original.ID}
	funding, err := s.Ledger.ReadWalletActionReceipt(ctx, readFunding)
	if status.Code(err) == codes.NotFound {
		request, requestErr := walletActionReceiptRequest(original, progress.GrantID, accepted, charge)
		if requestErr != nil {
			return false, requestErr
		}
		if err = s.beginStep(ctx, op, token, "complete_original_funding_receipt", 3, "ledger", "refund", request); err != nil {
			return false, err
		}
		if _, err = s.Ledger.AppendReceipt(ctx, request); err != nil {
			return false, s.failedDeletionCall(ctx, op, token, "complete_original_funding_receipt", "refund", *result, err)
		}
		funding, err = s.Ledger.ReadWalletActionReceipt(ctx, readFunding)
	}
	if err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "read_original_funding", "refund", *result, err)
	}
	if err = validatePaidFundingEvidence(original, accepted, funding); err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "read_original_funding", "refund", *result, err)
	}
	if !proto.Equal(charge, funding.GetWalletOperation()) {
		return false, status.Error(codes.DataLoss, "original funding differs from the wallet authority")
	}
	runtime, err := s.Serve.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: continuation(op, result.GrantID, "read_retired_runtime"), RuntimeInstanceId: order.RuntimeInstanceID, DeploymentId: order.DeploymentID, WorkspaceId: op.ResourceID, OriginalReservationIdempotencyKey: order.LaunchOperationID + ":reserve_runtime"})
	if err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "read_original_fulfillment", "refund", *result, err)
	}
	if err = validateClosingRuntime(op, *order, runtime); err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "read_original_fulfillment", "refund", *result, err)
	}
	if first := runtime.GetFirstFulfilledAt(); first != nil {
		if first.CheckValid() != nil || runtime.GetNeverDeliveredConfirmed() {
			return false, status.Error(codes.DataLoss, "Serve fulfillment history conflicts")
		}
		order.PeriodStart = first.AsTime().UTC().Format(time.RFC3339Nano)
	} else if !runtime.GetNeverDeliveredConfirmed() {
		return false, s.checkpointDeletion(ctx, op, token, "read_original_fulfillment", "refund", "awaiting_confirmation", "unknown", runtime.GetRuntimeInstanceId(), "DEPENDENCY_UNAVAILABLE", result)
	}
	order.OriginalChargeUSDMicros, order.OriginalChargeWalletOperationID, order.OriginalChargeReceiptID = charge.GetAmountUsdMicros(), charge.GetId(), charge.GetReceiptId()
	order.RefundPolicyVersionID = contracts.WorkspaceRefundPolicyVersion
	return true, nil
}

func validateClosingRuntime(op ownerstore.Operation, order deletionOrder, read *api.RuntimeReadback) error {
	if read.GetWorkspaceId() != op.ResourceID || read.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || read.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED || read.GetProcessReady() || read.GetApplicationAvailable() || (read.GetRuntimeInstanceId() == "") != (read.GetDeploymentId() == "") || (order.RuntimeInstanceID != "" && read.GetRuntimeInstanceId() != order.RuntimeInstanceID) || (order.DeploymentID != "" && read.GetDeploymentId() != order.DeploymentID) {
		return status.Error(codes.DataLoss, "Serve did not confirm the original runtime closeout")
	}
	if read.GetRuntimeInstanceId() == "" && !read.GetNeverDeliveredConfirmed() {
		return status.Error(codes.DataLoss, "missing runtime identity has no owner-confirmed delivery absence")
	}
	return nil
}

func (s *Service) confirmOriginalResourceCloseout(ctx context.Context, op ownerstore.Operation, token string, order deletionOrder, result *deletionResult) (bool, error) {
	if s.Fabric == nil {
		return false, s.checkpointDeletion(ctx, op, token, "delete_resources", "attachment_deletion", "awaiting_confirmation", "unknown", "", "DEPENDENCY_UNAVAILABLE", result)
	}
	command := &api.MutateResourcesCommand{Context: continuation(op, result.GrantID, "delete_resources"), WorkspaceId: op.ResourceID, OriginalProvisionIdempotencyKey: order.LaunchOperationID + ":ensure_resources"}
	if err := s.beginStep(ctx, op, token, "delete_resources", 1, "fabric", "attachment_deletion", command); err != nil {
		return false, err
	}
	deleted, err := s.Fabric.DeleteResources(ctx, command)
	if err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "delete_resources", "attachment_deletion", *result, err)
	}
	read, err := s.Fabric.ReadResources(ctx, &api.ResourceReadbackRequest{Context: continuation(op, result.GrantID, "read_resources"), WorkspaceId: op.ResourceID, OriginalProvisionIdempotencyKey: order.LaunchOperationID + ":ensure_resources"})
	if err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "read_resources", "storage_deletion", *result, err)
	}
	if read.GetWorkspaceId() != op.ResourceID {
		return false, status.Error(codes.DataLoss, "Fabric original closeout identifies another Workspace")
	}
	order.ResourceSetID = read.GetResourceSetId()
	if err = validateResourceDeletion(op, order, deleted); err != nil {
		return false, s.failedDeletionCall(ctx, op, token, "delete_resources", "attachment_deletion", *result, err)
	}
	result.DeleteOperation, result.ResourceReadback = wire(deleted), wire(read)
	if read.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || !read.GetAbsenceConfirmed() {
		return false, s.checkpointDeletion(ctx, op, token, "read_resources", "storage_deletion", "awaiting_confirmation", "unknown", order.ResourceSetID, "DEPENDENCY_UNAVAILABLE", result)
	}
	if result.DeletedAt == "" {
		result.DeletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if err = s.checkpointDeletion(ctx, op, token, "read_resources", "compute_deletion", "running", "confirmed", order.ResourceSetID, "", result); err != nil {
		return false, err
	}
	return true, nil
}
