package contracts

// WorkspacePurchaseEligibility is the retained Account owner's current answer
// for the live session's actor. It is read before a new Workspace order, never
// reconstructed from Tenant membership, wallet funds or an existing Workspace.
// The pointer preserves the distinction between an explicit false and an absent
// or unknown eligibility fact at the JSON boundary.
type WorkspacePurchaseEligibility struct {
	ActorID                  string `json:"actorId"`
	AccountID                string `json:"accountId"`
	WorkspacePurchaseEnabled *bool  `json:"workspacePurchaseEnabled"`
}
