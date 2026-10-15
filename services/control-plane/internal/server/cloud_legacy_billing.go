package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"opl-cloud/services/control-plane/internal/controlplane"
)

// Personal historical bills stay with their original Account and order owner.
// Tenant membership grants no access here and is never an Account selector.
func (app *controlPlaneServer) registerCloudLegacyBillingRoutes(mux *http.ServeMux, service *controlplane.Service) {
	for _, route := range []struct {
		path       string
		read       func(http.ResponseWriter, *http.Request, *controlplane.Service, string, string)
		pagination bool
	}{
		{"GET /internal/cloud/legacy-billing/receipts", app.listBillingReceiptsForAccount, true},
		{"GET /internal/cloud/legacy-billing/receipts/{id}", app.getBillingReceiptForAccount, false},
		{"GET /internal/cloud/legacy-billing/workspace-settlements", app.workspaceSettlementTrendForAccount, false},
	} {
		mux.HandleFunc(route.path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
			proof, ok := app.cloudSessionProof(w, r)
			if !ok {
				return
			}
			query, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil || r.ContentLength != 0 {
				writeError(w, http.StatusBadRequest, "invalid_legacy_billing_query")
				return
			}
			for key, values := range query {
				if !route.pagination || (key != "cursor" && key != "limit") || len(values) != 1 {
					writeError(w, http.StatusBadRequest, "invalid_legacy_billing_query")
					return
				}
			}
			identity, err := app.cloudUserLineage(ctx, proof)
			if err != nil {
				if errors.Is(err, errCloudUserLineage) {
					writeError(w, http.StatusConflict, "legacy_billing_identity_lineage_invalid")
				} else {
					writeSourceEnvelope(w, http.StatusServiceUnavailable, "control_plane", "unavailable", nil)
				}
				return
			}
			// No verified historical Account is not a successful zero balance or
			// empty bill collection, nor permission to use a Tenant's payer.
			if identity.legacyUserID == "" || identity.legacyAccountID == "" {
				writeError(w, http.StatusNotFound, "legacy_billing_account_not_found")
				return
			}
			route.read(w, r, service, identity.legacyAccountID, identity.legacyUserID)
		})
	}
}
