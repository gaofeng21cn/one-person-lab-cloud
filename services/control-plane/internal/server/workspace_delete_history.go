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

type workspaceDeleteStoreMutation struct {
	Create                 bool
	DeleteWorkspace        bool
	RequireWorkspaceAbsent bool
	ExpectedResult         string
	DesiredOperation       map[string]any
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

// workspaceDeleteBlocksRenewal reports that the recorded row is a Workspace
// deletion operation. The operator Runtime detail reads the deletion progress
// from those rows, and a still-running deletion keeps the Workspace pending
// instead of reporting a missing runtime.
func workspaceDeleteBlocksRenewal(row map[string]any) bool {
	return stringValue(row["action"]) == workspaceDeleteAction
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

const (
	workspaceDeleteAction       = "workspace.delete.v2"
	workspaceDeleteLegacyAction = "workspace.delete.v1"
	// Retained v2 encoding marker; delayed absence no longer has a lifetime read limit.
	workspaceDeleteComputeReadbackBudget = 8
)

var (
	errWorkspaceDeleteCASConflict = errors.New("workspace_delete_cas_conflict")
	errWorkspaceDeleteStateRead   = errors.New("workspace_delete_state_read_failed")
)

// Historical KeyStatus and KeyDelete fields preserve earlier operations; current deletion leaves Gateway keys untouched.

// The platform refund for a deleted Workspace is a separate operation owned by
// Control Plane. It is dispatched only when Fabric's authoritative readback for
// the same original Delete operation proves that every Tencent resource is
// gone. Delete success and refund success are independent states: a refused or
// failed refund never rewrites the deletion result, and running reads never
// mutate provider resources.
const (
	workspaceDeleteRefundAction = "workspace.delete.refund.v1"

	// A refund readback must be produced by the current attempt, after the
	// platform-confirmed deletion, and never older than this window.

	workspaceDeleteRefundStatusBlocked      = "blocked"
	workspaceDeleteRefundStatusPending      = "pending"
	workspaceDeleteRefundStatusManualReview = "manual_review"
	workspaceDeleteRefundStatusSucceeded    = "succeeded"
	workspaceDeleteRefundStatusNotDue       = "not_due"

	// Wallet-side reason codes. They never describe a deletion or provider fact.
)

// errWorkspaceDeleteRefundIdentity distinguishes a provider identity conflict
// from an unavailable readback, so the two are never collapsed into one state.

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

func workspaceDeleteLegacyComputeTerminal(status string) bool {
	switch status {
	case "destroyed", "external_deleted", "deleted", "missing":
		return true
	default:
		return false
	}
}

func workspaceDeleteLegacyOperationID(workspaceID string) string {
	return "workspace-delete-" + stableID(workspaceDeleteLegacyAction, workspaceID)[:18]
}

func workspaceDeleteLegacyRequestHash(operation workspaceDeleteLegacyOperation) string {
	purchaseReceipt, _ := json.Marshal(operation.PurchaseReceipt)
	return stableID(workspaceDeleteLegacyAction, operation.AccountID, operation.OwnerUserID, operation.WorkspaceID, operation.LaunchOperationID,
		operation.RuntimeID, operation.ComputeID, operation.StorageID, operation.AttachmentID, operation.GatewaySecretRef, operation.GatewayFingerprint,
		operation.DebitCode, operation.PurchaseReceiptID, string(purchaseReceipt), operation.RefundCode,
		strconv.FormatInt(operation.Sub2APIUserID, 10), strconv.FormatInt(operation.WorkspaceAPIKeyID, 10), strconv.FormatInt(operation.TotalUSDMicros, 10))
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

// workspaceDeleteIdentityFactExtends allows a provider identity to be bound once,
// and forbids rebinding it to a different value. An unbound fact may be learned;
// a bound fact is immutable for the life of the operation.
func workspaceDeleteIdentityFactExtends(current, desired string) bool {
	return current == "" || current == desired
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

func workspaceDeleteOperationID(workspaceID string) string {
	return "workspace-delete-" + stableID(workspaceDeleteAction, workspaceID)[:18]
}

func (app *controlPlaneServer) workspaceDeleteRefundRecord(ctx context.Context, workspaceID string) (workspaceDeleteRefundOperation, bool, error) {
	row, found, err := app.tables.GetRuntimeOperation(ctx, workspaceDeleteRefundOperationID(workspaceDeleteOperationID(workspaceID)))
	if err != nil || !found {
		return workspaceDeleteRefundOperation{}, found, err
	}
	operation, err := decodeWorkspaceDeleteRefund(row)
	return operation, err == nil, err
}

func workspaceDeleteStageKey(operation workspaceDeleteOperation, stage string) string {
	return operation.OperationID + ":" + stage
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

func decodeWorkspaceDeleteRefund(row map[string]any) (workspaceDeleteRefundOperation, error) {
	var operation workspaceDeleteRefundOperation
	if stringValue(row["action"]) != workspaceDeleteRefundAction || json.Unmarshal([]byte(stringValue(row["result"])), &operation) != nil ||
		operation.AccountID == "" || operation.WorkspaceID == "" || stringValue(row["accountId"]) != operation.AccountID ||
		operation.DeleteOperationID == "" || operation.PolicyVersion != contracts.WorkspaceRefundPolicyVersion ||
		operation.RefundUSDMicros < 0 || operation.OriginalChargeUSDMicros < 0 ||
		(operation.Status != workspaceDeleteRefundStatusBlocked && operation.Status != workspaceDeleteRefundStatusPending &&
			operation.Status != workspaceDeleteRefundStatusManualReview && operation.Status != workspaceDeleteRefundStatusSucceeded &&
			operation.Status != workspaceDeleteRefundStatusNotDue) {
		return workspaceDeleteRefundOperation{}, errWorkspaceDeleteStateRead
	}
	return operation, nil
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

type workspaceDeleteRefundOperation struct {
	SchemaVersion               int    `json:"schemaVersion"`
	AccountID                   string `json:"accountId"`
	WorkspaceID                 string `json:"workspaceId"`
	DeleteOperationID           string `json:"deleteOperationId"`
	LaunchOperationID           string `json:"launchOperationId"`
	PolicyVersion               string `json:"policyVersion"`
	Status                      string `json:"status"`
	ReasonCode                  string `json:"reasonCode,omitempty"`
	OriginalChargeCode          string `json:"originalChargeCode,omitempty"`
	OriginalChargeUSDMicros     int64  `json:"originalChargeUsdMicros,omitempty"`
	RefundUSDMicros             int64  `json:"refundUsdMicros,omitempty"`
	UsedHours                   int64  `json:"usedHours,omitempty"`
	RefundHours                 int64  `json:"refundHours,omitempty"`
	WalletAdjustmentOperationID string `json:"walletAdjustmentOperationId,omitempty"`
	// RefundOrderOperationID identifies the confirmed order this refund is reserved
	// against: the renewal when one paid for the period in use, otherwise the
	// original purchase.
	RefundOrderOperationID string `json:"refundOrderOperationId,omitempty"`
	RefundReceiptID        string `json:"refundReceiptId,omitempty"`
	DeletedAt              string `json:"deletedAt,omitempty"`
	ResourceFulfilledAt    string `json:"resourceFulfilledAt,omitempty"`
	ReadbackID             string `json:"readbackId,omitempty"`
	ReadbackProvider       string `json:"readbackProvider,omitempty"`
	ReadbackObservedAt     string `json:"readbackObservedAt,omitempty"`
	CreatedAt              string `json:"createdAt"`
	UpdatedAt              string `json:"updatedAt"`
}

func workspaceDeleteRefundOperationID(deleteOperationID string) string {
	return "workspace-delete-refund-" + stableID(deleteOperationID)[:24]
}

func workspaceDeleteComputeTerminal(status string) bool {
	switch status {
	case "destroyed", "external_deleted", "deleted", "missing", "absent":
		return true
	default:
		return false
	}
}

func (app *controlPlaneServer) workspaceDeleteOperation(ctx context.Context, workspaceID string) (workspaceDeleteOperation, bool, error) {
	row, found, err := app.tables.GetRuntimeOperation(ctx, workspaceDeleteOperationID(workspaceID))
	if err != nil || !found {
		return workspaceDeleteOperation{}, found, err
	}
	operation, err := decodeWorkspaceDeleteOperation(row)
	return operation, err == nil, err
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

func workspaceRefundBilledHours(resourceFulfilledAt, workspaceDeletedAt time.Time) (int64, int64) {
	used := workspaceDeletedAt.Sub(resourceFulfilledAt)
	usedHours := int64(used / contracts.WorkspaceRefundHourDuration)
	if used%contracts.WorkspaceRefundHourDuration != 0 {
		usedHours++
	}
	refundHours := int64(contracts.WorkspaceRefundMonthlyHours) - usedHours
	if refundHours < 0 {
		refundHours = 0
	}
	return usedHours, refundHours
}
