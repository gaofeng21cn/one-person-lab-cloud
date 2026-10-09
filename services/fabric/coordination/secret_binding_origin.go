package coordination

import (
	"context"
	"database/sql"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	api "opl-cloud/packages/contracts/go/api"
)

// secretBindingOrigin is the durable original command identity of one Secret
// binding: the exact runtime instance and Gateway key binding the binding was
// created for. It is read from the binding's originating command, which Fabric
// writes in the same transaction as every binding row.
type secretBindingOrigin struct {
	RuntimeInstanceID string
	KeyBindingID      string
}

// secretBindingOriginOf resolves one binding's original command. Every binding is
// produced by exactly one current writer — the initial BindSecret or the
// RebindSecret replacement — so the lookup names those two exact derived action
// identities; only one exists, and a binding with neither is data loss rather
// than an absent fact.
func secretBindingOriginOf(ctx context.Context, tx *sql.Tx, bindingID string) (secretBindingOrigin, error) {
	var raw []byte
	err := tx.QueryRowContext(ctx, `SELECT approved_input FROM fabric.resource_actions WHERE id=$1`, "raction_"+shortDigest(bindingID, "inject")).Scan(&raw)
	switch {
	case err == nil:
		var command api.SecretBindingCommand
		if protojson.Unmarshal(raw, &command) != nil || command.GetRuntimeInstanceId() == "" || command.GetKeyBindingId() == "" {
			return secretBindingOrigin{}, status.Error(codes.DataLoss, "the active Secret binding's original command is unreadable")
		}
		return secretBindingOrigin{RuntimeInstanceID: command.GetRuntimeInstanceId(), KeyBindingID: command.GetKeyBindingId()}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return secretBindingOrigin{}, persistenceError(err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT approved_input FROM fabric.resource_actions WHERE id=$1`, "raction_"+shortDigest(bindingID, "rebind")).Scan(&raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return secretBindingOrigin{}, status.Error(codes.DataLoss, "the active Secret binding has no original command")
		}
		return secretBindingOrigin{}, persistenceError(err)
	}
	var command api.SecretBindingRebindCommand
	if protojson.Unmarshal(raw, &command) != nil || command.GetRuntimeInstanceId() == "" || command.GetKeyBindingId() == "" {
		return secretBindingOrigin{}, status.Error(codes.DataLoss, "the active Secret binding's original replacement command is unreadable")
	}
	return secretBindingOrigin{RuntimeInstanceID: command.GetRuntimeInstanceId(), KeyBindingID: command.GetKeyBindingId()}, nil
}
