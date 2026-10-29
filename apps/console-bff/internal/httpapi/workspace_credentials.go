package httpapi

import (
	"net/http"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func (s *Server) registerWorkspaceCredentialRoutes(mux *http.ServeMux, workspace api.WorkspaceProductServiceClient) {
	// Reuse the typed product route's live session, CSRF, origin and idempotency
	// guards. This read never uses an idempotency response cache.
	route := http.NewServeMux()
	const pattern = "POST /api/v2/workspaces/{workspaceId}/application-credentials/reveal"
	s.publisherRoute(route, pattern, owneridentity.Workspace, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVEALWORKSPACEAPPLICATIONCREDENTIALS,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "workspaceId", nil,
		func(r *http.Request, call *api.CallContext, _ proto.Message) (proto.Message, error) {
			if workspace == nil {
				return nil, status.Error(codes.Unavailable, "workspace owner is not configured")
			}
			value, err := workspace.RevealWorkspaceApplicationCredentials(r.Context(), &api.RevealWorkspaceApplicationCredentialsRpcRequest{Context: call, WorkspaceId: r.PathValue("workspaceId")})
			if err != nil {
				return nil, err
			}
			if value.GetWorkspaceId() != r.PathValue("workspaceId") || value.GetRuntimeInstanceId() == "" || strings.TrimSpace(value.GetUsername()) == "" || value.GetPassword() == "" {
				return nil, status.Error(codes.Unavailable, "invalid application credential readback")
			}
			return &api.WorkspaceApplicationCredentials{WorkspaceId: value.GetWorkspaceId(), RuntimeInstanceId: value.GetRuntimeInstanceId(), Username: value.GetUsername(), Password: value.GetPassword()}, nil
		})
	mux.Handle(pattern, privateCredentialResponse{route})
}

type privateCredentialResponse struct{ http.Handler }

func (h privateCredentialResponse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.Handler.ServeHTTP(privateCredentialWriter{w}, r)
}

type privateCredentialWriter struct{ http.ResponseWriter }

func (w privateCredentialWriter) WriteHeader(code int) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.ResponseWriter.WriteHeader(code)
}
func (w privateCredentialWriter) Write(raw []byte) (int, error) {
	w.Header().Set("Cache-Control", "private, no-store")
	return w.ResponseWriter.Write(raw)
}
