package httpapi

import (
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func personalGatewayQuery(r *http.Request, allowed ...string) (url.Values, error) {
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil || r.ContentLength != 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid personal Gateway query")
	}
	for key, values := range query {
		accepted := false
		for _, field := range allowed {
			if key == field {
				accepted = true
				break
			}
		}
		if !accepted || len(values) != 1 {
			return nil, status.Error(codes.InvalidArgument, "unknown or repeated personal Gateway query parameter")
		}
	}
	return query, nil
}

func personalGatewayPagination(query url.Values) (int32, int32, error) {
	page, size := int32(1), int32(20)
	for _, field := range []string{"page", "pageSize"} {
		values, present := query[field]
		if !present {
			continue
		}
		n, err := strconv.ParseInt(values[0], 10, 32)
		max := int64(100)
		if field == "page" {
			max = 1_000_000
		}
		if err != nil || n < 1 || n > max || strconv.FormatInt(n, 10) != values[0] {
			return 0, 0, status.Error(codes.InvalidArgument, "invalid personal Gateway pagination")
		}
		if field == "page" {
			page = int32(n)
		} else {
			size = int32(n)
		}
	}
	return page, size, nil
}

func personalGatewayPeriod(query url.Values) (string, error) {
	switch period := query.Get("period"); period {
	case "today", "week", "month":
		return period, nil
	default:
		return "", status.Error(codes.InvalidArgument, "period must be today, week or month")
	}
}

func (s *Server) registerPersonalGatewayKeyRoutes(mux *http.ServeMux, gateway api.GatewayProductServiceClient) {
	s.publisherRoute(mux, "GET /api/v2/gateway/keys", owneridentity.Gateway, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", nil, func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
		query, err := personalGatewayQuery(r, "page", "pageSize", "search", "status", "groupId", "sortBy", "sortOrder")
		if err != nil {
			return nil, err
		}
		page, size, err := personalGatewayPagination(query)
		if err != nil {
			return nil, err
		}
		input := &api.ListPersonalGatewayKeysRpcRequest{Context: c, Page: page, PageSize: size, Search: query.Get("search"), Status: query.Get("status"), SortBy: "createdAt", SortOrder: "desc"}
		if !utf8.ValidString(input.Search) || utf8.RuneCountInString(input.Search) > 100 {
			return nil, status.Error(codes.InvalidArgument, "search exceeds 100 characters")
		}
		if values, present := query["status"]; present {
			switch values[0] {
			case "active", "disabled", "quota_exhausted", "expired":
			default:
				return nil, status.Error(codes.InvalidArgument, "invalid key status")
			}
		}
		if values, present := query["groupId"]; present {
			id, err := strconv.ParseInt(values[0], 10, 64)
			if err != nil || id < 0 || strconv.FormatInt(id, 10) != values[0] {
				return nil, status.Error(codes.InvalidArgument, "invalid group ID")
			}
			input.GroupId = proto.String(values[0])
		}
		if values, present := query["sortBy"]; present {
			switch values[0] {
			case "name", "id", "currentConcurrency", "expiresAt", "status", "lastUsedAt", "createdAt":
				input.SortBy = values[0]
			default:
				return nil, status.Error(codes.InvalidArgument, "invalid key sort")
			}
		}
		if values, present := query["sortOrder"]; present {
			switch values[0] {
			case "asc", "desc":
				input.SortOrder = values[0]
			default:
				return nil, status.Error(codes.InvalidArgument, "invalid key sort order")
			}
		}
		if err := requireGateway(gateway); err != nil {
			return nil, err
		}
		return gateway.ListPersonalGatewayKeys(r.Context(), input)
	})
	s.publisherRoute(mux, "GET /api/v2/gateway/keys/{keyId}", owneridentity.Gateway, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEY, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", nil, func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
		if _, err := personalGatewayQuery(r); err != nil {
			return nil, err
		}
		if err := requireGateway(gateway); err != nil {
			return nil, err
		}
		return gateway.GetPersonalGatewayKey(r.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: c, KeyId: r.PathValue("keyId")})
	})
	s.publisherRoute(mux, "GET /api/v2/gateway/groups", owneridentity.Gateway, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYGROUPS, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", nil, func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
		if _, err := personalGatewayQuery(r); err != nil {
			return nil, err
		}
		if err := requireGateway(gateway); err != nil {
			return nil, err
		}
		return gateway.ListPersonalGatewayGroups(r.Context(), &api.ListPersonalGatewayGroupsRpcRequest{Context: c})
	})
	s.publisherRoute(mux, "GET /api/v2/gateway/keys/{keyId}/usage", owneridentity.Gateway, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPERSONALGATEWAYKEYUSAGE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", nil, func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
		query, err := personalGatewayQuery(r, "page", "pageSize", "period")
		if err != nil {
			return nil, err
		}
		page, size, err := personalGatewayPagination(query)
		if err != nil {
			return nil, err
		}
		period, err := personalGatewayPeriod(query)
		if err != nil {
			return nil, err
		}
		if err := requireGateway(gateway); err != nil {
			return nil, err
		}
		return gateway.ListPersonalGatewayKeyUsage(r.Context(), &api.ListPersonalGatewayKeyUsageRpcRequest{Context: c, KeyId: r.PathValue("keyId"), Page: page, PageSize: size, Period: period})
	})
	s.publisherRoute(mux, "GET /api/v2/gateway/keys/{keyId}/usage-summary", owneridentity.Gateway, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETPERSONALGATEWAYKEYUSAGESUMMARY, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, "", nil, func(r *http.Request, c *api.CallContext, _ proto.Message) (proto.Message, error) {
		query, err := personalGatewayQuery(r, "period")
		if err != nil {
			return nil, err
		}
		period, err := personalGatewayPeriod(query)
		if err != nil {
			return nil, err
		}
		if err := requireGateway(gateway); err != nil {
			return nil, err
		}
		return gateway.GetPersonalGatewayKeyUsageSummary(r.Context(), &api.GetPersonalGatewayKeyUsageSummaryRpcRequest{Context: c, KeyId: r.PathValue("keyId"), Period: period})
	})
}
