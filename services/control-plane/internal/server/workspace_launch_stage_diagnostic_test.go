package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func TestWorkspaceLaunchGETRedactsDiagnosticCheckDetails(t *testing.T) {
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	client := &testSub2APIClient{
		identities: map[string]clients.Sub2APIIdentity{"alpha@example.com": {ID: 41, Email: "alpha@example.com", Status: "active"}},
		passwords:  map[string]string{"alpha@example.com": "CorrectHorseBatteryStaple!"},
	}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	session := loginForTest(t, server, "alpha@example.com", "CorrectHorseBatteryStaple!")
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID, command.Sub2APIUserID = "workspace-launch-redacted", "acct-alpha", "usr-alpha", 41
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	operation.Stage, operation.Status = "runtime", "manual_review"
	attempt := operation.Attempts[operation.Stage]
	attempt.Attempted, attempt.Unknown, attempt.Status = 1, 1, "unknown"
	operation.Attempts[operation.Stage] = attempt
	operation.Observations[operation.Stage] = workspaceLaunchStageObservation{
		State: workspaceLaunchStageUnknown,
		Diagnostic: &clients.WorkspaceLaunchStageDiagnostic{
			SchemaVersion: 1, Owner: "fabric.tencent_tke", BlockReason: "official_profile_apply_failed",
			ErrorCode: "official_profile_apply_failed", ObservedAt: "2026-08-24T10:48:02Z",
			Checks: []clients.WorkspaceLaunchStageCheck{{
				Name: "official_profile_apply", OK: false,
				Details: map[string]any{"secret": "must-not-reach-customer", "pod": "raw-provider-identity"},
			}},
		},
	}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))

	response := requestWithSession(t, server, session, http.MethodGet, "/api/workspace-launches/"+operation.ID, "")
	var body map[string]any
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil {
		t.Fatalf("GET status=%d body=%s", response.Code, response.Body.String())
	}
	checks, ok := body["checks"].([]any)
	if !ok || len(checks) != 1 {
		t.Fatalf("GET checks=%#v", body["checks"])
	}
	check, ok := checks[0].(map[string]any)
	if !ok || len(check) != 2 || check["name"] != "official_profile_apply" || check["ok"] != false ||
		body["blockReason"] != "official_profile_apply_failed" || strings.Contains(response.Body.String(), "details") ||
		strings.Contains(response.Body.String(), "must-not-reach-customer") || strings.Contains(response.Body.String(), "raw-provider-identity") {
		t.Fatalf("GET leaked diagnostic details: %s", response.Body.String())
	}
}

func TestWorkspaceLaunchStageDiagnosticReadsWithoutPersisting(t *testing.T) {
	row := workspaceLaunchUnknownStageManualReviewRow(t, "runtime")
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{stageObservations: map[string]workspaceLaunchStageObservation{
		"runtime": {
			State: workspaceLaunchStagePending,
			Diagnostic: &clients.WorkspaceLaunchStageDiagnostic{
				SchemaVersion: 1, Owner: "fabric.tencent_tke", BlockReason: "runtime_deployment_not_ready", Retryable: true,
				ObservedAt: "2026-08-24T10:48:02Z", Checks: []clients.WorkspaceLaunchStageCheck{{
					Name: "deployment_ready", OK: false, Details: map[string]any{"waitingReason": "CrashLoopBackOff"},
				}},
			},
		},
	}}
	before := stringValue(row["result"])
	diagnostic, found, err := observeWorkspaceLaunchStage(context.Background(), store, adapter, operation.ID)
	after, _, _ := store.GetRuntimeOperation(context.Background(), operation.ID)
	if err != nil || !found || diagnostic.SchemaVersion != 2 || diagnostic.OperationIdentityDigest == "" ||
		diagnostic.OperationVersion != operation.Version || diagnostic.OperationStatus != "manual_review" ||
		diagnostic.Stage != "runtime" || diagnostic.State != workspaceLaunchStagePending || diagnostic.ErrorCode != "none" ||
		diagnostic.Owner != "fabric.tencent_tke" || diagnostic.BlockReason != "runtime_deployment_not_ready" || !diagnostic.Retryable ||
		diagnostic.ObservedAt != "2026-08-24T10:48:02Z" || len(diagnostic.Checks) != 1 || diagnostic.Checks[0].Name != "deployment_ready" ||
		!diagnostic.AuthoritativeRead || diagnostic.MutationBudget != 0 || diagnostic.Attempt.Attempted != 1 ||
		diagnostic.Attempt.Confirmed != 0 || diagnostic.Attempt.Unknown != 1 || adapter.reads != 1 || adapter.mutations != 0 ||
		stringValue(after["result"]) != before {
		t.Fatalf("diagnostic=%#v found=%v reads=%d mutations=%d err=%v", diagnostic, found, adapter.reads, adapter.mutations, err)
	}
}

func TestWorkspaceLaunchStageDiagnosticReturnsSafeFabricError(t *testing.T) {
	row := workspaceLaunchStorageAfterExhaustedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &workspaceLaunchUnitAdapter{readErrors: map[string]error{
		"storage": &clients.FabricHTTPError{StatusCode: http.StatusServiceUnavailable, Body: `{"error":"UnauthorizedOperation: secret material"}`},
	}}
	diagnostic, found, err := observeWorkspaceLaunchStage(context.Background(), &workspaceLaunchUnitStore{row: row}, adapter, operation.ID)
	if err != nil || !found || diagnostic.State != workspaceLaunchStageUnknown || diagnostic.ErrorCode != "UnauthorizedOperation" ||
		strings.Contains(diagnostic.ErrorCode, "secret") || adapter.reads != 1 || adapter.mutations != 0 {
		t.Fatalf("diagnostic=%#v found=%v reads=%d mutations=%d err=%v", diagnostic, found, adapter.reads, adapter.mutations, err)
	}
}

func TestWorkspaceLaunchStageDiagnosticRouteRequiresCapability(t *testing.T) {
	t.Setenv("OPL_INTERNAL_SERVICE_TOKEN", "stage-diagnostic-capability")
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	row := workspaceLaunchStorageAfterExhaustedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	fabric := &workspaceLaunchStorageResumeFabric{}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	path := "/api/operator/workspace-launches/" + operation.ID + "/stage-observation"
	operator := reservedOperatorSessionForTest(t, server)

	unauthorized := httptest.NewRequest(http.MethodGet, path, nil)
	addAuth(unauthorized, operator)
	unauthorizedResponse := httptest.NewRecorder()
	server.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized || fabric.reads != 0 {
		t.Fatalf("unauthorized status=%d reads=%d", unauthorizedResponse.Code, fabric.reads)
	}

	authorized := httptest.NewRequest(http.MethodGet, path, nil)
	addAuth(authorized, operator)
	authorized.Header.Set(productionAcceptanceBCapability, "stage-diagnostic-capability")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, authorized)
	if response.Code != http.StatusOK || fabric.reads != 1 || fabric.ensures != 0 || response.Header().Get("Cache-Control") != "no-store" ||
		strings.Contains(response.Body.String(), operation.ID) || !strings.Contains(response.Body.String(), `"state":"absent"`) ||
		!strings.Contains(response.Body.String(), `"mutationBudget":0`) {
		t.Fatalf("status=%d body=%s reads=%d ensures=%d", response.Code, response.Body.String(), fabric.reads, fabric.ensures)
	}
}

func TestWorkspaceLaunchStageReadErrorCodeFallsBackWithoutLeaking(t *testing.T) {
	err := &clients.FabricHTTPError{StatusCode: http.StatusServiceUnavailable, Body: `{"error":"invalid error contains spaces"}`}
	if got := workspaceLaunchStageReadErrorCode(err); got != "fabric_http_503" {
		t.Fatalf("code=%q", got)
	}
	if got := workspaceLaunchStageReadErrorCode(errors.New("secret provider detail")); got != "stage_read_failed" {
		t.Fatalf("fallback=%q", got)
	}
}
