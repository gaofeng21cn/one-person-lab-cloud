package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"
)

type d5ReplacementProvider struct {
	runtimeImageReplacementTestProvider
	readFailures int
	writeFailure bool
	lostResponse bool
	waiting      bool
	patches      int
}

func (p *d5ReplacementProvider) WorkspaceRuntimeStatus(context.Context, string) (WorkspaceRuntime, error) {
	if p.readFailures > 0 {
		p.readFailures--
		return WorkspaceRuntime{}, context.DeadlineExceeded
	}
	return p.status, nil
}

func (p *d5ReplacementProvider) ReplaceWorkspaceRuntimeImage(ctx context.Context, input WorkspaceRuntimeImageReplacementInput) (WorkspaceRuntime, error) {
	if p.writeFailure {
		p.writeFailure = false
		return WorkspaceRuntime{}, context.DeadlineExceeded
	}
	if p.status.ImageID == input.ReplacementImageDigest {
		return p.status, nil
	}
	p.patches++
	p.status, _ = p.runtimeImageReplacementTestProvider.ReplaceWorkspaceRuntimeImage(ctx, input)
	if p.waiting {
		p.status.Status, p.status.Ready = "unready", false
	}
	if p.lostResponse {
		p.lostResponse = false
		return WorkspaceRuntime{}, context.DeadlineExceeded
	}
	return p.status, nil
}

func d5ReplacementFixture(input WorkspaceRuntimeImageReplacementInput) *d5ReplacementProvider {
	return &d5ReplacementProvider{runtimeImageReplacementTestProvider: runtimeImageReplacementTestProvider{
		status: WorkspaceRuntime{ID: input.RuntimeID, OperationID: input.RuntimeOperationID, WorkspaceID: input.WorkspaceID, ServiceName: input.RuntimeServiceName, ImageID: input.PreviousImageDigest, Status: "running", Ready: true},
	}}
}

func TestD5ImageReplacementResumesOriginalOperationAcrossServiceRestart(t *testing.T) {
	for _, scenario := range []string{"read fails before write", "provider fails before patch", "patch response is lost", "new image is not ready"} {
		t.Run(scenario, func(t *testing.T) {
			input := runtimeImageReplacementTestInput("d5-recover")
			provider := d5ReplacementFixture(input)
			switch scenario {
			case "read fails before write":
				provider.readFailures = 1
			case "provider fails before patch":
				provider.writeFailure = true
			case "patch response is lost":
				provider.lostResponse = true
			case "new image is not ready":
				provider.waiting = true
			}
			store := NewMemoryOperationStore()
			service := runtimeTestService(provider, store)
			if result, err := service.ReplaceWorkspaceRuntimeImage(context.Background(), input); err == nil || result.Status != "started" {
				t.Fatalf("interrupted result=%#v err=%v", result, err)
			}
			if provider.waiting {
				if result, err := service.ReplaceWorkspaceRuntimeImage(context.Background(), input); !errors.Is(err, ErrRuntimeOperationInProgress) || result.Status != "started" || provider.patches != 1 {
					t.Fatalf("unready target was declared complete or repatched: result=%#v err=%v patches=%d", result, err, provider.patches)
				}
				provider.status.Status, provider.status.Ready = "running", true
			}
			service = runtimeTestService(provider, store)
			result, err := service.ReplaceWorkspaceRuntimeImage(context.Background(), input)
			if err != nil || result.Status != "succeeded" || result.OperationID != input.IdempotencyKey || !replacementRuntimeReadbackMatches(result.Runtime, input) || provider.patches != 1 {
				t.Fatalf("recovery result=%#v err=%v patches=%d", result, err, provider.patches)
			}
			if replay, err := service.ReplaceWorkspaceRuntimeImage(context.Background(), input); err != nil || replay.OperationID != result.OperationID || provider.patches != 1 {
				t.Fatalf("replay=%#v err=%v patches=%d", replay, err, provider.patches)
			}
		})
	}
}

func TestD5ImageReplacementDoesNotResumeStoppedOrDeletingWorkspace(t *testing.T) {
	for _, state := range []string{"suspended", "stopped", "destroyed", "deleting", "suspension requested"} {
		t.Run(state, func(t *testing.T) {
			input := runtimeImageReplacementTestInput("d5-forbidden")
			provider := d5ReplacementFixture(input)
			store := NewMemoryOperationStore()
			service := runtimeTestService(provider, store)
			switch state {
			case "deleting":
				op := newOperation("destroy_workspace_runtime", "workspace_runtime", input.WorkspaceID, input.AccountID, input.WorkspaceID, "delete", "delete", time.Now())
				op.Status = "started"
				if err := store.Append(context.Background(), op); err != nil {
					t.Fatal(err)
				}
			case "suspension requested":
				power := WorkspaceRuntimePowerInput{SchemaVersion: 1, WorkspaceID: input.WorkspaceID, AccountID: input.AccountID, RuntimeID: input.RuntimeID, RuntimeOperationID: input.RuntimeOperationID, DesiredState: "suspended", PaidThrough: time.Now().Add(-time.Hour).Format(time.RFC3339Nano), IdempotencyKey: "suspend"}
				op := newOperation(workspaceRuntimePowerAction, "workspace_runtime_power", input.WorkspaceID, input.AccountID, input.WorkspaceID, power.IdempotencyKey, hashInput(power), time.Now())
				op.Status = "started"
				op.RedactedProviderPayload = map[string]any{"power": power}
				if err := store.Append(context.Background(), op); err != nil {
					t.Fatal(err)
				}
			default:
				provider.status.Status, provider.status.Ready = state, false
			}
			if _, err := service.ReplaceWorkspaceRuntimeImage(context.Background(), input); !errors.Is(err, ErrWorkspaceRuntimeImageReplacementConflict) || provider.patches != 0 {
				t.Fatalf("ineligible Workspace error=%v patches=%d", err, provider.patches)
			}
		})
	}
}

func TestD5ImageReplacementRollbackPreventsOldOperationReapplying(t *testing.T) {
	input := runtimeImageReplacementTestInput("d5-old-operation")
	provider := d5ReplacementFixture(input)
	provider.waiting = true
	store := NewMemoryOperationStore()
	service := runtimeTestService(provider, store)
	if _, err := service.ReplaceWorkspaceRuntimeImage(context.Background(), input); !errors.Is(err, ErrRuntimeOperationInProgress) {
		t.Fatal(err)
	}
	rollback := input
	rollback.IdempotencyKey = "d5-rollback"
	rollback.PreviousImageDigest, rollback.ReplacementImageDigest = input.ReplacementImageDigest, input.PreviousImageDigest
	provider.waiting = false
	if _, err := service.ReplaceWorkspaceRuntimeImage(context.Background(), rollback); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReplaceWorkspaceRuntimeImage(context.Background(), input); !errors.Is(err, ErrWorkspaceRuntimeImageReplacementConflict) || provider.patches != 2 || provider.status.ImageID != input.PreviousImageDigest {
		t.Fatalf("superseded operation changed the rollback: err=%v patches=%d image=%s", err, provider.patches, provider.status.ImageID)
	}
}

func TestD5ImageReplacementNewOperationAlreadyAtTargetDoesNotPatch(t *testing.T) {
	input := runtimeImageReplacementTestInput("d5-first-release")
	provider := d5ReplacementFixture(input)
	service := runtimeTestService(provider, NewMemoryOperationStore())
	if _, err := service.ReplaceWorkspaceRuntimeImage(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	input.IdempotencyKey = "d5-next-release-same-target"
	if result, err := service.ReplaceWorkspaceRuntimeImage(context.Background(), input); err != nil || result.Status != "succeeded" || result.OperationID != input.IdempotencyKey || provider.patches != 1 {
		t.Fatalf("already-at-target result=%#v err=%v patches=%d", result, err, provider.patches)
	}
}

type d5ObservedRuntimeLocks struct {
	ResourceLockStore
	requested chan string
}

func (s d5ObservedRuntimeLocks) WithPoolLock(ctx context.Context, key string, action func(context.Context) error) error {
	s.requested <- key
	return s.ResourceLockStore.WithPoolLock(ctx, key, action)
}

func TestD5ImageReplacementWaitsForConcurrentDeletionAndRechecksOwner(t *testing.T) {
	input := runtimeImageReplacementTestInput("d5-concurrent")
	provider := d5ReplacementFixture(input)
	store := NewMemoryOperationStore()
	service := runtimeTestService(provider, store)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	locked, finishDelete := make(chan struct{}), make(chan struct{})
	deleteResult := make(chan error, 1)
	go func() {
		deleteResult <- store.WithPoolLock(ctx, workspaceRuntimeLockKey(input.WorkspaceID), func(ctx context.Context) error {
			close(locked)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-finishDelete:
			}
			op := newOperation("destroy_workspace_runtime", "workspace_runtime", input.WorkspaceID, input.AccountID, input.WorkspaceID, "delete", "delete", time.Now())
			op.Status = "started"
			return store.Append(ctx, op)
		})
	}()
	<-locked
	requests := make(chan string, 1)
	service.resourceLocks = d5ObservedRuntimeLocks{ResourceLockStore: store, requested: requests}
	replacementResult := make(chan error, 1)
	go func() {
		_, err := service.ReplaceWorkspaceRuntimeImage(ctx, input)
		replacementResult <- err
	}()
	key := <-requests
	close(finishDelete)
	if err := <-deleteResult; err != nil {
		t.Fatal(err)
	}
	if err := <-replacementResult; key != workspaceRuntimeLockKey(input.WorkspaceID) || !errors.Is(err, ErrWorkspaceRuntimeImageReplacementConflict) || provider.patches != 0 {
		t.Fatalf("replacement crossed deletion boundary: lock=%q err=%v patches=%d", key, err, provider.patches)
	}
}

func TestD5PostgresImageReplacementResumesPersistedClaimAcrossRestart(t *testing.T) {
	databaseURL := fabricTestDatabaseURL(t)
	firstStore, err := newTestPostgresOperationStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = firstStore.client.Close() })
	input := runtimeImageReplacementTestInput("d5-persisted-read-failure")
	provider := d5ReplacementFixture(input)
	provider.readFailures = 1
	if _, err := runtimeTestService(provider, firstStore).ReplaceWorkspaceRuntimeImage(context.Background(), input); !errors.Is(err, context.DeadlineExceeded) || provider.patches != 0 {
		t.Fatalf("first read error=%v patches=%d", err, provider.patches)
	}
	secondStore, err := newTestPostgresOperationStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = secondStore.client.Close() })
	result, err := runtimeTestService(provider, secondStore).ReplaceWorkspaceRuntimeImage(context.Background(), input)
	if err != nil || result.Status != "succeeded" || provider.patches != 1 {
		t.Fatalf("restart result=%#v err=%v patches=%d", result, err, provider.patches)
	}
}

func TestD5TencentImageReplacementJournalReadsLostPatchBeforeRetry(t *testing.T) {
	for _, applied := range []bool{false, true} {
		t.Run(fmt.Sprintf("patch_applied_%t", applied), func(t *testing.T) {
			setProtectedResourceEnv(t)
			input := runtimeImageReplacementTestInput("d5-tencent-recovery")
			setWorkspaceImageReleaseCatalogForTest(t, input.ReplacementImageDigest, input.ReplacementImageDigest)
			provider := NewTencentProvider()
			compute := ComputeAllocation{ID: input.ComputeID, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, PackageID: "basic", ServiceName: input.RuntimeServiceName}
			volume := StorageVolume{ID: input.StorageID, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, ProviderData: map[string]string{"pvcName": "opl-storage-alpha-data"}}
			runtimeInput := WorkspaceRuntimeInput{WorkspaceID: input.WorkspaceID, ComputeID: input.ComputeID, VolumeID: input.StorageID, AttachmentID: input.AttachmentID, AttachmentOperationID: "attachment-operation", RuntimeOperationID: input.RuntimeOperationID, ImageID: input.PreviousImageDigest}
			fixture := &tencentRuntimeReadbackFixture{t: t, workspaceID: input.WorkspaceID, storage: volume}
			fixture.applied = workspaceManifest(runtimeInput, input.WorkspaceID, "seed", input.RuntimeID, input.RuntimeServiceName, compute, volume, oplCostTags(input.AccountID, input.WorkspaceID, input.RuntimeID, input.RuntimeOperationID))
			version := 17
			fixture.drift = func(resources map[string]map[string]any) {
				resources["Deployment"]["metadata"].(map[string]any)["resourceVersion"] = fmt.Sprint(version)
			}
			patches := 0
			provider.kubectl = func(ctx context.Context, args []string, stdin []byte) ([]byte, error) {
				if len(args) != 5 || !slices.Equal(args[:4], []string{"patch", "deployment/" + input.RuntimeServiceName, "--type=strategic", "-p"}) {
					return fixture.kubectl(ctx, args, stdin)
				}
				patches++
				var patch, manifest map[string]any
				if err := json.Unmarshal([]byte(args[4]), &patch); err != nil {
					t.Fatal(err)
				}
				if stringValue(nested(patch, "metadata", "resourceVersion")) != fmt.Sprint(version) {
					return nil, errors.New("resource version conflict")
				}
				if applied || patches > 1 {
					if err := json.Unmarshal(fixture.applied, &manifest); err != nil {
						t.Fatal(err)
					}
					for _, item := range manifest["items"].([]any) {
						resource := item.(map[string]any)
						if resource["kind"] == "Deployment" {
							containers := nested(resource, "spec", "template", "spec", "containers").([]any)
							containers[0].(map[string]any)["image"] = input.ReplacementImageDigest
						}
					}
					fixture.applied = mustJSON(manifest)
					version++
				}
				if patches == 1 {
					return nil, context.DeadlineExceeded
				}
				return nil, nil
			}
			store := NewMemoryOperationStore()
			service := runtimeTestService(provider, store)
			op := newOperation(workspaceRuntimeImageReplacementAction, "workspace_runtime", input.WorkspaceID, input.AccountID, input.WorkspaceID, input.IdempotencyKey, hashInput(input), time.Now())
			op.OperationID = input.IdempotencyKey
			ctx := service.providerMutationContextForRuntimeImageReplacement(context.Background(), op, input)
			if _, err := provider.ReplaceWorkspaceRuntimeImage(ctx, input); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("first patch error=%v", err)
			}
			result, err := provider.ReplaceWorkspaceRuntimeImage(ctx, input)
			wantPatches := 2
			if applied {
				wantPatches = 1
			}
			if err != nil || !result.Ready || !replacementRuntimeReadbackMatches(result, input) || patches != wantPatches {
				t.Fatalf("replay runtime=%#v err=%v patches=%d want=%d", result, err, patches, wantPatches)
			}
			operations, err := store.List(context.Background())
			if err != nil || len(operations) != 1 || operations[0].Status != "succeeded" {
				t.Fatalf("provider journal did not converge: operations=%#v err=%v", operations, err)
			}
		})
	}
}
