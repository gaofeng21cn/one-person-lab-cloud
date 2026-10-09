package launch

import (
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// paidFundingEvidence is the confirmed charge proof that releases the runtime for
// a paid order. It is the Ledger WALLET_ACTION receipt recorded for this exact
// obligation, whose charge the Gateway wallet authority confirmed. It is never a
// Local zero-charge receipt, and its amount must be the accepted quote amount.
func (r *orderResult) paidFundingEvidence() (*api.WalletActionReceiptEvidence, error) {
	if len(r.WalletActionReceipt) == 0 {
		return nil, nil
	}
	evidence := &api.WalletActionReceiptEvidence{}
	if protojson.Unmarshal(r.WalletActionReceipt, evidence) != nil {
		return nil, status.Error(codes.DataLoss, "stored Wallet action funding evidence is invalid")
	}
	return evidence, nil
}

// validatePaidFundingEvidence binds the recorded WALLET_ACTION funding proof to
// the original order, its accepted quote and the confirmed charge. A proof whose
// obligation, workspace, amount or charge identity differs is unreadable
// evidence: it can never release this order's runtime.
func validatePaidFundingEvidence(op ownerstore.Operation, accepted *api.QuoteAcceptance, evidence *api.WalletActionReceiptEvidence) error {
	if evidence == nil || localNoCharge(accepted) {
		return status.Error(codes.FailedPrecondition, "a paid Workspace order requires confirmed charge evidence before runtime")
	}
	receipt, charge := evidence.GetReceipt(), evidence.GetWalletOperation()
	if receipt.GetId() == "" || receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		receipt.GetOperationId() != op.ID || receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || receipt.GetCreatedAt() == nil || receipt.CreatedAt.CheckValid() != nil ||
		evidence.GetEvidenceDigest() != accepted.GetSnapshotDigest() || !proto.Equal(evidence.GetQuoteAcceptance(), accepted) {
		return status.Error(codes.DataLoss, "Wallet action funding evidence is not the original paid order")
	}
	if err := validateWalletCharge(op, accepted, charge); err != nil {
		return err
	}
	if charge.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED || charge.GetReceiptId() == "" || charge.GetReceiptId() != receipt.GetId() {
		return status.Error(codes.DataLoss, "Wallet action funding evidence has no confirmed charge receipt")
	}
	if charge.GetAmountUsdMicros() <= 0 || charge.GetAmountUsdMicros() != accepted.GetQuote().GetTotalUsdMicros() {
		return status.Error(codes.DataLoss, "Wallet action funding evidence does not match the accepted amount")
	}
	return nil
}

// fundingProof resolves the owner-authoritative proof that released this order's
// resources. A Local no-charge order is released by its Ledger zero-charge receipt;
// a paid order is released by the Ledger WALLET_ACTION receipt for its confirmed
// Gateway charge. Both are read from the durable result, so recovery replays the
// exact identity Fabric consumed instead of re-deriving one.
func (s *Service) fundingProof(op ownerstore.Operation, accepted *api.QuoteAcceptance, result *orderResult) (*api.Receipt, *api.WalletActionReceiptEvidence, error) {
	commit, err := evidence(op)
	if err != nil {
		return nil, nil, err
	}
	return s.fundingProofFor(op, accepted, result, commit)
}

// fundingProofFor is fundingProof with the owner commit evidence already resolved,
// so a caller that holds the commit does not re-derive it.
func (s *Service) fundingProofFor(op ownerstore.Operation, accepted *api.QuoteAcceptance, result *orderResult, commit *api.OwnerCommitEvidence) (*api.Receipt, *api.WalletActionReceiptEvidence, error) {
	if localNoCharge(accepted) {
		zero := &api.LocalNoChargeReceiptEvidence{}
		if protojson.Unmarshal(result.ZeroChargeReceipt, zero) != nil {
			return nil, nil, status.Error(codes.FailedPrecondition, "verified Local no-charge evidence is required before runtime")
		}
		if err := validateZeroChargeEvidence(op, accepted, commit, zero, nil); err != nil {
			return nil, nil, status.Error(codes.FailedPrecondition, "verified Local no-charge evidence is required before runtime")
		}
		return zero.GetReceipt(), nil, nil
	}
	paid, err := result.paidFundingEvidence()
	if err != nil {
		return nil, nil, err
	}
	if err = validatePaidFundingEvidence(op, accepted, paid); err != nil {
		return nil, nil, err
	}
	return nil, paid, nil
}
