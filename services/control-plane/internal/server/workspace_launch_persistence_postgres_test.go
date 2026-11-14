package server

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/domain"
)

type workspaceLaunchPendingReadGateStore struct {
	workspaceLaunchReconcileStore
	mu      sync.Mutex
	reads   int
	ready   chan struct{}
	release chan struct{}
}

func (s *workspaceLaunchPendingReadGateStore) GetRuntimeOperation(ctx context.Context, operationID string) (map[string]any, bool, error) {
	row, found, err := s.workspaceLaunchReconcileStore.GetRuntimeOperation(ctx, operationID)
	if err != nil || !found {
		return row, found, err
	}
	s.mu.Lock()
	s.reads++
	if s.reads == 2 {
		close(s.ready)
	}
	release := s.release
	s.mu.Unlock()
	<-release
	return row, found, nil
}

func seedPostgresWorkspaceLaunchFreshTypedPending(
	t *testing.T,
	suffix string,
) (workspaceLaunchReconcileStore, *workspaceLaunchUnitAdapter, workspaceLaunchReconcileOperation) {
	t.Helper()
	ctx := context.Background()
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	accountID, ownerID := "acct-fresh-pending-"+suffix, "usr-fresh-pending-"+suffix
	account, owner := provisionedAccountRowsFor(accountID, ownerID, "fresh-pending-"+suffix+"@example.com", 840)
	mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID = "workspace-launch-fresh-pending-"+suffix, accountID, ownerID
	command.WorkspaceID, command.Sub2APIUserID = "ws-fresh-pending-"+suffix, 840
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	operation.Version, operation.Stage, operation.Status = 4, "runtime", "pending"
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeOperation(ctx, row); err != nil {
		t.Fatal(err)
	}
	absent := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageAbsent}}
	pending := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStagePending}}
	adapter := &workspaceLaunchUnitAdapter{readResultsByStage: map[string][]workspaceLaunchUnitReadResult{"runtime": {absent, pending}}}
	seeded, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(ctx, operation.ID)
	if err != nil || seeded.Status != "pending" || seeded.Stage != "runtime" {
		t.Fatalf("seed PostgreSQL fresh pending: operation=%s err=%v", workspaceLaunchReconcileResultSummary(seeded), err)
	}
	return store, adapter, seeded
}

func TestPostgresWorkspaceLaunchPersistAndActivate(t *testing.T) {
	ctx := context.Background()
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	account, owner := provisionedAccountRowsFor("acct-launch-pg", "usr-launch-pg", "launch-pg@example.com", 41)
	mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))

	command := workspaceLaunchUnitCommand()
	command.OperationID = "workspace-launch-pg"
	command.AccountID = "acct-launch-pg"
	command.OwnerUserID = "usr-launch-pg"
	command.Sub2APIUserID = 41
	command.WorkspaceID = "ws-launch-pg"
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	seededRow, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeOperation(ctx, seededRow); err != nil {
		t.Fatalf("seed launch: %v", err)
	}
	persisted, found, err := store.GetRuntimeOperation(ctx, command.OperationID)
	if err != nil || !found || stringValue(persisted["result"]) != stringValue(seededRow["result"]) {
		t.Fatalf("seed readback found=%v operation=%#v err=%v", found, persisted, err)
	}

	desired, err := decodeWorkspaceLaunchReconcileOperation(persisted)
	if err != nil {
		t.Fatal(err)
	}
	desired.Version++
	desiredRow, err := workspaceLaunchReconcileOperationRow(desired)
	if err != nil {
		t.Fatal(err)
	}
	update := workspaceLaunchReconcileCAS{
		OperationID: command.OperationID, ExpectedOperationResult: stringValue(persisted["result"]), DesiredOperation: desiredRow,
	}
	if err := store.PersistWorkspaceLaunchReconcile(ctx, update); err != nil {
		t.Fatalf("persist exact result CAS: %v", err)
	}
	updated, found, err := store.GetRuntimeOperation(ctx, command.OperationID)
	if err != nil || !found || stringValue(updated["result"]) != stringValue(desiredRow["result"]) {
		t.Fatalf("persist readback found=%v operation=%#v err=%v", found, updated, err)
	}
	if err := store.PersistWorkspaceLaunchReconcile(ctx, update); !errors.Is(err, errWorkspaceLaunchCASConflict) {
		t.Fatalf("stale result CAS error=%v", err)
	}

	row := workspaceProjectionBillingRow(domain.WorkspaceProjection{
		ID: "ws-launch-pg", AccountID: "acct-launch-pg", OwnerID: "usr-launch-pg", Name: "Postgres Launch", PackageID: "basic", Provider: "fabric",
		Status: "running", ComputeID: "compute-fabric-pg", VolumeID: "storage-fabric-pg", AttachmentID: "attachment-fabric-pg",
		RuntimeID: "runtime-fabric-pg", RuntimeServiceName: "runtime-service-pg", RuntimeReady: true, URL: "https://workspace-pg.example",
	}, map[string]any{
		"autoRenew": false, "authorizedBy": "", "authorizedAt": "", "packageId": "basic", "storageGb": 10,
		"priceVersion": pricingCatalogVersion, "currency": pricingCurrency, "billingUnit": pricingBillingUnit,
		"computeUsdMicros": int64(50_000_000), "storageUsdMicros": int64(2_580_000), "totalUsdMicros": int64(52_580_000),
		"periodStart": "2026-08-12T00:00:00Z", "paidThrough": "2026-09-12T00:00:00Z", "nextRenewalAt": "2026-09-11T00:00:00Z",
		"billingAnchorDay": 12, "renewalStatus": "active", "computeAllocationId": "compute-fabric-pg", "storageId": "storage-fabric-pg",
	})
	row["activatedAt"] = "2026-08-12T00:01:00Z"
	activated, err := store.ActivateWorkspaceLaunchProjection(ctx, row)
	if err != nil {
		t.Fatalf("activate launch projection: %v", err)
	}
	readback, found, err := store.GetWorkspace(ctx, "ws-launch-pg")
	if err != nil || !found || stringValue(activated["accountId"]) != "acct-launch-pg" || stringValue(activated["ownerUserId"]) != "usr-launch-pg" || activated["customerProduct"] != true ||
		stringValue(readback["accountId"]) != "acct-launch-pg" || stringValue(readback["ownerUserId"]) != "usr-launch-pg" || readback["customerProduct"] != true {
		t.Fatalf("activation readback found=%v activated=%#v readback=%#v err=%v", found, activated, readback, err)
	}
	drifted := cloneMap(row)
	drifted["ownerAccountId"] = "acct-other"
	if _, err := store.ActivateWorkspaceLaunchProjection(ctx, drifted); !errors.Is(err, errWorkspaceActivationConflict) {
		t.Fatalf("owner drift activation error=%v", err)
	}
	computes, err := store.ListComputes(ctx, "acct-launch-pg")
	if err != nil {
		t.Fatal(err)
	}
	storages, err := store.ListStorages(ctx, "acct-launch-pg")
	if err != nil {
		t.Fatal(err)
	}
	attachments, err := store.ListAttachments(ctx, "acct-launch-pg")
	if err != nil {
		t.Fatal(err)
	}
	if len(computes) != 0 || len(storages) != 0 || len(attachments) != 0 {
		t.Fatalf("activation copied Fabric truth: computes=%#v storages=%#v attachments=%#v", computes, storages, attachments)
	}
}

func TestPostgresWorkspaceLaunchReceiptProjectionCASIsIdempotentAndFailClosed(t *testing.T) {
	ctx := context.Background()
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	account, owner := provisionedAccountRowsFor("acct-receipt-pg", "usr-receipt-pg", "receipt-pg@example.com", 41)
	mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))

	workspace := workspaceLaunchUnitActivationProjectionRow(t, "ws-receipt-pg", "acct-receipt-pg", "usr-receipt-pg")
	mustStore(t, store.SaveWorkspace(ctx, workspace))

	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID = "workspace-launch-receipt-pg", "acct-receipt-pg", "usr-receipt-pg"
	command.WorkspaceID, command.Sub2APIUserID = "ws-receipt-pg", 41
	operation, err := newWorkspaceLaunchFixtureOperation(command)
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
	seeded, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeOperation(ctx, seeded); err != nil {
		t.Fatal(err)
	}

	projection, err := workspaceLaunchReceiptProjectionFor(operation)
	if err != nil {
		t.Fatal(err)
	}
	operation.Version++
	first, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PersistWorkspaceLaunchReconcile(ctx, workspaceLaunchReconcileCAS{
		OperationID: command.OperationID, ExpectedOperationResult: stringValue(seeded["result"]), DesiredOperation: first, WorkspaceReceiptProjection: projection,
	}); err != nil {
		t.Fatalf("first projection: %v", err)
	}
	readback, found, err := store.GetWorkspace(ctx, command.WorkspaceID)
	if err != nil || !found || stringValue(readback["purchaseReceiptId"]) != "receipt-unit" {
		t.Fatalf("first projection readback found=%v workspace=%#v err=%v", found, readback, err)
	}

	operation.Version++
	second, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PersistWorkspaceLaunchReconcile(ctx, workspaceLaunchReconcileCAS{
		OperationID: command.OperationID, ExpectedOperationResult: stringValue(first["result"]), DesiredOperation: second, WorkspaceReceiptProjection: projection,
	}); err != nil {
		t.Fatalf("same receipt replay: %v", err)
	}

	readback["purchaseReceiptId"] = "receipt-other"
	if err := store.SaveWorkspace(ctx, readback); err != nil {
		t.Fatalf("seed conflicting workspace receipt: %v", err)
	}
	operation.Version++
	conflict, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PersistWorkspaceLaunchReconcile(ctx, workspaceLaunchReconcileCAS{
		OperationID: command.OperationID, ExpectedOperationResult: stringValue(second["result"]), DesiredOperation: conflict, WorkspaceReceiptProjection: projection,
	}); !errors.Is(err, errIdempotencyConflict) {
		t.Fatalf("different receipt error=%v", err)
	}
	readback, found, err = store.GetWorkspace(ctx, command.WorkspaceID)
	if err != nil || !found || stringValue(readback["purchaseReceiptId"]) != "receipt-other" {
		t.Fatalf("conflicting projection mutated workspace found=%v workspace=%#v err=%v", found, readback, err)
	}
}

func TestPostgresWorkspaceLaunchManualReviewAutoRecoveryPersistsRuntimeReadyReadOnly(t *testing.T) {
	ctx := context.Background()
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	accountID, ownerID := "acct-unit", "usr-unit"
	account, owner := provisionedAccountRowsFor(accountID, ownerID, "runtime-auto-recovery-pg@example.com", 13)
	mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))

	operation := workspaceLaunchAutomaticRuntimeReadyOperation(t)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeOperation(ctx, row); err != nil {
		t.Fatal(err)
	}

	adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{string(contracts.StageRuntime): true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	reconciler.now = func() time.Time { return time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC) }
	got, err := reconciler.AutoRecoverManualReview(ctx, operation.ID)
	if err != nil || got.Stage != contracts.StageActivation || got.Status != contracts.StatusPending || got.Version != operation.Version+1 ||
		got.Attempts[contracts.StageRuntime].Confirmed != 1 || got.ResumeAuthorization == nil ||
		got.ResumeAuthorization.AuthorizedBy != workspaceLaunchAutomaticFabricReadyAuthorizedBy || got.ResumeAuthorizationConsumedAt == "" ||
		adapter.reads != 1 || adapter.mutations != 0 {
		t.Fatalf("PostgreSQL automatic Runtime recovery did not persist: operation=%s reads=%d mutations=%d err=%v",
			workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutations, err)
	}

	persisted, found, err := store.GetRuntimeOperation(ctx, operation.ID)
	restarted, decodeErr := decodeWorkspaceLaunchReconcileOperation(persisted)
	if err != nil || !found || decodeErr != nil || restarted.Version != got.Version || restarted.Stage != contracts.StageActivation ||
		restarted.ResumeAuthorization == nil || restarted.ResumeAuthorization.AuthorizationID != got.ResumeAuthorization.AuthorizationID ||
		restarted.ResumeAuthorization.AuthorizedBy != workspaceLaunchAutomaticFabricReadyAuthorizedBy || restarted.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("PostgreSQL automatic Runtime recovery restart readback failed: found=%v operation=%s errors=%v/%v",
			found, workspaceLaunchReconcileResultSummary(restarted), err, decodeErr)
	}
}

func TestPostgresWorkspaceLaunchManualReviewAutoRecoveryPersistsComputeOwnershipContinuation(t *testing.T) {
	ctx := context.Background()
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	accountID, ownerID := "acct-unit", "usr-unit"
	account, owner := provisionedAccountRowsFor(accountID, ownerID, "compute-ownership-auto-recovery-pg@example.com", 14)
	mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))

	operation := workspaceLaunchAutomaticComputeOwnershipAfterFailedFreshContinuationOperation(t)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeOperation(ctx, row); err != nil {
		t.Fatal(err)
	}

	ownership := workspaceLaunchUnitReadResult{observation: workspaceLaunchStageObservation{State: workspaceLaunchStageOwnershipPending}}
	adapter := &workspaceLaunchUnitAdapter{
		readResultsByStage: map[string][]workspaceLaunchUnitReadResult{string(contracts.StageCompute): {ownership, ownership, ownership}},
		replayableStages:   map[string]bool{string(contracts.StageCompute): true},
	}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	reconciler.now = func() time.Time { return time.Date(2026, 8, 29, 4, 30, 0, 0, time.UTC) }
	got, err := reconciler.AutoRecoverManualReview(ctx, operation.ID)
	if err != nil || got.Stage != contracts.StageStorage || got.Status != contracts.StatusPending ||
		got.Attempts[contracts.StageCompute].Confirmed != 1 || got.ResumeAuthorization == nil ||
		got.ResumeAuthorization.AuthorizedBy != workspaceLaunchAutomaticComputeOwnershipAuthorizedBy || got.ResumeAuthorizationConsumedAt == "" ||
		adapter.reads != 4 || adapter.mutationsByStage[string(contracts.StageCompute)] != 1 {
		t.Fatalf("PostgreSQL automatic Compute ownership recovery did not persist: operation=%s reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutationsByStage, err)
	}

	persisted, found, err := store.GetRuntimeOperation(ctx, operation.ID)
	restarted, decodeErr := decodeWorkspaceLaunchReconcileOperation(persisted)
	claim := restarted.IdempotentReplayClaims[contracts.StageCompute]
	if err != nil || !found || decodeErr != nil || restarted.Version != got.Version || restarted.Stage != contracts.StageStorage ||
		restarted.ResumeAuthorization == nil || restarted.ResumeAuthorization.AuthorizationID != got.ResumeAuthorization.AuthorizationID ||
		restarted.ResumeAuthorization.AuthorizedBy != workspaceLaunchAutomaticComputeOwnershipAuthorizedBy || restarted.ResumeAuthorizationConsumedAt == "" ||
		claim.AuthorizationID != restarted.ResumeAuthorization.AuthorizationID || claim.Status != "succeeded" ||
		len(restarted.FreshContinuationAuthorizations) != 0 || len(restarted.ContinuationReadClaims) != 0 {
		t.Fatalf("PostgreSQL automatic Compute ownership recovery restart readback failed: found=%v operation=%s claim=%#v errors=%v/%v",
			found, workspaceLaunchReconcileResultSummary(restarted), claim, err, decodeErr)
	}
}

func TestPostgresWorkspaceLaunchManualReviewAutoRecoveryPersistsStorageAbsenceReplay(t *testing.T) {
	ctx := context.Background()
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	accountID, ownerID := "acct-unit", "usr-unit"
	account, owner := provisionedAccountRowsFor(accountID, ownerID, "storage-absence-auto-recovery-pg@example.com", 15)
	mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))

	operation := workspaceLaunchAutomaticStorageAbsenceOperation(t)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeOperation(ctx, row); err != nil {
		t.Fatal(err)
	}

	adapter := &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{string(contracts.StageStorage): true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	reconciler.now = func() time.Time { return time.Date(2026, 8, 29, 7, 0, 0, 0, time.UTC) }
	got, err := reconciler.AutoRecoverManualReview(ctx, operation.ID)
	attempt := got.Attempts[contracts.StageStorage]
	claim := got.IdempotentReplayClaims[contracts.StageStorage]
	if err != nil || got.Stage != contracts.StageAttachment || got.Status != contracts.StatusPending ||
		attempt.Confirmed != 1 || attempt.Unknown != 0 || attempt.IdempotencyKey != operation.Attempts[contracts.StageStorage].IdempotencyKey ||
		got.ResumeAuthorization == nil || got.ResumeAuthorization.AuthorizedBy != workspaceLaunchAutomaticStorageAbsenceAuthorizedBy ||
		got.ResumeAuthorizationConsumedAt == "" || claim.Status != "succeeded" ||
		adapter.reads != 4 || adapter.mutationsByStage[string(contracts.StageStorage)] != 1 {
		t.Fatalf("PostgreSQL automatic Storage absence recovery did not persist: operation=%s claim=%#v reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), claim, adapter.reads, adapter.mutationsByStage, err)
	}

	persisted, found, err := store.GetRuntimeOperation(ctx, operation.ID)
	restarted, decodeErr := decodeWorkspaceLaunchReconcileOperation(persisted)
	restartedClaim := restarted.IdempotentReplayClaims[contracts.StageStorage]
	if err != nil || !found || decodeErr != nil || restarted.Version != got.Version || restarted.Stage != contracts.StageAttachment ||
		restarted.ResumeAuthorization == nil || restarted.ResumeAuthorization.AuthorizationID != got.ResumeAuthorization.AuthorizationID ||
		restarted.ResumeAuthorizationConsumedAt == "" || restartedClaim != claim {
		t.Fatalf("PostgreSQL automatic Storage absence recovery restart readback failed: found=%v operation=%s claim=%#v errors=%v/%v",
			found, workspaceLaunchReconcileResultSummary(restarted), restartedClaim, err, decodeErr)
	}

	readsBefore, mutationsBefore := adapter.reads, adapter.mutations
	replayed, err := NewWorkspaceLaunchReconciler(store, adapter).AutoRecoverManualReview(ctx, operation.ID)
	if err != nil || replayed.Version != restarted.Version || adapter.reads != readsBefore || adapter.mutations != mutationsBefore {
		t.Fatalf("PostgreSQL restart repeated automatic Storage recovery: operation=%s reads=%d/%d mutations=%d/%d err=%v",
			workspaceLaunchReconcileResultSummary(replayed), adapter.reads, readsBefore, adapter.mutations, mutationsBefore, err)
	}
}

func TestPostgresWorkspaceLaunchCanonicalOperatorActivationPersistsAuthoritativeReadback(t *testing.T) {
	ctx := context.Background()
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	account := map[string]any{"id": "acct-admin", "ownerUserId": "usr-admin", "status": "active", "sub2apiUserId": int64(1)}
	owner := map[string]any{"id": "usr-admin", "email": "admin@opl.local", "accountId": "acct-admin", "role": "admin", "status": "active"}
	mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))

	row := workspaceLaunchUnitActivationProjectionRow(t, "ws-admin-pg", "acct-admin", "usr-admin")
	activated, err := store.ActivateWorkspaceLaunchProjection(ctx, row)
	if err != nil {
		t.Fatalf("activate canonical operator projection: %v", err)
	}
	readback, found, err := store.GetWorkspace(ctx, "ws-admin-pg")
	if err != nil || !found || stringValue(activated["accountId"]) != "acct-admin" || stringValue(activated["ownerUserId"]) != "usr-admin" || activated["customerProduct"] != true ||
		stringValue(readback["accountId"]) != "acct-admin" || stringValue(readback["ownerUserId"]) != "usr-admin" || readback["customerProduct"] != true {
		t.Fatalf("canonical activation readback found=%v activated=%#v readback=%#v err=%v", found, activated, readback, err)
	}
}

func TestPostgresWorkspaceLaunchFreshPendingReadClaimSurvivesCrashAndRestart(t *testing.T) {
	ctx := context.Background()
	store, adapter, seeded := seedPostgresWorkspaceLaunchFreshTypedPending(t, "restart")
	startedAt := time.Date(2026, 8, 16, 2, 0, 0, 0, time.UTC)
	adapter.panicOnReadNumber = 3
	first := NewWorkspaceLaunchReconciler(store, adapter)
	first.now = func() time.Time { return startedAt }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("expected simulated process crash")
			}
		}()
		_, _ = first.Reconcile(ctx, seeded.ID)
	}()
	durableRow, found, err := store.GetRuntimeOperation(ctx, seeded.ID)
	if err != nil || !found {
		t.Fatalf("read durable fresh claim found=%v err=%v", found, err)
	}
	durable, err := decodeWorkspaceLaunchReconcileOperation(durableRow)
	authorization := durable.FreshContinuationAuthorizations["runtime"]
	claim2 := durable.ContinuationReadClaims[workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, 2)]
	if err != nil || durable.Attempts["runtime"].PendingReadbacks != 2 || claim2.Status != "claimed" || adapter.mutationsByStage["runtime"] != 1 {
		t.Fatalf("PostgreSQL crash claim not durable: operation=%s claim=%#v mutations=%#v err=%v", workspaceLaunchReconcileResultSummary(durable), claim2, adapter.mutationsByStage, err)
	}

	adapter.panicOnReadNumber = 0
	adapter.stageObservations = map[string]workspaceLaunchStageObservation{"runtime": {State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("runtime")}}
	restarted := NewWorkspaceLaunchReconciler(store, adapter)
	restarted.now = func() time.Time { return startedAt.Add(workspaceLaunchFreshContinuationReadClaimLease + time.Second) }
	got, err := restarted.Reconcile(ctx, seeded.ID)
	if err != nil || got.Stage != "activation" || got.Attempts["runtime"].PendingReadbacks != 3 ||
		got.ContinuationReadClaims[workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, 2)].Status != "expired" ||
		got.ContinuationReadClaims[workspaceLaunchFreshContinuationClaimKey(authorization.AuthorizationID, 3)].Status != "ready" ||
		got.FreshContinuationAuthorizations["runtime"].Status != "consumed" || adapter.mutationsByStage["runtime"] != 1 {
		t.Fatalf("PostgreSQL restart skipped or refunded claim: operation=%s claims=%#v mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(got), got.ContinuationReadClaims, adapter.mutationsByStage, err)
	}
}

func TestPostgresWorkspaceLaunchFreshPendingContinuationCASAllowsOneOwnerRead(t *testing.T) {
	ctx := context.Background()
	store, adapter, seeded := seedPostgresWorkspaceLaunchFreshTypedPending(t, "concurrent")
	adapter.stageObservations = map[string]workspaceLaunchStageObservation{"runtime": {State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("runtime")}}
	gate := &workspaceLaunchPendingReadGateStore{
		workspaceLaunchReconcileStore: store,
		ready:                         make(chan struct{}), release: make(chan struct{}),
	}
	reconciler := NewWorkspaceLaunchReconciler(gate, adapter)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := reconciler.Reconcile(ctx, seeded.ID)
			results <- err
		}()
	}
	<-gate.ready
	close(gate.release)
	var successes, conflicts int
	for range 2 {
		switch err := <-results; {
		case err == nil:
			successes++
		case errors.Is(err, errWorkspaceLaunchCASConflict):
			conflicts++
		default:
			t.Fatalf("unexpected concurrent fresh continuation error: %v", err)
		}
	}
	durableRow, found, err := store.GetRuntimeOperation(ctx, seeded.ID)
	if err != nil || !found {
		t.Fatalf("read durable concurrent continuation found=%v err=%v", found, err)
	}
	durable, err := decodeWorkspaceLaunchReconcileOperation(durableRow)
	if err != nil || successes != 1 || conflicts != 1 || durable.Stage != "activation" || durable.Attempts["runtime"].PendingReadbacks != 2 ||
		adapter.reads != 3 || adapter.mutationsByStage["runtime"] != 1 || durable.FreshContinuationAuthorizations["runtime"].Status != "consumed" {
		t.Fatalf("PostgreSQL fresh continuation CAS mismatch: operation=%s successes=%d conflicts=%d reads=%d mutations=%#v err=%v",
			workspaceLaunchReconcileResultSummary(durable), successes, conflicts, adapter.reads, adapter.mutationsByStage, err)
	}
}

func TestPostgresWorkspaceLaunchLegacyV3MissingFreshContinuationFieldsHasZeroBudget(t *testing.T) {
	ctx := context.Background()
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	account, owner := provisionedAccountRowsFor(
		"acct-fresh-legacy", "usr-fresh-legacy", "fresh-legacy@example.com", 841,
	)
	mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID = "workspace-launch-fresh-legacy", "acct-fresh-legacy", "usr-fresh-legacy"
	command.WorkspaceID, command.Sub2APIUserID = "ws-fresh-legacy", 841
	operation, err := newWorkspaceLaunchFixtureOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	operation.Version, operation.Stage, operation.Status = 4, "runtime", "pending"
	attempt := operation.Attempts["runtime"]
	attempt.Attempted, attempt.Status = 1, "reserved"
	attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
	operation.Attempts["runtime"] = attempt
	operation.Observations["runtime"] = workspaceLaunchStageObservation{State: workspaceLaunchStagePending}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveRuntimeOperation(ctx, row); err != nil {
		t.Fatal(err)
	}
	adapter := &workspaceLaunchUnitAdapter{
		stageObservations: map[string]workspaceLaunchStageObservation{"runtime": {State: workspaceLaunchStageReady, Facts: workspaceLaunchReadyFacts("runtime")}},
		replayableStages:  map[string]bool{"runtime": true},
	}
	got, err := NewWorkspaceLaunchReconciler(store, adapter).Reconcile(ctx, operation.ID)
	if err != nil || got.Status != "manual_review" || got.Stage != "runtime" || adapter.reads != 0 || adapter.mutations != 0 ||
		got.Attempts["runtime"].PendingReadbacks != 0 || got.Attempts["runtime"].MaxPendingReadbacks != workspaceLaunchLegacyV3AuthoritativeReadBudget ||
		len(got.FreshContinuationAuthorizations) != 0 || len(got.ContinuationReadClaims) != 0 {
		t.Fatalf("PostgreSQL legacy v3 row invented continuation authority: operation=%s reads=%d mutations=%d err=%v",
			workspaceLaunchReconcileResultSummary(got), adapter.reads, adapter.mutations, err)
	}
}

func TestPostgresWorkspaceLaunchReservedDebitRestartsWithReadOnlyEvidence(t *testing.T) {
	for _, auditPresent := range []bool{false, true} {
		t.Run(strconv.FormatBool(auditPresent), func(t *testing.T) {
			ctx := context.Background()
			store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
			account, owner := provisionedAccountRowsFor("acct-debit-restart", "usr-debit-restart", "debit-restart@example.test", 841)
			mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))
			command := workspaceLaunchUnitCommand()
			command.AccountID, command.OwnerUserID, command.Sub2APIUserID = "acct-debit-restart", "usr-debit-restart", 841
			operation, err := newWorkspaceLaunchFixtureOperation(command)
			if err != nil {
				t.Fatal(err)
			}
			startedAt := time.Date(2026, 9, 10, 4, 0, 0, 0, time.UTC)
			operation.Stage, operation.Status = contracts.StageDebit, contracts.StatusPending
			attempt := operation.Attempts[contracts.StageDebit]
			attempt.Attempted, attempt.Status, attempt.IdempotencyKey = 1, "reserved", workspaceLaunchStageIdempotencyKey(operation, 1)
			attempt.DispatchLeaseExpiresAt = startedAt.Add(workspaceLaunchIdempotentReplayLease).Format(time.RFC3339Nano)
			operation.Attempts[contracts.StageDebit] = attempt
			row, err := workspaceLaunchReconcileOperationRow(operation)
			if err != nil {
				t.Fatal(err)
			}
			mustStore(t, store.SaveRuntimeOperation(ctx, row))
			adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{"debit": auditPresent}, replayableStages: map[string]bool{"debit": true}}
			restarted := NewWorkspaceLaunchReconciler(store, adapter)
			restarted.now = func() time.Time { return startedAt.Add(workspaceLaunchIdempotentReplayLease + time.Second) }
			got, err := restarted.Reconcile(ctx, operation.ID)
			if err != nil || adapter.mutations != 0 || got.Attempts[contracts.StageDebit].Attempted != 1 || got.Attempts[contracts.StageDebit].IdempotencyKey != attempt.IdempotencyKey {
				t.Fatalf("restart changed wallet dispatch: operation=%s writes=%d err=%v", workspaceLaunchReconcileResultSummary(got), adapter.mutations, err)
			}
			if auditPresent {
				if got.Stage != contracts.StageCompute || got.Attempts[contracts.StageDebit].Confirmed != 1 {
					t.Fatal("original debit audit did not advance purchase")
				}
			} else if got.Status != contracts.StatusManualReview || got.Stage != contracts.StageDebit || got.Attempts[contracts.StageDebit].Confirmed != 0 {
				t.Fatal("missing debit audit authorized progress")
			}
			durableRow, found, err := store.GetRuntimeOperation(ctx, operation.ID)
			if err != nil || !found {
				t.Fatalf("read persisted debit result: %v", err)
			}
			durable, err := decodeWorkspaceLaunchReconcileOperation(durableRow)
			if err != nil || durable.Status != got.Status || durable.Stage != got.Stage || durable.Attempts[contracts.StageDebit] != got.Attempts[contracts.StageDebit] {
				t.Fatalf("debit recovery lost durable state: %v", err)
			}
		})
	}
}
