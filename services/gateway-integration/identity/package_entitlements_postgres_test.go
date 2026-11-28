package identity_test

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func TestPackageEntitlementLifecycleAndExpiryPostgres(t *testing.T) {
	users := []directoryUserFixture{
		{id: 103, email: "root@example.test", status: "active", balance: "25.0", keyCount: 1},
		{id: 202, email: "user202@example.test", status: "active", balance: "5.0", keyCount: 0},
		{id: 203, email: "disabled203@example.test", status: "disabled", balance: "0", keyCount: 0},
	}
	sys := newPlatformAdminSystem(t, []string{"103"}, users)
	ctx := t.Context()

	rootCookie, rootSession := memberLogin(t, sys.bffClient, "root@example.test")
	rootSessionID := owneridentity.SessionReference(rootCookie)

	userCookie, userSession := memberLogin(t, sys.bffClient, "user202@example.test")
	userSessionID := owneridentity.SessionReference(userCookie)
	userCall := &api.CallContext{ActorId: userSession.GetActorId(), SessionId: proto.String(userSessionID)}

	// 1. Default fail-closed: user202 has zero restricted packages even though root admin is logged in.
	initialList, err := sys.bffClient.ListCallerPackageEntitlements(ctx, &api.ListCallerPackageEntitlementsRpcRequest{
		BearerToken: "isolated-gateway-user202@example.test",
	})
	if err != nil {
		t.Fatalf("initial ListCallerPackageEntitlements: %v", err)
	}
	if len(initialList.GetItems()) != 0 {
		t.Fatalf("expected zero initial package entitlements, got %+v", initialList.GetItems())
	}

	// Non-admin or invalid package id or mismatched confirmation id is refused.
	if _, err := sys.bffClient.SetPackageEntitlement(ctx, &api.SetPackageEntitlementRpcRequest{
		Context:       platformCall(userSession.GetActorId(), userSessionID, "req-pkg-denied", "idem-pkg-denied"),
		TargetActorId: "202",
		Body:          &api.SetPackageEntitlementRequest{ConfirmationAccountId: "202", PackageId: "occ", Enabled: true, Reason: "try_grant"},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("non-admin SetPackageEntitlement got %v want PermissionDenied", err)
	}
	if _, err := sys.bffClient.SetPackageEntitlement(ctx, &api.SetPackageEntitlementRpcRequest{
		Context:       platformCall(rootSession.GetActorId(), rootSessionID, "req-pkg-badid", "idem-pkg-badid"),
		TargetActorId: "202",
		Body:          &api.SetPackageEntitlementRequest{ConfirmationAccountId: "202", PackageId: "../occ", Enabled: true, Reason: "bad_id"},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid packageId got %v want InvalidArgument", err)
	}

	// 2. Grant "occ" with future expiry and "occ-beta" already expired in the past.
	futureExpiry := time.Now().UTC().Add(24 * time.Hour)
	pastExpiry := time.Now().UTC().Add(-1 * time.Hour)
	grantOCC, err := sys.bffClient.SetPackageEntitlement(ctx, &api.SetPackageEntitlementRpcRequest{
		Context:       platformCall(rootSession.GetActorId(), rootSessionID, "req-pkg-grant-occ", "idem-pkg-grant-occ"),
		TargetActorId: "202",
		Body: &api.SetPackageEntitlementRequest{
			ConfirmationAccountId: "202",
			PackageId:             "occ",
			Enabled:               true,
			Reason:                "allow_occ",
			ExpiresAt:             timestamppb.New(futureExpiry),
		},
	})
	if err != nil || !grantOCC.GetEnabled() || grantOCC.GetPackageId() != "occ" || grantOCC.GetExpiresAt() == nil {
		t.Fatalf("grant occ failed: resp=%+v err=%v", grantOCC, err)
	}
	if _, err := sys.bffClient.SetPackageEntitlement(ctx, &api.SetPackageEntitlementRpcRequest{
		Context:       platformCall(rootSession.GetActorId(), rootSessionID, "req-pkg-grant-beta", "idem-pkg-grant-beta"),
		TargetActorId: "202",
		Body: &api.SetPackageEntitlementRequest{
			ConfirmationAccountId: "202",
			PackageId:             "occ-beta",
			Enabled:               true,
			Reason:                "expired_pilot",
			ExpiresAt:             timestamppb.New(pastExpiry),
		},
	}); err != nil {
		t.Fatalf("grant expired occ-beta failed: %v", err)
	}

	// Caller readback returns only active, unexpired packages via both Session and Bearer.
	for _, req := range []*api.ListCallerPackageEntitlementsRpcRequest{
		{Context: userCall},
		{BearerToken: "isolated-gateway-user202@example.test"},
	} {
		list, err := sys.bffClient.ListCallerPackageEntitlements(ctx, req)
		if err != nil {
			t.Fatalf("ListCallerPackageEntitlements(%+v): %v", req, err)
		}
		if len(list.GetItems()) != 1 || list.GetItems()[0].GetPackageId() != "occ" {
			t.Fatalf("expected only active unexpired occ, got %+v", list.GetItems())
		}
	}

	// ListOperatorAccounts projects all rows (including expired metadata) for operators.
	page, err := sys.bffClient.ListOperatorAccounts(ctx, &api.ListOperatorAccountsRpcRequest{
		Context:  platformCall(rootSession.GetActorId(), rootSessionID, "req-pkg-list", "idem-pkg-list"),
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		t.Fatalf("ListOperatorAccounts: %v", err)
	}
	found202 := false
	for _, item := range page.GetItems() {
		if item.GetActorId() == "202" {
			found202 = len(item.GetPackageEntitlements()) == 2
		}
	}
	if !found202 {
		t.Fatalf("expected 2 package entitlement projections on actor 202, got %+v", page.GetItems())
	}

	// Revoke occ and confirm caller readback drops to 0.
	revokeOCC, err := sys.bffClient.SetPackageEntitlement(ctx, &api.SetPackageEntitlementRpcRequest{
		Context:       platformCall(rootSession.GetActorId(), rootSessionID, "req-pkg-revoke-occ", "idem-pkg-revoke-occ"),
		TargetActorId: "202",
		Body: &api.SetPackageEntitlementRequest{
			ConfirmationAccountId: "202",
			PackageId:             "occ",
			Enabled:               false,
			Reason:                "revoke_occ",
		},
	})
	if err != nil || revokeOCC.GetEnabled() {
		t.Fatalf("revoke occ failed: resp=%+v err=%v", revokeOCC, err)
	}
	afterRevoke, err := sys.bffClient.ListCallerPackageEntitlements(ctx, &api.ListCallerPackageEntitlementsRpcRequest{
		BearerToken: "isolated-gateway-user202@example.test",
	})
	if err != nil || len(afterRevoke.GetItems()) != 0 {
		t.Fatalf("expected zero active packages after revoke, got resp=%+v err=%v", afterRevoke, err)
	}
}
