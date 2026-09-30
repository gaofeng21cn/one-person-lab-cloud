package delivery

// Serve owns the Workspace application entry. This file is the data plane that
// reads the same access binding the route state machine commits and forwards a
// request to the exact running instance that binding names.
//
// The rules here are the owner boundary, not a policy layer:
//
//   - The upstream is never composed from the request. Only the confirmed
//     binding selects a target, and only that target's own runtime-instance row
//     supplies the in-cluster Service the request is forwarded to.
//   - A route that was fenced but never activated, a target that is not ready,
//     or a target whose in-cluster destination is unknown is refused. There is
//     no fallback to another owner's route and no best-effort destination.
//   - The application origin carries the application identity, so a request for
//     an origin that no longer matches the current target is refused instead of
//     being served by the replacement application.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// accessDefaultAddress is the entry's listen address when the installation
// declares none. It is an installation fact like every other owner address, and
// it must stay distinct from every other declared owner address: the Serve
// process already listens for its owner surface on OPL_SERVE_ADDR, and the
// gateway-integration process defaults to :8187.
const accessDefaultAddress = ":8188"

// accessRefusal is one typed reason the entry did not serve a request. The status
// is the transport answer and the code is the stable reason a client can act on.
type accessRefusal struct {
	status int
	code   string
}

func (e accessRefusal) Error() string { return e.code }

var (
	// errAccessOriginUnresolved: the request names no Workspace the installation
	// publishes.
	errAccessOriginUnresolved = accessRefusal{status: http.StatusNotFound, code: "workspace_application_origin_unresolved"}
	// errAccessRouteUnconfirmed: the Workspace has no confirmed route target.
	errAccessRouteUnconfirmed = accessRefusal{status: http.StatusServiceUnavailable, code: "workspace_application_route_unconfirmed"}
	// errAccessTargetNotReady: the confirmed target has no ready runtime instance
	// with an in-cluster destination.
	errAccessTargetNotReady = accessRefusal{status: http.StatusServiceUnavailable, code: "workspace_application_target_not_ready"}
	// errAccessApplicationSuperseded: the origin's application label does not
	// describe the confirmed target.
	errAccessApplicationSuperseded = accessRefusal{status: http.StatusGone, code: "workspace_application_origin_retired"}
)

// AccessEntry is Serve's Workspace application entry.
type AccessEntry struct {
	DB *sql.DB
	// Origin is the installation's declared route origin. An installation that
	// declares no application domain serves only the retained path entry.
	Origin RouteOrigin
	// Transport is the proxy transport. It stays nil in production, keeping the
	// reverse proxy's own defaults.
	Transport http.RoundTripper
	// Now reads the observation time a served response reports. It is overridable
	// only so a test can pin it.
	Now func() time.Time
}

// NewAccessEntry binds the entry to Serve's own database and the installation's
// declared origin.
func NewAccessEntry(db *sql.DB, origin RouteOrigin) (*AccessEntry, error) {
	if db == nil {
		return nil, errors.New("serve access entry requires the owner database")
	}
	return &AccessEntry{DB: db, Origin: origin}, nil
}

// accessRoute is one resolved request: the confirmed binding, its target
// instance, and the exact in-cluster destination to forward to.
type accessRoute struct {
	WorkspaceID       string
	ApplicationID     string
	RuntimeInstanceID string
	DeploymentID      string
	RouteGeneration   int64
	SwitchID          string
	Revision          string
	upstreamService   string
	upstreamPort      int
	forwardPath       string
}

// Handler returns the entry's HTTP handler.
func (e *AccessEntry) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", e.serve)
	return mux
}

// serve resolves the request against Serve's own binding and proxies it. The
// application sees its own paths, cookies and streaming behavior: the entry
// forwards the external host and scheme and never rewrites the body.
func (e *AccessEntry) serve(w http.ResponseWriter, r *http.Request) {
	route, err := e.Resolve(r.Context(), r.Host, r.URL.Path)
	if err != nil {
		var refusal accessRefusal
		if errors.As(err, &refusal) {
			writeAccessRefusal(w, r, refusal)
			return
		}
		writeAccessRefusal(w, r, accessRefusal{status: http.StatusServiceUnavailable, code: "workspace_application_route_unavailable"})
		return
	}
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort(route.upstreamService, strconv.Itoa(route.upstreamPort))}
	proxy := &httputil.ReverseProxy{
		Transport: e.Transport,
		// Streaming responses (SSE, chunked logs) must reach the client as they
		// arrive instead of being buffered by the entry.
		FlushInterval: -1,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(target)
			request.Out.URL.Path = route.forwardPath
			request.Out.Host = request.In.Host
			request.Out.Header.Set("X-Forwarded-Host", request.In.Host)
			request.Out.Header.Set("X-Forwarded-Proto", externalScheme(request.In))
			request.Out.Header.Del("X-Opl-Workspace-Id")
			request.Out.Header.Del("X-Opl-Route-Generation")
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			writeAccessRefusal(w, r, accessRefusal{status: http.StatusBadGateway, code: "workspace_application_upstream_failed"})
		},
	}
	proxy.ServeHTTP(w, r)
}

// Resolve maps one request to the confirmed target it must be served from. It is
// the only place that decides which Workspace a request names and which instance
// serves it.
func (e *AccessEntry) Resolve(ctx context.Context, host, requestPath string) (accessRoute, error) {
	workspaceID, applicationLabel, pathBased, refusal := e.resolveIdentity(host, requestPath)
	if refusal != nil {
		return accessRoute{}, refusal
	}
	route := accessRoute{WorkspaceID: workspaceID}
	err := e.DB.QueryRowContext(ctx, `
		SELECT b.route_generation, COALESCE(b.last_confirmed_switch_id,''), COALESCE(b.route_revision,''),
		       COALESCE(b.target_runtime_instance_id,''), COALESCE(b.target_deployment_id,'')
		FROM serve.access_bindings b WHERE b.workspace_id = $1`, workspaceID).
		Scan(&route.RouteGeneration, &route.SwitchID, &route.Revision, &route.RuntimeInstanceID, &route.DeploymentID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return accessRoute{}, errAccessRouteUnconfirmed
	case err != nil:
		return accessRoute{}, err
	}
	if route.SwitchID == "" || route.RuntimeInstanceID == "" || route.DeploymentID == "" {
		return accessRoute{}, errAccessRouteUnconfirmed
	}
	var status string
	err = e.DB.QueryRowContext(ctx, `
		SELECT status, COALESCE(access_upstream_service,''), COALESCE(access_upstream_port,0),
		       COALESCE(deployment_descriptor #>> '{applicationRevision,applicationId}','')
		FROM serve.agent_runtime_instances
		WHERE id=$1 AND workspace_id=$2 AND deployment_id=$3`,
		route.RuntimeInstanceID, workspaceID, route.DeploymentID).
		Scan(&status, &route.upstreamService, &route.upstreamPort, &route.ApplicationID)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return accessRoute{}, errAccessTargetNotReady
	case err != nil:
		return accessRoute{}, err
	}
	if status != "ready" || route.upstreamService == "" || route.upstreamPort < 1 || route.upstreamPort > 65535 {
		return accessRoute{}, errAccessTargetNotReady
	}
	if !pathBased {
		expected, ok := applicationOriginLabel(workspaceID, route.ApplicationID)
		if !ok || expected != applicationLabel {
			return accessRoute{}, errAccessApplicationSuperseded
		}
	}
	switch {
	case pathBased:
		route.forwardPath = strings.TrimPrefix(requestPath, "/w/"+workspaceID)
		if route.forwardPath == "" {
			route.forwardPath = "/"
		}
	case requestPath == "":
		route.forwardPath = "/"
	default:
		route.forwardPath = requestPath
	}
	return route, nil
}

// resolveIdentity reads the Workspace and application a request names. A
// per-binding application origin carries both; the retained path entry names
// only the Workspace and therefore serves whatever that Workspace's confirmed
// target is.
func (e *AccessEntry) resolveIdentity(host, requestPath string) (workspaceID, applicationLabel string, pathBased bool, refusal error) {
	if id, label, ok := e.Origin.ParseApplicationEntryHost(host); ok {
		return id, label, false, nil
	}
	if id, ok := retainedWorkspaceIDFromPath(requestPath); ok {
		return id, "", true, nil
	}
	return "", "", false, errAccessOriginUnresolved
}

// retainedWorkspaceIDFromPath reads the Workspace identity out of the retained
// path entry, scheme://<workspace domain>/w/<workspaceId>/....
func retainedWorkspaceIDFromPath(requestPath string) (string, bool) {
	if !strings.HasPrefix(requestPath, "/w/") {
		return "", false
	}
	rest := strings.TrimPrefix(requestPath, "/w/")
	if index := strings.Index(rest, "/"); index >= 0 {
		rest = rest[:index]
	}
	if !dnsLabelValid(rest) {
		return "", false
	}
	return rest, true
}

func externalScheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func writeAccessRefusal(w http.ResponseWriter, _ *http.Request, refusal accessRefusal) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(refusal.status)
	_, _ = fmt.Fprintf(w, `{"error":%q,"retryable":%t}`, refusal.code, refusal.status == http.StatusServiceUnavailable)
}
