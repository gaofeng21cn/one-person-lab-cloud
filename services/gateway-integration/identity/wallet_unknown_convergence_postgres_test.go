package identity_test

import (
	"testing"

	api "opl-cloud/packages/contracts/go/api"
)

// walletDebitCommand is one original charge for the coordination fixture.
func walletDebitCommand(step string) *api.WalletDebitCommand {
	return &api.WalletDebitCommand{Context: grantedCaller(step, "grant-workspace"), WorkspaceId: "ws-1", ObligationId: "op-1", QuoteAcceptanceId: "qa-1", AmountUsdMicros: 1_000_000, Currency: "USD"}
}

func walletCoordination(t *testing.T, stub *gatewayWalletStub) (api.GatewayCoordinationClient, api.GatewayCoordinationClient) {
	t.Helper()
	_, tenantDB, cookie, console, workspace := coordinationSystem(t, stub)
	issueGrant(t, tenantDB, "grant-workspace", "op-1", "ws-1", "createworkspace")
	if _, err := console.BindWallet(t.Context(), &api.WalletBindingCommand{Context: platformCaller("bind", cookie), TargetTenantId: "tenant-coord", BillingSub2ApiUserId: "77", AuthorizationReceiptId: "auth-receipt-1"}); err != nil {
		t.Fatalf("bind wallet: %v", err)
	}
	return console, workspace
}

// TestWalletUnknownConvergesToConfirmedByOriginalReadback proves an ACK loss on a
// charge the wallet actually committed converges to confirmed by reading the
// native history under the original business code, without issuing a second
// balance POST.
func TestWalletUnknownConvergesToConfirmedByOriginalReadback(t *testing.T) {
	stub := &gatewayWalletStub{applyThenFail: true}
	_, workspace := walletCoordination(t, stub)
	command := walletDebitCommand("charge")
	first, err := workspace.Debit(t.Context(), command)
	if err != nil {
		t.Fatalf("debit: %v", err)
	}
	if first.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN {
		t.Fatalf("ACK loss was not recorded as unknown: %v", first)
	}
	if stub.adjustments != 1 {
		t.Fatalf("adjustments after ACK loss: %d", stub.adjustments)
	}
	// The wallet now answers the same original code: the operation converges to
	// confirmed from native history, not from a second charge.
	stub.mu.Lock()
	stub.applyThenFail = false
	stub.mu.Unlock()
	recovered, err := workspace.Debit(t.Context(), command)
	if err != nil {
		t.Fatalf("converge: %v", err)
	}
	if recovered.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED || recovered.GetReceiptId() == "" {
		t.Fatalf("unknown charge did not converge to confirmed: %v", recovered)
	}
	if recovered.GetId() != first.GetId() {
		t.Fatalf("convergence changed the original operation identity: %s vs %s", first.GetId(), recovered.GetId())
	}
	if stub.adjustments != 1 {
		t.Fatalf("convergence re-issued the charge: adjustments=%d", stub.adjustments)
	}
	if stub.applied != 1 {
		t.Fatalf("native wallet effects=%d, want exactly one", stub.applied)
	}
	read, err := workspace.ReadWalletAction(t.Context(), &api.WalletReadbackRequest{Context: grantedCaller("read", "grant-workspace"), OriginalIdempotencyKey: command.GetContext().GetIdempotencyKey()})
	if err != nil || read.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED {
		t.Fatalf("readback did not retain the converged answer: %v %v", read, err)
	}
}

// TestWalletUnknownConvergesToRejectedByOriginalReadback proves a charge the
// wallet refused is recorded as rejected with its own cause, is never re-issued,
// and the recorded rejection is authoritative for a replay.
func TestWalletUnknownConvergesToRejectedByOriginalReadback(t *testing.T) {
	stub := &gatewayWalletStub{refuseBalance: true}
	_, workspace := walletCoordination(t, stub)
	command := walletDebitCommand("charge")
	rejected, err := workspace.Debit(t.Context(), command)
	if err != nil {
		t.Fatalf("debit: %v", err)
	}
	if rejected.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REJECTED || rejected.GetErrorCode() == api.ErrorCodeEnum_ERROR_CODE_ENUM_UNSPECIFIED {
		t.Fatalf("refusal was not recorded as rejected with a cause: %v", rejected)
	}
	replay, err := workspace.Debit(t.Context(), command)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if replay.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REJECTED || replay.GetId() != rejected.GetId() {
		t.Fatalf("replay did not reuse the rejected original: %v vs %v", rejected, replay)
	}
	if stub.adjustments != 1 {
		t.Fatalf("a rejected charge was re-issued: adjustments=%d", stub.adjustments)
	}
	if stub.applied != 0 {
		t.Fatalf("a refused charge still changed the wallet: applied=%d", stub.applied)
	}
}

// TestWalletUnknownStaysExplicitlyUnknownWithoutNativeEvidence proves a genuine
// unknown stays explicitly unknown: when the native history cannot confirm the
// original code, the operation is neither confirmed nor rejected and no second
// charge is issued.
func TestWalletUnknownStaysExplicitlyUnknownWithoutNativeEvidence(t *testing.T) {
	stub := &gatewayWalletStub{applyThenFail: true}
	_, workspace := walletCoordination(t, stub)
	command := walletDebitCommand("charge")
	first, err := workspace.Debit(t.Context(), command)
	if err != nil {
		t.Fatalf("debit: %v", err)
	}
	if first.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN {
		t.Fatalf("ACK loss was not recorded as unknown: %v", first)
	}
	// The native audit row is not visible yet: the operation must stay unknown
	// rather than inventing convergence.
	stub.mu.Lock()
	for code := range stub.entries {
		delete(stub.entries, code)
	}
	stub.mu.Unlock()
	stillUnknown, err := workspace.Debit(t.Context(), command)
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if stillUnknown.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN {
		t.Fatalf("an unproven outcome was reported as %v", stillUnknown.GetStatus())
	}
	if stillUnknown.GetId() != first.GetId() {
		t.Fatalf("identity drift on unknown readback: %s vs %s", first.GetId(), stillUnknown.GetId())
	}
	if stub.adjustments != 1 || stub.applied != 1 {
		t.Fatalf("unknown readback changed the wallet: adjustments=%d applied=%d", stub.adjustments, stub.applied)
	}
}

// TestReadWalletActionConvergesUnknownByOriginalCode proves the Workspace
// recovery read (ReadWalletAction) resolves an unknown operation from native
// history under the original idempotency key, so a caller that reads back after
// ACK loss converges without issuing anything.
func TestReadWalletActionConvergesUnknownByOriginalCode(t *testing.T) {
	stub := &gatewayWalletStub{applyThenFail: true}
	_, workspace := walletCoordination(t, stub)
	command := walletDebitCommand("charge")
	first, err := workspace.Debit(t.Context(), command)
	if err != nil {
		t.Fatalf("debit: %v", err)
	}
	if first.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_UNKNOWN {
		t.Fatalf("ACK loss was not recorded as unknown: %v", first)
	}
	// No new Debit call: the readback path itself must converge.
	read, err := workspace.ReadWalletAction(t.Context(), &api.WalletReadbackRequest{Context: grantedCaller("read", "grant-workspace"), OriginalIdempotencyKey: command.GetContext().GetIdempotencyKey()})
	if err != nil {
		t.Fatalf("readback: %v", err)
	}
	if read.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED || read.GetReceiptId() == "" {
		t.Fatalf("readback did not converge the unknown charge: %v", read)
	}
	if stub.adjustments != 1 {
		t.Fatalf("readback issued another charge: adjustments=%d", stub.adjustments)
	}
}
