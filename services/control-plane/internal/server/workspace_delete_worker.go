package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"opl-cloud/services/control-plane/internal/controlplane"
)

// The persisted, identity-bound Delete is the customer's authorization. The
// worker continues only that operation, including after its Workspace is gone.
func (app *controlPlaneServer) startWorkspaceDeleteWorker(ctx context.Context, service *controlplane.Service, interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			if err := app.runWorkspaceDeletesOnce(ctx, service); err != nil {
				log.Printf("workspace delete recovery failed: %v", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (app *controlPlaneServer) runWorkspaceDeletesOnce(ctx context.Context, service *controlplane.Service) error {
	query := runtimeOperationQuery{Action: workspaceDeleteAction, Statuses: []string{"running", "manual_review"}, Limit: 50}
	var errs []error
	for {
		page, err := app.tables.PageRuntimeOperations(ctx, query)
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		for _, row := range page.Items {
			created, err := time.Parse(time.RFC3339Nano, stringValue(row["createdAt"]))
			if err != nil {
				return errors.Join(append(errs, errors.New("workspace_delete_cursor_invalid"))...)
			}
			query.AfterCreatedAt, query.AfterID = created, stringValue(row["id"])
			operation, err := decodeWorkspaceDeleteOperation(row)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			unlock := app.lockResource("workspace-delete", operation.WorkspaceID)
			current, found, readErr := app.workspaceDeleteOperation(ctx, operation.WorkspaceID)
			if readErr == nil && found && current.Phase != "complete" {
				_, readErr = app.runWorkspaceDelete(ctx, service, current)
			}
			unlock()
			if readErr != nil && !errors.Is(readErr, errWorkspaceDeletePending) && !errors.Is(readErr, errWorkspaceDeleteUnconfirmed) && !errors.Is(readErr, errWorkspaceDeleteCASConflict) {
				errs = append(errs, readErr)
			}
		}
		if len(page.Items) < query.Limit {
			return errors.Join(errs...)
		}
	}
}

type workspaceDeletionStatusDTO struct {
	WorkspaceID string `json:"workspaceId"`
	OperationID string `json:"operationId"`
	Status      string `json:"status"`
	Phase       string `json:"phase"`
	ReceiptID   string `json:"receiptId,omitempty"`
}

func (app *controlPlaneServer) workspaceDeletionStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	operation, found, err := app.workspaceDeleteOperation(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "workspace_deletion_read_unavailable")
		return
	}
	if !found {
		workspace, exists, readErr := app.tables.GetWorkspace(r.Context(), strings.TrimSpace(r.PathValue("workspaceId")))
		if readErr != nil {
			writeError(w, http.StatusInternalServerError, "workspace_deletion_read_unavailable")
			return
		}
		if !exists {
			writeError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		if firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])) != stringValue(user["accountId"]) || firstNonEmpty(stringValue(workspace["ownerUserId"]), stringValue(workspace["ownerId"])) != stringValue(user["id"]) {
			writeError(w, http.StatusForbidden, "workspace_owner_required")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, nil)
		return
	}
	if operation.AccountID != stringValue(user["accountId"]) || operation.OwnerUserID != stringValue(user["id"]) {
		writeError(w, http.StatusForbidden, "workspace_owner_required")
		return
	}
	if !validWorkspaceDeleteIdentity(operation) {
		writeError(w, http.StatusConflict, "workspace_deletion_identity_conflict")
		return
	}
	status := "pending"
	if operation.Status == "manual_review" {
		status = "manual_review"
	}
	if operation.Status == "succeeded" && operation.Phase == "complete" {
		if _, exists, readErr := app.tables.GetWorkspace(r.Context(), operation.WorkspaceID); readErr != nil || exists {
			writeError(w, http.StatusConflict, "workspace_deletion_terminal_conflict")
			return
		}
		status = "deleted"
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, workspaceDeletionStatusDTO{WorkspaceID: operation.WorkspaceID, OperationID: operation.OperationID, Status: status, Phase: operation.Phase, ReceiptID: operation.DeletionReceiptID})
}
