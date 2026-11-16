package server

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func TestPostgresRetiredImagePolicyActivationPreservesStoredPolicyAndAudit(t *testing.T) {
	configureRetainedImageCatalog(t)
	ctx := context.Background()
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	mustStore(t, store.SaveRuntimeOperation(ctx, retainedImagePolicyRow()))
	mustStore(t, store.SaveAuditEvent(ctx, map[string]any{
		"id": "retained-image-policy-audit", "actorUserId": "usr-admin", "actorRole": "operator",
		"action": "workspace.image_release_activation", "resourceKind": "workspace_image_release_policy",
		"resourceId": workspaceImageReleasePolicyID, "result": "succeeded",
		"before": map[string]any{"revision": 6},
		"after":  map[string]any{"revision": 7, "activeImage": retainedImageActive},
	}))
	server := newServerForRetainedImages(t, store)
	operator := reservedOperatorSessionForTest(t, server)
	beforePolicy, found, err := store.GetRuntimeOperation(ctx, workspaceImageReleasePolicyID)
	if err != nil || !found {
		t.Fatalf("load retained policy: found=%v err=%v", found, err)
	}
	beforeAudit, err := store.ListAuditEvents(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(beforeAudit) != 1 || beforeAudit[0]["id"] != "retained-image-policy-audit" {
		t.Fatalf("retained audit missing: %#v", beforeAudit)
	}
	assertUnchanged := func(t *testing.T) {
		t.Helper()
		afterPolicy, found, err := store.GetRuntimeOperation(ctx, workspaceImageReleasePolicyID)
		if err != nil || !found || !reflect.DeepEqual(afterPolicy, beforePolicy) {
			t.Fatalf("retired activation changed policy: %#v err=%v", afterPolicy, err)
		}
		afterAudit, err := store.ListAuditEvents(ctx, "")
		if err != nil || !reflect.DeepEqual(afterAudit, beforeAudit) {
			t.Fatalf("retired activation changed audit: %#v err=%v", afterAudit, err)
		}
	}
	body := workspaceImageReleaseActivationBodyForTest(t, "26.8.26", 7, "activate approved PostgreSQL Workspace release")
	changedBody := workspaceImageReleaseActivationBodyForTest(t, "26.8.4", 7, "retain approved PostgreSQL Workspace release")
	for _, request := range []struct {
		name, body, key string
	}{
		{"first request", body, "workspace-release-postgres"},
		{"same request replay", body, "workspace-release-postgres"},
		{"changed intent", changedBody, "workspace-release-postgres"},
		{"new key", body, "workspace-release-postgres-new"},
	} {
		t.Run(request.name, func(t *testing.T) {
			response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-image-release-activations", request.body, request.key)
			if response.Code != http.StatusNotFound {
				t.Fatalf("retired activation status=%d body=%s", response.Code, response.Body.String())
			}
			assertUnchanged(t)
		})
	}
	server = newServerForRetainedImages(t, store)
	operator = reservedOperatorSessionForTest(t, server)
	response := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspace-runtime-image-policy", "")
	var envelope struct {
		Data workspaceImageReleasePolicyDTO `json:"data"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
		t.Fatalf("retained policy read=%d %s", response.Code, response.Body.String())
	}
	if policy := envelope.Data; policy.Revision != 7 || policy.Active.Version != "26.8.4" || policy.Active.Image != retainedImageActive || policy.InstalledDefault.Image != retainedImageInstalled {
		t.Fatalf("retained policy changed after restart: %#v", policy)
	}
	assertUnchanged(t)
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
