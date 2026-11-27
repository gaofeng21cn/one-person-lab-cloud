package identity

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// gatewayBalanceFixture is an upstream Gateway that serves the directory login
// and one admin balance-history endpoint. It answers the pagination the
// upstream promises, so the owner's own read is exercised against a real HTTP
// transport instead of a stubbed function.
type gatewayBalanceFixture struct {
	mu      sync.Mutex
	streams map[string][]map[string]any
	// broken answers a page that lies about its own pagination.
	broken bool
}

func (f *gatewayBalanceFixture) server(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/auth/login", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"access_token": "directory-token", "expires_in": 3600,
			"user": map[string]any{"id": 1, "email": "directory@example.test", "status": "active"},
		}})
	})
	mux.HandleFunc("/api/v1/admin/users/", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
		recordType := r.URL.Query().Get("type")

		f.mu.Lock()
		records, known := f.streams[recordType]
		broken := f.broken
		f.mu.Unlock()
		if !known {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		total := len(records)
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
			items = records[start:end]
		}
		echo := pageSize
		if broken {
			echo = pageSize + 1
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
			"items": items, "total": total, "page": page, "page_size": echo, "pages": pages,
		}})
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func balanceRecord(code, recordType, value, state string, usedBy int64, usedAt, createdAt string) map[string]any {
	record := map[string]any{"code": code, "type": recordType, "value": json.Number(value), "status": state, "created_at": createdAt}
	if usedBy > 0 {
		record["used_by"] = usedBy
	}
	if usedAt != "" {
		record["used_at"] = usedAt
	}
	return record
}

func gatewayWithFixture(t *testing.T, fixture *gatewayBalanceFixture) *Gateway {
	t.Helper()
	server := fixture.server(t)
	gateway, err := NewGatewayWithDirectory(server.URL, &GatewayDirectory{Email: "directory@example.test", Password: "password"})
	if err != nil {
		t.Fatal(err)
	}
	return gateway
}

func customerBalanceFixture() *gatewayBalanceFixture {
	return &gatewayBalanceFixture{streams: map[string][]map[string]any{
		"admin_balance": {
			balanceRecord("admin-1", "admin_balance", "1.5", "used", 101, "2026-10-16T00:00:00Z", "2026-10-16T00:00:00Z"),
		},
		"balance": {
			balanceRecord("bal-1", "balance", "2.25", "used", 101, "2026-10-16T01:00:00Z", "2026-10-16T01:00:00Z"),
			balanceRecord("bal-2", "balance", "-0.5", "unused", 0, "", "2026-10-15T00:00:00Z"),
		},
	}}
}

// TestCustomerBalanceHistoryMergesTheTwoMonetaryStreams proves the customer's
// own money is one ordered history: both monetary streams are read, merged and
// sorted, and the amount keeps its exact micros.
func TestCustomerBalanceHistoryMergesTheTwoMonetaryStreams(t *testing.T) {
	gateway := gatewayWithFixture(t, customerBalanceFixture())
	page, err := gateway.customerBalanceHistoryPage(t.Context(), 101, 1, 2)
	if err != nil {
		t.Fatalf("customerBalanceHistoryPage: %v", err)
	}
	if page.Total != 3 || page.Page != 1 || page.PageSize != 2 || page.Pages != 2 {
		t.Fatalf("page=%+v", page)
	}
	if len(page.Items) != 2 {
		t.Fatalf("items=%+v", page.Items)
	}
	if page.Items[0].ValueUSDMicros != 2_250_000 || page.Items[1].ValueUSDMicros != 1_500_000 {
		t.Fatalf("money=%+v", page.Items)
	}
	if page.Items[0].UsedAt == nil || page.Items[1].UsedAt == nil {
		t.Fatalf("used time lost: %+v", page.Items)
	}
	if page.Items[0].Type != "balance" || page.Items[1].Type != "balance" {
		t.Fatalf("type=%+v", page.Items)
	}

	second, err := gateway.customerBalanceHistoryPage(t.Context(), 101, 2, 2)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Items) != 1 || second.Items[0].ValueUSDMicros != -500_000 {
		t.Fatalf("second=%+v", second)
	}
}

// TestCustomerBalanceHistoryRefusesARecordOfAnotherUser proves a record spent
// by somebody else is refused instead of published as the caller's own money.
func TestCustomerBalanceHistoryRefusesARecordOfAnotherUser(t *testing.T) {
	fixture := &gatewayBalanceFixture{streams: map[string][]map[string]any{
		"admin_balance": {},
		"balance": {
			balanceRecord("bal-1", "balance", "2.25", "used", 999, "2026-10-16T01:00:00Z", "2026-10-16T01:00:00Z"),
		},
	}}
	gateway := gatewayWithFixture(t, fixture)
	if _, err := gateway.customerBalanceHistoryPage(t.Context(), 101, 1, 20); err == nil {
		t.Fatal("a record spent by another user was published")
	}
}

// TestCustomerBalanceHistoryRefusesAnUnreadableUpstreamPage proves the owner
// refuses an upstream answer that disagrees with its own pagination instead of
// showing a page it cannot vouch for.
func TestCustomerBalanceHistoryRefusesAnUnreadableUpstreamPage(t *testing.T) {
	fixture := customerBalanceFixture()
	fixture.broken = true
	gateway := gatewayWithFixture(t, fixture)
	if _, err := gateway.customerBalanceHistoryPage(t.Context(), 101, 1, 20); err == nil {
		t.Fatal("an upstream page that lies about its pagination was published")
	}
}

// TestCustomerBalanceHistoryRefusesAnInvalidPage proves the owner's own page
// bounds: no subject, no page and an oversized page size are refused before
// the Gateway is asked.
func TestCustomerBalanceHistoryRefusesAnInvalidPage(t *testing.T) {
	fixture := customerBalanceFixture()
	gateway := gatewayWithFixture(t, fixture)
	for _, row := range []struct {
		subject  int64
		page     int
		pageSize int
	}{
		{subject: 0, page: 1, pageSize: 20},
		{subject: 101, page: 0, pageSize: 20},
		{subject: 101, page: 1, pageSize: 0},
		{subject: 101, page: 1, pageSize: 101},
	} {
		if _, err := gateway.customerBalanceHistoryPage(t.Context(), row.subject, row.page, row.pageSize); err == nil {
			t.Fatalf("an invalid page was accepted: %+v", row)
		}
	}
}

// TestGatewayPublicEndpointPublishesTheV1Origin proves the owner publishes the
// origin its own configuration points at.
func TestGatewayPublicEndpointPublishesTheV1Origin(t *testing.T) {
	gateway := gatewayWithFixture(t, customerBalanceFixture())
	endpoint := gateway.PublicEndpoint()
	if !strings.HasSuffix(endpoint, "/v1") || !strings.HasPrefix(endpoint, "http") {
		t.Fatalf("endpoint=%q", endpoint)
	}
	empty := &Gateway{}
	if empty.PublicEndpoint() != "" {
		t.Fatalf("an unconfigured Gateway published an endpoint: %q", empty.PublicEndpoint())
	}
}
