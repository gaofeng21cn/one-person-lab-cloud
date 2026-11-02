package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func TestGatewayWorkspaceKeyIssuerLifecycleUsesExactDelegatedUserKey(t *testing.T) {
	statusValue := "active"
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/keys/42" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer user-token" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		methods = append(methods, r.Method)
		switch r.Method {
		case http.MethodGet:
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"id": 42, "user_id": 77, "status": statusValue,
			}})
		case http.MethodPut:
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["status"] != "inactive" {
				t.Fatalf("update body=%v", body)
			}
			statusValue = body["status"]
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{
				"id": 42, "user_id": 77, "status": statusValue,
			}})
		default:
			t.Fatalf("method=%s", r.Method)
		}
	}))
	defer server.Close()

	gateway, err := NewGateway(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := NewGatewayWorkspaceKeyIssuer(gateway, "Codex")
	if err != nil {
		t.Fatal(err)
	}
	ctx := withManagedKeyDelegation(context.Background(), &ManagedKeyDelegation{Subject: "77", Bearer: "user-token", ExpiresAt: time.Now().Add(time.Hour)})
	if _, err = issuer.ReadWorkspaceKeyState(ctx, "78", "42"); err == nil {
		t.Fatal("a delegated credential for another Gateway subject was accepted")
	}
	if _, err = issuer.ReadWorkspaceKeyState(context.Background(), "77", "42"); err == nil {
		t.Fatal("a lifecycle read without a delegated user credential was accepted")
	}

	state, err := issuer.ReadWorkspaceKeyState(ctx, "77", strconv.FormatInt(42, 10))
	if err != nil || state != ManagedKeyExternalActive {
		t.Fatalf("initial state=%q err=%v", state, err)
	}
	if err = issuer.SetWorkspaceKeyState(ctx, "77", "42", ManagedKeyExternalDisabled); err != nil {
		t.Fatal(err)
	}
	state, err = issuer.ReadWorkspaceKeyState(ctx, "77", "42")
	if err != nil || state != ManagedKeyExternalDisabled {
		t.Fatalf("disabled state=%q err=%v", state, err)
	}
	want := []string{"GET", "PUT", "GET"}
	if len(methods) != len(want) {
		t.Fatalf("methods=%v want=%v", methods, want)
	}
	for index := range want {
		if methods[index] != want[index] {
			t.Fatalf("methods=%v want=%v", methods, want)
		}
	}
}
