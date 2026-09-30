package delivery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// ExecutionTarget is the exact confirmed execution fact a delivery step runs
// against: the resource binding identity plus, when the resources owner publishes
// one, the infrastructure placement the application workload is scheduled onto.
// Both come from the same Fabric owner readback, so Serve never composes a
// placement of its own.
//
// The requirement to refuse an unconfirmed placement belongs to the executor that
// schedules the workload, not to this provider-neutral target: a cluster executor
// refuses a workload it cannot place onto the confirmed node, prepaid package and
// storage claim, while a boundary whose provider resolves its own placement needs
// none. Checking it here would refuse every provider whose resources are not
// scheduled onto a node, a package and a claim.
type ExecutionTarget struct {
	Binding   *api.ResourceExecutionBinding
	Placement *api.ApplicationExecutionPlacement
}

// RuntimeAdapter executes the already admitted descriptor through Serve's own
// execution boundary. Resource bindings and their placement are Fabric readbacks,
// never caller input.
type RuntimeAdapter interface {
	Start(context.Context, *api.RuntimeDeployCommand, ExecutionTarget) (RuntimeObservation, error)
	Observe(context.Context, *api.RuntimeDeployCommand, ExecutionTarget) (RuntimeObservation, error)
	// Lifecycle applies a desired lifecycle state (running, suspended, absent) to
	// the exact reserved runtime. It reports only what the provider confirmed.
	Lifecycle(context.Context, *api.RuntimeDeployCommand, ExecutionTarget, string) error
	// Reload applies the command's model configuration to the exact runtime.
	Reload(context.Context, *api.RuntimeDeployCommand, ExecutionTarget) error
	// Credentials reads the platform-issued WebUI credential for the exact runtime.
	Credentials(context.Context, *api.RuntimeDeployCommand, ExecutionTarget) (*api.WorkspaceApplicationCredentials, error)
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

// nextOwnerCall derives the call context of an owner-to-owner request. The peer's
// identity comes from the authenticated transport, so the caller's own
// authorization context is never forwarded. A frozen command persisted for a
// lifecycle action carries no call context by design, and the derived context is
// therefore an empty one rather than a panic.
func nextOwnerCall(c *api.CallContext) *api.CallContext {
	v := &api.CallContext{}
	if c != nil {
		v = proto.Clone(c).(*api.CallContext)
	}
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
	if tid == "" || call.GetIdempotencyKey() == "" || r.GetDeploymentId() != "" || r.GetResourceSetId() == "" || r.GetDataAttachmentId() == "" || r.GetDeploymentDescriptor() == nil || r.GetDeploymentDescriptorObjectRef() == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant, idempotency key, resource set, attachment and descriptor are required; Serve allocates deployment identity")
	}
	// Exactly one application source: the default OPL App names an approved Runtime
	// Release with no CapabilityVersion, while a built Agent names a
	// CapabilityVersion. The two never coexist and neither is implicit.
	selection := r.GetApplicationSelection()
	if selection == nil {
		if r.GetCapabilityVersionId() == "" {
			return nil, status.Error(codes.InvalidArgument, "an application selection or a capability version is required")
		}
		selection = &api.WorkspaceApplicationSelection{
			Kind:                api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT,
			CapabilityVersionId: proto.String(r.GetCapabilityVersionId()),
		}
	} else if r.GetCapabilityVersionId() != "" {
		return nil, status.Error(codes.InvalidArgument, "application selection and capability version are mutually exclusive")
	}
	if err := contracts.ValidateWorkspaceApplicationSelection(selection); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	expected, err := descriptorDigest(r.GetDeploymentDescriptor())
	if err != nil || expected != r.GetDeploymentDescriptorDigest() || !proto.Equal(r.GetArtifact(), r.GetDeploymentDescriptor().GetArtifact()) {
		return nil, status.Error(codes.InvalidArgument, "descriptor identity differs from its artifact or digest")
	}
	if s.References == nil {
		return nil, status.Error(codes.Unavailable, "Capability reference coordination is not configured")
	}
	if selection.GetKind() == api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT && s.Capability == nil {
		return nil, status.Error(codes.Unavailable, "Capability is not configured")
	}
	if selection.GetKind() == api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP && s.RuntimeReleases == nil {
		return nil, status.Error(codes.Unavailable, "Runtime Control is not configured")
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
	applicationKind := "agent"
	// The explicit application selection is the single source of the capability
	// version for a built Agent; the legacy top-level field is never combined with
	// it.
	capabilityVersionID := selection.GetCapabilityVersionId()
	runtimeVersionID := ""
	var dataCompatibility *api.DataCompatibility
	switch selection.GetKind() {
	case api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT:
		version, err := s.Capability.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: peerCall, CapabilityVersionId: capabilityVersionID})
		if err != nil {
			return nil, err
		}
		if version.GetId() != capabilityVersionID || version.GetStatus() != api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY || !proto.Equal(version.GetArtifact(), r.Artifact) || !proto.Equal(version.GetDeploymentDescriptor(), r.DeploymentDescriptor) || version.GetDeploymentDescriptorDigest() != expected || version.GetDeploymentDescriptorObjectRef() != r.DeploymentDescriptorObjectRef {
			return nil, status.Error(codes.FailedPrecondition, "Capability did not confirm the exact deployable descriptor")
		}
		dataCompatibility = version.GetDataCompatibility()
	case api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP:
		applicationKind = "opl_app"
		capabilityVersionID = ""
		release, err := s.runtimeRelease(ctx, peerCall, selection.GetRuntimeVersionId())
		if err != nil {
			return nil, err
		}
		runtimeVersionID = release.GetId()
		// The default App has no Build lineage; its descriptor must be the release's
		// own immutable OCI and application revision template, not a caller image.
		if !proto.Equal(release.GetPublisherContract().GetImage(), r.GetArtifact()) || !proto.Equal(release.GetPublisherContract().GetApplicationRevisionTemplate(), r.GetDeploymentDescriptor().GetApplicationRevision()) {
			return nil, status.Error(codes.FailedPrecondition, "Runtime Release does not match the default App descriptor")
		}
		// The default App's data contract is the release's own upgrade/rollback
		// contract, declared at admission; the runtime stores an empty compatibility
		// for the first delivery and reconciles it on replacement.
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown application selection kind")
	}
	deploymentID := stableID("dep_", tid, r.WorkspaceId, call.ActorId, call.IdempotencyKey)
	operationID := stableID("op_", deploymentID)
	runtimeID := stableID("rti_", deploymentID)
	claimTarget := &api.ReferenceTarget{}
	if applicationKind == "agent" {
		claimTarget.Target = &api.ReferenceTarget_CapabilityVersionId{CapabilityVersionId: capabilityVersionID}
	} else {
		claimTarget.Target = &api.ReferenceTarget_RuntimeVersionId{RuntimeVersionId: runtimeVersionID}
	}
	claim, err := s.References.AcquireReference(ctx, &api.ReferenceClaimRequest{Context: peerCall, Target: claimTarget, ClaimantOwner: api.OwnerEnum_OWNER_ENUM_SERVE, ClaimantResourceId: deploymentID})
	if err != nil {
		return nil, err
	}
	if claim.GetId() == "" || claim.GetClaimantOwner() != api.OwnerEnum_OWNER_ENUM_SERVE || claim.GetClaimantResourceId() != deploymentID || claim.GetState() == api.ReferenceClaimState_REFERENCE_CLAIM_STATE_RELEASED {
		return nil, status.Error(codes.FailedPrecondition, "reference claim identity mismatch")
	}
	if applicationKind == "agent" && claim.GetTarget().GetCapabilityVersionId() != capabilityVersionID {
		return nil, status.Error(codes.FailedPrecondition, "Capability claim identity mismatch")
	}
	if applicationKind == "opl_app" && claim.GetTarget().GetRuntimeVersionId() != runtimeVersionID {
		return nil, status.Error(codes.FailedPrecondition, "Runtime Release claim identity mismatch")
	}
	accepted, _ := json.Marshal(reservationInput{Request: reservationBytes(r), Actor: call.ActorId, Scope: wire(call.Scope), AuthorizationContextID: call.GetAuthorizationContextId(), Digest: "sha256:" + input.RequestSHA256})
	_, err = s.Store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: operationID, TenantID: tid, ActorID: call.ActorId, Kind: "runtime_deploy", ResourceID: deploymentID, Stage: "runtime", RequestID: call.RequestId, AcceptedInput: accepted})
	if err != nil {
		return nil, dbError(err)
	}
	compatibility, _ := publicjson.Marshal(dataCompatibility)
	if string(compatibility) == "null" || len(compatibility) == 0 {
		compatibility = []byte(`{}`)
	}
	descriptor, _ := publicjson.Marshal(r.DeploymentDescriptor)
	attachment, _ := json.Marshal(map[string]string{"attachmentId": r.DataAttachmentId})
	_, err = tx.ExecContext(ctx, `INSERT INTO serve.agent_deployments(id,workspace_id,capability_version_id,application_kind,runtime_version_id,artifact_digest,reference_claim_id,runtime_instance_id,operation_id,status,data_compatibility,execution_epoch) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'queued',$10,1)`, deploymentID, r.WorkspaceId, capabilityVersionID, applicationKind, nullable(runtimeVersionID), r.Artifact.Digest, claim.Id, runtimeID, operationID, compatibility)
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
	if err := s.resolveManagedKeyBinding(ctx, r); err != nil {
		return nil, err
	}
	if err := s.acceptDeploy(ctx, r); err != nil {
		return nil, err
	}
	if _, err := s.bindReservation(ctx, r.Context, &api.RuntimeReservation{DeploymentId: r.DeploymentId}); err != nil {
		return nil, err
	}
	return s.reconcileRuntime(ctx, r, true)
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
	if err = recordStartActionTx(ctx, tx, r); err != nil {
		return err
	}
	return dbError(tx.Commit())
}

// recordStartActionTx persists the frozen start command before any provider call,
// so every admitted delivery has exactly one original command that a later stop,
// reload, credential read or replacement resumes from. The command identity is
// deterministic, so a replay of the same delivery finds its own row instead of
// writing a second one, and a replay that presents different input is refused
// rather than silently re-targeting the provider call.
//
// It is shared by the Workspace-driven first delivery and Serve's own
// replacement, so a switched-to deployment is as recoverable as the first one.
func recordStartActionTx(ctx context.Context, tx *sql.Tx, r *api.RuntimeDeployCommand) error {
	key := stableID("start_", r.DeploymentId)
	var prior []byte
	err := tx.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE command_id=$1`, key).Scan(&prior)
	if err == nil {
		var old api.RuntimeDeployCommand
		expected := proto.Clone(r).(*api.RuntimeDeployCommand)
		expected.Context = nil
		if protojson.Unmarshal(prior, &old) != nil || !proto.Equal(&old, expected) {
			return status.Error(codes.AlreadyExists, "deployment execution input differs from its original command")
		}
	} else if !errors.Is(err, sql.ErrNoRows) {
		return dbError(err)
	} else {
		if _, err = tx.ExecContext(ctx, `INSERT INTO serve.agent_runtime_actions(id,runtime_instance_id,command_id,action,expected_deployment_id,input_snapshot,observation_result) VALUES($1,$2,$1,'start',$3,$4,'unknown')`, key, r.RuntimeInstanceId, r.DeploymentId, deployBytes(r)); err != nil {
			return dbError(err)
		}
	}
	// A delivery that is already the current application keeps its status; every
	// other admitted delivery is executing and is recorded as deploying.
	if _, err = tx.ExecContext(ctx, `UPDATE serve.agent_deployments SET status=CASE WHEN status='active' THEN status ELSE 'deploying' END,updated_at=now() WHERE id=$1`, r.DeploymentId); err != nil {
		return dbError(err)
	}
	return nil
}
func (s *Service) finishFirstDelivery(ctx context.Context, tx *sql.Tx, r *api.RuntimeDeployCommand, o RuntimeObservation) error {
	var err error
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
	// The Workspace has at most one current application, and Serve's own store
	// enforces exactly that. A replacement becomes current only by retiring the
	// delivery it replaced in this same transaction, so the invariant holds at
	// every statement boundary and the replaced delivery stays readable as
	// history instead of being deleted.
	if state == "active" {
		if _, err = tx.ExecContext(ctx, `UPDATE serve.agent_deployments SET status='superseded',updated_at=now() WHERE id=(SELECT previous_deployment_id FROM serve.agent_deployments WHERE id=$1) AND workspace_id=$2 AND status IN ('active','rolled_back')`, r.DeploymentId, r.WorkspaceId); err != nil {
			return dbError(err)
		}
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
		_, err = tx.ExecContext(ctx, `UPDATE serve.operations SET status='succeeded',stage='verification',observation_result='confirmed',completed_at=COALESCE(completed_at,now()),updated_at=now() WHERE id=(SELECT operation_id FROM serve.agent_deployments WHERE id=$1)`, r.DeploymentId)
		if err != nil {
			return dbError(err)
		}
		// Readiness alone does not expose the application. The deployment becomes
		// the Workspace's current route target in this same transaction, so the
		// access data plane starts serving exactly the instance whose readiness
		// was just recorded and no acknowledgement can be lost in between.
		if err = commitDeliveryRoute(ctx, tx, r, o.ReadinessEvidenceRef); err != nil {
			return err
		}
	}
	if err = appendReadinessEvent(ctx, tx, s.Store, r, o); err != nil {
		return dbError(err)
	}
	return nil
}

// appendReadinessEvent persists the exact runtime observation in Serve's
// owner-local Outbox. The deterministic aggregate identity makes a replay after
// a lost response append no second fact, while the existing delivery rows keep
// Ledger and Workspace acknowledgements independent.
func appendReadinessEvent(ctx context.Context, tx *sql.Tx, store *ownerstore.Store, r *api.RuntimeDeployCommand, o RuntimeObservation) error {
	tenantID := r.GetContext().GetScope().GetTenant().GetTenantId()
	if tenantID == "" || r.GetContext().GetRequestId() == "" || r.GetDeploymentId() == "" || r.GetRuntimeInstanceId() == "" || o.ObservedAt.IsZero() {
		return errors.New("runtime readiness event identity is incomplete")
	}
	outcome := "unknown"
	switch o.State {
	case api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY:
		outcome = "confirmed"
	case api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED,
		api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED:
		outcome = "rejected"
	}
	payload, err := protojson.Marshal(&api.RuntimeReadinessObservedEvent{
		RuntimeInstanceId:    r.GetRuntimeInstanceId(),
		WorkspaceId:          r.GetWorkspaceId(),
		DeploymentId:         r.GetDeploymentId(),
		Outcome:              outcome,
		ApplicationAvailable: o.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY,
		// Injection is proven only by a confirmed managed-key Secret binding that
		// the execution boundary was given; an application that declares no Secret
		// needs none, and absence of a binding is never treated as proof.
		CredentialInjectionVerified:      r.GetManagedKeyBinding() != nil && strings.TrimSpace(r.GetManagedKeyBinding().GetSecretBindingId()) != "" && strings.TrimSpace(r.GetManagedKeyBinding().GetSecretVersion()) != "",
		AppliedModelConfigurationVersion: o.AppliedModelConfigurationVersion,
		ReceiptId: func() *string {
			if o.ReadinessEvidenceRef == "" {
				return nil
			}
			v := o.ReadinessEvidenceRef
			return &v
		}(),
	})
	if err != nil {
		return err
	}
	const eventType = "serve.agent_readiness_observed.v1"
	receiptID := ""
	if o.ReadinessEvidenceRef != "" {
		receiptID = o.ReadinessEvidenceRef
	}
	eventID := stableID("evt_", eventType, r.GetDeploymentId(), r.GetRuntimeInstanceId(), strconv.FormatInt(r.GetExecutionEpoch(), 10), outcome, strconv.FormatBool(o.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY), strconv.FormatInt(r.GetModelConfigurationVersion(), 10), receiptID)
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM serve.outbox_events WHERE id=$1)`, eventID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	var revision int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(aggregate_revision), 0) + 1 FROM serve.outbox_events WHERE event_type=$1 AND aggregate_type=$2 AND aggregate_id=$3`, eventType, "agent_deployment", r.GetDeploymentId()).Scan(&revision); err != nil {
		return err
	}
	return store.AppendEvent(ctx, tx, ownerstore.Event{
		ID:        eventID,
		EventType: eventType, SchemaVersion: 1, AggregateType: "agent_deployment", AggregateID: r.GetDeploymentId(),
		AggregateRevision: revision, TenantID: tenantID, CorrelationID: r.GetContext().GetRequestId(),
		Payload: payload, OccurredAt: o.ObservedAt.UTC(),
	})
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
	return s.reconcileRuntime(ctx, command, false)
}
func (s *Service) runtimeReadback(ctx context.Context, runtimeID, deployment string) (*api.RuntimeReadback, error) {
	var st, url, receipt, digest, ref, workspace string
	var descriptorRaw []byte
	var observed sql.NullTime
	var epoch, version int64
	err := s.DB.QueryRowContext(ctx, `SELECT workspace_id,status,COALESCE(access_url,''),COALESCE(readiness_evidence_ref,''),deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref,observed_at,execution_epoch,applied_model_configuration_version FROM serve.agent_runtime_instances WHERE id=$1 AND deployment_id=$2`, runtimeID, deployment).Scan(&workspace, &st, &url, &receipt, &descriptorRaw, &digest, &ref, &observed, &epoch, &version)
	if err != nil {
		return nil, dbError(err)
	}
	descriptor := &api.DeploymentDescriptor{}
	if publicjson.Unmarshal(descriptorRaw, descriptor) != nil {
		return nil, status.Error(codes.DataLoss, "persisted deployment descriptor is invalid")
	}
	out := &api.RuntimeReadback{RuntimeInstanceId: runtimeID, WorkspaceId: workspace, DeploymentId: deployment, State: api.AgentRuntimeObservationState(api.AgentRuntimeObservationState_value["RUNTIME_INSTANCE_STATE_"+strings.ToUpper(st)]), ProcessReady: st == "ready", ApplicationAvailable: st == "ready", Artifact: descriptor.GetArtifact(), AppliedModelConfigurationVersion: version, ReadinessReceiptId: receipt, Outcome: api.Observation_OBSERVATION_UNKNOWN, DeploymentDescriptorDigest: digest, DeploymentDescriptorObjectRef: ref, ExecutionEpoch: epoch}
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

// confirmedExecutionTarget resolves the execution facts one delivery step runs
// against from the same confirmed readback: the executable resource binding and the
// placement the resources owner published for it. A provider that schedules no
// workload onto a node, a prepaid package and a storage claim publishes none, and
// the executor that schedules owns the refusal, so this confirmation never
// substitutes or drops a fact.
func confirmedExecutionTarget(command *api.RuntimeDeployCommand, resources *api.ResourceReadback) (ExecutionTarget, error) {
	binding, err := confirmedBinding(command, resources)
	if err != nil {
		return ExecutionTarget{}, err
	}
	return ExecutionTarget{Binding: binding, Placement: resources.GetApplicationPlacement()}, nil
}

// reconcileRuntime serializes the complete provider observation and owner commit
// under the same PostgreSQL workspace lock as Reserve. That lock spans service
// processes, so a slow earlier HTTP response cannot overwrite a later observation.
// Holding one owner transaction also makes runtime readiness and selection atomic.
func (s *Service) reconcileRuntime(ctx context.Context, command *api.RuntimeDeployCommand, start bool) (*api.RuntimeReadback, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()
	if err = lockWorkspace(ctx, tx, command.WorkspaceId); err != nil {
		return nil, dbError(err)
	}
	if err = validateReserved(ctx, tx, command); err != nil {
		return nil, err
	}
	if err = s.authorize(ctx, command.Context, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME, command.WorkspaceId); err != nil {
		return nil, err
	}
	if err = s.observeAndFinishDeliveryTx(ctx, tx, command, start); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return s.runtimeReadback(ctx, command.RuntimeInstanceId, command.DeploymentId)
}

// observeAndFinishDelivery observes the exact command through the execution
// adapter and commits the observation in its own transaction. It is the
// un-authorized half of reconcileRuntime, shared with Serve's own replacement
// product APIs, which authorize the switch they perform before calling it.
func (s *Service) observeAndFinishDelivery(ctx context.Context, command *api.RuntimeDeployCommand, start bool) (*api.RuntimeReadback, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockWorkspace(ctx, tx, command.WorkspaceId); err != nil {
		return nil, dbError(err)
	}
	if err = validateReserved(ctx, tx, command); err != nil {
		return nil, err
	}
	if err = s.observeAndFinishDeliveryTx(ctx, tx, command, start); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return s.runtimeReadback(ctx, command.RuntimeInstanceId, command.DeploymentId)
}

// observeAndFinishDeliveryTx executes and observes the command, records the
// observation and finishes the delivery step inside the caller's transaction.
func (s *Service) observeAndFinishDeliveryTx(ctx context.Context, tx *sql.Tx, command *api.RuntimeDeployCommand, start bool) error {
	resources, err := s.Resources.ReadResources(ctx, &api.ResourceReadbackRequest{Context: nextOwnerCall(command.Context), ResourceSetId: command.ResourceSetId})
	if err != nil {
		return dbError(err)
	}
	target, err := confirmedExecutionTarget(command, resources)
	if err != nil {
		return err
	}
	// Start's acknowledgement cannot prove readiness; only the separate read does.
	if start {
		if _, err = s.Runtime.Start(ctx, command, target); err != nil {
			return err
		}
	}
	observation, err := s.Runtime.Observe(ctx, command, target)
	if err != nil {
		return err
	}
	if _, err = recordDeploymentObservation(ctx, tx, command, observation); err != nil {
		return err
	}
	return s.finishFirstDelivery(ctx, tx, command, observation)
}
