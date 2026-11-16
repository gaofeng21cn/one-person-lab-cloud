package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/domain"
)

type workspaceLaunchUnitStore struct {
	mu  sync.Mutex
	row map[string]any
}

type workspaceLaunchValidatingUnitStore struct {
	*workspaceLaunchUnitStore
}

func (s *workspaceLaunchValidatingUnitStore) PersistWorkspaceLaunchReconcile(ctx context.Context, update workspaceLaunchReconcileCAS) error {
	current, found, err := s.GetRuntimeOperation(ctx, update.OperationID)
	if err != nil || !found || stringValue(current["result"]) != update.ExpectedOperationResult ||
		!workspaceLaunchReconcileIdentityMatches(current, update.DesiredOperation) {
		return errWorkspaceLaunchCASConflict
	}
	return s.workspaceLaunchUnitStore.PersistWorkspaceLaunchReconcile(ctx, update)
}

type workspaceLaunchReceiptLedgerClient struct {
	fakeLedgerClient
	receipts []clients.Receipt
}

func (c workspaceLaunchReceiptLedgerClient) ListReceipts(_ context.Context, query clients.ReceiptQuery) (clients.ReceiptPage, error) {
	if query.AccountID == "" {
		return clients.ReceiptPage{}, errors.New("account scope required")
	}
	receipts := []clients.Receipt{}
	for _, receipt := range c.receipts {
		if (query.RequestID == "" || receipt.RequestID == query.RequestID) && (query.WorkspaceID == "" || receipt.WorkspaceID == query.WorkspaceID) {
			receipts = append(receipts, receipt)
		}
	}
	return clients.ReceiptPage{Receipts: receipts}, nil
}

func (s *workspaceLaunchUnitStore) GetRuntimeOperation(_ context.Context, id string) (map[string]any, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.row == nil || stringValue(s.row["id"]) != id {
		return nil, false, nil
	}
	return cloneMap(s.row), true, nil
}

func (s *workspaceLaunchUnitStore) PersistWorkspaceLaunchReconcile(_ context.Context, update workspaceLaunchReconcileCAS) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.row == nil || stringValue(s.row["result"]) != update.ExpectedOperationResult {
		return errWorkspaceLaunchCASConflict
	}
	s.row = cloneMap(update.DesiredOperation)
	return nil
}

type workspaceLaunchUnitAdapter struct {
	mu                     sync.Mutex
	readyStages            map[string]bool
	unknownStages          map[string]bool
	stageObservations      map[string]workspaceLaunchStageObservation
	readErrors             map[string]error
	reads                  int
	mutations              int
	mutationsByStage       map[string]int
	mutationOperationID    string
	mutationWorkspaceID    string
	mutationRedeemCode     string
	mutationIdempotencyKey string
	mutationUserID         int64
	mutationAmount         int64
	mutationBlocked        bool
	replayableStages       map[string]bool
	readResultsByStage     map[string][]workspaceLaunchUnitReadResult
	mutationErrors         map[string]error
	panicBeforeMutations   map[string]int
	barrier                chan struct{}
	panicOnReadNumber      int
	blockReadNumber        int
	readStarted            chan struct{}
	releaseRead            chan struct{}
}

type workspaceLaunchUnitReadResult struct {
	observation workspaceLaunchStageObservation
	err         error
}

func (a *workspaceLaunchUnitAdapter) ReadStage(_ context.Context, operation workspaceLaunchReconcileOperation) (workspaceLaunchStageObservation, error) {
	a.mu.Lock()
	a.reads++
	readNumber := a.reads
	if a.panicOnReadNumber == readNumber {
		a.mu.Unlock()
		panic("simulated process crash after durable read claim")
	}
	if a.blockReadNumber == readNumber {
		if a.readStarted != nil {
			close(a.readStarted)
		}
		release := a.releaseRead
		a.mu.Unlock()
		if release != nil {
			<-release
		}
		a.mu.Lock()
	}
	// Both readers must reach the observation before either can advance the
	// operation. This also applies to ready/error observations, not only absent.
	if barrier := a.barrier; barrier != nil {
		if readNumber == 2 {
			close(barrier)
		}
		a.mu.Unlock()
		<-barrier
		a.mu.Lock()
	}
	stage := string(operation.Stage)
	if results := a.readResultsByStage[stage]; len(results) > 0 {
		result := results[0]
		a.readResultsByStage[stage] = results[1:]
		a.mu.Unlock()
		return result.observation, result.err
	}
	if err := a.readErrors[stage]; err != nil {
		a.mu.Unlock()
		return workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err
	}
	if observation, ok := a.stageObservations[stage]; ok {
		a.mu.Unlock()
		return observation, nil
	}
	if a.unknownStages[stage] {
		a.mu.Unlock()
		return workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, nil
	}
	if a.readyStages[stage] {
		a.mu.Unlock()
		return workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts(operation.Stage)}, nil
	}
	a.mu.Unlock()
	return workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}, nil
}

func (a *workspaceLaunchUnitAdapter) ObserveStage(ctx context.Context, operation workspaceLaunchReconcileOperation) (workspaceLaunchStageObservation, error) {
	return a.ReadStage(ctx, operation)
}

func (a *workspaceLaunchUnitAdapter) CanMutateStage(workspaceLaunchReconcileOperation) bool {
	return !a.mutationBlocked
}

func (a *workspaceLaunchUnitAdapter) CanReplayStage(operation workspaceLaunchReconcileOperation) bool {
	return a.replayableStages[string(operation.Stage)] && !a.mutationBlocked
}

func (a *workspaceLaunchUnitAdapter) MutateStage(_ context.Context, operation workspaceLaunchReconcileOperation, idempotencyKey string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	stage := string(operation.Stage)
	if a.panicBeforeMutations[stage] > 0 {
		a.panicBeforeMutations[stage]--
		panic("simulated process crash before transport send")
	}
	a.mutations++
	a.mutationOperationID = operation.ID
	a.mutationWorkspaceID = operation.stringFact("workspaceId")
	a.mutationRedeemCode = operation.stringFact("sub2apiRedeemCode")
	a.mutationIdempotencyKey = idempotencyKey
	a.mutationUserID = operation.int64Fact("sub2apiUserId")
	a.mutationAmount = operation.int64Fact("totalChargeUsdMicros")
	if a.readyStages == nil {
		a.readyStages = map[string]bool{}
	}
	if a.mutationsByStage == nil {
		a.mutationsByStage = map[string]int{}
	}
	a.mutationsByStage[stage]++
	if err := a.mutationErrors[stage]; err != nil {
		return err
	}
	a.readyStages[stage] = true
	return nil
}

func workspaceLaunchReadyFacts(stage contracts.Stage) map[string]any {
	switch stage {
	case contracts.StageKey:
		return map[string]any{"workspaceApiKeyId": int64(9), "workspaceKeyGroupId": int64(7), "workspaceKeyStatus": workspaceKeyCodexGroupBound, "workspaceKeyFingerprint": "sha256:" + strings.Repeat("a", 64)}
	case contracts.StageDebit:
		return map[string]any{"chargeAttempted": true, "chargeConfirmation": map[string]any{"status": "used"}, "preChargeBalanceUsdMicros": int64(100), "postChargeBalanceUsdMicros": int64(50), "postChargeBalanceKnown": true}
	case contracts.StageCompute:
		return map[string]any{"computeAllocationId": "ca-unit", "computeBindingRef": "workspace-launch-unit:ensure_compute_allocation"}
	case contracts.StageStorage:
		return map[string]any{"storageId": "vol-unit", "storageBindingRef": "workspace-launch-unit:storage"}
	case contracts.StageAttachment:
		return map[string]any{"attachmentId": "att-unit", "attachmentBindingRef": "workspace-launch-unit:attachment"}
	case contracts.StageSecret:
		return map[string]any{"gatewaySecretRef": "secret-unit", "gatewaySecretVersion": "v1", "secretBindingRef": "workspace-launch-unit:secret", "workspaceKeyStatus": "configured"}
	case contracts.StageRuntime:
		return map[string]any{"runtimeId": "rt-unit", "runtimeReady": true, "runtimeServiceName": "runtime-unit", "runtimeBindingRef": "workspace-launch-unit:runtime", "url": "https://workspace.example/unit"}
	case contracts.StageActivation:
		return map[string]any{"activationOperationId": "workspace-launch-unit:activation", "workspaceActivatedAt": "2026-08-15T00:00:00Z"}
	case contracts.StageReceipt:
		return map[string]any{"receiptId": "receipt-unit", "receiptOperationId": "workspace-launch-unit:purchase-receipt"}
	default:
		return nil
	}
}

func workspaceLaunchUnitCommand() workspaceLaunchReconcileCreate {
	return workspaceLaunchReconcileCreate{
		OperationID: "workspace-launch-unit", RequestHash: strings.Repeat("a", 64), AccountID: "acct-unit", OwnerUserID: "usr-unit",
		Sub2APIUserID: 11, WorkspaceKeyGroupID: 7, WorkspaceID: "ws-unit", Name: "Unit", PackageID: "basic", StorageGB: 10,
		PriceVersion: pricingCatalogVersion, TotalChargeUSDMicros: 52_580_000, ProviderProfileRef: "profile-unit", PreflightBindingRef: "binding-unit", SpecDigest: strings.Repeat("c", 64),
		WorkspaceImageDigest: "repo.example/workspace@sha256:" + strings.Repeat("b", 64), PreChargeBalanceMicros: 100_000_000,
		CreatedAt: time.Date(2026, 8, 12, 0, 0, 0, 0, time.UTC),
	}
}

func TestWorkspaceLaunchDecoderClassifiesRejectedPersistedState(t *testing.T) {
	valid, err := newWorkspaceLaunchFixtureOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	validRow, err := workspaceLaunchReconcileOperationRow(valid)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		category string
		mutate   func(map[string]any)
	}{
		{name: "invalid json", category: "invalid_json", mutate: func(row map[string]any) { row["result"] = "{" }},
		{name: "schema mismatch", category: "schema_version_mismatch", mutate: func(row map[string]any) {
			result := workspaceLaunchResultMap(t, row)
			result["schemaVersion"] = 2
			row["result"] = string(mustJSON(result))
		}},
		{name: "missing attempts", category: "missing_attempts", mutate: func(row map[string]any) {
			result := workspaceLaunchResultMap(t, row)
			delete(result, "attempts")
			row["result"] = string(mustJSON(result))
		}},
		{name: "forbidden legacy field", category: "forbidden_legacy_fields", mutate: func(row map[string]any) {
			result := workspaceLaunchResultMap(t, row)
			result["phase"] = "debit_pending"
			row["result"] = string(mustJSON(result))
		}},
		{name: "row identity mismatch", category: "row_identity_mismatch", mutate: func(row map[string]any) { row["accountId"] = "acct-other" }},
		{name: "status stage mismatch", category: "status_stage_mismatch", mutate: func(row map[string]any) { row["status"] = "succeeded" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := cloneMap(validRow)
			tc.mutate(row)
			_, decodeErr := decodeWorkspaceLaunchReconcileOperation(row)
			if !errors.Is(decodeErr, errInvalidWorkspaceLaunchOperation) {
				t.Fatalf("decode error = %v", decodeErr)
			}
			if got := workspaceLaunchDecodeFailureCategory(decodeErr); got != tc.category {
				t.Fatalf("category = %q, want %q", got, tc.category)
			}
		})
	}
}

func workspaceLaunchResultMap(t *testing.T, row map[string]any) map[string]any {
	t.Helper()
	var result map[string]any
	if err := json.Unmarshal([]byte(stringValue(row["result"])), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func workspaceLaunchManualReviewRow(t *testing.T) map[string]any {
	t.Helper()
	operation, err := newWorkspaceLaunchFixtureOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.Status = "manual_review"
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchReservedStageManualReviewRow(t *testing.T, stage contracts.Stage) map[string]any {
	t.Helper()
	operation, err := newWorkspaceLaunchFixtureOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.Version = 5
	operation.Stage = stage
	operation.Status = "manual_review"
	attempt := operation.Attempts[stage]
	attempt.Attempted = 1
	attempt.Status = "reserved"
	attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
	operation.Attempts[stage] = attempt
	operation.Observations[stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchReservedStageAuthorization(t *testing.T, row map[string]any, authorizationID string) workspaceLaunchResumeAuthorization {
	t.Helper()
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	return workspaceLaunchResumeAuthorization{
		AuthorizationID: authorizationID, LaunchVersion: operation.Version, AuthorizedStage: operation.Stage, AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-15T01:00:00Z", Reason: "exact reserved stage absent by authoritative readback",
		MutationBudget: 0, IdempotentReplayBudget: 1, AuthoritativeReadBudget: workspaceLaunchAuthoritativeReadBudget,
	}
}

func workspaceLaunchUnknownStageManualReviewRow(t *testing.T, stage contracts.Stage) map[string]any {
	t.Helper()
	row := workspaceLaunchReservedStageManualReviewRow(t, stage)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	attempt := operation.Attempts[stage]
	attempt.Unknown, attempt.Status = 1, "unknown"
	attempt.PendingReadbacks, attempt.MaxPendingReadbacks = workspaceLaunchAuthoritativeReadBudget, workspaceLaunchAuthoritativeReadBudget
	operation.Attempts[stage] = attempt
	operation.Observations[stage] = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchUnknownRuntimeReadAuthorization(t *testing.T, row map[string]any, authorizationID string) workspaceLaunchResumeAuthorization {
	t.Helper()
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	return workspaceLaunchResumeAuthorization{
		AuthorizationID: authorizationID, LaunchVersion: operation.Version, AuthorizedStage: operation.Stage, AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-18T06:00:00Z", Reason: "runtime became ready after the original authoritative read budget expired",
		MutationBudget: 0, IdempotentReplayBudget: 0, AuthoritativeReadBudget: workspaceLaunchAuthoritativeReadBudget,
	}
}

func workspaceLaunchUnknownRuntimeReplayAuthorization(t *testing.T, row map[string]any, authorizationID string) workspaceLaunchResumeAuthorization {
	t.Helper()
	authorization := workspaceLaunchUnknownRuntimeReadAuthorization(t, row, authorizationID)
	authorization.Reason = "authoritative absence permits the original runtime operation to continue"
	authorization.IdempotentReplayBudget = 1
	return authorization
}

func workspaceLaunchUnknownRuntimeAbsentManualReviewRow(t *testing.T) map[string]any {
	t.Helper()
	row := workspaceLaunchUnknownStageManualReviewRow(t, "runtime")
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	attempt := operation.Attempts[operation.Stage]
	attempt.PendingReadbacks, attempt.MaxPendingReadbacks = 0, 0
	operation.Attempts[operation.Stage] = attempt
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchRuntimeImageRevisionManualReviewRow(t *testing.T) map[string]any {
	t.Helper()
	operation := workspaceLaunchRuntimeRepairFixture(t)
	provider, err := json.Marshal("tencent-tke")
	if err != nil {
		t.Fatal(err)
	}
	operation.raw["providerProfileRef"] = provider
	operation.Version = 103
	attempt := operation.Attempts["runtime"]
	attempt.PendingReadbacks, attempt.MaxPendingReadbacks = workspaceLaunchAuthoritativeReadBudget, workspaceLaunchAuthoritativeReadBudget
	operation.Attempts["runtime"] = attempt
	operationVersion := operation.Version - 13
	authorizationID := workspaceLaunchFreshContinuationAuthorizationID(operation, attempt, operationVersion)
	operation.FreshContinuationAuthorizations["runtime"] = workspaceLaunchFreshContinuationAuthorization{
		SchemaVersion: workspaceLaunchFreshContinuationSchemaVersion, AuthorizationID: authorizationID,
		AuthorizationClass: workspaceLaunchFreshContinuationAuthorizationClass,
		AccountID:          operation.stringFact("accountId"), OperationID: operation.ID, WorkspaceID: operation.stringFact("workspaceId"),
		Stage: "runtime", IdempotencyKey: attempt.IdempotencyKey, Attempt: 1, OperationVersion: operationVersion,
		AuthoritativeReadBudget: workspaceLaunchFreshContinuationAdditionalReadBudget, ReadbacksAtAuthorization: 1,
		Status: "failed", ConsumedAt: "2026-08-23T07:58:00Z",
	}
	for readback := 2; readback <= workspaceLaunchAuthoritativeReadBudget; readback++ {
		key := workspaceLaunchFreshContinuationClaimKey(authorizationID, readback)
		operation.ContinuationReadClaims[key] = workspaceLaunchContinuationReadClaim{
			SchemaVersion: workspaceLaunchFreshContinuationSchemaVersion, AuthorizationID: authorizationID,
			Stage: "runtime", IdempotencyKey: attempt.IdempotencyKey, Readback: readback,
			Status: "pending", CompletedAt: "2026-08-23T07:58:00Z",
		}
	}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchRuntimeImageRevisionAuthorization(t *testing.T, row map[string]any, authorizationID string) workspaceLaunchResumeAuthorization {
	t.Helper()
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	return workspaceLaunchResumeAuthorization{
		AuthorizationID: authorizationID, LaunchVersion: operation.Version, AuthorizedStage: "runtime", AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-23T12:00:00Z", Reason: "approved replacement of the exact failed runtime image on the original launch",
		MutationBudget: 0, IdempotentReplayBudget: 1, AuthoritativeReadBudget: workspaceLaunchAuthoritativeReadBudget,
		ReplacementWorkspaceImageDigest: "registry.example/workspace@sha256:" + strings.Repeat("c", 64),
	}
}

func workspaceLaunchRetainedRuntimeImageRevisionManualReviewRow(t *testing.T) map[string]any {
	t.Helper()
	row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previous := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-v111-retained-image")
	previous.LaunchVersion = 111
	previous.ReadbacksAtAuthorization = 3 * workspaceLaunchAuthoritativeReadBudget
	completedAt := "2026-08-24T06:42:32Z"
	attempt := operation.Attempts["runtime"]
	attempt.PendingReadbacks, attempt.MaxPendingReadbacks = 4*workspaceLaunchAuthoritativeReadBudget, 4*workspaceLaunchAuthoritativeReadBudget
	operation.Attempts["runtime"] = attempt
	operation.Version = 116
	operation.ResumeAuthorization = &previous
	operation.ResumeAuthorizationConsumedAt = completedAt
	operation.IdempotentReplayClaims["runtime"] = workspaceLaunchIdempotentReplayClaim{
		AuthorizationID: previous.AuthorizationID, Stage: "runtime", IdempotencyKey: attempt.IdempotencyKey,
		Status: "failed", CompletedAt: completedAt,
	}
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchFailedRetainedRuntimeImageRevisionManualReviewRow(t *testing.T) map[string]any {
	t.Helper()
	row := workspaceLaunchRetainedRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previous := *operation.ResumeAuthorization
	retained := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-v116-failed-retained-image")
	retained.LaunchVersion = 116
	retained.ReadbacksAtAuthorization = 4 * workspaceLaunchAuthoritativeReadBudget
	retained.ReplacementWorkspaceImageDigest = "registry.example/workspace@sha256:" + strings.Repeat("d", 64)
	completedAt := "2026-08-24T09:31:05Z"
	attempt := operation.Attempts["runtime"]
	attempt.PendingReadbacks, attempt.MaxPendingReadbacks = 4*workspaceLaunchAuthoritativeReadBudget, 5*workspaceLaunchAuthoritativeReadBudget
	operation.Attempts["runtime"] = attempt
	operation.Version = 119
	operation.ConsumedResumeAuthorizations = append(operation.ConsumedResumeAuthorizations, workspaceLaunchConsumedResumeAuthorization{
		Authorization: previous, ConsumedAt: operation.ResumeAuthorizationConsumedAt,
	})
	operation.ResumeAuthorization = &retained
	operation.ResumeAuthorizationConsumedAt = completedAt
	operation.IdempotentReplayClaims["runtime"] = workspaceLaunchIdempotentReplayClaim{
		AuthorizationID: retained.AuthorizationID, Stage: "runtime", IdempotencyKey: attempt.IdempotencyKey,
		Status: "failed", CompletedAt: completedAt,
	}
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchUnknownStorageReplayAuthorization(t *testing.T, row map[string]any, authorizationID string) workspaceLaunchResumeAuthorization {
	t.Helper()
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	return workspaceLaunchResumeAuthorization{
		AuthorizationID: authorizationID, LaunchVersion: operation.Version, AuthorizedStage: operation.Stage, AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-23T01:00:00Z", Reason: "authoritative storage readback approved continuation of the original launch",
		MutationBudget: 0, IdempotentReplayBudget: 1, AuthoritativeReadBudget: workspaceLaunchAuthoritativeReadBudget,
	}
}

func workspaceLaunchUnknownStorageManualReviewRow(t *testing.T) map[string]any {
	t.Helper()
	row := workspaceLaunchUnknownStageManualReviewRow(t, "storage")
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	attempt := operation.Attempts[operation.Stage]
	attempt.PendingReadbacks, attempt.MaxPendingReadbacks = 0, 0
	operation.Attempts[operation.Stage] = attempt
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchUnknownStorageAfterFailedReplayRow(t *testing.T) map[string]any {
	t.Helper()
	row := workspaceLaunchUnknownStorageManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previous := workspaceLaunchUnknownStorageReplayAuthorization(t, row, "resume-unknown-storage-failed-replay")
	completedAt := "2026-08-23T01:11:16Z"
	operation.ResumeAuthorization = &previous
	operation.ResumeAuthorizationConsumedAt = completedAt
	attempt := operation.Attempts[operation.Stage]
	attempt.MaxPendingReadbacks = previous.ReadbacksAtAuthorization + previous.AuthoritativeReadBudget
	operation.Attempts[operation.Stage] = attempt
	operation.IdempotentReplayClaims[operation.Stage] = workspaceLaunchIdempotentReplayClaim{
		AuthorizationID: previous.AuthorizationID, Stage: operation.Stage, IdempotencyKey: attempt.IdempotencyKey,
		Status: "failed", CompletedAt: completedAt,
	}
	operation.Version++
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchFailedStorageReplayAuthorization(t *testing.T, row map[string]any, authorizationID string) workspaceLaunchResumeAuthorization {
	t.Helper()
	authorization := workspaceLaunchUnknownStorageReplayAuthorization(t, row, authorizationID)
	authorization.Reason = "authoritative read may confirm or replay the original storage attempt"
	return authorization
}

func workspaceLaunchStorageAfterExhaustedReplayRow(t *testing.T) map[string]any {
	t.Helper()
	row := workspaceLaunchUnknownStorageAfterFailedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	authorization := workspaceLaunchFailedStorageReplayAuthorization(t, row, "resume-failed-storage-second-replay")
	operation.rotateResumeAuthorization(authorization)
	operation.consumeResumeAuthorization(time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC))
	attempt := operation.Attempts[operation.Stage]
	attempt.MaxPendingReadbacks = authorization.ReadbacksAtAuthorization + authorization.AuthoritativeReadBudget
	operation.Attempts[operation.Stage] = attempt
	operation.IdempotentReplayClaims[operation.Stage] = workspaceLaunchIdempotentReplayClaim{
		AuthorizationID: authorization.AuthorizationID, Stage: operation.Stage, IdempotencyKey: attempt.IdempotencyKey,
		Status: "failed", CompletedAt: "2026-08-23T02:00:00Z",
	}
	operation.Version++
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchExhaustedStorageReadAuthorization(t *testing.T, row map[string]any, authorizationID string) workspaceLaunchResumeAuthorization {
	t.Helper()
	authorization := workspaceLaunchFailedStorageReplayAuthorization(t, row, authorizationID)
	authorization.Reason = "authoritative ready read may continue the original exhausted storage stage"
	authorization.IdempotentReplayBudget = 0
	return authorization
}

func workspaceLaunchExhaustedStorageBindingReplayAuthorization(t *testing.T, row map[string]any, authorizationID string) workspaceLaunchResumeAuthorization {
	t.Helper()
	authorization := workspaceLaunchFailedStorageReplayAuthorization(t, row, authorizationID)
	authorization.Reason = "authoritative absence permits the original storage binding to continue"
	return authorization
}

func workspaceLaunchUnknownComputeContinuationAuthorization(t *testing.T, row map[string]any, authorizationID string) workspaceLaunchResumeAuthorization {
	t.Helper()
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	return workspaceLaunchResumeAuthorization{
		AuthorizationID: authorizationID, LaunchVersion: operation.Version, AuthorizedStage: operation.Stage, AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-23T01:00:00Z", Reason: "provider read proves the original compute allocation can continue",
		MutationBudget: 0, IdempotentReplayBudget: 1,
		AuthoritativeReadBudget: workspaceLaunchRemainingComputeReadBudget(operation.Attempts[operation.Stage]),
	}
}

func workspaceLaunchUnknownComputeAfterFailedReplayRow(t *testing.T) map[string]any {
	t.Helper()
	row := workspaceLaunchUnknownStageManualReviewRow(t, "ensure_compute_allocation")
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previous := workspaceLaunchUnknownComputeContinuationAuthorization(t, row, "resume-unknown-compute-failed-original")
	completedAt := "2026-08-23T01:01:00Z"
	operation.ResumeAuthorization = &previous
	operation.ResumeAuthorizationConsumedAt = completedAt
	attempt := operation.Attempts[operation.Stage]
	attempt.MaxPendingReadbacks = previous.ReadbacksAtAuthorization + previous.AuthoritativeReadBudget
	operation.Attempts[operation.Stage] = attempt
	operation.IdempotentReplayClaims[operation.Stage] = workspaceLaunchIdempotentReplayClaim{
		AuthorizationID: previous.AuthorizationID, Stage: operation.Stage,
		IdempotencyKey: operation.Attempts[operation.Stage].IdempotencyKey,
		Status:         "failed", CompletedAt: completedAt,
	}
	operation.Version++
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchUnknownRuntimeWithFailedFreshContinuationRow(t *testing.T) map[string]any {
	t.Helper()
	row := workspaceLaunchUnknownStageManualReviewRow(t, "runtime")
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	attempt := operation.Attempts["runtime"]
	operationVersion := operation.Version - 1
	authorizationID := workspaceLaunchFreshContinuationAuthorizationID(operation, attempt, operationVersion)
	operation.FreshContinuationAuthorizations["runtime"] = workspaceLaunchFreshContinuationAuthorization{
		SchemaVersion: workspaceLaunchFreshContinuationSchemaVersion, AuthorizationID: authorizationID,
		AuthorizationClass: workspaceLaunchFreshContinuationAuthorizationClass,
		AccountID:          operation.stringFact("accountId"), OperationID: operation.ID, WorkspaceID: operation.stringFact("workspaceId"),
		Stage: "runtime", IdempotencyKey: attempt.IdempotencyKey, Attempt: 1, OperationVersion: operationVersion,
		AuthoritativeReadBudget: workspaceLaunchFreshContinuationAdditionalReadBudget, ReadbacksAtAuthorization: 1,
		Status: "failed", ConsumedAt: "2026-08-18T05:00:00Z",
	}
	for readback := 2; readback <= workspaceLaunchAuthoritativeReadBudget; readback++ {
		key := workspaceLaunchFreshContinuationClaimKey(authorizationID, readback)
		operation.ContinuationReadClaims[key] = workspaceLaunchContinuationReadClaim{
			SchemaVersion: workspaceLaunchFreshContinuationSchemaVersion, AuthorizationID: authorizationID,
			Stage: "runtime", IdempotencyKey: attempt.IdempotencyKey, Readback: readback,
			Status: "pending", CompletedAt: "2026-08-18T05:00:00Z",
		}
	}
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	return row
}

func workspaceLaunchAutomaticRuntimeReadyOperation(t *testing.T) workspaceLaunchReconcileOperation {
	t.Helper()
	return workspaceLaunchAutomaticFabricReadyOperation(t, contracts.StageRuntime)
}

func workspaceLaunchAutomaticFabricReadyOperation(t *testing.T, stage contracts.Stage) workspaceLaunchReconcileOperation {
	t.Helper()
	row := workspaceLaunchUnknownStageManualReviewRow(t, stage)
	if stage == contracts.StageRuntime {
		row = workspaceLaunchUnknownRuntimeWithFailedFreshContinuationRow(t)
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := json.Marshal("tencent-tke")
	if err != nil {
		t.Fatal(err)
	}
	operation.raw["providerProfileRef"] = provider
	return operation
}

func workspaceLaunchAutomaticComputeOwnershipOperation(t *testing.T) workspaceLaunchReconcileOperation {
	t.Helper()
	row := workspaceLaunchUnknownStageManualReviewRow(t, "ensure_compute_allocation")
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := json.Marshal("tencent-tke")
	if err != nil {
		t.Fatal(err)
	}
	operation.raw["providerProfileRef"] = provider
	return operation
}

func workspaceLaunchAutomaticComputeOwnershipAfterFailedFreshContinuationOperation(t *testing.T) workspaceLaunchReconcileOperation {
	t.Helper()
	operation := workspaceLaunchAutomaticComputeOwnershipOperation(t)
	attempt := operation.Attempts[contracts.StageCompute]
	attempt.PendingReadbacks = workspaceLaunchAuthoritativeReadBudget
	attempt.MaxPendingReadbacks = 1 + workspaceLaunchFreshContinuationReadBudget(contracts.StageCompute)
	attempt.PendingDeadlineAt = "2026-08-29T04:00:00Z"
	operation.Attempts[contracts.StageCompute] = attempt
	operationVersion := operation.Version - 1
	authorizationID := workspaceLaunchFreshContinuationAuthorizationID(operation, attempt, operationVersion)
	operation.FreshContinuationAuthorizations[contracts.StageCompute] = workspaceLaunchFreshContinuationAuthorization{
		SchemaVersion: workspaceLaunchFreshContinuationSchemaVersion, AuthorizationID: authorizationID,
		AuthorizationClass: workspaceLaunchFreshContinuationAuthorizationClass,
		AccountID:          operation.stringFact("accountId"), OperationID: operation.ID, WorkspaceID: operation.stringFact("workspaceId"),
		Stage: contracts.StageCompute, IdempotencyKey: attempt.IdempotencyKey, Attempt: 1, OperationVersion: operationVersion,
		IdempotentReplayBudget:  workspaceLaunchFreshContinuationReplayBudget(contracts.StageCompute),
		AuthoritativeReadBudget: workspaceLaunchFreshContinuationReadBudget(contracts.StageCompute), ReadbacksAtAuthorization: 1,
		Status: "failed", ConsumedAt: "2026-08-29T04:00:00Z",
	}
	for readback := 2; readback <= attempt.PendingReadbacks; readback++ {
		claimID := workspaceLaunchFreshContinuationClaimKey(authorizationID, readback)
		operation.ContinuationReadClaims[claimID] = workspaceLaunchContinuationReadClaim{
			SchemaVersion: workspaceLaunchFreshContinuationSchemaVersion, AuthorizationID: authorizationID,
			Stage: contracts.StageCompute, IdempotencyKey: attempt.IdempotencyKey, Readback: readback,
			Status: "pending", CompletedAt: "2026-08-29T04:00:00Z",
		}
	}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	return validated
}

func workspaceLaunchAutomaticStorageAbsenceOperation(t *testing.T) workspaceLaunchReconcileOperation {
	t.Helper()
	row := workspaceLaunchUnknownStorageManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	provider, err := json.Marshal("tencent-tke")
	if err != nil {
		t.Fatal(err)
	}
	operation.raw["providerProfileRef"] = provider
	return operation
}

func workspaceLaunchFreshTypedPendingForTest(t *testing.T, stage contracts.Stage) (*workspaceLaunchUnitStore, *workspaceLaunchUnitAdapter, workspaceLaunchReconcileOperation) {
	t.Helper()
	operation, err := newWorkspaceLaunchFixtureOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	// The retained launch execution that wrote this authorization is retired,
	// so the fixture persists the same fresh typed pending authorization the
	// execution produced: the persisted operation version is the version the
	// execution stamped on the authorization, the stage attempt stays reserved
	// with one authoritative readback pending, and the decoder still validates.
	operation.Version, operation.Stage, operation.Status = 5, stage, "pending"
	attempt := operation.Attempts[stage]
	attempt.Attempted = 1
	attempt.Max = 1
	attempt.Status = "reserved"
	attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operationWithStage(operation, stage), 1)
	readBudget := workspaceLaunchFreshContinuationReadBudget(stage)
	attempt.PendingReadbacks = 1
	attempt.MaxPendingReadbacks = 1 + readBudget
	operation.Attempts[stage] = attempt
	operation.FreshContinuationAuthorizations[stage] = workspaceLaunchFreshContinuationAuthorization{
		SchemaVersion:            workspaceLaunchFreshContinuationSchemaVersion,
		AuthorizationID:          workspaceLaunchFreshContinuationAuthorizationID(operation, attempt, operation.Version),
		AuthorizationClass:       workspaceLaunchFreshContinuationAuthorizationClass,
		AccountID:                operation.stringFact("accountId"),
		OperationID:              operation.ID,
		WorkspaceID:              operation.stringFact("workspaceId"),
		Stage:                    stage,
		IdempotencyKey:           attempt.IdempotencyKey,
		Attempt:                  attempt.Attempted,
		OperationVersion:         operation.Version,
		MutationBudget:           0,
		IdempotentReplayBudget:   workspaceLaunchFreshContinuationReplayBudget(stage),
		AuthoritativeReadBudget:  readBudget,
		ReadbacksAtAuthorization: 1,
		Status:                   "active",
	}
	operation.Observations[stage] = workspaceLaunchStageObservation{State: workspaceLaunchStagePending}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{}
	return store, adapter, operation
}

func TestWorkspaceLaunchFreshTypedPendingAuthorizationIdentityDriftIsRejected(t *testing.T) {
	store, _, _ := workspaceLaunchFreshTypedPendingForTest(t, "runtime")
	for _, tc := range []struct {
		name   string
		mutate func(*workspaceLaunchFreshContinuationAuthorization)
	}{
		{name: "authorization class", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.AuthorizationClass = "operator" }},
		{name: "account", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.AccountID += "-drift" }},
		{name: "operation", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.OperationID += "-drift" }},
		{name: "workspace", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.WorkspaceID += "-drift" }},
		{name: "stage", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.Stage = "storage" }},
		{name: "idempotency key", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.IdempotencyKey += "-drift" }},
		{name: "attempt", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.Attempt++ }},
		{name: "version", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.OperationVersion-- }},
		{name: "mutation budget", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.MutationBudget = 1 }},
		{name: "replay budget", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.IdempotentReplayBudget = 1 }},
		{name: "read budget", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.AuthoritativeReadBudget++ }},
		{name: "readback baseline", mutate: func(value *workspaceLaunchFreshContinuationAuthorization) { value.ReadbacksAtAuthorization++ }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operation, err := decodeWorkspaceLaunchReconcileOperation(store.row)
			if err != nil {
				t.Fatal(err)
			}
			authorization := operation.FreshContinuationAuthorizations["runtime"]
			tc.mutate(&authorization)
			operation.FreshContinuationAuthorizations["runtime"] = authorization
			row, err := workspaceLaunchReconcileOperationRow(operation)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeWorkspaceLaunchReconcileOperation(row); !errors.Is(err, errInvalidWorkspaceLaunchOperation) {
				t.Fatalf("authorization drift accepted: authorization=%#v err=%v", authorization, err)
			}
		})
	}
}

func (s *workspaceLaunchActivationCountingStore) ActivateWorkspaceLaunchProjection(ctx context.Context, row map[string]any) (map[string]any, error) {
	s.activationMutations++
	return s.memoryTableStore.ActivateWorkspaceLaunchProjection(ctx, row)
}

func workspaceLaunchUnitActivationProjectionRow(t *testing.T, workspaceID, accountID, ownerID string) map[string]any {
	t.Helper()
	quote, err := workspacePricingPreview(defaultPricingCatalog(), map[string]any{"packageId": "basic", "sizeGb": 10})
	if err != nil {
		t.Fatal(err)
	}
	computePrice, _ := requiredPositiveInteger(mapField(quote, "compute"), "chargeUsdMicros")
	storagePrice, _ := requiredPositiveInteger(mapField(quote, "storage"), "chargeUsdMicros")
	totalPrice, _ := requiredPositiveInteger(quote, "totalChargeUsdMicros")
	row := workspaceProjectionBillingRow(domain.WorkspaceProjection{
		ID: workspaceID, AccountID: accountID, OwnerID: ownerID, Name: "Unit", PackageID: "basic", Provider: "fabric",
		Status: "running", ComputeID: "compute-fabric", VolumeID: "storage-fabric", AttachmentID: "attachment-fabric",
		RuntimeID: "runtime-fabric", RuntimeServiceName: "runtime-service", RuntimeReady: true, URL: "https://workspace.example",
	}, map[string]any{
		"autoRenew": false, "authorizedBy": "", "authorizedAt": "", "packageId": "basic", "storageGb": 10,
		"priceVersion": pricingCatalogVersion, "currency": pricingCurrency, "billingUnit": pricingBillingUnit,
		"computeUsdMicros": computePrice, "storageUsdMicros": storagePrice, "totalUsdMicros": totalPrice,
		"periodStart": "2026-08-12T00:00:00Z", "paidThrough": "2026-09-12T00:00:00Z", "nextRenewalAt": "2026-09-11T00:00:00Z",
		"billingAnchorDay": 12, "renewalStatus": "active", "computeAllocationId": "compute-fabric", "storageId": "storage-fabric",
	})
	row["activatedAt"] = "2026-08-12T00:01:00Z"
	return row
}

func workspaceLaunchCanonicalActivationOperation(t *testing.T) workspaceLaunchReconcileOperation {
	t.Helper()
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID = "workspace-launch-admin", "acct-admin", "usr-admin"
	command.WorkspaceID, command.Sub2APIUserID = "ws-admin", 1
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-3] {
		operation.Stage = stage
		facts := workspaceLaunchReadyFacts(stage)
		switch stage {
		case "debit":
			facts["periodStart"], facts["paidThrough"], facts["billingAnchorDay"] = "2026-08-12T00:00:00Z", "2026-09-12T00:00:00Z", 12
		case "secret":
			facts["credentialStatus"], facts["credentialVersion"], facts["credentialSecretRef"] = "configured", "v1", "secret-unit"
		case "runtime":
			facts["runtimeUsername"] = "opl"
		}
		observation, reduceErr := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: facts})
		if reduceErr != nil {
			t.Fatalf("seed %s: %v", stage, reduceErr)
		}
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Status = 1, 1, "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
		operation.Attempts[stage] = attempt
		operation.Observations[stage] = observation
	}
	operation.Version, operation.Stage, operation.Status = 8, "activation", "pending"
	return operation
}

func TestWorkspaceLaunchReceiptInputUsesCanonicalUnionIdentity(t *testing.T) {
	chargedOperation := workspaceLaunchCanonicalActivationOperation(t)
	zeroCostOperation := workspaceLaunchCanonicalActivationOperation(t)
	zeroCostOperation.raw["resourceBillingEnabled"] = json.RawMessage(`false`)
	zeroCostOperation.raw["totalChargeUsdMicros"] = json.RawMessage(`0`)

	canonicalExecution := func(operation workspaceLaunchReconcileOperation) map[string]any {
		return map[string]any{
			"operationId": operation.ID, "resourceType": "workspace", "resourceId": operation.stringFact("workspaceId"),
			"computeAllocationId": operation.stringFact("computeAllocationId"), "storageId": operation.stringFact("storageId"),
			"attachmentId": operation.stringFact("attachmentId"), "runtimeId": operation.stringFact("runtimeId"),
			"workspaceApiKeyId": operation.int64Fact("workspaceApiKeyId"), "workspaceKeyFingerprint": operation.stringFact("workspaceKeyFingerprint"),
			"runtimeServiceName": operation.stringFact("runtimeServiceName"), "gatewaySecretRef": operation.stringFact("gatewaySecretRef"),
		}
	}
	canonicalOwner := func(operation workspaceLaunchReconcileOperation) map[string]any {
		return map[string]any{"accountId": operation.stringFact("accountId"), "workspaceId": operation.stringFact("workspaceId"), "ownerUserId": operation.stringFact("ownerUserId")}
	}

	charged, err := workspaceLaunchPurchaseReceiptInput(chargedOperation)
	if err != nil {
		t.Fatal(err)
	}
	wantCharged := clients.ReceiptInput{
		Type: "billing.workspace_purchased.v1", Status: "completed", Surface: "control_plane",
		AccountID: chargedOperation.stringFact("accountId"), WorkspaceID: chargedOperation.stringFact("workspaceId"), RequestID: chargedOperation.ID,
		Execution: canonicalExecution(chargedOperation), Owner: canonicalOwner(chargedOperation),
		Cost: map[string]any{
			"priceVersion": chargedOperation.stringFact("priceVersion"), "currency": pricingCurrency, "billingUnit": pricingBillingUnit,
			"totalUsdMicros": chargedOperation.int64Fact("totalChargeUsdMicros"), "sub2apiUserId": chargedOperation.int64Fact("sub2apiUserId"),
			"sub2apiRedeemCode": chargedOperation.stringFact("sub2apiRedeemCode"), "postChargeBalanceUsdMicros": chargedOperation.int64Fact("postChargeBalanceUsdMicros"),
			"periodStart": chargedOperation.stringFact("periodStart"), "paidThrough": chargedOperation.stringFact("paidThrough"),
			"resourceType": "workspace", "resourceId": chargedOperation.stringFact("workspaceId"),
			"components": map[string]any{
				"compute": map[string]any{"resourceType": "compute", "resourceId": chargedOperation.stringFact("computeAllocationId"), "chargeUsdMicros": int64(50_000_000)},
				"storage": map[string]any{"resourceType": "storage", "resourceId": chargedOperation.stringFact("storageId"), "sizeGb": int64(chargedOperation.intFact("sizeGb")), "chargeUsdMicros": int64(2_580_000)},
			},
		},
	}
	if !clients.ReceiptInputEqual(charged, wantCharged) {
		t.Fatalf("charged launch receipt = %#v, want %#v", charged, wantCharged)
	}

	zeroCost, err := workspaceLaunchPurchaseReceiptInput(zeroCostOperation)
	if err != nil {
		t.Fatal(err)
	}
	wantZeroCost := clients.ReceiptInput{
		Type: "workspace.created", Status: "completed", Surface: "control_plane",
		AccountID: zeroCostOperation.stringFact("accountId"), WorkspaceID: zeroCostOperation.stringFact("workspaceId"), RequestID: zeroCostOperation.ID,
		Execution: canonicalExecution(zeroCostOperation), Owner: canonicalOwner(zeroCostOperation),
	}
	if !clients.ReceiptInputEqual(zeroCost, wantZeroCost) {
		t.Fatalf("zero-cost launch receipt = %#v, want %#v", zeroCost, wantZeroCost)
	}
}

func TestWorkspaceLaunchActivationAndReceiptFailClosedOnUnresolvableAcceptedPricing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(workspaceLaunchReconcileOperation)
	}{
		{name: "unknown price version", mutate: func(operation workspaceLaunchReconcileOperation) {
			operation.raw["priceVersion"] = json.RawMessage(`"unknown-price-version"`)
		}},
		{name: "accepted total mismatch", mutate: func(operation workspaceLaunchReconcileOperation) {
			operation.raw["totalChargeUsdMicros"] = json.RawMessage(`52580001`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operation := workspaceLaunchCanonicalActivationOperation(t)
			tc.mutate(operation)
			if row, err := workspaceLaunchActivationRow(operation); err == nil {
				t.Fatalf("activation accepted unresolvable pricing row=%#v", row)
			}
			if receipt, err := workspaceLaunchPurchaseReceiptInput(operation); err == nil {
				t.Fatalf("purchase receipt accepted unresolvable pricing receipt=%#v", receipt)
			}
		})
	}
}

func TestWorkspaceLaunchActivationPreservesAutoRenewAuthorization(t *testing.T) {
	operation := workspaceLaunchCanonicalActivationOperation(t)
	operation.raw["autoRenew"] = json.RawMessage(`true`)
	row, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if row["autoRenew"] != true || row["authorizedBy"] != operation.stringFact("ownerUserId") || row["authorizedAt"] != operation.CreatedAt {
		t.Fatalf("activation renewal intent=%#v operation=%s", row, workspaceLaunchReconcileResultSummary(operation))
	}
	if err := validateWorkspaceBillingState(row); err != nil {
		t.Fatalf("activation billing projection invalid: %v row=%#v", err, row)
	}
	if !workspaceLaunchProjectionMatches(operation, row) {
		t.Fatalf("activation readback did not match durable launch intent: %#v", row)
	}
	for _, field := range []string{"autoRenew", "authorizedBy", "authorizedAt"} {
		drifted := cloneMap(row)
		switch field {
		case "autoRenew":
			drifted[field] = false
		default:
			drifted[field] = "drifted"
		}
		if workspaceLaunchProjectionMatches(operation, drifted) {
			t.Fatalf("%s drift matched durable launch intent: %#v", field, drifted)
		}
	}
}

func TestWorkspaceLaunchActivationRejectsAutoRenewForNonBillingWorkspace(t *testing.T) {
	operation := workspaceLaunchCanonicalActivationOperation(t)
	operation.raw["autoRenew"] = json.RawMessage(`true`)
	operation.raw["resourceBillingEnabled"] = json.RawMessage(`false`)
	operation.raw["totalChargeUsdMicros"] = json.RawMessage(`0`)
	if row, err := workspaceLaunchActivationRow(operation); !errors.Is(err, errInvalidWorkspaceLaunchOperation) {
		t.Fatalf("non-billing auto-renew activation row=%#v err=%v", row, err)
	}
}

func TestWorkspaceLaunchActivationWritesProjectionWithoutFabricRows(t *testing.T) {
	store := newMemoryTableStore()
	store.users["usr-unit"] = map[string]any{"id": "usr-unit", "accountId": "acct-unit", "role": "owner", "status": "active"}
	row := workspaceLaunchUnitActivationProjectionRow(t, "ws-unit", "acct-unit", "usr-unit")
	activated, err := store.ActivateWorkspaceLaunchProjection(context.Background(), row)
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(activated["id"]) != "ws-unit" || activated["customerProduct"] != true || len(store.computes) != 0 || len(store.storages) != 0 || len(store.attachments) != 0 {
		t.Fatalf("activation copied Fabric truth: workspace=%#v computes=%d storages=%d attachments=%d", activated, len(store.computes), len(store.storages), len(store.attachments))
	}
	drifted := cloneMap(row)
	drifted["ownerAccountId"] = "acct-other"
	if _, err := store.ActivateWorkspaceLaunchProjection(context.Background(), drifted); !errors.Is(err, errWorkspaceActivationConflict) {
		t.Fatalf("owner drift error=%v", err)
	}
}

func TestWorkspaceLaunchActivationRejectsNonOwnerIdentities(t *testing.T) {
	for _, tc := range []struct {
		name      string
		accountID string
		ownerID   string
		owner     map[string]any
	}{
		{name: "generic admin", accountID: "acct-generic", ownerID: "usr-generic", owner: map[string]any{"id": "usr-generic", "email": "generic@example.com", "accountId": "acct-generic", "role": "admin", "status": "active"}},
		{name: "wrong id", accountID: "acct-admin", ownerID: "usr-admin", owner: map[string]any{"id": "usr-other", "email": "admin@opl.local", "accountId": "acct-admin", "role": "admin", "status": "active"}},
		{name: "cross account", accountID: "acct-admin", ownerID: "usr-admin", owner: map[string]any{"id": "usr-admin", "email": "admin@opl.local", "accountId": "acct-other", "role": "admin", "status": "active"}},
		{name: "inactive", accountID: "acct-admin", ownerID: "usr-admin", owner: map[string]any{"id": "usr-admin", "email": "admin@opl.local", "accountId": "acct-admin", "role": "admin", "status": "inactive"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := newMemoryTableStore()
			store.users[tc.ownerID] = cloneMap(tc.owner)
			row := workspaceLaunchUnitActivationProjectionRow(t, "ws-rejected", tc.accountID, tc.ownerID)
			if _, err := store.ActivateWorkspaceLaunchProjection(context.Background(), row); !errors.Is(err, errWorkspaceActivationConflict) {
				t.Fatalf("activation error=%v", err)
			}
			if _, found, err := store.GetWorkspace(context.Background(), "ws-rejected"); err != nil || found {
				t.Fatalf("rejected activation persisted workspace found=%v err=%v", found, err)
			}
		})
	}
}

func TestWorkspaceLaunchProjectionMatchesCanonicalCurrentResourceFields(t *testing.T) {
	operation, err := newWorkspaceLaunchFixtureOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	for field, value := range map[string]string{
		"computeAllocationId": "compute-fabric", "storageId": "storage-fabric", "attachmentId": "attachment-fabric",
		"runtimeId": "runtime-fabric", "runtimeServiceName": "runtime-service", "url": "https://workspace.example",
		"runtimeUsername": "opl", "credentialStatus": "configured", "credentialVersion": "v1", "credentialSecretRef": "secret-ref",
		"periodStart": "2026-08-15T00:00:00Z", "paidThrough": "2026-09-15T00:00:00Z",
	} {
		operation.raw[field], _ = json.Marshal(value)
	}
	operation.raw["workspaceApiKeyId"], _ = json.Marshal(int64(9))
	operation.raw["billingAnchorDay"], _ = json.Marshal(15)
	workspace := map[string]any{
		"id": operation.stringFact("workspaceId"), "accountId": operation.stringFact("accountId"), "ownerUserId": operation.stringFact("ownerUserId"),
		"name": operation.stringFact("name"), "packageId": operation.stringFact("packageId"), "url": "https://workspace.example",
		"currentComputeAllocationId": "compute-fabric", "storageId": "storage-fabric", "currentAttachmentId": "attachment-fabric",
		"runtimeId": "runtime-fabric", "runtime": map[string]any{"serviceName": "runtime-service"}, "state": "running",
		"workspaceApiKeyId": int64(9), "access": map[string]any{"username": "opl", "credentialStatus": "configured", "credentialVersion": "v1", "secretRef": "secret-ref"},
		"autoRenew": false, "authorizedBy": "", "authorizedAt": "",
		"priceVersion": operation.stringFact("priceVersion"), "totalUsdMicros": operation.int64Fact("totalChargeUsdMicros"), "storageGb": operation.intFact("sizeGb"),
		"periodStart": "2026-08-15T00:00:00Z", "paidThrough": "2026-09-15T00:00:00Z", "billingAnchorDay": 15,
	}
	if !workspaceLaunchProjectionMatches(operation, workspace) {
		t.Fatalf("canonical PostgreSQL Workspace projection did not match: %#v", workspace)
	}
	for _, drift := range []struct {
		name  string
		apply func(map[string]any)
	}{
		{name: "attachment", apply: func(row map[string]any) { row["currentAttachmentId"] = "attachment-other" }},
		{name: "url", apply: func(row map[string]any) { row["url"] = "https://other.example" }},
		{name: "runtime", apply: func(row map[string]any) { row["runtime"].(map[string]any)["serviceName"] = "runtime-other" }},
		{name: "state", apply: func(row map[string]any) { row["state"] = "provisioning" }},
		{name: "key", apply: func(row map[string]any) { row["workspaceApiKeyId"] = int64(10) }},
		{name: "credential", apply: func(row map[string]any) { row["access"].(map[string]any)["secretRef"] = "secret-other" }},
		{name: "amount", apply: func(row map[string]any) { row["totalUsdMicros"] = operation.int64Fact("totalChargeUsdMicros") + 1 }},
	} {
		drifted := cloneMap(workspace)
		drifted["runtime"] = cloneMap(mapField(workspace, "runtime"))
		drifted["access"] = cloneMap(mapField(workspace, "access"))
		drift.apply(drifted)
		if workspaceLaunchProjectionMatches(operation, drifted) {
			t.Fatalf("%s drift matched: %#v", drift.name, drifted)
		}
		if fields := workspaceLaunchProjectionMismatchFields(operation, drifted); len(fields) != 1 {
			t.Fatalf("%s drift fields=%#v", drift.name, fields)
		}
	}
}

func TestCurrentWorkspaceImageDigestRequiresRepository(t *testing.T) {
	digest := "sha256:" + strings.Repeat("a", 64)
	t.Setenv("OPL_WORKSPACE_IMAGE", "@"+digest)
	if got := currentWorkspaceImageDigest(); got != "" {
		t.Fatalf("empty repository image=%q", got)
	}
	t.Setenv("OPL_WORKSPACE_IMAGE", "registry.example/workspace@"+digest)
	if got := currentWorkspaceImageDigest(); got != "registry.example/workspace@"+digest {
		t.Fatalf("valid image=%q", got)
	}
}
