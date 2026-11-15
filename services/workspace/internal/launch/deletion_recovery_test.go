package launch

import (
	"context"
	"database/sql"
	"sync"
	"time"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// lateConfirmationDeletionFabric is an owner whose provider finishes the resource
// release only after the Workspace has observed it for more passes than the
// recovery limit: it keeps reporting the original resources as present until its
// own delete count passes that limit, and only then confirms the absence.
//
// Every call must carry the same frozen command, exactly like the real Fabric
// owner, so a replayed command is the only command this owner accepts.
type lateConfirmationDeletionFabric struct {
	*deletionFabricClient
	first *api.MutateResourcesCommand
}

func (f *lateConfirmationDeletionFabric) DeleteResources(ctx context.Context, r *api.MutateResourcesCommand, options ...grpc.CallOption) (*api.Operation, error) {
	if f.first == nil {
		f.first = proto.Clone(r).(*api.MutateResourcesCommand)
	} else if !proto.Equal(f.first, r) {
		return nil, status.Error(codes.DataLoss, "retry changed frozen delete request")
	}
	out, err := f.deletionFabricClient.DeleteResources(ctx, r, options...)
	if f.deleteCalls > deletionRecoveryLimit {
		f.present = false
	}
	return out, err
}

// deletionWorker builds one Workspace worker over the persisted owner rows. A
// worker keeps no continuation in memory, so a second worker over the same
// database is a restarted process, not a resumed one.
func deletionWorker(t *testing.T, db *sql.DB, identity api.CloudIdentityAuthorizationClient, fabric api.FabricCoordinationClient, serve api.ServeAgentCoordinationClient, ledger api.LedgerCoordinationClient, gateway api.GatewayCoordinationClient) *Service {
	t.Helper()
	store, err := ownerstore.New(db, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	worker := &Service{Store: store, Identity: identity, Ledger: ledger, Fabric: fabric, Serve: serve, Gateway: gateway}
	worker.Auth = ownerservice.NewAuthorizer(owneridentity.Workspace, &modelReadIdentity{})
	return worker
}

// advanceSchedulingGate releases the persisted scheduling gate of one operation so
// the automatic worker may observe it again immediately. It touches the schedule
// only: the operation's own state is never written by a test.
func advanceSchedulingGate(t *testing.T, db *sql.DB, operationID string) {
	t.Helper()
	if _, err := db.ExecContext(t.Context(), `UPDATE workspace.saga_steps SET next_attempt_at=NULL WHERE operation_id=$1`, operationID); err != nil {
		t.Fatal(err)
	}
}

// TestDeletionConsumesALateOwnerConfirmationAfterTheRecoveryLimit proves that a
// deletion whose owner answers late is still continued by the automatic worker.
//
// The owner here confirms the resource absence only after the Workspace observed
// it for more passes than the recovery limit, so the deletion must not stop on
// the number of elapsed passes. The worker keeps observing the same owner under
// the same accepted operation, consumes the late confirmation and completes the
// original deletion. A direct Resume, a second DELETE or a test-side state reset
// may not stand in for that automatic recovery.
func TestDeletionConsumesALateOwnerConfirmationAfterTheRecoveryLimit(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	fabric := &lateConfirmationDeletionFabric{deletionFabricClient: &deletionFabricClient{present: true}}
	service.Fabric = fabric
	call.IdempotencyKey = "late-confirmation-delete"
	accepted, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	operationID := accepted.GetOperationId()
	original, err := service.Store.ReadOperation(ctx, operationID)
	if err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 3*deletionRecoveryLimit; pass++ {
		advanceSchedulingGate(t, db, operationID)
		if err = service.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		stored, err := service.Store.ReadOperation(ctx, operationID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status == "succeeded" {
			break
		}
	}
	completed, err := service.Store.ReadOperation(ctx, operationID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "succeeded" {
		t.Fatalf("the automatic worker stopped observing a pending deletion: status=%s error=%s", completed.Status, completed.ErrorCode)
	}
	// The late confirmation completed the original operation, not a new one.
	if completed.ID != operationID || string(completed.AcceptedInput) != string(original.AcceptedInput) {
		t.Fatal("the late confirmation did not complete the original accepted deletion")
	}
	// One irreversible effect each: the runtime is retired once, the deletion is
	// recorded once and the original charge is refunded once.
	if stops := service.Serve.(*deletionServeClient).stops; stops != 1 {
		t.Fatalf("runtime retirements=%d, want the one original retirement", stops)
	}
	if appends := service.Ledger.(*deletionLedgerClient).appends; appends != 1 {
		t.Fatalf("deletion receipts=%d, want the one confirmed deletion receipt", appends)
	}
	if refunds := service.Gateway.(*deletionGatewayClient).refunds; refunds != 1 {
		t.Fatalf("refunds=%d, want the one original settlement", refunds)
	}
	if fabric.deleteCalls < 2 {
		t.Fatalf("fabric delete calls=%d, want the frozen command replayed while the owner was pending", fabric.deleteCalls)
	}
	// The completion carries every owner fact the deletion depends on.
	_, result, err := decodeDeletion(completed)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.RetirementOperation) == 0 || len(result.ResourceReadback) == 0 || len(result.DeletionReceipt) == 0 {
		t.Fatalf("completed deletion is missing an owner fact: %+v", result)
	}
	if result.RefundStatus != "confirmed" {
		t.Fatalf("refund status=%q, want the wallet-confirmed settlement", result.RefundStatus)
	}
	if result.KeyCloseoutOutcome != "confirmed" && result.KeyCloseoutOutcome != "not_applicable" {
		t.Fatalf("key closeout=%q, want a confirmed or explicitly inapplicable closeout", result.KeyCloseoutOutcome)
	}
	if result.PendingStep != "" {
		t.Fatalf("pending step=%q, want no pending step on a completed deletion", result.PendingStep)
	}
}

// TestDeletionContinuesAfterTheWorkerRestarts proves that the automatic recovery
// survives a process restart. The Workspace keeps no continuation in memory, so a
// fresh worker over the same persisted owner rows must continue the same
// deletion and consume the owner's late confirmation.
func TestDeletionContinuesAfterTheWorkerRestarts(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	fabric := &lateConfirmationDeletionFabric{deletionFabricClient: &deletionFabricClient{present: true}}
	service.Fabric = fabric
	call.IdempotencyKey = "restart-late-confirmation-delete"
	accepted, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	operationID := accepted.GetOperationId()
	// The first process observes the pending owner across the recovery limit.
	for pass := 0; pass < deletionRecoveryLimit; pass++ {
		advanceSchedulingGate(t, db, operationID)
		if err = service.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// The worker restarts: a new process over the same persisted owner rows,
	// carrying no state of the previous one.
	restarted := deletionWorker(t, db, &deletionIdentity{}, fabric, &deletionServeClient{}, &deletionLedgerClient{}, &deletionGatewayClient{})
	for pass := 0; pass < 3*deletionRecoveryLimit; pass++ {
		advanceSchedulingGate(t, db, operationID)
		if err = restarted.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		stored, err := restarted.Store.ReadOperation(ctx, operationID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status == "succeeded" {
			break
		}
	}
	completed, err := restarted.Store.ReadOperation(ctx, operationID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "succeeded" {
		t.Fatalf("a restarted worker did not continue the pending deletion: status=%s error=%s", completed.Status, completed.ErrorCode)
	}
	if refunds := restarted.Gateway.(*deletionGatewayClient).refunds; refunds != 1 {
		t.Fatalf("refunds=%d, want the one original settlement", refunds)
	}
	// The retirement was already recorded before the restart, so the restarted
	// worker read it back instead of retiring the application a second time.
	if stops := service.Serve.(*deletionServeClient).stops; stops != 1 {
		t.Fatalf("runtime retirements=%d, want the one original retirement", stops)
	}
}

// TestDeletionRecoversAnOperationThatAlreadyParked covers the installations that
// already hold a deletion parked by the former recovery limit: the operation row
// carries the observation-only shape that limit produced, and the repaired worker
// must still observe its owner and consume the owner's late confirmation.
//
// The row is seeded to that shape on purpose; the recovery itself is still done
// by the automatic worker, without a direct Resume, another DELETE or a
// test-side reset of the operation's own progress.
func TestDeletionRecoversAnOperationThatAlreadyParked(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	fabric := &lateConfirmationDeletionFabric{deletionFabricClient: &deletionFabricClient{present: true}}
	service.Fabric = fabric
	call.IdempotencyKey = "already-parked-delete"
	accepted, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	operationID := accepted.GetOperationId()
	// Observe the pending owner until the deletion has waited past the limit.
	for pass := 0; pass < deletionRecoveryLimit; pass++ {
		advanceSchedulingGate(t, db, operationID)
		if err = service.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	// The parked shape an installation holds after the former recovery limit.
	if _, err = db.ExecContext(ctx, `UPDATE workspace.operations SET status='needs_attention',observation_result='unknown',error_code='DEPENDENCY_UNAVAILABLE' WHERE id=$1`, operationID); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 3*deletionRecoveryLimit; pass++ {
		advanceSchedulingGate(t, db, operationID)
		if err = service.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		stored, err := service.Store.ReadOperation(ctx, operationID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status == "succeeded" {
			break
		}
	}
	completed, err := service.Store.ReadOperation(ctx, operationID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "succeeded" {
		t.Fatalf("a parked deletion was not recovered by the automatic worker: status=%s error=%s", completed.Status, completed.ErrorCode)
	}
	if refunds := service.Gateway.(*deletionGatewayClient).refunds; refunds != 1 {
		t.Fatalf("refunds=%d, want the one original settlement", refunds)
	}
}

// lostAnswerDeletionServe is an owner that records the retirement and then loses
// the answer on the way back to Workspace. It keeps the one-send fence a real
// owner keeps: a repeated command under the same idempotency key replays the
// recorded operation instead of retiring a second runtime.
//
// Two workers observe the same owner at the same time, so the owner records
// under its own lock.
type lostAnswerDeletionServe struct {
	api.ServeAgentCoordinationClient
	mu       sync.Mutex
	requests int
	effects  int
	keys     []string
	recorded *api.Operation
}

func (s *lostAnswerDeletionServe) Retire(_ context.Context, r *api.RuntimeStopCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests++
	s.keys = append(s.keys, r.GetContext().GetIdempotencyKey())
	if s.recorded == nil {
		if r.GetRuntimeInstanceId() != "runtime-original" || r.GetDeploymentId() != "deployment-original" {
			return nil, status.Error(codes.NotFound, "different runtime")
		}
		s.effects++
		s.recorded = &api.Operation{OperationId: "serve-retire-original", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_RETIRE, ResourceId: r.GetRuntimeInstanceId(), Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_RETIREMENT, ObservationResult: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED.Enum()}
	}
	if s.requests == 1 {
		return nil, status.Error(codes.Unavailable, "the retirement answer was lost")
	}
	return proto.Clone(s.recorded).(*api.Operation), nil
}

// concurrentDeletionFabric is an owner that confirms the resource absence only
// after it has been observed for more passes than the recovery limit, and that
// accepts only the one frozen delete command. Two workers observe it at the same
// time, so it records under its own lock.
type concurrentDeletionFabric struct {
	api.FabricCoordinationClient
	mu       sync.Mutex
	requests int
	first    *api.MutateResourcesCommand
}

func (f *concurrentDeletionFabric) DeleteResources(_ context.Context, r *api.MutateResourcesCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if f.first == nil {
		f.first = proto.Clone(r).(*api.MutateResourcesCommand)
	} else if !proto.Equal(f.first, r) {
		return nil, status.Error(codes.DataLoss, "retry changed frozen delete request")
	}
	if r.GetWorkspaceId() != "workspace-original" || r.GetResourceSetId() != "resource-set-original" {
		return nil, status.Error(codes.InvalidArgument, "different original resources")
	}
	return &api.Operation{OperationId: "fabric-delete-original", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_FABRIC, ResourceId: r.GetResourceSetId(), Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_RUNNING, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_RETIREMENT}, nil
}

func (f *concurrentDeletionFabric) ReadResources(_ context.Context, r *api.ResourceReadbackRequest, _ ...grpc.CallOption) (*api.ResourceReadback, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.GetResourceSetId() != "resource-set-original" {
		return nil, status.Error(codes.NotFound, "different resources")
	}
	if f.requests <= deletionRecoveryLimit {
		return &api.ResourceReadback{ResourceSetId: r.GetResourceSetId(), WorkspaceId: "workspace-original", ResourceVersion: "version-current", Outcome: api.Observation_OBSERVATION_CONFIRMED, Resources: []*api.ResourceFact{{Id: "compute-original", Kind: contracts.WorkspaceDeleteResourceMachine, State: "active"}}, ObservedAt: timestamppb.Now()}, nil
	}
	return &api.ResourceReadback{ResourceSetId: r.GetResourceSetId(), WorkspaceId: "workspace-original", ResourceVersion: "version-current", Outcome: api.Observation_OBSERVATION_CONFIRMED, AbsenceConfirmed: true, ObservedAt: timestamppb.Now()}, nil
}

// TestConcurrentDeletionWorkerPassesKeepOneOperationAndOneEffect proves that
// workers observing the same pending deletion at the same time keep one
// operation, one retirement, one receipt and one settlement, even when an owner's
// answer is lost on the way back.
func TestConcurrentDeletionWorkerPassesKeepOneOperationAndOneEffect(t *testing.T) {
	db := runtimeDatabase(t)
	service, call, ctx, _, _ := deletionWorkspace(t, db)
	serve := &lostAnswerDeletionServe{}
	fabric := &concurrentDeletionFabric{}
	service.Serve = serve
	service.Fabric = fabric
	call.IdempotencyKey = "concurrent-worker-delete"
	accepted, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
	if err != nil {
		t.Fatal(err)
	}
	operationID := accepted.GetOperationId()
	second := deletionWorker(t, db, &deletionIdentity{}, fabric, serve, &deletionLedgerClient{}, &deletionGatewayClient{})
	for round := 0; round < 4*deletionRecoveryLimit; round++ {
		advanceSchedulingGate(t, db, operationID)
		var group sync.WaitGroup
		for _, worker := range []*Service{service, second} {
			group.Add(1)
			go func(w *Service) {
				defer group.Done()
				_ = w.RunOnce(ctx)
			}(worker)
		}
		group.Wait()
		stored, err := service.Store.ReadOperation(ctx, operationID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status == "succeeded" {
			break
		}
	}
	completed, err := service.Store.ReadOperation(ctx, operationID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != "succeeded" {
		t.Fatalf("the concurrent workers did not converge on one completed deletion: status=%s error=%s", completed.Status, completed.ErrorCode)
	}
	if serve.effects != 1 {
		t.Fatalf("runtime retirement effects=%d, want one", serve.effects)
	}
	for _, key := range serve.keys {
		if key != operationID+":retire_runtime" {
			t.Fatalf("retirement command key=%q, want the one original command key", key)
		}
	}
	refunds := service.Gateway.(*deletionGatewayClient).refunds + second.Gateway.(*deletionGatewayClient).refunds
	if refunds != 1 {
		t.Fatalf("refunds=%d, want the one original settlement", refunds)
	}
	appends := service.Ledger.(*deletionLedgerClient).appends + second.Ledger.(*deletionLedgerClient).appends
	if appends != 1 {
		t.Fatalf("deletion receipts=%d, want the one confirmed deletion receipt", appends)
	}
}

// revokedGrantIdentity accepts the deletion and then refuses to issue the
// deletion's own accepted-operation grant, exactly like an authorization that was
// invalidated between the acceptance and the cleanup.
type revokedGrantIdentity struct {
	api.CloudIdentityAuthorizationClient
	grants int
}

func (i *revokedGrantIdentity) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	return &api.AuthorizationDecision{
		Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		ActorId: r.GetActorId(), Scope: r.GetScope(), SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId,
		AudienceOwner: r.GetAudienceOwner(), Action: r.GetAction(), Resource: r.GetResource(),
		AuthorizationContextId: proto.String("authorization-deletion"),
		PermissionVersion:      1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}, nil
}

func (i *revokedGrantIdentity) IssueAcceptedOperationGrant(context.Context, *api.AcceptedOperationGrantRequest, ...grpc.CallOption) (*api.AcceptedOperationGrant, error) {
	i.grants++
	return nil, status.Error(codes.PermissionDenied, "the deletion's grant was revoked")
}

// TestDeletionKeepsAnExplicitReasonWhenItsAuthorizationOrEvidenceIsInvalid
// proves that a deletion whose owner refuses it keeps an explicit reason and
// stops, instead of being retried forever or reported as done.
//
// An invalidated authorization and a conflicting owner answer are different
// failures with different reasons, and neither of them may be turned into an
// automatic retry of an irreversible action.
func TestDeletionKeepsAnExplicitReasonWhenItsAuthorizationOrEvidenceIsInvalid(t *testing.T) {
	t.Run("authorization_invalid", func(t *testing.T) {
		db := runtimeDatabase(t)
		service, call, ctx, _, _ := deletionWorkspace(t, db)
		identity := &revokedGrantIdentity{}
		service.Identity = identity
		call.IdempotencyKey = "invalid-authorization-delete"
		accepted, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
		if err != nil {
			t.Fatal(err)
		}
		operationID := accepted.GetOperationId()
		for pass := 0; pass < 3; pass++ {
			advanceSchedulingGate(t, db, operationID)
			_ = service.RunOnce(ctx)
		}
		stored, err := service.Store.ReadOperation(ctx, operationID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != "needs_attention" || stored.ErrorCode != "FORBIDDEN" {
			t.Fatalf("deletion status=%s error=%s, want an explicit refusal for the invalidated authorization", stored.Status, stored.ErrorCode)
		}
		if stored.Observation != "rejected" {
			t.Fatalf("deletion observation=%s, want the refusal to be recorded as rejected", stored.Observation)
		}
		if identity.grants != 1 {
			t.Fatalf("grant requests=%d, want the one request that was refused", identity.grants)
		}
		if refunds := service.Gateway.(*deletionGatewayClient).refunds; refunds != 0 {
			t.Fatalf("refunds=%d, want no settlement without an authorized deletion", refunds)
		}
	})
	t.Run("evidence_conflict", func(t *testing.T) {
		db := runtimeDatabase(t)
		service, call, ctx, _, _ := deletionWorkspace(t, db)
		service.Serve = &conflictingDeletionServe{}
		call.IdempotencyKey = "conflicting-evidence-delete"
		accepted, err := service.DeleteWorkspace(ctx, &api.DeleteWorkspaceRpcRequest{Context: call, WorkspaceId: "workspace-original", Body: &api.DeleteWorkspaceRequest{ConfirmationName: "Original workspace", AcknowledgeDataDestruction: true}})
		if err != nil {
			t.Fatal(err)
		}
		operationID := accepted.GetOperationId()
		for pass := 0; pass < 3; pass++ {
			advanceSchedulingGate(t, db, operationID)
			_ = service.RunOnce(ctx)
		}
		stored, err := service.Store.ReadOperation(ctx, operationID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Status != "needs_attention" || stored.ErrorCode == "" || stored.ErrorCode == "DEPENDENCY_UNAVAILABLE" {
			t.Fatalf("deletion status=%s error=%s, want an explicit reason for the conflicting evidence", stored.Status, stored.ErrorCode)
		}
		if refunds := service.Gateway.(*deletionGatewayClient).refunds; refunds != 0 {
			t.Fatalf("refunds=%d, want no settlement without a confirmed deletion", refunds)
		}
		if appends := service.Ledger.(*deletionLedgerClient).appends; appends != 0 {
			t.Fatalf("deletion receipts=%d, want no receipt for a retirement that was never confirmed", appends)
		}
	})
}

// conflictingDeletionServe is an owner that answers with a retirement for a
// different runtime, so the deletion cannot bind the answer to its own frozen
// runtime.
type conflictingDeletionServe struct {
	api.ServeAgentCoordinationClient
}

func (s *conflictingDeletionServe) Retire(_ context.Context, _ *api.RuntimeStopCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	return &api.Operation{OperationId: "serve-retire-other", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_RETIRE, ResourceId: "runtime-other", Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_RETIREMENT, ObservationResult: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_CONFIRMED.Enum()}, nil
}
