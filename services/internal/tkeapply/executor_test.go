package tkeapply

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/protectedresource"
)

const testNamespace = "opl-cloud"

// fakeCluster wraps a real k8s.io/client-go fake.Clientset (which natively
// executes Server-Side Apply field tracking, label-selector List, Create, Get,
// UpdateScale, and Delete) and simulates kube-apiserver / controller-manager
// side effects: UID/Generation assignment, StringData->Data folding on Secrets,
// ReplicaSet/Pod materialization, scale subresource reconciliation, and owner
// garbage collection on Deployment deletion.
type fakeCluster struct {
	clientset *fake.Clientset
	base      ClusterClient
}

func newFakeCluster() *fakeCluster {
	cs := fake.NewClientset()
	c := &fakeCluster{
		clientset: cs,
		base:      NewClientsetCluster(cs, testNamespace),
	}
	cs.PrependReactor("update", "deployments", func(action k8stesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() != "scale" {
			return false, nil, nil
		}
		updateAction, ok := action.(k8stesting.UpdateAction)
		if !ok {
			return false, nil, nil
		}
		scale, ok := updateAction.GetObject().(*autoscalingv1.Scale)
		if !ok {
			return false, nil, nil
		}
		return true, scale, nil
	})
	return c
}

func (c *fakeCluster) scaleDeploymentInStore(name string, replicas int32) error {
	ctx := context.Background()
	dep, err := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	dep.Spec.Replicas = &replicas
	dep.Status = appsv1.DeploymentStatus{
		ObservedGeneration: dep.Generation,
		Replicas:           replicas,
		UpdatedReplicas:    replicas,
		ReadyReplicas:      replicas,
		AvailableReplicas:  replicas,
	}
	if _, err := c.clientset.AppsV1().Deployments(testNamespace).Update(ctx, dep, metav1.UpdateOptions{}); err != nil {
		return err
	}
	if replicas == 0 {
		_ = c.clientset.CoreV1().Pods(testNamespace).Delete(ctx, name+"-7d9", metav1.DeleteOptions{})
	} else {
		c.materializeDeployment(ctx, dep)
	}
	return nil
}

func (c *fakeCluster) Apply(ctx context.Context, bundle *ApplyBundle, target protectedresource.Target) error {
	if err := c.base.Apply(ctx, bundle, target); err != nil {
		return err
	}
	if bundle == nil {
		return nil
	}
	for _, item := range bundle.Components {
		if item.Service != nil && item.Service.Name != nil {
			if svc, err := c.clientset.CoreV1().Services(testNamespace).Get(ctx, *item.Service.Name, metav1.GetOptions{}); err == nil {
				svc.UID = types.UID("uid-Service-" + svc.Name)
				_, _ = c.clientset.CoreV1().Services(testNamespace).Update(ctx, svc, metav1.UpdateOptions{})
			}
		}
		if item.Deployment != nil && item.Deployment.Name != nil {
			if dep, err := c.clientset.AppsV1().Deployments(testNamespace).Get(ctx, *item.Deployment.Name, metav1.GetOptions{}); err == nil {
				dep.UID = types.UID("uid-Deployment-" + dep.Name)
				dep.Generation = 1
				replicas := int32(1)
				if dep.Spec.Replicas != nil {
					replicas = *dep.Spec.Replicas
				}
				dep.Status = appsv1.DeploymentStatus{
					ObservedGeneration: 1,
					Replicas:           replicas,
					UpdatedReplicas:    1,
					ReadyReplicas:      1,
					AvailableReplicas:  1,
				}
				if updated, err := c.clientset.AppsV1().Deployments(testNamespace).Update(ctx, dep, metav1.UpdateOptions{}); err == nil {
					c.materializeDeployment(ctx, updated)
				}
			}
		}
	}
	for _, policy := range bundle.NetworkPolicies {
		if policy != nil && policy.Name != nil {
			if np, err := c.clientset.NetworkingV1().NetworkPolicies(testNamespace).Get(ctx, *policy.Name, metav1.GetOptions{}); err == nil {
				np.UID = types.UID("uid-NetworkPolicy-" + np.Name)
				_, _ = c.clientset.NetworkingV1().NetworkPolicies(testNamespace).Update(ctx, np, metav1.UpdateOptions{})
			}
		}
	}
	return nil
}

func (c *fakeCluster) CreateSecret(ctx context.Context, secret *corev1.Secret, target protectedresource.Target) error {
	stored := secret.DeepCopy()
	if stored.Namespace == "" {
		stored.Namespace = testNamespace
	}
	stored.UID = types.UID("uid-Secret-" + stored.Name)
	if len(stored.StringData) > 0 {
		if stored.Data == nil {
			stored.Data = map[string][]byte{}
		}
		for k, v := range stored.StringData {
			stored.Data[k] = []byte(v)
		}
		stored.StringData = nil
	}
	return c.base.CreateSecret(ctx, stored, target)
}

func (c *fakeCluster) CreateConfigMap(ctx context.Context, configMap *corev1.ConfigMap, target protectedresource.Target) error {
	stored := configMap.DeepCopy()
	if stored.Namespace == "" {
		stored.Namespace = testNamespace
	}
	stored.UID = types.UID("uid-ConfigMap-" + stored.Name)
	return c.base.CreateConfigMap(ctx, stored, target)
}

func (c *fakeCluster) ScaleDeployment(ctx context.Context, name string, replicas int32, target protectedresource.Target) error {
	if err := c.base.ScaleDeployment(ctx, name, replicas, target); err != nil {
		return err
	}
	return c.scaleDeploymentInStore(name, replicas)
}

func (c *fakeCluster) DeleteResources(ctx context.Context, refs []ResourceRef, target protectedresource.Target) error {
	if err := c.base.DeleteResources(ctx, refs, target); err != nil {
		return err
	}
	// Simulate Kubernetes garbage collection via ownerReferences.
	for _, ref := range refs {
		if strings.EqualFold(ref.Kind, "deployment") {
			_ = c.clientset.AppsV1().ReplicaSets(testNamespace).Delete(ctx, ref.Name, metav1.DeleteOptions{})
			_ = c.clientset.CoreV1().Pods(testNamespace).Delete(ctx, ref.Name+"-7d9", metav1.DeleteOptions{})
		}
	}
	return nil
}

func (c *fakeCluster) ListApplicationResources(ctx context.Context, labelSelector string) (ApplicationResourceList, error) {
	return c.base.ListApplicationResources(ctx, labelSelector)
}

func (c *fakeCluster) ListPVCs(ctx context.Context, labelSelector string) ([]corev1.PersistentVolumeClaim, error) {
	return c.base.ListPVCs(ctx, labelSelector)
}

func (c *fakeCluster) GetSecret(ctx context.Context, name string) (*corev1.Secret, error) {
	return c.base.GetSecret(ctx, name)
}

func (c *fakeCluster) GetConfigMap(ctx context.Context, name string) (*corev1.ConfigMap, error) {
	return c.base.GetConfigMap(ctx, name)
}

func (c *fakeCluster) GetNetworkPolicy(ctx context.Context, name string) (*networkingv1.NetworkPolicy, error) {
	return c.base.GetNetworkPolicy(ctx, name)
}

func (c *fakeCluster) materializeDeployment(ctx context.Context, dep *appsv1.Deployment) {
	name := dep.Name
	controller := true
	replicaTemplate := dep.Spec.Template.DeepCopy()
	if replicaTemplate.Labels == nil {
		replicaTemplate.Labels = map[string]string{}
	}
	replicaTemplate.Labels["pod-template-hash"] = "hash"
	rs := &appsv1.ReplicaSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
			UID:       types.UID("uid-ReplicaSet-" + name),
			Labels:    maps.Clone(dep.Labels),
			OwnerReferences: []metav1.OwnerReference{{
				Kind:       "Deployment",
				UID:        dep.UID,
				Controller: &controller,
			}},
		},
		Spec: appsv1.ReplicaSetSpec{Template: *replicaTemplate},
	}
	if _, err := c.clientset.AppsV1().ReplicaSets(testNamespace).Update(ctx, rs, metav1.UpdateOptions{}); apierrors.IsNotFound(err) {
		_, _ = c.clientset.AppsV1().ReplicaSets(testNamespace).Create(ctx, rs, metav1.CreateOptions{})
	}
	replicas := int32(1)
	if dep.Spec.Replicas != nil {
		replicas = *dep.Spec.Replicas
	}
	if replicas > 0 {
		image := ""
		if len(dep.Spec.Template.Spec.Containers) > 0 {
			image = dep.Spec.Template.Spec.Containers[0].Image
		}
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name + "-7d9",
				Namespace: testNamespace,
				UID:       types.UID("uid-Pod-" + name),
				Labels:    maps.Clone(replicaTemplate.Labels),
				OwnerReferences: []metav1.OwnerReference{{
					Kind:       "ReplicaSet",
					UID:        rs.UID,
					Controller: &controller,
				}},
			},
			Spec: corev1.PodSpec{
				Containers:      append([]corev1.Container(nil), dep.Spec.Template.Spec.Containers...),
				SecurityContext: dep.Spec.Template.Spec.SecurityContext.DeepCopy(),
			},
			Status: corev1.PodStatus{
				Phase: corev1.PodRunning,
				Conditions: []corev1.PodCondition{{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				}},
				ContainerStatuses: []corev1.ContainerStatus{{
					Name:    "app",
					Ready:   true,
					ImageID: image,
				}},
			},
		}
		if _, err := c.clientset.CoreV1().Pods(testNamespace).Update(ctx, pod, metav1.UpdateOptions{}); apierrors.IsNotFound(err) {
			_, _ = c.clientset.CoreV1().Pods(testNamespace).Create(ctx, pod, metav1.CreateOptions{})
		}
	} else {
		_ = c.clientset.CoreV1().Pods(testNamespace).Delete(ctx, name+"-7d9", metav1.DeleteOptions{})
	}
}

func (c *fakeCluster) storePVC(pvc *corev1.PersistentVolumeClaim) {
	stored := pvc.DeepCopy()
	if stored.Namespace == "" {
		stored.Namespace = testNamespace
	}
	stored.UID = types.UID("uid-PersistentVolumeClaim-" + stored.Name)
	_, _ = c.clientset.CoreV1().PersistentVolumeClaims(stored.Namespace).Create(context.Background(), stored, metav1.CreateOptions{})
}

func (c *fakeCluster) storeSecret(secret *corev1.Secret) {
	stored := secret.DeepCopy()
	if stored.Namespace == "" {
		stored.Namespace = testNamespace
	}
	stored.UID = types.UID("uid-Secret-" + stored.Name)
	_, _ = c.clientset.CoreV1().Secrets(stored.Namespace).Create(context.Background(), stored, metav1.CreateOptions{})
}

func (c *fakeCluster) deployment(t *testing.T, name string) *appsv1.Deployment {
	t.Helper()
	dep, err := c.clientset.AppsV1().Deployments(testNamespace).Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get deployment %s: %v", name, err)
	}
	return dep
}

func (c *fakeCluster) objectCount(t *testing.T) int {
	t.Helper()
	ctx := context.Background()
	deps, _ := c.clientset.AppsV1().Deployments(testNamespace).List(ctx, metav1.ListOptions{})
	rss, _ := c.clientset.AppsV1().ReplicaSets(testNamespace).List(ctx, metav1.ListOptions{})
	pods, _ := c.clientset.CoreV1().Pods(testNamespace).List(ctx, metav1.ListOptions{})
	svcs, _ := c.clientset.CoreV1().Services(testNamespace).List(ctx, metav1.ListOptions{})
	nps, _ := c.clientset.NetworkingV1().NetworkPolicies(testNamespace).List(ctx, metav1.ListOptions{})
	secs, _ := c.clientset.CoreV1().Secrets(testNamespace).List(ctx, metav1.ListOptions{})
	cms, _ := c.clientset.CoreV1().ConfigMaps(testNamespace).List(ctx, metav1.ListOptions{})
	pvcs, _ := c.clientset.CoreV1().PersistentVolumeClaims(testNamespace).List(ctx, metav1.ListOptions{})
	return len(deps.Items) + len(rss.Items) + len(pods.Items) + len(svcs.Items) + len(nps.Items) + len(secs.Items) + len(cms.Items) + len(pvcs.Items)
}

// testInstallation is one installation's declared execution facts.
func testInstallation(peers ...map[string]string) Installation {
	return Installation{Namespace: testNamespace, ImagePullSecretName: "tcr-pull-secret", AdminPasswordSeed: "test-admin-password-seed-0001", ApplicationIngressPeers: peers}
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

func testExecutor(cluster ClusterClient, installation Installation) *Executor {
	return &Executor{
		Client:       cluster,
		Guard:        testGuard(),
		Installation: installation,
		ImageCommand: func(context.Context, string, string) ([]string, error) { return nil, nil },
	}
}

func pvcObject() *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "PersistentVolumeClaim"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pvc-1",
			Namespace: testNamespace,
			Labels: map[string]string{
				"oplcloud.cn/storage-id": k8sCostLabelValue("volume-1"),
			},
			Annotations: map[string]string{
				"opl_account_id":   "account-1",
				"opl_workspace_id": "workspace-1",
				"opl_resource_id":  "volume-1",
			},
		},
	}
}

// TestExecutorAppliesTheWorkloadOntoTheConfirmedPlacement is the decisive test of
// this ownership move: the workload's node, prepaid package, storage claim and
// image-pull Secret all come from the confirmed placement and the installation's own
// facts, and the application's ingress admits exactly the declared peer.
func TestExecutorAppliesTheWorkloadOntoTheConfirmedPlacement(t *testing.T) {
	cluster := newFakeCluster()
	cluster.storePVC(pvcObject())
	executor := testExecutor(cluster, testInstallation(map[string]string{"app.kubernetes.io/name": "opl-cloud", "app.kubernetes.io/component": "serve"}))

	input := testInput(t)
	observation, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement())
	if err != nil {
		t.Logf("observation=%+v", observation)
		t.Fatal(err)
	}
	if observation.Status != "ready" {
		t.Fatalf("observation=%+v want ready", observation)
	}
	applied := cluster.deployment(t, ComponentResourceName(input, contracts.WorkspaceApplicationComponentMain))
	if applied.Spec.Template.Spec.NodeSelector["kubernetes.io/hostname"] != "node-1" {
		t.Fatalf("nodeSelector does not name the confirmed node: %v", applied.Spec.Template.Spec.NodeSelector)
	}
	tolerations := applied.Spec.Template.Spec.Tolerations
	if len(tolerations) != 1 || tolerations[0].Value != "basic" {
		t.Fatalf("tolerations do not name the confirmed prepaid package: %v", tolerations)
	}
	claim := ""
	for _, volume := range applied.Spec.Template.Spec.Volumes {
		if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName != "" {
			claim = volume.PersistentVolumeClaim.ClaimName
		}
	}
	if claim != "pvc-1" {
		t.Fatalf("persistent mount does not use the confirmed storage claim: %v", applied.Spec.Template.Spec.Volumes)
	}
	pullSecrets := applied.Spec.Template.Spec.ImagePullSecrets
	if len(pullSecrets) != 1 || pullSecrets[0].Name != "tcr-pull-secret" {
		t.Fatalf("imagePullSecrets do not name the installation Secret: %v", pullSecrets)
	}
	policy, err := cluster.clientset.NetworkingV1().NetworkPolicies(testNamespace).Get(context.Background(), ComponentResourceName(input, "network"), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("get networkpolicy: %v", err)
	}
	if len(policy.Spec.Ingress) != 2 {
		t.Fatalf("application ingress must admit the declared peer and its siblings: %+v", policy.Spec.Ingress)
	}
	labels := policy.Spec.Ingress[0].From[0].PodSelector.MatchLabels
	if labels["app.kubernetes.io/component"] != "serve" || labels["app.kubernetes.io/name"] != "opl-cloud" {
		t.Fatalf("application ingress does not admit the installation's Serve access data plane: %v", labels)
	}
}

func TestExecutorProgressesTheOriginalDependencyChainOnlyOnEnsure(t *testing.T) {
	cluster := newFakeCluster()
	cluster.storePVC(pvcObject())
	executor := testExecutor(cluster, testInstallation(map[string]string{"app.kubernetes.io/component": "serve"}))
	input := testInput(t)
	input.Revision.Entrypoint = []string{"/main-entry"}
	executor.ImageCommand = func(context.Context, string, string) ([]string, error) { return []string{"main-default"}, nil }
	input.Revision.Dependencies = []contracts.WorkspaceApplicationDependency{
		{Name: "database", Image: input.Revision.Image, Command: contracts.WorkspaceApplicationDependencyCommand{Entrypoint: []string{"/database"}, Args: []string{"database-arg"}}},
		{Name: "service", Image: input.Revision.Image, DependsOn: []string{"database"}},
	}
	for attempt := 0; attempt < 3; attempt++ {
		observation, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement())
		if attempt == 2 {
			if err != nil || observation.Status != "ready" {
				t.Fatalf("final observation=%+v err=%v", observation, err)
			}
			for _, component := range observation.Components {
				deployment := cluster.deployment(t, ComponentResourceName(input, component.Name))
				expected := map[string][]string{"main": {"main-default"}, "database": {"database-arg"}}[component.Name]
				if !equalStringSlices(expected, deployment.Spec.Template.Spec.Containers[0].Args) {
					t.Fatalf("component %s args=%v want %v", component.Name, deployment.Spec.Template.Spec.Containers[0].Args, expected)
				}
			}
			continue
		}
		if !errors.Is(err, ErrWorkspaceLaunchPending) || observation.Status != "pending" {
			t.Fatalf("attempt=%d observation=%+v err=%v", attempt, observation, err)
		}
		if observation.Components[0].State != "absent" {
			t.Fatalf("main was admitted before its dependencies: %+v", observation.Components)
		}
		for _, component := range observation.Components {
			if component.Name == "database" && component.State != "ready" {
				t.Fatalf("database did not settle: %+v", component)
			}
			if component.Name == "service" {
				want := "absent"
				if attempt == 1 {
					want = "ready"
				}
				if component.State != want {
					t.Fatalf("service=%s want=%s", component.State, want)
				}
			}
		}
		// Read the settled dependencies repeatedly. No read may create the
		// next component that was absent from the original apply.
		objects := cluster.objectCount(t)
		for poll := 0; poll < 2; poll++ {
			readback, err := executor.ReadWorkspaceApplicationRuntime(context.Background(), input)
			if err != nil || readback.Status != "pending" || readback.Components[0].State != "absent" {
				t.Fatalf("readback=%+v err=%v", readback, err)
			}
			if cluster.objectCount(t) != objects {
				t.Fatal("read created runtime objects")
			}
		}
	}
}

func TestExecutorImageConflictRemainsReadableAsFailure(t *testing.T) {
	cluster := newFakeCluster()
	cluster.storePVC(pvcObject())
	executor := testExecutor(cluster, testInstallation(map[string]string{"app.kubernetes.io/component": "serve"}))
	input := testInput(t)
	if _, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); err != nil {
		t.Fatal(err)
	}
	name := ComponentResourceName(input, contracts.WorkspaceApplicationComponentMain)
	dep := cluster.deployment(t, name)
	dep.Spec.Template.Spec.Containers[0].Image = "registry.test/app@sha256:" + strings.Repeat("2", 64)
	if _, err := cluster.clientset.AppsV1().Deployments(testNamespace).Update(context.Background(), dep, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	readback, err := executor.ReadWorkspaceApplicationRuntime(context.Background(), input)
	if err != nil || readback.Status != "failed" {
		t.Fatalf("readback=%+v err=%v", readback, err)
	}
	if _, err = executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); err == nil || err.Error() != "tencent_application_component_conflict" {
		t.Fatalf("image conflict=%v", err)
	}
}

// TestExecutorRefusesAnInstallationFactItNeeds proves the executor never applies a
// workload onto an unconfirmed placement or with a missing installation fact.
func TestExecutorRefusesAnInstallationFactItNeeds(t *testing.T) {
	input := testInput(t)
	cluster := newFakeCluster()
	cluster.storePVC(pvcObject())

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
	deps, err := cluster.clientset.AppsV1().Deployments(testNamespace).List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(deps.Items) != 0 {
		t.Fatalf("a refused apply still created %d deployment(s)", len(deps.Items))
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
	cluster.storePVC(pvcObject())
	input := testInput(t)
	executor := testExecutor(cluster, testInstallation(map[string]string{"app": "opl"}))
	if _, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); err != nil {
		t.Fatal(err)
	}
	// A scaled-to-zero component is not an available application.
	name := ComponentResourceName(input, contracts.WorkspaceApplicationComponentMain)
	if err := cluster.scaleDeploymentInStore(name, 0); err != nil {
		t.Fatal(err)
	}
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
	cluster.storePVC(pvcObject())
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
	for _, gateway := range []bool{false, true} {
		t.Run(fmt.Sprintf("gateway=%t", gateway), func(t *testing.T) {
			cluster := newFakeCluster()
			cluster.storePVC(pvcObject())
			input := testInput(t)
			input.Revision.Credentials = []contracts.WorkspaceApplicationCredential{
				{Name: "admin", Kind: contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword, Target: "/run/secrets/admin", Username: "app-admin"},
				{Name: "session", Kind: contracts.WorkspaceApplicationCredentialWorkspaceSessionSecret, Target: "/run/secrets/session"},
			}
			input.Configuration.CredentialVersion = "frozen-application-credential-version"
			if gateway {
				input.Revision.Credentials = append(input.Revision.Credentials, contracts.WorkspaceApplicationCredential{Name: "gateway", Kind: contracts.WorkspaceApplicationCredentialGatewayKey, Target: "/run/secrets/gateway"})
				input.SecretBindings = []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", SecretRef: contracts.WorkspaceGatewaySecretRef(input.WorkspaceID), Version: "0123456789abcdef", Key: contracts.WorkspaceApplicationGatewayKeyField}}
				cluster.storeSecret(&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      contracts.WorkspaceGatewaySecretRef(input.WorkspaceID),
						Namespace: testNamespace,
						Annotations: map[string]string{
							"oplcloud.cn/account-id":     input.AccountID,
							"oplcloud.cn/workspace-id":   input.WorkspaceID,
							"oplcloud.cn/secret-version": input.SecretBindings[0].Version,
						},
					},
					Data: map[string][]byte{
						contracts.WorkspaceApplicationGatewayKeyField: []byte("gateway-key-value"),
					},
				})
			}
			var err error
			input.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
			if err != nil {
				t.Fatal(err)
			}
			installation := testInstallation(map[string]string{"app": "opl"})
			executor := testExecutor(cluster, installation)
			for _, operation := range []string{input.RuntimeOperationID, input.RuntimeOperationID + "-upgrade"} {
				input.RuntimeOperationID = operation
				if _, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), input, testPlacement()); err != nil {
					t.Fatal(err)
				}
				credentials, err := executor.ReadWorkspaceApplicationRuntimeCredentials(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				if credentials.WorkspaceID != input.WorkspaceID || credentials.RuntimeID != workspaceApplicationRuntimeID(operation) || credentials.WebUIUsername != "app-admin" || credentials.WebUIPassword != deriveWorkspaceAdminPassword(installation.AdminPasswordSeed, input.WorkspaceID, input.Configuration.CredentialVersion) {
					t.Fatal("applied password does not match its declared identity and frozen version")
				}
				secret, err := executor.applicationSecretObject(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				if string(secret.Data["admin"]) != deriveWorkspaceAdminPassword(installation.AdminPasswordSeed, input.WorkspaceID, input.Configuration.CredentialVersion) {
					t.Fatal("applied password changed its frozen identity")
				}
				if string(secret.Data["session"]) != deriveWorkspaceSessionSecret(installation.AdminPasswordSeed, input.WorkspaceID, input.Configuration.CredentialVersion) {
					t.Fatal("applied session secret changed its frozen identity")
				}
			}
		})
	}
}

type boundaryInterceptCluster struct {
	ClusterClient
	onApply func(protectedresource.Target) error
}

func (b *boundaryInterceptCluster) Apply(ctx context.Context, bundle *ApplyBundle, target protectedresource.Target) error {
	if err := b.onApply(target); err != nil {
		return err
	}
	return b.ClusterClient.Apply(ctx, bundle, target)
}

func TestExecutorRetainsOwnerBoundaryWithConfirmedTarget(t *testing.T) {
	cluster := newFakeCluster()
	boundaryFailure := errors.New("original owner journal unavailable")
	applyCalls := 0
	wrapped := &boundaryInterceptCluster{
		ClusterClient: cluster,
		onApply: func(target protectedresource.Target) error {
			applyCalls++
			if target.NodeName != testPlacement().ComputeNodeName {
				t.Fatal("owner boundary lost the confirmed target")
			}
			return boundaryFailure
		},
	}
	executor := testExecutor(wrapped, testInstallation(map[string]string{"app": "serve"}))
	_, err := executor.EnsureWorkspaceApplicationRuntime(context.Background(), testInput(t), testPlacement())
	if !errors.Is(err, boundaryFailure) || applyCalls != 1 {
		t.Fatalf("error=%v owner apply=%d", err, applyCalls)
	}
}
