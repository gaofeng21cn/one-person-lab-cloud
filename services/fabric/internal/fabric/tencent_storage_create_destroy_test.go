package fabric

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestTencentStorageDestroyAdmitsRealCreateOperationIdentity(t *testing.T) {
	input := StorageVolumeInput{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", IdempotencyKey: "original-create-key", Zone: "ap-guangzhou-3", SizeGB: 10}
	create := newOperation("create_storage_volume", "storage_volume", input.ID, input.AccountID, input.WorkspaceID, input.IdempotencyKey, hashInput(input), time.Now())
	input.OperationID = create.OperationID
	state := tencentCBSCreateMutationState{SchemaVersion: tencentCBSCreateMutationStateSchemaVersion, OperationID: create.OperationID, AccountID: input.AccountID, WorkspaceID: input.WorkspaceID, StorageID: input.ID, Region: "ap-guangzhou", Zone: input.Zone, DiskType: "CLOUD_BSSD", SizeGB: 10}
	resource := tencentCBSCreateVolume(input, state, time.Now())
	resource.ProviderResourceID, resource.Status = "disk-storage-alpha", "ready"
	if resource.OperationID == resource.CostTags["opl_operation_id"] {
		t.Fatal("fixture must preserve distinct caller and provider identities")
	}
	provider := NewTencentProvider()
	var deleted []string
	provider.kubectl = exactTencentStorageBindingKubectl(t, resource, &deleted)
	destroys := 0
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		if !reflect.DeepEqual(request.Tags, resource.CostTags) {
			t.Fatal("persisted ownership tags were rewritten")
		}
		if request.Action == "sync_storage_volume" {
			return canonicalTencentStorageStatusResponse(request), nil
		}
		if request.Action != "destroy_storage_volume" {
			t.Fatalf("unexpected action %s", request.Action)
		}
		destroys++
		return provisionerResponse{OK: true, StorageVolumeID: resource.ProviderResourceID, CBSStatus: "NOT_FOUND", Status: "external_deleted", ProviderRequestID: "req-destroy", MutationCount: 1, ProviderData: map[string]string{"storageVolumeId": resource.ProviderResourceID, "cbsStatus": "NOT_FOUND", "status": "external_deleted", "storageDestroyPhase": storageDestroyPhaseAbsenceConfirmed, "storageDestroyMutationCount": "1", "terminateCbsRequestId": "req-destroy", "describeCbsRequestId": "req-describe", "region": "ap-guangzhou"}}, nil
	}
	got, err := provider.DestroyStorageVolume(context.Background(), resource)
	if err != nil || got.Status != "external_deleted" || destroys != 1 || len(deleted) == 0 {
		t.Fatalf("destroy result=%+v err=%v destroys=%d deleted=%v", got, err, destroys, deleted)
	}
	if got.OperationID != input.IdempotencyKey || !reflect.DeepEqual(got.CostTags, resource.CostTags) {
		t.Fatal("original identity was not retained")
	}
	for _, field := range []string{"caller", "provider-tag", "workspace", "extra-tag"} {
		t.Run(field, func(t *testing.T) {
			bad := cloneStorageVolume(resource)
			switch field {
			case "caller":
				bad.OperationID = "foreign-key"
			case "provider-tag":
				bad.CostTags["opl_operation_id"] = "op-foreign"
			case "workspace":
				bad.WorkspaceID = "ws-foreign"
			case "extra-tag":
				bad.CostTags["extra"] = "foreign"
			}
			rejected := NewTencentProvider()
			calls := 0
			rejected.kubectl = func(context.Context, []string, []byte) ([]byte, error) { calls++; return nil, nil }
			rejected.provision = func(context.Context, provisionerRequest) (provisionerResponse, error) {
				calls++
				return provisionerResponse{}, nil
			}
			if _, err := rejected.DestroyStorageVolume(context.Background(), bad); err == nil || calls != 0 {
				t.Fatalf("foreign identity admitted: err=%v calls=%d", err, calls)
			}
		})
	}
}
