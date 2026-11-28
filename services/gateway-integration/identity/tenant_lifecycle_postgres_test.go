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

// tenantSuspensionSystem provisions the owner exactly as the purchase
// eligibility system does: one isolated PostgreSQL database, a real Gateway
// fixture, a real platform administrator and real gRPC clients.
func tenantSuspensionSystem(t *testing.T) purchaseEligibilitySystem {
	t.Helper()
	return newPurchaseEligibilitySystem(t, map[int64]bool{})
}

// platformCall is the CallContext the BFF produces for a platform
// administrator command: platform scope, the resolved session and the
// operator's own idempotency key.
func platformCall(actor, session, requestID, idempotencyKey string) *api.CallContext {
	return &api.CallContext{
		ActorId: actor, SessionId: proto.String(session),
		Scope:          &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
		RequestId:      requestID,
		IdempotencyKey: idempotencyKey,
	}
}

// TestTenantSuspensionPostgresLifecycle proves the owner's own fences make a
// suspended Tenant unusable for its members, and that the command is
// idempotent, auditable and refuses a second suspension.
func TestTenantSuspensionPostgresLifecycle(t *testing.T) {
	sys := tenantSuspensionSystem(t)
	ctx := t.Context()
	if _, err := sys.db.ExecContext(ctx, `
		INSERT INTO tenant.tenants(id,name) VALUES('tenant-suspension','Suspended customer');
		INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('member-suspension','tenant-suspension','201','owner')`); err != nil {
		t.Fatal(err)
	}
	adminCookie, _ := memberLogin(t, sys.bffClient, "admin@example.test")
	customerCookie, _ := memberLogin(t, sys.bffClient, "customer@example.test")
	adminSession := owneridentity.SessionReference(adminCookie)
	customerSession := owneridentity.SessionReference(customerCookie)

	// Before the suspension the customer reads its own Tenant.
	if _, err := sys.bffClient.GetTenant(ctx, &api.GetTenantRpcRequest{Context: tenantScopeCall(customerCookie, "201", "tenant-suspension")}); err != nil {
		t.Fatalf("the customer could not read its own Tenant before the suspension: %v", err)
	}

	// A customer is not a platform administrator.
	if _, err := sys.bffClient.SuspendTenant(ctx, &api.SuspendTenantRpcRequest{
		Context:  platformCall("201", customerSession, "req-customer-suspend", "idem-customer-suspend"),
		TenantId: "tenant-suspension",
		Body:     &api.TenantActionRequest{Reason: "self_suspend"},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a customer suspended its own Tenant: %v", err)
	}

	// An unknown Tenant is not suspended.
	if _, err := sys.bffClient.SuspendTenant(ctx, &api.SuspendTenantRpcRequest{
		Context:  platformCall("103", adminSession, "req-unknown-tenant", "idem-unknown-tenant"),
		TenantId: "tenant-absent",
		Body:     &api.TenantActionRequest{Reason: "operator_requested"},
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("an unknown Tenant was suspended: %v", err)
	}

	// A command without a reason is refused before anything is written.
	if _, err := sys.bffClient.SuspendTenant(ctx, &api.SuspendTenantRpcRequest{
		Context:  platformCall("103", adminSession, "req-no-reason", "idem-no-reason"),
		TenantId: "tenant-suspension",
		Body:     &api.TenantActionRequest{Reason: "   "},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a reasonless suspension was accepted: %v", err)
	}

	// A command without an idempotency key is refused.
	if _, err := sys.bffClient.SuspendTenant(ctx, &api.SuspendTenantRpcRequest{
		Context:  platformCall("103", adminSession, "req-no-key", ""),
		TenantId: "tenant-suspension",
		Body:     &api.TenantActionRequest{Reason: "operator_requested"},
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("a suspension without an idempotency key was accepted: %v", err)
	}

	// The platform administrator suspends the Tenant.
	request := &api.SuspendTenantRpcRequest{
		Context:  platformCall("103", adminSession, "req-suspend-1", "idem-suspend-1"),
		TenantId: "tenant-suspension",
		Body:     &api.TenantActionRequest{Reason: "operator_requested"},
	}
	operation, err := sys.bffClient.SuspendTenant(ctx, request)
	if err != nil {
		t.Fatalf("SuspendTenant: %v", err)
	}
	if operation.GetOperationId() == "" || operation.GetResourceId() != "tenant-suspension" ||
		operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_SUSPEND_TENANT ||
		operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED ||
		operation.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT {
		t.Fatalf("unexpected suspension operation: %+v", operation)
	}

	// The Tenant is suspended, and its members' live sessions are revoked.
	var tenantStatus string
	var suspendedAt sql.NullTime
	if err = sys.db.QueryRowContext(ctx, `SELECT status,suspended_at FROM tenant.tenants WHERE id='tenant-suspension'`).Scan(&tenantStatus, &suspendedAt); err != nil {
		t.Fatal(err)
	}
	if tenantStatus != "suspended" || !suspendedAt.Valid {
		t.Fatalf("tenant status=%q suspended_at=%v", tenantStatus, suspendedAt.Valid)
	}
	var revoked sql.NullTime
	if err = sys.db.QueryRowContext(ctx, `SELECT revoked_at FROM tenant.sessions WHERE actor_id='201'`).Scan(&revoked); err != nil {
		t.Fatal(err)
	}
	if !revoked.Valid {
		t.Fatal("the suspended Tenant's member session is still live")
	}

	// The operation and its audit evidence committed with the suspension.
	var operationCount, auditCount int
	if err = sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.operations WHERE kind='suspend_tenant' AND resource_id='tenant-suspension' AND status='succeeded'`).Scan(&operationCount); err != nil {
		t.Fatal(err)
	}
	if err = sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.audit_events WHERE action='tenant.suspend' AND resource_id='tenant-suspension' AND outcome='succeeded'`).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if operationCount != 1 || auditCount != 1 {
		t.Fatalf("operation rows=%d audit rows=%d", operationCount, auditCount)
	}

	// The owner's own fences refuse the suspended member: the session is gone
	// and the Tenant read is refused.
	if _, err = sys.bffClient.GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(customerSession)}}); err == nil {
		t.Fatal("the suspended member's session still resolved")
	}
	if _, err = sys.bffClient.GetTenant(ctx, &api.GetTenantRpcRequest{Context: tenantScopeCall(customerCookie, "201", "tenant-suspension")}); err == nil {
		t.Fatal("the suspended member still read its Tenant")
	}

	// The same key replays the original Operation instead of writing twice.
	replay, err := sys.bffClient.SuspendTenant(ctx, request)
	if err != nil || replay.GetOperationId() != operation.GetOperationId() {
		t.Fatalf("idempotent replay mismatch: %+v err=%v", replay, err)
	}
	var replayedOperations int
	if err = sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.operations WHERE kind='suspend_tenant' AND resource_id='tenant-suspension'`).Scan(&replayedOperations); err != nil {
		t.Fatal(err)
	}
	if replayedOperations != 1 {
		t.Fatalf("the replay wrote a second operation: %d", replayedOperations)
	}

	// The same key with a different command is a conflict, not a second write.
	conflict := proto.Clone(request).(*api.SuspendTenantRpcRequest)
	conflict.Body.Reason = "another_reason"
	if _, err = sys.bffClient.SuspendTenant(ctx, conflict); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected an idempotency conflict, got %v", err)
	}

	// A second suspension of the same Tenant is refused, and the refusal keeps
	// the Tenant exactly as it is.
	second, err := sys.bffClient.SuspendTenant(ctx, &api.SuspendTenantRpcRequest{
		Context:  platformCall("103", adminSession, "req-suspend-2", "idem-suspend-2"),
		TenantId: "tenant-suspension",
		Body:     &api.TenantActionRequest{Reason: "operator_requested"},
	})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a second suspension was accepted: %+v err=%v", second, err)
	}
	code, ok := owneridentity.ErrorCode(err)
	if !ok || code != api.ErrorCodeEnum_ERROR_CODE_ENUM_TENANT_INACTIVE {
		t.Fatalf("the refusal carries no canonical code: %v", err)
	}
	var refusalAudits int
	if err = sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.audit_events WHERE action='tenant.suspend' AND resource_id='tenant-suspension' AND outcome='rejected'`).Scan(&refusalAudits); err != nil {
		t.Fatal(err)
	}
	if refusalAudits != 1 {
		t.Fatalf("the refusal left no audit evidence: %d", refusalAudits)
	}
}

// tenantScopeCall is the CallContext the BFF produces for a tenant-scoped
// read of one Tenant.
func tenantScopeCall(cookie, actor, tenantID string) *api.CallContext {
	ref := owneridentity.SessionReference(cookie)
	return &api.CallContext{
		ActorId: actor, SessionId: &ref,
		Scope:     &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenantID}}},
		RequestId: "request-" + tenantID,
	}
}
