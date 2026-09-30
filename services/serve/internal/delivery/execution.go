package delivery

// This file owns Serve's own Agent execution wiring: which provider executor this
// process runs, and which installation facts it needs.
//
// Serve owns application execution. A TKE installation applies the workload
// through this process's own executor, which reaches the cluster only through the
// installation's namespace and kubeconfig Secret reference and refuses to mutate
// the installation's protected compute.
//
// The Fabric signed HTTP application-runtime surface remains a declared migration
// source with exactly one remaining provider: the Local-Docker installation whose
// in-process executor has not moved yet. An installation that declares its own
// cluster facts and the migration-source boundary at the same time would have two
// writers for one workload, so it is refused at startup instead of resolved by
// precedence.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/protectedresource"
	"opl-cloud/services/serve/internal/tkeapply"
)

// applicationExecutor is Serve's provider-neutral application execution port. The
// adapter carries no delivery state; the executor applies, observes, changes the
// lifecycle of and reads the credential of the exact workload for one admitted
// revision.
type applicationExecutor interface {
	// Configured reports whether the executor can reach its execution boundary. An
	// installation that declares no execution fact has an absent capability, not an
	// anonymous one.
	Configured() error
	EnsureWorkspaceApplicationRuntime(context.Context, contracts.WorkspaceApplicationRuntimeInput, tkeapply.Placement) (contracts.WorkspaceApplicationRuntimeObservation, error)
	ReadWorkspaceApplicationRuntime(context.Context, contracts.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error)
	SetWorkspaceApplicationRuntimeLifecycle(context.Context, contracts.WorkspaceApplicationRuntimeInput, string) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error)
	ReadWorkspaceApplicationRuntimeLifecycle(context.Context, contracts.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error)
	ReadWorkspaceApplicationRuntimeCredentials(context.Context, contracts.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeCredentials, error)
}

// configureAgentExecution binds the one execution boundary this installation
// declares, or leaves the execution capability absent.
func configureAgentExecution(service *Service, origin RouteOrigin) error {
	if service == nil {
		return errors.New("serve service is required")
	}
	namespace := strings.TrimSpace(os.Getenv("OPL_K8S_NAMESPACE"))
	bridge := strings.TrimSpace(os.Getenv("OPL_FABRIC_APPLICATION_URL"))
	if namespace != "" && bridge != "" {
		return errors.New("serve Agent execution is declared twice: OPL_K8S_NAMESPACE and OPL_FABRIC_APPLICATION_URL cannot both name this installation's execution boundary")
	}
	switch {
	case namespace != "":
		installation, err := installationFromEnv(namespace)
		if err != nil {
			return err
		}
		service.Runtime = &agentExecutionAdapter{
			Executor: &tkeapply.Executor{
				Kubectl: tkeapply.KubectlRunner(os.Getenv("TENCENT_DEPLOY_KUBECONFIG_REF"), namespace),
				// The installation's protected-compute declaration is the same fact
				// set that bounds Fabric's provider. Serve reads it explicitly in its
				// own source so this process's requirement stays part of the
				// installation contract instead of hiding behind a shared default.
				Guard: protectedresource.FromMap(map[string]string{
					"OPL_SYSTEM_COMPUTE_NODE_POOL_ID":              os.Getenv("OPL_SYSTEM_COMPUTE_NODE_POOL_ID"),
					"OPL_SYSTEM_COMPUTE_MACHINE_ID":                os.Getenv("OPL_SYSTEM_COMPUTE_MACHINE_ID"),
					"OPL_SYSTEM_COMPUTE_NODE_NAME":                 os.Getenv("OPL_SYSTEM_COMPUTE_NODE_NAME"),
					"OPL_SYSTEM_COMPUTE_MACHINE_TYPE":              os.Getenv("OPL_SYSTEM_COMPUTE_MACHINE_TYPE"),
					"OPL_SYSTEM_COMPUTE_CVM_ID":                    os.Getenv("OPL_SYSTEM_COMPUTE_CVM_ID"),
					"OPL_FABRIC_TENCENT_TKE_PROVIDER_PROFILE_JSON": os.Getenv("OPL_FABRIC_TENCENT_TKE_PROVIDER_PROFILE_JSON"),
				}),
				Installation: installation,
			},
			Origin: origin,
		}
	case bridge != "":
		service.Runtime = &agentExecutionAdapter{
			Executor: &fabricApplicationBridge{BaseURL: bridge, Token: os.Getenv("OPL_FABRIC_SERVE_SERVICE_TOKEN"), CapabilityKey: os.Getenv("OPL_FABRIC_SERVE_CAPABILITY_KEY")},
			Origin:   origin,
		}
	}
	return nil
}

// installationFromEnv reads the installation-owned facts the in-process executor
// applies a workload with. Every absent fact stays absent: the executor fails
// closed at the step that needs it rather than substituting a default.
func installationFromEnv(namespace string) (tkeapply.Installation, error) {
	peers, err := applicationIngressPeers(os.Getenv("OPL_WORKSPACE_APPLICATION_INGRESS_PEERS"))
	if err != nil {
		return tkeapply.Installation{}, err
	}
	return tkeapply.Installation{
		Namespace:               strings.TrimSpace(namespace),
		ImagePullSecretName:     strings.TrimSpace(os.Getenv("OPL_IMAGE_PULL_SECRET_NAME")),
		AdminPasswordSeed:       strings.TrimSpace(os.Getenv("OPL_AIONUI_ADMIN_PASSWORD_SEED")),
		ApplicationIngressPeers: peers,
	}, nil
}

// applicationIngressPeers parses the installation's declared ingress peers. The
// declaration is a JSON object of label names to values naming the pods allowed to
// reach a Workspace application — the Serve access data plane that proxies to it. A
// malformed declaration is refused instead of silently leaving an application
// unreachable.
func applicationIngressPeers(raw string) ([]map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var peer map[string]string
	if json.Unmarshal([]byte(raw), &peer) != nil || len(peer) == 0 {
		return nil, errors.New("OPL_WORKSPACE_APPLICATION_INGRESS_PEERS must be a JSON object of label names to values")
	}
	for name, value := range peer {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
			return nil, errors.New("OPL_WORKSPACE_APPLICATION_INGRESS_PEERS must not name an empty label")
		}
	}
	return []map[string]string{peer}, nil
}
