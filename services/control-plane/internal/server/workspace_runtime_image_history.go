package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"opl-cloud/services/control-plane/internal/clients"
)

// Retained image operations are read for operator history and safety barriers.
// Their original records are not rewritten or dispatched by these readers.
const workspaceRuntimeImageReplacementAction = "workspace.runtime_image_replacement"

var errWorkspaceRuntimeImageReplacementState = errors.New("workspace_runtime_image_replacement_state_invalid")

type workspaceRuntimeImageReplacementInput struct {
	LaunchOperationID      string `json:"launchOperationId"`
	AccountID              string `json:"accountId"`
	WorkspaceID            string `json:"workspaceId"`
	ComputeID              string `json:"computeId"`
	StorageID              string `json:"storageId"`
	AttachmentID           string `json:"attachmentId"`
	RuntimeID              string `json:"runtimeId"`
	RuntimeOperationID     string `json:"runtimeOperationId"`
	RuntimeServiceName     string `json:"runtimeServiceName"`
	PreviousImageDigest    string `json:"previousImageDigest"`
	ReplacementImageDigest string `json:"replacementImageDigest"`
}

type workspaceRuntimeImageReplacementOperation struct {
	RequestHash string                                `json:"requestHash"`
	Reason      string                                `json:"reason"`
	Input       workspaceRuntimeImageReplacementInput `json:"input"`
	Runtime     clients.WorkspaceRuntime              `json:"runtime"`
	AuditEvent  map[string]any                        `json:"auditEvent"`
	ErrorCode   string                                `json:"errorCode,omitempty"`
}

func (app *controlPlaneServer) getWorkspaceRuntimeImageReplacement(w http.ResponseWriter, r *http.Request) {
	workspaceID, operationID := strings.TrimSpace(r.PathValue("workspaceId")), strings.TrimSpace(r.PathValue("operationId"))
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), operationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found || stringValue(row["action"]) != workspaceRuntimeImageReplacementAction || stringValue(row["workspaceId"]) != workspaceID {
		writeError(w, http.StatusNotFound, "workspace_runtime_image_replacement_not_found")
		return
	}
	operation, status, err := decodeWorkspaceRuntimeImageReplacementOperation(row, stringValue(row["accountId"]), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	writeJSON(w, http.StatusOK, workspaceRuntimeImageReplacementResponse(row, operation, status))
}

func decodeWorkspaceRuntimeImageReplacementOperation(row map[string]any, accountID, workspaceID string) (workspaceRuntimeImageReplacementOperation, string, error) {
	if stringValue(row["accountId"]) != accountID || stringValue(row["workspaceId"]) != workspaceID || stringValue(row["action"]) != workspaceRuntimeImageReplacementAction {
		return workspaceRuntimeImageReplacementOperation{}, "", errWorkspaceRuntimeImageReplacementState
	}
	var operation workspaceRuntimeImageReplacementOperation
	if json.Unmarshal([]byte(stringValue(row["result"])), &operation) != nil || operation.RequestHash == "" || operation.Reason == "" ||
		operation.Input.LaunchOperationID == "" || operation.Input.WorkspaceID != workspaceID || operation.Input.AccountID != accountID ||
		operation.Input.RuntimeID == "" || operation.Input.RuntimeOperationID == "" || operation.Input.RuntimeServiceName == "" ||
		operation.Input.PreviousImageDigest == "" || operation.Input.ReplacementImageDigest == "" {
		return workspaceRuntimeImageReplacementOperation{}, "", errWorkspaceRuntimeImageReplacementState
	}
	status := stringValue(row["status"])
	if status != "started" && status != "succeeded" && status != "failed" {
		return workspaceRuntimeImageReplacementOperation{}, "", errWorkspaceRuntimeImageReplacementState
	}
	return operation, status, nil
}

func workspaceRuntimeImageReplacementResponse(row map[string]any, operation workspaceRuntimeImageReplacementOperation, status string) map[string]any {
	response := map[string]any{
		"operationId": row["operationId"], "status": status, "workspaceId": operation.Input.WorkspaceID,
		"runtimeId": operation.Input.RuntimeID, "previousImageDigest": operation.Input.PreviousImageDigest,
		"replacementImageDigest": operation.Input.ReplacementImageDigest, "reason": operation.Reason,
		"runtime": operation.Runtime, "createdAt": row["createdAt"], "updatedAt": row["updatedAt"],
	}
	if errorCode := firstNonEmpty(stringValue(row["errorCode"]), operation.ErrorCode); errorCode != "" {
		response["errorCode"] = errorCode
	}
	return response
}
