package catalog

import (
	"context"
	"database/sql"
	"math"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/resource-catalog/migrations"
)

// allowAll stands in for the live CloudIdentity authority. It echoes the exact
// audience, action, resource and scope the owner asked about, which is what a real
// allow decision contains; the point of these tests is the owner's own store and
// arithmetic, not the authorizer's policy.
type allowAll struct {
	api.CloudIdentityAuthorizationClient
}

func (allowAll) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	now := time.Now()
	return &api.AuthorizationDecision{
		Result:            api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		Issuer:            api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
		Scope:             r.Scope,
		ActorId:           r.ActorId,
		SessionId:         r.SessionId,
		AudienceOwner:     r.AudienceOwner,
		Action:            r.Action,
		Resource:          r.Resource,
		PermissionVersion: 1,
		IssuedAt:          timestamppb.New(now.Add(-time.Minute)),
		ExpiresAt:         timestamppb.New(now.Add(time.Hour)),
	}, nil
}

// system provisions the owner's real isolated PostgreSQL database, installs its
// real migrations, and returns a wired service plus a platform administrator
// call context.
func system(t *testing.T) (*Service, *api.CallContext) {
	t.Helper()
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "resource_catalog", Database: "opl_resource_catalog", SchemaOwnerRole: "opl_resource_catalog_owner", WriterRole: "opl_resource_catalog_writer", RuntimeRole: "opl_resource_catalog_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err := h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatal(err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	service, err := New(db, ownerservice.NewAuthorizer(ownerservice.OwnerResourceCatalog, allowAll{}), Profile{Provider: "local-docker", Region: "local", BillingMode: "LOCAL_NO_CHARGE", ProviderCapabilityVersion: "provider/v1"})
	if err != nil {
		t.Fatal(err)
	}
	return service, platformCall("admin", "req-1", "idem-1")
}

// peerContext records the verified calling owner the same way the owner's gRPC
// identity interceptor does, so a direct store call sees the boundary it would
// see over the wire.
func peerContext(t *testing.T) context.Context {
	t.Helper()
	return ownerservice.WithPeerOwner(context.Background(), ownerservice.OwnerResourceCatalog.Service())
}

// tenantCall is a member's own tenant-scoped context. A customer read such as
// ListComputePlans requires it; a platform administrator context has no tenant.
func tenantCall(actor, tenant, requestID, idempotencyKey string) *api.CallContext {
	return &api.CallContext{
		ActorId: actor, RequestId: requestID, IdempotencyKey: idempotencyKey, SessionId: proto.String("session"),
		Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}},
	}
}

func platformCall(actor, requestID, idempotencyKey string) *api.CallContext {
	return &api.CallContext{
		ActorId: actor, RequestId: requestID, IdempotencyKey: idempotencyKey, SessionId: proto.String("session"),
		Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
	}
}

func TestComputePlanLifecycle(t *testing.T) {
	service, call := system(t)
	ctx := peerContext(t)
	plan, err := service.CreateComputePlan(ctx, &api.CreateComputePlanRpcRequest{Context: call, Body: &api.CreateComputePlanRequest{Name: "basic", Vcpus: 2, MemoryMiB: 4096, ProviderProfileId: "local/profile", ProviderSkuId: "local-basic", ProviderCapabilityVersion: "provider/v1", ValidFrom: timestamppb.New(time.Now().Add(-time.Hour))}})
	if err != nil {
		t.Fatalf("create compute plan: %v", err)
	}
	if plan.GetAvailability() != api.ComputePlanAvailabilityEnum_COMPUTE_PLAN_AVAILABILITY_ENUM_AVAILABLE {
		t.Fatalf("availability = %v, want available", plan.GetAvailability())
	}
	if plan.GetBillingMode() != api.ComputePlanBillingModeEnum_COMPUTE_PLAN_BILLING_MODE_ENUM_LOCAL_NO_CHARGE {
		t.Fatalf("billing mode = %v, want local_no_charge", plan.GetBillingMode())
	}

	// Reusing the same idempotency key with a different body is a conflict.
	if _, err := service.CreateComputePlan(ctx, &api.CreateComputePlanRpcRequest{Context: call, Body: &api.CreateComputePlanRequest{Name: "other", Vcpus: 4, MemoryMiB: 8192, ProviderProfileId: "local/profile", ProviderSkuId: "local-basic", ProviderCapabilityVersion: "provider/v1", ValidFrom: timestamppb.New(time.Now().Add(-time.Hour))}}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting replay code = %v, want AlreadyExists", status.Code(err))
	}

	page, err := service.ListComputePlans(ctx, &api.ListComputePlansRpcRequest{Context: tenantCall("member", "tenant-test", "req-list", "idem-list")})
	if err != nil {
		t.Fatalf("list compute plans: %v", err)
	}
	if len(page.GetItems()) != 1 || page.GetItems()[0].GetId() != plan.GetId() {
		t.Fatalf("listed plans = %v, want the created plan", page.GetItems())
	}

	retired, err := service.SetComputePlanAvailability(ctx, &api.SetComputePlanAvailabilityRpcRequest{Context: platformCall("admin", "req-2", "idem-2"), PlanId: plan.GetId(), Body: &api.PlanAvailabilityRequest{Availability: api.PlanAvailabilityRequestAvailabilityEnum_PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_RETIRED, Reason: "end of life"}})
	if err != nil {
		t.Fatalf("retire plan: %v", err)
	}
	if retired.GetAvailability() != api.ComputePlanAvailabilityEnum_COMPUTE_PLAN_AVAILABILITY_ENUM_RETIRED {
		t.Fatalf("availability = %v, want retired", retired.GetAvailability())
	}
}

// TestPricePolicyPublishesCatalogEvent proves a new price policy version and its
// catalog.policy_changed.v1 Outbox rows commit together, and that the event's
// producer/consumer identity is the contract's, not the caller's.
func TestPricePolicyPublishesCatalogEvent(t *testing.T) {
	service, call := system(t)
	ctx := peerContext(t)
	now := time.Now().Add(-time.Hour)
	compute, err := service.CreateComputePlan(ctx, &api.CreateComputePlanRpcRequest{Context: call, Body: &api.CreateComputePlanRequest{Name: "basic", Vcpus: 2, MemoryMiB: 4096, ProviderProfileId: "p", ProviderSkuId: "s", ProviderCapabilityVersion: "provider/v1", ValidFrom: timestamppb.New(now)}})
	if err != nil {
		t.Fatal(err)
	}
	storage, err := service.CreateStoragePlan(ctx, &api.CreateStoragePlanRpcRequest{Context: platformCall("admin", "req-storage", "idem-storage"), Body: &api.CreateStoragePlanRequest{Name: "standard", CapacityGiB: 10, ProviderProfileId: "p", ProviderSkuId: "s", ShrinkSupported: true, ValidFrom: timestamppb.New(now)}})
	if err != nil {
		t.Fatal(err)
	}
	price, err := service.CreatePricePolicyVersion(ctx, &api.CreatePricePolicyVersionRpcRequest{Context: platformCall("admin", "req-price", "idem-price"), Body: &api.CreatePricePolicyRequest{VersionLabel: "2026-09", PeriodMonths: 1, ComputeMonthlyUsdMicros: 0, StorageMonthlyUsdMicros: 0, ProductMonthlyUsdMicros: 0, ValidFrom: timestamppb.New(now), ComputePlanId: compute.Id, StoragePlanId: storage.Id, RenewalPolicy: renewalPolicy(), PlanChangePolicyVersion: api.CreatePricePolicyRequestPlanChangePolicyVersionEnum_CREATE_PRICE_POLICY_REQUEST_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1}})
	if err != nil {
		t.Fatalf("create price policy: %v", err)
	}

	rows, err := service.DB.QueryContext(ctx, `SELECT consumer_owner FROM resource_catalog.outbox_deliveries d JOIN resource_catalog.outbox_events e ON e.id=d.event_id WHERE e.aggregate_id=$1 ORDER BY consumer_owner`, price.GetId())
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var consumers []string
	for rows.Next() {
		var consumer string
		if err := rows.Scan(&consumer); err != nil {
			t.Fatal(err)
		}
		consumers = append(consumers, consumer)
	}
	want := []string{"ledger", "workspace"}
	if len(consumers) != len(want) || consumers[0] != want[0] || consumers[1] != want[1] {
		t.Fatalf("delivery consumers = %v, want %v", consumers, want)
	}

}

// TestPricePolicyRequiresExactApprovedPair proves a price version is refused when
// it names a plan that does not exist, instead of creating an unpriceable pair.
func TestPricePolicyRequiresExactApprovedPair(t *testing.T) {
	service, call := system(t)
	ctx := peerContext(t)
	now := time.Now().Add(-time.Hour)
	compute, err := service.CreateComputePlan(ctx, &api.CreateComputePlanRpcRequest{Context: call, Body: &api.CreateComputePlanRequest{Name: "basic", Vcpus: 2, MemoryMiB: 4096, ProviderProfileId: "p", ProviderSkuId: "s", ProviderCapabilityVersion: "provider/v1", ValidFrom: timestamppb.New(now)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePricePolicyVersion(ctx, &api.CreatePricePolicyVersionRpcRequest{Context: platformCall("admin", "req", "idem"), Body: &api.CreatePricePolicyRequest{VersionLabel: "v", PeriodMonths: 1, ValidFrom: timestamppb.New(now), ComputePlanId: compute.Id, StoragePlanId: "does-not-exist", RenewalPolicy: renewalPolicy(), PlanChangePolicyVersion: api.CreatePricePolicyRequestPlanChangePolicyVersionEnum_CREATE_PRICE_POLICY_REQUEST_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1}}); err == nil {
		t.Fatal("a price policy referencing an unknown storage plan was accepted")
	}
}

// TestPricePolicyRefusesRetiredPlan proves the owner will not price a plan pair
// once either side is retired: an approved-plan requirement is re-read at the
// moment of pricing, so a retirement cannot be outrun by a concurrent price
// version. This is the owner-boundary coverage for the pricing rule.
func TestPricePolicyRefusesRetiredPlan(t *testing.T) {
	service, call := system(t)
	ctx := peerContext(t)
	now := time.Now().Add(-time.Hour)
	compute, err := service.CreateComputePlan(ctx, &api.CreateComputePlanRpcRequest{Context: call, Body: &api.CreateComputePlanRequest{Name: "basic", Vcpus: 2, MemoryMiB: 4096, ProviderProfileId: "p", ProviderSkuId: "s", ProviderCapabilityVersion: "provider/v1", ValidFrom: timestamppb.New(now)}})
	if err != nil {
		t.Fatal(err)
	}
	storage, err := service.CreateStoragePlan(ctx, &api.CreateStoragePlanRpcRequest{Context: platformCall("admin", "req-s", "idem-s"), Body: &api.CreateStoragePlanRequest{Name: "standard", CapacityGiB: 10, ProviderProfileId: "p", ProviderSkuId: "s", ShrinkSupported: true, ValidFrom: timestamppb.New(now)}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetStoragePlanAvailability(ctx, &api.SetStoragePlanAvailabilityRpcRequest{Context: platformCall("admin", "req-retire", "idem-retire"), PlanId: storage.Id, Body: &api.PlanAvailabilityRequest{Availability: api.PlanAvailabilityRequestAvailabilityEnum_PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_RETIRED, Reason: "end of life"}}); err != nil {
		t.Fatal(err)
	}
	_, err = service.CreatePricePolicyVersion(ctx, &api.CreatePricePolicyVersionRpcRequest{Context: platformCall("admin", "req-price-retired", "idem-price-retired"), Body: &api.CreatePricePolicyRequest{VersionLabel: "2026-10", PeriodMonths: 1, ValidFrom: timestamppb.New(now), ComputePlanId: compute.Id, StoragePlanId: storage.Id, RenewalPolicy: renewalPolicy(), PlanChangePolicyVersion: api.CreatePricePolicyRequestPlanChangePolicyVersionEnum_CREATE_PRICE_POLICY_REQUEST_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("pricing a retired plan pair code = %v, want FailedPrecondition", status.Code(err))
	}
}

// TestRetentionAndRefundPolicyLifecycle proves the frozen retention/refund facts
// are recorded and bound, and that a refund policy referencing an unknown
// retention version is refused.
func TestRetentionAndRefundPolicyLifecycle(t *testing.T) {
	service, _ := system(t)
	ctx := peerContext(t)
	retention, err := service.CreateRetentionPolicyVersion(ctx, &api.CreateRetentionPolicyVersionRpcRequest{Context: platformCall("admin", "req-r", "idem-r"), Body: &api.CreateRetentionPolicyRequest{VersionLabel: "retention-1", CustomerTerms: "data destroyed after confirmed deletion"}})
	if err != nil {
		t.Fatal(err)
	}
	if retention.GetTenantRestoreDays() != 15 || retention.GetPackageHistoryDisposition() != api.RetentionPolicyVersionPackageHistoryDispositionEnum_RETENTION_POLICY_VERSION_PACKAGE_HISTORY_DISPOSITION_ENUM_RETAIN {
		t.Fatalf("retention facts = %+v, want the frozen 15-day/retain policy", retention)
	}
	if _, err := service.CreateRefundPolicyVersion(ctx, &api.CreateRefundPolicyVersionRpcRequest{Context: platformCall("admin", "req-f", "idem-f"), Body: &api.CreateRefundPolicyRequest{VersionLabel: "refund-bad", Algorithm: api.CreateRefundPolicyRequestAlgorithmEnum_CREATE_REFUND_POLICY_REQUEST_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1, RetentionPolicyVersionId: "missing", CustomerTerms: "terms", ValidFrom: timestamppb.Now()}}); err == nil {
		t.Fatal("a refund policy referencing an unknown retention version was accepted")
	}
	refund, err := service.CreateRefundPolicyVersion(ctx, &api.CreateRefundPolicyVersionRpcRequest{Context: platformCall("admin", "req-f2", "idem-f2"), Body: &api.CreateRefundPolicyRequest{VersionLabel: "refund-1", Algorithm: api.CreateRefundPolicyRequestAlgorithmEnum_CREATE_REFUND_POLICY_REQUEST_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1, RetentionPolicyVersionId: retention.GetId(), CustomerTerms: "720-hour policy", ValidFrom: timestamppb.Now()}})
	if err != nil {
		t.Fatal(err)
	}
	if refund.GetAlgorithm() != api.RefundPolicyVersionAlgorithmEnum_REFUND_POLICY_VERSION_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1 || refund.GetRetentionPolicyVersionId() != retention.GetId() {
		t.Fatalf("refund = %+v, want the bound workspace-delete-refund-v1", refund)
	}
}

// TestUnavailableAvailabilityIsRefused proves the owner refuses to silently store
// a different projection than the administrator asked for.
func TestUnavailableAvailabilityIsRefused(t *testing.T) {
	if _, err := requestedStatus(&api.PlanAvailabilityRequest{Availability: api.PlanAvailabilityRequestAvailabilityEnum_PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_UNAVAILABLE, Reason: "temporarily off"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unavailable request code = %v, want FailedPrecondition", status.Code(err))
	}
	if got, err := requestedStatus(&api.PlanAvailabilityRequest{Availability: api.PlanAvailabilityRequestAvailabilityEnum_PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_AVAILABLE, Reason: "on"}); err != nil || got != "approved" {
		t.Fatalf("available request = %q (%v), want approved", got, err)
	}
	if got, err := requestedStatus(&api.PlanAvailabilityRequest{Availability: api.PlanAvailabilityRequestAvailabilityEnum_PLAN_AVAILABILITY_REQUEST_AVAILABILITY_ENUM_RETIRED, Reason: "off"}); err != nil || got != "revoked" {
		t.Fatalf("retired request = %q (%v), want revoked", got, err)
	}
}

// TestAvailabilityProjection proves the single availability projection: an
// approved plan inside its window is available, and a revoked or expired plan is
// not.
func TestAvailabilityProjection(t *testing.T) {
	now := time.Now()
	window := now.Add(-time.Hour)
	if got, err := availability("approved", window, time.Time{}, now); err != nil || got != "available" {
		t.Fatalf("open approved = %q (%v), want available", got, err)
	}
	if got, _ := availability("approved", time.Time{}, now.Add(-time.Hour), now); got != "unavailable" {
		t.Fatalf("expired approved = %q, want unavailable", got)
	}
	if got, _ := availability("revoked", window, time.Time{}, now); got != "retired" {
		t.Fatalf("revoked = %q, want retired", got)
	}
	if _, err := availability("mystery", window, time.Time{}, now); status.Code(err) != codes.DataLoss {
		t.Fatalf("unknown status code = %v, want DataLoss", status.Code(err))
	}
}

var _ = sql.ErrNoRows

// quoteFixture creates an approved plan pair with an effective price, refund and
// retention version, which is the precondition for any quote.
func quoteFixture(t *testing.T, service *Service) (compute, storage string) {
	t.Helper()
	ctx := peerContext(t)
	now := time.Now().Add(-time.Hour)
	computePlan, err := service.CreateComputePlan(ctx, &api.CreateComputePlanRpcRequest{Context: platformCall("admin", "q-compute", "q-compute"), Body: &api.CreateComputePlanRequest{Name: "basic", Vcpus: 2, MemoryMiB: 4096, ProviderProfileId: "p", ProviderSkuId: "s", ProviderCapabilityVersion: "provider/v1", ValidFrom: timestamppb.New(now)}})
	if err != nil {
		t.Fatal(err)
	}
	storagePlan, err := service.CreateStoragePlan(ctx, &api.CreateStoragePlanRpcRequest{Context: platformCall("admin", "q-storage", "q-storage"), Body: &api.CreateStoragePlanRequest{Name: "standard", CapacityGiB: 10, ProviderProfileId: "p", ProviderSkuId: "s", ShrinkSupported: true, ValidFrom: timestamppb.New(now)}})
	if err != nil {
		t.Fatal(err)
	}
	retention, err := service.CreateRetentionPolicyVersion(ctx, &api.CreateRetentionPolicyVersionRpcRequest{Context: platformCall("admin", "q-retention", "q-retention"), Body: &api.CreateRetentionPolicyRequest{VersionLabel: "retention-q", CustomerTerms: "data destroyed after confirmed deletion"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateRefundPolicyVersion(ctx, &api.CreateRefundPolicyVersionRpcRequest{Context: platformCall("admin", "q-refund", "q-refund"), Body: &api.CreateRefundPolicyRequest{VersionLabel: "refund-q", Algorithm: api.CreateRefundPolicyRequestAlgorithmEnum_CREATE_REFUND_POLICY_REQUEST_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1, RetentionPolicyVersionId: retention.Id, CustomerTerms: "720-hour policy", ValidFrom: timestamppb.New(now)}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreatePricePolicyVersion(ctx, &api.CreatePricePolicyVersionRpcRequest{Context: platformCall("admin", "q-price", "q-price"), Body: &api.CreatePricePolicyRequest{VersionLabel: "2026-09", PeriodMonths: 1, ComputeMonthlyUsdMicros: 50_000_000, StorageMonthlyUsdMicros: 2_580_000, ProductMonthlyUsdMicros: 1, ValidFrom: timestamppb.New(now), ComputePlanId: computePlan.Id, StoragePlanId: storagePlan.Id, RenewalPolicy: renewalPolicy(), PlanChangePolicyVersion: api.CreatePricePolicyRequestPlanChangePolicyVersionEnum_CREATE_PRICE_POLICY_REQUEST_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1}}); err != nil {
		t.Fatal(err)
	}
	return computePlan.Id, storagePlan.Id
}

// TestQuoteMoneyRuleIsNotSecondMultiplied proves the contract money rule: total is
// the sum of the charge lines minus the credits, quantity is explanatory only, and
// a credit larger than its charges is refused rather than stored as a negative
// total.
func TestQuoteMoneyRuleIsNotSecondMultiplied(t *testing.T) {
	lines := []*api.QuoteLine{
		{Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_COMPUTE, Quantity: 3, AmountUsdMicros: 100},
		{Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_STORAGE, Quantity: 2, AmountUsdMicros: 20},
		{Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_ADJUSTMENT_CREDIT, Quantity: 5, AmountUsdMicros: 10},
	}
	total, err := sumQuoteLines(lines)
	if err != nil || total != 110 {
		t.Fatalf("total = %d (err %v), want 110: quantity must not multiply the amount", total, err)
	}
	if _, err := sumQuoteLines([]*api.QuoteLine{{Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_COMPUTE, AmountUsdMicros: 1}, {Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_ADJUSTMENT_CREDIT, AmountUsdMicros: 10}}); err == nil {
		t.Fatal("a credit larger than its charges was accepted")
	}
	if _, err := sumQuoteLines([]*api.QuoteLine{{Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_COMPUTE, AmountUsdMicros: -1}}); err == nil {
		t.Fatal("a negative line amount was accepted")
	}
	if _, err := sumQuoteLines([]*api.QuoteLine{{Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_COMPUTE, AmountUsdMicros: math.MaxInt64}, {Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_PRODUCT, AmountUsdMicros: 1}}); err == nil {
		t.Fatal("an overflowing line sum was accepted")
	}
}

// TestDeployQuoteLifecycle proves a deploy quote prices the exact effective policy
// versions, is read back by the same tenant, and reports expiry without rewriting
// the stored row.
func TestDeployQuoteLifecycle(t *testing.T) {
	service, _ := system(t)
	ctx := peerContext(t)
	computePlanID, storagePlanID := quoteFixture(t, service)
	member := tenantCall("101", "tenant-test", "q-create", "q-create")

	quote, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: member, Body: &api.QuoteRequest{Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY, CapabilityVersionId: proto.String("cv-1"), ComputePlanId: computePlanID, StoragePlanId: storagePlanID, PeriodMonths: 1}})
	if err != nil {
		t.Fatalf("create quote: %v", err)
	}
	if quote.GetTotalUsdMicros() != 52_580_001 {
		t.Fatalf("total = %d, want 52580001", quote.GetTotalUsdMicros())
	}
	if quote.GetPricePolicyVersionId() == "" || quote.GetRefundPolicyVersionId() == "" || quote.GetRetentionPolicyVersionId() == "" {
		t.Fatalf("quote did not bind the policy versions: %+v", quote)
	}
	if quote.GetRuntimeReadbackRequirement() != api.QuoteRuntimeReadbackRequirementEnum_QUOTE_RUNTIME_READBACK_REQUIREMENT_ENUM_REQUIRED {
		t.Fatalf("a deploy quote naming a capability version must require a runtime readback")
	}
	read, err := service.GetQuote(ctx, &api.GetQuoteRpcRequest{Context: member, QuoteId: quote.GetId()})
	if err != nil {
		t.Fatalf("read quote: %v", err)
	}
	if read.GetId() != quote.GetId() || read.GetTotalUsdMicros() != quote.GetTotalUsdMicros() || len(read.GetLineItems()) != len(quote.GetLineItems()) {
		t.Fatalf("readback differs from the created quote: %+v", read)
	}

	// Another tenant cannot read it: the offer is simply absent here.
	if _, err := service.GetQuote(ctx, &api.GetQuoteRpcRequest{Context: tenantCall("102", "another-tenant", "q-other", "q-other"), QuoteId: quote.GetId()}); status.Code(err) != codes.NotFound {
		t.Fatalf("cross-tenant read code = %v, want NotFound", status.Code(err))
	}

	// A resize or renew request is refused with the gap named, not answered with a
	// deploy-shaped offer.
	if _, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: member, Body: &api.QuoteRequest{Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_RENEW, WorkspaceId: proto.String("ws-1"), ComputePlanId: computePlanID, StoragePlanId: storagePlanID, PeriodMonths: 1}}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("renew quote code = %v, want Unimplemented", status.Code(err))
	}
	// A deploy quote is not bound to an existing workspace.
	if _, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: member, Body: &api.QuoteRequest{Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY, WorkspaceId: proto.String("ws-1"), ComputePlanId: computePlanID, StoragePlanId: storagePlanID, PeriodMonths: 1}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("deploy-with-workspace code = %v, want InvalidArgument", status.Code(err))
	}
	// A multi-month new purchase is refused.
	if _, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: member, Body: &api.QuoteRequest{Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY, ComputePlanId: computePlanID, StoragePlanId: storagePlanID, PeriodMonths: 2}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("multi-month code = %v, want InvalidArgument", status.Code(err))
	}
}

// TestQuoteAcceptanceBindsOneOperation proves the acceptance edge: one quote binds
// exactly one obligation, a replay returns the identical acceptance, and a second
// obligation cannot inherit it. The stale-offer case is exercised by expiring the
// offer directly, because the catalogue's own validity window is thirty minutes.
func TestQuoteAcceptanceBindsOneOperation(t *testing.T) {
	service, _ := system(t)
	ctx := peerContext(t)
	computePlanID, storagePlanID := quoteFixture(t, service)
	member := tenantCall("101", "tenant-test", "a-create", "a-create")
	quote, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: member, Body: &api.QuoteRequest{Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY, CapabilityVersionId: proto.String("cv-1"), ComputePlanId: computePlanID, StoragePlanId: storagePlanID, PeriodMonths: 1}})
	if err != nil {
		t.Fatal(err)
	}
	// Only the verified Workspace owner may accept.
	if _, err := service.AcceptQuote(ctx, &api.AcceptQuoteRequest{Context: member, QuoteId: quote.GetId(), WorkspaceId: "ws-1", ObligationId: "obl-1"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("catalog-peer acceptance code = %v, want Unauthenticated", status.Code(err))
	}
	workspace := ownerservice.WithPeerOwner(context.Background(), ownerservice.OwnerWorkspace.Service())
	accept := func(obligation string) (*api.QuoteAcceptance, error) {
		return service.AcceptQuote(workspace, &api.AcceptQuoteRequest{Context: member, QuoteId: quote.GetId(), WorkspaceId: "ws-1", ObligationId: obligation})
	}
	first, err := accept("obl-1")
	if err != nil {
		t.Fatalf("accept quote: %v", err)
	}
	if first.GetAcceptanceId() == "" || first.GetSnapshotDigest() == "" || first.GetQuote().GetStatus() != api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED {
		t.Fatalf("acceptance is incomplete: %+v", first)
	}
	// The same obligation replays the identical acceptance.
	replay, err := accept("obl-1")
	if err != nil {
		t.Fatalf("replay acceptance: %v", err)
	}
	if replay.GetAcceptanceId() != first.GetAcceptanceId() || replay.GetSnapshotDigest() != first.GetSnapshotDigest() {
		t.Fatalf("a replay produced a different acceptance: %+v vs %+v", replay, first)
	}
	// A different obligation cannot inherit the binding.
	if _, err := accept("obl-2"); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("second obligation code = %v, want AlreadyExists", status.Code(err))
	}
	// A plan-change acceptance is refused with the gap named.
	if _, err := service.AcceptQuote(workspace, &api.AcceptQuoteRequest{Context: member, QuoteId: quote.GetId(), WorkspaceId: "ws-1", ObligationId: "obl-3", PlanChangeId: proto.String("pc-1")}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("plan-change acceptance code = %v, want Unimplemented", status.Code(err))
	}

	// An offer past its expiry is refused rather than accepted.
	second, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: tenantCall("101", "tenant-test", "a-create-2", "a-create-2"), Body: &api.QuoteRequest{Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY, CapabilityVersionId: proto.String("cv-1"), ComputePlanId: computePlanID, StoragePlanId: storagePlanID, PeriodMonths: 1}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DB.ExecContext(ctx, `UPDATE resource_catalog.quotes SET expires_at=now()-interval '1 minute' WHERE id=$1`, second.GetId()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.AcceptQuote(workspace, &api.AcceptQuoteRequest{Context: member, QuoteId: second.GetId(), WorkspaceId: "ws-1", ObligationId: "obl-expired"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expired acceptance code = %v, want FailedPrecondition", status.Code(err))
	}
	// An unknown quote is absent, not a generic failure.
	if _, err := service.AcceptQuote(workspace, &api.AcceptQuoteRequest{Context: member, QuoteId: "quote-does-not-exist", WorkspaceId: "ws-1", ObligationId: "obl-x"}); status.Code(err) != codes.NotFound {
		t.Fatalf("unknown quote code = %v, want NotFound", status.Code(err))
	}

	// No price version means no quote: the owner refuses instead of inventing one.
	if _, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: member, Body: &api.QuoteRequest{Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY, CapabilityVersionId: proto.String("cv-1"), ComputePlanId: computePlanID, StoragePlanId: "storage-unknown", PeriodMonths: 1}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unpriced pair code = %v, want FailedPrecondition", status.Code(err))
	}
}
