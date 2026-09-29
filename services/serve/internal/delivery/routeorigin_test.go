package delivery_test

import (
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/serve/internal/delivery"
)

// TestServeRouteOriginKeepsRetainedBindingHostname pins the byte-compatible
// application-origin label. An already published binding keeps the same hostname
// across the ownership move, so its TLS certificate and browser storage still
// apply; a different digest would silently move every binding.
func TestServeRouteOriginKeepsRetainedBindingHostname(t *testing.T) {
	origin := delivery.RouteOrigin{Scheme: "https", WorkspaceDomain: "workspaces.example", ApplicationDomain: "apps.example"}
	// The vector is the retained entry's own SHA-1 label for (workspace,
	// application) = ("ws-route", "knowledge-app").
	host, ok := origin.ApplicationEntryHost("ws-route", "knowledge-app")
	if !ok || host != "ws-route-62097bae4a90.apps.example" {
		t.Fatalf("application host=%q ok=%v", host, ok)
	}
	url, ok := origin.ApplicationEntryURL("ws-route", "knowledge-app")
	if !ok || url != "https://ws-route-62097bae4a90.apps.example/" {
		t.Fatalf("application url=%q ok=%v", url, ok)
	}
	workspaceURL, ok := origin.WorkspaceEntryURL("ws-route")
	if !ok || workspaceURL != "https://workspaces.example/w/ws-route/" {
		t.Fatalf("workspace url=%q ok=%v", workspaceURL, ok)
	}
}

// TestServeRouteOriginRefusesToGuess covers every shape the installation did not
// declare: Serve publishes nothing instead of an unroutable or inferred host.
func TestServeRouteOriginRefusesToGuess(t *testing.T) {
	empty := delivery.RouteOrigin{}
	if _, ok := empty.WorkspaceEntryURL("ws-route"); ok {
		t.Fatal("empty origin published a workspace entry")
	}
	if _, ok := empty.ApplicationEntryURL("ws-route", "knowledge-app"); ok {
		t.Fatal("empty origin published an application entry")
	}
	// A scheme but no domain, or a domain but no scheme, is still unpublishable.
	partial := delivery.RouteOrigin{Scheme: "https"}
	if _, ok := partial.WorkspaceEntryURL("ws-route"); ok {
		t.Fatal("scheme-only origin published an entry")
	}
	// An identity that cannot compose into a DNS label gets no origin.
	labelled := delivery.RouteOrigin{Scheme: "https", ApplicationDomain: "apps.example"}
	if _, ok := labelled.ApplicationEntryURL("WS_ROUTE", "knowledge-app"); ok {
		t.Fatal("non-DNS workspace identity published an application origin")
	}
	if _, ok := labelled.ApplicationEntryURL("ws-route", "Knowledge App"); ok {
		t.Fatal("non-DNS application identity published an application origin")
	}
}

// TestServeGatewayUpstreamValidatesProviderDestination proves the in-cluster
// destination is exactly one DNS label plus the declared port: an address, an
// external host or a compound name is refused, never reinterpreted.
func TestServeGatewayUpstreamValidatesProviderDestination(t *testing.T) {
	entry := contracts.WorkspaceApplicationEntry{ServiceName: "app-runtime-http-main", Port: 8080}
	origin := delivery.RouteOrigin{Scheme: "https", ApplicationDomain: "apps.example"}
	address, err := delivery.ResolveApplicationEntry(origin, "ws-route", "knowledge-app", entry)
	if err != nil || !strings.HasPrefix(address, "https://ws-route-62097bae4a90.apps.example/") {
		t.Fatalf("gateway entry address=%q err=%v", address, err)
	}
	for _, bad := range []contracts.WorkspaceApplicationEntry{
		{ServiceName: "app.main.default.svc.cluster.local", Port: 8080},
		{ServiceName: "10.0.0.5", Port: 8080},
		{ServiceName: "localhost", Port: 8080},
		{ServiceName: "-invalid", Port: 8080},
		{ServiceName: "app-runtime-http-main", Port: 0},
		{ServiceName: "app-runtime-http-main", Port: 70000},
	} {
		if _, err := delivery.ResolveApplicationEntry(origin, "ws-route", "knowledge-app", bad); err == nil {
			t.Fatalf("provider destination %+v was accepted", bad)
		}
	}
	// A gateway entry with no declared origin is refused, never published at a
	// guessed host.
	if _, err := delivery.ResolveApplicationEntry(delivery.RouteOrigin{Scheme: "https"}, "ws-route", "knowledge-app", entry); err == nil {
		t.Fatal("gateway entry with no declared origin was published")
	}
	// A provider that publishes its own endpoint keeps that URL.
	selfPublished, err := delivery.ResolveApplicationEntry(delivery.RouteOrigin{}, "ws-route", "knowledge-app", contracts.WorkspaceApplicationEntry{URL: "https://provider.example/app"})
	if err != nil || selfPublished != "https://provider.example/app" {
		t.Fatalf("self-published entry=%q err=%v", selfPublished, err)
	}
	if _, err := delivery.ResolveApplicationEntry(delivery.RouteOrigin{}, "ws-route", "knowledge-app", contracts.WorkspaceApplicationEntry{URL: "provider.example/app"}); err == nil {
		t.Fatal("schemeless provider URL was accepted")
	}
}
