package ledger

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	api "opl-cloud/packages/contracts/go/api"
)

var artifactDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// RecordDomainEvent is called only by the authenticated domain Inbox. It shares
// the existing immutable receipt/idempotency store without inventing a Workspace
// for build-time evidence. Generic receipt HTTP writes cannot enter this path.
func (s *PostgresStore) RecordDomainEvent(ctx context.Context, event *api.EventEnvelope) (Receipt, error) {
	if event == nil || event.EventId == "" || event.SchemaVersion != 1 || event.Scope != "tenant" || event.TenantId == "" || event.RequestId == "" || event.AggregateVersion < 1 || event.OccurredAt == nil || event.OccurredAt.CheckValid() != nil {
		return Receipt{}, ErrInvalidReceiptInput
	}
	job, digest, status := "", "", "completed"
	switch event.EventType {
	case "package.uploaded.v1":
		p := event.GetPackageUploaded()
		if event.Owner != "capability" || p == nil || p.PackageId == "" || p.PackageVersionId == "" || event.AggregateId != p.PackageVersionId || p.SizeBytes <= 0 || !artifactDigest.MatchString(p.Sha256) {
			return Receipt{}, ErrInvalidReceiptInput
		}
		digest = p.Sha256

	case "build.artifact_confirmed.v1":
		p := event.GetBuildArtifactConfirmed()
		if event.Owner != "build" || p == nil || p.BuildJobId == "" || event.AggregateId != p.BuildJobId || p.PackageVersionId == "" || p.RuntimeVersionId == "" || p.WebuiVersionId == "" || p.ArtifactReceiptId == "" || !artifactDigest.MatchString(p.ArtifactDigest) || !artifactDigest.MatchString(p.DeploymentDescriptorDigest) {
			return Receipt{}, ErrInvalidReceiptInput
		}
		job, digest = p.BuildJobId, p.ArtifactDigest
	case "build.failed.v1":
		p := event.GetBuildFailed()
		if event.Owner != "build" || p == nil || p.BuildJobId == "" || event.AggregateId != p.BuildJobId || api.ErrorCodeEnum_value["ERROR_CODE_ENUM_"+strings.ToUpper(p.ErrorCode)] == 0 {
			return Receipt{}, ErrInvalidReceiptInput
		}
		job, status = p.BuildJobId, "failed"
	case "capability.version_registered.v1":
		p := event.GetCapabilityVersionRegistered()
		if event.Owner != "capability" || p == nil || p.BuildJobId == "" || p.CapabilityVersionId == "" || event.AggregateId != p.CapabilityVersionId || !artifactDigest.MatchString(p.ArtifactDigest) {
			return Receipt{}, ErrInvalidReceiptInput
		}
		job, digest = p.BuildJobId, p.ArtifactDigest
	default:
		return Receipt{}, ErrInvalidReceiptInput
	}
	raw, err := protojson.Marshal(event)
	if err != nil {
		return Receipt{}, ErrInvalidReceiptInput
	}
	input := ReceiptInput{Type: event.EventType, Status: status, Surface: "cloud", OrganizationID: event.TenantId, JobID: job, ArtifactID: digest, RequestID: event.RequestId, Owner: map[string]any{"name": event.Owner}, InputRefs: map[string]any{"domainEvent": json.RawMessage(raw)}, IdempotencyKey: "domain:" + event.Owner + ":" + event.EventId}
	if containsForbiddenReceiptKey(input) {
		return Receipt{}, ErrInvalidReceiptInput
	}
	return s.recordReceipt(ctx, input)
}
