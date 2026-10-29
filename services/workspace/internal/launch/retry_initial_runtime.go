package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerstore"
)

// RetryInitialRuntime is a local Workspace-owner command, not an inbound RPC.
// The operator authorizes a replay; the original accepted grant alone authorizes
// the unchanged Workspace-to-Serve command. No caller supplies deployment input.
func (s *Service) RetryInitialRuntime(ctx context.Context, request *api.ReconcileOperationRpcRequest) (*api.Operation, error) {
	if err := ValidateInitialRuntimeRetry(request); err != nil {
		return nil, err
	}
	if s.Identity == nil || s.Serve == nil || s.Store == nil {
		return nil, status.Error(codes.Unavailable, "runtime recovery dependencies are not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, resumeTimeout)
	defer cancel()
	if err := s.authorizeInitialRuntimeRetry(ctx, request); err != nil {
		return nil, err
	}
	op, err := s.Store.ReadOperation(ctx, request.OperationId)
	if err != nil {
		return nil, dbError(err)
	}
	if err = initialRuntimeRetryOrder(op); err != nil {
		return nil, err
	}
	token := id("lease_")
	var leased string
	err = s.Store.DB().QueryRowContext(ctx, `UPDATE workspace.operations SET worker_lease_token=$2,worker_lease_until=now()+interval '60 seconds'
  WHERE id=$1 AND kind='create_workspace' AND stage='runtime' AND status NOT IN ('succeeded','failed','cancelled')
  AND (worker_lease_until IS NULL OR worker_lease_until<now()) RETURNING id`, op.ID, token).Scan(&leased)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Error(codes.Aborted, "original operation is busy or no longer eligible")
	}
	if err != nil {
		return nil, dbError(err)
	}
	defer func() {
		releaseCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		_, _ = s.Store.DB().ExecContext(releaseCtx, `UPDATE workspace.operations SET worker_lease_token=NULL,worker_lease_until=NULL WHERE id=$1 AND worker_lease_token=$2`, op.ID, token)
	}()
	// Deletion accepts its intent under this same Workspace row lock. Retain it
	// through the one Deploy and checkpoint so closing cannot race this replay.
	guard, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer guard.Rollback()
	var workspaceState, tenant, activeOperation string
	var closed, fulfilled, deleting bool
	err = guard.QueryRowContext(ctx, `SELECT status,tenant_id,COALESCE(active_operation_id,''),deleted_at IS NOT NULL,
  EXISTS(SELECT 1 FROM workspace.subscriptions WHERE workspace_id=w.id),
  EXISTS(SELECT 1 FROM workspace.operations WHERE resource_id=w.id AND kind='delete_workspace')
  FROM workspace.workspaces w WHERE id=$1 FOR UPDATE`, op.ResourceID).Scan(&workspaceState, &tenant, &activeOperation, &closed, &fulfilled, &deleting)
	if err != nil {
		return nil, dbError(err)
	}
	op, err = s.Store.ReadOperation(ctx, op.ID)
	if err != nil {
		return nil, dbError(err)
	}
	if err = initialRuntimeRetryOrder(op); err != nil {
		return nil, err
	}
	if op.WorkerLeaseToken != token || !op.WorkerLeaseUntil.After(time.Now()) {
		return nil, status.Error(codes.Aborted, "original operation lease was lost")
	}
	if tenant != op.TenantID || activeOperation != op.ID || closed || fulfilled || deleting || (workspaceState != "provisioning" && workspaceState != "needs_attention") {
		return nil, status.Error(codes.FailedPrecondition, "Workspace is no longer an unfulfilled initial delivery")
	}
	command, result, err := s.initialRuntimeRetryCommand(op)
	if err != nil {
		return nil, err
	}
	// Revalidate the human permission after waiting for the owner locks.
	if err = s.authorizeInitialRuntimeRetry(ctx, request); err != nil {
		return nil, err
	}
	step, repeated, err := s.recordInitialRuntimeRetry(ctx, op, token, request, command)
	if err != nil {
		return nil, err
	}
	if repeated {
		return operation(op)
	}
	// Deploy may add its own confirmed Secret fields to its input. Never allow
	// those to replace Workspace's stored, unconfirmed handover or command bytes.
	observed, err := s.Serve.Deploy(ctx, proto.Clone(command).(*api.RuntimeDeployCommand))
	if err != nil {
		return nil, s.failedCall(ctx, op, token, step, "runtime", result, err)
	}
	if err = validateRuntimeReadback(command, observed); err != nil {
		return nil, s.failedCall(ctx, op, token, step, "runtime", result, err)
	}
	result.RuntimeDeployAccepted = true
	result.RuntimeReadback = wire(observed)
	stage, state, outcome := "runtime", "awaiting_confirmation", "unknown"
	if observed.GetOutcome() == api.Observation_OBSERVATION_CONFIRMED {
		outcome = "confirmed"
	}
	if observed.GetOutcome() == api.Observation_OBSERVATION_REJECTED || observed.GetState() == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED {
		state, outcome = "needs_attention", "rejected"
	} else if observed.GetState() == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
		stage = "activation"
	}
	if err = s.checkpoint(ctx, op, token, step, stage, state, outcome, command.GetRuntimeInstanceId(), "", result); err != nil {
		return nil, err
	}
	updated, err := s.Store.ReadOperation(ctx, op.ID)
	if err != nil {
		return nil, dbError(err)
	}
	answer, err := operation(updated)
	if err != nil {
		return nil, err
	}
	if outcome == "rejected" {
		return answer, status.Error(codes.FailedPrecondition, "Serve rejected the original runtime")
	}
	return answer, nil
}

// ValidateInitialRuntimeRetry validates the operator reconcile request shared by
// the local CLI decoder and the Workspace owner command.
func ValidateInitialRuntimeRetry(r *api.ReconcileOperationRpcRequest) error {
	c := r.GetContext()
	if r.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE || c.GetScope().GetPlatform() == nil || c.GetAcceptedOperationGrantId() != "" || c.GetAuthorizationContextId() != "" {
		return status.Error(codes.InvalidArgument, "a platform operator session is required")
	}
	for _, field := range []struct {
		value string
		limit int
	}{{r.GetOperationId(), 256}, {c.GetActorId(), 256}, {c.GetRequestId(), 256}, {c.GetIdempotencyKey(), 256}, {r.GetBody().GetReason(), 1024}} {
		value, limit := field.value, field.limit
		if value == "" || strings.TrimSpace(value) != value || len(value) > limit || !utf8.ValidString(value) || strings.ContainsAny(value, "\x00\r\n") || (c.GetSessionId() != "" && strings.Contains(value, c.GetSessionId())) {
			return status.Error(codes.InvalidArgument, "invalid runtime retry identity or reason")
		}
	}
	if c.GetSessionId() == "" || len(c.GetSessionId()) > 4096 || strings.TrimSpace(c.GetSessionId()) != c.GetSessionId() || !utf8.ValidString(c.GetSessionId()) || strings.ContainsAny(c.GetSessionId(), "\x00\r\n") {
		return status.Error(codes.InvalidArgument, "operator session reference is required")
	}
	return nil
}

func (s *Service) authorizeInitialRuntimeRetry(ctx context.Context, r *api.ReconcileOperationRpcRequest) error {
	c := r.GetContext()
	auth := &api.AuthorizationRequest{ActorId: c.ActorId, SessionId: c.SessionId, Scope: c.Scope, RequestId: c.RequestId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RECONCILEOPERATION,
		Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION, Id: proto.String(r.OperationId)}}
	decision, err := s.Identity.AuthorizeAction(ctx, auth)
	if err != nil {
		return err
	}
	if owneridentity.ValidateDecision(auth, decision, time.Now()) != nil || decision.GetAuthorizationContextId() == "" {
		return status.Error(codes.PermissionDenied, "fresh operator reconciliation authorization required")
	}
	return nil
}

func initialRuntimeRetryOrder(op ownerstore.Operation) error {
	if op.Terminal() || op.Kind != "create_workspace" || op.Stage != "runtime" || op.TenantID == "" || op.ActorID == "" {
		return status.Error(codes.FailedPrecondition, "operation is not an open initial runtime delivery")
	}
	return nil
}

func (s *Service) initialRuntimeRetryCommand(op ownerstore.Operation) (*api.RuntimeDeployCommand, orderResult, error) {
	a, r, err := decodeOrder(op)
	if err != nil {
		return nil, r, err
	}
	fail := func() (*api.RuntimeDeployCommand, orderResult, error) {
		return nil, r, status.Error(codes.DataLoss, "original runtime delivery evidence is inconsistent")
	}
	offer, accepted := &api.QuoteAcceptance{}, &api.QuoteAcceptance{}
	if protojson.Unmarshal(a.Quote, offer) != nil || offer.GetQuote() == nil || protojson.Unmarshal(r.Acceptance, accepted) != nil || accepted.GetQuote() == nil {
		return fail()
	}
	if err = validateAcceptance(op, offer, accepted); err != nil {
		return nil, r, err
	}
	if r.GrantID == "" || r.FabricOperationID == "" || len(r.RuntimeBinding) == 0 || len(r.ApplicationSource) == 0 {
		return fail()
	}
	resources := &api.ResourceReadback{}
	if protojson.Unmarshal(r.ResourceReadback, resources) != nil {
		return fail()
	}
	commit, err := evidence(op)
	if err != nil {
		return nil, r, err
	}
	binding, err := s.runtimeBinding(op, accepted, resources, &r, commit)
	if err != nil {
		return nil, r, err
	}
	record := &sourceRecord{}
	if json.Unmarshal(r.ApplicationSource, record) != nil || (record.Kind != "agent" && record.Kind != "opl_app") {
		return fail()
	}
	source, err := record.resolve()
	if err != nil {
		return nil, r, err
	}
	if err = s.revalidateApplicationSource(accepted, source); err != nil {
		return nil, r, err
	}
	descriptor, err := publicjson.Marshal(source.DeploymentDescriptor)
	if err != nil {
		return fail()
	}
	digest := sha256.Sum256(descriptor)
	if source.DescriptorDigest != "sha256:"+hex.EncodeToString(digest[:]) || !proto.Equal(source.Artifact, source.DeploymentDescriptor.GetArtifact()) {
		return fail()
	}
	reserve := &api.RuntimeReservationCommand{WorkspaceId: op.ResourceID, ApplicationSelection: source.Selection, Artifact: source.Artifact,
		DeploymentDescriptor: source.DeploymentDescriptor, DeploymentDescriptorDigest: source.DescriptorDigest, DeploymentDescriptorObjectRef: source.DescriptorObjectRef,
		ResourceSetId: resources.ResourceSetId, DataAttachmentId: binding.DataAttachmentId}
	reservation := &api.RuntimeReservation{}
	if protojson.Unmarshal(r.RuntimeReservation, reservation) != nil {
		return fail()
	}
	if err = validateRuntimeReservation(reserve, reservation); err != nil {
		return nil, r, err
	}
	expected := runtimeCommand(op, r.GrantID, accepted, source, binding, resources.ResourceSetId, reservation)
	revision, err := commandRevision(expected)
	if err != nil {
		return nil, r, err
	}
	credential, declared := contracts.WorkspaceApplicationDeclaredCredential(revision, contracts.WorkspaceApplicationCredentialGatewayKey)
	if declared {
		handover := &api.RuntimeManagedKeyBinding{}
		if protojson.Unmarshal(r.ManagedKeyBinding, handover) != nil || handover.GetKeyBindingId() == "" || handover.GetFingerprint() == "" ||
			handover.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(op.ResourceID) || handover.GetTargetSlot() != credential.Name ||
			handover.GetSecretBindingId() != "" || handover.GetSecretVersion() != "" {
			return fail()
		}
		expected.ManagedKeyBinding = handover
	} else if len(r.ManagedKeyBinding) > 0 {
		return fail()
	}
	command := &api.RuntimeDeployCommand{}
	if protojson.Unmarshal(r.RuntimeCommand, command) != nil || !proto.Equal(command, expected) {
		return fail()
	}
	if len(r.RuntimeReadback) > 0 {
		readback := &api.RuntimeReadback{}
		if protojson.Unmarshal(r.RuntimeReadback, readback) != nil {
			return fail()
		}
		if err = validateRuntimeReadback(command, readback); err != nil {
			return nil, r, err
		}
		if readback.GetFirstFulfilledAt() != nil || readback.GetNeverDeliveredConfirmed() || readback.GetState() == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
			return nil, r, status.Error(codes.FailedPrecondition, "runtime was already fulfilled or retired")
		}
	}
	return command, r, nil
}

type initialRuntimeRetryAck struct {
	CommandDigest string `json:"commandDigest"`
	ActorID       string `json:"actorId"`
	RequestID     string `json:"requestId"`
	Reason        string `json:"reason"`
}

func (s *Service) recordInitialRuntimeRetry(ctx context.Context, op ownerstore.Operation, token string, request *api.ReconcileOperationRpcRequest, command *api.RuntimeDeployCommand) (string, bool, error) {
	// Only the command digest enters the ack, never its grant/Secret references
	// or the operator session. Different human requests cannot retarget this key.
	canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(command)
	if err != nil {
		return "", false, status.Error(codes.DataLoss, "cannot digest original command")
	}
	digest := sha256.Sum256(canonical)
	ack := initialRuntimeRetryAck{CommandDigest: "sha256:" + hex.EncodeToString(digest[:]), ActorID: request.Context.ActorId, RequestID: request.Context.RequestId, Reason: request.Body.Reason}
	step := derivedID("retry_initial_runtime_", op.ID, ack.ActorID, request.Context.IdempotencyKey)
	tx, err := s.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return "", false, dbError(err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE workspace.operations SET updated_at=now() WHERE id=$1 AND worker_lease_token=$2 AND worker_lease_until>now() AND status NOT IN ('succeeded','failed','cancelled')`, op.ID, token)
	if err = fenced(res, err); err != nil {
		return "", false, err
	}
	var raw []byte
	var observation string
	err = tx.QueryRowContext(ctx, `SELECT input_snapshot,COALESCE(observation_result,'') FROM workspace.saga_steps WHERE operation_id=$1 AND step_key=$2 FOR UPDATE`, op.ID, step).Scan(&raw, &observation)
	if err == nil {
		var prior initialRuntimeRetryAck
		if json.Unmarshal(raw, &prior) != nil || prior.CommandDigest != ack.CommandDigest || prior.ActorID != ack.ActorID || prior.Reason != ack.Reason {
			return "", false, status.Error(codes.AlreadyExists, "retry key has different acknowledged input")
		}
		if observation == "confirmed" {
			return step, true, nil
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", false, dbError(err)
	}
	raw, _ = json.Marshal(ack)
	_, err = tx.ExecContext(ctx, `INSERT INTO workspace.saga_steps(id,operation_id,step_key,sequence,target_owner,command_id,idempotency_key,input_snapshot,observation_result,attempt_count)
  VALUES($1,$2,$3,7,'serve',$4,$4,$5,'unknown',1) ON CONFLICT(operation_id,step_key) DO UPDATE SET attempt_count=workspace.saga_steps.attempt_count+1,observation_result='unknown',confirmed_at=NULL,next_attempt_at=NULL,updated_at=now()`, id("step_"), op.ID, step, op.ID+":"+step, raw)
	if err != nil {
		return "", false, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return "", false, dbError(err)
	}
	return step, false, nil
}
