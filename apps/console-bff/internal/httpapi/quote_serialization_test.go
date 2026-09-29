package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
)

// quoteProbe returns a real Quote as the Resource Catalog owner would, so the
// assertion below is against the owner's serialized response rather than a
// browser fixture.
type quoteProbe struct {
	api.ResourceCatalogProductServiceClient
	request *api.CreateQuoteRpcRequest
}

func (p *quoteProbe) CreateQuote(_ context.Context, r *api.CreateQuoteRpcRequest, _ ...grpc.CallOption) (*api.Quote, error) {
	p.request = r
	return &api.Quote{
		Id:                         "quote-1",
		Purpose:                    api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY,
		CapabilityVersionId:        ptr("cap-ready-1"),
		ComputePlanId:              "compute-1",
		StoragePlanId:              "storage-1",
		ModelSelections:            []*api.ModelSelection{{Slot: "default", ModelId: "model-1"}},
		PeriodMonths:               1,
		PeriodStart:                timestamppb.Now(),
		PeriodEnd:                  timestamppb.Now(),
		PricePolicyVersionId:       "price-1",
		RefundPolicyVersionId:      "refund-1",
		RetentionPolicyVersionId:   "retention-1",
		RefundTerms:                "refund",
		RetentionTerms:             "retention",
		ExpectedInterruption:       "none",
		LineItems:                  []*api.QuoteLine{{Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_COMPUTE, Description: "compute", Quantity: 1, AmountUsdMicros: 52_580_000}},
		TotalUsdMicros:             52_580_000,
		Status:                     api.QuoteStatusEnum_QUOTE_STATUS_ENUM_OFFERED,
		ExpiresAt:                  timestamppb.Now(),
		CreatedAt:                  timestamppb.Now(),
		RuntimeReadbackRequirement: api.QuoteRuntimeReadbackRequirementEnum_QUOTE_RUNTIME_READBACK_REQUIREMENT_ENUM_REQUIRED,
	}, nil
}

// TestQuoteRouteSerializesTheContractSpelling proves the BFF emits the exact
// money property names the Console caller requires (totalUSDMicros /
// amountUSDMicros) rather than the protobuf-derived totalUsdMicros spelling.
func TestQuoteRouteSerializesTheContractSpelling(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEQUOTE
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}
	probe := &quoteProbe{}
	handler := NewCatalogHandler(probe, identity)

	request := sessionRequest(http.MethodPost, "/api/v2/quotes")
	request.Body = io.NopCloser(strings.NewReader(`{"purpose":"deploy","capabilityVersionId":"cap-ready-1","computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[{"slot":"default","modelId":"model-1"}],"periodMonths":1}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-1")
	request.Header.Set("Idempotency-Key", "quote-request-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := response.Body.String()
	for _, want := range []string{`"totalUSDMicros":"52580000"`, `"amountUSDMicros":"52580000"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("owner response wire missing %s: %s", want, body)
		}
	}
	for _, forbidden := range []string{"totalUsdMicros", "amountUsdMicros"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("owner response wire leaked the protobuf spelling %s: %s", forbidden, body)
		}
	}
	// The wire is also valid JSON with the contract key, which is what the Console
	// decoder reads.
	var decoded map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded["totalUSDMicros"] != "52580000" {
		t.Fatalf("decoded totalUSDMicros=%v", decoded["totalUSDMicros"])
	}
}

// TestQuoteRouteCarriesDefaultAppSelection proves the real catalog boundary accepts
// and forwards the explicit default-OPL-App applicationSelection, so the Console
// default-App path is not silently coerced to the agent capabilityVersionId branch.
func TestQuoteRouteCarriesDefaultAppSelection(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEQUOTE
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}
	probe := &quoteProbe{}
	handler := NewCatalogHandler(probe, identity)

	request := sessionRequest(http.MethodPost, "/api/v2/quotes")
	request.Body = io.NopCloser(strings.NewReader(`{"purpose":"deploy","applicationSelection":{"kind":"opl_app","runtimeVersionId":"runtime-1"},"computePlanId":"compute-1","storagePlanId":"storage-1","modelSelections":[],"periodMonths":1}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-1")
	request.Header.Set("Idempotency-Key", "quote-request-default-app")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if probe.request == nil || probe.request.Body == nil {
		t.Fatalf("owner did not receive the create-quote request")
	}
	selection := probe.request.Body.GetApplicationSelection()
	if selection.GetKind() != api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP || selection.GetRuntimeVersionId() != "runtime-1" || selection.GetCapabilityVersionId() != "" {
		t.Fatalf("forwarded selection = %v", selection)
	}
}
