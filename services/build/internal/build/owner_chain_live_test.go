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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/build/migrations"
	capabilitycatalog "opl-cloud/services/capability/catalog"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	runtimecatalog "opl-cloud/services/runtime-control/catalog"
	runtimemigrations "opl-cloud/services/runtime-control/migrations"
)

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
func liveOwnerDB(t *testing.T, ctx context.Context, dsn, name string, source ownerstore.MigrationSource) (*sql.DB, string) {
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
	return db, h.RuntimeDSN
}

type lostInboxAck struct {
	api.DomainInboxClient
	lost      atomic.Bool
	last      *api.DeliverEventRequest
	lastError error
}

func (c *lostInboxAck) Deliver(ctx context.Context, r *api.DeliverEventRequest, opts ...grpc.CallOption) (*api.InboxAck, error) {
	c.last = proto.Clone(r).(*api.DeliverEventRequest)
	ack, err := c.DomainInboxClient.Deliver(ctx, r, opts...)
	c.lastError = err
	if err == nil && c.lost.CompareAndSwap(false, true) {
		return nil, status.Error(codes.Unavailable, "injected lost consumer acknowledgement")
	}
	return ack, err
}

func verifyOwnerChain(t *testing.T, ctx context.Context, dsn string, capability *capabilitycatalog.Service, capAddr string, runner *Runner, input *api.BuildInputSnapshot, identity *liveIdentity) {
	t.Helper()
	capWire := api.NewCapabilityProductServiceClient(liveConn(t, capAddr, owneridentity.ConsoleBFF))
	capClient := &publisherCapabilityClient{CapabilityProductServiceClient: capWire, t: t, base: newPublisherHTTP(t, capWire, nil, identity), identity: identity}
	schemaPathForWebui := "../../../../docs/spec/target/contracts/publisher-contract.schema.json"
	schemaData, err := os.ReadFile(schemaPathForWebui)
	if err != nil {
		t.Fatal(err)
	}
	if err = capability.ConfigurePublisherSchema(schemaPathForWebui, digest(schemaData)); err != nil {
		t.Fatal(err)
	}
	publisher, err := capClient.CreatePublisherNamespace(ctx, &api.CreatePublisherNamespaceRpcRequest{Context: identity.call("publisher-admit", "platform"), Body: &api.CreatePublisherNamespaceRequest{Name: "local-publisher", Kind: api.CreatePublisherNamespaceRequestKindEnum_CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_OFFICIAL, RegistryId: "local-registry", RepositoryPrefix: runner.RegistryPrefix[:len(runner.RegistryPrefix)-len("/result")], AdmissionReceiptId: "isolated-publisher-admission"}})
	if err != nil {
		t.Fatal(err)
	}
	// The real registry fixture uses runtime/webui/recipe repositories under one
	// authority. Admission reserves that exact prefix; no business tables are seeded.
	input.RuntimeContract.PublisherNamespaceId = publisher.Id
	input.WebuiContract.PublisherNamespaceId = publisher.Id
	webui, err := capClient.RegisterWebuiVersion(ctx, &api.RegisterWebuiVersionRpcRequest{Context: identity.call("webui-admit", "platform"), Body: &api.RegisterWebuiVersionRequest{Name: "local-webui", VersionLabel: "v1", PublisherNamespaceId: publisher.Id, PublisherContract: input.WebuiContract, AdmissionReceiptId: "local-webui-admission"}})
	if err != nil {
		t.Fatal(err)
	}
	input.WebuiVersionId = webui.Id
	forbidden := &api.CreatePublisherNamespaceRpcRequest{Context: identity.call("tenant-admin-cannot-admit", "tenant-live"), Body: &api.CreatePublisherNamespaceRequest{Name: "forged", Kind: api.CreatePublisherNamespaceRequestKindEnum_CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_OFFICIAL, RegistryId: "local-registry", RepositoryPrefix: publisher.RepositoryPrefix + "/forged", AdmissionReceiptId: "not-authority"}}
	if _, e := capClient.CreatePublisherNamespace(ctx, forbidden); e == nil {
		t.Fatal("tenant admin admitted a platform publisher")
	}
	forbidden.Context = identity.call("overlap-denied", "platform")
	forbidden.Body.Kind = api.CreatePublisherNamespaceRequestKindEnum_CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_THIRD_PARTY
	if _, e := capClient.CreatePublisherNamespace(ctx, forbidden); e == nil {
		t.Fatal("third-party prefix overlapped official authority")
	}
	wrongWebui := proto.Clone(input.WebuiContract).(*api.WebuiPublisherContract)
	wrongWebui.Image.Repository = "unapproved.example/foreign/image"
	if _, e := capClient.RegisterWebuiVersion(ctx, &api.RegisterWebuiVersionRpcRequest{Context: identity.call("foreign-image-denied", "platform"), Body: &api.RegisterWebuiVersionRequest{Name: "bad-ui", VersionLabel: "v1", PublisherNamespaceId: publisher.Id, PublisherContract: wrongWebui, AdmissionReceiptId: "not-sufficient"}}); e == nil {
		t.Fatal("out-of-prefix WebUI admitted")
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
	runtimeDB, _ := liveOwnerDB(t, ctx, dsn, "runtime_control", runtimeSource)
	runtimeService, err := runtimecatalog.New(runtimeDB, ownerservice.NewAuthorizer(ownerservice.OwnerRuntimeControl, identity.auth(t, owneridentity.RuntimeControl)), api.NewCapabilityProductServiceClient(liveConn(t, capAddr, owneridentity.RuntimeControl.Service())), schemaPath, digest(schemaBytes))
	if err != nil {
		t.Fatal(err)
	}
	runtimeAddr := liveServer(t, func(server *grpc.Server) { api.RegisterRuntimeControlProductServiceServer(server, runtimeService) })
	runtimeClient := api.NewRuntimeControlProductServiceClient(liveConn(t, runtimeAddr, owneridentity.ConsoleBFF))
	call := func(key string, platform bool) *api.CallContext {
		tid := "tenant-live"
		if platform {
			tid = "platform"
		}
		return identity.call(key, tid)
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
	buildDB, buildDSN := liveOwnerDB(t, ctx, dsn, "build", source)
	store, err := ownerstore.New(buildDB, "build")
	if err != nil {
		t.Fatal(err)
	}
	capConn := liveConn(t, capAddr, owneridentity.Build.Service())
	capDrop := &lostInboxAck{DomainInboxClient: api.NewDomainInboxClient(capConn)}
	ledgerDB, ledgerBuild, ledgerCapability := liveLedger(t, ctx)
	capability.LedgerInbox = ledgerCapability
	service, err := New(store, ownerservice.NewAuthorizer(ownerservice.OwnerBuild, identity.auth(t, owneridentity.Build)), api.NewCapabilityCoordinationClient(capConn), api.NewCapabilityProductServiceClient(capConn), map[string]api.DomainInboxClient{"capability": capDrop, "ledger": ledgerBuild}, identity.auth(t, owneridentity.Build), runner)
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
	identity.service.BuildCommit = api.NewOwnerCommitReadbackClient(liveConn(t, buildAddr, owneridentity.Tenant.Service()))
	client := &publisherBuildClient{t: t, base: newPublisherHTTP(t, api.NewCapabilityProductServiceClient(liveConn(t, capAddr, owneridentity.ConsoleBFF)), api.NewBuildProductServiceClient(liveConn(t, buildAddr, owneridentity.ConsoleBFF)), identity), identity: identity}
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
		if err = capability.DeliverRegistrations(ctx); err != nil {
			t.Fatal(err)
		}
		var buildPending, capPending int
		if err = buildDB.QueryRowContext(ctx, `SELECT count(*) FROM build.outbox_deliveries WHERE consumer_owner IN ('capability','ledger') AND acknowledged_at IS NULL`).Scan(&buildPending); err != nil {
			t.Fatal(err)
		}
		if err = capability.DB.QueryRowContext(ctx, `SELECT count(*) FROM capability.outbox_deliveries WHERE consumer_owner IN ('build','ledger') AND acknowledged_at IS NULL`).Scan(&capPending); err != nil {
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
			t.Fatalf("registration did not converge: state=%s pending=%d/%d ledger errors=%v / %v", rec.Job.Status, buildPending, capPending, ledgerBuild.lastError, ledgerCapability.lastError)
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
	verifyLedgerEvidence(t, ctx, ledgerDB, job.Id, ledgerBuild, ledgerCapability)
	verifyInterruptedWorker(t, ctx, buildDSN, capAddr, client, service, capability, runner, req)
	verifyPublisherBrowser(t, ctx, client.base, service, capability, runner, input)

	if _, e := capClient.SetWebuiVersionStatus(ctx, &api.SetWebuiVersionStatusRpcRequest{Context: identity.call("revoke-webui", "platform"), VersionId: webui.Id, Body: &api.CatalogStatusRequest{Status: api.CatalogStatusRequestStatusEnum_CATALOG_STATUS_REQUEST_STATUS_ENUM_REVOKED, Reason: "local negative qualification"}}); e != nil {
		t.Fatal(e)
	}
	if _, e := client.CreateBuild(ctx, &api.CreateBuildRpcRequest{Context: identity.call("revoked-webui-build", "tenant-live"), Body: req.Body}); e == nil {
		t.Fatal("new Build admitted revoked WebUI")
	}
	t.Log("real publisher/WebUI admission: platform role enforced, third-party prefix overlap and foreign image rejected, revoked WebUI blocks new Build")
	t.Logf("real owner chain: Runtime API admission, CreateBuild replay, three bound claims, one ready CapabilityVersion; both registration acknowledgements lost and recovered; artifact=%s", version.ArtifactDigest)
}
