package launch

import (
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// paidRuntimeOrder builds one accepted Tencent prepaid month order with the exact
// confirmed Gateway charge and Ledger WALLET_ACTION receipt that funded it, so the
// runtime gate can be exercised on the paid branch instead of the Local one.
func paidRuntimeOrder(t *testing.T) (ownerstore.Operation, *api.QuoteAcceptance, *orderResult) {
	t.Helper()
	op, _, accepted := orderFixture()
	accepted.ResourcePlan.Provider, accepted.ResourcePlan.BillingMode = "tencent", "PREPAID_MONTHLY"
	accepted.ResourcePlan.PrepaidMonths = 1
	accepted.Quote.PeriodMonths = 1
	accepted.Quote.PricePolicyVersionId = "price-policy-original"
	accepted.Quote.PeriodStart, accepted.Quote.PeriodEnd = timestamppb.New(time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)), timestamppb.New(time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC))
	charge := &api.WalletOperation{
		Id: "wallet-charge-original", WorkspaceId: proto.String(op.ResourceID),
		Kind:            api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE,
		AmountUsdMicros: accepted.GetQuote().GetTotalUsdMicros(),
		Status:          api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED,
		ReceiptId:       proto.String("wallet-action-receipt-original"), CreatedAt: timestamppb.Now(),
	}
	receipt := &api.Receipt{
		Id: "wallet-action-receipt-original", Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION,
		Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(op.ID),
		Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, CreatedAt: timestamppb.Now(),
	}
	evidence := &api.WalletActionReceiptEvidence{Receipt: receipt, QuoteAcceptance: accepted, WalletOperation: charge, EvidenceDigest: accepted.SnapshotDigest}
	result := &orderResult{GrantID: "grant-original", Acceptance: wire(accepted), WalletOperation: wire(charge), WalletActionReceipt: wire(evidence)}
	return op, accepted, result
}

func TestPaidRuntimeUsesConfirmedChargeEvidence(t *testing.T) {
	op, accepted, result := paidRuntimeOrder(t)
	if err := validatePaidFundingEvidence(op, accepted, mustPaid(t, result)); err != nil {
		t.Fatalf("confirmed charge evidence for the original paid order was refused: %v", err)
	}
	// The paid branch accepts its own evidence and must never be satisfied by a
	// Local zero-charge receipt.
	if localNoCharge(accepted) {
		t.Fatal("a paid order was classified as Local no-charge")
	}
}

func TestPaidRuntimeRejectsWrongOrderAmountAndMissingEvidence(t *testing.T) {
	op, accepted, result := paidRuntimeOrder(t)
	for name, mutate := range map[string]func(*api.WalletActionReceiptEvidence){
		"other obligation": func(e *api.WalletActionReceiptEvidence) { e.Receipt.OperationId = proto.String("operation-other") },
		"other workspace": func(e *api.WalletActionReceiptEvidence) {
			e.WalletOperation.WorkspaceId = proto.String("workspace-other")
		},
		"amount":      func(e *api.WalletActionReceiptEvidence) { e.WalletOperation.AmountUsdMicros++ },
		"zero amount": func(e *api.WalletActionReceiptEvidence) { e.WalletOperation.AmountUsdMicros = 0 },
		"unconfirmed": func(e *api.WalletActionReceiptEvidence) {
			e.WalletOperation.Status = api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN
		},
		"wrong receipt": func(e *api.WalletActionReceiptEvidence) { e.Receipt.Id = "receipt-other" },
		"local receipt": func(e *api.WalletActionReceiptEvidence) {
			e.Receipt.Kind = api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE
		},
		"digest": func(e *api.WalletActionReceiptEvidence) { e.EvidenceDigest = "sha256:other" },
		"acceptance": func(e *api.WalletActionReceiptEvidence) {
			changed := proto.Clone(e.QuoteAcceptance).(*api.QuoteAcceptance)
			changed.SnapshotDigest = "sha256:other"
			e.QuoteAcceptance = changed
		},
	} {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(mustPaid(t, result)).(*api.WalletActionReceiptEvidence)
			mutate(changed)
			if status.Code(validatePaidFundingEvidence(op, accepted, changed)) != codes.DataLoss {
				t.Fatal("funding evidence that is not the original confirmed charge was accepted")
			}
		})
	}
	t.Run("missing evidence", func(t *testing.T) {
		if status.Code(validatePaidFundingEvidence(op, accepted, nil)) != codes.FailedPrecondition {
			t.Fatal("a paid order without charge evidence was accepted")
		}
	})
	t.Run("local order cannot use paid evidence", func(t *testing.T) {
		localOp, _, localAccepted := orderFixture()
		localAccepted.Quote.TotalUsdMicros = 0
		localAccepted.ResourcePlan.Provider, localAccepted.ResourcePlan.BillingMode = "local-docker", "LOCAL_NO_CHARGE"
		if err := validatePaidFundingEvidence(localOp, localAccepted, mustPaid(t, result)); err == nil {
			t.Fatal("a Local no-charge order was released by paid charge evidence")
		}
	})
}

func mustPaid(t *testing.T, result *orderResult) *api.WalletActionReceiptEvidence {
	t.Helper()
	evidence, err := result.paidFundingEvidence()
	if err != nil || evidence == nil {
		t.Fatalf("paid funding evidence is missing or unreadable: %v", err)
	}
	return evidence
}

// TestPaidOrderReachesRuntimeOnItsOwnFundingProof is the end-to-end reproduction of
// the paid runtime gate: once Fabric confirms the order's own resources, a paid
// order whose confirmed Gateway charge has a Ledger WALLET_ACTION receipt must be
// delivered to Serve — without any Local zero-charge receipt.
func TestPaidOrderReachesRuntimeOnItsOwnFundingProof(t *testing.T) {
	db := paidOrderDatabase(t)
	service, op, accepted := seedPaidOrder(t, db)
	resources := &api.ResourceReadback{
		ResourceSetId: "resource-set-original", WorkspaceId: op.ResourceID,
		Outcome: api.Observation_OBSERVATION_CONFIRMED,
		ExecutionResources: &api.ResourceExecutionBinding{
			AccountId: "account-original", ComputeAllocationId: "compute-original",
			StorageVolumeId: "storage-original", DataAttachmentId: "attachment-original",
			DataAttachmentOperationId: "attachment-operation-original",
		},
	}
	charge := &api.WalletOperation{
		Id: "wallet-charge-original", WorkspaceId: proto.String(op.ResourceID),
		Kind:            api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE,
		AmountUsdMicros: accepted.GetQuote().GetTotalUsdMicros(),
		Status:          api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED,
		ReceiptId:       proto.String("wallet-action-receipt-original"), CreatedAt: timestamppb.Now(),
	}
	receipt := &api.Receipt{
		Id: "wallet-action-receipt-original", Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION,
		Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(op.ID),
		Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, CreatedAt: timestamppb.Now(),
	}
	evidence := &api.WalletActionReceiptEvidence{Receipt: receipt, QuoteAcceptance: accepted, WalletOperation: charge, EvidenceDigest: accepted.SnapshotDigest}
	stored := orderResult{GrantID: "grant-original", Acceptance: wire(accepted), WalletOperation: wire(charge), WalletActionReceipt: wire(evidence), FabricOperationID: "fabric-operation-original", ResourceSetID: resources.ResourceSetId, ResourceReadback: wire(resources)}
	raw, _ := json.Marshal(stored)
	if _, err := db.ExecContext(t.Context(), `UPDATE workspace.operations SET status='running',observation_result='confirmed',stage='runtime',result=$2 WHERE id=$1`, op.ID, raw); err != nil {
		t.Fatal(err)
	}
	serve := &runtimeServeClient{}
	service.Fabric = &runtimeResourceClient{readback: resources}
	service.Serve = serve
	service.Capability = &runtimeCapabilityClient{version: runtimeVersion(t, accepted.Quote.GetCapabilityVersionId())}
	if err := service.Resume(t.Context(), op.ID); err != nil {
		t.Fatalf("paid order did not reach its runtime on confirmed charge evidence: %v", err)
	}
	if serve.deployCalls != 1 {
		t.Fatalf("paid order reached Serve %d times, want exactly one original deployment", serve.deployCalls)
	}
	current, err := service.Store.ReadOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Serve readiness plus the order's own confirmed charge receipt completes the
	// entitlement: the operation is terminal and confirmed, not left waiting.
	if current.Status != "succeeded" || current.Stage != "succeeded" || current.Observation != "confirmed" || current.CompletedAt.IsZero() {
		t.Fatalf("paid order did not complete its entitlement: %s/%s/%s", current.Status, current.Stage, current.Observation)
	}
	var workspaceState string
	if err = db.QueryRowContext(t.Context(), `SELECT status FROM workspace.workspaces WHERE id=$1`, op.ResourceID).Scan(&workspaceState); err != nil {
		t.Fatal(err)
	}
	if workspaceState != "active" {
		t.Fatalf("paid order left its Workspace %s", workspaceState)
	}
	var subscriptions, periods int
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscriptions`).Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscription_periods`).Scan(&periods); err != nil {
		t.Fatal(err)
	}
	if subscriptions != 1 || periods != 1 {
		t.Fatalf("paid activation wrote %d subscriptions and %d periods, want exactly one each", subscriptions, periods)
	}
	// A resumed pass over the delivered order is terminal and must add nothing.
	if err = service.Resume(t.Context(), op.ID); err != nil {
		t.Fatalf("resuming a delivered order failed: %v", err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscriptions`).Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscription_periods`).Scan(&periods); err != nil {
		t.Fatal(err)
	}
	if serve.deployCalls != 1 || subscriptions != 1 || periods != 1 {
		t.Fatalf("resume repeated a delivered order: deploy=%d subscriptions=%d periods=%d", serve.deployCalls, subscriptions, periods)
	}
	// The customer-visible projection must now carry the accepted period end, read
	// from this owner's own confirmed subscription row.
	w, tenant, err := scanWorkspace(db.QueryRowContext(t.Context(), `SELECT `+workspaceColumns+workspaceJoin+` WHERE w.id=$1`, op.ResourceID))
	if err != nil {
		t.Fatal(err)
	}
	if tenant != op.TenantID || w.GetStatus() != api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE || w.CurrentPeriodEnd == nil ||
		!w.CurrentPeriodEnd.AsTime().UTC().Equal(accepted.GetQuote().GetPeriodEnd().AsTime().UTC()) {
		t.Fatalf("activated Workspace readback lost its confirmed period: %v", w)
	}
}

// TestLocalOrderKeepsItsActivationBoundary proves the Local no-charge branch is
// untouched by paid activation: Serve readiness records the boundary and writes no
// subscription, so a zero-fee period is never fabricated from paid evidence.
func TestLocalOrderKeepsItsActivationBoundary(t *testing.T) {
	db := runtimeDatabase(t)
	service, op, accepted, resources := seedRuntimeOrder(t, db)
	service.Capability = &runtimeCapabilityClient{version: runtimeVersion(t, accepted.Quote.GetCapabilityVersionId())}
	service.Serve = &runtimeServeClient{}
	if err := service.Resume(t.Context(), op.ID); err != nil {
		t.Fatalf("Local order did not reach its activation boundary: %v", err)
	}
	stored, err := service.Store.ReadOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "awaiting_confirmation" || stored.Stage != "activation" || !stored.CompletedAt.IsZero() {
		t.Fatalf("Local order left its activation boundary: %s/%s", stored.Status, stored.Stage)
	}
	var subscriptions, periods int
	var state string
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscriptions`).Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscription_periods`).Scan(&periods); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT status FROM workspace.workspaces WHERE id=$1`, op.ResourceID).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if subscriptions != 0 || periods != 0 || state != "provisioning" {
		t.Fatalf("Local readiness fabricated %d subscriptions, %d periods and Workspace %s", subscriptions, periods, state)
	}
	// The confirmed resource readback is still reflected, and no period is implied.
	if resources.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED {
		t.Fatal("Local resources were not confirmed")
	}
}

// TestPaidOrderRecoversFromLostDeployResponse proves the crash window between
// Serve recording its readiness and Workspace writing the entitlement: the first
// pass loses the deploy response, and the resumed pass reads the original
// readiness and completes the same single entitlement without a second deployment
// or a second charge.
func TestPaidOrderRecoversFromLostDeployResponse(t *testing.T) {
	db := paidOrderDatabase(t)
	service, op, accepted := seedPaidOrder(t, db)
	resources := &api.ResourceReadback{
		ResourceSetId: "resource-set-original", WorkspaceId: op.ResourceID,
		Outcome: api.Observation_OBSERVATION_CONFIRMED,
		ExecutionResources: &api.ResourceExecutionBinding{
			AccountId: "account-original", ComputeAllocationId: "compute-original",
			StorageVolumeId: "storage-original", DataAttachmentId: "attachment-original",
			DataAttachmentOperationId: "attachment-operation-original",
		},
	}
	charge := &api.WalletOperation{
		Id: "wallet-charge-original", WorkspaceId: proto.String(op.ResourceID),
		Kind:            api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE,
		AmountUsdMicros: accepted.GetQuote().GetTotalUsdMicros(),
		Status:          api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED,
		ReceiptId:       proto.String("wallet-action-receipt-original"), CreatedAt: timestamppb.Now(),
	}
	receipt := &api.Receipt{Id: "wallet-action-receipt-original", Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(op.ID), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, CreatedAt: timestamppb.Now()}
	evidence := &api.WalletActionReceiptEvidence{Receipt: receipt, QuoteAcceptance: accepted, WalletOperation: charge, EvidenceDigest: accepted.SnapshotDigest}
	stored := orderResult{GrantID: "grant-original", Acceptance: wire(accepted), WalletOperation: wire(charge), WalletActionReceipt: wire(evidence), FabricOperationID: "fabric-operation-original", ResourceSetID: resources.ResourceSetId, ResourceReadback: wire(resources)}
	raw, _ := json.Marshal(stored)
	if _, err := db.ExecContext(t.Context(), `UPDATE workspace.operations SET status='running',observation_result='confirmed',stage='runtime',result=$2 WHERE id=$1`, op.ID, raw); err != nil {
		t.Fatal(err)
	}
	serve := &runtimeServeClient{deployLost: true}
	service.Fabric = &runtimeResourceClient{readback: resources}
	service.Serve = serve
	service.Capability = &runtimeCapabilityClient{version: runtimeVersion(t, accepted.Quote.GetCapabilityVersionId())}
	if err := service.Resume(t.Context(), op.ID); status.Code(err) != codes.Unavailable {
		t.Fatalf("first pass must record the lost deploy response: %v", err)
	}
	lost, err := service.Store.ReadOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lost.Status != "awaiting_confirmation" || lost.Observation != "unknown" {
		t.Fatalf("lost deploy response was not durably recoverable: %s/%s", lost.Status, lost.Observation)
	}
	if err = service.Resume(t.Context(), op.ID); err != nil {
		t.Fatalf("resumed pass did not complete the original order: %v", err)
	}
	current, err := service.Store.ReadOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Status != "succeeded" || current.Stage != "succeeded" || current.Observation != "confirmed" {
		t.Fatalf("resumed order did not reach its confirmed terminal state: %s/%s/%s", current.Status, current.Stage, current.Observation)
	}
	var subscriptions, periods int
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscriptions`).Scan(&subscriptions); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscription_periods`).Scan(&periods); err != nil {
		t.Fatal(err)
	}
	if subscriptions != 1 || periods != 1 || serve.deployCalls != 1 {
		t.Fatalf("recovery repeated an effect: deploy=%d subscriptions=%d periods=%d", serve.deployCalls, subscriptions, periods)
	}
}
