package launch

import (
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

func orderFixture() (ownerstore.Operation, *api.QuoteAcceptance, *api.QuoteAcceptance) {
	op := ownerstore.Operation{ID: "operation-original", ResourceID: "workspace-original", ActorID: "actor-original", TenantID: "tenant-original", RequestID: "request-original"}
	offer := &api.QuoteAcceptance{Quote: &api.Quote{Id: "quote-original", Status: api.QuoteStatusEnum_QUOTE_STATUS_ENUM_OFFERED, Purpose: api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY, CapabilityVersionId: proto.String("capability-original"), ComputePlanId: "compute-original", StoragePlanId: "storage-original", PeriodMonths: 1, TotalUsdMicros: 1234567, ExpiresAt: timestamppb.New(time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC))}, ResourcePlan: &api.ResourcePlanSnapshot{ComputePlanId: "compute-original", StoragePlanId: "storage-original", ProviderComputeSkuId: "sku-original", PrepaidMonths: 1}}
	accepted := proto.Clone(offer).(*api.QuoteAcceptance)
	accepted.Quote.Status = api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED
	accepted.WorkspaceId, accepted.ObligationId = op.ResourceID, op.ID
	accepted.AcceptanceId, accepted.SnapshotDigest = "acceptance-original", "sha256:original"
	return op, offer, accepted
}

func TestAcceptedOrderRejectsCrossOwnerDrift(t *testing.T) {
	op, offer, accepted := orderFixture()
	if err := validateAcceptance(op, offer, accepted); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*api.QuoteAcceptance){
		"price":      func(a *api.QuoteAcceptance) { a.Quote.TotalUsdMicros++ },
		"capability": func(a *api.QuoteAcceptance) { a.Quote.CapabilityVersionId = proto.String("other") },
		"plan":       func(a *api.QuoteAcceptance) { a.ResourcePlan.ProviderComputeSkuId = "other" },
		"quote":      func(a *api.QuoteAcceptance) { a.Quote.Id = "other" },
		"workspace":  func(a *api.QuoteAcceptance) { a.WorkspaceId = "other" },
		"obligation": func(a *api.QuoteAcceptance) { a.ObligationId = "other" },
		"status":     func(a *api.QuoteAcceptance) { a.Quote.Status = api.QuoteStatusEnum_QUOTE_STATUS_ENUM_OFFERED },
		"digest":     func(a *api.QuoteAcceptance) { a.SnapshotDigest = "" },
	} {
		t.Run(name, func(t *testing.T) {
			altered := proto.Clone(accepted).(*api.QuoteAcceptance)
			change(altered)
			if status.Code(validateAcceptance(op, offer, altered)) != codes.DataLoss {
				t.Fatal("accepted order drift was not refused")
			}
		})
	}
}

func TestContinuationPreservesOriginalIdentityWithoutSession(t *testing.T) {
	op, _, _ := orderFixture()
	first := continuation(op, "grant-original", "ensure_resources")
	recovered := continuation(op, "grant-original", "ensure_resources")
	if !proto.Equal(first, recovered) || first.IdempotencyKey != "operation-original:ensure_resources" || first.ActorId != op.ActorID || first.Scope.GetTenant().GetTenantId() != op.TenantID {
		t.Fatal("recovery changed original command identity")
	}
	if first.SessionId != nil || first.DeadlineAt != nil || first.AuthorizationContextId != "" || first.GetAcceptedOperationGrantId() != "grant-original" {
		t.Fatal("recovery must rely only on the bounded durable grant")
	}
	if first.IdempotencyKey == continuation(op, "grant-original", "accept_quote").IdempotencyKey {
		t.Fatal("different owner commands share an idempotency key")
	}
}

func TestCatalogRejectionIsDistinctFromLostResponse(t *testing.T) {
	for _, tc := range []struct {
		step        string
		code        codes.Code
		state       string
		observation string
	}{
		{"accept_quote", codes.FailedPrecondition, "failed", "rejected"},
		{"accept_quote", codes.AlreadyExists, "failed", "rejected"},
		{"accept_quote", codes.Unavailable, "awaiting_confirmation", "unknown"},
		{"accept_quote", codes.DeadlineExceeded, "awaiting_confirmation", "unknown"},
		{"ensure_resources", codes.FailedPrecondition, "awaiting_confirmation", "unknown"},
		{"read_resources", codes.DataLoss, "needs_attention", "unknown"},
	} {
		t.Run(tc.step+"/"+tc.code.String(), func(t *testing.T) {
			state, observation, _ := callFailure(tc.step, status.Error(tc.code, "owner result"))
			if state != tc.state || observation != tc.observation {
				t.Fatalf("got %s/%s, want %s/%s", state, observation, tc.state, tc.observation)
			}
		})
	}
}

func TestZeroChargeEvidenceRequiresExactLocalOrder(t *testing.T) {
	op, _, accepted := orderFixture()
	accepted.Quote.TotalUsdMicros = 0
	accepted.ResourcePlan.Provider = "local-docker"
	accepted.ResourcePlan.BillingMode = "LOCAL_NO_CHARGE"
	commit := &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: op.ID, ResourceId: op.ResourceID, AcceptedInputDigest: "sha256:input"}
	receipt := &api.Receipt{Id: "receipt-original", Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(op.ID), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, CreatedAt: timestamppb.Now()}
	evidence := &api.LocalNoChargeReceiptEvidence{Receipt: receipt, QuoteAcceptance: accepted, OwnerCommitEvidence: commit, EvidenceDigest: accepted.SnapshotDigest}
	if err := validateZeroChargeEvidence(op, accepted, commit, evidence, receipt); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*api.LocalNoChargeReceiptEvidence){
		"operation": func(e *api.LocalNoChargeReceiptEvidence) { e.Receipt.OperationId = proto.String("other") },
		"owner":     func(e *api.LocalNoChargeReceiptEvidence) { e.Receipt.Owner = api.OwnerEnum_OWNER_ENUM_GATEWAY },
		"outcome": func(e *api.LocalNoChargeReceiptEvidence) {
			e.Receipt.Outcome = api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_UNKNOWN
		},
		"quote":     func(e *api.LocalNoChargeReceiptEvidence) { e.QuoteAcceptance.Quote.Id = "other" },
		"commit":    func(e *api.LocalNoChargeReceiptEvidence) { e.OwnerCommitEvidence.AcceptedInputDigest = "other" },
		"digest":    func(e *api.LocalNoChargeReceiptEvidence) { e.EvidenceDigest = "other" },
		"id":        func(e *api.LocalNoChargeReceiptEvidence) { e.Receipt.Id = "" },
		"timestamp": func(e *api.LocalNoChargeReceiptEvidence) { e.Receipt.CreatedAt = nil },
	} {
		t.Run(name, func(t *testing.T) {
			changed := proto.Clone(evidence).(*api.LocalNoChargeReceiptEvidence)
			mutate(changed)
			if status.Code(validateZeroChargeEvidence(op, accepted, commit, changed, receipt)) != codes.DataLoss {
				t.Fatal("different zero-charge evidence was accepted")
			}
		})
	}
	paid := proto.Clone(accepted).(*api.QuoteAcceptance)
	paid.Quote.TotalUsdMicros = 1
	if localNoCharge(paid) {
		t.Fatal("nonzero quote treated as no charge")
	}
	paid = proto.Clone(accepted).(*api.QuoteAcceptance)
	paid.ResourcePlan.BillingMode = "PREPAID_MONTHLY"
	if localNoCharge(paid) {
		t.Fatal("ordinary quote treated as Local no charge")
	}
	paid = proto.Clone(accepted).(*api.QuoteAcceptance)
	paid.ResourcePlan.Provider = "tencent"
	if localNoCharge(paid) {
		t.Fatal("remote provider treated as Local no charge")
	}
}

type workspaceRow struct {
	state    string
	accepted acceptedOrder
	result   orderResult
}

func (r workspaceRow) Scan(values ...any) error {
	*values[0].(*string) = "workspace-original"
	*values[1].(*string) = "tenant-original"
	*values[2].(*string) = "Original workspace"
	*values[3].(*string) = r.state
	*values[4].(*string) = "compute-original"
	*values[5].(*string) = "storage-original"
	*values[6].(*time.Time) = time.Date(2026, 9, 26, 0, 0, 0, 0, time.UTC)
	*values[7].(*time.Time) = *values[6].(*time.Time)
	*values[8].(*int64) = 7
	*values[9].(*[]byte), _ = json.Marshal(r.accepted)
	*values[10].(*[]byte), _ = json.Marshal(r.result)
	return nil
}

func TestWorkspaceReadbackDoesNotInventEntitlementOrAccess(t *testing.T) {
	_, offer, _ := orderFixture()
	for _, tc := range []struct {
		name      string
		state     string
		outcome   api.Observation
		status    api.WorkspaceStatusEnum
		readiness api.WorkspaceResourceReadinessEnum
	}{
		{"unknown", "provisioning", api.Observation_OBSERVATION_UNKNOWN, api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_PROVISIONING, api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_UNKNOWN},
		{"resources confirmed", "provisioning", api.Observation_OBSERVATION_CONFIRMED, api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_PROVISIONING, api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_READY},
		{"rejected", "failed", api.Observation_OBSERVATION_REJECTED, api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_FAILED, api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_UNAVAILABLE},
		{"suspended", "suspended", api.Observation_OBSERVATION_UNKNOWN, api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_SUSPENDED, api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_UNKNOWN},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readback := &api.ResourceReadback{ResourceSetId: "resources-original", WorkspaceId: "workspace-original", Outcome: tc.outcome}
			row := workspaceRow{state: tc.state, accepted: acceptedOrder{Quote: wire(offer)}, result: orderResult{ResourceSetID: readback.ResourceSetId, ResourceReadback: wire(readback)}}
			w, tenant, err := scanWorkspace(row)
			if err != nil {
				t.Fatal(err)
			}
			if w.Status != tc.status || w.ResourceReadiness != tc.readiness || w.Version != 7 || tenant != "tenant-original" || w.GetCapabilityVersionId() != offer.Quote.GetCapabilityVersionId() {
				t.Fatalf("stored Workspace facts lost: %v", w)
			}
			if w.CurrentPeriodEnd != nil || w.AccessUrl != nil || w.ApplicationAvailability == api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_AVAILABLE {
				t.Fatal("resource intent was promoted into an entitlement or application access")
			}
		})
	}
	row := workspaceRow{state: "active", accepted: acceptedOrder{Quote: wire(offer)}, result: orderResult{ResourceSetID: "resources-original", ResourceReadback: wire(&api.ResourceReadback{ResourceSetId: "resources-other", WorkspaceId: "workspace-original", Outcome: api.Observation_OBSERVATION_CONFIRMED})}}
	if _, _, err := scanWorkspace(row); status.Code(err) != codes.DataLoss {
		t.Fatal("mismatched resource readback was not refused")
	}
	row = workspaceRow{state: "invented", accepted: acceptedOrder{Quote: wire(offer)}}
	if _, _, err := scanWorkspace(row); status.Code(err) != codes.DataLoss {
		t.Fatal("unknown persisted status was silently converted")
	}
}
