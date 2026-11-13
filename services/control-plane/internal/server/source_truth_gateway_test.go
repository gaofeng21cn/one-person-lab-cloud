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
	"sync"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
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

type workspaceKeyRotationClient struct {
	*customerFactsSub2API
	mu              sync.Mutex
	keys            map[int64]clients.Sub2APIWorkspaceKey
	failAfter       string
	failed          bool
	createWrites    int
	updateWrites    int
	deleteWrites    int
	createInputs    []clients.Sub2APICreateKeyInput
	updateStages    []string
	onDisable       func(*clients.Sub2APIWorkspaceKey)
	createStarted   chan struct{}
	releaseCreate   chan struct{}
	createStartOnce sync.Once
}

func (c *workspaceKeyRotationClient) keyList(userID int64) []clients.Sub2APIWorkspaceKey {
	keys := make([]clients.Sub2APIWorkspaceKey, 0, len(c.keys))
	for _, key := range c.keys {
		if key.UserID == userID {
			keys = append(keys, key)
		}
	}
	return keys
}

func (c *workspaceKeyRotationClient) WorkspaceKeysForConvergence(_ context.Context, userID int64, name string) ([]clients.Sub2APIWorkspaceKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := c.keyList(userID)
	matches := make([]clients.Sub2APIWorkspaceKey, 0, 1)
	for _, key := range keys {
		if key.Name == name {
			matches = append(matches, key)
		}
	}
	return matches, nil
}

func (c *workspaceKeyRotationClient) WorkspaceUserKeysForConvergence(_ context.Context, credential clients.SessionDelegatedCredential, userID int64, name string) ([]clients.Sub2APIWorkspaceKey, error) {
	if credential.Bearer != "test-user-delegated-token" {
		return nil, errors.New("wrong delegated credential")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := c.keyList(userID)
	matches := make([]clients.Sub2APIWorkspaceKey, 0, 1)
	for _, key := range keys {
		if key.Name == name {
			matches = append(matches, key)
		}
	}
	return matches, nil
}

func (c *workspaceKeyRotationClient) UserKey(_ context.Context, credential clients.SessionDelegatedCredential, userID, keyID int64) (clients.Sub2APIWorkspaceKey, error) {
	if credential.Bearer != "test-user-delegated-token" {
		return clients.Sub2APIWorkspaceKey{}, errors.New("wrong delegated credential")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key, ok := c.keys[keyID]
	if !ok || key.UserID != userID {
		return clients.Sub2APIWorkspaceKey{}, clients.ErrSub2APIKeyNotFound
	}
	if keyID != 9 && c.fail("policy_readback") {
		return clients.Sub2APIWorkspaceKey{}, errors.New("replacement policy readback unavailable")
	}
	return key, nil
}

func (c *workspaceKeyRotationClient) WorkspaceKey(_ context.Context, userID int64) (clients.Sub2APIWorkspaceKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var match clients.Sub2APIWorkspaceKey
	for _, key := range c.keys {
		if key.UserID == userID && key.Name == "opl-workspace" && key.Status == "active" {
			if match.ID != 0 {
				return clients.Sub2APIWorkspaceKey{}, clients.ErrSub2APIWorkspaceKeyAmbiguous
			}
			match = key
		}
	}
	if match.ID == 0 {
		return clients.Sub2APIWorkspaceKey{}, clients.ErrSub2APIWorkspaceKeyMissing
	}
	return match, nil
}

func (c *workspaceKeyRotationClient) fail(stage string) bool {
	if c.failAfter == stage && !c.failed {
		c.failed = true
		return true
	}
	return false
}

func (c *workspaceKeyRotationClient) UserGroups(_ context.Context, credential clients.SessionDelegatedCredential, userID int64) ([]clients.Sub2APIGroup, error) {
	if credential.Bearer != "test-user-delegated-token" || userID != 41 {
		return nil, errors.New("wrong delegated group identity")
	}
	return []clients.Sub2APIGroup{{ID: 11, Name: "Codex", Platform: "openai", RateMultiplier: 1, Status: "active"}}, nil
}

func (c *workspaceKeyRotationClient) CreateUserKey(_ context.Context, credential clients.SessionDelegatedCredential, userID int64, input clients.Sub2APICreateKeyInput, idempotencyKey string) (clients.Sub2APIWorkspaceKey, error) {
	if credential.Bearer != "test-user-delegated-token" || userID != 41 || input.GroupID != 11 || idempotencyKey == "" || !strings.HasPrefix(input.Name, "opl-workspace-replacement-") {
		return clients.Sub2APIWorkspaceKey{}, errors.New("invalid replacement create")
	}
	if c.createStarted != nil {
		c.createStartOnce.Do(func() { close(c.createStarted) })
		<-c.releaseCreate
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, key := range c.keys {
		if key.UserID == userID && key.Name == input.Name {
			return key, nil
		}
	}
	keyID := int64(19)
	for {
		if _, exists := c.keys[keyID]; !exists {
			break
		}
		keyID++
	}
	groupID := input.GroupID
	key := clients.Sub2APIWorkspaceKey{
		ID: keyID, UserID: userID, Name: input.Name, Key: "replacement-workspace-key-secret", GroupID: &groupID, Status: "active",
		QuotaUSDMicros: input.QuotaUSDMicros, RateLimit5hUSDMicros: input.RateLimit5hUSDMicros,
		RateLimit1dUSDMicros: input.RateLimit1dUSDMicros, RateLimit7dUSDMicros: input.RateLimit7dUSDMicros,
	}
	c.keys[key.ID] = key
	c.createInputs = append(c.createInputs, input)
	c.createWrites++
	if c.fail("create") {
		return clients.Sub2APIWorkspaceKey{}, errors.New("create response lost")
	}
	return key, nil
}

func (c *workspaceKeyRotationClient) UpdateUserKey(_ context.Context, credential clients.SessionDelegatedCredential, userID, keyID int64, input clients.Sub2APIUpdateKeyInput) (clients.Sub2APIWorkspaceKey, error) {
	if credential.Bearer != "test-user-delegated-token" {
		return clients.Sub2APIWorkspaceKey{}, errors.New("wrong delegated credential")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key, ok := c.keys[keyID]
	if !ok || key.UserID != userID || input.Name == nil && input.Enabled == nil {
		return clients.Sub2APIWorkspaceKey{}, clients.ErrSub2APIKeyNotFound
	}
	stage := "disable"
	if input.Name != nil {
		key.Name = *input.Name
		if strings.HasPrefix(*input.Name, "opl-workspace-retired-") {
			stage = "retire"
		} else {
			stage = "promote"
		}
	}
	if input.Enabled != nil {
		key.Status = "disabled"
		if *input.Enabled {
			key.Status = "active"
		}
	}
	if stage == "disable" && c.onDisable != nil {
		c.onDisable(&key)
	}
	c.keys[keyID] = key
	c.updateStages = append(c.updateStages, stage)
	c.updateWrites++
	if c.fail(stage) {
		return clients.Sub2APIWorkspaceKey{}, errors.New(stage + " response lost")
	}
	return key, nil
}

func (c *workspaceKeyRotationClient) DeleteUserKey(_ context.Context, credential clients.SessionDelegatedCredential, userID, keyID int64) error {
	if credential.Bearer != "test-user-delegated-token" {
		return errors.New("wrong delegated credential")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	key, ok := c.keys[keyID]
	if !ok || key.UserID != userID {
		return clients.ErrSub2APIKeyNotFound
	}
	delete(c.keys, keyID)
	c.deleteWrites++
	if c.fail("delete") {
		return errors.New("delete response lost")
	}
	return nil
}

type workspaceKeyRotationFabric struct {
	fakeFabricClient
	failAfter string
	failed    bool
	bindings  []clients.WorkspaceRuntimeGatewaySecretInput
	onBind    func()
}

func (f *workspaceKeyRotationFabric) WriteGatewaySecret(ctx context.Context, input clients.GatewaySecretWriteInput, key string) (clients.GatewaySecretWriteResult, error) {
	result, err := f.fakeFabricClient.WriteGatewaySecret(ctx, input, key)
	if err == nil && f.failAfter == "secret" && !f.failed {
		f.failed = true
		return clients.GatewaySecretWriteResult{}, errors.New("Secret response lost")
	}
	return result, err
}

func (f *workspaceKeyRotationFabric) BindWorkspaceRuntimeGatewaySecret(_ context.Context, input clients.WorkspaceRuntimeGatewaySecretInput, _ string) (clients.WorkspaceRuntimeGatewaySecretBinding, error) {
	result := clients.WorkspaceRuntimeGatewaySecretBinding{
		WorkspaceID: input.WorkspaceID, WorkspaceAPIKeyID: input.WorkspaceAPIKeyID,
		SecretRef: input.SecretRef, Fingerprint: input.Fingerprint, Bound: true,
	}
	if len(f.bindings) > 0 && f.bindings[len(f.bindings)-1] == input {
		return result, nil
	}
	f.bindings = append(f.bindings, input)
	if f.onBind != nil {
		f.onBind()
	}
	if f.failAfter == "bind" && !f.failed {
		f.failed = true
		return clients.WorkspaceRuntimeGatewaySecretBinding{}, errors.New("Runtime bind response lost")
	}
	return result, nil
}

func (f *workspaceKeyRotationFabric) WorkspaceRuntimeGatewaySecret(_ context.Context, workspaceID string) (clients.WorkspaceRuntimeGatewaySecretBinding, error) {
	if f.failAfter == "readback" && !f.failed {
		f.failed = true
		return clients.WorkspaceRuntimeGatewaySecretBinding{}, errors.New("Runtime readback unavailable")
	}
	if len(f.bindings) == 0 {
		return clients.WorkspaceRuntimeGatewaySecretBinding{WorkspaceID: workspaceID}, nil
	}
	input := f.bindings[len(f.bindings)-1]
	return clients.WorkspaceRuntimeGatewaySecretBinding{WorkspaceID: workspaceID, WorkspaceAPIKeyID: input.WorkspaceAPIKeyID, SecretRef: input.SecretRef, Fingerprint: input.Fingerprint, Bound: true}, nil
}

type workspaceKeyRotationLedger struct {
	fakeLedgerClient
	mu        sync.Mutex
	receipts  map[string]clients.Receipt
	inputs    []clients.ReceiptInput
	failAfter string
	failed    bool
}

func (l *workspaceKeyRotationLedger) RecordReceipt(_ context.Context, input clients.ReceiptInput, key string) (clients.Receipt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if receipt, ok := l.receipts[key]; ok {
		return receipt, nil
	}
	receipt := clients.Receipt{ReceiptInput: input, ReceiptID: "receipt-" + stableID(key)[:12]}
	l.receipts[key] = receipt
	l.inputs = append(l.inputs, input)
	if l.failAfter == "receipt" && !l.failed {
		l.failed = true
		return clients.Receipt{}, errors.New("Receipt response lost")
	}
	return receipt, nil
}

type workspaceKeyRotationStore struct {
	*memoryTableStore
	failPhase             string
	failPersistedPhase    string
	failCASAfterCommit    bool
	failed                bool
	persistedResponseLost bool
	casResponseLost       bool
}

func (s *workspaceKeyRotationStore) SaveRuntimeOperation(ctx context.Context, row map[string]any) error {
	var phase string
	if stringValue(row["action"]) == "workspace.gateway_key.rotate" && !s.failed && s.failPhase != "" {
		var result map[string]any
		if json.Unmarshal([]byte(stringValue(row["result"])), &result) == nil {
			phase = stringValue(result["phase"])
		}
		if phase == s.failPhase {
			s.failed = true
			return errors.New("phase persist failed")
		}
	}
	if err := s.memoryTableStore.SaveRuntimeOperation(ctx, row); err != nil {
		return err
	}
	if phase == "" && stringValue(row["action"]) == "workspace.gateway_key.rotate" {
		var result map[string]any
		if json.Unmarshal([]byte(stringValue(row["result"])), &result) == nil {
			phase = stringValue(result["phase"])
		}
	}
	if !s.persistedResponseLost && s.failPersistedPhase != "" && phase == s.failPersistedPhase {
		s.persistedResponseLost = true
		return errors.New("phase persist response lost")
	}
	return nil
}

func (s *workspaceKeyRotationStore) CompareAndSwapWorkspaceAPIKey(ctx context.Context, workspaceID string, expectedID, newID int64) error {
	if err := s.memoryTableStore.CompareAndSwapWorkspaceAPIKey(ctx, workspaceID, expectedID, newID); err != nil {
		return err
	}
	if s.failCASAfterCommit && !s.casResponseLost {
		s.casResponseLost = true
		return errors.New("Workspace Key CAS response lost")
	}
	return nil
}

type workspaceKeyRotationFixture struct {
	server  http.Handler
	store   *workspaceKeyRotationStore
	session *httptest.ResponseRecorder
	email   string
	client  *workspaceKeyRotationClient
	fabric  *workspaceKeyRotationFabric
	ledger  *workspaceKeyRotationLedger
	service *controlplane.Service
}

func newWorkspaceKeyRotationFixture(t *testing.T, failAfter string) workspaceKeyRotationFixture {
	t.Helper()
	t.Setenv("OPL_MONTHLY_BILLING_WORKER_ENABLED", "false")
	t.Setenv("OPL_PROVIDER_RECONCILE_WORKER_ENABLED", "false")
	t.Setenv("OPL_ARCHIVE_RETENTION_WORKER_ENABLED", "false")
	store := &workspaceKeyRotationStore{memoryTableStore: newMemoryTableStore()}
	base := &customerFactsSub2API{testSub2APIClient: &testSub2APIClient{balance: 1_000_000_000, charges: map[string]int64{}}}
	client := &workspaceKeyRotationClient{customerFactsSub2API: base, keys: map[int64]clients.Sub2APIWorkspaceKey{
		9: {ID: 9, UserID: 41, Name: "opl-workspace", Key: "old-workspace-key-secret", Status: "active"},
	}, failAfter: failAfter}
	fabric := &workspaceKeyRotationFabric{fakeFabricClient: fakeFabricClient{
		gatewaySecret: clients.GatewaySecretWriteResult{SecretRef: "opl-gateway-ws-alpha", Version: "v2", Fingerprint: "sha256:f346c41dc52c526411868e85de9cda4bb694fe16f71d8c68823c93bf0bf21654"},
		runtimeStatus: clients.WorkspaceRuntime{ID: "runtime-alpha", WorkspaceID: "ws-alpha", Status: "running", ServiceName: "opl-compute-alpha", Ready: true, Access: clients.WorkspaceRuntimeAccess{
			Username: "opl", Password: "runtime-password-before", CredentialStatus: "configured", CredentialVersion: "v-before", SecretRef: "opl-compute-alpha-env",
		}},
	}, failAfter: failAfter}
	ledger := &workspaceKeyRotationLedger{receipts: map[string]clients.Receipt{}, failAfter: failAfter}
	service := controlplane.NewService(ledger, fabric, client)
	server, err := NewPersistentServer(service, store)
	if err != nil {
		t.Fatal(err)
	}
	session := tenantOwnerSessionForTest(t, server)
	ownerID := sessionUserIDForTest(t, server, session)
	users, _ := store.ListUsers(context.Background(), false)
	email := stringValue(findRecord(users, ownerID)["email"])
	seedRuntimeAccessWorkspaceForTest(t, store, ownerID, map[string]any{"workspaceApiKeyId": int64(9)})
	return workspaceKeyRotationFixture{server: server, store: store, session: session, email: email, client: client, fabric: fabric, ledger: ledger, service: service}
}

func (f workspaceKeyRotationFixture) accept(t *testing.T, key string) {
	t.Helper()
	ctx := context.Background()
	workspace, found, err := f.store.GetWorkspace(ctx, "ws-alpha")
	if err != nil || !found {
		t.Fatalf("accepted rotation Workspace found=%v err=%v", found, err)
	}
	workspaceID := stringValue(workspace["id"])
	accountID := firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"]))
	ownerID := firstNonEmpty(stringValue(workspace["ownerUserId"]), stringValue(workspace["ownerId"]))
	oldKeyID, valid := positiveIntegerField(workspace, "workspaceApiKeyId")
	if !valid {
		t.Fatal("accepted rotation requires the original Workspace Key")
	}
	users, err := f.store.ListUsers(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	owner := findRecord(users, ownerID)
	if owner == nil {
		t.Fatal("accepted rotation requires its original owner")
	}
	operationID := "workspace-key-rotate-" + stableID(workspaceID, key)[:18]
	if _, found, err := f.store.GetRuntimeOperation(ctx, operationID); err != nil || found {
		t.Fatalf("accepted rotation already exists or cannot be read: found=%v err=%v", found, err)
	}
	operation := workspaceKeyRotationOperation{
		RequestHash:     stableID("workspace-key-rotation-v1", workspaceID, string(mustJSON(struct{}{}))),
		Phase:           "replacement_check",
		OldKeyID:        oldKeyID,
		ReplacementName: "opl-workspace-replacement-" + stableID(operationID)[:12],
		RetiredName:     "opl-workspace-retired-" + stableID(operationID)[:12],
		AuditEvent: map[string]any{
			"id":              "audit-" + stableID(operationID, "workspace.gateway_key.rotate")[:12],
			"actorUserId":     ownerID,
			"actorRole":       stringValue(owner["role"]),
			"actorAccountId":  accountID,
			"targetAccountId": accountID,
			"action":          "workspace.gateway_key.rotate",
			"resourceKind":    "workspace_gateway_key",
			"resourceId":      workspaceID,
			"ipAddress":       "198.51.100.8",
			"userAgent":       "original-agent",
			"before":          map[string]any{"workspaceApiKeyId": fmt.Sprintf("%d", oldKeyID)},
			"after":           map[string]any{"operationId": operationID},
			"result":          "started",
			"createdAt":       "2026-08-16T00:00:00Z",
		},
	}
	// Seed a previously accepted command without passing through recovery fault injection.
	mustStore(t, f.store.memoryTableStore.SaveRuntimeOperation(ctx, workspaceKeyRotationRow(operationID, accountID, workspaceID, "started", operation)))
}

func (f workspaceKeyRotationFixture) rotate(t *testing.T, key string) *httptest.ResponseRecorder {
	t.Helper()
	return requestWithMutationKeyForTest(t, f.server, f.session, http.MethodPost, "/api/workspaces/ws-alpha/workspace-key/rotate", `{}`, key)
}

func (f workspaceKeyRotationFixture) rotateWithMetadata(t *testing.T, key, userAgent, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/workspaces/ws-alpha/workspace-key/rotate", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	request.Header.Set("User-Agent", userAgent)
	request.RemoteAddr = remoteAddr
	addAuth(request, f.session)
	response := httptest.NewRecorder()
	f.server.ServeHTTP(response, request)
	return response
}

func (f *workspaceKeyRotationFixture) restart(t *testing.T) {
	t.Helper()
	server, err := NewPersistentServer(f.service, f.store)
	if err != nil {
		t.Fatal(err)
	}
	f.server = server
	f.session = loginForTest(t, server, f.email, "CorrectHorseBatteryStaple!")
}

func assertWorkspaceKeyRotationComplete(t *testing.T, fixture workspaceKeyRotationFixture, response *httptest.ResponseRecorder) {
	t.Helper()
	if response.Code != http.StatusOK {
		t.Fatalf("rotation status=%d body=%s", response.Code, response.Body.String())
	}
	fixture.client.mu.Lock()
	keys := fixture.client.keyList(41)
	fixture.client.mu.Unlock()
	workspaces, _ := fixture.store.ListWorkspaces(context.Background(), "acct-alpha")
	if len(keys) != 1 || keys[0].ID != 19 || keys[0].Name != workspaceReservedKeyName("ws-alpha") || keys[0].Status != "active" || len(workspaces) != 1 || int64(numberField(workspaces[0], "workspaceApiKeyId", 0)) != 19 || len(fixture.ledger.receipts) != 1 {
		t.Fatalf("rotation did not converge: keys=%#v Workspaces=%#v receipts=%#v", keys, workspaces, fixture.ledger.receipts)
	}
	if len(fixture.fabric.bindings) != 1 || fixture.fabric.bindings[0].WorkspaceID != "ws-alpha" || fixture.fabric.bindings[0].SecretRef != "opl-gateway-ws-alpha" ||
		fixture.fabric.bindings[0].WorkspaceAPIKeyID != 19 || fixture.fabric.bindings[0].Fingerprint != "sha256:f346c41dc52c526411868e85de9cda4bb694fe16f71d8c68823c93bf0bf21654" || len(fixture.fabric.runtimeInputs) != 0 {
		t.Fatalf("runtime Gateway binding=%#v runtime applies=%#v", fixture.fabric.bindings, fixture.fabric.runtimeInputs)
	}
	if access := fixture.fabric.runtimeStatus.Access; access.Username != "opl" || access.Password != "runtime-password-before" || access.CredentialVersion != "v-before" || access.SecretRef != "opl-compute-alpha-env" {
		t.Fatalf("Key rotation changed Runtime credentials: %#v", access)
	}
	if !strings.Contains(response.Body.String(), `"workspaceApiKeyId":"19"`) || !strings.Contains(response.Body.String(), `"fingerprint":"sha256:f346c41dc52c526411868e85de9cda4bb694fe16f71d8c68823c93bf0bf21654"`) {
		t.Fatalf("rotation DTO=%s", response.Body.String())
	}
}
