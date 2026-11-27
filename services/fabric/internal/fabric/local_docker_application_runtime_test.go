package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	dockercontainer "github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/image"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	mobyclient "github.com/moby/moby/client"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	contracts "opl-cloud/packages/contracts/go"
)

// applicationRuntimeDockerRunner is an in-memory typed localDockerClient:
// ContainerCreate/Start materialises a running container from the typed SDK
// config, ContainerList/Inspect read it back, and unexpected calls fail closed
// via embedded stubDockerClient.
type applicationRuntimeImageConfig struct {
	Entrypoint []string
	Cmd        []string
}

type recordedApplicationContainerCreate struct {
	Name             string
	Config           *dockercontainer.Config
	HostConfig       *dockercontainer.HostConfig
	NetworkingConfig *network.NetworkingConfig
	Platform         *ocispec.Platform
}

type applicationRuntimeDockerCall struct {
	Method   string
	Target   string
	Platform string
}

type applicationRuntimeDockerRunner struct {
	stubDockerClient
	mu                   sync.Mutex
	containers           map[string]dockercontainer.InspectResponse
	runs                 []recordedApplicationContainerCreate
	probes               []recordedApplicationContainerCreate
	probeReady           bool
	probeErr             error
	images               map[string]bool
	removedImages        []string
	calls                []applicationRuntimeDockerCall
	imageConfigs         map[string]applicationRuntimeImageConfig
	imagePlatforms       map[string]string
	imageInspectOverride *image.InspectResponse
	imageInspectErr      error
	imagePullErr         error
	requireLocalImages   bool
}

func formatOCIPlatform(p *ocispec.Platform) string {
	if p == nil {
		return ""
	}
	out := p.OS + "/" + p.Architecture
	if p.Variant != "" {
		out += "/" + p.Variant
	}
	return out
}

func (r *applicationRuntimeDockerRunner) PullImage(_ context.Context, refStr string, platform ocispec.Platform) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	platStr := formatOCIPlatform(&platform)
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ImagePull", Target: refStr, Platform: platStr})
	if r.imagePullErr != nil {
		return r.imagePullErr
	}
	if r.images == nil {
		r.images = map[string]bool{}
	}
	if r.imagePlatforms == nil {
		r.imagePlatforms = map[string]string{}
	}
	r.images[refStr] = true
	r.imagePlatforms[refStr] = platStr
	return nil
}

func (r *applicationRuntimeDockerRunner) ImageInspect(_ context.Context, imageID string, platform *ocispec.Platform) (image.InspectResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ImageInspect", Target: imageID, Platform: formatOCIPlatform(platform)})
	if r.imageInspectErr != nil || r.imageInspectOverride != nil {
		var resp image.InspectResponse
		if r.imageInspectOverride != nil {
			resp = *r.imageInspectOverride
		}
		return resp, r.imageInspectErr
	}
	if r.images[imageID] {
		parts := strings.Split(r.imagePlatforms[imageID], "/")
		if len(parts) < 2 {
			parts = []string{"linux", "amd64"}
		}
		variant := ""
		if len(parts) == 3 {
			variant = parts[2]
		}
		cfg := r.imageConfigs[imageID]
		return image.InspectResponse{
			ID:           "sha256:" + strings.Repeat("a", 64),
			RepoDigests:  []string{imageID},
			Os:           parts[0],
			Architecture: parts[1],
			Variant:      variant,
			Config: &dockerspec.DockerOCIImageConfig{
				ImageConfig: ocispec.ImageConfig{
					Entrypoint: append([]string(nil), cfg.Entrypoint...),
					Cmd:        append([]string(nil), cfg.Cmd...),
				},
			},
		}, nil
	}
	return image.InspectResponse{}, fmt.Errorf("%w: No such image: %s", cerrdefs.ErrNotFound, imageID)
}

func (r *applicationRuntimeDockerRunner) ImageRemove(_ context.Context, imageID string, _ mobyclient.ImageRemoveOptions) ([]image.DeleteResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ImageRemove", Target: imageID})
	delete(r.images, imageID)
	r.removedImages = append(r.removedImages, imageID)
	return nil, nil
}

func (r *applicationRuntimeDockerRunner) ContainerList(_ context.Context, options mobyclient.ContainerListOptions) ([]dockercontainer.Summary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ContainerList"})
	if ancestors := dockerFilterValues(options.Filters, "ancestor"); len(ancestors) > 0 {
		ref := ancestors[0]
		var out []dockercontainer.Summary
		for name, c := range r.containers {
			if c.Config != nil && c.Config.Image == ref {
				out = append(out, dockercontainer.Summary{ID: c.ID, Names: []string{"/" + name}})
			}
		}
		return out, nil
	}
	if names := dockerFilterValues(options.Filters, "name"); len(names) > 0 {
		name := strings.TrimSuffix(strings.TrimPrefix(names[0], "^/"), "$")
		c, ok := r.containers[name]
		if !ok {
			return nil, nil
		}
		return []dockercontainer.Summary{{ID: c.ID, Names: []string{"/" + name}}}, nil
	}
	return nil, nil
}

func (r *applicationRuntimeDockerRunner) ContainerInspect(_ context.Context, containerID string) (dockercontainer.InspectResponse, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ContainerInspect", Target: containerID})
	name := strings.TrimPrefix(containerID, "cid-")
	if c, ok := r.containers[name]; ok {
		return c, nil
	}
	for _, c := range r.containers {
		if c.ID == containerID {
			return c, nil
		}
	}
	return dockercontainer.InspectResponse{}, fmt.Errorf("no such container: %s", containerID)
}

func (r *applicationRuntimeDockerRunner) ContainerCreate(_ context.Context, config *dockercontainer.Config, hostConfig *dockercontainer.HostConfig, networkingConfig *network.NetworkingConfig, platform *ocispec.Platform, containerName string) (mobyclient.ContainerCreateResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ContainerCreate", Target: containerName, Platform: formatOCIPlatform(platform)})
	if containerName == "" {
		return mobyclient.ContainerCreateResult{}, fmt.Errorf("create requires containerName")
	}
	rec := recordedApplicationContainerCreate{
		Name:             containerName,
		Config:           config,
		HostConfig:       hostConfig,
		NetworkingConfig: networkingConfig,
		Platform:         platform,
	}
	if strings.HasPrefix(containerName, "opl-app-probe-") {
		r.probes = append(r.probes, rec)
		r.containers[containerName] = dockercontainer.InspectResponse{
			ID:     "cid-" + containerName,
			Name:   "/" + containerName,
			State:  &dockercontainer.State{Running: r.probeErr != nil, Status: dockercontainer.StateExited},
			Config: config,
		}
		return mobyclient.ContainerCreateResult{ID: "cid-" + containerName}, nil
	}
	if r.requireLocalImages && !r.images[config.Image] {
		return mobyclient.ContainerCreateResult{}, fmt.Errorf("%w: No such image: %s", cerrdefs.ErrNotFound, config.Image)
	}
	r.runs = append(r.runs, rec)
	imageConfig := r.imageConfigs[config.Image]
	entrypoint, cmd := append([]string(nil), imageConfig.Entrypoint...), append([]string(nil), imageConfig.Cmd...)
	if len(config.Entrypoint) > 0 {
		entrypoint, cmd = append([]string(nil), config.Entrypoint...), nil
	}
	if len(config.Cmd) > 0 {
		cmd = append([]string(nil), config.Cmd...)
	}
	argv := append(append([]string(nil), entrypoint...), cmd...)
	path := ""
	var processArgs []string
	if len(argv) > 0 {
		path, processArgs = argv[0], argv[1:]
	}
	var mounts []dockercontainer.MountPoint
	published := network.PortMap{}
	if hostConfig != nil {
		for _, m := range hostConfig.Mounts {
			mp := dockercontainer.MountPoint{
				Type:        m.Type,
				Source:      m.Source,
				Destination: m.Target,
				RW:          !m.ReadOnly,
			}
			if m.BindOptions != nil {
				mp.Propagation = m.BindOptions.Propagation
			}
			mounts = append(mounts, mp)
		}
		for portKey := range hostConfig.PortBindings {
			published[portKey] = []network.PortBinding{{HostIP: parseDockerHostIP("127.0.0.1"), HostPort: "31080"}}
		}
	}
	labels := make(map[string]string, len(config.Labels))
	for k, v := range config.Labels {
		labels[k] = v
	}
	r.containers[containerName] = dockercontainer.InspectResponse{
		ID:         "cid-" + containerName,
		Name:       "/" + containerName,
		Path:       path,
		Args:       processArgs,
		State:      &dockercontainer.State{Status: dockercontainer.StateCreated, Running: false},
		HostConfig: hostConfig,
		Config: &dockercontainer.Config{
			Image:      config.Image,
			Labels:     labels,
			Env:        append([]string(nil), config.Env...),
			User:       config.User,
			Entrypoint: entrypoint,
			Cmd:        cmd,
		},
		Mounts: mounts,
		NetworkSettings: &dockercontainer.NetworkSettings{
			Ports: published,
		},
	}
	if r.images == nil {
		r.images = map[string]bool{}
	}
	r.images[config.Image] = true
	return mobyclient.ContainerCreateResult{ID: "cid-" + containerName}, nil
}

func (r *applicationRuntimeDockerRunner) ContainerStart(_ context.Context, containerID string, _ mobyclient.ContainerStartOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ContainerStart", Target: containerID})
	name := strings.TrimPrefix(containerID, "cid-")
	c, exists := r.containers[name]
	if !exists {
		return fmt.Errorf("container missing: %s", containerID)
	}
	if strings.HasPrefix(name, "opl-app-probe-") {
		return r.probeErr
	}
	c.State = &dockercontainer.State{
		Status:    dockercontainer.StateRunning,
		Running:   true,
		StartedAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	}
	r.containers[name] = c
	return nil
}

func (r *applicationRuntimeDockerRunner) ContainerStop(_ context.Context, containerID string, _ mobyclient.ContainerStopOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ContainerStop", Target: containerID})
	name := strings.TrimPrefix(containerID, "cid-")
	c, exists := r.containers[name]
	if !exists {
		return fmt.Errorf("container missing: %s", containerID)
	}
	c.State = &dockercontainer.State{
		Status:    dockercontainer.StateExited,
		Running:   false,
		StartedAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339Nano),
	}
	r.containers[name] = c
	return nil
}

func (r *applicationRuntimeDockerRunner) ContainerRemove(_ context.Context, containerID string, _ mobyclient.ContainerRemoveOptions) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ContainerRemove", Target: containerID})
	name := strings.TrimPrefix(containerID, "cid-")
	if _, exists := r.containers[name]; !exists {
		return fmt.Errorf("container missing: %s", containerID)
	}
	delete(r.containers, name)
	return nil
}

func (r *applicationRuntimeDockerRunner) ContainerWait(_ context.Context, containerID string, _ dockercontainer.WaitCondition) (<-chan dockercontainer.WaitResponse, <-chan error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ContainerWait", Target: containerID})
	statusCh := make(chan dockercontainer.WaitResponse, 1)
	errCh := make(chan error, 1)
	if r.probeErr != nil {
		errCh <- r.probeErr
		return statusCh, errCh
	}
	statusCh <- dockercontainer.WaitResponse{StatusCode: 0}
	return statusCh, errCh
}

func (r *applicationRuntimeDockerRunner) ContainerLogs(_ context.Context, containerID string, _ mobyclient.ContainerLogsOptions) (io.ReadCloser, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, applicationRuntimeDockerCall{Method: "ContainerLogs", Target: containerID})
	return encodeDockerStdoutStream([]byte(fmt.Sprintf(`{"ready":%t}`, r.probeReady))), nil
}

func (r *applicationRuntimeDockerRunner) runCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.runs)
}

func (r *applicationRuntimeDockerRunner) setContainerState(name, status string, running bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.containers[name]
	if !ok {
		panic("container missing: " + name)
	}
	c.State = &dockercontainer.State{Status: dockercontainer.ContainerState(status), Running: running}
	r.containers[name] = c
}

func (r *applicationRuntimeDockerRunner) runCreateForComponent(name string) *recordedApplicationContainerCreate {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.runs {
		if r.runs[i].Config != nil && r.runs[i].Config.Labels["opl.component.name"] == name {
			rec := r.runs[i]
			return &rec
		}
	}
	return nil
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
	runner := &applicationRuntimeDockerRunner{containers: map[string]dockercontainer.InspectResponse{}, probeReady: true}
	provider := newLocalDockerProvider(LocalDockerProviderConfig{
		GatewaySecretRoot: localDockerSecretTestRoot(t), HostStorageRoot: root, StorageQuotaBackend: localDockerStorageTestQuota(root),
		ApplicationProbeImage: "repo.example/opl-cloud@sha256:" + strings.Repeat("c", 64),
		WorkloadOwnership:     &fakeLocalDockerWorkloadOwnership{},
	}, runner)
	paths, err := provider.storagePaths(workspaceID)
	if err != nil {
		t.Fatal(err)
	}
	return provider, runner, paths
}

func hasMount(mounts []mount.Mount, mountType mount.Type, source, target string) bool {
	for _, m := range mounts {
		if m.Type == mountType && m.Target == target && (source == "" || m.Source == source) {
			return true
		}
	}
	return false
}

func TestLocalDockerApplicationRuntimeDependencyRunsOwnSpec(t *testing.T) {
	revision := applicationRevisionForTest()
	revision.Dependencies = []contracts.WorkspaceApplicationDependency{{
		Name:  "retrieval",
		Image: "repo.example/apps/retrieval@sha256:" + strings.Repeat("b", 64),
		Ports: []contracts.WorkspaceApplicationDependencyPort{{Name: "grpc", Port: 9200, Protocol: "TCP"}},
		Command: contracts.WorkspaceApplicationDependencyCommand{
			Entrypoint: []string{"/bin/serve", "worker"},
			Args:       []string{"--port=9200"},
			Env:        map[string]string{"RETRIEVAL_MODE": "local"},
		},
		ScratchMounts: []contracts.WorkspaceApplicationDependencyMount{{Name: "cache", MountPath: "/cache"}},
	}}
	provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	input := applicationRuntimeInput("app-runtime-dep-spec", revision)
	observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, ComputeAllocation{
		ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running",
	}, StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if observation.Status != "ready" {
		t.Fatalf("observation=%#v", observation)
	}
	dep := runner.runCreateForComponent("retrieval")
	if dep == nil {
		t.Fatal("missing retrieval container create")
	}
	if !reflect.DeepEqual([]string(dep.Config.Entrypoint), []string{"/bin/serve"}) ||
		!reflect.DeepEqual([]string(dep.Config.Cmd), []string{"worker", "--port=9200"}) ||
		dep.Config.Image != revision.Dependencies[0].Image {
		t.Fatalf("dependency image/command boundary invalid: image=%q entrypoint=%q cmd=%q", dep.Config.Image, dep.Config.Entrypoint, dep.Config.Cmd)
	}
	if !slices.Contains(dep.Config.Env, "RETRIEVAL_MODE=local") {
		t.Fatalf("dependency env missing RETRIEVAL_MODE=local: %v", dep.Config.Env)
	}
	if _, exposed := dep.Config.ExposedPorts[network.MustParsePort("9200/tcp")]; !exposed {
		t.Fatalf("dependency missing exposed port 9200/tcp: %v", dep.Config.ExposedPorts)
	}
	if !hasMount(dep.HostConfig.Mounts, mount.TypeTmpfs, "", "/cache") {
		t.Fatalf("dependency missing tmpfs mount /cache: %#v", dep.HostConfig.Mounts)
	}
	hasAlias := false
	if dep.NetworkingConfig != nil {
		for _, ep := range dep.NetworkingConfig.EndpointsConfig {
			if slices.Contains(ep.Aliases, "retrieval") {
				hasAlias = true
			}
		}
	}
	if !hasAlias {
		t.Fatalf("missing declared service alias: %#v", dep.NetworkingConfig)
	}
	// 依赖不得继承主组件的挂载与入口发布
	if hasMount(dep.HostConfig.Mounts, mount.TypeBind, "", "/data") || len(dep.HostConfig.PortBindings) > 0 {
		t.Fatalf("dependency inherited main-component facts: mounts=%#v ports=%#v", dep.HostConfig.Mounts, dep.HostConfig.PortBindings)
	}
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
	mainCreate := runner.runCreateForComponent("main")
	wantDataSource := filepath.Join(paths.Data, contracts.WorkspaceApplicationDataDirectory(input.DataBindingID), "data")
	if mainCreate == nil || !strings.HasPrefix(string(mainCreate.HostConfig.NetworkMode), "opl-compute-") ||
		!hasMount(mainCreate.HostConfig.Mounts, mount.TypeBind, wantDataSource, "/data") ||
		mainCreate.Config.Image != revision.Image {
		t.Fatalf("main create=%#v", mainCreate)
	}
	depCreate := runner.runCreateForComponent("retrieval")
	if depCreate == nil || !strings.HasPrefix(depCreate.Name, "opl-app-retrieval-") || depCreate.Config.Image != revision.Dependencies[0].Image {
		t.Fatalf("dependency create=%#v", depCreate)
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
	mainContainerName, nameErr := localDockerApplicationComponentName(input.RuntimeOperationID, "main")
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
	mainCreate := runner.runCreateForComponent("main")
	bindings := mainCreate.HostConfig.PortBindings[network.MustParsePort("8080/tcp")]
	if len(bindings) != 1 || !bindings[0].HostIP.IsValid() || bindings[0].HostIP.String() != "127.0.0.1" {
		t.Fatalf("main create must publish the declared port: %#v", mainCreate.HostConfig.PortBindings)
	}
	if localEntryURL(observation) == "" || !strings.HasPrefix(localEntryURL(observation), "http://127.0.0.1:") {
		t.Fatalf("entry URL=%q, want the published host port", localEntryURL(observation))
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
	if err != nil || observation.Status != "pending" || localEntryURL(observation) != "" {
		t.Fatalf("pending=%#v err=%v", observation, err)
	}
	mainCreate := runner.runCreateForComponent("main")
	depCreate := runner.runCreateForComponent("retrieval")
	if len(mainCreate.HostConfig.PortBindings) > 0 ||
		!reflect.DeepEqual([]string(mainCreate.Config.Entrypoint), []string{"/app/server"}) ||
		!reflect.DeepEqual([]string(mainCreate.Config.Cmd), []string{"--serve"}) {
		t.Fatalf("main=%#v", mainCreate)
	}
	if len(depCreate.Config.Entrypoint) > 0 || len(depCreate.Config.Cmd) > 0 || len(depCreate.HostConfig.Mounts) > 0 || len(depCreate.HostConfig.PortBindings) > 0 {
		t.Fatalf("main configuration leaked into dependency: %#v", depCreate)
	}
	if len(runner.probes) != 1 {
		t.Fatalf("probe calls=%d", len(runner.probes))
	}
	probe := runner.probes[0]
	if !strings.HasPrefix(string(probe.HostConfig.NetworkMode), "container:cid-") ||
		!probe.HostConfig.ReadonlyRootfs ||
		!slices.Contains([]string(probe.HostConfig.CapDrop), "ALL") ||
		!slices.Contains(probe.HostConfig.SecurityOpt, "no-new-privileges") ||
		!reflect.DeepEqual([]string(probe.Config.Entrypoint), []string{"node"}) ||
		probe.Config.Image != provider.applicationProbeImage {
		t.Fatalf("probe spec invalid: config=%#v hostConfig=%#v", probe.Config, probe.HostConfig)
	}
	if len(probe.HostConfig.Mounts) > 0 || len(probe.HostConfig.PortBindings) > 0 {
		t.Fatalf("probe unexpectedly exposes data or ports: %#v", probe.HostConfig)
	}
	runner.probeReady = true
	ready, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || ready.Status != "ready" || localEntryURL(ready) != "" {
		t.Fatalf("ready=%#v err=%v", ready, err)
	}
	runner.probeErr = errors.New("probe runtime unavailable")
	if _, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input); err == nil {
		t.Fatal("probe execution failure reported ready")
	}
	for name := range runner.containers {
		if strings.HasPrefix(name, "opl-app-probe-") {
			t.Fatalf("owned probe leaked after execution or failure: %s", name)
		}
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
	if err != nil || observation.Status != "pending" || len(runner.probes) != 0 || localEntryURL(observation) != "" {
		t.Fatalf("delayed=%#v err=%v probeCalls=%d", observation, err, len(runner.probes))
	}
	provider.now = func() time.Time { return time.Now() }
	observation, err = provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || observation.Status != "ready" || localEntryURL(observation) == "" || len(runner.probes) != 1 {
		t.Fatalf("ready=%#v err=%v probeCalls=%d", observation, err, len(runner.probes))
	}
	name, _ := localDockerApplicationComponentName(input.RuntimeOperationID, "main")
	c := runner.containers[name]
	c.Config.Image = "other-image"
	runner.containers[name] = c
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
			mainCreate := runner.runCreateForComponent("main")
			ports := mainCreate.HostConfig.PortBindings
			if _, hasUDP := ports[network.MustParsePort("5353/udp")]; hasUDP || (entry == "" && len(ports) > 0) || (entry != "" && len(ports[network.MustParsePort("8080/tcp")]) == 0) {
				t.Fatalf("ports=%#v", ports)
			}
			if (entry == "") != (localEntryURL(observation) == "") {
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
				name, err := localDockerApplicationComponentName(input.RuntimeOperationID, component)
				if err != nil {
					t.Fatal(err)
				}
				c := runner.containers[name]
				c.Config.Labels["opl.account.id"] = account
				runner.containers[name] = c
				mutations := runner.runCount()
				observation, err = provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
				if err == nil || err.Error() != "local_docker_application_component_conflict" || observation.Status == "ready" || runner.runCount() != mutations {
					t.Fatalf("drift=%#v err=%v mutationCount=%d", observation, err, runner.runCount())
				}
			})
		}
	}
}

func TestLocalDockerApplicationRuntimeDependencyDeclaredHealth(t *testing.T) {
	for _, mode := range []string{"ready", "not-ready", "delay", "exited", "probe-image-missing"} {
		t.Run(mode, func(t *testing.T) {
			provider, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
			revision := applicationRevisionForTest()
			revision.HealthChecks = nil
			revision.Dependencies[0].HealthChecks = []contracts.WorkspaceApplicationDependencyHealthCheck{
				{Type: "tcp", Port: 9200, InitialDelaySeconds: 5}, {Type: "http", Port: 9201, Path: "/health", InitialDelaySeconds: 10},
			}
			input := applicationRuntimeInput("dependency-health", revision)
			if mode == "probe-image-missing" {
				provider.applicationProbeImage = ""
			}
			if mode == "delay" {
				provider.now = func() time.Time { return time.Now().Add(-time.Hour + 7*time.Second) }
			}
			if mode == "not-ready" {
				runner.probeReady = false
			}
			observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input,
				ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running"},
				StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
			if mode == "probe-image-missing" {
				if err == nil || err.Error() != "local_docker_application_probe_image_required" || runner.runCount() != 0 {
					t.Fatalf("err=%v runs=%d", err, runner.runCount())
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mode == "delay" {
				if observation.Status != "pending" || len(runner.probes) != 0 {
					t.Fatalf("observation=%+v probes=%d", observation, len(runner.probes))
				}
				return
			}
			if len(runner.probes) != 1 {
				t.Fatalf("dependency must probe inside container network: %d", len(runner.probes))
			}
			probe := runner.probes[0]
			var checks []contracts.WorkspaceApplicationDependencyHealthCheck
			if err := json.Unmarshal([]byte(probe.Config.Cmd[len(probe.Config.Cmd)-1]), &checks); err != nil || len(checks) != 2 || checks[0].Type != "tcp" || checks[1].Path != "/health" {
				t.Fatalf("checks=%+v err=%v", checks, err)
			}
			dependencyName, _ := localDockerApplicationComponentNameForInput(input, revision.Dependencies[0].Name)
			if string(probe.HostConfig.NetworkMode) != "container:cid-"+dependencyName {
				t.Fatalf("wrong probe network: %q", probe.HostConfig.NetworkMode)
			}
			if mode == "not-ready" {
				if observation.Status != "pending" {
					t.Fatalf("observation=%+v", observation)
				}
				return
			}
			if observation.Status != "ready" {
				t.Fatalf("observation=%+v", observation)
			}
			if mode == "exited" {
				runner.setContainerState(dependencyName, "exited", false)
				observation, err = provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
				if err != nil || observation.Status != "failed" || len(runner.probes) != 1 || observation.Components[1].LastError != "container exited" {
					t.Fatalf("observation=%+v err=%v probes=%d", observation, err, len(runner.probes))
				}
			}
		})
	}
}

// The local provider must bound a declared component exactly like the hosted
// provider, including a hard memory ceiling with no swap headroom.
func TestLocalDockerApplicationResourceArgsFollowDeclaredEnvelope(t *testing.T) {
	empty := &dockercontainer.HostConfig{}
	applyLocalDockerApplicationResources(empty, contracts.WorkspaceApplicationCompute{})
	if empty.Resources.NanoCPUs != 0 || empty.Resources.Memory != 0 || empty.Resources.MemorySwap != 0 || empty.Resources.MemoryReservation != 0 {
		t.Fatalf("an undeclared envelope must leave resource limits zeroed: %#v", empty.Resources)
	}
	bounded := &dockercontainer.HostConfig{}
	applyLocalDockerApplicationResources(bounded, contracts.WorkspaceApplicationCompute{
		CPURequestMilli: 500, CPULimitMilli: 2000, MemoryRequestBytes: 2 << 30, MemoryLimitBytes: 3 << 30,
	})
	if bounded.Resources.NanoCPUs != 2_000_000_000 || bounded.Resources.Memory != 3<<30 ||
		bounded.Resources.MemorySwap != 3<<30 || bounded.Resources.MemoryReservation != 2<<30 {
		t.Fatalf("resources=%#v", bounded.Resources)
	}
}

// localEntryURL reads the endpoint a local provider publishes itself. A local
// provider binds a host port, so it reports a URL rather than a gateway route.
func localEntryURL(observation contracts.WorkspaceApplicationRuntimeObservation) string {
	if observation.Entry == nil {
		return ""
	}
	return observation.Entry.URL
}

func ensureApplicationCommandRuntime(p *LocalDockerProvider, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	return p.EnsureWorkspaceApplicationRuntime(context.Background(), input,
		ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", Status: "running"},
		StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "workspace-alpha", SizeGB: 10, Status: "ready"})
}

func TestLocalDockerApplicationMainCommandPreservesImageCMD(t *testing.T) {
	for _, test := range []struct {
		name       string
		entrypoint []string
		cmd        []string
		platform   string
	}{
		{name: "image defaults", cmd: []string{"serve", "--http=8080"}},
		{name: "single override", entrypoint: []string{"/wrapper"}, cmd: []string{"serve", "--http=8080"}},
		{name: "entrypoint tail precedes CMD", entrypoint: []string{"/wrapper", "--wrapper-flag", "two words", ""}, cmd: []string{"serve", "--literal=$HOME", ""}},
		{name: "empty CMD", entrypoint: []string{"/wrapper", "--tail"}, cmd: []string{}},
		{name: "null CMD", entrypoint: []string{"/wrapper"}},
		{name: "platform variant", entrypoint: []string{"/wrapper"}, cmd: []string{"serve"}, platform: "linux/arm/v7"},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
			revision := applicationRevisionForTest()
			revision.Entrypoint, revision.Dependencies = test.entrypoint, nil
			if test.platform != "" {
				revision.Platform = test.platform
			}
			runner.imageConfigs = map[string]applicationRuntimeImageConfig{revision.Image: {Entrypoint: []string{"/image-entrypoint"}, Cmd: test.cmd}}
			input := applicationRuntimeInput("main-command", revision)
			observation, err := ensureApplicationCommandRuntime(p, input)
			if err != nil || observation.Status != "ready" {
				t.Fatalf("observation=%+v err=%v", observation, err)
			}
			mainCreate := runner.runCreateForComponent("main")
			tail := []string(mainCreate.Config.Cmd)
			wantEntrypoint := revision.Entrypoint
			var wantTail []string
			if len(wantEntrypoint) == 0 {
				wantEntrypoint = []string{"/image-entrypoint"}
			} else {
				wantTail = append(append([]string{}, wantEntrypoint[1:]...), test.cmd...)
			}
			if !reflect.DeepEqual(tail, wantTail) && !(len(tail) == 0 && len(wantTail) == 0) {
				t.Fatalf("docker command tail=%q want=%q", tail, wantTail)
			}
			name, _ := localDockerApplicationComponentNameForInput(input, "main")
			actual := runner.containers[name]
			wantArgv := append(append([]string{}, wantEntrypoint...), test.cmd...)
			if got := append([]string{actual.Path}, actual.Args...); !reflect.DeepEqual(got, wantArgv) {
				t.Fatalf("actual argv=%q want=%q", got, wantArgv)
			}
			pull, inspect, create := -1, -1, -1
			for i, call := range runner.calls {
				if call.Method == "ImagePull" {
					pull = i
					if call.Target != revision.Image || call.Platform != revision.Platform {
						t.Fatalf("pull=%+v", call)
					}
				}
				if call.Method == "ImageInspect" {
					if inspect == -1 {
						inspect = i
					}
					if call.Target != revision.Image || call.Platform != revision.Platform {
						t.Fatalf("image inspect=%+v", call)
					}
				}
				if call.Method == "ContainerCreate" && call.Target == mainCreate.Name {
					create = i
				}
			}
			if len(revision.Entrypoint) == 0 {
				if pull != -1 || inspect != -1 || len(mainCreate.Config.Entrypoint) != 0 {
					t.Fatalf("image defaults overridden: pull=%d inspect=%d entrypoint=%q", pull, inspect, mainCreate.Config.Entrypoint)
				}
			} else if pull < 0 || inspect <= pull || create <= inspect {
				t.Fatalf("expected pull -> inspect -> create: pull=%d inspect=%d create=%d", pull, inspect, create)
			}
			if _, err := ensureApplicationCommandRuntime(p, input); err != nil || runner.runCount() != 1 {
				t.Fatalf("replay err=%v runs=%d", err, runner.runCount())
			}
		})
	}
}

func TestLocalDockerApplicationColdImageWithDefaultCommand(t *testing.T) {
	pullFailure := errors.New("registry unavailable")
	for _, test := range []struct {
		name    string
		pullErr error
	}{
		{name: "cold engine"},
		{name: "registry unavailable", pullErr: pullFailure},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
			revision := applicationRevisionForTest()
			revision.Entrypoint, revision.Dependencies = nil, nil
			runner.requireLocalImages = true
			runner.imagePullErr = test.pullErr
			runner.imageConfigs = map[string]applicationRuntimeImageConfig{
				revision.Image: {Entrypoint: []string{"/agent/bin/runtime"}, Cmd: []string{"serve"}},
			}
			input := applicationRuntimeInput("cold-build", revision)
			observation, err := ensureApplicationCommandRuntime(p, input)
			name, _ := localDockerApplicationComponentNameForInput(input, "main")
			var imageCalls []applicationRuntimeDockerCall
			for _, call := range runner.calls {
				if call.Method == "ImagePull" || (call.Method == "ContainerCreate" && call.Target == name) {
					imageCalls = append(imageCalls, call)
				}
			}
			create := applicationRuntimeDockerCall{Method: "ContainerCreate", Target: name, Platform: revision.Platform}
			pull := applicationRuntimeDockerCall{Method: "ImagePull", Target: revision.Image, Platform: revision.Platform}
			want := []applicationRuntimeDockerCall{create, pull}
			if test.pullErr != nil {
				if !errors.Is(err, test.pullErr) || observation.Status == "ready" || runner.runCount() != 0 || !slices.Equal(imageCalls, want) {
					t.Fatalf("observation=%+v err=%v calls=%+v", observation, err, imageCalls)
				}
				return
			}
			want = append(want, create)
			if err != nil || observation.Status != "ready" || runner.runCount() != 1 || !slices.Equal(imageCalls, want) {
				t.Fatalf("observation=%+v err=%v calls=%+v", observation, err, imageCalls)
			}
			container := runner.containers[name]
			if container.Path != "/agent/bin/runtime" || !slices.Equal(container.Args, []string{"serve"}) {
				t.Fatalf("image command changed: %s %q", container.Path, container.Args)
			}
			firstRead := len(runner.calls)
			if _, err := ensureApplicationCommandRuntime(p, input); err != nil || runner.runCount() != 1 {
				t.Fatalf("replay err=%v runs=%d", err, runner.runCount())
			}
			for _, call := range runner.calls[firstRead:] {
				if call.Method == "ImagePull" {
					t.Fatalf("replay pulled image: %+v", call)
				}
			}
		})
	}
}

func TestLocalDockerApplicationMainCommandKeepsDependencyArgs(t *testing.T) {
	p, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	revision := applicationRevisionForTest()
	revision.Entrypoint = []string{"/wrapper", "--main-tail"}
	revision.Dependencies[0].Command = contracts.WorkspaceApplicationDependencyCommand{
		Entrypoint: []string{"/dependency", "--dependency-tail"}, Args: []string{"worker", "--port=9200"},
	}
	runner.imageConfigs = map[string]applicationRuntimeImageConfig{
		revision.Image: {Cmd: []string{"main-default"}}, revision.Dependencies[0].Image: {Cmd: []string{"dependency-default"}},
	}
	if _, err := ensureApplicationCommandRuntime(p, applicationRuntimeInput("dependency-command", revision)); err != nil {
		t.Fatal(err)
	}
	dep := runner.runCreateForComponent("retrieval")
	wantCmd := []string{"--dependency-tail", "worker", "--port=9200"}
	if !reflect.DeepEqual([]string(dep.Config.Entrypoint), []string{"/dependency"}) || !reflect.DeepEqual([]string(dep.Config.Cmd), wantCmd) {
		t.Fatalf("dependency command changed: entrypoint=%q cmd=%q", dep.Config.Entrypoint, dep.Config.Cmd)
	}
	for _, call := range runner.calls {
		if (call.Method == "ImageInspect" || call.Method == "ImagePull") && call.Target == revision.Dependencies[0].Image {
			t.Fatalf("dependency unexpectedly resolved image CMD: %+v", call)
		}
	}
}

func TestLocalDockerApplicationMainCommandRejectsImageReadFailure(t *testing.T) {
	readFailure := errors.New("image inspect unavailable")
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "inspect error", err: readFailure},
		{name: "missing id"},
		{name: "missing config"},
		{name: "wrong digest"},
		{name: "wrong platform"},
		{name: "wrong variant"},
	} {
		t.Run(test.name, func(t *testing.T) {
			p, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
			revision := applicationRevisionForTest()
			revision.Entrypoint, revision.Dependencies = []string{"/wrapper"}, nil
			resp := &image.InspectResponse{
				ID:           "sha256:" + strings.Repeat("a", 64),
				RepoDigests:  []string{revision.Image},
				Os:           "linux",
				Architecture: "amd64",
				Config: &dockerspec.DockerOCIImageConfig{
					ImageConfig: ocispec.ImageConfig{Cmd: []string{"serve"}},
				},
			}
			switch test.name {
			case "missing id":
				resp.ID = ""
			case "missing config":
				resp.Config = nil
			case "wrong digest":
				resp.RepoDigests = []string{"other@sha256:" + strings.Repeat("a", 64)}
			case "wrong platform":
				resp.Architecture = "arm64"
			case "wrong variant":
				resp.Variant = "v8"
			}
			runner.imageInspectOverride = resp
			runner.imageInspectErr = test.err
			observation, err := ensureApplicationCommandRuntime(p, applicationRuntimeInput("image-command-failure", revision))
			if err == nil || observation.Status == "ready" || runner.runCount() != 0 {
				t.Fatalf("observation=%+v err=%v runs=%d", observation, err, runner.runCount())
			}
			if test.err != nil && !errors.Is(err, test.err) {
				t.Fatalf("lost inspect failure: %v", err)
			}
		})
	}
}

func TestLocalDockerApplicationMainCommandRejectsExistingArgvDrift(t *testing.T) {
	for _, operation := range []string{"ensure", "read"} {
		for _, drift := range []string{"legacy missing CMD", "reordered args", "different executable", "extra arg", "image inspect failure"} {
			t.Run(operation+"/"+drift, func(t *testing.T) {
				p, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
				revision := applicationRevisionForTest()
				revision.Entrypoint, revision.Dependencies = []string{"/wrapper", "--tail"}, nil
				runner.imageConfigs = map[string]applicationRuntimeImageConfig{revision.Image: {Cmd: []string{"serve", "--http"}}}
				input := applicationRuntimeInput("existing-command", revision)
				if _, err := ensureApplicationCommandRuntime(p, input); err != nil {
					t.Fatal(err)
				}
				name, _ := localDockerApplicationComponentNameForInput(input, "main")
				c := runner.containers[name]
				switch drift {
				case "legacy missing CMD":
					c.Args = []string{"--tail"}
					c.Config.Cmd = []string{"--tail"}
				case "reordered args":
					c.Args = []string{"serve", "--tail", "--http"}
				case "different executable":
					c.Path = "/different"
				case "extra arg":
					c.Args = []string{"--tail", "serve", "--http", "extra"}
				case "image inspect failure":
					runner.imageInspectErr = errors.New("image read unavailable")
				}
				runner.containers[name] = c
				callCount := len(runner.calls)
				var observation contracts.WorkspaceApplicationRuntimeObservation
				var err error
				if operation == "ensure" {
					observation, err = ensureApplicationCommandRuntime(p, input)
				} else {
					observation, err = p.ReadWorkspaceApplicationRuntime(context.Background(), input)
				}
				if err == nil || observation.Status == "ready" || runner.runCount() != 1 {
					t.Fatalf("drift accepted: observation=%+v err=%v runs=%d", observation, err, runner.runCount())
				}
				if drift != "image inspect failure" && err.Error() != "local_docker_application_component_conflict" {
					t.Fatalf("expected command conflict: %v", err)
				}
				for _, call := range runner.calls[callCount:] {
					if call.Method != "ImageInspect" && call.Method != "ContainerList" && call.Method != "ContainerInspect" {
						t.Fatalf("existing container was mutated: %+v", call)
					}
				}
			})
		}
	}
}

func TestLocalDockerApplicationMainCommandAcceptsOmittedImageCMD(t *testing.T) {
	p, runner, _ := applicationRuntimeProviderFixture(t, "workspace-alpha")
	revision := applicationRevisionForTest()
	runner.imageInspectOverride = &image.InspectResponse{
		ID:           "sha256:" + strings.Repeat("a", 64),
		RepoDigests:  []string{revision.Image},
		Os:           "linux",
		Architecture: "amd64",
		Config:       &dockerspec.DockerOCIImageConfig{},
	}
	args, err := p.localDockerApplicationImageCommand(context.Background(), revision.Image, "linux/amd64")
	if err != nil || len(args) != 0 {
		t.Fatalf("args=%v err=%v", args, err)
	}
}
