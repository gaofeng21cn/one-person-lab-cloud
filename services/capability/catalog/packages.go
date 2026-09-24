package catalog

import (
	"context"
	"database/sql"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"opl-cloud/packages/contracts/go/api"
	"time"
)

type scanner interface{ Scan(...any) error }

func scanNamespace(r scanner) (*api.Namespace, error) {
	v := &api.Namespace{}
	var kind, st string
	var created time.Time
	e := r.Scan(&v.Id, &v.Name, &kind, &st, &created)
	v.IsDefault = kind == "tenant_default"
	v.Status = api.NamespaceStatusEnum(api.NamespaceStatusEnum_value["NAMESPACE_STATUS_ENUM_"+upper(st)])
	v.CreatedAt = timestamppb.New(created)
	return v, dbError(e)
}
func upper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] -= 32
		}
	}
	return string(b)
}
func (s *Service) ListNamespaces(ctx context.Context, r *api.ListNamespacesRpcRequest) (*api.NamespacePage, error) {
	if e := s.auth(ctx, r.GetContext(), "ListNamespaces", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, tenant(r.GetContext())); e != nil {
		return nil, e
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT id,name,kind,status,created_at FROM capability.namespaces WHERE tenant_id=$1 AND status='active' ORDER BY id`, tenant(r.Context))
	if e != nil {
		return nil, dbError(e)
	}
	defer rows.Close()
	out := &api.NamespacePage{}
	for rows.Next() {
		v, e := scanNamespace(rows)
		if e != nil {
			return nil, e
		}
		out.Items = append(out.Items, v)
	}
	return out, dbError(rows.Err())
}
func (s *Service) CreateNamespace(ctx context.Context, r *api.CreateNamespaceRpcRequest) (*api.Namespace, error) {
	if e := s.auth(ctx, r.GetContext(), "CreateNamespace", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, tenant(r.GetContext())); e != nil {
		return nil, e
	}
	if e := requireTenant(r.Context); e != nil {
		return nil, e
	}
	if e := checkName(r.GetBody().GetName()); e != nil {
		return nil, e
	}
	out := &api.Namespace{}
	e := s.command(ctx, r.Context, "CreateNamespace", r.Body, out, func(tx *sql.Tx) error {
		v, e := scanNamespace(tx.QueryRowContext(ctx, `INSERT INTO capability.namespaces(id,tenant_id,name,kind) VALUES($1,$2,$3,'tenant_custom') RETURNING id,name,kind,status,created_at`, id("ns"), tenant(r.Context), r.Body.Name))
		if e == nil {
			proto.Merge(out, v)
		}
		return e
	})
	return out, e
}
func (s *Service) UpdateNamespace(ctx context.Context, r *api.UpdateNamespaceRpcRequest) (*api.Namespace, error) {
	if e := s.auth(ctx, r.GetContext(), "UpdateNamespace", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, tenant(r.GetContext())); e != nil {
		return nil, e
	}
	if e := checkName(r.GetBody().GetName()); e != nil {
		return nil, e
	}
	return scanNamespace(s.DB.QueryRowContext(ctx, `UPDATE capability.namespaces SET name=$1,updated_at=now() WHERE id=$2 AND tenant_id=$3 AND status='active' RETURNING id,name,kind,status,created_at`, r.Body.Name, r.NamespaceId, tenant(r.Context)))
}
func (s *Service) ArchiveNamespace(ctx context.Context, r *api.ArchiveNamespaceRpcRequest) (*api.Namespace, error) {
	if e := s.auth(ctx, r.GetContext(), "ArchiveNamespace", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, tenant(r.GetContext())); e != nil {
		return nil, e
	}
	return scanNamespace(s.DB.QueryRowContext(ctx, `UPDATE capability.namespaces n SET status='archived',updated_at=now() WHERE id=$1 AND tenant_id=$2 AND kind='tenant_custom' AND NOT EXISTS(SELECT 1 FROM capability.packages p WHERE p.namespace_id=n.id AND p.status='active') RETURNING id,name,kind,status,created_at`, r.NamespaceId, tenant(r.Context)))
}

const packageColumns = `p.id,p.namespace_id,p.name,COALESCE(p.description,''),p.visibility,p.status,p.created_at,p.updated_at`

func scanPackage(r scanner) (*api.Package, error) {
	v := &api.Package{}
	var visibility, st string
	var created, updated time.Time
	e := r.Scan(&v.Id, &v.NamespaceId, &v.Name, &v.Description, &visibility, &st, &created, &updated)
	v.Visibility = api.PackageVisibilityEnum(api.PackageVisibilityEnum_value["PACKAGE_VISIBILITY_ENUM_"+upper(visibility)])
	v.Status = api.PackageStatusEnum(api.PackageStatusEnum_value["PACKAGE_STATUS_ENUM_"+upper(st)])
	v.CreatedAt = timestamppb.New(created)
	v.UpdatedAt = timestamppb.New(updated)
	return v, dbError(e)
}
func (s *Service) ListPackages(ctx context.Context, r *api.ListPackagesRpcRequest) (*api.PackagePage, error) {
	if e := s.auth(ctx, r.GetContext(), "ListPackages", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, ""); e != nil {
		return nil, e
	}
	visibility := ""
	switch r.GetQueryVisibility() {
	case api.ListPackagesRpcRequestVisibilityEnum_LIST_PACKAGES_RPC_REQUEST_VISIBILITY_ENUM_OFFICIAL:
		visibility = "official"
	case api.ListPackagesRpcRequestVisibilityEnum_LIST_PACKAGES_RPC_REQUEST_VISIBILITY_ENUM_PRIVATE:
		visibility = "private"
	}
	st := "active"
	if r.GetQueryStatus() == api.ListPackagesRpcRequestStatusEnum_LIST_PACKAGES_RPC_REQUEST_STATUS_ENUM_ARCHIVED {
		st = "archived"
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT `+packageColumns+` FROM capability.packages p JOIN capability.namespaces n ON n.id=p.namespace_id WHERE (n.tenant_id=$1 OR p.visibility='official') AND ($2='' OR p.namespace_id=$2) AND ($3='' OR p.visibility=$3) AND p.status=$4 AND ($5='' OR p.name ILIKE '%'||$5||'%') AND p.id>$6 ORDER BY p.id LIMIT $7`, tenant(r.Context), r.GetQueryNamespaceId(), visibility, st, r.GetQuerySearch(), r.GetQueryCursor(), limit(r.GetQueryLimit())+1)
	if e != nil {
		return nil, dbError(e)
	}
	defer rows.Close()
	out := &api.PackagePage{}
	for rows.Next() {
		v, e := scanPackage(rows)
		if e != nil {
			return nil, e
		}
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > limit(r.GetQueryLimit()) {
		out.Items = out.Items[:len(out.Items)-1]
		cursor := out.Items[len(out.Items)-1].Id
		out.NextCursor = &cursor
	}
	return out, dbError(rows.Err())
}
func (s *Service) CreatePackage(ctx context.Context, r *api.CreatePackageRpcRequest) (*api.Package, error) {
	if e := s.auth(ctx, r.GetContext(), "CreatePackage", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, ""); e != nil {
		return nil, e
	}
	if e := requireTenant(r.Context); e != nil {
		return nil, e
	}
	if e := checkName(r.GetBody().GetName()); e != nil {
		return nil, e
	}
	out := &api.Package{}
	e := s.command(ctx, r.Context, "CreatePackage", r.Body, out, func(tx *sql.Tx) error {
		v, e := scanPackage(tx.QueryRowContext(ctx, `INSERT INTO capability.packages AS p(id,namespace_id,name,description,visibility,created_by) SELECT $1,n.id,$2,$3,'private',$4 FROM capability.namespaces n WHERE n.id=$5 AND n.tenant_id=$6 AND n.status='active' RETURNING `+packageColumns, id("pkg"), r.Body.Name, r.Body.Description, r.Context.ActorId, r.Body.NamespaceId, tenant(r.Context)))
		if e == nil {
			proto.Merge(out, v)
		}
		return e
	})
	return out, e
}
func (s *Service) GetPackage(ctx context.Context, r *api.GetPackageRpcRequest) (*api.Package, error) {
	if e := s.auth(ctx, r.GetContext(), "GetPackage", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, r.PackageId); e != nil {
		return nil, e
	}
	return scanPackage(s.DB.QueryRowContext(ctx, `SELECT `+packageColumns+` FROM capability.packages p JOIN capability.namespaces n ON n.id=p.namespace_id WHERE p.id=$1 AND (n.tenant_id=$2 OR p.visibility='official')`, r.PackageId, tenant(r.Context)))
}
func (s *Service) UpdatePackage(ctx context.Context, r *api.UpdatePackageRpcRequest) (*api.Package, error) {
	if e := s.auth(ctx, r.GetContext(), "UpdatePackage", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, r.PackageId); e != nil {
		return nil, e
	}
	if e := checkName(r.GetBody().GetName()); e != nil {
		return nil, e
	}
	return scanPackage(s.DB.QueryRowContext(ctx, `UPDATE capability.packages p SET namespace_id=$1,name=$2,description=$3,updated_at=now() FROM capability.namespaces old,capability.namespaces target WHERE p.id=$4 AND old.id=p.namespace_id AND old.tenant_id=$5 AND target.id=$1 AND target.tenant_id=$5 AND target.status='active' AND p.status='active' RETURNING `+packageColumns, r.Body.NamespaceId, r.Body.Name, r.Body.Description, r.PackageId, tenant(r.Context)))
}
func (s *Service) ArchivePackage(ctx context.Context, r *api.ArchivePackageRpcRequest) (*api.Package, error) {
	if e := s.auth(ctx, r.GetContext(), "ArchivePackage", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, r.PackageId); e != nil {
		return nil, e
	}
	return scanPackage(s.DB.QueryRowContext(ctx, `UPDATE capability.packages p SET status='archived',archived_at=COALESCE(archived_at,now()),updated_at=now() FROM capability.namespaces n WHERE p.id=$1 AND n.id=p.namespace_id AND n.tenant_id=$2 RETURNING `+packageColumns, r.PackageId, tenant(r.Context)))
}

const versionColumns = `v.id,v.package_id,v.version_label,v.status,v.sha256,v.size_bytes,v.created_at`

func scanVersion(r scanner) (*api.PackageVersion, error) {
	v := &api.PackageVersion{}
	var st string
	var created time.Time
	e := r.Scan(&v.Id, &v.PackageId, &v.VersionLabel, &st, &v.Sha256, &v.SizeBytes, &created)
	v.Status = api.PackageVersionStatusEnum(api.PackageVersionStatusEnum_value["PACKAGE_VERSION_STATUS_ENUM_"+upper(st)])
	v.CreatedAt = timestamppb.New(created)
	return v, dbError(e)
}
func (s *Service) GetPackageVersion(ctx context.Context, r *api.GetPackageVersionRpcRequest) (*api.PackageVersion, error) {
	if e := s.auth(ctx, r.GetContext(), "GetPackageVersion", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, r.PackageVersionId); e != nil {
		return nil, e
	}
	return scanVersion(s.DB.QueryRowContext(ctx, `SELECT `+versionColumns+` FROM capability.package_versions v JOIN capability.packages p ON p.id=v.package_id JOIN capability.namespaces n ON n.id=p.namespace_id WHERE v.id=$1 AND (n.tenant_id=$2 OR p.visibility='official')`, r.PackageVersionId, tenant(r.Context)))
}
func (s *Service) ListPackageVersions(ctx context.Context, r *api.ListPackageVersionsRpcRequest) (*api.PackageVersionPage, error) {
	if e := s.auth(ctx, r.GetContext(), "ListPackageVersions", api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, r.PackageId); e != nil {
		return nil, e
	}
	rows, e := s.DB.QueryContext(ctx, `SELECT `+versionColumns+` FROM capability.package_versions v JOIN capability.packages p ON p.id=v.package_id JOIN capability.namespaces n ON n.id=p.namespace_id WHERE p.id=$1 AND (n.tenant_id=$2 OR p.visibility='official') AND v.id>$3 ORDER BY v.id LIMIT $4`, r.PackageId, tenant(r.Context), r.GetQueryCursor(), limit(r.GetQueryLimit())+1)
	if e != nil {
		return nil, dbError(e)
	}
	defer rows.Close()
	out := &api.PackageVersionPage{}
	for rows.Next() {
		v, e := scanVersion(rows)
		if e != nil {
			return nil, e
		}
		out.Items = append(out.Items, v)
	}
	if len(out.Items) > limit(r.GetQueryLimit()) {
		out.Items = out.Items[:len(out.Items)-1]
		cursor := out.Items[len(out.Items)-1].Id
		out.NextCursor = &cursor
	}
	return out, dbError(rows.Err())
}

var _ = status.Error
var _ = codes.NotFound
