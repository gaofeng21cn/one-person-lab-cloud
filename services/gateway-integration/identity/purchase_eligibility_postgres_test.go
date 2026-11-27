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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

const (
	purchaseEligibilityBFFToken       = "isolated-purchase-elig-bff-token-0001"
	purchaseEligibilityWorkspaceToken = "isolated-purchase-elig-ws-token-00001"
)

type purchaseEligibilitySystem struct {
	db        *sql.DB
	bffClient api.TenantProductServiceClient
	wsAuth    api.CloudIdentityAuthorizationClient
	bffAuth   api.CloudIdentityAuthorizationClient
}

func newPurchaseEligibilitySystem(t *testing.T, disabledSubjects map[int64]bool) purchaseEligibilitySystem {
	t.Helper()
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

	actors := map[string]int64{
		"admin@example.test":     103,
		"customer@example.test":  201,
		"customer2@example.test": 202,
		"directory@example.test": 900,
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
			actor, ok := actors[body.Email]
			if !ok {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"access_token": "isolated-gateway-" + body.Email,
					"user":         identity.GatewayIdentity{ID: actor, Email: body.Email, Status: "active"},
				},
			})
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/v1/admin/users/") {
			if r.Header.Get("Authorization") != "Bearer isolated-gateway-directory@example.test" {
				w.WriteHeader(401)
				return
			}
			id, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/"), 10, 64)
			if err != nil {
				w.WriteHeader(404)
				return
			}
			for email, candidate := range actors {
				if candidate == id && email != "directory@example.test" {
					state := "active"
					if disabledSubjects[id] {
						state = "disabled"
					}
					_ = json.NewEncoder(w).Encode(map[string]any{
						"code": 0,
						"data": identity.GatewayIdentity{ID: id, Email: email, Status: state},
					})
					return
				}
			}
			w.WriteHeader(404)
			return
		}
		w.WriteHeader(404)
	}))
	t.Cleanup(gateway.Close)

	g, err := identity.NewGatewayWithDirectory(gateway.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := identity.New(db, g, bytes.Repeat([]byte("e"), 32), []string{"103"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	svc.GatewayStore = gatewayStoreSystem(t)

	config := ownerservice.Config{
		Owner: owneridentity.Tenant,
		TLS:   owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers: map[owneridentity.Service]string{
			owneridentity.ConsoleBFF:          purchaseEligibilityBFFToken,
			owneridentity.Workspace.Service(): purchaseEligibilityWorkspaceToken,
		},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.Register(server); err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)

	bffConn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, purchaseEligibilityBFFToken)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { bffConn.Close() })
	wsConn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.Workspace.Service(), purchaseEligibilityWorkspaceToken)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { wsConn.Close() })

	return purchaseEligibilitySystem{
		db:        db,
		bffClient: api.NewTenantProductServiceClient(bffConn),
		bffAuth:   api.NewCloudIdentityAuthorizationClient(bffConn),
		wsAuth:    api.NewCloudIdentityAuthorizationClient(wsConn),
	}
}

func TestWorkspacePurchaseEligibilityPostgresLifecycle(t *testing.T) {
	disabled := map[int64]bool{}
	sys := newPurchaseEligibilitySystem(t, disabled)
	ctx := t.Context()

	adminCookie, _ := memberLogin(t, sys.bffClient, "admin@example.test")
	customerCookie, _ := memberLogin(t, sys.bffClient, "customer@example.test")
	adminSession := owneridentity.SessionReference(adminCookie)
	customerSession := owneridentity.SessionReference(customerCookie)

	// 1. Regular customer fails closed (false) with no policy row; platform admin defaults eligible.
	custRead, err := sys.wsAuth.ReadWorkspacePurchaseEligibility(ctx, &api.ReadWorkspacePurchaseEligibilityRequest{
		Context: &api.CallContext{ActorId: "201", SessionId: proto.String(customerSession), RequestId: "req-read-1"},
	})
	if err != nil {
		t.Fatalf("customer read failed: %v", err)
	}
	if custRead.GetActorId() != "201" || custRead.GetAccountId() != "201" || custRead.GetWorkspacePurchaseEnabled() {
		t.Fatalf("expected fail-closed false for customer, got %+v", custRead)
	}
	adminRead, err := sys.wsAuth.ReadWorkspacePurchaseEligibility(ctx, &api.ReadWorkspacePurchaseEligibilityRequest{
		Context: &api.CallContext{ActorId: "103", SessionId: proto.String(adminSession), RequestId: "req-read-admin"},
	})
	if err != nil || !adminRead.GetWorkspacePurchaseEnabled() {
		t.Fatalf("expected platform admin default true, got %+v err=%v", adminRead, err)
	}

	// Non-Workspace peer cannot call ReadWorkspacePurchaseEligibility.
	if _, err := sys.bffAuth.ReadWorkspacePurchaseEligibility(ctx, &api.ReadWorkspacePurchaseEligibilityRequest{
		Context: &api.CallContext{ActorId: "201", SessionId: proto.String(customerSession), RequestId: "req-wrong-peer"},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for non-Workspace peer, got %v", err)
	}

	// Actor mismatch on read is rejected.
	if _, err := sys.wsAuth.ReadWorkspacePurchaseEligibility(ctx, &api.ReadWorkspacePurchaseEligibilityRequest{
		Context: &api.CallContext{ActorId: "103", SessionId: proto.String(customerSession), RequestId: "req-mismatch"},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for actor mismatch, got %v", err)
	}

	// 2. Non-admin customer cannot mutate eligibility.
	_, err = sys.bffClient.SetWorkspacePurchaseEligibility(ctx, &api.SetWorkspacePurchaseEligibilityRpcRequest{
		Context: &api.CallContext{
			ActorId: "201", SessionId: proto.String(customerSession),
			Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
			RequestId: "req-deny-customer", IdempotencyKey: "key-deny-1",
		},
		TargetActorId: "201",
		Body:          &api.SetWorkspacePurchaseEligibilityRequest{ConfirmationAccountId: "201", Enabled: true, Reason: "self_grant"},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for non-admin write, got %v", err)
	}

	// 3. Platform admin cannot target a reserved platform admin or nonexistent Gateway subject.
	_, err = sys.bffClient.SetWorkspacePurchaseEligibility(ctx, &api.SetWorkspacePurchaseEligibilityRpcRequest{
		Context: &api.CallContext{
			ActorId: "103", SessionId: proto.String(adminSession),
			Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
			RequestId: "req-deny-admin-target", IdempotencyKey: "key-deny-admin",
		},
		TargetActorId: "103",
		Body:          &api.SetWorkspacePurchaseEligibilityRequest{ConfirmationAccountId: "103", Enabled: false, Reason: "operator_requested"},
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for reserved admin target, got %v", err)
	}
	_, err = sys.bffClient.SetWorkspacePurchaseEligibility(ctx, &api.SetWorkspacePurchaseEligibilityRpcRequest{
		Context: &api.CallContext{
			ActorId: "103", SessionId: proto.String(adminSession),
			Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
			RequestId: "req-unknown-subject", IdempotencyKey: "key-unknown-subject",
		},
		TargetActorId: "99999",
		Body:          &api.SetWorkspacePurchaseEligibilityRequest{ConfirmationAccountId: "99999", Enabled: true, Reason: "operator_requested"},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent Gateway subject")
	}

	// 4. Platform admin grants eligibility, replay with same key returns same receipt, conflict rejects.
	grantReq := &api.SetWorkspacePurchaseEligibilityRpcRequest{
		Context: &api.CallContext{
			ActorId: "103", SessionId: proto.String(adminSession),
			Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
			RequestId: "req-grant-1", IdempotencyKey: "idem-grant-201",
		},
		TargetActorId: "201",
		Body:          &api.SetWorkspacePurchaseEligibilityRequest{ConfirmationAccountId: "201", Enabled: true, Reason: "operator_requested"},
	}
	grantResp, err := sys.bffClient.SetWorkspacePurchaseEligibility(ctx, grantReq)
	if err != nil {
		t.Fatalf("grant failed: %v", err)
	}
	if grantResp.GetAccountId() != "201" || grantResp.GetStatus() != "succeeded" || !grantResp.GetWorkspacePurchaseEnabled() || grantResp.GetOperationId() == "" {
		t.Fatalf("unexpected grant receipt: %+v", grantResp)
	}
	replayResp, err := sys.bffClient.SetWorkspacePurchaseEligibility(ctx, grantReq)
	if err != nil || replayResp.GetOperationId() != grantResp.GetOperationId() {
		t.Fatalf("idempotent replay mismatch: resp=%+v err=%v", replayResp, err)
	}
	conflictReq := proto.Clone(grantReq).(*api.SetWorkspacePurchaseEligibilityRpcRequest)
	conflictReq.Body.Enabled = false
	if _, err := sys.bffClient.SetWorkspacePurchaseEligibility(ctx, conflictReq); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected AlreadyExists on idempotency conflict, got %v", err)
	}

	// Workspace readback immediately sees granted=true.
	custRead, err = sys.wsAuth.ReadWorkspacePurchaseEligibility(ctx, &api.ReadWorkspacePurchaseEligibilityRequest{
		Context: &api.CallContext{ActorId: "201", SessionId: proto.String(customerSession), RequestId: "req-read-2"},
	})
	if err != nil || !custRead.GetWorkspacePurchaseEnabled() {
		t.Fatalf("expected true after grant, got %+v err=%v", custRead, err)
	}

	// Batch readback returns authoritative states for granted customer, untouched customer, and admin.
	listResp, err := sys.bffClient.ListWorkspacePurchaseEligibility(ctx, &api.ListWorkspacePurchaseEligibilityRpcRequest{
		Context: &api.CallContext{
			ActorId: "103", SessionId: proto.String(adminSession),
			Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
			RequestId: "req-list-1",
		},
		ActorIds: []string{"201", "202", "103"},
	})
	if err != nil || len(listResp.GetItems()) != 3 {
		t.Fatalf("list failed: resp=%+v err=%v", listResp, err)
	}
	if !listResp.Items[0].GetWorkspacePurchaseEnabled() || listResp.Items[1].GetWorkspacePurchaseEnabled() || !listResp.Items[2].GetWorkspacePurchaseEnabled() {
		t.Fatalf("unexpected batch eligibility projection: %+v", listResp.Items)
	}

	// Audit row was written atomically.
	var auditCount int
	if err := sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.audit_events WHERE action='account.workspace_purchase.grant' AND resource_id='201'`).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("expected 1 grant audit event, got %d err=%v", auditCount, err)
	}

	// 5. Revoke eligibility -> Workspace readback immediately returns false.
	revokeResp, err := sys.bffClient.SetWorkspacePurchaseEligibility(ctx, &api.SetWorkspacePurchaseEligibilityRpcRequest{
		Context: &api.CallContext{
			ActorId: "103", SessionId: proto.String(adminSession),
			Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
			RequestId: "req-revoke-1", IdempotencyKey: "idem-revoke-201",
		},
		TargetActorId: "201",
		Body:          &api.SetWorkspacePurchaseEligibilityRequest{ConfirmationAccountId: "201", Enabled: false, Reason: "policy_revoked"},
	})
	if err != nil || revokeResp.GetWorkspacePurchaseEnabled() {
		t.Fatalf("revoke failed: resp=%+v err=%v", revokeResp, err)
	}
	custRead, err = sys.wsAuth.ReadWorkspacePurchaseEligibility(ctx, &api.ReadWorkspacePurchaseEligibilityRequest{
		Context: &api.CallContext{ActorId: "201", SessionId: proto.String(customerSession), RequestId: "req-read-3"},
	})
	if err != nil || custRead.GetWorkspacePurchaseEnabled() {
		t.Fatalf("expected false after revoke, got %+v err=%v", custRead, err)
	}

	// 6. Disabled Gateway subject is rejected on read.
	disabled[201] = true
	if _, err := sys.wsAuth.ReadWorkspacePurchaseEligibility(ctx, &api.ReadWorkspacePurchaseEligibilityRequest{
		Context: &api.CallContext{ActorId: "201", SessionId: proto.String(customerSession), RequestId: "req-read-disabled"},
	}); status.Code(err) != codes.PermissionDenied && status.Code(err) != codes.Unauthenticated {
		t.Fatalf("expected denial for disabled subject, got %v", err)
	}
}
