package launch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerstore"
)

// applicationSource is the resolved, immutable source of one accepted order. Both
// branches carry the exact artifact and deployment descriptor Serve must reserve;
// the source differs: a built Agent names its CapabilityVersion, the default OPL
// App names an approved Runtime Release with no Build lineage.
type applicationSource struct {
	Selection            *api.WorkspaceApplicationSelection
	Artifact             *api.ArtifactReference
	DeploymentDescriptor *api.DeploymentDescriptor
	DescriptorDigest     string
	DescriptorObjectRef  string
	DataCompatibility    *api.DataCompatibility
	RuntimeVersionID     string
	CapabilityVersionID  string
}

// acceptedSelection derives the explicit selection from an accepted quote. A
// quote that names a Runtime version is the default App; one that names a
// CapabilityVersion is a built Agent. A quote with neither is rejected rather
// than assumed.
func acceptedSelection(quote *api.Quote) (*api.WorkspaceApplicationSelection, error) {
	if quote.GetRuntimeVersionId() != "" {
		return &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP, RuntimeVersionId: proto.String(quote.GetRuntimeVersionId())}, nil
	}
	if quote.GetCapabilityVersionId() != "" {
		return &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT, CapabilityVersionId: proto.String(quote.GetCapabilityVersionId())}, nil
	}
	return nil, status.Error(codes.FailedPrecondition, "accepted quote names no application source")
}

// resolveApplicationSource confirms the accepted source with its owning authority
// and returns the exact immutable facts Serve consumes. It never fabricates a
// Package, Build or CapabilityVersion for the default App.
func (s *Service) resolveApplicationSource(ctx context.Context, op ownerstore.Operation, grant string, accepted *api.QuoteAcceptance) (*applicationSource, error) {
	selection, err := acceptedSelection(accepted.GetQuote())
	if err != nil {
		return nil, err
	}
	if err := contracts.ValidateWorkspaceApplicationSelection(selection); err != nil {
		return nil, status.Error(codes.FailedPrecondition, err.Error())
	}
	switch selection.GetKind() {
	case api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT:
		if s.Capability == nil {
			return nil, status.Error(codes.Unavailable, "Capability is not configured")
		}
		request := &api.GetCapabilityVersionRpcRequest{Context: continuation(op, grant, "runtime_capability"), CapabilityVersionId: selection.GetCapabilityVersionId()}
		version, err := s.Capability.GetCapabilityVersion(ctx, request)
		if err != nil {
			return nil, err
		}
		if err := validateRuntimeVersion(accepted, version); err != nil {
			return nil, err
		}
		return &applicationSource{
			Selection: selection, Artifact: version.GetArtifact(), DeploymentDescriptor: version.GetDeploymentDescriptor(),
			DescriptorDigest: version.GetDeploymentDescriptorDigest(), DescriptorObjectRef: version.GetDeploymentDescriptorObjectRef(),
			DataCompatibility: version.GetDataCompatibility(), CapabilityVersionID: version.GetId(),
		}, nil
	case api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP:
		if s.RuntimeReleases == nil {
			return nil, status.Error(codes.Unavailable, "Runtime Control is not configured")
		}
		release, err := s.runtimeRelease(ctx, continuation(op, grant, "runtime_release"), selection.GetRuntimeVersionId())
		if err != nil {
			return nil, err
		}
		descriptor := &api.DeploymentDescriptor{
			SchemaVersion:        api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1,
			Artifact:             release.GetPublisherContract().GetImage(),
			Provenance:           api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_RUNTIME_RELEASE,
			ApplicationSelection: selection,
			ApplicationRevision:  release.GetPublisherContract().GetApplicationRevisionTemplate(),
		}
		raw, err := publicjson.Marshal(descriptor)
		if err != nil {
			return nil, status.Error(codes.DataLoss, "default App descriptor cannot be encoded")
		}
		sum := sha256.Sum256(raw)
		descriptorDigest := "sha256:" + hex.EncodeToString(sum[:])
		return &applicationSource{
			Selection: selection, Artifact: descriptor.GetArtifact(), DeploymentDescriptor: descriptor,
			DescriptorDigest: descriptorDigest, DescriptorObjectRef: "runtime-release:" + release.GetId() + "@" + descriptorDigest,
			RuntimeVersionID: release.GetId(),
		}, nil
	default:
		return nil, status.Error(codes.InvalidArgument, "unknown application selection kind")
	}
}

// sourceRecord is the durable, replayable form of a resolved application source.
// It carries the exact bytes Serve reserved so a recovered order reissues the same
// command without re-reading a possibly-changed catalog.
type sourceRecord struct {
	Kind                 string          `json:"kind"`
	RuntimeVersionID     string          `json:"runtimeVersionId,omitempty"`
	CapabilityVersionID  string          `json:"capabilityVersionId,omitempty"`
	Artifact             json.RawMessage `json:"artifact"`
	DeploymentDescriptor json.RawMessage `json:"deploymentDescriptor"`
	DescriptorDigest     string          `json:"deploymentDescriptorDigest"`
	DescriptorObjectRef  string          `json:"deploymentDescriptorObjectRef"`
	// DataCompatibility is the accepted version's data contract. It is part of the
	// runtime deploy command, so it must be part of the durable source record too,
	// otherwise a recovered command differs from the one originally frozen.
	DataCompatibility json.RawMessage `json:"dataCompatibility,omitempty"`
}

func (a *applicationSource) record() (json.RawMessage, error) {
	artifact, err := protojson.Marshal(a.Artifact)
	if err != nil {
		return nil, err
	}
	descriptor, err := publicjson.Marshal(a.DeploymentDescriptor)
	if err != nil {
		return nil, err
	}
	var compatibility json.RawMessage
	if a.DataCompatibility != nil {
		compatibility, err = publicjson.Marshal(a.DataCompatibility)
		if err != nil {
			return nil, err
		}
	}
	kind := "agent"
	if a.Selection.GetKind() == api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP {
		kind = "opl_app"
	}
	return json.Marshal(sourceRecord{Kind: kind, RuntimeVersionID: a.RuntimeVersionID, CapabilityVersionID: a.CapabilityVersionID, Artifact: artifact, DeploymentDescriptor: descriptor, DescriptorDigest: a.DescriptorDigest, DescriptorObjectRef: a.DescriptorObjectRef, DataCompatibility: compatibility})
}

func (s *sourceRecord) resolve() (*applicationSource, error) {
	artifact := &api.ArtifactReference{}
	if protojson.Unmarshal(s.Artifact, artifact) != nil {
		return nil, status.Error(codes.DataLoss, "stored application source artifact is invalid")
	}
	descriptor := &api.DeploymentDescriptor{}
	if publicjson.Unmarshal(s.DeploymentDescriptor, descriptor) != nil {
		return nil, status.Error(codes.DataLoss, "stored application source descriptor is invalid")
	}
	selection := &api.WorkspaceApplicationSelection{}
	if s.Kind == "opl_app" {
		selection.Kind = api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP
		selection.RuntimeVersionId = proto.String(s.RuntimeVersionID)
	} else {
		selection.Kind = api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT
		selection.CapabilityVersionId = proto.String(s.CapabilityVersionID)
	}
	var compatibility *api.DataCompatibility
	if len(s.DataCompatibility) > 0 {
		compatibility = &api.DataCompatibility{}
		if publicjson.Unmarshal(s.DataCompatibility, compatibility) != nil {
			return nil, status.Error(codes.DataLoss, "stored application source data compatibility is invalid")
		}
	}
	return &applicationSource{Selection: selection, Artifact: artifact, DeploymentDescriptor: descriptor, DescriptorDigest: s.DescriptorDigest, DescriptorObjectRef: s.DescriptorObjectRef, RuntimeVersionID: s.RuntimeVersionID, CapabilityVersionID: s.CapabilityVersionID, DataCompatibility: compatibility}, nil
}

// runtimeRelease reads the exact approved release from Runtime Control's paged
// catalog. It refuses an unapproved release instead of substituting another.
func (s *Service) runtimeRelease(ctx context.Context, call *api.CallContext, runtimeVersionID string) (*api.RuntimeVersion, error) {
	if runtimeVersionID == "" {
		return nil, status.Error(codes.InvalidArgument, "a default OPL App order requires a runtime version")
	}
	cursor := ""
	for {
		page, err := s.RuntimeReleases.ListRuntimeVersions(ctx, &api.ListRuntimeVersionsRpcRequest{Context: call, QueryCursor: &cursor})
		if err != nil {
			return nil, err
		}
		for _, release := range page.GetItems() {
			if release.GetId() != runtimeVersionID {
				continue
			}
			if release.GetStatus() != api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED {
				return nil, status.Error(codes.FailedPrecondition, "selected Runtime Release is not approved")
			}
			if release.GetPublisherContract() == nil || release.GetPublisherContract().GetImage() == nil || release.GetPublisherContract().GetApplicationRevisionTemplate() == nil {
				return nil, status.Error(codes.FailedPrecondition, "Runtime Release has no immutable application contract")
			}
			return release, nil
		}
		if page.GetNextCursor() == "" {
			return nil, status.Error(codes.NotFound, "Runtime Release not found")
		}
		cursor = page.GetNextCursor()
	}
}

// identity returns the durable identity of one resolved source for the commit
// evidence: a CapabilityVersion for an Agent, a Runtime Release for the default App.
func (a *applicationSource) identity() string {
	if a.CapabilityVersionID != "" {
		return a.CapabilityVersionID
	}
	return a.RuntimeVersionID
}
