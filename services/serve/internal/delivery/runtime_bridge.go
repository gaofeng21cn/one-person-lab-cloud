package delivery

// ServeRuntimeAdapter is Serve's own execution-adapter surface.
//
// The typed contract decomposes Serve's delivery steps: Serve reserves delivery
// identity, then executes and observes it through this adapter. The adapter holds
// no delivery state of its own; it executes or observes the exact original
// command through the configured TKE execution adapter and reports only what the
// provider actually observed. Persisting a delivery fact stays with the delivery
// step that owns it, so this bridge cannot become a second writer of Serve's own
// runtime records.

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
)

// ReasonRuntimeAdapterCapabilityUnavailable names an execution-adapter capability
// this Serve slice does not implement. It is never reported as a success.
const ReasonRuntimeAdapterCapabilityUnavailable = "runtime_adapter_capability_unavailable"

// StartRuntime executes the already reserved deployment through the execution
// adapter. A successful start acknowledges the request; it never claims readiness,
// which only ObserveRuntime may report.
func (s *Service) StartRuntime(ctx context.Context, command *api.RuntimeDeployCommand) (*api.RuntimeReadback, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	observation, err := s.executeRuntimeStep(ctx, command, true)
	if err != nil {
		return nil, err
	}
	return runtimeObservationReadback(command, observation), nil
}

// ObserveRuntime reports the execution adapter's own whole-runtime observation for
// the exact original command. It never derives readiness from a persisted pointer.
func (s *Service) ObserveRuntime(ctx context.Context, request *api.RuntimeReadbackRequest) (*api.RuntimeReadback, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	return s.runtimeReadback(ctx, request.GetRuntimeInstanceId(), request.GetDeploymentId())
}

// StopRuntime retires the exact original runtime through the installation's
// lifecycle boundary. It resolves the persisted original command from Serve's own
// store, so a lost caller response retires the same runtime rather than a new one.
func (s *Service) StopRuntime(ctx context.Context, command *api.RuntimeStopCommand) (*api.Operation, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	if s.Runtime == nil {
		return nil, status.Error(codes.Unavailable, "runtime execution adapter is not configured")
	}
	runtimeID := strings.TrimSpace(command.GetRuntimeInstanceId())
	if runtimeID == "" {
		return nil, status.Error(codes.InvalidArgument, "runtime instance is required")
	}
	deploy, err := s.persistedRuntimeCommand(ctx, runtimeID, command.GetDeploymentId())
	if err != nil {
		return nil, err
	}
	binding, err := s.confirmedRuntimeBinding(ctx, deploy.command)
	if err != nil {
		return nil, err
	}
	if err := s.Runtime.Lifecycle(ctx, deploy.command, binding, "suspended"); err != nil {
		return nil, err
	}
	return s.runtimeLifecycleOperation(ctx, deploy, "runtime_stop", "suspending"), nil
}

// ReloadRuntime applies a new model configuration to the exact persisted runtime.
// The applied version is only recorded after the installation confirms it, so a
// failed reload never advances applied_model_configuration_version.
func (s *Service) ReloadRuntime(ctx context.Context, command *api.RuntimeReloadCommand) (*api.Operation, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	if s.Runtime == nil {
		return nil, status.Error(codes.Unavailable, "runtime execution adapter is not configured")
	}
	runtimeID := strings.TrimSpace(command.GetRuntimeInstanceId())
	if runtimeID == "" || command.GetTargetVersion() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "runtime instance and a positive target version are required")
	}
	deploy, err := s.persistedRuntimeCommand(ctx, runtimeID, "")
	if err != nil {
		return nil, err
	}
	if current := deploy.appliedModelVersion; current != command.GetExpectedAppliedVersion() {
		return nil, status.Errorf(codes.FailedPrecondition, "runtime applied model configuration is %d, not the expected version", current)
	}
	reload := proto.Clone(deploy.command).(*api.RuntimeDeployCommand)
	reload.ModelConfigurationVersion = command.GetTargetVersion()
	reload.ModelSelections = command.GetSelections()
	binding, err := s.confirmedRuntimeBinding(ctx, reload)
	if err != nil {
		return nil, err
	}
	if err := s.Runtime.Reload(ctx, reload, binding); err != nil {
		return nil, err
	}
	// The requested version is NOT the applied version until the runtime confirms
	// it. Until a provider readback carries the applied model configuration, the
	// operation stays awaiting confirmation and the stored applied version is
	// unchanged, so a client can never read a desired version as applied.
	return s.runtimeLifecycleOperation(ctx, deploy, "runtime_reload", "awaiting_confirmation"), nil
}

// ReadApplicationCredentials returns the platform-issued WebUI credential for the
// exact persisted runtime through the installation's credential boundary. The
// value is never persisted, logged or written to an event.
func (s *Service) ReadApplicationCredentials(ctx context.Context, request *api.ReadApplicationCredentialsRequest) (*api.WorkspaceApplicationCredentials, error) {
	if err := requireServePeer(ctx); err != nil {
		return nil, err
	}
	if s.Runtime == nil {
		return nil, status.Error(codes.Unavailable, "runtime execution adapter is not configured")
	}
	runtimeID := strings.TrimSpace(request.GetRuntimeInstanceId())
	if runtimeID == "" {
		return nil, status.Error(codes.InvalidArgument, "runtime instance is required")
	}
	deploy, err := s.persistedRuntimeCommand(ctx, runtimeID, request.GetDeploymentId())
	if err != nil {
		return nil, err
	}
	binding, err := s.confirmedRuntimeBinding(ctx, deploy.command)
	if err != nil {
		return nil, err
	}
	credentials, err := s.Runtime.Credentials(ctx, deploy.command, binding)
	if err != nil {
		return nil, err
	}
	// The runtime's own credential for the same workspace is the only value served;
	// a mismatch is refused rather than returned.
	if credentials.GetRuntimeInstanceId() != runtimeID || credentials.GetWorkspaceId() != deploy.command.GetWorkspaceId() {
		return nil, status.Error(codes.FailedPrecondition, "runtime credentials do not match the exact runtime")
	}
	return credentials, nil
}

// executeRuntimeStep resolves the exact executable resource binding through the
// Fabric readback and executes or observes the original command. A binding that
// Fabric has not confirmed stops the step before any provider call.
func (s *Service) executeRuntimeStep(ctx context.Context, command *api.RuntimeDeployCommand, start bool) (RuntimeObservation, error) {
	if command == nil || s.Runtime == nil || s.Resources == nil {
		return RuntimeObservation{}, status.Error(codes.Unavailable, "runtime adapter and Fabric readback must be configured")
	}
	if command.GetWorkspaceId() == "" || command.GetResourceSetId() == "" || command.GetDataAttachmentId() == "" {
		return RuntimeObservation{}, status.Error(codes.InvalidArgument, "workspace, resource set and data attachment are required")
	}
	resources, err := s.Resources.ReadResources(ctx, &api.ResourceReadbackRequest{Context: nextOwnerCall(command.GetContext()), ResourceSetId: command.GetResourceSetId()})
	if err != nil {
		return RuntimeObservation{}, err
	}
	binding, err := confirmedBinding(command, resources)
	if err != nil {
		return RuntimeObservation{}, err
	}
	if start {
		if _, err = s.Runtime.Start(ctx, command, binding); err != nil {
			return RuntimeObservation{}, err
		}
	}
	return s.Runtime.Observe(ctx, command, binding)
}

// runtimeObservationReadback projects one adapter observation into the contract's
// readback shape without persisting it: the caller observes, the delivery step
// records. Readiness is reported only for an observation that is truly ready.
func runtimeObservationReadback(command *api.RuntimeDeployCommand, observation RuntimeObservation) *api.RuntimeReadback {
	readback := &api.RuntimeReadback{
		RuntimeInstanceId: command.GetRuntimeInstanceId(), WorkspaceId: command.GetWorkspaceId(), DeploymentId: command.GetDeploymentId(),
		State: observation.State, Artifact: command.GetDeploymentDescriptor().GetArtifact(),
		AppliedModelConfigurationVersion: observation.AppliedModelConfigurationVersion,
		DeploymentDescriptorDigest:       command.GetDeploymentDescriptorDigest(), ExecutionEpoch: command.GetExecutionEpoch(),
		DeploymentDescriptorObjectRef: command.GetDeploymentDescriptorObjectRef(), Outcome: api.Observation_OBSERVATION_UNKNOWN,
	}
	if !observation.ObservedAt.IsZero() {
		readback.ObservedAt = timestamppb.New(observation.ObservedAt.UTC())
		readback.Outcome = api.Observation_OBSERVATION_CONFIRMED
	}
	if observation.ApplicationEntry != nil {
		entry := &api.WorkspaceApplicationEntry{}
		if observation.ApplicationEntry.ServiceName != "" {
			entry.ServiceName = proto.String(observation.ApplicationEntry.ServiceName)
		}
		if observation.ApplicationEntry.URL != "" {
			entry.Url = proto.String(observation.ApplicationEntry.URL)
		}
		if observation.ApplicationEntry.Port != 0 {
			entry.Port = proto.Int32(int32(observation.ApplicationEntry.Port))
		}
		readback.ApplicationEntry = entry
	}
	if observation.AccessURL != "" {
		readback.AccessUrl = observation.AccessURL
	}
	if observation.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY && observation.AccessURL != "" {
		readback.ProcessReady = true
		readback.ApplicationAvailable = true
		readback.ReadinessReceiptId = observation.ReadinessEvidenceRef
	}
	return readback
}
