//go:build livebuild

package build

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"opl-cloud/apps/console-bff"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	cloudidentity "opl-cloud/services/gateway-integration/identity"
	identitymigrations "opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
)

const identityPeerToken = "isolated-cloud-identity-peer-token-0001"

type liveIdentity struct {
	service       *cloudidentity.Service
	client        bff.IdentityClient
	db            *sql.DB
	address       string
	cookies, csrf map[string]string
}

func identityConn(t *testing.T, address string, peer owneridentity.Service) *grpc.ClientConn {
	t.Helper()
	conn, e := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, owneridentity.PeerHeader, string(peer), owneridentity.TokenHeader, identityPeerToken)
		return invoke(ctx, method, req, reply, cc, opts...)
	}))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
func (s *liveIdentity) auth(t *testing.T, owner owneridentity.Owner) api.CloudIdentityAuthorizationClient {
	return api.NewCloudIdentityAuthorizationClient(identityConn(t, s.address, owner.Service()))
}
func newLiveIdentity(t *testing.T, ctx context.Context, dsn string) *liveIdentity {
	t.Helper()
	// Only the external Sub2API boundary is simulated. CloudIdentity executes its
	// real HTTP adapter, login/session issuance, live DB policy and typed grants.
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := int64(101)
		email := "publisher@example.test"
		if r.URL.Path == "/api/v1/auth/login" {
			var body struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != "isolated-password" {
				w.WriteHeader(401)
				return
			}
			email = body.Email
			switch email {
			case "publisher@example.test":
			case "other@example.test":
				actor = 102
			case "admin@example.test":
				actor = 103
			default:
				w.WriteHeader(401)
				return
			}
		} else if r.URL.Path == "/api/v1/auth/me" {
			switch r.Header.Get("Authorization") {
			case "Bearer isolated-gateway-publisher@example.test":
			case "Bearer isolated-gateway-other@example.test":
				actor = 102
				email = "other@example.test"
			case "Bearer isolated-gateway-admin@example.test":
				actor = 103
				email = "admin@example.test"
			default:
				w.WriteHeader(401)
				return
			}
		} else {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		user := cloudidentity.GatewayIdentity{ID: actor, Email: email, Status: "active"}
		if r.URL.Path == "/api/v1/auth/login" {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "isolated-gateway-" + email, "user": user}})
		} else {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": user})
		}
	}))
	t.Cleanup(gateway.Close)
	source, e := identitymigrations.Source()
	if e != nil {
		t.Fatal(e)
	}
	db, _ := liveOwnerDB(t, ctx, dsn, "tenant", source)
	// Known test Tenant membership is a declared local deployment prerequisite;
	// neither login nor qualification registers an external wallet.
	if _, e = db.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name) VALUES('tenant-live','Test Tenant'),('another-tenant','Other Tenant'); INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('member-live','tenant-live','101','admin'),('member-other','another-tenant','102','admin')`); e != nil {
		t.Fatal(e)
	}
	g, e := cloudidentity.NewGateway(gateway.URL)
	if e != nil {
		t.Fatal(e)
	}
	service, e := cloudidentity.New(db, g, bytes.Repeat([]byte("k"), 32), []string{"103"}, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	cfg := ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{}}
	for _, p := range []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.Build.Service(), owneridentity.Capability.Service(), owneridentity.RuntimeControl.Service()} {
		cfg.Peers[p] = identityPeerToken
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server, e := ownerservice.NewServer(cfg)
	if e != nil {
		t.Fatal(e)
	}
	if e = service.Register(server); e != nil {
		t.Fatal(e)
	}
	go server.ServeOn(l)
	t.Cleanup(server.Stop)
	client, e := bff.DialIdentity(l.Addr().String(), identityPeerToken, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(client.Close)
	out := &liveIdentity{service: service, client: client, db: db, address: l.Addr().String(), cookies: map[string]string{}, csrf: map[string]string{}}
	for tid, email := range map[string]string{"tenant-live": "publisher@example.test", "another-tenant": "other@example.test", "platform": "admin@example.test"} {
		_, challenge, e := client.LoginContext(ctx)
		if e != nil {
			t.Fatal(e)
		}
		session, cookie, e := client.Login(ctx, challenge, &api.LoginRequest{Username: email, Password: "isolated-password"})
		if e != nil {
			t.Fatal(e)
		}
		out.cookies[tid] = cookie
		out.csrf[tid] = session.CsrfToken
	}
	return out
}
func (s *liveIdentity) call(key, tenant string) *api.CallContext {
	scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}
	actor := "101"
	if tenant == "platform" {
		scope.Scope = &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}
		actor = "103"
	} else if tenant == "another-tenant" {
		actor = "102"
	}
	return &api.CallContext{Scope: scope, ActorId: actor, SessionId: proto.String(owneridentity.SessionReference(s.cookies[tenant])), RequestId: key, IdempotencyKey: key}
}
