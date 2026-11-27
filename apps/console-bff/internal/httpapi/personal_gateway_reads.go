package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"opl-cloud/apps/console-bff/internal/clients"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
)

// These two customer reads belong to the Gateway owner: the Gateway keeps the
// spendable balance, so the owner reads the balance history and publishes its
// own public endpoint. The BFF publishes what the owner answered and never
// derives a balance, a record or an endpoint of its own.
func (s *Server) registerPersonalGatewayReadRoutes(mux *http.ServeMux) {
	for _, kind := range []string{"balance-history", "endpoint"} {
		mux.HandleFunc("GET /api/v2/gateway/"+kind, func(w http.ResponseWriter, r *http.Request) {
			publisherRequestID(r)
			w.Header().Set("Cache-Control", "private, no-store")
			caller, err := RequireSession(r.Context(), s.identity, r)
			if err != nil {
				writePublisherIdentityError(w, r, err)
				return
			}
			query, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil || r.ContentLength != 0 {
				writePublisherError(w, r, 400, "invalid_gateway_query", "invalid gateway query")
				return
			}
			page, size := 1, 20
			for key, values := range query {
				if kind != "balance-history" || (key != "page" && key != "pageSize") || len(values) != 1 {
					writePublisherError(w, r, 400, "invalid_gateway_query", "only pagination is accepted")
					return
				}
				n, e := strconv.Atoi(values[0])
				max := 100
				if key == "page" {
					max = 1_000_000
				}
				if e != nil || n < 1 || n > max {
					writePublisherError(w, r, 400, "invalid_gateway_pagination", "invalid pagination")
					return
				}
				if key == "page" {
					page = n
				} else {
					size = n
				}
			}
			if s.gateway == nil {
				writePublisherError(w, r, 503, "owner_unconfigured", "gateway reader is not configured")
				return
			}
			ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
			call := clients.CallContext(ctx)
			var out any
			if kind == "balance-history" {
				history, e := s.gateway.ListPersonalGatewayBalanceHistory(ctx, &api.ListPersonalGatewayBalanceHistoryRpcRequest{
					Context: call, Page: int32(page), PageSize: int32(size),
				})
				if e != nil {
					writeOwnerCallFailure(w, r, e)
					return
				}
				items := make([]contracts.PersonalGatewayHistoryEntry, 0, len(history.GetItems()))
				for _, item := range history.GetItems() {
					entry := contracts.PersonalGatewayHistoryEntry{
						Type: item.GetType(), ValueUSDMicros: item.GetValueUsdMicros(),
						Status: item.GetStatus(), CreatedAt: item.GetCreatedAt(),
					}
					if item.UsedAt != nil {
						usedAt := item.GetUsedAt()
						entry.UsedAt = &usedAt
					}
					items = append(items, entry)
				}
				status := "available"
				if len(items) == 0 {
					status = "empty"
				}
				out = contracts.LegacyBillingSource[contracts.PersonalGatewayHistoryPage]{
					Source: "gateway", Status: status, Available: true,
					Data:      &contracts.PersonalGatewayHistoryPage{Items: items, Total: history.GetTotal(), Page: int(history.GetPage()), PageSize: int(history.GetPageSize()), Pages: int64(history.GetPages())},
					FetchedAt: time.Now().UTC().Format(time.RFC3339Nano),
				}
			} else {
				endpoint, e := s.gateway.GetPersonalGatewayEndpoint(ctx, &api.GetPersonalGatewayEndpointRpcRequest{Context: call})
				if e != nil {
					writeOwnerCallFailure(w, r, e)
					return
				}
				baseURL := strings.TrimSpace(endpoint.GetBaseUrl())
				if baseURL == "" {
					writePublisherError(w, r, 502, "invalid_owner_response", "invalid gateway owner response")
					return
				}
				out = contracts.LegacyBillingSource[contracts.PersonalGatewayEndpoint]{
					Source: "gateway", Status: "available", Available: true,
					Data:      &contracts.PersonalGatewayEndpoint{BaseURL: baseURL},
					FetchedAt: time.Now().UTC().Format(time.RFC3339Nano),
				}
			}
			writeJSON(w, 200, out)
		})
	}
}
