package catalog

import (
	"encoding/json"
	"fmt"
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
