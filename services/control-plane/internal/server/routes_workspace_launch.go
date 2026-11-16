package server

import (
	"net/http"

	"opl-cloud/services/control-plane/internal/controlplane"
)

func registerWorkspaceLaunchRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	mux.HandleFunc("GET /api/workspace-launches", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		accountID, ok := app.scopedAccountID(w, r, nil)
		if !ok {
			return
		}
		operations, err := queryRuntimeOperations(r.Context(), app.tables, runtimeOperationQuery{AccountID: accountID, Action: workspaceLaunchAction})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		rows := make([]any, 0, len(operations))
		for _, operation := range operations {
			body, err := workspaceLaunchResponse(operation)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "state_read_failed")
				return
			}
			rows = append(rows, body)
		}
		writeJSON(w, http.StatusOK, rows)
	}))

	mux.HandleFunc("GET /api/workspace-launches/{id}", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		accountID, ok := app.scopedAccountID(w, r, nil)
		if !ok {
			return
		}
		operation, found, err := app.tables.GetRuntimeOperation(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if found && stringValue(operation["accountId"]) == accountID && stringValue(operation["action"]) == workspaceLaunchAction {
			body, err := workspaceLaunchResponse(operation)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "state_read_failed")
				return
			}
			writeJSON(w, http.StatusOK, body)
			return
		}
		writeError(w, http.StatusNotFound, "workspace_launch_not_found")
	}))

}
