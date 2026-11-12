package catalog

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
)

var planPairCoordinateMismatches = []struct{ column, value string }{
	{"provider", "tencent"},
	{"provider_profile_ref", "other-profile"},
	{"region", "other-region"},
	{"billing_mode", "PREPAID_MONTHLY"},
	{"provider_capability_version", "provider/v2"},
}

func planPairPolicyCounts(t *testing.T, service *Service) [3]int64 {
	t.Helper()
	var counts [3]int64
	if err := service.DB.QueryRowContext(t.Context(), `SELECT
		(SELECT count(*) FROM resource_catalog.price_policy_versions),
		(SELECT count(*) FROM resource_catalog.outbox_events),
		(SELECT count(*) FROM resource_catalog.outbox_deliveries)`).Scan(&counts[0], &counts[1], &counts[2]); err != nil {
		t.Fatal(err)
	}
	return counts
}

func assertPlanPairCoordinateMismatch(t *testing.T, service *Service, compute, storage, column string) {
	t.Helper()
	var equal [5]bool
	if err := service.DB.QueryRowContext(t.Context(), `SELECT
		c.provider=s.provider, c.provider_profile_ref=s.provider_profile_ref,
		c.region=s.region, c.billing_mode=s.billing_mode,
		c.provider_capability_version=s.provider_capability_version
		FROM resource_catalog.compute_plans c JOIN resource_catalog.storage_plans s ON s.id=$2
		WHERE c.id=$1`, compute, storage).Scan(&equal[0], &equal[1], &equal[2], &equal[3], &equal[4]); err != nil {
		t.Fatal(err)
	}
	for i, coordinate := range planPairCoordinateMismatches {
		if equal[i] != (coordinate.column != column) {
			t.Fatalf("persisted %s equality = %v, want only %s to differ", coordinate.column, equal[i], column)
		}
	}
}

func planPairComputeRead(t *testing.T, service *Service, compute string) *api.ComputePlan {
	t.Helper()
	page, err := service.ListComputePlans(peerContext(t), &api.ListComputePlansRpcRequest{
		Context: tenantCall("member", "tenant-test", "pair-offers", ""),
	})
	if err != nil || len(page.GetItems()) != 1 || page.Items[0].Id != compute {
		t.Fatalf("compute offer read = %v, %v", page, err)
	}
	return page.Items[0]
}

func TestPlanPairPricePolicyRejectsIncompatiblePersistedCoordinates(t *testing.T) {
	for _, tc := range planPairCoordinateMismatches {
		t.Run(tc.column, func(t *testing.T) {
			service, _ := system(t)
			compute, storage := monthlyPricePlans(t, service, "pair")
			if tc.column == "billing_mode" {
				// This fixture isolates the persisted billing coordinate, which is
				// supplied by the instance rather than the plan-create request.
				if _, err := service.DB.ExecContext(t.Context(), `UPDATE resource_catalog.storage_plans SET billing_mode=$1 WHERE id=$2`, tc.value, storage); err != nil {
					t.Fatal(err)
				}
			} else {
				profile, profileID := service.Profile, "p"
				switch tc.column {
				case "provider":
					service.Profile.Provider = tc.value
				case "provider_profile_ref":
					profileID = tc.value
				case "region":
					service.Profile.Region = tc.value
				case "provider_capability_version":
					service.Profile.ProviderCapabilityVersion = tc.value
				}
				plan, err := service.CreateStoragePlan(peerContext(t), &api.CreateStoragePlanRpcRequest{
					Context: platformCall("admin", "other-storage", "other-storage"),
					Body: &api.CreateStoragePlanRequest{Name: "other-storage", CapacityGiB: 10,
						ProviderProfileId: profileID, ProviderSkuId: "s", ValidFrom: timestamppb.New(time.Now().Add(-time.Hour))},
				})
				service.Profile = profile
				if err != nil {
					t.Fatalf("create approved storage with different %s: %v", tc.column, err)
				}
				storage = plan.Id
			}
			assertPlanPairCoordinateMismatch(t, service, compute, storage, tc.column)
			before := planPairPolicyCounts(t, service)
			_, err := service.CreatePricePolicyVersion(peerContext(t), &api.CreatePricePolicyVersionRpcRequest{
				Context: platformCall("admin", "incompatible-price", "incompatible-price"),
				Body:    monthlyPriceRequest("incompatible-price", compute, storage, 100, time.Now().Add(-time.Hour), nil),
			})
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("price admission with mismatched %s = %v, want FailedPrecondition", tc.column, err)
			}
			if after := planPairPolicyCounts(t, service); after != before {
				t.Fatalf("rejected price changed policy/event/delivery counts: %v -> %v", before, after)
			}
		})
	}
}

func TestPlanPairHistoricalIncompatibilityIsNotOfferedOrQuoted(t *testing.T) {
	for _, tc := range planPairCoordinateMismatches {
		t.Run(tc.column, func(t *testing.T) {
			service, _ := system(t)
			compute, storage := quoteFixture(t, service)
			assertPlanPairCoordinateMismatch(t, service, compute, storage, "")
			policy := workspaceOfferPrice(t, service, "compatible-price", compute, storage, [3]int64{100, 20, 3}, time.Now().Add(-30*time.Minute), nil)
			assertWorkspaceOffers(t, planPairComputeRead(t, service, compute), &api.WorkspacePlanOffer{
				StoragePlanId: storage, StorageName: "standard", StorageCapacityGiB: 10,
				PricePolicyVersionId: policy.Id, MonthlyUsdMicros: 123,
			})
			request := &api.QuoteRequest{Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY,
				CapabilityVersionId: proto.String("cv-1"), ComputePlanId: compute, StoragePlanId: storage, PeriodMonths: 1}
			quote, err := service.CreateQuote(peerContext(t), &api.CreateQuoteRpcRequest{
				Context: tenantCall("member", "tenant-test", "compatible-quote", "compatible-quote"), Body: request,
			})
			if err != nil || quote.GetPricePolicyVersionId() != policy.Id || quote.GetTotalUsdMicros() != 123 {
				t.Fatalf("compatible pair quote = %v, %v; want published policy %s and total 123", quote, err, policy.Id)
			}
			before := planPairPolicyCounts(t, service)
			historyRequest := &api.ListPricePolicyVersionsRpcRequest{Context: platformCall("admin", "price-history", "")}
			beforeHistory, err := service.ListPricePolicyVersions(peerContext(t), historyRequest)
			if err != nil || len(beforeHistory.GetItems()) != 2 {
				t.Fatalf("published price history = %v, %v; want both legal versions", beforeHistory, err)
			}

			// Historical fixture: the old owner could persist a price for this
			// incompatible pair. Change only one coordinate after legal publication;
			// do not route the fixture through the new admission predicate.
			if _, err := service.DB.ExecContext(t.Context(), "UPDATE resource_catalog.storage_plans SET "+tc.column+"=$1 WHERE id=$2", tc.value, storage); err != nil {
				t.Fatal(err)
			}
			assertPlanPairCoordinateMismatch(t, service, compute, storage, tc.column)
			assertWorkspaceOffers(t, planPairComputeRead(t, service, compute))
			// Use a new idempotency key: replaying the successful quote is not a
			// fresh admission and must not be used to test current compatibility.
			_, err = service.CreateQuote(peerContext(t), &api.CreateQuoteRpcRequest{
				Context: tenantCall("member", "tenant-test", "historical-quote", "historical-quote"), Body: request,
			})
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("historical pair with mismatched %s quote = %v, want FailedPrecondition", tc.column, err)
			}
			history, err := service.ListPricePolicyVersions(peerContext(t), historyRequest)
			if err != nil || !proto.Equal(history, beforeHistory) {
				t.Fatalf("historical prices changed: got %v, %v; want %v", history, err, beforeHistory)
			}
			if after := planPairPolicyCounts(t, service); after != before {
				t.Fatalf("historical reads changed policy/event/delivery counts: %v -> %v", before, after)
			}
		})
	}
}
