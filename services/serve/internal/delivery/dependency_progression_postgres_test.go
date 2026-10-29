package delivery_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/serve/internal/delivery"
)

// lateDependencyRuntime models the PD-01 shape exactly.
//
// The executor admits a component when it already exists OR its dependencies
// are ready at that moment, so a main component whose dependency is still
// converging is filtered out of the apply. The read path is read-only by
// contract. The deployment can therefore only reach ready if something
// re-drives the apply after the dependency settles.
//
// The first apply observes pending. The test then settles the dependency
// independently; only another apply can materialize the missing main component.
type lateDependencyRuntime struct {
	mu sync.Mutex

	starts          int
	observations    int
	dependencyReady bool
	mainApplied     bool
}

func (f *lateDependencyRuntime) Start(context.Context, *api.RuntimeDeployCommand, delivery.ExecutionTarget) (delivery.RuntimeObservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	if f.dependencyReady {
		f.mainApplied = true
	}
	return delivery.RuntimeObservation{}, nil
}

func (*lateDependencyRuntime) Lifecycle(context.Context, *api.RuntimeDeployCommand, delivery.ExecutionTarget, string) error {
	return nil
}

func (*lateDependencyRuntime) Reload(context.Context, *api.RuntimeDeployCommand, delivery.ExecutionTarget) (int64, error) {
	return 0, errors.New("reload is not part of this progression")
}

func (*lateDependencyRuntime) Credentials(context.Context, *api.RuntimeDeployCommand, delivery.ExecutionTarget) (*api.WorkspaceApplicationCredentials, error) {
	return nil, errors.New("credentials unavailable")
}

func (f *lateDependencyRuntime) Observe(context.Context, *api.RuntimeDeployCommand, delivery.ExecutionTarget) (delivery.RuntimeObservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.observations++
	if !f.mainApplied {
		return delivery.RuntimeObservation{
			State:      api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING,
			ObservedAt: time.Now().UTC(),
		}, nil
	}
	return runtimeReady(applicationEntry(), "https://ws.example/app", "late-dependency-ready"), nil
}

func (f *lateDependencyRuntime) snapshot() (int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.starts, f.observations
}

// A poll must re-drive the apply. Without it, ReadRuntime observes the same
// absent main component forever and the deployment never leaves pending.
func TestPollRedrivesApplySoALateDependencyCanStillConverge(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = &resourcesForServe{confirmed: true}
	// The deploy call applies once; the dependency is still not ready.
	runtime := &lateDependencyRuntime{}
	s.Runtime = runtime
	if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
		t.Fatal(err)
	}
	startsAfterDeploy, _ := runtime.snapshot()
	if startsAfterDeploy != 1 {
		t.Fatalf("deploy should apply exactly once, got %d", startsAfterDeploy)
	}
	// Dependency readiness is an external fact, independent of Start count.
	runtime.dependencyReady = true

	// First poll. The dependency is ready by now, but only a re-drive can apply
	// the main component that the first apply filtered out.
	request := &api.RuntimeReadbackRequest{
		Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId,
	}
	if _, err = s.ReadRuntime(ctx, request); err != nil {
		t.Fatal(err)
	}
	startsAfterPoll, observations := runtime.snapshot()
	if startsAfterPoll <= startsAfterDeploy {
		t.Fatalf("poll did not re-drive the apply: starts %d -> %d; "+
			"a main component filtered out by the first apply can never be applied by a read-only poll",
			startsAfterDeploy, startsAfterPoll)
	}
	if observations < 2 {
		t.Fatalf("poll must re-observe after re-driving, saw %d observations", observations)
	}

	// And the deployment actually converges rather than staying pending forever.
	var runtimeState, deploymentState string
	if err = s.DB.QueryRowContext(ctx,
		`SELECT i.status,d.status FROM serve.agent_runtime_instances i JOIN serve.agent_deployments d ON d.id=i.deployment_id WHERE i.id=$1`,
		reservation.RuntimeInstanceId).Scan(&runtimeState, &deploymentState); err != nil {
		t.Fatal(err)
	}
	if runtimeState != "ready" || deploymentState != "active" {
		t.Fatalf("states=%s/%s, want ready/active after the poll re-drove the apply", runtimeState, deploymentState)
	}
}

// A deployment that is already ready must not be re-driven: the poll stays a
// read for a converged workload.
func TestPollDoesNotRedriveAnAlreadyConvergedDeployment(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = &resourcesForServe{confirmed: true}
	s.Runtime = &runtimeForServe{}
	if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
		t.Fatal(err)
	}
	converged := &lateDependencyRuntime{dependencyReady: true, mainApplied: true}
	s.Runtime = converged
	// The deploy already applied it and already read it once; that read is what
	// this poll repeats, and it must come back ready.
	converged.starts, converged.observations = 1, 1
	request := &api.RuntimeReadbackRequest{
		Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId,
	}
	if _, err = s.ReadRuntime(ctx, request); err != nil {
		t.Fatal(err)
	}
	if starts, _ := converged.snapshot(); starts != 1 {
		t.Fatalf("a converged deployment must not be re-applied, starts=%d", starts)
	}
	if _, observations := converged.snapshot(); observations != 2 {
		t.Fatalf("the poll must observe exactly once, observations=%d", observations)
	}
}

// The provider can successfully report a failed or stopped workload while
// refusing mutations. A read must preserve that observation without Start.
type progressionBoundaryRuntime struct {
	runtimeForServe
	startErr     error
	lifecycleErr error
}

func (f *progressionBoundaryRuntime) Start(context.Context, *api.RuntimeDeployCommand, delivery.ExecutionTarget) (delivery.RuntimeObservation, error) {
	f.starts++
	return delivery.RuntimeObservation{}, f.startErr
}

func (f *progressionBoundaryRuntime) Observe(context.Context, *api.RuntimeDeployCommand, delivery.ExecutionTarget) (delivery.RuntimeObservation, error) {
	f.observes++
	return delivery.RuntimeObservation{State: f.state, ObservedAt: time.Now().UTC()}, nil
}

func (f *progressionBoundaryRuntime) Lifecycle(_ context.Context, _ *api.RuntimeDeployCommand, _ delivery.ExecutionTarget, desired string) error {
	f.lifecycle = append(f.lifecycle, desired)
	return f.lifecycleErr
}

func TestPollPreservesFailedAndTerminalObservations(t *testing.T) {
	for _, state := range []api.AgentRuntimeObservationState{
		api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED,
		api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STOPPED,
		api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED,
	} {
		t.Run(state.String(), func(t *testing.T) {
			s, r, _ := reservationFixture(t)
			ctx := workspaceContext()
			reservation, err := s.Reserve(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			s.Resources = &resourcesForServe{confirmed: true}
			s.Runtime = &lateDependencyRuntime{}
			if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
				t.Fatal(err)
			}
			runtime := &progressionBoundaryRuntime{
				runtimeForServe: runtimeForServe{state: state},
				startErr:        errors.New("provider mutation refused"),
			}
			s.Runtime = runtime
			readback, err := s.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId})
			if err != nil {
				t.Fatalf("readable observation was lost: %v", err)
			}
			if readback.GetState() != state {
				t.Fatalf("state=%v, want %v", readback.GetState(), state)
			}
			if runtime.starts != 0 || runtime.observes != 1 {
				t.Fatalf("starts=%d observes=%d", runtime.starts, runtime.observes)
			}
		})
	}
}

func TestPollDoesNotRestartAPreviouslyActivatedDelivery(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = &resourcesForServe{confirmed: true}
	s.Runtime = &runtimeForServe{}
	if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
		t.Fatal(err)
	}
	runtime := &progressionBoundaryRuntime{runtimeForServe: runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING}, startErr: errors.New("provider mutation refused")}
	s.Runtime = runtime
	request := &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId}
	// The first degraded read changes active to verifying. Prior activation must
	// still prevent the second read from treating this as a first delivery.
	for i := 0; i < 2; i++ {
		readback, err := s.ReadRuntime(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if readback.GetState() != runtime.state {
			t.Fatalf("state=%v", readback.GetState())
		}
	}
	if runtime.starts != 0 || runtime.observes != 2 {
		t.Fatalf("starts=%d observes=%d", runtime.starts, runtime.observes)
	}
}

func TestPollDoesNotOverrideRecordedSuspension(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		name := "unknown"
		if confirmed {
			name = "confirmed"
		}
		t.Run(name, func(t *testing.T) {
			s, r, _ := reservationFixture(t)
			ctx := workspaceContext()
			reservation, err := s.Reserve(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			s.Resources = &resourcesForServe{confirmed: true}
			s.Runtime = &lateDependencyRuntime{}
			if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
				t.Fatal(err)
			}
			// The real adapter maps a suspended provider's pending observation to
			// STARTING. The persisted stop intent is the mutation authority.
			runtime := &progressionBoundaryRuntime{runtimeForServe: runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING}, startErr: errors.New("provider mutation refused")}
			if !confirmed {
				runtime.lifecycleErr = errors.New("suspension response lost")
			}
			s.Runtime = runtime
			_, stopErr := s.StopRuntime(servePeerContext(), &api.RuntimeStopCommand{RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId})
			if (stopErr == nil) != confirmed {
				t.Fatalf("stopErr=%v confirmed=%v", stopErr, confirmed)
			}
			if !confirmed && !errors.Is(stopErr, runtime.lifecycleErr) {
				t.Fatalf("stop did not reach the lifecycle boundary: %v", stopErr)
			}
			if _, err = s.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId}); err != nil {
				t.Fatal(err)
			}
			if runtime.starts != 0 {
				t.Fatalf("recorded suspension triggered %d starts", runtime.starts)
			}
			if len(runtime.lifecycle) != 1 || runtime.lifecycle[0] != "suspended" {
				t.Fatalf("lifecycle=%v", runtime.lifecycle)
			}
		})
	}
}

func TestPollHonorsWorkspaceRetirement(t *testing.T) {
	for _, confirmed := range []bool{false, true} {
		name := "unknown"
		if confirmed {
			name = "confirmed"
		}
		t.Run(name, func(t *testing.T) {
			s, r, _ := reservationFixture(t)
			ctx := workspaceContext()
			reservation, err := s.Reserve(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			s.Resources = &resourcesForServe{confirmed: true}
			s.Runtime = &lateDependencyRuntime{}
			if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
				t.Fatal(err)
			}
			runtime := &progressionBoundaryRuntime{
				runtimeForServe: runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING},
				startErr:        errors.New("a retiring runtime must not be restarted"),
			}
			if !confirmed {
				runtime.lifecycleErr = errors.New("retirement response lost")
			}
			s.Runtime = runtime
			command := &api.RuntimeStopCommand{Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId, RetainedDataAttachmentId: r.GetDataAttachmentId()}
			retired, retireErr := s.Retire(ctx, command)
			if confirmed && retireErr != nil {
				t.Fatalf("retirement did not reach its lifecycle boundary: %v", retireErr)
			}
			if !confirmed && !errors.Is(retireErr, runtime.lifecycleErr) {
				t.Fatalf("retirement lost its provider error: %v", retireErr)
			}
			var action, result, retained string
			if err = s.DB.QueryRowContext(ctx, `SELECT action,observation_result,input_snapshot->>'retainedDataAttachmentId' FROM serve.agent_runtime_actions WHERE runtime_instance_id=$1 AND action='terminate'`, reservation.RuntimeInstanceId).Scan(&action, &result, &retained); err != nil {
				t.Fatal(err)
			}
			wantResult := "unknown"
			if confirmed {
				wantResult = "confirmed"
			}
			if result != wantResult || retained != command.RetainedDataAttachmentId {
				t.Fatalf("retirement action=%s result=%s retained=%s", action, result, retained)
			}
			request := &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId}
			for poll := 0; poll < 2; poll++ {
				if _, err = s.ReadRuntime(ctx, request); err != nil {
					t.Fatalf("poll=%d: %v", poll, err)
				}
			}
			if runtime.starts != 0 || len(runtime.lifecycle) != 1 || runtime.lifecycle[0] != "absent" {
				t.Fatalf("starts=%d lifecycle=%v", runtime.starts, runtime.lifecycle)
			}
			// An unknown outcome can be retried; a confirmed one reuses its result.
			runtime.lifecycleErr = nil
			replayed, err := s.Retire(ctx, command)
			if err != nil || replayed.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_RETIRE || replayed.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || replayed.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
				t.Fatalf("retirement=%v err=%v", replayed, err)
			}
			if confirmed && replayed.GetOperationId() != retired.GetOperationId() {
				t.Fatal("confirmed retirement created a different operation")
			}
			calls := len(runtime.lifecycle)
			if _, err = s.Retire(ctx, command); err != nil || len(runtime.lifecycle) != calls {
				t.Fatalf("confirmed retirement reissued lifecycle: calls=%v err=%v", runtime.lifecycle, err)
			}
			wantCalls := 1
			if !confirmed {
				wantCalls = 2
			}
			if calls != wantCalls {
				t.Fatalf("lifecycle calls=%d want=%d", calls, wantCalls)
			}
		})
	}
}

func TestPollRefusesToReapplyAfterTheProviderReportedFailure(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = &resourcesForServe{confirmed: true}
	runtime := &progressionBoundaryRuntime{runtimeForServe: runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED}}
	s.Runtime = runtime
	if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
		t.Fatal(err)
	}
	runtime.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING
	runtime.startErr = errors.New("provider mutation refused")
	for poll := 0; poll < 2; poll++ {
		if _, err = s.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId}); err != nil {
			t.Fatalf("poll=%d: %v", poll, err)
		}
	}
	if runtime.starts != 1 {
		t.Fatalf("failed delivery was reapplied: starts=%d", runtime.starts)
	}
}

func TestPollDoesNotRecreateARetiredReplacement(t *testing.T) {
	s, r, original, version, replacement := exclusiveSwitchFixture(t)
	replacement.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED
	if _, err := switchVersion(t, s, "failed-switch-poll", version.GetId(), original.DeploymentId); err != nil {
		t.Fatal(err)
	}
	var deploymentID, runtimeID string
	if err := s.DB.QueryRow(`SELECT id,runtime_instance_id FROM serve.agent_deployments WHERE workspace_id=$1 AND execution_epoch=2`, r.WorkspaceId).Scan(&deploymentID, &runtimeID); err != nil {
		t.Fatal(err)
	}
	runtime := &progressionBoundaryRuntime{
		runtimeForServe: runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED},
		startErr:        errors.New("a retired writer must not be recreated"),
	}
	s.Runtime = runtime
	readback, err := s.ReadRuntime(workspaceContext(), &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: runtimeID, DeploymentId: deploymentID})
	if err != nil || readback.GetState() != runtime.state {
		t.Fatalf("retired readback=%v err=%v", readback, err)
	}
	if runtime.starts != 0 {
		t.Fatalf("retired replacement was started %d times", runtime.starts)
	}
	if id, state, epoch, _ := currentDeploymentFacts(t, s, r.WorkspaceId); id != original.DeploymentId || state != "active" || epoch != 1 {
		t.Fatalf("restored writer changed: id=%s state=%s epoch=%d", id, state, epoch)
	}
	if generation, target := routeBinding(t, s, r.WorkspaceId); generation != 1 || target != original.DeploymentId {
		t.Fatalf("restored route changed: generation=%d target=%s", generation, target)
	}
}

func TestExplicitDeployCanResumeDependencyProgressionAfterFailure(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = &resourcesForServe{confirmed: true}
	s.Runtime = &progressionBoundaryRuntime{runtimeForServe: runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED}}
	command := deployReserved(r, reservation)
	if _, err = s.Deploy(ctx, command); err != nil {
		t.Fatal(err)
	}
	// A new authorized Deploy of the same frozen command is distinct from
	// read-only polling. Its new observation permits subsequent progression.
	runtime := &lateDependencyRuntime{}
	s.Runtime = runtime
	if _, err = s.Deploy(ctx, command); err != nil {
		t.Fatal(err)
	}
	runtime.dependencyReady = true
	readback, err := s.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId})
	if err != nil || readback.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
		t.Fatalf("resumed readback=%v err=%v", readback, err)
	}
	if starts, _ := runtime.snapshot(); starts != 2 {
		t.Fatalf("resumed starts=%d want=2", starts)
	}
}

func TestPollRetainsATerminalFactAlreadyRecordedByTheOwner(t *testing.T) {
	for _, state := range []api.AgentRuntimeObservationState{
		api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED,
		api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STOPPED,
		api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATING,
		api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED,
	} {
		t.Run(state.String(), func(t *testing.T) {
			s, r, _ := reservationFixture(t)
			ctx := workspaceContext()
			reservation, err := s.Reserve(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			s.Resources = &resourcesForServe{confirmed: true}
			s.Runtime = &lateDependencyRuntime{}
			command := deployReserved(r, reservation)
			if _, err = s.Deploy(ctx, command); err != nil {
				t.Fatal(err)
			}
			// Use the existing observation owner entry, which records runtime
			// state independently of the first-delivery action outcome.
			if _, err = s.RecordDeploymentObservation(ctx, command, delivery.RuntimeObservation{State: state, ObservedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			runtime := &progressionBoundaryRuntime{runtimeForServe: runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING}, startErr: errors.New("provider mutation refused")}
			s.Runtime = runtime
			for poll := 0; poll < 2; poll++ {
				if _, err = s.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId}); err != nil {
					t.Fatalf("poll=%d: %v", poll, err)
				}
			}
			if runtime.starts != 0 {
				t.Fatalf("terminal owner fact caused %d starts", runtime.starts)
			}
		})
	}
}

func TestDeployReplayDoesNotStartPreviouslyFulfilledRuntime(t *testing.T) {
	for _, degraded := range []bool{false, true} {
		name := "active"
		if degraded {
			name = "degraded after activation"
		}
		t.Run(name, func(t *testing.T) {
			s, r, _ := reservationFixture(t)
			ctx := workspaceContext()
			reservation, err := s.Reserve(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			s.Resources = &resourcesForServe{confirmed: true}
			s.Runtime = &runtimeForServe{}
			command := deployReserved(r, reservation)
			original, err := s.Deploy(ctx, command)
			if err != nil {
				t.Fatal(err)
			}
			if original.GetFirstFulfilledAt() == nil {
				t.Fatal("first delivery not fulfilled")
			}
			runtime := &progressionBoundaryRuntime{runtimeForServe: runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING}, startErr: errors.New("must not start a fulfilled delivery")}
			s.Runtime = runtime
			prior := original
			if degraded {
				prior, err = s.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId})
				if err != nil {
					t.Fatal(err)
				}
			}
			var stateBefore string
			if err = s.DB.QueryRowContext(ctx, `SELECT status FROM serve.agent_deployments WHERE id=$1`, command.DeploymentId).Scan(&stateBefore); err != nil {
				t.Fatal(err)
			}
			observesBefore := runtime.observes
			replayed, err := s.Deploy(ctx, command)
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(replayed, prior) {
				t.Fatalf("replay changed original readback: before=%v after=%v", prior, replayed)
			}
			if runtime.starts != 0 || runtime.observes != observesBefore {
				t.Fatalf("fulfilled replay touched runtime: starts=%d observes=%d->%d", runtime.starts, observesBefore, runtime.observes)
			}
			var stateAfter string
			if err = s.DB.QueryRowContext(ctx, `SELECT status FROM serve.agent_deployments WHERE id=$1`, command.DeploymentId).Scan(&stateAfter); err != nil {
				t.Fatal(err)
			}
			if stateAfter != stateBefore {
				t.Fatalf("fulfilled deployment status changed: %s -> %s", stateBefore, stateAfter)
			}
		})
	}
}
