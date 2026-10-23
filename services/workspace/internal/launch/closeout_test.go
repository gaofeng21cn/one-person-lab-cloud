package launch

import (
	"context"
	"encoding/json"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"testing"
	"time"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// Exercise the existing customer command and readbacks. Unavailable downstream
// owners must not prevent accepting closure, or turn uncertainty into deletion.
func TestDeleteWorkspaceAcceptsUnfinishedOrderWithoutClaimingAbsence(t *testing.T) {
	db := runtimeDatabase(t)
	service, original, _, _ := seedRuntimeOrder(t, db)
	service.Identity = &deletionIdentity{}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	service.Fabric = nil
	call, ctx := deletionCallContext(original.TenantID)
	call.IdempotencyKey = "close-original-unfinished"
	operation, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{
		Context: call, WorkspaceId: original.ResourceID,
		Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true},
	})
	if err != nil {
		t.Fatalf("unfinished original order must accept closure: %v", err)
	}
	if operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_DELETE_WORKSPACE || operation.GetResourceId() != original.ResourceID {
		t.Fatalf("closure lost original Workspace identity: %v", operation)
	}
	readback, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: original.ResourceID})
	if err != nil {
		t.Fatal(err)
	}
	if readback.GetResourceDeletionStatus() != api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_UNKNOWN || readback.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_UNKNOWN {
		t.Fatalf("unanswered owners were presented as closed: %v", readback)
	}
}

func TestDeleteWorkspaceStopsTheOriginalCreateRecovery(t *testing.T) {
	db := runtimeDatabase(t)
	service, original, _, _ := seedRuntimeOrder(t, db)
	service.Identity = &deletionIdentity{}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	service.Fabric = nil
	call, ctx := deletionCallContext(original.TenantID)
	call.IdempotencyKey = "stop-original-create"
	_, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: original.ResourceID,
		Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	read := &api.GetOperationRpcRequest{Context: call, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, OperationId: original.ID}
	operation, err := service.GetOperation(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_CANCELLED {
		t.Fatalf("original create remains eligible for recovery after closure: %v", operation)
	}
	if err := service.Resume(ctx, original.ID); err != nil {
		t.Fatal(err)
	}
	after, err := service.GetOperation(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	if after.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_CANCELLED {
		t.Fatalf("closed create restarted: %v", after)
	}
}

func TestDeleteWorkspacePreservesDeliveredTermsWhenCreateAcknowledgementIsPending(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, total := deletionWorkspace(t, db)
	if _, err := db.ExecContext(ctx, `UPDATE workspace.operations SET status=$2,completed_at=NULL WHERE id=$1`, "operation-original", ownerstore.OperationAwaitingConfirm); err != nil {
		t.Fatal(err)
	}
	call.IdempotencyKey = "close-delivered-before-acknowledgement"
	operation, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	deletion, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || deletion.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED || service.Gateway.(*deletionGatewayClient).refundAmount <= 0 || service.Gateway.(*deletionGatewayClient).refundAmount >= total {
		t.Fatalf("a delivered paid period was relabelled as failed fulfillment: operation=%v deletion=%v", operation, deletion)
	}
}

func TestDeleteWorkspaceClosesUnfinishedLocalOrderThroughExistingOwners(t *testing.T) {
	db := runtimeDatabase(t)
	service, original, _, _ := seedRuntimeOrder(t, db)
	progress, err := json.Marshal(orderResult{
		RuntimeReservation: wire(&api.RuntimeReservation{WorkspaceId: original.ResourceID, RuntimeInstanceId: "runtime-original", DeploymentId: "deployment-original", ExecutionEpoch: 1}),
		RuntimeCommand:     wire(&api.RuntimeDeployCommand{WorkspaceId: original.ResourceID, RuntimeInstanceId: "runtime-original", DeploymentId: "deployment-original", ExecutionEpoch: 1}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.operations SET result=result || $2::jsonb WHERE id=$1`, original.ID, progress); err != nil {
		t.Fatal(err)
	}
	service.Identity = &deletionIdentity{}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	service.Serve, service.Fabric, service.Ledger, service.Gateway = &deletionServeClient{}, &deletionFabricClient{}, &deletionLedgerClient{}, &deletionGatewayClient{}
	call, ctx := deletionCallContext(original.TenantID)
	call.IdempotencyKey = "close-unfinished-local"
	operation, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: original.ResourceID, Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	deletion, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: original.ResourceID})
	if err != nil {
		t.Fatal(err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || deletion.GetResourceDeletionStatus() != api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_CONFIRMED || deletion.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_NOT_APPLICABLE {
		t.Fatalf("original unfulfilled Local order did not close through existing owners: operation=%v deletion=%v", operation, deletion)
	}
}

type closeoutServe struct {
	deletionServeClient
	fulfillment *api.RuntimeReadback
}

func (s *closeoutServe) ReadRuntime(context.Context, *api.RuntimeReadbackRequest, ...grpc.CallOption) (*api.RuntimeReadback, error) {
	return proto.Clone(s.fulfillment).(*api.RuntimeReadback), nil
}

type closeoutGateway struct {
	deletionGatewayClient
	charge      *api.WalletOperation
	chargeReads int
}

func (g *closeoutGateway) ReadWalletAction(ctx context.Context, request *api.WalletReadbackRequest, options ...grpc.CallOption) (*api.WalletOperation, error) {
	if request.GetOriginalIdempotencyKey() == "operation-original:wallet_charge" {
		g.chargeReads++
		return proto.Clone(g.charge).(*api.WalletOperation), nil
	}
	return g.deletionGatewayClient.ReadWalletAction(ctx, request, options...)
}

type closeoutLedger struct {
	deletionLedgerClient
	funding *api.WalletActionReceiptEvidence
}

func (l *closeoutLedger) ReadWalletActionReceipt(_ context.Context, r *api.GetReceiptByReferenceRequest, _ ...grpc.CallOption) (*api.WalletActionReceiptEvidence, error) {
	if r.GetOwnerEvidenceReference() != "operation-original" || l.funding == nil {
		return nil, status.Error(codes.NotFound, "original charge receipt unconfirmed")
	}
	return proto.Clone(l.funding).(*api.WalletActionReceiptEvidence), nil
}

func (l *closeoutLedger) AppendReceipt(ctx context.Context, request *api.AppendReceiptRequest, options ...grpc.CallOption) (*api.Receipt, error) {
	if request.GetReceipt().GetKind() == api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION {
		owner := &ledgerAuthority{}
		receipt, err := owner.AppendReceipt(ctx, request, options...)
		if err == nil {
			l.funding = owner.receipts[request.GetOwnerEvidenceReference()]
		}
		return receipt, err
	}
	return l.deletionLedgerClient.AppendReceipt(ctx, request, options...)
}

func TestUnfinishedPaidDeletionUsesOriginalFundingAndFulfillment(t *testing.T) {
	for _, tc := range []struct {
		name                                                    string
		missingFunding, delivered, unknownCharge, pendingRefund bool
	}{
		{name: "undelivered with confirmed funding"},
		{name: "original funding receipt response lost", missingFunding: true},
		{name: "delivered before create acknowledgement", delivered: true},
		{name: "unknown charge stays unknown", unknownCharge: true},
		{name: "refund response pending across owner restart", pendingRefund: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := runtimeDatabase(t)
			service, original, accepted := seedPaidOrder(t, db)
			command := &api.RuntimeDeployCommand{WorkspaceId: original.ResourceID, RuntimeInstanceId: "runtime-original", DeploymentId: "deployment-original"}
			progress, _ := json.Marshal(orderResult{RuntimeCommand: wire(command)})
			if _, err := db.ExecContext(t.Context(), `UPDATE workspace.operations SET result=result || $2::jsonb WHERE id=$1`, original.ID, progress); err != nil {
				t.Fatal(err)
			}
			commit, err := evidence(original)
			if err != nil {
				t.Fatal(err)
			}
			charge := &api.WalletOperation{Id: "wallet-op-charge-original", WorkspaceId: proto.String(original.ResourceID), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE, AmountUsdMicros: accepted.GetQuote().GetTotalUsdMicros(), Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED, ReceiptId: proto.String("receipt-original-charge"), CreatedAt: timestamppb.Now()}
			gateway := &closeoutGateway{charge: charge}
			ledger := &closeoutLedger{funding: &api.WalletActionReceiptEvidence{Receipt: &api.Receipt{Id: charge.GetReceiptId(), Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(original.ID), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, CreatedAt: timestamppb.Now()}, WalletOperation: charge, QuoteAcceptance: accepted, OwnerCommitEvidence: commit, EvidenceDigest: accepted.GetSnapshotDigest()}}
			if tc.missingFunding {
				ledger.funding = nil
			}
			service.Identity = &deletionIdentity{}
			service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
			service.Serve = &closeoutServe{fulfillment: &api.RuntimeReadback{WorkspaceId: original.ResourceID, RuntimeInstanceId: command.RuntimeInstanceId, DeploymentId: command.DeploymentId, State: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED, Outcome: api.Observation_OBSERVATION_CONFIRMED, NeverDeliveredConfirmed: true}}
			if tc.delivered {
				read := service.Serve.(*closeoutServe).fulfillment
				read.NeverDeliveredConfirmed = false
				read.FirstFulfilledAt = timestamppb.New(time.Now().Add(-100 * time.Hour))
			}
			if tc.unknownCharge {
				gateway.charge.Status = api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN
			}
			gateway.pendingOnce = tc.pendingRefund
			service.Fabric, service.Gateway, service.Ledger = &deletionFabricClient{}, gateway, ledger
			call, ctx := deletionCallContext(original.TenantID)
			call.IdempotencyKey = "close-paid-original"
			operation, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: original.ResourceID, Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Paid workspace", AcknowledgeDataDestruction: true}})
			if err != nil {
				t.Fatal(err)
			}
			deletion, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: original.ResourceID})
			if err != nil {
				t.Fatal(err)
			}
			if tc.unknownCharge {
				if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION || deletion.GetResourceDeletionStatus() != api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_CONFIRMED || deletion.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_UNKNOWN || gateway.refunds != 0 {
					t.Fatalf("unknown charge was relabelled settled: %v %v", operation, deletion)
				}
				return
			}
			if tc.pendingRefund {
				if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION || gateway.refunds != 1 {
					t.Fatalf("pending refund was not retained: %v", operation)
				}
				store, storeErr := ownerstore.New(db, "workspace")
				if storeErr != nil {
					t.Fatal(storeErr)
				}
				restarted := &Service{Store: store, Auth: service.Auth, Identity: service.Identity, Serve: service.Serve, Fabric: service.Fabric, Gateway: gateway, Ledger: ledger}
				if err = restarted.Resume(ctx, operation.GetOperationId()); err != nil {
					t.Fatal(err)
				}
				operation, err = restarted.GetOperation(ctx, &api.GetOperationRpcRequest{Context: call, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, OperationId: operation.GetOperationId()})
				if err != nil {
					t.Fatal(err)
				}
				deletion, err = restarted.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: original.ResourceID})
				if err != nil {
					t.Fatal(err)
				}
			}
			amountValid := gateway.refundAmount == charge.GetAmountUsdMicros()
			if tc.delivered {
				amountValid = gateway.refundAmount > 0 && gateway.refundAmount < charge.GetAmountUsdMicros()
			}
			if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || deletion.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED || !amountValid || gateway.chargeReads == 0 {
				t.Fatalf("unfinished charge did not close through original authorities: operation=%v deletion=%v refund=%d charge reads=%d", operation, deletion, gateway.refundAmount, gateway.chargeReads)
			}
			if err = service.Resume(ctx, operation.GetOperationId()); err != nil {
				t.Fatal(err)
			}
			if gateway.refunds != 1 {
				t.Fatalf("replay refunded the original charge again: %d", gateway.refunds)
			}
		})
	}
}

type originalKeyCloseout struct{ deletionGatewayClient }

func (g *originalKeyCloseout) DisableManagedKey(ctx context.Context, request *api.ManagedKeyLifecycleCommand, options ...grpc.CallOption) (*api.ManagedKeyLifecycleReadback, error) {
	if request.GetOriginalCreateIdempotencyKey() != "operation-original:create_managed_key" {
		return nil, status.Error(codes.InvalidArgument, "the original key command must be resolved")
	}
	g.disableCalls++
	return &api.ManagedKeyLifecycleReadback{WorkspaceId: request.GetWorkspaceId(), KeyBindingId: "original-key-after-ack-loss", State: api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED, Outcome: api.Observation_OBSERVATION_CONFIRMED, LifecycleVersion: 1, UpdatedAt: timestamppb.Now()}, nil
}

func TestUnfinishedDeletionResolvesTheOriginalKeyAfterAcknowledgementLoss(t *testing.T) {
	db := runtimeDatabase(t)
	service, original, _, _ := seedRuntimeOrder(t, db)
	progress, _ := json.Marshal(orderResult{RuntimeCommand: wire(&api.RuntimeDeployCommand{WorkspaceId: original.ResourceID, RuntimeInstanceId: "runtime-original", DeploymentId: "deployment-original"})})
	if _, err := db.ExecContext(t.Context(), `UPDATE workspace.operations SET result=result || $2::jsonb WHERE id=$1`, original.ID, progress); err != nil {
		t.Fatal(err)
	}
	gateway := &originalKeyCloseout{}
	service.Identity = &deletionIdentity{}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	service.Serve, service.Fabric, service.Gateway, service.Ledger = &deletionServeClient{}, &deletionFabricClient{}, gateway, &deletionLedgerClient{}
	call, ctx := deletionCallContext(original.TenantID)
	call.IdempotencyKey = "close-with-original-key-ack-loss"
	operation, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: original.ResourceID, Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || gateway.disableCalls != 1 {
		t.Fatalf("unrecorded key was skipped instead of closed by its owner: %v calls=%d", operation, gateway.disableCalls)
	}
}

func TestDeletionRefusesAmbiguousOriginalCreates(t *testing.T) {
	db := runtimeDatabase(t)
	service, original, _, _ := seedRuntimeOrder(t, db)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO workspace.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input,result) SELECT 'conflicting-original',tenant_id,actor_id,kind,resource_id,status,stage,'conflicting-request',accepted_input,result FROM workspace.operations WHERE id=$1`, original.ID); err != nil {
		t.Fatal(err)
	}
	service.Identity = &deletionIdentity{}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	call, ctx := deletionCallContext(original.TenantID)
	call.IdempotencyKey = "ambiguous-original-closeout"
	_, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: original.ResourceID, Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("deletion chose an original create from conflicting records: %v", err)
	}
}

type absentOriginalServe struct {
	api.ServeAgentCoordinationClient
}

func (s *absentOriginalServe) Retire(_ context.Context, r *api.RuntimeStopCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	if r.GetWorkspaceId() != "workspace-original" || r.GetOriginalReservationIdempotencyKey() != "operation-original:reserve_runtime" {
		return nil, status.Error(codes.InvalidArgument, "original reservation identity required")
	}
	return &api.Operation{OperationId: "serve-close-original", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_RETIRE, ResourceId: r.WorkspaceId, Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, ObservationResult: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED.Enum()}, nil
}
func (s *absentOriginalServe) ReadRuntime(_ context.Context, r *api.RuntimeReadbackRequest, _ ...grpc.CallOption) (*api.RuntimeReadback, error) {
	if r.GetWorkspaceId() != "workspace-original" || r.GetOriginalReservationIdempotencyKey() != "operation-original:reserve_runtime" {
		return nil, status.Error(codes.InvalidArgument, "original reservation read required")
	}
	return &api.RuntimeReadback{WorkspaceId: r.WorkspaceId, State: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED, Outcome: api.Observation_OBSERVATION_CONFIRMED, NeverDeliveredConfirmed: true}, nil
}

type absentOriginalFabric struct{ api.FabricCoordinationClient }

func (f *absentOriginalFabric) DeleteResources(_ context.Context, r *api.MutateResourcesCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	if r.GetWorkspaceId() != "workspace-original" || r.GetOriginalProvisionIdempotencyKey() != "operation-original:ensure_resources" {
		return nil, status.Error(codes.InvalidArgument, "original resource command required")
	}
	return &api.Operation{OperationId: "fabric-close-original", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_RESOURCE_DELETE, ResourceId: r.WorkspaceId, Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, ObservationResult: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED.Enum()}, nil
}
func (f *absentOriginalFabric) ReadResources(_ context.Context, r *api.ResourceReadbackRequest, _ ...grpc.CallOption) (*api.ResourceReadback, error) {
	if r.GetWorkspaceId() != "workspace-original" || r.GetOriginalProvisionIdempotencyKey() != "operation-original:ensure_resources" {
		return nil, status.Error(codes.InvalidArgument, "original resource read required")
	}
	return &api.ResourceReadback{WorkspaceId: r.WorkspaceId, Outcome: api.Observation_OBSERVATION_CONFIRMED, AbsenceConfirmed: true, ObservedAt: timestamppb.Now()}, nil
}
func TestUnfinishedDeletionUsesOwnerFencesWhenNoResourceIDsWereReturned(t *testing.T) {
	db := runtimeDatabase(t)
	service, original, _, _ := seedRuntimeOrder(t, db)
	if _, err := db.ExecContext(t.Context(), `UPDATE workspace.operations SET result=result-'resourceSetId'-'resourceReadback'-'fabricOperationId' WHERE id=$1`, original.ID); err != nil {
		t.Fatal(err)
	}
	service.Identity = &deletionIdentity{}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	service.Serve, service.Fabric, service.Gateway, service.Ledger = &absentOriginalServe{}, &absentOriginalFabric{}, &deletionGatewayClient{}, &deletionLedgerClient{}
	call, ctx := deletionCallContext(original.TenantID)
	call.IdempotencyKey = "close-without-returned-identities"
	operation, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: original.ResourceID, Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("original owner absence did not close the unfinished Workspace: %v %v", operation, err)
	}
}

type absentOriginalGateway struct{ deletionGatewayClient }

func (g *absentOriginalGateway) ReadWalletAction(_ context.Context, r *api.WalletReadbackRequest, _ ...grpc.CallOption) (*api.WalletOperation, error) {
	if !r.GetConfirmClosedAbsence() || r.GetWorkspaceId() != "workspace-original" || r.GetOriginalIdempotencyKey() != "operation-original:wallet_charge" {
		return nil, status.Error(codes.InvalidArgument, "fenced original charge read required")
	}
	return nil, status.Error(codes.NotFound, "Gateway confirms this original charge was never dispatched")
}
func TestUnfinishedPaidDeletionRequiresOwnerConfirmedChargeAbsence(t *testing.T) {
	for _, accepted := range []bool{true, false} {
		t.Run(map[bool]string{true: "quote accepted", false: "quote acceptance pending"}[accepted], func(t *testing.T) {
			db := runtimeDatabase(t)
			service, original, _ := seedPaidOrder(t, db)
			if _, err := db.ExecContext(t.Context(), `UPDATE workspace.operations SET result=result-'resourceSetId'-'resourceReadback'-'fabricOperationId' WHERE id=$1`, original.ID); err != nil {
				t.Fatal(err)
			}
			if !accepted {
				if _, err := db.ExecContext(t.Context(), `UPDATE workspace.operations SET result=result-'acceptance' WHERE id=$1`, original.ID); err != nil {
					t.Fatal(err)
				}
			}
			service.Identity = &deletionIdentity{}
			service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
			gateway := &absentOriginalGateway{}
			service.Serve, service.Fabric, service.Gateway, service.Ledger = &absentOriginalServe{}, &absentOriginalFabric{}, gateway, &deletionLedgerClient{}
			call, ctx := deletionCallContext(original.TenantID)
			call.IdempotencyKey = "close-never-charged-order"
			operation, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: original.ResourceID, Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Paid workspace", AcknowledgeDataDestruction: true}})
			if err != nil {
				t.Fatal(err)
			}
			read, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: original.ResourceID})
			if err != nil || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || read.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_NOT_APPLICABLE || gateway.refunds != 0 {
				t.Fatalf("unissued charge did not close from its owner evidence: %v %v %v", operation, read, err)
			}
		})
	}
}

type mismatchedOriginalRefund struct{ deletionGatewayClient }

func (g *mismatchedOriginalRefund) Refund(ctx context.Context, r *api.WalletRefundCommand, options ...grpc.CallOption) (*api.WalletOperation, error) {
	refund, err := g.deletionGatewayClient.Refund(ctx, r, options...)
	if err == nil {
		refund.OriginalChargeOperationId = proto.String("another-original-charge")
	}
	return refund, err
}
func TestDeletionRefusesARefundForAnotherOriginalCharge(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	service.Gateway = &mismatchedOriginalRefund{}
	call.IdempotencyKey = "reject-another-original-refund"
	operation, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	read, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil || operation.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || read.GetRefundStatus() == api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED {
		t.Fatalf("another original charge was accepted as this settlement: %v %v %v", operation, read, err)
	}
}

type inFlightOriginalReservation struct {
	absentOriginalServe
	entered chan struct{}
	release chan struct{}
}

func (s *inFlightOriginalReservation) Reserve(ctx context.Context, r *api.RuntimeReservationCommand, opts ...grpc.CallOption) (*api.RuntimeReservation, error) {
	close(s.entered)
	select {
	case <-s.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return (&runtimeServeClient{}).Reserve(ctx, r, opts...)
}
func (s *inFlightOriginalReservation) Deploy(context.Context, *api.RuntimeDeployCommand, ...grpc.CallOption) (*api.RuntimeReadback, error) {
	return nil, status.Error(codes.Unavailable, "original creation must not progress after closure")
}
func TestDeletionCancelsCreateWhileItsOriginalOwnerResponseIsInFlight(t *testing.T) {
	db := runtimeDatabase(t)
	service, original, accepted, _ := seedRuntimeOrder(t, db)
	service.Identity = &deletionIdentity{}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	service.Capability = &runtimeCapabilityClient{version: runtimeVersion(t, accepted.GetQuote().GetCapabilityVersionId())}
	serve := &inFlightOriginalReservation{entered: make(chan struct{}), release: make(chan struct{})}
	service.Serve = serve
	completed := make(chan error, 1)
	go func() { completed <- service.Resume(t.Context(), original.ID) }()
	select {
	case <-serve.entered:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	service.Fabric = nil
	call, ctx := deletionCallContext(original.TenantID)
	call.IdempotencyKey = "close-in-flight-create"
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	operation, err := service.DeleteWorkspace(bounded, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: original.ResourceID, Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	close(serve.release)
	workerErr := <-completed
	if err != nil || operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_DELETE_WORKSPACE {
		t.Fatalf("closing intent waited for an in-flight create instead of fencing its lease: %v %v", operation, err)
	}
	if workerErr != nil && status.Code(workerErr) != codes.Aborted {
		t.Fatalf("cancelled worker returned an unexpected outcome: %v", workerErr)
	}
	read, err := service.GetOperation(ctx, &api.GetOperationRpcRequest{Context: call, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, OperationId: original.ID})
	if err != nil || read.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_CANCELLED {
		t.Fatalf("late create response changed the cancelled original: %v %v", read, err)
	}
}
