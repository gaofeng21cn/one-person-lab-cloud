package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// Closeout is part of the original Launch. Its CAS stops every normal continuation;
// the physical owners fence and confirm their own writes before money is returned.
type workspaceLaunchCloseout struct {
	AuthorizationID    string `json:"authorizationId"`
	LaunchVersion      int    `json:"launchVersion"`
	AuthorizedBy       string `json:"authorizedBy"`
	AuthorizedAt       string `json:"authorizedAt"`
	Reason             string `json:"reason"`
	Phase              string `json:"phase"`
	FrozenAt           string `json:"frozenAt,omitempty"`
	KeyRevokedAt       string `json:"keyRevokedAt,omitempty"`
	ResourcesAbsentAt  string `json:"resourcesAbsentAt,omitempty"`
	DebitState         string `json:"debitState,omitempty"`
	RefundOperationID  string `json:"refundOperationId,omitempty"`
	RefundedUSDMicros  int64  `json:"refundedUsdMicros"`
	ReceiptRequestedAt string `json:"receiptRequestedAt,omitempty"`
	ReceiptID          string `json:"receiptId,omitempty"`
	CompletedAt        string `json:"completedAt,omitempty"`
	ErrorCode          string `json:"errorCode,omitempty"`
}

type workspaceLaunchCloseoutDTO struct {
	PendingConfirmation bool   `json:"pendingConfirmation,omitempty"`
	Status              string `json:"status"`
	RefundedUSDMicros   int64  `json:"refundedUsdMicros"`
	ReceiptID           string `json:"receiptId,omitempty"`
}

func workspaceLaunchCloseoutResponse(operation workspaceLaunchReconcileOperation) *workspaceLaunchCloseoutDTO {
	c := operation.Closeout
	if c == nil {
		return nil
	}
	status := "closing"
	switch c.Phase {
	case "complete":
		status = "closed"
	case "fulfilled":
		status = "fulfilled"
	case "freeze":
		status = "confirming"
	case "refund":
		status = "refunding"
	case "receipt":
		status = "recording"
	}
	return &workspaceLaunchCloseoutDTO{c.ErrorCode != "", status, c.RefundedUSDMicros, c.ReceiptID}
}

func workspaceLaunchCloseoutActive(operation workspaceLaunchReconcileOperation) bool {
	return operation.Closeout != nil && operation.Closeout.Phase != "fulfilled"
}

func validWorkspaceLaunchCloseout(operation workspaceLaunchReconcileOperation) bool {
	c := operation.Closeout
	if c == nil {
		return true
	}
	if c.AuthorizationID == "" || c.AuthorizedBy == "" || c.Reason == "" || c.Reason != strings.TrimSpace(c.Reason) || c.LaunchVersion < 1 || c.LaunchVersion >= operation.Version || operation.RuntimeRepair != nil || operation.DisposableReset != nil || c.RefundedUSDMicros < 0 || c.RefundedUSDMicros > operation.int64Fact("totalChargeUsdMicros") {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, c.AuthorizedAt); err != nil {
		return false
	}
	for _, stamp := range []string{c.FrozenAt, c.KeyRevokedAt, c.ResourcesAbsentAt, c.ReceiptRequestedAt, c.CompletedAt} {
		if stamp != "" {
			if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
				return false
			}
		}
	}
	if c.DebitState != "" && c.DebitState != "absent" && c.DebitState != "confirmed" {
		return false
	}
	if c.DebitState == "confirmed" {
		var charge contracts.WorkspaceLaunchCloseoutCharge
		if json.Unmarshal(operation.raw["chargeConfirmation"], &charge) != nil || charge.Status != "used" || charge.UserID != operation.int64Fact("sub2apiUserId") || charge.Code != operation.stringFact("sub2apiRedeemCode") || charge.ChargeUSDMicros != operation.int64Fact("totalChargeUsdMicros") || charge.ChargeUSDMicros <= 0 || !operation.boolFact("chargeAttempted") {
			return false
		}
		if _, err := time.Parse(time.RFC3339Nano, operation.stringFact("periodStart")); err != nil {
			return false
		}
		if _, err := time.Parse(time.RFC3339Nano, operation.stringFact("paidThrough")); err != nil {
			return false
		}
	}
	if c.DebitState == "absent" && (operation.boolFact("chargeAttempted") || (operation.raw["resourceBillingEnabled"] == nil || operation.boolFact("resourceBillingEnabled")) && operation.Attempts[contracts.StageDebit].Attempted > 0) {
		return false
	}
	if c.DebitState == "absent" && (c.RefundedUSDMicros != 0 || c.RefundOperationID != "") {
		return false
	}
	if c.KeyRevokedAt != "" && c.FrozenAt == "" || c.ResourcesAbsentAt != "" && c.KeyRevokedAt == "" || c.RefundedUSDMicros > 0 && (c.ResourcesAbsentAt == "" || c.DebitState != "confirmed") {
		return false
	}
	switch c.Phase {
	case "fulfilled":
		return c.FrozenAt == "" && c.KeyRevokedAt == "" && c.ResourcesAbsentAt == "" && c.ReceiptID == "" && c.CompletedAt == "" && c.RefundedUSDMicros == 0 && c.RefundOperationID == ""
	case "freeze":
		return operation.Status == contracts.StatusPending && c.FrozenAt == "" && c.KeyRevokedAt == "" && c.ResourcesAbsentAt == "" && c.ReceiptID == "" && c.CompletedAt == ""
	case "key":
		return operation.Status == contracts.StatusPending && c.FrozenAt != "" && c.KeyRevokedAt == "" && c.ResourcesAbsentAt == "" && c.ReceiptID == "" && c.CompletedAt == ""
	case "resources":
		return operation.Status == contracts.StatusPending && c.KeyRevokedAt != "" && c.ResourcesAbsentAt == "" && c.ReceiptID == "" && c.CompletedAt == ""
	case "refund", "receipt", "complete":
		if c.ResourcesAbsentAt == "" || c.DebitState == "" {
			return false
		}
		if c.Phase == "refund" {
			return operation.Status == contracts.StatusPending && c.ReceiptRequestedAt == "" && c.ReceiptID == "" && c.CompletedAt == ""
		}
		if c.DebitState == "confirmed" && c.RefundedUSDMicros != operation.int64Fact("totalChargeUsdMicros") || c.ReceiptRequestedAt == "" {
			return false
		}
		if c.Phase == "receipt" {
			return operation.Status == contracts.StatusPending && c.ReceiptID == "" && c.CompletedAt == ""
		}
		return c.ReceiptID != "" && c.CompletedAt == c.ReceiptRequestedAt && (c.DebitState == "confirmed" && operation.Status == contracts.StatusRefunded || c.DebitState == "absent" && operation.Status == contracts.StatusFailed)
	default:
		return false
	}
}

func workspaceLaunchCloseoutTransitionMatches(previous, next *workspaceLaunchCloseout) bool {
	if previous == nil {
		return true
	}
	if next == nil || previous.AuthorizationID != next.AuthorizationID || previous.LaunchVersion != next.LaunchVersion || previous.AuthorizedBy != next.AuthorizedBy || previous.AuthorizedAt != next.AuthorizedAt || previous.Reason != next.Reason || next.RefundedUSDMicros < previous.RefundedUSDMicros {
		return false
	}
	for _, values := range [][2]string{{previous.FrozenAt, next.FrozenAt}, {previous.KeyRevokedAt, next.KeyRevokedAt}, {previous.ResourcesAbsentAt, next.ResourcesAbsentAt}, {previous.DebitState, next.DebitState}, {previous.ReceiptRequestedAt, next.ReceiptRequestedAt}, {previous.ReceiptID, next.ReceiptID}, {previous.CompletedAt, next.CompletedAt}} {
		if values[0] != "" && values[0] != values[1] {
			return false
		}
	}
	if previous.Phase == "fulfilled" {
		return next.Phase == "fulfilled"
	}
	if previous.Phase == "freeze" && next.Phase == "fulfilled" {
		return true
	}
	phases := map[string]int{"freeze": 0, "key": 1, "resources": 2, "refund": 3, "receipt": 4, "complete": 5}
	before, oldOK := phases[previous.Phase]
	after, newOK := phases[next.Phase]
	return oldOK && newOK && (after == before || after == before+1)
}

func workspaceLaunchCloseoutEligible(operation workspaceLaunchReconcileOperation, now time.Time) bool {
	if operation.Closeout != nil || operation.Status != contracts.StatusManualReview || operation.RuntimeRepair != nil || operation.DisposableReset != nil || operation.Stage == contracts.StageActivation || operation.Stage == contracts.StageReceipt || operation.Stage == contracts.StageSucceeded || operation.stringFact("workspaceActivatedAt") != "" || operation.boolFact("runtimeReady") || operation.Observations[contracts.StageRuntime].State == workspaceLaunchStageReady || operation.ResumeAuthorization != nil && operation.ResumeAuthorizationConsumedAt == "" {
		return false
	}
	for _, attempt := range operation.Attempts {
		if attempt.DispatchLeaseExpiresAt != "" {
			deadline, err := time.Parse(time.RFC3339Nano, attempt.DispatchLeaseExpiresAt)
			if err != nil || deadline.After(now) {
				return false
			}
		}
	}
	return true
}

func workspaceLaunchCloseoutFabricInput(operation workspaceLaunchReconcileOperation) contracts.WorkspaceLaunchCloseoutInput {
	return contracts.WorkspaceLaunchCloseoutInput{SchemaVersion: 1, LaunchOperationID: operation.ID, AccountID: operation.stringFact("accountId"), WorkspaceID: operation.stringFact("workspaceId"), ProviderProfileRef: operation.stringFact("providerProfileRef"), ProviderBindingRef: operation.stringFact("preflightBindingRef"), SpecDigest: operation.stringFact("specDigest"), IdempotencyKey: operation.ID + ":closeout"}
}

func workspaceLaunchCloseoutFabricMatches(input contracts.WorkspaceLaunchCloseoutInput, result contracts.WorkspaceLaunchCloseoutResult) bool {
	return result.SchemaVersion == 1 && result.Binding == input
}

func (app *controlPlaneServer) workspaceLaunchRecovery(ctx context.Context, service *controlplane.Service, operation workspaceLaunchReconcileOperation) workspaceLaunchRecoveryDTO {
	result := workspaceLaunchRecoveryResponse(operation)
	if !workspaceLaunchCloseoutEligible(operation, time.Now()) {
		return result
	}
	input := workspaceLaunchCloseoutFabricInput(operation)
	observed, err := service.ReadWorkspaceLaunchCloseout(ctx, input)
	if err == nil && workspaceLaunchCloseoutFabricMatches(input, observed) && !observed.Frozen && (observed.State == "eligible" || observed.State == "absent") {
		result.AllowedActions = append(result.AllowedActions, "close_unfulfilled")
	}
	return result
}

func (app *controlPlaneServer) closeWorkspaceLaunch(ctx context.Context, service *controlplane.Service, operationID, authorizationID, actor, reason string, version int) (workspaceLaunchReconcileOperation, error) {
	row, found, err := app.tables.GetRuntimeOperation(ctx, operationID)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	if !found {
		return workspaceLaunchReconcileOperation{}, errBillingReviewNotFound
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	if operation.Closeout != nil {
		c := operation.Closeout
		if c.AuthorizationID != authorizationID || c.LaunchVersion != version || c.AuthorizedBy != actor || c.Reason != reason {
			return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
		}
		return operation, nil
	}
	if version != operation.Version || !workspaceLaunchCloseoutEligible(operation, time.Now()) {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	if _, _, exists := operation.resultCheckByID(authorizationID); exists {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	if _, _, exists := operation.resumeAuthorizationByID(authorizationID); exists {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	input := workspaceLaunchCloseoutFabricInput(operation)
	preview, err := service.ReadWorkspaceLaunchCloseout(ctx, input)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	if !workspaceLaunchCloseoutFabricMatches(input, preview) || preview.Frozen || preview.State != "eligible" && preview.State != "absent" {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	operation.Closeout = &workspaceLaunchCloseout{AuthorizationID: authorizationID, LaunchVersion: version, AuthorizedBy: actor, AuthorizedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: reason, Phase: "freeze"}
	operation.Status = contracts.StatusPending
	// The same original-row CAS that reserves dispatches freezes all CP continuation.
	return app.workspaceLaunchReconciler(service, clients.SessionDelegatedCredential{}, 0).persist(ctx, operation)
}

type workspaceLaunchCloseoutAdapter interface {
	ReconcileCloseout(context.Context, workspaceLaunchReconcileOperation) (workspaceLaunchReconcileOperation, error)
}

func (a *controlPlaneWorkspaceLaunchStageAdapter) ReconcileCloseout(ctx context.Context, operation workspaceLaunchReconcileOperation) (workspaceLaunchReconcileOperation, error) {
	c := operation.Closeout
	if c == nil || c.Phase == "fulfilled" || c.Phase == "complete" {
		return operation, nil
	}
	reconciler := NewWorkspaceLaunchReconciler(a.app.tables, a)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	input := workspaceLaunchCloseoutFabricInput(operation)
	var stepErr error
	switch c.Phase {
	case "freeze":
		// A possibly dispatched debit is never declared absent. The original exact-code
		// result must become known before any destructive closeout work can begin.
		debit, err := a.readWorkspaceLaunchDebit(ctx, operationWithStage(operation, contracts.StageDebit))
		if err != nil {
			stepErr = err
			break
		}
		if debit.State == workspaceLaunchStageReady {
			if operation.raw["resourceBillingEnabled"] == nil || operation.boolFact("resourceBillingEnabled") {
				if _, err := reduceWorkspaceLaunchStageObservationFor(&operation, contracts.StageDebit, debit); err != nil {
					return operation, err
				}
				c.DebitState = "confirmed"
			} else {
				c.DebitState = "absent"
			}
		} else if debit.State == workspaceLaunchStageAbsent && operation.Attempts[contracts.StageDebit].Attempted == 0 && !operation.boolFact("chargeAttempted") {
			c.DebitState = "absent"
		} else {
			stepErr = errors.New("workspace_launch_closeout_debit_unknown")
			break
		}
		frozen, err := a.service.FreezeWorkspaceLaunch(ctx, input)
		if err != nil {
			stepErr = err
			break
		}
		if !workspaceLaunchCloseoutFabricMatches(input, frozen) {
			stepErr = errors.New("workspace_launch_closeout_binding_mismatch")
			break
		}
		if frozen.State == "blocked" && !frozen.Frozen && frozen.Reason == "runtime_already_deliverable" {
			c.Phase = "fulfilled"
			c.ErrorCode = ""
			operation.Status = contracts.StatusManualReview
			observation, readErr := a.ReadStage(ctx, operation)
			if readErr == nil && observation.State == workspaceLaunchStageReady {
				return reconciler.convergeReadyObservation(ctx, operation, operation.Attempts[operation.Stage], observation)
			}
			return reconciler.persist(ctx, operation)
		}
		if !frozen.Frozen || frozen.State != "eligible" && frozen.State != "pending" && frozen.State != "absent" {
			stepErr = errors.New("workspace_launch_closeout_freeze_pending")
			break
		}
		c.FrozenAt, c.Phase = now, "key"
	case "key":
		if operation.int64Fact("workspaceApiKeyId") == 0 && operation.Attempts[contracts.StageKey].Attempted > 0 {
			// An old request may have created a Key whose ID was lost. Name absence
			// cannot prove that Key was never renamed; require a positive owner identity.
			keys, err := a.service.WorkspaceKeysForRevocation(ctx, operation.int64Fact("sub2apiUserId"), workspaceReservedKeyName(operation.stringFact("workspaceId")))
			reserved := workspaceKeysNamed(keys, workspaceReservedKeyName(operation.stringFact("workspaceId")))
			if err != nil || len(reserved) != 1 || reserved[0].ID <= 0 || reserved[0].UserID != operation.int64Fact("sub2apiUserId") {
				stepErr = errors.New("workspace_launch_closeout_key_unknown")
				break
			}
			// Revocation needs identity, not an active Key or a successful group bind.
			// Persist only the observed ID; do not fabricate successful Key-stage facts.
			operation.raw["workspaceApiKeyId"], _ = json.Marshal(reserved[0].ID)
			c.ErrorCode = ""
			return reconciler.persist(ctx, operation)
		}
		stepErr = a.service.RevokeWorkspaceKey(ctx, clients.Sub2APIWorkspaceKeyRevokeInput{UserID: operation.int64Fact("sub2apiUserId"), KeyID: operation.int64Fact("workspaceApiKeyId"), ExactName: workspaceReservedKeyName(operation.stringFact("workspaceId")), LaunchOperationID: operation.ID})
		if stepErr == nil {
			c.KeyRevokedAt, c.Phase = now, "resources"
		}
	case "resources":
		result, err := a.service.CloseoutWorkspaceLaunch(ctx, input)
		if err != nil {
			stepErr = err
			break
		}
		if !workspaceLaunchCloseoutFabricMatches(input, result) || !result.Frozen || result.State != "absent" {
			stepErr = errors.New("workspace_launch_closeout_resources_pending")
			break
		}
		// Each physical owner must be represented, including stages that were
		// never dispatched. An incomplete successful HTTP body is not absence.
		remaining := map[string]bool{"runtime": true, "secret": true, "storage": true, "ensure_compute_allocation": true, "attachment": true}
		for _, resource := range result.Resources {
			if !remaining[resource.Stage] || resource.State != "absent" {
				stepErr = errors.New("workspace_launch_closeout_resources_pending")
				break
			}
			delete(remaining, resource.Stage)
		}
		if len(remaining) != 0 {
			stepErr = errors.New("workspace_launch_closeout_resources_pending")
		}

		if stepErr == nil {
			c.ResourcesAbsentAt, c.Phase = now, "refund"
		}
	case "refund":
		if c.DebitState == "confirmed" {
			id, micros, complete, err := a.app.refundWorkspaceLaunchCloseout(ctx, a.service, operation)
			if err != nil {
				stepErr = err
				break
			}
			c.RefundOperationID, c.RefundedUSDMicros = id, micros
			if !complete {
				stepErr = errors.New("workspace_launch_closeout_refund_pending")
				break
			}
		}
		c.ReceiptRequestedAt, c.Phase = now, "receipt"
	case "receipt":
		expected := workspaceLaunchCloseoutReceiptInput(operation)
		receipt, found, err := workspaceLaunchPurchaseReceiptFromLedger(ctx, a, []clients.ReceiptInput{expected})
		if err != nil {
			stepErr = err
			break
		}
		if !found {
			_, err = a.service.RecordMonthlyReceipt(ctx, expected, operation.ID+":closeout:ledger")
			if err != nil {
				stepErr = err
				break
			}
			receipt, found, err = workspaceLaunchPurchaseReceiptFromLedger(ctx, a, []clients.ReceiptInput{expected})
		}
		if err != nil || !found {
			stepErr = errors.New("workspace_launch_closeout_receipt_pending")
			break
		}
		c.ReceiptID, c.CompletedAt, c.Phase = receipt.ReceiptID, c.ReceiptRequestedAt, "complete"
		operation.Status = contracts.StatusFailed
		if c.DebitState == "confirmed" {
			operation.Status = contracts.StatusRefunded
		}
	default:
		return operation, errInvalidWorkspaceLaunchOperation
	}
	c.ErrorCode = ""
	if stepErr != nil {
		// Persist a stable customer-safe reason, never raw provider data or credentials.
		c.ErrorCode = "workspace_launch_closeout_" + c.Phase + "_pending"
	}
	if c.ErrorCode != "" {
		previous, err := decodeWorkspaceLaunchReconcileOperation(map[string]any{"id": operation.ID, "action": workspaceLaunchAction, "status": string(operation.Status), "createdAt": operation.CreatedAt, "result": operation.PersistedResult})
		if err == nil && previous.Closeout != nil && *previous.Closeout == *c {
			return operation, nil
		}
	}
	return reconciler.persist(ctx, operation)
}

func reduceWorkspaceLaunchStageObservationFor(operation *workspaceLaunchReconcileOperation, stage contracts.Stage, observation workspaceLaunchStageObservation) (workspaceLaunchStageObservation, error) {
	previous := operation.Stage
	operation.Stage = stage
	result, err := reduceWorkspaceLaunchStageObservation(operation, observation)
	operation.Stage = previous
	return result, err
}

func workspaceLaunchCloseoutReceiptInput(operation workspaceLaunchReconcileOperation) clients.ReceiptInput {
	c := operation.Closeout
	var charge *contracts.WorkspaceLaunchCloseoutCharge
	outcome := "failed"
	cost := contracts.WorkspaceLaunchCloseoutReceiptCost{Currency: pricingCurrency, PriceVersion: operation.stringFact("priceVersion")}
	if c.DebitState == "confirmed" {
		_ = json.Unmarshal(operation.raw["chargeConfirmation"], &charge)
		outcome = "refunded"
		cost.ChargeUSDMicros = operation.int64Fact("totalChargeUsdMicros")
		cost.PeriodStart, cost.PaidThrough = operation.stringFact("periodStart"), operation.stringFact("paidThrough")
	}
	execution := contracts.WorkspaceLaunchCloseoutReceiptExecution{OperationID: operation.ID, AuthorizationID: c.AuthorizationID, AuthorizedAt: c.AuthorizedAt, Reason: c.Reason, Outcome: outcome, ChargeConfirmation: charge, RefundedUSDMicros: c.RefundedUSDMicros, RefundOperationID: c.RefundOperationID, FrozenAt: c.FrozenAt, KeyRevokedAt: c.KeyRevokedAt, ResourcesAbsentAt: c.ResourcesAbsentAt, CompletedAt: c.ReceiptRequestedAt}
	executionJSON, _ := json.Marshal(execution)
	costJSON, _ := json.Marshal(cost)
	input := clients.ReceiptInput{Type: string(contracts.ReceiptTypeWorkspaceClosed), Status: "completed", Surface: "control_plane", AccountID: operation.stringFact("accountId"), WorkspaceID: operation.stringFact("workspaceId"), RequestID: operation.ID, Actor: map[string]any{"userId": c.AuthorizedBy}}
	executionDecoder := json.NewDecoder(bytes.NewReader(executionJSON))
	executionDecoder.UseNumber()
	_ = executionDecoder.Decode(&input.Execution)
	costDecoder := json.NewDecoder(bytes.NewReader(costJSON))
	costDecoder.UseNumber()
	_ = costDecoder.Decode(&input.Cost)
	return input
}
