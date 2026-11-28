package composition

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// exitingStarter starts one owner that exits at once, so the composition sees
// the owner refuse to run.
type exitingStarter struct {
	service string
	err     error
}

func (s *exitingStarter) Start(_ context.Context, process Process) (Child, error) {
	child := &exitingChild{exited: make(chan struct{}), err: s.err}
	close(child.exited)
	return child, nil
}

type exitingChild struct {
	exited chan struct{}
	err    error
}

func (c *exitingChild) Exited() <-chan struct{} { return c.exited }
func (c *exitingChild) Err() error                { return c.err }
func (c *exitingChild) Signal(os.Signal) error    { return nil }

// failingStarter refuses to start one owner.
type failingStarter struct {
	err error
}

func (s *failingStarter) Start(_ context.Context, _ Process) (Child, error) {
	return nil, s.err
}

// classifiedFailure reads the canonical code and owner out of a composition
// failure.
func classifiedFailure(t *testing.T, err error) (contracts.CompositionFailureCode, string) {
	t.Helper()
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("the composition failure must be classified, got %q", err.Error())
	}
	return failure.Code, failure.Owner
}

// TestComposerFailureCarriesTheCanonicalCode proves the failure an
// installation reads names the owner that refused and the reason in the
// canonical vocabulary, so it never has to parse a free-text message.
func TestComposerFailureCarriesTheCanonicalCode(t *testing.T) {
	plan := testPlan(t)
	readiness := &fakeReadiness{attempts: map[string]int{}, rule: func(owner string, _ int) bool {
		return owner != "ledger"
	}}
	composer := Composer{
		Plan:            plan,
		Checker:         readiness,
		Starter:         newFakeStarter(),
		Timeout:         100 * time.Millisecond,
		Interval:        time.Millisecond,
		ShutdownTimeout: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := composer.Run(ctx)
	if err == nil {
		t.Fatal("the composition must fail when a required owner never becomes ready")
	}
	code, owner := classifiedFailure(t, err)
	if code != contracts.CompositionFailureOwnerReadinessTimeout {
		t.Fatalf("the failure must carry the readiness timeout code, got %q", code)
	}
	if owner != "ledger" {
		t.Fatalf("the failure must name the owner that never became ready, got %q", owner)
	}
}

// TestComposerOwnerExitCarriesTheCanonicalCode proves an owner that exits
// before it becomes ready is reported with its own code and owner, which is
// the failure a composed installation hits when one owner refuses its
// configuration.
func TestComposerOwnerExitCarriesTheCanonicalCode(t *testing.T) {
	plan := testPlan(t)
	readiness := &fakeReadiness{attempts: map[string]int{}, rule: func(string, int) bool { return false }}
	composer := Composer{
		Plan:            plan,
		Checker:         readiness,
		Starter:         &exitingStarter{err: errors.New("exit status 1")},
		Timeout:         100 * time.Millisecond,
		Interval:        time.Millisecond,
		ShutdownTimeout: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := composer.Run(ctx)
	if err == nil {
		t.Fatal("the composition must fail when an owner exits before it becomes ready")
	}
	code, owner := classifiedFailure(t, err)
	if code != contracts.CompositionFailureOwnerStartRefused {
		t.Fatalf("the failure must carry the startup refusal code, got %q", code)
	}
	if owner != plan.Steps[0].Process.Service {
		t.Fatalf("the failure must name the owner that exited, got %q", owner)
	}
}

// TestComposerMissingCertificateCarriesTheCanonicalCode proves an
// installation that cannot provide the owner certificates is reported as its
// own reason, not as an owner that refused to start.
func TestComposerMissingCertificateCarriesTheCanonicalCode(t *testing.T) {
	plan := testPlan(t)
	readiness := &fakeReadiness{attempts: map[string]int{}, rule: func(string, int) bool { return true }}
	composer := Composer{
		Plan:            plan,
		Checker:         readiness,
		Starter:         &failingStarter{err: &MaterialError{Owner: "ledger", Err: errors.New("certificate material is not readable")}},
		Timeout:         100 * time.Millisecond,
		Interval:        time.Millisecond,
		ShutdownTimeout: 50 * time.Millisecond,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := composer.Run(ctx)
	if err == nil {
		t.Fatal("the composition must fail when an owner cannot start")
	}
	code, owner := classifiedFailure(t, err)
	if code != contracts.CompositionFailureOwnerCertificateMissing {
		t.Fatalf("the failure must carry the missing certificate code, got %q", code)
	}
	if owner != "ledger" {
		t.Fatalf("the failure must name the owner whose certificate is missing, got %q", owner)
	}
}

// TestCompositionFailureLineNamesTheFailure proves the line an installation
// reads names the same code and owner the failure carries.
func TestCompositionFailureLineNamesTheFailure(t *testing.T) {
	err := fail(contracts.CompositionFailureOwnerStartRefused, "workspace", fmt.Errorf("owner workspace exited before it became ready"))
	line := contracts.FormatCompositionFailure(mustFailure(t, err).Code, mustFailure(t, err).Owner)
	if line != "opl-cloud-composition: failure code=owner_startup_refused owner=workspace" {
		t.Fatalf("the canonical line must name the failure, got %q", line)
	}
}

func mustFailure(t *testing.T, err error) *Failure {
	t.Helper()
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("the composition failure must be classified, got %q", err.Error())
	}
	return failure
}
