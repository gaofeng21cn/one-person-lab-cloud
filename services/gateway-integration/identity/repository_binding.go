package identity

import (
	"context"
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

// authenticatedUserDestination resolves the OCI destination of the actor that
// just authenticated.
//
// The Gateway owner holds the only live destination mapping, so the answer
// comes from there alone: either the owner already holds a usable destination
// for this actor, or the installation registry derives a candidate from the
// verified email. The frozen Tenant history is not read here. The one-time
// handover moved the attributable historical destinations into the owner, and
// a historical destination nobody owns is a residual the startup gate refuses
// to serve rather than a fact this path may fall back to.
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
	return s.registryHost, s.registryNamespace, repositorySlugCandidate(email), nil
}
