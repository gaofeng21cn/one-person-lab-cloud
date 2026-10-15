package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

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

// A successful Launch can retain an empty tag projection while the current
// ownership store still holds the exact active Machine claim.
func TestComputeOwnerReadbackUsesExactActiveOwnershipWithoutPersistingTags(t *testing.T) {
	for _, postgres := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "postgres-reopen"}[postgres], func(t *testing.T) {
			ctx := context.Background()
			fixture, provider, compute, _ := retainedTencentComputeTagsFixture(t, func(_ *ComputeAllocation, o *MachineOwnership) { *o = MachineOwnership{} })
			var store OperationStore = fixture
			var databaseURL string
			if postgres {
				databaseURL = fabricTestDatabaseURL(t)
				persisted, err := newTestPostgresOperationStore(databaseURL)
				if err != nil {
					t.Fatal(err)
				}
				for _, operation := range fixture.operation {
					if err := persisted.Append(ctx, operation); err != nil {
						t.Fatal(err)
					}
				}
				store = persisted
				t.Cleanup(func() { _ = persisted.client.Close() })
			}
			owner := MachineOwnership{ID: "current-owner", ResourceID: compute.ID, AccountID: compute.AccountID, WorkspaceID: compute.WorkspaceID,
				PackageID: compute.PackageID, NodePoolID: compute.NodePoolID, MachineID: compute.MachineName, InstanceID: compute.InstanceID,
				NodeName: compute.NodeName, Status: "claimed", ClaimedAt: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}
			if _, claimed, err := store.ClaimMachine(ctx, owner); err != nil || !claimed {
				t.Fatalf("claim=%t err=%v", claimed, err)
			}
			owner.Status = "active"
			if err := store.SaveMachineOwnership(ctx, owner); err != nil {
				t.Fatal(err)
			}
			if postgres {
				if err := store.(*PostgresOperationStore).client.Close(); err != nil {
					t.Fatal(err)
				}
				reopened, err := newTestPostgresOperationStore(databaseURL)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = reopened.client.Close() })
				store = reopened
			}
			service := NewServiceWithOperationStore(provider, store)
			before := cloneComputeAllocation(service.computes[compute.ID])
			operations, _ := store.List(ctx)
			originalBytes, _ := json.Marshal(operations)
			calls := 0
			provider.provision = func(_ context.Context, request provisionerRequest) (provisionerResponse, error) {
				if request.Action != "read_compute_destroy_status" {
					t.Fatal("readback attempted a mutation")
				}
				if request.Pool.ID != before.PoolID || request.Pool.NodePoolID != before.NodePoolID {
					t.Fatal("readback replaced retained pool identity")
				}
				calls++
				return computeDestroyStatusResponse(request, true, "RUNNING", "RUNNING"), nil
			}
			observed, err := service.ReadComputeDestroyStatus(ctx, compute.ID)
			if err != nil || calls != 1 || observed.ObservedAt == "" || observed.ReadbackID == "" ||
				!reflect.DeepEqual(observed.CostTags, oplCostTags(owner.AccountID, owner.WorkspaceID, owner.ResourceID, owner.ID)) {
				t.Fatalf("readback failed: err=%v calls=%d stamp=%q tags=%v", err, calls, observed.ObservedAt, observed.CostTags)
			}
			if !reflect.DeepEqual(before, service.computes[compute.ID]) {
				t.Fatal("readback persisted reconstructed tags or changed allocation")
			}
			after, _ := store.List(ctx)
			afterBytes, _ := json.Marshal(after)
			if string(originalBytes) != string(afterBytes) {
				t.Fatal("readback changed operation history")
			}
			persistedOwner, err := store.MachineOwnership(ctx, compute.ID)
			if err != nil || !reflect.DeepEqual(owner, persistedOwner) {
				t.Fatal("readback changed Machine ownership")
			}

		})
	}
}

type computeReadbackOwnershipStore struct {
	MachineOwnershipStore
	owner MachineOwnership
	err   error
}

func (store computeReadbackOwnershipStore) MachineOwnership(context.Context, string) (MachineOwnership, error) {
	return store.owner, store.err
}

func TestComputeOwnerReadbackRejectsUnprovenCurrentOwnership(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*MachineOwnership)
		err    error
		want   computeReadbackFailure
	}{
		{name: "missing", err: ErrMachineOwnershipNotFound, want: "compute_readback_ownership_missing"},
		{name: "store failure", err: errors.New("private database diagnostic"), want: "compute_readback_ownership_unavailable"},
		{name: "inactive", mutate: func(o *MachineOwnership) { o.Status = "claimed" }, want: "compute_readback_ownership_inactive"},
		{name: "missing claim id", mutate: func(o *MachineOwnership) { o.ID = "" }, want: "compute_readback_ownership_invalid"},
		{name: "missing claim time", mutate: func(o *MachineOwnership) { o.ClaimedAt = time.Time{} }, want: "compute_readback_ownership_invalid"},
		{name: "resource", mutate: func(o *MachineOwnership) { o.ResourceID += "-other" }, want: "compute_readback_ownership_resource"},
		{name: "account", mutate: func(o *MachineOwnership) { o.AccountID += "-other" }, want: "compute_readback_ownership_account"},
		{name: "workspace", mutate: func(o *MachineOwnership) { o.WorkspaceID += "-other" }, want: "compute_readback_ownership_workspace"},
		{name: "package", mutate: func(o *MachineOwnership) { o.PackageID += "-other" }, want: "compute_readback_ownership_package"},
		{name: "node pool", mutate: func(o *MachineOwnership) { o.NodePoolID += "-other" }, want: "compute_readback_ownership_node_pool"},
		{name: "machine", mutate: func(o *MachineOwnership) { o.MachineID += "-other" }, want: "compute_readback_ownership_machine"},
		{name: "instance", mutate: func(o *MachineOwnership) { o.InstanceID += "-other" }, want: "compute_readback_ownership_instance"},
		{name: "node", mutate: func(o *MachineOwnership) { o.NodeName += "-other" }, want: "compute_readback_ownership_node"},
	} {
		t.Run(test.name, func(t *testing.T) {
			store, provider, compute, _ := retainedTencentComputeTagsFixture(t, func(_ *ComputeAllocation, o *MachineOwnership) { *o = MachineOwnership{} })
			service := NewServiceWithOperationStore(provider, store)
			owner := MachineOwnership{ID: "current-owner", ResourceID: compute.ID, AccountID: compute.AccountID, WorkspaceID: compute.WorkspaceID,
				PackageID: compute.PackageID, NodePoolID: compute.NodePoolID, MachineID: compute.MachineName, InstanceID: compute.InstanceID,
				NodeName: compute.NodeName, Status: "active", ClaimedAt: time.Now().UTC()}
			if test.mutate != nil {
				test.mutate(&owner)
			}
			service.machineOwnership = computeReadbackOwnershipStore{owner: owner, err: test.err}
			calls := 0
			provider.provision = func(context.Context, provisionerRequest) (provisionerResponse, error) {
				calls++
				return provisionerResponse{}, nil
			}
			observed, err := service.ReadComputeDestroyStatus(context.Background(), compute.ID)
			if !errors.Is(err, test.want) || calls != 0 || observed.ObservedAt != "" || observed.ReadbackID != "" || len(service.computes[compute.ID].CostTags) != 0 {
				t.Fatalf("readback accepted unproven ownership: err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestComputeOwnerReadbackCurrentClaimCannotOverrideCanonicalConflict(t *testing.T) {
	for _, conflict := range []string{"duplicate", "missing", "original ownership", "stable identity"} {
		t.Run(conflict, func(t *testing.T) {
			store, provider, compute, _ := retainedTencentComputeTagsFixture(t, func(_ *ComputeAllocation, o *MachineOwnership) {
				*o = MachineOwnership{}
				if conflict == "original ownership" {
					o.ID = "conflicting-original-owner"
				}
			})
			if conflict == "duplicate" || conflict == "missing" {
				for i, operation := range store.operation {
					if operation.Action != "ensure_compute_allocation" {
						continue
					}
					if conflict == "missing" {
						store.operation = append(store.operation[:i], store.operation[i+1:]...)
					} else {
						duplicate := operation
						duplicate.ID += "-duplicate"
						store.operation = append(store.operation, duplicate)
					}
					break
				}
			}
			service := NewServiceWithOperationStore(provider, store)
			if conflict == "stable identity" {
				current := service.computes[compute.ID]
				current.ProviderData["vpcId"] = "other-vpc"
				service.computes[compute.ID] = current
			}
			service.machineOwnership = computeReadbackOwnershipStore{owner: MachineOwnership{ID: "current-owner", ResourceID: compute.ID, AccountID: compute.AccountID,
				WorkspaceID: compute.WorkspaceID, PackageID: compute.PackageID, NodePoolID: compute.NodePoolID, MachineID: compute.MachineName,
				InstanceID: compute.InstanceID, NodeName: compute.NodeName, Status: "active", ClaimedAt: time.Now().UTC()}}
			calls := 0
			provider.provision = func(context.Context, provisionerRequest) (provisionerResponse, error) {
				calls++
				return provisionerResponse{}, nil
			}
			observed, err := service.ReadComputeDestroyStatus(context.Background(), compute.ID)
			var reason computeReadbackFailure
			if !errors.As(err, &reason) || calls != 0 || observed.ReadbackID != "" {
				t.Fatalf("canonical conflict was bypassed: err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestTencentComputeReadbackIdentityFailureIsSpecificAndMutationGuardUnchanged(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*ComputeAllocation)
		want   computeReadbackFailure
	}{
		{"resource", func(c *ComputeAllocation) { c.ID = "" }, "compute_readback_identity_resource"},
		{"account", func(c *ComputeAllocation) { c.AccountID = "" }, "compute_readback_identity_account"},
		{"workspace", func(c *ComputeAllocation) { c.WorkspaceID = "" }, "compute_readback_identity_workspace"},
		{"package", func(c *ComputeAllocation) { c.PackageID = "" }, "compute_readback_identity_package"},
		{"provider", func(c *ComputeAllocation) { c.Provider = "other" }, "compute_readback_identity_provider"},
		{"pool", func(c *ComputeAllocation) { c.PoolID = "" }, "compute_readback_identity_pool"},
		{"node pool", func(c *ComputeAllocation) { c.NodePoolID = "" }, "compute_readback_identity_node_pool"},
		{"machine", func(c *ComputeAllocation) { c.MachineName = "" }, "compute_readback_identity_machine"},
		{"node", func(c *ComputeAllocation) { c.NodeName = "" }, "compute_readback_identity_node"},
		{"private address", func(c *ComputeAllocation) { c.PrivateIP = "" }, "compute_readback_identity_private_ip"},
		{"instance", func(c *ComputeAllocation) { c.CVMInstanceID = "ins-other" }, "compute_readback_identity_instance"},
		{"instance type", func(c *ComputeAllocation) { c.ProviderData["instanceType"] = "other" }, "compute_readback_identity_instance_type"},
		{"zone", func(c *ComputeAllocation) { c.ProviderData["zone"] = "other" }, "compute_readback_identity_zone"},
		{"cluster", func(c *ComputeAllocation) { delete(c.ProviderData, "clusterId") }, "compute_readback_identity_cluster"},
		{"region", func(c *ComputeAllocation) { delete(c.ProviderData, "region") }, "compute_readback_identity_region"},
		{"account tag", func(c *ComputeAllocation) { delete(c.CostTags, "opl_account_id") }, "compute_readback_identity_account_tag"},
		{"workspace tag", func(c *ComputeAllocation) { delete(c.CostTags, "opl_workspace_id") }, "compute_readback_identity_workspace_tag"},
		{"resource tag", func(c *ComputeAllocation) { delete(c.CostTags, "opl_resource_id") }, "compute_readback_identity_resource_tag"},
		{"operation tag", func(c *ComputeAllocation) { delete(c.CostTags, "opl_operation_id") }, "compute_readback_identity_operation_tag"},
		{"machine type", func(c *ComputeAllocation) { c.ProviderData["machineType"] = "other" }, "compute_readback_identity_machine_type"},
		{"cpu", func(c *ComputeAllocation) { c.ProviderData["cpu"] = "0" }, "compute_readback_identity_cpu"},
		{"memory", func(c *ComputeAllocation) { c.ProviderData["memoryGb"] = "invalid" }, "compute_readback_identity_memory"},
	} {
		t.Run(test.name, func(t *testing.T) {
			allocation := canonicalTencentComputeDestroyFixture()
			test.mutate(&allocation)
			provider := NewTencentProvider()
			calls := 0
			provider.provision = func(context.Context, provisionerRequest) (provisionerResponse, error) {
				calls++
				return provisionerResponse{}, nil
			}
			observed, err := provider.ReadComputeDestroyStatus(context.Background(), allocation)
			if !errors.Is(err, test.want) || calls != 0 || !reflect.DeepEqual(observed, allocation) {
				t.Fatalf("unexpected identity result: err=%v calls=%d", err, calls)
			}
			_, mutationErr := provider.admitTencentComputeDestroy(context.Background(), allocation)
			const mutationIdentityError = "compute_allocation_destroy_identity_required"
			if mutationErr == nil || mutationErr.Error() != mutationIdentityError {
				t.Fatalf("mutation admission changed: %v", mutationErr)
			}
		})
	}
}
