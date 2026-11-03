package server

import "net/http"

// Retained application history stays readable after operator deployment writes
// move to the native Runtime Control, Workspace and Serve owners.
func registerWorkspaceApplicationHistoryRoutes(mux *http.ServeMux, app *controlPlaneServer) {
	mux.HandleFunc("GET /api/operator/application-revisions/{applicationId}/{version}", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		row, found, err := app.tables.AdmittedApplicationRevision(r.Context(), r.PathValue("applicationId"), r.PathValue("version"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "workspace_application_revision_not_found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"decision": "identical", "revision": row})
	}))
	mux.HandleFunc("GET /api/operator/application-data-materials/{applicationId}/{version}", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		row, found, err := app.tables.AdmittedApplicationDataMaterial(r.Context(), r.PathValue("applicationId"), r.PathValue("version"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "workspace_application_data_material_not_found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"decision": "identical", "dataMaterial": row})
	}))
	mux.HandleFunc("GET /api/operator/application-deployments/{operationId}", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		app.writeOperatorApplicationDeployment(w, r)
	}))
}

func (app *controlPlaneServer) writeOperatorApplicationDeployment(w http.ResponseWriter, r *http.Request) {
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), r.PathValue("operationId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found || stringValue(row["action"]) != workspaceApplicationDeploymentAction {
		writeError(w, http.StatusNotFound, "workspace_application_deployment_not_found")
		return
	}
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "workspace_application_deployment_intent_invalid")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": stringValue(row["status"]), "intent": intent})
}
