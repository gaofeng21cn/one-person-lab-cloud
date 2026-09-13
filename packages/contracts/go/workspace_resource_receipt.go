package contracts

// WorkspaceResourceReceiptExecution records purchased resources independently
// of applications installed later. Retained full Launch receipts keep their
// original runtime and Key identity fields.
type WorkspaceResourceReceiptExecution struct {
	OperationID         string                    `json:"operationId"`
	ResourceType        string                    `json:"resourceType"`
	ResourceID          string                    `json:"resourceId"`
	ComputeAllocationID string                    `json:"computeAllocationId"`
	StorageID           string                    `json:"storageId"`
	AttachmentID        string                    `json:"attachmentId"`
	ProvisioningMode    WorkspaceProvisioningMode `json:"provisioningMode"`
}

func (e WorkspaceResourceReceiptExecution) Fields() map[string]any {
	return map[string]any{"operationId": e.OperationID, "resourceType": e.ResourceType, "resourceId": e.ResourceID, "computeAllocationId": e.ComputeAllocationID, "storageId": e.StorageID, "attachmentId": e.AttachmentID, "provisioningMode": string(e.ProvisioningMode)}
}

// WorkspaceApplicationRetirementReceipt projects confirmed cleanup facts;
// Control Plane's retry cursors and credentials are not Ledger contract facts.
type WorkspaceApplicationRetirementReceipt struct {
	CurrentDeploymentID   string                                         `json:"currentDeploymentId,omitempty"`
	Runtimes              []WorkspaceApplicationRuntimeRetirementReceipt `json:"runtimes,omitempty"`
	Secrets               []WorkspaceApplicationSecretRetirementReceipt  `json:"secrets,omitempty"`
	RetainedGatewayKeyIDs []int64                                        `json:"retainedGatewayKeyIds,omitempty"`
}

type WorkspaceApplicationRuntimeRetirementReceipt struct {
	RuntimeID          string                                       `json:"runtimeId"`
	RuntimeOperationID string                                       `json:"runtimeOperationId"`
	State              string                                       `json:"state"`
	ImageRetirement    []WorkspaceApplicationRuntimeImageRetirement `json:"imageRetirement,omitempty"`
}

type WorkspaceApplicationSecretRetirementReceipt struct {
	SecretRef string `json:"secretRef"`
	Ownership string `json:"ownership"`
	State     string `json:"state"`
}
