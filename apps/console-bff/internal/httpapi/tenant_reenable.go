package httpapi

import (
	"net/http"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// registerTenantReenableRoute serves the published account re-enable command.
//
// Re-enabling a Tenant is re-enabling a customer account: the personal
// customer owns exactly one Tenant, and the owner refuses that Tenant's members
// while it is suspended. The owner owns the write and decides the Tenant's own
// state; the BFF only carries the platform administrator's command over the
// typed client and publishes the owner's answer.
func (s *Server) registerTenantReenableRoute(mux *http.ServeMux) {
	s.publisherRoute(mux, "POST /api/v2/admin/tenants/{tenantId}/reenable", owneridentity.Tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REENABLETENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "tenantId", func() proto.Message { return &api.ReenableTenantRequest{} }, func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
		if s.tenant == nil {
			return nil, status.Error(codes.Unavailable, "tenant owner is not configured")
		}
		return s.tenant.ReenableTenant(r.Context(), &api.ReenableTenantRpcRequest{Context: c, TenantId: r.PathValue("tenantId"), Body: body.(*api.ReenableTenantRequest)})
	})
}
