package contracts

import "testing"

// The renewal stage vocabulary is a closed set. Two stages sharing a value
// would make the published phase ambiguous, and an empty value would be
// indistinguishable from a Workspace that has no renewal operation at all.
func TestWorkspaceRenewalStagesAreDistinctAndNamed(t *testing.T) {
	seen := map[string]bool{}
	for _, stage := range []string{
		WorkspaceRenewalStageFinancialPending,
		WorkspaceRenewalStageExpiryPending,
		WorkspaceRenewalStageExpiryComplete,
		WorkspaceRenewalStageSettled,
		WorkspaceRenewalStageCancelled,
		WorkspaceRenewalStageManualReview,
	} {
		if stage == "" {
			t.Fatal("a renewal stage is empty")
		}
		if seen[stage] {
			t.Fatalf("renewal stage %q is published twice", stage)
		}
		seen[stage] = true
	}
}
