package identity_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/gatewaymigrations"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

func TestServiceEntitlementsAndEd25519GrantPostgres(t *testing.T) {
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN: dsn, Owner: "tenant", Database: "opl_tenant",
		SchemaOwnerRole: "opl_tenant_owner", WriterRole: "opl_tenant_writer", RuntimeRole: "opl_tenant_runtime",
	})
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

	gwH, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN: dsn, Owner: "gateway", Database: "opl_gateway",
		SchemaOwnerRole: "opl_gateway_owner", WriterRole: "opl_gateway_writer", RuntimeRole: "opl_gateway_runtime",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gwH.Close(context.Background()) })
	gwSource, err := gatewaymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = gwH.Install(ctx, gwH.OwnerDSN, gwH.DatabaseName(), gwSource); err != nil {
		t.Fatal(err)
	}
	gwDB, err := gwH.Open(ctx, gwH.RuntimeDSN, gwH.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gwDB.Close() })

	actors := map[string]int64{
		"admin@example.test":     103,
		"customer@example.test":  201,
		"directory@example.test": 900,
	}
	gatewaySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/auth/login":
			var body struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != "password" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			actor, ok := actors[strings.ToLower(strings.TrimSpace(body.Email))]
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"access_token": "gw-" + body.Email,
					"user":         identity.GatewayIdentity{ID: actor, Email: body.Email, Status: "active"},
				},
			})
		case r.URL.Path == "/api/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer gw-customer@example.test" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": identity.GatewayIdentity{ID: 201, Email: "customer@example.test", Status: "active"},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/admin/users/"):
			id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/"), 10, 64)
			if err != nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			for email, candidate := range actors {
				if candidate == id && email != "directory@example.test" {
					_ = json.NewEncoder(w).Encode(map[string]any{
						"code": 0,
						"data": identity.GatewayIdentity{ID: id, Email: email, Status: "active"},
					})
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(gatewaySrv.Close)

	gwClient, err := identity.NewGatewayWithDirectory(gatewaySrv.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	signingKey := make([]byte, 32)
	_, _ = rand.Read(signingKey)
	svc, err := identity.New(db, gwClient, signingKey, []string{"103"}, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	gwStore, err := identity.NewGatewayStore(gwDB)
	if err != nil {
		t.Fatal(err)
	}
	svc.GatewayStore = gwStore

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := identity.ParseServiceCatalogJSON(`{
		"netfleet_cloud": {"default_origin":"https://netfleet.example.test","audience":"opl-netfleet-cloud","admission_mode":"active_gateway_default_with_overrides","default_limits":{"max_sites":10}},
		"link_relay": {"default_origin":"https://link.example.test","audience":"opl-link-relay","admission_mode":"allowlist_only"}
	}`)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := identity.NewGrantSigner(base64.RawURLEncoding.EncodeToString(priv.Seed()), "k-pg-1", "https://cloud.example.test", "", 2*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	svc.ConfigureServiceGrants(catalog, signer)

	const bffToken = "isolated-service-ent-bff-token-0001"
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	grpcSrv := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF)
		return handler(ctx, req)
	}))
	api.RegisterTenantProductServiceServer(grpcSrv, svc)
	api.RegisterCloudIdentityAuthorizationServer(grpcSrv, svc)
	go func() { _ = grpcSrv.Serve(ln) }()
	t.Cleanup(grpcSrv.Stop)

	conn, err := grpc.NewClient(ln.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := api.NewTenantProductServiceClient(conn)

	// 1. active_gateway_default_with_overrides admits Gateway customer without DB row, while allowlist_only fails closed.
	grant, err := client.IssueServiceGrant(ctx, &api.IssueServiceGrantRpcRequest{
		Body: &api.ServiceGrantRequest{Email: "customer@example.test", Password: "password", Service: "netfleet_cloud"},
	})
	if err != nil || !grant.GetEnabled() || grant.GetActorId() != "201" || grant.GetOrigin() != "https://netfleet.example.test" {
		t.Fatalf("default active_gateway admission failed: resp=%+v err=%v", grant, err)
	}
	if !verifyGrantEd25519(t, grant.GetGrantToken(), pub, "opl-netfleet-cloud", "201", "device:enroll") {
		t.Fatal("minted JWT failed verification")
	}

	if deniedResp, err := client.IssueServiceGrant(ctx, &api.IssueServiceGrantRpcRequest{
		BearerToken: "gw-customer@example.test",
		Body:        &api.ServiceGrantRequest{Service: "link_relay", Scope: proto.String("console:login")},
	}); status.Code(err) != codes.PermissionDenied || deniedResp != nil {
		t.Fatalf("expected fail-closed allowlist_only denial, got resp=%+v err=%v", deniedResp, err)
	}

	// Seed admin session row in DB for SetServiceEntitlement / ListServiceEntitlements calls.
	adminSession := seedSessionForTest(t, db, gwDB, "103", "admin@example.test")

	// 2. Disable netfleet_cloud for actor 201 -> immediately denied (403 service_not_entitled) with no origin/token.
	_, err = client.SetServiceEntitlement(ctx, &api.SetServiceEntitlementRpcRequest{
		Context: &api.CallContext{
			ActorId: "103", SessionId: proto.String(adminSession),
			Scope:          &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
			RequestId:      "req-disable-nf",
			IdempotencyKey: "idem-disable-nf",
		},
		TargetActorId: "201",
		ServiceId:     "netfleet_cloud",
		Body:          &api.SetServiceEntitlementRequest{ConfirmationAccountId: "201", Enabled: false, Reason: "suspended"},
	})
	if err != nil {
		t.Fatalf("SetServiceEntitlement disable failed: %v", err)
	}
	if deniedResp, err := client.IssueServiceGrant(ctx, &api.IssueServiceGrantRpcRequest{
		Body: &api.ServiceGrantRequest{Email: "customer@example.test", Password: "password", Service: "netfleet_cloud"},
	}); status.Code(err) != codes.PermissionDenied || deniedResp != nil {
		t.Fatalf("expected denial after disable, got resp=%+v err=%v", deniedResp, err)
	}

	// 3. Re-enable with origin_override + custom limits, and verify ListServiceEntitlements projection + audit event.
	limitsPB, _ := structpb.NewStruct(map[string]any{"max_sites": 25})
	future := timestamppb.New(time.Now().Add(time.Hour))
	cmd, err := client.SetServiceEntitlement(ctx, &api.SetServiceEntitlementRpcRequest{
		Context: &api.CallContext{
			ActorId: "103", SessionId: proto.String(adminSession),
			Scope:          &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
			RequestId:      "req-enable-nf",
			IdempotencyKey: "idem-enable-nf",
		},
		TargetActorId: "201",
		ServiceId:     "netfleet_cloud",
		Body: &api.SetServiceEntitlementRequest{
			ConfirmationAccountId: "201",
			Enabled:               true,
			OriginOverride:        proto.String("https://dedicated.example.test"),
			Limits:                limitsPB,
			ExpiresAt:             future,
			Reason:                "vip_dedicated_origin",
		},
	})
	if err != nil || cmd.GetOrigin() != "https://dedicated.example.test" {
		t.Fatalf("SetServiceEntitlement enable override failed: cmd=%+v err=%v", cmd, err)
	}
	overrideGrant, err := client.IssueServiceGrant(ctx, &api.IssueServiceGrantRpcRequest{
		BearerToken: "gw-customer@example.test",
		Body:        &api.ServiceGrantRequest{Service: "netfleet_cloud", Scope: proto.String("console:login")},
	})
	if err != nil || overrideGrant.GetOrigin() != "https://dedicated.example.test" || overrideGrant.GetLimits().AsMap()["max_sites"] != float64(25) {
		t.Fatalf("override grant failed: resp=%+v err=%v", overrideGrant, err)
	}
	listResp, err := client.ListServiceEntitlements(ctx, &api.ListServiceEntitlementsRpcRequest{
		Context: &api.CallContext{
			ActorId: "103", SessionId: proto.String(adminSession),
			Scope:     &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
			RequestId: "req-list-se",
		},
		ActorIds: []string{"201"},
	})
	if err != nil || len(listResp.GetItems()) != 2 {
		t.Fatalf("ListServiceEntitlements failed: resp=%+v err=%v", listResp, err)
	}
	var auditCount int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.audit_events WHERE resource_id='201' AND action LIKE 'account.service_entitlement.%'`).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("expected 2 audit rows, got %d err=%v", auditCount, err)
	}
	_ = bffToken
}

func verifyGrantEd25519(t *testing.T, token string, pub ed25519.PublicKey, wantAud, wantSub, wantScope string) bool {
	t.Helper()
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !ed25519.Verify(pub, []byte(parts[0]+"."+parts[1]), sig) {
		return false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims map[string]any
	if json.Unmarshal(raw, &claims) != nil {
		return false
	}
	return claims["aud"] == wantAud && claims["sub"] == wantSub && claims["scope"] == wantScope
}

func seedSessionForTest(t *testing.T, db, gwDB *sql.DB, actorID, email string) string {
	t.Helper()
	_, err := gwDB.ExecContext(context.Background(), `INSERT INTO gateway.identity_mappings(id,actor_id,sub2api_user_id,external_identity_ref,verified_at) VALUES($1,$2,$3,$4,now())`, "id-"+actorID, actorID, actorID, "sub2api:user:"+actorID)
	if err != nil {
		t.Fatal(err)
	}
	rawCookie := strings.Repeat("a", 64)
	ref := owneridentity.SessionReference(rawCookie)
	_, err = db.ExecContext(context.Background(), `INSERT INTO tenant.sessions(id,session_hash,actor_id,gateway_session_ref,csrf_hash,expires_at,display_name) VALUES($1,$2,$3,$1,$4,now() + interval '1 hour',$5)`, ref, rawCookie, actorID, rawCookie, email)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
