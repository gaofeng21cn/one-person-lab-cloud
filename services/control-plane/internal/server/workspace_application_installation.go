package server

import (
	"errors"
	"net/http"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// The Console Launch is resource-only by contract: it delivers capacity, and
// the application is installed afterwards as its own authorized operation.
// Before this route the only customer-facing application action was resume,
// which requires an installation request that a resource-only Launch never
// creates, so a Workspace bought from the Console could never reach an
// installed application at all.
//
// This route starts that operation for a Workspace the caller already owns. It
// changes no price and no provisioning contract: it admits the existing
// resource-only Launch, reuses the Workspace's own Sub2API identity and
// entitlement window, and hands the request to the installation machinery the
// full-Launch path already uses. Repeating it is idempotent — the Workspace
// carries at most one installation request, and the projection below is the
// same answer whether it was just created or already existed.
func registerWorkspaceApplicationInstallationRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	mux.HandleFunc("POST /api/workspaces/{workspaceID}/application-installation", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requiredMutationKey(w, r); !ok {
			return
		}
		accountID, ok := app.scopedAccountID(w, r, nil)
		if !ok {
			return
		}
		workspaceID := strings.TrimSpace(r.PathValue("workspaceID"))
		current, found, err := app.tables.GetWorkspace(r.Context(), workspaceID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if workspaceID == "" || !found || firstNonEmpty(stringValue(current["accountId"]), stringValue(current["ownerAccountId"])) != accountID {
			writeError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		rows, err := queryRuntimeOperations(r.Context(), app.tables, runtimeOperationQuery{WorkspaceID: workspaceID, Action: workspaceDefaultApplicationAction})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		switch len(rows) {
		case 0:
			if !app.startWorkspaceApplicationInstallation(w, r, service, current, accountID) {
				return
			}
		case 1:
			// The Workspace already carries its one installation request. Drive it
			// again so a repeat of this call is a retry, not a second installation.
			request, decodeErr := decodeWorkspaceDefaultApplication(rows[0])
			if decodeErr != nil {
				writeError(w, http.StatusInternalServerError, "state_read_failed")
				return
			}
			if request.Phase != "deployed" {
				if _, _, credential, ok := app.gatewayUserContext(w, r); ok {
					_ = app.prepareDefaultWorkspaceApplication(r.Context(), service, request.LaunchOperationID, credential, request.Sub2APIUserID)
					_ = app.runWorkspaceDefaultApplication(r.Context(), service, request.OperationID)
				}
			}
		default:
			writeError(w, http.StatusConflict, "workspace_default_application_ambiguous")
			return
		}
		current, found, err = app.tables.GetWorkspace(r.Context(), workspaceID)
		projection := map[string]any{"workspaceId": workspaceID, "applicationInstallation": nil}
		if err != nil || !found || app.projectWorkspaceApplicationInstallation(r.Context(), current, projection) != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		writeJSON(w, http.StatusAccepted, projection)
	}))
}

func (app *controlPlaneServer) startWorkspaceApplicationInstallation(w http.ResponseWriter, r *http.Request, service *controlplane.Service, workspace map[string]any, accountID string) bool {
	user, sub2APIUserID, credential, ok := app.gatewayUserContext(w, r)
	if !ok {
		return false
	}
	if stringValue(user["accountId"]) != accountID {
		writeError(w, http.StatusForbidden, "account_scope_forbidden")
		return false
	}
	workspaceID := stringValue(workspace["id"])
	launchRows, err := queryRuntimeOperations(r.Context(), app.tables, runtimeOperationQuery{WorkspaceID: workspaceID, Action: workspaceLaunchAction})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return false
	}
	if len(launchRows) != 1 {
		writeError(w, http.StatusConflict, "workspace_application_installation_unavailable")
		return false
	}
	launch, err := decodeWorkspaceLaunchReconcileOperation(launchRows[0])
	if err != nil || launch.provisioningMode() != contracts.WorkspaceProvisioningResourceOnly || launch.Status != contracts.StatusSucceeded ||
		launch.stringFact("workspaceId") != workspaceID || launch.stringFact("accountId") != accountID || launch.int64Fact("sub2apiUserId") != sub2APIUserID {
		writeError(w, http.StatusConflict, "workspace_application_installation_unavailable")
		return false
	}
	imageDigest := currentWorkspaceImageDigest()
	if imageDigest == "" {
		writeError(w, http.StatusConflict, "workspace_application_installation_unavailable")
		return false
	}
	keyGroupID, err := workspaceCodexGroupID(r.Context(), service, credential, sub2APIUserID)
	if err != nil {
		writeUpstreamError(w, err)
		return false
	}
	row, err := workspaceDefaultApplicationRow(workspaceDefaultApplicationRequest{
		SchemaVersion: 1, OperationID: workspaceDefaultApplicationOperationID(launch.ID), LaunchOperationID: launch.ID,
		AccountID: accountID, WorkspaceID: workspaceID, OwnerUserID: stringValue(user["id"]), Sub2APIUserID: sub2APIUserID,
		WorkspaceKeyGroupID: keyGroupID, Revision: defaultOPLApplicationRevision(imageDigest), Phase: "credentials_required",
	})
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return false
	}
	switch err := app.tables.CreateWorkspaceApplicationOperation(r.Context(), row); {
	case err == nil:
	case errors.Is(err, errWorkspaceApplicationOperationCASConflict):
		// Another start won the one request this Workspace may carry; its own
		// preparation and the installation worker own the rest.
		return true
	default:
		writeError(w, http.StatusConflict, "workspace_application_installation_conflict")
		return false
	}
	// The request is durable from here on: credential preparation and the
	// installation itself are retried by the projection's resume action and by
	// the installation worker, exactly as a full Launch treats its own request.
	_ = app.prepareDefaultWorkspaceApplication(r.Context(), service, launch.ID, credential, sub2APIUserID)
	_ = app.runWorkspaceDefaultApplication(r.Context(), service, stringValue(row["id"]))
	return true
}
