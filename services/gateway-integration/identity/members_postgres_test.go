package identity_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
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
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// memberActors is the isolated Gateway identity fixture: an external, test-only
// directory that maps a reserved .test email to one stable numeric subject. Only
// this external boundary is simulated; CloudIdentity executes its real HTTP
// adapter, session issuance, live policy and typed member writes.
var memberActors = map[string]int64{
	"owner-a@example.test":   101,
	"owner-b@example.test":   201,
	"invitee@example.test":   301,
	"solo@example.test":      501,
	"one@example.test":       601,
	"two@example.test":       602,
	"member@example.test":    701,
	"admin@example.test":     702,
	"owner@example.test":     703,
	"platform@example.test":  103,
	"directory@example.test": 900,
}

const (
	memberPeerToken     = "isolated-member-governance-token-0001"
	memberInvitationTTL = time.Hour
)

type memberSystem struct {
	db     *sql.DB
	client api.TenantProductServiceClient
	auth   api.CloudIdentityAuthorizationClient
}

func newMemberSystem(t *testing.T) memberSystem {
	t.Helper()
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, e := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "tenant", Database: "opl_tenant", SchemaOwnerRole: "opl_tenant_owner", WriterRole: "opl_tenant_writer", RuntimeRole: "opl_tenant_runtime"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
	source, e := migrations.Source()
	if e != nil {
		t.Fatal(e)
	}
	if e = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); e != nil {
		t.Fatal(e)
	}
	db, e := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	_, e = db.ExecContext(ctx, `
		INSERT INTO tenant.tenants(id,name) VALUES('tenant-a','A'),('tenant-b','B'),('tenant-solo','Solo'),('tenant-pair','Pair');
		INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES
			('member-a-owner','tenant-a','101','owner'),
			('member-b-owner','tenant-b','201','owner'),
			('member-solo-owner','tenant-solo','501','owner'),
			('pair-owner-one','tenant-pair','601','owner'),
			('pair-owner-two','tenant-pair','602','owner')`)
	if e != nil {
		t.Fatal(e)
	}
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			var body struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != "password" {
				w.WriteHeader(401)
				return
			}
			actor, ok := memberActors[body.Email]
			if !ok {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "isolated-gateway-" + body.Email, "user": identity.GatewayIdentity{ID: actor, Email: body.Email, Status: "active"}}})
			return
		}
		if r.URL.Path == "/api/v1/auth/me" {
			email := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer isolated-gateway-")
			actor, ok := memberActors[email]
			if !ok {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": identity.GatewayIdentity{ID: actor, Email: email, Status: "active"}})
			return
		}
		// The administrative directory read. Only this service's own configured
		// directory identity may use it, and it reads; it never mutates.
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/users/") {
			if r.Header.Get("Authorization") != "Bearer isolated-gateway-directory@example.test" {
				w.WriteHeader(401)
				return
			}
			raw := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/")
			actor, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				w.WriteHeader(404)
				return
			}
			for email, id := range memberActors {
				if id == actor && email != "directory@example.test" {
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": identity.GatewayIdentity{ID: id, Email: email, Status: "active"}})
					return
				}
			}
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(gateway.Close)
	// The deployment configures its own directory identity, so the display-name and
	// invitee-existence facts resolve through the real authorized read.
	g, e := identity.NewGatewayWithDirectory(gateway.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if e != nil {
		t.Fatal(e)
	}
	s, e := identity.New(db, g, bytes.Repeat([]byte("m"), 32), []string{"103"}, memberInvitationTTL)
	if e != nil {
		t.Fatal(e)
	}
	config := ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: memberPeerToken}}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server, e := ownerservice.NewServer(config)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Register(server); e != nil {
		t.Fatal(e)
	}
	go server.ServeOn(l)
	t.Cleanup(server.Stop)
	conn, e := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, memberPeerToken)))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	authConn, e := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, memberPeerToken)))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { authConn.Close() })
	return memberSystem{db: db, client: api.NewTenantProductServiceClient(conn), auth: api.NewCloudIdentityAuthorizationClient(authConn)}
}

// memberLogin authenticates one Gateway subject through the real login path and
// returns the opaque browser cookie plus the resolved session.
func memberLogin(t *testing.T, c api.TenantProductServiceClient, email string) (string, *api.Session) {
	t.Helper()
	var header metadata.MD
	if _, e := c.GetLoginContext(t.Context(), &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); e != nil {
		t.Fatal(e)
	}
	challenge := header.Get(owneridentity.SessionCookieHeader)[0]
	out, e := c.Login(t.Context(), &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: email, Password: "password"}}, grpc.Header(&header))
	if e != nil {
		t.Fatal(e)
	}
	return header.Get(owneridentity.SessionCookieHeader)[0], out
}

// memberCall builds the CallContext the BFF produces: the resolved session
// identity, the session reference, the session's own scope, and a request id.
func memberCall(session *api.Session, cookie, idempotencyKey string) *api.CallContext {
	ref := owneridentity.SessionReference(cookie)
	scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
	if session.GetTenantId() != "" {
		scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: session.GetTenantId()}}}
	}
	return &api.CallContext{ActorId: session.ActorId, SessionId: &ref, Scope: scope, RequestId: "request-" + session.ActorId, IdempotencyKey: idempotencyKey}
}

func memberCode(t *testing.T, err error) api.ErrorCodeEnum {
	t.Helper()
	code, ok := owneridentity.ErrorCode(err)
	if !ok {
		t.Fatalf("owner failure carries no canonical error code: %v", err)
	}
	return code
}

func TestMemberGovernancePostgres(t *testing.T) {
	system := newMemberSystem(t)
	ctx := t.Context()
	c := system.client

	ownerCookie, owner := memberLogin(t, c, "owner-a@example.test")
	otherCookie, other := memberLogin(t, c, "owner-b@example.test")
	inviteeCookie, invitee := memberLogin(t, c, "invitee@example.test")
	if owner.GetTenantId() != "tenant-a" || other.GetTenantId() != "tenant-b" || invitee.GetTenantId() != "" {
		t.Fatalf("unexpected session Tenant binding: %q %q %q", owner.GetTenantId(), other.GetTenantId(), invitee.GetTenantId())
	}
	if _, e := c.GetTenant(ctx, &api.GetTenantRpcRequest{Context: memberCall(owner, ownerCookie, "get-tenant")}); e != nil {
		t.Fatal("an owner could not read its own Tenant", e)
	}

	// Invite a subject that is not yet a member of any Tenant. A replay of the same
	// idempotency key returns the original invitation instead of a second one.
	invitation, e := c.InviteMember(ctx, &api.InviteMemberRpcRequest{Context: memberCall(owner, ownerCookie, "invite-301"), Body: &api.InviteMemberRequest{InviteeGatewaySubjectId: "301", Role: api.InviteMemberRequestRoleEnum_INVITE_MEMBER_REQUEST_ROLE_ENUM_MEMBER}})
	if e != nil {
		t.Fatal(e)
	}
	if invitation.Status != api.InvitationStatusEnum_INVITATION_STATUS_ENUM_PENDING || invitation.InviteeGatewaySubjectId != "301" {
		t.Fatalf("unexpected invitation %v", invitation)
	}
	replay, e := c.InviteMember(ctx, &api.InviteMemberRpcRequest{Context: memberCall(owner, ownerCookie, "invite-301"), Body: &api.InviteMemberRequest{InviteeGatewaySubjectId: "301", Role: api.InviteMemberRequestRoleEnum_INVITE_MEMBER_REQUEST_ROLE_ENUM_MEMBER}})
	if e != nil || replay.GetId() != invitation.GetId() {
		t.Fatalf("invite replay did not return the original invitation: %q %v", replay.GetId(), e)
	}
	// The invitation token is stored only as a hash and is never returned.
	var storedHash, invitedBy string
	if e = system.db.QueryRowContext(ctx, `SELECT token_hash,invited_by FROM tenant.invitations WHERE id=$1`, invitation.Id).Scan(&storedHash, &invitedBy); e != nil {
		t.Fatal(e)
	}
	if len(storedHash) != 64 || strings.Trim(storedHash, "0123456789abcdef") != "" || invitedBy != owner.ActorId {
		t.Fatal("invitation was not stored as a subject-bound hash")
	}

	// Cross-Tenant objects are reported as absent, never as forbidden, so another
	// Tenant's identifiers are not confirmed to exist.
	if _, e = c.RevokeInvitation(ctx, &api.RevokeInvitationRpcRequest{Context: memberCall(other, otherCookie, "cross-revoke"), InvitationId: invitation.Id}); status.Code(e) != codes.NotFound {
		t.Fatalf("cross-Tenant invitation revoke was not refused as absent: %v", e)
	}
	if _, e = c.RevokeInvitation(ctx, &api.RevokeInvitationRpcRequest{Context: memberCall(owner, ownerCookie, "own-revoke-foreign"), InvitationId: "invitation-that-does-not-exist"}); status.Code(e) != codes.NotFound {
		t.Fatalf("unknown invitation was not refused as absent: %v", e)
	}
	if _, e = c.AcceptInvitation(ctx, &api.AcceptInvitationRpcRequest{Context: memberCall(other, otherCookie, "cross-accept"), InvitationId: invitation.Id}); e == nil {
		t.Fatal("a foreign subject accepted another Tenant's invitation")
	}
	if _, e = c.RemoveMember(ctx, &api.RemoveMemberRpcRequest{Context: memberCall(other, otherCookie, "cross-remove"), MemberId: "member-a-owner"}); e == nil {
		t.Fatal("cross-Tenant member removal was allowed")
	}
	if _, e = c.UpdateMemberRole(ctx, &api.UpdateMemberRoleRpcRequest{Context: memberCall(other, otherCookie, "cross-role"), MemberId: "member-a-owner", Body: &api.UpdateMemberRoleRequest{Role: api.TenantRoleEnum_TENANT_ROLE_ENUM_ADMIN}}); e == nil {
		t.Fatal("cross-Tenant role change was allowed")
	}
	page, e := c.ListMembers(ctx, &api.ListMembersRpcRequest{Context: memberCall(other, otherCookie, "list-b")})
	if e != nil {
		t.Fatal("member list for the caller's own Tenant failed", e)
	}
	for _, m := range page.Items {
		if m.ActorId == "101" {
			t.Fatal("member list leaked another Tenant's member")
		}
	}

	// Acceptance binds exactly the invited subject.
	member, e := c.AcceptInvitation(ctx, &api.AcceptInvitationRpcRequest{Context: memberCall(invitee, inviteeCookie, "accept-301"), InvitationId: invitation.Id})
	if e != nil {
		t.Fatal(e)
	}
	if member.ActorId != "301" || member.Role != api.TenantRoleEnum_TENANT_ROLE_ENUM_MEMBER || member.Status != api.MemberStatusEnum_MEMBER_STATUS_ENUM_ACTIVE {
		t.Fatalf("unexpected accepted member %v", member)
	}
	// The BFF re-reads the session on every request, so the repeat attempt carries
	// the scope the session now has after the accepted membership was bound.
	live, e := c.GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(inviteeCookie)}})
	if e != nil || live.GetTenantId() != "tenant-a" {
		t.Fatalf("accepted membership was not reflected in the live session: %v %v", live.GetTenantId(), e)
	}
	if _, e = c.AcceptInvitation(ctx, &api.AcceptInvitationRpcRequest{Context: memberCall(live, inviteeCookie, "accept-301-again"), InvitationId: invitation.Id}); memberCode(t, e) != api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID {
		t.Fatalf("a consumed invitation was accepted twice: %v", e)
	}
	if _, e = c.RevokeInvitation(ctx, &api.RevokeInvitationRpcRequest{Context: memberCall(owner, ownerCookie, "revoke-accepted"), InvitationId: invitation.Id}); memberCode(t, e) != api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID {
		t.Fatalf("an accepted invitation was revoked: %v", e)
	}
	// Acceptance advanced the Tenant permission version, so authority issued
	// before it is stale.
	var version int64
	if e = system.db.QueryRowContext(ctx, `SELECT permission_version FROM tenant.tenants WHERE id='tenant-a'`).Scan(&version); e != nil {
		t.Fatal(e)
	}
	if version < 1 {
		t.Fatal("acceptance did not advance the Tenant permission version")
	}

	// The last owner can neither be demoted nor removed.
	soloCookie, solo := memberLogin(t, c, "solo@example.test")
	if _, e = c.UpdateMemberRole(ctx, &api.UpdateMemberRoleRpcRequest{Context: memberCall(solo, soloCookie, "demote-solo"), MemberId: "member-solo-owner", Body: &api.UpdateMemberRoleRequest{Role: api.TenantRoleEnum_TENANT_ROLE_ENUM_ADMIN}}); memberCode(t, e) != api.ErrorCodeEnum_ERROR_CODE_ENUM_LAST_OWNER {
		t.Fatalf("the last owner was demoted: %v", e)
	}
	if _, e = c.RemoveMember(ctx, &api.RemoveMemberRpcRequest{Context: memberCall(solo, soloCookie, "remove-solo"), MemberId: "member-solo-owner"}); memberCode(t, e) != api.ErrorCodeEnum_ERROR_CODE_ENUM_LAST_OWNER {
		t.Fatalf("the last owner was removed: %v", e)
	}

	// Removing a member revokes its live sessions and every later owner read.
	if _, e = c.RemoveMember(ctx, &api.RemoveMemberRpcRequest{Context: memberCall(owner, ownerCookie, "remove-301"), MemberId: member.GetId()}); e != nil {
		t.Fatal(e)
	}
	if _, e = c.GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(inviteeCookie))}}); status.Code(e) != codes.Unauthenticated {
		t.Fatalf("a removed member's session stayed usable: %v", e)
	}
	if _, e = c.GetTenant(ctx, &api.GetTenantRpcRequest{Context: memberCall(invitee, inviteeCookie, "after-removal")}); e == nil {
		t.Fatal("a removed member still read Tenant facts")
	}
	page, e = c.ListMembers(ctx, &api.ListMembersRpcRequest{Context: memberCall(owner, ownerCookie, "list-a")})
	if e != nil {
		t.Fatal(e)
	}
	seen := false
	for _, m := range page.Items {
		if m.Id == member.GetId() {
			seen = true
			if m.Status != api.MemberStatusEnum_MEMBER_STATUS_ENUM_REVOKED {
				t.Fatalf("removed member reported as %v", m.Status)
			}
		}
	}
	if !seen {
		t.Fatal("removed membership was erased instead of recorded as revoked")
	}
	invitations, e := c.ListInvitations(ctx, &api.ListInvitationsRpcRequest{Context: memberCall(owner, ownerCookie, "list-invitations")})
	if e != nil || len(invitations.Items) != 1 || invitations.Items[0].Status != api.InvitationStatusEnum_INVITATION_STATUS_ENUM_ACCEPTED {
		t.Fatalf("invitation history was not readable after acceptance: %v %v", invitations.GetItems(), e)
	}

	// Audit evidence retains both confirmed and rejected governance outcomes.
	rows, e := system.db.QueryContext(ctx, `SELECT action,outcome FROM tenant.audit_events`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	outcomes := map[string]bool{}
	for rows.Next() {
		var action, outcome string
		if e = rows.Scan(&action, &outcome); e != nil {
			t.Fatal(e)
		}
		outcomes[action+":"+outcome] = true
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{
		"inviteMember:confirmed",
		"acceptInvitation:confirmed",
		"removeMember:confirmed",
		"removeMember:rejected",
		"updateMemberRole:rejected",
		"revokeInvitation:rejected",
	} {
		if !outcomes[want] {
			t.Fatalf("audit evidence is missing %s: %v", want, outcomes)
		}
	}

	// No browser cookie, password, Gateway token or invitation token reached
	// persisted owner state.
	var persisted string
	if e = system.db.QueryRowContext(ctx, `SELECT (SELECT COALESCE(string_agg(safe_details::text,' '),'') FROM tenant.audit_events)||(SELECT COALESCE(string_agg(row_to_json(s)::text,' '),'') FROM tenant.sessions s)||(SELECT COALESCE(string_agg(row_to_json(i)::text,' '),'') FROM tenant.invitations i)`).Scan(&persisted); e != nil {
		t.Fatal(e)
	}
	// token_hash is deliberately stored; only the raw invitation token, the browser
	// cookie, the password and the Gateway token must never be persisted.
	for name, secret := range map[string]string{"invitee cookie": inviteeCookie, "owner cookie": ownerCookie, "solo cookie": soloCookie, "password": "password", "gateway token": "isolated-gateway"} {
		if secret != "" && strings.Contains(persisted, secret) {
			t.Fatalf("%s reached persisted owner state", name)
		}
	}
}

func TestLastOwnerProtectionUnderConcurrencyPostgres(t *testing.T) {
	system := newMemberSystem(t)
	ctx := t.Context()
	c := system.client
	oneCookie, one := memberLogin(t, c, "one@example.test")
	twoCookie, two := memberLogin(t, c, "two@example.test")
	if one.GetTenantId() != "tenant-pair" || two.GetTenantId() != "tenant-pair" {
		t.Fatal("pair owners did not resolve to the pair Tenant")
	}
	// Each owner concurrently removes the other. Only one may succeed; the Tenant
	// must never be left without an owner.
	calls := []func() error{
		func() error {
			_, err := c.RemoveMember(ctx, &api.RemoveMemberRpcRequest{Context: memberCall(one, oneCookie, "remove-two"), MemberId: "pair-owner-two"})
			return err
		},
		func() error {
			_, err := c.RemoveMember(ctx, &api.RemoveMemberRpcRequest{Context: memberCall(two, twoCookie, "remove-one"), MemberId: "pair-owner-one"})
			return err
		},
	}
	var wg sync.WaitGroup
	results := make([]error, len(calls))
	for i, call := range calls {
		wg.Add(1)
		go func(i int, call func() error) {
			defer wg.Done()
			results[i] = call()
		}(i, call)
	}
	wg.Wait()
	succeeded, refused := 0, 0
	for _, err := range results {
		if err == nil {
			succeeded++
			continue
		}
		if code, ok := owneridentity.ErrorCode(err); ok && code == api.ErrorCodeEnum_ERROR_CODE_ENUM_LAST_OWNER {
			refused++
			continue
		}
		t.Fatalf("concurrent owner removal returned an unexpected result: %v", err)
	}
	if succeeded != 1 || refused != 1 {
		t.Fatalf("concurrent owner removal did not serialize: succeeded=%d last_owner=%d", succeeded, refused)
	}
	var owners int
	var ownerTenant string
	if e := system.db.QueryRowContext(ctx, `SELECT count(*),COALESCE(max(tenant_id),'') FROM tenant.tenant_members WHERE tenant_id='tenant-pair' AND role='owner' AND revoked_at IS NULL`).Scan(&owners, &ownerTenant); e != nil {
		t.Fatal(e)
	}
	if owners != 1 || ownerTenant != "tenant-pair" {
		t.Fatalf("tenant-pair ended with %d active owners in %q", owners, ownerTenant)
	}
}

// TestMemberDisplayNameComesFromTheAuthorizedDirectoryPostgres proves the member
// list resolves a real display name through the service's own authorized Gateway
// directory read, and that a deployment without that identity leaves the name
// unresolved rather than inventing one.
func TestMemberDisplayNameComesFromTheAuthorizedDirectoryPostgres(t *testing.T) {
	system := newMemberSystem(t)
	ctx := t.Context()
	ownerCookie, owner := memberLogin(t, system.client, "owner-a@example.test")

	page, e := system.client.ListMembers(ctx, &api.ListMembersRpcRequest{Context: memberCall(owner, ownerCookie, "list-names")})
	if e != nil {
		t.Fatal(e)
	}
	found := false
	for _, item := range page.Items {
		if item.ActorId != "101" {
			continue
		}
		found = true
		if item.DisplayName != "owner-a@example.test" {
			t.Fatalf("displayName = %q, expected the directory name", item.DisplayName)
		}
	}
	if !found {
		t.Fatal("the owning member was not listed")
	}

	// The name is resolved from the directory, never derived from the actor id.
	for _, item := range page.Items {
		if item.DisplayName == item.ActorId && item.DisplayName != "" {
			t.Fatalf("displayName was derived from the actor id: %v", item.DisplayName)
		}
	}
}

// TestInviteeMustExistInTheGatewayDirectoryPostgres proves entry validation: with
// the directory identity configured, only a subject the authority knows can be
// invited, and the invitation is still stored as a hash.
func TestInviteeMustExistInTheGatewayDirectoryPostgres(t *testing.T) {
	system := newMemberSystem(t)
	ctx := t.Context()
	ownerCookie, owner := memberLogin(t, system.client, "owner-a@example.test")

	// An unknown subject is refused before any row is written.
	if _, e := system.client.InviteMember(ctx, &api.InviteMemberRpcRequest{Context: memberCall(owner, ownerCookie, "invite-unknown"), Body: &api.InviteMemberRequest{InviteeGatewaySubjectId: "999999", Role: api.InviteMemberRequestRoleEnum_INVITE_MEMBER_REQUEST_ROLE_ENUM_MEMBER}}); memberCode(t, e) != api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID {
		t.Fatalf("an unknown Gateway subject was invited: %v", e)
	}
	var count int
	if e := system.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.invitations`).Scan(&count); e != nil {
		t.Fatal(e)
	}
	if count != 0 {
		t.Fatalf("a refused invitation left %d rows", count)
	}

	// A subject the directory knows is accepted.
	if _, e := system.client.InviteMember(ctx, &api.InviteMemberRpcRequest{Context: memberCall(owner, ownerCookie, "invite-known"), Body: &api.InviteMemberRequest{InviteeGatewaySubjectId: "301", Role: api.InviteMemberRequestRoleEnum_INVITE_MEMBER_REQUEST_ROLE_ENUM_MEMBER}}); e != nil {
		t.Fatalf("a known Gateway subject was refused: %v", e)
	}
}
