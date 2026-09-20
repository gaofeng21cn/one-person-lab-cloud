package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// workspaceApplicationInstallationSub2API is the delegated-credential surface a
// customer-owned installation start needs: the Workspace's own Gateway group
// inventory. Key creation and the Gateway Secret write stay unimplemented so the
// test asserts the request admission rather than the installation itself.
type workspaceApplicationInstallationSub2API struct {
	*gatewayKeyCommandClient
	groupsCalls  int
	groupsUserID int64
}

func (c *workspaceApplicationInstallationSub2API) UserGroups(_ context.Context, credential clients.SessionDelegatedCredential, userID int64) ([]clients.Sub2APIGroup, error) {
	c.groupsCalls, c.groupsUserID = c.groupsCalls+1, userID
	if err := c.rememberCredential(credential); err != nil {
		return nil, err
	}
	return []clients.Sub2APIGroup{{ID: 7, Name: "Codex", Platform: "openai", RateMultiplier: 1, Status: "active"}}, nil
}

func workspaceApplicationInstallationFixture(t *testing.T) (http.Handler, *httptest.ResponseRecorder, *memoryTableStore, *controlPlaneHTTPHandler) {
	t.Helper()
	t.Setenv("OPL_WORKSPACE_IMAGE", "repo.example/opl-workspace@sha256:"+strings.Repeat("d", 64))
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	store := newMemoryTableStore()
	sub2API := &workspaceApplicationInstallationSub2API{gatewayKeyCommandClient: &gatewayKeyCommandClient{
		customerFactsSub2API: &customerFactsSub2API{
			testSub2APIClient: &testSub2APIClient{balance: 100_000_000, charges: map[string]int64{}},
			history:           map[int64][]clients.Sub2APIBalanceHistoryEntry{},
		},
		keys: map[int64]clients.Sub2APIWorkspaceKey{},
	}}
	service := controlplane.NewService(&fakeLedgerClient{}, &applicationReplacementFabric{}, sub2API)
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	command := workspaceLaunchResourceOnlyUnitCommand()
	command.OperationID, command.AccountID, command.WorkspaceID = "workspace-launch-alpha", "acct-alpha", "ws-alpha"
	command.OwnerUserID, command.Sub2APIUserID = "usr-alpha", testSub2APIUserID("alpha@example.com")
	seedResourceOnlyActivatedWorkspaceFor(t, store, command)
	return server, loginForTest(t, server, "alpha@example.com", "CorrectHorseBatteryStaple!"), store, server.(*controlPlaneHTTPHandler)
}

func decodeWorkspaceApplicationInstallationProjection(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if response.Code != http.StatusAccepted {
		t.Fatalf("installation start status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["workspaceId"] != "ws-alpha" {
		t.Fatalf("installation projection identity = %#v", body)
	}
	installation, _ := body["applicationInstallation"].(map[string]any)
	if installation == nil {
		t.Fatalf("installation start produced no projection: %#v", body)
	}
	return installation
}

func isWorkspaceApplicationInstallationStatus(value any) bool {
	switch stringValue(value) {
	case "pending", "running", "manual_review":
		return true
	}
	return false
}

func TestWorkspaceApplicationInstallationStartsTheOneRequestForAResourceOnlyWorkspace(t *testing.T) {
	server, session, store, handler := workspaceApplicationInstallationFixture(t)
	installation := decodeWorkspaceApplicationInstallationProjection(t, requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspaces/ws-alpha/application-installation", "{}", "installation-alpha"))

	if installation["applicationId"] != "opl-app" || !isWorkspaceApplicationInstallationStatus(installation["status"]) {
		t.Fatalf("installation projection = %#v", installation)
	}
	rows, err := queryRuntimeOperations(context.Background(), store, runtimeOperationQuery{WorkspaceID: "ws-alpha", Action: workspaceDefaultApplicationAction})
	if err != nil || len(rows) != 1 {
		t.Fatalf("installation requests = %d err=%v", len(rows), err)
	}
	request, err := decodeWorkspaceDefaultApplication(rows[0])
	if err != nil {
		t.Fatal(err)
	}
	if request.LaunchOperationID != "workspace-launch-alpha" || request.AccountID != "acct-alpha" || request.OwnerUserID != "usr-alpha" ||
		request.Sub2APIUserID != testSub2APIUserID("alpha@example.com") || request.WorkspaceKeyGroupID != 7 {
		t.Fatalf("installation request identity = %+v", request)
	}
	if request.Revision.Image != "repo.example/opl-workspace@sha256:"+strings.Repeat("d", 64) || request.Revision.ApplicationID != "opl-app" {
		t.Fatalf("installation request revision = %+v", request.Revision)
	}
	if request.Phase == "deployed" || request.Phase == "" {
		t.Fatalf("installation request phase = %q", request.Phase)
	}
	// The admitted request is the Workspace's only one, so a repeat is a retry.
	repeated := decodeWorkspaceApplicationInstallationProjection(t, requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspaces/ws-alpha/application-installation", "{}", "installation-alpha-retry"))
	if repeated["operationId"] != installation["operationId"] {
		t.Fatalf("repeat produced a second installation: %#v", repeated)
	}
	rows, err = queryRuntimeOperations(context.Background(), store, runtimeOperationQuery{WorkspaceID: "ws-alpha", Action: workspaceDefaultApplicationAction})
	if err != nil || len(rows) != 1 {
		t.Fatalf("installation requests after repeat = %d err=%v", len(rows), err)
	}
	if handler.app.deployment.customerOwned() {
		t.Fatalf("fixture unexpectedly runs in customer_owned mode")
	}
}

func TestWorkspaceApplicationInstallationRefusesWorkspacesWithoutAResourceOnlyLaunch(t *testing.T) {
	server, session, store, _ := workspaceApplicationInstallationFixture(t)
	owner, _, _ := store.GetWorkspace(context.Background(), "ws-alpha")
	activated := cloneMap(owner)
	activated["id"] = "ws-unlaunched"
	mustStore(t, store.SaveWorkspace(context.Background(), activated))

	unlaunched := requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspaces/ws-unlaunched/application-installation", "{}", "installation-unlaunched")
	if unlaunched.Code != http.StatusConflict || !strings.Contains(unlaunched.Body.String(), "workspace_application_installation_unavailable") {
		t.Fatalf("unlaunched installation status=%d body=%s", unlaunched.Code, unlaunched.Body.String())
	}

	seedTenantMember(t, store, "acct-beta", "org-beta", "usr-beta", "beta@example.com")
	other := loginForTest(t, server, "beta@example.com", "CorrectHorseBatteryStaple!")
	foreign := requestWithMutationKeyForTest(t, server, other, http.MethodPost, "/api/workspaces/ws-alpha/application-installation", "{}", "installation-foreign")
	if foreign.Code != http.StatusNotFound || !strings.Contains(foreign.Body.String(), "workspace_not_found") {
		t.Fatalf("foreign installation status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	rows, err := queryRuntimeOperations(context.Background(), store, runtimeOperationQuery{WorkspaceID: "ws-alpha", Action: workspaceDefaultApplicationAction})
	if err != nil || len(rows) != 0 {
		t.Fatalf("refused starts left requests behind: %d err=%v", len(rows), err)
	}
}

func workspaceApplicationInstallationCandidate(t *testing.T, mutate func(*workspaceDefaultApplicationRequest)) map[string]any {
	t.Helper()
	request := workspaceDefaultApplicationRequest{
		SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID("workspace-launch-alpha"), LaunchOperationID: "workspace-launch-alpha",
		AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OwnerUserID: "usr-alpha", Sub2APIUserID: 41, WorkspaceKeyGroupID: 7,
		Revision: defaultOPLApplicationRevision("repo.example/opl-workspace@sha256:" + strings.Repeat("d", 64)), Phase: "credentials_required",
	}
	if mutate != nil {
		mutate(&request)
	}
	// The request ID is derived from its Launch, so a mutant that moves to
	// another Launch stays a well-formed request and is rejected for the Launch
	// it names rather than for its own shape.
	request.OperationID = workspaceDefaultApplicationOperationID(request.LaunchOperationID)
	row, err := workspaceDefaultApplicationRow(request)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

// The start is admitted only while the Workspace, its one resource-only Launch,
// and the request agree on identity, and only while nothing else owns the
// Workspace's application lifecycle.
func TestWorkspaceApplicationInstallationAdmissionBindsTheWorkspaceAndItsLaunch(t *testing.T) {
	_, _, store, _ := workspaceApplicationInstallationFixture(t)
	workspace, _, _ := store.GetWorkspace(context.Background(), "ws-alpha")
	launchRow, _, _ := store.GetRuntimeOperation(context.Background(), "workspace-launch-alpha")
	launch, err := decodeWorkspaceLaunchReconcileOperation(launchRow)
	if err != nil {
		t.Fatal(err)
	}
	if launch.provisioningMode() != contracts.WorkspaceProvisioningResourceOnly || launch.Status != contracts.StatusSucceeded {
		t.Fatalf("seeded launch = %q/%q", launch.provisioningMode(), launch.Status)
	}
	accepted := workspaceApplicationInstallationCandidate(t, nil)
	operations := []map[string]any{launchRow}
	if err := validateWorkspaceApplicationInstallationStart(workspace, accepted, operations); err != nil {
		t.Fatalf("admitted start rejected: %v", err)
	}

	for name, rejected := range map[string]map[string]any{
		"launch of another Workspace": workspaceApplicationInstallationCandidate(t, func(request *workspaceDefaultApplicationRequest) { request.LaunchOperationID = "workspace-launch-other" }),
		"request of another owner":    workspaceApplicationInstallationCandidate(t, func(request *workspaceDefaultApplicationRequest) { request.OwnerUserID = "usr-other" }),
		"request of another account":  workspaceApplicationInstallationCandidate(t, func(request *workspaceDefaultApplicationRequest) { request.AccountID = "acct-beta" }),
		"request of another user":     workspaceApplicationInstallationCandidate(t, func(request *workspaceDefaultApplicationRequest) { request.Sub2APIUserID = 42 }),
	} {
		if err := validateWorkspaceApplicationInstallationStart(workspace, rejected, operations); !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
			t.Fatalf("%s was admitted: %v", name, err)
		}
	}

	// A Workspace carries one installation request and no lifecycle that a start
	// may cut in front of.
	for name, extra := range map[string]map[string]any{
		"second installation request": map[string]any{"id": "workspace-launch-alpha:default-application", "action": workspaceDefaultApplicationAction, "workspaceId": "ws-alpha", "status": "pending"},
		"Workspace deletion":          map[string]any{"id": workspaceDeleteOperationID("ws-alpha"), "action": workspaceDeleteAction, "workspaceId": "ws-alpha", "status": "pending"},
		"Gateway key rotation":        map[string]any{"id": "workspace-key-rotation-alpha", "action": "workspace.gateway_key.rotate", "workspaceId": "ws-alpha", "status": "pending"},
	} {
		if err := validateWorkspaceApplicationInstallationStart(workspace, accepted, append([]map[string]any{launchRow}, extra)); !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
			t.Fatalf("%s did not block the start: %v", name, err)
		}
	}

	// A closed entitlement window and a Launch that already carries application
	// facts both refuse the start even when every identity matches.
	closed := cloneMap(workspace)
	closed["state"] = "suspended"
	if err := validateWorkspaceApplicationInstallationStart(closed, accepted, operations); !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
		t.Fatalf("suspended Workspace admitted the start: %v", err)
	}
	fullLaunch := cloneMap(launchRow)
	fullResult := map[string]any{}
	if err := json.Unmarshal([]byte(stringValue(fullLaunch["result"])), &fullResult); err != nil {
		t.Fatal(err)
	}
	fullResult["workspaceKeyGroupId"] = json.Number("7")
	encoded, err := json.Marshal(fullResult)
	if err != nil {
		t.Fatal(err)
	}
	fullLaunch["result"] = string(encoded)
	if err := validateWorkspaceApplicationInstallationStart(workspace, accepted, []map[string]any{fullLaunch}); !errors.Is(err, errWorkspaceApplicationOperationCASConflict) {
		t.Fatalf("Launch with application facts admitted the start: %v", err)
	}
}
