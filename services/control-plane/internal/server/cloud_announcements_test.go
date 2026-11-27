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
	"opl-cloud/services/control-plane/internal/controlplane"
)

const cloudAnnouncementFixtureToken = "bff-announcement-isolated-fixture-871"
const cloudAnnouncementFixtureIdentityToken = "cloud-identity-isolated-fixture-871"

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
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, _, err := store.getSQLRecord(t.Context(), tx, "control_plane_announcements", "concurrent", announcementEntFields, true); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() {
		_, err := store.MarkCloudAnnouncementRead(t.Context(), "concurrent", cloud, legacy, now.Format(time.RFC3339Nano))
		finished <- err
	}()
	if _, err := store.execSQL(t.Context(), tx, `UPDATE control_plane_announcements SET status = ? WHERE id = ?`, "withdrawn", "concurrent"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, errAnnouncementNotActive) {
		t.Fatalf("withdrawal leaked replay: %v", err)
	}
}

func TestCloudIdentityReadCallerConfiguration(t *testing.T) {
	base := map[string]string{
		"OPL_CLOUD_IDENTITY_URL": "127.0.0.1:1", "OPL_CLOUD_IDENTITY_TOKEN": cloudAnnouncementFixtureIdentityToken,
		"OPL_GRPC_INSECURE_LOCAL": "1", "NODE_ENV": "test",
	}
	t.Run("BFF configured", func(t *testing.T) {
		getenv := func(k string) string {
			if k == "OPL_CONTROL_PLANE_BFF_TOKEN" {
				return cloudAnnouncementFixtureToken
			}
			return base[k]
		}
		source, err := cloudAnnouncementSourceFromEnv(getenv)
		if err != nil || source == nil {
			t.Fatalf("source=%v err=%v", source, err)
		}
		defer source.Close()
		if source.token != cloudAnnouncementFixtureToken {
			t.Fatal("caller credential changed or implicitly substituted")
		}
	})
	for _, missing := range []string{"OPL_CLOUD_IDENTITY_URL", "OPL_CLOUD_IDENTITY_TOKEN"} {
		t.Run("OPL_CONTROL_PLANE_BFF_TOKEN requires "+missing, func(t *testing.T) {
			source, err := cloudAnnouncementSourceFromEnv(func(k string) string {
				if k == missing {
					return ""
				}
				if k == "OPL_CONTROL_PLANE_BFF_TOKEN" {
					return cloudAnnouncementFixtureToken
				}
				return base[k]
			})
			if source != nil {
				defer source.Close()
			}
			if err == nil {
				t.Fatal("partial caller configuration accepted")
			}
		})
	}
	t.Run("OPL_CONTROL_PLANE_BFF_TOKEN short", func(t *testing.T) {
		source, err := cloudAnnouncementSourceFromEnv(func(k string) string {
			if k == "OPL_CONTROL_PLANE_BFF_TOKEN" {
				return "short"
			}
			return base[k]
		})
		if source != nil {
			defer source.Close()
		}
		if err == nil {
			t.Fatal("short caller token accepted")
		}
	})
	for _, outboundKey := range []string{"OPL_CLOUD_IDENTITY_TOKEN", "OPL_FABRIC_SERVICE_TOKEN", "OPL_LEDGER_SERVICE_TOKEN", "OPL_FABRIC_CAPABILITY_KEY", "OPL_LEDGER_CAPABILITY_KEY"} {
		t.Run("OPL_CONTROL_PLANE_BFF_TOKEN cannot reuse "+outboundKey, func(t *testing.T) {
			source, err := cloudAnnouncementSourceFromEnv(func(k string) string {
				if k == "OPL_CONTROL_PLANE_BFF_TOKEN" || k == outboundKey {
					return cloudAnnouncementFixtureIdentityToken
				}
				return base[k]
			})
			if source != nil {
				defer source.Close()
			}
			if err == nil {
				t.Fatal("caller reused outbound credential")
			}
		})
	}
}
