package composition

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// Starter starts one owner process.
type Starter interface {
	Start(ctx context.Context, process Process) (Child, error)
}

// Child is one supervised owner process.
type Child interface {
	// Exited is closed once the process has exited.
	Exited() <-chan struct{}
	// Err reports how the process exited.
	Err() error
	// Signal delivers a signal to the process.
	Signal(signal os.Signal) error
}

// Readiness verifies one process against the readiness criterion the contract
// declares for it.
type Readiness interface {
	Check(ctx context.Context, step Step) error
}

// Composer brings the declared owners up in the contract's order and holds
// each one back until it is ready.
type Composer struct {
	Plan    Plan
	Checker Readiness
	Starter Starter

	// Timeout is how long one owner may take to become ready.
	Timeout time.Duration
	// Interval is how often a not-ready owner is probed again.
	Interval time.Duration
	// ShutdownTimeout is how long a stopped owner may take to exit before it
	// is killed.
	ShutdownTimeout time.Duration

	Logf func(format string, args ...any)
}

// running pairs one started owner with the step it was started for.
type running struct {
	step  Step
	child Child
}

// Run starts every declared owner in the contract's order and returns when
// the composition stops: a signal arrives, or an owner exits.
func (c *Composer) Run(ctx context.Context) error {
	started := []running{}
	defer func() { c.shutdown(started) }()
	for _, step := range c.Plan.Steps {
		for _, dependency := range step.Dependencies {
			if err := c.awaitDependency(ctx, step, dependency); err != nil {
				return err
			}
		}
		c.logf("starting owner %s", step.Process.Service)
		child, err := c.Starter.Start(ctx, step.Process)
		if err != nil {
			return err
		}
		started = append(started, running{step: step, child: child})
		if err := c.awaitReady(ctx, step, child); err != nil {
			return err
		}
		c.logf("owner %s is ready", step.Process.Service)
	}
	return c.supervise(ctx, started)
}

// awaitDependency refuses to start an owner whose dependency is not ready.
// The dependency is checked again here, because a dependency that became
// unready after its own gate must not be silently used.
func (c *Composer) awaitDependency(ctx context.Context, step Step, dependency Step) error {
	if err := c.Checker.Check(ctx, dependency); err != nil {
		return fmt.Errorf("owner %s cannot start: its dependency %s is not ready: %w", step.Process.Service, dependency.Process.Service, err)
	}
	return nil
}

// awaitReady waits until the owner satisfies its declared criterion, and fails
// with the owner and the criterion that did not pass.
func (c *Composer) awaitReady(ctx context.Context, step Step, child Child) error {
	deadline := time.Now().Add(c.timeout())
	var last error
	for {
		err := c.Checker.Check(ctx, step)
		if err == nil {
			return nil
		}
		last = err
		select {
		case <-ctx.Done():
			return fmt.Errorf("owner %s did not become ready before the composition stopped: %w", step.Process.Service, ctx.Err())
		case <-child.Exited():
			return fmt.Errorf("owner %s exited before it became ready: %w", step.Process.Service, child.Err())
		case <-time.After(c.interval()):
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("owner %s did not become ready within %s: %w", step.Process.Service, c.timeout(), last)
		}
	}
}

// supervise stays up until the composition is stopped or an owner exits.
func (c *Composer) supervise(ctx context.Context, started []running) error {
	exits := make(chan running, len(started))
	for _, entry := range started {
		go func(entry running) {
			<-entry.child.Exited()
			exits <- entry
		}(entry)
	}
	select {
	case <-ctx.Done():
		return nil
	case entry := <-exits:
		return fmt.Errorf("owner %s exited while the composition was running: %w", entry.step.Process.Service, entry.child.Err())
	}
}

// shutdown stops the started owners gracefully. The owners are stopped in the
// reverse of the order they were started, and an owner that does not exit in
// time is killed.
func (c *Composer) shutdown(started []running) {
	for index := len(started) - 1; index >= 0; index-- {
		if err := started[index].child.Signal(syscall.SIGTERM); err != nil {
			c.logf("owner %s did not accept a stop signal: %v", started[index].step.Process.Service, err)
		}
	}
	deadline := time.Now().Add(c.shutdownTimeout())
	for _, entry := range started {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		select {
		case <-entry.child.Exited():
		case <-time.After(remaining):
		}
	}
	for _, entry := range started {
		select {
		case <-entry.child.Exited():
		default:
			_ = entry.child.Signal(syscall.SIGKILL)
		}
	}
}

func (c *Composer) timeout() time.Duration {
	if c.Timeout > 0 {
		return c.Timeout
	}
	return 2 * time.Minute
}

func (c *Composer) interval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return 500 * time.Millisecond
}

func (c *Composer) shutdownTimeout() time.Duration {
	if c.ShutdownTimeout > 0 {
		return c.ShutdownTimeout
	}
	return 30 * time.Second
}

func (c *Composer) logf(format string, args ...any) {
	if c.Logf == nil {
		return
	}
	c.Logf(format, args...)
}

// ExecStarter starts the owner binaries the contract declares.
type ExecStarter struct {
	// Environ is the installation's environment. Every owner process starts
	// from it.
	Environ func() []string
	// Getenv reads the installation's environment.
	Getenv func(string) string
	// Stdout and Stderr receive the owner processes' own output.
	Stdout io.Writer
	Stderr io.Writer
}

// Start runs one owner binary with the arguments the contract declares.
func (s *ExecStarter) Start(ctx context.Context, process Process) (Child, error) {
	getenv := s.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	environ := s.Environ
	if environ == nil {
		environ = os.Environ
	}
	environment, err := ChildEnvironment(process, environ(), getenv)
	if err != nil {
		return nil, err
	}
	command := exec.Command(process.Binary, process.DaemonArgs...)
	command.Env = environment
	command.Stdout = s.Stdout
	command.Stderr = s.Stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("owner %s did not start: %w", process.Service, err)
	}
	child := &execChild{command: command, exited: make(chan struct{})}
	go func() {
		child.err = command.Wait()
		close(child.exited)
	}()
	return child, nil
}

// execChild is one started owner process.
type execChild struct {
	command *exec.Cmd
	exited  chan struct{}
	err     error
}

func (c *execChild) Exited() <-chan struct{} { return c.exited }

func (c *execChild) Err() error { return c.err }

func (c *execChild) Signal(signal os.Signal) error {
	if c.command.Process == nil {
		return nil
	}
	return c.command.Process.Signal(signal)
}
