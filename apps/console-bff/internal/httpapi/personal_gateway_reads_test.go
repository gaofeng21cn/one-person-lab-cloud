package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// personalGatewayOwnerServer hosts the two customer Gateway reads the BFF
// forwards to the Gateway owner. The owner's own implementation is exercised
// by the live test in services/gateway-integration/identity (real PostgreSQL,
// real gRPC, real Sub2API HTTP fixture); this test proves the BFF's client
// transport, route boundary and serialization over a real gRPC connection
// rather than an in-process probe.
type personalGatewayOwnerServer struct {
	gatewayOwnerServer
	historyRequest   *api.ListPersonalGatewayBalanceHistoryRpcRequest
	historyResponse  *api.PersonalGatewayBalanceHistoryPage
	historyErr       error
	endpointRequest  *api.GetPersonalGatewayEndpointRpcRequest
	endpointResponse *api.PersonalGatewayEndpoint
	endpointErr      error
	calls            int
}

func (s *personalGatewayOwnerServer) ListPersonalGatewayBalanceHistory(_ context.Context, r *api.ListPersonalGatewayBalanceHistoryRpcRequest) (*api.PersonalGatewayBalanceHistoryPage, error) {
	s.calls++
	s.historyRequest = r
	return s.historyResponse, s.historyErr
}

func (s *personalGatewayOwnerServer) GetPersonalGatewayEndpoint(_ context.Context, r *api.GetPersonalGatewayEndpointRpcRequest) (*api.PersonalGatewayEndpoint, error) {
	s.calls++
	s.endpointRequest = r
	return s.endpointResponse, s.endpointErr
}

// TestPersonalGatewayBFFReadsTheGatewayOwner proves the two customer Gateway
// reads travel to the Gateway owner over the production typed client instead
// of a Control Plane proxy, and that the browser never names the subject.
func TestPersonalGatewayBFFReadsTheGatewayOwner(t *testing.T) {
	owner := &personalGatewayOwnerServer{
		historyResponse: &api.PersonalGatewayBalanceHistoryPage{
			Items: []*api.PersonalGatewayBalanceHistoryEntry{{
				Type: "balance", ValueUsdMicros: "-9223372036854775808", Status: "used",
				UsedAt: ptr("2026-10-16T00:00:00Z"), CreatedAt: "2026-10-16T00:00:00Z",
			}},
			Total: 2, Page: 2, PageSize: 1, Pages: 2,
		},
		endpointResponse: &api.PersonalGatewayEndpoint{BaseUrl: "https://gateway.example.test/v1"},
	}
	client := dialGatewayOwner(t, owner)
	identity := allowedIdentity()
	identity.session.TenantId = nil
	handler := NewServer(client, identity).Handler()

	history := httptest.NewRecorder()
	request := sessionRequest(http.MethodGet, "/api/v2/gateway/balance-history?page=2&pageSize=1")
	// A browser may try to name somebody else. The owner must still read the
	// caller's own balance, so no header may reach the owner as an actor.
	request.Header.Set("X-Actor-Id", "someone-else")
	handler.ServeHTTP(history, request)
	if history.Code != 200 {
		t.Fatalf("history=%d %s", history.Code, history.Body.String())
	}
	if !strings.Contains(history.Body.String(), `"valueUsdMicros":"-9223372036854775808"`) {
		t.Fatalf("money precision lost: %s", history.Body.String())
	}
	var envelope struct {
		Source string `json:"source"`
		Status string `json:"status"`
		Data   struct {
			Total int64 `json:"total"`
			Page  int   `json:"page"`
		} `json:"data"`
	}
	if err := json.Unmarshal(history.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Source != "gateway" || envelope.Status != "available" || envelope.Data.Total != 2 || envelope.Data.Page != 2 {
		t.Fatalf("envelope=%+v", envelope)
	}
	if owner.historyRequest.GetPage() != 2 || owner.historyRequest.GetPageSize() != 1 {
		t.Fatalf("owner pagination=%+v", owner.historyRequest)
	}
	if owner.historyRequest.GetContext().GetSessionId() != owneridentity.SessionReference("session-1") || owner.historyRequest.GetContext().GetActorId() != "actor-1" {
		t.Fatalf("owner identity=%+v", owner.historyRequest.GetContext())
	}

	endpoint := httptest.NewRecorder()
	handler.ServeHTTP(endpoint, sessionRequest(http.MethodGet, "/api/v2/gateway/endpoint"))
	if endpoint.Code != 200 || !strings.Contains(endpoint.Body.String(), `"baseUrl":"https://gateway.example.test/v1"`) {
		t.Fatalf("endpoint=%d %s", endpoint.Code, endpoint.Body.String())
	}
	if owner.endpointRequest.GetContext().GetSessionId() != owneridentity.SessionReference("session-1") {
		t.Fatalf("endpoint identity=%+v", owner.endpointRequest.GetContext())
	}
}

// TestPersonalGatewayBFFRefusesSubjectAndPaginationAbuse proves the customer
// read boundary only accepts pagination: a subject, a tenant or a duplicated
// page is refused before the owner is asked, and an unidentified browser
// reaches no owner at all.
func TestPersonalGatewayBFFRefusesSubjectAndPaginationAbuse(t *testing.T) {
	owner := &personalGatewayOwnerServer{
		historyResponse:  &api.PersonalGatewayBalanceHistoryPage{Total: 0, Page: 1, PageSize: 20, Pages: 1},
		endpointResponse: &api.PersonalGatewayEndpoint{BaseUrl: "https://gateway.example.test/v1"},
	}
	client := dialGatewayOwner(t, owner)
	handler := NewServer(client, allowedIdentity()).Handler()

	for _, path := range []string{
		"/api/v2/gateway/balance-history?userId=other",
		"/api/v2/gateway/balance-history?tenantId=other",
		"/api/v2/gateway/balance-history?page=1&page=2",
		"/api/v2/gateway/balance-history?pageSize=101",
		"/api/v2/gateway/endpoint?tenantId=other",
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, sessionRequest(http.MethodGet, path))
		if response.Code != 400 {
			t.Fatalf("query=%d %s", response.Code, response.Body.String())
		}
	}
	if owner.calls != 0 {
		t.Fatalf("owner was asked for a refused query: %d", owner.calls)
	}

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v2/gateway/endpoint", nil))
	if response.Code != 401 || owner.calls != 0 {
		t.Fatalf("guard=%d calls=%d", response.Code, owner.calls)
	}
}

// TestPersonalGatewayBFFPublishesTheOwnerDecision proves the BFF publishes the
// owner's own decision instead of masking it, and that an empty balance
// history is still an available answer.
func TestPersonalGatewayBFFPublishesTheOwnerDecision(t *testing.T) {
	owner := &personalGatewayOwnerServer{historyErr: status.Error(codes.NotFound, "no balance history")}
	client := dialGatewayOwner(t, owner)
	handler := NewServer(client, allowedIdentity()).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/gateway/balance-history"))
	if response.Code != 404 {
		t.Fatalf("owner decision lost: %d %s", response.Code, response.Body.String())
	}

	owner.historyErr = nil
	owner.historyResponse = &api.PersonalGatewayBalanceHistoryPage{Total: 0, Page: 1, PageSize: 20, Pages: 1}
	empty := httptest.NewRecorder()
	handler.ServeHTTP(empty, sessionRequest(http.MethodGet, "/api/v2/gateway/balance-history"))
	if empty.Code != 200 {
		t.Fatalf("empty=%d %s", empty.Code, empty.Body.String())
	}
	var envelope struct {
		Source    string `json:"source"`
		Status    string `json:"status"`
		Available bool   `json:"available"`
	}
	if err := json.Unmarshal(empty.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Source != "gateway" || envelope.Status != "empty" || !envelope.Available {
		t.Fatalf("empty envelope=%+v", envelope)
	}
}
