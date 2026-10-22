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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// lifecycleGatewayClient is the Gateway coordination surface one Workspace
// lifecycle event talks to. It keeps the authoritative external state the real
// owner keeps, so a read that follows a lost answer reports what the Gateway
// would report instead of what the Workspace hoped had happened.
type lifecycleGatewayClient struct {
	api.GatewayCoordinationClient
	state        api.ManagedKeyLifecycleState
	version      int64
	readCalls    int
	readKeyCalls int
	disableCalls int
	enableCalls  int
	refunds      int
	readErr      error
	disableErr   error
	enableErr    error
	// The exact binding, Workspace, grant and idempotency key of the last
	// lifecycle command the Gateway received.
	bindingID   string
	workspace   string
	grant       string
	idempotency string
	actor       string
}

func (c *lifecycleGatewayClient) readback(r *api.ManagedKeyLifecycleCommand) *api.ManagedKeyLifecycleReadback {
	return &api.ManagedKeyLifecycleReadback{KeyBindingId: r.GetKeyBindingId(), WorkspaceId: r.GetWorkspaceId(),
		State: c.state, LifecycleVersion: c.version, Outcome: api.Observation_OBSERVATION_CONFIRMED, UpdatedAt: timestamppb.Now()}
}

func (c *lifecycleGatewayClient) record(r *api.ManagedKeyLifecycleCommand) {
	c.bindingID, c.workspace = r.GetKeyBindingId(), r.GetWorkspaceId()
	c.grant, c.idempotency, c.actor = r.GetContext().GetAcceptedOperationGrantId(), r.GetContext().GetIdempotencyKey(), r.GetContext().GetActorId()
}

func (c *lifecycleGatewayClient) ReadManagedKeyLifecycle(_ context.Context, r *api.ManagedKeyLifecycleCommand, _ ...grpc.CallOption) (*api.ManagedKeyLifecycleReadback, error) {
	c.readCalls++
	c.record(r)
	if c.readErr != nil {
		return nil, c.readErr
	}
	return c.readback(r), nil
}

func (c *lifecycleGatewayClient) DisableManagedKey(_ context.Context, r *api.ManagedKeyLifecycleCommand, _ ...grpc.CallOption) (*api.ManagedKeyLifecycleReadback, error) {
	c.disableCalls++
	c.record(r)
	if c.disableErr != nil {
		return nil, c.disableErr
	}
	c.state, c.version = api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED, c.version+1
	return c.readback(r), nil
}

func (c *lifecycleGatewayClient) EnableManagedKey(_ context.Context, r *api.ManagedKeyLifecycleCommand, _ ...grpc.CallOption) (*api.ManagedKeyLifecycleReadback, error) {
	c.enableCalls++
	c.record(r)
	if c.enableErr != nil {
		return nil, c.enableErr
	}
	if c.state == api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_REVOKED {
		// The real owner refuses a revoked binding in both directions and never
		// issues a replacement.
		return nil, status.Error(codes.FailedPrecondition, "the external Gateway key is revoked and cannot be re-enabled")
	}
	c.state, c.version = api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE, c.version+1
	return c.readback(r), nil
}

// ReadManagedKey is the key owner's usable-binding read. It refuses a revoked or
// disabled binding, so one successful read proves the delivered credential works
// again after a recovery.
func (c *lifecycleGatewayClient) ReadManagedKey(_ context.Context, r *api.ManagedKeyReadCommand, _ ...grpc.CallOption) (*api.ManagedKeyBinding, error) {
	c.readKeyCalls++
	if c.state != api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE {
		return nil, status.Error(codes.FailedPrecondition, "the managed key binding is disabled")
	}
	return &api.ManagedKeyBinding{KeyBindingId: r.GetKeyBindingId(), WorkspaceId: r.GetWorkspaceId(),
		Fingerprint: "sha256:original-key", SecretDeliveryReference: "secret-reference-original", ModelIds: []string{"model-original"}}, nil
}

func (c *lifecycleGatewayClient) Refund(_ context.Context, r *api.WalletRefundCommand, _ ...grpc.CallOption) (*api.WalletOperation, error) {
	c.refunds++
	if r.GetWorkspaceId() != "workspace-original" || r.GetOriginalWalletOperationId() != "wallet-op-charge-original" ||
		r.GetConfirmedDeletionReceiptId() != "receipt-deletion-original" || r.GetAmountUsdMicros() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "different original refund")
	}
	return &api.WalletOperation{Id: "wallet-op-refund-original", WorkspaceId: proto.String("workspace-original"),
		Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND, AmountUsdMicros: r.GetAmountUsdMicros(),
		Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED, CreatedAt: timestamppb.Now()}, nil
}

// lifecycleWorkspace seeds one active paid Workspace with the managed key its
// launch delivered. With expired set, the confirmed paid period ended two hours
// ago, which is the owner's own fact an unpaid term leaves behind.
func lifecycleWorkspace(t *testing.T, db *sql.DB, expired bool) (*Service, *api.CallContext, context.Context, *lifecycleGatewayClient) {
	t.Helper()
	service, call, ctx, _, _ := deletionWorkspaceWithManagedKey(t, db)
	// The original resources still exist unless a test says otherwise, so the
	// provider reconcile is not the trigger under test.
	service.Fabric = &deletionFabricClient{present: true}
	gateway := &lifecycleGatewayClient{state: api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE, version: 1}
	service.Gateway = gateway
	if expired {
		lifecycleSetCurrentPeriod(t, db, ctx, "period-expired", time.Now().UTC().Add(-32*24*time.Hour), time.Now().UTC().Add(-2*time.Hour))
	}
	return service, call, ctx, gateway
}

// lifecycleSetCurrentPeriod makes one newly confirmed paid period the
// subscription's current term. The confirmed period history is immutable, so a
// boundary is a new period row with its own charge and receipt, exactly the shape
// a confirmed renewal leaves behind.
func lifecycleSetCurrentPeriod(t *testing.T, db *sql.DB, ctx context.Context, id string, start, end time.Time) {
	t.Helper()
	start, end = start.UTC().Truncate(time.Second), end.UTC().Truncate(time.Second)
	if _, err := db.ExecContext(ctx, `INSERT INTO workspace.subscription_periods
		(id,subscription_id,quote_id,accepted_quote_snapshot,period_start,period_end,billing_key,charge_wallet_operation_id,charge_receipt_id,provenance)
		SELECT $1,'subscription-original','quote-original',accepted_quote_snapshot,$2,$3,'billing:'||$1,'wallet-op-'||$1,'wallet-receipt-'||$1,'quoted'
		FROM workspace.subscription_periods WHERE id='period-original'`, id, start, end); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE workspace.subscriptions SET current_period_start=$2,current_period_end=$3,last_charge_wallet_operation_id='wallet-op-'||$1
		WHERE id='subscription-original'`, id, start, end); err != nil {
		t.Fatal(err)
	}
}

// lifecycleOperation reads the one lifecycle event of its kind together with its
// recorded order and result.
func lifecycleOperation(t *testing.T, db *sql.DB, ctx context.Context, kind string) (ownerstore.Operation, lifecycleOrder, lifecycleResult) {
	t.Helper()
	var op ownerstore.Operation
	var accepted, result []byte
	err := db.QueryRowContext(ctx, `SELECT id,kind,status,stage,COALESCE(observation_result,''),COALESCE(error_code,''),accepted_input,COALESCE(result,'{}'::jsonb)
		FROM workspace.operations WHERE kind=$1 ORDER BY created_at DESC,id DESC LIMIT 1`, kind).
		Scan(&op.ID, &op.Kind, &op.Status, &op.Stage, &op.Observation, &op.ErrorCode, &accepted, &result)
	if err != nil {
		t.Fatalf("read %s operation: %v", kind, err)
	}
	var order lifecycleOrder
	var recorded lifecycleResult
	if json.Unmarshal(accepted, &order) != nil || json.Unmarshal(result, &recorded) != nil {
		t.Fatalf("%s operation is not a typed lifecycle event", kind)
	}
	return op, order, recorded
}

// readDeletionOperation reads the one deletion of the same Workspace.
func readDeletionOperation(t *testing.T, db *sql.DB, ctx context.Context) (ownerstore.Operation, deletionResult) {
	t.Helper()
	var op ownerstore.Operation
	var result []byte
	err := db.QueryRowContext(ctx, `SELECT id,kind,status,stage,COALESCE(observation_result,''),COALESCE(result,'{}'::jsonb)
		FROM workspace.operations WHERE kind='delete_workspace' ORDER BY created_at DESC,id DESC LIMIT 1`).
		Scan(&op.ID, &op.Kind, &op.Status, &op.Stage, &op.Observation, &result)
	if err != nil {
		t.Fatalf("read deletion operation: %v", err)
	}
	var recorded deletionResult
	if json.Unmarshal(result, &recorded) != nil {
		t.Fatal("deletion operation is not a typed deletion")
	}
	return op, recorded
}

func lifecycleCount(t *testing.T, db *sql.DB, ctx context.Context, kind string) int {
	t.Helper()
	var count int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM workspace.operations WHERE kind=$1`, kind).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func lifecycleWorkspaceStatus(t *testing.T, db *sql.DB, ctx context.Context) string {
	t.Helper()
	var state string
	if err := db.QueryRowContext(ctx, `SELECT status FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func lifecycleAccess(t *testing.T, service *Service, verdict api.WorkspaceAccessVerdict) {
	t.Helper()
	ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.Serve.Service())
	answer, err := service.ReadWorkspaceAccess(ctx, &api.ReadWorkspaceAccessRequest{WorkspaceId: "workspace-original"})
	if err != nil || answer.GetVerdict() != verdict {
		t.Fatalf("access=%v err=%v want=%v", answer, err, verdict)
	}
}

// TestUnpaidExpirySuspendsBeforeItDisablesTheExactManagedKey drives the real
// expiry sweep against real owner rows: the confirmed term that ended is this
// owner's own fact, so the Workspace stops offering its obligation before any
// external owner is asked anything, and the one key it still owns is named
// exactly once under the launch's own grant.
func TestUnpaidExpirySuspendsBeforeItDisablesTheExactManagedKey(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, ctx, gateway := lifecycleWorkspace(t, db, true)
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if state := lifecycleWorkspaceStatus(t, db, ctx); state != "suspended" {
		t.Fatalf("workspace status=%q", state)
	}
	lifecycleAccess(t, service, api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED)
	op, order, result := lifecycleOperation(t, db, ctx, lifecycleSuspend)
	if op.Status != "succeeded" || op.Observation != "confirmed" || op.Stage != lifecycleStageSuspension {
		t.Fatalf("suspension operation=%+v", op)
	}
	if order.Reason != lifecycleReasonUnpaidExpiry || order.Trigger != lifecycleTriggerExpiry || order.GrantID != "grant-original" || order.LaunchOperationID != "operation-original" {
		t.Fatalf("suspension order=%+v", order)
	}
	if result.KeyLifecycleOutcome != lifecycleConfirmed || result.KeyLifecycleAction != "disable" || result.ManagedKeyBindingID != "gateway-key-original" {
		t.Fatalf("suspension key result=%+v", result)
	}
	if result.KeyLifecycleKey != op.ID+":"+lifecycleStep {
		t.Fatalf("key idempotency=%q want %q", result.KeyLifecycleKey, op.ID+":"+lifecycleStep)
	}
	// The Gateway received the exact binding under the original launch grant, and
	// the Workspace never carried a secret or a raw key into its own record.
	if gateway.disableCalls != 1 || gateway.bindingID != "gateway-key-original" || gateway.workspace != "workspace-original" ||
		gateway.grant != "grant-original" || gateway.actor != "actor-original" || gateway.idempotency != op.ID+":"+lifecycleStep {
		t.Fatalf("gateway disable binding=%q workspace=%q grant=%q actor=%q key=%q calls=%d",
			gateway.bindingID, gateway.workspace, gateway.grant, gateway.actor, gateway.idempotency, gateway.disableCalls)
	}
	var step string
	if err := db.QueryRowContext(ctx, `SELECT observation_result FROM workspace.saga_steps WHERE operation_id=$1 AND step_key=$2`, op.ID, lifecycleStep).Scan(&step); err != nil {
		t.Fatal(err)
	}
	if step != "confirmed" {
		t.Fatalf("lifecycle step=%q", step)
	}
	var accepted string
	if err := db.QueryRowContext(ctx, `SELECT accepted_input::text FROM workspace.operations WHERE id=$1`, op.ID).Scan(&accepted); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(accepted, "secret-binding-original") || strings.Contains(accepted, "sha256:original-key") {
		t.Fatalf("lifecycle event stored key material: %s", accepted)
	}
	// A second pass over the same standing is not a second suspension and not a
	// second key effect.
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if gateway.disableCalls != 1 || lifecycleCount(t, db, ctx, lifecycleSuspend) != 1 {
		t.Fatalf("replay disabled %d times over %d events", gateway.disableCalls, lifecycleCount(t, db, ctx, lifecycleSuspend))
	}
}

// TestProviderResourceLossSuspendsAndDisablesTheExactManagedKey drives the
// provider-reconcile suspension: only Fabric's confirmed absence of the original
// resource set suspends the Workspace, and the same owner route disables its key.
func TestProviderResourceLossSuspendsAndDisablesTheExactManagedKey(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, ctx, gateway := lifecycleWorkspace(t, db, false)
	service.Fabric = &deletionFabricClient{present: false}
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if state := lifecycleWorkspaceStatus(t, db, ctx); state != "suspended" {
		t.Fatalf("workspace status=%q", state)
	}
	_, order, result := lifecycleOperation(t, db, ctx, lifecycleSuspend)
	if order.Reason != lifecycleReasonResourceAbsent || order.Trigger != lifecycleTriggerReconcile {
		t.Fatalf("suspension order=%+v", order)
	}
	if result.KeyLifecycleOutcome != lifecycleConfirmed || gateway.disableCalls != 1 || gateway.bindingID != "gateway-key-original" {
		t.Fatalf("key result=%+v calls=%d binding=%q", result, gateway.disableCalls, gateway.bindingID)
	}
}

// TestSuspensionAndRecoveryReuseTheOneBindingAndNeverIssueANewKey shows the
// authorized recovery: the customer's confirmed paid term returns, the original
// binding is re-enabled, and the Workspace reopens only after its own key is
// usable again. No second key is issued and no second disable is sent.
func TestSuspensionAndRecoveryReuseTheOneBindingAndNeverIssueANewKey(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, ctx, gateway := lifecycleWorkspace(t, db, true)
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if gateway.disableCalls != 1 {
		t.Fatalf("disable calls=%d", gateway.disableCalls)
	}
	// A confirmed paid term again is the owner's own recovery fact.
	lifecycleSetCurrentPeriod(t, db, ctx, "period-restored", time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(29*24*time.Hour))
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if state := lifecycleWorkspaceStatus(t, db, ctx); state != "active" {
		t.Fatalf("workspace status=%q", state)
	}
	lifecycleAccess(t, service, api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_PERMITTED)
	op, order, result := lifecycleOperation(t, db, ctx, lifecycleResume)
	if op.Status != "succeeded" || op.Observation != "confirmed" {
		t.Fatalf("recovery operation=%+v", op)
	}
	if order.LaunchOperationID != "operation-original" || order.GrantID != "grant-original" {
		t.Fatalf("recovery order=%+v", order)
	}
	if result.KeyLifecycleOutcome != lifecycleConfirmed || result.KeyLifecycleAction != "enable" || result.ManagedKeyBindingID != "gateway-key-original" {
		t.Fatalf("recovery key result=%+v", result)
	}
	if gateway.enableCalls != 1 || gateway.bindingID != "gateway-key-original" || gateway.grant != "grant-original" {
		t.Fatalf("gateway enable binding=%q grant=%q calls=%d", gateway.bindingID, gateway.grant, gateway.enableCalls)
	}
	if gateway.state != api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_ACTIVE {
		t.Fatalf("external state=%v", gateway.state)
	}
	if gateway.readKeyCalls != 1 {
		t.Fatalf("recovery did not confirm the binding is readable again: %d", gateway.readKeyCalls)
	}
}

// TestUnknownKeyAnswerIsRecoveredByReadingTheAuthoritativeState proves the lost
// answer path: the disable request is recorded as unknown, the Workspace stays
// suspended, and the retry confirms the Gateway's state by reading it instead of
// sending a second effect.
func TestUnknownKeyAnswerIsRecoveredByReadingTheAuthoritativeState(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, ctx, gateway := lifecycleWorkspace(t, db, true)
	gateway.disableErr = status.Error(codes.Unavailable, "answer lost")
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	op, _, result := lifecycleOperation(t, db, ctx, lifecycleSuspend)
	if op.Status != lifecycleAwaiting || result.KeyLifecycleOutcome != lifecycleUnknown || result.KeyLifecycleCode != "KEY_CLOSEOUT_UNKNOWN" {
		t.Fatalf("unknown suspension operation=%+v result=%+v", op, result)
	}
	if state := lifecycleWorkspaceStatus(t, db, ctx); state != "suspended" {
		t.Fatalf("workspace status=%q", state)
	}
	// The Gateway did apply the change; only its answer was lost.
	gateway.state, gateway.disableErr = api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED, nil
	if _, err := db.ExecContext(ctx, `UPDATE workspace.saga_steps SET next_attempt_at=now() WHERE operation_id=$1`, op.ID); err != nil {
		t.Fatal(err)
	}
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	op, _, result = lifecycleOperation(t, db, ctx, lifecycleSuspend)
	if op.Status != "succeeded" || result.KeyLifecycleOutcome != lifecycleConfirmed || result.KeyLifecycleAction != "readback" {
		t.Fatalf("recovered operation=%+v result=%+v", op, result)
	}
	if gateway.disableCalls != 1 {
		t.Fatalf("a blind second disable was sent: %d", gateway.disableCalls)
	}
}

// TestConcurrentLifecycleTriggersSendOneEffectForOneBinding fires the expiry
// sweep and the provider reconcile on the same Workspace at the same time. They
// share one event identity and one idempotency key, so the exact binding receives
// one effect instead of two.
func TestConcurrentLifecycleTriggersSendOneEffectForOneBinding(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, ctx, gateway := lifecycleWorkspace(t, db, true)
	service.Fabric = &deletionFabricClient{present: false}
	start := make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		<-start
		_, err := service.suspendWorkspaceLifecycle(ctx, "workspace-original", lifecycleReasonUnpaidExpiry, lifecycleTriggerExpiry)
		errs <- err
	}()
	go func() {
		<-start
		_, err := service.suspendWorkspaceLifecycle(ctx, "workspace-original", lifecycleReasonResourceAbsent, lifecycleTriggerReconcile)
		errs <- err
	}()
	close(start)
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if events := lifecycleCount(t, db, ctx, lifecycleSuspend); events != 1 {
		t.Fatalf("suspension events=%d", events)
	}
	if gateway.disableCalls != 1 {
		t.Fatalf("disable calls=%d", gateway.disableCalls)
	}
	var steps int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM workspace.saga_steps WHERE step_key=$1`, lifecycleStep).Scan(&steps); err != nil {
		t.Fatal(err)
	}
	if steps != 1 {
		t.Fatalf("managed-key steps=%d", steps)
	}
	if state := lifecycleWorkspaceStatus(t, db, ctx); state != "suspended" {
		t.Fatalf("workspace status=%q", state)
	}
}

// TestRevokedManagedKeyStaysTerminalAcrossSuspensionAndRecovery shows the
// terminal rule #955 established: a revoked binding is neither disabled nor
// re-enabled, no replacement key is issued, and the Workspace stays suspended
// instead of reopening without a credential.
func TestRevokedManagedKeyStaysTerminalAcrossSuspensionAndRecovery(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, ctx, gateway := lifecycleWorkspace(t, db, true)
	gateway.state = api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_REVOKED
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	op, _, result := lifecycleOperation(t, db, ctx, lifecycleSuspend)
	if op.Status != "succeeded" || result.KeyLifecycleOutcome != lifecycleRejected || result.KeyLifecycleCode != "KEY_REVOKED" {
		t.Fatalf("revoked suspension operation=%+v result=%+v", op, result)
	}
	if gateway.disableCalls != 0 {
		t.Fatalf("a revoked key was disabled: %d", gateway.disableCalls)
	}
	lifecycleSetCurrentPeriod(t, db, ctx, "period-restored", time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(29*24*time.Hour))
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	recovery, _, recoveryResult := lifecycleOperation(t, db, ctx, lifecycleResume)
	if recovery.Status != "needs_attention" || recoveryResult.KeyLifecycleOutcome != lifecycleRejected || recoveryResult.KeyLifecycleCode != "KEY_REVOKED" {
		t.Fatalf("revoked recovery operation=%+v result=%+v", recovery, recoveryResult)
	}
	if gateway.enableCalls != 0 || gateway.bindingID != "gateway-key-original" {
		t.Fatalf("a revoked key was re-enabled or replaced: calls=%d binding=%q", gateway.enableCalls, gateway.bindingID)
	}
	if state := lifecycleWorkspaceStatus(t, db, ctx); state != "suspended" {
		t.Fatalf("workspace status=%q", state)
	}
	// A terminal refusal is not retried into a storm of new events.
	if events := lifecycleCount(t, db, ctx, lifecycleResume); events != 1 {
		t.Fatalf("recovery events=%d", events)
	}
}

// TestRefusedKeyCloseoutDoesNotBlockDeletionOrRefund keeps the boundary the
// target invariant requires: while the Gateway has no approved lifecycle
// endpoint, a suspension records the refusal and the Workspace's deletion and
// refund still complete on their own evidence.
func TestRefusedKeyCloseoutDoesNotBlockDeletionOrRefund(t *testing.T) {
	db := runtimeDatabase(t)
	// The suspension is driven by a provider-confirmed resource loss, so the
	// Workspace's original paid term and its original charge evidence stay the
	// facts the deletion settles.
	service, call, ctx, gateway := lifecycleWorkspace(t, db, false)
	service.Fabric = &deletionFabricClient{}
	// The production adapter answers Unimplemented until Sub2API exposes the
	// approved endpoint.
	gateway.disableErr = status.Error(codes.Unimplemented, "the external Gateway key endpoint is not implemented")
	if err := service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	op, _, result := lifecycleOperation(t, db, ctx, lifecycleSuspend)
	if op.Status != "succeeded" || result.KeyLifecycleOutcome != lifecycleRejected || result.KeyLifecycleCode != "KEY_CLOSEOUT_REFUSED" {
		t.Fatalf("refused suspension operation=%+v result=%+v", op, result)
	}
	if state := lifecycleWorkspaceStatus(t, db, ctx); state != "suspended" {
		t.Fatalf("workspace status=%q", state)
	}
	// Resource deletion and refund are independent facts. Their completion does
	// not discharge the refused managed-key obligation.
	service.Fabric = &deletionFabricClient{}
	call.IdempotencyKey = "idempotency-deletion-suspended"
	if _, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original",
		Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}}); err != nil {
		t.Fatal(err)
	}
	deletion, deletionResult := readDeletionOperation(t, db, ctx)
	if deletion.Status != "needs_attention" || deletion.Observation != "rejected" {
		t.Fatalf("deletion must retain its refused key obligation: %+v", deletion)
	}
	if deletionResult.KeyCloseoutOutcome != lifecycleRejected || gateway.refunds != 1 {
		t.Fatalf("deletion key closeout=%q refunds=%d", deletionResult.KeyCloseoutOutcome, gateway.refunds)
	}
	if state := lifecycleWorkspaceStatus(t, db, ctx); state != "deleted" {
		t.Fatalf("workspace status=%q", state)
	}
}
