//go:build livebuild

package catalog

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

// Include the HTTP query decoder, typed BFF client, gRPC owner, PostgreSQL read
// and public JSON money serialization; the BFF holds no price configuration.
func TestLivePlanMonthlyPricesExactPair(t *testing.T) {
	system := newLiveSystem(t)
	ctx := t.Context()
	from := timestamppb.New(time.Now().Add(-time.Hour))
	post := func(path, key string, body proto.Message) map[string]any {
		t.Helper()
		raw, err := publicjson.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		code, result := system.do(t, ctx, "platform-admin@example.test", http.MethodPost, path, key, string(raw))
		mustStatus(t, code, http.StatusCreated, result, key)
		return result
	}
	var computes, storages []string
	for i := 0; i < 2; i++ {
		key := fmt.Sprintf("plan-%d", i)
		c := post("/api/v2/admin/catalog/compute-plans", key+"-compute", &api.CreateComputePlanRequest{Name: key, Vcpus: 2, MemoryMiB: 4096, ProviderProfileId: "p", ProviderSkuId: "s", ProviderCapabilityVersion: "provider/v1", ValidFrom: from})
		s := post("/api/v2/admin/catalog/storage-plans", key+"-storage", &api.CreateStoragePlanRequest{Name: key, CapacityGiB: 10, ProviderProfileId: "p", ProviderSkuId: "s", ValidFrom: from})
		computes, storages = append(computes, c["id"].(string)), append(storages, s["id"].(string))
	}
	first := monthlyPriceRequest("first", computes[0], storages[0], 9_007_199_254_740_993, from.AsTime(), nil)
	first.StorageMonthlyUsdMicros = 0
	first.ProductMonthlyUsdMicros = 8 // Keep the total odd beyond JavaScript's safe integer range.
	p1 := post("/api/v2/admin/catalog/price-policies", "first", first)
	p2 := post("/api/v2/admin/catalog/price-policies", "second", monthlyPriceRequest("second", computes[0], storages[1], 120, from.AsTime(), nil))
	p3 := post("/api/v2/admin/catalog/price-policies", "third", monthlyPriceRequest("third", computes[1], storages[0], 340, from.AsTime(), nil))
	var stored int64
	if err := system.db.QueryRowContext(ctx, `SELECT compute_monthly_usd_micros FROM resource_catalog.price_policy_versions WHERE id=$1`, p1["id"]).Scan(&stored); err != nil || stored != first.ComputeMonthlyUsdMicros {
		t.Fatalf("persisted owner price = %d, %v", stored, err)
	}
	// Every approved exact pair is exposed as a fixed offer, independently of
	// the optional scalar resource-price filter. Expected totals are literal
	// decimal strings so no float conversion can conceal lost money precision.
	type workspaceOffer struct{ storageName, policy, total string }
	offers := map[string]map[string]workspaceOffer{
		computes[0]: {
			storages[0]: {"plan-0", p1["id"].(string), "9007199254741001"},
			storages[1]: {"plan-1", p2["id"].(string), "139"},
		},
		computes[1]: {
			storages[0]: {"plan-0", p3["id"].(string), "381"},
		},
	}
	type price struct{ amount, policy string }
	read := func(kind, otherKey, other string, want map[string]price) {
		t.Helper()
		cursor := ""
		seen := map[string]bool{}
		for {
			query := url.Values{"limit": {"1"}}
			if other != "" {
				query.Set(otherKey, other)
			}
			if cursor != "" {
				query.Set("cursor", cursor)
			}
			code, page := system.do(t, ctx, "member@example.test", http.MethodGet, "/api/v2/catalog/"+kind+"?"+query.Encode(), "list", "")
			mustStatus(t, code, http.StatusOK, page, kind)
			items, ok := page["items"].([]any)
			if !ok || len(items) != 1 {
				t.Fatalf("expected one persisted plan per page: %v", page)
			}
			item := items[0].(map[string]any)
			id := item["id"].(string)
			if seen[id] {
				t.Fatalf("pagination repeated plan %s", id)
			}
			seen[id] = true
			if kind == "compute-plans" {
				items, ok := item["workspaceOffers"].([]any)
				if !ok || len(items) != len(offers[id]) {
					t.Fatalf("HTTP workspace offers missing an approved exact pair: %v", item)
				}
				seenStorage := make(map[string]bool)
				for _, value := range items {
					offer, ok := value.(map[string]any)
					if !ok {
						t.Fatalf("HTTP workspace offer is not an object: %v", value)
					}
					storageID, _ := offer["storagePlanId"].(string)
					expected, approved := offers[id][storageID]
					if !approved || seenStorage[storageID] {
						t.Fatalf("HTTP workspace offer guessed or repeated a pair: %v", item)
					}
					seenStorage[storageID] = true
					if offer["storageName"] != expected.storageName || offer["storageCapacityGiB"] != float64(10) || offer["pricePolicyVersionId"] != expected.policy || offer["monthlyUSDMicros"] != expected.total {
						t.Fatalf("HTTP workspace offer lost storage facts, policy or full exact price: %v, want %v", offer, expected)
					}
				}
			}
			amount, hasAmount := item["monthlyPriceUSDMicros"]
			policy, hasPolicy := item["pricePolicyVersionId"]
			if expected, ok := want[id]; ok {
				if !hasAmount || !hasPolicy || amount != expected.amount || policy != expected.policy {
					t.Fatalf("%s (%s=%s): %v, want %v", kind, otherKey, other, item, expected)
				}
			} else if hasAmount || hasPolicy {
				t.Fatalf("unpriced plan acquired another pair's price: %v", item)
			}
			next, _ := page["nextCursor"].(string)
			if next == "" {
				break
			}
			cursor = next
		}
		if len(seen) != 2 {
			t.Fatalf("pagination read %d plans, want 2", len(seen))
		}
	}
	read("compute-plans", "storagePlanId", "", nil)
	read("storage-plans", "computePlanId", "", nil)
	read("compute-plans", "storagePlanId", "missing", nil)
	read("storage-plans", "computePlanId", "missing", nil)
	read("compute-plans", "storagePlanId", storages[0], map[string]price{computes[0]: {"9007199254740993", p1["id"].(string)}, computes[1]: {"340", p3["id"].(string)}})
	read("compute-plans", "storagePlanId", storages[1], map[string]price{computes[0]: {"120", p2["id"].(string)}})
	read("storage-plans", "computePlanId", computes[0], map[string]price{storages[0]: {"0", p1["id"].(string)}, storages[1]: {"12", p2["id"].(string)}})
	read("storage-plans", "computePlanId", computes[1], map[string]price{storages[0]: {"34", p3["id"].(string)}})
	var quotes int
	if err := system.db.QueryRowContext(ctx, `SELECT count(*) FROM resource_catalog.quotes`).Scan(&quotes); err != nil || quotes != 0 {
		t.Fatalf("catalog reads created quotes: %d, %v", quotes, err)
	}
}
