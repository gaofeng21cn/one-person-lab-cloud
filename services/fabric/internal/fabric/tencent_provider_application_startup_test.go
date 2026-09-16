package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func TestTencentApplicationStartupAndResumeFollowReadyDependencies(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	image := input.Revision.Dependencies[0].Image
	input.Revision.Dependencies = []contracts.WorkspaceApplicationDependency{
		{Name: "api", Image: image, DependsOn: []string{"database"}},
		{Name: "database", Image: image},
	}
	ctx := context.Background()
	exists := func(name string) bool {
		return fake.deployments[workspaceApplicationComponentResourceName(input, name)] != nil
	}
	for wave, want := range [][]string{{"database"}, {"database", "api"}, {"database", "api", "main"}} {
		observation, err := provider.EnsureWorkspaceApplicationRuntime(ctx, input, tencentApplicationCompute(), tencentApplicationVolume())
		if !errors.Is(err, ErrWorkspaceLaunchPending) || observation.Status != "pending" {
			t.Fatalf("wave %d observation=%#v err=%v", wave, observation, err)
		}
		for _, name := range []string{"database", "api", "main"} {
			included := false
			for _, expected := range want {
				included = included || name == expected
			}
			if exists(name) != included {
				t.Fatalf("wave %d %s existence=%v want %v", wave, name, exists(name), included)
			}
		}
		before := fake.applyCount()
		fake.setAllReady()
		if _, err := provider.ReadWorkspaceApplicationRuntime(ctx, input); err != nil {
			t.Fatal(err)
		}
		if fake.applyCount() != before {
			t.Fatal("Read advanced startup")
		}
	}
	stopped, err := provider.SetWorkspaceApplicationRuntimeLifecycle(ctx, input, "suspended")
	if err != nil || stopped.State != "suspended" {
		t.Fatalf("stop=%#v err=%v", stopped, err)
	}
	calls := fake.mutations
	if len(calls) != 3 {
		t.Fatalf("stop calls=%#v", calls)
	}
	for index, name := range []string{"main", "api", "database"} {
		if calls[index][1] != "deployment/"+workspaceApplicationComponentResourceName(input, name) || calls[index][2] != "--replicas=0" {
			t.Fatalf("stop order=%#v", calls)
		}
	}
	fake.mutations = nil
	for wave, want := range [][]string{{"database"}, {"database", "api"}, {"database", "api", "main"}} {
		if _, err := provider.SetWorkspaceApplicationRuntimeLifecycle(ctx, input, "running"); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"database", "api", "main"} {
			expected := 0
			for _, started := range want {
				if name == started {
					expected = 1
				}
			}
			actual := number(nested(fake.deployments[workspaceApplicationComponentResourceName(input, name)], "spec", "replicas"))
			if actual != float64(expected) {
				t.Fatalf("resume wave %d %s replicas=%v want %v", wave, name, actual, expected)
			}
		}
		fake.setAllReady()
	}
	resumed, err := provider.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
	if err != nil || resumed.State != "running" {
		t.Fatalf("resume=%#v err=%v", resumed, err)
	}
	order := []string{}
	for _, call := range fake.mutations {
		order = append(order, call[1])
	}
	expected := []string{}
	for _, name := range []string{"database", "api", "main"} {
		expected = append(expected, "deployment/"+workspaceApplicationComponentResourceName(input, name))
	}
	if !reflect.DeepEqual(order, expected) {
		t.Fatalf("resume order=%#v want %#v", order, expected)
	}
}

func TestTencentApplicationRejectsUnsupportedScratchOptionsBeforeMutation(t *testing.T) {
	for _, component := range []string{"main", "dependency"} {
		for _, option := range []string{"mode", "uid", "gid", "executable"} {
			t.Run(component+"/"+option, func(t *testing.T) {
				provider, fake, input := tencentApplicationRuntimeFixture(t)
				mount := contracts.WorkspaceApplicationMount{Name: "state", MountPath: "/state"}
				mode := uint32(0700)
				identity := int64(10001)
				switch option {
				case "mode":
					mount.Mode = &mode
				case "uid":
					mount.UserID = &identity
				case "gid":
					mount.GroupID = &identity
				case "executable":
					mount.Executable = true
				}
				if component == "main" {
					input.Revision.ScratchMounts = []contracts.WorkspaceApplicationMount{mount}
				} else {
					input.Revision.Dependencies[0].ScratchMounts = []contracts.WorkspaceApplicationDependencyMount{mount}
				}
				if err := provider.PreflightWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); err == nil || !strings.Contains(err.Error(), "tencent_application_scratch_options_unsupported") {
					t.Fatalf("preflight err=%v", err)
				}
				if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); err == nil || !strings.Contains(err.Error(), "tencent_application_scratch_options_unsupported") {
					t.Fatalf("ensure err=%v", err)
				}
				if fake.applyCount() != 0 || len(fake.deployments) != 0 || len(fake.resources) != 1 {
					t.Fatal("unsupported spec mutated provider resources")
				}
			})
		}
	}
}

func TestTencentApplicationScratchSizeIsRenderedAndVerified(t *testing.T) {
	_, _, input := tencentApplicationRuntimeFixture(t)
	input.Revision.ScratchMounts = []contracts.WorkspaceApplicationMount{{Name: "state", MountPath: "/state", SizeBytes: 1024}}
	input.Revision.Dependencies[0].ScratchMounts = []contracts.WorkspaceApplicationDependencyMount{{Name: "tmp", MountPath: "/tmp", SizeBytes: 2048}}
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		t.Run(component.Name, func(t *testing.T) {
			var deployment map[string]any
			if err := json.Unmarshal(mustJSON(workspaceApplicationComponentDeployment(input, tencentApplicationCompute(), tencentApplicationVolume(), component, nil, "fixture-pvc")), &deployment); err != nil {
				t.Fatal(err)
			}
			volumes := nested(deployment, "spec", "template", "spec", "volumes").([]any)
			expected := "1Ki"
			if component.Role == contracts.WorkspaceApplicationComponentDependency {
				expected = "2Ki"
			}
			for _, value := range volumes {
				volume := value.(map[string]any)
				if scratch, ok := volume["emptyDir"].(map[string]any); ok {
					scratch["sizeLimit"] = expected
				}
			}
			if !verifyTencentApplicationConfiguration(input, component, deployment, "fixture-pvc") {
				t.Fatal("normalized sizeLimit rejected")
			}
			for _, value := range volumes {
				volume := value.(map[string]any)
				if scratch, ok := volume["emptyDir"].(map[string]any); ok {
					scratch["sizeLimit"] = "4Ki"
				}
			}
			if verifyTencentApplicationConfiguration(input, component, deployment, "fixture-pvc") {
				t.Fatal("changed sizeLimit accepted")
			}
		})
	}
}

func TestTencentApplicationExecutionCapabilitiesRejectBeforeMutation(t *testing.T) {
	for _, component := range []string{"main", "dependency"} {
		for _, option := range []string{"init", "seccomp"} {
			t.Run(component+"/"+option, func(t *testing.T) {
				provider, fake, input := tencentApplicationRuntimeFixture(t)
				execution := contracts.WorkspaceApplicationExecution{}
				if option == "init" {
					execution.Init = true
				} else {
					execution.SeccompProfile = "sha256:" + strings.Repeat("a", 64)
				}
				if component == "main" {
					input.Revision.Execution = execution
				} else {
					input.Revision.Dependencies[0].Execution = execution
				}
				if err := provider.PreflightWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); err == nil || err.Error() != "tencent_application_execution_unsupported" {
					t.Fatalf("preflight err=%v", err)
				}
				if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); err == nil || err.Error() != "tencent_application_execution_unsupported" {
					t.Fatalf("ensure err=%v", err)
				}
				if fake.applyCount() != 0 || len(fake.resources) != 1 {
					t.Fatal("unsupported execution mutated resources")
				}
			})
		}
	}
}

func TestTencentApplicationExecutionIdentityRenderedAndReadBack(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	mainUser, mainGroup, depUser, depGroup := int64(10002), int64(10003), int64(20001), int64(20002)
	input.Revision.Execution = contracts.WorkspaceApplicationExecution{UserID: &mainUser, GroupID: &mainGroup}
	input.Revision.Dependencies[0].Execution = contracts.WorkspaceApplicationExecution{UserID: &depUser, GroupID: &depGroup}
	completeTencentApplicationStartup(t, provider, fake, input)
	for _, component := range contracts.WorkspaceApplicationRuntimeComponents(input.Revision) {
		deployment := fake.deployments[workspaceApplicationComponentResourceName(input, component.Name)]
		security := nested(deployment, "spec", "template", "spec", "securityContext").(map[string]any)
		user, group := mainUser, mainGroup
		if component.Role == contracts.WorkspaceApplicationComponentDependency {
			user, group = depUser, depGroup
		}
		if number(security["runAsUser"]) != float64(user) || number(security["runAsGroup"]) != float64(group) {
			t.Fatalf("identity=%#v", security)
		}
		if component.Role == contracts.WorkspaceApplicationComponentMain && number(security["fsGroup"]) != 10001 {
			t.Fatal("main storage group changed")
		}
		if component.Role == contracts.WorkspaceApplicationComponentDependency && security["fsGroup"] != nil {
			t.Fatal("dependency identity changes storage group")
		}
	}
	observed, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || observed.Status != "ready" {
		t.Fatalf("observation=%#v err=%v", observed, err)
	}
	main := fake.deployments[workspaceApplicationComponentResourceName(input, "main")]
	nested(main, "spec", "template", "spec", "securityContext").(map[string]any)["runAsUser"] = 10009
	observed, err = provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || observed.Status != "failed" {
		t.Fatalf("identity drift accepted=%#v err=%v", observed, err)
	}
}

// The provider admits a declared execution identity; which identity an
// application may use is the application's own decision, and reusing another
// generation's data is constrained at the runtime configuration boundary.
func TestTencentApplicationAdmitsDeclaredExecutionIdentity(t *testing.T) {
	_, _, input := tencentApplicationRuntimeFixture(t)
	user, group := int64(10002), int64(10002)
	input.Revision.Execution = contracts.WorkspaceApplicationExecution{UserID: &user, GroupID: &group}
	if err := validateTencentApplicationCapabilities(input.Revision); err != nil {
		t.Fatalf("declared identity=%v", err)
	}
	security := workspaceApplicationSecurityContext(input, contracts.WorkspaceApplicationRuntimeComponents(input.Revision)[0])
	if security["runAsUser"] != int64(10002) || security["runAsGroup"] != int64(10002) {
		t.Fatalf("declared identity must reach the container: %#v", security)
	}
}

func TestTencentApplicationDependencyExecProbeKeepsDeclaredCommandAndRejectsDrift(t *testing.T) {
	_, _, input := tencentApplicationRuntimeFixture(t)
	command := []string{"/bin/sh", "-c", `AUTH_TOKEN="$SERVICE_TOKEN" service-admin ping --silent`}
	input.Revision.Dependencies[0].HealthChecks = []contracts.WorkspaceApplicationDependencyHealthCheck{{Type: "exec", Command: command, InitialDelaySeconds: 10}}
	component := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)[1]
	var deployment map[string]any
	if err := json.Unmarshal(mustJSON(workspaceApplicationComponentDeployment(input, tencentApplicationCompute(), tencentApplicationVolume(), component, nil, "fixture-pvc")), &deployment); err != nil {
		t.Fatal(err)
	}
	probe := firstContainerField(deployment, "readinessProbe").(map[string]any)
	actual := nested(probe, "exec", "command").([]any)
	if len(actual) != len(command) {
		t.Fatalf("exec command=%#v", actual)
	}
	for index, argument := range command {
		if actual[index] != argument {
			t.Fatalf("argument %d changed: %#v", index, actual)
		}
	}
	if !verifyTencentApplicationConfiguration(input, component, deployment, "fixture-pvc") {
		t.Fatal("declared exec probe rejected")
	}
	actual[2] = "true"
	if verifyTencentApplicationConfiguration(input, component, deployment, "fixture-pvc") {
		t.Fatal("changed exec probe accepted")
	}
	actual[2] = command[2]
	delete(probe, "exec")
	probe["tcpSocket"] = map[string]any{"port": 9380}
	if verifyTencentApplicationConfiguration(input, component, deployment, "fixture-pvc") {
		t.Fatal("exec probe weakened to TCP")
	}
}

func TestTencentApplicationMultipleDependencyProbesRejectedBeforeApply(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	input.Revision.Dependencies[0].HealthChecks = []contracts.WorkspaceApplicationDependencyHealthCheck{{Type: "exec", Command: []string{"service-admin", "ping"}}, {Type: "tcp", Port: 9380}}
	if err := provider.PreflightWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); err == nil || err.Error() != "tencent_application_health_checks_unsupported" {
		t.Fatalf("preflight err=%v", err)
	}
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); err == nil || err.Error() != "tencent_application_health_checks_unsupported" {
		t.Fatalf("ensure err=%v", err)
	}
	if fake.applyCount() != 0 || len(fake.resources) != 1 {
		t.Fatal("unsupported probes caused provider mutation")
	}
}
