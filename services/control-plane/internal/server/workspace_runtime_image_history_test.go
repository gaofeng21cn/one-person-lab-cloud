package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const retainedImageInstalled = "registry.example/workspace@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const retainedImageActive = "registry.example/workspace@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func configureRetainedImageCatalog(t *testing.T) {
	t.Helper()
	t.Setenv("OPL_WORKSPACE_IMAGE", retainedImageInstalled)
	t.Setenv(contracts.WorkspaceImageReleasesEnv, `{"schemaVersion":1,"releases":[{"version":"26.8.26","image":"`+retainedImageInstalled+`"},{"version":"26.8.4","image":"`+retainedImageActive+`"}]}`)
}

// Seed the persisted format directly: these readers must not need a retired
// activation command or replacement worker to interpret historical records.
func retainedImagePolicyRow() map[string]any {
	return map[string]any{
		"id": workspaceImageReleasePolicyID, "operationId": workspaceImageReleasePolicyID,
		"resourceId": workspaceImageReleasePolicyID, "resourceKind": "workspace_image_release_policy",
		"action": workspaceImageReleasePolicyAction, "status": "active",
		"result": `{"schemaVersion":1,"revision":7,"activeVersion":"26.8.4","activeImage":"` + retainedImageActive + `","updatedAt":"2026-08-26T01:02:03Z","updatedBy":"usr-admin","reason":"retained approved release"}`,
	}
}

func retainedImageReplacementRow(status string) map[string]any {
	return map[string]any{
		"id": "image-replacement-original", "operationId": "image-replacement-original",
		"accountId": "acct-alpha", "workspaceId": "ws-alpha", "resourceId": "ws-alpha", "resourceKind": "workspace_runtime",
		"action": workspaceRuntimeImageReplacementAction, "provider": "fabric", "status": status,
		"result": `{"requestHash":"original-request-hash","reason":"retained approved image","input":{"launchOperationId":"launch-original","accountId":"acct-alpha","workspaceId":"ws-alpha","computeId":"compute-original","storageId":"storage-original","attachmentId":"attachment-original","runtimeId":"runtime-original","runtimeOperationId":"launch-original:runtime","runtimeServiceName":"service-original","previousImageDigest":"` + retainedImageInstalled + `","replacementImageDigest":"` + retainedImageActive + `"},"runtime":{"id":"runtime-original","workspaceId":"ws-alpha","imageId":"` + retainedImageActive + `","status":"running","ready":true},"auditEvent":{"id":"audit-original","action":"workspace.runtime_image_replacement"}}`,
	}
}

func TestRetainedImagePolicyReadPreservesStoredRevisionAndInstalledDefault(t *testing.T) {
	configureRetainedImageCatalog(t)
	store := newMemoryTableStore()
	original := retainedImagePolicyRow()
	mustStore(t, store.SaveRuntimeOperation(context.Background(), original))
	server := newServerForRetainedImages(t, store)
	operator := reservedOperatorSessionForTest(t, server)
	response := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspace-runtime-image-policy", "")
	var envelope struct {
		Data workspaceImageReleasePolicyDTO `json:"data"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
		t.Fatalf("policy read=%d %s", response.Code, response.Body.String())
	}
	policy := envelope.Data
	if policy.Revision != 7 || policy.Active.Version != "26.8.4" || policy.Active.Image != retainedImageActive || policy.InstalledDefault.Image != retainedImageInstalled || len(policy.Releases) != 2 || policy.UpdatedBy != "usr-admin" || policy.Reason != "retained approved release" {
		t.Fatalf("stored policy lost identity: %#v", policy)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" {
		t.Fatalf("policy cache=%q", response.Header().Get("Cache-Control"))
	}
	after, found, err := store.GetRuntimeOperation(context.Background(), workspaceImageReleasePolicyID)
	if err != nil || !found || !reflect.DeepEqual(after, original) || len(store.auditEvents) != 0 {
		t.Fatalf("read changed policy: %#v %v", after, err)
	}
}

func TestRetainedImagePolicyMissingRecordUsesInstalledDefaultWithoutPersisting(t *testing.T) {
	configureRetainedImageCatalog(t)
	store := newMemoryTableStore()
	server := newServerForRetainedImages(t, store)
	policy, _, _, err := server.(*controlPlaneHTTPHandler).app.currentWorkspaceImageReleasePolicy(context.Background())
	if err != nil || policy.Revision != 1 || policy.ActiveImage != retainedImageInstalled || len(store.runtimeOps) != 0 {
		t.Fatalf("installed policy=%#v error=%v", policy, err)
	}
}

type retainedImageReadFailureStore struct{ controlPlaneTableStore }

func (s retainedImageReadFailureStore) GetRuntimeOperation(context.Context, string) (map[string]any, bool, error) {
	return nil, false, errors.New("retained storage unavailable")
}

func TestRetainedImagePolicyReadRejectsCorruptAndUnavailableState(t *testing.T) {
	configureRetainedImageCatalog(t)
	for _, tc := range []struct {
		name        string
		mutate      func(map[string]any)
		unavailable bool
	}{
		{name: "malformed JSON", mutate: func(row map[string]any) { row["result"] = `{` }},
		{name: "wrong identity", mutate: func(row map[string]any) { row["resourceId"] = "other" }},
		{name: "wrong status", mutate: func(row map[string]any) { row["status"] = "failed" }},
		{name: "unknown active release", mutate: func(row map[string]any) {
			row["result"] = strings.Replace(stringValue(row["result"]), "26.8.4", "unknown", 1)
		}},
		{name: "invalid revision", mutate: func(row map[string]any) {
			row["result"] = strings.Replace(stringValue(row["result"]), `"revision":7`, `"revision":0`, 1)
		}},
		{name: "read unavailable", unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryTableStore()
			row := retainedImagePolicyRow()
			if tc.mutate != nil {
				tc.mutate(row)
			}
			mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
			server := newServerForRetainedImages(t, store)
			operator := reservedOperatorSessionForTest(t, server)
			if tc.unavailable {
				server.(*controlPlaneHTTPHandler).app.tables = retainedImageReadFailureStore{store}
			}
			response := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspace-runtime-image-policy", "")
			if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), `"status":"unavailable"`) || strings.Contains(response.Body.String(), retainedImageInstalled) {
				t.Fatalf("invalid policy became available: %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestRetainedImageHistoryReadPreservesOriginalOperation(t *testing.T) {
	for _, status := range []string{"started", "succeeded", "failed"} {
		t.Run(status, func(t *testing.T) {
			store := newMemoryTableStore()
			row := retainedImageReplacementRow(status)
			mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
			server := newServerForRetainedImages(t, store)
			operator := reservedOperatorSessionForTest(t, server)
			path := "/api/operator/workspaces/ws-alpha/runtime-image-replacements/image-replacement-original"
			response := requestWithSession(t, server, operator, http.MethodGet, path, "")
			var got map[string]any
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &got) != nil || got["operationId"] != row["operationId"] || got["status"] != status || got["runtimeId"] != "runtime-original" || got["replacementImageDigest"] != retainedImageActive {
				t.Fatalf("history read=%d %s", response.Code, response.Body.String())
			}
			after, found, err := store.GetRuntimeOperation(context.Background(), stringValue(row["id"]))
			if err != nil || !found || !reflect.DeepEqual(after, row) || len(store.auditEvents) != 0 {
				t.Fatalf("history read changed operation: %#v %v", after, err)
			}
			wrongWorkspace := requestWithSession(t, server, operator, http.MethodGet, strings.Replace(path, "ws-alpha", "ws-other", 1), "")
			if wrongWorkspace.Code != http.StatusNotFound {
				t.Fatalf("cross workspace history=%d", wrongWorkspace.Code)
			}
		})
	}
}

func TestRetainedImageHistoryReadRejectsCorruptAndUnavailableState(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		store := newMemoryTableStore()
		row := retainedImageReplacementRow("started")
		row["result"] = `{`
		mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
		server := newServerForRetainedImages(t, store)
		operator := reservedOperatorSessionForTest(t, server)
		if unavailable {
			server.(*controlPlaneHTTPHandler).app.tables = retainedImageReadFailureStore{store}
		}
		response := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspaces/ws-alpha/runtime-image-replacements/image-replacement-original", "")
		if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "state_read_failed") {
			t.Fatalf("invalid history unavailable=%v read=%d %s", unavailable, response.Code, response.Body.String())
		}
	}
}

func TestRetainedImageReadersRequireOperatorAuthentication(t *testing.T) {
	store := newMemoryTableStore()
	server := newServerForRetainedImages(t, store)
	customer := tenantOwnerSessionForTest(t, server)
	for _, path := range []string{"/api/operator/workspace-runtime-image-policy", "/api/operator/workspaces/ws-alpha/runtime-image-replacements/image-replacement-original"} {
		unauthenticated := httptest.NewRecorder()
		server.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, path, nil))
		forbidden := requestWithSession(t, server, customer, http.MethodGet, path, "")
		if unauthenticated.Code != http.StatusUnauthorized || forbidden.Code != http.StatusForbidden {
			t.Fatalf("%s auth=%d/%d", path, unauthenticated.Code, forbidden.Code)
		}
	}
}

func TestRetainedImageDecoderAndTransitionConsumers(t *testing.T) {
	workspace := map[string]any{"id": "ws-alpha", "accountId": "acct-alpha"}
	for _, status := range []string{"started", "succeeded", "failed"} {
		row := retainedImageReplacementRow(status)
		operation, gotStatus, err := decodeWorkspaceRuntimeImageReplacementOperation(row, "acct-alpha", "ws-alpha")
		if err != nil || gotStatus != status || operation.Input.LaunchOperationID != "launch-original" || operation.AuditEvent["id"] != "audit-original" {
			t.Fatalf("historical decoder=%#v %s %v", operation, gotStatus, err)
		}
		if pending := operatorRuntimeTransitionPending(workspace, []map[string]any{row}); pending != (status == "started") {
			t.Fatalf("status=%s pending=%v", status, pending)
		}
		if blocked := workspaceResourceReconcileBlocked([]map[string]any{row}); blocked != (status == "started") {
			t.Fatalf("status=%s resource reconcile blocked=%v", status, blocked)
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(map[string]any)
	}{
		{"malformed JSON", func(row map[string]any) { row["result"] = `{` }},
		{"different account", func(row map[string]any) { row["accountId"] = "acct-other" }},
		{"different workspace", func(row map[string]any) { row["workspaceId"] = "ws-other" }},
		{"different action", func(row map[string]any) { row["action"] = "workspace.other" }},
		{"unknown status", func(row map[string]any) { row["status"] = "unknown" }},
		{"missing original runtime operation", func(row map[string]any) {
			row["result"] = strings.Replace(stringValue(row["result"]), `"runtimeOperationId":"launch-original:runtime"`, `"runtimeOperationId":""`, 1)
		}},
		{"different payload owner", func(row map[string]any) {
			row["result"] = strings.Replace(stringValue(row["result"]), `"accountId":"acct-alpha"`, `"accountId":"acct-other"`, 1)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := retainedImageReplacementRow("started")
			tc.mutate(row)
			if _, _, err := decodeWorkspaceRuntimeImageReplacementOperation(row, "acct-alpha", "ws-alpha"); !errors.Is(err, errWorkspaceRuntimeImageReplacementState) {
				t.Fatalf("invalid history accepted: %v", err)
			}
			if operatorRuntimeTransitionPending(workspace, []map[string]any{row}) {
				t.Fatal("invalid historical record presented as a valid runtime transition")
			}
		})
	}
}

func TestPostgresRetainedImageReadersPreserveRecordsAcrossServerRestart(t *testing.T) {
	configureRetainedImageCatalog(t)
	store := newPostgresWorkspaceRenewalStore(t)
	policyRow, historyRow := retainedImagePolicyRow(), retainedImageReplacementRow("succeeded")
	for _, row := range []map[string]any{policyRow, historyRow} {
		mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	}
	for restart := 0; restart < 2; restart++ {
		server := newServerForRetainedImages(t, store)
		operator := reservedOperatorSessionForTest(t, server)
		response := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspace-runtime-image-policy", "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"revision":7`) || !strings.Contains(response.Body.String(), retainedImageActive) {
			t.Fatalf("restart %d policy=%d %s", restart, response.Code, response.Body.String())
		}
		response = requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspaces/ws-alpha/runtime-image-replacements/image-replacement-original", "")
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"operationId":"image-replacement-original"`) {
			t.Fatalf("restart %d history=%d %s", restart, response.Code, response.Body.String())
		}
		for _, original := range []map[string]any{policyRow, historyRow} {
			after, found, err := store.GetRuntimeOperation(context.Background(), stringValue(original["id"]))
			if err != nil || !found || after["result"] != original["result"] || after["status"] != original["status"] {
				t.Fatalf("restart %d changed retained row: %#v %v", restart, after, err)
			}
		}
	}
}

func newServerForRetainedImages(t *testing.T, store controlPlaneTableStore) http.Handler {
	t.Helper()
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func TestRetainedLaunchImageRecoveryCanAdvancePersistedPolicy(t *testing.T) {
	store := newMemoryTableStore()
	fabric := &workspaceLaunchRuntimeImageRevisionResumeFabric{}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previousImage := operation.stringFact("workspaceImageDigest")
	targetImage := "registry.example/workspace@sha256:" + strings.Repeat("e", 64)
	catalog, err := json.Marshal(contracts.WorkspaceImageReleaseCatalog{SchemaVersion: 1, Releases: []contracts.WorkspaceImageRelease{
		{Version: "1", Image: previousImage}, {Version: "2", Image: targetImage},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPL_WORKSPACE_IMAGE", targetImage)
	t.Setenv(contracts.WorkspaceImageReleasesEnv, string(catalog))
	policy := retainedImagePolicyRow()
	encoded, err := json.Marshal(workspaceImageReleasePolicy{SchemaVersion: 1, Revision: 7, ActiveVersion: "1", ActiveImage: previousImage})
	if err != nil {
		t.Fatal(err)
	}
	policy["result"] = string(encoded)
	mustStore(t, store.SaveRuntimeOperation(context.Background(), policy))
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	activation := requestWithMutationKeyForTest(t, server, operator, http.MethodPost,
		"/api/operator/workspace-image-release-activations", `{"releaseVersion":"2","expectedRevision":7,"reason":"approve qualified image for the original failed Launch"}`, "retained-launch-policy-advance")
	if activation.Code != http.StatusOK {
		t.Fatalf("original Launch policy advancement=%d %s", activation.Code, activation.Body.String())
	}
	body, err := json.Marshal(map[string]any{"launchVersion": operation.Version, "authorizedStage": string(contracts.StageRuntime),
		"reason": "recover the exact original failed Runtime", "mutationBudget": 0, "idempotentReplayBudget": 1,
		"authoritativeReadBudget": 3, "replacementWorkspaceImageDigest": targetImage})
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost,
		"/api/operator/workspace-launches/"+operation.ID+"/resume", string(body), "retained-launch-image-recovery")
	if response.Code != http.StatusOK || len(fabric.ensures) != 1 {
		t.Fatalf("original Launch recovery=%d %s ensures=%d", response.Code, response.Body.String(), len(fabric.ensures))
	}
}
