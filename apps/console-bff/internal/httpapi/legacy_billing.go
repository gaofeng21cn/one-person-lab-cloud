package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

type LegacyBillingReader interface {
	ListLegacyBillingReceipts(context.Context, string, string, int) (*contracts.LegacyBillingSource[contracts.LegacyBillingPage], error)
	GetLegacyBillingReceipt(context.Context, string, string) (*contracts.LegacyBillingSource[contracts.LegacyBillingReceipt], error)
	GetLegacyWorkspaceSettlements(context.Context, string) (*contracts.LegacyBillingSource[contracts.LegacySettlementTrend], error)
}

func (s *Server) registerLegacyBillingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v2/legacy-billing/receipts", s.legacyBillingHandler("list"))
	mux.HandleFunc("GET /api/v2/legacy-billing/receipts/{receiptId}", s.legacyBillingHandler("detail"))
	mux.HandleFunc("GET /api/v2/legacy-billing/workspace-settlements", s.legacyBillingHandler("trend"))
}

func validLegacyBillingSource[T any](out *contracts.LegacyBillingSource[T], source string) bool {
	return out != nil && out.Available && out.Data != nil && out.Source == source && out.FetchedAt != "" && (out.Status == "available" || out.Status == "empty")
}

func (s *Server) legacyBillingHandler(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		publisherRequestID(r)
		w.Header().Set("Cache-Control", "private, no-store")
		caller, err := RequireSession(r.Context(), s.identity, r)
		if err != nil {
			writePublisherIdentityError(w, r, err)
			return
		}
		// Only pagination is client-selected. A Tenant, actor or Account cannot
		// redirect this personal-history boundary to someone else's finances.
		query, err := url.ParseQuery(r.URL.RawQuery)
		if err != nil || r.ContentLength != 0 {
			writePublisherError(w, r, 400, "invalid_billing_query", "invalid billing query")
			return
		}
		for key, values := range query {
			if kind != "list" || (key != "cursor" && key != "limit") || len(values) != 1 {
				writePublisherError(w, r, 400, "invalid_billing_query", "only billing pagination is accepted")
				return
			}
		}
		if s.legacyBilling == nil {
			writePublisherError(w, r, 503, "owner_unconfigured", "historical billing owner is not configured")
			return
		}
		var out any
		valid := false
		switch kind {
		case "list":
			limit := 20
			if raw, ok := query["limit"]; ok {
				limit, err = strconv.Atoi(raw[0])
				if err != nil || limit < 1 || limit > 100 {
					writePublisherError(w, r, 400, "invalid_billing_limit", "limit must be between 1 and 100")
					return
				}
			}
			page, readErr := s.legacyBilling.ListLegacyBillingReceipts(r.Context(), caller.SessionID, query.Get("cursor"), limit)
			err, out = readErr, page
			valid = validLegacyBillingSource(page, "ledger")
			if valid {
				valid = page.Data.Receipts != nil && len(page.Data.Receipts) <= limit && (!page.Data.HasMore || page.Data.NextCursor != "")
			}
		case "detail":
			id := r.PathValue("receiptId")
			if id == "" || id != strings.TrimSpace(id) {
				writePublisherError(w, r, 400, "invalid_receipt_id", "receipt id is required")
				return
			}
			receipt, readErr := s.legacyBilling.GetLegacyBillingReceipt(r.Context(), caller.SessionID, id)
			err, out = readErr, receipt
			valid = validLegacyBillingSource(receipt, "ledger") && receipt.Data.ReceiptID == id
		case "trend":
			trend, readErr := s.legacyBilling.GetLegacyWorkspaceSettlements(r.Context(), caller.SessionID)
			err, out = readErr, trend
			valid = validLegacyBillingSource(trend, "control_plane") && trend.Data.Days != nil && trend.Data.Timezone != ""
		}
		if err != nil {
			writeOwnerCallFailure(w, r, err)
			return
		}
		if !valid {
			writePublisherError(w, r, 502, "invalid_owner_response", "invalid historical billing owner response")
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}
