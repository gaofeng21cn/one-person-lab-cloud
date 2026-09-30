package delivery_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/serve/internal/delivery"
)

func routeServeContext() context.Context {
	return ownerservice.WithPeerOwner(context.Background(), owneridentity.Serve.Service())
}

func routeCall(tenant string) *api.CallContext {
	c := call(tenant, false)
	c.IdempotencyKey = "route-1"
	return c
}

// absentPrecondition is the caller's assertion that it read Serve's own access
// binding and found no confirmed route, which is only truthful before the first
// switch. There is no external router revision and no absence receipt to present.
func absentPrecondition() *api.RouteRevisionPrecondition {
	return &api.RouteRevisionPrecondition{Condition: &api.RouteRevisionPrecondition_RequireAbsent{RequireAbsent: &api.RouteAbsence{}}}
}

func exactPrecondition(revision string) *api.RouteRevisionPrecondition {
	return &api.RouteRevisionPrecondition{Condition: &api.RouteRevisionPrecondition_ExactRevision{ExactRevision: revision}}
}

// routeFixture seeds one workspace with a delivered deployment and returns the
// Workspace's runtime instance identity together with Serve's own service.
func routeFixture(t *testing.T) (*delivery.Service, string, string, string, *sql.DB) {
	t.Helper()
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	workspace := "ws-route"
	seedObservationDeployment(t, db, workspace, tenant, "dep-route", "verifying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	var runtimeInstance string
	if err := db.QueryRowContext(context.Background(), `SELECT runtime_instance_id FROM serve.agent_deployments WHERE id=$1`, "dep-route").Scan(&runtimeInstance); err != nil {
		t.Fatal(err)
	}
	return service, tenant, workspace, runtimeInstance, db
}

func markRouteTargetReady(t *testing.T, db *sql.DB, runtimeInstance string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), `UPDATE serve.agent_runtime_instances SET status='ready',access_url='https://ws-route.example/',readiness_evidence_ref='readiness://dep-route',observed_at=now(),access_upstream_service='app-main',access_upstream_port=8080 WHERE id=$1`, runtimeInstance); err != nil {
		t.Fatal(err)
	}
}

func TestServeRouteFenceActivatesAndObservesSingleCurrent(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	ctx := routeServeContext()

	fenced, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition()})
	if err != nil || fenced.GetObservation() != api.Observation_OBSERVATION_CONFIRMED || fenced.GetCurrentGeneration() != 0 || fenced.GetAcceptedExecutionEpoch() != 1 {
		t.Fatalf("fence=%+v err=%v", fenced, err)
	}
	// The confirmed revision is derived from the exact switch identity, so a
	// reader can recompute it instead of trusting an opaque token.
	if want := "serve-route/" + fenced.GetSwitchId(); fenced.GetRouteRevision() != want {
		t.Fatalf("fence revision=%q want %q", fenced.GetRouteRevision(), want)
	}
	activated, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: exactPrecondition(fenced.GetRouteRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"})
	if err != nil || activated.GetObservation() != api.Observation_OBSERVATION_CONFIRMED || activated.GetCurrentGeneration() != 1 {
		t.Fatalf("activate=%+v err=%v", activated, err)
	}
	if activated.GetTargetRuntimeInstanceId() != runtimeInstance || activated.GetTargetDeploymentId() != "dep-route" {
		t.Fatalf("activate target=%+v", activated)
	}

	// Serve committed its own single current route fact and the deployment names
	// the switch that owns it.
	var revision, switchID, deploymentSwitch, bindingRuntime, bindingDeployment string
	if err := db.QueryRowContext(ctx, `SELECT route_revision,COALESCE(last_confirmed_switch_id,''),COALESCE(target_runtime_instance_id,''),COALESCE(target_deployment_id,'') FROM serve.access_bindings WHERE workspace_id=$1`, workspace).Scan(&revision, &switchID, &bindingRuntime, &bindingDeployment); err != nil {
		t.Fatal(err)
	}
	if revision != activated.GetRouteRevision() || switchID != activated.GetSwitchId() || bindingRuntime != runtimeInstance || bindingDeployment != "dep-route" {
		t.Fatalf("binding revision=%q switch=%q runtime=%q deployment=%q", revision, switchID, bindingRuntime, bindingDeployment)
	}
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(confirmed_route_switch_id,'') FROM serve.agent_deployments WHERE id='dep-route'`).Scan(&deploymentSwitch); err != nil {
		t.Fatal(err)
	}
	if deploymentSwitch != activated.GetSwitchId() {
		t.Fatalf("deployment switch=%q want %q", deploymentSwitch, activated.GetSwitchId())
	}
	var bindings int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM serve.access_bindings WHERE workspace_id=$1`, workspace).Scan(&bindings); err != nil {
		t.Fatal(err)
	}
	if bindings != 1 {
		t.Fatalf("route bindings for one workspace = %d, want 1", bindings)
	}
	observed, err := service.ObserveRoute(ctx, &api.RouteObserveRequest{Context: routeCall(tenant), WorkspaceId: workspace})
	if err != nil || observed.GetObservation() != api.Observation_OBSERVATION_UNKNOWN || observed.GetCurrentGeneration() != 1 || observed.GetTargetRuntimeInstanceId() != runtimeInstance {
		t.Fatalf("observe=%+v err=%v", observed, err)
	}

	// A second writer with the same expectation must not advance the generation
	// again: the conditional revision check refuses and the route stays where the
	// owner confirmed it.
	_, err = service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: exactPrecondition(fenced.GetRouteRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"})
	if status.Code(err) != codes.Aborted || !strings.Contains(err.Error(), delivery.ReasonRouteGenerationConflict) {
		t.Fatalf("stale generation activate err=%v", err)
	}
}

func TestServeRouteRefusesUnconfirmedPreconditionAndUnreadyTarget(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	ctx := routeServeContext()

	// A deployment whose runtime instance is not ready is never a route target.
	_, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition(), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonDeploymentNotReady) {
		t.Fatalf("unready target err=%v", err)
	}

	markRouteTargetReady(t, db, runtimeInstance)
	if _, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition()}); err != nil {
		t.Fatal(err)
	}
	// A revision the Workspace never confirmed does not condition a switch.
	_, err = service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: exactPrecondition("rev-invented"), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonRoutePreconditionInvalid) {
		t.Fatalf("invented revision err=%v", err)
	}
	// A confirmed-absence precondition is only truthful before the first switch.
	_, err = service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition(), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonRoutePreconditionInvalid) {
		t.Fatalf("absence after fence err=%v", err)
	}
}

// TestServeRouteLegacyUnresolvedSwitchBlocksAndIsReadBack proves the one state a
// crashed record-then-confirm writer can leave behind: an unresolved switch that
// must be read back by its original identity before any new switch is committed.
func TestServeRouteLegacyUnresolvedSwitchBlocksAndIsReadBack(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	ctx := routeServeContext()
	if _, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition()}); err != nil {
		t.Fatal(err)
	}
	var bindingID string
	if err := db.QueryRowContext(ctx, `SELECT id FROM serve.access_bindings WHERE workspace_id=$1`, workspace).Scan(&bindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO serve.access_switches(id,route_binding_id,workspace_id,operation_owner,operation_id,expected_route_generation,execution_epoch,status,action_kind,expected_route_revision) VALUES('rsw_interrupted',$1,$2,'serve','op-interrupted',0,1,'requested','fence','serve-route/rsw_previous')`, bindingID, workspace); err != nil {
		t.Fatal(err)
	}

	_, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: exactPrecondition("serve-route/rsw_interrupted-predecessor"), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonRouteSwitchUnresolved) {
		t.Fatalf("activate behind unresolved switch err=%v", err)
	}

	// The original switch is reported by its own identity, never replaced.
	readback, err := service.ObserveRoute(ctx, &api.RouteObserveRequest{Context: routeCall(tenant), WorkspaceId: workspace, SwitchId: proto.String("rsw_interrupted")})
	if err != nil || readback.GetObservation() != api.Observation_OBSERVATION_UNKNOWN || readback.GetSwitchId() != "rsw_interrupted" {
		t.Fatalf("readback=%+v err=%v", readback, err)
	}
}

func TestServeRouteRefusesOlderEpochAndUnadmittedCaller(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	ctx := routeServeContext()
	if _, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 3, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition()}); err != nil {
		t.Fatal(err)
	}
	// An older execution epoch may not fence or advance the binding: the accepted
	// epoch only moves forward.
	_, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 2, ExpectedRouteGeneration: 0, RevisionPrecondition: exactPrecondition("serve-route/rsw_anything")})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonStaleExecutionEpoch) {
		t.Fatalf("older epoch fence err=%v", err)
	}
	// Only Serve's own delivery step may execute a route switch.
	foreign := ownerservice.WithPeerOwner(context.Background(), owneridentity.Workspace.Service())
	if _, err := service.FenceRouteEpoch(foreign, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 4, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition()}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign caller fence err=%v", err)
	}
}

func TestServeRouteRollbackRestoresConfirmedTarget(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	ctx := routeServeContext()
	fenced, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition()})
	if err != nil {
		t.Fatal(err)
	}
	activated, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: exactPrecondition(fenced.GetRouteRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"})
	if err != nil {
		t.Fatal(err)
	}
	// A rollback to the original switch's predecessor target requires that switch
	// to be confirmed and a matching route revision; it advances the generation
	// by one.
	rolledBack, err := service.RollbackRoute(ctx, &api.RouteRollbackCommand{Context: routeCall(tenant), WorkspaceId: workspace, OriginalSwitchId: activated.GetSwitchId(), OperationId: "op-route", ExecutionEpoch: 2, ExpectedRouteGeneration: 1, RevisionPrecondition: exactPrecondition(activated.GetRouteRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route", CompatibilityReceiptId: "readiness://dep-route"})
	if err != nil || rolledBack.GetObservation() != api.Observation_OBSERVATION_CONFIRMED || rolledBack.GetCurrentGeneration() != 2 {
		t.Fatalf("rollback=%+v err=%v", rolledBack, err)
	}
	// An unknown original switch has no confirmed route to roll back from.
	_, err = service.RollbackRoute(ctx, &api.RouteRollbackCommand{Context: routeCall(tenant), WorkspaceId: workspace, OriginalSwitchId: "rsw_missing", OperationId: "op-route", ExecutionEpoch: 2, ExpectedRouteGeneration: 2, RevisionPrecondition: exactPrecondition(rolledBack.GetRouteRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.NotFound || !strings.Contains(err.Error(), delivery.ReasonRouteSwitchNotFound) {
		t.Fatalf("unknown original switch err=%v", err)
	}
}

// TestServeRouteReplayedSwitchKeepsOneIdentity proves the switch identity is the
// row identity: a replay of the same logical switch names the same row instead of
// allocating a second one, the route it may not re-apply is refused with the
// precondition that actually failed, and the reader recovers the confirmed fact by
// the original switch id. No intermediate command identity exists to disagree with
// it.
func TestServeRouteReplayedSwitchKeepsOneIdentity(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	ctx := routeServeContext()

	command := &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition()}
	fenced, err := service.FenceRouteEpoch(ctx, command)
	if err != nil || fenced.GetObservation() != api.Observation_OBSERVATION_CONFIRMED {
		t.Fatalf("fence=%+v err=%v", fenced, err)
	}
	if _, err := service.FenceRouteEpoch(ctx, command); status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonRoutePreconditionInvalid) {
		t.Fatalf("replayed fence err=%v", err)
	}
	var rows int
	var rowID string
	var rowRevision sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM serve.access_switches WHERE route_binding_id=(SELECT id FROM serve.access_bindings WHERE workspace_id=$1)`, workspace).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRowContext(ctx, `SELECT id, expected_route_revision FROM serve.access_switches WHERE route_binding_id=(SELECT id FROM serve.access_bindings WHERE workspace_id=$1)`, workspace).Scan(&rowID, &rowRevision); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || rowID != fenced.GetSwitchId() {
		t.Fatalf("switches=%d id=%q want one switch %q", rows, rowID, fenced.GetSwitchId())
	}
	// The fence asserted absence, so the row carries no expected revision at all
	// instead of an empty string that would read as a wildcard.
	if rowRevision.Valid {
		t.Fatalf("a fence conditioned on absence recorded expected revision %q", rowRevision.String)
	}
	// A confirmed fence leaves its route generation alone, so the route revision
	// it confirmed is the one a later switch must present.
	readback, err := service.ObserveRoute(ctx, &api.RouteObserveRequest{Context: routeCall(tenant), WorkspaceId: workspace, SwitchId: proto.String(fenced.GetSwitchId())})
	if err != nil || readback.GetObservation() != api.Observation_OBSERVATION_CONFIRMED || readback.GetSwitchId() != fenced.GetSwitchId() || readback.GetRouteRevision() != fenced.GetRouteRevision() {
		t.Fatalf("readback=%+v err=%v", readback, err)
	}
	if _, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: exactPrecondition(readback.GetRouteRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"}); err != nil {
		t.Fatal(err)
	}
}

// TestServeRouteUnknownSwitchIsResumedByItsOwnIdentity proves the one unresolved
// state an interrupted writer can leave: an unknown switch blocks every new switch
// for its binding, no presented precondition lifts the block, and the identity the
// reader resumes from is the switch row itself.
func TestServeRouteUnknownSwitchIsResumedByItsOwnIdentity(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	ctx := routeServeContext()
	if _, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition()}); err != nil {
		t.Fatal(err)
	}
	var bindingID string
	if err := db.QueryRowContext(ctx, `SELECT id FROM serve.access_bindings WHERE workspace_id=$1`, workspace).Scan(&bindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO serve.access_switches(id,route_binding_id,workspace_id,operation_owner,operation_id,expected_route_generation,execution_epoch,status,action_kind,expected_route_revision) VALUES('rsw_unknown',$1,$2,'serve','op-unknown',0,2,'unknown','fence','serve-route/rsw_earlier')`, bindingID, workspace); err != nil {
		t.Fatal(err)
	}
	// A later switch for a different epoch is blocked, and the refusal names the
	// identity the reader must resume from.
	_, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 3, ExpectedRouteGeneration: 0, RevisionPrecondition: exactPrecondition("serve-route/rsw_earlier"), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonRouteSwitchUnresolved) || !strings.Contains(err.Error(), "rsw_unknown") {
		t.Fatalf("blocked switch err=%v", err)
	}
	readback, err := service.ObserveRoute(ctx, &api.RouteObserveRequest{Context: routeCall(tenant), WorkspaceId: workspace, SwitchId: proto.String("rsw_unknown")})
	if err != nil || readback.GetObservation() != api.Observation_OBSERVATION_UNKNOWN || readback.GetSwitchId() != "rsw_unknown" || readback.GetAcceptedExecutionEpoch() != 2 {
		t.Fatalf("readback=%+v err=%v", readback, err)
	}
}

// TestServeRouteSwitchRowHoldsOnlyLocalRouteFacts pins the schema shape this owner
// migrated to: the switch row carries the route revision Serve itself confirmed and
// the switch identity, and none of the retired external-router facts.
func TestServeRouteSwitchRowHoldsOnlyLocalRouteFacts(t *testing.T) {
	_, _, _, _, db := routeFixture(t)
	ctx := context.Background()
	columns := map[string]bool{}
	rows, err := db.QueryContext(ctx, `SELECT column_name FROM information_schema.columns WHERE table_schema='serve' AND table_name IN ('access_switches','access_bindings')`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"route_revision", "expected_route_revision", "observed_route_revision"} {
		if !columns[want] {
			t.Errorf("serve route tables must hold %q", want)
		}
	}
	for _, retired := range []string{"provider_revision", "expected_provider_revision", "observed_provider_revision", "provider_command_id", "provider_request_ref", "expected_absence_receipt_id", "expected_absence_observed_at"} {
		if columns[retired] {
			t.Errorf("serve route tables must not hold the retired %q", retired)
		}
	}
}
