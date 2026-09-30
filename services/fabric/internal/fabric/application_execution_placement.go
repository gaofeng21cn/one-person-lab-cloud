package fabric

import api "opl-cloud/packages/contracts/go/api"

// ApplicationExecutionPlacement projects one confirmed resource readback into the
// infrastructure placement a Workspace application workload is scheduled onto:
// the exact node the workspace's prepaid compute landed on, the prepaid package
// and node pool that own it, the provider machine identity, and the prepaid
// storage claim name.
//
// It is a read projection of the same provider mutation that produced the
// execution binding, never a second resource authority: every value comes from
// the confirmed ComputeAllocation and StorageVolume Fabric already persisted. A
// readback that does not carry the placement facts projects nothing, so a
// consumer fails closed instead of scheduling onto a guessed node, package or
// claim.
func ApplicationExecutionPlacement(compute ComputeAllocation, volume StorageVolume) *api.ApplicationExecutionPlacement {
	placement := &api.ApplicationExecutionPlacement{
		ComputeNodeName:    compute.NodeName,
		ComputePackageId:   compute.PackageID,
		ComputeNodePoolId:  compute.NodePoolID,
		ComputeMachineName: compute.MachineName,
		ComputeInstanceId:  firstNonEmpty(compute.InstanceID, compute.CVMInstanceID),
		StoragePvcName:     StoragePVCName(volume),
	}
	if placement.ComputeNodeName == "" || placement.ComputePackageId == "" || placement.StoragePvcName == "" {
		return nil
	}
	return placement
}
