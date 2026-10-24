package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
)

const qualificationOwnerAPIProvider = "qualification-owner-api-fake"

// TestQualificationRefusesControlPlaneOnlyOwnerAPI exercises the real native
// orchestrator against retained CP HTTP and Ledger processes over isolated
// PostgreSQL. CP identity cannot stand in for a CloudIdentity live session.
// No fake v2 facts, retained purchase or successful qualification is permitted.
func TestQualificationRefusesControlPlaneOnlyOwnerAPI(t *testing.T) {
	if controlPlaneTestPostgresBaseURL() == "" {
		t.Skip("PostgreSQL test gate is not configured: the owner-API boundary needs the isolated owner database")
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
	for _, target := range []string{process.server.URL, sub2API.baseURL} {
		parsed, err := url.Parse(target)
		if err != nil || (parsed.Hostname() != "127.0.0.1" && parsed.Hostname() != "::1" && parsed.Hostname() != "localhost") {
			t.Fatalf("qualification integration fixture is not loopback: %q", target)
		}
	}
	api := process.login(t, sub2API.ownerEmail, gatewayAccountingOwnerPassword)
	session := qualificationOwnerAPISessionCookie(t, api)
	served := len(requests.snapshot())
	result := qualificationOwnerAPIRun(context.Background(), t, nodeBinary, process.server.URL, accountID, session, api.csrf, t.TempDir())
	observed := requests.snapshot()[served:]
	receipt := result.Receipt
	if result.State != "failed" || result.Reason != "account_binding_mismatch" || receipt.State != result.State || receipt.Reason != result.Reason {
		t.Fatalf("CP-only origin did not refuse native qualification: state=%q reason=%q receipt=%s responses=%v", result.State, result.Reason, mustJSON(receipt), result.Responses)
	}
	if receipt.Kind != "opl.workspace-qualification.purchase.v1" || receipt.Provider != qualificationOwnerAPIProvider {
		t.Fatalf("receipt kind/provider = %q/%q", receipt.Kind, receipt.Provider)
	}
	for name, count := range receipt.Mutations.Observed {
		if count != 0 {
			t.Fatalf("refused qualification observed %s=%d", name, count)
		}
	}
	for name, passed := range receipt.Checks {
		if passed {
			t.Fatalf("CP-only origin satisfied native check %q", name)
		}
	}
	if receipt.Operation.OperationID != "" || receipt.Operation.WorkspaceID != "" || receipt.Operation.PurchaseReceiptID != "" {
		t.Fatalf("refused qualification invented owner identities: %+v", receipt.Operation)
	}
	if !receipt.Cleanup.Performed || !receipt.Cleanup.Restored {
		t.Fatalf("refused qualification did not close its adapter scope: %+v", receipt.Cleanup)
	}
	if !reflect.DeepEqual(result.Requests, observed) {
		t.Fatalf("driver/server requests differ: driver=%v server=%v", result.Requests, observed)
	}
	if !reflect.DeepEqual(observed, []string{"GET /api/auth/me", "GET /api/v2/auth/session"}) {
		t.Fatalf("native identity refusal did not stop before business reads/writes: %v", observed)
	}
	for _, forbidden := range []string{accountID, sub2API.ownerEmail, session, api.csrf} {
		if strings.Contains(string(mustJSON(receipt)), forbidden) {
			t.Fatalf("receipt carries a raw customer or session identifier")
		}
	}
	rows, err := queryRuntimeOperations(context.Background(), process.store, runtimeOperationQuery{AccountID: accountID, Action: workspaceLaunchAction})
	if err != nil || len(rows) != 0 {
		t.Fatalf("refused qualification created retained operations: rows=%d err=%v", len(rows), err)
	}
	workspaces, err := process.store.ListWorkspaces(context.Background(), accountID)
	if err != nil || len(workspaces) != 0 {
		t.Fatalf("refused qualification created Workspaces: rows=%d err=%v", len(workspaces), err)
	}
	counts := sub2API.writeCounts(t)
	if counts.charges != 0 || counts.refunds != 0 || counts.keys != 0 {
		t.Fatalf("refused qualification mutated Sub2API: %+v", counts)
	}
	page, err := ledgerList.ListReceipts(context.Background(), clients.ReceiptQuery{AccountID: accountID, TypePrefix: "billing.workspace_purchased.", Limit: 50})
	if err != nil || len(page.Receipts) != 0 {
		t.Fatalf("refused qualification wrote purchase evidence: %#v err=%v", page, err)
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
