// Package httpapi exposes Serve's delivery facts through the shared BFF boundary.
package httpapi

import (
	"context"
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

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

// NewServeDeliveryHandler uses the same registration and guards as the process.
func NewServeDeliveryHandler(reader ServeDeliveryReader, identity IdentityReader) http.Handler {
	mux := http.NewServeMux()
	RegisterServeDeliveryRoutes(mux, reader, identity)
	return mux
}

// RegisterServeDeliveryRoutes delegates session, authorization, public JSON and
// error handling to the existing BFF boundary. The bodyless POST access route
// also receives its contract-required CSRF, same-origin and idempotency guards.
func RegisterServeDeliveryRoutes(mux *http.ServeMux, reader ServeDeliveryReader, identity IdentityReader) {
	s := &Server{identity: identity}
	const workspaceKind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE
	require := func() error {
		if reader == nil {
			return status.Error(codes.Unavailable, "serve reader is not configured")
		}
		return nil
	}

	s.publisherRoute(mux, "GET /api/v2/workspaces/{workspaceId}/deployments", owneridentity.Serve, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS, workspaceKind, "workspaceId", nil,
		func(r *http.Request, _ *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return reader.Deployments(r.Context(), r.PathValue("workspaceId"))
		})

	s.publisherRoute(mux, "GET /api/v2/workspaces/{workspaceId}/deployments/{deploymentId}", owneridentity.Serve, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETDEPLOYMENT, workspaceKind, "workspaceId", nil,
		func(r *http.Request, _ *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return reader.Deployment(r.Context(), r.PathValue("workspaceId"), r.PathValue("deploymentId"))
		})

	s.publisherRoute(mux, "POST /api/v2/workspaces/{workspaceId}/access", owneridentity.Serve, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS, workspaceKind, "workspaceId", nil,
		func(r *http.Request, _ *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return reader.WorkspaceAccess(r.Context(), r.PathValue("workspaceId"))
		})
}
