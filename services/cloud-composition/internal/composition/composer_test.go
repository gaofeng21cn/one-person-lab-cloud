package composition

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeStarter records the owners the composition starts.
type fakeStarter struct {
	mu       sync.Mutex
	started  []string
	children map[string]*fakeChild
}

func newFakeStarter() *fakeStarter {
	return &fakeStarter{children: map[string]*fakeChild{}}
}

func (f *fakeStarter) Start(_ context.Context, process Process) (Child, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, process.Service)
	child := &fakeChild{exited: make(chan struct{})}
	f.children[process.Service] = child
	return child, nil
}

func (f *fakeStarter) startedOwners() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.started...)
}

// fakeChild is one supervised owner process that never exits on its own.
type fakeChild struct {
	exited  chan struct{}
	err     error
	mu      sync.Mutex
	signals []os.Signal
}

func (f *fakeChild) Exited() <-chan struct{} { return f.exited }

func (f *fakeChild) Err() error { return f.err }

func (f *fakeChild) Signal(signal os.Signal) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signals = append(f.signals, signal)
	return nil
}

func (f *fakeChild) receivedSignals() []os.Signal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]os.Signal{}, f.signals...)
}

// fakeReadiness answers a criterion from the test's own rule.
type fakeReadiness struct {
	mu       sync.Mutex
	attempts map[string]int
	rule     func(owner string, attempt int) bool
}

func (f *fakeReadiness) Check(_ context.Context, step Step) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	owner := step.Process.Service
	f.attempts[owner]++
	if f.rule(owner, f.attempts[owner]) {
		return nil
	}
	// The error names the owner and the criterion the same way the contract
	// driven checker does.
	check := step.Criterion.Checks[0]
	for _, candidate := range step.Criterion.Checks {
		if candidate.Role == "readiness" {
			check = candidate
			break
		}
	}
	return fmt.Errorf("owner %s is not ready: %s check %s failed", owner, check.Role, describeCheck(check))
}

// testPlan returns the composition plan of the shipped contract.
func testPlan(t *testing.T) Plan {
	t.Helper()
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("load the owner topology contract: %v", err)
	}
	plan, err := NewPlan(topology, environment{}.getenv)
	if err != nil {
		t.Fatalf("build the composition plan: %v", err)
	}
	return plan
}

// TestComposerStartsEveryOwnerInTheContractOrder proves that the composition
// starts the declared owners in the declared order and waits for each of them
// to be ready before it starts the next one.
func TestComposerStartsEveryOwnerInTheContractOrder(t *testing.T) {
	plan := testPlan(t)
	starter := newFakeStarter()
	readiness := &fakeReadiness{attempts: map[string]int{}, rule: func(string, int) bool { return true }}
	composer := Composer{
		Plan:            plan,
		Checker:         readiness,
		Starter:         starter,
		Timeout:         time.Second,
		Interval:        time.Millisecond,
		ShutdownTimeout: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- composer.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if len(starter.startedOwners()) == len(plan.Steps) {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("the composition started %v, it declares %d owners", starter.startedOwners(), len(plan.Steps))
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("a composition whose owners are ready must stop without a failure: %v", err)
	}
	started := starter.startedOwners()
	for index, step := range plan.Steps {
		if started[index] != step.Process.Service {
			t.Fatalf("owner %d is %s, the contract declares %s", index, started[index], step.Process.Service)
		}
	}
}

// TestComposerFailsFastNamingTheOwnerThatNeverBecomesReady proves that an
// owner that never becomes ready stops the composition with an error that
// names the owner, and that the owners after it are never started.
func TestComposerFailsFastNamingTheOwnerThatNeverBecomesReady(t *testing.T) {
	plan := testPlan(t)
	starter := newFakeStarter()
	readiness := &fakeReadiness{attempts: map[string]int{}, rule: func(owner string, _ int) bool {
		return owner != "ledger"
	}}
	composer := Composer{
		Plan:            plan,
		Checker:         readiness,
		Starter:         starter,
		Timeout:         100 * time.Millisecond,
		Interval:        time.Millisecond,
		ShutdownTimeout: 50 * time.Millisecond,
	}
	err := composer.Run(context.Background())
	if err == nil {
		t.Fatal("the composition must fail when a required owner never becomes ready")
	}
	if !strings.Contains(err.Error(), "ledger") {
		t.Fatalf("the failure must name the owner that never became ready, got %q", err.Error())
	}
	if !strings.Contains(err.Error(), "readyz") {
		t.Fatalf("the failure must name the criterion that did not pass, got %q", err.Error())
	}
	started := starter.startedOwners()
	for _, owner := range started {
		if owner == "fabric" || owner == "control-plane" || owner == "console-bff" {
			t.Fatalf("owner %s was started although the Ledger owner never became ready", owner)
		}
	}
	if len(started) == 0 || started[len(started)-1] != "ledger" {
		t.Fatalf("the composition started %v, it must start the owners up to the one that never became ready", started)
	}
}

// TestComposerDoesNotStartADependentOwnerBeforeItsDependencyIsReady proves
// that an owner whose dependency is not ready is never started.
func TestComposerDoesNotStartADependentOwnerBeforeItsDependencyIsReady(t *testing.T) {
	plan := testPlan(t)
	starter := newFakeStarter()
	// The Capability owner is ready for its own gate and not ready any more,
	// so the Build owner, which the contract makes depend on it, must not
	// start.
	readiness := &fakeReadiness{attempts: map[string]int{}, rule: func(owner string, attempt int) bool {
		if owner == "capability" {
			return attempt == 1
		}
		return true
	}}
	composer := Composer{
		Plan:            plan,
		Checker:         readiness,
		Starter:         starter,
		Timeout:         100 * time.Millisecond,
		Interval:        time.Millisecond,
		ShutdownTimeout: 50 * time.Millisecond,
	}
	err := composer.Run(context.Background())
	if err == nil {
		t.Fatal("the composition must fail when a dependency is not ready")
	}
	if !strings.Contains(err.Error(), "capability") {
		t.Fatalf("the failure must name the dependency that is not ready, got %q", err.Error())
	}
	for _, owner := range starter.startedOwners() {
		if owner != "capability" {
			t.Fatalf("owner %s was started although the Capability owner was not ready", owner)
		}
	}
}

// TestComposerStopsEveryOwnerWhenTheCompositionIsStopped proves that a stop
// signal reaches the owner processes.
func TestComposerStopsEveryOwnerWhenTheCompositionIsStopped(t *testing.T) {
	plan := testPlan(t)
	starter := newFakeStarter()
	readiness := &fakeReadiness{attempts: map[string]int{}, rule: func(string, int) bool { return true }}
	composer := Composer{
		Plan:            plan,
		Checker:         readiness,
		Starter:         starter,
		Timeout:         time.Second,
		Interval:        time.Millisecond,
		ShutdownTimeout: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- composer.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for len(starter.startedOwners()) < len(plan.Steps) {
		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("the composition started %v, it declares %d owners", starter.startedOwners(), len(plan.Steps))
		}
		time.Sleep(2 * time.Millisecond)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("a stopped composition must not fail: %v", err)
	}
	for _, owner := range starter.startedOwners() {
		child := starter.children[owner]
		signals := child.receivedSignals()
		if len(signals) == 0 {
			t.Fatalf("owner %s received no stop signal", owner)
		}
	}
}
