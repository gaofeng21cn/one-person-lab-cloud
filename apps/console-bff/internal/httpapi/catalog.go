// Resource Catalog routes.
//
// This file is independent route registration only: it names the Resource
// Catalog's own REST paths and reuses the shared authenticated route boundary
// (browser session, CSRF, same-origin, Idempotency-Key, CloudIdentity
// authorization, typed owner call and error mapping). It restates no guard. The
// BFF owns no catalog data; every value it returns is the owner's own readback.
package httpapi

import (
	"net/http"
	"strconv"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// NewCatalogHandler exposes the Resource Catalog routes over the same
// authenticated boundary the BFF process serves. It exists so an isolated
// composition can exercise the real boundary with explicit typed dependencies.
func NewCatalogHandler(catalog api.ResourceCatalogProductServiceClient, identity IdentityReader) http.Handler {
	s := &Server{identity: identity}
	mux := http.NewServeMux()
	s.registerCatalogRoutes(mux, catalog)
	return mux
}

// registerCatalogRoutes installs the Resource Catalog product surface.
//
// Every route names the catalog resource kind with no resource id. The owner
// authorizes a catalog action on that same resource, and CloudIdentity verifies a
// repeated authorization context against the exact actor, action, audience and
// resource it originally allowed, so a BFF request that named a plan id while the
// owner named none would be refused as a different resource.
//
// Admin paths sit under /api/v2/admin/catalog/, which the shared route boundary
// treats as platform scope; the customer list paths carry the caller's own tenant
// scope. The owner authorizes each action again on its own boundary, so the BFF
// check is a precondition rather than the authorization.
func (s *Server) registerCatalogRoutes(mux *http.ServeMux, catalog api.ResourceCatalogProductServiceClient) {
	const catalogKind = api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG
	// A GET carries no body, so the shared boundary applies session, same-origin
	// and CloudIdentity authorization but no CSRF or idempotency key: a read writes
	// nothing. The nil catalog case is reported as an unavailable owner instead of a
	// nil call.
	require := func() error {
		if catalog == nil {
			return status.Error(codes.Unavailable, "resource catalog owner is not configured")
		}
		return nil
	}

	// Customer reads: a member lists the plans its own tenant may choose from.
	s.publisherRoute(mux, "GET /api/v2/catalog/compute-plans", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTCOMPUTEPLANS, catalogKind, "", nil,
		func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.ListComputePlans(r.Context(), &api.ListComputePlansRpcRequest{Context: c, QueryCursor: optionalQuery(r, "cursor"), QueryLimit: optionalLimit(r), QueryStoragePlanId: optionalQuery(r, "storagePlanId")})
		})
	s.publisherRoute(mux, "GET /api/v2/catalog/storage-plans", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTSTORAGEPLANS, catalogKind, "", nil,
		func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.ListStoragePlans(r.Context(), &api.ListStoragePlansRpcRequest{Context: c, QueryCursor: optionalQuery(r, "cursor"), QueryLimit: optionalLimit(r), QueryComputePlanId: optionalQuery(r, "computePlanId")})
		})

	// The customer pricing surface. A quote is priced by the owner and is neither a
	// reservation nor a charge; the caller's own tenant scope is what the owner
	// authorizes against.
	s.publisherRoute(mux, "POST /api/v2/quotes", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEQUOTE, catalogKind, "", func() proto.Message { return &api.QuoteRequest{} },
		func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.CreateQuote(r.Context(), &api.CreateQuoteRpcRequest{Context: c, Body: body.(*api.QuoteRequest)})
		})
	s.publisherRoute(mux, "GET /api/v2/quotes/{quoteId}", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE, catalogKind, "", nil,
		func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.GetQuote(r.Context(), &api.GetQuoteRpcRequest{Context: c, QuoteId: r.PathValue("quoteId")})
		})

	// Administrator plan admission.
	s.publisherRoute(mux, "POST /api/v2/admin/catalog/compute-plans", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATECOMPUTEPLAN, catalogKind, "", func() proto.Message { return &api.CreateComputePlanRequest{} },
		func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.CreateComputePlan(r.Context(), &api.CreateComputePlanRpcRequest{Context: c, Body: body.(*api.CreateComputePlanRequest)})
		})
	s.publisherRoute(mux, "PUT /api/v2/admin/catalog/compute-plans/{planId}/availability", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETCOMPUTEPLANAVAILABILITY, catalogKind, "", func() proto.Message { return &api.PlanAvailabilityRequest{} },
		func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.SetComputePlanAvailability(r.Context(), &api.SetComputePlanAvailabilityRpcRequest{Context: c, Body: body.(*api.PlanAvailabilityRequest), PlanId: r.PathValue("planId")})
		})
	s.publisherRoute(mux, "POST /api/v2/admin/catalog/storage-plans", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATESTORAGEPLAN, catalogKind, "", func() proto.Message { return &api.CreateStoragePlanRequest{} },
		func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.CreateStoragePlan(r.Context(), &api.CreateStoragePlanRpcRequest{Context: c, Body: body.(*api.CreateStoragePlanRequest)})
		})
	s.publisherRoute(mux, "PUT /api/v2/admin/catalog/storage-plans/{planId}/availability", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETSTORAGEPLANAVAILABILITY, catalogKind, "", func() proto.Message { return &api.PlanAvailabilityRequest{} },
		func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.SetStoragePlanAvailability(r.Context(), &api.SetStoragePlanAvailabilityRpcRequest{Context: c, Body: body.(*api.PlanAvailabilityRequest), PlanId: r.PathValue("planId")})
		})

	// Administrator policy versions.
	s.publisherRoute(mux, "GET /api/v2/admin/catalog/price-policies", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPRICEPOLICYVERSIONS, catalogKind, "", nil,
		func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.ListPricePolicyVersions(r.Context(), &api.ListPricePolicyVersionsRpcRequest{Context: c, QueryCursor: optionalQuery(r, "cursor"), QueryLimit: optionalLimit(r)})
		})
	s.publisherRoute(mux, "POST /api/v2/admin/catalog/price-policies", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEPRICEPOLICYVERSION, catalogKind, "", func() proto.Message { return &api.CreatePricePolicyRequest{} },
		func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.CreatePricePolicyVersion(r.Context(), &api.CreatePricePolicyVersionRpcRequest{Context: c, Body: body.(*api.CreatePricePolicyRequest)})
		})
	s.publisherRoute(mux, "GET /api/v2/admin/catalog/refund-policies", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTREFUNDPOLICYVERSIONS, catalogKind, "", nil,
		func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.ListRefundPolicyVersions(r.Context(), &api.ListRefundPolicyVersionsRpcRequest{Context: c, QueryCursor: optionalQuery(r, "cursor"), QueryLimit: optionalLimit(r)})
		})
	s.publisherRoute(mux, "POST /api/v2/admin/catalog/refund-policies", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEREFUNDPOLICYVERSION, catalogKind, "", func() proto.Message { return &api.CreateRefundPolicyRequest{} },
		func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.CreateRefundPolicyVersion(r.Context(), &api.CreateRefundPolicyVersionRpcRequest{Context: c, Body: body.(*api.CreateRefundPolicyRequest)})
		})
	s.publisherRoute(mux, "GET /api/v2/admin/catalog/retention-policies", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRETENTIONPOLICYVERSIONS, catalogKind, "", nil,
		func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.ListRetentionPolicyVersions(r.Context(), &api.ListRetentionPolicyVersionsRpcRequest{Context: c, QueryCursor: optionalQuery(r, "cursor"), QueryLimit: optionalLimit(r)})
		})
	s.publisherRoute(mux, "POST /api/v2/admin/catalog/retention-policies", owneridentity.ResourceCatalog, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATERETENTIONPOLICYVERSION, catalogKind, "", func() proto.Message { return &api.CreateRetentionPolicyRequest{} },
		func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
			if err := require(); err != nil {
				return nil, err
			}
			return catalog.CreateRetentionPolicyVersion(r.Context(), &api.CreateRetentionPolicyVersionRpcRequest{Context: c, Body: body.(*api.CreateRetentionPolicyRequest)})
		})
}

// optionalQuery returns the query parameter, always non-nil, so an absent cursor or
// filter is an empty value rather than an omitted field the owner would default.
func optionalQuery(r *http.Request, name string) *string {
	value := r.URL.Query().Get(name)
	return &value
}

func optionalLimit(r *http.Request) *int32 {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return nil
	}
	parsed, err := strconv.ParseInt(raw, 10, 32)
	if err != nil {
		return nil
	}
	value := int32(parsed)
	return &value
}
