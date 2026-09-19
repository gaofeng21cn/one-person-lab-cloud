package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const (
	workspaceDeleteAction       = "workspace.delete.v2"
	workspaceDeleteLegacyAction = "workspace.delete.v1"
	// Retained v2 encoding marker; delayed absence no longer has a lifetime read limit.
	workspaceDeleteComputeReadbackBudget   = 8
	workspaceDeleteComputeReadbackInterval = time.Second
)

var (
	errWorkspaceDeleteCASConflict        = errors.New("workspace_delete_cas_conflict")
	errWorkspaceDeleteUnconfirmed        = errors.New("workspace_delete_unconfirmed")
	errWorkspaceDeletePending            = errors.New("workspace_delete_pending")
	errWorkspaceDeleteHistoricalConflict = errors.New("workspace_delete_historical_manual_review")
	errWorkspaceDeleteTerminalConflict   = errors.New("workspace_delete_terminal_conflict")
	errWorkspaceDeleteStateRead          = errors.New("workspace_delete_state_read_failed")
)

// Historical KeyStatus and KeyDelete fields preserve earlier operations; current deletion leaves Gateway keys untouched.
type workspaceDeleteOperation struct {
	SchemaVersion                  int                                `json:"schemaVersion"`
	OperationID                    string                             `json:"operationId"`
	RequestHash                    string                             `json:"requestHash"`
	AccountID                      string                             `json:"accountId"`
	OwnerUserID                    string                             `json:"ownerUserId"`
	Sub2APIUserID                  int64                              `json:"sub2apiUserId"`
	WorkspaceID                    string                             `json:"workspaceId"`
	ResourceType                   string                             `json:"resourceType"`
	ResourceID                     string                             `json:"resourceId"`
	LaunchOperationID              string                             `json:"launchOperationId"`
	LaunchReceiptID                string                             `json:"launchReceiptId"`
	RuntimeID                      string                             `json:"runtimeId"`
	RuntimeServiceName             string                             `json:"runtimeServiceName"`
	ComputeID                      string                             `json:"computeId"`
	StorageID                      string                             `json:"storageId"`
	AttachmentID                   string                             `json:"attachmentId"`
	WorkspaceAPIKeyID              int64                              `json:"workspaceApiKeyId"`
	GatewaySecretRef               string                             `json:"gatewaySecretRef"`
	GatewayFingerprint             string                             `json:"gatewayFingerprint"`
	DeletionReceiptID              string                             `json:"deletionReceiptId,omitempty"`
	Phase                          string                             `json:"phase"`
	Status                         string                             `json:"status"`
	RuntimeStatus                  string                             `json:"runtimeStatus,omitempty"`
	SecretStatus                   string                             `json:"secretStatus,omitempty"`
	AttachmentStatus               string                             `json:"attachmentStatus,omitempty"`
	StorageStatus                  string                             `json:"storageStatus,omitempty"`
	ComputeStatus                  string                             `json:"computeStatus,omitempty"`
	ComputeReadbacks               int                                `json:"computeReadbacks,omitempty"`
	MaxComputeReadbacks            int                                `json:"maxComputeReadbacks,omitempty"`
	ComputeReadbackNotBefore       string                             `json:"computeReadbackNotBefore,omitempty"`
	KeyStatus                      string                             `json:"keyStatus,omitempty"`
	KeyDeleteAttempted             bool                               `json:"keyDeleteAttempted,omitempty"`
	KeyDeleteReplay                workspaceDeleteReplayAuthorization `json:"keyDeleteReplay,omitempty"`
	ProvisioningMode               string                             `json:"provisioningMode,omitempty"`
	CurrentApplicationDeploymentID string                             `json:"currentApplicationDeploymentId,omitempty"`
	// Identity facts of the original Launch/Delete operation. They bind every
	// later provider readback to the exact resources this deletion destroyed.
	LaunchFulfilledAt         string `json:"launchFulfilledAt,omitempty"`
	StorageProviderResourceID string `json:"storageProviderResourceId,omitempty"`
	ComputeMachineName        string `json:"computeMachineName,omitempty"`
	ComputeCVMInstanceID      string `json:"computeCvmInstanceId,omitempty"`
	DeletedAt                 string `json:"deletedAt,omitempty"`
	// StageEvidence records the accepted confirmation of each deletion stage. It is
	// appended to atomically with the phase advance and is never overwritten: a
	// confirmed stage binding is the evidence a later receipt and the refund gate
	// both rely on.
	StageEvidence      []contracts.WorkspaceDeleteStageEvidence `json:"stageEvidence,omitempty"`
	ApplicationCleanup *workspaceApplicationLifecycleOperation  `json:"applicationCleanup,omitempty"`
	ApplicationSecrets *workspaceApplicationSecretCleanup       `json:"applicationSecrets,omitempty"`
	LastErrorCode      string                                   `json:"lastErrorCode,omitempty"`
	CreatedAt          string                                   `json:"createdAt"`
}

type workspaceDeleteLegacyOperation struct {
	OperationID              string                             `json:"operationId"`
	RequestHash              string                             `json:"requestHash"`
	AccountID                string                             `json:"accountId"`
	OwnerUserID              string                             `json:"ownerUserId"`
	Sub2APIUserID            int64                              `json:"sub2apiUserId"`
	WorkspaceID              string                             `json:"workspaceId"`
	LaunchOperationID        string                             `json:"launchOperationId"`
	RuntimeID                string                             `json:"runtimeId"`
	ComputeID                string                             `json:"computeId"`
	StorageID                string                             `json:"storageId"`
	AttachmentID             string                             `json:"attachmentId"`
	WorkspaceAPIKeyID        int64                              `json:"workspaceApiKeyId"`
	GatewaySecretRef         string                             `json:"gatewaySecretRef"`
	GatewayFingerprint       string                             `json:"gatewayFingerprint"`
	DebitCode                string                             `json:"debitCode"`
	PurchaseReceiptID        string                             `json:"purchaseReceiptId"`
	PurchaseReceipt          clients.ReceiptInput               `json:"purchaseReceipt"`
	RefundCode               string                             `json:"refundCode"`
	RefundReceiptID          string                             `json:"refundReceiptId,omitempty"`
	TotalUSDMicros           int64                              `json:"totalUsdMicros"`
	Phase                    string                             `json:"phase"`
	Status                   string                             `json:"status"`
	RuntimeStatus            string                             `json:"runtimeStatus,omitempty"`
	SecretStatus             string                             `json:"secretStatus,omitempty"`
	AttachmentStatus         string                             `json:"attachmentStatus,omitempty"`
	StorageStatus            string                             `json:"storageStatus,omitempty"`
	ComputeStatus            string                             `json:"computeStatus,omitempty"`
	ComputeReadbacks         int                                `json:"computeReadbacks,omitempty"`
	MaxComputeReadbacks      int                                `json:"maxComputeReadbacks,omitempty"`
	ComputeReadbackNotBefore string                             `json:"computeReadbackNotBefore,omitempty"`
	KeyStatus                string                             `json:"keyStatus,omitempty"`
	KeyDeleteAttempted       bool                               `json:"keyDeleteAttempted,omitempty"`
	RefundAttempted          bool                               `json:"refundAttempted,omitempty"`
	KeyDeleteReplay          workspaceDeleteReplayAuthorization `json:"keyDeleteReplay,omitempty"`
	RefundReplay             workspaceDeleteReplayAuthorization `json:"refundReplay,omitempty"`
	RefundConfirmation       map[string]any                     `json:"refundConfirmation,omitempty"`
	LastErrorCode            string                             `json:"lastErrorCode,omitempty"`
	CreatedAt                string                             `json:"createdAt"`
}

type workspaceDeleteReplayAuthorization struct {
	SchemaVersion     int    `json:"schemaVersion,omitempty"`
	AuthorizationID   string `json:"authorizationId,omitempty"`
	IdempotencyKey    string `json:"idempotencyKey,omitempty"`
	State             string `json:"state,omitempty"`
	LeaseGeneration   int    `json:"leaseGeneration,omitempty"`
	LeaseExpiresAt    string `json:"leaseExpiresAt,omitempty"`
	DispatchStartedAt string `json:"dispatchStartedAt,omitempty"`
	ConsumedAt        string `json:"consumedAt,omitempty"`
}

type workspaceDeleteStoreMutation struct {
	Create                 bool
	DeleteWorkspace        bool
	RequireWorkspaceAbsent bool
	ExpectedResult         string
	DesiredOperation       map[string]any
}

type workspaceDeleteGatewayIdentity struct {
	WorkspaceAPIKeyID  int64
	GatewaySecretRef   string
	GatewayFingerprint string
}

func (app *controlPlaneServer) deleteWorkspace(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	_, ok := requiredMutationKey(w, r)
	if !ok {
		return
	}
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return
	}
	sub2APIUserID, userErr := app.sub2APIUserID(r.Context(), stringValue(user["accountId"]))
	if userErr != nil {
		writeError(w, http.StatusBadGateway, "sub2api_account_unavailable")
		return
	}
	workspaceID := strings.TrimSpace(r.PathValue("workspaceId"))
	if workspaceID == "" {
		writeError(w, http.StatusNotFound, "workspace_not_found")
		return
	}

	unlock := app.lockResource("workspace-delete", workspaceID)
	defer unlock()

	operation, found, err := app.workspaceDeleteOperation(r.Context(), workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "state_read_failed")
		return
	}
	if !found {
		legacy, legacyFound, legacyErr := app.workspaceDeleteLegacyOperation(r.Context(), workspaceID)
		if legacyErr != nil {
			writeJSON(w, http.StatusConflict, workspaceDeleteHistoricalConflictResponse(workspaceID, workspaceDeleteLegacyOperationID(workspaceID)))
			return
		}
		if legacyFound {
			if legacy.AccountID != stringValue(user["accountId"]) || legacy.OwnerUserID != stringValue(user["id"]) {
				writeError(w, http.StatusForbidden, "workspace_owner_required")
				return
			}
			if legacy.Sub2APIUserID != sub2APIUserID || !validWorkspaceDeleteLegacyTerminal(legacy) {
				writeJSON(w, http.StatusConflict, workspaceDeleteHistoricalConflictResponse(workspaceID, legacy.OperationID))
				return
			}
			if _, workspaceFound, readErr := app.tables.GetWorkspace(r.Context(), workspaceID); readErr != nil {
				writeError(w, http.StatusInternalServerError, "state_read_failed")
				return
			} else if workspaceFound {
				writeJSON(w, http.StatusConflict, workspaceDeleteHistoricalConflictResponse(workspaceID, legacy.OperationID))
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"workspaceId": workspaceID, "operationId": legacy.OperationID, "status": "deleted", "historical": true})
			return
		}

		workspace, workspaceFound, readErr := app.tables.GetWorkspace(r.Context(), workspaceID)
		if readErr != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if !workspaceFound {
			writeError(w, http.StatusNotFound, "workspace_not_found")
			return
		}
		accountID := firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"]))
		ownerUserID := firstNonEmpty(stringValue(workspace["ownerUserId"]), stringValue(workspace["ownerId"]))
		if accountID != stringValue(user["accountId"]) || !app.canAccessResource(r, workspace) {
			writeError(w, http.StatusForbidden, "account_scope_forbidden")
			return
		}
		if ownerUserID == "" || ownerUserID != stringValue(user["id"]) {
			writeError(w, http.StatusForbidden, "workspace_owner_required")
			return
		}
		operation, err = app.newWorkspaceDeleteOperation(r.Context(), service, workspace, sub2APIUserID, time.Now().UTC())
		if err != nil {
			if errors.Is(err, errWorkspaceKeyRotationInProgress) {
				writeError(w, http.StatusConflict, errWorkspaceKeyRotationInProgress.Error())
				return
			}
			writeError(w, http.StatusBadGateway, "workspace_delete_identity_unconfirmed")
			return
		}
		if err := app.tables.ApplyWorkspaceDelete(r.Context(), workspaceDeleteStoreMutation{Create: true, DesiredOperation: workspaceDeleteOperationRow(operation)}); errors.Is(err, errWorkspaceDeleteCASConflict) {
			operation, found, err = app.workspaceDeleteOperation(r.Context(), workspaceID)
			if err != nil || !found {
				writeError(w, http.StatusConflict, errWorkspaceDeleteCASConflict.Error())
				return
			}
		} else if err != nil {
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
			return
		}
	}

	if operation.AccountID != stringValue(user["accountId"]) || operation.OwnerUserID != stringValue(user["id"]) {
		writeError(w, http.StatusForbidden, "workspace_owner_required")
		return
	}
	if operation.RequestHash != workspaceDeleteRequestHash(operation) || operation.Sub2APIUserID != sub2APIUserID {
		writeError(w, http.StatusConflict, errIdempotencyConflict.Error())
		return
	}
	operation, err = app.runWorkspaceDelete(r.Context(), service, operation)
	if err != nil {
		if errors.Is(err, errWorkspaceDeleteTerminalConflict) {
			if auditErr := app.saveWorkspaceDeleteTerminalConflictAudit(r, operation); auditErr != nil {
				writeError(w, http.StatusInternalServerError, "state_persist_failed")
				return
			}
			writeJSON(w, http.StatusConflict, workspaceDeleteTerminalConflictResponse(operation))
			return
		}
		if errors.Is(err, errWorkspaceDeleteStateRead) {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if errors.Is(err, errWorkspaceDeletePending) {
			if retryAfter := workspaceDeleteComputeRetryAfter(operation, time.Now().UTC()); retryAfter > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
			}
			writeJSON(w, http.StatusAccepted, workspaceDeletePendingResponse(operation))
			return
		}
		if errors.Is(err, errWorkspaceDeleteUnconfirmed) {
			writeJSON(w, http.StatusBadGateway, workspaceDeleteResponse(operation, "workspace_delete_unconfirmed"))
			return
		}
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	// Delete success and refund success are independent results: a refused or
	// failed platform refund never rewrites the completed deletion.
	_ = app.runWorkspaceDeleteRefund(r.Context(), service, operation)
	writeJSON(w, http.StatusOK, workspaceDeleteResponse(operation, ""))
}

func (app *controlPlaneServer) workspaceDeleteOperation(ctx context.Context, workspaceID string) (workspaceDeleteOperation, bool, error) {
	row, found, err := app.tables.GetRuntimeOperation(ctx, workspaceDeleteOperationID(workspaceID))
	if err != nil || !found {
		return workspaceDeleteOperation{}, found, err
	}
	operation, err := decodeWorkspaceDeleteOperation(row)
	return operation, err == nil, err
}

func (app *controlPlaneServer) workspaceDeleteLegacyOperation(ctx context.Context, workspaceID string) (workspaceDeleteLegacyOperation, bool, error) {
	row, found, err := app.tables.GetRuntimeOperation(ctx, workspaceDeleteLegacyOperationID(workspaceID))
	if err != nil || !found {
		return workspaceDeleteLegacyOperation{}, found, err
	}
	operation, err := decodeWorkspaceDeleteLegacyOperation(row)
	return operation, err == nil, err
}

func decodeWorkspaceDeleteLegacyOperation(row map[string]any) (workspaceDeleteLegacyOperation, error) {
	var operation workspaceDeleteLegacyOperation
	if stringValue(row["action"]) != workspaceDeleteLegacyAction || json.Unmarshal([]byte(stringValue(row["result"])), &operation) != nil ||
		operation.OperationID == "" || operation.OperationID != workspaceDeleteLegacyOperationID(operation.WorkspaceID) || operation.OperationID != stringValue(row["id"]) ||
		operation.OperationID != stringValue(row["operationId"]) || operation.AccountID == "" || operation.AccountID != stringValue(row["accountId"]) ||
		operation.OwnerUserID == "" || operation.Sub2APIUserID <= 0 || operation.WorkspaceID == "" || operation.WorkspaceID != stringValue(row["workspaceId"]) ||
		operation.WorkspaceID != stringValue(row["resourceId"]) || stringValue(row["resourceKind"]) != "workspace" || operation.Status != stringValue(row["status"]) ||
		operation.ComputeID != stringValue(row["computeAllocationId"]) || operation.StorageID != stringValue(row["storageId"]) ||
		operation.AttachmentID != stringValue(row["attachmentId"]) || operation.RuntimeID != stringValue(row["runtimeId"]) ||
		operation.CreatedAt == "" || operation.CreatedAt != stringValue(row["createdAt"]) || !validWorkspaceDeleteLegacyIdentity(operation) ||
		operation.RequestHash == "" || operation.RequestHash != workspaceDeleteLegacyRequestHash(operation) {
		return workspaceDeleteLegacyOperation{}, errors.New("workspace_delete_legacy_operation_invalid")
	}
	return operation, nil
}

func workspaceDeleteLegacyRequestHash(operation workspaceDeleteLegacyOperation) string {
	purchaseReceipt, _ := json.Marshal(operation.PurchaseReceipt)
	return stableID(workspaceDeleteLegacyAction, operation.AccountID, operation.OwnerUserID, operation.WorkspaceID, operation.LaunchOperationID,
		operation.RuntimeID, operation.ComputeID, operation.StorageID, operation.AttachmentID, operation.GatewaySecretRef, operation.GatewayFingerprint,
		operation.DebitCode, operation.PurchaseReceiptID, string(purchaseReceipt), operation.RefundCode,
		strconv.FormatInt(operation.Sub2APIUserID, 10), strconv.FormatInt(operation.WorkspaceAPIKeyID, 10), strconv.FormatInt(operation.TotalUSDMicros, 10))
}

func validWorkspaceDeleteLegacyIdentity(operation workspaceDeleteLegacyOperation) bool {
	if operation.OperationID == "" || operation.OperationID != workspaceDeleteLegacyOperationID(operation.WorkspaceID) || operation.AccountID == "" || operation.OwnerUserID == "" ||
		operation.Sub2APIUserID <= 0 || operation.WorkspaceID == "" || operation.LaunchOperationID == "" || operation.RuntimeID == "" || operation.ComputeID == "" ||
		operation.StorageID == "" || operation.AttachmentID == "" || operation.WorkspaceAPIKeyID <= 0 || operation.GatewaySecretRef == "" || operation.GatewayFingerprint == "" ||
		operation.DebitCode == "" || operation.PurchaseReceiptID == "" || operation.RefundCode == "" || operation.RefundCode == operation.DebitCode || operation.TotalUSDMicros <= 0 ||
		operation.PurchaseReceipt.Type != "billing.workspace_purchased.v1" || operation.PurchaseReceipt.Status != "completed" ||
		operation.PurchaseReceipt.AccountID != operation.AccountID || operation.PurchaseReceipt.WorkspaceID != operation.WorkspaceID ||
		operation.PurchaseReceipt.RequestID != operation.LaunchOperationID || operation.CreatedAt == "" ||
		!validWorkspaceDeleteReplayAuthorization(operation.KeyDeleteReplay, operation.OperationID+":key") ||
		!validWorkspaceDeleteReplayAuthorization(operation.RefundReplay, operation.RefundCode) {
		return false
	}
	cost, execution := operation.PurchaseReceipt.Cost, operation.PurchaseReceipt.Execution
	return int64(numberField(cost, "sub2apiUserId", 0)) == operation.Sub2APIUserID && stringValue(cost["sub2apiRedeemCode"]) == operation.DebitCode &&
		int64(numberField(cost, "totalUsdMicros", 0)) == operation.TotalUSDMicros && stringValue(cost["resourceId"]) == operation.WorkspaceID &&
		stringValue(execution["runtimeId"]) == operation.RuntimeID && stringValue(execution["computeAllocationId"]) == operation.ComputeID &&
		stringValue(execution["storageId"]) == operation.StorageID && stringValue(execution["attachmentId"]) == operation.AttachmentID &&
		int64(numberField(execution, "workspaceApiKeyId", 0)) == operation.WorkspaceAPIKeyID
}

func validWorkspaceDeleteLegacyTerminal(operation workspaceDeleteLegacyOperation) bool {
	if operation.Phase != "complete" || operation.Status != "succeeded" || operation.RuntimeStatus != "absent" || operation.SecretStatus != "absent" ||
		operation.AttachmentStatus != "detached" || operation.KeyStatus != "absent" || operation.RefundReceiptID == "" || operation.LastErrorCode != "" ||
		!workspaceDeleteLegacyComputeTerminal(operation.ComputeStatus) {
		return false
	}
	if operation.StorageStatus != "destroyed" && operation.StorageStatus != "external_deleted" {
		return false
	}
	return stringValue(operation.RefundConfirmation["code"]) == operation.RefundCode &&
		int64(numberField(operation.RefundConfirmation, "userId", 0)) == operation.Sub2APIUserID &&
		int64(numberField(operation.RefundConfirmation, "refundUsdMicros", 0)) == operation.TotalUSDMicros &&
		stringValue(operation.RefundConfirmation["status"]) == "used"
}

func workspaceDeleteLegacyComputeTerminal(status string) bool {
	switch status {
	case "destroyed", "external_deleted", "deleted", "missing":
		return true
	default:
		return false
	}
}

func workspaceDeleteHistoricalConflictResponse(workspaceID, operationID string) map[string]any {
	return map[string]any{"workspaceId": workspaceID, "operationId": operationID, "status": "manual_review", "error": errWorkspaceDeleteHistoricalConflict.Error()}
}

func workspaceDeleteTerminalConflictResponse(operation workspaceDeleteOperation) map[string]any {
	return map[string]any{
		"workspaceId": operation.WorkspaceID, "operationId": operation.OperationID,
		"status": "manual_review", "error": errWorkspaceDeleteTerminalConflict.Error(),
	}
}

func (app *controlPlaneServer) saveWorkspaceDeleteTerminalConflictAudit(r *http.Request, operation workspaceDeleteOperation) error {
	action := "workspace.delete.terminal_conflict"
	after := map[string]any{"operationId": operation.OperationID, "error": errWorkspaceDeleteTerminalConflict.Error()}
	event := app.auditEvent(r, action, "workspace", operation.WorkspaceID, operation.AccountID, nil, after, "conflict")
	event["id"] = "audit-" + stableID(action, operation.OperationID, errWorkspaceDeleteTerminalConflict.Error())[:12]
	return app.tables.SaveAuditEvent(r.Context(), event)
}

func (app *controlPlaneServer) newWorkspaceDeleteOperation(ctx context.Context, service *controlplane.Service, workspace map[string]any, sub2APIUserID int64, now time.Time) (workspaceDeleteOperation, error) {
	workspaceID := stringValue(workspace["id"])
	accountID := firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"]))
	ownerUserID := firstNonEmpty(stringValue(workspace["ownerUserId"]), stringValue(workspace["ownerId"]))
	projection := workspaceLaunchStableProjectionMismatchFields
	if stringValue(workspace["currentApplicationDeploymentId"]) != "" {
		projection = workspaceLaunchResourceProjectionMismatchFields
	}
	launch, found, err := app.canonicalWorkspaceLaunch(ctx, workspace, projection, nil)
	if err != nil || !found || launch.int64Fact("sub2apiUserId") != sub2APIUserID {
		return workspaceDeleteOperation{}, errWorkspaceDeleteUnconfirmed
	}
	gatewayIdentity, err := app.currentWorkspaceDeleteGatewayIdentity(ctx, workspace, launch)
	if err != nil {
		if errors.Is(err, errWorkspaceKeyRotationInProgress) {
			return workspaceDeleteOperation{}, err
		}
		return workspaceDeleteOperation{}, errWorkspaceDeleteUnconfirmed
	}
	current, err := workspaceLaunchPurchaseReceiptInput(launch)
	if err != nil {
		return workspaceDeleteOperation{}, errWorkspaceDeleteUnconfirmed
	}
	expected := []clients.ReceiptInput{current}
	if launch.raw["resourceBillingEnabled"] != nil && !launch.boolFact("resourceBillingEnabled") {
		expected = append(expected, workspaceLaunchLegacyCreatedReceiptInput(launch))
	} else {
		expected = append(expected, workspaceLaunchHistoricalChargedReceiptInput(current))
	}
	launchReceiptID := launch.stringFact("receiptId")
	receipt, err := service.BillingReceiptForAccount(ctx, accountID, workspaceID, launchReceiptID)
	if err != nil || receipt.ReceiptID != launchReceiptID || !workspaceDeleteLaunchReceiptMatches(receipt.ReceiptInput, expected) {
		return workspaceDeleteOperation{}, errWorkspaceDeleteUnconfirmed
	}
	operation := workspaceDeleteOperation{
		SchemaVersion: 2, OperationID: workspaceDeleteOperationID(workspaceID), AccountID: accountID, OwnerUserID: ownerUserID, Sub2APIUserID: sub2APIUserID,
		WorkspaceID: workspaceID, ResourceType: "workspace", ResourceID: workspaceID, LaunchOperationID: launch.ID, LaunchReceiptID: launchReceiptID,
		RuntimeID: launch.stringFact("runtimeId"), RuntimeServiceName: launch.stringFact("runtimeServiceName"), ComputeID: launch.stringFact("computeAllocationId"),
		StorageID: launch.stringFact("storageId"), AttachmentID: launch.stringFact("attachmentId"), WorkspaceAPIKeyID: gatewayIdentity.WorkspaceAPIKeyID,
		GatewaySecretRef: gatewayIdentity.GatewaySecretRef, GatewayFingerprint: gatewayIdentity.GatewayFingerprint,
		ProvisioningMode:  launch.provisioningModeWire(),
		LaunchFulfilledAt: launch.stringFact("workspaceActivatedAt"),
		Phase:             "claimed", Status: "running", CreatedAt: now.Format(time.RFC3339Nano),
	}
	operation.CurrentApplicationDeploymentID = stringValue(workspace["currentApplicationDeploymentId"])
	// Capture the provider storage identity from the Control Plane projection while
	// it still exists. The deletion stage and the platform refund then bind every
	// provider readback to the exact disk this Workspace was launched with, instead
	// of learning it from a destroy response.
	if storage, found, readErr := app.tables.GetStorage(ctx, operation.StorageID); readErr == nil && found {
		operation.StorageProviderResourceID = stringValue(storage["providerResourceId"])
	}
	operation.RequestHash = workspaceDeleteRequestHash(operation)
	if !validWorkspaceDeleteIdentity(operation) {
		return workspaceDeleteOperation{}, errWorkspaceDeleteUnconfirmed
	}
	return operation, nil
}

// currentWorkspaceDeleteGatewayIdentity reads only Control Plane launch and rotation
// evidence to bind Fabric cleanup; deletion never reads or mutates Gateway keys.
func (app *controlPlaneServer) currentWorkspaceDeleteGatewayIdentity(ctx context.Context, workspace map[string]any, launch workspaceLaunchReconcileOperation) (workspaceDeleteGatewayIdentity, error) {
	workspaceID := stringValue(workspace["id"])
	if launch.provisioningMode() == contracts.WorkspaceProvisioningResourceOnly {
		// A resource-only Workspace owns no Gateway key or secret binding;
		// there is no gateway identity to confirm or clean up.
		return workspaceDeleteGatewayIdentity{}, nil
	}
	accountID := firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"]))
	currentKeyID, ok := positiveIntegerField(workspace, "workspaceApiKeyId")
	launchKeyID := launch.int64Fact("workspaceApiKeyId")
	if !ok || launchKeyID <= 0 {
		return workspaceDeleteGatewayIdentity{}, errWorkspaceDeleteUnconfirmed
	}
	rows, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{AccountID: accountID, WorkspaceID: workspaceID, Action: "workspace.gateway_key.rotate"})
	if err != nil {
		return workspaceDeleteGatewayIdentity{}, errWorkspaceDeleteUnconfirmed
	}
	byNewKeyID := make(map[int64]workspaceKeyRotationOperation, len(rows))
	for _, row := range rows {
		if stringValue(row["status"]) != "succeeded" {
			return workspaceDeleteGatewayIdentity{}, errWorkspaceKeyRotationInProgress
		}
		rotation, decodeErr := decodeWorkspaceKeyRotation(row)
		if decodeErr != nil || stringValue(row["accountId"]) != accountID || stringValue(row["workspaceId"]) != workspaceID ||
			stringValue(row["action"]) != "workspace.gateway_key.rotate" || !workspaceKeyRotationSucceededEvidenceValid(rotation) {
			return workspaceDeleteGatewayIdentity{}, errWorkspaceDeleteUnconfirmed
		}
		if _, duplicate := byNewKeyID[rotation.NewKeyID]; duplicate {
			return workspaceDeleteGatewayIdentity{}, errWorkspaceDeleteUnconfirmed
		}
		byNewKeyID[rotation.NewKeyID] = rotation
	}
	if currentKeyID == launchKeyID {
		identity := workspaceDeleteGatewayIdentity{
			WorkspaceAPIKeyID: currentKeyID, GatewaySecretRef: launch.stringFact("gatewaySecretRef"), GatewayFingerprint: launch.stringFact("workspaceKeyFingerprint"),
		}
		if len(byNewKeyID) != 0 || identity.GatewaySecretRef == "" || identity.GatewayFingerprint == "" {
			return workspaceDeleteGatewayIdentity{}, errWorkspaceDeleteUnconfirmed
		}
		return identity, nil
	}
	currentRotation, ok := byNewKeyID[currentKeyID]
	if !ok {
		return workspaceDeleteGatewayIdentity{}, errWorkspaceDeleteUnconfirmed
	}
	visited := make(map[int64]struct{}, len(byNewKeyID))
	for keyID := currentKeyID; keyID != launchKeyID; {
		if _, cycle := visited[keyID]; cycle {
			return workspaceDeleteGatewayIdentity{}, errWorkspaceDeleteUnconfirmed
		}
		rotation, exists := byNewKeyID[keyID]
		if !exists {
			return workspaceDeleteGatewayIdentity{}, errWorkspaceDeleteUnconfirmed
		}
		visited[keyID] = struct{}{}
		keyID = rotation.OldKeyID
	}
	if len(visited) != len(byNewKeyID) {
		return workspaceDeleteGatewayIdentity{}, errWorkspaceDeleteUnconfirmed
	}
	return workspaceDeleteGatewayIdentity{
		WorkspaceAPIKeyID: currentKeyID, GatewaySecretRef: currentRotation.SecretRef, GatewayFingerprint: currentRotation.Fingerprint,
	}, nil
}

func workspaceDeleteLaunchReceiptMatches(actual clients.ReceiptInput, expected []clients.ReceiptInput) bool {
	for _, candidate := range expected {
		if workspaceLaunchReceiptInputMatches(actual, candidate) {
			return true
		}
	}
	return false
}

func workspaceDeleteOperationID(workspaceID string) string {
	return "workspace-delete-" + stableID(workspaceDeleteAction, workspaceID)[:18]
}

func workspaceDeleteLegacyOperationID(workspaceID string) string {
	return "workspace-delete-" + stableID(workspaceDeleteLegacyAction, workspaceID)[:18]
}

func workspaceDeleteRequestHash(operation workspaceDeleteOperation) string {
	parts := []string{workspaceDeleteAction, strconv.Itoa(operation.SchemaVersion), operation.AccountID, operation.OwnerUserID, operation.WorkspaceID, operation.ResourceType, operation.ResourceID, operation.LaunchOperationID, operation.LaunchReceiptID,
		operation.RuntimeID, operation.RuntimeServiceName, operation.ComputeID, operation.StorageID, operation.AttachmentID,
		operation.GatewaySecretRef, operation.GatewayFingerprint, strconv.FormatInt(operation.Sub2APIUserID, 10), strconv.FormatInt(operation.WorkspaceAPIKeyID, 10)}
	// The mode stays omitted for retained full-Launch deletions so their
	// request hashes are byte-stable.
	if operation.ProvisioningMode != "" {
		parts = append(parts, operation.ProvisioningMode)
	}
	if operation.CurrentApplicationDeploymentID != "" {
		parts = append(parts, operation.CurrentApplicationDeploymentID)
	}
	return stableID(parts...)
}

func workspaceDeleteStageKey(operation workspaceDeleteOperation, stage string) string {
	return operation.OperationID + ":" + stage
}

func validWorkspaceDeleteReplayAuthorization(authorization workspaceDeleteReplayAuthorization, idempotencyKey string) bool {
	if authorization == (workspaceDeleteReplayAuthorization{}) {
		return true
	}
	if authorization.SchemaVersion != 1 || !validBillingReviewOpaqueID(authorization.AuthorizationID) || authorization.IdempotencyKey != idempotencyKey || authorization.LeaseGeneration <= 0 {
		return false
	}
	leaseExpiresAt, leaseErr := time.Parse(time.RFC3339Nano, authorization.LeaseExpiresAt)
	switch authorization.State {
	case "claimed":
		return leaseErr == nil && !leaseExpiresAt.IsZero() && authorization.DispatchStartedAt == "" && authorization.ConsumedAt == ""
	case "dispatched":
		_, dispatchErr := time.Parse(time.RFC3339Nano, authorization.DispatchStartedAt)
		return leaseErr == nil && !leaseExpiresAt.IsZero() && dispatchErr == nil && authorization.ConsumedAt == ""
	case "consumed":
		_, consumedErr := time.Parse(time.RFC3339Nano, authorization.ConsumedAt)
		dispatchValid := authorization.DispatchStartedAt == ""
		if !dispatchValid {
			_, dispatchErr := time.Parse(time.RFC3339Nano, authorization.DispatchStartedAt)
			dispatchValid = dispatchErr == nil
		}
		return leaseErr == nil && !leaseExpiresAt.IsZero() && dispatchValid && consumedErr == nil
	default:
		return false
	}
}

func workspaceDeleteOperationRow(operation workspaceDeleteOperation) map[string]any {
	encoded, _ := json.Marshal(operation)
	return map[string]any{
		"id": operation.OperationID, "operationId": operation.OperationID, "accountId": operation.AccountID, "workspaceId": operation.WorkspaceID,
		"resourceId": operation.WorkspaceID, "resourceKind": "workspace", "action": workspaceDeleteAction, "status": operation.Status, "result": string(encoded),
		"computeAllocationId": operation.ComputeID, "storageId": operation.StorageID, "attachmentId": operation.AttachmentID, "runtimeId": operation.RuntimeID,
		"createdAt": operation.CreatedAt,
	}
}

func validWorkspaceDeleteBaseIdentity(operation workspaceDeleteOperation) bool {
	resourceOnly := operation.ProvisioningMode == string(contracts.WorkspaceProvisioningResourceOnly)
	if operation.ProvisioningMode != "" && !resourceOnly {
		return false
	}
	if resourceOnly && (operation.RuntimeID != "" || operation.RuntimeServiceName != "" || operation.WorkspaceAPIKeyID > 0 ||
		operation.GatewaySecretRef != "" || operation.GatewayFingerprint != "") {
		return false
	}
	if !resourceOnly && (operation.RuntimeID == "" || operation.RuntimeServiceName == "" || operation.WorkspaceAPIKeyID <= 0 ||
		operation.GatewaySecretRef == "" || operation.GatewayFingerprint == "") {
		return false
	}
	return operation.SchemaVersion == 2 && operation.OperationID != "" && operation.OperationID == workspaceDeleteOperationID(operation.WorkspaceID) && operation.AccountID != "" && operation.OwnerUserID != "" &&
		operation.Sub2APIUserID > 0 && operation.WorkspaceID != "" && operation.LaunchOperationID != "" && operation.LaunchReceiptID != "" &&
		operation.ResourceType == "workspace" && operation.ResourceID == operation.WorkspaceID &&
		operation.ComputeID != "" && operation.StorageID != "" && operation.AttachmentID != "" && operation.CreatedAt != ""
}

func validWorkspaceDeleteIdentity(operation workspaceDeleteOperation) bool {
	return validWorkspaceDeleteBaseIdentity(operation) && validWorkspaceDeleteReplayAuthorization(operation.KeyDeleteReplay, workspaceDeleteStageKey(operation, "key")) &&
		validWorkspaceApplicationLifecycle(operation.ApplicationCleanup, operation.AccountID, operation.WorkspaceID, operation.OperationID, "absent") &&
		validWorkspaceApplicationSecretCleanup(operation.ApplicationSecrets) && validWorkspaceDeleteState(operation)
}

func validWorkspaceDeleteComputeReadbackState(operation workspaceDeleteOperation) bool {
	if operation.ComputeReadbacks == 0 && operation.MaxComputeReadbacks == 0 {
		return operation.ComputeStatus == "" && operation.ComputeReadbackNotBefore == ""
	}
	if operation.MaxComputeReadbacks != workspaceDeleteComputeReadbackBudget || operation.ComputeReadbacks <= 0 || operation.ComputeStatus == "" {
		return false
	}
	if operation.ComputeStatus == "destroying" {
		_, err := time.Parse(time.RFC3339Nano, operation.ComputeReadbackNotBefore)
		return operation.Phase == "storage_absent" && err == nil
	}
	if !workspaceDeleteComputeTerminal(operation.ComputeStatus) || operation.ComputeReadbackNotBefore != "" {
		return false
	}
	switch operation.Phase {
	case "compute_absent", "key_absent", "workspace_absent", "deletion_receipt_recorded", "complete":
		return true
	default:
		return false
	}
}

func workspaceDeletePhaseRank(phase string) (int, bool) {
	if phase == "key_absent" {
		return 4, true // Retained operations already confirmed Key absence under the old policy.
	}
	phases := []string{"claimed", "runtime_secret_absent", "attachment_absent", "storage_absent", "compute_absent", "workspace_absent", "deletion_receipt_recorded", "complete"}
	for rank, candidate := range phases {
		if phase == candidate {
			return rank, true
		}
	}
	return 0, false
}

func validWorkspaceDeleteState(operation workspaceDeleteOperation) bool {
	rank, ok := workspaceDeletePhaseRank(operation.Phase)
	if !ok || !validWorkspaceDeleteComputeReadbackState(operation) {
		return false
	}
	switch operation.Status {
	case "running":
		if operation.Phase == "complete" || operation.LastErrorCode != "" {
			return false
		}
	case "manual_review":
		if operation.Phase == "complete" || operation.LastErrorCode == "" {
			return false
		}
	case "succeeded":
		if operation.Phase != "complete" || operation.LastErrorCode != "" {
			return false
		}
	default:
		return false
	}

	runtimeAbsent := operation.RuntimeStatus == "absent" && operation.SecretStatus == "absent"
	if operation.ApplicationCleanup != nil && rank >= 1 && !workspaceApplicationLifecycleComplete(operation.ApplicationCleanup, "absent") {
		return false
	}
	if operation.CurrentApplicationDeploymentID != "" && rank >= 1 && operation.ApplicationCleanup == nil {
		return false
	}
	if rank >= 2 && (operation.ApplicationSecrets != nil && !workspaceApplicationSecretCleanupComplete(operation.ApplicationSecrets) || operation.CurrentApplicationDeploymentID != "" && operation.ApplicationSecrets == nil) {
		return false
	}
	attachmentAbsent := operation.AttachmentStatus == "absent"
	storageAbsent := operation.StorageStatus == "absent"
	computeAbsent := operation.ComputeStatus == "absent" && operation.ComputeReadbacks > 0 && operation.MaxComputeReadbacks == workspaceDeleteComputeReadbackBudget && operation.ComputeReadbackNotBefore == ""
	if rank < 1 && (operation.RuntimeStatus != "" || operation.SecretStatus != "") || rank >= 1 && !runtimeAbsent ||
		rank < 2 && operation.AttachmentStatus != "" || rank >= 2 && !attachmentAbsent ||
		rank < 3 && operation.StorageStatus != "" || rank >= 3 && !storageAbsent ||
		rank < 4 && operation.ComputeStatus != "" && !(rank == 3 && operation.ComputeStatus == "destroying") || rank >= 4 && !computeAbsent ||
		rank < 4 && operation.KeyStatus != "" || operation.KeyStatus != "" && operation.KeyStatus != "absent" || operation.Phase == "key_absent" && operation.KeyStatus != "absent" ||
		rank < 6 && operation.DeletionReceiptID != "" || rank >= 6 && operation.DeletionReceiptID == "" {
		return false
	}
	if rank < 4 && (operation.KeyDeleteAttempted || operation.KeyDeleteReplay != (workspaceDeleteReplayAuthorization{})) {
		return false
	}
	if !operation.KeyDeleteAttempted && operation.KeyDeleteReplay != (workspaceDeleteReplayAuthorization{}) {
		return false
	}
	if rank == 3 {
		if operation.ComputeStatus == "" {
			return operation.ComputeReadbacks == 0 && operation.MaxComputeReadbacks == 0 && operation.ComputeReadbackNotBefore == ""
		}
		return operation.ComputeStatus == "destroying"
	}
	return true
}

// workspaceDeleteEvidenceExtends reports whether the desired evidence preserves
// every recorded observation. Entries keep their stage and order, a confirmed
// binding is immutable, and a waiting or failed entry may only be replaced by the
// next observation of that same stage — so a later poll can never rewrite what a
// receipt or the refund gate already relies on, nor enter a stage whose
// predecessors were never observed.
func workspaceDeleteEvidenceExtends(current, desired []contracts.WorkspaceDeleteStageEvidence) bool {
	if len(desired) < len(current) {
		return false
	}
	order := contracts.WorkspaceDeleteStageOrder()
	position := -1
	for index, entry := range desired {
		stage := slices.Index(order, entry.Stage)
		if stage <= position {
			return false
		}
		position = stage
		if index >= len(current) {
			continue
		}
		recorded := current[index]
		if recorded.Stage != entry.Stage {
			return false
		}
		if recorded.Confirmed() && recorded != entry {
			return false
		}
	}
	return true
}

func validWorkspaceDeleteTransition(current, desired workspaceDeleteOperation, mutation workspaceDeleteStoreMutation) bool {
	if !workspaceDeleteEvidenceExtends(current.StageEvidence, desired.StageEvidence) {
		return false
	}
	if current.CurrentApplicationDeploymentID != desired.CurrentApplicationDeploymentID || !workspaceApplicationLifecycleTargetsMatch(current.ApplicationCleanup, desired.ApplicationCleanup) ||
		!workspaceApplicationSecretTargetsMatch(current.ApplicationSecrets, desired.ApplicationSecrets) {
		return false
	}
	currentRank, currentOK := workspaceDeletePhaseRank(current.Phase)
	desiredRank, desiredOK := workspaceDeletePhaseRank(desired.Phase)
	if !currentOK || !desiredOK || current.Phase == "complete" || desiredRank < currentRank || desiredRank > currentRank+1 || desired.ComputeReadbacks < current.ComputeReadbacks ||
		current.Phase != desired.Phase && currentRank == desiredRank || current.KeyStatus != desired.KeyStatus ||
		current.KeyDeleteAttempted != desired.KeyDeleteAttempted || current.KeyDeleteReplay != desired.KeyDeleteReplay || current.DeletionReceiptID != "" && desired.DeletionReceiptID != current.DeletionReceiptID {
		return false
	}
	deleteTransition := (current.Phase == "compute_absent" || current.Phase == "key_absent") && desired.Phase == "workspace_absent"
	if mutation.DeleteWorkspace != deleteTransition {
		return false
	}
	requireAbsent := !mutation.DeleteWorkspace && desiredRank >= 5
	return mutation.RequireWorkspaceAbsent == requireAbsent
}

func decodeWorkspaceDeleteOperation(row map[string]any) (workspaceDeleteOperation, error) {
	var operation workspaceDeleteOperation
	if json.Unmarshal([]byte(stringValue(row["result"])), &operation) != nil {
		return workspaceDeleteOperation{}, errors.New("workspace_delete_operation_invalid")
	}
	validIdentity := validWorkspaceDeleteIdentity(operation)
	terminalConflictCandidate := operation.Phase == "complete" && validWorkspaceDeleteBaseIdentity(operation)
	if stringValue(row["action"]) != workspaceDeleteAction ||
		operation.OperationID == "" || operation.OperationID != stringValue(row["id"]) || operation.OperationID != stringValue(row["operationId"]) ||
		operation.AccountID == "" || operation.AccountID != stringValue(row["accountId"]) || operation.OwnerUserID == "" ||
		operation.WorkspaceID == "" || operation.WorkspaceID != stringValue(row["workspaceId"]) || operation.WorkspaceID != stringValue(row["resourceId"]) ||
		stringValue(row["resourceKind"]) != "workspace" || operation.Status != stringValue(row["status"]) || !validIdentity && !terminalConflictCandidate ||
		operation.RequestHash == "" || operation.RequestHash != workspaceDeleteRequestHash(operation) {
		return workspaceDeleteOperation{}, errors.New("workspace_delete_operation_invalid")
	}
	return operation, nil
}

func workspaceDeleteOperationIdentityMatches(row map[string]any, desired workspaceDeleteOperation) bool {
	current, err := decodeWorkspaceDeleteOperation(row)
	return err == nil && current.SchemaVersion == desired.SchemaVersion && current.OperationID == desired.OperationID && current.RequestHash == desired.RequestHash && current.AccountID == desired.AccountID &&
		current.OwnerUserID == desired.OwnerUserID && current.WorkspaceID == desired.WorkspaceID && current.Sub2APIUserID == desired.Sub2APIUserID &&
		current.ResourceType == desired.ResourceType && current.ResourceID == desired.ResourceID &&
		current.LaunchOperationID == desired.LaunchOperationID && current.LaunchReceiptID == desired.LaunchReceiptID && current.RuntimeID == desired.RuntimeID &&
		current.RuntimeServiceName == desired.RuntimeServiceName && current.ComputeID == desired.ComputeID && current.StorageID == desired.StorageID &&
		current.AttachmentID == desired.AttachmentID && current.WorkspaceAPIKeyID == desired.WorkspaceAPIKeyID && current.GatewaySecretRef == desired.GatewaySecretRef &&
		current.GatewayFingerprint == desired.GatewayFingerprint && current.CreatedAt == desired.CreatedAt &&
		// Evidence and identity facts are part of the operation's identity: a write
		// may bind a provider identity once and may never silently drop a recorded
		// confirmation.
		workspaceDeleteIdentityFactExtends(current.StorageProviderResourceID, desired.StorageProviderResourceID) &&
		workspaceDeleteIdentityFactExtends(current.ComputeMachineName, desired.ComputeMachineName) &&
		workspaceDeleteIdentityFactExtends(current.ComputeCVMInstanceID, desired.ComputeCVMInstanceID) &&
		workspaceDeleteEvidenceExtends(current.StageEvidence, desired.StageEvidence)
}

// workspaceDeleteIdentityFactExtends allows a provider identity to be bound once,
// and forbids rebinding it to a different value. An unbound fact may be learned;
// a bound fact is immutable for the life of the operation.
func workspaceDeleteIdentityFactExtends(current, desired string) bool {
	return current == "" || current == desired
}

func validWorkspaceDeleteStoreMutation(mutation workspaceDeleteStoreMutation) (workspaceDeleteOperation, bool) {
	desired, err := decodeWorkspaceDeleteOperation(mutation.DesiredOperation)
	if err != nil || !validWorkspaceDeleteIdentity(desired) || mutation.DeleteWorkspace && mutation.RequireWorkspaceAbsent || mutation.Create && (mutation.ExpectedResult != "" || mutation.DeleteWorkspace || mutation.RequireWorkspaceAbsent) {
		return workspaceDeleteOperation{}, false
	}
	if !mutation.Create && mutation.ExpectedResult == "" {
		return workspaceDeleteOperation{}, false
	}
	return desired, true
}

func workspaceRenewalBlocksDelete(row map[string]any) bool {
	if stringValue(row["action"]) != "workspace.renewal" {
		return false
	}
	operation, err := decodeWorkspaceRenewalOperation(row)
	return err != nil || operation.Status == "manual_review" || !terminalWorkspaceRenewal(operation)
}

func workspaceKeyRotationBlocksDelete(row map[string]any) bool {
	if stringValue(row["action"]) != "workspace.gateway_key.rotate" {
		return false
	}
	operation, err := decodeWorkspaceKeyRotation(row)
	return err != nil || stringValue(row["status"]) != "succeeded" || !workspaceKeyRotationSucceededEvidenceValid(operation)
}

func workspaceDeleteBlocksRotation(row map[string]any) bool {
	action := stringValue(row["action"])
	if action == workspaceDeleteLegacyAction {
		return true
	}
	if action != workspaceDeleteAction {
		return false
	}
	operation, err := decodeWorkspaceDeleteOperation(row)
	return err != nil || operation.Status != "succeeded" || operation.Phase != "complete"
}

func workspaceDeleteWorkspaceProjectionMatches(operation workspaceDeleteOperation, row map[string]any, requireResources bool) bool {
	if row == nil || firstNonEmpty(stringValue(row["accountId"]), stringValue(row["ownerAccountId"])) != operation.AccountID ||
		firstNonEmpty(stringValue(row["ownerUserId"]), stringValue(row["ownerId"])) != operation.OwnerUserID {
		return false
	}
	resourceOnly := operation.ProvisioningMode == string(contracts.WorkspaceProvisioningResourceOnly)
	keyID, hasKey := positiveIntegerField(row, "workspaceApiKeyId")
	if stringValue(row["currentApplicationDeploymentId"]) != operation.CurrentApplicationDeploymentID {
		return false
	}
	if operation.CurrentApplicationDeploymentID == "" && (resourceOnly && hasKey || !resourceOnly && (!hasKey || keyID != operation.WorkspaceAPIKeyID)) {
		return false
	}
	return !requireResources ||
		firstNonEmpty(stringValue(row["currentComputeAllocationId"]), stringValue(row["computeAllocationId"])) == operation.ComputeID &&
			stringValue(row["storageId"]) == operation.StorageID &&
			firstNonEmpty(stringValue(row["currentAttachmentId"]), stringValue(row["attachmentId"])) == operation.AttachmentID &&
			stringValue(row["runtimeId"]) == operation.RuntimeID
}

func workspaceDeleteResourceProjectionMatches(operation workspaceDeleteOperation, kind string, row map[string]any) bool {
	if row == nil {
		return true
	}
	if firstNonEmpty(stringValue(row["accountId"]), stringValue(row["ownerAccountId"])) != operation.AccountID || stringValue(row["workspaceId"]) != operation.WorkspaceID {
		return false
	}
	if ownerID := firstNonEmpty(stringValue(row["ownerUserId"]), stringValue(row["ownerId"])); ownerID != "" && ownerID != operation.OwnerUserID {
		return false
	}
	switch kind {
	case "compute":
		return stringValue(row["id"]) == operation.ComputeID
	case "storage":
		return stringValue(row["id"]) == operation.StorageID
	case "attachment":
		return stringValue(row["id"]) == operation.AttachmentID && stringValue(row["computeAllocationId"]) == operation.ComputeID &&
			firstNonEmpty(stringValue(row["storageId"]), stringValue(row["volumeId"])) == operation.StorageID
	default:
		return false
	}
}

func workspaceDeleteBlocksRenewal(row map[string]any) bool {
	return stringValue(row["action"]) == workspaceDeleteAction
}

func (app *controlPlaneServer) runWorkspaceDelete(ctx context.Context, service *controlplane.Service, operation workspaceDeleteOperation) (workspaceDeleteOperation, error) {
	for {
		if operation.Phase == "complete" {
			if !validWorkspaceDeleteIdentity(operation) || operation.Status != "succeeded" || operation.RuntimeStatus != "absent" || operation.SecretStatus != "absent" ||
				operation.AttachmentStatus != "absent" || operation.StorageStatus != "absent" || operation.ComputeStatus != "absent" || operation.DeletionReceiptID == "" {
				return operation, errWorkspaceDeleteTerminalConflict
			}
			_, found, err := app.tables.GetWorkspace(ctx, operation.WorkspaceID)
			if err != nil {
				return operation, errWorkspaceDeleteStateRead
			}
			if found {
				return operation, errWorkspaceDeleteTerminalConflict
			}
			return operation, nil
		}
		if !validWorkspaceDeleteIdentity(operation) {
			return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "workspace_delete_identity_mismatch")
		}
		switch operation.Phase {
		case "claimed":
			if operation.ApplicationCleanup == nil || operation.ApplicationSecrets == nil {
				workspace, found, err := app.tables.GetWorkspace(ctx, operation.WorkspaceID)
				if err != nil || !found {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "workspace_delete_inventory_unavailable")
				}
				next := operation
				if next.ApplicationCleanup == nil {
					next.ApplicationCleanup, err = app.workspaceApplicationLifecycleInventory(ctx, service, workspace, "absent", operation.OperationID)
					if err != nil {
						return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "workspace_delete_inventory_unavailable")
					}
				}
				if next.ApplicationSecrets == nil {
					next.ApplicationSecrets, err = app.workspaceApplicationSecretInventory(ctx, workspace)
					if err != nil {
						return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "workspace_delete_secret_inventory_unavailable")
					}
				}
				if err := app.persistWorkspaceDelete(ctx, operation, next, false, false); err != nil {
					return operation, err
				}
				operation = next
			}
			persistedResult := stringValue(workspaceDeleteOperationRow(operation)["result"])
			if err := app.convergeWorkspaceApplicationLifecycle(ctx, service, operation.ApplicationCleanup, func() error {
				desired := workspaceDeleteOperationRow(operation)
				if err := app.tables.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{ExpectedResult: persistedResult, DesiredOperation: desired}); err != nil {
					return err
				}
				persistedResult = stringValue(desired["result"])
				return nil
			}); err != nil {
				return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_application_cleanup_unconfirmed")
			}
			if operation.ProvisioningMode == string(contracts.WorkspaceProvisioningResourceOnly) {
				// The owned independent application groups were cleaned above; this
				// purchase created no legacy Runtime or Secret. The absence is still a
				// provider readback: Fabric reads the labelled Runtime objects back.
				residual, readErr := service.ObserveWorkspaceDeleteRuntimeResiduals(ctx, operation.WorkspaceID)
				if readErr != nil || !workspaceDeleteRuntimeResidualsAbsent(residual, operation.WorkspaceID) {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_runtime_absence_unconfirmed")
				}
				next := confirmStageEvidence(operation, contracts.WorkspaceDeleteStageRuntimeAbsent, contracts.WorkspaceDeleteEvidenceAbsent, contracts.WorkspaceDeleteEvidenceProviderReadback,
					// A resource-only purchase has no Runtime; the readback is scoped to
					// the Workspace, so the Workspace is the resource identity.
					firstNonEmpty(operation.RuntimeID, operation.WorkspaceID), operation.RuntimeServiceName, residual.ObservedAt, residual.ReadbackID, 0)
				next.Phase, next.Status, next.RuntimeStatus, next.SecretStatus, next.LastErrorCode = "runtime_secret_absent", "running", "absent", "absent", ""
				if err := app.persistWorkspaceDelete(ctx, operation, next, false, false); err != nil {
					return operation, err
				}
				operation = next
			} else {
				runtimeObservation, secretObservation, err := observeWorkspaceDeleteRuntimeAndSecret(ctx, service, operation)
				if err != nil {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_runtime_readback_unavailable")
				}
				if !workspaceDeleteRuntimeAndSecretOwned(operation, runtimeObservation, secretObservation) {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_runtime_identity_conflict")
				}
				needsDestroy := !workspaceDeleteRuntimeAndSecretAbsent(runtimeObservation, secretObservation)
				// A query must never stand in for a destroy: this counts the mutation
				// attempt separately from the readbacks that confirm its result.
				runtimeMutationAttempts := 0
				needsDestroyRead := false
				var residual clients.WorkspaceRuntimeDeleteObservation
				if !needsDestroy {
					var readErr error
					residual, readErr = service.ObserveWorkspaceDeleteRuntimeResiduals(ctx, operation.WorkspaceID)
					needsDestroyRead = true
					if readErr != nil || residual.SchemaVersion != clients.WorkspaceRuntimeDeleteObservationSchemaVersion || residual.WorkspaceID != operation.WorkspaceID ||
						!workspaceDeleteRuntimeResidualsAbsent(residual, operation.WorkspaceID) && residual.State != clients.WorkspaceRuntimeDeleteObservationPresent {
						return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_runtime_absence_unconfirmed")
					}
					needsDestroy = !workspaceDeleteRuntimeResidualsAbsent(residual, operation.WorkspaceID)
				}
				if needsDestroy {
					// A destroy error is not itself the result: the fresh readback below
					// decides whether the Runtime is gone, still converging, or conflicting.
					runtimeMutationAttempts = 1
					_, _ = service.DestroyWorkspaceRuntime(ctx, operation.AccountID, operation.WorkspaceID, workspaceDeleteStageKey(operation, "runtime"))
					runtimeObservation, secretObservation, err = observeWorkspaceDeleteRuntimeAndSecret(ctx, service, operation)
					if err != nil {
						return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_runtime_readback_unavailable")
					}
					if !workspaceDeleteRuntimeAndSecretOwned(operation, runtimeObservation, secretObservation) {
						return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_runtime_identity_conflict")
					}
					if !workspaceDeleteRuntimeAndSecretAbsent(runtimeObservation, secretObservation) {
						// Owned Runtime objects still exist. That is an ordinary
						// propagation wait on the same operation, not a conflict, and
						// must not be recorded as a terminal review state. The wait is
						// recorded from a fresh owning read, so the observation time and
						// readback reference are the provider's rather than an inferred
						// or leftover value.
						return app.recordRuntimeDeleteWait(ctx, service, operation, runtimeMutationAttempts)
					}
					var readErr error
					residual, readErr = service.ObserveWorkspaceDeleteRuntimeResiduals(ctx, operation.WorkspaceID)
					needsDestroyRead = true
					if readErr != nil {
						// No readback was obtained. Record that explicitly and keep the
						// same operation retryable instead of claiming an observation.
						return app.recordRuntimeDeleteWait(ctx, service, operation, runtimeMutationAttempts)
					}
					if !workspaceDeleteRuntimeResidualsAbsent(residual, operation.WorkspaceID) {
						waiting := markStageWaiting(operation, contracts.WorkspaceDeleteStageRuntimeAbsent, "owned_runtime_objects_present",
							firstNonEmpty(operation.RuntimeID, operation.WorkspaceID), operation.RuntimeServiceName,
							residual.ObservedAt, residual.ReadbackID, runtimeMutationAttempts)
						if err := app.persistWorkspaceDelete(ctx, operation, waiting, false, false); err != nil {
							return operation, err
						}
						return waiting, errWorkspaceDeletePending
					}
				}
				if !needsDestroyRead {
					var readErr error
					residual, readErr = service.ObserveWorkspaceDeleteRuntimeResiduals(ctx, operation.WorkspaceID)
					if readErr != nil || !workspaceDeleteRuntimeResidualsAbsent(residual, operation.WorkspaceID) {
						return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_runtime_absence_unconfirmed")
					}
				}
				next := confirmStageEvidence(operation, contracts.WorkspaceDeleteStageRuntimeAbsent, contracts.WorkspaceDeleteEvidenceAbsent, contracts.WorkspaceDeleteEvidenceProviderReadback,
					firstNonEmpty(operation.RuntimeID, operation.WorkspaceID), operation.RuntimeServiceName, residual.ObservedAt, residual.ReadbackID,
					runtimeMutationAttempts)
				next.Phase, next.Status, next.RuntimeStatus, next.SecretStatus, next.LastErrorCode = "runtime_secret_absent", "running", "absent", "absent", ""
				if err := app.persistWorkspaceDelete(ctx, operation, next, false, false); err != nil {
					return operation, err
				}
				operation = next
			}
		case "runtime_secret_absent":
			if operation.ApplicationSecrets == nil {
				workspace, found, err := app.tables.GetWorkspace(ctx, operation.WorkspaceID)
				if err != nil || !found {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "workspace_delete_secret_inventory_unavailable")
				}
				next := operation
				next.ApplicationSecrets, err = app.workspaceApplicationSecretInventory(ctx, workspace)
				if err != nil {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "workspace_delete_secret_inventory_unavailable")
				}
				if err := app.persistWorkspaceDelete(ctx, operation, next, false, false); err != nil {
					return operation, err
				}
				operation = next
			}
			persistedResult := stringValue(workspaceDeleteOperationRow(operation)["result"])
			if err := app.convergeWorkspaceApplicationSecretCleanup(ctx, service, operation, func() error {
				desired := workspaceDeleteOperationRow(operation)
				if err := app.tables.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{ExpectedResult: persistedResult, DesiredOperation: desired}); err != nil {
					return err
				}
				persistedResult = stringValue(desired["result"])
				return nil
			}); err != nil {
				return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_application_secret_cleanup_unconfirmed")
			}
			attachment, err := service.DetachWorkspaceStorage(ctx, operation.AccountID, operation.WorkspaceID, operation.AttachmentID, workspaceDeleteStageKey(operation, "attachment"))
			if err != nil || !workspaceDeleteAttachmentMatches(operation, attachment) {
				return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_attachment_unconfirmed")
			}
			// Releasing the mount binding is a local transition. The physical CBS detach
			// is proven later by the storage stage, so this confirmation never claims a
			// provider readback.
			next := confirmStageEvidence(operation, contracts.WorkspaceDeleteStageAttachmentAbsent, contracts.WorkspaceDeleteEvidenceReleased, contracts.WorkspaceDeleteEvidenceLocalTransition,
				operation.AttachmentID, "", time.Now().UTC().Format(time.RFC3339Nano), "",
				1)
			next.Phase, next.Status, next.AttachmentStatus, next.LastErrorCode = "attachment_absent", "running", "absent", ""
			if err := app.persistWorkspaceDelete(ctx, operation, next, false, false); err != nil {
				return operation, err
			}
			operation = next
		case "attachment_absent":
			storage, err := service.DestroyWorkspaceStorage(ctx, operation.AccountID, operation.WorkspaceID, operation.StorageID, workspaceDeleteStageKey(operation, "storage"))
			if err != nil {
				return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_storage_unconfirmed")
			}
			if !workspaceDeleteStorageOwned(operation, storage) {
				return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_storage_identity_conflict")
			}
			if clients.StorageVolumeDeletionPending(storage) {
				// The CBS deletion is still converging (detached-but-not-yet-
				// terminated, or an unconfirmed terminate). Retry the same operation
				// instead of recording a terminal failure; a completed deletion and
				// an unverifiable provider response never carry this classification.
				// The observation itself is persisted so the wait is explainable from
				// state, not only from a log, and so the next attempt counts as a
				// further read rather than restarting the count.
				waiting := markStageWaiting(operation, contracts.WorkspaceDeleteStageStorageAbsent, storage.DestroyState,
					operation.StorageID, firstNonEmpty(storage.ProviderResourceID, operation.StorageProviderResourceID),
					storage.ObservedAt, storage.ProviderRequestID, 1)
				if err := app.persistWorkspaceDelete(ctx, operation, waiting, false, false); err != nil {
					return operation, err
				}
				return waiting, errWorkspaceDeletePending
			}
			if !workspaceDeleteStorageMatches(operation, storage) {
				return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_storage_unconfirmed")
			}
			// A destroy response may be a retained operation result. Confirm this
			// stage only from Fabric's fresh read surface, which owns the observation
			// time and reference, before persisting an immutable confirmation.
			storage, err = service.ReadWorkspaceDeleteStorage(ctx, operation.StorageID)
			if err != nil {
				return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_storage_readback_unavailable")
			}
			if !workspaceDeleteStorageOwned(operation, storage) {
				return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_storage_identity_conflict")
			}
			observedAt, observedErr := time.Parse(time.RFC3339Nano, storage.ObservedAt)
			createdAt, createdErr := time.Parse(time.RFC3339Nano, operation.CreatedAt)
			now := time.Now().UTC()
			if observedErr != nil || createdErr != nil || strings.TrimSpace(storage.ReadbackID) == "" ||
				observedAt.Before(createdAt) || observedAt.After(now) || now.Sub(observedAt) > workspaceDeleteRefundReadbackMaxAge ||
				strings.TrimSpace(storage.ProviderResourceID) == "" || !workspaceDeleteStorageMatches(operation, storage) ||
				storage.BindingPresent == nil || *storage.BindingPresent || storage.CBSStatus != contracts.WorkspaceDeleteProviderStatusNotFound {
				return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_storage_unconfirmed")
			}
			next := confirmStageEvidence(operation, contracts.WorkspaceDeleteStageStorageAbsent, contracts.WorkspaceDeleteEvidenceAbsent, contracts.WorkspaceDeleteEvidenceProviderReadback,
				operation.StorageID, firstNonEmpty(storage.ProviderResourceID, operation.StorageProviderResourceID),
				storage.ObservedAt, storage.ReadbackID, 1)
			next.Phase, next.Status, next.StorageStatus, next.LastErrorCode = "storage_absent", "running", "absent", ""
			next.StorageProviderResourceID = firstNonEmpty(storage.ProviderResourceID, operation.StorageProviderResourceID)
			if err := app.persistWorkspaceDelete(ctx, operation, next, false, false); err != nil {
				return operation, err
			}
			operation = next
		case "storage_absent":
			var compute clients.ComputeAllocation
			var err error
			readNow := time.Now().UTC()
			if operation.ComputeStatus == "destroying" {
				notBefore, parseErr := time.Parse(time.RFC3339Nano, operation.ComputeReadbackNotBefore)
				if parseErr != nil {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_compute_readback_schedule_invalid")
				}
				if readNow.Before(notBefore) {
					return operation, errWorkspaceDeletePending
				}
				if operation.MaxComputeReadbacks != workspaceDeleteComputeReadbackBudget {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_compute_absence_unconfirmed")
				}
				claimed := operation
				claimed.ComputeReadbacks++
				claimed.ComputeReadbackNotBefore = readNow.Add(workspaceDeleteComputeReadbackInterval).Format(time.RFC3339Nano)
				if err := app.persistWorkspaceDelete(ctx, operation, claimed, false, false); err != nil {
					return operation, err
				}
				operation = claimed
				compute, err = service.WorkspaceDeleteComputeStatus(ctx, operation.ComputeID)
				if err != nil {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_compute_readback_unavailable")
				}
				if !workspaceDeleteComputeIdentityMatches(operation, compute) {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_compute_identity_conflict")
				}
			} else {
				compute, err = service.DestroyWorkspaceCompute(ctx, operation.AccountID, operation.WorkspaceID, operation.ComputeID, workspaceDeleteStageKey(operation, "compute"))
				if err != nil {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_compute_destroy_unconfirmed")
				}
				if !workspaceDeleteComputeIdentityMatches(operation, compute) {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_compute_identity_conflict")
				}
				if clients.ComputeAllocationDeletionPending(compute) {
					// Fabric classified this destroy outcome as an unfinished deletion:
					// the same operation retries instead of recording a terminal result.
					return app.armWorkspaceDeleteComputeReadback(ctx, operation, readNow)
				}
				if !workspaceDeleteComputeStartMatches(operation, compute) {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_compute_destroy_unconfirmed")
				}
				claimed := operation
				claimed.ComputeStatus, claimed.MaxComputeReadbacks, claimed.ComputeReadbacks = "destroying", workspaceDeleteComputeReadbackBudget, 1
				claimed.ComputeReadbackNotBefore = readNow.Add(workspaceDeleteComputeReadbackInterval).Format(time.RFC3339Nano)
				if err := app.persistWorkspaceDelete(ctx, operation, claimed, false, false); err != nil {
					return operation, err
				}
				operation = claimed
				compute, err = service.WorkspaceDeleteComputeStatus(ctx, operation.ComputeID)
				if err != nil {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_compute_readback_unavailable")
				}
				if !workspaceDeleteComputeIdentityMatches(operation, compute) {
					return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_compute_identity_conflict")
				}
			}
			// A classified unfinished deletion and an in-flight destroy are ordinary
			// waits on the same operation. Only an unclassified non-terminal status
			// is reviewable.
			if clients.ComputeAllocationDeletionPending(compute) || compute.Status == "destroying" {
				// This observation read the provider; the destroy that this stage is
				// waiting on was counted when it was dispatched.
				waiting := markStageWaiting(operation, contracts.WorkspaceDeleteStageComputeAbsent, compute.DestroyState,
					operation.ComputeID, firstNonEmpty(compute.MachineName, compute.CVMInstanceID, compute.InstanceID, operation.ComputeCVMInstanceID),
					compute.ObservedAt, compute.ReadbackID, 0)
				if err := app.persistWorkspaceDelete(ctx, operation, waiting, false, false); err != nil {
					return operation, err
				}
				return waiting, errWorkspaceDeletePending
			}
			if !workspaceDeleteComputeTerminal(compute.Status) {
				return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "fabric_compute_absence_unconfirmed")
			}
			next := confirmStageEvidence(operation, contracts.WorkspaceDeleteStageComputeAbsent, contracts.WorkspaceDeleteEvidenceAbsent, contracts.WorkspaceDeleteEvidenceProviderReadback,
				operation.ComputeID, firstNonEmpty(compute.CVMInstanceID, compute.InstanceID, operation.ComputeCVMInstanceID),
				compute.ObservedAt, compute.ReadbackID, 1)
			next.Phase, next.Status, next.ComputeStatus, next.ComputeReadbackNotBefore, next.LastErrorCode = "compute_absent", "running", "absent", "", ""
			next.ComputeMachineName = firstNonEmpty(compute.MachineName, next.ComputeMachineName)
			next.ComputeCVMInstanceID = firstNonEmpty(compute.CVMInstanceID, compute.InstanceID, next.ComputeCVMInstanceID)
			if err := app.persistWorkspaceDelete(ctx, operation, next, false, false); err != nil {
				return operation, err
			}
			operation = next
		case "compute_absent", "key_absent":
			// Removing the Workspace projection is committed atomically with this
			// phase advance, so it is recorded as a local transition, not a readback.
			next := confirmStageEvidence(operation, contracts.WorkspaceDeleteStageWorkspaceAbsent, contracts.WorkspaceDeleteEvidenceRemoved, contracts.WorkspaceDeleteEvidenceLocalTransition,
				operation.WorkspaceID, "", time.Now().UTC().Format(time.RFC3339Nano), "",
				0)
			next.Phase, next.Status, next.LastErrorCode = "workspace_absent", "running", ""
			if err := app.persistWorkspaceDelete(ctx, operation, next, true, false); err != nil {
				return operation, err
			}
			operation = next
		case "workspace_absent":
			if _, found, err := app.tables.GetWorkspace(ctx, operation.WorkspaceID); err != nil || found {
				return operation, errWorkspaceDeleteUnconfirmed
			}
			next, err := app.recordWorkspaceDeletionReceipt(ctx, service, operation)
			if err != nil {
				return next, err
			}
			operation = next
		case "deletion_receipt_recorded":
			next := operation
			next.Phase, next.Status, next.LastErrorCode = "complete", "succeeded", ""
			if err := app.persistWorkspaceDelete(ctx, operation, next, false, true); err != nil {
				return operation, err
			}
			operation = next
		default:
			return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "workspace_delete_phase_invalid")
		}
	}
}

// armWorkspaceDeleteComputeReadback records the first scheduled absence readback
// for a compute deletion that Fabric reported as unfinished, so the same
// operation continues instead of ending in a terminal state.
func (app *controlPlaneServer) armWorkspaceDeleteComputeReadback(ctx context.Context, operation workspaceDeleteOperation, now time.Time) (workspaceDeleteOperation, error) {
	claimed := operation
	claimed.ComputeStatus, claimed.MaxComputeReadbacks, claimed.ComputeReadbacks = "destroying", workspaceDeleteComputeReadbackBudget, 1
	claimed.ComputeReadbackNotBefore = now.Add(workspaceDeleteComputeReadbackInterval).Format(time.RFC3339Nano)
	if err := app.persistWorkspaceDelete(ctx, operation, claimed, false, false); err != nil {
		return operation, err
	}
	return claimed, errWorkspaceDeletePending
}

// recordStageObservation records the latest observation of one stage. There is at
// most one entry per stage, and entries appear in the frozen stage order.
//
// A confirmed entry is immutable: once a stage is proven, no later poll can
// replace what a receipt or the refund gate already relies on. A waiting or failed
// entry is replaced by the next observation of the same stage, so a stalled
// deletion stays explainable from persisted state instead of only from a log.
//
// An operation that predates the evidence contract keeps an empty list and
// completes on the original receipt shape, instead of being back-filled with
// observations it never made.
func recordStageObservation(operation workspaceDeleteOperation, evidence contracts.WorkspaceDeleteStageEvidence) workspaceDeleteOperation {
	index := slices.Index(contracts.WorkspaceDeleteStageOrder(), evidence.Stage)
	if index < 0 {
		return operation
	}
	next := operation
	next.StageEvidence = append([]contracts.WorkspaceDeleteStageEvidence(nil), operation.StageEvidence...)
	if recorded, present := contracts.WorkspaceDeleteStageEvidenceLatest(next.StageEvidence, evidence.Stage); present {
		if recorded.Confirmed() {
			return operation
		}
		for position := range next.StageEvidence {
			if next.StageEvidence[position].Stage == evidence.Stage {
				next.StageEvidence[position] = evidence
				return next
			}
		}
	}
	if len(next.StageEvidence) != index {
		// The stage order is only entered at its next unrecorded stage, so evidence
		// can never claim a stage whose predecessors were never observed.
		return operation
	}
	next.StageEvidence = append(next.StageEvidence, evidence)
	return next
}

// stageObservationAttempts derives the attempt counters for one observation from the
// previously recorded entry. Every observation is one more read, and the caller
// reports how many provider mutations this observation dispatched, because only it
// knows. The two counters are separate and both accumulate: a query never stands in
// for a destroy, and a destroy is never counted as a query.
func stageObservationAttempts(operation workspaceDeleteOperation, stage string, dispatchedMutations int) (int, int) {
	readAttempts, mutationAttempts := 1, dispatchedMutations
	if previous, present := contracts.WorkspaceDeleteStageEvidenceLatest(operation.StageEvidence, stage); present {
		readAttempts, mutationAttempts = previous.ReadAttempts+1, previous.MutationAttempts+dispatchedMutations
	}
	return readAttempts, mutationAttempts
}

// recordRuntimeDeleteWait records the Runtime stage's current state while owned
// objects still exist, or while no readback could be obtained.
//
// It reads the labelled Runtime objects back itself, because that read is the owning
// observation of this stage: only its own result carries the observation time and the
// readback reference the record must name. When the read does not return a usable
// readback, the wait is recorded as unavailable rather than given an invented
// observation, and the same operation stays retryable.
func (app *controlPlaneServer) recordRuntimeDeleteWait(ctx context.Context, service *controlplane.Service, operation workspaceDeleteOperation, mutationAttempts int) (workspaceDeleteOperation, error) {
	resourceID := firstNonEmpty(operation.RuntimeID, operation.WorkspaceID)
	residual, readErr := service.ObserveWorkspaceDeleteRuntimeResiduals(ctx, operation.WorkspaceID)
	usable := readErr == nil && residual.SchemaVersion == clients.WorkspaceRuntimeDeleteObservationSchemaVersion &&
		residual.WorkspaceID == operation.WorkspaceID && strings.TrimSpace(residual.ReadbackID) != ""
	reasonCode := "fabric_runtime_readback_unavailable"
	observedAt, readbackID := "", ""
	if usable {
		reasonCode, observedAt, readbackID = "owned_runtime_objects_present", residual.ObservedAt, residual.ReadbackID
	}
	waiting := markStageWaiting(operation, contracts.WorkspaceDeleteStageRuntimeAbsent, reasonCode, resourceID, operation.RuntimeServiceName, observedAt, readbackID, mutationAttempts)
	if err := app.persistWorkspaceDelete(ctx, operation, waiting, false, false); err != nil {
		return operation, err
	}
	return waiting, errWorkspaceDeletePending
}

// markStageWaiting records the current observation of a stage that has not finished,
// so the deletion can explain itself while it converges.
//
// A waiting entry is a provider observation only when the owning read actually
// returned an observation time and the readback reference it came from. When it did
// not, the entry states explicitly that no readback was obtained: a local clock is
// never substituted for a provider observation, and a provider observation is never
// recorded without the readback behind it.
func markStageWaiting(operation workspaceDeleteOperation, stage, reasonCode, resourceID, providerResourceID, observedAt, readbackID string, mutationAttempts int) workspaceDeleteOperation {
	readAttempts, mutations := stageObservationAttempts(operation, stage, mutationAttempts)
	if strings.TrimSpace(observedAt) == "" || strings.TrimSpace(readbackID) == "" {
		// The attempt time is stamped here, and the entry's kind says that this was an
		// attempt that returned no provider fact.
		return recordStageObservation(operation, contracts.WorkspaceDeleteStageEvidence{
			Stage: stage, Result: contracts.WorkspaceDeleteEvidenceWaiting, ReasonCode: reasonCode,
			EvidenceKind: contracts.WorkspaceDeleteEvidenceUnavailable, ResourceID: resourceID, ProviderResourceID: providerResourceID,
			ObservedAt: time.Now().UTC().Format(time.RFC3339Nano), ReadAttempts: readAttempts, MutationAttempts: mutations,
		})
	}
	return recordStageObservation(operation, contracts.WorkspaceDeleteStageEvidence{
		Stage: stage, Result: contracts.WorkspaceDeleteEvidenceWaiting, ReasonCode: reasonCode,
		EvidenceKind: stageEvidenceKind(stage), ResourceID: resourceID, ProviderResourceID: providerResourceID,
		ObservedAt: observedAt, ReadbackID: readbackID, ReadAttempts: readAttempts, MutationAttempts: mutations,
	})
}

// stageEvidenceKind reports the evidence kind the frozen contract requires for a
// stage.
func stageEvidenceKind(stage string) string {
	_, kind, _ := contracts.WorkspaceDeleteStageEvidenceExpected(stage)
	return kind
}

// confirmStageEvidence records one confirmed stage result. observedAt must come from
// the observer (a provider readback time or the committed local transition time),
// never from the moment this record is written, and the attempt counters are derived
// from what was already recorded rather than restated by the caller.
func confirmStageEvidence(operation workspaceDeleteOperation, stage, result, kind, resourceID, providerResourceID, observedAt, readbackID string, mutationAttempts int) workspaceDeleteOperation {
	readAttempts, mutations := stageObservationAttempts(operation, stage, mutationAttempts)
	return recordStageObservation(operation, contracts.WorkspaceDeleteStageEvidence{
		Stage: stage, Result: result, EvidenceKind: kind, ResourceID: resourceID, ProviderResourceID: providerResourceID,
		ObservedAt: observedAt, ReadbackID: readbackID, ReadAttempts: readAttempts, MutationAttempts: mutations,
	})
}

// workspaceDeleteLastReadbackAt reports the most recent observation any stage
// confirmation recorded, which is what the deletion page shows as the last
// readback time.
func workspaceDeleteLastReadbackAt(operation workspaceDeleteOperation) string {
	latest := ""
	for _, evidence := range operation.StageEvidence {
		if evidence.ObservedAt > latest {
			latest = evidence.ObservedAt
		}
	}
	return latest
}

func (app *controlPlaneServer) persistWorkspaceDelete(ctx context.Context, current, next workspaceDeleteOperation, deleteWorkspace, requireAbsent bool) error {
	return app.tables.ApplyWorkspaceDelete(ctx, workspaceDeleteStoreMutation{
		DeleteWorkspace: deleteWorkspace, RequireWorkspaceAbsent: requireAbsent,
		ExpectedResult: stringValue(workspaceDeleteOperationRow(current)["result"]), DesiredOperation: workspaceDeleteOperationRow(next),
	})
}

func (app *controlPlaneServer) markWorkspaceDeleteUnconfirmed(ctx context.Context, operation workspaceDeleteOperation, code string) (workspaceDeleteOperation, error) {
	next := operation
	next.Status, next.LastErrorCode = "manual_review", code
	requireAbsent := operation.Phase == "workspace_absent" || operation.Phase == "deletion_receipt_recorded" || operation.Phase == "complete"
	if err := app.persistWorkspaceDelete(ctx, operation, next, false, requireAbsent); err != nil {
		return operation, err
	}
	return next, errWorkspaceDeleteUnconfirmed
}

func observeWorkspaceDeleteRuntimeAndSecret(ctx context.Context, service *controlplane.Service, operation workspaceDeleteOperation) (clients.WorkspaceRuntimeObservation, clients.WorkspaceRuntimeGatewaySecretObservation, error) {
	runtime, err := service.ObserveWorkspaceDeleteRuntime(ctx, operation.WorkspaceID)
	if err != nil {
		return clients.WorkspaceRuntimeObservation{}, clients.WorkspaceRuntimeGatewaySecretObservation{}, err
	}
	secret, err := service.ObserveWorkspaceDeleteRuntimeGatewaySecret(ctx, operation.WorkspaceID)
	return runtime, secret, err
}

func workspaceDeleteRuntimeAndSecretAbsent(runtime clients.WorkspaceRuntimeObservation, secret clients.WorkspaceRuntimeGatewaySecretObservation) bool {
	return runtime.SchemaVersion == clients.WorkspaceOwnerObservationSchemaVersion && secret.SchemaVersion == clients.WorkspaceOwnerObservationSchemaVersion &&
		runtime.State == clients.WorkspaceOwnerObservationAbsent && secret.State == clients.WorkspaceOwnerObservationAbsent && runtime.Runtime == nil && secret.Binding == nil
}

func workspaceDeleteRuntimeResidualsAbsent(observation clients.WorkspaceRuntimeDeleteObservation, workspaceID string) bool {
	return observation.SchemaVersion == clients.WorkspaceRuntimeDeleteObservationSchemaVersion && observation.WorkspaceID == workspaceID &&
		observation.State == clients.WorkspaceOwnerObservationAbsent && len(observation.Residuals) == 0
}

// Runtime and its standalone Gateway Secret can disappear independently. Every
// remaining object must still match the original Delete before the owner acts.
func workspaceDeleteRuntimeAndSecretOwned(operation workspaceDeleteOperation, runtime clients.WorkspaceRuntimeObservation, secret clients.WorkspaceRuntimeGatewaySecretObservation) bool {
	if runtime.SchemaVersion != clients.WorkspaceOwnerObservationSchemaVersion || secret.SchemaVersion != clients.WorkspaceOwnerObservationSchemaVersion ||
		runtime.WorkspaceID != operation.WorkspaceID || secret.WorkspaceID != operation.WorkspaceID {
		return false
	}
	runtimeOwned := runtime.State == clients.WorkspaceOwnerObservationAbsent && runtime.Runtime == nil ||
		(runtime.State == clients.WorkspaceOwnerObservationReady || runtime.State == clients.WorkspaceOwnerObservationPending) && runtime.Runtime != nil &&
			runtime.Runtime.WorkspaceID == operation.WorkspaceID && runtime.Runtime.ID == operation.RuntimeID
	secretOwned := secret.State == clients.WorkspaceOwnerObservationAbsent && secret.Binding == nil ||
		(secret.State == clients.WorkspaceOwnerObservationReady || secret.State == clients.WorkspaceOwnerObservationPending) && secret.Binding != nil &&
			secret.Binding.WorkspaceID == operation.WorkspaceID && secret.Binding.WorkspaceAPIKeyID == operation.WorkspaceAPIKeyID &&
			secret.Binding.SecretRef == operation.GatewaySecretRef && secret.Binding.Fingerprint == operation.GatewayFingerprint
	return runtimeOwned && secretOwned
}

// workspaceDeleteRecordedReceiptEvidence returns the evidence the deletion receipt
// attests: every stage that preceded the receipt write. The receipt's own stage is
// recorded afterwards, so verification rebuilds the payload from this prefix.
func workspaceDeleteRecordedReceiptEvidence(operation workspaceDeleteOperation) []contracts.WorkspaceDeleteStageEvidence {
	order := contracts.WorkspaceDeleteStageOrder()
	if len(order) == 0 {
		return operation.StageEvidence
	}
	attested := len(order) - 1
	if len(operation.StageEvidence) < attested {
		return operation.StageEvidence
	}
	return operation.StageEvidence[:attested]
}

func workspaceDeletionReceiptInput(operation workspaceDeleteOperation) clients.ReceiptInput {
	input := clients.ReceiptInput{
		Type: "workspace.deleted.v1", Status: "completed", Surface: "control_plane", AccountID: operation.AccountID, WorkspaceID: operation.WorkspaceID,
		RequestID: operation.OperationID, InputRefs: map[string]any{"launchReceiptId": operation.LaunchReceiptID},
		Execution: map[string]any{
			"operationId": operation.OperationID, "resourceType": "workspace", "resourceId": operation.WorkspaceID,
			"computeAllocationId": operation.ComputeID, "storageId": operation.StorageID, "attachmentId": operation.AttachmentID,
			"runtimeId": operation.RuntimeID, "workspaceApiKeyId": operation.WorkspaceAPIKeyID, "workspaceKeyFingerprint": operation.GatewayFingerprint,
			"runtimeServiceName": operation.RuntimeServiceName, "gatewaySecretRef": operation.GatewaySecretRef,
		},
		OutputRefs: map[string]any{
			"runtimeStatus": "absent", "gatewaySecretStatus": "absent", "attachmentStatus": "absent", "storageStatus": "absent",
			"computeStatus": "absent", "workspaceStatus": "absent",
		},
		Owner: map[string]any{"accountId": operation.AccountID, "workspaceId": operation.WorkspaceID, "ownerUserId": operation.OwnerUserID},
	}
	// Preserve exact payloads for receipts already requested by retained operations.
	if operation.KeyStatus == "absent" {
		input.OutputRefs["workspaceKeyStatus"] = "absent"
	}
	if operation.ProvisioningMode == string(contracts.WorkspaceProvisioningResourceOnly) {
		input.Execution = (contracts.WorkspaceResourceReceiptExecution{OperationID: operation.OperationID, ResourceType: "workspace", ResourceID: operation.WorkspaceID, ComputeAllocationID: operation.ComputeID, StorageID: operation.StorageID, AttachmentID: operation.AttachmentID, ProvisioningMode: contracts.WorkspaceProvisioningResourceOnly}).Fields()
	}
	retirement := contracts.WorkspaceApplicationRetirementReceipt{CurrentDeploymentID: operation.CurrentApplicationDeploymentID}
	if operation.ApplicationCleanup != nil {
		for _, runtime := range operation.ApplicationCleanup.Runtimes {
			retirement.Runtimes = append(retirement.Runtimes, contracts.WorkspaceApplicationRuntimeRetirementReceipt{RuntimeID: runtime.Input.RuntimeID, RuntimeOperationID: runtime.Input.RuntimeOperationID, State: runtime.Result.State, ImageRetirement: runtime.Result.ImageRetirement})
		}
	}
	if operation.ApplicationSecrets != nil {
		for _, secret := range operation.ApplicationSecrets.Secrets {
			retirement.Secrets = append(retirement.Secrets, contracts.WorkspaceApplicationSecretRetirementReceipt{SecretRef: secret.SecretRef, Ownership: secret.Ownership, State: secret.State})
		}
		retirement.RetainedGatewayKeyIDs = operation.ApplicationSecrets.RetainedGatewayKeyIDs
	}
	if len(retirement.Runtimes)+len(retirement.Secrets)+len(retirement.RetainedGatewayKeyIDs) > 0 {
		input.Execution["applicationRetirement"] = retirement
	}
	if len(retirement.RetainedGatewayKeyIDs) > 0 {
		input.OutputRefs["applicationGatewayKeysStatus"] = "retained"
	}
	// The receipt carries the stage evidence that was already accepted and
	// persisted for this operation. It is built only from recorded confirmations,
	// never from the current clock or from page text.
	//
	// A retained operation that predates this contract has no evidence to attest.
	// Its receipt keeps the original payload shape: it must not be back-filled with
	// fabricated confirmations, and the missing summary is what distinguishes it.
	if len(operation.StageEvidence) > 0 {
		input.Execution["stageEvidence"] = contracts.WorkspaceDeleteStageEvidenceDigests(operation.StageEvidence)
		input.Execution["stageEvidenceSchemaVersion"] = contracts.WorkspaceDeleteReadbackSchemaVersion
	}
	return input
}

func (app *controlPlaneServer) recordWorkspaceDeletionReceipt(ctx context.Context, service *controlplane.Service, operation workspaceDeleteOperation) (workspaceDeleteOperation, error) {
	// The receipt may only be written from already persisted stage confirmations. A
	// partial evidence list would let the receipt attest less than the deletion
	// contract requires, so it is refused instead of recorded. A retained operation
	// with no evidence at all predates the contract: it completes with the original
	// receipt shape rather than inventing confirmations it never recorded.
	if len(operation.StageEvidence) > 0 && !contracts.WorkspaceDeleteReceiptEvidenceComplete(operation.StageEvidence) {
		return app.markWorkspaceDeleteUnconfirmed(ctx, operation, "workspace_delete_evidence_incomplete")
	}
	input := workspaceDeletionReceiptInput(operation)
	receipt, err := service.RecordMonthlyReceipt(ctx, input, operation.OperationID+":deletion-receipt")
	if err != nil || receipt.ReceiptID == "" || !workspaceLaunchReceiptInputMatches(receipt.ReceiptInput, input) {
		return operation, errWorkspaceDeleteUnconfirmed
	}
	// The receipt's own stage is recorded against the written receipt, so the
	// operation ends with the full sequence and the receipt attests the stages that
	// preceded it.
	next := confirmStageEvidence(operation, contracts.WorkspaceDeleteStageReceiptRecorded, contracts.WorkspaceDeleteEvidenceRecorded, contracts.WorkspaceDeleteEvidenceLedgerReceipt,
		operation.WorkspaceID, receipt.ReceiptID, receipt.CreatedAt, receipt.ReceiptID,
		0)
	next.Phase, next.Status, next.DeletionReceiptID, next.LastErrorCode = "deletion_receipt_recorded", "running", receipt.ReceiptID, ""
	if deletedAt, parseErr := time.Parse(time.RFC3339Nano, receipt.CreatedAt); parseErr == nil {
		next.DeletedAt = deletedAt.UTC().Format(time.RFC3339Nano)
	}
	if err := app.persistWorkspaceDelete(ctx, operation, next, false, true); err != nil {
		return operation, err
	}
	return next, nil
}

func workspaceDeleteAttachmentMatches(operation workspaceDeleteOperation, attachment clients.StorageAttachment) bool {
	return attachment.ID == operation.AttachmentID && attachment.WorkspaceID == operation.WorkspaceID && attachment.ComputeID == operation.ComputeID &&
		attachment.VolumeID == operation.StorageID && attachment.Status == "detached"
}

// workspaceDeleteStorageOwned binds a storage result to the exact Workspace and
// volume identity this Delete operation owns. An identity conflict is never a
// retryable wait.
func workspaceDeleteStorageOwned(operation workspaceDeleteOperation, storage clients.StorageVolume) bool {
	if storage.ID != operation.StorageID || storage.WorkspaceID != operation.WorkspaceID {
		return false
	}
	return storage.ProviderResourceID == "" || operation.StorageProviderResourceID == "" || storage.ProviderResourceID == operation.StorageProviderResourceID
}

func workspaceDeleteStorageMatches(operation workspaceDeleteOperation, storage clients.StorageVolume) bool {
	if !workspaceDeleteStorageOwned(operation, storage) {
		return false
	}
	switch storage.Status {
	case "destroyed", "external_deleted":
		return true
	default:
		return false
	}
}

func workspaceDeleteComputeIdentityMatches(operation workspaceDeleteOperation, compute clients.ComputeAllocation) bool {
	return compute.ID == operation.ComputeID && compute.WorkspaceID == operation.WorkspaceID
}

func workspaceDeleteComputeStartMatches(operation workspaceDeleteOperation, compute clients.ComputeAllocation) bool {
	return workspaceDeleteComputeIdentityMatches(operation, compute) && (compute.Status == "destroying" || workspaceDeleteComputeTerminal(compute.Status))
}

func workspaceDeleteComputeTerminal(status string) bool {
	switch status {
	case "destroyed", "external_deleted", "deleted", "missing", "absent":
		return true
	default:
		return false
	}
}

func workspaceDeleteResponse(operation workspaceDeleteOperation, errorCode string) map[string]any {
	status := "deleted"
	if errorCode != "" {
		status = "manual_review"
	}
	response := map[string]any{"workspaceId": operation.WorkspaceID, "operationId": operation.OperationID, "status": status}
	if errorCode != "" {
		response["error"] = errorCode
		return response
	}
	response["accountId"] = operation.AccountID
	response["sub2apiUserId"] = operation.Sub2APIUserID
	response["launchOperationId"] = operation.LaunchOperationID
	response["launchReceiptId"] = operation.LaunchReceiptID
	response["deletionReceiptId"] = operation.DeletionReceiptID
	response["runtimeId"] = operation.RuntimeID
	response["workspaceApiKeyId"] = operation.WorkspaceAPIKeyID
	response["runtimeStatus"] = operation.RuntimeStatus
	response["secretStatus"] = operation.SecretStatus
	if operation.KeyStatus != "" {
		response["keyStatus"] = operation.KeyStatus
	}
	return response
}

func workspaceDeletePendingResponse(operation workspaceDeleteOperation) map[string]any {
	return map[string]any{
		"workspaceId": operation.WorkspaceID, "operationId": operation.OperationID, "status": "pending", "phase": operation.Phase,
		"ownerStage": "compute", "computeStatus": operation.ComputeStatus, "computeReadbacks": operation.ComputeReadbacks, "maxComputeReadbacks": operation.MaxComputeReadbacks,
	}
}

func workspaceDeleteComputeRetryAfter(operation workspaceDeleteOperation, now time.Time) int {
	notBefore, err := time.Parse(time.RFC3339Nano, operation.ComputeReadbackNotBefore)
	if err != nil || !notBefore.After(now) {
		return 0
	}
	remaining := notBefore.Sub(now)
	seconds := int((remaining + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}
