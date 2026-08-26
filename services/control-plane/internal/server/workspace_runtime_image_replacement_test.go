package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"opl-cloud/services/control-plane/internal/clients"
)

type runtimeImageReplacementRouteFabric struct {
	fakeFabricClient
	replacementRuntime clients.WorkspaceRuntime
	replacementInput   clients.WorkspaceRuntimeImageReplacementInput
	replacementCalls   atomic.Int32
	replacementErr     error
}

func (f *runtimeImageReplacementRouteFabric) ReplaceWorkspaceRuntimeImage(_ context.Context, input clients.WorkspaceRuntimeImageReplacementInput, _ string) (clients.WorkspaceRuntimeImageReplacementResult, error) {
	f.replacementCalls.Add(1)
	f.replacementInput = input
	if f.replacementErr != nil {
		return clients.WorkspaceRuntimeImageReplacementResult{}, f.replacementErr
	}
	return clients.WorkspaceRuntimeImageReplacementResult{
		SchemaVersion: 1, OperationID: input.IdempotencyKey, WorkspaceID: input.WorkspaceID, RuntimeID: input.RuntimeID,
		PreviousImageDigest: input.PreviousImageDigest, ReplacementImageDigest: input.ReplacementImageDigest,
		Status: "succeeded", Runtime: f.replacementRuntime,
	}, nil
}

func TestWorkspaceRuntimeImageReplacementTerminalConflictDoesNotRemainStarted(t *testing.T) {
	const workspaceID = "ws-alpha"
	const oldImage = "registry.example/workspace@sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const newImage = "registry.example/workspace@sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const key = "case-20260826-runtime-conflict"

	t.Setenv("OPL_WORKSPACE_IMAGE", newImage)
	t.Setenv("OPL_WORKSPACE_RUNTIME_IMAGE_REPLACEMENT_WORKER_ENABLED", "0")
	store := newMemoryTableStore()
	fabric := &runtimeImageReplacementRouteFabric{
		fakeFabricClient: fakeFabricClient{runtimeStatus: clients.WorkspaceRuntime{
			ID: "rt-unit", OperationID: "workspace-launch-unit:runtime", WorkspaceID: workspaceID,
			URL: "https://workspace.medopl.cn/w/ws-alpha/", Status: "running", ServiceName: "runtime-unit", ImageID: oldImage, Ready: true,
		}},
		replacementErr: &clients.FabricHTTPError{StatusCode: http.StatusConflict, Body: `{"error":"workspace_runtime_image_replacement_conflict"}`},
	}
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, fabric), store)
	if err != nil {
		t.Fatal(err)
	}
	seedCanonicalRuntimeAccessWorkspaceForTest(t, store, "usr-alpha")
	operator := reservedOperatorSessionForTest(t, server)
	body := `{"replacementImageDigest":"` + newImage + `","reason":"owner chain changed"}`
	created := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspaces/"+workspaceID+"/runtime-image-replacements", body, key)
	if created.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	handler := server.(*controlPlaneHTTPHandler)
	if err := handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), handler.service); err == nil {
		t.Fatal("terminal conflict unexpectedly succeeded")
	}
	operationID := workspaceRuntimeImageReplacementOperationID(workspaceID, key)
	row, found, err := store.GetRuntimeOperation(context.Background(), operationID)
	if err != nil || !found || row["status"] != "failed" || row["errorCode"] != "workspace_runtime_image_replacement_conflict" {
		t.Fatalf("persisted terminal operation=%#v found=%v err=%v", row, found, err)
	}
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspaces/"+workspaceID+"/runtime-image-replacements", body, key)
	if replay.Code != http.StatusAccepted || !strings.Contains(replay.Body.String(), `"status":"failed"`) || !strings.Contains(replay.Body.String(), `"errorCode":"workspace_runtime_image_replacement_conflict"`) {
		t.Fatalf("failed replay status=%d body=%s", replay.Code, replay.Body.String())
	}
}

type runtimeImageReplacementRouteResponse struct {
	OperationID         string                   `json:"operationId"`
	Status              string                   `json:"status"`
	WorkspaceID         string                   `json:"workspaceId"`
	RuntimeID           string                   `json:"runtimeId"`
	PreviousImageDigest string                   `json:"previousImageDigest"`
	ReplacementDigest   string                   `json:"replacementImageDigest"`
	Runtime             clients.WorkspaceRuntime `json:"runtime"`
}

func TestWorkspaceRuntimeImageReplacementRoutePersistsAndReplaysExactOperation(t *testing.T) {
	const workspaceID = "ws-alpha"
	const oldImage = "registry.example/workspace@sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const newImage = "registry.example/workspace@sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const key = "case-20260826-runtime-image"
	const reason = "promote the protected Workspace image release"

	t.Setenv("OPL_WORKSPACE_IMAGE", newImage)
	t.Setenv("OPL_WORKSPACE_RUNTIME_IMAGE_REPLACEMENT_WORKER_ENABLED", "0")
	store := newMemoryTableStore()
	fabric := &runtimeImageReplacementRouteFabric{
		fakeFabricClient: fakeFabricClient{runtimeStatus: clients.WorkspaceRuntime{
			ID: "rt-unit", OperationID: "workspace-launch-unit:runtime", WorkspaceID: workspaceID,
			URL: "https://workspace.medopl.cn/w/ws-alpha/", Status: "running", ServiceName: "runtime-unit", ImageID: oldImage, Ready: true,
		}},
		replacementRuntime: clients.WorkspaceRuntime{
			ID: "rt-unit", OperationID: "workspace-launch-unit:runtime", WorkspaceID: workspaceID,
			URL: "https://workspace.medopl.cn/w/ws-alpha/", Status: "running", ServiceName: "runtime-unit", ImageID: newImage, Ready: true,
		},
	}
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, fabric), store)
	if err != nil {
		t.Fatal(err)
	}
	seedCanonicalRuntimeAccessWorkspaceForTest(t, store, "usr-alpha")
	operator := reservedOperatorSessionForTest(t, server)
	body := `{"replacementImageDigest":"` + newImage + `","reason":"` + reason + `"}`

	first := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspaces/"+workspaceID+"/runtime-image-replacements", body, key)
	if first.Code != http.StatusAccepted {
		t.Fatalf("create status=%d body=%s", first.Code, first.Body.String())
	}
	var created runtimeImageReplacementRouteResponse
	if err := json.NewDecoder(first.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Status != "started" || created.WorkspaceID != workspaceID || created.PreviousImageDigest != oldImage || created.ReplacementDigest != newImage {
		t.Fatalf("create response=%#v", created)
	}

	handler := server.(*controlPlaneHTTPHandler)
	if err := handler.app.runWorkspaceRuntimeImageReplacementsOnce(context.Background(), handler.service); err != nil {
		t.Fatal(err)
	}
	if fabric.replacementCalls.Load() != 1 {
		t.Fatalf("replacement calls=%d, want 1", fabric.replacementCalls.Load())
	}
	if fabric.replacementInput.LaunchOperationID != "workspace-launch-alpha" || fabric.replacementInput.RuntimeOperationID != "workspace-launch-unit:runtime" {
		t.Fatalf("replacement owner chain input=%#v", fabric.replacementInput)
	}

	// The provider has now moved to the new digest. Replaying the same key must
	// return the persisted operation before a second runtime read or mutation.
	fabric.fakeFabricClient.runtimeStatus = fabric.replacementRuntime
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspaces/"+workspaceID+"/runtime-image-replacements", body, key)
	if replay.Code != http.StatusAccepted {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	var replayed runtimeImageReplacementRouteResponse
	if err := json.NewDecoder(replay.Body).Decode(&replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.OperationID != created.OperationID || replayed.Status != "succeeded" || replayed.Runtime.ImageID != newImage || fabric.replacementCalls.Load() != 1 {
		t.Fatalf("replay=%#v calls=%d", replayed, fabric.replacementCalls.Load())
	}

	changedBody := `{"replacementImageDigest":"` + strings.Replace(newImage, "b", "c", 64) + `","reason":"` + reason + `"}`
	changed := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspaces/"+workspaceID+"/runtime-image-replacements", changedBody, key)
	if changed.Code != http.StatusConflict || !strings.Contains(changed.Body.String(), "idempotency_conflict") {
		t.Fatalf("changed replay status=%d body=%s", changed.Code, changed.Body.String())
	}
}

func TestWorkspaceRuntimeImageReplacementReadRoutesExposeOnlyProtectedTargetAndPreview(t *testing.T) {
	const workspaceID = "ws-alpha"
	const oldImage = "registry.example/workspace@sha256:" + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const newImage = "registry.example/workspace@sha256:" + "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	t.Setenv("OPL_WORKSPACE_IMAGE", newImage)
	t.Setenv("OPL_WORKSPACE_RUNTIME_IMAGE_REPLACEMENT_WORKER_ENABLED", "0")
	store := newMemoryTableStore()
	fabric := &runtimeImageReplacementRouteFabric{fakeFabricClient: fakeFabricClient{runtimeStatus: clients.WorkspaceRuntime{
		ID: "rt-unit", OperationID: "workspace-launch-unit:runtime", WorkspaceID: workspaceID,
		URL: "https://workspace.medopl.com/w/ws-alpha/", Status: "running", ServiceName: "runtime-unit", ImageID: oldImage, Ready: true,
	}}}
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, fabric), store)
	if err != nil {
		t.Fatal(err)
	}
	seedCanonicalRuntimeAccessWorkspaceForTest(t, store, "usr-alpha")
	operator := reservedOperatorSessionForTest(t, server)

	policy := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspace-runtime-image-policy", "")
	if policy.Code != http.StatusOK || !strings.Contains(policy.Body.String(), `"image":"`+newImage+`"`) || !strings.Contains(policy.Body.String(), `"digest":"sha256:`) {
		t.Fatalf("policy status=%d body=%s", policy.Code, policy.Body.String())
	}
	preview := requestWithSession(t, server, operator, http.MethodGet, "/api/operator/workspaces/"+workspaceID+"/runtime-image-replacements/preview", "")
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"currentImageDigest":"`+oldImage+`"`) || !strings.Contains(preview.Body.String(), `"targetImageDigest":"`+newImage+`"`) || !strings.Contains(preview.Body.String(), `"canReplace":true`) {
		t.Fatalf("preview status=%d body=%s", preview.Code, preview.Body.String())
	}
}
