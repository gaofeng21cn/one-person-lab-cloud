package server

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
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
