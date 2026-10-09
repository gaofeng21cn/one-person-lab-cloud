package coordination_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
	"opl-cloud/services/internal/ownerservice"
)

// fabricSurface is the real Fabric target coordination surface with fixture
// Catalog/Identity dependencies and per-peer mTLS identities, so a focused test
// drives the exact typed path a production Serve peer uses.
type fabricSurface struct {
	service *coordination.Service
	catalog *catalog
	token   string
	addr    string
	config  ownerservice.Config
}

func startFabricSurface(t *testing.T, db *sql.DB) *fabricSurface {
	t.Helper()
	catalogOwner := &catalog{acceptances: map[string]*api.QuoteAcceptance{}}
	catalogServer := grpc.NewServer()
	api.RegisterCatalogCoordinationServer(catalogServer, catalogOwner)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go catalogServer.Serve(listener)
	t.Cleanup(catalogServer.Stop)
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.Close() })
	service, err := coordination.New(db, ownerservice.NewAuthorizer(ownerservice.OwnerFabric, &identity{}).Authorize, api.NewCatalogCoordinationClient(connection))
	if err != nil {
		t.Fatal(err)
	}
	token := "isolated-fabric-surface-token-00000001"
	config := ownerservice.Config{Owner: ownerservice.OwnerFabric, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Workspace.Service(): token, owneridentity.Serve.Service(): token}}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(server); err != nil {
		t.Fatal(err)
	}
	fabricListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(fabricListener)
	t.Cleanup(server.Stop)
	return &fabricSurface{service: service, catalog: catalogOwner, token: token, addr: fabricListener.Addr().String(), config: config}
}

func (f *fabricSurface) client(t *testing.T, peer owneridentity.Service) api.FabricCoordinationClient {
	t.Helper()
	opts, err := f.config.TLS.DialOptions(peer, owneridentity.Fabric.Service(), f.token)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(f.addr, opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return api.NewFabricCoordinationClient(conn)
}

// seedConfirmedExecutionResource provisions the confirmed resource set and the
// execution resource Serve binds a Secret into.
func seedConfirmedExecutionResource(t *testing.T, ctx context.Context, db *sql.DB, setID, workspace, tenant string) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO fabric.resource_sets (id,tenant_id,workspace_id,provider,provider_profile_ref,region,compute_plan_id,storage_plan_id,accepted_quote_id,approved_specification,observation_result,observed_at) VALUES ($1,$2,$3,'local-docker','profile','local','cp','sp','quote','{"billingMode":"LOCAL_NO_CHARGE"}'::jsonb,'confirmed',now())`, setID, tenant, workspace); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO fabric.resources (id,resource_set_id,kind,provider_purchase_key,billing_mode,requested_specification,observation_result) VALUES ($1,$2,'execution','execution:'||$1,'LOCAL_NO_CHARGE','{}'::jsonb,'confirmed')`, "res_exec_"+setID, setID); err != nil {
		t.Fatal(err)
	}
}

func activeSecretBindingCount(t *testing.T, ctx context.Context, db *sql.DB, setID, purpose string) (int, string) {
	t.Helper()
	var count int
	var id string
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM fabric.secret_bindings WHERE resource_set_id=$1 AND purpose=$2 AND revoked_at IS NULL`, setID, purpose).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count == 1 {
		if err := db.QueryRowContext(ctx, `SELECT id FROM fabric.secret_bindings WHERE resource_set_id=$1 AND purpose=$2 AND revoked_at IS NULL`, setID, purpose).Scan(&id); err != nil {
			t.Fatal(err)
		}
	}
	return count, id
}

// TestServeSecretBindingRejectsDifferentRuntimeKeyOrSlot proves the Serve-only
// typed path: only Serve may bind or replace, a replay names the exact original
// runtime/slot/key binding/Secret, and a request that names any different
// original is refused without touching the one active binding.
func TestServeSecretBindingRejectsDifferentRuntimeKeyOrSlot(t *testing.T) {
	db := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	surface := startFabricSurface(t, db)
	dispatcher := &rebindDispatcher{}
	surface.service.Dispatcher = dispatcher
	t.Cleanup(func() { surface.service.Dispatcher = nil })

	const workspace, tenant, setID = "workspace-secret-identity", "tenant", "rset_secret_identity"
	seedConfirmedExecutionResource(t, ctx, db, setID, workspace, tenant)
	serve := surface.client(t, owneridentity.Serve.Service())
	workspacePeer := surface.client(t, owneridentity.Workspace.Service())

	const firstFingerprint = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	const secondFingerprint = "sha256:fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
	call := func(key string) *api.CallContext {
		return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, IdempotencyKey: key, SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
	}
	bind := func(key, runtimeID, keyBindingID, slot, fingerprint string) (*api.SecretBindingReadback, error) {
		return serve.BindSecret(ctx, &api.SecretBindingCommand{Context: call(key), WorkspaceId: workspace, RuntimeInstanceId: runtimeID, KeyBindingId: keyBindingID, SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: slot, Fingerprint: fingerprint})
	}
	rebind := func(key, runtimeID, predecessorID, keyBindingID, slot, fingerprint string) (*api.SecretBindingRebindReadback, error) {
		return serve.RebindSecret(ctx, &api.SecretBindingRebindCommand{Context: call(key), WorkspaceId: workspace, RuntimeInstanceId: runtimeID, ExpectedCurrentSecretBindingId: predecessorID, KeyBindingId: keyBindingID, SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: slot, Fingerprint: fingerprint})
	}

	first, err := bind("bind-first", "rt-a", "key-1", "gateway", firstFingerprint)
	if err != nil || first.GetSecretBindingId() == "" || first.GetRuntimeInstanceId() != "rt-a" || dispatcher.calls != 1 {
		t.Fatalf("initial bind=%+v err=%v calls=%d", first, err, dispatcher.calls)
	}
	predecessor := first.GetSecretBindingId()
	replayed, err := bind("bind-first", "rt-a", "key-1", "gateway", firstFingerprint)
	if err != nil || !proto.Equal(first, replayed) || dispatcher.calls != 1 {
		t.Fatalf("exact replay=%+v err=%v calls=%d", replayed, err, dispatcher.calls)
	}
	// A different runtime instance, key binding or slot is a different original:
	// it can neither replay nor relabel the active binding.
	if _, err = bind("bind-other-runtime", "rt-b", "key-1", "gateway", firstFingerprint); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("foreign runtime bind err=%v want already exists", err)
	}
	if _, err = bind("bind-other-key", "rt-a", "key-2", "gateway", firstFingerprint); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("foreign key binding err=%v want already exists", err)
	}
	if count, active := activeSecretBindingCount(t, ctx, db, setID, "gateway"); count != 1 || active != predecessor {
		t.Fatalf("active=%q count=%d want the single original %q", active, count, predecessor)
	}
	if dispatcher.calls != 1 {
		t.Fatalf("refused identities reached the provider: calls=%d", dispatcher.calls)
	}

	// Replacement carries the same runtime identity rules: a foreign runtime or a
	// foreign slot is refused before the predecessor is retired.
	if _, err = rebind("rebind-other-runtime", "rt-b", predecessor, "key-9", "gateway", secondFingerprint); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("foreign runtime rebind err=%v want failed precondition", err)
	}
	if _, err = rebind("rebind-other-slot", "rt-a", predecessor, "key-9", "other-slot", secondFingerprint); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("foreign slot rebind err=%v want failed precondition", err)
	}
	if count, active := activeSecretBindingCount(t, ctx, db, setID, "gateway"); count != 1 || active != predecessor {
		t.Fatalf("refused rebind changed state: active=%q count=%d", active, count)
	}
	if _, err = workspacePeer.RebindSecret(ctx, &api.SecretBindingRebindCommand{Context: call("rebind-workspace"), WorkspaceId: workspace, RuntimeInstanceId: "rt-a", ExpectedCurrentSecretBindingId: predecessor, KeyBindingId: "key-2", SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: secondFingerprint}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("retired Workspace rebind err=%v want permission denied", err)
	}
	if _, err = workspacePeer.BindSecret(ctx, &api.SecretBindingCommand{Context: call("bind-workspace"), WorkspaceId: workspace, RuntimeInstanceId: "rt-a", KeyBindingId: "key-9", SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: secondFingerprint}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("retired Workspace bind err=%v want permission denied", err)
	}

	replacement, err := rebind("rebind-replace", "rt-a", predecessor, "key-2", "gateway", secondFingerprint)
	if err != nil || replacement.GetPreviousSecretBindingId() != predecessor || replacement.GetSecretBindingId() == predecessor || replacement.GetVersion() == "" || dispatcher.calls != 2 {
		t.Fatalf("replacement=%+v err=%v calls=%d", replacement, err, dispatcher.calls)
	}
	if count, active := activeSecretBindingCount(t, ctx, db, setID, "gateway"); count != 1 || active != replacement.GetSecretBindingId() {
		t.Fatalf("active=%q count=%d want replacement %q", active, count, replacement.GetSecretBindingId())
	}
	replacementReplay, err := rebind("rebind-replace", "rt-a", predecessor, "key-2", "gateway", secondFingerprint)
	if err != nil || !proto.Equal(replacement, replacementReplay) || dispatcher.calls != 2 {
		t.Fatalf("replacement replay=%+v err=%v calls=%d", replacementReplay, err, dispatcher.calls)
	}
	// The retired predecessor is no longer the CAS identity, and the superseded
	// initial bind never resurrects itself over the replacement.
	if _, err = rebind("rebind-stale", "rt-a", predecessor, "key-3", "gateway", firstFingerprint); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale predecessor rebind err=%v want failed precondition", err)
	}
	if _, err = bind("bind-after-replace", "rt-a", "key-1", "gateway", firstFingerprint); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("stale initial bind err=%v want already exists", err)
	}
	if count, active := activeSecretBindingCount(t, ctx, db, setID, "gateway"); count != 1 || active != replacement.GetSecretBindingId() {
		t.Fatalf("stale requests changed the active binding: active=%q count=%d", active, count)
	}
}

// TestConcurrentRebindKeepsOneActiveBinding proves the CAS under concurrency:
// two replacements naming the same predecessor serialize on the original
// Workspace, exactly one retires it, and one active binding remains.
func TestConcurrentRebindKeepsOneActiveBinding(t *testing.T) {
	db := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	surface := startFabricSurface(t, db)
	dispatcher := &rebindDispatcher{}
	surface.service.Dispatcher = dispatcher
	t.Cleanup(func() { surface.service.Dispatcher = nil })

	const workspace, tenant, setID = "workspace-secret-cas", "tenant", "rset_secret_cas"
	seedConfirmedExecutionResource(t, ctx, db, setID, workspace, tenant)
	serve := surface.client(t, owneridentity.Serve.Service())
	call := func(key string) *api.CallContext {
		return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, IdempotencyKey: key, SessionId: proto.String("session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
	}
	first, err := serve.BindSecret(ctx, &api.SecretBindingCommand{Context: call("cas-bind"), WorkspaceId: workspace, RuntimeInstanceId: "rt-cas", KeyBindingId: "key-1", SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"})
	if err != nil || first.GetSecretBindingId() == "" {
		t.Fatalf("initial bind=%+v err=%v", first, err)
	}
	predecessor := first.GetSecretBindingId()

	type outcome struct {
		readback *api.SecretBindingRebindReadback
		err      error
	}
	results := make(chan outcome, 2)
	var wg sync.WaitGroup
	for index, fingerprint := range []string{
		"sha256:1111111111111111111111111111111111111111111111111111111111111111",
		"sha256:2222222222222222222222222222222222222222222222222222222222222222",
	} {
		wg.Add(1)
		go func(index int, fingerprint string) {
			defer wg.Done()
			readback, err := serve.RebindSecret(ctx, &api.SecretBindingRebindCommand{Context: call(fmt.Sprintf("cas-rebind-%d", index)), WorkspaceId: workspace, RuntimeInstanceId: "rt-cas", ExpectedCurrentSecretBindingId: predecessor, KeyBindingId: fmt.Sprintf("key-%d", index+2), SecretDeliveryReference: "opl-gateway-" + workspace, TargetSlot: "gateway", Fingerprint: fingerprint})
			results <- outcome{readback: readback, err: err}
		}(index, fingerprint)
	}
	wg.Wait()
	close(results)
	var won *api.SecretBindingRebindReadback
	refused := 0
	for result := range results {
		switch {
		case result.err == nil:
			if won != nil {
				t.Fatalf("two concurrent replacements won: %+v", result.readback)
			}
			won = result.readback
		case status.Code(result.err) == codes.FailedPrecondition:
			refused++
		default:
			t.Fatalf("unexpected concurrent rebind error: %v", result.err)
		}
	}
	if won == nil || refused != 1 || won.GetPreviousSecretBindingId() != predecessor {
		t.Fatalf("won=%+v refused=%d", won, refused)
	}
	if count, active := activeSecretBindingCount(t, ctx, db, setID, "gateway"); count != 1 || active != won.GetSecretBindingId() {
		t.Fatalf("active=%q count=%d want single winner %q", active, count, won.GetSecretBindingId())
	}
	if dispatcher.calls != 2 {
		t.Fatalf("provider confirmations=%d want exactly the bind and the winning replacement", dispatcher.calls)
	}
}

// TestServeResourceReadbackCarriesConfirmedSpecsAndMountReferences proves the
// Serve readback of a confirmed non-Local dispatch: the exact prepaid specs,
// provider resource references and the PV/PVC mount reference of the original
// provider request are persisted and projected, not re-derived by the caller.
func TestServeResourceReadbackCarriesConfirmedSpecsAndMountReferences(t *testing.T) {
	db := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	surface := startFabricSurface(t, db)
	surface.service.Dispatcher = &tencentFixture{}
	surface.service.Ledger = &walletLedger{receipt: &api.Receipt{Id: "wallet-receipt", Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String("obligation-serve-refs"), Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION, Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED}}
	t.Cleanup(func() { surface.service.Dispatcher = nil; surface.service.Ledger = nil })
	workspace := surface.client(t, owneridentity.Workspace.Service())
	serve := surface.client(t, owneridentity.Serve.Service())
	command := tencentCommand("serve-refs")
	surface.catalog.acceptances[command.QuoteAcceptance.Quote.Id] = proto.Clone(command.QuoteAcceptance).(*api.QuoteAcceptance)

	got, err := workspace.EnsureResources(ctx, command)
	if err != nil || got.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || got.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		t.Fatalf("confirmed dispatch=%v err=%v", got, err)
	}
	read, err := serve.ReadResources(ctx, &api.ResourceReadbackRequest{Context: command.Context, ResourceSetId: got.ResourceId})
	if err != nil {
		t.Fatal(err)
	}
	binding := read.GetExecutionResources()
	if read.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED || binding.GetAccountId() != "acct-a" || binding.GetComputeAllocationId() == "" || binding.GetStorageVolumeId() == "" || binding.GetDataAttachmentId() == "" || binding.GetDataAttachmentOperationId() != got.OperationId+":attachment" {
		t.Fatalf("execution binding=%v", read)
	}
	placement := read.GetApplicationPlacement()
	if placement == nil || placement.GetComputeNodeName() != "10.66.0.10" || placement.GetComputePackageId() != "pkg-basic" ||
		placement.GetComputeNodePoolId() != "np-basic" || placement.GetComputeInstanceId() != "ins-"+binding.GetComputeAllocationId() ||
		placement.GetComputeMachineName() != "machine-"+binding.GetComputeAllocationId() ||
		!strings.HasPrefix(placement.GetStoragePvcName(), "opl-") || !strings.HasSuffix(placement.GetStoragePvcName(), "-data") {
		t.Fatalf("application placement=%v", placement)
	}
	// The provider resource references are the original request's readback facts.
	kinds := map[string]string{}
	for _, resource := range read.Resources {
		kinds[resource.Kind] = resource.OpaqueProviderReference
	}
	if kinds["compute"] != "ins-"+binding.GetComputeAllocationId() || kinds["storage"] != "disk-"+binding.GetStorageVolumeId() || kinds["network"] != "tke-node-pool/np-basic" {
		t.Fatalf("provider resource facts=%v", kinds)
	}
	// The persisted rows carry the accepted specification and the provider's
	// observed specification, not a re-serialized caller claim.
	var requestedProvider, requestedBilling, observedComputeRef, purchaseKey string
	var computeExpiry time.Time
	if err = db.QueryRowContext(ctx, `SELECT requested_specification->>'provider', requested_specification->>'billingMode', observed_specification->>'providerResourceId', provider_purchase_key, provider_expires_at FROM fabric.resources WHERE resource_set_id=$1 AND kind='compute'`, got.ResourceId).Scan(&requestedProvider, &requestedBilling, &observedComputeRef, &purchaseKey, &computeExpiry); err != nil {
		t.Fatal(err)
	}
	if requestedProvider != "tencent-tke" || requestedBilling != "PREPAID_MONTHLY" || observedComputeRef != "ins-"+binding.GetComputeAllocationId() || purchaseKey == "" || !computeExpiry.After(time.Now().UTC()) {
		t.Fatalf("compute spec=%q/%q/%q key=%q expiry=%v", requestedProvider, requestedBilling, observedComputeRef, purchaseKey, computeExpiry)
	}
	var observedStorageRef, diskType string
	var observedStorage []byte
	var storageExpiry time.Time
	if err = db.QueryRowContext(ctx, `SELECT observed_specification->>'providerResourceId', observed_specification->>'diskType', observed_specification, provider_expires_at FROM fabric.resources WHERE resource_set_id=$1 AND kind='storage'`, got.ResourceId).Scan(&observedStorageRef, &diskType, &observedStorage, &storageExpiry); err != nil {
		t.Fatal(err)
	}
	if observedStorageRef != "disk-"+binding.GetStorageVolumeId() || diskType != "CLOUD_BSSD" || !storageExpiry.After(time.Now().UTC()) {
		t.Fatalf("storage spec=%q/%q expiry=%v", observedStorageRef, diskType, storageExpiry)
	}
	// The projected mount claim is the provider-confirmed storage's own PVC name,
	// not a name re-derived from the caller request.
	var observedVolume fabric.StorageVolume
	if err = json.Unmarshal(observedStorage, &observedVolume); err != nil {
		t.Fatal(err)
	}
	if placement.GetStoragePvcName() != fabric.StoragePVCName(observedVolume) {
		t.Fatalf("placement pvc=%q want %q", placement.GetStoragePvcName(), fabric.StoragePVCName(observedVolume))
	}
	// The confirmed attachment carries its provider-authoritative PV/PVC mount
	// reference in the operation result and the persisted effect identity, and the
	// funding evidence beside it is the owner-read confirmed wallet charge.
	var attachmentRef, effectRef, fundingSource string
	var approvedInput []byte
	if err = db.QueryRowContext(ctx, `SELECT o.result->'attachment'->>'providerAttachmentId', a.approved_input->'resourceEffectIdentity'->'attachment'->>'providerReference', a.approved_input->'fundingEvidence'->>'source', a.approved_input FROM fabric.operations o JOIN fabric.resource_actions a ON a.command_id=o.id WHERE o.id=$1`, got.OperationId).Scan(&attachmentRef, &effectRef, &fundingSource, &approvedInput); err != nil {
		t.Fatal(err)
	}
	if attachmentRef != "pv/pv-x:pvc/pvc-x" || !strings.HasPrefix(effectRef, "pv/") || fundingSource != "ledger_confirmed_wallet_charge" {
		t.Fatalf("attachment=%q effect=%q funding=%q", attachmentRef, effectRef, fundingSource)
	}
	var identityWrapper struct {
		ResourceEffectIdentity struct {
			Provider string `json:"provider"`
			Compute  struct {
				ProviderReference string `json:"providerReference"`
				ProviderRequestID string `json:"providerRequestId"`
			} `json:"compute"`
		} `json:"resourceEffectIdentity"`
	}
	if err = json.Unmarshal(approvedInput, &identityWrapper); err != nil {
		t.Fatal(err)
	}
	if identityWrapper.ResourceEffectIdentity.Provider != "tencent-tke" ||
		identityWrapper.ResourceEffectIdentity.Compute.ProviderReference != "ins-"+binding.GetComputeAllocationId() ||
		!strings.HasPrefix(identityWrapper.ResourceEffectIdentity.Compute.ProviderRequestID, "req-cvm-") {
		t.Fatalf("effect identity=%s", approvedInput)
	}
}
