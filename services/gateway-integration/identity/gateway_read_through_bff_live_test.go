package identity_test

import (
	"bytes"
	"context"
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

	bff "opl-cloud/apps/console-bff"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// TestLiveGatewayReadThroughBFF is the joined end-to-end proof the review asked
// for: the real Console BFF handler, using the real clients.Dial transport and
// the real identity.Service, drives the Gateway owner over typed gRPC.
//
//	real CloudIdentity login -> the BFF's own wallet/model route guard and
//	authorization precondition -> Gateway Integration's real owner process over
//	typed gRPC -> its live Sub2API reads
//
// Only the external Sub2API HTTP boundary is simulated.
func TestLiveGatewayReadThroughBFF(t *testing.T) {
	ctx := context.Background()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "tenant", Database: "opl_tenant", SchemaOwnerRole: "opl_tenant_owner", WriterRole: "opl_tenant_writer", RuntimeRole: "opl_tenant_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatal(err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err = db.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name)VALUES('tenant-1','Test');INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role)VALUES('member-1','tenant-1','101','owner')`); err != nil {
		t.Fatal(err)
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		envelope := func(payload map[string]any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": payload})
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["password"] != "password" {
				w.WriteHeader(401)
				return
			}
			if body["email"] == "directory@example.test" {
				envelope(map[string]any{"access_token": "directory-token", "user": map[string]any{"id": 1, "email": "directory@example.test", "status": "active"}})
				return
			}
			envelope(map[string]any{"access_token": "user-token", "user": map[string]any{"id": 101, "email": "customer@example.test", "status": "active"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me":
			envelope(map[string]any{"id": 101, "email": "customer@example.test", "status": "active"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users/101":
			envelope(map[string]any{"id": 101, "balance": "12.5", "status": "active"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users/101/api-keys":
			envelope(map[string]any{"items": []map[string]any{{"key": "sk-caller", "status": "active"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5.6-sol"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	gateway, err := identity.NewGatewayWithDirectory(upstream.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := identity.New(db, gateway, bytes.Repeat([]byte("s"), 32), nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	const token = "isolated-gateway-read-token-0000001"
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := ownerservice.NewServer(ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: token}})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(server); err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)
	address := listener.Addr().String()

	// The BFF's own transport and its own route guard, not a bespoke handler.
	reader, err := bff.DialGatewayReader(address, token, token, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	handler := bff.NewGatewayReadHandler(reader.GatewayClient(), reader)

	// Sign in through the real CloudIdentity owner to obtain the browser cookie.
	loginConn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, token)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { loginConn.Close() })
	var header metadata.MD
	if _, err = api.NewTenantProductServiceClient(loginConn).GetLoginContext(ctx, &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	if _, cookie, err := reader.Login(ctx, header.Get(owneridentity.SessionCookieHeader)[0], &api.LoginRequest{Username: "customer@example.test", Password: "password"}); err != nil {
		t.Fatal(err)
	} else {
		read := func(path string) *httptest.ResponseRecorder {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			request.AddCookie(&http.Cookie{Name: "opl_session", Value: cookie})
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			return response
		}
		wallet := read("/api/v2/wallet")
		if wallet.Code != http.StatusOK || !strings.Contains(wallet.Body.String(), `"balanceUSDMicros":"12500000"`) {
			t.Fatalf("wallet status=%d body=%s", wallet.Code, wallet.Body.String())
		}
		models := read("/api/v2/catalog/models")
		if models.Code != http.StatusOK || !strings.Contains(models.Body.String(), `"id":"gpt-5.6-sol"`) {
			t.Fatalf("models status=%d body=%s", models.Code, models.Body.String())
		}
		for _, forbidden := range []string{"PricePerMillionTokens", "priceSource"} {
			if strings.Contains(models.Body.String(), forbidden) {
				t.Fatalf("model wire leaked %s: %s", forbidden, models.Body.String())
			}
		}
		anonymous := httptest.NewRecorder()
		handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, "/api/v2/wallet", nil))
		if anonymous.Code != http.StatusUnauthorized {
			t.Fatalf("anonymous wallet status=%d body=%s", anonymous.Code, anonymous.Body.String())
		}
	}
}
