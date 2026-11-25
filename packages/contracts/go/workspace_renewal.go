package contracts

// Workspace renewal stages. A stage names the durable phase of the renewal
// operation that covers a Workspace's current paid period.
//
// The stages are a closed vocabulary and they carry the distinction the
// settlement census needs: financial_pending, expiry_pending and
// manual_review still leave work owed for the current paid period, while
// expiry_complete, settled and cancelled do not. A reader distinguishes them
// by the published value alone; it never has to interpret the renewal
// executor's internal phase encoding.
const (
	// WorkspaceRenewalStageFinancialPending means the financial leg — the charge
	// and the provider renewal — is still working.
	WorkspaceRenewalStageFinancialPending = "financial_pending"
	// WorkspaceRenewalStageExpiryPending means the expiry leg — the suspension of
	// the Workspace whose period ended unpaid — is still working.
	WorkspaceRenewalStageExpiryPending = "expiry_pending"
	// WorkspaceRenewalStageExpiryComplete means the expiry leg finished. The
	// period ended, the Workspace was suspended and nothing is owed for it.
	WorkspaceRenewalStageExpiryComplete = "expiry_complete"
	// WorkspaceRenewalStageSettled means the financial leg settled and no expiry
	// leg applies to the operation.
	WorkspaceRenewalStageSettled = "settled"
	// WorkspaceRenewalStageCancelled means the renewal was cancelled. No renewal
	// is owed for the current paid period.
	WorkspaceRenewalStageCancelled = "cancelled"
	// WorkspaceRenewalStageManualReview means the operation needs a review
	// decision before it can advance.
	WorkspaceRenewalStageManualReview = "manual_review"
)
