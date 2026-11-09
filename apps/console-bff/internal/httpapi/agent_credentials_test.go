package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const agentCredentialFixtureSecret = "synthetic-agent-credential-secret"
const agentCredentialFixtureReference = "agent:credential-1"

// This double models the typed identity owner, not bearer parsing or BFF route
// policy. Its decisions bind only the restricted child reference; accidentally
// forwarding the parent session therefore cannot pass the owner boundary.
type agentCredentialIdentity struct {
	mu             sync.Mutex
	browser        *api.Session
	resolved       *api.ResolvedAgentCredential
	issued         []*api.IssueAgentCredentialRpcRequest
	revocations    []*api.RevokeAgentCredentialRpcRequest
	lists          []*api.ListAgentCredentialsRpcRequest
	authorizations []*api.AuthorizationRequest
	resolves       int
	sessionReads   int
	revoked        bool
	denyResolveAt  int
}

func newAgentCredentialIdentity() *agentCredentialIdentity {
	now := time.Now()
	metadata := &api.AgentCredential{Id: "credential-1", Label: "My Agent", TenantId: proto.String("tenant-1"), Audience: agentCredentialAudience,
		IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(time.Hour))}
	return &agentCredentialIdentity{
		browser: &api.Session{ActorId: "actor-1", TenantId: proto.String("tenant-1"), DisplayName: "Account owner", CsrfToken: "csrf-1", ExpiresAt: timestamppb.New(now.Add(12 * time.Hour))},
		resolved: &api.ResolvedAgentCredential{SessionReference: agentCredentialFixtureReference, Metadata: metadata,
			Session: &api.Session{ActorId: "actor-1", TenantId: proto.String("tenant-1"), ExpiresAt: metadata.ExpiresAt}},
	}
}

func (f *agentCredentialIdentity) Session(_ context.Context, credential string) (*api.Session, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sessionReads++
	if credential != "session-1" {
		return nil, status.Error(codes.Unauthenticated, "browser session required")
	}
	return proto.Clone(f.browser).(*api.Session), nil
}

func (f *agentCredentialIdentity) Authorize(_ context.Context, request *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authorizations = append(f.authorizations, proto.Clone(request).(*api.AuthorizationRequest))
	if f.revoked || request.GetSessionId() != agentCredentialFixtureReference {
		return nil, status.Error(codes.PermissionDenied, "restricted credential required")
	}
	decision := allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "")
	decision.Resource.Id = nil
	decision.SessionId = proto.String(agentCredentialFixtureReference)
	return decision, nil
}

func (f *agentCredentialIdentity) IssueAgentCredential(_ context.Context, request *api.IssueAgentCredentialRpcRequest) (*api.IssuedAgentCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.issued = append(f.issued, proto.Clone(request).(*api.IssueAgentCredentialRpcRequest))
	metadata := proto.Clone(f.resolved.Metadata).(*api.AgentCredential)
	metadata.Label = request.Label
	return &api.IssuedAgentCredential{Metadata: metadata, Credential: agentCredentialFixtureSecret}, nil
}

func (f *agentCredentialIdentity) ResolveAgentCredential(_ context.Context, request *api.ResolveAgentCredentialRequest) (*api.ResolvedAgentCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resolves++
	if request.GetCredential() != agentCredentialFixtureSecret || request.GetAudience() != agentCredentialAudience || f.revoked || (f.denyResolveAt != 0 && f.resolves >= f.denyResolveAt) {
		return nil, status.Error(codes.Unauthenticated, "credential unavailable")
	}
	return proto.Clone(f.resolved).(*api.ResolvedAgentCredential), nil
}

func (f *agentCredentialIdentity) ListAgentCredentials(_ context.Context, request *api.ListAgentCredentialsRpcRequest) (*api.AgentCredentialPage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lists = append(f.lists, proto.Clone(request).(*api.ListAgentCredentialsRpcRequest))
	metadata := proto.Clone(f.resolved.Metadata).(*api.AgentCredential)
	if f.revoked {
		metadata.RevokedAt = timestamppb.Now()
	}
	return &api.AgentCredentialPage{Items: []*api.AgentCredential{metadata}}, nil
}

func (f *agentCredentialIdentity) RevokeAgentCredential(_ context.Context, request *api.RevokeAgentCredentialRpcRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revocations = append(f.revocations, proto.Clone(request).(*api.RevokeAgentCredentialRpcRequest))
	if request.GetCredentialId() != "credential-1" {
		return status.Error(codes.NotFound, "credential not found")
	}
	f.revoked = true
	return nil
}

func credentialBrowserRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, "http://cloud.test"+path, strings.NewReader(body))
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-1"})
	request.Header.Set("Origin", "http://cloud.test")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-1")
	return request
}

func TestAgentCredentialBrowserDownloadAndMetadataCustody(t *testing.T) {
	identity := newAgentCredentialIdentity()
	identity.browser.DisplayName = "<script>untrusted display name</script>"
	handler := (&Server{identity: identity}).Handler()
	issue := httptest.NewRecorder()
	handler.ServeHTTP(issue, credentialBrowserRequest(http.MethodPost, "/api/v2/auth/agent-credentials", `{"label":"My trusted host"}`))
	if issue.Code != http.StatusCreated || !strings.Contains(issue.Header().Get("Content-Disposition"), "attachment;") || issue.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("credential issuance is not a private download: status=%d", issue.Code)
	}
	var downloaded map[string]any
	if err := json.Unmarshal(issue.Body.Bytes(), &downloaded); err != nil {
		t.Fatal(err)
	}
	if downloaded["credential"] != agentCredentialFixtureSecret || downloaded["id"] != "credential-1" || downloaded["label"] != "My trusted host" || downloaded["audience"] != agentCredentialAudience || downloaded["tenantId"] != "tenant-1" {
		t.Fatal("download does not preserve the owner's credential and metadata")
	}
	if len(identity.issued) != 1 || identity.issued[0].Context.GetSessionId() != owneridentity.SessionReference("session-1") || identity.issued[0].Context.GetActorId() != "actor-1" || identity.issued[0].Context.GetScope().GetTenant().GetTenantId() != "tenant-1" || identity.issued[0].Context.GetRequestId() == "" {
		t.Fatal("issuance did not use the resolved browser caller")
	}
	for _, path := range []string{"/api/v2/auth/agent-credentials", "/api/v2/agent/connections"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, credentialBrowserRequest(http.MethodGet, path, ""))
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("private metadata/page unavailable: %s status=%d", path, response.Code)
		}
		for _, secret := range []string{agentCredentialFixtureSecret, "session-1", agentCredentialFixtureReference} {
			if strings.Contains(response.Body.String(), secret) {
				t.Fatalf("metadata/page leaked a credential or internal reference: %s", path)
			}
		}
		if path == "/api/v2/agent/connections" {
			if !strings.Contains(response.Body.String(), `<form id="issue">`) || strings.Contains(response.Body.String(), "<script>untrusted display name</script>") || !strings.Contains(response.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
				t.Fatal("connection page lacks its guarded human entry or escapes")
			}
		} else if strings.Contains(response.Body.String(), "csrf-1") {
			t.Fatal("credential metadata leaked browser CSRF")
		}
	}
	revoke := httptest.NewRecorder()
	handler.ServeHTTP(revoke, credentialBrowserRequest(http.MethodDelete, "/api/v2/auth/agent-credentials/credential-1", ""))
	if revoke.Code != http.StatusNoContent || len(identity.revocations) != 1 || identity.revocations[0].Context.GetSessionId() != owneridentity.SessionReference("session-1") || identity.revocations[0].CredentialId != "credential-1" {
		t.Fatal("revocation did not forward the browser caller and exact credential ID")
	}
	list := httptest.NewRecorder()
	handler.ServeHTTP(list, credentialBrowserRequest(http.MethodGet, "/api/v2/auth/agent-credentials", ""))
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), "revokedAt") || strings.Contains(list.Body.String(), agentCredentialFixtureSecret) {
		t.Fatal("revoked metadata is missing or exposed secret bytes")
	}
}

func TestAgentCredentialBrowserGuards(t *testing.T) {
	for _, tc := range []struct {
		name, method, path, body string
		mutate                   func(*http.Request)
		want                     int
	}{
		{"issue requires cookie", "POST", "/api/v2/auth/agent-credentials", `{"label":"host"}`, func(r *http.Request) { r.Header.Del("Cookie") }, 401},
		{"list requires cookie", "GET", "/api/v2/auth/agent-credentials", "", func(r *http.Request) { r.Header.Del("Cookie") }, 401},
		{"page requires cookie", "GET", "/api/v2/agent/connections", "", func(r *http.Request) { r.Header.Del("Cookie") }, 401},
		{"issue requires csrf", "POST", "/api/v2/auth/agent-credentials", `{"label":"host"}`, func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, 403},
		{"revoke requires csrf", "DELETE", "/api/v2/auth/agent-credentials/credential-1", "", func(r *http.Request) { r.Header.Set("X-CSRF-Token", "wrong") }, 403},
		{"issue requires origin", "POST", "/api/v2/auth/agent-credentials", `{"label":"host"}`, func(r *http.Request) { r.Header.Del("Origin") }, 403},
		{"revoke requires same origin", "DELETE", "/api/v2/auth/agent-credentials/credential-1", "", func(r *http.Request) { r.Header.Set("Origin", "https://foreign.test") }, 403},
		{"cross-site cannot mint", "POST", "/api/v2/auth/agent-credentials", `{"label":"host"}`, func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"caller cannot widen scope", "POST", "/api/v2/auth/agent-credentials", `{"label":"host","tenantId":"foreign"}`, nil, 400},
		{"trailing payload rejected", "POST", "/api/v2/auth/agent-credentials", `{"label":"host"} {"actorId":"foreign"}`, nil, 400},
		{"JSON required", "POST", "/api/v2/auth/agent-credentials", `{"label":"host"}`, func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }, 415},
		{"bearer cannot mint", "POST", "/api/v2/auth/agent-credentials", `{"label":"host"}`, func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+agentCredentialFixtureSecret) }, 401},
		{"bearer cannot revoke", "DELETE", "/api/v2/auth/agent-credentials/credential-1", "", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+agentCredentialFixtureSecret) }, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := newAgentCredentialIdentity()
			request := credentialBrowserRequest(tc.method, tc.path, tc.body)
			if tc.mutate != nil {
				tc.mutate(request)
			}
			response := httptest.NewRecorder()
			(&Server{identity: identity}).Handler().ServeHTTP(response, request)
			if response.Code != tc.want || len(identity.issued) != 0 || len(identity.revocations) != 0 || len(identity.lists) != 0 {
				t.Fatalf("guard status=%d, want=%d; issued=%d revoked=%d listed=%d", response.Code, tc.want, len(identity.issued), len(identity.revocations), len(identity.lists))
			}
		})
	}
}

type agentBearerTransport struct{ credential, origin string }

func (transport agentBearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+transport.credential)
	if transport.origin != "" {
		r.Header.Set("Origin", transport.origin)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func connectAgentBearer(t *testing.T, endpoint, origin string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "cloud-agent-auth-fixture", Version: "1.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: endpoint + "/api/v2/mcp", HTTPClient: &http.Client{Transport: agentBearerTransport{agentCredentialFixtureSecret, origin}}, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatalf("bearer MCP connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestAgentBearerMCPRevalidatesEveryToolAndPreservesRestrictedReference(t *testing.T) {
	for _, withOrigin := range []bool{false, true} {
		t.Run(map[bool]string{false: "external host without Origin", true: "same-origin explicit Origin"}[withOrigin], func(t *testing.T) {
			identity := newAgentCredentialIdentity()
			probe := &agentCatalogProbe{quote: agentQuoteFixture()}
			server := httptest.NewServer((&Server{identity: identity, catalog: probe}).Handler())
			t.Cleanup(server.Close)
			origin := ""
			if withOrigin {
				origin = server.URL
			}
			client := connectAgentBearer(t, server.URL, origin)
			tools, err := client.ListTools(t.Context(), nil)
			if err != nil || len(tools.Tools) != 6 {
				t.Fatalf("tool discovery: %v", err)
			}
			for range 2 {
				identity.mu.Lock()
				before := identity.resolves
				identity.mu.Unlock()
				read := readMCPQuote(t, client, map[string]any{"id": "quote-1"})
				raw, _ := json.Marshal(read)
				for _, secret := range []string{agentCredentialFixtureSecret, "session-1", "csrf-1", agentCredentialFixtureReference, "authorizationContextId"} {
					if strings.Contains(string(raw), secret) {
						t.Fatal("MCP result leaked credentials or internal identity")
					}
				}
				identity.mu.Lock()
				resolvedAgain, browserReads := identity.resolves-before, identity.sessionReads
				identity.mu.Unlock()
				if resolvedAgain < 2 || browserReads != 0 {
					t.Fatalf("MCP and owner dispatch must each re-resolve the agent credential: resolves=%d browserReads=%d", resolvedAgain, browserReads)
				}
			}
			if len(probe.calls) != 2 {
				t.Fatalf("owner reads=%d, want 2", len(probe.calls))
			}
			for _, call := range probe.calls {
				if call.Context.GetSessionId() != agentCredentialFixtureReference || call.Context.GetActorId() != "actor-1" || call.Context.GetScope().GetTenant().GetTenantId() != "tenant-1" {
					t.Fatal("owner did not receive the restricted reference and resolved identity")
				}
			}
			identity.mu.Lock()
			identity.revoked = true
			identity.mu.Unlock()
			result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "cloud_get_quote", Arguments: map[string]any{"id": "quote-1"}})
			if (err == nil && !result.IsError) || len(probe.calls) != 2 {
				t.Fatal("revoked credential reused initialization or reached owner")
			}
		})
	}
}

func TestAgentBearerMCPRechecksBeforeOwnerDispatch(t *testing.T) {
	identity := newAgentCredentialIdentity()
	probe := &agentCatalogProbe{quote: agentQuoteFixture()}
	server := httptest.NewServer((&Server{identity: identity, catalog: probe}).Handler())
	t.Cleanup(server.Close)
	client := connectAgentBearer(t, server.URL, "")
	identity.mu.Lock()
	identity.denyResolveAt = identity.resolves + 2
	identity.mu.Unlock()
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "cloud_get_quote", Arguments: map[string]any{"id": "quote-1"}})
	if err != nil || !result.IsError || len(probe.calls) != 0 {
		t.Fatal("credential revoked after MCP entry was not rechecked before owner dispatch")
	}
}

func TestAgentBearerTransportIsolation(t *testing.T) {
	for _, tc := range []struct {
		name, path string
		mutate     func(*http.Request)
		want       int
	}{
		{"foreign Origin", "/api/v2/mcp", func(r *http.Request) { r.Header.Set("Origin", "https://foreign.test") }, 403},
		{"cross-site fetch", "/api/v2/mcp", func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") }, 403},
		{"mixed browser identity", "/api/v2/mcp", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-1"}) }, 401},
		{"duplicate Authorization", "/api/v2/mcp", func(r *http.Request) { r.Header.Add("Authorization", "Bearer "+agentCredentialFixtureSecret) }, 401},
		{"wrong scheme", "/api/v2/mcp", func(r *http.Request) { r.Header.Set("Authorization", "Basic "+agentCredentialFixtureSecret) }, 401},
		{"empty bearer", "/api/v2/mcp", func(r *http.Request) { r.Header.Set("Authorization", "Bearer ") }, 401},
		{"REST cannot use bearer", "/api/v2/quotes/quote-1", nil, 401},
		{"metadata cannot use bearer", "/api/v2/auth/agent-credentials", nil, 401},
		{"human page cannot use bearer", "/api/v2/agent/connections", nil, 401},
		{"login cannot use bearer", "/api/v2/auth/context", nil, 401},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := newAgentCredentialIdentity()
			probe := &agentCatalogProbe{quote: agentQuoteFixture()}
			request := httptest.NewRequest(http.MethodPost, "http://cloud.test"+tc.path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			request.Header.Set("Authorization", "Bearer "+agentCredentialFixtureSecret)
			request.Header.Set("Content-Type", "application/json")
			if tc.mutate != nil {
				tc.mutate(request)
			}
			response := httptest.NewRecorder()
			(&Server{identity: identity, catalog: probe}).Handler().ServeHTTP(response, request)
			if response.Code != tc.want || identity.resolves != 0 || identity.sessionReads != 0 || len(probe.calls) != 0 {
				t.Fatalf("transport guard status=%d want=%d resolves=%d browserReads=%d ownerReads=%d", response.Code, tc.want, identity.resolves, identity.sessionReads, len(probe.calls))
			}
		})
	}
}

func TestAgentBearerRejectsBrowserCookieAndNonBearerReferences(t *testing.T) {
	for name, value := range map[string]string{
		"browser cookie":     "session-1",
		"internal reference": agentCredentialFixtureReference,
		"unknown credential": "unknown-agent-credential",
	} {
		t.Run(name, func(t *testing.T) {
			identity := newAgentCredentialIdentity()
			probe := &agentCatalogProbe{quote: agentQuoteFixture()}
			request := httptest.NewRequest(http.MethodPost, "http://cloud.test/api/v2/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			request.Header.Set("Authorization", "Bearer "+value)
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			(&Server{identity: identity, catalog: probe}).Handler().ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || identity.resolves != 1 || identity.sessionReads != 0 || len(probe.calls) != 0 {
				t.Fatalf("non-credential became a browser identity: status=%d resolves=%d browserReads=%d ownerReads=%d", response.Code, identity.resolves, identity.sessionReads, len(probe.calls))
			}
		})
	}
}
