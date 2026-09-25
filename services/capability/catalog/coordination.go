package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

func targetInfo(t *api.ReferenceTarget) (string, string, error) {
	if t == nil {
		return "", "", status.Error(codes.InvalidArgument, "reference target is required")
	}
	switch v := t.Target.(type) {
	case *api.ReferenceTarget_PackageVersionId:
		return "package_version", v.PackageVersionId, nil
	case *api.ReferenceTarget_RuntimeVersionId:
		return "runtime_version", v.RuntimeVersionId, nil
	case *api.ReferenceTarget_WebuiVersionId:
		return "webui_version", v.WebuiVersionId, nil
	case *api.ReferenceTarget_CapabilityVersionId:
		return "capability_version", v.CapabilityVersionId, nil
	default:
		return "", "", status.Error(codes.InvalidArgument, "exactly one reference target is required")
	}
}

func ownerName(v api.OwnerEnum) string {
	return strings.ToLower(strings.TrimPrefix(v.String(), "OWNER_ENUM_"))
}

func (s *Service) ResolveBuildInput(ctx context.Context, r *api.BuildInputRequest) (*api.BuildInputSnapshot, error) {
	if e := s.auth(ctx, r.GetContext(), "ResolveBuildInput", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, r.GetPackageVersionId()); e != nil {
		return nil, e
	}
	if r.GetPackageVersionId() == "" || r.GetWebuiVersionId() == "" || s.Runtime == nil {
		return nil, status.Error(codes.InvalidArgument, "package, WebUI and Runtime selections are required")
	}
	var packageID, objectRef, sha string
	var size int64
	var packageStatus string
	if e := s.DB.QueryRowContext(ctx, `SELECT p.id,v.object_ref,v.sha256,v.size_bytes,v.status FROM capability.package_versions v JOIN capability.packages p ON p.id=v.package_id JOIN capability.namespaces n ON n.id=p.namespace_id WHERE v.id=$1 AND (n.tenant_id=$2 OR p.visibility='official')`, r.GetPackageVersionId(), tenant(r.GetContext())).Scan(&packageID, &objectRef, &sha, &size, &packageStatus); e != nil {
		return nil, dbError(e)
	}
	if packageStatus != "uploaded" || objectRef == "" {
		return nil, status.Error(codes.FailedPrecondition, "PackageVersion is not uploaded")
	}
	var webui api.WebuiPublisherContract
	var webuiRef api.PublisherContractReference
	var webuiStatus string
	var webuiRaw []byte
	if e := s.DB.QueryRowContext(ctx, `SELECT publisher_contract,status,publisher_namespace_id,publisher_contract_digest,publisher_contract_object_ref FROM capability.webui_versions WHERE id=$1`, r.GetWebuiVersionId()).Scan(&webuiRaw, &webuiStatus, &webuiRef.PublisherNamespaceId, &webuiRef.DescriptorDigest, &webuiRef.DescriptorObjectRef); e != nil {
		return nil, dbError(e)
	}
	if webuiStatus != "approved" || publicjson.Unmarshal(webuiRaw, &webui) != nil {
		return nil, status.Error(codes.FailedPrecondition, "WebUI version is not approved")
	}
	webuiRef.VersionId = r.GetWebuiVersionId()
	webuiRef.Kind = api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_WEBUI
	runtime, e := s.runtimeVersion(ctx, r.GetContext(), "")
	if e != nil {
		return nil, e
	}
	if runtime == nil {
		return nil, status.Error(codes.FailedPrecondition, "no approved Runtime catalog policy is available")
	}
	contract := runtime.GetPublisherContract()
	if contract == nil || contract.GetImage() == nil || contract.GetBuildRecipe() == nil {
		return nil, status.Error(codes.DataLoss, "Runtime publisher contract is incomplete")
	}
	for _, publisherID := range []string{webui.PublisherNamespaceId, contract.PublisherNamespaceId} {
		var admitted bool
		if e := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM capability.publisher_namespaces WHERE id=$1 AND status='approved')`, publisherID).Scan(&admitted); e != nil {
			return nil, dbError(e)
		}
		if !admitted {
			return nil, status.Error(codes.FailedPrecondition, "input publisher is no longer approved")
		}
	}
	runtimeRef := &api.PublisherContractReference{PublisherNamespaceId: runtime.GetPublisherNamespaceId(), VersionId: runtime.GetId(), Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_RUNTIME, DescriptorDigest: runtime.GetPublisherContractDigest(), DescriptorObjectRef: "runtime-contract:" + runtime.GetId() + "@" + runtime.GetPublisherContractDigest()}
	in := &api.BuildInputSnapshot{PackageId: packageID, PackageVersionId: r.GetPackageVersionId(), PackageObject: &api.SourceObjectReference{StorageObjectId: objectRef, VersionId: objectRef, Sha256: sha, SizeBytes: size}, RuntimeVersionId: runtime.GetId(), RuntimeArtifact: contract.GetImage(), WebuiVersionId: r.GetWebuiVersionId(), WebuiArtifact: webui.GetImage(), RuntimeContract: contract, WebuiContract: &webui, RuntimeContractReference: runtimeRef, WebuiContractReference: &webuiRef}
	b, _ := protojson.Marshal(in)
	in.SnapshotDigest = digest(b)
	return in, nil
}

func (s *Service) AcquireReference(ctx context.Context, r *api.ReferenceClaimRequest) (*api.ReferenceClaim, error) {
	kind, target, e := targetInfo(r.GetTarget())
	if e != nil {
		return nil, e
	}
	if r.GetClaimantResourceId() == "" || r.GetClaimantOwner() != api.OwnerEnum_OWNER_ENUM_BUILD {
		return nil, status.Error(codes.InvalidArgument, "Build claimant identity is required")
	}
	if e = s.authorizeReference(ctx, r.GetContext(), "AcquireReference", kind, target); e != nil {
		return nil, e
	}
	claim := &api.ReferenceClaim{Id: id("claim"), Target: r.Target, ClaimantOwner: r.ClaimantOwner, ClaimantResourceId: r.ClaimantResourceId, State: api.ReferenceClaimState_REFERENCE_CLAIM_STATE_ACQUIRED, AcquiredAt: timestamppb.Now()}
	_, e = s.DB.ExecContext(ctx, `INSERT INTO capability.reference_claims(id,target_type,package_version_id,capability_version_id,runtime_version_id,webui_version_id,claimant_owner,claimant_resource_id,purpose,request_id) VALUES($1,$2,NULLIF($3,''),NULLIF($4,''),NULLIF($5,''),NULLIF($6,''),$7,$8,'build',$9) ON CONFLICT DO NOTHING`, claim.Id, kind, func() string {
		if kind == "package_version" {
			return target
		}
		return ""
	}(), func() string {
		if kind == "capability_version" {
			return target
		}
		return ""
	}(), func() string {
		if kind == "runtime_version" {
			return target
		}
		return ""
	}(), func() string {
		if kind == "webui_version" {
			return target
		}
		return ""
	}(), ownerName(r.ClaimantOwner), r.ClaimantResourceId, r.GetContext().GetRequestId())
	if e != nil {
		return nil, dbError(e)
	}
	var existingID string
	var existingBound, existingReleased sql.NullTime
	var existingOperation, existingDigest sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT id,bound_at,bound_operation_id,bound_input_digest,released_at FROM capability.reference_claims WHERE target_type=$1 AND COALESCE(package_version_id,capability_version_id,runtime_version_id,webui_version_id)=$2 AND claimant_owner=$3 AND claimant_resource_id=$4 AND purpose='build' AND released_at IS NULL`, kind, target, ownerName(r.ClaimantOwner), r.GetClaimantResourceId()).Scan(&existingID, &existingBound, &existingOperation, &existingDigest, &existingReleased)
	if err != nil {
		return nil, dbError(err)
	}
	claim.Id = existingID
	if existingBound.Valid {
		claim.State = api.ReferenceClaimState_REFERENCE_CLAIM_STATE_BOUND
		claim.BoundOperationId = proto.String(existingOperation.String)
		claim.BoundInputDigest = proto.String(existingDigest.String)
	}
	return claim, nil
}

func (s *Service) BindReference(ctx context.Context, r *api.BindReferenceRequest) (*api.ReferenceClaim, error) {
	if r.GetClaimId() == "" || r.GetOwnerCommitEvidence() == nil {
		return nil, status.Error(codes.InvalidArgument, "claim and owner evidence are required")
	}
	if r.GetContext() == nil || r.GetContext().GetActorId() == "" || r.GetContext().GetRequestId() == "" {
		return nil, status.Error(codes.Unauthenticated, "authenticated request context is required")
	}
	if e := ownerservice.ValidateCallContext(ctx, r.GetContext()); e != nil {
		return nil, e
	}
	var claim api.ReferenceClaim
	var kind, target, owner string
	var boundAt sql.NullTime
	e := s.DB.QueryRowContext(ctx, `SELECT target_type,COALESCE(package_version_id,capability_version_id,runtime_version_id,webui_version_id),claimant_owner,claimant_resource_id,bound_at FROM capability.reference_claims WHERE id=$1`, r.GetClaimId()).Scan(&kind, &target, &owner, &claim.ClaimantResourceId, &boundAt)
	if e != nil {
		return nil, dbError(e)
	}
	if e := s.authorizeReference(ctx, r.GetContext(), "BindReference", kind, target); e != nil {
		return nil, e
	}
	if owner != ownerName(r.GetOwnerCommitEvidence().GetOwner()) || r.GetOwnerCommitEvidence().GetResourceId() != claim.ClaimantResourceId || r.GetOwnerCommitEvidence().GetOperationId() == "" || r.GetOwnerCommitEvidence().GetAcceptedInputDigest() == "" {
		return nil, status.Error(codes.FailedPrecondition, "owner commit evidence does not match claim")
	}
	if s.Commit == nil {
		return nil, status.Error(codes.Unavailable, "Build commit readback unavailable")
	}
	actual, e := s.Commit.ReadOwnerCommit(ctx, &api.ReadOwnerCommitRequest{Owner: r.OwnerCommitEvidence.Owner, OperationId: r.OwnerCommitEvidence.OperationId, ResourceId: r.OwnerCommitEvidence.ResourceId})
	if e != nil {
		return nil, e
	}
	if !proto.Equal(actual, r.OwnerCommitEvidence) {
		return nil, status.Error(codes.FailedPrecondition, "owner commit readback mismatch")
	}
	result, e := s.DB.ExecContext(ctx, `UPDATE capability.reference_claims SET bound_at=COALESCE(bound_at,now()),bound_operation_id=$2,bound_input_digest=$3,updated_at=now() WHERE id=$1 AND released_at IS NULL AND (bound_at IS NULL OR (bound_operation_id=$2 AND bound_input_digest=$3))`, r.ClaimId, actual.OperationId, actual.AcceptedInputDigest)
	if e != nil {
		return nil, dbError(e)
	}
	count, e := result.RowsAffected()
	if e != nil {
		return nil, dbError(e)
	}
	if count != 1 {
		return nil, status.Error(codes.FailedPrecondition, "claim binding conflicts with persisted evidence")
	}
	claim.Id = r.GetClaimId()
	claim.State = api.ReferenceClaimState_REFERENCE_CLAIM_STATE_BOUND
	claim.BoundOperationId = proto.String(r.GetOwnerCommitEvidence().GetOperationId())
	claim.BoundInputDigest = proto.String(r.GetOwnerCommitEvidence().GetAcceptedInputDigest())
	claim.ClaimantOwner = api.OwnerEnum(api.OwnerEnum_value["OWNER_ENUM_"+strings.ToUpper(owner)])
	claim.Target = targetRef(kind, target)
	return &claim, nil
}

// ReleaseReference closes a claim only after the claimant owner has returned
// terminal usage evidence. A caller-provided receipt is metadata; the live
// ClaimUsageReadback is the authority that proves the claim is no longer needed.
func (s *Service) ReleaseReference(ctx context.Context, r *api.ReleaseReferenceRequest) (*api.ReferenceClaim, error) {
	if r.GetClaimId() == "" || r.GetReleaseEvidence() == nil {
		return nil, status.Error(codes.InvalidArgument, "claim and release evidence are required")
	}
	if r.GetContext() == nil || r.GetContext().GetActorId() == "" || r.GetContext().GetRequestId() == "" {
		return nil, status.Error(codes.Unauthenticated, "authenticated request context is required")
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	evidence := r.GetReleaseEvidence()
	if evidence.GetOperationId() == "" || evidence.GetResourceId() == "" || evidence.GetTerminalReceiptId() == "" || evidence.GetTerminalStatus() == api.TerminalOperationStatus_TERMINAL_OPERATION_STATUS_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "complete terminal release evidence is required")
	}
	var targetType, targetID, claimantOwner, claimantResource string
	var boundAt, releasedAt sql.NullTime
	var boundOperation, boundDigest sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT target_type,COALESCE(package_version_id,capability_version_id,runtime_version_id,webui_version_id),claimant_owner,claimant_resource_id,bound_at,bound_operation_id,bound_input_digest,released_at FROM capability.reference_claims WHERE id=$1`, r.GetClaimId()).Scan(&targetType, &targetID, &claimantOwner, &claimantResource, &boundAt, &boundOperation, &boundDigest, &releasedAt)
	if err != nil {
		return nil, dbError(err)
	}
	if err := s.authorizeReference(ctx, r.GetContext(), "ReleaseReference", targetType, targetID); err != nil {
		return nil, err
	}
	if releasedAt.Valid {
		return nil, status.Error(codes.FailedPrecondition, "reference claim is already released")
	}
	if !boundAt.Valid || claimantOwner != ownerName(evidence.GetOwner()) || claimantResource != evidence.GetResourceId() || boundOperation.String != evidence.GetOperationId() {
		return nil, status.Error(codes.FailedPrecondition, "release evidence does not match claim")
	}
	if s.Usage == nil {
		return nil, status.Error(codes.Unavailable, "claim usage readback is unavailable")
	}
	usage, err := s.Usage.ReadClaimUsage(ctx, &api.ReadClaimUsageRequest{Context: r.GetContext(), ClaimId: r.GetClaimId(), ClaimantResourceId: evidence.GetResourceId(), ClaimantOperationId: evidence.GetOperationId()})
	if err != nil {
		return nil, err
	}
	if usage.GetClaimId() != r.GetClaimId() || usage.GetResourceId() != evidence.GetResourceId() || usage.GetOperationId() != evidence.GetOperationId() || usage.GetActivelyRequired() || usage.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || usage.GetTerminalReceiptId() != evidence.GetTerminalReceiptId() {
		return nil, status.Error(codes.FailedPrecondition, "claim usage is not terminal and confirmed")
	}
	if boundDigest.Valid && usage.GetAcceptedInputDigest() != boundDigest.String {
		return nil, status.Error(codes.FailedPrecondition, "claim usage input digest differs from bound claim")
	}
	releaseBytes, _ := protojson.Marshal(evidence)
	_, err = s.DB.ExecContext(ctx, `UPDATE capability.reference_claims SET released_at=now(),release_evidence_ref=$2,release_evidence=$3,updated_at=now() WHERE id=$1 AND released_at IS NULL`, r.GetClaimId(), evidence.GetTerminalReceiptId(), releaseBytes)
	if err != nil {
		return nil, dbError(err)
	}
	return &api.ReferenceClaim{Id: r.GetClaimId(), Target: targetRef(targetType, targetID), ClaimantOwner: api.OwnerEnum(api.OwnerEnum_value["OWNER_ENUM_"+strings.ToUpper(claimantOwner)]), ClaimantResourceId: claimantResource, State: api.ReferenceClaimState_REFERENCE_CLAIM_STATE_RELEASED, ReleasedAt: timestamppb.Now()}, nil
}

func referenceKind(kind string) api.AuthorizationResourceKind {
	switch kind {
	case "package_version", "runtime_version", "webui_version":
		return api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION
	case "capability_version":
		return api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION
	default:
		return api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_UNSPECIFIED
	}
}

func targetRef(kind, id string) *api.ReferenceTarget {
	switch kind {
	case "package_version":
		return &api.ReferenceTarget{Target: &api.ReferenceTarget_PackageVersionId{PackageVersionId: id}}
	case "runtime_version":
		return &api.ReferenceTarget{Target: &api.ReferenceTarget_RuntimeVersionId{RuntimeVersionId: id}}
	case "webui_version":
		return &api.ReferenceTarget{Target: &api.ReferenceTarget_WebuiVersionId{WebuiVersionId: id}}
	default:
		return &api.ReferenceTarget{Target: &api.ReferenceTarget_CapabilityVersionId{CapabilityVersionId: id}}
	}
}

func (s *Service) Deliver(ctx context.Context, r *api.DeliverEventRequest) (*api.InboxAck, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || peer != owneridentity.Build.Service() {
		return nil, status.Error(codes.Unauthenticated, "verified Build peer required")
	}
	event := r.GetEvent()
	payload := event.GetBuildArtifactConfirmed()
	if r.GetAuthenticatedProducer() != "build" || event.GetOwner() != "build" || event.GetEventType() != "build.artifact_confirmed.v1" || event.GetSchemaVersion() != 1 || event.GetEventId() == "" || payload == nil || event.GetAggregateId() != payload.BuildJobId || event.GetScope() != "tenant" {
		return nil, status.Error(codes.InvalidArgument, "invalid Build artifact event")
	}
	if s.Build == nil {
		return nil, status.Error(codes.Unavailable, "Build readback unavailable")
	}
	readback, e := s.Build.ReadArtifact(ctx, &api.ReadBuildArtifactRequest{BuildJobId: payload.BuildJobId, Context: eventCall(event)})
	if e != nil {
		return nil, e
	}
	if readback.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || readback.GetBuildJobId() != payload.BuildJobId || readback.GetInput().GetPackageVersionId() != payload.PackageVersionId || readback.GetInput().GetRuntimeVersionId() != payload.RuntimeVersionId || readback.GetInput().GetWebuiVersionId() != payload.WebuiVersionId || readback.GetArtifact().GetDigest() != payload.ArtifactDigest || readback.GetArtifactReceiptId() != payload.ArtifactReceiptId || readback.GetDeploymentDescriptorDigest() != payload.DeploymentDescriptorDigest {
		return nil, status.Error(codes.FailedPrecondition, "Build artifact readback mismatch")
	}
	descriptor, e := publicjson.Marshal(readback.GetDeploymentDescriptor())
	if e != nil || digest(descriptor) != payload.DeploymentDescriptorDigest {
		return nil, status.Error(codes.FailedPrecondition, "Build descriptor bytes differ from digest")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, dbError(e)
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, payload.BuildJobId); e != nil {
		return nil, dbError(e)
	}
	var tenantID string
	if e = tx.QueryRowContext(ctx, `SELECT n.tenant_id FROM capability.namespaces n JOIN capability.packages p ON p.namespace_id=n.id JOIN capability.package_versions v ON v.package_id=p.id WHERE p.id=$1 AND v.id=$2`, readback.Input.PackageId, payload.PackageVersionId).Scan(&tenantID); e != nil {
		return nil, dbError(e)
	}
	if tenantID != event.TenantId {
		return nil, status.Error(codes.PermissionDenied, "Build event belongs to another tenant")
	}
	result, e := s.Store.DeliverInbox(ctx, tx, ownerstore.InboundEvent{ID: "in_" + event.EventId, SourceOwner: "build", SourceEventID: event.EventId, EventType: event.EventType, SchemaVersion: 1, AggregateType: "build", AggregateID: payload.BuildJobId, AggregateRevision: event.AggregateVersion, Payload: jsonBytes(payload)}, time.Now())
	if e != nil {
		return nil, dbError(e)
	}
	if result.Decision == ownerstore.InboxConflict {
		return nil, status.Error(codes.AlreadyExists, "Build event identity conflicts")
	}
	if result.Applied {
		versionID := id("capv")
		_, e = tx.ExecContext(ctx, `INSERT INTO capability.capability_versions(id,package_id,package_version_id,build_job_id,version_label,runtime_version_id,webui_version_id,artifact_repository,artifact_digest,status,model_requirements,data_compatibility,provenance_evidence,provenance,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'ready',$10,$11,$12,'build',$13,$14,$15)`, versionID, readback.Input.PackageId, payload.PackageVersionId, payload.BuildJobId, readback.VersionLabel, payload.RuntimeVersionId, payload.WebuiVersionId, readback.Artifact.Repository, payload.ArtifactDigest, jsonBytesList(readback.ModelRequirements), jsonBytes(readback.DataCompatibility), jsonBytes(readback), descriptor, payload.DeploymentDescriptorDigest, readback.DeploymentDescriptorObjectRef)
		if e != nil {
			return nil, dbError(e)
		}
		if e = s.Store.MarkInboxProcessed(ctx, tx, "build", event.EventId, versionID, "", time.Now()); e != nil {
			return nil, dbError(e)
		}
		registered := &api.CapabilityVersionRegisteredEvent{CapabilityVersionId: versionID, BuildJobId: payload.BuildJobId, ArtifactDigest: payload.ArtifactDigest}
		if e = s.Store.AppendEvent(ctx, tx, ownerstore.Event{ID: "evt_" + versionID, EventType: "capability.version_registered.v1", SchemaVersion: 1, AggregateType: "capability_version", AggregateID: versionID, AggregateRevision: 1, TenantID: tenantID, CorrelationID: event.RequestId, Payload: jsonBytes(registered), OccurredAt: time.Now()}); e != nil {
			return nil, dbError(e)
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, dbError(e)
	}
	return &api.InboxAck{EventId: event.EventId, Consumer: "capability", Committed: true, Duplicate: result.Decision == ownerstore.InboxDuplicate, AppliedAggregateVersion: event.AggregateVersion}, nil
}

func eventCall(e *api.EventEnvelope) *api.CallContext {
	return &api.CallContext{RequestId: e.GetRequestId(), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: e.GetTenantId()}}}}
}
func jsonBytesList(v []*api.ModelRequirement) []byte {
	var values []json.RawMessage
	for _, value := range v {
		values = append(values, jsonBytes(value))
	}
	if values == nil {
		values = []json.RawMessage{}
	}
	b, _ := json.Marshal(values)
	return b
}

func (s *Service) authorizeReference(ctx context.Context, c *api.CallContext, action, kind, target string) error {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || peer != owneridentity.Build.Service() {
		return status.Error(codes.PermissionDenied, "Build reference peer required")
	}
	if kind == "package_version" || kind == "capability_version" {
		return s.auth(ctx, c, action, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, target)
	}
	if err := ownerservice.ValidateCallContext(ctx, c); err != nil {
		return err
	}
	if kind == "runtime_version" {
		if s.Runtime == nil {
			return status.Error(codes.Unavailable, "Runtime owner unavailable")
		}
		v, err := s.runtimeVersion(ctx, c, target)
		if err != nil {
			return err
		}
		if action != "ReleaseReference" && v.Status != api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED {
			return status.Error(codes.FailedPrecondition, "Runtime is not approved")
		}
	} else if kind == "webui_version" {
		var st string
		if err := s.DB.QueryRowContext(ctx, `SELECT status FROM capability.webui_versions WHERE id=$1`, target).Scan(&st); err != nil {
			return dbError(err)
		}
		if action != "ReleaseReference" && st != "approved" {
			return status.Error(codes.FailedPrecondition, "WebUI is not approved")
		}
	}
	return s.Authorize(ctx, c, api.AuthorizationActionEnum(api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_"+strings.ToUpper(action)]), &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String(target)}, ownerservice.ResourceScope{TenantID: tenant(c)})
}

func (s *Service) runtimeVersion(ctx context.Context, c *api.CallContext, id string) (*api.RuntimeVersion, error) {
	cursor := ""
	for {
		page, err := s.Runtime.ListRuntimeVersions(ctx, &api.ListRuntimeVersionsRpcRequest{Context: c, QueryCursor: &cursor})
		if err != nil {
			return nil, err
		}
		for _, v := range page.Items {
			if (id != "" && v.Id == id) || (id == "" && v.DefaultForNewBuilds && v.Status == api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED) {
				return v, nil
			}
		}
		if page.GetNextCursor() == "" {
			return nil, status.Error(codes.NotFound, "Runtime selection not found")
		}
		cursor = page.GetNextCursor()
	}
}
