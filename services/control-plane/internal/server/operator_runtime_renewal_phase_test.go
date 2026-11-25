package server

import (
	"context"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// renewalPhaseFixture builds one Workspace and the durable renewal operation
// that covers its current paid period. The operation is the owner's own record,
// so the observation publishes what the renewal executor actually persisted.
func renewalPhaseFixture(t *testing.T, workspaceID, accountID, ownerID string, operation workspaceRenewalOperation) (map[string]any, map[string]any) {
	t.Helper()
	workspace := workspaceLaunchUnitActivationProjectionRow(t, workspaceID, accountID, ownerID)
	operation.ID = "workspace-renewal-" + workspaceID
	operation.AccountID = accountID
	operation.OwnerUserID = ownerID
	operation.WorkspaceID = workspaceID
	operation.RequestHash = "renewal-request-" + workspaceID
	operation.PaidThrough = stringValue(workspace["paidThrough"])
	operation.CreatedAt = "2026-09-12T00:00:00Z"
	if operation.Status == "" {
		operation.Status = "claimed"
	}
	return workspace, workspaceRenewalOperationRow(operation)
}

// The settlement census has to tell a completed expiry from a renewal that
// still needs work. The published stage is the only place that distinction
// lives, so every durable shape of the renewal operation is pinned here.
func TestOperatorRuntimeRenewalPhaseDistinguishesACompletedExpiryFromOpenWork(t *testing.T) {
	for _, tc := range []struct {
		name      string
		operation workspaceRenewalOperation
		stage     string
	}{
		{
			name:      "an expiry that completed leaves no work",
			operation: workspaceRenewalOperation{Status: "expired_unpaid", ExpiryStatus: "expired_unpaid", ExpiryPhase: "complete"},
			stage:     contracts.WorkspaceRenewalStageExpiryComplete,
		},
		{
			name:      "an expiry still working leaves work",
			operation: workspaceRenewalOperation{Status: "expired_unpaid", ExpiryStatus: "past_due", ExpiryPhase: "suspend"},
			stage:     contracts.WorkspaceRenewalStageExpiryPending,
		},
		{
			name:      "a charge in flight leaves work",
			operation: workspaceRenewalOperation{Status: "debit_pending", Phase: "debit"},
			stage:     contracts.WorkspaceRenewalStageFinancialPending,
		},
		{
			name:      "a review decision leaves work",
			operation: workspaceRenewalOperation{Status: "manual_review", ErrorCode: "workspace_renewal_provider_truth_unavailable"},
			stage:     contracts.WorkspaceRenewalStageManualReview,
		},
		{
			name:      "a settled renewal leaves no work",
			operation: workspaceRenewalOperation{Status: "active", ReceiptID: "receipt-renewal"},
			stage:     contracts.WorkspaceRenewalStageSettled,
		},
		{
			name:      "a cancelled renewal leaves no work",
			operation: workspaceRenewalOperation{Status: "cancelled"},
			stage:     contracts.WorkspaceRenewalStageCancelled,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workspace, row := renewalPhaseFixture(t, "ws-renewal", "acct-admin", "usr-admin", tc.operation)
			stage, status, reasonCode, receiptID, expiryReceiptID := operatorRuntimeRenewalProgress(workspace, []map[string]any{row})
			if stage != tc.stage {
				t.Fatalf("stage=%q want %q", stage, tc.stage)
			}
			if status != tc.operation.Status {
				t.Fatalf("status=%q want %q", status, tc.operation.Status)
			}
			if reasonCode != tc.operation.ErrorCode {
				t.Fatalf("reason code=%q want %q", reasonCode, tc.operation.ErrorCode)
			}
			if receiptID != tc.operation.ReceiptID || expiryReceiptID != tc.operation.ExpiryReceiptID {
				t.Fatalf("receipts=%q %q want %q %q", receiptID, expiryReceiptID, tc.operation.ReceiptID, tc.operation.ExpiryReceiptID)
			}
		})
	}
}

// A renewal operation belongs to the paid period it renews from. An operation
// from an earlier period is not this Workspace's current renewal, so publishing
// it would report an obligation that already moved on.
func TestOperatorRuntimeRenewalProgressIgnoresAnOperationFromAnotherPeriod(t *testing.T) {
	workspace := workspaceLaunchUnitActivationProjectionRow(t, "ws-renewal", "acct-admin", "usr-admin")
	operation := workspaceRenewalOperation{Status: "debit_pending", Phase: "debit", PaidThrough: "2026-08-12T00:00:00Z"}
	operation.ID = "workspace-renewal-earlier"
	operation.AccountID = "acct-admin"
	operation.OwnerUserID = "usr-admin"
	operation.WorkspaceID = "ws-renewal"
	operation.RequestHash = "renewal-request-earlier"
	stage, status, reasonCode, receiptID, expiryReceiptID := operatorRuntimeRenewalProgress(workspace, []map[string]any{workspaceRenewalOperationRow(operation)})
	if stage != "" || status != "" || reasonCode != "" || receiptID != "" || expiryReceiptID != "" {
		t.Fatalf("another period was published: %q %q %q %q %q", stage, status, reasonCode, receiptID, expiryReceiptID)
	}
}

// A Workspace without a renewal operation publishes no renewal phase at all, so
// a reader never mistakes an absent operation for a completed one.
func TestOperatorRuntimeRenewalProgressPublishesNothingWithoutAnOperation(t *testing.T) {
	workspace := workspaceLaunchUnitActivationProjectionRow(t, "ws-renewal", "acct-admin", "usr-admin")
	stage, status, reasonCode, receiptID, expiryReceiptID := operatorRuntimeRenewalProgress(workspace, nil)
	if stage != "" || status != "" || reasonCode != "" || receiptID != "" || expiryReceiptID != "" {
		t.Fatalf("no operation published a renewal phase: %q %q %q %q %q", stage, status, reasonCode, receiptID, expiryReceiptID)
	}
}

// The published phase has to reach the observation itself, because the census
// reads the observation and not the renewal executor's internals.
func TestOperatorRuntimeObservationPublishesTheRenewalPhase(t *testing.T) {
	store := newMemoryTableStore()
	workspace, row := renewalPhaseFixture(t, "ws", "acct-admin", "usr-admin", workspaceRenewalOperation{
		Status:          "expired_unpaid",
		ExpiryStatus:    "expired_unpaid",
		ExpiryPhase:     "complete",
		ReceiptID:       "receipt-renewal",
		ExpiryReceiptID: "receipt-expiry",
	})
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	fabric := &runtimeHealthSummaryFabric{observations: operatorRuntimeFixtureObservations(1)}
	fabric.observations.Items[0] = contracts.RuntimeObservation{
		ObjectRef: "object-0", AccountID: "acct-admin", WorkspaceID: "ws", RuntimeID: "runtime-fabric",
		Ownership: contracts.RuntimeOwnershipVerified, DesiredState: contracts.ResourceObservedRunning, ObservedState: contracts.ResourceObservedRunning,
	}
	result, err := (&controlPlaneServer{tables: store}).operatorRuntimeObservations(context.Background(), controlplane.NewService(fakeLedgerClient{}, fabric, newOperatorProjectionClient()), operatorProjectionTime)
	if err != nil || len(result.Items) != 1 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	item := result.Items[0]
	if item.RenewalStage != contracts.WorkspaceRenewalStageExpiryComplete {
		t.Fatalf("renewal stage=%q", item.RenewalStage)
	}
	if item.RenewalStatus != "expired_unpaid" {
		t.Fatalf("renewal status=%q", item.RenewalStatus)
	}
	if item.RenewalReceiptID != "receipt-renewal" || item.RenewalExpiryReceiptID != "receipt-expiry" {
		t.Fatalf("renewal receipts=%q %q", item.RenewalReceiptID, item.RenewalExpiryReceiptID)
	}
}
