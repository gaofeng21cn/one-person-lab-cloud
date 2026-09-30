package delivery_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/serve/internal/delivery"
)

// accessOrigin is the installation-declared route origin under test.
func accessOrigin() delivery.RouteOrigin {
	return delivery.RouteOrigin{Scheme: "https", WorkspaceDomain: "workspace.example", ApplicationDomain: "apps.example"}
}

// serviceTransport routes a proxied request to the test server that plays the
// in-cluster Service the provider reported, so the entry's own destination
// validation still runs against a real Service name and port.
type serviceTransport struct {
	byService map[string]*httptest.Server
}

func (s serviceTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	upstream, ok := s.byService[request.URL.Hostname()]
	if !ok {
		return nil, fmt.Errorf("no test upstream for service %q", request.URL.Hostname())
	}
	request.URL.Scheme = "http"
	request.URL.Host = upstream.Listener.Addr().String()
	return http.DefaultTransport.RoundTrip(request)
}

// activateRoute drives Serve's own fence and activation so the entry is proven
// against a route the owner really committed, not a row a test inserted.
func activateRoute(t *testing.T, service *delivery.Service, tenant, workspace, runtimeInstance string) {
	t.Helper()
	ctx := routeServeContext()
	fenced, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: workspace, OperationId: "op-route", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: exactPrecondition(fenced.GetRouteRevision()), TargetExecutionResourceId: runtimeInstance, TargetRuntimeInstanceId: runtimeInstance, TargetDeploymentId: "dep-route", ConfirmedReadinessReceiptId: "readiness://dep-route"}); err != nil {
		t.Fatal(err)
	}
}

func applicationHost(t *testing.T, workspace string) string {
	t.Helper()
	entry := contracts.WorkspaceApplicationEntry{ServiceName: "app-main", Port: 8080}
	address, err := delivery.ResolveApplicationEntry(accessOrigin(), workspace, "knowledge-app", entry)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	return parsed.Host
}

func TestServeAccessEntryServesConfirmedTarget(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	activateRoute(t, service, tenant, workspace, runtimeInstance)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Seen-Path", r.URL.Path)
		w.Header().Set("X-Seen-Forwarded-Host", r.Header.Get("X-Forwarded-Host"))
		if r.Header.Get("X-Opl-Workspace-Id") != "" {
			t.Errorf("the entry must not forward a workspace selector header")
		}
		_, _ = io.WriteString(w, "application-response")
	}))
	defer upstream.Close()

	entry, err := delivery.NewAccessEntry(db, accessOrigin())
	if err != nil {
		t.Fatal(err)
	}
	entry.Transport = serviceTransport{byService: map[string]*httptest.Server{"app-main": upstream}}
	server := httptest.NewServer(entry.Handler())
	defer server.Close()

	// The binding's own origin reaches the confirmed target and keeps the
	// application's own path, host and scheme.
	response := accessGet(t, server.URL, applicationHost(t, workspace), "/console/static/app.js")
	if response.StatusCode != http.StatusOK {
		t.Fatalf("origin request status=%d body=%s", response.StatusCode, response.Body)
	}
	if response.Body != "application-response" || response.SeenPath != "/console/static/app.js" {
		t.Fatalf("origin request body=%q path=%q", response.Body, response.SeenPath)
	}
	if response.ForwardedHost != applicationHost(t, workspace) {
		t.Fatalf("X-Forwarded-Host=%q", response.ForwardedHost)
	}

	// The retained path entry serves the same confirmed target and strips its own
	// prefix, so the application sees its own root.
	pathResponse := accessGet(t, server.URL, "workspace.example", "/w/"+workspace+"/healthz")
	if pathResponse.StatusCode != http.StatusOK || pathResponse.SeenPath != "/healthz" {
		t.Fatalf("path entry status=%d path=%q", pathResponse.StatusCode, pathResponse.SeenPath)
	}
	if response := accessGet(t, server.URL, "other.example", "/w/"+workspace+"/healthz"); response.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign retained-path host status=%d body=%s", response.StatusCode, response.Body)
	}
}

func TestServeAccessEntryStripsPlatformCredentialsAndConfinesCookies(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	activateRoute(t, service, tenant, workspace, runtimeInstance)

	var seenAuthorization, seenCSRF, seenCookie string
	var seenPathAuthorization, seenPathCSRF, seenPathCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-Host") == "workspace.example" {
			seenPathAuthorization = r.Header.Get("Authorization")
			seenPathCSRF = r.Header.Get("X-OPL-CSRF")
			seenPathCookie = r.Header.Get("Cookie")
		} else {
			seenAuthorization = r.Header.Get("Authorization")
			seenCSRF = r.Header.Get("X-OPL-CSRF")
			seenCookie = r.Header.Get("Cookie")
		}
		w.Header().Set("Set-Cookie", "app_session=next; Domain=.apps.example; Path=/")
		_, _ = io.WriteString(w, "application-response")
	}))
	defer upstream.Close()

	entry, err := delivery.NewAccessEntry(db, accessOrigin())
	if err != nil {
		t.Fatal(err)
	}
	entry.Transport = serviceTransport{byService: map[string]*httptest.Server{"app-main": upstream}}
	server := httptest.NewServer(entry.Handler())
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = applicationHost(t, workspace)
	request.Header.Set("Authorization", "Bearer application-token")
	request.Header.Set("X-OPL-CSRF", "platform-csrf")
	request.AddCookie(&http.Cookie{Name: "opl_session", Value: "platform-session"})
	request.AddCookie(&http.Cookie{Name: "opl_ws_active", Value: workspace})
	request.AddCookie(&http.Cookie{Name: "opl_ws_session_runtime", Value: "runtime-session"})
	request.AddCookie(&http.Cookie{Name: "app_session", Value: "application-session"})
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", response.StatusCode)
	}
	if seenAuthorization != "Bearer application-token" || seenCSRF != "" || !strings.Contains(seenCookie, "app_session=application-session") || strings.Contains(seenCookie, "opl_session") || strings.Contains(seenCookie, "opl_ws_") {
		t.Fatalf("forwarded credentials authorization=%q csrf=%q cookie=%q", seenAuthorization, seenCSRF, seenCookie)
	}
	if cookie := response.Header.Get("Set-Cookie"); strings.Contains(cookie, "Domain=") {
		t.Fatalf("application cookie widened its domain: %q", cookie)
	}

	pathRequest, err := http.NewRequest(http.MethodGet, server.URL+"/w/"+workspace+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	pathRequest.Host = "workspace.example"
	pathRequest.Header.Set("Authorization", "Bearer platform-token")
	pathRequest.Header.Set("X-OPL-CSRF", "platform-csrf")
	pathRequest.AddCookie(&http.Cookie{Name: "opl_session", Value: "platform-session"})
	pathRequest.AddCookie(&http.Cookie{Name: "opl_ws_active", Value: workspace})
	pathRequest.AddCookie(&http.Cookie{Name: "opl_ws_session_runtime", Value: "runtime-session"})
	pathRequest.AddCookie(&http.Cookie{Name: "app_session", Value: "application-session"})
	pathResponse, err := (&http.Client{}).Do(pathRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer pathResponse.Body.Close()
	if pathResponse.StatusCode != http.StatusOK {
		t.Fatalf("path status=%d", pathResponse.StatusCode)
	}
	if seenPathAuthorization != "" || seenPathCSRF != "" || !strings.Contains(seenPathCookie, "app_session=application-session") || strings.Contains(seenPathCookie, "opl_session") || strings.Contains(seenPathCookie, "opl_ws_") {
		t.Fatalf("path forwarded credentials authorization=%q csrf=%q cookie=%q", seenPathAuthorization, seenPathCSRF, seenPathCookie)
	}
}

func TestServeAccessEntryRefusesUnconfirmedSupersededAndUnready(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)

	entry, err := delivery.NewAccessEntry(db, accessOrigin())
	if err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "application-response")
	}))
	defer upstream.Close()
	entry.Transport = serviceTransport{byService: map[string]*httptest.Server{"app-main": upstream}}
	server := httptest.NewServer(entry.Handler())
	defer server.Close()
	host := applicationHost(t, workspace)

	// Nothing is served before the owner confirms a route.
	if response := accessGet(t, server.URL, host, "/"); response.StatusCode != http.StatusServiceUnavailable || !strings.Contains(response.Body, "workspace_application_route_unconfirmed") {
		t.Fatalf("unconfirmed status=%d body=%s", response.StatusCode, response.Body)
	}

	markRouteTargetReady(t, db, runtimeInstance)
	activateRoute(t, service, tenant, workspace, runtimeInstance)

	// An origin whose application label no longer matches the confirmed target
	// belongs to a superseded application, not to this one.
	superseded := strings.Replace(host, applicationHost(t, workspace), "ws-route-000000000000.apps.example", 1)
	if response := accessGet(t, server.URL, superseded, "/"); response.StatusCode != http.StatusGone || !strings.Contains(response.Body, "workspace_application_origin_retired") {
		t.Fatalf("superseded status=%d body=%s", response.StatusCode, response.Body)
	}

	// A target that stopped being ready is refused instead of served.
	if _, err := db.ExecContext(context.Background(), `UPDATE serve.agent_runtime_instances SET status='failed' WHERE id=$1`, runtimeInstance); err != nil {
		t.Fatal(err)
	}
	if response := accessGet(t, server.URL, host, "/"); response.StatusCode != http.StatusServiceUnavailable || !strings.Contains(response.Body, "workspace_application_target_not_ready") {
		t.Fatalf("unready status=%d body=%s", response.StatusCode, response.Body)
	}

	// An origin the installation does not publish is not answered at all.
	if response := accessGet(t, server.URL, "other.example", "/"); response.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign host status=%d", response.StatusCode)
	}
}

func TestServeAccessEntryKeepsWorkspacesSeparate(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	activateRoute(t, service, tenant, workspace, runtimeInstance)

	// A second Workspace with its own delivery, route and upstream.
	other := "ws-other"
	seedObservationDeployment(t, db, other, tenant, "dep-other", "verifying", "", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	var otherRuntime string
	if err := db.QueryRowContext(context.Background(), `SELECT runtime_instance_id FROM serve.agent_deployments WHERE id='dep-other'`).Scan(&otherRuntime); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(context.Background(), `UPDATE serve.agent_runtime_instances SET status='ready',access_url='https://ws-other.example/',readiness_evidence_ref='readiness://dep-other',observed_at=now(),access_upstream_service='app-other',access_upstream_port=8080 WHERE id=$1`, otherRuntime); err != nil {
		t.Fatal(err)
	}
	ctx := routeServeContext()
	fenced, err := service.FenceRouteEpoch(ctx, &api.FenceRouteEpochCommand{Context: routeCall(tenant), WorkspaceId: other, OperationId: "op-other", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: absentPrecondition()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ActivateRoute(ctx, &api.RouteActivateCommand{Context: routeCall(tenant), WorkspaceId: other, OperationId: "op-other", ExecutionEpoch: 1, ExpectedRouteGeneration: 0, RevisionPrecondition: exactPrecondition(fenced.GetRouteRevision()), TargetExecutionResourceId: otherRuntime, TargetRuntimeInstanceId: otherRuntime, TargetDeploymentId: "dep-other", ConfirmedReadinessReceiptId: "readiness://dep-other"}); err != nil {
		t.Fatal(err)
	}

	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "first-workspace") }))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "second-workspace") }))
	defer second.Close()

	entry, err := delivery.NewAccessEntry(db, accessOrigin())
	if err != nil {
		t.Fatal(err)
	}
	entry.Transport = serviceTransport{byService: map[string]*httptest.Server{"app-main": first, "app-other": second}}
	server := httptest.NewServer(entry.Handler())
	defer server.Close()

	otherHost := func() string {
		entryValue := contracts.WorkspaceApplicationEntry{ServiceName: "app-other", Port: 8080}
		address, err := delivery.ResolveApplicationEntry(accessOrigin(), other, "knowledge-app", entryValue)
		if err != nil {
			t.Fatal(err)
		}
		parsed, _ := url.Parse(address)
		return parsed.Host
	}()

	if response := accessGet(t, server.URL, applicationHost(t, workspace), "/"); response.Body != "first-workspace" {
		t.Fatalf("workspace entry body=%q", response.Body)
	}
	if response := accessGet(t, server.URL, otherHost, "/"); response.Body != "second-workspace" {
		t.Fatalf("other workspace entry body=%q", response.Body)
	}
}

func TestServeAccessEntryStreamsResponses(t *testing.T) {
	service, tenant, workspace, runtimeInstance, db := routeFixture(t)
	markRouteTargetReady(t, db, runtimeInstance)
	activateRoute(t, service, tenant, workspace, runtimeInstance)

	released := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: first\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		<-released
		_, _ = io.WriteString(w, "data: second\n\n")
	}))
	defer upstream.Close()
	defer close(released)

	entry, err := delivery.NewAccessEntry(db, accessOrigin())
	if err != nil {
		t.Fatal(err)
	}
	entry.Transport = serviceTransport{byService: map[string]*httptest.Server{"app-main": upstream}}
	server := httptest.NewServer(entry.Handler())
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = applicationHost(t, workspace)
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	// The first event must arrive while the upstream still holds the response
	// open: a buffering entry would deliver nothing until the stream ended.
	first := make([]byte, len("data: first\n\n"))
	if _, err := io.ReadFull(response.Body, first); err != nil {
		t.Fatalf("read first event: %v", err)
	}
	if string(first) != "data: first\n\n" {
		t.Fatalf("first event=%q", string(first))
	}
}

type accessResponse struct {
	StatusCode    int
	Body          string
	SeenPath      string
	ForwardedHost string
}

func accessGet(t *testing.T, serverURL, host, path string) accessResponse {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, serverURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = host
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return accessResponse{
		StatusCode:    response.StatusCode,
		Body:          string(raw),
		SeenPath:      response.Header.Get("X-Seen-Path"),
		ForwardedHost: response.Header.Get("X-Seen-Forwarded-Host"),
	}
}
