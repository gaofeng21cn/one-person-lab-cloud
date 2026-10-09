package eventconsumer

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	ledgerhttp "opl-cloud/services/ledger/internal/http"
	"opl-cloud/services/ledger/internal/ledger"
)

// Dependencies are typed owner-response stubs over real gRPC, not a claim of
// full cross-owner qualification. Ledger persistence is real isolated PostgreSQL.
type coordinationOwners struct {
	api.UnimplementedCloudIdentityAuthorizationServer
	api.UnimplementedCatalogCoordinationServer
	api.UnimplementedOwnerCommitReadbackServer
	api.UnimplementedGatewayCoordinationServer
	mu                      sync.Mutex
	quote                   *api.QuoteAcceptance
	commit                  *api.OwnerCommitEvidence
	wallet                  *api.WalletOperation
	walletReads             int
	deny                    bool
	quoteReads, commitReads int
	authorizations          int
}

func (o *coordinationOwners) ReadWalletAction(_ context.Context, r *api.WalletReadbackRequest) (*api.WalletOperation, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.walletReads++
	if o.wallet == nil || r.GetWalletOperationId() != o.wallet.GetId() {
		return nil, status.Error(codes.NotFound, "wallet operation absent")
	}
	return proto.Clone(o.wallet).(*api.WalletOperation), nil
}

func (o *coordinationOwners) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.authorizations++
	if o.deny {
		return nil, status.Error(codes.PermissionDenied, "denied")
	}
	if r.AudienceOwner != api.OwnerEnum_OWNER_ENUM_LEDGER || r.Resource.GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE || r.Resource.GetId() != o.quote.WorkspaceId {
		return nil, status.Error(codes.PermissionDenied, "wrong Ledger authorization binding")
	}
	return &api.AuthorizationDecision{Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		ActorId: r.ActorId, Scope: r.Scope, SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId, AudienceOwner: r.AudienceOwner, Action: r.Action, Resource: r.Resource,
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, nil
}
func (o *coordinationOwners) ReadQuoteResourcePlan(_ context.Context, r *api.QuoteResourcePlanRequest) (*api.QuoteAcceptance, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.quoteReads++
	if r.Context.GetAuthorizationContextId() != "" || r.Context.GetAcceptedOperationGrantId() != "grant-local" {
		return nil, status.Error(codes.PermissionDenied, "foreign authorization context or missing accepted grant")
	}
	if r.QuoteId != o.quote.Quote.Id {
		return nil, status.Error(codes.NotFound, "quote absent")
	}
	return proto.Clone(o.quote).(*api.QuoteAcceptance), nil
}
func (o *coordinationOwners) ReadOwnerCommit(_ context.Context, r *api.ReadOwnerCommitRequest) (*api.OwnerCommitEvidence, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.commitReads++
	if r.Owner != o.commit.Owner || r.OperationId != o.commit.OperationId || r.ResourceId != o.commit.ResourceId {
		return nil, status.Error(codes.NotFound, "commit absent")
	}
	return proto.Clone(o.commit).(*api.OwnerCommitEvidence), nil
}

const coordinationToken = "isolated-ledger-coordination-token-0001"

func coordinationDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("LEDGER_TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("OPL_POSTGRES_TESTS") == "1" {
			dsn = "connect_timeout=10"
		} else {
			t.Skip("LEDGER_TEST_DATABASE_URL is not set")
		}
	}
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err = admin.PingContext(ctx); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("ledger_coord_%d", time.Now().UnixNano())
	if _, err = admin.ExecContext(ctx, "CREATE SCHEMA "+pq.QuoteIdentifier(schema)); err != nil {
		t.Fatal(err)
	}
	testDSN := dsn
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		testDSN = u.String()
	} else {
		testDSN += " search_path=" + pq.QuoteLiteral(schema)
	}
	db, err := sql.Open("postgres", testDSN)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() {
		db.Close()
		admin.Exec("DROP SCHEMA " + pq.QuoteIdentifier(schema) + " CASCADE")
		admin.Close()
	})
	return db
}
func coordinationFixtureRequest() *api.AppendReceiptRequest {
	scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-local"}}}
	workspace, order := "workspace-local", "order-local"
	q := &api.QuoteAcceptance{Quote: &api.Quote{Id: "quote-local", Status: api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED, ComputePlanId: "compute-local", StoragePlanId: "storage-local", TotalUsdMicros: 0,
		LineItems: []*api.QuoteLine{{Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_COMPUTE, AmountUsdMicros: 0}, {Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_STORAGE, AmountUsdMicros: 0}}},
		ObligationId: order, AcceptanceId: "accepted-local", SnapshotDigest: "sha256:" + strings.Repeat("a", 64), WorkspaceId: workspace,
		ResourcePlan: &api.ResourcePlanSnapshot{ComputePlanId: "compute-local", StoragePlanId: "storage-local", Provider: "local-docker", Region: "local", BillingMode: "LOCAL_NO_CHARGE"}}
	commit := &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: order, ResourceId: workspace, AcceptedInputDigest: "sha256:" + strings.Repeat("b", 64), CommittedVersion: 1,
		AcceptedAt: timestamppb.New(time.Now().Add(-time.Minute)), AuthorizationContextId: "accepted-authorization", ActorId: "actor-local", Scope: scope, AcceptedAction: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE}
	return &api.AppendReceiptRequest{Context: &api.CallContext{RequestId: "request-first", IdempotencyKey: "append-first", ActorId: "actor-local", Scope: scope, AcceptedOperationGrantId: proto.String("grant-local")},
		Receipt:        &api.Receipt{Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(order), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, EvidenceSummary: "Accepted local zero-charge quote"},
		EvidenceDigest: q.SnapshotDigest, OwnerEvidenceReference: order, QuoteAcceptance: q, OwnerCommitEvidence: commit}
}
func startCoordinationWire(t *testing.T, server *grpc.Server) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}
func coordinationClient(t *testing.T, address string, owner owneridentity.Owner) api.LedgerCoordinationClient {
	t.Helper()
	options, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(owner.Service(), owneridentity.Ledger.Service(), coordinationToken)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return api.NewLedgerCoordinationClient(conn)
}
func coordinatedService(t *testing.T) (*sql.DB, *Server, *coordinationOwners, api.LedgerCoordinationClient, api.LedgerCoordinationClient) {
	t.Helper()
	db := coordinationDB(t)
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := coordinationFixtureRequest()
	owners := &coordinationOwners{quote: request.QuoteAcceptance, commit: request.OwnerCommitEvidence}
	wire := grpc.NewServer()
	api.RegisterCloudIdentityAuthorizationServer(wire, owners)
	api.RegisterCatalogCoordinationServer(wire, owners)
	api.RegisterOwnerCommitReadbackServer(wire, owners)
	api.RegisterGatewayCoordinationServer(wire, owners)
	peerAddress := startCoordinationWire(t, wire)
	config := ownerservice.Config{Owner: owneridentity.Ledger, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Workspace.Service(): coordinationToken, owneridentity.Fabric.Service(): coordinationToken, owneridentity.Build.Service(): coordinationToken}, CloudIdentityAddr: peerAddress, CloudIdentityToken: coordinationToken}
	close, err := s.ConfigureCoordination(config, func(name string) string {
		if strings.HasSuffix(name, "_ADDR") {
			return peerAddress
		}
		if strings.HasSuffix(name, "_TOKEN") {
			return coordinationToken
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(close)
	server, err := s.NewGRPC(config)
	if err != nil {
		t.Fatal(err)
	}
	address := startCoordinationWire(t, server)
	return db, s, owners, coordinationClient(t, address, owneridentity.Workspace), coordinationClient(t, address, owneridentity.Fabric)
}

func TestLocalNoChargeReceiptPostgresWirePersistence(t *testing.T) {
	db, s, owners, client, fabric := coordinatedService(t)
	ctx := context.Background()
	request := coordinationFixtureRequest()
	// Use the exact fixed owner timestamp; caller evidence cannot drift.
	request.OwnerCommitEvidence = proto.Clone(owners.commit).(*api.OwnerCommitEvidence)
	request.Context.AuthorizationContextId = "ledger-audience-context"
	first, err := client.AppendReceipt(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Id == "" || first.CreatedAt == nil || first.Kind != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE {
		t.Fatalf("receipt=%v", first)
	}
	replay := proto.Clone(request).(*api.AppendReceiptRequest)
	replay.Context.RequestId = "request-retry"
	replay.Context.IdempotencyKey = "append-second"
	second, err := client.AppendReceipt(ctx, replay)
	if err != nil || !proto.Equal(first, second) {
		t.Fatalf("replay differs: %v %v", second, err)
	}
	read := &api.GetReceiptByReferenceRequest{Context: replay.Context, Owner: "workspace", OwnerEvidenceReference: request.OwnerEvidenceReference}
	actual, err := fabric.ReadLocalNoChargeReceipt(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(actual.Receipt, first) || !proto.Equal(actual.QuoteAcceptance, request.QuoteAcceptance) || !proto.Equal(actual.OwnerCommitEvidence, request.OwnerCommitEvidence) {
		t.Fatalf("typed evidence differs")
	}
	public, err := client.ReadReceiptByReference(ctx, read)
	if err != nil || !proto.Equal(public, first) {
		t.Fatal(err)
	}
	restarted := ledger.NewPostgresStore(db)
	stored, err := restarted.ReadLocalNoChargeReceipt(ctx, request.OwnerEvidenceReference)
	if err != nil || !proto.Equal(stored.Evidence, actual) {
		t.Fatalf("restart read=%v", err)
	}
	conflict := proto.Clone(request).(*api.AppendReceiptRequest)
	conflict.Receipt.EvidenceSummary = "conflicting payload"
	if _, err = client.AppendReceipt(ctx, conflict); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflict=%v", err)
	}
	if _, err = s.store.RecordReceipt(ctx, ledger.ReceiptInput{Type: ledger.LocalNoChargeReceiptType, Status: "completed", Surface: "cloud", WorkspaceID: "workspace-local", IdempotencyKey: "forged"}); err != ledger.ErrInvalidReceiptInput {
		t.Fatalf("generic writer=%v", err)
	}
	legacy, err := s.store.RecordReceipt(ctx, ledger.ReceiptInput{Type: "source_check", Status: "completed", Surface: "cloud", WorkspaceID: "legacy-workspace", IdempotencyKey: "legacy-receipt"})
	if err != nil || legacy.ReceiptID == "" {
		t.Fatalf("legacy=%v", err)
	}
	if _, err = s.store.Receipt(ctx, first.Id); err != ledger.ErrReceiptNotFound {
		t.Fatalf("generic read=%v", err)
	}
	page, err := s.store.ListReceipts(ctx, ledger.ReceiptQuery{WorkspaceID: "workspace-local"})
	if err != nil || len(page.Receipts) != 0 {
		t.Fatalf("generic list leaked typed evidence: %v %v", page, err)
	}
	if _, err = s.store.UpdateReceiptRetention(ctx, ledger.ReceiptRetentionInput{ReceiptID: first.Id, IdempotencyKey: "legacy-retention", LegalHold: true}); err != ledger.ErrReceiptNotFound {
		t.Fatalf("generic retention=%v", err)
	}
	if _, err = s.store.PrivacyDeleteReceipt(ctx, ledger.ReceiptPrivacyDeleteInput{ReceiptID: first.Id, IdempotencyKey: "legacy-privacy", Reason: "legacy caller"}); err != ledger.ErrReceiptNotFound {
		t.Fatalf("generic privacy=%v", err)
	}
	httpHandler := ledgerhttp.NewServer(s.store, coordinationToken)
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/ledger/receipts/" + first.Id, http.StatusNotFound},
		{"/ledger/receipts?workspaceId=workspace-local", http.StatusOK},
		{"/ledger/receipts/" + legacy.ReceiptID, http.StatusOK},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		r.Header.Set("Authorization", "Bearer "+coordinationToken)
		out := httptest.NewRecorder()
		httpHandler.ServeHTTP(out, r)
		if out.Code != tc.status || strings.Contains(out.Body.String(), "localNoCharge") {
			t.Fatalf("HTTP %s status=%d body=%s", tc.path, out.Code, out.Body.String())
		}
	}
	afterLegacy, err := fabric.ReadLocalNoChargeReceipt(ctx, read)
	if err != nil || !proto.Equal(afterLegacy, actual) {
		t.Fatalf("legacy operations changed typed evidence: %v", err)
	}
	var receipts int
	if err = db.QueryRow(`SELECT count(*) FROM evidence_receipts WHERE receipt_type=$1`, ledger.LocalNoChargeReceiptType).Scan(&receipts); err != nil || receipts != 1 {
		t.Fatalf("receipt rows=%d %v", receipts, err)
	}
}

func TestLocalNoChargeReceiptRejectsUnverifiedOrForeignEvidence(t *testing.T) {
	_, s, owners, client, fabric := coordinatedService(t)
	ctx := context.Background()
	base := coordinationFixtureRequest()
	base.OwnerCommitEvidence = proto.Clone(owners.commit).(*api.OwnerCommitEvidence)
	for _, tc := range []struct {
		name   string
		mutate func(*api.AppendReceiptRequest)
		code   codes.Code
	}{
		{"paid quote", func(r *api.AppendReceiptRequest) { r.QuoteAcceptance.Quote.TotalUsdMicros = 1 }, codes.InvalidArgument},
		{"nonzero line", func(r *api.AppendReceiptRequest) { r.QuoteAcceptance.Quote.LineItems[0].AmountUsdMicros = 1 }, codes.InvalidArgument},
		{"cloud provider", func(r *api.AppendReceiptRequest) { r.QuoteAcceptance.ResourcePlan.Provider = "tencent" }, codes.InvalidArgument},
		{"noncanonical local provider", func(r *api.AppendReceiptRequest) { r.QuoteAcceptance.ResourcePlan.Provider = "local" }, codes.InvalidArgument},
		{"paid mode", func(r *api.AppendReceiptRequest) { r.QuoteAcceptance.ResourcePlan.BillingMode = "PREPAID_MONTHLY" }, codes.InvalidArgument},
		{"foreign workspace", func(r *api.AppendReceiptRequest) { r.QuoteAcceptance.WorkspaceId = "foreign" }, codes.InvalidArgument},
		{"forged quote", func(r *api.AppendReceiptRequest) { r.QuoteAcceptance.ResourcePlan.Region = "foreign" }, codes.FailedPrecondition},
		{"forged owner commit", func(r *api.AppendReceiptRequest) { r.OwnerCommitEvidence.CommittedVersion++ }, codes.FailedPrecondition},
		{"caller receipt id", func(r *api.AppendReceiptRequest) { r.Receipt.Id = "invented" }, codes.InvalidArgument},
		{"foreign actor", func(r *api.AppendReceiptRequest) { r.Context.ActorId = "foreign" }, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := proto.Clone(base).(*api.AppendReceiptRequest)
			tc.mutate(r)
			if _, err := client.AppendReceipt(ctx, r); status.Code(err) != tc.code {
				t.Fatalf("error=%v", err)
			}
		})
	}
	if _, err := fabric.AppendReceipt(ctx, base); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Fabric append=%v", err)
	}
	owners.mu.Lock()
	owners.deny = true
	owners.mu.Unlock()
	if _, err := client.AppendReceipt(ctx, base); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked=%v", err)
	}
	owners.mu.Lock()
	owners.deny = false
	owners.mu.Unlock()
	if _, err := client.AppendReceipt(ctx, base); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"tenant", "actor"} {
		r := &api.GetReceiptByReferenceRequest{Context: proto.Clone(base.Context).(*api.CallContext), Owner: "workspace", OwnerEvidenceReference: base.OwnerEvidenceReference}
		if field == "tenant" {
			r.Context.Scope.GetTenant().TenantId = "foreign"
		} else {
			r.Context.ActorId = "foreign"
		}
		if _, err := fabric.ReadLocalNoChargeReceipt(ctx, r); status.Code(err) != codes.PermissionDenied {
			t.Fatalf("foreign %s read=%v", field, err)
		}
	}
	s.Catalog = nil
	if _, err := client.AppendReceipt(ctx, base); status.Code(err) != codes.Unavailable {
		t.Fatalf("missing owner=%v", err)
	}
}

func TestLocalNoChargeReceiptConcurrentSameAndConflictingKeys(t *testing.T) {
	db, _, owners, client, _ := coordinatedService(t)
	base := coordinationFixtureRequest()
	base.OwnerCommitEvidence = proto.Clone(owners.commit).(*api.OwnerCommitEvidence)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	ids := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := proto.Clone(base).(*api.AppendReceiptRequest)
			r.Context.IdempotencyKey = fmt.Sprintf("concurrent-%d", i)
			out, err := client.AppendReceipt(context.Background(), r)
			results <- err
			if err == nil {
				ids <- out.Id
			}
		}(i)
	}
	wg.Wait()
	close(results)
	close(ids)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for v := range ids {
		if id != "" && id != v {
			t.Fatal("duplicate receipts")
		}
		id = v
	}
	var count int
	db.QueryRow(`SELECT count(*) FROM evidence_receipts WHERE receipt_type=$1`, ledger.LocalNoChargeReceiptType).Scan(&count)
	if count != 1 {
		t.Fatalf("rows=%d", count)
	}
	other := proto.Clone(base).(*api.AppendReceiptRequest)
	other.Context.IdempotencyKey = "concurrent-0"
	other.OwnerEvidenceReference = "different-order"
	other.Receipt.OperationId = proto.String(other.OwnerEvidenceReference)
	other.QuoteAcceptance.ObligationId = other.OwnerEvidenceReference
	other.OwnerCommitEvidence.OperationId = other.OwnerEvidenceReference
	owners.mu.Lock()
	owners.quote = proto.Clone(other.QuoteAcceptance).(*api.QuoteAcceptance)
	owners.commit = proto.Clone(other.OwnerCommitEvidence).(*api.OwnerCommitEvidence)
	owners.mu.Unlock()
	if _, err := client.AppendReceipt(context.Background(), other); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("key retarget=%v", err)
	}
}

func TestLocalNoChargeReceiptReauthorizesAfterPostgresLockWait(t *testing.T) {
	db, _, owners, client, _ := coordinatedService(t)
	r := coordinationFixtureRequest()
	r.OwnerCommitEvidence = proto.Clone(owners.commit).(*api.OwnerCommitEvidence)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	blocker, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Rollback()
	var blockerPID int
	if err = blocker.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	keyBytes, err := json.Marshal([]string{"ledger.local_no_charge", "workspace", r.OwnerEvidenceReference})
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(keyBytes)
	lockKey := "ledger.local_no_charge:reference:" + hex.EncodeToString(digest[:])
	if _, err = blocker.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, lockKey); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() { _, err := client.AppendReceipt(ctx, r); result <- err }()
	for {
		var waiting bool
		if err = db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case err := <-result:
			t.Fatalf("append finished before lock wait: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
	owners.mu.Lock()
	owners.deny = true
	admissions := owners.authorizations
	owners.mu.Unlock()
	if admissions != 1 {
		t.Fatalf("expected one admission before lock wait, got %d", admissions)
	}
	if err = blocker.Commit(); err != nil {
		t.Fatal(err)
	}
	if err = <-result; status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked queued append=%v", err)
	}
	var receipts, keys int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type=$1`, ledger.LocalNoChargeReceiptType).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM idempotency_keys WHERE service='ledger.local_no_charge'`).Scan(&keys); err != nil {
		t.Fatal(err)
	}
	if receipts != 0 || keys != 0 {
		t.Fatalf("revoked request persisted receipts=%d keys=%d", receipts, keys)
	}
	owners.mu.Lock()
	owners.deny = false
	owners.mu.Unlock()
	if _, err = client.AppendReceipt(ctx, r); err != nil {
		t.Fatalf("authorized retry after rollback=%v", err)
	}
}

// paidCoordinationRequest builds a paid Workspace order's WALLET_ACTION evidence:
// a nonzero accepted quote, the Gateway charge that moved the money, and the same
// owner commit. It is the only shape that may be recorded as a funded obligation.
func paidCoordinationRequest() *api.AppendReceiptRequest {
	r := coordinationFixtureRequest()
	r.QuoteAcceptance.Quote.TotalUsdMicros = 52_580_000
	r.QuoteAcceptance.Quote.LineItems = []*api.QuoteLine{{Kind: api.QuoteLineKindEnum_QUOTE_LINE_KIND_ENUM_COMPUTE, AmountUsdMicros: 52_580_000}}
	r.QuoteAcceptance.ResourcePlan.Provider = "tencent"
	r.QuoteAcceptance.ResourcePlan.BillingMode = "PREPAID_MONTHLY"
	r.Receipt.Kind = api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION
	r.Receipt.EvidenceSummary = "Gateway wallet charge confirmed"
	r.WalletOperation = &api.WalletOperation{Id: "wallet-operation-local", WorkspaceId: proto.String("workspace-local"), Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE, AmountUsdMicros: 52_580_000, Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED, ReceiptId: proto.String("receipt-wallet-local"), CreatedAt: timestamppb.Now()}
	r.Context.IdempotencyKey = "append-wallet"
	return r
}

// TestWalletActionReceiptPostgresWirePersistence proves the paid funding path end
// to end against real Ledger persistence: the confirmed Gateway charge is recorded
// as a WALLET_ACTION receipt, read back by reference by both Fabric and the public
// reader, replays identically, and cannot be forged or routed to the generic writer.
func TestWalletActionReceiptPostgresWirePersistence(t *testing.T) {
	db, s, owners, client, fabric := coordinatedService(t)
	ctx := context.Background()
	request := paidCoordinationRequest()
	owners.quote = proto.Clone(request.QuoteAcceptance).(*api.QuoteAcceptance)
	owners.wallet = proto.Clone(request.WalletOperation).(*api.WalletOperation)
	owners.commit = proto.Clone(request.OwnerCommitEvidence).(*api.OwnerCommitEvidence)
	request.OwnerCommitEvidence = proto.Clone(owners.commit).(*api.OwnerCommitEvidence)

	first, err := client.AppendReceipt(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetId() != "receipt-wallet-local" || first.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION || first.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || first.GetCreatedAt() == nil {
		t.Fatalf("receipt=%v", first)
	}
	if owners.walletReads != 1 {
		t.Fatalf("the Gateway charge was read back %d times", owners.walletReads)
	}
	// Replaying by a new key returns the identical receipt rather than a second one.
	replay := proto.Clone(request).(*api.AppendReceiptRequest)
	replay.Context.IdempotencyKey = "append-wallet-retry"
	second, err := client.AppendReceipt(ctx, replay)
	if err != nil || !proto.Equal(first, second) {
		t.Fatalf("replay differs: %v %v", second, err)
	}
	read := &api.GetReceiptByReferenceRequest{Context: replay.Context, Owner: "workspace", OwnerEvidenceReference: request.OwnerEvidenceReference}
	evidence, err := fabric.ReadWalletActionReceipt(ctx, read)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(evidence.GetReceipt(), first) || !proto.Equal(evidence.GetWalletOperation(), request.WalletOperation) || evidence.GetEvidenceDigest() != request.EvidenceDigest {
		t.Fatalf("typed Wallet action evidence differs")
	}
	// The public read-by-reference resolves the same reference to the wallet receipt.
	public, err := client.ReadReceiptByReference(ctx, read)
	if err != nil || !proto.Equal(public, first) {
		t.Fatalf("public read=%v %v", public, err)
	}
	// A restart reads the same persisted evidence.
	restarted := ledger.NewPostgresStore(db)
	stored, err := restarted.ReadWalletActionReceipt(ctx, request.OwnerEvidenceReference)
	if err != nil || !proto.Equal(stored.Evidence, evidence) {
		t.Fatalf("restart read=%v", err)
	}
	// A receipt whose charge the wallet owner did not authorise is refused, and the
	// generic writer may never mint this type.
	foreign := paidCoordinationRequest()
	owners.quote = proto.Clone(foreign.QuoteAcceptance).(*api.QuoteAcceptance)
	owners.wallet = proto.Clone(foreign.WalletOperation).(*api.WalletOperation)
	owners.wallet.AmountUsdMicros++
	if _, err = client.AppendReceipt(ctx, foreign); err == nil {
		t.Fatal("a charge that differs from the wallet owner's amount was recorded")
	}
	if _, err = s.store.RecordReceipt(ctx, ledger.ReceiptInput{Type: ledger.WalletActionReceiptType, Status: "completed", Surface: "cloud", WorkspaceID: "workspace-local", IdempotencyKey: "forged-wallet"}); err != ledger.ErrInvalidReceiptInput {
		t.Fatalf("generic writer accepted the wallet type: %v", err)
	}
}

// inboxWireService starts the real Ledger domain listener over an isolated
// PostgreSQL store with every peer identity the current matrix installs:
// Workspace/Fabric for coordination, Build/Capability/Serve for their event
// streams, Resource Catalog for the platform-scope policy publication, and
// Gateway as the contract owner of wallet.operation_observed.v1.
func inboxWireService(t *testing.T) (*sql.DB, *Server, string) {
	t.Helper()
	db := coordinationDB(t)
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	peers := map[owneridentity.Service]string{}
	for _, owner := range []owneridentity.Owner{owneridentity.Workspace, owneridentity.Fabric, owneridentity.Build, owneridentity.Capability, owneridentity.Serve, owneridentity.ResourceCatalog, owneridentity.Gateway} {
		peers[owner.Service()] = coordinationToken
	}
	config := ownerservice.Config{Owner: owneridentity.Ledger, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: peers}
	server, err := s.NewGRPC(config)
	if err != nil {
		t.Fatal(err)
	}
	return db, s, startCoordinationWire(t, server)
}

func domainInboxClient(t *testing.T, address string, owner owneridentity.Owner) api.DomainInboxClient {
	t.Helper()
	options, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(owner.Service(), owneridentity.Ledger.Service(), coordinationToken)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return api.NewDomainInboxClient(conn)
}

// catalogPolicyEnvelope is the exact envelope shape the Resource Catalog Outbox
// hands to its Ledger delivery loop: platform scope, no tenant, aggregate equal
// to the policy version the payload names.
func catalogPolicyEnvelope() *api.EventEnvelope {
	return &api.EventEnvelope{
		EventId: "evt-catalog-policy-wire-1", EventType: "catalog.policy_changed.v1", SchemaVersion: 1,
		Owner: "resource_catalog", Scope: "platform", AggregateId: "policy-wire-1", AggregateVersion: 1,
		RequestId: "req-policy-wire-1", OccurredAt: timestamppb.New(time.Now().UTC()),
		Payload: &api.EventEnvelope_CatalogPolicyChanged{CatalogPolicyChanged: &api.CatalogPolicyChangedEvent{
			PolicyVersionId: "policy-wire-1", PolicyKind: "price", ValidFrom: timestamppb.New(time.Now().Add(-time.Hour).UTC()),
		}},
	}
}

// TestDomainInboxAcceptsRealResourceCatalogPublication proves the previously
// refused real producer path end to end over the wire: the platform-scope
// catalog.policy_changed.v1 delivery is acknowledged, persisted with exact
// immutable identity, replayed without a second row, and read back through the
// existing store reader. Forged producers and conflicting bytes stay refused.
func TestDomainInboxAcceptsRealResourceCatalogPublication(t *testing.T) {
	db, s, address := inboxWireService(t)
	ctx := context.Background()
	catalog := domainInboxClient(t, address, owneridentity.ResourceCatalog)
	event := catalogPolicyEnvelope()
	ack, err := catalog.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "resource_catalog", Event: event})
	if err != nil {
		t.Fatalf("deliver catalog policy: %v", err)
	}
	if ack.GetEventId() != event.EventId || ack.GetConsumer() != "ledger" || !ack.GetCommitted() || ack.GetDuplicate() {
		t.Fatalf("ack=%v", ack)
	}
	var receiptID, requestHash string
	var org, workspace, artifact string
	if err = db.QueryRowContext(ctx, `SELECT id, organization_id, workspace_id, artifact_id, request_hash FROM evidence_receipts WHERE receipt_type='catalog.policy_changed.v1' AND idempotency_key='domain:resource_catalog:'||$1`, event.EventId).Scan(&receiptID, &org, &workspace, &artifact, &requestHash); err != nil {
		t.Fatal(err)
	}
	if org != "platform" || workspace != "" || artifact == "" || requestHash == "" {
		t.Fatalf("stored policy evidence org=%q workspace=%q artifact=%q hash=%q", org, workspace, artifact, requestHash)
	}
	// The identical bytes replay as the original receipt, never a second fact.
	replay, err := catalog.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "resource_catalog", Event: proto.Clone(event).(*api.EventEnvelope)})
	if err != nil || !replay.GetCommitted() || !replay.GetDuplicate() {
		t.Fatalf("replay=%v %v", replay, err)
	}
	var rows int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type='catalog.policy_changed.v1'`).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("rows=%d %v", rows, err)
	}
	// Readback through the store's receipt reader returns the same immutable id.
	stored, err := s.store.Receipt(ctx, receiptID)
	if err != nil || stored.ReceiptID != receiptID || stored.Type != "catalog.policy_changed.v1" || stored.OrganizationID != "platform" {
		t.Fatalf("readback=%+v %v", stored, err)
	}
	// The same event id with different policy bytes is refused, not replaced.
	conflict := proto.Clone(event).(*api.EventEnvelope)
	conflict.GetCatalogPolicyChanged().PolicyKind = "retention"
	if _, err = catalog.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "resource_catalog", Event: conflict}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting policy bytes = %v, want AlreadyExists", err)
	}
	// A peer claiming another producer's envelope is refused before persistence.
	forged := proto.Clone(event).(*api.EventEnvelope)
	forged.EventId = "evt-forged-1"
	if _, err = catalog.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "build", Event: forged}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("forged producer = %v, want PermissionDenied", err)
	}
	build := domainInboxClient(t, address, owneridentity.Build)
	if _, err = build.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "build", Event: proto.Clone(forged).(*api.EventEnvelope)}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign owner envelope = %v, want PermissionDenied", err)
	}
	// A readiness event smuggled through the platform scope is refused, while the
	// legal tenant-scope shape still commits: tenancy was not loosened.
	serve := domainInboxClient(t, address, owneridentity.Serve)
	smuggled := &api.EventEnvelope{EventId: "evt-readiness-platform", EventType: "serve.agent_readiness_observed.v1", SchemaVersion: 1, Owner: "serve", Scope: "platform", AggregateId: "dep-wire-1", AggregateVersion: 1, RequestId: "req-readiness-1", OccurredAt: timestamppb.New(time.Now().UTC()), Payload: &api.EventEnvelope_RuntimeReadinessObserved{RuntimeReadinessObserved: &api.RuntimeReadinessObservedEvent{RuntimeInstanceId: "rt-wire-1", WorkspaceId: "ws-wire-1", DeploymentId: "dep-wire-1", Outcome: "unknown"}}}
	if _, err = serve.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "serve", Event: smuggled}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("platform readiness = %v, want InvalidArgument", err)
	}
	legal := proto.Clone(smuggled).(*api.EventEnvelope)
	legal.EventId, legal.Scope, legal.TenantId = "evt-readiness-tenant", "tenant", "tenant-wire-1"
	if ack, err = serve.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "serve", Event: legal}); err != nil || !ack.GetCommitted() {
		t.Fatalf("tenant readiness = %v %v", ack, err)
	}
	// The generic HTTP writer still cannot mint either newly typed evidence row.
	httpHandler := ledgerhttp.NewServer(s.store, coordinationToken)
	body := `{"type":"catalog.policy_changed.v1","status":"completed","surface":"cloud","organizationId":"platform","requestId":"req-forged","idempotencyKey":"forged-catalog"}`
	req := httptest.NewRequest(http.MethodPost, "/ledger/receipts", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+coordinationToken)
	req.Header.Set("Idempotency-Key", "forged-catalog")
	out := httptest.NewRecorder()
	httpHandler.ServeHTTP(out, req)
	if out.Code != http.StatusBadRequest || strings.Contains(out.Body.String(), "receiptId") {
		t.Fatalf("generic HTTP write status=%d body=%s", out.Code, out.Body.String())
	}
}

// firstChainObservationShapes returns the three contract-listed observation
// events this receiver now decodes, typed from their contract payloads. These
// are typed fixtures: the Serve and Gateway producer delivery loops are their
// own owners' write sets, so this matrix proves the Ledger receiver and the
// exact envelope identities those producers must deliver.
func firstChainObservationShapes() []struct {
	name     string
	owner    owneridentity.Owner
	envelope *api.EventEnvelope
} {
	now := timestamppb.New(time.Now().UTC())
	return []struct {
		name     string
		owner    owneridentity.Owner
		envelope *api.EventEnvelope
	}{
		{
			name: "serve.access_observed.v1", owner: owneridentity.Serve,
			envelope: &api.EventEnvelope{EventId: "evt-observe-access-1", EventType: "serve.access_observed.v1", SchemaVersion: 1, Owner: "serve", Scope: "tenant", TenantId: "tenant-observe", AggregateId: "switch-observe-1", AggregateVersion: 1, RequestId: "req-observe-1", OccurredAt: now,
				Payload: &api.EventEnvelope_RouteObserved{RouteObserved: &api.RouteObservedEvent{WorkspaceId: "workspace-observe", SwitchId: "switch-observe-1", ActionKind: "activate", RouteGeneration: 2, ExecutionEpoch: 1, RouteRevision: "route-revision-observe-1", Outcome: "confirmed", RouteReceiptId: proto.String("route-receipt-observe-1")}}},
		},
		{
			name: "fabric.resources_observed.v1", owner: owneridentity.Serve,
			envelope: &api.EventEnvelope{EventId: "evt-observe-resources-1", EventType: "fabric.resources_observed.v1", SchemaVersion: 1, Owner: "serve", Scope: "tenant", TenantId: "tenant-observe", AggregateId: "resource-set-observe-1", AggregateVersion: 1, RequestId: "req-observe-2", OccurredAt: now,
				Payload: &api.EventEnvelope_ResourcesObserved{ResourcesObserved: &api.ResourcesObservedEvent{ResourceSetId: "resource-set-observe-1", WorkspaceId: "workspace-observe", ResourceActionId: "resource-action-observe-1", Outcome: "confirmed", AbsenceConfirmed: false}}},
		},
		{
			name: "wallet.operation_observed.v1", owner: owneridentity.Gateway,
			envelope: &api.EventEnvelope{EventId: "evt-observe-wallet-1", EventType: "wallet.operation_observed.v1", SchemaVersion: 1, Owner: "gateway", Scope: "tenant", TenantId: "tenant-observe", AggregateId: "wallet-operation-observe-1", AggregateVersion: 1, RequestId: "req-observe-3", OccurredAt: now,
				Payload: &api.EventEnvelope_WalletOperationObserved{WalletOperationObserved: &api.WalletOperationObservedEvent{WalletOperationId: "wallet-operation-observe-1", WorkspaceId: "workspace-observe", Kind: "charge", Status: "confirmed", AmountUsdMicros: 52_580_000, ReceiptId: proto.String("wallet-receipt-observe-1")}}},
		},
	}
}

// TestDomainInboxAcceptsFirstChainObservationEvents proves the three
// previously-refused first-chain observation events are consumable by the real
// Ledger gRPC listener: first delivery commits once, identical bytes replay as
// Duplicate, changed bytes under the same identity are refused, an independent
// store reader reads back the identical immutable row bound to its workspace,
// and the generic HTTP writer still cannot mint any of the three types.
func TestDomainInboxAcceptsFirstChainObservationEvents(t *testing.T) {
	db, s, address := inboxWireService(t)
	ctx := context.Background()
	clients := map[owneridentity.Owner]api.DomainInboxClient{}
	for _, owner := range []owneridentity.Owner{owneridentity.Serve, owneridentity.Gateway} {
		clients[owner] = domainInboxClient(t, address, owner)
	}
	for _, shape := range firstChainObservationShapes() {
		t.Run(shape.name, func(t *testing.T) {
			client, event := clients[shape.owner], shape.envelope
			ack, err := client.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: shape.owner.String(), Event: event})
			if err != nil || !ack.GetCommitted() || ack.GetDuplicate() || ack.GetEventId() != event.EventId || ack.GetAppliedAggregateVersion() != event.AggregateVersion {
				t.Fatalf("first delivery ack=%v err=%v", ack, err)
			}
			var count int
			var receiptID string
			if err = db.QueryRowContext(ctx, `SELECT count(*), max(id) FROM evidence_receipts WHERE receipt_type=$1 AND idempotency_key='domain:'||$2||':'||$3`, shape.name, shape.owner.String(), event.EventId).Scan(&count, &receiptID); err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Fatalf("%s rows=%d", shape.name, count)
			}
			// Lost acknowledgement: the producer retries the original bytes.
			replay, err := client.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: shape.owner.String(), Event: proto.Clone(event).(*api.EventEnvelope)})
			if err != nil || !replay.GetCommitted() || !replay.GetDuplicate() {
				t.Fatalf("replay ack=%v err=%v", replay, err)
			}
			// Conflicting bytes under the same event identity never replace the fact.
			conflict := proto.Clone(event).(*api.EventEnvelope)
			conflict.RequestId = event.RequestId + "-conflict"
			if _, err = client.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: shape.owner.String(), Event: conflict}); status.Code(err) != codes.AlreadyExists {
				t.Fatalf("%s conflicting bytes = %v, want AlreadyExists", shape.name, err)
			}
			// An independent store reader over the same database returns the exact
			// immutable row, bound to the observed workspace.
			restarted := ledger.NewPostgresStore(db)
			stored, err := restarted.Receipt(ctx, receiptID)
			if err != nil || stored.ReceiptID != receiptID || stored.Type != shape.name || stored.OrganizationID != "tenant-observe" || stored.WorkspaceID != "workspace-observe" || stored.RequestID != event.RequestId {
				t.Fatalf("%s restart readback=%+v err=%v", shape.name, stored, err)
			}
			if stored.ArtifactID == "" || stored.IdempotencyKey != "" {
				t.Fatalf("%s stored evidence missing derived digest: %+v", shape.name, stored)
			}
		})
	}
	// The generic HTTP writer must never mint these typed rows.
	httpHandler := ledgerhttp.NewServer(s.store, coordinationToken)
	for _, shape := range firstChainObservationShapes() {
		body := `{"type":"` + shape.name + `","status":"completed","surface":"cloud","organizationId":"tenant-observe","workspaceId":"workspace-observe","requestId":"req-forged","idempotencyKey":"forged-` + shape.name + `"}`
		req := httptest.NewRequest(http.MethodPost, "/ledger/receipts", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+coordinationToken)
		req.Header.Set("Idempotency-Key", "forged-"+shape.name)
		out := httptest.NewRecorder()
		httpHandler.ServeHTTP(out, req)
		if out.Code != http.StatusBadRequest {
			t.Fatalf("generic HTTP write for %s status=%d body=%s", shape.name, out.Code, out.Body.String())
		}
	}
}

// TestFirstChainObservationRejections runs the refusal matrix over the real
// Ledger gRPC boundary with isolated PostgreSQL. Every case mutates exactly one
// contract identity or payload field of an otherwise legal envelope, so a pass
// proves that specific guard and nothing wider.
func TestFirstChainObservationRejections(t *testing.T) {
	db, _, address := inboxWireService(t)
	ctx := context.Background()
	serve := domainInboxClient(t, address, owneridentity.Serve)
	gateway := domainInboxClient(t, address, owneridentity.Gateway)
	build := domainInboxClient(t, address, owneridentity.Build)
	base := map[string]*api.EventEnvelope{}
	for _, shape := range firstChainObservationShapes() {
		base[shape.name] = shape.envelope
	}
	type rejectCase struct {
		name   string
		event  *api.EventEnvelope
		owner  owneridentity.Owner
		client api.DomainInboxClient
		mutate func(*api.EventEnvelope)
		code   codes.Code
	}
	cases := []rejectCase{
		{name: "access missing workspace", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.GetRouteObserved().WorkspaceId = "" }, code: codes.InvalidArgument},
		{name: "access wrong owner", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.Owner = "gateway" }, code: codes.PermissionDenied},
		{name: "access platform scope", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.Scope, e.TenantId = "platform", "" }, code: codes.InvalidArgument},
		{name: "access platform scope with tenant", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.Scope = "platform" }, code: codes.InvalidArgument},
		{name: "resources platform scope", event: base["fabric.resources_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.Scope, e.TenantId = "platform", "" }, code: codes.InvalidArgument},
		{name: "wallet platform scope", event: base["wallet.operation_observed.v1"], owner: owneridentity.Gateway, client: gateway, mutate: func(e *api.EventEnvelope) { e.Scope, e.TenantId = "platform", "" }, code: codes.InvalidArgument},
		{name: "access missing tenant", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.TenantId = "" }, code: codes.InvalidArgument},
		{name: "access wrong aggregate", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.AggregateId = "other-switch" }, code: codes.InvalidArgument},
		{name: "access missing switch", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.GetRouteObserved().SwitchId = "" }, code: codes.InvalidArgument},
		{name: "access bad action kind", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.GetRouteObserved().ActionKind = "swap" }, code: codes.InvalidArgument},
		{name: "access negative generation", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.GetRouteObserved().RouteGeneration = -1 }, code: codes.InvalidArgument},
		{name: "access missing revision", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.GetRouteObserved().RouteRevision = "" }, code: codes.InvalidArgument},
		{name: "access bad outcome", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.GetRouteObserved().Outcome = "maybe" }, code: codes.InvalidArgument},
		{name: "access empty optional receipt", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.GetRouteObserved().RouteReceiptId = proto.String("") }, code: codes.InvalidArgument},
		{name: "access nil payload", event: base["serve.access_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.Payload = nil }, code: codes.InvalidArgument},
		{name: "resources wrong aggregate", event: base["fabric.resources_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.AggregateId = "other-set" }, code: codes.InvalidArgument},
		{name: "resources missing action", event: base["fabric.resources_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.GetResourcesObserved().ResourceActionId = "" }, code: codes.InvalidArgument},
		{name: "resources absence claim without confirmed outcome", event: base["fabric.resources_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) {
			e.GetResourcesObserved().Outcome = "unknown"
			e.GetResourcesObserved().AbsenceConfirmed = true
		}, code: codes.InvalidArgument},
		{name: "resources bad outcome", event: base["fabric.resources_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.GetResourcesObserved().Outcome = "done" }, code: codes.InvalidArgument},
		{name: "wallet wrong owner", event: base["wallet.operation_observed.v1"], owner: owneridentity.Gateway, client: gateway, mutate: func(e *api.EventEnvelope) { e.Owner = "serve" }, code: codes.PermissionDenied},
		{name: "wallet wrong aggregate", event: base["wallet.operation_observed.v1"], owner: owneridentity.Gateway, client: gateway, mutate: func(e *api.EventEnvelope) { e.AggregateId = "other-operation" }, code: codes.InvalidArgument},
		{name: "wallet bad kind", event: base["wallet.operation_observed.v1"], owner: owneridentity.Gateway, client: gateway, mutate: func(e *api.EventEnvelope) { e.GetWalletOperationObserved().Kind = "withdrawal" }, code: codes.InvalidArgument},
		{name: "wallet bad status", event: base["wallet.operation_observed.v1"], owner: owneridentity.Gateway, client: gateway, mutate: func(e *api.EventEnvelope) { e.GetWalletOperationObserved().Status = "settled" }, code: codes.InvalidArgument},
		{name: "wallet negative amount", event: base["wallet.operation_observed.v1"], owner: owneridentity.Gateway, client: gateway, mutate: func(e *api.EventEnvelope) { e.GetWalletOperationObserved().AmountUsdMicros = -1 }, code: codes.InvalidArgument},
		{name: "wallet bad purpose", event: base["wallet.operation_observed.v1"], owner: owneridentity.Gateway, client: gateway, mutate: func(e *api.EventEnvelope) { e.GetWalletOperationObserved().Purpose = proto.String("donation") }, code: codes.InvalidArgument},
		{name: "wallet half coverage", event: base["wallet.operation_observed.v1"], owner: owneridentity.Gateway, client: gateway, mutate: func(e *api.EventEnvelope) {
			e.GetWalletOperationObserved().CoverageStart = timestamppb.New(time.Now().UTC())
		}, code: codes.InvalidArgument},
		{name: "serve cannot claim gateway wallet observation", event: base["wallet.operation_observed.v1"], owner: owneridentity.Serve, client: serve, mutate: func(e *api.EventEnvelope) { e.EventId = "evt-forged-wallet" }, code: codes.PermissionDenied},
		{name: "build cannot claim serve access observation", event: base["serve.access_observed.v1"], owner: owneridentity.Build, client: build, mutate: func(e *api.EventEnvelope) { e.EventId = "evt-forged-access" }, code: codes.PermissionDenied},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			event := proto.Clone(tc.event).(*api.EventEnvelope)
			event.EventId = fmt.Sprintf("evt-reject-%d", i)
			tc.mutate(event)
			_, err := tc.client.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: tc.owner.String(), Event: event})
			if status.Code(err) != tc.code {
				t.Fatalf("%s = %v, want %s", tc.name, err, tc.code)
			}
		})
	}
	// The refusal matrix must not have persisted a single observation row.
	var rows int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type IN ('serve.access_observed.v1','fabric.resources_observed.v1','wallet.operation_observed.v1')`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("refused observations persisted %d rows", rows)
	}
	// Cross-tenant re-use of a committed event identity never replaces the
	// original fact: the same event id with a different tenant is refused as a
	// conflict against the persisted request hash.
	legal := proto.Clone(base["serve.access_observed.v1"]).(*api.EventEnvelope)
	legal.EventId = "evt-tenant-reuse"
	if ack, err := serve.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "serve", Event: legal}); err != nil || !ack.GetCommitted() {
		t.Fatalf("legal access delivery=%v err=%v", ack, err)
	}
	foreignTenant := proto.Clone(legal).(*api.EventEnvelope)
	foreignTenant.TenantId = "tenant-other"
	if _, err := serve.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "serve", Event: foreignTenant}); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("cross-tenant identity reuse = %v, want AlreadyExists", err)
	}
	var storedTenant string
	if err := db.QueryRowContext(ctx, `SELECT organization_id FROM evidence_receipts WHERE idempotency_key='domain:serve:'||$1`, legal.EventId).Scan(&storedTenant); err != nil || storedTenant != legal.TenantId {
		t.Fatalf("stored tenant=%q err=%v", storedTenant, err)
	}
}

// lostResponseInboxClient is the lost-acknowledgement producer view: the call
// reaches the real Ledger gRPC listener and commits, but the caller never sees
// the acknowledgement, exactly as a dropped response would behave. The
// producer's legal recovery is to retry the identical original bytes.
type lostResponseInboxClient struct{ api.DomainInboxClient }

func (c lostResponseInboxClient) Deliver(ctx context.Context, r *api.DeliverEventRequest, opts ...grpc.CallOption) (*api.InboxAck, error) {
	if _, err := c.DomainInboxClient.Deliver(ctx, r, opts...); err != nil {
		return nil, err
	}
	return nil, status.Error(codes.Unavailable, "acknowledgement lost after commit")
}

// restartInboxWire starts a second real Ledger gRPC service instance over the
// same database: a new store and listener are constructed, and only the
// database survives. This proves readback across a service restart at the
// listener/store layer; it is not a killed-OS-process or SIGKILL test.
func restartInboxWire(t *testing.T, db *sql.DB) (*Server, string) {
	t.Helper()
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	peers := map[owneridentity.Service]string{}
	for _, owner := range []owneridentity.Owner{owneridentity.Workspace, owneridentity.Fabric, owneridentity.Build, owneridentity.Capability, owneridentity.Serve, owneridentity.ResourceCatalog, owneridentity.Gateway} {
		peers[owner.Service()] = coordinationToken
	}
	config := ownerservice.Config{Owner: owneridentity.Ledger, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: peers}
	server, err := s.NewGRPC(config)
	if err != nil {
		t.Fatal(err)
	}
	return s, startCoordinationWire(t, server)
}

// TestFirstChainObservationLostAckAndServiceRestart proves original-identity
// recovery for the three observation events: the producer commits through the
// real listener but loses the acknowledgement, retries the identical bytes,
// gets the original fact back as a duplicate, and a second service instance over
// the same database reads back exactly one immutable row per event.
func TestFirstChainObservationLostAckAndServiceRestart(t *testing.T) {
	db, _, address := inboxWireService(t)
	ctx := context.Background()
	type committed struct {
		eventID, receiptID, requestHash, owner string
		event                                  *api.EventEnvelope
	}
	var facts []committed
	for _, shape := range firstChainObservationShapes() {
		client := domainInboxClient(t, address, shape.owner)
		lost := lostResponseInboxClient{client}
		if _, err := lost.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: shape.owner.String(), Event: proto.Clone(shape.envelope).(*api.EventEnvelope)}); status.Code(err) != codes.Unavailable {
			t.Fatalf("%s lost-ack send = %v, want Unavailable", shape.name, err)
		}
		var receiptID, requestHash string
		if err := db.QueryRowContext(ctx, `SELECT id, request_hash FROM evidence_receipts WHERE idempotency_key='domain:'||$1||':'||$2`, shape.owner.String(), shape.envelope.EventId).Scan(&receiptID, &requestHash); err != nil {
			t.Fatalf("%s was not committed by the lost acknowledgement: %v", shape.name, err)
		}
		facts = append(facts, committed{eventID: shape.envelope.EventId, receiptID: receiptID, requestHash: requestHash, owner: shape.owner.String(), event: shape.envelope})
	}
	// A restarted service instance answers the producer's retry with the original
	// committed fact instead of recording a second one.
	restarted, restartAddress := restartInboxWire(t, db)
	for _, fact := range facts {
		client := domainInboxClient(t, restartAddress, owneridentity.Owner(fact.owner))
		ack, err := client.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: fact.owner, Event: proto.Clone(fact.event).(*api.EventEnvelope)})
		if err != nil || !ack.GetCommitted() || !ack.GetDuplicate() || ack.GetEventId() != fact.eventID {
			t.Fatalf("%s restart retry ack=%v err=%v", fact.eventID, ack, err)
		}
		stored, err := restarted.store.Receipt(ctx, fact.receiptID)
		if err != nil || stored.ReceiptID != fact.receiptID || stored.RequestID != fact.event.RequestId {
			t.Fatalf("%s restarted store readback=%+v err=%v", fact.eventID, stored, err)
		}
		var rows int
		if err := db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE idempotency_key='domain:'||$1||':'||$2 AND request_hash=$3`, fact.owner, fact.eventID, fact.requestHash).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 1 {
			t.Fatalf("%s has %d immutable rows after retry", fact.eventID, rows)
		}
	}
}

// currentProducerShapes returns one envelope per event type that has a real
// producer in this source tree, byte-shaped from that producer's own append
// call. They are typed fixtures: the producer processes are not started here,
// because ledger's write set cannot run another owner's delivery loop, and the
// per-event producer source locations are recorded in the step receipt.
func currentProducerShapes() []struct {
	name     string
	owner    owneridentity.Owner
	envelope *api.EventEnvelope
	digest   string
} {
	now := timestamppb.New(time.Now().UTC())
	sha := "sha256:" + strings.Repeat("ab", 32)
	return []struct {
		name     string
		owner    owneridentity.Owner
		envelope *api.EventEnvelope
		digest   string
	}{
		{
			name: "package.uploaded.v1", owner: owneridentity.Capability, digest: sha,
			envelope: &api.EventEnvelope{EventId: "evt-shape-package", EventType: "package.uploaded.v1", SchemaVersion: 1, Owner: "capability", Scope: "tenant", TenantId: "tenant-shape", AggregateId: "pv-shape-1", AggregateVersion: 1, RequestId: "req-shape-1", OccurredAt: now,
				Payload: &api.EventEnvelope_PackageUploaded{PackageUploaded: &api.PackageUploadedEvent{PackageVersionId: "pv-shape-1", PackageId: "pkg-shape-1", Sha256: sha, SizeBytes: 2048}}},
		},
		{
			name: "build.artifact_confirmed.v1", owner: owneridentity.Build, digest: sha,
			envelope: &api.EventEnvelope{EventId: "evt-shape-artifact", EventType: "build.artifact_confirmed.v1", SchemaVersion: 1, Owner: "build", Scope: "tenant", TenantId: "tenant-shape", AggregateId: "job-shape-1", AggregateVersion: 1, RequestId: "req-shape-2", OccurredAt: now,
				Payload: &api.EventEnvelope_BuildArtifactConfirmed{BuildArtifactConfirmed: &api.BuildArtifactConfirmedEvent{BuildJobId: "job-shape-1", PackageVersionId: "pv-shape-1", RuntimeVersionId: "rt-shape-1", WebuiVersionId: "webui-shape-1", ArtifactDigest: sha, ArtifactReceiptId: "artifact-shape-1", DeploymentDescriptorDigest: "sha256:" + strings.Repeat("cd", 32)}}},
		},
		{
			name: "build.failed.v1", owner: owneridentity.Build,
			envelope: &api.EventEnvelope{EventId: "evt-shape-failed", EventType: "build.failed.v1", SchemaVersion: 1, Owner: "build", Scope: "tenant", TenantId: "tenant-shape", AggregateId: "job-shape-2", AggregateVersion: 1, RequestId: "req-shape-3", OccurredAt: now,
				Payload: &api.EventEnvelope_BuildFailed{BuildFailed: &api.BuildFailedEvent{BuildJobId: "job-shape-2", ErrorCode: "build_failed"}}},
		},
		{
			name: "capability.version_registered.v1", owner: owneridentity.Capability, digest: sha,
			envelope: &api.EventEnvelope{EventId: "evt-shape-registered", EventType: "capability.version_registered.v1", SchemaVersion: 1, Owner: "capability", Scope: "tenant", TenantId: "tenant-shape", AggregateId: "capv-shape-1", AggregateVersion: 1, RequestId: "req-shape-4", OccurredAt: now,
				Payload: &api.EventEnvelope_CapabilityVersionRegistered{CapabilityVersionRegistered: &api.CapabilityVersionRegisteredEvent{CapabilityVersionId: "capv-shape-1", BuildJobId: "job-shape-1", ArtifactDigest: sha}}},
		},
		{
			name: "serve.agent_readiness_observed.v1", owner: owneridentity.Serve,
			envelope: &api.EventEnvelope{EventId: "evt-shape-readiness", EventType: "serve.agent_readiness_observed.v1", SchemaVersion: 1, Owner: "serve", Scope: "tenant", TenantId: "tenant-shape", AggregateId: "dep-shape-1", AggregateVersion: 3, RequestId: "req-shape-5", OccurredAt: now,
				Payload: &api.EventEnvelope_RuntimeReadinessObserved{RuntimeReadinessObserved: &api.RuntimeReadinessObservedEvent{RuntimeInstanceId: "rt-shape-1", WorkspaceId: "ws-shape-1", DeploymentId: "dep-shape-1", Outcome: "confirmed", ApplicationAvailable: true, ReceiptId: proto.String("readiness-shape-1"), AppliedModelConfigurationVersion: 2}}},
		},
	}
}

// TestDomainInboxAcceptsEveryCurrentProducerShape runs the acceptance matrix
// for every event type that has a live producer in this tree: authenticated
// delivery commits once, identical bytes replay as the original receipt after a
// lost acknowledgement, tampered bytes with the same identity are refused, the
// row survives an independent store reader, and refusal peers stay outside the
// producer set.
func TestDomainInboxAcceptsEveryCurrentProducerShape(t *testing.T) {
	db, _, address := inboxWireService(t)
	ctx := context.Background()
	clients := map[owneridentity.Owner]api.DomainInboxClient{}
	for _, owner := range []owneridentity.Owner{owneridentity.Build, owneridentity.Capability, owneridentity.Serve} {
		clients[owner] = domainInboxClient(t, address, owner)
	}
	for _, shape := range currentProducerShapes() {
		t.Run(shape.name, func(t *testing.T) {
			client := clients[shape.owner]
			event := shape.envelope
			ack, err := client.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: shape.owner.String(), Event: event})
			if err != nil || !ack.GetCommitted() || ack.GetDuplicate() || ack.GetEventId() != event.EventId || ack.GetAppliedAggregateVersion() != event.AggregateVersion {
				t.Fatalf("first delivery ack=%v err=%v", ack, err)
			}
			// Lost acknowledgement: the producer retries the original bytes and must
			// be answered with the same committed fact, not a second one.
			replay, err := client.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: shape.owner.String(), Event: proto.Clone(event).(*api.EventEnvelope)})
			if err != nil || !replay.GetCommitted() || !replay.GetDuplicate() {
				t.Fatalf("replay ack=%v err=%v", replay, err)
			}
			var count int
			var receiptID, requestHash string
			if err = db.QueryRowContext(ctx, `SELECT count(*), max(id), max(request_hash) FROM evidence_receipts WHERE receipt_type=$1 AND idempotency_key='domain:'||$2||':'||$3`, shape.name, shape.owner.String(), event.EventId).Scan(&count, &receiptID, &requestHash); err != nil {
				t.Fatal(err)
			}
			if count != 1 || requestHash == "" {
				t.Fatalf("%s rows=%d", shape.name, count)
			}
			if shape.digest != "" {
				var artifact string
				if err = db.QueryRowContext(ctx, `SELECT artifact_id FROM evidence_receipts WHERE id=$1`, receiptID).Scan(&artifact); err != nil || artifact != shape.digest {
					t.Fatalf("%s artifact=%q err=%v", shape.name, artifact, err)
				}
			}
			// Conflicting bytes under the same event identity are refused.
			conflict := proto.Clone(event).(*api.EventEnvelope)
			conflict.RequestId = event.RequestId + "-conflict"
			if _, err = client.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: shape.owner.String(), Event: conflict}); status.Code(err) != codes.AlreadyExists {
				t.Fatalf("%s conflicting bytes = %v, want AlreadyExists", shape.name, err)
			}
			// An independent store reader (a restarted process over the same database)
			// reads back the identical immutable row.
			restarted := ledger.NewPostgresStore(db)
			stored, err := restarted.Receipt(ctx, receiptID)
			if err != nil || stored.ReceiptID != receiptID || stored.Type != shape.name || stored.OrganizationID != "tenant-shape" {
				t.Fatalf("%s restart readback=%+v err=%v", shape.name, stored, err)
			}
			// The stored payload is the exact event bytes Ledger accepted, and the
			// shared secret gate the writer applies to every RecordDomainEvent input
			// is proven for this evidence shape in the ledger package tests.
			var payload string
			if err = db.QueryRowContext(ctx, `SELECT payload_json FROM evidence_receipts WHERE id=$1`, receiptID).Scan(&payload); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(payload, `"domainEvent"`) {
				t.Fatalf("%s stored payload lost the domain event reference: %s", shape.name, payload)
			}
		})
	}
	// A coordination peer outside the event-producer set cannot use the event
	// channel: Workspace may append receipts but never domain events, so its
	// delivery is refused before any store access.
	workspace := domainInboxClient(t, address, owneridentity.Workspace)
	if _, err := workspace.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "workspace", Event: currentProducerShapes()[0].envelope}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("workspace event delivery = %v, want Unauthenticated", err)
	}
	// And a producer cannot deliver another owner's event type.
	build := clients[owneridentity.Build]
	if _, err := build.Deliver(ctx, &api.DeliverEventRequest{AuthenticatedProducer: "build", Event: currentProducerShapes()[4].envelope}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("build claiming serve readiness = %v, want PermissionDenied", err)
	}
}

// deletionCoordinationRequest builds the confirmed Workspace deletion evidence
// the Workspace owner records after Serve and Fabric confirmed the original
// runtime and resources absent. The receipt names the deletion's own accepted
// DELETEWORKSPACE commit, not the original launch order.
func deletionCoordinationRequest() *api.AppendReceiptRequest {
	scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-local"}}}
	workspace, deletion := "workspace-local", "deletion-operation-local"
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspace)}
	commit := &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: deletion, ResourceId: workspace,
		AcceptedInputDigest: "sha256:" + strings.Repeat("c", 64), CommittedVersion: 1,
		AcceptedAt: timestamppb.New(time.Now().Add(-time.Minute)), AuthorizationContextId: "deletion-authorization", ActorId: "actor-local",
		Scope: scope, AcceptedAction: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE, AuthorizationResource: resource,
		ContinuationResources: []*api.AuthorizationResource{resource}}
	return &api.AppendReceiptRequest{Context: &api.CallContext{RequestId: "request-deletion", IdempotencyKey: "append-deletion", ActorId: "actor-local", Scope: scope, AcceptedOperationGrantId: proto.String("grant-deletion")},
		Receipt:        &api.Receipt{Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(deletion), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, EvidenceSummary: "Workspace deletion confirmed: the original application runtime and provider resources are absent."},
		EvidenceDigest: "sha256:" + strings.Repeat("d", 64), OwnerEvidenceReference: deletion, OwnerCommitEvidence: commit}
}

// TestDeletionReceiptPostgresWirePersistence proves the deletion evidence path
// end to end against real Ledger persistence: the confirmed deletion is recorded
// from the Workspace owner's own accepted commit, read back by reference by both
// the refunding owner and the public reader, replays identically, and neither a
// forged commit nor the generic writer can mint it.
func TestDeletionReceiptPostgresWirePersistence(t *testing.T) {
	db, s, owners, client, fabric := coordinatedService(t)
	ctx := context.Background()
	request := deletionCoordinationRequest()
	owners.commit = proto.Clone(request.OwnerCommitEvidence).(*api.OwnerCommitEvidence)

	first, err := client.AppendReceipt(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.GetId() == "" || first.GetKind() != api.ReceiptKindEnum_RECEIPT_KIND_ENUM_PROVIDER_ACTION || first.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || first.GetOutcome() != api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED || first.GetCreatedAt() == nil {
		t.Fatalf("receipt=%v", first)
	}
	if owners.commitReads != 1 {
		t.Fatalf("the Workspace commit was read back %d times", owners.commitReads)
	}
	// Replaying by a new key returns the identical receipt rather than a second one.
	replay := proto.Clone(request).(*api.AppendReceiptRequest)
	replay.Context.RequestId, replay.Context.IdempotencyKey = "request-deletion-retry", "append-deletion-retry"
	second, err := client.AppendReceipt(ctx, replay)
	if err != nil || !proto.Equal(first, second) {
		t.Fatalf("replay differs: %v %v", second, err)
	}
	read := &api.GetReceiptByReferenceRequest{Context: replay.Context, Owner: "workspace", OwnerEvidenceReference: request.OwnerEvidenceReference}
	// The contract reads the deletion through the one ReadReceiptByReference
	// operation. Both the Workspace owner that recorded it and Fabric, which
	// observes the same reference, resolve it to the identical receipt.
	for _, reader := range []api.LedgerCoordinationClient{client, fabric} {
		actual, err := reader.ReadReceiptByReference(ctx, read)
		if err != nil || !proto.Equal(actual, first) {
			t.Fatalf("deletion read=%v %v", actual, err)
		}
	}
	// A restart reads the same persisted evidence.
	restarted := ledger.NewPostgresStore(db)
	stored, err := restarted.ReadDeletionReceipt(ctx, request.OwnerEvidenceReference)
	if err != nil || !proto.Equal(stored.Evidence, first) {
		t.Fatalf("restart read=%v", err)
	}
	// The same reference with different evidence is refused, not replaced.
	conflict := proto.Clone(request).(*api.AppendReceiptRequest)
	conflict.Receipt.EvidenceSummary = "a different deletion"
	conflict.OwnerCommitEvidence.AcceptedInputDigest = "sha256:" + strings.Repeat("e", 64)
	owners.commit = proto.Clone(conflict.OwnerCommitEvidence).(*api.OwnerCommitEvidence)
	if _, err = client.AppendReceipt(ctx, conflict); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflict=%v", err)
	}
	// A commit the Workspace owner never accepted is refused before persistence.
	owners.commit = proto.Clone(request.OwnerCommitEvidence).(*api.OwnerCommitEvidence)
	owners.commit.OperationId = "another-deletion"
	forged := proto.Clone(request).(*api.AppendReceiptRequest)
	forged.Context.IdempotencyKey = "append-deletion-forged"
	if _, err = client.AppendReceipt(ctx, forged); err == nil {
		t.Fatal("a deletion the Workspace owner never accepted was recorded")
	}
	// The deletion type is read only through its typed coordination path: neither
	// the generic receipt reader nor the generic list projects it, and its a
	// launch's funding receipt still resolves to its own type.
	if _, err = s.store.Receipt(ctx, first.GetId()); err != ledger.ErrReceiptNotFound {
		t.Fatalf("generic read=%v", err)
	}
	page, err := s.store.ListReceipts(ctx, ledger.ReceiptQuery{WorkspaceID: "workspace-local"})
	if err != nil || len(page.Receipts) != 0 {
		t.Fatalf("generic list leaked deletion evidence: %v %v", page, err)
	}
	if _, err = s.store.RecordReceipt(ctx, ledger.ReceiptInput{Type: ledger.DeletionReceiptType, Status: "completed", Surface: "cloud", WorkspaceID: "workspace-local", IdempotencyKey: "forged-deletion"}); err != ledger.ErrInvalidReceiptInput {
		t.Fatalf("generic writer=%v", err)
	}
	var rows int
	if err = db.QueryRowContext(ctx, `SELECT count(*) FROM evidence_receipts WHERE receipt_type=$1`, ledger.DeletionReceiptType).Scan(&rows); err != nil || rows != 1 {
		t.Fatalf("deletion rows=%d %v", rows, err)
	}
}
