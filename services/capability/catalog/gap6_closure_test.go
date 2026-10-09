package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http/httptest"
	"os"
	"strings"
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
	"opl-cloud/services/internal/ownerstore"
)

const gapPublisherSchema = "../../../docs/spec/target/contracts/publisher-contract.schema.json"

// gapExamples decodes the approved publisher-contract schema examples instead of
// hand-writing contract JSON, so a fixture cannot drift from the schema the owner
// actually validates against.
func gapExamples(t *testing.T) []json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(gapPublisherSchema)
	if err != nil {
		t.Fatal(err)
	}
	var document struct{ Examples []json.RawMessage }
	if err = json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Examples) < 2 {
		t.Fatalf("publisher schema examples=%d", len(document.Examples))
	}
	return document.Examples
}

func gapRuntimeContract(t *testing.T) *api.RuntimePublisherContract {
	t.Helper()
	contract := &api.RuntimePublisherContract{}
	if err := publicjson.Unmarshal(gapExamples(t)[0], contract); err != nil {
		t.Fatal(err)
	}
	return contract
}

func gapWebuiContract(t *testing.T) *api.WebuiPublisherContract {
	t.Helper()
	contract := &api.WebuiPublisherContract{}
	if err := publicjson.Unmarshal(gapExamples(t)[1], contract); err != nil {
		t.Fatal(err)
	}
	return contract
}

// gapReadback is the Build/Runtime owner readback Capability registers against.
type gapReadback struct {
	api.BuildCoordinationClient
	api.RuntimeControlProductServiceClient
	artifact *api.BuildArtifactReadback
	runtime  *api.RuntimeVersion
}

func (g *gapReadback) ReadArtifact(context.Context, *api.ReadBuildArtifactRequest, ...grpc.CallOption) (*api.BuildArtifactReadback, error) {
	return g.artifact, nil
}

func (g *gapReadback) ListRuntimeVersions(context.Context, *api.ListRuntimeVersionsRpcRequest, ...grpc.CallOption) (*api.RuntimeVersionPage, error) {
	return &api.RuntimeVersionPage{Items: []*api.RuntimeVersion{g.runtime}}, nil
}

func gapTenantCall(tenant, key string) *api.CallContext {
	return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, SessionId: proto.String("session"), IdempotencyKey: key, Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
}

func gapPlatformCall(key string) *api.CallContext {
	return &api.CallContext{ActorId: "actor", RequestId: "request-" + key, SessionId: proto.String("session"), IdempotencyKey: key, Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}}
}

// gapOwner is the real Capability owner on a real isolated database.
type gapOwner struct {
	db      *sql.DB
	service *Service
	ctx     context.Context
}

func gapNewOwner(t *testing.T) *gapOwner {
	t.Helper()
	db := capabilityClaimDB(t)
	store, err := ownerstore.New(db, "capability")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{DB: db, Store: store, Authorize: func(context.Context, *api.CallContext, api.AuthorizationActionEnum, *api.AuthorizationResource, ownerservice.ResourceScope) error {
		return nil
	}}
	if err = service.ConfigurePublisherSchema(gapPublisherSchema, digestOfFile(t, gapPublisherSchema)); err != nil {
		t.Fatal(err)
	}
	return &gapOwner{db: db, service: service, ctx: ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)}
}

func digestOfFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return digest(raw)
}

// uploadedPackage drives the owner's own upload API over a direct-to-storage
// provider so the PackageVersion digest and immutable object reference are
// produced by Capability rather than asserted by the test.
func (o *gapOwner) uploadedPackage(t *testing.T, tenant, namespace, pkg string, archive []byte) (string, string) {
	t.Helper()
	server := httptest.NewTLSServer(newFakeCOS())
	t.Cleanup(server.Close)
	o.service.Objects = gapObjects(t, server)
	if _, err := o.db.ExecContext(o.ctx, `INSERT INTO capability.namespaces(id,tenant_id,name,kind) VALUES($1,$2,$1,'tenant_default')`, namespace, tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := o.db.ExecContext(o.ctx, `INSERT INTO capability.packages(id,namespace_id,name,visibility,created_by) VALUES($1,$2,$1,'private','actor')`, pkg, namespace); err != nil {
		t.Fatal(err)
	}
	session, parts, authorizations := startDirectUpload(t, o.service, o.ctx, pkg, tenant, "gap-upload", archive)
	for index, part := range parts {
		putShard(t, server.Client(), authorizations[index], archive, part, session.PartSizeBytes)
	}
	operation, err := o.service.CompleteUpload(o.ctx, &api.CompleteUploadRpcRequest{Context: gapTenantCall(tenant, "gap-upload"), UploadId: session.Id, Body: &api.CompleteUploadRequest{Parts: parts}})
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("upload operation=%v", operation)
	}
	return session.PackageVersionId, digest(archive)
}

// seedWebui admits a WebUI through the owner's own admission API and applies the
// requested lifecycle status, so approval is a real owner row.
func (o *gapOwner) seedWebui(t *testing.T, contract *api.WebuiPublisherContract, status string) string {
	t.Helper()
	key := "webui-" + strings.ReplaceAll(contract.GetImage().GetRepository(), "/", "-")
	label := strings.TrimPrefix(strings.ReplaceAll(contract.GetImage().GetRepository(), "/", "-"), "registry.example-official-")
	admitted, err := o.service.RegisterWebuiVersion(o.ctx, &api.RegisterWebuiVersionRpcRequest{Context: gapPlatformCall(key), Body: &api.RegisterWebuiVersionRequest{Name: key, VersionLabel: "1", PublisherNamespaceId: contract.PublisherNamespaceId, PublisherContract: contract, AdmissionReceiptId: "gap-fixture-receipt-not-an-authority"}})
	if err != nil {
		t.Fatal(err)
	}
	if status != "approved" {
		_ = label
		requested := api.CatalogStatusRequestStatusEnum(api.CatalogStatusRequestStatusEnum_value["CATALOG_STATUS_REQUEST_STATUS_ENUM_"+strings.ToUpper(status)])
		if _, err = o.service.SetWebuiVersionStatus(o.ctx, &api.SetWebuiVersionStatusRpcRequest{Context: gapPlatformCall(key + "-status"), VersionId: admitted.Id, Body: &api.CatalogStatusRequest{Status: requested, Reason: "gap negative case"}}); err != nil {
			t.Fatal(err)
		}
	}
	return admitted.Id
}

const gapSchemaBody = `{"type":"object"}`

func gapSchemaPath(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/package-schema.json"
	if err := os.WriteFile(path, []byte(gapSchemaBody), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// gapObjects binds the real Capability upload policy to the disposable
// direct-to-storage provider fixture.
func gapObjects(t *testing.T, server *httptest.Server) *Objects {
	t.Helper()
	objects, err := NewObjectsWithStorage(fakeCOSStorage(t, server), nil, UploadPolicy{MaxBytes: 1 << 20, PartBytes: 64, MaxExpandedBytes: 2 << 20, MaxFiles: 128, TTL: time.Hour, ManifestPath: "manifest.json", SchemaPath: gapSchemaPath(t), SchemaDigest: digest([]byte(gapSchemaBody))})
	if err != nil {
		t.Fatal(err)
	}
	return objects
}

// gapSnapshot assembles one frozen three-input snapshot exactly as the owner
// freezes it, so a registration test can vary one fact at a time.
func (o *gapOwner) gapSnapshot(t *testing.T, runtime *api.RuntimeVersion, contract *api.RuntimePublisherContract, webuiID string, webuiContract *api.WebuiPublisherContract, packageID, packageVersionID string, size int64, sha string) *api.BuildInputSnapshot {
	t.Helper()
	in := &api.BuildInputSnapshot{
		PackageId:                packageID,
		PackageVersionId:         packageVersionID,
		PackageObject:            &api.SourceObjectReference{StorageObjectId: sha, VersionId: sha, Sha256: sha, SizeBytes: size},
		RuntimeVersionId:         runtime.GetId(),
		RuntimeArtifact:          contract.GetImage(),
		WebuiVersionId:           webuiID,
		WebuiArtifact:            webuiContract.GetImage(),
		RuntimeContract:          contract,
		WebuiContract:            webuiContract,
		RuntimeContractReference: &api.PublisherContractReference{PublisherNamespaceId: contract.GetPublisherNamespaceId(), VersionId: runtime.GetId(), Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_RUNTIME, DescriptorDigest: runtime.GetPublisherContractDigest(), DescriptorObjectRef: "runtime-contract:" + runtime.GetId() + "@" + runtime.GetPublisherContractDigest()},
		WebuiContractReference:   &api.PublisherContractReference{PublisherNamespaceId: webuiContract.GetPublisherNamespaceId(), VersionId: webuiID, Kind: api.PublisherContractReferenceKindEnum_PUBLISHER_CONTRACT_REFERENCE_KIND_ENUM_WEBUI, DescriptorDigest: digestMust(t, webuiContract), DescriptorObjectRef: "webui-contract:" + webuiID + "@" + digestMust(t, webuiContract)},
	}
	snapshot, err := buildInputDigest(in)
	if err != nil {
		t.Fatal(err)
	}
	in.SnapshotDigest = snapshot
	return in
}

func digestMust(t *testing.T, m proto.Message) string {
	t.Helper()
	v, err := canonicalContractDigest(m)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// gapSeedInputs creates the tenant Package/WebUI rows and the admitted publisher
// the combination checks read, plus a bound claim per input at this snapshot
// digest, exactly as the Build owner's committed operation would have left them.
func (o *gapOwner) gapSeedInputs(t *testing.T, job, tenant, namespace, pkg, packageVersionID, webuiID, runtimeID string, in *api.BuildInputSnapshot) {
	t.Helper()
	bind := func(job, claimID, kind, target string) {
		var query string
		switch kind {
		case "package_version":
			query = `INSERT INTO capability.reference_claims(id,target_type,package_version_id,claimant_owner,claimant_resource_id,purpose,request_id,bound_at,bound_operation_id,bound_input_digest) VALUES($1,'package_version',$2,'build',$3,'build',$4,now(),$5,$6)`
		case "runtime_version":
			query = `INSERT INTO capability.reference_claims(id,target_type,runtime_version_id,claimant_owner,claimant_resource_id,purpose,request_id,bound_at,bound_operation_id,bound_input_digest) VALUES($1,'runtime_version',$2,'build',$3,'build',$4,now(),$5,$6)`
		default:
			query = `INSERT INTO capability.reference_claims(id,target_type,webui_version_id,claimant_owner,claimant_resource_id,purpose,request_id,bound_at,bound_operation_id,bound_input_digest) VALUES($1,'webui_version',$2,'build',$3,'build',$4,now(),$5,$6)`
		}
		if _, err := o.db.ExecContext(o.ctx, query, claimID, target, job, "request-"+job, "op-"+job, in.GetSnapshotDigest()); err != nil {
			t.Fatal(err)
		}
	}
	bind(job, "gap-claim-package-"+job, "package_version", in.GetPackageVersionId())
	bind(job, "gap-claim-runtime-"+job, "runtime_version", runtimeID)
	bind(job, "gap-claim-webui-"+job, "webui_version", webuiID)
	in.PackageClaimId = "gap-claim-package-" + job
	in.RuntimeClaimId = "gap-claim-runtime-" + job
	in.WebuiClaimId = "gap-claim-webui-" + job
	_ = tenant
	_ = namespace
	_ = pkg
	_ = packageVersionID
	// The Build owner acquires and binds the three claims after the freeze, so the
	// claims carry the frozen digest while the persisted snapshot message later
	// also carries the claim IDs. Re-derive the identity exactly as
	// validateFrozenInput does; the claim bookkeeping must not change it.
	derived, err := buildInputDigest(in)
	if err != nil {
		t.Fatal(err)
	}
	if derived != in.GetSnapshotDigest() {
		t.Fatalf("claim binding changed the frozen digest: %s want %s", derived, in.GetSnapshotDigest())
	}
}

// gapDeliver drives the registration event through the owner's real inbox path.
func (o *gapOwner) gapDeliver(t *testing.T, job string, in *api.BuildInputSnapshot, descriptor *api.DeploymentDescriptor, artifact *api.ArtifactReference, runtime *api.RuntimeVersion, eventID string, mutate func(*api.BuildArtifactReadback)) (*api.InboxAck, error) {
	t.Helper()
	descriptorBytes, err := publicjson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	readback := &api.BuildArtifactReadback{
		BuildJobId:                    job,
		Input:                         in,
		Artifact:                      artifact,
		ArtifactReceiptId:             "artifact_" + job,
		VersionLabel:                  job,
		Outcome:                       api.Observation_OBSERVATION_CONFIRMED,
		DeploymentDescriptor:          descriptor,
		DeploymentDescriptorDigest:    digest(descriptorBytes),
		DeploymentDescriptorObjectRef: "build-artifact://artifact_" + job + "/descriptor@" + digest(descriptorBytes),
		DataCompatibility:             &api.DataCompatibility{DataSchemaVersion: "agent-data/v1"},
	}
	if mutate != nil {
		mutate(readback)
	}
	o.service.Build = &gapReadback{artifact: readback}
	o.service.Runtime = &gapReadback{runtime: runtime}
	payload := &api.BuildArtifactConfirmedEvent{BuildJobId: job, PackageVersionId: in.GetPackageVersionId(), RuntimeVersionId: in.GetRuntimeVersionId(), WebuiVersionId: in.GetWebuiVersionId(), ArtifactDigest: artifact.GetDigest(), ArtifactReceiptId: readback.GetArtifactReceiptId(), DeploymentDescriptorDigest: readback.GetDeploymentDescriptorDigest()}
	event := &api.EventEnvelope{EventId: eventID, EventType: "build.artifact_confirmed.v1", SchemaVersion: 1, Owner: "build", TenantId: "tenant-gap", Scope: "tenant", AggregateId: job, AggregateVersion: 1, RequestId: "request-1", Payload: &api.EventEnvelope_BuildArtifactConfirmed{BuildArtifactConfirmed: payload}}
	ctx := ownerservice.WithPeerOwner(context.Background(), owneridentity.Build.Service())
	return o.service.Deliver(ctx, &api.DeliverEventRequest{Event: event, AuthenticatedProducer: "build"})
}

// gapRegisteredVersionCount reads the owner's own single-writer table.
func (o *gapOwner) registeredVersionCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := o.db.QueryRowContext(o.ctx, `SELECT count(*) FROM capability.capability_versions`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// gapRuntimeVersion registers one Runtime Release through the Runtime owner's own
// admission rules is not available here; the Runtime readback is the owner's
// typed response, so the test supplies the approved record the Capability owner
// would have read, and the DB row proves approval is re-read for the WebUI.
func gapApprovedRuntime(t *testing.T, contract *api.RuntimePublisherContract, id string) *api.RuntimeVersion {
	t.Helper()
	canonical, err := canonicalContractDigest(contract)
	if err != nil {
		t.Fatal(err)
	}
	return &api.RuntimeVersion{Id: id, Status: api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED, PublisherContract: contract, ArtifactDigest: contract.GetImage().GetDigest(), RuntimeAbiVersion: contract.GetRuntimeAbiVersion(), PublisherNamespaceId: contract.GetPublisherNamespaceId(), PublisherContractDigest: canonical, PublisherContractObjectRef: "runtime-contract:" + id + "@" + canonical}
}

func gapDescriptorFor(in *api.BuildInputSnapshot, artifact *api.ArtifactReference) *api.DeploymentDescriptor {
	packageVersion := in.GetPackageVersionId()
	inputDigest := in.GetSnapshotDigest()
	revision := proto.Clone(in.GetRuntimeContract().GetApplicationRevisionTemplate()).(*api.WorkspaceApplicationRevision)
	revision.Image = artifact.GetRepository() + "@" + artifact.GetDigest()
	return &api.DeploymentDescriptor{SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Artifact: artifact, RuntimeContract: in.GetRuntimeContract(), RuntimeContractReference: in.GetRuntimeContractReference(), WebuiContract: in.GetWebuiContract(), WebuiContractReference: in.GetWebuiContractReference(), PackageVersionId: &packageVersion, BuildInputDigest: &inputDigest, Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD, ApplicationRevision: revision}
}

func gapArtifact(repository string, platform *api.ImagePlatform) *api.ArtifactReference {
	return &api.ArtifactReference{Repository: repository, Digest: gapArtifactDigest, Platform: platform}
}

// TestGap6ThreeInputCombinationRejectsIncompatibleAbi reproduces GAP-6(c): an
// approved Runtime Release and an approved WebUI whose declared ABI sets do not
// intersect must be refused by the owner before any snapshot is frozen.
func TestGap6ThreeInputCombinationRejectsIncompatibleAbi(t *testing.T) {
	runtime := gapRuntimeContract(t)
	webui := gapWebuiContract(t)
	webui.RuntimeAbiVersions = []string{"opl-runtime/v9"}
	if err := validateBuildCombination(runtime, webui); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("individually approved but ABI-incompatible combination=%v", err)
	}
	webui.RuntimeAbiVersions = []string{runtime.GetRuntimeAbiVersion()}
	if err := validateBuildCombination(runtime, webui); err != nil {
		t.Fatalf("compatible combination=%v", err)
	}
}

// TestGap6ThreeInputCombinationRejectsUnapprovedPackageFormat reproduces the
// second compatibility axis: the Runtime Build recipe must select a Package
// format the Runtime Release itself approves.
func TestGap6ThreeInputCombinationRejectsUnapprovedPackageFormat(t *testing.T) {
	runtime := gapRuntimeContract(t)
	webui := gapWebuiContract(t)
	runtime.BuildRecipe.PackageInput.FormatVersion = "unapproved-format"
	if err := validateBuildCombination(runtime, webui); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("recipe selecting an unapproved Package format=%v", err)
	}
	runtime.BuildRecipe.PackageInput.FormatVersion = runtime.GetPackageFormatVersions()[0]
	if err := validateBuildCombination(runtime, webui); err != nil {
		t.Fatalf("approved Package format=%v", err)
	}
}

// TestGap6ThreeInputCombinationRejectsMissingBuildRecipe pins the boundary the
// custom Agent Build consumer keeps: even an individually approved Runtime
// Release whose publisher contract carries no Build recipe is incomplete state,
// so the Capability owner must refuse the combination instead of freezing a
// snapshot against a recipe it never declared.
func TestGap6ThreeInputCombinationRejectsMissingBuildRecipe(t *testing.T) {
	runtime := gapRuntimeContract(t)
	webui := gapWebuiContract(t)
	if len(runtime.GetPackageFormatVersions()) == 0 {
		t.Fatal("fixture precondition: Runtime contract must declare Package format versions")
	}
	runtime.BuildRecipe = nil
	if err := validateBuildCombination(runtime, webui); status.Code(err) != codes.DataLoss {
		t.Fatalf("Runtime contract missing Build recipe=%v", err)
	}
}

// TestGap6ThreeInputCombinationRejectsMissingPackageFormats pins the other half
// of the same boundary: a Runtime Release that keeps its Build recipe but
// declares no approved Package format must be refused as a failed precondition,
// never silently accepted as a custom Agent Build combination.
func TestGap6ThreeInputCombinationRejectsMissingPackageFormats(t *testing.T) {
	runtime := gapRuntimeContract(t)
	webui := gapWebuiContract(t)
	if runtime.GetBuildRecipe() == nil {
		t.Fatal("fixture precondition: Runtime contract must declare a Build recipe")
	}
	runtime.PackageFormatVersions = nil
	if err := validateBuildCombination(runtime, webui); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Runtime contract missing Package format versions (nil)=%v", err)
	}
	runtime.PackageFormatVersions = []string{}
	if err := validateBuildCombination(runtime, webui); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("Runtime contract missing Package format versions (empty)=%v", err)
	}
}

// TestGap6RegistrationRefusesUnapprovedWebui reproduces GAP-6(b) on the single
// READY writer: a Runtime Release or WebUI that was approved when the Build
// started and revoked before registration must not become a ready
// CapabilityVersion, and a revoked independent input must not be promotable.
func TestGap6RegistrationRefusesUnapprovedWebui(t *testing.T) {
	o := gapNewOwner(t)
	o.seedPublisher(t)
	runtimeContract := gapRuntimeContract(t)
	webuiContract := gapWebuiContract(t)
	archive := candidateZIPBytes(t)
	packageVersionID, sha := o.uploadedPackage(t, "tenant-gap", "gap-ns", "gap-pkg", archive)
	webuiID := o.seedWebui(t, webuiContract, "approved")
	runtime := gapApprovedRuntime(t, runtimeContract, "runtime-gap")
	platform := runtimeContract.GetImage().GetPlatform()
	artifact := gapArtifact("registry.example/official/agent", platform)

	// Positive: the approved, compatible combination registers exactly one ready
	// version through the single writer.
	first := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "gap-job-1", "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, webuiID, runtime.GetId(), first)
	ack, err := o.gapDeliver(t, "gap-job-1", first, gapDescriptorFor(first, artifact), artifact, runtime, "event-gap-1", nil)
	if err != nil {
		t.Fatalf("approved three-input registration=%v", err)
	}
	if !ack.GetCommitted() || o.registeredVersionCount(t) != 1 {
		t.Fatalf("ack=%v versions=%d", ack, o.registeredVersionCount(t))
	}

	// Negative: revoke the WebUI, then attempt an independent second registration
	// with a different Build job and event identity. The owner must refuse on the
	// revoked input, not merely on a duplicate identity.
	if _, err = o.service.SetWebuiVersionStatus(o.ctx, &api.SetWebuiVersionStatusRpcRequest{Context: gapPlatformCall("webui-revoke"), VersionId: webuiID, Body: &api.CatalogStatusRequest{Status: api.CatalogStatusRequestStatusEnum_CATALOG_STATUS_REQUEST_STATUS_ENUM_REVOKED, Reason: "gap negative case"}}); err != nil {
		t.Fatal(err)
	}
	second := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "gap-job-2", "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, webuiID, runtime.GetId(), second)
	ack, err = o.gapDeliver(t, "gap-job-2", second, gapDescriptorFor(second, artifact), artifact, runtime, "event-gap-2", nil)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("registration with a revoked WebUI=%v (versions=%d)", err, o.registeredVersionCount(t))
	}
	if ack != nil {
		t.Fatalf("refused registration returned an ack: %v", ack)
	}
	if o.registeredVersionCount(t) != 1 {
		t.Fatalf("refused registration wrote another ready version: %d", o.registeredVersionCount(t))
	}

	// Negative: an unevaluated WebUI (never approved, not merely revoked) is
	// likewise refused.
	otherContract := gapWebuiContract(t)
	otherContract.Image.Repository = "registry.example/official/other-webui"
	pending := o.seedWebui(t, otherContract, "deprecated")
	third := o.gapSnapshot(t, runtime, runtimeContract, pending, otherContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "gap-job-3", "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, pending, runtime.GetId(), third)
	if _, err = o.gapDeliver(t, "gap-job-3", third, gapDescriptorFor(third, artifact), artifact, runtime, "event-gap-3", nil); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("registration with a non-approved WebUI=%v", err)
	}
	if o.registeredVersionCount(t) != 1 {
		t.Fatalf("non-approved registration wrote another ready version: %d", o.registeredVersionCount(t))
	}
}

// TestGap6RegistrationRefusesUnapprovedRuntime is the Runtime-side mirror: the
// Runtime Release is re-read from the Runtime owner at registration and a release
// that is no longer approved must be refused even though the snapshot is intact.
func TestGap6RegistrationRefusesUnapprovedRuntime(t *testing.T) {
	o := gapNewOwner(t)
	o.seedPublisher(t)
	runtimeContract := gapRuntimeContract(t)
	webuiContract := gapWebuiContract(t)
	archive := candidateZIPBytes(t)
	packageVersionID, sha := o.uploadedPackage(t, "tenant-gap", "gap-ns", "gap-pkg", archive)
	webuiID := o.seedWebui(t, webuiContract, "approved")
	approved := gapApprovedRuntime(t, runtimeContract, "runtime-gap")
	revoked := proto.Clone(approved).(*api.RuntimeVersion)
	revoked.Status = api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_REVOKED
	platform := runtimeContract.GetImage().GetPlatform()
	artifact := gapArtifact("registry.example/official/agent", platform)

	in := o.gapSnapshot(t, approved, runtimeContract, webuiID, webuiContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "gap-job-1", "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, webuiID, approved.GetId(), in)
	if _, err := o.gapDeliver(t, "gap-job-1", in, gapDescriptorFor(in, artifact), artifact, revoked, "event-gap-4", nil); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("registration with a revoked Runtime Release=%v", err)
	}
	if o.registeredVersionCount(t) != 0 {
		t.Fatalf("revoked Runtime Release produced %d ready versions", o.registeredVersionCount(t))
	}
}

// TestGap6RegistrationRefusesEditedSnapshot reproduces GAP-6(e): the uploaded
// Package identity/digest must survive end-to-end. An edited snapshot whose
// Package object bytes differ from the uploaded object, or whose digest no longer
// describes its own bytes, must be refused by the owner.
func TestGap6RegistrationRefusesEditedSnapshot(t *testing.T) {
	o := gapNewOwner(t)
	o.seedPublisher(t)
	runtimeContract := gapRuntimeContract(t)
	webuiContract := gapWebuiContract(t)
	archive := candidateZIPBytes(t)
	packageVersionID, sha := o.uploadedPackage(t, "tenant-gap", "gap-ns", "gap-pkg", archive)
	webuiID := o.seedWebui(t, webuiContract, "approved")
	runtime := gapApprovedRuntime(t, runtimeContract, "runtime-gap")
	platform := runtimeContract.GetImage().GetPlatform()
	artifact := gapArtifact("registry.example/official/agent", platform)

	// A Package digest that differs from the uploaded object is refused.
	swapped := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "gap-job-5", "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, webuiID, runtime.GetId(), swapped)
	swapped.PackageObject.Sha256 = "sha256:" + strings.Repeat("c", 64)
	if _, err := o.gapDeliver(t, "gap-job-5", swapped, gapDescriptorFor(swapped, artifact), artifact, runtime, "event-gap-5", nil); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("snapshot naming different Package bytes=%v", err)
	}

	// A digest that no longer describes the snapshot bytes is refused.
	forged := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "gap-job-6", "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, webuiID, runtime.GetId(), forged)
	forged.RuntimeArtifact = proto.Clone(forged.RuntimeArtifact).(*api.ArtifactReference)
	forged.RuntimeArtifact.Digest = "sha256:" + strings.Repeat("d", 64)
	if _, err := o.gapDeliver(t, "gap-job-6", forged, gapDescriptorFor(forged, artifact), artifact, runtime, "event-gap-6", nil); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("snapshot whose digest does not describe its bytes=%v", err)
	}

	if o.registeredVersionCount(t) != 0 {
		t.Fatalf("edited snapshots produced %d ready versions", o.registeredVersionCount(t))
	}
}

// TestGap6RegistrationRefusesClaimBoundToDifferentDigest pins the binding axis:
// the frozen snapshot must be claim-bound at exactly its own digest, so a claim
// that was bound to another build's input cannot register this snapshot even
// when every other fact is intact.
func TestGap6RegistrationRefusesClaimBoundToDifferentDigest(t *testing.T) {
	o := gapNewOwner(t)
	o.seedPublisher(t)
	runtimeContract := gapRuntimeContract(t)
	webuiContract := gapWebuiContract(t)
	archive := candidateZIPBytes(t)
	packageVersionID, sha := o.uploadedPackage(t, "tenant-gap", "gap-ns", "gap-pkg", archive)
	webuiID := o.seedWebui(t, webuiContract, "approved")
	runtime := gapApprovedRuntime(t, runtimeContract, "runtime-gap")
	platform := runtimeContract.GetImage().GetPlatform()
	artifact := gapArtifact("registry.example/official/agent", platform)

	in := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "gap-job-9", "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, webuiID, runtime.GetId(), in)
	if _, err := o.db.ExecContext(o.ctx, `UPDATE capability.reference_claims SET bound_input_digest=$2 WHERE id=$1`, in.GetWebuiClaimId(), "sha256:"+strings.Repeat("e", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err := o.gapDeliver(t, "gap-job-9", in, gapDescriptorFor(in, artifact), artifact, runtime, "event-gap-9", nil); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("claim bound to a different digest registered=%v", err)
	}
	if o.registeredVersionCount(t) != 0 {
		t.Fatalf("mis-bound claim produced %d versions", o.registeredVersionCount(t))
	}
}

// TestGap6SnapshotDigestIsCanonical proves the frozen snapshot digest is
// re-derivable: the same facts always produce the same digest, and the digest is
// not protojson output (which is intentionally unstable across binaries).
func TestGap6SnapshotDigestIsCanonical(t *testing.T) {
	runtimeContract := gapRuntimeContract(t)
	webuiContract := gapWebuiContract(t)
	runtime := gapApprovedRuntime(t, runtimeContract, "runtime-gap")
	o := &gapOwner{}
	first := o.gapSnapshot(t, runtime, runtimeContract, "ui-gap", webuiContract, "pkg-gap", "pv-gap", 7, gapArtifactDigest)
	second := o.gapSnapshot(t, runtime, runtimeContract, "ui-gap", webuiContract, "pkg-gap", "pv-gap", 7, gapArtifactDigest)
	if first.GetSnapshotDigest() != second.GetSnapshotDigest() {
		t.Fatalf("same facts produced different digests: %s vs %s", first.GetSnapshotDigest(), second.GetSnapshotDigest())
	}
	if first.GetSnapshotDigest() == "" {
		t.Fatal("frozen snapshot has no digest")
	}
	// Round-tripping the snapshot through PostgreSQL jsonb must not change the
	// identity a later owner recomputes.
	raw, err := publicjson.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	decoded := &api.BuildInputSnapshot{}
	if err = publicjson.Unmarshal(raw, decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetSnapshotDigest() != first.GetSnapshotDigest() {
		t.Fatalf("round-trip digest=%s want=%s", decoded.GetSnapshotDigest(), first.GetSnapshotDigest())
	}
	again, err := buildInputDigest(decoded)
	if err != nil {
		t.Fatal(err)
	}
	if again != first.GetSnapshotDigest() {
		t.Fatalf("re-derived digest=%s want=%s", again, first.GetSnapshotDigest())
	}
}

// candidateZIPBytes reads the minimal valid OMA transport archive the intake
// tests already exercise.
func candidateZIPBytes(t *testing.T) []byte {
	t.Helper()
	archive, err := os.ReadFile(candidateArchive(t, nil))
	if err != nil {
		t.Fatal(err)
	}
	return archive
}

const gapArtifactDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// seedPublisher admits the publisher namespace the approved schema examples
// reference, so the WebUI/Runtime admission FK and prefix checks have an owner
// row instead of a test-authored string.
func (o *gapOwner) seedPublisher(t *testing.T) {
	t.Helper()
	contract := gapWebuiContract(t)
	if _, err := o.db.ExecContext(o.ctx, `INSERT INTO capability.publisher_namespaces(id,name,kind,registry_id,repository_prefix,admission_receipt_id) VALUES($1,$2,'official','registry.example',$3,'gap-fixture-receipt')`, contract.GetPublisherNamespaceId(), "gap-publisher", "registry.example/official"); err != nil {
		t.Fatal(err)
	}
}

// TestGap6SingleReadyWriterRejectsSideDoors reproduces GAP-6(d): CapabilityVersion
// has exactly one path that produces a ready row, and no second attempt — a
// duplicate event, a second event for the same job, or a direct status flip —
// creates or promotes another version.
func TestGap6SingleReadyWriterRejectsSideDoors(t *testing.T) {
	o := gapNewOwner(t)
	o.seedPublisher(t)
	runtimeContract := gapRuntimeContract(t)
	webuiContract := gapWebuiContract(t)
	archive := candidateZIPBytes(t)
	packageVersionID, sha := o.uploadedPackage(t, "tenant-gap", "gap-ns", "gap-pkg", archive)
	webuiID := o.seedWebui(t, webuiContract, "approved")
	runtime := gapApprovedRuntime(t, runtimeContract, "runtime-gap")
	platform := runtimeContract.GetImage().GetPlatform()
	artifact := gapArtifact("registry.example/official/agent", platform)

	in := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "gap-job-1", "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, webuiID, runtime.GetId(), in)
	descriptor := gapDescriptorFor(in, artifact)
	ack, err := o.gapDeliver(t, "gap-job-1", in, descriptor, artifact, runtime, "event-gap-1", nil)
	if err != nil || !ack.GetCommitted() {
		t.Fatalf("first registration=%v %v", ack, err)
	}
	if o.readyVersionCount(t) != 1 {
		t.Fatalf("ready versions=%d", o.readyVersionCount(t))
	}

	// A replayed event with the same identity is a duplicate, not a second ready
	// version, and the owner reports it as such.
	replay, err := o.gapDeliver(t, "gap-job-1", in, descriptor, artifact, runtime, "event-gap-1", nil)
	if err != nil || !replay.GetCommitted() || !replay.GetDuplicate() {
		t.Fatalf("replayed registration=%v %v", replay, err)
	}
	if o.readyVersionCount(t) != 1 || o.registeredVersionCount(t) != 1 {
		t.Fatalf("replay created another version: ready=%d total=%d", o.readyVersionCount(t), o.registeredVersionCount(t))
	}

	// A distinct event for the same Build job cannot create a second version for
	// that job: the version identity is unique per job.
	if _, err = o.gapDeliver(t, "gap-job-1", in, descriptor, artifact, runtime, "event-gap-1b", nil); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("second event for one Build job=%v", err)
	}
	if o.registeredVersionCount(t) != 1 {
		t.Fatalf("side door created another version: %d", o.registeredVersionCount(t))
	}

	// No RPC flips an existing version's status to ready: the only statement that
	// writes a ready row lives in the single Deliver registration path, and no
	// statement anywhere updates a version into 'ready'.
	var writers, promotions []string
	for _, path := range gapSourceFiles(t) {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(body), ";") {
			if !strings.Contains(statement, "capability_versions") {
				continue
			}
			writes := strings.Contains(statement, "INSERT INTO capability.capability_versions") || strings.Contains(statement, "UPDATE capability.capability_versions")
			if writes && strings.Contains(statement, "'ready'") {
				writers = append(writers, path)
			}
			if strings.Contains(statement, "UPDATE capability.capability_versions") && strings.Contains(statement, "status") {
				promotions = append(promotions, path)
			}
		}
	}
	if len(writers) != 1 || !strings.HasSuffix(writers[0], "coordination.go") {
		t.Fatalf("capability_versions ready writers=%v, want exactly the Deliver registration path", writers)
	}
	if len(promotions) != 0 {
		t.Fatalf("capability_versions status promotion statements=%v", promotions)
	}
}

// readyVersionCount counts only ready rows, so a deprecated or deleted version
// cannot be mistaken for a promoted one.
func (o *gapOwner) readyVersionCount(t *testing.T) int {
	t.Helper()
	var count int
	if err := o.db.QueryRowContext(o.ctx, `SELECT count(*) FROM capability.capability_versions WHERE status='ready'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

// gapSourceFiles lists this owner's non-test Go sources.
func gapSourceFiles(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		paths = append(paths, entry.Name())
	}
	if len(paths) == 0 {
		t.Fatal("no owner sources found")
	}
	return paths
}

// TestGap6FixtureReceiptIsNotApproval reproduces GAP-6(b)'s fixture clause: an
// admission receipt string — including an obvious fixture value — is never what
// makes an input approved. Approval is the owning record's current status, and
// the same record with an unchanged receipt is refused as soon as the owner says
// it is not approved.
func TestGap6FixtureReceiptIsNotApproval(t *testing.T) {
	o := gapNewOwner(t)
	o.seedPublisher(t)
	runtimeContract := gapRuntimeContract(t)
	webuiContract := gapWebuiContract(t)
	archive := candidateZIPBytes(t)
	packageVersionID, sha := o.uploadedPackage(t, "tenant-gap", "gap-ns", "gap-pkg", archive)
	// seedWebui admits with the explicit fixture receipt string.
	webuiID := o.seedWebui(t, webuiContract, "approved")
	runtime := gapApprovedRuntime(t, runtimeContract, "runtime-gap")
	platform := runtimeContract.GetImage().GetPlatform()
	artifact := gapArtifact("registry.example/official/agent", platform)

	var receipt string
	if err := o.db.QueryRowContext(o.ctx, `SELECT admission_receipt_id FROM capability.webui_versions WHERE id=$1`, webuiID).Scan(&receipt); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(receipt, "fixture") {
		t.Fatalf("fixture WebUI receipt=%q", receipt)
	}
	// The same fixture receipt on the publisher namespace is likewise never the
	// authority: revoking the namespace refuses the very same inputs.
	if _, err := o.db.ExecContext(o.ctx, `UPDATE capability.publisher_namespaces SET status='revoked' WHERE id=$1`, webuiContract.GetPublisherNamespaceId()); err != nil {
		t.Fatal(err)
	}
	in := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "gap-job-1", "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, webuiID, runtime.GetId(), in)
	if _, err := o.gapDeliver(t, "gap-job-1", in, gapDescriptorFor(in, artifact), artifact, runtime, "event-gap-1", nil); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("fixture receipt was accepted as approval=%v", err)
	}
	if o.registeredVersionCount(t) != 0 {
		t.Fatalf("fixture receipt produced %d ready versions", o.registeredVersionCount(t))
	}
	// Restoring the owner's approval admits the same bytes, which proves the
	// refusal came from the status column rather than the receipt value.
	if _, err := o.db.ExecContext(o.ctx, `UPDATE capability.publisher_namespaces SET status='approved' WHERE id=$1`, webuiContract.GetPublisherNamespaceId()); err != nil {
		t.Fatal(err)
	}
	if _, err := o.gapDeliver(t, "gap-job-1", in, gapDescriptorFor(in, artifact), artifact, runtime, "event-gap-1", nil); err != nil {
		t.Fatalf("approved owner status with the same fixture receipt=%v", err)
	}
}

// TestGap6RegisteredVersionKeepsOriginalPackageBytes is GAP-6(e)'s readback: the
// bytes uploaded through the intake API are served back by the object data plane
// with the same SHA-256 the frozen snapshot and the registered CapabilityVersion
// row carry, so the package identity is provably the original uploaded bytes and
// not a re-encoded or substituted artifact.
func TestGap6RegisteredVersionKeepsOriginalPackageBytes(t *testing.T) {
	o := gapNewOwner(t)
	o.seedPublisher(t)
	runtimeContract := gapRuntimeContract(t)
	webuiContract := gapWebuiContract(t)
	archive := candidateZIPBytes(t)
	packageVersionID, sha := o.uploadedPackage(t, "tenant-gap", "gap-ns", "gap-pkg", archive)
	webuiID := o.seedWebui(t, webuiContract, "approved")
	runtime := gapApprovedRuntime(t, runtimeContract, "runtime-gap")
	platform := runtimeContract.GetImage().GetPlatform()
	artifact := gapArtifact("registry.example/official/agent", platform)

	in := o.gapSnapshot(t, runtime, runtimeContract, webuiID, webuiContract, "gap-pkg", packageVersionID, int64(len(archive)), sha)
	o.gapSeedInputs(t, "gap-job-1", "tenant-gap", "gap-ns", "gap-pkg", packageVersionID, webuiID, runtime.GetId(), in)
	if _, err := o.gapDeliver(t, "gap-job-1", in, gapDescriptorFor(in, artifact), artifact, runtime, "event-gap-1", nil); err != nil {
		t.Fatalf("registration=%v", err)
	}

	// The frozen snapshot names the same immutable object and digest intake
	// recorded, and the registered row carries that PackageVersion.
	if in.GetPackageObject().GetSha256() != sha || in.GetPackageObject().GetStorageObjectId() != sha {
		t.Fatalf("snapshot object=%v want=%s", in.GetPackageObject(), sha)
	}
	var storedObject, storedSHA string
	var storedSize int64
	var rowPackageVersion string
	if err := o.db.QueryRowContext(o.ctx, `SELECT v.object_ref,v.sha256,v.size_bytes,c.package_version_id FROM capability.package_versions v JOIN capability.capability_versions c ON c.package_version_id=v.id WHERE v.id=$1`, packageVersionID).Scan(&storedObject, &storedSHA, &storedSize, &rowPackageVersion); err != nil {
		t.Fatal(err)
	}
	if storedObject != sha || storedSHA != sha || storedSize != int64(len(archive)) || rowPackageVersion != packageVersionID {
		t.Fatalf("stored object=%s sha=%s size=%d packageVersion=%s", storedObject, storedSHA, storedSize, rowPackageVersion)
	}

	// The byte readback is served by Capability's own object data plane, and the
	// bytes it returns are the uploaded archive under the frozen digest.
	handler, err := o.service.DataHandler(strings.Repeat("t", 32))
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("GET", "/objects/"+strings.TrimPrefix(sha, "sha256:"), nil)
	request.Header.Set("Authorization", "Bearer "+strings.Repeat("t", 32))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.Code != 200 || !bytes.Equal(body, archive) || digest(body) != in.GetPackageObject().GetSha256() {
		t.Fatalf("package readback status=%d bytes=%d want=%d digest=%s want=%s", response.Code, len(body), len(archive), digest(body), in.GetPackageObject().GetSha256())
	}
}
