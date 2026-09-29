package launch

import (
	"encoding/json"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
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
