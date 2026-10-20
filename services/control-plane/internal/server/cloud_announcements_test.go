package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/announcement"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func TestCloudAnnouncementCurrentHeadBaseline(t *testing.T) {
	f := newAnnouncementFixture(t)
	r := httptest.NewRequest(http.MethodGet, "/api/announcements", nil)
	r.AddCookie(&http.Cookie{Name: "opl_session", Value: "unknown-cloud-cookie"})
	w := httptest.NewRecorder()
	f.server.ServeHTTP(w, r)
	t.Logf("retained unknown Cloud credential: status=%d body=%s", w.Code, w.Body.String())
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("retained status=%d", w.Code)
	}
}

func TestCloudAnnouncementUnconfiguredOwnerBoundary(t *testing.T) {
	f := newAnnouncementFixture(t)
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/internal/cloud/announcements"},
		{http.MethodPost, "/internal/cloud/announcements/notice/read"},
	} {
		w := httptest.NewRecorder()
		f.server.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		t.Logf("%s %s: status=%d body=%s", tc.method, tc.path, w.Code, w.Body.String())
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Cloud owner boundary must report unconfigured source, got %d", w.Code)
		}
	}
}

const cloudAnnouncementFixtureToken = "bff-announcement-isolated-fixture-871"
const cloudAnnouncementFixtureIdentityToken = "cloud-identity-isolated-fixture-871"

// This isolated GatewayCoordination fixture proves the real HTTP/2 transport,
// caller credentials and request shape. Session/binding outcomes are controlled
// here; Gateway's own PostgreSQL tests own persisted proof validation.
type announcementGatewayFixture struct {
	api.UnimplementedGatewayCoordinationServer
	mu       sync.Mutex
	bindings map[string]*api.UserIdentityReadback
	failures map[string]codes.Code
	calls    int
}

func (g *announcementGatewayFixture) GetSelfUserIdentity(ctx context.Context, r *api.GetSelfUserIdentityRequest) (*api.UserIdentityReadback, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	md, _ := metadata.FromIncomingContext(ctx)
	if len(md.Get(owneridentity.PeerHeader)) != 1 || md.Get(owneridentity.PeerHeader)[0] != owneridentity.ControlPlane.String() || len(md.Get(owneridentity.TokenHeader)) != 1 || md.Get(owneridentity.TokenHeader)[0] != cloudAnnouncementFixtureIdentityToken {
		return nil, status.Error(codes.PermissionDenied, "fixture requires the Control Plane transport principal")
	}
	call := r.GetContext()
	if call == nil || call.GetSessionId() == "" || !proto.Equal(call, &api.CallContext{SessionId: proto.String(call.GetSessionId())}) {
		return nil, status.Error(codes.InvalidArgument, "fixture accepts only a session reference, no actor/tenant/target")
	}
	if code := g.failures[call.GetSessionId()]; code != codes.OK {
		return nil, status.Error(code, "isolated fixture session/binding rejection")
	}
	binding := g.bindings[call.GetSessionId()]
	if binding == nil {
		return nil, status.Error(codes.Unauthenticated, "fixture session expired or unknown")
	}
	return proto.Clone(binding).(*api.UserIdentityReadback), nil
}

func (g *announcementGatewayFixture) bind(raw, actor, subject string) string {
	ref := owneridentity.SessionReference(raw)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.bindings[ref] = &api.UserIdentityReadback{ActorId: actor, Sub2ApiUserId: subject,
		Status:                  api.UserExternalBindingReadbackStatusEnum_USER_EXTERNAL_BINDING_READBACK_STATUS_ENUM_ACTIVE,
		Verification:            api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_CONFIRMED,
		VerificationEvidenceRef: "sub2api:user:" + subject, VerifiedAt: timestamppb.Now()}
	return ref
}

func (g *announcementGatewayFixture) fail(ref string, code codes.Code) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.failures[ref] = code
}

type cloudAnnouncementHTTPFixture struct {
	server  *httptest.Server
	handler *controlPlaneHTTPHandler
	gateway *announcementGatewayFixture
	store   StateStore
}

func newCloudAnnouncementHTTPFixture(t *testing.T, store StateStore, services ...*controlplane.Service) *cloudAnnouncementHTTPFixture {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gateway := &announcementGatewayFixture{bindings: map[string]*api.UserIdentityReadback{}, failures: map[string]codes.Code{}}
	grpcServer := grpc.NewServer()
	api.RegisterGatewayCoordinationServer(grpcServer, gateway)
	go func() { _ = grpcServer.Serve(listener) }()
	t.Cleanup(grpcServer.Stop)
	t.Setenv("OPL_CONTROL_PLANE_BFF_TOKEN", cloudAnnouncementFixtureToken)
	t.Setenv("OPL_CLOUD_IDENTITY_URL", listener.Addr().String())
	t.Setenv("OPL_CLOUD_IDENTITY_TOKEN", cloudAnnouncementFixtureIdentityToken)
	t.Setenv("OPL_GRPC_INSECURE_LOCAL", "1")
	t.Setenv("NODE_ENV", "test")
	service := controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, &testSub2APIClient{balance: 1_000_000, charges: map[string]int64{}})
	if len(services) > 0 {
		service = services[0]
	}
	handler, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	typed := handler.(*controlPlaneHTTPHandler)
	t.Cleanup(func() { _ = typed.Close() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &cloudAnnouncementHTTPFixture{server: server, handler: typed, gateway: gateway, store: store}
}

type cloudAnnouncementHTTPResponse struct {
	code   int
	body   []byte
	header http.Header
}

func (response cloudAnnouncementHTTPResponse) cookies() []*http.Cookie {
	return (&http.Response{Header: response.header}).Cookies()
}

func (f *cloudAnnouncementHTTPFixture) request(t *testing.T, method, path, ref, body string, mutate ...func(http.Header)) cloudAnnouncementHTTPResponse {
	t.Helper()
	response := f.requestRaw(t, method, path, ref, body, mutate...)
	if response.header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("missing private cache policy: %v", response.header)
	}
	return response
}

// requestRaw returns the observed response without enforcing the cache policy,
// so a regression test can assert the status of an unregistered route before
// the success-path policy is available.
func (f *cloudAnnouncementHTTPFixture) requestRaw(t *testing.T, method, path, ref, body string, mutate ...func(http.Header)) cloudAnnouncementHTTPResponse {
	t.Helper()
	r, err := http.NewRequest(method, f.server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set(owneridentity.PeerHeader, owneridentity.ConsoleBFF.String())
	r.Header.Set(owneridentity.TokenHeader, cloudAnnouncementFixtureToken)
	r.Header.Set(contracts.CloudSessionHeader, ref)
	for _, fn := range mutate {
		fn(r.Header)
	}
	response, err := f.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return cloudAnnouncementHTTPResponse{code: response.StatusCode, body: payload, header: response.Header.Clone()}
}

func (response cloudAnnouncementHTTPResponse) page(t *testing.T) *contracts.AnnouncementPage {
	t.Helper()
	var envelope contracts.AnnouncementSource
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatal(err)
	}
	if response.code != http.StatusOK || envelope.Source != "control-plane" || !envelope.Available || envelope.Data == nil || envelope.FetchedAt == "" {
		t.Fatalf("Cloud source status=%d body=%s", response.code, response.body)
	}
	return envelope.Data
}

func (response cloudAnnouncementHTTPResponse) read(t *testing.T) contracts.AnnouncementRead {
	t.Helper()
	var ack contracts.AnnouncementRead
	if err := json.Unmarshal(response.body, &ack); err != nil {
		t.Fatal(err)
	}
	if response.code != http.StatusOK || ack.ReadAt == "" || ack.AnnouncementID == "" {
		t.Fatalf("Cloud ACK status=%d body=%s", response.code, response.body)
	}
	return ack
}

func seedCloudAnnouncement(t *testing.T, store StateStore, id, state string, start, end time.Time) {
	t.Helper()
	endsAt := ""
	if !end.IsZero() {
		endsAt = end.UTC().Format(time.RFC3339Nano)
	}
	_, err := store.ApplyAnnouncementMutation(t.Context(), announcementMutation{AnnouncementID: id, Create: true, RequestHash: id,
		Patch:      map[string]any{"title": "Isolated notice " + id, "body": "Original Control Plane owner", "status": state, "startsAt": start.UTC().Format(time.RFC3339Nano), "endsAt": endsAt, "publishedAt": start.UTC().Format(time.RFC3339Nano), "createdByUserId": "usr-admin", "updatedByUserId": "usr-admin"},
		AuditEvent: map[string]any{"id": "audit-" + id, "action": "announcement.create", "resourceKind": "announcement", "resourceId": id, "actorUserId": "usr-admin", "result": "succeeded"}})
	if err != nil {
		t.Fatal(err)
	}
}

func seedCloudAnnouncementLegacyIdentity(t *testing.T, store StateStore, id string, subject int64) {
	t.Helper()
	if err := store.CreateProvisionedAccount(t.Context(), map[string]any{"id": "acct-" + id, "ownerUserId": id, "sub2apiUserId": subject, "status": "active"},
		map[string]any{"id": id, "accountId": "acct-" + id, "email": id + "@fixture.example", "role": "owner", "status": "active"}); err != nil {
		t.Fatal(err)
	}
}

func cloudAnnouncementStores(t *testing.T) []struct {
	name string
	new  func(*testing.T) StateStore
} {
	return []struct {
		name string
		new  func(*testing.T) StateStore
	}{
		{"memory", func(t *testing.T) StateStore { return newMemoryTableStore() }},
		{"sqlite", func(t *testing.T) StateStore {
			return NewTestEntStateStore(t, t.TempDir()+"/cloud-announcements.sqlite")
		}},
		{"postgres", func(t *testing.T) StateStore { store, _ := newPostgresWorkspaceRenewalStoreWithDB(t); return store }},
	}
}

func TestCloudAnnouncementsOwnerHTTPIsolationInheritanceAndLaterMapping(t *testing.T) {
	for _, backend := range cloudAnnouncementStores(t) {
		t.Run(backend.name, func(t *testing.T) {
			store := backend.new(t)
			f := newCloudAnnouncementHTTPFixture(t, store)
			now := time.Now().UTC()
			seedCloudAnnouncement(t, store, "active", "published", now.Add(-48*time.Hour), now.Add(time.Hour))
			for id, state := range map[string]string{"draft": "draft", "withdrawn": "withdrawn", "future": "scheduled", "expired": "published"} {
				starts, ends := now.Add(-time.Hour), now.Add(time.Hour)
				if id == "future" {
					starts, ends = now.Add(time.Hour), now.Add(2*time.Hour)
				}
				if id == "expired" {
					starts, ends = now.Add(-2*time.Hour), now.Add(-time.Hour)
				}
				seedCloudAnnouncement(t, store, id, state, starts, ends)
			}
			seedCloudAnnouncementLegacyIdentity(t, store, "usr-legacy-a", 901)
			original := now.Add(-time.Hour).In(time.FixedZone("fixture", 8*60*60)).Format(time.RFC3339Nano)
			if _, err := store.MarkAnnouncementRead(t.Context(), "active", "usr-legacy-a", original); err != nil {
				t.Fatal(err)
			}
			alice := f.gateway.bind("alice-session", "opaque-cloud-a", "901")
			// Actor spelling deliberately equals another retained User; the verified
			// external subject is different and must never inherit that User's ACK.
			bob := f.gateway.bind("bob-session", "usr-legacy-a", "902")
			alicePage := f.request(t, http.MethodGet, "/internal/cloud/announcements", alice, "").page(t)
			if alicePage.Total != 1 || len(alicePage.Items) != 1 || !alicePage.Items[0].Read {
				t.Fatalf("inherited page=%+v", alicePage)
			}
			bobPage := f.request(t, http.MethodGet, "/internal/cloud/announcements", bob, "").page(t)
			if bobPage.Total != 1 || bobPage.Items[0].Read {
				t.Fatalf("another subject inherited ACK: %+v", bobPage)
			}
			first := f.request(t, http.MethodPost, "/internal/cloud/announcements/active/read", alice, "").read(t)
			repeat := f.request(t, http.MethodPost, "/internal/cloud/announcements/active/read", alice, "{}").read(t)
			if first.ReadAt != original || repeat.ReadAt != original {
				t.Fatalf("original timestamp lost: %s/%s want %s", first.ReadAt, repeat.ReadAt, original)
			}
			accountsBefore, _ := store.ListAccounts(t.Context(), "")
			bobFirst := f.request(t, http.MethodPost, "/internal/cloud/announcements/active/read", bob, "").read(t)
			accountsAfter, _ := store.ListAccounts(t.Context(), "")
			if len(accountsAfter) != len(accountsBefore) {
				t.Fatal("Cloud read/ACK created an Account")
			}
			cloudRows, err := store.ListAnnouncementReads(t.Context(), "cloud-user:usr-legacy-a")
			if err != nil || len(cloudRows) != 1 || cloudRows[0]["readAt"] != bobFirst.ReadAt {
				t.Fatalf("Cloud-owned reference=%v err=%v", cloudRows, err)
			}
			// A reciprocal original-owner mapping appears later. GET must immediately
			// see the exact alias; ACK folds it into legacy, keeping the earlier Cloud ACK.
			seedCloudAnnouncementLegacyIdentity(t, store, "usr-legacy-b", 902)
			later := now.Add(30 * time.Minute).Format(time.RFC3339Nano)
			if _, err := store.MarkAnnouncementRead(t.Context(), "active", "usr-legacy-b", later); err != nil {
				t.Fatal(err)
			}
			mappedPage := f.request(t, http.MethodGet, "/internal/cloud/announcements", bob, "").page(t)
			if !mappedPage.Items[0].Read {
				t.Fatal("later mapping forgot own Cloud ACK")
			}
			for i := 0; i < 2; i++ {
				if got := f.request(t, http.MethodPost, "/internal/cloud/announcements/active/read", bob, "").read(t); got.ReadAt != bobFirst.ReadAt {
					t.Fatalf("later mapping overwrote Cloud timestamp: %+v", got)
				}
			}
			cloudRows, _ = store.ListAnnouncementReads(t.Context(), "cloud-user:usr-legacy-a")
			legacyRows, _ := store.ListAnnouncementReads(t.Context(), "usr-legacy-b")
			if len(cloudRows) != 0 || len(legacyRows) != 1 || legacyRows[0]["readAt"] != bobFirst.ReadAt {
				t.Fatalf("not folded atomically: cloud=%v legacy=%v", cloudRows, legacyRows)
			}
			// An earlier Cloud alias must not hide an even earlier proven legacy ACK.
			if _, err := store.MarkCloudAnnouncementRead(t.Context(), "active", "cloud-user:opaque-cloud-a", "", now.Add(-30*time.Minute).Format(time.RFC3339Nano)); err != nil {
				t.Fatal(err)
			}
			if got := f.request(t, http.MethodPost, "/internal/cloud/announcements/active/read", alice, "").read(t); got.ReadAt != original {
				t.Fatalf("legacy earliest lost: %+v", got)
			}
			cloudRows, _ = store.ListAnnouncementReads(t.Context(), "cloud-user:opaque-cloud-a")
			if len(cloudRows) != 0 {
				t.Fatal("redundant Cloud alias retained after ACK")
			}
			for _, id := range []string{"draft", "withdrawn", "future", "expired", "missing"} {
				result := f.request(t, http.MethodPost, "/internal/cloud/announcements/"+id+"/read", alice, "")
				if result.code != http.StatusNotFound {
					t.Fatalf("inactive %s=%d %s", id, result.code, result.body)
				}
			}
			withdrawn := announcementMutation{AnnouncementID: "active", RequestHash: "withdraw", AllowedStatuses: []string{"published"}, Patch: map[string]any{"status": "withdrawn"}, AuditEvent: map[string]any{"id": "audit-withdraw-active", "action": "announcement.withdraw", "resourceKind": "announcement", "resourceId": "active", "actorUserId": "usr-admin", "result": "succeeded"}}
			if _, err := store.ApplyAnnouncementMutation(t.Context(), withdrawn); err != nil {
				t.Fatal(err)
			}
			replay := f.request(t, http.MethodPost, "/internal/cloud/announcements/active/read", alice, "")
			if replay.code != http.StatusNotFound || strings.Contains(string(replay.body), "readAt") {
				t.Fatalf("withdrawn existing ACK leaked: %d %s", replay.code, replay.body)
			}
			empty := f.request(t, http.MethodGet, "/internal/cloud/announcements", alice, "").page(t)
			if empty.Total != 0 || len(empty.Items) != 0 {
				t.Fatalf("withdrawn page=%+v", empty)
			}
		})
	}
}

func TestCloudAnnouncementsOwnerHTTPRejectsIdentitySelectorsAndDuplicateHeaders(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	ref := f.gateway.bind("headers", "actor", "901")
	cases := []struct {
		name, method, path, body string
		code                     int
		mutate                   func(http.Header)
	}{
		{name: "missing peer", code: 401, mutate: func(h http.Header) { h.Del(owneridentity.PeerHeader) }},
		{name: "wrong peer", code: 401, mutate: func(h http.Header) { h.Set(owneridentity.PeerHeader, "workspace") }},
		{name: "wrong token", code: 401, mutate: func(h http.Header) { h.Set(owneridentity.TokenHeader, "incorrect-credential") }},
		{name: "duplicate peer", code: 401, mutate: func(h http.Header) { h.Add(owneridentity.PeerHeader, owneridentity.ConsoleBFF.String()) }},
		{name: "duplicate token", code: 401, mutate: func(h http.Header) { h.Add(owneridentity.TokenHeader, cloudAnnouncementFixtureToken) }},
		{name: "duplicate session", code: 401, mutate: func(h http.Header) { h.Add(contracts.CloudSessionHeader, ref) }},
		{name: "joined session", code: 401, mutate: func(h http.Header) { h.Set(contracts.CloudSessionHeader, ref+","+ref) }},
		{name: "raw cookie instead of reference", code: 401, mutate: func(h http.Header) { h.Set(contracts.CloudSessionHeader, "browser-cookie") }},
		{name: "actor header", code: 400, mutate: func(h http.Header) { h.Set("x-opl-actor-id", "other-user") }},
		{name: "tenant header", code: 400, mutate: func(h http.Header) { h.Set("x-opl-tenant", "other-tenant") }},
		{name: "target user header", code: 400, mutate: func(h http.Header) { h.Set("x-opl-target-user", "usr-legacy-a") }},
		{name: "target user query", path: "/internal/cloud/announcements?userId=usr-legacy-a", code: 400},
		{name: "tenant query", path: "/internal/cloud/announcements?tenantId=tenant", code: 400},
		{name: "duplicate pagination", path: "/internal/cloud/announcements?page=1&page=2", code: 400},
		{name: "malformed query", path: "/internal/cloud/announcements?actor=%zz", code: 400},
		{name: "target ACK body", method: http.MethodPost, path: "/internal/cloud/announcements/active/read", body: `{"userId":"usr-legacy-a"}`, code: 400},
		{name: "null ACK body", method: http.MethodPost, path: "/internal/cloud/announcements/active/read", body: `null`, code: 400},
		{name: "multiple ACK objects", method: http.MethodPost, path: "/internal/cloud/announcements/active/read", body: `{} {}`, code: 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			method, path := tc.method, tc.path
			if method == "" {
				method = http.MethodGet
			}
			if path == "" {
				path = "/internal/cloud/announcements"
			}
			var mutate []func(http.Header)
			if tc.mutate != nil {
				mutate = append(mutate, tc.mutate)
			}
			result := f.request(t, method, path, ref, tc.body, mutate...)
			if result.code != tc.code {
				t.Fatalf("status=%d want %d body=%s", result.code, tc.code, result.body)
			}
		})
	}
	f.gateway.mu.Lock()
	defer f.gateway.mu.Unlock()
	if f.gateway.calls != 0 {
		t.Fatalf("rejected selectors/headers reached proof owner: %d calls", f.gateway.calls)
	}
}

func TestCloudAnnouncementsOwnerHTTPRevalidatesSessionAndBinding(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	ref := f.gateway.bind("revalidate", "actor", "901")
	seedCloudAnnouncement(t, f.store, "active", "published", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	f.request(t, http.MethodPost, "/internal/cloud/announcements/active/read", ref, "").read(t)
	for _, tc := range []struct {
		name string
		code codes.Code
		want int
	}{
		{"expired", codes.Unauthenticated, 401}, {"revoked", codes.FailedPrecondition, 403}, {"denied", codes.PermissionDenied, 403}, {"missing binding", codes.NotFound, 403}, {"unavailable", codes.Unavailable, 503}, {"timeout", codes.DeadlineExceeded, 503}, {"broken proof owner", codes.Internal, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f.gateway.fail(ref, tc.code)
			for _, method := range []string{http.MethodGet, http.MethodPost} {
				path := "/internal/cloud/announcements"
				if method == http.MethodPost {
					path += "/active/read"
				}
				result := f.request(t, method, path, ref, "")
				if result.code != tc.want || strings.Contains(string(result.body), "readAt") || strings.Contains(string(result.body), `"items"`) {
					t.Fatalf("proof failure became data: %d %s", result.code, result.body)
				}
			}
		})
	}
}

func TestCloudAnnouncementsOwnerHTTPRejectsBrokenOriginalLineage(t *testing.T) {
	for _, backend := range cloudAnnouncementStores(t) {
		t.Run(backend.name, func(t *testing.T) {
			store := backend.new(t)
			f := newCloudAnnouncementHTTPFixture(t, store)
			seedCloudAnnouncementLegacyIdentity(t, store, "usr-legacy", 901)
			ref := f.gateway.bind("lineage", "cloud-actor", "901")
			for _, tc := range []struct {
				name   string
				mutate func(map[string]any)
			}{
				{"disabled", func(u map[string]any) { u["status"] = "disabled"; u["disabledAt"] = "2026-10-15T01:00:00Z" }},
				{"deleted", func(u map[string]any) { u["status"] = "deleted"; u["deletedAt"] = "2026-10-15T01:00:00Z" }},
				{"wrong reciprocal account", func(u map[string]any) { u["accountId"] = "acct-other" }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					u, _, _ := store.GetUser(t.Context(), "usr-legacy")
					u["status"] = "active"
					u["disabledAt"] = ""
					u["deletedAt"] = ""
					u["accountId"] = "acct-usr-legacy"
					tc.mutate(u)
					if tc.name == "wrong reciprocal account" {
						if memory, ok := store.(*memoryTableStore); ok {
							// Deliberate corruption fixture; the real memory writer rejects it.
							memory.mu.Lock()
							memory.users["usr-legacy"] = cloneMap(u)
							memory.mu.Unlock()
						} else {
							entStore := store.(*postgresEntStateStore)
							_, err := entStore.client.User.UpdateOneID("usr-legacy").SetAccountID("acct-other").Save(t.Context())
							if backend.name == "postgres" {
								if !controlplaneent.IsConstraintError(err) {
									t.Fatalf("PostgreSQL reciprocal lineage constraint=%v", err)
								}
								return
							}
							if err != nil {
								t.Fatal(err)
							}
						}
					} else if err := store.ApplyUserLifecycle(t.Context(), u); err != nil {
						t.Fatal(err)
					}
					for _, method := range []string{http.MethodGet, http.MethodPost} {
						path := "/internal/cloud/announcements"
						if method == http.MethodPost {
							path += "/notice/read"
						}
						result := f.request(t, method, path, ref, "")
						if result.code != http.StatusConflict {
							t.Fatalf("unproven original lineage=%d %s", result.code, result.body)
						}
					}
				})
			}
			if err := store.DeleteUser(t.Context(), "usr-legacy"); err != nil {
				if backend.name == "postgres" && controlplaneent.IsConstraintError(err) {
					return
				}
				t.Fatal(err)
			}
			result := f.request(t, http.MethodGet, "/internal/cloud/announcements", ref, "")
			if result.code != 409 {
				t.Fatalf("missing owner user=%d %s", result.code, result.body)
			}
		})
	}
}

func TestCloudAnnouncementsOwnerHTTPRejectsAmbiguousOriginalSubject(t *testing.T) {
	store := newMemoryTableStore()
	f := newCloudAnnouncementHTTPFixture(t, store)
	seedCloudAnnouncementLegacyIdentity(t, store, "usr-one", 901)
	// Corruption fixture deliberately bypasses the real uniqueness-constrained
	// writer. A duplicate subject must fail, never select the first row.
	store.mu.Lock()
	store.accounts["acct-duplicate"] = map[string]any{"id": "acct-duplicate", "ownerUserId": "usr-other", "sub2apiUserId": int64(901), "status": "active"}
	store.mu.Unlock()
	ref := f.gateway.bind("ambiguous", "actor", "901")
	result := f.request(t, http.MethodGet, "/internal/cloud/announcements", ref, "")
	if result.code != 409 {
		t.Fatalf("ambiguous original subject=%d %s", result.code, result.body)
	}
}

func TestCloudAnnouncementsOwnerHTTPPaginationCannotOverflow(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	ref := f.gateway.bind("pages", "actor", "901")
	seedCloudAnnouncement(t, f.store, "active", "published", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	for _, page := range []int{int(^uint(0) >> 1), int(^uint(0)>>1)/20 + 2, 2} {
		result := f.request(t, http.MethodGet, fmt.Sprintf("/internal/cloud/announcements?page=%d&pageSize=20", page), ref, "")
		got := result.page(t)
		if got.Total != 1 || got.Page != page || len(got.Items) != 0 {
			t.Fatalf("overflow page=%+v", got)
		}
	}
}

func TestCloudAnnouncementConfigurationFailsClosedAndClosesConnection(t *testing.T) {
	base := map[string]string{"OPL_CONTROL_PLANE_BFF_TOKEN": cloudAnnouncementFixtureToken, "OPL_CLOUD_IDENTITY_URL": "127.0.0.1:1", "OPL_CLOUD_IDENTITY_TOKEN": cloudAnnouncementFixtureIdentityToken, "OPL_GRPC_INSECURE_LOCAL": "1", "NODE_ENV": "test"}
	for _, key := range []string{"OPL_CONTROL_PLANE_BFF_TOKEN", "OPL_CLOUD_IDENTITY_URL", "OPL_CLOUD_IDENTITY_TOKEN"} {
		t.Run("missing "+key, func(t *testing.T) {
			_, err := cloudAnnouncementSourceFromEnv(func(k string) string {
				if k == key {
					return ""
				}
				return base[k]
			})
			if err == nil {
				t.Fatal("partial configuration accepted")
			}
		})
	}
	for _, tc := range []struct{ name, key, value string }{
		{"short inbound", "OPL_CONTROL_PLANE_BFF_TOKEN", "short"}, {"short outbound", "OPL_CLOUD_IDENTITY_TOKEN", "short"},
		{"shared token", "OPL_CONTROL_PLANE_BFF_TOKEN", cloudAnnouncementFixtureIdentityToken},
		{"production requires TLS", "NODE_ENV", "production"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := cloudAnnouncementSourceFromEnv(func(k string) string {
				if k == tc.key {
					return tc.value
				}
				return base[k]
			})
			if source != nil {
				_ = source.Close()
			}
			if err == nil {
				t.Fatal("unsafe configuration accepted")
			}
		})
	}
	source, err := cloudAnnouncementSourceFromEnv(func(string) string { return "" })
	if err != nil || source != nil {
		t.Fatalf("unconfigured source=%v err=%v", source, err)
	}
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	if err := f.handler.Close(); err != nil {
		t.Fatal(err)
	}
	if f.handler.app.cloudAnnouncements.conn.GetState() != connectivity.Shutdown {
		t.Fatal("Close leaked CloudIdentity connection")
	}
}

// Unavailable owner state must not become a successful empty source.
type cloudAnnouncementFailingStore struct {
	*memoryTableStore
	stage string
}

func (s *cloudAnnouncementFailingStore) ListAnnouncements(ctx context.Context) ([]map[string]any, error) {
	if s.stage == "announcements" {
		return nil, errors.New("fixture persistence unavailable")
	}
	return s.memoryTableStore.ListAnnouncements(ctx)
}
func (s *cloudAnnouncementFailingStore) ListAnnouncementReads(ctx context.Context, id string) ([]map[string]any, error) {
	if s.stage == "reads" {
		return nil, errors.New("fixture persistence unavailable")
	}
	return s.memoryTableStore.ListAnnouncementReads(ctx, id)
}
func (s *cloudAnnouncementFailingStore) GetAccountBySub2APIUserID(ctx context.Context, id int64) (map[string]any, bool, error) {
	if s.stage == "identity" {
		return nil, false, errors.New("fixture persistence unavailable")
	}
	return s.memoryTableStore.GetAccountBySub2APIUserID(ctx, id)
}
func (s *cloudAnnouncementFailingStore) MarkCloudAnnouncementRead(ctx context.Context, id, cloud, legacy, at string) (map[string]any, error) {
	if s.stage == "ack" {
		return nil, errors.New("fixture persistence unavailable")
	}
	return s.memoryTableStore.MarkCloudAnnouncementRead(ctx, id, cloud, legacy, at)
}

func TestCloudAnnouncementsOwnerHTTPPersistenceFailureIsUnavailable(t *testing.T) {
	for _, stage := range []string{"announcements", "reads", "identity", "ack"} {
		t.Run(stage, func(t *testing.T) {
			store := &cloudAnnouncementFailingStore{memoryTableStore: newMemoryTableStore(), stage: stage}
			f := newCloudAnnouncementHTTPFixture(t, store)
			ref := f.gateway.bind("unavailable", "actor", "901")
			method, path := http.MethodGet, "/internal/cloud/announcements"
			if stage == "ack" {
				method = http.MethodPost
				path += "/active/read"
			}
			result := f.request(t, method, path, ref, "")
			var envelope contracts.AnnouncementSource
			if err := json.Unmarshal(result.body, &envelope); err != nil {
				t.Fatal(err)
			}
			if result.code != 503 || envelope.Available || envelope.Status != "unavailable" || envelope.Data != nil || envelope.ReasonCode == "" || envelope.FetchedAt == "" {
				t.Fatalf("failure became empty success: %d %s", result.code, result.body)
			}
		})
	}
}

func TestCloudAnnouncementsOwnerHTTPRejectsInvalidBindingProof(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*api.UserIdentityReadback)
	}{
		{"empty actor", func(b *api.UserIdentityReadback) { b.ActorId = "" }},
		{"empty subject", func(b *api.UserIdentityReadback) { b.Sub2ApiUserId = "" }},
		{"invalid subject", func(b *api.UserIdentityReadback) { b.Sub2ApiUserId = "not-an-id" }},
		{"ambiguous numeric subject", func(b *api.UserIdentityReadback) { b.Sub2ApiUserId = "0901" }},
		{"inactive", func(b *api.UserIdentityReadback) {
			b.Status = api.UserExternalBindingReadbackStatusEnum_USER_EXTERNAL_BINDING_READBACK_STATUS_ENUM_UNSPECIFIED
		}},
		{"unverified", func(b *api.UserIdentityReadback) {
			b.Verification = api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_UNSPECIFIED
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
			ref := f.gateway.bind("invalid-proof", "actor", "901")
			f.gateway.mu.Lock()
			tc.mutate(f.gateway.bindings[ref])
			f.gateway.mu.Unlock()
			result := f.request(t, http.MethodGet, "/internal/cloud/announcements", ref, "")
			if result.code != 409 {
				t.Fatalf("invalid binding proof accepted: %d %s", result.code, result.body)
			}
		})
	}
}

func TestCloudAnnouncementsOwnerHTTPRejectsInactiveOriginalAccount(t *testing.T) {
	for _, backend := range cloudAnnouncementStores(t) {
		t.Run(backend.name, func(t *testing.T) {
			store := backend.new(t)
			f := newCloudAnnouncementHTTPFixture(t, store)
			seedCloudAnnouncementLegacyIdentity(t, store, "usr-legacy", 901)
			ref := f.gateway.bind("inactive-account", "actor", "901")
			for _, state := range []string{"disabled", "deleted"} {
				account, _, _ := store.GetAccount(t.Context(), "acct-usr-legacy")
				account["status"] = state
				if memory, ok := store.(*memoryTableStore); ok {
					if err := memory.SaveAccount(t.Context(), account); err != nil {
						t.Fatal(err)
					}
				} else if _, err := store.(*postgresEntStateStore).client.Account.UpdateOneID("acct-usr-legacy").SetStatus(state).Save(t.Context()); err != nil {
					t.Fatal(err)
				}
				result := f.request(t, http.MethodGet, "/internal/cloud/announcements", ref, "")
				if result.code != 409 {
					t.Fatalf("inactive original Account accepted: %d %s", result.code, result.body)
				}
			}
		})
	}
}

func TestCloudAnnouncementPersistentServerRejectsPartialConfiguration(t *testing.T) {
	t.Setenv("OPL_CONTROL_PLANE_BFF_TOKEN", cloudAnnouncementFixtureToken)
	t.Setenv("OPL_CLOUD_IDENTITY_URL", "")
	t.Setenv("OPL_CLOUD_IDENTITY_TOKEN", "")
	store := newMemoryTableStore()
	before, _ := store.ListAccounts(t.Context(), "")
	_, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, nil), store)
	if err == nil {
		t.Fatal("persistent server accepted partial Cloud configuration")
	}
	accounts, _ := store.ListAccounts(t.Context(), "")
	if len(accounts) != len(before) {
		t.Fatal("invalid startup mutated legacy identity before rejecting Cloud configuration")
	}
}

func TestPostgresCloudAnnouncementsConcurrentACKAndWithdrawal(t *testing.T) {
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	now := time.Now().UTC()
	seedCloudAnnouncement(t, store, "concurrent", "published", now.Add(-48*time.Hour), now.Add(time.Hour))
	legacy, cloud := "usr-concurrent", "cloud-user:actor-concurrent"
	// Compare time values across offsets, preserving the winning timestamp bytes.
	legacyAt := now.Add(-3 * time.Hour).In(time.FixedZone("fixture", 8*60*60)).Format(time.RFC3339)
	cloudAt := now.Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)
	if _, err := store.MarkAnnouncementRead(t.Context(), "concurrent", legacy, legacyAt); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkCloudAnnouncementRead(t.Context(), "concurrent", cloud, "", cloudAt); err != nil {
		t.Fatal(err)
	}
	const writers = 16
	results := make(chan error, writers)
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			row, err := store.MarkCloudAnnouncementRead(t.Context(), "concurrent", cloud, legacy, now.Format(time.RFC3339Nano))
			if err == nil && stringValue(row["readAt"]) != legacyAt {
				err = fmt.Errorf("concurrent ACK changed timestamp: %v", row)
			}
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	oldRows, err := store.ListAnnouncementReads(t.Context(), cloud)
	if err != nil || len(oldRows) != 0 {
		t.Fatalf("Cloud alias survived: %v %v", oldRows, err)
	}
	canonical, err := store.ListAnnouncementReads(t.Context(), legacy)
	if err != nil || len(canonical) != 1 || canonical[0]["readAt"] != legacyAt {
		t.Fatalf("canonical ACK=%v %v", canonical, err)
	}
	// Withdrawing holds the same publication row lock used by the new ACK writer.
	tx, err := store.client.Tx(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Announcement.Query().Where(announcement.IDEQ("concurrent"), lockRowForUpdate).Only(t.Context()); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := store.MarkCloudAnnouncementRead(t.Context(), "concurrent", cloud, legacy, now.Format(time.RFC3339Nano))
		finished <- err
	}()
	if _, err := tx.Announcement.UpdateOneID("concurrent").SetStatus("withdrawn").Save(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, errAnnouncementNotActive) {
		t.Fatalf("withdrawal leaked replay: %v", err)
	}
}
