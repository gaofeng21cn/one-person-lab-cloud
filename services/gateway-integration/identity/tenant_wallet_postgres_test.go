package identity_test

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// TestTenantWalletReadsThePayingAccountInsteadOfThePersonalOne proves the two
// wallet reads are different facts. The caller's own account is the balance
// their personal Keys spend from; the Tenant's paying account is the balance an
// accepted Workspace order is charged to. A purchase gate that compares the
// price against the personal balance decides on the wrong number, so the Tenant
// read must resolve the tenant's own wallet binding.
func TestTenantWalletReadsThePayingAccountInsteadOfThePersonalOne(t *testing.T) {
	service, db, cookie, console, _ := coordinationSystem(t, &gatewayWalletStub{})
	if _, err := console.BindWallet(t.Context(), &api.WalletBindingCommand{Context: platformCaller("bind", cookie), TargetTenantId: "tenant-coord", BillingSub2ApiUserId: "77", AuthorizationReceiptId: "auth-receipt-1"}); err != nil {
		t.Fatalf("bind wallet: %v", err)
	}
	peerCtx := ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF)
	call := tenantWalletCaller(cookie)

	tenantWallet, err := service.GetTenantWallet(peerCtx, &api.GetTenantWalletRpcRequest{Context: call})
	if err != nil {
		t.Fatalf("tenant wallet: %v", err)
	}
	// The tenant's payer is the account the binding names, not the caller's own.
	if tenantWallet.GetBalanceUsdMicros() != 12_500_000 {
		t.Fatalf("tenant wallet=%v want the payer's balance", tenantWallet)
	}
	if tenantWallet.GetSource() != api.WalletSourceEnum_WALLET_SOURCE_ENUM_GATEWAY || tenantWallet.GetStatus() != api.WalletStatusEnum_WALLET_STATUS_ENUM_AVAILABLE {
		t.Fatalf("tenant wallet=%v", tenantWallet)
	}
	personal, err := service.GetWallet(peerCtx, &api.GetWalletRpcRequest{Context: call})
	if err != nil {
		t.Fatalf("personal wallet: %v", err)
	}
	if personal.GetBalanceUsdMicros() == tenantWallet.GetBalanceUsdMicros() {
		t.Fatalf("the tenant read answered the personal account: %v", personal)
	}

	call.Scope.GetTenant().TenantId = "different-tenant"
	if _, err := service.GetTenantWallet(peerCtx, &api.GetTenantWalletRpcRequest{Context: call}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-tenant wallet err=%v want permission denied", err)
	}
	call.Scope.GetTenant().TenantId = "tenant-coord"
	for _, role := range []string{"owner", "member"} {
		if _, err := db.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role=$1 WHERE actor_id='101'`, role); err != nil {
			t.Fatal(err)
		}
		wallet, err := service.GetTenantWallet(peerCtx, &api.GetTenantWalletRpcRequest{Context: call})
		if role == "owner" && (err != nil || wallet.GetBalanceUsdMicros() != tenantWallet.GetBalanceUsdMicros()) {
			t.Fatalf("owner wallet=%v err=%v", wallet, err)
		}
		if role == "member" && status.Code(err) != codes.PermissionDenied {
			t.Fatalf("member wallet=%v err=%v want permission denied", wallet, err)
		}
	}
	if wallet, err := service.GetWallet(peerCtx, &api.GetWalletRpcRequest{Context: call}); err != nil || wallet.GetBalanceUsdMicros() != personal.GetBalanceUsdMicros() {
		t.Fatalf("member personal wallet=%v err=%v", wallet, err)
	}
}

// TestTenantWalletRefusesATenantWithoutAPayer proves the read never invents a
// balance for a Tenant that has no paying account: an unbound Tenant is
// refused instead of answering zero, which would look like an empty wallet.
func TestTenantWalletRefusesATenantWithoutAPayer(t *testing.T) {
	service, _, cookie, _, _ := coordinationSystem(t, &gatewayWalletStub{})
	peerCtx := ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF)
	call := tenantWalletCaller(cookie)
	if _, err := service.GetTenantWallet(peerCtx, &api.GetTenantWalletRpcRequest{Context: call}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("err=%v want the unbound tenant refused", err)
	}
}

// TestTenantWalletNeedsTheConsoleBFFPeer proves the read stays on the owner's
// own boundary: an in-process call without the Console BFF peer identity never
// reaches the payer.
func TestTenantWalletNeedsTheConsoleBFFPeer(t *testing.T) {
	service, _, cookie, console, _ := coordinationSystem(t, &gatewayWalletStub{})
	if _, err := console.BindWallet(t.Context(), &api.WalletBindingCommand{Context: platformCaller("bind", cookie), TargetTenantId: "tenant-coord", BillingSub2ApiUserId: "77", AuthorizationReceiptId: "auth-receipt-1"}); err != nil {
		t.Fatalf("bind wallet: %v", err)
	}
	call := tenantWalletCaller(cookie)
	if _, err := service.GetTenantWallet(t.Context(), &api.GetTenantWalletRpcRequest{Context: call}); err == nil {
		t.Fatal("an unauthenticated in-process call reached the tenant payer")
	}
}

func tenantWalletCaller(cookie string) *api.CallContext {
	return &api.CallContext{
		RequestId: "tenant-wallet-read", ActorId: "101", SessionId: proto.String(owneridentity.SessionReference(cookie)),
		Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-coord"}}},
	}
}
