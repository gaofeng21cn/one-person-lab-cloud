package catalog

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

func monthlyPricePlans(t *testing.T, service *Service, label string) (string, string) {
	t.Helper()
	ctx := peerContext(t)
	from := timestamppb.New(time.Now().Add(-24 * time.Hour))
	compute, err := service.CreateComputePlan(ctx, &api.CreateComputePlanRpcRequest{
		Context: platformCall("admin", label+"-compute", label+"-compute"),
		Body:    &api.CreateComputePlanRequest{Name: label, Vcpus: 2, MemoryMiB: 4096, ProviderProfileId: "p", ProviderSkuId: "s", ProviderCapabilityVersion: "provider/v1", ValidFrom: from},
	})
	if err != nil {
		t.Fatal(err)
	}
	storage, err := service.CreateStoragePlan(ctx, &api.CreateStoragePlanRpcRequest{
		Context: platformCall("admin", label+"-storage", label+"-storage"),
		Body:    &api.CreateStoragePlanRequest{Name: label, CapacityGiB: 10, ProviderProfileId: "p", ProviderSkuId: "s", ValidFrom: from},
	})
	if err != nil {
		t.Fatal(err)
	}
	return compute.Id, storage.Id
}

func monthlyPriceRequest(label, compute, storage string, amount int64, from time.Time, until *timestamppb.Timestamp) *api.CreatePricePolicyRequest {
	return &api.CreatePricePolicyRequest{
		VersionLabel: label, ComputePlanId: compute, StoragePlanId: storage, PeriodMonths: 1,
		ComputeMonthlyUsdMicros: amount, StorageMonthlyUsdMicros: amount / 10, ProductMonthlyUsdMicros: 7,
		ValidFrom: timestamppb.New(from), ValidUntil: until, RenewalPolicy: renewalPolicy(),
		PlanChangePolicyVersion: api.CreatePricePolicyRequestPlanChangePolicyVersionEnum_CREATE_PRICE_POLICY_REQUEST_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1,
	}
}

// Exercise the real migrated owner store, including optional presence: zero is
// a published amount, while an absent or expired price is not zero.
func TestPlanMonthlyPriceValidity(t *testing.T) {
	now := time.Now().UTC()
	type version struct {
		amount int64
		from   time.Time
		until  *timestamppb.Timestamp
	}
	old := version{100, now.Add(-3 * time.Hour), nil}
	current := version{200, now.Add(-2 * time.Hour), timestamppb.New(now.Add(time.Hour))}
	expired := version{300, now.Add(-time.Hour), timestamppb.New(now.Add(-time.Minute))}
	future := version{400, now.Add(time.Hour), nil}
	for _, tc := range []struct {
		name     string
		versions []version
		want     int // -1 means both optional fields must be absent.
	}{
		{"missing", nil, -1},
		{"future-only", []version{future}, -1},
		{"zero", []version{{0, now.Add(-time.Hour), nil}}, 0},
		{"large-exact-money", []version{{9_007_199_254_740_993, now.Add(-time.Hour), nil}}, 0},
		{"latest", []version{old, current}, 1},
		{"future-does-not-shadow-current", []version{old, future}, 0},
		{"expired-does-not-revive-old", []version{old, expired, future}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, _ := system(t)
			compute, storage := monthlyPricePlans(t, service, tc.name)
			policies := make([]*api.PricePolicyVersion, 0, len(tc.versions))
			for i, v := range tc.versions {
				label := fmt.Sprintf("price-%d", i)
				policy, err := service.CreatePricePolicyVersion(peerContext(t), &api.CreatePricePolicyVersionRpcRequest{
					Context: platformCall("admin", label, label), Body: monthlyPriceRequest(label, compute, storage, v.amount, v.from, v.until),
				})
				if err != nil {
					t.Fatal(err)
				}
				policies = append(policies, policy)
			}
			call := tenantCall("member", "tenant-test", "list", "")
			computes, err := service.ListComputePlans(peerContext(t), &api.ListComputePlansRpcRequest{Context: call, QueryStoragePlanId: proto.String(storage)})
			if err != nil || len(computes.GetItems()) != 1 {
				t.Fatalf("compute read = %v, %v", computes, err)
			}
			storages, err := service.ListStoragePlans(peerContext(t), &api.ListStoragePlansRpcRequest{Context: call, QueryComputePlanId: proto.String(compute)})
			if err != nil || len(storages.GetItems()) != 1 {
				t.Fatalf("storage read = %v, %v", storages, err)
			}
			c, s := computes.Items[0], storages.Items[0]
			for i, plan := range []proto.Message{c, s} {
				raw, err := publicjson.Marshal(plan)
				if err != nil {
					t.Fatal(err)
				}
				var wire map[string]any
				if err := json.Unmarshal(raw, &wire); err != nil {
					t.Fatal(err)
				}
				amount, hasAmount := wire["monthlyPriceUSDMicros"]
				policy, hasPolicy := wire["pricePolicyVersionId"]
				if tc.want < 0 {
					if hasAmount || hasPolicy {
						t.Fatalf("missing price was serialized: %s", raw)
					}
				} else {
					want := policies[tc.want]
					expected := []int64{want.ComputeMonthlyUsdMicros, want.StorageMonthlyUsdMicros}[i]
					if amount != fmt.Sprint(expected) || policy != want.Id {
						t.Fatalf("public monthly money lost presence or precision: %s", raw)
					}
				}
			}
			if tc.want < 0 {
				if c.MonthlyPriceUsdMicros != nil || c.PricePolicyVersionId != nil || s.MonthlyPriceUsdMicros != nil || s.PricePolicyVersionId != nil {
					t.Fatalf("unpriced combination acquired a price: %v / %v", c, s)
				}
				return
			}
			want := policies[tc.want]
			if c.MonthlyPriceUsdMicros == nil || s.MonthlyPriceUsdMicros == nil || c.GetMonthlyPriceUsdMicros() != want.ComputeMonthlyUsdMicros || s.GetMonthlyPriceUsdMicros() != want.StorageMonthlyUsdMicros || c.GetPricePolicyVersionId() != want.Id || s.GetPricePolicyVersionId() != want.Id {
				t.Fatalf("monthly read = %v / %v, want policy %v", c, s, want)
			}
			// The persisted uniqueness constraint refuses ambiguous effective times.
			duplicate := monthlyPriceRequest("duplicate", compute, storage, 999, want.ValidFrom.AsTime(), nil)
			_, err = service.CreatePricePolicyVersion(peerContext(t), &api.CreatePricePolicyVersionRpcRequest{Context: platformCall("admin", "duplicate", "duplicate"), Body: duplicate})
			if status.Code(err) != codes.AlreadyExists {
				t.Fatalf("duplicate exact-pair effective timestamp = %v, want AlreadyExists", err)
			}
		})
	}
}

func TestPlanMonthlyPriceReadFailureIsNotMissingPrice(t *testing.T) {
	service, _ := system(t)
	compute, storage := monthlyPricePlans(t, service, "unavailable-db")
	if err := service.DB.Close(); err != nil {
		t.Fatal(err)
	}
	call := tenantCall("member", "tenant-test", "list", "")
	computes, err := service.ListComputePlans(peerContext(t), &api.ListComputePlansRpcRequest{Context: call, QueryStoragePlanId: proto.String(storage)})
	if computes != nil || status.Code(err) != codes.Internal {
		t.Fatalf("failed compute read = %v, %v; want an error, not an unpriced page", computes, err)
	}
	storages, err := service.ListStoragePlans(peerContext(t), &api.ListStoragePlansRpcRequest{Context: call, QueryComputePlanId: proto.String(compute)})
	if storages != nil || status.Code(err) != codes.Internal {
		t.Fatalf("failed storage read = %v, %v; want an error, not an unpriced page", storages, err)
	}
}

func workspaceOfferPrice(t *testing.T, service *Service, label, compute, storage string, amounts [3]int64, from time.Time, until *timestamppb.Timestamp) *api.PricePolicyVersion {
	t.Helper()
	body := monthlyPriceRequest(label, compute, storage, amounts[0], from, until)
	body.StorageMonthlyUsdMicros, body.ProductMonthlyUsdMicros = amounts[1], amounts[2]
	policy, err := service.CreatePricePolicyVersion(peerContext(t), &api.CreatePricePolicyVersionRpcRequest{
		Context: platformCall("admin", label, label), Body: body,
	})
	if err != nil {
		t.Fatalf("publish workspace offer %s: %v", label, err)
	}
	return policy
}

func assertWorkspaceOffers(t *testing.T, plan *api.ComputePlan, want ...*api.WorkspacePlanOffer) {
	t.Helper()
	if len(plan.GetWorkspaceOffers()) != len(want) {
		t.Fatalf("compute %s workspace offers = %v, want %v", plan.Id, plan.GetWorkspaceOffers(), want)
	}
	sort.Slice(want, func(i, j int) bool { return want[i].StoragePlanId < want[j].StoragePlanId })
	for i, offer := range plan.GetWorkspaceOffers() {
		if !proto.Equal(offer, want[i]) {
			t.Fatalf("compute %s offer[%d] = %v, want %v", plan.Id, i, offer, want[i])
		}
	}
	// Exercise the actual public encoder: money must remain an exact decimal
	// string, including zero and values beyond JavaScript's safe integer range.
	raw, err := publicjson.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Offers []struct {
			StoragePlanID string          `json:"storagePlanId"`
			PolicyID      string          `json:"pricePolicyVersionId"`
			Amount        json.RawMessage `json:"monthlyUSDMicros"`
		} `json:"workspaceOffers"`
	}
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Offers) != len(want) {
		t.Fatalf("public workspace offers lost presence: %s", raw)
	}
	for i, offer := range wire.Offers {
		var amount string
		if err := json.Unmarshal(offer.Amount, &amount); err != nil || amount != fmt.Sprint(want[i].MonthlyUsdMicros) || offer.StoragePlanID != want[i].StoragePlanId || offer.PolicyID != want[i].PricePolicyVersionId {
			t.Fatalf("public workspace offer lost identity or exact integer money: %s", raw)
		}
	}
}

func TestWorkspaceOffersCurrentPolicyAndExactMoney(t *testing.T) {
	now := time.Now().UTC()
	type version struct {
		amounts [3]int64
		from    time.Time
		until   *timestamppb.Timestamp
	}
	old := version{[3]int64{100, 20, 3}, now.Add(-3 * time.Hour), nil}
	current := version{[3]int64{200, 30, 9}, now.Add(-2 * time.Hour), timestamppb.New(now.Add(time.Hour))}
	expired := version{[3]int64{300, 40, 11}, now.Add(-time.Hour), timestamppb.New(now.Add(-time.Minute))}
	future := version{[3]int64{400, 50, 13}, now.Add(time.Hour), nil}
	for _, tc := range []struct {
		name     string
		versions []version
		want     int
	}{
		{"unpriced", nil, -1},
		{"future-only", []version{future}, -1},
		{"expired-only", []version{expired}, -1},
		{"zero-is-an-offer", []version{{[3]int64{}, now.Add(-time.Hour), nil}}, 0},
		{"product-fee-is-included", []version{{[3]int64{0, 0, 17}, now.Add(-time.Hour), nil}}, 0},
		{"beyond-js-safe-integer", []version{{[3]int64{9_007_199_254_740_991, 1, 1}, now.Add(-time.Hour), nil}}, 0},
		{"max-int64-total", []version{{[3]int64{math.MaxInt64 - 19, 12, 7}, now.Add(-time.Hour), nil}}, 0},
		{"latest-effective-version", []version{old, current}, 1},
		{"future-does-not-shadow-current", []version{old, current, future}, 1},
		{"expired-current-does-not-revive-old", []version{old, expired, future}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, _ := system(t)
			compute, storage := monthlyPricePlans(t, service, tc.name)
			policies := make([]*api.PricePolicyVersion, len(tc.versions))
			for i, v := range tc.versions {
				policies[i] = workspaceOfferPrice(t, service, fmt.Sprintf("offer-%d", i), compute, storage, v.amounts, v.from, v.until)
			}
			page, err := service.ListComputePlans(peerContext(t), &api.ListComputePlansRpcRequest{Context: tenantCall("member", "tenant-test", "offers", "")})
			if err != nil || len(page.GetItems()) != 1 || page.Items[0].Id != compute {
				t.Fatalf("workspace offers read = %v, %v", page, err)
			}
			plan := page.Items[0]
			if plan.MonthlyPriceUsdMicros != nil || plan.PricePolicyVersionId != nil {
				t.Fatalf("offer projection guessed a scalar price without an exact storage query: %v", plan)
			}
			if tc.want < 0 {
				assertWorkspaceOffers(t, plan)
				return
			}
			v, policy := tc.versions[tc.want], policies[tc.want]
			assertWorkspaceOffers(t, plan, &api.WorkspacePlanOffer{
				StoragePlanId: storage, StorageName: tc.name, StorageCapacityGiB: 10,
				PricePolicyVersionId: policy.Id, MonthlyUsdMicros: v.amounts[0] + v.amounts[1] + v.amounts[2],
			})
		})
	}
}

func TestWorkspaceOffersExactPairsAndComputePagination(t *testing.T) {
	service, _ := system(t)
	computeA, storageA := monthlyPricePlans(t, service, "pair-a")
	computeB, storageB := monthlyPricePlans(t, service, "pair-b")
	computeC, storageC := monthlyPricePlans(t, service, "unpriced-compute")
	// Distinct names and capacities make choosing a storage by name, position,
	// minimum price or another compute's approved pair observable.
	if _, err := service.DB.ExecContext(t.Context(), `UPDATE resource_catalog.storage_plans SET capacity_gib=80 WHERE id=$1`, storageB); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	workspaceOfferPrice(t, service, "aa-old", computeA, storageA, [3]int64{10, 1, 1}, now.Add(-4*time.Hour), nil)
	aa := workspaceOfferPrice(t, service, "aa-current", computeA, storageA, [3]int64{100, 20, 7}, now.Add(-2*time.Hour), nil)
	ab := workspaceOfferPrice(t, service, "ab", computeA, storageB, [3]int64{200, 80, 11}, now.Add(-3*time.Hour), nil)
	bb := workspaceOfferPrice(t, service, "bb", computeB, storageB, [3]int64{500, 90, 13}, now.Add(-time.Hour), nil)
	bc := workspaceOfferPrice(t, service, "bc", computeB, storageC, [3]int64{600, 40, 17}, now.Add(-time.Hour), nil)
	want := map[string][]*api.WorkspacePlanOffer{
		computeA: {
			{StoragePlanId: storageA, StorageName: "pair-a", StorageCapacityGiB: 10, PricePolicyVersionId: aa.Id, MonthlyUsdMicros: 127},
			{StoragePlanId: storageB, StorageName: "pair-b", StorageCapacityGiB: 80, PricePolicyVersionId: ab.Id, MonthlyUsdMicros: 291},
		},
		computeB: {
			{StoragePlanId: storageB, StorageName: "pair-b", StorageCapacityGiB: 80, PricePolicyVersionId: bb.Id, MonthlyUsdMicros: 603},
			{StoragePlanId: storageC, StorageName: "unpriced-compute", StorageCapacityGiB: 10, PricePolicyVersionId: bc.Id, MonthlyUsdMicros: 657},
		},
		computeC: nil,
	}
	// Verify that all projections, including pagination, work on a PostgreSQL
	// session which refuses writes rather than only checking returned values.
	service.DB.SetMaxOpenConns(1)
	if _, err := service.DB.ExecContext(t.Context(), `SET default_transaction_read_only = on`); err != nil {
		t.Fatal(err)
	}
	for _, pageSize := range []int32{1, 2} {
		t.Run(fmt.Sprintf("page-size-%d", pageSize), func(t *testing.T) {
			seen := make(map[string]bool)
			cursor := ""
			for pageNumber := 0; ; pageNumber++ {
				if pageNumber > len(want) {
					t.Fatal("pagination did not terminate")
				}
				page, err := service.ListComputePlans(peerContext(t), &api.ListComputePlansRpcRequest{
					Context: tenantCall("member", "tenant-test", "offers", ""), QueryCursor: proto.String(cursor), QueryLimit: proto.Int32(pageSize),
				})
				if err != nil || len(page.GetItems()) == 0 || len(page.Items) > int(pageSize) {
					t.Fatalf("compute page = %v, %v", page, err)
				}
				for _, plan := range page.Items {
					expected, exists := want[plan.Id]
					if !exists || seen[plan.Id] || plan.Id <= cursor {
						t.Fatalf("unexpected, repeated or unordered compute %s after %s", plan.Id, cursor)
					}
					assertWorkspaceOffers(t, plan, expected...)
					seen[plan.Id], cursor = true, plan.Id
				}
				if page.GetNextCursor() == "" {
					break
				}
				if page.GetNextCursor() != cursor || len(seen) == len(want) {
					t.Fatalf("next cursor = %q, last compute = %q, seen = %v", page.GetNextCursor(), cursor, seen)
				}
			}
			if len(seen) != len(want) {
				t.Fatalf("pagination lost computes: %v", seen)
			}
		})
	}
	// The old exact-resource scalar query and the full set of approved offers
	// must coexist: selecting storage A cannot suppress A's offer for B.
	page, err := service.ListComputePlans(peerContext(t), &api.ListComputePlansRpcRequest{
		Context: tenantCall("member", "tenant-test", "exact-storage", ""), QueryStoragePlanId: proto.String(storageA),
	})
	if err != nil || len(page.GetItems()) != len(want) {
		t.Fatalf("exact storage query = %v, %v", page, err)
	}
	for _, plan := range page.Items {
		assertWorkspaceOffers(t, plan, want[plan.Id]...)
		if plan.Id == computeA {
			if plan.GetMonthlyPriceUsdMicros() != 100 || plan.GetPricePolicyVersionId() != aa.Id {
				t.Fatalf("exact component price changed to a package total: %v", plan)
			}
		} else if plan.MonthlyPriceUsdMicros != nil || plan.PricePolicyVersionId != nil {
			t.Fatalf("unapproved exact pair acquired a scalar price: %v", plan)
		}
	}
}

func TestWorkspaceOffersRequireAvailablePlansAndMatchingBilling(t *testing.T) {
	for _, tc := range []struct {
		name          string
		computeChange string
		storageChange string
		want          bool
	}{
		{"available-local-pair", "", "", true},
		{"retired-storage", "", "status='revoked'", false},
		{"deprecated-storage", "", "status='deprecated'", false},
		{"future-storage", "", "valid_from=now()+interval '1 hour'", false},
		{"expired-storage", "", "valid_until=now()-interval '1 hour'", false},
		{"retired-compute", "status='revoked'", "", false},
		{"deprecated-compute", "status='deprecated'", "", false},
		{"future-compute", "valid_from=now()+interval '1 hour'", "", false},
		{"expired-compute", "valid_until=now()-interval '1 hour'", "", false},
		{"mismatched-billing", "", "billing_mode='PREPAID_MONTHLY'", false},
		{"matching-prepaid-pair", "billing_mode='PREPAID_MONTHLY'", "billing_mode='PREPAID_MONTHLY'", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, _ := system(t)
			compute, storage := monthlyPricePlans(t, service, tc.name)
			policy := workspaceOfferPrice(t, service, "approved-pair", compute, storage, [3]int64{20, 3, 1}, time.Now().Add(-time.Hour), nil)
			// Apply lifecycle changes after approval: a persisted price alone is
			// insufficient once either plan is no longer sellable.
			for _, change := range []struct{ table, id, assignment string }{
				{"compute_plans", compute, tc.computeChange},
				{"storage_plans", storage, tc.storageChange},
			} {
				if change.assignment != "" {
					query := "UPDATE resource_catalog." + change.table + " SET " + change.assignment + " WHERE id=$1"
					if _, err := service.DB.ExecContext(t.Context(), query, change.id); err != nil {
						t.Fatal(err)
					}
				}
			}
			page, err := service.ListComputePlans(peerContext(t), &api.ListComputePlansRpcRequest{Context: tenantCall("member", "tenant-test", "offers", "")})
			if err != nil || len(page.GetItems()) != 1 || page.Items[0].Id != compute {
				t.Fatalf("plan lifecycle read = %v, %v", page, err)
			}
			if tc.want {
				assertWorkspaceOffers(t, page.Items[0], &api.WorkspacePlanOffer{
					StoragePlanId: storage, StorageName: tc.name, StorageCapacityGiB: 10, PricePolicyVersionId: policy.Id, MonthlyUsdMicros: 24,
				})
			} else {
				assertWorkspaceOffers(t, page.Items[0])
			}
		})
	}
}

func TestWorkspaceOffersOverflowDoesNotFabricatePrice(t *testing.T) {
	service, _ := system(t)
	compute, storage := monthlyPricePlans(t, service, "overflow")
	workspaceOfferPrice(t, service, "overflow", compute, storage, [3]int64{math.MaxInt64, 1, 0}, time.Now().Add(-time.Hour), nil)
	page, err := service.ListComputePlans(peerContext(t), &api.ListComputePlansRpcRequest{Context: tenantCall("member", "tenant-test", "offers", "")})
	if page != nil || status.Code(err) != codes.Internal {
		t.Fatalf("unrepresentable offer total = %v, %v; want an error", page, err)
	}
}
