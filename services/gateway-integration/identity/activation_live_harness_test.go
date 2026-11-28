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
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	bff "opl-cloud/apps/console-bff"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/gatewaymigrations"
	"opl-cloud/services/gateway-integration/identity"
	"opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// activationSubject is the Gateway subject the retained activation command
// resolves for the activated customer.
const activationSubject = 58

// activationSub2API simulates the only external boundary: Sub2API. It authenticates
// a browser login and serves the administrative directory identity the Gateway
// owner re-reads before it binds a billing subject.
type activationSub2API struct {
	mu       sync.Mutex
	users    map[string]int64
	nextID   int64
	requests int
}

func newActivationSub2API() *activationSub2API {
	return &activationSub2API{users: map[string]int64{}, nextID: 40}
}

func (s *activationSub2API) register(email string, id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.users[email] = id
}

func (s *activationSub2API) resolve(email string) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id, ok := s.users[email]; ok {
		return id
	}
	s.nextID++
	s.users[email] = s.nextID
	return s.nextID
}

// emailOf reads the account the directory holds under this subject id.
func (s *activationSub2API) emailOf(id int64) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for email, candidate := range s.users {
		if candidate == id {
			return email
		}
	}
	return ""
}

// accounts lists the directory accounts, newest subject last.
func (s *activationSub2API) accounts() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]map[string]any, 0, len(s.users))
	for email, id := range s.users {
		out = append(out, map[string]any{"id": id, "email": email, "status": "active"})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i]["id"].(int64) < out[j]["id"].(int64)
	})
	return out
}

func (s *activationSub2API) handler() http.Handler {
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
		s.mu.Lock()
		s.requests++
		s.mu.Unlock()
		id := s.resolve(body["email"])
		envelope(w, map[string]any{"access_token": "token-" + body["email"], "expires_in": 3600,
			"user": map[string]any{"id": id, "email": body["email"], "status": "active"}})
	})
	mux.HandleFunc("GET /api/v1/auth/me", func(w http.ResponseWriter, r *http.Request) {
		email := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer token-")
		if email == "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		envelope(w, map[string]any{"id": s.resolve(email), "email": email, "status": "active"})
	})
	mux.HandleFunc("GET /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		search := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("search")))
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		if pageSize < 1 {
			pageSize = 100
		}
		matches := make([]map[string]any, 0, 1)
		for _, account := range s.accounts() {
			if search != "" && !strings.Contains(strings.ToLower(account["email"].(string)), search) {
				continue
			}
			matches = append(matches, account)
		}
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
		envelope(w, map[string]any{
			"items": matches[start:end], "total": total, "page": page, "page_size": pageSize, "pages": pages,
		})
	})
	mux.HandleFunc("POST /api/v1/admin/users", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		email := strings.ToLower(strings.TrimSpace(body["email"]))
		if email == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		id := s.resolve(email)
		envelope(w, map[string]any{"id": id, "email": email, "status": "active"})
	})
	mux.HandleFunc("GET /api/v1/admin/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil || id <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		envelope(w, map[string]any{"id": id, "email": s.emailOf(id), "balance": "0", "status": "active"})
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
	mux.HandleFunc("GET /v1/models", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"object":"list","data":[]}`))
	})
	return mux
}

// activationSystem is the joined cross-owner fixture: the real identity owner over
// typed gRPC, the real Console BFF handler over the real dialed clients, a real
// PostgreSQL Tenant and Gateway store, and a simulated Sub2API boundary. The
// retained Control Plane is a stub HTTP origin because its own writer is proved by
// its own module's tests.
type activationSystem struct {
	tenantDB  *sql.DB
	gatewayDB *sql.DB
	address   string
	token     string
	reader    bff.GatewayReadClient
	workspace api.WorkspaceProductServiceClient
	sub2api   *activationSub2API
}

func activationLiveSystem(t *testing.T) *activationSystem {
	t.Helper()
	ctx := context.Background()
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

	sub2api := newActivationSub2API()
	sub2api.register("operator@example.test", 101)
	sub2api.register("customer@example.test", activationSubject)
	upstream := httptest.NewServer(sub2api.handler())
	t.Cleanup(upstream.Close)

	dialer, err := identity.NewGatewayWithDirectory(upstream.URL, &identity.GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	service, err := identity.New(tenantDB, dialer, bytes.Repeat([]byte("s"), 32), []string{"101"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	store, err := identity.NewGatewayStore(gatewayDB)
	if err != nil {
		t.Fatal(err)
	}
	service.GatewayStore = store

	const token = "isolated-activation-console-bff-token-01"
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

	reader, err := bff.DialGatewayReader(listener.Addr().String(), token, token, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)

	return &activationSystem{
		tenantDB:  tenantDB,
		gatewayDB: gatewayDB,
		address:   listener.Addr().String(),
		token:     token,
		reader:    reader,
		workspace: &emptyWorkspaceOwner{},
		sub2api:   sub2api,
	}
}

// emptyWorkspaceOwner answers the customer's own Workspace list. It is the
// Workspace owner's read, not its authorization: the BFF authorizes the caller
// before it ever calls this owner, which is exactly the boundary the defect is in.
type emptyWorkspaceOwner struct {
	api.WorkspaceProductServiceClient
	calls int
}

func (o *emptyWorkspaceOwner) ListWorkspaces(context.Context, *api.ListWorkspacesRpcRequest, ...grpc.CallOption) (*api.WorkspacePage, error) {
	o.calls++
	return &api.WorkspacePage{Items: []*api.Workspace{}}, nil
}

// login signs one browser in through the real CloudIdentity owner and returns the
// session cookie and the session's own CSRF token.
func (s *activationSystem) login(t *testing.T, username string) (string, string) {
	t.Helper()
	ctx := context.Background()
	conn, err := grpc.NewClient(s.address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.ConsoleBFF, s.token)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	var header metadata.MD
	if _, err = api.NewTenantProductServiceClient(conn).GetLoginContext(ctx, &api.GetLoginContextRpcRequest{}, grpc.Header(&header)); err != nil {
		t.Fatal(err)
	}
	if _, cookie, err := s.reader.Login(ctx, header.Get(owneridentity.SessionCookieHeader)[0], &api.LoginRequest{Username: username, Password: "password"}); err != nil {
		t.Fatalf("login %s: %v", username, err)
	} else {
		session, err := s.reader.Session(ctx, cookie)
		if err != nil {
			t.Fatal(err)
		}
		return cookie, session.GetCsrfToken()
	}
	return "", ""
}

// tenantOf reads the Tenant the given owner admitted.
func (s *activationSystem) tenantOf(t *testing.T, actorID string) string {
	t.Helper()
	var tenantID string
	if err := s.tenantDB.QueryRowContext(t.Context(), `SELECT id FROM tenant.tenants WHERE id IN (SELECT tenant_id FROM tenant.tenant_members WHERE actor_id=$1 AND role='owner' AND revoked_at IS NULL)`, actorID).Scan(&tenantID); err != nil {
		t.Fatalf("tenant of %s: %v", actorID, err)
	}
	return tenantID
}

func (s *activationSystem) read(handler http.Handler, cookie, path string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(&http.Cookie{Name: "opl_console_session", Value: cookie})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func (s *activationSystem) post(handler http.Handler, cookie, csrf, path, key, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: "opl_console_session", Value: cookie})
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
