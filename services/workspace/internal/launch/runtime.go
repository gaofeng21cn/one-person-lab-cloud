package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
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
	binding, err := s.runtimeBinding(op, accepted, resources, result, commit)
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
	// A frozen revision that declares the installation Gateway credential is
	// delivered its Workspace-managed key handover here. Workspace issues the opaque
	// key binding through Gateway and freezes that handover on the deploy command;
	// Serve alone asks Fabric to bind the Secret into the runtime (F08) and confirms
	// the resulting Fabric binding in its own frozen command, so the Workspace never
	// presents a Fabric-confirmed binding and never binds into Fabric directly.
	if err = s.ensureRuntimeGatewayBinding(ctx, op, result, command); err != nil {
		if errors.Is(err, errRuntimeAwaitingKeyOwner) {
			return nil // The order stays awaiting its key owner rather than deploying without a credential.
		}
		return s.failedCall(ctx, op, token, "deploy_runtime", "runtime", *result, err)
	}
	recovering := len(result.RuntimeCommand) > 0
	if recovering {
		stored := &api.RuntimeDeployCommand{}
		if protojson.Unmarshal(result.RuntimeCommand, stored) != nil || !proto.Equal(stored, command) {
			return s.failedCall(ctx, op, token, "deploy_runtime", "runtime", *result, status.Error(codes.DataLoss, "runtime command differs from its original execution"))
		}
		// A previous command may already have reached Serve. Read its original
		// instance before deciding whether a replay of Start is necessary.
		observed, err := s.readRuntime(ctx, op, token, accepted, command, result)
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
	_, err = s.readRuntime(ctx, op, token, accepted, command, result)
	return err
}

// errRuntimeAwaitingKeyOwner reports that a runtime whose revision declares the
// installation Gateway credential reached the launch before the Gateway owner was
// configured. The order waits rather than deploying without a key.
var errRuntimeAwaitingKeyOwner = errors.New("runtime gateway key owner is not configured")

// commandRevision decodes the immutable application revision a deploy command was
// frozen against, rejecting an unreadable or invalid one.
func commandRevision(command *api.RuntimeDeployCommand) (contracts.WorkspaceApplicationRevision, error) {
	var zero contracts.WorkspaceApplicationRevision
	if command.GetDeploymentDescriptor() == nil {
		return zero, status.Error(codes.DataLoss, "runtime command carries no deployment descriptor")
	}
	// A descriptor with no application revision (a legacy build) declares no
	// installation Gateway credential, so it needs no managed key binding.
	if command.GetDeploymentDescriptor().GetApplicationRevision() == nil {
		return zero, nil
	}
	raw, err := publicjson.Marshal(command.GetDeploymentDescriptor().GetApplicationRevision())
	if err != nil {
		return zero, status.Error(codes.DataLoss, "runtime application revision cannot be encoded")
	}
	var revision contracts.WorkspaceApplicationRevision
	if json.Unmarshal(raw, &revision) != nil || contracts.ValidateWorkspaceApplicationRevision(revision) != nil {
		return zero, status.Error(codes.DataLoss, "runtime application revision is invalid")
	}
	return revision, nil
}

// ensureRuntimeGatewayBinding resolves the Workspace-managed Gateway key handover a
// launch command must carry. A revision that declares no Gateway credential needs
// none. For one that does, a recovered command reuses the handover it recorded as
// one durable fact, and a first attempt issues the key through Gateway and records
// the opaque handover - key binding, fingerprint, Secret delivery reference and
// publisher slot - on the command. The Fabric-confirmed binding identity is never
// part of it: Serve alone asks Fabric to bind the Secret and completes its own
// frozen command with what Fabric confirmed. The handover is frozen before dispatch
// so a restart replays the same key identity instead of minting a second one.
func (s *Service) ensureRuntimeGatewayBinding(ctx context.Context, op ownerstore.Operation, result *orderResult, command *api.RuntimeDeployCommand) error {
	revision, err := commandRevision(command)
	if err != nil {
		return err
	}
	credential, declared := contracts.WorkspaceApplicationDeclaredCredential(revision, contracts.WorkspaceApplicationCredentialGatewayKey)
	if !declared {
		return nil
	}
	if len(result.ManagedKeyBinding) > 0 {
		binding := &api.RuntimeManagedKeyBinding{}
		if protojson.Unmarshal(result.ManagedKeyBinding, binding) != nil {
			return status.Error(codes.DataLoss, "stored launch managed key binding is invalid")
		}
		// Under the Serve handover contract the Fabric-confirmed binding identity is
		// never the caller's. A stored handover that already carries one is not a
		// handover this launch may present again.
		if strings.TrimSpace(binding.GetSecretBindingId()) != "" || strings.TrimSpace(binding.GetSecretVersion()) != "" {
			return status.Error(codes.DataLoss, "stored launch managed key binding carries a Fabric-confirmed Secret binding")
		}
		command.ManagedKeyBinding = binding
		return nil
	}
	if s.Gateway == nil {
		return errRuntimeAwaitingKeyOwner
	}
	modelIDs := make([]string, 0, len(command.GetModelSelections()))
	for _, selection := range command.GetModelSelections() {
		if id := strings.TrimSpace(selection.GetModelId()); id != "" {
			modelIDs = append(modelIDs, id)
		}
	}
	if len(modelIDs) == 0 {
		return status.Error(codes.FailedPrecondition, "the frozen revision declares a Gateway credential but names no model")
	}
	key, err := s.Gateway.CreateManagedKey(ctx, &api.ManagedKeyCommand{Context: continuation(op, result.GrantID, "create_managed_key"), WorkspaceId: op.ResourceID, ModelIds: modelIDs, TargetRuntimeInstanceId: command.GetRuntimeInstanceId()})
	if err != nil {
		return err
	}
	if strings.TrimSpace(key.GetKeyBindingId()) == "" || strings.TrimSpace(key.GetSecretDeliveryReference()) == "" || strings.TrimSpace(key.GetFingerprint()) == "" ||
		key.GetWorkspaceId() != op.ResourceID || key.GetTargetRuntimeInstanceId() != command.GetRuntimeInstanceId() || key.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(op.ResourceID) {
		return status.Error(codes.DataLoss, "Gateway returned a managed key that differs from the accepted launch")
	}
	command.ManagedKeyBinding = &api.RuntimeManagedKeyBinding{KeyBindingId: key.GetKeyBindingId(), SecretDeliveryReference: key.GetSecretDeliveryReference(), Fingerprint: key.GetFingerprint(), TargetSlot: credential.Name}
	result.ManagedKeyBinding = wire(command.ManagedKeyBinding)
	return nil
}

func (s *Service) runtimeBinding(op ownerstore.Operation, accepted *api.QuoteAcceptance, resources *api.ResourceReadback, result *orderResult, commit *api.OwnerCommitEvidence) (*api.ResourceExecutionBinding, error) {
	// The gate is the order's own funding proof, not its provider or billing mode:
	// a Local no-charge order is released by its Ledger zero-charge receipt, while a
	// paid order is released only by the Ledger WALLET_ACTION receipt for its
	// confirmed Gateway charge. Either way the proof must name this exact obligation,
	// so a paid path can never be delivered on Local zero-charge evidence (or the
	// reverse) merely because its resources exist.
	if _, _, err := s.fundingProofFor(op, accepted, result, commit); err != nil {
		return nil, err
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

func (s *Service) readRuntime(ctx context.Context, op ownerstore.Operation, token string, accepted *api.QuoteAcceptance, command *api.RuntimeDeployCommand, result *orderResult) (*api.RuntimeReadback, error) {
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
	ready := observed.GetOutcome() == api.Observation_OBSERVATION_CONFIRMED && observed.GetState() == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY
	stage, state, observation := "runtime", "awaiting_confirmation", "unknown"
	if observed.Outcome == api.Observation_OBSERVATION_REJECTED || observed.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED {
		state, observation = "needs_attention", "rejected"
	} else if observed.Outcome == api.Observation_OBSERVATION_CONFIRMED {
		observation = "confirmed"
		if ready {
			stage = "activation"
		}
	}
	// Even Serve-ready does not prove Gateway, period or entitlement activation,
	// so the confirmed readiness is recorded at the activation boundary before the
	// entitlement is written. For a paid order the order's own confirmed charge
	// receipt then releases the one quoted subscription and its first immutable
	// period, the Workspace becomes active and the operation reaches its terminal
	// confirmed state. A Local no-charge order deliberately keeps the activation
	// boundary here: its zero-fee period is a separate product decision and is not
	// fabricated from paid evidence.
	if err = s.checkpoint(ctx, op, token, "read_runtime", stage, state, observation, command.RuntimeInstanceId, "DEPENDENCY_UNAVAILABLE", *result); err != nil {
		return nil, err
	}
	if ready && !localNoCharge(accepted) {
		if err = s.activatePaidOrder(ctx, op, token, accepted, result, observed); err != nil {
			return nil, s.failedCall(ctx, op, token, "read_runtime", "activation", *result, err)
		}
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
