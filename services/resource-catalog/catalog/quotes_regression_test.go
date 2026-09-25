package catalog

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

func TestDeployQuoteRequiresCapabilityVersion(t *testing.T) {
	// Exercise the public decoder as well as owner admission: the decoder maps
	// protobuf fields but does not enforce the contract's deploy discriminator.
	for _, capability := range []string{"", `,"capabilityVersionId":""`} {
		request := &api.QuoteRequest{}
		raw := []byte(`{"purpose":"deploy","computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[],"periodMonths":1` + capability + `}`)
		if err := publicjson.Unmarshal(raw, request); err != nil {
			t.Fatal(err)
		}
		if err := validateDeployQuoteRequest(request); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("deploy without capability version: %v, want InvalidArgument", err)
		}
	}
}

func TestDeployQuoteReplayKeepsOriginalResponse(t *testing.T) {
	for _, policy := range []string{"price", "refund"} {
		t.Run(policy, func(t *testing.T) {
			service, _ := system(t)
			ctx := peerContext(t)
			compute, storage := quoteFixture(t, service)
			call := tenantCall("101", "tenant-test", "quote-replay", "quote-replay")
			request := &api.QuoteRequest{Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY, CapabilityVersionId: proto.String("cv-1"), ComputePlanId: compute, StoragePlanId: storage, PeriodMonths: 1}
			first, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: call, Body: request})
			if err != nil {
				t.Fatal(err)
			}
			assertReplay := func() {
				t.Helper()
				replay, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: call, Body: request})
				if err != nil || !proto.Equal(first, replay) {
					t.Fatalf("retry changed the original offer: got %v, err %v; want %v", replay, err, first)
				}
			}
			assertReplay()

			// Publish a newer effective version that has already expired. The old
			// immutable policy remains intact, and only new quotes must be refused.
			now := time.Now().UTC()
			from, until := timestamppb.New(now.Add(-10*time.Minute)), timestamppb.New(now.Add(-time.Minute))
			admin := platformCall("admin", "expired-policy", "expired-policy")
			if policy == "price" {
				_, err = service.CreatePricePolicyVersion(ctx, &api.CreatePricePolicyVersionRpcRequest{Context: admin, Body: &api.CreatePricePolicyRequest{VersionLabel: "expired-price", PeriodMonths: 1, ComputeMonthlyUsdMicros: 60_000_000, ValidFrom: from, ValidUntil: until, ComputePlanId: compute, StoragePlanId: storage, RenewalPolicy: renewalPolicy(), PlanChangePolicyVersion: api.CreatePricePolicyRequestPlanChangePolicyVersionEnum_CREATE_PRICE_POLICY_REQUEST_PLAN_CHANGE_POLICY_VERSION_ENUM_WORKSPACE_PLAN_CHANGE_V1}})
			} else {
				_, err = service.CreateRefundPolicyVersion(ctx, &api.CreateRefundPolicyVersionRpcRequest{Context: admin, Body: &api.CreateRefundPolicyRequest{VersionLabel: "expired-refund", Algorithm: api.CreateRefundPolicyRequestAlgorithmEnum_CREATE_REFUND_POLICY_REQUEST_ALGORITHM_ENUM_WORKSPACE_DELETE_REFUND_V1, RetentionPolicyVersionId: first.RetentionPolicyVersionId, CustomerTerms: "new refund terms", ValidFrom: from, ValidUntil: until}})
			}
			if err != nil {
				t.Fatal(err)
			}
			assertReplay()
			freshCall := tenantCall("101", "tenant-test", "fresh", "fresh")
			if _, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: freshCall, Body: request}); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("new quote after policy expiry = %v, want FailedPrecondition", err)
			}
			conflict := proto.Clone(request).(*api.QuoteRequest)
			conflict.CapabilityVersionId = proto.String("cv-other")
			if _, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: call, Body: conflict}); status.Code(err) != codes.AlreadyExists {
				t.Fatalf("same key with changed input = %v, want AlreadyExists", err)
			}

			// An expired offer remains the original offer. Neither a retry nor a
			// changed catalog creates or reprices it under its existing key.
			if _, err := service.DB.ExecContext(ctx, `UPDATE resource_catalog.quotes SET expires_at=now()-interval '1 minute' WHERE id=$1`, first.Id); err != nil {
				t.Fatal(err)
			}
			if _, err := service.DB.ExecContext(ctx, `UPDATE resource_catalog.compute_plans SET status='revoked' WHERE id=$1`, compute); err != nil {
				t.Fatal(err)
			}
			assertReplay()
			read, err := service.GetQuote(ctx, &api.GetQuoteRpcRequest{Context: call, QuoteId: first.Id})
			if err != nil || read.GetStatus() != api.QuoteStatusEnum_QUOTE_STATUS_ENUM_EXPIRED {
				t.Fatalf("expired quote readback = %v, err %v", read, err)
			}
			var count int
			if err := service.DB.QueryRowContext(ctx, `SELECT count(*) FROM resource_catalog.quotes`).Scan(&count); err != nil || count != 1 {
				t.Fatalf("quote rows after replays = %d, err %v, want one original offer", count, err)
			}
		})
	}
}

func TestDeployQuoteRejectsUnavailablePlanWindows(t *testing.T) {
	service, _ := system(t)
	ctx := peerContext(t)
	compute, storage := quoteFixture(t, service)
	request := &api.QuoteRequest{Purpose: api.QuoteRequestPurposeEnum_QUOTE_REQUEST_PURPOSE_ENUM_DEPLOY, CapabilityVersionId: proto.String("cv-1"), ComputePlanId: compute, StoragePlanId: storage, PeriodMonths: 1}
	now := time.Now().UTC()
	for _, plan := range []struct{ table, id string }{{"compute_plans", compute}, {"storage_plans", storage}} {
		for _, window := range []struct {
			name        string
			from, until time.Time
		}{{"future", now.Add(time.Hour), now.Add(2 * time.Hour)}, {"expired", now.Add(-2 * time.Hour), now.Add(-time.Hour)}} {
			t.Run(plan.table+"/"+window.name, func(t *testing.T) {
				if _, err := service.DB.ExecContext(ctx, `UPDATE resource_catalog.`+plan.table+` SET valid_from=$1,valid_until=$2 WHERE id=$3`, window.from, window.until, plan.id); err != nil {
					t.Fatal(err)
				}
				key := plan.table + "-" + window.name
				if _, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: tenantCall("101", "tenant-test", key, key), Body: request}); status.Code(err) != codes.FailedPrecondition {
					t.Fatalf("quote with %s %s = %v, want FailedPrecondition", window.name, plan.table, err)
				}
			})
		}
		if _, err := service.DB.ExecContext(ctx, `UPDATE resource_catalog.`+plan.table+` SET valid_from=$1,valid_until=NULL WHERE id=$2`, now.Add(-time.Hour), plan.id); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := service.DB.QueryRowContext(ctx, `SELECT count(*) FROM resource_catalog.quotes`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("refused quote rows = %d, err %v, want zero", count, err)
	}
	if _, err := service.CreateQuote(ctx, &api.CreateQuoteRpcRequest{Context: tenantCall("101", "tenant-test", "available", "available"), Body: request}); err != nil {
		t.Fatalf("quote with both plans available: %v", err)
	}
}
