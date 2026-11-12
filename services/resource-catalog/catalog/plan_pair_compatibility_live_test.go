//go:build livebuild

package catalog

import (
	"net/http"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

// Exercise compatibility through HTTP -> authenticated BFF -> gRPC -> PostgreSQL.
// The owner table tests cover each of the five compatibility coordinates; this
// loop pins their public admission, offer and quote behavior for different profiles.
func TestLivePlanPairCompatibility(t *testing.T) {
	system := newLiveSystem(t)
	ctx := t.Context()
	const admin = "platform-admin@example.test"
	const member = "tenant-admin@example.test"
	from := timestamppb.New(time.Now().Add(-time.Hour))
	request := func(actor, method, path, key string, body proto.Message, want int) map[string]any {
		t.Helper()
		var payload string
		if body != nil {
			raw, err := publicjson.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
			payload = string(raw)
		}
		code, result := system.do(t, ctx, actor, method, path, key, payload)
		mustStatus(t, code, want, result, key)
		return result
	}
	post := func(path, key string, body proto.Message) string {
		t.Helper()
		result := request(admin, http.MethodPost, "/api/v2/admin/catalog/"+path, key, body, http.StatusCreated)
		id, _ := result["id"].(string)
		if id == "" {
			t.Fatalf("%s: response has no id: %v", key, result)
		}
		return id
	}
	compute := post("compute-plans", "pair-compute", &api.CreateComputePlanRequest{
		Name: "pair-compute", Vcpus: 2, MemoryMiB: 4096, ProviderProfileId: "profile-a",
		ProviderSkuId: "compute-sku", ProviderCapabilityVersion: "provider/v1", ValidFrom: from,
	})
	storage := func(name, profile string) string {
		t.Helper()
		return post("storage-plans", name, &api.CreateStoragePlanRequest{
			Name: name, CapacityGiB: 10, ProviderProfileId: profile, ProviderSkuId: "storage-sku", ValidFrom: from,
		})
	}
	compatible := storage("pair-compatible", "profile-a")
	incompatible := storage("pair-incompatible", "profile-b")
	historical := storage("pair-historical", "profile-a")
	retention := post("retention-policies", "pair-retention", &api.CreateRetentionPolicyRequest{
		VersionLabel: "pair-retention", CustomerTerms: "data destroyed after confirmed deletion",
	})
	post("refund-policies", "pair-refund", &api.CreateRefundPolicyRequest{
		VersionLabel: "pair-refund", Algorithm: api.CreateRefundPolicyRequestAlgorithmEnum_CREATE_REFUND_POLICY_REQUEST_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1,
		RetentionPolicyVersionId: retention, CustomerTerms: "720-hour policy", ValidFrom: from,
	})
	price := post("price-policies", "pair-price", monthlyPriceRequest("pair-price", compute, compatible, 100, from.AsTime(), nil))
	// Publish the historical policy through the same real API while its pair is
	// legal; only this independent storage will later simulate legacy persisted data.
	historicalPrice := post("price-policies", "pair-historical-price", monthlyPriceRequest("pair-historical-price", compute, historical, 200, from.AsTime(), nil))
	policyCount := func() int {
		t.Helper()
		var count int
		if err := system.db.QueryRowContext(ctx, `SELECT count(*) FROM resource_catalog.price_policy_versions`).Scan(&count); err != nil {
			t.Fatal(err)
		}
		return count
	}
	if count := policyCount(); count != 2 {
		t.Fatalf("published policy count = %d, want 2", count)
	}
	denied := request(admin, http.MethodPost, "/api/v2/admin/catalog/price-policies", "pair-rejected-price",
		monthlyPriceRequest("pair-rejected-price", compute, incompatible, 300, from.AsTime(), nil), http.StatusUnprocessableEntity)
	if denied["code"] != "VALIDATION_FAILED" || policyCount() != 2 {
		t.Fatalf("incompatible publication must return VALIDATION_FAILED without adding a policy: %v", denied)
	}

	type offer struct{ policy, total string }
	readOffers := func(want map[string]offer) {
		t.Helper()
		page := request(member, http.MethodGet, "/api/v2/catalog/compute-plans", "pair-offers", nil, http.StatusOK)
		items, ok := page["items"].([]any)
		if !ok || len(items) != 1 {
			t.Fatalf("expected the single compute plan to remain readable: %v", page)
		}
		plan, ok := items[0].(map[string]any)
		if !ok || plan["id"] != compute {
			t.Fatalf("compute plan changed: %v", items[0])
		}
		offers, ok := plan["workspaceOffers"].([]any)
		if !ok || len(offers) != len(want) {
			t.Fatalf("workspace offers = %v, want %v", plan, want)
		}
		seen := make(map[string]bool)
		for _, value := range offers {
			item, ok := value.(map[string]any)
			if !ok {
				t.Fatalf("workspace offer is not an object: %v", value)
			}
			storageID, _ := item["storagePlanId"].(string)
			expected, approved := want[storageID]
			if !approved || seen[storageID] || item["pricePolicyVersionId"] != expected.policy || item["monthlyUSDMicros"] != expected.total {
				t.Fatalf("unexpected, repeated or incorrectly priced workspace offer: %v; want %v", item, want)
			}
			seen[storageID] = true
		}
	}
	readOffers(map[string]offer{compatible: {price, "117"}, historical: {historicalPrice, "227"}})
	result, err := system.db.ExecContext(ctx, `UPDATE resource_catalog.storage_plans SET provider_profile_ref=$1 WHERE id=$2`, "profile-b", historical)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		t.Fatalf("historical storage fixture changed %d rows, want 1: %v", changed, err)
	}
	readOffers(map[string]offer{compatible: {price, "117"}})
	storagePage := request(member, http.MethodGet, "/api/v2/catalog/storage-plans", "pair-storage-read", nil, http.StatusOK)
	if !pageContainsID(storagePage, compatible) || !pageContainsID(storagePage, incompatible) || !pageContainsID(storagePage, historical) {
		t.Fatalf("compatibility filtering removed readable storage plans: %v", storagePage)
	}
	policyPage := request(admin, http.MethodGet, "/api/v2/admin/catalog/price-policies", "pair-policy-history", nil, http.StatusOK)
	if !pageContainsID(policyPage, price) || !pageContainsID(policyPage, historicalPrice) || policyCount() != 2 {
		t.Fatalf("compatibility filtering lost published policy history: %v", policyPage)
	}
	quoteRequest := func(storageID string) *api.QuoteRequest {
		return &api.QuoteRequest{
			Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY, CapabilityVersionId: proto.String("cv-live"),
			ComputePlanId: compute, StoragePlanId: storageID, PeriodMonths: 1,
		}
	}
	quote := request(member, http.MethodPost, "/api/v2/quotes", "pair-compatible-quote", quoteRequest(compatible), http.StatusCreated)
	if quote["id"] == "" || quote["id"] == nil || quote["computePlanId"] != compute || quote["storagePlanId"] != compatible || quote["pricePolicyVersionId"] != price || quote["totalUSDMicros"] != "117" {
		t.Fatalf("compatible quote must match the surviving offer's exact policy and amount: %v", quote)
	}
	denied = request(member, http.MethodPost, "/api/v2/quotes", "pair-historical-quote", quoteRequest(historical), http.StatusUnprocessableEntity)
	if denied["code"] != "VALIDATION_FAILED" || denied["requestId"] != "req-pair-historical-quote" {
		t.Fatalf("historical incompatible quote lost its machine error: %v", denied)
	}
	var quotes int
	if err := system.db.QueryRowContext(ctx, `SELECT count(*) FROM resource_catalog.quotes`).Scan(&quotes); err != nil || quotes != 1 {
		t.Fatalf("quote count = %d, want only the compatible quote: %v", quotes, err)
	}
	if policyCount() != 2 {
		t.Fatal("refusing a historical incompatible quote changed published policy history")
	}
}
