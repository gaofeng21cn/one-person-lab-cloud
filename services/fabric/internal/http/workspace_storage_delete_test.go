package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opl-cloud/services/fabric/internal/fabric"
)

// classifiedStorageDeleteProvider reports one stable, retryable storage deletion
// classification and never terminates the disk.
type classifiedStorageDeleteProvider struct {
	testProvider
	state string
}

func (p classifiedStorageDeleteProvider) DestroyStorageVolume(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	volume.Status, volume.CBSStatus = "ready", "ATTACHED"
	volume.ProviderData = map[string]string{"storageDestroyPhase": "terminate_not_attempted", "storageDestroyMutationCount": "0"}
	volume.DestroyState = p.state
	return volume, fabric.ErrWorkspaceLaunchPending
}

// A classified storage deletion outcome must reach the caller with its
// classification, so the owning operation can retry instead of recording a
// terminal failure. An unclassified failure keeps failing.
func TestStorageDestroyReportsClassifiedRetryableOutcome(t *testing.T) {
	store := fabric.NewMemoryOperationStore()
	provider := classifiedStorageDeleteProvider{state: fabric.StorageDestroyStatePendingRetry}
	service := fabric.NewServiceWithOperationStore(provider, store)
	server := newTestServer(service, "internal-secret")
	compute := createReadyCompute(t, service, server, "acct-alpha", "ws-alpha", "classified-storage-compute")

	create := testRequest(http.MethodPost, "/fabric/storage-volumes", bytes.NewBufferString(fmt.Sprintf(`{"accountId":"acct-alpha","workspaceId":"ws-alpha","computeId":%q,"zone":"ap-guangzhou-3","sizeGb":10}`, compute.ID)))
	create.Header.Set("Idempotency-Key", "classified-storage-create")
	createRec := httptest.NewRecorder()
	server.ServeHTTP(createRec, create)
	if createRec.Code != http.StatusAccepted {
		t.Fatalf("create status = %d: %s", createRec.Code, createRec.Body.String())
	}
	var resource fabric.StorageVolume
	if err := json.NewDecoder(createRec.Body).Decode(&resource); err != nil {
		t.Fatal(err)
	}

	request := testRequest(http.MethodPost, "/fabric/storage-volumes/"+resource.ID+"/destroy", strings.NewReader(`{}`))
	request.Header.Set("Idempotency-Key", "destroy-classified")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("classified status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if json.Unmarshal(response.Body.Bytes(), &body) != nil || body["destroyState"] != fabric.StorageDestroyStatePendingRetry {
		t.Fatalf("classified body=%s", response.Body.String())
	}
}

// An unclassified provider failure is still a failure, never a retryable wait.
func TestStorageDestroyKeepsUnclassifiedFailureFailing(t *testing.T) {
	provider := unclassifiedStorageDeleteProvider{}
	service := fabric.NewServiceWithOperationStore(provider, fabric.NewMemoryOperationStore())
	server := newTestServer(service, "internal-secret")
	request := testRequest(http.MethodPost, "/fabric/storage-volumes/storage-unclassified/destroy", strings.NewReader(`{}`))
	request.Header.Set("Idempotency-Key", "destroy-unclassified")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code == http.StatusAccepted {
		t.Fatalf("unclassified status=%d body=%s", response.Code, response.Body.String())
	}
}

type unclassifiedStorageDeleteProvider struct {
	testProvider
}

func (unclassifiedStorageDeleteProvider) DestroyStorageVolume(_ context.Context, volume fabric.StorageVolume) (fabric.StorageVolume, error) {
	return volume, errors.New("storage_volume_destroy_readback_mismatch")
}
