// Package delivery owns Serve's Agent delivery state. This file owns the one
// place where an executing runtime's observation becomes Serve's own
// runtime-instance record.
//
// Two rules drive everything here:
//
//   - A provisioned resource is never application readiness. Only a runtime
//     observation that reports the application available, with its own readiness
//     evidence, can move an instance to ready.
//   - Serve records what the executing runtime actually reported and refuses to
//     invent the rest. An unknown state, an unprovable entry, a stale epoch or a
//     mismatched identity is refused with a typed error instead of being written
//     as a hopeful status.
package delivery

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

// RuntimeObservation is the executing runtime's report for one deployment. It is
// the adapter's output, not a second contract: the caller supplies an observation
// plus the already-resolved publishable entry. Serve never derives an origin of
// its own here, because the installation, not Serve, decides which hostname
// publishes an Agent.
type RuntimeObservation struct {
	// State is the provider's observed runtime state.
	State api.AgentRuntimeObservationState
	// ApplicationEntry is the runtime's published entry, when it reports one.
	ApplicationEntry *contracts.WorkspaceApplicationEntry
	// AccessURL is the publishable URL the adapter resolved for that entry. It is
	// empty when nothing is publishable yet.
	AccessURL string
	// ReadinessEvidenceRef is the provider evidence identity for a ready report.
	ReadinessEvidenceRef string
	// ObservedAt is when the runtime reported this state.
	ObservedAt time.Time
}

// RuntimeRecord is Serve's own persisted runtime-instance fact.
type RuntimeRecord struct {
	ID              string
	WorkspaceID     string
	DeploymentID    string
	RuntimeID       string
	Status          string
	AccessURL       string
	ReadinessRef    string
	ObservedAt      time.Time
	ExecutionEpoch  int64
	ApplicationOpen bool
}

// ErrRuntimeObservationRefused reports an observation Serve will not record. It is
// returned alongside a stable reason so a caller can act on the specific refusal
// instead of retrying forever.
type ErrRuntimeObservationRefused struct{ Reason string }

func (e ErrRuntimeObservationRefused) Error() string { return e.Reason }

// Refusal reasons. Each names a distinct, actionable condition.
const (
	// ReasonIdentityMismatch: the observation does not belong to this deployment.
	ReasonIdentityMismatch = "runtime_observation_identity_mismatch"
	// ReasonStaleEpoch: the observation belongs to a superseded execution epoch.
	ReasonStaleEpoch = "runtime_observation_stale_epoch"
	// ReasonUnknownState: the runtime reported no decidable state.
	ReasonUnknownState = "runtime_observation_unknown_state"
	// ReasonAppAccessUnavailable: the application runs but no publishable entry is
	// provable, so Serve cannot expose it. It never records that as ready.
	ReasonAppAccessUnavailable = "app_access_unavailable"
	// ReasonInvalidDescriptor: the supplied deployment descriptor cannot be
	// authenticated against its own digest.
	ReasonInvalidDescriptor = "runtime_descriptor_invalid"
)

func refuse(reason string) error { return ErrRuntimeObservationRefused{Reason: reason} }

// descriptorDigest is the digest of the descriptor's canonical public JSON bytes,
// the same encoding Build and Capability hash. A descriptor whose bytes do not
// reproduce the declared digest is refused rather than recorded.
func descriptorDigest(descriptor *api.DeploymentDescriptor) (string, error) {
	raw, err := publicjson.Marshal(descriptor)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// RecordDeploymentObservation persists one runtime observation against Serve's own
// deployment. The deployment row is Serve's; the command supplies the identities
// the runtime-instance row requires.
//
// It is idempotent for the same epoch and refuses to regress: an observation for
// an older epoch, a different workspace/deployment/runtime/descriptor, or an
// undecidable state never overwrites a recorded fact.
func (s *Service) RecordDeploymentObservation(ctx context.Context, cmd *api.RuntimeDeployCommand, observation RuntimeObservation) (*RuntimeRecord, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, dbError(err)
	}
	defer tx.Rollback()
	if err = lockWorkspace(ctx, tx, cmd.GetWorkspaceId()); err != nil {
		return nil, dbError(err)
	}
	record, err := recordDeploymentObservation(ctx, tx, cmd, observation)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, dbError(err)
	}
	return record, nil
}

// recordDeploymentObservation shares the transaction holding the owner lock with
// runtime execution and selection. A response-completion timestamp is evidence
// metadata, not a concurrency fence.
func recordDeploymentObservation(ctx context.Context, tx *sql.Tx, cmd *api.RuntimeDeployCommand, observation RuntimeObservation) (*RuntimeRecord, error) {
	if cmd == nil || strings.TrimSpace(cmd.GetWorkspaceId()) == "" || strings.TrimSpace(cmd.GetDeploymentId()) == "" || strings.TrimSpace(cmd.GetRuntimeInstanceId()) == "" || strings.TrimSpace(cmd.GetResourceSetId()) == "" || cmd.GetDeploymentDescriptor() == nil {
		return nil, status.Error(codes.InvalidArgument, "workspace, deployment, runtime, resource set and deployment descriptor are required")
	}
	digest, err := descriptorDigest(cmd.GetDeploymentDescriptor())
	if err != nil || digest != cmd.GetDeploymentDescriptorDigest() {
		return nil, refuse(ReasonInvalidDescriptor)
	}

	state, accessURL, readinessRef, observedAt, err := resolveObservationState(observation)
	if err != nil {
		return nil, err
	}
	if err := validateReserved(ctx, tx, cmd); err != nil {
		return nil, err
	}
	var previous sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT observed_at FROM serve.agent_runtime_instances WHERE id=$1 FOR UPDATE`, cmd.GetRuntimeInstanceId()).Scan(&previous); err != nil {
		return nil, dbError(err)
	}
	if previous.Valid && observedAt.Before(previous.Time) {
		return nil, refuse(ReasonStaleEpoch)
	}
	var url, ref any
	if accessURL != "" {
		url = accessURL
	}
	if readinessRef != "" {
		ref = readinessRef
	}
	_, err = tx.ExecContext(ctx, `UPDATE serve.agent_runtime_instances SET status=$2,access_url=$3,readiness_evidence_ref=$4,observed_at=$5,applied_model_configuration_version=$6,updated_at=now() WHERE id=$1`, cmd.GetRuntimeInstanceId(), state, url, ref, observedAt.UTC(), cmd.GetModelConfigurationVersion())
	if err != nil {
		return nil, dbError(err)
	}
	id := cmd.GetRuntimeInstanceId()

	return &RuntimeRecord{
		ID: id, WorkspaceID: cmd.GetWorkspaceId(), DeploymentID: cmd.GetDeploymentId(),
		RuntimeID: cmd.GetRuntimeInstanceId(), Status: state, AccessURL: accessURL,
		ReadinessRef: readinessRef, ObservedAt: observedAt, ExecutionEpoch: cmd.GetExecutionEpoch(),
		ApplicationOpen: state == "ready",
	}, nil
}

// resolveObservationState maps one runtime observation onto Serve's own status
// vocabulary. It is the single place that decides whether an Agent may be called
// ready, and it refuses rather than guesses.
//
// Serve's status vocabulary is fixed by serve.agent_runtime_instances: ready
// requires an access URL, readiness evidence and an observation time, so an
// application that is running but has no publishable entry cannot be recorded as
// ready. That is APP_ACCESS_UNAVAILABLE, not a success and not a failure of the
// application.
func resolveObservationState(observation RuntimeObservation) (state, accessURL, readinessRef string, observedAt time.Time, err error) {
	if observation.State == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_UNSPECIFIED {
		return "", "", "", time.Time{}, refuse(ReasonUnknownState)
	}
	if observation.ObservedAt.IsZero() {
		return "", "", "", time.Time{}, refuse(ReasonUnknownState)
	}
	switch observation.State {
	case api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY:
		// Application readiness needs all three: the runtime says the application
		// is available, it reports an entry, and a publishable URL plus readiness
		// evidence exist. Anything less is recorded as not-open, never as ready.
		entry := observation.ApplicationEntry
		publishable := entry != nil && strings.TrimSpace(observation.AccessURL) != "" && strings.TrimSpace(observation.ReadinessEvidenceRef) != ""
		if !publishable {
			return "", "", "", time.Time{}, refuse(ReasonAppAccessUnavailable)
		}
		if err := contracts.ValidateWorkspaceApplicationEntry(*entry); err != nil {
			return "", "", "", time.Time{}, refuse(ReasonAppAccessUnavailable)
		}
		return "ready", strings.TrimSpace(observation.AccessURL), strings.TrimSpace(observation.ReadinessEvidenceRef), observation.ObservedAt, nil
	case api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING:
		return "pending", "", "", observation.ObservedAt, nil
	case api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING:
		return "starting", "", "", observation.ObservedAt, nil
	case api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STOPPED:
		return "stopped", "", "", observation.ObservedAt, nil
	case api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED:
		return "failed", "", "", observation.ObservedAt, nil
	case api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATING:
		return "terminating", "", "", observation.ObservedAt, nil
	case api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_TERMINATED:
		return "terminated", "", "", observation.ObservedAt, nil
	default:
		return "", "", "", time.Time{}, refuse(ReasonUnknownState)
	}
}

// validateReserved is called with the workspace lock held. Only Reserve may
// allocate an epoch or runtime identity; runtime observations cannot advance it.
func validateReserved(ctx context.Context, tx *sql.Tx, cmd *api.RuntimeDeployCommand) error {
	var workspace, capability, artifact, runtime string
	var epoch, maxEpoch int64
	err := tx.QueryRowContext(ctx, `SELECT workspace_id,capability_version_id,artifact_digest,COALESCE(runtime_instance_id,''),execution_epoch FROM serve.agent_deployments WHERE id=$1 FOR UPDATE`, cmd.GetDeploymentId()).Scan(&workspace, &capability, &artifact, &runtime, &epoch)
	if err != nil {
		return dbError(err)
	}
	if workspace != cmd.GetWorkspaceId() || capability != cmd.GetCapabilityVersionId() || artifact != cmd.GetDeploymentDescriptor().GetArtifact().GetDigest() || runtime != cmd.GetRuntimeInstanceId() {
		return refuse(ReasonIdentityMismatch)
	}
	if epoch != cmd.GetExecutionEpoch() {
		return refuse(ReasonStaleEpoch)
	}
	if err = tx.QueryRowContext(ctx, `SELECT max(execution_epoch) FROM serve.agent_deployments WHERE workspace_id=$1`, workspace).Scan(&maxEpoch); err != nil {
		return dbError(err)
	}
	if maxEpoch != epoch {
		return refuse(ReasonStaleEpoch)
	}
	var resourceSet, attachment, digest, ref string
	var storedDescriptor []byte
	err = tx.QueryRowContext(ctx, `SELECT fabric_resource_set_id,COALESCE(data_attachment_contract->>'attachmentId',''),deployment_descriptor_digest,deployment_descriptor_object_ref,deployment_descriptor FROM serve.agent_runtime_instances WHERE id=$1 AND deployment_id=$2 AND workspace_id=$3 AND execution_epoch=$4 FOR UPDATE`, runtime, cmd.GetDeploymentId(), workspace, epoch).Scan(&resourceSet, &attachment, &digest, &ref, &storedDescriptor)
	if err != nil {
		return dbError(err)
	}
	descriptor := &api.DeploymentDescriptor{}
	if publicjson.Unmarshal(storedDescriptor, descriptor) != nil {
		return status.Error(codes.DataLoss, "persisted descriptor is invalid")
	}
	if resourceSet != cmd.GetResourceSetId() || attachment != cmd.GetDataAttachmentId() || digest != cmd.GetDeploymentDescriptorDigest() || ref != cmd.GetDeploymentDescriptorObjectRef() || !proto.Equal(descriptor, cmd.GetDeploymentDescriptor()) {
		return refuse(ReasonIdentityMismatch)
	}
	return nil
}
