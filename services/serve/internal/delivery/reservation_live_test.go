//go:build livebuild

package delivery_test

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net"
	"os"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	capability "opl-cloud/services/capability/catalog"
	capabilitymigrations "opl-cloud/services/capability/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/serve/internal/delivery"
)

// Workspace commitment and the ready Capability are explicit prerequisites for
// this bounded owner-authorization test. They do not claim BuildKit execution,
// Workspace money/resource completion, a provider mutation, or app readiness.
type workspaceCommitFixture struct {
	api.UnimplementedOwnerCommitReadbackServer
	evidence *api.OwnerCommitEvidence
}

func (f *workspaceCommitFixture) ReadOwnerCommit(_ context.Context, r *api.ReadOwnerCommitRequest) (*api.OwnerCommitEvidence, error) {
	if f.evidence == nil || r.Owner != f.evidence.Owner || r.ResourceId != f.evidence.ResourceId || r.OperationId != f.evidence.OperationId {
		return nil, status.Error(codes.NotFound, "fixture original commitment not found")
	}
	return proto.Clone(f.evidence).(*api.OwnerCommitEvidence), nil
}
func serveLiveOwner(t *testing.T, owner owneridentity.Owner, peers []owneridentity.Service, register func(*ownerservice.Server) error) string {
	t.Helper()
	config := ownerservice.Config{Owner: owner, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{}}
	for _, peer := range peers {
		config.Peers[peer] = liveIdentityToken
	}
	server, err := ownerservice.NewServer(config)
	if err != nil {
		t.Fatal(err)
	}
	if err = register(server); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)
	return listener.Addr().String()
}
func serveLiveConn(t *testing.T, address string, caller, target owneridentity.Service) *grpc.ClientConn {
	t.Helper()
	options, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(caller, target, liveIdentityToken)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}
func serveCapabilityDB(t *testing.T, ctx context.Context, dsn string) *sql.DB {
	t.Helper()
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "capability", Database: "opl_capability", SchemaOwnerRole: "opl_capability_owner", WriterRole: "opl_capability_writer", RuntimeRole: "opl_capability_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
	source, err := capabilitymigrations.Source()
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
func seedReadyCapabilityForClaim(t *testing.T, db *sql.DB) *api.CapabilityVersion {
	t.Helper()
	ctx := context.Background()
	digest := "sha256:" + strings.Repeat("1", 64)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO capability.namespaces(id,tenant_id,name,kind) VALUES('ns',$1,'live','tenant_default');`, liveTenant)
	exec(`INSERT INTO capability.packages(id,namespace_id,name,visibility,created_by) VALUES('package','ns','agent','private',$1)`, liveMemberActor)
	exec(`INSERT INTO capability.package_versions(id,package_id,version_label,sha256,size_bytes,created_by) VALUES('package-version','package','1',$1,1,$2)`, digest, liveMemberActor)
	exec(`INSERT INTO capability.publisher_namespaces(id,name,kind,registry_id,repository_prefix,admission_receipt_id) VALUES('publisher','publisher','official','registry.test','registry.test','publisher-fixture')`)
	publisher, _ := json.Marshal(map[string]any{"schemaVersion": "opl-publisher-contract/v1", "kind": "webui", "publisherNamespaceId": "publisher", "image": map[string]any{"repository": "registry.test/ui", "digest": digest, "platform": map[string]string{"os": "linux", "architecture": "amd64"}}, "runtimeAbiVersions": []string{"abi"}, "uiProtocolVersion": "ui"})
	exec(`INSERT INTO capability.webui_versions(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_versions,ui_protocol_version,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref) VALUES('ui','ui','1','registry.test/ui',$1,$2,ARRAY['abi'],'ui','ui-fixture','publisher',$1,$3,'ui-fixture-ref')`, digest, liveMemberActor, publisher)
	descriptor := deployCommand(t, "ws-live-reservation", "unused", 1).DeploymentDescriptor
	descriptor.PackageVersionId = proto.String("package-version")
	descriptor.RuntimeContractReference = &api.PublisherContractReference{VersionId: "runtime", Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_RUNTIME, PublisherNamespaceId: "publisher", DescriptorDigest: digest, DescriptorObjectRef: "runtime-fixture"}
	descriptor.WebuiContractReference = &api.PublisherContractReference{VersionId: "ui", Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_WEBUI, PublisherNamespaceId: "publisher", DescriptorDigest: digest, DescriptorObjectRef: "ui-fixture"}
	raw, err := publicjson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	descriptorDigest := "sha256:" + hex.EncodeToString(sum[:])
	readback := &api.BuildArtifactReadback{BuildJobId: "build-fixture", Input: &api.BuildInputSnapshot{PackageId: "package", PackageVersionId: "package-version", RuntimeVersionId: "runtime", WebuiVersionId: "ui"}, Artifact: descriptor.Artifact, ArtifactReceiptId: "build-fixture-receipt", VersionLabel: "1", DataCompatibility: &api.DataCompatibility{DataSchemaVersion: "1"}, Outcome: api.Observation_OBSERVATION_CONFIRMED, DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: descriptorDigest, DeploymentDescriptorObjectRef: "descriptor-fixture"}
	evidence, _ := protojson.Marshal(readback)
	exec(`INSERT INTO capability.capability_versions(id,package_id,package_version_id,build_job_id,version_label,runtime_version_id,webui_version_id,artifact_repository,artifact_digest,status,model_requirements,data_compatibility,provenance_evidence,provenance,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref) VALUES('capability-live','package','package-version','build-fixture','1','runtime','ui','registry.test/app',$1,'ready','[]','{"dataSchemaVersion":"1"}',$2,'build',$3,$4,'descriptor-fixture')`, digest, evidence, raw, descriptorDigest)
	return &api.CapabilityVersion{Id: "capability-live", Artifact: descriptor.Artifact, DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: descriptorDigest, DeploymentDescriptorObjectRef: "descriptor-fixture"}
}
func TestLiveServeReservationUsesWorkspaceGrantAndCapabilityClaim(t *testing.T) {
	ctx := context.Background()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	identity := newLiveIdentityChain(t, ctx, dsn)
	workspace := &workspaceCommitFixture{}
	workspaceAddr := serveLiveOwner(t, owneridentity.Workspace, []owneridentity.Service{owneridentity.Tenant.Service()}, func(s *ownerservice.Server) error {
		return s.Register(func(g *grpc.Server) { api.RegisterOwnerCommitReadbackServer(g, workspace) })
	})
	identity.service.WorkspaceCommit = api.NewOwnerCommitReadbackClient(serveLiveConn(t, workspaceAddr, owneridentity.Tenant.Service(), owneridentity.Workspace.Service()))
	db := serveCapabilityDB(t, ctx, dsn)
	version := seedReadyCapabilityForClaim(t, db)
	capAuth := ownerservice.NewAuthorizer(owneridentity.Capability, api.NewCloudIdentityAuthorizationClient(serveLiveConn(t, identity.address, owneridentity.Capability.Service(), owneridentity.Tenant.Service())))
	capService := &capability.Service{DB: db, Authorize: capAuth.Authorize}
	capAddress := serveLiveOwner(t, owneridentity.Capability, []owneridentity.Service{owneridentity.Serve.Service()}, capService.Register)
	serveDB, _, _ := fixture(t)
	serveAuth := ownerservice.NewAuthorizer(owneridentity.Serve, api.NewCloudIdentityAuthorizationClient(serveLiveConn(t, identity.address, owneridentity.Serve.Service(), owneridentity.Tenant.Service())))
	service, err := delivery.New(serveDB, serveAuth.Authorize)
	if err != nil {
		t.Fatal(err)
	}
	capConn := serveLiveConn(t, capAddress, owneridentity.Serve.Service(), owneridentity.Capability.Service())
	service.Capability = api.NewCapabilityProductServiceClient(capConn)
	service.References = api.NewCapabilityCoordinationClient(capConn)
	serveAddress := serveLiveOwner(t, owneridentity.Serve, []owneridentity.Service{owneridentity.Workspace.Service(), owneridentity.Capability.Service()}, service.Register)
	capService.ServeCommit = api.NewOwnerCommitReadbackClient(serveLiveConn(t, serveAddress, owneridentity.Capability.Service(), owneridentity.Serve.Service()))
	capService.ServeUsage = api.NewClaimUsageReadbackClient(serveLiveConn(t, serveAddress, owneridentity.Capability.Service(), owneridentity.Serve.Service()))
	workspaceIdentity := api.NewCloudIdentityAuthorizationClient(serveLiveConn(t, identity.address, owneridentity.Workspace.Service(), owneridentity.Tenant.Service()))
	original := bffReadCall(t, ctx, identity, "serve", "accept-workspace")
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE}
	decision, err := workspaceIdentity.AuthorizeAction(ctx, &api.AuthorizationRequest{ActorId: original.ActorId, SessionId: original.SessionId, Scope: original.Scope, AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE, Resource: resource, RequestId: original.RequestId})
	if err != nil {
		t.Fatal(err)
	}
	workspace.evidence = &api.OwnerCommitEvidence{Owner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, OperationId: "workspace-operation-original", ResourceId: "ws-live-reservation", AcceptedInputDigest: "sha256:" + strings.Repeat("2", 64), CommittedVersion: 1, AcceptedAt: timestamppb.Now(), AuthorizationContextId: decision.GetAuthorizationContextId(), ActorId: original.ActorId, Scope: original.Scope, AcceptedAction: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE, AuthorizationResource: resource, ContinuationResources: []*api.AuthorizationResource{{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, Id: proto.String(version.Id)}}}
	grant, err := workspaceIdentity.IssueAcceptedOperationGrant(ctx, &api.AcceptedOperationGrantRequest{AuthorizationContextId: decision.GetAuthorizationContextId(), OwnerCommitEvidence: workspace.evidence, AllowedActions: []api.AuthorizationActionEnum{api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESERVERUNTIME, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDREFERENCE}})
	if err != nil {
		t.Fatal(err)
	}
	caller := &api.CallContext{RequestId: "reserve-original", ActorId: original.ActorId, Scope: original.Scope, AcceptedOperationGrantId: proto.String(grant.Id), IdempotencyKey: "reservation-original"}
	request := &api.RuntimeReservationCommand{Context: caller, WorkspaceId: workspace.evidence.ResourceId, CapabilityVersionId: version.Id, Artifact: version.Artifact, DeploymentDescriptor: version.DeploymentDescriptor, DeploymentDescriptorDigest: version.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: version.DeploymentDescriptorObjectRef, ResourceSetId: "resources-prerequisite", DataAttachmentId: "attachment-prerequisite"}
	client := api.NewServeAgentCoordinationClient(serveLiveConn(t, serveAddress, owneridentity.Workspace.Service(), owneridentity.Serve.Service()))
	reservation, err := client.Reserve(ctx, request)
	if err != nil {
		t.Fatalf("real Reserve/Capability claim chain: %v", err)
	}
	replay, err := client.Reserve(ctx, request)
	if err != nil || !proto.Equal(reservation, replay) {
		t.Fatalf("original reservation replay: %v %v", replay, err)
	}
	var count int
	var purpose, claimant, operation, digest string
	err = db.QueryRowContext(ctx, `SELECT count(*) FROM capability.reference_claims WHERE claimant_owner='serve' AND claimant_resource_id=$1`, reservation.DeploymentId).Scan(&count)
	if err != nil || count != 1 {
		t.Fatalf("claims=%d %v", count, err)
	}
	err = db.QueryRowContext(ctx, `SELECT purpose,claimant_owner,bound_operation_id,bound_input_digest FROM capability.reference_claims WHERE claimant_resource_id=$1`, reservation.DeploymentId).Scan(&purpose, &claimant, &operation, &digest)
	if err != nil || purpose != "deploy" || claimant != "serve" || operation != reservation.OperationId || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("bound original claim=%s %s %s %s %v", purpose, claimant, operation, digest, err)
	}
	capVersionClient := api.NewCapabilityProductServiceClient(capConn)
	if _, err = capVersionClient.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: caller, CapabilityVersionId: version.Id}); err != nil {
		t.Fatalf("accepted version authority: %v", err)
	}
	if _, err = capVersionClient.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: caller, CapabilityVersionId: "unaccepted-capability"}); err == nil {
		t.Fatal("grant admitted unaccepted Capability")
	}
	wrong := proto.Clone(request).(*api.RuntimeReservationCommand)
	wrong.WorkspaceId = "foreign-workspace"
	if _, err = client.Reserve(ctx, wrong); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("grant escaped workspace: %v", err)
	}
	wrong = proto.Clone(request).(*api.RuntimeReservationCommand)
	wrong.CapabilityVersionId = "unaccepted-capability"
	wrong.Context.IdempotencyKey = "different-key"
	if _, err = client.Reserve(ctx, wrong); err == nil {
		t.Fatal("unaccepted capability admitted")
	}
	if _, err = identity.db.ExecContext(ctx, `UPDATE tenant.tenant_members SET revoked_at=now() WHERE tenant_id=$1 AND actor_id=$2`, liveTenant, liveMemberActor); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Reserve(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("revoked member continued mutating: %v", err)
	}
	t.Log("real CloudIdentity Workspace grant -> Serve Reserve -> Capability acquire/bind -> Serve owner commit readback; exact replay and foreign/revoked denials passed; Build/Workspace facts are declared fixtures, no provider executed")
}
