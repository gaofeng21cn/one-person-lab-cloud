package composition

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"opl-cloud/packages/contracts/go/owneridentity"
)

// ModuleRunner executes one owner domain module in-process with its scoped environment.
type ModuleRunner func(ctx context.Context, env owneridentity.ModuleEnv) error

// InProcStarter launches each contract-declared owner module as an isolated
// goroutine inside the single Cloud backend process, projecting only the
// allowlisted environment variables declared for that owner and wiring
// owneridentity.InProcTransport for zero-copy internal RPCs.
type InProcStarter struct {
	// Modules maps each topology process service name to its domain module entrypoint.
	Modules map[string]ModuleRunner
	// InProc is shared across all in-process modules so owner-to-owner gRPC
	// traffic bypasses loopback TCP and TLS handshakes while still validating
	// unforgeable caller identity, bearer tokens, and CloudIdentity claims.
	InProc *owneridentity.InProcTransport
	// Environ supplies the host process environment; defaults to os.Environ.
	Environ func() []string
	// Getenv supplies the host environment lookup; defaults to os.Getenv.
	Getenv func(string) string
}

// Start projects the allowlisted environment for process and launches its
// registered ModuleRunner in its own goroutine.
func (s *InProcStarter) Start(_ context.Context, process Process) (Child, error) {
	runner, ok := s.Modules[process.Service]
	if !ok || runner == nil {
		return nil, fmt.Errorf("owner %s has no registered in-process module runner", process.Service)
	}
	getenv := s.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	environ := s.Environ
	if environ == nil {
		environ = os.Environ
	}
	projected, err := ChildEnvironment(process, environ(), getenv)
	if err != nil {
		return nil, err
	}
	values := make(map[string]string, len(projected))
	for _, entry := range projected {
		if name, value, found := strings.Cut(entry, "="); found && name != "" {
			values[name] = value
		}
	}
	moduleEnv := owneridentity.ModuleEnv{
		Getenv: func(key string) string {
			return values[key]
		},
		LookupEnv: func(key string) (string, bool) {
			value, exists := values[key]
			return value, exists
		},
		InProc: s.InProc,
	}
	childCtx, cancel := context.WithCancel(context.Background())
	child := &inprocChild{cancel: cancel, exited: make(chan struct{})}
	go func() {
		defer close(child.exited)
		defer func() {
			if recovered := recover(); recovered != nil {
				child.setErr(fmt.Errorf("owner %s panicked: %v", process.Service, recovered))
			}
		}()
		child.setErr(runner(childCtx, moduleEnv))
	}()
	return child, nil
}

type inprocChild struct {
	cancel context.CancelFunc
	exited chan struct{}
	mu     sync.Mutex
	err    error
}

func (c *inprocChild) setErr(err error) {
	c.mu.Lock()
	c.err = err
	c.mu.Unlock()
}

func (c *inprocChild) Exited() <-chan struct{} { return c.exited }

func (c *inprocChild) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *inprocChild) Signal(os.Signal) error {
	c.cancel()
	return nil
}
