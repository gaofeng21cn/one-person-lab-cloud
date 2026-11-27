package identity_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
)

// balanceHistoryStream is one monetary record stream of the Gateway fixture.
type balanceHistoryStream struct {
	records []map[string]any
}

// balanceHistoryFixture serves the Gateway's admin balance-history endpoint for
// one subject. It answers with the pagination the upstream promises: the
// requested page, the requested page size and exactly the items that page
// holds.
type balanceHistoryFixture struct {
	mu       sync.Mutex
	streams  map[string]balanceHistoryStream
	requests []string
	subjects []string
}

func (f *balanceHistoryFixture) handle(w http.ResponseWriter, r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/api/v1/admin/users/") || !strings.HasSuffix(r.URL.Path, "/balance-history") {
		return false
	}
	subject := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/admin/users/"), "/balance-history")
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	recordType := r.URL.Query().Get("type")

	f.mu.Lock()
	f.requests = append(f.requests, r.URL.RequestURI())
	f.subjects = append(f.subjects, subject)
	stream, known := f.streams[recordType]
	f.mu.Unlock()
	if !known {
		w.WriteHeader(http.StatusNotFound)
		return true
	}

	total := len(stream.records)
	pages := 1
	if total > 0 {
		pages = (total + pageSize - 1) / pageSize
	}
	items := []map[string]any{}
	if page >= 1 && page <= pages {
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		items = stream.records[start:end]
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
		"items": items, "total": total, "page": page, "page_size": pageSize, "pages": pages,
	}})
	return true
}

func balanceHistoryRecord(code, recordType, value, status string, usedBy int64, usedAt, createdAt string) map[string]any {
	record := map[string]any{"code": code, "type": recordType, "value": json.Number(value), "status": status, "created_at": createdAt}
	if usedBy > 0 {
		record["used_by"] = usedBy
	}
	if usedAt != "" {
		record["used_at"] = usedAt
	}
	return record
}

// customerBalanceHistoryFixture is the two monetary streams of one account:
// an admin adjustment and the customer's own balance movements.
func customerBalanceHistoryFixture() *balanceHistoryFixture {
	return &balanceHistoryFixture{streams: map[string]balanceHistoryStream{
		"admin_balance": {records: []map[string]any{
			balanceHistoryRecord("admin-1", "admin_balance", "1.5", "used", 101, "2026-10-16T00:00:00Z", "2026-10-16T00:00:00Z"),
		}},
		"balance": {records: []map[string]any{
			balanceHistoryRecord("bal-1", "balance", "2.25", "used", 101, "2026-10-16T01:00:00Z", "2026-10-16T01:00:00Z"),
			balanceHistoryRecord("bal-2", "balance", "-0.5", "unused", 0, "", "2026-10-15T00:00:00Z"),
		}},
	}}
}

func TestGatewayOwnerPublishesTheCallersOwnBalanceHistory(t *testing.T) {
	fixture := customerBalanceHistoryFixture()
	gatewayClient, tenantClient, _ := gatewayProductSystemWithUpstream(t, fixture.handle)
	raw, session := login(t, tenantClient)
	ref := owneridentity.SessionReference(raw)
	call := &api.CallContext{ActorId: session.GetActorId(), SessionId: proto.String(ref)}

	page, err := gatewayClient.ListPersonalGatewayBalanceHistory(t.Context(), &api.ListPersonalGatewayBalanceHistoryRpcRequest{Context: call, Page: 1, PageSize: 2})
	if err != nil {
		t.Fatalf("ListPersonalGatewayBalanceHistory: %v", err)
	}
	// The two monetary streams are merged and sorted by time, so the customer
	// sees one ordered history instead of two provider streams.
	if page.GetTotal() != 3 || page.GetPage() != 1 || page.GetPageSize() != 2 || page.GetPages() != 2 {
		t.Fatalf("page=%v", page)
	}
	if len(page.GetItems()) != 2 {
		t.Fatalf("items=%v", page.GetItems())
	}
	first, second := page.GetItems()[0], page.GetItems()[1]
	if first.GetValueUsdMicros() != "2250000" || second.GetValueUsdMicros() != "1500000" {
		t.Fatalf("money=%v %v", first, second)
	}
	if first.GetStatus() != "used" || first.GetType() != "balance" || first.GetCreatedAt() != "2026-10-16T01:00:00Z" {
		t.Fatalf("first=%v", first)
	}
	if second.GetCreatedAt() != "2026-10-16T00:00:00Z" || second.GetUsedAt() != "2026-10-16T00:00:00Z" {
		t.Fatalf("second=%v", second)
	}

	// The wire keeps the amount an exact decimal string, so a balance beyond
	// the JavaScript safe integer survives the public boundary.
	wire, err := publicjson.Marshal(page)
	if err != nil {
		t.Fatalf("marshal page: %v", err)
	}
	if !strings.Contains(string(wire), `"valueUsdMicros":"2250000"`) {
		t.Fatalf("wire=%s", wire)
	}

	// The subject is the caller's own bound identity. No request may name
	// another one, and the owner never asks the Gateway about anyone else.
	for _, subject := range fixture.subjects {
		if subject != "101" {
			t.Fatalf("owner read another subject: %q", subject)
		}
	}

	secondPage, err := gatewayClient.ListPersonalGatewayBalanceHistory(t.Context(), &api.ListPersonalGatewayBalanceHistoryRpcRequest{Context: call, Page: 2, PageSize: 2})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(secondPage.GetItems()) != 1 || secondPage.GetItems()[0].GetValueUsdMicros() != "-500000" {
		t.Fatalf("second page=%v", secondPage)
	}
}

func TestGatewayOwnerRefusesAForeignBalanceRecord(t *testing.T) {
	fixture := &balanceHistoryFixture{streams: map[string]balanceHistoryStream{
		"admin_balance": {records: []map[string]any{}},
		"balance": {records: []map[string]any{
			balanceHistoryRecord("bal-1", "balance", "2.25", "used", 999, "2026-10-16T01:00:00Z", "2026-10-16T01:00:00Z"),
		}},
	}}
	gatewayClient, tenantClient, _ := gatewayProductSystemWithUpstream(t, fixture.handle)
	raw, session := login(t, tenantClient)
	ref := owneridentity.SessionReference(raw)
	call := &api.CallContext{ActorId: session.GetActorId(), SessionId: proto.String(ref)}

	if _, err := gatewayClient.ListPersonalGatewayBalanceHistory(t.Context(), &api.ListPersonalGatewayBalanceHistoryRpcRequest{Context: call, Page: 1, PageSize: 20}); status.Code(err) != codes.Unavailable {
		t.Fatalf("a record spent by another user was published: %v", err)
	}
}

func TestGatewayOwnerRefusesAForgedActor(t *testing.T) {
	fixture := &balanceHistoryFixture{streams: map[string]balanceHistoryStream{
		"admin_balance": {records: []map[string]any{}},
		"balance":       {records: []map[string]any{}},
	}}
	gatewayClient, tenantClient, _ := gatewayProductSystemWithUpstream(t, fixture.handle)
	raw, _ := login(t, tenantClient)
	ref := owneridentity.SessionReference(raw)

	forged := &api.CallContext{ActorId: "someone-else", SessionId: proto.String(ref)}
	if _, err := gatewayClient.ListPersonalGatewayBalanceHistory(t.Context(), &api.ListPersonalGatewayBalanceHistoryRpcRequest{Context: forged, Page: 1, PageSize: 20}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a forged actor reached the balance history: %v", err)
	}
	if _, err := gatewayClient.GetPersonalGatewayEndpoint(t.Context(), &api.GetPersonalGatewayEndpointRpcRequest{Context: forged}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("a forged actor reached the endpoint: %v", err)
	}
}

func TestGatewayOwnerRefusesAnUnidentifiedCaller(t *testing.T) {
	fixture := &balanceHistoryFixture{streams: map[string]balanceHistoryStream{
		"admin_balance": {records: []map[string]any{}},
		"balance":       {records: []map[string]any{}},
	}}
	gatewayClient, _, _ := gatewayProductSystemWithUpstream(t, fixture.handle)

	if _, err := gatewayClient.ListPersonalGatewayBalanceHistory(t.Context(), &api.ListPersonalGatewayBalanceHistoryRpcRequest{Context: &api.CallContext{}, Page: 1, PageSize: 20}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("an unidentified caller reached the balance history: %v", err)
	}
	if _, err := gatewayClient.GetPersonalGatewayEndpoint(t.Context(), &api.GetPersonalGatewayEndpointRpcRequest{Context: &api.CallContext{}}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("an unidentified caller reached the endpoint: %v", err)
	}
}

func TestGatewayOwnerPublishesItsPublicEndpoint(t *testing.T) {
	fixture := &balanceHistoryFixture{streams: map[string]balanceHistoryStream{
		"admin_balance": {records: []map[string]any{}},
		"balance":       {records: []map[string]any{}},
	}}
	gatewayClient, tenantClient, _ := gatewayProductSystemWithUpstream(t, fixture.handle)
	raw, session := login(t, tenantClient)
	ref := owneridentity.SessionReference(raw)
	call := &api.CallContext{ActorId: session.GetActorId(), SessionId: proto.String(ref)}

	endpoint, err := gatewayClient.GetPersonalGatewayEndpoint(t.Context(), &api.GetPersonalGatewayEndpointRpcRequest{Context: call})
	if err != nil {
		t.Fatalf("GetPersonalGatewayEndpoint: %v", err)
	}
	if !strings.HasSuffix(endpoint.GetBaseUrl(), "/v1") || !strings.HasPrefix(endpoint.GetBaseUrl(), "http") {
		t.Fatalf("endpoint=%v", endpoint)
	}
}
