package fabric

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func finalReleaseFixture(t *testing.T, store OperationStore) (ComputeAllocation, ComputeFinalReleaseInput) {
	t.Helper()
	a := canonicalTencentComputeDestroyFixture()
	a.Status = "destroying"
	a.DestroyState = StorageDestroyStateUnconfirmedSend
	op := newOperation("destroy_compute_allocation", "compute_allocation", a.ID, a.AccountID, a.WorkspaceID, "", hashInput(map[string]string{"id": a.ID}), time.Now().UTC())
	op.Status = "started"
	op.ID = "destroy-original"
	op.CreatedAt = op.StartedAt
	fillOperationResource(&op, a)
	if err := store.Append(t.Context(), op); err != nil {
		t.Fatal(err)
	}
	return a, ComputeFinalReleaseInput{AccountID: a.AccountID, WorkspaceID: a.WorkspaceID, DestroyOperationID: op.OperationID, Confirmation: ComputeFinalReleaseConfirmation}
}

func finalReleaseProvider(t *testing.T, absent *bool, calls *int) *TencentProvider {
	t.Helper()
	p := NewTencentProvider()
	p.provision = func(_ context.Context, r provisionerRequest) (provisionerResponse, error) {
		if r.Action == computeFinalReleaseAction {
			*calls++
			return provisionerResponse{}, errors.New("lost response")
		}
		if r.Action != "read_compute_destroy_status" {
			t.Fatalf("unexpected action %s", r.Action)
		}
		present := false
		state := "SHUTDOWN"
		status := "present"
		if *absent {
			state = "NOT_FOUND"
			status = "external_deleted"
		}
		return provisionerResponse{OK: true, Status: status, ProviderRequestID: "read-provider", InstanceID: r.Allocation.InstanceID, MachinePresent: &present, TKEStatus: "NOT_FOUND", CVMStatus: state, ProviderData: map[string]string{
			"clusterId": r.Pool.ClusterID, "region": r.Region, "nodePoolId": r.Pool.NodePoolID, "machineName": r.Allocation.MachineName, "nodeName": r.Allocation.NodeName, "privateIp": r.Allocation.PrivateIP, "machineType": "NativeCVM", "cvmApplicable": "true", "machinePresent": "false", "tkeStatus": "NOT_FOUND", "cvmStatus": state, "syncResult": "missing", "describeClusterMachinesReq": "read-machine", "describeCvmRequestId": "read-cvm",
		}}, nil
	}
	return p
}

func testFinalReleaseRestart(t *testing.T, store OperationStore, reopen func(OperationStore) OperationStore) {
	a, input := finalReleaseFixture(t, store)
	calls := 0
	absent := false
	p := finalReleaseProvider(t, &absent, &calls)
	s := NewServiceWithOperationStore(p, store)
	before, _, _ := s.latestComputeDestroyOperation(t.Context(), a.ID)
	if _, err := s.ReleaseRecycledComputeAllocation(t.Context(), a.ID, input); err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	store = reopen(store)
	s = NewServiceWithOperationStore(p, store)
	got, err := s.ReleaseRecycledComputeAllocation(t.Context(), a.ID, input)
	if err != nil || got.AbsenceConfirmed || !got.DispatchAttempted || calls != 1 {
		t.Fatalf("pending=%+v err=%v calls=%d", got, err, calls)
	}
	absent = true
	got, err = s.ReleaseRecycledComputeAllocation(t.Context(), a.ID, input)
	if err != nil || !got.AbsenceConfirmed || got.Status != "succeeded" || calls != 1 {
		t.Fatalf("confirmed=%+v err=%v calls=%d", got, err, calls)
	}
	after, _, _ := s.latestComputeDestroyOperation(t.Context(), a.ID)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("final release rewrote original destroy")
	}
	read, err := p.ReadComputeDestroyStatus(t.Context(), a)
	if err != nil || read.DestroyState != "" {
		t.Fatalf("terminal read retained retry state: %+v %v", read, err)
	}
}

func TestFinalReleaseReplaysLostResponseWithoutAnotherMutation(t *testing.T) {
	testFinalReleaseRestart(t, NewMemoryOperationStore(), func(s OperationStore) OperationStore { return s })
}
func TestPostgresFinalReleaseFenceSurvivesReopen(t *testing.T) {
	u := fabricTestDatabaseURL(t)
	reopen := func(s OperationStore) OperationStore {
		if s != nil {
			if err := s.(*PostgresOperationStore).client.Close(); err != nil {
				t.Fatal(err)
			}
		}
		next, err := newTestPostgresOperationStore(u)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = next.client.Close() })
		return next
	}
	testFinalReleaseRestart(t, reopen(nil), reopen)
}
func TestFinalReleaseRejectsForeignOriginalOperation(t *testing.T) {
	for _, field := range []string{"account", "workspace", "destroy", "confirmation"} {
		t.Run(field, func(t *testing.T) {
			store := NewMemoryOperationStore()
			a, input := finalReleaseFixture(t, store)
			calls := 0
			absent := false
			p := finalReleaseProvider(t, &absent, &calls)
			switch field {
			case "account":
				input.AccountID = "other"
			case "workspace":
				input.WorkspaceID = "other"
			case "destroy":
				input.DestroyOperationID = "other"
			case "confirmation":
				input.Confirmation = ""
			}
			if _, err := NewServiceWithOperationStore(p, store).ReleaseRecycledComputeAllocation(t.Context(), a.ID, input); err == nil || calls != 0 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestFinalReleaseStartedFenceNeverRedispatches(t *testing.T) {
	store := NewMemoryOperationStore()
	a, input := finalReleaseFixture(t, store)
	calls, absent := 0, false
	s := NewServiceWithOperationStore(finalReleaseProvider(t, &absent, &calls), store)
	op := newOperation(computeFinalReleaseAction, "compute_final_release", a.ID, a.AccountID, a.WorkspaceID, a.ID, hashInput(input), time.Now().UTC())
	if err := s.recordOperation(t.Context(), op, "started", nil, nil); err != nil {
		t.Fatal(err)
	}
	reopened := NewServiceWithOperationStore(finalReleaseProvider(t, &absent, &calls), store)
	got, err := reopened.ReleaseRecycledComputeAllocation(t.Context(), a.ID, input)
	if err != nil || !got.DispatchAttempted || got.AbsenceConfirmed || calls != 0 {
		t.Fatalf("got=%+v err=%v calls=%d", got, err, calls)
	}
}
func TestFinalReleaseUsesOriginalLaunchOwnershipForMissingTags(t *testing.T) {
	for _, currentOwner := range []bool{false, true} {
		t.Run(map[bool]string{false: "original", true: "current"}[currentOwner], func(t *testing.T) {
			store, _, a, owner := retainedTencentComputeTagsFixture(t, func(_ *ComputeAllocation, o *MachineOwnership) {
				if currentOwner {
					*o = MachineOwnership{}
				}
			})
			if currentOwner {
				owner = MachineOwnership{ID: "current-owner", ResourceID: a.ID, AccountID: a.AccountID, WorkspaceID: a.WorkspaceID, PackageID: a.PackageID, NodePoolID: a.NodePoolID, MachineID: a.MachineName, InstanceID: a.InstanceID, NodeName: a.NodeName, Status: "claimed", ClaimedAt: time.Now().UTC()}
				if _, ok, err := store.ClaimMachine(t.Context(), owner); err != nil || !ok {
					t.Fatal(err)
				}
				owner.Status = "active"
				if err := store.SaveMachineOwnership(t.Context(), owner); err != nil {
					t.Fatal(err)
				}
			}
			op := newOperation("destroy_compute_allocation", "compute_allocation", a.ID, a.AccountID, a.WorkspaceID, "", hashInput(map[string]string{"id": a.ID}), time.Now().UTC())
			op.ID = "original-destroy"
			op.Status = "started"
			op.CreatedAt = op.StartedAt
			fillOperationResource(&op, a)
			if err := store.Append(t.Context(), op); err != nil {
				t.Fatal(err)
			}
			calls, absent := 0, false
			p := finalReleaseProvider(t, &absent, &calls)
			base := p.provision
			p.provision = func(ctx context.Context, r provisionerRequest) (provisionerResponse, error) {
				if r.Action == computeFinalReleaseAction && r.Tags["opl_operation_id"] != owner.ID {
					t.Fatalf("unproven ownership: %v", r.Tags)
				}
				return base(ctx, r)
			}
			_, err := NewServiceWithOperationStore(p, store).ReleaseRecycledComputeAllocation(t.Context(), a.ID, ComputeFinalReleaseInput{AccountID: a.AccountID, WorkspaceID: a.WorkspaceID, DestroyOperationID: op.OperationID, Confirmation: ComputeFinalReleaseConfirmation})
			if err == nil || calls != 1 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			latest, _, _ := NewServiceWithOperationStore(p, store).latestComputeDestroyOperation(t.Context(), a.ID)
			var unchanged ComputeAllocation
			decodeOperationResource(latest, &unchanged)
			if len(unchanged.CostTags) != 0 {
				t.Fatal("rewrote frozen original")
			}
		})
	}
}
