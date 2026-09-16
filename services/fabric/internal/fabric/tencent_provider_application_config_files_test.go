package fabric

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func tencentConfiguredDependencyInput(t *testing.T) (*TencentProvider, *fakeTencentKubectl, WorkspaceApplicationRuntimeInput) {
	t.Helper()
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	input.Revision.PersistentMounts = nil
	input.Revision.ConfigInputs = []contracts.WorkspaceApplicationConfigInput{{Name: "main-config", Target: "/etc/app.conf"}}
	input.Configuration.Files = map[string]string{"main-config": "mode=main\n", "dependency-config": "mode=dependency\n"}
	dependency := &input.Revision.Dependencies[0]
	dependency.Command = contracts.WorkspaceApplicationDependencyCommand{Entrypoint: []string{"/bin/server"}, Args: []string{"--serve"}, Env: map[string]string{"MODE": "dependency"}}
	dependency.Ports = []contracts.WorkspaceApplicationDependencyPort{{Name: "api", Port: 9380, Protocol: "TCP"}}
	dependency.HealthChecks = []contracts.WorkspaceApplicationDependencyHealthCheck{{Type: "http", Port: 9380, Path: "/ready", InitialDelaySeconds: 5}}
	dependency.PersistentMounts = []contracts.WorkspaceApplicationDependencyMount{{Name: "index", MountPath: "/index"}}
	dependency.ScratchMounts = []contracts.WorkspaceApplicationDependencyMount{{Name: "tmp", MountPath: "/tmp"}}
	dependency.ConfigInputs = []contracts.WorkspaceApplicationConfigInput{{Name: "dependency-config", Target: "/etc/service.conf"}}
	dependency.SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "file-token", Target: "/run/secrets/token"}, {Name: "env-token", Env: "SERVICE_TOKEN"}}
	input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "file-token", SecretRef: "fixture-secret", Version: "fixture-version", Key: "token"}, {Name: "env-token", SecretRef: "fixture-secret", Version: "fixture-version", Key: "token"}}
	fake.resources["Secret:fixture-secret"] = map[string]any{"kind": "Secret", "metadata": map[string]any{"name": "fixture-secret", "annotations": map[string]any{"oplcloud.cn/account-id": input.AccountID, "oplcloud.cn/workspace-id": input.WorkspaceID, "oplcloud.cn/secret-version": "fixture-version"}}, "data": map[string]any{"token": base64.StdEncoding.EncodeToString([]byte("synthetic-token"))}}
	var err error
	input.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	return provider, fake, input
}

func TestTencentApplicationDependencyConfigAndLifecycle(t *testing.T) {
	provider, fake, input := tencentConfiguredDependencyInput(t)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatal(err)
	}
	completeTencentApplicationStartup(t, provider, fake, input)
	observed, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || observed.Status != "ready" {
		t.Fatalf("observation=%#v err=%v", observed, err)
	}
	dependency := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)[1]
	deployment := fake.deployments[workspaceApplicationComponentResourceName(input, dependency.Name)]
	if !verifyTencentApplicationConfiguration(input, dependency, deployment, storagePVCName(tencentApplicationVolume())) {
		t.Fatal("declared dependency rejected")
	}
	security := nested(deployment, "spec", "template", "spec", "securityContext").(map[string]any)
	for _, field := range []string{"runAsUser", "runAsGroup", "runAsNonRoot", "fsGroup"} {
		if _, exists := security[field]; exists {
			t.Fatalf("dependency inherits main %s", field)
		}
	}
	if stringValue(nested(security, "seccompProfile", "type")) != "RuntimeDefault" {
		t.Fatal("dependency seccomp default removed")
	}
	main := fake.deployments[workspaceApplicationComponentResourceName(input, "main")]
	if number(nested(main, "spec", "template", "spec", "securityContext", "runAsUser")) != 10001 {
		t.Fatal("main identity changed")
	}
	configName := "ConfigMap:" + workspaceApplicationComponentResourceName(input, "config")
	if !verifyApplicationConfigObject(input, fake.resources[configName]) {
		t.Fatal("immutable config missing")
	}
	result, err := provider.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), input, "absent")
	if err != nil || result.State != "absent" {
		t.Fatalf("delete=%#v err=%v", result, err)
	}
	if fake.resources[configName] != nil {
		t.Fatal("generation config was not deleted")
	}
	if fake.resources["Secret:fixture-secret"] == nil {
		t.Fatal("source secret was deleted")
	}
}

func TestTencentApplicationDependencyConfigurationRejectsDrift(t *testing.T) {
	for _, change := range []string{"env", "command", "args", "port", "probe", "data pvc", "data path", "scratch", "file secret", "env secret", "config source", "config readonly", "extra mount"} {
		t.Run(change, func(t *testing.T) {
			_, _, input := tencentConfiguredDependencyInput(t)
			component := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)[1]
			raw := workspaceApplicationComponentDeployment(input, tencentApplicationCompute(), tencentApplicationVolume(), component, nil, "fixture-pvc")
			var deployment map[string]any
			if err := json.Unmarshal(mustJSON(raw), &deployment); err != nil {
				t.Fatal(err)
			}
			if !verifyTencentApplicationConfiguration(input, component, deployment, "fixture-pvc") {
				t.Fatal("baseline rejected")
			}
			spec := nested(deployment, "spec", "template", "spec").(map[string]any)
			container := spec["containers"].([]any)[0].(map[string]any)
			switch change {
			case "env":
				container["env"].([]any)[0].(map[string]any)["value"] = "changed"
			case "command":
				container["command"] = []any{"/wrong"}
			case "args":
				container["args"] = []any{"--wrong"}
			case "port":
				container["ports"].([]any)[0].(map[string]any)["containerPort"] = 9000
			case "probe":
				container["readinessProbe"].(map[string]any)["httpGet"].(map[string]any)["path"] = "/wrong"
			case "env secret":
				container["env"].([]any)[1].(map[string]any)["valueFrom"] = map[string]any{"secretKeyRef": map[string]any{"name": "foreign", "key": "token"}}
			case "extra mount":
				container["volumeMounts"] = append(container["volumeMounts"].([]any), map[string]any{"name": "extra", "mountPath": "/other"})
			}
			for _, value := range container["volumeMounts"].([]any) {
				mount := value.(map[string]any)
				if change == "data path" && mount["name"] == "workspace-data" {
					mount["subPath"] = "foreign"
				}
				if change == "file secret" && mount["name"] == "application-secrets" {
					mount["subPath"] = "foreign"
				}
				if change == "config readonly" && mount["name"] == "application-config" {
					mount["readOnly"] = false
				}
			}
			for _, value := range spec["volumes"].([]any) {
				volume := value.(map[string]any)
				if change == "data pvc" && volume["name"] == "workspace-data" {
					volume["persistentVolumeClaim"].(map[string]any)["claimName"] = "foreign"
				}
				if change == "scratch" && volume["emptyDir"] != nil {
					volume["emptyDir"].(map[string]any)["medium"] = ""
				}
				if change == "config source" && volume["name"] == "application-config" {
					volume["configMap"].(map[string]any)["name"] = "foreign"
				}
			}
			if verifyTencentApplicationConfiguration(input, component, deployment, "fixture-pvc") {
				t.Fatalf("accepted %s", change)
			}
		})
	}
}

func TestTencentApplicationConfigObjectRejectsDrift(t *testing.T) {
	for _, change := range []string{"data", "immutable", "digest", "owner"} {
		t.Run(change, func(t *testing.T) {
			provider, fake, input := tencentConfiguredDependencyInput(t)
			if err := provider.prepareApplicationConfigFiles(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			config := fake.resources["ConfigMap:"+workspaceApplicationComponentResourceName(input, "config")]
			switch change {
			case "data":
				config["data"].(map[string]any)["main-config"] = "changed"
			case "immutable":
				config["immutable"] = false
			case "digest":
				nested(config, "metadata", "annotations").(map[string]any)["oplcloud.cn/configuration-digest"] = "changed"
			case "owner":
				nested(config, "metadata", "labels").(map[string]any)["oplcloud.cn/account-id"] = "foreign"
			}
			if err := provider.prepareApplicationConfigFiles(context.Background(), input); !errors.Is(err, ErrLaunchStageBindingConflict) {
				t.Fatalf("accepted %s: %v", change, err)
			}
		})
	}
}

func TestTencentApplicationConfigCleanupRejectsForeignOwner(t *testing.T) {
	provider, fake, input := tencentConfiguredDependencyInput(t)
	if err := provider.prepareApplicationConfigFiles(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	config := fake.resources["ConfigMap:"+workspaceApplicationComponentResourceName(input, "config")]
	nested(config, "metadata", "labels").(map[string]any)["oplcloud.cn/runtime-id"] = "foreign"
	if _, err := provider.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), input, "absent"); !errors.Is(err, ErrLaunchStageBindingConflict) {
		t.Fatalf("foreign config deletion allowed: %v", err)
	}
	if len(fake.mutations) != 0 {
		t.Fatalf("mutated before ownership check: %#v", fake.mutations)
	}
}

func TestTencentApplicationConfigReadbackRejectsMissingAndChangedContents(t *testing.T) {
	for _, change := range []string{"missing", "content"} {
		t.Run(change, func(t *testing.T) {
			provider, fake, input := tencentConfiguredDependencyInput(t)
			if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
				t.Fatal(err)
			}
			key := "ConfigMap:" + workspaceApplicationComponentResourceName(input, "config")
			if change == "missing" {
				delete(fake.resources, key)
			} else {
				fake.resources[key]["data"].(map[string]any)["main-config"] = "changed"
			}
			if _, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input); !errors.Is(err, ErrLaunchStageBindingConflict) {
				t.Fatalf("accepted %s: %v", change, err)
			}
		})
	}
}

func TestTencentApplicationDependencyReadbackAllowsKubernetesProbeDefaults(t *testing.T) {
	_, _, input := tencentConfiguredDependencyInput(t)
	component := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)[1]
	raw := workspaceApplicationComponentDeployment(input, tencentApplicationCompute(), tencentApplicationVolume(), component, nil, "fixture-pvc")
	var deployment map[string]any
	if err := json.Unmarshal(mustJSON(raw), &deployment); err != nil {
		t.Fatal(err)
	}
	probe := firstContainerField(deployment, "readinessProbe").(map[string]any)
	probe["timeoutSeconds"], probe["successThreshold"], probe["failureThreshold"] = 1, 1, 3
	probe["httpGet"].(map[string]any)["scheme"] = "HTTP"
	if !verifyTencentApplicationConfiguration(input, component, deployment, "fixture-pvc") {
		t.Fatal("Kubernetes probe defaults rejected")
	}
}

func TestTencentApplicationConfigPrepareReusesExactImmutableObject(t *testing.T) {
	provider, fake, input := tencentConfiguredDependencyInput(t)
	if err := provider.prepareApplicationConfigFiles(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	original := fake.resources["ConfigMap:"+workspaceApplicationComponentResourceName(input, "config")]
	if err := provider.prepareApplicationConfigFiles(context.Background(), input); err != nil {
		t.Fatalf("identical replay recreated immutable config: %v", err)
	}
	if stringValue(nested(original, "metadata", "uid")) != "ConfigMap:"+workspaceApplicationComponentResourceName(input, "config") {
		t.Fatal("immutable object not bound to generation")
	}
}
