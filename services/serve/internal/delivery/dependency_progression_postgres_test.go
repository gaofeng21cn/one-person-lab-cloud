package delivery_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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
// Observe #1 is the post-start read and reports pending, which is exactly what
// Ensure returning ErrWorkspaceLaunchPending produces. Observe #2 is the first
// poll, and only then has the dependency become ready.
type lateDependencyRuntime struct {
	mu sync.Mutex

	starts      int
	observations int

	// dependencySettlesAfter is how many Start calls must happen before the
	// dependency counts as ready. The first apply filtered the main component
	// out, so the poll must be the thing that re-drives it.
	dependencySettlesAfter int
}

func (f *lateDependencyRuntime) Start(context.Context, *api.RuntimeDeployCommand, delivery.ExecutionTarget) (delivery.RuntimeObservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
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
	if f.observations <= 1 || f.starts <= f.dependencySettlesAfter {
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
	runtime := &lateDependencyRuntime{dependencySettlesAfter: 1}
	s.Runtime = runtime
	if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
		t.Fatal(err)
	}
	startsAfterDeploy, _ := runtime.snapshot()
	if startsAfterDeploy != 1 {
		t.Fatalf("deploy should apply exactly once, got %d", startsAfterDeploy)
	}

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
	converged := &lateDependencyRuntime{dependencySettlesAfter: 0}
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