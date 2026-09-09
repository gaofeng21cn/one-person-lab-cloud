package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func TestPostgresD3LateDebitRecoversOriginalPurchaseAfterRestart(t *testing.T) {
	t.Setenv("OPL_TENCENT_ZONE", "na-siliconvalley-1")
	for _, profile := range []string{"local-docker", "tencent-tke"} {
		t.Run(profile, func(t *testing.T) {
			ctx := context.Background()
			store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
			command := workspaceLaunchUnitCommand()
			command.ProviderProfileRef = profile
			account, owner := provisionedAccountRowsFor(command.AccountID, command.OwnerUserID, "d3-late@example.com", command.Sub2APIUserID)
			mustStore(t, store.CreateProvisionedAccount(ctx, account, owner))
			op, err := newWorkspaceLaunchReconcileOperation(command)
			if err != nil {
				t.Fatal(err)
			}
			op.Stage = contracts.StageDebit
			row, err := workspaceLaunchReconcileOperationRow(op)
			if err != nil {
				t.Fatal(err)
			}
			mustStore(t, store.ClaimWorkspaceLaunchReconcile(ctx, workspaceLaunchReconcileClaim{AccountID: command.AccountID, DesiredOperation: row}))
			stub := &workspaceLaunchDebitReadbackStub{testSub2APIClient: &testSub2APIClient{charges: map[string]int64{}}, history: map[string]clients.Sub2APIBalanceHistoryEntry{}, balance: clients.Sub2APIBalance{UserID: command.Sub2APIUserID, USDMicros: command.PreChargeBalanceMicros, Status: "active"}}
			service := controlplane.NewService(fakeLedgerClient{}, &gatewayAccountingFabric{}, stub)
			adapter := &controlPlaneWorkspaceLaunchStageAdapter{app: &controlPlaneServer{tables: store}, service: service}
			for i := 0; i < 3; i++ {
				op, err = NewWorkspaceLaunchReconciler(store, adapter).Reconcile(ctx, op.ID)
				if err != nil {
					t.Fatal(err)
				}
			}
			if op.Status != contracts.StatusManualReview || stub.chargeCalls != 1 {
				t.Fatalf("late purchase not retained: %s charges=%d", workspaceLaunchReconcileResultSummary(op), stub.chargeCalls)
			}
			originalKey := op.Attempts[contracts.StageDebit].IdempotencyKey
			amount, userID, code := op.int64Fact("totalChargeUsdMicros"), op.int64Fact("sub2apiUserId"), op.stringFact("sub2apiRedeemCode")
			usedAt := time.Date(2026, 9, 8, 12, 30, 0, 0, time.UTC)
			entry := clients.Sub2APIBalanceHistoryEntry{Code: code, Type: "balance", ValueUSDMicros: -amount, Status: "used", UsedBy: &userID, UsedAt: &usedAt, CreatedAt: usedAt}
			wrongUser := userID + 1
			entry.UsedBy = &wrongUser
			stub.history[code] = entry
			if _, err := NewWorkspaceLaunchReconciler(store, adapter).AutoRecoverManualReview(ctx, op.ID); err == nil {
				t.Fatal("foreign account debit was accepted")
			}
			entry.UsedBy = &userID
			stub.history[code] = entry
			recovered, err := NewWorkspaceLaunchReconciler(store, adapter).AutoRecoverManualReview(ctx, op.ID)
			if err != nil || recovered.Stage != contracts.StageCompute || recovered.Status != contracts.StatusPending || recovered.Attempts[contracts.StageDebit].Confirmed != 1 || recovered.Attempts[contracts.StageDebit].IdempotencyKey != originalKey || stub.chargeCalls != 1 {
				t.Fatalf("original debit did not recover: %s charges=%d err=%v", workspaceLaunchReconcileResultSummary(recovered), stub.chargeCalls, err)
			}
			if recovered.stringFact("periodStart") != usedAt.Format(time.RFC3339Nano) {
				t.Fatal("late read shifted original paid period")
			}
			row, _, err = store.GetRuntimeOperation(ctx, op.ID)
			reread, decodeErr := decodeWorkspaceLaunchReconcileOperation(row)
			if err != nil || decodeErr != nil || reread.Stage != contracts.StageCompute || reread.FreshContinuationAuthorizations[contracts.StageDebit].Status != "consumed" {
				t.Fatalf("restart lost recovery: %v/%v", err, decodeErr)
			}
		})
	}
}

func TestD3ExpiredFirstDispatchReservationRecoversReadOnly(t *testing.T) {
	op, err := decodeWorkspaceLaunchReconcileOperation(workspaceLaunchReservedStageManualReviewRow(t, contracts.StageRuntime))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	op.Status = contracts.StatusPending
	attempt := op.Attempts[op.Stage]
	attempt.DispatchLeaseExpiresAt = now.Add(workspaceLaunchIdempotentReplayLease).Format(time.RFC3339Nano)
	op.Attempts[op.Stage] = attempt
	row, err := workspaceLaunchReconcileOperationRow(op)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	reconciler.now = func() time.Time { return now }
	waiting, err := reconciler.Reconcile(context.Background(), op.ID)
	if err != nil || waiting.Status != contracts.StatusPending || adapter.reads != 0 || adapter.mutations != 0 {
		t.Fatalf("active reservation changed: %v", err)
	}
	now = now.Add(workspaceLaunchIdempotentReplayLease)
	parked, err := reconciler.Reconcile(context.Background(), op.ID)
	if err != nil || parked.Status != contracts.StatusManualReview || adapter.mutations != 0 {
		t.Fatalf("expired reservation replayed: %v", err)
	}
	adapter.readyStages = map[string]bool{string(contracts.StageRuntime): true}
	recovered, err := reconciler.AutoRecoverManualReview(context.Background(), op.ID)
	if err != nil || recovered.Stage != contracts.StageActivation || adapter.mutations != 0 {
		t.Fatalf("late original mutation did not converge read only: %s %v", workspaceLaunchReconcileResultSummary(recovered), err)
	}
}

type d3LostQueueResultStore struct {
	*workspaceLaunchUnitStore
	lost bool
}

func (s *d3LostQueueResultStore) PersistWorkspaceLaunchReconcile(ctx context.Context, update workspaceLaunchReconcileCAS) error {
	op, err := decodeWorkspaceLaunchReconcileOperation(update.DesiredOperation)
	if err != nil {
		return err
	}
	if !s.lost && op.Observations[contracts.StageCompute].State == workspaceLaunchStageComputePoolQueued {
		s.lost = true
		return errors.New("simulated process exit before saving the queue result")
	}
	return s.workspaceLaunchUnitStore.PersistWorkspaceLaunchReconcile(ctx, update)
}

func TestD3ComputeQueueCrossesTypedHTTPAndDispatchesOriginalIdentity(t *testing.T) {
	op, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	op.Stage = contracts.StageCompute
	provider, _ := json.Marshal("tencent-tke")
	op.raw["providerProfileRef"] = provider
	row, err := workspaceLaunchReconcileOperationRow(op)
	if err != nil {
		t.Fatal(err)
	}
	queued, ready := true, false
	ownershipPending := false
	ensures := 0
	var original clients.WorkspaceLaunchStageBinding
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input clients.WorkspaceLaunchStageInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		if original.LaunchOperationID == "" {
			original = input.Binding
		} else if original != input.Binding {
			t.Error("continuation changed original stage identity")
		}
		result := clients.WorkspaceLaunchStageResult{SchemaVersion: 1, Binding: input.Binding, Resources: input.Resources, State: "absent", Reason: "no_stage_record"}
		if r.URL.Path == "/fabric/workspace-launches/stages/ensure" {
			ensures++
			if !queued && ownershipPending {
				ready = true
			}
		}
		if ensures > 0 {
			result.State = "pending"
			result.Reason = "compute_pool_queued"
			if !queued {
				result.Reason = "compute_dispatch_pending"
				if ensures >= 2 {
					result.Reason = "provider_provisioning"
				}
				if ownershipPending {
					result.Reason = "ownership_pending"
				}
			}
		}
		if ready {
			result.State = "ready"
			result.Reason = "none"
			result.Resources.ComputeAllocationID = "compute-d3"
			result.Resources.ComputeBindingRef = "fabric:compute-d3"
		}
		_ = json.NewEncoder(w).Encode(result)
	}))
	defer endpoint.Close()
	client := clients.NewFabricHTTPClientWithCapability(endpoint.URL, "d3-local-token", "d3-local-capability", endpoint.Client())
	adapter := &controlPlaneWorkspaceLaunchStageAdapter{app: &controlPlaneServer{}, service: controlplane.NewService(fakeLedgerClient{}, client, &testSub2APIClient{})}
	store := &d3LostQueueResultStore{workspaceLaunchUnitStore: &workspaceLaunchUnitStore{row: row}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	reconciler.now = func() time.Time { return now }
	_, err = reconciler.Reconcile(context.Background(), op.ID)
	if err == nil || !store.lost || ensures != 1 {
		t.Fatalf("queue result loss was not injected: ensures=%d err=%v", ensures, err)
	}
	now = now.Add(time.Minute)
	reconciler = NewWorkspaceLaunchReconciler(store, adapter)
	reconciler.now = func() time.Time { return now }
	pending, err := reconciler.Reconcile(context.Background(), op.ID)
	if err != nil || pending.Status != contracts.StatusPending || ensures != 1 {
		t.Fatalf("queue entry failed: %s ensures=%d err=%v", workspaceLaunchReconcileResultSummary(pending), ensures, err)
	}
	for i := 0; i < 70; i++ {
		now = now.Add(time.Minute)
		pending, err = reconciler.Reconcile(context.Background(), op.ID)
		if err != nil || pending.Stage != contracts.StageCompute || ensures != 1 {
			t.Fatalf("queued order dispatched: %v ensures=%d", err, ensures)
		}
	}
	if pending.Attempts[contracts.StageCompute].PendingDeadlineAt != "" || pending.Attempts[contracts.StageCompute].PendingReadbacks != 0 {
		t.Fatal("queue consumed provider provisioning budget")
	}
	queued = false
	pending, err = reconciler.Reconcile(context.Background(), op.ID)
	if err != nil || pending.Stage != contracts.StageCompute || ensures != 2 {
		t.Fatalf("dispatch did not retain provisioning: %s ensures=%d err=%v", workspaceLaunchReconcileResultSummary(pending), ensures, err)
	}
	ownershipPending = true
	recovered, err := reconciler.Reconcile(context.Background(), op.ID)
	if err != nil || recovered.Stage != contracts.StageStorage || ensures != 3 || recovered.Attempts[contracts.StageCompute].Attempted != 1 {
		t.Fatalf("queue head did not dispatch original purchase: %s ensures=%d err=%v", workspaceLaunchReconcileResultSummary(recovered), ensures, err)
	}
}

func TestD3EveryStageLateReadyRecoversWithoutRepeatingItsMutation(t *testing.T) {
	for _, profile := range []string{"local-docker", "tencent-tke"} {
		for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
			t.Run(profile+"/"+string(stage), func(t *testing.T) {
				op, err := decodeWorkspaceLaunchReconcileOperation(workspaceLaunchUnknownStageManualReviewRow(t, stage))
				if err != nil {
					t.Fatal(err)
				}
				op.raw["providerProfileRef"], _ = json.Marshal(profile)
				row, err := workspaceLaunchReconcileOperationRow(op)
				if err != nil {
					t.Fatal(err)
				}
				store := &workspaceLaunchUnitStore{row: row}
				adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{string(stage): true}}
				recovered, err := NewWorkspaceLaunchReconciler(store, adapter).AutoRecoverManualReview(context.Background(), op.ID)
				if err != nil || recovered.Stage == stage || recovered.ID != op.ID || adapter.mutations != 0 || recovered.Attempts[stage].Confirmed != 1 {
					t.Fatalf("late stage did not advance read only: %s err=%v", workspaceLaunchReconcileResultSummary(recovered), err)
				}
			})
		}
	}
}

func TestD3ReadOnlyCheckPreservesOriginalRecoveryLineage(t *testing.T) {
	row := workspaceLaunchUnknownStorageAfterFailedReplayRow(t)
	original, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{replayableStages: map[string]bool{string(contracts.StageStorage): true}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	check := workspaceLaunchUnknownRuntimeReadAuthorization(t, row, "check-before-storage-recovery")
	checked, err := reconciler.CheckResult(context.Background(), original.ID, check)
	if err != nil || !reflect.DeepEqual(checked.ResumeAuthorization, original.ResumeAuthorization) || !reflect.DeepEqual(checked.ConsumedResumeAuthorizations, original.ConsumedResumeAuthorizations) || checked.Status != contracts.StatusManualReview {
		t.Fatalf("check changed recovery lineage: %s err=%v", workspaceLaunchReconcileResultSummary(checked), err)
	}
	next := workspaceLaunchFailedStorageReplayAuthorization(t, store.row, "recover-after-result-check")
	recovered, err := NewWorkspaceLaunchReconciler(store, adapter).Resume(context.Background(), original.ID, next)
	if err != nil || recovered.Stage != contracts.StageAttachment || adapter.mutations != 1 {
		t.Fatalf("read-only check blocked original-key recovery: %s mutations=%d err=%v", workspaceLaunchReconcileResultSummary(recovered), adapter.mutations, err)
	}
}
