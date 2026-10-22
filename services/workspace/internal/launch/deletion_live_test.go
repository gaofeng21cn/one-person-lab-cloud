//go:build livebuild

package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"strings"
	"sync"
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
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// These are backward owner-coupling tests, starting from an explicitly seeded
// nonzero paid period, not forward purchase/charge qualification. Workspace and
// Ledger execute their real handlers over authenticated local gRPC and separate
// PostgreSQL databases, including Ledger's callback to Workspace.ReadOwnerCommit.
// CloudIdentity, Serve retirement, Fabric/provider absence and Gateway/wallet
// settlement are test boundaries; no real provider or wallet is contacted.

type deletionLiveTrace struct {
	mu     sync.Mutex
	events []string
}

func (p *deletionLiveTrace) add(event string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, event)
}

func (p *deletionLiveTrace) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.events...)
}

// This observer always delegates to the real Ledger gRPC client. It never
// manufactures a receipt or substitutes an owner result.
type deletionLiveLedger struct {
	api.LedgerCoordinationClient
	db    *sql.DB
	trace *deletionLiveTrace
}

func (c *deletionLiveLedger) AppendReceipt(ctx context.Context, r *api.AppendReceiptRequest, options ...grpc.CallOption) (*api.Receipt, error) {
	c.trace.add("ledger.append")
	receipt, err := c.LedgerCoordinationClient.AppendReceipt(ctx, r, options...)
	if err != nil {
		c.trace.add("ledger.append." + status.Code(err).String())
		return nil, err
	}
	stored, err := deletionLiveStoredReceipt(ctx, c.db, receipt.GetId())
	if err != nil || !proto.Equal(stored.GetReceipt(), receipt) || !proto.Equal(stored.GetOwnerCommitEvidence(), r.GetOwnerCommitEvidence()) || stored.GetEvidenceDigest() != r.GetEvidenceDigest() {
		return nil, status.Errorf(codes.DataLoss, "committed Ledger deletion evidence differs: %v", err)
	}
	c.trace.add("ledger.committed")
	return receipt, nil
}

func (c *deletionLiveLedger) ReadReceiptByReference(ctx context.Context, r *api.GetReceiptByReferenceRequest, options ...grpc.CallOption) (*api.Receipt, error) {
	c.trace.add("ledger.readback")
	receipt, err := c.LedgerCoordinationClient.ReadReceiptByReference(ctx, r, options...)
	if err != nil {
		c.trace.add("ledger.readback." + status.Code(err).String())
		return nil, err
	}
	c.trace.add("ledger.readback.confirmed")
	return receipt, nil
}

// Read persisted bytes through SQL, independently of the append response and
// Workspace's saved result. Decode the owner's existing typed evidence message.
func deletionLiveStoredReceipt(ctx context.Context, db *sql.DB, receiptID string) (*api.ProviderActionReceiptEvidence, error) {
	var payload []byte
	var workspaceID, tenantID string
	err := db.QueryRowContext(ctx, `SELECT workspace_id,organization_id,payload_json FROM evidence_receipts WHERE id=$1 AND receipt_type='workspace.provider_action.v1'`, receiptID).Scan(&workspaceID, &tenantID, &payload)
	if err != nil {
		return nil, err
	}
	if workspaceID != "workspace-original" || tenantID != "tenant-original" {
		return nil, fmt.Errorf("persisted receipt scope differs: %s/%s", tenantID, workspaceID)
	}
	var envelope struct {
		InputRefs struct {
			ProviderAction json.RawMessage `json:"providerAction"`
		} `json:"inputRefs"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	evidence := &api.ProviderActionReceiptEvidence{}
	if err := protojson.Unmarshal(envelope.InputRefs.ProviderAction, evidence); err != nil {
		return nil, err
	}
	return evidence, nil
}

// Only the CloudIdentity dependency is scripted: an optional receipt-read
// refusal occurs inside Ledger's real read handler after its append committed.
type deletionLiveIdentityBoundary struct {
	deletionIdentity
	denyReceiptRead bool
}

func (i *deletionLiveIdentityBoundary) AuthorizeAction(ctx context.Context, r *api.AuthorizationRequest, options ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	if i.denyReceiptRead && r.GetAction() == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETRECEIPT {
		return nil, status.Error(codes.PermissionDenied, "test boundary: receipt read denied")
	}
	return i.deletionIdentity.AuthorizeAction(ctx, r, options...)
}

type deletionLiveFabricBoundary struct {
	deletionFabricClient
	trace   *deletionLiveTrace
	unknown bool
}

func (f *deletionLiveFabricBoundary) ReadResources(ctx context.Context, r *api.ResourceReadbackRequest, options ...grpc.CallOption) (*api.ResourceReadback, error) {
	readback, err := f.deletionFabricClient.ReadResources(ctx, r, options...)
	if err == nil && f.unknown {
		readback.Outcome = api.Observation_OBSERVATION_UNKNOWN
		readback.AbsenceConfirmed = false
	}
	if err == nil && strings.HasSuffix(r.GetContext().GetIdempotencyKey(), ":read_resources") && readback.GetAbsenceConfirmed() && readback.GetOutcome() == api.Observation_OBSERVATION_CONFIRMED {
		f.trace.add("fabric.absence.confirmed")
	}
	return readback, err
}

// This is an explicit Gateway/wallet boundary, served over gRPC to observe the
// actual Refund command. Its confirmed response proves ordering and binding,
// not Gateway implementation correctness or any real wallet effect.
type deletionLiveWalletBoundary struct {
	api.UnimplementedGatewayCoordinationServer
	mu      sync.Mutex
	command *api.WalletRefundCommand
	db      *sql.DB
	trace   *deletionLiveTrace
	pending bool
}

func (g *deletionLiveWalletBoundary) Refund(ctx context.Context, r *api.WalletRefundCommand) (*api.WalletOperation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.trace.add("gateway.refund")
	g.command = proto.Clone(r).(*api.WalletRefundCommand)
	before := g.trace.snapshot()
	if len(before) < 2 || before[len(before)-2] != "ledger.readback.confirmed" {
		return nil, status.Error(codes.FailedPrecondition, "refund preceded successful Ledger readback")
	}
	evidence, err := deletionLiveStoredReceipt(ctx, g.db, r.GetConfirmedDeletionReceiptId())
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "refund has no committed Ledger receipt: %v", err)
	}
	operationID := evidence.GetOwnerCommitEvidence().GetOperationId()
	if r.GetWorkspaceId() != "workspace-original" || r.GetOriginalWalletOperationId() != "wallet-op-charge-original" ||
		r.GetSubscriptionPeriodId() != "period-original" || r.GetRefundPolicyVersionId() != contracts.WorkspaceRefundPolicyVersion ||
		r.GetContext().GetIdempotencyKey() != operationID+":refund_confirmed_deletion" || r.GetContext().GetAcceptedOperationGrantId() != "grant-deletion-original" ||
		evidence.GetReceipt().GetOperationId() != operationID || evidence.GetReceipt().GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED ||
		r.GetAmountUsdMicros() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "refund differs from original deletion/payment")
	}
	status := api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED
	if g.pending {
		status = api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REQUESTED
	}
	return &api.WalletOperation{Id: "wallet-refund-boundary", WorkspaceId: proto.String(r.GetWorkspaceId()), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND,
		AmountUsdMicros: r.GetAmountUsdMicros(), Status: status, CreatedAt: timestamppb.Now()}, nil
}

func (g *deletionLiveWalletBoundary) ReadWalletAction(_ context.Context, r *api.WalletReadbackRequest) (*api.WalletOperation, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.command == nil || r.GetOriginalIdempotencyKey() != g.command.GetContext().GetIdempotencyKey() || !proto.Equal(r.GetContext(), g.command.GetContext()) {
		return nil, status.Error(codes.InvalidArgument, "refund readback differs from the original command")
	}
	g.trace.add("gateway.refund.readback")
	return &api.WalletOperation{Id: "wallet-refund-boundary", WorkspaceId: proto.String(g.command.WorkspaceId), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_REFUND,
		AmountUsdMicros: g.command.AmountUsdMicros, Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED, CreatedAt: timestamppb.Now()}, nil
}

func (g *deletionLiveWalletBoundary) refundCommand() *api.WalletRefundCommand {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.command == nil {
		return nil
	}
	return proto.Clone(g.command).(*api.WalletRefundCommand)
}

func TestLiveDeletionBackwardOwnerCoupling(t *testing.T) {
	// Only the explicitly supplied isolated test administrator DSN is accepted.
	// This suite never consults application or deployment database variables.
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	for _, tc := range []struct {
		name                           string
		present, unknown, writeFailure bool
		denyRead, pendingRefund        bool
		wantRows                       int
		wantEvents                     []string
	}{
		{name: "committed_receipt_readback_precedes_refund", wantRows: 1, wantEvents: []string{"fabric.absence.confirmed", "ledger.append", "ledger.committed", "ledger.readback", "ledger.readback.confirmed", "gateway.refund"}},
		{name: "requested_refund_settles_only_by_original_readback", pendingRefund: true, wantRows: 1, wantEvents: []string{"fabric.absence.confirmed", "ledger.append", "ledger.committed", "ledger.readback", "ledger.readback.confirmed", "gateway.refund"}},
		{name: "resources_present_no_receipt_or_refund", present: true},
		{name: "unknown_absence_no_receipt_or_refund", unknown: true},
		{name: "ledger_postgres_write_failure_no_refund", writeFailure: true, wantEvents: []string{"fabric.absence.confirmed", "ledger.append", "ledger.append.Unavailable"}},
		{name: "committed_but_readback_denied_no_refund", denyRead: true, wantRows: 1, wantEvents: []string{"fabric.absence.confirmed", "ledger.append", "ledger.committed", "ledger.readback", "ledger.readback.PermissionDenied"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
			defer cancel()
			workspaceDB := runtimeDatabase(t)
			workspace, call, _, periodStart, totalMicros := deletionWorkspace(t, workspaceDB)
			if totalMicros <= 0 {
				t.Fatal("backward deletion fixture requires an explicitly paid, nonzero period")
			}
			ledgerDB, ledgerOwner := liveLedgerDB(t, ctx, dsn)
			if tc.writeFailure {
				// An isolated-database constraint makes the real Ledger INSERT fail.
				// No Ledger RPC or persistence result is substituted by a stub.
				if _, err := ledgerDB.ExecContext(ctx, `ALTER TABLE evidence_receipts ADD CONSTRAINT deletion_test_reject_append CHECK (receipt_type <> 'workspace.provider_action.v1')`); err != nil {
					t.Fatal(err)
				}
			}
			trace := &deletionLiveTrace{}
			workspace.Fabric = &deletionLiveFabricBoundary{deletionFabricClient: deletionFabricClient{present: tc.present}, unknown: tc.unknown, trace: trace}
			workspaceAddress := liveOwnerServer(t, owneridentity.Workspace, []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.Ledger.Service()}, workspace.Register)
			ledgerOwner.Workspace = api.NewOwnerCommitReadbackClient(liveDial(t, workspaceAddress, owneridentity.Ledger, owneridentity.Workspace, livePeerToken))
			ledgerOwner.Authorizer = ownerservice.NewAuthorizer(owneridentity.Ledger, &deletionLiveIdentityBoundary{denyReceiptRead: tc.denyRead})
			ledgerServer, err := ledgerOwner.NewGRPC(ownerservice.Config{Owner: owneridentity.Ledger, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Workspace.Service(): livePeerToken}})
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			go ledgerServer.Serve(listener)
			t.Cleanup(ledgerServer.Stop)
			workspace.Ledger = &deletionLiveLedger{LedgerCoordinationClient: api.NewLedgerCoordinationClient(liveDial(t, listener.Addr().String(), owneridentity.Workspace, owneridentity.Ledger, livePeerToken)), db: ledgerDB, trace: trace}

			wallet := &deletionLiveWalletBoundary{db: ledgerDB, trace: trace, pending: tc.pendingRefund}
			gatewayAddress := liveOwnerServer(t, owneridentity.Gateway, []owneridentity.Service{owneridentity.Workspace.Service()}, func(s *ownerservice.Server) error {
				return s.RegisterGroup("GatewayCoordination", func(g *grpc.Server) { api.RegisterGatewayCoordinationServer(g, wallet) })
			})
			workspace.Gateway = api.NewGatewayCoordinationClient(liveDial(t, gatewayAddress, owneridentity.Workspace, owneridentity.Gateway, livePeerToken))
			client := api.NewWorkspaceProductServiceClient(liveDialService(t, workspaceAddress, owneridentity.ConsoleBFF, owneridentity.Workspace.Service(), livePeerToken))
			call.IdempotencyKey = "live-delete-backward"
			operation, err := client.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
			if err != nil {
				t.Fatal(err)
			}
			if got := trace.snapshot(); !reflect.DeepEqual(got, tc.wantEvents) {
				t.Fatalf("owner call order=%v, want %v", got, tc.wantEvents)
			}
			var rows int
			if err := ledgerDB.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type='workspace.provider_action.v1'`).Scan(&rows); err != nil || rows != tc.wantRows {
				t.Fatalf("persisted deletion receipts=%d err=%v, want %d", rows, err, tc.wantRows)
			}
			stored, err := workspace.Store.ReadOperation(ctx, operation.GetOperationId())
			if err != nil {
				t.Fatal(err)
			}
			order, result, err := decodeDeletion(stored)
			if err != nil {
				t.Fatal(err)
			}
			if order.LaunchOperationID != "operation-original" || order.SubscriptionPeriodID != "period-original" || order.OriginalChargeWalletOperationID != "wallet-op-charge-original" || order.OriginalChargeReceiptID != "wallet-receipt-original" || order.OriginalChargeUSDMicros != totalMicros {
				t.Fatalf("deletion lost original paid-order binding: %+v", order)
			}
			readback, err := client.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
			if err != nil {
				t.Fatal(err)
			}

			if tc.pendingRefund {
				if stored.Status == "succeeded" || readback.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_REQUESTED {
					t.Fatalf("requested refund was treated as settled: operation=%s readback=%v", stored.Status, readback)
				}
				if err := workspace.Resume(ctx, operation.GetOperationId()); err != nil {
					t.Fatal(err)
				}
				stored, err = workspace.Store.ReadOperation(ctx, operation.GetOperationId())
				if err != nil {
					t.Fatal(err)
				}
				order, result, err = decodeDeletion(stored)
				if err != nil {
					t.Fatal(err)
				}
				readback, err = client.GetWorkspaceDeletion(ctx, &api.GetWorkspaceDeletionRpcRequest{Context: call, WorkspaceId: "workspace-original"})
				if err != nil {
					t.Fatal(err)
				}
				refunds, reads := 0, 0
				for _, event := range trace.snapshot() {
					if event == "gateway.refund" {
						refunds++
					}
					if event == "gateway.refund.readback" {
						reads++
					}
				}
				if refunds != 1 || reads != 1 {
					t.Fatalf("refund was reissued instead of reading the original: refunds=%d reads=%d", refunds, reads)
				}
			}
			command := wallet.refundCommand()
			if tc.present || tc.unknown || tc.writeFailure || tc.denyRead {
				if command != nil || len(result.RefundCommand) != 0 || len(result.DeletionReceipt) != 0 || stored.Status == "succeeded" || readback.GetRefundStatus() == api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED {
					t.Fatalf("unconfirmed owner path reached refund/completion: command=%v result=%+v status=%s readback=%v", command, result, stored.Status, readback)
				}
				return
			}
			if command == nil || stored.Status != "succeeded" || readback.GetRefundStatus() != api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED || readback.GetRefundOperationId() != "wallet-refund-boundary" {
				t.Fatalf("confirmed deletion did not settle: command=%v status=%s readback=%v", command, stored.Status, readback)
			}
			deletedAt, err := time.Parse(time.RFC3339Nano, result.DeletedAt)
			if err != nil {
				t.Fatal(err)
			}
			wantAmount, err := contracts.PlatformWorkspaceDeleteRefundMicros(totalMicros, periodStart, deletedAt)
			if err != nil || wantAmount <= 0 || wantAmount >= totalMicros || command.GetAmountUsdMicros() != wantAmount {
				t.Fatalf("refund=%d, policy amount=%d original=%d err=%v", command.GetAmountUsdMicros(), wantAmount, totalMicros, err)
			}
			// Replaying the original deletion must not append or refund again.
			before := trace.snapshot()
			replay, err := client.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
			if err != nil || replay.GetOperationId() != operation.GetOperationId() || !reflect.DeepEqual(trace.snapshot(), before) {
				t.Fatalf("deletion replay changed original owner effects: operation=%v err=%v trace=%v", replay, err, trace.snapshot())
			}
		})
	}
}
