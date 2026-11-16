package server

import (
	"net/http"

	"opl-cloud/services/control-plane/internal/controlplane"
)

func registerStateRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	// The Instance installation reads this surface directly, so the route stays
	// registered even though no Cloud repository calls it.
	mux.HandleFunc("GET /api/management/state", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		computePools, ok := fabricComputePools(w, r, service)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, app.managementState(r.URL.Query().Get("includeDeleted") == "true", computePools))
	}))
}
