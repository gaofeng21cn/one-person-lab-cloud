package launch

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// purchaseAdmissionIdentity reuses the existing action decision fixture while
// issuing only the create grant bound to this test's actual committed order.
// deletionIdentity's own grant deliberately refuses a create obligation.
type purchaseAdmissionIdentity struct {
	deletionIdentity
	authorizations atomic.Int64
	grants         atomic.Int64
}

func (i *purchaseAdmissionIdentity) AuthorizeAction(ctx context.Context, r *api.AuthorizationRequest, options ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	i.authorizations.Add(1)
	return i.deletionIdentity.AuthorizeAction(ctx, r, options...)
}

func (i *purchaseAdmissionIdentity) IssueAcceptedOperationGrant(_ context.Context, r *api.AcceptedOperationGrantRequest, _ ...grpc.CallOption) (*api.AcceptedOperationGrant, error) {
	commit := r.GetOwnerCommitEvidence()
	if r.GetAuthorizationContextId() != "authorization-deletion" || commit.GetOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || commit.GetAcceptedAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE || commit.GetOperationId() == "" || commit.GetResourceId() == "" || commit.GetActorId() != "actor-purchase" || commit.GetScope().GetTenant().GetTenantId() != "tenant-purchase" {
		return nil, status.Error(codes.PermissionDenied, "grant does not identify this accepted Workspace purchase")
	}
	i.grants.Add(1)
	return &api.AcceptedOperationGrant{
		Id: "grant-purchase-" + commit.GetOperationId(), AcceptedOperationOwner: commit.GetOwner(), AcceptedOperationId: commit.GetOperationId(),
		AcceptedAction: commit.GetAcceptedAction(), ResourceId: commit.GetResourceId(), ActorId: commit.GetActorId(), Scope: commit.GetScope(),
		AllowedActions: append([]api.AuthorizationActionEnum(nil), r.GetAllowedActions()...),
	}, nil
}

// purchaseAdmissionCatalog returns a real typed, offered deploy quote through
// ReadQuoteResourcePlan. Acceptance remains unavailable: these tests stop at the
// durable order, without simulating any wallet charge or Provider completion.
type purchaseAdmissionCatalog struct {
	api.CatalogCoordinationClient
	offer                                *api.QuoteAcceptance
	reads                                atomic.Int64
	accepts                              atomic.Int64
	acceptedWorkspace, acceptedOperation string
}

func (c *purchaseAdmissionCatalog) ReadQuoteResourcePlan(_ context.Context, r *api.QuoteResourcePlanRequest, _ ...grpc.CallOption) (*api.QuoteAcceptance, error) {
	c.reads.Add(1)
	call := r.GetContext()
	if r.GetQuoteId() != c.offer.GetQuote().GetId() || call.GetActorId() != "actor-purchase" || call.GetSessionId() != purchaseEligibilityCall().GetSessionId() || call.GetScope().GetTenant().GetTenantId() != "tenant-purchase" || call.GetAuthorizationContextId() != "" {
		return nil, status.Error(codes.PermissionDenied, "quote read does not identify the live purchase")
	}
	return proto.Clone(c.offer).(*api.QuoteAcceptance), nil
}

func (c *purchaseAdmissionCatalog) AcceptQuote(_ context.Context, r *api.AcceptQuoteRequest, _ ...grpc.CallOption) (*api.QuoteAcceptance, error) {
	if r.GetQuoteId() != c.offer.GetQuote().GetId() || r.GetWorkspaceId() == "" || r.GetObligationId() == "" || r.GetContext().GetAcceptedOperationGrantId() != "grant-purchase-"+r.GetObligationId() {
		return nil, status.Error(codes.PermissionDenied, "quote acceptance is not bound to the original purchase")
	}
	c.accepts.Add(1)
	c.acceptedWorkspace, c.acceptedOperation = r.GetWorkspaceId(), r.GetObligationId()
	return nil, status.Error(codes.Unavailable, "Catalog acceptance intentionally unavailable in purchase admission test")
}

func purchaseAdmissionService(t *testing.T, db *sql.DB) (*Service, *purchaseAdmissionCatalog, *purchaseAdmissionIdentity) {
	t.Helper()
	_, offer, _ := orderFixture()
	offer.Quote.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
	catalog := &purchaseAdmissionCatalog{offer: offer}
	identity := &purchaseAdmissionIdentity{}
	service, err := New(db, ownerservice.NewAuthorizer(owneridentity.Workspace, identity), catalog, &deletionFabricClient{}, identity)
	if err != nil {
		t.Fatal(err)
	}
	return service, catalog, identity
}

func purchaseAdmissionRequest(key string) *api.CreateWorkspaceRpcRequest {
	call := purchaseEligibilityCall()
	call.IdempotencyKey = key
	call.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-purchase"}}}
	return &api.CreateWorkspaceRpcRequest{Context: call, Body: &api.CreateWorkspaceRequest{Name: "Purchase admission", QuoteId: "quote-original", RenewalMode: api.CreateWorkspaceRequestRenewalModeEnum_CREATE_WORKSPACE_REQUEST_RENEWAL_MODE_ENUM_MANUAL}}
}

func purchaseAdmissionContext(t *testing.T) context.Context {
	t.Helper()
	return ownerservice.WithPeerOwner(t.Context(), owneridentity.ConsoleBFF)
}

// Read all three durable acceptance surfaces from the real owner schema, both
// before and after a refusal; an error response alone cannot prove no new order.
func purchaseAdmissionCounts(t *testing.T, db *sql.DB) [3]int {
	t.Helper()
	var counts [3]int
	err := db.QueryRowContext(t.Context(), `SELECT (SELECT count(*) FROM workspace.workspaces), (SELECT count(*) FROM workspace.operations), (SELECT count(*) FROM workspace.idempotency_records)`).Scan(&counts[0], &counts[1], &counts[2])
	if err != nil {
		t.Fatal(err)
	}
	return counts
}

func assertPurchaseAdmissionCounts(t *testing.T, db *sql.DB, want [3]int) {
	t.Helper()
	if got := purchaseAdmissionCounts(t, db); got != want {
		t.Fatalf("durable workspaces/operations/idempotency=%v want=%v", got, want)
	}
}

func assertAcceptedPurchase(t *testing.T, service *Service, catalog *purchaseAdmissionCatalog, identity *purchaseAdmissionIdentity, operation *api.Operation, key string) {
	t.Helper()
	if operation == nil || operation.GetOperationId() == "" || operation.GetResourceId() == "" {
		t.Fatalf("accepted purchase has no durable identities: %v", operation)
	}
	stored, err := service.Store.ReadOperation(t.Context(), operation.GetOperationId())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Terminal() || stored.Kind != "create_workspace" || stored.ResourceID != operation.GetResourceId() || stored.ActorID != "actor-purchase" || stored.TenantID != "tenant-purchase" || stored.Stage != "quote_binding" || stored.Status != "awaiting_confirmation" || stored.ErrorCode != "DEPENDENCY_UNAVAILABLE" || stored.Observation != "unknown" {
		t.Fatalf("Catalog outage did not retain the original pending purchase: %+v", stored)
	}
	var workspaceID, operationID, name string
	err = service.Store.DB().QueryRowContext(t.Context(), `SELECT w.id,w.active_operation_id,w.name FROM workspace.workspaces w JOIN workspace.idempotency_records i ON i.resource_id=w.id AND i.operation_id=w.active_operation_id WHERE i.idempotency_key=$1 AND i.operation_name='createWorkspace' AND i.tenant_scope='tenant-purchase' AND i.actor_scope='actor-purchase' AND i.response_status=202`, key).Scan(&workspaceID, &operationID, &name)
	if err != nil || workspaceID != operation.GetResourceId() || operationID != operation.GetOperationId() || name != "Purchase admission" {
		t.Fatalf("accepted row linkage: workspace=%q operation=%q name=%q err=%v", workspaceID, operationID, name, err)
	}
	if catalog.accepts.Load() != 1 || identity.grants.Load() != 1 || catalog.acceptedWorkspace != workspaceID || catalog.acceptedOperation != operationID {
		t.Fatal("Resume did not attempt Catalog acceptance for the exact committed purchase")
	}
	if strings.Contains(string(stored.AcceptedInput), purchaseEligibilityCall().GetSessionId()) {
		t.Fatal("accepted input persisted the live session secret")
	}
}

func TestCreateWorkspacePurchaseEligibilityAdmission(t *testing.T) {
	db := paidOrderDatabase(t)
	enabled, disabled := true, false
	fact := func(value *bool) *contracts.WorkspacePurchaseEligibility {
		return &contracts.WorkspacePurchaseEligibility{ActorID: "actor-purchase", AccountID: "account-purchase", WorkspacePurchaseEnabled: value}
	}
	for _, tt := range []struct {
		name     string
		fact     *contracts.WorkspacePurchaseEligibility
		err      error
		noReader bool
		code     codes.Code
	}{
		{name: "true accepts new order", fact: fact(&enabled), code: codes.OK},
		{name: "false refuses new order", fact: fact(&disabled), code: codes.PermissionDenied},
		{name: "nil reader", noReader: true, code: codes.Unavailable},
		{name: "nil fact", code: codes.DataLoss},
		{name: "unknown boolean", fact: fact(nil), code: codes.DataLoss},
		{name: "wrong actor", fact: &contracts.WorkspacePurchaseEligibility{ActorID: "other-actor", AccountID: "account-purchase", WorkspacePurchaseEnabled: &enabled}, code: codes.DataLoss},
		{name: "empty account", fact: &contracts.WorkspacePurchaseEligibility{ActorID: "actor-purchase", WorkspacePurchaseEnabled: &enabled}, code: codes.DataLoss},
		{name: "owner unavailable", err: status.Error(codes.Unavailable, "eligibility owner unavailable"), code: codes.Unavailable},
		{name: "session rejected", err: status.Error(codes.Unauthenticated, "live session rejected"), code: codes.Unauthenticated},
		{name: "account unresolved", err: status.Error(codes.FailedPrecondition, "account unresolved"), code: codes.FailedPrecondition},
	} {
		t.Run(tt.name, func(t *testing.T) {
			service, catalog, identity := purchaseAdmissionService(t, db)
			before := purchaseAdmissionCounts(t, db)
			var reads atomic.Int64
			if !tt.noReader {
				service.PurchaseEligibility = purchaseEligibilityReadFunc(func(_ context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
					reads.Add(1)
					if identity.authorizations.Load() != 2 {
						t.Error("eligibility was read before the final authorization decision")
					}
					if !proto.Equal(call, purchaseAdmissionRequest(tt.name).GetContext()) {
						t.Error("eligibility reader did not receive the live purchase context")
					}
					assertPurchaseAdmissionCounts(t, db, before)
					return tt.fact, tt.err
				})
			}
			operation, err := service.CreateWorkspace(purchaseAdmissionContext(t), purchaseAdmissionRequest(tt.name))
			if status.Code(err) != tt.code {
				t.Fatalf("code=%s want=%s: %v", status.Code(err), tt.code, err)
			}
			if tt.code == codes.OK {
				assertAcceptedPurchase(t, service, catalog, identity, operation, tt.name)
				for j := range before {
					before[j]++
				}
			} else {
				if operation != nil {
					t.Fatal("refusal returned an operation")
				}
				if catalog.accepts.Load() != 0 || identity.grants.Load() != 0 {
					t.Fatal("refused purchase reached accepted-order continuation")
				}
			}
			assertPurchaseAdmissionCounts(t, db, before)
			wantReads := int64(1)
			if tt.noReader {
				wantReads = 0
			}
			if reads.Load() != wantReads || catalog.reads.Load() != 1 || identity.authorizations.Load() != 2 {
				t.Fatalf("eligibility reads=%d want=%d quote reads=%d authorizations=%d", reads.Load(), wantReads, catalog.reads.Load(), identity.authorizations.Load())
			}
		})
	}
}

func TestCreateWorkspacePurchaseEligibilityAcceptedReplay(t *testing.T) {
	db := paidOrderDatabase(t)
	service, catalog, identity := purchaseAdmissionService(t, db)
	var reads atomic.Int64
	answer := allowPurchaseEligibilityForTest
	service.PurchaseEligibility = purchaseEligibilityReadFunc(func(ctx context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
		reads.Add(1)
		return answer(ctx, call)
	})
	ctx := purchaseAdmissionContext(t)
	request := purchaseAdmissionRequest("original-purchase")
	original, err := service.CreateWorkspace(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	assertAcceptedPurchase(t, service, catalog, identity, original, request.Context.IdempotencyKey)
	assertPurchaseAdmissionCounts(t, db, [3]int{1, 1, 1})
	for _, tt := range []struct {
		name string
		code codes.Code
		read purchaseEligibilityReadFunc
	}{
		{name: "revoked", code: codes.PermissionDenied, read: func(ctx context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
			fact, err := allowPurchaseEligibilityForTest(ctx, call)
			*fact.WorkspacePurchaseEnabled = false
			return fact, err
		}},
		{name: "unavailable", code: codes.Unavailable, read: func(context.Context, *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
			return nil, status.Error(codes.Unavailable, "owner unavailable after acceptance")
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			answer = tt.read
			beforeReads, beforeQuoteReads := reads.Load(), catalog.reads.Load()
			replay, err := service.CreateWorkspace(ctx, proto.Clone(request).(*api.CreateWorkspaceRpcRequest))
			if err != nil || !proto.Equal(replay, original) {
				t.Fatalf("accepted same-key replay changed: replay=%v err=%v", replay, err)
			}
			if reads.Load() != beforeReads || catalog.reads.Load() != beforeQuoteReads {
				t.Fatal("accepted replay called the eligibility or quote owner again")
			}
			assertPurchaseAdmissionCounts(t, db, [3]int{1, 1, 1})
			refused, err := service.CreateWorkspace(ctx, purchaseAdmissionRequest("new-purchase-"+tt.name))
			if refused != nil || status.Code(err) != tt.code {
				t.Fatalf("new key after %s: operation=%v err=%v", tt.name, refused, err)
			}
			if reads.Load() != beforeReads+1 {
				t.Fatal("new key did not read current eligibility")
			}
			assertPurchaseAdmissionCounts(t, db, [3]int{1, 1, 1})
			assertAcceptedPurchase(t, service, catalog, identity, original, request.Context.IdempotencyKey)
		})
	}
	service.PurchaseEligibility = nil
	replay, err := service.CreateWorkspace(ctx, request)
	if err != nil || !proto.Equal(replay, original) {
		t.Fatalf("unconfigured owner broke accepted replay: %v", err)
	}
	refused, err := service.CreateWorkspace(ctx, purchaseAdmissionRequest("new-purchase-unconfigured"))
	if refused != nil || status.Code(err) != codes.Unavailable {
		t.Fatalf("unconfigured new purchase: operation=%v err=%v", refused, err)
	}
	assertPurchaseAdmissionCounts(t, db, [3]int{1, 1, 1})

	// A new Service must resume the accepted obligation without consulting the
	// current purchase policy or live authorization. Catalog stays unavailable,
	// so an actual persisted retry proves recovery without funding or resources.
	accepted, err := service.Store.ReadOperation(ctx, original.GetOperationId())
	if err != nil {
		t.Fatal(err)
	}
	for _, policy := range []string{"revoked", "unavailable", "unconfigured"} {
		t.Run("recovery after "+policy, func(t *testing.T) {
			restarted, err := New(db, service.Auth, catalog, service.Fabric, identity)
			if err != nil {
				t.Fatal(err)
			}
			if policy != "unconfigured" {
				restarted.PurchaseEligibility = purchaseEligibilityReadFunc(func(ctx context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
					reads.Add(1)
					if policy == "unavailable" {
						return nil, status.Error(codes.Unavailable, "eligibility owner unavailable during recovery")
					}
					fact, err := allowPurchaseEligibilityForTest(ctx, call)
					*fact.WorkspacePurchaseEnabled = false
					return fact, err
				})
			}
			beforeReads, beforeAuthorizations, beforeAttempts := reads.Load(), identity.authorizations.Load(), catalog.accepts.Load()
			if err := restarted.Resume(ctx, original.GetOperationId()); status.Code(err) != codes.Unavailable {
				t.Fatalf("recovery did not reach the original pending Catalog command: %v", err)
			}
			if reads.Load() != beforeReads || identity.authorizations.Load() != beforeAuthorizations || identity.grants.Load() != 1 || catalog.accepts.Load() != beforeAttempts+1 || catalog.acceptedWorkspace != original.GetResourceId() || catalog.acceptedOperation != original.GetOperationId() {
				t.Fatal("recovery changed the original obligation or consulted new-purchase admission")
			}
			var attempts int64
			var commandID, key string
			if err := db.QueryRowContext(ctx, `SELECT attempt_count,command_id,idempotency_key FROM workspace.saga_steps WHERE operation_id=$1 AND step_key='accept_quote'`, original.GetOperationId()).Scan(&attempts, &commandID, &key); err != nil {
				t.Fatal(err)
			}
			if attempts != beforeAttempts+1 || commandID != original.GetOperationId()+":accept_quote" || key != commandID {
				t.Fatalf("retry lost the original command: attempts=%d command=%q key=%q", attempts, commandID, key)
			}
			stored, err := restarted.Store.ReadOperation(ctx, original.GetOperationId())
			if err != nil || stored.ResourceID != accepted.ResourceID || stored.ActorID != accepted.ActorID || stored.TenantID != accepted.TenantID || string(stored.AcceptedInput) != string(accepted.AcceptedInput) || stored.Status != "awaiting_confirmation" || stored.Stage != "quote_binding" || stored.Observation != "unknown" || stored.ErrorCode != "DEPENDENCY_UNAVAILABLE" {
				t.Fatalf("recovery did not preserve the pending original order: %+v err=%v", stored, err)
			}
			assertPurchaseAdmissionCounts(t, db, [3]int{1, 1, 1})
		})
	}
}

func TestCreateWorkspacePurchaseEligibilityRevokedWhileQueued(t *testing.T) {
	db := paidOrderDatabase(t)
	// The shared fixture limits the pool to two connections. This test needs
	// separate sessions for the lock holder, queued command and lock observer.
	db.SetMaxOpenConns(3)
	service, catalog, identity := purchaseAdmissionService(t, db)
	var enabled atomic.Bool
	enabled.Store(true)
	var reads atomic.Int64
	service.PurchaseEligibility = purchaseEligibilityReadFunc(func(ctx context.Context, call *api.CallContext) (*contracts.WorkspacePurchaseEligibility, error) {
		reads.Add(1)
		fact, err := allowPurchaseEligibilityForTest(ctx, call)
		*fact.WorkspacePurchaseEnabled = enabled.Load()
		return fact, err
	})
	ctx, cancel := context.WithTimeout(purchaseAdmissionContext(t), 10*time.Second)
	defer cancel()
	request := purchaseAdmissionRequest("queued-purchase")
	lock, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Rollback()
	var blockerPID int
	if err = lock.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatal(err)
	}
	key := "tenant-purchase/actor-purchase/createWorkspace/" + request.Context.IdempotencyKey
	if _, err = lock.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, key); err != nil {
		t.Fatal(err)
	}
	type result struct {
		operation *api.Operation
		err       error
	}
	done := make(chan result, 1)
	go func() { operation, err := service.CreateWorkspace(ctx, request); done <- result{operation, err} }()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		// Match this exact blocking backend rather than any advisory wait in the
		// database; this establishes the request reached the same command lock.
		err = db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event='advisory' AND $1=ANY(pg_blocking_pids(pid)))`, blockerPID).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		select {
		case <-ticker.C:
		case r := <-done:
			t.Fatalf("create returned before waiting on held command lock: operation=%v err=%v", r.operation, r.err)
		case <-ctx.Done():
			t.Fatal("create did not wait on the exact owner command lock")
		}
	}
	if reads.Load() != 0 {
		t.Fatal("eligibility was read before the queued command acquired its lock")
	}
	assertPurchaseAdmissionCounts(t, db, [3]int{})
	enabled.Store(false)
	if err = lock.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.operation != nil || status.Code(r.err) != codes.PermissionDenied {
			t.Fatalf("queued purchase accepted revoked eligibility: operation=%v err=%v", r.operation, r.err)
		}
	case <-ctx.Done():
		t.Fatal("queued purchase did not finish after lock release")
	}
	if reads.Load() != 1 || catalog.reads.Load() != 1 || identity.authorizations.Load() != 2 || catalog.accepts.Load() != 0 || identity.grants.Load() != 0 {
		t.Fatalf("queued purchase reads=%d quotes=%d authorizations=%d accepts=%d grants=%d", reads.Load(), catalog.reads.Load(), identity.authorizations.Load(), catalog.accepts.Load(), identity.grants.Load())
	}
	assertPurchaseAdmissionCounts(t, db, [3]int{})
}
