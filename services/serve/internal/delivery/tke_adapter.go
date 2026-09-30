package delivery

// agentExecutionAdapter executes one already-admitted descriptor through this
// Serve process's own application executor.
//
// Before this adapter ran in-process, the workload was applied by Fabric's
// signed HTTP application-runtime surface: Serve only described what it wanted and
// another owner ran the cluster mutation. Application execution now belongs to
// Serve, so the adapter calls its own executor and Fabric keeps only infrastructure
// resource facts and their readback.
//
// The adapter exists because three facts have exactly one owner and Serve must
// respect each of them:
//
//   - The provider reports the *in-cluster destination* it created, not the
//     address a customer opens. Serve therefore resolves the publishable URL from
//     the installation's declared route origin and never treats the provider's
//     service name or port as an Entry URL. A gateway entry with no declared
//     origin is recorded as not-open rather than published at a guessed host.
//   - Readiness is the whole-runtime observation. A Pod that is merely Running, or
//     a persisted pointer, is not readiness: every declared component must be
//     observed ready *and* a publishable entry must resolve before Serve records
//     the Agent as ready.
//   - The workload is scheduled onto the exact placement Fabric confirmed for the
//     Workspace's own prepaid resources. A readback that does not carry that
//     placement is refused instead of scheduling onto a guessed node, package or
//     claim.

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/serve/internal/tkeapply"
)

// ReasonCredentialInjectionWireMissing names the one injection the current
// contract cannot carry. Serve holds an opaque Secret binding identity and the
// model configuration; the runtime input needs the Secret delivery reference, and
// no field on the runtime command or the runtime input transports it. Recording a
// delivery without injection would claim an Agent that cannot reach its model, so
// the adapter refuses and names the exact owner fact it needs.
const ReasonCredentialInjectionWireMissing = "credential_injection_wire_missing"

// agentExecutionAdapter executes one already-admitted descriptor against the
// installation's Agent execution boundary. Resource bindings and their confirmed
// placement are Fabric readbacks, never caller input, and the browser-facing origin
// comes from the installation's declared RouteOrigin.
type agentExecutionAdapter struct {
	// Executor is the one execution boundary this installation declares. A process
	// without one has an absent execution capability, not an anonymous one.
	Executor applicationExecutor
	Origin   RouteOrigin
}

func (a *agentExecutionAdapter) Start(ctx context.Context, c *api.RuntimeDeployCommand, target ExecutionTarget) (RuntimeObservation, error) {
	var zero RuntimeObservation
	input, err := a.runtimeInput(c, target)
	if err != nil {
		return zero, err
	}
	observation, err := a.Executor.EnsureWorkspaceApplicationRuntime(ctx, input, placementFor(target, c.GetWorkspaceId()))
	if err != nil {
		if !isExecutionPending(err) {
			return zero, err
		}
		// Pending is not a failure: the components are converging and the
		// observation carries the live state. Start still never claims readiness.
	}
	out, err := a.observe(c, input, observation)
	if err != nil {
		return zero, err
	}
	// Start acknowledges the request. Readiness is the separate observation's fact,
	// so a start never carries readiness evidence even when the workload it applied
	// already serves.
	out.State, out.ReadinessEvidenceRef = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING, ""
	return out, nil
}

func (a *agentExecutionAdapter) Observe(ctx context.Context, c *api.RuntimeDeployCommand, target ExecutionTarget) (RuntimeObservation, error) {
	var zero RuntimeObservation
	input, err := a.runtimeInput(c, target)
	if err != nil {
		return zero, err
	}
	observation, err := a.Executor.ReadWorkspaceApplicationRuntime(ctx, input)
	if err != nil {
		return zero, err
	}
	return a.observe(c, input, observation)
}

// runtimeInput restates the exact deployment descriptor Serve was asked to run as
// the executor's typed input. The resource binding is Serve's own readback, and the
// model configuration the caller requested is validated here rather than silently
// dropped.
func (a *agentExecutionAdapter) runtimeInput(c *api.RuntimeDeployCommand, target ExecutionTarget) (contracts.WorkspaceApplicationRuntimeInput, error) {
	var zero contracts.WorkspaceApplicationRuntimeInput
	if a == nil || a.Executor == nil {
		return zero, status.Error(codes.Unavailable, "Serve Agent execution is not configured")
	}
	if err := a.Executor.Configured(); err != nil {
		return zero, status.Error(codes.Unavailable, "Serve Agent execution is not configured")
	}
	if c == nil || c.GetDeploymentDescriptor() == nil || target.Binding == nil {
		return zero, status.Error(codes.InvalidArgument, "deployment descriptor and resource binding are required")
	}
	raw, err := publicjson.Marshal(c.GetDeploymentDescriptor().GetApplicationRevision())
	if err != nil {
		return zero, err
	}
	var revision contracts.WorkspaceApplicationRevision
	if json.Unmarshal(raw, &revision) != nil || contracts.ValidateWorkspaceApplicationRevision(revision) != nil {
		return zero, status.Error(codes.InvalidArgument, "invalid application revision")
	}
	b := target.Binding
	input := contracts.WorkspaceApplicationRuntimeInput{SchemaVersion: 2, AccountID: b.GetAccountId(), WorkspaceID: c.GetWorkspaceId(), ComputeID: b.GetComputeAllocationId(), VolumeID: b.GetStorageVolumeId(), AttachmentID: b.GetDataAttachmentId(), AttachmentOperationID: b.GetDataAttachmentOperationId(), RuntimeOperationID: c.GetRuntimeInstanceId(), Revision: revision, DataBindingID: c.GetDataAttachmentId()}
	secretBindings, err := managedKeyRuntimeBindings(c)
	if err != nil {
		return zero, err
	}
	// An application that declares a Secret or platform credential must execute
	// with the exact confirmed binding. A declared Secret with no resolved binding
	// is refused rather than started un-injected and unable to reach its model.
	if referencedSecretRequired(revision) && len(secretBindings) == 0 {
		return zero, owneridentity.WithErrorCode(status.Errorf(codes.FailedPrecondition, "%s: Serve holds Secret binding %q and model configuration version %d, but the declared Secret has no confirmed delivery to inject", ReasonCredentialInjectionWireMissing, strings.TrimSpace(c.GetSecretBindingId()), c.GetModelConfigurationVersion()), api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE)
	}
	input.SecretBindings = secretBindings
	// A revision that declares a platform credential carries that credential's
	// identity so the input names the exact generation it was bound against. The
	// managed-key binding's confirmed Secret version is that identity.
	if binding := c.GetManagedKeyBinding(); binding != nil && strings.TrimSpace(binding.GetSecretVersion()) != "" {
		input.Configuration.CredentialVersion = binding.GetSecretVersion()
	}
	input.ConfigurationDigest, err = contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
	if err != nil {
		return zero, err
	}
	if err = contracts.ValidateWorkspaceApplicationRuntimeConfiguration(input); err != nil {
		return zero, status.Error(codes.FailedPrecondition, "application configuration or protected secret binding is required")
	}
	return input, nil
}

// observe maps one executor observation onto Serve's runtime observation. Readiness
// is the whole-runtime fact, not one component and not a Pod phase, and a start
// acknowledges the request without claiming readiness.
func (a *agentExecutionAdapter) observe(c *api.RuntimeDeployCommand, input contracts.WorkspaceApplicationRuntimeInput, observation contracts.WorkspaceApplicationRuntimeObservation) (RuntimeObservation, error) {
	var zero RuntimeObservation
	if observation.WorkspaceID != input.WorkspaceID || observation.RuntimeID != contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID) || contracts.ValidateWorkspaceApplicationRuntimeObservation(input.Revision, observation) != nil {
		return zero, status.Error(codes.FailedPrecondition, "Agent runtime observation does not match the exact original input")
	}
	out := RuntimeObservation{ObservedAt: time.Now().UTC(), Components: observation.Components, ApplicationEntry: observation.Entry}
	switch observation.Status {
	case "ready":
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY
	case "pending":
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING
	case "failed":
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED
	case "absent":
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED
	default:
		return zero, status.Error(codes.FailedPrecondition, "Agent runtime state is unknown")
	}
	allComponentsReady := deploymentComponentsReady(input.Revision, observation.Components)
	if out.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY || !allComponentsReady {
		if out.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
			out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING
		}
		return out, nil
	}
	if observation.Entry == nil {
		// The runtime serves, but it publishes no web entry at all (for example an
		// application that declares no entry port or a cloud_private exposure). Serve
		// invents no address for it and claims no publishable readiness, so the
		// delivery is recorded as not-open rather than as an open application.
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING
		return out, nil
	}
	accessURL, upstream, resolveErr := resolveApplicationEntry(a.Origin, c.GetWorkspaceId(), input.Revision.ApplicationID, *observation.Entry)
	if resolveErr != nil {
		// The application runs, but no publishable address is provable. Serve never
		// publishes a guessed host; only a readback reports the running state, and a
		// start stays starting because it cannot prove readiness at all.
		out.AccessURL = ""
		out.State = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING
		return out, nil
	}
	out.AccessURL = accessURL
	// A gateway entry is reachable only through the installation's origin, and
	// the only destination behind it is the Service the executor created. Serve
	// records that exact Service and port so its own access data plane proxies to
	// a reported destination instead of composing one.
	if strings.TrimSpace(observation.Entry.URL) == "" {
		out.AccessUpstreamService = observation.Entry.ServiceName
		out.AccessUpstreamPort = observation.Entry.Port
	}
	out.ReadinessEvidenceRef = tkeReadinessEvidence(input.RuntimeOperationID, accessURL, upstream, observation, c.GetModelConfigurationVersion())
	return out, nil
}

// deploymentComponentsReady reports whether the observation accounts for every
// declared component (the main component plus one per dependency) and every one
// of them is ready. A missing component is never treated as ready.
func deploymentComponentsReady(revision contracts.WorkspaceApplicationRevision, observed []contracts.WorkspaceApplicationRuntimeComponentState) bool {
	declared := contracts.WorkspaceApplicationRuntimeComponents(revision)
	if len(observed) != len(declared) {
		return false
	}
	state := make(map[string]string, len(observed))
	for _, component := range observed {
		state[component.Name] = component.State
	}
	for _, component := range declared {
		if state[component.Name] != "ready" {
			return false
		}
	}
	return true
}

// tkeReadinessEvidence is the provider evidence identity for a ready report. It
// binds the runtime operation, the resolved route, the validated in-cluster
// upstream, the whole-runtime observation and the applied model configuration
// version, so the readiness fact names exactly what was observed.
func tkeReadinessEvidence(runtimeOperationID, accessURL, upstream string, observation contracts.WorkspaceApplicationRuntimeObservation, modelConfigurationVersion int64) string {
	raw, _ := json.Marshal(observation)
	sum := sha256.Sum256(append([]byte(fmt.Sprintf("tke-application-readback:%s:%s:%d ", upstream, accessURL, modelConfigurationVersion)), raw...))
	return fmt.Sprintf("tke-application-readback:%s:%s:sha256:%x", runtimeOperationID, accessURL, sum)
}

// referencedSecretRequired reports whether the revision declares a Secret or
// credential the execution boundary must inject. An application that declares
// none executes with no injection and needs no wire.
func referencedSecretRequired(revision contracts.WorkspaceApplicationRevision) bool {
	if len(revision.SecretInputs) > 0 {
		return true
	}
	for _, dependency := range revision.Dependencies {
		if len(dependency.SecretInputs) > 0 {
			return true
		}
	}
	return contracts.WorkspaceApplicationRequiresPlatformCredentials(revision)
}

// Lifecycle applies a desired state to the exact runtime through this Serve
// process's own executor and confirms the executor observed the applied state. A
// readback that does not show the requested state is refused rather than reported
// as an applied lifecycle.
func (a *agentExecutionAdapter) Lifecycle(ctx context.Context, c *api.RuntimeDeployCommand, target ExecutionTarget, desired string) error {
	input, err := a.runtimeInput(c, target)
	if err != nil {
		return err
	}
	result, err := a.Executor.SetWorkspaceApplicationRuntimeLifecycle(ctx, input, desired)
	if err != nil {
		return err
	}
	if result.RuntimeID != contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID) || result.WorkspaceID != input.WorkspaceID {
		return status.Error(codes.FailedPrecondition, "Agent lifecycle readback does not match the exact runtime")
	}
	// A suspended application may already be fully absent: the executor reports the
	// stronger fact, and both mean the writer stopped.
	if result.State != desired && !(desired == "suspended" && result.State == "absent") {
		return status.Errorf(codes.FailedPrecondition, "Agent lifecycle readback reports %q, not %q", result.State, desired)
	}
	return nil
}

// Reload applies the command's model configuration through the lifecycle
// boundary's running state, which re-reads the configuration the runtime already
// holds. Applying a new model configuration and reading its applied version back is
// a separate owner step; until then Serve does not report the requested version as
// applied.
func (a *agentExecutionAdapter) Reload(ctx context.Context, c *api.RuntimeDeployCommand, target ExecutionTarget) error {
	return a.Lifecycle(ctx, c, target, "running")
}

// Credentials reads the platform-issued WebUI credential for the exact runtime. The
// value travels only in the response and is never persisted.
func (a *agentExecutionAdapter) Credentials(ctx context.Context, c *api.RuntimeDeployCommand, target ExecutionTarget) (*api.WorkspaceApplicationCredentials, error) {
	input, err := a.runtimeInput(c, target)
	if err != nil {
		return nil, err
	}
	credentials, err := a.Executor.ReadWorkspaceApplicationRuntimeCredentials(ctx, input)
	if err != nil {
		return nil, err
	}
	if credentials.RuntimeID != contracts.WorkspaceApplicationRuntimeID(c.GetRuntimeInstanceId()) || credentials.WorkspaceID != c.GetWorkspaceId() {
		return nil, status.Error(codes.FailedPrecondition, "Agent credential readback does not match the exact runtime")
	}
	return &api.WorkspaceApplicationCredentials{WorkspaceId: credentials.WorkspaceID, RuntimeInstanceId: credentials.RuntimeID, Username: credentials.WebUIUsername, Password: credentials.WebUIPassword}, nil
}

// placementFor projects the resources owner's published placement onto the
// executor's own placement type. A readback that published none projects none, and
// the executor that schedules the workload refuses it; the Workspace identity is
// the delivery's own confirmed workspace, so the workload is never applied to
// another Workspace's placement.
func placementFor(target ExecutionTarget, workspaceID string) tkeapply.Placement {
	placement := target.Placement
	return tkeapply.Placement{
		AccountID:          target.Binding.GetAccountId(),
		WorkspaceID:        workspaceID,
		ComputeID:          target.Binding.GetComputeAllocationId(),
		StorageVolumeID:    target.Binding.GetStorageVolumeId(),
		ComputeNodeName:    placement.GetComputeNodeName(),
		ComputePackageID:   placement.GetComputePackageId(),
		ComputeNodePoolID:  placement.GetComputeNodePoolId(),
		ComputeMachineName: placement.GetComputeMachineName(),
		ComputeInstanceID:  placement.GetComputeInstanceId(),
		StoragePVCName:     placement.GetStoragePvcName(),
	}
}

// isExecutionPending reports whether the executor's error is its honest progress
// signal rather than a refused execution.
func isExecutionPending(err error) bool {
	return errors.Is(err, tkeapply.ErrWorkspaceLaunchPending)
}
