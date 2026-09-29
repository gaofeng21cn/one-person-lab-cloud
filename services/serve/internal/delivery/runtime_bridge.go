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

// StopRuntime, ReloadRuntime and ReadApplicationCredentials are part of the same
// contract but are not implemented in this slice: the installation boundary this
// adapter reaches exposes start and readback only. Each refuses with a named,
// actionable reason instead of returning an empty success.
func (s *Service) StopRuntime(context.Context, *api.RuntimeStopCommand) (*api.Operation, error) {
	return nil, status.Errorf(codes.Unimplemented, "%s: StopRuntime has no installation execution path in this slice", ReasonRuntimeAdapterCapabilityUnavailable)
}

func (s *Service) ReloadRuntime(context.Context, *api.RuntimeReloadCommand) (*api.Operation, error) {
	return nil, status.Errorf(codes.Unimplemented, "%s: ReloadRuntime has no installation execution path in this slice", ReasonRuntimeAdapterCapabilityUnavailable)
}

func (s *Service) ReadApplicationCredentials(context.Context, *api.ReadApplicationCredentialsRequest) (*api.WorkspaceApplicationCredentials, error) {
	return nil, status.Errorf(codes.Unimplemented, "%s: ReadApplicationCredentials has no installation credential path in this slice", ReasonRuntimeAdapterCapabilityUnavailable)
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
		AppliedModelConfigurationVersion: command.GetModelConfigurationVersion(),
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
