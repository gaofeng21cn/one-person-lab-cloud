package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync/atomic"
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
	// grantActions records what the deletion asked its own grant to carry. The
	// real CloudIdentity issues a delete_workspace grant only from the canonical
	// deletion surface, so the fixture refuses the provisioning write exactly
	// like the owning surface does.
	grantActions []api.AuthorizationActionEnum
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
	i.grantActions = append([]api.AuthorizationActionEnum(nil), r.GetAllowedActions()...)
	for _, action := range i.grantActions {
		if action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES {
			return nil, status.Error(codes.PermissionDenied, "the deletion's grant cannot carry the launch's provisioning write")
		}
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
	// The managed-key closeout is a separate Gateway-coordination request. It
	// records the exact binding the deletion asked for and can be scripted to
	// refuse or lose its answer, like the real owner route.
	disableCalls       int
	disableBindingID   string
	disableWorkspace   string
	disableGrant       string
	disableIdempotency string
	disableErr         error
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
	return &api.WalletOperation{Id: "wallet-op-refund-original", WorkspaceId: proto.String("workspace-original"), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND, OriginalChargeOperationId: proto.String("wallet-op-charge-original"), AmountUsdMicros: r.GetAmountUsdMicros(), Status: status, CreatedAt: timestamppb.Now()}, nil
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
	return &api.WalletOperation{Id: "wallet-op-refund-original", WorkspaceId: proto.String("workspace-original"), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND, OriginalChargeOperationId: proto.String("wallet-op-charge-original"), AmountUsdMicros: c.refundAmount, Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED, CreatedAt: timestamppb.Now()}, nil
}

// DisableManagedKey answers the key-closeout request with the owner's confirmed
// readback for the exact binding. A scripted error stands in for the production
// refusal or a lost answer, so one test can show both are recorded as facts
// instead of being mistaken for a disabled key.
func (c *deletionGatewayClient) DisableManagedKey(_ context.Context, r *api.ManagedKeyLifecycleCommand, _ ...grpc.CallOption) (*api.ManagedKeyLifecycleReadback, error) {
	c.disableCalls++
	c.disableBindingID = r.GetKeyBindingId()
	c.disableWorkspace = r.GetWorkspaceId()
	c.disableGrant = r.GetContext().GetAcceptedOperationGrantId()
	c.disableIdempotency = r.GetContext().GetIdempotencyKey()
	if c.disableErr != nil {
		return nil, c.disableErr
	}
	if r.GetOriginalCreateIdempotencyKey() != "" && r.GetKeyBindingId() == "" {
		return &api.ManagedKeyLifecycleReadback{WorkspaceId: r.GetWorkspaceId(), AbsenceConfirmed: true, Outcome: api.Observation_OBSERVATION_CONFIRMED, UpdatedAt: timestamppb.Now()}, nil
	}
	return &api.ManagedKeyLifecycleReadback{KeyBindingId: r.GetKeyBindingId(), WorkspaceId: r.GetWorkspaceId(),
		State: api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED, LifecycleVersion: 1,
		Outcome: api.Observation_OBSERVATION_CONFIRMED, UpdatedAt: timestamppb.Now()}, nil
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
	var workspaceStatus string
	var deletedAt sql.NullTime
	if err = db.QueryRowContext(ctx, `SELECT status,deleted_at FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&workspaceStatus, &deletedAt); err != nil {
		t.Fatal(err)
	}
	if workspaceStatus != "deleted" || !deletedAt.Valid {
		t.Fatalf("workspace status=%s deletedAt=%v", workspaceStatus, deletedAt)
	}
	readback, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if readback.GetOperationId() != created.GetOperationId() || readback.GetResourceDeletionStatus() != api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_CONFIRMED || readback.GetDataDeletionStatus() != api.WorkspaceDeletionDataDeletionStatusEnum_WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_CONFIRMED || readback.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED || readback.GetRefundOperationId() != "wallet-op-refund-original" {
		t.Fatalf("deletion readback=%v", readback)
	}
	if readback.GetDeletionReceipt() == nil || validateDeletionReceipt(ownerstore.Operation{ID: created.GetOperationId()}, readback.GetDeletionReceipt()) != nil {
		t.Fatalf("deletion readback lost the original Ledger evidence: %v", readback)
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
	// A stored receipt for another deletion must never become this customer's
	// evidence just because the operation status still says succeeded.
	if _, err = db.ExecContext(ctx, `UPDATE workspace.operations SET result=jsonb_set(result,'{deletionReceipt,operationId}',to_jsonb($2::text)) WHERE id=$1`, created.GetOperationId(), "another-deletion"); err != nil {
		t.Fatal(err)
	}
	if _, err = service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"}); status.Code(err) != codes.DataLoss {
		t.Fatalf("foreign deletion receipt was published: %v", err)
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
	if status := workspaceStatus(t, db); status != "deleted" {
		t.Fatalf("workspace status=%s, want deleted independently of the unresolved refund", status)
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
	if status := workspaceStatus(t, db); status != "deleted" {
		t.Fatalf("workspace status=%s, want deleted while the refund is independently unresolved", status)
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
	// The deletion's own grant carries the contract's resource release and never
	// the launch's provisioning write: Fabric's DeleteResources authorizes itself
	// with deleteAcceptedResources, while a grant that carried the creation write
	// could replay resource creation.
	identity := service.Identity.(*deletionIdentity)
	releases := 0
	for _, action := range identity.grantActions {
		if action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PROVISIONACCEPTEDRESOURCES {
			t.Fatalf("the deletion grant requested the launch's provisioning write: %v", identity.grantActions)
		}
		if action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEACCEPTEDRESOURCES {
			releases++
		}
	}
	if releases != 1 {
		t.Fatalf("the deletion grant does not carry the resource-release action: %v", identity.grantActions)
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

// Hold the first post-lock authorization while a second administrator enters.
// The second call must wait on the Workspace row, rather than authorize a
// second deletion before the first accepted intent commits.
type concurrentDeletionIdentity struct {
	*deletionIdentity
	calls         atomic.Int32
	firstEntered  chan struct{}
	secondEntered chan struct{}
	releaseFirst  chan struct{}
}

func (i *concurrentDeletionIdentity) AuthorizeAction(ctx context.Context, r *api.AuthorizationRequest, options ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	switch i.calls.Add(1) {
	case 1:
		close(i.firstEntered)
		select {
		case <-i.releaseFirst:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	case 2:
		close(i.secondEntered)
	}
	return i.deletionIdentity.AuthorizeAction(ctx, r, options...)
}
func (i *concurrentDeletionIdentity) IssueAcceptedOperationGrant(context.Context, *api.AcceptedOperationGrantRequest, ...grpc.CallOption) (*api.AcceptedOperationGrant, error) {
	return nil, status.Error(codes.Unavailable, "hold cleanup for acceptance test")
}

// Signal command overlap at the external authorization seam, independent of
// whether the owner serializes through row or advisory locks.
type deletionPreflightIdentity struct {
	modelReadIdentity
	secondEntered chan struct{}
	seen          atomic.Bool
}

func (i *deletionPreflightIdentity) AuthorizeAction(ctx context.Context, r *api.AuthorizationRequest, opts ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	if r.GetActorId() == "admin-two" && i.seen.CompareAndSwap(false, true) {
		close(i.secondEntered)
	}
	return i.modelReadIdentity.AuthorizeAction(ctx, r, opts...)
}

func TestConcurrentDeletionIntentsShareOneWorkspaceOperation(t *testing.T) {
	db := runtimeDatabase(t)
	// Two transactions hold connections while the observer checks the row lock.
	db.SetMaxOpenConns(4)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	identity := &concurrentDeletionIdentity{deletionIdentity: &deletionIdentity{}, firstEntered: make(chan struct{}), secondEntered: make(chan struct{}), releaseFirst: make(chan struct{})}
	service.Identity = identity
	preflight := &deletionPreflightIdentity{secondEntered: make(chan struct{})}
	service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, preflight)
	released := false
	defer func() {
		if !released {
			close(identity.releaseFirst)
		}
	}()
	type answer struct {
		operation *api.Operation
		err       error
	}
	replies := make(chan answer, 2)
	request := func(actor, key string) {
		own := proto.Clone(call).(*api.CallContext)
		own.ActorId, own.IdempotencyKey = actor, key
		op, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: own, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
		replies <- answer{op, err}
	}
	go request("admin-one", "delete-one")
	select {
	case <-identity.firstEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("first deletion did not reach authorization")
	}
	go request("admin-two", "delete-two")
	select {
	case <-preflight.secondEntered:
	case <-time.After(10 * time.Second):
		t.Fatal("second deletion did not reach its public authorization boundary")
	}
	close(identity.releaseFirst)
	released = true
	first, second := <-replies, <-replies
	if first.err != nil || second.err != nil {
		t.Fatalf("deletion errors: %v / %v", first.err, second.err)
	}
	if first.operation.GetOperationId() != second.operation.GetOperationId() {
		t.Fatalf("concurrent requests accepted distinct deletion operations: %s / %s", first.operation.GetOperationId(), second.operation.GetOperationId())
	}
	readback, err := service.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil || readback.GetOperationId() != first.operation.GetOperationId() {
		t.Fatalf("deletion readback differs from the shared intent: %v %v", readback, err)
	}

}

// A persistent dependency refusal cannot keep the automatic worker issuing
// commands forever. Attention retains the original identities and unknown fact;
// an explicitly authorized replay may resume them after the owner is repaired.
type refusingDeletionLedger struct {
	*deletionLedgerClient
	calls int
}

func (l *refusingDeletionLedger) AppendReceipt(context.Context, *api.AppendReceiptRequest, ...grpc.CallOption) (*api.Receipt, error) {
	l.calls++
	return nil, status.Error(codes.Unavailable, "Ledger temporarily unavailable")
}
func TestDeletionRecoveryStopsAndExplicitReplayUsesOriginalIntent(t *testing.T) {
	for _, missingFabric := range []bool{false, true} {
		name := "ledger_refusal"
		if missingFabric {
			name = "missing_fabric"
		}
		t.Run(name, func(t *testing.T) {
			db := runtimeDatabase(t)
			service, call, ctx, _, _ := deletionWorkspace(t, db)
			fabric := service.Fabric
			ledger := service.Ledger
			refusing := &refusingDeletionLedger{deletionLedgerClient: &deletionLedgerClient{}}
			if missingFabric {
				service.Fabric = nil
			} else {
				service.Ledger = refusing
			}
			call.IdempotencyKey = "bounded-delete"
			request := &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}}
			accepted, err := service.DeleteWorkspace(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			operationID := accepted.GetOperationId()
			original, err := service.Store.ReadOperation(ctx, operationID)
			if err != nil {
				t.Fatal(err)
			}
			for attempt := 1; attempt < deletionRecoveryLimit; attempt++ {
				// Advance only the test clock's persisted scheduling gate, without sleep.
				if _, err = db.ExecContext(ctx, `UPDATE workspace.saga_steps SET next_attempt_at=NULL WHERE operation_id=$1`, operationID); err != nil {
					t.Fatal(err)
				}
				_ = service.RunOnce(ctx)
			}
			stored, err := service.Store.ReadOperation(ctx, operationID)
			if err != nil {
				t.Fatal(err)
			}
			_, result, err := decodeDeletion(stored)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Status != "needs_attention" || stored.Observation != "unknown" {
				t.Fatalf("unconfirmed recovery=%+v result=%+v", stored, result)
			}
			if service.Gateway.(*deletionGatewayClient).refunds != 0 {
				t.Fatal("refunded without Ledger/absence evidence")
			}
			var workspaceState string
			if err = db.QueryRowContext(ctx, `SELECT status FROM workspace.workspaces WHERE id=$1`, "workspace-original").Scan(&workspaceState); err != nil {
				t.Fatal(err)
			}
			if workspaceState != "deleting" {
				t.Fatalf("workspace=%s", workspaceState)
			}
			before := refusing.calls
			for i := 0; i < 3; i++ {
				if err = service.RunOnce(ctx); err != nil {
					t.Fatal(err)
				}
			}
			after, err := service.Store.ReadOperation(ctx, operationID)
			if err != nil {
				t.Fatal(err)
			}
			if refusing.calls != before || string(after.Result) != string(stored.Result) {
				t.Fatal("automatic worker continued after attention")
			}
			// Read-only polls do not resume. Restoring the dependency alone does not
			// authorize another automatic command after the attention boundary.
			service.Fabric, service.Ledger = fabric, ledger
			if err = service.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
			replayed, err := service.DeleteWorkspace(ctx, request)
			if err != nil {
				t.Fatal(err)
			}
			completed, err := service.Store.ReadOperation(ctx, operationID)
			if err != nil {
				t.Fatal(err)
			}
			if replayed.GetOperationId() != operationID || completed.Status != "succeeded" || string(completed.AcceptedInput) != string(original.AcceptedInput) {
				t.Fatal("replay did not complete the original immutable intent")
			}
			if service.Serve.(*deletionServeClient).stops != 1 || service.Fabric.(*deletionFabricClient).deleteCalls != 1 || service.Gateway.(*deletionGatewayClient).refunds != 1 {
				t.Fatal("recovery duplicated retirement/deletion/refund")
			}
		})
	}
}

// This owner fixture advances only on the original DeleteResources caller, just
// as the real Fabric owner does. ReadResources alone must not pass this test.
type pendingCallerDeletionFabric struct {
	*deletionFabricClient
	first *api.MutateResourcesCommand
}

func (f *pendingCallerDeletionFabric) DeleteResources(ctx context.Context, r *api.MutateResourcesCommand, options ...grpc.CallOption) (*api.Operation, error) {
	if f.first == nil {
		f.first = proto.Clone(r).(*api.MutateResourcesCommand)
	} else if !proto.Equal(f.first, r) {
		return nil, status.Error(codes.DataLoss, "retry changed frozen delete request")
	}
	out, err := f.deletionFabricClient.DeleteResources(ctx, r, options...)
	if f.deleteCalls >= 2 {
		f.present = false
	}
	return out, err
}
func TestPendingFabricDeletionResumesThroughItsOriginalCaller(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	fabric := &pendingCallerDeletionFabric{deletionFabricClient: &deletionFabricClient{present: true}}
	service.Fabric = fabric
	call.IdempotencyKey = "pending-fabric-delete"
	created, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err = db.ExecContext(ctx, `UPDATE workspace.saga_steps SET next_attempt_at=NULL WHERE operation_id=$1`, created.GetOperationId()); err != nil {
			t.Fatal(err)
		}
		if err = service.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := service.Store.ReadOperation(ctx, created.GetOperationId())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "succeeded" || fabric.deleteCalls != 2 || service.Gateway.(*deletionGatewayClient).refunds != 1 {
		t.Fatalf("pending deletion status=%s calls=%d refunds=%d", stored.Status, fabric.deleteCalls, service.Gateway.(*deletionGatewayClient).refunds)
	}
}

// deletionWorkspaceWithManagedKey seeds the same paid Workspace, then records the
// confirmed launch-time managed key on its accepted launch result. The deletion
// therefore has one exact binding it must ask its owner to close out.
func deletionWorkspaceWithManagedKey(t *testing.T, db *sql.DB) (*Service, *api.CallContext, context.Context, time.Time, int64) {
	t.Helper()
	service, call, ctx, periodStart, charge := deletionWorkspace(t, db)
	var raw []byte
	if err := db.QueryRowContext(ctx, `SELECT result FROM workspace.operations WHERE id='operation-original'`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var launch orderResult
	if err := json.Unmarshal(raw, &launch); err != nil {
		t.Fatal(err)
	}
	launch.KeyBindingID = "gateway-key-original"
	launch.ManagedKeyBinding = wire(&api.RuntimeManagedKeyBinding{KeyBindingId: "gateway-key-original", SecretBindingId: "secret-binding-original",
		SecretVersion: "1", SecretDeliveryReference: "secret-reference-original", Fingerprint: "sha256:original-key", TargetSlot: "workspace_gateway_key"})
	raw, err := json.Marshal(launch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE workspace.operations SET result=$1 WHERE id='operation-original'`, raw); err != nil {
		t.Fatal(err)
	}
	return service, call, ctx, periodStart, charge
}

func TestDeleteWorkspaceDisablesTheExactManagedKeyThroughItsOwner(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspaceWithManagedKey(t, db)
	gateway := service.Gateway.(*deletionGatewayClient)
	call.IdempotencyKey = "idempotency-deletion-key-1"
	created, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	if gateway.disableCalls != 1 || gateway.disableBindingID != "gateway-key-original" || gateway.disableWorkspace != "workspace-original" ||
		gateway.disableGrant != "grant-deletion-original" || !strings.HasSuffix(gateway.disableIdempotency, ":disable_managed_key") {
		t.Fatalf("key closeout calls=%d binding=%q workspace=%q grant=%q idempotency=%q", gateway.disableCalls, gateway.disableBindingID, gateway.disableWorkspace, gateway.disableGrant, gateway.disableIdempotency)
	}
	// A replay settles the recorded closeout instead of asking the owner again, so a
	// retried deletion can never disable a second time or name another key.
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	if gateway.disableCalls != 1 {
		t.Fatalf("key closeout re-requested %d times", gateway.disableCalls)
	}
	var raw []byte
	if err = db.QueryRowContext(ctx, `SELECT result FROM workspace.operations WHERE id=$1`, created.GetOperationId()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result deletionResult
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.ManagedKeyBindingID != "gateway-key-original" || result.KeyCloseoutOutcome != "confirmed" || result.KeyCloseoutCode != "" {
		t.Fatalf("recorded key closeout=%q binding=%q code=%q", result.KeyCloseoutOutcome, result.ManagedKeyBindingID, result.KeyCloseoutCode)
	}
	var observation string
	if err = db.QueryRowContext(ctx, `SELECT observation_result FROM workspace.saga_steps WHERE operation_id=$1 AND step_key='disable_managed_key'`, created.GetOperationId()).Scan(&observation); err != nil {
		t.Fatal(err)
	}
	if observation != "confirmed" {
		t.Fatalf("key closeout step observation=%q", observation)
	}
}

func TestDeleteWorkspaceRecordsARefusedKeyCloseoutWithoutBlockingDeletion(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspaceWithManagedKey(t, db)
	gateway := service.Gateway.(*deletionGatewayClient)
	// This is the current production answer: the external authority exposes no
	// approved disable endpoint. The key therefore stays live, and the deletion
	// must say so instead of reporting a disabled key or waiting forever.
	gateway.disableErr = status.Error(codes.Unimplemented, "no approved Sub2API service key disable/enable endpoint is configured")
	call.IdempotencyKey = "idempotency-deletion-key-2"
	created, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	var workspaceStatus string
	if err = db.QueryRowContext(ctx, `SELECT status FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&workspaceStatus); err != nil {
		t.Fatal(err)
	}
	if workspaceStatus != "deleted" || gateway.refunds != 1 {
		t.Fatalf("workspace status=%q refunds=%d, want deletion and settlement to complete", workspaceStatus, gateway.refunds)
	}
	var raw []byte
	if err = db.QueryRowContext(ctx, `SELECT result FROM workspace.operations WHERE id=$1`, created.GetOperationId()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result deletionResult
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.KeyCloseoutOutcome != "rejected" || result.KeyCloseoutCode != "KEY_CLOSEOUT_REFUSED" || result.KeyCloseoutOutcome == "confirmed" {
		t.Fatalf("refused closeout recorded as outcome=%q code=%q", result.KeyCloseoutOutcome, result.KeyCloseoutCode)
	}
	var observation string
	if err = db.QueryRowContext(ctx, `SELECT observation_result FROM workspace.saga_steps WHERE operation_id=$1 AND step_key='disable_managed_key'`, created.GetOperationId()).Scan(&observation); err != nil {
		t.Fatal(err)
	}
	if observation != "rejected" {
		t.Fatalf("refused closeout step observation=%q", observation)
	}
}

func TestDeletionOperationProjectsLegacyKeyCloseoutErrors(t *testing.T) {
	for stored, want := range map[string]api.ErrorCodeEnum{
		"KEY_CLOSEOUT_REFUSED":     api.ErrorCodeEnum_ERROR_CODE_ENUM_OWNER_CAPABILITY_UNAVAILABLE,
		"KEY_BINDING_UNRESOLVED":   api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE,
		"KEY_CLOSEOUT_UNKNOWN":     api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE,
		"KEY_CLOSEOUT_UNCONFIRMED": api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE,
	} {
		t.Run(stored, func(t *testing.T) {
			got, err := deletionOperation(ownerstore.Operation{
				ID: "delete-legacy", Kind: "delete_workspace", Status: "needs_attention",
				Stage: "absence_verification", ErrorCode: stored,
			})
			if err != nil {
				t.Fatalf("project stored code %s: %v", stored, err)
			}
			if got.GetErrorCode() != want {
				t.Fatalf("stored code %s projected as %s, want %s", stored, got.GetErrorCode(), want)
			}
		})
	}
}

func TestDeleteWorkspaceRecordsAnUnknownKeyCloseoutWithoutClaimingSuccess(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspaceWithManagedKey(t, db)
	gateway := service.Gateway.(*deletionGatewayClient)
	// A lost answer cannot prove the key was not disabled, so it is recorded as
	// unknown rather than as either a refusal or a confirmed closeout.
	gateway.disableErr = status.Error(codes.Unavailable, "the Gateway answer was lost")
	call.IdempotencyKey = "idempotency-deletion-key-3"
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
	if result.KeyCloseoutOutcome != "unknown" || result.KeyCloseoutCode != "KEY_CLOSEOUT_UNKNOWN" {
		t.Fatalf("unknown closeout recorded as outcome=%q code=%q", result.KeyCloseoutOutcome, result.KeyCloseoutCode)
	}
}

func TestDeleteWorkspaceWithoutAManagedKeyRecordsNoCloseout(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	gateway := service.Gateway.(*deletionGatewayClient)
	call.IdempotencyKey = "idempotency-deletion-key-4"
	created, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Resume(ctx, created.GetOperationId()); err != nil {
		t.Fatal(err)
	}
	if gateway.disableCalls != 0 {
		t.Fatalf("key closeout requested %d times for a Workspace without a managed key", gateway.disableCalls)
	}
	var raw []byte
	if err = db.QueryRowContext(ctx, `SELECT result FROM workspace.operations WHERE id=$1`, created.GetOperationId()).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var result deletionResult
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	if result.KeyCloseoutOutcome != "not_applicable" || result.ManagedKeyBindingID != "" {
		t.Fatalf("closeout outcome=%q binding=%q, want not_applicable and no binding", result.KeyCloseoutOutcome, result.ManagedKeyBindingID)
	}
}
