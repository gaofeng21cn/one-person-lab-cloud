//go:build livebuild

package launch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lib/pq"
	"google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	bff "opl-cloud/apps/console-bff"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	capability "opl-cloud/services/capability/catalog"
	capabilitymigrations "opl-cloud/services/capability/migrations"
	fabricmigrations "opl-cloud/services/fabric/ownermigrations"
	"opl-cloud/services/gateway-integration/gatewaymigrations"
	cloudidentity "opl-cloud/services/gateway-integration/identity"
	identitymigrations "opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	catalog "opl-cloud/services/resource-catalog/catalog"
	catalogmigrations "opl-cloud/services/resource-catalog/migrations"
	servemigrations "opl-cloud/services/serve/migrations"
	"opl-cloud/services/workspace/migrations"
)

const qualificationNodeImage = "node:22-bookworm-slim@sha256:d649c27dae7ba0137b3cef5dd75baa422c08dc3d9e3fc0c23dfb172dc3cc6436"

// This is a real first application delivery on an isolated Linux host. By
// default, only the external login backend and the admitted Build fixture are
// fixtures. With OPL_LOCAL_FIRST_BUILD_HANDOFF, the test consumes the exact
// PackageVersion/Build/CapabilityVersion readback emitted by the live Build
// owner test and keeps the loopback registry alive for this run. CloudIdentity,
// Catalog, Ledger, Capability, Workspace, Serve and Fabric use their real
// handlers, databases and authenticated transport. Fabric and Serve are the
// production executables; quota, OCI image, container, mount, HTTP access,
// failure observation and recovery are actual host operations. The handoff
// mode requires a digest-pinned runnable Runtime Release and validates a real
// Agent response; it does not claim Gateway, paid activation or production
// deployment.
func TestLocalFirstApplicationDeploymentQualification(t *testing.T) {
	if os.Getenv("OPL_LOCAL_FIRST_DEPLOY_QUALIFICATION") != "1" {
		t.Skip("opt in with OPL_LOCAL_FIRST_DEPLOY_QUALIFICATION=1 on the isolated Linux qualification host")
	}
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		t.Fatal("qualification requires Linux root for the real project quota backend")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 12*time.Minute)
	defer cancel()
	fabricBinary, serveBinary := os.Getenv("OPL_QUALIFICATION_FABRIC_BINARY"), os.Getenv("OPL_QUALIFICATION_SERVE_BINARY")
	for _, p := range []string{fabricBinary, serveBinary} {
		info, err := os.Stat(p)
		if err != nil || !filepath.IsAbs(p) || info.IsDir() || info.Mode()&0111 == 0 {
			t.Fatalf("a precompiled executable is required: %q", p)
		}
	}
	bundle := filepath.Dir(fabricBinary)
	storageRoot := os.Getenv("OPL_TEST_PROJECT_QUOTA_ROOT")
	if filepath.Dir(serveBinary) != bundle || storageRoot != filepath.Join(bundle, "storage") {
		t.Fatal("executables and quota root must belong to the same isolated qualification bundle")
	}
	if out, err := exec.CommandContext(ctx, "mountpoint", "-q", storageRoot).CombinedOutput(); err != nil {
		t.Fatalf("real quota filesystem is not mounted: %v %s", err, out)
	}
	qDocker(t, ctx, "info", "--format", "{{.ServerVersion}}")
	handoffPath := strings.TrimSpace(os.Getenv("OPL_LOCAL_FIRST_BUILD_HANDOFF"))
	platform := "linux/amd64"
	if handoffPath == "" {
		observedPlatform := strings.TrimSpace(string(qDocker(t, ctx, "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", qualificationNodeImage)))
		if observedPlatform != platform {
			t.Fatalf("qualification image platform=%q", observedPlatform)
		}
		canonicalImage := "node@" + strings.Split(qualificationNodeImage, "@")[1]
		if string(qDocker(t, ctx, "image", "inspect", "--format", "{{.Id}}", canonicalImage)) != string(qDocker(t, ctx, "image", "inspect", "--format", "{{.Id}}", qualificationNodeImage)) {
			t.Fatal("canonical application digest does not identify the pre-pulled immutable image")
		}
	}
	dsn := os.Getenv("OPL_OWNER_MIGRATION_TEST_ADMIN_DSN")
	if dsn == "" || os.Getenv("OPL_POSTGRES_TESTS") != "1" {
		t.Fatal("isolated PostgreSQL admin DSN and OPL_POSTGRES_TESTS=1 are required")
	}
	legacyDSN := qLegacyDatabase(t, ctx, dsn)
	identitySource, err := identitymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	tenantDB := liveOwnerDB(t, ctx, dsn, "tenant", identitySource)
	if _, err = tenantDB.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name) VALUES('tenant-live','Qualification Tenant'); INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('qualification-admin','tenant-live','101','admin')`); err != nil {
		t.Fatal(err)
	}
	gateway, err := cloudidentity.NewGatewayWithDirectory(liveGatewayFixture(t), &cloudidentity.GatewayDirectory{Email: "directory@example.test", Password: "isolated-password"})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := cloudidentity.New(tenantDB, gateway, bytes.Repeat([]byte("q"), 32), []string{"103"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	gatewaySource, err := gatewaymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	identity.GatewayStore, err = cloudidentity.NewGatewayStore(liveOwnerDB(t, ctx, dsn, "gateway", gatewaySource))
	if err != nil {
		t.Fatal(err)
	}
	ia := liveOwnerServer(t, owneridentity.Tenant, []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.Workspace.Service(), owneridentity.ResourceCatalog.Service(), owneridentity.Fabric.Service(), owneridentity.Ledger.Service(), owneridentity.Serve.Service(), owneridentity.Capability.Service()}, identity.Register)
	auth := func(owner owneridentity.Owner) *ownerservice.Authorizer {
		return ownerservice.NewAuthorizer(owner, api.NewCloudIdentityAuthorizationClient(liveDial(t, ia, owner, owneridentity.Tenant, livePeerToken)))
	}
	cs, err := catalogmigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	catalogDB := liveOwnerDB(t, ctx, dsn, "resource_catalog", cs)
	catalogOwner, err := catalog.New(catalogDB, auth(owneridentity.ResourceCatalog), catalog.Profile{Provider: "local-docker", Region: "local", BillingMode: "LOCAL_NO_CHARGE", ProviderCapabilityVersion: "provider/v1"})
	if err != nil {
		t.Fatal(err)
	}
	ca := liveOwnerServer(t, owneridentity.ResourceCatalog, []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.Workspace.Service(), owneridentity.Fabric.Service(), owneridentity.Ledger.Service()}, catalogOwner.Register)
	caps, err := capabilitymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	capabilityDB := liveOwnerDB(t, ctx, dsn, "capability", caps)
	version, realBuild := qSeedBuildFixture(t, ctx, capabilityDB, platform, handoffPath)
	probeImage := qualificationNodeImage
	if realBuild {
		probeImage = version.GetArtifact().GetRepository() + "@" + version.GetArtifact().GetDigest()
		// The supplied Runtime already carries Node. Verify that exact image can
		// execute the provider's Node probe before allocating any resources.
		qDocker(t, ctx, "run", "--rm", "--pull", "never", "--entrypoint", "node", probeImage, "--version")
	}
	capOwner := &capability.Service{DB: capabilityDB, Authorize: auth(owneridentity.Capability).Authorize}
	capa := liveOwnerServer(t, owneridentity.Capability, []owneridentity.Service{owneridentity.Workspace.Service(), owneridentity.Serve.Service()}, capOwner.Register)
	fs, err := fabricmigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	fabricHarness, fabricDB := qOwnerProcessDB(t, ctx, dsn, "fabric", fs)
	ss, err := servemigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	serveHarness, serveDB := qOwnerProcessDB(t, ctx, dsn, "serve", ss)
	fa, fh, sa := qAddress(t), qAddress(t), qAddress(t)
	ws, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	workspaceDB := liveOwnerDB(t, ctx, dsn, "workspace", ws)
	wc := api.NewCatalogCoordinationClient(liveDial(t, ca, owneridentity.Workspace, owneridentity.ResourceCatalog, livePeerToken))
	wf := api.NewFabricCoordinationClient(liveDial(t, fa, owneridentity.Workspace, owneridentity.Fabric, livePeerToken))
	wi := api.NewCloudIdentityAuthorizationClient(liveDial(t, ia, owneridentity.Workspace, owneridentity.Tenant, livePeerToken))
	workspaceOwner, err := New(workspaceDB, auth(owneridentity.Workspace), wc, wf, wi)
	if err != nil {
		t.Fatal(err)
	}
	wa := liveOwnerServer(t, owneridentity.Workspace, []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.Tenant.Service(), owneridentity.Ledger.Service()}, workspaceOwner.Register)
	identity.WorkspaceCommit = api.NewOwnerCommitReadbackClient(liveDial(t, wa, owneridentity.Tenant, owneridentity.Workspace, livePeerToken))
	ledgerDB, ledgerOwner := liveLedgerDB(t, ctx, dsn)
	ledgerOwner.Authorizer = auth(owneridentity.Ledger)
	ledgerOwner.Catalog = api.NewCatalogCoordinationClient(liveDial(t, ca, owneridentity.Ledger, owneridentity.ResourceCatalog, livePeerToken))
	ledgerOwner.Workspace = api.NewOwnerCommitReadbackClient(liveDial(t, wa, owneridentity.Ledger, owneridentity.Workspace, livePeerToken))
	ledgerServer, err := ledgerOwner.NewGRPC(ownerservice.Config{Owner: owneridentity.Ledger, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Workspace.Service(): livePeerToken, owneridentity.Fabric.Service(): livePeerToken}})
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go ledgerServer.Serve(listener)
	t.Cleanup(ledgerServer.Stop)
	wl := api.NewLedgerCoordinationClient(liveDial(t, listener.Addr().String(), owneridentity.Workspace, owneridentity.Ledger, livePeerToken))
	workspaceOwner.Ledger = wl
	workspaceOwner.Capability = api.NewCapabilityProductServiceClient(liveDial(t, capa, owneridentity.Workspace, owneridentity.Capability, livePeerToken))
	workspaceOwner.Serve = api.NewServeAgentCoordinationClient(liveDial(t, sa, owneridentity.Workspace, owneridentity.Serve, livePeerToken))
	capConn := liveDial(t, sa, owneridentity.Capability, owneridentity.Serve, livePeerToken)
	capOwner.ServeCommit = api.NewOwnerCommitReadbackClient(capConn)
	capOwner.ServeUsage = api.NewClaimUsageReadbackClient(capConn)
	secretRoot, err := os.MkdirTemp(bundle, "fixture-secrets-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(secretRoot); err != nil {
			t.Errorf("remove isolated application credential store: %v", err)
		}
	})
	const serveToken = "qualification-serve-http-transport-000001"
	const serveKey = "qualification-serve-http-signature-000001"
	profile := `{"schemaVersion":1,"profileId":"local/profile","capabilityVersion":"provider/v1","region":"local","accountBindings":[{"tenantId":"tenant-live","accountId":"qualification-account"}],"packages":[{"id":"basic","name":"Basic","available":true,"compute":{"id":"local-basic","server":"2c4g","cpu":2,"memoryGb":4,"diskGb":10,"instanceType":"local-2c4g"},"storage":{"id":"local-storage","sizeGb":10,"quotaPolicy":"linux-project"}}]}`
	common := map[string]string{"OPL_GRPC_INSECURE_LOCAL": "1", "OPL_POSTGRES_TESTS": "1", "PGSSLMODE": "disable", "OPL_CLOUD_IDENTITY_URL": ia, "OPL_CLOUD_IDENTITY_TOKEN": livePeerToken, "OPL_AIONUI_ADMIN_PASSWORD_SEED": "isolated-qualification-application-password-seed"}
	fabricEnv := qEnv(common, map[string]string{
		"DATABASE_URL": legacyDSN, "OPL_FABRIC_DATABASE_URL": fabricHarness.OwnerDSN, "FABRIC_ADDR": fh, "OPL_FABRIC_ADDR": fa, "OPL_FABRIC_PROVIDER": "local-docker",
		"OPL_FABRIC_PEER_TOKENS": qPeers("workspace", "serve"), "OPL_RESOURCE_CATALOG_ADDR": ca, "OPL_RESOURCE_CATALOG_TOKEN": livePeerToken, "OPL_LEDGER_ADDR": listener.Addr().String(), "OPL_LEDGER_TOKEN": livePeerToken,
		"OPL_INTERNAL_SERVICE_TOKEN": "qualification-control-transport-000001", "OPL_FABRIC_RUNNER_SERVICE_TOKEN": "qualification-runner-transport-000001", "OPL_FABRIC_CAPABILITY_KEY": "qualification-control-signature-000001", "OPL_FABRIC_SERVE_SERVICE_TOKEN": serveToken, "OPL_FABRIC_SERVE_CAPABILITY_KEY": serveKey,
		"OPL_FABRIC_LOCAL_DOCKER_STORAGE_ROOT": storageRoot, "OPL_FABRIC_LOCAL_DOCKER_SECRET_ROOT": secretRoot, "OPL_FABRIC_LOCAL_DOCKER_HOST": "127.0.0.1", "OPL_FABRIC_LOCAL_DOCKER_PUBLISH_HOST": "127.0.0.1", "OPL_FABRIC_LOCAL_DOCKER_PROBE_IMAGE": probeImage, "OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON": profile,
	})
	serveEnv := qEnv(common, map[string]string{"DATABASE_URL": serveHarness.OwnerDSN, "OPL_SERVE_ADDR": sa, "OPL_SERVE_PEER_TOKENS": qPeers("workspace", "capability", string(owneridentity.ConsoleBFF)), "OPL_CAPABILITY_ADDR": capa, "OPL_CAPABILITY_TOKEN": livePeerToken, "OPL_FABRIC_COORDINATION_ADDR": fa, "OPL_FABRIC_COORDINATION_TOKEN": livePeerToken, "OPL_FABRIC_APPLICATION_URL": "http://" + fh, "OPL_FABRIC_SERVE_SERVICE_TOKEN": serveToken, "OPL_FABRIC_SERVE_CAPABILITY_KEY": serveKey})
	fabricProcess := qProcess(t, ctx, fabricBinary, fabricEnv, filepath.Join(bundle, "fabric.log"))
	serveProcess := qProcess(t, ctx, serveBinary, serveEnv, filepath.Join(bundle, "serve.log"))
	qHealthy(t, ctx, fa, owneridentity.Fabric)
	qHealthy(t, ctx, sa, owneridentity.Serve)
	identityClient, err := bff.DialIdentity(ia, livePeerToken, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(identityClient.Close)
	serveReader, err := bff.DialServeReader(sa, livePeerToken, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(serveReader.Close)
	serveFront := httptest.NewServer(bff.NewServeDeliveryHandler(serveReader, identityClient))
	t.Cleanup(serveFront.Close)
	catalogFront := httptest.NewServer(bff.NewCatalogHandler(api.NewResourceCatalogProductServiceClient(liveDialService(t, ca, owneridentity.ConsoleBFF, owneridentity.ResourceCatalog.Service(), livePeerToken)), identityClient))
	t.Cleanup(catalogFront.Close)
	workspaceFront := httptest.NewServer(bff.NewWorkspaceHandler(api.NewWorkspaceProductServiceClient(liveDialService(t, wa, owneridentity.ConsoleBFF, owneridentity.Workspace.Service(), livePeerToken)), identityClient))
	t.Cleanup(workspaceFront.Close)
	cookies, csrf := map[string]string{}, map[string]string{}
	for _, email := range []string{"platform-admin@example.test", "tenant-admin@example.test"} {
		_, challenge, e := identityClient.LoginContext(ctx)
		if e != nil {
			t.Fatal(e)
		}
		session, cookie, e := identityClient.Login(ctx, challenge, &api.LoginRequest{Username: email, Password: "isolated-password"})
		if e != nil {
			t.Fatal(e)
		}
		cookies[email], csrf[email] = cookie, session.CsrfToken
	}
	request := func(base, email, method, path, key, body string) (int, []byte) {
		t.Helper()
		r, e := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.AddCookie(&http.Cookie{Name: "opl_console_session", Value: cookies[email]})
		r.Header.Set("X-CSRF-Token", csrf[email])
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", base)
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		if e != nil {
			t.Fatal(e)
		}
		return res.StatusCode, raw
	}
	post := func(email, path, key, body string) map[string]any {
		t.Helper()
		code, raw := request(catalogFront.URL, email, "POST", path, key, body)
		if code < 200 || code >= 300 {
			t.Fatalf("%s %d %s", path, code, raw)
		}
		var out map[string]any
		if json.Unmarshal(raw, &out) != nil {
			t.Fatal(string(raw))
		}
		return out
	}
	now := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	admin, customer := "platform-admin@example.test", "tenant-admin@example.test"
	cp := post(admin, "/api/v2/admin/catalog/compute-plans", "cp", `{"name":"basic","vcpus":2,"memoryMiB":4096,"providerProfileId":"local/profile","providerSkuId":"local-basic","providerCapabilityVersion":"provider/v1","validFrom":"`+now+`"}`)["id"].(string)
	sp := post(admin, "/api/v2/admin/catalog/storage-plans", "sp", `{"name":"standard","capacityGiB":10,"providerProfileId":"local/profile","providerSkuId":"local-storage","shrinkSupported":true,"validFrom":"`+now+`"}`)["id"].(string)
	rp := post(admin, "/api/v2/admin/catalog/retention-policies", "rp", `{"versionLabel":"retention","customerTerms":"destroy after confirmation"}`)["id"].(string)
	post(admin, "/api/v2/admin/catalog/refund-policies", "refund", `{"versionLabel":"refund","algorithm":"workspace-delete-refund-v1","retentionPolicyVersionId":"`+rp+`","customerTerms":"qualification policy","validFrom":"`+now+`"}`)
	post(admin, "/api/v2/admin/catalog/price-policies", "price", `{"versionLabel":"price","periodMonths":1,"computeMonthlyUSDMicros":"0","storageMonthlyUSDMicros":"0","productMonthlyUSDMicros":"0","validFrom":"`+now+`","computePlanId":"`+cp+`","storagePlanId":"`+sp+`","renewalPolicy":{"version":"renewal-policy/v1","trigger":"manual_or_explicitly_consented_automatic","effectiveStart":"previous_paid_through","months":1,"usesAcceptedPriceSnapshot":true},"planChangePolicyVersion":"workspace-plan-change-v1"}`)
	quote := post(customer, "/api/v2/quotes", "quote", `{"purpose":"deploy","capabilityVersionId":"`+version.Id+`","computePlanId":"`+cp+`","storagePlanId":"`+sp+`","periodMonths":1}`)["id"].(string)
	body := `{"name":"Local first deployment qualification","quoteId":"` + quote + `","renewalMode":"manual"}`
	code, raw := request(workspaceFront.URL, customer, "POST", "/api/v2/workspaces", "qualification-create", body)
	if code != 202 {
		t.Fatalf("create %d %s", code, raw)
	}
	var accepted map[string]any
	if json.Unmarshal(raw, &accepted) != nil {
		t.Fatal(string(raw))
	}
	opID, wid := accepted["operationId"].(string), accepted["resourceId"].(string)
	assertPublicAccess := func(observed *api.RuntimeReadback, open bool, key string) {
		t.Helper()
		code, raw := request(serveFront.URL, customer, "GET", "/api/v2/workspaces/"+wid+"/deployments", "", "")
		var page api.DeploymentPage
		if code != http.StatusOK || publicjson.Unmarshal(raw, &page) != nil || len(page.Items) != 1 {
			t.Fatalf("real BFF current deployment history %d %s", code, raw)
		}
		current := page.Items[0]
		if current.Id != observed.DeploymentId || current.GetRuntimeInstanceId() != observed.RuntimeInstanceId || current.WorkspaceId != wid || (current.Status == api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE) != open {
			t.Fatalf("BFF deployment selection differs from real runtime: %v", current)
		}
		code, raw = request(serveFront.URL, customer, "POST", "/api/v2/workspaces/"+wid+"/access", key, "")
		if !open {
			var refusal api.Error
			if code != http.StatusConflict || publicjson.Unmarshal(raw, &refusal) != nil || refusal.Code != api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE || bytes.Contains(raw, []byte(`"url"`)) {
				t.Fatalf("real BFF exposed failed application %d %s", code, raw)
			}
			return
		}
		var access api.WorkspaceAccess
		mode := api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_ANONYMOUS
		credentials := version.GetDeploymentDescriptor().GetApplicationRevision().GetExposurePolicy() == api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION
		if credentials {
			mode = api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_APPLICATION_LOGIN
		}
		if code != http.StatusOK || publicjson.Unmarshal(raw, &access) != nil || access.WorkspaceId != wid || access.Url != observed.AccessUrl || access.AuthenticationMode != mode || access.GetApplicationCredentialsAvailable() != credentials {
			t.Fatalf("real BFF access differs from declared application access %d %s", code, raw)
		}
	}
	// Stop the owner processes before removing only this accepted Workspace's
	// actual Docker objects. The workflow separately unmounts its exact filesystem.
	t.Cleanup(func() { serveProcess(); fabricProcess(); qCleanupDocker(t, wid) })
	waitState := func(owner *Service, want api.AgentRuntimeObservationState) (ownerstore.Operation, orderResult, *api.RuntimeReadback) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Minute)
		var last ownerstore.Operation
		var result orderResult
		var observed api.RuntimeReadback
		var resumeErr error
		lastError := ""
		for time.Now().Before(deadline) {
			resumeErr = owner.Resume(ctx, opID)
			var e error
			last, e = owner.Store.ReadOperation(ctx, opID)
			if e != nil {
				t.Fatal(e)
			}
			_, result, e = decodeOrder(last)
			if e != nil {
				t.Fatal(e)
			}
			if resumeErr != nil && resumeErr.Error() != lastError {
				t.Logf("resume at %s: %v", last.Stage, resumeErr)
				lastError = resumeErr.Error()
			}
			observed.Reset()
			if resumeErr == nil && len(result.RuntimeReadback) > 0 && protojson.Unmarshal(result.RuntimeReadback, &observed) == nil && observed.State == want {
				return last, result, proto.Clone(&observed).(*api.RuntimeReadback)
			}
			if ctx.Err() != nil {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		var runtimeError, actionError string
		_ = serveDB.QueryRowContext(ctx, `SELECT COALESCE(error_code,'') FROM serve.agent_runtime_instances WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 1`, wid).Scan(&runtimeError)
		_ = serveDB.QueryRowContext(ctx, `SELECT COALESCE(a.error_code,'') FROM serve.agent_runtime_actions a JOIN serve.agent_runtime_instances r ON r.id=a.runtime_instance_id WHERE r.workspace_id=$1 ORDER BY a.updated_at DESC LIMIT 1`, wid).Scan(&actionError)
		t.Fatalf("runtime did not reach %s: operation status=%s stage=%s error=%s state=%s runtime_error=%s action_error=%s resume=%v", want, last.Status, last.Stage, last.ErrorCode, observed.State, runtimeError, actionError, resumeErr)
		return last, result, &observed
	}
	op, original, ready := waitState(workspaceOwner, api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY)
	if op.Status != "awaiting_confirmation" || op.Stage != "activation" || ready.Outcome != api.Observation_OBSERVATION_CONFIRMED || !ready.ProcessReady || !ready.ApplicationAvailable || ready.ReadinessReceiptId == "" {
		t.Fatalf("runtime readiness does not preserve activation boundary: %s %s %v", op.Status, op.Stage, ready)
	}
	assertPublicAccess(ready, true, "first-ready-access")
	containerID, mount := qApplication(t, ctx, wid, ready, storageRoot, version.GetDeploymentDescriptor().GetApplicationRevision())
	if realBuild {
		qReadAgentURL(t, ctx, ready.AccessUrl, containerID, version.GetDeploymentDescriptor().GetApplicationRevision(), false, bundle)
		// This local model fixture is installed into the validated persistent data
		// mount, then loaded by the existing stop/start recovery below. It is not
		// an image layer, Gateway credential, or product configuration claim.
		config, err := os.ReadFile(os.Getenv("OPL_QUALIFICATION_CODEX_CONFIG"))
		if err != nil || len(config) == 0 {
			t.Fatal("real Agent qualification requires OPL_QUALIFICATION_CODEX_CONFIG")
		}
		directory := filepath.Join(mount, "codex")
		if err = os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(directory, "config.toml")
		if err = os.WriteFile(path, config, 0600); err != nil {
			t.Fatal("write isolated model configuration")
		}
		for _, path := range []string{directory, path} {
			if err = os.Chown(path, 1000, 1000); err != nil {
				t.Fatal(err)
			}
		}
	} else {
		qReadURL(t, ctx, ready.AccessUrl, 1)
	}
	if !realBuild {
		qDocker(t, ctx, "exec", containerID, "node", "-e", `if(require('fs').readFileSync('/data/starts','utf8')!=='1')process.exit(1)`)
	}
	image := qualificationNodeImage
	if realBuild {
		image = ready.GetArtifact().GetRepository() + "@" + ready.GetArtifact().GetDigest()
	}
	t.Logf("first delivery: workspace=%s operation=%s resource_set=%s runtime=%s epoch=%d image=%s url=%s mount=%s", wid, opID, original.ResourceSetID, ready.RuntimeInstanceId, ready.ExecutionEpoch, image, ready.AccessUrl, mount)
	// A real process failure must be observed all the way back to the accepted
	// order. Repairing the same fixture container is an explicit host action, not
	// a claim that Serve has an automatic restart product capability.
	qDocker(t, ctx, "stop", "--time", "1", containerID)
	failedOp, _, failed := waitState(workspaceOwner, api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED)
	if failedOp.Status != "needs_attention" || failed.Outcome != api.Observation_OBSERVATION_CONFIRMED || failed.ApplicationAvailable || failed.AccessUrl != "" {
		t.Fatalf("stopped real application was advertised ready: %s %v", failedOp.Status, failed)
	}
	assertPublicAccess(failed, false, "failed-access")
	qUnavailableURL(t, ctx, ready.AccessUrl)
	serveProcess()
	qDocker(t, ctx, "start", containerID)
	serveProcess = qProcess(t, ctx, serveBinary, serveEnv, filepath.Join(bundle, "serve-restarted.log"))
	qHealthy(t, ctx, sa, owneridentity.Serve)
	restarted, err := New(workspaceDB, auth(owneridentity.Workspace), wc, wf, wi)
	if err != nil {
		t.Fatal(err)
	}
	restarted.Ledger, restarted.Capability, restarted.Serve = wl, workspaceOwner.Capability, workspaceOwner.Serve
	recoveredOp, recovered, recoveredReady := waitState(restarted, api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY)
	if recoveredOp.ID != opID || recovered.ResourceSetID != original.ResourceSetID || recovered.FabricOperationID != original.FabricOperationID || !bytes.Equal(recovered.RuntimeReservation, original.RuntimeReservation) || !bytes.Equal(recovered.RuntimeCommand, original.RuntimeCommand) || !bytes.Equal(recovered.ZeroChargeReceipt, original.ZeroChargeReceipt) || recoveredReady.RuntimeInstanceId != ready.RuntimeInstanceId || recoveredReady.DeploymentId != ready.DeploymentId || recoveredReady.ExecutionEpoch != ready.ExecutionEpoch {
		t.Fatal("recovery changed an original order, resource, receipt or runtime identity")
	}
	assertPublicAccess(recoveredReady, true, "recovered-access")
	recoveredID, recoveredMount := qApplication(t, ctx, wid, recoveredReady, storageRoot, version.GetDeploymentDescriptor().GetApplicationRevision())
	if realBuild {
		qReadAgentURL(t, ctx, recoveredReady.AccessUrl, recoveredID, version.GetDeploymentDescriptor().GetApplicationRevision(), true, bundle)
	} else {
		qReadURL(t, ctx, recoveredReady.AccessUrl, 2)
	}
	// Docker may assign a new ephemeral host port when the same container is
	// started again. The recovered entry must be the current owner readback,
	// and BFF/HTTP verification above proves that it is live and publishable.
	if recoveredOp.Status != "awaiting_confirmation" || recoveredOp.Stage != "activation" || recoveredReady.AccessUrl == "" {
		t.Fatal("recovery changed the published entry or fabricated Workspace activation")
	}
	if recoveredID != containerID || recoveredMount != mount {
		t.Fatal("recovery replaced the original application container or persistent mount")
	}
	code, raw = request(workspaceFront.URL, customer, "POST", "/api/v2/workspaces", "qualification-create", body)
	if code != 202 || !bytes.Contains(raw, []byte(opID)) {
		t.Fatalf("idempotent create %d %s", code, raw)
	}
	for _, check := range []struct {
		db    *sql.DB
		query string
		want  int
	}{
		{workspaceDB, `SELECT count(*) FROM workspace.workspaces`, 1}, {workspaceDB, `SELECT count(*) FROM workspace.subscriptions`, 0},
		{fabricDB, `SELECT count(*) FROM fabric.resource_sets`, 1}, {serveDB, `SELECT count(*) FROM serve.agent_deployments`, 1}, {serveDB, `SELECT count(*) FROM serve.agent_runtime_instances`, 1},
		{capabilityDB, `SELECT count(*) FROM capability.reference_claims WHERE claimant_owner='serve' AND purpose='deploy'`, 1},
		{ledgerDB, `SELECT count(*) FROM evidence_receipts WHERE receipt_type='workspace.local_no_charge.v1'`, 1},
	} {
		var got int
		if err = check.db.QueryRowContext(ctx, check.query).Scan(&got); err != nil || got != check.want {
			t.Fatalf("idempotence count got=%d want=%d query=%s err=%v", got, check.want, check.query, err)
		}
	}
	if realBuild {
		t.Log("qualified: uploaded Package -> real BuildKit OCI and CapabilityVersion handoff -> real quota-backed Local resources -> real Serve/Fabric Agent application -> live HTTP response; actual container failure observed; same container/data/operation/deployment/epoch recovered after Serve and Workspace restart; one zero-charge receipt and one Serve claim; Gateway and paid activation are not qualified")
	} else {
		t.Log("qualified: real quota-backed Local resources -> real Serve/Fabric application -> live HTTP; actual container failure observed; same container/data/operation/deployment/epoch recovered after Serve and Workspace restart; one zero-charge receipt and one Serve claim; Build-ready provenance is a fixture; Gateway and paid activation are not qualified")
	}
}

func qOwnerProcessDB(t *testing.T, ctx context.Context, dsn, owner string, source ownerstore.MigrationSource) (*ownerstoretest.Harness, *sql.DB) {
	t.Helper()
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: owner, Database: "opl_" + owner, SchemaOwnerRole: "opl_" + owner + "_owner", WriterRole: "opl_" + owner + "_writer", RuntimeRole: "opl_" + owner + "_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatal(err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return h, db
}
func qAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
func qPeers(peers ...string) string {
	m := map[string]string{}
	for _, peer := range peers {
		m[peer] = livePeerToken
	}
	raw, _ := json.Marshal(m)
	return string(raw)
}
func qEnv(groups ...map[string]string) []string {
	m := map[string]string{}
	for _, key := range []string{"PATH", "HOME", "TMPDIR"} {
		m[key] = os.Getenv(key)
	}
	for _, group := range groups {
		for k, v := range group {
			m[k] = v
		}
	}
	out := []string{}
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}
func qProcess(t *testing.T, ctx context.Context, binary string, env []string, logPath string) func() {
	t.Helper()
	log, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, binary)
	cmd.Env = env
	cmd.Stdout, cmd.Stderr = log, log
	if err = cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				_ = cmd.Process.Kill()
				<-done
			}
			log.Close()
			if t.Failed() {
				raw, _ := os.ReadFile(logPath)
				t.Logf("%s: %s", filepath.Base(logPath), raw)
			}
		})
	}
	t.Cleanup(stop)
	return stop
}
func qHealthy(t *testing.T, ctx context.Context, address string, owner owneridentity.Owner) {
	t.Helper()
	client := grpc_health_v1.NewHealthClient(liveDial(t, address, owneridentity.Workspace, owner, livePeerToken))
	deadline := time.Now().Add(20 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		probe, cancel := context.WithTimeout(ctx, time.Second)
		reply, err := client.Check(probe, &grpc_health_v1.HealthCheckRequest{})
		cancel()
		last = err
		if err == nil && reply.Status == grpc_health_v1.HealthCheckResponse_SERVING {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s executable never became healthy: %v", owner, last)
}
func qDocker(t *testing.T, ctx context.Context, args ...string) []byte {
	t.Helper()
	call, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	out, err := exec.CommandContext(call, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("docker %s: %v %s", strings.Join(args, " "), err, out)
	}
	return out
}
func qReadURL(t *testing.T, ctx context.Context, address string, starts int) {
	t.Helper()
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "http" || parsed.Hostname() != "127.0.0.1" {
		t.Fatalf("provider must publish the exact loopback application URL: %q", address)
	}
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	want := fmt.Sprintf("opl-first-deploy-qualification starts=%d\n", starts)
	if err != nil || res.StatusCode != 200 || string(raw) != want {
		t.Fatalf("actual application HTTP status=%d body=%q err=%v", res.StatusCode, raw, err)
	}
}
func qReadAgentURL(t *testing.T, ctx context.Context, address, containerID string, revision *api.WorkspaceApplicationRevision, send bool, bundle string) {
	t.Helper()
	entry, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	origin := entry.Scheme + "://" + entry.Host
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Timeout: 4 * time.Minute, Jar: jar, Transport: &http.Transport{Proxy: nil}}
	request := func(path string, body any, csrf string, result any) {
		t.Helper()
		var data []byte
		method := http.MethodGet
		if body != nil {
			method = http.MethodPost
			data, err = json.Marshal(body)
			if err != nil {
				t.Fatal(err)
			}
		}
		req, e := http.NewRequestWithContext(ctx, method, origin+path, bytes.NewReader(data))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", origin)
		if csrf != "" {
			req.Header.Set("X-CSRF-Token", csrf)
		}
		res, e := client.Do(req)
		if e != nil {
			t.Fatalf("application request %s: %v", path, e)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			t.Fatalf("application request %s returned HTTP %d", path, res.StatusCode)
		}
		if e = json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(result); e != nil {
			t.Fatalf("invalid application response %s: %v", path, e)
		}
	}
	var ready struct{ Status string }
	request("/readyz", nil, "", &ready)
	if ready.Status != "ready" {
		t.Fatal("application /readyz did not confirm readiness")
	}
	var credential *api.WorkspaceApplicationCredential
	for _, declared := range revision.GetCredentials() {
		if declared.GetKind() == api.WorkspaceApplicationCredentialKindEnum_WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_WORKSPACE_ADMIN_PASSWORD {
			credential = declared
		}
	}
	if credential == nil || credential.GetUsername() == "" || credential.GetTarget() == "" {
		t.Fatal("Agent login probe requires its declared password credential")
	}
	// Read the credential actually mounted into this exact runtime. Keep both
	// password and session token in memory, outside logs and persisted evidence.
	password, err := exec.CommandContext(ctx, "docker", "exec", "--user", "0", containerID, "cat", credential.GetTarget()).Output()
	if err != nil || len(bytes.TrimSpace(password)) == 0 {
		t.Fatal("read runtime's declared application password")
	}
	var login struct {
		CSRFToken string `json:"csrfToken"`
	}
	request("/api/auth/login", map[string]string{"username": credential.GetUsername(), "password": strings.TrimSpace(string(password))}, "", &login)
	if login.CSRFToken == "" {
		t.Fatal("application login did not establish a session")
	}
	pageRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		t.Fatal(err)
	}
	page, err := client.Do(pageRequest)
	if err != nil {
		t.Fatal(err)
	}
	pageBody, pageErr := io.ReadAll(io.LimitReader(page.Body, 16<<10))
	page.Body.Close()
	if pageErr != nil || page.StatusCode != http.StatusOK || len(bytes.TrimSpace(pageBody)) == 0 {
		t.Fatal("authenticated application route is unavailable")
	}
	var state struct {
		AppState struct {
			AppState struct {
				AgentPackages struct {
					Directory struct {
						Entries []struct {
							PackageID string `json:"package_id"`
							Readiness struct {
								OperationalReady bool `json:"operational_ready"`
								LaunchAllowed    bool `json:"launch_allowed"`
							} `json:"readiness"`
							HomeShortcuts []struct {
								ShortcutID string `json:"shortcut_id"`
								Route      struct {
									CodexVisibleEntry string `json:"codex_visible_entry"`
								} `json:"route"`
							} `json:"home_shortcuts"`
							CapabilityMetadata struct {
								RequiredSkillIDs []string `json:"required_skill_ids"`
							} `json:"capability_metadata"`
						} `json:"entries"`
					} `json:"directory"`
				} `json:"agent_packages"`
				Core struct {
					Codex struct {
						ModelAccessReady bool `json:"model_access_ready"`
					} `json:"codex"`
				} `json:"core"`
			} `json:"app_state"`
		} `json:"app_state"`
	}
	request("/api/opl/state?profile=fast", nil, "", &state)
	var selection map[string]any
	for _, agent := range state.AppState.AppState.AgentPackages.Directory.Entries {
		if agent.PackageID == revision.GetApplicationId() && agent.Readiness.OperationalReady && agent.Readiness.LaunchAllowed && len(agent.HomeShortcuts) > 0 {
			shortcut := agent.HomeShortcuts[0]
			if shortcut.ShortcutID != "" && shortcut.Route.CodexVisibleEntry != "" {
				selection = map[string]any{"package_id": agent.PackageID, "shortcut_id": shortcut.ShortcutID, "codex_visible_entry": shortcut.Route.CodexVisibleEntry, "required_skill_ids": agent.CapabilityMetadata.RequiredSkillIDs}
			}
		}
	}
	if selection == nil {
		t.Fatal("declared Agent is not callable in the authenticated application")
	}
	t.Log("application /readyz, password login and declared Agent route verified")
	if !send {
		return
	}
	if !state.AppState.AppState.Core.Codex.ModelAccessReady {
		t.Fatal("application model access is not ready after loading the isolated configuration")
	}
	var answer struct {
		Executor        string `json:"executor"`
		ThreadID        string `json:"threadId"`
		TurnID          string `json:"turnId"`
		FinalMessage    string `json:"finalMessage"`
		CanonicalThread struct {
			ID    string `json:"id"`
			Turns []struct {
				ID     string          `json:"id"`
				Status string          `json:"status"`
				Error  json.RawMessage `json:"error"`
			} `json:"turns"`
		} `json:"canonicalThread"`
	}
	request("/api/send-message", map[string]any{"prompt": "这是 Agent 运行验收，不是医学问诊。请简要说明你的名称、功能和证据使用边界；如果当前没有指南检索证据，请明确说明，勿生成医学建议或假称已检索。", "agentSelection": selection}, login.CSRFToken, &answer)
	completed := false
	for _, turn := range answer.CanonicalThread.Turns {
		if turn.ID == answer.TurnID && turn.Status == "completed" && (len(turn.Error) == 0 || string(turn.Error) == "null") {
			completed = true
		}
	}
	if answer.Executor != "codex_app_server" || answer.ThreadID == "" || answer.TurnID == "" || answer.CanonicalThread.ID != answer.ThreadID || !completed || strings.TrimSpace(answer.FinalMessage) == "" {
		t.Fatal("application did not confirm a canonical completed Agent response")
	}
	responseHash := sha256.Sum256([]byte(answer.FinalMessage))
	evidence, err := json.MarshalIndent(map[string]any{"scope": "local_workspace_serve_fabric_agent_response", "image": revision.GetImage(), "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "threadId": answer.ThreadID, "turnId": answer.TurnID, "turnStatus": "completed", "executor": answer.Executor, "agentSelection": selection, "responseBytes": len(answer.FinalMessage), "responseSha256": hex.EncodeToString(responseHash[:])}, "", "  ")
	path := filepath.Join(bundle, "agent-response-"+time.Now().UTC().Format("20060102T150405.000000000Z")+".json")
	if err != nil || os.WriteFile(path, evidence, 0600) != nil {
		t.Fatal("persist sanitized Agent response readback")
	}
	t.Logf("real Agent response: thread=%s turn=%s status=completed response_bytes=%d", answer.ThreadID, answer.TurnID, len(answer.FinalMessage))
}

func qUnavailableURL(t *testing.T, ctx context.Context, address string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := (&http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(req)
	if err == nil {
		defer res.Body.Close()
		if res.StatusCode == 200 {
			t.Fatal("stopped application still answered HTTP 200")
		}
	}
}
func qApplication(t *testing.T, ctx context.Context, wid string, r *api.RuntimeReadback, root string, revision *api.WorkspaceApplicationRevision) (string, string) {
	t.Helper()
	ids := strings.Fields(string(qDocker(t, ctx, "container", "ls", "-aq", "--filter", "label=opl.workspace.id="+wid, "--filter", "label=opl.fabric.kind=application_runtime")))
	if len(ids) != 1 {
		t.Fatalf("expected one real application container, got %d", len(ids))
	}
	var entries []struct {
		ID     string
		Config struct {
			Image  string
			Labels map[string]string
		}
		State  struct{ Running bool }
		Mounts []struct {
			Type, Source, Destination string
			RW                        bool
		}
	}
	if json.Unmarshal(qDocker(t, ctx, "inspect", ids[0]), &entries) != nil || len(entries) != 1 {
		t.Fatal("invalid container inspection")
	}
	c := entries[0]
	expectedImage := r.GetArtifact().GetRepository() + "@" + r.GetArtifact().GetDigest()
	if !c.State.Running || c.Config.Image != expectedImage || c.Config.Labels["opl.workspace.id"] != wid || c.Config.Labels["opl.runtime.id"] != contracts.WorkspaceApplicationRuntimeID(r.RuntimeInstanceId) || c.Config.Labels["opl.image.ref"] != expectedImage || c.Config.Labels["opl.account.id"] != "qualification-account" {
		t.Fatal("actual container identity/image differs from the accepted runtime")
	}
	mounts := revision.GetPersistentMounts()
	if len(mounts) != 1 || mounts[0].GetReadOnly() || mounts[0].GetMountPath() != "/data" {
		t.Fatal("native qualification requires exactly the declared writable /data mount")
	}
	persistentPath := mounts[0].GetMountPath()
	for _, mount := range c.Mounts {
		if mount.Destination == persistentPath {
			rel, err := filepath.Rel(root, mount.Source)
			if err != nil || rel == "." || strings.HasPrefix(rel, "..") || mount.Type != "bind" || !mount.RW {
				t.Fatal("application data is not bound inside the real quota filesystem")
			}
			return c.ID, mount.Source
		}
	}
	t.Fatal("real persistent data mount is absent")
	return "", ""
}
func qCleanupDocker(t *testing.T, wid string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for _, kind := range []string{"container", "network"} {
		args := []string{kind, "ls", "-q", "--filter", "label=opl.workspace.id=" + wid}
		if kind == "container" {
			args = []string{kind, "ls", "-aq", "--filter", "label=opl.workspace.id=" + wid}
		}
		raw, err := exec.CommandContext(ctx, "docker", args...).Output()
		if err != nil {
			t.Errorf("list own %s cleanup: %v", kind, err)
			continue
		}
		for _, id := range strings.Fields(string(raw)) {
			remove := []string{kind, "rm"}
			if kind == "container" {
				remove = append(remove, "-f")
			}
			remove = append(remove, id)
			if out, err := exec.CommandContext(ctx, "docker", remove...).CombinedOutput(); err != nil {
				t.Errorf("remove own %s: %v %s", kind, err, out)
			}
		}
	}
}

// The retained operation store requires an RFC1918 address. Resolve only the
// Docker service publishing the explicitly supplied loopback PostgreSQL port,
// then prove its system identifier equals the loopback server before use. No
// TLS exception or remote database discovery is added to product code.
func qLegacyDatabase(t *testing.T, ctx context.Context, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil || u.Scheme != "postgres" || u.Hostname() != "127.0.0.1" || u.Port() == "" {
		t.Fatal("qualification admin must be an explicit loopback postgres URL")
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { admin.Close() })
	var original string
	if err = admin.QueryRowContext(ctx, `SELECT system_identifier::text FROM pg_control_system()`).Scan(&original); err != nil {
		t.Fatal(err)
	}
	var bridge string
	ids := strings.Fields(string(qDocker(t, ctx, "ps", "-q", "--filter", "publish="+u.Port())))
	for _, id := range ids {
		var records []struct {
			NetworkSettings struct {
				Ports    map[string][]struct{ HostIp, HostPort string }
				Networks map[string]struct{ IPAddress string }
			}
		}
		if json.Unmarshal(qDocker(t, ctx, "inspect", id), &records) != nil || len(records) != 1 {
			continue
		}
		matched := false
		for _, binding := range records[0].NetworkSettings.Ports["5432/tcp"] {
			matched = matched || (binding.HostPort == u.Port() && (binding.HostIp == "0.0.0.0" || binding.HostIp == "127.0.0.1"))
		}
		if !matched {
			continue
		}
		for _, network := range records[0].NetworkSettings.Networks {
			ip := net.ParseIP(network.IPAddress)
			if ip == nil || ip.To4() == nil || !ip.IsPrivate() {
				continue
			}
			candidate := *u
			candidate.Host = net.JoinHostPort(network.IPAddress, "5432")
			db, e := sql.Open("postgres", candidate.String())
			if e != nil {
				continue
			}
			probe, cancel := context.WithTimeout(ctx, 2*time.Second)
			var found string
			e = db.QueryRowContext(probe, `SELECT system_identifier::text FROM pg_control_system()`).Scan(&found)
			cancel()
			db.Close()
			if e == nil && found == original {
				if bridge != "" && bridge != candidate.String() {
					t.Fatal("ambiguous isolated PostgreSQL service")
				}
				bridge = candidate.String()
			}
		}
	}
	if bridge == "" {
		t.Fatal("no Docker service bridge matched the isolated loopback PostgreSQL system identifier")
	}
	database := fmt.Sprintf("opl_qualification_fabric_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, `CREATE DATABASE `+database); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(cleanup, `DROP DATABASE `+database+` WITH (FORCE)`); err != nil {
			t.Errorf("drop isolated legacy database: %v", err)
		}
	})
	legacy, _ := url.Parse(bridge)
	legacy.Path = "/" + database
	return legacy.String()
}

func qSeedBuildFixture(t *testing.T, ctx context.Context, db *sql.DB, platform, handoffPath string) (*api.CapabilityVersion, bool) {
	if handoffPath != "" {
		return qSeedRealBuildHandoff(t, ctx, db, platform, handoffPath), true
	}
	return qSeedFixtureBuild(t, ctx, db, platform), false
}

type qBuildHandoff struct {
	SchemaVersion        int             `json:"schemaVersion"`
	RegistryHost         string          `json:"registryHost"`
	RegistryContainer    string          `json:"registryContainer"`
	CapabilityVersion    json.RawMessage `json:"capabilityVersion"`
	BuildArtifact        json.RawMessage `json:"buildArtifact"`
	PackageVersionStatus string          `json:"packageVersionStatus"`
}

func qSeedRealBuildHandoff(t *testing.T, ctx context.Context, db *sql.DB, platform, path string) *api.CapabilityVersion {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var handoff qBuildHandoff
	if err = json.Unmarshal(raw, &handoff); err != nil || handoff.SchemaVersion != 2 || handoff.RegistryHost == "" || handoff.RegistryContainer == "" {
		t.Fatalf("invalid Build handoff: %v", err)
	}
	version := &api.CapabilityVersion{}
	artifact := &api.BuildArtifactReadback{}
	if err = publicjson.Unmarshal(handoff.CapabilityVersion, version); err != nil {
		t.Fatalf("decode CapabilityVersion handoff: %v", err)
	}
	if err = protojson.Unmarshal(handoff.BuildArtifact, artifact); err != nil {
		t.Fatalf("decode Build artifact handoff: %v", err)
	}
	if artifact.GetRegistrationContext() != nil {
		t.Fatal("Build handoff must not carry registration authorization")
	}
	if handoff.PackageVersionStatus != "uploaded" {
		t.Fatalf("Build handoff PackageVersion status=%q want=uploaded", handoff.PackageVersionStatus)
	}
	if version.GetStatus() != api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY ||
		artifact.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED ||
		version.GetId() == "" || version.GetBuildJobId() == "" || artifact.GetBuildJobId() != version.GetBuildJobId() ||
		artifact.GetInput().GetPackageVersionId() == "" || artifact.GetInput().GetPackageObject().GetSha256() == "" ||
		artifact.GetArtifact().GetRepository() == "" || artifact.GetArtifact().GetDigest() == "" ||
		version.GetArtifact().GetRepository() != artifact.GetArtifact().GetRepository() || version.GetArtifact().GetDigest() != artifact.GetArtifact().GetDigest() ||
		version.GetDeploymentDescriptorDigest() != artifact.GetDeploymentDescriptorDigest() {
		t.Fatal("Build handoff does not carry a confirmed immutable CapabilityVersion")
	}
	descriptor := artifact.GetDeploymentDescriptor()
	if descriptor == nil || descriptor.GetApplicationRevision().GetPlatform() != platform || descriptor.GetApplicationRevision().GetImage() != descriptor.GetArtifact().GetRepository()+"@"+descriptor.GetArtifact().GetDigest() {
		t.Fatalf("Build handoff platform or application image mismatch: platform=%q image=%q", descriptor.GetApplicationRevision().GetPlatform(), descriptor.GetApplicationRevision().GetImage())
	}
	descriptorBytes, err := publicjson.Marshal(descriptor)
	if err != nil || digestBytes(descriptorBytes) != artifact.GetDeploymentDescriptorDigest() {
		t.Fatalf("Build handoff descriptor digest mismatch: %v", err)
	}
	// The supplying harness owns the registry lifecycle. Consuming an external
	// handoff never grants permission to remove that registry or its volume.
	if running := strings.TrimSpace(string(qDocker(t, ctx, "inspect", "-f", "{{.State.Running}}", handoff.RegistryContainer))); running != "true" {
		t.Fatalf("Build handoff registry is not running: %s", running)
	}
	image := artifact.GetArtifact().GetRepository() + "@" + artifact.GetArtifact().GetDigest()
	if strings.SplitN(artifact.GetArtifact().GetRepository(), "/", 2)[0] != handoff.RegistryHost {
		t.Fatalf("Build handoff artifact registry=%q does not match live registry=%q", artifact.GetArtifact().GetRepository(), handoff.RegistryHost)
	}
	qDocker(t, ctx, "pull", image)
	if got := strings.TrimSpace(string(qDocker(t, ctx, "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", image))); got != platform {
		t.Fatalf("Build handoff image platform=%q want=%q", got, platform)
	}

	input := artifact.GetInput()
	packageID, packageVersionID := input.GetPackageId(), input.GetPackageVersionId()
	if packageID == "" || packageVersionID == "" || input.GetRuntimeVersionId() == "" || input.GetWebuiVersionId() == "" {
		t.Fatal("Build handoff input is missing the three approved Build references")
	}
	webui := input.GetWebuiContract()
	webuiRef := descriptor.GetWebuiContractReference()
	if webui == nil || webuiRef == nil || webuiRef.GetPublisherNamespaceId() == "" {
		t.Fatal("Build handoff is missing the approved WebUI contract reference")
	}
	webuiRaw, err := publicjson.Marshal(webui)
	if err != nil {
		t.Fatal(err)
	}
	artifactRaw, err := protojson.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	modelRequirements, err := json.Marshal(version.GetModelRequirements())
	if err != nil {
		t.Fatal(err)
	}
	dataCompatibility, err := publicjson.Marshal(artifact.GetDataCompatibility())
	if err != nil {
		t.Fatal(err)
	}
	publisherNames := map[string]struct {
		ref   *api.PublisherContractReference
		image *api.ArtifactReference
	}{}
	publisherNames[webuiRef.GetPublisherNamespaceId()] = struct {
		ref   *api.PublisherContractReference
		image *api.ArtifactReference
	}{webuiRef, webui.GetImage()}
	if runtimeRef := descriptor.GetRuntimeContractReference(); runtimeRef != nil && runtimeRef.GetPublisherNamespaceId() != "" {
		publisherNames[runtimeRef.GetPublisherNamespaceId()] = struct {
			ref   *api.PublisherContractReference
			image *api.ArtifactReference
		}{runtimeRef, input.GetRuntimeContract().GetImage()}
	}
	execute := func(query string, args ...any) {
		t.Helper()
		if _, e := db.ExecContext(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	execute(`INSERT INTO capability.namespaces(id,tenant_id,name,kind) VALUES('qualification-ns','tenant-live','build-handoff','tenant_default')`)
	execute(`INSERT INTO capability.packages(id,namespace_id,name,visibility,created_by) VALUES($1,'qualification-ns','built-agent','private','101')`, packageID)
	object := input.GetPackageObject()
	manifest := []byte(`{"name":"live-package"}`)
	execute(`INSERT INTO capability.package_versions(id,package_id,version_label,status,sha256,size_bytes,object_ref,manifest,created_by,verified_at) VALUES($1,$2,'v1','uploaded',$3,$4,$5,$6,'101',now())`, packageVersionID, packageID, object.GetSha256(), object.GetSizeBytes(), object.GetStorageObjectId()+"/"+object.GetVersionId(), manifest)
	for id, item := range publisherNames {
		if item.image == nil || item.image.GetRepository() == "" || item.ref.GetDescriptorDigest() == "" {
			t.Fatal("Build handoff publisher namespace is incomplete")
		}
		repo := item.image.GetRepository()
		prefix := repo[:strings.LastIndex(repo, "/")]
		registry := strings.SplitN(repo, "/", 2)[0]
		kind := "official"
		if strings.Contains(strings.ToLower(item.ref.GetKind().String()), "third") {
			kind = "third_party"
		}
		execute(`INSERT INTO capability.publisher_namespaces(id,name,kind,registry_id,repository_prefix,admission_receipt_id) VALUES($1,$2,$3,$4,$5,$6)`, id, "handoff-"+id, kind, registry, prefix, "build-handoff-"+id)
	}
	webuiImage := webui.GetImage()
	execute(`INSERT INTO capability.webui_versions(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_versions,ui_protocol_version,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref) VALUES($1,'handoff-webui','v1',$2,$3,'103',$4,($5::jsonb->>'uiProtocolVersion'),'build-handoff-webui',$6,$7,$8,$9)`, input.GetWebuiVersionId(), webuiImage.GetRepository(), webuiImage.GetDigest(), pq.Array(webui.GetRuntimeAbiVersions()), webuiRaw, webuiRef.GetPublisherNamespaceId(), webuiRef.GetDescriptorDigest(), webuiRaw, webuiRef.GetDescriptorObjectRef())
	execute(`INSERT INTO capability.capability_versions(id,package_id,package_version_id,build_job_id,version_label,runtime_version_id,webui_version_id,artifact_repository,artifact_digest,status,model_requirements,data_compatibility,provenance_evidence,provenance,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'ready',$10,$11,$12,'build',$13,$14,$15)`, version.GetId(), packageID, packageVersionID, version.GetBuildJobId(), version.GetVersionLabel(), input.GetRuntimeVersionId(), input.GetWebuiVersionId(), artifact.GetArtifact().GetRepository(), artifact.GetArtifact().GetDigest(), modelRequirements, dataCompatibility, artifactRaw, descriptorBytes, artifact.GetDeploymentDescriptorDigest(), artifact.GetDeploymentDescriptorObjectRef())
	return version
}

func digestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func qSeedFixtureBuild(t *testing.T, ctx context.Context, db *sql.DB, platform string) *api.CapabilityVersion {
	t.Helper()
	parts := strings.Split(qualificationNodeImage, "@")
	repository, digest := parts[0], parts[1]
	// The repository of the immutable artifact excludes the tag used by the
	// pre-pull. Its application revision uses the exact same repo@digest form.
	repository = strings.TrimSuffix(repository, ":22-bookworm-slim")
	image := repository + "@" + digest
	source := `const fs=require('node:fs');const p='/data/starts';let n=0;try{n=Number(fs.readFileSync(p,'utf8'))||0}catch{};fs.writeFileSync(p,String(++n));require('node:http').createServer((q,s)=>{s.writeHead(200,{'content-type':'text/plain'});s.end(q.url==='/healthz'?'ok':'opl-first-deploy-qualification starts='+n+'\n')}).listen(8080,'0.0.0.0')`
	revision := &api.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationId: "qualification-counter", Version: "1", Platform: platform, Image: image, Entrypoint: []string{"node", "-e", source}, Ports: []*api.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: api.WorkspaceApplicationPortProtocolEnum_WORKSPACE_APPLICATION_PORT_PROTOCOL_ENUM_TCP}}, EntryPort: proto.String("http"), HealthChecks: []*api.WorkspaceApplicationHealthCheck{{Port: 8080, Path: "/healthz"}}, PersistentMounts: []*api.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}}, ExposurePolicy: api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_ANONYMOUS}
	descriptor := &api.DeploymentDescriptor{SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Artifact: &api.ArtifactReference{Repository: repository, Digest: digest}, Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD, ApplicationRevision: revision, PackageVersionId: proto.String("qualification-package-version"), RuntimeContractReference: &api.PublisherContractReference{VersionId: "qualification-runtime", Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_RUNTIME, PublisherNamespaceId: "qualification-publisher", DescriptorDigest: digest, DescriptorObjectRef: "fixture-runtime-contract"}, WebuiContractReference: &api.PublisherContractReference{VersionId: "qualification-ui", Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_WEBUI, PublisherNamespaceId: "qualification-publisher", DescriptorDigest: digest, DescriptorObjectRef: "fixture-ui-contract"}}
	raw, err := publicjson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	descriptorDigest := "sha256:" + hex.EncodeToString(sum[:])
	// Validate the actual Fabric contract before writing the declared Build fixture.
	revRaw, err := publicjson.Marshal(revision)
	if err != nil {
		t.Fatal(err)
	}
	var realRevision contracts.WorkspaceApplicationRevision
	if err = json.Unmarshal(revRaw, &realRevision); err != nil {
		t.Fatal(err)
	}
	if err = contracts.ValidateWorkspaceApplicationRevision(realRevision); err != nil {
		t.Fatal(err)
	}
	readback := &api.BuildArtifactReadback{BuildJobId: "qualification-build-fixture", Input: &api.BuildInputSnapshot{PackageId: "qualification-package", PackageVersionId: "qualification-package-version", RuntimeVersionId: "qualification-runtime", WebuiVersionId: "qualification-ui"}, Artifact: descriptor.Artifact, ArtifactReceiptId: "qualification-build-fixture-receipt", VersionLabel: "1", DataCompatibility: &api.DataCompatibility{DataSchemaVersion: "1"}, Outcome: api.Observation_OBSERVATION_CONFIRMED, DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: descriptorDigest, DeploymentDescriptorObjectRef: "qualification-build-fixture-descriptor"}
	evidence, err := protojson.Marshal(readback)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(query string, args ...any) {
		t.Helper()
		if _, e := db.ExecContext(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	execute(`INSERT INTO capability.namespaces(id,tenant_id,name,kind) VALUES('qualification-ns','tenant-live','qualification','tenant_default')`)
	execute(`INSERT INTO capability.packages(id,namespace_id,name,visibility,created_by) VALUES('qualification-package','qualification-ns','counter','private','101')`)
	execute(`INSERT INTO capability.package_versions(id,package_id,version_label,sha256,size_bytes,created_by) VALUES('qualification-package-version','qualification-package','1',$1,1,'101')`, digest)
	execute(`INSERT INTO capability.publisher_namespaces(id,name,kind,registry_id,repository_prefix,admission_receipt_id) VALUES('qualification-publisher','qualification','official','docker.io','docker.io/library','qualification-publisher-fixture')`)
	publisher, _ := json.Marshal(map[string]any{"schemaVersion": "opl-publisher-contract/v1", "kind": "webui", "publisherNamespaceId": "qualification-publisher", "image": map[string]any{"repository": repository, "digest": digest, "platform": map[string]string{"os": "linux", "architecture": "amd64"}}, "runtimeAbiVersions": []string{"qualification-abi"}, "uiProtocolVersion": "qualification-ui"})
	execute(`INSERT INTO capability.webui_versions(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_versions,ui_protocol_version,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref) VALUES('qualification-ui','qualification','1',$1,$2,'103',ARRAY['qualification-abi'],'qualification-ui','qualification-ui-fixture','qualification-publisher',$2,$3,'qualification-ui-fixture-ref')`, repository, digest, publisher)
	execute(`INSERT INTO capability.capability_versions(id,package_id,package_version_id,build_job_id,version_label,runtime_version_id,webui_version_id,artifact_repository,artifact_digest,status,model_requirements,data_compatibility,provenance_evidence,provenance,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref) VALUES('qualification-capability','qualification-package','qualification-package-version','qualification-build-fixture','1','qualification-runtime','qualification-ui',$1,$2,'ready','[]','{"dataSchemaVersion":"1"}',$3,'build',$4,$5,'qualification-build-fixture-descriptor')`, repository, digest, evidence, raw, descriptorDigest)
	return &api.CapabilityVersion{Id: "qualification-capability", Artifact: descriptor.Artifact, DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: descriptorDigest, DeploymentDescriptorObjectRef: "qualification-build-fixture-descriptor"}
}
