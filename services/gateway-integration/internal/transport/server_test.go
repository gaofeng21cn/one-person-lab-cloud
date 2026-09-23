package transport

import (
	"context"
	"errors"
	"strings"
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
	operation   tenantstore.Operation
	err         error
	evidence    tenantstore.CommittedEvidence
	evidenceErr error
	calls       int
}

func (f *fakeTenantOwner) ReadOperation(context.Context, string) (tenantstore.Operation, error) {
	f.calls++
	return f.operation, f.err
}

func (f *fakeTenantOwner) ReconcileOperation(context.Context, string) (tenantstore.ReconcileResult, error) {
	f.calls++
	return tenantstore.ReconcileResult{Operation: f.operation}, f.err
}

func (f *fakeTenantOwner) ReadCommittedEvidence(context.Context, string, string) (tenantstore.CommittedEvidence, error) {
	f.calls++
	return f.evidence, f.evidenceErr
}

type fakeGatewayOwner struct {
	operation   gatewaystore.Operation
	err         error
	evidence    gatewaystore.CommittedEvidence
	evidenceErr error
	calls       int
}

func (f *fakeGatewayOwner) ReadOperation(context.Context, string) (gatewaystore.Operation, error) {
	f.calls++
	return f.operation, f.err
}

func (f *fakeGatewayOwner) ReconcileOperation(context.Context, string) (gatewaystore.ReconcileResult, error) {
	f.calls++
	return gatewaystore.ReconcileResult{Operation: f.operation}, f.err
}

func (f *fakeGatewayOwner) ReadCommittedEvidence(context.Context, string, string) (gatewaystore.CommittedEvidence, error) {
	f.calls++
	return f.evidence, f.evidenceErr
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

func tenantCommittedEvidence() tenantstore.CommittedEvidence {
	return tenantstore.CommittedEvidence{
		OperationID:         "tenant-op-1",
		ResourceID:          "tenant-1",
		AcceptedInputDigest: "sha256:" + strings.Repeat("a", 64),
		CommittedVersion:    3,
		AcceptedAt:          time.Date(2026, 9, 22, 10, 1, 0, 0, time.UTC),
	}
}

func gatewayCommittedEvidence() gatewaystore.CommittedEvidence {
	return gatewaystore.CommittedEvidence{
		OperationID:         "gateway-op-1",
		ResourceID:          "key-1",
		AcceptedInputDigest: "sha256:" + strings.Repeat("b", 64),
		CommittedVersion:    7,
		AcceptedAt:          time.Date(2026, 9, 22, 11, 2, 0, 0, time.UTC),
	}
}

func codeOf(t *testing.T, err error) codes.Code {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	return status.Code(err)
}

// ALIGN-01: the request names the owner, so an operation id is resolved inside
// that owner and the other owner's records are never scanned.
func TestReadRoutesEachOwnerToItsOwnStoreOnly(t *testing.T) {
	tenant := &fakeTenantOwner{operation: tenantOperationRecord()}
	gateway := &fakeGatewayOwner{operation: gatewayOperationRecord()}
	server := NewServer(tenant, gateway)
	ctx := context.Background()

	operation, err := server.Read(ctx, &v226.OwnerOperationRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		OperationId: "tenant-op-1",
	})
	if err != nil {
		t.Fatalf("read tenant operation: %v", err)
	}
	if operation.GetOwner() != v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT {
		t.Fatalf("operation owner = %v", operation.GetOwner())
	}
	if tenant.calls != 1 || gateway.calls != 0 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 1 and 0", tenant.calls, gateway.calls)
	}

	operation, err = server.Read(ctx, &v226.OwnerOperationRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY,
		OperationId: "gateway-op-1",
	})
	if err != nil {
		t.Fatalf("read gateway operation: %v", err)
	}
	if operation.GetOwner() != v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY {
		t.Fatalf("operation owner = %v", operation.GetOwner())
	}
	if tenant.calls != 1 || gateway.calls != 1 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 1 and 1", tenant.calls, gateway.calls)
	}
}

// The same operation id may legitimately exist in both owners, so this is not an
// ambiguity to resolve: the request names the owner and only that owner is read.
func TestReadResolvesAnIdHeldByBothOwnersByTheRequestedOwner(t *testing.T) {
	tenant := &fakeTenantOwner{operation: tenantOperationRecord()}
	gateway := &fakeGatewayOwner{operation: gatewayOperationRecord()}
	tenant.operation.ID = "op-1"
	gateway.operation.ID = "op-1"

	operation, err := NewServer(tenant, gateway).Read(context.Background(), &v226.OwnerOperationRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		OperationId: "op-1",
	})
	if err != nil {
		t.Fatalf("read tenant operation: %v", err)
	}
	if operation.GetOwner() != v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT {
		t.Fatalf("operation owner = %v", operation.GetOwner())
	}
	if tenant.calls != 1 || gateway.calls != 0 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 1 and 0", tenant.calls, gateway.calls)
	}
}

// The two owners are separate databases. The addressed owner's answer must not
// depend on the other owner's database being reachable.
func TestReadKeepsAPeerOwnersDatabaseFailureOutOfTheAnswer(t *testing.T) {
	unavailable := errors.New("connection refused")
	ctx := context.Background()

	// The tenant database is down; a gateway read still answers.
	tenant := &fakeTenantOwner{err: unavailable}
	gateway := &fakeGatewayOwner{operation: gatewayOperationRecord()}
	operation, err := NewServer(tenant, gateway).Read(ctx, &v226.OwnerOperationRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY,
		OperationId: "gateway-op-1",
	})
	if err != nil {
		t.Fatalf("read the gateway owner while the tenant database is down: %v", err)
	}
	if operation.GetOperationId() != "gateway-op-1" {
		t.Fatalf("operation id = %q", operation.GetOperationId())
	}
	if tenant.calls != 0 || gateway.calls != 1 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 0 and 1", tenant.calls, gateway.calls)
	}

	// The gateway database is down; a tenant read still answers.
	tenant = &fakeTenantOwner{operation: tenantOperationRecord()}
	gateway = &fakeGatewayOwner{err: unavailable}
	operation, err = NewServer(tenant, gateway).Read(ctx, &v226.OwnerOperationRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		OperationId: "tenant-op-1",
	})
	if err != nil {
		t.Fatalf("read the tenant owner while the gateway database is down: %v", err)
	}
	if operation.GetOperationId() != "tenant-op-1" {
		t.Fatalf("operation id = %q", operation.GetOperationId())
	}
	if tenant.calls != 1 || gateway.calls != 0 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 1 and 0", tenant.calls, gateway.calls)
	}

	// The addressed owner's own database being down is still an error: nothing is
	// reported as a successful read.
	_, err = NewServer(&fakeTenantOwner{err: unavailable}, &fakeGatewayOwner{}).Read(ctx, &v226.OwnerOperationRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		OperationId: "tenant-op-1",
	})
	if got := codeOf(t, err); got != codes.Internal {
		t.Fatalf("addressed owner unavailable = %v, want Internal", got)
	}
}

func TestReadRefusesAnOwnerThisUnitDoesNotServe(t *testing.T) {
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
		_, err := server.Read(context.Background(), &v226.OwnerOperationRequest{
			Owner:       owner,
			OperationId: "op-1",
		})
		if got := codeOf(t, err); got != codes.InvalidArgument {
			t.Fatalf("owner %v error = %v, want InvalidArgument", owner, got)
		}
	}
	if tenant.calls != 0 || gateway.calls != 0 {
		t.Fatalf("rejected owners must not reach a store: tenant %d, gateway %d", tenant.calls, gateway.calls)
	}

	// The owner check precedes the id check, so an unserved owner is refused even
	// without an operation id.
	if _, err := server.Read(context.Background(), &v226.OwnerOperationRequest{
		Owner: v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC,
	}); codeOf(t, err) != codes.InvalidArgument {
		t.Fatalf("unserved owner without an id = %v, want InvalidArgument", err)
	}
	if _, err := server.Read(context.Background(), &v226.OwnerOperationRequest{
		Owner: v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
	}); codeOf(t, err) != codes.InvalidArgument {
		t.Fatalf("missing operation id error = %v, want InvalidArgument", err)
	}
}

func TestReadReportsNotFoundFromTheAddressedOwnersDatabase(t *testing.T) {
	server := NewServer(
		&fakeTenantOwner{err: tenantstore.ErrOperationNotFound},
		&fakeGatewayOwner{err: gatewaystore.ErrOperationNotFound},
	)
	_, err := server.Read(context.Background(), &v226.OwnerOperationRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		OperationId: "missing",
	})
	if got := codeOf(t, err); got != codes.NotFound {
		t.Fatalf("tenant error = %v, want NotFound", got)
	}
	_, err = server.Read(context.Background(), &v226.OwnerOperationRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY,
		OperationId: "missing",
	})
	if got := codeOf(t, err); got != codes.NotFound {
		t.Fatalf("gateway error = %v, want NotFound", got)
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
		if got := codeOf(t, err); got != codes.InvalidArgument {
			t.Fatalf("owner %v error = %v, want InvalidArgument", owner, got)
		}
	}
	if tenant.calls != 0 || gateway.calls != 0 {
		t.Fatalf("rejected owners must not reach a store: tenant %d, gateway %d", tenant.calls, gateway.calls)
	}

	if _, err := server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
		Owner: v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
	}); codeOf(t, err) != codes.InvalidArgument {
		t.Fatalf("missing operation id error = %v, want InvalidArgument", err)
	}
}

func TestReconcileReportsNotFoundFromTheAddressedOwnersDatabase(t *testing.T) {
	server := NewServer(
		&fakeTenantOwner{err: tenantstore.ErrOperationNotFound},
		&fakeGatewayOwner{err: gatewaystore.ErrOperationNotFound},
	)

	_, err := server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_TENANT,
		OperationId: "missing",
	})
	if got := codeOf(t, err); got != codes.NotFound {
		t.Fatalf("tenant error = %v, want NotFound", got)
	}
	_, err = server.Reconcile(context.Background(), &v226.ReconcileOperationRpcRequest{
		Owner:       v226.OperationOwnerEnum_OPERATION_OWNER_ENUM_GATEWAY,
		OperationId: "missing",
	})
	if got := codeOf(t, err); got != codes.NotFound {
		t.Fatalf("gateway error = %v, want NotFound", got)
	}
}

// ReadOwnerCommit must answer from the addressed owner's committed record and must
// never return another owner's evidence.
func TestReadOwnerCommitRoutesEachOwnerToItsOwnStore(t *testing.T) {
	tenant := &fakeTenantOwner{evidence: tenantCommittedEvidence()}
	gateway := &fakeGatewayOwner{evidence: gatewayCommittedEvidence()}
	server := NewServer(tenant, gateway)
	ctx := context.Background()

	evidence, err := server.ReadOwnerCommit(ctx, &v226.ReadOwnerCommitRequest{
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
	if evidence.GetAcceptedInputDigest() != tenantCommittedEvidence().AcceptedInputDigest {
		t.Fatalf("accepted input digest = %q", evidence.GetAcceptedInputDigest())
	}
	if evidence.GetCommittedVersion() != tenantCommittedEvidence().CommittedVersion {
		t.Fatalf("committed version = %d", evidence.GetCommittedVersion())
	}
	if !evidence.GetAcceptedAt().AsTime().Equal(tenantCommittedEvidence().AcceptedAt) {
		t.Fatalf("accepted at = %v", evidence.GetAcceptedAt().AsTime())
	}
	if tenant.calls != 1 || gateway.calls != 0 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 1 and 0", tenant.calls, gateway.calls)
	}

	evidence, err = server.ReadOwnerCommit(ctx, &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_GATEWAY,
		OperationId: "gateway-op-1",
	})
	if err != nil {
		t.Fatalf("read gateway commit: %v", err)
	}
	if evidence.GetOwner() != v226.OwnerEnum_OWNER_ENUM_GATEWAY || evidence.GetResourceId() != "key-1" {
		t.Fatalf("gateway evidence = %v/%q", evidence.GetOwner(), evidence.GetResourceId())
	}
	if tenant.calls != 1 || gateway.calls != 1 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 1 and 1", tenant.calls, gateway.calls)
	}
}

func TestReadOwnerCommitRefusesAnOwnerThisUnitDoesNotServe(t *testing.T) {
	tenant := &fakeTenantOwner{evidence: tenantCommittedEvidence()}
	gateway := &fakeGatewayOwner{evidence: gatewayCommittedEvidence()}
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
		if got := codeOf(t, err); got != codes.InvalidArgument {
			t.Fatalf("owner %v error = %v, want InvalidArgument", owner, got)
		}
	}
	if tenant.calls != 0 || gateway.calls != 0 {
		t.Fatalf("rejected owners must not reach a store: tenant %d, gateway %d", tenant.calls, gateway.calls)
	}

	if _, err := server.ReadOwnerCommit(context.Background(), &v226.ReadOwnerCommitRequest{
		Owner: v226.OwnerEnum_OWNER_ENUM_TENANT,
	}); codeOf(t, err) != codes.InvalidArgument {
		t.Fatalf("missing operation id error = %v, want InvalidArgument", err)
	}
}

// Committed evidence is never fabricated: an owner with no committed aggregate
// refuses rather than returning an empty digest or a fabricated version.
func TestReadOwnerCommitRefusesWithoutCommittedEvidence(t *testing.T) {
	tenant := &fakeTenantOwner{evidenceErr: tenantstore.ErrNoCommittedEvidence}
	gateway := &fakeGatewayOwner{evidenceErr: gatewaystore.ErrNoCommittedEvidence}
	server := NewServer(tenant, gateway)

	_, err := server.ReadOwnerCommit(context.Background(), &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_TENANT,
		OperationId: "tenant-op-1",
	})
	if got := codeOf(t, err); got != codes.FailedPrecondition {
		t.Fatalf("tenant without committed evidence = %v, want FailedPrecondition", got)
	}
	_, err = server.ReadOwnerCommit(context.Background(), &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_GATEWAY,
		OperationId: "gateway-op-1",
	})
	if got := codeOf(t, err); got != codes.FailedPrecondition {
		t.Fatalf("gateway without committed evidence = %v, want FailedPrecondition", got)
	}
	if tenant.calls != 1 || gateway.calls != 1 {
		t.Fatalf("tenant calls = %d, gateway calls = %d, want 1 and 1", tenant.calls, gateway.calls)
	}
}

func TestReadOwnerCommitMapsTheStoreRefusals(t *testing.T) {
	ctx := context.Background()

	_, err := NewServer(
		&fakeTenantOwner{evidenceErr: tenantstore.ErrOperationNotFound},
		&fakeGatewayOwner{},
	).ReadOwnerCommit(ctx, &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_TENANT,
		OperationId: "missing",
	})
	if got := codeOf(t, err); got != codes.NotFound {
		t.Fatalf("unknown operation = %v, want NotFound", got)
	}

	_, err = NewServer(
		&fakeTenantOwner{},
		&fakeGatewayOwner{evidenceErr: gatewaystore.ErrInvalidOperationInput},
	).ReadOwnerCommit(ctx, &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_GATEWAY,
		OperationId: "gateway-op-1",
		ResourceId:  "key-2",
	})
	if got := codeOf(t, err); got != codes.InvalidArgument {
		t.Fatalf("resource mismatch = %v, want InvalidArgument", got)
	}

	_, err = NewServer(
		&fakeTenantOwner{evidenceErr: errors.New("connection refused")},
		&fakeGatewayOwner{},
	).ReadOwnerCommit(ctx, &v226.ReadOwnerCommitRequest{
		Owner:       v226.OwnerEnum_OWNER_ENUM_TENANT,
		OperationId: "tenant-op-1",
	})
	if got := codeOf(t, err); got != codes.Internal {
		t.Fatalf("unavailable owner = %v, want Internal", got)
	}
}

// ALIGN-03: delivery names one logical Inbox, so a process carrying two owners
// addresses exactly that owner's Inbox and ACKs it in that owner's own
// transaction. The command that would apply an inbound event belongs to each
// owner's own work package, so the addressed Inbox refuses with UNIMPLEMENTED
// rather than acknowledging an event it did not apply.
func TestDeliverTargetsTheAddressedInbox(t *testing.T) {
	for owner, want := range map[v226.OwnerEnum]string{
		v226.OwnerEnum_OWNER_ENUM_TENANT:  "tenant",
		v226.OwnerEnum_OWNER_ENUM_GATEWAY: "gateway",
	} {
		tenant := &fakeTenantOwner{}
		gateway := &fakeGatewayOwner{}
		_, err := NewServer(tenant, gateway).Deliver(context.Background(), &v226.DeliverEventRequest{
			Event:                 &v226.EventEnvelope{EventId: "evt-1", Owner: want},
			AuthenticatedProducer: want,
			ConsumerOwner:         owner,
		})
		if got := codeOf(t, err); got != codes.Unimplemented {
			t.Fatalf("consumer owner %v error = %v, want Unimplemented", owner, got)
		}
		if !strings.Contains(status.Convert(err).Message(), want) {
			t.Fatalf("consumer owner %v refusal = %q, want it to name the %s inbox",
				owner, status.Convert(err).Message(), want)
		}
		if tenant.calls != 0 || gateway.calls != 0 {
			t.Fatalf("delivery dispatch is not implemented yet, so no store may be touched: tenant %d, gateway %d",
				tenant.calls, gateway.calls)
		}
	}
}

func TestDeliverRefusesAnInboxThisUnitDoesNotServe(t *testing.T) {
	tenant := &fakeTenantOwner{}
	gateway := &fakeGatewayOwner{}
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
		_, err := server.Deliver(context.Background(), &v226.DeliverEventRequest{
			Event:                 &v226.EventEnvelope{EventId: "evt-1", Owner: "workspace"},
			AuthenticatedProducer: "workspace",
			ConsumerOwner:         owner,
		})
		if got := codeOf(t, err); got != codes.InvalidArgument {
			t.Fatalf("consumer owner %v error = %v, want InvalidArgument", owner, got)
		}
	}
	if tenant.calls != 0 || gateway.calls != 0 {
		t.Fatalf("rejected inboxes must not reach a store: tenant %d, gateway %d", tenant.calls, gateway.calls)
	}
}

func TestDeliverRequiresAnAuthenticatedProducerMatchingTheEnvelopeOwner(t *testing.T) {
	server := NewServer(&fakeTenantOwner{}, &fakeGatewayOwner{})
	ctx := context.Background()

	_, err := server.Deliver(ctx, &v226.DeliverEventRequest{
		Event:         &v226.EventEnvelope{EventId: "evt-1", Owner: "workspace"},
		ConsumerOwner: v226.OwnerEnum_OWNER_ENUM_TENANT,
	})
	if got := codeOf(t, err); got != codes.Unauthenticated {
		t.Fatalf("missing producer = %v, want Unauthenticated", got)
	}

	_, err = server.Deliver(ctx, &v226.DeliverEventRequest{
		ConsumerOwner:         v226.OwnerEnum_OWNER_ENUM_TENANT,
		AuthenticatedProducer: "workspace",
	})
	if got := codeOf(t, err); got != codes.InvalidArgument {
		t.Fatalf("missing event = %v, want InvalidArgument", got)
	}

	// The transport peer is the authenticated producer, so an envelope claiming
	// another owner is refused.
	_, err = server.Deliver(ctx, &v226.DeliverEventRequest{
		Event:                 &v226.EventEnvelope{EventId: "evt-1", Owner: "tenant"},
		AuthenticatedProducer: "workspace",
		ConsumerOwner:         v226.OwnerEnum_OWNER_ENUM_GATEWAY,
	})
	if got := codeOf(t, err); got != codes.PermissionDenied {
		t.Fatalf("producer/envelope owner mismatch = %v, want PermissionDenied", got)
	}
}
