package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const cloudWorkspacePurchaseFixtureToken = "workspace-purchase-isolated-fixture-871"
const cloudWorkspacePurchasePath = "/internal/cloud/workspace-purchase-eligibility"

func newCloudWorkspacePurchaseFixture(t *testing.T, store StateStore) *cloudAnnouncementHTTPFixture {
	t.Helper()
	t.Setenv("OPL_CONTROL_PLANE_WORKSPACE_TOKEN", cloudWorkspacePurchaseFixtureToken)
	return newCloudAnnouncementHTTPFixture(t, store)
}

func workspacePurchaseHeaders(h http.Header) {
	h.Set(owneridentity.PeerHeader, owneridentity.Workspace.String())
	h.Set(owneridentity.TokenHeader, cloudWorkspacePurchaseFixtureToken)
}

func TestCloudWorkspacePurchaseEligibilityHTTPFacts(t *testing.T) {
	for _, backend := range cloudAnnouncementStores(t)[:2] {
		t.Run(backend.name, func(t *testing.T) {
			store := backend.new(t)
			f := newCloudWorkspacePurchaseFixture(t, store)
			seedCloudAnnouncementLegacyIdentity(t, store, "usr-purchase", 901)
			// Actor spelling is deliberately another CP User. Only the verified
			// external subject chooses the reciprocal Account, never the actor ID.
			ref := f.gateway.bind("purchase-facts", "usr-admin", "901")
			for _, enabled := range []bool{true, false, true} {
				account, _, err := store.GetAccount(t.Context(), "acct-usr-purchase")
				if err != nil {
					t.Fatal(err)
				}
				account["workspacePurchaseEnabled"] = enabled
				if err := store.SaveAccount(t.Context(), account); err != nil {
					t.Fatal(err)
				}
				before, err := store.ListAccounts(t.Context(), "acct-usr-purchase")
				if err != nil {
					t.Fatal(err)
				}
				result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath, ref, "", workspacePurchaseHeaders)
				if result.code != http.StatusOK {
					t.Fatalf("status=%d body=%s", result.code, result.body)
				}
				var fact contracts.WorkspacePurchaseEligibility
				decoder := json.NewDecoder(strings.NewReader(string(result.body)))
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&fact); err != nil {
					t.Fatal(err)
				}
				if err := decoder.Decode(new(any)); err != io.EOF {
					t.Fatalf("extra response JSON: %v", err)
				}
				if fact.ActorID != "usr-admin" || fact.AccountID != "acct-usr-purchase" || fact.WorkspacePurchaseEnabled == nil || *fact.WorkspacePurchaseEnabled != enabled {
					t.Fatalf("fact=%+v body=%s", fact, result.body)
				}
				after, err := store.ListAccounts(t.Context(), "acct-usr-purchase")
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatalf("read mutated accounts: before=%v after=%v err=%v", before, after, err)
				}
			}
			f.gateway.mu.Lock()
			defer f.gateway.mu.Unlock()
			if f.gateway.calls != 3 {
				t.Fatalf("session not revalidated on every read: calls=%d", f.gateway.calls)
			}
		})
	}
}

func TestCloudWorkspacePurchaseEligibilityHTTPRejectsUnknownFact(t *testing.T) {
	for _, tc := range []struct{ name, json string }{
		{"absent", `{}`}, {"null", `{"workspacePurchaseEnabled":null}`},
		{"string true", `{"workspacePurchaseEnabled":"true"}`}, {"string false", `{"workspacePurchaseEnabled":"false"}`},
		{"number", `{"workspacePurchaseEnabled":1}`}, {"object", `{"workspacePurchaseEnabled":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryTableStore()
			f := newCloudWorkspacePurchaseFixture(t, store)
			seedCloudAnnouncementLegacyIdentity(t, store, "usr-purchase", 901)
			account, _, _ := store.GetAccount(t.Context(), "acct-usr-purchase")
			var persisted map[string]any
			if err := json.Unmarshal([]byte(tc.json), &persisted); err != nil {
				t.Fatal(err)
			}
			for k, v := range persisted {
				account[k] = v
			}
			if err := store.SaveAccount(t.Context(), account); err != nil {
				t.Fatal(err)
			}
			ref := f.gateway.bind("purchase-unknown", "actor", "901")
			result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath, ref, "", workspacePurchaseHeaders)
			if result.code != http.StatusServiceUnavailable || strings.Contains(string(result.body), "workspacePurchaseEnabled") {
				t.Fatalf("unknown fact fabricated: status=%d body=%s", result.code, result.body)
			}
		})
	}
}

func TestCloudWorkspacePurchaseEligibilityHTTPRejectsAbsentAndBrokenLineage(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   int
		mutate func(*memoryTableStore)
	}{
		{"absent account", 403, func(s *memoryTableStore) { delete(s.accounts, "acct-admin") }},
		{"inactive account", 403, func(s *memoryTableStore) { s.accounts["acct-admin"]["status"] = "suspended" }},
		{"missing account status", 403, func(s *memoryTableStore) { delete(s.accounts["acct-admin"], "status") }},
		{"inactive user", 403, func(s *memoryTableStore) { s.users["usr-admin"]["status"] = "disabled" }},
		{"disabled user", 403, func(s *memoryTableStore) { s.users["usr-admin"]["disabledAt"] = "2026-11-04T00:00:00Z" }},
		{"deleted user", 403, func(s *memoryTableStore) { s.users["usr-admin"]["deletedAt"] = "2026-11-04T00:00:00Z" }},
		{"absent user", 409, func(s *memoryTableStore) { delete(s.users, "usr-admin") }},
		{"nonreciprocal user", 409, func(s *memoryTableStore) { s.users["usr-admin"]["accountId"] = "acct-other" }},
		{"wrong user id", 409, func(s *memoryTableStore) { s.users["usr-admin"]["id"] = "usr-other" }},
		{"missing owner", 409, func(s *memoryTableStore) { delete(s.accounts["acct-admin"], "ownerUserId") }},
		{"cloud alias owner", 409, func(s *memoryTableStore) { s.accounts["acct-admin"]["ownerUserId"] = "cloud-user:actor" }},
		{"missing account id", 409, func(s *memoryTableStore) { delete(s.accounts["acct-admin"], "id") }},
		{"ambiguous mapping", 409, func(s *memoryTableStore) {
			s.accounts["acct-other"] = map[string]any{"id": "acct-other", "ownerUserId": "usr-other", "sub2apiUserId": int64(1), "status": "active", "workspacePurchaseEnabled": true}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryTableStore()
			f := newCloudWorkspacePurchaseFixture(t, store)
			ref := f.gateway.bind("purchase-lineage", "actor", "1")
			store.mu.Lock()
			tc.mutate(store)
			before, _ := json.Marshal(store.accounts)
			store.mu.Unlock()
			result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath, ref, "", workspacePurchaseHeaders)
			if result.code != tc.code {
				t.Fatalf("status=%d want=%d body=%s", result.code, tc.code, result.body)
			}
			store.mu.Lock()
			after, _ := json.Marshal(store.accounts)
			store.mu.Unlock()
			if string(before) != string(after) {
				t.Fatalf("read created or changed Account: before=%s after=%s", before, after)
			}
		})
	}
}

func TestCloudWorkspacePurchaseEligibilityHTTPRejectsForgedRequests(t *testing.T) {
	f := newCloudWorkspacePurchaseFixture(t, newMemoryTableStore())
	ref := f.gateway.bind("purchase-headers", "actor", "1")
	for _, tc := range []struct {
		name, suffix, body string
		code               int
		mutate             func(http.Header)
	}{
		{name: "missing peer", code: 401, mutate: func(h http.Header) { h.Del(owneridentity.PeerHeader) }},
		{name: "BFF peer and token", code: 401, mutate: func(h http.Header) {
			h.Set(owneridentity.PeerHeader, owneridentity.ConsoleBFF.String())
			h.Set(owneridentity.TokenHeader, cloudAnnouncementFixtureToken)
		}},
		{name: "BFF token", code: 401, mutate: func(h http.Header) { h.Set(owneridentity.TokenHeader, cloudAnnouncementFixtureToken) }},
		{name: "outbound token", code: 401, mutate: func(h http.Header) { h.Set(owneridentity.TokenHeader, cloudAnnouncementFixtureIdentityToken) }},
		{name: "wrong peer", code: 401, mutate: func(h http.Header) { h.Set(owneridentity.PeerHeader, owneridentity.Fabric.String()) }},
		{name: "wrong token", code: 401, mutate: func(h http.Header) { h.Set(owneridentity.TokenHeader, "wrong-token") }},
		{name: "missing token", code: 401, mutate: func(h http.Header) { h.Del(owneridentity.TokenHeader) }},
		{name: "duplicate peer", code: 401, mutate: func(h http.Header) { h.Add(owneridentity.PeerHeader, owneridentity.Workspace.String()) }},
		{name: "duplicate token", code: 401, mutate: func(h http.Header) { h.Add(owneridentity.TokenHeader, cloudWorkspacePurchaseFixtureToken) }},
		{name: "joined peer", code: 401, mutate: func(h http.Header) { h.Set(owneridentity.PeerHeader, "workspace,workspace") }},
		{name: "joined token", code: 401, mutate: func(h http.Header) {
			h.Set(owneridentity.TokenHeader, cloudWorkspacePurchaseFixtureToken+","+cloudWorkspacePurchaseFixtureToken)
		}},
		{name: "missing session", code: 401, mutate: func(h http.Header) { h.Del(contracts.CloudSessionHeader) }},
		{name: "duplicate session", code: 401, mutate: func(h http.Header) { h.Add(contracts.CloudSessionHeader, ref) }},
		{name: "joined session", code: 401, mutate: func(h http.Header) { h.Set(contracts.CloudSessionHeader, ref+","+ref) }},
		{name: "raw session", code: 401, mutate: func(h http.Header) { h.Set(contracts.CloudSessionHeader, "raw-browser-cookie") }},
		{name: "actor header", code: 400, mutate: func(h http.Header) { h.Set("X-Opl-Actor-Id", "usr-admin") }},
		{name: "account header", code: 400, mutate: func(h http.Header) { h.Set("X-Opl-Account-Id", "acct-admin") }},
		{name: "tenant header", code: 400, mutate: func(h http.Header) { h.Set("X-Opl-Tenant", "tenant-other") }},
		{name: "user header", code: 400, mutate: func(h http.Header) { h.Set("X-Opl-Target-User", "usr-admin") }},
		{name: "actor query", suffix: "?actorId=usr-admin", code: 400},
		{name: "account query", suffix: "?accountId=acct-admin", code: 400},
		{name: "arbitrary query", suffix: "?page=1", code: 400},
		{name: "malformed query", suffix: "?actor=%zz", code: 400},
		{name: "empty query marker", suffix: "?", code: 400},
		{name: "empty parameter", suffix: "?&", code: 400},
		{name: "actor body", body: `{"actorId":"usr-admin"}`, code: 400},
		{name: "empty object body", body: `{}`, code: 400},
		{name: "null body", body: `null`, code: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mutate := []func(http.Header){workspacePurchaseHeaders}
			if tc.mutate != nil {
				mutate = append(mutate, tc.mutate)
			}
			result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath+tc.suffix, ref, tc.body, mutate...)
			if result.code != tc.code {
				t.Fatalf("status=%d want=%d body=%s", result.code, tc.code, result.body)
			}
		})
	}
	// All rejected inputs stop before the session owner or Account store.
	f.gateway.mu.Lock()
	defer f.gateway.mu.Unlock()
	if f.gateway.calls != 0 {
		t.Fatalf("forged request reached proof owner: calls=%d", f.gateway.calls)
	}
}

func TestCloudWorkspacePurchaseEligibilityHTTPRejectsChunkedBody(t *testing.T) {
	f := newCloudWorkspacePurchaseFixture(t, newMemoryTableStore())
	ref := f.gateway.bind("purchase-chunked", "actor", "1")
	r, err := http.NewRequest(http.MethodGet, f.server.URL+cloudWorkspacePurchasePath, io.NopCloser(strings.NewReader(`{"actorId":"forged"}`)))
	if err != nil {
		t.Fatal(err)
	}
	workspacePurchaseHeaders(r.Header)
	r.Header.Set(contracts.CloudSessionHeader, ref)
	r.ContentLength = -1
	r.TransferEncoding = []string{"chunked"}
	response, err := f.server.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 400 || response.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("chunked request status=%d headers=%v", response.StatusCode, response.Header)
	}
}

func TestCloudWorkspacePurchaseEligibilityHTTPRevalidatesSessionAndProof(t *testing.T) {
	for _, tc := range []struct {
		name string
		code codes.Code
		want int
	}{
		{"expired", codes.Unauthenticated, 401}, {"revoked binding", codes.PermissionDenied, 403},
		{"inactive binding", codes.FailedPrecondition, 403}, {"absent binding", codes.NotFound, 403},
		{"owner unavailable", codes.Unavailable, 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCloudWorkspacePurchaseFixture(t, newMemoryTableStore())
			ref := f.gateway.bind("purchase-session", "actor", "1")
			if result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath, ref, "", workspacePurchaseHeaders); result.code != 200 {
				t.Fatalf("initial=%d %s", result.code, result.body)
			}
			f.gateway.fail(ref, tc.code)
			result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath, ref, "", workspacePurchaseHeaders)
			if result.code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", result.code, tc.want, result.body)
			}
		})
	}
	for _, subject := range []string{"", "0", "-1", "01", "+1", "1.0", " 1", "9223372036854775808"} {
		t.Run("invalid subject "+subject, func(t *testing.T) {
			f := newCloudWorkspacePurchaseFixture(t, newMemoryTableStore())
			ref := f.gateway.bind("purchase-subject", "actor", subject)
			result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath, ref, "", workspacePurchaseHeaders)
			if result.code != 409 {
				t.Fatalf("subject=%q status=%d body=%s", subject, result.code, result.body)
			}
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*api.UserIdentityReadback)
	}{
		{"empty actor", func(p *api.UserIdentityReadback) { p.ActorId = "" }},
		{"padded actor", func(p *api.UserIdentityReadback) { p.ActorId = " actor " }},
		{"inactive proof", func(p *api.UserIdentityReadback) {
			p.Status = api.UserExternalBindingReadbackStatusEnum_USER_EXTERNAL_BINDING_READBACK_STATUS_ENUM_UNSPECIFIED
		}},
		{"unconfirmed proof", func(p *api.UserIdentityReadback) {
			p.Verification = api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_UNSPECIFIED
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newCloudWorkspacePurchaseFixture(t, newMemoryTableStore())
			ref := f.gateway.bind("purchase-proof", "actor", "1")
			f.gateway.mu.Lock()
			tc.mutate(f.gateway.bindings[ref])
			f.gateway.mu.Unlock()
			result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath, ref, "", workspacePurchaseHeaders)
			if result.code != 409 {
				t.Fatalf("status=%d body=%s", result.code, result.body)
			}
		})
	}
}

type cloudWorkspacePurchaseFailingStore struct {
	*memoryTableStore
	stage string
}

func (s *cloudWorkspacePurchaseFailingStore) GetAccountBySub2APIUserID(ctx context.Context, subject int64) (map[string]any, bool, error) {
	if s.stage == "account" {
		return nil, false, errors.New("fixture account persistence unavailable")
	}
	return s.memoryTableStore.GetAccountBySub2APIUserID(ctx, subject)
}
func (s *cloudWorkspacePurchaseFailingStore) GetUser(ctx context.Context, id string) (map[string]any, bool, error) {
	if s.stage == "user" {
		return nil, false, errors.New("fixture user persistence unavailable")
	}
	return s.memoryTableStore.GetUser(ctx, id)
}

func TestCloudWorkspacePurchaseEligibilityHTTPPersistenceUnavailable(t *testing.T) {
	for _, stage := range []string{"account", "user"} {
		t.Run(stage, func(t *testing.T) {
			store := &cloudWorkspacePurchaseFailingStore{memoryTableStore: newMemoryTableStore()}
			f := newCloudWorkspacePurchaseFixture(t, store)
			ref := f.gateway.bind("purchase-store", "actor", "1")
			store.stage = stage
			result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath, ref, "", workspacePurchaseHeaders)
			if result.code != 503 || strings.Contains(string(result.body), "workspacePurchaseEnabled") {
				t.Fatalf("status=%d body=%s", result.code, result.body)
			}
		})
	}
}

func TestCloudWorkspacePurchaseEligibilityHTTPUnconfiguredAndBFFIsolation(t *testing.T) {
	for _, source := range []string{"nil", "BFF only"} {
		t.Run(source, func(t *testing.T) {
			t.Setenv("OPL_CONTROL_PLANE_WORKSPACE_TOKEN", "")
			f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
			ref := f.gateway.bind("purchase-unconfigured", "actor", "1")
			if source == "nil" {
				original := f.handler.app.cloudAnnouncements
				t.Cleanup(func() { _ = original.Close() })
				f.handler.app.cloudAnnouncements = nil
			}
			for _, suffix := range []string{"", "?actorId=forged"} {
				result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath+suffix, ref, "", workspacePurchaseHeaders)
				if result.code != 503 {
					t.Fatalf("unconfigured status=%d body=%s", result.code, result.body)
				}
			}
		})
	}
	f := newCloudWorkspacePurchaseFixture(t, newMemoryTableStore())
	ref := f.gateway.bind("purchase-bff-fence", "actor", "1")
	for _, path := range []string{"/internal/cloud/announcements", "/internal/cloud/legacy-billing/receipts"} {
		result := f.request(t, http.MethodGet, path, ref, "", workspacePurchaseHeaders)
		if result.code != 401 {
			t.Fatalf("Workspace expanded BFF boundary %s: %d %s", path, result.code, result.body)
		}
		result = f.request(t, http.MethodGet, path, ref, "", func(h http.Header) { h.Set(owneridentity.TokenHeader, cloudWorkspacePurchaseFixtureToken) })
		if result.code != 401 {
			t.Fatalf("Workspace token accepted as BFF %s: %d %s", path, result.code, result.body)
		}
	}
	result := f.requestRaw(t, http.MethodPost, cloudWorkspacePurchasePath, ref, "", workspacePurchaseHeaders)
	if result.code != http.StatusNotFound {
		t.Fatalf("write method accepted: %d %s", result.code, result.body)
	}
	// Exercise the registered handler on a direct request with case-distinct
	// duplicate map keys, which are not represented by Header.Values alone.
	r := httptest.NewRequest(http.MethodGet, cloudWorkspacePurchasePath, nil)
	workspacePurchaseHeaders(r.Header)
	r.Header.Set(contracts.CloudSessionHeader, ref)
	r.Header[owneridentity.TokenHeader] = []string{cloudWorkspacePurchaseFixtureToken}
	w := httptest.NewRecorder()
	f.handler.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("case-distinct duplicate token accepted: %d %s", w.Code, w.Body.String())
	}
}

func TestCloudWorkspacePurchaseEligibilityHTTPWorkspaceOnlyConfiguration(t *testing.T) {
	f := newCloudWorkspacePurchaseFixture(t, newMemoryTableStore())
	t.Setenv("OPL_CONTROL_PLANE_BFF_TOKEN", "")
	handler, err := NewPersistentServer(f.handler.service, f.store)
	if err != nil {
		t.Fatal(err)
	}
	typed := handler.(*controlPlaneHTTPHandler)
	t.Cleanup(func() { _ = typed.Close() })
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	f.server = server
	ref := f.gateway.bind("purchase-workspace-only", "actor", "1")
	result := f.request(t, http.MethodGet, cloudWorkspacePurchasePath, ref, "", workspacePurchaseHeaders)
	if result.code != 200 {
		t.Fatalf("Workspace-only source rejected: %d %s", result.code, result.body)
	}
	var fact contracts.WorkspacePurchaseEligibility
	if err := json.Unmarshal(result.body, &fact); err != nil {
		t.Fatal(err)
	}
	if fact.ActorID != "actor" || fact.AccountID != "acct-admin" || fact.WorkspacePurchaseEnabled == nil || !*fact.WorkspacePurchaseEnabled {
		t.Fatalf("fact=%+v", fact)
	}
	result = f.request(t, http.MethodGet, "/internal/cloud/announcements", ref, "")
	if result.code != 503 {
		t.Fatalf("Workspace-only env enabled BFF: %d %s", result.code, result.body)
	}
}
