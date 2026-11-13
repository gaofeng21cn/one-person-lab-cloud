package server

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

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
