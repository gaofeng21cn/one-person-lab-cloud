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
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// gatewayProductSystem provisions one isolated tenant database, a real Gateway
// Integration Service with a directory identity, and a real gRPC server with the
// GatewayProductService group registered. Every read below therefore travels
// through the production owner implementation rather than a probe or an
// Unimplemented server.
func gatewayProductSystem(t *testing.T) (api.GatewayProductServiceClient, api.TenantProductServiceClient, *identity.Service) {
	t.Helper()
	return gatewayProductSystemWithUpstream(t, nil)
}

func gatewayProductSystemWithUpstream(t *testing.T, handle func(http.ResponseWriter, *http.Request) bool) (api.GatewayProductServiceClient, api.TenantProductServiceClient, *identity.Service) {
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
	if _, e = db.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name)VALUES('tenant-test','Test');INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role)VALUES('member-test','tenant-test','101','member')`); e != nil {
		t.Fatal(e)
	}

	// The upstream Gateway fixture serves only what the owner reads: directory and
	// user login, the caller's directory identity, the admin balance and API-key
	// reads, and the OpenAI-compatible model catalog.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if handle != nil && handle(w, r) {
			return
		}
		identityPayload := func(user identity.GatewayIdentity, token string) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": token, "expires_in": 3600, "user": user}})
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/auth/login":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["password"] != "password" {
				w.WriteHeader(401)
				return
			}
			switch body["email"] {
			case "directory@example.test":
				identityPayload(identity.GatewayIdentity{ID: 1, Email: "directory@example.test", Status: "active"}, "directory-token")
			case "test@example.test":
				identityPayload(identity.GatewayIdentity{ID: 101, Email: "test@example.test", Status: "active"}, "user-token")
			default:
				w.WriteHeader(401)
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer user-token" {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": 101, "email": "test@example.test", "status": "active"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users/101":
			if r.Header.Get("Authorization") != "Bearer directory-token" {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": 101, "balance": "12.5", "status": "active"}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/users/101/api-keys":
			if r.Header.Get("Authorization") != "Bearer directory-token" {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []map[string]any{{"key": "sk-caller", "status": "active"}}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/usage/stats":
			if r.Header.Get("Authorization") != "Bearer directory-token" || r.URL.Query().Get("user_id") != "101" || r.URL.Query().Get("timezone") != "Asia/Shanghai" || r.URL.Query().Get("start_date") == "" || r.URL.Query().Get("end_date") == "" {
				w.WriteHeader(400)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"total_requests": 17, "total_input_tokens": 170, "total_output_tokens": 34, "total_tokens": 204, "total_actual_cost": "0.1250019",
			}})
		case r.Method == http.MethodGet && r.URL.Path == "/v1/models":
			if r.Header.Get("Authorization") != "Bearer sk-caller" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"gpt-5.6-sol"},{"id":"gpt-5.4","hidden":true}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)

	gateway, e := identity.NewGatewayWithDirectory(upstream.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if e != nil {
		t.Fatal(e)
	}
	service, e := identity.New(db, gateway, bytes.Repeat([]byte("s"), 32), nil, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	service.GatewayStore = gatewayStoreSystem(t)
	const token = "isolated-identity-test-token-00000001"
	config := ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: token}}
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	server, e := ownerservice.NewServer(config)
	if e != nil {
		t.Fatal(e)
	}
	if e = service.Register(server); e != nil {
		t.Fatal(e)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)
	conn, e := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, token)))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { conn.Close() })
	return api.NewGatewayProductServiceClient(conn), api.NewTenantProductServiceClient(conn), service
}

func TestGatewayProductReadsUseTheRealOwnerAndSerializeTheContractSpelling(t *testing.T) {
	gatewayClient, tenantClient, _ := gatewayProductSystem(t)
	raw, _ := login(t, tenantClient)
	ref := owneridentity.SessionReference(raw)

	wallet, err := gatewayClient.GetWallet(t.Context(), &api.GetWalletRpcRequest{Context: &api.CallContext{SessionId: proto.String(ref)}})
	if err != nil {
		t.Fatalf("GetWallet: %v", err)
	}
	if wallet.GetBalanceUsdMicros() != 12_500_000 || wallet.GetSource() != api.WalletSourceEnum_WALLET_SOURCE_ENUM_GATEWAY {
		t.Fatalf("wallet=%v", wallet)
	}
	walletJSON, err := publicjson.Marshal(wallet)
	if err != nil {
		t.Fatalf("marshal wallet: %v", err)
	}
	if !strings.Contains(string(walletJSON), `"balanceUSDMicros":"12500000"`) {
		t.Fatalf("wallet wire=%s", walletJSON)
	}

	usage, err := gatewayClient.GetUsageSummary(t.Context(), &api.GetUsageSummaryRpcRequest{Context: &api.CallContext{SessionId: proto.String(ref)}, Period: "month"})
	if err != nil {
		t.Fatalf("GetUsageSummary: %v", err)
	}
	if usage.GetTotalRequests() != 17 || usage.GetTotalInputTokens() != 170 || usage.GetTotalOutputTokens() != 34 || usage.GetTotalTokens() != 204 || usage.GetTotalActualCostUsdMicros() != 125001 {
		t.Fatalf("usage=%v", usage)
	}
	usageJSON, err := publicjson.Marshal(usage)
	if err != nil {
		t.Fatalf("marshal usage: %v", err)
	}
	if !strings.Contains(string(usageJSON), `"totalActualCostUSDMicros":"125001"`) || !strings.Contains(string(usageJSON), `"totalRequests":"17"`) {
		t.Fatalf("usage wire=%s", usageJSON)
	}

	page, err := gatewayClient.ListModels(t.Context(), &api.ListModelsRpcRequest{Context: &api.CallContext{SessionId: proto.String(ref)}})
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(page.GetItems()) != 2 || page.GetItems()[0].GetId() != "gpt-5.6-sol" || !page.GetItems()[0].GetAvailable() || page.GetItems()[1].GetAvailable() {
		t.Fatalf("models=%v", page.GetItems())
	}
	pageJSON, err := publicjson.Marshal(page)
	if err != nil {
		t.Fatalf("marshal models: %v", err)
	}
	// The catalog carries no token price: the removed contract fields stay absent.
	for _, forbidden := range []string{"PricePerMillionTokens", "priceSource"} {
		if strings.Contains(string(pageJSON), forbidden) {
			t.Fatalf("model wire leaked %s: %s", forbidden, pageJSON)
		}
	}
}

func TestGatewayProductReadsRequireAuthorizedEndpoints(t *testing.T) {
	_, tenantClient, service := gatewayProductSystem(t)
	raw, _ := login(t, tenantClient)
	ref := owneridentity.SessionReference(raw)

	// An in-process call without the Console BFF peer identity never reaches the
	// owner even when it carries a live session reference.
	if _, err := service.GetWallet(context.Background(), &api.GetWalletRpcRequest{Context: &api.CallContext{SessionId: proto.String(ref)}}); err == nil {
		t.Fatal("an unauthenticated in-process call reached GetWallet")
	}
	if _, err := service.ListModels(context.Background(), &api.ListModelsRpcRequest{Context: &api.CallContext{SessionId: proto.String(ref)}}); err == nil {
		t.Fatal("an unauthenticated in-process call reached ListModels")
	}
	if _, err := service.GetUsageSummary(context.Background(), &api.GetUsageSummaryRpcRequest{Context: &api.CallContext{SessionId: proto.String(ref)}, Period: "month"}); err == nil {
		t.Fatal("an unauthenticated in-process call reached GetUsageSummary")
	}

	// Catalog remains role-authorized; personal reads are session-bound.
	peerCtx := ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
	scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-test"}}}
	allowed := &api.AuthorizationRequest{Scope: scope, ActorId: "101", SessionId: proto.String(ref), AudienceOwner: api.OwnerEnum_OWNER_ENUM_GATEWAY, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTMODELS, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}, RequestId: "request"}
	if _, err := service.AuthorizeAction(peerCtx, allowed); err != nil {
		t.Fatalf("member ListModels was denied: %v", err)
	}
	usageAllowed := proto.Clone(allowed).(*api.AuthorizationRequest)
	usageAllowed.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETUSAGESUMMARY
	usageAllowed.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String("tenant-test")}
	if _, err := service.AuthorizeAction(peerCtx, usageAllowed); err != nil {
		t.Fatalf("member GetUsageSummary was denied: %v", err)
	}
	denied := proto.Clone(allowed).(*api.AuthorizationRequest)
	denied.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET
	if _, err := service.AuthorizeAction(peerCtx, denied); err == nil {
		t.Fatal("wallet read accepted the wrong resource kind")
	}
}

// Existing installations may hold an opaque CloudIdentity actor independent of
// the upstream subject representation. Seed that identity shape by relocating
// the rows created through real authentication, then exercise the real reads.
func TestGatewayProductReadsResolveOpaqueActorThroughDurableMapping(t *testing.T) {
	gatewayClient, tenantClient, service := gatewayProductSystem(t)
	raw, _ := login(t, tenantClient)
	ref := owneridentity.SessionReference(raw)
	if _, err := service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET actor_id='cloud-user-opaque' WHERE actor_id='101';UPDATE tenant.sessions SET actor_id='cloud-user-opaque' WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GatewayStore.DB().ExecContext(t.Context(), `UPDATE gateway.identity_mappings SET actor_id='cloud-user-opaque' WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	call := &api.CallContext{SessionId: proto.String(ref)}
	wallet, err := gatewayClient.GetWallet(t.Context(), &api.GetWalletRpcRequest{Context: call})
	if err != nil || wallet.GetBalanceUsdMicros() != 12_500_000 {
		t.Fatalf("mapped wallet=%v err=%v", wallet, err)
	}
	usage, err := gatewayClient.GetUsageSummary(t.Context(), &api.GetUsageSummaryRpcRequest{Context: call, Period: "month"})
	if err != nil || usage.GetTotalRequests() != 17 {
		t.Fatalf("mapped usage=%v err=%v", usage, err)
	}
	models, err := gatewayClient.ListModels(t.Context(), &api.ListModelsRpcRequest{Context: call})
	if err != nil || len(models.GetItems()) != 2 {
		t.Fatalf("mapped models=%v err=%v", models, err)
	}
}

func TestPersonalGatewayAuthorizationDoesNotRequireTenantRole(t *testing.T) {
	for _, role := range []string{"", "member", "admin", "owner"} {
		t.Run("role="+role, func(t *testing.T) {
			gatewayClient, tenantClient, service := gatewayProductSystem(t)
			if role == "" {
				if _, err := service.DB.ExecContext(t.Context(), `DELETE FROM tenant.tenant_members WHERE actor_id='101'`); err != nil {
					t.Fatal(err)
				}
			} else if _, err := service.DB.ExecContext(t.Context(), `UPDATE tenant.tenant_members SET role=$1 WHERE actor_id='101'`, role); err != nil {
				t.Fatal(err)
			}
			raw, _ := login(t, tenantClient)
			ref := owneridentity.SessionReference(raw)
			scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
			tenantID := ""
			if role != "" {
				tenantID = "tenant-test"
				scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenantID}}}
			}
			peerCtx := ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF)
			for _, action := range []api.AuthorizationActionEnum{api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETUSAGESUMMARY,
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYS,
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEY,
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYGROUPS,
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYUSAGE,
				api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEYUSAGESUMMARY} {
				request := &api.AuthorizationRequest{Scope: scope, ActorId: "101", SessionId: proto.String(ref), AudienceOwner: api.OwnerEnum_OWNER_ENUM_GATEWAY, Action: action, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: proto.String(tenantID)}, RequestId: "personal-" + action.String()}
				if tenantID == "" {
					request.Resource.Id = nil
				}
				decision, err := service.AuthorizeAction(peerCtx, request)
				if err != nil {
					t.Fatalf("personal read denied: %v", err)
				}
				replay := proto.Clone(request).(*api.AuthorizationRequest)
				replay.AuthorizationContextId = proto.String(decision.GetAuthorizationContextId())
				if _, err := service.AuthorizeAction(peerCtx, replay); err != nil {
					t.Fatalf("exact replay: %v", err)
				}
				for _, mutate := range []func(*api.AuthorizationRequest){
					func(r *api.AuthorizationRequest) { r.ActorId = "102" },
					func(r *api.AuthorizationRequest) { r.Resource.Id = proto.String("other-tenant") },
					func(r *api.AuthorizationRequest) {
						r.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "other-tenant"}}}
					},
					func(r *api.AuthorizationRequest) { r.AudienceOwner = api.OwnerEnum_OWNER_ENUM_WORKSPACE },
				} {
					forged := proto.Clone(request).(*api.AuthorizationRequest)
					mutate(forged)
					if _, err := service.AuthorizeAction(peerCtx, forged); err == nil {
						t.Fatal("cross-subject authorization accepted")
					}
				}
			}
			call := &api.CallContext{SessionId: proto.String(ref)}
			if wallet, err := gatewayClient.GetWallet(t.Context(), &api.GetWalletRpcRequest{Context: call}); err != nil || wallet.GetBalanceUsdMicros() != 12_500_000 {
				t.Fatalf("wallet=%v err=%v", wallet, err)
			}
			if usage, err := gatewayClient.GetUsageSummary(t.Context(), &api.GetUsageSummaryRpcRequest{Context: call, Period: "month"}); err != nil || usage.GetTotalRequests() != 17 {
				t.Fatalf("usage=%v err=%v", usage, err)
			}
		})
	}
}
