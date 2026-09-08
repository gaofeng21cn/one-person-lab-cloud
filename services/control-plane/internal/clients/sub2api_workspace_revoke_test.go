package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceRevokeUsesServiceAuthorizationAndIndependentOwnerRead(t *testing.T) {
	for _, keyID := range []int64{0, 17} {
		input := Sub2APIWorkspaceKeyRevokeInput{UserID: 41, KeyID: keyID, ExactName: "opl-workspace-original", LaunchOperationID: "workspace-launch-original"}
		methods := []string{}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/v1/auth/login" {
				_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"admin-only","expires_in":3600}}`))
				return
			}
			if r.Header.Get("Authorization") != "Bearer admin-only" {
				t.Error("revocation did not use service authorization")
			}
			methods = append(methods, r.Method)
			if r.Method == http.MethodPost {
				var got Sub2APIWorkspaceKeyRevokeInput
				if json.NewDecoder(r.Body).Decode(&got) != nil || got != input || r.Header.Get("Idempotency-Key") == "" {
					t.Errorf("wrong original launch request: %+v", got)
				}
			} else if r.Method == http.MethodGet {
				if r.URL.Query().Get("user_id") != "41" || r.URL.Query().Get("exact_name") != input.ExactName || r.URL.Query().Get("launch_operation_id") != input.LaunchOperationID {
					t.Error("readback lost owner scope")
				}
			}
			absent := true
			now := time.Now().UTC()
			_ = json.NewEncoder(w).Encode(struct {
				Code int                           `json:"code"`
				Data sub2APIWorkspaceKeyRevocation `json:"data"`
			}{Data: sub2APIWorkspaceKeyRevocation{Sub2APIWorkspaceKeyRevokeInput: input, Capability: "workspace_key_revocation_v1", Absent: &absent, RevokedAt: &now}})
		}))
		client, err := NewSub2APIHTTPClient(Sub2APIConfig{BaseURL: server.URL, AdminEmail: "service@example.test", AdminPassword: "isolated-password", Timeout: time.Second}, server.Client())
		if err != nil {
			t.Fatal(err)
		}
		if err := client.RevokeWorkspaceKey(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		if strings.Join(methods, ",") != "POST,GET" {
			t.Fatalf("missing independent readback: %v", methods)
		}
		server.Close()
	}
}

func TestWorkspaceRevokeNeverTurnsUnverifiedAbsenceIntoCloseout(t *testing.T) {
	input := Sub2APIWorkspaceKeyRevokeInput{UserID: 41, KeyID: 17, ExactName: "opl-workspace-original", LaunchOperationID: "workspace-launch-original"}
	cases := []struct {
		name   string
		status int
		mutate func(*sub2APIWorkspaceKeyRevocation)
	}{
		{name: "old route", status: 404},
		{name: "wrong owner", mutate: func(r *sub2APIWorkspaceKeyRevocation) { r.UserID = 42 }},
		{name: "wrong key", mutate: func(r *sub2APIWorkspaceKeyRevocation) { r.KeyID = 18 }},
		{name: "other launch", mutate: func(r *sub2APIWorkspaceKeyRevocation) { r.LaunchOperationID = "workspace-launch-other" }},
		{name: "other name", mutate: func(r *sub2APIWorkspaceKeyRevocation) { r.ExactName = "opl-workspace-other" }},
		{name: "active", mutate: func(r *sub2APIWorkspaceKeyRevocation) { v := false; r.Absent = &v }},
		{name: "missing absence", mutate: func(r *sub2APIWorkspaceKeyRevocation) { r.Absent = nil }},
		{name: "missing marker", mutate: func(r *sub2APIWorkspaceKeyRevocation) { r.Capability = "" }},
		{name: "missing time", mutate: func(r *sub2APIWorkspaceKeyRevocation) { r.RevokedAt = nil }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/api/v1/auth/login" {
					_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"admin-only","expires_in":3600}}`))
					return
				}
				if r.Method == http.MethodGet && tc.status != 0 {
					w.WriteHeader(tc.status)
					_, _ = w.Write([]byte(`{"code":404,"message":"not found"}`))
					return
				}
				absent := true
				now := time.Now().UTC()
				result := sub2APIWorkspaceKeyRevocation{Sub2APIWorkspaceKeyRevokeInput: input, Capability: "workspace_key_revocation_v1", Absent: &absent, RevokedAt: &now}
				if r.Method == http.MethodGet && tc.mutate != nil {
					tc.mutate(&result)
				}
				_ = json.NewEncoder(w).Encode(struct {
					Code int                           `json:"code"`
					Data sub2APIWorkspaceKeyRevocation `json:"data"`
				}{Data: result})
			}))
			defer server.Close()
			client, err := NewSub2APIHTTPClient(Sub2APIConfig{BaseURL: server.URL, AdminEmail: "service@example.test", AdminPassword: "isolated-password", Timeout: time.Second}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			if err := client.RevokeWorkspaceKey(context.Background(), input); err == nil {
				t.Fatal("unverified revocation permitted closeout")
			}
		})
	}
}

func TestWorkspaceRevocationIdentityLookupAcceptsUnusableKeysWithoutClaimingUsability(t *testing.T) {
	for _, status := range []string{"disabled", "quota_exhausted", "expired"} {
		t.Run(status, func(t *testing.T) {
			name := "opl-workspace-original"
			detailReads := 0
			client := newSub2APITestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v1/auth/login":
					writeSub2APISuccess(t, w, map[string]any{"access_token": "service-token"})
				case "/api/v1/admin/usage/search-api-keys":
					if r.URL.Query().Get("user_id") != "41" || r.URL.Query().Get("q") != name {
						t.Error("identity search lost original owner/name")
					}
					writeSub2APISuccess(t, w, []any{map[string]any{"id": 17, "user_id": 41, "name": name}, map[string]any{"id": 18, "user_id": 41, "name": name + "-fuzzy"}})
				case "/api/v1/admin/users/41/api-keys":
					detailReads++
					writeSub2APISuccess(t, w, map[string]any{"items": []any{map[string]any{"id": 17, "user_id": 41, "name": name, "key": "private-credential", "status": status, "quota": 0, "quota_used": 0, "usage_5h": 0, "usage_1d": 0, "usage_7d": 0}}, "total": 1, "page": 1, "page_size": 1, "pages": 1})
				default:
					t.Errorf("unexpected route %s", r.URL.Path)
				}
			}, time.Second)
			keys, err := client.WorkspaceKeysForRevocation(context.Background(), 41, name)
			if err != nil || len(keys) != 1 || keys[0].ID != 17 || keys[0].UserID != 41 || keys[0].Name != name || keys[0].Key != "" || keys[0].Status != "" {
				t.Fatalf("identity lookup=%+v err=%v", keys, err)
			}
			if detailReads != 0 {
				t.Fatal("revocation downloaded key secret or usage")
			}
			if _, err := client.WorkspaceKeysForConvergence(context.Background(), 41, name); err == nil {
				t.Fatal("normal convergence accepted unusable key")
			}
			if detailReads != 1 {
				t.Fatal("normal convergence skipped authoritative status")
			}

		})
	}
}
