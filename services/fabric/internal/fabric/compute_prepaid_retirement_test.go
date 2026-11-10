package fabric

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func prepaidRetirementFixture(t *testing.T, store OperationStore) ComputeAllocation {
	t.Helper()
	a := computeDestroyPhaseResource()
	a.Status = "destroying"
	a.ProviderData[tencentComputeDestroyPhaseKey] = tencentComputeDestroyPhaseAttempted
	a.ProviderData["deleteMethod"] = "DeleteClusterMachines"
	a.ProviderData["scaleDown"] = "true"
	a.ProviderData["deleteMode"] = "terminate"
	a.ProviderData["describeNodePoolRequestId"] = "old-delete-pool"
	seedComputeDestroyPhaseResource(t, store, a)
	appendStartedComputeDestroy(t, store, a)
	return a
}

type prepaidRetirementLive struct {
	state          atomic.Int32 // 0: Machine present; 1: active CVM; 2: recycled; 3: absent.
	detach, retire atomic.Int32
	noEffect       string
	lostReply      bool
}

func (v *prepaidRetirementLive) provider(t *testing.T, a ComputeAllocation) *TencentProvider {
	t.Helper()
	p := NewTencentProvider()
	p.provision = func(_ context.Context, r provisionerRequest) (provisionerResponse, error) {
		switch r.Action {
		case "read_compute_destroy_status":
			state := v.state.Load()
			cvm, tke := "", "RUNNING"
			if state > 0 {
				tke = "NOT_FOUND"
				cvm = "RUNNING"
			}
			if state == 2 {
				cvm = "SHUTDOWN"
			}
			if state == 3 {
				cvm = "NOT_FOUND"
			}
			out := computeDestroyStatusResponse(r, state == 0, tke, cvm)
			out.ProviderData["clusterId"], out.ProviderData["region"] = r.Pool.ClusterID, r.Region
			if state == 1 {
				out.ProviderData["isolatedSource"] = "NOTISOLATED"
			}
			if state == 2 {
				out.ProviderData["isolatedSource"] = "MANMADE"
			}
			return out, nil
		case computePrepaidDetachAction, computePrepaidRetireAction:
			if !reflect.DeepEqual(r.Tags, a.CostTags) || r.Allocation.InstanceID != a.InstanceID || r.Zone != a.Zone || r.Pool.InstanceType != a.InstanceType {
				t.Error("lost exact retirement identity")
			}
			if r.Action == computePrepaidDetachAction {
				v.detach.Add(1)
				if v.noEffect != r.Action {
					v.state.Store(1)
				}
			} else {
				v.retire.Add(1)
				if v.noEffect != r.Action {
					v.state.Store(2)
				}
			}
			if v.lostReply {
				return provisionerResponse{InstanceID: a.InstanceID, MutationCount: 1}, errors.New("lost provider reply")
			}
			return provisionerResponse{OK: true, InstanceID: a.InstanceID, ProviderRequestID: "retirement-request", MutationCount: 1}, nil
		default:
			t.Errorf("unexpected provider action: %s", r.Action)
			return provisionerResponse{}, errors.New("unexpected provider action")
		}
	}
	return p
}
func TestPrepaidRetirementContinuesOriginalDestroyThroughTwoDistinctFences(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(map[bool]string{false: "accepted", true: "lost_reply_with_progress"}[lost], func(t *testing.T) {
			store := NewMemoryOperationStore()
			a := prepaidRetirementFixture(t, store)
			live := &prepaidRetirementLive{lostReply: lost}
			p := live.provider(t, a)
			s := NewServiceWithOperationStore(p, store)
			before, _, _ := s.latestComputeDestroyOperation(t.Context(), a.ID)
			result, err := s.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID)
			if err != nil || result.AbsenceConfirmed || live.detach.Load() != 1 || live.retire.Load() != 1 || live.state.Load() != 2 {
				t.Fatalf("result=%+v err=%v detach=%d retire=%d", result, err, live.detach.Load(), live.retire.Load())
			}
			after, _, _ := s.latestComputeDestroyOperation(t.Context(), a.ID)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("retirement rewrote original destroy")
			}
			observed, err := s.ReadComputeDestroyStatus(t.Context(), a.ID)
			if err != nil || !ComputeFinalReleaseEligible(observed) || ComputeDeletionAbsent(observed) {
				t.Fatalf("recycled state=%+v err=%v", observed, err)
			}
			restarted := NewServiceWithOperationStore(p, store)
			if _, err := restarted.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID); err != nil || live.detach.Load() != 1 || live.retire.Load() != 1 {
				t.Fatalf("restart resend: %v", err)
			}
			for _, action := range []string{computePrepaidDetachAction, computePrepaidRetireAction} {
				fence, e := store.Get(t.Context(), stableID("fop-compute-prepaid-dispatch", a.ID, action))
				if e != nil || fence.Action != action || fence.RedactedProviderPayload["originalDestroyOperationId"] != before.OperationID {
					t.Fatalf("missing exact durable fence: %+v %v", fence, e)
				}
			}
		})
	}
}
func TestPrepaidRetirementUnknownReplyNeverRedispatchesAfterRestart(t *testing.T) {
	for _, action := range []string{computePrepaidDetachAction, computePrepaidRetireAction} {
		t.Run(action, func(t *testing.T) {
			store := NewMemoryOperationStore()
			a := prepaidRetirementFixture(t, store)
			live := &prepaidRetirementLive{noEffect: action, lostReply: true}
			p := live.provider(t, a)
			s := NewServiceWithOperationStore(p, store)
			if _, err := s.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID); err == nil {
				t.Fatal("uncertain effect must remain unresolved")
			}
			detach, retire := live.detach.Load(), live.retire.Load()
			for range 3 {
				s = NewServiceWithOperationStore(p, store)
				result, err := s.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID)
				if err != nil || result.AbsenceConfirmed || live.detach.Load() != detach || live.retire.Load() != retire {
					t.Fatalf("uncertain action resent result=%+v err=%v", result, err)
				}
			}
		})
	}
}

// A provider can complete after the caller loses the reply. The next owner
// resumes from fresh facts without consuming a second fence for that action.
func TestPrepaidRetirementDelayedEffectResumesSameOriginalDestroy(t *testing.T) {
	for _, action := range []string{computePrepaidDetachAction, computePrepaidRetireAction} {
		t.Run(action, func(t *testing.T) {
			store := NewMemoryOperationStore()
			a := prepaidRetirementFixture(t, store)
			live := &prepaidRetirementLive{noEffect: action, lostReply: true}
			p := live.provider(t, a)
			s := NewServiceWithOperationStore(p, store)
			original, _, _ := s.latestComputeDestroyOperation(t.Context(), a.ID)
			if _, err := s.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID); err == nil {
				t.Fatal("missing asynchronous uncertainty")
			}
			if action == computePrepaidDetachAction {
				live.state.Store(1)
			} else {
				live.state.Store(2)
			}
			s = NewServiceWithOperationStore(p, store)
			result, err := s.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID)
			if err != nil || result.AbsenceConfirmed || live.detach.Load() != 1 || live.retire.Load() != 1 || live.state.Load() != 2 {
				t.Fatalf("resume=%+v err=%v detach=%d retire=%d", result, err, live.detach.Load(), live.retire.Load())
			}
			retained, _, _ := s.latestComputeDestroyOperation(t.Context(), a.ID)
			if !reflect.DeepEqual(original, retained) {
				t.Fatal("delayed completion rewrote original destroy")
			}
		})
	}
}

func TestPrepaidRetirementUnknownStateAndIdentityDriftDoNotClaim(t *testing.T) {
	for _, drift := range []string{"foreign_instance", "unknown_machine_state", "unknown_cvm_state", "isolation_unknown"} {
		t.Run(drift, func(t *testing.T) {
			store := NewMemoryOperationStore()
			a := prepaidRetirementFixture(t, store)
			live := &prepaidRetirementLive{}
			if drift == "unknown_cvm_state" || drift == "isolation_unknown" {
				live.state.Store(1)
			}
			p := live.provider(t, a)
			base := p.provision
			p.provision = func(ctx context.Context, r provisionerRequest) (provisionerResponse, error) {
				out, err := base(ctx, r)
				if r.Action == "read_compute_destroy_status" {
					switch drift {
					case "foreign_instance":
						out.InstanceID = "ins-foreign"
					case "unknown_machine_state":
						out.TKEStatus = "UNKNOWN"
					case "unknown_cvm_state":
						out.CVMStatus = "UNKNOWN"
					case "isolation_unknown":
						delete(out.ProviderData, "isolatedSource")
					}
				}
				return out, err
			}
			_, _ = NewServiceWithOperationStore(p, store).RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID)
			if live.detach.Load() != 0 || live.retire.Load() != 0 {
				t.Fatal("unknown or foreign facts sent a mutation")
			}
			for _, action := range []string{computePrepaidDetachAction, computePrepaidRetireAction} {
				if _, err := store.Get(t.Context(), stableID("fop-compute-prepaid-dispatch", a.ID, action)); err != ErrOperationNotFound {
					t.Fatalf("refused facts consumed a fence: %v", err)
				}
			}
		})
	}
}

func TestPrepaidRetirementRejectsWrongOwnerAndRequiresFreshReadBeforeClaim(t *testing.T) {
	store := NewMemoryOperationStore()
	a := prepaidRetirementFixture(t, store)
	live := &prepaidRetirementLive{}
	p := live.provider(t, a)
	s := NewServiceWithOperationStore(p, store)
	if _, err := s.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, "other-account", a.WorkspaceID); err == nil {
		t.Fatal("foreign owner admitted")
	}
	original := p.provision
	p.provision = func(ctx context.Context, r provisionerRequest) (provisionerResponse, error) {
		if r.Action == "read_compute_destroy_status" {
			return provisionerResponse{}, errors.New("unreadable provider")
		}
		return original(ctx, r)
	}
	if _, err := s.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID); err == nil {
		t.Fatal("unreadable identity admitted")
	}
	if live.detach.Load() != 0 || live.retire.Load() != 0 {
		t.Fatal("refusal sent mutation")
	}
	if _, err := store.Get(t.Context(), stableID("fop-compute-prepaid-dispatch", a.ID, computePrepaidDetachAction)); err != ErrOperationNotFound {
		t.Fatalf("failed read consumed a send fence: %v", err)
	}
}
func TestPrepaidRetirementAtomicFencesSurviveLostLockExclusion(t *testing.T) {
	for _, state := range []int32{0, 1} {
		t.Run(map[int32]string{0: "detach", 1: "retire"}[state], func(t *testing.T) {
			store := NewMemoryOperationStore()
			testPrepaidRetirementAtomicFences(t, store, store, state)
		})
	}
}

func TestPostgresPrepaidRetirementAtomicFencesSurviveLostLockExclusion(t *testing.T) {
	for _, state := range []int32{0, 1} {
		t.Run(map[int32]string{0: "detach", 1: "retire"}[state], func(t *testing.T) {
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
			testPrepaidRetirementAtomicFences(t, first, second, state)
		})
	}
}

func testPrepaidRetirementAtomicFences(t *testing.T, store, second OperationStore, state int32) {
	t.Helper()
	a := prepaidRetirementFixture(t, store)
	action := computePrepaidDetachAction
	if state == 1 {
		action = computePrepaidRetireAction
	}
	live := &prepaidRetirementLive{noEffect: action}
	live.state.Store(state)
	p := live.provider(t, a)
	provision := p.provision
	var ready sync.WaitGroup
	ready.Add(2)
	var reads atomic.Int32
	p.provision = func(ctx context.Context, r provisionerRequest) (provisionerResponse, error) {
		if r.Action == "read_compute_destroy_status" && reads.Add(1) <= 2 {
			ready.Done()
			ready.Wait()
		}
		return provision(ctx, r)
	}
	var finished sync.WaitGroup
	for _, backend := range []OperationStore{store, second} {
		finished.Add(1)
		go func() {
			defer finished.Done()
			s := NewServiceWithOperationStore(p, computeFinalReleaseUnlockedStore{backend})
			_, _ = s.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID)
		}()
	}
	finished.Wait()
	if state == 0 && (live.detach.Load() != 1 || live.retire.Load() != 0) ||
		state == 1 && (live.detach.Load() != 0 || live.retire.Load() != 1) {
		t.Fatalf("atomic fence sends detach=%d retire=%d", live.detach.Load(), live.retire.Load())
	}
}

func TestPrepaidRetirementUsesProvenLaunchTagsWithoutRewritingDestroy(t *testing.T) {
	for _, foreignCurrent := range []bool{false, true} {
		t.Run(map[bool]string{false: "canonical_launch", true: "foreign_current_identity"}[foreignCurrent], func(t *testing.T) {
			store, _, a, _ := retainedTencentComputeTagsFixture(t, func(a *ComputeAllocation, _ *MachineOwnership) {
				a.RenewFlag = "NOTIFY_AND_MANUAL_RENEW"
			})
			op := newOperation("destroy_compute_allocation", "compute_allocation", a.ID, a.AccountID, a.WorkspaceID, "", hashInput(map[string]string{"id": a.ID}), time.Now().UTC())
			op.ID, op.Status, op.CreatedAt = "prepaid-original-untagged-destroy", "started", op.StartedAt
			fillOperationResource(&op, a)
			if err := store.Append(t.Context(), op); err != nil {
				t.Fatal(err)
			}
			live := &prepaidRetirementLive{}
			seed := NewServiceWithOperationStore(NewTencentProvider(), store)
			proven, found := seed.GetComputeAllocation(t.Context(), a.ID)
			if !found || len(proven.CostTags) == 0 {
				t.Fatal("fixture did not recover canonical Launch tags")
			}
			p := live.provider(t, proven)
			s := NewServiceWithOperationStore(p, store)
			if foreignCurrent {
				foreign := cloneComputeAllocation(proven)
				foreign.NodeName = "foreign-node"
				s.computes[a.ID] = foreign
			}
			_, err := s.RetirePrepaidComputeAllocationForDeletion(t.Context(), a.ID, a.AccountID, a.WorkspaceID)
			if foreignCurrent {
				if err == nil || live.detach.Load() != 0 || live.retire.Load() != 0 {
					t.Fatalf("foreign identity admitted err=%v detach=%d retire=%d", err, live.detach.Load(), live.retire.Load())
				}
			} else if err != nil || live.detach.Load() != 1 || live.retire.Load() != 1 {
				t.Fatalf("canonical tags refused err=%v detach=%d retire=%d", err, live.detach.Load(), live.retire.Load())
			}
			retained, _, err := s.latestComputeDestroyOperation(t.Context(), a.ID)
			if err != nil || !reflect.DeepEqual(op, retained) {
				t.Fatalf("frozen original destroy changed: %v", err)
			}
		})
	}
}
