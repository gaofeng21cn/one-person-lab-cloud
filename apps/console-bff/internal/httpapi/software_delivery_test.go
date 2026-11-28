package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
)

type softwareTenantProbe struct {
	api.TenantProductServiceClient
	listResp    *api.OperatorGatewayAccountPage
	setReq      *api.SetPackageEntitlementRpcRequest
	callerReq   *api.ListCallerPackageEntitlementsRpcRequest
	callerItems []*api.OperatorPackageEntitlement
}

func (p *softwareTenantProbe) ListOperatorAccounts(_ context.Context, _ *api.ListOperatorAccountsRpcRequest, _ ...grpc.CallOption) (*api.OperatorGatewayAccountPage, error) {
	return p.listResp, nil
}

func (p *softwareTenantProbe) ListServiceEntitlements(_ context.Context, _ *api.ListServiceEntitlementsRpcRequest, _ ...grpc.CallOption) (*api.ServiceEntitlementList, error) {
	return &api.ServiceEntitlementList{}, nil
}

func (p *softwareTenantProbe) SetPackageEntitlement(_ context.Context, r *api.SetPackageEntitlementRpcRequest, _ ...grpc.CallOption) (*api.PackageEntitlementCommand, error) {
	p.setReq = r
	return &api.PackageEntitlementCommand{
		OperationId: "pkg-entitlement-op-1",
		AccountId:   r.GetTargetActorId(),
		PackageId:   r.GetBody().GetPackageId(),
		Status:      "succeeded",
		Enabled:     r.GetBody().GetEnabled(),
		ExpiresAt:   r.GetBody().GetExpiresAt(),
	}, nil
}

func (p *softwareTenantProbe) ListCallerPackageEntitlements(_ context.Context, r *api.ListCallerPackageEntitlementsRpcRequest, _ ...grpc.CallOption) (*api.CallerPackageEntitlementsReadback, error) {
	p.callerReq = r
	return &api.CallerPackageEntitlementsReadback{
		ActorId: "1042",
		Email:   "caller@example.test",
		Items:   p.callerItems,
	}, nil
}

func mapEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestOperatorPackageEntitlementWriteProjectionAndCatalog(t *testing.T) {
	now := timestamppb.New(time.Date(2026, 11, 28, 6, 0, 0, 0, time.UTC))
	tenant := &softwareTenantProbe{
		listResp: &api.OperatorGatewayAccountPage{
			Total:    1,
			Page:     1,
			PageSize: 20,
			Items: []*api.OperatorGatewayAccount{
				{
					ActorId:          "45",
					Email:            "software-user@example.test",
					Status:           "active",
					GatewayUpdatedAt: now,
					PackageEntitlements: []*api.OperatorPackageEntitlement{
						{
							PackageId: "occ",
							Enabled:   true,
							Reason:    "operator_requested",
							UpdatedAt: now,
						},
					},
				},
			},
		},
	}
	identity := &fakeIdentity{
		session: &api.Session{ActorId: "admin-1", TenantId: ptr("ignored"), CsrfToken: "csrf-admin"},
		decisions: map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
			operatorPrivilegeAction:                                                            activationPlatformDecision(operatorPrivilegeAction, api.OwnerEnum_OWNER_ENUM_CAPABILITY, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, "", "op-priv-1"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORACCOUNTS:         activationPlatformDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORACCOUNTS, api.OwnerEnum_OWNER_ENUM_TENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, "", "op-list-1"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETPACKAGEENTITLEMENT:        activationPlatformDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETPACKAGEENTITLEMENT, api.OwnerEnum_OWNER_ENUM_TENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, "45", "op-pkg-1"),
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORSOFTWAREPACKAGES: activationPlatformDecision(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORSOFTWAREPACKAGES, api.OwnerEnum_OWNER_ENUM_TENANT, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, "", "op-cat-1"),
		},
	}
	catalogJSON := `[{"packageId":"occ","displayName":"OPL Claude Code","summary":"Restricted CLI archive","version":"1.2.0","sha256":"` + strings.Repeat("a", 64) + `","sizeBytes":123,"repository":"gaofeng21cn/occ-pkg"}]`
	srv := &Server{
		identity: identity,
		tenant:   tenant,
		getenv: mapEnv(map[string]string{
			softwarePackagesEnv:         catalogJSON,
			softwareRegistryUsernameEnv: "secret-bot",
			softwareRegistryTokenEnv:    "ghp_supersecret_token_never_leak",
		}),
	}
	handler := srv.Handler()

	// 1. Accounts projection includes packageEntitlements.
	getReq := httptest.NewRequest(http.MethodGet, "/api/v2/operator/accounts", nil)
	getReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	getRes := httptest.NewRecorder()
	handler.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("GET accounts status=%d body=%s", getRes.Code, getRes.Body.String())
	}
	var listPayload struct {
		Data struct {
			Items []struct {
				PackageEntitlements []struct {
					PackageID string `json:"packageId"`
					Enabled   bool   `json:"enabled"`
				} `json:"packageEntitlements"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(getRes.Body.Bytes(), &listPayload); err != nil {
		t.Fatal(err)
	}
	if len(listPayload.Data.Items) != 1 || len(listPayload.Data.Items[0].PackageEntitlements) != 1 || listPayload.Data.Items[0].PackageEntitlements[0].PackageID != "occ" || !listPayload.Data.Items[0].PackageEntitlements[0].Enabled {
		t.Fatalf("unexpected packageEntitlements projection: %+v", listPayload.Data.Items)
	}

	// 2. Catalog read returns public metadata without secrets or repository internals.
	catReq := httptest.NewRequest(http.MethodGet, "/api/v2/operator/software-packages", nil)
	catReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	catRes := httptest.NewRecorder()
	handler.ServeHTTP(catRes, catReq)
	if catRes.Code != http.StatusOK {
		t.Fatalf("GET catalog status=%d body=%s", catRes.Code, catRes.Body.String())
	}
	if strings.Contains(catRes.Body.String(), "ghp_supersecret") || strings.Contains(catRes.Body.String(), "secret-bot") || strings.Contains(catRes.Body.String(), "repository") {
		t.Fatalf("catalog response leaked registry internals: %s", catRes.Body.String())
	}

	// 3. Operator package entitlement write validates strict JSON & forwards to Tenant owner.
	writeReq := httptest.NewRequest(http.MethodPost, "/api/v2/operator/accounts/45/package-entitlements",
		strings.NewReader(`{"confirmationAccountId":"45","packageId":"occ","enabled":true,"reason":"grant_occ"}`))
	writeReq.Header.Set("Content-Type", "application/json")
	writeReq.Header.Set("X-CSRF-Token", "csrf-admin")
	writeReq.Header.Set("Idempotency-Key", "pkg-idem-1")
	writeReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	writeRes := httptest.NewRecorder()
	handler.ServeHTTP(writeRes, writeReq)
	if writeRes.Code != http.StatusOK {
		t.Fatalf("POST package-entitlements status=%d body=%s", writeRes.Code, writeRes.Body.String())
	}
	if tenant.setReq == nil || tenant.setReq.GetTargetActorId() != "45" || tenant.setReq.GetBody().GetPackageId() != "occ" || !tenant.setReq.GetBody().GetEnabled() {
		t.Fatalf("unexpected SetPackageEntitlement request: %+v", tenant.setReq)
	}

	// 4. Unconfigured catalog returns configured=false cleanly.
	srvUnconfigured := (&Server{identity: identity, tenant: tenant, getenv: mapEnv(nil)}).Handler()
	catEmptyReq := httptest.NewRequest(http.MethodGet, "/api/v2/operator/software-packages", nil)
	catEmptyReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-admin"})
	catEmptyRes := httptest.NewRecorder()
	srvUnconfigured.ServeHTTP(catEmptyRes, catEmptyReq)
	if catEmptyRes.Code != http.StatusOK || !strings.Contains(catEmptyRes.Body.String(), `"configured":false`) {
		t.Fatalf("expected configured=false catalog response, got %d %s", catEmptyRes.Code, catEmptyRes.Body.String())
	}
}

func TestStage1SoftwareDeliveryGHCRChallengeCacheAndSecurity(t *testing.T) {
	payloadBytes := []byte("OPL-RESTRICTED-PACKAGE-ARCHIVE-V1.2.3\n")
	payloadSum := sha256.Sum256(payloadBytes)
	payloadSHA := hex.EncodeToString(payloadSum[:])

	var tokenCalls atomic.Int32
	var blobCalls atomic.Int32
	var serveCorrupted atomic.Bool

	var registryMux = http.NewServeMux()
	var upstream *httptest.Server
	registryMux.HandleFunc("GET /token", func(w http.ResponseWriter, r *http.Request) {
		user, pass, hasBasic := r.BasicAuth()
		if !hasBasic || user != "ghcr-reader" || pass != "ghcr-secret" {
			http.Error(w, "invalid basic auth", http.StatusUnauthorized)
			return
		}
		if r.URL.Query().Get("service") != "ghcr.io" || !strings.Contains(r.URL.Query().Get("scope"), "repository:gaofeng21cn/occ-pkg:pull") {
			http.Error(w, "unexpected token scope/service", http.StatusBadRequest)
			return
		}
		tokenCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"oci-short-lived-token"}`))
	})
	registryMux.HandleFunc("GET /v2/gaofeng21cn/occ-pkg/blobs/{digest}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oci-short-lived-token" {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="ghcr.io",scope="repository:gaofeng21cn/occ-pkg:pull"`, upstream.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		blobCalls.Add(1)
		if serveCorrupted.Load() {
			_, _ = w.Write([]byte("CORRUPTED-TAMPERED-ARCHIVE"))
			return
		}
		_, _ = w.Write(payloadBytes)
	})
	upstream = httptest.NewServer(registryMux)
	defer upstream.Close()

	cacheRoot := t.TempDir()
	catalogJSON := fmt.Sprintf(
		`[{"packageId":"occ","displayName":"OPL Claude Code","summary":"Restricted CLI archive","version":"1.2.3","sha256":"%s","sizeBytes":%d,"filename":"occ-1.2.3.tar.gz","blobUrl":"%s/v2/gaofeng21cn/occ-pkg/blobs/sha256:%s"}]`,
		payloadSHA, len(payloadBytes), upstream.URL, payloadSHA,
	)
	tenant := &softwareTenantProbe{
		callerItems: []*api.OperatorPackageEntitlement{
			{PackageId: "occ", Enabled: true, Reason: "paid_customer"},
		},
	}
	identity := &fakeIdentity{
		session: &api.Session{ActorId: "1042", CsrfToken: "csrf-caller"},
	}
	srv := &Server{
		identity:           identity,
		tenant:             tenant,
		softwareHTTPClient: upstream.Client(),
		getenv: mapEnv(map[string]string{
			softwarePackagesEnv:         catalogJSON,
			softwareRegistryUsernameEnv: "ghcr-reader",
			softwareRegistryTokenEnv:    "ghcr-secret",
			softwareCacheRootEnv:        cacheRoot,
		}),
	}
	handler := srv.Handler()

	// 1. Cross-site request to entitlements is rejected before auth.
	crossReq := httptest.NewRequest(http.MethodGet, "/api/v2/software/entitlements", nil)
	crossReq.Header.Set("Sec-Fetch-Site", "cross-site")
	crossRes := httptest.NewRecorder()
	handler.ServeHTTP(crossRes, crossReq)
	if crossRes.Code != http.StatusForbidden {
		t.Fatalf("cross-site entitlements status=%d want 403", crossRes.Code)
	}

	// 2. Bearer entitlements read merges catalog metadata cleanly.
	entReq := httptest.NewRequest(http.MethodGet, "/api/v2/software/entitlements", nil)
	entReq.Header.Set("Authorization", "Bearer gw-token-1")
	entRes := httptest.NewRecorder()
	handler.ServeHTTP(entRes, entReq)
	if entRes.Code != http.StatusOK || !strings.Contains(entRes.Body.String(), `"catalogAvailable":true`) || !strings.Contains(entRes.Body.String(), payloadSHA) {
		t.Fatalf("GET software/entitlements failed: status=%d body=%s", entRes.Code, entRes.Body.String())
	}

	// 3. First Bearer download triggers OCI Bearer challenge, verifies sha256, caches on PVC, and streams archive.
	dlReq1 := httptest.NewRequest(http.MethodPost, "/api/v2/software/packages/occ/download", nil)
	dlReq1.Header.Set("Authorization", "Bearer gw-token-1")
	dlRes1 := httptest.NewRecorder()
	handler.ServeHTTP(dlRes1, dlReq1)
	if dlRes1.Code != http.StatusOK || dlRes1.Body.String() != string(payloadBytes) {
		t.Fatalf("first download failed: status=%d body=%q", dlRes1.Code, dlRes1.Body.String())
	}
	if dlRes1.Header().Get("X-Content-Sha256") != payloadSHA || dlRes1.Header().Get("Content-Disposition") != `attachment; filename="occ-1.2.3.tar.gz"` {
		t.Fatalf("unexpected download headers: %v", dlRes1.Header())
	}
	if tokenCalls.Load() != 1 || blobCalls.Load() != 1 {
		t.Fatalf("expected 1 token & 1 blob call on cache miss, got token=%d blob=%d", tokenCalls.Load(), blobCalls.Load())
	}

	// 4. Second download hits PVC cache without calling upstream registry again.
	dlReq2 := httptest.NewRequest(http.MethodPost, "/api/v2/software/packages/occ/download", nil)
	dlReq2.Header.Set("Authorization", "Bearer gw-token-1")
	dlRes2 := httptest.NewRecorder()
	handler.ServeHTTP(dlRes2, dlReq2)
	if dlRes2.Code != http.StatusOK || dlRes2.Body.String() != string(payloadBytes) {
		t.Fatalf("cached download failed: status=%d body=%q", dlRes2.Code, dlRes2.Body.String())
	}
	if blobCalls.Load() != 1 {
		t.Fatalf("expected cached hit without upstream blob fetch, got blobCalls=%d", blobCalls.Load())
	}

	// 5. Session download requires CSRF header.
	csrfFailReq := httptest.NewRequest(http.MethodPost, "/api/v2/software/packages/occ/download", nil)
	csrfFailReq.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-caller"})
	csrfFailRes := httptest.NewRecorder()
	handler.ServeHTTP(csrfFailRes, csrfFailReq)
	if csrfFailRes.Code != http.StatusForbidden {
		t.Fatalf("session download without CSRF status=%d want 403", csrfFailRes.Code)
	}

	// 6. Unentitled package download fails closed with 403 without fetching upstream.
	deniedReq := httptest.NewRequest(http.MethodPost, "/api/v2/software/packages/glkvm-native/download", nil)
	deniedReq.Header.Set("Authorization", "Bearer gw-token-1")
	deniedRes := httptest.NewRecorder()
	handler.ServeHTTP(deniedRes, deniedReq)
	if deniedRes.Code != http.StatusForbidden {
		t.Fatalf("unentitled package status=%d want 403", deniedRes.Code)
	}

	// 7. Digest corruption fails closed with 502 and leaves zero cached file.
	serveCorrupted.Store(true)
	if err := os.Remove(filepath.Join(cacheRoot, "blobs", "sha256", payloadSHA)); err != nil {
		t.Fatal(err)
	}
	corruptReq := httptest.NewRequest(http.MethodPost, "/api/v2/software/packages/occ/download", nil)
	corruptReq.Header.Set("Authorization", "Bearer gw-token-1")
	corruptRes := httptest.NewRecorder()
	handler.ServeHTTP(corruptRes, corruptReq)
	if corruptRes.Code != http.StatusBadGateway {
		t.Fatalf("corrupted digest status=%d want 502 body=%s", corruptRes.Code, corruptRes.Body.String())
	}
	entries, err := os.ReadDir(filepath.Join(cacheRoot, "blobs", "sha256"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("expected corrupted temp file cleanup leaving 0 files, got entries=%v err=%v", entries, err)
	}

	// 8. Missing OPL_SOFTWARE_CACHE_ROOT fails closed with 503.
	noCacheSrv := &Server{
		identity:           identity,
		tenant:             tenant,
		softwareHTTPClient: upstream.Client(),
		getenv:             mapEnv(map[string]string{softwarePackagesEnv: catalogJSON}),
	}
	noCacheReq := httptest.NewRequest(http.MethodPost, "/api/v2/software/packages/occ/download", nil)
	noCacheReq.Header.Set("Authorization", "Bearer gw-token-1")
	noCacheRes := httptest.NewRecorder()
	noCacheSrv.Handler().ServeHTTP(noCacheRes, noCacheReq)
	if noCacheRes.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured cache status=%d want 503", noCacheRes.Code)
	}
}
