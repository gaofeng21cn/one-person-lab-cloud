package contracts

import (
	"errors"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// WorkspaceRegistryCatalogNamespaces lists the only registry namespaces a
// deployment selection may browse. The namespace boundary keeps the catalog
// server-side: a caller can enumerate and resolve images inside these
// namespaces, never outside them.
var WorkspaceRegistryCatalogNamespaces = []string{"oplcloud"}

// WorkspaceRegistryDeclaredRepositories parses the repositories an installation
// approves for deployment, written as "<namespace>/<repository>" entries.
//
// Which images may run in a Workspace is an installation decision, so the
// approved set is declared rather than discovered. A registry's catalog
// endpoint is not a dependable source for it: the same credential that reads a
// repository's tags can return an empty catalog, which is indistinguishable
// from an installation that has approved nothing. The declared list is also the
// narrower statement: it names what is approved instead of everything that
// exists.
//
// Every entry passes the same namespace boundary and repository validation as a
// lookup, so a declaration cannot widen what may be browsed. Order and
// duplicates are normalized, and the result is sorted so that two installations
// declaring the same set produce the same value.
func WorkspaceRegistryDeclaredRepositories(value string) ([]WorkspaceRegistryRepository, error) {
	repositories := make([]WorkspaceRegistryRepository, 0)
	seen := map[string]bool{}
	for _, entry := range strings.Split(value, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		namespace, repository, found := strings.Cut(entry, "/")
		if !found {
			return nil, errors.New("workspace_registry_repository_entry_invalid")
		}
		namespace, repository = strings.TrimSpace(namespace), strings.TrimSpace(repository)
		if err := ValidateWorkspaceRegistryRepository(namespace, repository); err != nil {
			return nil, errors.New("workspace_registry_repository_entry_invalid")
		}
		key := namespace + "/" + repository
		if seen[key] {
			continue
		}
		seen[key] = true
		repositories = append(repositories, WorkspaceRegistryRepository{Namespace: namespace, Repository: repository})
	}
	if len(repositories) == 0 {
		return nil, errors.New("workspace_registry_repository_entry_invalid")
	}
	sort.Slice(repositories, func(i, j int) bool {
		if repositories[i].Namespace != repositories[j].Namespace {
			return repositories[i].Namespace < repositories[j].Namespace
		}
		return repositories[i].Repository < repositories[j].Repository
	})
	return repositories, nil
}

type WorkspaceRegistryRepository struct {
	// Namespace is the TCR namespace the repository lives in; one catalog
	// request browses exactly one namespace.
	Namespace  string `json:"namespace"`
	Repository string `json:"repository"`
}

type WorkspaceRegistryTag struct {
	Tag string `json:"tag"`
}

type WorkspaceRegistryImageResolution struct {
	// Reference is the digest-pinned image reference that downstream
	// admission and Fabric execution consume. Tags stay discovery input.
	Reference string `json:"reference"`
	Digest    string `json:"digest"`
}

var (
	workspaceRegistryNamespacePattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,58}[a-z0-9]$`)
	workspaceRegistryRepositoryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._/-]{0,126}[a-z0-9]$`)
)

// ValidateWorkspaceRegistryNamespace enforces the server-side namespace
// boundary. Names outside the catalog namespaces are a client error, not a
// registry lookup.
func ValidateWorkspaceRegistryNamespace(namespace string) error {
	for _, allowed := range WorkspaceRegistryCatalogNamespaces {
		if namespace == allowed {
			return nil
		}
	}
	return errors.New("workspace_registry_namespace_not_cataloged")
}

func ValidateWorkspaceRegistryRepository(namespace, repository string) error {
	if err := ValidateWorkspaceRegistryNamespace(namespace); err != nil {
		return err
	}
	if !workspaceRegistryRepositoryPattern.MatchString(repository) {
		return errors.New("workspace_registry_repository_invalid")
	}
	return nil
}

// WorkspaceRegistryEndpoint returns the API base URL of the configured
// registry host. Only https endpoints are accepted.
func WorkspaceRegistryEndpoint(host string) (string, error) {
	host = strings.TrimSpace(host)
	parsed, err := url.Parse("https://" + host)
	if err != nil || parsed.Hostname() == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", errors.New("workspace_registry_host_invalid")
	}
	if parsed.Port() != "" {
		if _, err := parseRegistryPort(parsed.Port()); err != nil {
			return "", errors.New("workspace_registry_host_invalid")
		}
	}
	if !strings.Contains(parsed.Hostname(), ".") && parsed.Hostname() != "localhost" {
		return "", errors.New("workspace_registry_host_invalid")
	}
	return "https://" + parsed.Host, nil
}

func parseRegistryPort(port string) (int, error) {
	value := 0
	for _, digit := range port {
		if digit < '0' || digit > '9' {
			return 0, errors.New("workspace_registry_host_invalid")
		}
		value = value*10 + int(digit-'0')
	}
	if value < 1 || value > 65535 {
		return 0, errors.New("workspace_registry_host_invalid")
	}
	return value, nil
}

// WorkspaceRegistryImageReference assembles the registry host, namespace and
// repository into the repository path used by API v2 requests and image
// references. The namespace is a real path segment, not a string prefix.
func WorkspaceRegistryImageReference(host, namespace, repository, digest string) (string, error) {
	if err := ValidateWorkspaceRegistryRepository(namespace, repository); err != nil {
		return "", err
	}
	if !workspaceImageDigestPattern.MatchString(digest) {
		return "", errors.New("workspace_registry_digest_invalid")
	}
	endpoint, err := WorkspaceRegistryEndpoint(host)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(endpoint, "https://") + "/" + namespace + "/" + repository + "@" + digest, nil
}

// SplitWorkspaceImageReference is the inverse of WorkspaceRegistryImageReference:
// it reads the registry host, the repository path inside the cataloged
// namespace, and the pinned digest out of one executable image reference. The
// caller decides what the host and repository are allowed to be; this function
// only refuses a reference that cannot be read back into those parts.
func SplitWorkspaceImageReference(value string) (host, namespace, repository, digest string, err error) {
	value = strings.TrimSpace(value)
	repositoryPath, digest, found := strings.Cut(value, "@")
	if !found || !workspaceImageDigestPattern.MatchString(digest) {
		return "", "", "", "", errors.New("workspace_registry_image_reference_invalid")
	}
	host, repositoryPath, found = strings.Cut(repositoryPath, "/")
	if !found {
		return "", "", "", "", errors.New("workspace_registry_image_reference_invalid")
	}
	// The host carries no scheme and no path of its own; the endpoint validator
	// rejects anything a registry host cannot be.
	host = strings.TrimPrefix(host, "https://")
	if _, err = WorkspaceRegistryEndpoint(host); err != nil {
		return "", "", "", "", errors.New("workspace_registry_image_reference_invalid")
	}
	namespace, repository, found = strings.Cut(repositoryPath, "/")
	if !found {
		return "", "", "", "", errors.New("workspace_registry_image_reference_invalid")
	}
	// This is the reference's shape, not an installation's approval: which
	// namespaces a registry request may browse and which repositories an
	// installation approves for deployment are decided by the caller that owns
	// that boundary, not by reading a reference.
	if !workspaceRegistryNamespacePattern.MatchString(namespace) || !workspaceRegistryRepositoryPattern.MatchString(repository) {
		return "", "", "", "", errors.New("workspace_registry_image_reference_invalid")
	}
	return host, namespace, repository, digest, nil
}
