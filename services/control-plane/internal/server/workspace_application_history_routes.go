package server

import "net/http"

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
