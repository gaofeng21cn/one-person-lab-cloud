package contracts

const ReceiptTypeWorkspaceClosed ReceiptType = "billing.workspace_closed.v1"

// These fields cross the Control Plane / Ledger boundary. The receipt attests
// completed failure closeout; it is not a successful Workspace purchase.
type WorkspaceLaunchCloseoutCharge struct {
	Code            string `json:"code"`
	UserID          int64  `json:"userId"`
	ChargeUSDMicros int64  `json:"chargeUsdMicros"`
	Status          string `json:"status"`
}

type WorkspaceLaunchCloseoutReceiptExecution struct {
	OperationID        string                         `json:"operationId"`
	AuthorizationID    string                         `json:"authorizationId"`
	AuthorizedAt       string                         `json:"authorizedAt"`
	Reason             string                         `json:"reason"`
	Outcome            string                         `json:"outcome"`
	ChargeConfirmation *WorkspaceLaunchCloseoutCharge `json:"chargeConfirmation,omitempty"`
	RefundedUSDMicros  int64                          `json:"refundedUsdMicros"`
	RefundOperationID  string                         `json:"refundOperationId"`
	FrozenAt           string                         `json:"frozenAt"`
	KeyRevokedAt       string                         `json:"keyRevokedAt"`
	ResourcesAbsentAt  string                         `json:"resourcesAbsentAt"`
	CompletedAt        string                         `json:"completedAt"`
}

type WorkspaceLaunchCloseoutReceiptCost struct {
	Currency        string `json:"currency"`
	PriceVersion    string `json:"priceVersion"`
	ChargeUSDMicros int64  `json:"chargeUsdMicros"`
	PeriodStart     string `json:"periodStart,omitempty"`
	PaidThrough     string `json:"paidThrough,omitempty"`
}
