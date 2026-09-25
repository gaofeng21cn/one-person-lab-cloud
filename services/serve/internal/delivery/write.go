package delivery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// RuntimeAdapter executes the already admitted descriptor through the existing
// provider port. Resource bindings are Fabric readbacks, never caller input.
type RuntimeAdapter interface {
	Start(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) (RuntimeObservation, error)
	Observe(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) (RuntimeObservation, error)
}

type reservationInput struct {
	Request                json.RawMessage `json:"request"`
	Actor                  string          `json:"actor"`
	Scope                  json.RawMessage `json:"scope"`
	AuthorizationContextID string          `json:"authorizationContextId,omitempty"`
	Digest                 string          `json:"digest"`
}

func wire(m proto.Message) []byte {
	b, _ := protojson.MarshalOptions{UseProtoNames: true}.Marshal(m)
	return b
}
func stableID(prefix string, parts ...string) string {
	raw, _ := json.Marshal(parts)
	sum := sha256.Sum256(raw)
	return prefix + hex.EncodeToString(sum[:16])
}
func requirePeer(ctx context.Context, want owneridentity.Owner) error {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || peer != want.Service() {
		return status.Error(codes.PermissionDenied, "calling owner is not admitted")
	}
	return nil
}
func lockWorkspace(ctx context.Context, tx *sql.Tx, workspace string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "serve-workspace:"+workspace)
	return err
}
func reservationBytes(r *api.RuntimeReservationCommand) []byte {
	clean := proto.Clone(r).(*api.RuntimeReservationCommand)
	clean.Context = nil
	return wire(clean)
}
func nextOwnerCall(c *api.CallContext) *api.CallContext {
	v := proto.Clone(c).(*api.CallContext)
	v.AuthorizationContextId = ""
	return v
}

func deployBytes(r *api.RuntimeDeployCommand) []byte {
	clean := proto.Clone(r).(*api.RuntimeDeployCommand)
	clean.Context = nil
	return wire(clean)
}

// Reserve is the first-delivery admission. Serve allocates every delivery
// identity; a retry completes the same original claim even if Bind lost its reply.
func (s *Service) Reserve(ctx context.Context, r *api.RuntimeReservationCommand) (*api.RuntimeReservation, error) {
	if err := requirePeer(ctx, owneridentity.Workspace); err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME, r.GetWorkspaceId()); err != nil {
		return nil, err
	}
	call := r.GetContext()
	tid := call.GetScope().GetTenant().GetTenantId()
	if tid == "" || call.GetIdempotencyKey() == "" || r.GetDeploymentId() != "" || r.GetCapabilityVersionId() == "" || r.GetResourceSetId() == "" || r.GetDataAttachmentId() == "" || r.GetDeploymentDescriptor() == nil || r.GetDeploymentDescriptorObjectRef() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant, idempotency key, capability, resource set, attachment and descriptor are required; Serve allocates deployment identity")
	}
	expected, err := descriptorDigest(r.GetDeploymentDescriptor())
	if err != nil || expected != r.GetDeploymentDescriptorDigest() || !proto.Equal(r.GetArtifact(), r.GetDeploymentDescriptor().GetArtifact()) {
		return nil, status.Error(codes.InvalidArgument, "descriptor identity differs from its artifact or digest")
	}
	if s.Capability == nil || s.References == nil {
		return nil, status.Error(codes.Unavailable, "Capability is not configured")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()
	if err = lockWorkspace(ctx, tx, r.WorkspaceId); err != nil {
		return nil, dbError(err)
	}
	input := ownerstore.IdempotencyInput{ID: stableID("idem_", tid, call.ActorId, call.IdempotencyKey), TenantScope: tid, ActorScope: call.ActorId, OperationName: "ReserveRuntime", IdempotencyKey: call.IdempotencyKey, RequestSHA256: ownerstore.HashRequestBody(reservationBytes(r))}
	previous, found, err := s.Store.LookupIdempotency(ctx, tx, input)
	if errors.Is(err, ownerstore.ErrIdempotencyConflict) {
		return nil, status.Error(codes.AlreadyExists, "reservation key has different input")
	}
	if err != nil {
		return nil, dbError(err)
	}
	out := &api.RuntimeReservation{}
	if found {
		if err = protojson.Unmarshal(previous.ResponseBody, out); err != nil {
			return nil, status.Error(codes.DataLoss, "invalid reservation replay")
		}
		if err = tx.Commit(); err != nil {
			return nil, dbError(err)
		}
		return s.bindReservation(ctx, call, out)
	}
	// Recheck persisted ownership under the same lock used for creation, preventing
	// two tenants racing to reserve the same opaque Workspace identity.
	var owner string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(o.tenant_id,'') FROM serve.agent_deployments d JOIN serve.operations o ON o.id=d.operation_id WHERE d.workspace_id=$1 ORDER BY d.execution_epoch DESC LIMIT 1`, r.WorkspaceId).Scan(&owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, dbError(err)
	}
	if owner != "" && owner != tid {
		return nil, status.Error(codes.PermissionDenied, "workspace belongs to another tenant")
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.agent_deployments WHERE workspace_id=$1)`, r.WorkspaceId).Scan(&exists); err != nil {
		return nil, dbError(err)
	}
	if exists {
		return nil, status.Error(codes.FailedPrecondition, "Workspace already has a delivery; replacement requires explicit version-switch coordination")
	}
	if err = s.authorize(ctx, call, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME, r.WorkspaceId); err != nil {
		return nil, err
	}
	peerCall := nextOwnerCall(call)
	version, err := s.Capability.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: peerCall, CapabilityVersionId: r.CapabilityVersionId})
	if err != nil {
		return nil, err
	}
	if version.GetId() != r.CapabilityVersionId || version.GetStatus() != api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY || !proto.Equal(version.GetArtifact(), r.Artifact) || !proto.Equal(version.GetDeploymentDescriptor(), r.DeploymentDescriptor) || version.GetDeploymentDescriptorDigest() != expected || version.GetDeploymentDescriptorObjectRef() != r.DeploymentDescriptorObjectRef {
		return nil, status.Error(codes.FailedPrecondition, "Capability did not confirm the exact deployable descriptor")
	}
	deploymentID := stableID("dep_", tid, r.WorkspaceId, call.ActorId, call.IdempotencyKey)
	operationID := stableID("op_", deploymentID)
	runtimeID := stableID("rti_", deploymentID)
	claim, err := s.References.AcquireReference(ctx, &api.ReferenceClaimRequest{Context: peerCall, Target: &api.ReferenceTarget{Target: &api.ReferenceTarget_CapabilityVersionId{CapabilityVersionId: r.CapabilityVersionId}}, ClaimantOwner: api.OwnerEnum_OWNER_ENUM_SERVE, ClaimantResourceId: deploymentID})
	if err != nil {
		return nil, err
	}
	if claim.GetId() == "" || claim.GetClaimantOwner() != api.OwnerEnum_OWNER_ENUM_SERVE || claim.GetClaimantResourceId() != deploymentID || claim.GetTarget().GetCapabilityVersionId() != r.CapabilityVersionId || claim.GetState() == api.ReferenceClaimState_REFERENCE_CLAIM_STATE_RELEASED {
		return nil, status.Error(codes.FailedPrecondition, "Capability claim identity mismatch")
	}
	accepted, _ := json.Marshal(reservationInput{Request: reservationBytes(r), Actor: call.ActorId, Scope: wire(call.Scope), AuthorizationContextID: call.GetAuthorizationContextId(), Digest: "sha256:" + input.RequestSHA256})
	_, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operationID, TenantID: tid, ActorID: call.ActorId, Kind: "runtime_deploy", ResourceID: deploymentID, Stage: "runtime", RequestID: call.RequestId, AcceptedInput: accepted})
	if err != nil {
		return nil, dbError(err)
	}
	compatibility, _ := publicjson.Marshal(version.GetDataCompatibility())
	if string(compatibility) == "null" || len(compatibility) == 0 {
		compatibility = []byte(`{}`)
	}
	descriptor, _ := publicjson.Marshal(r.DeploymentDescriptor)
	attachment, _ := json.Marshal(map[string]string{"attachmentId": r.DataAttachmentId})
	_, err = tx.ExecContext(ctx, `INSERT INTO serve.agent_deployments(id,workspace_id,capability_version_id,artifact_digest,reference_claim_id,runtime_instance_id,operation_id,status,data_compatibility,execution_epoch) VALUES($1,$2,$3,$4,$5,$6,$7,'queued',$8,1)`, deploymentID, r.WorkspaceId, r.CapabilityVersionId, r.Artifact.Digest, claim.Id, runtimeID, operationID, compatibility)
	if err != nil {
		return nil, dbError(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO serve.agent_runtime_instances(id,workspace_id,deployment_id,artifact_digest,fabric_resource_set_id,status,data_attachment_contract,execution_epoch,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref) VALUES($1,$2,$3,$4,$5,'pending',$6,1,$7,$8,$9)`, runtimeID, r.WorkspaceId, deploymentID, r.Artifact.Digest, r.ResourceSetId, attachment, descriptor, expected, r.DeploymentDescriptorObjectRef)
	if err != nil {
		return nil, dbError(err)
	}
	out = &api.RuntimeReservation{RuntimeInstanceId: runtimeID, WorkspaceId: r.WorkspaceId, DeploymentId: deploymentID, Artifact: r.Artifact, DeploymentDescriptorDigest: expected, DeploymentDescriptorObjectRef: r.DeploymentDescriptorObjectRef, ExecutionEpoch: 1, OperationId: operationID}
	input.ResourceID = deploymentID
	input.OperationID = operationID
	input.ResponseStatus = 201
	input.ResponseBody = wire(out)
	if err = s.Store.RecordIdempotency(ctx, tx, input); err != nil {
		return nil, dbError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return s.bindReservation(ctx, call, out)
}

func (s *Service) bindReservation(ctx context.Context, call *api.CallContext, out *api.RuntimeReservation) (*api.RuntimeReservation, error) {
	evidence, claim, err := s.ownerEvidence(ctx, out.DeploymentId)
	if err != nil {
		return nil, err
	}
	bound, err := s.References.BindReference(ctx, &api.BindReferenceRequest{Context: nextOwnerCall(call), ClaimId: claim, OwnerCommitEvidence: evidence})
	if err != nil {
		return nil, err
	}
	if bound.GetId() != claim || bound.GetState() != api.ReferenceClaimState_REFERENCE_CLAIM_STATE_BOUND || bound.GetBoundOperationId() != evidence.OperationId || bound.GetBoundInputDigest() != evidence.AcceptedInputDigest {
		return nil, status.Error(codes.FailedPrecondition, "Capability did not confirm original claim binding")
	}
	return out, nil
}

func (s *Service) ownerEvidence(ctx context.Context, deployment string) (*api.OwnerCommitEvidence, string, error) {
	var op, claim string
	var accepted []byte
	var created time.Time
	err := s.DB.QueryRowContext(ctx, `SELECT d.operation_id,d.reference_claim_id,o.accepted_input,o.created_at FROM serve.agent_deployments d JOIN serve.operations o ON o.id=d.operation_id WHERE d.id=$1`, deployment).Scan(&op, &claim, &accepted, &created)
	if err != nil {
		return nil, "", dbError(err)
	}
	var in reservationInput
	var req api.RuntimeReservationCommand
	var scope api.AuthorizationScope
	if json.Unmarshal(accepted, &in) != nil || protojson.Unmarshal(in.Request, &req) != nil || protojson.Unmarshal(in.Scope, &scope) != nil || in.Digest == "" {
		return nil, "", status.Error(codes.DataLoss, "invalid persisted reservation evidence")
	}
	e := &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_SERVE, OperationId: op, ResourceId: deployment, AcceptedInputDigest: in.Digest, CommittedVersion: 1, AcceptedAt: timestamppb.New(created), ActorId: in.Actor, Scope: &scope, AcceptedAction: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME, AuthorizationResource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(req.WorkspaceId)}, ContinuationResources: []*api.AuthorizationResource{{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String(req.CapabilityVersionId)}}}
	if in.AuthorizationContextID != "" {
		e.AuthorizationContextId = in.AuthorizationContextID
	}
	return e, claim, nil
}
func (s *Service) ReadOwnerCommit(ctx context.Context, r *api.ReadOwnerCommitRequest) (*api.OwnerCommitEvidence, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || (peer != owneridentity.Capability.Service() && peer != owneridentity.Tenant.Service()) {
		return nil, status.Error(codes.PermissionDenied, "owner commit reader not admitted")
	}
	e, _, err := s.ownerEvidence(ctx, r.GetResourceId())
	if err != nil {
		return nil, err
	}
	if r.GetOwner() != api.OwnerEnum_OWNER_ENUM_SERVE || r.GetOperationId() != e.OperationId {
		return nil, status.Error(codes.FailedPrecondition, "original owner commit identity mismatch")
	}
	return e, nil
}
func (s *Service) ReadClaimUsage(ctx context.Context, r *api.ReadClaimUsageRequest) (*api.ClaimUsageEvidence, error) {
	if err := requirePeer(ctx, owneridentity.Capability); err != nil {
		return nil, err
	}
	e, claim, err := s.ownerEvidence(ctx, r.GetClaimantResourceId())
	if err != nil {
		return nil, err
	}
	if r.GetClaimId() != claim || r.GetClaimantOperationId() != e.OperationId {
		return nil, status.Error(codes.FailedPrecondition, "claim usage identity mismatch")
	}
	var state string
	var epoch int64
	var digest, ref string
	err = s.DB.QueryRowContext(ctx, `SELECT status,execution_epoch,deployment_descriptor_digest,deployment_descriptor_object_ref FROM serve.agent_runtime_instances WHERE deployment_id=$1`, r.ClaimantResourceId).Scan(&state, &epoch, &digest, &ref)
	if err != nil {
		return nil, dbError(err)
	}
	// Failed starts are still required until an adapter proves termination/absence.
	return &api.ClaimUsageEvidence{ClaimId: claim, Owner: api.OwnerEnum_OWNER_ENUM_SERVE, ResourceId: r.ClaimantResourceId, OperationId: e.OperationId, AcceptedInputDigest: e.AcceptedInputDigest, OperationStatus: api.OperationStatusEnum_OPERATION_STATUS_ENUM_RUNNING, ActivelyRequired: true, CommittedVersion: 1, Outcome: api.Observation_OBSERVATION_CONFIRMED, ObservedAt: timestamppb.Now(), DeploymentDescriptorDigest: digest, ExecutionEpoch: epoch, DeploymentDescriptorObjectRef: ref}, nil
}

func (s *Service) Deploy(ctx context.Context, r *api.RuntimeDeployCommand) (*api.RuntimeReadback, error) {
	if err := requirePeer(ctx, owneridentity.Workspace); err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME, r.GetWorkspaceId()); err != nil {
		return nil, err
	}
	if s.Runtime == nil || s.Resources == nil || s.References == nil {
		return nil, status.Error(codes.Unavailable, "runtime adapter, Fabric readback and Capability must be configured")
	}
	if err := s.acceptDeploy(ctx, r); err != nil {
		return nil, err
	}
	if _, err := s.bindReservation(ctx, r.Context, &api.RuntimeReservation{DeploymentId: r.DeploymentId}); err != nil {
		return nil, err
	}
	resources, err := s.Resources.ReadResources(ctx, &api.ResourceReadbackRequest{Context: nextOwnerCall(r.Context), ResourceSetId: r.ResourceSetId})
	if err != nil {
		return nil, err
	}
	b, err := confirmedBinding(r, resources)
	if err != nil {
		return nil, err
	}
	// Start is idempotent at the original provider operation. Its acknowledgement
	// never proves readiness: a separate live Observe must confirm it.
	if _, err = s.Runtime.Start(ctx, r, b); err != nil {
		return nil, err
	}
	observed, err := s.Runtime.Observe(ctx, r, b)
	if err != nil {
		return nil, err
	}
	if _, err = s.RecordDeploymentObservation(ctx, r, observed); err != nil {
		return nil, err
	}
	if err = s.finishFirstDelivery(ctx, r, observed); err != nil {
		return nil, err
	}
	return s.runtimeReadback(ctx, r.RuntimeInstanceId, r.DeploymentId)
}

func (s *Service) acceptDeploy(ctx context.Context, r *api.RuntimeDeployCommand) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	if err = lockWorkspace(ctx, tx, r.WorkspaceId); err != nil {
		return dbError(err)
	}
	if err = validateReserved(ctx, tx, r); err != nil {
		return err
	}
	if err = s.authorize(ctx, r.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME, r.WorkspaceId); err != nil {
		return err
	}
	key := stableID("start_", r.DeploymentId)
	var prior []byte
	err = tx.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE command_id=$1`, key).Scan(&prior)
	input := deployBytes(r)
	if err == nil {
		var old api.RuntimeDeployCommand
		if protojson.Unmarshal(prior, &old) != nil || !proto.Equal(&old, func() *api.RuntimeDeployCommand {
			v := proto.Clone(r).(*api.RuntimeDeployCommand)
			v.Context = nil
			return v
		}()) {
			return status.Error(codes.AlreadyExists, "deployment execution input differs from its original command")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return dbError(err)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO serve.agent_runtime_actions(id,runtime_instance_id,command_id,action,expected_deployment_id,input_snapshot,observation_result) VALUES($1,$2,$1,'start',$3,$4,'unknown')`, key, r.RuntimeInstanceId, r.DeploymentId, input)
		if err != nil {
			return dbError(err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE serve.agent_deployments SET status=CASE WHEN status='active' THEN status ELSE 'deploying' END,updated_at=now() WHERE id=$1`, r.DeploymentId)
	if err != nil {
		return dbError(err)
	}
	return dbError(tx.Commit())
}
func (s *Service) finishFirstDelivery(ctx context.Context, r *api.RuntimeDeployCommand, o RuntimeObservation) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return dbError(err)
	}
	defer tx.Rollback()
	if err = lockWorkspace(ctx, tx, r.WorkspaceId); err != nil {
		return dbError(err)
	}
	if err = validateReserved(ctx, tx, r); err != nil {
		return err
	}
	var recordedStatus, recordedEvidence string
	var recordedAt time.Time
	if err = tx.QueryRowContext(ctx, `SELECT status,COALESCE(readiness_evidence_ref,''),observed_at FROM serve.agent_runtime_instances WHERE id=$1 FOR UPDATE`, r.RuntimeInstanceId).Scan(&recordedStatus, &recordedEvidence, &recordedAt); err != nil {
		return dbError(err)
	}
	if recordedAt.Sub(o.ObservedAt.UTC()) >= time.Microsecond || o.ObservedAt.UTC().Sub(recordedAt) >= time.Microsecond || recordedEvidence != o.ReadinessEvidenceRef {
		return refuse(ReasonStaleEpoch)
	}
	state := "verifying"
	if o.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY && recordedStatus == "ready" {
		state = "active"
	}
	_, err = tx.ExecContext(ctx, `UPDATE serve.agent_deployments SET status=$2,verification_evidence_ref=NULLIF($3,''),activated_at=CASE WHEN $2='active' THEN COALESCE(activated_at,now()) ELSE activated_at END,updated_at=now() WHERE id=$1`, r.DeploymentId, state, o.ReadinessEvidenceRef)
	if err != nil {
		return dbError(err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE serve.agent_runtime_actions SET observation_result='confirmed',evidence_ref=NULLIF($2,''),updated_at=now() WHERE command_id=$1`, stableID("start_", r.DeploymentId), o.ReadinessEvidenceRef)
	if err != nil {
		return dbError(err)
	}
	if state == "active" {
		_, err = tx.ExecContext(ctx, `UPDATE serve.operations SET status='succeeded',stage='verify',observation_result='confirmed',completed_at=COALESCE(completed_at,now()),updated_at=now() WHERE id=(SELECT operation_id FROM serve.agent_deployments WHERE id=$1)`, r.DeploymentId)
		if err != nil {
			return dbError(err)
		}
	}
	return dbError(tx.Commit())
}
func (s *Service) ReadRuntime(ctx context.Context, r *api.RuntimeReadbackRequest) (*api.RuntimeReadback, error) {
	if err := requirePeer(ctx, owneridentity.Workspace); err != nil {
		return nil, err
	}
	var workspace string
	if err := s.DB.QueryRowContext(ctx, `SELECT workspace_id FROM serve.agent_runtime_instances WHERE id=$1 AND deployment_id=$2`, r.GetRuntimeInstanceId(), r.GetDeploymentId()).Scan(&workspace); err != nil {
		return nil, dbError(err)
	}
	if err := s.authorize(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME, workspace); err != nil {
		return nil, err
	}
	var raw []byte
	err := s.DB.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE command_id=$1`, stableID("start_", r.DeploymentId)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return s.runtimeReadback(ctx, r.RuntimeInstanceId, r.DeploymentId)
	}
	if err != nil {
		return nil, dbError(err)
	}
	if s.Runtime == nil || s.Resources == nil {
		return nil, status.Error(codes.Unavailable, "runtime observation dependencies are unavailable")
	}
	command := &api.RuntimeDeployCommand{}
	if protojson.Unmarshal(raw, command) != nil || command.RuntimeInstanceId != r.RuntimeInstanceId || command.DeploymentId != r.DeploymentId {
		return nil, status.Error(codes.DataLoss, "invalid original runtime command")
	}
	command.Context = r.Context
	resources, err := s.Resources.ReadResources(ctx, &api.ResourceReadbackRequest{Context: nextOwnerCall(r.Context), ResourceSetId: command.ResourceSetId})
	if err != nil {
		return nil, err
	}
	binding, err := confirmedBinding(command, resources)
	if err != nil {
		return nil, err
	}
	observation, err := s.Runtime.Observe(ctx, command, binding)
	if err != nil {
		return nil, err
	}
	if _, err = s.RecordDeploymentObservation(ctx, command, observation); err != nil {
		return nil, err
	}
	if err = s.finishFirstDelivery(ctx, command, observation); err != nil {
		return nil, err
	}
	return s.runtimeReadback(ctx, r.RuntimeInstanceId, r.DeploymentId)
}
func (s *Service) runtimeReadback(ctx context.Context, runtimeID, deployment string) (*api.RuntimeReadback, error) {
	var st, url, receipt, artifact, repository, digest, ref, workspace string
	var observed sql.NullTime
	var epoch, version int64
	err := s.DB.QueryRowContext(ctx, `SELECT workspace_id,status,COALESCE(access_url,''),COALESCE(readiness_evidence_ref,''),artifact_digest,deployment_descriptor#>>'{artifact,repository}',deployment_descriptor_digest,deployment_descriptor_object_ref,observed_at,execution_epoch,applied_model_configuration_version FROM serve.agent_runtime_instances WHERE id=$1 AND deployment_id=$2`, runtimeID, deployment).Scan(&workspace, &st, &url, &receipt, &artifact, &repository, &digest, &ref, &observed, &epoch, &version)
	if err != nil {
		return nil, dbError(err)
	}
	out := &api.RuntimeReadback{RuntimeInstanceId: runtimeID, WorkspaceId: workspace, DeploymentId: deployment, State: api.AgentRuntimeObservationState(api.AgentRuntimeObservationState_value["RUNTIME_INSTANCE_STATE_"+strings.ToUpper(st)]), ProcessReady: st == "ready", ApplicationAvailable: st == "ready", Artifact: &api.ArtifactReference{Repository: repository, Digest: artifact}, AppliedModelConfigurationVersion: version, ReadinessReceiptId: receipt, Outcome: api.Observation_OBSERVATION_UNKNOWN, DeploymentDescriptorDigest: digest, DeploymentDescriptorObjectRef: ref, ExecutionEpoch: epoch}
	if observed.Valid {
		out.ObservedAt = timestamppb.New(observed.Time)
		out.Outcome = api.Observation_OBSERVATION_CONFIRMED
	}
	if url != "" {
		out.AccessUrl = url
		out.ApplicationEntry = &api.WorkspaceApplicationEntry{Url: proto.String(url)}
	}
	return out, nil
}

func confirmedBinding(command *api.RuntimeDeployCommand, resources *api.ResourceReadback) (*api.ResourceExecutionBinding, error) {
	b := resources.GetExecutionResources()
	if resources.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || resources.GetResourceSetId() != command.ResourceSetId || resources.GetWorkspaceId() != command.WorkspaceId || resources.GetAbsenceConfirmed() || b.GetAccountId() == "" || b.GetComputeAllocationId() == "" || b.GetStorageVolumeId() == "" || b.GetDataAttachmentId() != command.DataAttachmentId || b.GetDataAttachmentOperationId() == "" {
		return nil, status.Error(codes.FailedPrecondition, "Fabric has not confirmed the exact executable resource binding")
	}
	return b, nil
}
