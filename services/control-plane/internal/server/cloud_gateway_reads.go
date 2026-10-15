package server

import (
	"context"
	"net/http"
	"net/url"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/control-plane/internal/controlplane"
	"strconv"
	"time"
)

// These retained reads use the verified live User, without a legacy login or
// Account selection. No customer credential is fabricated from a service token.
func (app *controlPlaneServer) registerCloudGatewayReadRoutes(mux *http.ServeMux, service *controlplane.Service) {
	for _, kind := range []string{"balance-history", "endpoint"} {
		mux.HandleFunc("GET /internal/cloud/gateway/"+kind, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
			proof, ok := app.cloudSessionProof(w, r)
			if !ok {
				return
			}
			subject, err := strconv.ParseInt(proof.GetSub2ApiUserId(), 10, 64)
			if err != nil || subject <= 0 || strconv.FormatInt(subject, 10) != proof.GetSub2ApiUserId() || proof.GetActorId() == "" || proof.GetStatus() != api.UserExternalBindingReadbackStatusEnum_USER_EXTERNAL_BINDING_READBACK_STATUS_ENUM_ACTIVE || proof.GetVerification() != api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_CONFIRMED {
				writeError(w, 403, "gateway_identity_invalid")
				return
			}
			query, err := url.ParseQuery(r.URL.RawQuery)
			if err != nil || r.ContentLength != 0 {
				writeError(w, 400, "invalid_gateway_query")
				return
			}
			for key, values := range query {
				if kind != "balance-history" || (key != "page" && key != "pageSize") || len(values) != 1 || values[0] == "" {
					writeError(w, 400, "invalid_gateway_query")
					return
				}
			}
			if kind == "balance-history" {
				page, size, ok := gatewayBalanceHistoryPagination(w, r)
				if !ok {
					return
				}
				app.gatewayBalanceHistoryForUser(w, r, service, subject, page, size)
				return
			}
			endpoint, err := service.GatewayPublicEndpoint()
			if err != nil {
				writeSourceEnvelope(w, 503, "sub2api", "unavailable", nil)
				return
			}
			writeSourceEnvelope(w, 200, "sub2api", "available", map[string]any{"baseUrl": endpoint})
		})
	}
}
