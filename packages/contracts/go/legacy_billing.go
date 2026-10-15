package contracts

import "encoding/json"

// LegacyBillingReceipt is the original owner's immutable customer projection.
// Optional original components are retained as exact JSON rather than decoded
// through float64. This reader does not calculate money or synthesize receipts.
type LegacyBillingReceipt struct {
	original           json.RawMessage
	ReceiptID          string          `json:"receiptId"`
	OperationID        string          `json:"operationId,omitempty"`
	RelatedOperationID string          `json:"relatedOperationId,omitempty"`
	Kind               string          `json:"kind,omitempty"`
	Type               string          `json:"type"`
	Status             string          `json:"status"`
	WorkspaceID        string          `json:"workspaceId"`
	CreatedAt          string          `json:"createdAt"`
	ResourceType       string          `json:"resourceType"`
	ResourceID         string          `json:"resourceId"`
	PriceVersion       string          `json:"priceVersion"`
	Currency           string          `json:"currency"`
	PeriodStart        string          `json:"periodStart"`
	PaidThrough        string          `json:"paidThrough"`
	ChargeUSDMicros    *int64          `json:"chargeUsdMicros,omitempty"`
	TotalUSDMicros     *int64          `json:"totalUsdMicros,omitempty"`
	RefundUSDMicros    *int64          `json:"refundUsdMicros,omitempty"`
	ChargeReference    string          `json:"chargeReference,omitempty"`
	RefundCode         string          `json:"refundCode,omitempty"`
	AccountID          string          `json:"accountId,omitempty"`
	ResourceStatus     json.RawMessage `json:"resourceStatus,omitempty"`
	LaunchReceiptID    string          `json:"launchReceiptId,omitempty"`
	StageEvidence      json.RawMessage `json:"stageEvidence,omitempty"`
	Components         json.RawMessage `json:"components,omitempty"`
	Fulfillment        json.RawMessage `json:"fulfillment,omitempty"`
}

// The retained owner has multiple historical receipt variants. Preserve its
// entire projection, including fields a newer reader does not yet render.
func (r *LegacyBillingReceipt) UnmarshalJSON(raw []byte) error {
	type fields LegacyBillingReceipt
	var parsed fields
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return err
	}
	*r = LegacyBillingReceipt(parsed)
	r.original = append(json.RawMessage(nil), raw...)
	return nil
}

func (r LegacyBillingReceipt) MarshalJSON() ([]byte, error) {
	if r.original != nil {
		return r.original, nil
	}
	type fields LegacyBillingReceipt
	return json.Marshal(fields(r))
}

type LegacyBillingPage struct {
	Receipts   []LegacyBillingReceipt `json:"receipts"`
	NextCursor string                 `json:"nextCursor"`
	HasMore    bool                   `json:"hasMore"`
}

type LegacySettlementDay struct {
	Date              string `json:"date"`
	ChargedUSDMicros  int64  `json:"chargedUsdMicros"`
	RefundedUSDMicros int64  `json:"refundedUsdMicros"`
	NetUSDMicros      int64  `json:"netUsdMicros"`
	ChargeCount       int64  `json:"chargeCount"`
	RefundCount       int64  `json:"refundCount"`
}

type LegacySettlementTrend struct {
	Timezone          string                `json:"timezone"`
	AsOf              string                `json:"asOf"`
	WindowStart       string                `json:"windowStart"`
	WindowEnd         string                `json:"windowEnd"`
	Days              []LegacySettlementDay `json:"days"`
	ChargedUSDMicros  int64                 `json:"chargedUsdMicros"`
	RefundedUSDMicros int64                 `json:"refundedUsdMicros"`
	NetUSDMicros      int64                 `json:"netUsdMicros"`
	SettledCount      int64                 `json:"settledCount"`
	InFlightCount     int64                 `json:"inFlightCount"`
	UnconfirmedCount  int64                 `json:"unconfirmedCount"`
	UnattributedCount int64                 `json:"unattributedCount"`
	OutOfWindowCount  int64                 `json:"outOfWindowCount"`
	Complete          bool                  `json:"complete"`
}

type LegacyBillingSource[T any] struct {
	Source    string `json:"source"`
	Status    string `json:"status"`
	Available bool   `json:"available"`
	Data      *T     `json:"data,omitempty"`
	FetchedAt string `json:"fetchedAt"`
}
