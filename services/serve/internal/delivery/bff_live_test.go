//go:build livebuild

package delivery_test

// This is the Serve read slice's end-to-end acceptance harness. It drives the
// real chain the product uses:
//
//	real CloudIdentity login -> the BFF's own Serve read handler -> Serve's owner
//	process over typed gRPC -> Serve's own opl_serve rows
//
// It uses the production CloudIdentity implementation, a real isolated
// PostgreSQL, the BFF's real client transport and the BFF's real route guard.
// Only the external Sub2API/Gateway HTTP boundary is simulated.
//
// Opt-in and therefore outside verify:local / verify:local:full:
//
//	OPL_OWNER_MIGRATION_TEST_ADMIN_DSN=<isolated postgres> \
//	  go test -tags=livebuild ./internal/delivery -run TestLiveServeReadThroughBFF -count=1 -v
//
// Seeded Serve rows exercise the read projection only. This is NOT a real
// deployment and no runtime observation is claimed.

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	bff "opl-cloud/apps/console-bff"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// bffServeProcess is a running Serve owner process this harness can read.
type bffServeProcess struct {
	address string
}

// ownerserviceConfigForBFF is the Serve process configuration this harness uses:
// the real CloudIdentity address and token, and only the Console BFF admitted as
// a peer.
func ownerserviceConfigForBFF(cloudIdentityAddr string) ownerservice.Config {
	return ownerservice.Config{
		Owner:              ownerservice.OwnerServe,
		TLS:                owneridentity.TLSConfig{AllowInsecureLocal: true},
		Peers:              map[ownerservice.Service]string{owneridentity.ConsoleBFF: liveServeToken},
		CloudIdentityAddr:  cloudIdentityAddr,
		CloudIdentityToken: liveIdentityToken,
	}
}

// startServeProcess starts Serve's real owner process on an ephemeral port over
// the already-installed owner database, and returns its address.
func startServeProcess(ctx context.Context, t *testing.T, runtimeDSN string, config ownerservice.Config) (*bffServeProcess, error) {
	t.Helper()
	bootstrap, err := ownerservice.StartWithDatabase(ctx, serveDatabase(t, runtimeDSN), config, noMigrations{}, configure(config))
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = bootstrap.Close() })
	if err := bootstrap.Server.Ready(ctx); err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	go func() { _ = bootstrap.Server.ServeOn(listener) }()
	t.Cleanup(bootstrap.Server.Stop)
	return &bffServeProcess{address: listener.Addr().String()}, nil
}

var _ = grpc.NewClient

// bffRequest issues one authenticated read against the BFF handler with the real
// session cookie the production authority issued.
func bffRequest(t *testing.T, base, method, path, cookie string, headers ...http.Header) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "opl_session", Value: cookie})
	}
	req.Header.Set(requestHeaderName, "live-bff-read")
	for _, values := range headers {
		for name, entries := range values {
			for _, value := range entries {
				req.Header.Add(name, value)
			}
		}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

const requestHeaderName = "x-opl-request-id"

func decodeInto(t *testing.T, resp *http.Response, out proto.Message) map[string]json.RawMessage {
	t.Helper()
	defer resp.Body.Close()
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Fatalf("response cache policy = %q, want no-store", resp.Header.Get("Cache-Control"))
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := publicjson.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode public response: %v; body: %s", err, raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	return fields
}

func assertBFFError(t *testing.T, resp *http.Response, code api.ErrorCodeEnum) {
	t.Helper()
	var failure api.Error
	fields := decodeInto(t, resp, &failure)
	if failure.GetCode() != code || failure.GetMessage() == "" || failure.GetRequestId() != "live-bff-read" {
		t.Fatalf("public error = %v", &failure)
	}
	if len(fields) != 3 {
		t.Fatalf("public error fields = %v, want code, message and requestId", fields)
	}
}

// TestLiveServeReadThroughBFF proves the Serve read slice end to end: the
// production CloudIdentity issues the session, the BFF's own handler guards and
// forwards it, and Serve answers its own delivery facts consistently.
func TestLiveServeReadThroughBFF(t *testing.T) {
	ctx := context.Background()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	chain := newLiveIdentityChain(t, ctx, dsn)

	serveDB, _, serveRuntimeDSN := fixture(t)
	// Seeded Serve-owned read-model rows (read projection only, not a Deploy).
	seedDeployment(t, serveDB, "ws-served", liveTenant, "dep-old", "superseded", "stopped", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	seedDeployment(t, serveDB, "ws-served", liveTenant, "dep-active", "active", "ready", "https://ws-served.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	seedDeployment(t, serveDB, "ws-pending", liveTenant, "dep-pending", "active", "starting", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	seedDeployment(t, serveDB, "ws-anon", liveTenant, "dep-anon", "active", "ready", "https://ws-anon.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_ANONYMOUS)

	config := ownerserviceConfigForBFF(chain.address)
	bootstrap, err := startServeProcess(ctx, t, serveRuntimeDSN, config)
	if err != nil {
		t.Fatalf("start serve owner process: %v", err)
	}
	reader, err := bff.DialServeReader(bootstrap.address, liveServeToken, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatalf("dial serve through the BFF client: %v", err)
	}
	t.Cleanup(reader.Close)

	// The BFF's own handler, not a bespoke test handler.
	server := httptest.NewServer(bff.NewServeDeliveryHandler(reader, chain.client))
	t.Cleanup(server.Close)

	tenantCookie := chain.cookies["serve"]
	foreignCookie := chain.cookies["other"]
	session, err := chain.client.Session(ctx, tenantCookie)
	if err != nil || session.GetCsrfToken() == "" {
		t.Fatalf("read the real session CSRF token: %v", err)
	}
	accessHeaders := http.Header{
		"X-Csrf-Token":    {session.GetCsrfToken()},
		"Idempotency-Key": {"live-bff-access"},
		"Origin":          {server.URL},
	}

	t.Run("history_through_bff", func(t *testing.T) {
		resp := bffRequest(t, server.URL, http.MethodGet, "/api/v2/workspaces/ws-served/deployments", tenantCookie)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var page api.DeploymentPage
		decodeInto(t, resp, &page)
		if len(page.Items) != 2 {
			t.Fatalf("history = %+v", page.Items)
		}
		// Newest first: the current Agent leads the history, the predecessor stays.
		if page.Items[0].Id != "dep-active" || page.Items[0].Status != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE {
			t.Fatalf("current deployment is not first: %+v", page.Items[0])
		}
		if page.Items[1].Id != "dep-old" || page.Items[1].Status != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_SUPERSEDED {
			t.Fatalf("history did not retain the superseded deployment: %+v", page.Items[1])
		}
		if page.Items[0].WorkspaceId != "ws-served" {
			t.Fatalf("deployment named another workspace: %+v", page.Items[0])
		}
	})

	t.Run("history_pagination_through_bff", func(t *testing.T) {
		firstResponse := bffRequest(t, server.URL, http.MethodGet, "/api/v2/workspaces/ws-served/deployments?limit=1", tenantCookie)
		if firstResponse.StatusCode != http.StatusOK {
			firstResponse.Body.Close()
			t.Fatalf("first-page status = %d", firstResponse.StatusCode)
		}
		var first api.DeploymentPage
		decodeInto(t, firstResponse, &first)
		if len(first.Items) != 1 || first.Items[0].GetId() != "dep-active" || first.GetNextCursor() == "" {
			t.Fatalf("first page = %v", &first)
		}
		secondResponse := bffRequest(t, server.URL, http.MethodGet, "/api/v2/workspaces/ws-served/deployments?limit=1&cursor="+url.QueryEscape(first.GetNextCursor()), tenantCookie)
		if secondResponse.StatusCode != http.StatusOK {
			secondResponse.Body.Close()
			t.Fatalf("second-page status = %d", secondResponse.StatusCode)
		}
		var second api.DeploymentPage
		decodeInto(t, secondResponse, &second)
		if len(second.Items) != 1 || second.Items[0].GetId() != "dep-old" || second.Items[0].GetId() == first.Items[0].GetId() || second.GetNextCursor() != "" {
			t.Fatalf("second page repeated or failed to terminate: %v", &second)
		}
	})

	t.Run("one_deployment_through_bff", func(t *testing.T) {
		resp := bffRequest(t, server.URL, http.MethodGet, "/api/v2/workspaces/ws-served/deployments/dep-active", tenantCookie)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var deployment api.Deployment
		fields := decodeInto(t, resp, &deployment)
		if string(fields["status"]) != `"active"` || string(fields["workspaceId"]) != `"ws-served"` {
			t.Fatalf("deployment public vocabulary = %v", fields)
		}
		if deployment.GetCreatedAt() == nil || deployment.GetUpdatedAt() == nil || deployment.GetDataCompatibility() == nil {
			t.Fatalf("deployment omitted required owner facts: %v", &deployment)
		}
		if deployment.Id != "dep-active" || deployment.GetRuntimeInstanceId() != "rt_dep-active" || deployment.Status != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE {
			t.Fatalf("deployment = %+v", deployment)
		}
	})

	for _, test := range []struct {
		name   string
		mutate func(http.Header)
		status int
		code   api.ErrorCodeEnum
	}{
		{"missing_csrf", func(h http.Header) { h.Del("X-CSRF-Token") }, http.StatusForbidden, api.ErrorCodeEnum_ERROR_CODE_ENUM_CSRF_INVALID},
		{"missing_idempotency_key", func(h http.Header) { h.Del("Idempotency-Key") }, http.StatusBadRequest, api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_REQUIRED},
		{"foreign_origin", func(h http.Header) { h.Set("Origin", "https://other.example") }, http.StatusForbidden, api.ErrorCodeEnum_ERROR_CODE_ENUM_ORIGIN_REJECTED},
	} {
		t.Run(test.name, func(t *testing.T) {
			headers := accessHeaders.Clone()
			test.mutate(headers)
			resp := bffRequest(t, server.URL, http.MethodPost, "/api/v2/workspaces/ws-served/access", tenantCookie, headers)
			if resp.StatusCode != test.status {
				resp.Body.Close()
				t.Fatalf("status = %d, want %d", resp.StatusCode, test.status)
			}
			assertBFFError(t, resp, test.code)
		})
	}

	t.Run("unconfigured_owner_is_unavailable", func(t *testing.T) {
		unconfigured := httptest.NewServer(bff.NewServeDeliveryHandler(nil, chain.client))
		defer unconfigured.Close()
		resp := bffRequest(t, unconfigured.URL, http.MethodGet, "/api/v2/workspaces/ws-served/deployments", tenantCookie)
		if resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
			resp.Body.Close()
			t.Fatalf("unconfigured owner status = %d, want 503 with retry guidance", resp.StatusCode)
		}
		assertBFFError(t, resp, api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE)
	})

	t.Run("access_through_bff_matches_current_deployment", func(t *testing.T) {
		resp := bffRequest(t, server.URL, http.MethodPost, "/api/v2/workspaces/ws-served/access", tenantCookie, accessHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var access api.WorkspaceAccess
		fields := decodeInto(t, resp, &access)
		if _, invented := fields["expiresAt"]; invented || access.GetExpiresAt() != nil {
			t.Fatal("access invented a provider expiry")
		}
		if access.WorkspaceId != "ws-served" || access.Url != "https://ws-served.example/app" || !access.GetApplicationCredentialsAvailable() {
			t.Fatalf("access = %+v", access)
		}
		if access.AuthenticationMode != api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_APPLICATION_LOGIN {
			t.Fatalf("access mode = %d", access.AuthenticationMode)
		}
	})

	t.Run("anonymous_exposure_reports_no_credentials", func(t *testing.T) {
		resp := bffRequest(t, server.URL, http.MethodPost, "/api/v2/workspaces/ws-anon/access", tenantCookie, accessHeaders)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var access api.WorkspaceAccess
		decodeInto(t, resp, &access)
		if access.AuthenticationMode != api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_ANONYMOUS || access.GetApplicationCredentialsAvailable() {
			t.Fatalf("anonymous access = %+v", access)
		}
	})

	t.Run("resource_ready_is_not_application_ready_through_bff", func(t *testing.T) {
		resp := bffRequest(t, server.URL, http.MethodPost, "/api/v2/workspaces/ws-pending/access", tenantCookie, accessHeaders)
		if resp.StatusCode != http.StatusConflict {
			resp.Body.Close()
			t.Fatalf("status = %d, want 409", resp.StatusCode)
		}
		assertBFFError(t, resp, api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE)
	})

	t.Run("empty_history_through_bff", func(t *testing.T) {
		resp := bffRequest(t, server.URL, http.MethodGet, "/api/v2/workspaces/ws-never-delivered/deployments", tenantCookie)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d", resp.StatusCode)
		}
		var page api.DeploymentPage
		fields := decodeInto(t, resp, &page)
		if string(fields["items"]) != "[]" {
			t.Fatalf("empty history must carry required items as [], got %s", fields["items"])
		}
		if len(page.Items) != 0 {
			t.Fatalf("never-delivered Workspace reported history: %+v", page.Items)
		}
	})

	t.Run("missing_deployment_is_not_found", func(t *testing.T) {
		resp := bffRequest(t, server.URL, http.MethodGet, "/api/v2/workspaces/ws-served/deployments/dep-missing", tenantCookie)
		if resp.StatusCode != http.StatusNotFound {
			resp.Body.Close()
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		assertBFFError(t, resp, api.ErrorCodeEnum_ERROR_CODE_ENUM_NOT_FOUND)
	})

	t.Run("cross_tenant_through_bff_is_forbidden", func(t *testing.T) {
		resp := bffRequest(t, server.URL, http.MethodGet, "/api/v2/workspaces/ws-served/deployments", foreignCookie)
		if resp.StatusCode != http.StatusForbidden {
			resp.Body.Close()
			t.Fatalf("status = %d, want 403", resp.StatusCode)
		}
		assertBFFError(t, resp, api.ErrorCodeEnum_ERROR_CODE_ENUM_FORBIDDEN)
	})

	t.Run("no_session_through_bff_is_unauthenticated", func(t *testing.T) {
		resp := bffRequest(t, server.URL, http.MethodGet, "/api/v2/workspaces/ws-served/deployments", "")
		if resp.StatusCode != http.StatusUnauthorized {
			resp.Body.Close()
			t.Fatalf("status = %d, want 401", resp.StatusCode)
		}
		assertBFFError(t, resp, api.ErrorCodeEnum_ERROR_CODE_ENUM_UNAUTHENTICATED)
	})

	t.Run("revoked_session_through_bff_is_refused", func(t *testing.T) {
		// Revoke the real session in the identity owner's own store. The identity
		// owner's member-removal RPC is its separate work package; revoking the
		// session row is a real change in that owner's data.
		ref := owneridentity.SessionReference(tenantCookie)
		tag, err := chain.db.ExecContext(ctx, `UPDATE tenant.sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, ref)
		if err != nil {
			t.Fatal(err)
		}
		if affected, err := tag.RowsAffected(); err != nil || affected != 1 {
			t.Fatalf("revoked %d sessions, want 1 (err=%v)", affected, err)
		}
		resp := bffRequest(t, server.URL, http.MethodGet, "/api/v2/workspaces/ws-served/deployments", tenantCookie)
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusUnauthorized, http.StatusForbidden, http.StatusServiceUnavailable:
			// Refused. The exact code depends on how the identity authority reports
			// a revoked session; the value is never served.
		default:
			t.Fatalf("a revoked session produced %d, want a refusal", resp.StatusCode)
		}
	})
}
