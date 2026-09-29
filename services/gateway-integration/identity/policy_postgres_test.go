package identity_test

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// authorizeAt calls the real CloudIdentityAuthorizeAction as the Console BFF, the
// only interactive originator, so the test exercises the same live session, role
// and scope checks a browser command does.
func authorizeAt(t *testing.T, system memberSystem, session *api.Session, cookie string, audience api.OwnerEnum, action api.AuthorizationActionEnum, kind api.AuthorizationResourceKind, resourceID, tenantID, requestID string) error {
	t.Helper()
	ref := owneridentity.SessionReference(cookie)
	scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
	if tenantID != "" {
		scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenantID}}}
	}
	resource := &api.AuthorizationResource{Kind: kind}
	if resourceID != "" {
		resource.Id = proto.String(resourceID)
	}
	_, err := system.auth.AuthorizeAction(t.Context(), &api.AuthorizationRequest{Scope: scope, ActorId: session.ActorId, SessionId: proto.String(ref), AudienceOwner: audience, Action: action, Resource: resource, RequestId: requestID})
	return err
}

// TestPolicyPlatformPermissionAndTenantIsolationPostgres proves the generated
// resource catalog and Serve rows behave as the SSOT requires: the platform
// administrator surface is reached only with a platform scope and the deployment
// administrator subject, the customer reads are reached only with the caller's own
// Tenant, and no path authorizes a tenant role for a platform action or widens a
// decision to another audience.
func TestPolicyPlatformPermissionAndTenantIsolationPostgres(t *testing.T) {
	system := newMemberSystem(t)
	ctx := t.Context()
	if _, e := system.db.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name) VALUES('tenant-catalog','Catalog');INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('catalog-member','tenant-catalog','701','member'),('catalog-admin','tenant-catalog','702','admin'),('catalog-owner','tenant-catalog','703','owner')`); e != nil {
		t.Fatal(e)
	}
	memberCookie, member := memberLogin(t, system.client, "member@example.test")
	ownerCookie, owner := memberLogin(t, system.client, "owner@example.test")
	adminCookie, _ := memberLogin(t, system.client, "admin@example.test")
	platformCookie, platform := memberLogin(t, system.client, "platform@example.test")
	if member.GetTenantId() != "tenant-catalog" || owner.GetTenantId() != "tenant-catalog" || platform.GetTenantId() != "" {
		t.Fatalf("unexpected session binding: %q %q %q", member.GetTenantId(), owner.GetTenantId(), platform.GetTenantId())
	}

	const catalogAdmin = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATECOMPUTEPLAN
	const catalogRead = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTCOMPUTEPLANS
	const serveRead = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS
	const catalogKind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG
	const workspaceKind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE
	const catalogOwner = api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG
	const serveOwner = api.OwnerEnum_OWNER_ENUM_SERVE
	const workspaceOwner = api.OwnerEnum_OWNER_ENUM_WORKSPACE

	// The deployment platform administrator reaches the catalog administrator
	// surface with a platform scope, and only because it is a configured admin
	// subject rather than a Tenant role holder.
	if e := authorizeAt(t, system, platform, platformCookie, catalogOwner, catalogAdmin, catalogKind, "", "", "platform-admin-catalog"); e != nil {
		t.Fatalf("platform administrator was refused the catalog administrator action: %v", e)
	}
	// A tenant owner must not inherit the platform permission from its Tenant role.
	if e := authorizeAt(t, system, owner, ownerCookie, catalogOwner, catalogAdmin, catalogKind, "", "tenant-catalog", "tenant-owner-catalog"); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("a tenant role reached the platform catalog action: %v", e)
	}
	// A platform administrator may target a Tenant other than its own session
	// Tenant, so the same action on an explicit Tenant scope is also allowed; the
	// permission still comes from the administrator subject, not from tenant
	// membership.
	if e := authorizeAt(t, system, platform, platformCookie, catalogOwner, catalogAdmin, catalogKind, "", "tenant-catalog", "platform-admin-target-tenant"); e != nil {
		t.Fatalf("a platform administrator was refused a target Tenant: %v", e)
	}
	// A plain member is refused the platform action.
	if e := authorizeAt(t, system, member, memberCookie, catalogOwner, catalogAdmin, catalogKind, "", "tenant-catalog", "member-catalog-admin"); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("a member reached the platform catalog action: %v", e)
	}

	// The customer catalog read is reached with the caller's own Tenant scope by a
	// member, admin and owner alike.
	for name, cookie := range map[string]string{"member": memberCookie, "admin": adminCookie, "owner": ownerCookie} {
		session := member
		if name == "admin" {
			_, session = memberLogin(t, system.client, "admin@example.test")
		}
		if name == "owner" {
			_, session = memberLogin(t, system.client, "owner@example.test")
		}
		if e := authorizeAt(t, system, session, cookie, catalogOwner, catalogRead, catalogKind, "", "tenant-catalog", "read-"+name); e != nil {
			t.Fatalf("%s was refused the catalog read: %v", name, e)
		}
	}
	// The same read is refused outside the caller's own Tenant.
	if e := authorizeAt(t, system, member, memberCookie, catalogOwner, catalogRead, catalogKind, "", "tenant-b", "cross-tenant-read"); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("a member read another Tenant's catalog: %v", e)
	}
	// A non-administrator has no platform scope at all, so the read cannot be
	// relocated to the platform scope to escape the Tenant check.
	if e := authorizeAt(t, system, member, memberCookie, catalogOwner, catalogRead, catalogKind, "", "", "member-platform-scope"); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("a member read the catalog through a platform scope: %v", e)
	}

	// The Serve read is reached by a member on its own Tenant with the Serve
	// audience.
	if e := authorizeAt(t, system, member, memberCookie, serveOwner, serveRead, workspaceKind, "ws-1", "tenant-catalog", "serve-read"); e != nil {
		t.Fatalf("a member was refused the Serve read: %v", e)
	}
	// The audience is part of the decision: the same action under another owner's
	// audience is refused, so a decision cannot be relocated between owners.
	if e := authorizeAt(t, system, member, memberCookie, catalogOwner, serveRead, workspaceKind, "ws-1", "tenant-catalog", "serve-action-wrong-audience"); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("a serve action was authorized under the catalog audience: %v", e)
	}
	// An action this policy does not serve is refused rather than defaulted.
	if e := authorizeAt(t, system, member, memberCookie, workspaceOwner, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS, workspaceKind, "ws-1", "tenant-catalog", "unserved-action"); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("an unserved action was authorized: %v", e)
	}
	// A revoked membership loses the read on its next request.
	if _, e := system.db.ExecContext(ctx, `UPDATE tenant.tenant_members SET revoked_at=now() WHERE id='catalog-member'`); e != nil {
		t.Fatal(e)
	}
	if e := authorizeAt(t, system, member, memberCookie, catalogOwner, catalogRead, catalogKind, "", "tenant-catalog", "revoked-read"); e == nil || status.Code(e) != codes.Unauthenticated && status.Code(e) != codes.PermissionDenied {
		t.Fatalf("a revoked member kept the catalog read: %v", e)
	}
}

// TestAcceptInvitationBindsTheOriginalAuthorizationPostgres proves the invitee
// entry reuses the same authorization path: a decision issued for the exact
// invitation is accepted, while a decision issued for a different action cannot be
// replayed to accept an invitation.
func TestAcceptInvitationBindsTheOriginalAuthorizationPostgres(t *testing.T) {
	system := newMemberSystem(t)
	ctx := t.Context()
	ownerCookie, owner := memberLogin(t, system.client, "owner-a@example.test")
	inviteeCookie, invitee := memberLogin(t, system.client, "invitee@example.test")

	invitation, e := system.client.InviteMember(ctx, &api.InviteMemberRpcRequest{Context: memberCall(owner, ownerCookie, "invite-ctx"), Body: &api.InviteMemberRequest{InviteeGatewaySubjectId: "301", Role: api.InviteMemberRequestRoleEnum_INVITE_MEMBER_REQUEST_ROLE_ENUM_MEMBER}})
	if e != nil {
		t.Fatal(e)
	}

	// A real decision issued for this exact invitation is reused by the owner.
	ref := owneridentity.SessionReference(inviteeCookie)
	decision, e := system.auth.AuthorizeAction(ctx, &api.AuthorizationRequest{
		Scope:         &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
		ActorId:       invitee.ActorId,
		SessionId:     proto.String(ref),
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACCEPTINVITATION,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String(invitation.Id)},
		RequestId:     "accept-context",
	})
	if e != nil {
		t.Fatal(e)
	}
	binding := memberCall(invitee, inviteeCookie, "accept-with-context")
	binding.AuthorizationContextId = decision.GetAuthorizationContextId()
	member, e := system.client.AcceptInvitation(ctx, &api.AcceptInvitationRpcRequest{Context: binding, InvitationId: invitation.Id})
	if e != nil {
		t.Fatalf("the exact invitation decision was not reused: %v", e)
	}
	if member.ActorId != "301" {
		t.Fatalf("unexpected accepted member %v", member)
	}

	// A decision issued for another action cannot accept an invitation.
	// A subject that still holds no active membership, so the second invitation is
	// itself legal and only the reused authorization context is under test.
	secondCookie, secondSubject := memberLogin(t, system.client, "member@example.test")
	if secondSubject.GetTenantId() != "" {
		t.Fatal("the second invitee already belongs to a Tenant")
	}
	second, e := system.client.InviteMember(ctx, &api.InviteMemberRpcRequest{Context: memberCall(owner, ownerCookie, "invite-ctx-2"), Body: &api.InviteMemberRequest{InviteeGatewaySubjectId: "701", Role: api.InviteMemberRequestRoleEnum_INVITE_MEMBER_REQUEST_ROLE_ENUM_MEMBER}})
	if e != nil {
		t.Fatal(e)
	}
	otherDecision, e := system.auth.AuthorizeAction(ctx, &api.AuthorizationRequest{
		Scope:         &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-a"}}},
		ActorId:       owner.ActorId,
		SessionId:     proto.String(owneridentity.SessionReference(ownerCookie)),
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTMEMBERS,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String("tenant-a")},
		RequestId:     "list-context",
	})
	if e != nil {
		t.Fatal(e)
	}
	widened := memberCall(secondSubject, secondCookie, "reused-list-context")
	widened.AuthorizationContextId = otherDecision.GetAuthorizationContextId()
	if _, e = system.client.AcceptInvitation(ctx, &api.AcceptInvitationRpcRequest{Context: widened, InvitationId: second.Id}); e == nil {
		t.Fatal("a decision for another action accepted an invitation")
	}
	// An unknown context is refused rather than treated as no context at all.
	forged := memberCall(secondSubject, secondCookie, "forged-context")
	forged.AuthorizationContextId = "auth_does-not-exist"
	if _, e = system.client.AcceptInvitation(ctx, &api.AcceptInvitationRpcRequest{Context: forged, InvitationId: second.Id}); e == nil {
		t.Fatal("a forged authorization context accepted an invitation")
	}
}

// TestInvitationExpiryBoundaryPostgres proves the two sides of the configured
// window: an invitation is acceptable strictly before it expires and is refused
// once the current time reaches the boundary.
func TestInvitationExpiryBoundaryPostgres(t *testing.T) {
	system := newMemberSystem(t)
	ctx := t.Context()
	ownerCookie, owner := memberLogin(t, system.client, "owner-a@example.test")
	inviteeCookie, invitee := memberLogin(t, system.client, "invitee@example.test")

	invitation, e := system.client.InviteMember(ctx, &api.InviteMemberRpcRequest{Context: memberCall(owner, ownerCookie, "invite-tty"), Body: &api.InviteMemberRequest{InviteeGatewaySubjectId: "301", Role: api.InviteMemberRequestRoleEnum_INVITE_MEMBER_REQUEST_ROLE_ENUM_MEMBER}})
	if e != nil {
		t.Fatal(e)
	}
	// The window the deployment configured is what the invitation records.
	var storedExpiry time.Time
	if e = system.db.QueryRowContext(ctx, `SELECT expires_at FROM tenant.invitations WHERE id=$1`, invitation.Id).Scan(&storedExpiry); e != nil {
		t.Fatal(e)
	}
	if !invitation.ExpiresAt.AsTime().Equal(storedExpiry) {
		t.Fatalf("invitation expiry %v does not match the persisted %v", invitation.ExpiresAt.AsTime(), storedExpiry)
	}
	if delta := time.Until(storedExpiry); delta <= 0 || delta > memberInvitationTTL {
		t.Fatalf("invitation expiry is outside the configured window: %v", delta)
	}

	// Move the boundary into the past: the same acceptance is now refused and the
	// invitation reads as expired.
	if _, e = system.db.ExecContext(ctx, `UPDATE tenant.invitations SET expires_at=now()-interval '1 second' WHERE id=$1`, invitation.Id); e != nil {
		t.Fatal(e)
	}
	if _, e = system.client.AcceptInvitation(ctx, &api.AcceptInvitationRpcRequest{Context: memberCall(invitee, inviteeCookie, "accept-expired"), InvitationId: invitation.Id}); memberCode(t, e) != api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID {
		t.Fatalf("an expired invitation was accepted: %v", e)
	}
	page, e := system.client.ListInvitations(ctx, &api.ListInvitationsRpcRequest{Context: memberCall(owner, ownerCookie, "list-expired")})
	if e != nil {
		t.Fatal(e)
	}
	for _, item := range page.Items {
		if item.Id == invitation.Id && item.Status != api.InvitationStatusEnum_INVITATION_STATUS_ENUM_EXPIRED {
			t.Fatalf("an expired invitation reads as %v", item.Status)
		}
	}
}

// TestOperationReadIsRouteResolvedAcrossOwnersPostgres proves the operation read
// the Console performs at /api/v2/operations/{owner}/{operationId}. The contract
// names no single Operation writer, so the decision is a live session's member
// read against the explicit owner route rather than one owner's role row.
// Without it every owner except Workspace would be refused its own operation
// read, and a row would have pinned the audience to an arbitrary owner.
func TestOperationReadIsRouteResolvedAcrossOwnersPostgres(t *testing.T) {
	system := newMemberSystem(t)
	memberCookie, member := memberLogin(t, system.client, "owner-a@example.test")
	platformCookie, platform := memberLogin(t, system.client, "platform@example.test")
	if member.GetTenantId() != "tenant-a" || platform.GetTenantId() != "" {
		t.Fatalf("unexpected session binding: %q %q", member.GetTenantId(), platform.GetTenantId())
	}
	const read = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION
	const operationKind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION

	// The same tenant session reads an Operation from each owner route it names;
	// the audience is the route, not a fixed Operation writer.
	for _, audience := range []api.OwnerEnum{
		api.OwnerEnum_OWNER_ENUM_FABRIC,
		api.OwnerEnum_OWNER_ENUM_SERVE,
		api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG,
		api.OwnerEnum_OWNER_ENUM_LEDGER,
		api.OwnerEnum_OWNER_ENUM_WORKSPACE,
	} {
		if e := authorizeAt(t, system, member, memberCookie, audience, read, operationKind, "operation-a", "tenant-a", "member-operation-"+audience.String()); e != nil {
			t.Fatalf("a tenant member was refused the %s operation route: %v", audience, e)
		}
	}
	// The read is authorized only for the Operation resource an owner route names,
	// so the same action cannot be reused against another resource kind.
	if e := authorizeAt(t, system, member, memberCookie, api.OwnerEnum_OWNER_ENUM_FABRIC, read, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "workspace-a", "tenant-a", "member-operation-wrong-kind"); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("an operation read reached another resource kind: %v", e)
	}
	// The audience must be a Cloud owner: the route itself is the authority, so an
	// unspecified owner has no Operation store to answer and cannot be allowed.
	if e := authorizeAt(t, system, member, memberCookie, api.OwnerEnum_OWNER_ENUM_UNSPECIFIED, read, operationKind, "operation-a", "tenant-a", "member-operation-no-owner"); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("an operation read without an owner route was allowed: %v", e)
	}
	// The tenant fence still applies: a tenant-a session cannot read a tenant-b
	// operation by naming it.
	if e := authorizeAt(t, system, member, memberCookie, api.OwnerEnum_OWNER_ENUM_FABRIC, read, operationKind, "operation-b", "tenant-b", "member-operation-other-tenant"); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("an operation read crossed tenants: %v", e)
	}
	// A platform-scoped read is the platform administrator's; a session with no
	// Tenant and no administrator subject is refused rather than treated as one.
	if e := authorizeAt(t, system, platform, platformCookie, api.OwnerEnum_OWNER_ENUM_FABRIC, read, operationKind, "operation-platform", "", "platform-operation-read"); e != nil {
		t.Fatalf("the platform administrator was refused an operation route: %v", e)
	}
	plainCookie, plain := memberLogin(t, system.client, "member@example.test")
	if e := authorizeAt(t, system, plain, plainCookie, api.OwnerEnum_OWNER_ENUM_FABRIC, read, operationKind, "operation-platform", "", "plain-operation-platform"); status.Code(e) != codes.PermissionDenied {
		t.Fatalf("a non-administrator session read a platform operation: %v", e)
	}
}
