// Package httpapi — Serve delivery read routes.
//
// These routes expose Serve's own delivery facts (deployment history, one
// deployment, and the current Agent's access state) to the Console. They are the
// Serve read surface only: they compose nothing from another owner and therefore
// do not depend on the Workspace read the composed delivery view needs.
//
// Every route carries the same two guards the composed delivery view uses: a
// live CloudIdentity session, and a CloudIdentity decision for the exact
// operation against the Serve audience. The caller's own session scope is what is
// presented; the BFF never widens it and never mints an identity of its own.
//
// The mux registration lives in the shared server file, which the identity
// integrator owns; the handler and its route installer are exported so that
// wiring is one line and the route table cannot diverge from this file.
package httpapi

import (
	"context"
	"errors"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// ServeDeliveryReader is the typed Serve read surface these routes need. It is
// satisfied by the BFF's gRPC client set and by a test double.
type ServeDeliveryReader interface {
	Deployments(ctx context.Context, workspaceID string) (*api.DeploymentPage, error)
	Deployment(ctx context.Context, workspaceID, deploymentID string) (*api.Deployment, error)
	WorkspaceAccess(ctx context.Context, workspaceID string) (*api.WorkspaceAccess, error)
}

// NewServeDeliveryHandler returns the authenticated Serve read routes. It is the
// same implementation the process serves, so a test drives the real guard, the
// real caller propagation and the real typed owner calls.
func NewServeDeliveryHandler(reader ServeDeliveryReader, identity IdentityReader) http.Handler {
	mux := http.NewServeMux()
	RegisterServeDeliveryRoutes(mux, reader, identity)
	return mux
}

// RegisterServeDeliveryRoutes installs the Serve read routes on a mux. The
// process passes its own mux; tests pass a fresh one. One installer means the
// route table cannot diverge.
func RegisterServeDeliveryRoutes(mux *http.ServeMux, reader ServeDeliveryReader, identity IdentityReader) {
	mux.HandleFunc("GET /api/v2/workspaces/{workspaceId}/deployments", serveReadRoute(reader, identity,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS,
		func(ctx context.Context, workspaceID, _ string) (any, error) {
			return reader.Deployments(ctx, workspaceID)
		}))

	mux.HandleFunc("GET /api/v2/workspaces/{workspaceId}/deployments/{deploymentId}", serveReadRoute(reader, identity,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETDEPLOYMENT,
		func(ctx context.Context, workspaceID, deploymentID string) (any, error) {
			if deploymentID == "" {
				return nil, errDeploymentIDRequired
			}
			return reader.Deployment(ctx, workspaceID, deploymentID)
		}))

	// getWorkspaceAccess is POST in the canonical contract; it reads the current
	// Agent's access state without changing it.
	mux.HandleFunc("POST /api/v2/workspaces/{workspaceId}/access", serveReadRoute(reader, identity,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS,
		func(ctx context.Context, workspaceID, _ string) (any, error) {
			return reader.WorkspaceAccess(ctx, workspaceID)
		}))
}

var errDeploymentIDRequired = errors.New("DEPLOYMENT_ID_REQUIRED")

// serveReadRoute is the single guard for the Serve read surface. It resolves the
// caller's live session, asks CloudIdentity for the exact Serve action and
// Workspace resource using the caller's own scope, and only then calls the owner.
// A caller that cannot be identified is 401; a decision that is not ALLOWED is
// 403; an unavailable upstream is 503. No failure path invents a value.
func serveReadRoute(reader ServeDeliveryReader, identity IdentityReader, action api.AuthorizationActionEnum, read func(ctx context.Context, workspaceID, deploymentID string) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if reader == nil {
			writeError(w, http.StatusInternalServerError, "bff_unconfigured", "serve reader is not configured")
			return
		}
		workspaceID := r.PathValue("workspaceId")
		if workspaceID == "" {
			writeServeReadError(w, http.StatusBadRequest, "WORKSPACE_ID_REQUIRED", "workspace id is required")
			return
		}
		caller, err := RequireSession(r.Context(), identity, r)
		if err != nil {
			writeServeReadIdentityError(w, err)
			return
		}
		ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
		call := clients.CallContext(ctx)
		if err := RequireAuthorizedAction(ctx, identity, caller, owneridentity.Serve, action,
			&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: &workspaceID},
			call.GetRequestId()); err != nil {
			writeServeReadIdentityError(w, err)
			return
		}
		value, err := read(ctx, workspaceID, r.PathValue("deploymentId"))
		if err != nil {
			if errors.Is(err, errDeploymentIDRequired) {
				writeServeReadError(w, http.StatusBadRequest, "DEPLOYMENT_ID_REQUIRED", "deployment id is required")
				return
			}
			// The owner's own refusal propagates with its status: a caller that may
			// not read this Workspace gets 403, a missing deployment 404, an
			// unreachable owner 503. The BFF never substitutes a value on failure.
			writeServeReadOwnerError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, value)
	}
}

// writeServeReadIdentityError maps an identity failure onto its own status. A
// denial is never reported as an outage.
func writeServeReadIdentityError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSessionRequired), status.Code(err) == codes.Unauthenticated:
		writeServeReadError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "session required")
	case errors.Is(err, ErrAuthorizationRequired), status.Code(err) == codes.PermissionDenied:
		writeServeReadError(w, http.StatusForbidden, "FORBIDDEN", "action is not authorized")
	default:
		writeServeReadError(w, http.StatusServiceUnavailable, "DEPENDENCY_UNAVAILABLE", "identity authority unavailable")
	}
}

// writeServeReadOwnerError maps the Serve owner's refusal onto its own status.
func writeServeReadOwnerError(w http.ResponseWriter, err error) {
	switch status.Code(err) {
	case codes.NotFound:
		writeServeReadError(w, http.StatusNotFound, "NOT_FOUND", "resource not found")
	case codes.PermissionDenied:
		writeServeReadError(w, http.StatusForbidden, "FORBIDDEN", "action is not authorized")
	case codes.InvalidArgument, codes.FailedPrecondition:
		writeServeReadError(w, http.StatusBadRequest, "INVALID_REQUEST", "request rejected by the owner")
	default:
		writeServeReadError(w, http.StatusServiceUnavailable, "OWNER_UNAVAILABLE", "owner unavailable")
	}
}

func writeServeReadError(w http.ResponseWriter, httpStatus int, code, message string) {
	writeJSON(w, httpStatus, map[string]any{"ok": false, "error": code, "safeMessage": message})
}
