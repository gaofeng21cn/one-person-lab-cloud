package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// tenantProbe is a typed CloudIdentity double. It records the CallContext the BFF
// forwards so a test can prove the owner receives only the authenticated session
// identity, never a browser-supplied actor or Tenant.
type tenantProbe struct {
	api.TenantProductServiceClient
	invite *api.InviteMemberRpcRequest
	remove *api.RemoveMemberRpcRequest
	accept *api.AcceptInvitationRpcRequest
	update *api.UpdateMemberRoleRpcRequest
	list   *api.ListMembersRpcRequest
	tenant *api.GetTenantRpcRequest
}

func (p *tenantProbe) InviteMember(_ context.Context, r *api.InviteMemberRpcRequest, _ ...grpc.CallOption) (*api.Invitation, error) {
	p.invite = r
	return &api.Invitation{Id: "invite-1", InviteeGatewaySubjectId: r.Body.InviteeGatewaySubjectId, Role: api.InvitationRoleEnum_INVITATION_ROLE_ENUM_MEMBER, Status: api.InvitationStatusEnum_INVITATION_STATUS_ENUM_PENDING}, nil
}
func (p *tenantProbe) RemoveMember(_ context.Context, r *api.RemoveMemberRpcRequest, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	p.remove = r
	return &emptypb.Empty{}, nil
}
func (p *tenantProbe) AcceptInvitation(_ context.Context, r *api.AcceptInvitationRpcRequest, _ ...grpc.CallOption) (*api.Member, error) {
	p.accept = r
	return &api.Member{Id: "member-1", ActorId: "actor-1", Role: api.TenantRoleEnum_TENANT_ROLE_ENUM_MEMBER, Status: api.MemberStatusEnum_MEMBER_STATUS_ENUM_ACTIVE}, nil
}
func (p *tenantProbe) UpdateMemberRole(_ context.Context, r *api.UpdateMemberRoleRpcRequest, _ ...grpc.CallOption) (*api.Member, error) {
	p.update = r
	return &api.Member{Id: r.MemberId, ActorId: "actor-9", Role: r.Body.Role, Status: api.MemberStatusEnum_MEMBER_STATUS_ENUM_ACTIVE}, nil
}
func (p *tenantProbe) ListMembers(_ context.Context, r *api.ListMembersRpcRequest, _ ...grpc.CallOption) (*api.MemberPage, error) {
	p.list = r
	return &api.MemberPage{Items: []*api.Member{{Id: "member-1", ActorId: "actor-1", Role: api.TenantRoleEnum_TENANT_ROLE_ENUM_OWNER, Status: api.MemberStatusEnum_MEMBER_STATUS_ENUM_ACTIVE}}}, nil
}
func (p *tenantProbe) GetTenant(_ context.Context, r *api.GetTenantRpcRequest, _ ...grpc.CallOption) (*api.Tenant, error) {
	p.tenant = r
	return &api.Tenant{Id: "tenant-1", Name: "Tenant One", Status: api.TenantStatusEnum_TENANT_STATUS_ENUM_ACTIVE, AssetCustodyStatus: api.TenantAssetCustodyStatusEnum_TENANT_ASSET_CUSTODY_STATUS_ENUM_TENANT_OWNED}, nil
}

func memberServer(probe *tenantProbe, identity IdentityReader) *Server {
	return &Server{identity: identity, tenant: probe}
}

func memberWrite(method, path, body string) *http.Request {
	r := sessionRequest(method, path)
	r.Header.Set("X-CSRF-Token", "csrf-1")
	r.Header.Set("Idempotency-Key", "stable-member-command")
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
		r.Body = io.NopCloser(strings.NewReader(body))
	}
	return r
}

func TestMemberCommandsRequireTheWriteGuard(t *testing.T) {
	probe := &tenantProbe{}
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_INVITEMEMBER
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_TENANT
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: ptr("tenant-1")}
	handler := memberServer(probe, identity).Handler()

	for name, mutate := range map[string]func(*http.Request){
		"missing CSRF":        func(r *http.Request) { r.Header.Del("X-CSRF-Token") },
		"missing idempotency": func(r *http.Request) { r.Header.Del("Idempotency-Key") },
		"cross-site request":  func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") },
		"forged actor and scope": func(r *http.Request) {
			r.Header.Set("x-opl-actor", "forged-actor")
			r.Header.Set("x-opl-tenant", "forged-tenant")
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := memberWrite("POST", "/api/v2/tenant/invitations", `{"inviteeGatewaySubjectId":"301","role":"member"}`)
			mutate(request)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if name == "forged actor and scope" {
				if response.Code != http.StatusCreated {
					t.Fatalf("status = %d, want 201; body = %s", response.Code, response.Body.String())
				}
				if probe.invite.GetContext().GetActorId() != "actor-1" || probe.invite.GetContext().GetScope().GetTenant().GetTenantId() != "tenant-1" {
					t.Fatalf("browser-supplied identity reached the owner: %v", probe.invite.GetContext())
				}
				return
			}
			if response.Code != http.StatusForbidden && response.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 or 403; body = %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestMemberCommandsForwardOnlyTheAuthenticatedCaller(t *testing.T) {
	probe := &tenantProbe{}
	identity := allowedIdentity()
	// Every member route authorizes the caller's own Tenant resource; only the
	// action varies, so a decision for one action cannot satisfy another.
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_TENANT
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: ptr("tenant-1")}
	handler := memberServer(probe, identity).Handler()

	// A bodyless write still needs the full write guard: accept and revoke carry no
	// request body but do mutate owner state.
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACCEPTINVITATION
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, memberWrite("POST", "/api/v2/invitations/invite-1/accept", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("accept status = %d, body = %s", response.Code, response.Body.String())
	}
	if probe.accept.GetInvitationId() != "invite-1" || probe.accept.GetContext().GetActorId() != "actor-1" {
		t.Fatalf("accept did not forward the authenticated caller: %v", probe.accept)
	}
	if probe.accept.GetContext().GetSessionId() != owneridentity.SessionReference("session-1") {
		t.Fatal("accept forwarded a non-reference session identity")
	}

	// Revoke without an idempotency key is refused before any owner call.
	noKey := sessionRequest("POST", "/api/v2/tenant/invitations/invite-1/revoke")
	noKey.Header.Set("X-CSRF-Token", "csrf-1")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, noKey)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "IDEMPOTENCY_REQUIRED") {
		t.Fatalf("bodyless write without a key was not refused: %d %s", response.Code, response.Body.String())
	}

	// Role change: the target member comes from the path, the role from the body.
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEMEMBERROLE
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, memberWrite("PUT", "/api/v2/tenant/members/member-9", `{"role":"admin"}`))
	if response.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", response.Code, response.Body.String())
	}
	if probe.update.GetMemberId() != "member-9" || probe.update.GetBody().GetRole() != api.TenantRoleEnum_TENANT_ROLE_ENUM_ADMIN {
		t.Fatalf("role change did not forward the target and role: %v", probe.update)
	}

	// Removal is 204 with no response body.
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REMOVEMEMBER
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, memberWrite("DELETE", "/api/v2/tenant/members/member-9", ""))
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("remove status = %d, body = %q", response.Code, response.Body.String())
	}
	if probe.remove.GetMemberId() != "member-9" {
		t.Fatalf("remove did not forward the target member: %v", probe.remove)
	}

	// Reads use the session's own Tenant as the authorization resource.
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTMEMBERS
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, memberWrite("GET", "/api/v2/tenant/members", ""))
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", response.Code, response.Body.String())
	}
	if probe.list.GetContext().GetScope().GetTenant().GetTenantId() != "tenant-1" {
		t.Fatalf("member list did not carry the session Tenant: %v", probe.list.GetContext())
	}
	last := identity.requests[len(identity.requests)-1]
	if last.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTMEMBERS || last.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_TENANT || last.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT || last.GetResource().GetId() != "tenant-1" {
		t.Fatalf("member list asked CloudIdentity for the wrong authority: %v", last)
	}
}

// TestMemberCommandsSurfaceOwnerRefusal proves a CloudIdentity denial is reported
// as 403 without the BFF substituting its own policy or retrying.
func TestMemberCommandsSurfaceOwnerRefusal(t *testing.T) {
	probe := &tenantProbe{}
	identity := allowedIdentity()
	identity.decision = &api.AuthorizationDecision{Result: api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED, Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY}
	handler := memberServer(probe, identity).Handler()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, memberWrite("POST", "/api/v2/tenant/invitations", `{"inviteeGatewaySubjectId":"301","role":"member"}`))
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "FORBIDDEN") {
		t.Fatalf("denied member command was not refused: %d %s", response.Code, response.Body.String())
	}
	if probe.invite != nil {
		t.Fatal("a denied member command still reached the owner")
	}
}
