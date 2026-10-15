package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func TestCloudLegacyBillingPreservesOriginalAccountAndDeletedWorkspaceHistory(t *testing.T) {
	for _, backend := range cloudAnnouncementStores(t) {
		t.Run(backend.name, func(t *testing.T) {
			store := backend.new(t)
			seedCloudAnnouncementLegacyIdentity(t, store, "alpha", 901)
			seedCloudAnnouncementLegacyIdentity(t, store, "beta", 902)
			receipt := d2CustomerRefundReceipt()
			ledger := &customerFactsLedger{receipt: receipt, page: clients.ReceiptPage{Receipts: []clients.Receipt{receipt}, NextCursor: "opaque-next", HasMore: true}}
			gateway := &customerFactsSub2API{testSub2APIClient: &testSub2APIClient{balance: 1_000_000, charges: map[string]int64{}}, history: map[int64][]clients.Sub2APIBalanceHistoryEntry{}}
			f := newCloudAnnouncementHTTPFixture(t, store, controlplane.NewService(ledger, &fakeFabricClient{}, gateway))
			originalRow := d2CustomerRefundOriginal(t, true)
			var originalResult map[string]any
			if err := json.Unmarshal([]byte(stringValue(originalRow["result"])), &originalResult); err != nil {
				t.Fatal(err)
			}
			originalResult["ownerUserId"] = "alpha"
			encodedOriginal, err := json.Marshal(originalResult)
			if err != nil {
				t.Fatal(err)
			}
			originalRow["result"] = string(encodedOriginal)
			mustStore(t, store.SaveRuntimeOperation(t.Context(), originalRow))
			alice := f.gateway.bind("legacy-alice", "cloud-alice", "901")
			// The actor spelling does not transfer alpha's original Account to beta.
			bob := f.gateway.bind("legacy-bob", "alpha", "902")
			base := "/internal/cloud/legacy-billing/receipts"
			listed := f.request(t, "GET", base+"?cursor=opaque-current&limit=20", alice, "")
			var page contracts.LegacyBillingSource[contracts.LegacyBillingPage]
			if listed.code != 200 || json.Unmarshal(listed.body, &page) != nil || page.Data == nil || len(page.Data.Receipts) != 1 || page.Data.NextCursor != "opaque-next" || !page.Data.HasMore {
				t.Fatalf("original page=%d %s", listed.code, listed.body)
			}
			if ledger.query.AccountID != "acct-alpha" || ledger.query.Cursor != "opaque-current" || ledger.query.Limit != 20 {
				t.Fatalf("wrong owner query: %+v", ledger.query)
			}
			detail := f.request(t, "GET", base+"/refund-receipt", alice, "")
			var original contracts.LegacyBillingSource[contracts.LegacyBillingReceipt]
			if detail.code != 200 || json.Unmarshal(detail.body, &original) != nil || original.Data == nil || original.Data.ReceiptID != "refund-receipt" || original.Data.WorkspaceID != "ws-deleted" || original.Data.RefundUSDMicros == nil || *original.Data.RefundUSDMicros != 3_000_000 || original.Data.CreatedAt != receipt.CreatedAt {
				t.Fatalf("original detail=%d %s", detail.code, detail.body)
			}
			if strings.Contains(string(detail.body), "private-upstream-history") || strings.Contains(string(detail.body), "operator-private") {
				t.Fatal("private evidence leaked")
			}
			if denied := f.request(t, "GET", base+"/refund-receipt", bob, ""); denied.code != 404 {
				t.Fatalf("other user's bill=%d %s", denied.code, denied.body)
			}
			if poisoned := f.request(t, "GET", base, bob, ""); poisoned.code != 502 {
				t.Fatalf("wrong-account upstream page=%d %s", poisoned.code, poisoned.body)
			}
			trend := f.request(t, "GET", "/internal/cloud/legacy-billing/workspace-settlements", alice, "")
			var settlement contracts.LegacyBillingSource[contracts.LegacySettlementTrend]
			if trend.code != 200 || json.Unmarshal(trend.body, &settlement) != nil || settlement.Data == nil || len(settlement.Data.Days) != 14 || settlement.Data.Complete || settlement.Data.UnconfirmedCount != 1 {
				t.Fatalf("unknown original money must remain unknown=%d %s", trend.code, trend.body)
			}
			var mismatchedResult map[string]any
			if err := json.Unmarshal([]byte(stringValue(originalRow["result"])), &mismatchedResult); err != nil {
				t.Fatal(err)
			}
			mismatchedResult["ownerUserId"] = "beta"
			encodedMismatch, err := json.Marshal(mismatchedResult)
			if err != nil {
				t.Fatal(err)
			}
			originalRow["result"] = string(encodedMismatch)
			mustStore(t, store.SaveRuntimeOperation(t.Context(), originalRow))
			if mismatched := f.request(t, "GET", base+"/refund-receipt", alice, ""); mismatched.code != 502 {
				t.Fatalf("original order owner mismatch=%d %s", mismatched.code, mismatched.body)
			}
			f.gateway.fail(alice, codes.PermissionDenied)
			if revoked := f.request(t, "GET", base, alice, ""); revoked.code != 403 {
				t.Fatalf("revoked binding=%d", revoked.code)
			}
		})
	}
}

func TestCloudLegacyBillingRejectsSelectorsMissingHistoryAndExpiredSession(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	ref := f.gateway.bind("new-user", "new-user", "903")
	base := "/internal/cloud/legacy-billing/receipts"
	for _, tc := range []struct {
		path, body string
		code       int
		header     func(http.Header)
	}{
		{base, "", 404, nil},
		{base + "?accountId=acct-alpha", "", 400, nil},
		{base + "?tenantId=tenant-alpha", "", 400, nil},
		{base + "?limit=1&limit=2", "", 400, nil},
		{base, `{}`, 400, nil},
		{base, "", 401, func(h http.Header) { h.Set(owneridentity.TokenHeader, "wrong") }},
		{base, "", 400, func(h http.Header) { h.Set("X-Opl-Actor-Id", "alpha") }},
	} {
		var mutate []func(http.Header)
		if tc.header != nil {
			mutate = append(mutate, tc.header)
		}
		got := f.request(t, "GET", tc.path, ref, tc.body, mutate...)
		if got.code != tc.code {
			t.Fatalf("%s=%d %s want %d", tc.path, got.code, got.body, tc.code)
		}
	}
	f.gateway.fail(ref, codes.Unauthenticated)
	if got := f.request(t, "GET", base, ref, ""); got.code != 401 {
		t.Fatalf("expired=%d", got.code)
	}
	f.gateway.fail(ref, codes.Unavailable)
	if got := f.request(t, "GET", base, ref, ""); got.code != 503 {
		t.Fatalf("unavailable=%d", got.code)
	}
}
