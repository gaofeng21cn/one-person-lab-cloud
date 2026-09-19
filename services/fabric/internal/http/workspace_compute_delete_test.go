package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"opl-cloud/services/fabric/internal/fabric"
)

// computeDestroyStatusProvider answers the read-only compute delete-status
// readback with typed absence facts and no provider mutation.
type computeDestroyStatusProvider struct {
	testProvider
	readback fabric.ComputeAllocation
	present  bool
}

func (p computeDestroyStatusProvider) ReadComputeDestroyStatus(_ context.Context, allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
	allocation.Status = p.readback.Status
	allocation.MachinePresent = &p.present
	allocation.TKEStatus = p.readback.TKEStatus
	allocation.CVMStatus = p.readback.CVMStatus
	allocation.DestroyState = p.readback.DestroyState
	return allocation, nil
}

func (p computeDestroyStatusProvider) DestroyComputeAllocation(_ context.Context, allocation fabric.ComputeAllocation) (fabric.ComputeAllocation, error) {
	return allocation, errors.New("unexpected mutation")
}

// validAbsenceClaim reports whether a readback asserts the complete absence facts
// the platform refund precondition requires.
func validAbsenceClaim(allocation fabric.ComputeAllocation) bool {
	return allocation.Status == "external_deleted" && allocation.TKEStatus == "NOT_FOUND" && allocation.CVMStatus == "NOT_FOUND"
}

// The compute delete-status endpoint is a typed, mutation-free readback: it must
// report the observed facts so the caller can retry the same operation instead of
// inferring state from a status string.
func TestComputeDestroyStatusReportsTypedAbsenceFacts(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		present   bool
		status    string
		tkeStatus string
		cvmStatus string
		state     string
	}{
		{name: "machine present", present: true, status: "present", tkeStatus: "RUNNING", state: fabric.StorageDestroyStatePendingRetry},
		{name: "machine absent while cvm terminates", status: "present", tkeStatus: "NOT_FOUND", cvmStatus: "SHUTDOWN", state: fabric.StorageDestroyStatePendingRetry},
		{name: "complete absence", status: "external_deleted", tkeStatus: "NOT_FOUND", cvmStatus: "NOT_FOUND"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			store := fabric.NewMemoryOperationStore()
			provider := computeDestroyStatusProvider{present: testCase.present, readback: fabric.ComputeAllocation{Status: testCase.status, TKEStatus: testCase.tkeStatus, CVMStatus: testCase.cvmStatus, DestroyState: testCase.state}}
			service := fabric.NewServiceWithOperationStore(provider, store)
			server := newTestServer(service, "internal-secret")
			compute := createReadyCompute(t, service, server, "acct-alpha", "ws-alpha", "compute-destroy-status-"+testCase.name)

			request := testRequest(http.MethodGet, "/fabric/compute-allocations/"+compute.ID+"/destroy-status", nil)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var body fabric.ComputeAllocation
			if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.ID != compute.ID || body.MachinePresent == nil || *body.MachinePresent != testCase.present ||
				body.TKEStatus != testCase.tkeStatus || body.CVMStatus != testCase.cvmStatus || body.DestroyState != testCase.state {
				t.Fatalf("readback body=%#v", body)
			}
			if testCase.state != "" && validAbsenceClaim(body) {
				t.Fatalf("unfinished deletion claimed absence: %#v", body)
			}
		})
	}
}
