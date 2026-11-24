package tkeapply

import (
	"context"
	"errors"
	"net/http"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"

	"opl-cloud/services/internal/protectedresource"
)

// Installation is the installation-owned cluster configuration the workload
// executor needs. Every field is declared by the installation that deploys its owning process;
// none of them is inferred from a caller, a resource readback or another owner's
// naming, because the installation is the only authority for its own namespace,
// its own image-pull Secret and the pods allowed to reach a Workspace
// application.
type Installation struct {
	// Namespace is the installation namespace every Workspace application object
	// is applied to.
	Namespace string
	// ImagePullSecretName is the installation's own registry pull Secret that the
	// application pods reference.
	ImagePullSecretName string
	// AdminPasswordSeed derives the workspace-admin credential of an application
	// that declares one. An application that declares no such credential does not
	// need it.
	AdminPasswordSeed string
	// ApplicationIngressPeers are the installation-declared label selectors of the
	// pods allowed to reach a Workspace application: the Serve access data plane
	// that proxies to it. An installation that declares none cannot express the
	// application's ingress, so applying the workload fails closed instead of
	// opening an application to unlisted pods.
	ApplicationIngressPeers []map[string]string
}

// Executor applies one Workspace application workload to the installation's own
// Kubernetes namespace. It owns the workload objects of the exact revision it was
// given and nothing else: it never purchases, deletes or mutates an
// infrastructure resource, and every value it schedules onto comes from the
// confirmed placement the resources owner published.
//
// Client and Guard are injected so the executor reaches the cluster through the
// installation's single typed Kubernetes API boundary (k8s.io/client-go) and
// refuses any mutation that targets the installation's own protected compute.
type Executor struct {
	// ImageCommand reads OCI CMD for the exact approved image/platform. Tests may
	// inject the registry transport; production uses the installation pull Secret
	// via go-containerregistry in memory.
	ImageCommand func(context.Context, string, string) ([]string, error)
	// RegistryTransport overrides the HTTP RoundTripper used by
	// go-containerregistry when inspecting OCI image configs. Nil uses
	// remote.DefaultTransport.
	RegistryTransport http.RoundTripper
	// CredentialToken resolves legacy credential provenance at its original owner.
	// A nil resolver retains the strict default with no historical proof.
	CredentialToken func(context.Context, WorkspaceApplicationRuntimeInput) (string, error)
	// Client is the single typed Kubernetes API boundary this executor uses to
	// reach the installation cluster.
	Client ClusterClient
	// Guard refuses a mutation that targets the installation's protected compute.
	Guard protectedresource.Config
	// Installation is the installation-owned cluster configuration the workload is
	// applied with.
	Installation Installation
	// HTTPClient issues the boundary's requests to the application it applied: the
	// publisher-declared model configuration interface, whose destination is the
	// Service this executor created for a declared port. A nil client uses the
	// boundary's own bounded default.
	HTTPClient *http.Client
}

// Configured reports whether the executor can reach the cluster. An installation
// that declares no namespace and no cluster access has an absent capability, not
// an anonymous one, and the workload step fails closed instead of guessing.
func (e *Executor) Configured() error {
	return e.clusterAccessError()
}

func (e *Executor) clusterAccessError() error {
	if e == nil || e.Client == nil {
		return errors.New("workspace application cluster access is not configured")
	}
	if strings.TrimSpace(e.Installation.Namespace) == "" {
		return errors.New("OPL_K8S_NAMESPACE is required")
	}
	return nil
}

// applyConfigError names every installation fact required to render a Workspace
// application. An installation that declares no pull Secret or no admitted
// ingress peer cannot apply the workload, so Ensure refuses instead of applying an
// object that pulls anonymously or admits no peer.
func (e *Executor) applyConfigError() error {
	if err := e.clusterAccessError(); err != nil {
		return err
	}
	if strings.TrimSpace(e.Installation.ImagePullSecretName) == "" {
		return errors.New("OPL_IMAGE_PULL_SECRET_NAME is required")
	}
	if len(e.Installation.ApplicationIngressPeers) == 0 {
		return errors.New("OPL_WORKSPACE_APPLICATION_INGRESS_PEERS is required")
	}
	return nil
}

func (e *Executor) checkMutation(target protectedresource.Target) error {
	if err := e.clusterAccessError(); err != nil {
		return err
	}
	return e.Guard.Check(target)
}

func (e *Executor) applyBundle(ctx context.Context, bundle *ApplyBundle, target protectedresource.Target) error {
	if err := e.checkMutation(target); err != nil {
		return err
	}
	return e.Client.Apply(ctx, bundle, target)
}

func (e *Executor) createSecret(ctx context.Context, secret *corev1.Secret, target protectedresource.Target) error {
	if err := e.checkMutation(target); err != nil {
		return err
	}
	return e.Client.CreateSecret(ctx, secret, target)
}

func (e *Executor) createConfigMap(ctx context.Context, configMap *corev1.ConfigMap, target protectedresource.Target) error {
	if err := e.checkMutation(target); err != nil {
		return err
	}
	return e.Client.CreateConfigMap(ctx, configMap, target)
}

func (e *Executor) scaleDeployment(ctx context.Context, name string, replicas int32, target protectedresource.Target) error {
	if err := e.checkMutation(target); err != nil {
		return err
	}
	return e.Client.ScaleDeployment(ctx, name, replicas, target)
}

func (e *Executor) deleteResources(ctx context.Context, refs []ResourceRef, target protectedresource.Target) error {
	if err := e.checkMutation(target); err != nil {
		return err
	}
	return e.Client.DeleteResources(ctx, refs, target)
}

func (e *Executor) listApplicationResources(ctx context.Context, labelSelector string) (ApplicationResourceList, error) {
	if err := e.clusterAccessError(); err != nil {
		return ApplicationResourceList{}, err
	}
	return e.Client.ListApplicationResources(ctx, labelSelector)
}

func (e *Executor) listPVCs(ctx context.Context, labelSelector string) ([]corev1.PersistentVolumeClaim, error) {
	if err := e.clusterAccessError(); err != nil {
		return nil, err
	}
	return e.Client.ListPVCs(ctx, labelSelector)
}

func (e *Executor) getSecret(ctx context.Context, name string) (*corev1.Secret, error) {
	if err := e.clusterAccessError(); err != nil {
		return nil, err
	}
	return e.Client.GetSecret(ctx, name)
}

func (e *Executor) getConfigMap(ctx context.Context, name string) (*corev1.ConfigMap, error) {
	if err := e.clusterAccessError(); err != nil {
		return nil, err
	}
	return e.Client.GetConfigMap(ctx, name)
}

func (e *Executor) getNetworkPolicy(ctx context.Context, name string) (*networkingv1.NetworkPolicy, error) {
	if err := e.clusterAccessError(); err != nil {
		return nil, err
	}
	return e.Client.GetNetworkPolicy(ctx, name)
}
