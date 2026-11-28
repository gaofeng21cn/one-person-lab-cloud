package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"opl-cloud/services/control-plane/internal/clients"
)

type sourceTruthIdentityClient struct {
	*testSub2APIClient
	mu              sync.Mutex
	users           map[int64]clients.Sub2APIIdentity
	balances        map[int64]int64
	userErrs        map[int64]error
	userIDs         []int64
	adminUserIDs    []int64
	batchUsageCalls int
}

func (c *sourceTruthIdentityClient) User(_ context.Context, userID int64) (clients.Sub2APIIdentity, error) {
	c.userIDs = append(c.userIDs, userID)
	if err := c.userErrs[userID]; err != nil {
		return clients.Sub2APIIdentity{}, err
	}
	identity, ok := c.users[userID]
	if !ok {
		return clients.Sub2APIIdentity{}, errors.New("identity unavailable")
	}
	return identity, nil
}

func (c *sourceTruthIdentityClient) AdminUser(_ context.Context, userID int64) (clients.Sub2APIUser, error) {
	c.mu.Lock()
	c.adminUserIDs = append(c.adminUserIDs, userID)
	c.mu.Unlock()
	if err := c.userErrs[userID]; err != nil {
		return clients.Sub2APIUser{}, err
	}
	identity, ok := c.users[userID]
	if !ok {
		return clients.Sub2APIUser{}, errors.New("identity unavailable")
	}
	return clients.Sub2APIUser{
		ID: identity.ID, Email: identity.Email, BalanceUSDMicros: c.balances[userID], Status: identity.Status,
		CreatedAt: time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 7, 19, 0, 0, 0, 0, time.UTC),
	}, nil
}

func (c *sourceTruthIdentityClient) BatchUsersUsage(_ context.Context, userIDs []int64) (map[int64]clients.Sub2APIBatchUserUsage, error) {
	c.batchUsageCalls++
	result := make(map[int64]clients.Sub2APIBatchUserUsage, len(userIDs))
	for _, id := range userIDs {
		result[id] = clients.Sub2APIBatchUserUsage{UserID: id}
	}
	return result, nil
}

func TestAuthMeUsesOnlySessionIdentityAndLiveSub2APIUser(t *testing.T) {
	client := &sourceTruthIdentityClient{
		testSub2APIClient: &testSub2APIClient{charges: map[string]int64{}},
		users:             map[int64]clients.Sub2APIIdentity{41: {ID: 41, Email: "gateway-owner@example.com", Status: "disabled"}},
		userErrs:          map[int64]error{},
	}
	server, session := newGatewayOwnerTestServer(t, client, nil)
	response := requestWithSession(t, server, session, http.MethodGet, "/api/auth/me?accountId=acct-other&sub2apiUserId=999", "")
	if response.Code != http.StatusOK {
		t.Fatalf("auth me = %d: %s", response.Code, response.Body.String())
	}
	if got, want := response.Header().Get("x-opl-csrf-token"), session.Header().Get("x-opl-csrf-token"); got == "" || got != want {
		t.Fatalf("auth me csrf recovery header = %q, want login token", got)
	}
	var envelope map[string]any
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	data := mapField(envelope, "data")
	if envelope["source"] != "sub2api" || envelope["status"] != "available" || envelope["available"] != true || len(data) != 6 {
		t.Fatalf("auth me envelope = %#v", envelope)
	}
	if data["consoleUserId"] != "usr-gateway-owner" || data["accountId"] != "acct-gateway" || data["role"] != "owner" || data["sub2apiUserId"] != "41" || data["email"] != "gateway-owner@example.com" || data["status"] != "disabled" {
		t.Fatalf("auth me data = %#v", data)
	}
	if len(client.userIDs) != 1 || client.userIDs[0] != 41 {
		t.Fatalf("auth me readback IDs = %#v", client.userIDs)
	}
	if _, err := time.Parse(time.RFC3339Nano, stringValue(envelope["fetchedAt"])); err != nil {
		t.Fatalf("auth me fetchedAt = %#v", envelope["fetchedAt"])
	}

	client.users[41] = clients.Sub2APIIdentity{ID: 41, Email: "mismatch@example.com", Status: "active"}
	mismatch := requestWithSession(t, server, session, http.MethodGet, "/api/auth/me", "")
	assertUnavailableIdentityEnvelope(t, mismatch, http.StatusBadGateway, "sub2api")
	client.users[41] = clients.Sub2APIIdentity{ID: 99, Email: "gateway-owner@example.com", Status: "active"}
	mismatch = requestWithSession(t, server, session, http.MethodGet, "/api/auth/me", "")
	assertUnavailableIdentityEnvelope(t, mismatch, http.StatusBadGateway, "sub2api")
	client.users[41] = clients.Sub2APIIdentity{ID: 41, Email: "gateway-owner@example.com", Status: "active"}
	client.userErrs[41] = errors.New("Sub2API unavailable")
	unavailable := requestWithSession(t, server, session, http.MethodGet, "/api/auth/me", "")
	assertUnavailableIdentityEnvelope(t, unavailable, http.StatusBadGateway, "sub2api")
}

func assertUnavailableIdentityEnvelope(t *testing.T, response *httptest.ResponseRecorder, wantStatus int, source string) {
	t.Helper()
	if response.Code != wantStatus {
		t.Fatalf("unavailable status = %d, want %d: %s", response.Code, wantStatus, response.Body.String())
	}
	var envelope map[string]any
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope) != 5 || envelope["source"] != source || envelope["status"] != "unavailable" || envelope["available"] != false || envelope["reasonCode"] != unavailableSourceReasonCode(source) || envelope["data"] != nil {
		t.Fatalf("unavailable identity envelope = %#v", envelope)
	}
}

func TestUnavailableSourceReasonCodeNormalizesCompositeAuthority(t *testing.T) {
	envelope := sourceEnvelope("control-plane+fabric+sub2api", "unavailable", nil, "")
	if envelope["reasonCode"] != "control_plane_fabric_sub2api_unavailable" {
		t.Fatalf("composite reasonCode = %#v", envelope)
	}
}
