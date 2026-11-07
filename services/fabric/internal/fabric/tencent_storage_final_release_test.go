package fabric

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func tencentStorageFinalReleaseFixture() StorageVolume {
	volume := canonicalTencentStorageDestroyFixture()
	volume.CBSStatus = "TORECYCLE"
	volume.Deadline = "2026-12-07T00:00:00Z"
	volume.RenewFlag = "NOTIFY_AND_MANUAL_RENEW"
	volume.DestroyState = StorageDestroyStateUnconfirmedSend
	volume.ProviderData["storageDestroyPhase"] = storageDestroyPhaseTerminateAttempted
	volume.ProviderData["storageDestroyMutationCount"] = "1"
	volume.ProviderData["storageVolumeId"] = volume.ProviderResourceID
	volume.ProviderData["cbsStatus"] = "TORECYCLE"
	volume.ProviderData["describeCbsRequestId"] = "req-original-read"
	volume.ProviderData["deadline"] = volume.Deadline
	volume.ProviderData["renewFlag"] = volume.RenewFlag
	return volume
}

func tencentStorageFinalReleaseContext(volume StorageVolume) context.Context {
	return context.WithValue(context.Background(), workspaceStorageDeleteOwnerContextKey{}, cloneStorageVolume(volume))
}

func tencentStorageFinalReleaseReadback(volume StorageVolume) provisionerResponse {
	return provisionerResponse{OK: true, Status: "pending", StorageVolumeID: volume.ProviderResourceID, CBSStatus: "TORECYCLE", ProviderRequestID: "req-recycled-read", ProviderData: cloneStorageVolume(volume).ProviderData}
}

func tencentStorageFinalReleaseAccepted(volume StorageVolume) provisionerResponse {
	return provisionerResponse{OK: true, Status: "accepted", StorageVolumeID: volume.ProviderResourceID, ProviderRequestID: "req-final-release", MutationCount: 1}
}

func TestTencentStorageFinalReleaseUsesFrozenJournalAndExactOriginalRequest(t *testing.T) {
	for _, mode := range []string{"original tags", "create operation tags", "caller phase ignored", "shortened deadline and manual flag"} {
		t.Run(mode, func(t *testing.T) {
			owner := tencentStorageFinalReleaseFixture()
			if mode == "create operation tags" {
				create := newOperation("create_storage_volume", "storage_volume", owner.ID, owner.AccountID, owner.WorkspaceID, owner.OperationID, "", time.Time{})
				owner.CostTags = oplCostTags(owner.AccountID, owner.WorkspaceID, owner.ID, create.OperationID)
			}
			input := cloneStorageVolume(owner)
			if mode == "caller phase ignored" {
				input.ProviderData["storageDestroyPhase"] = ""
			}
			frozen, beforeInput := cloneStorageVolume(owner), cloneStorageVolume(input)
			bindings := newStorageDeleteTestFixture(t, owner, false, false)
			provider := NewTencentProvider()
			provider.kubectl = bindings.run
			requests := []provisionerRequest{}
			provider.provision = func(_ context.Context, r provisionerRequest) (provisionerResponse, error) {
				requests = append(requests, r)
				switch r.Action {
				case "read_storage_for_delete":
					out := tencentStorageFinalReleaseReadback(owner)
					if mode == "shortened deadline and manual flag" {
						out.ProviderData["deadline"] = "2026-11-07T00:00:00Z"
						out.ProviderData["renewFlag"] = "DISABLE_NOTIFY_AND_MANUAL_RENEW"
					}
					return out, nil
				case storageFinalReleaseAction:
					return tencentStorageFinalReleaseAccepted(owner), nil
				default:
					t.Fatalf("unexpected action %s", r.Action)
					return provisionerResponse{}, nil
				}
			}
			ctx := tencentStorageFinalReleaseContext(owner)
			got, err := provider.ReleaseRecycledStorageVolume(ctx, input)
			if err != nil || got != "req-final-release" || len(requests) != 2 || len(bindings.deleteCalls) != 0 {
				t.Fatalf("got=%q err=%v requests=%+v binding deletes=%v", got, err, requests, bindings.deleteCalls)
			}
			want := provisionerRequest{Action: storageFinalReleaseAction, AccountID: frozen.AccountID, Region: frozen.ProviderData["region"], Zone: frozen.Zone, Tags: frozen.CostTags,
				Storage: provisionerStorage{ID: frozen.ProviderResourceID, SizeGB: uint64(frozen.SizeGB), Zone: frozen.Zone, DiskType: frozen.DiskType, Deadline: frozen.Deadline}}
			if !reflect.DeepEqual(requests[1], want) {
				t.Fatalf("request=%+v want=%+v", requests[1], want)
			}
			if !reflect.DeepEqual(owner, frozen) || !reflect.DeepEqual(input, beforeInput) || !reflect.DeepEqual(ctx.Value(workspaceStorageDeleteOwnerContextKey{}), frozen) {
				t.Fatal("final release changed original frozen identity or journal evidence")
			}
		})
	}
}

func TestTencentStorageFinalReleaseRejectsIdentityBeforeIO(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*StorageVolume, *StorageVolume)
		noOwner bool
	}{
		{name: "no journal context", mutate: func(*StorageVolume, *StorageVolume) {}, noOwner: true},
		{name: "missing journal phase", mutate: func(o *StorageVolume, _ *StorageVolume) { delete(o.ProviderData, "storageDestroyPhase") }},
		{name: "id", mutate: func(_ *StorageVolume, v *StorageVolume) { v.ID = "foreign" }},
		{name: "caller operation", mutate: func(_ *StorageVolume, v *StorageVolume) { v.OperationID = "foreign" }},
		{name: "account", mutate: func(_ *StorageVolume, v *StorageVolume) { v.AccountID = "foreign" }},
		{name: "workspace", mutate: func(_ *StorageVolume, v *StorageVolume) { v.WorkspaceID = "foreign" }},
		{name: "provider", mutate: func(_ *StorageVolume, v *StorageVolume) { v.Provider = "foreign" }},
		{name: "disk", mutate: func(_ *StorageVolume, v *StorageVolume) { v.ProviderResourceID = "disk-foreign" }},
		{name: "size", mutate: func(_ *StorageVolume, v *StorageVolume) { v.SizeGB++ }},
		{name: "storage class", mutate: func(_ *StorageVolume, v *StorageVolume) { v.StorageClass = "foreign" }},
		{name: "disk type", mutate: func(_ *StorageVolume, v *StorageVolume) { v.DiskType = "CLOUD_SSD" }},
		{name: "auto renew", mutate: func(_ *StorageVolume, v *StorageVolume) { v.RenewFlag = "NOTIFY_AND_AUTO_RENEW" }},
		{name: "deadline", mutate: func(_ *StorageVolume, v *StorageVolume) { v.Deadline = "2027-01-01T00:00:00Z" }},
		{name: "missing frozen deadline", mutate: func(o *StorageVolume, v *StorageVolume) { o.Deadline = ""; v.Deadline = "" }},
		{name: "zone", mutate: func(_ *StorageVolume, v *StorageVolume) { v.Zone = "ap-guangzhou-4" }},
		{name: "created at", mutate: func(_ *StorageVolume, v *StorageVolume) { v.CreatedAt = time.Now() }},
		{name: "region", mutate: func(_ *StorageVolume, v *StorageVolume) { v.ProviderData["region"] = "ap-shanghai" }},
		{name: "extra provider field", mutate: func(_ *StorageVolume, v *StorageVolume) { v.ProviderData["foreign"] = "value" }},
		{name: "wrong tags", mutate: func(_ *StorageVolume, v *StorageVolume) { v.CostTags["opl_operation_id"] = "foreign" }},
		{name: "extra tags", mutate: func(_ *StorageVolume, v *StorageVolume) { v.CostTags["foreign"] = "value" }},
		{name: "frozen tags drift", mutate: func(o *StorageVolume, _ *StorageVolume) { o.CostTags["opl_workspace_id"] = "foreign" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			owner := tencentStorageFinalReleaseFixture()
			input := cloneStorageVolume(owner)
			tc.mutate(&owner, &input)
			ctx := tencentStorageFinalReleaseContext(owner)
			if tc.noOwner {
				ctx = context.Background()
			}
			provider := NewTencentProvider()
			calls := 0
			provider.provision = func(context.Context, provisionerRequest) (provisionerResponse, error) {
				calls++
				return provisionerResponse{}, nil
			}
			provider.kubectl = func(context.Context, []string, []byte) ([]byte, error) { calls++; return nil, nil }
			if _, err := provider.ReleaseRecycledStorageVolume(ctx, input); err == nil || calls != 0 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestTencentStorageFinalReleaseRejectsUnverifiedReadbackOrRemainingBinding(t *testing.T) {
	for _, tc := range []struct {
		name         string
		mutate       func(*provisionerResponse)
		pvc          bool
		pv           bool
		readError    bool
		bindingError bool
	}{
		{name: "attached", mutate: func(r *provisionerResponse) { r.CBSStatus = "ATTACHED" }},
		{name: "unattached", mutate: func(r *provisionerResponse) { r.CBSStatus = "UNATTACHED" }},
		{name: "unknown", mutate: func(r *provisionerResponse) { r.CBSStatus = "UNKNOWN" }},
		{name: "absent before new stage", mutate: func(r *provisionerResponse) { r.CBSStatus = "NOT_FOUND"; r.Status = "external_deleted" }},
		{name: "missing cbs proof", mutate: func(r *provisionerResponse) { delete(r.ProviderData, "cbsStatus") }},
		{name: "missing disk proof", mutate: func(r *provisionerResponse) { delete(r.ProviderData, "storageVolumeId") }},
		{name: "missing describe request id", mutate: func(r *provisionerResponse) { r.ProviderData["describeCbsRequestId"] = "" }},
		{name: "missing describe proof", mutate: func(r *provisionerResponse) { delete(r.ProviderData, "describeCbsRequestId") }},
		{name: "wrong disk", mutate: func(r *provisionerResponse) { r.StorageVolumeID = "disk-foreign" }},
		{name: "wrong region", mutate: func(r *provisionerResponse) { r.ProviderData["region"] = "ap-shanghai" }},
		{name: "wrong size", mutate: func(r *provisionerResponse) { r.ProviderData["sizeGb"] = "99" }},
		{name: "wrong zone", mutate: func(r *provisionerResponse) { r.ProviderData["zone"] = "ap-guangzhou-4" }},
		{name: "extended deadline", mutate: func(r *provisionerResponse) { r.ProviderData["deadline"] = "2027-01-01T00:00:00Z" }},
		{name: "automatic renew", mutate: func(r *provisionerResponse) { r.ProviderData["renewFlag"] = "NOTIFY_AND_AUTO_RENEW" }},
		{name: "read rejected", mutate: func(r *provisionerResponse) { r.OK = false; r.ErrorCode = "rejected" }},
		{name: "read error", mutate: func(*provisionerResponse) {}, readError: true},
		{name: "remaining pvc", mutate: func(*provisionerResponse) {}, pvc: true},
		{name: "remaining pv", mutate: func(*provisionerResponse) {}, pv: true},
		{name: "binding read error", mutate: func(*provisionerResponse) {}, bindingError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			volume := tencentStorageFinalReleaseFixture()
			frozen := cloneStorageVolume(volume)
			bindings := newStorageDeleteTestFixture(t, volume, tc.pvc, tc.pv)
			provider := NewTencentProvider()
			provider.kubectl = bindings.run
			if tc.bindingError {
				provider.kubectl = func(context.Context, []string, []byte) ([]byte, error) {
					return nil, errors.New("binding read unavailable")
				}
			}
			releases := 0
			provider.provision = func(_ context.Context, r provisionerRequest) (provisionerResponse, error) {
				if r.Action == storageFinalReleaseAction {
					releases++
					return tencentStorageFinalReleaseAccepted(volume), nil
				}
				if r.Action != "read_storage_for_delete" {
					t.Fatalf("unexpected action %s", r.Action)
				}
				out := tencentStorageFinalReleaseReadback(volume)
				tc.mutate(&out)
				if tc.readError {
					return out, errors.New("read response lost")
				}
				return out, nil
			}
			if _, err := provider.ReleaseRecycledStorageVolume(tencentStorageFinalReleaseContext(volume), volume); err == nil || releases != 0 || len(bindings.deleteCalls) != 0 {
				t.Fatalf("err=%v releases=%d binding deletes=%v", err, releases, bindings.deleteCalls)
			}
			if !reflect.DeepEqual(volume, frozen) {
				t.Fatal("read refusal changed original journal")
			}
		})
	}
}

func TestTencentStorageFinalReleaseValidatesResponsesWithoutRetry(t *testing.T) {
	for _, tc := range []struct {
		name      string
		mutate    func(*provisionerResponse)
		sendError bool
		ok        bool
		requestID string
	}{
		{name: "accepted", mutate: func(*provisionerResponse) {}, ok: true, requestID: "req-final-release"},
		{name: "absent after precheck", mutate: func(r *provisionerResponse) {
			r.MutationCount = 0
			r.CBSStatus = "NOT_FOUND"
			r.Status = "external_deleted"
		}, ok: true, requestID: "req-final-release"},
		{name: "wrong disk", mutate: func(r *provisionerResponse) { r.StorageVolumeID = "disk-foreign" }},
		{name: "missing disk", mutate: func(r *provisionerResponse) { r.StorageVolumeID = "" }},
		{name: "negative mutation count", mutate: func(r *provisionerResponse) { r.MutationCount = -1 }},
		{name: "multiple mutations", mutate: func(r *provisionerResponse) { r.MutationCount = 2 }},
		{name: "zero without absence", mutate: func(r *provisionerResponse) { r.MutationCount = 0 }},
		{name: "absence with mutation", mutate: func(r *provisionerResponse) { r.Status = "external_deleted"; r.CBSStatus = "NOT_FOUND" }},
		{name: "missing request id", mutate: func(r *provisionerResponse) { r.ProviderRequestID = "" }},
		{name: "blank request id", mutate: func(r *provisionerResponse) { r.ProviderRequestID = " " }},
		{name: "wrong status", mutate: func(r *provisionerResponse) { r.Status = "pending" }},
		{name: "dispatch unconfirmed", mutate: func(r *provisionerResponse) { r.OK = false; r.ErrorCode = "storage_final_release_dispatch_unconfirmed" }, requestID: "req-final-release"},
		{name: "send response lost", mutate: func(*provisionerResponse) {}, sendError: true, requestID: "req-final-release"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			volume := tencentStorageFinalReleaseFixture()
			provider := NewTencentProvider()
			bindings := newStorageDeleteTestFixture(t, volume, false, false)
			provider.kubectl = bindings.run
			reads, releases := 0, 0
			provider.provision = func(_ context.Context, r provisionerRequest) (provisionerResponse, error) {
				if r.Action == "read_storage_for_delete" {
					reads++
					return tencentStorageFinalReleaseReadback(volume), nil
				}
				if r.Action != storageFinalReleaseAction {
					t.Fatalf("unexpected action %s", r.Action)
				}
				releases++
				out := tencentStorageFinalReleaseAccepted(volume)
				tc.mutate(&out)
				if tc.sendError {
					return out, errors.New("response lost after send")
				}
				return out, nil
			}
			got, err := provider.ReleaseRecycledStorageVolume(tencentStorageFinalReleaseContext(volume), volume)
			if (err == nil) != tc.ok || got != tc.requestID || reads != 1 || releases != 1 || len(bindings.deleteCalls) != 0 {
				t.Fatalf("got=%q err=%v reads=%d releases=%d deletes=%v", got, err, reads, releases, bindings.deleteCalls)
			}
		})
	}
}
