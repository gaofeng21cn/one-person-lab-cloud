package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
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

// deletionIdentity answers the Workspace owner's deletion authorization with the
// exact decision CloudIdentity would issue, including the authorization context
// the accepted deletion commit is bound to.
type deletionIdentity struct {
	api.CloudIdentityAuthorizationClient
	denial error
}

func (i *deletionIdentity) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	if i.denial != nil {
		return nil, i.denial
	}
	return &api.AuthorizationDecision{
		Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		ActorId: r.GetActorId(), Scope: r.GetScope(), SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId,
		AudienceOwner: r.GetAudienceOwner(), Action: r.GetAction(), Resource: r.GetResource(),
		AuthorizationContextId: proto.String("authorization-deletion"),
		PermissionVersion:      1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}, nil
}

// IssueAcceptedOperationGrant answers the deletion's own accepted grant. The
// grant the real CloudIdentity issues is bound to the deletion's actor and its
// DELETEWORKSPACE commit, so the fixture reproduces exactly that identity.
func (i *deletionIdentity) IssueAcceptedOperationGrant(_ context.Context, r *api.AcceptedOperationGrantRequest, _ ...grpc.CallOption) (*api.AcceptedOperationGrant, error) {
	if i.denial != nil {
		return nil, i.denial
	}
	commit := r.GetOwnerCommitEvidence()
	if commit.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || commit.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE {
		return nil, status.Error(codes.PermissionDenied, "grant is not bound to a Workspace deletion")
	}
	return &api.AcceptedOperationGrant{
		Id: "grant-deletion-original", AcceptedOperationOwner: commit.GetOwner(), AcceptedOperationId: commit.GetOperationId(),
		AcceptedAction: commit.GetAcceptedAction(), ResourceId: commit.GetResourceId(), ActorId: commit.GetActorId(), Scope: commit.GetScope(),
		AllowedActions: append([]api.AuthorizationActionEnum(nil), r.GetAllowedActions()...),
	}, nil
}

// deletionServeClient records the exact retire command and refuses a substitute
// runtime, so a deletion cannot stop a different application instance.
type deletionServeClient struct {
	api.ServeAgentCoordinationClient
	stops int
}

func (c *deletionServeClient) Retire(_ context.Context, r *api.RuntimeStopCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	c.stops++
	if r.GetRuntimeInstanceId() != "runtime-original" || r.GetDeploymentId() != "deployment-original" {
		return nil, status.Error(codes.NotFound, "different runtime")
	}
	return &api.Operation{OperationId: "serve-retire-original", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_RETIRE, ResourceId: r.GetRuntimeInstanceId(), Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_RETIREMENT, ObservationResult: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED.Enum()}, nil
}

// deletionFabricClient answers one resource deletion for the exact original
// resource set and proves absence only through its own readback.
type deletionFabricClient struct {
	api.FabricCoordinationClient
	deleteCalls, readCalls int
	present                bool
}

func (c *deletionFabricClient) DeleteResources(_ context.Context, r *api.MutateResourcesCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	c.deleteCalls++
	if r.GetWorkspaceId() != "workspace-original" || r.GetResourceSetId() != "resource-set-original" {
		return nil, status.Error(codes.InvalidArgument, "different original resources")
	}
	// A deletion must name the version the resource set holds now, not one observed
	// when the order was launched: Fabric refuses a stale expected version, so the
	// chain would otherwise never release a set that changed after launch.
	if r.GetExpectedResourceVersion() != "version-current" {
		return nil, status.Error(codes.FailedPrecondition, "resource set changed since it was read")
	}
	return &api.Operation{OperationId: "fabric-delete-original", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC, ResourceId: r.GetResourceSetId(), Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_RUNNING, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_RETIREMENT}, nil
}

func (c *deletionFabricClient) ReadResources(_ context.Context, r *api.ResourceReadbackRequest, _ ...grpc.CallOption) (*api.ResourceReadback, error) {
	c.readCalls++
	if r.GetResourceSetId() != "resource-set-original" {
		return nil, status.Error(codes.NotFound, "different resources")
	}
	if c.present {
		return &api.ResourceReadback{ResourceSetId: r.GetResourceSetId(), WorkspaceId: "workspace-original", ResourceVersion: "version-current", Outcome: api.Observation_OBSERVATION_CONFIRMED, Resources: []*api.ResourceFact{{Id: "compute-original", Kind: contracts.WorkspaceDeleteResourceMachine, State: "active"}}, ObservedAt: timestamppb.Now()}, nil
	}
	return &api.ResourceReadback{ResourceSetId: r.GetResourceSetId(), WorkspaceId: "workspace-original", ResourceVersion: "version-current", Outcome: api.Observation_OBSERVATION_CONFIRMED, AbsenceConfirmed: true, ObservedAt: timestamppb.Now()}, nil
}

// deletionLedgerClient records one deletion receipt for the deletion operation
// and reads it back by its original owner reference.
type deletionLedgerClient struct {
	api.LedgerCoordinationClient
	receipt *api.Receipt
	appends int
}

func (c *deletionLedgerClient) AppendReceipt(_ context.Context, r *api.AppendReceiptRequest, _ ...grpc.CallOption) (*api.Receipt, error) {
	c.appends++
	if r.GetReceipt().GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION || r.GetReceipt().GetOperationId() == "" || r.GetReceipt().GetOperationId() != r.GetOwnerEvidenceReference() {
		return nil, status.Error(codes.InvalidArgument, "different deletion evidence")
	}
	if r.GetOwnerCommitEvidence().GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE || r.GetOwnerCommitEvidence().GetOperationId() != r.GetReceipt().GetOperationId() {
		return nil, status.Error(codes.InvalidArgument, "deletion commit required")
	}
	c.receipt = &api.Receipt{Id: "receipt-deletion-original", Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(r.GetReceipt().GetOperationId()), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, CreatedAt: timestamppb.Now()}
	return proto.Clone(c.receipt).(*api.Receipt), nil
}

func (c *deletionLedgerClient) ReadReceiptByReference(_ context.Context, r *api.GetReceiptByReferenceRequest, _ ...grpc.CallOption) (*api.Receipt, error) {
	if c.receipt == nil || r.GetOwner() != "workspace" || r.GetOwnerEvidenceReference() != c.receipt.GetOperationId() {
		return nil, status.Error(codes.NotFound, "no deletion receipt")
	}
	return proto.Clone(c.receipt).(*api.Receipt), nil
}

// deletionGatewayClient refunds the original confirmed charge exactly once and
// refuses any command that does not name that original wallet operation.
type deletionGatewayClient struct {
	api.GatewayCoordinationClient
	refunds      int
	refundAmount int64
	// pendingOnce makes the first refund answer non-terminal, exactly like a
	// wallet owner that recorded the request before it could confirm the native
	// effect. The action itself is already issued: only a readback may settle it.
	pendingOnce bool
	// reads counts the readbacks of the original refund command, and readKey
	// records the identity they were requested by.
	reads   int
	readKey string
}

func (c *deletionGatewayClient) Refund(_ context.Context, r *api.WalletRefundCommand, _ ...grpc.CallOption) (*api.WalletOperation, error) {
	c.refunds++
	if r.GetWorkspaceId() != "workspace-original" || r.GetOriginalWalletOperationId() != "wallet-op-charge-original" || r.GetConfirmedDeletionReceiptId() != "receipt-deletion-original" || r.GetRefundPolicyVersionId() != contracts.WorkspaceRefundPolicyVersion || r.GetAmountUsdMicros() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "different original refund")
	}
	c.refundAmount = r.GetAmountUsdMicros()
	status := api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED
	if c.pendingOnce && c.refunds == 1 {
		status = api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REQUESTED
	}
	return &api.WalletOperation{Id: "wallet-op-refund-original", WorkspaceId: proto.String("workspace-original"), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND, AmountUsdMicros: r.GetAmountUsdMicros(), Status: status, CreatedAt: timestamppb.Now()}, nil
}

// ReadWalletAction answers the owner readback for the one original refund. The
// wallet recorded the request, so the same action is what a later read reports;
// a readback that named anything else would be a different obligation.
func (c *deletionGatewayClient) ReadWalletAction(_ context.Context, r *api.WalletReadbackRequest, _ ...grpc.CallOption) (*api.WalletOperation, error) {
	c.reads++
	c.readKey = r.GetOriginalIdempotencyKey()
	if !strings.HasSuffix(r.GetOriginalIdempotencyKey(), ":refund_confirmed_deletion") || c.refundAmount <= 0 {
		return nil, status.Error(codes.InvalidArgument, "readback must name the original refund command")
	}
	return &api.WalletOperation{Id: "wallet-op-refund-original", WorkspaceId: proto.String("workspace-original"), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND, AmountUsdMicros: c.refundAmount, Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED, CreatedAt: timestamppb.Now()}, nil
}

func deletionCallContext(tenant string) (*api.CallContext, context.Context) {
	session := "session-deletion"
	call := &api.CallContext{
		ActorId: "actor-original", SessionId: &session, RequestId: "request-deletion",
		Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}},
	}
	return call, ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
}

// deletionWorkspace seeds one active paid Workspace with its accepted launch
// result, its confirmed subscription period and its original confirmed charge, so
// the deletion can be resolved against real owner rows.
func deletionWorkspace(t *testing.T, db *sql.DB) (*Service, *api.CallContext, context.Context, time.Time, int64) {
	t.Helper()
	ctx := t.Context()
	store, err := ownerstore.New(db, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	periodStart := time.Now().UTC().Add(-100 * time.Hour).Truncate(time.Second)
	periodEnd := periodStart.Add(30 * 24 * time.Hour)
	const totalMicros = int64(720000000)
	quote := &api.Quote{Id: "quote-original", Status: api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED, Purpose: api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY,
		CapabilityVersionId: proto.String("capability-original"), ComputePlanId: "compute-original", StoragePlanId: "storage-original",
		PeriodMonths: 1, TotalUsdMicros: totalMicros, PeriodStart: timestamppb.New(periodStart), PeriodEnd: timestamppb.New(periodEnd),
		PricePolicyVersionId: "workspace-price-v1", ExpiresAt: timestamppb.New(time.Now().Add(time.Hour))}
	snapshot, err := protojson.Marshal(&api.QuoteAcceptance{Quote: quote, ResourcePlan: &api.ResourcePlanSnapshot{ComputePlanId: "compute-original", StoragePlanId: "storage-original", PrepaidMonths: 1}, WorkspaceId: "workspace-original", ObligationId: "operation-original", AcceptanceId: "acceptance-original", SnapshotDigest: "sha256:original"})
	if err != nil {
		t.Fatal(err)
	}
	launchResult, err := json.Marshal(orderResult{
		GrantID: "grant-original", ResourceSetID: "resource-set-original",
		RuntimeReservation: wire(&api.RuntimeReservation{RuntimeInstanceId: "runtime-original", DeploymentId: "deployment-original", WorkspaceId: "workspace-original", ExecutionEpoch: 1}),
		RuntimeCommand:     wire(&api.RuntimeDeployCommand{WorkspaceId: "workspace-original", RuntimeInstanceId: "runtime-original", DeploymentId: "deployment-original", ExecutionEpoch: 1}),
		ResourceReadback: wire(&api.ResourceReadback{ResourceSetId: "resource-set-original", WorkspaceId: "workspace-original", ResourceVersion: "1", Outcome: api.Observation_OBSERVATION_CONFIRMED,
			ExecutionResources: &api.ResourceExecutionBinding{AccountId: "account-original", ComputeAllocationId: "compute-original", StorageVolumeId: "storage-original", DataAttachmentId: "attachment-original", DataAttachmentOperationId: "attachment-operation-original"}}),
	})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.workspaces(id,tenant_id,name,status,compute_plan_id,storage_plan_id,created_by,version) VALUES('workspace-original','tenant-original','Original workspace','active','compute-original','storage-original','actor-original',3)`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input,result,observation_result,completed_at) VALUES('operation-original','tenant-original','actor-original','create_workspace','workspace-original','succeeded','succeeded','request-original','{}'::jsonb,$1,'confirmed',now())`, launchResult); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.subscriptions(id,workspace_id,accepted_quote_id,accepted_quote_snapshot,billing_subject_ref,current_period_start,current_period_end,last_charge_wallet_operation_id,period_months,provenance,renewal_mode,renewal_settings_version,version,current_price_policy_version_id,current_monthly_usd_micros,billing_anchor_day) VALUES('subscription-original','workspace-original','quote-original',$1,'tenant:tenant-original',$2,$3,'wallet-op-charge-original',1,'quoted','manual',0,0,'workspace-price-v1',$4,$5)`, snapshot, periodStart, periodEnd, totalMicros, periodStart.Day()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.subscription_periods(id,subscription_id,quote_id,accepted_quote_snapshot,period_start,period_end,billing_key,charge_wallet_operation_id,charge_receipt_id,provenance) VALUES('period-original','subscription-original','quote-original',$1,$2,$3,'billing:operation-original','wallet-op-charge-original','wallet-receipt-original','quoted')`, snapshot, periodStart, periodEnd); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	call, callCtx := deletionCallContext("tenant-original")
	service := &Service{Store: store, Identity: &deletionIdentity{}, Ledger: &deletionLedgerClient{}, Fabric: &deletionFabricClient{}, Serve: &deletionServeClient{}, Gateway: &deletionGatewayClient{}}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	return service, call, callCtx, periodStart, totalMicros
}

func TestDeleteWorkspaceRequiresExactConfirmationAndDestructionAck(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	// A confirmation name that is not the Workspace's own name must be refused
	// before any deletion intent is recorded.
	bad := proto.Clone(call).(*api.CallContext)
	bad.IdempotencyKey = "idempotency-deletion-1"
	if _, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: bad, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Other name", AcknowledgeDataDestruction: true}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("mismatched confirmation err=%v", err)
	}
	if _, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: bad, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: false}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("missing acknowledgement err=%v", err)
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM workspace.operations WHERE kind='delete_workspace'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("refused deletion recorded %d operations", count)
	}
	// A Workspace with no accepted deletion has no deletion readback to show: the
	// contract requires every WorkspaceDeletion field, so the read reports the
	// absence instead of inventing an operation, a status or a timestamp.
	if _, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: bad, WorkspaceId: "workspace-original"}); status.Code(err) != codes.NotFound {
		t.Fatalf("deletion readback before any deletion err=%v", err)
	}
	if _, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: bad, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}}); err != nil {
		t.Fatal(err)
	}
}

func TestDeleteWorkspaceRecordsOneOperationBoundToTheOriginalPayment(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, periodStart, totalMicros := deletionWorkspace(t, db)
	// Dependencies are held back so this test observes the accepted intent and the
	// frozen facts before the ordered cleanup runs.
	service.Serve, service.Fabric = nil, nil
	call.IdempotencyKey = "idempotency-deletion-2"
	first, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if first.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_DELETE_WORKSPACE || first.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE || first.GetResourceId() != "workspace-original" {
		t.Fatalf("deletion operation=%v", first)
	}
	// A second deletion intent for the same Workspace is the same deletion: the
	// owner returns the one unfinished operation instead of minting a second one.
	second := proto.Clone(call).(*api.CallContext)
	second.IdempotencyKey = "idempotency-deletion-3"
	replay, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: second, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if replay.GetOperationId() != first.GetOperationId() {
		t.Fatalf("second deletion operation=%s, want %s", replay.GetOperationId(), first.GetOperationId())
	}
	var count int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM workspace.operations WHERE kind='delete_workspace'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("delete operations=%d, want 1", count)
	}
	var raw []byte
	if err = db.QueryRowContext(ctx, `SELECT accepted_input FROM workspace.operations WHERE id=$1`, first.GetOperationId()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var order deletionOrder
	if err = json.Unmarshal(raw, &order); err != nil {
		t.Fatal(err)
	}
	if order.GrantID != "grant-original" || order.RuntimeInstanceID != "runtime-original" || order.DeploymentID != "deployment-original" || order.DataAttachmentID != "attachment-original" || order.ResourceSetID != "resource-set-original" {
		t.Fatalf("frozen launch facts=%+v", order)
	}
	if order.SubscriptionPeriodID != "period-original" || order.OriginalChargeWalletOperationID != "wallet-op-charge-original" || order.OriginalChargeReceiptID != "wallet-receipt-original" || order.OriginalChargeUSDMicros != totalMicros || order.RefundPolicyVersionID != contracts.WorkspaceRefundPolicyVersion {
		t.Fatalf("frozen payment facts=%+v", order)
	}
	if order.PeriodStart != periodStart.Format(time.RFC3339Nano) {
		t.Fatalf("frozen period start=%q, want %q", order.PeriodStart, periodStart.Format(time.RFC3339Nano))
	}
	var status string
	if err = db.QueryRowContext(ctx, `SELECT status FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "deleting" {
		t.Fatalf("workspace status=%s, want deleting", status)
	}
}

func TestDeleteWorkspaceResumeRetiresDeletesRefundsOnceAndReportsReadback(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, periodStart, totalMicros := deletionWorkspace(t, db)
	call.IdempotencyKey = "idempotency-deletion-4"
	created, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	serve, fabric, ledger, gateway := service.Serve.(*deletionServeClient), service.Fabric.(*deletionFabricClient), service.Ledger.(*deletionLedgerClient), service.Gateway.(*deletionGatewayClient)
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	// The second resume replays the recorded identities: no second retire, delete
	// or refund is issued for the same Workspace deletion.
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	if serve.stops != 1 || fabric.deleteCalls != 1 || ledger.appends != 1 || gateway.refunds != 1 {
		t.Fatalf("retire=%d delete=%d receipt=%d refund=%d, want one each", serve.stops, fabric.deleteCalls, ledger.appends, gateway.refunds)
	}
	var status string
	var deletedAt sql.NullTime
	if err = db.QueryRowContext(ctx, `SELECT status,deleted_at FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&status, &deletedAt); err != nil {
		t.Fatal(err)
	}
	if status != "deleted" || !deletedAt.Valid {
		t.Fatalf("workspace status=%s deletedAt=%v", status, deletedAt)
	}
	readback, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if readback.GetOperationId() != created.GetOperationId() || readback.GetResourceDeletionStatus() != api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_CONFIRMED || readback.GetDataDeletionStatus() != api.WorkspaceDeletionDataDeletionStatusEnum_WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_CONFIRMED || readback.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED || readback.GetRefundOperationId() != "wallet-op-refund-original" {
		t.Fatalf("deletion readback=%v", readback)
	}
	wantRefund, err := contracts.PlatformWorkspaceDeleteRefundMicros(totalMicros, periodStart, deletedAt.Time)
	if err != nil {
		t.Fatal(err)
	}
	if wantRefund <= 0 {
		t.Fatalf("expected a partial refund, got %d", wantRefund)
	}
	if gateway.refundAmount != wantRefund {
		t.Fatalf("refunded %d, want the platform policy amount %d", gateway.refundAmount, wantRefund)
	}
}

func TestDeleteWorkspaceWithoutConfirmedAbsenceDoesNotRefund(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	service.Fabric.(*deletionFabricClient).present = true
	call.IdempotencyKey = "idempotency-deletion-5"
	created, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	if refunds := service.Gateway.(*deletionGatewayClient).refunds; refunds != 0 {
		t.Fatalf("refunds=%d without confirmed absence", refunds)
	}
	var status string
	if err = db.QueryRowContext(ctx, `SELECT status FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "deleting" {
		t.Fatalf("workspace status=%s, want deleting while resources are present", status)
	}
	readback, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if readback.GetResourceDeletionStatus() != api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_PENDING {
		t.Fatalf("resource deletion=%v, want pending", readback.GetResourceDeletionStatus())
	}
	if readback.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_UNKNOWN {
		t.Fatalf("refund status=%v, want unknown before any refund", readback.GetRefundStatus())
	}
}

func TestDeleteWorkspaceWaitsForItsOwnersInsteadOfFabricatingFacts(t *testing.T) {
	// Each owner's absence is a separate fact. Without Serve the deletion cannot
	// retire the application, without Ledger it cannot record the confirmed
	// deletion, and without the wallet authority it cannot settle the refund. None
	// of the three may be reported as done, and no later stage may run.
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	service.Serve, service.Ledger, service.Gateway = nil, nil, nil
	call.IdempotencyKey = "idempotency-deletion-6"
	created, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	if deletes := service.Fabric.(*deletionFabricClient).deleteCalls; deletes != 0 {
		t.Fatalf("Fabric deletions=%d before the runtime retirement", deletes)
	}
	var status, observation string
	if err = db.QueryRowContext(ctx, `SELECT status,COALESCE(observation_result,'') FROM workspace.operations WHERE id=$1`, created.GetOperationId()).Scan(&status, &observation); err != nil {
		t.Fatal(err)
	}
	if status != "awaiting_confirmation" || observation != "unknown" {
		t.Fatalf("deletion status=%s observation=%s, want an unresolved retirement", status, observation)
	}
	if status := workspaceStatus(t, db); status != "deleting" {
		t.Fatalf("workspace status=%s, want deleting while the runtime is not retired", status)
	}
	readback, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if readback.GetResourceDeletionStatus() != api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_UNKNOWN ||
		readback.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_UNKNOWN {
		t.Fatalf("deletion readback=%v, want unresolved resources and refund", readback)
	}
}

func TestDeleteWorkspaceWithoutTheWalletAuthorityDoesNotSettleTheCharge(t *testing.T) {
	// The deletion is confirmed and recorded, but the owner of the money is not
	// present. A paid Workspace must not be reported as having nothing to refund:
	// the settlement stays unknown until Gateway answers.
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	service.Gateway = nil
	call.IdempotencyKey = "idempotency-deletion-7"
	created, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	if ledger := service.Ledger.(*deletionLedgerClient); ledger.appends != 1 {
		t.Fatalf("deletion receipts=%d, want the confirmed deletion recorded once", ledger.appends)
	}
	readback, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if readback.GetResourceDeletionStatus() != api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_CONFIRMED {
		t.Fatalf("resource deletion=%v, want the confirmed absence", readback.GetResourceDeletionStatus())
	}
	if readback.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_UNKNOWN {
		t.Fatalf("refund status=%v, want unknown without the wallet authority", readback.GetRefundStatus())
	}
	if status := workspaceStatus(t, db); status != "deleting" {
		t.Fatalf("workspace status=%s, want deleting until the refund is settled", status)
	}
	// The wallet authority, once present, still completes the same deletion from
	// the identities the operation recorded instead of restarting the chain.
	service.Gateway = &deletionGatewayClient{}
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	if refunds := service.Gateway.(*deletionGatewayClient).refunds; refunds != 1 {
		t.Fatalf("refunds=%d, want the one original settlement", refunds)
	}
	if status := workspaceStatus(t, db); status != "deleted" {
		t.Fatalf("workspace status=%s, want deleted after the refund settled", status)
	}
}

// TestDeleteWorkspaceReadsBackItsNonTerminalRefundInsteadOfReissuingIt proves the
// deletion settlement converges on the wallet owner's own answer. The original
// refund answers non-terminal, so a later pass must read that exact action back by
// its original command and complete only when the owner confirms it; re-issuing
// the refund could move the customer's money twice and is never allowed.
func TestDeleteWorkspaceReadsBackItsNonTerminalRefundInsteadOfReissuingIt(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	gateway := &deletionGatewayClient{pendingOnce: true}
	service.Gateway = gateway
	call.IdempotencyKey = "idempotency-deletion-8"
	created, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if gateway.refunds != 1 {
		t.Fatalf("refunds=%d, want the one original request", gateway.refunds)
	}
	readback, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if readback.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_REQUESTED {
		t.Fatalf("refund status=%v, want the recorded non-terminal request", readback.GetRefundStatus())
	}
	if status := workspaceStatus(t, db); status != "deleting" {
		t.Fatalf("workspace status=%s, want deleting while the refund is unresolved", status)
	}
	// The wallet owner recorded the request without a terminal answer. The next
	// pass resolves it by reading the same action back, never by a second refund.
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	if gateway.refunds != 1 {
		t.Fatalf("refunds=%d, want the refund read back instead of re-issued", gateway.refunds)
	}
	if gateway.reads != 1 || gateway.readKey != created.GetOperationId()+":refund_confirmed_deletion" {
		t.Fatalf("readbacks=%d key=%q, want one readback of the original refund command", gateway.reads, gateway.readKey)
	}
	if status := workspaceStatus(t, db); status != "deleted" {
		t.Fatalf("workspace status=%s, want deleted after the refund was confirmed", status)
	}
	readback, err = service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if readback.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED {
		t.Fatalf("refund status=%v, want the wallet-confirmed settlement", readback.GetRefundStatus())
	}
}

func TestDeleteWorkspaceRunsUnderItsOwnAcceptedGrant(t *testing.T) {
	// The deletion is its own accepted obligation. An administrator who did not
	// place the original order can still delete the Workspace, and the chain runs
	// under that administrator's own grant instead of the launch's.
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	call.ActorId = "actor-second-admin"
	call.IdempotencyKey = "idempotency-deletion-8"
	created, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = db.QueryRowContext(ctx, `SELECT result FROM workspace.operations WHERE id=$1`, created.GetOperationId()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result deletionResult
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.GrantID != "grant-deletion-original" {
		t.Fatalf("deletion grant=%q, want the deletion's own accepted grant", result.GrantID)
	}
	if serve, fabric, ledger, gateway := service.Serve.(*deletionServeClient), service.Fabric.(*deletionFabricClient), service.Ledger.(*deletionLedgerClient), service.Gateway.(*deletionGatewayClient); serve.stops != 1 || fabric.deleteCalls != 1 || ledger.appends != 1 || gateway.refunds != 1 {
		t.Fatalf("retire=%d delete=%d receipt=%d refund=%d, want one each", serve.stops, fabric.deleteCalls, ledger.appends, gateway.refunds)
	}
	if status := workspaceStatus(t, db); status != "deleted" {
		t.Fatalf("workspace status=%s, want deleted", status)
	}
}

func workspaceStatus(t *testing.T, db *sql.DB) string {
	t.Helper()
	var status string
	if err := db.QueryRowContext(t.Context(), `SELECT status FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&status); err != nil {
		t.Fatal(err)
	}
	return status
}
