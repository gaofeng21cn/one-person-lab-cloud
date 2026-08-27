package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func TestWorkspaceImageReleaseActivationPersistsDefaultForNewLaunchAndExistingRuntime(t *testing.T) {
	const currentImage = "registry.example/workspace@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const rollbackImage = "registry.example/workspace@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	catalogJSON, err := json.Marshal(contracts.WorkspaceImageReleaseCatalog{SchemaVersion: 1, Releases: []contracts.WorkspaceImageRelease{
		{Version: "26.8.26", Image: currentImage},
		{Version: "26.8.4", Image: rollbackImage},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPL_WORKSPACE_IMAGE", currentImage)
	t.Setenv(contracts.WorkspaceImageReleasesEnv, string(catalogJSON))
	t.Setenv("OPL_WORKSPACE_RUNTIME_IMAGE_REPLACEMENT_WORKER_ENABLED", "0")

	store := newMemoryTableStore()
	fabric := &runtimeImageReplacementRouteFabric{fakeFabricClient: fakeFabricClient{runtimeStatus: clientsWorkspaceRuntimeForReleaseTest(currentImage)}}
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, fabric), store)
	if err != nil {
		t.Fatal(err)
	}
	seedCanonicalRuntimeAccessWorkspaceForTest(t, store, "usr-alpha")
	operator := reservedOperatorSessionForTest(t, server)
	body := workspaceImageReleaseActivationBodyForTest(t, "26.8.4", 1, "rollback incompatible Workspace WebUI")
	activated := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-image-release-activations", body, "workspace-release-rollback")
	if activated.Code != http.StatusOK {
		t.Fatalf("activation status=%d body=%s", activated.Code, activated.Body.String())
	}
	var response workspaceImageReleasePolicyDTO
	if err := json.NewDecoder(activated.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Revision != 2 || response.Active.Version != "26.8.4" || response.Active.Image != rollbackImage || len(response.Releases) != 2 {
		t.Fatalf("activation response=%#v", response)
	}

	handler := server.(*controlPlaneHTTPHandler)
	policy, _, _, err := handler.app.currentWorkspaceImageReleasePolicy(context.Background())
	if err != nil || policy.ActiveImage != rollbackImage {
		t.Fatalf("persisted policy=%#v err=%v", policy, err)
	}
	descriptor, err := newWorkspaceLaunchDescriptorWithImage("acct-alpha", "usr-alpha", "Workspace", "basic", 10, true, "price-v1", "launch-key", policy.ActiveImage)
	if err != nil || descriptor.WorkspaceImageDigest != rollbackImage {
		t.Fatalf("new launch descriptor=%#v err=%v", descriptor, err)
	}

	preview := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspaces/ws-alpha/runtime-image-replacements/preview", "")
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"targetImageDigest":"`+rollbackImage+`"`) || !strings.Contains(preview.Body.String(), `"canReplace":true`) {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}

	replayed := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-image-release-activations", body, "workspace-release-rollback")
	if replayed.Code != http.StatusOK || !strings.Contains(replayed.Body.String(), `"revision":2`) {
		t.Fatalf("replay status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	conflict := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-image-release-activations", workspaceImageReleaseActivationBodyForTest(t, "26.8.26", 1, "changed intent"), "workspace-release-rollback")
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), "idempotency_conflict") {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	decimalRevision := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-image-release-activations", `{"releaseVersion":"26.8.26","expectedRevision":1.5,"reason":"invalid revision"}`, "workspace-release-invalid-revision")
	if decimalRevision.Code != http.StatusBadRequest || !strings.Contains(decimalRevision.Body.String(), errWorkspaceImageReleaseRequestInvalid.Error()) {
		t.Fatalf("decimal revision status=%d body=%s", decimalRevision.Code, decimalRevision.Body.String())
	}
}

func TestPostgresWorkspaceImageReleaseActivationPersistsPolicyAndAuditReplay(t *testing.T) {
	const currentImage = "registry.example/workspace@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const rollbackImage = "registry.example/workspace@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	catalogJSON, err := json.Marshal(contracts.WorkspaceImageReleaseCatalog{SchemaVersion: 1, Releases: []contracts.WorkspaceImageRelease{
		{Version: "26.8.26", Image: currentImage},
		{Version: "26.8.4", Image: rollbackImage},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPL_WORKSPACE_IMAGE", currentImage)
	t.Setenv(contracts.WorkspaceImageReleasesEnv, string(catalogJSON))
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	body := workspaceImageReleaseActivationBodyForTest(t, "26.8.4", 1, "rollback PostgreSQL Workspace release")
	first := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-image-release-activations", body, "workspace-release-postgres")
	if first.Code != http.StatusOK {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	handler := server.(*controlPlaneHTTPHandler)
	policy, _, _, err := handler.app.currentWorkspaceImageReleasePolicy(context.Background())
	if err != nil || policy.Revision != 2 || policy.ActiveVersion != "26.8.4" || policy.ActiveImage != rollbackImage {
		t.Fatalf("persisted policy=%#v err=%v", policy, err)
	}
	replayed := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-image-release-activations", body, "workspace-release-postgres")
	if replayed.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", replayed.Code, replayed.Body.String())
	}
	policy, _, _, err = handler.app.currentWorkspaceImageReleasePolicy(context.Background())
	if err != nil || policy.Revision != 2 || policy.ActiveImage != rollbackImage {
		t.Fatalf("replayed policy=%#v err=%v", policy, err)
	}
}

func TestWorkspaceImageReleaseActivationPinsNewLaunchPreflightToActiveImage(t *testing.T) {
	const currentImage = "registry.example/workspace@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const rollbackImage = "registry.example/workspace@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	catalogJSON, err := json.Marshal(contracts.WorkspaceImageReleaseCatalog{SchemaVersion: 1, Releases: []contracts.WorkspaceImageRelease{
		{Version: "26.8.26", Image: currentImage},
		{Version: "26.8.4", Image: rollbackImage},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPL_DEPLOYMENT_MODE", "customer_owned")
	t.Setenv("OPL_FABRIC_PROVIDER", "local-docker")
	t.Setenv("OPL_WORKSPACE_IMAGE", currentImage)
	t.Setenv(contracts.WorkspaceImageReleasesEnv, string(catalogJSON))

	fabric := &providerProfileCatalogFabricClient{packages: []clients.FabricWorkspacePackage{{ID: "basic", Name: "Basic Workspace", SizeGB: 10, Available: true}}}
	sub2API := &providerProfileSub2APIClient{testSub2APIClient: &testSub2APIClient{balance: 1_000_000_000_000, charges: map[string]int64{}}}
	server := NewServer(controlplane.NewService(fakeLedgerClient{}, fabric, sub2API))
	operator := reservedOperatorSessionForTest(t, server)
	activation := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-image-release-activations", workspaceImageReleaseActivationBodyForTest(t, "26.8.4", 1, "rollback new Workspace default"), "workspace-release-launch-default")
	if activation.Code != http.StatusOK {
		t.Fatalf("activation status=%d body=%s", activation.Code, activation.Body.String())
	}

	customer := loginForTest(t, server, "customer-owner@opl.local", "CorrectHorseBatteryStaple!")
	launch := requestWithMutationKeyForTest(t, server, customer, http.MethodPost, "/api/workspace-launches", `{"name":"Rollback Workspace","packageId":"basic","autoRenew":false}`, "workspace-release-launch")
	if launch.Code != http.StatusAccepted {
		t.Fatalf("launch status=%d body=%s", launch.Code, launch.Body.String())
	}
	if len(fabric.preflightInputs) != 1 || fabric.preflightInputs[0].WorkspaceImageDigest != rollbackImage {
		t.Fatalf("preflight inputs=%#v", fabric.preflightInputs)
	}
}

func workspaceImageReleaseActivationBodyForTest(t *testing.T, releaseVersion string, expectedRevision int, reason string) string {
	t.Helper()
	body, err := json.Marshal(contracts.WorkspaceImageReleaseActivationRequest{
		ReleaseVersion: releaseVersion, ExpectedRevision: expectedRevision, Reason: reason,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func clientsWorkspaceRuntimeForReleaseTest(image string) clients.WorkspaceRuntime {
	return clients.WorkspaceRuntime{
		ID: "rt-unit", OperationID: "workspace-launch-unit:runtime", WorkspaceID: "ws-alpha",
		URL: "https://workspace.medopl.com/w/ws-alpha/", Status: "running", ServiceName: "runtime-unit", ImageID: image, Ready: true,
	}
}
