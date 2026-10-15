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
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

const bindingPeerToken = "isolated-binding-peer-token-0000001"

// bindingSystem serves real Tenant and Gateway owners over isolated, separate
// PostgreSQL databases. Only the external Sub2API response is simulated.
func bindingSystem(t *testing.T) (*identity.Service, api.TenantProductServiceClient, string) {
	return bindingSystemWithHistory(t, nil)
}

func bindingSystemWithHistory(t *testing.T, seed func(*sql.DB)) (*identity.Service, api.TenantProductServiceClient, string) {
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
	if seed != nil {
		ownerDB, err := h.Open(ctx, h.OwnerDSN, h.DatabaseName())
		if err != nil {
			t.Fatal(err)
		}
		seed(ownerDB)
		ownerDB.Close()
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
			switch body["email"] {
			case "admin@example.test":
			case "testcloud4@medopl.com", "renamed@medopl.com":
				user = identity.GatewayIdentity{ID: 201, Email: body["email"], Status: "active"}
			case "other@example.test":
				user = identity.GatewayIdentity{ID: 202, Email: body["email"], Status: "active"}
			case "testcloud4@other.example":
				user = identity.GatewayIdentity{ID: 203, Email: body["email"], Status: "active"}
			default:
				w.WriteHeader(401)
				return
			}
			if body["password"] != "password" {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "admin-token", "user": user}})
			return
		}
		// The authoritative active-subject check reads the actor behind the session
		// through this service's own directory identity. This answer is synthetic
		// and stands in for the real Sub2API directory: it says only that actor
		// 103, the subject this fixture logs in as, is still active. The adapter
		// refuses a directory answer about any other subject id.
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/users/") && r.Header.Get("Authorization") == "Bearer admin-token" {
			subject, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/"), 10, 64)
			if err != nil || (subject != 103 && subject != 201 && subject != 202 && subject != 203) {
				w.WriteHeader(404)
				return
			}
			user.ID = subject
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": user})
			return
		}
		if r.URL.Path != "/api/v1/auth/me" || r.Header.Get("Authorization") != "Bearer admin-token" {
			w.WriteHeader(401)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": user})
	}))
	t.Cleanup(gateway.Close)
	g, e := identity.NewGatewayWithDirectory(gateway.URL, &identity.GatewayDirectory{Email: "admin@example.test", Password: "password"})
	if e != nil {
		t.Fatal(e)
	}
	s, e := identity.New(db, g, bytes.Repeat([]byte("b"), 32), []string{"103"}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	s.GatewayStore = gatewayStoreSystem(t)
	s.ConfigureRegistry("uswccr.ccs.tencentyun.com", "oplcloud")
	config := ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: bindingPeerToken, owneridentity.Build.Service(): bindingPeerToken, owneridentity.ControlPlane: bindingPeerToken}}
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

func strPtr(v string) *string { return &v }
