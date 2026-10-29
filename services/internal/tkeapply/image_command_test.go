package tkeapply

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func TestImageCommandDecoderSelectsExactPlatform(t *testing.T) {
	for _, test := range []struct {
		name, raw, platform string
		want                []string
		invalid             bool
	}{
		{name: "single", raw: `{"os":"linux","architecture":"amd64","config":{"Cmd":["node","scripts/headless/run.mjs"]}}`, platform: "linux/amd64", want: []string{"node", "scripts/headless/run.mjs"}},
		{name: "index", raw: `{"linux/amd64":{"os":"linux","architecture":"amd64","config":{"Cmd":["amd64"]}},"linux/arm64":{"os":"linux","architecture":"arm64","config":{"Cmd":["arm64"]}}}`, platform: "linux/amd64", want: []string{"amd64"}},
		{name: "variant", raw: `{"os":"linux","architecture":"arm","variant":"v7","config":{"Cmd":["arm"]}}`, platform: "linux/arm/v7", want: []string{"arm"}},
		{name: "empty", raw: `{"os":"linux","architecture":"amd64","config":{"Cmd":null}}`, platform: "linux/amd64"},
		{name: "omitted cmd", raw: `{"os":"linux","architecture":"amd64","config":{}}`, platform: "linux/amd64"},
		{name: "wrong platform", raw: `{"os":"linux","architecture":"arm64","config":{"Cmd":["arm64"]}}`, platform: "linux/amd64", invalid: true},
		{name: "missing platform", raw: `{"linux/arm64":{"os":"linux","architecture":"arm64","config":{}}}`, platform: "linux/amd64", invalid: true},
		{name: "missing config", raw: `{"os":"linux","architecture":"amd64"}`, platform: "linux/amd64", invalid: true},
		{name: "null config", raw: `{"os":"linux","architecture":"amd64","config":null}`, platform: "linux/amd64", invalid: true},
		{name: "invalid cmd", raw: `{"os":"linux","architecture":"amd64","config":{"Cmd":[1]}}`, platform: "linux/amd64", invalid: true},
		{name: "null argument", raw: `{"os":"linux","architecture":"amd64","config":{"Cmd":[null]}}`, platform: "linux/amd64", invalid: true},
		{name: "malformed", raw: `{`, platform: "linux/amd64", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			actual, err := decodeImageCommand([]byte(test.raw), test.platform)
			if test.invalid {
				if !errors.Is(err, errImageCommand) {
					t.Fatalf("got %v, %v", actual, err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(actual, test.want) {
				t.Fatalf("got %v, %v; want %v", actual, err, test.want)
			}
		})
	}
}

func TestExecutorPreservesMainImageCMDAndRejectsArgumentDrift(t *testing.T) {
	cluster := newFakeCluster()
	cluster.store(pvcObject())
	input := testInput(t)
	input.Revision.Entrypoint = []string{"/usr/local/bin/opl-webui-entrypoint", "entry-arg"}
	executor := testExecutor(cluster, testInstallation(map[string]string{"app": "serve"}))
	calls := 0
	executor.ImageCommand = func(_ context.Context, image, platform string) ([]string, error) {
		calls++
		if image != input.Revision.Image || platform != input.Revision.Platform {
			t.Fatalf("unbound image read: %s %s", image, platform)
		}
		return []string{"node", "scripts/headless/run.mjs", "$(LITERAL)", "$x", "$$"}, nil
	}
	result, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement())
	if err != nil || result.Status != "ready" {
		t.Fatalf("ensure=%+v %v", result, err)
	}
	component := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)[0]
	deployment := cluster.objects["Deployment/"+ComponentResourceName(input, component.Name)]
	if !bytes.Equal(mustJSON(firstContainerField(deployment, "command")), mustJSON(input.Revision.Entrypoint)) {
		t.Fatal("entrypoint changed")
	}
	expected := []string{"node", "scripts/headless/run.mjs", "$$(LITERAL)", "$$x", "$$$$"}
	if !bytes.Equal(mustJSON(firstContainerField(deployment, "args")), mustJSON(expected)) {
		t.Fatalf("args=%v", firstContainerField(deployment, "args"))
	}
	if calls == 0 {
		t.Fatal("image CMD was not read")
	}
	containers := nested(deployment, "spec", "template", "spec", "containers").([]any)
	containers[0].(map[string]any)["args"] = []string{"foreign-server"}
	result, err = executor.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || result.Status == "ready" {
		t.Fatalf("drift accepted: %+v %v", result, err)
	}
	// A replay of the original command repairs the old command-only workload;
	// a read above did not overwrite it.
	if !reflect.DeepEqual(firstContainerField(deployment, "args"), []string{"foreign-server"}) {
		t.Fatal("read mutated deployment")
	}
	result, err = executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement())
	if err != nil && !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("replay=%+v %v", result, err)
	}
	corrected := cluster.objects["Deployment/"+ComponentResourceName(input, component.Name)]
	if !bytes.Equal(mustJSON(firstContainerField(corrected, "args")), mustJSON(expected)) {
		t.Fatalf("original replay did not restore image CMD: %v", firstContainerField(corrected, "args"))
	}
}

func TestExecutorLeavesImageDefaultsWhenEntrypointIsNotOverridden(t *testing.T) {
	executor := &Executor{ImageCommand: func(context.Context, string, string) ([]string, error) {
		t.Fatal("unnecessary image read")
		return nil, nil
	}}
	args, err := executor.mainImageArgs(context.Background(), testInput(t))
	if err != nil || args != nil {
		t.Fatalf("args=%v err=%v", args, err)
	}
}

func TestImageCommandFailurePreventsApply(t *testing.T) {
	cluster := newFakeCluster()
	cluster.store(pvcObject())
	executor := testExecutor(cluster, testInstallation(map[string]string{"app": "serve"}))
	executor.ImageCommand = func(context.Context, string, string) ([]string, error) { return nil, errImageCommand }
	input := testInput(t)
	input.Revision.Entrypoint = []string{"/entrypoint"}
	if _, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); !errors.Is(err, errImageCommand) {
		t.Fatalf("err=%v", err)
	}
	if len(cluster.objects) != 1 {
		t.Fatal("failed image read mutated cluster")
	}
}

func TestImageCommandReadsInstallationSecretAndCachesOnlySuccess(t *testing.T) {
	image := "registry.test/cmd@sha256:" + strings.Repeat("b", 64)
	key := image + "\x00linux/amd64"
	imageCommandCache.Delete(key)
	t.Cleanup(func() { imageCommandCache.Delete(key) })
	dir := t.TempDir()
	// Exercise the real process/config transport without a registry or daemon.
	script := `#!/bin/sh
set -eu
[ "$1" = --config ]
[ -f "$2/config.json" ]
[ "$(cat "$2/config.json")" = '{"auths":{"registry.test":{"auth":"fixture"}}}' ]
printf '%s' "$2" > "$TEST_COMMAND_CONFIG_PATH"
[ "$3" = buildx ] && [ "$4" = imagetools ] && [ "$5" = inspect ]
[ "$6" = "$TEST_COMMAND_IMAGE" ] && [ "$7" = --format ] && [ "$8" = '{{json .Image}}' ]
[ "$TEST_COMMAND_FAIL" = 0 ] || exit 1
printf '%s' '{"os":"linux","architecture":"amd64","config":{"Cmd":["node","server.mjs"]}}'
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TEST_COMMAND_IMAGE", image)
	t.Setenv("TEST_COMMAND_CONFIG_PATH", filepath.Join(dir, "config-path"))
	t.Setenv("TEST_COMMAND_FAIL", "1")
	cluster := newFakeCluster()
	cluster.store(map[string]any{"kind": "Secret", "metadata": map[string]any{"name": "tcr-pull-secret", "namespace": "opl-cloud"}, "type": "kubernetes.io/dockerconfigjson", "data": map[string]any{".dockerconfigjson": base64String(`{"auths":{"registry.test":{"auth":"fixture"}}}`)}})
	executor := &Executor{Kubectl: cluster.kubectl, Installation: testInstallation()}
	if _, err := executor.readImageCommand(context.Background(), image, "linux/amd64"); !errors.Is(err, errImageCommand) {
		t.Fatalf("err=%v", err)
	}
	if _, cached := imageCommandCache.Load(key); cached {
		t.Fatal("failed read cached")
	}
	t.Setenv("TEST_COMMAND_FAIL", "0")
	actual, err := executor.readImageCommand(context.Background(), image, "linux/amd64")
	if err != nil || !reflect.DeepEqual(actual, []string{"node", "server.mjs"}) {
		t.Fatalf("args=%v err=%v", actual, err)
	}
	configPath, err := os.ReadFile(filepath.Join(dir, "config-path"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(string(configPath)); !os.IsNotExist(err) {
		t.Fatal("registry config was not removed")
	}
	actual[0] = "changed"
	t.Setenv("TEST_COMMAND_FAIL", "1")
	cached, err := executor.readImageCommand(context.Background(), image, "linux/amd64")
	if err != nil || cached[0] != "node" {
		t.Fatalf("cache mutated: %v %v", cached, err)
	}
}

func TestImageCommandRejectsUnverifiedSecretAndMutableImage(t *testing.T) {
	for _, test := range []struct {
		name, image string
		secret      map[string]any
	}{
		{name: "mutable", image: "registry.test/app:latest"},
		{name: "absent", image: "registry.test/app@sha256:" + strings.Repeat("c", 64)},
		{name: "foreign namespace", image: "registry.test/app@sha256:" + strings.Repeat("d", 64), secret: map[string]any{"kind": "Secret", "metadata": map[string]any{"name": "tcr-pull-secret", "namespace": "foreign"}, "type": "kubernetes.io/dockerconfigjson", "data": map[string]any{".dockerconfigjson": base64String(`{}`)}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			cluster := newFakeCluster()
			if test.secret != nil {
				cluster.store(test.secret)
			}
			executor := &Executor{Kubectl: cluster.kubectl, Installation: testInstallation()}
			if _, err := executor.readImageCommand(context.Background(), test.image, "linux/amd64"); !errors.Is(err, errImageCommand) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestAbsentRuntimeReadDoesNotRequireRegistry(t *testing.T) {
	cluster := newFakeCluster()
	executor := testExecutor(cluster, testInstallation())
	executor.ImageCommand = func(context.Context, string, string) ([]string, error) {
		t.Fatal("absent workload must not require registry")
		return nil, errImageCommand
	}
	input := testInput(t)
	input.Revision.Entrypoint = []string{"/entrypoint"}
	observation, err := executor.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || observation.Status != "absent" {
		t.Fatalf("read=%+v %v", observation, err)
	}
	lifecycle, err := executor.ReadWorkspaceApplicationRuntimeLifecycle(context.Background(), input)
	if err != nil || lifecycle.State != "absent" {
		t.Fatalf("lifecycle=%+v %v", lifecycle, err)
	}
}

func TestImageCommandPreservesCancellationWithoutLeakingTransportError(t *testing.T) {
	executor := &Executor{Installation: testInstallation(), Kubectl: func(context.Context, []string, []byte) ([]byte, error) {
		return nil, errors.Join(context.Canceled, errors.New("sensitive transport data"))
	}}
	_, err := executor.readImageCommand(context.Background(), "registry.test/cancel@sha256:"+strings.Repeat("e", 64), "linux/amd64")
	if !errors.Is(err, context.Canceled) || !errors.Is(err, errImageCommand) || strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("unsafe or lost cause: %v", err)
	}
}
