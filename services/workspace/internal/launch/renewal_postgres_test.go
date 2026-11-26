package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// renewalCatalogClient prices nothing and accepts one renewal offer. It records
// the exact acceptance the Workspace owner asked for, so a test can prove the
// offer was consumed for this workspace and this obligation only.
type renewalCatalogClient struct {
	api.CatalogCoordinationClient
	offer       *api.QuoteAcceptance
	accepted    *api.QuoteAcceptance
	readCalls   int
	acceptCalls int
}

func (c *renewalCatalogClient) ReadQuoteResourcePlan(_ context.Context, r *api.QuoteResourcePlanRequest, _ ...grpc.CallOption) (*api.QuoteAcceptance, error) {
	c.readCalls++
	if c.offer == nil || c.offer.GetQuote().GetId() != r.GetQuoteId() {
		return nil, status.Error(codes.NotFound, "no such renewal offer")
	}
	return proto.Clone(c.offer).(*api.QuoteAcceptance), nil
}

func (c *renewalCatalogClient) AcceptQuote(_ context.Context, r *api.AcceptQuoteRequest, _ ...grpc.CallOption) (*api.QuoteAcceptance, error) {
	c.acceptCalls++
	if c.offer == nil || c.offer.GetQuote().GetId() != r.GetQuoteId() {
		return nil, status.Error(codes.NotFound, "no such renewal offer")
	}
	accepted := proto.Clone(c.offer).(*api.QuoteAcceptance)
	accepted.Quote.Status = api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED
	accepted.WorkspaceId = r.GetWorkspaceId()
	accepted.ObligationId = r.GetObligationId()
	accepted.AcceptanceId = "acceptance-" + r.GetQuoteId()
	accepted.SnapshotDigest = "sha256:renewal-original"
	c.accepted = accepted
	return proto.Clone(accepted).(*api.QuoteAcceptance), nil
}

// renewalIdentityClient authorizes the renewal and issues its own bounded
// grant. It records the grant request, so a test can prove the renewal asked
// for exactly the renewal continuation surface and nothing more.
type renewalIdentityClient struct {
	api.CloudIdentityAuthorizationClient
	grants         []*api.AcceptedOperationGrantRequest
	authorizeCalls int
}

func (i *renewalIdentityClient) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	i.authorizeCalls++
	return &api.AuthorizationDecision{
		Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		ActorId: r.GetActorId(), Scope: r.GetScope(), SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId,
		AudienceOwner: r.GetAudienceOwner(), Action: r.GetAction(), Resource: r.GetResource(),
		AuthorizationContextId: proto.String("authorization-renewal-original"),
		PermissionVersion:      1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}, nil
}

func (i *renewalIdentityClient) IssueAcceptedOperationGrant(_ context.Context, r *api.AcceptedOperationGrantRequest, _ ...grpc.CallOption) (*api.AcceptedOperationGrant, error) {
	i.grants = append(i.grants, r)
	e := r.GetOwnerCommitEvidence()
	return &api.AcceptedOperationGrant{
		Id: "grant-renewal-original", Scope: e.GetScope(), ActorId: e.GetActorId(),
		AcceptedOperationOwner: e.GetOwner(), AcceptedOperationId: e.GetOperationId(),
		AcceptedAction: e.GetAcceptedAction(), ResourceId: e.GetResourceId(),
		AllowedActions: r.GetAllowedActions(), Mode: api.AcceptedGrantMode_ACCEPTED_GRANT_MODE_CONTINUE_ORIGINAL,
		IssuedAt: timestamppb.Now(),
	}, nil
}

// renewalFabricClient answers the resource readback and the renewal command.
// It records the exact renewal command, so a test can prove the provider was
// asked to extend the prepaid deadline of the resources this Workspace owns,
// with the confirmed charge receipt of this renewal.
type renewalFabricClient struct {
	api.FabricCoordinationClient
	readback   *api.ResourceReadback
	renewal    *api.Operation
	readCalls  int
	renewCalls int
	commands   []*api.RenewResourcesCommand
}

func (f *renewalFabricClient) ReadResources(_ context.Context, r *api.ResourceReadbackRequest, _ ...grpc.CallOption) (*api.ResourceReadback, error) {
	f.readCalls++
	if f.readback == nil || f.readback.GetResourceSetId() != r.GetResourceSetId() {
		return nil, status.Error(codes.NotFound, "no such resource set")
	}
	return proto.Clone(f.readback).(*api.ResourceReadback), nil
}

func (f *renewalFabricClient) RenewResources(_ context.Context, r *api.RenewResourcesCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	f.renewCalls++
	f.commands = append(f.commands, r)
	if f.renewal != nil {
		return proto.Clone(f.renewal).(*api.Operation), nil
	}
	return &api.Operation{OperationId: "fabric-renewal-original", ResourceId: r.GetResourceSetId(),
		Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_RESOURCE_RENEW,
		Status:            api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED,
		ObservationResult: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED.Enum()}, nil
}

// renewalWalletAuthority owns the one charge of the renewal obligation. It
// answers strictly by the original idempotency code and counts the charges, so
// a test can prove a replay never charges the customer twice.
type renewalWalletAuthority struct {
	api.GatewayCoordinationClient
	charges    map[string]*api.WalletOperation
	debitCalls int
	readCalls  int
}

func (w *renewalWalletAuthority) Debit(_ context.Context, r *api.WalletDebitCommand, _ ...grpc.CallOption) (*api.WalletOperation, error) {
	w.debitCalls++
	key := r.GetContext().GetIdempotencyKey()
	if key == "" || r.GetObligationId() == "" || r.GetWorkspaceId() == "" || r.GetQuoteAcceptanceId() == "" || r.GetAmountUsdMicros() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "charge is not an original obligation")
	}
	if w.charges == nil {
		w.charges = map[string]*api.WalletOperation{}
	}
	if existing := w.charges[key]; existing != nil {
		return proto.Clone(existing).(*api.WalletOperation), nil
	}
	operation := &api.WalletOperation{Id: "wallet-charge-renewal", WorkspaceId: proto.String(r.GetWorkspaceId()),
		Kind: api.WalletOperationKindEnum_WALLET_OPERATION_KIND_ENUM_CHARGE, AmountUsdMicros: r.GetAmountUsdMicros(),
		Status: api.WalletOperationStatusEnum_WALLET_OPERATION_STATUS_ENUM_CONFIRMED, ReceiptId: proto.String("charge-receipt-renewal"), CreatedAt: timestamppb.Now()}
	w.charges[key] = operation
	return proto.Clone(operation).(*api.WalletOperation), nil
}

func (w *renewalWalletAuthority) ReadWalletAction(_ context.Context, r *api.WalletReadbackRequest, _ ...grpc.CallOption) (*api.WalletOperation, error) {
	w.readCalls++
	existing := w.charges[r.GetOriginalIdempotencyKey()]
	if existing == nil {
		return nil, status.Error(codes.NotFound, "no charge for the original code")
	}
	return proto.Clone(existing).(*api.WalletOperation), nil
}

// renewalFixture is one paid Workspace with a running confirmed paid term, the
// accepted launch that owns its resource set, and the four owners the renewal
// talks to.
type renewalFixture struct {
	service  *Service
	call     *api.CallContext
	ctx      context.Context
	catalog  *renewalCatalogClient
	identity *renewalIdentityClient
	fabric   *renewalFabricClient
	wallet   *renewalWalletAuthority
	// anchor is the confirmed paid-through the renewal continues.
	anchor time.Time
}

// renewalOffer is the priced renewal offer the Catalog handed out: one month
// of the Workspace's own plan pair, bound to this Workspace.
func renewalOffer() *api.QuoteAcceptance {
	now := time.Now().UTC()
	return &api.QuoteAcceptance{
		Quote: &api.Quote{Id: "quote-renewal-original", Purpose: api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_RENEW,
			WorkspaceId: proto.String("workspace-original"), ComputePlanId: "compute-original", StoragePlanId: "storage-original",
			PeriodMonths: 1, TotalUsdMicros: 720000000, PeriodStart: timestamppb.New(now), PeriodEnd: timestamppb.New(now.AddDate(0, 1, 0)),
			PricePolicyVersionId: "workspace-price-v2", RefundPolicyVersionId: "refund-policy-v1", RetentionPolicyVersionId: "retention-policy-v1",
			RefundTerms: "a renewal is not refundable", RetentionTerms: "retained until the workspace is deleted",
			Status: api.QuoteStatusEnum_QUOTE_STATUS_ENUM_OFFERED, ExpiresAt: timestamppb.New(now.Add(time.Hour))},
		ResourcePlan: &api.ResourcePlanSnapshot{ComputePlanId: "compute-original", StoragePlanId: "storage-original",
			ProviderProfileId: "profile-original", ProviderComputeSkuId: "sku-original", ProviderStorageSkuId: "sku-original",
			Vcpus: 4, MemoryMib: 8192, CapacityGib: 100, PrepaidMonths: 1, Provider: "tencent", Region: "ap-guangzhou", BillingMode: "PREPAID_MONTHLY"},
	}
}

// renewalWorkspace seeds one active paid Workspace with the given confirmed
// paid term, with the accepted launch that owns its resource set. The renewal
// therefore has a real subscription, a real confirmed period and a real
// resource set to continue.
//
// The confirmed period is seeded rather than edited afterwards: a confirmed
// period is append-only history, so a test that needs another window seeds
// another Workspace.
func renewalWorkspace(t *testing.T, db *sql.DB, periodStart, periodEnd time.Time) renewalFixture {
	t.Helper()
	ctx := t.Context()
	store, err := ownerstore.New(db, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	const totalMicros = int64(720000000)
	quote := &api.Quote{Id: "quote-original", Status: api.QuoteStatusEnum_QUOTE_STATUS_ENUM_ACCEPTED, Purpose: api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY,
		CapabilityVersionId: proto.String("capability-original"), ComputePlanId: "compute-original", StoragePlanId: "storage-original",
		PeriodMonths: 1, TotalUsdMicros: totalMicros, PeriodStart: timestamppb.New(periodStart), PeriodEnd: timestamppb.New(periodEnd),
		PricePolicyVersionId: "workspace-price-v1", ExpiresAt: timestamppb.New(time.Now().Add(time.Hour))}
	snapshot, err := protojson.Marshal(&api.QuoteAcceptance{Quote: quote, ResourcePlan: &api.ResourcePlanSnapshot{ComputePlanId: "compute-original", StoragePlanId: "storage-original", PrepaidMonths: 1}, WorkspaceId: "workspace-original", ObligationId: "operation-original", AcceptanceId: "acceptance-original", SnapshotDigest: "sha256:original"})
	if err != nil {
		t.Fatal(err)
	}
	launchResult, err := json.Marshal(orderResult{GrantID: "grant-original", ResourceSetID: "resource-set-original"})
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.workspaces(id,tenant_id,name,status,compute_plan_id,storage_plan_id,created_by,version) VALUES('workspace-original','tenant-original','Original workspace','active','compute-original','storage-original','actor-original',3)`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input,result,observation_result,completed_at) VALUES('operation-original','tenant-original','actor-original','create_workspace','workspace-original','succeeded','succeeded','request-original','{}'::jsonb,$1,'confirmed',now())`, launchResult); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.subscriptions(id,workspace_id,accepted_quote_id,accepted_quote_snapshot,billing_subject_ref,current_period_start,current_period_end,last_charge_wallet_operation_id,period_months,provenance,renewal_mode,renewal_settings_version,version,current_price_policy_version_id,current_monthly_usd_micros,billing_anchor_day) VALUES('subscription-original','workspace-original','quote-original',$1,'tenant:tenant-original',$2,$3,'wallet-op-charge-original',1,'quoted','manual',0,0,'workspace-price-v1',$4,$5)`, snapshot, periodStart, periodEnd, totalMicros, periodStart.Day()); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.subscription_periods(id,subscription_id,quote_id,accepted_quote_snapshot,period_start,period_end,billing_key,charge_wallet_operation_id,charge_receipt_id,provenance) VALUES('period-original','subscription-original','quote-original',$1,$2,$3,'billing:operation-original','wallet-op-charge-original','wallet-receipt-original','quoted')`, snapshot, periodStart, periodEnd); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	call, callCtx := deletionCallContext("tenant-original")
	fixture := renewalFixture{
		service:  &Service{Store: store},
		call:     call,
		ctx:      callCtx,
		catalog:  &renewalCatalogClient{offer: renewalOffer()},
		identity: &renewalIdentityClient{},
		fabric: &renewalFabricClient{readback: &api.ResourceReadback{ResourceSetId: "resource-set-original",
			WorkspaceId: "workspace-original", Outcome: api.Observation_OBSERVATION_CONFIRMED}},
		wallet: &renewalWalletAuthority{},
		anchor: periodEnd,
	}
	fixture.service.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	fixture.service.Catalog = fixture.catalog
	fixture.service.Identity = fixture.identity
	fixture.service.Fabric = fixture.fabric
	fixture.service.Gateway = fixture.wallet
	fixture.service.Ledger = &ledgerAuthority{}
	return fixture
}

// runningPaidTerm is the confirmed paid term of a Workspace that is still
// inside its paid month.
func runningPaidTerm() (time.Time, time.Time) {
	start := time.Now().UTC().Add(-100 * time.Hour).Truncate(time.Second)
	return start, start.Add(30 * 24 * time.Hour)
}

// TestRenewWorkspaceChargesOnceAndRecordsTheRenewedPeriod proves the renewal
// charges the customer exactly once, asks the provider to extend the prepaid
// deadline of the resources the Workspace already owns, and records the renewed
// paid period on top of the confirmed paid-through.
func TestRenewWorkspaceChargesOnceAndRecordsTheRenewedPeriod(t *testing.T) {
	db := runtimeDatabase(t)
	start, end := runningPaidTerm()
	fixture := renewalWorkspace(t, db, start, end)
	fixture.call.IdempotencyKey = "idempotency-renewal-1"
	created, err := fixture.service.RenewWorkspace(fixture.ctx, &api.RenewWorkspaceRpcRequest{Context: fixture.call, WorkspaceId: "workspace-original", Body: &api.ApplyQuoteRequest{QuoteId: "quote-renewal-original"}})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RENEW_WORKSPACE || created.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || created.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED {
		t.Fatalf("renewal operation kind=%v status=%v observation=%v", created.GetKind(), created.GetStatus(), created.GetObservationResult())
	}
	if fixture.wallet.debitCalls != 1 || fixture.catalog.acceptCalls != 1 || fixture.fabric.renewCalls != 1 {
		t.Fatalf("charges=%d acceptances=%d renewals=%d", fixture.wallet.debitCalls, fixture.catalog.acceptCalls, fixture.fabric.renewCalls)
	}
	command := fixture.fabric.commands[0]
	if command.GetWorkspaceId() != "workspace-original" || command.GetResourceSetId() != "resource-set-original" || command.GetObligationId() != created.GetOperationId() ||
		command.GetPrepaidMonths() != 1 || command.GetConfirmedChargeReceiptId() != "charge-receipt-renewal" || command.GetSubscriptionPeriodId() == "" {
		t.Fatalf("renewal command=%v", command)
	}
	// The grant the renewal asked for is its own: the renewal action and the
	// renewal surface only.
	if len(fixture.identity.grants) != 1 {
		t.Fatalf("grants=%d", len(fixture.identity.grants))
	}
	grant := fixture.identity.grants[0]
	if grant.GetOwnerCommitEvidence().GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE {
		t.Fatalf("grant action=%v", grant.GetOwnerCommitEvidence().GetAcceptedAction())
	}
	if len(grant.GetAllowedActions()) != len(renewalContinuationActions) {
		t.Fatalf("grant actions=%v", grant.GetAllowedActions())
	}
	// The renewed period continues the confirmed paid-through by a full month.
	var renewedStart, renewedEnd time.Time
	if err := db.QueryRowContext(fixture.ctx, `SELECT current_period_start,current_period_end FROM workspace.subscriptions WHERE workspace_id='workspace-original'`).Scan(&renewedStart, &renewedEnd); err != nil {
		t.Fatal(err)
	}
	if !renewedStart.UTC().Equal(fixture.anchor.UTC()) || !renewedEnd.UTC().Equal(fixture.anchor.UTC().AddDate(0, 1, 0)) {
		t.Fatalf("renewed period=%v..%v, want %v..%v", renewedStart.UTC(), renewedEnd.UTC(), fixture.anchor.UTC(), fixture.anchor.UTC().AddDate(0, 1, 0))
	}
	// The confirmed period of the renewal is append-only history bound to this
	// renewal's own charge receipt.
	var periodID, quoteID, receiptID string
	if err := db.QueryRowContext(fixture.ctx, `SELECT id,quote_id,charge_receipt_id FROM workspace.subscription_periods WHERE subscription_id='subscription-original' AND billing_key=$1`, "billing:"+created.GetOperationId()).Scan(&periodID, &quoteID, &receiptID); err != nil {
		t.Fatal(err)
	}
	if periodID != command.GetSubscriptionPeriodId() || quoteID != "quote-renewal-original" || receiptID != "charge-receipt-renewal" {
		t.Fatalf("renewed period id=%q quote=%q receipt=%q", periodID, quoteID, receiptID)
	}
	// The Workspace keeps running: a renewal changes what the customer is paid
	// for, not whether the Workspace is served.
	var workspaceStatus string
	if err := db.QueryRowContext(fixture.ctx, `SELECT status FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&workspaceStatus); err != nil {
		t.Fatal(err)
	}
	if workspaceStatus != "active" {
		t.Fatalf("workspace status=%q", workspaceStatus)
	}
}

// TestRenewWorkspaceReplaysWithoutASecondCharge proves the same idempotency
// key replays the original renewal instead of charging the customer twice.
func TestRenewWorkspaceReplaysWithoutASecondCharge(t *testing.T) {
	db := runtimeDatabase(t)
	start, end := runningPaidTerm()
	fixture := renewalWorkspace(t, db, start, end)
	fixture.call.IdempotencyKey = "idempotency-renewal-2"
	request := &api.RenewWorkspaceRpcRequest{Context: fixture.call, WorkspaceId: "workspace-original", Body: &api.ApplyQuoteRequest{QuoteId: "quote-renewal-original"}}
	created, err := fixture.service.RenewWorkspace(fixture.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := fixture.service.RenewWorkspace(fixture.ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if replayed.GetOperationId() != created.GetOperationId() {
		t.Fatalf("replay operation=%q, want %q", replayed.GetOperationId(), created.GetOperationId())
	}
	if fixture.wallet.debitCalls != 1 || fixture.fabric.renewCalls != 1 {
		t.Fatalf("charges=%d renewals=%d", fixture.wallet.debitCalls, fixture.fabric.renewCalls)
	}
	var periods int
	if err := db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM workspace.subscription_periods WHERE subscription_id='subscription-original'`).Scan(&periods); err != nil {
		t.Fatal(err)
	}
	if periods != 2 {
		t.Fatalf("confirmed periods=%d, want the launch period and the renewed one", periods)
	}
}

// TestRenewWorkspaceRefusesAnEndedPaidTermBeforeAnyCharge proves a renewal
// whose paid term has ended is refused before any money moves and before the
// offer is consumed. The provider extends its deadline from its own expiry
// date, so a renewal bought after the term ended would charge for a month the
// provider does not cover.
func TestRenewWorkspaceRefusesAnEndedPaidTermBeforeAnyCharge(t *testing.T) {
	db := runtimeDatabase(t)
	// The confirmed paid term ended an hour ago.
	ended := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	fixture := renewalWorkspace(t, db, ended.Add(-30*24*time.Hour), ended)
	fixture.call.IdempotencyKey = "idempotency-renewal-expired"
	_, err := fixture.service.RenewWorkspace(fixture.ctx, &api.RenewWorkspaceRpcRequest{Context: fixture.call, WorkspaceId: "workspace-original", Body: &api.ApplyQuoteRequest{QuoteId: "quote-renewal-original"}})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ended term renewal err=%v", err)
	}
	if fixture.wallet.debitCalls != 0 || fixture.catalog.acceptCalls != 0 || fixture.fabric.renewCalls != 0 {
		t.Fatalf("charges=%d acceptances=%d renewals=%d", fixture.wallet.debitCalls, fixture.catalog.acceptCalls, fixture.fabric.renewCalls)
	}
	var operations int
	if err := db.QueryRowContext(fixture.ctx, `SELECT count(*) FROM workspace.operations WHERE kind='renew_workspace'`).Scan(&operations); err != nil {
		t.Fatal(err)
	}
	if operations != 0 {
		t.Fatalf("refused renewal recorded %d operations", operations)
	}
}

// TestRenewWorkspaceRefusesResourcesThatAreGoneBeforeAnyCharge proves a
// Workspace whose provider resources are gone is not charged for a renewal it
// cannot deliver.
func TestRenewWorkspaceRefusesResourcesThatAreGoneBeforeAnyCharge(t *testing.T) {
	db := runtimeDatabase(t)
	start, end := runningPaidTerm()
	fixture := renewalWorkspace(t, db, start, end)
	fixture.fabric.readback = &api.ResourceReadback{ResourceSetId: "resource-set-original", WorkspaceId: "workspace-original",
		Outcome: api.Observation_OBSERVATION_CONFIRMED, AbsenceConfirmed: true}
	fixture.call.IdempotencyKey = "idempotency-renewal-gone"
	created, err := fixture.service.RenewWorkspace(fixture.ctx, &api.RenewWorkspaceRpcRequest{Context: fixture.call, WorkspaceId: "workspace-original", Body: &api.ApplyQuoteRequest{QuoteId: "quote-renewal-original"}})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_FAILED || created.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED {
		t.Fatalf("gone resources renewal status=%v observation=%v", created.GetStatus(), created.GetObservationResult())
	}
	if fixture.wallet.debitCalls != 0 || fixture.fabric.renewCalls != 0 {
		t.Fatalf("charges=%d renewals=%d", fixture.wallet.debitCalls, fixture.fabric.renewCalls)
	}
	var code string
	if err := db.QueryRowContext(fixture.ctx, `SELECT error_code FROM workspace.operations WHERE id=$1`, created.GetOperationId()).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != "WORKSPACE_NOT_READY" {
		t.Fatalf("gone resources renewal code=%q", code)
	}
}

// TestRenewWorkspaceWaitsForAnUnansweredProviderRead proves an unanswered
// provider read proves nothing about the resources: the renewal waits and the
// customer is not charged for a renewal the provider has not confirmed.
func TestRenewWorkspaceWaitsForAnUnansweredProviderRead(t *testing.T) {
	db := runtimeDatabase(t)
	start, end := runningPaidTerm()
	fixture := renewalWorkspace(t, db, start, end)
	fixture.fabric.readback = &api.ResourceReadback{ResourceSetId: "resource-set-original", WorkspaceId: "workspace-original", Outcome: api.Observation_OBSERVATION_UNKNOWN}
	fixture.call.IdempotencyKey = "idempotency-renewal-unknown"
	created, err := fixture.service.RenewWorkspace(fixture.ctx, &api.RenewWorkspaceRpcRequest{Context: fixture.call, WorkspaceId: "workspace-original", Body: &api.ApplyQuoteRequest{QuoteId: "quote-renewal-original"}})
	if err != nil {
		t.Fatal(err)
	}
	if created.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION || created.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN {
		t.Fatalf("unanswered read renewal status=%v observation=%v", created.GetStatus(), created.GetObservationResult())
	}
	if fixture.wallet.debitCalls != 0 {
		t.Fatalf("charges=%d", fixture.wallet.debitCalls)
	}
	// A second renewal cannot start while the first one is still running: two
	// live renewals would charge the customer twice for one month.
	second := proto.Clone(fixture.call).(*api.CallContext)
	second.IdempotencyKey = "idempotency-renewal-unknown-2"
	if _, err := fixture.service.RenewWorkspace(fixture.ctx, &api.RenewWorkspaceRpcRequest{Context: second, WorkspaceId: "workspace-original", Body: &api.ApplyQuoteRequest{QuoteId: "quote-renewal-original"}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("concurrent renewal err=%v", err)
	}
}

// TestRenewWorkspaceRefusesASecondRenewalOfTheSamePaidPeriod proves a renewal
// of a paid period the subscription already confirmed is refused before the
// customer is charged for it twice.
func TestRenewWorkspaceRefusesASecondRenewalOfTheSamePaidPeriod(t *testing.T) {
	db := runtimeDatabase(t)
	start, end := runningPaidTerm()
	fixture := renewalWorkspace(t, db, start, end)
	// Another renewal already confirmed the month that starts at this
	// subscription's paid-through, so buying it again would charge the customer
	// twice for one month.
	if _, err := db.ExecContext(fixture.ctx, `INSERT INTO workspace.subscription_periods
		(id, subscription_id, quote_id, accepted_quote_snapshot, period_start, period_end,
		 billing_key, charge_wallet_operation_id, charge_receipt_id, provenance)
		SELECT 'period-repeat', s.id, s.accepted_quote_id, s.accepted_quote_snapshot, s.current_period_end, s.current_period_end + interval '1 month',
		 'billing:operation-repeat', 'wallet-op-charge-repeat', 'wallet-receipt-repeat', 'quoted'
		FROM workspace.subscriptions s WHERE s.workspace_id='workspace-original'`); err != nil {
		t.Fatal(err)
	}
	fixture.call.IdempotencyKey = "idempotency-renewal-3"
	repeated, err := fixture.service.RenewWorkspace(fixture.ctx, &api.RenewWorkspaceRpcRequest{Context: fixture.call, WorkspaceId: "workspace-original", Body: &api.ApplyQuoteRequest{QuoteId: "quote-renewal-original"}})
	if err != nil {
		t.Fatal(err)
	}
	if repeated.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_FAILED {
		t.Fatalf("repeated renewal status=%v", repeated.GetStatus())
	}
	if fixture.wallet.debitCalls != 0 {
		t.Fatalf("charges=%d, want none for a repeated month", fixture.wallet.debitCalls)
	}
	var code string
	if err := db.QueryRowContext(fixture.ctx, `SELECT error_code FROM workspace.operations WHERE id=$1`, repeated.GetOperationId()).Scan(&code); err != nil {
		t.Fatal(err)
	}
	if code != "VERSION_CONFLICT" {
		t.Fatalf("repeated renewal code=%q", code)
	}
}
