//go:build d3_sub2api_runtime

package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func d3RealClient(t *testing.T, transport http.RoundTripper) *Sub2APIHTTPClient {
	t.Helper()
	base := os.Getenv("OPL_D3_REAL_SUB2API_BASE_URL")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" {
		t.Fatal("D3 requires explicit isolated loopback Sub2API")
	}
	email, password := os.Getenv("OPL_D3_REAL_SUB2API_ADMIN_EMAIL"), os.Getenv("OPL_D3_REAL_SUB2API_ADMIN_PASSWORD")
	if email == "" || password == "" {
		t.Fatal("isolated admin credentials required")
	}
	client, err := NewSub2APIHTTPClient(Sub2APIConfig{BaseURL: base, AdminEmail: email, AdminPassword: password, Timeout: 10 * time.Second}, &http.Client{Transport: transport, Timeout: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return client
}

func d3RealUser(t *testing.T, client *Sub2APIHTTPClient) (int64, SessionDelegatedCredential) {
	t.Helper()
	email := fmt.Sprintf("d3-%d@example.test", time.Now().UnixNano())
	password := "isolated-d3-test-password"
	_, err := client.doAuthenticated(context.Background(), http.MethodPost, "/api/v1/admin/users", struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Balance  int    `json:"balance"`
	}{email, password, 10}, fmt.Sprintf("d3-user-%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	auth, err := client.AuthenticateUser(context.Background(), email, password)
	if err != nil {
		t.Fatal(err)
	}
	return auth.Identity.ID, SessionDelegatedCredential{Bearer: auth.AccessToken}
}

func d3RealCreateKey(client *Sub2APIHTTPClient, credential SessionDelegatedCredential, userID int64, name string) (Sub2APIWorkspaceKey, error) {
	body, err := client.request(context.Background(), http.MethodPost, "/api/v1/keys", struct {
		Name string `json:"name"`
	}{name}, credential.Bearer, fmt.Sprintf("d3-key-%d", time.Now().UnixNano()))
	if err != nil {
		return Sub2APIWorkspaceKey{}, err
	}
	return decodeSub2APIUserKey(body, userID, 0)
}

func d3RawKeyStatus(t *testing.T, client *Sub2APIHTTPClient, key string) int {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, client.baseURL+"/v1/models", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

type d3DropRevokeResponse struct{ once sync.Once }

func (d *d3DropRevokeResponse) RoundTrip(r *http.Request) (*http.Response, error) {
	response, err := http.DefaultTransport.RoundTrip(r)
	if err != nil {
		return response, err
	}
	drop := false
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/workspace-revocation") {
		d.once.Do(func() { drop = true })
	}
	if !drop {
		return response, nil
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	return nil, io.ErrUnexpectedEOF
}

func TestD3RealWorkspaceKeyRevocationBusinessChain(t *testing.T) {
	client := d3RealClient(t, nil)
	user, credential := d3RealUser(t, client)
	name := fmt.Sprintf("opl-workspace-d3-%d", user)
	key, err := d3RealCreateKey(client, credential, user, name)
	if err != nil {
		t.Fatal(err)
	}
	otherKey, err := d3RealCreateKey(client, credential, user, name+"-other")
	if err != nil {
		t.Fatal(err)
	}
	otherUser, otherCredential := d3RealUser(t, client)
	otherOwnerKey, err := d3RealCreateKey(client, otherCredential, otherUser, name)
	if err != nil {
		t.Fatal(err)
	}
	input := Sub2APIWorkspaceKeyRevokeInput{UserID: user, KeyID: key.ID, ExactName: name, LaunchOperationID: fmt.Sprintf("workspace-launch-d3-%d", user)}
	for _, wrong := range []Sub2APIWorkspaceKeyRevokeInput{
		{UserID: otherUser, KeyID: key.ID, ExactName: name, LaunchOperationID: input.LaunchOperationID},
		{UserID: user, KeyID: otherKey.ID, ExactName: name, LaunchOperationID: input.LaunchOperationID},
		{UserID: user, KeyID: key.ID, ExactName: name + "-wrong", LaunchOperationID: input.LaunchOperationID},
	} {
		if err := client.RevokeWorkspaceKey(context.Background(), wrong); err == nil {
			t.Fatal("wrong identity authorized revoke")
		}
	}
	// Customer authentication cannot dispatch the service/admin revocation route.
	if _, err := client.request(context.Background(), http.MethodPost, fmt.Sprintf("/api/v1/admin/api-keys/%d/workspace-revocation", key.ID), input, credential.Bearer, ""); err == nil {
		t.Fatal("customer session authorized admin revoke")
	}
	if status := d3RawKeyStatus(t, client, key.Key); status == 401 {
		t.Fatal("original raw key was unusable before revocation")
	}
	lost := d3RealClient(t, &d3DropRevokeResponse{})
	if err := lost.RevokeWorkspaceKey(context.Background(), input); err == nil {
		t.Fatal("lost response falsely confirmed closeout")
	}
	restarted := d3RealClient(t, nil)
	if err := restarted.RevokeWorkspaceKey(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if status := d3RawKeyStatus(t, client, key.Key); status != 401 {
		t.Fatalf("revoked warmed raw key still authenticated: status %d", status)
	}
	for _, pair := range []struct {
		id   int64
		cred SessionDelegatedCredential
		key  Sub2APIWorkspaceKey
	}{{user, credential, otherKey}, {otherUser, otherCredential, otherOwnerKey}} {
		if _, err := client.UserKey(context.Background(), pair.cred, pair.id, pair.key.ID); err != nil {
			t.Fatal("unrelated key was modified", err)
		}
	}
	if _, err := d3RealCreateKey(client, credential, user, name); err == nil {
		t.Fatal("late creation bypassed durable launch fence")
	}
	if _, err := client.UpdateUserKey(context.Background(), credential, user, otherKey.ID, Sub2APIUpdateKeyInput{Name: &name}); err == nil {
		t.Fatal("rename bypassed launch fence")
	}
	wrongLaunch := input
	wrongLaunch.LaunchOperationID += "-other"
	if err := client.RevokeWorkspaceKey(context.Background(), wrongLaunch); err == nil {
		t.Fatal("different launch reused revocation evidence")
	}
	absent := Sub2APIWorkspaceKeyRevokeInput{UserID: user, ExactName: name + "-never-created", LaunchOperationID: input.LaunchOperationID + "-absent"}
	if err := client.RevokeWorkspaceKey(context.Background(), absent); err != nil {
		t.Fatal(err)
	}
	if _, err := d3RealCreateKey(client, credential, user, absent.ExactName); err == nil {
		t.Fatal("zero-ID absence did not fence future creation")
	}
	disabledKey, err := d3RealCreateKey(client, credential, user, name+"-disabled")
	if err != nil {
		t.Fatal(err)
	}
	enabled := false
	disabledKey, err = client.UpdateUserKey(context.Background(), credential, user, disabledKey.ID, Sub2APIUpdateKeyInput{Enabled: &enabled})
	if err != nil || disabledKey.Status != "disabled" {
		t.Fatalf("disable original key: status=%s err=%v", disabledKey.Status, err)
	}
	if _, err := client.WorkspaceKeysForConvergence(context.Background(), user, disabledKey.Name); err == nil {
		t.Fatal("ordinary convergence accepted disabled key")
	}
	refs, err := client.WorkspaceKeysForRevocation(context.Background(), user, disabledKey.Name)
	if err != nil || len(refs) != 1 || refs[0].ID != disabledKey.ID || refs[0].UserID != user || refs[0].Name != disabledKey.Name || refs[0].Key != "" || refs[0].Status != "" {
		t.Fatalf("disabled original identity lookup failed: err=%v count=%d", err, len(refs))
	}
	disabledInput := Sub2APIWorkspaceKeyRevokeInput{UserID: user, KeyID: refs[0].ID, ExactName: refs[0].Name, LaunchOperationID: input.LaunchOperationID + "-disabled"}
	if err := client.RevokeWorkspaceKey(context.Background(), disabledInput); err != nil {
		t.Fatal("disabled original key could not be revoked", err)
	}
	refs, err = client.WorkspaceKeysForRevocation(context.Background(), user, disabledKey.Name)
	if err != nil || len(refs) != 0 {
		t.Fatalf("revoked disabled key still returned by owner: %v", err)
	}
	cacheKey, err := d3RealCreateKey(client, credential, user, name+"-cache-failure")
	if err != nil {
		t.Fatal(err)
	}
	cacheInput := Sub2APIWorkspaceKeyRevokeInput{UserID: user, KeyID: cacheKey.ID, ExactName: cacheKey.Name, LaunchOperationID: input.LaunchOperationID + "-cache"}
	cacheState, err := json.Marshal(cacheInput)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/tmp/d3-cache-failure-state.json", cacheState, 0600); err != nil {
		t.Fatal(err)
	}
	state, err := json.Marshal([]Sub2APIWorkspaceKeyRevokeInput{input, absent})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("/tmp/d3-revocation-state.json", state, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestD3RealWorkspaceKeyRevocationSurvivesServerRestart(t *testing.T) {
	data, err := os.ReadFile("/tmp/d3-revocation-state.json")
	if err != nil {
		t.Fatal("previous business chain state required", err)
	}
	var inputs []Sub2APIWorkspaceKeyRevokeInput
	if err := json.Unmarshal(data, &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 {
		t.Fatal("invalid restart fixture")
	}
	client := d3RealClient(t, nil)
	for _, input := range inputs {
		if err := client.RevokeWorkspaceKey(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
}

func d3CacheFailureInput(t *testing.T) Sub2APIWorkspaceKeyRevokeInput {
	t.Helper()
	data, err := os.ReadFile("/tmp/d3-cache-failure-state.json")
	if err != nil {
		t.Fatal(err)
	}
	var input Sub2APIWorkspaceKeyRevokeInput
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	return input
}

func TestD3RealWorkspaceKeyRevocationCacheUnavailable(t *testing.T) {
	client := d3RealClient(t, nil)
	client.accessToken = os.Getenv("OPL_D3_RUNTIME_PRIMED_ADMIN_TOKEN")
	if client.accessToken == "" {
		t.Fatal("existing isolated service session required for cache-commit fault test")
	}
	if err := client.RevokeWorkspaceKey(context.Background(), d3CacheFailureInput(t)); err == nil {
		t.Fatal("failed cache eviction falsely permitted closeout")
	}
}

func TestD3RealWorkspaceKeyRevocationCacheRecovery(t *testing.T) {
	client := d3RealClient(t, nil)
	if err := client.RevokeWorkspaceKey(context.Background(), d3CacheFailureInput(t)); err != nil {
		t.Fatal(err)
	}
}
