package identity_test

import (
	"context"
	"database/sql"
	"net"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/gatewaymigrations"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// walletAdjustmentSystem provisions the owner exactly as the settlement system
// does: one isolated PostgreSQL tenant database, one isolated PostgreSQL
// Gateway database with the owner's own migrations, a real Gateway fixture, a
// real platform administrator, the real receipt owner in front of it and a real
// gRPC client.
func walletAdjustmentSystem(t *testing.T, stub *gatewayWalletStub) (*identity.Service, *sql.DB, *sql.DB, string, api.GatewayProductServiceClient, api.LedgerCoordinationClient) {
	t.Helper()
	ctx := t.Context()
	admin := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)

	tenant, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: admin, Owner: "tenant", Database: "opl_tenant", SchemaOwnerRole: "opl_tenant_owner", WriterRole: "opl_tenant_writer", RuntimeRole: "opl_tenant_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tenant.Close(context.Background()) })
	tenantSource, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = tenant.Install(ctx, tenant.OwnerDSN, tenant.DatabaseName(), tenantSource); err != nil {
		t.Fatal(err)
	}
	tenantDB, err := tenant.Open(ctx, tenant.RuntimeDSN, tenant.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tenantDB.Close() })
	if _, err = tenantDB.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name)VALUES('tenant-adjust','Adjusted')`); err != nil {
		t.Fatal(err)
	}
	if _, err = tenantDB.ExecContext(ctx, `INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role)VALUES('member-adjust','tenant-adjust','101','admin')`); err != nil {
		t.Fatal(err)
	}

	gateway, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: admin, Owner: "gateway", Database: "opl_gateway", SchemaOwnerRole: "opl_gateway_owner", WriterRole: "opl_gateway_writer", RuntimeRole: "opl_gateway_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gateway.Close(context.Background()) })
	gatewaySource, err := gatewaymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = gateway.Install(ctx, gateway.OwnerDSN, gateway.DatabaseName(), gatewaySource); err != nil {
		t.Fatal(err)
	}
	gatewayDB, err := gateway.Open(ctx, gateway.RuntimeDSN, gateway.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gatewayDB.Close() })
	// The Tenant's spendable balance is the Gateway subject its wallet binding
	// names. The owner never asks the Gateway about anyone else.
	if _, err = gatewayDB.ExecContext(ctx, `INSERT INTO gateway.tenant_wallet_bindings(id,tenant_id,billing_sub2api_user_id,delegation_ref,verification_evidence_ref,bound_at)
		VALUES('binding-adjust','tenant-adjust','77','delegation-adjust','verification-adjust',now())`); err != nil {
		t.Fatal(err)
	}

	sub2api := httptest.NewServer(stub.handler())
	t.Cleanup(sub2api.Close)
	g, err := identity.NewGatewayWithDirectory(sub2api.URL, &identity.GatewayDirectory{Email: "admin@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := identity.New(tenantDB, g, []byte(strings.Repeat("s", 32)), []string{"101"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store, err := identity.NewGatewayStore(gatewayDB)
	if err != nil {
		t.Fatal(err)
	}
	s.GatewayStore = store

	const consoleToken = "isolated-adjust-console-bff-token-0001"
	config := ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: consoleToken, owneridentity.Ledger.Service(): adjustmentLedgerToken}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Register(server); err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)

	// The administrator's balance change is acknowledged by the receipt owner,
	// so the owner reaches it as the tenant deployment unit that carries the
	// Gateway owner.
	ledgerDB, ledger := adjustmentReceiptOwner(t, listener.Addr().String())
	s.Ledger = ledger

	consoleConn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, consoleToken)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { consoleConn.Close() })

	var header metadata.MD
	if _, err = api.NewTenantProductServiceClient(consoleConn).GetLoginContext(ctx, &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	challenge := header.Get(owneridentity.SessionCookieHeader)[0]
	session, err := api.NewTenantProductServiceClient(consoleConn).Login(ctx, &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: "admin@example.test", Password: "password"}}, grpc.Header(&header))
	if err != nil {
		t.Fatal(err)
	}
	if session.ActorId != "101" {
		t.Fatalf("unexpected session actor %s", session.ActorId)
	}
	return s, gatewayDB, ledgerDB, header.Get(owneridentity.SessionCookieHeader)[0], api.NewGatewayProductServiceClient(consoleConn), ledger
}

// adminAdjustmentCall is the platform administrator's own command context.
func adminAdjustmentCall(step, cookie string) *api.CallContext {
	return platformCaller(step, cookie)
}

// TestAdminWalletAdjustmentMovesTheBalanceOnce proves the administrator's
// balance change is the owner's own money fact: it moves the balance once, it
// records the operator and the reason with the money, it replays to the
// original operation, and a different command under the same key is refused.
func TestAdminWalletAdjustmentMovesTheBalanceOnce(t *testing.T) {
	stub := &gatewayWalletStub{}
	_, gatewayDB, _, cookie, client, _ := walletAdjustmentSystem(t, stub)

	command := &api.AdjustTenantWalletRpcRequest{
		Context:  adminAdjustmentCall("adjust-1", cookie),
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:            api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_RECHARGE,
			AmountUsdMicros: 2_500_000,
			Reason:          "operator_requested",
		},
	}
	operation, err := client.AdjustTenantWallet(t.Context(), command)
	if err != nil {
		t.Fatalf("AdjustTenantWallet: %v", err)
	}
	if operation.GetOperationId() == "" || operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_WALLET_ADJUSTMENT ||
		operation.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY ||
		operation.GetResourceId() != "tenant-adjust" ||
		operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("unexpected adjustment operation: %+v", operation)
	}

	// The money moved exactly once, and the owner's own record carries the
	// operator, the reason and the Gateway's own adjustment code.
	if stub.applied != 1 {
		t.Fatalf("the balance moved %d times", stub.applied)
	}
	var kind, state, actor, reason, external, receipt string
	var amount int64
	if err = gatewayDB.QueryRowContext(t.Context(), `SELECT kind,status,amount_usd_micros,COALESCE(actor_id,''),COALESCE(reason,''),COALESCE(external_reference,''),COALESCE(receipt_id,'')
		FROM gateway.wallet_operations WHERE id=$1`, operation.GetOperationId()).
		Scan(&kind, &state, &amount, &actor, &reason, &external, &receipt); err != nil {
		t.Fatal(err)
	}
	if kind != "recharge" || state != "confirmed" || amount != 2_500_000 || actor != "101" || reason != "operator_requested" || external == "" || receipt == "" {
		t.Fatalf("recorded adjustment kind=%q status=%q amount=%d actor=%q reason=%q external=%q receipt=%q", kind, state, amount, actor, reason, external, receipt)
	}

	// The same key replays the original operation and never moves the balance
	// a second time.
	replay, err := client.AdjustTenantWallet(t.Context(), command)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.GetOperationId() != operation.GetOperationId() || stub.applied != 1 {
		t.Fatalf("replay moved the balance again: %+v applied=%d", replay, stub.applied)
	}

	// The same key with a different command is a conflict, not a second change.
	changed := proto.Clone(command).(*api.AdjustTenantWalletRpcRequest)
	changed.Body.AmountUsdMicros = 3_000_000
	if _, err = client.AdjustTenantWallet(t.Context(), changed); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected a conflict, got %v", err)
	}
	if stub.applied != 1 {
		t.Fatalf("a conflicting command moved the balance again: %d", stub.applied)
	}

	// The administrator reads the change back from the owner's own record.
	page, err := client.ListRechargeRecords(t.Context(), &api.ListRechargeRecordsRpcRequest{
		Context: adminAdjustmentCall("adjust-read", cookie), QueryTenantId: proto.String("tenant-adjust"),
	})
	if err != nil {
		t.Fatalf("ListRechargeRecords: %v", err)
	}
	if len(page.GetItems()) != 1 || page.GetItems()[0].GetId() != operation.GetOperationId() ||
		page.GetItems()[0].GetKind() != api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_RECHARGE ||
		page.GetItems()[0].GetAmountUsdMicros() != 2_500_000 ||
		page.GetItems()[0].GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED {
		t.Fatalf("unexpected adjustment page: %+v", page)
	}
}

// TestAdminWalletAdjustmentRefusesAnUnsafeCommand proves the owner's guards:
// a debit is recorded as a debit, a business refund names its original charge,
// a refund never exceeds that charge, and a customer cannot adjust a balance.
func TestAdminWalletAdjustmentRefusesAnUnsafeCommand(t *testing.T) {
	stub := &gatewayWalletStub{}
	_, gatewayDB, _, cookie, client, _ := walletAdjustmentSystem(t, stub)

	// A debit collects from the customer, so the Gateway's own record of it is
	// negative.
	debit, err := client.AdjustTenantWallet(t.Context(), &api.AdjustTenantWalletRpcRequest{
		Context:  adminAdjustmentCall("adjust-debit", cookie),
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:            api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_DEBIT,
			AmountUsdMicros: 1_000_000,
			Reason:          "operator_requested",
		},
	})
	if err != nil {
		t.Fatalf("AdjustTenantWallet debit: %v", err)
	}
	if debit.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("unexpected debit operation: %+v", debit)
	}
	var kind, external string
	if err = gatewayDB.QueryRowContext(t.Context(), `SELECT kind,COALESCE(external_reference,'') FROM gateway.wallet_operations WHERE id=$1`, debit.GetOperationId()).Scan(&kind, &external); err != nil {
		t.Fatal(err)
	}
	if kind != "debit" || external == "" {
		t.Fatalf("recorded debit kind=%q external=%q", kind, external)
	}
	stub.mu.Lock()
	native, applied := stub.entries[external], stub.applied
	stub.mu.Unlock()
	if native.String() != "-1.000000" {
		t.Fatalf("the native record of a debit is %q", native.String())
	}
	if applied != 1 {
		t.Fatalf("the balance moved %d times", applied)
	}

	// A business refund names the confirmed original charge it credits back.
	if _, err = client.AdjustTenantWallet(t.Context(), &api.AdjustTenantWalletRpcRequest{
		Context:  adminAdjustmentCall("adjust-refund-missing", cookie),
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:            api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_BUSINESS_REFUND,
			AmountUsdMicros: 1_000_000,
			Reason:          "operator_requested",
		},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("expected a refused refund, got %v", err)
	}
	if _, err = client.AdjustTenantWallet(t.Context(), &api.AdjustTenantWalletRpcRequest{
		Context:  adminAdjustmentCall("adjust-refund-absent", cookie),
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:                      api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_BUSINESS_REFUND,
			AmountUsdMicros:           1_000_000,
			Reason:                    "operator_requested",
			OriginalChargeOperationId: proto.String("wallet-op-absent"),
		},
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected a refused refund, got %v", err)
	}

	// A business refund credits back a confirmed original charge and never
	// exceeds it, not even together with the refunds already recorded against
	// that same charge.
	if _, err = gatewayDB.ExecContext(t.Context(), `INSERT INTO gateway.wallet_operations(id,tenant_id,wallet_binding_id,kind,status,amount_usd_micros,business_idempotency_key,request_fingerprint,external_reference,confirmed_at)
		VALUES('wallet-op-charge','tenant-adjust','binding-adjust','charge','confirmed',5000000,'charge-key','charge-fingerprint','charge-external',now())`); err != nil {
		t.Fatal(err)
	}
	if _, err = client.AdjustTenantWallet(t.Context(), &api.AdjustTenantWalletRpcRequest{
		Context:  adminAdjustmentCall("adjust-refund-ok", cookie),
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:                      api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_BUSINESS_REFUND,
			AmountUsdMicros:           3_000_000,
			Reason:                    "operator_requested",
			OriginalChargeOperationId: proto.String("wallet-op-charge"),
		},
	}); err != nil {
		t.Fatalf("the first business refund was refused: %v", err)
	}
	if _, err = client.AdjustTenantWallet(t.Context(), &api.AdjustTenantWalletRpcRequest{
		Context:  adminAdjustmentCall("adjust-refund-over", cookie),
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:                      api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_BUSINESS_REFUND,
			AmountUsdMicros:           3_000_000,
			Reason:                    "operator_requested",
			OriginalChargeOperationId: proto.String("wallet-op-charge"),
		},
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("the cumulative refunds exceeded the original charge: %v", err)
	}
	if _, err = client.AdjustTenantWallet(t.Context(), &api.AdjustTenantWalletRpcRequest{
		Context:  adminAdjustmentCall("adjust-refund-too-much", cookie),
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:                      api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_BUSINESS_REFUND,
			AmountUsdMicros:           6_000_000,
			Reason:                    "operator_requested",
			OriginalChargeOperationId: proto.String("wallet-op-charge"),
		},
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a refund larger than the original charge was accepted: %v", err)
	}

	// A customer is not a platform administrator.
	session := owneridentity.SessionReference(cookie)
	if _, err = client.AdjustTenantWallet(t.Context(), &api.AdjustTenantWalletRpcRequest{
		Context:  &api.CallContext{ActorId: "someone-else", SessionId: &session, RequestId: "req-forged", IdempotencyKey: "opl-test:forged", Scope: platformScope()},
		TenantId: "tenant-adjust",
		Body: &api.WalletAdjustmentRequest{
			Kind:            api.WalletAdjustmentRequestKindEnum_WALLET_ADJUSTMENT_REQUEST_KIND_ENUM_RECHARGE,
			AmountUsdMicros: 1_000_000,
			Reason:          "operator_requested",
		},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a customer adjusted a balance: %v", err)
	}
}
