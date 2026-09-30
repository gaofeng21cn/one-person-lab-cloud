package delivery

// Serve owns the Workspace's current application, so a version switch or a
// rollback is Serve's own replacement of it. Serve admits one new delivery at
// execution_epoch+1 that names the deployment it replaces, executes it against
// the same confirmed infrastructure, and only when the replacement instance is
// ready commits the route switch in the same owner transaction that supersedes
// the previous delivery. A replacement that never becomes ready leaves the
// current route and the previous deployment untouched, so a failed switch can
// never take the running application away.
//
// The replacement reuses the Workspace's already-confirmed resource set and data
// attachment: a version switch changes the application, not the purchased
// infrastructure. A switch that would need new resources belongs to the
// resource-owner workflow instead and is refused here.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerstore"
)

// Refusal reasons for a replacement. Each names a distinct, actionable condition.
const (
	// ReasonCurrentDeploymentConflict: the caller's expected current deployment is
	// not the Workspace's current one, so the switch would race another writer.
	ReasonCurrentDeploymentConflict = "current_deployment_conflict"
	// ReasonReplacementUnavailable: the Workspace has no current application, the
	// requested replacement source is not an admitted deployable version for it,
	// or the named rollback target was never a delivery of this Workspace.
	ReasonReplacementUnavailable = "replacement_source_unavailable"
	// ReasonDataCompatibilityUnproven: the replacement would run against data the
	// version does not declare it can carry forward, so Serve must not guess that
	// the data survives the change.
	ReasonDataCompatibilityUnproven = "data_compatibility_unproven"
	// ReasonDataRollbackUnsafe: the replacement (or the rollback out of the
	// running version) is not declared rollback-safe, so the change could not be
	// undone and is refused instead of executed.
	ReasonDataRollbackUnsafe = "data_rollback_not_safe"
)

// UpdateWorkspaceVersion replaces the Workspace's current application with the
// named ready CapabilityVersion. The caller must present the exact current
// deployment it read, so two switches can never both believe they replaced the
// same application.
func (s *Service) UpdateWorkspaceVersion(ctx context.Context, r *api.UpdateWorkspaceVersionRpcRequest) (*api.Operation, error) {
	workspaceID := strings.TrimSpace(r.GetWorkspaceId())
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION, workspaceID); err != nil {
		return nil, err
	}
	body := r.GetBody()
	if body == nil {
		return nil, status.Error(codes.InvalidArgument, "an update version body is required")
	}
	capabilityVersionID := strings.TrimSpace(body.GetCapabilityVersionId())
	expectedCurrent := strings.TrimSpace(body.GetExpectedCurrentAgentDeploymentId())
	if workspaceID == "" || capabilityVersionID == "" || expectedCurrent == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace, capability version and the expected current deployment are required")
	}
	if s.Runtime == nil || s.Resources == nil || s.References == nil || s.Capability == nil {
		return nil, status.Error(codes.Unavailable, "runtime adapter, Fabric readback, Capability and reference coordination must be configured")
	}
	call := r.GetContext()
	version, err := s.Capability.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: nextOwnerCall(call), CapabilityVersionId: capabilityVersionID})
	if err != nil {
		return nil, err
	}
	if version.GetId() != capabilityVersionID || version.GetStatus() != api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY || version.GetDeploymentDescriptor() == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "%s: Capability did not confirm the requested version as deployable", ReasonReplacementUnavailable)
	}
	compatibility, err := compatibilityJSON(version.GetDataCompatibility())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "cannot record the admitted data contract: %v", err)
	}
	return s.runReplacement(ctx, replacementPlan{
		kind:                "update_workspace",
		call:                call,
		workspaceID:         workspaceID,
		expectedCurrent:     expectedCurrent,
		capabilityVersionID: capabilityVersionID,
		descriptor:          version.GetDeploymentDescriptor(),
		descriptorDigest:    version.GetDeploymentDescriptorDigest(),
		descriptorRef:       version.GetDeploymentDescriptorObjectRef(),
		compatibility:       compatibility,
	})
}

// compatibilityJSON is the recorded data contract of one admitted delivery. A
// version that declares no compatibility is recorded as the empty object, exactly
// as the first-reservation path records it, so the column stays non-null and no
// compatibility claim is invented. A declaration that cannot be serialized is an
// error rather than an empty contract.
func compatibilityJSON(m *api.DataCompatibility) ([]byte, error) {
	if m == nil {
		return []byte(`{}`), nil
	}
	raw, err := publicjson.Marshal(m)
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return []byte(`{}`), nil
	}
	return raw, nil
}

// RollbackWorkspace restores a deployment the Workspace previously ran. It
// re-executes that deployment's own stored descriptor, so the restored
// application is exactly the admitted revision, and it refuses a target that was
// never a delivery of this Workspace.
func (s *Service) RollbackWorkspace(ctx context.Context, r *api.RollbackWorkspaceRpcRequest) (*api.Operation, error) {
	workspaceID := strings.TrimSpace(r.GetWorkspaceId())
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ROLLBACKWORKSPACE, workspaceID); err != nil {
		return nil, err
	}
	body := r.GetBody()
	if body == nil {
		return nil, status.Error(codes.InvalidArgument, "a rollback body is required")
	}
	targetDeploymentID := strings.TrimSpace(body.GetTargetDeploymentId())
	expectedCurrent := strings.TrimSpace(body.GetExpectedCurrentAgentDeploymentId())
	if workspaceID == "" || targetDeploymentID == "" || expectedCurrent == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace, target deployment and the expected current deployment are required")
	}
	if s.Runtime == nil || s.Resources == nil || s.References == nil {
		return nil, status.Error(codes.Unavailable, "runtime adapter, Fabric readback and reference coordination must be configured")
	}
	plan, err := s.rollbackPlan(ctx, workspaceID, targetDeploymentID)
	if err != nil {
		return nil, err
	}
	plan.kind = "rollback_workspace"
	plan.call = r.GetContext()
	plan.expectedCurrent = expectedCurrent
	return s.runReplacement(ctx, plan)
}

// rollbackPlan reads the target deployment's own stored descriptor and source
// identity, so a rollback re-executes exactly the revision that was admitted.
func (s *Service) rollbackPlan(ctx context.Context, workspaceID, targetDeploymentID string) (replacementPlan, error) {
	var capabilityVersionID, runtimeVersionID sql.NullString
	var descriptor, compatibility []byte
	var digest, ref string
	err := s.DB.QueryRowContext(ctx, `
		SELECT d.capability_version_id, d.runtime_version_id, i.deployment_descriptor, i.deployment_descriptor_digest, i.deployment_descriptor_object_ref, d.data_compatibility
		FROM serve.agent_deployments d
		JOIN serve.agent_runtime_instances i ON i.deployment_id = d.id
		WHERE d.workspace_id = $1 AND d.id = $2`, workspaceID, targetDeploymentID).
		Scan(&capabilityVersionID, &runtimeVersionID, &descriptor, &digest, &ref, &compatibility)
	if errors.Is(err, sql.ErrNoRows) {
		return replacementPlan{}, status.Errorf(codes.NotFound, "%s: deployment %s is not a delivery of workspace %s", ReasonReplacementUnavailable, targetDeploymentID, workspaceID)
	}
	if err != nil {
		return replacementPlan{}, dbError(err)
	}
	restored := &api.DeploymentDescriptor{}
	if err = publicjson.Unmarshal(descriptor, restored); err != nil {
		return replacementPlan{}, status.Errorf(codes.FailedPrecondition, "%s: the target deployment has no readable descriptor", ReasonReplacementUnavailable)
	}
	return replacementPlan{
		workspaceID:         workspaceID,
		capabilityVersionID: capabilityVersionID.String,
		runtimeVersionID:    runtimeVersionID.String,
		descriptor:          restored,
		descriptorDigest:    digest,
		descriptorRef:       ref,
		// A rollback restores the data contract the target delivery was admitted
		// with. It is read from that delivery's own row rather than re-declared, so
		// the restored application carries exactly the contract it ran under.
		compatibility: compatibility,
	}, nil
}

// replacementPlan is the validated shape of one replacement admission.
type replacementPlan struct {
	call                *api.CallContext
	workspaceID         string
	expectedCurrent     string
	capabilityVersionID string
	runtimeVersionID    string
	descriptor          *api.DeploymentDescriptor
	descriptorDigest    string
	descriptorRef       string
	// compatibility is the JSONB data contract recorded for the replacement
	// delivery: the admitted version's own declaration for a switch, or the target
	// delivery's recorded contract for a rollback.
	compatibility []byte
	// kind is the owning product operation this replacement records:
	// update_workspace for a version switch, rollback_workspace for a restore.
	kind string
}

// runReplacement admits, executes and commits one replacement. The current route
// is untouched until the replacement instance is proven ready, so a failure
// leaves the Workspace serving its previous application.
func (s *Service) runReplacement(ctx context.Context, plan replacementPlan) (*api.Operation, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockWorkspace(ctx, tx, plan.workspaceID); err != nil {
		return nil, dbError(err)
	}
	operationID := replacementOperationID(plan)
	replay, err := s.replacementReplay(ctx, operationID, plan.workspaceID)
	if err != nil {
		return nil, err
	}
	var command *api.RuntimeDeployCommand
	var current *currentDelivery
	exclusive := false
	if replay != nil {
		// A retry of the same logical switch converges on the delivery it already
		// owns, whatever the Workspace's current application has since become: the
		// caller is asking for the result of its own accepted command, not for a
		// second freshness check. Nothing is admitted or stopped again.
		command = replacementCommand(plan, replay.DeploymentID, replay.RuntimeID, replay.ResourceSetID, replay.DataAttachment, replay.ExecutionEpoch)
	} else {
		if current, err = currentDeliveryTx(ctx, tx, plan.workspaceID); err != nil {
			return nil, err
		}
		if current == nil {
			return nil, status.Errorf(codes.FailedPrecondition, "%s: workspace %s has no current application to replace", ReasonReplacementUnavailable, plan.workspaceID)
		}
		if current.DeploymentID != plan.expectedCurrent {
			return nil, status.Errorf(codes.FailedPrecondition, "%s: workspace %s current deployment is %s, not the expected %s", ReasonCurrentDeploymentConflict, plan.workspaceID, current.DeploymentID, plan.expectedCurrent)
		}
		if err = replacementCompatibilityRefusal(current.Compatibility, plan.compatibility, plan.kind == "rollback_workspace"); err != nil {
			return nil, err
		}
		if plan.capabilityVersionID == "" && plan.runtimeVersionID == "" {
			return nil, status.Errorf(codes.FailedPrecondition, "%s: the replacement names no admitted application source", ReasonReplacementUnavailable)
		}
		// The admitted descriptor and the digest Serve records for it must be the
		// same fact. A mismatch is refused before any identity is allocated, so
		// Serve never persists evidence that does not describe what it executes.
		digest, err := descriptorDigest(plan.descriptor)
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, "the replacement descriptor cannot be canonicalized")
		}
		if digest != plan.descriptorDigest {
			return nil, status.Errorf(codes.FailedPrecondition, "%s: the replacement descriptor does not match its admitted digest", ReasonReplacementUnavailable)
		}
		// A read-write mount the publisher declares it cannot share forces the two
		// revisions apart: the running writer must stop before the replacement
		// mounts the same retained data. Both sides are compared, because either
		// revision may be the one that states the limit.
		if exclusive, err = exclusiveDataWriter(current.Descriptor, plan.descriptor); err != nil {
			return nil, err
		}
		if command, err = s.admitReplacementTx(ctx, tx, plan, current, operationID); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	// The reference claim binds the replacement to the exact admitted version
	// before any provider call, exactly as the first delivery does.
	if _, err = s.bindReservation(ctx, plan.call, &api.RuntimeReservation{DeploymentId: command.GetDeploymentId()}); err != nil {
		return nil, err
	}
	// The frozen start command is persisted before the provider call, so this
	// replacement is as recoverable as a first delivery: stop, reload, credentials
	// and a later replacement all resume the identical command, and a replay of
	// this switch finds its own row instead of writing a second one.
	if err = s.acceptReplacementStart(ctx, command); err != nil {
		return nil, err
	}
	if replay == nil && exclusive {
		// The old writer is stopped only after its replacement is admitted, so the
		// Workspace always has a recorded reason for the gap, and the stop is
		// recorded before the provider call so a replay never issues it twice.
		if err = s.quiesceCurrentWriter(ctx, plan, current, command.GetDeploymentId()); err != nil {
			return nil, err
		}
	}
	readback, err := s.observeAndFinishDelivery(ctx, command, true)
	if err != nil {
		// The replacement's outcome is unknown. No new side effect is issued
		// against it: a writer this switch stopped stays stopped, and the
		// unresolved replacement is resolved by reading it back, exactly as an
		// unresolved route switch blocks every later switch. Returning the
		// original error keeps the caller from reading a guess as a result.
		return nil, err
	}
	if readback.GetApplicationAvailable() {
		// The replacement is the Workspace's current application. The route and the
		// superseded delivery were committed in the transaction that observed it.
		return s.deliveryOperation(ctx, operationID)
	}
	if state := readback.GetState(); state != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED && state != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED {
		// Still converging: the replacement may yet become the current
		// application, so nothing is retired and the owning operation reports the
		// state the provider actually observed.
		return s.deliveryOperation(ctx, operationID)
	}
	// A definite failure. The replacement's own runtime is retired so it cannot
	// remain a second instance of this Workspace, and a writer the switch stopped
	// is restored. The route never moved, so the Workspace serves the delivery it
	// already had.
	if err = s.retireFailedReplacement(ctx, command); err != nil {
		return nil, err
	}
	if err = s.restoreWriterStoppedByReplacement(ctx, command.GetDeploymentId()); err != nil {
		return nil, err
	}
	return s.deliveryOperation(ctx, operationID)
}

// restoreWriterStoppedByReplacement restores the delivery a switch stopped before
// the replacement failed. Whether this switch took the writer away is a persisted
// fact -- the confirmed stop it recorded against the replacement's own identity --
// not a value re-derived from whichever application the Workspace serves now. A
// replayed switch therefore restores exactly the writer it stopped, and a switch
// that stopped nothing issues no recovery call.
func (s *Service) restoreWriterStoppedByReplacement(ctx context.Context, replacementDeploymentID string) error {
	var stopped bool
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.agent_runtime_actions WHERE command_id=$1 AND action='stop' AND observation_result='confirmed')`, stableID("stop_", replacementDeploymentID)).Scan(&stopped); err != nil {
		return dbError(err)
	}
	if !stopped {
		return nil
	}
	// The delivery this replacement displaced is the one whose writer was taken
	// away, and its runtime instance is the exact object the stop was applied to.
	var replacedDeploymentID, replacedRuntimeID string
	if err := s.DB.QueryRowContext(ctx, `
		SELECT d.previous_deployment_id, i.id
		FROM serve.agent_deployments d
		JOIN serve.agent_runtime_instances i ON i.deployment_id = d.previous_deployment_id
		WHERE d.id=$1`, replacementDeploymentID).Scan(&replacedDeploymentID, &replacedRuntimeID); err != nil {
		return dbError(err)
	}
	return s.resumeCurrentWriter(ctx, &currentDelivery{DeploymentID: replacedDeploymentID, RuntimeID: replacedRuntimeID}, replacementDeploymentID)
}

// acceptReplacementStart persists the frozen start command of one replacement
// delivery in its own transaction, exactly as the first delivery does.
func (s *Service) acceptReplacementStart(ctx context.Context, command *api.RuntimeDeployCommand) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockWorkspace(ctx, tx, command.GetWorkspaceId()); err != nil {
		return dbError(err)
	}
	if err = recordStartActionTx(ctx, tx, command); err != nil {
		return err
	}
	return dbError(tx.Commit())
}

// admitReplacementTx admits or resumes one replacement delivery inside the
// caller's transaction. A first attempt allocates the delivery identity, claims
// the admitted source and records the owning operation; a retry of the same
// logical switch names the same identities and resumes the row it already owns
// instead of allocating a second delivery.
//
// The new deployment names the deployment it replaces, carries its own runtime
// instance at the same confirmed resource set and data attachment, and stays
// queued until the execution adapter observes it.
func (s *Service) admitReplacementTx(ctx context.Context, tx *sql.Tx, plan replacementPlan, current *currentDelivery, operationID string) (*api.RuntimeDeployCommand, error) {
	var tenantID string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(o.tenant_id,'') FROM serve.agent_deployments d JOIN serve.operations o ON o.id=d.operation_id WHERE d.id=$1`, current.DeploymentID).Scan(&tenantID); err != nil {
		return nil, dbError(err)
	}
	deploymentID := stableID("dep_", tenantID, plan.workspaceID, plan.call.GetActorId(), plan.call.GetIdempotencyKey())
	runtimeID := stableID("rti_", deploymentID)
	claimTarget := &api.ReferenceTarget{}
	if plan.capabilityVersionID != "" {
		claimTarget.Target = &api.ReferenceTarget_CapabilityVersionId{CapabilityVersionId: plan.capabilityVersionID}
	} else {
		claimTarget.Target = &api.ReferenceTarget_RuntimeVersionId{RuntimeVersionId: plan.runtimeVersionID}
	}
	claim, err := s.References.AcquireReference(ctx, &api.ReferenceClaimRequest{Context: nextOwnerCall(plan.call), Target: claimTarget, ClaimantOwner: api.OwnerEnum_OWNER_ENUM_SERVE, ClaimantResourceId: deploymentID})
	if err != nil {
		return nil, err
	}
	if claim.GetId() == "" || claim.GetClaimantOwner() != api.OwnerEnum_OWNER_ENUM_SERVE || claim.GetClaimantResourceId() != deploymentID || claim.GetState() == api.ReferenceClaimState_REFERENCE_CLAIM_STATE_RELEASED {
		return nil, status.Error(codes.FailedPrecondition, "reference claim identity mismatch")
	}
	applicationKind := "agent"
	if plan.capabilityVersionID == "" {
		applicationKind = "opl_app"
	}
	if applicationKind == "agent" && claim.GetTarget().GetCapabilityVersionId() != plan.capabilityVersionID {
		return nil, status.Error(codes.FailedPrecondition, "Capability claim identity mismatch")
	}
	if applicationKind == "opl_app" && claim.GetTarget().GetRuntimeVersionId() != plan.runtimeVersionID {
		return nil, status.Error(codes.FailedPrecondition, "Runtime Release claim identity mismatch")
	}
	accepted, err := replacementAcceptedInput(plan, deploymentID, current.ResourceSetID, current.DataAttachment)
	if err != nil {
		return nil, err
	}
	if _, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operationID, TenantID: tenantID, ActorID: plan.call.GetActorId(), Kind: plan.kind, ResourceID: deploymentID, Stage: "runtime", RequestID: plan.call.GetRequestId(), AcceptedInput: accepted}); err != nil {
		return nil, dbError(err)
	}
	compatibility := plan.compatibility
	descriptor, err := publicjson.Marshal(plan.descriptor)
	if err != nil {
		return nil, err
	}
	attachment, _ := json.Marshal(map[string]string{"attachmentId": current.DataAttachment})
	if _, err = tx.ExecContext(ctx, `INSERT INTO serve.agent_deployments(id,workspace_id,capability_version_id,application_kind,runtime_version_id,artifact_digest,reference_claim_id,runtime_instance_id,previous_deployment_id,operation_id,status,data_compatibility,execution_epoch) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'queued',$11,$12)`,
		deploymentID, plan.workspaceID, plan.capabilityVersionID, applicationKind, nullable(plan.runtimeVersionID), plan.descriptor.GetArtifact().GetDigest(), claim.GetId(), runtimeID, current.DeploymentID, operationID, compatibility, current.ExecutionEpoch+1); err != nil {
		return nil, dbError(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO serve.agent_runtime_instances(id,workspace_id,deployment_id,artifact_digest,fabric_resource_set_id,status,data_attachment_contract,execution_epoch,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref) VALUES($1,$2,$3,$4,$5,'pending',$6,$7,$8,$9,$10)`,
		runtimeID, plan.workspaceID, deploymentID, plan.descriptor.GetArtifact().GetDigest(), current.ResourceSetID, attachment, current.ExecutionEpoch+1, descriptor, plan.descriptorDigest, plan.descriptorRef); err != nil {
		return nil, dbError(err)
	}
	command := replacementCommand(plan, deploymentID, runtimeID, current.ResourceSetID, current.DataAttachment, current.ExecutionEpoch+1)
	return command, nil
}

// replacementOperationID is the owning operation of one logical switch. It keys
// the admission on the caller's own command identity, so a retry of the same
// switch names the same operation while two different switches never collide.
func replacementOperationID(plan replacementPlan) string {
	return stableID("op_", plan.kind, plan.workspaceID, plan.call.GetActorId(), plan.call.GetIdempotencyKey())
}

// replacementReplay reports the delivery a retry of this logical switch already
// owns. The owning operation is the recorded identity, and the delivery it named
// is read back rather than re-derived, so a replay never depends on which
// application the Workspace happens to be serving now.
func (s *Service) replacementReplay(ctx context.Context, operationID, workspaceID string) (*currentDelivery, error) {
	var deploymentID string
	err := s.DB.QueryRowContext(ctx, `SELECT resource_id FROM serve.operations WHERE id=$1 AND kind IN ('update_workspace','rollback_workspace')`, operationID).Scan(&deploymentID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbError(err)
	}
	return existingReplacementTx(ctx, s.DB, deploymentID, workspaceID)
}

// existingReplacementTx reads one admitted replacement delivery by its own id.
func existingReplacementTx(ctx context.Context, q rowQueryer, deploymentID, workspaceID string) (*currentDelivery, error) {
	row := q.QueryRowContext(ctx, `
		SELECT d.id, COALESCE(d.runtime_instance_id,''), d.execution_epoch, i.fabric_resource_set_id, COALESCE(i.data_attachment_contract->>'attachmentId',''), d.data_compatibility, i.deployment_descriptor
		FROM serve.agent_deployments d
		JOIN serve.agent_runtime_instances i ON i.deployment_id = d.id
		WHERE d.id=$1 AND d.workspace_id=$2`, deploymentID, workspaceID)
	out := &currentDelivery{}
	if err := row.Scan(&out.DeploymentID, &out.RuntimeID, &out.ExecutionEpoch, &out.ResourceSetID, &out.DataAttachment, &out.Compatibility, &out.Descriptor); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, dbError(err)
	}
	return out, nil
}

// replacementCommand is the single builder of a replacement's execution command,
// so an admitted delivery and its replayed form are the same command.
func replacementCommand(plan replacementPlan, deploymentID, runtimeID, resourceSetID, dataAttachmentID string, epoch int64) *api.RuntimeDeployCommand {
	command := &api.RuntimeDeployCommand{
		Context: plan.call, WorkspaceId: plan.workspaceID, DeploymentId: deploymentID,
		CapabilityVersionId: plan.capabilityVersionID, DeploymentDescriptor: plan.descriptor,
		ResourceSetId: resourceSetID, DataAttachmentId: dataAttachmentID, RuntimeInstanceId: runtimeID,
		DeploymentDescriptorDigest: plan.descriptorDigest, ExecutionEpoch: epoch, DeploymentDescriptorObjectRef: plan.descriptorRef,
	}
	if plan.capabilityVersionID != "" {
		command.ApplicationSelection = &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT, CapabilityVersionId: proto.String(plan.capabilityVersionID)}
	} else {
		command.ApplicationSelection = &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP, RuntimeVersionId: proto.String(plan.runtimeVersionID)}
	}
	return command
}

// replacementAcceptedInput stores the replacement in the same accepted-input
// shape the reservation path uses, so the reference claim's owner evidence and
// Capability's own readback work for a replacement without a second format.
func replacementAcceptedInput(plan replacementPlan, deploymentID, resourceSetID, dataAttachmentID string) ([]byte, error) {
	reservation := &api.RuntimeReservationCommand{
		Context: plan.call, WorkspaceId: plan.workspaceID, DeploymentId: deploymentID, CapabilityVersionId: plan.capabilityVersionID,
		Artifact: plan.descriptor.GetArtifact(), DeploymentDescriptor: plan.descriptor,
		DeploymentDescriptorDigest: plan.descriptorDigest, DeploymentDescriptorObjectRef: plan.descriptorRef,
		ResourceSetId: resourceSetID, DataAttachmentId: dataAttachmentID,
	}
	if plan.capabilityVersionID == "" {
		reservation.ApplicationSelection = &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP, RuntimeVersionId: proto.String(plan.runtimeVersionID)}
	} else {
		reservation.ApplicationSelection = &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT, CapabilityVersionId: proto.String(plan.capabilityVersionID)}
	}
	request := wire(reservation)
	scope := wire(plan.call.GetScope())
	return json.Marshal(reservationInput{Request: request, Actor: plan.call.GetActorId(), Scope: scope, AuthorizationContextID: plan.call.GetAuthorizationContextId(), Digest: "sha256:" + ownerstore.HashRequestBody(reservationBytes(reservation))})
}

// rowQueryer is the read surface both an owner transaction and a plain owner
// connection provide, so one delivery reader serves the locked admission path and
// the lock-free replay probe without two copies of the same query.
type rowQueryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// currentDelivery is the Workspace's one current application together with the
// facts a replacement must act on: the frozen command it executes under, the
// confirmed infrastructure it holds, and its own declared data/mount facts.
type currentDelivery struct {
	DeploymentID   string
	RuntimeID      string
	ExecutionEpoch int64
	ResourceSetID  string
	DataAttachment string
	Compatibility  []byte
	Descriptor     []byte
}

// currentDeliveryTx reads the Workspace's current delivery and the confirmed
// infrastructure it runs on. A Workspace with no active delivery returns nil
// rather than an empty identity, so a caller cannot confuse "no application"
// with a delivery whose identity happens to be blank.
func currentDeliveryTx(ctx context.Context, q rowQueryer, workspaceID string) (*currentDelivery, error) {
	out := &currentDelivery{}
	err := q.QueryRowContext(ctx, `
		SELECT d.id, COALESCE(d.runtime_instance_id,''), d.execution_epoch, i.fabric_resource_set_id, COALESCE(i.data_attachment_contract->>'attachmentId',''), d.data_compatibility, i.deployment_descriptor
		FROM serve.agent_deployments d
		JOIN serve.agent_runtime_instances i ON i.deployment_id = d.id
		WHERE d.workspace_id = $1 AND d.status = 'active'
		ORDER BY d.created_at DESC, d.id DESC LIMIT 1`, workspaceID).
		Scan(&out.DeploymentID, &out.RuntimeID, &out.ExecutionEpoch, &out.ResourceSetID, &out.DataAttachment, &out.Compatibility, &out.Descriptor)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, dbError(err)
	}
	return out, nil
}

// deliveryOperation reports the owning Operation for one admitted delivery from
// Serve's own operation row.
func (s *Service) deliveryOperation(ctx context.Context, operationID string) (*api.Operation, error) {
	return s.operationByID(ctx, operationID)
}

// operationByID reports one of Serve's own Operations exactly as it is recorded,
// so a caller never reads a state Serve has not committed.
func (s *Service) operationByID(ctx context.Context, operationID string) (*api.Operation, error) {
	var kind, statusText, stage, resource, requestID, result string
	var created, updated, completed sql.NullTime
	err := s.DB.QueryRowContext(ctx, `SELECT kind,status,stage,resource_id,COALESCE(request_id,''),COALESCE(observation_result,''),created_at,updated_at,completed_at FROM serve.operations WHERE id=$1`, operationID).
		Scan(&kind, &statusText, &stage, &resource, &requestID, &result, &created, &updated, &completed)
	if err != nil {
		return nil, dbError(err)
	}
	out := &api.Operation{OperationId: operationID, Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE, ResourceId: resource, RequestId: requestID}
	if value, ok := api.OperationKindEnum_value["OPERATION_KIND_ENUM_"+strings.ToUpper(kind)]; ok {
		out.Kind = api.OperationKindEnum(value)
	}
	if value, ok := api.OperationStageEnum_value["OPERATION_STAGE_ENUM_"+strings.ToUpper(stage)]; ok {
		out.Stage = api.OperationStageEnum(value)
	}
	if value, ok := api.OperationStatusEnum_value["OPERATION_STATUS_ENUM_"+strings.ToUpper(statusText)]; ok {
		out.Status = api.OperationStatusEnum(value)
	}
	if value, ok := api.OperationObservationResultEnum_value["OPERATION_OBSERVATION_RESULT_ENUM_"+strings.ToUpper(result)]; ok {
		out.ObservationResult = api.OperationObservationResultEnum(value).Enum()
	}
	if created.Valid {
		out.CreatedAt = timestamppb.New(created.Time.UTC())
	}
	if updated.Valid {
		out.UpdatedAt = timestamppb.New(updated.Time.UTC())
	}
	_ = completed
	return out, nil
}

// replacementCompatibilityRefusal is the declared data-compatibility gate of a
// replacement. It compares only facts a publisher declared for the exact
// revisions involved: the schema version each side writes, the schema versions
// the target revision can carry forward from, and the target's own declared
// rollback safety. A version string is compared for equality and never ordered,
// so no compatibility is inferred from a version's name.
//
// The Workspace's data is the one fact a replacement must not risk: an update
// that cannot carry the existing schema forward is refused, and a change whose
// publisher does not declare it can be rolled back is refused rather than
// executed, exactly because Serve cannot undo data it was never told how to
// restore.
func replacementCompatibilityRefusal(running, target []byte, rollback bool) error {
	current := &api.DataCompatibility{}
	if len(running) > 0 {
		if err := publicjson.Unmarshal(running, current); err != nil {
			return status.Errorf(codes.DataLoss, "the current delivery has no readable data contract")
		}
	}
	declared := &api.DataCompatibility{}
	if len(target) > 0 {
		if err := publicjson.Unmarshal(target, declared); err != nil {
			return status.Errorf(codes.FailedPrecondition, "%s: the replacement declares no readable data contract", ReasonReplacementUnavailable)
		}
	}
	currentSchema, targetSchema := current.GetDataSchemaVersion(), declared.GetDataSchemaVersion()
	if currentSchema == "" && targetSchema == "" {
		// Neither revision declares a data schema, so this replacement makes no
		// schema transition to prove and no declared contract is violated.
		return nil
	}
	if targetSchema != "" && targetSchema == currentSchema {
		// The same declared schema: the data passes unchanged. A version that still
		// runs a publisher migration over it must also declare that migration
		// rollback-safe.
		return migrationRollbackRefusal(declared)
	}
	if rollback {
		// Restoring an older application returns the data to the schema it expects.
		// That is proven only by the running version declaring its own data
		// rollback-safe.
		if current.GetRollbackSafe() {
			return nil
		}
		return status.Errorf(codes.FailedPrecondition, "%s: the running schema %q is not declared rollback-safe, so schema %q cannot be restored", ReasonDataRollbackUnsafe, currentSchema, targetSchema)
	}
	for _, from := range declared.GetCompatibleFromVersions() {
		if currentSchema != "" && from == currentSchema {
			return migrationRollbackRefusal(declared)
		}
	}
	return status.Errorf(codes.FailedPrecondition, "%s: schema %q does not declare compatibility with the current schema %q", ReasonDataCompatibilityUnproven, targetSchema, currentSchema)
}

// migrationRollbackRefusal refuses an admitted version that requires a publisher
// migration but does not declare that the migrated data can be rolled back.
func migrationRollbackRefusal(declared *api.DataCompatibility) error {
	if declared.GetMigrationRequired() && !declared.GetRollbackSafe() {
		return status.Errorf(codes.FailedPrecondition, "%s: schema %q requires a migration the publisher does not declare rollback-safe", ReasonDataRollbackUnsafe, declared.GetDataSchemaVersion())
	}
	return nil
}

// exclusiveDataWriter reports whether the two admitted revisions must not hold
// the Workspace's retained data at the same time. The publisher owns the data
// schema and states, per persistent mount, whether more than one writer is
// allowed; a read-write mount its publisher does not declare shareable can only
// be held by one running application. Both sides are compared, because either
// revision may be the one that states the limit.
//
// A revision that declares no such mount shares nothing exclusively, so the
// ordinary update needs no stop and no gap in service.
func exclusiveDataWriter(running []byte, target *api.DeploymentDescriptor) (bool, error) {
	if descriptorDeclaresExclusiveWriter(target) {
		return true, nil
	}
	if len(running) == 0 {
		return false, nil
	}
	descriptor := &api.DeploymentDescriptor{}
	if err := publicjson.Unmarshal(running, descriptor); err != nil {
		return false, status.Error(codes.DataLoss, "the current delivery has no readable deployment descriptor")
	}
	return descriptorDeclaresExclusiveWriter(descriptor), nil
}

// descriptorDeclaresExclusiveWriter is the one place that reads a revision's
// declared mount policy. A policy counts only when it names a mount the same
// revision declares read-write, so a policy for a read-only or absent mount
// never blocks a replacement.
func descriptorDeclaresExclusiveWriter(descriptor *api.DeploymentDescriptor) bool {
	readWrite := map[string]bool{}
	for _, mount := range descriptor.GetApplicationRevision().GetPersistentMounts() {
		if !mount.GetReadOnly() {
			readWrite[mount.GetName()] = true
		}
	}
	for _, policy := range descriptor.GetRuntimeContract().GetData().GetMountPolicies() {
		if !policy.GetConcurrentWritersSupported() && readWrite[policy.GetMountName()] {
			return true
		}
	}
	return false
}

// recordRuntimeAction persists one lifecycle intent against a runtime instance
// before the provider call and reports whether that intent was already confirmed
// by an earlier attempt. A confirmed identical action needs no second provider
// call; an unconfirmed one is issued again, because the desired state is
// idempotent and the earlier outcome is unknown.
func (s *Service) recordRuntimeAction(ctx context.Context, id, runtimeInstanceID, action, expectedDeploymentID string, snapshot []byte) (bool, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, dbError(err)
	}
	defer func() { _ = tx.Rollback() }()
	// The stored snapshot is PostgreSQL jsonb, so compare it semantically in the
	// database rather than byte-for-byte against Go's encoding of the request.
	var result string
	var identical bool
	err = tx.QueryRowContext(ctx, `SELECT observation_result,input_snapshot=$2::jsonb FROM serve.agent_runtime_actions WHERE command_id=$1 FOR UPDATE`, id, snapshot).Scan(&result, &identical)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if _, err = tx.ExecContext(ctx, `INSERT INTO serve.agent_runtime_actions(id,runtime_instance_id,command_id,action,expected_deployment_id,input_snapshot,observation_result) VALUES($1,$2,$1,$3,$4,$5,'unknown')`, id, runtimeInstanceID, action, expectedDeploymentID, snapshot); err != nil {
			return false, dbError(err)
		}
		if err = tx.Commit(); err != nil {
			return false, dbError(err)
		}
		return false, nil
	case err != nil:
		return false, dbError(err)
	}
	if !identical {
		return false, status.Error(codes.AlreadyExists, "runtime lifecycle action input differs from its original command")
	}
	return result == "confirmed", dbError(tx.Commit())
}

// confirmRuntimeAction records that the provider confirmed one lifecycle action.
func (s *Service) confirmRuntimeAction(ctx context.Context, id, evidence string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE serve.agent_runtime_actions SET observation_result='confirmed',evidence_ref=NULLIF($2,''),updated_at=now() WHERE command_id=$1`, id, evidence)
	return dbError(err)
}

// quiesceCurrentWriter stops the application the Workspace currently serves
// before a replacement that cannot share its retained data with it. The stop is
// recorded against the replacement's own identity before the provider call, so a
// replayed switch never issues a second stop.
func (s *Service) quiesceCurrentWriter(ctx context.Context, plan replacementPlan, current *currentDelivery, replacementDeploymentID string) error {
	deploy, err := s.persistedRuntimeCommand(ctx, current.RuntimeID, current.DeploymentID)
	if err != nil {
		return err
	}
	target, err := s.confirmedRuntimeTarget(ctx, deploy.command)
	if err != nil {
		return err
	}
	id := stableID("stop_", replacementDeploymentID)
	snapshot, err := json.Marshal(map[string]string{"action": "stop", "runtimeInstanceId": current.RuntimeID, "deploymentId": current.DeploymentID, "replacementDeploymentId": replacementDeploymentID})
	if err != nil {
		return status.Error(codes.Internal, "cannot record the replacement stop")
	}
	confirmed, err := s.recordRuntimeAction(ctx, id, current.RuntimeID, "stop", current.DeploymentID, snapshot)
	if err != nil {
		return err
	}
	if confirmed {
		return nil
	}
	if err = s.Runtime.Lifecycle(ctx, deploy.command, target, "suspended"); err != nil {
		return err
	}
	if err = s.confirmRuntimeAction(ctx, id, "suspended:"+replacementDeploymentID); err != nil {
		return err
	}
	return s.setRuntimeInstanceState(ctx, current.RuntimeID, "stopped")
}

// resumeCurrentWriter restores the application a switch stopped, after a
// replacement failed definitively. It is the inverse of quiesceCurrentWriter
// under its own recorded identity, and it restores the same runtime instance the
// Workspace was already serving rather than admitting a new delivery.
func (s *Service) resumeCurrentWriter(ctx context.Context, current *currentDelivery, replacementDeploymentID string) error {
	deploy, err := s.persistedRuntimeCommand(ctx, current.RuntimeID, current.DeploymentID)
	if err != nil {
		return err
	}
	target, err := s.confirmedRuntimeTarget(ctx, deploy.command)
	if err != nil {
		return err
	}
	resumeID := stableID("resume_", replacementDeploymentID)
	snapshot, err := json.Marshal(map[string]string{"action": "start", "runtimeInstanceId": current.RuntimeID, "deploymentId": current.DeploymentID, "replacementDeploymentId": replacementDeploymentID})
	if err != nil {
		return status.Error(codes.Internal, "cannot record the replacement recovery")
	}
	confirmed, err := s.recordRuntimeAction(ctx, resumeID, current.RuntimeID, "start", current.DeploymentID, snapshot)
	if err != nil {
		return err
	}
	if !confirmed {
		if err = s.Runtime.Lifecycle(ctx, deploy.command, target, "running"); err != nil {
			return err
		}
		if err = s.confirmRuntimeAction(ctx, resumeID, "running:"+replacementDeploymentID); err != nil {
			return err
		}
	}
	return s.setRuntimeInstanceState(ctx, current.RuntimeID, "ready")
}

// setRuntimeInstanceState records the state Serve itself applied and the provider
// confirmed for one runtime instance. A replacement that stops the only writer,
// or restores it, changes the state of that instance and must not leave Serve's
// own record claiming a state the provider was told to leave.
func (s *Service) setRuntimeInstanceState(ctx context.Context, runtimeID, state string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `UPDATE serve.agent_runtime_instances SET status=$2,observed_at=now(),updated_at=now() WHERE id=$1`, runtimeID, state); err != nil {
		return dbError(err)
	}
	return dbError(tx.Commit())
}

// retireFailedReplacement removes the exact runtime a replacement created after
// it failed to become the Workspace's application. It is recorded under the
// replacement's own identity, so a replayed switch retires the same runtime
// instead of a newly derived one.
func (s *Service) retireFailedReplacement(ctx context.Context, command *api.RuntimeDeployCommand) error {
	if s.Runtime == nil {
		return status.Error(codes.Unavailable, "runtime execution adapter is not configured")
	}
	target, err := s.confirmedRuntimeTarget(ctx, command)
	if err != nil {
		return err
	}
	id := stableID("terminate_", command.GetDeploymentId())
	snapshot, err := json.Marshal(map[string]string{"action": "terminate", "runtimeInstanceId": command.GetRuntimeInstanceId(), "deploymentId": command.GetDeploymentId()})
	if err != nil {
		return status.Error(codes.Internal, "cannot record the failed replacement retirement")
	}
	confirmed, err := s.recordRuntimeAction(ctx, id, command.GetRuntimeInstanceId(), "terminate", command.GetDeploymentId(), snapshot)
	if err != nil {
		return err
	}
	if confirmed {
		return nil
	}
	if err = s.Runtime.Lifecycle(ctx, command, target, "absent"); err != nil {
		return err
	}
	if err = s.confirmRuntimeAction(ctx, id, "absent:"+command.GetDeploymentId()); err != nil {
		return err
	}
	return s.setRuntimeInstanceState(ctx, command.GetRuntimeInstanceId(), "terminated")
}
