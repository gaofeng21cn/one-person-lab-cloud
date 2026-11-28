package identity_test

import (
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func TestLinkRelayEntitlementLifecycleAndSeedPostgres(t *testing.T) {
	// Include >50 users to exercise paginated seeding with DirectoryUsers pageSize=50,
	// alongside bootstrap admin (103), delegated admin candidate (201), regular user (202), and disabled user (203).
	users := []directoryUserFixture{
		{id: 103, email: "root@example.test", status: "active", balance: "25.0", keyCount: 1},
		{id: 201, email: "delegate@example.test", status: "active", balance: "10.0", keyCount: 1},
		{id: 202, email: "user202@example.test", status: "active", balance: "5.0", keyCount: 0},
		{id: 203, email: "disabled203@example.test", status: "disabled", balance: "0", keyCount: 0},
	}
	for i := 300; i < 350; i++ {
		users = append(users, directoryUserFixture{
			id:      int64(i),
			email:   fmt.Sprintf("seed-%d@example.test", i),
			status:  "active",
			balance: "1.0",
		})
	}
	sys := newPlatformAdminSystem(t, []string{"103"}, users)
	ctx := t.Context()

	rootCookie, rootSession := memberLogin(t, sys.bffClient, "root@example.test")
	rootSessionID := owneridentity.SessionReference(rootCookie)
	rootCall := platformCall(rootSession.GetActorId(), rootSessionID, "req-relay-read-root", "idem-relay-read-root")

	userCookie, userSession := memberLogin(t, sys.bffClient, "user202@example.test")
	userSessionID := owneridentity.SessionReference(userCookie)
	userCall := &api.CallContext{ActorId: userSession.GetActorId(), SessionId: proto.String(userSessionID)}

	// 1. Default fail-closed for non-admin; default enabled for bootstrap admin; Bearer and session both work.
	readUser, err := sys.bffClient.ReadCallerLinkRelayEntitlement(ctx, &api.ReadCallerLinkRelayEntitlementRpcRequest{
		BearerToken: "isolated-gateway-user202@example.test",
	})
	if err != nil {
		t.Fatalf("ReadCallerLinkRelayEntitlement(bearer 202): %v", err)
	}
	if readUser.GetActorId() != "202" || readUser.GetLinkRelayEnabled() || readUser.GetIsPlatformAdmin() {
		t.Fatalf("expected fail-closed non-admin readback, got %+v", readUser)
	}

	readRoot, err := sys.bffClient.ReadCallerLinkRelayEntitlement(ctx, &api.ReadCallerLinkRelayEntitlementRpcRequest{
		Context: rootCall,
	})
	if err != nil {
		t.Fatalf("ReadCallerLinkRelayEntitlement(root session): %v", err)
	}
	if !readRoot.GetLinkRelayEnabled() || !readRoot.GetIsBootstrapAdmin() || !readRoot.GetIsPlatformAdmin() {
		t.Fatalf("expected bootstrap root enabled=true, got %+v", readRoot)
	}

	// Internal CloudIdentityAuthorization RPC fails closed on disabled user and permits bootstrap root.
	readDisabled, err := sys.authzClient.ReadLinkRelayEntitlement(ctx, &api.ReadLinkRelayEntitlementRequest{ActorId: "203"})
	if err != nil {
		t.Fatalf("ReadLinkRelayEntitlement(disabled 203): %v", err)
	}
	if readDisabled.GetLinkRelayEnabled() || readDisabled.GetReason() != "gateway_account_inactive" {
		t.Fatalf("expected disabled user refused, got %+v", readDisabled)
	}

	// 2. Non-admin cannot mutate or seed Link Relay entitlements.
	if _, err := sys.bffClient.SetLinkRelayEntitlement(ctx, &api.SetLinkRelayEntitlementRpcRequest{
		Context:       platformCall(userSession.GetActorId(), userSessionID, "req-relay-denied-1", "idem-relay-denied-1"),
		TargetActorId: "202",
		Body:          &api.SetLinkRelayEntitlementRequest{ConfirmationAccountId: "202", Enabled: true, Reason: "try_grant"},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("non-admin SetLinkRelayEntitlement got %v want PermissionDenied", err)
	}
	if _, err := sys.bffClient.SeedActiveGatewayLinkRelayEntitlements(ctx, &api.SeedActiveGatewayLinkRelayEntitlementsRpcRequest{
		Context: platformCall(userSession.GetActorId(), userSessionID, "req-seed-denied-1", "idem-seed-denied-1"),
		Body:    &api.SeedActiveGatewayLinkRelayEntitlementsRequest{Reason: "try_seed"},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("non-admin Seed got %v want PermissionDenied", err)
	}

	// 3. Grant and explicitly revoke 202 before backfill, then verify backfill preserves explicit revocation!
	grantResp, err := sys.bffClient.SetLinkRelayEntitlement(ctx, &api.SetLinkRelayEntitlementRpcRequest{
		Context:       platformCall(rootSession.GetActorId(), rootSessionID, "req-relay-grant-202", "idem-relay-grant-202"),
		TargetActorId: "202",
		Body:          &api.SetLinkRelayEntitlementRequest{ConfirmationAccountId: "202", Enabled: true, Reason: "allow_relay"},
	})
	if err != nil || !grantResp.GetLinkRelayEnabled() {
		t.Fatalf("grant 202 failed: resp=%+v err=%v", grantResp, err)
	}
	if _, err := sys.bffClient.SetLinkRelayEntitlement(ctx, &api.SetLinkRelayEntitlementRpcRequest{
		Context:       platformCall(rootSession.GetActorId(), rootSessionID, "req-relay-revoke-202", "idem-relay-revoke-202"),
		TargetActorId: "202",
		Body:          &api.SetLinkRelayEntitlementRequest{ConfirmationAccountId: "202", Enabled: false, Reason: "explicit_revoke"},
	}); err != nil {
		t.Fatalf("revoke 202 failed: %v", err)
	}

	// Bootstrap admin revocation is refused; forged caller actor & disabled bearer are refused.
	if _, err := sys.bffClient.SetLinkRelayEntitlement(ctx, &api.SetLinkRelayEntitlementRpcRequest{
		Context:       platformCall(rootSession.GetActorId(), rootSessionID, "req-relay-revoke-root", "idem-relay-revoke-root"),
		TargetActorId: "103",
		Body:          &api.SetLinkRelayEntitlementRequest{ConfirmationAccountId: "103", Enabled: false, Reason: "revoke_root"},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("bootstrap revoke got %v want PermissionDenied", err)
	}
	if _, err := sys.bffClient.ReadCallerLinkRelayEntitlement(ctx, &api.ReadCallerLinkRelayEntitlementRpcRequest{
		Context: &api.CallContext{ActorId: "103", SessionId: userCall.SessionId},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("forged ActorId got %v want PermissionDenied", err)
	}
	if _, err := sys.bffClient.ReadCallerLinkRelayEntitlement(ctx, &api.ReadCallerLinkRelayEntitlementRpcRequest{
		BearerToken: "isolated-gateway-disabled203@example.test",
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("disabled bearer got %v want Unauthenticated", err)
	}

	// 4. Seed active Gateway users (53 active out of 54 total: 103, 201, 202, 300..349).
	// User 202 already has an explicit false row -> ON CONFLICT DO NOTHING leaves it false!
	seedResp, err := sys.bffClient.SeedActiveGatewayLinkRelayEntitlements(ctx, &api.SeedActiveGatewayLinkRelayEntitlementsRpcRequest{
		Context: platformCall(rootSession.GetActorId(), rootSessionID, "req-seed-active-1", "idem-seed-active-1"),
		Body:    &api.SeedActiveGatewayLinkRelayEntitlementsRequest{Reason: "initial_backfill"},
	})
	if err != nil {
		t.Fatalf("SeedActiveGatewayLinkRelayEntitlements: %v", err)
	}
	if seedResp.GetScannedCount() != 53 || seedResp.GetSeededCount() != 52 || seedResp.GetExistingCount() != 1 {
		t.Fatalf("unexpected seed counts: %+v", seedResp)
	}

	// Re-running seed with a new idempotency key sees 0 new rows and preserves existing records.
	seedAgain, err := sys.bffClient.SeedActiveGatewayLinkRelayEntitlements(ctx, &api.SeedActiveGatewayLinkRelayEntitlementsRpcRequest{
		Context: platformCall(rootSession.GetActorId(), rootSessionID, "req-seed-active-2", "idem-seed-active-2"),
		Body:    &api.SeedActiveGatewayLinkRelayEntitlementsRequest{Reason: "second_backfill"},
	})
	if err != nil {
		t.Fatalf("second SeedActiveGatewayLinkRelayEntitlements: %v", err)
	}
	if seedAgain.GetScannedCount() != 53 || seedAgain.GetSeededCount() != 0 || seedAgain.GetExistingCount() != 53 {
		t.Fatalf("unexpected second seed counts: %+v", seedAgain)
	}

	// Verify 202 stayed revoked and 201 & 349 became enabled in both readback and ListOperatorAccounts.
	readRevoked, err := sys.bffClient.ReadCallerLinkRelayEntitlement(ctx, &api.ReadCallerLinkRelayEntitlementRpcRequest{Context: userCall})
	if err != nil || readRevoked.GetLinkRelayEnabled() || readRevoked.GetReason() != "explicit_revoke" {
		t.Fatalf("expected 202 explicit revocation preserved, got resp=%+v err=%v", readRevoked, err)
	}
	readSeeded, err := sys.authzClient.ReadLinkRelayEntitlement(ctx, &api.ReadLinkRelayEntitlementRequest{ActorId: "349"})
	if err != nil || !readSeeded.GetLinkRelayEnabled() || readSeeded.GetReason() != "initial_backfill" {
		t.Fatalf("expected 349 seeded enabled=true, got resp=%+v err=%v", readSeeded, err)
	}

	page, err := sys.bffClient.ListOperatorAccounts(ctx, &api.ListOperatorAccountsRpcRequest{
		Context:  platformCall(rootSession.GetActorId(), rootSessionID, "req-list-relay-1", "idem-list-relay-1"),
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListOperatorAccounts: %v", err)
	}
	found201, found202 := false, false
	for _, item := range page.GetItems() {
		if item.GetActorId() == "201" {
			found201 = item.GetLinkRelayEnabled()
		}
		if item.GetActorId() == "202" {
			found202 = !item.GetLinkRelayEnabled()
		}
	}
	if !found201 || !found202 {
		t.Fatalf("expected ListOperatorAccounts linkRelayEnabled 201=true and 202=false, got items=%+v", page.GetItems())
	}
}
