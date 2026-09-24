package build

import (
	"context"
	"encoding/json"
	"errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerstore"
	"strings"
	"time"
)

// Run resumes durable work. A database session lock prevents two live
// coordinators exporting a Job. A restarted building Job only reads the original
// registry identity; it never starts another exporter with an unknown outcome.
func (s *Service) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		_ = s.RunOnce(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
func (s *Service) RunOnce(ctx context.Context) error {
	if err := s.deliver(ctx); err != nil {
		return err
	}
	rows, err := s.store.DB().QueryContext(ctx, `SELECT id FROM build.build_jobs WHERE status IN ('queued','validating','building','pushing','registering','needs_attention') ORDER BY created_at LIMIT 20`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var v string
		if err = rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, jobID := range ids {
		conn, err := s.store.DB().Conn(ctx)
		if err != nil {
			return err
		}
		var locked bool
		err = conn.QueryRowContext(ctx, `SELECT pg_try_advisory_lock(hashtextextended($1,0))`, jobID).Scan(&locked)
		if err != nil || !locked {
			conn.Close()
			continue
		}
		r, readErr := s.read(ctx, jobID)
		if readErr == nil {
			readErr = s.advance(ctx, r)
		}
		unlockCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		_, unlockErr := conn.ExecContext(unlockCtx, `SELECT pg_advisory_unlock(hashtextextended($1,0))`, jobID)
		cancel()
		conn.Close()
		if readErr != nil {
			return readErr
		}
		if unlockErr != nil {
			return unlockErr
		}
	}
	return nil
}
func (s *Service) log(ctx context.Context, r *record, message string) error {
	_, err := s.store.DB().ExecContext(ctx, `INSERT INTO build.build_logs(id,build_job_id,sequence,stage,level,message) SELECT $1,$2,COALESCE(MAX(sequence),-1)+1,$3,'info',$4 FROM build.build_logs WHERE build_job_id=$2`, id("log_"), r.Job.Id, r.Job.Stage, message)
	return err
}
func (s *Service) stage(ctx context.Context, r *record, stage, opStatus, code string) error {
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	terminal := stage == "failed" || stage == "succeeded"
	_, err = tx.ExecContext(ctx, `UPDATE build.build_jobs SET status=$2,stage=$2,error_code=NULLIF($3,''),started_at=COALESCE(started_at,now()),finished_at=CASE WHEN $4 THEN now() ELSE finished_at END,updated_at=now() WHERE id=$1 AND status NOT IN ('failed','succeeded')`, r.Job.Id, stage, code, terminal)
	if err != nil {
		return err
	}
	observation := ""
	if stage == "needs_attention" {
		observation = "unknown"
	}
	if stage == "failed" {
		observation = "rejected"
	}
	_, err = tx.ExecContext(ctx, `UPDATE build.operations SET status=$2,stage=$3,error_code=NULLIF($4,''),observation_result=NULLIF($5,''),started_at=COALESCE(started_at,now()),completed_at=CASE WHEN $6 THEN now() ELSE completed_at END,updated_at=now() WHERE id=$1 AND status NOT IN ('failed','succeeded')`, r.Job.OperationId, opStatus, stage, code, observation, terminal)
	if err != nil {
		return err
	}
	if stage == "failed" {
		err = s.store.AppendEvent(ctx, tx, ownerstore.Event{ID: "evt_" + r.Job.Id + "_failed", EventType: "build.failed.v1", SchemaVersion: 1, AggregateType: "build", AggregateID: r.Job.Id, AggregateRevision: 1, TenantID: r.Tenant, CorrelationID: r.Call.RequestId, Payload: wire(&api.BuildFailedEvent{BuildJobId: r.Job.Id, ErrorCode: code}), OccurredAt: time.Now().UTC()})
		if err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	r.Job.Stage = stage
	r.Job.Status = api.BuildJobStatusEnum(api.BuildJobStatusEnum_value["BUILD_JOB_STATUS_ENUM_"+strings.ToUpper(stage)])
	return nil
}
func (s *Service) grant(ctx context.Context, r *record) error {
	if r.Call.GetAcceptedOperationGrantId() != "" {
		return nil
	}
	if s.identity == nil {
		return errors.New("accepted-operation grant issuer is unavailable")
	}
	grant, err := s.identity.IssueAcceptedOperationGrant(ctx, &api.AcceptedOperationGrantRequest{AuthorizationContextId: r.Call.AuthorizationContextId, OwnerCommitEvidence: evidence(r), AllowedActions: []api.AuthorizationActionEnum{api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDREFERENCE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION}})
	if err != nil {
		return err
	}
	if grant.GetId() == "" || grant.GetAcceptedOperationId() != r.Job.OperationId || grant.GetResourceId() != r.Job.Id || grant.GetAcceptedOperationOwner() != api.OwnerEnum_OWNER_ENUM_BUILD {
		return errors.New("accepted-operation grant identity mismatch")
	}
	r.Call.AcceptedOperationGrantId = &grant.Id
	r.Call.SessionId = nil
	r.Call.DeadlineAt = nil
	_, err = s.store.DB().ExecContext(ctx, `UPDATE build.build_jobs SET call_context=$2,updated_at=now() WHERE id=$1`, r.Job.Id, wire(r.Call))
	return err
}
func (s *Service) claims(ctx context.Context, r *record) error {
	targets := []*api.ReferenceTarget{{Target: &api.ReferenceTarget_PackageVersionId{PackageVersionId: r.Input.PackageVersionId}}, {Target: &api.ReferenceTarget_RuntimeVersionId{RuntimeVersionId: r.Input.RuntimeVersionId}}, {Target: &api.ReferenceTarget_WebuiVersionId{WebuiVersionId: r.Input.WebuiVersionId}}}
	claimIDs := []*string{&r.Input.PackageClaimId, &r.Input.RuntimeClaimId, &r.Input.WebuiClaimId}
	for i, target := range targets {
		if *claimIDs[i] == "" {
			claim, err := s.capability.AcquireReference(ctx, &api.ReferenceClaimRequest{Context: r.Call, Target: target, ClaimantOwner: api.OwnerEnum_OWNER_ENUM_BUILD, ClaimantResourceId: r.Job.Id})
			if err != nil {
				return err
			}
			if claim.GetId() == "" || claim.GetClaimantOwner() != api.OwnerEnum_OWNER_ENUM_BUILD || claim.GetClaimantResourceId() != r.Job.Id || !proto.Equal(claim.Target, target) {
				return errors.New("reference claim identity mismatch")
			}
			*claimIDs[i] = claim.Id
		}
	}
	_, err := s.store.DB().ExecContext(ctx, `UPDATE build.build_jobs SET input_snapshot=$2,updated_at=now() WHERE id=$1`, r.Job.Id, wire(r.Input))
	if err != nil {
		return err
	}
	r.Job.InputClaimIds = []string{r.Input.PackageClaimId, r.Input.RuntimeClaimId, r.Input.WebuiClaimId}
	for _, claimID := range r.Job.InputClaimIds {
		claim, err := s.capability.BindReference(ctx, &api.BindReferenceRequest{Context: r.Call, ClaimId: claimID, OwnerCommitEvidence: evidence(r)})
		if err != nil {
			return err
		}
		if claim.GetId() != claimID || claim.State != api.ReferenceClaimState_REFERENCE_CLAIM_STATE_BOUND || claim.GetBoundOperationId() != r.Job.OperationId || claim.GetBoundInputDigest() != r.Input.SnapshotDigest {
			return errors.New("reference binding evidence mismatch")
		}
	}
	return nil
}
func (s *Service) advance(ctx context.Context, r *record) error {
	if r.Job.Status == api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_REGISTERING {
		return nil
	}
	repo := strings.TrimSuffix(r.Executor, ":"+r.Job.Id)
	if !repositoryPattern.MatchString(repo) {
		return s.stage(ctx, r, "failed", "failed", "validation_failed")
	}
	switch r.Job.Status {
	case api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_BUILDING, api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_PUSHING, api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_NEEDS_ATTENTION:
		manifest, err := s.runner.ReadManifest(ctx, repo, r.Job.Id, r.Input.RuntimeArtifact.Platform)
		if err == nil {
			manifest, err = s.runner.ReadManifest(ctx, repo, manifest.Digest, r.Input.RuntimeArtifact.Platform)
		}
		if err != nil {
			if r.Job.Status != api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_NEEDS_ATTENTION {
				return s.stage(ctx, r, "needs_attention", "needs_attention", "build_result_unknown")
			}
			return nil
		}
		return s.confirm(ctx, r, repo, manifest)
	}
	if err := s.stage(ctx, r, "validating", "running", ""); err != nil {
		return err
	}
	if err := s.grant(ctx, r); err != nil {
		return err
	}
	if err := s.claims(ctx, r); err != nil {
		if status.Code(err) == codes.FailedPrecondition || status.Code(err) == codes.PermissionDenied || status.Code(err) == codes.NotFound {
			return s.stage(ctx, r, "failed", "failed", "validation_failed")
		}
		return err
	}
	if err := s.runner.ValidateInput(r.Input); err != nil {
		return s.stage(ctx, r, "failed", "failed", "validation_failed")
	}
	if err := s.stage(ctx, r, "building", "running", ""); err != nil {
		return err
	}
	executionCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var logErr error
	result := s.runner.Execute(executionCtx, r.Job.Id, repo, r.Input, func(message string) {
		if logErr == nil {
			logErr = s.log(executionCtx, r, message)
			if logErr != nil {
				cancel()
			}
		}
	})
	finalCtx, done := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer done()
	if result.Err != nil {
		if !result.Started || errors.Is(result.Err, ErrBuildRejected) {
			if err := s.log(finalCtx, r, result.Err.Error()); err != nil {
				return err
			}
			return s.stage(finalCtx, r, "failed", "failed", "build_failed")
		}
		return s.stage(finalCtx, r, "needs_attention", "needs_attention", "build_result_unknown")
	}
	if err := s.stage(finalCtx, r, "pushing", "running", ""); err != nil {
		return err
	}
	return s.confirm(finalCtx, r, repo, result.Manifest)
}
func descriptor(in *api.BuildInputSnapshot, artifact *api.ArtifactReference, jobID string) *api.DeploymentDescriptor {
	revision := proto.Clone(in.RuntimeContract.ApplicationRevisionTemplate).(*api.WorkspaceApplicationRevision)
	revision.Image = artifact.Repository + "@" + artifact.Digest
	revision.Version = jobID
	packageVersion, inputDigest := in.PackageVersionId, in.SnapshotDigest
	return &api.DeploymentDescriptor{SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Artifact: artifact, RuntimeContract: in.RuntimeContract, RuntimeContractReference: in.RuntimeContractReference, WebuiContract: in.WebuiContract, WebuiContractReference: in.WebuiContractReference, PackageVersionId: &packageVersion, BuildInputDigest: &inputDigest, Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD, ApplicationRevision: revision}
}
func descriptorBytes(d *api.DeploymentDescriptor) ([]byte, error) {
	return publicjson.Marshal(d)
}
func (s *Service) confirm(ctx context.Context, r *record, repository string, m Manifest) error {
	a := &api.ArtifactReference{Repository: repository, Digest: m.Digest, Platform: r.Input.RuntimeArtifact.Platform}
	d := descriptor(r.Input, a, r.Job.Id)
	bytes, err := descriptorBytes(d)
	if err != nil {
		return err
	}
	dd := digest(bytes)
	artifactID := "artifact_" + r.Job.Id
	ref := "build-artifact://" + artifactID + "/descriptor@" + dd
	provenance := wire(&api.BuildArtifactReadback{BuildJobId: r.Job.Id, Input: r.Input, Artifact: a, ArtifactReceiptId: artifactID, VersionLabel: r.Job.Id, DeploymentDescriptor: d, DeploymentDescriptorDigest: dd, DeploymentDescriptorObjectRef: ref, Outcome: api.Observation_OBSERVATION_CONFIRMED, DataCompatibility: &api.DataCompatibility{DataSchemaVersion: r.Input.RuntimeContract.GetData().GetSchemaVersion(), CompatibleFromVersions: r.Input.RuntimeContract.GetData().GetUpgrade().GetCompatibleFromSchemaVersions(), RollbackSafe: r.Input.RuntimeContract.GetData().GetRollback().GetSafe(), MigrationRequired: r.Input.RuntimeContract.GetData().GetUpgrade().GetMode() == api.DataUpgradeContractModeEnum_DATA_UPGRADE_CONTRACT_MODE_ENUM_PUBLISHER_MIGRATION}})
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM build.build_jobs WHERE id=$1 FOR UPDATE`, r.Job.Id).Scan(&state); err != nil {
		return err
	}
	if state == "registering" || state == "succeeded" {
		return nil
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO build.build_artifacts(id,build_job_id,artifact_repository,artifact_digest,size_bytes,provenance,verification_evidence_ref,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref,descriptor_bytes) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, artifactID, r.Job.Id, repository, m.Digest, m.Size, provenance, repository+"@"+m.Digest, bytes, dd, ref, bytes)
	if err != nil {
		return err
	}
	event := &api.BuildArtifactConfirmedEvent{BuildJobId: r.Job.Id, PackageVersionId: r.Input.PackageVersionId, RuntimeVersionId: r.Input.RuntimeVersionId, WebuiVersionId: r.Input.WebuiVersionId, ArtifactDigest: m.Digest, ArtifactReceiptId: artifactID, DeploymentDescriptorDigest: dd}
	err = s.store.AppendEvent(ctx, tx, ownerstore.Event{ID: "evt_" + r.Job.Id + "_artifact", EventType: "build.artifact_confirmed.v1", SchemaVersion: 1, AggregateType: "build", AggregateID: r.Job.Id, AggregateRevision: 1, TenantID: r.Tenant, CorrelationID: r.Call.RequestId, Payload: wire(event), OccurredAt: time.Now().UTC()})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE build.build_jobs SET status='registering',stage='registering',artifact_digest=$2,error_code=NULL,updated_at=now() WHERE id=$1`, r.Job.Id, m.Digest)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE build.operations SET status='awaiting_confirmation',stage='registering',observation_result='confirmed',error_code=NULL,updated_at=now() WHERE id=$1`, r.Job.OperationId)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (s *Service) ReadArtifact(ctx context.Context, req *api.ReadBuildArtifactRequest) (*api.BuildArtifactReadback, error) {
	if err := requirePeer(ctx, owneridentity.Capability, owneridentity.Ledger); err != nil {
		return nil, err
	}
	var raw, descriptor []byte
	if err := s.store.DB().QueryRowContext(ctx, `SELECT provenance, descriptor_bytes FROM build.build_artifacts WHERE build_job_id=$1`, req.GetBuildJobId()).Scan(&raw, &descriptor); err != nil {
		return nil, databaseError(err)
	}
	result := &api.BuildArtifactReadback{}
	if err := protojson.Unmarshal(raw, result); err != nil {
		return nil, databaseError(err)
	}
	if result.GetDeploymentDescriptorDigest() == "" || digest(descriptor) != result.GetDeploymentDescriptorDigest() {
		result.Outcome = api.Observation_OBSERVATION_UNKNOWN
		return result, nil
	}
	if _, err := s.runner.ReadManifest(ctx, result.Artifact.Repository, result.Artifact.Digest, result.Artifact.Platform); err != nil {
		result.Outcome = api.Observation_OBSERVATION_UNKNOWN
	}
	return result, nil
}
func (s *Service) deliver(ctx context.Context) error {
	for consumer, client := range s.inboxes {
		pending, err := s.store.PendingDeliveries(ctx, consumer, 20)
		if err != nil {
			return err
		}
		for _, p := range pending {
			e := p.Event
			envelope := &api.EventEnvelope{EventId: e.ID, EventType: e.EventType, SchemaVersion: e.SchemaVersion, Owner: "build", TenantId: e.TenantID, AggregateId: e.AggregateID, AggregateVersion: e.AggregateRevision, OccurredAt: timestamppb.New(e.OccurredAt), RequestId: e.CorrelationID, Scope: "tenant"}
			switch e.EventType {
			case "build.artifact_confirmed.v1":
				payload := &api.BuildArtifactConfirmedEvent{}
				if err := protojson.Unmarshal(e.Payload, payload); err != nil {
					return err
				}
				envelope.Payload = &api.EventEnvelope_BuildArtifactConfirmed{BuildArtifactConfirmed: payload}
			case "build.failed.v1":
				payload := &api.BuildFailedEvent{}
				if err := protojson.Unmarshal(e.Payload, payload); err != nil {
					return err
				}
				envelope.Payload = &api.EventEnvelope_BuildFailed{BuildFailed: payload}
			default:
				return errors.New("unsupported Build outbox event")
			}
			callCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
			ack, err := client.Deliver(callCtx, &api.DeliverEventRequest{Event: envelope, AuthenticatedProducer: "build"})
			cancel()
			if err != nil || ack.GetEventId() != e.ID || ack.GetConsumer() != consumer || !ack.GetCommitted() {
				if err = s.store.RecordDeliveryFailure(ctx, p.DeliveryID, "consumer_unavailable", time.Now().Add(5*time.Second)); err != nil {
					return err
				}
				continue
			}
			if err = s.store.AcknowledgeDelivery(ctx, consumer, e.ID); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) Deliver(ctx context.Context, req *api.DeliverEventRequest) (*api.InboxAck, error) {
	if err := requirePeer(ctx, owneridentity.Capability); err != nil {
		return nil, err
	}
	event := req.GetEvent()
	payload := event.GetCapabilityVersionRegistered()
	if event.GetOwner() != "capability" || req.GetAuthenticatedProducer() != "capability" || event.GetEventType() != "capability.version_registered.v1" || event.GetSchemaVersion() != 1 || payload == nil || event.GetScope() != "tenant" || event.GetEventId() == "" || event.GetAggregateId() != payload.CapabilityVersionId {
		return nil, status.Error(codes.InvalidArgument, "invalid Capability registration event")
	}
	r, err := s.read(ctx, payload.BuildJobId)
	if err != nil {
		return nil, err
	}
	if event.TenantId != r.Tenant || payload.ArtifactDigest != r.Job.GetArtifactDigest() {
		return nil, status.Error(codes.FailedPrecondition, "registration does not match Build output")
	}
	version, err := s.versions.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: r.Call, CapabilityVersionId: payload.CapabilityVersionId})
	if err != nil {
		return nil, err
	}
	if version.GetBuildJobId() != r.Job.Id || version.ArtifactDigest != payload.ArtifactDigest || version.GetPackageVersionId() != r.Input.PackageVersionId || version.GetRuntimeVersionId() != r.Input.RuntimeVersionId || version.GetWebuiVersionId() != r.Input.WebuiVersionId || version.Status != api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY {
		return nil, status.Error(codes.FailedPrecondition, "Capability readback differs from Build output")
	}
	var descriptorDigest, repository string
	if err = s.store.DB().QueryRowContext(ctx, `SELECT deployment_descriptor_digest,artifact_repository FROM build.build_artifacts WHERE build_job_id=$1`, r.Job.Id).Scan(&descriptorDigest, &repository); err != nil {
		return nil, databaseError(err)
	}
	if version.DeploymentDescriptorDigest != descriptorDigest || version.GetArtifact().GetRepository() != repository || version.GetArtifact().GetDigest() != payload.ArtifactDigest || !proto.Equal(version.Artifact.Platform, r.Input.RuntimeArtifact.Platform) {
		return nil, status.Error(codes.FailedPrecondition, "Capability artifact descriptor mismatch")
	}
	tx, err := s.store.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, databaseError(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT id FROM build.build_jobs WHERE id=$1 FOR UPDATE`, r.Job.Id); err != nil {
		return nil, databaseError(err)
	}
	result, err := s.store.DeliverInbox(ctx, tx, ownerstore.InboundEvent{ID: "in_" + event.EventId, SourceOwner: "capability", SourceEventID: event.EventId, EventType: event.EventType, SchemaVersion: 1, AggregateType: "capability_version", AggregateID: event.AggregateId, AggregateRevision: event.AggregateVersion, Payload: wire(payload)}, time.Now())
	if err != nil {
		return nil, databaseError(err)
	}
	if result.Decision == ownerstore.InboxConflict {
		return nil, status.Error(codes.AlreadyExists, "event identity conflict")
	}
	if result.Applied {
		res, err := tx.ExecContext(ctx, `UPDATE build.build_jobs SET status='succeeded',stage='succeeded',result_capability_version_id=$2,finished_at=now(),updated_at=now() WHERE id=$1 AND status='registering'`, r.Job.Id, payload.CapabilityVersionId)
		if err != nil {
			return nil, databaseError(err)
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return nil, status.Error(codes.FailedPrecondition, "Build is not registering")
		}
		safeResult, _ := json.Marshal(struct {
			CapabilityVersionID string `json:"capabilityVersionId"`
		}{payload.CapabilityVersionId})
		_, err = tx.ExecContext(ctx, `UPDATE build.operations SET status='succeeded',stage='succeeded',observation_result='confirmed',result=$2,completed_at=now(),updated_at=now() WHERE id=$1`, r.Job.OperationId, safeResult)
		if err != nil {
			return nil, databaseError(err)
		}
		if err = s.store.MarkInboxProcessed(ctx, tx, "capability", event.EventId, payload.CapabilityVersionId, "", time.Now()); err != nil {
			return nil, databaseError(err)
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, databaseError(err)
	}
	return &api.InboxAck{EventId: event.EventId, Consumer: "build", Committed: true, Duplicate: result.Decision == ownerstore.InboxDuplicate, AppliedAggregateVersion: event.AggregateVersion}, nil
}
