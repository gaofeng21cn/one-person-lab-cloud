package tkeapply

// Placement is the confirmed infrastructure placement one Workspace application
// workload is scheduled onto. Every field comes from the owner readbacks of the
// resource mutation: the execution binding names the compute allocation and the
// prepaid storage volume, and the placement projection names the exact node, the
// prepaid package, the node pool, the provider machine and the storage claim.
//
// The executor never re-derives a node, a package or a claim name from a caller
// claim, and it refuses to apply a workload for a placement the resources owner
// has not confirmed.
type Placement struct {
	AccountID       string
	WorkspaceID     string
	ComputeID       string
	StorageVolumeID string

	ComputeNodeName    string
	ComputePackageID   string
	ComputeNodePoolID  string
	ComputeMachineName string
	ComputeInstanceID  string
	StoragePVCName     string
}
