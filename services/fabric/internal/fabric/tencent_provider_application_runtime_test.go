package fabric

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	contracts "opl-cloud/packages/contracts/go"
)

// fakeTencentKubectl records applies and materialises Deployments so the
// readback path sees what was applied.
type fakeTencentKubectl struct {
	mu          sync.Mutex
	deployments map[string]map[string]any
	resources   map[string]map[string]any
	applies     [][]map[string]any
	mutations   [][]string
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
		if err := validateApplicationManifestObjects(manifest.Items); err != nil {
			return nil, err
		}
		f.applies = append(f.applies, manifest.Items)
		for _, item := range manifest.Items {
			meta := item["metadata"].(map[string]any)
			name := meta["name"].(string)
			meta["uid"] = stringValue(item["kind"]) + ":" + name
			if item["kind"] == "Deployment" {
				meta["generation"] = float64(1)
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
			} else {
				key := stringValue(item["kind"]) + ":" + name
				if previous := f.resources[key]; previous != nil {
					item["status"] = previous["status"]
				}
				f.resources[key] = item
			}
		}
		return nil, nil

	case "create":
		var item map[string]any
		if err := json.Unmarshal(stdin, &item); err != nil {
			return nil, err
		}
		name := stringValue(nested(item, "metadata", "name"))
		if item["kind"] == "ConfigMap" {
			item["metadata"].(map[string]any)["uid"] = "ConfigMap:" + name
			if f.resources["ConfigMap:"+name] != nil {
				return nil, errors.New("already exists")
			}
			f.resources["ConfigMap:"+name] = item
			return nil, nil
		}
		if item["kind"] != "Secret" {
			return nil, errors.New("unexpected create")
		}
		item["metadata"].(map[string]any)["uid"] = "Secret:" + name
		data := map[string]any{}
		for key, value := range item["stringData"].(map[string]any) {
			data[key] = base64.StdEncoding.EncodeToString([]byte(stringValue(value)))
		}
		item["data"] = data
		delete(item, "stringData")
		if f.resources["Secret:"+name] != nil {
			return nil, errors.New("already exists")
		}
		f.resources["Secret:"+name] = item
		return nil, nil
	case "scale":
		f.mutations = append(f.mutations, append([]string(nil), args...))
		name := strings.TrimPrefix(args[1], "deployment/")
		deployment := f.deployments[name]
		if deployment == nil {
			return nil, errors.New("deployment missing")
		}
		replicas, err := strconv.Atoi(strings.TrimPrefix(args[2], "--replicas="))
		if err != nil {
			return nil, err
		}
		deployment["spec"].(map[string]any)["replicas"] = replicas
		deployment["metadata"].(map[string]any)["generation"] = number(nested(deployment, "metadata", "generation")) + 1
		deployment["status"] = map[string]any{"replicas": replicas, "readyReplicas": 0, "observedGeneration": nested(deployment, "metadata", "generation")}
		if replicas == 0 {
			delete(f.resources, "ReplicaSet:"+name)
			delete(f.resources, "Pod:"+name)
		}
		return nil, nil
	case "delete":
		f.mutations = append(f.mutations, append([]string(nil), args...))
		for _, target := range args[1:] {
			if strings.HasPrefix(target, "--") {
				continue
			}
			kind, name, ok := strings.Cut(target, "/")
			if !ok {
				return nil, errors.New("unscoped delete")
			}
			if kind == "deployment" {
				delete(f.deployments, name)
				delete(f.resources, "ReplicaSet:"+name)
				delete(f.resources, "Pod:"+name)
			}
			for key, resource := range f.resources {
				if strings.EqualFold(stringValue(resource["kind"]), kind) && stringValue(nested(resource, "metadata", "name")) == name {
					delete(f.resources, key)
				}
			}
		}
		return nil, nil
	case "get":
		if kind, name, named := strings.Cut(args[1], "/"); named {
			var object map[string]any
			for _, resource := range f.resources {
				if strings.EqualFold(stringValue(resource["kind"]), kind) && stringValue(nested(resource, "metadata", "name")) == name {
					object = resource
				}
			}
			if object == nil {
				return nil, nil
			}
			return json.Marshal(object)
		}
		selector := map[string]string{}
		for i, arg := range args {
			if arg == "-l" && i+1 < len(args) {
				for _, pair := range strings.Split(args[i+1], ",") {
					key, value, ok := strings.Cut(pair, "=")
					if ok {
						selector[key] = value
					}
				}
			}
		}
		matches := func(item map[string]any) bool {
			kind := strings.ToLower(stringValue(item["kind"]))
			if kind == "persistentvolumeclaim" {
				kind = "pvc"
			}
			included := false
			for _, requested := range strings.Split(args[1], ",") {
				included = included || requested == kind
			}
			if !included {
				return false
			}
			for key, value := range selector {
				if stringValue(nested(item, "metadata", "labels", key)) != value {
					return false
				}
			}
			return true
		}
		items := make([]map[string]any, 0)
		for _, deployment := range f.deployments {
			if matches(deployment) {
				items = append(items, deployment)
			}
		}
		for _, resource := range f.resources {
			if matches(resource) {
				items = append(items, resource)
			}
		}
		return json.Marshal(map[string]any{"apiVersion": "v1", "kind": "List", "items": items})

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
		if number(nested(deployment, "spec", "replicas")) == 0 {
			continue
		}
		status, ok := deployment["status"].(map[string]any)
		if !ok {
			status = map[string]any{}
			deployment["status"] = status
		}
		status["readyReplicas"] = 1
		status["availableReplicas"] = 1
		status["updatedReplicas"] = 1
		status["observedGeneration"] = nested(deployment, "metadata", "generation")
		f.deployments[name] = deployment
		template := applicationObjectCopy(nested(deployment, "spec", "template"))
		labels := nested(deployment, "metadata", "labels")
		replicaSetUID := "ReplicaSet:" + name
		f.resources[replicaSetUID] = map[string]any{
			"kind": "ReplicaSet", "metadata": map[string]any{"name": name + "-rs", "uid": replicaSetUID, "labels": labels,
				"ownerReferences": []any{map[string]any{"kind": "Deployment", "uid": nested(deployment, "metadata", "uid"), "controller": true}}},
			"spec": map[string]any{"template": template},
		}
		f.resources["Pod:"+name] = map[string]any{
			"kind": "Pod", "metadata": map[string]any{"name": name + "-pod", "uid": "Pod:" + name, "labels": labels,
				"ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "uid": replicaSetUID, "controller": true}}},
			"spec": template["spec"], "status": map[string]any{"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}},
				"containerStatuses": []any{map[string]any{"name": "app", "ready": true, "imageID": "docker-pullable://" + workspaceApplicationDeploymentImage(deployment)}}},
		}
	}
}

func (f *fakeTencentKubectl) setEntryReady() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, resource := range f.resources {
		if resource["kind"] == "Ingress" {
			resource["status"] = map[string]any{"loadBalancer": map[string]any{"ingress": []any{map[string]any{"hostname": "lb.example"}}}}
		}
	}
}

func applicationObjectCopy(value any) map[string]any {
	var result map[string]any
	if err := json.Unmarshal(mustJSON(value), &result); err != nil {
		panic(err)
	}
	return result
}

// Validate the Kubernetes fields implicated in the application admission bugs.
// LabelSelectors use the actual Kubernetes type and parser; Service and Ingress
// checks exercise their port/path requirements without contacting a cluster.
func decodeTencentApplicationListItems(t *testing.T, input WorkspaceApplicationRuntimeInput) []map[string]any {
	t.Helper()
	var list struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(workspaceApplicationManifest(input, tencentApplicationCompute(), tencentApplicationVolume()), &list); err != nil {
		t.Fatalf("manifest decode: %v", err)
	}
	if err := validateApplicationManifestObjects(list.Items); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	return list.Items
}

func findTencentApplicationDeployment(items []map[string]any, componentName string) map[string]any {
	for _, item := range items {
		if item["kind"] != "Deployment" {
			continue
		}
		labels, _ := nested(item, "metadata", "labels").(map[string]any)
		if stringValue(labels["oplcloud.cn/component-name"]) == componentName {
			return item
		}
	}
	return nil
}

func validateApplicationManifestObjects(items []map[string]any) error {
	selector := func(value any) error {
		decoder := json.NewDecoder(bytes.NewReader(mustJSON(value)))
		decoder.DisallowUnknownFields()
		var parsed metav1.LabelSelector
		if err := decoder.Decode(&parsed); err != nil {
			return err
		}
		_, err := metav1.LabelSelectorAsSelector(&parsed)
		return err
	}
	for _, item := range items {
		spec, _ := item["spec"].(map[string]any)
		switch item["kind"] {
		case "Service":
			ports, _ := spec["ports"].([]any)
			if len(ports) == 0 && spec["clusterIP"] != "None" {
				return errors.New("ClusterIP Service requires ports")
			}
		case "Ingress":
			rules, _ := spec["rules"].([]any)
			for _, value := range rules {
				rule, _ := value.(map[string]any)
				paths, _ := nested(rule, "http", "paths").([]any)
				for _, value := range paths {
					path, _ := value.(map[string]any)
					if !strings.HasPrefix(stringValue(path["path"]), "/") && (path["pathType"] == "Prefix" || path["pathType"] == "Exact") {
						return errors.New("Ingress Prefix/Exact path must be absolute")
					}
				}
			}
		case "NetworkPolicy":
			if err := selector(spec["podSelector"]); err != nil {
				return err
			}
			for _, direction := range []struct{ rules, peer string }{{"ingress", "from"}, {"egress", "to"}} {
				rules, _ := spec[direction.rules].([]any)
				for _, value := range rules {
					rule, _ := value.(map[string]any)
					peers, _ := rule[direction.peer].([]any)
					for _, value := range peers {
						peer, _ := value.(map[string]any)
						for _, key := range []string{"podSelector", "namespaceSelector"} {
							if raw, exists := peer[key]; exists {
								if err := selector(raw); err != nil {
									return fmt.Errorf("%s: %w", key, err)
								}
							}
						}
					}
				}
			}
		}
	}
	return nil
}

func tencentApplicationRuntimeFixture(t *testing.T) (*TencentProvider, *fakeTencentKubectl, WorkspaceApplicationRuntimeInput) {
	t.Helper()
	setProtectedResourceEnv(t)
	t.Setenv("OPL_INGRESS_CLASS", "qcloud")
	provider := NewTencentProvider()
	fake := &fakeTencentKubectl{deployments: map[string]map[string]any{}, resources: map[string]map[string]any{}}
	provider.kubectl = fake.call
	revision := applicationRevisionForTest()
	input := applicationRuntimeInput("app-runtime-tencent", revision)
	input.WorkspaceID = "ws-alpha"
	input.Revision.EntryPort = "http"
	volume := tencentApplicationVolume()
	fake.resources["PersistentVolumeClaim:"+storagePVCName(volume)] = map[string]any{"kind": "PersistentVolumeClaim", "metadata": map[string]any{"name": storagePVCName(volume), "uid": "pvc-owned", "labels": map[string]any{"oplcloud.cn/storage-id": volume.ID}, "annotations": map[string]any{"opl_account_id": input.AccountID, "opl_workspace_id": input.WorkspaceID, "opl_resource_id": volume.ID}}}
	return provider, fake, input
}

func tencentApplicationCompute() ComputeAllocation {
	return ComputeAllocation{ID: "compute-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", Status: "running", NodeName: "10.66.1.7", NodePoolID: "np-basic", PackageID: "basic"}
}

func tencentApplicationVolume() StorageVolume {
	return StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", SizeGB: 10, Status: "ready", ProviderResourceID: "pvc/opl-data-storage-alpha"}
}

func TestTencentApplicationManifestDependencyOwnSpec(t *testing.T) {
	_, _, input := tencentApplicationRuntimeFixture(t)
	revision := input.Revision
	revision.Dependencies = []contracts.WorkspaceApplicationDependency{{
		Name:  "retrieval",
		Image: "repo.example/apps/retrieval@sha256:" + strings.Repeat("b", 64),
		Ports: []contracts.WorkspaceApplicationDependencyPort{{Name: "grpc", Port: 9200, Protocol: "TCP"}},
		Command: contracts.WorkspaceApplicationDependencyCommand{
			Entrypoint: []string{"/bin/serve"},
			Args:       []string{"--port=9200"},
			Env:        map[string]string{"RETRIEVAL_MODE": "local"},
		},
		HealthChecks:     []contracts.WorkspaceApplicationDependencyHealthCheck{{Type: "tcp", Port: 9200, InitialDelaySeconds: 15}},
		PersistentMounts: []contracts.WorkspaceApplicationDependencyMount{{Name: "index", MountPath: "/index"}},
	}}
	input.Revision = revision
	items := decodeTencentApplicationListItems(t, input)

	deployment := findTencentApplicationDeployment(items, "retrieval")
	if deployment == nil {
		t.Fatal("retrieval deployment missing")
	}
	container := deployment["spec"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	if fmt.Sprint(container["command"]) != "[/bin/serve]" || fmt.Sprint(container["args"]) != "[--port=9200]" {
		t.Fatalf("dependency command/args = %v / %v", container["command"], container["args"])
	}
	env := container["env"].([]any)[0].(map[string]any)
	if env["name"] != "RETRIEVAL_MODE" || env["value"] != "local" {
		t.Fatalf("dependency env = %v", env)
	}
	probe := container["readinessProbe"].(map[string]any)
	if probe["tcpSocket"] == nil || probe["httpGet"] != nil {
		t.Fatalf("tcp dependency probe = %v", probe)
	}
	ports := container["ports"].([]any)[0].(map[string]any)
	if ports["containerPort"] != float64(9200) {
		t.Fatalf("dependency port = %v", ports)
	}
	foundIndex := false
	for _, item := range container["volumeMounts"].([]any) {
		mount := item.(map[string]any)
		if mount["mountPath"] == "/index" && mount["name"] == "workspace-data" {
			foundIndex = true
		}
	}
	if !foundIndex {
		t.Fatalf("dependency persistent mount missing: %v", container["volumeMounts"])
	}
}

func TestTencentApplicationRuntimeEnsureAppliesAndReportsPending(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	observation, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume())
	if !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("first ensure err=%v, want pending", err)
	}
	if observation.Status != "pending" || len(observation.Components) != 2 || observation.Components[0].State != "absent" || observation.Components[1].State != "pending" {
		t.Fatalf("pending observation=%#v", observation)
	}
	if fake.applyCount() != 1 {
		t.Fatalf("apply calls=%d", fake.applyCount())
	}
	manifest := fake.applies[0]
	if len(manifest) != 5 {
		t.Fatalf("manifest items=%d, want only the admitted dependency deployment/service, policies and ingress", len(manifest))
	}
	kinds := map[string]int{}
	for _, item := range manifest {
		kinds[item["kind"].(string)]++
	}
	if kinds["Deployment"] != 1 || kinds["Service"] != 1 || kinds["NetworkPolicy"] != 2 || kinds["Ingress"] != 1 {
		t.Fatalf("manifest kinds=%v", kinds)
	}
}

func TestTencentApplicationRuntimeReadyAfterDeploymentsConverge(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("first ensure err=%v", err)
	}
	completeTencentApplicationStartup(t, provider, fake, input)
	fake.setEntryReady()
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
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatalf("first ensure err=%v", err)
	}
	completeTencentApplicationStartup(t, provider, fake, input)
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
	completeTencentApplicationStartup(t, provider, fake, input)
	encoded, err := json.Marshal(fake.applies[len(fake.applies)-1])
	if err != nil {
		t.Fatal(err)
	}
	manifest := string(encoded)
	if !strings.Contains(manifest, "persistentVolumeClaim") || !strings.Contains(manifest, `"subPath":"`+contracts.WorkspaceApplicationDataDirectory(input.DataBindingID)+`/data"`) {
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

func TestTencentApplicationRuntimeReadbackRequiresCurrentOwnedReadyImage(t *testing.T) {
	cases := []struct {
		name   string
		change func(*fakeTencentKubectl, string)
		status string
	}{
		{"unobserved generation", func(f *fakeTencentKubectl, name string) {
			f.deployments[name]["metadata"].(map[string]any)["generation"] = 2
		}, "pending"},
		{"no pod", func(f *fakeTencentKubectl, name string) { delete(f.resources, "Pod:"+name) }, "pending"},
		{"pod not ready", func(f *fakeTencentKubectl, name string) {
			f.resources["Pod:"+name]["status"].(map[string]any)["conditions"] = []any{map[string]any{"type": "Ready", "status": "False"}}
		}, "pending"},
		{"foreign pod owner", func(f *fakeTencentKubectl, name string) {
			f.resources["Pod:"+name]["metadata"].(map[string]any)["ownerReferences"] = []any{map[string]any{"kind": "ReplicaSet", "controller": true, "uid": "other"}}
		}, "pending"},
		{"previous replicaset template", func(f *fakeTencentKubectl, name string) {
			containers := nested(f.resources["ReplicaSet:"+name], "spec", "template", "spec", "containers").([]any)
			containers[0].(map[string]any)["image"] = "registry.example/previous@sha256:" + strings.Repeat("f", 64)
		}, "pending"},
		{"wrong running digest", func(f *fakeTencentKubectl, name string) {
			statuses := nested(f.resources["Pod:"+name], "status", "containerStatuses").([]any)
			statuses[0].(map[string]any)["imageID"] = "containerd://sha256:" + strings.Repeat("f", 64)
		}, "failed"},
		{"wrong desired image", func(f *fakeTencentKubectl, name string) {
			containers := nested(f.deployments[name], "spec", "template", "spec", "containers").([]any)
			containers[0].(map[string]any)["image"] = "registry.example/previous@sha256:" + strings.Repeat("f", 64)
		}, "failed"},
		{"missing declared readiness probe", func(f *fakeTencentKubectl, name string) {
			containers := nested(f.deployments[name], "spec", "template", "spec", "containers").([]any)
			delete(containers[0].(map[string]any), "readinessProbe")
		}, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider, fake, input := tencentApplicationRuntimeFixture(t)
			if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
				t.Fatal(err)
			}
			completeTencentApplicationStartup(t, provider, fake, input)
			fake.setEntryReady()
			if observed, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input); err != nil || observed.Status != "ready" {
				t.Fatalf("baseline=%#v err=%v", observed, err)
			}
			tc.change(fake, workspaceApplicationComponentResourceName(input, "main"))
			observed, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
			if err != nil || observed.Status != tc.status || observed.EntryURL != "" {
				t.Fatalf("readback=%#v err=%v", observed, err)
			}
		})
	}
}

func TestTencentApplicationRuntimeEntryRequiresLiveControllerRoute(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatal(err)
	}
	completeTencentApplicationStartup(t, provider, fake, input)
	read := func() contracts.WorkspaceApplicationRuntimeObservation {
		t.Helper()
		observation, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		return observation
	}
	if observation := read(); observation.Status != "pending" || observation.Components[0].State != "pending" || observation.EntryURL != "" {
		t.Fatalf("a required entry must keep the main component pending until its route is published: %#v", observation)
	}
	ingress := fake.resources["Ingress:"+workspaceApplicationComponentResourceName(input, "entry")]
	fake.setEntryReady()
	if observation := read(); observation.Status != "ready" || observation.Components[0].State != "ready" || observation.EntryURL != "http://"+workspaceApplicationIngressHost(input)+"/" {
		t.Fatalf("controller admitted HTTP route=%#v", observation)
	}
	rules := nested(ingress, "spec", "rules").([]any)
	rules[0].(map[string]any)["host"] = "other.example"
	if observation := read(); observation.Status != "pending" || observation.Components[0].State != "pending" || observation.EntryURL != "" {
		t.Fatalf("foreign route=%#v", observation)
	}
}

func TestTencentApplicationRuntimeEntryUsesAdmittedClusterDefaultClass(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	t.Setenv("OPL_INGRESS_CLASS", "")
	if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatal(err)
	}
	completeTencentApplicationStartup(t, provider, fake, input)
	ingress := fake.resources["Ingress:"+workspaceApplicationComponentResourceName(input, "entry")]
	spec := ingress["spec"].(map[string]any)
	if _, declared := spec["ingressClassName"]; declared {
		t.Fatal("the adapter must not invent an installation Ingress class")
	}
	// The Kubernetes admission/controller owns selection when no class is set.
	spec["ingressClassName"] = "cluster-default"
	fake.setEntryReady()
	observation, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || observation.Status != "ready" || observation.EntryURL == "" {
		t.Fatalf("default controller route=%#v err=%v", observation, err)
	}
	t.Setenv("OPL_INGRESS_CLASS", "explicit-class")
	observation, err = provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || observation.Status != "pending" || observation.EntryURL != "" {
		t.Fatalf("a different explicit class must not use the old controller: %#v err=%v", observation, err)
	}
}

func TestTencentApplicationRuntimeDoesNotRequireAnUndeclaredOrPrivateEntry(t *testing.T) {
	for _, scenario := range []string{"no entry", "cloud private"} {
		t.Run(scenario, func(t *testing.T) {
			provider, fake, input := tencentApplicationRuntimeFixture(t)
			if scenario == "no entry" {
				input.Revision.EntryPort = ""
			} else {
				input.Revision.ExposurePolicy = "cloud_private"
			}
			if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
				t.Fatal(err)
			}
			completeTencentApplicationStartup(t, provider, fake, input)
			observed, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
			if err != nil || observed.Status != "ready" || observed.Components[0].State != "ready" || observed.EntryURL != "" {
				t.Fatalf("entry not required: observation=%#v err=%v", observed, err)
			}
		})
	}
}

func TestTencentApplicationRuntimeManifestDeclaresOnlySelectedPublicEntry(t *testing.T) {
	_, _, input := tencentApplicationRuntimeFixture(t)
	input.Revision.Ports = append(input.Revision.Ports,
		contracts.WorkspaceApplicationPort{Name: "admin", Port: 9000, Protocol: "TCP"},
		contracts.WorkspaceApplicationPort{Name: "events", Port: 9001, Protocol: "UDP"})
	input.Revision.EntryPort = "admin"
	input.Revision.Entrypoint = []string{"/app/server", "--serve"}
	var manifest struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(workspaceApplicationManifest(input, tencentApplicationCompute(), tencentApplicationVolume()), &manifest); err != nil {
		t.Fatal(err)
	}
	if err := validateApplicationManifestObjects(manifest.Items); err != nil {
		t.Fatal(err)
	}
	for _, item := range manifest.Items {
		spec, _ := item["spec"].(map[string]any)
		name := stringValue(nested(item, "metadata", "name"))
		switch {
		case item["kind"] == "Service" && strings.HasSuffix(name, "retrieval"):
			if spec["clusterIP"] != "None" || spec["ports"] != nil {
				t.Fatalf("dependency DNS service=%#v", spec)
			}
		case item["kind"] == "NetworkPolicy" && strings.HasSuffix(name, "application-entry"):
			selector := nested(spec, "podSelector", "matchLabels").(map[string]any)
			if selector["app.kubernetes.io/instance"] != workspaceApplicationComponentResourceName(input, "main") {
				t.Fatalf("public scope=%#v", selector)
			}
			rule := spec["ingress"].([]any)[0].(map[string]any)
			ports := rule["ports"].([]any)
			if rule["from"] != nil || len(ports) != 1 || number(ports[0].(map[string]any)["port"]) != 9000 {
				t.Fatalf("public ingress=%#v", rule)
			}
		case item["kind"] == "Ingress":
			path := spec["rules"].([]any)[0].(map[string]any)["http"].(map[string]any)["paths"].([]any)[0].(map[string]any)
			if path["path"] != "/" || number(nested(path, "backend", "service", "port", "number")) != 9000 || spec["ingressClassName"] != "qcloud" {
				t.Fatalf("selected route=%#v", item)
			}
		case item["kind"] == "Deployment" && strings.HasSuffix(name, "-main"):
			command := firstContainerField(item, "command").([]any)
			if len(command) != 2 || command[0] != "/app/server" {
				t.Fatalf("declared command=%#v", command)
			}
		case item["kind"] == "Deployment" && strings.HasSuffix(name, "-retrieval"):
			for _, field := range []string{"volumeMounts", "command", "ports", "readinessProbe"} {
				if value := firstContainerField(item, field); value != nil {
					t.Fatalf("dependency must not inherit the main application's %s: %#v", field, value)
				}
			}
			if value := nested(item, "spec", "template", "spec", "volumes"); value != nil {
				t.Fatalf("dependency must not acquire main application volumes: %#v", value)
			}
		}
	}
	input.Revision.EntryPort = ""
	if workspaceApplicationIngress(input, tencentApplicationCompute(), nil) != nil || workspaceApplicationPublicEntryPolicy(input, nil) != nil {
		t.Fatal("service ports alone must not imply a public HTTP entry")
	}
	input.Revision.PersistentMounts, input.Revision.ScratchMounts = nil, nil
	main := contracts.WorkspaceApplicationRuntimeComponents(input.Revision)[0]
	deployment := workspaceApplicationComponentDeployment(input, tencentApplicationCompute(), tencentApplicationVolume(), main, nil, "unused-pvc")
	if firstContainerField(deployment, "volumeMounts") != nil || nested(deployment, "spec", "template", "spec", "volumes") != nil {
		t.Fatal("an application without declared mounts must not acquire a PVC or scratch volume")
	}
}

func TestTencentApplicationRuntimeRejectsUnsupportedHealthChecksBeforeApply(t *testing.T) {
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	input.Revision.HealthChecks = append(input.Revision.HealthChecks, contracts.WorkspaceApplicationHealthCheck{Port: 8080, Path: "/ready"})
	_, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume())
	if err == nil || err.Error() != "tencent_application_health_checks_unsupported" || fake.applyCount() != 0 {
		t.Fatalf("extra required health check must not be ignored: err=%v applies=%d", err, fake.applyCount())
	}
}

func TestTencentApplicationRuntimeReadbackRejectsAccountDrift(t *testing.T) {
	for _, kind := range []string{"Deployment", "ReplicaSet", "Pod", "Service", "Ingress"} {
		for _, account := range []string{"", "acct-foreign"} {
			t.Run(kind+"/account="+account, func(t *testing.T) {
				provider, fake, input := tencentApplicationRuntimeFixture(t)
				if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
					t.Fatal(err)
				}
				completeTencentApplicationStartup(t, provider, fake, input)
				fake.setEntryReady()
				if observed, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input); err != nil || observed.Status != "ready" {
					t.Fatalf("baseline=%#v err=%v", observed, err)
				}
				var object map[string]any
				if kind == "Deployment" {
					object = fake.deployments[workspaceApplicationComponentResourceName(input, "main")]
				} else {
					for _, resource := range fake.resources {
						if resource["kind"] == kind {
							object = resource
							break
						}
					}
				}
				if object == nil {
					t.Fatal("fixture resource missing")
				}
				nested(object, "metadata", "labels").(map[string]any)["oplcloud.cn/account-id"] = account
				mutations := fake.applyCount()
				observed, err := provider.ReadWorkspaceApplicationRuntime(context.Background(), input)
				if err == nil || err.Error() != "tencent_application_runtime_readback_invalid" || observed.Status == "ready" || fake.applyCount() != mutations {
					t.Fatalf("drift=%#v err=%v mutationCount=%d", observed, err, fake.applyCount())
				}
			})
		}
	}
}

func TestTencentApplicationLifecycleTargetsOneGenerationAndRetainsSuccessor(t *testing.T) {
	ctx := context.Background()
	provider, fake, old := tencentApplicationRuntimeFixture(t)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(ctx, old, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatal(err)
	}
	next := old
	next.RuntimeOperationID = "successor-runtime"
	next.IdempotencyKey = next.RuntimeOperationID
	next.Revision.ApplicationID = "successor-app"
	next.DataBindingID = "successor-data"
	next.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(next.Configuration, next.SecretBindings, next.DataBindingID)
	if _, err := provider.EnsureWorkspaceApplicationRuntime(ctx, next, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
		t.Fatal(err)
	}
	completeTencentApplicationStartup(t, provider, fake, old)
	completeTencentApplicationStartup(t, provider, fake, next)
	fake.setEntryReady()
	oldLive, err := provider.ReadWorkspaceApplicationRuntime(ctx, old)
	if err != nil || oldLive.Status != "ready" {
		t.Fatalf("old ready=%#v err=%v", oldLive, err)
	}
	nextLive, err := provider.ReadWorkspaceApplicationRuntime(ctx, next)
	if err != nil || nextLive.Status != "ready" || oldLive.EntryURL == nextLive.EntryURL {
		t.Fatalf("successor ready=%#v err=%v", nextLive, err)
	}
	suspended, err := provider.SetWorkspaceApplicationRuntimeLifecycle(ctx, old, "suspended")
	if err != nil || suspended.State != "suspended" {
		t.Fatalf("suspend=%#v err=%v", suspended, err)
	}
	if _, err := provider.SetWorkspaceApplicationRuntimeLifecycle(ctx, old, "running"); err != nil {
		t.Fatal(err)
	}
	completeTencentApplicationResume(t, provider, fake, old)
	resumed, err := provider.ReadWorkspaceApplicationRuntimeLifecycle(ctx, old)
	if err != nil || resumed.State != "running" {
		t.Fatalf("resume=%#v err=%v", resumed, err)
	}
	removed, err := provider.SetWorkspaceApplicationRuntimeLifecycle(ctx, old, "absent")
	if err != nil || removed.State != "absent" {
		t.Fatalf("remove=%#v err=%v", removed, err)
	}
	for _, image := range removed.ImageRetirement {
		if image.State != "instance_required" {
			t.Fatal("Tencent image retirement falsely claimed")
		}
	}
	nextLive, err = provider.ReadWorkspaceApplicationRuntime(ctx, next)
	if err != nil || nextLive.Status != "ready" {
		t.Fatalf("cleanup touched successor: %#v err=%v", nextLive, err)
	}
	for _, call := range fake.mutations {
		for _, arg := range call {
			if arg == "--all" || strings.Contains(arg, next.RuntimeOperationID) || strings.HasPrefix(arg, "pvc/") || strings.HasPrefix(arg, "pv/") {
				t.Fatalf("unbounded lifecycle mutation %v", call)
			}
		}
	}
}

func TestTencentApplicationReadbackRejectsActualMountDrift(t *testing.T) {
	for _, change := range []string{"data subpath", "data readonly", "data pvc", "storage owner", "scratch medium", "secret subpath", "secret readonly", "secret source", "extra mount"} {
		t.Run(change, func(t *testing.T) {
			ctx := context.Background()
			provider, fake, input := tencentApplicationRuntimeFixture(t)
			input.Revision.SecretInputs = []contracts.WorkspaceApplicationSecretInput{{Name: "service-token", Target: "/run/secrets/service-token"}}
			input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "service-token", SecretRef: "declared-fixture-secret", Version: "fixture-version", Key: "token"}}
			input.ConfigurationDigest, _ = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
			fake.resources["Secret:declared-fixture-secret"] = map[string]any{"kind": "Secret", "metadata": map[string]any{"name": "declared-fixture-secret", "annotations": map[string]any{"oplcloud.cn/account-id": input.AccountID, "oplcloud.cn/workspace-id": input.WorkspaceID, "oplcloud.cn/secret-version": "fixture-version"}}, "data": map[string]any{"token": base64.StdEncoding.EncodeToString([]byte("synthetic-token"))}}
			if _, err := provider.EnsureWorkspaceApplicationRuntime(ctx, input, tencentApplicationCompute(), tencentApplicationVolume()); !errors.Is(err, ErrWorkspaceLaunchPending) {
				t.Fatal(err)
			}
			completeTencentApplicationStartup(t, provider, fake, input)
			fake.setEntryReady()
			if baseline, err := provider.ReadWorkspaceApplicationRuntime(ctx, input); err != nil || baseline.Status != "ready" {
				t.Fatalf("baseline state=%s err=%v", baseline.Status, err)
			}
			deployment := fake.deployments[workspaceApplicationComponentResourceName(input, "main")]
			mounts := firstContainerField(deployment, "volumeMounts").([]any)
			volumes := nested(deployment, "spec", "template", "spec", "volumes").([]any)
			for _, value := range mounts {
				mount := value.(map[string]any)
				switch {
				case mount["name"] == "workspace-data" && change == "data subpath":
					mount["subPath"] = "another-application/data"
				case mount["name"] == "workspace-data" && change == "data readonly":
					mount["readOnly"] = true
				case mount["name"] == "application-secrets" && change == "secret subpath":
					mount["subPath"] = "other-key"
				case mount["name"] == "application-secrets" && change == "secret readonly":
					mount["readOnly"] = false
				}
			}
			for _, value := range volumes {
				volume := value.(map[string]any)
				switch {
				case volume["name"] == "workspace-data" && change == "data pvc":
					volume["persistentVolumeClaim"].(map[string]any)["claimName"] = "foreign-storage"
				case volume["name"] == "scratch-0" && change == "scratch medium":
					volume["emptyDir"].(map[string]any)["medium"] = ""
				case volume["name"] == "application-secrets" && change == "secret source":
					volume["secret"].(map[string]any)["secretName"] = "another-generation"
				}
			}
			if change == "storage owner" {
				pvc := fake.resources["PersistentVolumeClaim:"+storagePVCName(tencentApplicationVolume())]
				nested(pvc, "metadata", "annotations").(map[string]any)["opl_account_id"] = "foreign-account"
			}
			if change == "extra mount" {
				container := nested(deployment, "spec", "template", "spec", "containers").([]any)[0].(map[string]any)
				container["volumeMounts"] = append(mounts, map[string]any{"name": "workspace-data", "mountPath": "/leaked-data"})
			}
			// Even a rolled-out, healthy pod must not turn drifted mounts into ready.
			fake.setAllReady()
			observed, err := provider.ReadWorkspaceApplicationRuntime(ctx, input)
			if err == nil && observed.Status != "failed" {
				t.Fatalf("drift escaped readback: state=%s err=%v", observed.Status, err)
			}
			before := len(fake.mutations)
			if _, err := provider.SetWorkspaceApplicationRuntimeLifecycle(ctx, input, "suspended"); err == nil || len(fake.mutations) != before {
				t.Fatal("lifecycle mutated a conflicting mount binding")
			}
		})
	}
}

func TestTencentApplicationHistoricalPolicyRequiresExactOwnerAndAbsence(t *testing.T) {
	ctx := context.Background()
	provider, fake, input := tencentApplicationRuntimeFixture(t)
	input.SchemaVersion = 0
	input.DataBindingID = ""
	input.ConfigurationDigest = strings.Repeat("c", 64)
	tags := oplCostTags(input.AccountID, input.WorkspaceID, applicationRuntimeID(input), input.RuntimeOperationID)
	for _, policy := range []map[string]any{workspaceApplicationNetworkPolicy(input, tags), workspaceApplicationPublicEntryPolicy(input, tags)} {
		metadata := policy["metadata"].(map[string]any)
		delete(metadata, "labels") // Exact original policy representation.
		fake.resources["NetworkPolicy:"+stringValue(metadata["name"])] = applicationObjectCopy(policy)
	}
	live, err := provider.ReadWorkspaceApplicationRuntimeLifecycle(ctx, input)
	if err != nil || live.State != "pending" {
		t.Fatalf("unlabelled policies were omitted: state=%s err=%v", live.State, err)
	}
	name := workspaceApplicationComponentResourceName(input, "network")
	annotations := nested(fake.resources["NetworkPolicy:"+name], "metadata", "annotations").(map[string]any)
	annotations["opl_account_id"] = "foreign-account"
	if _, err := provider.SetWorkspaceApplicationRuntimeLifecycle(ctx, input, "absent"); err == nil || len(fake.mutations) != 0 {
		t.Fatal("foreign policy was deleted")
	}
	annotations["opl_account_id"] = input.AccountID
	removed, err := provider.SetWorkspaceApplicationRuntimeLifecycle(ctx, input, "absent")
	if err != nil || removed.State != "absent" {
		t.Fatalf("owned historical policy cleanup: state=%s err=%v", removed.State, err)
	}
	for key := range fake.resources {
		if strings.HasPrefix(key, "NetworkPolicy:") {
			t.Fatal("historical policy remained after absent readback")
		}
	}
}

// Fixtures advance the real Ensure/Set paths between provider-ready readbacks;
// no test manufactures a component that the admission path has not created.
func completeTencentApplicationStartup(t *testing.T, provider *TencentProvider, fake *fakeTencentKubectl, input WorkspaceApplicationRuntimeInput) {
	t.Helper()
	for round := 0; round < len(input.Revision.Dependencies)+1; round++ {
		fake.setAllReady()
		if _, err := provider.EnsureWorkspaceApplicationRuntime(context.Background(), input, tencentApplicationCompute(), tencentApplicationVolume()); err != nil && !errors.Is(err, ErrWorkspaceLaunchPending) {
			t.Fatal(err)
		}
	}
	fake.setAllReady()
}

func completeTencentApplicationResume(t *testing.T, provider *TencentProvider, fake *fakeTencentKubectl, input WorkspaceApplicationRuntimeInput) {
	t.Helper()
	for round := 0; round < len(input.Revision.Dependencies)+1; round++ {
		fake.setAllReady()
		if _, err := provider.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), input, "running"); err != nil {
			t.Fatal(err)
		}
	}
	fake.setAllReady()
}
