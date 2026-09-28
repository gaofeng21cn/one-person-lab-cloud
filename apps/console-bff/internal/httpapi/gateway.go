package httpapi

import (
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// registerGatewayRoutes exposes the Gateway Integration product reads the Console
// launch journey consumes. The BFF holds no wallet or model state of its own; the
// Gateway owner performs authorization and returns its own live readback.
func (s *Server) registerGatewayRoutes(mux *http.ServeMux, gateway api.GatewayProductServiceClient) {
	s.publisherRoute(mux, "GET /api/v2/wallet", owneridentity.Gateway, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", nil, func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
		if gateway == nil {
			return nil, status.Error(codes.Unavailable, "gateway owner is not configured")
		}
		return gateway.GetWallet(r.Context(), &api.GetWalletRpcRequest{Context: c})
	})
}

// registerModelCatalogRoute serves the Gateway-owned model catalog. It is
// registered by the Resource Catalog route set in the running process and by the
// Gateway read composition an isolated cross-owner test drives.
func (s *Server) registerModelCatalogRoute(mux *http.ServeMux, gateway api.GatewayProductServiceClient) {
	s.publisherRoute(mux, "GET /api/v2/catalog/models", owneridentity.Gateway, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTMODELS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", nil, func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
		if err := requireGateway(gateway); err != nil {
			return nil, err
		}
		return gateway.ListModels(r.Context(), &api.ListModelsRpcRequest{Context: c, QueryCursor: optionalQuery(r, "cursor"), QueryLimit: optionalLimit(r)})
	})
}

// NewGatewayReadHandler exposes only the Gateway-owned product reads (wallet and
// model catalog) over the BFF's authenticated boundary. It exists so a caller
// outside apps/console-bff, which cannot reach the internal package, can still
// drive the real route guard, authorization precondition and serialization.
func NewGatewayReadHandler(gateway api.GatewayProductServiceClient, identity IdentityReader) http.Handler {
	s := &Server{identity: identity, gateway: gateway}
	mux := http.NewServeMux()
	s.registerGatewayRoutes(mux, gateway)
	s.registerModelCatalogRoute(mux, gateway)
	return mux
}
