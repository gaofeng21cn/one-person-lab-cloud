package delivery_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
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

// runtimeControlForServe serves one approved Runtime Release over the real
// RuntimeControlProductService client surface so the default OPL App reservation
// path (no CapabilityVersion, no Package, no Build) is exercised against Serve's
// real owner store rather than a stub.
type runtimeControlForServe struct {
	api.RuntimeControlProductServiceClient
	release *api.RuntimeVersion
}

func (f *runtimeControlForServe) ListRuntimeVersions(context.Context, *api.ListRuntimeVersionsRpcRequest, ...grpc.CallOption) (*api.RuntimeVersionPage, error) {
	return &api.RuntimeVersionPage{Items: []*api.RuntimeVersion{proto.Clone(f.release).(*api.RuntimeVersion)}}, nil
}

type resourcesForServe struct {
	api.FabricCoordinationClient
	confirmed      bool
	workspace      string
	dataAttachment string
}

func (f *resourcesForServe) ReadResources(_ context.Context, r *api.ResourceReadbackRequest, _ ...grpc.CallOption) (*api.ResourceReadback, error) {
	workspace := f.workspace
	if workspace == "" {
		workspace = "ws-first"
	}
	attachment := f.dataAttachment
	if attachment == "" {
		attachment = "attachment-original"
	}
	out := &api.ResourceReadback{ResourceSetId: r.ResourceSetId, WorkspaceId: workspace, Outcome: api.Observation_OBSERVATION_UNKNOWN}
	if f.confirmed {
		out.Outcome = api.Observation_OBSERVATION_CONFIRMED
		out.ExecutionResources = &api.ResourceExecutionBinding{AccountId: "account-original", ComputeAllocationId: "compute-original", StorageVolumeId: "volume-original", DataAttachmentId: attachment, DataAttachmentOperationId: "attach-operation-original"}
	}
	return out, nil
}

type runtimeForServe struct {
	starts, observes int
	observeErr       bool
	state            api.AgentRuntimeObservationState
	readiness        string
}

func (f *runtimeForServe) Start(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) (delivery.RuntimeObservation, error) {
	f.starts++
	return runtimeReady(applicationEntry(), "https://ws.example/app", "ack-only"), nil
}
func (*runtimeForServe) Lifecycle(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding, string) error {
	return nil
}
func (*runtimeForServe) Reload(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) error {
	return nil
}
func (*runtimeForServe) Credentials(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) (*api.WorkspaceApplicationCredentials, error) {
	return nil, errors.New("credentials unavailable")
}
func (f *runtimeForServe) Observe(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) (delivery.RuntimeObservation, error) {
	f.observes++
	if f.observeErr {
		return delivery.RuntimeObservation{}, errors.New("readback unavailable")
	}
	if f.state == api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING {
		return delivery.RuntimeObservation{State: f.state, ObservedAt: time.Now().UTC()}, nil
	}
	readiness := f.readiness
	if readiness == "" {
		readiness = "readback-original"
	}
	return runtimeReady(applicationEntry(), "https://ws.example/app", readiness), nil
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
	var readinessEvents int
	if err = s.DB.QueryRowContext(ctx, `SELECT count(*) FROM serve.outbox_events WHERE event_type='serve.agent_readiness_observed.v1' AND aggregate_id=$1`, out.DeploymentId).Scan(&readinessEvents); err != nil {
		t.Fatal(err)
	}
	if readinessEvents != 1 {
		t.Fatalf("ready deployment emitted %d readiness events, want one", readinessEvents)
	}
	access, err := s.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: r.Context, WorkspaceId: r.WorkspaceId})
	if err != nil || access.GetUrl() != "https://ws.example/app" {
		t.Fatalf("access=%v %v", access, err)
	}
	operations, err := ownerservice.NewOperations(ownerservice.OwnerServe, s.Store, ownerservice.NewAuthorizer(ownerservice.OwnerServe, &fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	operation, err := operations.Read(serveContext(), &api.OwnerOperationRequest{Context: r.Context, OperationId: out.OperationId})
	if err != nil || operation.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_VERIFICATION || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("public operation readback=%v %v", operation, err)
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

func TestServeReadinessOutboxPreservesSameEpochProgression(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = &resourcesForServe{confirmed: true}
	runtime := &runtimeForServe{state: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING}
	s.Runtime = runtime
	command := deployReserved(r, reservation)
	if record, err := s.Deploy(ctx, command); err != nil || record.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING {
		t.Fatalf("pending deployment=%v %v", record, err)
	}
	runtime.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY
	runtime.readiness = "ready-evidence"
	if record, err := s.Deploy(ctx, command); err != nil || !record.GetApplicationAvailable() {
		t.Fatalf("ready deployment=%v %v", record, err)
	}
	if _, err := s.Deploy(ctx, command); err != nil {
		t.Fatal(err)
	}
	var events int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM serve.outbox_events WHERE event_type='serve.agent_readiness_observed.v1' AND aggregate_id=$1`, reservation.DeploymentId).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 2 {
		t.Fatalf("same-epoch readiness events=%d, want pending and ready", events)
	}
	var deliveries, acknowledged int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE acknowledged_at IS NOT NULL) FROM serve.outbox_deliveries d JOIN serve.outbox_events e ON e.id=d.event_id WHERE e.aggregate_id=$1 AND d.consumer_owner='ledger'`, reservation.DeploymentId).Scan(&deliveries, &acknowledged); err != nil {
		t.Fatal(err)
	}
	if deliveries != 2 || acknowledged != 0 {
		t.Fatalf("readiness delivery rows deliveries=%d acknowledged=%d, want two pending before Ledger wiring", deliveries, acknowledged)
	}
}

type orderedObservationRuntime struct {
	entered      chan int
	releaseFirst chan struct{}
	mu           sync.Mutex
	calls        int
}

func (f *orderedObservationRuntime) Start(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) (delivery.RuntimeObservation, error) {
	return delivery.RuntimeObservation{}, nil
}
func (*orderedObservationRuntime) Lifecycle(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding, string) error {
	return nil
}
func (*orderedObservationRuntime) Reload(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) error {
	return nil
}
func (*orderedObservationRuntime) Credentials(context.Context, *api.RuntimeDeployCommand, *api.ResourceExecutionBinding) (*api.WorkspaceApplicationCredentials, error) {
	return nil, errors.New("credentials unavailable")
}
func (f *orderedObservationRuntime) Observe(ctx context.Context, _ *api.RuntimeDeployCommand, _ *api.ResourceExecutionBinding) (delivery.RuntimeObservation, error) {
	f.mu.Lock()
	f.calls++
	n := f.calls
	f.mu.Unlock()
	f.entered <- n
	if n == 1 {
		select {
		case <-f.releaseFirst:
		case <-ctx.Done():
			return delivery.RuntimeObservation{}, ctx.Err()
		}
		return delivery.RuntimeObservation{State: api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING, ObservedAt: time.Now().UTC()}, nil
	}
	return runtimeReady(applicationEntry(), "https://ws.example/app", "ordered-ready"), nil
}

// Separate Service instances model two processes sharing only the owner database.
// The first read sampled pending and stalls; the second cannot sample ready until
// the first observation and selection commit together under the database lock.
func TestServeSerializesObservationThroughSelectionAcrossInstances(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	s.Resources = &resourcesForServe{confirmed: true}
	s.Runtime = &runtimeForServe{}
	command := deployReserved(r, reservation)
	if _, err = s.Deploy(ctx, command); err != nil {
		t.Fatal(err)
	}
	second, err := delivery.New(s.DB, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	sequence := &orderedObservationRuntime{entered: make(chan int, 2), releaseFirst: make(chan struct{})}
	second.Resources = s.Resources
	second.Runtime = sequence
	s.Runtime = sequence
	request := &api.RuntimeReadbackRequest{Context: r.Context, RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId}
	results := make(chan error, 2)
	go func() { _, err := s.ReadRuntime(ctx, request); results <- err }()
	select {
	case n := <-sequence.entered:
		if n != 1 {
			t.Fatalf("first observation=%d", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first observation did not start")
	}
	go func() { _, err := second.ReadRuntime(ctx, request); results <- err }()
	select {
	case n := <-sequence.entered:
		close(sequence.releaseFirst)
		for i := 0; i < 2; i++ {
			<-results
		}
		t.Fatalf("concurrent observation %d bypassed the owner lock", n)
	case <-time.After(75 * time.Millisecond):
	}
	close(sequence.releaseFirst)
	select {
	case n := <-sequence.entered:
		if n != 2 {
			t.Fatalf("second observation=%d", n)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second observation did not resume")
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	var runtimeState, deploymentState string
	if err = s.DB.QueryRowContext(ctx, `SELECT i.status,d.status FROM serve.agent_runtime_instances i JOIN serve.agent_deployments d ON d.id=i.deployment_id WHERE i.id=$1`, reservation.RuntimeInstanceId).Scan(&runtimeState, &deploymentState); err != nil || runtimeState != "ready" || deploymentState != "active" {
		t.Fatalf("final states=%s/%s err=%v", runtimeState, deploymentState, err)
	}
}

// defaultAppRelease builds one approved Runtime Release whose publisher contract
// carries the immutable OCI and the application revision template the default OPL
// App deploys. The descriptor is the release's own contract, never a caller image.
func defaultAppRelease(t *testing.T, s *delivery.Service) *api.RuntimeVersion {
	t.Helper()
	revision := &api.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationId: "opl-app", Version: "1", Platform: "linux/amd64",
		Image:          "registry.test/opl-app@" + artifactDigest,
		ExposurePolicy: api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION,
	}
	return &api.RuntimeVersion{
		Id: "rv-opl-app", Status: api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED,
		PublisherContract: &api.RuntimePublisherContract{
			Image:                       &api.ArtifactReference{Repository: "registry.test/opl-app", Digest: artifactDigest},
			ApplicationRevisionTemplate: revision,
		},
	}
}

// descriptorDigestOf is the digest of a descriptor's canonical public JSON bytes,
// the same encoding Serve records at reservation.
func descriptorDigestOf(t *testing.T, descriptor *api.DeploymentDescriptor) string {
	t.Helper()
	raw, err := publicjson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TestServeDefaultAppReservationCarriesNoCapabilityVersion proves the default OPL
// App is a first-class delivery source: Reserve accepts an explicit opl_app
// selection backed by an approved Runtime Release, records the runtime version and
// no CapabilityVersion, acquires exactly one Runtime Release reference claim, and
// refuses a mixed source that also names a CapabilityVersion.
func TestServeDefaultAppReservationCarriesNoCapabilityVersion(t *testing.T) {
	s, _, cap := reservationFixture(t)
	capCalls := cap.acquired
	ctx := workspaceContext()
	release := defaultAppRelease(t, s)
	s.RuntimeReleases = &runtimeControlForServe{release: release}

	tenant := "tenant-opl-app"
	c := call(tenant, false)
	c.IdempotencyKey = "default-app-first-delivery"
	descriptor := &api.DeploymentDescriptor{SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Artifact: release.GetPublisherContract().GetImage(), Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_RUNTIME_RELEASE, ApplicationRevision: release.GetPublisherContract().GetApplicationRevisionTemplate()}
	request := &api.RuntimeReservationCommand{
		Context:     c,
		WorkspaceId: "ws-opl-app",
		ApplicationSelection: &api.WorkspaceApplicationSelection{
			Kind:             api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP,
			RuntimeVersionId: proto.String(release.GetId()),
		},
		Artifact:                      release.GetPublisherContract().GetImage(),
		DeploymentDescriptor:          descriptor,
		DeploymentDescriptorDigest:    descriptorDigestOf(t, descriptor),
		DeploymentDescriptorObjectRef: "runtime-contract:" + release.GetId(),
		ResourceSetId:                 "resource-set-opl-app",
		DataAttachmentId:              "attachment-opl-app",
	}
	out, err := s.Reserve(ctx, request)
	if err != nil {
		t.Fatalf("default App reservation: %v", err)
	}
	if out.DeploymentId == "" || out.RuntimeInstanceId == "" || out.ExecutionEpoch != 1 {
		t.Fatalf("reservation=%v", out)
	}
	var applicationKind, capability, runtimeVersion string
	if err := s.DB.QueryRowContext(ctx, `SELECT application_kind, capability_version_id, COALESCE(runtime_version_id,'') FROM serve.agent_deployments WHERE id=$1`, out.DeploymentId).Scan(&applicationKind, &capability, &runtimeVersion); err != nil {
		t.Fatal(err)
	}
	if applicationKind != "opl_app" || capability != "" || runtimeVersion != release.GetId() {
		t.Fatalf("default App row kind=%q capability=%q runtime=%q", applicationKind, capability, runtimeVersion)
	}
	if cap.acquired != capCalls+1 {
		t.Fatalf("the default App must acquire exactly one Runtime Release reference claim, delta=%d", cap.acquired-capCalls)
	}
	mixed := proto.Clone(request).(*api.RuntimeReservationCommand)
	mixed.Context = call(tenant, false)
	mixed.Context.IdempotencyKey = "mixed"
	mixed.CapabilityVersionId = "cv_1"
	if _, err := s.Reserve(ctx, mixed); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("mixed source accepted: %v", err)
	}
}

// TestServeDefaultAppDeploysReadyAndPublishesAccess proves the default OPL App is
// deployable end to end inside Serve's own owner store: after Reserve admits an
// approved Runtime Release (no CapabilityVersion, no Build, no Package), a
// Workspace deploy command drives the real runtime adapter to readiness, Serve
// commits the single active deployment and its confirmed access entry, and the
// runtime-availability fact is published on Send. The applied model configuration
// is not fabricated: with no runtime readback it stays 0.
func TestServeDefaultAppDeploysReadyAndPublishesAccess(t *testing.T) {
	s, _, _ := reservationFixture(t)
	ctx := workspaceContext()
	release := defaultAppRelease(t, s)
	s.RuntimeReleases = &runtimeControlForServe{release: release}

	tenant := "tenant-opl-app"
	c := call(tenant, false)
	c.IdempotencyKey = "default-app-first-delivery"
	descriptor := &api.DeploymentDescriptor{SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Artifact: release.GetPublisherContract().GetImage(), Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_RUNTIME_RELEASE, ApplicationRevision: release.GetPublisherContract().GetApplicationRevisionTemplate()}
	reserved, err := s.Reserve(ctx, &api.RuntimeReservationCommand{
		Context: c, WorkspaceId: "ws-opl-app",
		ApplicationSelection:          &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP, RuntimeVersionId: proto.String(release.GetId())},
		Artifact:                      release.GetPublisherContract().GetImage(),
		DeploymentDescriptor:          descriptor,
		DeploymentDescriptorDigest:    descriptorDigestOf(t, descriptor),
		DeploymentDescriptorObjectRef: "runtime-contract:" + release.GetId(),
		ResourceSetId:                 "resource-set-opl-app",
		DataAttachmentId:              "attachment-opl-app",
	})
	if err != nil {
		t.Fatalf("default App reservation: %v", err)
	}

	s.Resources = &resourcesForServe{confirmed: true, workspace: "ws-opl-app", dataAttachment: "attachment-opl-app"}
	runtime := &runtimeForServe{}
	s.Runtime = runtime
	command := &api.RuntimeDeployCommand{
		Context: c, WorkspaceId: "ws-opl-app", DeploymentId: reserved.DeploymentId,
		ApplicationSelection: &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP, RuntimeVersionId: proto.String(release.GetId())},
		RuntimeInstanceId:    reserved.RuntimeInstanceId, ExecutionEpoch: reserved.ExecutionEpoch,
		ResourceSetId: "resource-set-opl-app", DataAttachmentId: "attachment-opl-app",
		DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: reserved.DeploymentDescriptorDigest,
		DeploymentDescriptorObjectRef: reserved.DeploymentDescriptorObjectRef,
	}
	state, err := s.Deploy(ctx, command)
	if err != nil {
		t.Fatalf("default App deploy: %v", err)
	}
	if !state.GetApplicationAvailable() || state.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY || state.GetAccessUrl() != "https://ws.example/app" {
		t.Fatalf("runtime readback=%v", state)
	}
	if state.GetAppliedModelConfigurationVersion() != 0 {
		t.Fatalf("applied model version must not be fabricated, got %d", state.GetAppliedModelConfigurationVersion())
	}
	var deploymentStatus, applicationKind string
	if err := s.DB.QueryRowContext(ctx, `SELECT status, application_kind FROM serve.agent_deployments WHERE id=$1`, reserved.DeploymentId).Scan(&deploymentStatus, &applicationKind); err != nil {
		t.Fatal(err)
	}
	if deploymentStatus != "active" || applicationKind != "opl_app" {
		t.Fatalf("deployment status=%q kind=%q", deploymentStatus, applicationKind)
	}
	var readinessEvents int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM serve.outbox_events WHERE event_type='serve.agent_readiness_observed.v1' AND aggregate_id=$1`, reserved.DeploymentId).Scan(&readinessEvents); err != nil {
		t.Fatal(err)
	}
	if readinessEvents != 1 {
		t.Fatalf("ready default App emitted %d readiness events, want one", readinessEvents)
	}
	access, err := s.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: c, WorkspaceId: "ws-opl-app"})
	if err != nil || access.GetUrl() != "https://ws.example/app" {
		t.Fatalf("default App access=%v err=%v", access, err)
	}
}
