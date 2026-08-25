package contracts

// BlockReason represents why a runtime stage is blocked (from Fabric diagnostics).
type BlockReason string

const (
	BlockNone                    BlockReason = "none"
	BlockImageNotReady           BlockReason = "runtime_image_not_ready"
	BlockStorageNotReady         BlockReason = "runtime_storage_not_ready"
	BlockDeploymentNotReady      BlockReason = "runtime_deployment_not_ready"
	BlockNetworkNotReady         BlockReason = "runtime_network_not_ready"
	BlockCredentialsNotReady     BlockReason = "runtime_credentials_not_ready"
	BlockIsolationNotReady       BlockReason = "runtime_isolation_not_ready"
	BlockAuthoritativeReadFailed BlockReason = "runtime_authoritative_read_failed"
)
