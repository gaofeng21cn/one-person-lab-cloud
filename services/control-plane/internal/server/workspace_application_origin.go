package server

import (
	"net"
	"net/http"
	"os"
	"strings"
)

// A Workspace's binding to one application is published at its own browser
// origin: one hostname per (Workspace, application) pair. The binding owns that
// whole host, so the application keeps its own root paths, cookies, storage and
// streaming behavior, and two Workspaces on this installation never share an
// origin. Control Plane owns the route; the instance owns the domain, its DNS
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
	return strings.Trim(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(os.Getenv(workspaceApplicationOriginDomainEnv)), "https://"), "http://"), "/")
}

// workspaceApplicationOriginDigestLength keeps the derived application part
// short while making an accidental collision between two applications of the
// same Workspace practically impossible.
const workspaceApplicationOriginDigestLength = 12

// workspaceApplicationOriginLabel is the application half of the hostname. It is
// derived from the binding identity only, never from a tag, a digest or a
// deployment attempt.
func workspaceApplicationOriginLabel(workspaceID, applicationID string) (string, bool) {
	workspaceID, applicationID = strings.TrimSpace(workspaceID), strings.TrimSpace(applicationID)
	if !workspaceOriginIdentifierValid(workspaceID) || !workspaceOriginIdentifierValid(applicationID) {
		return "", false
	}
	digest := stableID("workspace-application-origin", workspaceID, applicationID)
	return digest[:workspaceApplicationOriginDigestLength], true
}

// workspaceApplicationOriginHost composes the binding's external hostname from
// the installation's application-origin domain. It reports false when the
// installation publishes no such domain, or when the identity cannot form a DNS
// label; such a binding simply has no application origin and keeps the retained
// path-based entry.
func workspaceApplicationOriginHost(workspaceID, applicationID string) (string, bool) {
	domain := workspaceApplicationOriginDomain()
	workspaceID = strings.TrimSpace(workspaceID)
	if domain == "" || !workspaceOriginIdentifierValid(workspaceID) {
		return "", false
	}
	label, ok := workspaceApplicationOriginLabel(workspaceID, applicationID)
	if !ok {
		return "", false
	}
	host := workspaceID + "-" + label + "." + strings.ToLower(domain)
	if !workspaceOriginHostValid(host) {
		return "", false
	}
	return host, true
}

// workspaceApplicationOriginURL is the address an operator opens for one
// binding. The installation terminates TLS in front of this server, which is
// the same assumption the retained Workspace entry already makes.
func workspaceApplicationOriginURL(workspaceID, applicationID string) (string, bool) {
	host, ok := workspaceApplicationOriginHost(workspaceID, applicationID)
	if !ok {
		return "", false
	}
	scheme := workspaceExternalScheme()
	if scheme == "" {
		return "", false
	}
	return scheme + "://" + host + "/", true
}

// parseWorkspaceApplicationOriginHost reverses workspaceApplicationOriginHost
// for routing. It reads the Workspace identity out of the name, so routing needs
// no lookup table; the caller still confirms the application part against the
// Workspace's current binding, because a name that no longer matches belongs to
// a superseded application rather than to this one.
func parseWorkspaceApplicationOriginHost(host string) (workspaceID, applicationLabel string, ok bool) {
	host = strings.TrimSpace(host)
	if host == "" {
		return "", "", false
	}
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	} else if strings.Contains(host, ":") {
		return "", "", false
	}
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	domain := strings.ToLower(workspaceApplicationOriginDomain())
	if domain == "" {
		return "", "", false
	}
	suffix := "." + domain
	if !strings.HasSuffix(host, suffix) {
		return "", "", false
	}
	label := strings.TrimSuffix(host, suffix)
	if label == "" || strings.Contains(label, ".") {
		return "", "", false
	}
	separator := strings.LastIndex(label, "-")
	if separator <= 0 || separator == len(label)-1 {
		return "", "", false
	}
	workspaceID, applicationLabel = label[:separator], label[separator+1:]
	if len(applicationLabel) != workspaceApplicationOriginDigestLength || !workspaceOriginHexValid(applicationLabel) {
		return "", "", false
	}
	if !workspaceOriginIdentifierValid(workspaceID) {
		return "", "", false
	}
	return workspaceID, applicationLabel, true
}

// workspaceApplicationOriginRequest reports the binding a request is addressed
// to, when the request arrives on a binding origin.
func workspaceApplicationOriginRequest(r *http.Request) (string, string, bool) {
	return parseWorkspaceApplicationOriginHost(r.Host)
}

// workspaceOriginIdentifierValid accepts what may appear as a DNS label. The
// accepted alphabet is deliberately narrow: an identity that cannot compose
// directly into a hostname gets no origin instead of an escaped one.
func workspaceOriginIdentifierValid(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, char := range value {
		alphanumeric := char >= 'a' && char <= 'z' || char >= '0' && char <= '9'
		if alphanumeric {
			continue
		}
		if char != '-' || index == 0 || index == len(value)-1 {
			return false
		}
	}
	return true
}

func workspaceOriginHexValid(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

// workspaceOriginHostValid checks the composed name against the label rules a
// resolver applies, so an installation cannot configure a domain that produces
// an unreachable binding address.
func workspaceOriginHostValid(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !workspaceOriginIdentifierValid(label) {
			return false
		}
	}
	return true
}
