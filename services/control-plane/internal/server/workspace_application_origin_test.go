package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	contracts "opl-cloud/packages/contracts/go"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A binding's origin is a pure function of its identity: deriving it twice gives
// the same name, and the name can be turned back into the binding for routing.
// No allocation table is involved, so no module can disagree about an address.
func TestWorkspaceApplicationOriginIsDerivedAndReversible(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")
	host, ok := workspaceApplicationOriginHost("ws-alpha", "knowledge-app")
	if !ok {
		t.Fatal("origin host was not derived")
	}
	again, ok := workspaceApplicationOriginHost("ws-alpha", "knowledge-app")
	if !ok || again != host {
		t.Fatalf("derivation is not stable: %q then %q", host, again)
	}
	if !strings.HasSuffix(host, "."+workspaceApplicationOriginDomain()) {
		t.Fatalf("host %q is not under the application origin domain", host)
	}
	if url, ok := workspaceApplicationOriginURL("ws-alpha", "knowledge-app"); !ok || url != "https://"+host+"/" {
		t.Fatalf("origin url = %q ok=%v", url, ok)
	}

	workspaceID, label, ok := parseWorkspaceApplicationOriginHost(host)
	if !ok || workspaceID != "ws-alpha" {
		t.Fatalf("parsed workspace=%q ok=%v", workspaceID, ok)
	}
	expected, ok := workspaceApplicationOriginLabel("ws-alpha", "knowledge-app")
	if !ok || label != expected {
		t.Fatalf("parsed label=%q want %q", label, expected)
	}
	// The name is usable as a real request host, including an explicit port.
	if parsed, _, ok := parseWorkspaceApplicationOriginHost(host + ":443"); !ok || parsed != "ws-alpha" {
		t.Fatalf("host with port did not resolve: %q ok=%v", parsed, ok)
	}
}

// The origin must change when the Workspace's application changes, and stay the
// same for a compatible update of the same application. That is what keeps a
// replaced application's browser storage and service worker away from its
// successor while letting an ordinary update keep the visitor's session.
func TestWorkspaceApplicationOriginFollowsApplicationIdentity(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")
	knowledge, ok := workspaceApplicationOriginURL("ws-alpha", "knowledge-app")
	if !ok {
		t.Fatal("knowledge origin was not derived")
	}
	other, ok := workspaceApplicationOriginURL("ws-alpha", "database-app")
	if !ok || other == knowledge {
		t.Fatalf("a different application reused the origin: %q vs %q", knowledge, other)
	}
	neighbour, ok := workspaceApplicationOriginURL("ws-beta", "knowledge-app")
	if !ok || neighbour == knowledge {
		t.Fatalf("a different Workspace reused the origin: %q vs %q", neighbour, knowledge)
	}
}

func TestParseWorkspaceApplicationOriginRejectsForeignHosts(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")
	if _, ok := workspaceApplicationOriginHost("ws-alpha", "knowledge-app"); !ok {
		t.Fatal("fixture origin was not derived")
	}
	label, _ := workspaceApplicationOriginLabel("ws-alpha", "knowledge-app")
	cases := map[string]string{
		"console host":            "console.example",
		"bare workspace domain":   workspaceDomain(),
		"foreign domain":          "ws-alpha-" + label + ".example.invalid",
		"missing application":     "ws-alpha." + workspaceDomain(),
		"non hexadecimal label":   "ws-alpha-zzzzzzzzzzzz." + workspaceDomain(),
		"wrong label length":      "ws-alpha-abcdef." + workspaceDomain(),
		"nested label":            "a.ws-alpha-" + label + "." + workspaceDomain(),
		"empty":                   "",
		"only the domain suffix":  "." + workspaceDomain(),
		"trailing separator only": "ws-alpha-." + workspaceDomain(),
	}
	for name, host := range cases {
		if _, _, ok := parseWorkspaceApplicationOriginHost(host); ok {
			t.Fatalf("%s was accepted as a binding origin: %q", name, host)
		}
	}
}

// An installation that publishes no application-origin domain, or an identity
// that cannot be a DNS label, gets no origin at all. Such a binding keeps the
// retained path-based entry rather than an address that cannot resolve.
func TestWorkspaceApplicationOriginRequiresResolvableIdentity(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "")
	if host, ok := workspaceApplicationOriginHost("ws-alpha", "knowledge-app"); ok || host != "" {
		t.Fatalf("origin derived without an application origin domain: %q", host)
	}
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")
	for _, workspaceID := range []string{"", "ws_alpha", "WS-ALPHA", "ws-alpha-", "-ws-alpha", strings.Repeat("a", 64)} {
		if host, ok := workspaceApplicationOriginHost(workspaceID, "knowledge-app"); ok {
			t.Fatalf("unsafe workspace identity %q produced host %q", workspaceID, host)
		}
	}
	// An application identity that is not a DNS-safe identifier has no origin;
	// it never becomes an escaped hostname.
	if host, ok := workspaceApplicationOriginHost("ws-alpha", "knowledge_app"); ok {
		t.Fatalf("unsafe application identity produced host %q", host)
	}
	if host, ok := workspaceApplicationOriginHost("ws-alpha", ""); ok {
		t.Fatalf("missing application identity produced host %q", host)
	}
}

// On a binding's own origin, only platform credentials are removed. The
// application keeps its own Authorization header and cookies, and the Console's
// session and the shared-host routing cookie never reach it.
func TestWorkspaceOriginProxyStripsOnlyPlatformCredentials(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "http://ws-alpha-0123456789ab.workspace.example/", nil)
	req.Header.Set("Authorization", "Bearer application-token")
	req.Header.Set("X-OPL-CSRF", "csrf")
	req.Header.Set("X-OPL-CSRF-Token", "csrf-token")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "platform-session"})
	req.AddCookie(&http.Cookie{Name: "opl_ws_active", Value: "ws-alpha"})
	req.AddCookie(&http.Cookie{Name: workspaceGatewayRuntimeSessionCookieName("ws-alpha"), Value: "runtime-session"})
	req.AddCookie(&http.Cookie{Name: "app_session", Value: "application-session"})

	stripWorkspaceOriginPlatformCredentials(req, "ws-alpha")

	if got := req.Header.Get("Authorization"); got != "Bearer application-token" {
		t.Fatalf("application Authorization was removed: %q", got)
	}
	for _, header := range platformCredentialHeaders {
		if got := req.Header.Get(header); got != "" {
			t.Fatalf("platform header %s reached the application: %q", header, got)
		}
	}
	names := map[string]string{}
	for _, cookie := range req.Cookies() {
		names[cookie.Name] = cookie.Value
	}
	if len(names) != 1 || names["app_session"] != "application-session" {
		t.Fatalf("forwarded cookies = %#v", names)
	}
}

// An application may set its own cookies, but it cannot widen them onto a
// sibling binding or onto the Console's origin, either of which would let one
// application read or overwrite another origin's browser state.
func TestWorkspaceOriginConfinesApplicationResponseCookies(t *testing.T) {
	response := &http.Response{Header: http.Header{"Set-Cookie": []string{
		"app_session=abc; Path=/; Domain=.workspace.example; HttpOnly",
		"preference=dark; Path=/",
	}}}
	if err := confineOriginResponseCookies(response); err != nil {
		t.Fatal(err)
	}
	cookies := response.Cookies()
	if len(cookies) != 2 {
		t.Fatalf("confined cookies = %#v", cookies)
	}
	for _, cookie := range cookies {
		if cookie.Domain != "" {
			t.Fatalf("cookie %s kept a widening domain: %q", cookie.Name, cookie.Domain)
		}
	}
	if cookies[0].Name != "app_session" || cookies[0].Value != "abc" || cookies[1].Name != "preference" {
		t.Fatalf("cookie semantics changed: %#v", cookies)
	}
}

// A binding origin serves only its application. This server's own management
// routes must not answer there, or an application host would silently expose the
// Console's API instead of the application.
func TestWorkspaceApplicationOriginOwnsItsHost(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "knowledge-app", true)

	host, ok := workspaceApplicationOriginHost("ws-alpha", "knowledge-app")
	if !ok {
		t.Fatal("origin host was not derived")
	}
	req := httptest.NewRequest(http.MethodGet, "/api/healthz", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), `"status":"ok"`) {
		t.Fatalf("the management health route answered on a binding origin: %d %s", rec.Code, rec.Body.String())
	}
}

// An origin that no longer matches the Workspace's current application belonged
// to a superseded one. Serving the replacement there would let the previous
// application's browser state act on the new deployment.
func TestWorkspaceApplicationOriginRetiresASupersededApplication(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "knowledge-app", true)

	retiredHost, ok := workspaceApplicationOriginHost("ws-alpha", "previous-app")
	if !ok {
		t.Fatal("retired origin host was not derived")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = retiredHost
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusGone || !strings.Contains(rec.Body.String(), "workspace_application_origin_retired") {
		t.Fatalf("superseded origin status=%d body=%s", rec.Code, rec.Body.String())
	}

	// A Workspace identity in the name that this installation does not own is
	// not a binding origin at all.
	unknownHost, ok := workspaceApplicationOriginHost("ws-missing", "knowledge-app")
	if !ok {
		t.Fatal("unknown origin host was not derived")
	}
	unknown := httptest.NewRequest(http.MethodGet, "/", nil)
	unknown.Host = unknownHost
	unknownRec := httptest.NewRecorder()
	fixture.server.ServeHTTP(unknownRec, unknown)
	if unknownRec.Code != http.StatusNotFound {
		t.Fatalf("unknown Workspace origin status=%d body=%s", unknownRec.Code, unknownRec.Body.String())
	}
}

// The external scheme is an installation fact, stated once. The in-cluster hop
// is plain HTTP behind the instance's TLS terminator, so the request cannot
// report it; the Console's public URL does, and every published Workspace
// address and every forwarded protocol header must agree with that one fact.
func TestWorkspaceExternalOriginIsOneInstallationFact(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")

	t.Setenv("OPL_PUBLIC_URL", "https://console.example")
	originURL, ok := workspaceApplicationOriginURL("ws-alpha", "knowledge-app")
	if !ok || !strings.HasPrefix(originURL, "https://") {
		t.Fatalf("origin url = %q ok=%v", originURL, ok)
	}
	if entry := workspaceGatewayEntryURL("ws-alpha"); !strings.HasPrefix(entry, "https://") {
		t.Fatalf("retained entry url = %q", entry)
	}
	if scheme := workspaceExternalScheme(); scheme != "https" {
		t.Fatalf("external scheme = %q", scheme)
	}

	// A different installation state produces a different, consistent answer.
	t.Setenv("OPL_PUBLIC_URL", "http://localhost:8787")
	originURL, ok = workspaceApplicationOriginURL("ws-alpha", "knowledge-app")
	if !ok || !strings.HasPrefix(originURL, "http://") {
		t.Fatalf("origin url = %q ok=%v", originURL, ok)
	}
	if scheme := workspaceExternalScheme(); scheme != "http" {
		t.Fatalf("external scheme = %q", scheme)
	}

	// Without a declared public origin there is no external scheme, so no
	// address is published at all rather than an unresolvable one.
	t.Setenv("OPL_PUBLIC_URL", "")
	if scheme := workspaceExternalScheme(); scheme != "" {
		t.Fatalf("external scheme = %q without a public URL", scheme)
	}
	if url, ok := workspaceApplicationOriginURL("ws-alpha", "knowledge-app"); ok || url != "" {
		t.Fatalf("origin url was published without an external origin: %q", url)
	}
	if entry := workspaceGatewayEntryURL("ws-alpha"); entry != "" {
		t.Fatalf("retained entry url was published without an external origin: %q", entry)
	}
}

// A binding origin must serve the application it publishes. The entitlement
// projection alone cannot decide that: it deletes the entry and reports not
// openable until the application's own live readback says otherwise. This test
// therefore requires more than the absence of a refusal: the gateway must
// forward the request to the destination the binding states and return the
// application's real response.
func TestWorkspaceApplicationOriginServesItsReadyApplication(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	intent := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "knowledge-app", true)
	revision := contracts.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationID: "knowledge-app", Version: "1.0.0", Platform: "linux/amd64",
		Image: "registry.example/knowledge-app@sha256:" + strings.Repeat("a", 64), ExposurePolicy: "application",
		EntryPort: "http", Ports: []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}}}
	components := contracts.WorkspaceApplicationRuntimeComponents(revision)
	for index := range components {
		components[index].State = "ready"
	}
	// What the provider reports for a ready publishing revision: the components are
	// ready and the destination the installation gateway serves is stated.
	fixture.fabric.applicationRuntimeObservation = contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: "ws-alpha", RuntimeID: contracts.WorkspaceApplicationRuntimeID(intent.OperationID + ":runtime"),
		Status: "ready", Entry: applicationGatewayEntry(revision), Components: components,
	}

	// The real application behind the destination: it answers with its own body
	// and records what the gateway actually forwarded to it.
	type forwardedRequest struct {
		host, forwardedHost, forwardedProto, path, rawQuery string
		platformCSRF, platformCookie                        string
	}
	forwarded := make([]forwardedRequest, 0, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded = append(forwarded, forwardedRequest{
			host: r.Host, forwardedHost: r.Header.Get("X-Forwarded-Host"), forwardedProto: r.Header.Get("X-Forwarded-Proto"),
			path: r.URL.Path, rawQuery: r.URL.RawQuery,
			platformCSRF: r.Header.Get("X-OPL-CSRF"), platformCookie: r.Header.Get("Cookie"),
		})
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Add("Set-Cookie", "app_session=abc; Domain=example.com; Path=/")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "knowledge-app-live-response")
	}))
	defer upstream.Close()
	upstreamURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	app.workspaceProxyTransport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		r.URL.Scheme, r.URL.Host = upstreamURL.Scheme, upstreamURL.Host
		return http.DefaultTransport.RoundTrip(r)
	})

	host, ok := workspaceApplicationOriginHost("ws-alpha", "knowledge-app")
	if !ok {
		t.Fatal("origin host was not derived")
	}
	req := httptest.NewRequest(http.MethodGet, "/chat?session=alpha", nil)
	req.Host = host
	// Platform credentials travel on the shared host and must never reach the
	// application, no matter which admission path let the request in.
	req.Header.Set("X-OPL-CSRF", "platform-csrf-token")
	req.Header.Set("X-OPL-CSRF-Token", "platform-csrf-token")
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "platform-session"})
	req.AddCookie(&http.Cookie{Name: "opl_ws_active", Value: "platform-active"})
	req.AddCookie(&http.Cookie{Name: workspaceGatewayRuntimeSessionCookieName("ws-alpha"), Value: "runtime-session"})
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || rec.Body.String() != "knowledge-app-live-response" {
		t.Fatalf("the origin did not return the application's own response: %d %q", rec.Code, rec.Body.String())
	}
	if len(forwarded) != 1 {
		t.Fatalf("the gateway forwarded %d requests, want exactly one", len(forwarded))
	}
	got := forwarded[0]
	if got.host != "app-entry-main:8080" {
		t.Fatalf("the request was not addressed to the binding's stated destination: Host %q", got.host)
	}
	if got.forwardedHost != host || got.forwardedProto != workspaceExternalScheme() {
		t.Fatalf("external identity headers = %q/%q, want %q/%q", got.forwardedHost, got.forwardedProto, host, workspaceExternalScheme())
	}
	if got.path != "/chat" || got.rawQuery != "session=alpha" {
		t.Fatalf("the forwarded target = %q?%q, want the requested path and query", got.path, got.rawQuery)
	}
	if got.platformCSRF != "" || strings.Contains(got.platformCookie, "opl_session") ||
		strings.Contains(got.platformCookie, "opl_ws_active") || strings.Contains(got.platformCookie, "opl_ws_session_") {
		t.Fatalf("platform credentials reached the application: csrf=%q cookie=%q", got.platformCSRF, got.platformCookie)
	}
	// The application's own cookie passes, confined to the origin host.
	cookie := rec.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "app_session=abc") || strings.Contains(cookie, "Domain=") {
		t.Fatalf("the application cookie was not confined to the origin: %q", cookie)
	}
	// Admission is driven by the application's live readback, exactly once.
	if len(fixture.fabric.applicationRuntimeInputs) != 1 {
		t.Fatalf("live application reads = %d, want exactly one", len(fixture.fabric.applicationRuntimeInputs))
	}
}

// An application Fabric has not reported ready is refused with the same
// not-ready conflict: the origin never forwards on a stale or absent readiness.
func TestWorkspaceApplicationOriginRefusesAnUnreadyApplication(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	intent := seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "knowledge-app", true)
	revision := contracts.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationID: "knowledge-app", Version: "1.0.0", Platform: "linux/amd64",
		Image: "registry.example/knowledge-app@sha256:" + strings.Repeat("a", 64), ExposurePolicy: "application",
		EntryPort: "http", Ports: []contracts.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: "TCP"}}}
	components := contracts.WorkspaceApplicationRuntimeComponents(revision)
	for index := range components {
		components[index].State = "pending"
	}
	fixture.fabric.applicationRuntimeObservation = contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: "ws-alpha", RuntimeID: contracts.WorkspaceApplicationRuntimeID(intent.OperationID + ":runtime"),
		Status: "pending", Entry: applicationGatewayEntry(revision), Components: components,
	}

	host, ok := workspaceApplicationOriginHost("ws-alpha", "knowledge-app")
	if !ok {
		t.Fatal("origin host was not derived")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "workspace_runtime_not_ready") {
		t.Fatalf("an unready application was not refused as not ready: %d %s", rec.Code, rec.Body.String())
	}
}

// When the live readback itself fails, the origin refuses rather than admits on
// the entitlement projection alone: no read, no entry.
func TestWorkspaceApplicationOriginRefusesWhenItsReadbackFails(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "knowledge-app", true)
	fixture.fabric.applicationRuntimeErr = errors.New("injected_runtime_read_unavailable")

	host, ok := workspaceApplicationOriginHost("ws-alpha", "knowledge-app")
	if !ok {
		t.Fatal("origin host was not derived")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "workspace_runtime_not_ready") {
		t.Fatalf("a failed readback was not refused as not ready: %d %s", rec.Code, rec.Body.String())
	}
}

// A suspended Workspace keeps its lifecycle refusal at the origin too: the
// live-read admission path never bypasses it, and no application read is even
// attempted.
func TestWorkspaceApplicationOriginKeepsTheSuspendedRefusal(t *testing.T) {
	t.Setenv("OPL_WORKSPACE_DOMAIN", "workspace.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DOMAIN", "application.example")
	t.Setenv("OPL_WORKSPACE_APPLICATION_DEPLOYMENT_WORKER_ENABLED", "0")
	fixture, _, _, _ := newResourceOnlyWorkspaceLifecycleFixture(t)
	app := fixture.server.(*controlPlaneHTTPHandler).app
	seedCurrentApplicationForLifecycle(t, app, "ws-alpha", "knowledge-app", true)
	workspace, found, err := app.tables.GetWorkspace(context.Background(), "ws-alpha")
	if err != nil || !found {
		t.Fatal("missing workspace", err)
	}
	workspace["state"] = "suspended"
	if err := app.tables.SaveWorkspace(context.Background(), workspace); err != nil {
		t.Fatal(err)
	}

	host, ok := workspaceApplicationOriginHost("ws-alpha", "knowledge-app")
	if !ok {
		t.Fatal("origin host was not derived")
	}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = host
	rec := httptest.NewRecorder()
	fixture.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "workspace_suspended") {
		t.Fatalf("a suspended workspace lost its refusal: %d %s", rec.Code, rec.Body.String())
	}
	if len(fixture.fabric.applicationRuntimeInputs) != 0 {
		t.Fatalf("a suspended workspace triggered %d application reads, want none", len(fixture.fabric.applicationRuntimeInputs))
	}
}
