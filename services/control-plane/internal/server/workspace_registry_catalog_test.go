package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// registryRouteTestHost is the installation registry host these route tests
// configure. It stands in for the instance-owned OPL_WORKSPACE_REGISTRY_HOST.
const registryRouteTestHost = "registry.test.example"

// workspaceRegistryRouteFixture overrides the route's registry client with a
// stub, because the route itself is what these tests own: session checks,
// namespace boundaries and error mapping — not the OCI client.
type workspaceRegistryStub struct {
	tags          []contracts.WorkspaceRegistryTag
	tagsErr       error
	resolution    contracts.WorkspaceRegistryImageResolution
	resolutionErr error
}

func (stub *workspaceRegistryStub) ListTags(_ context.Context, _, _ string) ([]contracts.WorkspaceRegistryTag, error) {
	return stub.tags, stub.tagsErr
}

func (stub *workspaceRegistryStub) ResolveTag(_ context.Context, _, _, _ string) (contracts.WorkspaceRegistryImageResolution, error) {
	return stub.resolution, stub.resolutionErr
}

// registryRouteTestRepositories stands in for the repositories an installation
// declares in OPL_WORKSPACE_REGISTRY_REPOSITORIES.
var registryRouteTestRepositories = []contracts.WorkspaceRegistryRepository{
	{Namespace: "oplcloud", Repository: "chaokang_agent_ibd"},
	{Namespace: "oplcloud", Repository: "one-person-lab-app"},
}

func newRegistryRouteTestServer(t *testing.T, stub *workspaceRegistryStub) (http.Handler, *httptest.ResponseRecorder) {
	t.Helper()
	// The host and the declared set are the installation registry facts the
	// route reports back to clients, standing in for the instance-owned
	// OPL_WORKSPACE_REGISTRY_HOST and OPL_WORKSPACE_REGISTRY_REPOSITORIES.
	return newRegistryRouteTestServerWithCatalog(t, &workspaceApplicationRegistryCatalog{
		client: stub, host: registryRouteTestHost, declared: registryRouteTestRepositories,
	})
}

func newRegistryRouteTestServerWithCatalog(t *testing.T, catalog *workspaceApplicationRegistryCatalog) (http.Handler, *httptest.ResponseRecorder) {
	t.Helper()
	server, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &fakeFabricClient{}), newMemoryTableStore())
	if err != nil {
		t.Fatal(err)
	}
	handler := server.(*controlPlaneHTTPHandler)
	// The route table is built once; re-registering on a fresh mux keeps the
	// test on the real production handler code path.
	mux := http.NewServeMux()
	registerWorkspaceRegistryCatalogRoutesWithCatalog(mux, handler.app, catalog)
	return mux, operatorSessionForTest(t, server)
}

// The installation owns the registry endpoint. This product has no default:
// an absent host is an absent capability, while a host that is present must be
// valid and completely credentialed or startup fails.
func TestRegistryCatalogInstallationConfiguration(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_REGISTRY_HOST", "")
	t.Setenv("OPL_WORKSPACE_REGISTRY_USERNAME", "")
	t.Setenv("OPL_WORKSPACE_REGISTRY_PASSWORD", "")
	t.Setenv("OPL_WORKSPACE_REGISTRY_REPOSITORIES", "")
	catalog, err := workspaceRegistryCatalogFromEnv()
	if err != nil || catalog != nil {
		t.Fatalf("absent host: catalog=%v err=%v, want an absent capability", catalog, err)
	}

	t.Setenv("OPL_WORKSPACE_REGISTRY_HOST", "not a host")
	if _, err := workspaceRegistryCatalogFromEnv(); err == nil {
		t.Fatal("invalid host was accepted")
	}

	// A configured registry without an approved set is a half-configured
	// capability: it would report an empty catalog that reads like an
	// installation which approved nothing. A foreign namespace in the
	// declaration is refused for the same reason a lookup there is.
	for _, declared := range []string{"", "   ", "library/nginx", "no-namespace", "oplcloud/Bad@name", ","} {
		t.Setenv("OPL_WORKSPACE_REGISTRY_HOST", registryRouteTestHost)
		t.Setenv("OPL_WORKSPACE_REGISTRY_REPOSITORIES", declared)
		if _, err := workspaceRegistryCatalogFromEnv(); err == nil {
			t.Fatalf("declared repositories %q were accepted", declared)
		}
	}

	t.Setenv("OPL_WORKSPACE_REGISTRY_REPOSITORIES", "oplcloud/one-person-lab-app")
	t.Setenv("OPL_WORKSPACE_REGISTRY_USERNAME", "user")
	if _, err := workspaceRegistryCatalogFromEnv(); err == nil {
		t.Fatal("half-configured credential pair was accepted")
	}

	t.Setenv("OPL_WORKSPACE_REGISTRY_PASSWORD", "password")
	catalog, err = workspaceRegistryCatalogFromEnv()
	if err != nil || catalog == nil || catalog.host != registryRouteTestHost || len(catalog.declared) != 1 {
		t.Fatalf("configured host: catalog=%v err=%v", catalog, err)
	}
	if catalog.declared[0].Namespace != "oplcloud" || catalog.declared[0].Repository != "one-person-lab-app" {
		t.Fatalf("declared repository = %+v", catalog.declared[0])
	}
}
