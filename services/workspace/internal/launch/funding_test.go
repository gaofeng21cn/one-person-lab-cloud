package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/workspace/migrations"
)

// walletAuthority is an isolated financial authority: it owns the one charge for
// an obligation and answers strictly by the original idempotency code. It records
// how many times it was asked to charge, so a test can prove a lost response or an
// unknown outcome never becomes a second charge, and it counts refunds so a test
// can prove an unknown charge is never reversed.
type walletAuthority struct {
	api.GatewayCoordinationClient
	charges     map[string]*api.WalletOperation
	debitCalls  int
	readCalls   int
	refundCalls int
	loseAck     bool
	reject      *api.ErrorCodeEnum
	unresolved  bool
	converge    bool
}

func (w *walletAuthority) Debit(_ context.Context, r *api.WalletDebitCommand, _ ...grpc.CallOption) (*api.WalletOperation, error) {
	w.debitCalls++
	key := r.GetContext().GetIdempotencyKey()
	if key == "" || r.GetObligationId() == "" || r.GetWorkspaceId() == "" || r.GetQuoteAcceptanceId() == "" || r.GetAmountUsdMicros() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "charge is not an original obligation")
	}
	if w.charges == nil {
		w.charges = map[string]*api.WalletOperation{}
	}
	if existing := w.charges[key]; existing != nil {
		// The original code always answers with the same charge.
		return proto.Clone(existing).(*api.WalletOperation), nil
	}
	operation := &api.WalletOperation{Id: "wallet-charge-original", WorkspaceId: proto.String(r.GetWorkspaceId()), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE, AmountUsdMicros: r.GetAmountUsdMicros(), Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED, ReceiptId: proto.String("charge-receipt-original"), CreatedAt: timestamppb.Now()}
	switch {
	case w.reject != nil:
		operation.Status = api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REJECTED
		operation.ErrorCode = w.reject
		operation.ReceiptId = nil
	case w.unresolved && !w.converge:
		operation.Status = api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN
		operation.ReceiptId = nil
	}
	w.charges[key] = operation
	if w.loseAck {
		w.loseAck = false
		return nil, status.Error(codes.Unavailable, "charge committed before its response was lost")
	}
	return proto.Clone(operation).(*api.WalletOperation), nil
}

func (w *walletAuthority) ReadWalletAction(_ context.Context, r *api.WalletReadbackRequest, _ ...grpc.CallOption) (*api.WalletOperation, error) {
	w.readCalls++
	existing := w.charges[r.GetOriginalIdempotencyKey()]
	if existing == nil {
		return nil, status.Error(codes.NotFound, "no charge for the original code")
	}
	if w.converge && existing.GetStatus() == api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN {
		existing.Status = api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED
		existing.ReceiptId = proto.String("charge-receipt-original")
		existing.CreatedAt = timestamppb.Now()
		w.converge = false
	}
	return proto.Clone(existing).(*api.WalletOperation), nil
}

func (w *walletAuthority) Refund(_ context.Context, r *api.WalletRefundCommand, _ ...grpc.CallOption) (*api.WalletOperation, error) {
	w.refundCalls++
	return nil, status.Error(codes.Internal, "a refund must never be issued by launch")
}

// fundingFabricClient records the charge evidence Fabric was released with. It
// answers with the same original resource identity every time, so a repeated
// pass cannot be read as a new resource.
type fundingFabricClient struct {
	api.FabricCoordinationClient
	readback    *api.ResourceReadback
	receiptIDs  []string
	ensureCalls int
}

func (f *fundingFabricClient) EnsureResources(_ context.Context, r *api.EnsureResourcesCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	f.ensureCalls++
	if r.WorkspaceId != "workspace-original" || r.ObligationId != "operation-original" || r.Context.GetIdempotencyKey() != "operation-original:ensure_resources" {
		return nil, status.Error(codes.InvalidArgument, "different original resource intent")
	}
	f.receiptIDs = append(f.receiptIDs, r.GetConfirmedChargeReceiptId())
	return &api.Operation{OperationId: "fabric-operation-original", ResourceId: "resource-set-original", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_RESOURCE_PROVISION}, nil
}

func (f *fundingFabricClient) ReadResources(_ context.Context, r *api.ResourceReadbackRequest, _ ...grpc.CallOption) (*api.ResourceReadback, error) {
	if r.GetResourceSetId() != f.readback.GetResourceSetId() {
		return nil, status.Error(codes.NotFound, "different resources")
	}
	return proto.Clone(f.readback).(*api.ResourceReadback), nil
}

// paidOrderDatabase provisions the same isolated Workspace database the runtime
// tests use, so the paid funding path is exercised against the owner's real schema.
func paidOrderDatabase(t *testing.T) *sql.DB {
	t.Helper()
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "workspace", Database: "opl_workspace", SchemaOwnerRole: "opl_workspace_owner", WriterRole: "opl_workspace_writer", RuntimeRole: "opl_workspace_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatal(err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// seedPaidOrder commits one accepted paid order (a Tencent prepaid month, not a
// Local no-charge one) with its resource intent already frozen, so the only
// unsettled obligation is the wallet charge.
func seedPaidOrder(t *testing.T, db *sql.DB) (*Service, ownerstore.Operation, *api.QuoteAcceptance) {
	t.Helper()
	ctx := t.Context()
	_, offer, accepted := orderFixture()
	offer.ResourcePlan.Provider, accepted.ResourcePlan.Provider = "tencent", "tencent"
	offer.ResourcePlan.BillingMode, accepted.ResourcePlan.BillingMode = "PREPAID_MONTHLY", "PREPAID_MONTHLY"
	store, err := ownerstore.New(db, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(acceptedOrder{Quote: wire(offer), AuthorizationContextID: "authorization-original", InputDigest: "sha256:original"})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	op, err := store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: "operation-original", TenantID: "tenant-original", ActorID: "actor-original", Kind: "create_workspace", ResourceID: "workspace-original", Stage: "resource_preflight", RequestID: "request-original", AcceptedInput: input})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.workspaces(id,tenant_id,name,status,compute_plan_id,storage_plan_id,active_operation_id,created_by,version) VALUES($1,$2,'Paid workspace','provisioning','compute-original','storage-original',$3,$4,1)`, op.ResourceID, op.TenantID, op.ID, op.ActorID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	resources := &api.ResourceReadback{ResourceSetId: "resource-set-original", WorkspaceId: op.ResourceID, Outcome: api.Observation_OBSERVATION_UNKNOWN}
	stored := orderResult{GrantID: "grant-original", Acceptance: wire(accepted), FabricOperationID: "fabric-operation-original", ResourceSetID: resources.ResourceSetId, ResourceReadback: wire(resources)}
	raw, _ := json.Marshal(stored)
	if _, err = db.ExecContext(ctx, `UPDATE workspace.operations SET status='awaiting_confirmation',observation_result='unknown',result=$2 WHERE id=$1`, op.ID, raw); err != nil {
		t.Fatal(err)
	}
	fabric := &fundingFabricClient{readback: resources}
	return &Service{Store: store, Fabric: fabric}, op, accepted
}

func fundingService(t *testing.T, db *sql.DB) (*Service, ownerstore.Operation, *api.QuoteAcceptance, *fundingFabricClient, *walletAuthority) {
	t.Helper()
	service, op, accepted := seedPaidOrder(t, db)
	authority := &walletAuthority{}
	service.Gateway = authority
	fabric, ok := service.Fabric.(*fundingFabricClient)
	if !ok {
		t.Fatal("unexpected resource client")
	}
	return service, op, accepted, fabric, authority
}

// TestPaidOrderChargesTheOriginalObligationOnce proves a paid order settles its
// charge through the wallet authority exactly once and releases resources only
// with that charge's own receipt. A lost response must read the original code back
// rather than charge again, and no path may refund an unsettled charge.
func TestPaidOrderChargesTheOriginalObligationOnce(t *testing.T) {
	for _, lostAck := range []bool{true, false} {
		name := "confirmed"
		if lostAck {
			name = "response lost after commit"
		}
		t.Run(name, func(t *testing.T) {
			service, op, _, fabric, authority := fundingService(t, paidOrderDatabase(t))
			authority.loseAck = lostAck
			if err := service.Resume(t.Context(), op.ID); err != nil {
				t.Fatal(err)
			}
			if authority.debitCalls != 1 || len(authority.charges) != 1 {
				t.Fatalf("the original obligation was charged %d times", authority.debitCalls)
			}
			if lostAck != (authority.readCalls == 1) {
				t.Fatalf("lost response read back %d times", authority.readCalls)
			}
			if authority.refundCalls != 0 {
				t.Fatal("launch refunded a settled charge")
			}
			if len(fabric.receiptIDs) != 1 || fabric.receiptIDs[0] != "charge-receipt-original" {
				t.Fatalf("resources were released with %v", fabric.receiptIDs)
			}
			stored, err := service.Store.ReadOperation(t.Context(), op.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, result, err := decodeOrder(stored)
			if err != nil {
				t.Fatal(err)
			}
			charge := &api.WalletOperation{}
			if protojson.Unmarshal(result.WalletOperation, charge) != nil || charge.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED || charge.GetReceiptId() != "charge-receipt-original" {
				t.Fatalf("the confirmed charge was not persisted: %s", result.WalletOperation)
			}
			command := &api.WalletDebitCommand{}
			if protojson.Unmarshal(result.WalletDebitCommand, command) != nil || command.GetObligationId() != op.ID || command.GetWorkspaceId() != op.ResourceID || command.GetAmountUsdMicros() != 1234567 || command.GetContext().GetIdempotencyKey() != op.ID+":wallet_charge" || command.GetContext().GetAcceptedOperationGrantId() != "grant-original" {
				t.Fatalf("the charge lost its original obligation identity: %s", result.WalletDebitCommand)
			}
		})
	}
}

// TestUnknownChargeIsReadBackAndNeverRefunded proves an unknown outcome keeps the
// obligation, is only ever read back by its original code, and converges without a
// second charge or an advance refund.
func TestUnknownChargeIsReadBackAndNeverRefunded(t *testing.T) {
	service, op, _, fabric, authority := fundingService(t, paidOrderDatabase(t))
	authority.unresolved = true
	if err := service.Resume(t.Context(), op.ID); err != nil {
		t.Fatal(err)
	}
	// The wallet answered UNKNOWN, so the order must be pending rather than funded.
	if len(fabric.receiptIDs) != 0 {
		t.Fatal("an unknown charge released resources")
	}
	stored, err := service.Store.ReadOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "awaiting_confirmation" || stored.Observation != "unknown" || stored.Terminal() {
		t.Fatalf("an unknown charge was recorded as %s/%s", stored.Status, stored.Observation)
	}
	// The original code is the only thing re-read: a later pass must not charge.
	authority.converge = true
	restarted := &Service{Store: service.Store, Fabric: service.Fabric, Gateway: authority}
	if err = restarted.Resume(t.Context(), op.ID); err != nil {
		t.Fatal(err)
	}
	if authority.debitCalls != 1 || authority.readCalls < 2 {
		t.Fatalf("the unknown charge was re-charged: debit=%d read=%d", authority.debitCalls, authority.readCalls)
	}
	if authority.refundCalls != 0 {
		t.Fatal("an unknown charge was refunded")
	}
	if len(fabric.receiptIDs) != 1 || fabric.receiptIDs[0] != "charge-receipt-original" {
		t.Fatalf("the converged charge did not release resources: %v", fabric.receiptIDs)
	}
}

// TestRefusedChargeStopsTheDownstream proves the wallet's explicit refusal ends the
// original obligation: no resource is provisioned, the refusal's own cause is
// recorded, and no refund is issued for a charge that never happened.
func TestRefusedChargeStopsTheDownstream(t *testing.T) {
	service, op, _, fabric, authority := fundingService(t, paidOrderDatabase(t))
	refusal := api.ErrorCodeEnum_ERROR_CODE_ENUM_INSUFFICIENT_BALANCE
	authority.reject = &refusal
	if err := service.Resume(t.Context(), op.ID); err != nil {
		t.Fatal(err)
	}
	stored, err := service.Store.ReadOperation(t.Context(), op.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Status != "needs_attention" || stored.Observation != "rejected" || stored.ErrorCode != "INSUFFICIENT_BALANCE" {
		t.Fatalf("the refusal reads as %s/%s/%s", stored.Status, stored.Observation, stored.ErrorCode)
	}
	if fabric.ensureCalls != 0 || len(fabric.receiptIDs) != 0 {
		t.Fatal("a refused charge released resources")
	}
	// The refusal is the original answer for this code, so a later pass neither
	// charges again nor invents a refund.
	if err = service.Resume(t.Context(), op.ID); err != nil {
		t.Fatal(err)
	}
	if authority.debitCalls != 1 || authority.refundCalls != 0 {
		t.Fatalf("a refused charge was retried: debit=%d refund=%d", authority.debitCalls, authority.refundCalls)
	}
}

// TestUnfundedAndForeignChargesCannotReleaseResources proves the charge evidence is
// bound to the original order: a charge for another workspace or another amount is
// refused, and an order whose funding owner is not configured stays pending rather
// than reading as free.
func TestUnfundedAndForeignChargesCannotReleaseResources(t *testing.T) {
	op, _, accepted := orderFixture()
	foreign := &api.WalletOperation{Id: "charge-other", WorkspaceId: proto.String("workspace-other"), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE, AmountUsdMicros: accepted.GetQuote().GetTotalUsdMicros(), Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED, ReceiptId: proto.String("receipt-other"), CreatedAt: timestamppb.Now()}
	bound := proto.Clone(foreign).(*api.WalletOperation)
	bound.WorkspaceId = proto.String("workspace-original")
	for name, change := range map[string]func(*api.WalletOperation){
		"other workspace": func(w *api.WalletOperation) { w.WorkspaceId = proto.String("workspace-other") },
		"amount":          func(w *api.WalletOperation) { w.AmountUsdMicros++ },
		"kind": func(w *api.WalletOperation) {
			w.Kind = api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND
		},
		"zero":     func(w *api.WalletOperation) { w.AmountUsdMicros = 0 },
		"identity": func(w *api.WalletOperation) { w.Id = "" },
	} {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(bound).(*api.WalletOperation)
			change(changed)
			if status.Code(validateWalletCharge(op, accepted, changed)) != codes.DataLoss {
				t.Fatal("a charge to another obligation was accepted")
			}
		})
	}
	// The same charge, bound to the original workspace and amount, is the evidence
	// the resource owner verifies.
	if err := validateWalletCharge(op, accepted, bound); err != nil {
		t.Fatal(err)
	}
	// A Local no-charge order is funded by its own Ledger receipt, so it must never
	// be presented with a wallet charge.
	local, _, localAccepted := orderFixture()
	localAccepted.Quote.TotalUsdMicros = 0
	localAccepted.ResourcePlan.Provider = "local-docker"
	localAccepted.ResourcePlan.BillingMode = "LOCAL_NO_CHARGE"
	if status.Code(validateWalletCharge(local, localAccepted, bound)) != codes.DataLoss {
		t.Fatal("a Local no-charge order accepted a wallet charge")
	}
	// Without a configured funding owner the paid order stays pending: no charge is
	// invented, so no resource is released and no receipt is reported.
	service, op2, _, fabric, _ := fundingService(t, paidOrderDatabase(t))
	service.Gateway = nil
	receipt, refused, err := service.fundingEvidence(t.Context(), op2, "lease-original", accepted, &orderResult{GrantID: "grant-original"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt != "" || refused {
		t.Fatalf("an unfunded order reported funding %q/%v", receipt, refused)
	}
	if fabric.ensureCalls != 0 {
		t.Fatal("an unfunded order released resources")
	}
}
