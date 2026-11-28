package delivery

// The Console facts are what let an application guide a customer back to the
// Console when the model is not configured or the session expired. The
// application can only build those links when the Runtime publishes an
// absolute console URL and the Workspace it belongs to, so Serve states both
// on the request it forwards.
//
// These tests drive the entry's own proxy over one confirmed route. The
// confirmed route is the only input: the Workspace is the route's own, never
// the caller's, and the Console address is the installation's declared one or
// nothing at all.

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// consoleFactsTransport captures the request the entry forwarded to the
// application and answers it, so a test can read the facts Serve stated.
type consoleFactsTransport struct {
	forwarded *http.Request
}

func (t *consoleFactsTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	t.forwarded = request
	body := "application-response"
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        make(http.Header),
		Body:          io.NopCloser(strings.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       request,
	}, nil
}

// forwardedConsoleFacts drives the entry's proxy with one resolved route and
// returns the headers the application received.
func forwardedConsoleFacts(t *testing.T, origin RouteOrigin, route accessRoute, callerHeaders map[string]string) http.Header {
	t.Helper()
	entry := &AccessEntry{Origin: origin}
	transport := &consoleFactsTransport{}
	entry.Transport = transport
	request, err := http.NewRequest(http.MethodGet, "http://app-main.example/api/auth/user", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Host = "app-main.example"
	for name, value := range callerHeaders {
		request.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	entry.accessProxy(route).ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("forwarded request status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if transport.forwarded == nil {
		t.Fatal("the entry forwarded no request to the application")
	}
	return transport.forwarded.Header
}

// confirmedRoute is the route the entry resolved before it forwards anything.
func confirmedRoute() accessRoute {
	return accessRoute{
		WorkspaceID:       "ws-route",
		ApplicationID:     "knowledge-app",
		upstreamService:   "app-main",
		upstreamPort:      8080,
		forwardPath:       "/api/auth/user",
		RuntimeInstanceID: "runtime-route",
		DeploymentID:      "dep-route",
	}
}

// TestServeAccessEntryStatesTheConfirmedConsoleFacts proves the entry states
// the Console facts on the request it forwards: the installation's declared
// Console address and the Workspace of the confirmed route. A caller's own
// value for either name is replaced by Serve's own, and the caller's route
// selector stays stripped.
func TestServeAccessEntryStatesTheConfirmedConsoleFacts(t *testing.T) {
	origin := RouteOrigin{
		Scheme:            "https",
		WorkspaceDomain:   "workspace.example",
		ApplicationDomain: "apps.example",
		ConsoleURL:        "https://console.example",
	}
	header := forwardedConsoleFacts(t, origin, confirmedRoute(), map[string]string{
		"X-Opl-Console-Url":          "https://forged.example",
		"X-Opl-Console-Workspace-Id": "forged-workspace",
		"X-Opl-Workspace-Id":         "forged-workspace",
	})
	if header.Get("X-Opl-Console-Url") != "https://console.example" {
		t.Fatalf("console url=%q, want the installation's declared address", header.Get("X-Opl-Console-Url"))
	}
	if header.Get("X-Opl-Console-Workspace-Id") != "ws-route" {
		t.Fatalf("console workspace=%q, want the confirmed route's Workspace", header.Get("X-Opl-Console-Workspace-Id"))
	}
	if header.Get("X-Opl-Workspace-Id") != "" {
		t.Fatalf("the entry forwarded the caller's route selector: %q", header.Get("X-Opl-Workspace-Id"))
	}
}

// TestServeAccessEntryStatesNoConsoleAddressWithoutADeclaredOne proves an
// installation that declared no Console address states none: not a relative
// path, not the Workspace domain, not a guess. A caller's own value for the
// header is removed rather than forwarded.
func TestServeAccessEntryStatesNoConsoleAddressWithoutADeclaredOne(t *testing.T) {
	for name, declared := range map[string]string{
		"undeclared":   "",
		"not a url":    "console example",
		"host only":    "console.example",
		"relative":     "/console",
		"wrong scheme": "ftp://console.example",
		"no host":      "https://",
		"credentials":  "https://user:secret@console.example",
		"query":        "https://console.example?x=1",
	} {
		t.Run(name, func(t *testing.T) {
			origin := RouteOrigin{
				Scheme:            "https",
				WorkspaceDomain:   "workspace.example",
				ApplicationDomain: "apps.example",
				ConsoleURL:        declared,
			}
			header := forwardedConsoleFacts(t, origin, confirmedRoute(), map[string]string{
				"X-Opl-Console-Url": "https://forged.example",
			})
			if values := header.Values("X-Opl-Console-Url"); len(values) != 0 {
				t.Fatalf("the entry stated a console address %q for the declared %q", values, declared)
			}
			// The Workspace of the confirmed route is stated either way: it is
			// the route's own fact and never depends on the Console address.
			if header.Get("X-Opl-Console-Workspace-Id") != "ws-route" {
				t.Fatalf("console workspace=%q, want the confirmed route's Workspace", header.Get("X-Opl-Console-Workspace-Id"))
			}
		})
	}
}
