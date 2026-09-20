package server

import (
	"errors"
	"net/http"

	"opl-cloud/services/control-plane/internal/controlplane"
)

func registerCoreRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	mux.HandleFunc("/w/", func(w http.ResponseWriter, r *http.Request) { app.proxyWorkspace(w, r, service) })
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { app.proxyWorkspaceRoot(w, r, service) })
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) { app.proxyWorkspaceRoot(w, r, service) })
	mux.HandleFunc("GET /api/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /api/runtime/readiness", func(w http.ResponseWriter, r *http.Request) {
		readiness, err := app.fabricReadiness(r.Context(), service)
		if err != nil {
			if errors.Is(err, errProviderConsistencyFailure) {
				writeJSON(w, http.StatusOK, readiness)
				return
			}
			writeUpstreamError(w)
			return
		}
		writeJSON(w, http.StatusOK, readiness)
	})
	mux.HandleFunc("GET /api/production/readiness", func(w http.ResponseWriter, r *http.Request) {
		readiness, err := app.fabricReadiness(r.Context(), service)
		if err != nil {
			if errors.Is(err, errProviderConsistencyFailure) {
				writeJSON(w, http.StatusOK, map[string]any{
					"provider": readiness.Provider, "ready": false,
					"cloudImagesReady": readiness.CloudImagesReady, "workspaceImagesReady": readiness.WorkspaceImagesReady,
					"immutableImagesReady": readiness.ImmutableImagesReady, "checks": []any{},
				})
				return
			}
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
