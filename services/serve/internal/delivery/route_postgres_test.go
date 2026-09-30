package delivery_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

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

func absentPrecondition() *api.ProviderRevisionPrecondition {
	return &api.ProviderRevisionPrecondition{Condition: &api.ProviderRevisionPrecondition_RequireAbsent{RequireAbsent: &api.ConfirmedRouteAbsence{ReceiptId: "absence-receipt", ObservedAt: timestamppb.New(time.Now().Add(-time.Minute).UTC())}}}
}

func exactPrecondition(revision string) *api.ProviderRevisionPrecondition {
	return &api.ProviderRevisionPrecondition{Condition: &api.ProviderRevisionPrecondition_ExactRevision{ExactRevision: revision}}
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

	fenced, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()})
	if err != nil || fenced.GetObservation() != api.Observation_OBSERVATION_CONFIRMED || fenced.GetCurrentGeneration() != 0 || fenced.GetAcceptedExecutionEpoch() != 1 {
		t.Fatalf("fence=%+v err=%v", fenced, err)
	}
	// The confirmed revision is derived from the exact switch identity, so a
	// reader can recompute it instead of trusting an opaque token.
	if want := "serve-route/" + fenced.GetSwitchId(); fenced.GetProviderRevision() != want {
		t.Fatalf("fence revision=%q want %q", fenced.GetProviderRevision(), want)
	}
	activated, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: exactPrecondition(fenced.GetProviderRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"})
	if err != nil || activated.GetObservation() != api.Observation_OBSERVATION_CONFIRMED || activated.GetCurrentGeneration() != 1 {
		t.Fatalf("activate=%+v err=%v", activated, err)
	}
	if activated.GetTargetRuntimeInstanceId() != runtimeInstance || activated.GetTargetDeploymentId() != "dep-route" {
		t.Fatalf("activate target=%+v", activated)
	}

	// Serve committed its own single current route fact and the deployment names
	// the switch that owns it.
	var revision, switchID, deploymentSwitch, bindingRuntime, bindingDeployment string
	if err := db.QueryRowContext(ctx, `SELECT provider_revision,COALESCE(last_confirmed_switch_id,''),COALESCE(target_runtime_instance_id,''),COALESCE(target_deployment_id,'') FROM serve.access_bindings WHERE workspace_id=$1`, workspace).Scan(&revision, &switchID, &bindingRuntime, &bindingDeployment); err != nil {
		t.Fatal(err)
	}
	if revision != activated.GetProviderRevision() || switchID != activated.GetSwitchId() || bindingRuntime != runtimeInstance || bindingDeployment != "dep-route" {
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
	_, err = service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: exactPrecondition(fenced.GetProviderRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"})
	if status.Code(err) != codes.Aborted || !strings.Contains(err.Error(), delivery.ReasonRouteGenerationConflict) {
		t.Fatalf("stale generation activate err=%v", err)
	}
}

func TestServeRouteRefusesUnconfirmedPreconditionAndUnreadyTarget(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	ctx := routeServeContext()

	// A deployment whose runtime instance is not ready is never a route target.
	_, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition(), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonDeploymentNotReady) {
		t.Fatalf("unready target err=%v", err)
	}

	markRouteTargetReady(t, db, runtimeInstance)
	if _, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()}); err != nil {
		t.Fatal(err)
	}
	// A revision the Workspace never confirmed does not condition a switch.
	_, err = service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: exactPrecondition("rev-invented"), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonRoutePreconditionInvalid) {
		t.Fatalf("invented revision err=%v", err)
	}
	// A confirmed-absence precondition is only truthful before the first switch.
	_, err = service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition(), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
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
	if _, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()}); err != nil {
		t.Fatal(err)
	}
	var bindingID string
	if err := db.QueryRowContext(ctx, `SELECT id FROM serve.access_bindings WHERE workspace_id=$1`, workspace).Scan(&bindingID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO serve.access_switches(id,route_binding_id,workspace_id,operation_owner,operation_id,expected_route_generation,execution_epoch,provider_command_id,status,action_kind,expected_provider_revision) VALUES('rsw_interrupted',$1,$2,'serve','op-interrupted',0,1,'serve-route:rsw_interrupted','requested','fence','serve-route/rsw_previous')`, bindingID, workspace); err != nil {
		t.Fatal(err)
	}

	_, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: exactPrecondition("serve-route/rsw_interrupted-predecessor"), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonRouteSwitchUnresolved) {
		t.Fatalf("activate behind unresolved switch err=%v", err)
	}

	// The original switch is reported by its own identity, never replaced.
	readback, err := service.ObserveRoute(ctx, &api.RouteObserveRequest{Context: routeCall(tenant), WorkspaceId: workspace, SwitchId: proto.String("rsw_interrupted")})
	if err != nil || readback.GetObservation() != api.Observation_OBSERVATION_UNKNOWN || readback.GetProviderCommandId() != "serve-route:rsw_interrupted" {
		t.Fatalf("readback=%+v err=%v", readback, err)
	}
}

func TestServeRouteRefusesOlderEpochAndUnadmittedCaller(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	ctx := routeServeContext()
	if _, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 3, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()}); err != nil {
		t.Fatal(err)
	}
	// An older execution epoch may not fence or advance the binding: the accepted
	// epoch only moves forward.
	_, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 2, ExpectedRouteGeneration: 0, ProviderPrecondition: exactPrecondition("serve-route/rsw_anything")})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonStaleExecutionEpoch) {
		t.Fatalf("older epoch fence err=%v", err)
	}
	// Only Serve's own delivery step may execute a route switch.
	foreign := ownerservice.WithPeerOwner(context.Background(), owneridentity.Workspace.Service())
	if _, err := service.FenceRouteEpoch(foreign, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 4, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign caller fence err=%v", err)
	}
}

func TestServeRouteRollbackRestoresConfirmedTarget(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	ctx := routeServeContext()
	fenced, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()})
	if err != nil {
		t.Fatal(err)
	}
	activated, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: exactPrecondition(fenced.GetProviderRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"})
	if err != nil {
		t.Fatal(err)
	}
	// A rollback to the original switch's predecessor target requires that switch
	// to be confirmed and a matching route revision; it advances the generation
	// by one.
	rolledBack, err := service.RollbackRoute(ctx, &api.RouteRollbackCommand{Context: routeCall(tenant), WorkspaceId: workspace, OriginalSwitchId: activated.GetSwitchId(), OperationId: "op-route", ExecutionEpoch: 2, ExpectedRouteGeneration: 1, ProviderPrecondition: exactPrecondition(activated.GetProviderRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route", CompatibilityReceiptId: "readiness://dep-route"})
	if err != nil || rolledBack.GetObservation() != api.Observation_OBSERVATION_CONFIRMED || rolledBack.GetCurrentGeneration() != 2 {
		t.Fatalf("rollback=%+v err=%v", rolledBack, err)
	}
	// An unknown original switch has no confirmed route to roll back from.
	_, err = service.RollbackRoute(ctx, &api.RouteRollbackCommand{Context: routeCall(tenant), WorkspaceId: workspace, OriginalSwitchId: "rsw_missing", OperationId: "op-route", ExecutionEpoch: 2, ExpectedRouteGeneration: 2, ProviderPrecondition: exactPrecondition(rolledBack.GetProviderRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.NotFound || !strings.Contains(err.Error(), delivery.ReasonRouteSwitchNotFound) {
		t.Fatalf("unknown original switch err=%v", err)
	}
}
