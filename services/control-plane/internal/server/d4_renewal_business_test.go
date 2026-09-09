package server

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type d4RenewalWalletHTTP struct {
	mu           sync.Mutex
	balance      int64
	writes       []string
	history      map[string]clients.Sub2APICharge
	loseResponse bool
	client       *clients.Sub2APIHTTPClient
}

func newD4RenewalWalletHTTP(t *testing.T, balance int64) *d4RenewalWalletHTTP {
	t.Helper()
	wallet := &d4RenewalWalletHTTP{balance: balance, history: make(map[string]clients.Sub2APICharge)}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wallet.mu.Lock()
		defer wallet.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		success := func(data any) {
			if err := json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data}); err != nil {
				t.Error(err)
			}
		}
		switch r.URL.Path {
		case "/api/v1/auth/login":
			var input struct {
				Email string `json:"email"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Error(err)
			}
			success(map[string]any{"access_token": "local-access", "refresh_token": "local-refresh", "user": clients.Sub2APIIdentity{ID: 1, Email: input.Email, Status: "active"}})
		case "/api/v1/admin/users/1":
			success(clients.Sub2APIIdentity{ID: 1, Email: "admin@opl.local", Status: "active"})
		case "/api/v1/admin/users/41":
			success(map[string]any{"id": 41, "status": "active", "balance": json.RawMessage(fmt.Sprintf("%d.%06d", wallet.balance/1_000_000, wallet.balance%1_000_000))})
		case "/api/v1/admin/users/41/api-keys":
			success(map[string]any{"items": []any{map[string]any{"id": 9, "user_id": 41, "name": workspaceReservedKeyName("workspace-monthly"), "key": "local-workspace-key", "status": "active", "quota": 0, "quota_used": 0, "usage_5h": 0, "usage_1d": 0, "usage_7d": 0}}, "total": 1, "page": 1, "page_size": 1, "pages": 1})
		case "/api/v1/admin/usage/search-api-keys":
			success([]any{map[string]any{"id": 9, "user_id": 41, "name": workspaceReservedKeyName("workspace-monthly")}})
		case "/api/v1/admin/redeem-codes/create-and-redeem":
			var input struct {
				Code   string      `json:"code"`
				Type   string      `json:"type"`
				Value  json.Number `json:"value"`
				UserID int64       `json:"user_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil || r.Method != http.MethodPost || input.UserID != 41 || input.Type != "balance" || input.Value.String() != "-52.580000" || r.Header.Get("Idempotency-Key") != input.Code {
				t.Errorf("invalid renewal money boundary: input=%#v err=%v", input, err)
				http.Error(w, "invalid", 400)
				return
			}
			wallet.writes = append(wallet.writes, input.Code)
			if _, exists := wallet.history[input.Code]; exists {
				http.Error(w, "already redeemed", http.StatusConflict)
				return
			}
			if wallet.balance < 52_580_000 {
				http.Error(w, "insufficient", http.StatusBadRequest)
				return
			}
			wallet.balance -= 52_580_000
			wallet.history[input.Code] = clients.Sub2APICharge{Code: input.Code, UserID: 41, ChargeUSDMicros: 52_580_000, Status: "used"}
			if wallet.loseResponse {
				wallet.loseResponse = false
				http.Error(w, "response lost after applied debit", http.StatusServiceUnavailable)
				return
			}
			success(map[string]any{"redeem_code": authoritativeHistoryEntry(input.Code, "-52.580000")})
		case "/api/v1/admin/redeem-codes/by-code":
			if r.Method != http.MethodGet || r.URL.Query().Get("user_id") != "41" {
				t.Errorf("invalid exact lookup: %s", r.URL.String())
				http.Error(w, "invalid", 400)
				return
			}
			code := r.URL.Query().Get("code")
			var record any
			if _, ok := wallet.history[code]; ok {
				record = authoritativeHistoryEntry(code, "-52.580000")
			}
			success(map[string]any{"lookup": "exact_code_v1", "redeem_code": record})
		default:
			t.Errorf("unexpected wallet HTTP request %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected", 400)
		}
	}))
	t.Cleanup(upstream.Close)
	client, err := clients.NewSub2APIHTTPClient(clients.Sub2APIConfig{BaseURL: upstream.URL, AdminEmail: "admin@opl.local", AdminPassword: "local-secret", Timeout: time.Second}, upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	wallet.client = client
	return wallet
}

func d4RenewalOwnerSession(t *testing.T, handler *controlPlaneHTTPHandler) *httptest.ResponseRecorder {
	t.Helper()
	user, found, err := handler.app.tables.GetUser(context.Background(), "usr-monthly-owner")
	if err != nil || !found {
		t.Fatalf("owner missing: %v", err)
	}
	payload, id, err := handler.app.createSession(user, "local-delegated-token")
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	http.SetCookie(rec, sessionCookie(id, 12*60*60))
	rec.Header().Set("x-opl-csrf-token", stringValue(payload["csrfToken"]))
	return rec
}

func TestD4ExpiredWorkspaceRequiresExplicitOriginalRenewalAfterTopUp(t *testing.T) {
	fixture := newWorkspaceRenewalRuntimeFixture(t, nil)
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	seedTenantMember(t, store, "acct-monthly", "org-monthly", "usr-monthly-owner", "monthly-owner@example.com")
	mustStore(t, store.SaveCompute(context.Background(), fixture.compute))
	mustStore(t, store.SaveStorage(context.Background(), fixture.storage))
	mustStore(t, store.SaveWorkspace(context.Background(), fixture.workspace))
	seedD4RenewalRuntime(t, store, fixture.workspace, fixture.fabric)
	wallet := newD4RenewalWalletHTTP(t, 10_000_000)
	fixture.service = controlplane.NewService(fixture.ledger, fixture.fabric, wallet.client)
	fixture.app, _ = newControlPlaneAppWithStore(store)
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, fixture.paidThrough.Add(-monthlyRenewalLead)); err != nil {
		t.Fatal(err)
	}
	if len(wallet.writes) != 0 {
		t.Fatal("insufficient balance was charged")
	}
	now := time.Now().UTC()
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now); err != nil {
		t.Fatal(err)
	}
	expired := d1RenewalOperation(t, fixture)
	if expired.Status != "expired_unpaid" {
		t.Fatalf("unpaid expiry=%#v", expired)
	}
	wallet.balance = 100_000_000
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	workspace, _ := fixture.app.getWorkspace(expired.WorkspaceID)
	if workspace["state"] != "suspended" || len(wallet.writes) != 0 {
		t.Fatalf("top-up silently resumed workspace: %#v writes=%q", workspace, wallet.writes)
	}
	server, err := NewPersistentServer(fixture.service, store)
	if err != nil {
		t.Fatal(err)
	}
	owner := d4RenewalOwnerSession(t, server.(*controlPlaneHTTPHandler))
	get := requestWithSession(t, server, owner, http.MethodGet, "/api/workspaces/workspace-monthly/renewal", "")
	var status struct {
		Recovery workspaceRenewalRecovery `json:"recovery"`
	}
	if err := json.Unmarshal(get.Body.Bytes(), &status); err != nil || get.Code != 200 || status.Recovery.State != "recoverable" {
		t.Fatalf("recovery qualification: status=%d body=%s err=%v", get.Code, get.Body.String(), err)
	}
	response := requestWithMutationKeyForTest(t, server, owner, http.MethodPost, "/api/workspaces/workspace-monthly/auto-renew", `{"autoRenew":true}`, "d4-original-recovery")
	if response.Code != 200 {
		t.Fatalf("authorize recovery status=%d body=%s", response.Code, response.Body.String())
	}
	wallet.loseResponse = true
	fixture.app, _ = newControlPlaneAppWithStore(store)
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(2*time.Second)); !errors.Is(err, clients.ErrSub2APIChargeUnknown) {
		t.Fatalf("lost response err=%v", err)
	}
	pending := d1RenewalOperation(t, fixture)
	if pending.ID != expired.ID || pending.RedeemCode != expired.RedeemCode || len(wallet.writes) != 1 || len(fixture.fabric.computeRenewKeys) != 0 {
		t.Fatalf("recovery replaced original or fulfilled unknown: %#v writes=%q", pending, wallet.writes)
	}
	restarted, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app = restarted
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	completed := d1RenewalOperation(t, fixture)
	workspace, _ = fixture.app.getWorkspace(completed.WorkspaceID)
	if completed.Status != "active" || completed.ID != expired.ID || workspace["state"] != "running" || workspace["paidThrough"] != fixture.renewedThrough.Format(time.RFC3339Nano) || len(wallet.writes) != 1 || len(wallet.history) != 1 || wallet.balance != 47_420_000 || len(fixture.fabric.computeRenewKeys) != 1 || len(fixture.fabric.storageRenewKeys) != 1 {
		t.Fatalf("original renewal did not recover exactly once: operation=%#v workspace=%#v writes=%q", completed, workspace, wallet.writes)
	}
	replay := requestWithMutationKeyForTest(t, server, owner, http.MethodPost, "/api/workspaces/workspace-monthly/auto-renew", `{"autoRenew":true}`, "d4-original-recovery")
	if replay.Code != 200 || replay.Body.String() != response.Body.String() {
		t.Fatalf("authorization replay changed: %d %s", replay.Code, replay.Body.String())
	}
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(4*time.Second)); err != nil {
		t.Fatal(err)
	}
	if len(wallet.writes) != 1 || len(fixture.ledger.receipts) != 2 {
		t.Fatalf("replay repeated settlement or lost expiry evidence: writes=%q receipts=%#v", wallet.writes, fixture.ledger.receipts)
	}
}

func TestD4RenewalRechecksReclaimedResourcesBeforeRetryDebit(t *testing.T) {
	for _, lostResponse := range []bool{false, true} {
		t.Run(fmt.Sprintf("attempted_%t", lostResponse), func(t *testing.T) {
			fixture := newWorkspaceRenewalWorkerFixture(t, []int64{1_000_000, 100_000_000})
			now := fixture.paidThrough.Add(-monthlyRenewalLead)
			if lostResponse {
				fixture.sub2API.balances = []int64{100_000_000}
				fixture.sub2API.chargeErrors = []error{clients.ErrSub2APIChargeUnknown}
			}
			firstErr := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now)
			if lostResponse && !errors.Is(firstErr, clients.ErrSub2APIChargeUnknown) {
				t.Fatalf("initial err=%v", firstErr)
			}
			before := len(fixture.sub2API.charges)
			fixture.fabric.storageRenew.Status = "external_deleted"
			_ = fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(time.Second))
			operation := d1RenewalOperation(t, fixture)
			if operation.Status != "manual_review" || operation.ErrorCode != "workspace_renewal_resources_reclaimed" || len(fixture.sub2API.charges) != before || len(fixture.sub2API.refunds) != 0 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.ledger.receipts) != 0 {
				t.Fatalf("reclaimed resources were charged or replaced: %#v charges=%#v", operation, fixture.sub2API.charges)
			}
		})
	}
}

func TestD4WorkspaceExistingWebSocketClosesAtPaidThrough(t *testing.T) {
	t.Run("unpaid", func(t *testing.T) { d4WorkspaceExistingWebSocketAtPaidThrough(t, false) })
	t.Run("renewed_before_expiry", func(t *testing.T) { d4WorkspaceExistingWebSocketAtPaidThrough(t, true) })
}

func d4WorkspaceExistingWebSocketAtPaidThrough(t *testing.T, renew bool) {
	t.Helper()
	backendClosed := make(chan struct{})
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" {
			_, _ = io.WriteString(w, "runtime-ready")
			return
		}
		if r.Header.Get("Cookie") != workspaceRuntimeSessionCookieName+"=existing-login" {
			t.Errorf("old runtime login not forwarded: %q", r.Header.Get("Cookie"))
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		defer close(backendClosed)
		accept := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(accept[:]))
		rw.Flush()
		for {
			frame := make([]byte, 8)
			if _, err := io.ReadFull(rw, frame); err != nil {
				return
			}
			if frame[0] != 0x81 || frame[1] != 0x82 {
				t.Errorf("unexpected websocket frame %x", frame)
				return
			}
			if _, err := rw.Write([]byte{0x81, 2, frame[6] ^ frame[2], frame[7] ^ frame[3]}); err != nil {
				t.Error(err)
				return
			}
			rw.Flush()
		}
	}))
	defer backend.Close()
	original := http.DefaultTransport
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address != "opl-runtime-d4:3000" {
			return nil, fmt.Errorf("unexpected destination %s", address)
		}
		return (&net.Dialer{}).DialContext(ctx, network, backend.Listener.Addr().String())
	}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = original; transport.CloseIdleConnections() }()
	fixture := newWorkspaceRenewalRuntimeFixture(t, []int64{100_000_000, 47_420_000})
	store := fixture.app.tables.(*memoryTableStore)
	server, err := NewPersistentServer(fixture.service, store)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app = server.(*controlPlaneHTTPHandler).app
	owner := d4RenewalOwnerSession(t, server.(*controlPlaneHTTPHandler))
	launchRows, err := queryRuntimeOperations(context.Background(), store, runtimeOperationQuery{WorkspaceID: "workspace-monthly", Action: workspaceLaunchAction})
	if err != nil || len(launchRows) != 1 {
		t.Fatalf("missing Launch: %v", err)
	}
	operation, err := decodeWorkspaceLaunchReconcileOperation(launchRows[0])
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().UTC().Add(1500 * time.Millisecond)
	start := deadline.AddDate(0, -1, 0)
	for key, value := range map[string]any{"runtimeServiceName": "opl-runtime-d4", "periodStart": start.Format(time.RFC3339Nano), "paidThrough": deadline.Format(time.RFC3339Nano), "billingAnchorDay": deadline.Day()} {
		operation.raw[key] = mustJSON(value)
	}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store.runtimeOps = []map[string]any{row}
	workspace, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	workspace = mergeMaps(workspace, fixture.workspace)
	workspace["periodStart"], workspace["paidThrough"] = start.Format(time.RFC3339Nano), deadline.Format(time.RFC3339Nano)
	workspace["billingAnchorDay"], workspace["nextRenewalAt"] = deadline.Day(), deadline.Add(-monthlyRenewalLead).Format(time.RFC3339Nano)
	fixture.workspace, fixture.paidThrough = workspace, deadline
	fixture.renewedThrough = nextBillingMonth(deadline, deadline.Day())
	fixture.fabric.computeRenew.Deadline = fixture.renewedThrough.Format(time.RFC3339Nano)
	fixture.fabric.storageRenew.Deadline = fixture.renewedThrough.Format(time.RFC3339Nano)
	fixture.fabric.computeSync, fixture.fabric.storageSync = fixture.fabric.computeRenew, fixture.fabric.storageRenew
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))
	proxy := httptest.NewServer(server)
	defer proxy.Close()
	conn, err := net.Dial("tcp", proxy.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	cookies := []string{workspaceGatewayRuntimeSessionCookieName("workspace-monthly") + "=existing-login"}
	for _, cookie := range owner.Result().Cookies() {
		cookies = append(cookies, cookie.Name+"="+cookie.Value)
	}
	fmt.Fprintf(conn, "GET /w/workspace-monthly/ HTTP/1.1\r\nHost: localhost\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nCookie: %s\r\n\r\n", strings.Join(cookies, "; "))
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 101 {
		body, _ := io.ReadAll(response.Body)
		t.Fatalf("workspace socket status=%d body=%s", response.StatusCode, body)
	}
	conn.Write([]byte{0x81, 0x82, 1, 2, 3, 4, 'o' ^ 1, 'k' ^ 2})
	frame := make([]byte, 4)
	if _, err := io.ReadFull(reader, frame); err != nil || string(frame[2:]) != "ok" {
		t.Fatalf("existing logged-in websocket failed before expiry: frame=%x err=%v", frame, err)
	}
	if renew {
		oldDeadline := deadline
		if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		renewal := d1RenewalOperation(t, fixture)
		if renewal.Status != "active" || !renewal.EntitlementCommitted || len(fixture.sub2API.charges) != 1 || len(fixture.fabric.runtimePowerCalls) != 0 {
			t.Fatalf("ordinary renewal did not commit uninterrupted paid service: %#v", renewal)
		}
		timer := time.NewTimer(time.Until(oldDeadline.Add(100 * time.Millisecond)))
		defer timer.Stop()
		<-timer.C
		if _, err := conn.Write([]byte{0x81, 0x82, 1, 2, 3, 4, 'o' ^ 1, 'k' ^ 2}); err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(reader, frame); err != nil || string(frame[2:]) != "ok" {
			t.Fatalf("paid renewal disconnected the existing socket at old expiry: frame=%x err=%v", frame, err)
		}
		request := httptest.NewRequest(http.MethodGet, "/w/workspace-monthly/", nil)
		request.Header.Set("Cookie", strings.Join(cookies, "; "))
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != "runtime-ready" {
			t.Fatalf("renewed workspace rejected a new request: %d %s", response.Code, response.Body.String())
		}
		currentLaunch, found, err := store.GetRuntimeOperation(context.Background(), operation.ID)
		if err != nil || !found || stringValue(currentLaunch["result"]) != stringValue(row["result"]) {
			t.Fatalf("renewal rewrote initial Launch evidence: %v", err)
		}
		for _, mutation := range []struct {
			name  string
			apply func(*workspaceRenewalOperation)
		}{
			{"uncommitted", func(op *workspaceRenewalOperation) { op.EntitlementCommitted = false }},
			{"foreign_compute", func(op *workspaceRenewalOperation) { op.ComputeID = "other-compute" }},
			{"unconfirmed_debit", func(op *workspaceRenewalOperation) { op.ChargeConfirmation = nil }},
		} {
			t.Run(mutation.name, func(t *testing.T) {
				invalid := renewal
				mutation.apply(&invalid)
				mustStore(t, store.SaveRuntimeOperation(context.Background(), workspaceRenewalOperationRow(invalid)))
				defer func() {
					mustStore(t, store.SaveRuntimeOperation(context.Background(), workspaceRenewalOperationRow(renewal)))
				}()
				denied := requestWithSession(t, server, owner, http.MethodGet, "/w/workspace-monthly/", "")
				if denied.Code != http.StatusConflict || !strings.Contains(denied.Body.String(), "workspace_runtime_truth_unavailable") {
					t.Fatalf("renewed dates admitted without original committed entitlement: %d %s", denied.Code, denied.Body.String())
				}
			})
		}
		return
	}
	_, err = reader.ReadByte()
	if err == nil {
		t.Fatal("expired websocket remained usable")
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		t.Fatalf("proxy did not close socket at paid expiry: %v", err)
	}
	if time.Now().Before(deadline) {
		t.Fatal("paid socket closed before entitlement ended")
	}
	select {
	case <-backendClosed:
	case <-time.After(time.Second):
		t.Fatal("upstream socket still running after expiry")
	}
	denied := requestWithSession(t, server, owner, http.MethodGet, "/w/workspace-monthly/", "")
	if denied.Code != http.StatusConflict || !strings.Contains(denied.Body.String(), "workspace_billing_period_expired") {
		t.Fatalf("old session regained unpaid access: %d %s", denied.Code, denied.Body.String())
	}
}

func seedD4RenewalRuntime(t *testing.T, store controlPlaneTableStore, workspace map[string]any, fabric *monthlyFabric) {
	t.Helper()
	command := workspaceLaunchUnitCommand()
	command.OperationID, command.AccountID, command.OwnerUserID = "workspace-launch-monthly", stringValue(workspace["accountId"]), stringValue(workspace["ownerUserId"])
	command.WorkspaceID = stringValue(workspace["id"])
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range workspaceLaunchReconcileStages[:len(workspaceLaunchReconcileStages)-1] {
		operation.Stage = stage
		facts := workspaceLaunchReadyFacts(stage)
		for key, value := range map[string]any{"computeAllocationId": workspace["computeAllocationId"], "storageId": workspace["storageId"], "attachmentId": workspace["currentAttachmentId"], "runtimeId": "runtime-workspace-monthly", "runtimeServiceName": "opl-runtime-monthly"} {
			if _, ok := facts[key]; ok {
				facts[key] = value
			}
		}
		if stage == contracts.StageRuntime {
			facts["runtimeBindingRef"] = operation.ID + ":runtime"
		}
		if stage == contracts.StageActivation {
			facts["activationOperationId"] = operation.ID + ":activation"
		}
		if stage == contracts.StageReceipt {
			facts["receiptOperationId"] = operation.ID + ":purchase-receipt"
		}
		observation, err := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{State: workspaceLaunchStageReady, Facts: facts})
		if err != nil {
			t.Fatalf("seed renewal Runtime stage %s: %v", stage, err)
		}
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Status = 1, 1, "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
		operation.Attempts[stage], operation.Observations[stage] = attempt, observation
	}
	operation.Stage, operation.Status = contracts.StageSucceeded, contracts.StatusSucceeded
	operation.Version = len(workspaceLaunchReconcileStages)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	workspace["runtimeId"] = "runtime-workspace-monthly"
	mustStore(t, store.SaveWorkspace(context.Background(), workspace))
	fabric.runtimePowerIdentity = contracts.WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: command.AccountID, WorkspaceID: command.WorkspaceID, RuntimeID: "runtime-workspace-monthly", RuntimeOperationID: operation.stringFact("runtimeBindingRef")}
	fabric.runtimePowerState = "running"
}

func (fabric *monthlyFabric) ReadWorkspaceRuntimePower(_ context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	fabric.runtimePowerReads = append(fabric.runtimePowerReads, input)
	if err := fabric.validateRuntimePower(input); err != nil {
		return contracts.WorkspaceRuntimePowerResult{}, err
	}
	return contracts.WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: fabric.runtimePowerState}, nil
}

func (fabric *monthlyFabric) SetWorkspaceRuntimePower(_ context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	if err := fabric.validateRuntimePower(input); err != nil {
		return contracts.WorkspaceRuntimePowerResult{}, err
	}
	fabric.runtimePowerCalls = append(fabric.runtimePowerCalls, input)
	if fabric.runtimePowerErr != nil {
		return contracts.WorkspaceRuntimePowerResult{}, fabric.runtimePowerErr
	}
	fabric.runtimePowerState = input.DesiredState
	return contracts.WorkspaceRuntimePowerResult{SchemaVersion: 1, Binding: input, State: fabric.runtimePowerState}, nil
}

func (fabric *monthlyFabric) validateRuntimePower(input contracts.WorkspaceRuntimePowerInput) error {
	binding := fabric.runtimePowerIdentity
	if binding.RuntimeID == "" || input.SchemaVersion != 1 || input.AccountID != binding.AccountID || input.WorkspaceID != binding.WorkspaceID || input.RuntimeID != binding.RuntimeID || input.RuntimeOperationID != binding.RuntimeOperationID || input.IdempotencyKey == "" || input.PaidThrough == "" || input.DesiredState != "running" && input.DesiredState != "suspended" {
		return errors.New("runtime_power_identity_mismatch")
	}
	if _, err := time.Parse(time.RFC3339, input.PaidThrough); err != nil {
		return err
	}
	return nil
}

func newWorkspaceRenewalRuntimeFixture(t *testing.T, balances []int64) workspaceRenewalWorkerFixture {
	t.Helper()
	fixture := newWorkspaceRenewalWorkerFixture(t, balances)
	seedD4RenewalRuntime(t, fixture.app.tables, fixture.workspace, fixture.fabric)
	return fixture
}

func d4RenewalPostgresFixture(t *testing.T, balances []int64) workspaceRenewalWorkerFixture {
	t.Helper()
	fixture := newWorkspaceRenewalRuntimeFixture(t, balances)
	store, _ := newPostgresWorkspaceRenewalStoreWithDB(t)
	seedTenantMember(t, store, "acct-monthly", "org-monthly", "usr-monthly-owner", "monthly-owner@example.com")
	mustStore(t, store.SaveCompute(context.Background(), fixture.compute))
	mustStore(t, store.SaveStorage(context.Background(), fixture.storage))
	seedD4RenewalRuntime(t, store, fixture.workspace, fixture.fabric)
	var err error
	fixture.app, err = newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	return fixture
}

type d4LostPowerResponseFabric struct {
	*monthlyFabric
	lost bool
}

func (fabric *d4LostPowerResponseFabric) SetWorkspaceRuntimePower(ctx context.Context, input contracts.WorkspaceRuntimePowerInput) (contracts.WorkspaceRuntimePowerResult, error) {
	result, err := fabric.monthlyFabric.SetWorkspaceRuntimePower(ctx, input)
	if err == nil && !fabric.lost {
		fabric.lost = true
		return contracts.WorkspaceRuntimePowerResult{}, errors.New("runtime stop response lost after applied")
	}
	return result, err
}

func TestD4ExpiryRecoversLostStopResponseAfterPostgresRestart(t *testing.T) {
	fixture := d4RenewalPostgresFixture(t, nil)
	fabric := &d4LostPowerResponseFabric{monthlyFabric: fixture.fabric}
	fixture.service = controlplane.NewService(fixture.ledger, fabric, fixture.sub2API)
	now := fixture.paidThrough.Add(time.Second)
	if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now); err == nil {
		t.Fatal("lost Runtime stop response was treated as success")
	}
	pending := d1RenewalOperation(t, fixture)
	workspace, _ := fixture.app.getWorkspace(pending.WorkspaceID)
	if pending.ExpiryPhase != "runtime_suspend" || pending.ExpiryRuntimePower == nil || pending.ExpiryRuntimePower.State != "pending" || workspace["state"] != "suspended" || len(fixture.ledger.receipts) != 0 || len(fixture.fabric.runtimePowerCalls) != 1 || fixture.fabric.runtimePowerState != "suspended" {
		t.Fatalf("lost-stop checkpoint operation=%#v workspace=%#v", pending, workspace)
	}
	var err error
	fixture.app, err = newControlPlaneAppWithStore(fixture.app.tables)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := range 2 {
		if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(time.Duration(attempt+1)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	complete := d1RenewalOperation(t, fixture)
	if complete.ID != pending.ID || complete.ExpiryPhase != "complete" || complete.ExpiryRuntimePower == nil || complete.ExpiryRuntimePower.State != "suspended" || complete.ExpiryRuntimePower.Binding != pending.ExpiryRuntimePower.Binding || len(fixture.fabric.runtimePowerCalls) != 1 || len(fixture.ledger.receipts) != 1 || len(fixture.sub2API.charges) != 0 || len(fixture.fabric.computeRenewKeys) != 0 || len(fixture.fabric.storageRenewKeys) != 0 {
		t.Fatalf("restart duplicated effects or lost identity: %#v", complete)
	}
}

func TestD4ExpiryStopsPreviouslyCompletedUnpaidWorkspaceWithoutReplacingReceipt(t *testing.T) {
	for _, phase := range []string{"compute", "receipt", "complete"} {
		t.Run(phase, func(t *testing.T) {
			fixture := newWorkspaceRenewalRuntimeFixture(t, nil)
			now := fixture.paidThrough.Add(time.Second)
			operation, err := newWorkspaceRenewalOperation(fixture.workspace, now)
			if err != nil {
				t.Fatal(err)
			}
			operation.Status, operation.ExpiryStatus, operation.ExpiryPhase = "expired_unpaid", "expired_unpaid", phase
			operation.ExpiryPaidThrough, operation.ExpiryPeriodStart = operation.PaidThrough, operation.PeriodStart
			if phase == "complete" {
				operation.ExpiryReceiptID, operation.ReceiptID = "immutable-old-expiry-receipt", "immutable-old-expiry-receipt"
			}
			fixture.workspace["state"], fixture.workspace["status"], fixture.workspace["renewalStatus"], fixture.workspace["autoRenew"] = "suspended", "suspended", "expired_unpaid", false
			mustStore(t, fixture.app.tables.SaveWorkspace(context.Background(), fixture.workspace))
			mustStore(t, fixture.app.tables.SaveRuntimeOperation(context.Background(), workspaceRenewalOperationRow(operation)))
			for attempt := range 2 {
				if err := fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, now.Add(time.Duration(attempt)*time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			complete := d1RenewalOperation(t, fixture)
			wantNewReceipts := 1
			if phase == "complete" {
				wantNewReceipts = 0
				if complete.ExpiryReceiptID != operation.ExpiryReceiptID || complete.ReceiptID != operation.ReceiptID {
					t.Fatalf("old receipt replaced: %#v", complete)
				}
			}
			if complete.ID != operation.ID || complete.ExpiryPhase != "complete" || complete.ExpiryRuntimePower == nil || complete.ExpiryRuntimePower.State != "suspended" || len(fixture.fabric.runtimePowerCalls) != 1 || len(fixture.ledger.receipts) != wantNewReceipts || len(fixture.sub2API.charges) != 0 {
				t.Fatalf("historical expiry remained running or repeated settlement: %#v", complete)
			}
		})
	}
}

func TestD4ExpiryUsesPersistedRuntimeRepairBinding(t *testing.T) {
	for _, bindingCase := range []string{"repaired", "missing", "foreign"} {
		t.Run(bindingCase, func(t *testing.T) {
			fixture := newWorkspaceRenewalRuntimeFixture(t, nil)
			rows, err := queryRuntimeOperations(context.Background(), fixture.app.tables, runtimeOperationQuery{WorkspaceID: "workspace-monthly", Action: workspaceLaunchAction})
			if err != nil || len(rows) != 1 {
				t.Fatalf("Launch binding missing: %v", err)
			}
			launch, err := decodeWorkspaceLaunchReconcileOperation(rows[0])
			if err != nil {
				t.Fatal(err)
			}
			repairedBinding := launch.ID + ":runtime-repair:owner-authorized:create"
			binding := repairedBinding
			switch bindingCase {
			case "missing":
				binding = ""
			case "foreign":
				binding = "other-workspace:runtime"
			}
			launch.raw["runtimeBindingRef"], _ = json.Marshal(binding)
			launch.Observations[contracts.StageRuntime].Facts["runtimeBindingRef"] = binding
			row, err := workspaceLaunchReconcileOperationRow(launch)
			if err != nil {
				t.Fatal(err)
			}
			mustStore(t, fixture.app.tables.SaveRuntimeOperation(context.Background(), row))
			fixture.fabric.runtimePowerIdentity.RuntimeOperationID = repairedBinding
			err = fixture.app.runMonthlyBillingOnce(context.Background(), fixture.service, fixture.paidThrough.Add(time.Second))
			if bindingCase != "repaired" {
				if err == nil || len(fixture.fabric.runtimePowerCalls) != 0 || len(fixture.ledger.receipts) != 0 || len(fixture.sub2API.charges) != 0 {
					t.Fatalf("unproven Runtime binding mutated: err=%v power=%#v", err, fixture.fabric.runtimePowerCalls)
				}
				return
			}
			if err != nil || len(fixture.fabric.runtimePowerCalls) != 1 || fixture.fabric.runtimePowerCalls[0].RuntimeOperationID != repairedBinding || len(fixture.ledger.receipts) != 1 {
				t.Fatalf("repaired Runtime not stopped with current owner identity: err=%v power=%#v", err, fixture.fabric.runtimePowerCalls)
			}
		})
	}
}

type d4ExpiryCompletionStore struct {
	StateStore
	completed chan struct{}
}

func (store *d4ExpiryCompletionStore) PersistWorkspaceRenewal(ctx context.Context, input workspaceRenewalPersistCAS) error {
	if err := store.StateStore.PersistWorkspaceRenewal(ctx, input); err != nil {
		return err
	}
	operation, err := decodeWorkspaceRenewalOperation(input.DesiredOperation)
	if err == nil && operation.ExpiryPhase == "complete" {
		select {
		case store.completed <- struct{}{}:
		default:
		}
	}
	return nil
}

func TestD4MonthlyWorkerStopsAtObservedPaidThroughBeforePollingInterval(t *testing.T) {
	fixture := newWorkspaceRenewalRuntimeFixture(t, nil)
	now := time.Now().UTC()
	paidThrough := now.Add(500 * time.Millisecond)
	fixture.workspace["periodStart"], fixture.workspace["paidThrough"] = now.AddDate(0, -1, 0).Format(time.RFC3339Nano), paidThrough.Format(time.RFC3339Nano)
	fixture.workspace["nextRenewalAt"], fixture.workspace["billingAnchorDay"] = paidThrough.Add(-monthlyRenewalLead).Format(time.RFC3339Nano), int64(paidThrough.Day())
	fixture.workspace["autoRenew"] = false
	mustStore(t, fixture.app.tables.SaveWorkspace(context.Background(), fixture.workspace))
	store := &d4ExpiryCompletionStore{StateStore: fixture.app.tables.(StateStore), completed: make(chan struct{}, 1)}
	app, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app.startMonthlyBillingWorker(ctx, fixture.service, time.Hour)
	select {
	case <-store.completed:
		cancel()
	case <-time.After(3 * time.Second):
		t.Fatal("observed expiry waited for the hourly polling interval")
	}
	workspace, _ := app.getWorkspace("workspace-monthly")
	if time.Now().Before(paidThrough) || workspace["state"] != "suspended" || len(fixture.fabric.runtimePowerCalls) != 1 || fixture.fabric.runtimePowerCalls[0].DesiredState != "suspended" || len(fixture.sub2API.charges) != 0 {
		t.Fatalf("deadline stop failed: workspace=%#v power=%#v", workspace, fixture.fabric.runtimePowerCalls)
	}
}

func TestD4UnexpiredPostgresConcurrentRenewalKeepsWorkspaceRunning(t *testing.T) {
	fixture := d4RenewalPostgresFixture(t, nil)
	wallet := newD4RenewalWalletHTTP(t, 100_000_000)
	fixture.service = controlplane.NewService(fixture.ledger, fixture.fabric, wallet.client)
	second, err := newControlPlaneAppWithStore(fixture.app.tables)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, app := range []*controlPlaneServer{fixture.app, second} {
		wg.Add(1)
		go func(app *controlPlaneServer) {
			defer wg.Done()
			errs <- app.runMonthlyBillingOnce(context.Background(), fixture.service, fixture.paidThrough.Add(-monthlyRenewalLead))
		}(app)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	operation := d1RenewalOperation(t, fixture)
	workspace, _ := fixture.app.getWorkspace(operation.WorkspaceID)
	if operation.Status != "active" || workspace["state"] != "running" || len(wallet.writes) != 1 || len(wallet.history) != 1 || wallet.balance != 47_420_000 || len(fixture.fabric.computeRenewKeys) != 1 || len(fixture.fabric.storageRenewKeys) != 1 || len(fixture.ledger.receipts) != 1 || len(fixture.fabric.runtimePowerCalls) != 0 {
		t.Fatalf("ordinary renewal interrupted service or settled twice: operation=%#v workspace=%#v", operation, workspace)
	}
}
