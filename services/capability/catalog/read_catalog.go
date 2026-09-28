package catalog

import (
	"context"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"strings"
	"time"
)

func (s *Service) ListWebuiVersions(ctx context.Context, r *api.ListWebuiVersionsRpcRequest) (*api.WebuiVersionPage, error) {
	if err := s.auth(ctx, r.GetContext(), "ListWebuiVersions", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, ""); err != nil {
		return nil, err
	}
	n := limit(r.GetQueryLimit())
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,version_label,artifact_digest,status,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract_object_ref,publisher_contract,created_at FROM capability.webui_versions WHERE id>$1 ORDER BY id LIMIT $2`, r.GetQueryCursor(), n+1)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	out := &api.WebuiVersionPage{}
	for rows.Next() {
		v := &api.WebuiVersion{}
		var raw []byte
		var state string
		var created time.Time
		if err := rows.Scan(&v.Id, &v.Name, &v.VersionLabel, &v.ArtifactDigest, &state, &v.AdmissionReceiptId, &v.PublisherNamespaceId, &v.PublisherContractDigest, &v.PublisherContractObjectRef, &raw, &created); err != nil {
			return nil, dbError(err)
		}
		v.PublisherContract = &api.WebuiPublisherContract{}
		if err := publicjson.Unmarshal(raw, v.PublisherContract); err != nil {
			return nil, status.Error(codes.DataLoss, "invalid persisted WebUI contract")
		}
		v.RuntimeAbiVersions = v.PublisherContract.RuntimeAbiVersions
		v.UiProtocolVersion = "opl-webui/v1"
		v.Status = api.WebuiVersionStatusEnum(api.WebuiVersionStatusEnum_value["WEBUI_VERSION_STATUS_ENUM_"+upper(state)])
		v.CreatedAt = timestamppb.New(created)
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = proto.String(out.Items[n-1].Id)
	}
	return out, dbError(rows.Err())
}

func (s *Service) ListPublisherNamespaces(ctx context.Context, r *api.ListPublisherNamespacesRpcRequest) (*api.PublisherNamespacePage, error) {
	if err := s.auth(ctx, r.GetContext(), "ListPublisherNamespaces", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, ""); err != nil {
		return nil, err
	}
	n := limit(r.GetQueryLimit())
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,kind,registry_id,repository_prefix,admission_receipt_id,status,created_at FROM capability.publisher_namespaces WHERE id>$1 ORDER BY id LIMIT $2`, r.GetQueryCursor(), n+1)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	out := &api.PublisherNamespacePage{}
	for rows.Next() {
		v := &api.PublisherNamespace{}
		var kind, st string
		var created time.Time
		if err = rows.Scan(&v.Id, &v.Name, &kind, &v.RegistryId, &v.RepositoryPrefix, &v.AdmissionReceiptId, &st, &created); err != nil {
			return nil, dbError(err)
		}
		v.Kind = api.PublisherNamespaceKindEnum(api.PublisherNamespaceKindEnum_value["PUBLISHER_NAMESPACE_KIND_ENUM_"+upper(kind)])
		v.Status = api.PublisherNamespaceStatusEnum(api.PublisherNamespaceStatusEnum_value["PUBLISHER_NAMESPACE_STATUS_ENUM_"+upper(st)])
		v.CreatedAt = timestamppb.New(created)
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = proto.String(out.Items[n-1].Id)
	}
	return out, dbError(rows.Err())
}
func (s *Service) GetCapabilityVersion(ctx context.Context, r *api.GetCapabilityVersionRpcRequest) (*api.CapabilityVersion, error) {
	if err := s.auth(ctx, r.GetContext(), "GetCapabilityVersion", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, r.CapabilityVersionId); err != nil {
		return nil, err
	}
	var raw []byte
	var state string
	var created time.Time
	if err := s.DB.QueryRowContext(ctx, `SELECT provenance_evidence,status,created_at FROM capability.capability_versions WHERE id=$1`, r.CapabilityVersionId).Scan(&raw, &state, &created); err != nil {
		return nil, dbError(err)
	}
	return buildCapabilityVersion(r.CapabilityVersionId, raw, state, created)
}

func buildCapabilityVersion(id string, raw []byte, state string, created time.Time) (*api.CapabilityVersion, error) {
	v := &api.BuildArtifactReadback{}
	if err := protojson.Unmarshal(raw, v); err != nil {
		return nil, dbError(err)
	}
	if v.Input == nil || v.Artifact == nil || v.Outcome != api.Observation_OBSERVATION_CONFIRMED {
		return nil, status.Error(codes.DataLoss, "invalid persisted Build evidence")
	}
	return &api.CapabilityVersion{Id: id, PackageId: proto.String(v.Input.PackageId), PackageVersionId: proto.String(v.Input.PackageVersionId), RuntimeVersionId: proto.String(v.Input.RuntimeVersionId), WebuiVersionId: proto.String(v.Input.WebuiVersionId), BuildJobId: proto.String(v.BuildJobId), VersionLabel: v.VersionLabel, ArtifactDigest: v.Artifact.Digest, Artifact: v.Artifact, Status: api.CapabilityVersionStatusEnum(api.CapabilityVersionStatusEnum_value["CAPABILITY_VERSION_STATUS_ENUM_"+upper(state)]), CreatedAt: timestamppb.New(created), Provenance: api.CapabilityVersionProvenanceEnum_CAPABILITY_VERSION_PROVENANCE_ENUM_BUILD, DeploymentDescriptor: v.DeploymentDescriptor, DeploymentDescriptorDigest: v.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: v.DeploymentDescriptorObjectRef, ModelRequirements: v.ModelRequirements, DataCompatibility: v.DataCompatibility}, nil
}

// ListCapabilityVersions reads only versions of packages visible to the caller.
// Build provenance remains the immutable owner receipt; live state and active
// reference counts come from Capability's own tables.
func (s *Service) ListCapabilityVersions(ctx context.Context, r *api.ListCapabilityVersionsRpcRequest) (*api.CapabilityVersionPage, error) {
	if err := s.auth(ctx, r.GetContext(), "ListCapabilityVersions", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, ""); err != nil {
		return nil, err
	}
	state := ""
	if r.GetQueryStatus() != api.ListCapabilityVersionsRpcRequestStatusEnum_LIST_CAPABILITY_VERSIONS_RPC_REQUEST_STATUS_ENUM_UNSPECIFIED {
		name, ok := api.ListCapabilityVersionsRpcRequestStatusEnum_name[int32(r.GetQueryStatus())]
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "invalid CapabilityVersion status")
		}
		state = strings.ToLower(strings.TrimPrefix(name, "LIST_CAPABILITY_VERSIONS_RPC_REQUEST_STATUS_ENUM_"))
	}
	n := limit(r.GetQueryLimit())
	rows, err := s.DB.QueryContext(ctx, `SELECT v.id,v.provenance_evidence,v.status,v.created_at,(SELECT COUNT(*) FROM capability.reference_claims rc WHERE rc.capability_version_id=v.id AND rc.released_at IS NULL) FROM capability.capability_versions v JOIN capability.packages p ON p.id=v.package_id JOIN capability.namespaces ns ON ns.id=p.namespace_id WHERE (ns.tenant_id=$1 OR p.visibility='official') AND ($2='' OR v.package_id=$2) AND ($3='' OR v.status=$3) AND v.id>$4 ORDER BY v.id LIMIT $5`, tenant(r.GetContext()), r.GetQueryPackageId(), state, r.GetQueryCursor(), n+1)
	if err != nil {
		return nil, dbError(err)
	}
	defer rows.Close()
	out := &api.CapabilityVersionPage{}
	for rows.Next() {
		var id, state string
		var raw []byte
		var created time.Time
		var count int64
		if err := rows.Scan(&id, &raw, &state, &created, &count); err != nil {
			return nil, dbError(err)
		}
		v, err := buildCapabilityVersion(id, raw, state, created)
		if err != nil {
			return nil, err
		}
		v.ReferenceCount = count
		out.Items = append(out.Items, v)
	}
	if err := rows.Err(); err != nil {
		return nil, dbError(err)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		out.NextCursor = proto.String(out.Items[n-1].Id)
	}
	return out, nil
}
