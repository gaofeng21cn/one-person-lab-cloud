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
	workspaceRegistryHostEnv        = "OPL_WORKSPACE_REGISTRY_HOST"
	workspaceRegistryUsernameEnv    = "OPL_WORKSPACE_REGISTRY_USERNAME"
	workspaceRegistryPasswordEnv    = "OPL_WORKSPACE_REGISTRY_PASSWORD"
	workspaceRegistryDefaultHost    = "uswccr.ccs.tencentyun.com"
	workspaceRegistryRequestTimeout = 30 * time.Second
)

// workspaceApplicationRegistryCatalog is the Control Plane-owned read model
// over the approved registry namespaces. It exists so an operator can pick a
// repository and tag from the real registry and receive the digest-pinned
// reference that admission stores — hand-typed repository@digest input ends
// here.
type workspaceApplicationRegistryCatalog struct {
	client clients.WorkspaceRegistryClient
}

func workspaceRegistryCatalogFromEnv() (*workspaceApplicationRegistryCatalog, error) {
	host := strings.TrimSpace(os.Getenv(workspaceRegistryHostEnv))
	if host == "" {
		host = workspaceRegistryDefaultHost
	}
	if os.Getenv(workspaceRegistryUsernameEnv) != "" && os.Getenv(workspaceRegistryPasswordEnv) == "" ||
		os.Getenv(workspaceRegistryUsernameEnv) == "" && os.Getenv(workspaceRegistryPasswordEnv) != "" {
		return nil, errors.New("workspace_registry_credential_half_configured")
	}
	config := clients.WorkspaceRegistryConfig{
		Host: host,
		Credential: clients.RegistryCredential{
			Username: os.Getenv(workspaceRegistryUsernameEnv),
			Password: os.Getenv(workspaceRegistryPasswordEnv),
		},
		Timeout: workspaceRegistryRequestTimeout,
	}
	client, err := clients.NewWorkspaceRegistryHTTPClient(config, nil)
	if err != nil {
		return nil, err
	}
	return &workspaceApplicationRegistryCatalog{client: client}, nil
}

type workspaceRegistryCatalogResponse struct {
	Host       string                                  `json:"host"`
	Namespaces []string                                `json:"namespaces"`
	Items      []contracts.WorkspaceRegistryRepository `json:"items,omitempty"`
}

func (catalog *workspaceApplicationRegistryCatalog) repositories(ctx context.Context, namespace string) (workspaceRegistryCatalogResponse, error) {
	host := strings.TrimSpace(os.Getenv(workspaceRegistryHostEnv))
	if host == "" {
		host = workspaceRegistryDefaultHost
	}
	endpoint, err := contracts.WorkspaceRegistryEndpoint(host)
	if err != nil {
		return workspaceRegistryCatalogResponse{}, err
	}
	namespaces := contracts.WorkspaceRegistryCatalogNamespaces
	if namespace != "" {
		if err := contracts.ValidateWorkspaceRegistryNamespace(namespace); err != nil {
			return workspaceRegistryCatalogResponse{}, &clients.RegistryAPIError{Operation: "catalog", Status: http.StatusBadRequest, Code: "workspace_registry_namespace_not_cataloged"}
		}
		namespaces = []string{namespace}
	}
	response := workspaceRegistryCatalogResponse{Host: strings.TrimPrefix(endpoint, "https://"), Namespaces: contracts.WorkspaceRegistryCatalogNamespaces}
	for _, name := range namespaces {
		items, err := catalog.client.ListRepositories(ctx, name)
		if err != nil {
			return workspaceRegistryCatalogResponse{}, err
		}
		response.Items = append(response.Items, items...)
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

func registerWorkspaceRegistryCatalogRoutesWithCatalog(mux *http.ServeMux, app *controlPlaneServer, catalog *workspaceApplicationRegistryCatalog) {
	mux.HandleFunc("GET /api/operator/registry/repositories", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		namespace := strings.TrimSpace(r.URL.Query().Get("namespace"))
		response, err := catalog.repositories(r.Context(), namespace)
		if err != nil {
			writeWorkspaceRegistryError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, response)
	}))
	mux.HandleFunc("GET /api/operator/registry/tags/{namespace}/{repository}", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, http.StatusOK, map[string]any{
			"namespace": namespace, "repository": repository, "tag": tag,
			"digest": resolution.Digest, "reference": resolution.Reference,
		})
	}))
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
