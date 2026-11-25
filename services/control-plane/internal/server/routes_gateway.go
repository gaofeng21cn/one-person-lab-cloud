package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

func registerGatewayRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	app.registerCloudGatewayReadRoutes(mux, service)
	mux.HandleFunc("GET /api/gateway/endpoint", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		endpoint, err := service.GatewayPublicEndpoint()
		if err != nil {
			writeSourceEnvelope(w, http.StatusInternalServerError, "sub2api", "unavailable", nil)
			return
		}
		writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", map[string]any{"baseUrl": endpoint})
	}))
	mux.HandleFunc("GET /api/gateway/groups", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		_, userID, credential, ok := app.gatewayUserContext(w, r)
		if !ok {
			return
		}
		groups, err := service.GatewayUserGroups(r.Context(), credential, userID)
		if err != nil {
			writeGatewaySourceError(w, err)
			return
		}
		items := make([]any, 0, len(groups))
		for _, group := range groups {
			items = append(items, map[string]any{
				"id": strconv.FormatInt(group.ID, 10), "name": group.Name, "description": group.Description,
				"platform": group.Platform, "rateMultiplier": group.RateMultiplier,
				"subscriptionType": group.SubscriptionType, "status": group.Status,
			})
		}
		status := "available"
		if len(items) == 0 {
			status = "empty"
		}
		writeSourceEnvelope(w, http.StatusOK, "sub2api", status, map[string]any{"items": items, "total": len(items)})
	}))
	mux.HandleFunc("GET /api/gateway/wallet", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		userID, ok := app.gatewaySub2APIUserID(w, r)
		if !ok {
			return
		}
		balance, err := service.Sub2APIBalance(r.Context(), userID)
		if err != nil {
			writeGatewaySourceError(w, err)
			return
		}
		writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", map[string]any{
			"userId": strconv.FormatInt(balance.UserID, 10), "currency": "USD", "usdMicros": strconv.FormatInt(balance.USDMicros, 10), "status": balance.Status,
		})
	}))
	mux.HandleFunc("GET /api/gateway/keys", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		_, userID, credential, ok := app.gatewayUserContext(w, r)
		if !ok {
			return
		}
		query, ok := gatewayKeyPageQuery(w, r)
		if !ok {
			return
		}
		page, err := service.GatewayUserKeyPage(r.Context(), credential, userID, query)
		if err != nil {
			writeGatewaySourceError(w, err)
			return
		}
		items := make([]any, 0, len(page.Items))
		for _, key := range page.Items {
			items = append(items, gatewayKeySummary(key))
		}
		status := "available"
		if len(items) == 0 {
			status = "empty"
		}
		writeSourceEnvelope(w, http.StatusOK, "sub2api", status, map[string]any{"items": items, "total": page.Total, "page": page.Page, "pageSize": page.PageSize, "pages": page.Pages})
	}))
	mux.HandleFunc("GET /api/gateway/keys/{keyId}", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		app.gatewayKey(w, r, service)
	}))
	mux.HandleFunc("POST /api/gateway/keys/{keyId}/reveal", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		app.revealGatewayKey(w, r, service)
	}))
	mux.HandleFunc("GET /api/gateway/keys/{keyId}/usage", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		app.gatewayKeyUsage(w, r, service)
	}))
	mux.HandleFunc("GET /api/gateway/keys/{keyId}/usage-summary", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		app.gatewayKeyUsageSummary(w, r, service)
	}))
	mux.HandleFunc("GET /api/gateway/usage-summary", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		app.gatewayAccountUsageSummary(w, r, service)
	}))
	mux.HandleFunc("GET /api/gateway/balance-history", app.protected(false, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "private, no-store")
		userID, ok := app.gatewaySub2APIUserID(w, r)
		if !ok {
			return
		}
		pageNumber, pageSize, ok := gatewayBalanceHistoryPagination(w, r)
		if !ok {
			return
		}
		app.gatewayBalanceHistoryForUser(w, r, service, userID, pageNumber, pageSize)
	}))
}

type gatewayKeyCommandEvidence struct {
	ID                   int64    `json:"id"`
	Name                 string   `json:"name,omitempty"`
	GroupID              int64    `json:"groupId,omitempty"`
	Status               string   `json:"status"`
	IPWhitelist          []string `json:"ipWhitelist,omitempty"`
	IPBlacklist          []string `json:"ipBlacklist,omitempty"`
	QuotaUSDMicros       int64    `json:"quotaUsdMicros,omitempty"`
	RateLimit5hUSDMicros int64    `json:"rateLimit5hUsdMicros,omitempty"`
	RateLimit1dUSDMicros int64    `json:"rateLimit1dUsdMicros,omitempty"`
	RateLimit7dUSDMicros int64    `json:"rateLimit7dUsdMicros,omitempty"`
	ExpiresAt            string   `json:"expiresAt,omitempty"`
}

func (app *controlPlaneServer) gatewayKey(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	_, userID, credential, ok := app.gatewayUserContext(w, r)
	if !ok {
		return
	}
	keyID, ok := gatewayKeyID(w, r)
	if !ok {
		return
	}
	key, err := service.GatewayUserKey(r.Context(), credential, userID, keyID)
	if err != nil {
		writeGatewayUserKeyError(w, err)
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", gatewayKeySummary(key))
}

func (app *controlPlaneServer) revealGatewayKey(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	user, userID, credential, ok := app.gatewayUserContext(w, r)
	if !ok {
		return
	}
	keyID, ok := gatewayKeyID(w, r)
	if !ok {
		return
	}
	key, err := service.GatewayUserKey(r.Context(), credential, userID, keyID)
	if err != nil {
		writeGatewayUserKeyError(w, err)
		return
	}
	if key.Key == "" {
		writeSourceEnvelope(w, http.StatusBadGateway, "sub2api", "unavailable", nil)
		return
	}
	if err := app.appendGatewayKeyRevealAudit(r, user, key); err != nil {
		writeError(w, http.StatusInternalServerError, "state_persist_failed")
		return
	}
	writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", map[string]any{
		"id": strconv.FormatInt(key.ID, 10), "name": key.Name, "status": key.Status, "value": key.Key,
	})
}

const gatewayKeyRevealAuditWindow = time.Hour

func (app *controlPlaneServer) appendGatewayKeyRevealAudit(r *http.Request, user map[string]any, key clients.Sub2APIWorkspaceKey) error {
	const action = "gateway.key_reveal"
	resourceID := strconv.FormatInt(key.ID, 10)
	accountID := stringValue(user["accountId"])
	event := app.auditEvent(r, action, "gateway_key", resourceID, accountID, nil, gatewayKeyEvidence(key), "succeeded")
	createdAt, err := time.Parse(time.RFC3339, stringValue(event["createdAt"]))
	if err != nil {
		return err
	}
	windowStart := createdAt.Truncate(gatewayKeyRevealAuditWindow).Format(time.RFC3339)
	event["id"] = "audit-" + stableID(action, accountID, stringValue(event["actorUserId"]), resourceID, windowStart)[:12]
	return app.tables.SaveAuditEvent(r.Context(), event)
}

func (app *controlPlaneServer) gatewayKeyUsage(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	_, userID, credential, ok := app.gatewayUserContext(w, r)
	if !ok {
		return
	}
	keyID, ok := gatewayKeyID(w, r)
	if !ok {
		return
	}
	page, pageSize, ok := gatewayUsagePagination(w, r)
	if !ok {
		return
	}
	period, ok := gatewayUsagePeriod(w, r)
	if !ok {
		return
	}
	usage, err := service.GatewayKeyUsage(r.Context(), credential, userID, keyID, page, pageSize, period)
	if err != nil {
		writeGatewayUserKeyError(w, err)
		return
	}
	writeGatewayUsagePage(w, usage)
}

func (app *controlPlaneServer) gatewayKeyUsageSummary(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	_, userID, credential, ok := app.gatewayUserContext(w, r)
	if !ok {
		return
	}
	keyID, ok := gatewayKeyID(w, r)
	if !ok {
		return
	}
	period, ok := gatewayUsagePeriod(w, r)
	if !ok {
		return
	}
	stats, err := service.GatewayKeyUsageStats(r.Context(), credential, userID, keyID, period)
	if err != nil {
		writeGatewayUserKeyError(w, err)
		return
	}
	writeGatewayUsageStats(w, stats)
}

func (app *controlPlaneServer) gatewayAccountUsageSummary(w http.ResponseWriter, r *http.Request, service *controlplane.Service) {
	userID, ok := app.gatewaySub2APIUserID(w, r)
	if !ok {
		return
	}
	period, ok := gatewayUsagePeriod(w, r)
	if !ok {
		return
	}
	stats, err := service.GatewayAccountUsageStats(r.Context(), userID, period)
	if err != nil {
		writeGatewaySourceError(w, err)
		return
	}
	writeGatewayUsageStats(w, stats)
}

func gatewayKeySummary(key clients.Sub2APIWorkspaceKey) map[string]any {
	var groupID, lastUsedAt, expiresAt, createdAt, updatedAt any
	if key.GroupID != nil {
		groupID = strconv.FormatInt(*key.GroupID, 10)
	}
	if key.LastUsedAt != nil {
		lastUsedAt = key.LastUsedAt.UTC().Format(time.RFC3339Nano)
	}
	if key.ExpiresAt != nil {
		expiresAt = key.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	if !key.CreatedAt.IsZero() {
		createdAt = key.CreatedAt.UTC().Format(time.RFC3339Nano)
	}
	if !key.UpdatedAt.IsZero() {
		updatedAt = key.UpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	workspace := reservedWorkspaceKeyName(key.Name)
	kind := "general"
	if workspace {
		kind = "workspace"
	}
	return map[string]any{
		"id": strconv.FormatInt(key.ID, 10), "name": key.Name, "groupId": groupID, "kind": kind, "status": key.Status,
		"ipWhitelist": append([]string(nil), key.IPWhitelist...), "ipBlacklist": append([]string(nil), key.IPBlacklist...),
		"quotaUsdMicros": key.QuotaUSDMicros, "quotaUsedUsdMicros": key.QuotaUsedUSDMicros,
		"rateLimit5hUsdMicros": key.RateLimit5hUSDMicros, "rateLimit1dUsdMicros": key.RateLimit1dUSDMicros,
		"rateLimit7dUsdMicros": key.RateLimit7dUSDMicros,
		"usage5hUsdMicros":     key.Usage5hUSDMicros, "usage1dUsdMicros": key.Usage1dUSDMicros,
		"usage7dUsdMicros": key.Usage7dUSDMicros, "currentConcurrency": key.CurrentConcurrency,
		"lastUsedAt": lastUsedAt, "lastUsedIp": key.LastUsedIP, "expiresAt": expiresAt, "createdAt": createdAt, "updatedAt": updatedAt,
		"manageable": !workspace, "deletable": !workspace,
	}
}

func gatewayKeyEvidence(key clients.Sub2APIWorkspaceKey) *gatewayKeyCommandEvidence {
	evidence := &gatewayKeyCommandEvidence{
		ID: key.ID, Name: key.Name, Status: key.Status, IPWhitelist: append([]string(nil), key.IPWhitelist...),
		IPBlacklist: append([]string(nil), key.IPBlacklist...), QuotaUSDMicros: key.QuotaUSDMicros,
		RateLimit5hUSDMicros: key.RateLimit5hUSDMicros, RateLimit1dUSDMicros: key.RateLimit1dUSDMicros,
		RateLimit7dUSDMicros: key.RateLimit7dUSDMicros,
	}
	if key.GroupID != nil {
		evidence.GroupID = *key.GroupID
	}
	if key.ExpiresAt != nil {
		evidence.ExpiresAt = key.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	return evidence
}

func gatewayKeyRequestHash(domain, accountID, resourceID string, input any) string {
	payload, _ := json.Marshal(input)
	return stableID(domain, accountID, resourceID, string(payload))
}

func reservedWorkspaceKeyName(name string) bool {
	return strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "opl-workspace")
}

func gatewayKeyID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	raw := strings.TrimSpace(r.PathValue("keyId"))
	keyID, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || keyID <= 0 || strconv.FormatInt(keyID, 10) != raw {
		writeError(w, http.StatusNotFound, "gateway_key_not_found")
		return 0, false
	}
	return keyID, true
}

func (app *controlPlaneServer) gatewayUserContext(w http.ResponseWriter, r *http.Request) (map[string]any, int64, clients.SessionDelegatedCredential, bool) {
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return nil, 0, clients.SessionDelegatedCredential{}, false
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "reauthentication_required")
		return nil, 0, clients.SessionDelegatedCredential{}, false
	}
	credential, ok := app.sessionCredentials.Get(sessionLookupKey(cookie.Value))
	if !ok {
		http.SetCookie(w, sessionCookie("", -1))
		writeError(w, http.StatusUnauthorized, "reauthentication_required")
		return nil, 0, clients.SessionDelegatedCredential{}, false
	}
	userID, err := app.sub2APIUserID(r.Context(), stringValue(user["accountId"]))
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "sub2api", "unavailable", nil)
		return nil, 0, clients.SessionDelegatedCredential{}, false
	}
	return user, userID, credential, true
}

func decodeStrictGatewayRequest(r *http.Request, output any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("invalid gateway key request")
	}
	return nil
}

func gatewayKeyPageQuery(w http.ResponseWriter, r *http.Request) (clients.Sub2APIKeyPageQuery, bool) {
	query := clients.Sub2APIKeyPageQuery{Page: 1, PageSize: 20, SortBy: "createdAt", SortOrder: "desc"}
	var err error
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		query.Page, err = strconv.Atoi(raw)
	}
	if err == nil {
		if raw := strings.TrimSpace(r.URL.Query().Get("pageSize")); raw != "" {
			query.PageSize, err = strconv.Atoi(raw)
		}
	}
	query.Search = strings.TrimSpace(r.URL.Query().Get("search"))
	query.Status = strings.TrimSpace(r.URL.Query().Get("status"))
	if raw := strings.TrimSpace(r.URL.Query().Get("groupId")); raw != "" {
		value, parseErr := strconv.ParseInt(raw, 10, 64)
		if parseErr != nil || value < 0 || strconv.FormatInt(value, 10) != raw {
			err = errors.New("invalid group")
		} else {
			query.GroupID = &value
		}
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("sortBy")); raw != "" {
		query.SortBy = raw
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("sortOrder")); raw != "" {
		query.SortOrder = raw
	}
	validStatus := query.Status == "" || query.Status == "active" || query.Status == "disabled" || query.Status == "quota_exhausted" || query.Status == "expired"
	validSort := query.SortBy == "name" || query.SortBy == "id" || query.SortBy == "currentConcurrency" || query.SortBy == "expiresAt" || query.SortBy == "status" || query.SortBy == "lastUsedAt" || query.SortBy == "createdAt"
	if err != nil || query.Page <= 0 || query.Page > 1_000_000 || query.PageSize <= 0 || query.PageSize > 100 || len([]rune(query.Search)) > 100 || !validStatus || !validSort || query.SortOrder != "asc" && query.SortOrder != "desc" {
		writeError(w, http.StatusBadRequest, "invalid_gateway_key_pagination")
		return clients.Sub2APIKeyPageQuery{}, false
	}
	return query, true
}

func gatewayUsagePagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	page, pageSize := 1, 20
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1_000_000 {
			writeError(w, http.StatusBadRequest, "invalid_gateway_usage_pagination")
			return 0, 0, false
		}
		page = value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("pageSize")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			writeError(w, http.StatusBadRequest, "invalid_gateway_usage_pagination")
			return 0, 0, false
		}
		pageSize = value
	}
	return page, pageSize, true
}

func gatewayBalanceHistoryPagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	page, pageSize := 1, 20
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 1_000_000 {
			writeError(w, http.StatusBadRequest, "invalid_gateway_balance_history_pagination")
			return 0, 0, false
		}
		page = value
	}
	if raw := strings.TrimSpace(r.URL.Query().Get("pageSize")); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			writeError(w, http.StatusBadRequest, "invalid_gateway_balance_history_pagination")
			return 0, 0, false
		}
		pageSize = value
	}
	return page, pageSize, true
}

func gatewayUsagePeriod(w http.ResponseWriter, r *http.Request) (string, bool) {
	period := strings.TrimSpace(r.URL.Query().Get("period"))
	if period == "" {
		period = "month"
	}
	if period != "today" && period != "week" && period != "month" {
		writeError(w, http.StatusBadRequest, "invalid_gateway_usage_period")
		return "", false
	}
	return period, true
}

func writeGatewayUsagePage(w http.ResponseWriter, usage clients.Sub2APIUsagePage) {
	items := make([]any, 0, len(usage.Items))
	for _, item := range usage.Items {
		items = append(items, map[string]any{
			"apiKeyId": strconv.FormatInt(item.APIKeyID, 10), "requestId": item.RequestID, "createdAt": item.CreatedAt.UTC().Format(time.RFC3339Nano),
			"model": item.Model, "inboundEndpoint": item.InboundEndpoint, "requestType": item.RequestType,
			"inputTokens": item.InputTokens, "outputTokens": item.OutputTokens, "cacheCreationTokens": item.CacheCreationTokens,
			"cacheReadTokens": item.CacheReadTokens, "actualCostUsdMicros": item.ActualCostUSDMicros,
			"durationMs": item.DurationMS, "firstTokenMs": item.FirstTokenMS,
		})
	}
	status := "available"
	if len(items) == 0 {
		status = "empty"
	}
	writeSourceEnvelope(w, http.StatusOK, "sub2api", status, map[string]any{"items": items, "total": usage.Total, "page": usage.Page, "pageSize": usage.PageSize, "pages": usage.Pages})
}

func writeGatewayUsageStats(w http.ResponseWriter, stats clients.Sub2APIUsageStats) {
	writeSourceEnvelope(w, http.StatusOK, "sub2api", "available", map[string]any{
		"totalRequests": stats.TotalRequests, "totalInputTokens": stats.TotalInputTokens,
		"totalOutputTokens": stats.TotalOutputTokens, "totalTokens": stats.TotalTokens,
		"totalActualCostUsdMicros": stats.TotalActualCostUSDMicros,
	})
}

func writeGatewayUserKeyError(w http.ResponseWriter, err error) {
	if errors.Is(err, clients.ErrSub2APIKeyNotFound) {
		writeError(w, http.StatusNotFound, "gateway_key_not_found")
		return
	}
	writeGatewaySourceError(w, err)
}

func writeGatewaySourceError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	if errors.Is(err, clients.ErrSub2APIWorkspaceKeyMissing) || errors.Is(err, clients.ErrSub2APIWorkspaceKeyAmbiguous) {
		status = http.StatusConflict
	}
	writeSourceEnvelope(w, status, "sub2api", "unavailable", nil)
}

func (app *controlPlaneServer) gatewaySub2APIUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	user, ok := app.sessionUserContext(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "not_authenticated")
		return 0, false
	}
	userID, err := app.sub2APIUserID(r.Context(), stringValue(user["accountId"]))
	if err != nil {
		writeSourceEnvelope(w, http.StatusInternalServerError, "sub2api", "unavailable", nil)
		return 0, false
	}
	return userID, true
}

func (app *controlPlaneServer) gatewayBalanceHistoryForUser(w http.ResponseWriter, r *http.Request, service *controlplane.Service, userID int64, pageNumber, pageSize int) {
	history, err := service.GatewayBalanceHistoryPage(r.Context(), userID, clients.Sub2APIBalanceHistoryPageQuery{Page: pageNumber, PageSize: pageSize})
	if err != nil {
		writeGatewaySourceError(w, err)
		return
	}
	items := make([]any, 0, len(history.Items))
	for _, entry := range history.Items {
		var usedAt any
		if entry.UsedAt != nil {
			usedAt = entry.UsedAt.UTC().Format(time.RFC3339Nano)
		}
		items = append(items, map[string]any{
			"type": entry.Type, "valueUsdMicros": strconv.FormatInt(entry.ValueUSDMicros, 10), "status": entry.Status,
			"usedAt": usedAt, "createdAt": entry.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	status := "available"
	if len(items) == 0 {
		status = "empty"
	}
	writeSourceEnvelope(w, http.StatusOK, "sub2api", status, map[string]any{"items": items, "total": history.Total, "page": history.Page, "pageSize": history.PageSize, "pages": history.Pages})
}
