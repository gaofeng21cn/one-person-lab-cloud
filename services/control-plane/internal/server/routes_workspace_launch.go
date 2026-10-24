package server

import (
	"net/http"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func registerWorkspaceLaunchRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	mux.HandleFunc("POST /api/workspace-launches", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		input := decodeJSON(r)
		key, ok := requiredMutationKey(w, r)
		if !ok {
			return
		}
		accountID, ok := app.scopedAccountID(w, r, input)
		if !ok {
			return
		}
		user, ok := app.sessionUserContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not_authenticated")
			return
		}
		name, validName := input["name"].(string)
		packageID, validPackage := input["packageId"].(string)
		name, packageID = strings.TrimSpace(name), strings.TrimSpace(packageID)
		autoRenew, validAutoRenew := input["autoRenew"].(bool)
		if !validName || !validPackage || name == "" || packageID == "" || !validAutoRenew {
			writeError(w, http.StatusBadRequest, "invalid_pricing_input")
			return
		}
		provisioningMode := ""
		if rawMode, supplied := input["provisioningMode"]; supplied {
			mode, isText := rawMode.(string)
			if !isText || mode != "" && mode != string(contracts.WorkspaceProvisioningFull) && mode != string(contracts.WorkspaceProvisioningResourceOnly) {
				writeError(w, http.StatusBadRequest, "invalid_pricing_input")
				return
			}
			if mode == string(contracts.WorkspaceProvisioningResourceOnly) {
				provisioningMode = mode
			}
		}
		resourceOnly := provisioningMode == string(contracts.WorkspaceProvisioningResourceOnly)
		if _, supplied := input["sizeGb"]; supplied {
			writeError(w, http.StatusBadRequest, "invalid_pricing_input")
			return
		}
		if _, supplied := input["priceVersion"]; supplied {
			writeError(w, http.StatusBadRequest, "client_pricing_forbidden")
			return
		}
		if _, supplied := input["totalChargeUsdMicros"]; supplied {
			writeError(w, http.StatusBadRequest, "client_pricing_forbidden")
			return
		}
		ownerUserID := stringValue(user["id"])
		account, found, err := app.tables.GetAccount(r.Context(), accountID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if !found || stringValue(account["status"]) != "active" || stringValue(account["ownerUserId"]) != ownerUserID {
			writeError(w, http.StatusForbidden, "account_scope_forbidden")
			return
		}
		unlock := app.lockResource("workspace-launch", accountID)
		defer unlock()
		operationID := workspaceLaunchOperationID(accountID, key)
		row, found, err := app.tables.GetRuntimeOperation(r.Context(), operationID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if found {
			persisted, decodeErr := decodeWorkspaceLaunchReconcileOperation(row)
			matchingMode := contracts.WorkspaceProvisioningMode(provisioningMode)
			defaultRow, defaultFound, defaultErr := app.tables.GetRuntimeOperation(r.Context(), workspaceDefaultApplicationOperationID(operationID))
			if defaultErr != nil {
				writeError(w, http.StatusInternalServerError, "state_read_failed")
				return
			}
			if defaultFound {
				defaultRequest, err := decodeWorkspaceDefaultApplication(defaultRow)
				if err != nil || resourceOnly || defaultRequest.AccountID != accountID {
					writeError(w, http.StatusConflict, errIdempotencyConflict.Error())
					return
				}
				matchingMode = contracts.WorkspaceProvisioningResourceOnly
			}

			if decodeErr != nil || !workspaceLaunchReconcileRequestMatches(persisted, accountID, ownerUserID, name, packageID, autoRenew, matchingMode) {
				writeError(w, http.StatusConflict, errIdempotencyConflict.Error())
				return
			}
			if (persisted.Status == contracts.StatusPending && persisted.Stage == contracts.StageKey) || (app.deployment.customerOwned() && persisted.Status == contracts.StatusManualReview && (persisted.Stage == contracts.StageKey || persisted.Stage == contracts.StageSecret) && persisted.Observations[persisted.Stage].State == workspaceLaunchStageUnknown) {
				credentialUser, sub2APIUserID, credential, credentialOK := app.gatewayUserContext(w, r)
				if !credentialOK {
					return
				}
				if stringValue(credentialUser["accountId"]) != accountID || sub2APIUserID != persisted.int64Fact("sub2apiUserId") {
					writeError(w, http.StatusForbidden, "account_scope_forbidden")
					return
				}
				continued, reconcileErr := app.workspaceLaunchReconciler(service, credential, sub2APIUserID).Reconcile(r.Context(), persisted.ID)
				if reconcileErr != nil {
					writeError(w, http.StatusInternalServerError, "state_persist_failed")
					return
				}
				persisted = continued
			}
			if defaultFound {
				_, sub2APIUserID, credential, ok := app.gatewayUserContext(w, r)
				if !ok {
					return
				}
				_ = app.prepareDefaultWorkspaceApplication(r.Context(), service, operationID, credential, sub2APIUserID)
				_ = app.runWorkspaceDefaultApplication(r.Context(), service, workspaceDefaultApplicationOperationID(operationID))
			}
			app.respondWorkspaceLaunchContinuation(w, r, persisted)
			return
		}
		// This route remains the owner of the Launch obligations it admitted
		// before the purchase cutover, and the block above replays or continues
		// exactly those operations. A key with no persisted operation is a new
		// purchase command. The Workspace owner now prices and admits those
		// purchases, so refuse here before pricing, payment, provider or
		// persistence work can create a second writer.
		writeRetainedWorkspaceLaunchNewPurchaseRetired(w)
		return
	}))

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

	mux.HandleFunc("POST /api/workspace-launches/{id}/resume", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := requiredMutationKey(w, r); !ok {
			return
		}
		if !app.deployment.customerOwned() {
			writeError(w, http.StatusNotFound, "workspace_launch_not_found")
			return
		}
		accountID, ok := app.scopedAccountID(w, r, nil)
		if !ok {
			return
		}
		unlock := app.lockResource("workspace-launch", accountID)
		defer unlock()
		row, found, err := app.tables.GetRuntimeOperation(r.Context(), r.PathValue("id"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if !found || stringValue(row["accountId"]) != accountID || stringValue(row["action"]) != workspaceLaunchAction {
			writeError(w, http.StatusNotFound, "workspace_launch_not_found")
			return
		}
		operation, err := decodeWorkspaceLaunchReconcileOperation(row)
		recoverableManualReview := operation.Status == contracts.StatusManualReview && (operation.Stage == contracts.StageKey || operation.Stage == contracts.StageStorage || operation.Stage == contracts.StageAttachment || operation.Stage == contracts.StageSecret || operation.Stage == contracts.StageRuntime || operation.Stage == contracts.StageActivation) && operation.Observations[operation.Stage].State == workspaceLaunchStageUnknown
		recoverablePendingStage := operation.Status == contracts.StatusPending && (operation.Stage == contracts.StageKey || operation.Stage == contracts.StageStorage || operation.Stage == contracts.StageAttachment || operation.Stage == contracts.StageSecret || operation.Stage == contracts.StageRuntime)
		if err != nil || !recoverableManualReview && !recoverablePendingStage {
			writeError(w, http.StatusConflict, "workspace_launch_not_recoverable")
			return
		}
		_, sub2APIUserID, credential, ok := app.gatewayUserContext(w, r)
		if !ok {
			return
		}
		if sub2APIUserID != operation.int64Fact("sub2apiUserId") {
			writeError(w, http.StatusForbidden, "account_scope_forbidden")
			return
		}
		continued, err := app.workspaceLaunchReconciler(service, credential, sub2APIUserID).Reconcile(r.Context(), operation.ID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
			return
		}
		app.respondWorkspaceLaunchContinuation(w, r, continued)
	}))
}

func (app *controlPlaneServer) respondWorkspaceLaunchContinuation(w http.ResponseWriter, r *http.Request, operation workspaceLaunchReconcileOperation) {
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), operation.ID)
	if err != nil || !found {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	body, err := workspaceLaunchResponse(row)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	writeJSON(w, http.StatusAccepted, body)
}

// retainedWorkspaceLaunchNewPurchaseRetired is the stable refusal for a new
// purchase command on the retained Launch route. The route still replays and
// continues the operations it admitted before the cutover, but new purchases
// belong to the Workspace owner.
const retainedWorkspaceLaunchNewPurchaseRetired = "workspace_launch_new_purchase_retired"

func writeRetainedWorkspaceLaunchNewPurchaseRetired(w http.ResponseWriter) {
	writeJSON(w, http.StatusConflict, map[string]any{
		"error": retainedWorkspaceLaunchNewPurchaseRetired,
		"owner": "workspace",
		"next":  []string{"POST /api/v2/quotes", "POST /api/v2/workspaces"},
	})
}
