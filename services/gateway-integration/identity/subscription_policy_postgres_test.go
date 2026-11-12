package identity_test

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
)

func TestSubscriptionPolicyRequiresCurrentTenantAdminPostgres(t *testing.T) {
	system := newMemberSystem(t)
	if _, err := system.db.ExecContext(t.Context(), `INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('subscription-member','tenant-a','701','member'),('subscription-admin','tenant-a','702','admin'),('subscription-owner','tenant-a','703','owner')`); err != nil {
		t.Fatal(err)
	}
	const action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION
	for _, role := range []string{"member", "admin", "owner", "platform", "invitee"} {
		t.Run(role, func(t *testing.T) {
			cookie, session := memberLogin(t, system.client, role+"@example.test")
			if (role == "platform" || role == "invitee") && (session.TenantId != nil || session.Role != nil) {
				t.Fatal("tenantless fixture unexpectedly holds a Tenant role")
			}
			for _, tenantID := range []string{"tenant-a", "tenant-b", ""} {
				err := authorizeAt(t, system, session, cookie, api.OwnerEnum_OWNER_ENUM_WORKSPACE, action,
					api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "workspace-subscription", tenantID, "subscription-read-"+role)
				if tenantID == "tenant-a" && (role == "admin" || role == "owner") {
					if err != nil {
						t.Fatalf("Tenant %s cannot read its subscription: %v", role, err)
					}
				} else if status.Code(err) != codes.PermissionDenied {
					t.Fatalf("%s reached subscription finances under Tenant %q: %v", role, tenantID, err)
				}
			}
			readback, err := system.client.GetSession(t.Context(), &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: &cookie}})
			if err != nil {
				t.Fatal(err)
			}
			for _, actual := range []*api.Session{session, readback} {
				found := false
				for _, permission := range actual.Permissions {
					found = found || permission == action
				}
				if found != (role == "admin" || role == "owner") {
					t.Fatalf("%s login/session exposes the wrong subscription permission: %v", role, found)
				}
			}
			if role == "admin" || role == "owner" {
				// Authorization must re-read the current role, even with the original
				// still-live login session and its previously projected permissions.
				if _, err := system.db.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role='member' WHERE tenant_id='tenant-a' AND actor_id=$1`, session.ActorId); err != nil {
					t.Fatal(err)
				}
				if err := authorizeAt(t, system, session, cookie, api.OwnerEnum_OWNER_ENUM_WORKSPACE, action,
					api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "workspace-subscription", "tenant-a", "subscription-role-revoked"); status.Code(err) != codes.PermissionDenied {
					t.Fatalf("former %s retained subscription read after demotion: %v", role, err)
				}
			}
		})
	}
}
