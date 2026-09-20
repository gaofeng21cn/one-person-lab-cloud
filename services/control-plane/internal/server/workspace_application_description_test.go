package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

// newDeploymentCompletionFixture registers the deployment route on the real
// handler with a stub registry catalog, so the test exercises the route's own
// completion of an operator's description rather than a hand-written revision.
func newDeploymentCompletionFixture(t *testing.T, catalog *workspaceApplicationRegistryCatalog) (http.Handler, *httptest.ResponseRecorder, controlPlaneTableStore) {
	t.Helper()
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	mux := http.NewServeMux()
	registerApplicationDeploymentRoutes(mux, handler.app, handler.service, catalog)
	return mux, operatorSessionForTest(t, fixture.server), fixture.store
}

// approvedDeploymentCatalog is one installation that approved exactly the two
// repositories the product ships, standing in for
// OPL_WORKSPACE_REGISTRY_HOST + OPL_WORKSPACE_REGISTRY_REPOSITORIES.
func approvedDeploymentCatalog(stub *workspaceRegistryStub) *workspaceApplicationRegistryCatalog {
	if stub.facts.Ports == nil {
		stub.facts = clients.WorkspaceRegistryImageFacts{Ports: []int{8082}, Volumes: []string{"/data"}, User: "10001:10001"}
	}
	return &workspaceApplicationRegistryCatalog{client: stub, host: registryRouteTestHost, declared: registryRouteTestRepositories}
}

// unapprovedFactsCatalog is the approved catalog with facts the test states
// exactly, including an image that declares no port at all.
func unapprovedFactsCatalog(stub *workspaceRegistryStub, facts clients.WorkspaceRegistryImageFacts) *workspaceApplicationRegistryCatalog {
	stub.facts = facts
	return &workspaceApplicationRegistryCatalog{client: stub, host: registryRouteTestHost, declared: registryRouteTestRepositories}
}

func deploymentImage(digestCharacter string) string {
	return registryRouteTestHost + "/oplcloud/chaokang_agent_ibd@sha256:" + strings.Repeat(digestCharacter, 64)
}

// An operator selects an image and states how it is exposed; the platform
// completes the description, so no registration step and no operator-typed
// application identity exist anywhere on this path.
func TestApplicationDeploymentCompletesAnOperatorDescription(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	stub := &workspaceRegistryStub{}
	server, operator, store := newDeploymentCompletionFixture(t, approvedDeploymentCatalog(stub))
	body, err := json.Marshal(map[string]any{
		"workspaceId": "ws-alpha", "configuration": map[string]any{"environment": map[string]any{}},
		"revision": map[string]any{"schemaVersion": 1, "platform": "linux/amd64", "image": deploymentImage("a"), "exposurePolicy": "anonymous"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", string(body), "deploy-image-only")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("image-only deployment status=%d body=%s", rec.Code, rec.Body.String())
	}
	var response struct {
		Intent struct {
			ApplicationID  string `json:"applicationId"`
			TargetRevision string `json:"targetRevision"`
		} `json:"intent"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &response) != nil || response.Intent.ApplicationID == "" || response.Intent.TargetRevision == "" {
		t.Fatalf("intent body=%s", rec.Body.String())
	}
	if !strings.HasPrefix(response.Intent.ApplicationID, "chaokang-agent-ibd-") || !strings.HasPrefix(response.Intent.TargetRevision, "d") {
		t.Fatalf("derived identity = %q@%q", response.Intent.ApplicationID, response.Intent.TargetRevision)
	}
	admitted, found, err := store.AdmittedApplicationRevision(context.Background(), response.Intent.ApplicationID, response.Intent.TargetRevision)
	if err != nil || !found {
		t.Fatalf("revision not admitted: found=%v err=%v", found, err)
	}
	revision, ok := decodeApplicationRevisionPayload(stringValue(admitted["payload"]))
	if !ok {
		t.Fatalf("admitted payload unreadable: %s", stringValue(admitted["payload"]))
	}
	// The image's own declarations are the run requirements: its one TCP port,
	// its data path, and the process identity it states.
	if revision.Image != deploymentImage("a") || revision.EntryPort != "http" || len(revision.Ports) != 1 || revision.Ports[0].Port != 8082 {
		t.Fatalf("derived ports = %+v entry=%q", revision.Ports, revision.EntryPort)
	}
	if len(revision.PersistentMounts) != 1 || revision.PersistentMounts[0].MountPath != "/data" || revision.PersistentMounts[0].Name != "data" {
		t.Fatalf("derived mounts = %+v", revision.PersistentMounts)
	}
	if revision.Execution.UserID == nil || *revision.Execution.UserID != 10001 || revision.Execution.GroupID == nil || *revision.Execution.GroupID != 10001 {
		t.Fatalf("derived execution = %+v", revision.Execution)
	}
	// The same image and facts resolve to the same version, so replaying the
	// operator's action never creates a second immutable version.
	second, err := json.Marshal(map[string]any{
		"workspaceId": "ws-alpha", "configuration": map[string]any{"environment": map[string]any{}},
		"revision": map[string]any{"schemaVersion": 1, "platform": "linux/amd64", "image": deploymentImage("a"), "exposurePolicy": "anonymous"},
	})
	if err != nil {
		t.Fatal(err)
	}
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", string(second), "deploy-image-only")
	var replayed struct {
		Intent struct {
			TargetRevision string `json:"targetRevision"`
		} `json:"intent"`
	}
	if replay.Code != http.StatusConflict {
		// The same Workspace may hold one selected application; a second distinct
		// command under a different key is refused by the binding transition, not
		// by a second admitted version. Either way the version is unchanged.
		if json.Unmarshal(replay.Body.Bytes(), &replayed) != nil || replayed.Intent.TargetRevision != response.Intent.TargetRevision {
			t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
		}
	}
	stored, found, err := store.AdmittedApplicationRevision(context.Background(), response.Intent.ApplicationID, response.Intent.TargetRevision)
	if err != nil || !found || stringValue(stored["payload"]) != stringValue(admitted["payload"]) {
		t.Fatalf("derived revision changed on replay: found=%v err=%v", found, err)
	}
}

// An image the installation did not approve is refused by name, not routed to a
// registry the platform cannot read.
func TestApplicationDeploymentRefusesAnUnapprovedImage(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	server, operator, _ := newDeploymentCompletionFixture(t, approvedDeploymentCatalog(&workspaceRegistryStub{}))
	body, err := json.Marshal(map[string]any{
		"workspaceId": "ws-alpha", "configuration": map[string]any{"environment": map[string]any{}},
		"revision": map[string]any{"schemaVersion": 1, "platform": "linux/amd64", "image": registryRouteTestHost + "/oplcloud/unapproved@sha256:" + strings.Repeat("b", 64), "exposurePolicy": "anonymous"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", string(body), "deploy-unapproved")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "workspace_application_image_not_approved") {
		t.Fatalf("unapproved image status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// An installation without a registry declares that absence instead of inventing
// the image's run requirements.
func TestApplicationDeploymentRequiresAConfiguredRegistryToCompleteADescription(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	server, operator, _ := newDeploymentCompletionFixture(t, nil)
	body, err := json.Marshal(map[string]any{
		"workspaceId": "ws-alpha", "configuration": map[string]any{"environment": map[string]any{}},
		"revision": map[string]any{"schemaVersion": 1, "platform": "linux/amd64", "image": deploymentImage("c"), "exposurePolicy": "anonymous"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", string(body), "deploy-unconfigured")
	if rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "workspace_registry_unconfigured") {
		t.Fatalf("unconfigured registry status=%d body=%s", rec.Code, rec.Body.String())
	}
	// A description that states every run fact itself still deploys, because it
	// never asks the platform to read anything.
	complete, err := json.Marshal(map[string]any{
		"workspaceId": "ws-alpha", "configuration": map[string]any{"environment": map[string]any{}},
		"revision": map[string]any{
			"schemaVersion": 1, "platform": "linux/amd64", "image": deploymentImage("d"), "exposurePolicy": "anonymous",
			"ports": []any{map[string]any{"name": "webui", "port": 8082, "protocol": "TCP"}}, "entryPort": "webui",
			"persistentMounts": []any{map[string]any{"name": "data", "mountPath": "/data"}},
			"execution":        map[string]any{"userId": 10001, "groupId": 10001},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stated := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", string(complete), "deploy-stated")
	if stated.Code != http.StatusAccepted {
		t.Fatalf("stated description status=%d body=%s", stated.Code, stated.Body.String())
	}
}

// A published application without an entry the platform can determine is
// refused rather than deployed unreachable.
func TestApplicationDeploymentRequiresADeterminableEntryPort(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	stub := &workspaceRegistryStub{}
	server, operator, _ := newDeploymentCompletionFixture(t, unapprovedFactsCatalog(stub, clients.WorkspaceRegistryImageFacts{Volumes: []string{"/data"}}))
	body, err := json.Marshal(map[string]any{
		"workspaceId": "ws-alpha", "configuration": map[string]any{"environment": map[string]any{}},
		"revision": map[string]any{"schemaVersion": 1, "platform": "linux/amd64", "image": deploymentImage("e"), "exposurePolicy": "anonymous"},
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", string(body), "deploy-no-entry")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "workspace_application_entry_port_required") {
		t.Fatalf("no determinable entry status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// A description that states its own identity is its publisher's immutable
// statement: it is admitted exactly as written and never completed from the
// image, so a published revision cannot change under the publisher's version.
func TestApplicationDeploymentKeepsAPublisherStatementUnchanged(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	operator := operatorSessionForTest(t, fixture.server)
	revision := contracts.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "published-app", Version: "7.0.0", Platform: "linux/amd64",
		Image: "repo.example/published@sha256:" + strings.Repeat("f", 64), ExposurePolicy: "application",
	}
	body, err := json.Marshal(map[string]any{
		"workspaceId": "ws-alpha", "configuration": map[string]any{"environment": map[string]any{}}, "revision": revision,
	})
	if err != nil {
		t.Fatal(err)
	}
	rec := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, "/api/operator/application-deployments", string(body), "deploy-published")
	if rec.Code != http.StatusAccepted {
		t.Fatalf("publisher statement status=%d body=%s", rec.Code, rec.Body.String())
	}
	admitted, found, err := fixture.store.AdmittedApplicationRevision(context.Background(), "published-app", "7.0.0")
	if err != nil || !found {
		t.Fatalf("published revision not admitted: found=%v err=%v", found, err)
	}
	stored, ok := decodeApplicationRevisionPayload(stringValue(admitted["payload"]))
	if !ok || stored.Image != revision.Image || len(stored.Ports) != 0 || len(stored.PersistentMounts) != 0 || stored.Execution.UserID != nil {
		t.Fatalf("stored payload was completed: %+v", stored)
	}
}
