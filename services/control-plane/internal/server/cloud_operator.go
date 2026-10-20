package server

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"opl-cloud/services/control-plane/internal/controlplane"
)

// The Console BFF serves the operator console under CloudIdentity
// authorization. It reaches these routes with the same service token and live
// CloudIdentity session reference the retained customer reads use; Control
// Plane proves the peer, the token and the session, then runs the same read
// functions the retained /api/operator/* routes run.
//
// The operator privilege itself is a CloudIdentity decision (a configured
// platform admin), not a Control Plane session fact. Control Plane therefore
// does not accept a browser admin session here and never mints or clears one:
// a rejected read on this path leaves every cookie untouched.
func (app *controlPlaneServer) registerCloudOperatorRoutes(mux *http.ServeMux, service *controlplane.Service) {
	pagination := map[string]bool{"page": true, "pageSize": true}
	replacementPreview := map[string]bool{"replacementImageDigest": true}
	for _, route := range []struct {
		path  string
		query map[string]bool
		read  http.HandlerFunc
	}{
		{"/internal/cloud/operator/overview", nil, func(w http.ResponseWriter, r *http.Request) {
			app.writeOperatorOverview(w, r, service)
		}},
		{"/internal/cloud/operator/accounts", pagination, func(w http.ResponseWriter, r *http.Request) {
			app.writeOperatorAccounts(w, r, service)
		}},
		{"/internal/cloud/operator/workspaces", pagination, func(w http.ResponseWriter, r *http.Request) {
			app.writeOperatorWorkspaces(w, r, service)
		}},
		{"/internal/cloud/operator/workspaces/{workspaceId}", nil, func(w http.ResponseWriter, r *http.Request) {
			app.writeOperatorWorkspaceDetail(w, r, service)
		}},
		{"/internal/cloud/operator/workspace-runtime-image-policy", nil, func(w http.ResponseWriter, r *http.Request) {
			app.getWorkspaceImageReleasePolicy(w, r)
		}},
		{"/internal/cloud/operator/announcements", pagination, func(w http.ResponseWriter, r *http.Request) {
			app.listOperatorAnnouncements(w, r)
		}},
		{"/internal/cloud/operator/workspaces/{workspaceId}/runtime-image-replacements/preview", replacementPreview, func(w http.ResponseWriter, r *http.Request) {
			app.previewWorkspaceRuntimeImageReplacement(w, r, service)
		}},
	} {
		route := route
		mux.HandleFunc("GET "+route.path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "private, no-store")
			ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
			defer cancel()
			r = r.WithContext(ctx)
			if !app.cloudOperatorProof(w, r, route.query) {
				return
			}
			route.read(w, r)
		})
	}
}

// cloudOperatorProof requires the established BFF peer/token/session proof and
// admits only the query keys the shared read writer itself validates. A nil
// allowlist admits no query at all. No browser-selected tenant, actor or account
// can reach an operator read.
func (app *controlPlaneServer) cloudOperatorProof(w http.ResponseWriter, r *http.Request, allowed map[string]bool) bool {
	if _, ok := app.cloudSessionProof(w, r); !ok {
		return false
	}
	if r.ContentLength != 0 {
		writeError(w, http.StatusBadRequest, "invalid_operator_read")
		return false
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_operator_query")
		return false
	}
	for key, values := range query {
		if !allowed[key] || len(values) != 1 {
			writeError(w, http.StatusBadRequest, "invalid_operator_query")
			return false
		}
	}
	return true
}
