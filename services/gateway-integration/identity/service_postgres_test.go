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
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

type commitReader struct {
	api.OwnerCommitReadbackClient
	actual *api.OwnerCommitEvidence
	reads  int
}

func (r *commitReader) ReadOwnerCommit(_ context.Context, _ *api.ReadOwnerCommitRequest, _ ...grpc.CallOption) (*api.OwnerCommitEvidence, error) {
	r.reads++
	return proto.Clone(r.actual).(*api.OwnerCommitEvidence), nil
}

// gatewayFixture is the synthetic Gateway the identity tests read through. The
// subject status is what the authoritative directory read reports, and the read
// counter is what proves an authorization decision actually consulted the Gateway
// owner instead of being satisfied by the session row alone.
type gatewayFixture struct {
	mu     sync.Mutex
	status string
	reads  int
	// url is the fixture's own base URL, so a second replica can reach the same
	// synthetic Gateway with its own adapter.
	url string
}

func (f *gatewayFixture) setStatus(status string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.status = status
}

func (f *gatewayFixture) subjectReads() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads
}

// newGatewayFixture serves the member login, the caller's own identity read and
// the administrative directory read that the authoritative active-subject check
// uses. The member credential and the service's own directory credential are
// different tokens, so a test can tell which one answered a call.
func newGatewayFixture(t *testing.T) (*httptest.Server, *gatewayFixture) {
	t.Helper()
	fixture := &gatewayFixture{status: "active"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fixture.mu.Lock()
		status := fixture.status
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/users/") {
			fixture.reads++
		}
		fixture.mu.Unlock()
		user := identity.GatewayIdentity{ID: 101, Email: "test@example.test", Status: status}
		switch {
		case r.URL.Path == "/api/v1/auth/login":
			var body map[string]string
			if json.NewDecoder(r.Body).Decode(&body) != nil || body["password"] != "password" {
				w.WriteHeader(401)
				return
			}
			// The member login and this service's own directory identity reach the
			// same endpoint and never share a token.
			credential := "private-gateway-token"
			if body["email"] == "directory@example.test" {
				user = identity.GatewayIdentity{ID: 1, Email: "directory@example.test", Status: "active"}
				credential = "private-directory-token"
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": credential, "user": user}})
		case r.URL.Path == "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer private-gateway-token" {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": user})
		case r.URL.Path == "/api/v1/admin/users/101":
			if r.Header.Get("Authorization") != "Bearer private-directory-token" {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": user})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(server.Close)
	fixture.url = server.URL
	return server, fixture
}

func system(t *testing.T) (*identity.Service, *sql.DB, api.TenantProductServiceClient, string) {
	t.Helper()
	s, db, c, address, _ := systemWithFixture(t)
	return s, db, c, address
}

// systemWithFixture is system plus the synthetic Gateway it reads through, so a
// test can change what the Gateway reports about the session's subject.
func systemWithFixture(t *testing.T) (*identity.Service, *sql.DB, api.TenantProductServiceClient, string, *gatewayFixture) {
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
	_, e = db.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name)VALUES('tenant-test','Test');INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role)VALUES('member-test','tenant-test','101','admin')`)
	if e != nil {
		t.Fatal(e)
	}
	gateway, fixture := newGatewayFixture(t)
	g, e := identity.NewGatewayWithDirectory(gateway.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if e != nil {
		t.Fatal(e)
	}
	s, e := identity.New(db, g, bytes.Repeat([]byte("s"), 32), nil, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	s.GatewayStore = gatewayStoreSystem(t)
	const token = "isolated-identity-test-token-00000001"
	config := ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: token}}
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
	conn, e := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, token)))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	return s, db, api.NewTenantProductServiceClient(conn), l.Addr().String(), fixture
}
func login(t *testing.T, c api.TenantProductServiceClient) (string, *api.Session) {
	t.Helper()
	var header metadata.MD
	_, e := c.GetLoginContext(t.Context(), &api.GetLoginContextRpcRequest{}, grpc.Header(&header))
	if e != nil {
		t.Fatal(e)
	}
	challenge := header.Get(owneridentity.SessionCookieHeader)[0]
	out, e := c.Login(t.Context(), &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: "test@example.test", Password: "password"}}, grpc.Header(&header))
	if e != nil {
		t.Fatal(e)
	}
	return header.Get(owneridentity.SessionCookieHeader)[0], out
}
func TestPublisherSessionAndGrantPostgres(t *testing.T) {
	s, db, c, address := system(t)
	raw, session := login(t, c)
	ctx := t.Context()
	ref := owneridentity.SessionReference(raw)
	if ref == raw || session.ActorId != "101" || session.GetTenantId() != "tenant-test" {
		t.Fatal("incorrect issued identity")
	}
	// The browser credential, Gateway credential and password never enter owner DB.
	var persisted string
	if e := db.QueryRowContext(ctx, `SELECT row_to_json(s)::text FROM tenant.sessions s WHERE id=$1`, ref).Scan(&persisted); e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{raw, "private-gateway-token", "password"} {
		if strings.Contains(persisted, secret) {
			t.Fatal("raw credential persisted")
		}
	}
	if _, e := c.GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: &ref}}); e == nil {
		t.Fatal("non-bearer reference accepted as browser cookie")
	}
	forged, e := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if e != nil {
		t.Fatal(e)
	}
	defer forged.Close()
	if _, e = api.NewTenantProductServiceClient(forged).GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: &raw}}); e == nil {
		t.Fatal("unauthenticated peer reached identity")
	}
	peerCtx := ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF)
	request := &api.AuthorizationRequest{Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}}, ActorId: "101", SessionId: &ref, AudienceOwner: api.OwnerEnum_OWNER_ENUM_BUILD, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String("package-version")}, RequestId: "request"}
	decision, e := s.AuthorizeAction(peerCtx, request)
	if e != nil {
		t.Fatal(e)
	}
	introspected, e := s.GetAuthorizationContext(ownerservice.WithPeerOwner(ctx, owneridentity.Build.Service()), &api.GetAuthorizationContextRequest{AuthorizationContextId: decision.GetAuthorizationContextId(), ExpectedAudienceOwner: request.AudienceOwner, ExpectedAction: request.Action, ExpectedResource: request.Resource, RequestId: "introspect"})
	if e != nil || introspected.GetAuthorizationContextId() != decision.GetAuthorizationContextId() {
		t.Fatal("context introspection changed identity", e)
	}
	// Publisher continuations preserve the original session and permission version,
	// even though the called owner/action is deliberately different.
	publisher := proto.Clone(request).(*api.AuthorizationRequest)
	publisher.AudienceOwner = api.OwnerEnum_OWNER_ENUM_CAPABILITY
	publisher.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESOLVEBUILDINPUT
	publisher.AuthorizationContextId = decision.AuthorizationContextId
	capabilityContext := ownerservice.WithPeerOwner(ctx, owneridentity.Capability.Service())
	if _, e = s.AuthorizeAction(capabilityContext, publisher); e != nil {
		t.Fatal("original publisher continuation was refused", e)
	}
	secondCookie, _ := login(t, c)
	switched := proto.Clone(publisher).(*api.AuthorizationRequest)
	switched.SessionId = proto.String(owneridentity.SessionReference(secondCookie))
	if _, e = s.AuthorizeAction(capabilityContext, switched); e == nil {
		t.Fatal("another session reused the original publisher authorization")
	}
	if _, e = db.ExecContext(ctx, `UPDATE tenant.tenants SET permission_version=permission_version+1 WHERE id='tenant-test'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthorizeAction(capabilityContext, publisher); e == nil {
		t.Fatal("publisher continuation reused a stale permission version")
	}
	decision, e = s.AuthorizeAction(peerCtx, request)
	if e != nil {
		t.Fatal(e)
	}
	for _, mutate := range []func(*api.AuthorizationRequest){
		func(r *api.AuthorizationRequest) { r.ActorId = "forged" },
		func(r *api.AuthorizationRequest) {
			r.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "other"}}}
		},
		func(r *api.AuthorizationRequest) { r.AudienceOwner = api.OwnerEnum_OWNER_ENUM_LEDGER },
		func(r *api.AuthorizationRequest) {
			r.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPUBLISHERNAMESPACE
			r.AudienceOwner = api.OwnerEnum_OWNER_ENUM_CAPABILITY
			r.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
		},
	} {
		r := proto.Clone(request).(*api.AuthorizationRequest)
		mutate(r)
		if _, e = s.AuthorizeAction(peerCtx, r); e == nil {
			t.Fatal("forged authority allowed")
		}
	}
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String("runtime")}
	proof := &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_BUILD, OperationId: "operation", ResourceId: "build", AcceptedInputDigest: "sha256:" + strings.Repeat("a", 64), CommittedVersion: 1, AcceptedAt: timestamppb.Now(), AuthorizationContextId: decision.GetAuthorizationContextId(), ActorId: request.ActorId, Scope: request.Scope, AcceptedAction: request.Action, AuthorizationResource: request.Resource, ContinuationResources: []*api.AuthorizationResource{resource}}
	commits := &commitReader{actual: proof}
	s.BuildCommit = commits
	grantRequest := &api.AcceptedOperationGrantRequest{AuthorizationContextId: decision.GetAuthorizationContextId(), OwnerCommitEvidence: proof, AllowedActions: []api.AuthorizationActionEnum{api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE}}
	buildCtx := ownerservice.WithPeerOwner(ctx, owneridentity.Build.Service())
	grant, e := s.IssueAcceptedOperationGrant(buildCtx, grantRequest)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := s.IssueAcceptedOperationGrant(buildCtx, grantRequest)
	if e != nil || replay.Id != grant.Id || commits.reads < 2 {
		t.Fatal("grant issuance did not re-read and replay")
	}
	bad := proto.Clone(grantRequest).(*api.AcceptedOperationGrantRequest)
	bad.OwnerCommitEvidence.ResourceId = "another-build"
	if _, e = s.IssueAcceptedOperationGrant(buildCtx, bad); e == nil {
		t.Fatal("caller-authored commit accepted")
	}
	bad = proto.Clone(grantRequest).(*api.AcceptedOperationGrantRequest)
	bad.AllowedActions = append(bad.AllowedActions, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE)
	if _, e = s.IssueAcceptedOperationGrant(buildCtx, bad); e == nil {
		t.Fatal("grant action expansion accepted")
	}
	if _, e = c.Logout(ctx, &api.LogoutRpcRequest{Context: &api.CallContext{SessionId: &ref}}); e != nil {
		t.Fatal(e)
	}
	if _, e = c.GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: &raw}}); e == nil {
		t.Fatal("logged out session accepted")
	}
	continuation := &api.AuthorizationRequest{Scope: request.Scope, ActorId: request.ActorId, AcceptedOperationGrantId: &grant.Id, AudienceOwner: api.OwnerEnum_OWNER_ENUM_CAPABILITY, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE, Resource: resource, RequestId: "continuation"}
	capCtx := ownerservice.WithPeerOwner(ctx, owneridentity.Capability.Service())
	if _, e = s.AuthorizeAction(peerCtx, continuation); e == nil {
		t.Fatal("browser peer used an internal continuation grant")
	}
	if _, e = s.AuthorizeAction(capCtx, continuation); e != nil {
		t.Fatal("logout erased original obligation", e)
	}
	wrong := proto.Clone(continuation).(*api.AuthorizationRequest)
	wrong.Resource.Id = proto.String("unrelated")
	if _, e = s.AuthorizeAction(capCtx, wrong); e == nil {
		t.Fatal("grant escaped original resources")
	}
	// Revocation is read from the owner on each request. Closeout remains possible.
	if _, e = db.ExecContext(ctx, `UPDATE tenant.tenant_members SET revoked_at=now() WHERE actor_id='101';UPDATE tenant.tenants SET permission_version=permission_version+1 WHERE id='tenant-test'`); e != nil {
		t.Fatal(e)
	}
	if _, e = s.AuthorizeAction(capCtx, continuation); e == nil {
		t.Fatal("revoked member acquired a new reference")
	}
	continuation.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE
	if _, e = s.AuthorizeAction(capCtx, continuation); e != nil {
		t.Fatal("original cleanup was lost", e)
	}
	var mode string
	if e = db.QueryRowContext(ctx, `SELECT mode FROM tenant.accepted_operation_grants WHERE id=$1`, grant.Id).Scan(&mode); e != nil || mode != "closeout_only" {
		t.Fatal("revoked grant mode did not converge", e)
	}
	// Restart drops only volatile delegated credentials; the accepted obligation
	// remains durable and does not need a browser password/session to close out.
	restarted, e := identity.New(db, s.Gateway, bytes.Repeat([]byte("s"), 32), nil, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	restarted.GatewayStore = s.GatewayStore
	restarted.BuildCommit = commits
	if _, e = restarted.AuthorizeAction(capCtx, continuation); e != nil {
		t.Fatal("restart lost original grant", e)
	}
	if _, e = db.ExecContext(ctx, `UPDATE tenant.accepted_operation_grants SET expires_at=issued_at+interval '1 millisecond' WHERE id=$1`, grant.Id); e != nil {
		t.Fatal(e)
	}
	time.Sleep(2 * time.Millisecond)
	if _, e = restarted.AuthorizeAction(capCtx, continuation); e == nil {
		t.Fatal("expired grant accepted")
	}
}

// A session has to answer on whichever replica receives the request. `other` is a
// second Service over the same database with its own Gateway adapter, which is what
// a replica that never saw the login looks like: it holds no member credential and
// no cached directory credential either. Under the removed in-process credential
// this returned "reauthentication required after credential expiry or process
// restart".
func TestSessionAnswersOnAReplicaThatDidNotLogin(t *testing.T) {
	s, db, c, _, fixture := systemWithFixture(t)
	raw, session := login(t, c)
	ref := owneridentity.SessionReference(raw)
	ctx := t.Context()

	var stored string
	if e := db.QueryRowContext(ctx, `SELECT display_name FROM tenant.sessions WHERE id=$1`, ref).Scan(&stored); e != nil {
		t.Fatal(e)
	}
	if stored != session.GetDisplayName() {
		t.Fatalf("session row display name %q does not match the issued session %q", stored, session.GetDisplayName())
	}
	var persisted string
	if e := db.QueryRowContext(ctx, `SELECT row_to_json(s)::text FROM tenant.sessions s WHERE id=$1`, ref).Scan(&persisted); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(persisted, "private-gateway-token") || strings.Contains(persisted, "private-directory-token") {
		t.Fatal("gateway credential persisted to make the session portable")
	}

	other, e := identity.NewGatewayWithDirectory(fixture.url, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if e != nil {
		t.Fatal(e)
	}
	replica, e := identity.New(db, other, bytes.Repeat([]byte("s"), 32), nil, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	request := &api.AuthorizationRequest{Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}}, ActorId: "101", SessionId: &ref, AudienceOwner: api.OwnerEnum_OWNER_ENUM_BUILD, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String("package-version")}, RequestId: "replica"}
	replica.GatewayStore = s.GatewayStore
	before := fixture.subjectReads()
	if _, e = replica.AuthorizeAction(ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF), request); e != nil {
		t.Fatal("a session must not depend on the process that logged in", e)
	}
	if after := fixture.subjectReads(); after == before {
		t.Fatal("the replica answered a session without consulting the Gateway owner")
	}
	// The authoritative check is the Gateway's, so it applies on a replica that
	// never saw the login too.
	fixture.setStatus("disabled")
	if _, e = replica.AuthorizeAction(ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF), request); e == nil {
		t.Fatal("a disabled subject obtained new authorization on the replica that did not log in")
	}
	fixture.setStatus("active")

	// Revocation stays the owner's own boundary and still ends the session
	// everywhere, not only in the replica that revoked it.
	if _, e = replica.Logout(ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF), &api.LogoutRpcRequest{Context: &api.CallContext{SessionId: &ref}}); e != nil {
		t.Fatal(e)
	}
	if _, e = replica.AuthorizeAction(ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF), request); e == nil {
		t.Fatal("revoked session still authorized on another replica")
	}
	if _, e = s.AuthorizeAction(ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF), request); e == nil {
		t.Fatal("a logout on one replica left the session usable on another")
	}
	if _, e = c.GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: &raw}}); e == nil {
		t.Fatal("logged out session accepted")
	}
}

// A running identity service must not accept a configuration that can only fail
// every session read: session authorization always checks the Gateway directory.
func TestServiceRequiresDirectoryIdentityAtStartupPostgres(t *testing.T) {
	_, db, _, _, fixture := systemWithFixture(t)

	withoutDirectory, e := identity.NewGateway(fixture.url)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = identity.New(db, withoutDirectory, bytes.Repeat([]byte("s"), 32), nil, time.Hour); e == nil {
		t.Fatal("identity startup accepted a Gateway without the directory required by session authorization")
	}

	withDirectory, e := identity.NewGatewayWithDirectory(fixture.url, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = identity.New(db, withDirectory, bytes.Repeat([]byte("s"), 32), nil, time.Hour); e != nil {
		t.Fatalf("configured identity startup failed: %v", e)
	}
}

// Disabling the Gateway account is an external state change that never touches the
// session row, so the row alone cannot tell an active actor from a disabled one.
// New interactive authorization therefore has to re-read the Gateway owner on the
// request, and it has to do so on any replica: answering this from the session row
// alone lets a disabled actor keep obtaining new authorization for the rest of the
// session's lifetime.
func TestExternallyDisabledSubjectCannotAuthorizeANewAction(t *testing.T) {
	s, db, c, _, fixture := systemWithFixture(t)
	raw, _ := login(t, c)
	ref := owneridentity.SessionReference(raw)
	ctx := t.Context()
	peerCtx := ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF)
	request := &api.AuthorizationRequest{Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}}, ActorId: "101", SessionId: &ref, AudienceOwner: api.OwnerEnum_OWNER_ENUM_BUILD, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String("package-version")}, RequestId: "disabled-subject"}

	if _, e := s.AuthorizeAction(peerCtx, request); e != nil {
		t.Fatal("an active subject must authorize", e)
	}

	fixture.setStatus("disabled")
	if _, e := s.AuthorizeAction(peerCtx, request); e == nil {
		t.Fatal("an externally disabled subject obtained new authorization")
	} else if code := status.Code(e); code != codes.Unauthenticated {
		t.Fatalf("disabled subject refusal code = %v, want Unauthenticated (%v)", code, e)
	}
	if reads := fixture.subjectReads(); reads == 0 {
		t.Fatal("the refusal did not read the Gateway owner")
	}
	// The refusal is the Gateway's authority speaking, not Cloud erasing the
	// session: the row is untouched, so the same session authorizes again as soon
	// as the Gateway reports the subject active.
	var revoked sql.NullTime
	if e := db.QueryRowContext(ctx, `SELECT revoked_at FROM tenant.sessions WHERE id=$1`, ref).Scan(&revoked); e != nil {
		t.Fatal(e)
	}
	if revoked.Valid {
		t.Fatal("a disabled subject must be denied by the active-subject check, not by a Cloud revocation")
	}
	fixture.setStatus("active")
	if _, e := s.AuthorizeAction(peerCtx, request); e != nil {
		t.Fatal("the same live session did not authorize again once the subject was active", e)
	}
}

// beforeDisplayName narrows the owner's real embedded migration set to the files
// that predate the display-name column, so the transition is proven against the
// exact SQL the running migration applies instead of a rewritten copy.
type beforeDisplayName struct{ source ownerstore.MigrationSource }

func (s beforeDisplayName) ReadDir(name string) ([]ownerstore.DirEntry, error) {
	entries, err := s.source.ReadDir(name)
	if err != nil {
		return nil, err
	}
	kept := make([]ownerstore.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), "0003") {
			continue
		}
		kept = append(kept, entry)
	}
	return kept, nil
}

func (s beforeDisplayName) ReadFile(name string) ([]byte, error) { return s.source.ReadFile(name) }

// The old Login INSERT omits display_name. It has to keep working after the column
// exists, both while old and new replicas run together and after an image-only
// rollback, and the rows a disabled migration would have broken have to survive it
// with their own state intact.
func TestSessionDisplayNameMigrationKeepsRemainingOldWriterWorking(t *testing.T) {
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
	// The old writer's exact INSERT, from the pre-change image.
	const oldWriterInsert = `INSERT INTO tenant.sessions(id,session_hash,actor_id,tenant_id,gateway_session_ref,csrf_hash,expires_at) VALUES($1,$2,$3,$4,$1,$5,$6)`
	// The old reader's exact session SELECT, from the pre-change image. An image-only
	// rollback runs this, so it has to keep answering for rows this change wrote.
	const oldReaderSelect = `SELECT s.actor_id,s.tenant_id,s.expires_at,m.role,COALESCE(t.permission_version,0)+1 FROM tenant.sessions s LEFT JOIN tenant.tenants t ON t.id=s.tenant_id LEFT JOIN tenant.tenant_members m ON m.tenant_id=s.tenant_id AND m.actor_id=s.actor_id AND m.revoked_at IS NULL WHERE s.id=$1 AND s.revoked_at IS NULL AND s.expires_at>now() AND (s.tenant_id IS NULL OR (t.status='active' AND m.id IS NOT NULL))`

	if e = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), beforeDisplayName{source}); e != nil {
		t.Fatal(e)
	}
	db, e := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	if _, e = db.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name)VALUES('tenant-test','Test');INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role)VALUES('member-test','tenant-test','101','admin')`); e != nil {
		t.Fatal(e)
	}
	for _, row := range []struct {
		id      string
		expires time.Time
		revoked bool
	}{
		{id: "preexisting-valid", expires: time.Now().Add(time.Hour)},
		{id: "preexisting-expired", expires: time.Now().Add(-time.Hour)},
		{id: "preexisting-revoked", expires: time.Now().Add(time.Hour), revoked: true},
	} {
		if _, e = db.ExecContext(ctx, oldWriterInsert, row.id, "hash-"+row.id, "101", "tenant-test", "csrf-"+row.id, row.expires); e != nil {
			t.Fatal(e)
		}
		if row.revoked {
			if _, e = db.ExecContext(ctx, `UPDATE tenant.sessions SET revoked_at=now() WHERE id=$1`, row.id); e != nil {
				t.Fatal(e)
			}
		}
	}
	var beforeActor, beforeTenant, beforeRole sql.NullString
	var beforeExpires time.Time
	var beforeVersion int64
	if e = db.QueryRowContext(ctx, oldReaderSelect, "preexisting-valid").Scan(&beforeActor, &beforeTenant, &beforeExpires, &beforeRole, &beforeVersion); e != nil {
		t.Fatalf("the pre-existing valid session did not answer before the migration: %v", e)
	}

	// The real migration entrypoint, now applying only the new file.
	if e = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); e != nil {
		t.Fatal(e)
	}
	for _, want := range []struct {
		id       string
		revoked  bool
		answered bool
	}{
		{id: "preexisting-valid", answered: true},
		{id: "preexisting-expired"},
		{id: "preexisting-revoked", revoked: true},
	} {
		var name string
		var revoked sql.NullTime
		if e = db.QueryRowContext(ctx, `SELECT display_name,revoked_at FROM tenant.sessions WHERE id=$1`, want.id).Scan(&name, &revoked); e != nil {
			t.Fatalf("pre-existing session %s did not survive the migration: %v", want.id, e)
		}
		if name != "101" {
			t.Fatalf("pre-existing session %s display name = %q, want the backfilled actor id", want.id, name)
		}
		if revoked.Valid != want.revoked {
			t.Fatalf("pre-existing session %s revoked_at present = %v, want %v", want.id, revoked.Valid, want.revoked)
		}
		var actor, tenant, role sql.NullString
		var expires time.Time
		var version int64
		readErr := db.QueryRowContext(ctx, oldReaderSelect, want.id).Scan(&actor, &tenant, &expires, &role, &version)
		if want.answered && readErr != nil {
			t.Fatalf("the pre-existing valid session stopped answering after the migration: %v", readErr)
		}
		if !want.answered && readErr != sql.ErrNoRows {
			t.Fatalf("pre-existing session %s answered the pre-change reader after the migration: %v", want.id, readErr)
		}
	}

	// The remaining old writer: its INSERT omits the new column entirely.
	if _, e = db.ExecContext(ctx, oldWriterInsert, "old-writer-after-migration", "hash-old-writer", "101", "tenant-test", "csrf-old-writer", time.Now().Add(time.Hour)); e != nil {
		t.Fatalf("the old Login INSERT cannot write after the migration: %v", e)
	}
	var written sql.NullString
	if e = db.QueryRowContext(ctx, `SELECT display_name FROM tenant.sessions WHERE id='old-writer-after-migration'`).Scan(&written); e != nil {
		t.Fatal(e)
	}
	if written.Valid {
		t.Fatalf("the old writer's row display name = %q, want no captured name", written.String)
	}
	// The new read still names that session, from the same fallback the old image
	// used, so a mixed-version fleet displays one name for one session.
	var name string
	if e = db.QueryRowContext(ctx, `SELECT COALESCE(display_name,actor_id) FROM tenant.sessions WHERE id='old-writer-after-migration'`).Scan(&name); e != nil {
		t.Fatal(e)
	}
	if name != "101" {
		t.Fatalf("the old writer's row displays %q, want the actor id the old image displayed", name)
	}
	// The image-rollback boundary: the old image's session read still answers for
	// the row the new writer created with an explicit display name.
	if _, e = db.ExecContext(ctx, `INSERT INTO tenant.sessions(id,session_hash,actor_id,tenant_id,gateway_session_ref,csrf_hash,expires_at,display_name) VALUES('new-writer-row','hash-new-writer','101','tenant-test','new-writer-row','csrf-new-writer',$1,'new@example.test')`, time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	var actor, tenant, role sql.NullString
	var expires time.Time
	var version int64
	if e = db.QueryRowContext(ctx, oldReaderSelect, "new-writer-row").Scan(&actor, &tenant, &expires, &role, &version); e != nil {
		t.Fatalf("the pre-change image cannot read a row this change wrote: %v", e)
	}
	if actor.String != "101" || tenant.String != "tenant-test" || role.String != "admin" {
		t.Fatalf("the pre-change reader saw %v/%v/%v, want the unchanged session facts", actor, tenant, role)
	}
}
