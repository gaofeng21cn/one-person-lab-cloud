package delivery_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/serve/internal/delivery"
)

type capabilityForServe struct {
	api.CapabilityProductServiceClient
	api.CapabilityCoordinationClient
	version         *api.CapabilityVersion
	service         *delivery.Service
	bindFail        bool
	acquired, bound int
}

func (f *capabilityForServe) GetCapabilityVersion(context.Context, *api.GetCapabilityVersionRpcRequest, ...grpc.CallOption) (*api.CapabilityVersion, error) {
	return proto.Clone(f.version).(*api.CapabilityVersion), nil
}
func (f *capabilityForServe) AcquireReference(_ context.Context, r *api.ReferenceClaimRequest, _ ...grpc.CallOption) (*api.ReferenceClaim, error) {
	f.acquired++
	return &api.ReferenceClaim{Id: "claim-original", Target: r.Target, ClaimantOwner: r.ClaimantOwner, ClaimantResourceId: r.ClaimantResourceId, State: api.ReferenceClaimState_REFERENCE_CLAIM_STATE_ACQUIRED}, nil
}
func (f *capabilityForServe) BindReference(ctx context.Context, r *api.BindReferenceRequest, _ ...grpc.CallOption) (*api.ReferenceClaim, error) {
	f.bound++
	if f.bindFail {
		return nil, status.Error(codes.Unavailable, "response lost")
	}
	actual, err := f.service.ReadOwnerCommit(ownerservice.WithPeerOwner(ctx, owneridentity.Capability.Service()), &api.ReadOwnerCommitRequest{Owner: api.OwnerEnum_OWNER_ENUM_SERVE, OperationId: r.OwnerCommitEvidence.OperationId, ResourceId: r.OwnerCommitEvidence.ResourceId})
	if err != nil {
		return nil, err
	}
	if !proto.Equal(actual, r.OwnerCommitEvidence) {
		return nil, errors.New("owner evidence mismatch")
	}
	return &api.ReferenceClaim{Id: r.ClaimId, State: api.ReferenceClaimState_REFERENCE_CLAIM_STATE_BOUND, BoundOperationId: proto.String(actual.OperationId), BoundInputDigest: proto.String(actual.AcceptedInputDigest)}, nil
}

type resourcesForServe struct {
	api.FabricCoordinationClient
	confirmed bool
}

func (f *resourcesForServe) ReadResources(_ context.Context, r *api.ResourceReadbackRequest, _ ...grpc.CallOption) (*api.ResourceReadback, error) {
	out := &api.ResourceReadback{ResourceSetId: r.ResourceSetId, WorkspaceId: "ws-first", Outcome: api.Observation_OBSERVATION_UNKNOWN}
	if f.confirmed {
		out.Outcome = api.Observation_OBSERVATION_CONFIRMED
		out.ExecutionResources = &api.ResourceExecutionBinding{AccountId: "account-original", ComputeAllocationId: "compute-original", StorageVolumeId: "volume-original", DataAttachmentId: "attachment-original", DataAttachmentOperationId: "attach-operation-original"}
	}
	return out, nil
}

type runtimeForServe struct {
	starts, observes int
	observeErr       bool
}

func (f *runtimeForServe) Start(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) (delivery.RuntimeObservation, error) {
	f.starts++
	return runtimeReady(applicationEntry(), "https://ws.example/app", "ack-only"), nil
}
func (f *runtimeForServe) Observe(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) (delivery.RuntimeObservation, error) {
	f.observes++
	if f.observeErr {
		return delivery.RuntimeObservation{}, errors.New("readback unavailable")
	}
	return runtimeReady(applicationEntry(), "https://ws.example/app", "readback-original"), nil
}
func reservationFixture(t *testing.T) (*delivery.Service, *api.RuntimeReservationCommand, *capabilityForServe) {
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	d := deployCommand(t, "ws-first", "unused", 1)
	c := call(tenant, false)
	c.IdempotencyKey = "first-delivery"
	request := &api.RuntimeReservationCommand{Context: c, WorkspaceId: "ws-first", CapabilityVersionId: "cv_1", Artifact: d.DeploymentDescriptor.Artifact, DeploymentDescriptor: d.DeploymentDescriptor, DeploymentDescriptorDigest: d.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: d.DeploymentDescriptorObjectRef, ResourceSetId: "resource-set-original", DataAttachmentId: "attachment-original"}
	cap := &capabilityForServe{service: service, version: &api.CapabilityVersion{Id: "cv_1", Status: api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY, Artifact: request.Artifact, DeploymentDescriptor: request.DeploymentDescriptor, DeploymentDescriptorDigest: request.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: request.DeploymentDescriptorObjectRef, DataCompatibility: &api.DataCompatibility{DataSchemaVersion: "1"}}}
	service.Capability = cap
	service.References = cap
	return service, request, cap
}
func workspaceContext() context.Context {
	return ownerservice.WithPeerOwner(context.Background(), owneridentity.Workspace.Service())
}
func deployReserved(r *api.RuntimeReservationCommand, out *api.RuntimeReservation) *api.RuntimeDeployCommand {
	return &api.RuntimeDeployCommand{Context: r.Context, WorkspaceId: r.WorkspaceId, DeploymentId: out.DeploymentId, CapabilityVersionId: r.CapabilityVersionId, RuntimeInstanceId: out.RuntimeInstanceId, ExecutionEpoch: out.ExecutionEpoch, ResourceSetId: r.ResourceSetId, DataAttachmentId: r.DataAttachmentId, DeploymentDescriptor: r.DeploymentDescriptor, DeploymentDescriptorDigest: r.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: r.DeploymentDescriptorObjectRef}
}
func TestServeReservationRecoversOriginalClaimAndRejectsConflicts(t *testing.T) {
	s, r, cap := reservationFixture(t)
	ctx := workspaceContext()
	cap.bindFail = true
	if _, err := s.Reserve(ctx, r); status.Code(err) != codes.Unavailable {
		t.Fatalf("Bind failure=%v", err)
	}
	cap.bindFail = false
	out, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Reserve(ctx, r)
	if err != nil || !proto.Equal(out, again) {
		t.Fatalf("replay=%v %v", again, err)
	}
	if cap.acquired != 1 || out.ExecutionEpoch != 1 || out.DeploymentId == "" || out.RuntimeInstanceId == "" || out.OperationId == "" {
		t.Fatalf("reservation=%v acquired=%d", out, cap.acquired)
	}
	changed := proto.Clone(r).(*api.RuntimeReservationCommand)
	changed.ResourceSetId = "foreign-resources"
	if _, err = s.Reserve(ctx, changed); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflict=%v", err)
	}
	foreign := proto.Clone(r).(*api.RuntimeReservationCommand)
	foreign.Context = call("other-tenant", false)
	foreign.Context.IdempotencyKey = "foreign"
	if _, err = s.Reserve(ctx, foreign); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("tenant=%v", err)
	}
	next := proto.Clone(r).(*api.RuntimeReservationCommand)
	next.Context.IdempotencyKey = "another-deployment"
	if _, err = s.Reserve(ctx, next); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("second delivery=%v", err)
	}
	if _, err = s.Reserve(serveContext(), r); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("BFF bypass=%v", err)
	}
	usage, err := s.ReadClaimUsage(ownerservice.WithPeerOwner(ctx, owneridentity.Capability.Service()), &api.ReadClaimUsageRequest{ClaimId: "claim-original", ClaimantResourceId: out.DeploymentId, ClaimantOperationId: out.OperationId})
	if err != nil || !usage.ActivelyRequired {
		t.Fatalf("usage=%v %v", usage, err)
	}
}
func TestServeDeployRequiresResourceAndApplicationReadback(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	out, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	command := deployReserved(r, out)
	resources := &resourcesForServe{}
	runtime := &runtimeForServe{observeErr: true}
	s.Resources = resources
	s.Runtime = runtime
	if _, err = s.Deploy(ctx, command); status.Code(err) != codes.FailedPrecondition || runtime.starts != 0 {
		t.Fatalf("unconfirmed resources=%v starts=%d", err, runtime.starts)
	}
	resources.confirmed = true
	if _, err = s.Deploy(ctx, command); err == nil {
		t.Fatal("start acknowledgement became ready without readback")
	}
	state, err := s.ReadRuntime(ctx, &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: out.RuntimeInstanceId, DeploymentId: out.DeploymentId})
	if err == nil {
		t.Fatalf("unavailable live observation returned persisted success: %v", state)
	}
	var persisted string
	if err = s.DB.QueryRowContext(ctx, `SELECT status FROM serve.agent_runtime_instances WHERE id=$1`, out.RuntimeInstanceId).Scan(&persisted); err != nil || persisted != "pending" {
		t.Fatalf("start acknowledgement changed state: %s %v", persisted, err)
	}
	runtime.observeErr = false
	state, err = s.Deploy(ctx, command)
	if err != nil || !state.ApplicationAvailable || state.ReadinessReceiptId != "readback-original" {
		t.Fatalf("deployment=%v %v", state, err)
	}
	access, err := s.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: r.Context, WorkspaceId: r.WorkspaceId})
	if err != nil || access.GetUrl() != "https://ws.example/app" {
		t.Fatalf("access=%v %v", access, err)
	}
	for _, change := range []func(*api.RuntimeDeployCommand){func(v *api.RuntimeDeployCommand) { v.ExecutionEpoch++ }, func(v *api.RuntimeDeployCommand) { v.RuntimeInstanceId = "other-runtime" }, func(v *api.RuntimeDeployCommand) { v.ResourceSetId = "other-resource-set" }, func(v *api.RuntimeDeployCommand) { v.DataAttachmentId = "other-attachment" }} {
		bad := proto.Clone(command).(*api.RuntimeDeployCommand)
		change(bad)
		before := runtime.starts
		if _, err = s.Deploy(ctx, bad); err == nil || runtime.starts != before {
			t.Fatalf("invalid input reached runtime: %v", err)
		}
	}
	old := runtimeReady(applicationEntry(), "https://older.example/app", "old")
	old.ObservedAt = time.Now().Add(-time.Hour)
	if _, err = s.RecordDeploymentObservation(ctx, command, old); err == nil {
		t.Fatal("older same-epoch observation overwrote ready record")
	}
}
