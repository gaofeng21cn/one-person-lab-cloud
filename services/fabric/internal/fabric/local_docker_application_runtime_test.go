package fabric

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// applicationRuntimeDockerRunner is a scripted docker CLI: `run` materialises
// a running container from the command args, `container ls`/`inspect` read it
// back, and everything else fails loudly.
type applicationRuntimeDockerRunner struct {
	mu         sync.Mutex
	containers map[string][]byte
	runs       [][]string
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
		r.runs = append(r.runs, args)
		name, image, labels := "", args[len(args)-1], map[string]string{}
		published := map[string]any{}
		for index := 0; index < len(args)-1; index++ {
			switch args[index] {
			case "--name":
				name = args[index+1]
			case "--label":
				if pair := strings.SplitN(args[index+1], "=", 2); len(pair) == 2 {
					labels[pair[0]] = pair[1]
				}
			case "-p":
				if parts := strings.Split(args[index+1], "::"); len(parts) == 2 {
					published[parts[1]+"/tcp"] = []map[string]any{{"HostIP": "127.0.0.1", "HostPort": "31080"}}
				}
			}
		}
		if name == "" {
			return nil, fmt.Errorf("run requires --name")
		}
		inspect := []map[string]any{{
			"Id": "cid-" + name, "Name": "/" + name,
			"Config":          map[string]any{"Image": image, "Labels": labels},
			"State":           map[string]any{"Status": "running", "Running": true},
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
	runner := &applicationRuntimeDockerRunner{containers: map[string][]byte{}}
	provider := newLocalDockerProvider(LocalDockerProviderConfig{
		GatewaySecretRoot: localDockerSecretTestRoot(t), HostStorageRoot: root, StorageQuotaBackend: localDockerStorageTestQuota(root),
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
