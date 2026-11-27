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
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/gatewaymigrations"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

const provisioningBFTToken = "isolated-provisioning-bff-token-00001"

// provisioningGateway is the simulated Sub2API boundary. It records how often
// the account write was dispatched, so a test can prove the owner never sends
// it twice.
type provisioningGateway struct {
	mu sync.Mutex
	// users maps the account email to its Gateway subject.
	users map[string]int64
	// nextID is the subject the next created account receives.
	nextID int64
	// createCalls counts the dispatched account writes.
	createCalls int
	// listCalls counts the directory account reads.
	listCalls int
	// failCreate makes the account write unavailable.
	failCreate bool
	// failNewUserRead makes the directory identity read of a newly created
	// account unavailable.
	failNewUserRead bool
}

// provisioningNewSubjectBase is the first subject the simulated Gateway hands
// to a newly created account. The operator and the customer keep their seeded
// subjects below it, so a test can fail exactly the reads of the accounts this
// command creates.
const provisioningNewSubjectBase = 5000

func newProvisioningGateway() *provisioningGateway {
	return &provisioningGateway{users: map[string]int64{}, nextID: provisioningNewSubjectBase}
}

func (g *provisioningGateway) subject(email string) int64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	if id, ok := g.users[email]; ok {
		return id
	}
	g.nextID++
	g.users[email] = g.nextID
	return g.nextID
}

func (g *provisioningGateway) emailOf(id int64) string {
	g.mu.Lock()
	defer g.mu.Unlock()
	for email, candidate := range g.users {
		if candidate == id {
			return email
		}
	}
	return ""
}

func (g *provisioningGateway) counts() (creates, lists int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.createCalls, g.listCalls
}

func (g *provisioningGateway) handler() http.Handler {
	envelope := func(w http.ResponseWriter, payload map[string]any) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": payload})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["password"] != "password" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		email := strings.ToLower(strings.TrimSpace(body["email"]))
		id := g.subject(email)
		envelope(w, map[string]any{"access_token": "token-" + email, "expires_in": 3600,
			"user": map[string]any{"id": id, "email": email, "status": "active"}})
	})
	mux.HandleFunc("GET /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.listCalls++
		g.mu.Unlock()
		search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		if pageSize < 1 {
			pageSize = 100
		}
		g.mu.Lock()
		matches := make([]map[string]any, 0, 1)
		for email, id := range g.users {
			if search != "" && !strings.Contains(strings.ToLower(email), search) {
				continue
			}
			matches = append(matches, map[string]any{"id": id, "email": email, "status": "active"})
		}
		g.mu.Unlock()
		sort.Slice(matches, func(i, j int) bool { return matches[i]["id"].(int64) < matches[j]["id"].(int64) })
		total := int64(len(matches))
		pages := int((total + int64(pageSize) - 1) / int64(pageSize))
		if pages < 1 {
			pages = 1
		}
		start := (page - 1) * pageSize
		if start > len(matches) {
			start = len(matches)
		}
		end := start + pageSize
		if end > len(matches) {
			end = len(matches)
		}
		envelope(w, map[string]any{"items": matches[start:end], "total": total, "page": page, "page_size": pageSize, "pages": pages})
	})
	mux.HandleFunc("POST /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.createCalls++
		failed := g.failCreate
		g.mu.Unlock()
		if failed {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		email := strings.ToLower(strings.TrimSpace(body["email"]))
		if email == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id := g.subject(email)
		envelope(w, map[string]any{"id": id, "email": email, "status": "active"})
	})
	mux.HandleFunc("GET /api/v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		g.mu.Lock()
		failed := g.failNewUserRead && id >= provisioningNewSubjectBase
		g.mu.Unlock()
		if failed {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		envelope(w, map[string]any{"id": id, "email": g.emailOf(id), "balance": "0", "status": "active"})
	})
	mux.HandleFunc("GET /api/v1/admin/users/{id}/api-keys", func(w http.ResponseWriter, r *http.Request) {
		envelope(w, map[string]any{"items": []map[string]any{}})
	})
	mux.HandleFunc("POST /api/v1/admin/dashboard/users-usage", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			UserIDs []int64 `json:"user_ids"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		stats := map[string]any{}
		for _, id := range body.UserIDs {
			stats[strconv.FormatInt(id, 10)] = map[string]any{
				"user_id": id, "today_actual_cost": "0", "total_actual_cost": "0", "by_platform": []any{},
			}
		}
		envelope(w, map[string]any{"stats": stats})
	})
	return mux
}

// provisioningSystem is the joined fixture: the real owner over typed gRPC, a
// real PostgreSQL Tenant and Gateway store, and the simulated Gateway boundary.
type provisioningSystem struct {
	tenantDB  *sql.DB
	gatewayDB *sql.DB
	client    api.TenantProductServiceClient
	gateway   *provisioningGateway
}

func newProvisioningSystem(t *testing.T) *provisioningSystem {
	t.Helper()
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)

	tenant, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN: dsn, Owner: "tenant", Database: "opl_tenant",
		SchemaOwnerRole: "opl_tenant_owner", WriterRole: "opl_tenant_writer", RuntimeRole: "opl_tenant_runtime",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tenant.Close(context.Background()) })
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = tenant.Install(ctx, tenant.OwnerDSN, tenant.DatabaseName(), source); err != nil {
		t.Fatal(err)
	}
	tenantDB, err := tenant.Open(ctx, tenant.RuntimeDSN, tenant.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tenantDB.Close() })

	gateway, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN: dsn, Owner: "gateway", Database: "opl_gateway",
		SchemaOwnerRole: "opl_gateway_owner", WriterRole: "opl_gateway_writer", RuntimeRole: "opl_gateway_runtime",
	})
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

	upstream := &provisioningGateway{users: map[string]int64{}, nextID: provisioningNewSubjectBase}
	upstream.users["admin@example.test"] = 101
	upstream.users["customer@example.test"] = 201
	server := httptest.NewServer(upstream.handler())
	t.Cleanup(server.Close)

	dialer, err := identity.NewGatewayWithDirectory(server.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := identity.New(tenantDB, dialer, bytes.Repeat([]byte("p"), 32), []string{"101"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store, err := identity.NewGatewayStore(gatewayDB)
	if err != nil {
		t.Fatal(err)
	}
	service.GatewayStore = store

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	owners, err := ownerservice.NewServer(ownerservice.Config{
		Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: provisioningBFTToken},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(owners); err != nil {
		t.Fatal(err)
	}
	go owners.ServeOn(listener)
	t.Cleanup(owners.Stop)

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, provisioningBFTToken)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })

	return &provisioningSystem{
		tenantDB:  tenantDB,
		gatewayDB: gatewayDB,
		client:    api.NewTenantProductServiceClient(conn),
		gateway:   upstream,
	}
}

// provisionCall is the BFF's platform-scoped command context.
func provisionCall(session *api.Session, cookie, key string) *api.CallContext {
	ref := owneridentity.SessionReference(cookie)
	return &api.CallContext{
		ActorId: session.GetActorId(), SessionId: &ref,
		Scope:          &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
		RequestId:      "request-" + session.GetActorId() + "-" + key,
		IdempotencyKey: key,
	}
}

func provisionBody(email, name string) *api.ProvisionAccountRequest {
	return &api.ProvisionAccountRequest{Email: email, Password: "CorrectHorseBatteryStaple!", Name: proto.String(name)}
}

// TestProvisionOperatorAccountOpensOneAccountTenantAndBinding proves the one
// command opens the account, admits its single-tenant owner and binds its
// Gateway wallet, and that the owner's operation record is durable.
func TestProvisionOperatorAccountOpensOneAccountTenantAndBinding(t *testing.T) {
	system := newProvisioningSystem(t)
	cookie, session := memberLogin(t, system.client, "admin@example.test")

	operation, err := system.client.ProvisionOperatorAccount(t.Context(), &api.ProvisionOperatorAccountRpcRequest{
		Context: provisionCall(session, cookie, "provision-key-1"),
		Body:    provisionBody("newcomer@example.test", "New Comer"),
	})
	if err != nil {
		t.Fatalf("provision: %v", err)
	}
	if operation.GetStatus() != "succeeded" ||
		operation.GetAccountId() == "" || operation.GetOperationId() == "" {
		t.Fatalf("provision operation=%+v", operation)
	}
	if !operation.GetWorkspacePurchaseEnabled() {
		t.Fatalf("a full Cloud customer was not granted purchase eligibility: %+v", operation)
	}
	if operation.GetPollAfterSeconds() != 0 {
		t.Fatalf("a terminal operation asked for polling: %+v", operation)
	}

	// The account fact, the Tenant, its owner membership and the wallet binding
	// are each persisted exactly once.
	var accounts, tenants, owners, bindings, identities int
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.customer_accounts WHERE email='newcomer@example.test'`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	actorID := system.actorOf(t, "newcomer@example.test")
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenants WHERE id IN (SELECT tenant_id FROM tenant.tenant_members WHERE actor_id=$1 AND role='owner' AND revoked_at IS NULL)`, actorID).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_members WHERE actor_id=$1 AND role='owner' AND revoked_at IS NULL`, actorID).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	tenantID := system.tenantOf(t, actorID)
	if err := system.gatewayDB.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.tenant_wallet_bindings WHERE tenant_id=$1 AND billing_sub2api_user_id=$2 AND revoked_at IS NULL`, tenantID, actorID).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if err := system.gatewayDB.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.identity_mappings WHERE actor_id=$1 AND sub2api_user_id=$1`, actorID).Scan(&identities); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || tenants != 1 || owners != 1 || bindings != 1 || identities != 1 {
		t.Fatalf("provision persisted accounts=%d tenants=%d owners=%d bindings=%d identities=%d", accounts, tenants, owners, bindings, identities)
	}

	// The durable operation record names the account and the command.
	var kind, status, stage string
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT kind,status,stage FROM tenant.operations WHERE id=$1`, operation.GetOperationId()).Scan(&kind, &status, &stage); err != nil {
		t.Fatal(err)
	}
	if kind != "provision_account" || status != "succeeded" || stage != "succeeded" {
		t.Fatalf("operation record kind=%q status=%q stage=%q", kind, status, stage)
	}

	// The account is listed by the owner that holds it.
	page, err := system.client.ListOperatorAccounts(t.Context(), &api.ListOperatorAccountsRpcRequest{
		Context: provisionCall(session, cookie, "provision-list-1"), Page: 1, PageSize: 20,
	})
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if page.GetTotal() != 1 || len(page.GetItems()) != 1 {
		t.Fatalf("account page=%+v", page)
	}
	item := page.GetItems()[0]
	if item.GetEmail() != "newcomer@example.test" || item.GetActorId() != actorID ||
		!item.GetWorkspacePurchaseEnabled() || item.GetDisplayName() != "New Comer" {
		t.Fatalf("account item=%+v", item)
	}
}

// TestProvisionOperatorAccountReplayConvergesAfterAMidwayFailure proves the
// acceptance case the blueprint names: a command that failed half way converges
// on one account, one Tenant and one wallet binding when it is replayed under
// the same idempotency key.
func TestProvisionOperatorAccountReplayConvergesAfterAMidwayFailure(t *testing.T) {
	system := newProvisioningSystem(t)
	cookie, session := memberLogin(t, system.client, "admin@example.test")

	// The Gateway identity read fails after the account and the Tenant exist, so
	// the command stops between the Tenant admission and the wallet binding.
	system.gateway.mu.Lock()
	system.gateway.failNewUserRead = true
	system.gateway.mu.Unlock()
	_, err := system.client.ProvisionOperatorAccount(t.Context(), &api.ProvisionOperatorAccountRpcRequest{
		Context: provisionCall(session, cookie, "provision-replay-1"),
		Body:    provisionBody("replay@example.test", "Replay Customer"),
	})
	t.Logf("the interrupted command failed with %v", err)
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("the interrupted command status=%v err=%v", status.Code(err), err)
	}
	system.gateway.mu.Lock()
	system.gateway.failNewUserRead = false
	system.gateway.mu.Unlock()

	// The half-finished command left the account and the Tenant behind.
	actorID := system.actorOf(t, "replay@example.test")
	if actorID == "" {
		t.Fatal("the interrupted command left no account behind")
	}
	tenantID := system.tenantOf(t, actorID)
	if tenantID == "" {
		t.Fatal("the interrupted command left no tenant behind")
	}

	// The same command under the same key converges.
	operation, err := system.client.ProvisionOperatorAccount(t.Context(), &api.ProvisionOperatorAccountRpcRequest{
		Context: provisionCall(session, cookie, "provision-replay-1"),
		Body:    provisionBody("replay@example.test", "Replay Customer"),
	})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if operation.GetStatus() != "succeeded" || operation.GetAccountId() == "" {
		t.Fatalf("replay operation=%+v", operation)
	}

	// The converged state is one account, one Tenant, one owner membership and
	// one wallet binding: no half-finished duplicate is left behind.
	var accounts, tenants, owners, bindings int
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.customer_accounts WHERE email='replay@example.test'`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenants`).Scan(&tenants); err != nil {
		t.Fatal(err)
	}
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_members WHERE role='owner' AND revoked_at IS NULL`).Scan(&owners); err != nil {
		t.Fatal(err)
	}
	if err := system.gatewayDB.QueryRowContext(t.Context(), `SELECT count(*) FROM gateway.tenant_wallet_bindings WHERE revoked_at IS NULL`).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 || tenants != 1 || owners != 1 || bindings != 1 {
		t.Fatalf("replay persisted accounts=%d tenants=%d owners=%d bindings=%d", accounts, tenants, owners, bindings)
	}
	creates, _ := system.gateway.counts()
	if creates != 1 {
		t.Fatalf("the account write was dispatched %d times", creates)
	}
}

// TestProvisionOperatorAccountRefusesAReusedKeyWithAnotherCommand proves one
// idempotency key names one command: a different command under the same key is
// refused instead of being interpreted as the original one.
func TestProvisionOperatorAccountRefusesAReusedKeyWithAnotherCommand(t *testing.T) {
	system := newProvisioningSystem(t)
	cookie, session := memberLogin(t, system.client, "admin@example.test")

	if _, err := system.client.ProvisionOperatorAccount(t.Context(), &api.ProvisionOperatorAccountRpcRequest{
		Context: provisionCall(session, cookie, "provision-conflict-1"),
		Body:    provisionBody("first@example.test", "First Customer"),
	}); err != nil {
		t.Fatalf("first command: %v", err)
	}
	_, err := system.client.ProvisionOperatorAccount(t.Context(), &api.ProvisionOperatorAccountRpcRequest{
		Context: provisionCall(session, cookie, "provision-conflict-1"),
		Body:    provisionBody("second@example.test", "Second Customer"),
	})
	if status.Code(err) != codes.AlreadyExists {
		t.Fatalf("the reused key status=%v err=%v", status.Code(err), err)
	}
	code, ok := owneridentity.ErrorCode(err)
	if !ok || code != api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT {
		t.Fatalf("the reused key error code=%v err=%v", code, err)
	}
	var accounts int
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.customer_accounts`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if accounts != 1 {
		t.Fatalf("the refused command persisted %d accounts", accounts)
	}
}

// TestProvisionOperatorAccountRefusesANonAdministrator proves the command is a
// platform-administrator command: a signed-in customer may not open accounts.
func TestProvisionOperatorAccountRefusesANonAdministrator(t *testing.T) {
	system := newProvisioningSystem(t)
	cookie, session := memberLogin(t, system.client, "customer@example.test")

	_, err := system.client.ProvisionOperatorAccount(t.Context(), &api.ProvisionOperatorAccountRpcRequest{
		Context: provisionCall(session, cookie, "provision-denied-1"),
		Body:    provisionBody("intruder@example.test", "Intruder"),
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("the customer command status=%v err=%v", status.Code(err), err)
	}
	var accounts int
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.customer_accounts`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if accounts != 0 {
		t.Fatalf("the refused command persisted %d accounts", accounts)
	}
	creates, _ := system.gateway.counts()
	if creates != 0 {
		t.Fatalf("the refused command dispatched %d account writes", creates)
	}
}

// TestProvisionOperatorAccountDoesNotDispatchTheAccountWriteTwice proves the
// external write is dispatched at most once per command: while its outcome is
// unconfirmed the owner reads the Gateway account back instead of writing a
// second one, and the command's state stays readable.
func TestProvisionOperatorAccountDoesNotDispatchTheAccountWriteTwice(t *testing.T) {
	system := newProvisioningSystem(t)
	cookie, session := memberLogin(t, system.client, "admin@example.test")

	system.gateway.mu.Lock()
	system.gateway.failCreate = true
	system.gateway.mu.Unlock()

	first, err := system.client.ProvisionOperatorAccount(t.Context(), &api.ProvisionOperatorAccountRpcRequest{
		Context: provisionCall(session, cookie, "provision-unavailable-1"),
		Body:    provisionBody("unavailable@example.test", "Unavailable Customer"),
	})
	if err != nil {
		t.Fatalf("the unavailable command failed instead of staying open: %v", err)
	}
	if first.GetStatus() != "awaiting_confirmation" || first.GetPollAfterSeconds() <= 0 {
		t.Fatalf("the unavailable command operation=%+v", first)
	}

	// The same command under the same key reads its own state back and does not
	// send the account write again.
	second, err := system.client.ProvisionOperatorAccount(t.Context(), &api.ProvisionOperatorAccountRpcRequest{
		Context: provisionCall(session, cookie, "provision-unavailable-1"),
		Body:    provisionBody("unavailable@example.test", "Unavailable Customer"),
	})
	if err != nil {
		t.Fatalf("the replay failed instead of staying open: %v", err)
	}
	if second.GetOperationId() != first.GetOperationId() ||
		second.GetStatus() != "awaiting_confirmation" {
		t.Fatalf("the replay operation=%+v first=%+v", second, first)
	}
	creates, _ := system.gateway.counts()
	if creates != 1 {
		t.Fatalf("the account write was dispatched %d times", creates)
	}
	var accounts int
	if err := system.tenantDB.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.customer_accounts`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if accounts != 0 {
		t.Fatalf("the unconfirmed command persisted %d accounts", accounts)
	}
}

// actorOf reads the Gateway subject the owner bound to an account email.
func (s *provisioningSystem) actorOf(t *testing.T, email string) string {
	t.Helper()
	var actorID string
	if err := s.tenantDB.QueryRowContext(t.Context(), `SELECT actor_id FROM tenant.customer_accounts WHERE email=$1`, email).Scan(&actorID); err != nil {
		t.Fatalf("actor of %s: %v", email, err)
	}
	return actorID
}

// tenantOf reads the Tenant the given owner admitted.
func (s *provisioningSystem) tenantOf(t *testing.T, actorID string) string {
	t.Helper()
	var tenantID string
	if err := s.tenantDB.QueryRowContext(t.Context(), `SELECT id FROM tenant.tenants WHERE id IN (SELECT tenant_id FROM tenant.tenant_members WHERE actor_id=$1 AND role='owner' AND revoked_at IS NULL)`, actorID).Scan(&tenantID); err != nil {
		t.Fatalf("tenant of %s: %v", actorID, err)
	}
	return tenantID
}
