package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

const workspaceLaunchReconcileSchemaVersion = 3

const workspaceLaunchIdempotentReplayLease = 30 * time.Second
const workspaceLaunchAuthoritativeReadBudget = 3
const workspaceLaunchLegacyV3AuthoritativeReadBudget = 0
const workspaceLaunchFreshContinuationSchemaVersion = 1
const workspaceLaunchFreshContinuationAuthorizationClass = "fresh_typed_pending_system"
const workspaceLaunchFreshContinuationAdditionalReadBudget = 2
const workspaceLaunchFreshContinuationReadClaimLease = 30 * time.Second
const workspaceLaunchComputePendingWindow = 10 * time.Minute
const workspaceLaunchComputeFreshContinuationAdditionalReadBudget = int(workspaceLaunchComputePendingWindow / defaultWorkspaceLaunchInterval)

const (
	workspaceLaunchStageAbsent                      = contracts.StageStateAbsent
	workspaceLaunchStageOwnershipPending            = contracts.StageStateOwnershipPending
	workspaceLaunchStagePending                     = contracts.StageStatePending
	workspaceLaunchStageReady                       = contracts.StageStateReady
	workspaceLaunchStageRuntimeImageRevisionPending = contracts.StageStateRuntimeImageRevisionPending
	workspaceLaunchStageUnknown                     = contracts.StageStateUnknown
)

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

type workspaceLaunchDecodeError struct {
	category     string
	failedFields []string
}

func (err workspaceLaunchDecodeError) Error() string {
	return errInvalidWorkspaceLaunchOperation.Error()
}

func (err workspaceLaunchDecodeError) Unwrap() error {
	return errInvalidWorkspaceLaunchOperation
}

func invalidWorkspaceLaunchDecode(category string) error {
	return workspaceLaunchDecodeError{category: category}
}

func invalidWorkspaceLaunchDecodeFields(category string, failedFields ...string) error {
	return workspaceLaunchDecodeError{category: category, failedFields: append([]string(nil), failedFields...)}
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

type workspaceLaunchCanonicalFactKind string

const (
	workspaceLaunchCanonicalFactString  workspaceLaunchCanonicalFactKind = "string"
	workspaceLaunchCanonicalFactInteger workspaceLaunchCanonicalFactKind = "integer"
	workspaceLaunchCanonicalFactBool    workspaceLaunchCanonicalFactKind = "bool"
	workspaceLaunchCanonicalFactObject  workspaceLaunchCanonicalFactKind = "object"
)

type workspaceLaunchCanonicalFactSpec struct {
	Kind     workspaceLaunchCanonicalFactKind
	Required bool
	Positive bool
	Exact    any
}

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
		"postChargeBalanceUsdMicros": {Kind: workspaceLaunchCanonicalFactInteger, Required: true},
		"postChargeBalanceKnown":     {Kind: workspaceLaunchCanonicalFactBool, Required: true},
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

var errWorkspaceLaunchGrantConflict = errors.New("workspace_launch_resume_authorization_conflict")
var errWorkspaceLaunchMutationNotDispatched = errors.New("workspace_launch_mutation_not_dispatched")

type workspaceLaunchStageAttempt struct {
	Attempted           int    `json:"attempted"`
	Confirmed           int    `json:"confirmed"`
	Unknown             int    `json:"unknown"`
	Max                 int    `json:"max"`
	Status              string `json:"status,omitempty"`
	IdempotencyKey      string `json:"idempotencyKey,omitempty"`
	PendingReadbacks    int    `json:"pendingReadbacks,omitempty"`
	MaxPendingReadbacks int    `json:"maxPendingReadbacks,omitempty"`
	PendingDeadlineAt   string `json:"pendingDeadlineAt,omitempty"`
}

type workspaceLaunchStageObservation struct {
	State      contracts.StageState                    `json:"state"`
	Facts      map[string]any                          `json:"facts,omitempty"`
	Diagnostic *clients.WorkspaceLaunchStageDiagnostic `json:"diagnostic,omitempty"`
}

func workspaceLaunchObservationWithState(observation workspaceLaunchStageObservation, state contracts.StageState) workspaceLaunchStageObservation {
	observation.State = state
	observation.Facts = nil
	return observation
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

type workspaceLaunchReconcileOperation struct {
	ID                              string                                                            `json:"-"`
	Status                          contracts.LaunchStatus                                            `json:"-"`
	CreatedAt                       string                                                            `json:"-"`
	PersistedResult                 string                                                            `json:"-"`
	SchemaVersion                   int                                                               `json:"schemaVersion"`
	Version                         int                                                               `json:"version"`
	Stage                           contracts.Stage                                                   `json:"stage"`
	Attempts                        map[contracts.Stage]workspaceLaunchStageAttempt                   `json:"attempts"`
	Observations                    map[contracts.Stage]workspaceLaunchStageObservation               `json:"observations,omitempty"`
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

type workspaceLaunchReconcileCreate struct {
	OperationID             string
	RequestHash             string
	AccountID               string
	OwnerUserID             string
	Sub2APIUserID           int64
	WorkspaceKeyGroupID     int64
	WorkspaceID             string
	Name                    string
	PackageID               string
	StorageGB               int
	AutoRenew               bool
	PriceVersion            string
	TotalChargeUSDMicros    int64
	ProviderProfileRef      string
	PreflightBindingRef     string
	SpecDigest              string
	WorkspaceImageDigest    string
	PreChargeBalanceMicros  int64
	AcceptanceBCapacitySlot bool
	ResourceBillingEnabled  *bool
	CreatedAt               time.Time
}

type workspaceLaunchReconcileClaim struct {
	AccountID               string
	DesiredOperation        map[string]any
	AcceptanceBCapacitySlot bool
}

type workspaceLaunchReconcileCAS struct {
	OperationID                string
	ExpectedOperationResult    string
	DesiredOperation           map[string]any
	WorkspaceReceiptProjection *workspaceLaunchReceiptProjection
}

type workspaceLaunchReceiptProjection struct {
	AccountID   string
	OwnerUserID string
	WorkspaceID string
	ReceiptID   string
}

type workspaceLaunchCanonicalFactRepairCAS struct {
	OperationID             string
	ExpectedOperationResult string
	DesiredOperation        map[string]any
	AuditEvent              map[string]any
}

type workspaceLaunchReconcileStore interface {
	GetRuntimeOperation(context.Context, string) (map[string]any, bool, error)
	ClaimWorkspaceLaunchReconcile(context.Context, workspaceLaunchReconcileClaim) error
	PersistWorkspaceLaunchReconcile(context.Context, workspaceLaunchReconcileCAS) error
}

type workspaceLaunchStageAdapter interface {
	ReadStage(context.Context, workspaceLaunchReconcileOperation) (workspaceLaunchStageObservation, error)
	CanMutateStage(workspaceLaunchReconcileOperation) bool
	CanReplayStage(workspaceLaunchReconcileOperation) bool
	MutateStage(context.Context, workspaceLaunchReconcileOperation, string) error
}

type WorkspaceLaunchReconciler struct {
	store   workspaceLaunchReconcileStore
	adapter workspaceLaunchStageAdapter
	now     func() time.Time
}

func (r *WorkspaceLaunchReconciler) readStage(ctx context.Context, operation workspaceLaunchReconcileOperation) (workspaceLaunchStageObservation, error) {
	startedAt := time.Now()
	observation, err := r.adapter.ReadStage(ctx, operation)
	outcome, errorCode := "success", "none"
	if err != nil {
		outcome, errorCode = "error", workspaceLaunchStageReadErrorCode(err)
	}
	blockReason := workspaceLaunchObservationBlockReason(observation)
	if err != nil {
		blockReason = errorCode
		slog.ErrorContext(ctx, "workspace launch external call",
			workspaceLaunchLogAttrs(operation.ID, string(operation.Stage), "authoritative_read", false, outcome, string(observation.State), blockReason, errorCode, time.Since(startedAt))...)
	} else {
		slog.InfoContext(ctx, "workspace launch external call",
			workspaceLaunchLogAttrs(operation.ID, string(operation.Stage), "authoritative_read", false, outcome, string(observation.State), blockReason, errorCode, time.Since(startedAt))...)
	}
	return observation, err
}

func (r *WorkspaceLaunchReconciler) mutateStage(ctx context.Context, operation workspaceLaunchReconcileOperation, idempotencyKey string) error {
	startedAt := time.Now()
	err := r.adapter.MutateStage(ctx, operation, idempotencyKey)
	outcome, errorCode := "success", "none"
	if err != nil {
		outcome, errorCode = "error", workspaceLaunchStageReadErrorCode(err)
	}
	if err != nil {
		slog.ErrorContext(ctx, "workspace launch external call",
			workspaceLaunchLogAttrs(operation.ID, string(operation.Stage), "ensure", true, outcome, "not_observed", errorCode, errorCode, time.Since(startedAt))...)
	} else {
		slog.InfoContext(ctx, "workspace launch external call",
			workspaceLaunchLogAttrs(operation.ID, string(operation.Stage), "ensure", true, outcome, "not_observed", "none", errorCode, time.Since(startedAt))...)
	}
	return err
}

func workspaceLaunchLogAttrs(operationID, stage, action string, mutation bool, outcome, state, blockReason, errorCode string, duration time.Duration) []any {
	digest := sha256.Sum256([]byte(operationID))
	return []any{
		"operation_digest", fmt.Sprintf("sha256:%x", digest),
		"stage", stage,
		"action", action,
		"mutation", mutation,
		"outcome", outcome,
		"state", firstNonEmpty(state, "not_observed"),
		"block_reason", firstNonEmpty(blockReason, "none"),
		"error_code", firstNonEmpty(errorCode, "none"),
		"duration_ms", duration.Milliseconds(),
	}
}

func workspaceLaunchObservationBlockReason(observation workspaceLaunchStageObservation) string {
	if observation.Diagnostic != nil && observation.Diagnostic.BlockReason != "" {
		return observation.Diagnostic.BlockReason
	}
	switch observation.State {
	case workspaceLaunchStageReady:
		return "none"
	case workspaceLaunchStageAbsent:
		return "stage_resource_absent"
	case workspaceLaunchStagePending, workspaceLaunchStageOwnershipPending, workspaceLaunchStageRuntimeImageRevisionPending:
		return "stage_provider_pending"
	case workspaceLaunchStageUnknown:
		return "stage_observation_unknown"
	default:
		return "stage_observation_invalid"
	}
}

func NewWorkspaceLaunchReconciler(store workspaceLaunchReconcileStore, adapter workspaceLaunchStageAdapter) *WorkspaceLaunchReconciler {
	return &WorkspaceLaunchReconciler{store: store, adapter: adapter, now: time.Now}
}

func (r *WorkspaceLaunchReconciler) clockNow() time.Time {
	if r != nil && r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}

func (r *WorkspaceLaunchReconciler) Create(ctx context.Context, command workspaceLaunchReconcileCreate) (workspaceLaunchReconcileOperation, error) {
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	if err := r.store.ClaimWorkspaceLaunchReconcile(ctx, workspaceLaunchReconcileClaim{
		AccountID: command.AccountID, DesiredOperation: row, AcceptanceBCapacitySlot: command.AcceptanceBCapacitySlot,
	}); err != nil {
		if !errors.Is(err, errWorkspaceLaunchCASConflict) {
			return workspaceLaunchReconcileOperation{}, err
		}
		current, found, readErr := r.store.GetRuntimeOperation(ctx, command.OperationID)
		if readErr != nil || !found {
			return workspaceLaunchReconcileOperation{}, err
		}
		existing, decodeErr := decodeWorkspaceLaunchReconcileOperation(current)
		if decodeErr != nil || !workspaceLaunchReconcileSubmissionMatches(existing, command) {
			return workspaceLaunchReconcileOperation{}, err
		}
		if existing.Status == contracts.StatusPending {
			return r.Reconcile(ctx, command.OperationID)
		}
		return existing, nil
	}
	return r.Reconcile(ctx, command.OperationID)
}

func (r *WorkspaceLaunchReconciler) Reconcile(ctx context.Context, operationID string) (workspaceLaunchReconcileOperation, error) {
	if r == nil || r.store == nil || r.adapter == nil {
		return workspaceLaunchReconcileOperation{}, errors.New("WorkspaceLaunchReconciler dependencies are required")
	}
	row, found, err := r.store.GetRuntimeOperation(ctx, operationID)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	if !found {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchCASConflict
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	if operation.Status == contracts.StatusManualReview && !operation.boolFact("resourceBillingEnabled") &&
		(operation.Stage == contracts.StageKey || operation.Stage == contracts.StageStorage || operation.Stage == contracts.StageAttachment || operation.Stage == contracts.StageSecret || operation.Stage == contracts.StageRuntime || operation.Stage == contracts.StageActivation) && operation.Observations[operation.Stage].State == workspaceLaunchStageUnknown {
		attempt := operation.Attempts[operation.Stage]
		attempt.Attempted, attempt.Confirmed, attempt.Unknown, attempt.Status, attempt.IdempotencyKey = 0, 0, 0, "", ""
		attempt.PendingReadbacks = 0
		operation.Attempts[operation.Stage] = attempt
		delete(operation.FreshContinuationAuthorizations, operation.Stage)
		for claimID, claim := range operation.ContinuationReadClaims {
			if claim.Stage == operation.Stage {
				delete(operation.ContinuationReadClaims, claimID)
			}
		}
		operation.Status = contracts.StatusPending
		operation, err = r.persist(ctx, operation)
		if err != nil {
			return workspaceLaunchReconcileOperation{}, err
		}
	}
	if operation.Status == contracts.StatusManualReview || terminalWorkspaceLaunchStatus(string(operation.Status)) {
		return operation, nil
	}
	attempt := operation.Attempts[operation.Stage]
	if authorization, ok := operation.activeFreshContinuationAuthorization(); ok {
		return r.continueFreshTypedPending(ctx, operation, attempt, authorization)
	}
	if attempt.Status == "reserved" && operation.Observations[operation.Stage].State == workspaceLaunchStagePending &&
		!workspaceLaunchContinuationReadAuthorized(operation, attempt) {
		operation.Status = contracts.StatusManualReview
		return r.persist(ctx, operation)
	}

	observation, readErr := r.readStage(ctx, operation)
	if readErr != nil {
		observation = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
	}
	switch observation.State {
	case workspaceLaunchStageReady:
		observation, err = reduceWorkspaceLaunchStageObservation(&operation, observation)
		if err != nil {
			return workspaceLaunchReconcileOperation{}, err
		}
		attempt := operation.Attempts[operation.Stage]
		if attempt.Attempted > 0 {
			attempt.Confirmed, attempt.Unknown, attempt.Status = 1, 0, "confirmed"
			operation.Attempts[operation.Stage] = attempt
		}
		operation.Observations[operation.Stage] = observation
		operation.completeIdempotentReplay("succeeded", r.clockNow())
		operation.consumeResumeAuthorization(r.clockNow())
		operation.advance()
		return r.persist(ctx, operation)
	case workspaceLaunchStageUnknown:
		attempt := operation.Attempts[operation.Stage]
		if readErr != nil && attempt.Attempted == 0 && attempt.Status == "" {
			return operation, nil
		}
		if attempt.Status == "reserved" {
			attempt.Unknown = 1
			attempt.Status = "unknown"
			operation.Attempts[operation.Stage] = attempt
		}
		operation.Observations[operation.Stage] = workspaceLaunchObservationWithState(observation, workspaceLaunchStageUnknown)
		operation.completeIdempotentReplay("failed", r.clockNow())
		operation.consumeResumeAuthorization(r.clockNow())
		operation.Status = contracts.StatusManualReview
		return r.persist(ctx, operation)
	case workspaceLaunchStagePending:
		attempt := operation.Attempts[operation.Stage]
		if attempt.Attempted != 1 || attempt.Status != "reserved" || attempt.Confirmed != 0 || attempt.Unknown != 0 ||
			!workspaceLaunchContinuationReadAuthorized(operation, attempt) {
			operation.Observations[operation.Stage] = workspaceLaunchObservationWithState(observation, workspaceLaunchStageUnknown)
			operation.completeIdempotentReplay("failed", r.clockNow())
			operation.consumeResumeAuthorization(r.clockNow())
			operation.Status = contracts.StatusManualReview
			return r.persist(ctx, operation)
		}
		if _, claimed := operation.IdempotentReplayClaims[operation.Stage]; !claimed &&
			workspaceLaunchPostDispatchRuntimeImageRevisionAuthorized(operation, attempt) {
			return r.replayReservedStage(ctx, operation, attempt)
		}
		attempt.PendingReadbacks++
		operation.Attempts[operation.Stage] = attempt
		if attempt.PendingReadbacks >= attempt.MaxPendingReadbacks || workspaceLaunchPendingDeadlineExpired(operation.Stage, attempt, r.clockNow()) {
			attempt.Unknown, attempt.Status = 1, "unknown"
			operation.Attempts[operation.Stage] = attempt
			operation.Observations[operation.Stage] = workspaceLaunchObservationWithState(observation, workspaceLaunchStageUnknown)
			operation.completeIdempotentReplay("failed", r.clockNow())
			operation.consumeResumeAuthorization(r.clockNow())
			operation.Status = contracts.StatusManualReview
			return r.persist(ctx, operation)
		}
		operation.Observations[operation.Stage] = observation
		return r.persist(ctx, operation)
	case workspaceLaunchStageRuntimeImageRevisionPending:
		attempt := operation.Attempts[operation.Stage]
		if !workspaceLaunchRuntimeImageRevisionContinuationAuthorized(operation, attempt) {
			return r.parkClaimedReplay(ctx, operation, observation)
		}
		operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageRuntimeImageRevisionPending}
		if claim, exists := operation.IdempotentReplayClaims[operation.Stage]; exists {
			if claim.AuthorizationID != operation.ResumeAuthorization.AuthorizationID || claim.Stage != operation.Stage ||
				claim.IdempotencyKey != attempt.IdempotencyKey || claim.Status != "waiting" {
				return r.parkClaimedReplay(ctx, operation, observation)
			}
			return r.mutateReservedStage(ctx, operation)
		}
		return r.replayReservedStage(ctx, operation, attempt)
	case workspaceLaunchStageOwnershipPending:
		attempt := operation.Attempts[operation.Stage]
		claim, replayDispatched := operation.IdempotentReplayClaims[operation.Stage]
		if operation.Stage != contracts.StageCompute || attempt.Attempted != 1 || attempt.Status != "reserved" ||
			attempt.Confirmed != 0 || attempt.Unknown != 0 || !workspaceLaunchContinuationReadAuthorized(operation, attempt) {
			operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
			operation.completeIdempotentReplay("failed", r.clockNow())
			operation.consumeResumeAuthorization(r.clockNow())
			operation.Status = contracts.StatusManualReview
			return r.persist(ctx, operation)
		}
		if replayDispatched && claim.Status == "waiting" {
			if attempt.PendingReadbacks < attempt.MaxPendingReadbacks &&
				observation.State == workspaceLaunchStageOwnershipPending && r.adapter.CanReplayStage(operation) {
				return r.mutateReservedStage(ctx, operation)
			}
			attempt.PendingReadbacks++
			operation.Attempts[operation.Stage] = attempt
			if attempt.PendingReadbacks >= attempt.MaxPendingReadbacks || workspaceLaunchPendingDeadlineExpired(operation.Stage, attempt, r.clockNow()) {
				attempt.Unknown, attempt.Status = 1, "unknown"
				operation.Attempts[operation.Stage] = attempt
				operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
				operation.completeIdempotentReplay("failed", r.clockNow())
				operation.consumeResumeAuthorization(r.clockNow())
				operation.Status = contracts.StatusManualReview
				return r.persist(ctx, operation)
			}
			operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageOwnershipPending}
			return r.persist(ctx, operation)
		}
		if !workspaceLaunchIdempotentReplayAuthorized(operation, attempt) {
			operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
			operation.consumeResumeAuthorization(r.clockNow())
			operation.Status = contracts.StatusManualReview
			return r.persist(ctx, operation)
		}
		operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageOwnershipPending}
		return r.replayReservedStage(ctx, operation, attempt)
	case workspaceLaunchStageAbsent:
		if claim, exists := operation.IdempotentReplayClaims[operation.Stage]; exists && claim.Status == "waiting" {
			return r.parkClaimedReplay(ctx, operation, observation)
		}
	default:
		return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
	}

	attempt = operation.Attempts[operation.Stage]
	if attempt.Status == "reserved" || attempt.Attempted >= attempt.Max {
		if workspaceLaunchIdempotentReplayAuthorized(operation, attempt) {
			return r.replayReservedStage(ctx, operation, attempt)
		}
		operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}
		operation.consumeResumeAuthorization(r.clockNow())
		operation.Status = contracts.StatusManualReview
		return r.persist(ctx, operation)
	}
	if !r.adapter.CanMutateStage(operation) {
		return operation, nil
	}
	attempt.Attempted++
	attempt.Status = "reserved"
	attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, attempt.Attempted)
	operation.Attempts[operation.Stage] = attempt
	reserved, err := r.persist(ctx, operation)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	return r.mutateReservedStage(ctx, reserved)
}

func (r *WorkspaceLaunchReconciler) continueFreshTypedPending(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchFreshContinuationAuthorization,
) (workspaceLaunchReconcileOperation, error) {
	now := r.clockNow()
	if replay, ok := operation.IdempotentReplayClaims[operation.Stage]; ok && replay.AuthorizationID == authorization.AuthorizationID && replay.Status == "claimed" {
		lease, err := time.Parse(time.RFC3339Nano, replay.LeaseExpiresAt)
		if err != nil {
			return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
		}
		if lease.After(now) {
			return operation, nil
		}
		if workspaceLaunchPendingDeadlineExpired(operation.Stage, attempt, now) {
			return r.parkFreshTypedPending(ctx, operation, attempt, authorization, "")
		}
		replay.LeaseExpiresAt = now.Add(workspaceLaunchIdempotentReplayLease).Format(time.RFC3339Nano)
		operation.IdempotentReplayClaims[operation.Stage] = replay
		claimed, err := r.persist(ctx, operation)
		if err != nil {
			return workspaceLaunchReconcileOperation{}, err
		}
		return r.convergeFreshComputeReplay(ctx, claimed, authorization)
	}
	for key, claim := range operation.ContinuationReadClaims {
		if claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "claimed" {
			continue
		}
		lease, err := time.Parse(time.RFC3339Nano, claim.LeaseExpiresAt)
		if err != nil {
			return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
		}
		if lease.After(now) {
			return operation, nil
		}
		claim.Status, claim.LeaseExpiresAt, claim.CompletedAt = "expired", "", now.Format(time.RFC3339Nano)
		operation.ContinuationReadClaims[key] = claim
	}
	if attempt.PendingReadbacks >= attempt.MaxPendingReadbacks || workspaceLaunchPendingDeadlineExpired(operation.Stage, attempt, now) {
		return r.parkFreshTypedPending(ctx, operation, attempt, authorization, "")
	}

	attempt.PendingReadbacks++
	claimKey := workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, attempt.PendingReadbacks)
	if _, exists := operation.ContinuationReadClaims[claimKey]; exists {
		return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
	}
	claim := workspaceLaunchContinuationReadClaim{
		SchemaVersion:   workspaceLaunchFreshContinuationSchemaVersion,
		AuthorizationID: authorization.AuthorizationID,
		Stage:           operation.Stage, IdempotencyKey: attempt.IdempotencyKey, Readback: attempt.PendingReadbacks,
		Status: "claimed", LeaseExpiresAt: now.Add(workspaceLaunchFreshContinuationReadClaimLease).Format(time.RFC3339Nano),
	}
	operation.Attempts[operation.Stage] = attempt
	operation.ContinuationReadClaims[claimKey] = claim
	claimed, err := r.persist(ctx, operation)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}

	observation, readErr := r.readStage(ctx, claimed)
	if readErr != nil {
		observation = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
	}
	claim = claimed.ContinuationReadClaims[claimKey]
	claim.LeaseExpiresAt, claim.CompletedAt = "", r.clockNow().Format(time.RFC3339Nano)
	claimed.ContinuationReadClaims[claimKey] = claim
	switch observation.State {
	case workspaceLaunchStageReady:
		reduced, reduceErr := reduceWorkspaceLaunchStageObservation(&claimed, observation)
		if reduceErr != nil {
			return workspaceLaunchReconcileOperation{}, reduceErr
		}
		claim.Status = "ready"
		claimed.ContinuationReadClaims[claimKey] = claim
		attempt = claimed.Attempts[claimed.Stage]
		attempt.Confirmed, attempt.Unknown, attempt.Status = 1, 0, "confirmed"
		claimed.Attempts[claimed.Stage] = attempt
		claimed.Observations[claimed.Stage] = reduced
		claimed.completeIdempotentReplay("succeeded", r.clockNow())
		authorization.Status, authorization.ConsumedAt = "consumed", claim.CompletedAt
		claimed.FreshContinuationAuthorizations[claimed.Stage] = authorization
		claimed.advance()
		return r.persist(ctx, claimed)
	case workspaceLaunchStagePending, workspaceLaunchStageOwnershipPending:
		claim.Status = "pending"
		claimed.ContinuationReadClaims[claimKey] = claim
		claimed.Observations[claimed.Stage] = observation
		attempt = claimed.Attempts[claimed.Stage]
		if attempt.PendingReadbacks >= attempt.MaxPendingReadbacks || workspaceLaunchPendingDeadlineExpired(claimed.Stage, attempt, r.clockNow()) {
			return r.parkFreshTypedPending(ctx, claimed, attempt, authorization, claimKey)
		}
		if observation.State == workspaceLaunchStageOwnershipPending {
			if claimed.Stage != contracts.StageCompute || authorization.IdempotentReplayBudget != 1 || !r.adapter.CanReplayStage(claimed) {
				return r.parkFreshTypedPending(ctx, claimed, attempt, authorization, claimKey)
			}
			if replay, exists := claimed.IdempotentReplayClaims[claimed.Stage]; exists {
				if replay.AuthorizationID != authorization.AuthorizationID || replay.Status != "waiting" {
					return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
				}
				return r.persist(ctx, claimed)
			}
			now = r.clockNow()
			claimed.IdempotentReplayClaims[claimed.Stage] = workspaceLaunchIdempotentReplayClaim{
				AuthorizationID: authorization.AuthorizationID, Stage: claimed.Stage, IdempotencyKey: attempt.IdempotencyKey,
				Status: "claimed", LeaseExpiresAt: now.Add(workspaceLaunchIdempotentReplayLease).Format(time.RFC3339Nano),
			}
			replayClaimed, err := r.persist(ctx, claimed)
			if err != nil {
				return workspaceLaunchReconcileOperation{}, err
			}
			return r.convergeFreshComputeReplay(ctx, replayClaimed, authorization)
		}
		return r.persist(ctx, claimed)
	case workspaceLaunchStageAbsent, workspaceLaunchStageUnknown:
		claim.Status = "failed"
		claimed.ContinuationReadClaims[claimKey] = claim
		claimed.Observations[claimed.Stage] = observation
		return r.parkFreshTypedPending(ctx, claimed, claimed.Attempts[claimed.Stage], authorization, claimKey)
	default:
		return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
	}
}

func (r *WorkspaceLaunchReconciler) convergeFreshComputeReplay(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	authorization workspaceLaunchFreshContinuationAuthorization,
) (workspaceLaunchReconcileOperation, error) {
	observation, readErr := r.readStage(ctx, operation)
	if readErr != nil {
		observation = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
	}
	switch observation.State {
	case workspaceLaunchStageReady:
		return r.completeFreshComputeReplay(ctx, operation, authorization, observation)
	case workspaceLaunchStagePending:
		operation.Observations[operation.Stage] = observation
		return r.persist(ctx, operation)
	case workspaceLaunchStageOwnershipPending:
		mutationErr := r.mutateStage(ctx, operation, operation.Attempts[operation.Stage].IdempotencyKey)
		postRead, postReadErr := r.readStage(ctx, operation)
		if postReadErr != nil {
			postRead = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
		}
		switch postRead.State {
		case workspaceLaunchStageReady:
			return r.completeFreshComputeReplay(ctx, operation, authorization, postRead)
		case workspaceLaunchStagePending, workspaceLaunchStageOwnershipPending:
			return r.waitFreshComputeReplay(ctx, operation, postRead.State)
		default:
			parked, err := r.parkFreshTypedPending(ctx, operation, operation.Attempts[operation.Stage], authorization, "")
			if err != nil {
				return workspaceLaunchReconcileOperation{}, err
			}
			if mutationErr != nil {
				return parked, mutationErr
			}
			return parked, nil
		}
	case workspaceLaunchStageAbsent, workspaceLaunchStageUnknown:
		operation.Observations[operation.Stage] = observation
		return r.parkFreshTypedPending(ctx, operation, operation.Attempts[operation.Stage], authorization, "")
	default:
		return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
	}
}

func (r *WorkspaceLaunchReconciler) completeFreshComputeReplay(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	authorization workspaceLaunchFreshContinuationAuthorization,
	observation workspaceLaunchStageObservation,
) (workspaceLaunchReconcileOperation, error) {
	reduced, err := reduceWorkspaceLaunchStageObservation(&operation, observation)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	now := r.clockNow()
	attempt := operation.Attempts[operation.Stage]
	attempt.Confirmed, attempt.Unknown, attempt.Status = 1, 0, "confirmed"
	operation.Attempts[operation.Stage] = attempt
	operation.Observations[operation.Stage] = reduced
	operation.completeIdempotentReplay("succeeded", now)
	authorization.Status, authorization.ConsumedAt = "consumed", now.Format(time.RFC3339Nano)
	operation.FreshContinuationAuthorizations[operation.Stage] = authorization
	operation.advance()
	return r.persist(ctx, operation)
}

func (r *WorkspaceLaunchReconciler) waitFreshComputeReplay(ctx context.Context, operation workspaceLaunchReconcileOperation, state contracts.StageState) (workspaceLaunchReconcileOperation, error) {
	operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: state}
	operation.completeIdempotentReplay("waiting", r.clockNow())
	return r.persist(ctx, operation)
}

func (r *WorkspaceLaunchReconciler) parkFreshTypedPending(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchFreshContinuationAuthorization,
	claimKey string,
) (workspaceLaunchReconcileOperation, error) {
	if claimKey != "" {
		claim := operation.ContinuationReadClaims[claimKey]
		if claim.Status == "claimed" {
			claim.Status, claim.LeaseExpiresAt, claim.CompletedAt = "failed", "", r.clockNow().Format(time.RFC3339Nano)
			operation.ContinuationReadClaims[claimKey] = claim
		}
	}
	attempt.Unknown, attempt.Status = 1, "unknown"
	operation.Attempts[operation.Stage] = attempt
	operation.Observations[operation.Stage] = workspaceLaunchObservationWithState(operation.Observations[operation.Stage], workspaceLaunchStageUnknown)
	operation.completeIdempotentReplay("failed", r.clockNow())
	authorization.Status, authorization.ConsumedAt = "failed", r.clockNow().Format(time.RFC3339Nano)
	operation.FreshContinuationAuthorizations[operation.Stage] = authorization
	operation.Status = contracts.StatusManualReview
	return r.persist(ctx, operation)
}

func (r *WorkspaceLaunchReconciler) replayReservedStage(ctx context.Context, operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt) (workspaceLaunchReconcileOperation, error) {
	if !r.adapter.CanReplayStage(operation) {
		return operation, nil
	}
	claimed, dispatch, err := r.claimIdempotentReplay(ctx, operation, attempt)
	if err != nil || !dispatch {
		return claimed, err
	}
	observation, readErr := r.readStage(ctx, claimed)
	if readErr != nil {
		observation = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
	}
	if observation.State == workspaceLaunchStageUnknown {
		return r.parkClaimedReplay(ctx, claimed, observation)
	}
	switch observation.State {
	case workspaceLaunchStageReady:
		return r.convergeClaimedReplay(ctx, claimed, observation)
	case workspaceLaunchStagePending:
		if workspaceLaunchPostDispatchRuntimeImageRevisionAuthorized(claimed, claimed.Attempts[claimed.Stage]) {
			return r.mutateReservedStage(ctx, claimed)
		}
		return r.waitClaimedReplay(ctx, claimed, observation)
	case workspaceLaunchStageAbsent, workspaceLaunchStageOwnershipPending, workspaceLaunchStageRuntimeImageRevisionPending:
		return r.mutateReservedStage(ctx, claimed)
	default:
		return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
	}
}

func (r *WorkspaceLaunchReconciler) claimIdempotentReplay(ctx context.Context, operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt) (workspaceLaunchReconcileOperation, bool, error) {
	now := r.clockNow()
	claim, exists := operation.IdempotentReplayClaims[operation.Stage]
	if exists {
		if claim.AuthorizationID != operation.ResumeAuthorization.AuthorizationID || claim.Stage != operation.Stage || claim.IdempotencyKey != attempt.IdempotencyKey || claim.Status != "claimed" {
			return workspaceLaunchReconcileOperation{}, false, errWorkspaceLaunchGrantConflict
		}
		lease, err := time.Parse(time.RFC3339Nano, claim.LeaseExpiresAt)
		if err != nil {
			return workspaceLaunchReconcileOperation{}, false, errInvalidWorkspaceLaunchOperation
		}
		if lease.After(now) {
			return operation, false, nil
		}
	} else {
		claim = workspaceLaunchIdempotentReplayClaim{
			AuthorizationID: operation.ResumeAuthorization.AuthorizationID,
			Stage:           operation.Stage, IdempotencyKey: attempt.IdempotencyKey, Status: "claimed",
		}
	}
	claim.LeaseExpiresAt = now.Add(workspaceLaunchIdempotentReplayLease).Format(time.RFC3339Nano)
	operation.IdempotentReplayClaims[operation.Stage] = claim
	claimed, err := r.persist(ctx, operation)
	return claimed, err == nil, err
}

func (r *WorkspaceLaunchReconciler) convergeClaimedReplay(ctx context.Context, operation workspaceLaunchReconcileOperation, observation workspaceLaunchStageObservation) (workspaceLaunchReconcileOperation, error) {
	reduced, err := reduceWorkspaceLaunchStageObservation(&operation, observation)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	attempt := operation.Attempts[operation.Stage]
	attempt.Confirmed, attempt.Unknown, attempt.Status = 1, 0, "confirmed"
	operation.Attempts[operation.Stage] = attempt
	operation.Observations[operation.Stage] = reduced
	operation.completeIdempotentReplay("succeeded", r.clockNow())
	operation.consumeResumeAuthorization(r.clockNow())
	operation.advance()
	return r.persist(ctx, operation)
}

func (r *WorkspaceLaunchReconciler) waitClaimedReplay(ctx context.Context, operation workspaceLaunchReconcileOperation, observation workspaceLaunchStageObservation) (workspaceLaunchReconcileOperation, error) {
	attempt := operation.Attempts[operation.Stage]
	attempt.PendingReadbacks++
	operation.Attempts[operation.Stage] = attempt
	operation.Observations[operation.Stage] = observation
	operation.completeIdempotentReplay("waiting", r.clockNow())
	if attempt.PendingReadbacks >= attempt.MaxPendingReadbacks || workspaceLaunchPendingDeadlineExpired(operation.Stage, attempt, r.clockNow()) {
		attempt.Unknown, attempt.Status = 1, "unknown"
		operation.Attempts[operation.Stage] = attempt
		operation.Observations[operation.Stage] = workspaceLaunchObservationWithState(observation, workspaceLaunchStageUnknown)
		operation.completeIdempotentReplay("failed", r.clockNow())
		operation.consumeResumeAuthorization(r.clockNow())
		operation.Status = contracts.StatusManualReview
	} else {
		operation.Status = contracts.StatusPending
	}
	return r.persist(ctx, operation)
}

func (r *WorkspaceLaunchReconciler) parkClaimedReplay(ctx context.Context, operation workspaceLaunchReconcileOperation, observation workspaceLaunchStageObservation) (workspaceLaunchReconcileOperation, error) {
	attempt := operation.Attempts[operation.Stage]
	attempt.Unknown, attempt.Status = 1, "unknown"
	operation.Attempts[operation.Stage] = attempt
	operation.Observations[operation.Stage] = workspaceLaunchObservationWithState(observation, workspaceLaunchStageUnknown)
	operation.completeIdempotentReplay("failed", r.clockNow())
	operation.consumeResumeAuthorization(r.clockNow())
	operation.Status = contracts.StatusManualReview
	return r.persist(ctx, operation)
}

func (r *WorkspaceLaunchReconciler) mutateReservedStage(ctx context.Context, reserved workspaceLaunchReconcileOperation) (workspaceLaunchReconcileOperation, error) {
	attempt := reserved.Attempts[reserved.Stage]
	mutationErr := r.mutateStage(ctx, reserved, attempt.IdempotencyKey)
	postRead, postReadErr := r.readStage(ctx, reserved)
	if postReadErr != nil {
		postRead = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
	}
	attempt = reserved.Attempts[reserved.Stage]
	if postRead.State == workspaceLaunchStageReady {
		reduced, err := reduceWorkspaceLaunchStageObservation(&reserved, postRead)
		if err != nil {
			return workspaceLaunchReconcileOperation{}, err
		}
		reserved.Observations[reserved.Stage] = reduced
		attempt.Confirmed, attempt.Unknown, attempt.Status = 1, 0, "confirmed"
		reserved.Attempts[reserved.Stage] = attempt
		reserved.completeIdempotentReplay("succeeded", r.clockNow())
		reserved.consumeResumeAuthorization(r.clockNow())
		reserved.advance()
		return r.persist(ctx, reserved)
	}
	if (postRead.State == workspaceLaunchStagePending || postRead.State == workspaceLaunchStageOwnershipPending || postRead.State == workspaceLaunchStageRuntimeImageRevisionPending) &&
		!errors.Is(mutationErr, errWorkspaceLaunchMutationNotDispatched) {
		if _, replay := reserved.IdempotentReplayClaims[reserved.Stage]; replay {
			return r.waitClaimedReplay(ctx, reserved, postRead)
		}
		attempt.PendingReadbacks = 1
		attempt.MaxPendingReadbacks = 1 + workspaceLaunchFreshContinuationReadBudget(reserved.Stage)
		attempt.PendingDeadlineAt = workspaceLaunchPendingDeadline(reserved.Stage, r.clockNow())
		reserved.Attempts[reserved.Stage] = attempt
		authorization := workspaceLaunchFreshContinuationAuthorization{
			SchemaVersion:      workspaceLaunchFreshContinuationSchemaVersion,
			AuthorizationID:    workspaceLaunchFreshContinuationAuthorizationID(reserved, attempt, reserved.Version+1),
			AuthorizationClass: workspaceLaunchFreshContinuationAuthorizationClass,
			AccountID:          reserved.stringFact("accountId"), OperationID: reserved.ID, WorkspaceID: reserved.stringFact("workspaceId"),
			Stage: reserved.Stage, IdempotencyKey: attempt.IdempotencyKey, Attempt: attempt.Attempted, OperationVersion: reserved.Version + 1,
			MutationBudget: 0, IdempotentReplayBudget: workspaceLaunchFreshContinuationReplayBudget(reserved.Stage),
			AuthoritativeReadBudget: workspaceLaunchFreshContinuationReadBudget(reserved.Stage), ReadbacksAtAuthorization: 1, Status: "active",
		}
		reserved.FreshContinuationAuthorizations[reserved.Stage] = authorization
		reserved.consumeResumeAuthorization(r.clockNow())
		reserved.Observations[reserved.Stage] = postRead
		reserved.Status = contracts.StatusPending
		return r.persist(ctx, reserved)
	}
	reserved.Observations[reserved.Stage] = workspaceLaunchObservationWithState(postRead, workspaceLaunchStageUnknown)
	attempt.Unknown = 1
	attempt.Status = "unknown"
	reserved.Attempts[reserved.Stage] = attempt
	reserved.completeIdempotentReplay("failed", r.clockNow())
	reserved.consumeResumeAuthorization(r.clockNow())
	reserved.Status = contracts.StatusManualReview
	parked, persistErr := r.persist(ctx, reserved)
	if persistErr != nil {
		return workspaceLaunchReconcileOperation{}, persistErr
	}
	if mutationErr != nil {
		return parked, mutationErr
	}
	return parked, nil
}

func workspaceLaunchIdempotentReplayAuthorized(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt) bool {
	authorization := operation.ResumeAuthorization
	return authorization != nil && operation.ResumeAuthorizationConsumedAt == "" && authorization.AuthorizedStage == operation.Stage &&
		authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 1 &&
		authorization.ReadbacksAtAuthorization+authorization.AuthoritativeReadBudget == attempt.MaxPendingReadbacks &&
		attempt.Max == 1 && attempt.Attempted == attempt.Max && attempt.Confirmed == 0 && attempt.Unknown == 0 && attempt.Status == "reserved" &&
		attempt.IdempotencyKey == workspaceLaunchStageIdempotencyKey(operation, 1)
}

func workspaceLaunchContinuationReadAuthorized(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt) bool {
	authorization := operation.ResumeAuthorization
	return authorization != nil && operation.ResumeAuthorizationConsumedAt == "" && authorization.AuthorizedStage == operation.Stage &&
		authorization.MutationBudget == 0 && authorization.AuthoritativeReadBudget > 0 &&
		authorization.ReadbacksAtAuthorization >= 0 && authorization.ReadbacksAtAuthorization <= attempt.PendingReadbacks &&
		authorization.ReadbacksAtAuthorization+authorization.AuthoritativeReadBudget == attempt.MaxPendingReadbacks &&
		attempt.PendingReadbacks < attempt.MaxPendingReadbacks
}

func workspaceLaunchFreshContinuationReadBudget(stage contracts.Stage) int {
	if stage == contracts.StageCompute {
		return workspaceLaunchComputeFreshContinuationAdditionalReadBudget
	}
	return workspaceLaunchFreshContinuationAdditionalReadBudget
}

func workspaceLaunchFreshContinuationReplayBudget(stage contracts.Stage) int {
	if stage == contracts.StageCompute {
		return 1
	}
	return 0
}

func workspaceLaunchMaximumPersistedReadbacks(stage contracts.Stage) int {
	if stage == contracts.StageCompute {
		return workspaceLaunchAuthoritativeReadBudget + workspaceLaunchComputeFreshContinuationAdditionalReadBudget
	}
	if stage == contracts.StageRuntime {
		return 7 * workspaceLaunchAuthoritativeReadBudget
	}
	return workspaceLaunchAuthoritativeReadBudget
}

func workspaceLaunchRemainingComputeReadBudget(attempt workspaceLaunchStageAttempt) int {
	return min(workspaceLaunchComputeFreshContinuationAdditionalReadBudget,
		workspaceLaunchMaximumPersistedReadbacks(contracts.StageCompute)-attempt.PendingReadbacks)
}

func workspaceLaunchPendingDeadline(stage contracts.Stage, now time.Time) string {
	if stage != contracts.StageCompute {
		return ""
	}
	return now.UTC().Add(workspaceLaunchComputePendingWindow).Format(time.RFC3339Nano)
}

func workspaceLaunchPendingDeadlineExpired(stage contracts.Stage, attempt workspaceLaunchStageAttempt, now time.Time) bool {
	if stage != contracts.StageCompute {
		return false
	}
	if attempt.PendingDeadlineAt == "" {
		return false
	}
	deadline, err := time.Parse(time.RFC3339Nano, attempt.PendingDeadlineAt)
	return err != nil || !now.Before(deadline)
}

func workspaceLaunchFreshContinuationAuthorizationID(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, operationVersion int) string {
	return operation.ID + ":" + operation.stringFact("accountId") + ":" + operation.stringFact("workspaceId") + ":" + string(operation.Stage) + ":" +
		attempt.IdempotencyKey + ":fresh-typed-pending:" + strconv.Itoa(attempt.Attempted) + ":version:" + strconv.Itoa(operationVersion)
}

func workspaceLaunchFreshContinuationClaimKey(authorizationID string, readback int) string {
	return authorizationID + ":readback:" + strconv.Itoa(readback)
}

func (operation workspaceLaunchReconcileOperation) activeFreshContinuationAuthorization() (workspaceLaunchFreshContinuationAuthorization, bool) {
	authorization, ok := operation.FreshContinuationAuthorizations[operation.Stage]
	return authorization, ok && authorization.Status == "active"
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

func (r *WorkspaceLaunchReconciler) Resume(ctx context.Context, operationID string, authorization workspaceLaunchResumeAuthorization) (workspaceLaunchReconcileOperation, error) {
	if !validWorkspaceLaunchResumeAuthorization(authorization) {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	row, found, err := r.store.GetRuntimeOperation(ctx, operationID)
	if err != nil || !found {
		if err == nil {
			err = errWorkspaceLaunchCASConflict
		}
		return workspaceLaunchReconcileOperation{}, err
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	if existing, consumed, found := operation.resumeAuthorizationByID(authorization.AuthorizationID); found {
		if existing != authorization || workspaceLaunchResumeAuthorizationDigest(existing) != workspaceLaunchResumeAuthorizationDigest(authorization) {
			return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
		}
		if consumed {
			return operation, nil
		}
		return r.Reconcile(ctx, operationID)
	}
	if operation.ResumeAuthorization != nil && operation.ResumeAuthorizationConsumedAt == "" {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	attempt, ok := operation.Attempts[operation.Stage]
	if !ok {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	remainingBudget := attempt.Max - attempt.Attempted
	if operation.Status != contracts.StatusManualReview || operation.Stage == contracts.StageSucceeded ||
		authorization.LaunchVersion != operation.Version || authorization.AuthorizedStage != operation.Stage {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	if workspaceLaunchUnknownComputeContinuationEligible(operation, attempt, authorization) {
		return r.recoverUnknownComputeStage(ctx, operation, attempt, authorization)
	}
	if workspaceLaunchUnknownStorageRecoveryEligible(operation, attempt, authorization) {
		return r.recoverUnknownStorageStage(ctx, operation, attempt, authorization)
	}
	if workspaceLaunchFailedStorageReplayRecoveryEligible(operation, attempt, authorization) {
		return r.recoverFailedStorageReplayStage(ctx, operation, attempt, authorization)
	}
	if workspaceLaunchExhaustedStorageReadyReadEligible(operation, attempt, authorization) {
		return r.readExhaustedStorageStage(ctx, operation, attempt, authorization)
	}
	if workspaceLaunchExhaustedStorageBindingReplayEligible(operation, attempt, authorization) {
		return r.replayExhaustedStorageBinding(ctx, operation, attempt, authorization)
	}
	if workspaceLaunchRuntimeImageRevisionEligible(operation, attempt, authorization) {
		return r.reviseUnknownRuntimeImage(ctx, operation, attempt, authorization)
	}
	if workspaceLaunchUnknownRuntimeReadEligible(operation, attempt, authorization) {
		return r.recoverUnknownReadyStage(ctx, operation, attempt, authorization)
	}
	if workspaceLaunchUnknownRuntimeReplayEligible(operation, attempt, authorization) {
		return r.recoverUnknownRuntimeAfterAbsence(ctx, operation, attempt, authorization)
	}
	if attempt.Status == "reserved" || attempt.Attempted >= attempt.Max {
		return r.authorizeExhaustedStage(ctx, operation, attempt, authorization)
	}
	if authorization.MutationBudget != remainingBudget || authorization.IdempotentReplayBudget != 0 {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	operation.rotateResumeAuthorization(authorization)
	operation.Status = contracts.StatusPending
	if _, err := r.persist(ctx, operation); err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	return r.Reconcile(ctx, operationID)
}

func workspaceLaunchRuntimeImageRevisionEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	_, hasReplayClaim := operation.IdempotentReplayClaims[operation.Stage]
	failedReplayReauthorization := workspaceLaunchFailedRuntimeReplayImageRevisionEligible(operation, attempt, authorization)
	failedRetainedImageReplayBeforeReadback := failedReplayReauthorization && operation.ResumeAuthorization != nil &&
		operation.ResumeAuthorization.ReplacementWorkspaceImageDigest != authorization.ReplacementWorkspaceImageDigest &&
		attempt.PendingReadbacks == operation.ResumeAuthorization.ReadbacksAtAuthorization &&
		attempt.MaxPendingReadbacks == 5*workspaceLaunchAuthoritativeReadBudget
	postDispatchContinuation := workspaceLaunchPostDispatchRuntimeImageRevisionEligible(operation, attempt, authorization)
	return workspaceLaunchRuntimeRepairEligible(operation) && operation.RuntimeRepair == nil && operation.stringFact("providerProfileRef") == "tencent-tke" &&
		authorization.AuthorizedStage == operation.Stage && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 1 &&
		authorization.AuthoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget && authorization.ReadbacksAtAuthorization == 0 &&
		workspaceImageReferenceWithDigest(authorization.ReplacementWorkspaceImageDigest) &&
		authorization.ReplacementWorkspaceImageDigest != operation.stringFact("workspaceImageDigest") &&
		(attempt.MaxPendingReadbacks == workspaceLaunchAuthoritativeReadBudget || failedReplayReauthorization) &&
		(attempt.PendingReadbacks == 0 || attempt.PendingReadbacks == attempt.MaxPendingReadbacks || postDispatchContinuation || failedRetainedImageReplayBeforeReadback) &&
		(!hasReplayClaim && !operation.idempotentReplayAuthorizationUsed(operation.Stage) || failedReplayReauthorization)
}

func workspaceLaunchFailedRuntimeReplayImageRevisionEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	claim, found := operation.IdempotentReplayClaims[operation.Stage]
	previous := operation.ResumeAuthorization
	if !found || previous == nil || operation.ResumeAuthorizationConsumedAt == "" ||
		claim.AuthorizationID != previous.AuthorizationID || claim.Stage != contracts.StageRuntime ||
		claim.IdempotencyKey != workspaceLaunchStageIdempotencyKey(operationWithStage(operation, contracts.StageRuntime), 1) ||
		claim.Status != "failed" || claim.CompletedAt == "" || previous.AuthorizedStage != contracts.StageRuntime ||
		previous.MutationBudget != 0 || previous.IdempotentReplayBudget != 1 ||
		previous.AuthoritativeReadBudget != workspaceLaunchAuthoritativeReadBudget ||
		previous.ReadbacksAtAuthorization+previous.AuthoritativeReadBudget != attempt.MaxPendingReadbacks {
		return false
	}
	firstImageRevision := previous.ReplacementWorkspaceImageDigest == "" &&
		attempt.MaxPendingReadbacks == workspaceLaunchAuthoritativeReadBudget
	retryUndispatchedImageRevision := previous.ReplacementWorkspaceImageDigest != "" &&
		previous.ReplacementWorkspaceImageDigest == authorization.ReplacementWorkspaceImageDigest &&
		attempt.MaxPendingReadbacks == 2*workspaceLaunchAuthoritativeReadBudget
	continueDispatchedImageRevision := previous.ReplacementWorkspaceImageDigest != "" &&
		previous.ReplacementWorkspaceImageDigest == authorization.ReplacementWorkspaceImageDigest &&
		attempt.MaxPendingReadbacks == 3*workspaceLaunchAuthoritativeReadBudget
	supersedeUnreadyImageRevision := previous.ReplacementWorkspaceImageDigest != "" &&
		previous.ReplacementWorkspaceImageDigest != authorization.ReplacementWorkspaceImageDigest &&
		attempt.MaxPendingReadbacks == 4*workspaceLaunchAuthoritativeReadBudget
	supersedeFailedRetainedImageRevision := previous.ReplacementWorkspaceImageDigest != "" &&
		previous.ReplacementWorkspaceImageDigest != authorization.ReplacementWorkspaceImageDigest &&
		attempt.MaxPendingReadbacks == 5*workspaceLaunchAuthoritativeReadBudget
	supersedeExhaustedFailedRetainedImageRevision := previous.ReplacementWorkspaceImageDigest != "" &&
		previous.ReplacementWorkspaceImageDigest != authorization.ReplacementWorkspaceImageDigest &&
		attempt.MaxPendingReadbacks == 6*workspaceLaunchAuthoritativeReadBudget
	return firstImageRevision || retryUndispatchedImageRevision || continueDispatchedImageRevision ||
		supersedeUnreadyImageRevision || supersedeFailedRetainedImageRevision || supersedeExhaustedFailedRetainedImageRevision
}

func workspaceLaunchPostDispatchRuntimeImageRevisionEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	previous := operation.ResumeAuthorization
	return previous != nil && previous.ReplacementWorkspaceImageDigest != "" &&
		previous.ReplacementWorkspaceImageDigest == authorization.ReplacementWorkspaceImageDigest &&
		previous.ReadbacksAtAuthorization == 2*workspaceLaunchAuthoritativeReadBudget &&
		(attempt.PendingReadbacks == previous.ReadbacksAtAuthorization ||
			attempt.PendingReadbacks == 3*workspaceLaunchAuthoritativeReadBudget) &&
		attempt.MaxPendingReadbacks == 3*workspaceLaunchAuthoritativeReadBudget
}

func workspaceLaunchPostDispatchRuntimeImageRevisionAuthorized(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt) bool {
	authorization := operation.ResumeAuthorization
	return operation.Stage == contracts.StageRuntime && authorization != nil && operation.ResumeAuthorizationConsumedAt == "" &&
		authorization.ReplacementWorkspaceImageDigest != "" && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 1 &&
		authorization.AuthoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget &&
		authorization.ReadbacksAtAuthorization == 3*workspaceLaunchAuthoritativeReadBudget &&
		attempt.PendingReadbacks >= authorization.ReadbacksAtAuthorization &&
		attempt.MaxPendingReadbacks == 4*workspaceLaunchAuthoritativeReadBudget
}

func (r *WorkspaceLaunchReconciler) reviseUnknownRuntimeImage(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchResumeAuthorization,
) (workspaceLaunchReconcileOperation, error) {
	postDispatchContinuation := workspaceLaunchPostDispatchRuntimeImageRevisionEligible(operation, attempt, authorization)
	// Older runtime manual-review rows can retain the configured read budget while
	// reporting zero completed readbacks. Treat that configured budget as the
	// consumed baseline for this explicitly authorized image revision so the
	// replacement receives the same fresh three-read window as newer rows.
	authorization.ReadbacksAtAuthorization = attempt.MaxPendingReadbacks
	attempt.PendingReadbacks = attempt.MaxPendingReadbacks
	observed := operation
	observed.rotateResumeAuthorization(authorization)
	observation, readErr := r.readStage(ctx, observed)
	if readErr != nil || observation.State != workspaceLaunchStageRuntimeImageRevisionPending &&
		(!postDispatchContinuation || observation.State != workspaceLaunchStagePending) {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	if workspaceLaunchFailedRuntimeReplayImageRevisionEligible(operation, attempt, authorization) {
		delete(operation.IdempotentReplayClaims, operation.Stage)
	}
	attempt.Unknown, attempt.Status = 0, "reserved"
	attempt.MaxPendingReadbacks = attempt.PendingReadbacks + authorization.AuthoritativeReadBudget
	operation.Attempts[operation.Stage] = attempt
	operation.rotateResumeAuthorization(authorization)
	operation.Observations[operation.Stage] = observation
	operation.Status = contracts.StatusPending
	if _, err := r.persist(ctx, operation); err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	return r.Reconcile(ctx, operation.ID)
}

func workspaceLaunchRuntimeImageRevisionContinuationAuthorized(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt) bool {
	authorization := operation.ResumeAuthorization
	return operation.Stage == contracts.StageRuntime && authorization != nil && authorization.ReplacementWorkspaceImageDigest != "" &&
		workspaceLaunchContinuationReadAuthorized(operation, attempt) && authorization.IdempotentReplayBudget == 1
}

func workspaceLaunchUnknownComputeContinuationEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	_, hasFreshContinuation := operation.FreshContinuationAuthorizations[operation.Stage]
	_, hasReplayClaim := operation.IdempotentReplayClaims[operation.Stage]
	replayAvailable := !hasReplayClaim && !operation.idempotentReplayAuthorizationUsed(operation.Stage) ||
		workspaceLaunchFailedComputeReplayReauthorizationEligible(operation)
	return operation.Status == contracts.StatusManualReview && operation.boolFact("resourceBillingEnabled") && operation.Stage == contracts.StageCompute &&
		authorization.AuthorizedStage == operation.Stage && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 1 &&
		authorization.AuthoritativeReadBudget == workspaceLaunchRemainingComputeReadBudget(attempt) && authorization.ReadbacksAtAuthorization == 0 &&
		operation.Observations[operation.Stage].State == workspaceLaunchStageUnknown && attempt.Max == 1 && attempt.Attempted == attempt.Max &&
		attempt.Confirmed == 0 && attempt.Unknown == 1 && attempt.Status == "unknown" &&
		attempt.IdempotencyKey == workspaceLaunchStageIdempotencyKey(operation, 1) && !hasFreshContinuation && replayAvailable
}

func workspaceLaunchFailedComputeReplayReauthorizationEligible(operation workspaceLaunchReconcileOperation) bool {
	claim, found := operation.IdempotentReplayClaims[operation.Stage]
	previous := operation.ResumeAuthorization
	attempt, attemptFound := operation.Attempts[operation.Stage]
	return found && attemptFound && previous != nil && operation.ResumeAuthorizationConsumedAt != "" &&
		claim.AuthorizationID == previous.AuthorizationID && claim.Stage == operation.Stage &&
		claim.IdempotencyKey == workspaceLaunchStageIdempotencyKey(operation, 1) && claim.Status == "failed" && claim.CompletedAt != "" &&
		previous.AuthorizedStage == operation.Stage && previous.MutationBudget == 0 && previous.IdempotentReplayBudget == 1 &&
		previous.AuthoritativeReadBudget > 0 && previous.ReadbacksAtAuthorization+previous.AuthoritativeReadBudget == attempt.MaxPendingReadbacks
}

func (r *WorkspaceLaunchReconciler) recoverUnknownComputeStage(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchResumeAuthorization,
) (workspaceLaunchReconcileOperation, error) {
	observation, readErr := r.readStage(ctx, operation)
	if readErr != nil || observation.State != workspaceLaunchStagePending && observation.State != workspaceLaunchStageOwnershipPending {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	if workspaceLaunchFailedComputeReplayReauthorizationEligible(operation) {
		delete(operation.IdempotentReplayClaims, operation.Stage)
	}
	authorization.ReadbacksAtAuthorization = attempt.PendingReadbacks
	attempt.Unknown, attempt.Status = 0, "reserved"
	attempt.MaxPendingReadbacks = attempt.PendingReadbacks + authorization.AuthoritativeReadBudget
	attempt.PendingDeadlineAt = workspaceLaunchPendingDeadline(operation.Stage, r.clockNow())
	operation.Attempts[operation.Stage] = attempt
	operation.rotateResumeAuthorization(authorization)
	operation.Observations[operation.Stage] = observation
	operation.Status = contracts.StatusPending
	if _, err := r.persist(ctx, operation); err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	return r.Reconcile(ctx, operation.ID)
}

func workspaceLaunchReservedStageReplayEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	return authorization.AuthorizedStage == operation.Stage && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 1 &&
		authorization.AuthoritativeReadBudget > 0 && attempt.MaxPendingReadbacks == attempt.PendingReadbacks &&
		attempt.Max == 1 && attempt.Attempted == attempt.Max && attempt.Confirmed == 0 && attempt.Unknown == 0 && attempt.Status == "reserved" &&
		attempt.IdempotencyKey == workspaceLaunchStageIdempotencyKey(operation, 1) && !operation.idempotentReplayAuthorizationUsed(operation.Stage)
}

func workspaceLaunchReservedStageReadEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	return authorization.AuthorizedStage == operation.Stage && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 0 &&
		authorization.AuthoritativeReadBudget > 0 && operation.Observations[operation.Stage].State == workspaceLaunchStagePending &&
		attempt.Max == 1 && attempt.Attempted == attempt.Max && attempt.Confirmed == 0 && attempt.Unknown == 0 && attempt.Status == "reserved" &&
		attempt.IdempotencyKey == workspaceLaunchStageIdempotencyKey(operation, 1)
}

func workspaceLaunchUnknownStorageRecoveryEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	return operation.Status == contracts.StatusManualReview && operation.boolFact("resourceBillingEnabled") && operation.Stage == contracts.StageStorage &&
		authorization.AuthorizedStage == operation.Stage && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 1 &&
		authorization.AuthoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget && authorization.ReadbacksAtAuthorization == 0 &&
		operation.Observations[operation.Stage].State == workspaceLaunchStageUnknown && attempt.Max == 1 && attempt.Attempted == attempt.Max &&
		attempt.Confirmed == 0 && attempt.Unknown == 1 && attempt.Status == "unknown" && attempt.PendingReadbacks == 0 && attempt.MaxPendingReadbacks == 0 &&
		attempt.IdempotencyKey == workspaceLaunchStageIdempotencyKey(operation, 1) && !operation.idempotentReplayAuthorizationUsed(operation.Stage)
}

func (r *WorkspaceLaunchReconciler) recoverUnknownStorageStage(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchResumeAuthorization,
) (workspaceLaunchReconcileOperation, error) {
	observation, readErr := r.readStage(ctx, operation)
	if readErr != nil {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	switch observation.State {
	case workspaceLaunchStageReady:
		reduced, err := reduceWorkspaceLaunchStageObservation(&operation, observation)
		if err != nil {
			return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
		}
		operation.rotateResumeAuthorization(authorization)
		attempt.Confirmed, attempt.Unknown, attempt.Status = 1, 0, "confirmed"
		operation.Attempts[operation.Stage] = attempt
		operation.Observations[operation.Stage] = reduced
		operation.consumeResumeAuthorization(r.clockNow())
		operation.advance()
		return r.persist(ctx, operation)
	case workspaceLaunchStageAbsent, workspaceLaunchStagePending:
		authorization.ReadbacksAtAuthorization = attempt.PendingReadbacks
		attempt.Unknown, attempt.Status = 0, "reserved"
		attempt.MaxPendingReadbacks = attempt.PendingReadbacks + authorization.AuthoritativeReadBudget
		operation.Attempts[operation.Stage] = attempt
		operation.rotateResumeAuthorization(authorization)
		operation.Observations[operation.Stage] = observation
		operation.Status = contracts.StatusPending
		if _, err := r.persist(ctx, operation); err != nil {
			return workspaceLaunchReconcileOperation{}, err
		}
		return r.Reconcile(ctx, operation.ID)
	default:
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
}

func workspaceLaunchFailedStorageReplayRecoveryEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	return workspaceLaunchFailedStorageReplayEvidenceMatches(operation, attempt) &&
		authorization.AuthorizedStage == operation.Stage && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 1 &&
		authorization.AuthoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget && authorization.ReadbacksAtAuthorization == 0 &&
		operation.idempotentReplayAuthorizationCount(operation.Stage) == 1
}

func workspaceLaunchFailedStorageReplayEvidenceMatches(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt) bool {
	claim, hasClaim := operation.IdempotentReplayClaims[operation.Stage]
	previous := operation.ResumeAuthorization
	return operation.Status == contracts.StatusManualReview && operation.boolFact("resourceBillingEnabled") && operation.Stage == contracts.StageStorage &&
		operation.Observations[operation.Stage].State == workspaceLaunchStageUnknown && attempt.Max == 1 && attempt.Attempted == attempt.Max &&
		attempt.Confirmed == 0 && attempt.Unknown == 1 && attempt.Status == "unknown" && attempt.PendingReadbacks >= 0 &&
		attempt.PendingReadbacks <= attempt.MaxPendingReadbacks && attempt.IdempotencyKey == workspaceLaunchStageIdempotencyKey(operation, 1) &&
		hasClaim && previous != nil && claim.AuthorizationID != "" && claim.AuthorizationID == previous.AuthorizationID && claim.Stage == operation.Stage &&
		claim.IdempotencyKey == attempt.IdempotencyKey && claim.Status == "failed" && claim.CompletedAt != "" &&
		operation.ResumeAuthorizationConsumedAt != "" && previous.AuthorizedStage == operation.Stage && previous.MutationBudget == 0 &&
		previous.IdempotentReplayBudget == 1 && previous.AuthoritativeReadBudget > 0 &&
		previous.ReadbacksAtAuthorization+previous.AuthoritativeReadBudget == attempt.MaxPendingReadbacks
}

func (r *WorkspaceLaunchReconciler) recoverFailedStorageReplayStage(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchResumeAuthorization,
) (workspaceLaunchReconcileOperation, error) {
	observation, readErr := r.readStage(ctx, operation)
	if readErr != nil {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	switch observation.State {
	case workspaceLaunchStageReady:
		return r.convergeReadyRecovery(ctx, operation, attempt, authorization, observation)
	case workspaceLaunchStageAbsent:
		return r.continueStorageReplayAfterAbsence(ctx, operation, attempt, authorization)
	default:
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
}

func workspaceLaunchExhaustedStorageReadyReadEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	return workspaceLaunchFailedStorageReplayEvidenceMatches(operation, attempt) &&
		authorization.AuthorizedStage == operation.Stage && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 0 &&
		authorization.AuthoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget && authorization.ReadbacksAtAuthorization == 0 &&
		operation.idempotentReplayAuthorizationCount(operation.Stage) == 2
}

func (r *WorkspaceLaunchReconciler) readExhaustedStorageStage(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchResumeAuthorization,
) (workspaceLaunchReconcileOperation, error) {
	observation, readErr := r.readStage(ctx, operation)
	if readErr != nil || observation.State != workspaceLaunchStageReady {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	return r.convergeReadyRecovery(ctx, operation, attempt, authorization, observation)
}

func workspaceLaunchExhaustedStorageBindingReplayEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	return workspaceLaunchFailedStorageReplayEvidenceMatches(operation, attempt) &&
		authorization.AuthorizedStage == operation.Stage && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 1 &&
		authorization.AuthoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget && authorization.ReadbacksAtAuthorization == 0 &&
		operation.idempotentReplayAuthorizationCount(operation.Stage) >= 2
}

func (r *WorkspaceLaunchReconciler) replayExhaustedStorageBinding(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchResumeAuthorization,
) (workspaceLaunchReconcileOperation, error) {
	observation, readErr := r.readStage(ctx, operation)
	if readErr != nil || observation.State != workspaceLaunchStageAbsent {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	return r.continueStorageReplayAfterAbsence(ctx, operation, attempt, authorization)
}

func (r *WorkspaceLaunchReconciler) continueStorageReplayAfterAbsence(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchResumeAuthorization,
) (workspaceLaunchReconcileOperation, error) {
	delete(operation.IdempotentReplayClaims, operation.Stage)
	authorization.ReadbacksAtAuthorization = attempt.PendingReadbacks
	attempt.Unknown, attempt.Status = 0, "reserved"
	attempt.MaxPendingReadbacks = attempt.PendingReadbacks + authorization.AuthoritativeReadBudget
	operation.Attempts[operation.Stage] = attempt
	operation.rotateResumeAuthorization(authorization)
	operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}
	operation.Status = contracts.StatusPending
	if _, err := r.persist(ctx, operation); err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	return r.Reconcile(ctx, operation.ID)
}

func workspaceLaunchUnknownRuntimeReadEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	return operation.Status == contracts.StatusManualReview && operation.boolFact("resourceBillingEnabled") && operation.Stage == contracts.StageRuntime &&
		authorization.AuthorizedStage == operation.Stage && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 0 &&
		authorization.AuthoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget && authorization.ReadbacksAtAuthorization == 0 &&
		operation.Observations[operation.Stage].State == workspaceLaunchStageUnknown && attempt.Max == 1 && attempt.Attempted == attempt.Max &&
		attempt.Confirmed == 0 && attempt.Unknown == 1 && attempt.Status == "unknown" &&
		attempt.IdempotencyKey == workspaceLaunchStageIdempotencyKey(operation, 1)
}

func workspaceLaunchUnknownRuntimeReplayEligible(operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) bool {
	_, hasFreshContinuation := operation.FreshContinuationAuthorizations[operation.Stage]
	return operation.Status == contracts.StatusManualReview && operation.boolFact("resourceBillingEnabled") && operation.Stage == contracts.StageRuntime &&
		authorization.AuthorizedStage == operation.Stage && authorization.MutationBudget == 0 && authorization.IdempotentReplayBudget == 1 &&
		authorization.AuthoritativeReadBudget == workspaceLaunchAuthoritativeReadBudget && authorization.ReadbacksAtAuthorization == 0 &&
		operation.Observations[operation.Stage].State == workspaceLaunchStageUnknown && attempt.Max == 1 && attempt.Attempted == attempt.Max &&
		attempt.Confirmed == 0 && attempt.Unknown == 1 && attempt.Status == "unknown" && attempt.PendingReadbacks == 0 && attempt.MaxPendingReadbacks == 0 &&
		attempt.IdempotencyKey == workspaceLaunchStageIdempotencyKey(operation, 1) && !hasFreshContinuation &&
		!operation.idempotentReplayAuthorizationUsed(operation.Stage)
}

func (r *WorkspaceLaunchReconciler) recoverUnknownRuntimeAfterAbsence(
	ctx context.Context,
	operation workspaceLaunchReconcileOperation,
	attempt workspaceLaunchStageAttempt,
	authorization workspaceLaunchResumeAuthorization,
) (workspaceLaunchReconcileOperation, error) {
	observation, readErr := r.readStage(ctx, operation)
	if readErr != nil || observation.State != workspaceLaunchStageAbsent {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	authorization.ReadbacksAtAuthorization = attempt.PendingReadbacks
	attempt.Unknown, attempt.Status = 0, "reserved"
	attempt.MaxPendingReadbacks = attempt.PendingReadbacks + authorization.AuthoritativeReadBudget
	operation.Attempts[operation.Stage] = attempt
	operation.rotateResumeAuthorization(authorization)
	operation.Observations[operation.Stage] = observation
	operation.Status = contracts.StatusPending
	if _, err := r.persist(ctx, operation); err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	return r.Reconcile(ctx, operation.ID)
}

func (r *WorkspaceLaunchReconciler) recoverUnknownReadyStage(ctx context.Context, operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) (workspaceLaunchReconcileOperation, error) {
	observation, readErr := r.readStage(ctx, operation)
	if readErr != nil || observation.State != workspaceLaunchStageReady {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	return r.convergeReadyRecovery(ctx, operation, attempt, authorization, observation)
}

func (r *WorkspaceLaunchReconciler) convergeReadyRecovery(ctx context.Context, operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization, observation workspaceLaunchStageObservation) (workspaceLaunchReconcileOperation, error) {
	reduced, err := reduceWorkspaceLaunchStageObservation(&operation, observation)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	now := r.clockNow()
	if continuation, ok := operation.FreshContinuationAuthorizations[operation.Stage]; ok {
		if continuation.Status != "failed" {
			return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
		}
		continuation.Status, continuation.ConsumedAt = "consumed", now.Format(time.RFC3339Nano)
		operation.FreshContinuationAuthorizations[operation.Stage] = continuation
	}
	operation.rotateResumeAuthorization(authorization)
	attempt.Confirmed, attempt.Unknown, attempt.Status = 1, 0, "confirmed"
	operation.Attempts[operation.Stage] = attempt
	operation.Observations[operation.Stage] = reduced
	operation.consumeResumeAuthorization(now)
	operation.advance()
	return r.persist(ctx, operation)
}

func (r *WorkspaceLaunchReconciler) authorizeExhaustedStage(ctx context.Context, operation workspaceLaunchReconcileOperation, attempt workspaceLaunchStageAttempt, authorization workspaceLaunchResumeAuthorization) (workspaceLaunchReconcileOperation, error) {
	if !workspaceLaunchReservedStageReplayEligible(operation, attempt, authorization) &&
		!workspaceLaunchReservedStageReadEligible(operation, attempt, authorization) {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	observation, readErr := r.readStage(ctx, operation)
	if readErr != nil {
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
	switch observation.State {
	case workspaceLaunchStageReady:
		reduced, err := reduceWorkspaceLaunchStageObservation(&operation, observation)
		if err != nil {
			return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
		}
		operation.rotateResumeAuthorization(authorization)
		attempt.Confirmed, attempt.Unknown, attempt.Status = 1, 0, "confirmed"
		operation.Attempts[operation.Stage] = attempt
		operation.Observations[operation.Stage] = reduced
		operation.consumeResumeAuthorization(r.clockNow())
		operation.advance()
		return r.persist(ctx, operation)
	case workspaceLaunchStageAbsent:
		if authorization.IdempotentReplayBudget != 1 {
			return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
		}
		authorization.ReadbacksAtAuthorization = attempt.PendingReadbacks
		attempt.MaxPendingReadbacks = attempt.PendingReadbacks + authorization.AuthoritativeReadBudget
		operation.Attempts[operation.Stage] = attempt
		operation.rotateResumeAuthorization(authorization)
		operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}
		operation.Status = contracts.StatusPending
		if _, err := r.persist(ctx, operation); err != nil {
			return workspaceLaunchReconcileOperation{}, err
		}
		return r.Reconcile(ctx, operation.ID)
	case workspaceLaunchStagePending:
		authorization.ReadbacksAtAuthorization = attempt.PendingReadbacks
		attempt.MaxPendingReadbacks = attempt.PendingReadbacks + authorization.AuthoritativeReadBudget
		operation.Attempts[operation.Stage] = attempt
		operation.rotateResumeAuthorization(authorization)
		operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStagePending}
		operation.Status = contracts.StatusPending
		if _, err := r.persist(ctx, operation); err != nil {
			return workspaceLaunchReconcileOperation{}, err
		}
		return r.Reconcile(ctx, operation.ID)
	default:
		return workspaceLaunchReconcileOperation{}, errWorkspaceLaunchGrantConflict
	}
}

func (r *WorkspaceLaunchReconciler) persist(ctx context.Context, operation workspaceLaunchReconcileOperation) (workspaceLaunchReconcileOperation, error) {
	startedAt := time.Now()
	previousStage := workspaceLaunchPersistedStage(operation)
	action, stage := "persist", operation.Stage
	if previousStage != "" && previousStage != operation.Stage {
		action, stage = "stage_advance", previousStage
	} else if operation.Status == contracts.StatusManualReview {
		action = "manual_review"
	}
	observation := operation.Observations[stage]
	operation.Version++
	desired, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		slog.ErrorContext(ctx, "workspace launch persist",
			workspaceLaunchLogAttrs(operation.ID, string(stage), action, true, "error", string(observation.State), workspaceLaunchObservationBlockReason(observation), "operation_encode_failed", time.Since(startedAt))...)
		return workspaceLaunchReconcileOperation{}, err
	}
	projection, err := workspaceLaunchReceiptProjectionFor(operation)
	if err != nil {
		slog.ErrorContext(ctx, "workspace launch persist",
			workspaceLaunchLogAttrs(operation.ID, string(stage), action, true, "error", string(observation.State), workspaceLaunchObservationBlockReason(observation), "receipt_projection_failed", time.Since(startedAt))...)
		return workspaceLaunchReconcileOperation{}, err
	}
	if err := r.store.PersistWorkspaceLaunchReconcile(ctx, workspaceLaunchReconcileCAS{
		OperationID: operation.ID, ExpectedOperationResult: operation.PersistedResult, DesiredOperation: desired, WorkspaceReceiptProjection: projection,
	}); err != nil {
		errorCode := "persist_failed"
		if errors.Is(err, errWorkspaceLaunchCASConflict) {
			errorCode = "cas_conflict"
		}
		slog.ErrorContext(ctx, "workspace launch persist",
			workspaceLaunchLogAttrs(operation.ID, string(stage), action, true, "error", string(observation.State), workspaceLaunchObservationBlockReason(observation), errorCode, time.Since(startedAt))...)
		return workspaceLaunchReconcileOperation{}, err
	}
	operation.PersistedResult = stringValue(desired["result"])
	slog.InfoContext(ctx, "workspace launch persist",
		workspaceLaunchLogAttrs(operation.ID, string(stage), action, true, "success", string(observation.State), workspaceLaunchObservationBlockReason(observation), "none", time.Since(startedAt))...)
	return operation, nil
}

func workspaceLaunchPersistedStage(operation workspaceLaunchReconcileOperation) contracts.Stage {
	var previous struct {
		Stage contracts.Stage `json:"stage"`
	}
	if json.Unmarshal([]byte(operation.PersistedResult), &previous) != nil {
		return ""
	}
	return previous.Stage
}

func workspaceLaunchReceiptProjectionFor(operation workspaceLaunchReconcileOperation) (*workspaceLaunchReceiptProjection, error) {
	if operation.Stage != contracts.StageSucceeded {
		return nil, nil
	}
	receiptID := operation.stringFact("receiptId")
	if operation.Status != contracts.StatusSucceeded || strings.TrimSpace(operation.stringFact("accountId")) == "" ||
		strings.TrimSpace(operation.stringFact("ownerUserId")) == "" || strings.TrimSpace(operation.stringFact("workspaceId")) == "" || receiptID == "" {
		return nil, errInvalidWorkspaceLaunchOperation
	}
	return &workspaceLaunchReceiptProjection{
		AccountID: operation.stringFact("accountId"), OwnerUserID: operation.stringFact("ownerUserId"),
		WorkspaceID: operation.stringFact("workspaceId"), ReceiptID: receiptID,
	}, nil
}

func newWorkspaceLaunchReconcileOperation(command workspaceLaunchReconcileCreate) (workspaceLaunchReconcileOperation, error) {
	if command.CreatedAt.IsZero() {
		command.CreatedAt = time.Now().UTC()
	}
	if strings.TrimSpace(command.OperationID) == "" || strings.TrimSpace(command.RequestHash) == "" || strings.TrimSpace(command.AccountID) == "" ||
		strings.TrimSpace(command.OwnerUserID) == "" || strings.TrimSpace(command.WorkspaceID) == "" || strings.TrimSpace(command.Name) == "" ||
		command.Sub2APIUserID <= 0 || command.WorkspaceKeyGroupID <= 0 || strings.TrimSpace(command.PackageID) == "" || command.StorageGB <= 0 ||
		strings.TrimSpace(command.PriceVersion) == "" || command.TotalChargeUSDMicros < 0 || command.ResourceBillingEnabled != nil && *command.ResourceBillingEnabled && command.TotalChargeUSDMicros <= 0 || strings.TrimSpace(command.ProviderProfileRef) == "" ||
		strings.TrimSpace(command.PreflightBindingRef) == "" || !workspaceProviderSpecDigestPattern.MatchString(command.SpecDigest) || strings.TrimSpace(command.WorkspaceImageDigest) == "" {
		return workspaceLaunchReconcileOperation{}, errInvalidWorkspaceLaunchOperation
	}
	facts := map[string]any{
		"schemaVersion":             workspaceLaunchReconcileSchemaVersion,
		"version":                   1,
		"stage":                     string(contracts.StageKey),
		"requestHash":               command.RequestHash,
		"accountId":                 command.AccountID,
		"ownerUserId":               command.OwnerUserID,
		"sub2apiUserId":             command.Sub2APIUserID,
		"workspaceKeyGroupId":       command.WorkspaceKeyGroupID,
		"workspaceId":               command.WorkspaceID,
		"name":                      command.Name,
		"packageId":                 command.PackageID,
		"sizeGb":                    command.StorageGB,
		"autoRenew":                 command.AutoRenew,
		"priceVersion":              command.PriceVersion,
		"totalChargeUsdMicros":      command.TotalChargeUSDMicros,
		"providerProfileRef":        command.ProviderProfileRef,
		"preflightBindingRef":       command.PreflightBindingRef,
		"specDigest":                command.SpecDigest,
		"workspaceImageDigest":      command.WorkspaceImageDigest,
		"sub2apiRedeemCode":         monthlyRedeemCode(monthlyEnvironment(), command.OperationID),
		"preChargeBalanceUsdMicros": command.PreChargeBalanceMicros,
		"acceptanceBCapacitySlot":   command.AcceptanceBCapacitySlot,
		"resourceBillingEnabled":    command.ResourceBillingEnabled == nil || *command.ResourceBillingEnabled,
	}
	attempts := make(map[contracts.Stage]workspaceLaunchStageAttempt, len(workspaceLaunchReconcileStages)-1)
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		attempts[stage] = workspaceLaunchStageAttempt{Max: 1, MaxPendingReadbacks: workspaceLaunchLegacyV3AuthoritativeReadBudget}
	}
	facts["attempts"] = attempts
	facts["observations"] = map[contracts.Stage]workspaceLaunchStageObservation{}
	rawBytes, err := json.Marshal(facts)
	if err != nil {
		return workspaceLaunchReconcileOperation{}, err
	}
	row := map[string]any{
		"id": command.OperationID, "operationId": command.OperationID, "accountId": command.AccountID,
		"workspaceId": command.WorkspaceID, "resourceId": command.WorkspaceID, "resourceKind": "workspace_launch",
		"action": workspaceLaunchAction, "status": string(contracts.StatusPending), "result": string(rawBytes),
		"createdAt": command.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	return decodeWorkspaceLaunchReconcileOperation(row)
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
	if json.Unmarshal(raw["stage"], &operation.Stage) != nil || !workspaceLaunchReconcileStageValid(operation.Stage) {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_stage")
	}
	if len(raw["attempts"]) == 0 {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("missing_attempts")
	}
	if json.Unmarshal(raw["attempts"], &operation.Attempts) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecodeFields("invalid_attempts", "attempts_object")
	}
	if len(operation.Attempts) != len(workspaceLaunchReconcileStages)-1 {
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
	if value := raw["consumedResumeAuthorizations"]; len(value) > 0 && json.Unmarshal(value, &operation.ConsumedResumeAuthorizations) != nil {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_resume_authorization")
	}
	for stage, observation := range operation.Observations {
		allowed := workspaceLaunchStageCanonicalFacts[stage]
		if allowed == nil || observation.State != workspaceLaunchStageReady && observation.State != workspaceLaunchStageAbsent && observation.State != workspaceLaunchStagePending &&
			observation.State != workspaceLaunchStageOwnershipPending && observation.State != workspaceLaunchStageRuntimeImageRevisionPending && observation.State != workspaceLaunchStageUnknown ||
			observation.State == workspaceLaunchStageOwnershipPending && stage != contracts.StageCompute ||
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
		expectedReadBudget := workspaceLaunchFreshContinuationReadBudget(stage)
		expectedReplayBudget := workspaceLaunchFreshContinuationReplayBudget(stage)
		runtimeImageRevisionContinuation := stage == contracts.StageRuntime && authorization.Status == "failed" && operation.hasRuntimeImageRevisionAuthorization()
		if !exists || stage != authorization.Stage || !workspaceLaunchReconcileStageValid(stage) || stage == contracts.StageSucceeded ||
			authorization.SchemaVersion != workspaceLaunchFreshContinuationSchemaVersion || authorization.AuthorizationClass != workspaceLaunchFreshContinuationAuthorizationClass ||
			authorization.AuthorizationID != workspaceLaunchFreshContinuationAuthorizationID(boundOperation, attempt, authorization.OperationVersion) ||
			authorization.AccountID != operation.stringFact("accountId") || authorization.OperationID != operation.ID || authorization.WorkspaceID != operation.stringFact("workspaceId") ||
			authorization.IdempotencyKey != workspaceLaunchStageIdempotencyKey(boundOperation, 1) || authorization.Attempt != 1 ||
			authorization.OperationVersion <= 0 || authorization.OperationVersion > operation.Version || authorization.MutationBudget != 0 ||
			authorization.IdempotentReplayBudget != expectedReplayBudget || authorization.AuthoritativeReadBudget != expectedReadBudget ||
			authorization.ReadbacksAtAuthorization != 1 || attempt.Attempted != 1 || attempt.Max != 1 ||
			attempt.MaxPendingReadbacks != 1+expectedReadBudget && !runtimeImageRevisionContinuation || attempt.PendingReadbacks < authorization.ReadbacksAtAuthorization ||
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
				operation.Observations[stage].State != workspaceLaunchStagePending && operation.Observations[stage].State != workspaceLaunchStageOwnershipPending {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
		case "consumed":
			if !validWorkspaceLaunchResumeAuthorizationConsumedAt(authorization.ConsumedAt) || attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("invalid_continuation_claim")
			}
		case "failed":
			originalFailure := operation.Stage == stage &&
				(operation.Status == contracts.StatusManualReview || operation.Status == contracts.StatusFailed && operation.DisposableReset != nil) &&
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
		operation.int64Fact("sub2apiUserId") <= 0 || operation.int64Fact("workspaceKeyGroupId") <= 0 ||
		operation.stringFact("workspaceId") == "" || operation.stringFact("name") == "" || operation.stringFact("packageId") == "" ||
		operation.stringFact("priceVersion") == "" || operation.intFact("sizeGb") <= 0 || operation.int64Fact("totalChargeUsdMicros") < 0 || operation.boolFact("resourceBillingEnabled") && operation.int64Fact("totalChargeUsdMicros") <= 0 ||
		operation.stringFact("providerProfileRef") == "" || operation.stringFact("preflightBindingRef") == "" || !workspaceProviderSpecDigestPattern.MatchString(operation.stringFact("specDigest")) ||
		operation.stringFact("workspaceImageDigest") == "" || operation.stringFact("sub2apiRedeemCode") == "" {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("missing_canonical_facts")
	}
	if stringValue(row["action"]) != "" && stringValue(row["action"]) != workspaceLaunchAction ||
		stringValue(row["accountId"]) != "" && stringValue(row["accountId"]) != operation.stringFact("accountId") ||
		stringValue(row["workspaceId"]) != "" && stringValue(row["workspaceId"]) != operation.stringFact("workspaceId") {
		return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecode("row_identity_mismatch")
	}
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		attempt, exists := operation.Attempts[stage]
		if failedFields := workspaceLaunchAttemptDecodeFailedFields(operation, stage, attempt, exists); len(failedFields) > 0 {
			return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecodeFields("invalid_attempts", failedFields...)
		}
		if attempt.PendingDeadlineAt != "" {
			deadline, err := time.Parse(time.RFC3339Nano, attempt.PendingDeadlineAt)
			if stage != contracts.StageCompute || err != nil || deadline.IsZero() {
				return workspaceLaunchReconcileOperation{}, invalidWorkspaceLaunchDecodeFields("invalid_attempts", string(stage)+"_pending_deadline")
			}
		}
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
		{"runtime_revision_authorization", stage != contracts.StageRuntime || attempt.MaxPendingReadbacks <= workspaceLaunchAuthoritativeReadBudget || operation.hasRuntimeImageRevisionAuthorization()},
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
		"observations": operation.Observations, "consumedResumeAuthorizations": operation.ConsumedResumeAuthorizations, "resumeAuthorization": operation.ResumeAuthorization,
		"resumeAuthorizationConsumedAt": operation.ResumeAuthorizationConsumedAt, "idempotentReplayClaims": operation.IdempotentReplayClaims,
		"freshContinuationAuthorizations": operation.FreshContinuationAuthorizations, "continuationReadClaims": operation.ContinuationReadClaims,
		"runtimeRepair":   operation.RuntimeRepair,
		"disposableReset": operation.DisposableReset,
	} {
		if key == "consumedResumeAuthorizations" && len(operation.ConsumedResumeAuthorizations) == 0 ||
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

func (operation *workspaceLaunchReconcileOperation) advance() {
	index := -1
	for i, stage := range workspaceLaunchReconcileStages {
		if stage == operation.Stage {
			index = i
			break
		}
	}
	if index < 0 || index == len(workspaceLaunchReconcileStages)-1 {
		operation.Stage, operation.Status = contracts.StageSucceeded, contracts.StatusSucceeded
		return
	}
	operation.Stage = workspaceLaunchReconcileStages[index+1]
	if operation.Stage == contracts.StageSucceeded {
		operation.Status = contracts.StatusSucceeded
	} else {
		operation.Status = contracts.StatusPending
	}
}

func (operation *workspaceLaunchReconcileOperation) consumeResumeAuthorization(now time.Time) {
	if operation.ResumeAuthorization != nil && operation.ResumeAuthorizationConsumedAt == "" {
		operation.ResumeAuthorizationConsumedAt = now.UTC().Format(time.RFC3339Nano)
	}
}

func (operation *workspaceLaunchReconcileOperation) rotateResumeAuthorization(authorization workspaceLaunchResumeAuthorization) {
	if operation.ResumeAuthorization != nil {
		operation.ConsumedResumeAuthorizations = append(operation.ConsumedResumeAuthorizations, workspaceLaunchConsumedResumeAuthorization{
			Authorization: *operation.ResumeAuthorization,
			ConsumedAt:    operation.ResumeAuthorizationConsumedAt,
		})
	}
	operation.ResumeAuthorization = &authorization
	operation.ResumeAuthorizationConsumedAt = ""
}

func (operation workspaceLaunchReconcileOperation) idempotentReplayAuthorizationUsed(stage contracts.Stage) bool {
	if _, exists := operation.IdempotentReplayClaims[stage]; exists {
		return true
	}
	if operation.ResumeAuthorization != nil && operation.ResumeAuthorization.AuthorizedStage == stage && operation.ResumeAuthorization.IdempotentReplayBudget == 1 {
		return true
	}
	for _, consumed := range operation.ConsumedResumeAuthorizations {
		if consumed.Authorization.AuthorizedStage == stage && consumed.Authorization.IdempotentReplayBudget == 1 {
			return true
		}
	}
	return false
}

func (operation workspaceLaunchReconcileOperation) idempotentReplayAuthorizationCount(stage contracts.Stage) int {
	count := 0
	if operation.ResumeAuthorization != nil && operation.ResumeAuthorization.AuthorizedStage == stage && operation.ResumeAuthorization.IdempotentReplayBudget == 1 {
		count++
	}
	for _, consumed := range operation.ConsumedResumeAuthorizations {
		if consumed.Authorization.AuthorizedStage == stage && consumed.Authorization.IdempotentReplayBudget == 1 {
			count++
		}
	}
	return count
}

func (operation *workspaceLaunchReconcileOperation) completeIdempotentReplay(status string, now time.Time) {
	claim, exists := operation.IdempotentReplayClaims[operation.Stage]
	if !exists || claim.Status != "claimed" && claim.Status != "waiting" {
		return
	}
	claim.Status, claim.LeaseExpiresAt, claim.CompletedAt = status, "", now.UTC().Format(time.RFC3339Nano)
	operation.IdempotentReplayClaims[operation.Stage] = claim
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

func validWorkspaceLaunchResumeAuthorizationConsumedAt(value string) bool {
	consumedAt, err := time.Parse(time.RFC3339Nano, value)
	return err == nil && !consumedAt.IsZero()
}

func workspaceLaunchReconcileSubmissionMatches(operation workspaceLaunchReconcileOperation, command workspaceLaunchReconcileCreate) bool {
	return operation.ID == command.OperationID && operation.stringFact("requestHash") == command.RequestHash &&
		operation.stringFact("accountId") == command.AccountID && operation.stringFact("ownerUserId") == command.OwnerUserID &&
		operation.int64Fact("sub2apiUserId") == command.Sub2APIUserID && operation.int64Fact("workspaceKeyGroupId") == command.WorkspaceKeyGroupID &&
		operation.stringFact("workspaceId") == command.WorkspaceID
}

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

func workspaceLaunchReconcileIdentityMatches(current, desired map[string]any) bool {
	next, nextErr := decodeWorkspaceLaunchReconcileOperation(desired)
	if nextErr != nil {
		return false
	}
	if existing, existingErr := decodeWorkspaceLaunchReconcileOperation(current); existingErr == nil {
		return existing.ID == next.ID && existing.stringFact("accountId") == next.stringFact("accountId") &&
			existing.stringFact("workspaceId") == next.stringFact("workspaceId") && existing.stringFact("ownerUserId") == next.stringFact("ownerUserId") &&
			existing.stringFact("requestHash") == next.stringFact("requestHash") && next.Version == existing.Version+1
	}
	return false
}

func workspaceLaunchReconcileAcceptanceSlot(row map[string]any) bool {
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	return err == nil && operation.boolFact("acceptanceBCapacitySlot")
}

func workspaceLaunchReconcileResultSummary(operation workspaceLaunchReconcileOperation) string {
	return fmt.Sprintf("%s/%s/%s", operation.ID, operation.Status, operation.Stage)
}
