package coordination_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
)

const (
	prepaidDetachAction       = "detach_prepaid_compute_allocation"
	prepaidRetireAction       = "retire_prepaid_compute_allocation"
	prepaidReleaseAction      = "release_recycled_compute_allocation"
	prepaidOperationStarted   = "started"
	prepaidOperationSucceeded = "succeeded"
	prepaidOperationFailed    = "failed"
)

type prepaidDeletionState int

const (
	prepaidMachinePresent prepaidDeletionState = iota
	prepaidMachineAbsent
	prepaidRecycled
	prepaidAbsent
)

// The embedded Tencent adapter supplies the real private destroy finalizer.
// Only external compute mutation/readback is replaced; cleanup goes through the
// adapter's normal kubectl command into an isolated, exact-argument fixture.
// In particular, these tests never append a successful destroy outcome.
type prepaidDeletionProvider struct {
	*fabric.TencentProvider
	fixture         *deletionProviderFixture
	mu              sync.Mutex
	state           prepaidDeletionState
	original        fabric.ComputeAllocation
	sends           []string
	reads           []prepaidDeletionState
	uncertainAction string
	lostReply       bool
}

func (p *prepaidDeletionProvider) DestroyComputeAllocation(ctx context.Context, a fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
	return p.fixture.DestroyComputeAllocation(ctx, a)
}
func (p *prepaidDeletionProvider) ReadComputeDestroyStatus(ctx context.Context, a fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
	return p.fixture.ReadComputeDestroyStatus(ctx, a)
}
func (p *prepaidDeletionProvider) ReleaseRecycledComputeAllocation(ctx context.Context, a fabric.ComputeAllocation) (string, error) {
	return p.fixture.ReleaseRecycledComputeAllocation(ctx, a)
}
func (p *prepaidDeletionProvider) DetachPrepaidComputeAllocation(_ context.Context, a fabric.ComputeAllocation) (string, error) {
	return p.advance(a, prepaidDetachAction, prepaidMachinePresent, prepaidMachineAbsent)
}
func (p *prepaidDeletionProvider) RetirePrepaidComputeAllocation(_ context.Context, a fabric.ComputeAllocation) (string, error) {
	return p.advance(a, prepaidRetireAction, prepaidMachineAbsent, prepaidRecycled)
}
func (p *prepaidDeletionProvider) advance(a fabric.ComputeAllocation, action string, before, after prepaidDeletionState) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !reflect.DeepEqual(a, p.original) {
		return "", fmt.Errorf("%s changed the frozen original allocation", action)
	}
	if p.state != before {
		return "", fmt.Errorf("%s sent in state %d, want %d", action, p.state, before)
	}
	p.sends = append(p.sends, action)
	if p.uncertainAction != action {
		p.state = after
	}
	if p.lostReply || p.uncertainAction == action {
		return "", errors.New("prepaid fixture lost provider reply")
	}
	return "request-" + action, nil
}
func (p *prepaidDeletionProvider) read(a fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reads = append(p.reads, p.state)
	a = computeFinalReleaseDeletionReadback(a, p.state == prepaidAbsent)
	switch p.state {
	case prepaidMachinePresent:
		present := true
		a.MachinePresent = &present
		a.TKEStatus, a.CVMStatus = "RUNNING", ""
		a.ProviderData["machinePresent"], a.ProviderData["tkeStatus"] = "true", "RUNNING"
		delete(a.ProviderData, "cvmStatus")
		delete(a.ProviderData, "isolatedSource")
	case prepaidMachineAbsent:
		a.CVMStatus = "RUNNING"
		a.ProviderData["cvmStatus"], a.ProviderData["isolatedSource"] = "RUNNING", "NOTISOLATED"
	}
	return a, nil
}
func (p *prepaidDeletionProvider) snapshot() (prepaidDeletionState, []string, []prepaidDeletionState) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.state, append([]string(nil), p.sends...), append([]prepaidDeletionState(nil), p.reads...)
}

// A temporary executable is the external cleanup boundary, not a replacement
// finalizer. Before the gate opens it refuses cleanup; after it opens the real
// Service releases ownership and records the original destroy as succeeded.
func newPrepaidDeletionSystem(t *testing.T) (*prepaidDeletionProvider, *fabric.MemoryOperationStore, fabric.FabricOperation, coordination.ResourceDeletionIntent, string) {
	t.Helper()
	dir := t.TempDir()
	gate := filepath.Join(dir, "allow-cleanup")
	script := `#!/bin/sh
set -eu
[ "$#" -eq 8 ]
[ "$1" = "--namespace" ] && [ "$2" = "prepaid-deletion-test" ]
[ "$3" = "delete" ]
[ "$4" = "deployment/opl-compute-durable-delete" ]
[ "$5" = "service/opl-compute-durable-delete" ]
[ "$6" = "secret/opl-compute-durable-delete-env" ]
[ "$7" = "--ignore-not-found=true" ]
[ "$8" = "--wait=true" ]
`
	script += `[ -f "$OPL_PREPAID_TEST_CLEANUP_GATE" ] || { echo cleanup_not_confirmed >&2; exit 1; }
printf 'cleanup-confirmed\n' >> "$OPL_PREPAID_TEST_CLEANUP_LOG"
`
	if err := os.WriteFile(filepath.Join(dir, "kubectl"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"PATH":                                         dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TENCENT_DEPLOY_KUBECONFIG_REF":                "",
		"OPL_PREPAID_TEST_CLEANUP_GATE":                gate,
		"OPL_PREPAID_TEST_CLEANUP_LOG":                 filepath.Join(dir, "cleanup-log"),
		"OPL_WORKSPACE_DOMAIN":                         "prepaid.invalid",
		"OPL_WORKSPACE_IMAGE":                          "registry.invalid/workspace@sha256:" + strings.Repeat("a", 64),
		"OPL_WORKSPACE_IMAGE_RELEASES_JSON":            "",
		"OPL_K8S_NAMESPACE":                            "prepaid-deletion-test",
		"OPL_TENCENT_PROVISIONER_BIN":                  "/unused-prepaid-test-provisioner",
		"OPL_SYSTEM_COMPUTE_NODE_POOL_ID":              "np-system-test",
		"OPL_SYSTEM_COMPUTE_MACHINE_ID":                "machine-system-test",
		"OPL_SYSTEM_COMPUTE_NODE_NAME":                 "node-system-test",
		"OPL_SYSTEM_COMPUTE_MACHINE_TYPE":              "NativeCVM",
		"OPL_SYSTEM_COMPUTE_CVM_ID":                    "ins-system-test",
		"OPL_FABRIC_TENCENT_TKE_PROVIDER_PROFILE_JSON": `{"packages":[{"id":"basic","available":true,"nodePoolId":"np-basic"}]}`,
	} {
		t.Setenv(key, value)
	}
	fixture := &deletionProviderFixture{}
	system, allocation, original := newComputeFinalReleaseDeletionSystem(t, fixture)
	provider := &prepaidDeletionProvider{TencentProvider: fabric.NewTencentProvider(), fixture: fixture, original: allocation}
	fixture.computeReadback = provider.read
	fixture.computeDestroy = func(a fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
		return a, errors.New("generic destroy must not be sent again")
	}
	fixture.computeRelease = func(a fabric.ComputeAllocation) (string, error) {
		return provider.advance(a, prepaidReleaseAction, prepaidRecycled, prepaidAbsent)
	}
	ownership := fabric.MachineOwnership{ID: "owner-prepaid-deletion", ResourceID: allocation.ID, AccountID: allocation.AccountID,
		WorkspaceID: allocation.WorkspaceID, PackageID: allocation.PackageID, NodePoolID: allocation.NodePoolID,
		MachineID: allocation.MachineName, InstanceID: allocation.InstanceID, NodeName: allocation.NodeName, Status: "active"}
	if _, claimed, err := system.store.ClaimMachine(t.Context(), ownership); err != nil || !claimed {
		t.Fatalf("claim ownership: claimed=%v err=%v", claimed, err)
	}
	intent := deletionIntent(allocation.WorkspaceID, allocation.ID, "")
	intent.AccountID = allocation.AccountID
	return provider, system.store, original, intent, gate
}

func prepaidDeletionDispatcher(provider *prepaidDeletionProvider, store *fabric.MemoryOperationStore) (*fabric.Service, coordination.ResourceDispatcher) {
	service := fabric.NewServiceWithOperationStore(provider, store)
	return service, coordination.NewLocalDispatcher(service, fabric.NewLocalDockerProvider())
}

func assertPrepaidSends(t *testing.T, p *prepaidDeletionProvider, expected ...string) {
	t.Helper()
	_, sends, _ := p.snapshot()
	if !reflect.DeepEqual(sends, expected) {
		t.Fatalf("provider sends=%v, want %v", sends, expected)
	}
	_, _, generic, _ := p.fixture.calls()
	if generic != 0 {
		t.Fatalf("generic destroy resent %d times", generic)
	}
}

func assertPrepaidJournal(t *testing.T, store *fabric.MemoryOperationStore, original fabric.FabricOperation, action, status string) {
	t.Helper()
	operations, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	claims := 0
	found := false
	for _, op := range operations {
		if op.Action != action {
			continue
		}
		if op.ResourceID != original.ResourceID || op.AccountID != original.AccountID || op.WorkspaceID != original.WorkspaceID ||
			op.CallerService != "workspace" || op.RedactedProviderPayload["originalDestroyOperationId"] != original.OperationID {
			t.Fatalf("%s lost original destroy binding: %+v", action, op)
		}
		if op.Status == prepaidOperationStarted {
			claims++
		}
		if op.Status == status {
			found = true
		}
	}
	if claims != 1 || !found {
		t.Fatalf("%s claims=%d status %q found=%v", action, claims, status, found)
	}
}

func assertPrepaidPending(t *testing.T, fact coordination.ResourceDeletionFact, err error) {
	t.Helper()
	if err != nil || fact.State != coordination.DeletionStatePending || fact.EvidenceRef == "" || fact.ObservedAt.IsZero() {
		t.Fatalf("pending deletion fact=%+v err=%v", fact, err)
	}
}

func TestDispatcherPrepaidDeletionStagesRequireFreshAbsenceAndOriginalFinalization(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost_reply_%t", lost), func(t *testing.T) {
			provider, store, original, intent, gate := newPrepaidDeletionSystem(t)
			provider.lostReply = lost
			service, dispatcher := prepaidDeletionDispatcher(provider, store)
			fact, err := dispatcher.DeleteResource(t.Context(), intent, coordination.DeletionKindCompute)
			assertPrepaidPending(t, fact, err)
			if fact.ProviderStatus != "external_deleted" || fact.DestroyState != "" {
				t.Fatalf("post-release fact=%+v", fact)
			}
			assertPrepaidSends(t, provider, prepaidDetachAction, prepaidRetireAction, prepaidReleaseAction)
			state, _, reads := provider.snapshot()
			if state != prepaidAbsent {
				t.Fatalf("provider state=%d", state)
			}
			for _, expected := range []prepaidDeletionState{prepaidMachinePresent, prepaidMachineAbsent, prepaidRecycled, prepaidAbsent} {
				if !containsPrepaidState(reads, expected) {
					t.Fatalf("state %d never read back: %v", expected, reads)
				}
			}
			absentReads := 0
			for _, state := range reads {
				if state == prepaidAbsent {
					absentReads++
				}
			}
			if absentReads < 2 {
				t.Fatalf("only %d reads after release; dispatcher needs fresh absence", absentReads)
			}
			waitForComputeFinalReleaseDestroyRecovery(t, store, intent.ComputeID)
			if done, err := service.ComputeDeletionFinalized(t.Context(), intent.ComputeID); err != nil || done {
				t.Fatalf("absence bypassed cleanup: done=%v err=%v", done, err)
			}
			for _, action := range []string{prepaidDetachAction, prepaidRetireAction, prepaidReleaseAction} {
				assertPrepaidJournal(t, store, original, action, prepaidOperationSucceeded)
			}
			// Reconstruct all process-local state while cleanup still refuses. The
			// provider is already absent, but the original owner is not yet finalized.
			for range 2 {
				service, dispatcher = prepaidDeletionDispatcher(provider, store)
				next, err := dispatcher.DeleteResource(t.Context(), intent, coordination.DeletionKindCompute)
				assertPrepaidPending(t, next, err)
				if next.EvidenceRef == fact.EvidenceRef {
					t.Fatal("restart reused a stale readback")
				}
				waitForComputeFinalReleaseDestroyRecovery(t, store, intent.ComputeID)
				fact = next
				assertPrepaidSends(t, provider, prepaidDetachAction, prepaidRetireAction, prepaidReleaseAction)
			}
			if err := os.WriteFile(gate, []byte("allow exact cleanup"), 0600); err != nil {
				t.Fatal(err)
			}
			service, dispatcher = prepaidDeletionDispatcher(provider, store)
			if _, err := dispatcher.DeleteResource(t.Context(), intent, coordination.DeletionKindCompute); err != nil {
				t.Fatal(err)
			}
			waitForDeletionCondition(t, 5*time.Second, "original compute destroy finalized", func() bool {
				done, err := service.ComputeDeletionFinalized(t.Context(), intent.ComputeID)
				if err != nil {
					t.Error(err)
				}
				return done && err == nil
			})
			final, err := dispatcher.DeleteResource(t.Context(), intent, coordination.DeletionKindCompute)
			if err != nil || final.State != coordination.DeletionStateAbsent || final.ProviderStatus != "external_deleted" || final.DestroyState != "" ||
				final.ProviderReference != provider.original.InstanceID || final.EvidenceRef == "" || final.EvidenceRef == fact.EvidenceRef || final.ObservedAt.IsZero() {
				t.Fatalf("finalized deletion fact=%+v err=%v", final, err)
			}
			ownership, err := store.MachineOwnership(t.Context(), intent.ComputeID)
			if err != nil || ownership.Status != "released" || ownership.ReleasedAt == nil {
				t.Fatalf("ownership=%+v err=%v", ownership, err)
			}
			cleanup, err := os.ReadFile(filepath.Join(filepath.Dir(gate), "cleanup-log"))
			if err != nil || string(cleanup) != "cleanup-confirmed\n" {
				t.Fatalf("cleanup=%q err=%v", cleanup, err)
			}
			assertPrepaidOriginalFinalized(t, store, original)
			assertPrepaidSends(t, provider, prepaidDetachAction, prepaidRetireAction, prepaidReleaseAction)
		})
	}
}

func containsPrepaidState(states []prepaidDeletionState, expected prepaidDeletionState) bool {
	for _, state := range states {
		if state == expected {
			return true
		}
	}
	return false
}

func assertPrepaidOriginalFinalized(t *testing.T, store *fabric.MemoryOperationStore, original fabric.FabricOperation) {
	t.Helper()
	operations, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, op := range operations {
		if op.Action != original.Action || op.ResourceID != original.ResourceID {
			continue
		}
		if op.OperationID != original.OperationID || op.AccountID != original.AccountID || op.WorkspaceID != original.WorkspaceID {
			t.Fatalf("original destroy identity changed: %+v", op)
		}
		if op.Status == prepaidOperationSucceeded {
			a, ok := op.RedactedProviderPayload["resource"].(fabric.ComputeAllocation)
			if !ok || !fabric.ComputeDeletionAbsent(a) {
				t.Fatalf("original finalized without absence: %+v", op)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("real Service did not finalize the original destroy")
	}
}

func TestDispatcherPrepaidDeletionRestartNeverResendsUnconfirmedStage(t *testing.T) {
	for _, action := range []string{prepaidDetachAction, prepaidRetireAction, prepaidReleaseAction} {
		t.Run(action, func(t *testing.T) {
			provider, store, original, intent, _ := newPrepaidDeletionSystem(t)
			provider.uncertainAction = action
			_, dispatcher := prepaidDeletionDispatcher(provider, store)
			fact, err := dispatcher.DeleteResource(t.Context(), intent, coordination.DeletionKindCompute)
			if err == nil || fact.State == coordination.DeletionStateAbsent {
				t.Fatalf("unconfirmed mutation fact=%+v err=%v", fact, err)
			}
			waitForComputeFinalReleaseDestroyRecovery(t, store, intent.ComputeID)
			_, sends, _ := provider.snapshot()
			assertPrepaidJournal(t, store, original, action, prepaidOperationFailed)
			for range 3 {
				_, dispatcher = prepaidDeletionDispatcher(provider, store)
				fact, err := dispatcher.DeleteResource(t.Context(), intent, coordination.DeletionKindCompute)
				assertPrepaidPending(t, fact, err)
				waitForComputeFinalReleaseDestroyRecovery(t, store, intent.ComputeID)
				assertPrepaidSends(t, provider, sends...)
			}
			// The delayed provider effect may become visible after another process has
			// reconstructed the Service. Only the next distinct stage may then send.
			provider.mu.Lock()
			switch action {
			case prepaidDetachAction:
				provider.state = prepaidMachineAbsent
			case prepaidRetireAction:
				provider.state = prepaidRecycled
			case prepaidReleaseAction:
				provider.state = prepaidAbsent
			}
			provider.mu.Unlock()
			_, dispatcher = prepaidDeletionDispatcher(provider, store)
			fact, err = dispatcher.DeleteResource(t.Context(), intent, coordination.DeletionKindCompute)
			assertPrepaidPending(t, fact, err)
			waitForComputeFinalReleaseDestroyRecovery(t, store, intent.ComputeID)
			assertPrepaidSends(t, provider, prepaidDetachAction, prepaidRetireAction, prepaidReleaseAction)
		})
	}
}
