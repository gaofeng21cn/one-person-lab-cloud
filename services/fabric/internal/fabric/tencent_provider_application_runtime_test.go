package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// fakeTencentKubectl records applies and materialises Deployments so the
// readback path sees what was applied.
type fakeTencentKubectl struct {
	mu          sync.Mutex
	deployments map[string]map[string]any
	applies     [][]map[string]any
}

func (f *fakeTencentKubectl) call(_ context.Context, args []string, stdin []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch args[0] {
	case "apply":
		var manifest struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(stdin, &manifest); err != nil {
			return nil, err
		}
		f.applies = append(f.applies, manifest.Items)
		for _, item := range manifest.Items {
			if item["kind"] == "Deployment" {
				if meta, ok := item["metadata"].(map[string]any); ok {
					if name, ok := meta["name"].(string); ok {
						if previous, exists := f.deployments[name]; exists {
							// A real apply updates the spec and keeps the live status.
							if status, hasStatus := previous["status"]; hasStatus {
								item["status"] = status
							}
						}
						f.deployments[name] = item
					}
				}
			}
		}
		return nil, nil
	case "get":
		items := make([]map[string]any, 0, len(f.deployments))
		for _, deployment := range f.deployments {
			items = append(items, deployment)
		}
		body, err := json.Marshal(map[string]any{"apiVersion": "v1", "kind": "List", "items": items})
		if err != nil {
			return nil, err
		}
		return body, nil
	}
	return nil, errors.New("unexpected kubectl call: " + strings.Join(args, " "))
}

func (f *fakeTencentKubectl) applyCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.applies)
}

func (f *fakeTencentKubectl) setAllReady() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for name, deployment := range f.deployments {
		status, ok := deployment["status"].(map[string]any)
		if !ok {
			status = map[string]any{}
			deployment["status"] = status
		}
		status["readyReplicas"] = 1
		f.deployments[name] = deployment
		_ = name
	}
}

func tencentApplicationRuntimeFixture(t *testing.T) (*TencentProvider, *fakeTencentKubectl, WorkspaceApplicationRuntimeInput) {
	t.Helper()
	setProtectedResourceEnv(t)
	provider := NewTencentProvider()
	fake := &fakeTencentKubectl{deployments: map[string]map[string]any{}}
	provider.kubectl = fake.call
	revision := applicationRevisionForTest()
	input := applicationRuntimeInput("app-runtime-tencent", revision)
	input.WorkspaceID = "ws-alpha"
	return provider, fake, input
}

func tencentApplicationCompute() ComputeAllocation {
	return ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", Status: "running", NodeName: "10.66.1.7", NodePoolID: "np-basic", PackageID: "basic"}
}

func tencentApplicationVolume() StorageVolume {
	return StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", SizeGB: 10, Status: "ready", ProviderResourceID: "pvc/opl-data-storage-alpha"}
}

func TestTencentApplicationRuntimeEnsureAppliesAndReportsPending(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume())
	if !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("first ensure err=%v, want pending", err)
	}
	if observation.Status != "pending" || len(observation.Components) != 2 || observation.Components[0].State != "pending" {
		t.Fatalf("pending observation=%#v", observation)
	}
	if fake.applyCount() != 1 {
		t.Fatalf("apply calls=%d", fake.applyCount())
	}
	manifest := fake.applies[0]
	if len(manifest) != 6 {
		t.Fatalf("manifest items=%d, want two deployments, two services, one network policy and one ingress", len(manifest))
	}
	kinds := map[string]int{}
	for _, item := range manifest {
		kinds[item["kind"].(string)]++
	}
	if kinds["Deployment"] != 2 || kinds["Service"] != 2 || kinds["NetworkPolicy"] != 1 || kinds["Ingress"] != 1 {
		t.Fatalf("manifest kinds=%v", kinds)
	}
}

func TestTencentApplicationRuntimeReadyAfterDeploymentsConverge(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("first ensure err=%v", err)
	}
	fake.setAllReady()
	observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume())
	if err != nil || observation.Status != "ready" || len(observation.Components) != 2 {
		t.Fatalf("ready observation=%#v err=%v", observation, err)
	}
	if observation.Components[0].State != "ready" || len(observation.Components[0].Ports) != 1 {
		t.Fatalf("ready component=%#v", observation.Components[0])
	}
}

func TestTencentApplicationRuntimeReadbackReportsAbsent(t *testing.T) {
	provider, _, input := tencentApplicationRuntimeFixture(t)
	observation, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if observation.Status != "absent" || len(observation.Components) != 2 || observation.Components[0].State != "absent" {
		t.Fatalf("absent observation=%#v", observation)
	}
}

func TestTencentApplicationRuntimeRejectsImageDrift(t *testing.T) {
	provider, _, input := tencentApplicationRuntimeFixture(t)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("first ensure err=%v", err)
	}
	drifted := input
	drifted.Revision.Image = "repo.example/apps/knowledge@sha256:" + strings.Repeat("f", 64)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), drifted, tencentApplicationCompute(), tencentApplicationVolume()); err == nil || !strings.Contains(err.Error(), "tencent_application_component_conflict") {
		t.Fatalf("drift error=%v, want component conflict", err)
	}
}

func TestTencentApplicationRuntimeManifestBindsWorkspaceData(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("first ensure err=%v", err)
	}
	encoded, err := json.Marshal(fake.applies[0])
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(encoded)
	if !strings.Contains(manifest, "persistentVolumeClaim") || !strings.Contains(manifest, `"subPath":"main/data"`) {
		t.Fatalf("manifest must bind the workspace PVC through per-component subPaths: %s", manifest)
	}
	if !strings.Contains(manifest, "readinessProbe") || !strings.Contains(manifest, `"medium":"Memory"`) {
		t.Fatalf("manifest must carry the declared readiness probe and memory-backed scratch mounts: %s", manifest)
	}
	if !strings.Contains(manifest, "kubernetes.io/hostname") {
		t.Fatalf("manifest must pin components to the workspace node: %s", manifest)
	}
}

func TestTencentApplicationRuntimeIngressHonorsExposurePolicy(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("first ensure err=%v", err)
	}
	encoded, err := json.Marshal(fake.applies[0])
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(encoded)
	if !strings.Contains(manifest, `"kind":"Ingress"`) {
		t.Fatalf("application exposure must create the entry ingress: %s", manifest)
	}
	host := workspaceApplicationIngressHost(input)
	if !strings.Contains(manifest, host) || !strings.Contains(manifest, `"name":"`+workspaceApplicationComponentResourceName(input, contracts.WorkspaceApplicationComponentMain)+`"`) {
		t.Fatalf("ingress must route the application host to the main component service: %s", manifest)
	}

	fake.applies = nil
	private := input
	private.Revision.ExposurePolicy = "cloud_private"
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), private, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("private ensure err=%v", err)
	}
	encodedPrivate, err := json.Marshal(fake.applies[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encodedPrivate), `"kind":"Ingress"`) {
		t.Fatalf("cloud-private application must stay cluster-internal: %s", encodedPrivate)
	}
}
