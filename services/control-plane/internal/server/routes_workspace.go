package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func registerWorkspaceRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	mux.HandleFunc("GET /api/workspaces", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		page, pageSize, ok := operatorPagination(w, r)
		if !ok {
			return
		}
		user, ok := app.sessionUserContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not_authenticated")
			return
		}
		workspacePage, err := app.tables.PageWorkspaces(r.Context(), stringValue(user["accountId"]), tablePageQuery{Offset: (page - 1) * pageSize, Limit: pageSize})
		if err != nil {
			writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)
			return
		}
		items := make([]any, 0, len(workspacePage.Items))
		for _, row := range workspacePage.Items {
			item, ok := workspaceSourceProjection(row)
			if !ok {
				writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)
				return
			}
			if stringValue(row["currentApplicationDeploymentId"]) != "" {
				current, _, readErr := app.readWorkspaceCurrentApplication(r.Context(), service, row)
				if current == nil || readErr != nil && current.OperationID == "" {
					writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)
					return
				}
				projectWorkspaceCurrentApplication(row, item, current)
			}
			if err := app.projectWorkspaceApplicationInstallation(r.Context(), row, item); err != nil {
				writeSourceEnvelope(w, http.StatusInternalServerError, "control-plane", "unavailable", nil)
				return
			}
			items = append(items, item)
		}
		status := "available"
		if len(items) == 0 {
			status = "empty"
		}
		writeSourceEnvelope(w, http.StatusOK, "control-plane", status, map[string]any{"items": items, "total": workspacePage.Total, "page": page, "pageSize": pageSize})
	}))
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/deletion", app.protected(false, func(w http.ResponseWriter, r *http.Request) { app.workspaceDeletionStatus(w, r) }))
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/runtime-status", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
		user, ok := app.sessionUserContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not_authenticated")
			return
		}
		accountID := stringValue(user["accountId"])
		workspace, ok, err := app.workspaceForSource(r.Context(), accountID, workspaceID)
		if err != nil {
			writeSourceEnvelope(w, http.StatusInternalServerError, "fabric", "unavailable", nil)
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		if !app.canAccessResource(r, workspace) {
			writeError(w, http.StatusForbidden, "account_scope_forbidden")
			return
		}
		if !app.workspaceAccessAllowed(w, r, workspace) {
			return
		}
		unlock := app.lockEntitlementResources(
			firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])),
			stringValue(workspace["storageId"]),
			firstNonEmpty(stringValue(workspace["currentAttachmentId"]), stringValue(workspace["attachmentId"])),
		)
		defer unlock()
		workspace, ok, err = app.workspaceForSource(r.Context(), accountID, workspaceID)
		if err != nil {
			writeSourceEnvelope(w, http.StatusInternalServerError, "fabric", "unavailable", nil)
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		if !app.canAccessResource(r, workspace) {
			writeError(w, http.StatusForbidden, "account_scope_forbidden")
			return
		}
		if !app.workspaceAccessAllowed(w, r, workspace) {
			return
		}
		switch stringValue(workspace["state"]) {
		case "suspended", "stopped":
			writeError(w, http.StatusConflict, "workspace_suspended")
			return
		case "data_deleted", "unrecoverable", "storage_missing", "destroyed":
			writeError(w, http.StatusGone, "workspace_storage_destroyed")
			return
		}
		if stringValue(workspace["currentApplicationDeploymentId"]) != "" {
			current, observation, err := app.readWorkspaceCurrentApplication(r.Context(), service, workspace)
			if err != nil || current == nil {
				writeSourceEnvelope(w, http.StatusBadGateway, "fabric", "unavailable", nil)
				return
			}
			w.Header().Set("Cache-Control", "private, no-store")
			writeSourceEnvelope(w, http.StatusOK, "fabric", "available", workspaceCurrentApplicationRuntimeResponse(current, observation))
			return
		}
		if stringValue(workspace["applicationBinding"]) == "empty" {
			if mode, err := app.workspaceLaunchProvisioningMode(r.Context(), workspaceID); err != nil || mode != contracts.WorkspaceProvisioningResourceOnly {
				writeSourceEnvelope(w, http.StatusBadGateway, "control-plane", "unavailable", nil)
				return
			}
			w.Header().Set("Cache-Control", "private, no-store")
			writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", map[string]any{"workspaceId": workspaceID, "status": "not_found", "ready": false, "currentApplication": nil, "checks": []any{}})
			return
		}
		runtime, err := service.WorkspaceRuntimeStatus(r.Context(), workspaceID)
		if err != nil {
			writeSourceEnvelope(w, http.StatusBadGateway, "fabric", "unavailable", nil)
			return
		}
		body, ok := workspaceRuntimeStatusResponse(runtime, workspaceID)
		if !ok {
			writeSourceEnvelope(w, http.StatusBadGateway, "fabric", "unavailable", nil)
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		writeSourceEnvelope(w, http.StatusOK, "fabric", "available", body)
	}))
	mux.HandleFunc("POST /api/workspaces/{workspaceId}/runtime-credentials/reveal", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		workspaceID := r.PathValue("workspaceId")
		workspace, ok := app.ownedWorkspaceForCredentialCommand(w, r, workspaceID)
		if !ok {
			return
		}
		key, ok := requiredMutationKey(w, r)
		if !ok {
			return
		}
		if app.workspaceCurrentApplicationCredentials(w, r, service, workspace) {
			return
		}
		runtime, err := service.RevealWorkspaceRuntimeCredentials(r.Context(), stringValue(workspace["accountId"]), workspaceID, key)
		if err != nil {
			writeUpstreamError(w, err)
			return
		}
		if !runtime.Ready || runtime.Status == "not_found" || runtime.Access.Password == "" {
			writeError(w, http.StatusConflict, "workspace_credentials_unavailable")
			return
		}
		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, http.StatusOK, workspaceRuntimeCredentialResponse(runtime))
	}))
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/gateway-budget", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		app.workspaceGatewayBudget(w, r, service)
	}))
	mux.HandleFunc("PATCH /api/workspaces/{workspaceId}/gateway-budget", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		app.updateWorkspaceGatewayBudget(w, r, service)
	}))
	mux.HandleFunc("GET /api/workspaces/{workspaceId}/renewal", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		workspace, ok := app.getWorkspace(r.PathValue("workspaceId"))
		if !ok {
			writeError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		if !app.canAccessResource(r, workspace) {
			writeError(w, http.StatusForbidden, "account_scope_forbidden")
			return
		}
		operations, err := queryRuntimeOperations(r.Context(), app.tables, runtimeOperationQuery{WorkspaceID: stringValue(workspace["id"])})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		now := time.Now().UTC()
		response, err := workspaceAutoRenewResponse(workspace, operations, workspace["autoRenew"] == true, now)
		if err != nil {
			writeError(w, http.StatusConflict, "workspace_billing_state_invalid")
			return
		}
		response["renewalStatus"] = stringValue(workspace["renewalStatus"])
		response["recovery"] = app.workspaceRenewalRecoveryState(r.Context(), service, workspace, operations, now)
		w.Header().Set("Cache-Control", "private, no-store")
		writeJSON(w, http.StatusOK, response)
	}))
}

func (app *controlPlaneServer) workspaceForSource(ctx context.Context, accountID, workspaceID string) (map[string]any, bool, error) {
	workspace, ok, err := app.tables.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return nil, false, err
	}
	if !ok || firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])) != accountID {
		return nil, false, nil
	}
	return workspace, true, nil
}

func workspaceSourceProjection(row map[string]any) (map[string]any, bool) {
	item := map[string]any{}
	for _, key := range []string{"id", "ownerAccountId", "ownerUserId", "state", "createdAt", "updatedAt"} {
		value := stringValue(row[key])
		if value == "" {
			return nil, false
		}
		item[key] = value
	}
	if _, err := time.Parse(time.RFC3339, stringValue(item["createdAt"])); err != nil {
		return nil, false
	}
	if _, err := time.Parse(time.RFC3339, stringValue(item["updatedAt"])); err != nil {
		return nil, false
	}
	for _, key := range []string{"name", "url", "storageId", "currentComputeAllocationId", "currentAttachmentId", "runtimeId", "applicationBinding"} {
		if value := stringValue(row[key]); value != "" {
			item[key] = value
		}
	}
	if keyID, ok := requiredPositiveInteger(row, "workspaceApiKeyId"); ok {
		item["workspaceApiKeyId"] = strconv.FormatInt(keyID, 10)
	}
	for _, key := range []string{"packageId", "priceVersion", "currency", "renewalStatus"} {
		if raw, exists := row[key]; exists {
			value, ok := raw.(string)
			if !ok || strings.TrimSpace(value) == "" {
				return nil, false
			}
			item[key] = value
		}
	}
	if packageID := stringValue(item["packageId"]); packageID != "" && packageID != "basic" && packageID != "pro" {
		return nil, false
	}
	if currency := stringValue(item["currency"]); currency != "" && currency != "USD" {
		return nil, false
	}
	for _, key := range []string{"storageGb", "totalUsdMicros"} {
		if _, exists := row[key]; exists {
			value, ok := requiredNonNegativeInteger(row, key)
			if !ok || (key == "storageGb" && value == 0) {
				return nil, false
			}
			item[key] = value
		}
	}
	if raw, exists := row["autoRenew"]; exists {
		value, ok := raw.(bool)
		if !ok {
			return nil, false
		}
		item["autoRenew"] = value
	}
	for _, key := range []string{"periodStart", "paidThrough"} {
		if raw, exists := row[key]; exists {
			value, ok := raw.(string)
			if !ok {
				return nil, false
			}
			if _, err := time.Parse(time.RFC3339, value); err != nil {
				return nil, false
			}
			item[key] = value
		}
	}
	return item, true
}

func (app *controlPlaneServer) ownedWorkspaceForCredentialCommand(w http.ResponseWriter, r *http.Request, workspaceID string) (map[string]any, bool) {
	workspace, workspaceOK := app.getWorkspace(workspaceID)
	user, userOK := app.sessionUserContext(r)
	if !workspaceOK || !userOK || !app.canAccessResource(r, workspace) ||
		firstNonEmpty(stringValue(workspace["ownerUserId"]), stringValue(workspace["ownerId"])) != stringValue(user["id"]) {
		writeError(w, http.StatusForbidden, "workspace_owner_required")
		return nil, false
	}
	if !app.workspaceAccessAllowed(w, r, workspace) {
		return nil, false
	}
	return workspace, true
}

func (app *controlPlaneServer) workspaceAccessAllowed(w http.ResponseWriter, r *http.Request, workspace map[string]any) bool {
	_, reason := app.workspaceAccessResponse(r.Context(), cloneMap(workspace), time.Now().UTC())
	if reason != "" {
		writeError(w, http.StatusConflict, reason)
		return false
	}
	return true
}
