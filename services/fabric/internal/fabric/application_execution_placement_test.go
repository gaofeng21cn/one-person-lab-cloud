package fabric

import "testing"

// TestApplicationExecutionPlacementProjectsOnlyConfirmedFacts proves the workload
// placement is a read projection of the confirmed compute and storage readback,
// and that an incomplete readback projects nothing so a consumer fails closed
// instead of scheduling onto a guessed node, package or claim.
func TestApplicationExecutionPlacementProjectsOnlyConfirmedFacts(t *testing.T) {
	compute := ComputeAllocation{
		ID: "ca-1", AccountID: "acct", WorkspaceID: "ws", PackageID: "pkg-basic", NodePoolID: "np-basic",
		MachineName: "machine-ca-1", NodeName: "10.66.0.10", InstanceID: "ins-ca-1",
	}
	volume := StorageVolume{ID: "vol-1", WorkspaceID: "ws", ProviderResourceID: "pvc/pvc-1-data"}
	placement := ApplicationExecutionPlacement(compute, volume)
	if placement == nil {
		t.Fatal("a confirmed readback projected no placement")
	}
	if placement.GetComputeNodeName() != "10.66.0.10" || placement.GetComputePackageId() != "pkg-basic" || placement.GetComputeNodePoolId() != "np-basic" ||
		placement.GetComputeMachineName() != "machine-ca-1" || placement.GetComputeInstanceId() != "ins-ca-1" || placement.GetStoragePvcName() != "pvc-1-data" {
		t.Fatalf("placement=%v", placement)
	}
	if got := ApplicationExecutionPlacement(compute, StorageVolume{ID: "vol-1"}); got != nil {
		t.Fatalf("storage without a confirmed claim projected a placement: %v", got)
	}
	if got := ApplicationExecutionPlacement(ComputeAllocation{NodeName: "10.66.0.10"}, volume); got != nil {
		t.Fatalf("compute without a confirmed package projected a placement: %v", got)
	}
	if got := ApplicationExecutionPlacement(ComputeAllocation{PackageID: "pkg-basic"}, volume); got != nil {
		t.Fatalf("compute without a confirmed node projected a placement: %v", got)
	}
}
