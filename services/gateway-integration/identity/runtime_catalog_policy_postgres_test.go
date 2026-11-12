package identity_test

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	bff "opl-cloud/apps/console-bff"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
)

// Only the Runtime release store is simulated. Both BFF authorization and the
// receiving owner's Authorizer use the real CloudIdentity evaluator over gRPC,
// live Gateway identity readback and isolated Tenant/Gateway PostgreSQL stores.
type runtimeCatalogPolicyOwner struct {
	api.RuntimeControlProductServiceClient
	auth       *ownerservice.Authorizer
	registered *api.CallContext
	listed     *api.CallContext
	items      []*api.RuntimeVersion
}

func (p *runtimeCatalogPolicyOwner) RegisterRuntimeVersion(ctx context.Context, r *api.RegisterRuntimeVersionRpcRequest, _ ...grpc.CallOption) (*api.RuntimeVersion, error) {
	if err := p.auth.Authorize(ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF), r.Context,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REGISTERRUNTIMEVERSION,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}, ownerservice.ResourceScope{}); err != nil {
		return nil, err
	}
	p.registered = proto.Clone(r.Context).(*api.CallContext)
	v := &api.RuntimeVersion{Id: "runtime-policy", Name: r.Body.Name, VersionLabel: r.Body.VersionLabel,
		Status: api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED}
	p.items = append(p.items, v)
	return v, nil
}

func (p *runtimeCatalogPolicyOwner) ListRuntimeVersions(ctx context.Context, r *api.ListRuntimeVersionsRpcRequest, _ ...grpc.CallOption) (*api.RuntimeVersionPage, error) {
	if err := p.auth.Authorize(ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF), r.Context,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG},
		ownerservice.ResourceScope{TenantID: r.GetContext().GetScope().GetTenant().GetTenantId()}); err != nil {
		return nil, err
	}
	p.listed = proto.Clone(r.Context).(*api.CallContext)
	return &api.RuntimeVersionPage{Items: p.items}, nil
}

func TestRuntimeCatalogTenantlessPlatformAdminPolicyPostgres(t *testing.T) {
	system := newMemberSystem(t)
	cookie, session := memberLogin(t, system.client, "platform@example.test")
	var memberships int
	if err := system.db.QueryRowContext(t.Context(), `SELECT count(*) FROM tenant.tenant_members WHERE actor_id=$1 AND revoked_at IS NULL`, session.ActorId).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if memberships != 0 || session.TenantId != nil || session.Role != nil {
		t.Fatalf("fixture must be a platform administrator without Tenant membership: count=%d session=%v", memberships, session)
	}

	const token = "isolated-runtime-catalog-policy-token-0001"
	server, err := ownerservice.NewServer(ownerservice.Config{
		Owner: owneridentity.Tenant, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: token, owneridentity.RuntimeControl.Service(): token, owneridentity.Build.Service(): token},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := system.service.Register(server); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)
	auth, conn, err := ownerservice.AuthorizerFromConfig(ownerservice.Config{
		Owner: owneridentity.RuntimeControl, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true},
		CloudIdentityAddr: listener.Addr().String(), CloudIdentityToken: token,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	reader, err := bff.DialIdentity(listener.Addr().String(), token, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reader.Close)
	owner := &runtimeCatalogPolicyOwner{auth: auth}
	handler := bff.NewPublisherHandler(nil, owner, nil, reader)
	request := func(method, path, body string, csrf bool) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(&http.Cookie{Name: "opl_console_session", Value: cookie})
		if method == http.MethodPost {
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", "runtime-policy-registration")
			if csrf {
				r.Header.Set("X-CSRF-Token", session.CsrfToken)
			}
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	const body = `{"name":"opl-app","versionLabel":"v1","publisherNamespaceId":"publisher-policy","admissionReceiptId":"receipt-policy"}`
	// This was the reachable bug: the same tenantless session could POST 201,
	// then the real generated policy refused its GET with 403.
	if w := request(http.MethodPost, "/api/v2/admin/catalog/runtime-versions", body, true); w.Code != http.StatusCreated {
		t.Fatalf("platform runtime registration status=%d body=%s", w.Code, w.Body.String())
	}
	if w := request(http.MethodGet, "/api/v2/catalog/runtime-versions", "", false); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"runtime-policy"`) {
		t.Fatalf("tenantless platform administrator cannot read registered runtime: status=%d body=%s", w.Code, w.Body.String())
	}
	for _, call := range []*api.CallContext{owner.registered, owner.listed} {
		if call.GetActorId() != session.ActorId || call.GetScope().GetPlatform() == nil || call.GetScope().GetTenant() != nil || call.GetAuthorizationContextId() == "" || call.GetSessionId() != owneridentity.SessionReference(cookie) {
			t.Fatalf("BFF lost the exact platform authority before owner revalidation: %v", call)
		}
	}
	for _, actual := range []*api.Session{session, mustRuntimeCatalogSession(t, reader, cookie)} {
		for _, action := range []api.AuthorizationActionEnum{api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REGISTERRUNTIMEVERSION} {
			found := false
			for _, permission := range actual.Permissions {
				found = found || permission == action
			}
			if !found {
				t.Fatalf("login/session readback omitted %v", action)
			}
		}
	}

	t.Run("write-guard", func(t *testing.T) {
		if w := request(http.MethodPost, "/api/v2/admin/catalog/runtime-versions", body, false); w.Code != http.StatusForbidden || len(owner.items) != 1 {
			t.Fatalf("missing CSRF reached runtime registration: status=%d items=%d", w.Code, len(owner.items))
		}
	})
	t.Run("owner-context-boundary", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			change func(*api.CallContext)
		}{
			{"registration-context", func(c *api.CallContext) { c.AuthorizationContextId = owner.registered.AuthorizationContextId }},
			{"other-actor", func(c *api.CallContext) { c.ActorId = "701" }},
			{"other-tenant", func(c *api.CallContext) {
				c.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-b"}}}
			}},
		} {
			t.Run(tc.name, func(t *testing.T) {
				call := proto.Clone(owner.listed).(*api.CallContext)
				tc.change(call)
				if _, err := owner.ListRuntimeVersions(t.Context(), &api.ListRuntimeVersionsRpcRequest{Context: call}); status.Code(err) != codes.PermissionDenied {
					t.Fatalf("owner accepted widened read context: %v", err)
				}
			})
		}
		if err := auth.Authorize(ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF), owner.listed,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS,
			&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, Id: proto.String("other-catalog")}, ownerservice.ResourceScope{}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("owner accepted another resource under the original context: %v", err)
		}
		options, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(owneridentity.Build.Service(), owneridentity.Tenant.Service(), token)
		if err != nil {
			t.Fatal(err)
		}
		foreign, err := grpc.NewClient(listener.Addr().String(), options...)
		if err != nil {
			t.Fatal(err)
		}
		defer foreign.Close()
		if _, err := api.NewCloudIdentityAuthorizationClient(foreign).AuthorizeAction(t.Context(), &api.AuthorizationRequest{
			Scope: owner.listed.Scope, ActorId: owner.listed.ActorId, SessionId: owner.listed.SessionId,
			AudienceOwner: api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS,
			Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}, RequestId: "foreign-runtime-peer",
		}); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("foreign service originated a Runtime read: %v", err)
		}
		if err := authorizeAt(t, system, session, cookie, api.OwnerEnum_OWNER_ENUM_BUILD, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", "", "wrong-runtime-audience"); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("Runtime read authorized for another audience: %v", err)
		}
	})
	t.Run("revoked-session", func(t *testing.T) {
		if err := reader.Logout(t.Context(), owneridentity.SessionReference(cookie)); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.ListRuntimeVersions(t.Context(), &api.ListRuntimeVersionsRpcRequest{Context: owner.listed}); status.Code(err) != codes.Unauthenticated {
			t.Fatalf("owner must reject the revoked session before reusing the read context: %v", err)
		}
		if w := request(http.MethodGet, "/api/v2/catalog/runtime-versions", "", false); w.Code != http.StatusUnauthorized {
			t.Fatalf("BFF accepted a revoked platform session: status=%d", w.Code)
		}
	})
}

func mustRuntimeCatalogSession(t *testing.T, reader bff.IdentityReader, cookie string) *api.Session {
	t.Helper()
	session, err := reader.Session(t.Context(), cookie)
	if err != nil {
		t.Fatal(err)
	}
	// Session's public projection must retain the same permission enum as login.
	raw, err := publicjson.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &api.Session{}
	if err := publicjson.Unmarshal(raw, decoded); err != nil || !proto.Equal(session, decoded) {
		t.Fatalf("session permissions changed across public JSON: %v", err)
	}
	return decoded
}

func TestRuntimeCatalogPolicyTenantAndRoleBoundariesPostgres(t *testing.T) {
	system := newMemberSystem(t)
	if _, err := system.db.ExecContext(t.Context(), `INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('runtime-member','tenant-a','701','member'),('runtime-admin','tenant-a','702','admin'),('runtime-owner','tenant-a','703','owner')`); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"member", "admin", "owner", "invitee"} {
		t.Run(role, func(t *testing.T) {
			cookie, session := memberLogin(t, system.client, role+"@example.test")
			for _, tenantID := range []string{"tenant-a", "tenant-b", ""} {
				err := authorizeAt(t, system, session, cookie, api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", tenantID, "runtime-read-"+role)
				if tenantID == "tenant-a" && role != "invitee" {
					if err != nil {
						t.Fatalf("tenant %s lost its member read: %v", role, err)
					}
				} else if status.Code(err) != codes.PermissionDenied {
					t.Fatalf("%s read outside its Tenant under %q: %v", role, tenantID, err)
				}
				for _, action := range []api.AuthorizationActionEnum{
					api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REGISTERRUNTIMEVERSION,
					api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETRUNTIMEVERSIONSTATUS,
					api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETBUILDRUNTIMEPOLICY,
				} {
					if err := authorizeAt(t, system, session, cookie, api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL, action, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", tenantID, "runtime-write-"+role); status.Code(err) != codes.PermissionDenied {
						t.Fatalf("tenant %s inherited platform write %v under %q: %v", role, action, tenantID, err)
					}
				}
			}
		})
	}
	// Granting this catalog action must not turn platform administration into a
	// blanket substitute for membership on customer-only reads.
	cookie, session := memberLogin(t, system.client, "platform@example.test")
	if err := authorizeAt(t, system, session, cookie, api.OwnerEnum_OWNER_ENUM_SERVE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "workspace-runtime", "", "no-blanket-platform-read"); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("runtime catalog permission widened a customer-only read: %v", err)
	}
	// A well-formed, consumable decision remains bound to Runtime Control.
	r := &api.AuthorizationRequest{Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}, ActorId: session.ActorId, SessionId: proto.String(owneridentity.SessionReference(cookie)), AudienceOwner: api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}, RequestId: "runtime-decision-binding"}
	d, err := system.auth.AuthorizeAction(t.Context(), r)
	if err != nil {
		t.Fatal(err)
	}
	if err := owneridentity.ValidateDecision(r, d, time.Now()); err != nil {
		t.Fatalf("Runtime Control cannot consume its read decision: %v", err)
	}
}
