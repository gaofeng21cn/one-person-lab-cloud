package ledger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	api "opl-cloud/packages/contracts/go/api"
)

var artifactDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// evidenceDigest derives the immutable artifact reference Ledger records for an
// event whose payload has no owner-supplied digest of its own. It is a function
// of the exact owner identities, so a changed policy version or kind can never
// reuse another publication's recorded reference.
func evidenceDigest(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func readinessReceiptStatus(event *api.EventEnvelope, p *api.RuntimeReadinessObservedEvent) (string, error) {
	if event == nil || p == nil || event.Owner != "serve" || strings.TrimSpace(p.RuntimeInstanceId) == "" || strings.TrimSpace(p.WorkspaceId) == "" || strings.TrimSpace(p.DeploymentId) == "" || event.AggregateId != p.DeploymentId || p.AppliedModelConfigurationVersion < 0 {
		return "", ErrInvalidReceiptInput
	}
	switch p.Outcome {
	case "confirmed":
		if !p.ApplicationAvailable || p.ReceiptId == nil || strings.TrimSpace(p.GetReceiptId()) == "" {
			return "", ErrInvalidReceiptInput
		}
		return "completed", nil
	case "rejected":
		if p.ApplicationAvailable || p.CredentialInjectionVerified {
			return "", ErrInvalidReceiptInput
		}
		return "failed", nil
	case "unknown":
		if p.ApplicationAvailable || p.CredentialInjectionVerified {
			return "", ErrInvalidReceiptInput
		}
		return "review_required", nil
	default:
		return "", ErrInvalidReceiptInput
	}
}

// observationReceiptStatus maps the contract's three-valued observation outcome
// onto the receipt store's status vocabulary. "unknown" is explicitly not
// "failed": it records that the effect is unresolved and needs readback.
func observationReceiptStatus(outcome string) (string, bool) {
	switch outcome {
	case "confirmed":
		return "completed", true
	case "rejected":
		return "failed", true
	case "unknown":
		return "review_required", true
	default:
		return "", false
	}
}

// accessObservedEvidence validates the Serve route-switch observation against
// the contract and the switch row it mirrors: the aggregate is the switch, the
// enums are exact and the generation counters are non-negative. routeReceiptId
// is optional in the contract, so when it is present it must be a real value
// rather than an empty string, and it stays part of the recorded digest.
func accessObservedEvidence(event *api.EventEnvelope, p *api.RouteObservedEvent) (string, string, error) {
	if event == nil || p == nil || event.Owner != "serve" ||
		strings.TrimSpace(p.WorkspaceId) == "" || strings.TrimSpace(p.SwitchId) == "" || event.AggregateId != p.SwitchId ||
		p.RouteGeneration < 0 || p.ExecutionEpoch < 0 || strings.TrimSpace(p.RouteRevision) == "" {
		return "", "", ErrInvalidReceiptInput
	}
	switch p.ActionKind {
	case "fence", "activate", "rollback":
	default:
		return "", "", ErrInvalidReceiptInput
	}
	target := strings.TrimSpace(p.GetTargetExecutionResourceId())
	if p.TargetExecutionResourceId != nil && target == "" {
		return "", "", ErrInvalidReceiptInput
	}
	routeReceipt := strings.TrimSpace(p.GetRouteReceiptId())
	if p.RouteReceiptId != nil && routeReceipt == "" {
		return "", "", ErrInvalidReceiptInput
	}
	receiptStatus, ok := observationReceiptStatus(p.Outcome)
	if !ok {
		return "", "", ErrInvalidReceiptInput
	}
	digest := evidenceDigest("route", p.WorkspaceId, p.SwitchId, p.ActionKind,
		strconv.FormatInt(p.RouteGeneration, 10), strconv.FormatInt(p.ExecutionEpoch, 10),
		p.RouteRevision, target, p.Outcome, routeReceipt)
	return receiptStatus, digest, nil
}

// resourcesObservedEvidence validates the Serve resource observation. The
// supervision probe and the resource set identity fix the aggregate as the
// resource set, not the action inside it. absenceConfirmed is the
// deletion-absence claim, so it is only consistent with a confirmed
// observation; receiptId stays optional exactly as the contract declares.
func resourcesObservedEvidence(event *api.EventEnvelope, p *api.ResourcesObservedEvent) (string, string, error) {
	if event == nil || p == nil || event.Owner != "serve" ||
		strings.TrimSpace(p.ResourceSetId) == "" || strings.TrimSpace(p.WorkspaceId) == "" || strings.TrimSpace(p.ResourceActionId) == "" ||
		event.AggregateId != p.ResourceSetId {
		return "", "", ErrInvalidReceiptInput
	}
	receipt := strings.TrimSpace(p.GetReceiptId())
	if p.ReceiptId != nil && receipt == "" {
		return "", "", ErrInvalidReceiptInput
	}
	receiptStatus, ok := observationReceiptStatus(p.Outcome)
	if !ok || p.AbsenceConfirmed && p.Outcome != "confirmed" {
		return "", "", ErrInvalidReceiptInput
	}
	digest := evidenceDigest("resources", p.ResourceSetId, p.WorkspaceId, p.ResourceActionId, p.Outcome, strconv.FormatBool(p.AbsenceConfirmed), receipt)
	return receiptStatus, digest, nil
}

var walletObservationPurposes = map[string]bool{
	"base_period": true, "upgrade_supplement": true, "base_period_delete": true,
	"upgrade_failure_full": true, "supplement_delete_unused": true,
	"next_period_plan_failure_full": true, "recharge": true,
}

// walletObservedEvidence records the Gateway's own wallet observation as
// metadata only. It never proves that money moved: the paid-charge proof stays
// the separate WALLET_ACTION path, which re-reads a confirmed charge from the
// Gateway owner. Here the decoder validates the contract shape, enum membership
// and optional-reference completeness; it writes no business or wallet state.
func walletObservedEvidence(event *api.EventEnvelope, p *api.WalletOperationObservedEvent) (string, string, error) {
	if event == nil || p == nil || event.Owner != "gateway" ||
		strings.TrimSpace(p.WalletOperationId) == "" || strings.TrimSpace(p.WorkspaceId) == "" ||
		event.AggregateId != p.WalletOperationId || p.AmountUsdMicros < 0 {
		return "", "", ErrInvalidReceiptInput
	}
	switch p.Kind {
	case "charge", "refund":
	default:
		return "", "", ErrInvalidReceiptInput
	}
	status := ""
	switch p.Status {
	case "requested":
		status = "running"
	case "confirmed":
		status = "completed"
	case "rejected":
		status = "failed"
	case "unknown":
		status = "review_required"
	default:
		return "", "", ErrInvalidReceiptInput
	}
	// The optional fields are independently optional under the event contract, so
	// an absent field is accepted while a present-but-empty string or an
	// out-of-enum purpose is not: minLength/enum are contract facts, and the
	// Gateway owner's table-level business rules are never re-judged here.
	external := strings.TrimSpace(p.GetExternalReference())
	if p.ExternalReference != nil && external == "" {
		return "", "", ErrInvalidReceiptInput
	}
	receipt := strings.TrimSpace(p.GetReceiptId())
	if p.ReceiptId != nil && receipt == "" {
		return "", "", ErrInvalidReceiptInput
	}
	purpose := strings.TrimSpace(p.GetPurpose())
	if p.Purpose != nil && !walletObservationPurposes[purpose] {
		return "", "", ErrInvalidReceiptInput
	}
	planChange := strings.TrimSpace(p.GetPlanChangeId())
	if p.PlanChangeId != nil && planChange == "" {
		return "", "", ErrInvalidReceiptInput
	}
	originalCharge := strings.TrimSpace(p.GetOriginalChargeOperationId())
	if p.OriginalChargeOperationId != nil && originalCharge == "" {
		return "", "", ErrInvalidReceiptInput
	}
	// The contract lists coverage start/end as independently optional timestamps,
	// so a present pair must be valid and ordered; an inverted window cannot come
	// from a real Gateway row.
	coverageStart, coverageEnd := "", ""
	if p.CoverageStart != nil || p.CoverageEnd != nil {
		if p.CoverageStart == nil || p.CoverageEnd == nil || p.CoverageStart.CheckValid() != nil || p.CoverageEnd.CheckValid() != nil || !p.CoverageEnd.AsTime().After(p.CoverageStart.AsTime()) {
			return "", "", ErrInvalidReceiptInput
		}
		coverageStart = strconv.FormatInt(p.CoverageStart.AsTime().UnixNano(), 10)
		coverageEnd = strconv.FormatInt(p.CoverageEnd.AsTime().UnixNano(), 10)
	}
	digest := evidenceDigest("wallet", p.WalletOperationId, p.WorkspaceId, p.Kind, p.Status,
		strconv.FormatInt(p.AmountUsdMicros, 10), external, receipt, purpose, planChange, originalCharge, coverageStart, coverageEnd)
	return status, digest, nil
}

// RecordDomainEvent is called only by the authenticated domain Inbox. It shares
// the existing immutable receipt/idempotency store without inventing a Workspace
// for build-time evidence. Generic receipt HTTP writes cannot enter this path.
func (s *PostgresStore) RecordDomainEvent(ctx context.Context, event *api.EventEnvelope) (Receipt, error) {
	if event == nil || event.EventId == "" || event.SchemaVersion != 1 || event.RequestId == "" || event.AggregateVersion < 1 || event.OccurredAt == nil || event.OccurredAt.CheckValid() != nil {
		return Receipt{}, ErrInvalidReceiptInput
	}
	// Contract scope never drifts from the payload: a tenant event must carry its
	// tenant, and the one platform-scope event Ledger consumes is the catalog
	// policy publication, which carries its own policy version identity.
	switch event.Scope {
	case "tenant":
		if event.TenantId == "" {
			return Receipt{}, ErrInvalidReceiptInput
		}
	case "platform":
		if event.TenantId != "" || event.EventType != "catalog.policy_changed.v1" {
			return Receipt{}, ErrInvalidReceiptInput
		}
	default:
		return Receipt{}, ErrInvalidReceiptInput
	}
	job, digest, status, workspaceID := "", "", "completed", ""
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
	case "serve.agent_readiness_observed.v1":
		p := event.GetRuntimeReadinessObserved()
		var err error
		status, err = readinessReceiptStatus(event, p)
		if err != nil {
			return Receipt{}, ErrInvalidReceiptInput
		}
		job = p.DeploymentId
	case "catalog.policy_changed.v1":
		p := event.GetCatalogPolicyChanged()
		if event.Owner != "resource_catalog" || event.Scope != "platform" || p == nil || strings.TrimSpace(p.PolicyVersionId) == "" || strings.TrimSpace(p.PolicyKind) == "" || event.AggregateId != p.PolicyVersionId || p.ValidFrom == nil || p.ValidFrom.CheckValid() != nil {
			return Receipt{}, ErrInvalidReceiptInput
		}
		digest = evidenceDigest(p.PolicyVersionId, p.PolicyKind)
	case "serve.access_observed.v1":
		p := event.GetRouteObserved()
		var err error
		status, digest, err = accessObservedEvidence(event, p)
		if err != nil {
			return Receipt{}, ErrInvalidReceiptInput
		}
		workspaceID = p.WorkspaceId
	case "fabric.resources_observed.v1":
		p := event.GetResourcesObserved()
		var err error
		status, digest, err = resourcesObservedEvidence(event, p)
		if err != nil {
			return Receipt{}, ErrInvalidReceiptInput
		}
		workspaceID = p.WorkspaceId
	case "wallet.operation_observed.v1":
		p := event.GetWalletOperationObserved()
		var err error
		status, digest, err = walletObservedEvidence(event, p)
		if err != nil {
			return Receipt{}, ErrInvalidReceiptInput
		}
		workspaceID = p.WorkspaceId
	default:
		return Receipt{}, ErrInvalidReceiptInput
	}
	raw, err := protojson.Marshal(event)
	if err != nil {
		return Receipt{}, ErrInvalidReceiptInput
	}
	if workspaceID == "" {
		if p := event.GetRuntimeReadinessObserved(); p != nil {
			workspaceID = p.WorkspaceId
		}
	}
	organizationID := event.TenantId
	if event.Scope == "platform" {
		organizationID = "platform"
	}
	input := ReceiptInput{Type: event.EventType, Status: status, Surface: "cloud", OrganizationID: organizationID, WorkspaceID: workspaceID, JobID: job, ArtifactID: digest, RequestID: event.RequestId, Owner: map[string]any{"name": event.Owner}, InputRefs: map[string]any{"domainEvent": json.RawMessage(raw)}, IdempotencyKey: "domain:" + event.Owner + ":" + event.EventId}
	if containsForbiddenReceiptKey(input) {
		return Receipt{}, ErrInvalidReceiptInput
	}
	return s.recordReceipt(ctx, input)
}
