package http

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"opl-cloud/services/fabric/internal/fabric"
)

const (
	storageReleaseAction   string = "release_recycled_storage_volume"
	storageDestroyAction   string = "destroy_storage_volume"
	storageJournalStarted  string = "started"
	storagePhaseAuthorized string = "dispatch_authorized_uncertain"
	storagePhaseAbsent     string = "absence_confirmed"
	storageCBSRecycled     string = "TORECYCLE"
	storageCBSAbsent       string = "NOT_FOUND"
	storageProviderPending string = "pending"
	storageProviderAbsent  string = "external_deleted"
)

func TestStorageFinalReleaseCapabilityAndJSONDecoder(t *testing.T) {
	valid, err := json.Marshal(fabric.StorageFinalReleaseInput{AccountID: "acct-alpha", WorkspaceID: "ws-alpha", DestroyOperationID: "original-storage-destroy", Confirmation: fabric.StorageFinalReleaseConfirmation})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, caller, action, resource, workspace, token string
		body                                             []byte
		omitCapability                                   bool
		want                                             int
	}{
		{name: "ordinary destroy signature", caller: "control-plane", action: storageDestroyAction, want: http.StatusForbidden},
		{name: "control plane caller", caller: "control-plane", action: storageReleaseAction, want: http.StatusForbidden},
		{name: "foreign resource", resource: "storage-other", want: http.StatusForbidden},
		{name: "foreign workspace", workspace: "ws-other", want: http.StatusForbidden},
		{name: "runner bearer", token: "runner-secret", want: http.StatusForbidden},
		{name: "anonymous", token: "absent", want: http.StatusUnauthorized},
		{name: "missing capability", omitCapability: true, want: http.StatusForbidden},
		{name: "trailing JSON", body: append(append([]byte{}, valid...), []byte(`{}`)...), want: http.StatusForbidden},
		{name: "wrong field type", body: []byte(`{"accountId":"acct-alpha","workspaceId":"ws-alpha","destroyOperationId":42}`), want: http.StatusBadRequest},
		{name: "unknown field", body: []byte(`{"accountId":"acct-alpha","workspaceId":"ws-alpha","destroyOperationId":"original-storage-destroy","confirmation":"RELEASE_ORIGINAL_RECYCLED_CBS","extra":true}`), want: http.StatusBadRequest},
		{name: "exact capability reaches owner", want: http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := fabric.NewMemoryOperationStore()
			server := NewServerWithAuth(fabric.NewServiceWithOperationStore(testProvider{}, store), ServerAuthConfig{ControlPlaneToken: "internal-secret", RunnerToken: "runner-secret", CapabilityKey: testFabricCapabilityKey})
			body := tc.body
			if body == nil {
				body = valid
			}
			req := httptest.NewRequest(http.MethodPost, "/fabric/storage-volumes/storage-alpha/release-recycled", bytes.NewReader(body))
			token := tc.token
			if token == "" {
				token = "internal-secret"
			}
			if token != "absent" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			req.Header.Set("Idempotency-Key", "storage-final-release")
			caller, action, resource, workspace := tc.caller, tc.action, tc.resource, tc.workspace
			if caller == "" {
				caller = "operator"
			}
			if action == "" {
				action = storageReleaseAction
			}
			if resource == "" {
				resource = "storage-alpha"
			}
			if workspace == "" {
				workspace = "ws-alpha"
			}
			if !tc.omitCapability {
				req.Header.Set(fabricCapabilityHeader, fabricCapabilityForTest(t, fabricCapabilityClaimsForTest{Version: 1, Caller: caller, AccountID: "acct-alpha", WorkspaceID: workspace, ResourceKind: "storage_volume", ResourceID: resource, Action: action, OperationID: "storage-final-release", ExpiresAt: time.Now().Add(time.Minute).Unix()}, body))
			}
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", rec.Code, tc.want, rec.Body.String())
			}
			operations, err := store.List(t.Context())
			if err != nil || len(operations) != 0 {
				t.Fatalf("rejected request wrote operations: %v %v", operations, err)
			}
		})
	}
}

type storageReleaseHTTPProvider struct {
	testProvider
	releases, reads int
	seenPhase       string
}

func (p *storageReleaseHTTPProvider) ReadStorageVolumeStatus(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	p.reads++
	p.seenPhase = volume.ProviderData["storageDestroyPhase"]
	absent := false
	volume.BindingPresent = &absent
	volume.ProviderData["describeCbsRequestId"] = "describe-owned-storage"
	volume.Status, volume.CBSStatus = storageProviderPending, storageCBSRecycled
	volume.ProviderData["status"] = storageProviderPending
	volume.ProviderData["cbsStatus"] = storageCBSRecycled
	volume.ProviderData["storageVolumeId"] = volume.ProviderResourceID
	if p.releases > 0 {
		volume.Status, volume.CBSStatus, volume.DestroyState = storageProviderAbsent, storageCBSAbsent, ""
		volume.ProviderData["status"] = storageProviderAbsent
		volume.ProviderData["cbsStatus"] = storageCBSAbsent
		volume.ProviderData["storageDestroyPhase"] = storagePhaseAbsent
		volume.ProviderData["storageDestroyMutationCount"] = "0"
	}
	return volume, nil
}
func (p *storageReleaseHTTPProvider) ReleaseRecycledStorageVolume(_ context.Context, _ fabric.StorageVolume) (string, error) {
	p.releases++
	return "release-owned-storage", nil
}

func storageReleaseHTTPFixture(t *testing.T) (*fabric.Service, *fabric.MemoryOperationStore, *storageReleaseHTTPProvider, fabric.FabricOperation) {
	t.Helper()
	at := time.Date(2026, time.November, 7, 0, 0, 0, 123, time.UTC)
	volume := fabric.StorageVolume{ID: "storage-alpha", AccountID: "acct-alpha", WorkspaceID: "ws-alpha", OperationID: "create-alpha", Provider: "tencent-tke", ProviderResourceID: "disk-alpha", ProviderRequestID: "original-storage-request", SizeGB: 10, Zone: "ap-guangzhou-3", DiskType: "CLOUD_BSSD", RenewFlag: "NOTIFY_AND_MANUAL_RENEW", Deadline: "2026-12-07T00:00:00Z", CreatedAt: at,
		CostTags:     map[string]string{"opl_account_id": "acct-alpha", "opl_workspace_id": "ws-alpha", "opl_resource_id": "storage-alpha", "opl_operation_id": "create-alpha"},
		ProviderData: map[string]string{"region": "ap-guangzhou", "pvName": "opl-storage-alpha-pv", "pvcName": "opl-storage-alpha-data", "storageVolumeId": "disk-alpha", "storageDestroyPhase": storagePhaseAuthorized, "storageDestroyMutationCount": "0"},
	}
	input, err := json.Marshal(map[string]string{"id": volume.ID})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(input)
	original := fabric.FabricOperation{ID: "original-journal", OperationID: "original-storage-destroy", Action: storageDestroyAction, ResourceKind: "storage_volume", ResourceID: volume.ID, AccountID: volume.AccountID, WorkspaceID: volume.WorkspaceID, Provider: volume.Provider, ProviderRequestID: volume.ProviderRequestID, RequestHash: hex.EncodeToString(sum[:]), Status: storageJournalStarted, StartedAt: at, CreatedAt: at, RedactedProviderPayload: map[string]any{"resource": volume}}
	store := fabric.NewMemoryOperationStore()
	if err := store.Append(t.Context(), original); err != nil {
		t.Fatal(err)
	}
	provider := &storageReleaseHTTPProvider{}
	return fabric.NewServiceWithOperationStore(provider, store), store, provider, original
}

func TestStorageFinalReleaseHTTPReplaysOneFencedActionAndReadsOriginalJournal(t *testing.T) {
	service, store, provider, original := storageReleaseHTTPFixture(t)
	before, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	server := NewServerWithAuth(service, ServerAuthConfig{ControlPlaneToken: "internal-secret", CapabilityKey: testFabricCapabilityKey})
	for attempt := 0; attempt < 2; attempt++ {
		body, err := json.Marshal(fabric.StorageFinalReleaseInput{AccountID: original.AccountID, WorkspaceID: original.WorkspaceID, DestroyOperationID: original.OperationID, Confirmation: fabric.StorageFinalReleaseConfirmation})
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/fabric/storage-volumes/"+original.ResourceID+"/release-recycled", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer internal-secret")
		req.Header.Set("Idempotency-Key", "storage-final-release")
		req.Header.Set(fabricCapabilityHeader, fabricCapabilityForTest(t, fabricCapabilityClaimsForTest{Version: 1, Caller: "operator", AccountID: original.AccountID, WorkspaceID: original.WorkspaceID, ResourceKind: "storage_volume", ResourceID: original.ResourceID, Action: storageReleaseAction, OperationID: "storage-final-release", ExpiresAt: time.Now().Add(time.Minute).Unix()}, body))
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("release status=%d body=%s", rec.Code, rec.Body.String())
		}
		var result fabric.StorageFinalReleaseResult
		if err := json.NewDecoder(rec.Body).Decode(&result); err != nil || result.Status != fabric.StorageFinalReleaseStatusSucceeded || !result.AbsenceConfirmed || !result.DispatchAttempted {
			t.Fatalf("result=%#v err=%v", result, err)
		}
	}
	if provider.releases != 1 {
		t.Fatalf("provider releases=%d", provider.releases)
	}
	operations, err := store.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/fabric/storage-volumes/"+original.ResourceID+"/destroy-status", nil)
	request.Header.Set("Authorization", "Bearer internal-secret")
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, request)
	if rec.Code != http.StatusOK {
		t.Fatalf("read status=%d body=%s", rec.Code, rec.Body.String())
	}
	var observed fabric.StorageVolume
	if err := json.NewDecoder(rec.Body).Decode(&observed); err != nil || observed.ProviderResourceID != "disk-alpha" || observed.CBSStatus != storageCBSAbsent || observed.BindingPresent == nil || *observed.BindingPresent || observed.ObservedAt == "" || observed.ReadbackID == "" {
		t.Fatalf("readback=%#v err=%v", observed, err)
	}
	after, err := store.List(t.Context())
	if err != nil || !reflect.DeepEqual(operations, after) {
		t.Fatalf("GET changed operations: %v", err)
	}
	retained, err := store.Get(t.Context(), original.ID)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(retained)
	if err != nil || !bytes.Equal(before, encoded) {
		t.Fatalf("original destroy journal changed: %s", encoded)
	}
}

func TestStorageDestroyStatusHTTPRequiresServiceReadIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, token string
		want        int
	}{{"anonymous", "", http.StatusUnauthorized}, {"runner", "runner-secret", http.StatusForbidden}, {"owner", "internal-secret", http.StatusOK}} {
		t.Run(tc.name, func(t *testing.T) {
			service, store, provider, original := storageReleaseHTTPFixture(t)
			server := NewServerWithAuth(service, ServerAuthConfig{ControlPlaneToken: "internal-secret", RunnerToken: "runner-secret", CapabilityKey: testFabricCapabilityKey})
			req := httptest.NewRequest(http.MethodGet, "/fabric/storage-volumes/"+original.ResourceID+"/destroy-status", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := httptest.NewRecorder()
			server.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if tc.want == http.StatusOK && provider.seenPhase != storagePhaseAuthorized {
				t.Fatalf("read did not use original phase: %s", provider.seenPhase)
			}
			operations, err := store.List(t.Context())
			if err != nil || len(operations) != 1 || provider.releases != 0 {
				t.Fatalf("GET wrote state operations=%d releases=%d err=%v", len(operations), provider.releases, err)
			}
		})
	}
}
