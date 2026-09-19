package fabric

import (
	"context"
	"errors"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

// computeDestroyStatusResponse builds the provisioner readback response for one
// authoritative compute delete-status read, which is always mutation-free.
func computeDestroyStatusResponse(request provisionerRequest, machinePresent bool, tkeStatus, cvmStatus string) provisionerResponse {
	providerData := map[string]string{
		"clusterId": "cls-alpha", "region": "ap-guangzhou", "nodePoolId": request.Pool.NodePoolID,
		"machineName": request.Allocation.MachineName, "nodeName": request.Allocation.NodeName, "privateIp": request.Allocation.PrivateIP,
		"machineType": request.Allocation.MachineType, "cvmApplicable": computeDestroyCVMApplicable(request.Allocation.MachineType),
		"machinePresent": "false", "tkeStatus": tkeStatus, "describeClusterMachinesReq": "req-describe-machines",
	}
	if machinePresent {
		providerData["machinePresent"], providerData["syncResult"] = "true", "found"
	}
	status := "external_deleted"
	if machinePresent || cvmStatus != "" && cvmStatus != "NOT_FOUND" {
		status = "present"
	}
	if cvmStatus != "" {
		providerData["cvmStatus"] = cvmStatus
		providerData["describeCvmRequestId"] = "req-describe-cvm"
	}
	if machinePresent {
		providerData["syncResult"] = "found"
	} else if cvmStatus == "NOT_FOUND" || cvmStatus == "" {
		providerData["syncResult"] = "missing"
	}
	return provisionerResponse{
		OK: true, Status: status, InstanceID: request.Allocation.InstanceID, MachinePresent: &machinePresent,
		TKEStatus: tkeStatus, CVMStatus: cvmStatus, ProviderRequestID: "req-compute-destroy-status", ProviderData: providerData, MutationCount: 0,
	}
}

// computeDestroyCVMApplicable mirrors the persisted provider convention: only a
// NativeCVM machine owns a CVM that must be read back separately.
func computeDestroyCVMApplicable(machineType string) string {
	if machineType == "NativeCVM" {
		return "true"
	}
	return "false"
}

// computeDestroyReadbackAllocation returns the retained allocation state after a
// Tencent compute terminate was already dispatched.
func computeDestroyReadbackAllocation(id string, phase, mutationCount string) ComputeAllocation {
	resource := computeDestroyPhaseResource()
	resource.ID, resource.OperationID = id, "op-"+id
	resource.ProviderResourceID = resource.InstanceID
	resource.Status = "destroying"
	resource.ProviderData[tencentComputeDestroyPhaseKey] = phase
	resource.ProviderData[tencentComputeDestroyMutationCountKey] = mutationCount
	resource.CostTags["opl_resource_id"] = id
	return resource
}

func tencentComputeDestroyStatusProvider(t *testing.T, response provisionerResponse) *TencentProvider {
	t.Helper()
	provider := NewTencentProvider()
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		if request.Action != "read_compute_destroy_status" {
			return provisionerResponse{}, errors.New("unexpected provisioner request")
		}
		return response, nil
	}
	return provider
}

func TestTencentComputeDestroyStatusStaysRetryableWhileCvmTerminates(t *testing.T) {
	// The Machine is gone but its CVM is still being reclaimed. This is an
	// ordinary convergence wait: it must report typed facts, never absence.
	allocation := computeDestroyReadbackAllocation("compute-cvm-terminating", tencentComputeDestroyPhaseAttempted, "1")
	var captured provisionerRequest
	provider := NewTencentProvider()
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		captured = request
		return computeDestroyStatusResponse(request, false, "NOT_FOUND", "SHUTDOWN"), nil
	}

	readback, err := provider.ReadComputeDestroyStatus(context.Background(), allocation)
	if err != nil {
		t.Fatalf("cvm terminating readback err=%v", err)
	}
	if readback.Status == "external_deleted" || validTencentComputeAbsenceEvidence(readback) {
		t.Fatalf("cvm terminating reported as deleted: %#v", readback)
	}
	if readback.MachinePresent == nil || *readback.MachinePresent || readback.TKEStatus != "NOT_FOUND" || readback.CVMStatus != "SHUTDOWN" {
		t.Fatalf("typed facts=%#v", readback)
	}
	if readback.DestroyState != contractsComputeDestroyState(t, false) {
		t.Fatalf("state=%q", readback.DestroyState)
	}
	if captured.Allocation.MachineName != allocation.MachineName || captured.Allocation.InstanceID != allocation.InstanceID ||
		captured.Pool.NodePoolID != allocation.NodePoolID || captured.Allocation.MachineType != allocation.ProviderData["machineType"] {
		t.Fatalf("readback identity=%#v", captured)
	}
}

func TestTencentComputeDestroyStatusStaysRetryableWhileMachinePresent(t *testing.T) {
	allocation := computeDestroyReadbackAllocation("compute-machine-present", tencentComputeDestroyPhaseAttempted, "1")
	provider := NewTencentProvider()
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		return computeDestroyStatusResponse(request, true, "RUNNING", ""), nil
	}
	readback, err := provider.ReadComputeDestroyStatus(context.Background(), allocation)
	if err != nil {
		t.Fatalf("machine present readback err=%v", err)
	}
	if readback.MachinePresent == nil || !*readback.MachinePresent || readback.TKEStatus != "RUNNING" || validTencentComputeAbsenceEvidence(readback) {
		t.Fatalf("machine present readback=%#v", readback)
	}
	if readback.DestroyState != contractsComputeDestroyState(t, false) {
		t.Fatalf("state=%q", readback.DestroyState)
	}
}

func TestTencentComputeDestroyStatusReportsNoDispatchAsPendingRetry(t *testing.T) {
	// The retained allocation records no authorized terminate, so the owning
	// operation may still delete the node and the classification says so.
	allocation := computeDestroyReadbackAllocation("compute-no-dispatch", "", "")
	delete(allocation.ProviderData, tencentComputeDestroyPhaseKey)
	delete(allocation.ProviderData, tencentComputeDestroyMutationCountKey)
	provider := NewTencentProvider()
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		return computeDestroyStatusResponse(request, false, "NOT_FOUND", "RUNNING"), nil
	}
	readback, err := provider.ReadComputeDestroyStatus(context.Background(), allocation)
	if err != nil || readback.DestroyState != contractsComputeDestroyState(t, true) {
		t.Fatalf("no-dispatch readback=%#v err=%v", readback, err)
	}
}

func TestTencentComputeDestroyStatusMarksAuthorizedDispatchAsUnconfirmedSend(t *testing.T) {
	// Fabric persists this phase immediately before the terminate RPC, so the
	// mutation may already be in flight and a retry must stay readback-only.
	allocation := computeDestroyReadbackAllocation("compute-authorized-dispatch", tencentComputeDestroyPhaseDispatchAuthorized, "0")
	provider := NewTencentProvider()
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		return computeDestroyStatusResponse(request, true, "RUNNING", ""), nil
	}
	readback, err := provider.ReadComputeDestroyStatus(context.Background(), allocation)
	if err != nil || readback.DestroyState != contractsComputeDestroyState(t, false) {
		t.Fatalf("authorized dispatch readback=%#v err=%v", readback, err)
	}
}

func TestTencentComputeDestroyStatusConfirmsOnlyCompleteAbsence(t *testing.T) {
	allocation := computeDestroyReadbackAllocation("compute-complete-absence", tencentComputeDestroyPhaseAttempted, "1")
	provider := NewTencentProvider()
	provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
		return computeDestroyStatusResponse(request, false, "NOT_FOUND", "NOT_FOUND"), nil
	}
	readback, err := provider.ReadComputeDestroyStatus(context.Background(), allocation)
	if err != nil || readback.Status != "external_deleted" || readback.DestroyState != "" || !validTencentComputeAbsenceEvidence(readback) {
		t.Fatalf("absence readback=%#v err=%v", readback, err)
	}
	// Only this complete absence may satisfy the platform refund gate.
	if readback.MachinePresent == nil || *readback.MachinePresent || readback.CVMStatus != "NOT_FOUND" {
		t.Fatalf("absence facts=%#v", readback)
	}
}

func TestTencentComputeDestroyStatusKeepsIdentityDriftAndSdkFailureFailing(t *testing.T) {
	allocation := computeDestroyReadbackAllocation("compute-readback-failures", tencentComputeDestroyPhaseAttempted, "1")
	t.Run("identity drift", func(t *testing.T) {
		provider := NewTencentProvider()
		provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
			response := computeDestroyStatusResponse(request, false, "NOT_FOUND", "NOT_FOUND")
			response.ProviderData["machineName"] = "machine-other"
			response.ProviderData["privateIp"] = "10.0.0.99"
			return response, nil
		}
		readback, err := provider.ReadComputeDestroyStatus(context.Background(), allocation)
		if err == nil || readback.DestroyState != "" || validTencentComputeAbsenceEvidence(readback) {
			t.Fatalf("identity drift readback=%#v err=%v", readback, err)
		}
	})
	t.Run("sdk readback failure", func(t *testing.T) {
		provider := NewTencentProvider()
		provider.provision = func(_ context.Context, _ provisionerRequest) (provisionerResponse, error) {
			return provisionerResponse{OK: false, ErrorCode: "tencent_describe_cvm_failed", Message: "Tencent CVM readback failed", Retryable: true, MutationCount: 0}, nil
		}
		readback, err := provider.ReadComputeDestroyStatus(context.Background(), allocation)
		if err == nil || readback.DestroyState != "" || validTencentComputeAbsenceEvidence(readback) {
			t.Fatalf("sdk failure readback=%#v err=%v", readback, err)
		}
	})
}

// contractsComputeDestroyState names the expected classification for a readback
// that may still be waiting: with no dispatched terminate the owning operation
// may still delete, otherwise the retry is readback-only.
func contractsComputeDestroyState(t *testing.T, noDispatch bool) string {
	t.Helper()
	if noDispatch {
		return contracts.WorkspaceDeleteOutcomePendingRetry
	}
	return contracts.WorkspaceDeleteOutcomeUnconfirmedSend
}
