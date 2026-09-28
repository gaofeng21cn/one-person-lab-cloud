package ledger

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	api "opl-cloud/packages/contracts/go/api"
)

var artifactDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func validateRuntimeReadinessEvent(event *api.EventEnvelope) (string, error) {
	p := event.GetRuntimeReadinessObserved()
	if event.Owner != "serve" || p == nil || p.RuntimeInstanceId == "" || p.WorkspaceId == "" || p.DeploymentId == "" || event.AggregateId != p.RuntimeInstanceId || (p.Outcome != "confirmed" && p.Outcome != "rejected" && p.Outcome != "unknown") || (p.Outcome == "confirmed" && (!p.ApplicationAvailable || p.GetReceiptId() == "")) {
		return "", ErrInvalidReceiptInput
	}
	return p.DeploymentId, nil
}

// RecordDomainEvent commits one authenticated owner event as append-only evidence.
// The transport Inbox calls the Tx form so Inbox, receipt and Ledger Outbox commit
// atomically before the producer receives an ACK.
func (s *PostgresStore) RecordDomainEvent(ctx context.Context, event *api.EventEnvelope) (Receipt, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Receipt{}, err
	}
	defer tx.Rollback()
	receipt, err := s.RecordDomainEventTx(ctx, tx, event)
	if err != nil {
		return Receipt{}, err
	}
	if err = tx.Commit(); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

// RecordDomainEventTx is the Ledger owner transaction boundary for a typed event.
func (s *PostgresStore) RecordDomainEventTx(ctx context.Context, tx *sql.Tx, event *api.EventEnvelope) (Receipt, error) {
	input, err := domainEventReceiptInput(event)
	if err != nil {
		return Receipt{}, err
	}
	receipt, err := s.recordReceiptTx(ctx, tx, input)
	if err != nil {
		return Receipt{}, err
	}
	if !receipt.Replayed {
		if err = s.appendReceiptRecordedEventTx(ctx, tx, event, receipt); err != nil {
			return Receipt{}, err
		}
	}
	return receipt, nil
}

func domainEventReceiptInput(event *api.EventEnvelope) (ReceiptInput, error) {
	if event == nil || event.EventId == "" || event.SchemaVersion != 1 || event.Scope != "tenant" || event.TenantId == "" || event.RequestId == "" || event.AggregateVersion < 1 || event.OccurredAt == nil || event.OccurredAt.CheckValid() != nil {
		return ReceiptInput{}, ErrInvalidReceiptInput
	}
	job, digest, status := "", "", "completed"
	switch event.EventType {
	case "package.uploaded.v1":
		p := event.GetPackageUploaded()
		if event.Owner != "capability" || p == nil || p.PackageId == "" || p.PackageVersionId == "" || event.AggregateId != p.PackageVersionId || p.SizeBytes <= 0 || !artifactDigest.MatchString(p.Sha256) {
			return ReceiptInput{}, ErrInvalidReceiptInput
		}
		digest = p.Sha256
	case "build.artifact_confirmed.v1":
		p := event.GetBuildArtifactConfirmed()
		if event.Owner != "build" || p == nil || p.BuildJobId == "" || event.AggregateId != p.BuildJobId || p.PackageVersionId == "" || p.RuntimeVersionId == "" || p.WebuiVersionId == "" || p.ArtifactReceiptId == "" || !artifactDigest.MatchString(p.ArtifactDigest) || !artifactDigest.MatchString(p.DeploymentDescriptorDigest) {
			return ReceiptInput{}, ErrInvalidReceiptInput
		}
		job, digest = p.BuildJobId, p.ArtifactDigest
	case "build.failed.v1":
		p := event.GetBuildFailed()
		if event.Owner != "build" || p == nil || p.BuildJobId == "" || event.AggregateId != p.BuildJobId || api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+strings.ToUpper(p.ErrorCode)] == 0 {
			return ReceiptInput{}, ErrInvalidReceiptInput
		}
		job, status = p.BuildJobId, "failed"
	case "capability.version_registered.v1":
		p := event.GetCapabilityVersionRegistered()
		if event.Owner != "capability" || p == nil || p.BuildJobId == "" || p.CapabilityVersionId == "" || event.AggregateId != p.CapabilityVersionId || !artifactDigest.MatchString(p.ArtifactDigest) {
			return ReceiptInput{}, ErrInvalidReceiptInput
		}
		job, digest = p.BuildJobId, p.ArtifactDigest
	case "serve.agent_readiness_observed.v1":
		validatedJob, validationErr := validateRuntimeReadinessEvent(event)
		if validationErr != nil {
			return ReceiptInput{}, validationErr
		}
		job = validatedJob
	default:
		return ReceiptInput{}, ErrInvalidReceiptInput
	}
	raw, err := protojson.Marshal(event)
	if err != nil {
		return ReceiptInput{}, ErrInvalidReceiptInput
	}
	input := ReceiptInput{Type: event.EventType, Status: status, Surface: "cloud", OrganizationID: event.TenantId, JobID: job, ArtifactID: digest, RequestID: event.RequestId, Owner: map[string]any{"name": event.Owner}, InputRefs: map[string]any{"domainEvent": json.RawMessage(raw)}, IdempotencyKey: "domain:" + event.Owner + ":" + event.EventId}
	if containsForbiddenReceiptKey(input) {
		return ReceiptInput{}, ErrInvalidReceiptInput
	}
	return input, nil
}
