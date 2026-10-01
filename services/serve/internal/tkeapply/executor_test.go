package tkeapply

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/protectedresource"
)

// The fake cluster is deliberately small but real in the one way that matters: it
// stores exactly the objects the executor applied, and it answers the reads the
// executor issues out of that store. A test therefore proves what the executor put
// into the cluster, not what a fixture asserted about it.
type fakeCluster struct {
	objects map[string]map[string]any // "Kind/name" -> object
	pvcName string
}

func newFakeCluster() *fakeCluster {
	return &fakeCluster{objects: map[string]map[string]any{}}
}

func (c *fakeCluster) kubectl(_ context.Context, args []string, stdin []byte) ([]byte, error) {
	switch {
	case len(args) > 0 && args[0] == "apply":
		return c.apply(stdin), nil
	case len(args) > 0 && args[0] == "create":
		return c.create(stdin)
	case len(args) > 0 && args[0] == "scale":
		return c.scale(args), nil
	case len(args) > 0 && args[0] == "delete":
		return c.delete(args), nil
	case len(args) > 1 && args[0] == "get" && strings.HasPrefix(args[1], "pvc"):
		return c.listPVC(), nil
	case len(args) > 1 && args[0] == "get" && strings.HasPrefix(args[1], "secret/"):
		return c.readSecret(strings.TrimPrefix(args[1], "secret/")), nil
	case len(args) > 1 && args[0] == "get" && strings.HasPrefix(args[1], "networkpolicy/"):
		name := strings.TrimPrefix(args[1], "networkpolicy/")
		object, found := c.objects["NetworkPolicy/"+name]
		if !found {
			return nil, nil
		}
		return mustJSON(object), nil
	case len(args) > 3 && args[0] == "get" && args[2] == "-l":
		return c.list(args[3]), nil
	}
	return nil, fmt.Errorf("unsupported kubectl action %v", args)
}

func (c *fakeCluster) apply(stdin []byte) []byte {
	var document map[string]any
	if json.Unmarshal(stdin, &document) != nil {
		return nil
	}
	items, _ := document["items"].([]any)
	for _, item := range items {
		object, _ := item.(map[string]any)
		c.store(object)
	}
	return mustJSON(document)
}

func (c *fakeCluster) create(stdin []byte) ([]byte, error) {
	var stored map[string]any
	if json.Unmarshal(stdin, &stored) != nil || stringValue(stored["kind"]) == "" {
		return nil, fmt.Errorf("kubernetes_response_invalid")
	}
	name := stringValue(nested(stored, "metadata", "name"))
	if c.objects[stringValue(stored["kind"])+"/"+name] != nil {
		return nil, fmt.Errorf("a resource with that name already exists")
	}
	c.store(stored)
	return mustJSON(stored), nil
}

func (c *fakeCluster) scale(args []string) []byte {
	name := strings.TrimPrefix(args[1], "deployment/")
	replicas := number(strings.TrimPrefix(args[2], "--replicas="))
	if object := c.objects["Deployment/"+name]; object != nil {
		object["spec"].(map[string]any)["replicas"] = replicas
		object["status"] = map[string]any{"observedGeneration": float64(1), "replicas": replicas, "updatedReplicas": replicas, "readyReplicas": replicas, "availableReplicas": replicas}
		if replicas == 0 {
			delete(c.objects, "Pod/"+name)
		}
	}
	return nil
}

func (c *fakeCluster) list(selector string) []byte {
	keys := make([]string, 0, len(c.objects))
	for key := range c.objects {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	matched, included := []any{}, map[string]bool{}
	for _, key := range keys {
		object := c.objects[key]
		if included[key] || !selectorMatches(selector, stringMap(nested(object, "metadata", "labels"))) {
			continue
		}
		matched, included[key] = append(matched, c.materialize(object)), true
		// A real cluster reports the objects a Deployment owns in the same read.
		if name := stringValue(nested(object, "metadata", "name")); strings.HasPrefix(key, "Deployment/") {
			for _, derived := range []string{"ReplicaSet/" + name, "Pod/" + name} {
				if owned := c.objects[derived]; owned != nil && !included[derived] {
					matched, included[derived] = append(matched, owned), true
				}
			}
		}
	}
	return mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": matched})
}

// delete removes the named objects, which is how kubectl reports a retirement: the
// objects either exist and are deleted, or the call is refused.
func (c *fakeCluster) delete(args []string) []byte {
	for _, target := range args[1:] {
		if strings.HasPrefix(target, "--") {
			continue
		}
		parts := strings.SplitN(target, "/", 2)
		if len(parts) != 2 {
			continue
		}
		kind := map[string]string{"deployment": "Deployment", "service": "Service", "networkpolicy": "NetworkPolicy", "secret": "Secret", "configmap": "ConfigMap", "pvc": "PersistentVolumeClaim"}[parts[0]]
		if kind == "" {
			continue
		}
		if _, found := c.objects[kind+"/"+parts[1]]; found {
			delete(c.objects, kind+"/"+parts[1])
			// A real cluster garbage-collects the objects a deleted Deployment
			// owns through ownerReferences.
			if kind == "Deployment" {
				delete(c.objects, "ReplicaSet/"+parts[1])
				delete(c.objects, "Pod/"+parts[1])
			}
			continue
		}
		return nil
	}
	return nil
}

func (c *fakeCluster) listPVC() []byte {
	return mustJSON(map[string]any{"apiVersion": "v1", "kind": "List", "items": []any{c.objects["PersistentVolumeClaim/"+c.pvcName]}})
}

func (c *fakeCluster) readSecret(name string) []byte {
	object := c.objects["Secret/"+name]
	if object == nil {
		return nil
	}
	return mustJSON(object)
}

// store keeps the object and fills in the readback fields a real cluster would
// report: uids, generation and the pods a running Deployment owns.
func (c *fakeCluster) store(object map[string]any) {
	kind, name := stringValue(object["kind"]), stringValue(nested(object, "metadata", "name"))
	if kind == "PersistentVolumeClaim" {
		c.pvcName = name
	}
	metadata := object["metadata"].(map[string]any)
	metadata["uid"] = "uid-" + kind + "-" + name
	if kind == "Deployment" {
		metadata["generation"] = float64(1)
		object["status"] = map[string]any{"observedGeneration": float64(1), "replicas": number(nested(object, "spec", "replicas")), "updatedReplicas": float64(1), "readyReplicas": float64(1), "availableReplicas": float64(1)}
	}
	// A Secret is declared as stringData; a real cluster returns data. The
	// readback the executor verifies is the decoded value, so the fixture folds it
	// the same way.
	if strings, ok := object["stringData"].(map[string]any); ok {
		data := map[string]any{}
		for key, value := range strings {
			data[key] = base64String(stringValue(value))
		}
		object["data"] = data
		delete(object, "stringData")
	}
	c.objects[kind+"/"+name] = object
}

// materialize adds the objects a real cluster derives from a Deployment: its
// ReplicaSet and the Pod that ReplicaSet owns.
func (c *fakeCluster) materialize(object map[string]any) map[string]any {
	if stringValue(object["kind"]) != "Deployment" {
		return object
	}
	owner := stringValue(nested(object, "metadata", "uid"))
	name := stringValue(nested(object, "metadata", "name"))
	if c.objects["ReplicaSet/"+name] == nil {
		template, _ := nested(object, "spec", "template").(map[string]any)
		replicaTemplate := map[string]any{}
		for key, value := range template {
			replicaTemplate[key] = value
		}
		metadata := map[string]any{}
		for key, value := range replicaTemplate["metadata"].(map[string]any) {
			metadata[key] = value
		}
		labels := map[string]any{"pod-template-hash": "hash"}
		for key, value := range metadata["labels"].(map[string]any) {
			labels[key] = value
		}
		metadata["labels"] = labels
		replicaTemplate["metadata"] = metadata
		c.objects["ReplicaSet/"+name] = map[string]any{"apiVersion": "apps/v1", "kind": "ReplicaSet", "metadata": map[string]any{
			"name": name, "uid": "uid-ReplicaSet-" + name, "labels": nested(object, "metadata", "labels"), "ownerReferences": []any{map[string]any{"kind": "Deployment", "uid": owner, "controller": true}},
		}, "spec": map[string]any{"template": replicaTemplate}}
		replicas := number(nested(object, "spec", "replicas"))
		containers, _ := nested(template, "spec", "containers").([]any)
		image := stringValue(nested(containers[0].(map[string]any), "image"))
		if replicas > 0 {
			c.objects["Pod/"+name] = map[string]any{"apiVersion": "v1", "kind": "Pod", "metadata": map[string]any{
				"name": name + "-7d9", "uid": "uid-Pod-" + name, "labels": nested(replicaTemplate, "metadata", "labels"), "ownerReferences": []any{map[string]any{"kind": "ReplicaSet", "uid": "uid-ReplicaSet-" + name, "controller": true}},
			}, "spec": map[string]any{"containers": containers, "securityContext": nested(template, "spec", "securityContext")}, "status": map[string]any{
				"phase": "Running", "conditions": []any{map[string]any{"type": "Ready", "status": "True"}}, "containerStatuses": []any{map[string]any{"name": "app", "ready": true, "imageID": strings.Split(image, "@")[0] + "@" + strings.SplitN(image, "@", 2)[1]}},
			}}
		} else {
			delete(c.objects, "Pod/"+name)
		}
	}
	return object
}

func selectorMatches(selector string, labels map[string]string) bool {
	for _, pair := range strings.Split(selector, ",") {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 || labels[parts[0]] != parts[1] {
			return false
		}
	}
	return true
}

func stringMap(value any) map[string]string {
	labels, _ := value.(map[string]any)
	result := map[string]string{}
	for key, item := range labels {
		result[key] = stringValue(item)
	}
	return result
}

func base64String(value string) string {
	return base64.StdEncoding.EncodeToString([]byte(value))
}

// testInstallation is one installation's declared execution facts.
func testInstallation(peers ...map[string]string) Installation {
	return Installation{Namespace: "opl-cloud", ImagePullSecretName: "tcr-pull-secret", AdminPasswordSeed: "test-admin-password-seed-0001", ApplicationIngressPeers: peers}
}

func testPlacement() Placement {
	return Placement{
		AccountID: "account-1", WorkspaceID: "workspace-1", ComputeID: "compute-1", StorageVolumeID: "volume-1",
		ComputeNodeName: "node-1", ComputePackageID: "basic", ComputeNodePoolID: "np-basic",
		ComputeMachineName: "machine-1", ComputeInstanceID: "ins-1", StoragePVCName: "pvc-1",
	}
}

func testInput(t *testing.T) WorkspaceApplicationRuntimeInput {
	t.Helper()
	revision := contracts.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationID: "app", Version: "1", Platform: "linux/amd64",
		Image:          "registry.test/app@sha256:1111111111111111111111111111111111111111111111111111111111111111",
		ExposurePolicy: "application", EntryPort: "http",
		Ports:            []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}},
		HealthChecks:     []contracts.WorkspaceApplicationHealthCheck{{Path: "/healthz", Port: 8080}},
		PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}},
	}
	input := WorkspaceApplicationRuntimeInput{
		SchemaVersion: 2, AccountID: "account-1", WorkspaceID: "workspace-1",
		ComputeID: "compute-1", VolumeID: "volume-1", AttachmentID: "attachment-1",
		AttachmentOperationID: "attachment-operation-1", RuntimeOperationID: "runtime-operation-1",
		Revision: revision, DataBindingID: "attachment-1",
	}
	digest, err := contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, nil, input.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	input.ConfigurationDigest = digest
	if err := contracts.ValidateWorkspaceApplicationRuntimeConfiguration(input); err != nil {
		t.Fatalf("fixture input is not a valid runtime configuration: %v", err)
	}
	return input
}

// testGuard is the installation's own protected-compute declaration. An
// installation that cannot describe its system compute cannot mutate anything, so
// every positive fixture carries a complete one.
func testGuard() protectedresource.Config {
	return protectedresource.Config{
		SystemNodePoolID: "np-system", SystemMachineID: "machine-system", SystemNodeName: "node-system",
		SystemMachineType: "Native", PackageNodePools: map[string]string{"basic": "np-basic"},
	}
}

func testExecutor(cluster *fakeCluster, installation Installation) *Executor {
	return &Executor{Kubectl: cluster.kubectl, Guard: testGuard(), Installation: installation}
}

func pvcObject() map[string]any {
	return map[string]any{"apiVersion": "v1", "kind": "PersistentVolumeClaim", "metadata": map[string]any{
		"name": "pvc-1",
		"labels": map[string]any{
			"oplcloud.cn/storage-id": k8sCostLabelValue("volume-1"),
		},
		"annotations": map[string]any{"opl_account_id": "account-1", "opl_workspace_id": "workspace-1", "opl_resource_id": "volume-1"},
	}}
}

// TestExecutorAppliesTheWorkloadOntoTheConfirmedPlacement is the decisive test of
// this ownership move: the workload's node, prepaid package, storage claim and
// image-pull Secret all come from the confirmed placement and the installation's own
// facts, and the application's ingress admits exactly the declared peer.
func TestExecutorAppliesTheWorkloadOntoTheConfirmedPlacement(t *testing.T) {
	cluster := newFakeCluster()
	cluster.store(pvcObject())
	executor := testExecutor(cluster, testInstallation(map[string]string{"app.kubernetes.io/name": "opl-cloud", "app.kubernetes.io/component": "serve"}))

	observation, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), testInput(t), testPlacement())
	if err != nil {
		t.Logf("observation=%+v", observation)
		t.Fatal(err)
	}
	if observation.Status != "ready" {
		t.Fatalf("observation=%+v want ready", observation)
	}
	deployment := cluster.objects["Deployment/"+observation.Components[0].Name]
	_ = deployment
	var applied map[string]any
	for key, object := range cluster.objects {
		if strings.HasPrefix(key, "Deployment/") {
			applied = object
		}
	}
	if applied == nil {
		t.Fatal("no Deployment applied")
	}
	if stringValue(nested(applied, "spec", "template", "spec", "nodeSelector", "kubernetes.io/hostname")) != "node-1" {
		t.Fatalf("nodeSelector does not name the confirmed node: %v", nested(applied, "spec", "template", "spec", "nodeSelector"))
	}
	tolerations, _ := nested(applied, "spec", "template", "spec", "tolerations").([]any)
	if len(tolerations) != 1 || stringValue(nested(tolerations[0].(map[string]any), "value")) != "basic" {
		t.Fatalf("tolerations do not name the confirmed prepaid package: %v", tolerations)
	}
	volumes, _ := nested(applied, "spec", "template", "spec", "volumes").([]any)
	claim := ""
	for _, item := range volumes {
		volume, _ := item.(map[string]any)
		if name := stringValue(nested(volume, "persistentVolumeClaim", "claimName")); name != "" {
			claim = name
		}
	}
	if claim != "pvc-1" {
		t.Fatalf("persistent mount does not use the confirmed storage claim: %v", volumes)
	}
	pullSecrets, _ := nested(applied, "spec", "template", "spec", "imagePullSecrets").([]any)
	if len(pullSecrets) != 1 || stringValue(nested(pullSecrets[0].(map[string]any), "name")) != "tcr-pull-secret" {
		t.Fatalf("imagePullSecrets do not name the installation Secret: %v", pullSecrets)
	}
	var policy map[string]any
	for key, object := range cluster.objects {
		if strings.HasPrefix(key, "NetworkPolicy/") {
			policy = object
		}
	}
	ingress, _ := nested(policy, "spec", "ingress").([]any)
	if len(ingress) != 2 {
		t.Fatalf("application ingress must admit the declared peer and its siblings: %v", ingress)
	}
	peer, _ := nested(ingress[0].(map[string]any), "from").([]any)
	labels, _ := nested(peer[0].(map[string]any), "podSelector", "matchLabels").(map[string]any)
	if stringValue(labels["app.kubernetes.io/component"]) != "serve" || stringValue(labels["app.kubernetes.io/name"]) != "opl-cloud" {
		t.Fatalf("application ingress does not admit the installation's Serve access data plane: %v", labels)
	}
}

// TestExecutorRefusesAnInstallationFactItNeeds proves the executor never applies a
// workload onto an unconfirmed placement or with a missing installation fact.
func TestExecutorRefusesAnInstallationFactItNeeds(t *testing.T) {
	input := testInput(t)
	cluster := newFakeCluster()
	cluster.store(pvcObject())

	incomplete := testInstallation(map[string]string{"app.kubernetes.io/component": "serve"})
	incomplete.ImagePullSecretName = ""
	if _, err := testExecutor(cluster, incomplete).EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); err == nil {
		t.Fatal("an installation with no image-pull Secret applied a workload")
	}
	if _, err := testExecutor(cluster, testInstallation()).EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); err == nil {
		t.Fatal("an installation with no declared ingress peer applied a workload")
	}
	executor := testExecutor(cluster, testInstallation(map[string]string{"app": "opl"}))
	installation := testInstallation(map[string]string{"app": "opl"})
	placement := testPlacement()
	placement.StoragePVCName = ""
	if _, err := testExecutor(cluster, installation).EnsureWorkspaceApplicationRuntime(context.Background(), input, placement); !errors.Is(err, ErrPlacementUnconfirmed) {
		t.Fatalf("a placement with no storage claim err=%v want the unconfirmed-placement refusal", err)
	}
	// A resources owner that publishes no placement reaches this executor as an
	// absent one, and the executor that schedules the workload is what refuses it.
	if _, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, Placement{}); !errors.Is(err, ErrPlacementUnconfirmed) {
		t.Fatalf("an absent placement err=%v want the unconfirmed-placement refusal", err)
	}
	if len(cluster.objects) == 0 {
		t.Fatal("fixture did not run")
	}
	for key := range cluster.objects {
		if strings.HasPrefix(key, "Deployment/") {
			t.Fatalf("a refused apply still created %s", key)
		}
	}
}

// TestExecutorGuardRefusesTheInstallationsProtectedCompute proves the workload can
// never be scheduled onto the installation's own system compute.
func TestExecutorGuardRefusesTheInstallationsProtectedCompute(t *testing.T) {
	cluster := newFakeCluster()
	executor := testExecutor(cluster, testInstallation(map[string]string{"app": "opl"}))
	placement := testPlacement()
	placement.ComputeNodeName = "node-system"
	_, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), testInput(t), placement)
	if !errors.Is(err, protectedresource.ErrProtectedResource) {
		t.Fatalf("err=%v want the protected-resource refusal", err)
	}
}

// TestExecutorReportsPendingUntilEveryComponentServes proves a created object is
// not an available application: the workload stays pending until its pod is ready.
func TestExecutorReportsPendingUntilEveryComponentServes(t *testing.T) {
	cluster := newFakeCluster()
	cluster.store(pvcObject())
	input := testInput(t)
	executor := testExecutor(cluster, testInstallation(map[string]string{"app": "opl"}))
	if _, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); err != nil {
		t.Fatal(err)
	}
	// A scaled-to-zero component is not an available application.
	name := workspaceApplicationComponentResourceName(input, contracts.WorkspaceApplicationComponentMain)
	cluster.scale([]string{"scale", "deployment/" + name, "--replicas=0"})
	observation, err := executor.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Status != "pending" {
		t.Fatalf("observation=%+v want pending while the component serves nothing", observation)
	}
}

// TestExecutorLifecycleConfirmsTheAppliedState proves the lifecycle readback is the
// fact Serve records, including a runtime that is entirely absent after retirement.
func TestExecutorLifecycleConfirmsTheAppliedState(t *testing.T) {
	cluster := newFakeCluster()
	cluster.store(pvcObject())
	input := testInput(t)
	executor := testExecutor(cluster, testInstallation(map[string]string{"app": "opl"}))
	if _, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); err != nil {
		t.Fatal(err)
	}
	suspended, err := executor.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), input, "suspended")
	if err != nil {
		t.Fatal(err)
	}
	if suspended.State != "suspended" {
		t.Fatalf("suspended=%+v", suspended)
	}
	absent, err := executor.SetWorkspaceApplicationRuntimeLifecycle(context.Background(), input, "absent")
	if err != nil {
		t.Fatal(err)
	}
	if absent.State != "absent" {
		t.Fatalf("absent=%+v", absent)
	}
	if len(absent.ImageRetirement) == 0 {
		t.Fatal("a retired application must report the image retire obligation")
	}
}

// TestExecutorReadsTheApplicationsOwnCredential proves the platform-derived
// credential comes from the applied Secret and never from the request.
func TestExecutorReadsTheApplicationsOwnCredential(t *testing.T) {
	cluster := newFakeCluster()
	cluster.store(pvcObject())
	input := testInput(t)
	input.Revision.Credentials = []contracts.WorkspaceApplicationCredential{
		{Name: "admin", Kind: contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword, Target: "/run/secrets/admin", Username: "opl"},
		{Name: "gateway", Kind: contracts.WorkspaceApplicationCredentialGatewayKey, Target: "/run/secrets/gateway"},
	}
	input.Configuration.CredentialVersion = "0123456789abcdef"
	input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", SecretRef: contracts.WorkspaceGatewaySecretRef(input.WorkspaceID), Version: "0123456789abcdef", Key: contracts.WorkspaceApplicationGatewayKeyField}}
	digest, err := contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err != nil {
		t.Fatal(err)
	}
	input.ConfigurationDigest = digest
	// The installation's own Workspace Gateway Secret is what the declared platform
	// credential is bound against; the workload is injected from it, never from a
	// value in the request.
	cluster.store(map[string]any{"apiVersion": "v1", "kind": "Secret", "metadata": map[string]any{
		"name":        contracts.WorkspaceGatewaySecretRef(input.WorkspaceID),
		"annotations": map[string]any{"oplcloud.cn/account-id": input.AccountID, "oplcloud.cn/workspace-id": input.WorkspaceID, "oplcloud.cn/secret-version": input.SecretBindings[0].Version},
	}, "data": map[string]any{contracts.WorkspaceApplicationGatewayKeyField: base64.StdEncoding.EncodeToString([]byte("gateway-key-value"))}})
	executor := testExecutor(cluster, testInstallation(map[string]string{"app": "opl"}))
	if _, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); err != nil {
		t.Fatal(err)
	}
	credentials, err := executor.ReadWorkspaceApplicationRuntimeCredentials(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if credentials.WorkspaceID != input.WorkspaceID || credentials.RuntimeID != workspaceApplicationRuntimeID(input.RuntimeOperationID) || credentials.WebUIUsername != "opl" || credentials.WebUIPassword == "" {
		t.Fatalf("credentials=%+v", credentials)
	}
}
