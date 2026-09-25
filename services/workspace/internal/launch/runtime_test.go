package launch

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/workspace/migrations"
)

type runtimeCapabilityClient struct {
	api.CapabilityProductServiceClient
	version *api.CapabilityVersion
	calls   int
}

func (c *runtimeCapabilityClient) GetCapabilityVersion(_ context.Context, r *api.GetCapabilityVersionRpcRequest, _ ...grpc.CallOption) (*api.CapabilityVersion, error) {
	c.calls++
	if r.CapabilityVersionId != c.version.Id {
		return nil, status.Error(codes.NotFound, "different version")
	}
	return proto.Clone(c.version).(*api.CapabilityVersion), nil
}

type runtimeServeClient struct {
	api.ServeAgentCoordinationClient
	reserveLost, deployLost              bool
	dispatchLost, observationUnavailable bool
	reserveCalls, deployCalls, readCalls int
	reserve                              *api.RuntimeReservationCommand
	reservation                          *api.RuntimeReservation
	command                              *api.RuntimeDeployCommand
	readback                             *api.RuntimeReadback
}

func (s *runtimeServeClient) Reserve(_ context.Context, r *api.RuntimeReservationCommand, _ ...grpc.CallOption) (*api.RuntimeReservation, error) {
	s.reserveCalls++
	if s.reserve == nil {
		s.reserve = proto.Clone(r).(*api.RuntimeReservationCommand)
		s.reservation = &api.RuntimeReservation{RuntimeInstanceId: "runtime-original", DeploymentId: "deployment-original", WorkspaceId: r.WorkspaceId, Artifact: r.Artifact, DeploymentDescriptorDigest: r.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: r.DeploymentDescriptorObjectRef, ExecutionEpoch: 1, OperationId: "serve-operation-original"}
	} else if !proto.Equal(s.reserve, r) {
		return nil, status.Error(codes.AlreadyExists, "reservation replay changed")
	}
	if s.reserveLost {
		s.reserveLost = false
		return nil, status.Error(codes.Unavailable, "reservation committed but response lost")
	}
	return proto.Clone(s.reservation).(*api.RuntimeReservation), nil
}

func (s *runtimeServeClient) Deploy(_ context.Context, r *api.RuntimeDeployCommand, _ ...grpc.CallOption) (*api.RuntimeReadback, error) {
	s.deployCalls++
	if s.command != nil && !proto.Equal(s.command, r) {
		return nil, status.Error(codes.AlreadyExists, "deployment replay changed")
	}
	s.command = proto.Clone(r).(*api.RuntimeDeployCommand)
	if s.dispatchLost {
		s.dispatchLost = false
		s.observationUnavailable = true
		return nil, status.Error(codes.Unavailable, "start committed before adapter dispatch")
	}
	s.readback = readyRuntime(r)
	if s.deployLost {
		s.deployLost = false
		return nil, status.Error(codes.Unavailable, "runtime ready but response lost")
	}
	return proto.Clone(s.readback).(*api.RuntimeReadback), nil
}

func (s *runtimeServeClient) ReadRuntime(_ context.Context, r *api.RuntimeReadbackRequest, _ ...grpc.CallOption) (*api.RuntimeReadback, error) {
	s.readCalls++
	if s.observationUnavailable {
		s.observationUnavailable = false
		return nil, status.Error(codes.Unavailable, "runtime has not been started")
	}
	if r.RuntimeInstanceId != s.reservation.RuntimeInstanceId || r.DeploymentId != s.reservation.DeploymentId {
		return nil, status.Error(codes.NotFound, "different runtime")
	}
	if s.readback == nil {
		return &api.RuntimeReadback{WorkspaceId: s.reservation.WorkspaceId, RuntimeInstanceId: s.reservation.RuntimeInstanceId, DeploymentId: s.reservation.DeploymentId, Artifact: s.reservation.Artifact, DeploymentDescriptorDigest: s.reservation.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: s.reservation.DeploymentDescriptorObjectRef, ExecutionEpoch: s.reservation.ExecutionEpoch, State: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING, Outcome: api.Observation_OBSERVATION_UNKNOWN}, nil
	}
	return proto.Clone(s.readback).(*api.RuntimeReadback), nil
}

type runtimeResourceClient struct {
	api.FabricCoordinationClient
	readback *api.ResourceReadback
}

type deniedResourceContinuation struct {
	api.FabricCoordinationClient
	denial                 codes.Code
	readback               *api.ResourceReadback
	readErr                error
	ensureCalls, readCalls int
}

func (f *deniedResourceContinuation) EnsureResources(_ context.Context, r *api.EnsureResourcesCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	f.ensureCalls++
	if r.WorkspaceId != "workspace-original" || r.ObligationId != "operation-original" || r.ConfirmedChargeReceiptId != "receipt-original" || r.Context.GetIdempotencyKey() != "operation-original:ensure_resources" {
		return nil, status.Error(codes.InvalidArgument, "different original funded request")
	}
	return nil, status.Error(f.denial, "continuation authority revoked")
}

func (f *deniedResourceContinuation) ReadResources(_ context.Context, r *api.ResourceReadbackRequest, _ ...grpc.CallOption) (*api.ResourceReadback, error) {
	f.readCalls++
	if r.ResourceSetId != "resource-set-original" || r.Context.GetAcceptedOperationGrantId() != "grant-original" {
		return nil, status.Error(codes.InvalidArgument, "different original resource read")
	}
	if f.readErr != nil {
		return nil, f.readErr
	}
	return proto.Clone(f.readback).(*api.ResourceReadback), nil
}

// A configured Ledger marker: these tests begin with its already confirmed,
// persisted receipt. Calling a new Ledger action is intentionally unsupported.
type persistedReceiptLedger struct{ api.LedgerCoordinationClient }

func (f *runtimeResourceClient) ReadResources(_ context.Context, r *api.ResourceReadbackRequest, _ ...grpc.CallOption) (*api.ResourceReadback, error) {
	if r.ResourceSetId != f.readback.ResourceSetId {
		return nil, status.Error(codes.NotFound, "different resources")
	}
	return proto.Clone(f.readback).(*api.ResourceReadback), nil
}

func readyRuntime(r *api.RuntimeDeployCommand) *api.RuntimeReadback {
	return &api.RuntimeReadback{WorkspaceId: r.WorkspaceId, RuntimeInstanceId: r.RuntimeInstanceId, DeploymentId: r.DeploymentId, Artifact: r.DeploymentDescriptor.Artifact, DeploymentDescriptorDigest: r.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: r.DeploymentDescriptorObjectRef, ExecutionEpoch: r.ExecutionEpoch, State: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY, ProcessReady: true, ApplicationAvailable: true, Outcome: api.Observation_OBSERVATION_CONFIRMED, ObservedAt: timestamppb.Now(), ReadinessReceiptId: "serve-observation-original", AccessUrl: "http://127.0.0.1:18080/"}
}

func runtimeVersion(t *testing.T, capabilityID string) *api.CapabilityVersion {
	t.Helper()
	artifact := &api.ArtifactReference{Repository: "local.example/application", Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111", Platform: &api.ImagePlatform{Os: api.ImagePlatformOsEnum_IMAGE_PLATFORM_OS_ENUM_LINUX, Architecture: api.ImagePlatformArchitectureEnum_IMAGE_PLATFORM_ARCHITECTURE_ENUM_AMD64}}
	descriptor := &api.DeploymentDescriptor{Artifact: artifact, SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD}
	raw, err := publicjson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(raw)
	return &api.CapabilityVersion{Id: capabilityID, Status: api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY, Artifact: artifact, ArtifactDigest: artifact.Digest, DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: "sha256:" + hex.EncodeToString(digest[:]), DeploymentDescriptorObjectRef: "descriptor-original"}
}

func runtimeDatabase(t *testing.T) *sql.DB {
	t.Helper()
	ctx := t.Context()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "workspace", Database: "opl_workspace", SchemaOwnerRole: "opl_workspace_owner", WriterRole: "opl_workspace_writer", RuntimeRole: "opl_workspace_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
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

func seedRuntimeOrder(t *testing.T, db *sql.DB) (*Service, ownerstore.Operation, *api.QuoteAcceptance, *api.ResourceReadback) {
	t.Helper()
	ctx := t.Context()
	_, offer, accepted := orderFixture()
	offer.Quote.TotalUsdMicros, accepted.Quote.TotalUsdMicros = 0, 0
	offer.ResourcePlan.Provider, accepted.ResourcePlan.Provider = "local-docker", "local-docker"
	offer.ResourcePlan.BillingMode, accepted.ResourcePlan.BillingMode = "LOCAL_NO_CHARGE", "LOCAL_NO_CHARGE"
	store, err := ownerstore.New(db, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	input, _ := json.Marshal(acceptedOrder{Quote: wire(offer), AuthorizationContextID: "authorization-original", InputDigest: "sha256:original"})
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	op, err := store.CreateOperation(ctx, tx, ownerstore.OperationInput{ID: "operation-original", TenantID: "tenant-original", ActorID: "actor-original", Kind: "create_workspace", ResourceID: "workspace-original", Stage: "runtime", RequestID: "request-original", AcceptedInput: input})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO workspace.workspaces(id,tenant_id,name,status,compute_plan_id,storage_plan_id,active_operation_id,created_by,version) VALUES($1,$2,'Original workspace','provisioning','compute-original','storage-original',$3,$4,1)`, op.ResourceID, op.TenantID, op.ID, op.ActorID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	commit, err := evidence(op)
	if err != nil {
		t.Fatal(err)
	}
	receipt := &api.LocalNoChargeReceiptEvidence{Receipt: &api.Receipt{Id: "receipt-original", Kind: api.ReceiptKindEnum_RECEIPT_KIND_ENUM_LOCAL_NO_CHARGE, Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: proto.String(op.ID), Outcome: api.ReceiptOutcomeEnum_RECEIPT_OUTCOME_ENUM_CONFIRMED, CreatedAt: timestamppb.Now()}, QuoteAcceptance: accepted, OwnerCommitEvidence: commit, EvidenceDigest: accepted.SnapshotDigest}
	resources := &api.ResourceReadback{ResourceSetId: "resource-set-original", WorkspaceId: op.ResourceID, Outcome: api.Observation_OBSERVATION_CONFIRMED, ExecutionResources: &api.ResourceExecutionBinding{AccountId: "account-original", ComputeAllocationId: "compute-original", StorageVolumeId: "storage-original", DataAttachmentId: "attachment-original", DataAttachmentOperationId: "attachment-operation-original"}}
	result, _ := json.Marshal(orderResult{GrantID: "grant-original", Acceptance: wire(accepted), ZeroChargeReceipt: wire(receipt), FabricOperationID: "fabric-operation-original", ResourceSetID: resources.ResourceSetId, ResourceReadback: wire(resources)})
	if _, err = db.ExecContext(ctx, `UPDATE workspace.operations SET status='awaiting_confirmation',observation_result='confirmed',result=$2 WHERE id=$1`, op.ID, result); err != nil {
		t.Fatal(err)
	}
	return &Service{Store: store, Fabric: &runtimeResourceClient{readback: resources}}, op, accepted, resources
}

func TestRuntimeRecoveryUsesDurableOriginalIdentities(t *testing.T) {
	for _, loss := range []string{"reserve", "deploy", "dispatch"} {
		t.Run(loss+" response lost", func(t *testing.T) {
			db := runtimeDatabase(t)
			service, op, accepted, resources := seedRuntimeOrder(t, db)
			capability := &runtimeCapabilityClient{version: runtimeVersion(t, accepted.Quote.GetCapabilityVersionId())}
			serve := &runtimeServeClient{reserveLost: loss == "reserve", deployLost: loss == "deploy", dispatchLost: loss == "dispatch"}
			service.Capability, service.Serve = capability, serve
			if err := service.Resume(t.Context(), op.ID); status.Code(err) != codes.Unavailable {
				t.Fatalf("first pass must record response loss: %v", err)
			}
			stored, err := service.Store.ReadOperation(t.Context(), op.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.WorkerLeaseToken != "" || stored.Status != "awaiting_confirmation" || stored.Observation != "unknown" {
				t.Fatalf("lost response was not durably recoverable: %+v", stored)
			}
			_, before, err := decodeOrder(stored)
			if err != nil {
				t.Fatal(err)
			}
			if len(before.RuntimeCapability) == 0 || len(before.RuntimeBinding) == 0 {
				t.Fatal("descriptor or execution binding was not frozen before side effects")
			}
			if loss == "deploy" && (len(before.RuntimeReservation) == 0 || len(before.RuntimeCommand) == 0) {
				t.Fatal("deployment identity was not frozen before dispatch")
			}
			// A fresh service instance resumes only the persisted original request.
			restarted := &Service{Store: service.Store, Fabric: service.Fabric, Capability: capability, Serve: serve}
			if _, err = db.ExecContext(t.Context(), `UPDATE workspace.saga_steps SET next_attempt_at=NULL WHERE operation_id=$1`, op.ID); err != nil {
				t.Fatal(err)
			}
			if err = restarted.RunOnce(t.Context()); err != nil {
				t.Fatal(err)
			}
			stored, err = restarted.Store.ReadOperation(t.Context(), op.ID)
			if err != nil {
				t.Fatal(err)
			}
			_, after, err := decodeOrder(stored)
			if err != nil {
				t.Fatal(err)
			}
			readback := &api.RuntimeReadback{}
			if protojson.Unmarshal(after.RuntimeReadback, readback) != nil || readback.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY || readback.WorkspaceId != op.ResourceID || after.ResourceSetID != resources.ResourceSetId {
				t.Fatal("original runtime ready evidence was not persisted")
			}
			if stored.Status != "awaiting_confirmation" || stored.Stage != "activation" || stored.CompletedAt.IsZero() == false {
				t.Fatal("runtime readiness incorrectly completed Workspace activation")
			}
			wantDeployCalls := 1
			if loss == "dispatch" {
				wantDeployCalls = 2
			}
			if serve.deployCalls != wantDeployCalls || capability.calls != 1 {
				t.Fatalf("restart repeated execution or refetched descriptor: deploy=%d capability=%d", serve.deployCalls, capability.calls)
			}
			if loss == "reserve" && serve.reserveCalls != 2 {
				t.Fatal("lost reservation was not replayed")
			}
			if loss == "deploy" && serve.reserveCalls != 1 {
				t.Fatal("restart reserved another runtime")
			}
			if serve.reserve.Context.IdempotencyKey != op.ID+":reserve_runtime" || serve.command.Context.IdempotencyKey != op.ID+":deploy_runtime" {
				t.Fatal("runtime commands lost original idempotency identity")
			}
			var subscriptions int
			var workspaceState string
			if err = db.QueryRowContext(t.Context(), `SELECT count(*) FROM workspace.subscriptions`).Scan(&subscriptions); err != nil {
				t.Fatal(err)
			}
			if err = db.QueryRowContext(t.Context(), `SELECT status FROM workspace.workspaces WHERE id=$1`, op.ResourceID).Scan(&workspaceState); err != nil {
				t.Fatal(err)
			}
			if subscriptions != 0 || workspaceState != "provisioning" {
				t.Fatal("runtime confirmation fabricated a paid period or Workspace activation")
			}
			// A later Fabric binding drift cannot start the accepted descriptor on
			// another allocation under the same original runtime identity.
			resources.ExecutionResources.ComputeAllocationId = "different-compute"
			if err = restarted.Resume(t.Context(), op.ID); status.Code(err) != codes.DataLoss {
				t.Fatalf("execution binding drift not refused: %v", err)
			}
			if serve.deployCalls != wantDeployCalls {
				t.Fatal("drift dispatched another runtime")
			}
		})
	}
}

func TestRevokedResourceContinuationReadsOriginalWithoutNewRuntime(t *testing.T) {
	for _, tc := range []struct {
		name                string
		denial              codes.Code
		outcome             api.Observation
		readErr             error
		wantRead            bool
		wantOperationError  string
		wantReadObservation string
	}{
		{"forbidden confirmed", codes.PermissionDenied, api.Observation_OBSERVATION_CONFIRMED, nil, true, "FORBIDDEN", "confirmed"},
		{"unauthenticated unknown", codes.Unauthenticated, api.Observation_OBSERVATION_UNKNOWN, nil, true, "UNAUTHENTICATED", "unknown"},
		{"forbidden read unavailable", codes.PermissionDenied, api.Observation_OBSERVATION_CONFIRMED, status.Error(codes.Unavailable, "read unavailable"), true, "FORBIDDEN", "unknown"},
		{"transport does not authorize closeout", codes.Unavailable, api.Observation_OBSERVATION_CONFIRMED, nil, false, "DEPENDENCY_UNAVAILABLE", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := runtimeDatabase(t)
			service, op, accepted, resources := seedRuntimeOrder(t, db)
			resources.Outcome = tc.outcome
			fabric := &deniedResourceContinuation{denial: tc.denial, readback: resources, readErr: tc.readErr}
			capability := &runtimeCapabilityClient{version: runtimeVersion(t, accepted.Quote.GetCapabilityVersionId())}
			serve := &runtimeServeClient{}
			service.Fabric, service.Ledger, service.Capability, service.Serve = fabric, &persistedReceiptLedger{}, capability, serve
			err := service.Resume(t.Context(), op.ID)
			if tc.wantRead && tc.readErr == nil && err != nil {
				t.Fatal(err)
			}
			if (!tc.wantRead || tc.readErr != nil) && status.Code(err) != codes.Unavailable {
				t.Fatalf("missing original failure: %v", err)
			}
			if fabric.ensureCalls != 1 || (fabric.readCalls == 1) != tc.wantRead {
				t.Fatalf("unexpected continuation calls: ensure=%d read=%d", fabric.ensureCalls, fabric.readCalls)
			}
			if capability.calls != 0 || serve.reserveCalls != 0 || serve.deployCalls != 0 || serve.readCalls != 0 {
				t.Fatal("denied continuation reached runtime owners")
			}
			stored, err := service.Store.ReadOperation(t.Context(), op.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.ErrorCode != tc.wantOperationError || stored.Terminal() || !stored.CompletedAt.IsZero() || stored.WorkerLeaseToken != "" {
				t.Fatalf("denial/unknown evidence lost: %+v", stored)
			}
			_, result, err := decodeOrder(stored)
			if err != nil {
				t.Fatal(err)
			}
			if result.ResourceSetID != "resource-set-original" || result.FabricOperationID != "fabric-operation-original" || len(result.RuntimeReservation) > 0 {
				t.Fatal("closeout changed original resource identity")
			}
			var ensureObservation, ensureError string
			if err = db.QueryRowContext(t.Context(), `SELECT observation_result,error_code FROM workspace.saga_steps WHERE operation_id=$1 AND step_key='ensure_resources'`, op.ID).Scan(&ensureObservation, &ensureError); err != nil {
				t.Fatal(err)
			}
			if tc.wantRead {
				if stored.Status != "needs_attention" || stored.Observation != "rejected" || stored.Stage != "original_action_readback" || ensureObservation != "rejected" || ensureError != tc.wantOperationError {
					t.Fatal("read-only closeout erased the provisioning rejection")
				}
				var readObservation string
				if err = db.QueryRowContext(t.Context(), `SELECT observation_result FROM workspace.saga_steps WHERE operation_id=$1 AND step_key='read_resources'`, op.ID).Scan(&readObservation); err != nil {
					t.Fatal(err)
				}
				if readObservation != tc.wantReadObservation {
					t.Fatalf("read outcome=%s, want=%s", readObservation, tc.wantReadObservation)
				}
				if tc.readErr == nil {
					r := &api.ResourceReadback{}
					if protojson.Unmarshal(result.ResourceReadback, r) != nil || !proto.Equal(r, resources) {
						t.Fatal("original resource readback was not saved exactly")
					}
				}
			} else if ensureObservation != "unknown" || stored.Observation != "unknown" {
				t.Fatal("transport failure was treated as explicit rejection")
			}
			var state string
			if err = db.QueryRowContext(t.Context(), `SELECT status FROM workspace.workspaces WHERE id=$1`, op.ResourceID).Scan(&state); err != nil {
				t.Fatal(err)
			}
			if state != "provisioning" {
				t.Fatal("closeout fabricated Workspace readiness")
			}
		})
	}
}

func TestRuntimeReadbackRejectsIdentityAndReadinessDrift(t *testing.T) {
	op, _, accepted := orderFixture()
	version := runtimeVersion(t, accepted.Quote.GetCapabilityVersionId())
	binding := &api.ResourceExecutionBinding{DataAttachmentId: "attachment-original"}
	reservation := &api.RuntimeReservation{DeploymentId: "deployment-original", RuntimeInstanceId: "runtime-original", ExecutionEpoch: 1}
	command := runtimeCommand(op, "grant-original", accepted, version, binding, "resource-set-original", reservation)
	ready := readyRuntime(command)
	if err := validateRuntimeReadback(command, ready); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*api.RuntimeReadback){
		"workspace":         func(r *api.RuntimeReadback) { r.WorkspaceId = "other" },
		"runtime":           func(r *api.RuntimeReadback) { r.RuntimeInstanceId = "other" },
		"deployment":        func(r *api.RuntimeReadback) { r.DeploymentId = "other" },
		"epoch":             func(r *api.RuntimeReadback) { r.ExecutionEpoch++ },
		"descriptor":        func(r *api.RuntimeReadback) { r.DeploymentDescriptorDigest = "other" },
		"object":            func(r *api.RuntimeReadback) { r.DeploymentDescriptorObjectRef = "other" },
		"artifact":          func(r *api.RuntimeReadback) { r.Artifact.Digest = "other" },
		"platform":          func(r *api.RuntimeReadback) { r.Artifact.Platform = nil },
		"model version":     func(r *api.RuntimeReadback) { r.AppliedModelConfigurationVersion++ },
		"unknown ready":     func(r *api.RuntimeReadback) { r.Outcome = api.Observation_OBSERVATION_UNKNOWN },
		"unavailable ready": func(r *api.RuntimeReadback) { r.ApplicationAvailable = false },
		"missing time":      func(r *api.RuntimeReadback) { r.ObservedAt = nil },
		"missing receipt":   func(r *api.RuntimeReadback) { r.ReadinessReceiptId = "" },
	} {
		t.Run(name, func(t *testing.T) {
			altered := proto.Clone(ready).(*api.RuntimeReadback)
			change(altered)
			if status.Code(validateRuntimeReadback(command, altered)) != codes.DataLoss {
				t.Fatal("runtime drift was accepted")
			}
		})
	}
}
