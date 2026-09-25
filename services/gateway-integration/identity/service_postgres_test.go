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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
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
func system(t *testing.T) (*identity.Service, *sql.DB, api.TenantProductServiceClient, string) {
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
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := identity.GatewayIdentity{ID: 101, Email: "test@example.test", Status: "active"}
		if r.URL.Path == "/api/v1/auth/login" {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["password"] != "password" {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "private-gateway-token", "user": user}})
			return
		}
		if r.URL.Path != "/api/v1/auth/me" || r.Header.Get("Authorization") != "Bearer private-gateway-token" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": user})
	}))
	t.Cleanup(gateway.Close)
	g, e := identity.NewGateway(gateway.URL)
	if e != nil {
		t.Fatal(e)
	}
	s, e := identity.New(db, g, bytes.Repeat([]byte("s"), 32), nil, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
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
	return s, db, api.NewTenantProductServiceClient(conn), l.Addr().String()
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
