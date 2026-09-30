package delivery

// Serve publishes an Agent at an address the installation owns, never one Serve
// derives from a provider payload.
//
// The executing TKE provider reports only the in-cluster destination it created:
// one DNS label plus the declared entry port. The browser-facing origin is the
// installation's declared domain, held by the installation and passed in through
// configuration. Serve therefore resolves the publishable URL from that declared
// configuration and refuses when the installation declares none; it never
// composes a hostname out of a provider name, and it never treats the in-cluster
// upstream as the address a customer opens.
//
// The per-binding application origin label is deliberately byte-compatible with
// the retained entry logic so an already published binding keeps the same origin
// across the ownership move: the label is the first 12 hex characters of the
// SHA-1 over the null-separated parts "workspace-application-origin", the
// Workspace id and the application id. A different digest would silently move
// every existing binding to a new hostname and orphan its TLS certificate and
// browser storage.

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

// The installation's route origin is read from three literal environment names so
// the installation contract in deploy/portable/opl-cloud-owner-topology.json can
// derive them from this file. The entry a customer's browser opens and the access
// data plane that answers it are both served by Serve, so which domain and scheme
// they use is an installation fact the instance owner declares, never an inference
// Serve makes.

// applicationOriginLabelLength keeps the derived application part short while
// making an accidental collision between two applications of the same Workspace
// practically impossible. It is part of the retained entry contract.
const applicationOriginLabelLength = 12

// RouteOrigin is the installation's declared route origin for Agent delivery. An
// empty field means the installation declared no such fact, and the corresponding
// address is then simply unpublishable: Serve records the delivery as running but
// not open rather than publishing an address that cannot resolve.
type RouteOrigin struct {
	// Scheme is the browser scheme for any published entry ("https" or "http").
	Scheme string
	// WorkspaceDomain is the host Workspace entries are published under.
	WorkspaceDomain string
	// ApplicationDomain is the domain per-binding application origins live under.
	ApplicationDomain string
}

// RouteOriginFromEnv reads the installation's declared route origin. It performs
// no inference: a missing or malformed value stays empty and the affected address
// is refused later, never guessed.
func RouteOriginFromEnv() RouteOrigin {
	return RouteOrigin{
		// OPL_PUBLIC_URL: the installation's own public address; its scheme is the
		// scheme every published entry uses, so the two cannot disagree.
		Scheme: originScheme(os.Getenv("OPL_PUBLIC_URL")),
		// OPL_WORKSPACE_DOMAIN: the host Workspace entries are published under, for
		// example the `/w/<workspaceId>/` route.
		WorkspaceDomain: bareDomain(os.Getenv("OPL_WORKSPACE_DOMAIN")),
		// OPL_WORKSPACE_APPLICATION_DOMAIN: the domain per-binding application
		// origins live under. An installation that publishes none states none.
		ApplicationDomain: bareDomain(os.Getenv("OPL_WORKSPACE_APPLICATION_DOMAIN")),
	}
}

// Configured reports whether the installation declared any publishable origin.
func (o RouteOrigin) Configured() bool {
	return o.Scheme != "" && (o.WorkspaceDomain != "" || o.ApplicationDomain != "")
}

// WorkspaceEntryURL is the retained path-based entry the installation publishes
// for one Workspace: scheme://<workspace domain>/w/<workspaceId>/. It reports
// false when the installation declares no workspace domain or scheme.
func (o RouteOrigin) WorkspaceEntryURL(workspaceID string) (string, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	if o.Scheme == "" || o.WorkspaceDomain == "" || !dnsLabelValid(workspaceID) {
		return "", false
	}
	if !hostValid(o.WorkspaceDomain) {
		return "", false
	}
	return o.Scheme + "://" + o.WorkspaceDomain + "/w/" + workspaceID + "/", true
}

// ApplicationEntryURL is the address one (Workspace, application) binding is
// served at: scheme://<workspaceId>-<label>.<application domain>/. It reports
// false when the installation declares no application-origin domain, or when the
// identity cannot compose directly into a hostname; the caller then keeps the
// retained Workspace entry.
func (o RouteOrigin) ApplicationEntryURL(workspaceID, applicationID string) (string, bool) {
	host, ok := o.ApplicationEntryHost(workspaceID, applicationID)
	if !ok || o.Scheme == "" {
		return "", false
	}
	return o.Scheme + "://" + host + "/", true
}

// ApplicationEntryHost composes the binding's external hostname from the
// installation's application-origin domain.
func (o RouteOrigin) ApplicationEntryHost(workspaceID, applicationID string) (string, bool) {
	workspaceID = strings.TrimSpace(workspaceID)
	if o.ApplicationDomain == "" || !dnsLabelValid(workspaceID) {
		return "", false
	}
	label, ok := applicationOriginLabel(workspaceID, strings.TrimSpace(applicationID))
	if !ok {
		return "", false
	}
	host := workspaceID + "-" + label + "." + strings.ToLower(o.ApplicationDomain)
	if !hostValid(host) {
		return "", false
	}
	return host, true
}

// applicationOriginLabel derives the application half of the hostname from the
// binding identity only, never from a tag, a digest or a deployment attempt. The
// digest is SHA-1 over the null-separated parts to stay byte-compatible with the
// retained entry that already published these bindings.
func applicationOriginLabel(workspaceID, applicationID string) (string, bool) {
	if !dnsLabelValid(workspaceID) || !dnsLabelValid(applicationID) {
		return "", false
	}
	hash := sha1.New()
	for _, part := range []string{"workspace-application-origin", workspaceID, applicationID} {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))[:applicationOriginLabelLength], true
}

// gatewayUpstream validates the single in-cluster destination a provider asked
// the installation gateway to proxy to: one DNS label the cluster resolves, and
// the declared entry port. An address, an external host or a compound name is
// refused, because the destination is one Service the provider created and not a
// value Serve may reinterpret.
func gatewayUpstream(serviceName string, port int) (string, error) {
	value := strings.TrimSpace(serviceName)
	if !dnsLabelValid(value) {
		return "", errors.New("workspace_application_upstream_invalid")
	}
	if net.ParseIP(value) != nil || strings.EqualFold(value, "localhost") || strings.HasSuffix(strings.ToLower(value), ".localhost") {
		return "", errors.New("workspace_application_upstream_invalid")
	}
	if port < 1 || port > 65535 {
		return "", errors.New("workspace_application_upstream_invalid")
	}
	return "http://" + value + ":" + strconv.Itoa(port), nil
}

// dnsLabelValid accepts what may appear as one DNS label.
func dnsLabelValid(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' {
			continue
		}
		if char != '-' || index == 0 || index == len(value)-1 {
			return false
		}
	}
	return true
}

// hostValid checks a composed name against the label rules a resolver applies.
func hostValid(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !dnsLabelValid(label) {
			return false
		}
	}
	return true
}

// originScheme reads the browser scheme from the installation's public address.
// It reports an empty scheme for an address that is not a plain web origin, so a
// misconfigured installation publishes nothing instead of an unroutable scheme.
func originScheme(publicURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(publicURL))
	if err != nil || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	return scheme
}

// bareDomain normalizes a declared domain: no scheme, no path, lower case.
func bareDomain(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(value, "https://")
	value = strings.TrimPrefix(value, "http://")
	return strings.ToLower(strings.Trim(value, "/"))
}

// ResolveApplicationEntry is the exported view of resolveApplicationEntry for the
// adapter's boundary tests and any owner-side caller that only needs the address.
func ResolveApplicationEntry(origin RouteOrigin, workspaceID, applicationID string, entry contracts.WorkspaceApplicationEntry) (string, error) {
	accessURL, _, err := resolveApplicationEntry(origin, workspaceID, applicationID, entry)
	return accessURL, err
}

// resolveApplicationEntry resolves the publishable address of one admitted entry
// plus the validated in-cluster upstream the installation gateway proxies to.
//
// A provider that publishes its own endpoint states its URL and has no gateway
// upstream; a gateway entry is addressed through the installation's declared
// origin. An entry whose origin the installation did not declare is unpublishable
// and refused with a typed reason, never replaced by an inferred hostname.
func resolveApplicationEntry(origin RouteOrigin, workspaceID, applicationID string, entry contracts.WorkspaceApplicationEntry) (accessURL, upstream string, err error) {
	if err := contracts.ValidateWorkspaceApplicationEntry(entry); err != nil {
		return "", "", refuse(ReasonAppAccessUnavailable)
	}
	if entry.URL != "" {
		parsed, err := url.Parse(strings.TrimSpace(entry.URL))
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			return "", "", refuse(ReasonAppAccessUnavailable)
		}
		return parsed.String(), "", nil
	}
	upstream, err = gatewayUpstream(entry.ServiceName, entry.Port)
	if err != nil {
		return "", "", refuse(ReasonAppAccessUnavailable)
	}
	if address, ok := origin.ApplicationEntryURL(workspaceID, applicationID); ok {
		return address, upstream, nil
	}
	if address, ok := origin.WorkspaceEntryURL(workspaceID); ok {
		return address, upstream, nil
	}
	return "", "", refuse(ReasonAppAccessUnavailable)
}

// ParseApplicationEntryHost reverses ApplicationEntryHost. It reads the
// Workspace identity and the application label back out of the name, so the
// access data plane resolves a request without a lookup table; the caller still
// confirms the label against the binding's current target, because a name that
// no longer matches belongs to a superseded application rather than to this one.
func (o RouteOrigin) ParseApplicationEntryHost(host string) (workspaceID, applicationLabel string, ok bool) {
	host = strings.TrimSpace(strings.ToLower(host))
	if parsed, _, err := net.SplitHostPort(host); err == nil {
		host = parsed
	} else if strings.Contains(host, ":") {
		return "", "", false
	}
	host = strings.TrimSuffix(host, ".")
	domain := strings.ToLower(strings.TrimSpace(o.ApplicationDomain))
	if domain == "" || !strings.HasSuffix(host, "."+domain) {
		return "", "", false
	}
	prefix := strings.TrimSuffix(host, "."+domain)
	index := strings.LastIndex(prefix, "-")
	if index <= 0 {
		return "", "", false
	}
	workspaceID, applicationLabel = prefix[:index], prefix[index+1:]
	if !dnsLabelValid(workspaceID) || len(applicationLabel) != applicationOriginLabelLength || !hexLabelValid(applicationLabel) {
		return "", "", false
	}
	return workspaceID, applicationLabel, true
}

// hexLabelValid accepts the lowercase hex the origin label is composed of.
func hexLabelValid(value string) bool {
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
