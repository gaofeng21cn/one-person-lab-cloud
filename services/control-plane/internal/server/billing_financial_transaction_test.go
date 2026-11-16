package server

import (
	"encoding/json"
	"time"
)

// financialTransactionListItem is the upstream financial transaction row the
// billing settlement trend reads. The d1 finance chain suite that first needed
// it is retired with the retained launch execution, but the trend coverage
// still reads these rows.
type financialTransactionListItem struct {
	Code         string      `json:"code"`
	Type         string      `json:"type"`
	Value        json.Number `json:"value"`
	AppliedValue json.Number `json:"balance_applied_value,omitempty"`
	Status       string      `json:"status"`
	UsedBy       int64       `json:"used_by"`
	UsedAt       time.Time   `json:"used_at"`
	CreatedAt    time.Time   `json:"created_at"`
	Notes        string      `json:"notes"`
}
