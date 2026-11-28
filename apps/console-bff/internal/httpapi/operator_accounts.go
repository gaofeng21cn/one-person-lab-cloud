package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func (s *Server) readOperatorAccounts(w http.ResponseWriter, r *http.Request, ctx context.Context, caller Caller, page, pageSize int, search string) {
	if s.tenant == nil {
		writeOwnerCallFailure(w, r, status.Error(codes.Unavailable, "Tenant owner is not configured"))
		return
	}
	call := proto.Clone(clients.CallContext(ctx)).(*api.CallContext)
	call.AuthorizationContextId = ""
	tenantCtx := clients.WithCallContext(ctx, call)
	if err := RequireAuthorizedAction(
		tenantCtx,
		s.identity,
		caller,
		owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORACCOUNTS,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM},
		call.GetRequestId(),
	); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	resp, err := s.tenant.ListOperatorAccounts(tenantCtx, &api.ListOperatorAccountsRpcRequest{
		Context:  call,
		Page:     int32(page),
		PageSize: int32(pageSize),
		Search:   search,
	})
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	if resp == nil || resp.Page != int32(page) || resp.PageSize != int32(pageSize) || resp.Total < 0 || len(resp.Items) > pageSize {
		writeOwnerCallFailure(w, r, status.Error(codes.DataLoss, "invalid Tenant operator accounts page"))
		return
	}
	actorIDs := make([]string, 0, len(resp.Items))
	for _, entry := range resp.Items {
		if entry.GetActorId() != "" {
			actorIDs = append(actorIDs, entry.GetActorId())
		}
	}
	var serviceEntitlementsByActor map[string][]map[string]any
	if len(actorIDs) > 0 {
		serviceEntitlementsByActor, err = s.projectOperatorServiceEntitlements(tenantCtx, call, actorIDs)
		if err != nil {
			writeOwnerCallFailure(w, r, err)
			return
		}
	}
	items := make([]any, 0, len(resp.Items))
	for _, entry := range resp.Items {
		actorID := entry.GetActorId()
		if actorID == "" {
			writeOwnerCallFailure(w, r, status.Error(codes.DataLoss, "invalid operator account actor id"))
			return
		}
		role := "user"
		if entry.GetPlatformAdminEnabled() {
			role = "admin"
		}
		updatedAt := operatorTime(entry.GetGatewayUpdatedAt())
		gatewayIdentity := operatorSource("sub2api", map[string]any{
			"userId": actorID,
			"email":  entry.GetEmail(),
			"status": entry.GetStatus(),
		}, "", updatedAt)

		var wallet map[string]any
		if entry.GetWalletAvailable() {
			wallet = operatorSource("sub2api", map[string]any{
				"userId":    actorID,
				"currency":  "USD",
				"usdMicros": strconv.FormatInt(entry.GetWalletUsdMicros(), 10),
				"status":    entry.GetStatus(),
			}, "", updatedAt)
		} else {
			wallet = operatorSource("sub2api", nil, "wallet_unavailable", "")
		}

		var usage map[string]any
		if entry.GetUsageAvailable() {
			platforms := make([]any, 0, len(entry.GetUsageByPlatform()))
			for _, p := range entry.GetUsageByPlatform() {
				platforms = append(platforms, map[string]any{
					"platform":                 p.GetPlatform(),
					"todayActualCostUsdMicros": p.GetTodayActualCostUsdMicros(),
					"totalActualCostUsdMicros": p.GetTotalActualCostUsdMicros(),
				})
			}
			usage = operatorSource("sub2api", map[string]any{
				"todayActualCostUsdMicros": entry.GetTodayActualCostUsdMicros(),
				"totalActualCostUsdMicros": entry.GetTotalActualCostUsdMicros(),
				"byPlatform":               platforms,
			}, "", "")
		} else {
			usage = operatorSource("sub2api", nil, "usage_unavailable", "")
		}

		var keyCount map[string]any
		if entry.GetKeyCountAvailable() {
			keyCount = operatorSource("sub2api", entry.GetKeyCount(), "", "")
		} else {
			keyCount = operatorSource("sub2api", nil, "key_count_unavailable", "")
		}

		entitlements := make([]any, 0, len(entry.GetPackageEntitlements()))
		for _, pkg := range entry.GetPackageEntitlements() {
			rec := map[string]any{
				"packageId": pkg.GetPackageId(),
				"enabled":   pkg.GetEnabled(),
				"reason":    pkg.GetReason(),
			}
			if pkg.GetExpiresAt() != nil {
				rec["expiresAt"] = pkg.GetExpiresAt().AsTime().UTC().Format(time.RFC3339Nano)
			}
			if pkg.GetUpdatedAt() != nil {
				rec["updatedAt"] = pkg.GetUpdatedAt().AsTime().UTC().Format(time.RFC3339Nano)
			}
			entitlements = append(entitlements, rec)
		}

		items = append(items, map[string]any{
			"accountId":                actorID,
			"actorId":                  actorID,
			"consoleUserId":            "",
			"role":                     role,
			"sub2apiUserId":            actorID,
			"email":                    entry.GetEmail(),
			"status":                   entry.GetStatus(),
			"workspacePurchaseEnabled": entry.GetWorkspacePurchaseEnabled(),
			"platformAdminEnabled":     entry.GetPlatformAdminEnabled(),
			"isBootstrapAdmin":         entry.GetIsBootstrapAdmin(),
			"linkRelayEnabled":         entry.GetLinkRelayEnabled(),
			"packageEntitlements":      entitlements,
			"serviceEntitlements": func() []map[string]any {
				if entries := serviceEntitlementsByActor[actorID]; entries != nil {
					return entries
				}
				return []map[string]any{}
			}(),
			"gatewayIdentity":          gatewayIdentity,
			"wallet":                   wallet,
			"keyCount":                 keyCount,
			"usage":                    usage,
			"workspaceCount":           operatorSource("workspace", nil, "workspace_count_not_read", ""),
		})
	}
	envelope := operatorSource("tenant+sub2api", map[string]any{
		"items":    items,
		"total":    resp.Total,
		"page":     page,
		"pageSize": pageSize,
	}, "", "")
	if len(items) == 0 {
		envelope["status"] = "empty"
	}
	writeJSON(w, http.StatusOK, envelope)
}
