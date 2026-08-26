package contracts

// WorkspaceLaunchResumeAuthorizationProjection is the authorization subset of
// the operator Workspace Launch resume response.
type WorkspaceLaunchResumeAuthorizationProjection struct {
	ResumeAuthorization         *WorkspaceLaunchResumeAuthorization         `json:"resumeAuthorization"`
	ResumeAuthorizationReadback *WorkspaceLaunchResumeAuthorizationReadback `json:"resumeAuthorizationReadback"`
}

type WorkspaceLaunchResumeAuthorization struct {
	AuthorizationID                 string                                           `json:"authorizationId"`
	LaunchVersion                   int                                              `json:"launchVersion"`
	AuthorizedStage                 Stage                                            `json:"authorizedStage"`
	AuthorizedBy                    string                                           `json:"authorizedBy"`
	AuthorizedAt                    string                                           `json:"authorizedAt"`
	Reason                          string                                           `json:"reason"`
	MutationBudget                  int                                              `json:"mutationBudget"`
	IdempotentReplayBudget          int                                              `json:"idempotentReplayBudget,omitempty"`
	AuthoritativeReadBudget         int                                              `json:"authoritativeReadBudget,omitempty"`
	ReadbacksAtAuthorization        int                                              `json:"readbacksAtAuthorization,omitempty"`
	ReplacementWorkspaceImageDigest string                                           `json:"replacementWorkspaceImageDigest,omitempty"`
	AcceptanceBResumeExisting       *WorkspaceLaunchAcceptanceBResumeExistingBinding `json:"acceptanceBResumeExisting,omitempty"`
}

type WorkspaceLaunchResumeAuthorizationReadback struct {
	SchemaVersion             int                                              `json:"schemaVersion"`
	OperationID               string                                           `json:"operationId"`
	OperationVersion          int                                              `json:"operationVersion"`
	AuthorizationID           string                                           `json:"authorizationId"`
	AuthorizationVersion      int                                              `json:"authorizationVersion"`
	AuthorizedStage           Stage                                            `json:"authorizedStage"`
	AuthorizedBy              string                                           `json:"authorizedBy"`
	Status                    string                                           `json:"status"`
	ConsumedAt                string                                           `json:"consumedAt"`
	SingleUse                 bool                                             `json:"singleUse"`
	Attempt                   WorkspaceLaunchResumeAttemptReadback             `json:"attempt"`
	Convergence               WorkspaceLaunchResumeConvergenceReadback         `json:"convergence"`
	AcceptanceBResumeExisting *WorkspaceLaunchAcceptanceBResumeExistingBinding `json:"acceptanceBResumeExisting"`
}

type WorkspaceLaunchResumeAttemptReadback struct {
	Attempted           int    `json:"attempted"`
	Confirmed           int    `json:"confirmed"`
	Unknown             int    `json:"unknown"`
	Max                 int    `json:"max"`
	Status              string `json:"status"`
	IdempotencyKey      string `json:"idempotencyKey"`
	PendingReadbacks    int    `json:"pendingReadbacks"`
	MaxPendingReadbacks int    `json:"maxPendingReadbacks"`
}

type WorkspaceLaunchResumeConvergenceReadback struct {
	OperationStatus LaunchStatus `json:"operationStatus"`
	Stage           Stage        `json:"stage"`
	Version         int          `json:"version"`
}

type WorkspaceLaunchAcceptanceBResumeExistingBinding struct {
	SchemaVersion            int                                       `json:"schemaVersion"`
	ApprovalID               string                                    `json:"approvalId"`
	ApprovalSHA256           string                                    `json:"approvalSha256"`
	CanonicalCloudSHA        string                                    `json:"canonicalCloudSha"`
	CanonicalCloudTree       string                                    `json:"canonicalCloudTree"`
	DeployedCloudImageDigest string                                    `json:"deployedCloudImageDigest"`
	AuthoritativeState       StageState                                `json:"authoritativeState"`
	IdentityDigests          WorkspaceLaunchAcceptanceBIdentityDigests `json:"identityDigests"`
}

type WorkspaceLaunchAcceptanceBIdentityDigests struct {
	OperationIdentitySHA256 string `json:"operationIdentitySha256"`
	AccountIdentitySHA256   string `json:"accountIdentitySha256"`
	WorkspaceIdentitySHA256 string `json:"workspaceIdentitySha256"`
	QuoteIdentitySHA256     string `json:"quoteIdentitySha256"`
	KeyIdentitySHA256       string `json:"keyIdentitySha256"`
	DebitIdentitySHA256     string `json:"debitIdentitySha256"`
	ProviderIdentitySHA256  string `json:"providerIdentitySha256"`
}
