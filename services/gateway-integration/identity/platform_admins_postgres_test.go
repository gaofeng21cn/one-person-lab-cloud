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
	"sort"
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

type directoryUserFixture struct {
	id        int64
	email     string
	status    string
	balance   string
	keyCount  int
	hasUsage  bool
	todayCost string
	totalCost string
}

type platformAdminSystem struct {
	db          *sql.DB
	bffClient   api.TenantProductServiceClient
	authzClient api.CloudIdentityAuthorizationClient
}

func newPlatformAdminSystem(t *testing.T, bootstrapAdmins []string, directoryUsers []directoryUserFixture) platformAdminSystem {
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

	actorsByEmail := map[string]directoryUserFixture{
		"directory@example.test": {id: 900, email: "directory@example.test", status: "active", balance: "0"},
	}
	actorsByID := map[int64]directoryUserFixture{}
	for _, u := range directoryUsers {
		actorsByEmail[u.email] = u
		actorsByID[u.id] = u
	}
	now := time.Date(2026, 11, 28, 1, 0, 0, 0, time.UTC)

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/v1/auth/me":
			const prefix = "Bearer isolated-gateway-"
			authHeader := r.Header.Get("Authorization")
			if !strings.HasPrefix(authHeader, prefix) {
				w.WriteHeader(401)
				return
			}
			email := strings.TrimPrefix(authHeader, prefix)
			user, ok := actorsByEmail[email]
			if !ok {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": identity.GatewayIdentity{ID: user.id, Email: user.email, Status: user.status},
			})
		case r.URL.Path == "/api/v1/auth/login":
			var body struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != "password" {
				w.WriteHeader(401)
				return
			}
			user, ok := actorsByEmail[body.Email]
			if !ok {
				w.WriteHeader(401)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"access_token": "isolated-gateway-" + body.Email,
					"user":         identity.GatewayIdentity{ID: user.id, Email: user.email, Status: user.status},
				},
			})
		case r.URL.Path == "/api/v1/admin/users":
			if r.Header.Get("Authorization") != "Bearer isolated-gateway-directory@example.test" {
				w.WriteHeader(401)
				return
			}
			q := r.URL.Query()
			page, _ := strconv.Atoi(q.Get("page"))
			pageSize, _ := strconv.Atoi(q.Get("page_size"))
			search := strings.ToLower(strings.TrimSpace(q.Get("search")))
			if page < 1 {
				page = 1
			}
			if pageSize < 1 {
				pageSize = 20
			}
			matched := make([]directoryUserFixture, 0, len(directoryUsers))
			for _, u := range directoryUsers {
				if search != "" && !strings.Contains(strings.ToLower(u.email), search) && strconv.FormatInt(u.id, 10) != search {
					continue
				}
				matched = append(matched, u)
			}
			sort.Slice(matched, func(i, j int) bool { return matched[i].id < matched[j].id })
			pages := (len(matched) + pageSize - 1) / pageSize
			if pages < 1 {
				pages = 1
			}
			start := min((page-1)*pageSize, len(matched))
			end := min(start+pageSize, len(matched))
			items := make([]map[string]any, 0, end-start)
			for _, u := range matched[start:end] {
				items = append(items, map[string]any{
					"id":         u.id,
					"email":      u.email,
					"balance":    u.balance,
					"status":     u.status,
					"created_at": now.Format(time.RFC3339Nano),
					"updated_at": now.Format(time.RFC3339Nano),
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": map[string]any{
					"items":     items,
					"total":     len(matched),
					"page":      page,
					"page_size": pageSize,
					"pages":     pages,
				},
			})
		case r.URL.Path == "/api/v1/admin/dashboard/users-usage":
			if r.Header.Get("Authorization") != "Bearer isolated-gateway-directory@example.test" {
				w.WriteHeader(401)
				return
			}
			var body struct {
				UserIDs []int64 `json:"user_ids"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil {
				w.WriteHeader(400)
				return
			}
			stats := map[string]any{}
			for _, id := range body.UserIDs {
				u, ok := actorsByID[id]
				if !ok || !u.hasUsage {
					continue
				}
				stats[strconv.FormatInt(id, 10)] = map[string]any{
					"user_id":           id,
					"today_actual_cost": json.Number(u.todayCost),
					"total_actual_cost": json.Number(u.totalCost),
					"by_platform": []map[string]any{
						{"platform": "openai", "today_actual_cost": json.Number(u.todayCost), "total_actual_cost": json.Number(u.totalCost)},
					},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"stats": stats}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/admin/users/") && strings.HasSuffix(r.URL.Path, "/api-keys"):
			if r.Header.Get("Authorization") != "Bearer isolated-gateway-directory@example.test" {
				w.WriteHeader(401)
				return
			}
			rawID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/"), "/api-keys")
			id, parseErr := strconv.ParseInt(rawID, 10, 64)
			u, ok := actorsByID[id]
			if parseErr != nil || !ok {
				w.WriteHeader(404)
				return
			}
			if u.keyCount < 0 {
				w.WriteHeader(500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []any{}, "total": u.keyCount}})
		case strings.HasPrefix(r.URL.Path, "/api/v1/admin/users/"):
			if r.Header.Get("Authorization") != "Bearer isolated-gateway-directory@example.test" {
				w.WriteHeader(401)
				return
			}
			id, parseErr := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/"), 10, 64)
			u, ok := actorsByID[id]
			if parseErr != nil || !ok {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 0,
				"data": identity.GatewayIdentity{ID: u.id, Email: u.email, Status: u.status},
			})
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(gateway.Close)

	g, err := identity.NewGatewayWithDirectory(gateway.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	svc, err := identity.New(db, g, bytes.Repeat([]byte("a"), 32), bootstrapAdmins, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	svc.GatewayStore = gatewayStoreSystem(t)

	const token = "isolated-platform-admin-bff-token-01"
	config := ownerservice.Config{
		Owner: owneridentity.Tenant,
		TLS:   owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: token},
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

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, token)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return platformAdminSystem{
		db:          db,
		bffClient:   api.NewTenantProductServiceClient(conn),
		authzClient: api.NewCloudIdentityAuthorizationClient(conn),
	}
}

func TestPlatformAdminDelegationAndOperatorAccountsPostgresLifecycle(t *testing.T) {
	users := []directoryUserFixture{
		{id: 103, email: "root@example.test", status: "active", balance: "25.5", keyCount: 2, hasUsage: true, todayCost: "1.25", totalCost: "9.5"},
		{id: 201, email: "delegate@example.test", status: "active", balance: "12.0", keyCount: 1, hasUsage: true, todayCost: "0.5", totalCost: "2.0"},
		{id: 202, email: "gatewayonly@example.test", status: "active", balance: "invalid", keyCount: -1, hasUsage: false},
		{id: 203, email: "disabled@example.test", status: "disabled", balance: "0", keyCount: 0, hasUsage: false},
	}
	sys := newPlatformAdminSystem(t, []string{"103"}, users)
	ctx := t.Context()

	rootCookie, rootSession := memberLogin(t, sys.bffClient, "root@example.test")
	delegateCookie, delegateSession := memberLogin(t, sys.bffClient, "delegate@example.test")
	rootSessionID := owneridentity.SessionReference(rootCookie)
	delegateSessionID := owneridentity.SessionReference(delegateCookie)

	hasAdminPerm := func(s *api.Session) bool {
		for _, p := range s.GetPermissions() {
			if p == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETPLATFORMADMIN {
				return true
			}
		}
		return false
	}
	if !hasAdminPerm(rootSession) || hasAdminPerm(delegateSession) {
		t.Fatalf("unexpected initial permissions root=%v delegate=%v", hasAdminPerm(rootSession), hasAdminPerm(delegateSession))
	}

	// Non-admin cannot list operator accounts or grant platform admin.
	if _, err := sys.bffClient.ListOperatorAccounts(ctx, &api.ListOperatorAccountsRpcRequest{
		Context:  &api.CallContext{ActorId: "201", SessionId: proto.String(delegateSessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-deny-list"},
		Page:     1,
		PageSize: 20,
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for non-admin list, got %v", err)
	}
	if _, err := sys.bffClient.SetPlatformAdmin(ctx, &api.SetPlatformAdminRpcRequest{
		Context:       &api.CallContext{ActorId: "201", SessionId: proto.String(delegateSessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-deny-grant", IdempotencyKey: "k-deny"},
		TargetActorId: "201",
		Body:          &api.SetPlatformAdminRequest{ConfirmationAccountId: "201", Enabled: true, Reason: "self_promote"},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for non-admin grant, got %v", err)
	}

	// Bootstrap admin cannot be modified or revoked.
	if _, err := sys.bffClient.SetPlatformAdmin(ctx, &api.SetPlatformAdminRpcRequest{
		Context:       &api.CallContext{ActorId: "103", SessionId: proto.String(rootSessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-bootstrap-immutable", IdempotencyKey: "k-boot"},
		TargetActorId: "103",
		Body:          &api.SetPlatformAdminRequest{ConfirmationAccountId: "103", Enabled: false, Reason: "remove_root"},
	}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied for bootstrap root modification, got %v", err)
	}

	// Root admin grants delegated admin to 201 + idempotent replay + conflict.
	grantReq := &api.SetPlatformAdminRpcRequest{
		Context:       &api.CallContext{ActorId: "103", SessionId: proto.String(rootSessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-grant-201", IdempotencyKey: "k-grant-201"},
		TargetActorId: "201",
		Body:          &api.SetPlatformAdminRequest{ConfirmationAccountId: "201", Enabled: true, Reason: "promote_operator"},
	}
	grantResp, err := sys.bffClient.SetPlatformAdmin(ctx, grantReq)
	if err != nil || !grantResp.GetPlatformAdminEnabled() || grantResp.GetIsBootstrapAdmin() || grantResp.GetAccountId() != "201" {
		t.Fatalf("grant failed: resp=%+v err=%v", grantResp, err)
	}
	replayResp, err := sys.bffClient.SetPlatformAdmin(ctx, grantReq)
	if err != nil || replayResp.GetOperationId() != grantResp.GetOperationId() {
		t.Fatalf("idempotency replay mismatch: resp=%+v err=%v", replayResp, err)
	}
	conflictReq := proto.Clone(grantReq).(*api.SetPlatformAdminRpcRequest)
	conflictReq.Body.Enabled = false
	if _, err := sys.bffClient.SetPlatformAdmin(ctx, conflictReq); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("expected AlreadyExists on conflict, got %v", err)
	}

	// 201 immediately gets platform_admin permissions on live GetSession.
	liveDelegate, err := sys.bffClient.GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(delegateCookie)}})
	if err != nil || !hasAdminPerm(liveDelegate) {
		t.Fatalf("delegated admin session did not gain platform permissions: err=%v perms=%v", err, liveDelegate.GetPermissions())
	}

	// Self-revocation is blocked even by delegated admin.
	if _, err := sys.bffClient.SetPlatformAdmin(ctx, &api.SetPlatformAdminRpcRequest{
		Context:       &api.CallContext{ActorId: "201", SessionId: proto.String(delegateSessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-self-revoke", IdempotencyKey: "k-self-revoke"},
		TargetActorId: "201",
		Body:          &api.SetPlatformAdminRequest{ConfirmationAccountId: "201", Enabled: false, Reason: "self_revoke"},
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition on self-revocation, got %v", err)
	}

	// ListOperatorAccounts returns ALL Sub2API users (including Gateway-only user 202 and disabled user 203),
	// preserving missing/unavailable balance, usage, and key counts without fabricating zeroes.
	page, err := sys.bffClient.ListOperatorAccounts(ctx, &api.ListOperatorAccountsRpcRequest{
		Context:  &api.CallContext{ActorId: "201", SessionId: proto.String(delegateSessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-list-all"},
		Page:     1,
		PageSize: 20,
	})
	if err != nil || page.GetTotal() != 4 || len(page.GetItems()) != 4 {
		t.Fatalf("unexpected accounts page: page=%+v err=%v", page, err)
	}
	byID := map[string]*api.OperatorGatewayAccount{}
	for _, item := range page.GetItems() {
		byID[item.GetActorId()] = item
	}
	if !byID["103"].GetIsBootstrapAdmin() || !byID["103"].GetPlatformAdminEnabled() || !byID["103"].GetWorkspacePurchaseEnabled() {
		t.Fatalf("unexpected root account state: %+v", byID["103"])
	}
	if byID["201"].GetIsBootstrapAdmin() || !byID["201"].GetPlatformAdminEnabled() || !byID["201"].GetWorkspacePurchaseEnabled() || byID["201"].GetWalletUsdMicros() != 12_000_000 || byID["201"].GetKeyCount() != 1 {
		t.Fatalf("unexpected delegated account state: %+v", byID["201"])
	}
	if byID["202"].GetPlatformAdminEnabled() || byID["202"].GetWorkspacePurchaseEnabled() || byID["202"].GetWalletAvailable() || byID["202"].GetUsageAvailable() || byID["202"].GetKeyCountAvailable() {
		t.Fatalf("gateway-only partial/unavailable facts were corrupted: %+v", byID["202"])
	}
	if byID["203"].GetStatus() != "disabled" {
		t.Fatalf("expected disabled status for 203, got %+v", byID["203"])
	}

	// Search filters Sub2API users directly.
	filtered, err := sys.bffClient.ListOperatorAccounts(ctx, &api.ListOperatorAccountsRpcRequest{
		Context:  &api.CallContext{ActorId: "103", SessionId: proto.String(rootSessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-list-search"},
		Page:     1,
		PageSize: 20,
		Search:   "gatewayonly",
	})
	if err != nil || filtered.GetTotal() != 1 || len(filtered.GetItems()) != 1 || filtered.GetItems()[0].GetActorId() != "202" {
		t.Fatalf("unexpected search page: filtered=%+v err=%v", filtered, err)
	}

	// Revoke delegated admin -> session loses permissions & cannot list operator accounts.
	revokeResp, err := sys.bffClient.SetPlatformAdmin(ctx, &api.SetPlatformAdminRpcRequest{
		Context:       &api.CallContext{ActorId: "103", SessionId: proto.String(rootSessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-revoke-201", IdempotencyKey: "k-revoke-201"},
		TargetActorId: "201",
		Body:          &api.SetPlatformAdminRequest{ConfirmationAccountId: "201", Enabled: false, Reason: "revoke_operator"},
	})
	if err != nil || revokeResp.GetPlatformAdminEnabled() {
		t.Fatalf("revoke failed: resp=%+v err=%v", revokeResp, err)
	}
	liveDelegate, err = sys.bffClient.GetSession(ctx, &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(delegateCookie)}})
	if err != nil || hasAdminPerm(liveDelegate) {
		t.Fatalf("revoked admin kept platform permissions: err=%v perms=%v", err, liveDelegate.GetPermissions())
	}
	var auditCount int
	if err := sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.audit_events WHERE action IN ('account.platform_admin.grant','account.platform_admin.revoke') AND resource_id='201' AND outcome='succeeded'`).Scan(&auditCount); err != nil || auditCount != 2 {
		t.Fatalf("expected 2 audit events for 201, got %d err=%v", auditCount, err)
	}
}

func TestLastPlatformAdminLockoutProtectionPostgres(t *testing.T) {
	users := []directoryUserFixture{
		{id: 201, email: "onlyadmin@example.test", status: "active", balance: "0", keyCount: 0, hasUsage: false},
		{id: 202, email: "other@example.test", status: "active", balance: "0", keyCount: 0, hasUsage: false},
	}
	// Boot with no static bootstrap admins; seed 202 directly as delegated DB admin to execute revoke on 201,
	// or have 202 revoke itself? No, self-revocation is separately refused first.
	// To exercise last-admin transaction lock, let's test BOTH via direct DB state where 202 revokes 201? Wait: if 202 is active admin, remainingAdmins >= 1.
	// Actually the only time remainingAdmins == 0 during a non-self-revoke is if an external operator/cron or disabled admin existed, OR if bootstrap is empty and 202 was revoked concurrently.
	// Wait: let's test both: 1) seed ONLY 201 in tenant.platform_admins, attempt self-revoke -> FailedPrecondition;
	// 2) verify that calling SetPlatformAdmin with empty bootstrap admins keeps an active admin intact and records audit on valid checks.
	// Wait: what if we call SetPlatformAdmin using an accepted DB row where actor 202 calls SetPlatformAdmin to revoke 201 while 202 has enabled=false in DB? Wait - 202 wouldn't pass AuthorizeAction!
	// Wait: the only caller passing AuthorizeAction IS an active admin. If caller != target, caller is ALREADY an active admin, so remainingAdmins >= 1 unless target == caller!
	//Aha! Look at why last-admin protection is STILL crucial when self-revocation of the *last* admin vs non-last admin:
	// Wait: should a delegated admin be allowed to step down themselves IF another admin exists, OR block self-revocation unconditionally?
	// Let's check: keeping self-revocation blocked (only another admin or bootstrap root can revoke a delegated admin) guarantees even stronger lockout prevention, AND the DB check `len(s.admins)+otherDelegated == 0` handles any future non-interactive caller too!
	sys := newPlatformAdminSystem(t, nil, users)
	ctx := t.Context()

	// Seed 201 and 202 directly as initial delegated admins via SQL (migration/bootstrap scenario).
	if _, err := sys.db.ExecContext(ctx, `INSERT INTO tenant.platform_admins(actor_id,enabled,reason,updated_by) VALUES ('201',true,'seed','system')`); err != nil {
		t.Fatal(err)
	}
	cookie, _ := memberLogin(t, sys.bffClient, "onlyadmin@example.test")
	sessionID := owneridentity.SessionReference(cookie)
	if _, err := sys.bffClient.SetPlatformAdmin(ctx, &api.SetPlatformAdminRpcRequest{
		Context:       &api.CallContext{ActorId: "201", SessionId: proto.String(sessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-last-self", IdempotencyKey: "k-last-self"},
		TargetActorId: "201",
		Body:          &api.SetPlatformAdminRequest{ConfirmationAccountId: "201", Enabled: false, Reason: "try_remove_last"},
	}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition preventing last admin lockout, got %v", err)
	}
	var stillEnabled bool
	if err := sys.db.QueryRowContext(ctx, `SELECT enabled FROM tenant.platform_admins WHERE actor_id='201'`).Scan(&stillEnabled); err != nil || !stillEnabled {
		t.Fatalf("expected actor 201 to remain enabled, got %v err=%v", stillEnabled, err)
	}

	// Concurrent mutual revocation between two delegated admins leaves at least one active admin.
	if _, err := sys.db.ExecContext(ctx, `INSERT INTO tenant.platform_admins(actor_id,enabled,reason,updated_by) VALUES ('202',true,'seed','system') ON CONFLICT (actor_id) DO UPDATE SET enabled=true`); err != nil {
		t.Fatal(err)
	}
	otherCookie, _ := memberLogin(t, sys.bffClient, "other@example.test")
	otherSessionID := owneridentity.SessionReference(otherCookie)
	errs := make(chan error, 2)
	go func() {
		_, err := sys.bffClient.SetPlatformAdmin(ctx, &api.SetPlatformAdminRpcRequest{
			Context:       &api.CallContext{ActorId: "201", SessionId: proto.String(sessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-mutual-1", IdempotencyKey: "k-mutual-1"},
			TargetActorId: "202",
			Body:          &api.SetPlatformAdminRequest{ConfirmationAccountId: "202", Enabled: false, Reason: "mutual_revoke_1"},
		})
		errs <- err
	}()
	go func() {
		_, err := sys.bffClient.SetPlatformAdmin(ctx, &api.SetPlatformAdminRpcRequest{
			Context:       &api.CallContext{ActorId: "202", SessionId: proto.String(otherSessionID), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, RequestId: "req-mutual-2", IdempotencyKey: "k-mutual-2"},
			TargetActorId: "201",
			Body:          &api.SetPlatformAdminRequest{ConfirmationAccountId: "201", Enabled: false, Reason: "mutual_revoke_2"},
		})
		errs <- err
	}()
	err1, err2 := <-errs, <-errs
	if (err1 == nil) == (err2 == nil) {
		t.Fatalf("expected exactly one mutual revocation to succeed and one to fail, got err1=%v err2=%v", err1, err2)
	}
	var remaining int
	if err := sys.db.QueryRowContext(ctx, `SELECT count(*) FROM tenant.platform_admins WHERE enabled=true`).Scan(&remaining); err != nil || remaining != 1 {
		t.Fatalf("expected exactly 1 remaining active platform admin, got %d err=%v", remaining, err)
	}
}
