package eventconsumer

import (
	"context"
	"database/sql"
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
	mu                      sync.Mutex
	quote                   *api.QuoteAcceptance
	commit                  *api.OwnerCommitEvidence
	deny                    bool
	quoteReads, commitReads int
}

func (o *coordinationOwners) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
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
