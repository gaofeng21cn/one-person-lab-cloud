package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

// The gateway accounting HTTP helper and its fixtures stay because the
// qualification and historical settlement suites still read the gateway
// accounting readback. The launch chain that produced those operations is
// retired with the retained Control Plane launch execution.

const (
	gatewayAccountingAdminEmail    = "admin-gateway@example.test"
	gatewayAccountingAdminPassword = "gateway-accounting-admin-password"
	gatewayAccountingOwnerPassword = "gateway-accounting-owner-password"
	gatewayAccountingInitialMicros = int64(100_000_000)
	gatewayAccountingChargeMicros  = int64(52_580_000)
	gatewayAccountingSub2APIUserID = int64(41)
)

type gatewayAccountingAPI struct {
	baseURL string
	client  *http.Client
	csrf    string
}

func (a *gatewayAccountingAPI) mustRequest(t *testing.T, method, path string, input any, idempotencyKey string, wantStatus int) map[string]any {
	t.Helper()
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(context.Background(), method, a.baseURL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if input != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if a.csrf != "" && method != http.MethodGet && method != http.MethodHead {
		request.Header.Set("x-opl-csrf", a.csrf)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	response, err := a.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	decoder.UseNumber()
	var payload map[string]any
	if err := decoder.Decode(&payload); err != nil {
		t.Fatalf("decode %s %s: %v", method, path, err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status=%d want=%d body=%#v", method, path, response.StatusCode, wantStatus, payload)
	}
	return payload
}

func startGatewayAccountingLedger(t *testing.T, databaseURLs ...string) (clients.LedgerClient, clients.LedgerReceiptListClient) {
	t.Helper()
	var databaseURL string
	if len(databaseURLs) > 0 {
		databaseURL = databaseURLs[0]
	} else {
		databaseURL = gatewayAccountingDatabase(t, "opl_gateway_ledger_")
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test location")
	}
	ledgerDir := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "ledger"))
	binary := filepath.Join(t.TempDir(), "opl-ledger")
	build := exec.Command("go", "build", "-o", binary, "./cmd/ledger")
	build.Dir = ledgerDir
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build real Ledger HTTP owner: %v\n%s", err, output)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	token := "gateway-accounting-ledger-token"
	capabilityKey := "gateway-accounting-ledger-capability-key-32-chars"
	command := exec.Command(binary)
	command.Env = gatewayAccountingProcessEnv(os.Environ(), map[string]string{
		"LEDGER_ADDR": address, "OPL_INTERNAL_SERVICE_TOKEN": token, "DATABASE_URL": databaseURL,
		"OPL_LEDGER_CAPABILITY_KEY": capabilityKey,
		"NODE_ENV":                  "local", "OPL_POSTGRES_TESTS": "1",
	})
	var logs bytes.Buffer
	command.Stdout, command.Stderr = &logs, &logs
	if err := command.Start(); err != nil {
		t.Fatalf("start real Ledger HTTP owner: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	})
	baseURL := "http://" + address
	deadline := time.Now().Add(10 * time.Second)
	for {
		request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+"/readyz", nil)
		response, requestErr := (&http.Client{Timeout: time.Second}).Do(request)
		if requestErr == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				break
			}
		}
		select {
		case processErr := <-done:
			t.Fatalf("Ledger exited before readiness: %v\n%s", processErr, logs.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("Ledger readiness timed out: %s", logs.String())
		}
		time.Sleep(25 * time.Millisecond)
	}
	ledger := clients.NewLedgerHTTPClientWithCapability(baseURL, token, capabilityKey, &http.Client{Timeout: 5 * time.Second})
	list, ok := ledger.(clients.LedgerReceiptListClient)
	if !ok {
		t.Fatal("typed Ledger HTTP client does not support receipt readback")
	}
	ready, ok := ledger.(clients.LedgerReadinessClient)
	if !ok || ready.Ready(context.Background()) != nil {
		t.Fatal("typed Ledger HTTP client readiness failed")
	}
	return ledger, list
}

func gatewayAccountingDatabase(t *testing.T, prefixes ...string) string {
	t.Helper()
	admin := openControlPlaneTestPostgres(t)
	prefix := "opl_gateway_accounting_"
	if len(prefixes) > 0 {
		prefix = prefixes[0]
	}
	database := prefix + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := admin.Exec(`CREATE DATABASE "` + database + `"`); err != nil {
		_ = admin.Close()
		t.Fatalf("create gateway accounting database: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, database)
		_, _ = admin.Exec(`DROP DATABASE "` + database + `"`)
		_ = admin.Close()
	})
	databaseURL := controlPlaneTestPostgresURL(t, database, "")
	if parsed, err := url.Parse(databaseURL); err == nil && parsed.Scheme != "" {
		return databaseURL
	}
	host, port, user := os.Getenv("PGHOST"), os.Getenv("PGPORT"), os.Getenv("PGUSER")
	if net.ParseIP(host) == nil || port == "" || user == "" {
		t.Fatalf("keyword PostgreSQL test DSN requires explicit PGHOST, PGPORT, and PGUSER for subprocess use")
	}
	return (&url.URL{
		Scheme: "postgresql", User: url.User(user), Host: net.JoinHostPort(host, port), Path: "/" + database,
		RawQuery: url.Values{"sslmode": {"disable"}}.Encode(),
	}).String()
}

func gatewayAccountingProcessEnv(base []string, overrides map[string]string) []string {
	result := make([]string, 0, len(base)+len(overrides))
	for _, entry := range base {
		key, _, _ := strings.Cut(entry, "=")
		if _, replaced := overrides[key]; !replaced {
			result = append(result, entry)
		}
	}
	for key, value := range overrides {
		result = append(result, key+"="+value)
	}
	return result
}

type gatewayAccountingWriteCounts struct {
	charges int
	refunds int
	keys    int
}

type gatewayAccountingSub2API struct {
	client         *clients.Sub2APIHTTPClient
	baseURL        string
	authorityToken string
	ownerEmail     string
	responseLoss   bool
	process        *exec.Cmd
	lossInjected   bool
	historyLoss    bool
	mu             sync.Mutex
}

func newGatewayAccountingSub2API(t *testing.T, ownerEmail string, responseLoss bool) *gatewayAccountingSub2API {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve integration test location")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "../../../.."))
	statePath := filepath.Join(t.TempDir(), "authority-state.json")
	password := gatewayAccountingOwnerPassword
	userToken := "gateway-accounting-user-token-32-chars"
	authorityToken := "gateway-accounting-authority-token-32-chars"
	command := exec.Command("node", "--experimental-strip-types", filepath.Join(repositoryRoot, "tools", "local-sub2api-authority-fixture.ts"))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve Sub2API authority port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	command.Env = gatewayAccountingProcessEnv(os.Environ(), map[string]string{
		"OPL_QUALIFICATION_PORT":               strconv.Itoa(port),
		"OPL_QUALIFICATION_USER_EMAIL":         ownerEmail,
		"OPL_QUALIFICATION_USER_PASSWORD":      password,
		"OPL_QUALIFICATION_USER_TOKEN":         userToken,
		"OPL_QUALIFICATION_ADMIN_EMAIL":        gatewayAccountingAdminEmail,
		"OPL_QUALIFICATION_ADMIN_PASSWORD":     gatewayAccountingAdminPassword,
		"OPL_QUALIFICATION_ADMIN_TOKEN":        "gateway-accounting-admin-token-32-chars",
		"OPL_QUALIFICATION_AUTHORITY_TOKEN":    authorityToken,
		"OPL_QUALIFICATION_INITIAL_USD_MICROS": strconv.FormatInt(gatewayAccountingInitialMicros, 10),
		"OPL_QUALIFICATION_STATE_PATH":         statePath,
	})
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("open Sub2API authority stdout: %v", err)
	}
	var logs bytes.Buffer
	command.Stderr = &logs
	if err := command.Start(); err != nil {
		t.Fatalf("start Sub2API authority: %v", err)
	}
	ready := make(chan struct {
		origin string
		err    error
	}, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var payload struct {
				Status string `json:"status"`
				Origin string `json:"origin"`
			}
			if json.Unmarshal(scanner.Bytes(), &payload) == nil && payload.Status == "READY" && payload.Origin != "" {
				ready <- struct {
					origin string
					err    error
				}{origin: payload.Origin}
				return
			}
		}
		ready <- struct {
			origin string
			err    error
		}{err: fmt.Errorf("Sub2API authority exited before READY: %s", strings.TrimSpace(logs.String()))}
	}()
	var origin string
	select {
	case result := <-ready:
		if result.err != nil {
			_ = command.Process.Kill()
			_ = command.Wait()
			t.Fatal(result.err)
		}
		origin = result.origin
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("Sub2API authority did not become ready: %s", strings.TrimSpace(logs.String()))
	}
	fixture := &gatewayAccountingSub2API{
		baseURL: origin, authorityToken: authorityToken, ownerEmail: ownerEmail, responseLoss: responseLoss, process: command,
	}
	t.Cleanup(func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	})
	baseClient := &http.Client{Timeout: 5 * time.Second}
	baseClient.Transport = &gatewayAccountingFaultTransport{base: http.DefaultTransport, fixture: fixture}
	client, err := clients.NewSub2APIHTTPClient(clients.Sub2APIConfig{
		BaseURL: origin, AdminEmail: gatewayAccountingAdminEmail, AdminPassword: gatewayAccountingAdminPassword, Timeout: 3 * time.Second,
	}, baseClient)
	if err != nil {
		t.Fatal(err)
	}
	fixture.client = client
	return fixture
}

func (f *gatewayAccountingSub2API) writeCounts(t *testing.T) gatewayAccountingWriteCounts {
	t.Helper()
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, f.baseURL+"/qualification/state", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+f.authorityToken)
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		t.Fatalf("read Sub2API authority state: %v", err)
	}
	defer response.Body.Close()
	var payload struct {
		Code json.Number `json:"code"`
		Data struct {
			WriteCounts struct {
				KeyCreates int `json:"keyCreates"`
				Debits     int `json:"debits"`
				Refunds    int `json:"refunds"`
			} `json:"writeCounts"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		t.Fatalf("decode Sub2API authority state: %v", err)
	}
	if response.StatusCode != http.StatusOK || payload.Code != json.Number("0") {
		t.Fatalf("Sub2API authority state status=%d code=%q", response.StatusCode, payload.Code)
	}
	return gatewayAccountingWriteCounts{charges: payload.Data.WriteCounts.Debits, refunds: payload.Data.WriteCounts.Refunds, keys: payload.Data.WriteCounts.KeyCreates}
}

type gatewayAccountingFaultTransport struct {
	base    http.RoundTripper
	fixture *gatewayAccountingSub2API
}

type gatewayAccountingRoundTripper func(*http.Request) (*http.Response, error)

func (roundTrip gatewayAccountingRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func (t *gatewayAccountingFaultTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(request)
	if err != nil {
		return response, err
	}
	isAdjustment := request.Method == http.MethodPost && strings.HasPrefix(request.URL.Path, "/api/v1/admin/users/") && strings.HasSuffix(request.URL.Path, "/balance")
	isHistory := request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/api/v1/admin/users/") && strings.HasSuffix(request.URL.Path, "/balance-history")
	t.fixture.mu.Lock()
	loss := false
	if t.fixture.responseLoss && isAdjustment && !t.fixture.lossInjected {
		t.fixture.lossInjected, t.fixture.historyLoss, loss = true, true, true
	} else if isHistory && t.fixture.historyLoss {
		t.fixture.historyLoss, loss = false, true
	}
	t.fixture.mu.Unlock()
	if !loss {
		return response, nil
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return nil, io.ErrUnexpectedEOF
}

type gatewayAccountingFabric struct {
	fakeFabricClient
	mu sync.Mutex
	// applicationEntryURL is the provider-published web entry this local fake
	// reports for an application runtime. It is empty unless a caller asks for
	// a full application delivery, so the resource-only accounting tests keep
	// the behavior they already assert.
	applicationEntryURL string
}

func newGatewayAccountingFabric() *gatewayAccountingFabric {
	return &gatewayAccountingFabric{}
}

func (f *gatewayAccountingFabric) EnsureWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput, idempotencyKey string) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	observation, err := f.fakeFabricClient.EnsureWorkspaceApplicationRuntime(ctx, input, idempotencyKey)
	if err == nil && f.applicationEntryURL != "" && observation.Entry == nil {
		observation.Entry = &contracts.WorkspaceApplicationEntry{URL: f.applicationEntryURL}
	}
	return observation, err
}

func (f *gatewayAccountingFabric) ReadWorkspaceApplicationRuntime(ctx context.Context, input clients.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	return f.EnsureWorkspaceApplicationRuntime(ctx, input, "readback")
}

func (f *gatewayAccountingFabric) PreflightWorkspaceLaunch(_ context.Context, input clients.WorkspaceLaunchPreflightInput) (clients.WorkspaceLaunchPreflight, error) {
	return clients.WorkspaceLaunchPreflight{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, Available: true, Reason: "none",
		LaunchOperationID: input.LaunchOperationID, RequestHash: input.RequestHash,
		ProviderProfileRef: string(fabricLocalDocker), BindingRef: "local-preflight-" + stableID(input.LaunchOperationID)[:12], SpecDigest: strings.Repeat("a", 64),
	}, nil
}

func (*gatewayAccountingFabric) MonthlyPreflight(_ context.Context, input clients.MonthlyPreflightInput) (clients.MonthlyPreflight, error) {
	return clients.MonthlyPreflight{
		ResourceType: input.ResourceType, PackageID: input.PackageID, SizeGB: input.SizeGB, Zone: input.Zone,
		Available: true, ChargeType: "PREPAID", PeriodMonths: 1, RenewFlag: "NOTIFY_AND_MANUAL_RENEW",
		ProviderPriceCNY: 12.34,
	}, nil
}

type gatewayAccountingControlPlane struct {
	server  *httptest.Server
	handler *controlPlaneHTTPHandler
	store   StateStore
	fabric  *gatewayAccountingFabric
}

// wrapHandler is optional. A caller that has to observe the owner-API surface
// the real handler served can install one middleware before the server starts;
// the Control Plane handler itself stays the one this helper builds.
func newGatewayAccountingControlPlane(t *testing.T, ledger clients.LedgerClient, sub2API *gatewayAccountingSub2API, accountID, userID string, wrapHandler ...func(http.Handler) http.Handler) *gatewayAccountingControlPlane {
	t.Helper()
	t.Setenv("NODE_ENV", "")
	t.Setenv("OPL_WORKSPACE_LAUNCH_WORKER_ENABLED", "0")
	t.Setenv(controlledBasicPilotEnabledEnv, "1")
	t.Setenv(controlledBasicPilotAccountsEnv, accountID)
	t.Setenv(controlledBasicPilotMaxInFlightEnv, "1")
	databaseURL := gatewayAccountingDatabase(t)
	store, err := newTestPostgresEntStateStore(databaseURL)
	if err != nil {
		t.Fatalf("start Control Plane PostgreSQL store: %v", err)
	}
	t.Cleanup(func() { _ = store.(*postgresEntStateStore).client.Close() })
	account, user := provisionedAccountRowsFor(accountID, userID, sub2API.ownerEmail, gatewayAccountingSub2APIUserID)
	if err := store.CreateProvisionedAccount(context.Background(), account, user); err != nil {
		t.Fatalf("seed Control Plane owner: %v", err)
	}
	fabric := newGatewayAccountingFabric()
	fabricCalls := []string{}
	fabric.fakeFabricClient.calls = &fabricCalls
	service := controlplane.NewService(ledger, fabric, sub2API.client)
	handler, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatalf("start Control Plane HTTP owner: %v", err)
	}
	typed, ok := handler.(*controlPlaneHTTPHandler)
	if !ok {
		t.Fatalf("Control Plane handler type = %T", handler)
	}
	var served http.Handler = handler
	for _, wrap := range wrapHandler {
		served = wrap(served)
	}
	server := httptest.NewTLSServer(served)
	t.Cleanup(server.Close)
	return &gatewayAccountingControlPlane{server: server, handler: typed, store: store, fabric: fabric}
}

func gatewayAccountingCredentialForTest(t *testing.T, server http.Handler, api *gatewayAccountingAPI) clients.SessionDelegatedCredential {
	t.Helper()
	handler, ok := server.(*controlPlaneHTTPHandler)
	if !ok {
		t.Fatalf("unexpected control-plane test handler %T", server)
	}
	parsed, err := url.Parse(api.baseURL)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range api.client.Jar.Cookies(parsed) {
		if value, found := handler.app.sessionCredentials.Get(sessionLookupKey(cookie.Value)); found {
			return value
		}
	}
	t.Fatal("missing delegated session credential")
	return clients.SessionDelegatedCredential{}
}

// Only the fields needed at this JSON presentation boundary are decoded here;
// financial assertions use the persisted owner's walletAdjustmentOperation.
type d1WalletHTTPResponse struct {
	OperationID string `json:"operationId"`
	Status      string `json:"status"`
	Error       string `json:"error"`
}

func (p *gatewayAccountingControlPlane) login(t *testing.T, email, password string) *gatewayAccountingAPI {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := p.server.Client()
	client.Jar, client.Timeout = jar, 10*time.Second
	api := &gatewayAccountingAPI{baseURL: p.server.URL, client: client}
	response := api.mustRequest(t, http.MethodPost, "/api/auth/login", map[string]any{"email": email, "password": password}, "", http.StatusOK)
	api.csrf = stringValue(response["csrfToken"])
	if api.csrf == "" {
		t.Fatalf("Control Plane login response = %#v", response)
	}
	return api
}

const controlledBasicPilotAccountsEnv = "OPL_CONTROLLED_BASIC_PILOT_ACCOUNT_IDS"

func workspaceLaunchResourceOnlyUnitCommand() workspaceLaunchReconcileCreate {
	command := workspaceLaunchUnitCommand()
	command.Mode = contracts.WorkspaceProvisioningResourceOnly
	command.WorkspaceImageDigest = ""
	command.WorkspaceKeyGroupID = 0
	return command
}
