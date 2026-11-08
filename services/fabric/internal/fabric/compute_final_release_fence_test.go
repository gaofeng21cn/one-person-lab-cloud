package fabric

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// Simulate two senders whose advisory locks no longer exclude one another.
// The durable primary-key claim must remain the final dispatch boundary.
type computeFinalReleaseUnlockedStore struct{ OperationStore }

func (s computeFinalReleaseUnlockedStore) WithPoolLock(ctx context.Context, _ string, fn func(context.Context) error) error {
	return fn(ctx)
}

func TestComputeFinalReleaseAtomicClaimSurvivesLostLockExclusion(t *testing.T) {
	store := NewMemoryOperationStore()
	testComputeFinalReleaseAtomicClaim(t, store, store)
}

func TestPostgresComputeFinalReleaseAtomicClaimSurvivesLostLockExclusion(t *testing.T) {
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
	testComputeFinalReleaseAtomicClaim(t, first, second)
}

func testComputeFinalReleaseAtomicClaim(t *testing.T, store, second OperationStore) {
	t.Helper()
	a, input := finalReleaseFixture(t, store)
	var reads, sends atomic.Int32
	var ready sync.WaitGroup
	ready.Add(2)
	p := NewTencentProvider()
	p.provision = func(_ context.Context, r provisionerRequest) (provisionerResponse, error) {
		if r.Action == computeFinalReleaseAction {
			sends.Add(1)
			return provisionerResponse{OK: true, InstanceID: a.InstanceID, ProviderRequestID: "terminate-accepted", MutationCount: 1}, nil
		}
		if r.Action != "read_compute_destroy_status" {
			t.Errorf("unexpected action: %s", r.Action)
			return provisionerResponse{}, nil
		}
		if reads.Add(1) <= 2 {
			ready.Done()
			ready.Wait()
		}
		response := computeDestroyStatusResponse(r, false, "NOT_FOUND", "SHUTDOWN")
		response.ProviderData["clusterId"], response.ProviderData["region"] = r.Pool.ClusterID, r.Region
		response.ProviderData["isolatedSource"] = "MANMADE"
		return response, nil
	}
	services := []*Service{NewServiceWithOperationStore(p, computeFinalReleaseUnlockedStore{store}), NewServiceWithOperationStore(p, computeFinalReleaseUnlockedStore{second})}
	var finished sync.WaitGroup
	for _, service := range services {
		finished.Add(1)
		go func(s *Service) {
			defer finished.Done()
			result, err := s.ReleaseRecycledComputeAllocation(t.Context(), a.ID, input)
			if !result.DispatchAttempted {
				t.Errorf("missing dispatch fence: %+v err=%v", result, err)
			}
		}(service)
	}
	finished.Wait()
	if sends.Load() != 1 {
		t.Fatalf("provider sends=%d, want exactly one", sends.Load())
	}
	fence, err := store.Get(t.Context(), stableID("fop-compute-final-release-dispatch", a.ID))
	if err != nil || fence.Status != "started" {
		t.Fatalf("durable claim=%+v err=%v", fence, err)
	}
}

func TestComputeFinalReleaseEligibilityNeverReusesRetainedIsolationSource(t *testing.T) {
	a := computeDestroyReadbackAllocation("compute-stale-isolation", tencentComputeDestroyPhaseAttempted, "1")
	a.ProviderData["isolatedSource"] = "MANMADE"
	p := NewTencentProvider()
	p.provision = func(_ context.Context, r provisionerRequest) (provisionerResponse, error) {
		return computeDestroyStatusResponse(r, false, "NOT_FOUND", "SHUTDOWN"), nil
	}
	observed, err := p.ReadComputeDestroyStatus(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	if ComputeFinalReleaseEligible(observed) || observed.ProviderData["isolatedSource"] != "" {
		t.Fatalf("retained isolation was reused: %+v", observed)
	}
	if a.ProviderData["isolatedSource"] != "MANMADE" {
		t.Fatal("readback changed original allocation")
	}
}

func TestComputeFinalReleaseContinuesOriginalDestroyFinalization(t *testing.T) {
	store := NewMemoryOperationStore()
	a, input := finalReleaseFixture(t, store)
	ownership := MachineOwnership{ID: "owner-final-release", ResourceID: a.ID, AccountID: a.AccountID, WorkspaceID: a.WorkspaceID, PackageID: a.PackageID, NodePoolID: a.NodePoolID, MachineID: a.MachineName, InstanceID: a.InstanceID, NodeName: a.NodeName, Status: "active"}
	if _, claimed, err := store.ClaimMachine(t.Context(), ownership); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	calls, absent := 0, false
	p := finalReleaseProvider(t, &absent, &calls)
	var cleanup atomic.Int32
	p.kubectl = func(context.Context, []string, []byte) ([]byte, error) { cleanup.Add(1); return nil, nil }
	s := NewServiceWithOperationStore(p, store)
	if _, err := s.DestroyComputeAllocation(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	waitForComputeDestroyPhaseOperationCount(t, s, a.ID, "failed", 1)
	if _, err := s.ReleaseRecycledComputeAllocation(t.Context(), a.ID, input); err == nil || calls != 1 {
		t.Fatalf("release err=%v calls=%d", err, calls)
	}
	absent = true
	if result, err := s.ReleaseRecycledComputeAllocation(t.Context(), a.ID, input); err != nil || !result.AbsenceConfirmed {
		t.Fatalf("release result=%+v err=%v", result, err)
	}
	if done, err := s.ComputeDeletionFinalized(t.Context(), a.ID); err != nil || done {
		t.Fatalf("cloud absence must wait for original cleanup: done=%v err=%v", done, err)
	}
	if _, err := s.DestroyComputeAllocation(t.Context(), a.ID); err != nil {
		t.Fatal(err)
	}
	waitForComputeDestroyPhaseOperationCount(t, s, a.ID, "succeeded", 1)
	if done, err := s.ComputeDeletionFinalized(t.Context(), a.ID); err != nil || !done {
		t.Fatalf("finalized=%v err=%v", done, err)
	}
	released, err := store.MachineOwnership(t.Context(), a.ID)
	if err != nil || released.Status != "released" || cleanup.Load() != 1 || calls != 1 {
		t.Fatalf("ownership=%+v err=%v cleanup=%d sends=%d", released, err, cleanup.Load(), calls)
	}
}
