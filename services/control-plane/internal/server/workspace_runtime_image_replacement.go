package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const workspaceRuntimeImageReplacementAction = "workspace.runtime_image_replacement"

var (
	errWorkspaceRuntimeImageReplacementRequest  = errors.New("invalid_workspace_runtime_image_replacement_request")
	errWorkspaceRuntimeImageReplacementState    = errors.New("workspace_runtime_image_replacement_state_invalid")
	errWorkspaceRuntimeImageReplacementConflict = errors.New("workspace_runtime_image_replacement_conflict")
)

type workspaceRuntimeImageReplacementRequest struct {
	ReplacementImageDigest string `json:"replacementImageDigest"`
	Reason                 string `json:"reason"`
}

type workspaceRuntimeImageReplacementPreview struct {
	WorkspaceID        string `json:"workspaceId"`
	WorkspaceStatus    string `json:"workspaceStatus"`
	RuntimeID          string `json:"runtimeId"`
	RuntimeStatus      string `json:"runtimeStatus"`
	CurrentImageDigest string `json:"currentImageDigest"`
	TargetImageDigest  string `json:"targetImageDigest"`
	CanReplace         bool   `json:"canReplace"`
}

type workspaceRuntimeImageReplacementOperation struct {
	RequestHash string                                        `json:"requestHash"`
	Reason      string                                        `json:"reason"`
	Input       clients.WorkspaceRuntimeImageReplacementInput `json:"input"`
	Runtime     clients.WorkspaceRuntime                      `json:"runtime"`
	AuditEvent  map[string]any                                `json:"auditEvent"`
	ErrorCode   string                                        `json:"errorCode,omitempty"`
}

func registerWorkspaceRuntimeImageReplacementRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	mux.HandleFunc("GET /api/operator/workspace-runtime-image-policy", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		image := currentWorkspaceImageDigest()
		if image == "" {
			writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
			return
		}
		writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", map[string]any{
			"image":  image,
			"digest": deployedImageDigest(image),
			"source": "OPL_WORKSPACE_IMAGE",
		})
	}))
	mux.HandleFunc("GET /api/operator/workspaces/{workspaceId}/runtime-image-replacements/preview", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		app.previewWorkspaceRuntimeImageReplacement(w, r, service)
	}))
	mux.HandleFunc("POST /api/operator/workspaces/{workspaceId}/runtime-image-replacements", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		app.createWorkspaceRuntimeImageReplacement(w, r, service)
	}))
	mux.HandleFunc("GET /api/operator/workspaces/{workspaceId}/runtime-image-replacements/{operationId}", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		app.getWorkspaceRuntimeImageReplacement(w, r)
	}))
}

func (app *controlPlaneServer) previewWorkspaceRuntimeImageReplacement(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
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
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	workspaceStatus := firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"]))
	if !workspaceLaunchStableProjectionMatches(launch, workspace) || workspaceStatus != "running" {
		writeError(w, http.StatusConflict, errWorkspaceRuntimeImageReplacementConflict.Error())
		return
	}
	runtime, err := service.WorkspaceRuntimeStatus(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "workspace_runtime_readback_unavailable")
		return
	}
	target := currentWorkspaceImageDigest()
	if target == "" {
		writeError(w, http.StatusServiceUnavailable, "workspace_image_not_current_protected_release")
		return
	}
	preview := workspaceRuntimeImageReplacementPreview{
		WorkspaceID: workspaceID, WorkspaceStatus: workspaceStatus, RuntimeID: runtime.ID,
		RuntimeStatus: runtime.Status, CurrentImageDigest: runtime.ImageID, TargetImageDigest: target,
		CanReplace: runtime.ID != "" && runtime.WorkspaceID == workspaceID && runtime.Status == "running" && runtime.Ready && runtime.ImageID != "" && runtime.ImageID != target,
	}
	writeSourceEnvelope(w, http.StatusOK, "control-plane+fabric", "available", preview)
}

func (app *controlPlaneServer) createWorkspaceRuntimeImageReplacement(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	key, ok := requiredMutationKey(w, r)
	if !ok || !validBillingReviewOpaqueID(key) {
		if ok {
			writeError(w, http.StatusBadRequest, errWorkspaceRuntimeImageReplacementRequest.Error())
		}
		return
	}
	input := decodeJSON(r)
	if !exactWorkspaceComputeClaimKeys(input, []string{"replacementImageDigest", "reason"}) {
		writeError(w, http.StatusBadRequest, errWorkspaceRuntimeImageReplacementRequest.Error())
		return
	}
	request := workspaceRuntimeImageReplacementRequest{ReplacementImageDigest: stringValue(input["replacementImageDigest"]), Reason: stringValue(input["reason"])}
	if request.ReplacementImageDigest != strings.TrimSpace(request.ReplacementImageDigest) || request.Reason == "" || request.Reason != strings.TrimSpace(request.Reason) ||
		!workspaceImageReferenceWithDigest(request.ReplacementImageDigest) {
		writeError(w, http.StatusBadRequest, errWorkspaceRuntimeImageReplacementRequest.Error())
		return
	}
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
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
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	if !workspaceLaunchStableProjectionMatches(launch, workspace) || firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"])) != "running" {
		writeError(w, http.StatusConflict, errWorkspaceRuntimeImageReplacementConflict.Error())
		return
	}
	accountID := launch.stringFact("accountId")
	operationID := workspaceRuntimeImageReplacementOperationID(workspaceID, key)
	row, found, err := app.tables.GetRuntimeOperation(r.Context(), operationID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if found {
		operation, status, decodeErr := decodeWorkspaceRuntimeImageReplacementOperation(row, accountID, workspaceID)
		if decodeErr != nil || operation.Reason != request.Reason || operation.Input.ReplacementImageDigest != request.ReplacementImageDigest {
			writeError(w, http.StatusConflict, errIdempotencyConflict.Error())
			return
		}
		writeJSON(w, http.StatusAccepted, workspaceRuntimeImageReplacementResponse(row, operation, status))
		return
	}
	runtime, err := service.WorkspaceRuntimeStatus(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusBadGateway, "workspace_runtime_readback_unavailable")
		return
	}
	replacement := clients.WorkspaceRuntimeImageReplacementInput{
		LaunchOperationID: launch.ID, AccountID: accountID, WorkspaceID: workspaceID,
		ComputeID: launch.stringFact("computeAllocationId"), StorageID: launch.stringFact("storageId"), AttachmentID: launch.stringFact("attachmentId"),
		RuntimeID: launch.stringFact("runtimeId"), RuntimeOperationID: launch.stringFact("runtimeBindingRef"), RuntimeServiceName: launch.stringFact("runtimeServiceName"),
		PreviousImageDigest: runtime.ImageID, ReplacementImageDigest: request.ReplacementImageDigest,
	}
	if runtime.ID != replacement.RuntimeID || runtime.OperationID != replacement.RuntimeOperationID || runtime.ServiceName != replacement.RuntimeServiceName ||
		runtime.WorkspaceID != workspaceID || runtime.ImageID == "" || runtime.ImageID == request.ReplacementImageDigest {
		writeError(w, http.StatusConflict, errWorkspaceRuntimeImageReplacementConflict.Error())
		return
	}
	protectedImage := currentWorkspaceImageDigest()
	if protectedImage == "" || request.ReplacementImageDigest != protectedImage {
		writeError(w, http.StatusConflict, "workspace_image_not_current_protected_release")
		return
	}
	requestHash := workspaceRuntimeImageReplacementRequestHash(accountID, replacement, request.Reason)
	audit := app.auditEvent(r, "workspace.runtime_image_replacement", "workspace_runtime", workspaceID, accountID,
		map[string]any{"imageDigest": runtime.ImageID}, map[string]any{"imageDigest": request.ReplacementImageDigest, "reason": request.Reason}, "started")
	audit["id"] = "audit-" + stableID(workspaceRuntimeImageReplacementAction, operationID)[:12]
	operation := workspaceRuntimeImageReplacementOperation{RequestHash: requestHash, Reason: request.Reason, Input: replacement, Runtime: runtime, AuditEvent: audit}
	if err := app.saveWorkspaceRuntimeImageReplacementOperation(r.Context(), operationID, accountID, workspaceID, "started", operation); err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	if err := app.tables.SaveAuditEvent(r.Context(), audit); err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	if workspaceRuntimeImageReplacementWorkerEnabled() {
		go func() {
			_ = app.runWorkspaceRuntimeImageReplacement(context.Background(), service, operationID)
		}()
	}
	row, _, _ = app.tables.GetRuntimeOperation(r.Context(), operationID)
	writeJSON(w, http.StatusAccepted, workspaceRuntimeImageReplacementResponse(row, operation, "started"))
}

func workspaceRuntimeImageReplacementOperationID(workspaceID, idempotencyKey string) string {
	return "workspace-runtime-image-replacement-" + stableID(workspaceID, idempotencyKey)[:18]
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

func successfulWorkspaceLaunchForReplacement(ctx context.Context, store controlPlaneTableStore, workspaceID string) (workspaceLaunchReconcileOperation, error) {
	rows, err := queryRuntimeOperations(ctx, store, runtimeOperationQuery{Action: workspaceLaunchAction})
	if err != nil {
		return workspaceLaunchReconcileOperation{}, errWorkspaceRuntimeImageReplacementState
	}
	var result workspaceLaunchReconcileOperation
	found := false
	for _, row := range rows {
		if !isWorkspaceLaunchAction(stringValue(row["action"])) || stringValue(row["workspaceId"]) != workspaceID || stringValue(row["status"]) != string(contracts.StatusSucceeded) {
			continue
		}
		decoded, decodeErr := decodeWorkspaceLaunchReconcileOperation(row)
		if decodeErr != nil {
			return workspaceLaunchReconcileOperation{}, errWorkspaceRuntimeImageReplacementState
		}
		if found {
			return workspaceLaunchReconcileOperation{}, errWorkspaceRuntimeImageReplacementConflict
		}
		result, found = decoded, true
	}
	if !found {
		return workspaceLaunchReconcileOperation{}, errWorkspaceRuntimeImageReplacementState
	}
	return result, nil
}

func workspaceRuntimeImageReplacementRequestHash(accountID string, input clients.WorkspaceRuntimeImageReplacementInput, reason string) string {
	return stableID(workspaceRuntimeImageReplacementAction, accountID, input.WorkspaceID, input.RuntimeID, input.PreviousImageDigest, input.ReplacementImageDigest, reason)
}

func (app *controlPlaneServer) saveWorkspaceRuntimeImageReplacementOperation(ctx context.Context, operationID, accountID, workspaceID, status string, operation workspaceRuntimeImageReplacementOperation) error {
	payload, err := json.Marshal(operation)
	if err != nil {
		return err
	}
	row := map[string]any{
		"id": operationID, "operationId": operationID, "accountId": accountID, "workspaceId": workspaceID,
		"resourceId": workspaceID, "resourceKind": "workspace_runtime", "action": workspaceRuntimeImageReplacementAction,
		"provider": "fabric", "status": status, "errorCode": operation.ErrorCode, "result": string(payload),
		"computeAllocationId": operation.Input.ComputeID, "storageId": operation.Input.StorageID,
		"attachmentId": operation.Input.AttachmentID, "runtimeServiceName": operation.Input.RuntimeServiceName,
	}
	return app.tables.SaveRuntimeOperation(ctx, row)
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

func workspaceRuntimeImageReplacementWorkerEnabled() bool {
	value := strings.TrimSpace(os.Getenv("OPL_WORKSPACE_RUNTIME_IMAGE_REPLACEMENT_WORKER_ENABLED"))
	if value == "" {
		return workspaceLaunchWorkerEnabled()
	}
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func (app *controlPlaneServer) runWorkspaceRuntimeImageReplacement(ctx context.Context, service *controlplane.Service, operationID string) error {
	unlock, err := app.lockResourceContext(ctx, "workspace-runtime-image-replacement", operationID)
	if err != nil {
		return err
	}
	defer unlock()
	row, found, err := app.tables.GetRuntimeOperation(ctx, operationID)
	if err != nil || !found {
		return errWorkspaceRuntimeImageReplacementState
	}
	operation, status, err := decodeWorkspaceRuntimeImageReplacementOperation(row, stringValue(row["accountId"]), stringValue(row["workspaceId"]))
	if err != nil || status != "started" {
		return err
	}
	operation.Input.IdempotencyKey = operationID
	result, callErr := service.ReplaceWorkspaceRuntimeImage(ctx, operation.Input, operationID)
	if callErr != nil {
		if result.Runtime.ID != "" {
			operation.Runtime = result.Runtime
		}
		var retryable bool
		operation.ErrorCode, retryable = workspaceRuntimeImageReplacementFailure(callErr)
		if retryable {
			if saveErr := app.saveWorkspaceRuntimeImageReplacementOperation(ctx, operationID, operation.Input.AccountID, operation.Input.WorkspaceID, "started", operation); saveErr != nil {
				return saveErr
			}
			return callErr
		}
		operation.AuditEvent = cloneMap(operation.AuditEvent)
		operation.AuditEvent["errorCode"] = operation.ErrorCode
		operation.AuditEvent["result"] = "failed"
		if err := app.saveWorkspaceRuntimeImageReplacementAudit(ctx, operation); err != nil {
			return err
		}
		if err := app.saveWorkspaceRuntimeImageReplacementOperation(ctx, operationID, operation.Input.AccountID, operation.Input.WorkspaceID, "failed", operation); err != nil {
			return err
		}
		return callErr
	}
	operation.Runtime = result.Runtime
	operation.AuditEvent = cloneMap(operation.AuditEvent)
	operation.AuditEvent["after"] = map[string]any{"imageDigest": operation.Input.ReplacementImageDigest, "reason": operation.Reason, "runtimeReady": result.Runtime.Ready}
	operation.AuditEvent["result"] = "succeeded"
	if err := app.saveWorkspaceRuntimeImageReplacementAudit(ctx, operation); err != nil {
		return err
	}
	return app.saveWorkspaceRuntimeImageReplacementOperation(ctx, operationID, operation.Input.AccountID, operation.Input.WorkspaceID, "succeeded", operation)
}

func (app *controlPlaneServer) saveWorkspaceRuntimeImageReplacementAudit(ctx context.Context, operation workspaceRuntimeImageReplacementOperation) error {
	exists, err := app.auditIdentityExists(ctx, operation.Input.AccountID, operation.AuditEvent)
	if err != nil {
		return errWorkspaceRuntimeImageReplacementState
	}
	if exists {
		return nil
	}
	if err := app.tables.SaveAuditEvent(ctx, operation.AuditEvent); err != nil {
		return errWorkspaceRuntimeImageReplacementState
	}
	return nil
}

// workspaceRuntimeImageReplacementFailure converts provider details into a
// stable operator-visible code and decides whether the durable operation can
// safely be retried. Identity/configuration conflicts are terminal; transport
// and provider convergence failures remain started for the worker to retry.
func workspaceRuntimeImageReplacementFailure(err error) (string, bool) {
	if err == nil {
		return "", false
	}
	var httpErr *clients.FabricHTTPError
	bodyCode := ""
	if errors.As(err, &httpErr) {
		var body struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(httpErr.Body), &body) == nil {
			bodyCode = strings.TrimSpace(body.Error)
		}
	}
	knownCodes := []string{
		"workspace_runtime_image_replacement_input_invalid",
		"workspace_runtime_image_replacement_conflict",
		"workspace_runtime_image_replacement_unavailable",
		"workspace_runtime_status_ownership_conflict",
		"workspace_runtime_status_readback_mismatch",
		"workspace_runtime_status_iam_rbac",
		"workspace_runtime_status_timeout",
		"workspace_runtime_status_provider_error",
		"runtime_operation_in_progress",
		"runtime_operation_failed",
		"launch_stage_binding_conflict",
	}
	code := bodyCode
	if code == "" {
		message := err.Error()
		for _, candidate := range knownCodes {
			if strings.Contains(message, candidate) {
				code = candidate
				break
			}
		}
	}
	if code == "" {
		if httpErr != nil && httpErr.StatusCode >= http.StatusBadRequest && httpErr.StatusCode < http.StatusInternalServerError {
			return "workspace_runtime_image_replacement_request_rejected", false
		}
		return "workspace_runtime_image_replacement_provider_error", true
	}
	switch code {
	case "workspace_runtime_status_timeout", "workspace_runtime_status_provider_error", "runtime_operation_in_progress":
		return code, true
	default:
		return code, false
	}
}

func (app *controlPlaneServer) runWorkspaceRuntimeImageReplacementsOnce(ctx context.Context, service *controlplane.Service) error {
	rows, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{Action: workspaceRuntimeImageReplacementAction, Statuses: []string{"started"}})
	if err != nil {
		return err
	}
	var errs []error
	for _, row := range rows {
		if operationID := stringValue(row["operationId"]); operationID != "" {
			if err := app.runWorkspaceRuntimeImageReplacement(ctx, service, operationID); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (app *controlPlaneServer) startWorkspaceRuntimeImageReplacementWorker(ctx context.Context, service *controlplane.Service, interval time.Duration) {
	if interval <= 0 {
		interval = workspaceLaunchWorkerInterval()
	}
	go func() {
		_ = app.runWorkspaceRuntimeImageReplacementsOnce(ctx, service)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = app.runWorkspaceRuntimeImageReplacementsOnce(ctx, service)
			}
		}
	}()
}
