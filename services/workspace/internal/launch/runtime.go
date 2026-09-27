package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerstore"
)

// resumeRuntime delivers only the accepted Local no-charge order. Workspace
// retains coordination evidence; Serve remains the deployment/readiness owner.
func (s *Service) resumeRuntime(ctx context.Context, op ownerstore.Operation, token string, accepted *api.QuoteAcceptance, resources *api.ResourceReadback, result *orderResult) error {
	if s.Capability == nil || s.Serve == nil {
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
	version := &api.CapabilityVersion{}
	if len(result.RuntimeCapability) == 0 {
		request := &api.GetCapabilityVersionRpcRequest{Context: continuation(op, result.GrantID, "runtime_capability"), CapabilityVersionId: accepted.Quote.GetCapabilityVersionId()}
		if err = s.beginStep(ctx, op, token, "runtime_capability", 5, "capability", "runtime", request); err != nil {
			return err
		}
		version, err = s.Capability.GetCapabilityVersion(ctx, request)
		if err != nil {
			return s.failedCall(ctx, op, token, "runtime_capability", "runtime", *result, err)
		}
		if err = validateRuntimeVersion(accepted, version); err != nil {
			return s.failedCall(ctx, op, token, "runtime_capability", "runtime", *result, err)
		}
		result.RuntimeCapability, result.RuntimeBinding = wire(version), wire(binding)
		if err = s.checkpoint(ctx, op, token, "runtime_capability", "runtime", "running", "confirmed", version.Id, "", *result); err != nil {
			return err
		}
	} else {
		if protojson.Unmarshal(result.RuntimeCapability, version) != nil {
			return status.Error(codes.DataLoss, "stored runtime capability is invalid")
		}
		if err = validateRuntimeVersion(accepted, version); err != nil {
			return err
		}
	}
	reserve := &api.RuntimeReservationCommand{Context: continuation(op, result.GrantID, "reserve_runtime"), WorkspaceId: op.ResourceID, CapabilityVersionId: version.Id, Artifact: version.Artifact, DeploymentDescriptor: version.DeploymentDescriptor, DeploymentDescriptorDigest: version.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: version.DeploymentDescriptorObjectRef, ResourceSetId: resources.ResourceSetId, DataAttachmentId: binding.DataAttachmentId}
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
	command := runtimeCommand(op, result.GrantID, accepted, version, binding, resources.ResourceSetId, reservation)
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
			if observed.GetOutcome() == api.Observation_OBSERVATION_CONFIRMED && observed.GetState() == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
				return s.ensureDeploymentReceipt(ctx, op, token, accepted, resources, command, observed, result)
			}
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
	observed, err = s.readRuntime(ctx, op, token, command, result)
	if err != nil {
		return err
	}
	if observed.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || observed.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
		return nil
	}
	return s.ensureDeploymentReceipt(ctx, op, token, accepted, resources, command, observed, result)
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

func runtimeCommand(op ownerstore.Operation, grant string, accepted *api.QuoteAcceptance, version *api.CapabilityVersion, binding *api.ResourceExecutionBinding, resourceSetID string, reservation *api.RuntimeReservation) *api.RuntimeDeployCommand {
	return &api.RuntimeDeployCommand{Context: continuation(op, grant, "deploy_runtime"), WorkspaceId: op.ResourceID, DeploymentId: reservation.DeploymentId, RuntimeInstanceId: reservation.RuntimeInstanceId, CapabilityVersionId: version.Id, DeploymentDescriptor: version.DeploymentDescriptor, DeploymentDescriptorDigest: version.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: version.DeploymentDescriptorObjectRef, ResourceSetId: resourceSetID, DataAttachmentId: binding.DataAttachmentId, DataCompatibility: version.DataCompatibility, ModelSelections: accepted.Quote.ModelSelections, ExecutionEpoch: reservation.ExecutionEpoch, RuntimeConfiguration: runtimeConfiguration(version.DeploymentDescriptor, binding, op.ResourceID, reservation.RuntimeInstanceId, version.DeploymentDescriptorDigest, reservation.ExecutionEpoch)}
}

func (s *Service) readRuntime(ctx context.Context, op ownerstore.Operation, token string, command *api.RuntimeDeployCommand, result *orderResult) (*api.RuntimeReadback, error) {
	request := &api.RuntimeReadbackRequest{Context: continuation(op, result.GrantID, "read_runtime"), RuntimeInstanceId: command.RuntimeInstanceId, DeploymentId: command.DeploymentId}
	if err := s.beginStepAtEpoch(ctx, op, token, "read_runtime", 8, "serve", "runtime", request, command.GetExecutionEpoch()); err != nil {
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

const deploymentReceiptReferenceSuffix = ":deployment"

func deploymentReceiptReference(operationID string) string {
	return operationID + deploymentReceiptReferenceSuffix
}

func (s *Service) ensureDeploymentReceipt(ctx context.Context, op ownerstore.Operation, token string, accepted *api.QuoteAcceptance, resources *api.ResourceReadback, command *api.RuntimeDeployCommand, observed *api.RuntimeReadback, result *orderResult) error {
	if s.Ledger == nil {
		// Runtime readiness is not a Workspace terminal fact until the independent
		// deployment receipt has been appended and read back from Ledger.
		return nil
	}
	commit, err := evidence(op)
	if err != nil {
		return err
	}
	digest, err := deploymentEvidenceDigest(command, observed, resources)
	if err != nil {
		return err
	}
	request := &api.AppendReceiptRequest{
		Context: continuation(op, result.GrantID, "deployment_receipt"),
		Receipt: &api.Receipt{
			Kind:            api.ReceiptKindEnum_RECEIPT_KIND_ENUM_DEPLOYMENT,
			Owner:           api.OwnerEnum_OWNER_ENUM_WORKSPACE,
			OperationId:     proto.String(op.ID),
			ArtifactDigest:  proto.String(command.GetDeploymentDescriptor().GetArtifact().GetDigest()),
			Outcome:         api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED,
			EvidenceSummary: fmt.Sprintf("Workspace deployment %s ready at execution epoch %d.", command.GetDeploymentId(), command.GetExecutionEpoch()),
		},
		EvidenceDigest:         digest,
		OwnerEvidenceReference: deploymentReceiptReference(op.ID),
		QuoteAcceptance:        accepted,
		OwnerCommitEvidence:    commit,
	}
	if err = s.beginStepAtEpoch(ctx, op, token, "deployment_receipt_append", 9, "ledger", "receipt", request, command.GetExecutionEpoch()); err != nil {
		return err
	}
	appended, err := s.Ledger.AppendReceipt(ctx, request)
	if err != nil {
		return s.failedCall(ctx, op, token, "deployment_receipt_append", "receipt", *result, err)
	}
	if err = validateDeploymentReceipt(op, command, appended); err != nil {
		return s.failedCall(ctx, op, token, "deployment_receipt_append", "receipt", *result, err)
	}
	readRequest := &api.GetReceiptByReferenceRequest{Context: continuation(op, result.GrantID, "read_deployment_receipt"), Owner: "workspace", OwnerEvidenceReference: deploymentReceiptReference(op.ID)}
	if err = s.beginStepAtEpoch(ctx, op, token, "deployment_receipt_read", 10, "ledger", "receipt", readRequest, command.GetExecutionEpoch()); err != nil {
		return err
	}
	readback, err := s.Ledger.ReadReceiptByReference(ctx, readRequest)
	if err != nil {
		return s.failedCall(ctx, op, token, "deployment_receipt_read", "receipt", *result, err)
	}
	if err = validateDeploymentReceipt(op, command, readback); err != nil || !proto.Equal(appended, readback) {
		if err == nil {
			err = status.Error(codes.DataLoss, "Ledger deployment receipt readback differs from append result")
		}
		return s.failedCall(ctx, op, token, "deployment_receipt_read", "receipt", *result, err)
	}
	result.DeploymentReceipt = wire(readback)
	return s.checkpointOutcome(ctx, op, token, "deployment_receipt_read", "succeeded", "succeeded", "confirmed", "confirmed", readback.GetId(), "", "", *result)
}

func deploymentEvidenceDigest(command *api.RuntimeDeployCommand, observed *api.RuntimeReadback, resources *api.ResourceReadback) (string, error) {
	material, err := (proto.MarshalOptions{Deterministic: true}).Marshal(&api.RuntimeDeployCommand{
		WorkspaceId:                   command.GetWorkspaceId(),
		DeploymentId:                  command.GetDeploymentId(),
		RuntimeInstanceId:             command.GetRuntimeInstanceId(),
		CapabilityVersionId:           command.GetCapabilityVersionId(),
		DeploymentDescriptor:          command.GetDeploymentDescriptor(),
		ResourceSetId:                 command.GetResourceSetId(),
		DataAttachmentId:              command.GetDataAttachmentId(),
		ModelConfigurationVersion:     command.GetModelConfigurationVersion(),
		ModelSelections:               command.GetModelSelections(),
		DataCompatibility:             command.GetDataCompatibility(),
		DeploymentDescriptorDigest:    command.GetDeploymentDescriptorDigest(),
		ExecutionEpoch:                command.GetExecutionEpoch(),
		DeploymentDescriptorObjectRef: command.GetDeploymentDescriptorObjectRef(),
		RuntimeConfiguration:          command.GetRuntimeConfiguration(),
	})
	if err != nil {
		return "", status.Error(codes.Internal, "deployment command evidence cannot be encoded")
	}
	readback, err := (proto.MarshalOptions{Deterministic: true}).Marshal(observed)
	if err != nil {
		return "", status.Error(codes.Internal, "runtime readback evidence cannot be encoded")
	}
	resourceReadback, err := (proto.MarshalOptions{Deterministic: true}).Marshal(resources)
	if err != nil {
		return "", status.Error(codes.Internal, "Fabric readback evidence cannot be encoded")
	}
	combined := append(append(material, 0), readback...)
	combined = append(append(combined, 0), resourceReadback...)
	sum := sha256.Sum256(combined)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func validateDeploymentReceipt(op ownerstore.Operation, command *api.RuntimeDeployCommand, receipt *api.Receipt) error {
	if receipt == nil || receipt.GetId() == "" || receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_DEPLOYMENT || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || receipt.GetOperationId() != op.ID || receipt.GetArtifactDigest() != command.GetDeploymentDescriptor().GetArtifact().GetDigest() || receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetCreatedAt() == nil || receipt.GetCreatedAt().CheckValid() != nil {
		return status.Error(codes.DataLoss, "Ledger deployment receipt differs from the original runtime execution")
	}
	return nil
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

func runtimeConfiguration(descriptor *api.DeploymentDescriptor, binding *api.ResourceExecutionBinding, workspaceID, runtimeID, descriptorDigest string, epoch int64) *api.WorkspaceApplicationRuntimeConfiguration {
	if descriptor == nil || binding == nil || len(binding.GetInjectionHandles()) == 0 {
		return nil
	}
	configuration := &api.WorkspaceApplicationRuntimeConfiguration{}
	for _, handle := range binding.GetInjectionHandles() {
		if handle == nil || handle.GetWorkspaceId() != workspaceID || handle.GetRuntimeInstanceId() != runtimeID || handle.GetDeploymentDescriptorDigest() != descriptorDigest || handle.GetExecutionEpoch() != epoch {
			continue
		}
		slot := handle.GetTargetSlot()
		switch handle.GetKind() {
		case api.RuntimeInjectionHandle_SECRET:
			for _, input := range descriptor.GetApplicationRevision().GetSecretInputs() {
				if input.GetName() == slot {
					configuration.SecretBindings = append(configuration.SecretBindings, &api.RuntimeSecretBindingReference{InputName: input.GetName(), Target: input.GetTarget(), Env: input.GetEnv(), SecretBindingId: handle.GetHandleId(), Handle: handle, Fingerprint: handle.GetFingerprint()})
				}
			}
		case api.RuntimeInjectionHandle_CONFIG:
			for _, input := range descriptor.GetApplicationRevision().GetConfigInputs() {
				if input.GetName() == slot {
					configuration.ConfigBindings = append(configuration.ConfigBindings, &api.RuntimeConfigBinding{InputName: input.GetName(), Target: input.GetTarget(), Handle: handle})
				}
			}
		case api.RuntimeInjectionHandle_DATA_MOUNT, api.RuntimeInjectionHandle_SCRATCH_MOUNT:
			for _, input := range append(append([]*api.WorkspaceApplicationMount{}, descriptor.GetApplicationRevision().GetPersistentMounts()...), descriptor.GetApplicationRevision().GetScratchMounts()...) {
				if input.GetName() == slot {
					mode := "read_write"
					if input.GetReadOnly() {
						mode = "read_only"
					}
					configuration.MountBindings = append(configuration.MountBindings, &api.RuntimeMountBinding{MountName: input.GetName(), Target: input.GetMountPath(), AccessMode: mode, Handle: handle})
				}
			}
		}
	}
	return configuration
}
