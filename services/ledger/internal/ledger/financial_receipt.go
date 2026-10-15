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
	if record.TenantID != tenantID || record.WorkspaceID != workspaceID || evidence.GetReceipt().GetId() != receiptID ||
		evidence.GetWalletOperation().GetReceiptId() != receiptID || evidence.GetWalletOperation().GetWorkspaceId() != workspaceID ||
		evidence.GetQuoteAcceptance().GetWorkspaceId() != workspaceID || evidence.GetQuoteAcceptance().GetQuote() == nil || evidence.GetEvidenceDigest() == "" {
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
