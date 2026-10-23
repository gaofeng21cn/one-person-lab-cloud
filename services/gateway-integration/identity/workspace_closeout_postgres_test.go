package identity_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/gateway-integration/identity"
)

func TestWorkspaceRefundsShareTheOriginalChargeLimit(t *testing.T) {
	stub := &gatewayWalletStub{}
	_, workspace := walletCoordination(t, stub)
	original, err := workspace.Debit(t.Context(), walletDebitCommand("original-closeout-charge"))
	if err != nil || original.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED {
		t.Fatalf("original charge: %v %v", original, err)
	}
	command := &api.WalletRefundCommand{Context: grantedCaller("first-closeout-refund", "grant-workspace"), WorkspaceId: "ws-1", OriginalWalletOperationId: original.GetId(), AmountUsdMicros: 1_000_000, RefundPolicyVersionId: contracts.WorkspaceRefundPolicyVersion, ConfirmedDeletionReceiptId: "receipt-original-deletion"}
	first, err := workspace.Refund(t.Context(), command)
	if err != nil || first.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED {
		t.Fatalf("original settlement: %v %v", first, err)
	}
	command.Context = grantedCaller("second-closeout-refund", "grant-workspace")
	if _, err = workspace.Refund(t.Context(), command); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a second settlement exceeded the original charge: %v", err)
	}
}

func TestWorkspaceRefundUsesTheOriginalWalletAfterRebinding(t *testing.T) {
	stub := &gatewayWalletStub{}
	_, tenantDB, cookie, console, workspace := coordinationSystem(t, stub)
	issueGrant(t, tenantDB, "grant-workspace", "op-1", "ws-1", "createworkspace")
	if _, err := console.BindWallet(t.Context(), &api.WalletBindingCommand{Context: platformCaller("bind-original", cookie), TargetTenantId: "tenant-coord", BillingSub2ApiUserId: "77", AuthorizationReceiptId: "auth-original"}); err != nil {
		t.Fatal(err)
	}
	original, err := workspace.Debit(t.Context(), walletDebitCommand("charge-before-rebind"))
	if err != nil || original.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED {
		t.Fatalf("charge: %v %v", original, err)
	}
	if _, err = console.BindWallet(t.Context(), &api.WalletBindingCommand{Context: platformCaller("bind-next", cookie), TargetTenantId: "tenant-coord", BillingSub2ApiUserId: "101", AuthorizationReceiptId: "auth-next", ExpectedBindingVersion: 1}); err != nil {
		t.Fatal(err)
	}
	refund, err := workspace.Refund(t.Context(), &api.WalletRefundCommand{Context: grantedCaller("refund-after-rebind", "grant-workspace"), WorkspaceId: "ws-1", OriginalWalletOperationId: original.GetId(), AmountUsdMicros: 1_000_000, RefundPolicyVersionId: contracts.WorkspaceRefundPolicyVersion, ConfirmedDeletionReceiptId: "receipt-original-deletion"})
	if err != nil || refund.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED {
		t.Fatalf("refund must settle against the original wallet: %v %v", refund, err)
	}
}

func TestWorkspaceKeyCloseoutFencesAnUnissuedOriginalCommand(t *testing.T) {
	issuer := newLifecycleIssuer()
	_, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	original := managedKeyCommand("original-late-key", "model-a")
	closed, err := workspace.DisableManagedKey(t.Context(), &api.ManagedKeyLifecycleCommand{Context: grantedCaller("close-original-key", "grant-workspace"), WorkspaceId: "ws-1", OriginalCreateIdempotencyKey: original.GetContext().GetIdempotencyKey()})
	if err != nil || closed.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || !closed.GetAbsenceConfirmed() {
		t.Fatalf("unissued original key was not closed: %v %v", closed, err)
	}
	if _, err = workspace.CreateManagedKey(t.Context(), original); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("late key creation survived Workspace closeout: %v", err)
	}
}

func TestClosedWorkspaceChargeAbsenceRejectsALateOriginalDebit(t *testing.T) {
	_, workspace := walletCoordination(t, &gatewayWalletStub{})
	original := walletDebitCommand("original-never-dispatched-charge")
	if _, err := workspace.DisableManagedKey(t.Context(), &api.ManagedKeyLifecycleCommand{Context: grantedCaller("close-before-charge", "grant-workspace"), WorkspaceId: "ws-1", OriginalCreateIdempotencyKey: "original-never-dispatched-key"}); err != nil {
		t.Fatal(err)
	}
	_, err := workspace.ReadWalletAction(t.Context(), &api.WalletReadbackRequest{Context: grantedCaller("read-fenced-original-charge", "grant-workspace"), OriginalIdempotencyKey: original.GetContext().GetIdempotencyKey(), WorkspaceId: "ws-1", ConfirmClosedAbsence: true})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("owner did not confirm the original charge absence: %v", err)
	}
	if _, err = workspace.Debit(t.Context(), original); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a late original charge crossed its absence fence: %v", err)
	}
}

func TestWorkspaceKeyCloseoutResolvesAConfirmedOriginalWithoutItsBindingResponse(t *testing.T) {
	issuer := newLifecycleIssuer()
	issuer.states[managedKeyExternalIDForTest("ws-1", []string{"model-a"})] = identity.ManagedKeyExternalActive
	_, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	original := managedKeyCommand("original-key-before-ack-loss", "model-a")
	created, err := workspace.CreateManagedKey(t.Context(), original)
	if err != nil {
		t.Fatal(err)
	}
	command := &api.ManagedKeyLifecycleCommand{Context: grantedCaller("close-key-without-response", "grant-workspace"), WorkspaceId: "ws-1", OriginalCreateIdempotencyKey: original.GetContext().GetIdempotencyKey()}
	for range 2 {
		closed, closeErr := workspace.DisableManagedKey(t.Context(), command)
		if closeErr != nil || closed.GetKeyBindingId() != created.GetKeyBindingId() || closed.GetState() != api.ManagedKeyLifecycleState_MANAGED_KEY_LIFECYCLE_STATE_DISABLED || closed.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED {
			t.Fatalf("original Key did not close from its own command: %v %v", closed, closeErr)
		}
	}
	if _, err = workspace.EnableManagedKey(t.Context(), lifecycleCommand("late-enable-after-closeout", created.GetKeyBindingId())); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a late Key enable crossed Workspace closeout: %v", err)
	}
	if issuer.setCalls != 1 {
		t.Fatalf("Key closeout replay caused %d external changes", issuer.setCalls)
	}
}

func TestWorkspaceCloseoutDoesNotHoldAConnectionWaitingForAnInFlightCharge(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	stub := &gatewayWalletStub{balanceEntered: entered, balanceRelease: release}
	_, workspace := walletCoordination(t, stub)
	finished := make(chan error, 1)
	go func() {
		_, err := workspace.Debit(t.Context(), walletDebitCommand("in-flight-original-charge"))
		finished <- err
	}()
	select {
	case <-entered:
	case <-t.Context().Done():
		t.Fatal(t.Context().Err())
	}
	bounded, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	_, err := workspace.DisableManagedKey(bounded, &api.ManagedKeyLifecycleCommand{Context: grantedCaller("close-in-flight-wallet", "grant-workspace"), WorkspaceId: "ws-1", OriginalCreateIdempotencyKey: "original-key"})
	cancel()
	close(release)
	debitErr := <-finished
	if debitErr != nil {
		t.Fatal(debitErr)
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("a busy closeout consumed the connection needed by the in-flight charge: %v", err)
	}
	if _, err = workspace.DisableManagedKey(t.Context(), &api.ManagedKeyLifecycleCommand{Context: grantedCaller("close-in-flight-wallet", "grant-workspace"), WorkspaceId: "ws-1", OriginalCreateIdempotencyKey: "original-key"}); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspaceKeyCloseoutCannotClaimAbsenceForAnUnmatchedOriginal(t *testing.T) {
	issuer := newLifecycleIssuer()
	_, workspace := managedKeyCoordination(t, issuer, &managedKeySecretStore{})
	if _, err := workspace.CreateManagedKey(t.Context(), managedKeyCommand("actual-original-key", "model-a")); err != nil {
		t.Fatal(err)
	}
	read, err := workspace.DisableManagedKey(t.Context(), &api.ManagedKeyLifecycleCommand{Context: grantedCaller("unresolved-original-key-closeout", "grant-workspace"), WorkspaceId: "ws-1", OriginalCreateIdempotencyKey: "another-original-key"})
	if err == nil || read.GetAbsenceConfirmed() {
		t.Fatalf("an existing Workspace key was relabelled absent under an unmatched original: %v %v", read, err)
	}
}
