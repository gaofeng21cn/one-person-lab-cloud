package fabric

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

type applicationExecProbeRunner struct {
	base    *applicationRuntimeDockerRunner
	outputs []string
	err     error
	calls   [][]string
	bounded bool
}

func (r *applicationExecProbeRunner) Run(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	if args[0] != "exec" {
		return r.base.Run(ctx, stdin, args...)
	}
	r.calls = append(r.calls, append([]string(nil), args...))
	deadline, ok := ctx.Deadline()
	r.bounded = ok && time.Until(deadline) <= 30*time.Second
	if r.err != nil {
		return []byte("secret-command-output"), r.err
	}
	if len(r.outputs) == 0 {
		return nil, nil
	}
	output := r.outputs[0]
	r.outputs = r.outputs[1:]
	return []byte(output), nil
}
func applicationExecProbeFixture(t *testing.T) (*LocalDockerProvider, *applicationExecProbeRunner, WorkspaceApplicationRuntimeInput, dockerContainerInspect) {
	t.Helper()
	p, base, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	runner := &applicationExecProbeRunner{base: base, outputs: []string{"opl-health-exit:0\n"}}
	p.runner = runner
	input := applicationRuntimeInput("exec-health", applicationRevisionForTest())
	var container dockerContainerInspect
	container.ID = "declared-component-id"
	container.State.StartedAt = time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
	container.State.Running = true
	return p, runner, input, container
}
func shellHealthCheck() contracts.WorkspaceApplicationDependencyHealthCheck {
	return contracts.WorkspaceApplicationDependencyHealthCheck{Type: "exec", Command: []string{"/bin/sh", "-c", `check-auth --password "$PASSWORD"`}}
}

func TestLocalDockerApplicationExecProbeReadinessAndRedaction(t *testing.T) {
	for _, mode := range []string{"ready", "unready", "transport", "canceled", "deadline", "empty", "extra", "invalid-code", "leading-zero"} {
		t.Run(mode, func(t *testing.T) {
			p, runner, input, container := applicationExecProbeFixture(t)
			ctx := context.Background()
			wantReady := false
			wantError := true
			switch mode {
			case "ready":
				wantReady = true
				wantError = false
			case "unready":
				runner.outputs = []string{"opl-health-exit:7\n"}
				wantError = false
			case "transport":
				runner.err = errors.New("daemon unavailable: secret-command-output")
			case "canceled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			case "deadline":
				var cancel context.CancelFunc
				ctx, cancel = context.WithDeadline(ctx, time.Now().Add(-time.Second))
				defer cancel()
			case "empty":
				runner.outputs = nil
			case "extra":
				runner.outputs = []string{"secret-command-output\nopl-health-exit:0\n"}
			case "invalid-code":
				runner.outputs = []string{"opl-health-exit:256\n"}
			case "leading-zero":
				runner.outputs = []string{"opl-health-exit:00\n"}
			}
			ready, err := p.probeLocalDockerDependencyChecks(ctx, input, container, []contracts.WorkspaceApplicationDependencyHealthCheck{shellHealthCheck()})
			if ready != wantReady || (err != nil) != wantError {
				t.Fatalf("ready=%v err=%v", ready, err)
			}
			if err != nil && strings.Contains(err.Error(), "secret-command-output") {
				t.Fatal("probe output leaked")
			}
			if mode == "canceled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation hidden")
			}
			if mode == "deadline" && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal("deadline hidden")
			}
			if !runner.bounded || len(runner.calls) != 1 {
				t.Fatal("exec check not bounded")
			}
			args := runner.calls[0]
			if len(args) != 9 || args[0] != "exec" || args[1] != container.ID || args[2] != "/bin/sh" || args[4] != localDockerApplicationExecProbeScript || args[6] != "/bin/sh" || args[7] != "-c" || args[8] != shellHealthCheck().Command[2] {
				t.Fatalf("command structure invalid: %v", args)
			}
			if len(runner.base.probes) != 0 {
				t.Fatal("exec sent to network runner")
			}
		})
	}
}
func TestLocalDockerApplicationExecProbeAllChecksAndDelay(t *testing.T) {
	p, runner, input, container := applicationExecProbeFixture(t)
	checks := []contracts.WorkspaceApplicationDependencyHealthCheck{shellHealthCheck(), shellHealthCheck()}
	runner.outputs = []string{"opl-health-exit:0\n", "opl-health-exit:2\n"}
	if ready, err := p.probeLocalDockerDependencyChecks(context.Background(), input, container, checks); err != nil || ready || len(runner.calls) != 2 {
		t.Fatalf("second exec did not gate readiness: %v", err)
	}
	runner.calls = nil
	runner.outputs = []string{"opl-health-exit:0\n"}
	started, _ := time.Parse(time.RFC3339Nano, container.State.StartedAt)
	p.now = func() time.Time { return started.Add(7 * time.Second) }
	checks[0].InitialDelaySeconds = 5
	checks[1].InitialDelaySeconds = 10
	if ready, err := p.probeLocalDockerDependencyChecks(context.Background(), input, container, checks); err != nil || ready || len(runner.calls) != 0 {
		t.Fatalf("later delay ignored: %v", err)
	}
	p.now = time.Now
	runner.outputs = []string{"opl-health-exit:0\n"}
	runner.base.probeReady = false
	mixed := []contracts.WorkspaceApplicationDependencyHealthCheck{shellHealthCheck(), {Type: "http", Port: 8080, Path: "/health"}}
	if ready, err := p.probeLocalDockerDependencyChecks(context.Background(), input, container, mixed); err != nil || ready || len(runner.calls) != 1 || len(runner.base.probes) != 1 {
		t.Fatalf("network check omitted: %v", err)
	}
}
func TestLocalDockerApplicationExecProbeCapabilityAdmission(t *testing.T) {
	p, runner, input, _ := applicationExecProbeFixture(t)
	input.Revision.HealthChecks = nil
	p.applicationProbeImage = ""
	input.Revision.Dependencies[0].HealthChecks = []contracts.WorkspaceApplicationDependencyHealthCheck{shellHealthCheck()}
	if err := p.validateLocalDockerApplicationProbe(input.Revision); err != nil {
		t.Fatal(err)
	}
	for _, command := range [][]string{{"mysqladmin", "ping"}, {"sh", "-c", "true"}, {"/bin/sh", "-c", "true", "unexpected"}} {
		input.Revision.Dependencies[0].HealthChecks[0].Command = command
		err := p.PreflightWorkspaceApplicationRuntime(context.Background(), input, ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha"}, StorageVolume{})
		if err == nil || err.Error() != "local_docker_application_exec_probe_unsupported" || runner.base.runCount() != 0 {
			t.Fatalf("unsupported exec admitted: %v", err)
		}
	}
}
func TestLocalDockerApplicationExecWrapperSuppressesOriginalOutput(t *testing.T) {
	command := exec.Command("/bin/sh", "-c", localDockerApplicationExecProbeScript, "opl-health", "/bin/sh", "-c", `printf '%s' "$PASSWORD"; printf 'opl-health-exit:0\n' >&2; exit 7`)
	command.Env = append(os.Environ(), "PASSWORD=must-not-appear-in-argv-or-output")
	output, err := command.CombinedOutput()
	if err != nil || string(output) != "opl-health-exit:7\n" {
		t.Fatalf("wrapper did not isolate result: err=%v", err)
	}
	for _, arg := range command.Args {
		if strings.Contains(arg, "must-not-appear") {
			t.Fatal("secret value interpolated into command")
		}
	}
}

func TestLocalDockerApplicationExecReadinessGatesStartupWithoutFailure(t *testing.T) {
	p, runner, input, _ := applicationExecProbeFixture(t)
	input.Revision.HealthChecks = nil
	input.Revision.Dependencies[0].HealthChecks = []contracts.WorkspaceApplicationDependencyHealthCheck{shellHealthCheck()}
	p.applicationProbeImage = ""
	runner.outputs = []string{"opl-health-exit:7\n"}
	first := ensureApplicationStartup(t, p, input)
	if first.Status != "pending" || runner.base.runCount() != 1 {
		t.Fatalf("unready exec did not defer main: %s", first.Status)
	}
	runner.outputs = []string{"opl-health-exit:0\n"}
	second := ensureApplicationStartup(t, p, input)
	if second.Status != "ready" || runner.base.runCount() != 2 || len(runner.base.probes) != 0 {
		t.Fatalf("healthy exec did not release main: %s", second.Status)
	}
}
