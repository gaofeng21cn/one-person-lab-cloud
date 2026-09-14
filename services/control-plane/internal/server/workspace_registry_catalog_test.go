package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

// workspaceRegistryRouteFixture overrides the route's registry client with a
// stub, because the route itself is what these tests own: session checks,
// namespace boundaries and error mapping — not the OCI client.
type workspaceRegistryStub struct {
	repositories []contracts.WorkspaceRegistryRepository
	repositoriesErr error
	tags        []contracts.WorkspaceRegistryTag
	tagsErr     error
	resolution  contracts.WorkspaceRegistryImageResolution
	resolutionErr error
}

func (stub *workspaceRegistryStub) ListRepositories(_ context.Context, _ string) ([]contracts.WorkspaceRegistryRepository, error) {
	return stub.repositories, stub.repositoriesErr
}

func (stub *workspaceRegistryStub) ListTags(_ context.Context, _, _ string) ([]contracts.WorkspaceRegistryTag, error) {
	return stub.tags, stub.tagsErr
}

func (stub *workspaceRegistryStub) ResolveTag(_ context.Context, _, _, _ string) (contracts.WorkspaceRegistryImageResolution, error) {
	return stub.resolution, stub.resolutionErr
}

func newRegistryRouteTestServer(t *testing.T, stub *workspaceRegistryStub) (http.Handler, *httptest.ResponseRecorder) {
	t.Helper()
	server, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), newMemoryTableStore())
	if err != nil {
		t.Fatal(err)
	}
	// Replace the env-built catalog with the stub after construction.
	catalog := &workspaceApplicationRegistryCatalog{client: stub}
	registered := false
	for _, route := range []string{} {
		_ = route
	}
	_ = registered
	handler := server.(*controlPlaneHTTPHandler)
	// The route table is built once; re-registering on a fresh mux keeps the
	// test on the real production handler code path.
	mux := http.NewServeMux()
	registerWorkspaceRegistryCatalogRoutesWithCatalog(mux, handler.app, catalog)
	return mux, operatorSessionForTest(t, server)
}

func TestRegistryCatalogRequiresAdminSession(t *testing.T) {
	stub := &workspaceRegistryStub{}
	server, _ := newRegistryRouteTestServer(t, stub)
	req := httptest.NewRequest(http.MethodGet, "/api/operator/registry/repositories", nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated repositories status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegistryCatalogRepositories(t *testing.T) {
	stub := &workspaceRegistryStub{repositories: []contracts.WorkspaceRegistryRepository{
		{Namespace: "oplcloud", Repository: "one-person-lab-app"},
		{Namespace: "oplcloud", Repository: "chaokang_agent_ibd"},
	}}
	server, operator := newRegistryRouteTestServer(t, stub)
	rec := requestWithMutationKeyForTest(t, server, operator, http.MethodGet, "/api/operator/registry/repositories?namespace=oplcloud", "", "r")
	if rec.Code != http.StatusOK {
		t.Fatalf("repositories status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response workspaceRegistryCatalogResponse
	if json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Host == "" || len(response.Items) != 2 {
		t.Fatalf("repositories body=%s", rec.Body.String())
	}
	if response.Items[0].Repository != "one-person-lab-app" || response.Items[1].Repository != "chaokang_agent_ibd" {
		t.Fatalf("items are not namespace-scoped: %+v", response.Items)
	}
}

func TestRegistryCatalogRejectsForeignNamespace(t *testing.T) {
	stub := &workspaceRegistryStub{}
	server, operator := newRegistryRouteTestServer(t, stub)
	rec := requestWithMutationKeyForTest(t, server, operator, http.MethodGet, "/api/operator/registry/repositories?namespace=library", "", "r")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("foreign namespace status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegistryCatalogTags(t *testing.T) {
	stub := &workspaceRegistryStub{tags: []contracts.WorkspaceRegistryTag{{Tag: "v1"}}}
	server, operator := newRegistryRouteTestServer(t, stub)
	rec := requestWithMutationKeyForTest(t, server, operator, http.MethodGet, "/api/operator/registry/tags/oplcloud/one-person-lab-app", "", "r")
	if rec.Code != http.StatusOK {
		t.Fatalf("tags status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Namespace  string   `json:"namespace"`
		Repository string   `json:"repository"`
		Tags       []contracts.WorkspaceRegistryTag `json:"tags"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Repository != "one-person-lab-app" || len(response.Tags) != 1 {
		t.Fatalf("tags body=%s", rec.Body.String())
	}
	bad := requestWithMutationKeyForTest(t, server, operator, http.MethodGet, "/api/operator/registry/tags/library/app", "", "r")
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("foreign namespace tags status=%d", bad.Code)
	}
}

func TestRegistryCatalogResolve(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	stub := &workspaceRegistryStub{resolution: contracts.WorkspaceRegistryImageResolution{
		Reference: "uswccr.ccs.tencentyun.com/oplcloud/one-person-lab-app@" + digest, Digest: digest,
	}}
	server, operator := newRegistryRouteTestServer(t, stub)
	rec := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/registry/resolve",
		`{"namespace":"oplcloud","repository":"one-person-lab-app","tag":"v1.0.0"}`, "resolve-1")
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response contracts.WorkspaceRegistryImageResolution
	if json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Digest != digest || response.Reference != "uswccr.ccs.tencentyun.com/oplcloud/one-person-lab-app@"+digest {
		t.Fatalf("resolve body=%s", rec.Body.String())
	}
}

func TestRegistryCatalogResolveInputValidation(t *testing.T) {
	stub := &workspaceRegistryStub{}
	server, operator := newRegistryRouteTestServer(t, stub)
	cases := []struct{ name, body string }{
		{"missing fields", `{}`},
		{"foreign namespace", `{"namespace":"library","repository":"app","tag":"v1"}`},
		{"bad repository", `{"namespace":"oplcloud","repository":"../escape","tag":"v1"}`},
		{"empty tag", `{"namespace":"oplcloud","repository":"app","tag":" "}`},
	}
	for _, testCase := range cases {
		rec := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/registry/resolve", testCase.body, "resolve-"+testCase.name)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s: status=%d body=%s", testCase.name, rec.Code, rec.Body.String())
		}
	}
}

func TestRegistryErrorMapping(t *testing.T) {
	stub := &workspaceRegistryStub{repositoriesErr: &clients.RegistryAPIError{Operation: "catalog", Status: http.StatusUnauthorized, Code: "UNAUTHORIZED"}}
	server, operator := newRegistryRouteTestServer(t, stub)
	rec := requestWithMutationKeyForTest(t, server, operator, http.MethodGet, "/api/operator/registry/repositories?namespace=oplcloud", "", "r")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "workspace_registry_access_denied") {
		t.Fatalf("401 mapping status=%d body=%s", rec.Code, rec.Body.String())
	}

	unreachable := &workspaceRegistryStub{repositoriesErr: &clients.RegistryAPIError{Operation: "catalog", Status: 0, Code: "transport_failure"}}
	server, operator = newRegistryRouteTestServer(t, unreachable)
	rec = requestWithMutationKeyForTest(t, server, operator, http.MethodGet, "/api/operator/registry/repositories?namespace=oplcloud", "", "r")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "workspace_registry_unreachable") {
		t.Fatalf("transport mapping status=%d body=%s", rec.Code, rec.Body.String())
	}

	unknown := &workspaceRegistryStub{tagsErr: &clients.RegistryAPIError{Operation: "tags", Status: http.StatusNotFound, Code: "NAME_UNKNOWN"}}
	server, operator = newRegistryRouteTestServer(t, unknown)
	rec = requestWithMutationKeyForTest(t, server, operator, http.MethodGet, "/api/operator/registry/tags/oplcloud/missing-app", "", "r")
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), "workspace_registry_target_unknown") {
		t.Fatalf("404 mapping status=%d body=%s", rec.Code, rec.Body.String())
	}

	gateway := &workspaceRegistryStub{resolutionErr: &clients.RegistryAPIError{Operation: "manifest", Status: http.StatusInternalServerError, Code: "http_500"}}
	server, operator = newRegistryRouteTestServer(t, gateway)
	rec = requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/registry/resolve",
		fmt.Sprintf(`{"namespace":"oplcloud","repository":"app","tag":"v1"}`), "resolve-gw")
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "workspace_registry_unavailable") {
		t.Fatalf("502 mapping status=%d body=%s", rec.Code, rec.Body.String())
	}
}
