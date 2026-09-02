package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestWorkspaceServiceTargetAcceptsOnlyDNSServiceIdentity(t *testing.T) {
	raw, err := os.ReadFile("../../../../packages/contracts/opl-cloud-workspace-runtime-abi-contract.json")
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		WorkspaceWebUI struct {
			Port int `json:"port"`
		} `json:"workspaceWebUI"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	if workspaceRuntimeWebUIPort != strconv.Itoa(contract.WorkspaceWebUI.Port) {
		t.Fatalf("Control Plane Workspace Runtime port=%s, contract=%d", workspaceRuntimeWebUIPort, contract.WorkspaceWebUI.Port)
	}
	for _, valid := range []string{"opl-runtime-alpha", "opl-compute-alpha"} {
		target, err := workspaceServiceTarget(valid)
		if err != nil || target.String() != "http://"+valid+":"+workspaceRuntimeWebUIPort {
			t.Fatalf("workspaceServiceTarget(%q) = %v, %v", valid, target, err)
		}
	}
	for _, invalid := range []string{
		"http://127.0.0.1:8080", "https://runtime.example", "127.0.0.1", "runtime:8080",
		"user@runtime", "runtime/path", "Runtime", "-runtime", "runtime-", "runtime\nadmin", "runtime-alpha", "opl-runtime.alpha",
	} {
		if target, err := workspaceServiceTarget(invalid); err == nil {
			t.Fatalf("workspaceServiceTarget(%q) accepted %v", invalid, target)
		}
	}
}

func TestCreateSessionEvictsOldestActiveSessionAtPerUserBound(t *testing.T) {
	store := newMemoryTableStore()
	app, err := newControlPlaneAppWithStore(store)
	if err != nil {
		t.Fatal(err)
	}
	user := map[string]any{"id": "usr-session-bound", "accountId": "acct-session-bound"}
	now := time.Now().UTC()
	oldestKey := ""
	for index := 0; index < 8; index++ {
		key := sessionLookupKey(fmt.Sprintf("existing-%d", index))
		if index == 0 {
			oldestKey = key
		}
		if err := store.SaveSession(context.Background(), map[string]any{
			"id": key, "userId": user["id"], "csrf": "csrf", "expiresAt": now.Add(time.Duration(index+1) * time.Hour).Format(time.RFC3339),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := app.createSession(user, "delegated-user-token"); err != nil {
		t.Fatal(err)
	}
	sessions, err := store.ListSessionsByUser(context.Background(), "usr-session-bound")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 8 || sessions[oldestKey] != nil {
		t.Fatalf("bounded sessions = %d, oldest present=%v", len(sessions), sessions[oldestKey] != nil)
	}
}

func TestWorkspaceProxyStripsPlatformCredentials(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "http://workspace.medopl.cn/w/ws-alpha/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer platform-session")
	req.Header.Set("Cookie", "opl_session=platform-session; opl_ws_active=ws-alpha; "+workspaceGatewayRuntimeSessionCookieName("ws-alpha")+"=runtime-session")
	req.Header.Set("X-OPL-CSRF", "csrf")
	req.Header.Set("X-OPL-CSRF-Token", "csrf-token")
	stripWorkspaceProxyCredentials(req, "ws-alpha")
	for _, header := range []string{"Authorization", "X-OPL-CSRF", "X-OPL-CSRF-Token"} {
		if got := req.Header.Get(header); got != "" {
			t.Fatalf("proxy forwarded %s=%q", header, got)
		}
	}
	if got := req.Header.Get("Cookie"); got != workspaceRuntimeSessionCookieName+"=runtime-session" {
		t.Fatalf("proxy Runtime cookie = %q", got)
	}
	otherWorkspaceRequest := httptest.NewRequest(http.MethodGet, "http://workspace.medopl.cn/api/auth/user", nil)
	otherWorkspaceRequest.AddCookie(&http.Cookie{Name: workspaceGatewayRuntimeSessionCookieName("ws-beta"), Value: "other-runtime-session"})
	stripWorkspaceProxyCredentials(otherWorkspaceRequest, "ws-alpha")
	if got := otherWorkspaceRequest.Header.Get("Cookie"); got != "" {
		t.Fatalf("proxy forwarded another Workspace Runtime cookie = %q", got)
	}
}

func TestWorkspaceProxyProjectsOnlyWorkspaceScopedRuntimeSessionCookie(t *testing.T) {
	response := &http.Response{Header: http.Header{"Set-Cookie": []string{
		"platform=forbidden; Path=/",
		workspaceRuntimeSessionCookieName + "=runtime-session; Path=/; HttpOnly",
	}}}
	projectWorkspaceRuntimeSessionCookie(response, "ws-alpha")
	cookies := response.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("projected cookies = %#v", cookies)
	}
	cookie := cookies[0]
	if cookie.Name != workspaceGatewayRuntimeSessionCookieName("ws-alpha") || cookie.Value != "runtime-session" ||
		cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("projected Runtime cookie = %#v", cookie)
	}
}

func TestRetentionWorkerDefaultsOnAndAllowsExplicitDisable(t *testing.T) {
	t.Setenv("OPL_ARCHIVE_RETENTION_WORKER_ENABLED", "")
	if !retentionWorkerEnabled() {
		t.Fatal("retention worker defaulted off")
	}
	t.Setenv("OPL_ARCHIVE_RETENTION_WORKER_ENABLED", "false")
	if retentionWorkerEnabled() {
		t.Fatal("retention worker ignored explicit disable")
	}
}
