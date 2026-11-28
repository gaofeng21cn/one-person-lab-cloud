package server

import (
	"net/http"
	"os"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

// A Workspace's binding to one application is published at its own browser
// origin: one hostname per (Workspace, application) pair. The binding owns that
// whole host, so the application keeps its own root paths, cookies, storage and
// streaming behavior, and two Workspaces on this installation never share an
// origin. Serve owns the current route; the instance owns the domain, its DNS
// and its certificate.
//
// The hostname is a pure function of the binding identity, so it needs no
// allocation table and no second writer: every module derives the same value.
// The application part is in the name because an origin must change when the
// Workspace's application changes. A compatible update keeps the same
// application identity and therefore the same origin; an unrelated application
// gets a different one, so the previous application's service worker or browser
// storage cannot act on its replacement.

// workspaceApplicationOriginDomainEnv names the domain the per-binding origins
// are published under.
const workspaceApplicationOriginDomainEnv = "OPL_WORKSPACE_APPLICATION_DOMAIN"

// workspaceApplicationOriginDomain is the domain an application origin lives
// under. It is deliberately separate from the Workspace host: the entry shape is
// what decides which DNS records and which certificate the installation must
// hold, so deriving one domain from the other would hide an installation
// requirement behind an inference. An installation that publishes no
// application origins states no such domain, and every binding then keeps the
// retained path-based entry.
func workspaceApplicationOriginDomain() string {
	return workspaceApplicationOriginDomainWith(os.Getenv)
}

func workspaceApplicationOriginDomainWith(getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	return strings.Trim(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(getenv(workspaceApplicationOriginDomainEnv)), "https://"), "http://"), "/")
}

// workspaceApplicationOriginLabel uses the same canonical binding identity as Serve.
func workspaceApplicationOriginLabel(workspaceID, applicationID string) (string, bool) {
	return contracts.WorkspaceApplicationOriginLabel(workspaceID, applicationID)
}

// workspaceApplicationOriginHost shares the canonical DNS encoding with Serve;
// retained origins keep their exact bytes and native identities remain reversible.
func workspaceApplicationOriginHost(workspaceID, applicationID string) (string, bool) {
	return workspaceApplicationOriginHostWith(os.Getenv, workspaceID, applicationID)
}

func workspaceApplicationOriginHostWith(getenv func(string) string, workspaceID, applicationID string) (string, bool) {
	return contracts.WorkspaceApplicationOriginHost(workspaceID, applicationID, workspaceApplicationOriginDomainWith(getenv))
}

// workspaceApplicationOriginURL is the address an operator opens for one
// binding. The installation terminates TLS in front of this server, which is
// the same assumption the retained Workspace entry already makes.
func workspaceApplicationOriginURL(workspaceID, applicationID string) (string, bool) {
	return workspaceApplicationOriginURLWith(os.Getenv, workspaceID, applicationID)
}

func workspaceApplicationOriginURLWith(getenv func(string) string, workspaceID, applicationID string) (string, bool) {
	host, ok := workspaceApplicationOriginHostWith(getenv, workspaceID, applicationID)
	if !ok {
		return "", false
	}
	scheme := workspaceExternalSchemeWith(getenv)
	if scheme == "" {
		return "", false
	}
	return scheme + "://" + host + "/", true
}

func (app *controlPlaneServer) workspaceApplicationOriginURL(workspaceID, applicationID string) (string, bool) {
	return workspaceApplicationOriginURLWith(app.env, workspaceID, applicationID)
}

// parseWorkspaceApplicationOriginHost reverses workspaceApplicationOriginHost
// for routing. It reads the Workspace identity out of the name, so routing needs
// no lookup table; the caller still confirms the application part against the
// Workspace's current binding, because a name that no longer matches belongs to
// a superseded application rather than to this one.
func parseWorkspaceApplicationOriginHost(host string) (workspaceID, applicationLabel string, ok bool) {
	return parseWorkspaceApplicationOriginHostWith(os.Getenv, host)
}

func parseWorkspaceApplicationOriginHostWith(getenv func(string) string, host string) (workspaceID, applicationLabel string, ok bool) {
	return contracts.ParseWorkspaceApplicationOriginHost(host, workspaceApplicationOriginDomainWith(getenv))
}

// workspaceApplicationOriginRequest reports the binding a request is addressed
// to, when the request arrives on a binding origin.
func (app *controlPlaneServer) workspaceApplicationOriginRequest(r *http.Request) (string, string, bool) {
	return parseWorkspaceApplicationOriginHostWith(app.env, r.Host)
}
