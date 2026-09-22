package transport

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	v226 "opl-cloud/packages/contracts/go/v226"
	gatewaystore "opl-cloud/services/gateway-integration/internal/gateway/store"
	tenantstore "opl-cloud/services/gateway-integration/internal/tenant/store"
)

// The two fakes stand in for the two owner databases. Each records every call so
// the tests can prove that a request reaches exactly one owner's store.
type fakeTenantOwner struct {
	operation tenantstore.Operation
	err       error
	calls     int
}

func (f *fakeTenantOwner) ReadOperation(context.Context, string) (tenantstore.Operation, error) {
	f.calls++
	return f.operation, f.err
}

func (f *fakeTenantOwner) ReconcileOperation(context.Context, string) (tenantstore.ReconcileResult, error) {
	f.calls++
	return tenantstore.ReconcileResult{Operation: f.operation}, f.err
}

type fakeGatewayOwner struct {
	operation gatewaystore.Operation
	err       error
	calls     int
}

func (f *fakeGatewayOwner) ReadOperation(context.Context, string) (gatewaystore.Operation, error) {
	f.calls++
	return f.operation, f.err
}

func (f *fakeGatewayOwner) ReconcileOperation(context.Context, string) (gatewaystore.ReconcileResult, error) {
	f.calls++
	return gatewaystore.ReconcileResult{Operation: f.operation}, f.err
}

func tenantOperationRecord() tenantstore.Operation {
	return tenantstore.Operation{
		ID:          "tenant-op-1",
		ActorID:     "actor-1",
		Kind:        "suspend_tenant",
		ResourceID:  "tenant-1",
		Status:      "needs_attention",
		Stage:       "membership",
		ErrorCode:   "gateway_unavailable",
		Observation: "unknown",
		RequestID:   "req-1",
		CreatedAt:   time.Date(2026, 9, 22, 10, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 9, 22, 10, 5, 0, 0, time.UTC),
	}
}

func gatewayOperationRecord() gatewaystore.Operation {
	return gatewaystore.Operation{
		ID:          "gateway-op-1",
		ActorID:     "actor-1",
		Kind:        "revoke_key",
		ResourceID:  "key-1",
		Status:      "running",
		Stage:       "key_revocation",
		Observation: "unknown",
		RequestID:   "req-2",
		CreatedAt:   time.Date(2026, 9, 22, 11, 0, 0, 0, time.UTC),
		UpdatedAt:   time.Date(2026, 9, 22, 11, 1, 0, 0, time.UTC),
	}
}

// Reconcile names the owner, so the request must reach that owner's store and no
// other owner's database.
func TestReconcileRoutesEachOwnerToItsOwnStore(t *testing.T) {
	tenant := &fakeTenantOwner{operation: tenantOperationRecord()}
	gateway := &fakeGatewayOwner{operation: gatewayOperationRecord()}
	server := NewServer(tenant, gateway)

	operation, err := server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		OperationId: "tenant-op-1",
	})
	if err != nil {
		t.Fatalf("reconcile tenant operation: %v", err)
	}
	if operation.GetOperationId() != "tenant-op-1" {
		t.Fatalf("operation id = %q", operation.GetOperationId())
	}
	if operation.GetOwner() != v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT {
		t.Fatalf("operation owner = %v", operation.GetOwner())
	}
	if tenant.calls != 1 || gateway.calls != 0 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 1 and 0", tenant.calls, gateway.calls)
	}

	operation, err = server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY,
		OperationId: "gateway-op-1",
	})
	if err != nil {
		t.Fatalf("reconcile gateway operation: %v", err)
	}
	if operation.GetOperationId() != "gateway-op-1" {
		t.Fatalf("operation id = %q", operation.GetOperationId())
	}
	if operation.GetOwner() != v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY {
		t.Fatalf("operation owner = %v", operation.GetOwner())
	}
	if tenant.calls != 1 || gateway.calls != 1 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 1 and 1", tenant.calls, gateway.calls)
	}
}

// The stored text vocabulary is mapped to the typed contract on the way out; an
// unknown external result stays unknown and the error code stays typed.
func TestReconcileMapsTheStoredVocabularyToTheContract(t *testing.T) {
	server := NewServer(
		&fakeTenantOwner{operation: tenantOperationRecord()},
		&fakeGatewayOwner{operation: gatewayOperationRecord()},
	)

	operation, err := server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		OperationId: "tenant-op-1",
	})
	if err != nil {
		t.Fatalf("reconcile tenant operation: %v", err)
	}
	if operation.GetKind() != v226.OperationKindEnum_OPERATION_KIND_ENUM_SUSPEND_TENANT {
		t.Fatalf("kind = %v", operation.GetKind())
	}
	if operation.GetStage() != v226.OperationStageEnum_OPERATION_STAGE_ENUM_MEMBERSHIP {
		t.Fatalf("stage = %v", operation.GetStage())
	}
	if operation.GetStatus() != v226.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION {
		t.Fatalf("status = %v", operation.GetStatus())
	}
	if operation.GetObservationResult() != v226.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN {
		t.Fatalf("observation = %v", operation.GetObservationResult())
	}
	if operation.GetErrorCode() != v226.ErrorCodeEnum_ERROR_CODE_ENUM_GATEWAY_UNAVAILABLE {
		t.Fatalf("error code = %v", operation.GetErrorCode())
	}
	if operation.GetResourceId() != "tenant-1" || operation.GetRequestId() != "req-1" {
		t.Fatalf("resource/request = %q/%q", operation.GetResourceId(), operation.GetRequestId())
	}
}

func TestReconcileRejectsAnOwnerThisUnitDoesNotServe(t *testing.T) {
	tenant := &fakeTenantOwner{operation: tenantOperationRecord()}
	gateway := &fakeGatewayOwner{operation: gatewayOperationRecord()}
	server := NewServer(tenant, gateway)

	for _, owner := range []v226.OperationOwnerEnum{
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_UNSPECIFIED,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_CAPABILITY,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_BUILD,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_RUNTIME_CONTROL,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC,
		v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_RESOURCE_CATALOG,
	} {
		_, err := server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
			Owner:       owner,
			OperationId: "op-1",
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("owner %v error = %v, want InvalidArgument", owner, err)
		}
	}
	if tenant.calls != 0 || gateway.calls != 0 {
		t.Fatalf("rejected owners must not reach a store: tenant %d, gateway %d", tenant.calls, gateway.calls)
	}

	if _, err := server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
		Owner: v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing operation id error = %v, want InvalidArgument", err)
	}
}

func TestReconcileReportsNotFoundFromTheOwnersOwnDatabase(t *testing.T) {
	server := NewServer(
		&fakeTenantOwner{err: tenantstore.ErrOperationNotFound},
		&fakeGatewayOwner{err: gatewaystore.ErrOperationNotFound},
	)

	_, err := server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		OperationId: "missing",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("tenant error = %v, want NotFound", err)
	}
	_, err = server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY,
		OperationId: "missing",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("gateway error = %v, want NotFound", err)
	}
}

// ReadOwnerCommit must answer from the requested owner's committed record and
// must not accept a resource id the owner did not commit.
func TestReadOwnerCommitRoutesAndChecksTheCommittedResource(t *testing.T) {
	tenant := &fakeTenantOwner{operation: tenantOperationRecord()}
	gateway := &fakeGatewayOwner{operation: gatewayOperationRecord()}
	server := NewServer(tenant, gateway)

	evidence, err := server.ReadOwnerCommit(context.Background(), &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_TENANT,
		OperationId: "tenant-op-1",
		ResourceId:  "tenant-1",
	})
	if err != nil {
		t.Fatalf("read tenant commit: %v", err)
	}
	if evidence.GetOwner() != v226.OwnerEnum_OWNER_ENUM_TENANT {
		t.Fatalf("evidence owner = %v", evidence.GetOwner())
	}
	if evidence.GetOperationId() != "tenant-op-1" || evidence.GetResourceId() != "tenant-1" {
		t.Fatalf("evidence identity = %q/%q", evidence.GetOperationId(), evidence.GetResourceId())
	}
	if !evidence.GetAcceptedAt().AsTime().Equal(tenantOperationRecord().CreatedAt) {
		t.Fatalf("accepted at = %v", evidence.GetAcceptedAt().AsTime())
	}
	if tenant.calls != 1 || gateway.calls != 0 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 1 and 0", tenant.calls, gateway.calls)
	}

	if _, err := server.ReadOwnerCommit(context.Background(), &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_TENANT,
		OperationId: "tenant-op-1",
		ResourceId:  "tenant-2",
	}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("mismatched resource error = %v, want InvalidArgument", err)
	}

	evidence, err = server.ReadOwnerCommit(context.Background(), &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_GATEWAY,
		OperationId: "gateway-op-1",
	})
	if err != nil {
		t.Fatalf("read gateway commit: %v", err)
	}
	if evidence.GetOwner() != v226.OwnerEnum_OWNER_ENUM_GATEWAY || evidence.GetResourceId() != "key-1" {
		t.Fatalf("gateway evidence = %v/%q", evidence.GetOwner(), evidence.GetResourceId())
	}
	if gateway.calls != 1 {
		t.Fatalf("gateway calls = %d, want 1", gateway.calls)
	}
}

func TestReadOwnerCommitRejectsAnOwnerThisUnitDoesNotServe(t *testing.T) {
	tenant := &fakeTenantOwner{operation: tenantOperationRecord()}
	gateway := &fakeGatewayOwner{operation: gatewayOperationRecord()}
	server := NewServer(tenant, gateway)

	for _, owner := range []v226.OwnerEnum{
		v226.OwnerEnum_OWNER_ENUM_UNSPECIFIED,
		v226.OwnerEnum_OWNER_ENUM_CAPABILITY,
		v226.OwnerEnum_OWNER_ENUM_BUILD,
		v226.OwnerEnum_OWNER_ENUM_WORKSPACE,
		v226.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL,
		v226.OwnerEnum_OWNER_ENUM_FABRIC,
		v226.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG,
		v226.OwnerEnum_OWNER_ENUM_LEDGER,
	} {
		_, err := server.ReadOwnerCommit(context.Background(), &v226.ReadOwnerCommitRequest{
			Owner:       owner,
			OperationId: "op-1",
		})
		if status.Code(err) != codes.InvalidArgument {
			t.Fatalf("owner %v error = %v, want InvalidArgument", owner, err)
		}
	}
	if tenant.calls != 0 || gateway.calls != 0 {
		t.Fatalf("rejected owners must not reach a store: tenant %d, gateway %d", tenant.calls, gateway.calls)
	}
}

// OwnerOperationRequest carries no owner, so Read resolves the owner from the
// records that actually hold the id and refuses to pick one when both do.
func TestReadResolvesTheOwningStoreFromTheRecords(t *testing.T) {
	notFoundTenant := &fakeTenantOwner{err: tenantstore.ErrOperationNotFound}
	foundGateway := &fakeGatewayOwner{operation: gatewayOperationRecord()}
	if _, err := NewServer(notFoundTenant, foundGateway).Read(context.Background(), &v226.OwnerOperationRequest{
		OperationId: "gateway-op-1",
	}); err != nil {
		t.Fatalf("read gateway operation: %v", err)
	}
	if notFoundTenant.calls != 1 || foundGateway.calls != 1 {
		t.Fatalf("calls = %d/%d, want 1/1", notFoundTenant.calls, foundGateway.calls)
	}

	foundTenant := &fakeTenantOwner{operation: tenantOperationRecord()}
	notFoundGateway := &fakeGatewayOwner{err: gatewaystore.ErrOperationNotFound}
	operation, err := NewServer(foundTenant, notFoundGateway).Read(context.Background(), &v226.OwnerOperationRequest{
		OperationId: "tenant-op-1",
	})
	if err != nil {
		t.Fatalf("read tenant operation: %v", err)
	}
	if operation.GetOwner() != v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT {
		t.Fatalf("operation owner = %v", operation.GetOwner())
	}

	_, err = NewServer(notFoundTenant, notFoundGateway).Read(context.Background(), &v226.OwnerOperationRequest{
		OperationId: "missing",
	})
	if status.Code(err) != codes.NotFound {
		t.Fatalf("unknown operation error = %v, want NotFound", err)
	}

	ambiguousTenant := &fakeTenantOwner{operation: tenantOperationRecord()}
	ambiguousGateway := &fakeGatewayOwner{operation: gatewayOperationRecord()}
	_, err = NewServer(ambiguousTenant, ambiguousGateway).Read(context.Background(), &v226.OwnerOperationRequest{
		OperationId: "op-1",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ambiguous operation error = %v, want InvalidArgument", err)
	}

	if _, err := NewServer(notFoundTenant, notFoundGateway).Read(context.Background(), &v226.OwnerOperationRequest{}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing operation id error = %v, want InvalidArgument", err)
	}
}

func TestReadReportsAnUnavailableOwnerDatabase(t *testing.T) {
	unavailable := errors.New("connection refused")
	server := NewServer(
		&fakeTenantOwner{err: unavailable},
		&fakeGatewayOwner{err: gatewaystore.ErrOperationNotFound},
	)

	_, err := server.Read(context.Background(), &v226.OwnerOperationRequest{OperationId: "op-1"})
	if status.Code(err) != codes.Internal {
		t.Fatalf("unavailable owner error = %v, want Internal", err)
	}
}

// This unit has no accepted inbound event type yet, so Deliver must refuse
// instead of recording or acknowledging an event it cannot apply.
func TestDeliverRefusesToFakeApplication(t *testing.T) {
	server := NewServer(&fakeTenantOwner{}, &fakeGatewayOwner{})

	_, err := server.Deliver(context.Background(), &v226.DeliverEventRequest{
		Event:                 &v226.EventEnvelope{EventId: "evt-1", EventType: "tenant.restored.v1"},
		AuthenticatedProducer: "tenant",
	})
	if status.Code(err) != codes.Unimplemented {
		t.Fatalf("deliver error = %v, want Unimplemented", err)
	}

	_, err = server.Deliver(context.Background(), &v226.DeliverEventRequest{
		AuthenticatedProducer: "tenant",
	})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing event error = %v, want InvalidArgument", err)
	}

	_, err = server.Deliver(context.Background(), &v226.DeliverEventRequest{
		Event: &v226.EventEnvelope{EventId: "evt-1", EventType: "tenant.restored.v1"},
	})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("missing producer error = %v, want Unauthenticated", err)
	}
}
