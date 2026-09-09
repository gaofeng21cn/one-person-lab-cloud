package ledger

import (
	"bytes"
	"encoding/json"
	"io"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

func validWorkspaceLaunchCloseoutReceipt(input ReceiptInput) bool {
	if input.Status != "completed" || input.Surface != "control_plane" || input.AccountID == "" || input.WorkspaceID == "" || input.RequestID == "" || input.IdempotencyKey != input.RequestID+":closeout:ledger" || input.SupersedesReceiptID != "" || input.OrganizationID != "" || input.ProjectID != "" || input.TaskID != "" || input.ApprovalID != "" || input.JobID != "" || input.ArtifactID != "" || input.ReviewID != "" || input.ContinuationID != "" || len(input.Actor) != 1 || len(input.Plan)+len(input.Environment)+len(input.InputRefs)+len(input.OutputRefs)+len(input.ReviewerChecks)+len(input.Owner)+len(input.Continuation) != 0 {
		return false
	}
	actor, ok := input.Actor["userId"].(string)
	if !ok || !isOpaqueReference(actor) {
		return false
	}
	var execution contracts.WorkspaceLaunchCloseoutReceiptExecution
	var cost contracts.WorkspaceLaunchCloseoutReceiptCost
	if !decodeCloseoutReceiptObject(input.Execution, &execution) || !decodeCloseoutReceiptObject(input.Cost, &cost) || execution.OperationID != input.RequestID || !isOpaqueReference(execution.AuthorizationID) || execution.Reason == "" || cost.Currency != "USD" || cost.PriceVersion == "" {
		return false
	}
	var previous time.Time
	for _, stamp := range []string{execution.AuthorizedAt, execution.FrozenAt, execution.KeyRevokedAt, execution.ResourcesAbsentAt, execution.CompletedAt} {
		current, err := time.Parse(time.RFC3339Nano, stamp)
		if err != nil || current.Before(previous) {
			return false
		}
		previous = current
	}
	switch execution.Outcome {
	case "failed":
		return execution.ChargeConfirmation == nil && execution.RefundedUSDMicros == 0 && execution.RefundOperationID == "" && cost.ChargeUSDMicros == 0 && cost.PeriodStart == "" && cost.PaidThrough == ""
	case "refunded":
		charge := execution.ChargeConfirmation
		start, startErr := time.Parse(time.RFC3339Nano, cost.PeriodStart)
		end, endErr := time.Parse(time.RFC3339Nano, cost.PaidThrough)
		return charge != nil && charge.Status == "used" && charge.UserID > 0 && isOpaqueReference(charge.Code) && charge.ChargeUSDMicros > 0 && charge.ChargeUSDMicros == cost.ChargeUSDMicros && execution.RefundedUSDMicros == cost.ChargeUSDMicros && (execution.RefundOperationID == "" || isOpaqueReference(execution.RefundOperationID)) && startErr == nil && endErr == nil && end.After(start)
	default:
		return false
	}
}

func decodeCloseoutReceiptObject(value map[string]any, target any) bool {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target) == nil && decoder.Decode(new(any)) == io.EOF
}
