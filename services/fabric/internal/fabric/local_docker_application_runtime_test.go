package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// applicationRuntimeDockerRunner is a scripted docker CLI: `run` materialises
// a running container from the command args, `container ls`/`inspect` read it
// back, and everything else fails loudly.
type applicationRuntimeDockerRunner struct {
	mu         sync.Mutex
	containers map[string][]byte
	runs       [][]string
	probes     [][]string
	probeReady bool
	probeErr   error
}

func (r *applicationRuntimeDockerRunner) Run(_ context.Context, _ []byte, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch args[0] {
	case "container":
		switch args[1] {
		case "ls":
			name := ""
			for _, arg := range args {
				if strings.HasPrefix(arg, "name=^/") {
					name = strings.TrimSuffix(strings.TrimPrefix(arg, "name=^/"), "$")
				}
			}
			if _, ok := r.containers[name]; !ok {
				// docker ls prints nothing for a missing object; one JSON
				// object per line for a match.
				return nil, nil
			}
			return []byte(fmt.Sprintf("{\"ID\":\"cid-%s\",\"Name\":\"%s\"}", name, name)), nil
		case "inspect":
			name := strings.TrimPrefix(args[2], "cid-")
			body, ok := r.containers[name]
			if !ok {
				return nil, fmt.Errorf("no such container: %s", name)
			}
			return body, nil
		}
	case "run":
		if len(args) > 1 && args[1] == "--rm" {
			r.probes = append(r.probes, args)
			return []byte(fmt.Sprintf("{\"ready\":%t}", r.probeReady)), r.probeErr
		}
		r.runs = append(r.runs, args)
		name, image, labels := "", "", map[string]string{}
		published := map[string]any{}
		for index := 1; index < len(args); index++ {
			if args[index] == "-d" {
				continue
			}
			if !strings.HasPrefix(args[index], "-") {
				image = args[index]
				break
			}
			switch args[index] {
			case "--name":
				name = args[index+1]
			case "--label":
				if pair := strings.SplitN(args[index+1], "=", 2); len(pair) == 2 {
					labels[pair[0]] = pair[1]
				}
			case "-p":
				if parts := strings.Split(args[index+1], "::"); len(parts) == 2 {
					published[strings.TrimSuffix(parts[1], "/tcp")+"/tcp"] = []map[string]any{{"HostIP": "127.0.0.1", "HostPort": "31080"}}
				}
			}
			index++
		}
		if name == "" {
			return nil, fmt.Errorf("run requires --name")
		}
		inspect := []map[string]any{{
			"Id": "cid-" + name, "Name": "/" + name,
			"Config":          map[string]any{"Image": image, "Labels": labels},
			"State":           map[string]any{"Status": "running", "Running": true, "StartedAt": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano)},
			"NetworkSettings": map[string]any{"Ports": published},
		}}
		body, err := json.Marshal(inspect)
		if err != nil {
			return nil, err
		}
		r.containers[name] = body
		r.containers["cid-"+name] = body
		return []byte("cid-" + name), nil
	}
	return nil, fmt.Errorf("unexpected docker call: %v", args)
}

func (r *applicationRuntimeDockerRunner) runCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs)
}

func (r *applicationRuntimeDockerRunner) runArgs(index int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs[index]
}

func (r *applicationRuntimeDockerRunner) setContainerState(name, status string, running bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var inspect []map[string]any
	if err := json.Unmarshal(r.containers[name], &inspect); err != nil {
		panic(err)
	}
	inspect[0]["State"] = map[string]any{"Status": status, "Running": running}
	body, err := json.Marshal(inspect)
	if err != nil {
		panic(err)
	}
	r.containers[name] = body
	r.containers["cid-"+name] = body
}

func applicationRuntimeProviderFixture(t *testing.T, workspaceID string) (*LocalDockerProvider, *applicationRuntimeDockerRunner, localDockerStoragePaths) {
	t.Helper()
	root := localDockerStorageTestRoot(t)
	rootHandle, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	workspaceName := localDockerName("opl-workspace", workspaceID)
	for _, name := range []string{workspaceName, ".storage-" + workspaceName} {
		if err := ensureLocalDockerStorageDirectory(rootHandle, name, 0700); err != nil {
			t.Fatal(err)
		}
		for _, child := range []string{"data", "projects"} {
			if err := ensureLocalDockerStorageDirectory(rootHandle, name+"/"+child, 0700); err != nil {
				t.Fatal(err)
			}
		}
		if err := writeLocalDockerStorageMetadata(rootHandle, name+"/"+localDockerStorageMetadataFile, localDockerStorageMetadata{
			SchemaVersion: localDockerStorageMetadataSchemaVersion, StorageID: "storage-alpha",
			AccountID: "acct-alpha", WorkspaceID: workspaceID, ProjectID: 7, SizeGB: 10,
		}); err != nil {
			rootHandle.Close()
			t.Fatal(err)
		}
	}
	rootHandle.Close()
	quota, quotaOK := localDockerStorageTestQuota(root).(*fakeLocalDockerProjectQuota)
	if !quotaOK {
		t.Fatal("storage test quota backend is not the fake")
	}
	hardLimit, err := localDockerStorageLimitBytes(10)
	if err != nil {
		t.Fatal(err)
	}
	if err := quota.Apply(root, 7, hardLimit); err != nil {
		t.Fatal(err)
	}
	runner := &applicationRuntimeDockerRunner{containers: map[string][]byte{}, probeReady: true}
	provider := newLocalDockerProvider(LocalDockerProviderConfig{
		GatewaySecretRoot: localDockerSecretTestRoot(t), HostStorageRoot: root, StorageQuotaBackend: localDockerStorageTestQuota(root),
		ApplicationProbeImage: "repo.example/opl-cloud@sha256:" + strings.Repeat("c", 64),
	}, runner)
	paths, err := provider.storagePaths(workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return provider, runner, paths
}

func TestLocalDockerApplicationRuntimeEnsureCreatesDeclaredComponents(t *testing.T) {
	revision := applicationRevisionForTest()
	provider, runner, paths := applicationRuntimeProviderFixture(t, "workspace-alpha")
	input := applicationRuntimeInput("app-runtime-local", revision)

	observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, ComputeAllocation{
		ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running",
	}, StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if observation.Status != "ready" || len(observation.Components) != 2 || observation.Components[0].State != "ready" ||
		observation.Components[1].Name != "retrieval" || observation.RuntimeID == "" {
		t.Fatalf("ensure observation=%#v", observation)
	}
	if runner.runCount() != 2 {
		t.Fatalf("run calls=%d, want one per declared component", runner.runCount())
	}
	mainArgs := strings.Join(runner.runArgs(0), " ")
	if !strings.Contains(mainArgs, "--network opl-compute-") ||
		!strings.Contains(mainArgs, "type=bind,source="+filepath.ToSlash(paths.Data)+"/data,target=/data") ||
		!strings.Contains(mainArgs, revision.Image) {
		t.Fatalf("main run args=%s", mainArgs)
	}
	dependencyArgs := strings.Join(runner.runArgs(1), " ")
	if !strings.Contains(dependencyArgs, "opl-app-retrieval-") || !strings.Contains(dependencyArgs, revision.Dependencies[0].Image) {
		t.Fatalf("dependency run args=%s", dependencyArgs)
	}

	idempotent, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, ComputeAllocation{
		ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running",
	}, StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
	if err != nil || idempotent.Status != "ready" || runner.runCount() != 2 {
		t.Fatalf("idempotent ensure observation=%#v err=%v runCalls=%d", idempotent, err, runner.runCount())
	}
}

func TestLocalDockerApplicationRuntimeRejectsDriftedContainer(t *testing.T) {
	revision := applicationRevisionForTest()
	provider, _, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	input := applicationRuntimeInput("app-runtime-drift", revision)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, ComputeAllocation{
		ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running",
	}, StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"}); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	drifted := revision
	drifted.Image = "repo.example/apps/knowledge@sha256:" + strings.Repeat("f", 64)
	input.Revision = drifted
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, ComputeAllocation{
		ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running",
	}, StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"}); err == nil || !strings.Contains(err.Error(), "component_conflict") {
		t.Fatalf("drifted container error=%v, want component conflict", err)
	}
}

func TestLocalDockerApplicationRuntimeReadbackReportsStates(t *testing.T) {
	revision := applicationRevisionForTest()
	provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	input := applicationRuntimeInput("app-runtime-read", revision)
	read := func() contracts.WorkspaceApplicationRuntimeObservation {
		t.Helper()
		observation, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		return observation
	}
	absent := read()
	if absent.Status != "absent" || absent.Components[0].State != "absent" || absent.Components[1].State != "absent" {
		t.Fatalf("absent observation=%#v", absent)
	}
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, ComputeAllocation{
		ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running",
	}, StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"}); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	ready := read()
	if ready.Status != "ready" {
		t.Fatalf("ready observation=%#v", ready)
	}
	mainContainerName, nameErr := localDockerApplicationComponentName(input.WorkspaceID, "main")
	if nameErr != nil {
		t.Fatal(nameErr)
	}
	runner.setContainerState(mainContainerName, "exited", false)
	failed := read()
	if failed.Status != "failed" || failed.Components[0].State != "failed" {
		t.Fatalf("failed observation=%#v", failed)
	}
}

func TestWorkspaceApplicationRuntimeEngineDrivesLocalDockerProvider(t *testing.T) {
	revision := applicationRevisionForTest()
	provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	store := NewMemoryOperationStore()
	service := runtimeTestService(provider, store)
	volume := service.volumes["storage-alpha"]
	volume.ProviderResourceID = ""
	volume.SizeGB = 10
	service.volumes["storage-alpha"] = volume
	input := applicationRuntimeInput("app-runtime-engine", revision)

	observation, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil {
		t.Fatalf("engine-driven create: %v", err)
	}
	if observation.Status != "ready" || len(observation.Components) != 2 || runner.runCount() != 2 {
		t.Fatalf("engine observation=%#v runCalls=%d", observation, runner.runCount())
	}
	replayed, err := service.CreateWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || replayed.RuntimeID != observation.RuntimeID || runner.runCount() != 2 {
		t.Fatalf("engine replay observation=%#v err=%v runCalls=%d", replayed, err, runner.runCount())
	}
}

func TestLocalDockerApplicationRuntimePublishesDeclaredPortsAndEntryURL(t *testing.T) {
	revision := applicationRevisionForTest()
	provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	input := applicationRuntimeInput("app-runtime-entry", revision)

	observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, ComputeAllocation{
		ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running",
	}, StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	mainArgs := strings.Join(runner.runArgs(0), " ")
	if !strings.Contains(mainArgs, "-p 127.0.0.1::8080") {
		t.Fatalf("main run args must publish the declared port: %s", mainArgs)
	}
	if observation.EntryURL == "" || !strings.HasPrefix(observation.EntryURL, "http://127.0.0.1:") {
		t.Fatalf("entry URL=%q, want the published host port", observation.EntryURL)
	}
}

func TestLocalDockerApplicationRuntimeProbesMainWithoutPublishingPrivatePorts(t *testing.T) {
	provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	revision := applicationRevisionForTest()
	revision.ExposurePolicy = "cloud_private"
	revision.Entrypoint = []string{"/app/server", "--serve"}
	input := applicationRuntimeInput("app-private-probes", revision)
	runner.probeReady = false
	observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input,
		ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running"},
		StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
	if err != nil || observation.Status != "pending" || observation.EntryURL != "" {
		t.Fatalf("pending=%#v err=%v", observation, err)
	}
	main := strings.Join(runner.runArgs(0), " ")
	dependency := strings.Join(runner.runArgs(1), " ")
	if strings.Contains(main, " -p ") || !strings.Contains(main, "--entrypoint /app/server") || !strings.HasSuffix(main, revision.Image+" --serve") {
		t.Fatalf("main=%s", main)
	}
	for _, flag := range []string{"--entrypoint", "--mount", " -p ", "--serve"} {
		if strings.Contains(dependency, flag) {
			t.Fatalf("main configuration leaked into dependency: %s", dependency)
		}
	}
	if len(runner.probes) != 1 {
		t.Fatalf("probe calls=%d", len(runner.probes))
	}
	probe := strings.Join(runner.probes[0], " ")
	for _, required := range []string{"--rm", "--network container:cid-", "--read-only", "--cap-drop ALL", "--security-opt no-new-privileges", "--entrypoint node", provider.applicationProbeImage} {
		if !strings.Contains(probe, required) {
			t.Fatalf("missing %s: %s", required, probe)
		}
	}
	if strings.Contains(probe, "--mount") || strings.Contains(probe, " -p ") {
		t.Fatalf("probe unexpectedly exposes data or ports: %s", probe)
	}
	runner.probeReady = true
	ready, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || ready.Status != "ready" || ready.EntryURL != "" {
		t.Fatalf("ready=%#v err=%v", ready, err)
	}
	runner.probeErr = errors.New("probe runtime unavailable")
	if _, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input); err == nil {
		t.Fatal("probe execution failure reported ready")
	}
}

func TestLocalDockerApplicationRuntimeRequiresProbeImageBeforeCreatingAnyComponent(t *testing.T) {
	provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	provider.applicationProbeImage = ""
	_, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), applicationRuntimeInput("app-probe-config", applicationRevisionForTest()),
		ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running"}, StorageVolume{})
	if err == nil || err.Error() != "local_docker_application_probe_image_required" || runner.runCount() != 0 {
		t.Fatalf("err=%v runs=%d", err, runner.runCount())
	}
}

func TestLocalDockerApplicationRuntimeInitialDelayAndLiveEntryReadback(t *testing.T) {
	provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	provider.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	input := applicationRuntimeInput("app-probe-delay", applicationRevisionForTest())
	observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input,
		ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running"},
		StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
	if err != nil || observation.Status != "pending" || len(runner.probes) != 0 || observation.EntryURL != "" {
		t.Fatalf("delayed=%#v err=%v probeCalls=%d", observation, err, len(runner.probes))
	}
	provider.now = func() time.Time { return time.Now() }
	observation, err = provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || observation.Status != "ready" || observation.EntryURL == "" || len(runner.probes) != 1 {
		t.Fatalf("ready=%#v err=%v probeCalls=%d", observation, err, len(runner.probes))
	}
	name, _ := localDockerApplicationComponentName(input.WorkspaceID, "main")
	var container []map[string]any
	if err := json.Unmarshal(runner.containers[name], &container); err != nil {
		t.Fatal(err)
	}
	container[0]["Config"].(map[string]any)["Image"] = "other-image"
	runner.containers[name], _ = json.Marshal(container)
	if _, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input); err == nil {
		t.Fatal("readback accepted different image")
	}
}

func TestLocalDockerApplicationRuntimePublishesOnlySelectedEntry(t *testing.T) {
	for _, entry := range []string{"", "http"} {
		t.Run("entry="+entry, func(t *testing.T) {
			provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
			revision := applicationRevisionForTest()
			revision.EntryPort = entry
			revision.Ports = append([]contracts.WorkspaceApplicationPort{{Name: "dns", Port: 5353, Protocol: "UDP"}}, revision.Ports...)
			input := applicationRuntimeInput("app-explicit-entry", revision)
			observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input,
				ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running"},
				StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
			if err != nil {
				t.Fatal(err)
			}
			args := strings.Join(runner.runArgs(0), " ")
			if strings.Contains(args, "::5353") || (entry == "" && strings.Contains(args, " -p ")) || (entry != "" && !strings.Contains(args, "::8080/tcp")) {
				t.Fatalf("args=%s", args)
			}
			if (entry == "") != (observation.EntryURL == "") {
				t.Fatalf("entry=%q observation=%#v", entry, observation)
			}
		})
	}
}

func TestLocalDockerApplicationRuntimeReadbackRejectsAccountDrift(t *testing.T) {
	for _, component := range []string{"main", "retrieval"} {
		for _, account := range []string{"", "acct-foreign"} {
			t.Run(component+"/account="+account, func(t *testing.T) {
				provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
				input := applicationRuntimeInput("app-account-readback", applicationRevisionForTest())
				observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input,
					ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running"},
					StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
				if err != nil || observation.Status != "ready" {
					t.Fatalf("baseline=%#v err=%v", observation, err)
				}
				name, err := localDockerApplicationComponentName(input.WorkspaceID, component)
				if err != nil {
					t.Fatal(err)
				}
				var containers []dockerContainerInspect
				if err := json.Unmarshal(runner.containers[name], &containers); err != nil {
					t.Fatal(err)
				}
				containers[0].Config.Labels["opl.account.id"] = account
				runner.containers[name], err = json.Marshal(containers)
				if err != nil {
					t.Fatal(err)
				}
				mutations := runner.runCount()
				observation, err = provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
				if err == nil || err.Error() != "local_docker_application_component_conflict" || observation.Status == "ready" || runner.runCount() != mutations {
					t.Fatalf("drift=%#v err=%v mutationCount=%d", observation, err, runner.runCount())
				}
			})
		}
	}
}
