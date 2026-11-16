package clients

const WorkspaceLaunchFabricSchemaVersion = 1

type WorkspaceLaunchPreflightInput struct {
	SchemaVersion        int    `json:"schemaVersion"`
	LaunchOperationID    string `json:"launchOperationId"`
	AccountID            string `json:"accountId"`
	WorkspaceID          string `json:"workspaceId"`
	PackageID            string `json:"packageId"`
	SizeGB               int    `json:"sizeGb"`
	WorkspaceImageDigest string `json:"workspaceImageDigest"`
	ProvisioningMode     string `json:"provisioningMode,omitempty"`
	RequestHash          string `json:"requestHash"`
}

type WorkspaceLaunchPreflight struct {
	SchemaVersion      int    `json:"schemaVersion"`
	Available          bool   `json:"available"`
	Reason             string `json:"reason"`
	LaunchOperationID  string `json:"launchOperationId"`
	RequestHash        string `json:"requestHash"`
	ProviderProfileRef string `json:"providerProfileRef"`
	// BindingRef is retained as an internal Go name while the wire contract
	// uses the provider-specific identity explicitly.
	BindingRef string `json:"providerBindingRef"`
	SpecDigest string `json:"specDigest"`
}

type WorkspaceLaunchStageDiagnostic struct {
	SchemaVersion int                         `json:"schemaVersion"`
	Owner         string                      `json:"owner"`
	BlockReason   string                      `json:"blockReason"`
	ErrorCode     string                      `json:"errorCode,omitempty"`
	Retryable     bool                        `json:"retryable"`
	ObservedAt    string                      `json:"observedAt"`
	Checks        []WorkspaceLaunchStageCheck `json:"checks,omitempty"`
}

type WorkspaceLaunchStageCheck struct {
	Name    string         `json:"name"`
	OK      bool           `json:"ok"`
	Details map[string]any `json:"details,omitempty"`
}
