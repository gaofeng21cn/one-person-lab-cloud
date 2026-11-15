package fabric

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

const (
	workspaceImageTrustServiceToken = "workspace-image-trust-source-service-token-0001"
	workspaceImageTrustRepository   = "127.0.0.1:5000/qualification/local-qualification"
	workspaceImageTrustOtherRepo    = "127.0.0.1:5000/qualification/other-qualification"
	// workspaceImageTrustDocumentedPath is written out literally on purpose:
	// it is the path the Control Plane route is documented at, and the shared
	// contract constant is checked against it in the contracts package.
	workspaceImageTrustDocumentedPath = "/internal/workspace-image-trust-source"
)

func workspaceImageTrustReferenceForTest(digit string) string {
	return workspaceImageTrustRepository + "@sha256:" + strings.Repeat(digit, 64)
}

// workspaceImageTrustControlPlaneStub serves the same envelope shape the
// Control Plane writes, so the client is exercised against the real wire
// contract instead of a private convenience format.
func workspaceImageTrustControlPlaneStub(t *testing.T, repositories []string, revision int) (*httptest.Server, *[]string) {
	t.Helper()
	seen := &[]string{}
	mux := http.NewServeMux()
	// The stub answers only the documented path, so a client that calls a
	// different one cannot pass by accident.
	mux.HandleFunc("GET "+workspaceImageTrustDocumentedPath, func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Header.Get("x-opl-service-token"))
		source := contracts.WorkspaceImageTrustSource{SchemaVersion: 1, Revision: revision, Repositories: repositories}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"source": "control-plane", "status": "available", "available": true,
			"fetchedAt": time.Now().UTC().Format(time.RFC3339Nano), "data": source,
		})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, seen
}

func workspaceImageTrustClientForTest(url, token string) *workspaceImageTrustSourceClient {
	return newWorkspaceImageTrustSourceClient(func(name string) string {
		switch name {
		case "OPL_CONTROL_PLANE_URL":
			return url
		case "OPL_INTERNAL_SERVICE_TOKEN":
			return token
		}
		return ""
	})
}

// The client reads the Control Plane's registered trust source over the shared
// internal credential. Without that credential it must not admit anything.
func TestWorkspaceImageTrustSourceClientReadsTheRegisteredSource(t *testing.T) {
	server, seen := workspaceImageTrustControlPlaneStub(t, []string{workspaceImageTrustRepository}, 2)
	client := workspaceImageTrustClientForTest(server.URL, workspaceImageTrustServiceToken)
	if err := client.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(*seen) != 1 || (*seen)[0] != workspaceImageTrustServiceToken {
		t.Fatalf("the client did not present the internal credential: %#v", *seen)
	}
	source := client.Source()
	if source.Revision != 2 || !source.TrustsRepository(workspaceImageTrustRepository) {
		t.Fatalf("source=%#v", source)
	}
	if source.TrustsRepository(workspaceImageTrustOtherRepo) {
		t.Fatal("the readback trusted a repository it never named")
	}
}

func TestWorkspaceImageTrustSourceClientWithoutACredentialTrustsNothing(t *testing.T) {
	server, _ := workspaceImageTrustControlPlaneStub(t, []string{workspaceImageTrustRepository}, 2)
	client := workspaceImageTrustClientForTest(server.URL, "")
	if err := client.Refresh(context.Background()); err == nil {
		t.Fatal("a client without the internal credential reported a successful read")
	}
	if client.Source().TrustsImage(workspaceImageTrustReferenceForTest("a")) {
		t.Fatal("a client without the internal credential admitted an image")
	}
}

// An unreachable Control Plane must neither widen the admission decision nor
// silently empty it. The last known source is kept.
func TestWorkspaceImageTrustSourceClientKeepsTheLastKnownSourceWhenTheControlPlaneIsUnreachable(t *testing.T) {
	server, _ := workspaceImageTrustControlPlaneStub(t, []string{workspaceImageTrustRepository}, 2)
	client := workspaceImageTrustClientForTest(server.URL, workspaceImageTrustServiceToken)
	if err := client.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	server.Close()
	client.mu.Lock()
	client.refreshed = time.Now().Add(-2 * workspaceImageTrustSourceTTL)
	client.mu.Unlock()
	if err := client.Refresh(context.Background()); err == nil {
		t.Fatal("a closed Control Plane reported a successful read")
	}
	if !client.Source().TrustsRepository(workspaceImageTrustRepository) {
		t.Fatal("an unreachable Control Plane emptied the admission decision")
	}
	if client.Source().TrustsRepository(workspaceImageTrustOtherRepo) {
		t.Fatal("an unreachable Control Plane widened the admission decision")
	}
}

func workspaceImageTrustLocalDockerProvider(t *testing.T, client *workspaceImageTrustSourceClient, trustedSources []string) *LocalDockerProvider {
	t.Helper()
	storageRoot := localDockerStorageTestRoot(t)
	secretRoot := t.TempDir()
	if err := os.Chmod(secretRoot, 0700); err != nil {
		t.Fatal(err)
	}
	profile, err := json.Marshal(localDockerProviderProfile{SchemaVersion: 1, Packages: []localDockerPackageProfile{{
		ID: "qualification", Name: "Qualification", Available: true,
		Compute: ComputePlan{ID: "qualification", Server: "qualification", InstanceType: "local-qualification", CPU: 2, MemoryGB: 4, DiskGB: 10},
		Storage: localDockerStoragePlan{SizeGB: 10, QuotaPolicy: "linux-project"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return newLocalDockerProvider(LocalDockerProviderConfig{
		GatewaySecretRoot: secretRoot, HostStorageRoot: storageRoot, PublishHost: "127.0.0.1",
		ProviderProfileJSON: profile, StorageQuotaBackend: localDockerStorageTestQuota(storageRoot),
		TrustedWorkspaceImageSources: trustedSources, TrustSourceClient: client,
	}, &recordingDockerRunner{})
}

// The publisher's own build output starts a workspace because its repository is
// registered. An image published to any other repository does not.
func TestLocalDockerProviderAdmitsOnlyImagesInATrustedRepository(t *testing.T) {
	server, _ := workspaceImageTrustControlPlaneStub(t, []string{workspaceImageTrustRepository}, 2)
	client := workspaceImageTrustClientForTest(server.URL, workspaceImageTrustServiceToken)
	provider := workspaceImageTrustLocalDockerProvider(t, client, []string{})

	if !provider.ValidateWorkspaceImageReference(workspaceImageTrustReferenceForTest("a")) {
		t.Fatal("an image published to the registered repository was rejected")
	}
	untrusted := workspaceImageTrustOtherRepo + "@sha256:" + strings.Repeat("b", 64)
	if provider.ValidateWorkspaceImageReference(untrusted) {
		t.Fatal("an image published to an unregistered repository was admitted")
	}
	if provider.ValidateWorkspaceImageReference(workspaceImageTrustRepository + ":latest") {
		t.Fatal("a mutable reference was admitted")
	}
}

// The installation's own repository trust keeps working. The registered trust
// source is added to it, not substituted for it.
func TestLocalDockerProviderKeepsItsInstallationRepositoryTrust(t *testing.T) {
	server, _ := workspaceImageTrustControlPlaneStub(t, []string{workspaceImageTrustRepository}, 2)
	client := workspaceImageTrustClientForTest(server.URL, workspaceImageTrustServiceToken)
	provider := workspaceImageTrustLocalDockerProvider(t, client, []string{workspaceImageTrustOtherRepo})

	if !provider.ValidateWorkspaceImageReference(workspaceImageTrustOtherRepo+"@sha256:"+strings.Repeat("c", 64)) {
		t.Fatal("the installation's own repository trust was lost")
	}
	if !provider.ValidateWorkspaceImageReference(workspaceImageTrustReferenceForTest("d")) {
		t.Fatal("the registered repository was not admitted")
	}
	if provider.ValidateWorkspaceImageReference("registry.example/opl/workspace@sha256:" + strings.Repeat("e", 64)) {
		t.Fatal("an image outside every trusted repository was admitted")
	}
}

// The same rule on the TKE provider: the published catalog stays an exact
// image match, and the registered trust source admits a repository.
func TestTencentProviderAdmitsOnlyImagesInATrustedRepository(t *testing.T) {
	server, _ := workspaceImageTrustControlPlaneStub(t, []string{workspaceImageTrustRepository}, 2)
	client := workspaceImageTrustClientForTest(server.URL, workspaceImageTrustServiceToken)

	installed := workspaceImageTrustReferenceForTest("a")
	catalog, err := contracts.DecodeWorkspaceImageReleaseCatalog(
		`{"schemaVersion":1,"releases":[{"version":"26.11.15","image":"`+installed+`"}]}`, installed)
	if err != nil {
		t.Fatal(err)
	}
	provider := &TencentProvider{workspaceImages: catalog, workspaceImageTrustSource: client}

	if !provider.ValidateWorkspaceImageReference(installed) {
		t.Fatal("the published catalog image was rejected")
	}
	// The same catalog without a trust source still answers exactly: the
	// repository-level answer comes from the registered trust source alone.
	catalogOnly := &TencentProvider{workspaceImages: catalog}
	other := workspaceImageTrustRepository + "@sha256:" + strings.Repeat("b", 64)
	if catalogOnly.ValidateWorkspaceImageReference(other) {
		t.Fatal("the published catalog admitted an image it does not name")
	}
	if !provider.ValidateWorkspaceImageReference(other) {
		t.Fatal("the registered repository did not admit its own image")
	}
	untrusted := workspaceImageTrustOtherRepo + "@sha256:" + strings.Repeat("c", 64)
	if provider.ValidateWorkspaceImageReference(untrusted) {
		t.Fatal("an image published to an unregistered repository was admitted")
	}
}

// A provider built without a trust source keeps the installation's own rules
// and never reaches for a Control Plane that was not wired.
func TestProvidersWithoutATrustSourceAdmitNothingExtra(t *testing.T) {
	storageRoot := localDockerStorageTestRoot(t)
	provider := newLocalDockerProvider(localDockerStorageTestConfig(storageRoot), &recordingDockerRunner{})
	if provider.ValidateWorkspaceImageReference(workspaceImageTrustReferenceForTest("a")) {
		t.Fatal("a provider without a trust source admitted an image")
	}
	if err := provider.RefreshWorkspaceImageTrust(context.Background()); err != nil {
		t.Fatalf("a provider without a trust source failed to refresh: %v", err)
	}
}
