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
	"strconv"
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

func TestD1RealSub2APIChargeRefundAndExactHistory(t *testing.T) {
	client := d1RealSub2API(t, nil)
	userID := d1RealUser(t, client, 30)
	chargeCode := fmt.Sprintf("d1-go-%d-charge", userID)
	charge, err := client.Charge(context.Background(), Sub2APIChargeInput{UserID: userID, Code: chargeCode, ChargeUSDMicros: 10_000_000})
	if err != nil || charge.Status != "used" {
		t.Fatalf("charge=%#v err=%v", charge, err)
	}
	refundCode := fmt.Sprintf("d1-go-%d-refund", userID)
	refund := Sub2APIRefundInput{UserID: userID, Code: refundCode, RefundUSDMicros: 3_000_000}
	var workers sync.WaitGroup
	for range 10 {
		workers.Add(1)
		go func() { defer workers.Done(); _, _ = client.Refund(context.Background(), refund) }()
	}
	workers.Wait()
	result, err := client.Refund(context.Background(), refund)
	if err != nil || result.Status != "used" {
		t.Fatalf("refund replay=%#v err=%v", result, err)
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
	if _, err := client.FinancialBalanceHistoryByCodes(context.Background(), otherID, []string{refundCode}); !errors.Is(err, ErrSub2APIChargeConflict) {
		t.Fatalf("wrong owner lookup err=%v", err)
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
	if request.Method == http.MethodPost && request.URL.Path == "/api/v1/admin/redeem-codes/create-and-redeem" && request.Header.Get("Idempotency-Key") == d.code {
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
	if _, err := restarted.Refund(context.Background(), input); err != nil {
		t.Fatalf("same-code replay: %v", err)
	}
	balance, err := restarted.Balance(context.Background(), userID)
	if err != nil || balance.USDMicros != 13_000_000 {
		t.Fatalf("wallet after replay=%#v err=%v", balance, err)
	}
}

func TestD1RealSub2APIRejectsHistoricalUnverifiedDebit(t *testing.T) {
	client := d1RealSub2API(t, nil)
	code := os.Getenv("OPL_D1_REAL_SUB2API_LEGACY_CODE")
	userID, err := strconv.ParseInt(os.Getenv("OPL_D1_REAL_SUB2API_LEGACY_USER_ID"), 10, 64)
	if code == "" || err != nil || userID <= 0 {
		t.Fatal("a pre-upgrade unverified debit is required")
	}
	if _, err := client.FinancialBalanceHistoryByCodes(context.Background(), userID, []string{code}); !errors.Is(err, ErrSub2APIChargeConflict) {
		t.Fatalf("historical unverified debit accepted: %v", err)
	}
	if _, err := client.Charge(context.Background(), Sub2APIChargeInput{UserID: userID, Code: code, ChargeUSDMicros: 50_000_000, Notes: "isolated D1 verification"}); !errors.Is(err, ErrSub2APIChargeUnknown) {
		t.Fatalf("historical code replay accepted: %v", err)
	}
}
