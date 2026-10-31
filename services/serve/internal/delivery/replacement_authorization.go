package delivery

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

func refusePendingReplacement(ctx context.Context, q rowQueryer, workspace, operation string) error {
	var pending bool
	err := q.QueryRowContext(ctx, `SELECT
 EXISTS(SELECT 1 FROM serve.agent_deployments d JOIN serve.agent_runtime_instances i ON i.deployment_id=d.id WHERE d.workspace_id=$1 AND d.previous_deployment_id IS NOT NULL AND d.status IN ('queued','deploying','verifying','rolling_back','needs_attention','failed') AND i.status <> 'terminated')
 OR EXISTS(SELECT 1 FROM serve.operations o WHERE o.kind='update_workspace' AND o.id<>$2 AND o.status<>'succeeded' AND o.accepted_input->'request'->>'workspace_id'=$1 AND NOT EXISTS(SELECT 1 FROM serve.agent_deployments d WHERE d.operation_id=o.id))`, workspace, operation).Scan(&pending)
	if err != nil {
		return dbError(err)
	}
	if pending {
		return status.Errorf(codes.FailedPrecondition, "%s: resume the original replacement before changing its writer", ReasonCurrentDeploymentConflict)
	}
	return nil
}

// A definitive authority refusal is visible on the original operation. Its
// reference outcome can still be unknown after a lost ACK, so it remains fenced
// for owner recovery instead of being cancelled or silently readmitted.
func (s *Service) replacementRefusal(ctx context.Context, operation string, cause error) error {
	switch status.Code(cause) {
	case codes.PermissionDenied, codes.FailedPrecondition, codes.NotFound:
		_, err := s.DB.ExecContext(ctx, `UPDATE serve.operations SET status='needs_attention',stage='runtime',error_code=$2,observation_result='unknown',updated_at=now() WHERE id=$1 AND status NOT IN ('succeeded','failed','cancelled')`, operation, ReasonReplacementUnavailable)
		if err != nil {
			return dbError(err)
		}
	}
	return cause
}

// An accepted update exists before its first reference claim. Identity reads
// this committed owner evidence to issue the existing, bounded operation grant.
func (s *Service) acceptReplacementTx(ctx context.Context, tx *sql.Tx, plan *replacementPlan, current *currentDelivery, operationID string) error {
	var tenant string
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(o.tenant_id,'') FROM serve.agent_deployments d JOIN serve.operations o ON o.id=d.operation_id WHERE d.id=$1`, current.DeploymentID).Scan(&tenant); err != nil {
		return dbError(err)
	}
	deployment := stableID("dep_", tenant, plan.workspaceID, plan.call.GetActorId(), plan.call.GetIdempotencyKey())
	plan.accepted = true
	accepted, err := replacementAcceptedInput(*plan, deployment, current.ResourceSetID, current.DataAttachment)
	if err != nil {
		return err
	}
	_, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operationID, TenantID: tenant, ActorID: plan.call.GetActorId(), Kind: plan.kind, ResourceID: deployment, Stage: "runtime", RequestID: plan.call.GetRequestId(), AcceptedInput: accepted})
	return dbError(err)
}

// Normalize JSON objects with exact numbers before hashing: PostgreSQL jsonb
// reorders object keys, while execution epochs must never pass through float64.
func replacementAdmissionDigest(input replacementInput) (string, error) {
	input.Digest = ""
	raw, err := json.Marshal(input)
	if err != nil {
		return "", err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err = decoder.Decode(&value); err != nil {
		return "", err
	}
	raw, err = json.Marshal(value)
	return "sha256:" + ownerstore.HashRequestBody(raw), err
}

func restoreAcceptedReplacement(input replacementInput, plan *replacementPlan) error {
	digest, err := replacementAdmissionDigest(input)
	request, predecessor, scope := &api.RuntimeReservationCommand{}, &api.RuntimeDeployCommand{}, &api.AuthorizationScope{}
	if err != nil || input.AcceptanceVersion != 1 || digest != input.Digest ||
		protojson.Unmarshal(input.Request, request) != nil || protojson.Unmarshal(input.ReplacementState, predecessor) != nil || protojson.Unmarshal(input.Scope, scope) != nil || len(input.Compatibility) == 0 {
		return status.Error(codes.DataLoss, "accepted replacement evidence is invalid")
	}
	selection := request.GetApplicationSelection()
	if input.Actor != plan.call.GetActorId() || !proto.Equal(scope, plan.call.GetScope()) || request.GetWorkspaceId() != plan.workspaceID || input.ExpectedCurrent != plan.expectedCurrent || selection.GetCapabilityVersionId() != plan.capabilityVersionID || selection.GetRuntimeVersionId() != plan.runtimeVersionID {
		return status.Error(codes.AlreadyExists, "replacement input differs from its original command")
	}
	if predecessor.GetWorkspaceId() != plan.workspaceID || predecessor.GetDeploymentId() != input.ExpectedCurrent || predecessor.GetRuntimeInstanceId() == "" || predecessor.GetExecutionEpoch() < 1 || request.GetResourceSetId() != predecessor.GetResourceSetId() || request.GetDataAttachmentId() != predecessor.GetDataAttachmentId() {
		return status.Error(codes.DataLoss, "accepted replacement predecessor identity mismatch")
	}
	descriptor := request.GetDeploymentDescriptor()
	descriptorHash, err := descriptorDigest(descriptor)
	if err != nil || descriptorHash != request.GetDeploymentDescriptorDigest() || (descriptor.GetApplicationSelection() != nil && !proto.Equal(descriptor.GetApplicationSelection(), selection)) || !proto.Equal(descriptor.GetArtifact(), request.GetArtifact()) {
		return status.Error(codes.DataLoss, "accepted replacement descriptor identity mismatch")
	}
	plan.descriptor, plan.descriptorDigest, plan.descriptorRef = descriptor, descriptorHash, request.GetDeploymentDescriptorObjectRef()
	plan.inherited, plan.exclusive, plan.compatibility, plan.accepted = predecessor, input.ExclusiveWriter, input.Compatibility, true
	return nil
}

func (s *Service) replacementContinuation(ctx context.Context, plan replacementPlan) (*api.CallContext, error) {
	if s.Identity == nil {
		return nil, status.Error(codes.Unavailable, "accepted-operation grant issuer is unavailable")
	}
	var deployment string
	if err := s.DB.QueryRowContext(ctx, `SELECT resource_id FROM serve.operations WHERE id=$1 AND kind='update_workspace'`, replacementOperationID(plan)).Scan(&deployment); err != nil {
		return nil, dbError(err)
	}
	evidence, _, err := s.ownerEvidence(ctx, deployment)
	if err != nil {
		return nil, err
	}
	grant, err := s.Identity.IssueAcceptedOperationGrant(ctx, &api.AcceptedOperationGrantRequest{AuthorizationContextId: evidence.GetAuthorizationContextId(), OwnerCommitEvidence: evidence, AllowedActions: []api.AuthorizationActionEnum{
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDREFERENCE,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS,
	}})
	if err != nil {
		return nil, err
	}
	if grant.GetId() == "" || grant.GetAcceptedOperationOwner() != api.OwnerEnum_OWNER_ENUM_SERVE || grant.GetAcceptedOperationId() != evidence.GetOperationId() || grant.GetResourceId() != deployment || grant.GetActorId() != evidence.GetActorId() || !proto.Equal(grant.GetScope(), evidence.GetScope()) {
		return nil, status.Error(codes.FailedPrecondition, "replacement grant identity mismatch")
	}
	call := nextOwnerCall(plan.call)
	call.AcceptedOperationGrantId = proto.String(grant.GetId())
	call.SessionId, call.DeadlineAt = nil, nil
	return call, nil
}
