package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// This is a local business-chain test: real Control Plane HTTP routes, separate
// PostgreSQL connections, the real Sub2API HTTP client, and the real Ledger HTTP
// process. Sub2API financial behavior and Fabric procurement are explicit local
// fixtures; passing these tests does not qualify a deployed Sub2API or provider.
func TestD1FinanceBusinessChain(t *testing.T) {
	if controlPlaneTestPostgresBaseURL() == "" {
		t.Skip("PostgreSQL test gate is not configured")
	}
	t.Setenv("OPL_TENCENT_ZONE", "na-siliconvalley-1")
	ledger, receipts := startGatewayAccountingLedger(t)

	t.Run("another account cannot receive the original purchase refund", func(t *testing.T) {
		chain := newD1FinanceChain(t, ledger)
		account, user := provisionedAccountRowsFor("acct-d1-other", "usr-d1-other", "other-d1@example.test", 42)
		if err := chain.process.store.CreateProvisionedAccount(context.Background(), account, user); err != nil {
			t.Fatal(err)
		}
		before := chain.remote.snapshot()
		input := chain.refund("3.00")
		input.ConfirmationAccountID = "acct-d1-other"
		status, output := chain.request(t, "/api/operator/accounts/acct-d1-other/wallet-adjustments", input, "other-account-refund")
		if status != http.StatusConflict || output.OperationID != "" {
			t.Fatalf("cross-account refund status=%d response=%+v", status, output)
		}
		after := chain.remote.snapshot()
		if before != after {
			t.Fatalf("rejected refund changed funds or submitted a mutation: before=%+v after=%+v", before, after)
		}
		chain.assertRefundReceipts(t, receipts, 0)
	})

	t.Run("a successful local purchase cannot override mismatched upstream debit evidence", func(t *testing.T) {
		chain := newD1FinanceChain(t, ledger)
		chain.remote.mu.Lock()
		for i := range chain.remote.transactions {
			if chain.remote.transactions[i].Code == chain.purchase.stringFact("sub2apiRedeemCode") {
				chain.remote.transactions[i].Value = json.Number("-1.00")
			}
		}
		chain.remote.mu.Unlock()
		before := chain.remote.snapshot()
		status, output := chain.request(t, chain.refundPath(), chain.refund("3.00"), "original-debit-mismatch")
		if status < http.StatusBadRequest && output.Status != "manual_review" {
			t.Fatalf("mismatched original payment was not rejected or held for review: status=%d response=%+v", status, output)
		}
		if after := chain.remote.snapshot(); before != after {
			t.Fatalf("mismatched original payment caused a refund: before=%+v after=%+v", before, after)
		}
		chain.assertRefundReceipts(t, receipts, 0)
	})

	t.Run("partial refunds cover exactly the purchase despite concurrent usage", func(t *testing.T) {
		chain := newD1FinanceChain(t, ledger)
		chain.remote.mu.Lock()
		chain.remote.consumeAfterRefund = 5_000_000
		chain.remote.mu.Unlock()
		for i, amount := range []string{"30.00", "22.58"} {
			status, output := chain.request(t, chain.refundPath(), chain.refund(amount), fmt.Sprintf("partial-%d", i))
			if status != http.StatusCreated || output.Status != "succeeded" {
				t.Fatalf("partial refund amount=%s status=%d response=%+v", amount, status, output)
			}
			operation := chain.operation(t, output.OperationID)
			if operation.BalanceHistoryRef == "" || operation.ReceiptID == "" || operation.RelatedOperationID != chain.purchase.ID {
				t.Fatalf("completed refund lost original purchase or evidence: %+v", operation)
			}
		}
		status, _ := chain.request(t, chain.refundPath(), chain.refund("0.000001"), "refund-one-micro-over")
		if status != http.StatusConflict {
			t.Fatalf("one micro over paid amount status=%d", status)
		}
		state := chain.remote.snapshot()
		if state.refundWrites != 2 || state.refunded != gatewayAccountingChargeMicros || state.ownerBalance != gatewayAccountingInitialMicros-5_000_000 {
			t.Fatalf("partial refunds or concurrent usage funds incorrect: %+v", state)
		}
		chain.assertRefundReceipts(t, receipts, 2)
	})

	t.Run("two control planes reserve one original refund budget", func(t *testing.T) {
		chain := newD1FinanceChain(t, ledger)
		second := chain.openControlPlane(t, ledger)
		secondSession := reservedOperatorSessionForTest(t, second.handler)
		arrived, release := make(chan struct{}), make(chan struct{})
		chain.remote.mu.Lock()
		chain.remote.refundArrived, chain.remote.refundRelease = arrived, release
		chain.remote.mu.Unlock()
		t.Cleanup(func() {
			select {
			case <-release:
			default:
				close(release)
			}
		})
		type requestResult struct {
			status int
			body   d1WalletHTTPResponse
			err    error
		}
		firstResult := make(chan requestResult, 1)
		go func() {
			status, body, err := d1WalletHTTPRequest(chain.process, chain.session, chain.refundPath(), chain.refund("30.00"), "parallel-a")
			firstResult <- requestResult{status, body, err}
		}()
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			t.Fatal("first refund did not reach the Sub2API financial boundary")
		}
		// The first refund is still in flight, so only the persisted reservation
		// can prevent a second process from spending the same refundable budget.
		status, body, err := d1WalletHTTPRequest(second, secondSession, chain.refundPath(), chain.refund("30.00"), "parallel-b")
		if err != nil || status != http.StatusConflict {
			t.Fatalf("second CP over-refund status=%d response=%+v error=%v", status, body, err)
		}
		close(release)
		select {
		case first := <-firstResult:
			if first.err != nil || first.status != http.StatusCreated || first.body.Status != "succeeded" {
				t.Fatalf("reserved refund result=%+v", first)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("first refund failed to resume after the remote response was released")
		}
		state := chain.remote.snapshot()
		if state.refundRequests != 1 || state.refundWrites != 1 || state.refunded != 30_000_000 {
			t.Fatalf("two processes overspent or submitted a rejected refund: %+v", state)
		}
		chain.assertRefundReceipts(t, receipts, 1)
	})

	t.Run("lost refund response survives restart without another payment", func(t *testing.T) {
		chain := newD1FinanceChain(t, ledger)
		chain.remote.mu.Lock()
		chain.remote.dropRefundResponse = true
		chain.remote.mu.Unlock()
		status, first := chain.request(t, chain.refundPath(), chain.refund("10.00"), "lost-response")
		if status != http.StatusAccepted || first.Status != "manual_review" {
			t.Fatalf("unconfirmed response status=%d response=%+v", status, first)
		}
		chain.assertRefundReceipts(t, receipts, 0)
		before := chain.remote.snapshot()
		if before.refundWrites != 1 || before.refunded != 10_000_000 {
			t.Fatalf("fault was not injected after payment: %+v", before)
		}
		chain.restart(t, ledger)
		chain.remote.mu.Lock()
		chain.remote.historyUnavailable = false
		chain.remote.mu.Unlock()
		input := walletAdjustmentRecoveryRequest{AccountID: chain.accountID, EvidenceRef: "case-20260908-d1response"}
		status, recovered := chain.request(t, "/api/operator/wallet-adjustments/"+first.OperationID+"/recover", input, "recover-lost-response")
		if status != http.StatusOK || recovered.Status != "succeeded" || recovered.OperationID != first.OperationID {
			t.Fatalf("restart recovery status=%d response=%+v", status, recovered)
		}
		after := chain.remote.snapshot()
		if before != after {
			t.Fatalf("authoritative recovery submitted another payment: before=%+v after=%+v", before, after)
		}
		chain.assertRefundReceipts(t, receipts, 1)
	})

	t.Run("lost ledger receipt response retries evidence without refunding again", func(t *testing.T) {
		chain := newD1FinanceChain(t, ledger)
		fault := &d1LedgerResponseLoss{LedgerClient: ledger}
		chain.restart(t, fault)
		status, first := chain.request(t, chain.refundPath(), chain.refund("10.00"), "receipt-response-loss")
		if status != http.StatusBadGateway {
			t.Fatalf("receipt response loss status=%d response=%+v", status, first)
		}
		operationID := "wallet-adjustment-" + stableID(chain.accountID, "receipt-response-loss")[:18]
		operation := chain.operation(t, operationID)
		if operation.Phase != "ledger" || operation.Status != "pending" {
			t.Fatalf("funds-confirmed operation did not retain evidence obligation: %+v", operation)
		}
		chain.assertRefundReceipts(t, receipts, 1)
		before := chain.remote.snapshot()
		chain.restart(t, ledger)
		status, replay := chain.request(t, chain.refundPath(), chain.refund("10.00"), "receipt-response-loss")
		if status != http.StatusOK || replay.Status != "succeeded" || replay.OperationID != operationID {
			t.Fatalf("receipt retry after restart status=%d response=%+v", status, replay)
		}
		if after := chain.remote.snapshot(); before != after {
			t.Fatalf("receipt retry paid twice: before=%+v after=%+v", before, after)
		}
		chain.assertRefundReceipts(t, receipts, 1)
	})
}

type d1FinanceChain struct {
	accountID   string
	databaseURL string
	process     *gatewayAccountingControlPlane
	session     *httptest.ResponseRecorder
	remote      *d1FinancialHTTPFixture
	purchase    workspaceLaunchReconcileOperation
}

func newD1FinanceChain(t *testing.T, ledger clients.LedgerClient) *d1FinanceChain {
	t.Helper()
	accountID := "acct-d1-" + stableID(t.Name())[:12]
	chain := &d1FinanceChain{accountID: accountID, databaseURL: gatewayAccountingDatabase(t)}
	chain.remote = newD1FinancialHTTPFixture(t)
	t.Setenv("NODE_ENV", "")
	t.Setenv("OPL_WORKSPACE_LAUNCH_WORKER_ENABLED", "0")
	t.Setenv(controlledBasicPilotEnabledEnv, "1")
	t.Setenv(controlledBasicPilotAccountsEnv, accountID)
	t.Setenv(controlledBasicPilotMaxInFlightEnv, "1")
	chain.process = chain.openControlPlane(t, ledger)
	account, user := provisionedAccountRowsFor(accountID, "usr-d1-"+stableID(t.Name())[:12], chain.remote.sub2api.ownerEmail, gatewayAccountingSub2APIUserID)
	if err := chain.process.store.CreateProvisionedAccount(context.Background(), account, user); err != nil {
		t.Fatal(err)
	}
	owner := chain.process.login(t, chain.remote.sub2api.ownerEmail, gatewayAccountingOwnerPassword)
	// The launch owner still accepts generic JSON. Feed its real decoder and
	// obtain all refundable facts by running the actual purchase, not seeding it.
	launch := owner.mustRequest(t, http.MethodPost, "/api/workspace-launches", json.RawMessage(`{"name":"D1 paid Workspace","packageId":"basic","autoRenew":false}`), "d1-original-purchase", http.StatusAccepted)
	chain.purchase = runGatewayAccountingLaunch(t, chain.process, stringValue(launch["operationId"]), false)
	if chain.purchase.Status != "succeeded" {
		t.Fatalf("original purchase failed: %s", workspaceLaunchReconcileResultSummary(chain.purchase))
	}
	chain.session = reservedOperatorSessionForTest(t, chain.process.handler)
	return chain
}

func (c *d1FinanceChain) openControlPlane(t *testing.T, ledger clients.LedgerClient) *gatewayAccountingControlPlane {
	t.Helper()
	store, err := newTestPostgresEntStateStore(c.databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.(*postgresEntStateStore).client.Close() })
	fabric := newGatewayAccountingFabric()
	if c.process != nil {
		fabric = c.process.fabric
	}
	service := controlplane.NewService(ledger, fabric, c.remote.sub2api.client)
	handler, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	return &gatewayAccountingControlPlane{server: server, handler: handler.(*controlPlaneHTTPHandler), store: store, fabric: fabric}
}

func (c *d1FinanceChain) restart(t *testing.T, ledger clients.LedgerClient) {
	t.Helper()
	c.process.server.Close()
	if err := c.process.store.(*postgresEntStateStore).client.Close(); err != nil {
		t.Fatal(err)
	}
	c.process = c.openControlPlane(t, ledger)
	c.session = reservedOperatorSessionForTest(t, c.process.handler)
}

func (c *d1FinanceChain) refundPath() string {
	return "/api/operator/accounts/" + c.accountID + "/wallet-adjustments"
}

func (c *d1FinanceChain) refund(amount string) walletAdjustmentRequest {
	return walletAdjustmentRequest{Kind: "business_refund", AmountUSD: amount, Reason: "original purchase service correction", RelatedOperationID: c.purchase.ID, ConfirmationAccountID: c.accountID}
}

// Only the fields needed at this JSON presentation boundary are decoded here;
// financial assertions use the persisted owner's walletAdjustmentOperation.
type d1WalletHTTPResponse struct {
	OperationID string `json:"operationId"`
	Status      string `json:"status"`
	Error       string `json:"error"`
}

func (c *d1FinanceChain) request(t *testing.T, path string, input any, key string) (int, d1WalletHTTPResponse) {
	t.Helper()
	status, output, err := d1WalletHTTPRequest(c.process, c.session, path, input, key)
	if err != nil {
		t.Fatal(err)
	}
	return status, output
}

func d1WalletHTTPRequest(process *gatewayAccountingControlPlane, session *httptest.ResponseRecorder, path string, input any, key string) (int, d1WalletHTTPResponse, error) {
	var output d1WalletHTTPResponse
	body, err := json.Marshal(input)
	if err != nil {
		return 0, output, err
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, process.server.URL+path, bytes.NewReader(body))
	if err != nil {
		return 0, output, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	addAuth(request, session)
	client := process.server.Client()
	client.Timeout = 10 * time.Second
	response, err := client.Do(request)
	if err != nil {
		return 0, output, err
	}
	defer response.Body.Close()
	err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&output)
	return response.StatusCode, output, err
}

func (c *d1FinanceChain) operation(t *testing.T, id string) walletAdjustmentOperation {
	t.Helper()
	operation, found, err := c.process.handler.app.walletAdjustment(context.Background(), id, "")
	if err != nil || !found {
		t.Fatalf("persisted wallet operation found=%t error=%v", found, err)
	}
	return operation
}

func (c *d1FinanceChain) assertRefundReceipts(t *testing.T, ledger clients.LedgerReceiptListClient, count int) {
	t.Helper()
	page, err := ledger.ListReceipts(context.Background(), clients.ReceiptQuery{AccountID: c.accountID, TypePrefix: "gateway.wallet_adjustment.", Limit: 50})
	if err != nil || len(page.Receipts) != count {
		t.Fatalf("refund receipt count=%d want=%d error=%v", len(page.Receipts), count, err)
	}
	for _, receipt := range page.Receipts {
		operation := c.operation(t, receipt.RequestID)
		if receipt.AccountID != c.accountID || receipt.Type != "gateway.wallet_adjustment.v1" || receipt.Status != "completed" || operation.RelatedOperationID != c.purchase.ID {
			t.Fatalf("receipt lost original account or transaction: receipt=%+v operation=%+v", receipt, operation)
		}
		// Ledger intentionally owns opaque JSON provenance; decode its actual
		// financial boundary instead of comparing an untyped map.
		var provenance struct {
			RelatedOperationID string `json:"relatedOperationId"`
			BalanceHistoryRef  string `json:"balanceHistoryRef"`
		}
		var execution struct {
			Kind            string `json:"kind"`
			AmountUSDMicros int64  `json:"amountUsdMicros"`
		}
		encoded, _ := json.Marshal(receipt.InputRefs)
		if err := json.Unmarshal(encoded, &provenance); err != nil {
			t.Fatal(err)
		}
		encoded, _ = json.Marshal(receipt.Execution)
		if err := json.Unmarshal(encoded, &execution); err != nil {
			t.Fatal(err)
		}
		if provenance.RelatedOperationID != c.purchase.ID || provenance.BalanceHistoryRef == "" || execution.Kind != "business_refund" || execution.AmountUSDMicros != operation.AmountUSDMicros {
			t.Fatalf("receipt refund evidence=%+v execution=%+v operation=%+v", provenance, execution, operation)
		}
	}
}

type d1LedgerResponseLoss struct {
	clients.LedgerClient
	lost bool
}

func (l *d1LedgerResponseLoss) RecordReceipt(ctx context.Context, input clients.ReceiptInput, key string) (clients.Receipt, error) {
	receipt, err := l.LedgerClient.RecordReceipt(ctx, input, key)
	if err == nil && input.Type == "gateway.wallet_adjustment.v1" && !l.lost {
		l.lost = true
		return clients.Receipt{}, io.ErrUnexpectedEOF
	}
	return receipt, err
}

// Authentication and Key routes reuse the existing local qualification fixture.
// All financial writes/readback below share one atomic Go fixture, with no limit
// on distinct transactions. It is not the real Sub2API implementation.
type d1FinancialHTTPFixture struct {
	sub2api            *gatewayAccountingSub2API
	mu                 sync.Mutex
	balances           map[int64]int64
	transactions       []d1FinancialTransaction
	refundRequests     int
	refundWrites       int
	consumeAfterRefund int64
	dropRefundResponse bool
	historyUnavailable bool
	refundArrived      chan struct{}
	refundRelease      chan struct{}
}

type d1FinancialTransaction struct {
	Code         string      `json:"code"`
	Type         string      `json:"type"`
	Value        json.Number `json:"value"`
	AppliedValue json.Number `json:"balance_applied_value"`
	Status       string      `json:"status"`
	UsedBy       int64       `json:"used_by"`
	UsedAt       time.Time   `json:"used_at"`
	CreatedAt    time.Time   `json:"created_at"`
	Notes        string      `json:"-"`
}

type d1FinancialState struct {
	ownerBalance   int64
	otherBalance   int64
	refundRequests int
	refundWrites   int
	refunded       int64
}

func newD1FinancialHTTPFixture(t *testing.T) *d1FinancialHTTPFixture {
	t.Helper()
	remote := newGatewayAccountingSub2API(t, "owner-d1@example.test", false)
	fixture := &d1FinancialHTTPFixture{sub2api: remote, balances: map[int64]int64{41: gatewayAccountingInitialMicros, 42: gatewayAccountingInitialMicros}}
	upstream, err := url.Parse(remote.baseURL)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !fixture.serveFinancial(w, r) {
			proxy.ServeHTTP(w, r)
		}
	}))
	t.Cleanup(server.Close)
	remote.client, err = clients.NewSub2APIHTTPClient(clients.Sub2APIConfig{BaseURL: server.URL, AdminEmail: gatewayAccountingAdminEmail, AdminPassword: gatewayAccountingAdminPassword, Timeout: 5 * time.Second}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

func (f *d1FinancialHTTPFixture) snapshot() d1FinancialState {
	f.mu.Lock()
	defer f.mu.Unlock()
	state := d1FinancialState{ownerBalance: f.balances[41], otherBalance: f.balances[42], refundRequests: f.refundRequests, refundWrites: f.refundWrites}
	for _, entry := range f.transactions {
		amount, err := clients.ParseUSDDecimalMicros(string(entry.Value))
		if err == nil && amount > 0 {
			state.refunded += amount
		}
	}
	return state
}

func (f *d1FinancialHTTPFixture) serveFinancial(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/admin/redeem-codes/by-code" {
		userID, err := strconv.ParseInt(r.URL.Query().Get("user_id"), 10, 64)
		code := r.URL.Query().Get("code")
		if err != nil || userID <= 0 || code == "" {
			writeError(w, http.StatusBadRequest, "invalid_exact_lookup")
			return true
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.historyUnavailable {
			writeError(w, http.StatusServiceUnavailable, "d1_injected_history_unavailable")
			return true
		}
		var matched *d1FinancialTransaction
		for _, entry := range f.transactions {
			if entry.Code != code {
				continue
			}
			if entry.UsedBy != userID || entry.Type != "balance" {
				writeError(w, http.StatusConflict, "exact_lookup_identity_conflict")
				return true
			}
			matched = &entry
			break
		}
		d1Sub2APISuccess(w, struct {
			Lookup     string                  `json:"lookup"`
			RedeemCode *d1FinancialTransaction `json:"redeem_code"`
		}{"exact_code_v1", matched})
		return true
	}
	if r.Method == http.MethodPost && r.URL.Path == "/api/v1/admin/redeem-codes/create-and-redeem" {
		f.redeem(w, r)
		return true
	}
	for _, userID := range []int64{41, 42} {
		userPath := "/api/v1/admin/users/" + strconv.FormatInt(userID, 10)
		if r.Method == http.MethodGet && r.URL.Path == userPath {
			f.mu.Lock()
			balance := f.balances[userID]
			f.mu.Unlock()
			email := f.sub2api.ownerEmail
			if userID == 42 {
				email = "other-d1@example.test"
			}
			d1Sub2APISuccess(w, struct {
				ID      int64       `json:"id"`
				Email   string      `json:"email"`
				Status  string      `json:"status"`
				Balance json.Number `json:"balance"`
			}{userID, email, "active", json.Number(formatWalletUSD(balance))})
			return true
		}
		if r.Method == http.MethodGet && r.URL.Path == userPath+"/balance-history" {
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.historyUnavailable {
				writeError(w, http.StatusServiceUnavailable, "d1_injected_history_unavailable")
				return true
			}
			items := make([]d1FinancialTransaction, 0)
			for _, entry := range f.transactions {
				if entry.UsedBy == userID {
					items = append(items, entry)
				}
			}
			page, _ := strconv.Atoi(r.URL.Query().Get("page"))
			pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
			if page < 1 || pageSize < 1 {
				writeError(w, http.StatusBadRequest, "invalid_pagination")
				return true
			}
			total := len(items)
			pages := max(1, (total+pageSize-1)/pageSize)
			start := min(total, (page-1)*pageSize)
			end := min(total, start+pageSize)
			d1Sub2APISuccess(w, struct {
				Items    []d1FinancialTransaction `json:"items"`
				Total    int                      `json:"total"`
				Page     int                      `json:"page"`
				PageSize int                      `json:"page_size"`
				Pages    int                      `json:"pages"`
			}{items[start:end], total, page, pageSize, pages})
			return true
		}
	}
	return false
}

func (f *d1FinancialHTTPFixture) redeem(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code   string      `json:"code"`
		Type   string      `json:"type"`
		Value  json.Number `json:"value"`
		UserID int64       `json:"user_id"`
		Notes  string      `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return
	}
	amount, err := clients.ParseUSDDecimalMicros(strings.TrimPrefix(string(input.Value), "-"))
	if strings.HasPrefix(string(input.Value), "-") {
		amount = -amount
	}
	if err != nil || amount == 0 || input.Code == "" || input.Code != r.Header.Get("Idempotency-Key") || input.Type != "balance" {
		writeError(w, http.StatusBadRequest, "invalid_adjustment")
		return
	}
	f.mu.Lock()
	if amount > 0 {
		f.refundRequests++
	}
	for _, entry := range f.transactions {
		if entry.Code != input.Code {
			continue
		}
		f.mu.Unlock()
		if entry.Value != input.Value || entry.UsedBy != input.UserID || entry.Notes != input.Notes {
			writeError(w, http.StatusConflict, "redeem_conflict")
			return
		}
		d1Sub2APISuccess(w, struct {
			RedeemCode d1FinancialTransaction `json:"redeem_code"`
		}{entry})
		return
	}
	balance, found := f.balances[input.UserID]
	if !found || balance+amount < 0 {
		f.mu.Unlock()
		writeError(w, http.StatusConflict, "insufficient_balance")
		return
	}
	now := time.Now().UTC()
	entry := d1FinancialTransaction{Code: input.Code, Type: input.Type, Value: input.Value, AppliedValue: input.Value, UsedBy: input.UserID, Status: "used", UsedAt: now, CreatedAt: now, Notes: input.Notes}
	f.transactions = append(f.transactions, entry)
	f.balances[input.UserID] += amount
	var drop bool
	var arrived, release chan struct{}
	if amount > 0 {
		f.refundWrites++
		f.balances[input.UserID] -= f.consumeAfterRefund
		f.consumeAfterRefund = 0
		drop, f.dropRefundResponse = f.dropRefundResponse, false
		if drop {
			f.historyUnavailable = true
		}
		arrived, release = f.refundArrived, f.refundRelease
		f.refundArrived, f.refundRelease = nil, nil
	}
	f.mu.Unlock()
	if arrived != nil {
		close(arrived)
		<-release
	}
	if drop {
		connection, _, hijackErr := w.(http.Hijacker).Hijack()
		if hijackErr == nil {
			_ = connection.Close()
		}
		return
	}
	d1Sub2APISuccess(w, struct {
		RedeemCode d1FinancialTransaction `json:"redeem_code"`
	}{entry})
}

func d1Sub2APISuccess(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, struct {
		Code int `json:"code"`
		Data any `json:"data"`
	}{Data: data})
}
