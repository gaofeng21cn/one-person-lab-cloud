//go:build livebuild

package delivery_test

// This is the Serve read slice's real-identity acceptance harness. Unlike the
// owner unit tests in this package, which use a decision stub, it runs the
// production CloudIdentity implementation from services/gateway-integration over
// real gRPC with its real policy table, real session issuance and a real
// isolated PostgreSQL database. Only the external Sub2API/Gateway HTTP boundary
// is simulated, exactly as the existing Build live chain does.
//
// It is opt-in and therefore not part of verify:local or verify:local:full:
//
//	OPL_OWNER_MIGRATION_TEST_ADMIN_DSN=<isolated postgres> \
//	  go test -tags=livebuild ./internal/delivery -run TestLiveServeReadChain -count=1 -v
//
// A denial here is the real, current behaviour; it must not be reported as a
// Serve defect and must not be reported as a passing read slice.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
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

	bff "opl-cloud/apps/console-bff"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/gateway-integration/identity"
	identitymigrations "opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// The isolated local identities: real values for this test only, never
// production, and long enough for the shared minimum.
const (
	liveIdentityToken = "isolated-cloud-identity-peer-token-0001"
	liveServeToken    = "isolated-serve-bff-peer-token-0001"
	livePassword      = "isolated-password"
)

// liveTenant owns the Serve Workspace under test; liveOtherTenant is the Tenant
// whose member must never read it.
const (
	liveTenant      = "tenant-serve-live"
	liveOtherTenant = "tenant-other-live"
)

// The actor ids the simulated Gateway issues for each known local subject.
const (
	liveMemberActor  = "201"
	liveForeignActor = "202"
)

// liveTenantDB provisions and installs the identity owner's own database through
// its real migration entrypoint.
func liveTenantDB(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN: dsn, Owner: "tenant", Database: "opl_tenant",
		SchemaOwnerRole: "opl_tenant_owner", WriterRole: "opl_tenant_writer", RuntimeRole: "opl_tenant_runtime",
	})
	if err != nil {
		t.Fatalf("provision isolated tenant database: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	source, err := identitymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatalf("install tenant migrations: %v", err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatalf("open tenant database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// liveGateway simulates only the external Sub2API authentication boundary.
// CloudIdentity still executes its real HTTP adapter, login, session issuance and
// policy.
func liveGateway(t *testing.T) *httptest.Server {
	t.Helper()
	actorFor := func(email string) (int64, bool) {
		switch email {
		case "serve-member@example.test":
			return 201, true
		case "other-member@example.test":
			return 202, true
		default:
			return 0, false
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/auth/login" && r.URL.Path != "/api/v1/auth/me" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var email string
		switch r.URL.Path {
		case "/api/v1/auth/login":
			var body struct{ Email, Password string }
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != livePassword {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			email = body.Email
		default:
			// The identity owner re-checks the live subject with the token it
			// received at login, so the simulated boundary must resolve that token
			// to the same subject.
			email = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer isolated-gateway-")
		}
		actor, known := actorFor(email)
		if !known {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		user := identity.GatewayIdentity{ID: actor, Email: email, Status: "active"}
		if r.URL.Path == "/api/v1/auth/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "isolated-gateway-" + email, "user": user}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": user})
	}))
	t.Cleanup(server.Close)
	return server
}

// liveIdentityChain is the running production CloudIdentity owner plus the real
// BFF login transport, so every session used below is issued by the production
// implementation rather than minted by the test.
type liveIdentityChain struct {
	client  bff.IdentityClient
	db      *sql.DB
	address string
	service *identity.Service
	cookies map[string]string
}

func newLiveIdentityChain(t *testing.T, ctx context.Context, dsn string) *liveIdentityChain {
	t.Helper()
	db := liveTenantDB(t, ctx, dsn)
	// Known test Tenant membership is a declared local deployment prerequisite,
	// exactly as the existing publisher identity chain documents.
	if _, err := db.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name) VALUES($1,'Serve Live Tenant'),($2,'Other Live Tenant')`, liveTenant, liveOtherTenant); err != nil {
		t.Fatalf("seed known local Tenants: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('member-serve',$1,$3,'admin'),('member-other',$2,$4,'admin')`,
		liveTenant, liveOtherTenant, liveMemberActor, liveForeignActor); err != nil {
		t.Fatalf("seed known local members: %v", err)
	}
	gateway, err := identity.NewGateway(liveGateway(t).URL)
	if err != nil {
		t.Fatal(err)
	}
	service, err := identity.New(db, gateway, bytes.Repeat([]byte("k"), 32), nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	config := ownerservice.Config{Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{}}
	for _, peer := range []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.Serve.Service(), owneridentity.Capability.Service(), owneridentity.Workspace.Service()} {
		config.Peers[peer] = liveIdentityToken
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(server); err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.ServeOn(listener) }()
	t.Cleanup(server.Stop)

	client, err := bff.DialIdentity(listener.Addr().String(), liveIdentityToken, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)

	chain := &liveIdentityChain{client: client, db: db, address: listener.Addr().String(), service: service, cookies: map[string]string{}}
	for name, email := range map[string]string{"serve": "serve-member@example.test", "other": "other-member@example.test"} {
		_, challenge, err := client.LoginContext(ctx)
		if err != nil {
			t.Fatalf("login challenge for %s: %v", name, err)
		}
		session, cookie, err := client.Login(ctx, challenge, &api.LoginRequest{Username: email, Password: livePassword})
		if err != nil {
			t.Fatalf("real login for %s: %v", name, err)
		}
		if session.GetActorId() == "" || cookie == "" {
			t.Fatalf("real login for %s issued no actor or cookie", name)
		}
		chain.cookies[name] = cookie
	}
	return chain
}

// bffReadCall builds the CallContext the Console BFF itself forwards to an owner:
// the exact shape httpapi.WithCaller produces from a resolved session (actor,
// hashed session reference, session scope, request id, deadline). It is
// reproduced here because apps/console-bff/internal is not importable outside its
// own module; the BFF's own delivery route is covered by the BFF's tests.
func bffReadCall(t *testing.T, ctx context.Context, chain *liveIdentityChain, name, requestID string) *api.CallContext {
	t.Helper()
	cookie := chain.cookies[name]
	session, err := chain.client.Session(ctx, cookie)
	if err != nil {
		t.Fatalf("read real session for %s: %v", name, err)
	}
	scope := &api.AuthorizationScope{}
	if tenantID := session.GetTenantId(); tenantID != "" {
		scope.Scope = &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenantID}}
	} else {
		scope.Scope = &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}
	}
	sessionID := owneridentity.SessionReference(cookie)
	return &api.CallContext{ActorId: session.GetActorId(), SessionId: &sessionID, Scope: scope, RequestId: requestID, DeadlineAt: timestamppb.New(time.Now().Add(30 * time.Second))}
}

// serveReadClient dials the running Serve owner as the Console BFF, presenting
// this test's isolated Serve token and asserting the observed identity.
func serveReadClient(t *testing.T, address string) api.ServeProductServiceClient {
	t.Helper()
	interceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		ctx = metadata.AppendToOutgoingContext(ctx, owneridentity.PeerHeader, string(owneridentity.ConsoleBFF), owneridentity.TokenHeader, liveServeToken)
		return invoke(ctx, method, req, reply, cc, opts...)
	}
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(interceptor))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return api.NewServeProductServiceClient(conn)
}

// serveReadAuthorityEvidence records, for the receipt, which reads the production
// authority admits. It is asserted in the subtest below.
const serveReadAuthorityEvidence = "production CloudIdentity policy: " +
	"audience=serve admits LISTDEPLOYMENTS, GETDEPLOYMENT and GETWORKSPACEACCESS for a tenant member " +
	"(services/gateway-integration/identity/policy_generated.go). GETWORKSPACEACCESS became a " +
	"serve-audience row when docs/spec/target/03_api_contract_complete.yaml corrected its owner to " +
	"serve; the proto already declared the RPC on ServeProductService."

// serveReadAuthorization is the exact authorization request Serve's own
// authorize() builds for one read: the caller's own scope and session, the
// audience Serve declares as its own owner, and the Workspace resource.
func serveReadAuthorization(call *api.CallContext, action api.AuthorizationActionEnum, workspaceID string, requestID string) *api.AuthorizationRequest {
	return &api.AuthorizationRequest{
		Scope:         call.GetScope(),
		ActorId:       call.GetActorId(),
		SessionId:     call.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_SERVE,
		Action:        action,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: &workspaceID},
		RequestId:     requestID,
	}
}

func requireServed(t *testing.T, err error, what string) {
	t.Helper()
	if err != nil {
		t.Fatalf("production CloudIdentity refused %s: %v\n%s", what, err, serveReadAuthorityEvidence)
	}
}

// TestLiveServeReadChain exercises the whole read slice against the production
// CloudIdentity implementation: a real session read through the BFF's own
// authenticated read contract reaches Serve's owner-local delivery history and
// current access state consistently, while the production authority still refuses
// cross-Tenant, unauthenticated and revoked callers.
func TestLiveServeReadChain(t *testing.T) {
	ctx := context.Background()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	chain := newLiveIdentityChain(t, ctx, dsn)

	serveDB, _, serveRuntimeDSN := fixture(t)
	// Seeded Serve-owned read-model rows. These are NOT a real Deploy: they
	// exercise the read projection only, and no Runtime observation is claimed.
	seedDeployment(t, serveDB, "ws-served", liveTenant, "dep-old", "superseded", "stopped", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	seedDeployment(t, serveDB, "ws-served", liveTenant, "dep-active", "active", "ready", "https://ws-served.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	seedDeployment(t, serveDB, "ws-pending", liveTenant, "dep-pending", "active", "starting", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	seedDeployment(t, serveDB, "ws-attempt", liveTenant, "dep-attempt", "queued", "stopped", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)

	config := ownerservice.Config{
		Owner:              ownerservice.OwnerServe,
		TLS:                owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers:              map[ownerservice.Service]string{owneridentity.ConsoleBFF: liveServeToken},
		CloudIdentityAddr:  chain.address,
		CloudIdentityToken: liveIdentityToken,
	}
	bootstrap, err := ownerservice.StartWithDatabase(ctx, serveDatabase(t, serveRuntimeDSN), config, noMigrations{}, configure(config))
	if err != nil {
		t.Fatalf("start serve owner process: %v", err)
	}
	t.Cleanup(func() { _ = bootstrap.Close() })
	if err = bootstrap.Server.Ready(ctx); err != nil {
		t.Fatalf("serve process not ready against the production authority: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = bootstrap.Server.ServeOn(listener) }()
	t.Cleanup(bootstrap.Server.Stop)
	client := serveReadClient(t, listener.Addr().String())

	// Ask the production authority directly, with the exact request Serve sends,
	// so a chain failure is attributed correctly: a policy denial at the
	// authority is not a Serve defect, and the shared authorizer collates every
	// authority error as Unavailable at the Serve boundary.
	t.Run("authority_admits_all_three_serve_reads", func(t *testing.T) {
		call := bffReadCall(t, ctx, chain, "serve", "live-direct-decision")
		for _, action := range []api.AuthorizationActionEnum{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETDEPLOYMENT,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS,
		} {
			request := serveReadAuthorization(call, action, "ws-served", "live-direct-"+action.String())
			decision, err := chain.client.Authorize(ctx, request)
			if err != nil {
				t.Fatalf("the production authority refused %s: %v\n%s", action, err, serveReadAuthorityEvidence)
			}
			if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED || decision.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_SERVE {
				t.Fatalf("the production authority did not admit %s: %+v", action, decision)
			}
		}
		// The authority decides audience, action and the caller's own role and scope;
		// it holds no Workspace-ownership fact, so it necessarily admits a tenant
		// member for a workspace-shaped resource. Refusing another Tenant's member
		// for this Workspace is Serve's own persisted-authority guard, which the
		// cross_tenant_is_refused subtest asserts end to end. Recording that split
		// keeps a future reader from "fixing" the authority by giving it a second
		// copy of Serve's ownership data.
		foreign := bffReadCall(t, ctx, chain, "other", "live-direct-foreign")
		decision, err := chain.client.Authorize(ctx, serveReadAuthorization(foreign, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS, "ws-served", "live-direct-foreign"))
		if err != nil || decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
			t.Fatalf("the authority no longer decides this read by audience/action/role alone: %v / %+v", err, decision)
		}
	})

	// Rejection paths that must hold whatever the policy table admits.
	t.Run("cross_tenant_is_refused", func(t *testing.T) {
		call := bffReadCall(t, ctx, chain, "other", "live-cross-tenant")
		if _, err := client.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call, WorkspaceId: "ws-served"}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("the other Tenant's member read ws-served: %v", err)
		}
	})
	t.Run("no_session_is_unauthenticated", func(t *testing.T) {
		bare := &api.CallContext{ActorId: liveMemberActor, Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: liveTenant}}}, RequestId: "live-no-session"}
		if _, err := client.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: bare, WorkspaceId: "ws-served"}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("a session-less read was not refused: %v", err)
		}
	})

	// The owning Tenant's live member must reach Serve's own delivery facts.
	t.Run("owning_member_reads_owner_truth", func(t *testing.T) {
		call := bffReadCall(t, ctx, chain, "serve", "live-history")
		page, err := client.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call, WorkspaceId: "ws-served"})
		requireServed(t, err, "ListDeployments for the owning Tenant's member")
		if len(page.GetItems()) != 2 {
			t.Fatalf("history length = %d, want 2 (one current Agent plus its retained predecessor)", len(page.GetItems()))
		}
		// Newest first: the current Agent leads the history rather than replacing it.
		if page.GetItems()[0].GetId() != "dep-active" || page.GetItems()[0].GetStatus() != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE {
			t.Fatalf("current deployment is not the newest active row: %+v", page.GetItems()[0])
		}
		if page.GetItems()[1].GetId() != "dep-old" || page.GetItems()[1].GetStatus() != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_SUPERSEDED {
			t.Fatalf("history did not retain the superseded deployment: %+v", page.GetItems()[1])
		}
		current, err := client.GetDeployment(ctx, &api.GetDeploymentRpcRequest{Context: call, WorkspaceId: "ws-served", DeploymentId: "dep-active"})
		requireServed(t, err, "GetDeployment for the owning Tenant's member")
		if current.GetRuntimeInstanceId() != "rt_dep-active" {
			t.Fatalf("current deployment runtime = %q, want rt_dep-active", current.GetRuntimeInstanceId())
		}

		// The access read is admitted and must report the same current Agent the
		// history leads with: the active deployment's own observed entry, never the
		// superseded predecessor's.
		access, err := client.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call, WorkspaceId: "ws-served"})
		requireServed(t, err, "GetWorkspaceAccess for the owning Tenant's member")
		if access.GetWorkspaceId() != "ws-served" || access.GetUrl() != "https://ws-served.example/app" || !access.GetApplicationCredentialsAvailable() {
			t.Fatalf("access readback = %+v", access)
		}
		if access.GetAuthenticationMode() != api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_APPLICATION_LOGIN {
			t.Fatalf("access mode = %v", access.GetAuthenticationMode())
		}
	})

	t.Run("pending_runtime_is_not_ready_in_history", func(t *testing.T) {
		call := bffReadCall(t, ctx, chain, "serve", "live-pending")
		page, err := client.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call, WorkspaceId: "ws-pending"})
		requireServed(t, err, "ListDeployments for a Workspace whose runtime is starting")
		if len(page.GetItems()) != 1 || page.GetItems()[0].GetStatus() != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE {
			t.Fatalf("pending history = %+v", page.GetItems())
		}
		// The current Agent is active while its application is not ready, so the
		// access read must publish no URL and claim no credentials: a provisioned
		// resource is not a ready application.
		access, err := client.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call, WorkspaceId: "ws-pending"})
		assertAccessUnavailable(t, access, err)
	})

	t.Run("never_delivered_workspace_has_empty_history", func(t *testing.T) {
		call := bffReadCall(t, ctx, chain, "serve", "live-never-delivered")
		page, err := client.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call, WorkspaceId: "ws-never-delivered"})
		requireServed(t, err, "ListDeployments for a Workspace Serve has never delivered")
		if len(page.GetItems()) != 0 {
			t.Fatalf("never-delivered Workspace reported history: %+v", page.GetItems())
		}
		access, err := client.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call, WorkspaceId: "ws-never-delivered"})
		assertAccessUnavailable(t, access, err)
	})

	t.Run("attempt_without_active_agent_publishes_no_entry", func(t *testing.T) {
		call := bffReadCall(t, ctx, chain, "serve", "live-attempt")
		page, err := client.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call, WorkspaceId: "ws-attempt"})
		requireServed(t, err, "ListDeployments for a Workspace with a queued attempt")
		if len(page.GetItems()) != 1 || page.GetItems()[0].GetStatus() != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_QUEUED {
			t.Fatalf("queued attempt history = %+v", page.GetItems())
		}
		access, err := client.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call, WorkspaceId: "ws-attempt"})
		assertAccessUnavailable(t, access, err)
	})

	t.Run("revoked_session_is_refused", func(t *testing.T) {
		call := bffReadCall(t, ctx, chain, "serve", "live-revoked")
		// The identity owner stores the session under its own hashed reference;
		// revoking that row is a real change in the identity owner's own store.
		// Member removal also revokes live sessions, but that RPC belongs to the
		// identity owner's separate member-governance work package.
		ref := owneridentity.SessionReference(chain.cookies["serve"])
		tag, err := chain.db.ExecContext(ctx, `UPDATE tenant.sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, ref)
		if err != nil {
			t.Fatal(err)
		}
		if affected, err := tag.RowsAffected(); err != nil || affected != 1 {
			t.Fatalf("revoked %d sessions, want 1 (err=%v)", affected, err)
		}
		// The production authority must itself refuse the revoked session.
		if _, err := chain.client.Authorize(ctx, serveReadAuthorization(call, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS, "ws-served", "live-revoked")); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("the production authority answered %v for a revoked session, want unauthenticated", err)
		}
		// Serve must fail closed as well. Its shared authorizer collates any
		// authority error as Unavailable, so the read is refused without leaking
		// the owner's facts; the exact denial code is asserted above against the
		// authority itself.
		if _, err := client.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call, WorkspaceId: "ws-served"}); err == nil {
			t.Fatal("a revoked session still read Serve's delivery history")
		} else if code := status.Code(err); code != codes.Unauthenticated && code != codes.PermissionDenied && code != codes.Unavailable {
			t.Fatalf("a revoked session produced %s, want a refusal", code)
		}
	})
}
