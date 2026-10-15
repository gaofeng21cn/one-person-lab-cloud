package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

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

func parseLegacyInt64(raw json.RawMessage) (int64, error) {
	value := bytes.TrimSpace(raw)
	if len(value) > 0 && value[0] == '"' {
		var text string
		if err := json.Unmarshal(value, &text); err != nil {
			return 0, err
		}
		return strconv.ParseInt(text, 10, 64)
	}
	var number json.Number
	if err := json.Unmarshal(value, &number); err != nil {
		return 0, err
	}
	return number.Int64()
}

func rewriteLegacyMoneyObject(raw []byte, keys []string, asString bool) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	for _, key := range keys {
		value, ok := object[key]
		if !ok {
			continue
		}
		amount, err := parseLegacyInt64(value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		if asString {
			object[key], err = json.Marshal(strconv.FormatInt(amount, 10))
		} else {
			object[key] = json.RawMessage(strconv.FormatInt(amount, 10))
		}
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(object)
}

func rewriteLegacyReceiptMoney(raw []byte, asString bool) ([]byte, error) {
	rewritten, err := rewriteLegacyMoneyObject(raw, []string{"chargeUsdMicros", "totalUsdMicros", "refundUsdMicros"}, asString)
	if err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(rewritten, &object); err != nil {
		return nil, err
	}
	if components, ok := object["components"]; ok && len(components) > 0 && string(components) != "null" {
		components, err = rewriteLegacyMoneyObject(components, []string{"chargeUsdMicros"}, asString)
		if err != nil {
			return nil, err
		}
		var componentObject map[string]json.RawMessage
		if err := json.Unmarshal(components, &componentObject); err != nil {
			return nil, err
		}
		for _, name := range []string{"compute", "storage"} {
			if child, ok := componentObject[name]; ok && len(child) > 0 && string(child) != "null" {
				child, err = rewriteLegacyMoneyObject(child, []string{"chargeUsdMicros"}, asString)
				if err != nil {
					return nil, err
				}
				componentObject[name] = child
			}
		}
		object["components"], err = json.Marshal(componentObject)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(object)
}

// The retained owner has multiple historical receipt variants. Preserve its
// entire projection, including fields a newer reader does not yet render.
func (r *LegacyBillingReceipt) UnmarshalJSON(raw []byte) error {
	normalized, err := rewriteLegacyReceiptMoney(raw, false)
	if err != nil {
		return err
	}
	type fields LegacyBillingReceipt
	var parsed fields
	if err := json.Unmarshal(normalized, &parsed); err != nil {
		return err
	}
	*r = LegacyBillingReceipt(parsed)
	r.original = append(json.RawMessage(nil), raw...)
	return nil
}

func (r LegacyBillingReceipt) MarshalJSON() ([]byte, error) {
	raw := r.original
	if raw == nil {
		type fields LegacyBillingReceipt
		var err error
		raw, err = json.Marshal(fields(r))
		if err != nil {
			return nil, err
		}
	}
	return rewriteLegacyReceiptMoney(raw, true)
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

func (d LegacySettlementDay) MarshalJSON() ([]byte, error) {
	type fields LegacySettlementDay
	raw, err := json.Marshal(fields(d))
	if err != nil {
		return nil, err
	}
	return rewriteLegacyMoneyObject(raw, []string{"chargedUsdMicros", "refundedUsdMicros", "netUsdMicros"}, true)
}

func (d *LegacySettlementDay) UnmarshalJSON(raw []byte) error {
	normalized, err := rewriteLegacyMoneyObject(raw, []string{"chargedUsdMicros", "refundedUsdMicros", "netUsdMicros"}, false)
	if err != nil {
		return err
	}
	type fields LegacySettlementDay
	var parsed fields
	if err := json.Unmarshal(normalized, &parsed); err != nil {
		return err
	}
	*d = LegacySettlementDay(parsed)
	return nil
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

func (t LegacySettlementTrend) MarshalJSON() ([]byte, error) {
	type fields LegacySettlementTrend
	raw, err := json.Marshal(fields(t))
	if err != nil {
		return nil, err
	}
	return rewriteLegacyMoneyObject(raw, []string{"chargedUsdMicros", "refundedUsdMicros", "netUsdMicros"}, true)
}

func (t *LegacySettlementTrend) UnmarshalJSON(raw []byte) error {
	normalized, err := rewriteLegacyMoneyObject(raw, []string{"chargedUsdMicros", "refundedUsdMicros", "netUsdMicros"}, false)
	if err != nil {
		return err
	}
	type fields LegacySettlementTrend
	var parsed fields
	if err := json.Unmarshal(normalized, &parsed); err != nil {
		return err
	}
	*t = LegacySettlementTrend(parsed)
	return nil
}

type LegacyBillingSource[T any] struct {
	Source    string `json:"source"`
	Status    string `json:"status"`
	Available bool   `json:"available"`
	Data      *T     `json:"data,omitempty"`
	FetchedAt string `json:"fetchedAt"`
}
