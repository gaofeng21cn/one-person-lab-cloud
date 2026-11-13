package server

import (
	"errors"
	"net/http"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func registerCoreRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	mux.HandleFunc("/w/", func(w http.ResponseWriter, r *http.Request) { app.proxyWorkspace(w, r, service) })
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { app.proxyWorkspaceRoot(w, r, service) })
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) { app.proxyWorkspaceRoot(w, r, service) })
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		// The product revision is baked into the binary at image build time,
		// so the running product states which revision it is instead of only
		// repeating what the deployment claims about it.
		writeJSON(w, http.StatusOK, map[string]string{
			"status":           "ok",
			"productSourceSha": contracts.BakedProductSourceSHA(),
		})
	})
	mux.HandleFunc("GET /api/runtime/readiness", func(w http.ResponseWriter, r *http.Request) {
		readiness, err := app.fabricReadiness(r.Context(), service)
		if err != nil && !errors.Is(err, errProviderConsistencyFailure) {
			writeUpstreamError(w)
			return
		}
		writeJSON(w, http.StatusOK, readiness)
	})
	mux.HandleFunc("GET /api/production/readiness", func(w http.ResponseWriter, r *http.Request) {
		readiness, err := app.fabricReadiness(r.Context(), service)
		if err != nil && !errors.Is(err, errProviderConsistencyFailure) {
			writeUpstreamError(w)
			return
		}
		cloudImagesReady := readiness.CloudImagesReady
		workspaceImagesReady := readiness.WorkspaceImagesReady
		immutableImagesReady := readiness.ImmutableImagesReady
		writeJSON(w, http.StatusOK, map[string]any{
			"provider": readiness.Provider, "ready": readiness.Ready && cloudImagesReady && workspaceImagesReady && immutableImagesReady,
			"cloudImagesReady": cloudImagesReady, "workspaceImagesReady": workspaceImagesReady, "immutableImagesReady": immutableImagesReady, "checks": []any{},
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { app.consoleStatic(w, r, service) })
}
