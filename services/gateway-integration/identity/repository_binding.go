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

// legacyRepositorySlug gives a pre-binding Tenant a stable destination without
// re-reading an old owner email from Gateway. Tenant ids are already durable
// authorization identities, so this preserves existing artifact history while
// avoiding a dependency on an external identity lookup during startup.
func legacyRepositorySlug(tenantID string) string {
	return "tenant-" + hash(tenantID)[:24]
}

func (s *Service) reserveRepositoryBindingCandidate(ctx context.Context, tx *sql.Tx, tenantID, base string) (*api.TenantRepositoryBinding, error) {
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
	for attempt := 0; attempt < 1000; attempt++ {
		candidate := base
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d", base, attempt)
		}
		// The uniqueness index is the durable guard. This transaction-scoped lock
		// serializes the read-then-insert reservation among concurrent admissions.
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, s.registryHost+"/"+s.registryNamespace+"/"+candidate); e != nil {
			return nil, persistence(e)
		}
		var taken bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant.tenant_repository_bindings WHERE registry_host=$1 AND registry_namespace=$2 AND repository=$3)`, s.registryHost, s.registryNamespace, candidate).Scan(&taken); e != nil {
			return nil, persistence(e)
		}
		if taken {
			continue
		}
		out := &api.TenantRepositoryBinding{TenantId: tenantID, RegistryHost: s.registryHost, RegistryNamespace: s.registryNamespace, Repository: candidate, Status: "reserved"}
		if e := tx.QueryRowContext(ctx, `INSERT INTO tenant.tenant_repository_bindings(tenant_id,registry_host,registry_namespace,repository) VALUES($1,$2,$3,$4) RETURNING created_at,updated_at`, out.TenantId, out.RegistryHost, out.RegistryNamespace, out.Repository).Scan(&createdAt, &updatedAt); e != nil {
			return nil, persistence(e)
		}
		out.CreatedAt, out.UpdatedAt = stampOf(createdAt), stampOf(updatedAt)
		return out, nil
	}
	return nil, status.Error(codes.ResourceExhausted, "no Tenant repository destination is available")
}

// reserveRepositoryBinding reserves one stable tenant_id -> repository binding.
// The candidate is used only when free; otherwise a deterministic numeric
// suffix is chosen. An existing binding is never changed, and a caller-supplied
// destination is never accepted.
func (s *Service) reserveRepositoryBinding(ctx context.Context, tx *sql.Tx, tenantID, email string) (*api.TenantRepositoryBinding, error) {
	return s.reserveRepositoryBindingCandidate(ctx, tx, tenantID, repositorySlugCandidate(email))
}

// BackfillTenantRepositoryBindings reserves stable destinations for Tenants
// admitted before tenant_repository_bindings existed. It runs at Tenant-owner
// startup after the installation registry facts are configured, before requests
// can reach Build. It never consults Gateway or replaces an existing binding.
func (s *Service) BackfillTenantRepositoryBindings(ctx context.Context) error {
	if s.registryHost == "" || s.registryNamespace == "" {
		var missing bool
		e := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant.tenants t WHERE NOT EXISTS(SELECT 1 FROM tenant.tenant_repository_bindings b WHERE b.tenant_id=t.id))`).Scan(&missing)
		if e != nil {
			return persistence(e)
		}
		if missing {
			return status.Error(codes.FailedPrecondition, "installation registry host and namespace are required to backfill existing Tenant repositories")
		}
		return nil
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return persistence(e)
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('tenant_repository_bindings_backfill',0))`); e != nil {
		return persistence(e)
	}
	rows, e := tx.QueryContext(ctx, `SELECT t.id FROM tenant.tenants t WHERE NOT EXISTS(SELECT 1 FROM tenant.tenant_repository_bindings b WHERE b.tenant_id=t.id) ORDER BY t.id`)
	if e != nil {
		return persistence(e)
	}
	var tenantIDs []string
	for rows.Next() {
		var tenantID string
		if e = rows.Scan(&tenantID); e != nil {
			rows.Close()
			return persistence(e)
		}
		tenantIDs = append(tenantIDs, tenantID)
	}
	if e = rows.Err(); e != nil {
		rows.Close()
		return persistence(e)
	}
	if e = rows.Close(); e != nil {
		return persistence(e)
	}
	for _, tenantID := range tenantIDs {
		if _, e = s.reserveRepositoryBindingCandidate(ctx, tx, tenantID, legacyRepositorySlug(tenantID)); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return persistence(e)
	}
	return nil
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
