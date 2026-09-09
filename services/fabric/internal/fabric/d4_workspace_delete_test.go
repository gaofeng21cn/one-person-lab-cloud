package fabric

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"sync"
	"testing"
	"time"
)

// Only the Kubernetes transport is substituted; adapters, Service, and stores
// execute their ordinary production deletion and readback paths.
type d4KubernetesFixture struct {
	mu              sync.Mutex
	items           map[string]map[string]any
	deletes         []string
	failAfterDelete bool
	failRead        bool
}

func newD4KubernetesFixture(items ...map[string]any) *d4KubernetesFixture {
	f := &d4KubernetesFixture{items: map[string]map[string]any{}}
	for _, item := range items {
		f.items[strings.ToLower(stringValue(item["kind"]))+"/"+stringValue(nested(item, "metadata", "name"))] = item
	}
	return f
}

func d4KubernetesKind(kind string) string {
	switch kind {
	case "pv":
		return "persistentvolume"
	case "pvc":
		return "persistentvolumeclaim"
	}
	return kind
}

func (f *d4KubernetesFixture) run(_ context.Context, args []string, _ []byte) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if args[0] != "get" && args[0] != "delete" {
		return nil, fmt.Errorf("unexpected test IO %v", args)
	}
	if args[0] == "get" && f.failRead {
		return nil, context.DeadlineExceeded
	}
	if args[0] == "delete" {
		for _, arg := range args[1:] {
			if strings.HasPrefix(arg, "--") {
				break
			}
			parts := strings.SplitN(arg, "/", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("unexpected delete %v", args)
			}
			key := d4KubernetesKind(parts[0]) + "/" + parts[1]
			if _, exists := f.items[key]; exists {
				delete(f.items, key)
				f.deletes = append(f.deletes, key)
			}
			if f.failAfterDelete {
				f.failAfterDelete = false
				return nil, context.DeadlineExceeded
			}
		}
		return nil, nil
	}
	if strings.Contains(args[1], "/") {
		items := []any{}
		for _, arg := range args[1:] {
			if strings.HasPrefix(arg, "--") {
				break
			}
			parts := strings.SplitN(arg, "/", 2)
			if len(parts) != 2 {
				break
			}
			if item, exists := f.items[d4KubernetesKind(parts[0])+"/"+parts[1]]; exists {
				items = append(items, item)
			}
		}
		if strings.HasPrefix(args[1], "secret/") {
			if len(items) == 0 {
				return nil, nil
			}
			return json.Marshal(items[0])
		}
		return json.Marshal(map[string]any{"kind": "List", "items": items})
	}
	selector := ""
	for i, arg := range args {
		if arg == "-l" && i+1 < len(args) {
			selector = strings.TrimPrefix(args[i+1], "oplcloud.cn/workspace-id=")
		}
	}
	kinds := strings.Split(args[1], ",")
	items := []any{}
	for key, item := range f.items {
		for _, kind := range kinds {
			if strings.HasPrefix(key, d4KubernetesKind(kind)+"/") && (selector == "" || stringValue(nested(item, "metadata", "labels", "oplcloud.cn/workspace-id")) == selector) {
				items = append(items, item)
			}
		}
	}
	return json.Marshal(map[string]any{"kind": "List", "items": items})
}

func d4GatewaySecret(accountID, workspaceID string) map[string]any {
	key := "isolated-d4-key"
	sum := sha256.Sum256([]byte(key))
	digest := hex.EncodeToString(sum[:])
	return map[string]any{"kind": "Secret", "type": "Opaque", "metadata": map[string]any{"name": gatewaySecretName(workspaceID), "labels": map[string]any{"app.kubernetes.io/name": "opl-gateway-secret"}, "annotations": map[string]any{"oplcloud.cn/account-id": accountID, "oplcloud.cn/workspace-id": workspaceID, "oplcloud.cn/workspace-api-key-id": "17", "oplcloud.cn/secret-version": digest[:16], "oplcloud.cn/secret-fingerprint": "sha256:" + digest}}, "data": map[string]any{"opl_gateway_api_key": base64.StdEncoding.EncodeToString([]byte(key))}}
}

func TestD4TencentRuntimeDeleteRemovesExactStandaloneSecretAndRejectsForeignOwner(t *testing.T) {
	ctx := context.Background()
	for _, foreign := range []bool{false, true} {
		t.Run(fmt.Sprint(foreign), func(t *testing.T) {
			secret := d4GatewaySecret("acct-alpha", "ws-alpha")
			if foreign {
				secret = d4GatewaySecret("acct-other", "ws-alpha")
			}
			fixture := newD4KubernetesFixture(secret, d4GatewaySecret("acct-sibling", "ws-sibling"))
			provider := NewTencentProvider()
			provider.kubectl = fixture.run
			ownerCtx := context.WithValue(ctx, workspaceRuntimeOwnerContextKey{}, "acct-alpha")
			observation, err := provider.ObserveWorkspaceRuntimeDelete(ownerCtx, "ws-alpha")
			if foreign {
				if !errors.Is(err, ErrLaunchStageBindingConflict) {
					t.Fatalf("foreign observation=%#v err=%v", observation, err)
				}
				if _, err := provider.DestroyWorkspaceRuntime(ownerCtx, "ws-alpha"); err == nil || len(fixture.deletes) != 0 {
					t.Fatal("foreign secret deleted")
				}
				return
			}
			if err != nil || observation.State != "present" || len(observation.Residuals) != 1 {
				t.Fatalf("residual=%#v err=%v", observation, err)
			}
			binding, err := provider.WorkspaceRuntimeGatewaySecret(ctx, "ws-alpha")
			if err != nil || binding.Bound || binding.WorkspaceAPIKeyID != 17 {
				t.Fatalf("orphan binding=%#v err=%v", binding, err)
			}
			for range 2 {
				if _, err := provider.DestroyWorkspaceRuntime(ownerCtx, "ws-alpha"); err != nil {
					t.Fatal(err)
				}
			}
			if len(fixture.deletes) != 1 || fixture.items["secret/"+gatewaySecretName("ws-sibling")] == nil {
				t.Fatalf("wrong deletion=%v", fixture.deletes)
			}
			if _, err := provider.WorkspaceRuntimeGatewaySecret(ctx, "ws-alpha"); !errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
				t.Fatalf("absence=%v", err)
			}
		})
	}
}

func TestD4PostgresRuntimeDeleteReopensOldSuccessOnlyForActualResidue(t *testing.T) {
	ctx := context.Background()
	databaseURL := fabricTestDatabaseURL(t)
	store, err := newTestPostgresOperationStore(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.client.Close()
	provider := NewTencentProvider()
	fixture := newD4KubernetesFixture(d4GatewaySecret("acct-alpha", "ws-alpha"))
	provider.kubectl = fixture.run
	now := time.Now().Add(-time.Hour)
	key := "d4-original-runtime-delete"
	op := newOperation("destroy_workspace_runtime", "workspace_runtime", "ws-alpha", "", "ws-alpha", key, hashInput(map[string]string{"workspaceId": "ws-alpha"}), now)
	op.ID = "fop_runtime_destroy_claim_" + stableSuffix("destroy_workspace_runtime", key)
	op.Status = "succeeded"
	op.FinishedAt = now
	op.CreatedAt = now
	fillOperationResource(&op, WorkspaceRuntime{WorkspaceID: "ws-alpha", Status: "destroyed"})
	if err := store.Append(ctx, op); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reopened, err := newTestPostgresOperationStore(databaseURL)
			if err != nil {
				failures <- err
				return
			}
			defer reopened.client.Close()
			_, err = NewServiceWithOperationStore(provider, reopened).DestroyWorkspaceRuntime(ctx, "ws-alpha", key)
			failures <- err
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	final, err := store.Get(ctx, op.ID)
	if err != nil || final.Status != "succeeded" || !final.FinishedAt.After(now) || len(fixture.deletes) != 1 {
		t.Fatalf("final=%#v err=%v deletes=%v", final, err, fixture.deletes)
	}
}

func TestD4TencentStorageDeleteResumesPartialBindingBeforeFirstCBSMutation(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(fmt.Sprint(postgres), func(t *testing.T) {
			ctx := context.Background()
			var store OperationStore = NewMemoryOperationStore()
			var databaseURL string
			if postgres {
				databaseURL = fabricTestDatabaseURL(t)
				pg, err := newTestPostgresOperationStore(databaseURL)
				if err != nil {
					t.Fatal(err)
				}
				defer pg.client.Close()
				store = pg
			}
			volume := storageDestroyTestVolume("d4-original-storage")
			volume.ProviderData["region"] = "ap-guangzhou"
			volume.ProviderData["pvName"], volume.ProviderData["pvcName"] = k8sName(volume.ID)+"-pv", k8sName(volume.ID)+"-data"
			appendSucceededStorageCreate(t, store, volume)
			var manifest struct {
				Items []map[string]any `json:"items"`
			}
			if json.Unmarshal(staticCBSManifest(volume), &manifest) != nil {
				t.Fatal("invalid manifest")
			}
			fixture := newD4KubernetesFixture(manifest.Items...)
			fixture.failAfterDelete = true
			provider := NewTencentProvider()
			provider.kubectl = fixture.run
			deletes := 0
			absent := false
			provider.provision = func(_ context.Context, input provisionerRequest) (provisionerResponse, error) {
				if input.Storage.ID != volume.ProviderResourceID || input.AccountID != volume.AccountID {
					t.Fatalf("foreign disk request=%#v", input)
				}
				if input.Action == "destroy_storage_volume" {
					deletes++
					absent = true
				} else if input.Action != "sync_storage_volume" {
					t.Fatalf("unexpected cloud action=%s", input.Action)
				}
				response := provisionerResponse{OK: true, StorageVolumeID: volume.ProviderResourceID, ProviderRequestID: "d4-local-read", Status: "ready", CBSStatus: "UNATTACHED", ProviderData: map[string]string{"region": "ap-guangzhou"}}
				if absent {
					proof := exactStorageDestroyAbsence(volume)
					response.Status, response.CBSStatus = "external_deleted", "NOT_FOUND"
					response.ProviderData = maps.Clone(proof.ProviderData)
					response.ProviderData["storageDestroyMutationCount"] = "1"
					response.MutationCount = 1
				}
				return response, nil
			}
			service := NewServiceWithOperationStore(provider, store)
			if _, err := service.DestroyStorageVolume(ctx, volume.ID); !errors.Is(err, context.DeadlineExceeded) || deletes != 0 || len(fixture.items) != 1 {
				t.Fatalf("first err=%v CBS=%d remains=%d", err, deletes, len(fixture.items))
			}
			if postgres {
				reopened, err := newTestPostgresOperationStore(databaseURL)
				if err != nil {
					t.Fatal(err)
				}
				defer reopened.client.Close()
				store = reopened
			}
			service = NewServiceWithOperationStore(provider, store)
			result, err := service.DestroyStorageVolume(ctx, volume.ID)
			if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || deletes != 1 || len(fixture.items) != 0 {
				t.Fatalf("resumed=%#v err=%v CBS=%d remains=%d", result, err, deletes, len(fixture.items))
			}
			if _, err := service.DestroyStorageVolume(ctx, volume.ID); err != nil || deletes != 1 {
				t.Fatalf("replay err=%v CBS=%d", err, deletes)
			}
		})
	}
}

func TestD4TencentStorageDeleteOriginalOwnerAndOldSuccessRepairPartialBindings(t *testing.T) {
	for _, priorSuccess := range []bool{false, true} {
		for _, residual := range []string{"PersistentVolume", "PersistentVolumeClaim", "none"} {
			t.Run(fmt.Sprintf("old_success_%t_%s", priorSuccess, residual), func(t *testing.T) {
				ctx := context.Background()
				databaseURL := fabricTestDatabaseURL(t)
				store, err := newTestPostgresOperationStore(databaseURL)
				if err != nil {
					t.Fatal(err)
				}
				defer store.client.Close()
				volume := canonicalTencentStorageDestroyFixture()
				appendSucceededStorageCreate(t, store, volume)
				if priorSuccess {
					completed := exactStorageDestroyAbsence(volume)
					now := time.Date(2026, 1, 20, 8, 0, 0, 0, time.UTC)
					op := newOperation("destroy_storage_volume", "storage_volume", volume.ID, volume.AccountID, volume.WorkspaceID, "", hashInput(map[string]string{"id": volume.ID}), now)
					op.ID, op.Status, op.CreatedAt, op.FinishedAt = "old-success", "succeeded", now, now
					fillOperationResource(&op, completed)
					if err := store.Append(ctx, op); err != nil {
						t.Fatal(err)
					}
				}
				fixture := newD4KubernetesFixture()
				var manifest struct {
					Items []map[string]any `json:"items"`
				}
				if err := json.Unmarshal(staticCBSManifest(volume), &manifest); err != nil {
					t.Fatal(err)
				}
				for _, item := range manifest.Items {
					if item["kind"] == residual {
						fixture.items[strings.ToLower(residual)+"/"+stringValue(nested(item, "metadata", "name"))] = item
					}
				}
				provider := NewTencentProvider()
				provider.kubectl = fixture.run
				absent, destroys := priorSuccess, 0
				provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
					if request.Storage.ID != volume.ProviderResourceID || request.AccountID != volume.AccountID {
						t.Fatalf("wrong owner request=%#v", request)
					}
					if request.Action == "destroy_storage_volume" {
						if absent {
							t.Fatal("CBS destroy repeated after authoritative absence")
						}
						destroys++
						absent = true
					} else if request.Action != "sync_storage_volume" {
						t.Fatalf("unexpected provider action=%s", request.Action)
					}
					if !absent {
						return canonicalTencentStorageStatusResponse(request), nil
					}
					proof := exactStorageDestroyAbsence(volume)
					return provisionerResponse{OK: true, StorageVolumeID: volume.ProviderResourceID, ProviderRequestID: proof.ProviderRequestID, Status: proof.Status, CBSStatus: proof.CBSStatus, ProviderData: maps.Clone(proof.ProviderData)}, nil
				}
				for range 2 {
					reopened, err := newTestPostgresOperationStore(databaseURL)
					if err != nil {
						t.Fatal(err)
					}
					service := NewServiceWithOperationStore(provider, reopened)
					result, err := service.DestroyStorageVolume(ctx, volume.ID)
					_ = reopened.client.Close()
					if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || len(fixture.items) != 0 {
						t.Fatalf("result=%#v err=%v residual=%d", result, err, len(fixture.items))
					}
				}
				wantDestroys := 1
				if priorSuccess {
					wantDestroys = 0
				}
				if destroys != wantDestroys {
					t.Fatalf("CBS destroys=%d want=%d", destroys, wantDestroys)
				}
			})
		}
	}
}

func TestD4TencentStorageDeletionRejectsForeignRemainingPVBeforeMutation(t *testing.T) {
	volume := storageDestroyTestVolume("d4-foreign-storage")
	volume.ProviderData["region"] = "ap-guangzhou"
	volume.ProviderData["pvName"], volume.ProviderData["pvcName"] = k8sName(volume.ID)+"-pv", k8sName(volume.ID)+"-data"
	var manifest struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(staticCBSManifest(volume), &manifest)
	pv := manifest.Items[0]
	pv["spec"].(map[string]any)["csi"].(map[string]any)["volumeHandle"] = "disk-foreign"
	fixture := newD4KubernetesFixture(pv)
	provider := NewTencentProvider()
	provider.kubectl = fixture.run
	provider.provision = func(context.Context, provisionerRequest) (provisionerResponse, error) {
		t.Fatal("foreign binding crossed provider boundary")
		return provisionerResponse{}, nil
	}
	store := NewMemoryOperationStore()
	appendSucceededStorageCreate(t, store, volume)
	if _, err := NewServiceWithOperationStore(provider, store).DestroyStorageVolume(context.Background(), volume.ID); err == nil || len(fixture.deletes) != 0 {
		t.Fatalf("foreign delete=%v err=%v", fixture.deletes, err)
	}
}
