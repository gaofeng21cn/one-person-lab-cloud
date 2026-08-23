package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func TestWorkspaceLaunchStageDiagnosticReadsWithoutPersisting(t *testing.T) {
	row := workspaceLaunchStorageAfterExhaustedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{stageObservations: map[string]workspaceLaunchStageObservation{
		"storage": {State: workspaceLaunchStageAbsent},
	}}
	before := stringValue(row["result"])
	diagnostic, found, err := observeWorkspaceLaunchStage(context.Background(), store, adapter, operation.ID)
	after, _, _ := store.GetRuntimeOperation(context.Background(), operation.ID)
	if err != nil || !found || diagnostic.SchemaVersion != 1 || diagnostic.OperationIdentityDigest == "" ||
		diagnostic.OperationVersion != operation.Version || diagnostic.OperationStatus != "manual_review" ||
		diagnostic.Stage != "storage" || diagnostic.State != workspaceLaunchStageAbsent || diagnostic.ErrorCode != "none" ||
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
