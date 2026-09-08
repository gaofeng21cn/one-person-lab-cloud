package contracts

// ReceiptType represents a canonical receipt type stored in Ledger.
type ReceiptType string

// ReceiptLookupScope identifies the filters Ledger applied before pagination.
// It is returned for exact request and inclusive type lookups so an older server
// cannot turn an unsupported filter into confirmed absence or an incomplete bill.
type ReceiptLookupScope struct {
	AccountID            string `json:"accountId"`
	WorkspaceID          string `json:"workspaceId"`
	RequestID            string `json:"requestId"`
	Type                 string `json:"type"`
	TypePrefix           string `json:"typePrefix"`
	IncludeType          string `json:"includeType"`
	IncludeExecutionKind string `json:"includeExecutionKind"`
}

const (
	ReceiptTypeWorkspacePurchased ReceiptType = "billing.workspace_purchased.v1"
	ReceiptTypeWorkspaceRenewed   ReceiptType = "billing.workspace_renewed.v1"
	ReceiptTypeWorkspaceExpired   ReceiptType = "billing.workspace_expired.v1"
	ReceiptTypeWorkspaceDeleted   ReceiptType = "workspace.deleted.v1"
	ReceiptTypeWorkspaceCreated   ReceiptType = "workspace.created"
	ReceiptTypeKeyRotated         ReceiptType = "workspace.gateway_key_rotated.v1"
	ReceiptTypeWalletAdjustment   ReceiptType = "gateway.wallet_adjustment.v1"
)
