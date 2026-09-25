//go:build livebuild

package launch

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"opl-cloud/apps/console-bff"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	fabric "opl-cloud/services/fabric/coordination"
	fabricmigrations "opl-cloud/services/fabric/ownermigrations"
	cloudidentity "opl-cloud/services/gateway-integration/identity"
	identitymigrations "opl-cloud/services/gateway-integration/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	ledger "opl-cloud/services/ledger/eventconsumer"
	catalog "opl-cloud/services/resource-catalog/catalog"
	catalogmigrations "opl-cloud/services/resource-catalog/migrations"
	"opl-cloud/services/workspace/migrations"
	"os"
	"strings"
	"testing"
	"time"
)

const livePeerToken = "isolated-resource-catalog-peer-token-0001"

// liveOwnerDB provisions one owner's real isolated database and installs its real
// migrations.
func liveOwnerDB(t *testing.T, ctx context.Context, dsn, name string, source ownerstore.MigrationSource) *sql.DB {
	t.Helper()
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: name, Database: "opl_" + name, SchemaOwnerRole: "opl_" + name + "_owner", WriterRole: "opl_" + name + "_writer", RuntimeRole: "opl_" + name + "_runtime"})
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
	return db
}

// Ledger retains its append-only schema and installer. It uses its own isolated
// database and schema-owner login; no test owner reads another owner's database.
func liveLedgerDB(t *testing.T, ctx context.Context, dsn string) (*sql.DB, *ledger.Server) {
	t.Helper()
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "ledger", Database: "opl_ledger", SchemaOwnerRole: "opl_ledger_owner", WriterRole: "opl_ledger_writer", RuntimeRole: "opl_ledger_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	db, err := h.Open(ctx, h.OwnerDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	owner, err := ledger.New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = owner.Install(ctx); err != nil {
		t.Fatal(err)
	}
	return db, owner
}

// liveOwnerServer serves one real owner process over a real gRPC listener behind
// the shared owner identity interceptor.
func liveOwnerServer(t *testing.T, owner owneridentity.Owner, peers []owneridentity.Service, register func(*ownerservice.Server) error) string {
	t.Helper()
	config := ownerservice.Config{Owner: owner, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{}}
	for _, peer := range peers {
		config.Peers[peer] = livePeerToken
	}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := register(server); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}

// liveDial opens a typed gRPC connection that presents the caller's real service
// identity and token on every call.
// liveDialService opens a connection that presents an explicit service identity,
// which is what a caller outside the ten data owners (the Console BFF) uses.
func liveDialService(t *testing.T, address string, caller, target owneridentity.Service, token string) *grpc.ClientConn {
	t.Helper()
	options, err := owneridentity.TLSConfig{AllowInsecureLocal: true}.DialOptions(caller, target, token)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func liveDial(t *testing.T, address string, caller, target owneridentity.Owner, token string) *grpc.ClientConn {
	t.Helper()
	options, err := owneridentity.TLSConfig{AllowInsecureLocal: true}.DialOptions(caller.Service(), target.Service(), token)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func liveGatewayFixture(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor := int64(101)
		email := "admin@example.test"
		if r.URL.Path == "/api/v1/auth/login" {
			var body struct {
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			if json.NewDecoder(r.Body).Decode(&body) != nil || body.Password != "isolated-password" {
				w.WriteHeader(401)
				return
			}
			email = body.Email
			switch email {
			case "platform-admin@example.test":
				actor = 103
			case "tenant-admin@example.test":
				actor = 101
			case "member@example.test":
				actor = 104
			default:
				w.WriteHeader(401)
				return
			}
		} else if r.URL.Path == "/api/v1/auth/me" {
			switch strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") {
			case "isolated-gateway-platform-admin@example.test":
				actor, email = 103, "platform-admin@example.test"
			case "isolated-gateway-tenant-admin@example.test":
				actor, email = 101, "tenant-admin@example.test"
			case "isolated-gateway-member@example.test":
				actor, email = 104, "member@example.test"
			default:
				w.WriteHeader(401)
				return
			}
		} else {
			w.WriteHeader(404)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		user := cloudidentity.GatewayIdentity{ID: actor, Email: email, Status: "active"}
		if r.URL.Path == "/api/v1/auth/login" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"access_token": "isolated-gateway-" + email, "user": user}})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": user})
		}
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// This test uses real Cloud owners, real gRPC, real owner PostgreSQL databases
// and the real BFF boundary. Only the external identity backend is isolated.
func TestLiveAcceptedWorkspaceResourceReference(t *testing.T) {
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	identitySource, _ := identitymigrations.Source()
	tenantDB := liveOwnerDB(t, ctx, dsn, "tenant", identitySource)
	_, err := tenantDB.ExecContext(ctx, `INSERT INTO tenant.tenants(id,name) VALUES('tenant-live','Test Tenant'),('tenant-other','Other Tenant'); INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES('member-admin','tenant-live','101','admin'),('member-member','tenant-live','104','member')`)
	if err != nil {
		t.Fatal(err)
	}
	gateway, err := cloudidentity.NewGateway(liveGatewayFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	identity, err := cloudidentity.New(tenantDB, gateway, bytes.Repeat([]byte("k"), 32), []string{"103"}, 168*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	ia := liveOwnerServer(t, owneridentity.Tenant, []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.Workspace.Service(), owneridentity.ResourceCatalog.Service(), owneridentity.Fabric.Service(), owneridentity.Ledger.Service()}, identity.Register)
	auth := func(owner owneridentity.Owner) *ownerservice.Authorizer {
		return ownerservice.NewAuthorizer(owner, api.NewCloudIdentityAuthorizationClient(liveDial(t, ia, owner, owneridentity.Tenant, livePeerToken)))
	}
	cs, _ := catalogmigrations.Source()
	cdb := liveOwnerDB(t, ctx, dsn, "resource_catalog", cs)
	catalogOwner, err := catalog.New(cdb, auth(owneridentity.ResourceCatalog), catalog.Profile{Provider: "local-docker", Region: "local", BillingMode: "LOCAL_NO_CHARGE", ProviderCapabilityVersion: "provider/v1"})
	if err != nil {
		t.Fatal(err)
	}
	ca := liveOwnerServer(t, owneridentity.ResourceCatalog, []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.Workspace.Service(), owneridentity.Fabric.Service(), owneridentity.Ledger.Service()}, catalogOwner.Register)
	fs, _ := fabricmigrations.Source()
	fdb := liveOwnerDB(t, ctx, dsn, "fabric", fs)
	fabricOwner, err := fabric.New(fdb, auth(owneridentity.Fabric).Authorize, api.NewCatalogCoordinationClient(liveDial(t, ca, owneridentity.Fabric, owneridentity.ResourceCatalog, livePeerToken)))
	if err != nil {
		t.Fatal(err)
	}
	fa := liveOwnerServer(t, owneridentity.Fabric, []owneridentity.Service{owneridentity.Workspace.Service(), owneridentity.Serve.Service()}, fabricOwner.Register)
	ws, _ := migrations.Source()
	wdb := liveOwnerDB(t, ctx, dsn, "workspace", ws)
	wc := api.NewCatalogCoordinationClient(liveDial(t, ca, owneridentity.Workspace, owneridentity.ResourceCatalog, livePeerToken))
	wf := api.NewFabricCoordinationClient(liveDial(t, fa, owneridentity.Workspace, owneridentity.Fabric, livePeerToken))
	wi := api.NewCloudIdentityAuthorizationClient(liveDial(t, ia, owneridentity.Workspace, owneridentity.Tenant, livePeerToken))
	workspaceOwner, err := New(wdb, auth(owneridentity.Workspace), wc, wf, wi)
	if err != nil {
		t.Fatal(err)
	}
	wa := liveOwnerServer(t, owneridentity.Workspace, []owneridentity.Service{owneridentity.ConsoleBFF, owneridentity.Tenant.Service(), owneridentity.Ledger.Service()}, workspaceOwner.Register)
	identity.WorkspaceCommit = api.NewOwnerCommitReadbackClient(liveDial(t, wa, owneridentity.Tenant, owneridentity.Workspace, livePeerToken))
	ldb, ledgerOwner := liveLedgerDB(t, ctx, dsn)
	ledgerOwner.Authorizer = auth(owneridentity.Ledger)
	ledgerOwner.Catalog = api.NewCatalogCoordinationClient(liveDial(t, ca, owneridentity.Ledger, owneridentity.ResourceCatalog, livePeerToken))
	ledgerOwner.Workspace = api.NewOwnerCommitReadbackClient(liveDial(t, wa, owneridentity.Ledger, owneridentity.Workspace, livePeerToken))
	ledgerServer, err := ledgerOwner.NewGRPC(ownerservice.Config{Owner: owneridentity.Ledger, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Workspace.Service(): livePeerToken, owneridentity.Fabric.Service(): livePeerToken}})
	if err != nil {
		t.Fatal(err)
	}
	ledgerListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go ledgerServer.Serve(ledgerListener)
	t.Cleanup(ledgerServer.Stop)
	wl := api.NewLedgerCoordinationClient(liveDial(t, ledgerListener.Addr().String(), owneridentity.Workspace, owneridentity.Ledger, livePeerToken))
	fl := api.NewLedgerCoordinationClient(liveDial(t, ledgerListener.Addr().String(), owneridentity.Fabric, owneridentity.Ledger, livePeerToken))
	workspaceOwner.Ledger, fabricOwner.Ledger = wl, fl
	identityClient, err := bff.DialIdentity(ia, livePeerToken, owneridentity.TLSConfig{AllowInsecureLocal: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(identityClient.Close)
	catalogClient := api.NewResourceCatalogProductServiceClient(liveDialService(t, ca, owneridentity.ConsoleBFF, owneridentity.ResourceCatalog.Service(), livePeerToken))
	workspaceClient := api.NewWorkspaceProductServiceClient(liveDialService(t, wa, owneridentity.ConsoleBFF, owneridentity.Workspace.Service(), livePeerToken))
	catalogFront := httptest.NewServer(bff.NewCatalogHandler(catalogClient, identityClient))
	t.Cleanup(catalogFront.Close)
	workspaceFront := httptest.NewServer(bff.NewWorkspaceHandler(workspaceClient, identityClient))
	t.Cleanup(workspaceFront.Close)
	cookies, csrf := map[string]string{}, map[string]string{}
	for _, email := range []string{"platform-admin@example.test", "tenant-admin@example.test", "member@example.test"} {
		_, challenge, e := identityClient.LoginContext(ctx)
		if e != nil {
			t.Fatal(e)
		}
		session, cookie, e := identityClient.Login(ctx, challenge, &api.LoginRequest{Username: email, Password: "isolated-password"})
		if e != nil {
			t.Fatal(e)
		}
		cookies[email] = cookie
		csrf[email] = session.CsrfToken
	}
	request := func(base, email, method, path, key, body string) (int, []byte) {
		t.Helper()
		r, e := http.NewRequestWithContext(ctx, method, base+path, strings.NewReader(body))
		if e != nil {
			t.Fatal(e)
		}
		r.AddCookie(&http.Cookie{Name: "opl_session", Value: cookies[email]})
		r.Header.Set("X-CSRF-Token", csrf[email])
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Origin", base)
		res, e := http.DefaultClient.Do(r)
		if e != nil {
			t.Fatal(e)
		}
		defer res.Body.Close()
		raw, e := io.ReadAll(res.Body)
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
	admin := "platform-admin@example.test"
	customer := "tenant-admin@example.test"
	cp := post(admin, "/api/v2/admin/catalog/compute-plans", "cp", `{"name":"basic","vcpus":2,"memoryMiB":4096,"providerProfileId":"local/profile","providerSkuId":"local-basic","providerCapabilityVersion":"provider/v1","validFrom":"`+now+`"}`)["id"].(string)
	sp := post(admin, "/api/v2/admin/catalog/storage-plans", "sp", `{"name":"standard","capacityGiB":10,"providerProfileId":"local/profile","providerSkuId":"local-storage","shrinkSupported":true,"validFrom":"`+now+`"}`)["id"].(string)
	rp := post(admin, "/api/v2/admin/catalog/retention-policies", "rp", `{"versionLabel":"retention","customerTerms":"destroy after confirmation"}`)["id"].(string)
	post(admin, "/api/v2/admin/catalog/refund-policies", "refund", `{"versionLabel":"refund","algorithm":"workspace-delete-refund-v1","retentionPolicyVersionId":"`+rp+`","customerTerms":"original policy","validFrom":"`+now+`"}`)
	post(admin, "/api/v2/admin/catalog/price-policies", "price", `{"versionLabel":"price","periodMonths":1,"computeMonthlyUSDMicros":"0","storageMonthlyUSDMicros":"0","productMonthlyUSDMicros":"0","validFrom":"`+now+`","computePlanId":"`+cp+`","storagePlanId":"`+sp+`","renewalPolicy":{"version":"renewal-policy/v1","trigger":"manual_or_explicitly_consented_automatic","effectiveStart":"previous_paid_through","months":1,"usesAcceptedPriceSnapshot":true},"planChangePolicyVersion":"workspace-plan-change-v1"}`)
	quote := post(customer, "/api/v2/quotes", "quote", `{"purpose":"deploy","capabilityVersionId":"cap-live","computePlanId":"`+cp+`","storagePlanId":"`+sp+`","periodMonths":1}`)["id"].(string)
	body := `{"name":"Original workspace","quoteId":"` + quote + `","renewalMode":"manual"}`
	code, raw := request(workspaceFront.URL, customer, "POST", "/api/v2/workspaces", "workspace", body)
	if code != 202 {
		t.Fatalf("create: %d %s", code, raw)
	}
	var public map[string]any
	if json.Unmarshal(raw, &public) != nil {
		t.Fatal(string(raw))
	}
	opID := public["operationId"].(string)
	wid := public["resourceId"].(string)
	op, err := workspaceOwner.Store.ReadOperation(ctx, opID)
	if err != nil {
		t.Fatal(err)
	}
	_, result, err := decodeOrder(op)
	if err != nil {
		t.Fatal(err)
	}
	if result.ResourceSetID == "" {
		t.Fatalf("no Fabric reference: status=%s stage=%s resume=%v", op.Status, op.Stage, workspaceOwner.Resume(ctx, opID))
	}
	if op.Status == "succeeded" || op.Stage != "readback" {
		t.Fatalf("unconfirmed order status=%s stage=%s", op.Status, op.Stage)
	}
	zeroCharge := &api.LocalNoChargeReceiptEvidence{}
	if protojson.Unmarshal(result.ZeroChargeReceipt, zeroCharge) != nil || zeroCharge.GetReceipt().GetId() == "" || zeroCharge.GetReceipt().GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE || zeroCharge.GetQuoteAcceptance().GetWorkspaceId() != wid || zeroCharge.GetOwnerCommitEvidence().GetOperationId() != opID {
		t.Fatalf("missing original Ledger zero-charge evidence: %s", result.ZeroChargeReceipt)
	}
	fabricReceipt, err := fl.ReadLocalNoChargeReceipt(ctx, &api.GetReceiptByReferenceRequest{Context: continuation(op, result.GrantID, "fabric_receipt_read"), Owner: "workspace", OwnerEvidenceReference: opID})
	if err != nil || !proto.Equal(fabricReceipt, zeroCharge) {
		t.Fatalf("Fabric peer receipt differs: %v", err)
	}
	var storedQuote, storedWorkspace, observed string
	if err = fdb.QueryRowContext(ctx, `SELECT accepted_quote_id,workspace_id,observation_result FROM fabric.resource_sets WHERE id=$1`, result.ResourceSetID).Scan(&storedQuote, &storedWorkspace, &observed); err != nil {
		t.Fatal(err)
	}
	if storedQuote != quote || storedWorkspace != wid || observed != "unknown" {
		t.Fatalf("fabric reference not original: %s %s %s", storedQuote, storedWorkspace, observed)
	}
	var acceptedBy string
	if err = cdb.QueryRowContext(ctx, `SELECT accepted_by_operation_id FROM resource_catalog.quotes WHERE id=$1`, quote).Scan(&acceptedBy); err != nil || acceptedBy != opID {
		t.Fatalf("quote acceptance=%s err=%v", acceptedBy, err)
	}
	var subscriptions, providerRefs int
	_ = wdb.QueryRowContext(ctx, `SELECT count(*) FROM workspace.subscriptions`).Scan(&subscriptions)
	_ = fdb.QueryRowContext(ctx, `SELECT count(*) FROM fabric.resources WHERE provider_resource_ref IS NOT NULL`).Scan(&providerRefs)
	if subscriptions != 0 || providerRefs != 0 {
		t.Fatal("pending order fabricated paid or provider facts")
	}
	code, raw = request(workspaceFront.URL, customer, "POST", "/api/v2/workspaces", "workspace", body)
	if code != 202 || !bytes.Contains(raw, []byte(opID)) {
		t.Fatalf("replay: %d %s", code, raw)
	}
	code, raw = request(workspaceFront.URL, customer, "POST", "/api/v2/workspaces", "workspace", strings.Replace(body, "Original workspace", "Other workspace", 1))
	if code != 409 {
		t.Fatalf("conflict: %d %s", code, raw)
	}
	code, raw = request(workspaceFront.URL, "member@example.test", "POST", "/api/v2/workspaces", "member", body)
	if code != 403 {
		t.Fatalf("member create: %d %s", code, raw)
	}
	code, raw = request(workspaceFront.URL, customer, "GET", "/api/v2/workspaces/"+wid, "", "")
	if code != 200 || !bytes.Contains(raw, []byte(`"status":"provisioning"`)) || bytes.Contains(raw, []byte(`"accessUrl":`)) {
		t.Fatalf("workspace read: %d %s", code, raw)
	}
	// A real queued request cannot commit after membership is revoked while it
	// waits for the same command lock. Observe the PostgreSQL wait before revoke.
	queuedQuote := post(customer, "/api/v2/quotes", "queued-quote", `{"purpose":"deploy","capabilityVersionId":"cap-live","computePlanId":"`+cp+`","storagePlanId":"`+sp+`","periodMonths":1}`)["id"].(string)
	observer, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	lock, err := wdb.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lock.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "tenant-live/101/createWorkspace/queued-create"); err != nil {
		t.Fatal(err)
	}
	type response struct {
		code int
		raw  []byte
	}
	queued := make(chan response, 1)
	go func() {
		code, raw := request(workspaceFront.URL, customer, "POST", "/api/v2/workspaces", "queued-create", `{"name":"Must not commit","quoteId":"`+queuedQuote+`","renewalMode":"manual"}`)
		queued <- response{code, raw}
	}()
	deadline := time.Now().Add(5 * time.Second)
	waiting := false
	for time.Now().Before(deadline) {
		if err = observer.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname='opl_workspace' AND wait_event='advisory')`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waiting {
		lock.Rollback()
		t.Fatal("queued create did not reach owner lock")
	}
	if _, err = tenantDB.ExecContext(ctx, `UPDATE tenant.tenant_members SET revoked_at=now() WHERE actor_id='101'; UPDATE tenant.tenants SET permission_version=permission_version+1 WHERE id='tenant-live'`); err != nil {
		lock.Rollback()
		t.Fatal(err)
	}
	if err = lock.Commit(); err != nil {
		t.Fatal(err)
	}
	queuedResult := <-queued
	if queuedResult.code != 403 && queuedResult.code != 401 {
		t.Fatalf("queued revoked create %d %s", queuedResult.code, queuedResult.raw)
	}
	var unwanted int
	if err = wdb.QueryRowContext(ctx, `SELECT count(*) FROM workspace.workspaces WHERE name='Must not commit'`).Scan(&unwanted); err != nil || unwanted != 0 {
		t.Fatalf("queued create committed after revoke: %d %v", unwanted, err)
	}
	if _, err = tenantDB.ExecContext(ctx, `UPDATE tenant.tenant_members SET revoked_at=NULL WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}

	// Expire both the interactive session and context, then create a fresh service
	// instance. Only the same accepted grant may finish/read the original intent.
	_, err = tenantDB.ExecContext(ctx, `UPDATE tenant.sessions SET revoked_at=now() WHERE actor_id='101'; UPDATE tenant.authorization_contexts SET expires_at=now()-interval '1 millisecond' WHERE actor_id='101'`)
	if err != nil {
		t.Fatal(err)
	}
	restarted, err := New(wdb, auth(owneridentity.Workspace), wc, wf, wi)
	if err != nil {
		t.Fatal(err)
	}
	restarted.Ledger = wl
	// Simulate a crash after Ledger committed but before Workspace checkpointed
	// the reply. Recovery must find the same owner receipt through its original key.
	lostReply := result
	lostReply.ZeroChargeReceipt = nil
	lostRaw, _ := json.Marshal(lostReply)
	if _, err = wdb.ExecContext(ctx, `UPDATE workspace.operations SET result=$2 WHERE id=$1`, opID, lostRaw); err != nil {
		t.Fatal(err)
	}
	if err = restarted.Resume(ctx, opID); err != nil {
		t.Fatal(err)
	}
	replayed, _ := restarted.Store.ReadOperation(ctx, opID)
	_, again, _ := decodeOrder(replayed)
	if again.ResourceSetID != result.ResourceSetID || again.FabricOperationID != result.FabricOperationID {
		t.Fatal("restart changed original resource identity")
	}
	recoveredReceipt := &api.LocalNoChargeReceiptEvidence{}
	if protojson.Unmarshal(again.ZeroChargeReceipt, recoveredReceipt) != nil || !proto.Equal(recoveredReceipt, zeroCharge) {
		t.Fatal("restart changed original Ledger evidence")
	}
	var receiptCount int
	if err = ldb.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type='workspace.local_no_charge.v1'`).Scan(&receiptCount); err != nil || receiptCount != 1 {
		t.Fatalf("recovery duplicated Ledger receipt: %d %v", receiptCount, err)
	}
	_, err = tenantDB.ExecContext(ctx, `UPDATE tenant.tenant_members SET revoked_at=now() WHERE actor_id='101'`)
	if err != nil {
		t.Fatal(err)
	}
	if err = restarted.Resume(ctx, opID); err != nil {
		t.Fatalf("revoked original-order closeout: %v", err)
	}
	closed, err := restarted.Store.ReadOperation(ctx, opID)
	if err != nil || closed.Status != "needs_attention" || closed.Stage != "original_action_readback" || closed.ErrorCode != "FORBIDDEN" {
		t.Fatalf("revoked continuation lost refusal: status=%s stage=%s code=%s err=%v", closed.Status, closed.Stage, closed.ErrorCode, err)
	}
	call := &api.CallContext{ActorId: "101", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-live"}}}, AcceptedOperationGrantId: proto.String(result.GrantID), RequestId: "no-new-procurement", IdempotencyKey: opID + "/resources"}
	if _, err = wf.ReadResources(ctx, &api.ResourceReadbackRequest{Context: call, ResourceSetId: result.ResourceSetID}); err != nil {
		t.Fatalf("closeout resource readback: %v", err)
	}
	if got, err := wl.ReadLocalNoChargeReceipt(ctx, &api.GetReceiptByReferenceRequest{Context: call, Owner: "workspace", OwnerEvidenceReference: opID}); err != nil || !proto.Equal(got, zeroCharge) {
		t.Fatalf("closeout receipt readback: %v", err)
	}
	a := &api.QuoteAcceptance{}
	if protojson.Unmarshal(result.Acceptance, a) != nil {
		t.Fatal("acceptance decode")
	}
	if _, err = wf.EnsureResources(ctx, &api.EnsureResourcesCommand{Context: call, WorkspaceId: wid, ObligationId: opID, Plan: a.ResourcePlan, QuoteAcceptance: a}); err == nil {
		t.Fatal("revoked actor authorized new resource dispatch")
	}
	call.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-other"}}}
	if _, err = wf.ReadResources(ctx, &api.ResourceReadbackRequest{Context: call, ResourceSetId: result.ResourceSetID}); err == nil {
		t.Fatal("cross tenant resource read admitted")
	}
	t.Logf("original_order=%s workspace=%s quote=%s resource_set=%s receipt=%s; real zero-charge evidence, provider unconfirmed, no charges or provider dispatch", opID, wid, quote, result.ResourceSetID, zeroCharge.Receipt.Id)
	_ = timestamppb.Now
}
