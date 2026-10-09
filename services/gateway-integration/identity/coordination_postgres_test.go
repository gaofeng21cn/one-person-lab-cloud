package identity_test

import (
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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/gatewaymigrations"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// gatewayWalletStub is a minimal Sub2API admin server: it authenticates the
// directory identity, applies one balance change per business code, and answers
// native balance history. It counts balance POSTs so a test can prove a lost
// response is not repeated.
type gatewayWalletStub struct {
	mu          sync.Mutex
	adjustments int
	applied     int
	failBalance bool
	// applyThenFail applies the native adjustment and then drops the response, so
	// the caller experiences ACK loss on an effect the wallet actually committed.
	applyThenFail bool
	// refuseBalance answers the balance POST with a definite 4xx refusal without
	// touching the wallet, exactly like a pre-dispatch rejection.
	refuseBalance bool
	entries       map[string]json.Number
}

func (s *gatewayWalletStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "admin-token", "user": map[string]any{"id": 101, "email": "admin@example.test", "status": "active"}}})
	})
	mux.HandleFunc("/api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": 101, "email": "admin@example.test", "status": "active"}})
	})
	mux.HandleFunc("/api/v1/admin/users/77", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": 77, "email": "billing@example.test", "status": "active"}})
	})
	mux.HandleFunc("/api/v1/admin/users/77/balance", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.adjustments++
		fail, applyThenFail, refuse := s.failBalance, s.applyThenFail, s.refuseBalance
		s.mu.Unlock()
		if refuse {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		var body struct {
			Balance   json.Number `json:"balance"`
			Operation string      `json:"operation"`
			Notes     string      `json:"notes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		code := strings.TrimPrefix(body.Notes, "OPL Cloud balance adjustment: ")
		// The native wallet records the signed applied delta: a subtract is a
		// negative history value and an add is positive, exactly as Sub2API does.
		value := body.Balance
		if body.Operation == "subtract" {
			value = json.Number("-" + value.String())
		}
		s.mu.Lock()
		if s.entries == nil {
			s.entries = map[string]json.Number{}
		}
		s.entries[code] = value
		s.applied++
		s.mu.Unlock()
		if applyThenFail {
			http.Error(w, "connection lost after commit", http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"id": 77}})
	})
	mux.HandleFunc("/api/v1/admin/users/77/balance-history", func(w http.ResponseWriter, r *http.Request) {
		recordType := r.URL.Query().Get("type")
		s.mu.Lock()
		items := []map[string]any{}
		for code, value := range s.entries {
			if recordType == "admin_balance" {
				items = append(items, map[string]any{"code": "adj-" + code, "notes": "OPL Cloud balance adjustment: " + code, "type": "admin_balance", "value": value, "status": "used", "used_by": 77, "used_at": "2026-09-30T00:00:00Z", "created_at": "2026-09-30T00:00:00Z"})
			} else {
				items = append(items, map[string]any{"code": code, "type": "balance", "value": value, "balance_applied_value": value, "status": "used", "used_by": 77, "used_at": "2026-09-30T00:00:00Z", "created_at": "2026-09-30T00:00:00Z"})
			}
		}
		s.mu.Unlock()
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": items, "total": len(items), "page": 1, "page_size": 100, "pages": 1}})
	})
	return mux
}

// coordinationCommit is a stub Workspace owner-commit readback so the original
// grant can be resolved without a second live Workspace process.
type coordinationCommit struct{ evidence *api.OwnerCommitEvidence }

func (c coordinationCommit) ReadOwnerCommit(context.Context, *api.ReadOwnerCommitRequest, ...grpc.CallOption) (*api.OwnerCommitEvidence, error) {
	return proto.Clone(c.evidence).(*api.OwnerCommitEvidence), nil
}

func coordinationSystem(t *testing.T, stub *gatewayWalletStub) (*identity.Service, *sql.DB, string, api.GatewayCoordinationClient, api.GatewayCoordinationClient) {
	t.Helper()
	ctx := t.Context()
	admin := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)

	tenant, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: admin, Owner: "tenant", Database: "opl_tenant", SchemaOwnerRole: "opl_tenant_owner", WriterRole: "opl_tenant_writer", RuntimeRole: "opl_tenant_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tenant.Close(context.Background()) })
	tenantSource, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = tenant.Install(ctx, tenant.OwnerDSN, tenant.DatabaseName(), tenantSource); err != nil {
		t.Fatal(err)
	}
	tenantDB, err := tenant.Open(ctx, tenant.RuntimeDSN, tenant.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tenantDB.Close() })

	issued := time.Now().Add(-time.Minute).UTC()
	expires := time.Now().Add(time.Hour).UTC()
	for _, stmt := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO tenant.tenants(id,name)VALUES('tenant-coord','Coord')`, nil},
		{`INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role)VALUES('member-coord','tenant-coord','101','admin')`, nil},
	} {
		if _, err = tenantDB.ExecContext(ctx, stmt.sql, stmt.args...); err != nil {
			t.Fatal(err)
		}
	}

	gateway, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: admin, Owner: "gateway", Database: "opl_gateway", SchemaOwnerRole: "opl_gateway_owner", WriterRole: "opl_gateway_writer", RuntimeRole: "opl_gateway_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gateway.Close(context.Background()) })
	gatewaySource, err := gatewaymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = gateway.Install(ctx, gateway.OwnerDSN, gateway.DatabaseName(), gatewaySource); err != nil {
		t.Fatal(err)
	}
	gatewayDB, err := gateway.Open(ctx, gateway.RuntimeDSN, gateway.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { gatewayDB.Close() })

	sub2api := httptest.NewServer(stub.handler())
	t.Cleanup(sub2api.Close)
	g, err := identity.NewGatewayWithDirectory(sub2api.URL, &identity.GatewayDirectory{Email: "admin@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	s, err := identity.New(tenantDB, g, []byte(strings.Repeat("s", 32)), []string{"101"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store, err := identity.NewGatewayStore(gatewayDB)
	if err != nil {
		t.Fatal(err)
	}
	s.GatewayStore = store
	s.WorkspaceCommit = coordinationCommit{evidence: &api.OwnerCommitEvidence{
		Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: "op-1", ResourceId: "ws-1", AuthorizationContextId: "ctx-1",
		ActorId: "101", Scope: tenantScope("tenant-coord"), AcceptedAction: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE,
		AcceptedAt: timestamppb.New(issued.Add(time.Second)), AcceptedInputDigest: "sha256:" + strings.Repeat("a", 64), CommittedVersion: 1,
		AuthorizationResource: workspaceResource("ws-1"),
	}}

	const consoleToken = "isolated-gateway-console-bff-token-0001"
	const workspaceToken = "isolated-gateway-workspace-token-00001"
	config := ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: consoleToken, owneridentity.Workspace.Service(): workspaceToken}}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Register(server); err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)

	consoleConn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, consoleToken)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { consoleConn.Close() })
	workspaceConn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.Workspace.Service(), workspaceToken)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { workspaceConn.Close() })

	// Log in through the owner's own browser path so the settlement authorization
	// runs against a live session, not a fabricated one.
	var header metadata.MD
	if _, err = api.NewTenantProductServiceClient(consoleConn).GetLoginContext(ctx, &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	challenge := header.Get(owneridentity.SessionCookieHeader)[0]
	session, err := api.NewTenantProductServiceClient(consoleConn).Login(ctx, &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: &api.LoginRequest{Username: "admin@example.test", Password: "password"}}, grpc.Header(&header))
	if err != nil {
		t.Fatal(err)
	}
	if session.ActorId != "101" {
		t.Fatalf("unexpected session actor %s", session.ActorId)
	}
	cookie := header.Get(owneridentity.SessionCookieHeader)[0]
	// The original workspace acceptance context is bound to that session cookie.
	if _, err = tenantDB.ExecContext(ctx, `INSERT INTO tenant.authorization_contexts(id,scope_type,tenant_id,actor_id,session_id,permission_version,audience_owner,action,resource_kind,resource_id,issuer,issued_at,expires_at,accepted_operation_grant_id)
		VALUES('ctx-1','tenant','tenant-coord','101',$1,1,'workspace','CREATEWORKSPACE','workspace','ws-1','cloud_identity',$2,$3,NULL)`,
		owneridentity.SessionReference(cookie), issued, expires); err != nil {
		t.Fatal(err)
	}
	return s, tenantDB, cookie, api.NewGatewayCoordinationClient(consoleConn), api.NewGatewayCoordinationClient(workspaceConn)
}

// TestWalletDebitOnceAndReadback proves the gateway owner records one original
// charge, replays the same identity on a repeat, rejects a changed amount, and
// reads back by both operation id and original code.
func TestWalletDebitOnceAndReadback(t *testing.T) {
	stub := &gatewayWalletStub{}
	_, tenantDB, cookie, console, workspace := coordinationSystem(t, stub)
	issueGrant(t, tenantDB, "grant-workspace", "op-1", "ws-1", "createworkspace")

	binding, err := console.BindWallet(t.Context(), &api.WalletBindingCommand{Context: platformCaller("bind", cookie), TargetTenantId: "tenant-coord", BillingSub2ApiUserId: "77", AuthorizationReceiptId: "auth-receipt-1"})
	if err != nil {
		t.Fatalf("bind wallet: %v", err)
	}
	if binding.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || binding.GetBindingVersion() != 1 {
		t.Fatalf("unexpected binding: %v", binding)
	}

	command := &api.WalletDebitCommand{Context: grantedCaller("charge", "grant-workspace"), WorkspaceId: "ws-1", ObligationId: "op-1", QuoteAcceptanceId: "qa-1", AmountUsdMicros: 5_250_000, Currency: "USD"}
	first, err := workspace.Debit(t.Context(), command)
	if err != nil {
		t.Fatalf("debit: %v", err)
	}
	if first.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED || first.GetReceiptId() == "" || first.GetAmountUsdMicros() != 5_250_000 {
		t.Fatalf("unexpected confirmed charge: %v", first)
	}
	replay, err := workspace.Debit(t.Context(), command)
	if err != nil {
		t.Fatalf("replay debit: %v", err)
	}
	if replay.GetId() != first.GetId() || stub.adjustments != 1 {
		t.Fatalf("charge was not idempotent: id=%s/%s adjustments=%d", first.GetId(), replay.GetId(), stub.adjustments)
	}
	changed := proto.Clone(command).(*api.WalletDebitCommand)
	changed.AmountUsdMicros = 1
	if _, err := workspace.Debit(t.Context(), changed); err == nil {
		t.Fatal("a changed amount reused the original obligation")
	}
	byKey, err := workspace.ReadWalletAction(t.Context(), &api.WalletReadbackRequest{Context: grantedCaller("read", "grant-workspace"), OriginalIdempotencyKey: command.GetContext().GetIdempotencyKey()})
	if err != nil || byKey.GetId() != first.GetId() || byKey.GetReceiptId() != first.GetReceiptId() {
		t.Fatalf("read by original code failed: %v %v", byKey, err)
	}
	byID, err := workspace.ReadWalletAction(t.Context(), &api.WalletReadbackRequest{Context: grantedCaller("read2", "grant-workspace"), WalletOperationId: first.GetId()})
	if err != nil || byID.GetId() != first.GetId() {
		t.Fatalf("read by id failed: %v %v", byID, err)
	}
}

// TestWalletDebitUnknownNotRepeated proves a dispatched-but-unconfirmed charge is
// never re-issued: the operation stays non-terminal and a later call reads it back
// without another balance POST.
func TestWalletDebitUnknownNotRepeated(t *testing.T) {
	stub := &gatewayWalletStub{failBalance: true}
	_, tenantDB, cookie, console, workspace := coordinationSystem(t, stub)
	issueGrant(t, tenantDB, "grant-workspace", "op-1", "ws-1", "createworkspace")
	if _, err := console.BindWallet(t.Context(), &api.WalletBindingCommand{Context: platformCaller("bind", cookie), TargetTenantId: "tenant-coord", BillingSub2ApiUserId: "77", AuthorizationReceiptId: "auth-receipt-1"}); err != nil {
		t.Fatalf("bind wallet: %v", err)
	}
	command := &api.WalletDebitCommand{Context: grantedCaller("charge", "grant-workspace"), WorkspaceId: "ws-1", ObligationId: "op-1", QuoteAcceptanceId: "qa-1", AmountUsdMicros: 1_000_000, Currency: "USD"}
	out, err := workspace.Debit(t.Context(), command)
	if err != nil {
		t.Fatalf("debit: %v", err)
	}
	if out.GetStatus() == api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED || out.GetReceiptId() != "" {
		t.Fatalf("unconfirmed charge was reported confirmed: %v", out)
	}
	if _, err := workspace.Debit(t.Context(), command); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if stub.adjustments != 1 {
		t.Fatalf("unknown charge was re-issued: adjustments=%d", stub.adjustments)
	}
}

// TestWalletRejectsWrongOwner proves a wallet operation is not readable outside
// its tenant: the same operation read with another tenant scope is refused.
func TestWalletRejectsWrongOwner(t *testing.T) {
	stub := &gatewayWalletStub{}
	_, tenantDB, cookie, console, workspace := coordinationSystem(t, stub)
	issueGrant(t, tenantDB, "grant-workspace", "op-1", "ws-1", "createworkspace")
	if _, err := console.BindWallet(t.Context(), &api.WalletBindingCommand{Context: platformCaller("bind", cookie), TargetTenantId: "tenant-coord", BillingSub2ApiUserId: "77", AuthorizationReceiptId: "auth-receipt-1"}); err != nil {
		t.Fatalf("bind wallet: %v", err)
	}
	if _, err := workspace.Debit(t.Context(), &api.WalletDebitCommand{Context: grantedCaller("charge", "grant-workspace"), WorkspaceId: "ws-1", ObligationId: "op-1", QuoteAcceptanceId: "qa-1", AmountUsdMicros: 1_000_000, Currency: "USD"}); err != nil {
		t.Fatalf("debit: %v", err)
	}
	wrong := grantedCaller("read-wrong", "grant-workspace")
	wrong.Scope = tenantScope("tenant-other")
	if _, err := workspace.ReadWalletAction(t.Context(), &api.WalletReadbackRequest{Context: wrong, OriginalIdempotencyKey: "opl-test:charge"}); err == nil {
		t.Fatal("a wallet operation was readable across tenants")
	}
}

func issueGrant(t *testing.T, db *sql.DB, grantID, operationID, workspaceID, action string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `INSERT INTO tenant.accepted_operation_grants
		(id,scope_type,tenant_id,actor_id,accepted_operation_owner,accepted_operation_id,accepted_action,resource_id,accepted_permission_version,allowed_actions,issued_at,mode)
		VALUES($1,'tenant','tenant-coord','101','workspace',$2,$3,$4,1,$5,$6,'continue_original')`,
		grantID, operationID, strings.ToUpper(action), workspaceID,
		[]string{"CHARGEACCEPTEDOBLIGATION", "READWALLETACTION", "REFUNDCONFIRMEDDELETION", "BINDMANAGEDSECRET"},
		time.Now().Add(-time.Minute).UTC()); err != nil {
		t.Fatal(err)
	}
}

func tenantScope(tenant string) *api.AuthorizationScope {
	return &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}
}

func platformScope() *api.AuthorizationScope {
	return &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
}

func workspaceResource(id string) *api.AuthorizationResource {
	return &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(id)}
}

func platformCaller(step, cookie string) *api.CallContext {
	session := owneridentity.SessionReference(cookie)
	return &api.CallContext{RequestId: "req-" + step, IdempotencyKey: "opl-test:" + step, ActorId: "101", SessionId: &session, Scope: platformScope()}
}

func grantedCaller(step, grant string) *api.CallContext {
	return &api.CallContext{RequestId: "req-" + step, IdempotencyKey: "opl-test:" + step, ActorId: "101", AcceptedOperationGrantId: proto.String(grant), Scope: tenantScope("tenant-coord")}
}
