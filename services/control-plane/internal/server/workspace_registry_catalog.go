package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

const (
	workspaceRegistryHostEnv         = "OPL_WORKSPACE_REGISTRY_HOST"
	workspaceRegistryUsernameEnv     = "OPL_WORKSPACE_REGISTRY_USERNAME"
	workspaceRegistryPasswordEnv     = "OPL_WORKSPACE_REGISTRY_PASSWORD"
	workspaceRegistryRepositoriesEnv = "OPL_WORKSPACE_REGISTRY_REPOSITORIES"
	workspaceRegistryRequestTimeout  = 30 * time.Second
)

// workspaceApplicationRegistryCatalog is the Control Plane-owned read model
// over the approved registry namespaces. It exists so an operator can pick a
// repository and tag from the real registry and receive the digest-pinned
// reference that admission stores — hand-typed repository@digest input ends
// here.
type workspaceApplicationRegistryCatalog struct {
	client clients.WorkspaceRegistryClient
	// host is the configured registry host this catalog browses. It is an
	// installation fact, so it is read once from the installation environment
	// and reported back to the client instead of being re-derived per request.
	host string
	// declared is the repository set this installation approves for deployment.
	// The catalog reports it instead of enumerating the registry, because a
	// registry credential that reads a repository's tags may still return an
	// empty catalog, and an empty catalog cannot be told apart from an
	// installation that approved nothing.
	declared []contracts.WorkspaceRegistryRepository
}

// workspaceRegistryCatalogFromEnv reads the installation's registry
// configuration. The host has no default: a registry endpoint is an
// installation fact owned by the instance, not a value this product may invent.
//
// Two distinguishable outcomes, and no third:
//
//   - The installation configures no host. Image selection is not part of this
//     installation's capability set, so the catalog is absent and its routes
//     report workspace_registry_unconfigured. Nothing is fabricated and no
//     anonymous request is sent to an unknown endpoint.
//   - The installation configures a host. It must be valid and its credential
//     pair complete; otherwise startup fails, because that is a misconfigured
//     capability rather than an absent one.
func workspaceRegistryCatalogFromEnv() (*workspaceApplicationRegistryCatalog, error) {
	host := strings.TrimSpace(os.Getenv(workspaceRegistryHostEnv))
	if host == "" {
		return nil, nil
	}
	endpoint, err := contracts.WorkspaceRegistryEndpoint(host)
	if err != nil {
		return nil, err
	}
	declared, err := contracts.WorkspaceRegistryDeclaredRepositories(os.Getenv(workspaceRegistryRepositoriesEnv))
	if err != nil {
		return nil, err
	}
	username, password := os.Getenv(workspaceRegistryUsernameEnv), os.Getenv(workspaceRegistryPasswordEnv)
	if username != "" && password == "" || username == "" && password != "" {
		return nil, errors.New("workspace_registry_credential_half_configured")
	}
	config := clients.WorkspaceRegistryConfig{
		Host: strings.TrimPrefix(endpoint, "https://"),
		Credential: clients.RegistryCredential{
			Username: username,
			Password: password,
		},
		Timeout: workspaceRegistryRequestTimeout,
	}
	client, err := clients.NewWorkspaceRegistryHTTPClient(config, nil)
	if err != nil {
		return nil, err
	}
	return &workspaceApplicationRegistryCatalog{client: client, host: strings.TrimPrefix(endpoint, "https://"), declared: declared}, nil
}

type workspaceRegistryCatalogResponse struct {
	Host       string                                  `json:"host"`
	Namespaces []string                                `json:"namespaces"`
	Items      []contracts.WorkspaceRegistryRepository `json:"items,omitempty"`
}

// repositories reports the declared repository set, optionally narrowed to one
// cataloged namespace. It performs no registry enumeration, so an installation
// whose credential cannot list the catalog still reports the repositories it
// actually approved rather than an empty list.
func (catalog *workspaceApplicationRegistryCatalog) repositories(namespace string) (workspaceRegistryCatalogResponse, error) {
	namespaces := contracts.WorkspaceRegistryCatalogNamespaces
	if namespace != "" {
		if err := contracts.ValidateWorkspaceRegistryNamespace(namespace); err != nil {
			return workspaceRegistryCatalogResponse{}, &clients.RegistryAPIError{Operation: "catalog", Status: http.StatusBadRequest, Code: "workspace_registry_namespace_not_cataloged"}
		}
		namespaces = []string{namespace}
	}
	response := workspaceRegistryCatalogResponse{Host: catalog.host, Namespaces: contracts.WorkspaceRegistryCatalogNamespaces, Items: []contracts.WorkspaceRegistryRepository{}}
	for _, name := range namespaces {
		for _, repository := range catalog.declared {
			if repository.Namespace == name {
				response.Items = append(response.Items, repository)
			}
		}
	}
	return response, nil
}

func (catalog *workspaceApplicationRegistryCatalog) tags(ctx context.Context, namespace, repository string) ([]contracts.WorkspaceRegistryTag, error) {
	return catalog.client.ListTags(ctx, namespace, repository)
}

func (catalog *workspaceApplicationRegistryCatalog) resolve(ctx context.Context, namespace, repository, tag string) (contracts.WorkspaceRegistryImageResolution, error) {
	return catalog.client.ResolveTag(ctx, namespace, repository, tag)
}

func registerWorkspaceRegistryCatalogRoutes(mux *http.ServeMux, app *controlPlaneServer) {
	catalog, err := workspaceRegistryCatalogFromEnv()
	if err != nil {
		// A registry misconfiguration must fail startup, not browse time.
		panic(err)
	}
	registerWorkspaceRegistryCatalogRoutesWithCatalog(mux, app, catalog)
}

// registerWorkspaceRegistryCatalogRoutesWithCatalog registers the registry
// catalog routes. A nil catalog means this installation configures no registry
// endpoint; the routes then answer workspace_registry_unconfigured instead of
// returning a plausible empty catalog.
func registerWorkspaceRegistryCatalogRoutesWithCatalog(mux *http.ServeMux, app *controlPlaneServer, catalog *workspaceApplicationRegistryCatalog) {
	mux.HandleFunc("GET /api/operator/registry/repositories", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		if !requireWorkspaceRegistryCatalog(w, catalog) {
			return
		}
		namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
		response, err := catalog.repositories(namespace)
		if err != nil {
			writeWorkspaceRegistryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	}))
	mux.HandleFunc("GET /api/operator/registry/tags/{namespace}/{repository}", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		if !requireWorkspaceRegistryCatalog(w, catalog) {
			return
		}
		namespace, repository, ok := workspaceRegistryPathParams(w, r)
		if !ok {
			return
		}
		tags, err := catalog.tags(r.Context(), namespace, repository)
		if err != nil {
			writeWorkspaceRegistryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"namespace": namespace, "repository": repository, "tags": tags})
	}))
	mux.HandleFunc("POST /api/operator/registry/resolve", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		if !requireWorkspaceRegistryCatalog(w, catalog) {
			return
		}
		input := decodeJSON(r)
		namespace, repository, tag := strings.TrimSpace(stringValue(input["namespace"])), strings.TrimSpace(stringValue(input["repository"])), strings.TrimSpace(stringValue(input["tag"]))
		if namespace == "" || repository == "" || tag == "" {
			writeError(w, http.StatusBadRequest, "workspace_registry_request_invalid")
			return
		}
		if err := contracts.ValidateWorkspaceRegistryRepository(namespace, repository); err != nil {
			writeError(w, http.StatusBadRequest, "workspace_registry_request_invalid")
			return
		}
		resolution, err := catalog.resolve(r.Context(), namespace, repository, tag)
		if err != nil {
			writeWorkspaceRegistryError(w, err)
			return
		}
		// host and reference are returned together so a client confirms the
		// resolved identity against the catalog host the server used, rather
		// than assembling a second reference format of its own.
		writeJSON(w, http.StatusOK, map[string]any{
			"host": catalog.host, "namespace": namespace, "repository": repository, "tag": tag,
			"digest": resolution.Digest, "reference": resolution.Reference,
		})
	}))
}

// requireWorkspaceRegistryCatalog answers the explicit unconfigured result.
// It keeps every registry route on one reason instead of letting an absent
// installation configuration look like an empty registry.
func requireWorkspaceRegistryCatalog(w http.ResponseWriter, catalog *workspaceApplicationRegistryCatalog) bool {
	if catalog != nil {
		return true
	}
	writeError(w, http.StatusServiceUnavailable, "workspace_registry_unconfigured")
	return false
}

func workspaceRegistryPathParams(w http.ResponseWriter, r *http.Request) (namespace, repository string, ok bool) {
	namespace = strings.TrimSpace(r.PathValue("namespace"))
	repository = strings.TrimSpace(r.PathValue("repository"))
	if err := contracts.ValidateWorkspaceRegistryRepository(namespace, repository); err != nil {
		writeError(w, http.StatusBadRequest, "workspace_registry_request_invalid")
		return "", "", false
	}
	return namespace, repository, true
}

// writeWorkspaceRegistryError maps registry failures to their explicit HTTP
// result. Anonymous access that the registry rejects is 401; an unreachable or
// misbehaving registry is 502; nothing degrades into fabricated catalog data.
func writeWorkspaceRegistryError(w http.ResponseWriter, err error) {
	var apiErr *clients.RegistryAPIError
	if !errors.As(err, &apiErr) {
		writeError(w, http.StatusInternalServerError, "workspace_registry_unexpected_failure")
		return
	}
	switch {
	case apiErr.Status == http.StatusBadRequest:
		writeError(w, http.StatusBadRequest, apiErr.Code)
	case apiErr.Status == http.StatusUnauthorized || apiErr.Code == "UNAUTHORIZED" || apiErr.Code == "DENIED":
		writeError(w, http.StatusUnauthorized, "workspace_registry_access_denied")
	case apiErr.Status == http.StatusNotFound || apiErr.Code == "NAME_UNKNOWN":
		writeError(w, http.StatusNotFound, "workspace_registry_target_unknown")
	case apiErr.Status == 0 && (apiErr.Code == "request_timeout" || apiErr.Code == "transport_failure" || apiErr.Code == "request_canceled"):
		writeError(w, http.StatusBadGateway, "workspace_registry_unreachable")
	case apiErr.Status >= 400:
		writeError(w, http.StatusBadGateway, "workspace_registry_unavailable")
	default:
		writeError(w, http.StatusBadRequest, apiErr.Code)
	}
}
