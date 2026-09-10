//go:build d1_sub2api_runtime

package clients

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// These tests require an isolated real Sub2API, never a production wallet.
func d1RealSub2API(t *testing.T, transport http.RoundTripper) *Sub2APIHTTPClient {
	t.Helper()
	base := os.Getenv("OPL_D1_REAL_SUB2API_BASE_URL")
	if base == "" {
		t.Fatal("isolated real Sub2API was not configured")
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" {
		t.Fatal("real D1 verification requires a loopback isolated Sub2API")
	}
	email, password := os.Getenv("OPL_D1_REAL_SUB2API_ADMIN_EMAIL"), os.Getenv("OPL_D1_REAL_SUB2API_ADMIN_PASSWORD")
	if email == "" || password == "" {
		t.Fatal("isolated Sub2API admin credentials are required")
	}
	client, err := NewSub2APIHTTPClient(Sub2APIConfig{BaseURL: base, AdminEmail: email, AdminPassword: password, Timeout: 10 * time.Second}, &http.Client{Transport: transport, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func d1RealUser(t *testing.T, client *Sub2APIHTTPClient, balance int64) int64 {
	t.Helper()
	input := struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Balance  int64  `json:"balance"`
	}{
		Email: fmt.Sprintf("d1-go-%d@example.test", time.Now().UnixNano()), Password: "isolated-d1-local-password", Balance: balance,
	}
	body, err := client.doAuthenticated(context.Background(), http.MethodPost, "/api/v1/admin/users", input, fmt.Sprintf("d1-user-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("create isolated user: %v", err)
	}
	var user struct {
		ID int64 `json:"id"`
	}
	if err := decodeSub2APIEnvelope(body, &user); err != nil || user.ID <= 0 {
		t.Fatalf("isolated user decode: %v", err)
	}
	return user.ID
}

func TestD1RealSub2APIChargeRefundAndNativeHistory(t *testing.T) {
	client := d1RealSub2API(t, nil)
	userID := d1RealUser(t, client, 30)
	chargeCode := fmt.Sprintf("d1-go-%d-charge", userID)
	charge, err := client.Charge(context.Background(), Sub2APIChargeInput{UserID: userID, Code: chargeCode, ChargeUSDMicros: 10_000_000})
	if err != nil || charge.Status != "used" {
		t.Fatalf("charge=%#v err=%v", charge, err)
	}
	refundCode := fmt.Sprintf("d1-go-%d-refund", userID)
	refund := Sub2APIRefundInput{UserID: userID, Code: refundCode, RefundUSDMicros: 3_000_000}
	result, err := client.Refund(context.Background(), refund)
	if err != nil || result.Status != "used" {
		t.Fatalf("refund=%#v err=%v", result, err)
	}
	evidence, err := client.FinancialBalanceHistoryByCodes(context.Background(), userID, []string{chargeCode, refundCode, "opl:wallet-adjustment:" + strings.Repeat("a", 24) + ":v1"})
	if err != nil || len(evidence) != 2 || evidence[chargeCode].ValueUSDMicros != -10_000_000 || evidence[refundCode].ValueUSDMicros != 3_000_000 {
		t.Fatalf("exact adjustment evidence=%#v err=%v", evidence, err)
	}
	balance, err := client.Balance(context.Background(), userID)
	if err != nil || balance.USDMicros != 23_000_000 {
		t.Fatalf("wallet=%#v err=%v", balance, err)
	}
	otherID := d1RealUser(t, client, 5)
	if other, err := client.FinancialBalanceHistoryByCodes(context.Background(), otherID, []string{refundCode}); err != nil || len(other) != 0 {
		t.Fatalf("wrong owner lookup facts=%#v err=%v", other, err)
	}
}

type d1DropCommittedResponse struct {
	base http.RoundTripper
	code string
	once sync.Once
}

func (d *d1DropCommittedResponse) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := d.base.RoundTrip(request)
	if err != nil {
		return response, err
	}
	drop := false
	if request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/balance") && request.Header.Get("Idempotency-Key") == d.code {
		d.once.Do(func() { drop = true })
	}
	if !drop {
		return response, nil
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return nil, io.ErrUnexpectedEOF
}

func TestD1RealSub2APILostRefundResponseRecoversWithoutSecondCredit(t *testing.T) {
	initial := d1RealSub2API(t, nil)
	userID := d1RealUser(t, initial, 10)
	code := fmt.Sprintf("d1-go-%d-lost", userID)
	dropped := d1RealSub2API(t, &d1DropCommittedResponse{base: http.DefaultTransport, code: code})
	input := Sub2APIRefundInput{UserID: userID, Code: code, RefundUSDMicros: 3_000_000}
	if _, err := dropped.Refund(context.Background(), input); !errors.Is(err, ErrSub2APIChargeUnknown) {
		t.Fatalf("lost response err=%v", err)
	}
	restarted := d1RealSub2API(t, nil)
	facts, err := restarted.FinancialBalanceHistoryByCodes(context.Background(), userID, []string{code})
	if err != nil || len(facts) != 1 || facts[code].ValueUSDMicros != 3_000_000 {
		t.Fatalf("restarted exact facts=%#v err=%v", facts, err)
	}
	balance, err := restarted.Balance(context.Background(), userID)
	if err != nil || balance.USDMicros != 13_000_000 {
		t.Fatalf("wallet after read-only recovery=%#v err=%v", balance, err)
	}
}

func TestD1RealSub2APIRejectsOfficialHistoricalUnverifiedDebit(t *testing.T) {
	client := d1RealSub2API(t, nil)
	userID := d1RealUser(t, client, 7)
	code := fmt.Sprintf("d1-old-%d", userID)
	// Exercise the official retained redeem path in the isolated wallet. Its
	// used record does not prove that this requested ten-dollar debit applied.
	input := struct {
		Code   string `json:"code"`
		Type   string `json:"type"`
		Value  int64  `json:"value"`
		UserID int64  `json:"user_id"`
	}{code, "balance", -10, userID}
	if _, err := client.doAuthenticated(context.Background(), http.MethodPost, "/api/v1/admin/redeem-codes/create-and-redeem", input, code); err != nil {
		t.Fatalf("create isolated legacy record: %v", err)
	}
	if _, err := client.FinancialBalanceHistoryByCodes(context.Background(), userID, []string{code}); !errors.Is(err, ErrSub2APIChargeUnknown) {
		t.Fatalf("historical unverified debit accepted: %v", err)
	}
}

func TestD1RealSub2APIConcurrentDebitsApplyOnlyWholeAmounts(t *testing.T) {
	client := d1RealSub2API(t, nil)
	userID := d1RealUser(t, client, 10)
	codes := []string{fmt.Sprintf("d1-%d-one", userID), fmt.Sprintf("d1-%d-two", userID)}
	var workers sync.WaitGroup
	for _, code := range codes {
		workers.Add(1)
		go func() {
			defer workers.Done()
			_, _ = client.Charge(context.Background(), Sub2APIChargeInput{UserID: userID, Code: code, ChargeUSDMicros: 7_000_000})
		}()
	}
	workers.Wait()
	balance, err := client.Balance(context.Background(), userID)
	if err != nil || balance.USDMicros != 3_000_000 {
		t.Fatalf("wallet=%#v err=%v", balance, err)
	}
	facts, err := client.FinancialBalanceHistoryByCodes(context.Background(), userID, codes)
	if err != nil || len(facts) != 1 {
		t.Fatalf("whole debit facts=%#v err=%v", facts, err)
	}
	for _, fact := range facts {
		if fact.ValueUSDMicros != -7_000_000 {
			t.Fatalf("partial debit=%#v", fact)
		}
	}
}
