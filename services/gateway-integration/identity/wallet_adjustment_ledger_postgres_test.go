package identity_test

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/ledger/eventconsumer"
)

// adjustmentLedgerToken is the peer token the Gateway Integration deployment
// unit and the receipt owner share in this suite.
const adjustmentLedgerToken = "isolated-adjustment-ledger-token-0001"

// adjustmentLedgerDB opens an isolated schema for the receipt owner's own
// store, the same way the receipt owner's own suite does.
func adjustmentLedgerDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := strings.TrimSpace(os.Getenv("LEDGER_TEST_DATABASE_URL"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("OPL_OWNER_MIGRATION_TEST_ADMIN_DSN"))
	}
	if dsn == "" {
		t.Skip("set OPL_OWNER_MIGRATION_TEST_ADMIN_DSN to run the adjustment receipt suite against PostgreSQL")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = admin.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("adjustment_ledger_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	testDSN := dsn
	if parsed, err := url.Parse(dsn); err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		testDSN = parsed.String()
	} else {
		testDSN += " search_path=" + "'" + strings.ReplaceAll(schema, "'", "''") + "'"
	}
	db, err := sql.Open("pgx", testDSN)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() {
		db.Close()
		admin.Exec("DROP SCHEMA " + pgx.Identifier{schema}.Sanitize() + " CASCADE")
		admin.Close()
	})
	return db
}

// adjustmentReceiptOwner starts the real receipt owner in front of the Gateway
// owner: the Ledger coordination listener over its own isolated store. It
// reaches the Gateway owner to re-read the confirmed wallet operation, and it
// reaches the CloudIdentity authority that the same deployment unit serves.
//
// The Gateway owner reaches the receipt owner as the tenant deployment unit
// that carries it, because that unit is the only transport identity the
// process can present.
func adjustmentReceiptOwner(t *testing.T, gatewayAddress string) (*sql.DB, api.LedgerCoordinationClient) {
	t.Helper()
	ctx := t.Context()
	ledgerDB := adjustmentLedgerDB(t)
	ledgerServer, err := eventconsumer.New(ledgerDB)
	if err != nil {
		t.Fatal(err)
	}
	if err = ledgerServer.Install(ctx); err != nil {
		t.Fatal(err)
	}
	ledgerConfig := ownerservice.Config{Owner: owneridentity.Ledger, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers:             map[owneridentity.Service]string{owneridentity.Tenant.Service(): adjustmentLedgerToken},
		CloudIdentityAddr: gatewayAddress, CloudIdentityToken: adjustmentLedgerToken}
	closeCoordination, err := ledgerServer.ConfigureCoordination(ledgerConfig, func(name string) string {
		switch {
		case strings.HasSuffix(name, "_ADDR"):
			return gatewayAddress
		case strings.HasSuffix(name, "_TOKEN"):
			return adjustmentLedgerToken
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeCoordination)
	ledgerGRPC, err := ledgerServer.NewGRPC(ledgerConfig)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go ledgerGRPC.Serve(listener)
	t.Cleanup(ledgerGRPC.Stop)
	options, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(owneridentity.Tenant.Service(), owneridentity.Ledger.Service(), adjustmentLedgerToken)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(listener.Addr().String(), options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return ledgerDB, api.NewLedgerCoordinationClient(conn)
}

// TestAdminWalletAdjustmentAppendsItsLedgerReceipt proves the administrator's
// balance change is recorded in the Ledger as the Gateway owner's own
// receipt: the money moves once, the receipt names the owner's confirmed
// operation with its operator and reason, and the receipt reads back from the
// receipt owner.
func TestAdminWalletAdjustmentAppendsItsLedgerReceipt(t *testing.T) {
	stub := &gatewayWalletStub{}
	_, gatewayDB, ledgerDB, cookie, client, ledger := walletAdjustmentSystem(t, stub)

	operation, err := client.AdjustTenantWallet(t.Context(), &api.AdjustTenantWalletRpcRequest{
		Context:  adminAdjustmentCall("adjust-receipt-1", cookie),
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:            api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_RECHARGE,
			AmountUsdMicros: 2_500_000,
			Reason:          "operator_requested",
		},
	})
	if err != nil {
		t.Fatalf("AdjustTenantWallet: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("unexpected adjustment operation: %+v", operation)
	}
	if stub.applied != 1 {
		t.Fatalf("the balance moved %d times", stub.applied)
	}

	// The owner's own row carries the receipt the receipt owner records.
	var kind, state, actor, reason, receipt string
	var amount int64
	if err = gatewayDB.QueryRowContext(t.Context(), `SELECT kind,status,amount_usd_micros,COALESCE(actor_id,''),COALESCE(reason,''),COALESCE(receipt_id,'')
		FROM gateway.wallet_operations WHERE id=$1`, operation.GetOperationId()).Scan(&kind, &state, &amount, &actor, &reason, &receipt); err != nil {
		t.Fatal(err)
	}
	if kind != "recharge" || state != "confirmed" || amount != 2_500_000 || actor != "101" || reason != "operator_requested" || receipt == "" {
		t.Fatalf("recorded adjustment kind=%q status=%q amount=%d actor=%q reason=%q receipt=%q", kind, state, amount, actor, reason, receipt)
	}

	// The receipt owner stored the owner's own confirmed operation.
	var receiptType, organization, workspaceID string
	if err = ledgerDB.QueryRowContext(t.Context(), `SELECT receipt_type,organization_id,workspace_id FROM evidence_receipts WHERE id=$1`, receipt).Scan(&receiptType, &organization, &workspaceID); err != nil {
		t.Fatal(err)
	}
	if receiptType != "gateway.wallet_adjustment.confirmed.v1" || organization != "tenant-adjust" || workspaceID != "" {
		t.Fatalf("stored receipt type=%q organization=%q workspace=%q", receiptType, organization, workspaceID)
	}

	// The receipt reads back from the receipt owner, in Receipt space and in
	// the owner's typed evidence space.
	read := &api.GetReceiptByReferenceRequest{
		Context: &api.CallContext{RequestId: "req-read-receipt", IdempotencyKey: "opl-test:read-receipt", ActorId: "101",
			SessionId: proto.String(owneridentity.SessionReference(cookie)),
			Scope:     &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-adjust"}}}},
		Owner: "gateway", OwnerEvidenceReference: operation.GetOperationId(),
	}
	public, err := ledger.ReadReceiptByReference(t.Context(), read)
	if err != nil {
		t.Fatalf("read the adjustment receipt back: %v", err)
	}
	if public.GetId() != receipt || public.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_GATEWAY_WALLET_ADJUSTMENT ||
		public.GetOwner() != api.OwnerEnum_OWNER_ENUM_GATEWAY || public.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED ||
		public.GetOperationId() != operation.GetOperationId() {
		t.Fatalf("unexpected adjustment receipt: %+v", public)
	}
	typed, err := ledger.ReadGatewayWalletAdjustmentReceipt(t.Context(), read)
	if err != nil {
		t.Fatalf("read the typed adjustment evidence back: %v", err)
	}
	if typed.GetWalletOperation().GetActorId() != "101" || typed.GetWalletOperation().GetReason() != "operator_requested" ||
		typed.GetWalletOperation().GetAmountUsdMicros() != 2_500_000 || typed.GetWalletOperation().GetReceiptId() != receipt {
		t.Fatalf("unexpected typed evidence: %+v", typed.GetWalletOperation())
	}

	// A replay of the same command replays the same receipt and never moves
	// the balance a second time.
	replay, err := client.AdjustTenantWallet(t.Context(), &api.AdjustTenantWalletRpcRequest{
		Context:  adminAdjustmentCall("adjust-receipt-1", cookie),
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:            api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_RECHARGE,
			AmountUsdMicros: 2_500_000,
			Reason:          "operator_requested",
		},
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.GetOperationId() != operation.GetOperationId() || stub.applied != 1 {
		t.Fatalf("the replay moved the balance again: %+v applied=%d", replay, stub.applied)
	}
	var rows int
	if err = ledgerDB.QueryRowContext(t.Context(), `SELECT count(*) FROM evidence_receipts WHERE receipt_type='gateway.wallet_adjustment.confirmed.v1'`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("stored receipts=%d %v", rows, err)
	}
}

// TestAdminWalletAdjustmentWithoutTheReceiptOwnerIsRefused proves the owner
// does not pretend a balance change was acknowledged when the receipt owner
// is absent: the command fails instead of returning a success no receipt
// backs.
func TestAdminWalletAdjustmentWithoutTheReceiptOwnerIsRefused(t *testing.T) {
	stub := &gatewayWalletStub{}
	s, _, _, cookie, client, _ := walletAdjustmentSystem(t, stub)
	s.Ledger = nil

	_, err := client.AdjustTenantWallet(t.Context(), &api.AdjustTenantWalletRpcRequest{
		Context:  adminAdjustmentCall("adjust-no-receipt", cookie),
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:            api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_RECHARGE,
			AmountUsdMicros: 2_500_000,
			Reason:          "operator_requested",
		},
	})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("expected an unavailable receipt owner, got %v", err)
	}
}
