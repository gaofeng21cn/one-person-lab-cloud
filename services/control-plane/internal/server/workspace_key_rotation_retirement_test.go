package server

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestWorkspaceKeyRotationFreshRequestsLeaveNoAcceptedOperation(t *testing.T) {
	fixture := newWorkspaceKeyRotationFixture(t, "")
	for _, key := range []string{"new-rotation", "new-rotation", "another-rotation"} {
		response := fixture.rotate(t, key)
		if response.Code != http.StatusGone || !strings.Contains(response.Body.String(), "workspace_key_rotation_creation_retired") {
			t.Fatalf("fresh rotation status=%d body=%s", response.Code, response.Body.String())
		}
	}
	operations, err := fixture.store.ListRuntimeOperations(context.Background())
	if err != nil || len(operations) != 0 {
		t.Fatalf("withdrawn command accepted an operation: count=%d err=%v", len(operations), err)
	}
	audits, err := fixture.store.ListAuditEvents(context.Background(), "acct-alpha")
	if err != nil || len(audits) != 0 {
		t.Fatalf("withdrawn command created audit: count=%d err=%v", len(audits), err)
	}
	assertWorkspaceKeyRotationNoExternalWrites(t, fixture)
	workspace, found, err := fixture.store.GetWorkspace(context.Background(), "ws-alpha")
	if err != nil || !found || int64(numberField(workspace, "workspaceApiKeyId", 0)) != 9 {
		t.Fatalf("withdrawn command changed Workspace binding: found=%v err=%v", found, err)
	}
}

func TestWorkspaceKeyRotationRecoveryRejectsUntrustedStoredOperation(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		mutate func(map[string]any)
	}{
		{name: "malformed result", status: http.StatusInternalServerError, mutate: func(row map[string]any) { row["result"] = `{` }},
		{name: "different account", status: http.StatusInternalServerError, mutate: func(row map[string]any) { row["accountId"] = "another-account" }},
		{name: "different workspace", status: http.StatusInternalServerError, mutate: func(row map[string]any) { row["workspaceId"] = "ws-beta" }},
		{name: "different action", status: http.StatusInternalServerError, mutate: func(row map[string]any) { row["action"] = "workspace.gateway_key.reveal" }},
		{name: "different request", status: http.StatusConflict, mutate: func(row map[string]any) {
			operation, err := decodeWorkspaceKeyRotation(row)
			if err != nil {
				panic(err)
			}
			operation.RequestHash = "different-request"
			row["result"] = encodeWorkspaceKeyRotation(operation)
		}},
		{name: "invalid status", status: http.StatusInternalServerError, mutate: func(row map[string]any) { row["status"] = "unknown" }},
		{name: "unproven success", status: http.StatusInternalServerError, mutate: func(row map[string]any) { row["status"] = "succeeded" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWorkspaceKeyRotationFixture(t, "")
			fixture.accept(t, "original-rotation")
			operationID := "workspace-key-rotate-" + stableID("ws-alpha", "original-rotation")[:18]
			row, found, err := fixture.store.GetRuntimeOperation(context.Background(), operationID)
			if err != nil || !found {
				t.Fatalf("accepted operation found=%v err=%v", found, err)
			}
			test.mutate(row)
			mustStore(t, fixture.store.SaveRuntimeOperation(context.Background(), row))
			response := fixture.rotate(t, "original-rotation")
			if response.Code != test.status || strings.Contains(response.Body.String(), "creation_retired") {
				t.Fatalf("untrusted state masked as absence: status=%d body=%s", response.Code, response.Body.String())
			}
			after, found, err := fixture.store.GetRuntimeOperation(context.Background(), operationID)
			if err != nil || !found || !reflect.DeepEqual(after, row) {
				t.Fatalf("rejected recovery changed accepted evidence: found=%v err=%v", found, err)
			}
			assertWorkspaceKeyRotationNoExternalWrites(t, fixture)
		})
	}
}

type unavailableWorkspaceKeyRotationStore struct {
	controlPlaneTableStore
	operationID string
}

func (s unavailableWorkspaceKeyRotationStore) GetRuntimeOperation(ctx context.Context, id string) (map[string]any, bool, error) {
	if id == s.operationID {
		return nil, false, errors.New("operation read unavailable")
	}
	return s.controlPlaneTableStore.GetRuntimeOperation(ctx, id)
}

func TestWorkspaceKeyRotationReadFailureIsNotRetirement(t *testing.T) {
	fixture := newWorkspaceKeyRotationFixture(t, "")
	app := fixture.server.(*controlPlaneHTTPHandler).app
	app.tables = unavailableWorkspaceKeyRotationStore{
		controlPlaneTableStore: fixture.store,
		operationID:            "workspace-key-rotate-" + stableID("ws-alpha", "read-unavailable")[:18],
	}
	response := fixture.rotate(t, "read-unavailable")
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "state_persist_failed") {
		t.Fatalf("unknown operation read became absence: status=%d body=%s", response.Code, response.Body.String())
	}
	assertWorkspaceKeyRotationNoExternalWrites(t, fixture)
}

func TestWorkspaceKeyRotationRecoveryKeepsRequestAndOwnerChecks(t *testing.T) {
	fixture := newWorkspaceKeyRotationFixture(t, "")
	fixture.accept(t, "original-rotation")
	path := "/api/workspaces/ws-alpha/workspace-key/rotate"
	for _, body := range []string{`{"unexpected":true}`, `{} {}`, `{`} {
		response := requestWithMutationKeyForTest(t, fixture.server, fixture.session, http.MethodPost, path, body, "original-rotation")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("invalid recovery request status=%d body=%s", response.Code, response.Body.String())
		}
	}
	missingKey := fixture.rotate(t, "")
	if missingKey.Code != http.StatusBadRequest {
		t.Fatalf("missing recovery identity status=%d", missingKey.Code)
	}
	app := fixture.server.(*controlPlaneHTTPHandler).app
	_, err := app.createUser(context.Background(), fixture.service, map[string]any{
		"email": "other-rotation-owner@example.com", "accountId": "acct-beta", "password": "CorrectHorseBatteryStaple!",
	})
	if err != nil {
		t.Fatal(err)
	}
	other := loginForTest(t, fixture.server, "other-rotation-owner@example.com", "CorrectHorseBatteryStaple!")
	response := requestWithMutationKeyForTest(t, fixture.server, other, http.MethodPost, path, `{}`, "original-rotation")
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-owner resumed accepted operation: status=%d body=%s", response.Code, response.Body.String())
	}
	assertWorkspaceKeyRotationNoExternalWrites(t, fixture)
}

func assertWorkspaceKeyRotationNoExternalWrites(t *testing.T, fixture workspaceKeyRotationFixture) {
	t.Helper()
	if fixture.client.createWrites != 0 || fixture.client.updateWrites != 0 || fixture.client.deleteWrites != 0 ||
		len(fixture.fabric.gatewaySecretInputs) != 0 || len(fixture.fabric.bindings) != 0 || len(fixture.fabric.runtimeInputs) != 0 || len(fixture.ledger.receipts) != 0 {
		t.Fatal("rejected rotation crossed a Gateway, Secret, Runtime or Ledger mutation boundary")
	}
}
