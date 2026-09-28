package identity_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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

const bindingPeerToken = "isolated-binding-peer-token-0000001"

// bindingSystem stands up the real Tenant owner with a platform-admin actor and
// a Build peer, so CreateTenant and the cross-owner binding read run their
// actual handlers over one isolated PostgreSQL database.
func bindingSystem(t *testing.T) (*identity.Service, api.TenantProductServiceClient, string) {
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
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := identity.GatewayIdentity{ID: 103, Email: "admin@example.test", Status: "active"}
		if r.URL.Path == "/api/v1/auth/login" {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["password"] != "password" {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "admin-token", "user": user}})
			return
		}
		if r.URL.Path != "/api/v1/auth/me" || r.Header.Get("Authorization") != "Bearer admin-token" {
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
	s, e := identity.New(db, g, bytes.Repeat([]byte("b"), 32), []string{"103"}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	s.ConfigureRegistry("uswccr.ccs.tencentyun.com", "oplcloud")
	config := ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: bindingPeerToken, owneridentity.Build.Service(): bindingPeerToken}}
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
	conn, e := grpc.NewClient(l.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, bindingPeerToken)))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	return s, api.NewTenantProductServiceClient(conn), l.Addr().String()
}

// platformAdminCall creates a platform-admin session and returns the call
// context needed to admit a Tenant.
func platformAdminCall(t *testing.T, c api.TenantProductServiceClient) *api.CallContext {
	t.Helper()
	var header metadata.MD
	if _, e := c.GetLoginContext(t.Context(), &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); e != nil {
		t.Fatal(e)
	}
	challenge := header.Get(owneridentity.SessionCookieHeader)[0]
	session, e := c.Login(t.Context(), &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: "admin@example.test", Password: "password"}}, grpc.Header(&header))
	if e != nil {
		t.Fatal(e)
	}
	ref := owneridentity.SessionReference(header.Get(owneridentity.SessionCookieHeader)[0])
	scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
	return &api.CallContext{SessionId: &ref, ActorId: session.ActorId, Scope: scope, RequestId: "admit-" + session.ActorId, IdempotencyKey: "admit-" + session.ActorId}
}

func TestTenantRepositoryBindingPostgres(t *testing.T) {
	_, c, address := bindingSystem(t)
	ctx := t.Context()
	base := platformAdminCall(t, c)

	create := func(key, name, email, ownerSubject string) *api.Operation {
		t.Helper()
		call := proto.Clone(base).(*api.CallContext)
		call.RequestId = key
		call.IdempotencyKey = key
		op, e := c.CreateTenant(ctx, &api.CreateTenantRpcRequest{Context: call, Body: &api.CreateTenantRequest{Name: name, OwnerGatewaySubjectId: ownerSubject, OwnerEmail: email}})
		if e != nil {
			t.Fatal(e)
		}
		return op
	}
	first := create("create-a", "Alpha", "huangrende@fenggaolab.org", "201")
	if first.Status != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || first.ResourceId == "" {
		t.Fatalf("createTenant did not succeed: %v", first)
	}
	// A second Tenant with the same local-part must not alias the first; the
	// binding gets a deterministic suffix.
	second := create("create-b", "Beta", "huangrende@other.example", "202")
	if second.ResourceId == first.ResourceId {
		t.Fatal("two Tenants received the same id")
	}

	buildConn, e := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.Build.Service(), bindingPeerToken)))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { buildConn.Close() })
	bindings := api.NewCloudIdentityAuthorizationClient(buildConn)

	a, e := bindings.GetTenantRepositoryBinding(ctx, &api.GetTenantRepositoryBindingRequest{TenantId: first.ResourceId})
	if e != nil {
		t.Fatal(e)
	}
	if a.GetRegistryHost() != "uswccr.ccs.tencentyun.com" || a.GetRegistryNamespace() != "oplcloud" || a.GetRepository() != "huangrende" {
		t.Fatalf("first binding = %v", a)
	}
	b, e := bindings.GetTenantRepositoryBinding(ctx, &api.GetTenantRepositoryBindingRequest{TenantId: second.ResourceId})
	if e != nil {
		t.Fatal(e)
	}
	if b.GetRepository() != "huangrende-1" {
		t.Fatalf("collision suffix = %q, want huangrende-1", b.GetRepository())
	}
	// Re-reading the same Tenant is stable and never re-derives from email.
	again, e := bindings.GetTenantRepositoryBinding(ctx, &api.GetTenantRepositoryBindingRequest{TenantId: first.ResourceId})
	if e != nil || again.GetRepository() != "huangrende" {
		t.Fatalf("binding is not stable: %v %v", again, e)
	}
	// An unknown Tenant has no binding.
	if _, e := bindings.GetTenantRepositoryBinding(ctx, &api.GetTenantRepositoryBindingRequest{TenantId: "does-not-exist"}); status.Code(e) != codes.NotFound {
		t.Fatalf("unknown tenant read code = %v", status.Code(e))
	}

	// ConsoleBFF may read too, but no other peer may.
	consoleConn, e := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, bindingPeerToken)))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { consoleConn.Close() })
	if _, e := api.NewCloudIdentityAuthorizationClient(consoleConn).GetTenantRepositoryBinding(ctx, &api.GetTenantRepositoryBindingRequest{TenantId: first.ResourceId}); e != nil {
		t.Fatalf("console bff read refused: %v", e)
	}
	ledgerConn, e := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.Ledger.Service(), bindingPeerToken)))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { ledgerConn.Close() })
	if _, e := api.NewCloudIdentityAuthorizationClient(ledgerConn).GetTenantRepositoryBinding(ctx, &api.GetTenantRepositoryBindingRequest{TenantId: first.ResourceId}); e == nil {
		t.Fatal("a non-Build/non-BFF peer read the binding")
	}
}

func TestCreateTenantRequiresPlatformAdminPostgres(t *testing.T) {
	s, c, _ := bindingSystem(t)
	ctx := t.Context()
	// A non-admin platform actor is refused before any write.
	call := &api.CallContext{SessionId: strPtr("nope"), ActorId: "999", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "deny", IdempotencyKey: "deny"}
	if _, e := c.CreateTenant(ctx, &api.CreateTenantRpcRequest{Context: call, Body: &api.CreateTenantRequest{Name: "X", OwnerGatewaySubjectId: "103", OwnerEmail: "x@example.com"}}); e == nil {
		t.Fatal("non-admin admitted a Tenant")
	}
	_ = s
}

func TestBackfillTenantRepositoryBindingsPostgres(t *testing.T) {
	s, _, address := bindingSystem(t)
	ctx := t.Context()
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name) VALUES('tenant-existing-a','Existing A'),('tenant-existing-b','Existing B')`); e != nil {
		t.Fatal(e)
	}
	if _, e := s.DB.ExecContext(ctx, `INSERT INTO tenant.tenant_repository_bindings(tenant_id,registry_host,registry_namespace,repository) VALUES('tenant-existing-a','uswccr.ccs.tencentyun.com','oplcloud','preserved-destination')`); e != nil {
		t.Fatal(e)
	}
	if e := s.BackfillTenantRepositoryBindings(ctx); e != nil {
		t.Fatal(e)
	}
	// Repeating owner startup does not replace or allocate another binding.
	if e := s.BackfillTenantRepositoryBindings(ctx); e != nil {
		t.Fatal(e)
	}
	conn, e := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.Build.Service(), bindingPeerToken)))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	bindings := api.NewCloudIdentityAuthorizationClient(conn)
	for _, tenantID := range []string{"tenant-existing-a", "tenant-existing-b"} {
		binding, e := bindings.GetTenantRepositoryBinding(ctx, &api.GetTenantRepositoryBindingRequest{TenantId: tenantID})
		if e != nil {
			t.Fatal(e)
		}
		sum := sha256.Sum256([]byte(tenantID))
		wantRepository := "tenant-" + hex.EncodeToString(sum[:])[:24]
		if tenantID == "tenant-existing-a" {
			wantRepository = "preserved-destination"
		}
		if binding.GetRegistryHost() != "uswccr.ccs.tencentyun.com" || binding.GetRegistryNamespace() != "oplcloud" || binding.GetRepository() != wantRepository {
			t.Fatalf("backfilled binding for %s = %v", tenantID, binding)
		}
	}
	var count int
	if e := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM tenant.tenant_repository_bindings WHERE tenant_id IN ('tenant-existing-a','tenant-existing-b')`).Scan(&count); e != nil || count != 2 {
		t.Fatalf("backfilled binding count = %d, %v", count, e)
	}
}

func strPtr(v string) *string { return &v }
