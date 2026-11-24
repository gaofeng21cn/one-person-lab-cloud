package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type workspaceRenewalAPIFixture struct {
	server    http.Handler
	app       *controlPlaneServer
	owner     *httptest.ResponseRecorder
	workspace map[string]any
}

type workspaceRenewalQueryStore struct {
	*memoryTableStore
	queries []runtimeOperationQuery
	gets    []string
}

func (s *workspaceRenewalQueryStore) PageRuntimeOperations(ctx context.Context, query runtimeOperationQuery) (tablePage, error) {
	s.queries = append(s.queries, query)
	return s.memoryTableStore.PageRuntimeOperations(ctx, query)
}

func (s *workspaceRenewalQueryStore) GetRuntimeOperation(ctx context.Context, id string) (map[string]any, bool, error) {
	s.gets = append(s.gets, id)
	return s.memoryTableStore.GetRuntimeOperation(ctx, id)
}

func newWorkspaceRenewalAPIFixture(t *testing.T) workspaceRenewalAPIFixture {
	return newWorkspaceRenewalAPIFixtureWithStore(t, newMemoryTableStore())
}

func currentWorkspaceRenewalAPIRow() map[string]any {
	workspace := canonicalWorkspaceRenewalRow(false)
	periodStart := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	paidThrough := nextBillingMonth(periodStart, periodStart.Day())
	workspace["periodStart"], workspace["paidThrough"] = periodStart.Format(time.RFC3339Nano), paidThrough.Format(time.RFC3339Nano)
	workspace["nextRenewalAt"], workspace["billingAnchorDay"] = paidThrough.Add(-monthlyRenewalLead).Format(time.RFC3339Nano), int64(periodStart.Day())
	return workspace
}

func newWorkspaceRenewalAPIFixtureWithStore(t *testing.T, store StateStore) workspaceRenewalAPIFixture {
	t.Helper()
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, &fakeFabricClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	handler := server.(*controlPlaneHTTPHandler)
	owner := tenantOwnerSessionForTest(t, server)
	ownerID := sessionUserIDForTest(t, server, owner)
	workspace := currentWorkspaceRenewalAPIRow()
	workspace["id"], workspace["accountId"], workspace["ownerAccountId"], workspace["ownerUserId"] = "workspace-renewal-api", "acct-alpha", "acct-alpha", ownerID
	workspace["state"], workspace["status"] = "running", "running"
	mustStore(t, handler.app.tables.SaveWorkspace(context.Background(), workspace))
	return workspaceRenewalAPIFixture{server: server, app: handler.app, owner: owner, workspace: workspace}
}

func (f workspaceRenewalAPIFixture) request(t *testing.T, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	return requestWithMutationKeyForTest(t, f.server, f.owner, http.MethodPost, "/api/workspaces/"+stringValue(f.workspace["id"])+"/auto-renew", body, key)
}

func decodeWorkspaceAutoRenewResponse(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("auto-renew status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	wantKeys := []string{"autoRenew", "effectiveAfter", "nextRenewalAt", "paidThrough", "renewalStatus"}
	if len(body) != len(wantKeys) {
		t.Fatalf("auto-renew response=%#v, want exactly %v", body, wantKeys)
	}
	for _, key := range wantKeys {
		if _, ok := body[key]; !ok {
			t.Fatalf("auto-renew response missing %s: %#v", key, body)
		}
	}
	return body
}

func attachWorkspaceRenewalIntentAuditForTest(intent *workspaceRenewalIntentCAS, current map[string]any) {
	before := workspaceRenewalIntentState(current["autoRenew"] == true, stringValue(current["authorizedBy"]), stringValue(current["authorizedAt"]))
	after := workspaceRenewalIntentState(intent.WorkspacePatch.AutoRenew, intent.WorkspacePatch.AuthorizedBy, intent.WorkspacePatch.AuthorizedAt)
	event := map[string]any{
		"actorUserId": intent.OwnerUserID, "actorRole": "owner", "actorAccountId": intent.AccountID, "targetAccountId": intent.AccountID,
		"action": "workspace.auto_renew", "resourceKind": "workspace", "resourceId": intent.WorkspaceID,
		"ipAddress": "192.0.2.1", "userAgent": "workspace-renewal-test", "before": before, "after": after, "result": "succeeded",
	}
	intent.AuditEvent = bindWorkspaceAutoRenewAudit(intent.CommandOperation, event)
}

func enableWorkspaceAutoRenewForTest(t *testing.T, fixture workspaceRenewalAPIFixture) map[string]any {
	t.Helper()
	workspace, ok := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if !ok {
		t.Fatal("workspace missing")
	}
	operations, err := fixture.app.tables.ListRuntimeOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ownerID := sessionUserIDForTest(t, fixture.server, fixture.owner)
	update, _, err := planWorkspaceRenewalIntent(workspace, map[string]any{"id": ownerID, "role": "owner"}, operations, true, "internal-test-enable", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	attachWorkspaceRenewalIntentAuditForTest(&update, workspace)
	mustStore(t, fixture.app.tables.ApplyWorkspaceRenewalIntent(context.Background(), update))
	workspace, _ = fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	return workspace
}

func TestWorkspaceAutoRenewEnablePersistsAuthorizationAndReplaysOriginalCommand(t *testing.T) {
	fixture := newWorkspaceRenewalAPIFixture(t)
	workspaceID := stringValue(fixture.workspace["id"])
	key := "workspace-renewal-enable"
	first := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":true}`, key))
	stored, _ := fixture.app.getWorkspace(workspaceID)
	ownerID := sessionUserIDForTest(t, fixture.server, fixture.owner)
	if first["autoRenew"] != true || stored["autoRenew"] != true || stored["authorizedBy"] != ownerID || stringValue(stored["authorizedAt"]) == "" {
		t.Fatalf("enable response=%#v stored=%#v", first, stored)
	}
	if _, err := time.Parse(time.RFC3339, stringValue(stored["authorizedAt"])); err != nil {
		t.Fatalf("authorizedAt=%q: %v", stored["authorizedAt"], err)
	}
	operations, err := fixture.app.tables.ListRuntimeOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	audits, err := fixture.app.tables.ListAuditEvents(context.Background(), stringValue(fixture.workspace["accountId"]))
	if err != nil {
		t.Fatal(err)
	}
	commandID := workspaceAutoRenewCommandID(workspaceID, key)
	if len(operations) != 1 || recordByID(operations, commandID) == nil || len(audits) != 1 || recordByID(audits, workspaceAutoRenewAuditID(commandID)) == nil {
		t.Fatalf("enable history operations=%#v audits=%#v", operations, audits)
	}

	replay := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":true}`, key))
	noOp := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":true}`, "workspace-renewal-enable-noop"))
	operationsAfter, _ := fixture.app.tables.ListRuntimeOperations(context.Background())
	auditsAfter, _ := fixture.app.tables.ListAuditEvents(context.Background(), stringValue(fixture.workspace["accountId"]))
	if string(mustJSON(replay)) != string(mustJSON(first)) || string(mustJSON(noOp)) != string(mustJSON(first)) || len(operationsAfter) != 1 || len(auditsAfter) != 1 {
		t.Fatalf("enable replay/no-op changed result or history: first=%#v replay=%#v noOp=%#v operations=%d audits=%d", first, replay, noOp, len(operationsAfter), len(auditsAfter))
	}
	conflict := fixture.request(t, `{"autoRenew":false}`, key)
	if conflict.Code != http.StatusConflict || !strings.Contains(conflict.Body.String(), errIdempotencyConflict.Error()) {
		t.Fatalf("conflicting replay status=%d body=%s", conflict.Code, conflict.Body.String())
	}
}

func TestRetainedWorkspaceLaunchReplaysHistoricalAutoRenewIntent(t *testing.T) {
	t.Setenv(controlledBasicPilotEnabledEnv, "1")
	t.Setenv(controlledBasicPilotAccountsEnv, "acct-alpha")
	t.Setenv("OPL_TENCENT_ZONE", "ap-guangzhou-1")
	t.Setenv("OPL_WORKSPACE_LAUNCH_WORKER_ENABLED", "0")
	server, store, _, _, _ := newWorkspaceLaunchMonthlyPreflightFixture(t, "")
	session := loginForTest(t, server, "alpha@example.com", "CorrectHorseBatteryStaple!")
	key := "workspace-launch-auto-renew"
	seedRetainedWorkspaceLaunchForTest(t, server, "acct-alpha", "usr-alpha", 41, "Auto Renew", "basic", key, true)
	response := requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspace-launches", `{"name":"Auto Renew","packageId":"basic","autoRenew":true,"provisioningMode":"resource_only"}`, key)
	if response.Code != http.StatusAccepted {
		t.Fatalf("auto-renew replay status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil || body["autoRenew"] != true {
		t.Fatalf("auto-renew replay response=%#v err=%v", body, err)
	}
	row, found, err := store.GetRuntimeOperation(context.Background(), workspaceLaunchOperationID("acct-alpha", key))
	if err != nil || !found {
		t.Fatalf("launch operation found=%v err=%v", found, err)
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil || !operation.boolFact("autoRenew") {
		t.Fatalf("durable launch intent operation=%#v err=%v", operation, err)
	}
}

func TestRetainedWorkspaceLaunchFencePrecedesAutoRenewAdmission(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_LAUNCH_WORKER_ENABLED", "0")
	server, store, client, _, events := newWorkspaceLaunchMonthlyPreflightFixture(t, "")
	handler, ok := server.(*controlPlaneHTTPHandler)
	if !ok {
		t.Fatal("unexpected control-plane test handler")
	}
	handler.app.deployment = deploymentProfile{Mode: deploymentCustomerOwned, FabricProvider: fabricLocalDocker}
	session := loginForTest(t, server, "alpha@example.com", "CorrectHorseBatteryStaple!")
	response := requestWithMutationKeyForTest(t, server, session, http.MethodPost, "/api/workspace-launches", `{"name":"No Billing Renewal","packageId":"basic","autoRenew":true}`, "workspace-launch-non-billing-auto-renew")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), retainedWorkspaceLaunchNewPurchaseRetired) {
		t.Fatalf("retained non-billing auto-renew status=%d body=%s", response.Code, response.Body.String())
	}
	operations, err := store.ListRuntimeOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(operations) != 0 || len(client.charges) != 0 || len(*events) != 0 {
		t.Fatalf("non-billing auto-renew crossed mutation boundary: operations=%#v charges=%#v events=%#v", operations, client.charges, *events)
	}
}

func TestWorkspaceAutoRenewRejectsEnableForNonBillingWorkspaceWithoutMutation(t *testing.T) {
	fixture := newWorkspaceRenewalAPIFixture(t)
	workspace := cloneMap(fixture.workspace)
	workspace["id"] = "workspace-renewal-non-billing"
	workspace["resourceBillingEnabled"], workspace["renewalStatus"] = false, workspaceBillingNotApplicable
	workspace["autoRenew"], workspace["authorizedBy"], workspace["authorizedAt"] = false, "", ""
	workspace["computeUsdMicros"], workspace["storageUsdMicros"], workspace["totalUsdMicros"] = int64(0), int64(0), int64(0)
	workspace["computeAllocationId"], workspace["currentComputeAllocationId"] = "compute-non-billing", "compute-non-billing"
	workspace["storageId"] = "storage-non-billing"
	mustStore(t, fixture.app.tables.SaveWorkspace(context.Background(), workspace))

	response := requestWithMutationKeyForTest(t, fixture.server, fixture.owner, http.MethodPost, "/api/workspaces/workspace-renewal-non-billing/auto-renew", `{"autoRenew":true}`, "workspace-non-billing-enable")
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "autoRenew_unavailable") {
		t.Fatalf("non-billing enable status=%d body=%s", response.Code, response.Body.String())
	}
	stored, _ := fixture.app.getWorkspace("workspace-renewal-non-billing")
	operations, _ := fixture.app.tables.ListRuntimeOperations(context.Background())
	audits, _ := fixture.app.tables.ListAuditEvents(context.Background(), stringValue(workspace["accountId"]))
	if stored["autoRenew"] != false || len(operations) != 0 || len(audits) != 0 {
		t.Fatalf("non-billing enable mutated state: workspace=%#v operations=%#v audits=%#v", stored, operations, audits)
	}
}

func TestWorkspaceAutoRenewIntentIsIndependentPerWorkspace(t *testing.T) {
	fixture := newWorkspaceRenewalAPIFixture(t)
	ownerID := sessionUserIDForTest(t, fixture.server, fixture.owner)
	workspaceB := cloneMap(fixture.workspace)
	workspaceB["id"], workspaceB["autoRenew"] = "workspace-renewal-b", true
	workspaceB["authorizedBy"], workspaceB["authorizedAt"] = ownerID, time.Now().UTC().Add(-time.Hour).Format(time.RFC3339Nano)
	mustStore(t, fixture.app.tables.SaveWorkspace(context.Background(), workspaceB))

	enableA := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":true}`, "workspace-a-enable"))
	disableBResponse := requestWithMutationKeyForTest(t, fixture.server, fixture.owner, http.MethodPost, "/api/workspaces/workspace-renewal-b/auto-renew", `{"autoRenew":false}`, "workspace-b-disable")
	disableB := decodeWorkspaceAutoRenewResponse(t, disableBResponse)
	storedA, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	storedB, _ := fixture.app.getWorkspace("workspace-renewal-b")
	if enableA["autoRenew"] != true || disableB["autoRenew"] != false || storedA["autoRenew"] != true || storedB["autoRenew"] != false || storedA["paidThrough"] != storedB["paidThrough"] {
		t.Fatalf("independent intent A=%#v B=%#v storedA=%#v storedB=%#v", enableA, disableB, storedA, storedB)
	}
	operations, err := fixture.app.tables.ListRuntimeOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for workspaceID, key := range map[string]string{stringValue(fixture.workspace["id"]): "workspace-a-enable", "workspace-renewal-b": "workspace-b-disable"} {
		if recordByID(operations, workspaceAutoRenewCommandID(workspaceID, key)) == nil {
			t.Fatalf("missing Workspace-scoped command for %s: %#v", workspaceID, operations)
		}
	}
}

func TestWorkspaceAutoRenewAllowsDisable(t *testing.T) {
	fixture := newWorkspaceRenewalAPIFixture(t)
	body := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":false}`, "workspace-renewal-disable"))
	stored, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
	if body["autoRenew"] != false || stored["autoRenew"] != false {
		t.Fatalf("disable response=%#v stored=%#v", body, stored)
	}
}

func TestWorkspaceAutoRenewRepeatedDisableIsStableNoOp(t *testing.T) {
	store := &workspaceRenewalQueryStore{memoryTableStore: newMemoryTableStore()}
	fixture := newWorkspaceRenewalAPIFixtureWithStore(t, store)
	enableWorkspaceAutoRenewForTest(t, fixture)
	ctx := context.Background()

	first := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":false}`, "workspace-renewal-disable"))
	operationsBefore, err := store.ListRuntimeOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	auditsBefore, err := store.ListAuditEvents(ctx, stringValue(fixture.workspace["accountId"]))
	if err != nil {
		t.Fatal(err)
	}

	second := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":false}`, "workspace-renewal-noop-1"))
	third := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":false}`, "workspace-renewal-noop-2"))
	if string(mustJSON(first)) != string(mustJSON(second)) || string(mustJSON(first)) != string(mustJSON(third)) {
		t.Fatalf("repeated disable response changed: first=%#v second=%#v third=%#v", first, second, third)
	}
	operationsAfter, err := store.ListRuntimeOperations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	auditsAfter, err := store.ListAuditEvents(ctx, stringValue(fixture.workspace["accountId"]))
	if err != nil {
		t.Fatal(err)
	}
	if len(operationsAfter) != len(operationsBefore) || len(auditsAfter) != len(auditsBefore) {
		t.Fatalf("no-op disable appended history: operations=%d/%d audits=%d/%d", len(operationsBefore), len(operationsAfter), len(auditsBefore), len(auditsAfter))
	}
	if len(store.queries) != 1 {
		t.Fatalf("no-op disable paged operation history: %#v", store.queries)
	}
	paidThrough, err := time.Parse(time.RFC3339, stringValue(fixture.workspace["paidThrough"]))
	if err != nil {
		t.Fatal(err)
	}
	wantRenewalID := workspaceRenewalOperationID(stringValue(fixture.workspace["id"]), paidThrough)
	if len(store.gets) != 5 {
		t.Fatalf("operation reads=%#v, want command plus exact renewal lookups", store.gets)
	}
	for _, got := range []string{store.gets[2], store.gets[4]} {
		if got != wantRenewalID {
			t.Fatalf("no-op disable read operation %q, want exact current renewal %q", got, wantRenewalID)
		}
	}
}

func TestCloudAdminCanManageOwnWorkspaceRenewal(t *testing.T) {
	server, err := NewPersistentServer(newTestService(fakeLedgerClient{}, &fakeFabricClient{}), newMemoryTableStore())
	if err != nil {
		t.Fatal(err)
	}
	handler := server.(*controlPlaneHTTPHandler)
	workspace := currentWorkspaceRenewalAPIRow()
	workspace["id"], workspace["accountId"], workspace["ownerAccountId"], workspace["ownerUserId"] = "workspace-admin", "acct-admin", "acct-admin", "usr-admin"
	workspace["state"], workspace["status"] = "running", "running"
	mustStore(t, handler.app.tables.SaveWorkspace(context.Background(), workspace))
	response := requestWithMutationKeyForTest(t, server, reservedOperatorSessionForTest(t, server), http.MethodPost, "/api/workspaces/workspace-admin/auto-renew", `{"autoRenew":false}`, "workspace-admin-renewal")
	if response.Code != http.StatusOK {
		t.Fatalf("admin auto-renew status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWorkspaceAutoRenewAuditIsBoundToOriginalCommandAndRequest(t *testing.T) {
	for _, storeCase := range []struct {
		name string
		new  func(*testing.T) StateStore
	}{
		{name: "memory", new: func(*testing.T) StateStore { return newMemoryTableStore() }},
		{name: "postgres", new: func(t *testing.T) StateStore { return newPostgresWorkspaceRenewalStore(t).(StateStore) }},
	} {
		t.Run(storeCase.name, func(t *testing.T) {
			fixture := newWorkspaceRenewalAPIFixtureWithStore(t, storeCase.new(t))
			seeded := enableWorkspaceAutoRenewForTest(t, fixture)
			workspaceID := stringValue(fixture.workspace["id"])
			path := "/api/workspaces/" + workspaceID + "/auto-renew"
			request := func(remoteAddr, userAgent string) *httptest.ResponseRecorder {
				req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"autoRenew":false}`))
				req.RemoteAddr = remoteAddr
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Idempotency-Key", "stable-request-audit")
				req.Header.Set("User-Agent", userAgent)
				addAuth(req, fixture.owner)
				rec := httptest.NewRecorder()
				fixture.server.ServeHTTP(rec, req)
				return rec
			}
			decodeWorkspaceAutoRenewResponse(t, request("198.51.100.23:4321", "opl-original-client/1.0"))

			commandID := workspaceAutoRenewCommandID(workspaceID, "stable-request-audit")
			operations, err := fixture.app.tables.ListRuntimeOperations(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			command := recordByID(operations, commandID)
			if command == nil {
				t.Fatalf("auto-renew command %s missing: %#v", commandID, operations)
			}
			audits, err := fixture.app.tables.ListAuditEvents(context.Background(), stringValue(fixture.workspace["accountId"]))
			if err != nil {
				t.Fatal(err)
			}
			auditID := workspaceAutoRenewAuditID(commandID)
			audit := recordByID(audits, auditID)
			if audit == nil {
				t.Fatalf("deterministic audit %s missing: %#v", auditID, audits)
			}
			ownerID := sessionUserIDForTest(t, fixture.server, fixture.owner)
			if audit["createdAt"] != command["createdAt"] || audit["actorUserId"] != ownerID || audit["actorRole"] != "owner" ||
				audit["actorAccountId"] != fixture.workspace["accountId"] || audit["ipAddress"] != "198.51.100.23" || audit["userAgent"] != "opl-original-client/1.0" {
				t.Fatalf("audit request identity=%#v command=%#v", audit, command)
			}
			wantBefore := map[string]any{"autoRenew": true, "authorizedBy": ownerID, "authorizedAt": seeded["authorizedAt"]}
			wantAfter := map[string]any{"autoRenew": false, "authorizedBy": "", "authorizedAt": ""}
			if string(mustJSON(mapField(audit, "before"))) != string(mustJSON(wantBefore)) || string(mustJSON(mapField(audit, "after"))) != string(mustJSON(wantAfter)) {
				t.Fatalf("audit intent snapshots before=%#v after=%#v", audit["before"], audit["after"])
			}

			originalAudit := string(mustJSON(audit))
			decodeWorkspaceAutoRenewResponse(t, request("203.0.113.45:9876", "opl-replay-client/2.0"))
			audits, err = fixture.app.tables.ListAuditEvents(context.Background(), stringValue(fixture.workspace["accountId"]))
			if err != nil {
				t.Fatal(err)
			}
			if got := recordByID(audits, auditID); got == nil || string(mustJSON(got)) != originalAudit {
				t.Fatalf("replay changed original audit: before=%s after=%#v", originalAudit, got)
			}
		})
	}
}

func TestWorkspaceAutoRenewRequiresExplicitBooleanOwnerCSRFAndMutationKey(t *testing.T) {
	fixture := newWorkspaceRenewalAPIFixture(t)
	path := "/api/workspaces/" + stringValue(fixture.workspace["id"]) + "/auto-renew"
	for _, body := range []string{`{}`, `{"autoRenew":null}`, `{"autoRenew":1}`, `{"autoRenew":"true"}`} {
		response := fixture.request(t, body, "workspace-renewal-invalid")
		if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "autoRenew_required") {
			t.Fatalf("body=%s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
	missingKey := fixture.request(t, `{"autoRenew":true}`, "")
	if missingKey.Code != http.StatusBadRequest || !strings.Contains(missingKey.Body.String(), "missing Idempotency-Key") {
		t.Fatalf("missing key status=%d body=%s", missingKey.Code, missingKey.Body.String())
	}

	withoutCSRF := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(`{"autoRenew":true}`))
	withoutCSRF.Header.Set("Content-Type", "application/json")
	withoutCSRF.Header.Set("Idempotency-Key", "workspace-renewal-no-csrf")
	addSessionCookies(withoutCSRF, fixture.owner)
	withoutCSRFResponse := httptest.NewRecorder()
	fixture.server.ServeHTTP(withoutCSRFResponse, withoutCSRF)
	if withoutCSRFResponse.Code != http.StatusForbidden || !strings.Contains(withoutCSRFResponse.Body.String(), "csrf_token_invalid") {
		t.Fatalf("missing CSRF status=%d body=%s", withoutCSRFResponse.Code, withoutCSRFResponse.Body.String())
	}

}

func TestWorkspaceAutoRenewDeniesCrossTenantWorkspace(t *testing.T) {
	fixture := newWorkspaceRenewalAPIFixture(t)
	other := cloneMap(fixture.workspace)
	other["id"], other["accountId"] = "workspace-other-tenant", "acct-other"
	mustStore(t, fixture.app.tables.SaveWorkspace(context.Background(), other))
	response := requestWithMutationKeyForTest(t, fixture.server, fixture.owner, http.MethodPost, "/api/workspaces/workspace-other-tenant/auto-renew", `{"autoRenew":false}`, "workspace-renewal-cross-tenant")
	if response.Code != http.StatusForbidden || !strings.Contains(response.Body.String(), "account_scope_forbidden") {
		t.Fatalf("cross-tenant status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestWorkspaceAutoRenewDisableBeforeAndAfterClaim(t *testing.T) {
	t.Run("before claim cancels current renewal", func(t *testing.T) {
		fixture := newWorkspaceRenewalAPIFixture(t)
		enableWorkspaceAutoRenewForTest(t, fixture)
		body := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":false}`, "workspace-renewal-disable-before-claim"))
		if body["autoRenew"] != false || body["renewalStatus"] != "cancelled" || body["effectiveAfter"] != body["paidThrough"] {
			t.Fatalf("before-claim disable=%#v", body)
		}
	})

	t.Run("after claim applies to following period", func(t *testing.T) {
		fixture := newWorkspaceRenewalAPIFixture(t)
		enableWorkspaceAutoRenewForTest(t, fixture)
		workspace, _ := fixture.app.getWorkspace(stringValue(fixture.workspace["id"]))
		paidThrough, err := time.Parse(time.RFC3339, stringValue(workspace["paidThrough"]))
		if err != nil {
			t.Fatal(err)
		}
		operationID := "workspace-renewal-" + stableID(stringValue(workspace["id"]), paidThrough.Format(time.RFC3339))[:18]
		mustStore(t, fixture.app.tables.SaveRuntimeOperation(context.Background(), map[string]any{
			"id": operationID, "operationId": operationID, "accountId": workspace["accountId"], "workspaceId": workspace["id"],
			"resourceId": workspace["id"], "resourceKind": "workspace_renewal", "action": "workspace.renewal", "status": "claimed",
			"result": `{"requestHash":"test","phase":"claimed","paidThrough":"` + paidThrough.Format(time.RFC3339Nano) + `"}`,
		}))
		body := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":false}`, "workspace-renewal-disable-after-claim"))
		anchor := int(numberField(workspace, "billingAnchorDay", float64(paidThrough.Day())))
		wantEffective := nextBillingMonth(paidThrough, anchor).UTC().Format(time.RFC3339Nano)
		if body["autoRenew"] != false || body["renewalStatus"] != "claimed" || body["effectiveAfter"] != wantEffective {
			t.Fatalf("after-claim disable=%#v want effectiveAfter=%s", body, wantEffective)
		}
		operationsBefore, _ := fixture.app.tables.ListRuntimeOperations(context.Background())
		auditsBefore, _ := fixture.app.tables.ListAuditEvents(context.Background(), stringValue(fixture.workspace["accountId"]))
		repeated := decodeWorkspaceAutoRenewResponse(t, fixture.request(t, `{"autoRenew":false}`, "workspace-renewal-disable-after-claim-fresh"))
		operationsAfter, _ := fixture.app.tables.ListRuntimeOperations(context.Background())
		auditsAfter, _ := fixture.app.tables.ListAuditEvents(context.Background(), stringValue(fixture.workspace["accountId"]))
		if string(mustJSON(repeated)) != string(mustJSON(body)) || len(operationsAfter) != len(operationsBefore) || len(auditsAfter) != len(auditsBefore) {
			t.Fatalf("after-claim no-op response=%#v want=%#v operations=%d/%d audits=%d/%d", repeated, body, len(operationsBefore), len(operationsAfter), len(auditsBefore), len(auditsAfter))
		}
	})
}

type workspaceRenewalWorkerFixture struct {
	app            *controlPlaneServer
	service        *controlplane.Service
	sub2API        *monthlySub2API
	fabric         *monthlyFabric
	ledger         *monthlyLedger
	events         *[]string
	workspace      map[string]any
	compute        map[string]any
	storage        map[string]any
	paidThrough    time.Time
	renewedThrough time.Time
}

func newWorkspaceRenewalWorkerFixture(t *testing.T, balances []int64) workspaceRenewalWorkerFixture {
	t.Helper()
	return newWorkspaceRenewalWorkerFixtureAt(t, balances, time.Date(2026, 8, 31, 9, 30, 0, 0, time.UTC))
}

func newWorkspaceRenewalWorkerFixtureAt(t *testing.T, balances []int64, paidThrough time.Time) workspaceRenewalWorkerFixture {
	t.Helper()
	app, service, sub2API, fabric, ledger, events := newMonthlyBillingTest(t, balances)
	renewedThrough := nextBillingMonth(paidThrough, paidThrough.Day())
	ownerID, workspaceID := "usr-monthly-owner", "workspace-monthly"
	computeID, storageID := "compute-workspace-monthly", "storage-workspace-monthly"
	compute := monthlyActiveResource("compute", computeID, paidThrough)
	storage := monthlyActiveResource("storage", storageID, paidThrough)
	for _, row := range []map[string]any{compute, storage} {
		row["workspaceId"], row["ownerUserId"], row["autoRenew"] = workspaceID, ownerID, false
		if !monthlyPriceSnapshotAvailable(row) {
			t.Fatalf("test child price snapshot invalid: %#v", row)
		}
	}
	storage["computeAllocationId"] = computeID
	mustStore(t, app.tables.SaveCompute(context.Background(), compute))
	mustStore(t, app.tables.SaveStorage(context.Background(), storage))
	billing, code := workspaceBillingStateFromChildren(compute, storage, workspaceBillingChildIdentity{
		AccountID: "acct-monthly", OwnerUserID: ownerID, WorkspaceID: workspaceID, PackageID: "basic",
		ComputeID: computeID, StorageID: storageID, StorageGB: 10,
	})
	if code != "" {
		t.Fatalf("test Workspace billing state: %s", code)
	}
	billing["autoRenew"], billing["authorizedBy"], billing["authorizedAt"] = true, ownerID, paidThrough.AddDate(0, -1, 0).Format(time.RFC3339Nano)
	workspace := mergeMaps(map[string]any{
		"id": workspaceID, "accountId": "acct-monthly", "ownerAccountId": "acct-monthly", "ownerUserId": ownerID,
		"state": "running", "status": "running", "currentComputeAllocationId": computeID, "storageId": storageID,
		"currentAttachmentId": "attachment-workspace-monthly", "attachmentId": "attachment-workspace-monthly",
		"workspaceApiKeyId": int64(9),
	}, billing)
	mustStore(t, app.tables.SaveWorkspace(context.Background(), workspace))
	fabric.computeRenew = clients.ComputeAllocation{
		ID: computeID, AccountID: "acct-monthly", WorkspaceID: workspaceID, PackageID: "basic", Status: "running",
		Provider: "fabric", ProviderResourceID: stringValue(compute["providerResourceId"]), ProviderRequestID: "renew-compute-workspace",
		Zone: "provider-zone", Deadline: renewedThrough.Format(time.RFC3339),
	}
	fabric.storageRenew = clients.StorageVolume{
		ID: storageID, AccountID: "acct-monthly", WorkspaceID: workspaceID, Status: "available", Provider: "fabric",
		ProviderResourceID: stringValue(storage["providerResourceId"]), ProviderRequestID: "renew-storage-workspace", SizeGB: 10,
		Deadline: renewedThrough.Format(time.RFC3339), Zone: "provider-zone",
	}
	fabric.computeSync, fabric.storageSync = fabric.computeRenew, fabric.storageRenew
	return workspaceRenewalWorkerFixture{
		app: app, service: service, sub2API: sub2API, fabric: fabric, ledger: ledger, events: events,
		workspace: workspace, compute: compute, storage: storage, paidThrough: paidThrough, renewedThrough: renewedThrough,
	}
}

func (f workspaceRenewalWorkerFixture) operation(t *testing.T) map[string]any {
	t.Helper()
	operations, err := f.app.tables.ListRuntimeOperations(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range operations {
		if stringValue(operation["action"]) == "workspace.renewal" && stringValue(operation["workspaceId"]) == stringValue(f.workspace["id"]) {
			return operation
		}
	}
	t.Fatal("Workspace renewal operation not found")
	return nil
}

// seedRenewalLaunchRuntime persists a succeeded Launch with its Runtime
// stages for the renewal fixture's Workspace. The renewal execution engine
// that used to consume it is withdrawn; the retained recovery readers still
// need the Runtime identity it proves.
func seedRenewalLaunchRuntime(t *testing.T, store controlPlaneTableStore, workspace map[string]any, fabric *monthlyFabric) {
	t.Helper()
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID = "workspace-launch-monthly", stringValue(workspace["accountId"]), stringValue(workspace["ownerUserId"])
	command.WorkspaceID = stringValue(workspace["id"])
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		operation.Stage = stage
		facts := workspaceLaunchReadyFacts(stage)
		for key, value := range map[string]any{"computeAllocationId": workspace["computeAllocationId"], "storageId": workspace["storageId"], "attachmentId": workspace["currentAttachmentId"], "runtimeId": "runtime-workspace-monthly", "runtimeServiceName": "opl-runtime-monthly"} {
			if _, ok := facts[key]; ok {
				facts[key] = value
			}
		}
		if stage == contracts.StageRuntime {
			facts["runtimeBindingRef"] = operation.ID + ":runtime"
		}
		if stage == contracts.StageActivation {
			facts["activationOperationId"] = operation.ID + ":activation"
		}
		if stage == contracts.StageReceipt {
			facts["receiptOperationId"] = operation.ID + ":purchase-receipt"
		}
		observation, err := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: facts})
		if err != nil {
			t.Fatalf("seed renewal Runtime stage %s: %v", stage, err)
		}
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Status = 1, 1, "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
		operation.Attempts[stage], operation.Observations[stage] = attempt, observation
	}
	operation.Stage, operation.Status = contracts.StageSucceeded, contracts.StatusSucceeded
	operation.Version = len(workspaceLaunchReconcileStages)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	workspace["runtimeId"] = "runtime-workspace-monthly"
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))
	fabric.runtimePowerIdentity = contracts.WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: command.AccountID, WorkspaceID: command.WorkspaceID, RuntimeID: "runtime-workspace-monthly", RuntimeOperationID: operation.stringFact("runtimeBindingRef")}
	fabric.runtimePowerState = "running"
}

func newWorkspaceRenewalRuntimeFixture(t *testing.T, balances []int64) workspaceRenewalWorkerFixture {
	t.Helper()
	fixture := newWorkspaceRenewalWorkerFixture(t, balances)
	seedRenewalLaunchRuntime(t, fixture.app.tables, fixture.workspace, fixture.fabric)
	return fixture
}
