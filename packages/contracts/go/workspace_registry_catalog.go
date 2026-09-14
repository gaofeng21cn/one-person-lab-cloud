package contracts

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// WorkspaceRegistryCatalogNamespaces lists the only registry namespaces a
// deployment selection may browse. The namespace boundary keeps the catalog
// server-side: a caller can enumerate and resolve images inside these
// namespaces, never outside them.
var WorkspaceRegistryCatalogNamespaces = []string{"oplcloud"}

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
