package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
	operation, err := newWorkspaceLaunchFixtureOperation(command)
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

func TestWorkspaceLaunchStageReadErrorCodeFallsBackWithoutLeaking(t *testing.T) {
	err := &clients.FabricHTTPError{StatusCode: http.StatusServiceUnavailable, Body: `{"error":"invalid error contains spaces"}`}
	if got := workspaceLaunchStageReadErrorCode(err); got != "fabric_http_503" {
		t.Fatalf("code=%q", got)
	}
	if got := workspaceLaunchStageReadErrorCode(errors.New("secret provider detail")); got != "stage_read_failed" {
		t.Fatalf("fallback=%q", got)
	}
}
