package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	api "opl-cloud/packages/contracts/go/api"
)

// WalletOperationDigest returns the canonical digest of one owner-recorded
// wallet operation.
//
// The Gateway owner computes it over the confirmed adjustment it recorded and
// the Ledger recomputes it over the operation it re-read from that owner, so
// the receipt names the exact bytes of the operator, the reason, the amount
// and the native adjustment code it accepted. A caller cannot borrow another
// operation's digest, because the digest is over the operation itself and not
// over a reference the caller chooses.
//
// The digest shape is the contract's digest shape, so a stored receipt can
// carry it beside the other evidence digests without a second spelling.
func WalletOperationDigest(operation *api.WalletOperation) (string, error) {
	if operation == nil {
		return "", fmt.Errorf("wallet operation digest: a wallet operation is required")
	}
	raw, err := protojson.Marshal(operation)
	if err != nil {
		return "", fmt.Errorf("wallet operation digest: %w", err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
