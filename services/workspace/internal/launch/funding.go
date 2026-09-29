package launch

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// fundingEvidence returns the confirmed charge evidence that releases resource
// provisioning for this original order, or "" while its funding owner has not
// answered and the order must stay awaiting that dependency. stopped reports that
// the funding owner explicitly refused this order, so the downstream must not run.
//
// A Local no-charge order is funded by the Ledger receipt its own owner records
// for the same obligation. Any other order is funded by the Gateway wallet charge
// for the same obligation, which is the only authority that may move customer
// money. Treating an unanswered charge as absent, free or already refunded would
// misrepresent the obligation the customer accepted.
func (s *Service) fundingEvidence(ctx context.Context, op ownerstore.Operation, token string, accepted *api.QuoteAcceptance, result *orderResult) (string, bool, error) {
	if localNoCharge(accepted) {
		if s.Ledger == nil {
			return "", false, nil
		}
		receipt, err := s.zeroChargeReceipt(ctx, op, token, accepted, result)
		if err != nil {
			return "", false, err
		}
		return receipt.GetId(), false, nil
	}
	if s.Gateway == nil {
		return "", false, nil
	}
	return s.walletCharge(ctx, op, token, accepted, result)
}

// walletChargeCommand is the immutable charge for one accepted order. Its
// obligation is the durability key: a retry, a second worker or a lost response
// all replay this same command identity instead of creating another charge.
func walletChargeCommand(op ownerstore.Operation, grant string, accepted *api.QuoteAcceptance) *api.WalletDebitCommand {
	return &api.WalletDebitCommand{
		Context:           continuation(op, grant, "wallet_charge"),
		WorkspaceId:       op.ResourceID,
		ObligationId:      op.ID,
		QuoteAcceptanceId: accepted.GetAcceptanceId(),
		AmountUsdMicros:   accepted.GetQuote().GetTotalUsdMicros(),
		Currency:          "USD",
	}
}

// walletCharge resolves the one charge for this original obligation. A charge that
// already answered is never re-charged: the recorded answer is reused, an
// unresolved answer is only read back by its original code, and the charge is
// issued at most once per resolution attempt. An outcome that stays unknown leaves
// the obligation pending rather than refunded, because an unknown charge may still
// have moved money.
func (s *Service) walletCharge(ctx context.Context, op ownerstore.Operation, token string, accepted *api.QuoteAcceptance, result *orderResult) (string, bool, error) {
	command := walletChargeCommand(op, result.GrantID, accepted)
	if len(result.WalletDebitCommand) == 0 {
		result.WalletDebitCommand = wire(command)
	} else {
		stored := &api.WalletDebitCommand{}
		if protojson.Unmarshal(result.WalletDebitCommand, stored) != nil || !proto.Equal(stored, command) {
			return "", false, s.failedCall(ctx, op, token, "wallet_charge", "funding", *result, status.Error(codes.DataLoss, "wallet command differs from its original execution"))
		}
	}
	recorded, hasRecord, err := readWalletCharge(op, accepted, result)
	if err != nil {
		return "", false, err
	}
	if hasRecord && walletChargeSettled(recorded.GetStatus()) {
		return s.finishWalletCharge(ctx, op, token, recorded, result)
	}
	if err = s.beginStep(ctx, op, token, "wallet_charge", 3, "gateway", "resource_preflight", command); err != nil {
		return "", false, err
	}
	var observed *api.WalletOperation
	if !hasRecord {
		// The obligation has no answer yet, so its original command may be issued.
		charged, chargeErr := s.Gateway.Debit(ctx, command)
		if chargeErr != nil {
			if !walletReadCanRecover(chargeErr) {
				return "", false, s.failedCall(ctx, op, token, "wallet_charge", "funding", *result, chargeErr)
			}
		} else {
			if err = validateWalletCharge(op, accepted, charged); err != nil {
				return "", false, s.failedCall(ctx, op, token, "wallet_charge", "funding", *result, err)
			}
			observed = charged
		}
	}
	if observed == nil || !walletChargeSettled(observed.GetStatus()) {
		// The charge may have committed before its response was lost, so the
		// original action is read back by its original code instead of re-issued.
		read, readErr := s.Gateway.ReadWalletAction(ctx, &api.WalletReadbackRequest{Context: continuation(op, result.GrantID, "read_wallet_charge"), OriginalIdempotencyKey: command.GetContext().GetIdempotencyKey()})
		if readErr != nil {
			return "", false, s.failedCall(ctx, op, token, "wallet_charge", "funding", *result, readErr)
		}
		if err = validateWalletCharge(op, accepted, read); err != nil {
			return "", false, s.failedCall(ctx, op, token, "wallet_charge", "funding", *result, err)
		}
		observed = read
	}
	return s.finishWalletCharge(ctx, op, token, observed, result)
}

// readWalletCharge decodes the charge already recorded for this obligation. A
// recorded charge that does not belong to this order is unreadable evidence, not a
// charge to ignore.
func readWalletCharge(op ownerstore.Operation, accepted *api.QuoteAcceptance, result *orderResult) (*api.WalletOperation, bool, error) {
	if len(result.WalletOperation) == 0 {
		return nil, false, nil
	}
	recorded := &api.WalletOperation{}
	if protojson.Unmarshal(result.WalletOperation, recorded) != nil {
		return nil, false, status.Error(codes.DataLoss, "stored wallet charge is invalid")
	}
	if err := validateWalletCharge(op, accepted, recorded); err != nil {
		return nil, false, status.Error(codes.DataLoss, "stored wallet charge is invalid")
	}
	return recorded, true, nil
}

// finishWalletCharge records what the wallet authority answered for this original
// obligation. Only a confirmed charge names the receipt that releases resources;
// an explicit refusal stops the downstream, and any other answer stays an unknown
// obligation that a later pass reads back instead of charging again.
func (s *Service) finishWalletCharge(ctx context.Context, op ownerstore.Operation, token string, charged *api.WalletOperation, result *orderResult) (string, bool, error) {
	result.WalletOperation = wire(charged)
	switch charged.GetStatus() {
	case api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED:
		if charged.GetReceiptId() == "" || charged.GetCreatedAt() == nil || charged.CreatedAt.CheckValid() != nil {
			return "", false, s.failedCall(ctx, op, token, "wallet_charge", "funding", *result, status.Error(codes.DataLoss, "confirmed wallet charge has no receipt evidence"))
		}
		if err := s.checkpoint(ctx, op, token, "wallet_charge", "resource_preflight", "running", "confirmed", charged.GetReceiptId(), "", *result); err != nil {
			return "", false, err
		}
		return charged.GetReceiptId(), false, nil
	case api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REJECTED:
		// An explicit refusal stops the downstream. The cause is the wallet's own
		// code, so an unnamed refusal is unreadable evidence rather than a guess.
		if !validErrorCode(charged.GetErrorCode()) {
			return "", false, s.failedCall(ctx, op, token, "wallet_charge", "funding", *result, status.Error(codes.DataLoss, "rejected wallet charge names no cause"))
		}
		if err := s.checkpoint(ctx, op, token, "wallet_charge", "resource_preflight", "needs_attention", "rejected", charged.GetId(), errorCodeText(charged.GetErrorCode()), *result); err != nil {
			return "", false, err
		}
		return "", true, nil
	default:
		// Requested, unknown or unspecified: the charge may still be in flight, so
		// the obligation stays pending and is only read back, never refunded.
		if err := s.checkpoint(ctx, op, token, "wallet_charge", "resource_preflight", "awaiting_confirmation", "unknown", charged.GetId(), "DEPENDENCY_UNAVAILABLE", *result); err != nil {
			return "", false, err
		}
		return "", false, nil
	}
}

func walletChargeSettled(status api.WalletOperationStatusEnum) bool {
	return status == api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED || status == api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_REJECTED
}

// validateWalletCharge binds one wallet action to the original order. Another
// workspace, amount or action kind is a different obligation even when it is
// otherwise well formed, so it can never release this order's resources.
func validateWalletCharge(op ownerstore.Operation, accepted *api.QuoteAcceptance, charged *api.WalletOperation) error {
	if localNoCharge(accepted) {
		return status.Error(codes.DataLoss, "a Local no-charge order has no wallet charge")
	}
	if charged.GetId() == "" || charged.GetWorkspaceId() != op.ResourceID || charged.GetKind() != api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE || charged.GetAmountUsdMicros() <= 0 || charged.GetAmountUsdMicros() != accepted.GetQuote().GetTotalUsdMicros() {
		return status.Error(codes.DataLoss, "wallet charge does not identify the original order")
	}
	return nil
}

// walletReadCanRecover reports whether a failed charge may already have taken
// effect. A refusal, an invalid request or a conflict is an answer; a transport or
// availability failure is not, and only those are read back before the outcome is
// recorded as unknown. An unrecognised status is treated as recoverable so a
// charge is never repeated on a failure this owner cannot explain.
func walletReadCanRecover(err error) bool {
	switch status.Code(err) {
	case codes.InvalidArgument, codes.FailedPrecondition, codes.AlreadyExists, codes.NotFound, codes.PermissionDenied, codes.Unauthenticated, codes.ResourceExhausted, codes.OutOfRange:
		return false
	default:
		return true
	}
}

// errorCodeText is the short contract spelling stored in the operation's error
// column, matching the codes the other steps record.
func errorCodeText(code api.ErrorCodeEnum) string {
	return strings.TrimPrefix(code.String(), "ERROR_CODE_ENUM_")
}

func validErrorCode(code api.ErrorCodeEnum) bool {
	if code == api.ErrorCodeEnum_ERROR_CODE_ENUM_UNSPECIFIED {
		return false
	}
	_, ok := api.ErrorCodeEnum_name[int32(code)]
	return ok
}
