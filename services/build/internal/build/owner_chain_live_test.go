//go:build livebuild

package build

import (
	"context"
	"database/sql"
	"net"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publisherjson"
	"opl-cloud/services/build/migrations"
	capabilitycatalog "opl-cloud/services/capability/catalog"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	runtimecatalog "opl-cloud/services/runtime-control/catalog"
	runtimemigrations "opl-cloud/services/runtime-control/migrations"
)

// CloudIdentity's decisions are fixtures. All tested domain commands below use
// the real owner handlers, separate writer-role databases and typed gRPC clients.
type fixtureIdentity struct {
	api.CloudIdentityAuthorizationClient
}

func (fixtureIdentity) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	if r.SessionId == nil && r.GetAcceptedOperationGrantId() != "isolated-grant" {
		return nil, status.Error(codes.PermissionDenied, "fixture session required")
	}
	return &api.AuthorizationDecision{Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Scope: r.Scope, ActorId: r.ActorId, SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId, AudienceOwner: r.AudienceOwner, Action: r.Action, Resource: r.Resource, PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, nil
}
func (fixtureIdentity) IssueAcceptedOperationGrant(_ context.Context, r *api.AcceptedOperationGrantRequest, _ ...grpc.CallOption) (*api.AcceptedOperationGrant, error) {
	e := r.OwnerCommitEvidence
	return &api.AcceptedOperationGrant{Id: "isolated-grant", AcceptedOperationOwner: e.Owner, AcceptedOperationId: e.OperationId, ResourceId: e.ResourceId}, nil
}
func liveConn(t *testing.T, address string, peer owneridentity.Service) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoke grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		return invoke(metadata.AppendToOutgoingContext(ctx, "test-peer", string(peer)), method, req, reply, cc, opts...)
	}))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
func liveServer(t *testing.T, register func(*grpc.Server)) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, r any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		values := metadata.ValueFromIncomingContext(ctx, "test-peer")
		if len(values) != 1 {
			return nil, status.Error(codes.Unauthenticated, "fixture peer missing")
		}
		return next(ownerservice.WithPeerOwner(ctx, owneridentity.Service(values[0])), r)
	}))
	register(server)
	go server.Serve(l)
	t.Cleanup(server.Stop)
	return l.Addr().String()
}
func liveOwnerDB(t *testing.T, ctx context.Context, dsn, name string, source ownerstore.MigrationSource) *sql.DB {
	t.Helper()
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: name, Database: "opl_" + name, SchemaOwnerRole: "opl_" + name + "_owner", WriterRole: "opl_" + name + "_writer", RuntimeRole: "opl_" + name + "_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
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

type lostInboxAck struct {
	api.DomainInboxClient
	lost atomic.Bool
}

func (c *lostInboxAck) Deliver(ctx context.Context, r *api.DeliverEventRequest, opts ...grpc.CallOption) (*api.InboxAck, error) {
	ack, err := c.DomainInboxClient.Deliver(ctx, r, opts...)
	if err == nil && c.lost.CompareAndSwap(false, true) {
		return nil, status.Error(codes.Unavailable, "injected lost consumer acknowledgement")
	}
	return ack, err
}

func verifyOwnerChain(t *testing.T, ctx context.Context, dsn string, capability *capabilitycatalog.Service, capAddr string, runner *Runner, input *api.BuildInputSnapshot) {
	t.Helper()
	// The isolated publisher's already-approved namespace and WebUI artifacts are
	// explicit fixtures. Runtime admission and policy selection use the real API.
	if _, err := capability.DB.ExecContext(ctx, `INSERT INTO capability.publisher_namespaces(id,name,kind,registry_id,repository_prefix,admission_receipt_id) VALUES($1,'local-publisher','official','local-registry',$2,'isolated-publisher-admission')`, input.RuntimeContract.PublisherNamespaceId, runner.RegistryPrefix[:len(runner.RegistryPrefix)-len("/result")]); err != nil {
		t.Fatal(err)
	}
	webuiBytes, err := publisherjson.Marshal(input.WebuiContract)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = capability.DB.ExecContext(ctx, `INSERT INTO capability.webui_versions(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_versions,ui_protocol_version,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref) VALUES($1,'local-webui','v1',$2,$3,'publisher',$4,'opl-webui/v1','local-admission',$5,$6,$7,'local-webui-contract')`, input.WebuiVersionId, input.WebuiArtifact.Repository, input.WebuiArtifact.Digest, pq.Array(input.WebuiContract.RuntimeAbiVersions), input.WebuiContract.PublisherNamespaceId, digest(webuiBytes), webuiBytes); err != nil {
		t.Fatal(err)
	}
	schemaPath := "../../../../docs/spec/target/contracts/publisher-contract.schema.json"
	schemaBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	runtimeSource, err := runtimemigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	runtimeDB := liveOwnerDB(t, ctx, dsn, "runtime_control", runtimeSource)
	runtimeService, err := runtimecatalog.New(runtimeDB, ownerservice.NewAuthorizer(ownerservice.OwnerRuntimeControl, fixtureIdentity{}), api.NewCapabilityProductServiceClient(liveConn(t, capAddr, owneridentity.RuntimeControl.Service())), schemaPath, digest(schemaBytes))
	if err != nil {
		t.Fatal(err)
	}
	runtimeAddr := liveServer(t, func(server *grpc.Server) { api.RegisterRuntimeControlProductServiceServer(server, runtimeService) })
	runtimeClient := api.NewRuntimeControlProductServiceClient(liveConn(t, runtimeAddr, owneridentity.ConsoleBFF))
	call := func(key string, platform bool) *api.CallContext {
		scope := &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-live"}}}
		if platform {
			scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
		}
		return &api.CallContext{Scope: scope, ActorId: "publisher", SessionId: proto.String("isolated-publisher"), RequestId: key, IdempotencyKey: key, AuthorizationContextId: "fixture-authorization"}
	}
	runtime, err := runtimeClient.RegisterRuntimeVersion(ctx, &api.RegisterRuntimeVersionRpcRequest{Context: call("runtime-admit", true), Body: &api.RegisterRuntimeVersionRequest{Name: "local-runtime", VersionLabel: "v1", PublisherNamespaceId: input.RuntimeContract.PublisherNamespaceId, PublisherContract: input.RuntimeContract, AdmissionReceiptId: "local-runtime-admission"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = runtimeClient.SetBuildRuntimePolicy(ctx, &api.SetBuildRuntimePolicyRpcRequest{Context: call("runtime-policy", true), Body: &api.SetBuildRuntimePolicyRequest{RuntimeVersionId: runtime.Id}}); err != nil {
		t.Fatal(err)
	}
	capability.Runtime = api.NewRuntimeControlProductServiceClient(liveConn(t, runtimeAddr, owneridentity.Capability.Service()))
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	buildDB := liveOwnerDB(t, ctx, dsn, "build", source)
	store, err := ownerstore.New(buildDB, "build")
	if err != nil {
		t.Fatal(err)
	}
	capConn := liveConn(t, capAddr, owneridentity.Build.Service())
	capDrop := &lostInboxAck{DomainInboxClient: api.NewDomainInboxClient(capConn)}
	service, err := New(store, ownerservice.NewAuthorizer(ownerservice.OwnerBuild, fixtureIdentity{}), api.NewCapabilityCoordinationClient(capConn), api.NewCapabilityProductServiceClient(capConn), map[string]api.DomainInboxClient{"capability": capDrop}, fixtureIdentity{}, runner)
	if err != nil {
		t.Fatal(err)
	}
	buildAddr := liveServer(t, func(server *grpc.Server) {
		api.RegisterBuildProductServiceServer(server, service)
		api.RegisterBuildCoordinationServer(server, service)
		api.RegisterOwnerCommitReadbackServer(server, service)
		api.RegisterClaimUsageReadbackServer(server, service)
		api.RegisterDomainInboxServer(server, service)
	})
	buildCapConn := liveConn(t, buildAddr, owneridentity.Capability.Service())
	capability.Build = api.NewBuildCoordinationClient(buildCapConn)
	capability.Commit = api.NewOwnerCommitReadbackClient(buildCapConn)
	capability.Usage = api.NewClaimUsageReadbackClient(buildCapConn)
	buildDrop := &lostInboxAck{DomainInboxClient: api.NewDomainInboxClient(buildCapConn)}
	capability.BuildInbox = buildDrop
	client := api.NewBuildProductServiceClient(liveConn(t, buildAddr, owneridentity.ConsoleBFF))
	req := &api.CreateBuildRpcRequest{Context: call("create-build", false), Body: &api.CreateBuildRequest{PackageVersionId: input.PackageVersionId, WebuiVersionId: input.WebuiVersionId}}
	job, err := client.CreateBuild(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	coordination := api.NewCapabilityCoordinationClient(capConn)
	if _, err := coordination.AcquireReference(ctx, &api.ReferenceClaimRequest{Context: req.Context, Target: &api.ReferenceTarget{Target: &api.ReferenceTarget_PackageVersionId{PackageVersionId: input.PackageVersionId}}, ClaimantOwner: api.OwnerEnum_OWNER_ENUM_SERVE, ClaimantResourceId: job.Id}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("accepted forged claimant owner: %v", err)
	}
	replay, err := client.CreateBuild(ctx, req)
	if err != nil || replay.Id != job.Id {
		t.Fatalf("admission replay: %v", err)
	}
	if err = service.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	rec, err := service.read(ctx, job.Id)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Job.Status != api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_REGISTERING {
		t.Fatalf("real worker did not register: %v", rec.Job)
	}
	// Replaying bound claims must preserve the original binding, not fail a
	// restarted worker after some Acquire/Bind acknowledgements were lost.
	if err = service.claims(ctx, rec); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(12 * time.Second); ; {
		if err = service.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
		if err = capability.DeliverBuildResults(ctx); err != nil {
			t.Fatal(err)
		}
		var buildPending, capPending int
		if err = buildDB.QueryRowContext(ctx, `SELECT count(*) FROM build.outbox_deliveries WHERE consumer_owner='capability' AND acknowledged_at IS NULL`).Scan(&buildPending); err != nil {
			t.Fatal(err)
		}
		if err = capability.DB.QueryRowContext(ctx, `SELECT count(*) FROM capability.outbox_deliveries WHERE consumer_owner='build' AND acknowledged_at IS NULL`).Scan(&capPending); err != nil {
			t.Fatal(err)
		}
		rec, err = service.read(ctx, job.Id)
		if err != nil {
			t.Fatal(err)
		}
		if rec.Job.Status == api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_SUCCEEDED && buildPending == 0 && capPending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("registration did not converge: state=%s pending=%d/%d", rec.Job.Status, buildPending, capPending)
		}
		time.Sleep(100 * time.Millisecond)
	}
	version, err := api.NewCapabilityProductServiceClient(capConn).GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: req.Context, CapabilityVersionId: rec.Job.GetResultCapabilityVersionId()})
	if err != nil {
		t.Fatal(err)
	}
	var versions, claims int
	if err = capability.DB.QueryRowContext(ctx, `SELECT count(*) FROM capability.capability_versions WHERE build_job_id=$1`, job.Id).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if err = capability.DB.QueryRowContext(ctx, `SELECT count(*) FROM capability.reference_claims WHERE claimant_resource_id=$1 AND bound_at IS NOT NULL`, job.Id).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if versions != 1 || claims != 3 || version.ArtifactDigest != rec.Job.GetArtifactDigest() {
		t.Fatalf("inconsistent registration: versions=%d claims=%d", versions, claims)
	}
	if !capDrop.lost.Load() || !buildDrop.lost.Load() {
		t.Fatal("lost acknowledgement scenarios did not execute")
	}
	t.Logf("real owner chain: Runtime API admission, CreateBuild replay, three bound claims, one ready CapabilityVersion; both registration acknowledgements lost and recovered; artifact=%s", version.ArtifactDigest)
}
