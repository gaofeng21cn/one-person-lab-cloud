package eventconsumer

import (
	"context"
	"database/sql"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/ledger/internal/ledger"
)

// gatewayAdjustmentOwners is the owner set the Gateway adjustment receipt
// needs: the Gateway owner answers the readback that proves the submitted
// evidence, and CloudIdentity answers the receipt owner's own authorization.
type gatewayAdjustmentOwners struct {
	api.UnimplementedCloudIdentityAuthorizationServer
	api.UnimplementedGatewayCoordinationServer
	mu     sync.Mutex
	wallet *api.WalletOperation
	reads  int
}

func (o *gatewayAdjustmentOwners) ReadWalletAction(_ context.Context, r *api.WalletReadbackRequest) (*api.WalletOperation, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.reads++
	if o.wallet == nil || r.GetWalletOperationId() != o.wallet.GetId() {
		return nil, status.Error(codes.NotFound, "wallet operation absent")
	}
	return proto.Clone(o.wallet).(*api.WalletOperation), nil
}

func (o *gatewayAdjustmentOwners) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	// The receipt owner authorizes against the Tenant the balance change
	// belongs to and never against a Workspace.
	if r.AudienceOwner != api.OwnerEnum_OWNER_ENUM_LEDGER || r.Resource.GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT || r.Resource.GetId() == "" {
		return nil, status.Error(codes.PermissionDenied, "wrong Ledger authorization binding")
	}
	return &api.AuthorizationDecision{Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		ActorId: r.ActorId, Scope: r.Scope, SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId, AudienceOwner: r.AudienceOwner, Action: r.Action, Resource: r.Resource,
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, nil
}

// gatewayAdjustmentCoordination starts the real Ledger coordination listener
// with the peers this slice needs: the tenant deployment unit that carries the
// Gateway owner writes the Gateway owner's own administrator balance change,
// and the Workspace and Fabric peers stay the writers of the funded receipts.
func gatewayAdjustmentCoordination(t *testing.T) (*sql.DB, *Server, *gatewayAdjustmentOwners, api.LedgerCoordinationClient, api.LedgerCoordinationClient, api.LedgerCoordinationClient) {
	t.Helper()
	db := coordinationDB(t)
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	owners := &gatewayAdjustmentOwners{}
	wire := grpc.NewServer()
	api.RegisterCloudIdentityAuthorizationServer(wire, owners)
	api.RegisterGatewayCoordinationServer(wire, owners)
	peerAddress := startCoordinationWire(t, wire)
	config := ownerservice.Config{Owner: owneridentity.Ledger, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers:             map[owneridentity.Service]string{owneridentity.Workspace.Service(): coordinationToken, owneridentity.Fabric.Service(): coordinationToken, owneridentity.Tenant.Service(): coordinationToken},
		CloudIdentityAddr: peerAddress, CloudIdentityToken: coordinationToken}
	close, err := s.ConfigureCoordination(config, func(name string) string {
		if strings.HasSuffix(name, "_ADDR") {
			return peerAddress
		}
		if strings.HasSuffix(name, "_TOKEN") {
			return coordinationToken
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(close)
	server, err := s.NewGRPC(config)
	if err != nil {
		t.Fatal(err)
	}
	address := startCoordinationWire(t, server)
	return db, s, owners, coordinationClient(t, address, owneridentity.Tenant), coordinationClient(t, address, owneridentity.Workspace), coordinationClient(t, address, owneridentity.Fabric)
}

// gatewayAdjustmentWalletOperation is the Gateway owner's own confirmed
// administrator balance change: the operator, the reason, the amount and the
// receipt id the owner named.
func gatewayAdjustmentWalletOperation() *api.WalletOperation {
	return &api.WalletOperation{
		Id:                "wallet-op-adjust-1",
		Kind:              api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_RECHARGE,
		AmountUsdMicros:   2_500_000,
		Status:            api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED,
		ExternalReference: proto.String("gw-adjust-native-1"),
		ReceiptId:         proto.String("recharge-receipt-wallet-op-adjust-1"),
		ActorId:           proto.String("operator-1"),
		Reason:            proto.String("operator_requested"),
		CreatedAt:         timestamppb.New(time.Now().Add(-time.Minute).UTC()),
		UpdatedAt:         timestamppb.New(time.Now().UTC()),
	}
}

// gatewayAdjustmentRequest is the append the Gateway owner sends: its own
// confirmed wallet operation, the receipt it named and the digest of that
// operation.
func gatewayAdjustmentRequest(t *testing.T, wallet *api.WalletOperation) *api.AppendReceiptRequest {
	t.Helper()
	digest, err := contracts.WalletOperationDigest(wallet)
	if err != nil {
		t.Fatal(err)
	}
	receipt := &api.Receipt{
		Kind:            api.ReceiptKindEnum_RECEIPT_KIND_ENUM_GATEWAY_WALLET_ADJUSTMENT,
		Owner:           api.OwnerEnum_OWNER_ENUM_GATEWAY,
		OperationId:     proto.String(wallet.GetId()),
		Outcome:         api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED,
		EvidenceSummary: "Administrator balance change confirmed by the Gateway owner.",
	}
	return &api.AppendReceiptRequest{
		Context: &api.CallContext{RequestId: "request-adjust-1", IdempotencyKey: "adjust-receipt-1", ActorId: "operator-1", SessionId: proto.String("session-operator-1"),
			Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-adjust"}}}},
		Receipt:                receipt,
		EvidenceDigest:         digest,
		OwnerEvidenceReference: wallet.GetId(),
		GatewayWalletAdjustmentEvidence: &api.GatewayWalletAdjustmentReceiptEvidence{
			Receipt: proto.Clone(receipt).(*api.Receipt), WalletOperation: proto.Clone(wallet).(*api.WalletOperation), EvidenceDigest: digest,
		},
	}
}

// TestGatewayWalletAdjustmentReceiptIsWrittenAndReadBack proves the Gateway
// owner's administrator balance change is recorded as an owner-acknowledged
// receipt: the Gateway peer appends it, the Ledger stores the owner's own
// confirmed wallet operation, and the receipt reads back by its own reference.
func TestGatewayWalletAdjustmentReceiptIsWrittenAndReadBack(t *testing.T) {
	db, s, owners, gateway, workspace, fabric := gatewayAdjustmentCoordination(t)
	ctx := context.Background()
	wallet := gatewayAdjustmentWalletOperation()
	owners.mu.Lock()
	owners.wallet = proto.Clone(wallet).(*api.WalletOperation)
	owners.mu.Unlock()
	request := gatewayAdjustmentRequest(t, wallet)

	appended, err := gateway.AppendReceipt(ctx, request)
	if err != nil {
		t.Fatalf("append the administrator balance change: %v", err)
	}
	if appended.GetId() != wallet.GetReceiptId() || appended.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_GATEWAY_WALLET_ADJUSTMENT ||
		appended.GetOwner() != api.OwnerEnum_OWNER_ENUM_GATEWAY || appended.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED ||
		appended.GetCreatedAt() == nil || appended.GetOperationId() != wallet.GetId() {
		t.Fatalf("unexpected appended receipt: %+v", appended)
	}
	// The Ledger re-read the named operation from the Gateway owner before it
	// stored anything.
	if owners.reads != 1 {
		t.Fatalf("the Gateway owner was read %d times", owners.reads)
	}

	// The stored row is the tenant-scoped fact: it names the Tenant and no
	// Workspace.
	var receiptType, organization, workspaceID, payload string
	if err = db.QueryRowContext(ctx, `SELECT receipt_type,organization_id,workspace_id,payload_json FROM evidence_receipts WHERE id=$1`, wallet.GetReceiptId()).Scan(&receiptType, &organization, &workspaceID, &payload); err != nil {
		t.Fatal(err)
	}
	if receiptType != ledger.GatewayWalletAdjustmentReceiptType || organization != "tenant-adjust" || workspaceID != "" {
		t.Fatalf("stored receipt type=%q organization=%q workspace=%q", receiptType, organization, workspaceID)
	}

	// The receipt reads back by its own reference, in Receipt space and in the
	// owner's typed evidence space.
	read := &api.GetReceiptByReferenceRequest{Context: proto.Clone(request.Context).(*api.CallContext), Owner: "gateway", OwnerEvidenceReference: wallet.GetId()}
	public, err := gateway.ReadReceiptByReference(ctx, read)
	if err != nil || !proto.Equal(public, appended) {
		t.Fatalf("public read=%v %v", public, err)
	}
	typed, err := gateway.ReadGatewayWalletAdjustmentReceipt(ctx, read)
	if err != nil {
		t.Fatalf("typed read: %v", err)
	}
	if !proto.Equal(typed.GetReceipt(), appended) || !proto.Equal(typed.GetWalletOperation(), wallet) {
		t.Fatalf("typed evidence differs: %+v", typed)
	}

	// The same append under the same key replays the original receipt, never a
	// second balance change.
	replay := proto.Clone(request).(*api.AppendReceiptRequest)
	replay.Context.RequestId = "request-adjust-2"
	second, err := gateway.AppendReceipt(ctx, replay)
	if err != nil || !proto.Equal(second, appended) {
		t.Fatalf("replay=%v %v", second, err)
	}
	var rows int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type=$1`, ledger.GatewayWalletAdjustmentReceiptType).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("stored rows=%d %v", rows, err)
	}

	// Only the peer that carries the Gateway owner may write this kind. The
	// Workspace and Fabric peers write the funded receipts, never the
	// administrator's balance change.
	for name, client := range map[string]api.LedgerCoordinationClient{"workspace": workspace, "fabric": fabric} {
		if _, err = client.AppendReceipt(ctx, request); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("the %s peer wrote the Gateway owner's receipt: %v", name, err)
		}
		if _, err = client.ReadGatewayWalletAdjustmentReceipt(ctx, read); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("the %s peer read the Gateway owner's receipt: %v", name, err)
		}
	}
	// The generic HTTP writer may never mint this type.
	if _, err = s.store.RecordReceipt(ctx, ledger.ReceiptInput{Type: ledger.GatewayWalletAdjustmentReceiptType, Status: "completed", Surface: "cloud", IdempotencyKey: "forged-adjustment"}); err != ledger.ErrInvalidReceiptInput {
		t.Fatalf("the generic writer minted the adjustment receipt: %v", err)
	}
}

// TestGatewayWalletAdjustmentReceiptRefusesUnownedEvidence proves the receipt
// records only what the Gateway owner itself confirmed: an operation the owner
// did not record, a borrowed digest and a foreign operator are all refused.
func TestGatewayWalletAdjustmentReceiptRefusesUnownedEvidence(t *testing.T) {
	_, _, owners, gateway, _, _ := gatewayAdjustmentCoordination(t)
	ctx := context.Background()
	wallet := gatewayAdjustmentWalletOperation()
	owners.mu.Lock()
	owners.wallet = proto.Clone(wallet).(*api.WalletOperation)
	owners.mu.Unlock()
	base := gatewayAdjustmentRequest(t, wallet)

	for _, tc := range []struct {
		name   string
		mutate func(*api.AppendReceiptRequest, *api.WalletOperation)
		code   codes.Code
	}{
		{"another operation", func(r *api.AppendReceiptRequest, owned *api.WalletOperation) {
			// The Gateway owner confirmed a different amount, so the submitted
			// evidence is not the owner's own record.
			owned.AmountUsdMicros = 3_000_000
		}, codes.FailedPrecondition},
		{"borrowed digest", func(r *api.AppendReceiptRequest, owned *api.WalletOperation) {
			r.GatewayWalletAdjustmentEvidence.EvidenceDigest = "sha256:" + strings.Repeat("a", 64)
			r.EvidenceDigest = r.GatewayWalletAdjustmentEvidence.EvidenceDigest
		}, codes.InvalidArgument},
		{"foreign operator", func(r *api.AppendReceiptRequest, owned *api.WalletOperation) {
			r.Context.ActorId = "operator-other"
		}, codes.InvalidArgument},
		{"workspace scoped", func(r *api.AppendReceiptRequest, owned *api.WalletOperation) {
			r.GatewayWalletAdjustmentEvidence.WalletOperation.WorkspaceId = proto.String("workspace-other")
		}, codes.InvalidArgument},
		{"unconfirmed operation", func(r *api.AppendReceiptRequest, owned *api.WalletOperation) {
			r.GatewayWalletAdjustmentEvidence.WalletOperation.Status = api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN
		}, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := proto.Clone(base).(*api.AppendReceiptRequest)
			owned := proto.Clone(wallet).(*api.WalletOperation)
			tc.mutate(r, owned)
			owners.mu.Lock()
			owners.wallet = owned
			owners.mu.Unlock()
			if _, err := gateway.AppendReceipt(ctx, r); status.Code(err) != tc.code {
				t.Fatalf("error=%v, want %v", err, tc.code)
			}
		})
	}
}

// gatewayAdjustmentLiveListener is the address the Ledger coordination server
// listens on, used by the Gateway owner's own suite to reach the real receipt
// owner over the wire.
func gatewayAdjustmentLiveListener(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return listener.Addr().String()
}
