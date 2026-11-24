package identity_test

import (
	"encoding/json"
	"net/http"
	"opl-cloud/services/gateway-integration/identity"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

// personalWriteCall authenticates the Gateway User through the real login path
// and returns the BFF call context a personal Key command carries. The User
// has no Tenant membership: a personal Key belongs to the User, not to a
// Tenant.
func personalWriteCall(t *testing.T, tenant api.TenantProductServiceClient, service *identity.Service, idempotencyKey string) *api.CallContext {
	t.Helper()
	if _, err := service.DB.ExecContext(t.Context(), `DELETE FROM tenant.tenant_members WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	raw, session := login(t, tenant)
	return memberCall(session, raw, idempotencyKey)
}

// personalWrittenKey is the provider's answer for one personal Key. The
// caller states which facts the answer carries, so a test can prove that the
// command reached the provider with the exact values it asked for.
func personalWrittenKey(overrides map[string]any) map[string]any {
	item := personalKeyPayload()
	item["name"] = "personal key"
	item["quota"] = "1.500000"
	item["rate_limit_5h"] = "2.000000"
	item["rate_limit_1d"] = "3.000000"
	item["rate_limit_7d"] = "4.000000"
	item["ip_whitelist"] = []string{"10.0.0.0/8"}
	item["ip_blacklist"] = []string{"192.168.1.1"}
	item["expires_at"] = "2026-12-01T00:00:00Z"
	for name, value := range overrides {
		if value == nil {
			delete(item, name)
			continue
		}
		item[name] = value
	}
	return item
}

// personalKeyAuditRows reads the personal Key audit evidence this owner wrote.
func personalKeyAuditRows(t *testing.T, service *identity.Service) []string {
	t.Helper()
	rows, err := service.DB.QueryContext(t.Context(), `SELECT action||' '||resource_id||' '||outcome FROM tenant.audit_events WHERE resource_type='gateway.personal_key' ORDER BY created_at, id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var row string
		if err = rows.Scan(&row); err != nil {
			t.Fatal(err)
		}
		out = append(out, row)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPersonalKeyCreateCarriesEveryFieldAndReturnsTheSecretOnce(t *testing.T) {
	var request map[string]json.RawMessage
	var idempotency string
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys" && r.URL.Path != "/api/v1/keys/71" {
			return false
		}
		if r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		if r.Method == http.MethodPost {
			idempotency = r.Header.Get("Idempotency-Key")
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return true
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": personalWrittenKey(nil)})
		return true
	})
	call := personalWriteCall(t, tenant, service, "personal-key-create-1")
	secret, err := client.CreatePersonalGatewayKey(t.Context(), &api.CreatePersonalGatewayKeyRpcRequest{Context: call, Body: &api.CreatePersonalGatewayKeyRequest{
		Name: "personal key", GroupId: "4", IpWhitelist: []string{"10.0.0.0/8"}, IpBlacklist: []string{"192.168.1.1"},
		QuotaUsdMicros: 1_500_000, ExpiresInDays: proto.Int32(30),
		RateLimit5HUsdMicros: 2_000_000, RateLimit1DUsdMicros: 3_000_000, RateLimit7DUsdMicros: 4_000_000,
	}})
	if err != nil {
		t.Fatalf("personal key create: %v", err)
	}
	if idempotency != "personal-key-create-1" {
		t.Fatalf("the provider received idempotency key %q", idempotency)
	}
	// Every requested fact reaches the provider with its exact value.
	for name, want := range map[string]string{
		"name": `"personal key"`, "group_id": `4`, "quota": `1.500000`,
		"ip_whitelist": `["10.0.0.0/8"]`, "ip_blacklist": `["192.168.1.1"]`,
		"expires_in_days": `30`, "rate_limit_5h": `2.000000`, "rate_limit_1d": `3.000000`, "rate_limit_7d": `4.000000`,
	} {
		got, ok := request[name]
		if !ok {
			t.Fatalf("the provider never received %s: %v", name, request)
		}
		if string(got) != want {
			t.Fatalf("the provider received %s=%s, want %s", name, got, want)
		}
	}
	if secret.GetSecret() != "raw-secret-must-never-escape" {
		t.Fatalf("the create answer carries no secret: %v", secret)
	}
	key := secret.GetKey()
	if key.GetId() != "71" || key.GetName() != "personal key" || key.GetGroupId() != "4" || key.GetQuotaUsdMicros() != 1_500_000 || key.GetRateLimit5HUsdMicros() != 2_000_000 || key.GetRateLimit1DUsdMicros() != 3_000_000 || key.GetRateLimit7DUsdMicros() != 4_000_000 || key.GetStatus() != api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_ACTIVE {
		t.Fatalf("the created key projection is wrong: %v", key)
	}
	// The ordinary projection carries the address lists but never the secret.
	if len(key.GetIpWhitelist()) != 1 || key.GetIpWhitelist()[0] != "10.0.0.0/8" || len(key.GetIpBlacklist()) != 1 {
		t.Fatalf("the created key projection lost its address lists: %v", key)
	}
	if rows := personalKeyAuditRows(t, service); len(rows) != 1 || rows[0] != "gateway.personal_key_create 71 confirmed" {
		t.Fatalf("the create left no audit evidence: %v", rows)
	}
}

func TestPersonalKeyUpdateCarriesEveryFieldAndConfirmsTheReadback(t *testing.T) {
	var request map[string]json.RawMessage
	state := personalWrittenKey(nil)
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys/71" {
			return false
		}
		if r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		if r.Method == http.MethodPut {
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Error(err)
				w.WriteHeader(400)
				return true
			}
			state = personalWrittenKey(map[string]any{
				"name": "renamed personal key", "quota": "2.000000", "rate_limit_5h": "5.000000",
				"rate_limit_1d": "6.000000", "rate_limit_7d": "7.000000", "ip_whitelist": []string{"10.0.0.0/8"},
				"ip_blacklist": []string{}, "status": "inactive",
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": state})
		return true
	})
	call := personalWriteCall(t, tenant, service, "personal-key-update-1")
	updated, err := client.UpdatePersonalGatewayKey(t.Context(), &api.UpdatePersonalGatewayKeyRpcRequest{Context: call, KeyId: "71", Body: &api.UpdatePersonalGatewayKeyRequest{
		Name: proto.String("renamed personal key"), IpWhitelist: &api.PersonalGatewayKeyStringList{Values: []string{"10.0.0.0/8"}},
		IpBlacklist: &api.PersonalGatewayKeyStringList{Values: []string{}}, QuotaUsdMicros: proto.Int64(2_000_000),
		RateLimit5HUsdMicros: proto.Int64(5_000_000), RateLimit1DUsdMicros: proto.Int64(6_000_000), RateLimit7DUsdMicros: proto.Int64(7_000_000),
		Enabled: proto.Bool(false),
	}})
	if err != nil {
		t.Fatalf("personal key update: %v", err)
	}
	for name, want := range map[string]string{
		"name": `"renamed personal key"`, "quota": `2.000000`, "rate_limit_5h": `5.000000`,
		"rate_limit_1d": `6.000000`, "rate_limit_7d": `7.000000`, "status": `"inactive"`,
		"ip_whitelist": `["10.0.0.0/8"]`, "ip_blacklist": `[]`,
	} {
		got, ok := request[name]
		if !ok {
			t.Fatalf("the provider never received %s: %v", name, request)
		}
		if string(got) != want {
			t.Fatalf("the provider received %s=%s, want %s", name, got, want)
		}
	}
	if updated.GetName() != "renamed personal key" || updated.GetQuotaUsdMicros() != 2_000_000 || updated.GetRateLimit5HUsdMicros() != 5_000_000 || updated.GetStatus() != api.PersonalGatewayKeyStatusEnum_PERSONAL_GATEWAY_KEY_STATUS_ENUM_DISABLED {
		t.Fatalf("the updated key projection is wrong: %v", updated)
	}
	if rows := personalKeyAuditRows(t, service); len(rows) != 1 || rows[0] != "gateway.personal_key_update 71 confirmed" {
		t.Fatalf("the update left no audit evidence: %v", rows)
	}
}

func TestPersonalKeyUpdateRefusesAReadbackThatContradictsTheRequest(t *testing.T) {
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys/71" {
			return false
		}
		if r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		// The provider accepts the command but keeps a different quota.
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": personalWrittenKey(map[string]any{"quota": "9.000000"})})
		return true
	})
	call := personalWriteCall(t, tenant, service, "personal-key-update-2")
	if _, err := client.UpdatePersonalGatewayKey(t.Context(), &api.UpdatePersonalGatewayKeyRpcRequest{Context: call, KeyId: "71", Body: &api.UpdatePersonalGatewayKeyRequest{QuotaUsdMicros: proto.Int64(2_000_000)}}); status.Code(err) != codes.Unavailable {
		t.Fatalf("a contradicting readback was accepted: %v", err)
	}
	if rows := personalKeyAuditRows(t, service); len(rows) != 0 {
		t.Fatalf("a refused update left audit evidence: %v", rows)
	}
}

func TestPersonalKeyRevealReturnsTheSecretAndNeverAcrossAccounts(t *testing.T) {
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys/71" && r.URL.Path != "/api/v1/keys/72" {
			return false
		}
		if r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		item := personalWrittenKey(nil)
		if r.URL.Path == "/api/v1/keys/72" {
			item["id"] = 72
			item["user_id"] = 102
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": item})
		return true
	})
	call := personalWriteCall(t, tenant, service, "personal-key-reveal-1")
	secret, err := client.RevealPersonalGatewayKey(t.Context(), &api.RevealPersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"})
	if err != nil {
		t.Fatalf("personal key reveal: %v", err)
	}
	if secret.GetSecret() != "raw-secret-must-never-escape" || secret.GetKey().GetId() != "71" {
		t.Fatalf("the reveal answer carries no secret: %v", secret)
	}
	// Another account's key never yields a secret through this owner's path.
	other, err := client.RevealPersonalGatewayKey(t.Context(), &api.RevealPersonalGatewayKeyRpcRequest{Context: call, KeyId: "72"})
	if err == nil {
		t.Fatalf("another account's key was revealed: %v", other)
	}
	if other.GetSecret() != "" {
		t.Fatalf("another account's secret escaped: %v", other)
	}
	if rows := personalKeyAuditRows(t, service); len(rows) != 1 || rows[0] != "gateway.personal_key_reveal 71 confirmed" {
		t.Fatalf("the reveal left no audit evidence: %v", rows)
	}
}

func TestPersonalKeyRevokeConfirmsAbsenceAndIsIdempotent(t *testing.T) {
	deleted := false
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys/71" {
			return false
		}
		if r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		switch r.Method {
		case http.MethodDelete:
			deleted = true
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": nil})
			return true
		default:
			if deleted {
				w.WriteHeader(http.StatusNotFound)
				return true
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": personalWrittenKey(nil)})
			return true
		}
	})
	call := personalWriteCall(t, tenant, service, "personal-key-revoke-1")
	operation, err := client.RevokePersonalGatewayKey(t.Context(), &api.RevokePersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"})
	if err != nil {
		t.Fatalf("personal key revoke: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || operation.GetResourceId() != "71" || operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_REVOKE_KEY {
		t.Fatalf("the revoke operation is wrong: %v", operation)
	}
	// A repeated revoke is already satisfied, not a new failure.
	again, err := client.RevokePersonalGatewayKey(t.Context(), &api.RevokePersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"})
	if err != nil {
		t.Fatalf("repeated personal key revoke: %v", err)
	}
	if again.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("the repeated revoke did not succeed: %v", again)
	}
	if rows := personalKeyAuditRows(t, service); len(rows) != 2 {
		t.Fatalf("the revoke left no audit evidence: %v", rows)
	}
}

func TestPersonalKeyRevokeFailsClosedWhenTheProviderKeepsTheKey(t *testing.T) {
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys/71" {
			return false
		}
		if r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		if r.Method == http.MethodDelete {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": nil})
			return true
		}
		// The provider still holds the Key after the delete.
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": personalWrittenKey(nil)})
		return true
	})
	call := personalWriteCall(t, tenant, service, "personal-key-revoke-2")
	operation, err := client.RevokePersonalGatewayKey(t.Context(), &api.RevokePersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"})
	if err == nil {
		t.Fatalf("an unconfirmed revoke reported success: %v", operation)
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("an unconfirmed revoke reported %v", err)
	}
	if rows := personalKeyAuditRows(t, service); len(rows) != 0 {
		t.Fatalf("a refused revoke left audit evidence: %v", rows)
	}
}

func TestPersonalKeyRevokeFailsClosedWhenTheProviderRefusesTheDelete(t *testing.T) {
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys/71" {
			return false
		}
		if r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		if r.Method == http.MethodDelete {
			// The provider refuses the delete and still holds the Key.
			w.WriteHeader(http.StatusNotFound)
			return true
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": personalWrittenKey(nil)})
		return true
	})
	call := personalWriteCall(t, tenant, service, "personal-key-revoke-3")
	operation, err := client.RevokePersonalGatewayKey(t.Context(), &api.RevokePersonalGatewayKeyRpcRequest{Context: call, KeyId: "71"})
	if err == nil {
		t.Fatalf("a refused delete reported success: %v", operation)
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("a refused delete reported %v", err)
	}
	if rows := personalKeyAuditRows(t, service); len(rows) != 0 {
		t.Fatalf("a refused revoke left audit evidence: %v", rows)
	}
}

func TestPersonalKeyProviderFailureNeverReportsSuccess(t *testing.T) {
	client, tenant, service := gatewayProductSystemWithUpstream(t, func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path != "/api/v1/keys" && r.URL.Path != "/api/v1/keys/71" {
			return false
		}
		if r.Header.Get("Authorization") != "Bearer user-token" {
			w.WriteHeader(401)
			return true
		}
		w.WriteHeader(http.StatusInternalServerError)
		return true
	})
	call := personalWriteCall(t, tenant, service, "personal-key-create-2")
	secret, err := client.CreatePersonalGatewayKey(t.Context(), &api.CreatePersonalGatewayKeyRpcRequest{Context: call, Body: &api.CreatePersonalGatewayKeyRequest{Name: "personal key", GroupId: "4"}})
	if err == nil {
		t.Fatalf("a failed provider call reported success: %v", secret)
	}
	if secret.GetSecret() != "" {
		t.Fatalf("a failed provider call leaked a secret: %v", secret)
	}
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("a failed provider call reported %v", err)
	}
	if rows := personalKeyAuditRows(t, service); len(rows) != 0 {
		t.Fatalf("a failed provider call left audit evidence: %v", rows)
	}
}
