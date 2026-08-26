package server

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strconv"
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
	return clients.ReceiptPage{Receipts: c.receipts}, nil
}

func (s *workspaceLaunchUnitStore) GetRuntimeOperation(_ context.Context, id string) (map[string]any, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.row == nil || stringValue(s.row["id"]) != id {
		return nil, false, nil
	}
	return cloneMap(s.row), true, nil
}

func (s *workspaceLaunchUnitStore) ClaimWorkspaceLaunchReconcile(_ context.Context, claim workspaceLaunchReconcileClaim) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.row != nil {
		return errWorkspaceLaunchCASConflict
	}
	s.row = cloneMap(claim.DesiredOperation)
	return nil
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
	if a.barrier != nil && a.reads == 2 {
		close(a.barrier)
	}
	barrier := a.barrier
	a.mu.Unlock()
	if barrier != nil {
		<-barrier
	}
	return workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}, nil
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
	valid, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
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
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
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
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
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

func TestWorkspaceLaunchReservedStageReplayMatrix(t *testing.T) {
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		t.Run(string(stage)+"/absent replays one logical claim", func(t *testing.T) {
			row := workspaceLaunchReservedStageManualReviewRow(t, stage)
			authorization := workspaceLaunchReservedStageAuthorization(t, row, "resume-"+string(stage))
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{string(stage): true}}
			got, err := NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
			if err != nil {
				t.Fatal(err)
			}
			attempt := got.Attempts[stage]
			if attempt.Attempted != 1 || attempt.Max != 1 || attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" ||
				adapter.mutationsByStage[string(stage)] != 1 || adapter.mutationIdempotencyKey != attempt.IdempotencyKey ||
				got.IdempotentReplayClaims[stage].AuthorizationID != authorization.AuthorizationID {
				t.Fatalf("stage replay changed budget or identity: operation=%s attempt=%#v claims=%#v mutations=%#v err=%v", workspaceLaunchReconcileResultSummary(got), attempt, got.IdempotentReplayClaims, adapter.mutationsByStage, err)
			}
		})

		t.Run(string(stage)+"/ready converges read only", func(t *testing.T) {
			row := workspaceLaunchReservedStageManualReviewRow(t, stage)
			authorization := workspaceLaunchReservedStageAuthorization(t, row, "resume-ready-"+string(stage))
			adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{string(stage): true}, replayableStages: map[string]bool{string(stage): true}}
			got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
			if err != nil || got.Attempts[stage].Confirmed != 1 || adapter.mutations != 0 {
				t.Fatalf("ready stage did not converge read-only: operation=%s mutations=%d err=%v", workspaceLaunchReconcileResultSummary(got), adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchUnknownRuntimeRecoveryConvergesReadyReadOnly(t *testing.T) {
	row := workspaceLaunchUnknownStageManualReviewRow(t, "runtime")
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{"runtime": true}, replayableStages: map[string]bool{"runtime": true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	authorization := workspaceLaunchUnknownRuntimeReadAuthorization(t, row, "resume-unknown-runtime-ready")

	got, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
	if err != nil || got.Status != "pending" || got.Stage != "activation" || got.Attempts["runtime"].Confirmed != 1 ||
		got.Attempts["runtime"].Unknown != 0 || got.Attempts["runtime"].Status != "confirmed" ||
		got.ResumeAuthorization == nil || got.ResumeAuthorizationConsumedAt == "" || adapter.reads != 1 || adapter.mutations != 0 {
		t.Fatalf("unknown Runtime did not converge read-only: operation=%s reads=%d mutations=%d err=%v", workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutations, err)
	}

	readsBefore, persistedBefore := adapter.reads, stringValue(store.row["result"])
	replayed, err := reconciler.Resume(context.Background(), got.ID, authorization)
	if err != nil || replayed.Version != got.Version || adapter.reads != readsBefore || adapter.mutations != 0 || stringValue(store.row["result"]) != persistedBefore {
		t.Fatalf("consumed Runtime recovery repeated work: operation=%s reads=%d/%d mutations=%d err=%v", workspaceLaunchReconcileResultSummary(replayed), adapter.reads, readsBefore, adapter.mutations, err)
	}
}

func TestWorkspaceLaunchUnknownRuntimeAuthoritativeAbsenceReplaysOriginalKey(t *testing.T) {
	row := workspaceLaunchUnknownRuntimeAbsentManualReviewRow(t)
	before, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{"runtime": true}}
	authorization := workspaceLaunchUnknownRuntimeReplayAuthorization(t, row, "resume-unknown-runtime-absent")

	got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), before.ID, authorization)
	attempt := got.Attempts["runtime"]
	claim := got.IdempotentReplayClaims["runtime"]
	if err != nil || got.Status != "pending" || got.Stage != "activation" || attempt.Attempted != 1 || attempt.Max != 1 ||
		attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" ||
		attempt.IdempotencyKey != before.Attempts["runtime"].IdempotencyKey || adapter.mutationsByStage["runtime"] != 1 ||
		adapter.mutationOperationID != before.ID || adapter.mutationIdempotencyKey != attempt.IdempotencyKey || adapter.reads != 4 ||
		claim.AuthorizationID != authorization.AuthorizationID || claim.IdempotencyKey != attempt.IdempotencyKey || claim.Status != "succeeded" ||
		got.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("absent Runtime did not replay the original operation: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionContinuesOriginalRuntimeStage(t *testing.T) {
	row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
	before, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	revisionPending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageRuntimeImageRevisionPending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"runtime": {revisionPending, revisionPending, revisionPending}},
		replayableStages:   map[string]bool{"runtime": true},
	}
	authorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-runtime-image-revision")

	got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), before.ID, authorization)
	attempt := got.Attempts["runtime"]
	claim := got.IdempotentReplayClaims["runtime"]
	if err != nil || got.Status != "pending" || got.Stage != "activation" || attempt.Attempted != 1 || attempt.Max != 1 ||
		attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" ||
		attempt.IdempotencyKey != before.Attempts["runtime"].IdempotencyKey || adapter.mutationsByStage["runtime"] != 1 ||
		adapter.mutationOperationID != before.ID || adapter.mutationIdempotencyKey != before.Attempts["runtime"].IdempotencyKey ||
		claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "succeeded" ||
		got.ResumeAuthorization == nil || got.ResumeAuthorization.ReplacementWorkspaceImageDigest != authorization.ReplacementWorkspaceImageDigest ||
		got.ResumeAuthorizationConsumedAt == "" || got.RuntimeRepair != nil {
		t.Fatalf("Runtime image revision forked the Launch or failed to continue it: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionAcceptsInitialUnknownWithConfiguredReadBudget(t *testing.T) {
	row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	// The production v103 row was parked immediately after the first runtime
	// read failed: the configured three-read window exists, but no readback was
	// completed and no fresh continuation authorization was persisted.
	attempt := operation.Attempts["runtime"]
	attempt.PendingReadbacks = 0
	operation.Attempts["runtime"] = attempt
	operation.FreshContinuationAuthorizations = map[contracts.Stage]workspaceLaunchFreshContinuationAuthorization{}
	operation.ContinuationReadClaims = map[string]workspaceLaunchContinuationReadClaim{}
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	before, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	read := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageRuntimeImageRevisionPending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"runtime": {read, read, read}},
		replayableStages:   map[string]bool{"runtime": true},
	}
	authorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-runtime-image-revision-initial-unknown")

	got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), before.ID, authorization)
	attempt = got.Attempts["runtime"]
	if err != nil || got.Status != "pending" || got.Stage != "activation" || attempt.Confirmed != 1 || attempt.Unknown != 0 ||
		attempt.Status != "confirmed" || attempt.PendingReadbacks != 3 || attempt.MaxPendingReadbacks != 6 ||
		adapter.mutationsByStage["runtime"] != 1 || got.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("initial unknown runtime did not use the qualified image revision path: operation=%s attempt=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionReauthorizesMatchingFailedReplay(t *testing.T) {
	row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	attempt := operation.Attempts["runtime"]
	attempt.PendingReadbacks = 0
	operation.Attempts["runtime"] = attempt
	operation.FreshContinuationAuthorizations = map[contracts.Stage]workspaceLaunchFreshContinuationAuthorization{}
	operation.ContinuationReadClaims = map[string]workspaceLaunchContinuationReadClaim{}
	previous := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-runtime-failed-original", LaunchVersion: 100, AuthorizedStage: "runtime", AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-23T08:00:00Z", Reason: "continue the original runtime after authoritative readback",
		MutationBudget: 0, IdempotentReplayBudget: 1, AuthoritativeReadBudget: workspaceLaunchAuthoritativeReadBudget,
	}
	completedAt := "2026-08-23T08:01:00Z"
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
	read := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageRuntimeImageRevisionPending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"runtime": {read, read, read}},
		replayableStages:   map[string]bool{"runtime": true},
	}
	authorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-runtime-image-after-failed-replay")

	got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), operation.ID, authorization)
	attempt = got.Attempts["runtime"]
	claim := got.IdempotentReplayClaims["runtime"]
	if err != nil || got.Status != "pending" || got.Stage != "activation" || attempt.Confirmed != 1 || attempt.Unknown != 0 ||
		attempt.Status != "confirmed" || attempt.PendingReadbacks != 3 || attempt.MaxPendingReadbacks != 6 ||
		attempt.IdempotencyKey != operation.Attempts["runtime"].IdempotencyKey || adapter.mutationsByStage["runtime"] != 1 ||
		claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "succeeded" || got.ResumeAuthorizationConsumedAt == "" ||
		len(got.ConsumedResumeAuthorizations) == 0 || got.ConsumedResumeAuthorizations[len(got.ConsumedResumeAuthorizations)-1].Authorization.AuthorizationID != previous.AuthorizationID {
		t.Fatalf("failed Runtime replay was not replaced on the original Launch: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionRetriesSameUndispatchedReplacementOnce(t *testing.T) {
	row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previous := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-runtime-image-undispatched")
	previous.ReadbacksAtAuthorization = workspaceLaunchAuthoritativeReadBudget
	completedAt := "2026-08-24T03:12:32Z"
	attempt := operation.Attempts["runtime"]
	attempt.PendingReadbacks, attempt.MaxPendingReadbacks = 2*workspaceLaunchAuthoritativeReadBudget, 2*workspaceLaunchAuthoritativeReadBudget
	operation.Attempts["runtime"] = attempt
	operation.Version = 108
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
	read := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageRuntimeImageRevisionPending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"runtime": {read, read, read}},
		replayableStages:   map[string]bool{"runtime": true},
	}
	authorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-runtime-image-undispatched-final")

	got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), operation.ID, authorization)
	attempt = got.Attempts["runtime"]
	claim := got.IdempotentReplayClaims["runtime"]
	if err != nil || got.Status != "pending" || got.Stage != "activation" || attempt.Confirmed != 1 || attempt.Unknown != 0 ||
		attempt.Status != "confirmed" || attempt.PendingReadbacks != 6 || attempt.MaxPendingReadbacks != 9 ||
		adapter.mutationsByStage["runtime"] != 1 || claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "succeeded" ||
		got.ResumeAuthorizationConsumedAt == "" || len(got.ConsumedResumeAuthorizations) == 0 ||
		got.ConsumedResumeAuthorizations[len(got.ConsumedResumeAuthorizations)-1].Authorization.AuthorizationID != previous.AuthorizationID {
		t.Fatalf("undispatched Runtime replacement did not continue once: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
	}

	for name, mutate := range map[string]func(*workspaceLaunchReconcileOperation, *workspaceLaunchResumeAuthorization){
		"different replacement": func(_ *workspaceLaunchReconcileOperation, value *workspaceLaunchResumeAuthorization) {
			value.ReplacementWorkspaceImageDigest = "registry.example/workspace@sha256:" + strings.Repeat("d", 64)
		},
		"claim not failed": func(value *workspaceLaunchReconcileOperation, _ *workspaceLaunchResumeAuthorization) {
			claim := value.IdempotentReplayClaims["runtime"]
			claim.Status = "succeeded"
			value.IdempotentReplayClaims["runtime"] = claim
		},
		"third image window": func(value *workspaceLaunchReconcileOperation, _ *workspaceLaunchResumeAuthorization) {
			attempt := value.Attempts["runtime"]
			attempt.PendingReadbacks, attempt.MaxPendingReadbacks = 3*workspaceLaunchAuthoritativeReadBudget, 3*workspaceLaunchAuthoritativeReadBudget
			value.Attempts["runtime"] = attempt
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate, decodeErr := decodeWorkspaceLaunchReconcileOperation(row)
			if decodeErr != nil {
				t.Fatal(decodeErr)
			}
			candidateAuthorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-runtime-image-rejected-"+strings.ReplaceAll(name, " ", "-"))
			mutate(&candidate, &candidateAuthorization)
			if workspaceLaunchRuntimeImageRevisionEligible(candidate, candidate.Attempts["runtime"], candidateAuthorization) {
				t.Fatal("invalid final Runtime image authorization was eligible")
			}
		})
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionContinuesSameDispatchedReplacementOnce(t *testing.T) {
	row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previous := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-v108-image")
	previous.ReadbacksAtAuthorization = 2 * workspaceLaunchAuthoritativeReadBudget
	completedAt := "2026-08-24T05:12:32Z"
	attempt := operation.Attempts["runtime"]
	attempt.PendingReadbacks, attempt.MaxPendingReadbacks = 2*workspaceLaunchAuthoritativeReadBudget, 3*workspaceLaunchAuthoritativeReadBudget
	operation.Attempts["runtime"] = attempt
	operation.Version = 111
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
	pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"runtime": {pending, pending, pending, pending}},
		replayableStages:   map[string]bool{"runtime": true},
	}
	authorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-v111-same-image")

	continued, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), operation.ID, authorization)
	attempt = continued.Attempts["runtime"]
	claim := continued.IdempotentReplayClaims["runtime"]
	if err != nil || continued.Status != "pending" || continued.Stage != "runtime" || attempt.Status != "reserved" ||
		attempt.PendingReadbacks != 3*workspaceLaunchAuthoritativeReadBudget+1 || attempt.MaxPendingReadbacks != 4*workspaceLaunchAuthoritativeReadBudget ||
		adapter.mutationsByStage["runtime"] != 1 || adapter.mutationOperationID != operation.ID ||
		adapter.mutationIdempotencyKey != operation.Attempts["runtime"].IdempotencyKey || claim.AuthorizationID != authorization.AuthorizationID ||
		claim.Status != "waiting" || continued.ResumeAuthorizationConsumedAt != "" {
		t.Fatalf("post-dispatch Runtime continuation did not reapply once: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(continued), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
	}

	adapter.stageObservations = map[string]workspaceLaunchStageObservation{
		"runtime": {State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("runtime")},
	}
	continuedRow, err := workspaceLaunchReconcileOperationRow(continued)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: continuedRow}
	converged, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(context.Background(), operation.ID)
	if err != nil || converged.Status != "pending" || converged.Stage != "activation" ||
		converged.Attempts["runtime"].Status != "confirmed" || adapter.mutationsByStage["runtime"] != 1 ||
		converged.IdempotentReplayClaims["runtime"].Status != "succeeded" || converged.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("post-dispatch Runtime continuation did not converge original Launch: operation=%s mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(converged), adapter.mutationsByStage, err)
	}

	for name, mutate := range map[string]func(*workspaceLaunchReconcileOperation, *workspaceLaunchResumeAuthorization){
		"different image": func(_ *workspaceLaunchReconcileOperation, value *workspaceLaunchResumeAuthorization) {
			value.ReplacementWorkspaceImageDigest = "registry.example/workspace@sha256:" + strings.Repeat("d", 64)
		},
		"partial readback drift": func(value *workspaceLaunchReconcileOperation, _ *workspaceLaunchResumeAuthorization) {
			attempt := value.Attempts["runtime"]
			attempt.PendingReadbacks++
			value.Attempts["runtime"] = attempt
		},
		"fourth image window": func(value *workspaceLaunchReconcileOperation, _ *workspaceLaunchResumeAuthorization) {
			attempt := value.Attempts["runtime"]
			attempt.PendingReadbacks, attempt.MaxPendingReadbacks = 4*workspaceLaunchAuthoritativeReadBudget, 4*workspaceLaunchAuthoritativeReadBudget
			value.Attempts["runtime"] = attempt
			value.ResumeAuthorization.ReadbacksAtAuthorization = 3 * workspaceLaunchAuthoritativeReadBudget
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := operation
			candidate.Attempts = maps.Clone(operation.Attempts)
			candidate.IdempotentReplayClaims = maps.Clone(operation.IdempotentReplayClaims)
			prior := *operation.ResumeAuthorization
			candidate.ResumeAuthorization = &prior
			candidateAuthorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-v111-rejected-"+strings.ReplaceAll(name, " ", "-"))
			mutate(&candidate, &candidateAuthorization)
			if workspaceLaunchRuntimeImageRevisionEligible(candidate, candidate.Attempts["runtime"], candidateAuthorization) {
				t.Fatal("invalid post-dispatch Runtime continuation was eligible")
			}
		})
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionSupersedesExactUnreadyRetainedImage(t *testing.T) {
	row := workspaceLaunchRetainedRuntimeImageRevisionManualReviewRow(t)
	before, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	revisionPending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageRuntimeImageRevisionPending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"runtime": {revisionPending, revisionPending, revisionPending}},
		replayableStages:   map[string]bool{"runtime": true},
	}
	authorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-v116-next-image")
	authorization.ReplacementWorkspaceImageDigest = "registry.example/workspace@sha256:" + strings.Repeat("d", 64)

	got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), before.ID, authorization)
	attempt := got.Attempts["runtime"]
	claim := got.IdempotentReplayClaims["runtime"]
	if err != nil || got.Status != "pending" || got.Stage != "activation" || attempt.Confirmed != 1 || attempt.Unknown != 0 ||
		attempt.Status != "confirmed" || attempt.PendingReadbacks != 4*workspaceLaunchAuthoritativeReadBudget ||
		attempt.MaxPendingReadbacks != 5*workspaceLaunchAuthoritativeReadBudget || attempt.IdempotencyKey != before.Attempts["runtime"].IdempotencyKey ||
		adapter.mutationsByStage["runtime"] != 1 || claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "succeeded" ||
		got.ResumeAuthorization == nil || got.ResumeAuthorization.ReplacementWorkspaceImageDigest != authorization.ReplacementWorkspaceImageDigest ||
		got.ResumeAuthorizationConsumedAt == "" || got.RuntimeRepair != nil {
		t.Fatalf("retained Runtime image was not superseded on the original Launch: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionSupersedesFailedRetainedImageBeforeFreshReadback(t *testing.T) {
	row := workspaceLaunchFailedRetainedRuntimeImageRevisionManualReviewRow(t)
	before, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	revisionPending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageRuntimeImageRevisionPending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"runtime": {revisionPending, revisionPending, revisionPending}},
		replayableStages:   map[string]bool{"runtime": true},
	}
	authorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-v119-next-image")
	authorization.ReplacementWorkspaceImageDigest = "registry.example/workspace@sha256:" + strings.Repeat("e", 64)

	got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), before.ID, authorization)
	attempt := got.Attempts["runtime"]
	claim := got.IdempotentReplayClaims["runtime"]
	if err != nil || got.Status != "pending" || got.Stage != "activation" || attempt.Confirmed != 1 || attempt.Unknown != 0 ||
		attempt.Status != "confirmed" || attempt.PendingReadbacks != 5*workspaceLaunchAuthoritativeReadBudget ||
		attempt.MaxPendingReadbacks != 6*workspaceLaunchAuthoritativeReadBudget || attempt.IdempotencyKey != before.Attempts["runtime"].IdempotencyKey ||
		adapter.mutationsByStage["runtime"] != 1 || claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "succeeded" ||
		got.ResumeAuthorization == nil || got.ResumeAuthorization.ReplacementWorkspaceImageDigest != authorization.ReplacementWorkspaceImageDigest ||
		got.ResumeAuthorizationConsumedAt == "" || got.RuntimeRepair != nil {
		t.Fatalf("failed retained Runtime image was not superseded on the original Launch: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionRejectsMismatchedFailedReplay(t *testing.T) {
	row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	attempt := operation.Attempts["runtime"]
	previous := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-runtime-failed-original", LaunchVersion: 100, AuthorizedStage: "runtime", AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-23T08:00:00Z", Reason: "continue the original runtime after authoritative readback",
		MutationBudget: 0, IdempotentReplayBudget: 1, AuthoritativeReadBudget: workspaceLaunchAuthoritativeReadBudget,
	}
	operation.ResumeAuthorization = &previous
	operation.ResumeAuthorizationConsumedAt = "2026-08-23T08:01:00Z"
	operation.IdempotentReplayClaims["runtime"] = workspaceLaunchIdempotentReplayClaim{
		AuthorizationID: previous.AuthorizationID, Stage: "runtime", IdempotencyKey: attempt.IdempotencyKey,
		Status: "failed", CompletedAt: "2026-08-23T08:01:00Z",
	}

	for name, mutate := range map[string]func(*workspaceLaunchReconcileOperation){
		"authorization": func(value *workspaceLaunchReconcileOperation) {
			value.IdempotentReplayClaims["runtime"] = workspaceLaunchIdempotentReplayClaim{AuthorizationID: "different", Stage: "runtime", IdempotencyKey: attempt.IdempotencyKey, Status: "failed", CompletedAt: "2026-08-23T08:01:00Z"}
		},
		"idempotency key": func(value *workspaceLaunchReconcileOperation) {
			claim := value.IdempotentReplayClaims["runtime"]
			claim.IdempotencyKey = "different:runtime"
			value.IdempotentReplayClaims["runtime"] = claim
		},
		"status": func(value *workspaceLaunchReconcileOperation) {
			claim := value.IdempotentReplayClaims["runtime"]
			claim.Status = "succeeded"
			value.IdempotentReplayClaims["runtime"] = claim
		},
		"replacement already used": func(value *workspaceLaunchReconcileOperation) {
			value.ResumeAuthorization.ReplacementWorkspaceImageDigest = "registry.example/workspace@sha256:" + strings.Repeat("b", 64)
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := operation
			candidate.IdempotentReplayClaims = map[contracts.Stage]workspaceLaunchIdempotentReplayClaim{
				"runtime": operation.IdempotentReplayClaims["runtime"],
			}
			prior := *operation.ResumeAuthorization
			candidate.ResumeAuthorization = &prior
			mutate(&candidate)
			authorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-runtime-image-mismatch-"+strings.ReplaceAll(name, " ", "-"))
			if workspaceLaunchFailedRuntimeReplayImageRevisionEligible(candidate, candidate.Attempts["runtime"], authorization) {
				t.Fatal("mismatched failed Runtime replay was eligible")
			}
		})
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionRequiresExactImageOnlyClassification(t *testing.T) {
	for _, state := range []contracts.StageState{workspaceLaunchStageReady, workspaceLaunchStagePending, workspaceLaunchStageAbsent, workspaceLaunchStageUnknown} {
		t.Run(string(state), func(t *testing.T) {
			row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
			before := stringValue(row["result"])
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{stageObservations: map[string]workspaceLaunchStageObservation{"runtime": {State: state}}, replayableStages: map[string]bool{"runtime": true}}
			authorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-runtime-image-revision-"+string(state))

			_, err := NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || adapter.reads != 1 || adapter.mutations != 0 || stringValue(store.row["result"]) != before {
				t.Fatalf("classification %q changed the original Launch: reads=%d mutations=%d err=%v", state, adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionProofPreservesOriginalFabricBinding(t *testing.T) {
	operation := workspaceLaunchRuntimeRepairFixture(t)
	base, err := (&controlPlaneWorkspaceLaunchStageAdapter{}).workspaceLaunchFabricStageInput(context.Background(), operation, false)
	if err != nil {
		t.Fatal(err)
	}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	authorization := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-runtime-image-proof")
	operation.ResumeAuthorization = &authorization
	revised, err := (&controlPlaneWorkspaceLaunchStageAdapter{}).workspaceLaunchFabricStageInput(context.Background(), operation, false)
	if err != nil {
		t.Fatal(err)
	}
	proof := revised.RuntimeImageRevision
	if revised.Binding != base.Binding || revised.WorkspaceImageDigest != base.WorkspaceImageDigest ||
		proof == nil || proof.LaunchOperationID != operation.ID || proof.WorkspaceID != operation.stringFact("workspaceId") ||
		proof.RuntimeOperationID != base.Binding.FabricOperationID || proof.PreviousImageDigest != base.WorkspaceImageDigest ||
		proof.ReplacementImageDigest != authorization.ReplacementWorkspaceImageDigest ||
		proof.AuthorizationDigest != workspaceLaunchResumeAuthorizationDigest(authorization) {
		t.Fatalf("revision proof changed original Fabric identity: base=%#v revised=%#v", base, revised)
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionContinuationRetainsDispatchedProof(t *testing.T) {
	operation := workspaceLaunchRuntimeRepairFixture(t)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	dispatched := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-v108-image")
	dispatched.ReadbacksAtAuthorization = 2 * workspaceLaunchAuthoritativeReadBudget
	continued := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-v111-same-image")
	continued.ReadbacksAtAuthorization = 3 * workspaceLaunchAuthoritativeReadBudget
	operation.ResumeAuthorization = &continued
	operation.ConsumedResumeAuthorizations = []workspaceLaunchConsumedResumeAuthorization{{Authorization: dispatched, ConsumedAt: "2026-08-24T05:12:32Z"}}
	attempt := operation.Attempts["runtime"]
	attempt.PendingReadbacks, attempt.MaxPendingReadbacks = 3*workspaceLaunchAuthoritativeReadBudget, 4*workspaceLaunchAuthoritativeReadBudget
	operation.Attempts["runtime"] = attempt

	input, err := (&controlPlaneWorkspaceLaunchStageAdapter{}).workspaceLaunchFabricStageInput(context.Background(), operation, false)
	if err != nil || input.RuntimeImageRevision == nil || input.RuntimeImageRevision.AuthorizationDigest != workspaceLaunchResumeAuthorizationDigest(dispatched) ||
		input.RuntimeImageRevision.AuthorizationDigest == workspaceLaunchResumeAuthorizationDigest(continued) ||
		input.RuntimeImageRevision.ReplacementImageDigest != continued.ReplacementWorkspaceImageDigest {
		t.Fatalf("post-dispatch continuation did not retain original Fabric proof: input=%#v err=%v", input, err)
	}

	operation.ConsumedResumeAuthorizations[0].Authorization.ReplacementWorkspaceImageDigest = "registry.example/workspace@sha256:" + strings.Repeat("d", 64)
	if _, err := (&controlPlaneWorkspaceLaunchStageAdapter{}).workspaceLaunchFabricStageInput(context.Background(), operation, false); !errors.Is(err, errInvalidWorkspaceLaunchOperation) {
		t.Fatalf("drifted dispatched proof was accepted: %v", err)
	}
}

func TestWorkspaceLaunchRuntimeImageRevisionSupersessionProofUsesRetainedImage(t *testing.T) {
	row := workspaceLaunchRetainedRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previous := *operation.ResumeAuthorization
	active := workspaceLaunchRuntimeImageRevisionAuthorization(t, row, "resume-v116-next-image-proof")
	active.ReplacementWorkspaceImageDigest = "registry.example/workspace@sha256:" + strings.Repeat("d", 64)
	active.ReadbacksAtAuthorization = 4 * workspaceLaunchAuthoritativeReadBudget
	operation.ConsumedResumeAuthorizations = append(operation.ConsumedResumeAuthorizations, workspaceLaunchConsumedResumeAuthorization{
		Authorization: previous, ConsumedAt: operation.ResumeAuthorizationConsumedAt,
	})
	operation.ResumeAuthorization = &active
	operation.ResumeAuthorizationConsumedAt = ""
	attempt := operation.Attempts["runtime"]
	attempt.MaxPendingReadbacks = 5 * workspaceLaunchAuthoritativeReadBudget
	operation.Attempts["runtime"] = attempt

	input, err := (&controlPlaneWorkspaceLaunchStageAdapter{}).workspaceLaunchFabricStageInput(context.Background(), operation, false)
	proof := input.RuntimeImageRevision
	if err != nil || proof == nil || proof.AuthorizationDigest != workspaceLaunchResumeAuthorizationDigest(active) ||
		proof.PreviousImageDigest != previous.ReplacementWorkspaceImageDigest ||
		proof.ReplacementImageDigest != active.ReplacementWorkspaceImageDigest ||
		input.WorkspaceImageDigest != operation.stringFact("workspaceImageDigest") || input.Binding.IdempotencyKey != operation.Attempts["runtime"].IdempotencyKey {
		t.Fatalf("retained image supersession proof changed identity: input=%#v err=%v", input, err)
	}
}

func TestWorkspaceLaunchDecoderRejectsExtendedRuntimeReadbackWithoutImageRevisionAuthorization(t *testing.T) {
	row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	attempt := operation.Attempts["runtime"]
	attempt.MaxPendingReadbacks = 2 * workspaceLaunchAuthoritativeReadBudget
	operation.Attempts["runtime"] = attempt
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeWorkspaceLaunchReconcileOperation(row); !errors.Is(err, errInvalidWorkspaceLaunchOperation) {
		t.Fatalf("extended Runtime readback decoded without image revision authorization: %v", err)
	}
}

func TestWorkspaceLaunchUnknownRuntimeReplayRefusesUnprovenAbsence(t *testing.T) {
	tests := []struct {
		name    string
		adapter *workspaceLaunchUnitAdapter
	}{
		{name: "ready", adapter: &workspaceLaunchUnitAdapter{readyStages: map[string]bool{"runtime": true}}},
		{name: "pending", adapter: &workspaceLaunchUnitAdapter{stageObservations: map[string]workspaceLaunchStageObservation{"runtime": {State: workspaceLaunchStagePending}}}},
		{name: "unknown", adapter: &workspaceLaunchUnitAdapter{unknownStages: map[string]bool{"runtime": true}}},
		{name: "read error", adapter: &workspaceLaunchUnitAdapter{readErrors: map[string]error{"runtime": errors.New("read failed")}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := workspaceLaunchUnknownRuntimeAbsentManualReviewRow(t)
			persistedBefore := stringValue(row["result"])
			store := &workspaceLaunchUnitStore{row: row}
			authorization := workspaceLaunchUnknownRuntimeReplayAuthorization(t, row, "resume-unknown-runtime-replay-"+strings.ReplaceAll(tc.name, " ", "-"))

			_, err := NewWorkspaceLaunchReconciler(store, tc.adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || tc.adapter.reads != 1 || tc.adapter.mutations != 0 || stringValue(store.row["result"]) != persistedBefore {
				t.Fatalf("unproven Runtime absence changed operation: reads=%d mutations=%d err=%v", tc.adapter.reads, tc.adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchUnknownStorageRecoveryUsesAuthoritativeClassification(t *testing.T) {
	t.Run("ready converges read only", func(t *testing.T) {
		row := workspaceLaunchUnknownStorageManualReviewRow(t)
		adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{"storage": true}, replayableStages: map[string]bool{"storage": true}}
		authorization := workspaceLaunchUnknownStorageReplayAuthorization(t, row, "resume-unknown-storage-ready")

		got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
		attempt := got.Attempts["storage"]
		if err != nil || got.Stage != "attachment" || got.Status != "pending" || attempt.Confirmed != 1 || attempt.Unknown != 0 ||
			attempt.Status != "confirmed" || adapter.reads != 1 || adapter.mutations != 0 || got.ResumeAuthorizationConsumedAt == "" {
			t.Fatalf("ready storage did not converge read-only: operation=%s reads=%d mutations=%d err=%v", workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutations, err)
		}
	})

	t.Run("absent replays original attempt once", func(t *testing.T) {
		row := workspaceLaunchUnknownStorageManualReviewRow(t)
		adapter := &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{"storage": true}}
		authorization := workspaceLaunchUnknownStorageReplayAuthorization(t, row, "resume-unknown-storage-absent")

		got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
		attempt := got.Attempts["storage"]
		claim := got.IdempotentReplayClaims["storage"]
		if err != nil || got.Stage != "attachment" || got.Status != "pending" || attempt.Attempted != 1 || attempt.Max != 1 ||
			attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" || adapter.mutationsByStage["storage"] != 1 ||
			adapter.mutationIdempotencyKey != attempt.IdempotencyKey || claim.AuthorizationID != authorization.AuthorizationID ||
			claim.Status != "succeeded" || got.ResumeAuthorizationConsumedAt == "" {
			t.Fatalf("absent storage did not replay original attempt: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
				workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
		}
	})

	t.Run("pending continues read only", func(t *testing.T) {
		row := workspaceLaunchUnknownStorageManualReviewRow(t)
		pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
		adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"storage": {pending, pending}}, replayableStages: map[string]bool{"storage": true}}
		authorization := workspaceLaunchUnknownStorageReplayAuthorization(t, row, "resume-unknown-storage-pending")

		got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
		attempt := got.Attempts["storage"]
		if err != nil || got.Stage != "storage" || got.Status != "pending" || attempt.Attempted != 1 || attempt.Confirmed != 0 ||
			attempt.Unknown != 0 || attempt.Status != "reserved" || attempt.PendingReadbacks != 1 || attempt.MaxPendingReadbacks != workspaceLaunchAuthoritativeReadBudget ||
			adapter.reads != 2 || adapter.mutations != 0 || got.ResumeAuthorizationConsumedAt != "" {
			t.Fatalf("pending storage escaped read-only continuation: operation=%s attempt=%#v reads=%d mutations=%d err=%v",
				workspaceLaunchReconcileResultSummary(got), attempt, adapter.reads, adapter.mutations, err)
		}
	})

	for _, test := range []struct {
		name string
		read workspaceLaunchUnitReadResult
	}{
		{name: "unknown", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}}},
		{name: "read error", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err: errors.New("read failed")}},
	} {
		t.Run(test.name+" stays fail closed", func(t *testing.T) {
			row := workspaceLaunchUnknownStorageManualReviewRow(t)
			before := stringValue(row["result"])
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"storage": {test.read}}, replayableStages: map[string]bool{"storage": true}}
			authorization := workspaceLaunchUnknownStorageReplayAuthorization(t, row, "resume-unknown-storage-"+strings.ReplaceAll(test.name, " ", "-"))

			_, err := NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || adapter.reads != 1 || adapter.mutations != 0 || stringValue(store.row["result"]) != before {
				t.Fatalf("unproven storage changed operation: reads=%d mutations=%d err=%v", adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchFailedStorageReplayConvergesReadyReadOnly(t *testing.T) {
	row := workspaceLaunchUnknownStorageAfterFailedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previous := *operation.ResumeAuthorization
	claimBefore := operation.IdempotentReplayClaims["storage"]
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{"storage": true}, replayableStages: map[string]bool{"storage": true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	authorization := workspaceLaunchFailedStorageReplayAuthorization(t, row, "resume-failed-storage-ready-read")

	got, err := reconciler.Resume(context.Background(), operation.ID, authorization)
	attempt := got.Attempts["storage"]
	claimAfter := got.IdempotentReplayClaims["storage"]
	if err != nil || got.Stage != "attachment" || got.Status != "pending" || attempt.Attempted != 1 || attempt.Confirmed != 1 ||
		attempt.Unknown != 0 || attempt.Status != "confirmed" || attempt.IdempotencyKey != operation.Attempts["storage"].IdempotencyKey ||
		adapter.reads != 1 || adapter.mutations != 0 || got.ResumeAuthorization == nil || *got.ResumeAuthorization != authorization ||
		got.ResumeAuthorizationConsumedAt == "" || claimAfter != claimBefore || len(got.ConsumedResumeAuthorizations) != 1 ||
		got.ConsumedResumeAuthorizations[0].Authorization != previous {
		t.Fatalf("failed storage replay did not converge ready read-only: operation=%s attempt=%#v claim=%#v reads=%d mutations=%d err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claimAfter, adapter.reads, adapter.mutations, err)
	}

	readsBefore, persistedBefore := adapter.reads, stringValue(store.row["result"])
	replayed, err := reconciler.Resume(context.Background(), operation.ID, authorization)
	if err != nil || replayed.Version != got.Version || adapter.reads != readsBefore || adapter.mutations != 0 || stringValue(store.row["result"]) != persistedBefore {
		t.Fatalf("consumed storage ready-read repeated work: operation=%s reads=%d/%d mutations=%d err=%v",
			workspaceLaunchReconcileResultSummary(replayed), adapter.reads, readsBefore, adapter.mutations, err)
	}
}

func TestWorkspaceLaunchFailedStorageReplayReadyReadFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name string
		read workspaceLaunchUnitReadResult
	}{
		{name: "pending", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}},
		{name: "unknown", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}}},
		{name: "read error", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err: errors.New("read failed")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := workspaceLaunchUnknownStorageAfterFailedReplayRow(t)
			before := stringValue(row["result"])
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"storage": {test.read}}, replayableStages: map[string]bool{"storage": true}}
			authorization := workspaceLaunchFailedStorageReplayAuthorization(t, row, "resume-failed-storage-"+strings.ReplaceAll(test.name, " ", "-"))

			_, err := NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || adapter.reads != 1 || adapter.mutations != 0 || stringValue(store.row["result"]) != before {
				t.Fatalf("unproven failed storage replay changed operation: reads=%d mutations=%d err=%v", adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchFailedStorageReplayAuthoritativeAbsentReplaysOriginalKeyOnce(t *testing.T) {
	row := workspaceLaunchUnknownStorageAfterFailedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previous := *operation.ResumeAuthorization
	previousClaim := operation.IdempotentReplayClaims["storage"]
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{"storage": true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	authorization := workspaceLaunchFailedStorageReplayAuthorization(t, row, "resume-failed-storage-authoritative-absent")

	got, err := reconciler.Resume(context.Background(), operation.ID, authorization)
	attempt := got.Attempts["storage"]
	claim := got.IdempotentReplayClaims["storage"]
	if err != nil || got.Stage != "attachment" || got.Status != "pending" || attempt.Attempted != 1 || attempt.Max != 1 ||
		attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" ||
		attempt.IdempotencyKey != operation.Attempts["storage"].IdempotencyKey || adapter.mutationsByStage["storage"] != 1 ||
		adapter.mutationIdempotencyKey != attempt.IdempotencyKey || claim.AuthorizationID != authorization.AuthorizationID ||
		claim.AuthorizationID == previousClaim.AuthorizationID || claim.Status != "succeeded" || got.ResumeAuthorizationConsumedAt == "" ||
		len(got.ConsumedResumeAuthorizations) != 1 || got.ConsumedResumeAuthorizations[0].Authorization != previous {
		t.Fatalf("authoritative absent storage did not replay the original key once: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
	}

	readsBefore, mutationsBefore, persistedBefore := adapter.reads, adapter.mutations, stringValue(store.row["result"])
	replayed, err := reconciler.Resume(context.Background(), operation.ID, authorization)
	if err != nil || replayed.Version != got.Version || adapter.reads != readsBefore || adapter.mutations != mutationsBefore ||
		stringValue(store.row["result"]) != persistedBefore {
		t.Fatalf("consumed storage absent recovery repeated work: operation=%s reads=%d/%d mutations=%d/%d err=%v",
			workspaceLaunchReconcileResultSummary(replayed), adapter.reads, readsBefore, adapter.mutations, mutationsBefore, err)
	}
}

func TestWorkspaceLaunchFailedStorageReplayCanBeReauthorizedAfterRepeatedReadbackFailure(t *testing.T) {
	row := workspaceLaunchUnknownStorageAfterFailedReplayRow(t)
	store := &workspaceLaunchUnitStore{row: row}
	unknown := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"storage": {
			{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}},
			{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}, unknown,
			{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}},
			{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}, unknown,
		}},
		replayableStages: map[string]bool{"storage": true},
	}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	second := workspaceLaunchFailedStorageReplayAuthorization(t, row, "resume-failed-storage-final-replay")

	parked, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, second)
	if err != nil || parked.Status != "manual_review" || parked.Stage != "storage" ||
		parked.idempotentReplayAuthorizationCount("storage") != 2 || adapter.reads != 3 || adapter.mutations != 0 {
		t.Fatalf("final storage replay did not park after unknown readback: operation=%s replayAuthorizations=%d reads=%d mutations=%d err=%v",
			workspaceLaunchReconcileResultSummary(parked), parked.idempotentReplayAuthorizationCount("storage"), adapter.reads, adapter.mutations, err)
	}

	third := workspaceLaunchExhaustedStorageBindingReplayAuthorization(t, store.row, "resume-exhausted-storage-final-binding")
	parked, err = reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, third)
	if err != nil || parked.Status != "manual_review" || parked.Stage != "storage" ||
		parked.idempotentReplayAuthorizationCount("storage") != 3 || adapter.reads != 6 || adapter.mutations != 0 {
		t.Fatalf("storage binding replay did not park after unknown readback: operation=%s replayAuthorizations=%d reads=%d mutations=%d err=%v",
			workspaceLaunchReconcileResultSummary(parked), parked.idempotentReplayAuthorizationCount("storage"), adapter.reads, adapter.mutations, err)
	}

	before, err := decodeWorkspaceLaunchReconcileOperation(store.row)
	if err != nil {
		t.Fatal(err)
	}
	readsBefore, mutationsBefore := adapter.reads, adapter.mutations
	fourth := workspaceLaunchExhaustedStorageBindingReplayAuthorization(t, store.row, "resume-storage-current-evidence-replay")
	got, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, fourth)
	attempt := got.Attempts["storage"]
	claim := got.IdempotentReplayClaims["storage"]
	if err != nil || got.Status != "pending" || got.Stage != "attachment" ||
		attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" ||
		attempt.IdempotencyKey != before.Attempts["storage"].IdempotencyKey || claim.AuthorizationID != fourth.AuthorizationID || claim.Status != "succeeded" ||
		got.ResumeAuthorizationConsumedAt == "" || got.idempotentReplayAuthorizationCount("storage") != 4 ||
		adapter.reads != readsBefore+4 || adapter.mutations != mutationsBefore+1 || adapter.mutationsByStage["storage"] != 1 ||
		adapter.mutationIdempotencyKey != attempt.IdempotencyKey {
		t.Fatalf("current failed storage evidence did not permit one authorized replay: operation=%s attempt=%#v claim=%#v reads=%d/%d mutations=%d/%d err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, readsBefore, adapter.mutations, mutationsBefore, err)
	}
}

func TestWorkspaceLaunchExhaustedStorageReplayConvergesReadyReadOnly(t *testing.T) {
	row := workspaceLaunchStorageAfterExhaustedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	claimBefore := operation.IdempotentReplayClaims["storage"]
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{"storage": true}, replayableStages: map[string]bool{"storage": true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	authorization := workspaceLaunchExhaustedStorageReadAuthorization(t, row, "resume-exhausted-storage-ready-read")

	got, err := reconciler.Resume(context.Background(), operation.ID, authorization)
	attempt := got.Attempts["storage"]
	if err != nil || got.Stage != "attachment" || got.Status != "pending" || attempt.Attempted != 1 || attempt.Confirmed != 1 ||
		attempt.Unknown != 0 || attempt.Status != "confirmed" || adapter.reads != 1 || adapter.mutations != 0 ||
		got.IdempotentReplayClaims["storage"] != claimBefore || got.idempotentReplayAuthorizationCount("storage") != 2 ||
		got.ResumeAuthorization == nil || *got.ResumeAuthorization != authorization || got.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("exhausted storage did not converge ready read-only: operation=%s attempt=%#v reads=%d mutations=%d err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, adapter.reads, adapter.mutations, err)
	}

	readsBefore, persistedBefore := adapter.reads, stringValue(store.row["result"])
	replayed, err := reconciler.Resume(context.Background(), operation.ID, authorization)
	if err != nil || replayed.Version != got.Version || adapter.reads != readsBefore || adapter.mutations != 0 || stringValue(store.row["result"]) != persistedBefore {
		t.Fatalf("consumed exhausted storage read repeated work: operation=%s reads=%d/%d mutations=%d err=%v",
			workspaceLaunchReconcileResultSummary(replayed), adapter.reads, readsBefore, adapter.mutations, err)
	}
}

func TestWorkspaceLaunchExhaustedStorageReplayReadFailsClosedWithoutReady(t *testing.T) {
	for _, test := range []struct {
		name string
		read workspaceLaunchUnitReadResult
	}{
		{name: "absent", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}},
		{name: "pending", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}},
		{name: "unknown", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}}},
		{name: "read error", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err: errors.New("read failed")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := workspaceLaunchStorageAfterExhaustedReplayRow(t)
			before := stringValue(row["result"])
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"storage": {test.read}}, replayableStages: map[string]bool{"storage": true}}
			authorization := workspaceLaunchExhaustedStorageReadAuthorization(t, row, "resume-exhausted-storage-"+strings.ReplaceAll(test.name, " ", "-"))

			_, err := NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || adapter.reads != 1 || adapter.mutations != 0 || stringValue(store.row["result"]) != before {
				t.Fatalf("unready exhausted storage changed operation: reads=%d mutations=%d err=%v", adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchExhaustedStorageBindingReplaysOriginalKeyAfterAuthoritativeAbsence(t *testing.T) {
	row := workspaceLaunchStorageAfterExhaustedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	previous := *operation.ResumeAuthorization
	previousClaim := operation.IdempotentReplayClaims["storage"]
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{"storage": true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	authorization := workspaceLaunchExhaustedStorageBindingReplayAuthorization(t, row, "resume-exhausted-storage-binding")

	got, err := reconciler.Resume(context.Background(), operation.ID, authorization)
	attempt := got.Attempts["storage"]
	claim := got.IdempotentReplayClaims["storage"]
	if err != nil || got.Stage != "attachment" || got.Status != "pending" || attempt.Attempted != 1 || attempt.Max != 1 ||
		attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" ||
		attempt.IdempotencyKey != operation.Attempts["storage"].IdempotencyKey || adapter.reads != 4 || adapter.mutationsByStage["storage"] != 1 ||
		adapter.mutationIdempotencyKey != attempt.IdempotencyKey || claim.AuthorizationID != authorization.AuthorizationID ||
		claim.AuthorizationID == previousClaim.AuthorizationID || claim.Status != "succeeded" || got.ResumeAuthorizationConsumedAt == "" ||
		got.idempotentReplayAuthorizationCount("storage") != 3 || len(got.ConsumedResumeAuthorizations) != 2 ||
		got.ConsumedResumeAuthorizations[1].Authorization != previous {
		t.Fatalf("authoritative absence did not continue the original storage binding: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
	}

	readsBefore, mutationsBefore, persistedBefore := adapter.reads, adapter.mutations, stringValue(store.row["result"])
	replayed, err := reconciler.Resume(context.Background(), operation.ID, authorization)
	if err != nil || replayed.Version != got.Version || adapter.reads != readsBefore || adapter.mutations != mutationsBefore ||
		stringValue(store.row["result"]) != persistedBefore {
		t.Fatalf("consumed storage binding continuation repeated work: operation=%s reads=%d/%d mutations=%d/%d err=%v",
			workspaceLaunchReconcileResultSummary(replayed), adapter.reads, readsBefore, adapter.mutations, mutationsBefore, err)
	}
}

func TestWorkspaceLaunchExhaustedStorageBindingReplayFailsClosedWithoutAbsence(t *testing.T) {
	for _, test := range []struct {
		name string
		read workspaceLaunchUnitReadResult
	}{
		{name: "ready", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("storage")}}},
		{name: "pending", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}},
		{name: "unknown", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}}},
		{name: "read error", read: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err: errors.New("read failed")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := workspaceLaunchStorageAfterExhaustedReplayRow(t)
			before := stringValue(row["result"])
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"storage": {test.read}}, replayableStages: map[string]bool{"storage": true}}
			authorization := workspaceLaunchExhaustedStorageBindingReplayAuthorization(t, row, "resume-storage-binding-"+strings.ReplaceAll(test.name, " ", "-"))

			_, err := NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || adapter.reads != 1 || adapter.mutations != 0 || stringValue(store.row["result"]) != before {
				t.Fatalf("unproven exhausted storage binding changed operation: reads=%d mutations=%d err=%v", adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchFailedStorageReplayReadyReadRequiresExactEvidence(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*workspaceLaunchReconcileOperation, *workspaceLaunchResumeAuthorization)
	}{
		{name: "claim not failed", mutate: func(operation *workspaceLaunchReconcileOperation, _ *workspaceLaunchResumeAuthorization) {
			claim := operation.IdempotentReplayClaims["storage"]
			claim.Status = "succeeded"
			operation.IdempotentReplayClaims["storage"] = claim
		}},
		{name: "claim key drift", mutate: func(operation *workspaceLaunchReconcileOperation, _ *workspaceLaunchResumeAuthorization) {
			claim := operation.IdempotentReplayClaims["storage"]
			claim.IdempotencyKey += "-drift"
			operation.IdempotentReplayClaims["storage"] = claim
		}},
		{name: "previous authorization unconsumed", mutate: func(operation *workspaceLaunchReconcileOperation, _ *workspaceLaunchResumeAuthorization) {
			operation.ResumeAuthorizationConsumedAt = ""
		}},
		{name: "missing replay budget", mutate: func(_ *workspaceLaunchReconcileOperation, authorization *workspaceLaunchResumeAuthorization) {
			authorization.IdempotentReplayBudget = 0
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			row := workspaceLaunchUnknownStorageAfterFailedReplayRow(t)
			operation, err := decodeWorkspaceLaunchReconcileOperation(row)
			if err != nil {
				t.Fatal(err)
			}
			authorization := workspaceLaunchFailedStorageReplayAuthorization(t, row, "resume-failed-storage-evidence-"+strings.ReplaceAll(test.name, " ", "-"))
			test.mutate(&operation, &authorization)
			row, err = workspaceLaunchReconcileOperationRow(operation)
			if err != nil {
				t.Fatal(err)
			}
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{"storage": true}, replayableStages: map[string]bool{"storage": true}}
			_, err = NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), operation.ID, authorization)
			if err == nil || adapter.reads != 0 || adapter.mutations != 0 {
				t.Fatalf("drifted failed storage replay evidence reached owner: reads=%d mutations=%d err=%v", adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchUnknownComputeResumeContinuesProviderProvisioningReadOnly(t *testing.T) {
	row := workspaceLaunchUnknownStageManualReviewRow(t, "ensure_compute_allocation")
	store := &workspaceLaunchUnitStore{row: row}
	pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
	adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"ensure_compute_allocation": {pending, pending}}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	now := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	reconciler.now = func() time.Time { return now }
	authorization := workspaceLaunchUnknownComputeContinuationAuthorization(t, row, "resume-unknown-compute-provider-pending")

	got, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
	attempt := got.Attempts["ensure_compute_allocation"]
	if err != nil || got.Status != "pending" || got.Stage != "ensure_compute_allocation" || attempt.Attempted != 1 || attempt.Max != 1 ||
		attempt.Confirmed != 0 || attempt.Unknown != 0 || attempt.Status != "reserved" || attempt.IdempotencyKey != workspaceLaunchStageIdempotencyKey(got, 1) ||
		attempt.PendingReadbacks != workspaceLaunchAuthoritativeReadBudget+1 || attempt.MaxPendingReadbacks != workspaceLaunchAuthoritativeReadBudget+workspaceLaunchComputeFreshContinuationAdditionalReadBudget ||
		attempt.PendingDeadlineAt != now.Add(workspaceLaunchComputePendingWindow).Format(time.RFC3339Nano) || adapter.reads != 2 || adapter.mutations != 0 ||
		got.ResumeAuthorization == nil || got.ResumeAuthorization.IdempotentReplayBudget != 1 || got.ResumeAuthorizationConsumedAt != "" {
		t.Fatalf("compute provisioning resume escaped the original read-only stage: operation=%s attempt=%#v reads=%d mutations=%d err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, adapter.reads, adapter.mutations, err)
	}
}

func TestWorkspaceLaunchUnknownComputeResumeAcceptsPersistedHistoricalReadbackCounts(t *testing.T) {
	for _, pendingReadbacks := range []int{0, 1, workspaceLaunchAuthoritativeReadBudget, 46} {
		t.Run(strconv.Itoa(pendingReadbacks), func(t *testing.T) {
			row := workspaceLaunchUnknownStageManualReviewRow(t, "ensure_compute_allocation")
			operation, err := decodeWorkspaceLaunchReconcileOperation(row)
			if err != nil {
				t.Fatal(err)
			}
			attempt := operation.Attempts[operation.Stage]
			attempt.PendingReadbacks = pendingReadbacks
			attempt.MaxPendingReadbacks = max(attempt.MaxPendingReadbacks, pendingReadbacks)
			operation.Attempts[operation.Stage] = attempt
			row, err = workspaceLaunchReconcileOperationRow(operation)
			if err != nil {
				t.Fatal(err)
			}
			pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
			adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{string(operation.Stage): {pending, pending}}}
			authorization := workspaceLaunchUnknownComputeContinuationAuthorization(t, row, "resume-compute-readbacks-"+strconv.Itoa(pendingReadbacks))
			got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchValidatingUnitStore{workspaceLaunchUnitStore: &workspaceLaunchUnitStore{row: row}}, adapter).Resume(context.Background(), operation.ID, authorization)
			gotAttempt := got.Attempts[operation.Stage]
			wantMaxPendingReadbacks := pendingReadbacks + workspaceLaunchRemainingComputeReadBudget(operation.Attempts[operation.Stage])
			if err != nil || got.Status != "pending" || gotAttempt.PendingReadbacks != pendingReadbacks+1 ||
				gotAttempt.MaxPendingReadbacks != wantMaxPendingReadbacks || gotAttempt.Unknown != 0 || adapter.mutations != 0 ||
				got.ResumeAuthorization.ReadbacksAtAuthorization+got.ResumeAuthorization.AuthoritativeReadBudget != wantMaxPendingReadbacks {
				t.Fatalf("historical readbacks=%d did not resume: operation=%s attempt=%#v err=%v", pendingReadbacks, workspaceLaunchReconcileResultSummary(got), gotAttempt, err)
			}
		})
	}
}

func TestWorkspaceLaunchUnknownComputeResumeClaimsOwnershipWithOriginalKey(t *testing.T) {
	row := workspaceLaunchUnknownStageManualReviewRow(t, "ensure_compute_allocation")
	store := &workspaceLaunchUnitStore{row: row}
	ownership := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageOwnershipPending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"ensure_compute_allocation": {ownership, ownership, ownership}},
		replayableStages:   map[string]bool{"ensure_compute_allocation": true},
	}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	reconciler.now = func() time.Time { return time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC) }
	authorization := workspaceLaunchUnknownComputeContinuationAuthorization(t, row, "resume-unknown-compute-ownership")

	got, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
	attempt := got.Attempts["ensure_compute_allocation"]
	claim := got.IdempotentReplayClaims["ensure_compute_allocation"]
	if err != nil || got.Status != "pending" || got.Stage != "storage" || attempt.Attempted != 1 || attempt.Max != 1 || attempt.Confirmed != 1 || attempt.Unknown != 0 ||
		attempt.IdempotencyKey != workspaceLaunchStageIdempotencyKey(operationWithStage(got, "ensure_compute_allocation"), 1) ||
		adapter.mutationsByStage["ensure_compute_allocation"] != 1 || adapter.mutationIdempotencyKey != attempt.IdempotencyKey ||
		claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "succeeded" || got.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("compute ownership resume changed attempt identity: operation=%s attempt=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.reads, adapter.mutationsByStage, err)
	}
	for _, stage := range []string{"storage", "attachment", "secret", "runtime", "activation", "receipt"} {
		if adapter.mutationsByStage[stage] != 0 {
			t.Fatalf("downstream stage %s started before compute confirmation: mutations=%#v", stage, adapter.mutationsByStage)
		}
	}

	for err == nil && got.Status == "pending" {
		got, err = reconciler.Reconcile(context.Background(), got.ID)
	}
	if err != nil || got.Status != "succeeded" || got.Stage != "succeeded" || got.stringFact("receiptId") != "receipt-unit" {
		t.Fatalf("compute continuation did not reach the original receipt: operation=%s mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), adapter.mutationsByStage, err)
	}
	for _, stage := range []string{"ensure_compute_allocation", "storage", "attachment", "secret", "runtime", "activation", "receipt"} {
		if adapter.mutationsByStage[stage] != 1 {
			t.Fatalf("stage %s mutation count=%d want=1 after compute continuation: mutations=%#v", stage, adapter.mutationsByStage[stage], adapter.mutationsByStage)
		}
	}
	mutationsBeforeReplay := adapter.mutations
	got, err = reconciler.Reconcile(context.Background(), got.ID)
	if err != nil || got.Status != "succeeded" || adapter.mutations != mutationsBeforeReplay {
		t.Fatalf("terminal receipt replay performed work: operation=%s mutations=%d/%d err=%v",
			workspaceLaunchReconcileResultSummary(got), adapter.mutations, mutationsBeforeReplay, err)
	}
}

func TestWorkspaceLaunchUnknownComputeResumeReauthorizesOneFailedReplayWithOriginalKey(t *testing.T) {
	row := workspaceLaunchUnknownComputeAfterFailedReplayRow(t)
	before, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	ownership := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageOwnershipPending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"ensure_compute_allocation": {ownership, ownership, ownership}},
		replayableStages:   map[string]bool{"ensure_compute_allocation": true},
	}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	reconciler.now = func() time.Time { return time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC) }
	authorization := workspaceLaunchUnknownComputeContinuationAuthorization(t, row, "resume-unknown-compute-failed-replacement")

	got, err := reconciler.Resume(context.Background(), before.ID, authorization)
	attempt := got.Attempts["ensure_compute_allocation"]
	claim := got.IdempotentReplayClaims["ensure_compute_allocation"]
	if err != nil || got.Status != "pending" || got.Stage != "storage" || attempt.Attempted != 1 || attempt.Max != 1 ||
		attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" ||
		attempt.IdempotencyKey != before.Attempts["ensure_compute_allocation"].IdempotencyKey ||
		adapter.mutationsByStage["ensure_compute_allocation"] != 1 || adapter.mutationIdempotencyKey != attempt.IdempotencyKey ||
		claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "succeeded" || got.ResumeAuthorizationConsumedAt == "" ||
		len(got.ConsumedResumeAuthorizations) != 1 || got.ConsumedResumeAuthorizations[0].Authorization.AuthorizationID != before.ResumeAuthorization.AuthorizationID {
		t.Fatalf("failed compute replay was not replaced in the original attempt: operation=%s attempt=%#v claim=%#v history=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, got.ConsumedResumeAuthorizations, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchUnknownComputeResumeRetriesWaitingOwnershipWithOriginalKey(t *testing.T) {
	row := workspaceLaunchUnknownComputeAfterFailedReplayRow(t)
	before, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	ownership := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageOwnershipPending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"ensure_compute_allocation": {ownership, ownership, ownership, ownership, ownership}},
		replayableStages:   map[string]bool{"ensure_compute_allocation": true},
		mutationErrors:     map[string]error{"ensure_compute_allocation": errors.New("first ownership replay did not converge")},
	}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	reconciler.now = func() time.Time { return time.Date(2026, 8, 23, 2, 0, 0, 0, time.UTC) }
	authorization := workspaceLaunchUnknownComputeContinuationAuthorization(t, row, "resume-unknown-compute-waiting-replacement")

	waiting, err := reconciler.Resume(context.Background(), before.ID, authorization)
	waitingAttempt := waiting.Attempts["ensure_compute_allocation"]
	waitingClaim := waiting.IdempotentReplayClaims["ensure_compute_allocation"]
	if err != nil || waiting.Status != "pending" || waiting.Stage != "ensure_compute_allocation" ||
		waitingAttempt.Attempted != 1 || waitingAttempt.Status != "reserved" || waitingAttempt.PendingReadbacks != before.Attempts["ensure_compute_allocation"].PendingReadbacks+1 ||
		waitingClaim.AuthorizationID != authorization.AuthorizationID || waitingClaim.Status != "waiting" ||
		adapter.mutationsByStage["ensure_compute_allocation"] != 1 || adapter.mutationIdempotencyKey != before.Attempts["ensure_compute_allocation"].IdempotencyKey {
		t.Fatalf("first ownership replay did not wait on the original attempt: operation=%s attempt=%#v claim=%#v mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(waiting), waitingAttempt, waitingClaim, adapter.mutationsByStage, err)
	}

	delete(adapter.mutationErrors, "ensure_compute_allocation")
	reconciler.now = func() time.Time { return time.Date(2026, 8, 23, 2, 11, 0, 0, time.UTC) }
	got, err := reconciler.Reconcile(context.Background(), before.ID)
	attempt := got.Attempts["ensure_compute_allocation"]
	claim := got.IdempotentReplayClaims["ensure_compute_allocation"]
	if err != nil || got.Status != "pending" || got.Stage != "storage" || attempt.Attempted != 1 || attempt.Max != 1 ||
		attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" || attempt.IdempotencyKey != before.Attempts["ensure_compute_allocation"].IdempotencyKey ||
		adapter.mutationsByStage["ensure_compute_allocation"] != 2 || adapter.mutationIdempotencyKey != attempt.IdempotencyKey ||
		claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "succeeded" || got.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("waiting ownership replay did not converge with the original key: operation=%s attempt=%#v claim=%#v mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchUnknownComputeResumeAllowsAnotherReviewedFailedReplayReplacement(t *testing.T) {
	row := workspaceLaunchUnknownComputeAfterFailedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	first := *operation.ResumeAuthorization
	productionAttempt := operation.Attempts[operation.Stage]
	productionAttempt.PendingReadbacks = 46
	productionAttempt.MaxPendingReadbacks = workspaceLaunchComputeFreshContinuationAdditionalReadBudget
	operation.Attempts[operation.Stage] = productionAttempt
	operation.ConsumedResumeAuthorizations = append(operation.ConsumedResumeAuthorizations, workspaceLaunchConsumedResumeAuthorization{
		Authorization: first, ConsumedAt: operation.ResumeAuthorizationConsumedAt,
	})
	second := first
	second.AuthorizationID = "resume-unknown-compute-failed-replacement"
	second.LaunchVersion = operation.Version
	operation.ResumeAuthorization = &second
	operation.ResumeAuthorizationConsumedAt = "2026-08-23T02:01:00Z"
	previousClaim := operation.IdempotentReplayClaims[operation.Stage]
	previousClaim.AuthorizationID = second.AuthorizationID
	previousClaim.CompletedAt = operation.ResumeAuthorizationConsumedAt
	operation.IdempotentReplayClaims[operation.Stage] = previousClaim
	operation.Version++
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchValidatingUnitStore{workspaceLaunchUnitStore: &workspaceLaunchUnitStore{row: row}}
	ownership := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageOwnershipPending}}
	ready := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{
		State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("ensure_compute_allocation"),
	}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"ensure_compute_allocation": {ownership, ownership, ownership, ready}},
		replayableStages:   map[string]bool{"ensure_compute_allocation": true},
	}
	authorization := workspaceLaunchUnknownComputeContinuationAuthorization(t, row, "resume-unknown-compute-third")

	got, err := NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), operation.ID, authorization)
	attempt := got.Attempts["ensure_compute_allocation"]
	claim := got.IdempotentReplayClaims["ensure_compute_allocation"]
	if err != nil || got.Status != "pending" || got.Stage != "storage" || attempt.Attempted != 1 || attempt.Max != 1 ||
		attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" || attempt.IdempotencyKey != operation.Attempts[operation.Stage].IdempotencyKey ||
		adapter.reads != 4 || adapter.mutationsByStage["ensure_compute_allocation"] != 1 || adapter.mutationIdempotencyKey != attempt.IdempotencyKey ||
		claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "succeeded" || got.ResumeAuthorizationConsumedAt == "" ||
		len(got.ConsumedResumeAuthorizations) != 2 {
		t.Fatalf("reviewed failed replay replacement did not continue the original attempt: operation=%s attempt=%#v claim=%#v history=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, claim, got.ConsumedResumeAuthorizations, adapter.reads, adapter.mutationsByStage, err)
	}
	for _, stage := range []string{"storage", "attachment", "secret", "runtime", "activation", "receipt"} {
		if adapter.mutationsByStage[stage] != 0 {
			t.Fatalf("downstream stage %s started before the next reconcile: mutations=%#v", stage, adapter.mutationsByStage)
		}
	}
}

func TestWorkspaceLaunchUnknownComputeResumeRefusesUnprovenState(t *testing.T) {
	for _, tc := range []struct {
		name        string
		observation workspaceLaunchStageObservation
		err         error
	}{
		{name: "ready", observation: workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("ensure_compute_allocation")}},
		{name: "absent", observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}},
		{name: "unknown", observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}},
		{name: "read error", observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err: errors.New("read failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := workspaceLaunchUnknownStageManualReviewRow(t, "ensure_compute_allocation")
			before := stringValue(row["result"])
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{
				"ensure_compute_allocation": {{observation: tc.observation, err: tc.err}},
			}}
			authorization := workspaceLaunchUnknownComputeContinuationAuthorization(t, row, "resume-compute-refuse-"+strings.ReplaceAll(tc.name, " ", "-"))
			_, err := NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || adapter.reads != 1 || adapter.mutations != 0 || stringValue(store.row["result"]) != before {
				t.Fatalf("unproven compute state changed operation: reads=%d mutations=%d err=%v", adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchUnknownRuntimeRecoveryConvergesFailedFreshContinuation(t *testing.T) {
	row := workspaceLaunchUnknownRuntimeWithFailedFreshContinuationRow(t)
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{"runtime": true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	reconciler.now = func() time.Time { return time.Date(2026, 8, 18, 6, 0, 0, 0, time.UTC) }
	authorization := workspaceLaunchUnknownRuntimeReadAuthorization(t, row, "resume-failed-fresh-runtime-ready")

	got, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
	fresh := got.FreshContinuationAuthorizations["runtime"]
	if err != nil || got.Status != "pending" || got.Stage != "activation" || fresh.Status != "consumed" ||
		fresh.ConsumedAt != "2026-08-18T06:00:00Z" || adapter.reads != 1 || adapter.mutations != 0 {
		t.Fatalf("failed fresh continuation did not converge with Runtime: operation=%s fresh=%#v reads=%d mutations=%d err=%v",
			workspaceLaunchReconcileResultSummary(got), fresh, adapter.reads, adapter.mutations, err)
	}
}

func TestWorkspaceLaunchUnknownRuntimeRecoveryRefusesUnconfirmedAuthority(t *testing.T) {
	tests := []struct {
		name    string
		adapter *workspaceLaunchUnitAdapter
	}{
		{name: "pending", adapter: &workspaceLaunchUnitAdapter{stageObservations: map[string]workspaceLaunchStageObservation{"runtime": {State: workspaceLaunchStagePending}}}},
		{name: "absent", adapter: &workspaceLaunchUnitAdapter{}},
		{name: "unknown", adapter: &workspaceLaunchUnitAdapter{unknownStages: map[string]bool{"runtime": true}}},
		{name: "read error", adapter: &workspaceLaunchUnitAdapter{readErrors: map[string]error{"runtime": errors.New("read failed")}}},
		{name: "invalid ready facts", adapter: &workspaceLaunchUnitAdapter{stageObservations: map[string]workspaceLaunchStageObservation{"runtime": {State: workspaceLaunchStageReady, Facts: map[string]any{}}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := workspaceLaunchUnknownStageManualReviewRow(t, "runtime")
			persistedBefore := stringValue(row["result"])
			store := &workspaceLaunchUnitStore{row: row}
			authorization := workspaceLaunchUnknownRuntimeReadAuthorization(t, row, "resume-unknown-runtime-"+strings.ReplaceAll(tc.name, " ", "-"))
			_, err := NewWorkspaceLaunchReconciler(store, tc.adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || tc.adapter.reads != 1 || tc.adapter.mutations != 0 || stringValue(store.row["result"]) != persistedBefore {
				t.Fatalf("unconfirmed Runtime authority changed operation: reads=%d mutations=%d err=%v", tc.adapter.reads, tc.adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchUnknownRuntimeRecoveryIsBillingAndStageScoped(t *testing.T) {
	tests := []struct {
		name      string
		stage     contracts.Stage
		configure func(*workspaceLaunchReconcileOperation)
	}{
		{name: "non Runtime stage", stage: "debit"},
		{name: "customer owned Runtime", stage: "runtime", configure: func(operation *workspaceLaunchReconcileOperation) {
			operation.raw["resourceBillingEnabled"] = json.RawMessage("false")
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			row := workspaceLaunchUnknownStageManualReviewRow(t, tc.stage)
			operation, err := decodeWorkspaceLaunchReconcileOperation(row)
			if err != nil {
				t.Fatal(err)
			}
			if tc.configure != nil {
				tc.configure(&operation)
			}
			row, err = workspaceLaunchReconcileOperationRow(operation)
			if err != nil {
				t.Fatal(err)
			}
			adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{string(tc.stage): true}}
			authorization := workspaceLaunchUnknownRuntimeReadAuthorization(t, row, "resume-unknown-scope-"+strings.ReplaceAll(tc.name, " ", "-"))
			_, err = NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), operation.ID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || adapter.reads != 0 || adapter.mutations != 0 {
				t.Fatalf("out-of-scope recovery reached owner: reads=%d mutations=%d err=%v", adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchReservedStageReplayRefusesUncertainAuthority(t *testing.T) {
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		for _, tc := range []struct {
			name    string
			adapter *workspaceLaunchUnitAdapter
		}{
			{name: "unknown", adapter: &workspaceLaunchUnitAdapter{unknownStages: map[string]bool{string(stage): true}}},
			{name: "read error", adapter: &workspaceLaunchUnitAdapter{readErrors: map[string]error{string(stage): errors.New("read failed")}}},
			{name: "conflicting ready facts", adapter: &workspaceLaunchUnitAdapter{stageObservations: map[string]workspaceLaunchStageObservation{string(stage): {State: workspaceLaunchStageReady, Facts: map[string]any{}}}}},
		} {
			t.Run(string(stage)+"/"+tc.name, func(t *testing.T) {
				row := workspaceLaunchReservedStageManualReviewRow(t, stage)
				persistedBefore := stringValue(row["result"])
				store := &workspaceLaunchUnitStore{row: row}
				tc.adapter.replayableStages = map[string]bool{string(stage): true}
				authorization := workspaceLaunchReservedStageAuthorization(t, row, "resume-"+string(stage)+"-"+strings.ReplaceAll(tc.name, " ", "-"))
				_, err := NewWorkspaceLaunchReconciler(store, tc.adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
				if !errors.Is(err, errWorkspaceLaunchGrantConflict) || tc.adapter.reads != 1 || tc.adapter.mutations != 0 || stringValue(store.row["result"]) != persistedBefore {
					t.Fatalf("uncertain authority changed %s: reads=%d mutations=%d err=%v", stage, tc.adapter.reads, tc.adapter.mutations, err)
				}
			})
		}
	}
}

func TestWorkspaceLaunchReservedStageReplayRefusesStateAndAuthorizationDrift(t *testing.T) {
	cases := []struct {
		name       string
		mutateOp   func(*workspaceLaunchReconcileOperation)
		mutateAuth func(*workspaceLaunchResumeAuthorization)
	}{
		{name: "status", mutateOp: func(operation *workspaceLaunchReconcileOperation) { operation.Status = "pending" }},
		{name: "attempt", mutateOp: func(operation *workspaceLaunchReconcileOperation) {
			attempt := operation.Attempts["debit"]
			attempt.Unknown, attempt.Status = 1, "unknown"
			operation.Attempts["debit"] = attempt
		}},
		{name: "version", mutateAuth: func(authorization *workspaceLaunchResumeAuthorization) { authorization.LaunchVersion++ }},
		{name: "stage", mutateAuth: func(authorization *workspaceLaunchResumeAuthorization) { authorization.AuthorizedStage = "key" }},
		{name: "budget", mutateAuth: func(authorization *workspaceLaunchResumeAuthorization) { authorization.MutationBudget = 1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := workspaceLaunchReservedStageManualReviewRow(t, "debit")
			operation, err := decodeWorkspaceLaunchReconcileOperation(row)
			if err != nil {
				t.Fatal(err)
			}
			if tc.mutateOp != nil {
				tc.mutateOp(&operation)
				row, err = workspaceLaunchReconcileOperationRow(operation)
				if err != nil {
					t.Fatal(err)
				}
			}
			authorization := workspaceLaunchReservedStageAuthorization(t, row, "resume-debit-drift-"+tc.name)
			if tc.mutateAuth != nil {
				tc.mutateAuth(&authorization)
			}
			store, adapter := &workspaceLaunchUnitStore{row: row}, &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{"debit": true}}
			persistedBefore := stringValue(row["result"])
			_, err = NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), operation.ID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || adapter.mutations != 0 || stringValue(store.row["result"]) != persistedBefore {
				t.Fatalf("drift changed debit: reads=%d mutations=%d err=%v", adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchReadOnlyContinuationRequiresReservedTypedPending(t *testing.T) {
	cases := []struct {
		name        string
		observation contracts.StageState
		mutate      func(*workspaceLaunchStageAttempt)
	}{
		{name: "absent observation", observation: workspaceLaunchStageAbsent},
		{name: "unknown observation", observation: workspaceLaunchStageUnknown},
		{name: "unknown attempt", observation: workspaceLaunchStagePending, mutate: func(attempt *workspaceLaunchStageAttempt) {
			attempt.Unknown, attempt.Status = 1, "unknown"
		}},
		{name: "confirmed attempt", observation: workspaceLaunchStagePending, mutate: func(attempt *workspaceLaunchStageAttempt) {
			attempt.Confirmed, attempt.Status = 1, "confirmed"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := workspaceLaunchReservedStageManualReviewRow(t, "debit")
			operation, err := decodeWorkspaceLaunchReconcileOperation(row)
			if err != nil {
				t.Fatal(err)
			}
			operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: tc.observation}
			attempt := operation.Attempts[operation.Stage]
			if tc.mutate != nil {
				tc.mutate(&attempt)
				operation.Attempts[operation.Stage] = attempt
			}
			row, err = workspaceLaunchReconcileOperationRow(operation)
			if err != nil {
				t.Fatal(err)
			}
			authorization := workspaceLaunchReservedStageAuthorization(t, row, "resume-read-only-"+strings.ReplaceAll(tc.name, " ", "-"))
			authorization.IdempotentReplayBudget = 0
			store, adapter := &workspaceLaunchUnitStore{row: row}, &workspaceLaunchUnitAdapter{readyStages: map[string]bool{"debit": true}}
			persistedBefore := stringValue(row["result"])

			_, err = NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), operation.ID, authorization)
			if !errors.Is(err, errWorkspaceLaunchGrantConflict) || adapter.reads != 0 || adapter.mutations != 0 || stringValue(store.row["result"]) != persistedBefore {
				t.Fatalf("invalid read-only continuation changed operation: reads=%d mutations=%d err=%v", adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchReadOnlyContinuationExtendsPersistedTypedPending(t *testing.T) {
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		t.Run(string(stage), func(t *testing.T) {
			row := workspaceLaunchReservedStageManualReviewRow(t, stage)
			operation, err := decodeWorkspaceLaunchReconcileOperation(row)
			if err != nil {
				t.Fatal(err)
			}
			operation.Observations[operation.Stage] = workspaceLaunchStageObservation{State: workspaceLaunchStagePending}
			row, err = workspaceLaunchReconcileOperationRow(operation)
			if err != nil {
				t.Fatal(err)
			}
			authorization := workspaceLaunchReservedStageAuthorization(t, row, "resume-read-only-pending-"+string(stage))
			authorization.IdempotentReplayBudget = 0
			pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
			adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{string(stage): {pending, pending}}}

			got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), operation.ID, authorization)
			attempt := got.Attempts[stage]
			if err != nil || got.Status != "pending" || got.Stage != stage || attempt.Attempted != 1 || attempt.Max != 1 ||
				attempt.PendingReadbacks != 1 || attempt.MaxPendingReadbacks != workspaceLaunchAuthoritativeReadBudget || adapter.reads != 2 || adapter.mutations != 0 {
				t.Fatalf("typed pending continuation mismatch: operation=%s reads=%d mutations=%d err=%v", workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutations, err)
			}
		})
	}
}

func TestWorkspaceLaunchFreshTypedPendingCreatesReadOnlySystemAuthorization(t *testing.T) {
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		t.Run(string(stage), func(t *testing.T) {
			operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
			if err != nil {
				t.Fatal(err)
			}
			operation.Version, operation.Stage, operation.Status = 4, stage, "pending"
			row, err := workspaceLaunchReconcileOperationRow(operation)
			if err != nil {
				t.Fatal(err)
			}
			absent := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}
			pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
			adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{string(stage): {absent, pending}}}
			store := &workspaceLaunchUnitStore{row: row}

			got, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(context.Background(), operation.ID)
			attempt := got.Attempts[stage]
			expectedReadBudget := workspaceLaunchFreshContinuationReadBudget(stage)
			expectedReplayBudget := workspaceLaunchFreshContinuationReplayBudget(stage)
			if err != nil || got.Status != "pending" || got.Stage != stage || attempt.Attempted != 1 || attempt.Confirmed != 0 ||
				attempt.Unknown != 0 || attempt.Max != 1 || attempt.Status != "reserved" || attempt.PendingReadbacks != 1 ||
				attempt.MaxPendingReadbacks != 1+expectedReadBudget || adapter.reads != 2 || adapter.mutationsByStage[string(stage)] != 1 ||
				(stage == "ensure_compute_allocation") != (attempt.PendingDeadlineAt != "") ||
				got.ResumeAuthorization != nil {
				t.Fatalf("fresh typed pending did not persist system-only continuation: operation=%s reads=%d mutations=%#v resume=%#v err=%v",
					workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutationsByStage, got.ResumeAuthorization, err)
			}
			authorization, ok := got.FreshContinuationAuthorizations[stage]
			if !ok || authorization.AuthorizationClass != workspaceLaunchFreshContinuationAuthorizationClass || authorization.AccountID != got.stringFact("accountId") ||
				authorization.OperationID != got.ID || authorization.WorkspaceID != got.stringFact("workspaceId") || authorization.Stage != stage ||
				authorization.IdempotencyKey != attempt.IdempotencyKey || authorization.Attempt != 1 || authorization.OperationVersion != got.Version ||
				authorization.MutationBudget != 0 || authorization.IdempotentReplayBudget != expectedReplayBudget ||
				authorization.AuthoritativeReadBudget != expectedReadBudget || authorization.ReadbacksAtAuthorization != 1 ||
				authorization.Status != "active" || len(got.ContinuationReadClaims) != 0 {
				t.Fatalf("fresh continuation binding mismatch: authorization=%#v claims=%#v", authorization, got.ContinuationReadClaims)
			}
		})
	}
}

func workspaceLaunchFreshTypedPendingForTest(t *testing.T, stage contracts.Stage) (*workspaceLaunchUnitStore, *workspaceLaunchUnitAdapter, workspaceLaunchReconcileOperation) {
	t.Helper()
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.Version, operation.Stage, operation.Status = 4, stage, "pending"
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	absent := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}
	pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
	adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{string(stage): {absent, pending}}}
	store := &workspaceLaunchUnitStore{row: row}
	got, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(context.Background(), operation.ID)
	if err != nil || got.Status != "pending" || got.Stage != stage {
		t.Fatalf("seed fresh typed pending: operation=%s err=%v", workspaceLaunchReconcileResultSummary(got), err)
	}
	return store, adapter, got
}

func TestWorkspaceLaunchFreshTypedPendingReadTransitionMatrix(t *testing.T) {
	readError := errors.New("owner read error")
	for stageIndex, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		nextStage := workspaceLaunchReconcileStages[stageIndex+1]
		readyStatus := contracts.StatusPending
		if nextStage == contracts.StageSucceeded {
			readyStatus = contracts.StatusSucceeded
		}
		for _, tc := range []struct {
			name        string
			result      workspaceLaunchUnitReadResult
			wantStatus  contracts.LaunchStatus
			wantStage   contracts.Stage
			wantClaim   string
			wantUnknown int
		}{
			{name: "ready", result: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts(stage)}}, wantStatus: readyStatus, wantStage: nextStage, wantClaim: "ready"},
			{name: "absent conflict", result: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}, wantStatus: "manual_review", wantStage: stage, wantClaim: "failed", wantUnknown: 1},
			{name: "unknown", result: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}}, wantStatus: "manual_review", wantStage: stage, wantClaim: "failed", wantUnknown: 1},
			{name: "read error", result: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err: readError}, wantStatus: "manual_review", wantStage: stage, wantClaim: "failed", wantUnknown: 1},
		} {
			t.Run(string(stage)+"/"+tc.name, func(t *testing.T) {
				store, adapter, seeded := workspaceLaunchFreshTypedPendingForTest(t, stage)
				adapter.readResultsByStage[string(stage)] = []workspaceLaunchUnitReadResult{tc.result}
				got, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(context.Background(), seeded.ID)
				attempt := got.Attempts[stage]
				authorization := got.FreshContinuationAuthorizations[stage]
				claim := got.ContinuationReadClaims[workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, 2)]
				if err != nil || got.Status != tc.wantStatus || got.Stage != tc.wantStage || attempt.Attempted != 1 || attempt.Max != 1 ||
					attempt.PendingReadbacks != 2 || attempt.MaxPendingReadbacks != 1+workspaceLaunchFreshContinuationReadBudget(stage) || attempt.Unknown != tc.wantUnknown ||
					claim.Status != tc.wantClaim || adapter.reads != 3 || adapter.mutationsByStage[string(stage)] != 1 || got.ResumeAuthorization != nil {
					t.Fatalf("fresh continuation transition mismatch: operation=%s authorization=%#v claim=%#v reads=%d mutations=%#v err=%v",
						workspaceLaunchReconcileResultSummary(got), authorization, claim, adapter.reads, adapter.mutationsByStage, err)
				}
			})
		}
	}
}

func TestWorkspaceLaunchFreshTypedPendingExhaustsExactReadBudget(t *testing.T) {
	store, adapter, seeded := workspaceLaunchFreshTypedPendingForTest(t, "runtime")
	pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
	adapter.readResultsByStage["runtime"] = []workspaceLaunchUnitReadResult{pending, pending}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	continued, err := reconciler.Reconcile(context.Background(), seeded.ID)
	if err != nil || continued.Status != "pending" || continued.Attempts["runtime"].PendingReadbacks != 2 {
		t.Fatalf("first continuation read: operation=%s err=%v", workspaceLaunchReconcileResultSummary(continued), err)
	}
	got, err := reconciler.Reconcile(context.Background(), seeded.ID)
	attempt := got.Attempts["runtime"]
	authorization := got.FreshContinuationAuthorizations["runtime"]
	if err != nil || got.Status != "manual_review" || got.Stage != "runtime" || attempt.PendingReadbacks != 3 || attempt.MaxPendingReadbacks != 3 ||
		attempt.Unknown != 1 || attempt.Status != "unknown" || authorization.Status != "failed" || authorization.ConsumedAt == "" ||
		adapter.reads != 4 || adapter.mutationsByStage["runtime"] != 1 || got.Observations["runtime"].State != workspaceLaunchStageUnknown {
		t.Fatalf("fresh continuation exhaustion mismatch: operation=%s authorization=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), authorization, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchFreshComputePendingClaimsOwnershipWithOneSameKeyReplay(t *testing.T) {
	store, adapter, seeded := workspaceLaunchFreshTypedPendingForTest(t, "ensure_compute_allocation")
	adapter.replayableStages = map[string]bool{"ensure_compute_allocation": true}
	ownership := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageOwnershipPending}}
	adapter.readResultsByStage["ensure_compute_allocation"] = []workspaceLaunchUnitReadResult{ownership, ownership}

	got, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(context.Background(), seeded.ID)
	attempt := got.Attempts["ensure_compute_allocation"]
	authorization := got.FreshContinuationAuthorizations["ensure_compute_allocation"]
	claim := got.IdempotentReplayClaims["ensure_compute_allocation"]
	if err != nil || got.Status != "pending" || got.Stage != "storage" || attempt.Attempted != 1 || attempt.Max != 1 || attempt.Confirmed != 1 || attempt.Unknown != 0 ||
		adapter.mutationsByStage["ensure_compute_allocation"] != 2 || adapter.mutationIdempotencyKey != attempt.IdempotencyKey ||
		authorization.IdempotentReplayBudget != 1 || authorization.Status != "consumed" || claim.AuthorizationID != authorization.AuthorizationID || claim.Status != "succeeded" {
		t.Fatalf("fresh compute ownership continuation changed logical attempt: operation=%s attempt=%#v authorization=%#v claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, authorization, claim, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchFreshComputePendingDeadlineFailsClosedWithoutOwnerCall(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.Version, operation.Stage, operation.Status = 4, "ensure_compute_allocation", "pending"
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	absent := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}
	pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
	adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"ensure_compute_allocation": {absent, pending}}}
	store := &workspaceLaunchUnitStore{row: row}
	startedAt := time.Date(2026, 8, 23, 1, 0, 0, 0, time.UTC)
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	reconciler.now = func() time.Time { return startedAt }
	seeded, err := reconciler.Reconcile(context.Background(), operation.ID)
	if err != nil || seeded.Status != "pending" {
		t.Fatalf("seed compute pending: operation=%s err=%v", workspaceLaunchReconcileResultSummary(seeded), err)
	}
	readsBefore := adapter.reads
	reconciler.now = func() time.Time { return startedAt.Add(workspaceLaunchComputePendingWindow) }
	got, err := reconciler.Reconcile(context.Background(), operation.ID)
	if err != nil || got.Status != "manual_review" || got.Stage != "ensure_compute_allocation" || got.Attempts[got.Stage].Unknown != 1 ||
		got.Observations[got.Stage].State != workspaceLaunchStageUnknown || adapter.reads != readsBefore || adapter.mutationsByStage[string(got.Stage)] != 1 {
		t.Fatalf("compute deadline did not fail closed before another owner call: operation=%s reads=%d/%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), adapter.reads, readsBefore, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchFreshTypedPendingMutationResponseLossContinuesReadOnly(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.Version, operation.Stage, operation.Status = 4, "runtime", "pending"
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	absent := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}
	pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"runtime": {absent, pending}},
		mutationErrors:     map[string]error{"runtime": errors.New("transport response lost")},
	}
	store := &workspaceLaunchUnitStore{row: row}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	waiting, err := reconciler.Reconcile(context.Background(), operation.ID)
	if err != nil || waiting.Status != "pending" || waiting.Stage != "runtime" || adapter.mutationsByStage["runtime"] != 1 {
		t.Fatalf("response loss did not retain typed pending continuation: operation=%s mutations=%#v err=%v", workspaceLaunchReconcileResultSummary(waiting), adapter.mutationsByStage, err)
	}
	adapter.stageObservations = map[string]workspaceLaunchStageObservation{"runtime": {State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("runtime")}}
	got, err := reconciler.Reconcile(context.Background(), operation.ID)
	if err != nil || got.Stage != "activation" || got.Attempts["runtime"].Confirmed != 1 || adapter.mutationsByStage["runtime"] != 1 || adapter.reads != 3 {
		t.Fatalf("response loss continuation repeated mutation or failed convergence: operation=%s reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchFreshTypedPendingClaimSurvivesCrashWithoutRefund(t *testing.T) {
	store, adapter, seeded := workspaceLaunchFreshTypedPendingForTest(t, "runtime")
	startedAt := time.Date(2026, 8, 16, 1, 0, 0, 0, time.UTC)
	adapter.panicOnReadNumber = 3
	first := NewWorkspaceLaunchReconciler(store, adapter)
	first.now = func() time.Time { return startedAt }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected simulated read-claim crash")
			}
		}()
		_, _ = first.Reconcile(context.Background(), seeded.ID)
	}()
	durable, err := decodeWorkspaceLaunchReconcileOperation(store.row)
	authorization := durable.FreshContinuationAuthorizations["runtime"]
	claim2 := durable.ContinuationReadClaims[workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, 2)]
	if err != nil || durable.Attempts["runtime"].PendingReadbacks != 2 || claim2.Status != "claimed" || adapter.mutationsByStage["runtime"] != 1 {
		t.Fatalf("durable crash claim mismatch: operation=%s claim=%#v mutations=%#v err=%v", workspaceLaunchReconcileResultSummary(durable), claim2, adapter.mutationsByStage, err)
	}

	adapter.panicOnReadNumber = 0
	adapter.stageObservations = map[string]workspaceLaunchStageObservation{"runtime": {State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("runtime")}}
	restarted := NewWorkspaceLaunchReconciler(store, adapter)
	restarted.now = func() time.Time { return startedAt.Add(workspaceLaunchFreshContinuationReadClaimLease + time.Second) }
	got, err := restarted.Reconcile(context.Background(), seeded.ID)
	authorization = got.FreshContinuationAuthorizations["runtime"]
	claim2 = got.ContinuationReadClaims[workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, 2)]
	claim3 := got.ContinuationReadClaims[workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, 3)]
	if err != nil || got.Stage != "activation" || got.Attempts["runtime"].PendingReadbacks != 3 || claim2.Status != "expired" || claim3.Status != "ready" ||
		authorization.Status != "consumed" || adapter.reads != 4 || adapter.mutationsByStage["runtime"] != 1 {
		t.Fatalf("restart reused or refunded crashed claim: operation=%s authorization=%#v claims=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), authorization, got.ContinuationReadClaims, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestWorkspaceLaunchFreshTypedPendingConcurrentLoserStopsBeforeOwnerRead(t *testing.T) {
	store, adapter, seeded := workspaceLaunchFreshTypedPendingForTest(t, "runtime")
	adapter.stageObservations = map[string]workspaceLaunchStageObservation{"runtime": {State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("runtime")}}
	adapter.blockReadNumber, adapter.readStarted, adapter.releaseRead = 3, make(chan struct{}), make(chan struct{})
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	results := make(chan error, 2)
	go func() {
		_, err := reconciler.Reconcile(context.Background(), seeded.ID)
		results <- err
	}()
	<-adapter.readStarted
	go func() {
		_, err := reconciler.Reconcile(context.Background(), seeded.ID)
		results <- err
	}()
	secondErr := <-results
	adapter.mu.Lock()
	readsBeforeWinner := adapter.reads
	adapter.mu.Unlock()
	close(adapter.releaseRead)
	firstErr := <-results
	if firstErr != nil || secondErr != nil || readsBeforeWinner != 3 || adapter.mutationsByStage["runtime"] != 1 {
		t.Fatalf("concurrent loser crossed owner read: reads=%d mutations=%#v first=%v second=%v", readsBeforeWinner, adapter.mutationsByStage, firstErr, secondErr)
	}
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

func TestWorkspaceLaunchFreshTypedPendingStateAndClaimDriftIsRejected(t *testing.T) {
	seedStore, seedAdapter, seeded := workspaceLaunchFreshTypedPendingForTest(t, "runtime")
	pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
	seedAdapter.readResultsByStage["runtime"] = []workspaceLaunchUnitReadResult{pending}
	continued, err := NewWorkspaceLaunchReconciler(seedStore, seedAdapter).Reconcile(context.Background(), seeded.ID)
	if err != nil || continued.Attempts["runtime"].PendingReadbacks != 2 {
		t.Fatalf("seed continuation claim: operation=%s err=%v", workspaceLaunchReconcileResultSummary(continued), err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*workspaceLaunchReconcileOperation)
	}{
		{name: "operation status", mutate: func(value *workspaceLaunchReconcileOperation) { value.Status = "manual_review" }},
		{name: "attempt", mutate: func(value *workspaceLaunchReconcileOperation) {
			attempt := value.Attempts["runtime"]
			attempt.Attempted = 0
			value.Attempts["runtime"] = attempt
		}},
		{name: "missing claimed ordinal", mutate: func(value *workspaceLaunchReconcileOperation) {
			authorization := value.FreshContinuationAuthorizations["runtime"]
			delete(value.ContinuationReadClaims, workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, 2))
		}},
		{name: "claim idempotency", mutate: func(value *workspaceLaunchReconcileOperation) {
			authorization := value.FreshContinuationAuthorizations["runtime"]
			key := workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, 2)
			claim := value.ContinuationReadClaims[key]
			claim.IdempotencyKey += "-drift"
			value.ContinuationReadClaims[key] = claim
		}},
		{name: "claim readback", mutate: func(value *workspaceLaunchReconcileOperation) {
			authorization := value.FreshContinuationAuthorizations["runtime"]
			key := workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, 2)
			claim := value.ContinuationReadClaims[key]
			claim.Readback = 3
			value.ContinuationReadClaims[key] = claim
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			operation, err := decodeWorkspaceLaunchReconcileOperation(seedStore.row)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(&operation)
			row, err := workspaceLaunchReconcileOperationRow(operation)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeWorkspaceLaunchReconcileOperation(row); !errors.Is(err, errInvalidWorkspaceLaunchOperation) {
				t.Fatalf("fresh continuation state/claim drift accepted: operation=%s claims=%#v err=%v",
					workspaceLaunchReconcileResultSummary(operation), operation.ContinuationReadClaims, err)
			}
		})
	}
}

func TestWorkspaceLaunchReservedStageReplayCannotBeAuthorizedTwice(t *testing.T) {
	row := workspaceLaunchReservedStageManualReviewRow(t, "debit")
	store, adapter := &workspaceLaunchUnitStore{row: row}, &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{"debit": true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	firstAuthorization := workspaceLaunchReservedStageAuthorization(t, row, "resume-debit-first")
	first, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, firstAuthorization)
	if err != nil || adapter.mutations != 1 {
		t.Fatalf("first recovery failed: operation=%s mutations=%d err=%v", workspaceLaunchReconcileResultSummary(first), adapter.mutations, err)
	}

	first.Stage = "debit"
	first.Status = "manual_review"
	attempt := first.Attempts["debit"]
	attempt.Attempted, attempt.Confirmed, attempt.Unknown, attempt.Status = 1, 0, 0, "reserved"
	first.Attempts["debit"] = attempt
	first.Observations["debit"] = workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}
	store.row, err = workspaceLaunchReconcileOperationRow(first)
	if err != nil {
		t.Fatal(err)
	}
	adapter.readyStages["debit"] = false
	secondAuthorization := workspaceLaunchReservedStageAuthorization(t, store.row, "resume-debit-second")
	persistedBefore := stringValue(store.row["result"])
	if _, err = reconciler.Resume(context.Background(), first.ID, secondAuthorization); !errors.Is(err, errWorkspaceLaunchGrantConflict) || adapter.mutations != 1 || stringValue(store.row["result"]) != persistedBefore {
		t.Fatalf("second recovery authorization changed debit: mutations=%d err=%v", adapter.mutations, err)
	}
}

func TestWorkspaceLaunchReservedStageReplayCASAllowsOneWriter(t *testing.T) {
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		t.Run(string(stage), func(t *testing.T) {
			row := workspaceLaunchReservedStageManualReviewRow(t, stage)
			authorization := workspaceLaunchReservedStageAuthorization(t, row, "resume-"+string(stage)+"-concurrent")
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{barrier: make(chan struct{}), replayableStages: map[string]bool{string(stage): true}}
			reconciler := NewWorkspaceLaunchReconciler(store, adapter)
			start := make(chan struct{})
			results := make(chan error, 2)
			for range 2 {
				go func() {
					<-start
					_, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
					results <- err
				}()
			}
			close(start)
			var successes, conflicts int
			for range 2 {
				switch err := <-results; {
				case err == nil:
					successes++
				case errors.Is(err, errWorkspaceLaunchCASConflict):
					conflicts++
				default:
					t.Fatalf("unexpected concurrent recovery error: %v", err)
				}
			}
			if successes != 1 || conflicts != 1 || adapter.mutationsByStage[string(stage)] != 1 {
				t.Fatalf("successes=%d conflicts=%d %s mutations=%d", successes, conflicts, stage, adapter.mutationsByStage[string(stage)])
			}
		})
	}
}

func TestWorkspaceLaunchReservedStageReplaySurvivesCrashBeforeTransportSend(t *testing.T) {
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		t.Run(string(stage), func(t *testing.T) {
			row := workspaceLaunchReservedStageManualReviewRow(t, stage)
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{
				replayableStages:     map[string]bool{string(stage): true},
				panicBeforeMutations: map[string]int{string(stage): 1},
			}
			startedAt := time.Date(2026, 8, 15, 2, 0, 0, 0, time.UTC)
			reconciler := NewWorkspaceLaunchReconciler(store, adapter)
			reconciler.now = func() time.Time { return startedAt }
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("expected simulated process crash")
					}
				}()
				_, _ = reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, workspaceLaunchReservedStageAuthorization(t, row, "resume-crash-"+string(stage)))
			}()

			claimed, err := decodeWorkspaceLaunchReconcileOperation(store.row)
			if err != nil || claimed.Status != "pending" || claimed.Stage != stage || claimed.IdempotentReplayClaims[stage].Status != "claimed" ||
				claimed.ResumeAuthorization == nil || claimed.ResumeAuthorizationConsumedAt != "" || adapter.mutations != 0 {
				t.Fatalf("crash cut not durable: operation=%s claim=%#v mutations=%d err=%v", workspaceLaunchReconcileResultSummary(claimed), claimed.IdempotentReplayClaims[stage], adapter.mutations, err)
			}

			restarted := NewWorkspaceLaunchReconciler(store, adapter)
			restarted.now = func() time.Time { return startedAt.Add(workspaceLaunchIdempotentReplayLease + time.Second) }
			got, err := restarted.Reconcile(context.Background(), claimed.ID)
			if err != nil || got.Attempts[stage].Attempted != 1 || got.Attempts[stage].Max != 1 || got.Attempts[stage].Confirmed != 1 ||
				got.IdempotentReplayClaims[stage].Status != "succeeded" || adapter.mutationsByStage[string(stage)] != 1 || adapter.mutationIdempotencyKey != claimed.Attempts[stage].IdempotencyKey {
				t.Fatalf("restart did not recover exact replay: operation=%s claim=%#v mutations=%#v err=%v", workspaceLaunchReconcileResultSummary(got), got.IdempotentReplayClaims[stage], adapter.mutationsByStage, err)
			}
		})
	}
}

func TestWorkspaceLaunchReservedStageReplayPostReadMatrix(t *testing.T) {
	mutationErr := errors.New("transport response lost")
	for stageIndex, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		nextStage := workspaceLaunchReconcileStages[stageIndex+1]
		readyStatus := contracts.StatusPending
		if nextStage == contracts.StageSucceeded {
			readyStatus = contracts.StatusSucceeded
		}
		cases := []struct {
			name              string
			mutationErr       error
			postRead          workspaceLaunchUnitReadResult
			wantStatus        contracts.LaunchStatus
			wantStage         contracts.Stage
			wantClaim         string
			wantUnknown       int
			wantReturnedError bool
		}{
			{name: "success ready", postRead: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts(stage)}}, wantStatus: readyStatus, wantStage: nextStage, wantClaim: "succeeded"},
			{name: "error ready", mutationErr: mutationErr, postRead: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts(stage)}}, wantStatus: readyStatus, wantStage: nextStage, wantClaim: "succeeded"},
			{name: "success pending", postRead: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}, wantStatus: "pending", wantStage: stage, wantClaim: "waiting"},
			{name: "error pending", mutationErr: mutationErr, postRead: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}, wantStatus: "pending", wantStage: stage, wantClaim: "waiting"},
			{name: "success absent", postRead: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}, wantStatus: "manual_review", wantStage: stage, wantClaim: "failed", wantUnknown: 1},
			{name: "error absent", mutationErr: mutationErr, postRead: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}, wantStatus: "manual_review", wantStage: stage, wantClaim: "failed", wantUnknown: 1, wantReturnedError: true},
			{name: "success unknown", postRead: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}}, wantStatus: "manual_review", wantStage: stage, wantClaim: "failed", wantUnknown: 1},
			{name: "error read error", mutationErr: mutationErr, postRead: workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}, err: errors.New("owner read failed")}, wantStatus: "manual_review", wantStage: stage, wantClaim: "failed", wantUnknown: 1, wantReturnedError: true},
		}
		for _, tc := range cases {
			t.Run(string(stage)+"/"+tc.name, func(t *testing.T) {
				row := workspaceLaunchReservedStageManualReviewRow(t, stage)
				absent := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}
				adapter := &workspaceLaunchUnitAdapter{
					replayableStages: map[string]bool{string(stage): true}, mutationErrors: map[string]error{string(stage): tc.mutationErr},
					readResultsByStage: map[string][]workspaceLaunchUnitReadResult{string(stage): {absent, absent, absent, tc.postRead}},
				}
				got, err := NewWorkspaceLaunchReconciler(&workspaceLaunchUnitStore{row: row}, adapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, workspaceLaunchReservedStageAuthorization(t, row, "resume-post-read-"+string(stage)+"-"+strings.ReplaceAll(tc.name, " ", "-")))
				attempt := got.Attempts[stage]
				if (err != nil) != tc.wantReturnedError || got.Status != tc.wantStatus || got.Stage != tc.wantStage || got.IdempotentReplayClaims[stage].Status != tc.wantClaim ||
					attempt.Attempted != 1 || attempt.Max != 1 || attempt.Unknown != tc.wantUnknown || adapter.mutationsByStage[string(stage)] != 1 {
					t.Fatalf("post-read transition mismatch: operation=%s attempt=%#v claim=%#v mutations=%#v err=%v", workspaceLaunchReconcileResultSummary(got), attempt, got.IdempotentReplayClaims[stage], adapter.mutationsByStage, err)
				}
			})
		}
	}
}

func TestWorkspaceLaunchPendingReadbackIsBoundedAndCanConvergeReadOnly(t *testing.T) {
	for stageIndex, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		nextStage := workspaceLaunchReconcileStages[stageIndex+1]
		readyStatus := contracts.StatusPending
		if nextStage == contracts.StageSucceeded {
			readyStatus = contracts.StatusSucceeded
		}
		for _, tc := range []struct {
			name       string
			followups  []workspaceLaunchUnitReadResult
			wantStatus contracts.LaunchStatus
			wantStage  contracts.Stage
		}{
			{name: "pending then ready", followups: []workspaceLaunchUnitReadResult{{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}, {observation: workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts(stage)}}}, wantStatus: readyStatus, wantStage: nextStage},
			{name: "permanent pending exhausts", followups: []workspaceLaunchUnitReadResult{{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}, {observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}}, wantStatus: "manual_review", wantStage: stage},
		} {
			t.Run(string(stage)+"/"+tc.name, func(t *testing.T) {
				row := workspaceLaunchReservedStageManualReviewRow(t, stage)
				absent := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}
				pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
				adapter := &workspaceLaunchUnitAdapter{
					replayableStages:   map[string]bool{string(stage): true},
					readResultsByStage: map[string][]workspaceLaunchUnitReadResult{string(stage): append([]workspaceLaunchUnitReadResult{absent, absent, absent, pending}, tc.followups...)},
				}
				store := &workspaceLaunchUnitStore{row: row}
				reconciler := NewWorkspaceLaunchReconciler(&workspaceLaunchValidatingUnitStore{workspaceLaunchUnitStore: store}, adapter)
				got, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, workspaceLaunchReservedStageAuthorization(t, row, "resume-pending-"+string(stage)+"-"+strings.ReplaceAll(tc.name, " ", "-")))
				for err == nil && got.Status == "pending" && got.Stage == stage && got.Attempts[stage].PendingReadbacks < got.Attempts[stage].MaxPendingReadbacks {
					got, err = reconciler.Reconcile(context.Background(), got.ID)
				}
				attempt := got.Attempts[stage]
				if err != nil || got.Status != tc.wantStatus || got.Stage != tc.wantStage || adapter.mutationsByStage[string(stage)] != 1 ||
					attempt.PendingReadbacks > attempt.MaxPendingReadbacks {
					t.Fatalf("bounded pending mismatch: operation=%s attempt=%#v mutations=%#v err=%v", workspaceLaunchReconcileResultSummary(got), attempt, adapter.mutationsByStage, err)
				}
				if tc.wantStatus == "manual_review" && (attempt.Unknown != 1 || attempt.Status != "unknown" || got.Observations[stage].State != workspaceLaunchStageUnknown ||
					got.IdempotentReplayClaims[stage].Status != "failed" || got.ResumeAuthorizationConsumedAt == "" || got.Observations[stage].State == workspaceLaunchStageAbsent) {
					t.Fatalf("exhaustion inferred absence or left authorization active: operation=%s attempt=%#v", workspaceLaunchReconcileResultSummary(got), attempt)
				}
			})
		}
	}
}

func TestWorkspaceLaunchPendingReadbackRequiresPersistedOperatorAuthorization(t *testing.T) {
	row := workspaceLaunchReservedStageManualReviewRow(t, "debit")
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	operation.Status = "pending"
	operation.Observations["debit"] = workspaceLaunchStageObservation{State: workspaceLaunchStagePending}
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{stageObservations: map[string]workspaceLaunchStageObservation{
		"debit": {State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("debit")},
	}}

	got, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(context.Background(), operation.ID)
	if err != nil || got.Status != "manual_review" || got.Stage != "debit" || adapter.reads != 0 || adapter.mutations != 0 {
		t.Fatalf("unauthorized pending continuation read owner state: operation=%s reads=%d mutations=%d err=%v", workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutations, err)
	}
}

func TestWorkspaceLaunchLegacyV3MissingReadBudgetDefaultsToSafeStop(t *testing.T) {
	row := workspaceLaunchReservedStageManualReviewRow(t, "debit")
	var result map[string]any
	if err := json.Unmarshal([]byte(stringValue(row["result"])), &result); err != nil {
		t.Fatal(err)
	}
	attempts := mapField(result, "attempts")
	debit := mapField(attempts, "debit")
	delete(debit, "pendingReadbacks")
	delete(debit, "maxPendingReadbacks")
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	row["result"] = string(encoded)

	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	if got := operation.Attempts["debit"]; got.PendingReadbacks != 0 || got.MaxPendingReadbacks != workspaceLaunchLegacyV3AuthoritativeReadBudget ||
		len(operation.FreshContinuationAuthorizations) != 0 || len(operation.ContinuationReadClaims) != 0 {
		t.Fatalf("legacy compatibility invented owner facts or reads: attempt=%#v", got)
	}
	operation.Status = "pending"
	operation.Observations["debit"] = workspaceLaunchStageObservation{State: workspaceLaunchStagePending}
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{"debit": true}, replayableStages: map[string]bool{"debit": true}}
	got, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(context.Background(), operation.ID)
	if err != nil || got.Status != "manual_review" || got.Stage != "debit" || adapter.reads != 0 || adapter.mutations != 0 {
		t.Fatalf("legacy zero budget performed owner work: operation=%s reads=%d mutations=%d err=%v", workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutations, err)
	}
}

func TestWorkspaceLaunchRecoveryAtEveryStageContinuesOriginalOperationToSucceeded(t *testing.T) {
	for _, failedStage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		t.Run(string(failedStage), func(t *testing.T) {
			row := workspaceLaunchReservedStageManualReviewRow(t, failedStage)
			store := &workspaceLaunchUnitStore{row: row}
			adapter := &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{string(failedStage): true}}
			reconciler := NewWorkspaceLaunchReconciler(store, adapter)
			got, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, workspaceLaunchReservedStageAuthorization(t, row, "resume-terminal-"+string(failedStage)))
			for err == nil && got.Status == "pending" {
				got, err = reconciler.Reconcile(context.Background(), got.ID)
			}
			if err != nil || got.Status != "succeeded" || got.Stage != "succeeded" || got.ID != workspaceLaunchUnitCommand().OperationID || got.stringFact("workspaceId") != workspaceLaunchUnitCommand().WorkspaceID {
				t.Fatalf("recovery did not reach terminal: operation=%s mutations=%#v err=%v", workspaceLaunchReconcileResultSummary(got), adapter.mutationsByStage, err)
			}
			for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
				if adapter.mutationsByStage[string(stage)] > 1 {
					t.Fatalf("stage %s mutated %d times after %s recovery", stage, adapter.mutationsByStage[string(stage)], failedStage)
				}
			}
		})
	}
}

func TestWorkspaceLaunchReceiptOnlyReplayReachesTerminalWithoutRepeatingPriorStages(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-2] {
		operation.Stage = stage
		observation, reduceErr := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts(stage)})
		if reduceErr != nil {
			t.Fatalf("seed %s: %v", stage, reduceErr)
		}
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Status = 1, 1, "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
		operation.Attempts[stage] = attempt
		operation.Observations[stage] = observation
	}
	operation.Version, operation.Stage, operation.Status = 17, "receipt", "manual_review"
	receiptAttempt := operation.Attempts["receipt"]
	receiptAttempt.Attempted, receiptAttempt.Status = 1, "reserved"
	receiptAttempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
	operation.Attempts["receipt"] = receiptAttempt
	operation.Observations["receipt"] = workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{"receipt": true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	authorization := workspaceLaunchReservedStageAuthorization(t, row, "resume-receipt-only")
	got, err := reconciler.Resume(context.Background(), operation.ID, authorization)
	if err != nil || got.Status != "succeeded" || got.Stage != "succeeded" || got.stringFact("workspaceId") != operation.stringFact("workspaceId") ||
		got.stringFact("sub2apiRedeemCode") != operation.stringFact("sub2apiRedeemCode") || got.int64Fact("totalChargeUsdMicros") != operation.int64Fact("totalChargeUsdMicros") ||
		got.stringFact("receiptId") != "receipt-unit" || got.stringFact("receiptOperationId") != operation.ID+":purchase-receipt" || adapter.mutationsByStage["receipt"] != 1 {
		t.Fatalf("receipt-only recovery mismatch: operation=%s mutations=%#v err=%v", workspaceLaunchReconcileResultSummary(got), adapter.mutationsByStage, err)
	}
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-2] {
		if adapter.mutationsByStage[string(stage)] != 0 {
			t.Fatalf("receipt recovery repeated %s mutation", stage)
		}
	}
	readsBefore, mutationsBefore, persistedBefore := adapter.reads, adapter.mutations, stringValue(store.row["result"])
	replayed, err := reconciler.Resume(context.Background(), operation.ID, authorization)
	if err != nil || replayed.Status != "succeeded" || adapter.reads != readsBefore || adapter.mutations != mutationsBefore || stringValue(store.row["result"]) != persistedBefore {
		t.Fatalf("exact receipt authorization replay caused work: operation=%s reads=%d/%d mutations=%d/%d err=%v", workspaceLaunchReconcileResultSummary(replayed), adapter.reads, readsBefore, adapter.mutations, mutationsBefore, err)
	}
}

func TestWorkspaceLaunchReceiptProjectionCASIsIdempotentAndFailClosed(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		operation.Stage = stage
		observation, reduceErr := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts(stage)})
		if reduceErr != nil {
			t.Fatalf("reduce %s: %v", stage, reduceErr)
		}
		operation.Observations[stage] = observation
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Status = 1, 1, "confirmed"
		operation.Attempts[stage] = attempt
		operation.advance()
	}
	if operation.Stage != "succeeded" || operation.Status != "succeeded" || operation.stringFact("receiptId") != "receipt-unit" {
		t.Fatalf("terminal operation=%s", workspaceLaunchReconcileResultSummary(operation))
	}
	projection, err := workspaceLaunchReceiptProjectionFor(operation)
	if err != nil {
		t.Fatal(err)
	}
	store := newMemoryTableStore()
	store.accounts["acct-unit"] = map[string]any{"id": "acct-unit", "ownerUserId": "usr-unit", "status": "active"}
	store.workspaces["ws-unit"] = map[string]any{
		"id": "ws-unit", "accountId": "acct-unit", "ownerAccountId": "acct-unit", "ownerUserId": "usr-unit", "purchaseReceiptId": "",
	}
	claimed, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ClaimWorkspaceLaunchReconcile(context.Background(), workspaceLaunchReconcileClaim{AccountID: "acct-unit", DesiredOperation: claimed}); err != nil {
		t.Fatal(err)
	}

	operation.Version++
	first, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PersistWorkspaceLaunchReconcile(context.Background(), workspaceLaunchReconcileCAS{
		OperationID: operation.ID, ExpectedOperationResult: stringValue(claimed["result"]), DesiredOperation: first, WorkspaceReceiptProjection: projection,
	}); err != nil {
		t.Fatalf("first projection=%v", err)
	}
	workspace := store.workspaces["ws-unit"]
	if stringValue(workspace["purchaseReceiptId"]) != "receipt-unit" {
		t.Fatalf("workspace receipt=%#v", workspace)
	}

	operation.Version++
	second, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PersistWorkspaceLaunchReconcile(context.Background(), workspaceLaunchReconcileCAS{
		OperationID: operation.ID, ExpectedOperationResult: stringValue(first["result"]), DesiredOperation: second, WorkspaceReceiptProjection: projection,
	}); err != nil {
		t.Fatalf("same receipt replay=%v", err)
	}

	operation.Version++
	conflict, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store.workspaces["ws-unit"]["purchaseReceiptId"] = "receipt-other"
	if err := store.PersistWorkspaceLaunchReconcile(context.Background(), workspaceLaunchReconcileCAS{
		OperationID: operation.ID, ExpectedOperationResult: stringValue(second["result"]), DesiredOperation: conflict, WorkspaceReceiptProjection: projection,
	}); !errors.Is(err, errIdempotencyConflict) {
		t.Fatalf("different receipt error=%v", err)
	}
	if stringValue(store.workspaces["ws-unit"]["purchaseReceiptId"]) != "receipt-other" {
		t.Fatalf("conflicting projection mutated workspace=%#v", store.workspaces["ws-unit"])
	}

	operation.Version++
	identityConflict, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	identityProjection := *projection
	identityProjection.OwnerUserID = "usr-other"
	if err := store.PersistWorkspaceLaunchReconcile(context.Background(), workspaceLaunchReconcileCAS{
		OperationID: operation.ID, ExpectedOperationResult: stringValue(second["result"]), DesiredOperation: identityConflict, WorkspaceReceiptProjection: &identityProjection,
	}); !errors.Is(err, errWorkspaceLaunchCASConflict) {
		t.Fatalf("identity-drift projection error=%v", err)
	}
}

func TestWorkspaceLaunchCreateAndResumeUseReconcile(t *testing.T) {
	createStore, createAdapter := &workspaceLaunchUnitStore{}, &workspaceLaunchUnitAdapter{}
	created, err := NewWorkspaceLaunchReconciler(createStore, createAdapter).Create(context.Background(), workspaceLaunchUnitCommand())
	if err != nil || created.Stage != "debit" || createAdapter.mutations != 1 {
		t.Fatalf("create operation=%s mutations=%d err=%v", workspaceLaunchReconcileResultSummary(created), createAdapter.mutations, err)
	}

	resumeStore, resumeAdapter := &workspaceLaunchUnitStore{row: workspaceLaunchManualReviewRow(t)}, &workspaceLaunchUnitAdapter{}
	resumed, err := NewWorkspaceLaunchReconciler(resumeStore, resumeAdapter).Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-unit", LaunchVersion: 1, AuthorizedStage: "key", AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-12T00:01:00Z", Reason: "authoritative readback approved", MutationBudget: 1,
	})
	if err != nil || resumed.Stage != "debit" || resumeAdapter.mutations != 1 || resumed.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("resume operation=%s mutations=%d err=%v", workspaceLaunchReconcileResultSummary(resumed), resumeAdapter.mutations, err)
	}
}

func TestWorkspaceLaunchCASAllowsOneMutationReservation(t *testing.T) {
	store := &workspaceLaunchUnitStore{row: workspaceLaunchManualReviewRow(t)}
	operation, err := decodeWorkspaceLaunchReconcileOperation(store.row)
	if err != nil {
		t.Fatal(err)
	}
	operation.Status = "pending"
	store.row, _ = workspaceLaunchReconcileOperationRow(operation)
	adapter := &workspaceLaunchUnitAdapter{barrier: make(chan struct{})}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	start := make(chan struct{})
	results := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			_, err := reconciler.Reconcile(context.Background(), operation.ID)
			results <- err
		}()
	}
	close(start)
	var successes, conflicts int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, errWorkspaceLaunchCASConflict):
			conflicts++
		default:
			t.Fatalf("unexpected reconcile error: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 || adapter.mutations != 1 {
		t.Fatalf("successes=%d conflicts=%d mutations=%d", successes, conflicts, adapter.mutations)
	}
}

func TestWorkspaceLaunchPreAttemptReadFailureRemainsPending(t *testing.T) {
	row := workspaceLaunchManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	operation.Status = "pending"
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{readErrors: map[string]error{"key": errors.New("transient read failure")}}
	persistedBefore := stringValue(row["result"])

	got, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(context.Background(), operation.ID)
	if err != nil || got.Status != "pending" || got.Stage != "key" || got.Attempts["key"].Attempted != 0 ||
		got.ResumeAuthorization != nil || adapter.mutations != 0 || stringValue(store.row["result"]) != persistedBefore {
		t.Fatalf("pre-attempt read failure changed launch: operation=%s mutations=%d err=%v", workspaceLaunchReconcileResultSummary(got), adapter.mutations, err)
	}
}

func TestWorkspaceLaunchAuthorizedMutationWaitsForCapableCaller(t *testing.T) {
	store := &workspaceLaunchUnitStore{row: workspaceLaunchManualReviewRow(t)}
	adapter := &workspaceLaunchUnitAdapter{mutationBlocked: true}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	authorization := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-caller-credential", LaunchVersion: 1, AuthorizedStage: "key", AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-12T00:01:00Z", Reason: "bounded retry", MutationBudget: 1,
	}

	waiting, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
	if err != nil || waiting.Status != "pending" || waiting.Stage != "key" || waiting.Attempts["key"].Attempted != 0 ||
		waiting.ResumeAuthorization == nil || *waiting.ResumeAuthorization != authorization || waiting.ResumeAuthorizationConsumedAt != "" || adapter.mutations != 0 {
		t.Fatalf("blocked caller consumed authorization: operation=%s mutations=%d err=%v", workspaceLaunchReconcileResultSummary(waiting), adapter.mutations, err)
	}

	adapter.mutationBlocked = false
	continued, err := reconciler.Reconcile(context.Background(), waiting.ID)
	if err != nil || continued.Status != "pending" || continued.Stage != "debit" || continued.Attempts["key"].Attempted != 1 ||
		continued.ResumeAuthorization == nil || *continued.ResumeAuthorization != authorization || continued.ResumeAuthorizationConsumedAt == "" || adapter.mutations != 1 {
		t.Fatalf("capable caller did not continue launch: operation=%s mutations=%d err=%v", workspaceLaunchReconcileResultSummary(continued), adapter.mutations, err)
	}
}

func TestWorkspaceLaunchResumeAuthorizationIsImmutable(t *testing.T) {
	store, adapter := &workspaceLaunchUnitStore{row: workspaceLaunchManualReviewRow(t)}, &workspaceLaunchUnitAdapter{}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	authorization := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-unit", LaunchVersion: 1, AuthorizedStage: "key", AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-12T00:01:00Z", Reason: "bounded retry", MutationBudget: 1,
	}
	first, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, authorization)
	if err != nil || second.ID != first.ID || second.stringFact("workspaceId") != first.stringFact("workspaceId") || second.Attempts["key"].Max != 1 {
		t.Fatalf("idempotent resume changed launch: first=%s second=%s err=%v", workspaceLaunchReconcileResultSummary(first), workspaceLaunchReconcileResultSummary(second), err)
	}
	drifted := authorization
	drifted.Reason = "different reason"
	if _, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, drifted); !errors.Is(err, errWorkspaceLaunchGrantConflict) {
		t.Fatalf("drifted authorization error=%v", err)
	}
}

func TestWorkspaceLaunchResumeAuthorizationsRotateAcrossStages(t *testing.T) {
	store := &workspaceLaunchUnitStore{row: workspaceLaunchManualReviewRow(t)}
	adapter := &workspaceLaunchUnitAdapter{unknownStages: map[string]bool{}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	stageA := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-stage-a", LaunchVersion: 1, AuthorizedStage: "key", AuthorizedBy: "usr-admin-a",
		AuthorizedAt: "2026-08-12T00:01:00Z", Reason: "stage A reviewed", MutationBudget: 1,
	}
	afterA, err := reconciler.Resume(context.Background(), workspaceLaunchUnitCommand().OperationID, stageA)
	if err != nil || afterA.Stage != "debit" || afterA.ResumeAuthorization == nil || *afterA.ResumeAuthorization != stageA || afterA.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("stage A resume=%s err=%v", workspaceLaunchReconcileResultSummary(afterA), err)
	}

	adapter.unknownStages["debit"] = true
	reviewed, err := reconciler.Reconcile(context.Background(), afterA.ID)
	if err != nil || reviewed.Status != "manual_review" || reviewed.Stage != "debit" || reviewed.Attempts["debit"].Attempted != 0 {
		t.Fatalf("stage B review=%s err=%v", workspaceLaunchReconcileResultSummary(reviewed), err)
	}
	stageB := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-stage-b", LaunchVersion: reviewed.Version, AuthorizedStage: reviewed.Stage, AuthorizedBy: "usr-admin-b",
		AuthorizedAt: "2026-08-12T00:02:00Z", Reason: "stage B reviewed", MutationBudget: 1,
	}
	adapter.unknownStages["debit"] = false
	afterB, err := reconciler.Resume(context.Background(), reviewed.ID, stageB)
	if err != nil || afterB.Stage != "ensure_compute_allocation" || afterB.ResumeAuthorization == nil || *afterB.ResumeAuthorization != stageB || afterB.ResumeAuthorizationConsumedAt == "" ||
		len(afterB.ConsumedResumeAuthorizations) != 1 || afterB.ConsumedResumeAuthorizations[0].Authorization != stageA || afterB.ConsumedResumeAuthorizations[0].ConsumedAt == "" ||
		adapter.mutationsByStage["key"] != 1 || adapter.mutationsByStage["debit"] != 1 {
		t.Fatalf("stage B resume=%s history=%#v mutations=%#v err=%v", workspaceLaunchReconcileResultSummary(afterB), afterB.ConsumedResumeAuthorizations, adapter.mutationsByStage, err)
	}

	persistedBefore := stringValue(store.row["result"])
	readsBefore, mutationsBefore := adapter.reads, adapter.mutations
	for name, authorization := range map[string]workspaceLaunchResumeAuthorization{"stage A": stageA, "stage B": stageB} {
		got, retryErr := reconciler.Resume(context.Background(), afterB.ID, authorization)
		if retryErr != nil || got.ResumeAuthorization == nil || *got.ResumeAuthorization != stageB || len(got.ConsumedResumeAuthorizations) != 1 {
			t.Fatalf("%s exact retry changed authorization: operation=%s err=%v", name, workspaceLaunchReconcileResultSummary(got), retryErr)
		}
	}
	if adapter.reads != readsBefore || adapter.mutations != mutationsBefore || stringValue(store.row["result"]) != persistedBefore {
		t.Fatalf("exact retry caused work: reads=%d/%d mutations=%d/%d", adapter.reads, readsBefore, adapter.mutations, mutationsBefore)
	}

	drifts := []struct {
		name   string
		mutate func(*workspaceLaunchResumeAuthorization)
	}{
		{name: "authorization ID", mutate: func(value *workspaceLaunchResumeAuthorization) { value.AuthorizationID += "-changed" }},
		{name: "launch version", mutate: func(value *workspaceLaunchResumeAuthorization) { value.LaunchVersion++ }},
		{name: "stage", mutate: func(value *workspaceLaunchResumeAuthorization) { value.AuthorizedStage = "storage" }},
		{name: "reviewer", mutate: func(value *workspaceLaunchResumeAuthorization) { value.AuthorizedBy += "-changed" }},
		{name: "time", mutate: func(value *workspaceLaunchResumeAuthorization) { value.AuthorizedAt = "2026-08-12T00:03:00Z" }},
		{name: "reason", mutate: func(value *workspaceLaunchResumeAuthorization) { value.Reason += " changed" }},
		{name: "budget", mutate: func(value *workspaceLaunchResumeAuthorization) { value.MutationBudget = 0 }},
	}
	for stageName, authorization := range map[string]workspaceLaunchResumeAuthorization{"stage A": stageA, "stage B": stageB} {
		for _, drift := range drifts {
			drifted := authorization
			drift.mutate(&drifted)
			if _, driftErr := reconciler.Resume(context.Background(), afterB.ID, drifted); !errors.Is(driftErr, errWorkspaceLaunchGrantConflict) {
				t.Fatalf("%s %s drift error=%v", stageName, drift.name, driftErr)
			}
		}
	}
	if adapter.reads != readsBefore || adapter.mutations != mutationsBefore || stringValue(store.row["result"]) != persistedBefore {
		t.Fatalf("authorization drift changed launch state")
	}
}

func TestWorkspaceLaunchResumeAuthorizationReadbackFindsConsumedHistory(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	first := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-history-first", LaunchVersion: 1, AuthorizedStage: "key", AuthorizedBy: "operator-a",
		AuthorizedAt: "2026-08-16T00:00:00Z", Reason: "first stage", MutationBudget: 1,
	}
	second := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-history-second", LaunchVersion: 2, AuthorizedStage: "debit", AuthorizedBy: "operator-b",
		AuthorizedAt: "2026-08-16T00:01:00Z", Reason: "second stage", MutationBudget: 0, IdempotentReplayBudget: 1, AuthoritativeReadBudget: 3,
	}
	operation.rotateResumeAuthorization(first)
	operation.consumeResumeAuthorization(time.Date(2026, 8, 16, 0, 0, 30, 0, time.UTC))
	operation.rotateResumeAuthorization(second)

	readback, found := workspaceLaunchResumeAuthorizationReadback(operation, first.AuthorizationID)
	attempt, _ := readback["attempt"].(map[string]any)
	if !found || readback["status"] != "consumed" || readback["authorizedBy"] != "operator-a" ||
		readback["consumedAt"] != "2026-08-16T00:00:30Z" || readback["singleUse"] != true ||
		int(numberField(readback, "authorizationVersion", 0)) != 1 || !exactWorkspaceComputeClaimKeys(attempt, []string{
		"attempted", "confirmed", "unknown", "max", "status", "idempotencyKey", "pendingReadbacks", "maxPendingReadbacks",
	}) || attempt["status"] != "" || attempt["idempotencyKey"] != "" || int(numberField(attempt, "pendingReadbacks", -1)) != 0 {
		t.Fatalf("historical readback found=%v value=%#v", found, readback)
	}
}

func TestWorkspaceLaunchResumeWrapperReusesAuthorizedAtOnExactRetry(t *testing.T) {
	row := workspaceLaunchManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	authorization := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-route-unit", LaunchVersion: 1, AuthorizedStage: "key", AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-12T00:01:00Z", Reason: "bounded retry", MutationBudget: 1,
	}
	current := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-route-current", LaunchVersion: 3, AuthorizedStage: "debit", AuthorizedBy: "usr-admin-current",
		AuthorizedAt: "2026-08-12T00:02:00Z", Reason: "current bounded retry", MutationBudget: 1,
	}
	operation.Version = 4
	operation.Stage = "ensure_compute_allocation"
	operation.Status = "pending"
	operation.ConsumedResumeAuthorizations = []workspaceLaunchConsumedResumeAuthorization{{Authorization: authorization, ConsumedAt: "2026-08-12T00:01:30Z"}}
	operation.ResumeAuthorization = &current
	operation.ResumeAuthorizationConsumedAt = "2026-08-12T00:02:00Z"
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store := newMemoryTableStore()
	store.runtimeOps = []map[string]any{row}
	app := &controlPlaneServer{tables: store}
	retry := authorization
	retry.AuthorizedAt = ""
	persistedBefore := stringValue(row["result"])
	got, err := app.resumeWorkspaceLaunch(context.Background(), nil, operation.ID, retry)
	if err != nil || got.ResumeAuthorization == nil || *got.ResumeAuthorization != current || len(got.ConsumedResumeAuthorizations) != 1 ||
		got.ConsumedResumeAuthorizations[0].Authorization != authorization || stringValue(store.runtimeOps[0]["result"]) != persistedBefore {
		t.Fatalf("exact retry changed authorization: operation=%s err=%v", workspaceLaunchReconcileResultSummary(got), err)
	}
}

func TestWorkspaceLaunchLedgerEvidenceCannotAuthorizeContinuation(t *testing.T) {
	row := workspaceLaunchManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	operation.raw["receiptId"] = json.RawMessage(`"receipt-evidence"`)
	operation.raw["continuationRef"] = json.RawMessage(`"ledger-continuation"`)
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store, adapter := &workspaceLaunchUnitStore{row: row}, &workspaceLaunchUnitAdapter{}
	got, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(context.Background(), operation.ID)
	if err != nil || got.Status != "manual_review" || got.ResumeAuthorization != nil || adapter.reads != 0 || adapter.mutations != 0 {
		t.Fatalf("ledger evidence continued launch: operation=%s reads=%d mutations=%d err=%v", workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutations, err)
	}
}

func TestWorkspaceLaunchFabricBindingDriftBecomesUnknown(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.Stage = "storage"
	input := clients.WorkspaceLaunchStageInput{Binding: clients.WorkspaceLaunchStageBinding{SchemaVersion: 1, LaunchOperationID: operation.ID, Stage: "storage"}}
	result := clients.WorkspaceLaunchStageResult{SchemaVersion: 1, State: string(workspaceLaunchStageReady), Binding: input.Binding, Resources: clients.WorkspaceLaunchResources{StorageID: "storage-unit", StorageBindingRef: "binding-a"}}
	result.Binding.RequestHash = "drifted"
	observation, err := workspaceLaunchFabricObservation(operation, input, result)
	if err != nil || observation.State != workspaceLaunchStageUnknown {
		t.Fatalf("binding drift observation=%#v err=%v", observation, err)
	}
}

func TestWorkspaceLaunchFabricComputeOwnershipPendingRemainsTyped(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.Stage = "ensure_compute_allocation"
	input := clients.WorkspaceLaunchStageInput{Binding: clients.WorkspaceLaunchStageBinding{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, LaunchOperationID: operation.ID, Stage: string(operation.Stage),
	}}
	result := clients.WorkspaceLaunchStageResult{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: string(workspaceLaunchStagePending), Reason: "ownership_pending", Binding: input.Binding,
	}
	observation, err := workspaceLaunchFabricObservation(operation, input, result)
	if err != nil || observation.State != workspaceLaunchStageOwnershipPending {
		t.Fatalf("ownership pending observation=%#v err=%v", observation, err)
	}
	operation.Stage, input.Binding.Stage = "storage", "storage"
	result.Binding, result.Binding.Stage = input.Binding, "storage"
	observation, err = workspaceLaunchFabricObservation(operation, input, result)
	if err != nil || observation.State != workspaceLaunchStageUnknown {
		t.Fatalf("non-compute ownership pending escaped fail-closed: observation=%#v err=%v", observation, err)
	}
}

func TestWorkspaceLaunchFiveFabricStageCallersUseCanonicalHashPayload(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.raw["computeAllocationId"] = json.RawMessage(`"compute-unit"`)
	operation.raw["computeBindingRef"] = json.RawMessage(`"binding-compute"`)
	operation.raw["storageId"] = json.RawMessage(`"storage-unit"`)
	operation.raw["storageBindingRef"] = json.RawMessage(`"binding-storage"`)
	operation.raw["attachmentId"] = json.RawMessage(`"attachment-unit"`)
	operation.raw["attachmentBindingRef"] = json.RawMessage(`"binding-attachment"`)
	operation.raw["gatewaySecretRef"] = json.RawMessage(`"secret-unit"`)
	operation.raw["gatewaySecretVersion"] = json.RawMessage(`"version-unit"`)
	operation.raw["secretBindingRef"] = json.RawMessage(`"binding-secret"`)
	operation.raw["runtimeId"] = json.RawMessage(`"runtime-unit"`)
	operation.raw["runtimeBindingRef"] = json.RawMessage(`"binding-runtime"`)

	stages := []struct {
		stage           contracts.Stage
		action          string
		expectedBinding string
	}{
		{"ensure_compute_allocation", "ensure_compute_allocation", "binding-compute"},
		{"storage", "ensure_storage", "binding-storage"},
		{"attachment", "ensure_attachment", "binding-attachment"},
		{"secret", "ensure_gateway_secret", "binding-secret"},
		{"runtime", "ensure_runtime", "binding-runtime"},
	}
	for _, stage := range stages {
		t.Run(string(stage.stage), func(t *testing.T) {
			current := operation
			current.Stage = stage.stage
			input, err := (&controlPlaneWorkspaceLaunchStageAdapter{}).workspaceLaunchFabricStageInput(context.Background(), current, false)
			if err != nil {
				t.Fatal(err)
			}
			launchRequestHash := current.stringFact("requestHash")
			if input.ProviderProfileRef != current.stringFact("providerProfileRef") || input.PreflightBindingRef != current.stringFact("preflightBindingRef") || input.SpecDigest != current.stringFact("specDigest") ||
				input.Binding.FabricOperationID != current.ID+":"+string(stage.stage) || input.Binding.LaunchOperationID != current.ID ||
				input.Binding.AccountID != current.stringFact("accountId") || input.Binding.WorkspaceID != current.stringFact("workspaceId") ||
				input.Binding.Stage != string(stage.stage) || input.Binding.Action != stage.action || input.Binding.IdempotencyKey != workspaceLaunchStageIdempotencyKey(current, 1) ||
				input.Binding.RequestHash != workspaceLaunchFabricRequestHash(input, launchRequestHash) || input.Binding.ExpectedResourceBinding != stage.expectedBinding {
				t.Fatalf("incomplete explicit Fabric stage input=%#v", input)
			}

			if workspaceLaunchFabricRequestHash(input, strings.Repeat("f", 64)) == input.Binding.RequestHash {
				t.Fatal("launch request is not bound by stage request hash")
			}
			includedMutations := map[string]func(*clients.WorkspaceLaunchStageInput){
				"action":    func(changed *clients.WorkspaceLaunchStageInput) { changed.Binding.Action += "-changed" },
				"package":   func(changed *clients.WorkspaceLaunchStageInput) { changed.PackageID += "-changed" },
				"size":      func(changed *clients.WorkspaceLaunchStageInput) { changed.SizeGB += 10 },
				"image":     func(changed *clients.WorkspaceLaunchStageInput) { changed.WorkspaceImageDigest += "-changed" },
				"resources": func(changed *clients.WorkspaceLaunchStageInput) { changed.Resources.RuntimeURL += "-changed" },
			}
			for name, mutate := range includedMutations {
				changed := input
				mutate(&changed)
				if workspaceLaunchFabricRequestHash(changed, launchRequestHash) == input.Binding.RequestHash {
					t.Fatalf("%s is not bound by stage request hash", name)
				}
			}

			excluded := input
			excluded.ProviderProfileRef += "-changed"
			excluded.PreflightBindingRef += "-changed"
			excluded.SpecDigest = strings.Repeat("d", 64)
			excluded.GatewayCredential = &clients.WorkspaceLaunchGatewayCredential{KeyID: 9, Value: "credential-value"}
			excluded.Binding.SchemaVersion++
			excluded.Binding.LaunchOperationID += "-changed"
			excluded.Binding.AccountID += "-changed"
			excluded.Binding.WorkspaceID += "-changed"
			excluded.Binding.Stage += "-changed"
			excluded.Binding.FabricOperationID += "-changed"
			excluded.Binding.IdempotencyKey += "-changed"
			excluded.Binding.RequestHash += "-changed"
			excluded.Binding.ExpectedResourceBinding += "-changed"
			if workspaceLaunchFabricRequestHash(excluded, launchRequestHash) != input.Binding.RequestHash {
				t.Fatal("stage request hash included independently validated identity or transient credential")
			}
		})
	}
}

func TestWorkspaceLaunchFabricRequestHashMatchesContractGoldenVectors(t *testing.T) {
	contractPath := filepath.Join("..", "..", "..", "..", "packages", "contracts", "opl-cloud-fabric-launch-binding-contract.json")
	contractJSON, err := os.ReadFile(contractPath)
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		StageRequestHash struct {
			GoldenVectors []struct {
				Stage   string `json:"stage"`
				Payload struct {
					LaunchRequestHash string                           `json:"launchRequestHash"`
					Action            string                           `json:"action"`
					PackageID         string                           `json:"packageId"`
					SizeGB            int                              `json:"sizeGb"`
					ImageDigest       string                           `json:"imageDigest"`
					Resources         clients.WorkspaceLaunchResources `json:"resources"`
				} `json:"payload"`
				SHA256 string `json:"sha256"`
			} `json:"goldenVectors"`
		} `json:"stageRequestHash"`
		RuntimeImageRevisionProof struct {
			SchemaVersion           int                                         `json:"schemaVersion"`
			Stage                   string                                      `json:"stage"`
			ProviderProfileRef      string                                      `json:"providerProfileRef"`
			StageRequestHashBinding string                                      `json:"stageRequestHashBinding"`
			GoldenVector            clients.WorkspaceLaunchRuntimeImageRevision `json:"goldenVector"`
		} `json:"runtimeImageRevisionProof"`
	}
	if err := json.Unmarshal(contractJSON, &contract); err != nil {
		t.Fatal(err)
	}
	expectedStages := map[string]bool{
		"ensure_compute_allocation": false,
		"storage":                   false,
		"attachment":                false,
		"secret":                    false,
		"runtime":                   false,
	}
	if len(contract.StageRequestHash.GoldenVectors) != len(expectedStages) {
		t.Fatalf("golden vector count=%d", len(contract.StageRequestHash.GoldenVectors))
	}
	for _, vector := range contract.StageRequestHash.GoldenVectors {
		seen, ok := expectedStages[vector.Stage]
		if !ok || seen {
			t.Fatalf("unexpected or duplicate golden vector stage=%q", vector.Stage)
		}
		expectedStages[vector.Stage] = true
		input := clients.WorkspaceLaunchStageInput{
			Binding:   clients.WorkspaceLaunchStageBinding{Stage: vector.Stage, Action: vector.Payload.Action},
			PackageID: vector.Payload.PackageID, SizeGB: vector.Payload.SizeGB,
			WorkspaceImageDigest: vector.Payload.ImageDigest, Resources: vector.Payload.Resources,
		}
		if vector.Stage == "secret" {
			input.GatewayCredential = &clients.WorkspaceLaunchGatewayCredential{KeyID: 9, Value: "transient-secret"}
		}
		if got := workspaceLaunchFabricRequestHash(input, vector.Payload.LaunchRequestHash); got != vector.SHA256 {
			t.Fatalf("stage=%s hash=%s want=%s", vector.Stage, got, vector.SHA256)
		}
	}
	proofContract := contract.RuntimeImageRevisionProof
	proof := proofContract.GoldenVector
	if proofContract.SchemaVersion != 1 || proofContract.Stage != "runtime" || proofContract.ProviderProfileRef != "tencent-tke" ||
		proofContract.StageRequestHashBinding != "excluded_preserves_original_stage_hash" || proof.SchemaVersion != 1 ||
		proof.LaunchOperationID == "" || proof.WorkspaceID == "" || proof.RuntimeOperationID == "" || proof.AuthorizationDigest == "" ||
		proof.PreviousImageDigest == proof.ReplacementImageDigest {
		t.Fatalf("invalid Runtime image revision contract=%#v", proofContract)
	}
	input := clients.WorkspaceLaunchStageInput{
		Binding:   clients.WorkspaceLaunchStageBinding{Stage: "runtime", Action: "ensure_runtime"},
		PackageID: "basic", SizeGB: 10, WorkspaceImageDigest: proof.PreviousImageDigest, RuntimeImageRevision: &proof,
	}
	withProof := workspaceLaunchFabricRequestHash(input, strings.Repeat("b", 64))
	input.RuntimeImageRevision = nil
	if withoutProof := workspaceLaunchFabricRequestHash(input, strings.Repeat("b", 64)); withProof != withoutProof {
		t.Fatalf("Runtime image revision changed original stage hash: with=%s without=%s", withProof, withoutProof)
	}
}

func TestWorkspaceLaunchFabricReadyWithoutRequiredFactsBecomesUnknown(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.Stage = "storage"
	input := clients.WorkspaceLaunchStageInput{Binding: clients.WorkspaceLaunchStageBinding{SchemaVersion: 1, LaunchOperationID: operation.ID, Stage: "storage"}}
	result := clients.WorkspaceLaunchStageResult{
		SchemaVersion: 1,
		State:         string(workspaceLaunchStageReady),
		Binding:       input.Binding,
		Resources:     clients.WorkspaceLaunchResources{StorageID: "storage-unit"},
	}
	observation, err := workspaceLaunchFabricObservation(operation, input, result)
	if err != nil || observation.State != workspaceLaunchStageUnknown {
		t.Fatalf("incomplete ready observation=%#v err=%v", observation, err)
	}
}

type workspaceLaunchActivationCountingStore struct {
	*memoryTableStore
	activationMutations int
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
	operation, err := newWorkspaceLaunchReconcileOperation(command)
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
	if !workspaceLaunchReceiptInputMatches(charged, wantCharged) {
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
	if !workspaceLaunchReceiptInputMatches(zeroCost, wantZeroCost) {
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

func TestWorkspaceLaunchReceiptReadAcceptsExactCurrentOrLegacyIdentity(t *testing.T) {
	operation := workspaceLaunchCanonicalActivationOperation(t)
	operation.raw["resourceBillingEnabled"] = json.RawMessage(`false`)
	operation.raw["totalChargeUsdMicros"] = json.RawMessage(`0`)
	current := clients.ReceiptInput{
		Type: "workspace.created", Status: "completed", Surface: "control_plane", AccountID: operation.stringFact("accountId"),
		WorkspaceID: operation.stringFact("workspaceId"), RequestID: operation.ID,
		Execution: map[string]any{
			"operationId": operation.ID, "resourceType": "workspace", "resourceId": operation.stringFact("workspaceId"),
			"computeAllocationId": operation.stringFact("computeAllocationId"), "storageId": operation.stringFact("storageId"), "attachmentId": operation.stringFact("attachmentId"),
			"runtimeId": operation.stringFact("runtimeId"), "workspaceApiKeyId": operation.int64Fact("workspaceApiKeyId"),
			"workspaceKeyFingerprint": operation.stringFact("workspaceKeyFingerprint"), "runtimeServiceName": operation.stringFact("runtimeServiceName"),
			"gatewaySecretRef": operation.stringFact("gatewaySecretRef"),
		},
		Owner: map[string]any{"accountId": operation.stringFact("accountId"), "workspaceId": operation.stringFact("workspaceId"), "ownerUserId": operation.stringFact("ownerUserId")},
	}
	legacy := clients.ReceiptInput{
		Type: "workspace.created", Status: "completed", Surface: "workspace", AccountID: operation.stringFact("accountId"),
		WorkspaceID: operation.stringFact("workspaceId"), RequestID: operation.ID + ":purchase-receipt",
		Execution:  map[string]any{"operationId": operation.ID + ":purchase-receipt", "runtimeId": operation.stringFact("runtimeId")},
		OutputRefs: map[string]any{"url": operation.stringFact("url")},
		Owner:      map[string]any{"accountId": operation.stringFact("accountId"), "userId": operation.stringFact("ownerUserId")},
	}

	for _, test := range []struct {
		name     string
		receipts []clients.Receipt
		wantErr  bool
	}{
		{name: "current", receipts: []clients.Receipt{{ReceiptInput: current, ReceiptID: "receipt-current"}}},
		{name: "legacy schema v3", receipts: []clients.Receipt{{ReceiptInput: legacy, ReceiptID: "receipt-legacy"}}},
		{name: "mixed current and legacy", receipts: []clients.Receipt{{ReceiptInput: current, ReceiptID: "receipt-current"}, {ReceiptInput: legacy, ReceiptID: "receipt-legacy"}}, wantErr: true},
		{name: "legacy extra field", receipts: []clients.Receipt{{ReceiptInput: func() clients.ReceiptInput {
			extra := legacy
			extra.Execution = map[string]any{"operationId": operation.ID + ":purchase-receipt", "runtimeId": operation.stringFact("runtimeId"), "computeAllocationId": operation.stringFact("computeAllocationId")}
			return extra
		}(), ReceiptID: "receipt-legacy-extra"}}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ledgerClient := workspaceLaunchReceiptLedgerClient{receipts: test.receipts}
			adapter := &controlPlaneWorkspaceLaunchStageAdapter{service: newTestService(ledgerClient, &fakeFabricClient{})}
			observation, err := adapter.readWorkspaceLaunchReceipt(context.Background(), operation)
			if (err != nil) != test.wantErr {
				t.Fatalf("observation=%#v err=%v wantErr=%v", observation, err, test.wantErr)
			}
			if !test.wantErr && (observation.State != workspaceLaunchStageReady || stringValue(observation.Facts["receiptId"]) != test.receipts[0].ReceiptID) {
				t.Fatalf("receipt read did not close original launch: %#v", observation)
			}
		})
	}
}

func TestWorkspaceLaunchChargedReceiptReadAcceptsExactCurrentOrHistoricalIdentity(t *testing.T) {
	operation := workspaceLaunchCanonicalActivationOperation(t)
	current, err := workspaceLaunchPurchaseReceiptInput(operation)
	if err != nil {
		t.Fatal(err)
	}
	historical := current
	historical.Execution = cloneMap(current.Execution)
	delete(historical.Execution, "operationId")
	delete(historical.Execution, "gatewaySecretRef")

	withHistoricalExecution := func(mutate func(map[string]any)) clients.ReceiptInput {
		input := historical
		input.Execution = cloneMap(historical.Execution)
		mutate(input.Execution)
		return input
	}

	for _, test := range []struct {
		name     string
		receipts []clients.Receipt
		wantErr  bool
	}{
		{name: "current", receipts: []clients.Receipt{{ReceiptInput: current, ReceiptID: "receipt-current"}}},
		{name: "historical charged", receipts: []clients.Receipt{{ReceiptInput: historical, ReceiptID: "receipt-historical"}}},
		{name: "mixed current and historical", receipts: []clients.Receipt{
			{ReceiptInput: current, ReceiptID: "receipt-current"},
			{ReceiptInput: historical, ReceiptID: "receipt-historical"},
		}, wantErr: true},
		{name: "historical extra field", receipts: []clients.Receipt{{ReceiptInput: withHistoricalExecution(func(execution map[string]any) {
			execution["operationId"] = operation.ID
		}), ReceiptID: "receipt-historical-extra"}}, wantErr: true},
		{name: "historical missing field", receipts: []clients.Receipt{{ReceiptInput: withHistoricalExecution(func(execution map[string]any) {
			delete(execution, "runtimeId")
		}), ReceiptID: "receipt-historical-missing"}}, wantErr: true},
		{name: "historical approximate surface", receipts: []clients.Receipt{{ReceiptInput: func() clients.ReceiptInput {
			input := historical
			input.Surface = "workspace"
			return input
		}(), ReceiptID: "receipt-historical-near"}}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ledgerClient := workspaceLaunchReceiptLedgerClient{receipts: test.receipts}
			adapter := &controlPlaneWorkspaceLaunchStageAdapter{service: newTestService(ledgerClient, &fakeFabricClient{})}
			observation, readErr := adapter.readWorkspaceLaunchReceipt(context.Background(), operation)
			if (readErr != nil) != test.wantErr {
				t.Fatalf("observation=%#v err=%v wantErr=%v", observation, readErr, test.wantErr)
			}
			if !test.wantErr && (observation.State != workspaceLaunchStageReady || stringValue(observation.Facts["receiptId"]) != test.receipts[0].ReceiptID) {
				t.Fatalf("receipt read did not close original charged launch: %#v", observation)
			}
		})
	}
}

func TestWorkspaceLaunchActivationCanonicalOperatorAdvancesToReceipt(t *testing.T) {
	store := &workspaceLaunchActivationCountingStore{memoryTableStore: newMemoryTableStore()}
	operation := workspaceLaunchCanonicalActivationOperation(t)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store.runtimeOps = []map[string]any{row}
	service := newTestService(fakeLedgerClient{}, &fakeFabricClient{})
	adapter := &controlPlaneWorkspaceLaunchStageAdapter{app: &controlPlaneServer{tables: store}, service: service}
	got, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(context.Background(), operation.ID)
	attempt := got.Attempts["activation"]
	if err != nil || got.Status != "pending" || got.Stage != "receipt" || attempt.Attempted != 1 || attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.Status != "confirmed" ||
		store.activationMutations != 1 || len(got.FreshContinuationAuthorizations) != 0 {
		t.Fatalf("canonical activation did not converge: operation=%s attempt=%#v mutations=%d authorizations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), attempt, store.activationMutations, got.FreshContinuationAuthorizations, err)
	}
	workspace, found, readErr := store.GetWorkspace(context.Background(), operation.stringFact("workspaceId"))
	if readErr != nil || !found || !workspaceLaunchProjectionMatches(got, workspace) {
		t.Fatalf("canonical activation readback found=%v workspace=%#v err=%v", found, workspace, readErr)
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
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
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

func TestWorkspaceLaunchResumeAuthorizationDigestBindsImmutableAuthorization(t *testing.T) {
	authorization := workspaceLaunchResumeAuthorization{
		AuthorizationID: "resume-unit", LaunchVersion: 1, AuthorizedStage: "runtime", AuthorizedBy: "usr-admin",
		AuthorizedAt: "2026-08-12T00:01:00Z", Reason: "bounded retry", MutationBudget: 1,
	}
	first := workspaceLaunchResumeAuthorizationDigest(authorization)
	if !strings.HasPrefix(first, "sha256:") || first != workspaceLaunchResumeAuthorizationDigest(authorization) {
		t.Fatalf("unstable authorization digest=%q", first)
	}
	authorization.Reason = "different authorization"
	if second := workspaceLaunchResumeAuthorizationDigest(authorization); second == first {
		t.Fatalf("authorization drift retained digest=%q", second)
	}
}
