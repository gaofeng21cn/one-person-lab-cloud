package ledger

import (
	"context"
	"database/sql"
	"errors"

	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

// WorkspaceFinancialReceipt projects the monetary evidence Ledger actually
// accepted. It exposes neither the private committing actor nor execution data.
// An observation event or an external reference is not a funded-charge receipt.
func (s *PostgresStore) WorkspaceFinancialReceipt(ctx context.Context, tenantID, workspaceID, receiptID string) (*api.WorkspaceFinancialReceipt, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT payload_json FROM evidence_receipts
  WHERE id=$1 AND organization_id=$2 AND workspace_id=$3 AND receipt_type=$4`, receiptID, tenantID, workspaceID, WalletActionReceiptType).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrReceiptNotFound
	}
	if err != nil {
		return nil, err
	}
	record, err := decodeWalletAction(payload)
	if err != nil {
		return nil, err
	}
	evidence := record.Evidence
	receipt := evidence.GetReceipt()
	wallet := evidence.GetWalletOperation()
	quoteAcceptance := evidence.GetQuoteAcceptance()
	quote := quoteAcceptance.GetQuote()
	if record.TenantID != tenantID || record.WorkspaceID != workspaceID || receipt == nil || wallet == nil || quoteAcceptance == nil || quote == nil ||
		receipt.GetId() != receiptID || receipt.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION || receipt.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || receipt.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED ||
		wallet.GetId() == "" || wallet.GetReceiptId() != receiptID || wallet.GetWorkspaceId() != workspaceID || wallet.GetKind() != api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE || wallet.GetStatus() != api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED ||
		quoteAcceptance.GetWorkspaceId() != workspaceID || quote.GetWorkspaceId() != "" && quote.GetWorkspaceId() != workspaceID || quote.GetStatus() != api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED || quote.GetTotalUsdMicros() <= 0 || wallet.GetAmountUsdMicros() != quote.GetTotalUsdMicros() || evidence.GetEvidenceDigest() == "" {
		return nil, ErrInvalidReceiptInput
	}
	return &api.WorkspaceFinancialReceipt{
		WorkspaceId:     workspaceID,
		Receipt:         proto.Clone(evidence.Receipt).(*api.Receipt),
		WalletOperation: proto.Clone(evidence.WalletOperation).(*api.WalletOperation),
		Quote:           proto.Clone(evidence.QuoteAcceptance.Quote).(*api.Quote),
		EvidenceDigest:  evidence.EvidenceDigest,
	}, nil
}
