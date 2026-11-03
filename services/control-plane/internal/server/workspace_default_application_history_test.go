package server

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"opl-cloud/services/control-plane/internal/clients"
)

func TestWorkspaceDefaultApplicationHistoryDecoderMemory(t *testing.T) {
	testWorkspaceDefaultApplicationHistoryDecoder(t, newMemoryTableStore())
}

func TestWorkspaceDefaultApplicationHistoryDecoderPostgres(t *testing.T) {
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	testWorkspaceDefaultApplicationHistoryDecoder(t, store)
}

func testWorkspaceDefaultApplicationHistoryDecoder(t *testing.T, store controlPlaneTableStore) {
	t.Helper()
	ctx := context.Background()
	request := workspaceDefaultApplicationRequest{
		SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-history"), LaunchOperationID: "workspace-launch-history",
		AccountID: "acct-history", WorkspaceID: "ws-history", OwnerUserID: "usr-history", Sub2APIUserID: 41, WorkspaceKeyGroupID: 7,
		Revision: defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64)), Phase: "preparing_credentials",
	}
	row, err := workspaceDefaultApplicationRow(request)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(ctx, row))
	persisted, found, err := store.GetRuntimeOperation(ctx, request.OperationID)
	if err != nil || !found {
		t.Fatalf("historical request found=%v err=%v", found, err)
	}
	decoded, err := decodeWorkspaceDefaultApplication(persisted)
	if err != nil || !reflect.DeepEqual(decoded, request) || !workspaceDefaultApplicationPreparationBlocks(persisted) {
		t.Fatalf("historical preparing request=%+v err=%v", decoded, err)
	}

	// Exercise the real generic persistence -> JSON decoder -> history fence
	// boundary. Invalid JSON is intentionally not constructed by the fixture builder.
	payloadWith := func(key string, value any) string {
		t.Helper()
		var payload map[string]any
		if err := json.Unmarshal([]byte(stringValue(row["result"])), &payload); err != nil {
			t.Fatal(err)
		}
		payload[key] = value
		encoded, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		return string(encoded)
	}
	payloads := map[string]string{
		"truncated": `{`, "null": `null`, "empty": `{}`, "array": `[]`, "trailing": stringValue(row["result"]) + ` {}`,
		"schema type": payloadWith("schemaVersion", "1"), "schema version": payloadWith("schemaVersion", 2),
		"operation identity": payloadWith("operationId", "other-operation"), "launch identity": payloadWith("launchOperationId", "other-launch"),
		"account identity": payloadWith("accountId", "other-account"), "workspace identity": payloadWith("workspaceId", "other-workspace"),
		"missing owner": payloadWith("ownerUserId", ""), "invalid user": payloadWith("sub2apiUserId", 0), "user type": payloadWith("sub2apiUserId", "41"),
		"invalid group": payloadWith("workspaceKeyGroupId", 0), "revision type": payloadWith("revision", "invalid"),
		"revision typed field":  payloadWith("revision", map[string]any{"schemaVersion": "invalid"}),
		"invalid revision":      payloadWith("revision", map[string]any{"schemaVersion": 1, "applicationId": "opl-app"}),
		"non platform revision": payloadWith("revision", knowledgeApplicationRevisionForTest()),
		"secret typed field":    payloadWith("gatewaySecret", map[string]any{"version": 7}),
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			invalid := cloneMap(row)
			invalid["result"] = payload
			mustStore(t, store.SaveRuntimeOperation(ctx, invalid))
			actual, found, err := store.GetRuntimeOperation(ctx, request.OperationID)
			if err != nil || !found {
				t.Fatalf("generic history read found=%v err=%v", found, err)
			}
			if _, err := decodeWorkspaceDefaultApplication(actual); err == nil {
				t.Fatalf("decoder accepted malformed historical JSON: %s", payload)
			}
			if !workspaceDefaultApplicationPreparationBlocks(actual) {
				t.Fatal("invalid historical preparation lost its read fence")
			}
		})
	}
	for _, field := range []string{"id", "accountId", "workspaceId"} {
		t.Run("row "+field, func(t *testing.T) {
			identityRequest := request
			identityRequest.LaunchOperationID += ":" + field
			identityRequest.OperationID = workspaceDefaultApplicationOperationID(identityRequest.LaunchOperationID)
			invalid, err := workspaceDefaultApplicationRow(identityRequest)
			if err != nil {
				t.Fatal(err)
			}
			invalid[field] = stringValue(invalid[field]) + "-other"
			mustStore(t, store.SaveRuntimeOperation(ctx, invalid))
			actual, found, err := store.GetRuntimeOperation(ctx, stringValue(invalid["id"]))
			if err != nil || !found {
				t.Fatalf("generic row read found=%v err=%v", found, err)
			}
			if _, err := decodeWorkspaceDefaultApplication(actual); err == nil {
				t.Fatalf("decoder accepted row identity mismatch: %s", field)
			}
		})
	}
	other := cloneMap(row)
	other["action"] = workspaceApplicationDeploymentAction
	if _, err := decodeWorkspaceDefaultApplication(other); err == nil || workspaceDefaultApplicationPreparationBlocks(other) {
		t.Fatal("unrelated action was treated as default installation history")
	}
}

func TestWorkspaceDefaultApplicationHistoryProjection(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	ctx := context.Background()
	workspace, _, _ := fixture.store.GetWorkspace(ctx, "ws-alpha")
	request := workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: stringValue(workspace["ownerUserId"]), Sub2APIUserID: 41, WorkspaceKeyGroupID: 7, Revision: defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64))}
	for _, test := range []struct{ phase, status string }{
		{"credentials_required", "manual_review"}, {"preparing_credentials", "pending"}, {"waiting_resources", "pending"}, {"installing", "pending"}, {"failed", "manual_review"}, {"deployed", ""},
	} {
		t.Run(test.phase, func(t *testing.T) {
			request.Phase = test.phase
			row, err := workspaceDefaultApplicationRow(request)
			if err != nil {
				t.Fatal(err)
			}
			mustStore(t, fixture.store.SaveRuntimeOperation(ctx, row))
			projection := map[string]any{}
			mustStore(t, handler.app.projectWorkspaceApplicationInstallation(ctx, workspace, projection))
			installation, exists := projection["applicationInstallation"]
			if test.status == "" {
				if exists {
					t.Fatalf("completed history offered installation: %+v", projection)
				}
				return
			}
			expected := workspaceApplicationInstallationProjection{OperationID: request.OperationID, ApplicationID: request.Revision.ApplicationID, Revision: request.Revision.Version, Status: test.status}
			if !reflect.DeepEqual(installation, expected) {
				t.Fatalf("history projection=%+v want=%+v", installation, expected)
			}
			encoded, err := json.Marshal(installation)
			if err != nil {
				t.Fatal(err)
			}
			var actual map[string]any
			if err := json.Unmarshal(encoded, &actual); err != nil {
				t.Fatal(err)
			}
			if _, exists := actual["canResume"]; exists {
				t.Fatalf("retired resume field serialized: %s", encoded)
			}
			if actual["canRetry"] != false {
				t.Fatalf("history offered operator deployment retry: %s", encoded)
			}
			retained, _, err := fixture.store.GetRuntimeOperation(ctx, request.OperationID)
			if err != nil || retained["result"] != row["result"] {
				t.Fatal("projection mutated historical installation", err)
			}
		})
	}
}

type retiredDefaultApplicationScanStore struct {
	controlPlaneTableStore
	actions []string
}

func (s *retiredDefaultApplicationScanStore) PageRuntimeOperations(ctx context.Context, query runtimeOperationQuery) (tablePage, error) {
	s.actions = append(s.actions, query.Action)
	return s.controlPlaneTableStore.PageRuntimeOperations(ctx, query)
}

func TestWorkspaceApplicationWorkerDoesNotScanHistoricalDefaultInstallations(t *testing.T) {
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	handler := fixture.server.(*controlPlaneHTTPHandler)
	ctx := context.Background()
	request := workspaceDefaultApplicationRequest{SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: stringValue(fixture.workspace["ownerUserId"]), Sub2APIUserID: 41, WorkspaceKeyGroupID: 7, Revision: defaultOPLApplicationRevision("registry.example/opl-app@sha256:" + strings.Repeat("a", 64)), Phase: "waiting_resources", WorkspaceAPIKeyID: 19, GatewaySecret: &clients.GatewaySecretWriteResult{SecretRef: "historical-gateway", Version: "v1"}}
	row, err := workspaceDefaultApplicationRow(request)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, fixture.store.SaveRuntimeOperation(ctx, row))
	operations, err := fixture.store.ListRuntimeOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	workspace, _, _ := fixture.store.GetWorkspace(ctx, request.WorkspaceID)
	scan := &retiredDefaultApplicationScanStore{controlPlaneTableStore: fixture.store}
	handler.app.tables = scan
	mustStore(t, handler.app.runWorkspaceApplicationDeploymentsOnce(ctx, handler.service))
	if !reflect.DeepEqual(scan.actions, []string{workspaceApplicationDeploymentAction}) {
		t.Fatalf("deployment worker scanned retired actions: %v", scan.actions)
	}
	retained, err := fixture.store.ListRuntimeOperations(ctx)
	if err != nil || !reflect.DeepEqual(retained, operations) {
		t.Fatal("worker changed installation or order history", err)
	}
	after, _, err := fixture.store.GetWorkspace(ctx, request.WorkspaceID)
	if err != nil || !reflect.DeepEqual(after, workspace) {
		t.Fatal("worker changed workspace selection", err)
	}
}
