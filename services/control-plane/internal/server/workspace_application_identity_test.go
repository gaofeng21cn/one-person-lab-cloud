package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// derivedImage builds an immutable image reference for the derivation tests.
func derivedImage(digest string) string {
	return "registry.example/oplcloud/simple@sha256:" + strings.Repeat(digest, 64/len(digest))
}

// derivedRevision builds a minimal published description for the derivation tests.
func derivedRevision(image string) contracts.WorkspaceApplicationRevision {
	return contracts.WorkspaceApplicationRevision{
		SchemaVersion: 1, Platform: "linux/amd64", Image: image,
		Ports: []contracts.WorkspaceApplicationPort{{Name: "http", Port: 3000, Protocol: "TCP"}}, EntryPort: "http",
		ExposurePolicy: "application",
	}
}

// The application identity is stable for the Workspace's application slot, and the
// version is derived from the description's content, so the two halves answer
// different questions: an image update is a new version of the same application, and
// a different legal run description is a new version rather than a conflict.
func TestWorkspaceApplicationDerivedIdentitySeparatesApplicationFromVersion(t *testing.T) {
	firstApplicationID, firstVersion, err := workspaceApplicationDerivedIdentity("ws-alpha", derivedRevision(derivedImage("a")))
	if err != nil || firstApplicationID == "" || firstVersion == "" {
		t.Fatalf("derivation failed: %q %q %v", firstApplicationID, firstVersion, err)
	}
	againApplicationID, againVersion, err := workspaceApplicationDerivedIdentity("ws-alpha", derivedRevision(derivedImage("a")))
	if err != nil || againApplicationID != firstApplicationID || againVersion != firstVersion {
		t.Fatalf("derivation is not stable: %q/%q vs %q/%q (%v)", firstApplicationID, firstVersion, againApplicationID, againVersion, err)
	}
	// A new image on the same application keeps the identity and becomes a new version.
	upgradedApplicationID, upgradedVersion, err := workspaceApplicationDerivedIdentity("ws-alpha", derivedRevision(derivedImage("b")))
	if err != nil || upgradedApplicationID != firstApplicationID {
		t.Fatalf("an image update changed the application identity: %q vs %q (%v)", upgradedApplicationID, firstApplicationID, err)
	}
	if upgradedVersion == firstVersion {
		t.Fatal("an image update did not produce a new version")
	}
	// A different legal run description for the same image is a new version too, so
	// it cannot conflict with the description already admitted.
	otherPort := derivedRevision(derivedImage("a"))
	otherPort.Ports = []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8081, Protocol: "TCP"}}
	describedApplicationID, describedVersion, err := workspaceApplicationDerivedIdentity("ws-alpha", otherPort)
	if err != nil || describedApplicationID != firstApplicationID || describedVersion == firstVersion {
		t.Fatalf("a different run description was not a new version of the same application: %q/%q (%v)", describedApplicationID, describedVersion, err)
	}
	// A different Workspace is a different application, so the same image never
	// collides across Workspaces.
	otherWorkspaceApplicationID, _, err := workspaceApplicationDerivedIdentity("ws-beta", derivedRevision(derivedImage("a")))
	if err != nil || otherWorkspaceApplicationID == firstApplicationID {
		t.Fatalf("two Workspaces shared one application identity: %q vs %q (%v)", otherWorkspaceApplicationID, firstApplicationID, err)
	}
	// The derived identity must satisfy the same contract the owner validates.
	revision := derivedRevision(derivedImage("a"))
	revision.ApplicationID, revision.Version = firstApplicationID, firstVersion
	if err := contracts.ValidateWorkspaceApplicationRevision(revision); err != nil {
		t.Fatalf("derived identity rejected by the contract: %v", err)
	}
	// Without a digest or a Workspace there is nothing to derive from, so the
	// command is refused rather than given a fabricated identity.
	for _, invalid := range []string{"", "repo.example/app:latest", "repo.example/app@sha256:short"} {
		if _, _, err := workspaceApplicationDerivedIdentity("ws-alpha", derivedRevision(invalid)); err == nil {
			t.Fatalf("derivation accepted %q", invalid)
		}
	}
	if _, _, err := workspaceApplicationDerivedIdentity("", derivedRevision(derivedImage("a"))); err == nil {
		t.Fatal("derivation accepted an empty Workspace")
	}
	// An invalid description has no derivable content.
	invalid := derivedRevision(derivedImage("a"))
	invalid.EntryPort = ""
	if _, _, err := workspaceApplicationDerivedIdentity("ws-alpha", invalid); err == nil {
		t.Fatal("derivation accepted an invalid description")
	}
}

// A deployment whose description changed keeps the same data namespace, so updating
// an application's image never silently moves its data or its published entry.
func TestWorkspaceApplicationImageUpdateKeepsDataNamespace(t *testing.T) {
	firstApplicationID, _, err := workspaceApplicationDerivedIdentity("ws-alpha", derivedRevision(derivedImage("a")))
	if err != nil {
		t.Fatal(err)
	}
	upgradedApplicationID, _, err := workspaceApplicationDerivedIdentity("ws-alpha", derivedRevision(derivedImage("b")))
	if err != nil {
		t.Fatal(err)
	}
	if firstApplicationID != upgradedApplicationID {
		t.Fatalf("image update changed the application identity and its data namespace: %s vs %s", firstApplicationID, upgradedApplicationID)
	}
}

// A deployment that was described by its image and never named by its operator is
// admitted under the platform-derived identity, and the admitted revision carries
// exactly the facts the description declared.
func TestWorkspaceApplicationDeploymentAdmitsDescriptionWithoutIdentity(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	ctx := context.Background()
	store := newMemoryTableStore()
	service := newTestService(&fakeLedgerClient{}, &applicationReplacementFabric{})
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	app := server.(*controlPlaneHTTPHandler).app
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-alpha", "ws-alpha")
	operator := operatorSessionForTest(t, server)

	image := derivedImage("c")
	description := map[string]any{
		"schemaVersion": 1, "platform": "linux/amd64", "image": image,
		"ports": []any{map[string]any{"name": "http", "port": 3000, "protocol": "TCP"}}, "entryPort": "http",
		"exposurePolicy": "application",
	}
	expectedApplicationID, expectedVersion, err := workspaceApplicationDerivedIdentity("ws-alpha", revisionFromDescription(t, description))
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(struct {
		WorkspaceID   string         `json:"workspaceId"`
		Configuration map[string]any `json:"configuration"`
		Revision      map[string]any `json:"revision"`
	}{
		WorkspaceID:   "ws-alpha",
		Configuration: map[string]any{},
		// The description carries the image and its run requirements, and no
		// application identity at all.
		Revision: description,
	})
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", string(payload), "deploy-derived-identity")
	if response.Code != http.StatusAccepted {
		t.Fatalf("deployment status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		Intent struct {
			ApplicationID  string `json:"applicationId"`
			TargetRevision string `json:"targetRevision"`
		} `json:"intent"`
	}
	if json.Unmarshal(response.Body.Bytes(), &body) != nil ||
		body.Intent.ApplicationID != expectedApplicationID || body.Intent.TargetRevision != expectedVersion {
		t.Fatalf("intent=%s want %s/%s", response.Body.String(), expectedApplicationID, expectedVersion)
	}
	// The derived revision is admitted once, so a retry resolves to it and the
	// stored payload keeps only what the description declared.
	row, found, err := store.AdmittedApplicationRevision(ctx, expectedApplicationID, expectedVersion)
	if err != nil || !found {
		t.Fatalf("admitted revision=%#v found=%v err=%v", row, found, err)
	}
	// The stored digest is the canonical revision digest, not the raw image digest:
	// the identity is derived from the image, and the snapshot keeps its own content
	// digest. It must be a real digest, and a repeat of the same description must
	// resolve to the very same admitted revision instead of creating a second one.
	admittedDigest := stringValue(row["digest"])
	if len(admittedDigest) != 64 {
		t.Fatalf("stored digest=%q", admittedDigest)
	}
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", string(payload), "deploy-derived-identity")
	if replay.Code != http.StatusAccepted {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	replayedRow, found, err := store.AdmittedApplicationRevision(ctx, expectedApplicationID, expectedVersion)
	if err != nil || !found || stringValue(replayedRow["digest"]) != admittedDigest || stringValue(replayedRow["id"]) != stringValue(row["id"]) {
		t.Fatalf("replay created a second revision: %#v vs %#v", replayedRow, row)
	}
	revision, valid := decodeApplicationRevisionPayload(stringValue(row["payload"]))
	if !valid || revision.ApplicationID != expectedApplicationID || revision.Version != expectedVersion ||
		len(revision.HealthChecks) != 0 || len(revision.PersistentMounts) != 0 || len(revision.Dependencies) != 0 {
		t.Fatalf("admitted revision=%#v", revision)
	}
	// A caller that names an identity that does not match the description is refused.
	conflicting, err := json.Marshal(struct {
		WorkspaceID   string         `json:"workspaceId"`
		ApplicationID string         `json:"applicationId"`
		Revision      string         `json:"targetRevision"`
		Configuration map[string]any `json:"configuration"`
		RevisionBody  map[string]any `json:"revision"`
	}{"ws-alpha", "named-app", "2.0.0", map[string]any{}, map[string]any{
		"schemaVersion": 1, "applicationId": expectedApplicationID, "version": expectedVersion, "platform": "linux/amd64",
		"image": image, "exposurePolicy": "application",
		"ports": []any{map[string]any{"name": "http", "port": 3000, "protocol": "TCP"}}, "entryPort": "http",
	}})
	if err != nil {
		t.Fatal(err)
	}
	rejected := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", string(conflicting), "deploy-identity-mismatch")
	if rejected.Code != http.StatusBadRequest {
		t.Fatalf("identity mismatch status=%d body=%s", rejected.Code, rejected.Body.String())
	}
	_ = app
}

// revisionFromDescription decodes one JSON description into the typed revision the
// platform admits, which is what the derivation reads.
func revisionFromDescription(t *testing.T, description map[string]any) contracts.WorkspaceApplicationRevision {
	t.Helper()
	payload, err := json.Marshal(description)
	if err != nil {
		t.Fatal(err)
	}
	var revision contracts.WorkspaceApplicationRevision
	if err := json.Unmarshal(payload, &revision); err != nil {
		t.Fatal(err)
	}
	return revision
}

// The review's Step 6 case: the same image with a different legal run description in
// another Workspace must deploy, not conflict.
func TestReviewStep6SameImageDifferentDescription(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	store := newMemoryTableStore()
	server, err := NewPersistentServer(newTestService(&fakeLedgerClient{}, &applicationReplacementFabric{}), store)
	if err != nil {
		t.Fatal(err)
	}
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-alpha", "ws-alpha")
	seedResourceOnlyActivatedWorkspace(t, store, "workspace-launch-beta", "ws-beta")
	operator := operatorSessionForTest(t, server)
	submit := func(workspaceID string, port int, key string) (int, string) {
		payload, marshalErr := json.Marshal(map[string]any{
			"workspaceId": workspaceID, "configuration": map[string]any{},
			"revision": map[string]any{
				"schemaVersion": 1, "platform": "linux/amd64", "image": derivedImage("c"),
				"ports": []any{map[string]any{"name": "http", "port": port, "protocol": "TCP"}}, "entryPort": "http",
				"exposurePolicy": "application",
			},
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/application-deployments", string(payload), key)
		return response.Code, response.Body.String()
	}
	first, body := submit("ws-alpha", 3000, "review-deploy-first")
	if first != http.StatusAccepted {
		t.Fatalf("first=%d %s", first, body)
	}
	second, body := submit("ws-beta", 8081, "review-deploy-second")
	if second != http.StatusAccepted {
		t.Fatalf("a valid independent description was rejected: %d %s", second, body)
	}
	// The same Workspace already has a deployment in flight (the worker is disabled in
	// this fixture), so another one is refused by the in-flight rule. What must never
	// happen is a revision conflict: a different legal description of the same image is
	// a new immutable version of that application, not a collision.
	third, body := submit("ws-alpha", 8081, "review-deploy-third")
	if third == http.StatusAccepted {
		return
	}
	if !strings.Contains(body, "workspace_application_deployment_intent_conflict") ||
		strings.Contains(body, "revision_conflict") {
		t.Fatalf("a different description of the same image conflicted on the revision: %d %s", third, body)
	}
}
