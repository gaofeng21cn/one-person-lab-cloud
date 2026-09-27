package httpapi

import (
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func (s *Server) registerGatewayRoutes(mux *http.ServeMux, gateway api.GatewayProductServiceClient) {
	s.publisherRoute(mux, "GET /api/v2/wallet", owneridentity.Gateway, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", nil, func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
		if gateway == nil {
			return nil, status.Error(codes.Unavailable, "gateway owner is not configured")
		}
		return gateway.GetWallet(r.Context(), &api.GetWalletRpcRequest{Context: c})
	})
}
