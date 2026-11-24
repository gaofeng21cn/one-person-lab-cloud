package fabric

import (
	"context"
	"errors"
	"strings"
	"testing"

	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	contracts "opl-cloud/packages/contracts/go"
)

type applicationStartupRunner struct {
	*applicationRuntimeDockerRunner
	base    *applicationRuntimeDockerRunner
	ready   map[string]bool
	actions []string
}

func (r *applicationStartupRunner) ContainerCreate(ctx context.Context, config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (mobyclient.ContainerCreateResult, error) {
	if strings.HasPrefix(containerName, "opl-app-probe-") && hostConfig != nil {
		r.base.probeReady = true
		for name, ready := range r.ready {
			if strings.Contains(string(hostConfig.NetworkMode), "container:cid-opl-app-"+name+"-") {
				r.base.probeReady = ready
			}
		}
	}
	return r.base.ContainerCreate(ctx, config, hostConfig, networkingConfig, platform, containerName)
}

func (r *applicationStartupRunner) ContainerStart(ctx context.Context, containerID string, options mobyclient.ContainerStartOptions) error {
	if !strings.HasPrefix(containerID, "cid-") && !strings.HasPrefix(containerID, "opl-app-probe-") {
		r.actions = append(r.actions, "start:"+containerID)
	}
	return r.base.ContainerStart(ctx, containerID, options)
}

func (r *applicationStartupRunner) ContainerStop(ctx context.Context, containerID string, options mobyclient.ContainerStopOptions) error {
	if !strings.HasPrefix(containerID, "opl-app-probe-") {
		r.actions = append(r.actions, "stop:"+containerID)
	}
	return r.base.ContainerStop(ctx, containerID, options)
}

func (r *applicationStartupRunner) ContainerRemove(ctx context.Context, containerID string, options mobyclient.ContainerRemoveOptions) error {
	if !strings.HasPrefix(containerID, "opl-app-probe-") {
		r.actions = append(r.actions, "rm:"+containerID)
	}
	return r.base.ContainerRemove(ctx, containerID, options)
}

func applicationStartupFixture(t *testing.T) (*LocalDockerProvider, *applicationStartupRunner, WorkspaceApplicationRuntimeInput) {
	t.Helper()
	provider, base, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	runner := &applicationStartupRunner{applicationRuntimeDockerRunner: base, base: base, ready: map[string]bool{"database": false, "retrieval": false}}
	provider.docker = runner
	revision := applicationRevisionForTest()
	revision.Dependencies[0].DependsOn = []string{"database"}
	revision.Dependencies[0].HealthChecks = []contracts.WorkspaceApplicationDependencyHealthCheck{{Type: "http", Port: 9200, Path: "/health"}}
	revision.Dependencies = append(revision.Dependencies, contracts.WorkspaceApplicationDependency{Name: "database", Image: "repo.example/database@sha256:" + strings.Repeat("d", 64), HealthChecks: []contracts.WorkspaceApplicationDependencyHealthCheck{{Type: "tcp", Port: 3306}}})
	return provider, runner, applicationRuntimeInput("dependency-startup", revision)
}
func ensureApplicationStartup(t *testing.T, p *LocalDockerProvider, input WorkspaceApplicationRuntimeInput) contracts.WorkspaceApplicationRuntimeObservation {
	t.Helper()
	result, err := p.EnsureWorkspaceApplicationRuntime(context.Background(), input, ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running"}, StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func assertApplicationStartupState(t *testing.T, result contracts.WorkspaceApplicationRuntimeObservation, name, state string) {
	t.Helper()
	for _, component := range result.Components {
		if component.Name == name {
			if component.State != state {
				t.Fatalf("%s=%s want %s", name, component.State, state)
			}
			return
		}
	}
	t.Fatalf("component %s missing", name)
}
func TestLocalDockerApplicationDependencyStartupProgressesByReadiness(t *testing.T) {
	p, runner, input := applicationStartupFixture(t)
	first := ensureApplicationStartup(t, p, input)
	assertApplicationStartupState(t, first, "database", "pending")
	assertApplicationStartupState(t, first, "retrieval", "absent")
	assertApplicationStartupState(t, first, "main", "absent")
	if runner.base.runCount() != 1 {
		t.Fatal("dependent started before database healthy")
	}
	ensureApplicationStartup(t, p, input)
	if runner.base.runCount() != 1 {
		t.Fatal("replay duplicated container")
	}
	runner.ready["database"] = true
	second := ensureApplicationStartup(t, p, input)
	assertApplicationStartupState(t, second, "database", "ready")
	assertApplicationStartupState(t, second, "retrieval", "pending")
	assertApplicationStartupState(t, second, "main", "absent")
	if runner.base.runCount() != 2 {
		t.Fatal("dependent readiness gate bypassed")
	}
	runner.ready["retrieval"] = true
	third := ensureApplicationStartup(t, p, input)
	if third.Status != "ready" || runner.base.runCount() != 3 {
		t.Fatalf("not converged: %s count=%d", third.Status, runner.base.runCount())
	}
	ensureApplicationStartup(t, p, input)
	if runner.base.runCount() != 3 {
		t.Fatal("ready replay recreated containers")
	}
}
func TestLocalDockerApplicationDependencyFailureDoesNotStartDependents(t *testing.T) {
	p, runner, input := applicationStartupFixture(t)
	ensureApplicationStartup(t, p, input)
	name, _ := localDockerApplicationComponentNameForInput(input, "database")
	runner.base.setContainerState(name, "exited", false)
	result := ensureApplicationStartup(t, p, input)
	if result.Status != "failed" || runner.base.runCount() != 1 {
		t.Fatalf("failed prerequisite started dependents: %s", result.Status)
	}
	assertApplicationStartupState(t, result, "main", "absent")
}
func TestLocalDockerApplicationDependencyLifecycleOrder(t *testing.T) {
	p, runner, input := applicationStartupFixture(t)
	runner.ready["database"], runner.ready["retrieval"] = true, true
	ensureApplicationStartup(t, p, input)
	result, err := p.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), input, "suspended")
	if err != nil || result.State != "suspended" {
		t.Fatalf("suspend err=%v state=%s", err, result.State)
	}
	for index, name := range []string{"main", "retrieval", "database"} {
		if !strings.HasPrefix(runner.actions[index], "stop:opl-app-"+name+"-") {
			t.Fatalf("stop order=%v", runner.actions)
		}
	}
	runner.actions = nil
	runner.ready["database"], runner.ready["retrieval"] = false, false
	result, err = p.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), input, "running")
	if err != nil || len(runner.actions) != 1 || !strings.HasPrefix(runner.actions[0], "start:opl-app-database-") {
		t.Fatalf("resume err=%v actions=%v", err, runner.actions)
	}
	runner.ready["database"] = true
	result, err = p.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), input, "running")
	if err != nil || len(runner.actions) != 2 || !strings.HasPrefix(runner.actions[1], "start:opl-app-retrieval-") {
		t.Fatalf("resume err=%v actions=%v", err, runner.actions)
	}
	runner.ready["retrieval"] = true
	result, err = p.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), input, "running")
	if err != nil || result.State != "running" || len(runner.actions) != 3 || !strings.HasPrefix(runner.actions[2], "start:opl-app-main-") {
		t.Fatalf("resume err=%v state=%s actions=%v", err, result.State, runner.actions)
	}
}
func TestWorkspaceApplicationCreateReplayContinuesOnlyDeferredNodes(t *testing.T) {
	for _, mode := range []string{"continue", "deleted-existing", "failed-existing", "read-only", "retired"} {
		t.Run(mode, func(t *testing.T) {
			p, runner, input := applicationStartupFixture(t)
			service := runtimeTestService(p, NewMemoryOperationStore())
			volume := service.volumes["storage-alpha"]
			volume.ProviderResourceID = ""
			volume.SizeGB = 10
			service.volumes["storage-alpha"] = volume
			first, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input)
			if !errors.Is(err, ErrWorkspaceLaunchPending) || first.Status != "pending" || runner.base.runCount() != 1 {
				t.Fatalf("first err=%v state=%s", err, first.Status)
			}
			runner.ready["database"] = true
			switch mode {
			case "retired":
				result, err := service.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), applicationLifecycleInput(input, "absent", "cancel-deferred"))
				if err != nil || result.State != "absent" {
					t.Fatalf("retire err=%v state=%s", err, result.State)
				}
			case "deleted-existing":
				name, _ := localDockerApplicationComponentNameForInput(input, "database")
				delete(runner.base.containers, name)
			case "failed-existing":
				name, _ := localDockerApplicationComponentNameForInput(input, "database")
				runner.base.setContainerState(name, "exited", false)
			case "read-only":
				_, _ = service.WorkspaceApplicationRuntimeReadback(context.Background(), input)
				if runner.base.runCount() != 1 {
					t.Fatal("read mutated runtime")
				}
				return
			}
			_, _ = service.CreateWorkspaceApplicationRuntime(context.Background(), input)
			if mode != "continue" {
				if runner.base.runCount() != 1 {
					t.Fatal("missing or failed created node recreated")
				}
				return
			}
			if runner.base.runCount() != 2 {
				t.Fatal("authorized replay did not advance deferred node")
			}
			runner.ready["retrieval"] = true
			final, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input)
			if err != nil || final.Status != "ready" || runner.base.runCount() != 3 {
				t.Fatalf("final err=%v state=%s runs=%d", err, final.Status, runner.base.runCount())
			}
		})
	}
}

func TestWorkspaceApplicationPartialStartupSuspendResumeAndFence(t *testing.T) {
	p, runner, input := applicationStartupFixture(t)
	service := runtimeTestService(p, NewMemoryOperationStore())
	volume := service.volumes["storage-alpha"]
	volume.ProviderResourceID = ""
	volume.SizeGB = 10
	service.volumes["storage-alpha"] = volume
	if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatal(err)
	}
	suspend := applicationLifecycleInput(input, "suspended", "suspend-partial")
	result, err := service.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), suspend)
	if err != nil || result.State != "suspended" {
		t.Fatalf("partial suspend err=%v state=%s", err, result.State)
	}
	assertApplicationStartupState(t, result.Observation, "main", "absent")
	before := runner.base.runCount()
	if _, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input); err == nil || runner.base.runCount() != before {
		t.Fatal("suspension fence allowed original create")
	}
	resume := applicationLifecycleInput(input, "running", "resume-partial")
	result, err = service.ReadWorkspaceApplicationRuntimeLifecycle(context.Background(), resume)
	if err != nil || result.State != "suspended" || runner.base.runCount() != before {
		t.Fatalf("read must not resume: err=%v state=%s", err, result.State)
	}
	result, err = service.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), resume)
	if err != nil || result.State != "pending" || runner.base.runCount() != 1 {
		t.Fatalf("premature dependents err=%v state=%s", err, result.State)
	}
	runner.ready["database"] = true
	result, err = service.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), resume)
	if err != nil || result.State != "pending" || runner.base.runCount() != 2 {
		t.Fatalf("retrieval not advanced err=%v state=%s runs=%d", err, result.State, runner.base.runCount())
	}
	runner.ready["retrieval"] = true
	result, err = service.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), resume)
	if err != nil || result.State != "running" || runner.base.runCount() != 3 {
		t.Fatalf("main not advanced err=%v state=%s runs=%d", err, result.State, runner.base.runCount())
	}
	result, err = service.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), resume)
	if err != nil || result.State != "running" || runner.base.runCount() != 3 {
		t.Fatal("completed resume recreated runtime")
	}
}

func TestWorkspaceApplicationRunningDoesNotRecreateSucceededRuntime(t *testing.T) {
	p, runner, input := applicationStartupFixture(t)
	runner.ready["database"], runner.ready["retrieval"] = true, true
	service := runtimeTestService(p, NewMemoryOperationStore())
	volume := service.volumes["storage-alpha"]
	volume.ProviderResourceID = ""
	volume.SizeGB = 10
	service.volumes["storage-alpha"] = volume
	if result, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input); err != nil || result.Status != "ready" {
		t.Fatalf("initial create err=%v", err)
	}
	name, _ := localDockerApplicationComponentNameForInput(input, "retrieval")
	delete(runner.base.containers, name)
	_, _ = service.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), applicationLifecycleInput(input, "running", "repair-not-authorized"))
	if runner.base.runCount() != 3 {
		t.Fatal("running request recreated previously successful component")
	}
}
