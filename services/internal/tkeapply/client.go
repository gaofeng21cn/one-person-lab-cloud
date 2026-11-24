package tkeapply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applyappsv1 "k8s.io/client-go/applyconfigurations/apps/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	applynetworkingv1 "k8s.io/client-go/applyconfigurations/networking/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"opl-cloud/services/internal/protectedresource"
)

const ssaFieldManager = "opl-cloud-tkeapply"

// ResourceRef names one typed Kubernetes object to retire within the
// installation namespace.
type ResourceRef struct {
	Kind string
	Name string
}

// ComponentApplyItem pairs one admitted component's Service and Deployment
// Server-Side Apply configurations in application order.
type ComponentApplyItem struct {
	Service    *applycorev1.ServiceApplyConfiguration
	Deployment *applyappsv1.DeploymentApplyConfiguration
}

// ApplyBundle holds the strongly-typed Server-Side Apply configurations for one
// Workspace application revision.
type ApplyBundle struct {
	Components      []ComponentApplyItem
	NetworkPolicies []*applynetworkingv1.NetworkPolicyApplyConfiguration
}

// ListJSON encodes the apply bundle as a standard Kubernetes v1 List JSON
// document for manifest rendering and inspection.
func (b *ApplyBundle) ListJSON() []byte {
	items := make([]any, 0, len(b.Components)*2+len(b.NetworkPolicies))
	for _, component := range b.Components {
		if component.Service != nil {
			items = append(items, component.Service)
		}
		if component.Deployment != nil {
			items = append(items, component.Deployment)
		}
	}
	for _, policy := range b.NetworkPolicies {
		if policy != nil {
			items = append(items, policy)
		}
	}
	return mustJSON(map[string]any{
		"apiVersion": "v1",
		"kind":       "List",
		"items":      items,
	})
}

// ApplicationResourceList carries typed readback objects for one Workspace
// application workload within the installation namespace.
type ApplicationResourceList struct {
	Deployments     []appsv1.Deployment
	ReplicaSets     []appsv1.ReplicaSet
	Pods            []corev1.Pod
	Services        []corev1.Service
	NetworkPolicies []networkingv1.NetworkPolicy
	Secrets         []corev1.Secret
	ConfigMaps      []corev1.ConfigMap
}

// ClusterClient is the installation's single typed Kubernetes API boundary for
// Workspace application workloads. Production uses NewClusterClient backed by
// k8s.io/client-go (Server-Side Apply plus typed appsv1/corev1/networkingv1).
type ClusterClient interface {
	Apply(ctx context.Context, bundle *ApplyBundle, target protectedresource.Target) error
	CreateSecret(ctx context.Context, secret *corev1.Secret, target protectedresource.Target) error
	CreateConfigMap(ctx context.Context, configMap *corev1.ConfigMap, target protectedresource.Target) error
	ScaleDeployment(ctx context.Context, name string, replicas int32, target protectedresource.Target) error
	DeleteResources(ctx context.Context, refs []ResourceRef, target protectedresource.Target) error
	ListApplicationResources(ctx context.Context, labelSelector string) (ApplicationResourceList, error)
	ListPVCs(ctx context.Context, labelSelector string) ([]corev1.PersistentVolumeClaim, error)
	GetSecret(ctx context.Context, name string) (*corev1.Secret, error)
	GetConfigMap(ctx context.Context, name string) (*corev1.ConfigMap, error)
	GetNetworkPolicy(ctx context.Context, name string) (*networkingv1.NetworkPolicy, error)
}

type clientGoCluster struct {
	kubeconfigRef string
	namespace     string

	once      sync.Once
	clientset kubernetes.Interface
	initErr   error
}

// NewClusterClient constructs the installation's k8s.io/client-go cluster
// boundary against one installation namespace, loading the declared kubeconfig
// path when present or falling back to standard in-cluster / default kubeconfig
// discovery. The REST client initializes lazily on the first API call so wiring
// startup checks stay offline-safe.
func NewClusterClient(kubeconfigRef, namespace string) ClusterClient {
	return &clientGoCluster{
		kubeconfigRef: strings.TrimSpace(kubeconfigRef),
		namespace:     strings.TrimSpace(namespace),
	}
}

// NewClientsetCluster wraps an already constructed k8s.io/client-go
// kubernetes.Interface (such as *kubernetes.Clientset or fake.NewClientset)
// scoped to one installation namespace.
func NewClientsetCluster(clientset kubernetes.Interface, namespace string) ClusterClient {
	c := &clientGoCluster{
		namespace: strings.TrimSpace(namespace),
		clientset: clientset,
	}
	c.once.Do(func() {
		if c.namespace == "" {
			c.initErr = errors.New("OPL_K8S_NAMESPACE is required")
		} else if c.clientset == nil {
			c.initErr = errors.New("kubernetes clientset is required")
		}
	})
	return c
}

func (c *clientGoCluster) client() (kubernetes.Interface, error) {
	c.once.Do(func() {
		if c.namespace == "" {
			c.initErr = errors.New("OPL_K8S_NAMESPACE is required")
			return
		}
		cfg, err := buildRESTConfig(c.kubeconfigRef)
		if err != nil {
			c.initErr = err
			return
		}
		clientset, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			c.initErr = err
			return
		}
		c.clientset = clientset
	})
	if c.initErr != nil {
		return nil, c.initErr
	}
	return c.clientset, nil
}

func buildRESTConfig(kubeconfigRef string) (*rest.Config, error) {
	if kubeconfigRef != "" {
		return clientcmd.BuildConfigFromFlags("", kubeconfigRef)
	}
	if cfg, err := rest.InClusterConfig(); err == nil {
		return cfg, nil
	}
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{}).ClientConfig()
}

func (c *clientGoCluster) Apply(ctx context.Context, bundle *ApplyBundle, _ protectedresource.Target) error {
	clientset, err := c.client()
	if err != nil {
		return err
	}
	if bundle == nil {
		return nil
	}
	opts := metav1.ApplyOptions{FieldManager: ssaFieldManager, Force: true}
	for _, component := range bundle.Components {
		if component.Service != nil {
			if _, err := clientset.CoreV1().Services(c.namespace).Apply(ctx, component.Service, opts); err != nil {
				return err
			}
		}
		if component.Deployment != nil {
			if _, err := clientset.AppsV1().Deployments(c.namespace).Apply(ctx, component.Deployment, opts); err != nil {
				return err
			}
		}
	}
	for _, policy := range bundle.NetworkPolicies {
		if policy != nil {
			if _, err := clientset.NetworkingV1().NetworkPolicies(c.namespace).Apply(ctx, policy, opts); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *clientGoCluster) CreateSecret(ctx context.Context, secret *corev1.Secret, _ protectedresource.Target) error {
	clientset, err := c.client()
	if err != nil {
		return err
	}
	_, err = clientset.CoreV1().Secrets(c.namespace).Create(ctx, secret, metav1.CreateOptions{FieldManager: ssaFieldManager})
	return err
}

func (c *clientGoCluster) CreateConfigMap(ctx context.Context, configMap *corev1.ConfigMap, _ protectedresource.Target) error {
	clientset, err := c.client()
	if err != nil {
		return err
	}
	_, err = clientset.CoreV1().ConfigMaps(c.namespace).Create(ctx, configMap, metav1.CreateOptions{FieldManager: ssaFieldManager})
	return err
}

func (c *clientGoCluster) ScaleDeployment(ctx context.Context, name string, replicas int32, _ protectedresource.Target) error {
	clientset, err := c.client()
	if err != nil {
		return err
	}
	scale := &autoscalingv1.Scale{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: c.namespace},
		Spec:       autoscalingv1.ScaleSpec{Replicas: replicas},
	}
	_, err = clientset.AppsV1().Deployments(c.namespace).UpdateScale(ctx, name, scale, metav1.UpdateOptions{})
	return err
}

func (c *clientGoCluster) DeleteResources(ctx context.Context, refs []ResourceRef, _ protectedresource.Target) error {
	clientset, err := c.client()
	if err != nil {
		return err
	}
	policy := metav1.DeletePropagationBackground
	opts := metav1.DeleteOptions{PropagationPolicy: &policy}
	for _, ref := range refs {
		var delErr error
		switch strings.ToLower(ref.Kind) {
		case "deployment":
			delErr = clientset.AppsV1().Deployments(c.namespace).Delete(ctx, ref.Name, opts)
		case "service":
			delErr = clientset.CoreV1().Services(c.namespace).Delete(ctx, ref.Name, opts)
		case "configmap":
			delErr = clientset.CoreV1().ConfigMaps(c.namespace).Delete(ctx, ref.Name, opts)
		case "networkpolicy":
			delErr = clientset.NetworkingV1().NetworkPolicies(c.namespace).Delete(ctx, ref.Name, opts)
		case "secret":
			delErr = clientset.CoreV1().Secrets(c.namespace).Delete(ctx, ref.Name, opts)
		default:
			return fmt.Errorf("unsupported delete resource kind %q", ref.Kind)
		}
		if delErr != nil && !apierrors.IsNotFound(delErr) {
			return delErr
		}
	}
	return nil
}

func (c *clientGoCluster) ListApplicationResources(ctx context.Context, labelSelector string) (ApplicationResourceList, error) {
	clientset, err := c.client()
	if err != nil {
		return ApplicationResourceList{}, err
	}
	opts := metav1.ListOptions{LabelSelector: labelSelector}
	deployments, err := clientset.AppsV1().Deployments(c.namespace).List(ctx, opts)
	if err != nil {
		return ApplicationResourceList{}, err
	}
	replicaSets, err := clientset.AppsV1().ReplicaSets(c.namespace).List(ctx, opts)
	if err != nil {
		return ApplicationResourceList{}, err
	}
	pods, err := clientset.CoreV1().Pods(c.namespace).List(ctx, opts)
	if err != nil {
		return ApplicationResourceList{}, err
	}
	services, err := clientset.CoreV1().Services(c.namespace).List(ctx, opts)
	if err != nil {
		return ApplicationResourceList{}, err
	}
	networkPolicies, err := clientset.NetworkingV1().NetworkPolicies(c.namespace).List(ctx, opts)
	if err != nil {
		return ApplicationResourceList{}, err
	}
	secrets, err := clientset.CoreV1().Secrets(c.namespace).List(ctx, opts)
	if err != nil {
		return ApplicationResourceList{}, err
	}
	configMaps, err := clientset.CoreV1().ConfigMaps(c.namespace).List(ctx, opts)
	if err != nil {
		return ApplicationResourceList{}, err
	}
	return ApplicationResourceList{
		Deployments:     deployments.Items,
		ReplicaSets:     replicaSets.Items,
		Pods:            pods.Items,
		Services:        services.Items,
		NetworkPolicies: networkPolicies.Items,
		Secrets:         secrets.Items,
		ConfigMaps:      configMaps.Items,
	}, nil
}

func (c *clientGoCluster) ListPVCs(ctx context.Context, labelSelector string) ([]corev1.PersistentVolumeClaim, error) {
	clientset, err := c.client()
	if err != nil {
		return nil, err
	}
	list, err := clientset.CoreV1().PersistentVolumeClaims(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (c *clientGoCluster) GetSecret(ctx context.Context, name string) (*corev1.Secret, error) {
	clientset, err := c.client()
	if err != nil {
		return nil, err
	}
	secret, err := clientset.CoreV1().Secrets(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return secret, nil
}

func (c *clientGoCluster) GetConfigMap(ctx context.Context, name string) (*corev1.ConfigMap, error) {
	clientset, err := c.client()
	if err != nil {
		return nil, err
	}
	configMap, err := clientset.CoreV1().ConfigMaps(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return configMap, nil
}

func (c *clientGoCluster) GetNetworkPolicy(ctx context.Context, name string) (*networkingv1.NetworkPolicy, error) {
	clientset, err := c.client()
	if err != nil {
		return nil, err
	}
	policy, err := clientset.NetworkingV1().NetworkPolicies(c.namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return policy, nil
}

func toMap(value any) map[string]any {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if json.Unmarshal(raw, &out) != nil {
		return nil
	}
	return out
}
