package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
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
	// Completed deletions remain in scope so the platform refund operation they
	// own can still be dispatched or recovered after a restart.
	query := runtimeOperationQuery{Action: workspaceDeleteAction, Statuses: []string{"running", "manual_review", "succeeded"}, Limit: 50}
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
			} else if readErr == nil && found {
				readErr = app.runWorkspaceDeleteRefund(ctx, service, current)
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

// Workspace deletion block classifications. An identity conflict needs a human,
// while an unavailable or unconfirmed readback is retried by the same worker, so
// the page must not present them as the same kind of stop.
const (
	workspaceDeleteBlockRetryableReadback = "retryable_readback"
	workspaceDeleteBlockIdentity          = "blocked_identity"
)

// workspaceDeleteIdentityBlockCodes names every block reason that means the
// provider returned resources or facts belonging to something other than this
// Workspace. It is an explicit enumeration, not a substring match, so a new code
// must be classified deliberately.
func workspaceDeleteIdentityBlockCodes() map[string]bool {
	return map[string]bool{
		"workspace_delete_identity_mismatch": true,
		"fabric_runtime_identity_conflict":   true,
		"fabric_storage_identity_conflict":   true,
		"fabric_compute_identity_conflict":   true,
	}
}

// workspaceDeleteBlockClass classifies a durable block reason. Unknown codes stay
// retryable so the worker keeps trying instead of the page claiming a permanent
// conflict it cannot prove.
func workspaceDeleteBlockClass(errorCode string) string {
	if workspaceDeleteIdentityBlockCodes()[errorCode] {
		return workspaceDeleteBlockIdentity
	}
	return workspaceDeleteBlockRetryableReadback
}

// workspaceDeletePageState projects the durable operation onto the customer-visible
// deletion page state: waiting, retrying, blocked, or completed.
func workspaceDeletePageState(operation workspaceDeleteOperation) string {
	if operation.Status == "succeeded" && operation.Phase == "complete" {
		return contracts.WorkspaceDeletePageStateCompleted
	}
	if operation.Status == "manual_review" && workspaceDeleteBlockClass(operation.LastErrorCode) == workspaceDeleteBlockIdentity {
		return contracts.WorkspaceDeletePageStateBlocked
	}
	if operation.LastErrorCode != "" || operation.ComputeStatus == "destroying" {
		return contracts.WorkspaceDeletePageStateRetrying
	}
	return contracts.WorkspaceDeletePageStateWaiting
}

// workspaceDeleteStageInProgress reports which platform deletion stage this
// operation is currently working on. It walks the frozen stage order against the
// facts the operation itself has recorded — the same facts validWorkspaceDeleteState
// enforces — so it never has to interpret the durable phase. The durable phase
// names the stage that just completed, which is why the phase alone would name the
// wrong stage during a long wait such as an in-flight compute termination.
func workspaceDeleteStageInProgress(operation workspaceDeleteOperation) string {
	confirmed := map[string]bool{
		contracts.WorkspaceDeleteStageRuntimeAbsent:    operation.RuntimeStatus == "absent" && operation.SecretStatus == "absent",
		contracts.WorkspaceDeleteStageAttachmentAbsent: operation.AttachmentStatus == "absent",
		contracts.WorkspaceDeleteStageStorageAbsent:    operation.StorageStatus == "absent",
		contracts.WorkspaceDeleteStageComputeAbsent:    operation.ComputeStatus == "absent",
		// Removing the Workspace projection is committed atomically with the phase
		// advance to workspace_absent, so that phase already proves the projection is
		// gone and the operation is working on the deletion receipt.
		contracts.WorkspaceDeleteStageWorkspaceAbsent: operation.Phase == "workspace_absent" ||
			operation.Phase == "deletion_receipt_recorded" || operation.Phase == "complete",
		contracts.WorkspaceDeleteStageReceiptRecorded: operation.DeletionReceiptID != "",
	}
	for _, stage := range contracts.WorkspaceDeleteStageOrder() {
		if !confirmed[stage] {
			return stage
		}
	}
	return contracts.WorkspaceDeleteStageReceiptRecorded
}

type workspaceDeletionStatusDTO struct {
	WorkspaceID string `json:"workspaceId"`
	OperationID string `json:"operationId"`
	Status      string `json:"status"`
	// Stage is the platform deletion stage this operation has reached. It is
	// projected from the durable phase by the owning vocabulary, so Console never
	// interprets the internal phase itself.
	Stage string `json:"stage,omitempty"`
	// Phase is the durable operation phase, retained for the technical panel.
	Phase string `json:"phase"`
	// PageState is waiting, retrying, blocked, or completed.
	PageState string `json:"pageState,omitempty"`
	// ReasonCode is the stable block reason when the operation stopped,
	// LastReadbackAt is the most recent observation any stage confirmation
	// recorded, and NextRetryAt is when the worker's next readback is due.
	ReasonCode     string `json:"reasonCode,omitempty"`
	LastReadbackAt string `json:"lastReadbackAt,omitempty"`
	NextRetryAt    string `json:"nextRetryAt,omitempty"`
	ReceiptID      string `json:"receiptId,omitempty"`
	// Resource deletion and platform refund are reported separately. The refund
	// is dispatchable only after the deletion readback precondition is met.
	RefundStatus            string `json:"refundStatus,omitempty"`
	RefundReasonCode        string `json:"refundReasonCode,omitempty"`
	RefundOperationID       string `json:"refundOperationId,omitempty"`
	RefundReceiptID         string `json:"refundReceiptId,omitempty"`
	RefundUSDMicros         int64  `json:"refundUsdMicros,omitempty"`
	OriginalChargeUSDMicros int64  `json:"originalChargeUsdMicros,omitempty"`
	RefundPolicyVersion     string `json:"refundPolicyVersion,omitempty"`
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
	refund, refundFound, refundErr := app.workspaceDeleteRefundRecord(r.Context(), operation.WorkspaceID)
	if refundErr != nil {
		writeError(w, http.StatusInternalServerError, "workspace_deletion_read_unavailable")
		return
	}
	dto := workspaceDeletionStatusDTO{WorkspaceID: operation.WorkspaceID, OperationID: operation.OperationID, Status: status, Phase: operation.Phase, ReceiptID: operation.DeletionReceiptID}
	dto.Stage = workspaceDeleteStageInProgress(operation)
	dto.PageState = workspaceDeletePageState(operation)
	dto.ReasonCode = operation.LastErrorCode
	dto.LastReadbackAt = workspaceDeleteLastReadbackAt(operation)
	if operation.ComputeStatus == "destroying" {
		dto.NextRetryAt = operation.ComputeReadbackNotBefore
	}
	if refundFound {
		dto.RefundStatus, dto.RefundReasonCode, dto.RefundOperationID, dto.RefundReceiptID = refund.Status, refund.ReasonCode, refund.WalletAdjustmentOperationID, refund.RefundReceiptID
		dto.RefundUSDMicros, dto.OriginalChargeUSDMicros, dto.RefundPolicyVersion = refund.RefundUSDMicros, refund.OriginalChargeUSDMicros, refund.PolicyVersion
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, dto)
}
