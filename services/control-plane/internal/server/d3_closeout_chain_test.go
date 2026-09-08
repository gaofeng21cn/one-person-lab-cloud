package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// Real CP HTTP + PostgreSQL + financial HTTP client + Ledger HTTP. Resource
// provisioning and revocation are explicit isolated owner fixtures; their real
// adapter and upstream boundaries have separate tests.
type d3CloseoutFabric struct {
	*gatewayAccountingFabric
	muClose                                                 sync.Mutex
	frozen, absent, ready, unknown, loseFreeze, loseCleanup bool
	incomplete                                              bool
	closeCalls                                              int
}

func (f *d3CloseoutFabric) ReadWorkspaceLaunchStage(ctx context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	if input.Binding.Stage == "storage" {
		f.mu.Lock()
		_, exists := f.stages["storage"]
		f.mu.Unlock()
		if exists {
			return clients.WorkspaceLaunchStageResult{}, errors.New("lost original storage result")
		}
	}
	return f.gatewayAccountingFabric.ReadWorkspaceLaunchStage(ctx, input)
}
func (f *d3CloseoutFabric) EnsureWorkspaceLaunchStage(ctx context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.muClose.Lock()
	frozen := f.frozen
	f.muClose.Unlock()
	if frozen {
		return clients.WorkspaceLaunchStageResult{}, errors.New("frozen")
	}
	result, err := f.gatewayAccountingFabric.EnsureWorkspaceLaunchStage(ctx, input)
	if input.Binding.Stage == "storage" {
		return result, errors.New("lost original storage result")
	}
	return result, err
}
func (f *d3CloseoutFabric) result(input contracts.WorkspaceLaunchCloseoutInput) contracts.WorkspaceLaunchCloseoutResult {
	r := contracts.WorkspaceLaunchCloseoutResult{SchemaVersion: 1, Binding: input, State: "eligible", Reason: "resources_identified", Frozen: f.frozen, Resources: []contracts.WorkspaceLaunchCloseoutResource{{Stage: "runtime", ResourceID: input.WorkspaceID, State: "absent"}, {Stage: "storage", OperationID: input.LaunchOperationID + ":storage", State: "present"}, {Stage: "ensure_compute_allocation", OperationID: input.LaunchOperationID + ":ensure_compute_allocation", State: "present"}, {Stage: "secret", State: "absent"}, {Stage: "attachment", State: "absent"}}}
	if f.ready && !f.frozen {
		r.State, r.Reason = "blocked", "runtime_already_deliverable"
	}
	if f.unknown {
		r.State, r.Reason = "pending", "storage_owner_unknown"
	}
	if f.absent {
		r.State, r.Reason = "absent", "resources_absent"
		for i := range r.Resources {
			r.Resources[i].State = "absent"
		}
	}
	if f.incomplete {
		r.Resources = r.Resources[:len(r.Resources)-1]
	}
	return r
}
func (f *d3CloseoutFabric) ReadWorkspaceLaunchCloseout(_ context.Context, input contracts.WorkspaceLaunchCloseoutInput) (contracts.WorkspaceLaunchCloseoutResult, error) {
	f.muClose.Lock()
	defer f.muClose.Unlock()
	return f.result(input), nil
}
func (f *d3CloseoutFabric) FreezeWorkspaceLaunch(_ context.Context, input contracts.WorkspaceLaunchCloseoutInput) (contracts.WorkspaceLaunchCloseoutResult, error) {
	f.muClose.Lock()
	defer f.muClose.Unlock()
	r := f.result(input)
	if r.State == "blocked" {
		return r, nil
	}
	f.frozen = true
	if f.loseFreeze {
		f.loseFreeze = false
		return contracts.WorkspaceLaunchCloseoutResult{}, errors.New("lost freeze response")
	}
	return f.result(input), nil
}
func (f *d3CloseoutFabric) CloseoutWorkspaceLaunch(_ context.Context, input contracts.WorkspaceLaunchCloseoutInput) (contracts.WorkspaceLaunchCloseoutResult, error) {
	f.muClose.Lock()
	defer f.muClose.Unlock()
	if !f.frozen {
		return contracts.WorkspaceLaunchCloseoutResult{}, errors.New("missing freeze")
	}
	f.closeCalls++
	if !f.unknown {
		f.absent = true
	}
	if f.loseCleanup {
		f.loseCleanup = false
		return contracts.WorkspaceLaunchCloseoutResult{}, errors.New("lost cleanup response")
	}
	return f.result(input), nil
}

type d3CloseoutKeys struct {
	*clients.Sub2APIHTTPClient
	revoked  bool
	lose     bool
	expected clients.Sub2APIWorkspaceKeyRevokeInput
	calls    int
}

func (k *d3CloseoutKeys) RevokeWorkspaceKey(_ context.Context, input clients.Sub2APIWorkspaceKeyRevokeInput) error {
	if input != k.expected {
		return errors.New("incorrect original key identity")
	}
	k.calls++
	k.revoked = true
	if k.lose {
		k.lose = false
		return errors.New("lost revoke response")
	}
	return nil
}

type d3CloseoutLedger struct {
	clients.LedgerClient
	lose bool
}

func (l *d3CloseoutLedger) ReceiptForAccount(ctx context.Context, accountID, workspaceID, receiptID string) (clients.Receipt, error) {
	return l.LedgerClient.(clients.LedgerScopedReceiptClient).ReceiptForAccount(ctx, accountID, workspaceID, receiptID)
}

func (l *d3CloseoutLedger) ListReceipts(ctx context.Context, q clients.ReceiptQuery) (clients.ReceiptPage, error) {
	return l.LedgerClient.(clients.LedgerReceiptListClient).ListReceipts(ctx, q)
}
func (l *d3CloseoutLedger) RecordReceipt(ctx context.Context, input clients.ReceiptInput, key string) (clients.Receipt, error) {
	r, e := l.LedgerClient.RecordReceipt(ctx, input, key)
	if e == nil && input.Type == string(contracts.ReceiptTypeWorkspaceClosed) && l.lose {
		l.lose = false
		return clients.Receipt{}, errors.New("lost receipt response")
	}
	return r, e
}

type d3CloseoutChain struct {
	finance *d1FinanceChain
	fabric  *d3CloseoutFabric
	keys    *d3CloseoutKeys
	ledger  *d3CloseoutLedger
}

func newD3CloseoutChain(t *testing.T) *d3CloseoutChain {
	t.Helper()
	t.Setenv("OPL_TENCENT_ZONE", "na-siliconvalley-1")
	t.Setenv("NODE_ENV", "")
	t.Setenv("OPL_WORKSPACE_LAUNCH_WORKER_ENABLED", "0")
	ledger, _ := startGatewayAccountingLedger(t)
	c := &d3CloseoutChain{finance: &d1FinanceChain{accountID: "acct-d3-" + stableID(t.Name())[:12], databaseURL: gatewayAccountingDatabase(t), remote: newD1FinancialHTTPFixture(t)}, fabric: &d3CloseoutFabric{gatewayAccountingFabric: newGatewayAccountingFabric()}, ledger: &d3CloseoutLedger{LedgerClient: ledger}}
	c.keys = &d3CloseoutKeys{Sub2APIHTTPClient: c.finance.remote.sub2api.client}
	t.Setenv(controlledBasicPilotEnabledEnv, "1")
	t.Setenv(controlledBasicPilotAccountsEnv, c.finance.accountID)
	t.Setenv(controlledBasicPilotMaxInFlightEnv, "1")
	c.restart(t)
	account, user := provisionedAccountRowsFor(c.finance.accountID, "usr-d3-"+stableID(t.Name())[:12], c.finance.remote.sub2api.ownerEmail, gatewayAccountingSub2APIUserID)
	mustStore(t, c.finance.process.store.CreateProvisionedAccount(context.Background(), account, user))
	owner := c.finance.process.login(t, c.finance.remote.sub2api.ownerEmail, gatewayAccountingOwnerPassword)
	result := owner.mustRequest(t, http.MethodPost, "/api/workspace-launches", json.RawMessage(`{"name":"D3 original failed Workspace","packageId":"basic","autoRenew":false}`), "d3-closeout-original", http.StatusAccepted)
	id := stringValue(result["operationId"])
	for range 12 {
		_ = c.finance.process.handler.app.runWorkspaceLaunch(context.Background(), c.finance.process.handler.service, id)
		row, found, err := c.finance.process.store.GetRuntimeOperation(context.Background(), id)
		if err != nil || !found {
			t.Fatalf("original launch missing: %v", err)
		}
		c.finance.purchase, err = decodeWorkspaceLaunchReconcileOperation(row)
		if err != nil {
			t.Fatal(err)
		}
		if c.finance.purchase.Status == contracts.StatusManualReview {
			break
		}
	}
	op := c.finance.purchase
	if op.Status != contracts.StatusManualReview || op.Stage != contracts.StageStorage || !op.boolFact("chargeAttempted") {
		t.Fatalf("did not reach paid storage failure: %s", workspaceLaunchReconcileResultSummary(op))
	}
	c.keys.expected = clients.Sub2APIWorkspaceKeyRevokeInput{UserID: op.int64Fact("sub2apiUserId"), KeyID: op.int64Fact("workspaceApiKeyId"), ExactName: workspaceReservedKeyName(op.stringFact("workspaceId")), LaunchOperationID: op.ID}
	return c
}
func (c *d3CloseoutChain) restart(t *testing.T) {
	t.Helper()
	if c.finance.process != nil {
		c.finance.process.server.Close()
		_ = c.finance.process.store.(*postgresEntStateStore).client.Close()
	}
	store, err := newTestPostgresEntStateStore(c.finance.databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.(*postgresEntStateStore).client.Close() })
	service := controlplane.NewService(c.ledger, c.fabric, c.keys)
	handler, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	c.finance.process = &gatewayAccountingControlPlane{server: server, handler: handler.(*controlPlaneHTTPHandler), store: store, fabric: c.fabric.gatewayAccountingFabric}
	c.finance.session = reservedOperatorSessionForTest(t, c.finance.process.handler)
}
func (c *d3CloseoutChain) step(t *testing.T) workspaceLaunchReconcileOperation {
	t.Helper()
	p := c.finance.process
	if err := p.handler.app.runWorkspaceLaunch(context.Background(), p.handler.service, c.finance.purchase.ID); err != nil {
		t.Fatal(err)
	}
	row, _, err := p.store.GetRuntimeOperation(context.Background(), c.finance.purchase.ID)
	if err != nil {
		t.Fatal(err)
	}
	op, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	return op
}
func (c *d3CloseoutChain) authorize(t *testing.T) workspaceLaunchReconcileOperation {
	t.Helper()
	p := c.finance.process
	original := c.finance.purchase
	op, err := p.handler.app.closeWorkspaceLaunch(context.Background(), p.handler.service, original.ID, "d3-authorized-closeout", "usr-operator", "停止未完成的开通并原路退回余额", original.Version)
	if err != nil {
		t.Fatal(err)
	}
	return op
}

func TestPostgresD3CloseoutBusinessChain(t *testing.T) {
	for _, loss := range []bool{false, true} {
		t.Run(map[bool]string{false: "original order closes", true: "each owner response can be lost"}[loss], func(t *testing.T) {
			c := newD3CloseoutChain(t)
			c.fabric.loseFreeze, c.fabric.loseCleanup, c.keys.lose, c.ledger.lose = loss, loss, loss, loss
			op := c.authorize(t)
			if op.Closeout == nil || op.Closeout.Phase != "freeze" {
				t.Fatal("authorization did not freeze original continuation")
			}
			if _, err := c.finance.process.handler.app.workspaceLaunchReconciler(c.finance.process.handler.service, clients.SessionDelegatedCredential{}, 0).CheckResult(context.Background(), op.ID, workspaceLaunchResumeAuthorization{AuthorizationID: "forbidden-reopen", LaunchVersion: op.Version, AuthorizedStage: op.Stage, AuthorizedBy: "operator", AuthorizedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: "late check", AuthoritativeReadBudget: workspaceLaunchAuthoritativeReadBudget}); !errors.Is(err, errWorkspaceLaunchGrantConflict) {
				t.Fatalf("frozen launch reopened: %v", err)
			}
			for range 18 {
				c.restart(t)
				op = c.step(t)
				if op.Closeout.Phase == "complete" {
					break
				}
			}
			if op.Status != contracts.StatusRefunded || op.ID != c.finance.purchase.ID || op.Closeout == nil || op.Closeout.ReceiptID == "" || op.Closeout.RefundedUSDMicros != gatewayAccountingChargeMicros || !c.keys.revoked || !c.fabric.absent {
				_, _, _, refundErr := c.finance.process.handler.app.refundWorkspaceLaunchCloseout(context.Background(), c.finance.process.handler.service, op)
				t.Fatalf("business closeout incomplete: %s %#v refundErr=%v", workspaceLaunchReconcileResultSummary(op), op.Closeout, refundErr)
			}
			funds := c.finance.remote.snapshot()
			if funds.refundWrites != 1 || funds.refunded != gatewayAccountingChargeMicros || funds.ownerBalance != gatewayAccountingInitialMicros {
				t.Fatalf("money was lost or duplicated: %+v", funds)
			}
			replay, err := c.finance.process.handler.app.closeWorkspaceLaunch(context.Background(), c.finance.process.handler.service, op.ID, "d3-authorized-closeout", "usr-operator", "停止未完成的开通并原路退回余额", c.finance.purchase.Version)
			if err != nil || replay.Closeout.ReceiptID != op.Closeout.ReceiptID {
				t.Fatalf("original authorization replay changed: %v", err)
			}
			c.step(t)
			if c.finance.remote.snapshot() != funds {
				t.Fatal("terminal restart touched money")
			}
			if _, found, err := c.finance.process.store.GetWorkspace(context.Background(), op.stringFact("workspaceId")); err != nil || found {
				t.Fatal("failed workspace was activated")
			}
			receipt, err := c.finance.process.handler.service.BillingReceiptForAccount(context.Background(), op.stringFact("accountId"), op.stringFact("workspaceId"), op.Closeout.ReceiptID)
			if err != nil || !workspaceLaunchReceiptInputMatches(receipt.ReceiptInput, workspaceLaunchCloseoutReceiptInput(op)) {
				t.Fatalf("closure receipt mismatch: %v", err)
			}
			response, err := workspaceLaunchReconcileResponse(op, nil)
			if err != nil || response["status"] != "refunded" || !reflect.DeepEqual(response["closeout"], workspaceLaunchCloseoutResponse(op)) {
				t.Fatal("customer cannot see confirmed closure")
			}
			owner := c.finance.process.login(t, c.finance.remote.sub2api.ownerEmail, gatewayAccountingOwnerPassword)
			repurchase := owner.mustRequest(t, http.MethodPost, "/api/workspace-launches", json.RawMessage(`{"name":"Fresh purchase after original closeout","packageId":"basic","autoRenew":false}`), "repurchase-after-closeout", http.StatusAccepted)
			if stringValue(repurchase["operationId"]) == op.ID || stringValue(repurchase["operationId"]) == "" {
				t.Fatal("closed original still holds customer admission")
			}

		})
	}
}

func TestPostgresD3CloseoutUnknownResourcesNeverRefund(t *testing.T) {
	c := newD3CloseoutChain(t)
	c.authorize(t)
	c.step(t)
	c.step(t)
	c.fabric.unknown = true
	for range 3 {
		op := c.step(t)
		if op.Closeout.Phase != "resources" || op.Status != contracts.StatusPending {
			t.Fatalf("unknown became terminal: %#v", op.Closeout)
		}
	}
	if c.finance.remote.snapshot().refundWrites != 0 {
		t.Fatal("unknown resources refunded")
	}
	c.fabric.unknown = false
	c.fabric.incomplete = true
	if op := c.step(t); op.Closeout.Phase != "resources" || c.finance.remote.snapshot().refundWrites != 0 {
		t.Fatal("incomplete absence response released money")
	}
	c.fabric.incomplete = false
	for range 5 {
		op := c.step(t)
		if op.Closeout.Phase == "complete" {
			return
		}
	}
	t.Fatal("original closeout did not resume after owner absence")
}

func TestPostgresD3ReadyBeforeFreezeNeverRevokesOrRefunds(t *testing.T) {
	c := newD3CloseoutChain(t)
	c.authorize(t)
	c.fabric.ready = true
	op := c.step(t)
	if op.Closeout.Phase != "fulfilled" || op.Status != contracts.StatusManualReview || c.keys.calls != 0 || c.fabric.closeCalls != 0 || c.finance.remote.snapshot().refundWrites != 0 {
		t.Fatalf("late success destroyed: %#v", op.Closeout)
	}
}

func TestPostgresD3CloseoutRejectsUnknownOriginalDebit(t *testing.T) {
	c := newD3CloseoutChain(t)
	c.authorize(t)
	c.finance.remote.mu.Lock()
	c.finance.remote.historyUnavailable = true
	c.finance.remote.mu.Unlock()
	op := c.step(t)
	if op.Closeout.Phase != "freeze" || c.fabric.frozen || c.keys.revoked || c.finance.remote.snapshot().refundWrites != 0 {
		t.Fatal("unknown money reached destructive work")
	}
	c.finance.remote.mu.Lock()
	c.finance.remote.historyUnavailable = false
	c.finance.remote.mu.Unlock()
	if next := c.step(t); next.Closeout.Phase != "key" {
		t.Fatal("exact original payment could not recover")
	}
}

func TestD3CloseoutPersistsFailedContinuationLineage(t *testing.T) {
	op, err := decodeWorkspaceLaunchReconcileOperation(workspaceLaunchUnknownRuntimeWithFailedFreshContinuationRow(t))
	if err != nil {
		t.Fatal(err)
	}
	op.Closeout = &workspaceLaunchCloseout{AuthorizationID: "close-failed-runtime", LaunchVersion: op.Version, AuthorizedBy: "usr-admin", AuthorizedAt: time.Now().UTC().Format(time.RFC3339Nano), Reason: "unfulfilled", Phase: "freeze"}
	op.Status = contracts.StatusPending
	op.Version++
	row, err := workspaceLaunchReconcileOperationRow(op)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil || !reflect.DeepEqual(decoded.FreshContinuationAuthorizations, op.FreshContinuationAuthorizations) {
		t.Fatalf("closure corrupted recovery lineage: %v", err)
	}
}

func TestD3CloseoutOperatorRouteFreezesOriginalCAS(t *testing.T) {
	store := newMemoryTableStore()
	fabric := &d3CloseoutFabric{gatewayAccountingFabric: newGatewayAccountingFabric()}
	service := controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{})
	handler, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, handler)
	customer := tenantOwnerSessionForTest(t, handler)
	row := workspaceLaunchUnknownStorageManualReviewRow(t)
	original, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	base := "/api/operator/workspace-launches/" + original.ID
	preview := requestWithSession(t, handler, operator, http.MethodGet, base+"/recovery", "")
	var dto workspaceLaunchRecoveryDTO
	if preview.Code != http.StatusOK || json.Unmarshal(preview.Body.Bytes(), &dto) != nil || !reflect.DeepEqual(dto.AllowedActions, []string{"check_result", "close_unfulfilled"}) {
		t.Fatalf("closeout unavailable: %d %s", preview.Code, preview.Body.String())
	}
	body, _ := json.Marshal(struct {
		Action        string `json:"action"`
		LaunchVersion int    `json:"launchVersion"`
		Reason        string `json:"reason"`
	}{"close_unfulfilled", original.Version, "客户终止未完成的开通"})
	denied := requestWithMutationKeyForTest(t, handler, customer, http.MethodPost, base+"/recover", string(body), "close-customer")
	if denied.Code != http.StatusForbidden {
		t.Fatalf("customer got operator mutation: %d", denied.Code)
	}
	response := requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, base+"/recover", string(body), "close-original")
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &dto) != nil || dto.Closeout == nil || dto.Closeout.Status != "confirming" || len(dto.AllowedActions) != 0 {
		t.Fatalf("close response: %d %s", response.Code, response.Body.String())
	}
	replay := requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, base+"/recover", string(body), "close-original")
	if replay.Code != http.StatusOK || replay.Body.String() != response.Body.String() {
		t.Fatal("idempotent close changed")
	}
	conflict := requestWithMutationKeyForTest(t, handler, operator, http.MethodPost, base+"/recover", string(body), "close-another")
	if conflict.Code != http.StatusConflict {
		t.Fatal("second authorization admitted")
	}
	// A worker holding the pre-freeze snapshot must lose before another dispatch.
	original.Status = contracts.StatusPending
	if _, err := NewWorkspaceLaunchReconciler(store, &workspaceLaunchUnitAdapter{}).persist(context.Background(), original); !errors.Is(err, errWorkspaceLaunchCASConflict) {
		t.Fatalf("stale worker overwrote freeze: %v", err)
	}
	if fabric.frozen || fabric.closeCalls != 0 {
		t.Fatal("read/authorize unexpectedly deleted resources")
	}
	current, _, _ := store.GetRuntimeOperation(context.Background(), original.ID)
	closed, err := decodeWorkspaceLaunchReconcileOperation(current)
	if err != nil {
		t.Fatal(err)
	}
	closed.Closeout = nil
	if _, err := NewWorkspaceLaunchReconciler(store, &workspaceLaunchUnitAdapter{}).persist(context.Background(), closed); !errors.Is(err, errWorkspaceLaunchCASConflict) {
		t.Fatalf("current worker stripped durable closeout: %v", err)
	}

}

func TestD3CloseoutWithoutDebitDoesNotCreateRefund(t *testing.T) {
	store := newMemoryTableStore()
	command := workspaceLaunchUnitCommand()
	disabled := false
	command.ResourceBillingEnabled = &disabled
	op, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	op.Status = contracts.StatusManualReview
	row, err := workspaceLaunchReconcileOperationRow(op)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	fabric := &d3CloseoutFabric{gatewayAccountingFabric: newGatewayAccountingFabric(), absent: true}
	keys := &d3CloseoutKeys{expected: clients.Sub2APIWorkspaceKeyRevokeInput{UserID: op.int64Fact("sub2apiUserId"), ExactName: workspaceReservedKeyName(op.stringFact("workspaceId")), LaunchOperationID: op.ID}}
	ledger := &workspaceLaunchRepairLedger{receipts: map[string]clients.Receipt{}}
	service := controlplane.NewService(ledger, fabric, keys)
	app := &controlPlaneServer{tables: store}
	_, err = app.closeWorkspaceLaunch(context.Background(), service, op.ID, "close-no-charge", "usr-operator", "unfulfilled without payment", op.Version)
	if err != nil {
		t.Fatal(err)
	}
	r := app.workspaceLaunchReconciler(service, clients.SessionDelegatedCredential{}, 0)
	for range 6 {
		op, err = r.Reconcile(context.Background(), op.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	if op.Status != contracts.StatusFailed || op.Closeout.Phase != "complete" || op.Closeout.RefundedUSDMicros != 0 || op.Closeout.RefundOperationID != "" || !keys.revoked || ledger.records != 1 {
		t.Fatalf("uncharged closeout fabricated a refund: %+v", op.Closeout)
	}
	receipt := workspaceLaunchCloseoutReceiptInput(op)
	if receipt.Cost["chargeUsdMicros"] != json.Number("0") || receipt.Execution["outcome"] != "failed" {
		t.Fatalf("uncharged receipt has money: %+v", receipt)
	}
}

type d3UnknownIdentityKeys struct {
	*d3CloseoutKeys
	identities []clients.Sub2APIWorkspaceKey
}

func (k *d3UnknownIdentityKeys) WorkspaceKeysForRevocation(context.Context, int64, string) ([]clients.Sub2APIWorkspaceKey, error) {
	return k.identities, nil
}

func TestD3CloseoutUnknownKeyIDCannotUseNameAbsenceAsProof(t *testing.T) {
	store := newMemoryTableStore()
	op, err := decodeWorkspaceLaunchReconcileOperation(workspaceLaunchUnknownStageManualReviewRow(t, contracts.StageKey))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	op.Closeout = &workspaceLaunchCloseout{AuthorizationID: "close-unknown-key", LaunchVersion: op.Version, AuthorizedBy: "usr-operator", AuthorizedAt: now, Reason: "unfulfilled", Phase: "key", FrozenAt: now, DebitState: "absent"}
	op.Status = contracts.StatusPending
	op.Version++
	row, err := workspaceLaunchReconcileOperationRow(op)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	keys := &d3UnknownIdentityKeys{d3CloseoutKeys: &d3CloseoutKeys{}}
	service := controlplane.NewService(fakeLedgerClient{}, &d3CloseoutFabric{gatewayAccountingFabric: newGatewayAccountingFabric(), frozen: true}, keys)
	app := &controlPlaneServer{tables: store}
	next, err := app.workspaceLaunchReconciler(service, clients.SessionDelegatedCredential{}, 0).Reconcile(context.Background(), op.ID)
	if err != nil || next.Closeout.Phase != "key" || next.Closeout.KeyRevokedAt != "" || keys.calls != 0 {
		t.Fatalf("name absence lost unknown original key: %+v %v", next.Closeout, err)
	}
	name := workspaceReservedKeyName(op.stringFact("workspaceId"))
	keys.identities = []clients.Sub2APIWorkspaceKey{{ID: 77, UserID: op.int64Fact("sub2apiUserId"), Name: name}}
	keys.expected = clients.Sub2APIWorkspaceKeyRevokeInput{UserID: op.int64Fact("sub2apiUserId"), KeyID: 77, ExactName: name, LaunchOperationID: op.ID}
	next, err = app.workspaceLaunchReconciler(service, clients.SessionDelegatedCredential{}, 0).Reconcile(context.Background(), op.ID)
	if err != nil || next.int64Fact("workspaceApiKeyId") != 77 || next.stringFact("workspaceKeyStatus") != "" || keys.calls != 0 {
		t.Fatalf("original key identity was not saved without success facts: %v", err)
	}
	next, err = app.workspaceLaunchReconciler(service, clients.SessionDelegatedCredential{}, 0).Reconcile(context.Background(), op.ID)
	if err != nil || next.Closeout.Phase != "resources" || keys.calls != 1 {
		t.Fatalf("resolved original key could not revoke after restart: %v", err)
	}
}

func TestPostgresD3HistoricalPaidLaunchWithoutBillingFlagRefunds(t *testing.T) {
	c := newD3CloseoutChain(t)
	original := c.finance.purchase
	delete(original.raw, "resourceBillingEnabled")
	row, err := workspaceLaunchReconcileOperationRow(original)
	if err != nil {
		t.Fatal(err)
	}
	// af7ac0a8 introduced the flag within schema 3. Retained earlier paid rows
	// have no flag and remain paid; this is persisted historical input.
	mustStore(t, c.finance.process.store.SaveRuntimeOperation(context.Background(), row))
	c.finance.purchase, err = decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	c.authorize(t)
	var op workspaceLaunchReconcileOperation
	for range 8 {
		op = c.step(t)
		if op.Status == contracts.StatusRefunded {
			break
		}
	}
	if op.Status != contracts.StatusRefunded || op.Closeout.DebitState != "confirmed" || op.Closeout.RefundedUSDMicros != gatewayAccountingChargeMicros || c.finance.remote.snapshot().refundWrites != 1 {
		t.Fatalf("historical paid order lost its refund: %+v", op.Closeout)
	}
}
