package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

const (
	qualificationOwnerAPIProvider = "qualification-owner-api-fake"
	// The loopback entry the local Fabric fake publishes for the application
	// Runtime. The qualification records it as evidence and never connects.
	qualificationOwnerAPIEntryURL = "http://127.0.0.1:18080/"
)

// TestQualificationOwnerAPIBusinessChain is the AC#8 proof of the highest
// public qualification seam.
//
// The seam under test is `runWorkspaceQualification` in
// tools/workspace-qualification-orchestrator.ts, the real provider-neutral
// orchestrator. This test runs it for real and gives it the real owner API:
//
//   - real Control Plane owner HTTP routes in this process (account, wallet,
//     quote, purchase, charge receipt, Workspace, Runtime) backed by isolated
//     PostgreSQL;
//   - the real Ledger HTTP owner process, on its own isolated database;
//   - the local Sub2API authority fixture at the HTTP boundary, and the
//     in-process Fabric fake, which are declared test boundaries rather than a
//     production service;
//   - a declared fake provider adapter: it supplies the three reachability
//     operations of the adapter boundary and cannot answer any business fact.
//
// Nothing here reaches a production endpoint, a provider, a cluster, or a real
// customer. The receipt this run produces is not published as qualification
// evidence; it proves that the public seam settles against the real owner
// surface with no production mutation.
func TestQualificationOwnerAPIBusinessChain(t *testing.T) {
	if controlPlaneTestPostgresBaseURL() == "" {
		t.Skip("PostgreSQL test gate is not configured: the AC#8 owner-API proof needs the isolated owner database")
	}
	nodeBinary, err := exec.LookPath("node")
	if err != nil {
		t.Fatalf("Node is required to run the real qualification orchestrator: %v", err)
	}
	t.Setenv("OPL_TENCENT_ZONE", "na-siliconvalley-1")
	ledger, ledgerList := startGatewayAccountingLedger(t)
	sub2API := newGatewayAccountingSub2API(t, "qualification-owner-api@example.test", false)
	accountID := "acct-qualification-owner-api-" + stableID(t.Name())[:12]
	requests := &qualificationOwnerAPIRequestLog{}
	process := newGatewayAccountingControlPlane(t, ledger, sub2API, accountID, "usr-qualification-owner-api", requests.wrap)
	// The whole proof stays on loopback: the real Control Plane owner HTTP
	// surface, the real Ledger HTTP owner and the local Sub2API authority
	// fixture. A fixture that pointed anywhere else would not be this proof.
	for _, target := range []string{process.server.URL, sub2API.baseURL} {
		parsed, err := url.Parse(target)
		if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1" && parsed.Hostname() != "localhost") {
			t.Fatalf("qualification integration fixture is not loopback: %q", target)
		}
	}
	// The local Fabric fake publishes the application entry a Local-Docker
	// provider would publish. It is a loopback address the qualification reads
	// as evidence and never dials; no provider is reached by this test.
	process.fabric.applicationEntryURL = qualificationOwnerAPIEntryURL
	api := process.login(t, sub2API.ownerEmail, gatewayAccountingOwnerPassword)
	session := qualificationOwnerAPISessionCookie(t, api)
	// Everything the qualification itself asks for is recorded after the
	// fixture's own login, so the two request logs are directly comparable.
	served := len(requests.snapshot())

	// The purchase and its default application installation advance only when
	// the deployment's own workers run. The platform the orchestrator talks to
	// therefore has to make progress while the orchestrator polls, exactly as
	// the deployed Control Plane does.
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	var worker struct {
		mu  sync.Mutex
		err error
	}
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			err := errors.Join(
				process.handler.app.runWorkspaceLaunchesOnce(ctx, process.handler.service),
				process.handler.app.runWorkspaceApplicationDeploymentsOnce(ctx, process.handler.service),
			)
			if err != nil {
				worker.mu.Lock()
				worker.err = err
				worker.mu.Unlock()
			}
		}
	}()

	result := qualificationOwnerAPIRun(ctx, t, nodeBinary, process.server.URL, accountID, session, api.csrf, t.TempDir())
	observed := requests.snapshot()[served:]

	receipt := result.Receipt
	if result.State != "settled" || result.Reason != "none" || receipt.State != "settled" || receipt.Reason != "none" {
		worker.mu.Lock()
		workerErr := worker.err
		worker.mu.Unlock()
		t.Fatalf("real orchestrator over the real owner API did not settle: state=%q reason=%q receipt=%s requests=%v non-2xx=%v last worker error=%v",
			result.State, result.Reason, mustJSON(receipt), result.Requests, result.Responses, workerErr)
	}
	if receipt.Kind != "opl.workspace-qualification.purchase.v1" || receipt.Provider != qualificationOwnerAPIProvider {
		t.Fatalf("receipt kind/provider = %q/%q", receipt.Kind, receipt.Provider)
	}
	for _, name := range []string{
		"account_bound", "quote_priced", "purchase_issued", "purchase_receipt_bound",
		"workspace_ready", "runtime_ready", "charge_receipted", "wallet_confirmed",
	} {
		if !receipt.Checks[name] {
			t.Fatalf("settled receipt does not carry the %s check: %s", name, mustJSON(receipt.Checks))
		}
	}
	if receipt.Mutations.Declared["purchases"] != 1 || receipt.Mutations.Declared["charges"] != 1 ||
		receipt.Mutations.Observed["purchases"] != 1 || receipt.Mutations.Observed["charges"] != 1 ||
		receipt.Mutations.Observed["refunds"] != 0 || receipt.Mutations.Observed["workspaceDeletions"] != 0 ||
		receipt.Mutations.Observed["managedKeys"] != 0 {
		t.Fatalf("bounded mutation accounting = declared %v observed %v", receipt.Mutations.Declared, receipt.Mutations.Observed)
	}
	if receipt.Operation.Resumed || receipt.Operation.OperationID == "" || receipt.Operation.WorkspaceID == "" ||
		receipt.Operation.PurchaseReceiptID == "" {
		t.Fatalf("settled purchase identity incomplete: %+v", receipt.Operation)
	}
	if receipt.Accounting.QuoteUSDMicros != strconv.FormatInt(gatewayAccountingChargeMicros, 10) ||
		receipt.Accounting.WalletBeforeUSDMicros == "" || receipt.Accounting.WalletAfterUSDMicros == "" ||
		!strings.HasPrefix(receipt.Accounting.ChargeReferenceDigest, "sha256:") {
		t.Fatalf("settled accounting block = %+v", receipt.Accounting)
	}
	if !receipt.Cleanup.Performed || !receipt.Cleanup.Restored || receipt.DeploymentHealth != "healthy" {
		t.Fatalf("cleanup/health = %+v/%q", receipt.Cleanup, receipt.DeploymentHealth)
	}
	if !strings.HasPrefix(receipt.Delivery.WorkspaceEntryDigest, "sha256:") ||
		!strings.HasPrefix(receipt.Delivery.ApplicationOperationDigest, "sha256:") ||
		receipt.Delivery.ComponentsObserved < 1 {
		t.Fatalf("delivery evidence = %+v", receipt.Delivery)
	}

	// The receipt may carry only platform-minted identities and digests. The
	// customer identifier, the session and its CSRF token must not leak.
	encodedReceipt := string(mustJSON(receipt))
	for _, forbidden := range []string{accountID, sub2API.ownerEmail, session, api.csrf} {
		if strings.Contains(encodedReceipt, forbidden) {
			t.Fatalf("receipt carries a raw customer or session identifier %q: %s", forbidden, encodedReceipt)
		}
	}

	// Every read the orchestrator says it made has to have reached the real
	// owner route, and the driver's own log has to be the server's log.
	if !reflect.DeepEqual(result.Requests, observed) {
		t.Fatalf("orchestrator request log does not match the owner routes served:\ndriver=%v\nserver=%v", result.Requests, observed)
	}
	for _, expected := range []string{
		"GET /api/auth/me",
		"GET /api/gateway/wallet",
		"POST /api/pricing/preview",
		"POST /api/workspace-launches",
		"GET /api/workspace-launches/" + receipt.Operation.OperationID,
		"GET /api/billing/receipts/" + receipt.Operation.PurchaseReceiptID,
		"GET /api/workspaces",
		"GET /api/workspaces/" + receipt.Operation.WorkspaceID + "/runtime-status",
	} {
		if !qualificationOwnerAPIRequestSeen(observed, expected) {
			t.Fatalf("owner route %q was not read by the qualification: %v", expected, observed)
		}
	}
	for _, request := range observed {
		method, path, _ := strings.Cut(request, " ")
		if method == http.MethodGet || method == http.MethodHead {
			continue
		}
		// The quote read is a POST that changes nothing; the purchase command is
		// the one mutation this flow may issue. A resume would be an allowed
		// continuation, but this healthy run must not need one.
		if path != "/api/pricing/preview" && path != "/api/workspace-launches" {
			t.Fatalf("qualification issued an unexpected owner mutation: %s", request)
		}
	}

	// Bounded counts come from the owner readbacks, not from the receipt.
	rows, err := queryRuntimeOperations(context.Background(), process.store, runtimeOperationQuery{AccountID: accountID, Action: workspaceLaunchAction})
	if err != nil || len(rows) != 1 {
		t.Fatalf("owner launch operations = %d error=%v, want exactly one", len(rows), err)
	}
	launch, err := decodeWorkspaceLaunchReconcileOperation(rows[0])
	if err != nil || launch.ID != receipt.Operation.OperationID || launch.Status != contracts.StatusSucceeded ||
		launch.stringFact("workspaceId") != receipt.Operation.WorkspaceID ||
		launch.stringFact("receiptId") != receipt.Operation.PurchaseReceiptID ||
		launch.provisioningMode() != contracts.WorkspaceProvisioningResourceOnly {
		t.Fatalf("owner launch does not match the settled receipt: %+v error=%v", launch, err)
	}
	workspace, found, err := process.store.GetWorkspace(context.Background(), receipt.Operation.WorkspaceID)
	if err != nil || !found || stringValue(workspace["state"]) != "running" ||
		stringValue(workspace["currentApplicationDeploymentId"]) == "" {
		t.Fatalf("owner Workspace = %#v found=%t error=%v", workspace, found, err)
	}

	counts := sub2API.writeCounts(t)
	if counts.charges != 1 || counts.refunds != 0 || counts.keys != 1 {
		t.Fatalf("Sub2API authority writes after one settled qualification = %+v, want one debit, no refund and one default-application Key", counts)
	}
	balance, err := sub2API.client.Balance(context.Background(), gatewayAccountingSub2APIUserID)
	if err != nil || balance.USDMicros != gatewayAccountingInitialMicros-gatewayAccountingChargeMicros {
		t.Fatalf("Sub2API authority balance = %#v error=%v", balance, err)
	}
	if receipt.Accounting.WalletAfterUSDMicros != strconv.FormatInt(balance.USDMicros, 10) {
		t.Fatalf("receipt wallet-after %q does not match the owner balance %d", receipt.Accounting.WalletAfterUSDMicros, balance.USDMicros)
	}

	page, err := ledgerList.ListReceipts(context.Background(), clients.ReceiptQuery{AccountID: accountID, TypePrefix: "billing.workspace_purchased.", Limit: 50})
	if err != nil || len(page.Receipts) != 1 {
		t.Fatalf("owner purchase receipts = %#v error=%v, want exactly one", page, err)
	}
	purchase := page.Receipts[0]
	if purchase.ReceiptID != receipt.Operation.PurchaseReceiptID || purchase.RequestID != receipt.Operation.OperationID ||
		purchase.WorkspaceID != receipt.Operation.WorkspaceID || purchase.Type != "billing.workspace_purchased.v1" ||
		purchase.Status != "completed" ||
		fmt.Sprint(purchase.Cost["totalUsdMicros"]) != receipt.Accounting.QuoteUSDMicros {
		t.Fatalf("owner purchase receipt does not bind the settled operation: receiptID=%q requestID=%q workspaceID=%q type=%q status=%q cost=%v; want receiptID=%q requestID=%q workspaceID=%q quote=%q",
			purchase.ReceiptID, purchase.RequestID, purchase.WorkspaceID, purchase.Type, purchase.Status, purchase.Cost,
			receipt.Operation.PurchaseReceiptID, receipt.Operation.OperationID, receipt.Operation.WorkspaceID, receipt.Accounting.QuoteUSDMicros)
	}
	refunds, err := ledgerList.ListReceipts(context.Background(), clients.ReceiptQuery{AccountID: accountID, TypePrefix: "billing.workspace_refunded.", Limit: 50})
	if err != nil || len(refunds.Receipts) != 0 {
		t.Fatalf("settled qualification wrote a refund receipt: %#v error=%v", refunds, err)
	}

	wallet := gatewayAccountingEnvelopeData(t, api.mustRequest(t, http.MethodGet, "/api/gateway/wallet", nil, "", http.StatusOK))
	if stringValue(wallet["usdMicros"]) != receipt.Accounting.WalletAfterUSDMicros {
		t.Fatalf("owner wallet readback = %#v, receipt wallet-after = %q", wallet, receipt.Accounting.WalletAfterUSDMicros)
	}
}

// qualificationOwnerAPIRequestLog records the exact owner surface the real
// handler served, so the test can prove which routes the orchestrator used.
type qualificationOwnerAPIRequestLog struct {
	mu       sync.Mutex
	requests []string
}

func (log *qualificationOwnerAPIRequestLog) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.mu.Lock()
		log.requests = append(log.requests, r.Method+" "+r.URL.Path+qualificationOwnerAPIQuerySuffix(r.URL))
		log.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

func (log *qualificationOwnerAPIRequestLog) snapshot() []string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return append([]string(nil), log.requests...)
}

func qualificationOwnerAPIQuerySuffix(resource *url.URL) string {
	if resource.RawQuery == "" {
		return ""
	}
	return "?" + resource.RawQuery
}

func qualificationOwnerAPIRequestSeen(requests []string, expected string) bool {
	for _, request := range requests {
		if request == expected || strings.HasPrefix(request, expected+"?") {
			return true
		}
	}
	return false
}

func qualificationOwnerAPISessionCookie(t *testing.T, api *gatewayAccountingAPI) string {
	t.Helper()
	baseURL, err := url.Parse(api.baseURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range api.client.Jar.Cookies(baseURL) {
		if cookie.Name == sessionCookieName {
			return cookie.Name + "=" + cookie.Value
		}
	}
	t.Fatalf("Control Plane login did not set the %s session cookie", sessionCookieName)
	return ""
}

type qualificationOwnerAPIReceipt struct {
	Kind      string          `json:"kind"`
	Provider  string          `json:"provider"`
	State     string          `json:"state"`
	Reason    string          `json:"reason"`
	Checks    map[string]bool `json:"checks"`
	Mutations struct {
		Declared map[string]int `json:"declared"`
		Observed map[string]int `json:"observed"`
	} `json:"mutations"`
	Operation struct {
		OperationID       string `json:"operationId"`
		WorkspaceID       string `json:"workspaceId"`
		PurchaseReceiptID string `json:"purchaseReceiptId"`
		Resumed           bool   `json:"resumed"`
	} `json:"operation"`
	Accounting struct {
		QuoteUSDMicros        string `json:"quoteUsdMicros"`
		WalletBeforeUSDMicros string `json:"walletBeforeUsdMicros"`
		WalletAfterUSDMicros  string `json:"walletAfterUsdMicros"`
		ChargeReferenceDigest string `json:"chargeReferenceDigest"`
	} `json:"accounting"`
	Delivery struct {
		WorkspaceEntryDigest       string `json:"workspaceEntryDigest"`
		ApplicationOperationDigest string `json:"applicationOperationDigest"`
		ComponentsObserved         int    `json:"componentsObserved"`
	} `json:"delivery"`
	Cleanup struct {
		Performed bool `json:"performed"`
		Restored  bool `json:"restored"`
	} `json:"cleanup"`
	DeploymentHealth string `json:"deploymentHealth"`
}

type qualificationOwnerAPIResult struct {
	State     string                       `json:"state"`
	Reason    string                       `json:"reason"`
	Receipt   qualificationOwnerAPIReceipt `json:"receipt"`
	Requests  []string                     `json:"requests"`
	Responses []map[string]any             `json:"responses"`
}

func qualificationOwnerAPIRun(parent context.Context, t *testing.T, nodeBinary, origin, accountID, session, csrf string, workDirectory string) qualificationOwnerAPIResult {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve qualification owner API test location")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../.."))
	driver := filepath.Join(repositoryRoot, "services", "control-plane", "internal", "server", "testdata", "qualification-owner-api-driver.ts")
	ctx, cancel := context.WithTimeout(parent, 2*time.Minute)
	defer cancel()
	command := exec.CommandContext(ctx, nodeBinary, "--experimental-strip-types", driver)
	command.Dir = workDirectory
	command.Env = gatewayAccountingProcessEnv(os.Environ(), map[string]string{
		"OPL_QUALIFICATION_OWNER_API_ORIGIN":     origin,
		"OPL_QUALIFICATION_OWNER_API_ACCOUNT_ID": accountID,
		"OPL_QUALIFICATION_OWNER_API_COOKIE":     session,
		"OPL_QUALIFICATION_OWNER_API_CSRF":       csrf,
		"OPL_QUALIFICATION_OWNER_API_PROVIDER":   qualificationOwnerAPIProvider,
		"OPL_QUALIFICATION_OWNER_API_POLL_MS":    "1",
	})
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	started := time.Now()
	if err := command.Run(); err != nil {
		t.Fatalf("real qualification orchestrator exited after %s: %v\nstdout:\n%s\nstderr:\n%s",
			time.Since(started), err, stdout.String(), stderr.String())
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var result qualificationOwnerAPIResult
	if len(lines) == 0 || json.Unmarshal([]byte(lines[len(lines)-1]), &result) != nil {
		t.Fatalf("real qualification orchestrator returned no decodable result: %q stderr=%q", stdout.String(), stderr.String())
	}
	if result.State == "" || result.Receipt.Kind == "" {
		t.Fatalf("real qualification orchestrator result is incomplete: %q stderr=%q", stdout.String(), stderr.String())
	}
	return result
}
