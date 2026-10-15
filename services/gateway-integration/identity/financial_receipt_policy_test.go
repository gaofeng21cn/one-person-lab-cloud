package identity_test

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"testing"
)

func TestFinancialReceiptPolicyUsesCurrentTeamRoleAndSession(t *testing.T) {
	_, tenant, service := gatewayProductSystem(t)
	raw, _ := login(t, tenant)
	request := &api.AuthorizationRequest{RequestId: "receipt-read", ActorId: "101", SessionId: proto.String(owneridentity.SessionReference(raw)), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}}, AudienceOwner: api.OwnerEnum_OWNER_ENUM_LEDGER, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEFINANCIALRECEIPT, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String("deleted-workspace")}}
	for _, role := range []string{"owner", "admin", "member"} {
		if _, err := service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role=$1 WHERE actor_id='101'`, role); err != nil {
			t.Fatal(err)
		}
		_, err := service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), request)
		if (role == "member" && status.Code(err) != codes.PermissionDenied) || (role != "member" && err != nil) {
			t.Fatalf("role %s: %v", role, err)
		}
	}
	if _, err := service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role='owner' WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	request.Scope.GetTenant().TenantId = "tenant-other"
	if _, err := service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("wrong Tenant: %v", err)
	}
	request.Scope.GetTenant().TenantId = "tenant-test"
	if _, err := service.DB.ExecContext(t.Context(), `UPDATE tenant.sessions SET expires_at=now()-interval '1 second'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AuthorizeAction(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), request); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expired session: %v", err)
	}
}
