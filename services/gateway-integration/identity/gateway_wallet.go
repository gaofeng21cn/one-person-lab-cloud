package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Sub2API keeps the only spendable balance. The adapter below performs the
// administrator balance adjustment and reads back the native adjustment record,
// matching the retained Control Plane semantics exactly: one POST per business
// code, an unconfirmed response is never repeated, and evidence is only accepted
// from the positive adjustment history.
const (
	gatewayWalletResponseLimit   = 1 << 20
	gatewayBalanceNotePrefix     = "OPL Cloud balance adjustment: "
	gatewayAdjustmentCodeLimit   = 32
	gatewayAdjustmentLookups     = 100
	gatewayBalanceHistoryTypes   = 2
	maxGatewayBalanceHistoryPage = 1_000_000
)

// ErrGatewayChargeUnknown reports that a balance POST was dispatched but its
// outcome is unconfirmed. It is never retried inside this adapter: the upstream
// audit/idempotency writes are not atomic with the balance write, so repeating the
// POST is not a recovery protocol. The caller reads the operation back instead.
var ErrGatewayChargeUnknown = errors.New("gateway balance adjustment result unknown")

// ErrGatewayChargeConflict reports that the wallet already answers differently
// from the submitted evidence: a different identity, amount, or a duplicate native
// adjustment.
var ErrGatewayChargeConflict = errors.New("gateway balance adjustment conflict")

// ErrGatewayAdjustmentUnrepresentable reports that the requested micros amount
// cannot be encoded into the native numeric(20,8) balance API without changing the
// value.
var ErrGatewayAdjustmentUnrepresentable = errors.New("gateway adjustment amount is not exactly representable")

// GatewayWalletAdjustment is the confirmed result of one admin balance change.
type GatewayWalletAdjustment struct {
	Code   string
	UserID int64
	Micros int64
}

// GatewayBalanceHistoryEntry is one native positive balance-adjustment record.
type GatewayBalanceHistoryEntry struct {
	Code   string
	Micros int64
}

// AdjustBalance once dispatches a single administrator balance change under the
// business code and never retries it. operation is exactly "subtract" for a charge
// and "add" for a refund.
func (g *Gateway) AdjustBalance(ctx context.Context, userID int64, code string, micros int64, operation string) error {
	if userID <= 0 || micros <= 0 || (operation != "subtract" && operation != "add") {
		return status.Error(codes.InvalidArgument, "a positive wallet identity, amount and a known operation are required")
	}
	if len(code) > gatewayAdjustmentCodeLimit || strings.TrimSpace(code) != code || code == "" {
		return status.Error(codes.InvalidArgument, "a wallet adjustment code of at most 32 characters without outer whitespace is required")
	}
	balance, err := gatewayUSDMicrosJSON(micros)
	if err != nil {
		return status.Error(codes.InvalidArgument, err.Error())
	}
	// The upstream decodes float64 into numeric(20,8); compare the exact decimal
	// that lib/pq will actually send against our micros before dispatch.
	nativeBalance, parseErr := strconv.ParseFloat(string(balance), 64)
	nativeMicros, amountErr := gatewayDecimalUSDMicros(json.Number(strconv.FormatFloat(nativeBalance, 'f', -1, 64)))
	if parseErr != nil || amountErr != nil || nativeMicros != micros || micros >= 1_000_000_000_000_000_000 {
		return status.Error(codes.InvalidArgument, ErrGatewayAdjustmentUnrepresentable.Error())
	}
	if operation == "add" {
		if err := g.requireRefundWithoutRebate(ctx); err != nil {
			return err
		}
	}
	if !g.DirectoryConfigured() {
		return status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return err
	}
	payload := struct {
		Balance   json.RawMessage `json:"balance"`
		Operation string          `json:"operation"`
		Notes     string          `json:"notes"`
	}{Balance: balance, Operation: operation, Notes: gatewayBalanceNotePrefix + code}
	body, err := g.singleAdminRequest(ctx, "/api/v1/admin/users/"+strconv.FormatInt(userID, 10)+"/balance", payload, token)
	if err != nil {
		var httpErr *gatewayHTTPError
		if errors.As(err, &httpErr) && (httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden) {
			return status.Error(codes.Unauthenticated, "Gateway wallet adjustment rejected")
		}
		if errors.As(err, &httpErr) && httpErr.StatusCode < http.StatusInternalServerError && httpErr.StatusCode != http.StatusTooManyRequests {
			return status.Error(codes.FailedPrecondition, "Gateway wallet adjustment refused")
		}
		return fmt.Errorf("%w: %v", ErrGatewayChargeUnknown, err)
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if decodeGatewayEnvelope(body, &user) != nil || user.ID != userID {
		return fmt.Errorf("%w: balance adjustment response identity is unconfirmed", ErrGatewayChargeUnknown)
	}
	return nil
}

// RefundRebateBlocked reads the official settings and refuses an "add" adjustment
// that the upstream would treat as an affiliate-eligible recharge. It reads the
// settings without changing Gateway configuration.
func (g *Gateway) requireRefundWithoutRebate(ctx context.Context) error {
	token, err := g.directoryToken(ctx)
	if err != nil {
		return err
	}
	var out struct {
		AffiliateEnabled     *bool `json:"affiliate_enabled"`
		AdminRechargeEnabled *bool `json:"affiliate_admin_recharge_enabled"`
	}
	body, err := g.adminRequest(ctx, "/api/v1/admin/settings", nil, token)
	if err != nil {
		return status.Error(codes.Unavailable, "Gateway refund rebate policy unavailable")
	}
	if decodeGatewayEnvelope(body, &out) != nil || out.AffiliateEnabled == nil || out.AdminRechargeEnabled == nil {
		return status.Error(codes.FailedPrecondition, "Gateway refund rebate policy unavailable")
	}
	if *out.AffiliateEnabled && *out.AdminRechargeEnabled {
		return status.Error(codes.FailedPrecondition, "Gateway refund would accrue an affiliate rebate")
	}
	return nil
}

// PositiveBalanceHistoryByCodes returns the native adjustment records the wallet
// owner positively confirms for the given business codes. An empty result is never
// proof that an attempted adjustment did not change the balance: the upstream
// writes its audit record after the atomic balance update. A conflicting or
// malformed record is refused rather than returned as evidence.
func (g *Gateway) PositiveBalanceHistoryByCodes(ctx context.Context, userID int64, adjustmentCodes []string) (map[string]GatewayBalanceHistoryEntry, error) {
	if userID <= 0 || len(adjustmentCodes) == 0 {
		return nil, status.Error(codes.InvalidArgument, "a positive wallet identity and at least one adjustment code are required")
	}
	targets := make(map[string]struct{}, len(adjustmentCodes))
	for _, code := range adjustmentCodes {
		if code == "" || len(code) > 200 || strings.TrimSpace(code) != code {
			return nil, status.Error(codes.InvalidArgument, "invalid Gateway adjustment code")
		}
		targets[code] = struct{}{}
	}
	if !g.DirectoryConfigured() {
		return nil, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return nil, err
	}
	matches := make(map[string]GatewayBalanceHistoryEntry, len(targets))
	for _, recordType := range []string{"admin_balance", "balance"} {
		for page := 1; ; page++ {
			data, err := g.balanceHistoryPage(ctx, token, userID, page, recordType)
			if err != nil {
				return nil, err
			}
			for _, item := range data.Items {
				code := item.Code
				if recordType == "admin_balance" {
					if !strings.HasPrefix(item.Notes, gatewayBalanceNotePrefix) {
						continue
					}
					code = strings.TrimPrefix(item.Notes, gatewayBalanceNotePrefix)
				}
				if _, wanted := targets[code]; !wanted {
					continue
				}
				entry, entryErr := gatewayConfirmedAdjustment(item, recordType, code, userID)
				if previous, exists := matches[code]; entryErr == nil && exists {
					if previous.Micros != entry.Micros {
						return nil, status.Error(codes.FailedPrecondition, ErrGatewayChargeConflict.Error())
					}
				}
				if entryErr != nil {
					return nil, status.Error(codes.FailedPrecondition, entryErr.Error())
				}
				matches[code] = entry
			}
			if page >= data.Pages {
				break
			}
		}
	}
	return matches, nil
}

type gatewayBalanceRecord struct {
	Code                string       `json:"code"`
	Notes               string       `json:"notes"`
	Type                string       `json:"type"`
	Value               *json.Number `json:"value"`
	BalanceAppliedValue *json.Number `json:"balance_applied_value"`
	Status              string       `json:"status"`
	UsedBy              *int64       `json:"used_by"`
	UsedAt              *string      `json:"used_at"`
	CreatedAt           *string      `json:"created_at"`
}

type gatewayBalancePage struct {
	Items    []gatewayBalanceRecord `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
	Pages    int                    `json:"pages"`
}

func (g *Gateway) balanceHistoryPage(ctx context.Context, token string, userID int64, page int, recordType string) (gatewayBalancePage, error) {
	if page > maxGatewayBalanceHistoryPage {
		return gatewayBalancePage{}, status.Error(codes.InvalidArgument, "Gateway balance history page exceeds supported range")
	}
	path := "/api/v1/admin/users/" + strconv.FormatInt(userID, 10) + "/balance-history?page=" + strconv.Itoa(page) + "&page_size=" + strconv.Itoa(gatewayAdjustmentLookups) + "&type=" + recordType
	body, err := g.adminRequest(ctx, path, nil, token)
	if err != nil {
		return gatewayBalancePage{}, err
	}
	var data gatewayBalancePage
	if decodeGatewayEnvelope(body, &data) != nil {
		return gatewayBalancePage{}, status.Error(codes.Unavailable, "invalid Gateway balance history response")
	}
	expectedPages := 1
	expectedItems := 0
	if data.Total > 0 {
		expectedPages = int((data.Total + int64(gatewayAdjustmentLookups) - 1) / int64(gatewayAdjustmentLookups))
		if page <= expectedPages {
			expectedItems = gatewayAdjustmentLookups
			if remaining := data.Total - int64(page-1)*int64(gatewayAdjustmentLookups); remaining < int64(expectedItems) {
				expectedItems = int(remaining)
			}
		}
	}
	if data.Total < 0 || data.Page != page || data.PageSize != gatewayAdjustmentLookups || data.Pages != expectedPages || page > data.Pages || len(data.Items) != expectedItems {
		return gatewayBalancePage{}, status.Error(codes.Unavailable, "invalid Gateway balance history pagination")
	}
	return data, nil
}

func gatewayConfirmedAdjustment(item gatewayBalanceRecord, recordType, code string, userID int64) (GatewayBalanceHistoryEntry, error) {
	if item.Type != recordType || item.Code == "" || item.Status != "used" || item.UsedBy == nil || *item.UsedBy != userID || item.UsedAt == nil || *item.UsedAt == "" || item.CreatedAt == nil || *item.CreatedAt == "" {
		return GatewayBalanceHistoryEntry{}, fmt.Errorf("%w: balance adjustment identity or state differs", ErrGatewayChargeConflict)
	}
	if item.Value == nil {
		return GatewayBalanceHistoryEntry{}, fmt.Errorf("%w: balance adjustment has no value", ErrGatewayChargeConflict)
	}
	value, err := gatewayDecimalUSDMicros(*item.Value)
	if err != nil || value == 0 {
		return GatewayBalanceHistoryEntry{}, fmt.Errorf("%w: invalid balance adjustment amount", ErrGatewayChargeConflict)
	}
	if recordType == "balance" {
		if item.BalanceAppliedValue == nil {
			return GatewayBalanceHistoryEntry{}, fmt.Errorf("%w: balance adjustment has no verified applied amount", ErrGatewayChargeConflict)
		}
		applied, err := gatewayDecimalUSDMicros(*item.BalanceAppliedValue)
		if err != nil || applied != value {
			return GatewayBalanceHistoryEntry{}, fmt.Errorf("%w: balance adjustment applied amount differs", ErrGatewayChargeConflict)
		}
	}
	return GatewayBalanceHistoryEntry{Code: code, Micros: value}, nil
}

// adminRequest performs one authenticated GET/POST against the Gateway admin API
// and returns the raw response body. A non-2xx answer is reported as a typed HTTP
// error so the money path can distinguish a refusal from an unknown outcome.
func (g *Gateway) adminRequest(ctx context.Context, path string, body any, token string) ([]byte, error) {
	var raw []byte
	method := http.MethodGet
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, status.Error(codes.Internal, "encode Gateway request")
		}
		raw, method = encoded, http.MethodPost
	}
	r, err := http.NewRequestWithContext(ctx, method, g.base+path, bytes.NewReader(raw))
	if err != nil {
		return nil, status.Error(codes.Internal, "create Gateway request")
	}
	if body != nil {
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Authorization", "Bearer "+token)
	res, err := g.client.Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, gatewayWalletResponseLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > gatewayWalletResponseLimit {
		return nil, errors.New("Gateway response too large")
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, &gatewayHTTPError{StatusCode: res.StatusCode}
	}
	return data, nil
}

// singleAdminRequest performs at most one POST: it disables redirects and body
// replay so a connection failure cannot repeat the balance adjustment.
func (g *Gateway) singleAdminRequest(ctx context.Context, path string, body any, token string) ([]byte, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, status.Error(codes.Internal, "encode Gateway request")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, g.base+path, bytes.NewReader(encoded))
	if err != nil {
		return nil, status.Error(codes.Internal, "create Gateway request")
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+token)
	single := *g.client
	single.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := single.Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, gatewayWalletResponseLimit+1))
	if err != nil {
		return nil, err
	}
	if len(data) > gatewayWalletResponseLimit {
		return nil, errors.New("Gateway response too large")
	}
	if res.StatusCode < http.StatusOK || res.StatusCode >= http.StatusMultipleChoices {
		return nil, &gatewayHTTPError{StatusCode: res.StatusCode}
	}
	return data, nil
}

type gatewayHTTPError struct{ StatusCode int }

func (e *gatewayHTTPError) Error() string {
	return fmt.Sprintf("gateway request failed with status %d", e.StatusCode)
}

func decodeGatewayEnvelope(body []byte, output any) error {
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.Code != 0 || len(envelope.Data) == 0 {
		return errors.New("invalid Gateway response envelope")
	}
	return json.Unmarshal(envelope.Data, output)
}

func gatewayDecimalUSDMicros(value json.Number) (int64, error) {
	rational, ok := new(big.Rat).SetString(value.String())
	if !ok {
		return 0, errors.New("invalid decimal")
	}
	rational.Mul(rational, big.NewRat(1_000_000, 1))
	if rational.Denom().Cmp(big.NewInt(1)) != 0 || !rational.Num().IsInt64() {
		return 0, errors.New("decimal is not representable as USD micros")
	}
	return rational.Num().Int64(), nil
}

func gatewayUSDMicrosJSON(micros int64) (json.RawMessage, error) {
	if micros <= 0 {
		return nil, errors.New("a positive USD micros amount is required")
	}
	return json.RawMessage(fmt.Sprintf("%d.%06d", micros/1_000_000, micros%1_000_000)), nil
}
