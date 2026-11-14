package server

import (
	"encoding/json"
	"errors"
	"net/http"

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

// registerWorkspaceImageHistoryRoutes installs the retained image readers. The
// Instance installation reads the release policy directly, so that route stays
// registered even though no Cloud repository calls it.
func registerWorkspaceImageHistoryRoutes(mux *http.ServeMux, app *controlPlaneServer) {
	mux.HandleFunc("GET /api/operator/workspace-runtime-image-policy", app.protected(true, app.getWorkspaceImageReleasePolicy))
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
