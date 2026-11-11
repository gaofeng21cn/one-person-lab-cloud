package fabric

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
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
