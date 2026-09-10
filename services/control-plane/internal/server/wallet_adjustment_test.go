package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type walletAdjustmentSub2API struct {
	*testSub2APIClient
	history         []clients.Sub2APIBalanceHistoryEntry
	historyErr      error
	balanceErr      error
	balanceResult   *clients.Sub2APIBalance
	adjustmentErr   error
	applyBeforeErr  bool
	chargeCalls     int
	refundCalls     int
	writeCodes      []string
	balanceCalled   chan struct{}
	afterAdjustment func()
}

func (s *walletAdjustmentSub2API) Balance(ctx context.Context, userID int64) (clients.Sub2APIBalance, error) {
	if s.balanceCalled != nil {
		select {
		case s.balanceCalled <- struct{}{}:
		default:
		}
	}
	if s.balanceErr != nil {
		return clients.Sub2APIBalance{}, s.balanceErr
	}
	if s.balanceResult != nil {
		return *s.balanceResult, nil
	}
	return s.testSub2APIClient.Balance(ctx, userID)
}

func (s *walletAdjustmentSub2API) Charge(_ context.Context, input clients.Sub2APIChargeInput) (clients.Sub2APICharge, error) {
	s.chargeCalls++
	s.writeCodes = append(s.writeCodes, input.Code)
	if s.adjustmentErr != nil {
		if s.applyBeforeErr {
			s.applyBeforeErr = false
			s.balance -= input.ChargeUSDMicros
			s.appendHistory(input.Code, -input.ChargeUSDMicros, input.UserID)
		}
		return clients.Sub2APICharge{}, s.adjustmentErr
	}
	s.balance -= input.ChargeUSDMicros
	s.appendHistory(input.Code, -input.ChargeUSDMicros, input.UserID)
	if s.afterAdjustment != nil {
		s.afterAdjustment()
	}
	return clients.Sub2APICharge{Code: input.Code, UserID: input.UserID, ChargeUSDMicros: input.ChargeUSDMicros, Status: "used"}, nil
}

func (s *walletAdjustmentSub2API) Refund(_ context.Context, input clients.Sub2APIRefundInput) (clients.Sub2APIRefund, error) {
	s.refundCalls++
	s.writeCodes = append(s.writeCodes, input.Code)
	if s.adjustmentErr != nil {
		if s.applyBeforeErr {
			s.applyBeforeErr = false
			s.balance += input.RefundUSDMicros
			s.appendHistory(input.Code, input.RefundUSDMicros, input.UserID)
		}
		return clients.Sub2APIRefund{}, s.adjustmentErr
	}
	s.balance += input.RefundUSDMicros
	s.appendHistory(input.Code, input.RefundUSDMicros, input.UserID)
	if s.afterAdjustment != nil {
		s.afterAdjustment()
	}
	return clients.Sub2APIRefund{Code: input.Code, UserID: input.UserID, RefundUSDMicros: input.RefundUSDMicros, Status: "used"}, nil
}

func (s *walletAdjustmentSub2API) appendHistory(code string, value, userID int64) {
	now := operatorProjectionTime
	s.history = append(s.history, clients.Sub2APIBalanceHistoryEntry{
		Code: code, Type: "balance", ValueUSDMicros: value, Status: "used", UsedBy: &userID, UsedAt: &now, CreatedAt: now,
	})
}

func (s *walletAdjustmentSub2API) Usage(context.Context, clients.Sub2APIUsageQuery) (clients.Sub2APIUsagePage, error) {
	return clients.Sub2APIUsagePage{}, nil
}

func (s *walletAdjustmentSub2API) UsageStats(context.Context, clients.Sub2APIUsageStatsQuery) (clients.Sub2APIUsageStats, error) {
	return clients.Sub2APIUsageStats{}, nil
}

func (s *walletAdjustmentSub2API) FinancialBalanceHistoryByCodes(_ context.Context, _ int64, codes []string) (map[string]clients.Sub2APIBalanceHistoryEntry, error) {
	if s.historyErr != nil {
		return nil, s.historyErr
	}
	matches := make(map[string]clients.Sub2APIBalanceHistoryEntry)
	for _, entry := range s.history {
		for _, code := range codes {
			if entry.Code == code {
				matches[code] = entry
			}
		}
	}
	return matches, nil
}

type walletAdjustmentLedger struct {
	fakeLedgerClient
	receipts []clients.ReceiptInput
}

func (l *walletAdjustmentLedger) RecordReceipt(_ context.Context, input clients.ReceiptInput, _ string) (clients.Receipt, error) {
	l.receipts = append(l.receipts, input)
	return clients.Receipt{ReceiptInput: input, ReceiptID: "receipt-wallet-adjustment"}, nil
}

type walletAdjustmentFixture struct {
	server http.Handler
	store  *memoryTableStore
	remote *walletAdjustmentSub2API
	ledger *walletAdjustmentLedger
}

type walletAdjustmentAuditOnceStore struct {
	*memoryTableStore
	failAudit bool
}

func (s *walletAdjustmentAuditOnceStore) SaveAuditEvent(ctx context.Context, event map[string]any) error {
	if s.failAudit {
		s.failAudit = false
		return errors.New("audit temporarily unavailable")
	}
	return s.memoryTableStore.SaveAuditEvent(ctx, event)
}

type walletAdjustmentRecoveryAuditOnceStore struct {
	*memoryTableStore
	failRecoveryAudit bool
}

type walletAdjustmentDiagnosticPersistStore struct {
	*memoryTableStore
	failed bool
}

type walletAdjustmentV2PersistStore struct {
	*memoryTableStore
	rejectSupersession bool
}

func (s *walletAdjustmentDiagnosticPersistStore) SaveWalletAdjustment(ctx context.Context, operationID string, operation walletAdjustmentOperation) (walletAdjustmentOperation, error) {
	if !s.failed && operation.UpstreamFailure != nil {
		s.failed = true
		return operation, errors.New("diagnostic persist unavailable")
	}
	return s.memoryTableStore.SaveWalletAdjustment(ctx, operationID, operation)
}

func (s *walletAdjustmentV2PersistStore) SaveWalletAdjustment(ctx context.Context, operationID string, operation walletAdjustmentOperation) (walletAdjustmentOperation, error) {
	if s.rejectSupersession && operation.RedeemCodeVersion == "v2" && operation.LegacySupersession == "v2_adopted" {
		return operation, errors.New("v2 supersession persist unavailable")
	}
	return s.memoryTableStore.SaveWalletAdjustment(ctx, operationID, operation)
}

func (s *walletAdjustmentRecoveryAuditOnceStore) SaveAuditEvent(ctx context.Context, event map[string]any) error {
	if s.failRecoveryAudit && stringValue(event["action"]) == "gateway.wallet_adjustment.recover" {
		s.failRecoveryAudit = false
		return errors.New("recovery audit temporarily unavailable")
	}
	return s.memoryTableStore.SaveAuditEvent(ctx, event)
}

func newWalletAdjustmentFixture(t *testing.T) walletAdjustmentFixture {
	t.Helper()
	store := newMemoryTableStore()
	seedOperatorProjectionAccount(t, store, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	remote := &walletAdjustmentSub2API{testSub2APIClient: &testSub2APIClient{
		balance: 100_000_000,
		charges: map[string]int64{},
		identities: map[string]clients.Sub2APIIdentity{
			"alpha@example.com": {ID: 41, Email: "alpha@example.com", Status: "active"},
		},
		passwords: map[string]string{"alpha@example.com": "CorrectHorseBatteryStaple!"},
	}}
	ledger := &walletAdjustmentLedger{}
	server, err := NewPersistentServer(controlplane.NewService(ledger, &fakeFabricClient{}, remote), store)
	if err != nil {
		t.Fatal(err)
	}
	return walletAdjustmentFixture{server: server, store: store, remote: remote, ledger: ledger}
}

func sendWalletAdjustmentRequest(t *testing.T, fixture walletAdjustmentFixture, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	return requestWithMutationKeyForTest(t, fixture.server, reservedOperatorSessionForTest(t, fixture.server), http.MethodPost,
		"/api/operator/accounts/acct-alpha/wallet-adjustments", body, key)
}

func sendWalletAdjustmentRecoveryRequest(t *testing.T, fixture walletAdjustmentFixture, operationID, key string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"accountId":"acct-alpha","evidenceRef":"case-20260722-local"}`)
	return requestWithMutationKeyForTest(t, fixture.server, reservedOperatorSessionForTest(t, fixture.server), http.MethodPost,
		"/api/operator/wallet-adjustments/"+operationID+"/recover", body, key)
}

func decodeWalletAdjustmentResponse(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode wallet adjustment: %v: %s", err, response.Body.String())
	}
	return body
}

func seedLegacyWalletAdjustment(t *testing.T, fixture walletAdjustmentFixture, operationID string) {
	t.Helper()
	now := operatorProjectionTime.Format(time.RFC3339Nano)
	operation := walletAdjustmentOperation{
		RequestHash: "legacy-request-hash", Phase: "authoritative_readback", AccountID: "acct-alpha", Sub2APIUserID: 41,
		Kind: "recharge", AmountUSDMicros: 60_000_000, AmountUSD: "60.00", Reason: "local pilot credit", ActorUserID: "usr-admin",
		AdjustmentAttempted: true, BeforeBalanceKnown: true, BeforeBalanceMicros: 100_000_000, BeforeBalanceReadAt: now,
		ErrorCode: "authoritative_readback_unavailable", CreatedAt: now, UpdatedAt: now, Status: "manual_review",
	}
	app := fixture.server.(*controlPlaneHTTPHandler).app
	if err := app.persistWalletAdjustment(context.Background(), operationID, &operation); err != nil {
		t.Fatalf("seed legacy wallet adjustment: %v", err)
	}
}

func TestWalletAdjustmentRedeemCodeV2(t *testing.T) {
	operationID := "wallet-adjustment-7f902adbca8d1780e7"
	code := walletAdjustmentRedeemCode(operationID)
	if len(code) != 32 || !regexp.MustCompile(`^opl:[0-9a-f]{28}$`).MatchString(code) {
		t.Fatalf("v2 code shape = %q length=%d", code, len(code))
	}
	if code != walletAdjustmentRedeemCode(operationID) || code == walletAdjustmentRedeemCode(operationID+"-different") {
		t.Fatal("v2 code must be stable and operation-bound")
	}
	if code == monthlyRedeemCode("local", operationID) || code == monthlyRefundCode("local", operationID) {
		t.Fatal("wallet adjustment identity must be isolated from monthly charge and refund")
	}
	legacyCode := "opl:wallet-adjustment:" + stableID(operationID)[:24] + ":v1"
	if len(legacyCode) != 49 {
		t.Fatalf("legacy code length = %d, want 49", len(legacyCode))
	}
}

func TestWalletAdjustmentAuth(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	body := `{"kind":"debit","amountUsd":"1.00","reason":"manual correction","confirmationAccountId":"acct-alpha"}`

	anonymous := httptest.NewRecorder()
	fixture.server.ServeHTTP(anonymous, httptest.NewRequest(http.MethodPost, "/api/operator/accounts/acct-alpha/wallet-adjustments", strings.NewReader(body)))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d body=%s", anonymous.Code, anonymous.Body.String())
	}

	owner := tenantOwnerSessionForTest(t, fixture.server)
	forbidden := requestWithMutationKeyForTest(t, fixture.server, owner, http.MethodPost, "/api/operator/accounts/acct-alpha/wallet-adjustments", body, "wallet-auth-owner")
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("owner status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}
}

func TestWalletAdjustmentValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "unknown kind", body: `{"kind":"credit","amountUsd":"1.00","reason":"manual correction","confirmationAccountId":"acct-alpha"}`},
		{name: "fraction beyond micros", body: `{"kind":"debit","amountUsd":"1.0000001","reason":"manual correction","confirmationAccountId":"acct-alpha"}`},
		{name: "non positive", body: `{"kind":"debit","amountUsd":"0","reason":"manual correction","confirmationAccountId":"acct-alpha"}`},
		{name: "wrong confirmation", body: `{"kind":"debit","amountUsd":"1.00","reason":"manual correction","confirmationAccountId":"acct-beta"}`},
		{name: "refund missing link", body: `{"kind":"business_refund","amountUsd":"1.00","reason":"service recovery","confirmationAccountId":"acct-alpha"}`},
		{name: "unexpected field", body: `{"kind":"recharge","amountUsd":"1.00","reason":"manual correction","confirmationAccountId":"acct-alpha","sub2apiUserId":41}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newWalletAdjustmentFixture(t)
			response := sendWalletAdjustmentRequest(t, fixture, tc.body, "wallet-invalid-"+strings.ReplaceAll(tc.name, " ", "-"))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if fixture.remote.chargeCalls+fixture.remote.refundCalls != 0 {
				t.Fatalf("invalid request reached Sub2API: charge=%d refund=%d", fixture.remote.chargeCalls, fixture.remote.refundCalls)
			}
		})
	}
}

func TestWalletAdjustmentAmountUsesFullPositiveInt64MicrosRange(t *testing.T) {
	request := walletAdjustmentRequest{Kind: "recharge", AmountUSD: "9223372036854.775807", Reason: "manual correction", ConfirmationAccountID: "acct-alpha"}
	amount, ok := validWalletAdjustmentRequest(request, "acct-alpha", "wallet-max-int64")
	if !ok || amount != math.MaxInt64 {
		t.Fatalf("maximum int64 amount=%d ok=%t", amount, ok)
	}
	request.AmountUSD = "9223372036854.775808"
	if amount, ok := validWalletAdjustmentRequest(request, "acct-alpha", "wallet-overflow"); ok || amount != 0 {
		t.Fatalf("overflow amount=%d ok=%t", amount, ok)
	}
}

func TestWalletAdjustmentIdempotency(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	body := `{"kind":"debit","amountUsd":"1.250001","reason":"manual correction","confirmationAccountId":"acct-alpha"}`
	first := sendWalletAdjustmentRequest(t, fixture, body, "wallet-debit-once")
	if first.Code != http.StatusCreated {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	firstBody := decodeWalletAdjustmentResponse(t, first)
	replay := sendWalletAdjustmentRequest(t, fixture, body, "wallet-debit-once")
	if replay.Code != http.StatusOK {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	if string(mustJSON(firstBody)) != string(mustJSON(decodeWalletAdjustmentResponse(t, replay))) || fixture.remote.chargeCalls != 1 || len(fixture.ledger.receipts) != 1 {
		t.Fatalf("replay changed result or side effects: first=%#v calls=%d receipts=%d", firstBody, fixture.remote.chargeCalls, len(fixture.ledger.receipts))
	}

	conflict := sendWalletAdjustmentRequest(t, fixture, `{"kind":"debit","amountUsd":"2.00","reason":"manual correction","confirmationAccountId":"acct-alpha"}`, "wallet-debit-once")
	if conflict.Code != http.StatusConflict || fixture.remote.chargeCalls != 1 {
		t.Fatalf("conflict status=%d calls=%d body=%s", conflict.Code, fixture.remote.chargeCalls, conflict.Body.String())
	}

	operationID := stringValue(firstBody["operationId"])
	read := requestWithSession(t, fixture.server, reservedOperatorSessionForTest(t, fixture.server), http.MethodGet, "/api/operator/wallet-adjustments/"+operationID, "")
	if read.Code != http.StatusOK || string(mustJSON(decodeWalletAdjustmentResponse(t, read))) != string(mustJSON(firstBody)) {
		t.Fatalf("readback status=%d body=%s", read.Code, read.Body.String())
	}
}

func TestWalletAdjustmentUsesAccountWalletLock(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	fixture.remote.balanceCalled = make(chan struct{}, 1)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	unlock := app.lockResource("sub2api-wallet", "acct-alpha")
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response <- sendWalletAdjustmentRequest(t, fixture, `{"kind":"recharge","amountUsd":"1.00","reason":"pilot credit","confirmationAccountId":"acct-alpha"}`, "wallet-shared-lock")
	}()

	select {
	case <-fixture.remote.balanceCalled:
		unlock()
		t.Fatal("wallet adjustment reached Sub2API while the account wallet lock was held")
	case <-time.After(50 * time.Millisecond):
	}
	unlock()
	select {
	case result := <-response:
		if result.Code != http.StatusCreated {
			t.Fatalf("wallet adjustment status=%d body=%s", result.Code, result.Body.String())
		}
	case <-time.After(time.Second):
		t.Fatal("wallet adjustment did not resume after the account wallet lock was released")
	}
}

func TestWalletAdjustmentInsufficientBalanceReplayNeverBecomesDelayedDebit(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	fixture.remote.balance = 500_000
	body := `{"kind":"debit","amountUsd":"1.00","reason":"manual correction","confirmationAccountId":"acct-alpha"}`

	first := sendWalletAdjustmentRequest(t, fixture, body, "wallet-insufficient")
	fixture.remote.balance = 100_000_000
	replay := sendWalletAdjustmentRequest(t, fixture, body, "wallet-insufficient")
	operations, _ := fixture.store.ListRuntimeOperations(context.Background())
	if first.Code != http.StatusConflict || replay.Code != http.StatusConflict || fixture.remote.chargeCalls != 0 || len(operations) != 1 || operations[0]["status"] != "failed" {
		t.Fatalf("first=%d replay=%d chargeCalls=%d operations=%#v", first.Code, replay.Code, fixture.remote.chargeCalls, operations)
	}
}

func TestWalletAdjustmentRefundLink(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID, command.WorkspaceID = "workspace-launch-alpha", "acct-alpha", "usr-alpha", "ws-alpha"
	command.Sub2APIUserID, command.TotalChargeUSDMicros = 41, 5_000_000
	purchase, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	code := purchase.stringFact("sub2apiRedeemCode")
	charge := walletRefundCharge{Code: code, UserID: 41, AmountUSDMicros: 5_000_000, Status: "used"}
	purchase.raw["chargeConfirmation"] = mustJSON(charge)
	purchase.Status = "succeeded"
	row, err := workspaceLaunchReconcileOperationRow(purchase)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, fixture.store.SaveRuntimeOperation(context.Background(), row))
	fixture.remote.appendHistory(code, -5_000_000, 41)

	for i, amount := range []string{"3.00", "2.00"} {
		input := walletAdjustmentRequest{Kind: "business_refund", AmountUSD: amount, Reason: "service recovery", RelatedOperationID: purchase.ID, ConfirmationAccountID: "acct-alpha"}
		body := string(mustJSON(input))
		response := sendWalletAdjustmentRequest(t, fixture, body, "wallet-refund-"+amount)
		if response.Code != http.StatusCreated {
			t.Fatalf("refund %d status=%d body=%s", i, response.Code, response.Body.String())
		}
		var result d1WalletHTTPResponse
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		operation, found, err := fixture.server.(*controlPlaneHTTPHandler).app.walletAdjustment(context.Background(), result.OperationID, "")
		if err != nil || !found || operation.Status != "succeeded" || operation.AccountID != "acct-alpha" || operation.Sub2APIUserID != charge.UserID || operation.RelatedOperationID != purchase.ID || operation.BalanceHistoryRef == "" || operation.ReceiptID == "" {
			t.Fatalf("refund did not settle against its original transaction: operation=%#v found=%t err=%v", operation, found, err)
		}
	}
	input := walletAdjustmentRequest{Kind: "business_refund", AmountUSD: "0.000001", Reason: "service recovery", RelatedOperationID: purchase.ID, ConfirmationAccountID: "acct-alpha"}
	over := sendWalletAdjustmentRequest(t, fixture, string(mustJSON(input)), "wallet-refund-over")
	if over.Code != http.StatusConflict || fixture.remote.refundCalls != 2 || len(fixture.ledger.receipts) != 2 || fixture.remote.balance != 105_000_000 {
		t.Fatalf("over-refund status=%d calls=%d body=%s", over.Code, fixture.remote.refundCalls, over.Body.String())
	}
}

func TestWalletAdjustmentReadback(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	fixture.remote.adjustmentErr = clients.ErrSub2APIChargeUnknown
	fixture.remote.historyErr = errors.New("balance history unavailable")
	response := sendWalletAdjustmentRequest(t, fixture, `{"kind":"debit","amountUsd":"1.00","reason":"manual correction","confirmationAccountId":"acct-alpha"}`, "wallet-unknown")
	if response.Code != http.StatusAccepted {
		t.Fatalf("unknown status=%d body=%s", response.Code, response.Body.String())
	}
	body := decodeWalletAdjustmentResponse(t, response)
	if body["status"] != "manual_review" || body["phase"] != "authoritative_readback" || body["afterBalance"].(map[string]any)["available"] != false || len(fixture.ledger.receipts) != 0 {
		t.Fatalf("unknown result=%#v receipts=%d", body, len(fixture.ledger.receipts))
	}
	replay := sendWalletAdjustmentRequest(t, fixture, `{"kind":"debit","amountUsd":"1.00","reason":"manual correction","confirmationAccountId":"acct-alpha"}`, "wallet-unknown")
	if replay.Code != http.StatusAccepted || fixture.remote.chargeCalls != 1 {
		t.Fatalf("unknown replay status=%d chargeCalls=%d body=%s", replay.Code, fixture.remote.chargeCalls, replay.Body.String())
	}
}

func TestWalletAdjustmentPersistsSafeUpstreamDiagnostics(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	fixture.remote.adjustmentErr = fmt.Errorf("%w: %w", clients.ErrSub2APIChargeUnknown, &clients.Sub2APIHTTPError{
		StatusCode: http.StatusServiceUnavailable, ErrorCode: "gateway_busy", RequestID: "req-wallet-503",
	})
	fixture.remote.historyErr = fixture.remote.adjustmentErr
	response := sendWalletAdjustmentRequest(t, fixture, `{"kind":"recharge","amountUsd":"60.00","reason":"local pilot credit","confirmationAccountId":"acct-alpha"}`, "wallet-diagnostic")
	if response.Code != http.StatusAccepted {
		t.Fatalf("diagnostic status=%d body=%s", response.Code, response.Body.String())
	}
	body := decodeWalletAdjustmentResponse(t, response)
	failure := body["upstreamFailure"].(map[string]any)
	if failure["phase"] != "authoritative_readback" || failure["httpStatus"] != float64(http.StatusServiceUnavailable) || failure["errorCode"] != "gateway_busy" || failure["requestId"] != "req-wallet-503" {
		t.Fatalf("upstream failure=%#v", failure)
	}
	operations, _ := fixture.store.ListRuntimeOperations(context.Background())
	serialized := string(mustJSON(operations))
	for _, forbidden := range []string{"response-secret", "rawSub2apiResponse", "adminToken"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("persisted upstream failure leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestWalletAdjustmentFailsClosedWhenUpstreamDiagnosticCannotPersist(t *testing.T) {
	store := &walletAdjustmentDiagnosticPersistStore{memoryTableStore: newMemoryTableStore()}
	seedOperatorProjectionAccount(t, store.memoryTableStore, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	remote := &walletAdjustmentSub2API{testSub2APIClient: &testSub2APIClient{
		balance: 100_000_000,
		charges: map[string]int64{},
		identities: map[string]clients.Sub2APIIdentity{
			"alpha@example.com": {ID: 41, Email: "alpha@example.com", Status: "active"},
		},
		passwords: map[string]string{"alpha@example.com": "CorrectHorseBatteryStaple!"},
	}, balanceErr: &clients.Sub2APIHTTPError{StatusCode: http.StatusServiceUnavailable, ErrorCode: "balance_busy", RequestID: "req-balance-503"}}
	server, err := NewPersistentServer(controlplane.NewService(&walletAdjustmentLedger{}, &fakeFabricClient{}, remote), store)
	if err != nil {
		t.Fatal(err)
	}
	fixture := walletAdjustmentFixture{server: server, store: store.memoryTableStore, remote: remote, ledger: &walletAdjustmentLedger{}}
	response := sendWalletAdjustmentRequest(t, fixture, `{"kind":"recharge","amountUsd":"60.00","reason":"local pilot credit","confirmationAccountId":"acct-alpha"}`, "wallet-diagnostic-persist")
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), "state_persist_failed") || remote.refundCalls != 0 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, remote.refundCalls, response.Body.String())
	}
}

func TestWalletAdjustmentRecoveryAfterRestartReconcilesWithoutSecondWrite(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	fixture.remote.adjustmentErr = fmt.Errorf("%w: %w", clients.ErrSub2APIChargeUnknown, &clients.Sub2APIHTTPError{
		StatusCode: http.StatusServiceUnavailable, ErrorCode: "response_lost", RequestID: "req-lost-after-write",
	})
	fixture.remote.applyBeforeErr = true
	fixture.remote.historyErr = errors.New("temporary history outage")
	requestBody := `{"kind":"recharge","amountUsd":"60.00","reason":"local pilot credit","confirmationAccountId":"acct-alpha"}`
	first := sendWalletAdjustmentRequest(t, fixture, requestBody, "wallet-recovery-reconcile")
	if first.Code != http.StatusAccepted || fixture.remote.refundCalls != 1 || fixture.remote.balance != 160_000_000 {
		t.Fatalf("first status=%d calls=%d balance=%d body=%s", first.Code, fixture.remote.refundCalls, fixture.remote.balance, first.Body.String())
	}
	operationID := stringValue(decodeWalletAdjustmentResponse(t, first)["operationId"])

	fixture.remote.adjustmentErr = nil
	fixture.remote.historyErr = nil
	restarted, err := NewPersistentServer(controlplane.NewService(fixture.ledger, &fakeFabricClient{}, fixture.remote), fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.server = restarted
	recovered := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-recovery-command")
	if recovered.Code != http.StatusOK || decodeWalletAdjustmentResponse(t, recovered)["status"] != "succeeded" || fixture.remote.refundCalls != 1 || len(fixture.ledger.receipts) != 1 {
		t.Fatalf("recovered status=%d writes=%d receipts=%d body=%s", recovered.Code, fixture.remote.refundCalls, len(fixture.ledger.receipts), recovered.Body.String())
	}
	if len(fixture.remote.history) != 1 || fixture.remote.history[0].Code != walletAdjustmentRedeemCode(operationID) {
		t.Fatalf("history=%#v operation=%s", fixture.remote.history, operationID)
	}
}

func TestWalletAdjustmentUnknownRecoveryNeverDispatchesAgain(t *testing.T) {
	for _, kind := range []string{"debit", "recharge"} {
		for _, applied := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/applied=%t", kind, applied), func(t *testing.T) {
				fixture := newWalletAdjustmentFixture(t)
				fixture.remote.adjustmentErr = clients.ErrSub2APIChargeUnknown
				fixture.remote.applyBeforeErr = applied
				fixture.remote.historyErr = errors.New("audit unavailable")
				body, err := json.Marshal(walletAdjustmentRequest{Kind: kind, AmountUSD: "60.00", Reason: "account correction", ConfirmationAccountID: "acct-alpha"})
				if err != nil {
					t.Fatal(err)
				}
				first := sendWalletAdjustmentRequest(t, fixture, string(body), "wallet-native-unknown")
				operationID := stringValue(decodeWalletAdjustmentResponse(t, first)["operationId"])
				if first.Code != http.StatusAccepted {
					t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
				}
				balance := fixture.remote.balance
				fixture.remote.adjustmentErr, fixture.remote.historyErr = nil, nil
				fixture.remote.history = nil // The native wallet can commit without an audit row.
				for range 3 {
					restarted, err := NewPersistentServer(controlplane.NewService(fixture.ledger, &fakeFabricClient{}, fixture.remote), fixture.store)
					if err != nil {
						t.Fatal(err)
					}
					fixture.server = restarted
					recovery := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-native-recovery")
					if recovery.Code != http.StatusOK || decodeWalletAdjustmentResponse(t, recovery)["status"] != "manual_review" ||
						fixture.remote.chargeCalls+fixture.remote.refundCalls != 1 || fixture.remote.balance != balance || len(fixture.ledger.receipts) != 0 {
						t.Fatalf("unknown money was replayed: status=%d calls=%d balance=%d body=%s", recovery.Code, fixture.remote.chargeCalls+fixture.remote.refundCalls, fixture.remote.balance, recovery.Body.String())
					}
				}
			})
		}
	}
}

func TestWalletAdjustmentRecoveryConflictingTransactionsStopMoneyWrites(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	operationID := "wallet-adjustment-recovery-history-conflict"
	seedLegacyWalletAdjustment(t, fixture, operationID)
	fixture.remote.balance = 160_000_000
	fixture.remote.appendHistory(legacyWalletAdjustmentRedeemCode(operationID), 60_000_000, 41)
	fixture.remote.appendHistory(walletAdjustmentRedeemCode(operationID), 60_000_000, 41)
	first := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-recovery-stop-command")
	if first.Code != http.StatusConflict || fixture.remote.refundCalls != 0 {
		t.Fatalf("first status=%d calls=%d body=%s", first.Code, fixture.remote.refundCalls, first.Body.String())
	}
	fixture.remote.history = nil
	fixture.remote.balance = 100_000_000
	replay := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-recovery-stop-command")
	if replay.Code != http.StatusConflict || fixture.remote.refundCalls != 0 || fixture.remote.balance != 100_000_000 {
		t.Fatalf("replay status=%d calls=%d balance=%d body=%s", replay.Code, fixture.remote.refundCalls, fixture.remote.balance, replay.Body.String())
	}
}

func TestWalletAdjustmentLegacyHistoryConvergesReadOnly(t *testing.T) {
	for _, tc := range []struct {
		name      string
		codes     func(string) []string
		wantHTTP  int
		wantFunds int64
	}{
		{name: "legacy", codes: func(id string) []string { return []string{"opl:wallet-adjustment:" + stableID(id)[:24] + ":v1"} }, wantHTTP: http.StatusOK, wantFunds: 160_000_000},
		{name: "v2", codes: func(id string) []string { return []string{walletAdjustmentRedeemCode(id)} }, wantHTTP: http.StatusOK, wantFunds: 160_000_000},
		{name: "both conflict", codes: func(id string) []string {
			return []string{"opl:wallet-adjustment:" + stableID(id)[:24] + ":v1", walletAdjustmentRedeemCode(id)}
		}, wantHTTP: http.StatusConflict, wantFunds: 160_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newWalletAdjustmentFixture(t)
			operationID := "wallet-adjustment-history-" + strings.ReplaceAll(tc.name, " ", "-")
			seedLegacyWalletAdjustment(t, fixture, operationID)
			fixture.remote.balance = tc.wantFunds
			for _, code := range tc.codes(operationID) {
				fixture.remote.appendHistory(code, 60_000_000, 41)
			}
			response := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-history-command")
			if response.Code != tc.wantHTTP || fixture.remote.refundCalls != 0 || fixture.remote.balance != tc.wantFunds {
				t.Fatalf("status=%d calls=%d balance=%d body=%s", response.Code, fixture.remote.refundCalls, fixture.remote.balance, response.Body.String())
			}
		})
	}
}

func TestWalletAdjustmentRecoveryAuditReplayDoesNotRepeatAdjustment(t *testing.T) {
	store := &walletAdjustmentRecoveryAuditOnceStore{memoryTableStore: newMemoryTableStore(), failRecoveryAudit: true}
	seedOperatorProjectionAccount(t, store.memoryTableStore, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	remote := &walletAdjustmentSub2API{testSub2APIClient: &testSub2APIClient{
		balance: 100_000_000,
		charges: map[string]int64{},
		identities: map[string]clients.Sub2APIIdentity{
			"alpha@example.com": {ID: 41, Email: "alpha@example.com", Status: "active"},
		},
		passwords: map[string]string{"alpha@example.com": "CorrectHorseBatteryStaple!"},
	}}
	ledger := &walletAdjustmentLedger{}
	server, err := NewPersistentServer(controlplane.NewService(ledger, &fakeFabricClient{}, remote), store)
	if err != nil {
		t.Fatal(err)
	}
	fixture := walletAdjustmentFixture{server: server, store: store.memoryTableStore, remote: remote, ledger: ledger}
	operationID := "wallet-adjustment-recovery-audit"
	seedLegacyWalletAdjustment(t, fixture, operationID)
	remote.appendHistory(legacyWalletAdjustmentRedeemCode(operationID), 60_000_000, 41)
	remote.balance = 160_000_000
	recovery := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-recovery-audit-command")
	if recovery.Code != http.StatusInternalServerError || remote.refundCalls != 0 || len(ledger.receipts) != 1 {
		t.Fatalf("recovery status=%d calls=%d receipts=%d body=%s", recovery.Code, remote.refundCalls, len(ledger.receipts), recovery.Body.String())
	}
	replay := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-recovery-audit-command")
	events, _ := store.ListAuditEvents(context.Background(), "acct-alpha")
	if replay.Code != http.StatusOK || remote.refundCalls != 0 || len(ledger.receipts) != 1 || len(events) != 2 || events[1]["action"] != "gateway.wallet_adjustment.recover" {
		t.Fatalf("replay status=%d calls=%d receipts=%d events=%#v body=%s", replay.Code, remote.refundCalls, len(ledger.receipts), events, replay.Body.String())
	}
}

func TestWalletAdjustmentRecoveryBindsIntentBeforeReadback(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	fixture.remote.adjustmentErr = clients.ErrSub2APIChargeUnknown
	fixture.remote.historyErr = errors.New("balance history unavailable")
	first := sendWalletAdjustmentRequest(t, fixture, `{"kind":"recharge","amountUsd":"60.00","reason":"local pilot credit","confirmationAccountId":"acct-alpha"}`, "wallet-recovery-bind")
	operationID := stringValue(decodeWalletAdjustmentResponse(t, first)["operationId"])
	if first.Code != http.StatusAccepted || fixture.remote.refundCalls != 1 {
		t.Fatalf("first status=%d calls=%d body=%s", first.Code, fixture.remote.refundCalls, first.Body.String())
	}

	readbackUnavailable := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-recovery-bind-command")
	operation, found, err := fixture.server.(*controlPlaneHTTPHandler).app.walletAdjustment(context.Background(), operationID, "")
	if err != nil || !found {
		t.Fatalf("load recovery operation: found=%t err=%v", found, err)
	}
	if readbackUnavailable.Code != http.StatusOK || operation.RecoveryRequestHash == "" || operation.RecoveryEvidenceRef != "case-20260722-local" || fixture.remote.refundCalls != 1 {
		t.Fatalf("readback status=%d operation=%#v calls=%d body=%s", readbackUnavailable.Code, operation, fixture.remote.refundCalls, readbackUnavailable.Body.String())
	}

	fixture.remote.adjustmentErr = nil
	fixture.remote.historyErr = nil
	conflict := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-recovery-different-command")
	if conflict.Code != http.StatusConflict || fixture.remote.refundCalls != 1 || fixture.remote.balance != 100_000_000 {
		t.Fatalf("conflict status=%d calls=%d balance=%d body=%s", conflict.Code, fixture.remote.refundCalls, fixture.remote.balance, conflict.Body.String())
	}
}

func TestWalletAdjustmentConcurrentRecoveryDoesNotTurnMissingHistoryIntoFunds(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	operationID := "wallet-adjustment-recovery-concurrent"
	seedLegacyWalletAdjustment(t, fixture, operationID)

	session := reservedOperatorSessionForTest(t, fixture.server)
	cookies := session.Result().Cookies()
	csrfToken := session.Header().Get("x-opl-csrf-token")
	start := make(chan struct{})
	responses := make(chan *httptest.ResponseRecorder, 8)
	var workers sync.WaitGroup
	for range 8 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			req := httptest.NewRequest(http.MethodPost, "/api/operator/wallet-adjustments/"+operationID+"/recover", strings.NewReader(`{"accountId":"acct-alpha","evidenceRef":"case-20260722-local"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Idempotency-Key", "wallet-recovery-concurrent-command")
			req.Header.Set("x-opl-csrf", csrfToken)
			for _, cookie := range cookies {
				cookieCopy := *cookie
				req.AddCookie(&cookieCopy)
			}
			response := httptest.NewRecorder()
			fixture.server.ServeHTTP(response, req)
			responses <- response
		}()
	}
	close(start)
	workers.Wait()
	close(responses)
	for response := range responses {
		if response.Code != http.StatusOK {
			t.Fatalf("concurrent recovery status=%d body=%s", response.Code, response.Body.String())
		}
	}
	if fixture.remote.refundCalls != 0 || fixture.remote.balance != 100_000_000 || len(fixture.ledger.receipts) != 0 {
		t.Fatalf("writes=%d balance=%d receipts=%d", fixture.remote.refundCalls, fixture.remote.balance, len(fixture.ledger.receipts))
	}
}

func TestWalletAdjustmentRecoveryUnknownAfterWriteCannotWriteAgainAfterRestart(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	fixture.remote.adjustmentErr = clients.ErrSub2APIChargeUnknown
	operationID := "wallet-adjustment-recovery-unknown"
	seedLegacyWalletAdjustment(t, fixture, operationID)
	recovery := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-recovery-unknown-command")
	if recovery.Code != http.StatusOK || decodeWalletAdjustmentResponse(t, recovery)["status"] != "manual_review" || fixture.remote.refundCalls != 0 || fixture.remote.balance != 100_000_000 {
		t.Fatalf("recovery status=%d calls=%d balance=%d body=%s", recovery.Code, fixture.remote.refundCalls, fixture.remote.balance, recovery.Body.String())
	}
	restarted, err := NewPersistentServer(controlplane.NewService(fixture.ledger, &fakeFabricClient{}, fixture.remote), fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.server = restarted
	replay := sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-recovery-unknown-command")
	if replay.Code != http.StatusOK || fixture.remote.refundCalls != 0 || fixture.remote.balance != 100_000_000 || len(fixture.ledger.receipts) != 0 {
		t.Fatalf("replay status=%d calls=%d balance=%d receipts=%d body=%s", replay.Code, fixture.remote.refundCalls, fixture.remote.balance, len(fixture.ledger.receipts), replay.Body.String())
	}
}

func TestWalletAdjustmentRecoveryAuthorizationAndValidation(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	fixture.remote.adjustmentErr = clients.ErrSub2APIChargeUnknown
	fixture.remote.historyErr = errors.New("balance history unavailable")
	first := sendWalletAdjustmentRequest(t, fixture, `{"kind":"recharge","amountUsd":"60.00","reason":"local pilot credit","confirmationAccountId":"acct-alpha"}`, "wallet-recovery-boundary")
	operationID := stringValue(decodeWalletAdjustmentResponse(t, first)["operationId"])
	path := "/api/operator/wallet-adjustments/" + operationID + "/recover"
	body := `{"accountId":"acct-alpha","evidenceRef":"case-20260722-local"}`
	if first.Code != http.StatusAccepted || fixture.remote.refundCalls != 1 {
		t.Fatalf("first status=%d calls=%d body=%s", first.Code, fixture.remote.refundCalls, first.Body.String())
	}

	anonymousRequest := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	anonymousRequest.Header.Set("Content-Type", "application/json")
	anonymousRequest.Header.Set("Idempotency-Key", "wallet-recovery-anonymous")
	anonymous := httptest.NewRecorder()
	fixture.server.ServeHTTP(anonymous, anonymousRequest)
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous status=%d body=%s", anonymous.Code, anonymous.Body.String())
	}
	owner := requestWithMutationKeyForTest(t, fixture.server, tenantOwnerSessionForTest(t, fixture.server), http.MethodPost, path, body, "wallet-recovery-owner")
	if owner.Code != http.StatusForbidden {
		t.Fatalf("owner status=%d body=%s", owner.Code, owner.Body.String())
	}

	operator := reservedOperatorSessionForTest(t, fixture.server)
	missingCSRFRequest := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	missingCSRFRequest.Header.Set("Content-Type", "application/json")
	missingCSRFRequest.Header.Set("Idempotency-Key", "wallet-recovery-missing-csrf")
	addSessionCookies(missingCSRFRequest, operator)
	missingCSRF := httptest.NewRecorder()
	fixture.server.ServeHTTP(missingCSRF, missingCSRFRequest)
	if missingCSRF.Code != http.StatusForbidden {
		t.Fatalf("missing CSRF status=%d body=%s", missingCSRF.Code, missingCSRF.Body.String())
	}
	missingKeyRequest := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	missingKeyRequest.Header.Set("Content-Type", "application/json")
	addAuth(missingKeyRequest, operator)
	missingKey := httptest.NewRecorder()
	fixture.server.ServeHTTP(missingKey, missingKeyRequest)
	if missingKey.Code != http.StatusBadRequest {
		t.Fatalf("missing key status=%d body=%s", missingKey.Code, missingKey.Body.String())
	}

	for _, tc := range []struct {
		name string
		body string
	}{
		{name: "unexpected field", body: `{"accountId":"acct-alpha","evidenceRef":"case-20260722-local","retry":true}`},
		{name: "invalid evidence", body: `{"accountId":"acct-alpha","evidenceRef":""}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response := requestWithMutationKeyForTest(t, fixture.server, operator, http.MethodPost, path, tc.body, "wallet-recovery-invalid-"+strings.ReplaceAll(tc.name, " ", "-"))
			if response.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
	if fixture.remote.refundCalls != 1 || fixture.remote.balance != 100_000_000 {
		t.Fatalf("boundary requests reached Sub2API: calls=%d balance=%d", fixture.remote.refundCalls, fixture.remote.balance)
	}
}

func TestWalletAdjustmentManualReviewAuditRecoversWithoutReplayingAdjustment(t *testing.T) {
	store := &walletAdjustmentAuditOnceStore{memoryTableStore: newMemoryTableStore(), failAudit: true}
	seedOperatorProjectionAccount(t, store.memoryTableStore, "acct-alpha", "usr-alpha", "alpha@example.com", 41)
	remote := &walletAdjustmentSub2API{testSub2APIClient: &testSub2APIClient{
		balance: 100_000_000,
		charges: map[string]int64{},
		identities: map[string]clients.Sub2APIIdentity{
			"alpha@example.com": {ID: 41, Email: "alpha@example.com", Status: "active"},
		},
		passwords: map[string]string{"alpha@example.com": "CorrectHorseBatteryStaple!"},
	}, adjustmentErr: clients.ErrSub2APIChargeUnknown, historyErr: errors.New("balance history unavailable")}
	ledger := &walletAdjustmentLedger{}
	server, err := NewPersistentServer(controlplane.NewService(ledger, &fakeFabricClient{}, remote), store)
	if err != nil {
		t.Fatal(err)
	}
	fixture := walletAdjustmentFixture{server: server, store: store.memoryTableStore, remote: remote, ledger: ledger}
	body := `{"kind":"debit","amountUsd":"1.00","reason":"manual correction","confirmationAccountId":"acct-alpha"}`

	first := sendWalletAdjustmentRequest(t, fixture, body, "wallet-manual-review-audit")
	if first.Code != http.StatusInternalServerError {
		t.Fatalf("first status=%d body=%s", first.Code, first.Body.String())
	}
	replay := sendWalletAdjustmentRequest(t, fixture, body, "wallet-manual-review-audit")
	events, _ := store.ListAuditEvents(context.Background(), "acct-alpha")
	if replay.Code != http.StatusAccepted || remote.chargeCalls != 1 || len(events) != 1 || events[0]["result"] != "manual_review" {
		t.Fatalf("replay status=%d chargeCalls=%d events=%#v body=%s", replay.Code, remote.chargeCalls, events, replay.Body.String())
	}
}

func TestWalletAdjustmentAudit(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	response := sendWalletAdjustmentRequest(t, fixture, `{"kind":"recharge","amountUsd":"2.50","reason":"pilot credit","confirmationAccountId":"acct-alpha"}`, "wallet-recharge-audit")
	if response.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := decodeWalletAdjustmentResponse(t, response)
	before := body["beforeBalance"].(map[string]any)
	after := body["afterBalance"].(map[string]any)
	if nested(before, "data", "usdMicros") != "100000000" || nested(after, "data", "usdMicros") != "102500000" || body["balanceHistoryRef"] == "" {
		t.Fatalf("authoritative balances=%#v", body)
	}

	events, _ := fixture.store.ListAuditEvents(context.Background(), "acct-alpha")
	operations, _ := fixture.store.ListRuntimeOperations(context.Background())
	if len(events) != 1 || events[0]["action"] != "gateway.wallet_adjustment" || events[0]["result"] != "succeeded" || len(operations) != 1 || operations[0]["status"] != "succeeded" {
		t.Fatalf("audit=%#v operations=%#v", events, operations)
	}
	if len(fixture.ledger.receipts) != 1 {
		t.Fatalf("receipts=%#v", fixture.ledger.receipts)
	}
	receipt := fixture.ledger.receipts[0]
	if receipt.Type != "gateway.wallet_adjustment.v1" || receipt.AccountID != "acct-alpha" || receipt.Execution["operationId"] != body["operationId"] || receipt.Execution["amountUsdMicros"] != int64(2_500_000) || receipt.InputRefs["balanceHistoryRef"] != body["balanceHistoryRef"] || receipt.Actor["userId"] == "" {
		t.Fatalf("receipt=%#v", receipt)
	}
	var persisted map[string]any
	if json.Unmarshal([]byte(stringValue(operations[0]["result"])), &persisted) != nil || persisted["canonicalRedeemCode"] != walletAdjustmentRedeemCode(stringValue(body["operationId"])) || persisted["redeemCodeVersion"] != "v2" {
		t.Fatalf("internal canonical identity=%#v", persisted)
	}
	serialized := string(mustJSON(map[string]any{"body": body, "audit": events, "receipt": receipt}))
	for _, forbidden := range []string{stringValue(persisted["canonicalRedeemCode"]), "canonicalRedeemCode", "redeemCodeVersion", "adminToken", "rawSub2apiResponse"} {
		if strings.Contains(strings.ToLower(serialized), strings.ToLower(forbidden)) {
			t.Fatalf("forbidden %q escaped RuntimeOperation: %s", forbidden, serialized)
		}
	}
}

func TestWalletAdjustmentConfirmedTransactionSurvivesConcurrentWalletActivity(t *testing.T) {
	for _, test := range []struct {
		name        string
		kind        string
		otherChange int64
		wantBalance int64
		wantDebit   int
		wantCredit  int
	}{
		{name: "debit_then_API_consumption", kind: "debit", otherChange: -5_000_000, wantBalance: 92_500_000, wantDebit: 1},
		{name: "debit_then_recharge", kind: "debit", otherChange: 5_000_000, wantBalance: 102_500_000, wantDebit: 1},
		{name: "recharge_then_API_consumption", kind: "recharge", otherChange: -5_000_000, wantBalance: 97_500_000, wantCredit: 1},
		{name: "recharge_then_another_recharge", kind: "recharge", otherChange: 5_000_000, wantBalance: 107_500_000, wantCredit: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newWalletAdjustmentFixture(t)
			otherTransactions := 0
			fixture.remote.afterAdjustment = func() {
				fixture.remote.balance += test.otherChange
				otherTransactions++
			}
			input := walletAdjustmentRequest{Kind: test.kind, AmountUSD: "2.50", Reason: "manual correction", ConfirmationAccountID: "acct-alpha"}
			body := string(mustJSON(input))
			key := "wallet-concurrent-" + test.name
			response := sendWalletAdjustmentRequest(t, fixture, body, key)
			if response.Code != http.StatusCreated {
				t.Fatalf("confirmed transaction status=%d body=%s", response.Code, response.Body.String())
			}
			var output d1WalletHTTPResponse
			if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			operation, found, err := fixture.server.(*controlPlaneHTTPHandler).app.walletAdjustment(context.Background(), output.OperationID, "")
			if err != nil || !found || operation.Status != "succeeded" || operation.Phase != "complete" || operation.BalanceHistoryRef == "" || operation.ReceiptID == "" || !operation.AfterBalanceKnown || operation.AfterBalanceMicros != test.wantBalance || otherTransactions != 1 {
				t.Fatalf("unrelated wallet activity blocked confirmed transaction: operation=%#v found=%t err=%v otherTransactions=%d", operation, found, err, otherTransactions)
			}
			if len(fixture.ledger.receipts) != 1 || fixture.ledger.receipts[0].RequestID != output.OperationID || fixture.ledger.receipts[0].AccountID != "acct-alpha" {
				t.Fatalf("confirmed transaction receipt missing: %#v", fixture.ledger.receipts)
			}
			replay := sendWalletAdjustmentRequest(t, fixture, body, key)
			if replay.Code != http.StatusOK || fixture.remote.chargeCalls != test.wantDebit || fixture.remote.refundCalls != test.wantCredit || fixture.remote.balance != test.wantBalance || len(fixture.ledger.receipts) != 1 || otherTransactions != 1 {
				t.Fatalf("replay repeated transaction: status=%d debit=%d credit=%d balance=%d receipts=%d", replay.Code, fixture.remote.chargeCalls, fixture.remote.refundCalls, fixture.remote.balance, len(fixture.ledger.receipts))
			}
		})
	}
}

func TestWalletAdjustmentConfirmedTransactionFinishesWithUnavailableAfterBalance(t *testing.T) {
	for _, kind := range []string{"debit", "recharge"} {
		t.Run(kind, func(t *testing.T) {
			fixture := newWalletAdjustmentFixture(t)
			fixture.remote.afterAdjustment = func() { fixture.remote.balanceErr = errors.New("current wallet unavailable") }
			input := walletAdjustmentRequest{Kind: kind, AmountUSD: "2.50", Reason: "manual correction", ConfirmationAccountID: "acct-alpha"}
			body := string(mustJSON(input))
			key := "wallet-no-after-balance-" + kind
			response := sendWalletAdjustmentRequest(t, fixture, body, key)
			if response.Code != http.StatusCreated {
				t.Fatalf("confirmed transaction status=%d body=%s", response.Code, response.Body.String())
			}
			var output d1WalletHTTPResponse
			if err := json.Unmarshal(response.Body.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			operation, found, err := fixture.server.(*controlPlaneHTTPHandler).app.walletAdjustment(context.Background(), output.OperationID, "")
			if err != nil || !found || operation.Status != "succeeded" || operation.Phase != "complete" || operation.BalanceHistoryRef == "" || operation.ReceiptID == "" || operation.AfterBalanceKnown || operation.AfterBalanceReadAt != "" || len(fixture.ledger.receipts) != 1 {
				t.Fatalf("wallet availability overruled confirmed transaction: operation=%#v found=%t err=%v receipts=%#v", operation, found, err, fixture.ledger.receipts)
			}
			// The response is a generic source envelope. Check its real JSON
			// boundary without defining a second money or source schema.
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(response.Body.Bytes(), &fields); err != nil {
				t.Fatal(err)
			}
			var after map[string]json.RawMessage
			if err := json.Unmarshal(fields["afterBalance"], &after); err != nil {
				t.Fatal(err)
			}
			var available bool
			if err := json.Unmarshal(after["available"], &available); err != nil || available {
				t.Fatalf("afterBalance falsely available: %s err=%v", fields["afterBalance"], err)
			}
			if data, present := after["data"]; present && string(data) != "null" {
				t.Fatalf("unavailable wallet fabricated money: %s", data)
			}
			restarted, err := NewPersistentServer(controlplane.NewService(fixture.ledger, &fakeFabricClient{}, fixture.remote), fixture.store)
			if err != nil {
				t.Fatal(err)
			}
			fixture.server = restarted
			replay := sendWalletAdjustmentRequest(t, fixture, body, key)
			if replay.Code != http.StatusOK || fixture.remote.chargeCalls+fixture.remote.refundCalls != 1 || len(fixture.ledger.receipts) != 1 {
				t.Fatalf("restart repeated a confirmed transaction: status=%d writes=%d receipts=%d", replay.Code, fixture.remote.chargeCalls+fixture.remote.refundCalls, len(fixture.ledger.receipts))
			}
		})
	}
}

func TestWalletAdjustmentRetainedRecoveryReservationCannotDispatchAfterUpgrade(t *testing.T) {
	fixture := newWalletAdjustmentFixture(t)
	fixture.remote.adjustmentErr = clients.ErrSub2APIChargeUnknown
	body := `{"kind":"recharge","amountUsd":"60.00","reason":"account correction","confirmationAccountId":"acct-alpha"}`
	first := sendWalletAdjustmentRequest(t, fixture, body, "wallet-retained-reservation")
	operationID := stringValue(decodeWalletAdjustmentResponse(t, first)["operationId"])
	sendWalletAdjustmentRecoveryRequest(t, fixture, operationID, "wallet-retained-recovery")
	app := fixture.server.(*controlPlaneHTTPHandler).app
	operation, found, err := app.walletAdjustment(context.Background(), operationID, "")
	if err != nil || !found {
		t.Fatalf("load: found=%t err=%v", found, err)
	}
	operation.Status, operation.Phase = "pending", "adjustment"
	operation.RecoveryAttempted, operation.AdjustmentAttempted = true, false
	if err := app.persistWalletAdjustment(context.Background(), operationID, &operation); err != nil {
		t.Fatal(err)
	}
	fixture.remote.adjustmentErr = nil
	restarted, err := NewPersistentServer(controlplane.NewService(fixture.ledger, &fakeFabricClient{}, fixture.remote), fixture.store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.server = restarted
	replay := sendWalletAdjustmentRequest(t, fixture, body, "wallet-retained-reservation")
	if replay.Code != http.StatusAccepted || decodeWalletAdjustmentResponse(t, replay)["status"] != "manual_review" || fixture.remote.refundCalls != 1 || fixture.remote.balance != 100_000_000 || len(fixture.ledger.receipts) != 0 {
		t.Fatalf("retained unknown adjustment was replayed: status=%d calls=%d balance=%d body=%s", replay.Code, fixture.remote.refundCalls, fixture.remote.balance, replay.Body.String())
	}
}
