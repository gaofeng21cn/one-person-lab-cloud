package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
)

type agentCatalogProbe struct {
	api.ResourceCatalogProductServiceClient
	quote *api.Quote
	calls []*api.GetQuoteRpcRequest
}

func (p *agentCatalogProbe) GetQuote(_ context.Context, request *api.GetQuoteRpcRequest, _ ...grpc.CallOption) (*api.Quote, error) {
	p.calls = append(p.calls, request)
	return proto.Clone(p.quote).(*api.Quote), nil
}

func agentQuoteFixture() *api.Quote {
	return &api.Quote{
		Id: "quote-1", Purpose: api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY,
		RuntimeVersionId: proto.String("release-1"), ComputePlanId: "compute-1", StoragePlanId: "storage-1",
		PeriodMonths: 1, PeriodStart: timestamppb.Now(), PeriodEnd: timestamppb.New(time.Now().Add(720 * time.Hour)),
		PricePolicyVersionId: "price-1", RefundPolicyVersionId: "refund-1", RetentionPolicyVersionId: "retention-1",
		RefundTerms: "<script>alert('not executable')</script>", RetentionTerms: "Original retention terms", ExpectedInterruption: "none",
		Status: api.QuoteStatusEnum_QUOTE_STATUS_ENUM_OFFERED, TotalUsdMicros: 9007199254740993,
		LineItems: []*api.QuoteLine{{Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_COMPUTE, Description: "Monthly compute", Quantity: 1, AmountUsdMicros: 9007199254740993}},
		ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)), CreatedAt: timestamppb.Now(),
		RuntimeReadbackRequirement: api.QuoteRuntimeReadbackRequirementEnum_QUOTE_RUNTIME_READBACK_REQUIREMENT_ENUM_REQUIRED,
	}
}

type agentCookieTransport struct{ origin, cookie string }

func (t agentCookieTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Origin", t.origin)
	r.AddCookie(&http.Cookie{Name: SessionCookieName, Value: t.cookie})
	return http.DefaultTransport.RoundTrip(r)
}

func agentClient(t *testing.T, endpoint string) *mcp.ClientSession {
	t.Helper()
	client := mcp.NewClient(&mcp.Implementation{Name: "cloud-read-fixture", Version: "1.0"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: endpoint + "/api/v2/mcp", HTTPClient: &http.Client{Transport: agentCookieTransport{endpoint, "session-1"}}, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func agentQuoteServer() (*httptest.Server, *agentCatalogProbe, *fakeIdentity) {
	identity := allowedIdentity()
	identity.decision = allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "")
	identity.decision.Resource.Id = nil
	probe := &agentCatalogProbe{quote: agentQuoteFixture()}
	server := httptest.NewServer((&Server{identity: identity, catalog: probe}).Handler())
	return server, probe, identity
}

func readMCPQuote(t *testing.T, client *mcp.ClientSession, args map[string]any) agentRead {
	t.Helper()
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "cloud_get_quote", Arguments: args})
	if err != nil || result.IsError {
		raw, _ := json.Marshal(result)
		t.Fatalf("quote read: %v %s", err, raw)
	}
	raw, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var read agentRead
	if err := json.Unmarshal(raw, &read); err != nil {
		t.Fatal(err)
	}
	return read
}

func TestAgentMCPUsesLiveOwnerAuthorizationAndNoUIReadback(t *testing.T) {
	server, probe, identity := agentQuoteServer()
	defer server.Close()
	client := agentClient(t, server.URL)
	list, err := client.ListTools(t.Context(), nil)
	if err != nil || len(list.Tools) != 6 {
		t.Fatalf("discovery: %v %+v", err, list)
	}
	for _, tool := range list.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("not read only: %+v", tool)
		}
	}
	read := readMCPQuote(t, client, map[string]any{"id": "quote-1"})
	if !strings.Contains(string(read.Data), `"totalUSDMicros":"9007199254740993"`) || !strings.HasPrefix(read.Revision, "sha256:") || read.ReviewStatus != "offered" {
		t.Fatalf("lossy read: %+v", read)
	}
	if len(probe.calls) != 1 || probe.calls[0].QuoteId != "quote-1" || probe.calls[0].Context.GetActorId() != "actor-1" || probe.calls[0].Context.GetScope().GetTenant().GetTenantId() != "tenant-1" {
		t.Fatalf("owner binding: %+v", probe.calls)
	}
	raw, _ := json.Marshal(read)
	for _, secret := range []string{"csrf-1", "session-1", "authorizationContextId"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("credential leak: %s", secret)
		}
	}
	// A new tool call must use the now-revoked permission, not initialization.
	identity.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "cloud_get_quote", Arguments: map[string]any{"id": "quote-1"}})
	if err != nil || !result.IsError || len(probe.calls) != 1 {
		t.Fatalf("revoked caller reached owner: %v %+v", err, result)
	}
	identity.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED
	identity.session.ActorId = "actor-2"
	result, err = client.CallTool(t.Context(), &mcp.CallToolParams{Name: "cloud_get_quote", Arguments: map[string]any{"id": "quote-1"}})
	if err != nil || !result.IsError || len(probe.calls) != 1 {
		t.Fatalf("changed actor reused authority: %v %+v", err, result)
	}
}

func TestAgentQuoteReviewReopensExactVersionAndRejectsStaleRead(t *testing.T) {
	server, probe, _ := agentQuoteServer()
	defer server.Close()
	client := agentClient(t, server.URL)
	read := readMCPQuote(t, client, map[string]any{"id": "quote-1"})
	httpClient := &http.Client{Transport: agentCookieTransport{server.URL, "session-1"}}
	for i := 0; i < 2; i++ {
		response, err := httpClient.Get(server.URL + read.ReviewPath)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || !strings.Contains(string(body), "$9007199254.740993") || strings.Contains(string(body), "<script>") || !strings.Contains(string(body), "&lt;script&gt;") {
			t.Fatalf("review changed/unsafe: %d %s", response.StatusCode, body)
		}
		if response.Header.Get("Cache-Control") != "no-store" || !strings.Contains(response.Header.Get("Content-Security-Policy"), "form-action 'none'") {
			t.Fatal("review must remain private and read-only")
		}
	}
	probe.quote.TotalUsdMicros++
	response, err := httpClient.Get(server.URL + read.ReviewPath)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 409 {
		t.Fatalf("stale review accepted: %d", response.StatusCode)
	}
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "cloud_get_quote", Arguments: map[string]any{"id": "quote-1", "expectedRevision": read.Revision}})
	if err != nil || !result.IsError {
		t.Fatalf("stale Agent read accepted: %v %+v", err, result)
	}
	probe.quote.ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
	fresh := readMCPQuote(t, client, map[string]any{"id": "quote-1"})
	if fresh.ReviewStatus != "expired" {
		t.Fatalf("expired quote: %+v", fresh)
	}
}

func TestAgentMCPRejectsArbitraryAuthorityAndMutation(t *testing.T) {
	server, probe, _ := agentQuoteServer()
	defer server.Close()
	client := agentClient(t, server.URL)
	for _, params := range []*mcp.CallToolParams{
		{Name: "cloud_create_workspace", Arguments: map[string]any{"quoteId": "quote-1"}},
		{Name: "cloud_get_quote", Arguments: map[string]any{"id": "quote-1", "actorId": "forged"}},
		{Name: "cloud_get_quote", Arguments: map[string]any{"id": "quote-1", "path": "/api/v2/auth/session"}},
		{Name: "cloud_list_workspaces", Arguments: map[string]any{"limit": 101}},
	} {
		result, err := client.CallTool(t.Context(), params)
		if err == nil && !result.IsError {
			t.Fatalf("unbounded input accepted: %+v", params)
		}
	}
	if len(probe.calls) != 0 {
		t.Fatalf("invalid tool invoked owner: %+v", probe.calls)
	}
	for _, origin := range []string{"https://foreign.test", ""} {
		request, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v2/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-1"})
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatalf("foreign origin accepted: %d", response.StatusCode)
		}
	}
}

func TestAgentQuoteRejectsDifferentOwnerObject(t *testing.T) {
	server, probe, _ := agentQuoteServer()
	defer server.Close()
	client := agentClient(t, server.URL)
	probe.quote.Id = "quote-unrelated"
	result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "cloud_get_quote", Arguments: map[string]any{"id": "quote-1"}})
	if err != nil || !result.IsError {
		t.Fatalf("unrelated quote accepted: %v %+v", err, result)
	}
}

func TestAgentOperationReadPreservesUnknownOriginalOutcome(t *testing.T) {
	identity := allowedIdentity()
	identity.decision = allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION, "op-original")
	reader := &fakeReader{operation: &api.Operation{OperationId: "op-original", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_CREATE_WORKSPACE, ResourceId: "ws-original", Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_ADMISSION, ObservationResult: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN.Enum(), ErrorCode: api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE.Enum(), RequestId: "request-original", CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now(), PollAfterSeconds: proto.Int32(5)}}
	server := httptest.NewServer(NewServer(reader, identity).Handler())
	defer server.Close()
	client := agentClient(t, server.URL)
	for i := 0; i < 2; i++ {
		result, err := client.CallTool(t.Context(), &mcp.CallToolParams{Name: "cloud_get_workspace_operation", Arguments: map[string]any{"id": "op-original"}})
		if err != nil || result.IsError {
			t.Fatalf("operation read: %v %+v", err, result)
		}
		raw, _ := json.Marshal(result.StructuredContent)
		if !strings.Contains(string(raw), `"observationResult":"unknown"`) || !strings.Contains(string(raw), `"operationId":"op-original"`) {
			t.Fatalf("lost owner outcome: %s", raw)
		}
	}
	if identity.requests[0].GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || identity.requests[0].GetResource().GetId() != "op-original" {
		t.Fatal("wrong operation authority")
	}
}
