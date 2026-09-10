package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func d1HistoryRecord(code string, valueUSDMicros, userID int64) sub2APIBalanceHistoryRecord {
	value := json.Number(string(usdMicrosJSON(valueUSDMicros)))
	created := time.Date(2026, 7, 16, 0, 0, 0, 0, time.UTC)
	used := created.Add(time.Minute)
	return sub2APIBalanceHistoryRecord{Code: "upstream-" + code, Notes: sub2APIBalanceAdjustmentNotePrefix + code, Type: "admin_balance", Value: &value, Status: "used", UsedBy: &userID, UsedAt: &used, CreatedAt: &created}
}

func writeD1Sub2APILogin(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	writeSub2APISuccess(t, w, struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}{"access", "refresh"})
}

func writeD1HistoryPage(t *testing.T, w http.ResponseWriter, r *http.Request, records []sub2APIBalanceHistoryRecord) {
	t.Helper()
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if page <= 0 || size <= 0 {
		t.Fatalf("invalid pagination %s", r.URL)
	}
	pages := max(1, (len(records)+size-1)/size)
	start := min((page-1)*size, len(records))
	end := min(start+size, len(records))
	writeSub2APISuccess(t, w, sub2APIBalanceHistoryRecordsPage{Items: records[start:end], Total: int64(len(records)), Page: page, PageSize: size, Pages: pages})
}

func TestSub2APIFinancialHistoryUsesNativeExactNotesAndAllPages(t *testing.T) {
	records := make([]sub2APIBalanceHistoryRecord, 201)
	for index := range records {
		records[index] = d1HistoryRecord(fmt.Sprintf("other-%d", index), -1, 41)
	}
	records[200] = d1HistoryRecord("opl:target", -52_580_000, 41)
	// A substring or a note with additional text is not the same operation.
	records[0].Notes = "prefix " + records[200].Notes
	records[1].Notes = records[200].Notes + " suffix"
	requests := 0
	client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			writeD1Sub2APILogin(t, w)
			return
		}
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/admin/users/41/balance-history" {
			t.Errorf("unexpected route %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		requests++
		if r.URL.Query().Get("type") == "admin_balance" {
			writeD1HistoryPage(t, w, r, records)
		} else {
			writeD1HistoryPage(t, w, r, nil)
		}
	}, time.Second)
	facts, err := client.FinancialBalanceHistoryByCodes(context.Background(), 41, []string{"opl:target", "opl:missing", "opl:target"})
	if err != nil || len(facts) != 1 || facts["opl:target"].Code != "opl:target" || facts["opl:target"].Type != "balance" || facts["opl:target"].ValueUSDMicros != -52_580_000 {
		t.Fatalf("facts=%#v err=%v", facts, err)
	}
	if requests != 4 {
		t.Fatalf("history requests=%d, want all three native pages and one historical page", requests)
	}
}

func TestSub2APIFinancialHistoryRejectsConflictingOwnerEvidence(t *testing.T) {
	for _, scenario := range []struct {
		name      string
		mutate    func(*sub2APIBalanceHistoryRecord)
		duplicate bool
	}{
		{name: "wrong user", mutate: func(record *sub2APIBalanceHistoryRecord) { other := int64(42); record.UsedBy = &other }},
		{name: "wrong type", mutate: func(record *sub2APIBalanceHistoryRecord) { record.Type = "balance" }},
		{name: "unused", mutate: func(record *sub2APIBalanceHistoryRecord) { record.Status = "unused" }},
		{name: "missing amount", mutate: func(record *sub2APIBalanceHistoryRecord) { record.Value = nil }},
		{name: "fractional micro", mutate: func(record *sub2APIBalanceHistoryRecord) { value := json.Number("-1.0000001"); record.Value = &value }},
		{name: "zero amount", mutate: func(record *sub2APIBalanceHistoryRecord) { value := json.Number("0"); record.Value = &value }},
		{name: "missing used time", mutate: func(record *sub2APIBalanceHistoryRecord) { record.UsedAt = nil }},
		{name: "missing original identity", mutate: func(record *sub2APIBalanceHistoryRecord) { record.Code = "" }},
		{name: "two actual adjustments", duplicate: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			record := d1HistoryRecord("opl:target", -1_000_000, 41)
			if scenario.mutate != nil {
				scenario.mutate(&record)
			}
			records := []sub2APIBalanceHistoryRecord{record}
			if scenario.duplicate {
				other := record
				other.Code = "second-upstream-code"
				records = append(records, other)
			}
			client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/login" {
					writeD1Sub2APILogin(t, w)
					return
				}
				if r.Method != http.MethodGet {
					t.Errorf("history made a write: %s", r.Method)
				}
				writeD1HistoryPage(t, w, r, records)
			}, time.Second)
			if _, err := client.FinancialBalanceHistoryByCodes(context.Background(), 41, []string{"opl:target"}); !errors.Is(err, ErrSub2APIChargeConflict) {
				t.Fatalf("conflicting evidence accepted: %v", err)
			}
		})
	}
}

func TestSub2APIFinancialHistoryRepeatingOneOriginalRecordIsNotAnotherAdjustment(t *testing.T) {
	record := d1HistoryRecord("opl:target", -1, 41)
	client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/auth/login" {
			writeD1Sub2APILogin(t, w)
			return
		}
		if r.URL.Query().Get("type") == "admin_balance" {
			writeD1HistoryPage(t, w, r, []sub2APIBalanceHistoryRecord{record, record})
			return
		}
		writeD1HistoryPage(t, w, r, nil)
	}, time.Second)
	facts, err := client.FinancialBalanceHistoryByCodes(context.Background(), 41, []string{"opl:target"})
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts=%#v err=%v", facts, err)
	}
}

func TestSub2APIFinancialHistoryDoesNotInferHistoricalAppliedMoney(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		applied *json.Number
		want    error
	}{
		{name: "official unverified record", want: ErrSub2APIChargeUnknown},
		{name: "clamped original", applied: func() *json.Number { n := json.Number("-40"); return &n }(), want: ErrSub2APIChargeConflict},
		{name: "retained confirmed original", applied: func() *json.Number { n := json.Number("-50"); return &n }()},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			record := d1HistoryRecord("old-op", -50_000_000, 41)
			record.Code, record.Type, record.Notes, record.BalanceAppliedValue = "old-op", "balance", "", scenario.applied
			client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/login" {
					writeD1Sub2APILogin(t, w)
					return
				}
				if r.URL.Query().Get("type") == "balance" {
					writeD1HistoryPage(t, w, r, []sub2APIBalanceHistoryRecord{record})
					return
				}
				writeD1HistoryPage(t, w, r, nil)
			}, time.Second)
			facts, err := client.FinancialBalanceHistoryByCodes(context.Background(), 41, []string{"old-op"})
			if scenario.want != nil {
				if !errors.Is(err, scenario.want) {
					t.Fatalf("error=%v want=%v", err, scenario.want)
				}
				return
			}
			if err != nil || facts["old-op"].ValueUSDMicros != -50_000_000 {
				t.Fatalf("facts=%#v err=%v", facts, err)
			}
		})
	}
}

func TestSub2APIFinancialHistoryUnavailableIsNotEmptyEvidence(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusServiceUnavailable} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/login" {
					writeD1Sub2APILogin(t, w)
					return
				}
				http.Error(w, "unavailable", status)
			}, time.Second)
			if _, err := client.FinancialBalanceHistoryByCodes(context.Background(), 41, []string{"opl:target"}); err == nil {
				t.Fatal("unavailable history returned no error")
			}
		})
	}
}

func TestSub2APIAdjustmentNeverRepostsOrRefreshesAfterDispatch(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusConflict, http.StatusInternalServerError, http.StatusTemporaryRedirect} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			posts, otherCalls := 0, 0
			client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/auth/login" {
					writeD1Sub2APILogin(t, w)
					return
				}
				if r.URL.Path == "/api/v1/admin/users/41/balance" {
					posts++
					w.Header().Set("Location", "/must-not-follow")
					w.WriteHeader(status)
					return
				}
				otherCalls++
				http.Error(w, "unexpected", http.StatusInternalServerError)
			}, time.Second)
			_, err := client.Charge(context.Background(), Sub2APIChargeInput{UserID: 41, Code: "opl:once", ChargeUSDMicros: 1_000_000})
			if !errors.Is(err, ErrSub2APIChargeUnknown) || posts != 1 || otherCalls != 0 {
				t.Fatalf("err=%v posts=%d other=%d", err, posts, otherCalls)
			}
		})
	}
}

func TestSub2APIRefundRequiresReadOnlyRebatePolicy(t *testing.T) {
	for _, scenario := range []struct {
		name, settings string
		allowed        bool
	}{
		{name: "all disabled", settings: `{"affiliate_enabled":false,"affiliate_admin_recharge_enabled":false}`, allowed: true},
		{name: "overall disabled", settings: `{"affiliate_enabled":false,"affiliate_admin_recharge_enabled":true}`, allowed: true},
		{name: "admin disabled", settings: `{"affiliate_enabled":true,"affiliate_admin_recharge_enabled":false}`, allowed: true},
		{name: "rebate enabled", settings: `{"affiliate_enabled":true,"affiliate_admin_recharge_enabled":true}`},
		{name: "missing", settings: `{}`},
		{name: "missing overall", settings: `{"affiliate_admin_recharge_enabled":false}`},
		{name: "invalid type", settings: `{"affiliate_enabled":false,"affiliate_admin_recharge_enabled":"false"}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			posts := 0
			client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/auth/login":
					writeD1Sub2APILogin(t, w)
				case "/api/v1/admin/settings":
					if r.Method != http.MethodGet {
						t.Errorf("settings mutation")
					}
					writeSub2APISuccess(t, w, json.RawMessage(scenario.settings))
				case "/api/v1/admin/users/41/balance":
					posts++
					writeSub2APISuccess(t, w, struct {
						ID int64 `json:"id"`
					}{41})
				default:
					t.Errorf("unexpected route %s", r.URL)
					http.NotFound(w, r)
				}
			}, time.Second)
			_, err := client.Refund(context.Background(), Sub2APIRefundInput{UserID: 41, Code: "opl:refund", RefundUSDMicros: 1_000_000})
			if scenario.allowed {
				if err != nil || posts != 1 {
					t.Fatalf("err=%v posts=%d", err, posts)
				}
			} else if err == nil || posts != 0 {
				t.Fatalf("unsafe refund: err=%v posts=%d", err, posts)
			}
		})
	}
}

func TestSub2APIAdjustmentConnectionLossDoesNotRepeatDebit(t *testing.T) {
	posts := 0
	client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			writeD1Sub2APILogin(t, w)
		case "/api/v1/admin/users/41":
			writeSub2APISuccess(t, w, struct {
				ID      int64  `json:"id"`
				Balance int    `json:"balance"`
				Status  string `json:"status"`
			}{41, 10, "active"})
		case "/api/v1/admin/users/41/balance":
			var input struct {
				Balance json.Number `json:"balance"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
				return
			}
			posts++
			// Close a reused connection after receiving the complete debit body.
			// net/http may replay an Idempotency-Key POST when GetBody is present.
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
		default:
			t.Errorf("unexpected request %s", r.URL)
		}
	}, time.Second)
	if _, err := client.Balance(context.Background(), 41); err != nil {
		t.Fatal(err)
	}
	_, err := client.Charge(context.Background(), Sub2APIChargeInput{UserID: 41, Code: "opl:lost-connection", ChargeUSDMicros: 1_000_000})
	if !errors.Is(err, ErrSub2APIChargeUnknown) || posts != 1 {
		t.Fatalf("err=%v debit requests=%d", err, posts)
	}
}
