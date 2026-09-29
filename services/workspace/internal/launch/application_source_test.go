package launch

import (
	"context"
	"encoding/json"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// TestAcceptedSelectionDerivesSourceFromQuote proves the accepted quote decides the
// application source: a Runtime version is the default App, a CapabilityVersion is
// a built Agent, and a quote with neither is refused rather than assumed.
func TestAcceptedSelectionDerivesSourceFromQuote(t *testing.T) {
	defaultApp := &api.Quote{RuntimeVersionId: proto.String("runtime-1")}
	selection, err := acceptedSelection(defaultApp)
	if err != nil || selection.GetKind() != api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP || selection.GetRuntimeVersionId() != "runtime-1" {
		t.Fatalf("default app selection: %v %v", selection, err)
	}
	agent := &api.Quote{CapabilityVersionId: proto.String("capability-1")}
	selection, err = acceptedSelection(agent)
	if err != nil || selection.GetKind() != api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT || selection.GetCapabilityVersionId() != "capability-1" {
		t.Fatalf("agent selection: %v %v", selection, err)
	}
	if _, err := acceptedSelection(&api.Quote{}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("quote with no source: %v, want FailedPrecondition", err)
	}
}

// TestSourceRecordRoundTripsDefaultApp proves the durable source record replays the
// exact default-App facts without re-reading a changed catalog.
func TestSourceRecordRoundTripsDefaultApp(t *testing.T) {
	source := &applicationSource{
		Selection:            &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP, RuntimeVersionId: proto.String("runtime-1")},
		Artifact:             &api.ArtifactReference{Repository: "local.example/app", Digest: "sha256:deadbeef"},
		DeploymentDescriptor: &api.DeploymentDescriptor{SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_RUNTIME_RELEASE},
		DescriptorDigest:     "sha256:abc", DescriptorObjectRef: "runtime-release:runtime-1@sha256:abc", RuntimeVersionID: "runtime-1",
	}
	record, err := source.record()
	if err != nil {
		t.Fatal(err)
	}
	stored := &sourceRecord{}
	if err := json.Unmarshal(record, stored); err != nil {
		t.Fatal(err)
	}
	recovered, err := stored.resolve()
	if err != nil {
		t.Fatal(err)
	}
	if recovered.RuntimeVersionID != "runtime-1" || recovered.CapabilityVersionID != "" || recovered.Selection.GetKind() != api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP {
		t.Fatalf("recovered default app source: %+v", recovered)
	}
}

// runtimeReleaseStub serves one approved Runtime Release through the paged
// RuntimeControlProductService surface the Workspace resolver reads.
type runtimeReleaseStub struct {
	api.RuntimeControlProductServiceClient
	releases []*api.RuntimeVersion
	pages    []string
}

func (s *runtimeReleaseStub) ListRuntimeVersions(_ context.Context, r *api.ListRuntimeVersionsRpcRequest, _ ...grpc.CallOption) (*api.RuntimeVersionPage, error) {
	page := &api.RuntimeVersionPage{}
	if len(s.pages) > 0 {
		next := s.pages[0]
		page.NextCursor = &next
		s.pages = s.pages[1:]
	}
	page.Items = s.releases
	return page, nil
}

// TestResolveApplicationSourceDefaultAppBuildsRuntimeReleaseDescriptor proves the
// default OPL App branch resolves its immutable artifact and application revision
// template from the approved Runtime Release, stamps runtime_release provenance and
// fabricates no Build/Package/CapabilityVersion, while an unapproved release is
// refused rather than substituted.
func TestResolveApplicationSourceDefaultAppBuildsRuntimeReleaseDescriptor(t *testing.T) {
	image := &api.ArtifactReference{Repository: "registry.test/opl-app", Digest: "sha256:2222222222222222222222222222222222222222222222222222222222222222"}
	revision := &api.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationId: "opl-app", Version: "1", Platform: "linux/amd64", Image: "registry.test/opl-app@sha256:2222222222222222222222222222222222222222222222222222222222222222", ExposurePolicy: api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION}
	approved := &api.RuntimeVersion{Id: "rv-1", Status: api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED, PublisherContract: &api.RuntimePublisherContract{Image: image, ApplicationRevisionTemplate: revision}}
	service := &Service{RuntimeReleases: &runtimeReleaseStub{releases: []*api.RuntimeVersion{approved}}}
	op := ownerstore.Operation{ID: "op-1", ResourceID: "ws-1", ActorID: "actor-1", TenantID: "tenant-1", RequestID: "req-1"}
	accepted := &api.QuoteAcceptance{Quote: &api.Quote{Id: "quote-1", Purpose: api.QuotePurposeEnum_QUOTE_PURPOSE_ENUM_DEPLOY, RuntimeVersionId: proto.String("rv-1"), ComputePlanId: "compute-1", StoragePlanId: "storage-1", PeriodMonths: 1}}

	source, err := service.resolveApplicationSource(context.Background(), op, "grant-1", accepted)
	if err != nil {
		t.Fatalf("default App resolution: %v", err)
	}
	if source.Selection.GetKind() != api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP || source.RuntimeVersionID != "rv-1" || source.CapabilityVersionID != "" {
		t.Fatalf("resolved source=%+v", source)
	}
	if source.DeploymentDescriptor.GetProvenance() != api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_RUNTIME_RELEASE || source.DeploymentDescriptor.GetProvenance() == api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD {
		t.Fatalf("default App provenance=%v", source.DeploymentDescriptor.GetProvenance())
	}
	if !proto.Equal(source.Artifact, image) || !proto.Equal(source.DeploymentDescriptor.GetApplicationRevision(), revision) {
		t.Fatalf("default App descriptor=%v", source.DeploymentDescriptor)
	}
	if source.DescriptorDigest == "" || source.DescriptorObjectRef == "" {
		t.Fatal("default App source has no descriptor identity")
	}
	// An unapproved release must be refused, never replaced by another.
	unapproved := proto.Clone(approved).(*api.RuntimeVersion)
	unapproved.Status = api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_REVOKED
	service.RuntimeReleases = &runtimeReleaseStub{releases: []*api.RuntimeVersion{unapproved}}
	if _, err := service.resolveApplicationSource(context.Background(), op, "grant-1", accepted); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unapproved release err=%v, want FailedPrecondition", err)
	}
}
