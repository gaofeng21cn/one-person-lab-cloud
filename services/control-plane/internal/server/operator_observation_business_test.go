package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func operatorSummaryFixture(t *testing.T, count int) (*memoryTableStore, *operatorProjectionSub2API) {
	t.Helper()
	store := newMemoryTableStore()
	client := newOperatorProjectionClient(operatorProjectionUser(1, "admin@opl.local", "active", 0))
	client.userUsage[1] = clients.Sub2APIBatchUserUsage{UserID: 1}
	for i := 1; i < count; i++ {
		id := int64(i + 1)
		email := fmt.Sprintf("owner-%03d@example.com", i)
		seedOperatorProjectionAccount(t, store, fmt.Sprintf("acct-%03d", i), fmt.Sprintf("usr-%03d", i), email, id)
		client.users = append(client.users, operatorProjectionUser(id, email, "active", 1_000_000))
		client.userUsage[id] = clients.Sub2APIBatchUserUsage{UserID: id, TodayActualCostUSDMicros: 2_000_000, TotalActualCostUSDMicros: 3_000_000}
		client.keyCounts[id] = 2
	}
	return store, client
}

func TestOperatorGatewaySummaryIncludesCompleteMappingAndDisabledAssets(t *testing.T) {
	store, client := operatorSummaryFixture(t, 51)
	store.accounts["acct-001"]["status"] = "disabled"
	store.users["usr-001"]["status"] = "disabled"
	client.users[1].Status = "disabled"
	client.users = append(client.users, operatorProjectionUser(999, "gateway-only@example.com", "active", math.MaxInt64))
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(store.accounts)
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/overview", "")
	if response.Code != http.StatusOK {
		t.Fatalf("overview=%s", response.Body.String())
	}
	data := mapField(decodeOperatorEnvelope(t, response), "data")
	if mapField(mapField(data, "wallet"), "data")["usdMicros"] != "50000000" || mapField(mapField(data, "keys"), "data")["total"] != float64(100) || mapField(mapField(data, "usage"), "data")["totalActualCostUsdMicros"] != float64(150_000_000) {
		t.Fatalf("totals=%#v", data)
	}
	if len(client.requestedUserIDs) != 2 || len(client.requestedUserIDs[0]) != 50 || len(client.requestedUserIDs[1]) != 1 || client.adminUsersCalls != 0 || client.adminUserCalls != 51 {
		t.Fatalf("reads=%#v exact=%d", client.requestedUserIDs, client.adminUserCalls)
	}
	after, _ := json.Marshal(store.accounts)
	if string(before) != string(after) || len(client.charges) != 0 {
		t.Fatal("summary mutated accounts or wallet")
	}
}

func TestOperatorGatewaySummaryIncompleteAndPrecisionSemantics(t *testing.T) {
	tests := []struct {
		name                string
		mutate              func(*memoryTableStore, *operatorProjectionSub2API)
		wallet, keys, usage bool
	}{
		{"zero balances", func(_ *memoryTableStore, c *operatorProjectionSub2API) { c.users[1].BalanceUSDMicros = 0 }, true, true, true},
		{"balance field unavailable", func(_ *memoryTableStore, c *operatorProjectionSub2API) { c.users[1].BalanceUnavailable = true }, false, true, true},
		{"identity read unavailable", func(_ *memoryTableStore, c *operatorProjectionSub2API) {
			c.adminUserErrs[2] = errors.New("upstream failed")
		}, false, false, false},
		{"identity mismatch", func(_ *memoryTableStore, c *operatorProjectionSub2API) { c.users[1].Email = "different@example.com" }, false, false, false},
		{"key count unavailable", func(_ *memoryTableStore, c *operatorProjectionSub2API) {
			c.keyCountErrs[2] = errors.New("upstream failed")
		}, true, false, true},
		{"last batch missing", func(_ *memoryTableStore, c *operatorProjectionSub2API) { delete(c.userUsage, 1) }, true, true, false},
		{"usage precision", func(_ *memoryTableStore, c *operatorProjectionSub2API) {
			c.userUsage[2] = clients.Sub2APIBatchUserUsage{UserID: 2, TodayActualCostUSDMicros: operatorMaxSafeInteger, TotalActualCostUSDMicros: operatorMaxSafeInteger}
		}, true, true, false},
		{"wallet overflow", func(_ *memoryTableStore, c *operatorProjectionSub2API) { c.users[1].BalanceUSDMicros = math.MaxInt64 }, false, true, true},
		{"duplicate binding", func(s *memoryTableStore, _ *operatorProjectionSub2API) {
			s.accounts["acct-001"]["sub2apiUserId"] = int64(1)
		}, false, false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, client := operatorSummaryFixture(t, 51)
			test.mutate(store, client)
			result := (&controlPlaneServer{tables: store}).operatorGatewaySummary(context.Background(), controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client))
			for field, want := range map[string]bool{"wallet": test.wallet, "keys": test.keys, "usage": test.usage} {
				envelope := mapField(result, field)
				if envelope["available"] != want {
					t.Fatalf("%s=%#v", field, envelope)
				}
				if !want && envelope["data"] != nil {
					t.Fatal("partial data exposed")
				}
			}
		})
	}
}

func TestOperatorGatewaySummaryEmptyAvoidsGateway(t *testing.T) {
	store, client := operatorSummaryFixture(t, 1)
	store.accounts = controlPlaneRecordSet{}
	result := (&controlPlaneServer{tables: store}).operatorGatewaySummary(context.Background(), controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client))
	for _, field := range []string{"wallet", "keys", "usage"} {
		if mapField(result, field)["status"] != "empty" {
			t.Fatalf("%s=%#v", field, result[field])
		}
	}
	if client.adminUserCalls != 0 || client.batchUsersCalls != 0 || len(client.keyCountCalls) != 0 {
		t.Fatal("empty set contacted gateway")
	}
}

func TestOperatorGatewaySummarySharesOneDeadlineAndBoundedReads(t *testing.T) {
	store, base := operatorSummaryFixture(t, 51)
	release := make(chan struct{})
	close(release)
	client := &boundedOperatorPageSub2API{operatorProjectionSub2API: base, started: make(chan string, 102), release: release}
	result := (&controlPlaneServer{tables: store}).operatorGatewaySummary(context.Background(), controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client))
	if mapField(result, "wallet")["available"] != true {
		t.Fatalf("summary=%#v", result)
	}
	if client.missingDeadline || client.maxActive > operatorPageTotalConcurrency || client.userMax > operatorPageLaneConcurrency || client.keyMax > operatorPageLaneConcurrency {
		t.Fatalf("bounds=%#v", client)
	}
	for _, deadline := range client.deadlines {
		if !deadline.Equal(client.deadlines[0]) {
			t.Fatal("per-batch deadline reset")
		}
	}
}

func TestOperatorResourceReadsObservationWithoutLegacyFallback(t *testing.T) {
	store := newMemoryTableStore()
	workspace := map[string]any{"id": "ws", "accountId": "acct-admin", "ownerAccountId": "acct-admin", "ownerUserId": "usr-admin"}
	account, _, _ := store.GetAccount(context.Background(), "acct-admin")
	owner, _, _ := store.GetUser(context.Background(), "usr-admin")
	for _, state := range []contracts.ResourceObservedState{contracts.ResourceObservedStopped, contracts.ResourceObservedAbsent, contracts.ResourceObservedPending, contracts.ResourceObservedRunning} {
		t.Run(string(state), func(t *testing.T) {
			fact := clients.ProviderFact{AccountID: "acct-admin", WorkspaceID: "ws", ResourceType: "compute", ResourceID: "compute", Available: false, ErrorCode: "old_error", Facts: clients.ProviderResourceFacts{Status: "RUNNING", ProviderID: "old-instance"}, Observation: &contracts.ResourceObservation{Available: true, State: state, ObservedAt: operatorProjectionTime.Format(time.RFC3339Nano), ProviderID: "current-instance"}}
			if state == contracts.ResourceObservedRunning {
				fact.Observation.ReasonCode = "compute_provider_partial_identity_machine_missing_tke_instance_missing"
			}
			facts := operatorWorkspaceFacts{providerFacts: map[string]clients.ProviderFact{operatorProviderFactKey("acct-admin", "ws", "compute", "compute"): fact}}
			app := &controlPlaneServer{tables: store}
			result := app.operatorResourceDTO(context.Background(), nil, "compute", map[string]any{"id": "compute"}, account, owner, workspace, facts, false)
			if mapField(result, "status")["data"] != string(state) || mapField(result, "providerId")["data"] != "current-instance" {
				t.Fatalf("projection=%#v", result)
			}
			if state == contracts.ResourceObservedRunning && (mapField(result, "providerErrorCode")["data"] != fact.Observation.ReasonCode || fact.Available) {
				t.Fatal("CVM observation lost its TKE binding failure or promoted Compute readiness")
			}
			fact.Observation = nil
			facts.providerFacts[operatorProviderFactKey("acct-admin", "ws", "compute", "compute")] = fact
			result = app.operatorResourceDTO(context.Background(), nil, "compute", map[string]any{"id": "compute"}, account, owner, workspace, facts, false)
			if mapField(result, "status")["available"] != false || mapField(result, "providerId")["available"] != false {
				t.Fatal("legacy fallback")
			}
		})
	}
}

func TestOperatorRuntimeReconciliationBusinessMatrixIsReadOnly(t *testing.T) {
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name              string
		state             string
		expired           bool
		desired, observed contracts.ResourceObservedState
		missing           bool
		status, reason    string
	}{
		{"running", "running", false, contracts.ResourceObservedRunning, contracts.ResourceObservedRunning, false, "running", ""},
		{"normal pause", "suspended", false, contracts.ResourceObservedSuspended, contracts.ResourceObservedSuspended, false, "suspended", ""},
		{"expired running", "running", true, contracts.ResourceObservedRunning, contracts.ResourceObservedRunning, false, "attention", "workspace_billing_period_expired"},
		{"expired stopped", "running", true, contracts.ResourceObservedSuspended, contracts.ResourceObservedSuspended, false, "suspended", ""},
		{"unexpected stop", "running", false, contracts.ResourceObservedSuspended, contracts.ResourceObservedSuspended, false, "attention", "runtime_unexpected_suspension"},
		{"missing", "running", false, "", "", true, "attention", "runtime_missing"},
		{"unready without transition", "running", false, contracts.ResourceObservedRunning, contracts.ResourceObservedPending, false, "attention", "runtime_not_ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryTableStore()
			workspace := workspaceLaunchUnitActivationProjectionRow(t, "ws", "acct-admin", "usr-admin")
			workspace["state"], workspace["status"] = tc.state, tc.state
			at := now
			if tc.expired {
				at = time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)
			}
			mustStore(t, store.SaveWorkspace(context.Background(), workspace))
			fabric := &runtimeHealthSummaryFabric{observations: contracts.RuntimeObservations{ObservedAt: now.Format(time.RFC3339Nano), Items: []contracts.RuntimeObservation{}}}
			if !tc.missing {
				fabric.observations.Items = append(fabric.observations.Items, contracts.RuntimeObservation{ObjectRef: "object", AccountID: "acct-admin", WorkspaceID: "ws", RuntimeID: "runtime-fabric", Ownership: contracts.RuntimeOwnershipVerified, DesiredState: tc.desired, ObservedState: tc.observed})
			}
			before := cloneMap(store.workspaces["ws"])
			result, err := (&controlPlaneServer{tables: store}).operatorRuntimeObservations(context.Background(), controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient()), at)
			if err != nil || len(result.Items) != 1 || result.Items[0].Status != tc.status || result.Items[0].ReasonCode != tc.reason {
				t.Fatalf("result=%#v err=%v", result, err)
			}
			if !reflect.DeepEqual(before, store.workspaces["ws"]) || len(store.runtimeOps) != 0 || fabric.fakeFabricClient.calls != nil {
				t.Fatal("observation mutated business/provider")
			}
		})
	}
}

func TestOperatorRuntimeInventoryKeepsUnmatchedAndDuplicateObjects(t *testing.T) {
	store := newMemoryTableStore()
	workspace := workspaceLaunchUnitActivationProjectionRow(t, "ws", "acct-admin", "usr-admin")
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))
	fabric := &runtimeHealthSummaryFabric{observations: operatorRuntimeFixtureObservations(24)}
	for i := 0; i < 2; i++ {
		fabric.observations.Items[i] = contracts.RuntimeObservation{ObjectRef: fmt.Sprintf("object-%d", i), AccountID: "acct-admin", WorkspaceID: "ws", RuntimeID: "runtime-fabric", Ownership: contracts.RuntimeOwnershipVerified, DesiredState: contracts.ResourceObservedRunning, ObservedState: contracts.ResourceObservedRunning}
	}
	result, err := (&controlPlaneServer{tables: store}).operatorRuntimeObservations(context.Background(), controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient()), operatorProjectionTime)
	if err != nil || result.OwnershipScope != "workspaces_and_retained_operations" || result.ObservedTotal != 24 || result.BusinessTotal != 1 || result.UnmatchedCount != 22 || result.AttentionCount != 24 || len(result.Items) != 24 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	for _, item := range result.Items {
		if item.WorkspaceID == "ws" && item.ReasonCode != "runtime_multiple_objects" {
			t.Fatalf("duplicate=%#v", item)
		}
	}
}

func TestOperatorRuntimeMissingBeforeLaunchRuntimeStageIsNotMissingMachine(t *testing.T) {
	command := workspaceLaunchUnitCommand()
	launch, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	row, err := workspaceLaunchReconcileOperationRow(launch)
	if err != nil {
		t.Fatal(err)
	}
	status, reason := operatorRuntimeState(map[string]any{"id": command.WorkspaceID, "accountId": command.AccountID, "state": "creating"}, []map[string]any{row}, nil, time.Now())
	if status != "pending" || reason != "workspace_runtime_not_created" {
		t.Fatalf("state=%s reason=%s", status, reason)
	}
}

func TestOperatorRuntimeWithoutWorkspacePreservesRetainedLaunchOwnership(t *testing.T) {
	store := newMemoryTableStore()
	command := workspaceLaunchUnitCommand()
	launch, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	row, err := workspaceLaunchReconcileOperationRow(launch)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	fabric := &runtimeHealthSummaryFabric{observations: contracts.RuntimeObservations{
		ObservedAt: operatorProjectionTime.Format(time.RFC3339Nano),
		Items: []contracts.RuntimeObservation{{ObjectRef: "launch-runtime", AccountID: command.AccountID,
			WorkspaceID: command.WorkspaceID, RuntimeID: "runtime-fabric", Ownership: contracts.RuntimeOwnershipVerified,
			DesiredState: contracts.ResourceObservedRunning, ObservedState: contracts.ResourceObservedPending}},
	}}
	result, err := (&controlPlaneServer{tables: store}).operatorRuntimeObservations(context.Background(), controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient()), operatorProjectionTime)
	if err != nil || result.OwnershipScope != "workspaces_and_retained_operations" || len(result.Items) != 1 || result.BusinessTotal != 0 || result.UnmatchedCount != 1 || result.Items[0].ReasonCode != "runtime_operation_without_workspace" {
		t.Fatalf("retained launch lost its Runtime ownership: result=%#v err=%v", result, err)
	}
	if len(store.runtimeOps) != 1 || len(store.workspaces) != 0 || fabric.fakeFabricClient.calls != nil {
		t.Fatal("ownership observation mutated its sources")
	}
}

type operatorRuntimeOperationsUnavailableStore struct{ *memoryTableStore }

func (s *operatorRuntimeOperationsUnavailableStore) ListRuntimeOperations(context.Context) ([]map[string]any, error) {
	return nil, errors.New("runtime_operations_unavailable")
}

func TestOperatorRuntimeCannotDeclareUnmatchedWhenBusinessOperationsAreUnreadable(t *testing.T) {
	store := &operatorRuntimeOperationsUnavailableStore{newMemoryTableStore()}
	fabric := &runtimeHealthSummaryFabric{observations: operatorRuntimeFixtureObservations(1)}
	result, err := (&controlPlaneServer{tables: store}).operatorRuntimeObservations(context.Background(), controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient()), operatorProjectionTime)
	if err == nil || result.Items != nil || result.OwnershipScope != "" {
		t.Fatalf("unreadable business operations became absence: result=%#v err=%v", result, err)
	}
}

func TestOperatorRuntimeObservationEndpointIsAdministratorOnly(t *testing.T) {
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-a", "usr-a", "a@example.com", 41)
	fabric := &runtimeHealthSummaryFabric{observations: operatorRuntimeFixtureObservations(1)}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient()), store)
	if err != nil {
		t.Fatal(err)
	}
	customer := loginForTest(t, server, "a@example.com", "CorrectHorseBatteryStaple!")
	response := requestWithSession(t, server, customer, http.MethodGet, "/api/operator/runtime-observations", "")
	if response.Code != http.StatusForbidden {
		t.Fatalf("customer=%d %s", response.Code, response.Body.String())
	}
	response = requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/runtime-observations", "")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "control-plane+fabric") {
		t.Fatalf("admin=%d %s", response.Code, response.Body.String())
	}
}

type operatorIndependentReadinessFabric struct{ fakeFabricClient }

func (operatorIndependentReadinessFabric) Readiness(context.Context) (contracts.FabricReadiness, error) {
	return contracts.FabricReadiness{ServiceReady: true, Ready: false, CloudImagesReady: true, FailedChecks: []string{"workspace_image_id"}}, nil
}

func TestOperatorFabricServiceReadyDoesNotPromoteReleaseReadiness(t *testing.T) {
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &operatorIndependentReadinessFabric{}, newOperatorProjectionClient()), newMemoryTableStore())
	if err != nil {
		t.Fatal(err)
	}
	response := requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/operator/health", "")
	fabric := mapField(mapField(mapField(decodeOperatorEnvelope(t, response), "data"), "fabric"), "data")
	if fabric["ready"] != true || fabric["serviceReady"] != true || fabric["releaseReady"] != false || len(fabric["failedChecks"].([]any)) != 1 {
		t.Fatalf("fabric=%#v", fabric)
	}
	response = requestWithSession(t, server, reservedOperatorSessionForTest(t, server), http.MethodGet, "/api/production/readiness", "")
	var release map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &release); err != nil {
		t.Fatal(err)
	}
	if release["ready"] != false {
		t.Fatalf("strict release qualified by service readiness: %#v", release)
	}
}

func TestOperatorRuntimePendingRequiresAuthoritativeTransition(t *testing.T) {
	command := workspaceLaunchUnitCommand()
	launch := workspaceLaunchRuntimeRepairFixture(t)
	launch.Stage = contracts.StageRuntime
	launch.Status = contracts.StatusPending
	attempt := launch.Attempts[contracts.StageRuntime]
	attempt.Unknown, attempt.Status = 0, "reserved"
	launch.Attempts[contracts.StageRuntime] = attempt
	launch.Observations[contracts.StageRuntime] = workspaceLaunchStageObservation{State: workspaceLaunchStagePending}
	row, err := workspaceLaunchReconcileOperationRow(launch)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeWorkspaceLaunchReconcileOperation(row); err != nil {
		t.Fatal(err)
	}
	workspace := workspaceLaunchUnitActivationProjectionRow(t, command.WorkspaceID, command.AccountID, command.OwnerUserID)
	observation := contracts.RuntimeObservation{DesiredState: contracts.ResourceObservedRunning, ObservedState: contracts.ResourceObservedPending}
	now := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	status, reason := operatorRuntimeState(workspace, []map[string]any{row}, &observation, now)
	if status != "pending" || reason != "runtime_not_ready" {
		t.Fatalf("active launch=%s %s", status, reason)
	}
	status, reason = operatorRuntimeState(workspace, nil, &observation, now)
	if status != "attention" || reason != "runtime_not_ready" {
		t.Fatalf("without launch=%s %s", status, reason)
	}
	observation.ReasonCode = "runtime_generation_pending"
	if state, _ := operatorRuntimeState(workspace, nil, &observation, now); state != "pending" {
		t.Fatalf("generation update=%s", state)
	}
	observation.ReasonCode = "runtime_workload_pending"
	if state, _ := operatorRuntimeState(workspace, nil, &observation, now); state != "attention" {
		t.Fatalf("workload fault=%s", state)
	}
	workspace["state"], workspace["status"] = "suspended", "suspended"
	status, reason = operatorRuntimeState(workspace, nil, nil, now)
	if status != "suspended" || reason != "runtime_absent_while_suspended" {
		t.Fatalf("missing paused runtime hidden: %s %s", status, reason)
	}
}

func TestOperatorRuntimeIncompleteDiscoveryCannotProveAbsence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result contracts.RuntimeObservations
		err    error
	}{
		{"failure", contracts.RuntimeObservations{}, errors.New("cloud timeout")},
		{"missing items", contracts.RuntimeObservations{ObservedAt: "2026-09-10T00:00:00Z"}, nil},
		{"duplicate object", contracts.RuntimeObservations{ObservedAt: "2026-09-10T00:00:00Z", Items: []contracts.RuntimeObservation{{ObjectRef: "same", Ownership: contracts.RuntimeOwnershipUnregistered, DesiredState: contracts.ResourceObservedUnknown, ObservedState: contracts.ResourceObservedUnknown}, {ObjectRef: "same", Ownership: contracts.RuntimeOwnershipUnregistered, DesiredState: contracts.ResourceObservedUnknown, ObservedState: contracts.ResourceObservedUnknown}}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fabric := &runtimeHealthSummaryFabric{observations: tc.result, observationErr: tc.err}
			app := &controlPlaneServer{tables: newMemoryTableStore()}
			health := app.operatorRuntimeHealth(context.Background(), controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient()))
			if health["available"] != false || health["data"] != nil {
				t.Fatalf("incomplete discovery=%#v", health)
			}
		})
	}
}

func TestOperatorRuntimeRejectsIncompleteHTTPDiscovery(t *testing.T) {
	for _, body := range []string{`{}`, `{"observedAt":"2026-09-10T00:00:00Z","items":null}`, `{"observedAt":"","items":[]}`} {
		t.Run(body, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet || r.URL.Path != "/fabric/runtime-observations" {
					t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
				}
				_, _ = w.Write([]byte(body))
			}))
			defer upstream.Close()
			fabric := clients.NewFabricHTTPClientWithCapability(upstream.URL, "internal-secret", "capability-key", upstream.Client())
			_, err := (&controlPlaneServer{tables: newMemoryTableStore()}).operatorRuntimeObservations(context.Background(), controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient()), time.Now())
			if err == nil {
				t.Fatal("incomplete HTTP read proved empty inventory")
			}
		})
	}
}

type operatorSecondUsageBatchFailure struct{ *operatorProjectionSub2API }

func (c *operatorSecondUsageBatchFailure) BatchUsersUsage(ctx context.Context, ids []int64) (map[int64]clients.Sub2APIBatchUserUsage, error) {
	if c.batchUsersCalls == 1 {
		c.batchUsersCalls++
		return nil, errors.New("second_batch_failed")
	}
	return c.operatorProjectionSub2API.BatchUsersUsage(ctx, ids)
}
func TestOperatorGatewaySummarySecondBatchFailurePreservesOtherCompleteMetrics(t *testing.T) {
	store, base := operatorSummaryFixture(t, 51)
	client := &operatorSecondUsageBatchFailure{base}
	result := (&controlPlaneServer{tables: store}).operatorGatewaySummary(context.Background(), controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client))
	if client.batchUsersCalls != 2 || mapField(result, "usage")["available"] != false || mapField(result, "usage")["data"] != nil || mapField(result, "wallet")["available"] != true || mapField(result, "keys")["available"] != true {
		t.Fatalf("batch failure=%#v", result)
	}
}
