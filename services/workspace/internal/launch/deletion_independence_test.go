package launch

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"testing"
	"time"
)

func TestDeletedWorkspaceRemainsDeletedWhileOriginalRefundIsPending(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	gateway := &deletionGatewayClient{pendingOnce: true}
	service.Gateway = gateway
	call.IdempotencyKey = "delete-independent-refund"
	accepted, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := service.GetWorkspace(ctx, &api.GetWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.GetStatus() != api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_DELETED {
		t.Fatalf("confirmed absence with unresolved refund: Workspace=%v, want deleted", workspace.GetStatus())
	}
	facts, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if facts.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_REQUESTED || facts.GetRefundOperationId() == "" {
		t.Fatalf("original pending refund=%v", facts)
	}
	// A new handler instance must recover from the persisted intent, not a closure.
	restarted := &Service{Store: service.Store, Identity: service.Identity, Ledger: service.Ledger, Fabric: service.Fabric, Serve: service.Serve, Gateway: service.Gateway, Auth: service.Auth}
	call.IdempotencyKey = "second-delete-intent-after-tombstone"
	replay, err := restarted.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if replay.GetOperationId() != accepted.GetOperationId() {
		t.Fatal("tombstone replay replaced original operation")
	}
	if err = restarted.Resume(ctx, accepted.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	facts, err = restarted.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if facts.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED || gateway.refunds != 1 {
		t.Fatalf("refund=%v dispatches=%d", facts, gateway.refunds)
	}
}

func TestDeletedWorkspaceRetainsRefusedKeyObligationAndReplaysOriginalIntent(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspaceWithManagedKey(t, db)
	gateway := service.Gateway.(*deletionGatewayClient)
	gateway.disableErr = status.Error(codes.Unimplemented, "key disable unavailable")
	call.IdempotencyKey = "delete-key-obligation"
	request := &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}}
	accepted, err := service.DeleteWorkspace(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := service.GetWorkspace(ctx, &api.GetWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if workspace.GetStatus() != api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_DELETED {
		t.Fatalf("deleted resources must retain deleted Workspace: %v", workspace.GetStatus())
	}
	op, err := service.GetOperation(ctx, &api.GetOperationRpcRequest{Context: call, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, OperationId: accepted.GetOperationId()})
	if err != nil {
		t.Fatal(err)
	}
	if op.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION || op.GetErrorCode() != api.ErrorCodeEnum_ERROR_CODE_ENUM_OWNER_CAPABILITY_UNAVAILABLE {
		t.Fatalf("refused key obligation became terminal or unreadable: %v", op)
	}
	gateway.disableErr = nil
	call.IdempotencyKey = "explicit-key-replay-after-tombstone"
	replayed, err := service.DeleteWorkspace(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.GetOperationId() != accepted.GetOperationId() || replayed.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || gateway.refunds != 1 {
		t.Fatalf("replay=%v refund dispatches=%d", replayed, gateway.refunds)
	}
}

type unknownDeletionRefundClient struct{ *deletionGatewayClient }

func (c *unknownDeletionRefundClient) Refund(ctx context.Context, r *api.WalletRefundCommand, opts ...grpc.CallOption) (*api.WalletOperation, error) {
	out, err := c.deletionGatewayClient.Refund(ctx, r, opts...)
	if err == nil {
		out.Status = api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN
	}
	return out, err
}

func TestDeletedWorkspaceReportsUnknownRefundAndRecoversOnlyItsOriginalAction(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	gateway := &unknownDeletionRefundClient{&deletionGatewayClient{}}
	service.Gateway = gateway
	call.IdempotencyKey = "delete-unknown-refund"
	accepted, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if facts.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_UNKNOWN {
		t.Fatalf("unknown refund was relabeled: %v", facts)
	}
	if err = service.Resume(ctx, accepted.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	facts, err = service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if facts.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED || gateway.refunds != 1 || gateway.reads != 1 {
		t.Fatalf("refund=%v dispatches=%d reads=%d", facts, gateway.refunds, gateway.reads)
	}
}

func TestDeletionSettlesTheCurrentSubscriptionPeriodInsteadOfTheFirstHistoricalCharge(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, currentStart, _ := deletionWorkspace(t, db)
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT accepted_quote_snapshot FROM workspace.subscription_periods WHERE id='period-original'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	historical := &api.QuoteAcceptance{}
	if err := protojson.Unmarshal(raw, historical); err != nil {
		t.Fatal(err)
	}
	historical.Quote.Id = "quote-historical"
	historical.Quote.PeriodStart = timestamppb.New(currentStart.Add(-30 * 24 * time.Hour))
	historical.Quote.PeriodEnd = timestamppb.New(currentStart)
	historical.ObligationId = "operation-historical"
	historical.AcceptanceId = "acceptance-historical"
	historical.SnapshotDigest = "sha256:historical"
	snapshot, err := protojson.Marshal(historical)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `INSERT INTO workspace.subscription_periods(id,subscription_id,quote_id,accepted_quote_snapshot,period_start,period_end,billing_key,charge_wallet_operation_id,charge_receipt_id,provenance) VALUES('period-historical','subscription-original','quote-historical',$1,$2,$3,'billing:historical','wallet-op-charge-historical','wallet-receipt-historical','quoted')`, snapshot, historical.Quote.PeriodStart.AsTime(), currentStart); err != nil {
		t.Fatal(err)
	}
	call.IdempotencyKey = "delete-current-period"
	accepted, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	// The wallet boundary accepts only the currently applicable original charge.
	if accepted.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("deletion selected a historical charge instead of the current subscription period: %v", accepted)
	}
	facts, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if facts.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED {
		t.Fatalf("current original charge was not settled: %v", facts)
	}
}

func TestRefusedKeyRemainsAttentionWhileOriginalRefundIsPending(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspaceWithManagedKey(t, db)
	gateway := service.Gateway.(*deletionGatewayClient)
	gateway.pendingOnce = true
	gateway.disableErr = status.Error(codes.Unimplemented, "key disable unavailable")
	call.IdempotencyKey = "delete-refused-key-pending-refund"
	accepted, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION {
		t.Fatalf("pending refund hid the refused key obligation: %v", accepted)
	}
	before := gateway.disableCalls
	if err = service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if gateway.disableCalls != before || gateway.refunds != 1 {
		t.Fatal("automatic recovery replayed a refused mutation")
	}
	facts, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if facts.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_REQUESTED || facts.GetResourceDeletionStatus() != api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_CONFIRMED {
		t.Fatalf("independent facts=%v", facts)
	}
}
