package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const workspaceRuntimeGatewayNetworkRecoveryAction = "workspace.runtime_gateway_network_recovery"

var (
	errWorkspaceRuntimeGatewayNetworkRecoveryRequest  = errors.New("invalid_workspace_runtime_gateway_network_recovery_request")
	errWorkspaceRuntimeGatewayNetworkRecoveryState    = errors.New("workspace_runtime_gateway_network_recovery_state_invalid")
	errWorkspaceRuntimeGatewayNetworkRecoveryConflict = errors.New("workspace_runtime_gateway_network_recovery_conflict")
)

type workspaceRuntimeGatewayNetworkRecoveryOperation struct {
	RequestHash string                                               `json:"requestHash"`
	Reason      string                                               `json:"reason"`
	Input       clients.WorkspaceRuntimeGatewayNetworkRecoveryInput  `json:"input"`
	Result      clients.WorkspaceRuntimeGatewayNetworkRecoveryResult `json:"result"`
	AuditEvent  map[string]any                                       `json:"auditEvent"`
	ErrorCode   string                                               `json:"errorCode,omitempty"`
}

func registerWorkspaceRuntimeGatewayNetworkRecoveryRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	mux.HandleFunc("POST /api/operator/workspaces/{workspaceId}/runtime-gateway-network/recover", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		app.recoverWorkspaceRuntimeGatewayNetwork(w, r, service)
	}))
}

func (app *controlPlaneServer) recoverWorkspaceRuntimeGatewayNetwork(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	key, ok := requiredMutationKey(w, r)
	if !ok || !validBillingReviewOpaqueID(key) {
		if ok {
			writeError(w, http.StatusBadRequest, errWorkspaceRuntimeGatewayNetworkRecoveryRequest.Error())
		}
		return
	}
	body := decodeJSON(r)
	if !exactWorkspaceComputeClaimKeys(body, []string{"confirmationWorkspaceId", "reason"}) {
		writeError(w, http.StatusBadRequest, errWorkspaceRuntimeGatewayNetworkRecoveryRequest.Error())
		return
	}
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	reason, confirmation := stringValue(body["reason"]), stringValue(body["confirmationWorkspaceId"])
	if workspaceID == "" || confirmation != workspaceID || reason == "" || reason != strings.TrimSpace(reason) || len(reason) > 512 {
		writeError(w, http.StatusBadRequest, errWorkspaceRuntimeGatewayNetworkRecoveryRequest.Error())
		return
	}
	unlock := app.lockResource("workspace-runtime-gateway-network-recovery", workspaceID)
	defer unlock()
	workspace, found, err := app.tables.GetWorkspace(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found {
		writeError(w, http.StatusNotFound, "workspace_not_found")
		return
	}
	launch, err := successfulWorkspaceLaunchForReplacement(r.Context(), app.tables, workspaceID)
	if err != nil || launch.Status != contracts.StatusSucceeded || launch.stringFact("providerProfileRef") != "local-docker" ||
		!workspaceLaunchStableProjectionMatches(launch, workspace) || firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"])) != "running" {
		writeError(w, http.StatusConflict, errWorkspaceRuntimeGatewayNetworkRecoveryConflict.Error())
		return
	}
	accountID := launch.stringFact("accountId")
	operationID := workspaceRuntimeGatewayNetworkRecoveryOperationID(workspaceID, key)
	requestHash := stableID(workspaceRuntimeGatewayNetworkRecoveryAction, accountID, workspaceID, reason, confirmation)
	row, existing, err := app.tables.GetRuntimeOperation(r.Context(), operationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	var operation workspaceRuntimeGatewayNetworkRecoveryOperation
	if existing {
		var status string
		operation, status, err = decodeWorkspaceRuntimeGatewayNetworkRecoveryOperation(row, accountID, workspaceID)
		if err != nil || operation.RequestHash != requestHash || operation.Reason != reason {
			writeError(w, http.StatusConflict, errIdempotencyConflict.Error())
			return
		}
		if status != "started" {
			statusCode := http.StatusOK
			if status == "failed" {
				statusCode = http.StatusConflict
			}
			writeJSON(w, statusCode, workspaceRuntimeGatewayNetworkRecoveryResponse(row, operation, status))
			return
		}
	} else {
		input := clients.WorkspaceRuntimeGatewayNetworkRecoveryInput{
			AccountID: accountID, WorkspaceID: workspaceID, ComputeID: launch.stringFact("computeAllocationId"),
			RuntimeID: launch.stringFact("runtimeId"), RuntimeOperationID: launch.stringFact("runtimeBindingRef"), RuntimeServiceName: launch.stringFact("runtimeServiceName"),
		}
		if input.ComputeID == "" || input.RuntimeID == "" || input.RuntimeOperationID == "" || input.RuntimeServiceName == "" {
			writeError(w, http.StatusConflict, errWorkspaceRuntimeGatewayNetworkRecoveryState.Error())
			return
		}
		beforeState := "network_binding_mismatch"
		runtime, statusErr := service.WorkspaceRuntimeStatus(r.Context(), workspaceID)
		if statusErr == nil {
			if runtime.ID != input.RuntimeID || runtime.OperationID != input.RuntimeOperationID || runtime.ServiceName != input.RuntimeServiceName || !runtime.Ready {
				writeError(w, http.StatusConflict, errWorkspaceRuntimeGatewayNetworkRecoveryConflict.Error())
				return
			}
			beforeState = "ready"
		} else if !workspaceRuntimeGatewayNetworkMismatch(statusErr) {
			writeError(w, http.StatusConflict, errWorkspaceRuntimeGatewayNetworkRecoveryConflict.Error())
			return
		}
		audit := app.auditEvent(r, workspaceRuntimeGatewayNetworkRecoveryAction, "workspace_runtime_gateway_network", workspaceID, accountID,
			map[string]any{"state": beforeState, "runtimeId": input.RuntimeID}, map[string]any{"state": "ready", "runtimeId": input.RuntimeID}, "started")
		audit["id"] = "audit-" + stableID(workspaceRuntimeGatewayNetworkRecoveryAction, operationID)[:12]
		operation = workspaceRuntimeGatewayNetworkRecoveryOperation{RequestHash: requestHash, Reason: reason, Input: input, AuditEvent: audit}
		if err := app.saveWorkspaceRuntimeGatewayNetworkRecoveryOperation(r.Context(), operationID, "started", operation); err != nil {
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
			return
		}
		row, _, _ = app.tables.GetRuntimeOperation(r.Context(), operationID)
	}
	operation.Input.IdempotencyKey = operationID
	result, callErr := service.RecoverWorkspaceRuntimeGatewayNetwork(r.Context(), operation.Input, operationID)
	status := "succeeded"
	if callErr != nil {
		status, operation.ErrorCode = "failed", workspaceRuntimeGatewayNetworkRecoveryFailure(callErr)
	} else {
		operation.Result = result
		if result.Status != "succeeded" || !result.Runtime.Ready || result.WorkspaceID != workspaceID || result.RuntimeID != operation.Input.RuntimeID {
			status, operation.ErrorCode = "failed", errWorkspaceRuntimeGatewayNetworkRecoveryConflict.Error()
		}
	}
	operation.AuditEvent = cloneMap(operation.AuditEvent)
	operation.AuditEvent["result"] = status
	if status == "succeeded" {
		operation.AuditEvent["after"] = map[string]any{"state": "ready", "runtimeId": result.RuntimeID, "gatewayContainerId": result.GatewayContainerID, "networkId": result.NetworkID}
	} else {
		operation.AuditEvent["errorCode"] = operation.ErrorCode
	}
	if err := app.saveWorkspaceRuntimeGatewayNetworkRecoveryAudit(r.Context(), operation); err != nil || app.saveWorkspaceRuntimeGatewayNetworkRecoveryOperation(r.Context(), operationID, status, operation) != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	row, _, _ = app.tables.GetRuntimeOperation(r.Context(), operationID)
	if status == "failed" {
		writeJSON(w, http.StatusConflict, workspaceRuntimeGatewayNetworkRecoveryResponse(row, operation, status))
		return
	}
	writeJSON(w, http.StatusOK, workspaceRuntimeGatewayNetworkRecoveryResponse(row, operation, status))
}

func workspaceRuntimeGatewayNetworkMismatch(err error) bool {
	var httpErr *clients.FabricHTTPError
	if !errors.As(err, &httpErr) {
		return false
	}
	var body struct {
		Error string `json:"error"`
	}
	return json.Unmarshal([]byte(httpErr.Body), &body) == nil && body.Error == "local_docker_runtime_gateway_network_readback_mismatch"
}

func workspaceRuntimeGatewayNetworkRecoveryOperationID(workspaceID, key string) string {
	return "workspace-runtime-gateway-network-recovery-" + stableID(workspaceID, key)[:18]
}

func (app *controlPlaneServer) saveWorkspaceRuntimeGatewayNetworkRecoveryOperation(ctx context.Context, operationID, status string, operation workspaceRuntimeGatewayNetworkRecoveryOperation) error {
	payload, err := json.Marshal(operation)
	if err != nil {
		return err
	}
	return app.tables.SaveRuntimeOperation(ctx, map[string]any{
		"id": operationID, "operationId": operationID, "accountId": operation.Input.AccountID, "workspaceId": operation.Input.WorkspaceID,
		"resourceId": operation.Input.WorkspaceID, "resourceKind": "workspace_runtime_gateway_network", "action": workspaceRuntimeGatewayNetworkRecoveryAction,
		"provider": "fabric", "status": status, "errorCode": operation.ErrorCode, "result": string(payload),
	})
}

func decodeWorkspaceRuntimeGatewayNetworkRecoveryOperation(row map[string]any, accountID, workspaceID string) (workspaceRuntimeGatewayNetworkRecoveryOperation, string, error) {
	var operation workspaceRuntimeGatewayNetworkRecoveryOperation
	status := stringValue(row["status"])
	if stringValue(row["accountId"]) != accountID || stringValue(row["workspaceId"]) != workspaceID || stringValue(row["action"]) != workspaceRuntimeGatewayNetworkRecoveryAction ||
		(status != "started" && status != "succeeded" && status != "failed") || json.Unmarshal([]byte(stringValue(row["result"])), &operation) != nil ||
		operation.RequestHash == "" || operation.Reason == "" || operation.Input.AccountID != accountID || operation.Input.WorkspaceID != workspaceID ||
		operation.Input.ComputeID == "" || operation.Input.RuntimeID == "" || operation.Input.RuntimeOperationID == "" || operation.Input.RuntimeServiceName == "" {
		return workspaceRuntimeGatewayNetworkRecoveryOperation{}, "", errWorkspaceRuntimeGatewayNetworkRecoveryState
	}
	return operation, status, nil
}

func workspaceRuntimeGatewayNetworkRecoveryResponse(row map[string]any, operation workspaceRuntimeGatewayNetworkRecoveryOperation, status string) map[string]any {
	return map[string]any{
		"operationId": row["operationId"], "status": status, "workspaceId": operation.Input.WorkspaceID, "runtimeId": operation.Input.RuntimeID,
		"reason": operation.Reason, "errorCode": operation.ErrorCode, "result": operation.Result, "createdAt": row["createdAt"], "updatedAt": row["updatedAt"],
	}
}

func workspaceRuntimeGatewayNetworkRecoveryFailure(err error) string {
	var httpErr *clients.FabricHTTPError
	if errors.As(err, &httpErr) {
		var body struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(httpErr.Body), &body) == nil && operatorProviderErrorCodePattern.MatchString(body.Error) {
			return body.Error
		}
	}
	return "workspace_runtime_gateway_network_recovery_provider_error"
}

func (app *controlPlaneServer) saveWorkspaceRuntimeGatewayNetworkRecoveryAudit(ctx context.Context, operation workspaceRuntimeGatewayNetworkRecoveryOperation) error {
	exists, err := app.auditIdentityExists(ctx, operation.Input.AccountID, operation.AuditEvent)
	if err != nil || exists {
		return err
	}
	return app.tables.SaveAuditEvent(ctx, operation.AuditEvent)
}
