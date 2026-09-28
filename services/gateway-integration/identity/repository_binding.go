package identity

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// A repository name is one lowercase path segment. The Tenant owner reserves it
// and never accepts a destination from a caller.
var repositoryNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,126}[a-z0-9])?$`)

func stampOf(v time.Time) *timestamppb.Timestamp { return timestamppb.New(v) }

// repositorySlugCandidate derives the initial repository-name candidate from a
// verified email local-part. It is only a candidate: the Tenant id remains the
// authorization identity and a collision gets a deterministic suffix.
func repositorySlugCandidate(email string) string {
	local := strings.TrimSpace(email)
	if at := strings.LastIndex(local, "@"); at >= 0 {
		local = local[:at]
	}
	local = strings.ToLower(local)
	var b strings.Builder
	last := rune(0)
	for _, r := range local {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			last = r
		case r == '.' || r == '_' || r == '-':
			if b.Len() > 0 && last != '.' && last != '_' && last != '-' {
				b.WriteRune(r)
				last = r
			}
		default:
			if b.Len() > 0 && last != '.' && last != '_' && last != '-' {
				b.WriteRune('-')
				last = '-'
			}
		}
	}
	slug := strings.Trim(b.String(), ".-_")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], ".-_")
	}
	if slug == "" || !repositoryNamePattern.MatchString(slug) {
		return "tenant"
	}
	return slug
}

// reserveRepositoryBinding reserves one stable tenant_id -> repository binding.
// The candidate is used only when free; otherwise a deterministic numeric
// suffix is chosen. An existing binding is never changed, and a caller-supplied
// destination is never accepted.
func (s *Service) reserveRepositoryBinding(ctx context.Context, tx *sql.Tx, tenantID, email string) (*api.TenantRepositoryBinding, error) {
	var existing api.TenantRepositoryBinding
	var createdAt, updatedAt time.Time
	e := tx.QueryRowContext(ctx, `SELECT tenant_id,registry_host,registry_namespace,repository,status,created_at,updated_at FROM tenant.tenant_repository_bindings WHERE tenant_id=$1`, tenantID).Scan(&existing.TenantId, &existing.RegistryHost, &existing.RegistryNamespace, &existing.Repository, &existing.Status, &createdAt, &updatedAt)
	if e == nil {
		existing.CreatedAt, existing.UpdatedAt = stampOf(createdAt), stampOf(updatedAt)
		return &existing, nil
	}
	if e != sql.ErrNoRows {
		return nil, persistence(e)
	}
	if s.registryHost == "" || s.registryNamespace == "" {
		return nil, status.Error(codes.FailedPrecondition, "installation registry host and namespace are required to reserve a Tenant repository")
	}
	base := repositorySlugCandidate(email)
	candidate := base
	for attempt := 1; attempt < 1000; attempt++ {
		var taken bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant.tenant_repository_bindings WHERE registry_host=$1 AND registry_namespace=$2 AND repository=$3)`, s.registryHost, s.registryNamespace, candidate).Scan(&taken); e != nil {
			return nil, persistence(e)
		}
		if !taken {
			break
		}
		candidate = fmt.Sprintf("%s-%d", base, attempt)
	}
	out := &api.TenantRepositoryBinding{TenantId: tenantID, RegistryHost: s.registryHost, RegistryNamespace: s.registryNamespace, Repository: candidate, Status: "reserved"}
	if e := tx.QueryRowContext(ctx, `INSERT INTO tenant.tenant_repository_bindings(tenant_id,registry_host,registry_namespace,repository) VALUES($1,$2,$3,$4) RETURNING created_at,updated_at`, out.TenantId, out.RegistryHost, out.RegistryNamespace, out.Repository).Scan(&createdAt, &updatedAt); e != nil {
		return nil, persistence(e)
	}
	out.CreatedAt, out.UpdatedAt = stampOf(createdAt), stampOf(updatedAt)
	return out, nil
}

// GetTenantRepositoryBinding is a cross-owner coordination read. Build resolves
// its immutable output destination here before a Build job is created; the
// destination is never accepted from a request body. The verified mTLS peer plus
// the requested tenant is the authorization, so no interactive role row applies.
func (s *Service) GetTenantRepositoryBinding(ctx context.Context, r *api.GetTenantRepositoryBindingRequest) (*api.TenantRepositoryBinding, error) {
	peer, ok := ownerservice.PeerOwner(ctx)
	if !ok || (peer != owneridentity.Build.Service() && peer != owneridentity.ConsoleBFF) {
		return nil, denied()
	}
	tenantID := strings.TrimSpace(r.GetTenantId())
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant id is required")
	}
	out := &api.TenantRepositoryBinding{}
	var createdAt, updatedAt time.Time
	e := s.DB.QueryRowContext(ctx, `SELECT tenant_id,registry_host,registry_namespace,repository,status,created_at,updated_at FROM tenant.tenant_repository_bindings WHERE tenant_id=$1`, tenantID).Scan(&out.TenantId, &out.RegistryHost, &out.RegistryNamespace, &out.Repository, &out.Status, &createdAt, &updatedAt)
	if e == sql.ErrNoRows {
		return nil, status.Error(codes.NotFound, "tenant repository binding not found")
	}
	if e != nil {
		return nil, persistence(e)
	}
	out.CreatedAt, out.UpdatedAt = stampOf(createdAt), stampOf(updatedAt)
	return out, nil
}
