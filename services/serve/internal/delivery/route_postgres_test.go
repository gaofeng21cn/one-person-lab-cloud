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

// routeProviderForServe is an isolated installation route provider fixture. It
// records every switch Serve committed to and answers exactly like a provider that
// performs conditional-revision CAS, so Serve's own fencing, CAS and settlement
// are exercised without a real cluster.
type routeProviderForServe struct {
	revision                       string
	evidencePrefix                 string
	fenceOutcome                   api.Observation
	activateOutcome                api.Observation
	rollbackOutcome                api.Observation
	failWith                       error
	calls                          []delivery.RouteProviderRequest
	observeCalls                   int
	observedGenerationOverride     *int64
	observedExecutionEpochOverride *int64
}

func (p *routeProviderForServe) result(outcome api.Observation, request delivery.RouteProviderRequest) (delivery.RouteProviderResult, error) {
	p.calls = append(p.calls, request)
	if p.failWith != nil {
		return delivery.RouteProviderResult{Outcome: api.Observation_OBSERVATION_UNKNOWN, ProviderCommandID: request.ProviderCommandID}, p.failWith
	}
	generation := request.ExpectedRouteGeneration
	if request.ActionKind != "fence" {
		generation++
	}
	result := delivery.RouteProviderResult{
		Outcome: outcome, ProviderCommandID: request.ProviderCommandID,
		ProviderRevision: p.revision, ObservedRouteGeneration: generation, ObservedExecutionEpoch: request.ExecutionEpoch,
	}
	if outcome == api.Observation_OBSERVATION_CONFIRMED {
		result.EvidenceRef = p.evidencePrefix + request.SwitchID
	}
	if p.observedGenerationOverride != nil {
		result.ObservedRouteGeneration = *p.observedGenerationOverride
	}
	if p.observedExecutionEpochOverride != nil {
		result.ObservedExecutionEpoch = *p.observedExecutionEpochOverride
	}
	return result, nil
}

func (p *routeProviderForServe) FenceRoute(_ context.Context, r delivery.RouteProviderRequest) (delivery.RouteProviderResult, error) {
	return p.result(p.fenceOutcome, r)
}
func (p *routeProviderForServe) ActivateRoute(_ context.Context, r delivery.RouteProviderRequest) (delivery.RouteProviderResult, error) {
	return p.result(p.activateOutcome, r)
}
func (p *routeProviderForServe) RollbackRoute(_ context.Context, r delivery.RouteProviderRequest) (delivery.RouteProviderResult, error) {
	return p.result(p.rollbackOutcome, r)
}
func (p *routeProviderForServe) ObserveRoute(_ context.Context, r delivery.RouteProviderObserveRequest) (delivery.RouteProviderResult, error) {
	p.observeCalls++
	return delivery.RouteProviderResult{Outcome: api.Observation_OBSERVATION_UNKNOWN, ProviderCommandID: r.ProviderCommandID}, nil
}

func confirmedProvider() *routeProviderForServe {
	return &routeProviderForServe{
		revision: "rev-1", evidencePrefix: "route-evidence://",
		fenceOutcome:    api.Observation_OBSERVATION_CONFIRMED,
		activateOutcome: api.Observation_OBSERVATION_CONFIRMED,
		rollbackOutcome: api.Observation_OBSERVATION_CONFIRMED,
	}
}

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

// routeFixture seeds one workspace with a delivered deployment whose runtime
// instance is ready, then binds the fixture provider.
func routeFixture(t *testing.T, provider *routeProviderForServe) (*delivery.Service, string, string, *sql.DB) {
	t.Helper()
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	service.Route = provider
	return service, tenant, "ws-route", db
}

func TestServeRouteFenceActivatesAndObservesSingleCurrent(t *testing.T) {
	provider := confirmedProvider()
	service, tenant, workspace, db := routeFixture(t, provider)
	seedObservationDeployment(t, db, workspace, tenant, "dep-route", "verifying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	if _, err := db.ExecContext(context.Background(), `UPDATE serve.agent_runtime_instances SET status='ready',access_url='https://ws-route.example/',readiness_evidence_ref='readiness://dep-route',observed_at=now() WHERE deployment_id=$1`, "dep-route"); err != nil {
		t.Fatal(err)
	}
	ctx := routeServeContext()

	fenced, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()})
	if err != nil || fenced.GetObservation() != api.Observation_OBSERVATION_CONFIRMED || fenced.GetCurrentGeneration() != 0 || fenced.GetAcceptedExecutionEpoch() != 1 {
		t.Fatalf("fence=%+v err=%v", fenced, err)
	}
	activated, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: exactPrecondition("rev-1"), TargetExecutionResourceId: "rt_dep-route", TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"})
	if err != nil || activated.GetObservation() != api.Observation_OBSERVATION_CONFIRMED || activated.GetCurrentGeneration() != 1 || activated.GetTargetExecutionResourceId() != "rt_dep-route" {
		t.Fatalf("activate=%+v err=%v", activated, err)
	}
	// Serve switched the exact confirmed provider revision and committed its own
	// single current route fact, and the deployment names the switch that owns it.
	var revision, switchID, deploymentSwitch string
	if err := db.QueryRowContext(ctx, `SELECT provider_revision,last_confirmed_switch_id FROM serve.access_bindings WHERE workspace_id=$1`, workspace).Scan(&revision, &switchID); err != nil {
		t.Fatal(err)
	}
	if revision != "rev-1" || switchID != activated.GetSwitchId() {
		t.Fatalf("binding revision=%q switch=%q want rev-1 and %q", revision, switchID, activated.GetSwitchId())
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
	if err != nil || observed.GetObservation() != api.Observation_OBSERVATION_UNKNOWN || observed.GetCurrentGeneration() != 1 {
		t.Fatalf("observe=%+v err=%v", observed, err)
	}

	// A second writer with the same expectation must not advance the generation
	// again: the CAS refuses and the route stays where the provider confirmed it.
	_, err = service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: exactPrecondition("rev-1"), TargetExecutionResourceId: "rt_dep-route", TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"})
	if status.Code(err) != codes.Aborted || !strings.Contains(err.Error(), delivery.ReasonRouteGenerationConflict) {
		t.Fatalf("stale generation activate err=%v", err)
	}
	if err := db.QueryRowContext(ctx, `SELECT route_generation FROM serve.access_bindings WHERE workspace_id=$1`, workspace).Scan(&revision); err != nil {
		t.Fatal(err)
	}
}

func TestServeRouteLostAckBlocksAndIsReadBack(t *testing.T) {
	provider := confirmedProvider()
	provider.failWith = status.Error(codes.Unavailable, "provider response lost")
	service, tenant, workspace, db := routeFixture(t, provider)
	seedObservationDeployment(t, db, workspace, tenant, "dep-route", "verifying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	ctx := routeServeContext()

	unknown, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()})
	if err != nil || unknown.GetObservation() != api.Observation_OBSERVATION_UNKNOWN {
		t.Fatalf("lost-ack fence=%+v err=%v", unknown, err)
	}
	var switchStatus string
	if err := db.QueryRowContext(ctx, `SELECT status FROM serve.access_switches WHERE id=$1`, unknown.GetSwitchId()).Scan(&switchStatus); err != nil {
		t.Fatal(err)
	}
	if switchStatus != "unknown" {
		t.Fatalf("switch after lost ack = %q, want unknown", switchStatus)
	}
	// An unresolved switch blocks every new switch for that binding.
	if _, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition(), TargetExecutionResourceId: "rt_dep-route", TargetDeploymentId: "dep-route"}); status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonRouteSwitchUnresolved) {
		t.Fatalf("activate behind unresolved switch err=%v", err)
	}
	// The original switch is read back by its own provider command identity, never
	// replaced by a new one.
	readback, err := service.ObserveRoute(ctx, &api.RouteObserveRequest{Context: routeCall(tenant), WorkspaceId: workspace, SwitchId: proto.String(unknown.GetSwitchId())})
	if err != nil || readback.GetObservation() != api.Observation_OBSERVATION_UNKNOWN || readback.GetProviderCommandId() != unknown.GetProviderCommandId() {
		t.Fatalf("readback=%+v err=%v", readback, err)
	}
	if provider.observeCalls != 0 {
		t.Fatalf("ObserveRoute must report Serve's own recorded fact, provider observe calls=%d", provider.observeCalls)
	}
}

func TestServeRouteRefusesWithoutProviderAndWithoutReadyTarget(t *testing.T) {
	service, tenant, workspace, db := routeFixture(t, confirmedProvider())
	seedObservationDeployment(t, db, workspace, tenant, "dep-route", "verifying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	ctx := routeServeContext()

	// A deployment whose runtime instance is not ready is never a route target.
	_, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition(), TargetExecutionResourceId: "rt_dep-route", TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), delivery.ReasonDeploymentNotReady) {
		t.Fatalf("unready target err=%v", err)
	}

	service.Route = nil
	_, err = service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()})
	if status.Code(err) != codes.Unavailable || !strings.Contains(err.Error(), delivery.ReasonRouteProviderUnavailable) {
		t.Fatalf("missing provider err=%v", err)
	}
	// The unconfirmed switch it committed to is recorded and blocks a new one.
	var status string
	if err := db.QueryRowContext(ctx, `SELECT status FROM serve.access_switches WHERE workspace_id=$1`, workspace).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "requested" {
		t.Fatalf("switch recorded without provider = %q, want requested", status)
	}
}

func TestServeRouteRefusesOlderEpochAndUnadmittedCaller(t *testing.T) {
	provider := confirmedProvider()
	service, tenant, workspace, db := routeFixture(t, provider)
	seedObservationDeployment(t, db, workspace, tenant, "dep-route", "verifying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	ctx := routeServeContext()
	if _, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 3, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()}); err != nil {
		t.Fatal(err)
	}
	// An older execution epoch may not fence or advance the binding: the accepted
	// epoch only moves forward.
	_, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 2, ExpectedRouteGeneration: 0, ProviderPrecondition: exactPrecondition("rev-1")})
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
	provider := confirmedProvider()
	service, tenant, workspace, db := routeFixture(t, provider)
	seedObservationDeployment(t, db, workspace, tenant, "dep-route", "verifying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	if _, err := db.ExecContext(context.Background(), `UPDATE serve.agent_runtime_instances SET status='ready',access_url='https://ws-route.example/',readiness_evidence_ref='readiness://dep-route',observed_at=now() WHERE deployment_id=$1`, "dep-route"); err != nil {
		t.Fatal(err)
	}
	ctx := routeServeContext()
	if _, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: absentPrecondition()}); err != nil {
		t.Fatal(err)
	}
	activated, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, ProviderPrecondition: exactPrecondition("rev-1"), TargetExecutionResourceId: "rt_dep-route", TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"})
	if err != nil {
		t.Fatal(err)
	}
	// A rollback to the original switch's predecessor target requires that switch
	// to be confirmed and a provider CAS; it advances the generation by one.
	rolledBack, err := service.RollbackRoute(ctx, &api.RouteRollbackCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", OriginalSwitchId: activated.GetSwitchId(), ExecutionEpoch: 2, ExpectedRouteGeneration: 1, ProviderPrecondition: exactPrecondition("rev-1"), TargetExecutionResourceId: "rt_original", TargetDeploymentId: "dep-route", CompatibilityReceiptId: "readiness://dep-route"})
	if err != nil || rolledBack.GetObservation() != api.Observation_OBSERVATION_CONFIRMED || rolledBack.GetCurrentGeneration() != 2 || rolledBack.GetTargetExecutionResourceId() != "rt_original" {
		t.Fatalf("rollback=%+v err=%v", rolledBack, err)
	}
	// An unknown original switch has no confirmed route to roll back from.
	_, err = service.RollbackRoute(ctx, &api.RouteRollbackCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", OriginalSwitchId: "rsw_missing", ExecutionEpoch: 2, ExpectedRouteGeneration: 2, ProviderPrecondition: exactPrecondition("rev-1"), TargetExecutionResourceId: "rt_original", TargetDeploymentId: "dep-route"})
	if status.Code(err) != codes.NotFound || !strings.Contains(err.Error(), delivery.ReasonRouteSwitchNotFound) {
		t.Fatalf("unknown original switch err=%v", err)
	}
}
