package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
)

type sourceTruthGatewayClient struct {
	*customerFactsSub2API
	balanceErr error
	keys       []clients.Sub2APIWorkspaceKey
	keysErr    error
	userKeyErr error
	keyUserIDs []int64
}

func (*sourceTruthGatewayClient) PublicEndpoint() string { return "https://gateway.example.test/v1" }

func (c *sourceTruthGatewayClient) UserGroups(_ context.Context, credential clients.SessionDelegatedCredential, userID int64) ([]clients.Sub2APIGroup, error) {
	if credential.Bearer != "test-user-delegated-token" || userID != 41 {
		return nil, errors.New("wrong delegated credential")
	}
	return []clients.Sub2APIGroup{{ID: 7, Name: "Basic", Platform: "openai", RateMultiplier: 1, Status: "active"}, {ID: 11, Name: "Codex", Platform: "openai", RateMultiplier: 1, Status: "active"}}, nil
}

func (c *sourceTruthGatewayClient) Balance(ctx context.Context, userID int64) (clients.Sub2APIBalance, error) {
	if c.balanceErr != nil {
		return clients.Sub2APIBalance{}, c.balanceErr
	}
	return c.testSub2APIClient.Balance(ctx, userID)
}

func (c *sourceTruthGatewayClient) WorkspaceKeysForConvergence(_ context.Context, userID int64, name string) ([]clients.Sub2APIWorkspaceKey, error) {
	c.keyUserIDs = append(c.keyUserIDs, userID)
	keys := make([]clients.Sub2APIWorkspaceKey, 0, 1)
	for _, key := range c.keys {
		if key.UserID == userID && key.Name == name {
			keys = append(keys, key)
		}
	}
	return keys, c.keysErr
}

func (c *sourceTruthGatewayClient) WorkspaceUserKeysForConvergence(ctx context.Context, credential clients.SessionDelegatedCredential, userID int64, name string) ([]clients.Sub2APIWorkspaceKey, error) {
	if credential.Bearer != "test-user-delegated-token" {
		return nil, errors.New("wrong delegated credential")
	}
	return c.WorkspaceKeysForConvergence(ctx, userID, name)
}

func (c *sourceTruthGatewayClient) UserKeyPage(_ context.Context, credential clients.SessionDelegatedCredential, userID int64, query clients.Sub2APIKeyPageQuery) (clients.Sub2APIKeyPage, error) {
	if credential.Bearer != "test-user-delegated-token" {
		return clients.Sub2APIKeyPage{}, errors.New("wrong delegated credential")
	}
	if c.keysErr != nil {
		return clients.Sub2APIKeyPage{}, c.keysErr
	}
	keys := make([]clients.Sub2APIWorkspaceKey, 0, len(c.keys))
	for _, key := range c.keys {
		if key.UserID == userID {
			keys = append(keys, key)
		}
	}
	return clients.Sub2APIKeyPage{Items: keys, Total: len(keys), Page: query.Page, PageSize: query.PageSize, Pages: 1}, nil
}

func (c *sourceTruthGatewayClient) UserKey(_ context.Context, credential clients.SessionDelegatedCredential, userID, keyID int64) (clients.Sub2APIWorkspaceKey, error) {
	if credential.Bearer != "test-user-delegated-token" {
		return clients.Sub2APIWorkspaceKey{}, errors.New("wrong delegated credential")
	}
	c.keyUserIDs = append(c.keyUserIDs, userID)
	if c.userKeyErr != nil {
		return clients.Sub2APIWorkspaceKey{}, c.userKeyErr
	}
	for _, key := range c.keys {
		if key.ID == keyID && key.UserID == userID {
			return key, nil
		}
	}
	return clients.Sub2APIWorkspaceKey{}, clients.ErrSub2APIKeyNotFound
}

func decodeSourceEnvelope(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode source envelope: %v: %s", err, response.Body.String())
	}
	if envelope["source"] != "sub2api" || envelope["available"] != true {
		t.Fatalf("source envelope = %#v", envelope)
	}
	if _, err := time.Parse(time.RFC3339Nano, stringValue(envelope["fetchedAt"])); err != nil {
		t.Fatalf("fetchedAt = %#v: %v", envelope["fetchedAt"], err)
	}
	if _, exists := envelope["sourceUpdatedAt"]; exists {
		t.Fatalf("sourceUpdatedAt was fabricated: %#v", envelope)
	}
	return envelope
}

func assertUnavailableSourceEnvelope(t *testing.T, response *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("unavailable status = %d, want %d: %s", response.Code, wantStatus, response.Body.String())
	}
	var envelope map[string]any
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope) != 5 || envelope["source"] != "sub2api" || envelope["status"] != "unavailable" || envelope["available"] != false || envelope["reasonCode"] != "sub2api_unavailable" || envelope["data"] != nil {
		t.Fatalf("unavailable envelope = %#v", envelope)
	}
}

func TestGatewaySourceTruthRoutesUseSessionIdentityAndStrictEnvelopes(t *testing.T) {
	createdAt := time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC)
	lastUsedAt := createdAt.Add(-time.Hour)
	durationMS := int64(987)
	base := &customerFactsSub2API{
		testSub2APIClient: &testSub2APIClient{
			balance: 0, charges: map[string]int64{},
			workspaceKey: clients.Sub2APIWorkspaceKey{ID: 9, UserID: 41, Name: "opl-workspace", Key: "workspace-secret", Status: "active"},
		},
		usagePage: clients.Sub2APIUsagePage{
			Items: []clients.Sub2APIUsageRecord{{
				UserID: 41, APIKeyID: 9, RequestID: "request-1", CreatedAt: createdAt, Model: "gpt-5",
				InboundEndpoint: "/v1/responses", RequestType: "sync", InputTokens: 1, OutputTokens: 2,
				CacheCreationTokens: 3, CacheReadTokens: 4, ActualCostUSDMicros: 5,
				DurationMS: &durationMS, FirstTokenMS: nil,
			}},
			Total: 1, Page: 1, PageSize: 50, Pages: 1,
		},
		usageStats: clients.Sub2APIUsageStats{},
		history: map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {{
			Code: "adjustment-1", Type: "balance", ValueUSDMicros: -5, Status: "used", UsedAt: &createdAt, CreatedAt: createdAt,
		}}},
	}
	client := &sourceTruthGatewayClient{
		customerFactsSub2API: base,
		keys: []clients.Sub2APIWorkspaceKey{
			{ID: 8, UserID: 41, Name: "retired", Key: "must-not-leak-retired", Status: "disabled"},
			{ID: 9, UserID: 41, Name: "opl-workspace", Key: "must-not-leak-active", Status: "active", QuotaUSDMicros: 10, QuotaUsedUSDMicros: 2, Usage5hUSDMicros: 1, Usage1dUSDMicros: 2, Usage7dUSDMicros: 3, LastUsedAt: &lastUsedAt},
		},
	}
	server, session := newGatewayOwnerTestServer(t, client, nil)
	spoofed := "?accountId=acct-other&user_id=999&api_key_id=999&sub2apiUserId=999"

	wallet := requestWithSession(t, server, session, http.MethodGet, "/api/gateway/wallet"+spoofed, "")
	if wallet.Code != http.StatusOK {
		t.Fatalf("wallet = %d: %s", wallet.Code, wallet.Body.String())
	}
	walletEnvelope := decodeSourceEnvelope(t, wallet)
	if walletEnvelope["status"] != "available" {
		t.Fatalf("zero wallet is not available: %#v", walletEnvelope)
	}
	walletData := mapField(walletEnvelope, "data")
	if len(walletData) != 4 || walletData["userId"] != "41" || walletData["currency"] != "USD" || walletData["usdMicros"] != "0" || walletData["status"] != "active" {
		t.Fatalf("wallet data = %#v", walletData)
	}

	keysResponse := requestWithSession(t, server, session, http.MethodGet, "/api/gateway/keys"+spoofed, "")
	if keysResponse.Code != http.StatusOK || strings.Contains(keysResponse.Body.String(), "must-not-leak") {
		t.Fatalf("keys = %d: %s", keysResponse.Code, keysResponse.Body.String())
	}
	keysEnvelope := decodeSourceEnvelope(t, keysResponse)
	keysData := mapField(keysEnvelope, "data")
	keyItems, _ := keysData["items"].([]any)
	if keysEnvelope["status"] != "available" || len(keyItems) != 2 || keysData["total"] != float64(2) {
		t.Fatalf("keys envelope = %#v", keysEnvelope)
	}
	activeKey := keyItems[1].(map[string]any)
	if len(activeKey) != 23 || activeKey["id"] != "9" || activeKey["status"] != "active" || activeKey["quotaUsdMicros"] != float64(10) ||
		activeKey["kind"] != "workspace" || activeKey["manageable"] != false || activeKey["deletable"] != false || activeKey["expiresAt"] != nil {
		t.Fatalf("active key = %#v", activeKey)
	}

	usage := requestWithSession(t, server, session, http.MethodGet, "/api/gateway/keys/9/usage"+spoofed+"&page=1&pageSize=50&period=week", "")
	if usage.Code != http.StatusOK {
		t.Fatalf("usage = %d: %s", usage.Code, usage.Body.String())
	}
	usageEnvelope := decodeSourceEnvelope(t, usage)
	usageItems, _ := mapField(usageEnvelope, "data")["items"].([]any)
	usageItem := usageItems[0].(map[string]any)
	if len(usageItems) != 1 || len(usageItem) != 13 || usageItem["apiKeyId"] != "9" || usageItem["durationMs"] != float64(987) || usageItem["firstTokenMs"] != nil {
		t.Fatalf("usage envelope = %#v", usageEnvelope)
	}
	for _, forbidden := range []string{"duration_ms", "first_token_ms", "prompt", "response"} {
		if _, exists := usageItem[forbidden]; exists {
			t.Fatalf("usage item exposed %q: %#v", forbidden, usageItem)
		}
	}

	stats := requestWithSession(t, server, session, http.MethodGet, "/api/gateway/keys/9/usage-summary"+spoofed+"&period=month", "")
	if stats.Code != http.StatusOK {
		t.Fatalf("stats = %d: %s", stats.Code, stats.Body.String())
	}
	statsEnvelope := decodeSourceEnvelope(t, stats)
	if statsEnvelope["status"] != "available" || numberField(mapField(statsEnvelope, "data"), "totalRequests", -1) != 0 {
		t.Fatalf("zero stats = %#v", statsEnvelope)
	}

	history := requestWithSession(t, server, session, http.MethodGet, "/api/gateway/balance-history"+spoofed, "")
	if history.Code != http.StatusOK || strings.Contains(history.Body.String(), "adjustment-1") || strings.Contains(history.Body.String(), "usedBy") {
		t.Fatalf("history = %d: %s", history.Code, history.Body.String())
	}
	historyEnvelope := decodeSourceEnvelope(t, history)
	historyData := mapField(historyEnvelope, "data")
	historyItems, _ := historyData["items"].([]any)
	if len(historyItems) != 1 || len(historyItems[0].(map[string]any)) != 5 || historyItems[0].(map[string]any)["valueUsdMicros"] != "-5" || historyData["total"] != float64(1) || historyData["page"] != float64(1) || historyData["pageSize"] != float64(20) || historyData["pages"] != float64(1) {
		t.Fatalf("history envelope = %#v", historyEnvelope)
	}

	if len(client.keyUserIDs) != 2 || client.keyUserIDs[0] != 41 || client.keyUserIDs[1] != 41 || base.usageQuery != (clients.Sub2APIUsageQuery{UserID: 41, APIKeyID: 9, Page: 1, PageSize: 50, Period: "week"}) || base.statsQuery.UserID != 41 || base.statsQuery.APIKeyID != 9 || len(base.historyIDs) != 1 || base.historyIDs[0] != 41 || !reflect.DeepEqual(base.historyPageQueries, []clients.Sub2APIBalanceHistoryPageQuery{{Page: 1, PageSize: 20}}) {
		t.Fatalf("session identity was not authoritative: keys=%#v usage=%#v stats=%#v history=%#v", client.keyUserIDs, base.usageQuery, base.statsQuery, base.historyIDs)
	}
}

func TestGatewayBalanceHistoryUsesOnlyRequestedPage(t *testing.T) {
	createdAt := time.Date(2026, 7, 18, 1, 2, 3, 0, time.UTC)
	rows := make([]clients.Sub2APIBalanceHistoryEntry, 41)
	for index := range rows {
		rows[index] = clients.Sub2APIBalanceHistoryEntry{Code: fmt.Sprintf("opl:history:%d", index), Type: "balance", ValueUSDMicros: int64(index), Status: "used", UsedAt: &createdAt, CreatedAt: createdAt}
	}
	client := &sourceTruthGatewayClient{customerFactsSub2API: &customerFactsSub2API{
		testSub2APIClient: &testSub2APIClient{charges: map[string]int64{}},
		history:           map[int64][]clients.Sub2APIBalanceHistoryEntry{41: rows},
	}}
	server, session := newGatewayOwnerTestServer(t, client, nil)
	response := requestWithSession(t, server, session, http.MethodGet, "/api/gateway/balance-history?page=3&pageSize=20", "")
	if response.Code != http.StatusOK {
		t.Fatalf("history page = %d: %s", response.Code, response.Body.String())
	}
	data := mapField(decodeSourceEnvelope(t, response), "data")
	items := data["items"].([]any)
	if len(items) != 1 || data["total"] != float64(41) || data["page"] != float64(3) || data["pageSize"] != float64(20) || data["pages"] != float64(3) || items[0].(map[string]any)["valueUsdMicros"] != "40" {
		t.Fatalf("history page data = %#v", data)
	}
	if !reflect.DeepEqual(client.historyPageQueries, []clients.Sub2APIBalanceHistoryPageQuery{{Page: 3, PageSize: 20}}) {
		t.Fatalf("history page queries = %#v", client.historyPageQueries)
	}
}

func TestGatewayBalanceAndHistoryUseLosslessDecimalStrings(t *testing.T) {
	createdAt := time.Date(2026, 7, 25, 1, 2, 3, 0, time.UTC)
	client := &sourceTruthGatewayClient{customerFactsSub2API: &customerFactsSub2API{
		testSub2APIClient: &testSub2APIClient{balance: math.MaxInt64, charges: map[string]int64{}},
		history: map[int64][]clients.Sub2APIBalanceHistoryEntry{41: {{
			Code: "adjustment-max", Type: "balance", ValueUSDMicros: math.MinInt64,
			Status: "used", UsedAt: &createdAt, CreatedAt: createdAt,
		}}},
	}}
	server, session := newGatewayOwnerTestServer(t, client, nil)

	wallet := requestWithSession(t, server, session, http.MethodGet, "/api/gateway/wallet", "")
	if wallet.Code != http.StatusOK || mapField(decodeSourceEnvelope(t, wallet), "data")["usdMicros"] != "9223372036854775807" {
		t.Fatalf("lossless wallet = %d: %s", wallet.Code, wallet.Body.String())
	}
	history := requestWithSession(t, server, session, http.MethodGet, "/api/gateway/balance-history", "")
	if history.Code != http.StatusOK {
		t.Fatalf("history = %d: %s", history.Code, history.Body.String())
	}
	items := mapField(decodeSourceEnvelope(t, history), "data")["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["valueUsdMicros"] != "-9223372036854775808" {
		t.Fatalf("lossless history = %s", history.Body.String())
	}
}

func TestGatewaySourceTruthEmptyAndUnavailableAreNotFabricated(t *testing.T) {
	baseClient := func() *sourceTruthGatewayClient {
		return &sourceTruthGatewayClient{customerFactsSub2API: &customerFactsSub2API{
			testSub2APIClient: &testSub2APIClient{charges: map[string]int64{}, workspaceKey: clients.Sub2APIWorkspaceKey{ID: 9, UserID: 41, Name: "opl-workspace", Key: "workspace-secret", Status: "active"}},
			usagePage:         clients.Sub2APIUsagePage{Page: 1, PageSize: 50, Pages: 1},
			history:           map[int64][]clients.Sub2APIBalanceHistoryEntry{},
		}}
	}

	for _, tc := range []struct {
		path   string
		mutate func(*sourceTruthGatewayClient)
	}{
		{path: "/api/gateway/keys"},
		{path: "/api/gateway/keys/9/usage", mutate: func(c *sourceTruthGatewayClient) {
			c.keys = []clients.Sub2APIWorkspaceKey{{ID: 9, UserID: 41, Name: "general", Status: "active"}}
		}},
		{path: "/api/gateway/balance-history"},
	} {
		t.Run("empty "+tc.path, func(t *testing.T) {
			client := baseClient()
			if tc.mutate != nil {
				tc.mutate(client)
			}
			server, session := newGatewayOwnerTestServer(t, client, nil)
			response := requestWithSession(t, server, session, http.MethodGet, tc.path, "")
			if response.Code != http.StatusOK {
				t.Fatalf("empty status = %d: %s", response.Code, response.Body.String())
			}
			envelope := decodeSourceEnvelope(t, response)
			if envelope["status"] != "empty" {
				t.Fatalf("empty envelope = %#v", envelope)
			}
		})
	}

	for _, tc := range []struct {
		name, path string
		mutate     func(*sourceTruthGatewayClient)
	}{
		{name: "wallet", path: "/api/gateway/wallet", mutate: func(c *sourceTruthGatewayClient) { c.balanceErr = errors.New("wallet unavailable") }},
		{name: "keys", path: "/api/gateway/keys", mutate: func(c *sourceTruthGatewayClient) { c.keysErr = errors.New("keys unavailable") }},
		{name: "usage", path: "/api/gateway/keys/9/usage", mutate: func(c *sourceTruthGatewayClient) {
			c.keys = []clients.Sub2APIWorkspaceKey{{ID: 9, UserID: 41, Name: "general", Status: "active"}}
			c.usageErr = errors.New("usage unavailable")
		}},
		{name: "usage pagination", path: "/api/gateway/keys/9/usage", mutate: func(c *sourceTruthGatewayClient) {
			c.keys = []clients.Sub2APIWorkspaceKey{{ID: 9, UserID: 41, Name: "general", Status: "active"}}
			c.usageErr = errors.New("invalid sub2api usage pagination")
		}},
		{name: "stats", path: "/api/gateway/keys/9/usage-summary", mutate: func(c *sourceTruthGatewayClient) {
			c.keys = []clients.Sub2APIWorkspaceKey{{ID: 9, UserID: 41, Name: "general", Status: "active"}}
			c.statsErr = errors.New("stats unavailable")
		}},
		{name: "history", path: "/api/gateway/balance-history", mutate: func(c *sourceTruthGatewayClient) { c.historyErr = errors.New("history unavailable") }},
	} {
		t.Run("unavailable "+tc.name, func(t *testing.T) {
			client := baseClient()
			tc.mutate(client)
			server, session := newGatewayOwnerTestServer(t, client, nil)
			response := requestWithSession(t, server, session, http.MethodGet, tc.path, "")
			if response.Code != http.StatusBadGateway {
				t.Fatalf("unavailable status = %d: %s", response.Code, response.Body.String())
			}
			var envelope map[string]any
			if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
				t.Fatal(err)
			}
			if len(envelope) != 5 || envelope["source"] != "sub2api" || envelope["status"] != "unavailable" || envelope["available"] != false || envelope["reasonCode"] != "sub2api_unavailable" || envelope["data"] != nil {
				t.Fatalf("unavailable envelope = %#v", envelope)
			}
			if _, err := time.Parse(time.RFC3339Nano, stringValue(envelope["fetchedAt"])); err != nil {
				t.Fatalf("unavailable fetchedAt = %#v", envelope["fetchedAt"])
			}
		})
	}
}

func TestGatewayRevealIsStrictSub2APISource(t *testing.T) {
	client := &sourceTruthGatewayClient{
		customerFactsSub2API: &customerFactsSub2API{testSub2APIClient: &testSub2APIClient{charges: map[string]int64{}}},
		keys:                 []clients.Sub2APIWorkspaceKey{{ID: 9, UserID: 41, Name: "opl-workspace", Key: "workspace-secret", Status: "active"}},
	}
	server, session := newGatewayOwnerTestServer(t, client, nil)
	response := requestWithSession(t, server, session, http.MethodPost, "/api/gateway/keys/9/reveal?accountId=acct-other&sub2apiUserId=999", "{}")
	if response.Code != http.StatusOK {
		t.Fatalf("reveal = %d: %s", response.Code, response.Body.String())
	}
	envelope := decodeSourceEnvelope(t, response)
	data := mapField(envelope, "data")
	if envelope["status"] != "available" || len(data) != 4 || data["id"] != "9" || data["name"] != "opl-workspace" || data["status"] != "active" || data["value"] != "workspace-secret" {
		t.Fatalf("reveal envelope = %#v", envelope)
	}
	if response.Header().Get("Cache-Control") != "private, no-store" || len(client.keyUserIDs) != 1 || client.keyUserIDs[0] != 41 {
		t.Fatalf("reveal boundary cache=%q users=%#v", response.Header().Get("Cache-Control"), client.keyUserIDs)
	}
}
