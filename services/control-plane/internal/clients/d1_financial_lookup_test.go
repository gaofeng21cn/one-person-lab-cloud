package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"testing"
	"time"
)

func d1HistoryRecord(code string, valueUSDMicros, userID int64) sub2APIBalanceHistoryRecord {
	value := json.Number(string(usdMicrosJSON(valueUSDMicros)))
	created := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	used := created.Add(time.Minute)
	return sub2APIBalanceHistoryRecord{Code: code, Type: "balance", Value: &value, BalanceAppliedValue: &value, Status: "used", UsedBy: &userID, UsedAt: &used, CreatedAt: &created}
}

func d1ExactHistoryData(t *testing.T, record *sub2APIBalanceHistoryRecord) sub2APIExactBalanceHistoryResponse {
	t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	return sub2APIExactBalanceHistoryResponse{Lookup: "exact_code_v1", RedeemCode: data}
}

func writeD1Sub2APILogin(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	writeSub2APISuccess(t, w, struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}{"access", "refresh"})
}

func TestSub2APIAdjustmentReplay(t *testing.T) {
	for _, adjustment := range []struct {
		name   string
		code   string
		signed int64
		call   func(*Sub2APIHTTPClient) (string, error)
	}{
		{name: "charge", code: "opl:replay:charge", signed: -50_000_000, call: func(client *Sub2APIHTTPClient) (string, error) {
			result, err := client.Charge(context.Background(), Sub2APIChargeInput{UserID: 41, Code: "opl:replay:charge", ChargeUSDMicros: 50_000_000})
			return result.Status, err
		}},
		{name: "refund", code: "opl:replay:refund", signed: 50_000_000, call: func(client *Sub2APIHTTPClient) (string, error) {
			result, err := client.Refund(context.Background(), Sub2APIRefundInput{UserID: 41, Code: "opl:replay:refund", RefundUSDMicros: 50_000_000})
			return result.Status, err
		}},
	} {
		for _, scenario := range []struct {
			name          string
			amountDelta   int64
			userID        int64
			missing       bool
			duplicate     bool
			lookupStatus  int
			missingMarker bool
			wantErr       error
		}{
			{name: "exact", userID: 41},
			{name: "different amount", userID: 41, amountDelta: 1, wantErr: ErrSub2APIChargeConflict},
			{name: "different account", userID: 42, wantErr: ErrSub2APIChargeConflict},
			{name: "missing", missing: true, wantErr: ErrSub2APIChargeUnknown},
			{name: "duplicate evidence", userID: 41, duplicate: true, wantErr: ErrSub2APIChargeConflict},
			{name: "lookup unavailable", lookupStatus: http.StatusServiceUnavailable, wantErr: ErrSub2APIChargeUnknown},
			{name: "old server route missing", lookupStatus: http.StatusNotFound, wantErr: ErrSub2APIChargeUnknown},
			{name: "contract marker missing", userID: 41, missingMarker: true, wantErr: ErrSub2APIChargeUnknown},
		} {
			t.Run(adjustment.name+" "+scenario.name, func(t *testing.T) {
				postCalls, lookupCalls := 0, 0
				client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v1/auth/login":
						writeD1Sub2APILogin(t, w)
					case "/api/v1/admin/redeem-codes/create-and-redeem":
						postCalls++
						http.Error(w, "conflict", http.StatusConflict)
					case "/api/v1/admin/redeem-codes/by-code":
						lookupCalls++
						if r.URL.Query().Get("code") != adjustment.code || r.URL.Query().Get("user_id") != "41" || r.Method != http.MethodGet {
							t.Errorf("lookup query = %s %s", r.Method, r.URL)
						}
						if scenario.lookupStatus != 0 {
							http.Error(w, "unavailable", scenario.lookupStatus)
							return
						}
						record := d1HistoryRecord(adjustment.code, adjustment.signed+scenario.amountDelta, scenario.userID)
						data := d1ExactHistoryData(t, &record)
						if scenario.missing {
							data = d1ExactHistoryData(t, nil)
						}
						if scenario.duplicate {
							data.RedeemCode, _ = json.Marshal([]sub2APIBalanceHistoryRecord{record, record})
						}
						if scenario.missingMarker {
							data.Lookup = ""
						}
						writeSub2APISuccess(t, w, data)
					default:
						t.Errorf("unexpected route %s", r.URL.Path)
						http.NotFound(w, r)
					}
				}, time.Second)
				status, err := adjustment.call(client)
				if scenario.wantErr == nil {
					if err != nil || status != "used" {
						t.Fatalf("confirmed replay status=%q err=%v", status, err)
					}
				} else if !errors.Is(err, scenario.wantErr) {
					t.Fatalf("replay error=%v, want %v", err, scenario.wantErr)
				}
				if postCalls != 1 || lookupCalls != 1 {
					t.Fatalf("replay calls post=%d lookup=%d", postCalls, lookupCalls)
				}
			})
		}
	}
}

func TestSub2APIFinancialBalanceHistoryByCodesQueriesEachOriginalTransaction(t *testing.T) {
	queries := []string{}
	client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			writeD1Sub2APILogin(t, w)
			return
		}
		if r.URL.Path != "/api/v1/admin/redeem-codes/by-code" || r.URL.Query().Get("user_id") != "41" {
			t.Errorf("unexpected financial lookup %s", r.URL)
			http.NotFound(w, r)
			return
		}
		code := r.URL.Query().Get("code")
		queries = append(queries, code)
		if code == "opl:missing" {
			writeSub2APISuccess(t, w, d1ExactHistoryData(t, nil))
			return
		}
		record := d1HistoryRecord(code, -52_580_000, 41)
		writeSub2APISuccess(t, w, d1ExactHistoryData(t, &record))
	}, time.Second)
	matches, err := client.FinancialBalanceHistoryByCodes(context.Background(), 41, []string{"opl:original", "opl:missing", "opl:original"})
	if err != nil || !slices.Equal(queries, []string{"opl:original", "opl:missing"}) || len(matches) != 1 {
		t.Fatalf("matches=%#v queries=%#v err=%v", matches, queries, err)
	}
	entry := matches["opl:original"]
	if entry.Code != "opl:original" || entry.ValueUSDMicros != -52_580_000 || entry.UsedBy == nil || *entry.UsedBy != 41 {
		t.Fatalf("entry=%#v", entry)
	}
}

func TestSub2APIFinancialBalanceHistoryByCodesDoesNotScanTenThousandUnrelatedTransactions(t *testing.T) {
	requests := 0
	history := make([]sub2APIBalanceHistoryRecord, 10001)
	for i := range history {
		history[i] = d1HistoryRecord(fmt.Sprintf("opl:history:%d", i), -1, 41)
	}
	client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			writeD1Sub2APILogin(t, w)
			return
		}
		requests++
		if r.URL.Path != "/api/v1/admin/redeem-codes/by-code" {
			t.Errorf("financial confirmation scanned account history: %s", r.URL)
			http.NotFound(w, r)
			return
		}
		var match *sub2APIBalanceHistoryRecord
		for i := range history {
			if history[i].Code == r.URL.Query().Get("code") {
				match = &history[i]
			}
		}
		writeSub2APISuccess(t, w, d1ExactHistoryData(t, match))
	}, time.Second)
	matches, err := client.FinancialBalanceHistoryByCodes(context.Background(), 41, []string{"opl:history:10000"})
	if err != nil || requests != 1 || len(matches) != 1 || matches["opl:history:10000"].ValueUSDMicros != -1 {
		t.Fatalf("matches=%#v requests=%d err=%v", matches, requests, err)
	}
}

func TestSub2APIFinancialBalanceHistoryByCodesRejectsUnconfirmedOrUnsupportedEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		body   string
		status int
	}{
		{name: "old route missing", status: http.StatusNotFound},
		{name: "old dynamic route rejects code", status: http.StatusBadRequest},
		{name: "missing marker", body: `{"redeem_code":null}`},
		{name: "missing record field", body: `{"lookup":"exact_code_v1"}`},
		{name: "wrong marker", body: `{"lookup":"search_v1","redeem_code":null}`},
		{name: "wrong code", body: `{"lookup":"exact_code_v1","redeem_code":{"code":"opl:another","type":"balance","value":-1,"status":"used","used_by":41,"used_at":"2026-07-16T00:01:00Z","created_at":"2026-07-16T00:00:00Z"}}`},
		{name: "other owner", body: `{"lookup":"exact_code_v1","redeem_code":{"code":"opl:target","type":"balance","value":-1,"status":"used","used_by":42,"used_at":"2026-07-16T00:01:00Z","created_at":"2026-07-16T00:00:00Z"}}`},
		{name: "unsettled", body: `{"lookup":"exact_code_v1","redeem_code":{"code":"opl:target","type":"balance","value":-1,"status":"unused","created_at":"2026-07-16T00:00:00Z"}}`},
		{name: "fractional micro", body: `{"lookup":"exact_code_v1","redeem_code":{"code":"opl:target","type":"balance","value":-0.0000001,"status":"used","used_by":41,"used_at":"2026-07-16T00:01:00Z","created_at":"2026-07-16T00:00:00Z"}}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			requests := 0
			client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/login" {
					writeD1Sub2APILogin(t, w)
					return
				}
				requests++
				if scenario.status != 0 {
					http.Error(w, "unavailable", scenario.status)
					return
				}
				writeSub2APISuccess(t, w, json.RawMessage(scenario.body))
			}, time.Second)
			if _, err := client.FinancialBalanceHistoryByCodes(context.Background(), 41, []string{"opl:target"}); err == nil {
				t.Fatal("unconfirmed or unsupported financial evidence accepted")
			}
			if requests != 1 {
				t.Fatalf("unexpected fallback/mutation requests=%d", requests)
			}
		})
	}
}

func TestSub2APIFinancialConfirmationRequiresAppliedMoney(t *testing.T) {
	for _, sign := range []int64{-1, 1} {
		for _, scenario := range []struct {
			name    string
			applied *int64
			want    error
		}{
			{name: "historical amount unknown", want: ErrSub2APIChargeUnknown},
			{name: "only part of requested money applied", applied: func() *int64 { v := sign * 10_000_000; return &v }(), want: ErrSub2APIChargeConflict},
		} {
			t.Run(fmt.Sprintf("%d/%s", sign, scenario.name), func(t *testing.T) {
				code := "opl:applied-proof"
				record := d1HistoryRecord(code, sign*50_000_000, 41)
				record.BalanceAppliedValue = nil
				if scenario.applied != nil {
					number := json.Number(string(usdMicrosJSON(*scenario.applied)))
					record.BalanceAppliedValue = &number
				}
				client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v1/auth/login":
						writeD1Sub2APILogin(t, w)
					case "/api/v1/admin/redeem-codes/create-and-redeem":
						writeSub2APISuccess(t, w, struct {
							RedeemCode sub2APIBalanceHistoryRecord `json:"redeem_code"`
						}{record})
					case "/api/v1/admin/redeem-codes/by-code":
						writeSub2APISuccess(t, w, d1ExactHistoryData(t, &record))
					default:
						t.Errorf("unexpected financial path %s", r.URL)
						http.NotFound(w, r)
					}
				}, time.Second)
				var err error
				if sign < 0 {
					_, err = client.Charge(context.Background(), Sub2APIChargeInput{UserID: 41, Code: code, ChargeUSDMicros: 50_000_000})
				} else {
					_, err = client.Refund(context.Background(), Sub2APIRefundInput{UserID: 41, Code: code, RefundUSDMicros: 50_000_000})
				}
				if !errors.Is(err, scenario.want) {
					t.Fatalf("write response confirmed unverified money: %v", err)
				}
				if _, err := client.FinancialBalanceHistoryByCodes(context.Background(), 41, []string{code}); !errors.Is(err, scenario.want) {
					t.Fatalf("exact response confirmed unverified money: %v", err)
				}
			})
		}
	}
}
