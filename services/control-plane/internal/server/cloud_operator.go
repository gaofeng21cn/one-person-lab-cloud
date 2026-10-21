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

		{"/internal/cloud/operator/health", nil, func(w http.ResponseWriter, r *http.Request) {
			writeSourceEnvelope(w, http.StatusOK, "control-plane", "available", app.operatorHealth(r.Context(), service))
		}},
		{"/internal/cloud/operator/runtime-observations", nil, func(w http.ResponseWriter, r *http.Request) { app.writeOperatorRuntimeObservations(w, r, service) }},
		{"/internal/cloud/operator/reconciliation", pagination, app.writeOperatorReconciliation},
		{"/internal/cloud/operator/workspace-launches/{operationId}/recovery", nil, func(w http.ResponseWriter, r *http.Request) { app.writeOperatorWorkspaceLaunchRecovery(w, r, service) }},
		{"/internal/cloud/operator/wallet-adjustments/{operationId}", nil, app.getWalletAdjustment},
		{"/internal/cloud/operator/workspaces/{workspaceId}/runtime-image-replacements/{operationId}", nil, app.getWorkspaceRuntimeImageReplacement},
		{"/internal/cloud/operator/application-deployments/{operationId}", nil, app.writeOperatorApplicationDeployment},
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
		mux.HandleFunc("GET "+route.path, app.cloudOperatorRead(route.read, route.query))
	}
	// Activation is the one operator command the Console BFF composes with the
	// Tenant owner. It runs the same writer as the retained browser route; the
	// BFF proves its peer identity, service token and live CloudIdentity session
	// and authorizes the platform operator before reaching it.
	mux.HandleFunc("POST /internal/cloud/operator/accounts", app.cloudOperatorWrite(func(w http.ResponseWriter, r *http.Request) {
		app.provisionOperatorAccount(w, r, service)
	}))
}

// cloudOperatorWrite is the write counterpart of cloudOperatorRead. It shares the
// same peer/token/session proof, admits no query selector, and leaves the
// command's own body, CSRF and idempotency validation to the shared writer.
func (app *controlPlaneServer) cloudOperatorWrite(command http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		if _, ok := app.cloudSessionProof(w, r); !ok {
			return
		}
		if r.URL.RawQuery != "" {
			writeError(w, http.StatusBadRequest, "invalid_operator_write")
			return
		}
		if r.ContentLength <= 0 {
			writeError(w, http.StatusBadRequest, "invalid_operator_write")
			return
		}
		command(w, r)
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

// cloudOperatorRead shares the established peer/token/session proof with each
// retained GET writer. It never enters the legacy browser-session middleware.
func (app *controlPlaneServer) cloudOperatorRead(read http.HandlerFunc, query map[string]bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		r = r.WithContext(ctx)
		if !app.cloudOperatorProof(w, r, query) {
			return
		}
		read(w, r)
	}
}
