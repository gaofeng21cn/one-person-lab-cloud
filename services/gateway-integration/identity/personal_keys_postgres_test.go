package identity_test

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"opl-cloud/services/gateway-integration/identity"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
)

func personalKeyPayload() map[string]any {
	return map[string]any{"id": 71, "user_id": 101, "name": "workspace-looking-but-general", "key": "raw-secret-must-never-escape", "status": "active", "group_id": 4, "quota": "1.0000019", "quota_used": "0.25", "rate_limit_5h": "2.5", "rate_limit_1d": 0, "rate_limit_7d": 0, "usage_5h": "0.0000019", "usage_1d": 0, "usage_7d": 0, "current_concurrency": 2, "ip_whitelist": []string{"127.0.0.1"}, "ip_blacklist": []string{}, "created_at": "2026-10-15T00:00:00Z", "updated_at": "2026-10-16T00:00:00Z"}
}
func TestPersonalKeyListUsesDelegatedUserAndReturnsRedactedFacts(t *testing.T) {
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys" {
			return false
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		if r.URL.Query().Get("page") != "1" || r.URL.Query().Get("page_size") != "20" || r.URL.Query().Get("status") != "inactive" || r.URL.Query().Get("group_id") != "0" || r.URL.Query().Get("sort_by") != "current_concurrency" || r.URL.Query().Get("sort_order") != "asc" || r.URL.Query().Get("search") != "example" {
			t.Error("invalid forwarded key filters")
			w.WriteHeader(400)
			return true
		}
		item := personalKeyPayload()
		item["status"] = "inactive"
		item["group_id"] = nil
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []any{item}, "total": 1, "page": 1, "page_size": 20, "pages": 1}})
		return true
	})
	if _, err := service.DB.ExecContext(t.Context(), `DELETE FROM tenant.tenant_members WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	raw, _ := login(t, tenant)
	page, err := client.ListPersonalGatewayKeys(t.Context(), &api.ListPersonalGatewayKeysRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}, Page: 1, PageSize: 20, Search: " example ", Status: "disabled", GroupId: proto.String("0"), SortBy: "currentConcurrency", SortOrder: "asc"})
	if err != nil {
		t.Fatalf("personal key read after login: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].GetQuotaUsdMicros() != 1000001 || page.Items[0].GetKind() != api.PersonalGatewayKeyKindEnum_PERSONAL_GATEWAY_KEY_KIND_ENUM_GENERAL || page.Items[0].GetStatus() != api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_DISABLED || page.Items[0].GroupId != nil {
		t.Fatalf("incorrect key facts: %v", page)
	}
	wire, err := publicjson.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err = json.Unmarshal(wire, &data); err != nil {
		t.Fatal(err)
	}
	key := data["items"].([]any)[0].(map[string]any)
	if _, ok := key["key"]; ok {
		t.Fatal("raw key escaped")
	}
	if key["status"] != "disabled" || key["groupId"] != nil || key["quotaUSDMicros"] != "1000001" || key["rateLimit5hUSDMicros"] != "2500000" || key["usage5hUSDMicros"] != "1" || key["manageable"] != false || key["deletable"] != false {
		t.Fatalf("incorrect wire facts: %s", wire)
	}
}

func TestPersonalKeyDetailUsesActualManagedBindingAndChecksOwnership(t *testing.T) {
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys/71" && r.URL.Path != "/api/v1/keys/72" {
			return false
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		item := personalKeyPayload()
		if r.URL.Path == "/api/v1/keys/72" {
			item["id"] = 72
			item["user_id"] = 102
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": item})
		return true
	})
	if _, err := service.GatewayStore.DB().ExecContext(t.Context(), `INSERT INTO gateway.key_bindings(id,tenant_id,workspace_id,actor_id,external_key_id,fingerprint,secret_ref,purpose,model_ids,observation_result,name) VALUES('bound-71','tenant-test','workspace-71','101','71','fingerprint','secret-reference','workspace_managed','{}','confirmed','arbitrary-name')`); err != nil {
		t.Fatal(err)
	}
	raw, _ := login(t, tenant)
	call := &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}
	key, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"})
	if err != nil {
		t.Fatalf("personal key detail: %v", err)
	}
	if key.GetKind() != api.PersonalGatewayKeyKindEnum_PERSONAL_GATEWAY_KEY_KIND_ENUM_WORKSPACE || key.GetManageable() || key.GetDeletable() {
		t.Fatalf("managed key facts: %v", key)
	}
	if _, err = client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: call, KeyId: "72"}); err == nil {
		t.Fatal("another user's key returned")
	}
}

func TestPersonalGroupsApplyUserRatesIncludingZero(t *testing.T) {
	client, tenant, _ := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/groups/available" && r.URL.Path != "/api/v1/groups/rates" {
			return false
		}
		if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		var data any = map[string]any{"4": 0}
		if r.URL.Path == "/api/v1/groups/available" {
			data = []map[string]any{{"id": 4, "name": "Personal group", "description": "Available to this user", "platform": "openai", "subscription_type": "standard", "status": "active", "rate_multiplier": 1.5}, {"id": 5, "name": "Default rate", "platform": "openai", "subscription_type": "standard", "status": "active", "rate_multiplier": 2}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
		return true
	})
	raw, _ := login(t, tenant)
	groups, err := client.ListPersonalGatewayGroups(t.Context(), &api.ListPersonalGatewayGroupsRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}})
	if err != nil {
		t.Fatalf("personal groups: %v", err)
	}
	if groups.GetTotal() != 2 || len(groups.Items) != 2 || groups.Items[0].GetRateMultiplier() != 0 || groups.Items[1].GetRateMultiplier() != 2 || groups.Items[0].GetPlatform() != "openai" {
		t.Fatalf("wrong available group rates: %v", groups)
	}
}

func personalUsagePayload() map[string]any {
	return map[string]any{"user_id": 101, "api_key_id": 71, "request_id": "request-71", "created_at": "2026-10-16T00:00:00Z", "model": "gpt-5.4", "inbound_endpoint": "/v1/responses", "request_type": "stream", "input_tokens": 17, "output_tokens": 3, "cache_creation_tokens": 2, "cache_read_tokens": 1, "actual_cost": "0.1250019", "duration_ms": 123, "first_token_ms": nil}
}
func personalUsageFixture(t *testing.T, w http.ResponseWriter, r *http.Request) bool {
	t.Helper()
	if r.URL.Path != "/api/v1/keys/71" && r.URL.Path != "/api/v1/usage" && r.URL.Path != "/api/v1/usage/stats" {
		return false
	}
	if r.Method != http.MethodGet || r.Header.Get("Authorization") != "Bearer user-token" {
		w.WriteHeader(401)
		return true
	}
	var data any = personalKeyPayload()
	if r.URL.Path != "/api/v1/keys/71" {
		q := r.URL.Query()
		if q.Get("api_key_id") != "71" || q.Get("timezone") != "Asia/Shanghai" || q.Get("start_date") == "" || q.Get("end_date") == "" || q.Has("user_id") || q.Has("period") {
			t.Error("invalid personal usage filters")
			w.WriteHeader(400)
			return true
		}
		if r.URL.Path == "/api/v1/usage" {
			data = map[string]any{"items": []any{personalUsagePayload()}, "total": 1, "page": 1, "page_size": 20, "pages": 1}
		} else {
			data = map[string]any{"total_requests": 1, "total_input_tokens": 17, "total_output_tokens": 3, "total_tokens": 20, "total_actual_cost": "0.1250019"}
		}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	return true
}
func TestPersonalKeyUsageForwardsKeyAndShanghaiCalendar(t *testing.T) {
	client, tenant, _ := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool { return personalUsageFixture(t, w, r) })
	raw, _ := login(t, tenant)
	page, err := client.ListPersonalGatewayKeyUsage(t.Context(), &api.ListPersonalGatewayKeyUsageRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}, KeyId: "71", Page: 1, PageSize: 20, Period: "month"})
	if err != nil {
		t.Fatalf("personal key usage: %v", err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].GetInputTokens() != 17 || page.Items[0].GetActualCostUsdMicros() != 125001 || page.Items[0].GetDurationMs() != 123 || page.Items[0].FirstTokenMs != nil {
		t.Fatalf("incorrect usage facts: %v", page)
	}
	wire, err := publicjson.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err = json.Unmarshal(wire, &data); err != nil {
		t.Fatal(err)
	}
	item := data["items"].([]any)[0].(map[string]any)
	// Optional absent durations remain absent/null, never a fabricated zero.
	if item["durationMs"] != "123" || item["firstTokenMs"] != nil || item["inputTokens"] != "17" {
		t.Fatalf("incorrect usage scalar forms: %s", wire)
	}
}

func TestPersonalKeyUsageSummaryReadsTheOwnedKeyTotals(t *testing.T) {
	client, tenant, _ := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool { return personalUsageFixture(t, w, r) })
	raw, _ := login(t, tenant)
	summary, err := client.GetPersonalGatewayKeyUsageSummary(t.Context(), &api.GetPersonalGatewayKeyUsageSummaryRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}, KeyId: "71", Period: "week"})
	if err != nil {
		t.Fatalf("personal key usage summary: %v", err)
	}
	if summary.GetTotalRequests() != 1 || summary.GetTotalInputTokens() != 17 || summary.GetTotalOutputTokens() != 3 || summary.GetTotalTokens() != 20 || summary.GetTotalActualCostUsdMicros() != 125001 {
		t.Fatalf("wrong key totals: %v", summary)
	}
}

func TestPersonalKeyMissingUpstreamKeyIsNotFound(t *testing.T) {
	client, tenant, _ := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys/71" {
			return false
		}
		w.WriteHeader(http.StatusNotFound)
		return true
	})
	raw, _ := login(t, tenant)
	_, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}, KeyId: "71"})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("missing personal key: %v", err)
	}
}

func TestPersonalKeyPaginationPreservesNativeEmptyAndLargePages(t *testing.T) {
	cases := []struct {
		name              string
		page, size, pages int32
		total             int64
		items             []any
	}{
		{"empty-out-of-range", 99, 20, 1, 0, []any{}},
		{"populated-out-of-range", 99, 20, 1, 1, []any{}},
		{"large-page-count", 1, 1, 1000001, 1000001, []any{personalKeyPayload()}},
		{"concurrent-insert-after-count", 1, 20, 1, 0, []any{personalKeyPayload()}},
		{"concurrent-delete-after-count", 1, 20, 1, 1, []any{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, tenant, _ := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != "/api/v1/keys" {
					return false
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": tc.items, "total": tc.total, "page": tc.page, "page_size": tc.size, "pages": tc.pages}})
				return true
			})
			raw, _ := login(t, tenant)
			page, err := client.ListPersonalGatewayKeys(t.Context(), &api.ListPersonalGatewayKeysRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}, Page: tc.page, PageSize: tc.size})
			if err != nil {
				t.Fatalf("valid native page rejected: %v", err)
			}
			if page.Total != tc.total || page.Pages != tc.pages || page.Page != tc.page || len(page.Items) != len(tc.items) {
				t.Fatalf("native page changed: %v", page)
			}
		})
	}
}

// The fixture owns all credentials below; tests observe only the production RPCs.
func personalCredentialSystem(t *testing.T, bearer string, expires any) (api.GatewayProductServiceClient, api.TenantProductServiceClient, *identity.Service, *atomic.Int32) {
	t.Helper()
	state := &atomic.Int32{}
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/api/v1/auth/login" {
			var input map[string]string
			_ = json.NewDecoder(r.Body).Decode(&input)
			user := identity.GatewayIdentity{ID: 101, Email: "test@example.test", Status: "active"}
			token := bearer
			if input["email"] == "directory@example.test" {
				user = identity.GatewayIdentity{ID: 1, Email: "directory@example.test", Status: "active"}
				token = "directory-token"
			}
			payload := map[string]any{"access_token": token, "user": user}
			if expires != nil {
				payload["expires_in"] = expires
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": payload})
			return true
		}
		if r.URL.Path == "/api/v1/auth/me" {
			if state.Load() == 4 {
				timer := time.NewTimer(1500 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-timer.C:
				case <-r.Context().Done():
					return true
				}
			}
			if r.Header.Get("Authorization") != "Bearer "+bearer || state.Load() == 2 {
				w.WriteHeader(401)
				return true
			}
			user := identity.GatewayIdentity{ID: 101, Email: "test@example.test", Status: "active"}
			if state.Load() == 1 {
				user.ID = 102
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": user})
			return true
		}
		if r.URL.Path == "/api/v1/keys/71" {
			if r.Header.Get("Authorization") != "Bearer "+bearer || state.Load() == 3 {
				w.WriteHeader(403)
				return true
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": personalKeyPayload()})
			return true
		}
		return false
	})
	return client, tenant, service, state
}
func jwtFixture(exp time.Time) string {
	raw, _ := json.Marshal(map[string]int64{"exp": exp.Unix()})
	return "e30." + base64.RawURLEncoding.EncodeToString(raw) + ".fixture-signature"
}
func requireReauthentication(t *testing.T, err error) {
	t.Helper()
	code, ok := owneridentity.ErrorCode(err)
	if status.Code(err) != codes.Unauthenticated || !ok || code != api.ErrorCodeEnum_ERROR_CODE_ENUM_REAUTHENTICATION_REQUIRED {
		t.Fatalf("expected typed Key reauthentication: %v", err)
	}
}
func TestPersonalKeyLoginAcceptsProvenJWTExpiryWithoutExpiresIn(t *testing.T) {
	client, tenant, _, _ := personalCredentialSystem(t, jwtFixture(time.Now().Add(time.Hour)), nil)
	raw, _ := login(t, tenant)
	key, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}, KeyId: "71"})
	if err != nil || key.GetId() != "71" {
		t.Fatalf("JWT exp-only login cannot read own Key: %v", err)
	}
}

func TestPersonalKeyUnknownOrExpiredLifetimeKeepsSessionAndWalletUsable(t *testing.T) {
	for _, tc := range []struct {
		name, bearer string
		expires      any
	}{
		{"opaque-without-ttl", "user-token", nil},
		{"nonpositive-ttl", "user-token", 0},
		{"expired-jwt-only", jwtFixture(time.Now().Add(-time.Hour)), nil},
		{"jwt-earlier-than-ttl", jwtFixture(time.Now().Add(-time.Hour)), 3600},
		{"malformed-ttl", "user-token", "not-a-lifetime"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, tenant, service, _ := personalCredentialSystem(t, tc.bearer, tc.expires)
			raw, _ := login(t, tenant)
			call := &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}
			_, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"})
			requireReauthentication(t, err)
			if session, err := tenant.GetSession(t.Context(), &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(raw)}}); err != nil || session.GetActorId() != "101" {
				t.Fatalf("Cloud session lost: %v", err)
			}
			if wallet, err := client.GetWallet(t.Context(), &api.GetWalletRpcRequest{Context: call}); err != nil || wallet.GetBalanceUsdMicros() != 12500000 {
				t.Fatalf("wallet lost: %v", err)
			}
			var stored string
			if err = service.DB.QueryRowContext(t.Context(), `SELECT row_to_json(s)::text FROM tenant.sessions s WHERE id=$1`, call.GetSessionId()).Scan(&stored); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(stored, tc.bearer) || strings.Contains(stored, "directory-token") {
				t.Fatal("credential persisted in Cloud session")
			}
		})
	}
}
func TestPersonalKeyRejectedCredentialsAreDiscardedOnlyForTheirSession(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state int32
	}{{"different-subject", 1}, {"rejected-auth-me", 2}, {"rejected-key-read", 3}} {
		t.Run(tc.name, func(t *testing.T) {
			client, tenant, _, upstream := personalCredentialSystem(t, "user-token", 3600)
			first, _ := login(t, tenant)
			second, _ := login(t, tenant)
			firstCall := &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(first))}
			secondCall := &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(second))}
			upstream.Store(tc.state)
			_, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: firstCall, KeyId: "71"})
			requireReauthentication(t, err)
			upstream.Store(0)
			_, err = client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: firstCall, KeyId: "71"})
			requireReauthentication(t, err)
			if key, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: secondCall, KeyId: "71"}); err != nil || key.GetId() != "71" {
				t.Fatalf("another session's credential invalidated: %v", err)
			}
			if wallet, err := client.GetWallet(t.Context(), &api.GetWalletRpcRequest{Context: firstCall}); err != nil || wallet.GetBalanceUsdMicros() != 12500000 {
				t.Fatalf("bearer rejection broke wallet: %v", err)
			}
		})
	}
}
func TestPersonalKeyCredentialExpiresBeforeCloudSession(t *testing.T) {
	client, tenant, _, _ := personalCredentialSystem(t, "user-token", 3)
	raw, _ := login(t, tenant)
	started := time.Now()
	call := &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}
	if _, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"}); err != nil {
		t.Fatal(err)
	}
	timer := time.NewTimer(time.Until(started.Add(3 * time.Second)))
	defer timer.Stop()
	<-timer.C
	_, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"})
	requireReauthentication(t, err)
	if wallet, err := client.GetWallet(t.Context(), &api.GetWalletRpcRequest{Context: call}); err != nil || wallet.GetBalanceUsdMicros() != 12500000 {
		t.Fatalf("TTL expiration broke wallet: %v", err)
	}
	if _, err = tenant.GetSession(t.Context(), &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(raw)}}); err != nil {
		t.Fatalf("TTL expiration broke Cloud session: %v", err)
	}
}
func TestPersonalKeyLogoutRevokesSessionAndDoesNotAffectAnotherLogin(t *testing.T) {
	client, tenant, _, _ := personalCredentialSystem(t, "user-token", 3600)
	first, _ := login(t, tenant)
	second, _ := login(t, tenant)
	firstCall := &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(first))}
	if _, err := tenant.Logout(t.Context(), &api.LogoutRpcRequest{Context: firstCall}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: firstCall, KeyId: "71"}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("logged out session read personal Key: %v", err)
	}
	if _, err := tenant.GetSession(t.Context(), &api.GetSessionRpcRequest{Context: &api.CallContext{SessionId: proto.String(first)}}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("logged out session usable: %v", err)
	}
	if _, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(second))}, KeyId: "71"}); err != nil {
		t.Fatalf("logout invalidated another login: %v", err)
	}
}

func TestPersonalKeyDoesNotUseCredentialAfterTTLExpiresDuringIdentityRead(t *testing.T) {
	client, tenant, _, upstream := personalCredentialSystem(t, "user-token", 1)
	raw, _ := login(t, tenant)
	upstream.Store(4)
	_, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}, KeyId: "71"})
	requireReauthentication(t, err)
}

func TestPersonalReadsRejectUnownedOrMalformedUpstreamFacts(t *testing.T) {
	var mode atomic.Int32
	client, tenant, _ := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		var data any
		switch r.URL.Path {
		case "/api/v1/keys/71":
			item := personalKeyPayload()
			switch mode.Load() {
			case 1:
				item["user_id"] = 102
			case 2:
				item["id"] = 72
			case 3:
				item["status"] = "invented"
			case 4:
				item["quota"] = "-1"
			case 5:
				delete(item, "rate_limit_5h")
			case 6:
				item["created_at"] = nil
			case 7:
				item["current_concurrency"] = -1
			case 8:
				item["quota"] = "9223372036854775808"
			case 9:
				item["group_id"] = -1
			case 10:
				item["current_concurrency"] = int64(1) << 32
			case 11:
				item["last_used_at"] = "0001-01-01T00:00:00Z"
			case 12:
				w.WriteHeader(http.StatusTooManyRequests)
				return true
			}
			data = item
		case "/api/v1/keys":
			data = map[string]any{"items": []any{personalKeyPayload(), personalKeyPayload()}, "total": 2, "page": 1, "page_size": 20, "pages": 1}
		case "/api/v1/groups/available":
			group := map[string]any{"id": 4, "name": "Personal group", "platform": "openai", "status": "active", "rate_multiplier": 1.5}
			if mode.Load() == 21 {
				delete(group, "rate_multiplier")
			}
			data = []any{group}
			if mode.Load() == 22 {
				data = []any{group, group}
			}
		case "/api/v1/groups/rates":
			data = map[string]any{"4": 0}
			if mode.Load() == 23 {
				data = map[string]any{"4": -1}
			}
			if mode.Load() == 24 {
				data = map[string]any{"bad-id": 1}
			}
		case "/api/v1/usage":
			item := personalUsagePayload()
			switch mode.Load() {
			case 31:
				item["user_id"] = 102
			case 32:
				item["api_key_id"] = 72
			case 33:
				delete(item, "cache_read_tokens")
			case 34:
				item["actual_cost"] = "-1"
			case 35:
				item["duration_ms"] = -1
			case 36:
				item["first_token_ms"] = -1
			case 37:
				item["created_at"] = nil
			}
			data = map[string]any{"items": []any{item}, "total": 1, "page": 1, "page_size": 20, "pages": 1}
			if mode.Load() == 38 {
				data = map[string]any{"items": []any{item}, "total": 2, "page": 1, "page_size": 20, "pages": 2}
			}
			if mode.Load() == 39 {
				items := make([]any, 21)
				for i := range items {
					items[i] = personalUsagePayload()
				}
				data = map[string]any{"items": items, "total": 21, "page": 1, "page_size": 20, "pages": 2}
			}
		case "/api/v1/usage/stats":
			payload := map[string]any{"total_requests": 1, "total_input_tokens": 17, "total_output_tokens": 3, "total_tokens": 20, "total_actual_cost": "0.125"}
			if mode.Load() == 41 {
				delete(payload, "total_requests")
			}
			if mode.Load() == 42 {
				payload["total_tokens"] = -1
			}
			if mode.Load() == 43 {
				payload["total_actual_cost"] = "-1"
			}
			data = payload
		default:
			return false
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
		return true
	})
	raw, _ := login(t, tenant)
	call := &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}
	for _, tc := range []struct {
		name   string
		mode   int32
		method string
	}{
		{"wrong-key-user", 1, "key"}, {"wrong-key-id", 2, "key"}, {"unknown-status", 3, "key"}, {"negative-quota", 4, "key"}, {"missing-rate-limit", 5, "key"}, {"missing-created-at", 6, "key"}, {"negative-concurrency", 7, "key"}, {"overflow-quota", 8, "key"}, {"invalid-group-id", 9, "key"}, {"overflow-concurrency", 10, "key"}, {"invalid-timestamp", 11, "key"},
		{"duplicate-key-list", 13, "list"}, {"missing-group-rate", 21, "groups"}, {"duplicate-group", 22, "groups"}, {"negative-group-override", 23, "groups"}, {"invalid-group-override-id", 24, "groups"},
		{"foreign-usage-user", 31, "usage"}, {"foreign-usage-key", 32, "usage"}, {"missing-token-count", 33, "usage"}, {"negative-usage-cost", 34, "usage"}, {"negative-duration", 35, "usage"}, {"negative-first-token", 36, "usage"}, {"missing-usage-time", 37, "usage"}, {"inconsistent-usage-page", 38, "usage"}, {"oversized-usage-page", 39, "usage"},
		{"missing-summary-count", 41, "summary"}, {"negative-summary-count", 42, "summary"}, {"negative-summary-cost", 43, "summary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode.Store(tc.mode)
			var err error
			switch tc.method {
			case "key":
				_, err = client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"})
			case "list":
				_, err = client.ListPersonalGatewayKeys(t.Context(), &api.ListPersonalGatewayKeysRpcRequest{Context: call, Page: 1, PageSize: 20})
			case "groups":
				_, err = client.ListPersonalGatewayGroups(t.Context(), &api.ListPersonalGatewayGroupsRpcRequest{Context: call})
			case "usage":
				_, err = client.ListPersonalGatewayKeyUsage(t.Context(), &api.ListPersonalGatewayKeyUsageRpcRequest{Context: call, KeyId: "71", Page: 1, PageSize: 20, Period: "month"})
			case "summary":
				_, err = client.GetPersonalGatewayKeyUsageSummary(t.Context(), &api.GetPersonalGatewayKeyUsageSummaryRpcRequest{Context: call, KeyId: "71", Period: "month"})
			}
			if status.Code(err) != codes.Unavailable {
				t.Fatalf("unowned/malformed native facts accepted: %v", err)
			}
		})
	}
	mode.Store(12)
	if _, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("rate limit lost: %v", err)
	}
	mode.Store(0)
	if _, err := client.GetPersonalGatewayKey(t.Context(), &api.GetPersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"}); err != nil {
		t.Fatalf("malformed facts invalidated usable credential: %v", err)
	}
}

func TestPersonalCollectionsRequireAuthoritativeArraysAndEnvelope(t *testing.T) {
	var mode atomic.Int32
	client, tenant, _ := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		envelope := map[string]any{"code": 0}
		switch r.URL.Path {
		case "/api/v1/keys/71":
			envelope["data"] = personalKeyPayload()
		case "/api/v1/keys", "/api/v1/usage":
			page := map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 20, "pages": 1}
			switch mode.Load() {
			case 1:
				delete(page, "items")
			case 2:
				page["items"] = nil
			case 3:
				delete(envelope, "code")
			case 4:
				envelope["code"] = nil
			}
			envelope["data"] = page
			if mode.Load() == 5 {
				delete(envelope, "data")
			}
			if mode.Load() == 6 {
				envelope["data"] = nil
			}
		case "/api/v1/groups/available":
			envelope["data"] = []any{}
			switch mode.Load() {
			case 1:
				delete(envelope, "data")
			case 2:
				envelope["data"] = nil
			case 3:
				delete(envelope, "code")
			case 4:
				envelope["code"] = nil
			}
		case "/api/v1/groups/rates":
			envelope["data"] = nil // explicitly legal upstream
		default:
			return false
		}
		_ = json.NewEncoder(w).Encode(envelope)
		return true
	})
	raw, _ := login(t, tenant)
	call := &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}
	for _, method := range []string{"keys", "usage", "groups"} {
		read := func() error {
			switch method {
			case "keys":
				_, err := client.ListPersonalGatewayKeys(t.Context(), &api.ListPersonalGatewayKeysRpcRequest{Context: call, Page: 1, PageSize: 20})
				return err
			case "usage":
				_, err := client.ListPersonalGatewayKeyUsage(t.Context(), &api.ListPersonalGatewayKeyUsageRpcRequest{Context: call, KeyId: "71", Page: 1, PageSize: 20, Period: "month"})
				return err
			default:
				_, err := client.ListPersonalGatewayGroups(t.Context(), &api.ListPersonalGatewayGroupsRpcRequest{Context: call})
				return err
			}
		}
		for i, name := range []string{"missing-collection", "null-collection", "missing-code", "null-code", "missing-data", "null-data"} {
			if method == "groups" && i >= 4 {
				continue
			}
			t.Run(method+"/"+name, func(t *testing.T) {
				mode.Store(int32(i + 1))
				if err := read(); status.Code(err) != codes.Unavailable {
					t.Fatalf("missing authoritative shape became available: %v", err)
				}
			})
		}
		t.Run(method+"/authoritative-empty-array", func(t *testing.T) {
			mode.Store(0)
			if err := read(); err != nil {
				t.Fatalf("valid empty array rejected: %v", err)
			}
		})
	}
}

// Sub2API performs COUNT and item reads separately; both are authoritative facts
// from their own read, so concurrent insert/delete need not agree on cardinality.
func TestPersonalUsagePaginationPreservesConcurrentNativeReadback(t *testing.T) {
	for _, total := range []int64{0, 2} {
		t.Run(string(rune('0'+total)), func(t *testing.T) {
			client, tenant, _ := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
				if r.URL.Path != "/api/v1/usage" {
					return personalUsageFixture(t, w, r)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []any{personalUsagePayload()}, "total": total, "page": 1, "page_size": 20, "pages": 1}})
				return true
			})
			raw, _ := login(t, tenant)
			page, err := client.ListPersonalGatewayKeyUsage(t.Context(), &api.ListPersonalGatewayKeyUsageRpcRequest{Context: &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}, KeyId: "71", Page: 1, PageSize: 20, Period: "month"})
			if err != nil || page.GetTotal() != total || len(page.GetItems()) != 1 {
				t.Fatalf("concurrent native usage read changed: page=%v err=%v", page, err)
			}
		})
	}
}

func TestPersonalKeySearchRespectsNativeUTF8ByteLimit(t *testing.T) {
	var keyReads atomic.Int32
	client, tenant, _ := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys" {
			return false
		}
		keyReads.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"items": []any{}, "total": 0, "page": 1, "page_size": 20, "pages": 1}})
		return true
	})
	raw, _ := login(t, tenant)
	call := &api.CallContext{SessionId: proto.String(owneridentity.SessionReference(raw))}
	if _, err := client.ListPersonalGatewayKeys(t.Context(), &api.ListPersonalGatewayKeysRpcRequest{Context: call, Page: 1, PageSize: 20, Search: strings.Repeat("界", 33) + "a"}); err != nil {
		t.Fatalf("legal 100-byte UTF-8 search refused: %v", err)
	}
	before := keyReads.Load()
	_, err := client.ListPersonalGatewayKeys(t.Context(), &api.ListPersonalGatewayKeysRpcRequest{Context: call, Page: 1, PageSize: 20, Search: strings.Repeat("界", 34)})
	if status.Code(err) != codes.InvalidArgument || keyReads.Load() != before {
		t.Fatalf("oversize search reached native truncation: code=%v reads=%d before=%d", status.Code(err), keyReads.Load(), before)
	}
}
