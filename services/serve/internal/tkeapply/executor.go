package tkeapply

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"opl-cloud/services/internal/protectedresource"
)

// Installation is the installation-owned cluster configuration the workload
// executor needs. Every field is declared by the installation that deploys Serve;
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
// Kubectl and Guard are injected so the executor reaches the cluster through the
// installation's single cluster-access boundary and refuses any mutation that
// targets the installation's own protected compute.
type Executor struct {
	// Kubectl runs one kubectl invocation against the installation cluster. It is
	// the only way this executor reaches the cluster.
	Kubectl func(ctx context.Context, args []string, stdin []byte) ([]byte, error)
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
	if e == nil || e.Kubectl == nil {
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

// callKubectl runs one kubectl invocation, guarding every mutating action against
// the installation's protected compute before it reaches the cluster. Read-only
// actions pass through unchanged so an observation never fails on a guard that
// only constrains mutations.
func (e *Executor) callKubectl(ctx context.Context, args []string, stdin []byte, target protectedresource.Target) ([]byte, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("kubectl_action_required")
	}
	if err := e.clusterAccessError(); err != nil {
		return nil, err
	}
	switch args[0] {
	case "get", "wait", "logs", "describe", "version":
	default:
		if err := e.Guard.Check(target); err != nil {
			return nil, err
		}
	}
	return e.Kubectl(ctx, args, stdin)
}
