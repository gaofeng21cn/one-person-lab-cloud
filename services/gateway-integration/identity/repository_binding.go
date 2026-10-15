package identity

import (
	"context"
	"database/sql"
	"regexp"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var repositoryNamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,126}[a-z0-9])?$`)

// repositorySlugCandidate derives the initial repository-name candidate from a
// verified email local-part. It is only a candidate: the actor id remains the
// authorization identity. The database rejects destination collisions.
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
		return "user"
	}
	return slug
}

// authenticatedUserDestination performs the historical handover within the
// CloudIdentity owner. GatewayStore never reads the Tenant database. The old
// table is read-only; an ambiguous historical owner is never guessed.
func (s *Service) authenticatedUserDestination(ctx context.Context, actorID, email string) (string, string, string, error) {
	existing, err := s.GatewayStore.ReadUserExternalBinding(ctx, actorID)
	if err == nil && existing.RegistryHost != "" {
		if _, err = s.bindingReadback(existing); err != nil {
			return "", "", "", err
		}
		return existing.RegistryHost, existing.RegistryNamespace, existing.Repository, nil
	}
	if err != nil && status.Code(err) != codes.NotFound {
		return "", "", "", err
	}
	if s.registryHost == "" || s.registryNamespace == "" {
		return "", "", "", nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT b.registry_host,b.registry_namespace,b.repository,b.status,t.status,
  (SELECT count(*) FROM tenant.tenant_members owners WHERE owners.tenant_id=t.id AND owners.role='owner' AND owners.revoked_at IS NULL)
  FROM tenant.tenant_members m JOIN tenant.tenants t ON t.id=m.tenant_id
  JOIN tenant.tenant_repository_bindings b ON b.tenant_id=t.id
  WHERE m.actor_id=$1 AND m.role='owner' AND m.revoked_at IS NULL`, actorID)
	if err != nil {
		return "", "", "", persistence(err)
	}
	var host, namespace, repository string
	count := 0
	for rows.Next() {
		var bindingStatus, tenantStatus string
		var owners int
		if err = rows.Scan(&host, &namespace, &repository, &bindingStatus, &tenantStatus, &owners); err != nil {
			rows.Close()
			return "", "", "", persistence(err)
		}
		count++
		if count > 1 || owners != 1 || tenantStatus != "active" || (bindingStatus != "reserved" && bindingStatus != "active") {
			rows.Close()
			return "", "", "", status.Error(codes.FailedPrecondition, "historical OCI destination has ambiguous or inactive ownership")
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", "", "", persistence(err)
	}
	if count == 1 {
		if host != s.registryHost || namespace != s.registryNamespace {
			return "", "", "", status.Error(codes.FailedPrecondition, "historical OCI destination conflicts with installation registry")
		}
		return host, namespace, repository, nil
	}
	repository = repositorySlugCandidate(email)
	if err = s.checkHistoricalDestination(ctx, actorID, s.registryHost, s.registryNamespace, repository); err != nil {
		return "", "", "", err
	}
	return s.registryHost, s.registryNamespace, repository, nil
}

// checkHistoricalDestination protects every historical reservation, including a
// retired or ownerless Tenant. Only its single active owner may adopt its path.
func (s *Service) checkHistoricalDestination(ctx context.Context, actorID, host, namespace, repository string) error {
	var tenantStatus, bindingStatus string
	var owners, matchingOwners int
	err := s.DB.QueryRowContext(ctx, `SELECT t.status,b.status,
  (SELECT count(*) FROM tenant.tenant_members m WHERE m.tenant_id=t.id AND m.role='owner' AND m.revoked_at IS NULL),
  (SELECT count(*) FROM tenant.tenant_members m WHERE m.tenant_id=t.id AND m.role='owner' AND m.revoked_at IS NULL AND m.actor_id=$4)
  FROM tenant.tenant_repository_bindings b JOIN tenant.tenants t ON t.id=b.tenant_id
  WHERE b.registry_host=$1 AND b.registry_namespace=$2 AND b.repository=$3`, host, namespace, repository, actorID).Scan(&tenantStatus, &bindingStatus, &owners, &matchingOwners)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return persistence(err)
	}
	if tenantStatus != "active" || (bindingStatus != "active" && bindingStatus != "reserved") || owners != 1 || matchingOwners != 1 {
		return status.Error(codes.FailedPrecondition, "OCI destination is reserved by historical Tenant ownership")
	}
	return nil
}
