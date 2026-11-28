package identity_test

import (
	"database/sql"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// tenantReenableSystem provisions the owner exactly as the purchase
// eligibility system does: one isolated PostgreSQL database, a real Gateway
// fixture, a real platform administrator and real gRPC clients.
func tenantReenableSystem(t *testing.T) purchaseEligibilitySystem {
	t.Helper()
	return newPurchaseEligibilitySystem(t, map[int64]bool{})
}

// reenablePlatformCall is the CallContext the BFF produces for a platform
// administrator command: platform scope, the resolved session and the
// operator's own idempotency key.
func reenablePlatformCall(actor, session, requestID, idempotencyKey string) *api.CallContext {
	return &api.CallContext{
		ActorId: actor, SessionId: proto.String(session),
		Scope:          &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
		RequestId:      requestID,
		IdempotencyKey: idempotencyKey,
	}
}

// reenableTenantScopeCall is the CallContext the BFF produces for a
// tenant-scoped read of one Tenant.
func reenableTenantScopeCall(cookie, actor, tenantID string) *api.CallContext {
	ref := owneridentity.SessionReference(cookie)
	return &api.CallContext{
		ActorId: actor, SessionId: &ref,
		Scope:     &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenantID}}},
		RequestId: "request-" + tenantID,
	}
}

// TestTenantReenablePostgresLifecycle proves the owner's own fences serve a
// suspended Tenant's members again once the platform administrator re-enables
// it, and that the command is idempotent, auditable and refuses a Tenant that
// is not suspended.
func TestTenantReenablePostgresLifecycle(t *testing.T) {
	sys := tenantReenableSystem(t)
	ctx := t.Context()
	if _, err := sys.db.ExecContext(ctx, `
		INSERT INTO tenant.tenants(id,name) VALUES('tenant-reenable','Reenabled customer');
		INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('member-reenable','tenant-reenable','201','owner')`); err != nil {
		t.Fatal(err)
	}
	adminCookie, _ := memberLogin(t, sys.bffClient, "admin@example.test")
	customerCookie, _ := memberLogin(t, sys.bffClient, "customer@example.test")
	otherCookie, _ := memberLogin(t, sys.bffClient, "customer2@example.test")
	adminSession := owneridentity.SessionReference(adminCookie)
	customerSession := owneridentity.SessionReference(customerCookie)
	otherSession := owneridentity.SessionReference(otherCookie)

	// The account is disabled the way the owner's own suspension leaves it: the
	// Tenant is suspended and the members' live sessions are revoked.
	if _, err := sys.db.ExecContext(ctx, `UPDATE tenant.tenants SET status='suspended', suspended_at=now() WHERE id='tenant-reenable'`); err != nil {
		t.Fatal(err)
	}
	if _, err := sys.db.ExecContext(ctx, `UPDATE tenant.sessions SET revoked_at=now() WHERE actor_id='201' AND revoked_at IS NULL`); err != nil {
		t.Fatal(err)
	}

	// While the Tenant is suspended the owner's own fences refuse its member.
	if _, err := sys.bffClient.GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(customerSession)}}); err == nil {
		t.Fatal("the suspended member's session still resolved")
	}
	if _, err := sys.bffClient.GetTenant(ctx, &api.GetTenantRpcRequest{Context: reenableTenantScopeCall(customerCookie, "201", "tenant-reenable")}); err == nil {
		t.Fatal("the suspended member still read its Tenant")
	}

	// A customer is not a platform administrator. The caller keeps a live
	// session of its own Tenant, so the refusal is the authorization decision
	// and not a stale session.
	if _, err := sys.bffClient.ReenableTenant(ctx, &api.ReenableTenantRpcRequest{
		Context:  reenablePlatformCall("202", otherSession, "req-customer-reenable", "idem-customer-reenable"),
		TenantId: "tenant-reenable",
		Body:     &api.ReenableTenantRequest{Reason: "self_reenable"},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a customer re-enabled its own Tenant: %v", err)
	}

	// An unknown Tenant is not re-enabled.
	if _, err := sys.bffClient.ReenableTenant(ctx, &api.ReenableTenantRpcRequest{
		Context:  reenablePlatformCall("103", adminSession, "req-unknown-tenant", "idem-unknown-tenant"),
		TenantId: "tenant-absent",
		Body:     &api.ReenableTenantRequest{Reason: "operator_requested"},
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("an unknown Tenant was re-enabled: %v", err)
	}

	// A command without a reason is refused before anything is written.
	if _, err := sys.bffClient.ReenableTenant(ctx, &api.ReenableTenantRpcRequest{
		Context:  reenablePlatformCall("103", adminSession, "req-no-reason", "idem-no-reason"),
		TenantId: "tenant-reenable",
		Body:     &api.ReenableTenantRequest{Reason: "   "},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a reasonless re-enable was accepted: %v", err)
	}

	// A command without an idempotency key is refused.
	if _, err := sys.bffClient.ReenableTenant(ctx, &api.ReenableTenantRpcRequest{
		Context:  reenablePlatformCall("103", adminSession, "req-no-key", ""),
		TenantId: "tenant-reenable",
		Body:     &api.ReenableTenantRequest{Reason: "operator_requested"},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a re-enable without an idempotency key was accepted: %v", err)
	}

	// The platform administrator re-enables the Tenant.
	request := &api.ReenableTenantRpcRequest{
		Context:  reenablePlatformCall("103", adminSession, "req-reenable-1", "idem-reenable-1"),
		TenantId: "tenant-reenable",
		Body:     &api.ReenableTenantRequest{Reason: "operator_requested"},
	}
	operation, err := sys.bffClient.ReenableTenant(ctx, request)
	if err != nil {
		t.Fatalf("ReenableTenant: %v", err)
	}
	if operation.GetOperationId() == "" || operation.GetResourceId() != "tenant-reenable" ||
		operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_REENABLE_TENANT ||
		operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED ||
		operation.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT {
		t.Fatalf("unexpected re-enable operation: %+v", operation)
	}

	// The Tenant is active again, its suspension is cleared and its permission
	// version advanced, so the owner's own fences serve it afterwards.
	var tenantStatus string
	var suspendedAt sql.NullTime
	var permissionVersion int64
	if err = sys.db.QueryRowContext(ctx, `SELECT status,suspended_at,permission_version FROM tenant.tenants WHERE id='tenant-reenable'`).Scan(&tenantStatus, &suspendedAt, &permissionVersion); err != nil {
		t.Fatal(err)
	}
	if tenantStatus != "active" || suspendedAt.Valid || permissionVersion == 0 {
		t.Fatalf("tenant status=%q suspended_at=%v permission_version=%d", tenantStatus, suspendedAt.Valid, permissionVersion)
	}

	// The revoked session stays revoked: the customer signs in again.
	var revoked sql.NullTime
	if err = sys.db.QueryRowContext(ctx, `SELECT revoked_at FROM tenant.sessions WHERE actor_id='201' AND revoked_at IS NOT NULL`).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if !revoked.Valid {
		t.Fatal("the re-enable resurrected a revoked session")
	}
	if _, err = sys.bffClient.GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(customerSession)}}); err == nil {
		t.Fatal("the revoked session still resolved after the re-enable")
	}
	secondCookie, _ := memberLogin(t, sys.bffClient, "customer@example.test")
	if _, err = sys.bffClient.GetTenant(ctx, &api.GetTenantRpcRequest{Context: reenableTenantScopeCall(secondCookie, "201", "tenant-reenable")}); err != nil {
		t.Fatalf("the re-enabled member could not read its own Tenant: %v", err)
	}

	// The operation and its audit evidence committed with the re-enable.
	var operationCount, auditCount int
	if err = sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.operations WHERE kind='reenable_tenant' AND resource_id='tenant-reenable' AND status='succeeded'`).Scan(&operationCount); err != nil {
		t.Fatal(err)
	}
	if err = sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.audit_events WHERE action='tenant.reenable' AND resource_id='tenant-reenable' AND outcome='succeeded'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if operationCount != 1 || auditCount != 1 {
		t.Fatalf("operation rows=%d audit rows=%d", operationCount, auditCount)
	}

	// The same key replays the original Operation instead of writing twice.
	replay, err := sys.bffClient.ReenableTenant(ctx, request)
	if err != nil || replay.GetOperationId() != operation.GetOperationId() {
		t.Fatalf("idempotent replay mismatch: %+v err=%v", replay, err)
	}
	var replayedOperations int
	if err = sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.operations WHERE kind='reenable_tenant' AND resource_id='tenant-reenable'`).Scan(&replayedOperations); err != nil {
		t.Fatal(err)
	}
	if replayedOperations != 1 {
		t.Fatalf("the replay wrote a second operation: %d", replayedOperations)
	}

	// The same key with a different command is a conflict, not a second write.
	conflict := proto.Clone(request).(*api.ReenableTenantRpcRequest)
	conflict.Body.Reason = "another_reason"
	if _, err = sys.bffClient.ReenableTenant(ctx, conflict); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected an idempotency conflict, got %v", err)
	}

	// A Tenant that is not suspended is refused, and the refusal keeps the
	// Tenant exactly as it is.
	second, err := sys.bffClient.ReenableTenant(ctx, &api.ReenableTenantRpcRequest{
		Context:  reenablePlatformCall("103", adminSession, "req-reenable-2", "idem-reenable-2"),
		TenantId: "tenant-reenable",
		Body:     &api.ReenableTenantRequest{Reason: "operator_requested"},
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a second re-enable was accepted: %+v err=%v", second, err)
	}
	code, ok := owneridentity.ErrorCode(err)
	if !ok || code != api.ErrorCodeEnum_ERROR_CODE_ENUM_TENANT_NOT_SUSPENDED {
		t.Fatalf("the refusal carries no canonical code: %v", err)
	}
	var refusalAudits int
	if err = sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.audit_events WHERE action='tenant.reenable' AND resource_id='tenant-reenable' AND outcome='rejected'`).Scan(&refusalAudits); err != nil {
		t.Fatal(err)
	}
	if refusalAudits != 1 {
		t.Fatalf("the refusal left no audit evidence: %d", refusalAudits)
	}
	if err = sys.db.QueryRowContext(ctx, `SELECT status FROM tenant.tenants WHERE id='tenant-reenable'`).Scan(&tenantStatus); err != nil {
		t.Fatal(err)
	}
	if tenantStatus != "active" {
		t.Fatalf("the refusal changed the Tenant to %q", tenantStatus)
	}
}
