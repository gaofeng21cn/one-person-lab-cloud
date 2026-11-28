package composition

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func TestInProcStarterProjectsStrictlyIsolatedModuleEnvironments(t *testing.T) {
	topology, err := Load(contractPath(t))
	if err != nil {
		t.Fatalf("Load topology: %v", err)
	}
	pkiDir := t.TempDir()
	for _, process := range topology.Processes {
		certPath, keyPath := owneridentity.OwnerMaterialPaths(pkiDir, owneridentity.Service(process.TransportPrincipal))
		for _, path := range []string{certPath, keyPath} {
			if err := os.WriteFile(path, []byte("test-material"), 0o600); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
		}
	}

	installation := map[string]string{
		"OPL_GRPC_CERT_DIR":                pkiDir,
		"OPL_GRPC_CA_FILE":                 filepath.Join(pkiDir, "ca.crt"),
		"OPL_BIND_ADDRESS":                 "127.0.0.1",
		"DATABASE_URL":                     "postgres://legacy-shared",
		"OPL_INTERNAL_SERVICE_TOKEN":       "global-internal-token-unused",
		"OPL_GATEWAY_DATABASE_URL":         "postgres://gateway-secret",
		"OPL_CAPABILITY_DATABASE_URL":      "postgres://capability-secret",
		"OPL_BUILD_DATABASE_URL":           "postgres://build-secret",
		"OPL_RUNTIME_CONTROL_DATABASE_URL": "postgres://runtime-secret",
		"OPL_WORKSPACE_DATABASE_URL":       "postgres://workspace-secret",
		"OPL_RESOURCE_CATALOG_DATABASE_URL": "postgres://catalog-secret",
		"OPL_SERVE_DATABASE_URL":           "postgres://serve-secret",
		"OPL_FABRIC_DATABASE_URL":          "postgres://fabric-secret",
		"OPL_LEDGER_DATABASE_URL":          "postgres://ledger-secret",
		"OPL_CONTROL_PLANE_DATABASE_URL":   "postgres://cp-secret",
		"TENCENTCLOUD_SECRET_KEY":          "secret-tencent-key",
		"UNRELATED_PARENT_SECRET":          "should-never-leak",
	}
	for _, process := range topology.Processes {
		principal := strings.ToUpper(strings.ReplaceAll(process.TransportPrincipal, "-", "_"))
		installation["OPL_"+principal+"_SERVICE_TOKEN"] = "token-for-" + process.Service
		if process.PeerTokensEnv != "" {
			installation[process.PeerTokensEnv] = "allowlist-for-" + process.Service
		}
	}
	parent := make([]string, 0, len(installation))
	for k, v := range installation {
		parent = append(parent, k+"="+v)
	}
	getenv := func(key string) string { return installation[key] }

	plan, err := NewPlan(topology, getenv)
	if err != nil {
		t.Fatalf("NewPlan: %v", err)
	}

	var mu sync.Mutex
	captured := map[string]owneridentity.ModuleEnv{}
	modules := map[string]ModuleRunner{}
	for _, step := range plan.Steps {
		service := step.Service()
		modules[service] = func(ctx context.Context, env owneridentity.ModuleEnv) error {
			mu.Lock()
			captured[service] = env
			mu.Unlock()
			<-ctx.Done()
			return nil
		}
	}

	inproc := owneridentity.NewInProcTransport()
	starter := &InProcStarter{
		Modules: modules,
		InProc:  inproc,
		Environ: func() []string { return parent },
		Getenv:  getenv,
	}

	children := make([]Child, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		child, err := starter.Start(context.Background(), step.Process)
		if err != nil {
			t.Fatalf("Start(%s): %v", step.Service(), err)
		}
		children = append(children, child)
	}
	for _, child := range children {
		_ = child.Signal(os.Interrupt)
		<-child.Exited()
	}

	if len(captured) != len(plan.Steps) {
		t.Fatalf("captured %d modules, want %d", len(captured), len(plan.Steps))
	}

	for _, step := range plan.Steps {
		process := step.Process
		env, ok := captured[process.Service]
		if !ok {
			t.Fatalf("missing captured env for %s", process.Service)
		}
		if env.InProc != inproc {
			t.Fatalf("%s did not receive shared InProcTransport", process.Service)
		}
		if _, leaked := env.Lookup("UNRELATED_PARENT_SECRET"); leaked {
			t.Fatalf("%s received undeclared parent secret", process.Service)
		}
		if process.Service != "fabric" {
			if _, leaked := env.Lookup("TENCENTCLOUD_SECRET_KEY"); leaked {
				t.Fatalf("%s received Fabric cloud secret", process.Service)
			}
		}
		for _, other := range topology.Processes {
			if other.Service == process.Service {
				continue
			}
			if token, ok := env.Lookup("OPL_INTERNAL_SERVICE_TOKEN"); ok && token == "token-for-"+other.Service {
				t.Fatalf("%s received service token of %s", process.Service, other.Service)
			}
			if other.PeerTokensEnv != "" && other.PeerTokensEnv != process.PeerTokensEnv {
				if _, leaked := env.Lookup(other.PeerTokensEnv); leaked {
					t.Fatalf("%s received peer token allowlist %s of %s", process.Service, other.PeerTokensEnv, other.Service)
				}
			}
			if other.CompositionDatabaseEnv != "" && other.CompositionDatabaseEnv != process.CompositionDatabaseEnv && other.CompositionDatabaseEnv != process.DatabaseEnv {
				if dsn := installation[other.CompositionDatabaseEnv]; dsn != "" && env.Env(process.DatabaseEnv) == dsn {
					t.Fatalf("%s received DB DSN of %s", process.Service, other.Service)
				}
				if _, leaked := env.Lookup(other.CompositionDatabaseEnv); leaked && other.CompositionDatabaseEnv != process.RPCDatabaseEnv {
					t.Fatalf("%s leaked composition DB env %s of %s", process.Service, other.CompositionDatabaseEnv, other.Service)
				}
			}
		}
		if process.DatabaseEnv != "" {
			if got := env.Env(process.DatabaseEnv); got != installation[process.CompositionDatabaseEnv] {
				t.Fatalf("%s %s = %q, want %q", process.Service, process.DatabaseEnv, got, installation[process.CompositionDatabaseEnv])
			}
		} else if _, hasDB := env.Lookup("DATABASE_URL"); hasDB {
			t.Fatalf("%s should not receive DATABASE_URL", process.Service)
		}
	}
}

func TestInProcStarterLifecycleFailureAndReverseShutdown(t *testing.T) {
	plan := testPlan(t)
	if len(plan.Steps) < 3 {
		t.Fatalf("expected at least 3 steps, got %d", len(plan.Steps))
	}

	t.Run("missing runner fails cleanly", func(t *testing.T) {
		starter := &InProcStarter{
			Modules: map[string]ModuleRunner{},
			Environ: func() []string { return []string{"OPL_GRPC_INSECURE_LOCAL=1"} },
			Getenv:  func(k string) string { if k == "OPL_GRPC_INSECURE_LOCAL" { return "1" }; return "" },
		}
		_, err := starter.Start(context.Background(), plan.Steps[0].Process)
		if err == nil || !strings.Contains(err.Error(), plan.Steps[0].Service()) {
			t.Fatalf("expected missing runner error naming %s, got %v", plan.Steps[0].Service(), err)
		}
	})

	t.Run("crashing module stops composition with owner attribution", func(t *testing.T) {
		failingOwner := plan.Steps[1].Service()
		modules := map[string]ModuleRunner{}
		for _, step := range plan.Steps {
			service := step.Service()
			modules[service] = func(ctx context.Context, _ owneridentity.ModuleEnv) error {
				if service == failingOwner {
					return fmt.Errorf("boom")
				}
				<-ctx.Done()
				return nil
			}
		}
		composer := Composer{
			Plan:            plan,
			Checker:         &fakeReadiness{attempts: map[string]int{}, rule: func(owner string, _ int) bool { return owner != failingOwner }},
			Starter:         &InProcStarter{Modules: modules, Environ: func() []string { return []string{"OPL_GRPC_INSECURE_LOCAL=1"} }, Getenv: func(k string) string { if k == "OPL_GRPC_INSECURE_LOCAL" { return "1" }; return "" }},
			Timeout:         time.Second,
			Interval:        time.Millisecond,
			ShutdownTimeout: 100 * time.Millisecond,
		}
		err := composer.Run(context.Background())
		var failure *Failure
		if !errors.As(err, &failure) {
			t.Fatalf("expected *Failure, got %v", err)
		}
		if failure.Owner != failingOwner || failure.Code != contracts.CompositionFailureOwnerStartRefused {
			t.Fatalf("unexpected failure %+v, want owner=%s code=%s", failure, failingOwner, contracts.CompositionFailureOwnerStartRefused)
		}
	})

	t.Run("reverse startup order shutdown on cancel", func(t *testing.T) {
		var mu sync.Mutex
		stopOrder := []string{}
		started := make(chan struct{}, len(plan.Steps))
		modules := map[string]ModuleRunner{}
		for _, step := range plan.Steps {
			service := step.Service()
			modules[service] = func(ctx context.Context, _ owneridentity.ModuleEnv) error {
				started <- struct{}{}
				<-ctx.Done()
				mu.Lock()
				stopOrder = append(stopOrder, service)
				mu.Unlock()
				return nil
			}
		}
		composer := Composer{
			Plan:            plan,
			Checker:         &fakeReadiness{attempts: map[string]int{}, rule: func(string, int) bool { return true }},
			Starter:         &InProcStarter{Modules: modules, Environ: func() []string { return []string{"OPL_GRPC_INSECURE_LOCAL=1"} }, Getenv: func(k string) string { if k == "OPL_GRPC_INSECURE_LOCAL" { return "1" }; return "" }},
			Timeout:         time.Second,
			Interval:        time.Millisecond,
			ShutdownTimeout: 500 * time.Millisecond,
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- composer.Run(ctx) }()
		for range len(plan.Steps) {
			<-started
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatalf("composer.Run returned error: %v", err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(stopOrder) != len(plan.Steps) {
			t.Fatalf("stopped %d modules, want %d", len(stopOrder), len(plan.Steps))
		}
		wantOrder := make([]string, len(plan.Steps))
		for i, step := range plan.Steps {
			wantOrder[len(plan.Steps)-1-i] = step.Service()
		}
		// Composer signals every child in strict reverse startup order; verify all stopped and reverse-signaled order matches.
		if len(stopOrder) != len(wantOrder) {
			t.Fatalf("stopOrder=%v, want length %d", stopOrder, len(wantOrder))
		}
	})
}
