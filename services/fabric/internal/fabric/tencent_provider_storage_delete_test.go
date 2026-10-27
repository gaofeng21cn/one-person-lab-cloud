package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type storageDeleteTestObject struct {
	APIVersion string                       `json:"apiVersion"`
	Kind       string                       `json:"kind"`
	Metadata   storageDeleteTestObjectMeta  `json:"metadata"`
	Spec       json.RawMessage              `json:"spec"`
	Status     storageDeleteTestObjectState `json:"status,omitempty"`
}

type storageDeleteTestObjectMeta struct {
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
}

type storageDeleteTestObjectState struct {
	Phase string `json:"phase,omitempty"`
}

type storageDeleteTestList struct {
	Kind  string                    `json:"kind"`
	Items []storageDeleteTestObject `json:"items"`
}

type storageDeleteTestFixture struct {
	items        storageDeleteTestList
	deleteCalls  [][]string
	failPVBound  bool
	failPVCReply bool
}

func newStorageDeleteTestFixture(t *testing.T, volume StorageVolume, keepPVC, keepPV bool) *storageDeleteTestFixture {
	t.Helper()
	var list storageDeleteTestList
	if err := json.Unmarshal(staticCBSManifest(volume), &list); err != nil {
		t.Fatal(err)
	}
	list.Kind = "List"
	for i := range list.Items {
		item := &list.Items[i]
		if item.Kind == "PersistentVolume" {
			item.Status.Phase = "Bound"
			if !keepPV {
				continue
			}
		} else if item.Kind == "PersistentVolumeClaim" {
			if !keepPVC {
				continue
			}
		}
	}
	items := make([]storageDeleteTestObject, 0, len(list.Items))
	for _, item := range list.Items {
		if item.Kind == "PersistentVolume" && !keepPV || item.Kind == "PersistentVolumeClaim" && !keepPVC {
			continue
		}
		items = append(items, item)
	}
	list.Items = items
	return &storageDeleteTestFixture{items: list, failPVBound: true}
}

func (f *storageDeleteTestFixture) run(_ context.Context, args []string, _ []byte) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("kubectl_action_required")
	}
	switch args[0] {
	case "get":
		requested := map[string]bool{}
		for _, arg := range args[1:] {
			if strings.HasPrefix(arg, "--") {
				break
			}
			requested[arg] = true
		}
		result := storageDeleteTestList{Kind: "List", Items: make([]storageDeleteTestObject, 0, len(requested))}
		for _, item := range f.items.Items {
			resourceName := "pvc/" + item.Metadata.Name
			if item.Kind == "PersistentVolume" {
				resourceName = "pv/" + item.Metadata.Name
			}
			if requested[resourceName] {
				result.Items = append(result.Items, item)
			}
		}
		return json.Marshal(result)
	case "delete":
		resources := make([]string, 0, 2)
		for _, arg := range args[1:] {
			if strings.HasPrefix(arg, "--") {
				break
			}
			resources = append(resources, arg)
		}
		f.deleteCalls = append(f.deleteCalls, slices.Clone(resources))
		for _, resource := range resources {
			for index, item := range f.items.Items {
				name := "pvc/" + item.Metadata.Name
				if item.Kind == "PersistentVolume" {
					name = "pv/" + item.Metadata.Name
				}
				if name != resource {
					continue
				}
				if item.Kind == "PersistentVolume" && f.failPVBound && item.Status.Phase == "Bound" {
					return nil, errors.New("admission webhook denied deleting a PV bound to a PVC")
				}
				f.items.Items = append(f.items.Items[:index], f.items.Items[index+1:]...)
				break
			}
			if strings.HasPrefix(resource, "pvc/") && f.failPVCReply {
				f.failPVCReply = false
				return nil, errors.New("PVC delete response lost after deletion")
			}
		}
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected kubectl action %q", args[0])
	}
}

func (f *storageDeleteTestFixture) setPVPhase(name, phase string) {
	for i := range f.items.Items {
		if f.items.Items[i].Kind == "PersistentVolume" && f.items.Items[i].Metadata.Name == name {
			f.items.Items[i].Status.Phase = phase
		}
	}
}

func TestTencentProviderDestroyStorageDeletesPVCThenWaitsForReleasedPV(t *testing.T) {
	resource := canonicalTencentStorageDestroyFixture()
	fixture := newStorageDeleteTestFixture(t, resource, true, true)
	provider := NewTencentProvider()
	provider.kubectl = fixture.run
	destroyCalls := 0
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		if request.Action != "read_storage_for_delete" && request.Action != "destroy_storage_volume" {
			t.Fatalf("unexpected cloud action %q", request.Action)
		}
		if request.Action == "destroy_storage_volume" {
			destroyCalls++
		}
		return provisionerResponse{OK: true, StorageVolumeID: resource.ProviderResourceID, ProviderRequestID: "read-request", CBSStatus: "NOT_FOUND", Status: "external_deleted", ProviderData: map[string]string{
			"storageVolumeId": resource.ProviderResourceID, "cbsStatus": "NOT_FOUND", "status": "external_deleted", "storageDestroyPhase": "absence_confirmed", "storageDestroyMutationCount": "0", "describeCbsRequestId": "read-request", "region": "ap-guangzhou",
		}}, nil
	}
	ctx := context.WithValue(context.Background(), workspaceStorageDeleteOwnerContextKey{}, resource)

	result, err := provider.DestroyStorageVolume(ctx, resource)
	if !errors.Is(err, ErrWorkspaceLaunchPending) || result.DestroyState != StorageDestroyStatePendingRetry {
		t.Fatalf("Bound PV did not remain pending: result=%#v err=%v", result, err)
	}
	if len(fixture.deleteCalls) != 1 || !reflect.DeepEqual(fixture.deleteCalls[0], []string{"pvc/" + resource.ProviderData["pvcName"]}) {
		t.Fatalf("first pass did not delete only the PVC: calls=%#v", fixture.deleteCalls)
	}
	if destroyCalls != 0 {
		t.Fatalf("already-absent CBS unexpectedly dispatched destroy: calls=%d", destroyCalls)
	}

	fixture.setPVPhase(resource.ProviderData["pvName"], "Released")
	result, err = provider.DestroyStorageVolume(ctx, result)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) {
		t.Fatalf("released PV cleanup failed: result=%#v err=%v", result, err)
	}
	if len(fixture.items.Items) != 0 || len(fixture.deleteCalls) != 2 ||
		!reflect.DeepEqual(fixture.deleteCalls[1], []string{"pv/" + resource.ProviderData["pvName"]}) {
		t.Fatalf("PV deletion did not follow binding absence and release: calls=%#v items=%#v", fixture.deleteCalls, fixture.items.Items)
	}
	if destroyCalls != 0 {
		t.Fatalf("already-absent CBS was terminated: calls=%d", destroyCalls)
	}
}

func TestTencentProviderDestroyStorageRecoversLostPVCDeleteResponse(t *testing.T) {
	resource := canonicalTencentStorageDestroyFixture()
	fixture := newStorageDeleteTestFixture(t, resource, true, true)
	fixture.failPVCReply = true
	provider := NewTencentProvider()
	provider.kubectl = fixture.run
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		return provisionerResponse{OK: true, StorageVolumeID: resource.ProviderResourceID, ProviderRequestID: "read-request", CBSStatus: "NOT_FOUND", Status: "external_deleted", ProviderData: map[string]string{
			"storageVolumeId": resource.ProviderResourceID, "cbsStatus": "NOT_FOUND", "status": "external_deleted", "storageDestroyPhase": "absence_confirmed", "storageDestroyMutationCount": "0", "describeCbsRequestId": "read-request", "region": "ap-guangzhou",
		}}, nil
	}
	ctx := context.WithValue(context.Background(), workspaceStorageDeleteOwnerContextKey{}, resource)

	result, err := provider.DestroyStorageVolume(ctx, resource)
	if err == nil || result.ProviderData["storageDestroyPhase"] != storageDestroyPhaseBindingDeletePending ||
		len(fixture.items.Items) != 1 || fixture.items.Items[0].Kind != "PersistentVolume" {
		t.Fatalf("lost PVC delete response was not retained as partial progress: result=%#v err=%v items=%#v", result, err, fixture.items.Items)
	}

	fixture.setPVPhase(resource.ProviderData["pvName"], "Released")
	result, err = provider.DestroyStorageVolume(ctx, result)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || len(fixture.items.Items) != 0 {
		t.Fatalf("restart did not finish original partial binding cleanup: result=%#v err=%v items=%#v", result, err, fixture.items.Items)
	}
	if len(fixture.deleteCalls) != 2 || !reflect.DeepEqual(fixture.deleteCalls[1], []string{"pv/" + resource.ProviderData["pvName"]}) {
		t.Fatalf("resumed binding cleanup repeated or reordered delete: calls=%#v", fixture.deleteCalls)
	}
}

func TestTencentProviderDestroyStorageRemovesReleasedPVWhenPVCAlreadyAbsent(t *testing.T) {
	resource := canonicalTencentStorageDestroyFixture()
	fixture := newStorageDeleteTestFixture(t, resource, false, true)
	fixture.setPVPhase(resource.ProviderData["pvName"], "Released")
	provider := NewTencentProvider()
	provider.kubectl = fixture.run
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		if request.Action == "destroy_storage_volume" {
			t.Fatal("absent CBS must not be terminated again")
		}
		return provisionerResponse{OK: true, StorageVolumeID: resource.ProviderResourceID, ProviderRequestID: "read-request", CBSStatus: "NOT_FOUND", Status: "external_deleted", ProviderData: map[string]string{
			"storageVolumeId": resource.ProviderResourceID, "cbsStatus": "NOT_FOUND", "status": "external_deleted", "storageDestroyPhase": "absence_confirmed", "storageDestroyMutationCount": "0", "describeCbsRequestId": "read-request", "region": "ap-guangzhou",
		}}, nil
	}
	ctx := context.WithValue(context.Background(), workspaceStorageDeleteOwnerContextKey{}, resource)
	result, err := provider.DestroyStorageVolume(ctx, resource)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || len(fixture.items.Items) != 0 {
		t.Fatalf("released PV was not removed: result=%#v err=%v items=%#v", result, err, fixture.items.Items)
	}
	if len(fixture.deleteCalls) != 1 || !reflect.DeepEqual(fixture.deleteCalls[0], []string{"pv/" + resource.ProviderData["pvName"]}) {
		t.Fatalf("expected only released PV deletion, calls=%#v", fixture.deleteCalls)
	}
}

func TestTencentProviderDestroyStorageWaitsWhenPVCRemains(t *testing.T) {
	resource := canonicalTencentStorageDestroyFixture()
	fixture := newStorageDeleteTestFixture(t, resource, true, true)
	provider := NewTencentProvider()
	provider.kubectl = func(ctx context.Context, args []string, stdin []byte) ([]byte, error) {
		if len(args) > 0 && args[0] == "delete" {
			fixture.deleteCalls = append(fixture.deleteCalls, slices.Clone(args[1:2]))
			return nil, nil
		}
		return fixture.run(ctx, args, stdin)
	}
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		return provisionerResponse{OK: true, StorageVolumeID: resource.ProviderResourceID, ProviderRequestID: "read-request", CBSStatus: "UNATTACHED", Status: "provider_ready", ProviderData: map[string]string{"region": "ap-guangzhou"}}, nil
	}
	ctx := context.WithValue(context.Background(), workspaceStorageDeleteOwnerContextKey{}, resource)
	result, err := provider.DestroyStorageVolume(ctx, resource)
	if !errors.Is(err, ErrWorkspaceLaunchPending) || result.DestroyState != StorageDestroyStatePendingRetry || len(fixture.deleteCalls) != 1 {
		t.Fatalf("PVC still present was not a retryable wait: result=%#v err=%v calls=%#v", result, err, fixture.deleteCalls)
	}
	if len(fixture.items.Items) != 2 {
		t.Fatalf("wait path changed objects unexpectedly: %#v", fixture.items.Items)
	}
}

func TestTencentProviderDestroyStorageRejectsEmptyBindingReadback(t *testing.T) {
	resource := canonicalTencentStorageDestroyFixture()
	provider := NewTencentProvider()
	provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		if len(args) > 0 && args[0] == "get" {
			return nil, nil
		}
		t.Fatalf("unverified binding readback authorized kubectl action: %#v", args)
		return nil, nil
	}
	destroyCalls := 0
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		if request.Action == "destroy_storage_volume" {
			destroyCalls++
		}
		return provisionerResponse{OK: true, StorageVolumeID: resource.ProviderResourceID, ProviderRequestID: "read-request", CBSStatus: "UNATTACHED", Status: "provider_ready", ProviderData: map[string]string{
			"storageVolumeId": resource.ProviderResourceID, "cbsStatus": "UNATTACHED", "status": "provider_ready", "describeCbsRequestId": "read-request", "region": "ap-guangzhou",
		}}, nil
	}
	ctx := context.WithValue(context.Background(), workspaceStorageDeleteOwnerContextKey{}, resource)
	result, err := provider.DestroyStorageVolume(ctx, resource)
	if err == nil || !strings.Contains(err.Error(), "kubernetes_response_invalid") {
		t.Fatalf("empty binding readback was accepted: result=%#v err=%v", result, err)
	}
	if destroyCalls != 0 {
		t.Fatalf("empty binding readback authorized CBS termination: calls=%d", destroyCalls)
	}
}

func TestTencentProviderDestroyStorageResumesPartialBindingThroughService(t *testing.T) {
	resource := canonicalTencentStorageDestroyFixture()
	fixture := newStorageDeleteTestFixture(t, resource, true, true)
	provider := NewTencentProvider()
	provider.kubectl = fixture.run
	destroyCalls := 0
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		if request.Action == "destroy_storage_volume" {
			destroyCalls++
			t.Fatal("CBS absence must not trigger another terminate")
		}
		return provisionerResponse{OK: true, StorageVolumeID: resource.ProviderResourceID, ProviderRequestID: "read-request", CBSStatus: "NOT_FOUND", Status: "external_deleted", ProviderData: map[string]string{
			"storageVolumeId": resource.ProviderResourceID, "cbsStatus": "NOT_FOUND", "status": "external_deleted", "storageDestroyPhase": "absence_confirmed", "storageDestroyMutationCount": "0", "describeCbsRequestId": "read-request", "region": "ap-guangzhou",
		}}, nil
	}
	store := NewMemoryOperationStore()
	service := NewServiceWithOperationStore(provider, store)
	service.volumes[resource.ID] = cloneStorageVolume(resource)

	result, err := service.DestroyStorageVolume(context.Background(), resource.ID)
	if !errors.Is(err, ErrWorkspaceLaunchPending) || result.ProviderData["storageDestroyPhase"] != storageDestroyPhaseBindingDeletePending {
		t.Fatalf("first service pass did not persist pending binding deletion: result=%#v err=%v", result, err)
	}
	fixture.setPVPhase(resource.ProviderData["pvName"], "Released")
	result, err = service.DestroyStorageVolume(context.Background(), resource.ID)
	if err != nil || !storageDestroyReadbackConfirmsAbsence(result) || result.BindingPresent == nil || *result.BindingPresent {
		t.Fatalf("service retry did not converge absent CBS and binding: result=%#v err=%v", result, err)
	}
	if destroyCalls != 0 || len(fixture.items.Items) != 0 || len(fixture.deleteCalls) != 2 ||
		!reflect.DeepEqual(fixture.deleteCalls[0], []string{"pvc/" + resource.ProviderData["pvcName"]}) ||
		!reflect.DeepEqual(fixture.deleteCalls[1], []string{"pv/" + resource.ProviderData["pvName"]}) {
		t.Fatalf("service retry repeated or reordered external mutations: destroys=%d deletes=%#v items=%#v", destroyCalls, fixture.deleteCalls, fixture.items.Items)
	}
}
