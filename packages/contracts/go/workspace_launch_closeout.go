package contracts

// WorkspaceLaunchCloseoutInput binds failure closeout to the original admitted
// Launch. It carries no replacement resource identities or procurement authority.
type WorkspaceLaunchCloseoutInput struct {
	SchemaVersion      int    `json:"schemaVersion"`
	LaunchOperationID  string `json:"launchOperationId"`
	AccountID          string `json:"accountId"`
	WorkspaceID        string `json:"workspaceId"`
	ProviderProfileRef string `json:"providerProfileRef"`
	ProviderBindingRef string `json:"providerBindingRef"`
	SpecDigest         string `json:"specDigest"`
	IdempotencyKey     string `json:"idempotencyKey"`
}

// Frozen proves subsequent Ensure calls are rejected. Only absent together
// with Frozen proves resource closeout; pending, blocked and unknown never do.
// Successful observations always contain exactly runtime, secret, storage,
// ensure_compute_allocation and attachment. Consumers require all five absent
// before proceeding with a refund; a missing entry is never absence.
type WorkspaceLaunchCloseoutResult struct {
	SchemaVersion int                               `json:"schemaVersion"`
	Binding       WorkspaceLaunchCloseoutInput      `json:"binding"`
	Frozen        bool                              `json:"frozen"`
	State         string                            `json:"state"`
	Reason        string                            `json:"reason"`
	Resources     []WorkspaceLaunchCloseoutResource `json:"resources"`
}

type WorkspaceLaunchCloseoutResource struct {
	Stage       string `json:"stage"`
	OperationID string `json:"operationId"`
	ResourceID  string `json:"resourceId"`
	State       string `json:"state"`
}
