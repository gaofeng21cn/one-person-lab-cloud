package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerstore"
)

// resumeRuntime delivers the accepted order whose resources are confirmed. The
// gate is the resource readback, not the order's provider or billing mode, so a
// paid order whose resources actually exist is delivered the same way and an
// order whose funding or resources are unresolved never reaches Serve. Workspace
// retains coordination evidence; Serve remains the deployment/readiness owner.
func (s *Service) resumeRuntime(ctx context.Context, op ownerstore.Operation, token string, accepted *api.QuoteAcceptance, resources *api.ResourceReadback, result *orderResult) error {
	if s.Serve == nil {
		return nil // The persisted order remains awaiting its runtime dependencies.
	}
	commit, err := evidence(op)
	if err != nil {
		return err
	}
	binding, err := runtimeBinding(op, accepted, resources, result, commit)
	if err != nil {
		return s.failedCall(ctx, op, token, "read_resources", "runtime", *result, err)
	}
	source := &applicationSource{}
	if len(result.ApplicationSource) == 0 {
		source, err = s.resolveApplicationSource(ctx, op, result.GrantID, accepted)
		if err != nil {
			return s.failedCall(ctx, op, token, "runtime_capability", "runtime", *result, err)
		}
		record, recErr := source.record()
		if recErr != nil {
			return s.failedCall(ctx, op, token, "runtime_capability", "runtime", *result, recErr)
		}
		result.ApplicationSource, result.RuntimeBinding = record, wire(binding)
		if err = s.checkpoint(ctx, op, token, "runtime_capability", "runtime", "running", "confirmed", source.identity(), "", *result); err != nil {
			return err
		}
	} else {
		record := &sourceRecord{}
		if json.Unmarshal(result.ApplicationSource, record) != nil {
			return status.Error(codes.DataLoss, "stored application source is invalid")
		}
		source, err = record.resolve()
		if err != nil {
			return err
		}
		if err = s.revalidateApplicationSource(accepted, source); err != nil {
			return err
		}
	}
	reserve := &api.RuntimeReservationCommand{Context: continuation(op, result.GrantID, "reserve_runtime"), WorkspaceId: op.ResourceID, ApplicationSelection: source.Selection, Artifact: source.Artifact, DeploymentDescriptor: source.DeploymentDescriptor, DeploymentDescriptorDigest: source.DescriptorDigest, DeploymentDescriptorObjectRef: source.DescriptorObjectRef, ResourceSetId: resources.ResourceSetId, DataAttachmentId: binding.DataAttachmentId}
	reservation := &api.RuntimeReservation{}
	if len(result.RuntimeReservation) == 0 {
		if err = s.beginStep(ctx, op, token, "reserve_runtime", 6, "serve", "runtime", reserve); err != nil {
			return err
		}
		reservation, err = s.Serve.Reserve(ctx, reserve)
		if err != nil {
			return s.failedCall(ctx, op, token, "reserve_runtime", "runtime", *result, err)
		}
		if err = validateRuntimeReservation(reserve, reservation); err != nil {
			return s.failedCall(ctx, op, token, "reserve_runtime", "runtime", *result, err)
		}
		result.RuntimeReservation = wire(reservation)
		if err = s.checkpoint(ctx, op, token, "reserve_runtime", "runtime", "running", "confirmed", reservation.OperationId, "", *result); err != nil {
			return err
		}
	} else if protojson.Unmarshal(result.RuntimeReservation, reservation) != nil {
		return status.Error(codes.DataLoss, "stored runtime reservation is invalid")
	}
	if err = validateRuntimeReservation(reserve, reservation); err != nil {
		return err
	}
	command := runtimeCommand(op, result.GrantID, accepted, source, binding, resources.ResourceSetId, reservation)
	recovering := len(result.RuntimeCommand) > 0
	if recovering {
		stored := &api.RuntimeDeployCommand{}
		if protojson.Unmarshal(result.RuntimeCommand, stored) != nil || !proto.Equal(stored, command) {
			return s.failedCall(ctx, op, token, "deploy_runtime", "runtime", *result, status.Error(codes.DataLoss, "runtime command differs from its original execution"))
		}
		// A previous command may already have reached Serve. Read its original
		// instance before deciding whether a replay of Start is necessary.
		observed, err := s.readRuntime(ctx, op, token, command, result)
		if err != nil {
			if result.RuntimeDeployAccepted || ctx.Err() != nil || !runtimeReadCanRetryStart(err) {
				return err
			}
			// Serve may have persisted Start before its adapter was reached. Its
			// original command is idempotent, so an unavailable observation may
			// resume that same Start instead of leaving a never-started runtime.
		} else if result.RuntimeDeployAccepted || !runtimeNeedsStart(observed) {
			return nil
		}
	} else {
		result.RuntimeCommand = wire(command)
		// Freeze the full command before dispatch. No new deployment or epoch is
		// generated when a worker dies after Serve accepts it.
		if err = s.checkpoint(ctx, op, token, "reserve_runtime", "runtime", "running", "confirmed", reservation.OperationId, "", *result); err != nil {
			return err
		}
	}
	if err = s.beginStep(ctx, op, token, "deploy_runtime", 7, "serve", "runtime", command); err != nil {
		return err
	}
	observed, err := s.Serve.Deploy(ctx, command)
	if err != nil {
		return s.failedCall(ctx, op, token, "deploy_runtime", "runtime", *result, err)
	}
	if err = validateRuntimeReadback(command, observed); err != nil {
		return s.failedCall(ctx, op, token, "deploy_runtime", "runtime", *result, err)
	}
	result.RuntimeDeployAccepted = true
	result.RuntimeReadback = wire(observed)
	if err = s.checkpoint(ctx, op, token, "deploy_runtime", "runtime", "awaiting_confirmation", "confirmed", reservation.OperationId, "", *result); err != nil {
		return err
	}
	_, err = s.readRuntime(ctx, op, token, command, result)
	return err
}

func runtimeBinding(op ownerstore.Operation, accepted *api.QuoteAcceptance, resources *api.ResourceReadback, result *orderResult, commit *api.OwnerCommitEvidence) (*api.ResourceExecutionBinding, error) {
	zero := &api.LocalNoChargeReceiptEvidence{}
	if protojson.Unmarshal(result.ZeroChargeReceipt, zero) != nil || validateZeroChargeEvidence(op, accepted, commit, zero, nil) != nil {
		return nil, status.Error(codes.FailedPrecondition, "verified Local no-charge evidence is required before runtime")
	}
	b := resources.GetExecutionResources()
	if resources.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || resources.GetAbsenceConfirmed() || resources.GetWorkspaceId() != op.ResourceID || resources.GetResourceSetId() == "" || resources.GetResourceSetId() != result.ResourceSetID || b.GetAccountId() == "" || b.GetComputeAllocationId() == "" || b.GetStorageVolumeId() == "" || b.GetDataAttachmentId() == "" || b.GetDataAttachmentOperationId() == "" {
		return nil, status.Error(codes.FailedPrecondition, "Fabric has not confirmed the original executable resource binding")
	}
	if len(result.RuntimeBinding) > 0 {
		stored := &api.ResourceExecutionBinding{}
		if protojson.Unmarshal(result.RuntimeBinding, stored) != nil || !proto.Equal(stored, b) {
			return nil, status.Error(codes.DataLoss, "Fabric runtime resources differ from the original execution binding")
		}
	}
	return b, nil
}

// revalidateApplicationSource binds a recovered application source to the accepted
// quote: the recovered CapabilityVersion or Runtime Release must still be the one
// the accepted quote named, so a recovery cannot silently switch sources.
func (s *Service) revalidateApplicationSource(accepted *api.QuoteAcceptance, source *applicationSource) error {
	if accepted.GetQuote().GetCapabilityVersionId() != source.CapabilityVersionID || accepted.GetQuote().GetRuntimeVersionId() != source.RuntimeVersionID {
		return status.Error(codes.DataLoss, "stored application source differs from its accepted quote")
	}
	return nil
}

func validateRuntimeVersion(accepted *api.QuoteAcceptance, version *api.CapabilityVersion) error {
	descriptor, artifact := version.GetDeploymentDescriptor(), version.GetArtifact()
	if version.GetId() == "" || version.GetId() != accepted.GetQuote().GetCapabilityVersionId() || version.GetStatus() != api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY || descriptor == nil || artifact.GetRepository() == "" || artifact.GetDigest() == "" || artifact.GetDigest() != version.GetArtifactDigest() || !proto.Equal(artifact, descriptor.GetArtifact()) || version.GetDeploymentDescriptorObjectRef() == "" {
		return status.Error(codes.FailedPrecondition, "Capability has not confirmed the accepted deployable version")
	}
	raw, err := publicjson.Marshal(descriptor)
	if err != nil {
		return status.Error(codes.DataLoss, "Capability descriptor cannot be encoded")
	}
	digest := sha256.Sum256(raw)
	if version.DeploymentDescriptorDigest != "sha256:"+hex.EncodeToString(digest[:]) {
		return status.Error(codes.DataLoss, "Capability descriptor does not match its digest")
	}
	return nil
}

func validateRuntimeReservation(request *api.RuntimeReservationCommand, r *api.RuntimeReservation) error {
	if r.GetWorkspaceId() != request.WorkspaceId || r.GetDeploymentId() == "" || r.GetRuntimeInstanceId() == "" || r.GetOperationId() == "" || r.GetExecutionEpoch() < 1 || !proto.Equal(r.GetArtifact(), request.Artifact) || r.GetDeploymentDescriptorDigest() != request.DeploymentDescriptorDigest || r.GetDeploymentDescriptorObjectRef() != request.DeploymentDescriptorObjectRef {
		return status.Error(codes.DataLoss, "Serve reservation differs from the original Workspace deployment")
	}
	return nil
}

func runtimeCommand(op ownerstore.Operation, grant string, accepted *api.QuoteAcceptance, source *applicationSource, binding *api.ResourceExecutionBinding, resourceSetID string, reservation *api.RuntimeReservation) *api.RuntimeDeployCommand {
	return &api.RuntimeDeployCommand{Context: continuation(op, grant, "deploy_runtime"), WorkspaceId: op.ResourceID, DeploymentId: reservation.DeploymentId, RuntimeInstanceId: reservation.RuntimeInstanceId, ApplicationSelection: source.Selection, DeploymentDescriptor: source.DeploymentDescriptor, DeploymentDescriptorDigest: source.DescriptorDigest, DeploymentDescriptorObjectRef: source.DescriptorObjectRef, ResourceSetId: resourceSetID, DataAttachmentId: binding.DataAttachmentId, DataCompatibility: source.DataCompatibility, ModelSelections: accepted.Quote.ModelSelections, ExecutionEpoch: reservation.ExecutionEpoch}
}

func (s *Service) readRuntime(ctx context.Context, op ownerstore.Operation, token string, command *api.RuntimeDeployCommand, result *orderResult) (*api.RuntimeReadback, error) {
	request := &api.RuntimeReadbackRequest{Context: continuation(op, result.GrantID, "read_runtime"), RuntimeInstanceId: command.RuntimeInstanceId, DeploymentId: command.DeploymentId}
	if err := s.beginStep(ctx, op, token, "read_runtime", 8, "serve", "runtime", request); err != nil {
		return nil, err
	}
	observed, err := s.Serve.ReadRuntime(ctx, request)
	if err != nil {
		return nil, s.failedCall(ctx, op, token, "read_runtime", "runtime", *result, err)
	}
	if err = validateRuntimeReadback(command, observed); err != nil {
		return nil, s.failedCall(ctx, op, token, "read_runtime", "runtime", *result, err)
	}
	result.RuntimeReadback = wire(observed)
	stage, state, observation := "runtime", "awaiting_confirmation", "unknown"
	if observed.Outcome == api.Observation_OBSERVATION_REJECTED || observed.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED {
		state, observation = "needs_attention", "rejected"
	} else if observed.Outcome == api.Observation_OBSERVATION_CONFIRMED {
		observation = "confirmed"
		if observed.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
			stage = "activation"
		}
	}
	// Even Serve-ready does not prove Gateway, period or entitlement activation.
	if err = s.checkpoint(ctx, op, token, "read_runtime", stage, state, observation, command.RuntimeInstanceId, "DEPENDENCY_UNAVAILABLE", *result); err != nil {
		return nil, err
	}
	return observed, nil
}

func runtimeNeedsStart(observed *api.RuntimeReadback) bool {
	return observed.GetOutcome() == api.Observation_OBSERVATION_UNKNOWN && observed.GetState() == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING
}

func runtimeReadCanRetryStart(err error) bool {
	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded, codes.NotFound:
		return true
	default:
		return false
	}
}

func validateRuntimeReadback(command *api.RuntimeDeployCommand, r *api.RuntimeReadback) error {
	artifact := command.GetDeploymentDescriptor().GetArtifact()
	if r.GetWorkspaceId() != command.WorkspaceId || r.GetRuntimeInstanceId() != command.RuntimeInstanceId || r.GetDeploymentId() != command.DeploymentId || r.GetExecutionEpoch() != command.ExecutionEpoch || r.GetDeploymentDescriptorDigest() != command.DeploymentDescriptorDigest || r.GetDeploymentDescriptorObjectRef() != command.DeploymentDescriptorObjectRef || !proto.Equal(r.GetArtifact(), artifact) || r.GetAppliedModelConfigurationVersion() != command.ModelConfigurationVersion {
		return status.Error(codes.DataLoss, "Serve readback differs from the original runtime execution")
	}
	if _, ok := api.AgentRuntimeObservationState_name[int32(r.GetState())]; !ok || r.GetState() == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_UNSPECIFIED {
		return status.Error(codes.DataLoss, "Serve returned an invalid runtime state")
	}
	if r.GetOutcome() != api.Observation_OBSERVATION_UNKNOWN && r.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED && r.GetOutcome() != api.Observation_OBSERVATION_REJECTED {
		return status.Error(codes.DataLoss, "Serve returned an invalid runtime observation")
	}
	if r.GetOutcome() == api.Observation_OBSERVATION_CONFIRMED && (r.GetObservedAt() == nil || r.ObservedAt.CheckValid() != nil) {
		return status.Error(codes.DataLoss, "Serve confirmation has no valid observation time")
	}
	if r.GetState() == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY && (r.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || !r.GetProcessReady() || !r.GetApplicationAvailable() || r.GetReadinessReceiptId() == "") {
		return status.Error(codes.DataLoss, "Serve readiness lacks confirmed application evidence")
	}
	return nil
}
