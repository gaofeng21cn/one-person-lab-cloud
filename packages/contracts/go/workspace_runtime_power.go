package contracts

// Runtime power changes preserve the original paid Workspace's resource identity.
type WorkspaceRuntimePowerInput struct {
	SchemaVersion      int    `json:"schemaVersion"`
	AccountID          string `json:"accountId"`
	WorkspaceID        string `json:"workspaceId"`
	RuntimeID          string `json:"runtimeId"`
	RuntimeOperationID string `json:"runtimeOperationId"`
	PaidThrough        string `json:"paidThrough"`
	DesiredState       string `json:"desiredState"`
	IdempotencyKey     string `json:"idempotencyKey"`
}

type WorkspaceRuntimePowerResult struct {
	SchemaVersion int                        `json:"schemaVersion"`
	Binding       WorkspaceRuntimePowerInput `json:"binding"`
	State         string                     `json:"state"`
	Reason        string                     `json:"reason"`
}
