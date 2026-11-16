package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/domain"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

func workspaceLaunchAccessProjectionMismatchFields(operation workspaceLaunchReconcileOperation, workspace map[string]any) []string {
	mismatches := workspaceLaunchStableProjectionMismatchFields(operation, workspace)
	// A resource-only Launch provisions resources only, so it owns no Gateway
	// key: the default-application install that follows binds the key it created
	// onto the same Workspace row. The key is that install's fact, not the
	// Launch's, so comparing it here would refuse the very application the
	// Workspace publishes once the key exists. Full Launches keep the check.
	if operation.provisioningMode() != contracts.WorkspaceProvisioningResourceOnly &&
		int64(numberField(workspace, "workspaceApiKeyId", 0)) != operation.int64Fact("workspaceApiKeyId") {
		mismatches = append(mismatches, "workspace_api_key_id")
	}
	return mismatches
}

func workspaceLaunchHistoricalChargedReceiptInput(current clients.ReceiptInput) clients.ReceiptInput {
	historical := current
	historical.Execution = map[string]any{
		"resourceType": current.Execution["resourceType"], "resourceId": current.Execution["resourceId"],
		"computeAllocationId": current.Execution["computeAllocationId"], "storageId": current.Execution["storageId"],
		"attachmentId": current.Execution["attachmentId"], "runtimeId": current.Execution["runtimeId"],
		"workspaceApiKeyId": current.Execution["workspaceApiKeyId"], "workspaceKeyFingerprint": current.Execution["workspaceKeyFingerprint"],
		"runtimeServiceName": current.Execution["runtimeServiceName"],
	}
	return historical
}

func workspaceLaunchPurchaseReceiptInput(operation workspaceLaunchReconcileOperation) (clients.ReceiptInput, error) {
	execution := workspaceLaunchCanonicalReceiptExecution(operation)
	owner := map[string]any{"accountId": operation.stringFact("accountId"), "workspaceId": operation.stringFact("workspaceId"), "ownerUserId": operation.stringFact("ownerUserId")}
	computePrice, storagePrice, err := workspaceLaunchAcceptedPriceComponents(operation)
	if err != nil {
		return clients.ReceiptInput{}, err
	}
	if operation.raw["resourceBillingEnabled"] != nil && !operation.boolFact("resourceBillingEnabled") {
		return clients.ReceiptInput{Type: "workspace.created", Status: "completed", Surface: "control_plane", AccountID: operation.stringFact("accountId"), WorkspaceID: operation.stringFact("workspaceId"), RequestID: operation.ID, Execution: execution, Owner: owner}, nil
	}
	input := clients.ReceiptInput{
		Type: "billing.workspace_purchased.v1", Status: "completed", Surface: "control_plane", AccountID: operation.stringFact("accountId"),
		WorkspaceID: operation.stringFact("workspaceId"), RequestID: operation.ID,
		Execution: execution,
		Cost: map[string]any{"priceVersion": operation.stringFact("priceVersion"), "currency": pricingCurrency, "billingUnit": pricingBillingUnit,
			"totalUsdMicros": operation.int64Fact("totalChargeUsdMicros"), "sub2apiUserId": operation.int64Fact("sub2apiUserId"),
			"sub2apiRedeemCode": operation.stringFact("sub2apiRedeemCode"),
			"periodStart":       operation.stringFact("periodStart"), "paidThrough": operation.stringFact("paidThrough"), "resourceType": "workspace", "resourceId": operation.stringFact("workspaceId"),
			"components": map[string]any{"compute": map[string]any{"resourceType": "compute", "resourceId": operation.stringFact("computeAllocationId"), "chargeUsdMicros": computePrice},
				"storage": map[string]any{"resourceType": "storage", "resourceId": operation.stringFact("storageId"), "sizeGb": int64(operation.intFact("sizeGb")), "chargeUsdMicros": storagePrice}}},
		Owner: owner,
	}
	if operation.boolFact("postChargeBalanceKnown") {
		input.Cost["postChargeBalanceUsdMicros"] = operation.int64Fact("postChargeBalanceUsdMicros")
	}
	return input, nil
}

// Closeout is part of the original Launch. Its CAS stops every normal continuation;
// the physical owners fence and confirm their own writes before money is returned.
// KeyRevokedAt is retained only for exact historical receipt replay; new closeouts leave Gateway keys untouched.
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

// The recovery read reports the accepted operation's own state. The recovery
// command retired with the retained CP launch execution, so the read no longer
// contacts the provider to decide whether an operator may act: there is no
// action left to offer.
func (app *controlPlaneServer) workspaceLaunchRecovery(operation workspaceLaunchReconcileOperation) workspaceLaunchRecoveryDTO {
	return workspaceLaunchRecoveryResponse(operation)
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

const workspaceLaunchReconcileSchemaVersion = 3

var workspaceLaunchReconcileStages = contracts.AllLaunchStages()

var workspaceLaunchReconcileForbiddenFields = []string{
	"phase",
	"currentDecision",
	"recoveryPlan",
	"recoveryExecution",
	"recoveryHistory",
	"readbackRecoveryApproval",
	"readbackRecoveryProof",
	"computeClaimApproval",
	"computeClaimProof",
	"computeClaimTerminalEvidence",
	"recoveryAcceptanceApprovalDigest",
	"operatorGrants",
	"currentGrantId",
	"proof",
	"artifact",
	"operatorGrant",
}

func (err workspaceLaunchDecodeError) Error() string {
	return errInvalidWorkspaceLaunchOperation.Error()
}

func (err workspaceLaunchDecodeError) Unwrap() error {
	return errInvalidWorkspaceLaunchOperation
}

func workspaceLaunchDecodeFailureCategory(err error) string {
	var decodeErr workspaceLaunchDecodeError
	if errors.As(err, &decodeErr) {
		return decodeErr.category
	}
	return "unknown_decode_failure"
}

func workspaceLaunchDecodeFailedFields(err error) []string {
	var decodeErr workspaceLaunchDecodeError
	if errors.As(err, &decodeErr) {
		return append([]string(nil), decodeErr.failedFields...)
	}
	return nil
}

type workspaceLaunchReconcileOperation struct {
	Closeout                        *workspaceLaunchCloseout                                          `json:"closeout,omitempty"`
	ID                              string                                                            `json:"-"`
	Status                          contracts.LaunchStatus                                            `json:"-"`
	CreatedAt                       string                                                            `json:"-"`
	PersistedResult                 string                                                            `json:"-"`
	SchemaVersion                   int                                                               `json:"schemaVersion"`
	Version                         int                                                               `json:"version"`
	Stage                           contracts.Stage                                                   `json:"stage"`
	Attempts                        map[contracts.Stage]workspaceLaunchStageAttempt                   `json:"attempts"`
	Observations                    map[contracts.Stage]workspaceLaunchStageObservation               `json:"observations,omitempty"`
	ResultChecks                    []workspaceLaunchResultCheck                                      `json:"resultChecks,omitempty"`
	ConsumedResumeAuthorizations    []workspaceLaunchConsumedResumeAuthorization                      `json:"consumedResumeAuthorizations,omitempty"`
	ResumeAuthorization             *workspaceLaunchResumeAuthorization                               `json:"resumeAuthorization,omitempty"`
	ResumeAuthorizationConsumedAt   string                                                            `json:"resumeAuthorizationConsumedAt,omitempty"`
	IdempotentReplayClaims          map[contracts.Stage]workspaceLaunchIdempotentReplayClaim          `json:"idempotentReplayClaims,omitempty"`
	FreshContinuationAuthorizations map[contracts.Stage]workspaceLaunchFreshContinuationAuthorization `json:"freshContinuationAuthorizations,omitempty"`
	ContinuationReadClaims          map[string]workspaceLaunchContinuationReadClaim                   `json:"continuationReadClaims,omitempty"`
	RuntimeRepair                   *workspaceLaunchRuntimeRepair                                     `json:"runtimeRepair,omitempty"`
	DisposableReset                 *workspaceLaunchDisposableResetEvidence                           `json:"disposableReset,omitempty"`
	raw                             map[string]json.RawMessage
}

type workspaceLaunchReconcileCAS struct {
	OperationID                string
	ExpectedOperationResult    string
	DesiredOperation           map[string]any
	WorkspaceReceiptProjection *workspaceLaunchReceiptProjection
}

func decodeWorkspaceLaunchReconcileOperation(row map[string]any) (workspaceLaunchReconcileOperation, error) {
	result := stringValue(row["result"])
	var raw map[string]json.RawMessage
	if result == "" {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("empty_result")
	}
	if json.Unmarshal([]byte(result), &raw) != nil || raw == nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_json")
	}
	operation := workspaceLaunchReconcileOperation{
		ID:     firstNonEmpty(stringValue(row["operationId"]), stringValue(row["id"])),
		Status: contracts.LaunchStatus(stringValue(row["status"])), CreatedAt: stringValue(row["createdAt"]), PersistedResult: result, raw: raw,
		Attempts: map[contracts.Stage]workspaceLaunchStageAttempt{}, Observations: map[contracts.Stage]workspaceLaunchStageObservation{}, IdempotentReplayClaims: map[contracts.Stage]workspaceLaunchIdempotentReplayClaim{},
		FreshContinuationAuthorizations: map[contracts.Stage]workspaceLaunchFreshContinuationAuthorization{}, ContinuationReadClaims: map[string]workspaceLaunchContinuationReadClaim{},
	}
	if json.Unmarshal(raw["schemaVersion"], &operation.SchemaVersion) != nil || operation.SchemaVersion != workspaceLaunchReconcileSchemaVersion {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("schema_version_mismatch")
	}
	if json.Unmarshal(raw["version"], &operation.Version) != nil || operation.Version <= 0 {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_version")
	}
	mode, modeErr := workspaceLaunchProvisioningModeFromRaw(raw)
	if modeErr != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_provisioning_mode")
	}
	plan, planErr := contracts.WorkspaceProvisioningStages(mode)
	if planErr != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_provisioning_mode")
	}
	if !workspaceLaunchModeRequiresApplicationFacts(mode) {
		for _, field := range workspaceLaunchApplicationFactFields {
			if _, exists := raw[field]; exists {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecodeFields("invalid_provisioning_mode", field)
			}
		}
	}
	if json.Unmarshal(raw["stage"], &operation.Stage) != nil || !workspaceLaunchStageInPlan(plan, operation.Stage) {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_stage")
	}
	if len(raw["attempts"]) == 0 {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("missing_attempts")
	}
	if json.Unmarshal(raw["attempts"], &operation.Attempts) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecodeFields("invalid_attempts", "attempts_object")
	}
	if len(operation.Attempts) != len(plan)-1 {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecodeFields("invalid_attempts", "attempts_cardinality")
	}
	for stage, attempt := range operation.Attempts {
		if attempt.MaxPendingReadbacks == 0 {
			attempt.MaxPendingReadbacks = workspaceLaunchLegacyV3AuthoritativeReadBudget
			operation.Attempts[stage] = attempt
		}
	}
	if value := raw["observations"]; len(value) > 0 && json.Unmarshal(value, &operation.Observations) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_observations")
	}
	if value := raw["resultChecks"]; len(value) > 0 && json.Unmarshal(value, &operation.ResultChecks) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_result_checks")
	}
	if value := raw["consumedResumeAuthorizations"]; len(value) > 0 && json.Unmarshal(value, &operation.ConsumedResumeAuthorizations) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_resume_authorization")
	}
	for stage, observation := range operation.Observations {
		allowed := workspaceLaunchStageCanonicalFacts[stage]
		if allowed == nil || observation.State != workspaceLaunchStageReady && observation.State != workspaceLaunchStageAbsent && observation.State != workspaceLaunchStagePending &&
			observation.State != workspaceLaunchStageOwnershipPending && observation.State != workspaceLaunchStageComputeDispatchPending && observation.State != workspaceLaunchStageComputePoolQueued && observation.State != workspaceLaunchStageRuntimeImageRevisionPending && observation.State != workspaceLaunchStageUnknown ||
			(observation.State == workspaceLaunchStageOwnershipPending || observation.State == workspaceLaunchStageComputeDispatchPending || observation.State == workspaceLaunchStageComputePoolQueued) && stage != contracts.StageCompute ||
			observation.State == workspaceLaunchStageRuntimeImageRevisionPending && stage != contracts.StageRuntime ||
			observation.State != workspaceLaunchStageReady && len(observation.Facts) != 0 ||
			!validWorkspaceLaunchFabricDiagnostic(string(stage), string(observation.State), observation.Diagnostic) {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_observations")
		}
		if observation.State == workspaceLaunchStageReady {
			if _, err := validateWorkspaceLaunchStageFacts(stage, observation.Facts, false); err != nil {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_observations")
			}
		}
	}
	if value := raw["resumeAuthorization"]; len(value) > 0 && json.Unmarshal(value, &operation.ResumeAuthorization) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_resume_authorization")
	}
	if value := raw["resumeAuthorizationConsumedAt"]; len(value) > 0 && json.Unmarshal(value, &operation.ResumeAuthorizationConsumedAt) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_resume_authorization")
	}
	if value := raw["runtimeRepair"]; len(value) > 0 && json.Unmarshal(value, &operation.RuntimeRepair) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_runtime_repair")
	}
	if operation.RuntimeRepair != nil && (operation.RuntimeRepair.AuthorizationID == "" || operation.RuntimeRepair.LaunchVersion <= 0 ||
		operation.RuntimeRepair.AuthorizedBy == "" || operation.RuntimeRepair.AuthorizedAt == "" || operation.RuntimeRepair.Reason == "" || operation.RuntimeRepair.ImageDigest == "") {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_runtime_repair")
	}
	if operation.RuntimeRepair != nil {
		if _, err := time.Parse(time.RFC3339Nano, operation.RuntimeRepair.AuthorizedAt); err != nil {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_runtime_repair")
		}
	}
	if value := raw["closeout"]; len(value) > 0 && json.Unmarshal(value, &operation.Closeout) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_closeout")
	}

	if value := raw["disposableReset"]; len(value) > 0 && json.Unmarshal(value, &operation.DisposableReset) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_disposable_reset")
	}
	if operation.DisposableReset != nil && !validWorkspaceLaunchDisposableResetEvidence(*operation.DisposableReset, operation.Version) {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_disposable_reset")
	}
	if value := raw["idempotentReplayClaims"]; len(value) > 0 && json.Unmarshal(value, &operation.IdempotentReplayClaims) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
	}
	if value := raw["freshContinuationAuthorizations"]; len(value) > 0 && json.Unmarshal(value, &operation.FreshContinuationAuthorizations) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
	}
	if value := raw["continuationReadClaims"]; len(value) > 0 && json.Unmarshal(value, &operation.ContinuationReadClaims) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
	}
	if operation.ResumeAuthorization != nil && !validWorkspaceLaunchResumeAuthorization(*operation.ResumeAuthorization) {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_resume_authorization")
	}
	authorizationIDs := make(map[string]struct{}, len(operation.ConsumedResumeAuthorizations)+1)
	for _, check := range operation.ResultChecks {
		a := check.Authorization
		if check.State != workspaceLaunchStageReady && check.State != workspaceLaunchStageAbsent && check.State != workspaceLaunchStagePending && check.State != workspaceLaunchStageUnknown && check.State != workspaceLaunchStageOwnershipPending && check.State != workspaceLaunchStageComputeDispatchPending && check.State != workspaceLaunchStageComputePoolQueued && check.State != workspaceLaunchStageRuntimeImageRevisionPending || !workspaceLaunchStageDiagnosticErrorPattern.MatchString(check.ErrorCode) {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_result_checks")
		}
		if !validWorkspaceLaunchResumeAuthorization(a) || !validWorkspaceLaunchResumeAuthorizationConsumedAt(check.ConsumedAt) || a.LaunchVersion >= operation.Version || a.MutationBudget != 0 || a.IdempotentReplayBudget != 0 || a.AuthoritativeReadBudget != workspaceLaunchAuthoritativeReadBudget || a.ReplacementWorkspaceImageDigest != "" {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_result_checks")
		}
		if _, duplicate := authorizationIDs[a.AuthorizationID]; duplicate {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_result_checks")
		}
		authorizationIDs[a.AuthorizationID] = struct{}{}
	}

	for _, consumed := range operation.ConsumedResumeAuthorizations {
		if !validWorkspaceLaunchResumeAuthorization(consumed.Authorization) || !validWorkspaceLaunchResumeAuthorizationConsumedAt(consumed.ConsumedAt) ||
			consumed.Authorization.LaunchVersion >= operation.Version {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_resume_authorization")
		}
		if _, duplicate := authorizationIDs[consumed.Authorization.AuthorizationID]; duplicate {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_resume_authorization")
		}
		authorizationIDs[consumed.Authorization.AuthorizationID] = struct{}{}
	}
	if operation.ResumeAuthorizationConsumedAt != "" {
		if !validWorkspaceLaunchResumeAuthorizationConsumedAt(operation.ResumeAuthorizationConsumedAt) || operation.ResumeAuthorization == nil {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_resume_authorization")
		}
	}
	if operation.ResumeAuthorization != nil && operation.ResumeAuthorization.LaunchVersion >= operation.Version {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_resume_authorization")
	}
	if operation.ResumeAuthorization != nil {
		if _, duplicate := authorizationIDs[operation.ResumeAuthorization.AuthorizationID]; duplicate {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_resume_authorization")
		}
	}
	for _, field := range workspaceLaunchReconcileForbiddenFields {
		if _, exists := raw[field]; exists {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("forbidden_legacy_fields")
		}
	}
	for stage, claim := range operation.IdempotentReplayClaims {
		authorization, consumed, found := operation.resumeAuthorizationByID(claim.AuthorizationID)
		freshAuthorization, fresh := operation.FreshContinuationAuthorizations[stage]
		if !found && fresh && freshAuthorization.AuthorizationID == claim.AuthorizationID {
			found = true
			consumed = freshAuthorization.Status != "active"
			authorization = workspaceLaunchResumeAuthorization{
				AuthorizationID: freshAuthorization.AuthorizationID, AuthorizedStage: freshAuthorization.Stage,
				MutationBudget: freshAuthorization.MutationBudget, IdempotentReplayBudget: freshAuthorization.IdempotentReplayBudget,
				AuthoritativeReadBudget: freshAuthorization.AuthoritativeReadBudget,
			}
		} else {
			fresh = false
		}
		if stage != claim.Stage || !workspaceLaunchReconcileStageValid(stage) || stage == contracts.StageSucceeded || strings.TrimSpace(claim.AuthorizationID) == "" ||
			claim.IdempotencyKey != workspaceLaunchStageIdempotencyKey(operationWithStage(operation, stage), 1) ||
			claim.Status != "claimed" && claim.Status != "waiting" && claim.Status != "succeeded" && claim.Status != "failed" ||
			!found || authorization.AuthorizedStage != stage || authorization.MutationBudget != 0 || authorization.IdempotentReplayBudget != 1 ||
			authorization.AuthoritativeReadBudget <= 0 || authorization.AuthoritativeReadBudget > workspaceLaunchMaximumPersistedReadbacks(stage) {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
		}
		if claim.Status == "claimed" {
			activeAuthorization := fresh && operation.Stage == stage && freshAuthorization.Status == "active" ||
				!fresh && operation.ResumeAuthorizationConsumedAt == "" && operation.ResumeAuthorization != nil && operation.ResumeAuthorization.AuthorizationID == claim.AuthorizationID
			if consumed || !activeAuthorization ||
				claim.CompletedAt != "" {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
			if _, err := time.Parse(time.RFC3339Nano, claim.LeaseExpiresAt); err != nil {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
		} else if claim.Status == "waiting" {
			activeAuthorization := fresh && operation.Stage == stage && freshAuthorization.Status == "active" ||
				!fresh && operation.ResumeAuthorizationConsumedAt == "" && operation.ResumeAuthorization != nil && operation.ResumeAuthorization.AuthorizationID == claim.AuthorizationID
			if consumed || !activeAuthorization ||
				claim.LeaseExpiresAt != "" || !validWorkspaceLaunchResumeAuthorizationConsumedAt(claim.CompletedAt) {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
		} else if !consumed || claim.LeaseExpiresAt != "" || !validWorkspaceLaunchResumeAuthorizationConsumedAt(claim.CompletedAt) {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
		}
	}
	freshAuthorizationsByID := make(map[string]workspaceLaunchFreshContinuationAuthorization, len(operation.FreshContinuationAuthorizations))
	for stage, authorization := range operation.FreshContinuationAuthorizations {
		attempt, exists := operation.Attempts[stage]
		boundOperation := operationWithStage(operation, stage)
		validAuthorizationContract := workspaceLaunchFreshContinuationAuthorizationContractValid(boundOperation, attempt, authorization)
		expectedReplayBudget := workspaceLaunchFreshContinuationReplayBudget(stage)
		runtimeImageRevisionContinuation := stage == contracts.StageRuntime && authorization.Status == "failed" && operation.hasRuntimeImageRevisionAuthorization()
		if !exists || stage != authorization.Stage || !workspaceLaunchReconcileStageValid(stage) || stage == contracts.StageSucceeded ||
			!validAuthorizationContract ||
			authorization.AccountID != operation.stringFact("accountId") || authorization.OperationID != operation.ID || authorization.WorkspaceID != operation.stringFact("workspaceId") ||
			authorization.IdempotencyKey != workspaceLaunchStageIdempotencyKey(boundOperation, 1) || authorization.Attempt != 1 ||
			authorization.OperationVersion <= 0 || authorization.OperationVersion > operation.Version || authorization.MutationBudget != 0 ||
			authorization.IdempotentReplayBudget != expectedReplayBudget ||
			authorization.ReadbacksAtAuthorization != 1 || attempt.Attempted != 1 || attempt.Max != 1 ||
			attempt.MaxPendingReadbacks != 1+authorization.AuthoritativeReadBudget && !runtimeImageRevisionContinuation || attempt.PendingReadbacks < authorization.ReadbacksAtAuthorization ||
			stage == contracts.StageCompute && attempt.PendingDeadlineAt == "" {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
		}
		if _, duplicate := freshAuthorizationsByID[authorization.AuthorizationID]; duplicate {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
		}
		freshAuthorizationsByID[authorization.AuthorizationID] = authorization
		switch authorization.Status {
		case "active":
			if authorization.ConsumedAt != "" || operation.Stage != stage || operation.Status != contracts.StatusPending || attempt.Status != "reserved" ||
				attempt.Confirmed != 0 || attempt.Unknown != 0 ||
				operation.Observations[stage].State != workspaceLaunchStagePending && operation.Observations[stage].State != workspaceLaunchStageOwnershipPending && operation.Observations[stage].State != workspaceLaunchStageComputeDispatchPending {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
		case "consumed":
			if !validWorkspaceLaunchResumeAuthorizationConsumedAt(authorization.ConsumedAt) || attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
		case "failed":
			originalFailure := operation.Stage == stage &&
				(operation.Status == contracts.StatusManualReview || workspaceLaunchCloseoutActive(operation) || operation.Status == contracts.StatusFailed && operation.DisposableReset != nil) &&
				attempt.Confirmed == 0 && attempt.Unknown == 1 && attempt.Status == "unknown" && operation.Observations[stage].State == workspaceLaunchStageUnknown
			revisionContinuation := runtimeImageRevisionContinuation && (operation.Stage == stage && operation.Status == contracts.StatusPending && attempt.Confirmed == 0 && attempt.Unknown == 0 && attempt.Status == "reserved" &&
				(operation.Observations[stage].State == workspaceLaunchStageRuntimeImageRevisionPending || operation.Observations[stage].State == workspaceLaunchStagePending) ||
				operation.Stage == stage && operation.Status == contracts.StatusManualReview && attempt.Confirmed == 0 && attempt.Unknown == 1 && attempt.Status == "unknown" ||
				operation.Stage != stage && attempt.Confirmed == 1 && attempt.Unknown == 0 && attempt.Status == "confirmed")
			if !validWorkspaceLaunchResumeAuthorizationConsumedAt(authorization.ConsumedAt) || !originalFailure && !revisionContinuation {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
		default:
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
		}
	}
	for key, claim := range operation.ContinuationReadClaims {
		authorization, found := freshAuthorizationsByID[claim.AuthorizationID]
		attempt := operation.Attempts[claim.Stage]
		if !found || claim.SchemaVersion != workspaceLaunchFreshContinuationSchemaVersion || claim.Stage != authorization.Stage ||
			claim.IdempotencyKey != authorization.IdempotencyKey || claim.Readback <= authorization.ReadbacksAtAuthorization ||
			claim.Readback > authorization.ReadbacksAtAuthorization+authorization.AuthoritativeReadBudget ||
			key != workspaceLaunchFreshContinuationClaimKey(claim.AuthorizationID, claim.Readback) || claim.Readback > attempt.PendingReadbacks {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
		}
		switch claim.Status {
		case "claimed":
			if authorization.Status != "active" || claim.CompletedAt != "" {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
			if _, err := time.Parse(time.RFC3339Nano, claim.LeaseExpiresAt); err != nil {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
		case "pending", "ready", "failed", "expired":
			if claim.LeaseExpiresAt != "" || !validWorkspaceLaunchResumeAuthorizationConsumedAt(claim.CompletedAt) ||
				claim.Status == "ready" && authorization.Status != "consumed" || claim.Status == "failed" && authorization.Status != "failed" {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
		default:
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
		}
	}
	for stage, authorization := range operation.FreshContinuationAuthorizations {
		attempt := operation.Attempts[stage]
		readbackLimit := min(attempt.PendingReadbacks, authorization.ReadbacksAtAuthorization+authorization.AuthoritativeReadBudget)
		for readback := authorization.ReadbacksAtAuthorization + 1; readback <= readbackLimit; readback++ {
			if _, exists := operation.ContinuationReadClaims[workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, readback)]; !exists {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
		}
	}
	if operation.ID == "" || operation.stringFact("requestHash") == "" || operation.stringFact("accountId") == "" || operation.stringFact("ownerUserId") == "" ||
		operation.int64Fact("sub2apiUserId") <= 0 ||
		operation.stringFact("workspaceId") == "" || operation.stringFact("name") == "" || operation.stringFact("packageId") == "" ||
		operation.stringFact("priceVersion") == "" || operation.intFact("sizeGb") <= 0 || operation.int64Fact("totalChargeUsdMicros") < 0 || operation.boolFact("resourceBillingEnabled") && operation.int64Fact("totalChargeUsdMicros") <= 0 ||
		operation.stringFact("providerProfileRef") == "" || operation.stringFact("preflightBindingRef") == "" || !workspaceProviderSpecDigestPattern.MatchString(operation.stringFact("specDigest")) ||
		operation.stringFact("sub2apiRedeemCode") == "" {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("missing_canonical_facts")
	}
	if workspaceLaunchModeRequiresApplicationFacts(mode) && (operation.int64Fact("workspaceKeyGroupId") <= 0 || operation.stringFact("workspaceImageDigest") == "") {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("missing_canonical_facts")
	}
	if stringValue(row["action"]) != "" && stringValue(row["action"]) != workspaceLaunchAction ||
		stringValue(row["accountId"]) != "" && stringValue(row["accountId"]) != operation.stringFact("accountId") ||
		stringValue(row["workspaceId"]) != "" && stringValue(row["workspaceId"]) != operation.stringFact("workspaceId") {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("row_identity_mismatch")
	}
	for _, stage := range plan[:len(plan)-1] {
		attempt, exists := operation.Attempts[stage]
		if failedFields := workspaceLaunchAttemptDecodeFailedFields(operation, stage, attempt, exists); len(failedFields) > 0 {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecodeFields("invalid_attempts", failedFields...)
		}
		if attempt.DispatchLeaseExpiresAt != "" {
			deadline, err := time.Parse(time.RFC3339Nano, attempt.DispatchLeaseExpiresAt)
			if err != nil || deadline.IsZero() || attempt.Attempted != 1 {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecodeFields("invalid_attempts", string(stage)+"_dispatch_lease")
			}
		}
		if attempt.PendingDeadlineAt != "" {
			deadline, err := time.Parse(time.RFC3339Nano, attempt.PendingDeadlineAt)
			if stage != contracts.StageCompute || err != nil || deadline.IsZero() {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecodeFields("invalid_attempts", string(stage)+"_pending_deadline")
			}
		}
	}
	if !validWorkspaceLaunchCloseout(operation) {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_closeout")
	}
	if workspaceLaunchCloseoutActive(operation) {
		if operation.Stage == contracts.StageSucceeded || operation.Stage == contracts.StageActivation || operation.Stage == contracts.StageReceipt || operation.boolFact("runtimeReady") || operation.stringFact("workspaceActivatedAt") != "" {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_closeout")
		}
		return operation, nil
	}

	if operation.Stage == contracts.StageSucceeded {
		if operation.Status != contracts.StatusSucceeded {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("status_stage_mismatch")
		}
	} else if operation.Status == contracts.StatusFailed {
		if operation.Stage != contracts.StageDebit || operation.DisposableReset == nil {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("status_stage_mismatch")
		}
	} else if operation.Status != contracts.StatusPending && operation.Status != contracts.StatusManualReview || operation.DisposableReset != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("status_stage_mismatch")
	}
	return operation, nil
}

func workspaceLaunchReconcileOperationRow(operation workspaceLaunchReconcileOperation) (map[string]any, error) {
	if operation.raw == nil {
		return nil, errInvalidWorkspaceLaunchOperation
	}
	raw := make(map[string]json.RawMessage, len(operation.raw)+8)
	for key, value := range operation.raw {
		raw[key] = append(json.RawMessage(nil), value...)
	}
	for _, field := range workspaceLaunchReconcileForbiddenFields {
		delete(raw, field)
	}
	for key, value := range map[string]any{
		"schemaVersion": operation.SchemaVersion, "version": operation.Version, "stage": operation.Stage, "attempts": operation.Attempts,
		"observations": operation.Observations, "resultChecks": operation.ResultChecks, "consumedResumeAuthorizations": operation.ConsumedResumeAuthorizations, "resumeAuthorization": operation.ResumeAuthorization,
		"resumeAuthorizationConsumedAt": operation.ResumeAuthorizationConsumedAt, "idempotentReplayClaims": operation.IdempotentReplayClaims,
		"freshContinuationAuthorizations": operation.FreshContinuationAuthorizations, "continuationReadClaims": operation.ContinuationReadClaims,
		"runtimeRepair":   operation.RuntimeRepair,
		"disposableReset": operation.DisposableReset,
		"closeout":        operation.Closeout,
	} {
		if key == "closeout" && operation.Closeout == nil || key == "resultChecks" && len(operation.ResultChecks) == 0 ||
			key == "consumedResumeAuthorizations" && len(operation.ConsumedResumeAuthorizations) == 0 ||
			key == "resumeAuthorization" && operation.ResumeAuthorization == nil || key == "resumeAuthorizationConsumedAt" && operation.ResumeAuthorizationConsumedAt == "" ||
			key == "idempotentReplayClaims" && len(operation.IdempotentReplayClaims) == 0 ||
			key == "freshContinuationAuthorizations" && len(operation.FreshContinuationAuthorizations) == 0 ||
			key == "continuationReadClaims" && len(operation.ContinuationReadClaims) == 0 ||
			key == "runtimeRepair" && operation.RuntimeRepair == nil || key == "disposableReset" && operation.DisposableReset == nil {
			delete(raw, key)
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		raw[key] = encoded
	}
	result, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"id": operation.ID, "operationId": operation.ID,
		"accountId": operation.stringFact("accountId"), "workspaceId": operation.stringFact("workspaceId"),
		"resourceId": operation.stringFact("workspaceId"), "resourceKind": "workspace_launch",
		"action": workspaceLaunchAction, "status": string(operation.Status), "result": string(result),
		"computeAllocationId": operation.stringFact("computeAllocationId"), "storageId": operation.stringFact("storageId"),
		"createdAt": operation.CreatedAt,
	}, nil
}

func (operation workspaceLaunchReconcileOperation) stringFact(field string) string {
	var value string
	_ = json.Unmarshal(operation.raw[field], &value)
	return value
}

func (operation workspaceLaunchReconcileOperation) intFact(field string) int {
	var value int
	_ = json.Unmarshal(operation.raw[field], &value)
	return value
}

func (operation workspaceLaunchReconcileOperation) int64Fact(field string) int64 {
	var value int64
	_ = json.Unmarshal(operation.raw[field], &value)
	return value
}

func (operation workspaceLaunchReconcileOperation) boolFact(field string) bool {
	var value bool
	_ = json.Unmarshal(operation.raw[field], &value)
	return value
}

func workspaceLaunchReconcileStageValid(stage contracts.Stage) bool {
	for _, candidate := range workspaceLaunchReconcileStages {
		if stage == candidate {
			return true
		}
	}
	return false
}

func workspaceLaunchReconcileIdentityMatches(current, desired map[string]any) bool {
	next, nextErr := decodeWorkspaceLaunchReconcileOperation(desired)
	if nextErr != nil {
		return false
	}
	if existing, existingErr := decodeWorkspaceLaunchReconcileOperation(current); existingErr == nil {
		return existing.ID == next.ID && existing.stringFact("accountId") == next.stringFact("accountId") &&
			existing.stringFact("workspaceId") == next.stringFact("workspaceId") && existing.stringFact("ownerUserId") == next.stringFact("ownerUserId") &&
			existing.stringFact("requestHash") == next.stringFact("requestHash") && next.Version == existing.Version+1 && workspaceLaunchCloseoutTransitionMatches(existing.Closeout, next.Closeout)
	}
	return false
}

const (
	workspaceLaunchStageAbsent                      = contracts.StageStateAbsent
	workspaceLaunchStageOwnershipPending            = contracts.StageStateOwnershipPending
	workspaceLaunchStageComputeDispatchPending      = contracts.StageStateComputeDispatchPending
	workspaceLaunchStageComputePoolQueued           = contracts.StageStateComputePoolQueued
	workspaceLaunchStagePending                     = contracts.StageStatePending
	workspaceLaunchStageReady                       = contracts.StageStateReady
	workspaceLaunchStageRuntimeImageRevisionPending = contracts.StageStateRuntimeImageRevisionPending
	workspaceLaunchStageUnknown                     = contracts.StageStateUnknown
)

const (
	workspaceLaunchCanonicalFactString  workspaceLaunchCanonicalFactKind = "string"
	workspaceLaunchCanonicalFactInteger workspaceLaunchCanonicalFactKind = "integer"
	workspaceLaunchCanonicalFactBool    workspaceLaunchCanonicalFactKind = "bool"
	workspaceLaunchCanonicalFactObject  workspaceLaunchCanonicalFactKind = "object"
)

type workspaceLaunchDecodeError struct {
	category     string
	failedFields []string
}

type workspaceLaunchCanonicalFactKind string

type workspaceLaunchCanonicalFactSpec struct {
	Kind     workspaceLaunchCanonicalFactKind
	Required bool
	Positive bool
	Exact    any
}

type workspaceLaunchStageAttempt struct {
	Attempted              int    `json:"attempted"`
	Confirmed              int    `json:"confirmed"`
	Unknown                int    `json:"unknown"`
	Max                    int    `json:"max"`
	Status                 string `json:"status,omitempty"`
	IdempotencyKey         string `json:"idempotencyKey,omitempty"`
	PendingReadbacks       int    `json:"pendingReadbacks,omitempty"`
	MaxPendingReadbacks    int    `json:"maxPendingReadbacks,omitempty"`
	PendingDeadlineAt      string `json:"pendingDeadlineAt,omitempty"`
	DispatchLeaseExpiresAt string `json:"dispatchLeaseExpiresAt,omitempty"`
}

type workspaceLaunchStageObservation struct {
	State      contracts.StageState                    `json:"state"`
	Facts      map[string]any                          `json:"facts,omitempty"`
	Diagnostic *clients.WorkspaceLaunchStageDiagnostic `json:"diagnostic,omitempty"`
}

type workspaceLaunchResumeAuthorization struct {
	AuthorizationID                 string                                           `json:"authorizationId"`
	LaunchVersion                   int                                              `json:"launchVersion"`
	AuthorizedStage                 contracts.Stage                                  `json:"authorizedStage"`
	AuthorizedBy                    string                                           `json:"authorizedBy"`
	AuthorizedAt                    string                                           `json:"authorizedAt"`
	Reason                          string                                           `json:"reason"`
	MutationBudget                  int                                              `json:"mutationBudget"`
	IdempotentReplayBudget          int                                              `json:"idempotentReplayBudget,omitempty"`
	AuthoritativeReadBudget         int                                              `json:"authoritativeReadBudget,omitempty"`
	ReadbacksAtAuthorization        int                                              `json:"readbacksAtAuthorization,omitempty"`
	ReplacementWorkspaceImageDigest string                                           `json:"replacementWorkspaceImageDigest,omitempty"`
	AcceptanceBResumeExisting       *workspaceLaunchAcceptanceBResumeExistingBinding `json:"acceptanceBResumeExisting,omitempty"`
}

type workspaceLaunchResultCheck struct {
	Authorization workspaceLaunchResumeAuthorization `json:"authorization"`
	ConsumedAt    string                             `json:"consumedAt"`
	State         contracts.StageState               `json:"state"`
	ErrorCode     string                             `json:"errorCode"`
}

type workspaceLaunchConsumedResumeAuthorization struct {
	Authorization workspaceLaunchResumeAuthorization `json:"authorization"`
	ConsumedAt    string                             `json:"consumedAt"`
}

type workspaceLaunchIdempotentReplayClaim struct {
	AuthorizationID string          `json:"authorizationId"`
	Stage           contracts.Stage `json:"stage"`
	IdempotencyKey  string          `json:"idempotencyKey"`
	Status          string          `json:"status"`
	LeaseExpiresAt  string          `json:"leaseExpiresAt,omitempty"`
	CompletedAt     string          `json:"completedAt,omitempty"`
}

type workspaceLaunchFreshContinuationAuthorization struct {
	SchemaVersion            int             `json:"schemaVersion"`
	AuthorizationID          string          `json:"authorizationId"`
	AuthorizationClass       string          `json:"authorizationClass"`
	AccountID                string          `json:"accountId"`
	OperationID              string          `json:"operationId"`
	WorkspaceID              string          `json:"workspaceId"`
	Stage                    contracts.Stage `json:"stage"`
	IdempotencyKey           string          `json:"idempotencyKey"`
	Attempt                  int             `json:"attempt"`
	OperationVersion         int             `json:"operationVersion"`
	MutationBudget           int             `json:"mutationBudget"`
	IdempotentReplayBudget   int             `json:"idempotentReplayBudget"`
	AuthoritativeReadBudget  int             `json:"authoritativeReadBudget"`
	ReadbacksAtAuthorization int             `json:"readbacksAtAuthorization"`
	Status                   string          `json:"status"`
	ConsumedAt               string          `json:"consumedAt,omitempty"`
}

type workspaceLaunchContinuationReadClaim struct {
	SchemaVersion   int             `json:"schemaVersion"`
	AuthorizationID string          `json:"authorizationId"`
	Stage           contracts.Stage `json:"stage"`
	IdempotencyKey  string          `json:"idempotencyKey"`
	Readback        int             `json:"readback"`
	Status          string          `json:"status"`
	LeaseExpiresAt  string          `json:"leaseExpiresAt,omitempty"`
	CompletedAt     string          `json:"completedAt,omitempty"`
}

type workspaceLaunchRuntimeRepair struct {
	AuthorizationID string `json:"authorizationId"`
	LaunchVersion   int    `json:"launchVersion"`
	AuthorizedBy    string `json:"authorizedBy"`
	AuthorizedAt    string `json:"authorizedAt"`
	Reason          string `json:"reason"`
	ImageDigest     string `json:"imageDigest"`
}

type workspaceLaunchDisposableResetEvidence struct {
	SchemaVersion            int    `json:"schemaVersion"`
	LaunchVersion            int    `json:"launchVersion"`
	ResetPlanDigest          string `json:"resetPlanDigest"`
	AuthorityDigest          string `json:"authorityDigest"`
	LedgerReceiptDigest      string `json:"ledgerReceiptDigest"`
	CompletedAt              string `json:"completedAt"`
	MutationScopeMatchedPlan bool   `json:"mutationScopeMatchedPlan"`
}

type workspaceLaunchReceiptProjection struct {
	AccountID   string
	OwnerUserID string
	WorkspaceID string
	ReceiptID   string
}

func workspaceLaunchStageIdempotencyKey(operation workspaceLaunchReconcileOperation, attempt int) string {
	switch operation.Stage {
	case contracts.StageKey:
		return operation.ID + ":workspace-key"
	case contracts.StageDebit:
		return operation.stringFact("sub2apiRedeemCode")
	case contracts.StageCompute:
		return operation.ID + ":ensure-compute-allocation"
	case contracts.StageStorage, contracts.StageAttachment, contracts.StageSecret, contracts.StageRuntime:
		return operation.ID + ":" + string(operation.Stage)
	case contracts.StageActivation:
		return operation.ID + ":activation"
	case contracts.StageReceipt:
		return operation.ID + ":purchase-receipt"
	}
	return operation.ID + ":" + string(operation.Stage) + ":attempt:" + strconv.Itoa(attempt)
}

func workspaceLaunchAcceptedPriceComponents(operation workspaceLaunchReconcileOperation) (int64, int64, error) {
	catalog, found := pricingCatalogByVersion(operation.stringFact("priceVersion"))
	if !found {
		return 0, 0, errors.New("workspace_launch_price_version_unknown")
	}
	resourceBillingEnabled := operation.raw["resourceBillingEnabled"] == nil || operation.boolFact("resourceBillingEnabled")
	if !resourceBillingEnabled {
		if operation.int64Fact("totalChargeUsdMicros") != 0 {
			return 0, 0, errors.New("workspace_launch_pricing_snapshot_invalid")
		}
		return 0, 0, nil
	}
	quote, err := workspacePricingPreview(catalog, map[string]any{"packageId": operation.stringFact("packageId"), "sizeGb": operation.intFact("sizeGb")})
	if err != nil {
		return 0, 0, err
	}
	computePrice, computeOK := requiredPositiveInteger(mapField(quote, "compute"), "chargeUsdMicros")
	storagePrice, storageOK := requiredPositiveInteger(mapField(quote, "storage"), "chargeUsdMicros")
	totalPrice, totalOK := requiredPositiveInteger(quote, "totalChargeUsdMicros")
	if !computeOK || !storageOK || !totalOK || totalPrice != operation.int64Fact("totalChargeUsdMicros") {
		return 0, 0, errors.New("workspace_launch_pricing_snapshot_invalid")
	}
	return computePrice, storagePrice, nil
}

func workspaceLaunchCanonicalReceiptExecution(operation workspaceLaunchReconcileOperation) map[string]any {
	if operation.provisioningMode() == contracts.WorkspaceProvisioningResourceOnly {
		return (contracts.WorkspaceResourceReceiptExecution{OperationID: operation.ID, ResourceType: "workspace", ResourceID: operation.stringFact("workspaceId"), ComputeAllocationID: operation.stringFact("computeAllocationId"), StorageID: operation.stringFact("storageId"), AttachmentID: operation.stringFact("attachmentId"), ProvisioningMode: contracts.WorkspaceProvisioningResourceOnly}).Fields()
	}
	return map[string]any{
		"operationId": operation.ID, "resourceType": "workspace", "resourceId": operation.stringFact("workspaceId"),
		"computeAllocationId": operation.stringFact("computeAllocationId"), "storageId": operation.stringFact("storageId"), "attachmentId": operation.stringFact("attachmentId"),
		"runtimeId": operation.stringFact("runtimeId"), "workspaceApiKeyId": operation.int64Fact("workspaceApiKeyId"),
		"workspaceKeyFingerprint": operation.stringFact("workspaceKeyFingerprint"), "runtimeServiceName": operation.stringFact("runtimeServiceName"),
		"gatewaySecretRef": operation.stringFact("gatewaySecretRef"),
	}
}

func workspaceLaunchStableProjectionMismatchFields(operation workspaceLaunchReconcileOperation, workspace map[string]any) []string {
	checks := []struct {
		field string
		match bool
	}{
		{"account_id", firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])) == operation.stringFact("accountId")},
		{"owner_user_id", stringValue(workspace["ownerUserId"]) == operation.stringFact("ownerUserId")},
		{"workspace_id", stringValue(workspace["id"]) == operation.stringFact("workspaceId")},
		{"name", stringValue(workspace["name"]) == operation.stringFact("name")},
		{"package_id", stringValue(workspace["packageId"]) == operation.stringFact("packageId")},
		{"url", stringValue(workspace["url"]) == operation.stringFact("url")},
		{"compute_allocation_id", firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])) == operation.stringFact("computeAllocationId")},
		{"storage_id", stringValue(workspace["storageId"]) == operation.stringFact("storageId")},
		{"attachment_id", firstNonEmpty(stringValue(workspace["currentAttachmentId"]), stringValue(workspace["attachmentId"])) == operation.stringFact("attachmentId")},
		{"runtime_id", stringValue(workspace["runtimeId"]) == operation.stringFact("runtimeId")},
		{"runtime_service_name", stringValue(nested(workspace, "runtime", "serviceName")) == operation.stringFact("runtimeServiceName")},
		{"runtime_username", stringValue(nested(workspace, "access", "username")) == operation.stringFact("runtimeUsername")},
		{"credential_status", stringValue(nested(workspace, "access", "credentialStatus")) == operation.stringFact("credentialStatus")},
		{"credential_version", stringValue(nested(workspace, "access", "credentialVersion")) == operation.stringFact("credentialVersion")},
		{"credential_secret_ref", stringValue(nested(workspace, "access", "secretRef")) == operation.stringFact("credentialSecretRef")},
		{"price_version", stringValue(workspace["priceVersion"]) == operation.stringFact("priceVersion")},
		{"total_usd_micros", int64(numberField(workspace, "totalUsdMicros", 0)) == operation.int64Fact("totalChargeUsdMicros")},
		{"period_start", stringValue(workspace["periodStart"]) == operation.stringFact("periodStart")},
		{"paid_through", stringValue(workspace["paidThrough"]) == operation.stringFact("paidThrough")},
		{"billing_anchor_day", int(numberField(workspace, "billingAnchorDay", 0)) == operation.intFact("billingAnchorDay")},
		{"storage_gb", int(numberField(workspace, "storageGb", 0)) == operation.intFact("sizeGb")},
		{"workspace_running", firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"])) == "running"},
	}
	mismatches := make([]string, 0)
	for _, check := range checks {
		if !check.match {
			mismatches = append(mismatches, check.field)
		}
	}
	return mismatches
}

func invalidWorkspaceLaunchDecode(category string) error {
	return workspaceLaunchDecodeError{category: category}
}

func invalidWorkspaceLaunchDecodeFields(category string, failedFields ...string) error {
	return workspaceLaunchDecodeError{category: category, failedFields: append([]string(nil), failedFields...)}
}

const workspaceLaunchLegacyV3AuthoritativeReadBudget = 0

var workspaceLaunchStageCanonicalFacts = map[contracts.Stage]map[string]workspaceLaunchCanonicalFactSpec{
	contracts.StageKey: {
		"workspaceApiKeyId":       {Kind: workspaceLaunchCanonicalFactInteger, Required: true, Positive: true},
		"workspaceKeyGroupId":     {Kind: workspaceLaunchCanonicalFactInteger, Required: true, Positive: true},
		"workspaceKeyStatus":      {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"workspaceKeyFingerprint": {Kind: workspaceLaunchCanonicalFactString, Required: true},
	},
	contracts.StageDebit: {
		"chargeAttempted":            {Kind: workspaceLaunchCanonicalFactBool, Required: true},
		"chargeConfirmation":         {Kind: workspaceLaunchCanonicalFactObject, Required: true},
		"preChargeBalanceUsdMicros":  {Kind: workspaceLaunchCanonicalFactInteger, Required: true},
		"postChargeBalanceUsdMicros": {Kind: workspaceLaunchCanonicalFactInteger},
		"postChargeBalanceKnown":     {Kind: workspaceLaunchCanonicalFactBool},
		"billingPeriodState":         {Kind: workspaceLaunchCanonicalFactString},
		"periodStart":                {Kind: workspaceLaunchCanonicalFactString},
		"paidThrough":                {Kind: workspaceLaunchCanonicalFactString},
		"billingAnchorDay":           {Kind: workspaceLaunchCanonicalFactInteger, Positive: true},
	},
	contracts.StageCompute: {
		"computeAllocationId": {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"computeBindingRef":   {Kind: workspaceLaunchCanonicalFactString, Required: true},
	},
	contracts.StageStorage: {
		"storageId":         {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"storageBindingRef": {Kind: workspaceLaunchCanonicalFactString, Required: true},
	},
	contracts.StageAttachment: {
		"attachmentId":         {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"attachmentBindingRef": {Kind: workspaceLaunchCanonicalFactString, Required: true},
	},
	contracts.StageSecret: {
		"gatewaySecretRef":        {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"gatewaySecretVersion":    {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"secretBindingRef":        {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"workspaceKeyStatus":      {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"workspaceKeyFingerprint": {Kind: workspaceLaunchCanonicalFactString},
		"credentialStatus":        {Kind: workspaceLaunchCanonicalFactString},
		"credentialVersion":       {Kind: workspaceLaunchCanonicalFactString},
		"credentialSecretRef":     {Kind: workspaceLaunchCanonicalFactString},
	},
	contracts.StageRuntime: {
		"runtimeId":          {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"runtimeReady":       {Kind: workspaceLaunchCanonicalFactBool, Required: true, Exact: true},
		"runtimeServiceName": {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"runtimeBindingRef":  {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"runtimeUsername":    {Kind: workspaceLaunchCanonicalFactString},
		"url":                {Kind: workspaceLaunchCanonicalFactString, Required: true},
	},
	contracts.StageActivation: {
		"activationOperationId": {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"workspaceActivatedAt":  {Kind: workspaceLaunchCanonicalFactString, Required: true},
	},
	contracts.StageReceipt: {
		"receiptId":          {Kind: workspaceLaunchCanonicalFactString, Required: true},
		"receiptOperationId": {Kind: workspaceLaunchCanonicalFactString},
	},
}

func validWorkspaceLaunchFabricDiagnostic(stage, state string, diagnostic *clients.WorkspaceLaunchStageDiagnostic) bool {
	if diagnostic == nil {
		return true
	}
	if stage != "runtime" || diagnostic.SchemaVersion != 1 || diagnostic.Owner != "fabric.tencent_tke" ||
		!workspaceLaunchStageDiagnosticErrorPattern.MatchString(diagnostic.BlockReason) || len(diagnostic.Checks) > 32 {
		return false
	}
	if _, err := time.Parse(time.RFC3339Nano, diagnostic.ObservedAt); err != nil {
		return false
	}
	if diagnostic.ErrorCode != "" && !workspaceLaunchStageDiagnosticErrorPattern.MatchString(diagnostic.ErrorCode) {
		return false
	}
	stageState := contracts.StageState(state)
	if stageState == workspaceLaunchStageReady && (diagnostic.BlockReason != "none" || diagnostic.Retryable) ||
		stageState != workspaceLaunchStageReady && diagnostic.BlockReason == "none" {
		return false
	}
	for _, check := range diagnostic.Checks {
		if !workspaceLaunchStageDiagnosticErrorPattern.MatchString(check.Name) {
			return false
		}
		encoded, err := json.Marshal(check.Details)
		if err != nil || len(encoded) > 16*1024 {
			return false
		}
	}
	return true
}

func validateWorkspaceLaunchStageFacts(stage contracts.Stage, facts map[string]any, ignoreUnknown bool) (map[string]json.RawMessage, error) {
	specs := workspaceLaunchStageCanonicalFacts[stage]
	if specs == nil {
		return nil, errInvalidWorkspaceLaunchOperation
	}
	encodedFacts := make(map[string]json.RawMessage, len(facts))
	for field, value := range facts {
		spec, ok := specs[field]
		if !ok {
			if ignoreUnknown {
				continue
			}
			return nil, errInvalidWorkspaceLaunchOperation
		}
		encoded, err := json.Marshal(value)
		if err != nil || !validWorkspaceLaunchCanonicalFact(encoded, spec) {
			return nil, errInvalidWorkspaceLaunchOperation
		}
		encodedFacts[field] = encoded
	}
	for field, spec := range specs {
		if spec.Required {
			if _, ok := encodedFacts[field]; !ok {
				return nil, errInvalidWorkspaceLaunchOperation
			}
		}
	}
	return encodedFacts, nil
}

func validWorkspaceLaunchDisposableResetEvidence(evidence workspaceLaunchDisposableResetEvidence, operationVersion int) bool {
	if evidence.SchemaVersion != 1 || evidence.LaunchVersion <= 0 || evidence.LaunchVersion+1 != operationVersion ||
		!workspaceLaunchDisposableResetDigestPattern.MatchString(evidence.ResetPlanDigest) ||
		!workspaceLaunchDisposableResetDigestPattern.MatchString(evidence.AuthorityDigest) || !workspaceLaunchDisposableResetDigestPattern.MatchString(evidence.LedgerReceiptDigest) ||
		!evidence.MutationScopeMatchedPlan {
		return false
	}
	_, err := time.Parse(time.RFC3339Nano, evidence.CompletedAt)
	return err == nil
}

func validWorkspaceLaunchResumeAuthorization(authorization workspaceLaunchResumeAuthorization) bool {
	maximumReadBudget := workspaceLaunchAuthoritativeReadBudget
	authorizedStage := authorization.AuthorizedStage
	if authorizedStage == contracts.StageCompute {
		maximumReadBudget = workspaceLaunchComputeFreshContinuationAdditionalReadBudget
	}
	runtimeImageRevisionValid := authorization.ReplacementWorkspaceImageDigest == "" ||
		authorizedStage == contracts.StageRuntime && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 1 &&
			authorization.AuthoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget && authorization.ReadbacksAtAuthorization >= 0 &&
			workspaceImageReferenceWithDigest(authorization.ReplacementWorkspaceImageDigest) &&
			authorization.ReplacementWorkspaceImageDigest == strings.TrimSpace(authorization.ReplacementWorkspaceImageDigest)
	if strings.TrimSpace(authorization.AuthorizationID) == "" || authorization.LaunchVersion <= 0 || !workspaceLaunchReconcileStageValid(authorizedStage) || authorizedStage == contracts.StageSucceeded ||
		strings.TrimSpace(authorization.AuthorizedBy) == "" || strings.TrimSpace(authorization.Reason) == "" || authorization.MutationBudget < 0 || authorization.MutationBudget > 1 ||
		authorization.IdempotentReplayBudget < 0 || authorization.IdempotentReplayBudget > 1 || authorization.MutationBudget+authorization.IdempotentReplayBudget > 1 ||
		authorization.AuthoritativeReadBudget < 0 || authorization.AuthoritativeReadBudget > maximumReadBudget || authorization.ReadbacksAtAuthorization < 0 ||
		authorization.MutationBudget > 0 && (authorization.IdempotentReplayBudget != 0 || authorization.AuthoritativeReadBudget != 0) ||
		authorization.IdempotentReplayBudget > 0 && authorization.AuthoritativeReadBudget == 0 ||
		authorization.AuthorizationID != strings.TrimSpace(authorization.AuthorizationID) || authorization.AuthorizedBy != strings.TrimSpace(authorization.AuthorizedBy) || authorization.Reason != strings.TrimSpace(authorization.Reason) ||
		!runtimeImageRevisionValid {
		return false
	}
	authorizedAt, err := time.Parse(time.RFC3339, authorization.AuthorizedAt)
	return err == nil && !authorizedAt.IsZero() && validWorkspaceLaunchAcceptanceBResumeExistingBinding(authorization.AcceptanceBResumeExisting)
}

func operationWithStage(operation workspaceLaunchReconcileOperation, stage contracts.Stage) workspaceLaunchReconcileOperation {
	operation.Stage = stage
	return operation
}

func (operation workspaceLaunchReconcileOperation) resumeAuthorizationByID(authorizationID string) (workspaceLaunchResumeAuthorization, bool, bool) {
	if operation.ResumeAuthorization != nil && operation.ResumeAuthorization.AuthorizationID == authorizationID {
		return *operation.ResumeAuthorization, operation.ResumeAuthorizationConsumedAt != "", true
	}
	for _, consumed := range operation.ConsumedResumeAuthorizations {
		if consumed.Authorization.AuthorizationID == authorizationID {
			return consumed.Authorization, true, true
		}
	}
	return workspaceLaunchResumeAuthorization{}, false, false
}

func validWorkspaceLaunchResumeAuthorizationConsumedAt(value string) bool {
	consumedAt, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !consumedAt.IsZero()
}

const workspaceLaunchAuthoritativeReadBudget = 3

func workspaceLaunchMaximumPersistedReadbacks(stage contracts.Stage) int {
	if stage == contracts.StageCompute {
		return workspaceLaunchAuthoritativeReadBudget + workspaceLaunchComputeFreshContinuationAdditionalReadBudget
	}
	if stage == contracts.StageRuntime {
		return 7 * workspaceLaunchAuthoritativeReadBudget
	}
	return workspaceLaunchAuthoritativeReadBudget
}

func (operation workspaceLaunchReconcileOperation) hasRuntimeImageRevisionAuthorization() bool {
	if operation.ResumeAuthorization != nil && operation.ResumeAuthorization.AuthorizedStage == contracts.StageRuntime &&
		operation.ResumeAuthorization.ReplacementWorkspaceImageDigest != "" {
		return true
	}
	for _, consumed := range operation.ConsumedResumeAuthorizations {
		if consumed.Authorization.AuthorizedStage == contracts.StageRuntime && consumed.Authorization.ReplacementWorkspaceImageDigest != "" {
			return true
		}
	}
	return false
}

func validWorkspaceLaunchCanonicalFact(encoded json.RawMessage, spec workspaceLaunchCanonicalFactSpec) bool {
	switch spec.Kind {
	case workspaceLaunchCanonicalFactString:
		var value string
		if json.Unmarshal(encoded, &value) != nil || strings.TrimSpace(value) == "" {
			return false
		}
		return spec.Exact == nil || value == spec.Exact
	case workspaceLaunchCanonicalFactInteger:
		value, err := strconv.ParseInt(string(encoded), 10, 64)
		if err != nil || spec.Positive && value <= 0 {
			return false
		}
		return spec.Exact == nil || value == spec.Exact
	case workspaceLaunchCanonicalFactBool:
		var value bool
		if json.Unmarshal(encoded, &value) != nil || string(encoded) != "true" && string(encoded) != "false" {
			return false
		}
		return spec.Exact == nil || value == spec.Exact
	case workspaceLaunchCanonicalFactObject:
		var value map[string]json.RawMessage
		return json.Unmarshal(encoded, &value) == nil && len(value) > 0
	default:
		return false
	}
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
	if c.KeyRevokedAt != "" && c.FrozenAt == "" || c.ResourcesAbsentAt != "" && c.FrozenAt == "" || c.RefundedUSDMicros > 0 && (c.ResourcesAbsentAt == "" || c.DebitState != "confirmed") {
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
		return operation.Status == contracts.StatusPending && c.FrozenAt != "" && c.ResourcesAbsentAt == "" && c.ReceiptID == "" && c.CompletedAt == ""
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

func workspaceLaunchAttemptDecodeFailedFields(operation workspaceLaunchReconcileOperation, stage contracts.Stage, attempt workspaceLaunchStageAttempt, exists bool) []string {
	checks := []struct {
		field string
		valid bool
	}{
		{"present", exists},
		{"max", attempt.Max == 1},
		{"attempted", attempt.Attempted >= 0 && attempt.Attempted <= 1},
		{"confirmed", attempt.Confirmed >= 0 && attempt.Confirmed <= attempt.Attempted},
		{"unknown", attempt.Unknown >= 0 && attempt.Unknown <= attempt.Attempted},
		{"outcome_count", attempt.Confirmed+attempt.Unknown <= attempt.Attempted},
		{"max_pending_readbacks", attempt.MaxPendingReadbacks >= workspaceLaunchLegacyV3AuthoritativeReadBudget && attempt.MaxPendingReadbacks <= workspaceLaunchMaximumPersistedReadbacks(stage)},
		{"pending_readbacks", attempt.PendingReadbacks >= 0 && attempt.PendingReadbacks <= attempt.MaxPendingReadbacks},
		{"runtime_revision_authorization", stage != contracts.StageRuntime || attempt.MaxPendingReadbacks <= workspaceLaunchAuthoritativeReadBudget ||
			operation.hasRuntimeImageRevisionAuthorization() || workspaceLaunchLocalDockerRuntimeReadbackAuthorized(operation, attempt)},
		{"status", attempt.Status == "" || attempt.Status == "reserved" || attempt.Status == "confirmed" || attempt.Status == "unknown"},
	}
	failedFields := make([]string, 0)
	for _, check := range checks {
		if !check.valid {
			failedFields = append(failedFields, string(stage)+"_"+check.field)
		}
	}
	return failedFields
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
	phases := map[string]int{"freeze": 0, "key": 1, "resources": 1, "refund": 2, "receipt": 3, "complete": 4}
	before, oldOK := phases[previous.Phase]
	after, newOK := phases[next.Phase]
	return oldOK && newOK && (after == before && (previous.Phase == next.Phase || previous.Phase == "key" && next.Phase == "resources") || after == before+1)
}

func workspaceLaunchFreshContinuationAuthorizationContractValid(
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchFreshContinuationAuthorization,
) bool {
	legacyReadBudget := workspaceLaunchFreshContinuationReadBudget(operation.Stage)
	if authorization.SchemaVersion == workspaceLaunchFreshContinuationSchemaVersion &&
		authorization.AuthorizationClass == workspaceLaunchFreshContinuationAuthorizationClass &&
		authorization.AuthoritativeReadBudget == legacyReadBudget {
		return authorization.AuthorizationID == workspaceLaunchFreshContinuationAuthorizationID(operation, attempt, authorization.OperationVersion)
	}
	if operation.Stage != contracts.StageRuntime || operation.stringFact("providerProfileRef") != "local-docker" {
		return false
	}
	extendedReadBudget := workspaceLaunchFreshContinuationReadBudgetForOperation(operation, operation.Stage)
	return authorization.SchemaVersion == workspaceLaunchLocalDockerRuntimeFreshContinuationSchemaVersion &&
		authorization.AuthorizationClass == workspaceLaunchLocalDockerRuntimeFreshContinuationAuthorizationClass &&
		authorization.AuthoritativeReadBudget == extendedReadBudget &&
		authorization.AuthorizationID == workspaceLaunchFreshContinuationAuthorizationIDForContract(
			operation, attempt, authorization.OperationVersion, authorization.SchemaVersion, authorization.AuthorizationClass, authorization.AuthoritativeReadBudget,
		)
}

func workspaceLaunchFreshContinuationClaimKey(authorizationID string, readback int) string {
	return authorizationID + ":readback:" + strconv.Itoa(readback)
}

func workspaceLaunchFreshContinuationReplayBudget(stage contracts.Stage) int {
	if stage == contracts.StageCompute {
		return 1
	}
	return 0
}

const workspaceLaunchFreshContinuationSchemaVersion = 1

func validWorkspaceLaunchAcceptanceBResumeExistingBinding(binding *workspaceLaunchAcceptanceBResumeExistingBinding) bool {
	if binding == nil {
		return true
	}
	state := contracts.StageState(binding.AuthoritativeState)
	return binding.SchemaVersion == 1 && productionAcceptanceBApprovalIDPattern.MatchString(binding.ApprovalID) &&
		workspaceImageDigestPattern.MatchString(binding.ApprovalSHA256) && productionAcceptanceBReleaseSHAPattern.MatchString(binding.CanonicalCloudSHA) &&
		productionAcceptanceBReleaseSHAPattern.MatchString(binding.CanonicalCloudTree) && workspaceImageDigestPattern.MatchString(binding.DeployedCloudImageDigest) &&
		(state == workspaceLaunchStageReady || state == workspaceLaunchStageAbsent || state == workspaceLaunchStagePending) &&
		validWorkspaceLaunchAcceptanceBIdentityDigests(binding.IdentityDigests)
}

// The original continuation read cadence. The worker that used it as a poll
// interval is retired; the decoder still derives the fresh-continuation read
// budget from it, so the value stays.
const workspaceLaunchContinuationReadCadence = 10 * time.Second

const workspaceLaunchComputeFreshContinuationAdditionalReadBudget = int(workspaceLaunchComputePendingWindow / workspaceLaunchContinuationReadCadence)

var workspaceLaunchDisposableResetDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

const workspaceLaunchFreshContinuationAuthorizationClass = "fresh_typed_pending_system"

func workspaceLaunchFreshContinuationAuthorizationID(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, operationVersion int) string {
	return operation.ID + ":" + operation.stringFact("accountId") + ":" + operation.stringFact("workspaceId") + ":" + string(operation.Stage) + ":" +
		attempt.IdempotencyKey + ":fresh-typed-pending:" + strconv.Itoa(attempt.Attempted) + ":version:" + strconv.Itoa(operationVersion)
}

func workspaceLaunchFreshContinuationReadBudget(stage contracts.Stage) int {
	if stage == contracts.StageCompute {
		return workspaceLaunchComputeFreshContinuationAdditionalReadBudget
	}
	return workspaceLaunchFreshContinuationAdditionalReadBudget
}

func workspaceLaunchFreshContinuationReadBudgetForOperation(operation workspaceLaunchReconcileOperation, stage contracts.Stage) int {
	if stage == contracts.StageRuntime && operation.stringFact("providerProfileRef") == "local-docker" {
		return workspaceLaunchMaximumPersistedReadbacks(stage) - 1
	}
	return workspaceLaunchFreshContinuationReadBudget(stage)
}

func workspaceLaunchLocalDockerRuntimeReadbackAuthorized(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt) bool {
	authorization, exists := operation.FreshContinuationAuthorizations[contracts.StageRuntime]
	extendedReadBudget := workspaceLaunchFreshContinuationReadBudgetForOperation(operation, contracts.StageRuntime)
	return exists && extendedReadBudget != workspaceLaunchFreshContinuationReadBudget(contracts.StageRuntime) &&
		workspaceLaunchFreshContinuationAuthorizationContractValid(operationWithStage(operation, contracts.StageRuntime), attempt, authorization) &&
		attempt.MaxPendingReadbacks == 1+extendedReadBudget
}

const workspaceLaunchComputePendingWindow = 10 * time.Minute

const workspaceLaunchFreshContinuationAdditionalReadBudget = 2

func workspaceLaunchFreshContinuationAuthorizationIDForContract(
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	operationVersion int,
	schemaVersion int,
	authorizationClass string,
	readBudget int,
) string {
	legacyID := workspaceLaunchFreshContinuationAuthorizationID(operation, attempt, operationVersion)
	if schemaVersion == workspaceLaunchFreshContinuationSchemaVersion &&
		authorizationClass == workspaceLaunchFreshContinuationAuthorizationClass &&
		readBudget == workspaceLaunchFreshContinuationReadBudget(operation.Stage) {
		return legacyID
	}
	return legacyID + ":schema:" + strconv.Itoa(schemaVersion) + ":class:" + authorizationClass + ":read-budget:" + strconv.Itoa(readBudget)
}

const workspaceLaunchLocalDockerRuntimeFreshContinuationAuthorizationClass = "fresh_typed_pending_local_docker_runtime_system"

const workspaceLaunchLocalDockerRuntimeFreshContinuationSchemaVersion = 2

func reduceWorkspaceLaunchStageObservation(operation *workspaceLaunchReconcileOperation, observation workspaceLaunchStageObservation) (workspaceLaunchStageObservation, error) {
	if operation == nil || observation.State != workspaceLaunchStageReady {
		return workspaceLaunchStageObservation{}, errInvalidWorkspaceLaunchOperation
	}
	encodedFacts, err := validateWorkspaceLaunchStageFacts(operation.Stage, observation.Facts, true)
	if err != nil {
		return workspaceLaunchStageObservation{}, err
	}
	reduced := workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: map[string]any{}, Diagnostic: observation.Diagnostic}
	for field, encoded := range encodedFacts {
		operation.raw[field] = encoded
		reduced.Facts[field] = observation.Facts[field]
	}
	return reduced, nil
}

var workspaceLaunchStageDiagnosticErrorPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._-]{0,127}$`)

func workspaceImageReferenceWithDigest(value string) bool {
	repository, digest, ok := strings.Cut(strings.TrimSpace(value), "@")
	return ok && repository != "" && !strings.Contains(repository, "@") && workspaceImageDigestPattern.MatchString(digest)
}

func workspaceLaunchActivationRow(operation workspaceLaunchReconcileOperation) (map[string]any, error) {
	if err := workspaceLaunchActivationRuntimeFactsAllowed(operation); err != nil {
		return nil, err
	}
	paidThrough, paidThroughErr := time.Parse(time.RFC3339, operation.stringFact("paidThrough"))
	if paidThroughErr != nil {
		return nil, errInvalidWorkspaceLaunchOperation
	}
	resourceBillingEnabled := operation.raw["resourceBillingEnabled"] == nil || operation.boolFact("resourceBillingEnabled")
	autoRenew := operation.boolFact("autoRenew")
	if autoRenew && !resourceBillingEnabled {
		return nil, errInvalidWorkspaceLaunchOperation
	}
	authorizedBy, authorizedAt := "", ""
	if autoRenew {
		authorizedBy, authorizedAt = operation.stringFact("ownerUserId"), operation.CreatedAt
	}
	computePrice, storagePrice, err := workspaceLaunchAcceptedPriceComponents(operation)
	if err != nil {
		return nil, err
	}
	row := workspaceProjectionRow(domain.WorkspaceProjection{
		ID: operation.stringFact("workspaceId"), AccountID: operation.stringFact("accountId"), OwnerID: operation.stringFact("ownerUserId"),
		Name: operation.stringFact("name"), PackageID: operation.stringFact("packageId"), Provider: "fabric", URL: operation.stringFact("url"), Status: "running",
		ComputeID: operation.stringFact("computeAllocationId"), VolumeID: operation.stringFact("storageId"), AttachmentID: operation.stringFact("attachmentId"),
		RuntimeID: operation.stringFact("runtimeId"), RuntimeServiceName: operation.stringFact("runtimeServiceName"), WorkspaceAPIKeyID: operation.int64Fact("workspaceApiKeyId"),
		RuntimeReady: operation.boolFact("runtimeReady"), RuntimeUsername: operation.stringFact("runtimeUsername"), CredentialStatus: operation.stringFact("credentialStatus"),
		CredentialVersion: operation.stringFact("credentialVersion"), CredentialSecretRef: operation.stringFact("credentialSecretRef"),
		ApplicationBinding: workspaceLaunchActivationApplicationBinding(operation),
	})
	for key, value := range map[string]any{
		"resourceBillingEnabled": resourceBillingEnabled, "autoRenew": autoRenew, "authorizedBy": authorizedBy, "authorizedAt": authorizedAt, "priceVersion": operation.stringFact("priceVersion"), "currency": pricingCurrency,
		"billingUnit": pricingBillingUnit, "computeUsdMicros": computePrice, "storageUsdMicros": storagePrice, "totalUsdMicros": operation.int64Fact("totalChargeUsdMicros"),
		"periodStart": operation.stringFact("periodStart"), "paidThrough": operation.stringFact("paidThrough"), "nextRenewalAt": paidThrough.Add(-24 * time.Hour).Format(time.RFC3339Nano),
		"billingAnchorDay": operation.intFact("billingAnchorDay"), "renewalStatus": map[bool]string{true: "active", false: workspaceBillingNotApplicable}[resourceBillingEnabled], "computeAllocationId": operation.stringFact("computeAllocationId"),
		"storageId": operation.stringFact("storageId"), "storageGb": operation.intFact("sizeGb"), "activatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	} {
		row[key] = value
	}
	return row, nil
}

// workspaceLaunchActivationRuntimeFactsAllowed applies the domain activation
// guard: a resource-only activation establishes the resource entitlement with
// an empty current application binding and must not carry runtime facts.
func workspaceLaunchActivationRuntimeFactsAllowed(operation workspaceLaunchReconcileOperation) error {
	runtimeFacts := map[string]any{}
	for _, key := range []string{"runtimeId", "runtimeReady", "runtimeServiceName", "runtimeBindingRef"} {
		if _, exists := operation.raw[key]; exists {
			runtimeFacts[key] = operation.raw[key]
		}
	}
	return provisioning.ValidateActivationWrites(operation.provisioningMode(), runtimeFacts)
}

// workspaceLaunchActivationApplicationBinding names the application binding a
// completed activation establishes: the retained fixed OPL App for a full
// Launch, or the explicit empty binding for resource-only provisioning.
func workspaceLaunchActivationApplicationBinding(operation workspaceLaunchReconcileOperation) string {
	if operation.provisioningMode() == contracts.WorkspaceProvisioningResourceOnly {
		return provisioning.ApplicationBindingEmpty
	}
	return provisioning.ApplicationBindingOPLApp
}

// workspaceLaunchActivationRuntimeFactsAllowed applies the domain activation
// guard: a resource-only activation establishes the resource entitlement with
// an empty current application binding and must not carry runtime facts.

func workspaceLaunchRemainingComputeReadBudget(attempt workspaceLaunchStageAttempt) int {
	return min(workspaceLaunchComputeFreshContinuationAdditionalReadBudget,
		workspaceLaunchMaximumPersistedReadbacks(contracts.StageCompute)-attempt.PendingReadbacks)
}

func workspaceLaunchReconcileResultSummary(operation workspaceLaunchReconcileOperation) string {
	return fmt.Sprintf("%s/%s/%s", operation.ID, operation.Status, operation.Stage)
}

func workspaceLaunchProjectionMatches(operation workspaceLaunchReconcileOperation, workspace map[string]any) bool {
	return len(workspaceLaunchProjectionMismatchFields(operation, workspace)) == 0
}

// workspaceLaunchActivationApplicationBinding names the application binding a
// completed activation establishes: the retained fixed OPL App for a full
// Launch, or the explicit empty binding for resource-only provisioning.

func workspaceLaunchProjectionMismatchFields(operation workspaceLaunchReconcileOperation, workspace map[string]any) []string {
	autoRenew := operation.boolFact("autoRenew")
	authorizedBy, authorizedAt := "", ""
	if autoRenew {
		authorizedBy, authorizedAt = operation.stringFact("ownerUserId"), operation.CreatedAt
	}
	mismatches := workspaceLaunchStableProjectionMismatchFields(operation, workspace)
	for _, check := range []struct {
		field string
		match bool
	}{
		{"workspace_api_key_id", int64(numberField(workspace, "workspaceApiKeyId", 0)) == operation.int64Fact("workspaceApiKeyId")},
		{"auto_renew", workspace["autoRenew"] == autoRenew},
		{"authorized_by", stringValue(workspace["authorizedBy"]) == authorizedBy},
		{"authorized_at", stringValue(workspace["authorizedAt"]) == authorizedAt},
	} {
		if !check.match {
			mismatches = append(mismatches, check.field)
		}
	}
	return mismatches
}

func workspaceLaunchStageReadErrorCode(err error) string {
	if err == nil {
		return "none"
	}
	var fabricErr *clients.FabricHTTPError
	if errors.As(err, &fabricErr) {
		var envelope struct {
			Error string `json:"error"`
		}
		if json.Unmarshal([]byte(fabricErr.Body), &envelope) == nil {
			candidate := strings.TrimSpace(strings.SplitN(envelope.Error, ":", 2)[0])
			if workspaceLaunchStageDiagnosticErrorPattern.MatchString(candidate) {
				return candidate
			}
		}
		return fmt.Sprintf("fabric_http_%d", fabricErr.StatusCode)
	}
	for _, candidate := range []struct {
		err  error
		code string
	}{
		{errWorkspaceLaunchStageAdapterUnavailable, "stage_adapter_unavailable"},
		{errInvalidWorkspaceLaunchOperation, "stage_operation_invalid"},
	} {
		if errors.Is(err, candidate.err) {
			return candidate.code
		}
	}
	return "stage_read_failed"
}

var errWorkspaceLaunchStageAdapterUnavailable = errors.New("workspace_launch_stage_adapter_unavailable")
