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
	"encoding/json"
	"errors"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
	if cmd == nil || strings.TrimSpace(cmd.GetWorkspaceId()) == "" || strings.TrimSpace(cmd.GetDeploymentId()) == "" || strings.TrimSpace(cmd.GetRuntimeInstanceId()) == "" || strings.TrimSpace(cmd.GetResourceSetId()) == "" || cmd.GetDeploymentDescriptor() == nil {
		return nil, status.Error(codes.InvalidArgument, "workspace, deployment, runtime, resource set and deployment descriptor are required")
	}
	digest, err := descriptorDigest(cmd.GetDeploymentDescriptor())
	if err != nil || digest != cmd.GetDeploymentDescriptorDigest() {
		return nil, refuse(ReasonInvalidDescriptor)
	}
	descriptorRaw, err := publicjson.Marshal(cmd.GetDeploymentDescriptor())
	if err != nil {
		return nil, refuse(ReasonInvalidDescriptor)
	}

	// Serve's own deployment row is the authority for the workspace, artifact and
	// current epoch. The command cannot introduce a deployment Serve never wrote.
	var (
		deploymentWorkspace string
		artifactDigest      string
		deploymentEpoch     int64
	)
	err = s.DB.QueryRowContext(ctx, `
		SELECT workspace_id, artifact_digest, execution_epoch
		FROM serve.agent_deployments WHERE id = $1`, cmd.GetDeploymentId()).
		Scan(&deploymentWorkspace, &artifactDigest, &deploymentEpoch)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "Serve has no deployment %s", cmd.GetDeploymentId())
	}
	if err != nil {
		return nil, dbError(err)
	}
	if deploymentWorkspace != cmd.GetWorkspaceId() {
		return nil, refuse(ReasonIdentityMismatch)
	}
	if artifactDigest != cmd.GetDeploymentDescriptor().GetArtifact().GetDigest() {
		return nil, refuse(ReasonIdentityMismatch)
	}
	if cmd.GetExecutionEpoch() < deploymentEpoch {
		return nil, refuse(ReasonStaleEpoch)
	}

	state, accessURL, readinessRef, observedAt, err := resolveObservationState(observation)
	if err != nil {
		return nil, err
	}

	// Fencing: a row for this deployment at a newer epoch is never regressed, and a
	// same-epoch replay converges on the latest report instead of creating a
	// second instance.
	var existingEpoch sql.NullInt64
	var existingID string
	err = s.DB.QueryRowContext(ctx, `SELECT id, execution_epoch FROM serve.agent_runtime_instances WHERE deployment_id = $1`, cmd.GetDeploymentId()).Scan(&existingID, &existingEpoch)
	switch {
	case err == nil && existingEpoch.Int64 > cmd.GetExecutionEpoch():
		return nil, refuse(ReasonStaleEpoch)
	case err != nil && !errors.Is(err, sql.ErrNoRows):
		return nil, dbError(err)
	}

	id := existingID
	if id == "" {
		id = "rti_" + cmd.GetDeploymentId()
	}
	var url, ref, observed any
	if accessURL != "" {
		url = accessURL
	}
	if readinessRef != "" {
		ref = readinessRef
	}
	if !observedAt.IsZero() {
		observed = observedAt.UTC()
	}
	// data_attachment_contract is Serve's own column and 02 declares no source
	// shape for it, so Serve records exactly what it was given: the opaque
	// attachment identity. It is not a second contract for another owner's fact.
	attachmentContract, err := json.Marshal(map[string]string{"attachmentId": cmd.GetDataAttachmentId()})
	if err != nil {
		return nil, dbError(err)
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO serve.agent_runtime_instances
			(id, workspace_id, deployment_id, artifact_digest, fabric_resource_set_id, status, access_url,
			 data_attachment_contract, readiness_evidence_ref, observed_at, execution_epoch,
			 deployment_descriptor, deployment_descriptor_digest, deployment_descriptor_object_ref)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		ON CONFLICT (deployment_id) DO UPDATE SET
			status = EXCLUDED.status, access_url = EXCLUDED.access_url,
			readiness_evidence_ref = EXCLUDED.readiness_evidence_ref, observed_at = EXCLUDED.observed_at,
			execution_epoch = EXCLUDED.execution_epoch, fabric_resource_set_id = EXCLUDED.fabric_resource_set_id,
			deployment_descriptor = EXCLUDED.deployment_descriptor,
			deployment_descriptor_digest = EXCLUDED.deployment_descriptor_digest,
			deployment_descriptor_object_ref = EXCLUDED.deployment_descriptor_object_ref,
			updated_at = now()`,
		id, cmd.GetWorkspaceId(), cmd.GetDeploymentId(), artifactDigest, cmd.GetResourceSetId(), state, url,
		attachmentContract, ref, observed, cmd.GetExecutionEpoch(),
		descriptorRaw, digest, cmd.GetDeploymentDescriptorObjectRef())
	if err != nil {
		return nil, dbError(err)
	}
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
