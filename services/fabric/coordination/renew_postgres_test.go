package coordination_test

import (
	"context"
	"database/sql"
	"fmt"
	"net"
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

// The renewal surface of ResourceDispatcher is new in this slice. The fixtures
// below cover the purchase, Secret and deletion surfaces of the same interface,
// and their own suites never dispatch a renewal, so the adaptation belongs to
// this slice's own test file rather than to each fixture's suite. Every stub
// refuses instead of inventing a renewal fact.
func (d *localFixture) RenewResource(context.Context, coordination.ResourceRenewalIntent) (coordination.ResourceRenewalResult, error) {
	return coordination.ResourceRenewalResult{}, fmt.Errorf("fixture_provider_renew_unavailable")
}

func (d *managedSecretDispatcher) RenewResource(context.Context, coordination.ResourceRenewalIntent) (coordination.ResourceRenewalResult, error) {
	return coordination.ResourceRenewalResult{}, fmt.Errorf("fixture_provider_renew_unavailable")
}

func (d *tencentFixture) RenewResource(context.Context, coordination.ResourceRenewalIntent) (coordination.ResourceRenewalResult, error) {
	return coordination.ResourceRenewalResult{}, fmt.Errorf("fixture_provider_renew_unavailable")
}

func (d *secretDispatcher) RenewResource(context.Context, coordination.ResourceRenewalIntent) (coordination.ResourceRenewalResult, error) {
	return coordination.ResourceRenewalResult{}, fmt.Errorf("fixture_provider_renew_unavailable")
}

func (d *rebindDispatcher) RenewResource(context.Context, coordination.ResourceRenewalIntent) (coordination.ResourceRenewalResult, error) {
	return coordination.ResourceRenewalResult{}, fmt.Errorf("fixture_provider_renew_unavailable")
}

// renewalLedger answers the confirmed wallet charge of each renewal
// obligation. An obligation the fixture does not hold is not answered, so the
// renewal path refuses the same way it would against the live Ledger.
type renewalLedger struct {
	api.LedgerCoordinationClient
	mu       sync.Mutex
	receipts map[string]*api.Receipt
}

func (l *renewalLedger) ReadReceiptByReference(_ context.Context, r *api.GetReceiptByReferenceRequest, _ ...grpc.CallOption) (*api.Receipt, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	receipt := l.receipts[r.GetOwnerEvidenceReference()]
	if receipt == nil {
		return nil, status.Error(codes.NotFound, "no confirmed wallet charge")
	}
	return proto.Clone(receipt).(*api.Receipt), nil
}

// confirmedCharge is the confirmed wallet charge of one renewal obligation.
func confirmedCharge(suffix string) *api.Receipt {
	return &api.Receipt{Id: "receipt-" + suffix, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String("obligation-" + suffix), Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_WALLET_ACTION, Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED}
}

// renewalFixture is the provider adapter behind the renewal: it confirms the
// same compute and storage with a deadline advanced by the renewed prepaid
// months, and counts the dispatched renewals so a replay can prove that no
// second provider mutation was sent.
type renewalFixture struct {
	mu       sync.Mutex
	calls    int
	fail     bool
	deadline time.Time
}

func (d *renewalFixture) Provider() string { return "tencent-tke" }

func (d *renewalFixture) EnsureResources(context.Context, coordination.ResourceIntent) (*coordination.ResourceResult, error) {
	return nil, fmt.Errorf("resource dispatch not used")
}

func (d *renewalFixture) BindSecret(context.Context, coordination.SecretBindIntent) (coordination.SecretBindResult, error) {
	return coordination.SecretBindResult{}, fmt.Errorf("fixture_provider_bind_unavailable")
}

func (d *renewalFixture) PutManagedSecret(context.Context, coordination.PutManagedSecretIntent) (coordination.ManagedSecretResult, error) {
	return coordination.ManagedSecretResult{}, status.Error(codes.Unimplemented, "fixture Secret write is not used")
}

func (d *renewalFixture) DeleteResource(context.Context, coordination.ResourceDeletionIntent, string) (coordination.ResourceDeletionFact, error) {
	return coordination.ResourceDeletionFact{}, fmt.Errorf("fixture_provider_delete_unavailable")
}

func (d *renewalFixture) RenewResource(_ context.Context, in coordination.ResourceRenewalIntent) (coordination.ResourceRenewalResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.fail {
		return coordination.ResourceRenewalResult{}, fmt.Errorf("fixture provider pending")
	}
	// The provider renews by its own period, exactly like the Tencent provisioner's
	// one-month renewal, whatever prepaid months the caller declared.
	deadline := d.deadline.AddDate(0, 1, 0).Format(time.RFC3339)
	return coordination.ResourceRenewalResult{
		Compute:    fabric.ComputeAllocation{ID: in.ComputeID, AccountID: "acct-renew", WorkspaceID: in.WorkspaceID, Status: "running", Provider: "tencent-tke", ProviderResourceID: "ins-" + in.ComputeID, ProviderRequestID: "req-renew-compute", Deadline: deadline},
		Storage:    fabric.StorageVolume{ID: in.StorageID, AccountID: "acct-renew", WorkspaceID: in.WorkspaceID, Status: "ready", Provider: "tencent-tke", ProviderResourceID: "disk-" + in.StorageID, ProviderRequestID: "req-renew-storage", Deadline: deadline},
		ObservedAt: time.Now().UTC(),
	}, nil
}

func (d *renewalFixture) renewals() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// renewalCommand is the Workspace owner's renewal command for one accepted
// resource set and one renewed subscription period.
func renewalCommand(suffix string) *api.RenewResourcesCommand {
	return &api.RenewResourcesCommand{
		Context:                  &api.CallContext{ActorId: "actor", RequestId: "request-" + suffix, IdempotencyKey: "key-" + suffix, SessionId: proto.String("session"), AuthorizationContextId: "workspace-renewal-decision", Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant"}}}},
		WorkspaceId:              "workspace-" + suffix,
		ResourceSetId:            "rset-" + suffix,
		SubscriptionPeriodId:     "period-" + suffix,
		PrepaidMonths:            1,
		ConfirmedChargeReceiptId: "receipt-" + suffix,
		ObligationId:             "obligation-" + suffix,
	}
}

// seedRenewableResourceSet stores the confirmed resource set a renewal extends:
// one prepaid monthly compute and storage whose provider deadline the renewal
// has to move forward.
func seedRenewableResourceSet(t *testing.T, db *sql.DB, suffix, billingMode string, deadline time.Time) {
	t.Helper()
	setID := "rset-" + suffix
	if _, err := db.ExecContext(context.Background(), `INSERT INTO fabric.resource_sets (id,tenant_id,workspace_id,provider,provider_profile_ref,region,compute_plan_id,storage_plan_id,accepted_quote_id,approved_specification,observation_result,observed_at) VALUES ($1,'tenant','workspace-`+suffix+`','tencent-tke','profile','ap-guangzhou','cp','sp','quote-`+suffix+`','{}'::jsonb,'confirmed',now())`, setID); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"compute", "storage"} {
		id := "res-" + suffix + "-" + kind
		if _, err := db.ExecContext(context.Background(), `INSERT INTO fabric.resources (id,resource_set_id,kind,provider_resource_ref,provider_purchase_key,billing_mode,requested_specification,observed_specification,observation_result,provider_expires_at,observed_at) VALUES ($1,$2,$3,$4,$5,$6,'{}'::jsonb,'{}'::jsonb,'confirmed',$7,now())`, id, setID, kind, "provider-"+suffix+"-"+kind, "purchase:"+id, billingMode, deadline.UTC()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRenewResourcesExtendsThePrepaidProviderPeriodOnlyWithConfirmedFunding(t *testing.T) {
	db := database(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	original := time.Now().UTC().AddDate(0, 0, 15).Truncate(time.Second)
	seedRenewableResourceSet(t, db, "renew", "PREPAID_MONTHLY", original)
	dispatcher := &renewalFixture{deadline: original}
	ledgerOwner := &renewalLedger{receipts: map[string]*api.Receipt{"obligation-renew": confirmedCharge("renew"), "obligation-stuck": confirmedCharge("stuck"), "obligation-shorter": confirmedCharge("shorter")}}
	client := renewalSystem(t, db, dispatcher, ledgerOwner)

	command := renewalCommand("renew")
	t.Run("a confirmed charge extends the provider deadline of the same resources", func(t *testing.T) {
		operation, err := client.RenewResources(ctx, command)
		if err != nil {
			t.Fatal(err)
		}
		if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || operation.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED || operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED || operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_RESOURCE_RENEW {
			t.Fatalf("renewal did not reach a confirmed state: %v", operation)
		}
		if dispatcher.renewals() != 1 {
			t.Fatalf("provider renewals=%d, want 1", dispatcher.renewals())
		}
		var compute, storage time.Time
		if err = db.QueryRowContext(ctx, `SELECT provider_expires_at FROM fabric.resources WHERE id='res-renew-compute'`).Scan(&compute); err != nil {
			t.Fatal(err)
		}
		if err = db.QueryRowContext(ctx, `SELECT provider_expires_at FROM fabric.resources WHERE id='res-renew-storage'`).Scan(&storage); err != nil {
			t.Fatal(err)
		}
		if !compute.After(original.UTC()) || !storage.After(original.UTC()) {
			t.Fatalf("provider deadline did not move forward: compute=%s storage=%s", compute, storage)
		}
		if !compute.Equal(storage) {
			t.Fatalf("compute and storage deadlines diverged: compute=%s storage=%s", compute, storage)
		}
		var observation, receiptRef, evidence string
		if err = db.QueryRowContext(ctx, `SELECT observation_result,authorization_receipt_ref,evidence_ref FROM fabric.resource_actions WHERE action='renew'`).Scan(&observation, &receiptRef, &evidence); err != nil {
			t.Fatal(err)
		}
		if observation != "confirmed" || receiptRef != command.GetConfirmedChargeReceiptId() || evidence == "" {
			t.Fatalf("renewal action lost its confirmed evidence: %s %s %s", observation, receiptRef, evidence)
		}
		var accepted string
		if err = db.QueryRowContext(ctx, `SELECT approved_input->'fundingEvidence'->'receipt'->>'id' FROM fabric.resource_actions WHERE action='renew'`).Scan(&accepted); err != nil {
			t.Fatal(err)
		}
		if accepted != command.GetConfirmedChargeReceiptId() {
			t.Fatalf("funding evidence does not name the confirmed charge: %s", accepted)
		}
	})
	t.Run("the same renewal replays without a second provider mutation", func(t *testing.T) {
		replay := proto.Clone(command).(*api.RenewResourcesCommand)
		replay.Context.RequestId = "request-renew-replay"
		operation, err := client.RenewResources(ctx, replay)
		if err != nil {
			t.Fatal(err)
		}
		if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
			t.Fatalf("replayed renewal lost its confirmed state: %v", operation)
		}
		if dispatcher.renewals() != 1 {
			t.Fatalf("provider renewals=%d, want 1 after the replay", dispatcher.renewals())
		}
	})
	t.Run("a renewal without a confirmed charge stays unknown and never reaches the provider", func(t *testing.T) {
		unfunded := renewalCommand("unfunded")
		seedRenewableResourceSet(t, db, "unfunded", "PREPAID_MONTHLY", original)
		operation, err := client.RenewResources(ctx, unfunded)
		if err != nil {
			t.Fatal(err)
		}
		if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION || operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN || operation.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_PROVIDER_RENEWAL {
			t.Fatalf("unfunded renewal reported a provider effect: %v", operation)
		}
		if dispatcher.renewals() != 1 {
			t.Fatalf("provider renewals=%d, want 1 after the unfunded renewal", dispatcher.renewals())
		}
		var deadline time.Time
		if err = db.QueryRowContext(ctx, `SELECT provider_expires_at FROM fabric.resources WHERE id='res-unfunded-compute'`).Scan(&deadline); err != nil {
			t.Fatal(err)
		}
		if !deadline.Equal(original.UTC()) {
			t.Fatalf("unfunded renewal moved the provider deadline: %s", deadline)
		}
	})
	t.Run("a zero-charge resource set has no provider period to renew", func(t *testing.T) {
		zero := renewalCommand("zero")
		seedRenewableResourceSet(t, db, "zero", "LOCAL_NO_CHARGE", original)
		_, err := client.RenewResources(ctx, zero)
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("zero-charge renewal error=%v, want FailedPrecondition", err)
		}
		if dispatcher.renewals() != 1 {
			t.Fatalf("provider renewals=%d, want 1 after the refused renewal", dispatcher.renewals())
		}
	})
	t.Run("a second renewal of the same subscription period is refused", func(t *testing.T) {
		second := renewalCommand("renew")
		second.Context.IdempotencyKey = "key-renew-second"
		second.Context.RequestId = "request-renew-second"
		_, err := client.RenewResources(ctx, second)
		if status.Code(err) != codes.AlreadyExists {
			t.Fatalf("second renewal error=%v, want AlreadyExists", err)
		}
		if dispatcher.renewals() != 1 {
			t.Fatalf("provider renewals=%d, want 1 after the refused second renewal", dispatcher.renewals())
		}
	})
	t.Run("a provider period shorter than the purchased months is refused", func(t *testing.T) {
		shorter := renewalCommand("shorter")
		shorter.PrepaidMonths = 2
		seedRenewableResourceSet(t, db, "shorter", "PREPAID_MONTHLY", original)
		_, err := client.RenewResources(ctx, shorter)
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("renewal shorter than the purchased months error=%v, want FailedPrecondition", err)
		}
		var deadline time.Time
		if err = db.QueryRowContext(ctx, `SELECT provider_expires_at FROM fabric.resources WHERE id='res-shorter-compute'`).Scan(&deadline); err != nil {
			t.Fatal(err)
		}
		if !deadline.Equal(original.UTC()) {
			t.Fatalf("refused renewal stored a provider deadline: %s", deadline)
		}
	})
	t.Run("a provider deadline that does not move forward is refused and not stored", func(t *testing.T) {
		stuck := renewalCommand("stuck")
		seedRenewableResourceSet(t, db, "stuck", "PREPAID_MONTHLY", original)
		dispatcher.mu.Lock()
		dispatcher.deadline = original.AddDate(0, -1, 0)
		dispatcher.mu.Unlock()
		_, err := client.RenewResources(ctx, stuck)
		if status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("renewal without a forward deadline error=%v, want FailedPrecondition", err)
		}
		var deadline time.Time
		if err = db.QueryRowContext(ctx, `SELECT provider_expires_at FROM fabric.resources WHERE id='res-stuck-compute'`).Scan(&deadline); err != nil {
			t.Fatal(err)
		}
		if !deadline.Equal(original.UTC()) {
			t.Fatalf("refused renewal stored a provider deadline: %s", deadline)
		}
	})
}

// renewalSystem starts the Fabric coordination owner with one renewal
// dispatcher and one Ledger that answers the confirmed wallet charge.
func renewalSystem(t *testing.T, db *sql.DB, dispatcher coordination.ResourceDispatcher, ledgerClient api.LedgerCoordinationClient) api.FabricCoordinationClient {
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
	authority := &identity{}
	service, err := coordination.New(db, ownerservice.NewAuthorizer(ownerservice.OwnerFabric, authority).Authorize, api.NewCatalogCoordinationClient(connection))
	if err != nil {
		t.Fatal(err)
	}
	service.Dispatcher = dispatcher
	service.Ledger = ledgerClient
	token := "isolated-fabric-renewal-token-000000000001"
	config := ownerservice.Config{Owner: ownerservice.OwnerFabric, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.Workspace.Service(): token}}
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
	opts, err := config.TLS.DialOptions(owneridentity.Workspace.Service(), owneridentity.Fabric.Service(), token)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(fabricListener.Addr().String(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return api.NewFabricCoordinationClient(conn)
}
