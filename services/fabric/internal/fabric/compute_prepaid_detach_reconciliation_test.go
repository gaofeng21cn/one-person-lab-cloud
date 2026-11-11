package fabric

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrepaidDetachReconciliationRequiresExplicitBoundIntent(t *testing.T) {
	store := NewMemoryOperationStore()
	a := prepaidRetirementFixture(t, store)
	live := &prepaidRetirementLive{noEffect: computePrepaidDetachAction}
	p := live.provider(t, a)
	service := NewServiceWithOperationStore(p, store)
	if _, err := service.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID); err == nil {
		t.Fatal("fixture must retain the original uncertain dispatch")
	}
	before, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := service.latestComputeDestroyOperation(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	fence, err := store.Get(t.Context(), stableID("fop-compute-prepaid-dispatch", a.ID, computePrepaidDetachAction))
	if err != nil {
		t.Fatal(err)
	}
	input := ComputePrepaidDetachReconciliationInput{AccountID: a.AccountID, WorkspaceID: a.WorkspaceID, DestroyOperationID: original.OperationID, DetachOperationID: fence.OperationID, DetachRequestHash: fence.RequestHash, AuthorizationReference: "owner-reconciliation-1", Confirmation: ComputePrepaidDetachReconciliationConfirmation}
	for _, change := range []func(*ComputePrepaidDetachReconciliationInput){
		func(in *ComputePrepaidDetachReconciliationInput) { in.Confirmation = "" },
		func(in *ComputePrepaidDetachReconciliationInput) { in.AuthorizationReference = "" },
		func(in *ComputePrepaidDetachReconciliationInput) { in.AccountID = "foreign-account" },
		func(in *ComputePrepaidDetachReconciliationInput) { in.WorkspaceID = "foreign-workspace" },
		func(in *ComputePrepaidDetachReconciliationInput) { in.DestroyOperationID = "foreign-destroy" },
		func(in *ComputePrepaidDetachReconciliationInput) { in.DetachOperationID = "foreign-detach" },
		func(in *ComputePrepaidDetachReconciliationInput) { in.DetachRequestHash = "foreign-hash" },
	} {
		invalid := input
		change(&invalid)
		if _, err := service.ReconcilePrepaidComputeDetachment(t.Context(), a.ID, invalid); err == nil {
			t.Fatal("unbound recovery intent admitted")
		}
		if live.detach.Load() != 1 || live.retire.Load() != 0 {
			t.Fatal("refusal sent a mutation")
		}
	}
	live.noEffect = ""
	result, err := service.ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input)
	if err != nil || !result.MachineAbsent || !result.DispatchAttempted || live.detach.Load() != 2 || live.retire.Load() != 0 {
		t.Fatalf("recovery=%+v err=%v detach=%d retire=%d", result, err, live.detach.Load(), live.retire.Load())
	}
	after, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, previous := range before {
		found := false
		for _, current := range after {
			if previous.ID == current.ID {
				found = reflect.DeepEqual(previous, current)
			}
		}
		if !found {
			t.Fatal("recovery rewrote original journal/fence")
		}
	}
	restarted := NewServiceWithOperationStore(p, store)
	if _, err := restarted.ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input); err != nil {
		t.Fatal(err)
	}
	if live.detach.Load() != 2 {
		t.Fatal("replay resent recovery")
	}
	if _, err := restarted.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID); err != nil || live.detach.Load() != 2 || live.retire.Load() != 1 {
		t.Fatalf("original deletion did not continue: %v", err)
	}
}

func TestPrepaidDetachReconciliationAcceptsOnlyBoundedProviderFailureClasses(t *testing.T) {
	for _, value := range []string{"compute_prepaid_detach_dispatch_unconfirmed", "compute_prepaid_retirement_unconfirmed",
		"compute_prepaid_detach_dispatch_unconfirmed:AuthFailure", "compute_prepaid_detach_dispatch_unconfirmed:InvalidParameterValue",
		"compute_prepaid_detach_dispatch_unconfirmed:UnsupportedOperation", "compute_prepaid_detach_dispatch_unconfirmed:RequestLimitExceeded",
		"compute_prepaid_detach_dispatch_unconfirmed:InternalError", "compute_prepaid_detach_dispatch_unconfirmed:ResourceNotFound"} {
		if !validUnconfirmedPrepaidDetachError(value) {
			t.Fatalf("valid historical/provider outcome refused: %s", value)
		}
	}
	for _, value := range []string{"compute_prepaid_detach_dispatch_unconfirmed:ClientError", "compute_prepaid_detach_dispatch_unconfirmed:PrivateSubcode",
		"compute_prepaid_retirement_unconfirmed:AuthFailure:SecretKey=leak", "foreign_error"} {
		if validUnconfirmedPrepaidDetachError(value) {
			t.Fatalf("untrusted provider detail admitted: %s", value)
		}
	}
}

// Snapshot the persisted representation: MemoryOperationStore returns payload maps
// by reference, so retaining its structs would miss an in-place history rewrite.
func detachReconciliationSnapshot(t *testing.T, store OperationStore) map[string]string {
	t.Helper()
	rows, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[string]string, len(rows))
	for _, row := range rows {
		if _, exists := out[row.ID]; exists {
			t.Fatalf("duplicate journal ID %q", row.ID)
		}
		data, err := json.Marshal(row)
		if err != nil {
			t.Fatal(err)
		}
		out[row.ID] = string(data)
	}
	return out
}

func assertDetachReconciliationHistoryRetained(t *testing.T, store OperationStore, before map[string]string) {
	t.Helper()
	after := detachReconciliationSnapshot(t, store)
	for id, row := range before {
		if after[id] != row {
			t.Errorf("historical operation %q was removed or rewritten", id)
		}
	}
}

func detachReconciliationSlot(a ComputeAllocation, input ComputePrepaidDetachReconciliationInput, retry bool) FabricOperation {
	key := input.DestroyOperationID + ":" + computePrepaidDetachReconciliationAction
	id := stableID("fop-compute-prepaid-detach-reconciliation", a.ID)
	if retry {
		key += ":auth-retry"
		id = stableID("fop-compute-prepaid-detach-reconciliation-auth-retry", a.ID)
	}
	op := newOperation(computePrepaidDetachReconciliationAction, "compute_prepaid_reconciliation", a.ID, a.AccountID, a.WorkspaceID, key, hashInput(input), time.Time{})
	op.ID, op.CallerService, op.Provider = id, "operator", a.Provider
	op.RedactedProviderPayload = map[string]any{"originalDestroyOperationId": input.DestroyOperationID, "originalDetachOperationId": input.DetachOperationID,
		"originalDetachRequestHash": input.DetachRequestHash, "authorizationReference": input.AuthorizationReference}
	return op
}

func assertDetachReconciliationAttempt(t *testing.T, store OperationStore, a ComputeAllocation, input ComputePrepaidDetachReconciliationInput, retry bool, status, errorCode, requestID string) FabricOperation {
	t.Helper()
	want := detachReconciliationSlot(a, input, retry)
	rows, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var started, terminal []FabricOperation
	for _, row := range rows {
		if row.Action != computePrepaidDetachReconciliationAction || row.OperationID != want.OperationID {
			continue
		}
		if row.ResourceID != want.ResourceID || row.ResourceKind != want.ResourceKind || row.AccountID != want.AccountID || row.WorkspaceID != want.WorkspaceID ||
			row.CallerService != want.CallerService || row.Provider != want.Provider || row.IdempotencyKey != want.IdempotencyKey || row.RequestHash != want.RequestHash ||
			!reflect.DeepEqual(row.RedactedProviderPayload, want.RedactedProviderPayload) || row.StartedAt.IsZero() || row.CreatedAt.IsZero() {
			t.Errorf("attempt lost its immutable binding: %+v; want %+v", row, want)
		}
		if row.Status == "started" {
			started = append(started, row)
		} else {
			terminal = append(terminal, row)
		}
	}
	terminalCount := 1
	if status == "started" {
		terminalCount = 0
	}
	if len(started) != 1 || len(terminal) != terminalCount {
		t.Fatalf("attempt %s: started=%d terminal=%d, want 1+%d rows", want.OperationID, len(started), len(terminal), terminalCount)
	}
	if started[0].ID != want.ID || !started[0].FinishedAt.IsZero() || started[0].ErrorCode != "" || started[0].ProviderRequestID != "" {
		t.Errorf("fixed started fence changed: %+v; want ID=%s", started[0], want.ID)
	}
	if terminalCount == 0 {
		return started[0]
	}
	row := terminal[0]
	if row.ID == started[0].ID || row.Status != status || row.ErrorCode != errorCode || row.ProviderRequestID != requestID ||
		row.FinishedAt.IsZero() || !row.StartedAt.Equal(started[0].StartedAt) {
		t.Errorf("terminal evidence=%+v; want status=%s error=%s request=%s and a separate row", row, status, errorCode, requestID)
	}
	return row
}

func assertDetachReconciliationRowCount(t *testing.T, store OperationStore, want int) {
	t.Helper()
	rows, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.Action == computePrepaidDetachReconciliationAction {
			count++
		}
	}
	if count != want {
		t.Errorf("reconciliation history rows=%d, want %d", count, want)
	}
}

func failFirstDetachReconciliationAuthorization(t *testing.T, store OperationStore, a ComputeAllocation, live *prepaidRetirementLive, p *TencentProvider, input ComputePrepaidDetachReconciliationInput) FabricOperation {
	t.Helper()
	base := p.provision
	p.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
		response, err := base(ctx, request)
		if request.Action == computePrepaidDetachAction {
			response.OK, response.ErrorCode, response.ProviderErrorClass = false, "compute_prepaid_detach_dispatch_unconfirmed", "AuthFailure"
			response.ProviderRequestID = "first-auth-failure-request"
		}
		return response, err
	}
	before := detachReconciliationSnapshot(t, store)
	result, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input)
	p.provision = base
	if err == nil || err.Error() != "compute_prepaid_detach_dispatch_unconfirmed:AuthFailure" || !result.DispatchAttempted || result.MachineAbsent ||
		result.OperationID != detachReconciliationSlot(a, input, false).OperationID || live.detach.Load() != 2 || live.retire.Load() != 0 {
		t.Fatalf("first auth failure=%+v err=%v detach=%d retire=%d", result, err, live.detach.Load(), live.retire.Load())
	}
	assertDetachReconciliationHistoryRetained(t, store, before)
	assertDetachReconciliationRowCount(t, store, 2)
	return assertDetachReconciliationAttempt(t, store, a, input, false, "failed", "compute_prepaid_detach_dispatch_unconfirmed:AuthFailure", "first-auth-failure-request")
}

func TestPrepaidDetachReconciliationAllowsOneExplicitRetryAfterAuthFailure(t *testing.T) {
	for _, outcome := range []string{"succeeded", "auth_failure", "unknown_reply", "other_failure"} {
		t.Run(outcome, func(t *testing.T) {
			store := NewMemoryOperationStore()
			a, live, p, input := reconciliationFixture(t, store)
			failFirstDetachReconciliationAuthorization(t, store, a, live, p, input)
			firstHistory := detachReconciliationSnapshot(t, store)
			firstID := detachReconciliationSlot(a, input, false).OperationID
			for range 2 {
				result, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input)
				if err != nil || result.OperationID != firstID || !result.DispatchAttempted || result.MachineAbsent || live.detach.Load() != 2 {
					t.Fatalf("same first authorization must only replay: result=%+v err=%v detach=%d", result, err, live.detach.Load())
				}
			}
			if !reflect.DeepEqual(firstHistory, detachReconciliationSnapshot(t, store)) {
				t.Fatal("read-only first replay appended or changed history")
			}

			base := p.provision
			wantStatus, wantError, wantRequest := "failed", "", "retry-provider-request"
			switch outcome {
			case "succeeded":
				live.noEffect = ""
				wantStatus = "succeeded"
			case "auth_failure":
				wantError = "compute_prepaid_detach_dispatch_unconfirmed:AuthFailure"
			case "unknown_reply":
				wantError, wantRequest = "retry_reply_lost", ""
			case "other_failure":
				wantError = "compute_prepaid_detach_dispatch_unconfirmed:InternalError"
			}
			p.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
				response, err := base(ctx, request)
				if request.Action == computePrepaidDetachAction {
					response.ProviderRequestID = wantRequest
					switch outcome {
					case "unknown_reply":
						return response, fmt.Errorf("retry_reply_lost")
					case "auth_failure", "other_failure":
						response.OK, response.ErrorCode = false, "compute_prepaid_detach_dispatch_unconfirmed"
						response.ProviderErrorClass = map[string]string{"auth_failure": "AuthFailure", "other_failure": "InternalError"}[outcome]
					}
				}
				return response, err
			}
			retryInput := input
			retryInput.AuthorizationReference = "operator-reconciliation-auth-retry"
			wantRetry := detachReconciliationSlot(a, retryInput, true)
			result, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, retryInput)
			if (err == nil) != (outcome == "succeeded") || err != nil && err.Error() != wantError || !result.DispatchAttempted ||
				result.MachineAbsent != (outcome == "succeeded") || result.OperationID != wantRetry.OperationID || live.detach.Load() != 3 || live.retire.Load() != 0 {
				t.Fatalf("retry=%+v err=%v detach=%d retire=%d; want op=%s error=%s", result, err, live.detach.Load(), live.retire.Load(), wantRetry.OperationID, wantError)
			}
			assertDetachReconciliationHistoryRetained(t, store, firstHistory)
			assertDetachReconciliationAttempt(t, store, a, input, false, "failed", "compute_prepaid_detach_dispatch_unconfirmed:AuthFailure", "first-auth-failure-request")
			assertDetachReconciliationAttempt(t, store, a, retryInput, true, wantStatus, wantError, wantRequest)
			assertDetachReconciliationRowCount(t, store, 4)
			completed := detachReconciliationSnapshot(t, store)
			for _, replay := range []ComputePrepaidDetachReconciliationInput{input, retryInput} {
				wantID := firstID
				if replay.AuthorizationReference == retryInput.AuthorizationReference {
					wantID = wantRetry.OperationID
				}
				result, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, replay)
				if err != nil || !result.DispatchAttempted || result.OperationID != wantID || live.detach.Load() != 3 {
					t.Errorf("accepted authorization must only replay its attempt: result=%+v err=%v detach=%d want op=%s", result, err, live.detach.Load(), wantID)
				}
			}
			for _, reference := range []string{"operator-third-authorization", "operator-fourth-authorization"} {
				retryInput.AuthorizationReference = reference
				if _, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, retryInput); err == nil || live.detach.Load() != 3 {
					t.Errorf("third attempt admitted after %s: err=%v detach=%d", outcome, err, live.detach.Load())
				}
			}
			if !reflect.DeepEqual(completed, detachReconciliationSnapshot(t, store)) || live.retire.Load() != 0 {
				t.Fatal("replays/refusals changed completed history or retired the CVM")
			}
		})
	}
}

func TestPrepaidDetachReconciliationRetryRequiresCurrentBoundRunningReadback(t *testing.T) {
	for _, scenario := range []string{"failed_read", "unknown_machine", "unknown_tke", "not_running", "foreign_instance", "foreign_pool", "already_absent", "missing_authorization"} {
		t.Run(scenario, func(t *testing.T) {
			store := NewMemoryOperationStore()
			a, live, p, input := reconciliationFixture(t, store)
			failFirstDetachReconciliationAuthorization(t, store, a, live, p, input)
			before := detachReconciliationSnapshot(t, store)
			input.AuthorizationReference = "operator-new-authorization"
			if scenario == "missing_authorization" {
				input.AuthorizationReference = ""
			}
			if scenario == "already_absent" {
				live.state.Store(1)
			}
			base := p.provision
			var reads atomic.Int32
			p.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
				if request.Action == "read_compute_destroy_status" {
					reads.Add(1)
					if scenario == "failed_read" {
						return provisionerResponse{}, fmt.Errorf("current_read_unavailable")
					}
				}
				response, err := base(ctx, request)
				if request.Action == "read_compute_destroy_status" {
					switch scenario {
					case "unknown_machine":
						response.MachinePresent = nil
					case "unknown_tke":
						response.TKEStatus = "UNKNOWN"
					case "not_running":
						response.TKEStatus = "STOPPED"
					case "foreign_instance":
						response.InstanceID = "ins-foreign"
					case "foreign_pool":
						response.ProviderData["nodePoolId"] = "pool-foreign"
					}
				}
				return response, err
			}
			result, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input)
			if scenario == "already_absent" {
				if err != nil || !result.MachineAbsent || result.DispatchAttempted {
					t.Errorf("absence must be a read-only no-op: result=%+v err=%v", result, err)
				}
			} else if err == nil {
				t.Errorf("%s admitted an auth retry: %+v", scenario, result)
			}
			if scenario != "missing_authorization" && reads.Load() == 0 {
				t.Error("retry admission did not read current provider facts")
			}
			if live.detach.Load() != 2 || live.retire.Load() != 0 || !reflect.DeepEqual(before, detachReconciliationSnapshot(t, store)) {
				t.Fatal("ineligible retry mutated provider or journal")
			}
			if _, err := store.Get(t.Context(), detachReconciliationSlot(a, input, true).ID); err != ErrOperationNotFound {
				t.Fatalf("ineligible retry consumed fixed retry slot: %v", err)
			}
		})
	}
}

func TestPrepaidDetachReconciliationOtherFirstOutcomesNeverAuthorizeRetry(t *testing.T) {
	for _, outcome := range []string{"succeeded", "unknown_reply", "unconfirmed", "other_failure", "auth_without_request_id"} {
		t.Run(outcome, func(t *testing.T) {
			store := NewMemoryOperationStore()
			a, live, p, input := reconciliationFixture(t, store)
			before := detachReconciliationSnapshot(t, store)
			base := p.provision
			wantStatus, wantError, wantRequest := "failed", "", "first-provider-request"
			switch outcome {
			case "succeeded":
				live.noEffect, wantStatus = "", "succeeded"
			case "unknown_reply":
				wantError, wantRequest = "first_reply_lost", ""
			case "unconfirmed":
				wantError = "compute_prepaid_detach_reconciliation_unconfirmed"
			case "other_failure":
				wantError = "compute_prepaid_detach_dispatch_unconfirmed:InternalError"
			case "auth_without_request_id":
				wantError, wantRequest = "compute_prepaid_detach_dispatch_unconfirmed:AuthFailure", ""
			}
			p.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
				response, err := base(ctx, request)
				if request.Action == computePrepaidDetachAction {
					response.ProviderRequestID = wantRequest
					switch outcome {
					case "unknown_reply":
						return response, fmt.Errorf("first_reply_lost")
					case "other_failure", "auth_without_request_id":
						response.OK, response.ErrorCode = false, "compute_prepaid_detach_dispatch_unconfirmed"
						response.ProviderErrorClass = map[string]string{"other_failure": "InternalError", "auth_without_request_id": "AuthFailure"}[outcome]
					}
				}
				return response, err
			}
			result, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input)
			if (err == nil) != (outcome == "succeeded") || err != nil && err.Error() != wantError || !result.DispatchAttempted || live.detach.Load() != 2 {
				t.Fatalf("first outcome=%+v err=%v detach=%d", result, err, live.detach.Load())
			}
			assertDetachReconciliationHistoryRetained(t, store, before)
			assertDetachReconciliationAttempt(t, store, a, input, false, wantStatus, wantError, wantRequest)
			assertDetachReconciliationRowCount(t, store, 2)
			completed := detachReconciliationSnapshot(t, store)
			if _, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input); err != nil {
				t.Errorf("same reference must replay first outcome: %v", err)
			}
			input.AuthorizationReference = "operator-new-authorization"
			if _, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input); err == nil {
				t.Errorf("%s first outcome authorized another attempt", outcome)
			}
			if live.detach.Load() != 2 || live.retire.Load() != 0 || !reflect.DeepEqual(completed, detachReconciliationSnapshot(t, store)) {
				t.Fatal("ineligible first outcome sent again or changed history")
			}
		})
	}
}

func reconciliationFixture(t *testing.T, store OperationStore) (ComputeAllocation, *prepaidRetirementLive, *TencentProvider, ComputePrepaidDetachReconciliationInput) {
	t.Helper()
	a := prepaidRetirementFixture(t, store)
	live := &prepaidRetirementLive{noEffect: computePrepaidDetachAction}
	p := live.provider(t, a)
	s := NewServiceWithOperationStore(p, store)
	if _, err := s.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID); err == nil {
		t.Fatal("original call must remain uncertain")
	}
	destroy, _, err := s.latestComputeDestroyOperation(t.Context(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	fence, err := store.Get(t.Context(), stableID("fop-compute-prepaid-dispatch", a.ID, computePrepaidDetachAction))
	if err != nil {
		t.Fatal(err)
	}
	return a, live, p, ComputePrepaidDetachReconciliationInput{AccountID: a.AccountID, WorkspaceID: a.WorkspaceID,
		DestroyOperationID: destroy.OperationID, DetachOperationID: fence.OperationID, DetachRequestHash: fence.RequestHash,
		AuthorizationReference: "operator-reconciliation-original", Confirmation: ComputePrepaidDetachReconciliationConfirmation}
}

func TestPrepaidDetachReconciliationUncertaintyAndReadback(t *testing.T) {
	for _, scenario := range []string{"unconfirmed", "already_absent", "failed_read", "foreign_read", "lost_reply_with_progress"} {
		t.Run(scenario, func(t *testing.T) {
			store := NewMemoryOperationStore()
			a, live, p, input := reconciliationFixture(t, store)
			base := p.provision
			if scenario == "already_absent" {
				live.state.Store(1)
			}
			if scenario == "lost_reply_with_progress" {
				live.noEffect, live.lostReply = "", true
			}
			p.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
				if request.Action == "read_compute_destroy_status" && scenario == "failed_read" {
					return provisionerResponse{}, fmt.Errorf("read unavailable")
				}
				out, err := base(ctx, request)
				if request.Action == "read_compute_destroy_status" && scenario == "foreign_read" {
					out.InstanceID = "ins-foreign"
				}
				return out, err
			}
			result, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input)
			wantDispatch := scenario == "unconfirmed" || scenario == "lost_reply_with_progress"
			wantAbsence := scenario == "already_absent" || scenario == "lost_reply_with_progress"
			if (err == nil) != wantAbsence || result.DispatchAttempted != wantDispatch || result.MachineAbsent != wantAbsence {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if live.detach.Load() != 1+map[bool]int32{true: 1, false: 0}[wantDispatch] || live.retire.Load() != 0 {
				t.Fatal("unexpected mutation")
			}
			if !wantDispatch {
				if _, err := store.Get(t.Context(), stableID("fop-compute-prepaid-detach-reconciliation", a.ID)); err != ErrOperationNotFound {
					t.Fatalf("refused/no-op command consumed a fence: %v", err)
				}
				return
			}
			for range 2 {
				_, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input)
				if err != nil || live.detach.Load() != 2 {
					t.Fatalf("replay resent command: %v", err)
				}
			}
			input.AuthorizationReference = "different-intent"
			if _, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input); err == nil || live.detach.Load() != 2 {
				t.Fatal("changed authorization admitted another send")
			}
		})
	}
}

func testDetachReconciliationAtomicClaim(t *testing.T, first, second OperationStore) {
	t.Helper()
	a, live, p, input := reconciliationFixture(t, first)
	base := p.provision
	var initialReads sync.WaitGroup
	initialReads.Add(2)
	var reads atomic.Int32
	p.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
		if request.Action == "read_compute_destroy_status" && reads.Add(1) <= 2 {
			initialReads.Done()
			initialReads.Wait()
		}
		return base(ctx, request)
	}
	var wg sync.WaitGroup
	for _, store := range []OperationStore{first, second} {
		wg.Add(1)
		go func(store OperationStore) {
			defer wg.Done()
			_, _ = NewServiceWithOperationStore(p, computeFinalReleaseUnlockedStore{store}).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input)
		}(store)
	}
	wg.Wait()
	if live.detach.Load() != 2 || live.retire.Load() != 0 {
		t.Fatalf("concurrent owner resent reconciliation: detach=%d retire=%d", live.detach.Load(), live.retire.Load())
	}
}

func TestPrepaidDetachReconciliationAtomicClaim(t *testing.T) {
	store := NewMemoryOperationStore()
	testDetachReconciliationAtomicClaim(t, store, store)
}

func TestPostgresPrepaidDetachReconciliationAtomicClaim(t *testing.T) {
	u := fabricTestDatabaseURL(t)
	first, err := newTestPostgresOperationStore(u)
	if err != nil {
		t.Fatal(err)
	}
	defer first.client.Close()
	second, err := newTestPostgresOperationStore(u)
	if err != nil {
		t.Fatal(err)
	}
	defer second.client.Close()
	testDetachReconciliationAtomicClaim(t, first, second)
}

func TestPrepaidDetachReconciliationRejectsMalformedHistoricalAttempts(t *testing.T) {
	cases := []struct {
		name   string
		target string
		change func(*FabricOperation)
	}{
		{"unfinished_auth_failure", "terminal", func(row *FabricOperation) { row.FinishedAt = time.Time{} }},
		{"auth_without_request_id", "terminal", func(row *FabricOperation) { row.ProviderRequestID = "" }},
		{"wrong_error_prefix", "terminal", func(row *FabricOperation) { row.ErrorCode = "compute_prepaid_retirement_unconfirmed:AuthFailure" }},
		{"unknown_error_class", "terminal", func(row *FabricOperation) {
			row.ErrorCode = "compute_prepaid_detach_dispatch_unconfirmed:PrivateSubcode"
		}},
		{"succeeded_with_auth_code", "terminal", func(row *FabricOperation) { row.Status = "succeeded" }},
		{"unknown_status", "terminal", func(row *FabricOperation) { row.Status = "unknown" }},
		{"wrong_started_id", "started", func(row *FabricOperation) { row.ID = "foreign-first-slot" }},
		{"unknown_operation_id", "both", func(row *FabricOperation) { row.OperationID = "foreign-reconciliation-operation" }},
		{"wrong_idempotency_key", "both", func(row *FabricOperation) { row.IdempotencyKey += ":foreign" }},
		{"wrong_request_hash", "both", func(row *FabricOperation) { row.RequestHash = hashInput("foreign-request") }},
		{"foreign_account", "terminal", func(row *FabricOperation) { row.AccountID = "foreign-account" }},
		{"foreign_workspace", "terminal", func(row *FabricOperation) { row.WorkspaceID = "foreign-workspace" }},
		{"foreign_provider", "terminal", func(row *FabricOperation) { row.Provider = "foreign-provider" }},
		{"wrong_caller", "terminal", func(row *FabricOperation) { row.CallerService = "workspace" }},
		{"wrong_resource_kind", "terminal", func(row *FabricOperation) { row.ResourceKind = "compute_allocation" }},
		{"foreign_destroy_binding", "terminal", func(row *FabricOperation) {
			row.RedactedProviderPayload["originalDestroyOperationId"] = "foreign-destroy"
		}},
		{"foreign_detach_binding", "terminal", func(row *FabricOperation) {
			row.RedactedProviderPayload["originalDetachOperationId"] = "foreign-detach"
		}},
		{"foreign_detach_hash", "terminal", func(row *FabricOperation) {
			row.RedactedProviderPayload["originalDetachRequestHash"] = hashInput("foreign-detach")
		}},
		{"different_terminal_authorization", "terminal", func(row *FabricOperation) {
			row.RedactedProviderPayload["authorizationReference"] = "unaccepted-authorization"
		}},
		{"unexpected_payload", "terminal", func(row *FabricOperation) { row.RedactedProviderPayload["unknown"] = true }},
		{"missing_started", "drop_started", nil},
		{"still_in_progress", "drop_terminal", nil},
		{"duplicate_terminal", "duplicate_terminal", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := NewMemoryOperationStore()
			a, live, p, input := reconciliationFixture(t, source)
			failFirstDetachReconciliationAuthorization(t, source, a, live, p, input)
			// Load a distinct persisted historical dataset; never repair/rewrite the
			// source store to manufacture retry eligibility.
			store := NewMemoryOperationStore()
			archived := detachReconciliationSnapshot(t, source)
			history, err := source.List(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			for _, original := range history {
				var row FabricOperation
				if err := json.Unmarshal([]byte(archived[original.ID]), &row); err != nil {
					t.Fatal(err)
				}
				if row.Action == computePrepaidDetachReconciliationAction {
					started := row.Status == "started"
					if tc.target == "drop_started" && started || tc.target == "drop_terminal" && !started {
						continue
					}
					if tc.target == "both" || tc.target == "started" && started || tc.target == "terminal" && !started {
						tc.change(&row)
					}
					if tc.target == "duplicate_terminal" && !started {
						duplicate := row
						duplicate.ID += "-duplicate"
						if err := store.Append(t.Context(), duplicate); err != nil {
							t.Fatal(err)
						}
					}
				}
				if err := store.Append(t.Context(), row); err != nil {
					t.Fatal(err)
				}
			}
			before := detachReconciliationSnapshot(t, store)
			input.AuthorizationReference = "operator-new-authorization"
			if result, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input); err == nil {
				t.Errorf("malformed historical attempt admitted: %+v", result)
			}
			if live.detach.Load() != 2 || live.retire.Load() != 0 || !reflect.DeepEqual(before, detachReconciliationSnapshot(t, store)) {
				t.Fatal("malformed history sent a mutation or was rewritten")
			}
		})
	}
}

func TestPrepaidDetachReconciliationInFlightRetryNeverAdmitsThirdAttempt(t *testing.T) {
	store := NewMemoryOperationStore()
	a, live, p, input := reconciliationFixture(t, store)
	failFirstDetachReconciliationAuthorization(t, store, a, live, p, input)
	before := detachReconciliationSnapshot(t, store)
	input.AuthorizationReference = "operator-auth-retry-in-flight"
	base := p.provision
	entered, release := make(chan struct{}), make(chan struct{})
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var releaseOnce sync.Once
	var wg sync.WaitGroup
	defer func() {
		releaseOnce.Do(func() { close(release) })
		wg.Wait()
	}()
	p.provision = func(ctx context.Context, request provisionerRequest) (provisionerResponse, error) {
		response, err := base(ctx, request)
		if request.Action == computePrepaidDetachAction {
			if live.detach.Load() != 3 {
				return response, fmt.Errorf("unexpected_third_reconciliation_send")
			}
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return response, ctx.Err()
			}
			response.OK, response.ErrorCode, response.ProviderErrorClass = false, "compute_prepaid_detach_dispatch_unconfirmed", "AuthFailure"
			response.ProviderRequestID = "retry-auth-failure-request"
		}
		return response, err
	}
	type completion struct {
		result ComputePrepaidDetachReconciliationResult
		err    error
	}
	done := make(chan completion, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		result, err := NewServiceWithOperationStore(p, computeFinalReleaseUnlockedStore{store}).ReconcilePrepaidComputeDetachment(ctx, a.ID, input)
		done <- completion{result, err}
	}()
	select {
	case <-entered:
	case result := <-done:
		t.Fatalf("retry did not reach provider: %+v err=%v", result.result, result.err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	assertDetachReconciliationAttempt(t, store, a, input, true, "started", "", "")
	assertDetachReconciliationRowCount(t, store, 3)
	pending := detachReconciliationSnapshot(t, store)
	for _, reference := range []string{input.AuthorizationReference, "operator-third-while-in-flight"} {
		replay := input
		replay.AuthorizationReference = reference
		result, err := NewServiceWithOperationStore(p, computeFinalReleaseUnlockedStore{store}).ReconcilePrepaidComputeDetachment(ctx, a.ID, replay)
		if reference == input.AuthorizationReference {
			if err != nil || !result.DispatchAttempted || result.OperationID != detachReconciliationSlot(a, input, true).OperationID {
				t.Errorf("pending same-reference replay=%+v err=%v", result, err)
			}
		} else if err == nil {
			t.Error("in-flight retry admitted a third authorization")
		}
	}
	if live.detach.Load() != 3 || !reflect.DeepEqual(pending, detachReconciliationSnapshot(t, store)) {
		t.Fatal("pending retry replay changed history or resent")
	}
	releaseOnce.Do(func() { close(release) })
	completed := <-done
	if completed.err == nil || completed.err.Error() != "compute_prepaid_detach_dispatch_unconfirmed:AuthFailure" {
		t.Fatalf("in-flight retry completion=%+v err=%v", completed.result, completed.err)
	}
	assertDetachReconciliationAttempt(t, store, a, input, true, "failed", "compute_prepaid_detach_dispatch_unconfirmed:AuthFailure", "retry-auth-failure-request")
	assertDetachReconciliationHistoryRetained(t, store, before)
	assertDetachReconciliationHistoryRetained(t, store, pending)
	assertDetachReconciliationRowCount(t, store, 4)
	terminal := detachReconciliationSnapshot(t, store)
	input.AuthorizationReference = "operator-third-after-in-flight-failure"
	if _, err := NewServiceWithOperationStore(p, store).ReconcilePrepaidComputeDetachment(ctx, a.ID, input); err == nil || live.detach.Load() != 3 || live.retire.Load() != 0 {
		t.Fatalf("completed retry admitted third attempt: err=%v detach=%d retire=%d", err, live.detach.Load(), live.retire.Load())
	}
	if !reflect.DeepEqual(terminal, detachReconciliationSnapshot(t, store)) {
		t.Fatal("third authorization changed completed retry history")
	}
}

// Both contenders reach the durable claim before either can dispatch. Removing
// lock exclusion proves that distinct authorizations share one fixed retry slot.
type detachReconciliationRetryClaimBarrier struct {
	computeFinalReleaseUnlockedStore
	claims chan<- FabricOperation
	ready  <-chan struct{}
	arrive func()
}

func (s detachReconciliationRetryClaimBarrier) ClaimRuntime(ctx context.Context, operation FabricOperation) (FabricOperation, bool, error) {
	if operation.Action == computePrepaidDetachReconciliationAction {
		s.claims <- operation
		s.arrive()
		select {
		case <-s.ready:
		case <-ctx.Done():
			return FabricOperation{}, false, ctx.Err()
		}
	}
	return s.OperationStore.ClaimRuntime(ctx, operation)
}

func testDetachReconciliationAuthRetryAtomicClaim(t *testing.T, first, second OperationStore) {
	t.Helper()
	a, live, p, input := reconciliationFixture(t, first)
	failFirstDetachReconciliationAuthorization(t, first, a, live, p, input)
	before := detachReconciliationSnapshot(t, first)
	live.noEffect = ""
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	claims, ready := make(chan FabricOperation, 2), make(chan struct{})
	var arrived atomic.Int32
	arrive := func() {
		if arrived.Add(1) == 2 {
			close(ready)
		}
	}
	type completion struct {
		input  ComputePrepaidDetachReconciliationInput
		result ComputePrepaidDetachReconciliationResult
		err    error
	}
	done := make(chan completion, 2)
	for index, store := range []OperationStore{first, second} {
		retryInput := input
		retryInput.AuthorizationReference = fmt.Sprintf("operator-concurrent-auth-retry-%d", index)
		wrapped := detachReconciliationRetryClaimBarrier{computeFinalReleaseUnlockedStore{store}, claims, ready, arrive}
		service := NewServiceWithOperationStore(p, wrapped)
		go func() {
			result, err := service.ReconcilePrepaidComputeDetachment(ctx, a.ID, retryInput)
			done <- completion{retryInput, result, err}
		}()
	}
	outcomes := []completion{<-done, <-done}
	if ctx.Err() != nil {
		t.Fatalf("contenders did not both reach the durable retry claim: arrived=%d err=%v", arrived.Load(), ctx.Err())
	}
	if arrived.Load() != 2 || len(claims) != 2 {
		t.Fatalf("claim contenders=%d, want 2", arrived.Load())
	}
	for range 2 {
		claim := <-claims
		want := detachReconciliationSlot(a, input, true)
		if claim.ID != want.ID || claim.IdempotencyKey != want.IdempotencyKey || claim.OperationID != want.OperationID {
			t.Errorf("authorization selected a different retry slot: %+v; want ID=%s key=%s op=%s", claim, want.ID, want.IdempotencyKey, want.OperationID)
		}
	}
	var winner *completion
	successes := 0
	for index := range outcomes {
		outcome := &outcomes[index]
		if outcome.err == nil {
			successes++
			winner = outcome
			if !outcome.result.DispatchAttempted || !outcome.result.MachineAbsent || outcome.result.OperationID != detachReconciliationSlot(a, outcome.input, true).OperationID {
				t.Errorf("winning claim=%+v", outcome.result)
			}
		}
	}
	if successes != 1 || live.detach.Load() != 3 || live.retire.Load() != 0 {
		t.Fatalf("different references under lost locks: successes=%d detach=%d retire=%d outcomes=%+v", successes, live.detach.Load(), live.retire.Load(), outcomes)
	}
	assertDetachReconciliationHistoryRetained(t, first, before)
	assertDetachReconciliationAttempt(t, second, a, winner.input, true, "succeeded", "", "retirement-request")
	assertDetachReconciliationRowCount(t, first, 4)
	completed := detachReconciliationSnapshot(t, first)
	if !reflect.DeepEqual(completed, detachReconciliationSnapshot(t, second)) {
		t.Fatal("independent stores disagree about persisted attempts")
	}
	if _, err := NewServiceWithOperationStore(p, second).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, winner.input); err != nil {
		t.Errorf("winning authorization did not replay after restart: %v", err)
	}
	input.AuthorizationReference = "operator-third-after-concurrent-retry"
	if _, err := NewServiceWithOperationStore(p, second).ReconcilePrepaidComputeDetachment(t.Context(), a.ID, input); err == nil {
		t.Error("concurrent retry admitted a third attempt")
	}
	if live.detach.Load() != 3 || live.retire.Load() != 0 || !reflect.DeepEqual(completed, detachReconciliationSnapshot(t, first)) {
		t.Fatal("concurrent retry replay/refusal changed provider or journal")
	}
}

func TestPrepaidDetachReconciliationAuthRetryAtomicClaimSurvivesLostLockExclusion(t *testing.T) {
	store := NewMemoryOperationStore()
	testDetachReconciliationAuthRetryAtomicClaim(t, store, store)
}

func TestPostgresPrepaidDetachReconciliationAuthRetryAtomicClaimSurvivesLostLockExclusion(t *testing.T) {
	u := fabricTestDatabaseURL(t)
	first, err := newTestPostgresOperationStore(u)
	if err != nil {
		t.Fatal(err)
	}
	defer first.client.Close()
	second, err := newTestPostgresOperationStore(u)
	if err != nil {
		t.Fatal(err)
	}
	defer second.client.Close()
	testDetachReconciliationAuthRetryAtomicClaim(t, first, second)
}
