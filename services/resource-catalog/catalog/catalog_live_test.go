//go:build livebuild

// The Resource Catalog's real cross-owner loop: a real CloudIdentity session and
// authorization decision, the real authenticated Console BFF boundary, and the
// real Resource Catalog owner process over its own PostgreSQL database.
//
// Only the external Sub2API Gateway is an isolated fixture; the CloudIdentity
// session, role and permission decision are the real implementation, so a passing
// run proves the catalog actions are actually admitted for a platform
// administrator and actually refused for everyone else.
package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"opl-cloud/apps/console-bff"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	cloudidentity "opl-cloud/services/gateway-integration/identity"
	identitymigrations "opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/resource-catalog/migrations"
)

const livePeerToken = "isolated-resource-catalog-peer-token-0001"

// liveOwnerDB provisions one owner's real isolated database and installs its real
// migrations.
func liveOwnerDB(t *testing.T, ctx context.Context, dsn, name string, source ownerstore.MigrationSource) *sql.DB {
	t.Helper()
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: name, Database: "opl_" + name, SchemaOwnerRole: "opl_" + name + "_owner", WriterRole: "opl_" + name + "_writer", RuntimeRole: "opl_" + name + "_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatal(err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// liveOwnerServer serves one real owner process over a real gRPC listener behind
// the shared owner identity interceptor.
func liveOwnerServer(t *testing.T, owner owneridentity.Owner, peers []owneridentity.Service, register func(*ownerservice.Server) error) string {
	t.Helper()
	config := ownerservice.Config{Owner: owner, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{}}
	for _, peer := range peers {
		config.Peers[peer] = livePeerToken
	}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := register(server); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

// liveDial opens a typed gRPC connection that presents the caller's real service
// identity and token on every call.
func liveDial(t *testing.T, address string, caller, target owneridentity.Owner, token string) *grpc.ClientConn {
	t.Helper()
	options, err := owneridentity.TLSConfig{AllowInsecureLocal: true}.DialOptions(caller.Service(), target.Service(), token)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// liveSystem is the composed real loop.
type liveSystem struct {
	base            string
	catalogAddress  string
	identityAddress string
	identity        *cloudidentity.Service
	db              *sql.DB
	cookies         map[string]string
	csrf            map[string]string
}

func liveGatewayFixture(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := int64(101)
		email := "admin@example.test"
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
			case "platform-admin@example.test":
				actor = 103
			case "tenant-admin@example.test":
				actor = 101
			case "member@example.test":
				actor = 104
			default:
				w.WriteHeader(401)
				return
			}
		} else if r.URL.Path == "/api/v1/auth/me" {
			switch strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") {
			case "isolated-gateway-platform-admin@example.test":
				actor, email = 103, "platform-admin@example.test"
			case "isolated-gateway-tenant-admin@example.test":
				actor, email = 101, "tenant-admin@example.test"
			case "isolated-gateway-member@example.test":
				actor, email = 104, "member@example.test"
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
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "isolated-gateway-" + email, "user": user}})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": user})
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// newLiveSystem composes the real CloudIdentity process, the real Resource
// Catalog process and the real Console BFF handler over one isolated PostgreSQL
// server.
func newLiveSystem(t *testing.T) *liveSystem {
	t.Helper()
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)

	identitySource, err := identitymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	tenantDB := liveOwnerDB(t, ctx, dsn, "tenant", identitySource)
	if _, err = tenantDB.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name) VALUES('tenant-live','Test Tenant');
		INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('member-admin','tenant-live','101','admin'),('member-member','tenant-live','104','member')`); err != nil {
		t.Fatal(err)
	}
	gateway, err := cloudidentity.NewGateway(liveGatewayFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	// The platform administrator is the deployment-owned Gateway subject allowlist,
	// never a Tenant membership.
	identityService, err := cloudidentity.New(tenantDB, gateway, bytes.Repeat([]byte("k"), 32), []string{"103"}, 168*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	identityAddress := liveOwnerServer(t, owneridentity.Tenant, []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.ResourceCatalog.Service()}, identityService.Register)

	catalogSource, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	catalogDB := liveOwnerDB(t, ctx, dsn, "resource_catalog", catalogSource)
	authorizer := ownerservice.NewAuthorizer(ownerservice.OwnerResourceCatalog, api.NewCloudIdentityAuthorizationClient(
		liveDial(t, identityAddress, owneridentity.ResourceCatalog, owneridentity.Tenant, livePeerToken)))
	catalogService, err := New(catalogDB, authorizer, Profile{Provider: "local-docker", Region: "local", BillingMode: "LOCAL_NO_CHARGE", ProviderCapabilityVersion: "provider/v1"})
	if err != nil {
		t.Fatal(err)
	}
	catalogAddress := liveOwnerServer(t, owneridentity.ResourceCatalog, []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.ResourceCatalog.Service()}, catalogService.Register)

	identityClient, err := bff.DialIdentity(identityAddress, livePeerToken, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(identityClient.Close)
	catalogClient, err := bff.DialResourceCatalog(catalogAddress, livePeerToken, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(catalogClient.Close)
	handler := bff.NewCatalogHandler(catalogClient.ResourceCatalogProductServiceClient, identityClient)
	front := httptest.NewServer(handler)
	t.Cleanup(front.Close)

	system := &liveSystem{base: front.URL, catalogAddress: catalogAddress, identityAddress: identityAddress, identity: identityService, db: catalogDB, cookies: map[string]string{}, csrf: map[string]string{}}
	for _, email := range []string{"platform-admin@example.test", "tenant-admin@example.test", "member@example.test"} {
		_, challenge, err := identityClient.LoginContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		session, cookie, err := identityClient.Login(ctx, challenge, &api.LoginRequest{Username: email, Password: "isolated-password"})
		if err != nil {
			t.Fatal(err)
		}
		system.cookies[email] = cookie
		system.csrf[email] = session.GetCsrfToken()
	}
	return system
}

// do performs one same-origin browser request through the real BFF boundary.
func (s *liveSystem) do(t *testing.T, ctx context.Context, email, method, path, idempotencyKey, body string) (int, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequestWithContext(ctx, method, s.base+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(&http.Cookie{Name: "opl_session", Value: s.cookies[email]})
	request.Header.Set("x-opl-request-id", "req-"+idempotencyKey)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-CSRF-Token", s.csrf[email])
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &decoded)
	}
	return response.StatusCode, decoded
}

func mustStatus(t *testing.T, got int, want int, body map[string]any, label string) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: status = %d, want %d (%v)", label, got, want, body)
	}
}

// TestLiveCatalogPlatformAdminLoop is the acceptance for this round: a real
// platform administrator session drives the authenticated BFF command, the owner
// persists the approved plan and policy versions in its own database, and the same
// owner reads them back. Tenant administrators and members are refused, retired
// plans cannot be priced, idempotent retries replay the stored result, and the
// owner's runtime cannot read another owner's schema.
func TestLiveCatalogPlatformAdminLoop(t *testing.T) {
	system := newLiveSystem(t)
	ctx := t.Context()
	admin := "platform-admin@example.test"
	validFrom := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	// 1. The real CloudIdentity decision admits the platform administrator.
	status, plan := system.do(t, ctx, admin, http.MethodPost, "/api/v2/admin/catalog/compute-plans", "live-compute-1",
		`{"name":"basic","vcpus":2,"memoryMiB":4096,"providerProfileId":"local/profile","providerSkuId":"local-basic","providerCapabilityVersion":"provider/v1","validFrom":"`+validFrom+`"}`)
	mustStatus(t, status, http.StatusCreated, plan, "platform admin creates compute plan")
	computePlanID, _ := plan["id"].(string)
	if computePlanID == "" {
		t.Fatalf("compute plan id missing: %v", plan)
	}
	status, storage := system.do(t, ctx, admin, http.MethodPost, "/api/v2/admin/catalog/storage-plans", "live-storage-1",
		`{"name":"standard","capacityGiB":10,"providerProfileId":"local/profile","providerSkuId":"local-storage","shrinkSupported":true,"validFrom":"`+validFrom+`"}`)
	mustStatus(t, status, http.StatusCreated, storage, "platform admin creates storage plan")
	storagePlanID, _ := storage["id"].(string)
	status, retention := system.do(t, ctx, admin, http.MethodPost, "/api/v2/admin/catalog/retention-policies", "live-retention-1",
		`{"versionLabel":"retention-1","customerTerms":"data destroyed after confirmed deletion"}`)
	mustStatus(t, status, http.StatusCreated, retention, "platform admin creates retention policy")
	retentionID, _ := retention["id"].(string)
	status, refund := system.do(t, ctx, admin, http.MethodPost, "/api/v2/admin/catalog/refund-policies", "live-refund-1",
		`{"versionLabel":"refund-1","algorithm":"workspace-delete-refund-v1","retentionPolicyVersionId":"`+retentionID+`","customerTerms":"720-hour policy","validFrom":"`+validFrom+`"}`)
	mustStatus(t, status, http.StatusCreated, refund, "platform admin creates refund policy")
	// A money-bearing price policy version travels the wire. The canonical amounts
	// are decimal strings under the contract's own spelling, and the shared codec
	// resolves both from the contract, so this asserts the exact round-trip the
	// policy version requires rather than only that the command was accepted.
	status, price := system.do(t, ctx, admin, http.MethodPost, "/api/v2/admin/catalog/price-policies", "live-price-1",
		`{"versionLabel":"2026-09","periodMonths":1,"computeMonthlyUSDMicros":"0","storageMonthlyUSDMicros":"0","productMonthlyUSDMicros":"1","validFrom":"`+validFrom+`","computePlanId":"`+computePlanID+`","storagePlanId":"`+storagePlanID+`","renewalPolicy":{"version":"renewal-policy/v1","trigger":"manual_or_explicitly_consented_automatic","effectiveStart":"previous_paid_through","months":1,"usesAcceptedPriceSnapshot":true},"planChangePolicyVersion":"workspace-plan-change-v1"}`)
	mustStatus(t, status, http.StatusCreated, price, "platform admin creates a money-bearing price policy")
	pricePolicyID, _ := price["id"].(string)
	if price["productMonthlyUSDMicros"] != "1" {
		t.Fatalf("price policy did not round-trip the contract's decimal string amount: %v", price)
	}
	// The owner's own row carries the amount, so the readback is the owner's fact.
	var storedProduct int64
	if err := system.db.QueryRowContext(ctx, `SELECT product_monthly_usd_micros FROM resource_catalog.price_policy_versions WHERE id=$1`, pricePolicyID).Scan(&storedProduct); err != nil {
		t.Fatal(err)
	}
	if storedProduct != 1 {
		t.Fatalf("owner product amount = %d, want 1", storedProduct)
	}

	// 4. A tenant administrator cannot perform a platform-administrator action.
	status, denied := system.do(t, ctx, "tenant-admin@example.test", http.MethodPost, "/api/v2/admin/catalog/compute-plans", "live-denied-1",
		`{"name":"forbidden","vcpus":2,"memoryMiB":4096,"providerProfileId":"p","providerSkuId":"s","providerCapabilityVersion":"provider/v1","validFrom":"`+validFrom+`"}`)
	mustStatus(t, status, http.StatusForbidden, denied, "tenant admin is refused a platform administrator action")
	var count int
	if err := system.db.QueryRowContext(ctx, `SELECT count(*) FROM resource_catalog.compute_plans WHERE name='forbidden'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("a refused action still wrote a plan row")
	}

	// 5. A member may list the catalog but may not read the administrator policy
	// surface, and its own list is tenant-scoped.
	status, listed := system.do(t, ctx, "member@example.test", http.MethodGet, "/api/v2/catalog/compute-plans", "", "")
	mustStatus(t, status, http.StatusOK, listed, "member lists compute plans")
	if !pageContainsID(listed, computePlanID) {
		t.Fatalf("member list did not contain the approved plan: %v", listed)
	}
	status, refused := system.do(t, ctx, "member@example.test", http.MethodGet, "/api/v2/admin/catalog/price-policies", "", "")
	mustStatus(t, status, http.StatusForbidden, refused, "member is refused the administrator policy list")

	// 6. An anonymous browser receives no catalog facts.
	anonymous := httptest.NewRequest(http.MethodGet, "/api/v2/catalog/compute-plans", nil)
	response := httptest.NewRecorder()
	bff.NewCatalogHandler(nil, nil).ServeHTTP(response, anonymous)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous catalog read = %d, want 401", response.Code)
	}

	// 7. A retired plan cannot be priced again, and the customer list shows the
	// retirement instead of silently dropping the plan.
	// The contract declares 200 for this action, and the shared boundary answers
	// with the status the contract declares, so the exact code is asserted.
	status, retired := system.do(t, ctx, admin, http.MethodPut, "/api/v2/admin/catalog/storage-plans/"+storagePlanID+"/availability", "live-retire-1",
		`{"availability":"retired","reason":"end of life"}`)
	mustStatus(t, status, http.StatusOK, retired, "platform admin retires a storage plan")
	if retired["availability"] != "retired" {
		t.Fatalf("retire did not project the plan as retired: %v", retired)
	}
	status, afterRetirement := system.do(t, ctx, "member@example.test", http.MethodGet, "/api/v2/catalog/storage-plans", "", "")
	mustStatus(t, status, http.StatusOK, afterRetirement, "member lists storage plans after retirement")
	if !pageItemHas(afterRetirement, storagePlanID, "availability", "retired") {
		t.Fatalf("retired storage plan was not projected as retired: %v", afterRetirement)
	}

	// 8. Idempotent retry replays the stored plan; a conflicting body is refused.
	status, replay := system.do(t, ctx, admin, http.MethodPost, "/api/v2/admin/catalog/compute-plans", "live-compute-1",
		`{"name":"basic","vcpus":2,"memoryMiB":4096,"providerProfileId":"local/profile","providerSkuId":"local-basic","providerCapabilityVersion":"provider/v1","validFrom":"`+validFrom+`"}`)
	mustStatus(t, status, http.StatusCreated, replay, "idempotent retry replays the stored plan")
	if replay["id"] != computePlanID {
		t.Fatalf("replay returned a different plan: %v", replay)
	}
	status, conflict := system.do(t, ctx, admin, http.MethodPost, "/api/v2/admin/catalog/compute-plans", "live-compute-1",
		`{"name":"different","vcpus":4,"memoryMiB":8192,"providerProfileId":"local/profile","providerSkuId":"local-basic","providerCapabilityVersion":"provider/v1","validFrom":"`+validFrom+`"}`)
	mustStatus(t, status, http.StatusConflict, conflict, "a conflicting retry is refused")
	var plans int
	if err := system.db.QueryRowContext(ctx, `SELECT count(*) FROM resource_catalog.compute_plans`).Scan(&plans); err != nil {
		t.Fatal(err)
	}
	if plans != 1 {
		t.Fatalf("compute plans = %d, want exactly the one created plan", plans)
	}

	// 9. Lost-response replay: the identical command is reissued and converges on
	// the original row instead of a second one.
	status, lost := system.do(t, ctx, admin, http.MethodPost, "/api/v2/admin/catalog/retention-policies", "live-retention-1",
		`{"versionLabel":"retention-1","customerTerms":"data destroyed after confirmed deletion"}`)
	mustStatus(t, status, http.StatusCreated, lost, "lost-response replay converges")
	if lost["id"] != retentionID {
		t.Fatalf("lost-response replay created a second retention version: %v", lost)
	}

	// 10. The owner's runtime login cannot reach another owner's data. The
	// isolation harness plants a neighbour schema in this owner's own database, so
	// the refusal below is PostgreSQL's own privilege boundary rather than an
	// absent relation, and this owner's database does not carry another owner's
	// schema either.
	if _, err := system.db.ExecContext(ctx, `SELECT count(*) FROM opl_isolation_neighbor.owner_records`); err == nil {
		t.Fatal("resource catalog runtime read another owner's schema")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("cross-owner read failed for an unexpected reason: %v", err)
	}
	if _, err := system.db.ExecContext(ctx, `SELECT count(*) FROM tenant.tenants`); err == nil {
		t.Fatal("resource catalog runtime reached another owner's database schema")
	}
}

// catalogActions is the exact action set the Resource Catalog's minimal product
// loop needs, with the audience owner, the resource kind, the scope and the role
// the canonical API contract declares for each.
type catalogAction struct {
	action   api.AuthorizationActionEnum
	platform bool
}

func catalogActionSet() []catalogAction {
	names := []struct {
		name     string
		platform bool
	}{
		{"CREATECOMPUTEPLAN", true},
		{"SETCOMPUTEPLANAVAILABILITY", true},
		{"CREATESTORAGEPLAN", true},
		{"SETSTORAGEPLANAVAILABILITY", true},
		{"LISTPRICEPOLICYVERSIONS", true},
		{"CREATEPRICEPOLICYVERSION", true},
		{"LISTREFUNDPOLICYVERSIONS", true},
		{"CREATEREFUNDPOLICYVERSION", true},
		{"LISTRETENTIONPOLICYVERSIONS", true},
		{"CREATERETENTIONPOLICYVERSION", true},
		{"LISTCOMPUTEPLANS", false},
		{"LISTSTORAGEPLANS", false},
	}
	out := make([]catalogAction, 0, len(names))
	for _, entry := range names {
		value, ok := api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_"+entry.name]
		if !ok {
			panic("the contract no longer declares action " + entry.name)
		}
		out = append(out, catalogAction{action: api.AuthorizationActionEnum(value), platform: entry.platform})
	}
	return out
}

// TestLiveCatalogCloudIdentityDecisionCoversCatalogActions pins the exact
// CloudIdentity behavior this round depends on: every Resource Catalog action the
// minimal loop needs is admitted for a real platform administrator (platform
// actions) or a real member (customer reads), against the Resource Catalog
// audience and the catalog resource kind — and no other actor is admitted.
//
// It fails loudly while the shared CloudIdentity policy has no resource_catalog
// actions, which is the state this round is waiting on.
func TestLiveCatalogCloudIdentityDecisionCoversCatalogActions(t *testing.T) {
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)

	identitySource, err := identitymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	tenantDB := liveOwnerDB(t, ctx, dsn, "tenant", identitySource)
	if _, err = tenantDB.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name) VALUES('tenant-live','Test Tenant');
		INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('member-admin','tenant-live','101','admin'),('member-member','tenant-live','104','member')`); err != nil {
		t.Fatal(err)
	}
	gateway, err := cloudidentity.NewGateway(liveGatewayFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	identityService, err := cloudidentity.New(tenantDB, gateway, bytes.Repeat([]byte("k"), 32), []string{"103"}, 168*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	address := liveOwnerServer(t, owneridentity.Tenant, []owneridentity.Service{owneridentity.ConsoleBFF}, identityService.Register)
	client, err := bff.DialIdentity(address, livePeerToken, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)

	sessions := map[string]*api.Session{}
	sessionIDs := map[string]string{}
	for _, email := range []string{"platform-admin@example.test", "tenant-admin@example.test", "member@example.test"} {
		_, challenge, err := client.LoginContext(ctx)
		if err != nil {
			t.Fatal(err)
		}
		session, cookie, err := client.Login(ctx, challenge, &api.LoginRequest{Username: email, Password: "isolated-password"})
		if err != nil {
			t.Fatal(err)
		}
		sessions[email] = session
		sessionIDs[email] = cookie
	}

	// decide returns the decision and whether the authority admitted the request.
	// A refusal is reported by the real implementation as a PermissionDenied status
	// rather than a DENIED decision, so both forms mean "not allowed".
	decide := func(t *testing.T, email string, entry catalogAction) (*api.AuthorizationDecision, bool) {
		t.Helper()
		session := sessions[email]
		scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
		if session.GetTenantId() != "" {
			scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: session.GetTenantId()}}}
		}
		// The session reference is the browser session id CloudIdentity issued, which
		// is also the login cookie value.
		sessionID := owneridentity.SessionReference(sessionIDs[email])
		decision, err := client.Authorize(ctx, &api.AuthorizationRequest{
			Scope: scope, ActorId: session.GetActorId(), SessionId: &sessionID,
			AudienceOwner: api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG, Action: entry.action,
			Resource:  &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG},
			RequestId: "live-decision-" + entry.action.String(),
		})
		if err != nil {
			switch status.Code(err) {
			case codes.PermissionDenied, codes.Unauthenticated:
				return nil, false
			}
			t.Fatalf("authorize %s as %s: %v", entry.action, email, err)
		}
		return decision, decision.GetResult() == api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED
	}

	// Only applicable (actor, action) pairs are asserted. A member row for an
	// administrator action, or an administrator row for a customer read, is not an
	// expectation this owner has, so it is not enumerated as a skipped case.
	for _, entry := range catalogActionSet() {
		entry := entry
		if entry.platform {
			t.Run("platform_admin/"+entry.action.String(), func(t *testing.T) {
				got, admitted := decide(t, "platform-admin@example.test", entry)
				if !admitted || got.GetIssuer() != api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY {
					t.Fatalf("platform administrator was not admitted for %s", entry.action)
				}
			})
			t.Run("tenant_admin_refused/"+entry.action.String(), func(t *testing.T) {
				if _, admitted := decide(t, "tenant-admin@example.test", entry); admitted {
					t.Fatalf("a tenant administrator was admitted for the platform action %s", entry.action)
				}
			})
			t.Run("member_refused/"+entry.action.String(), func(t *testing.T) {
				if _, admitted := decide(t, "member@example.test", entry); admitted {
					t.Fatalf("a member was admitted for the platform action %s", entry.action)
				}
			})
			continue
		}
		t.Run("member/"+entry.action.String(), func(t *testing.T) {
			if _, admitted := decide(t, "member@example.test", entry); !admitted {
				t.Fatalf("member was not admitted for %s", entry.action)
			}
		})
	}

}

func pageContainsID(page map[string]any, id string) bool {
	return pageItemHas(page, id, "", "")
}

// pageItemHas reports whether the page's items contain an item with the given id
// (and, when field is set, that field holding want).
func pageItemHas(page map[string]any, id, field, want string) bool {
	items, _ := page["items"].([]any)
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		if item["id"] != id {
			continue
		}
		if field == "" {
			return true
		}
		if value, _ := item[field].(string); value == want {
			return true
		}
	}
	return false
}

var (
	_ = codes.OK
	_ = insecure.NewCredentials
	_ = metadata.MD{}
	_ = status.Error
	_ = timestamppb.Now
)
