package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"

	"github.com/lib/pq"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

func (s *Service) ConfigurePublisherSchema(path, sha string) error {
	data, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if digest(data) != sha {
		return status.Error(codes.FailedPrecondition, "publisher schema digest mismatch")
	}
	var value any
	if e = json.Unmarshal(data, &value); e != nil {
		return e
	}
	c := publicjson.NewSchemaCompiler()
	if e = c.AddResource("publisher.json", value); e != nil {
		return e
	}
	s.PublisherSchema, e = c.Compile("publisher.json")
	return e
}
func (s *Service) admin(ctx context.Context, c *api.CallContext, action, id string) error {
	if c.GetScope().GetPlatform() == nil {
		return status.Error(codes.PermissionDenied, "platform authorization required")
	}
	return s.auth(ctx, c, action, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG, id)
}
func (s *Service) CreatePublisherNamespace(ctx context.Context, r *api.CreatePublisherNamespaceRpcRequest) (*api.PublisherNamespace, error) {
	if e := s.admin(ctx, r.GetContext(), "CreatePublisherNamespace", ""); e != nil {
		return nil, e
	}
	b := r.GetBody()
	if b == nil || checkName(b.Name) != nil || checkName(b.RegistryId) != nil || b.AdmissionReceiptId == "" || (b.Kind != api.CreatePublisherNamespaceRequestKindEnum_CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_OFFICIAL && b.Kind != api.CreatePublisherNamespaceRequestKindEnum_CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_THIRD_PARTY) {
		return nil, status.Error(codes.InvalidArgument, "publisher identity and admission receipt required")
	}
	prefix := b.RepositoryPrefix
	if prefix == "" || strings.TrimSpace(prefix) != prefix || strings.HasSuffix(prefix, "/") || strings.ContainsAny(prefix, "@?#\\ ") || strings.Contains(prefix, "..") || strings.Contains(prefix, "://") || strings.ToLower(prefix) != prefix {
		return nil, status.Error(codes.InvalidArgument, "exact registry repository prefix required")
	}
	out := &api.PublisherNamespace{}
	e := s.command(ctx, r.Context, "CreatePublisherNamespace", b, out, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('publisher-prefix-admission',0))`); e != nil {
			return dbError(e)
		}
		var overlap bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM capability.publisher_namespaces WHERE repository_prefix=$1 OR starts_with(repository_prefix,$1||'/') OR starts_with($1,repository_prefix||'/'))`, prefix).Scan(&overlap); e != nil {
			return dbError(e)
		}
		if overlap {
			return status.Error(codes.AlreadyExists, "repository prefix overlaps an admitted publisher")
		}
		kind := "third_party"
		if b.Kind == api.CreatePublisherNamespaceRequestKindEnum_CREATE_PUBLISHER_NAMESPACE_REQUEST_KIND_ENUM_OFFICIAL {
			kind = "official"
		}
		*out = api.PublisherNamespace{Id: id("publisher"), Name: b.Name, Kind: api.PublisherNamespaceKindEnum(api.PublisherNamespaceKindEnum_value["PUBLISHER_NAMESPACE_KIND_ENUM_"+upper(kind)]), RegistryId: b.RegistryId, RepositoryPrefix: prefix, AdmissionReceiptId: b.AdmissionReceiptId, Status: api.PublisherNamespaceStatusEnum_PUBLISHER_NAMESPACE_STATUS_ENUM_APPROVED, CreatedAt: timestamppb.Now()}
		_, e := tx.ExecContext(ctx, `INSERT INTO capability.publisher_namespaces(id,name,kind,registry_id,repository_prefix,admission_receipt_id,created_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, out.Id, out.Name, kind, out.RegistryId, prefix, out.AdmissionReceiptId, out.CreatedAt.AsTime())
		return dbError(e)
	})
	return out, e
}
func (s *Service) RevokePublisherNamespace(ctx context.Context, r *api.RevokePublisherNamespaceRpcRequest) (*api.PublisherNamespace, error) {
	if e := s.admin(ctx, r.GetContext(), "RevokePublisherNamespace", r.GetPublisherNamespaceId()); e != nil {
		return nil, e
	}
	if strings.TrimSpace(r.GetBody().GetReason()) == "" {
		return nil, status.Error(codes.InvalidArgument, "revocation reason required")
	}
	out := &api.PublisherNamespace{}
	e := s.command(ctx, r.Context, "RevokePublisherNamespace", &api.RevokePublisherNamespaceRpcRequest{PublisherNamespaceId: r.PublisherNamespaceId, Body: r.Body}, out, func(tx *sql.Tx) error {
		var kind string
		var created sql.NullTime
		e := tx.QueryRowContext(ctx, `UPDATE capability.publisher_namespaces SET status='revoked',updated_at=now() WHERE id=$1 RETURNING id,name,kind,registry_id,repository_prefix,admission_receipt_id,created_at`, r.PublisherNamespaceId).Scan(&out.Id, &out.Name, &kind, &out.RegistryId, &out.RepositoryPrefix, &out.AdmissionReceiptId, &created)
		if e != nil {
			return dbError(e)
		}
		out.Kind = api.PublisherNamespaceKindEnum(api.PublisherNamespaceKindEnum_value["PUBLISHER_NAMESPACE_KIND_ENUM_"+upper(kind)])
		out.Status = api.PublisherNamespaceStatusEnum_PUBLISHER_NAMESPACE_STATUS_ENUM_REVOKED
		out.CreatedAt = timestamppb.New(created.Time)
		return nil
	})
	return out, e
}
func (s *Service) RegisterWebuiVersion(ctx context.Context, r *api.RegisterWebuiVersionRpcRequest) (*api.WebuiVersion, error) {
	if e := s.admin(ctx, r.GetContext(), "RegisterWebuiVersion", ""); e != nil {
		return nil, e
	}
	b := r.GetBody()
	if b == nil || checkName(b.Name) != nil || checkName(b.VersionLabel) != nil || b.AdmissionReceiptId == "" || b.PublisherContract == nil || b.PublisherNamespaceId != b.PublisherContract.PublisherNamespaceId {
		return nil, status.Error(codes.InvalidArgument, "invalid WebUI admission")
	}
	if s.PublisherSchema == nil {
		return nil, status.Error(codes.Unavailable, "approved publisher schema not configured")
	}
	raw, e := publicjson.Marshal(b.PublisherContract)
	if e != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid WebUI contract")
	}
	var value any
	if json.Unmarshal(raw, &value) != nil || s.PublisherSchema.Validate(value) != nil {
		return nil, status.Error(codes.InvalidArgument, "WebUI contract fails approved schema")
	}
	out := &api.WebuiVersion{}
	e = s.command(ctx, r.Context, "RegisterWebuiVersion", b, out, func(tx *sql.Tx) error {
		var prefix, st string
		if e := tx.QueryRowContext(ctx, `SELECT repository_prefix,status FROM capability.publisher_namespaces WHERE id=$1 FOR SHARE`, b.PublisherNamespaceId).Scan(&prefix, &st); e != nil {
			return dbError(e)
		}
		image := b.PublisherContract.Image
		if st != "approved" || (image.Repository != prefix && !strings.HasPrefix(image.Repository, prefix+"/")) {
			return status.Error(codes.PermissionDenied, "WebUI image is outside its approved publisher")
		}
		*out = api.WebuiVersion{Id: id("webui"), Name: b.Name, VersionLabel: b.VersionLabel, ArtifactDigest: image.Digest, Status: api.WebuiVersionStatusEnum_WEBUI_VERSION_STATUS_ENUM_APPROVED, RuntimeAbiVersions: b.PublisherContract.RuntimeAbiVersions, UiProtocolVersion: "opl-webui/v1", AdmissionReceiptId: b.AdmissionReceiptId, PublisherNamespaceId: b.PublisherNamespaceId, PublisherContract: b.PublisherContract, PublisherContractDigest: digest(raw), CreatedAt: timestamppb.Now()}
		out.PublisherContractObjectRef = "webui-contract:" + out.Id + "@" + out.PublisherContractDigest
		_, e := tx.ExecContext(ctx, `INSERT INTO capability.webui_versions(id,name,version_label,artifact_repository,artifact_digest,approved_by,runtime_abi_versions,ui_protocol_version,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract,publisher_contract_object_ref,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`, out.Id, out.Name, out.VersionLabel, image.Repository, image.Digest, r.Context.ActorId, pq.Array(out.RuntimeAbiVersions), out.UiProtocolVersion, out.AdmissionReceiptId, out.PublisherNamespaceId, out.PublisherContractDigest, raw, out.PublisherContractObjectRef, out.CreatedAt.AsTime())
		return dbError(e)
	})
	return out, e
}
func (s *Service) SetWebuiVersionStatus(ctx context.Context, r *api.SetWebuiVersionStatusRpcRequest) (*api.WebuiVersion, error) {
	if e := s.admin(ctx, r.GetContext(), "SetWebuiVersionStatus", r.GetVersionId()); e != nil {
		return nil, e
	}
	b := r.GetBody()
	st := strings.ToLower(strings.TrimPrefix(b.GetStatus().String(), "CATALOG_STATUS_REQUEST_STATUS_ENUM_"))
	if (st != "approved" && st != "deprecated" && st != "revoked") || strings.TrimSpace(b.GetReason()) == "" {
		return nil, status.Error(codes.InvalidArgument, "valid status and reason required")
	}
	out := &api.WebuiVersion{}
	e := s.command(ctx, r.Context, "SetWebuiVersionStatus", &api.SetWebuiVersionStatusRpcRequest{VersionId: r.VersionId, Body: b}, out, func(tx *sql.Tx) error {
		var raw []byte
		var old string
		if e := tx.QueryRowContext(ctx, `SELECT publisher_contract,status FROM capability.webui_versions WHERE id=$1 FOR UPDATE`, r.VersionId).Scan(&raw, &old); e != nil {
			return dbError(e)
		}
		if old == "revoked" && st != "revoked" {
			return status.Error(codes.FailedPrecondition, "revoked version is immutable")
		}
		if _, e := tx.ExecContext(ctx, `UPDATE capability.webui_versions SET status=$2,updated_at=now() WHERE id=$1`, r.VersionId, st); e != nil {
			return dbError(e)
		}
		// Read the same typed owner projection inside this transaction.
		var created sql.NullTime
		e := tx.QueryRowContext(ctx, `SELECT id,name,version_label,artifact_digest,admission_receipt_id,publisher_namespace_id,publisher_contract_digest,publisher_contract_object_ref,created_at FROM capability.webui_versions WHERE id=$1`, r.VersionId).Scan(&out.Id, &out.Name, &out.VersionLabel, &out.ArtifactDigest, &out.AdmissionReceiptId, &out.PublisherNamespaceId, &out.PublisherContractDigest, &out.PublisherContractObjectRef, &created)
		if e != nil {
			return dbError(e)
		}
		out.PublisherContract = &api.WebuiPublisherContract{}
		if e = publicjson.Unmarshal(raw, out.PublisherContract); e != nil {
			return e
		}
		out.RuntimeAbiVersions = out.PublisherContract.RuntimeAbiVersions
		out.UiProtocolVersion = "opl-webui/v1"
		out.CreatedAt = timestamppb.New(created.Time)
		out.Status = api.WebuiVersionStatusEnum(api.WebuiVersionStatusEnum_value["WEBUI_VERSION_STATUS_ENUM_"+upper(st)])
		return nil
	})
	return out, e
}
