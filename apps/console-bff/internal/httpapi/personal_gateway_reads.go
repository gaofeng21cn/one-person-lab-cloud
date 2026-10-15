package httpapi

import (
	"context"
	"net/http"
	"net/url"
	contracts "opl-cloud/packages/contracts/go"
	"strconv"
)

type PersonalGatewayReader interface {
	GetPersonalGatewayHistory(context.Context, string, int, int) (*contracts.LegacyBillingSource[contracts.PersonalGatewayHistoryPage], error)
	GetPersonalGatewayEndpoint(context.Context, string) (*contracts.LegacyBillingSource[contracts.PersonalGatewayEndpoint], error)
}

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
			if s.personalGateway == nil {
				writePublisherError(w, r, 503, "owner_unconfigured", "gateway reader is not configured")
				return
			}
			var out any
			valid := false
			if kind == "balance-history" {
				history, e := s.personalGateway.GetPersonalGatewayHistory(r.Context(), caller.SessionID, page, size)
				err, out = e, history
				valid = validLegacyBillingSource(history, "sub2api") && history.Data.Items != nil && history.Data.Page == page && history.Data.PageSize == size && len(history.Data.Items) <= size && history.Data.Total >= 0 && history.Data.Pages >= 0
			} else {
				endpoint, e := s.personalGateway.GetPersonalGatewayEndpoint(r.Context(), caller.SessionID)
				err, out = e, endpoint
				valid = validLegacyBillingSource(endpoint, "sub2api") && endpoint.Data.BaseURL != ""
			}
			if err != nil {
				writeOwnerCallFailure(w, r, err)
				return
			}
			if !valid {
				writePublisherError(w, r, 502, "invalid_owner_response", "invalid gateway owner response")
				return
			}
			writeJSON(w, 200, out)
		})
	}
}
