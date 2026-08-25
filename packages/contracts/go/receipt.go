package contracts

// ReceiptType represents a canonical receipt type stored in Ledger.
type ReceiptType string

const (
	ReceiptTypeWorkspacePurchased ReceiptType = "billing.workspace_purchased.v1"
	ReceiptTypeWorkspaceRenewed   ReceiptType = "billing.workspace_renewed.v1"
	ReceiptTypeWorkspaceExpired   ReceiptType = "billing.workspace_expired.v1"
	ReceiptTypeWorkspaceDeleted   ReceiptType = "workspace.deleted.v1"
	ReceiptTypeWorkspaceCreated   ReceiptType = "workspace.created"
	ReceiptTypeKeyRotated         ReceiptType = "workspace.gateway_key_rotated.v1"
	ReceiptTypeWalletAdjustment   ReceiptType = "gateway.wallet_adjustment.v1"
)
