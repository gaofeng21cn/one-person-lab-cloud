package clients

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// WorkspaceRegistryClient browses one registry namespace through the OCI
// Distribution API and resolves a tag to its digest-pinned image reference.
// It is a read-only discovery client: it never pushes, deletes or rewrites
// registry state, and it never receives Secret material.
// WorkspaceRegistryClient resolves what a deployment selection needs from one
// repository: its tags and the digest a tag currently names. It deliberately has
// no enumeration: which repositories an installation approves is declared
// configuration, and a registry's catalog endpoint is not a dependable source
// for it.
type WorkspaceRegistryClient interface {
	ListTags(ctx context.Context, namespace, repository string) ([]contracts.WorkspaceRegistryTag, error)
	ResolveTag(ctx context.Context, namespace, repository, tag string) (contracts.WorkspaceRegistryImageResolution, error)
	ImageFacts(ctx context.Context, namespace, repository, digest, platform string) (WorkspaceRegistryImageFacts, error)
}

// WorkspaceRegistryImageFacts is what one digest-pinned image declares about
// its own runtime shape: the TCP ports it exposes, the filesystem paths it
// marks as data, and the process identity it expects. A deployment description
// can then be derived from the image instead of being invented by the platform
// or retyped by an operator. The image is read through the Distribution API
// only; nothing here mutates registry state.
type WorkspaceRegistryImageFacts struct {
	Ports   []int
	Volumes []string
	User    string
}

type WorkspaceRegistryConfig struct {
	Host string
	// Credential carries an authenticated registry identity. An empty
	// credential browses anonymously; a failed anonymous probe on a private
	// namespace is an explicit error, never a silent downgrade.
	Credential RegistryCredential
	Timeout    time.Duration
	Client     *http.Client
}

type RegistryCredential struct {
	Username string
	Password string
}

// registryDigestPattern is the pinned-digest shape every registry reference in
// this client must carry. Tags are discovery input and never reach here.
var registryDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

const (
	maxRegistryResponseBytes = 8 << 20 // 8 MiB: bounded catalog/tag/manifest reads
	maxRegistryPages         = 50
	defaultRegistryTimeout   = 30 * time.Second
)

type workspaceRegistryHTTPClient struct {
	config WorkspaceRegistryConfig
	client *http.Client
}

func NewWorkspaceRegistryHTTPClient(config WorkspaceRegistryConfig, client *http.Client) (WorkspaceRegistryClient, error) {
	if config.Credential.Username != "" && config.Credential.Password == "" ||
		config.Credential.Username == "" && config.Credential.Password != "" {
		return nil, errors.New("workspace_registry_credential_half_configured")
	}
	endpoint, err := contracts.WorkspaceRegistryEndpoint(config.Host)
	if err != nil {
		return nil, err
	}
	config.Host = endpoint
	if config.Timeout <= 0 {
		config.Timeout = defaultRegistryTimeout
	}
	if client == nil {
		client = &http.Client{}
	}
	return &workspaceRegistryHTTPClient{config: config, client: client}, nil
}

type RegistryAPIError struct {
	Operation string
	Status    int
	Code      string
	Detail    string
}

func (e *RegistryAPIError) Error() string {
	return fmt.Sprintf("workspace_registry_%s_failed: http_%d %s %s", e.Operation, e.Status, e.Code, e.Detail)
}

func (c *workspaceRegistryHTTPClient) get(ctx context.Context, operation, path string, query url.Values, accept []string) (*http.Response, []byte, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()
	target := c.config.Host + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("workspace_registry_%s_request_invalid", operation)
	}
	req.Header.Set("Accept", strings.Join(accept, ", "))
	if c.config.Credential.Username != "" {
		req.SetBasicAuth(c.config.Credential.Username, c.config.Credential.Password)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return nil, nil, registryTransportError(operation, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxRegistryResponseBytes+1))
	if err != nil {
		return nil, nil, &RegistryAPIError{Operation: operation, Status: res.StatusCode, Code: "response_read_failure"}
	}
	if len(body) > maxRegistryResponseBytes {
		return nil, nil, &RegistryAPIError{Operation: operation, Status: res.StatusCode, Code: "response_too_large"}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return res, body, registryFailure(operation, res, body)
	}
	return res, body, nil
}

func registryTransportError(operation string, err error) error {
	code := "transport_failure"
	if errors.Is(err, context.DeadlineExceeded) {
		code = "request_timeout"
	} else if errors.Is(err, context.Canceled) {
		code = "request_canceled"
	}
	return &RegistryAPIError{Operation: operation, Status: 0, Code: code}
}

func registryFailure(operation string, res *http.Response, body []byte) *RegistryAPIError {
	apiErr := &RegistryAPIError{Operation: operation, Status: res.StatusCode, Code: fmt.Sprintf("http_%d", res.StatusCode)}
	var envelope struct {
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &envelope) == nil && len(envelope.Errors) > 0 {
		apiErr.Code = envelope.Errors[0].Code
		apiErr.Detail = envelope.Errors[0].Message
	}
	return apiErr
}

// workspaceRegistryTokenCache holds one negotiated bearer token per scope.
// TCR issues its own token even for anonymous pulls; every namespace request
// reuses the cached token for its scope until the registry reports 401.
type workspaceRegistryTokenCache struct {
	token     string
	scope     string
	issuedAt  time.Time
	expiresIn time.Duration
}

func (c *workspaceRegistryHTTPClient) bearerToken(ctx context.Context, scope string) (string, error) {
	challenge, err := c.pingAuthChallenge(ctx, scope)
	if err != nil {
		return "", err
	}
	if challenge == nil {
		// No auth challenge: the endpoint allows plain v2 access.
		return "", nil
	}
	if cached := c.tokenForScope(challenge.scope); cached != "" {
		return cached, nil
	}
	token, err := c.negotiateToken(ctx, challenge)
	if err != nil {
		return "", err
	}
	return token, nil
}

type registryAuthChallenge struct {
	realm   string
	scope   string
	service string
}

func (c *workspaceRegistryHTTPClient) tokenForScope(scope string) string {
	return ""
}

func (c *workspaceRegistryHTTPClient) pingAuthChallenge(ctx context.Context, scope string) (*registryAuthChallenge, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.Host+"/v2/", nil)
	if err != nil {
		return nil, &RegistryAPIError{Operation: "ping", Code: "request_invalid"}
	}
	if c.config.Credential.Username != "" {
		req.SetBasicAuth(c.config.Credential.Username, c.config.Credential.Password)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return nil, registryTransportError("ping", err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	switch {
	case res.StatusCode == http.StatusOK:
		return nil, nil
	case res.StatusCode == http.StatusUnauthorized:
		header := res.Header.Get("WWW-Authenticate")
		challenge, err := parseWWWAuthenticateBearer(header)
		if err != nil {
			return nil, &RegistryAPIError{Operation: "ping", Status: res.StatusCode, Code: "auth_challenge_unsupported"}
		}
		challenge.scope = scope
		return challenge, nil
	default:
		return nil, &RegistryAPIError{Operation: "ping", Status: res.StatusCode, Code: fmt.Sprintf("http_%d", res.StatusCode)}
	}
}

func parseWWWAuthenticateBearer(header string) (*registryAuthChallenge, error) {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, errors.New("workspace_registry_auth_challenge_missing")
	}
	challenge := &registryAuthChallenge{}
	for _, part := range strings.Split(strings.TrimPrefix(header, "Bearer "), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		value = strings.Trim(value, `"`)
		switch key {
		case "realm":
			challenge.realm = value
		case "service":
			challenge.service = value
		case "scope":
			challenge.scope = value
		}
	}
	if challenge.realm == "" {
		return nil, errors.New("workspace_registry_auth_realm_missing")
	}
	return challenge, nil
}

func (c *workspaceRegistryHTTPClient) negotiateToken(ctx context.Context, challenge *registryAuthChallenge) (string, error) {
	target, err := url.Parse(challenge.realm)
	if err != nil {
		return "", &RegistryAPIError{Operation: "auth", Code: "auth_realm_invalid"}
	}
	if target.Scheme != "https" && target.Scheme != "http" {
		return "", &RegistryAPIError{Operation: "auth", Code: "auth_realm_invalid"}
	}
	query := target.Query()
	if challenge.service != "" {
		query.Set("service", challenge.service)
	}
	if challenge.scope != "" {
		query.Set("scope", challenge.scope)
	}
	target.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", &RegistryAPIError{Operation: "auth", Code: "auth_request_invalid"}
	}
	if c.config.Credential.Username != "" {
		req.SetBasicAuth(c.config.Credential.Username, c.config.Credential.Password)
	}
	res, err := c.client.Do(req)
	if err != nil {
		return "", registryTransportError("auth", err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxRegistryResponseBytes))
	if err != nil {
		return "", &RegistryAPIError{Operation: "auth", Status: res.StatusCode, Code: "response_read_failure"}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", registryFailure("auth", res, body)
	}
	var payload struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if json.Unmarshal(body, &payload) != nil || (payload.Token == "" && payload.AccessToken == "") {
		return "", &RegistryAPIError{Operation: "auth", Code: "auth_token_unavailable"}
	}
	token := payload.Token
	if token == "" {
		token = payload.AccessToken
	}
	_ = payload.ExpiresIn
	return token, nil
}

func (c *workspaceRegistryHTTPClient) authorize(ctx context.Context, operation, scope string, request func(bearer string) (*http.Response, []byte, error)) (*http.Response, []byte, error) {
	token, err := c.bearerToken(ctx, scope)
	if err != nil {
		return nil, nil, err
	}
	res, body, err := request(token)
	if err != nil {
		var apiErr *RegistryAPIError
		if errors.As(err, &apiErr) && apiErr.Status == http.StatusUnauthorized {
			// One fresh negotiation after expiry; loops are registry bugs.
			token, err = c.negotiateFreshToken(ctx, scope)
			if err != nil {
				return nil, nil, err
			}
			return request(token)
		}
		return nil, nil, err
	}
	return res, body, nil
}

func (c *workspaceRegistryHTTPClient) negotiateFreshToken(ctx context.Context, scope string) (string, error) {
	challenge, err := c.pingAuthChallenge(ctx, scope)
	if err != nil {
		return "", err
	}
	if challenge == nil {
		return "", nil
	}
	return c.negotiateToken(ctx, challenge)
}

func (c *workspaceRegistryHTTPClient) ListTags(ctx context.Context, namespace, repository string) ([]contracts.WorkspaceRegistryTag, error) {
	if err := contracts.ValidateWorkspaceRegistryRepository(namespace, repository); err != nil {
		return nil, err
	}
	scope := "repository:" + namespace + "/" + repository + ":pull"
	_, body, err := c.authorize(ctx, "tags", scope, func(bearer string) (*http.Response, []byte, error) {
		query := url.Values{"n": []string{"1000"}}
		return c.get(ctx, "tags", "/v2/"+namespace+"/"+repository+"/tags/list", query, []string{"application/json"})
	})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Tags []string `json:"tags"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil, &RegistryAPIError{Operation: "tags", Code: "tags_response_invalid"}
	}
	tags := make([]contracts.WorkspaceRegistryTag, 0, len(payload.Tags))
	seen := map[string]bool{}
	for _, tag := range payload.Tags {
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, contracts.WorkspaceRegistryTag{Tag: tag})
	}
	sort.Slice(tags, func(i, j int) bool { return tags[i].Tag < tags[j].Tag })
	return tags, nil
}

var registryManifestAccept = []string{
	"application/vnd.oci.image.manifest.v1+json",
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.docker.distribution.manifest.v2+json",
	"application/vnd.docker.distribution.manifest.list.v2+json",
}

func (c *workspaceRegistryHTTPClient) ResolveTag(ctx context.Context, namespace, repository, tag string) (contracts.WorkspaceRegistryImageResolution, error) {
	if err := contracts.ValidateWorkspaceRegistryRepository(namespace, repository); err != nil {
		return contracts.WorkspaceRegistryImageResolution{}, err
	}
	tag = strings.TrimSpace(tag)
	if tag == "" || len(tag) > 128 || strings.ContainsAny(tag, " /@") {
		return contracts.WorkspaceRegistryImageResolution{}, errors.New("workspace_registry_tag_invalid")
	}
	scope := "repository:" + namespace + "/" + repository + ":pull"
	res, _, err := c.authorize(ctx, "manifest", scope, func(bearer string) (*http.Response, []byte, error) {
		return c.get(ctx, "manifest", "/v2/"+namespace+"/"+repository+"/manifests/"+tag, nil, registryManifestAccept)
	})
	if err != nil {
		return contracts.WorkspaceRegistryImageResolution{}, err
	}
	digest := res.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return contracts.WorkspaceRegistryImageResolution{}, &RegistryAPIError{Operation: "manifest", Code: "manifest_digest_missing"}
	}
	reference, err := contracts.WorkspaceRegistryImageReference(registryHostOf(c.config.Host), namespace, repository, digest)
	if err != nil {
		return contracts.WorkspaceRegistryImageResolution{}, err
	}
	return contracts.WorkspaceRegistryImageResolution{Reference: reference, Digest: digest}, nil
}

func registryHostOf(endpoint string) string {
	return strings.TrimPrefix(endpoint, "https://")
}

// registryManifestIndexMediaTypes are the manifest media types that list
// per-platform children instead of describing one image directly.
var registryImageIndexMediaTypes = map[string]struct{}{
	"application/vnd.oci.image.index.v1+json":                   {},
	"application/vnd.docker.distribution.manifest.list.v2+json": {},
}

// ImageFacts reads the declared runtime facts of one digest-pinned image for
// the requested platform. An index is resolved to its child manifest for that
// platform first, so the facts always describe the image the deployment will
// actually run. A repository, digest or platform the registry cannot answer for
// is an explicit error; nothing is defaulted.
func (c *workspaceRegistryHTTPClient) ImageFacts(ctx context.Context, namespace, repository, digest, platform string) (WorkspaceRegistryImageFacts, error) {
	facts := WorkspaceRegistryImageFacts{}
	if err := contracts.ValidateWorkspaceRegistryRepository(namespace, repository); err != nil {
		return facts, err
	}
	if !registryDigestPattern.MatchString(digest) {
		return facts, &RegistryAPIError{Operation: "facts", Code: "image_digest_invalid"}
	}
	operatingSystem, architecture, found := strings.Cut(strings.TrimSpace(platform), "/")
	if !found || operatingSystem == "" || architecture == "" {
		return facts, &RegistryAPIError{Operation: "facts", Code: "image_platform_invalid"}
	}
	scope := "repository:" + namespace + "/" + repository + ":pull"
	manifest, err := c.fetchManifest(ctx, scope, namespace, repository, digest)
	if err != nil {
		return facts, err
	}
	if _, isIndex := registryImageIndexMediaTypes[stringValueOf(manifest["mediaType"])]; isIndex {
		child, err := registryImageIndexChild(manifest, operatingSystem, architecture)
		if err != nil {
			return facts, err
		}
		if manifest, err = c.fetchManifest(ctx, scope, namespace, repository, child); err != nil {
			return facts, err
		}
	}
	configDigest := stringValueOf(nestedMapValue(manifest, "config", "digest"))
	if !registryDigestPattern.MatchString(configDigest) {
		return facts, &RegistryAPIError{Operation: "facts", Code: "image_config_digest_missing"}
	}
	_, body, err := c.authorize(ctx, "config", scope, func(bearer string) (*http.Response, []byte, error) {
		return c.get(ctx, "config", "/v2/"+namespace+"/"+repository+"/blobs/"+configDigest, nil, []string{"application/json"})
	})
	if err != nil {
		return facts, err
	}
	return decodeRegistryImageFacts(body)
}

func (c *workspaceRegistryHTTPClient) fetchManifest(ctx context.Context, scope, namespace, repository, reference string) (map[string]any, error) {
	_, body, err := c.authorize(ctx, "manifest", scope, func(bearer string) (*http.Response, []byte, error) {
		return c.get(ctx, "manifest", "/v2/"+namespace+"/"+repository+"/manifests/"+reference, nil, registryManifestAccept)
	})
	if err != nil {
		return nil, err
	}
	var manifest map[string]any
	if json.Unmarshal(body, &manifest) != nil {
		return nil, &RegistryAPIError{Operation: "manifest", Code: "manifest_response_invalid"}
	}
	return manifest, nil
}

// registryImageIndexChild selects the one child manifest that matches the
// requested platform. An index that carries no such child cannot be deployed on
// that platform, so it is refused rather than guessed.
func registryImageIndexChild(manifest map[string]any, operatingSystem, architecture string) (string, error) {
	children, _ := manifest["manifests"].([]any)
	for _, candidate := range children {
		entry, ok := candidate.(map[string]any)
		if !ok {
			continue
		}
		childPlatform, _ := entry["platform"].(map[string]any)
		if stringValueOf(childPlatform["os"]) != operatingSystem || stringValueOf(childPlatform["architecture"]) != architecture {
			continue
		}
		digest := stringValueOf(entry["digest"])
		if registryDigestPattern.MatchString(digest) {
			return digest, nil
		}
	}
	return "", &RegistryAPIError{Operation: "manifest", Code: "image_platform_unsupported"}
}

func decodeRegistryImageFacts(body []byte) (WorkspaceRegistryImageFacts, error) {
	facts := WorkspaceRegistryImageFacts{}
	var payload struct {
		Config struct {
			User         string              `json:"User"`
			ExposedPorts map[string]struct{} `json:"ExposedPorts"`
			Volumes      map[string]struct{} `json:"Volumes"`
		} `json:"config"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return facts, &RegistryAPIError{Operation: "config", Code: "image_config_response_invalid"}
	}
	seenPorts := map[int]bool{}
	for exposed := range payload.Config.ExposedPorts {
		portText, protocol, found := strings.Cut(exposed, "/")
		port, err := strconv.Atoi(portText)
		if !found || err != nil || strings.ToLower(protocol) != "tcp" || port < 1 || port > 65535 || seenPorts[port] {
			continue
		}
		seenPorts[port] = true
		facts.Ports = append(facts.Ports, port)
	}
	sort.Ints(facts.Ports)
	seenVolumes := map[string]bool{}
	for path := range payload.Config.Volumes {
		if !strings.HasPrefix(path, "/") || strings.Contains(path, "..") || seenVolumes[path] {
			continue
		}
		seenVolumes[path] = true
		facts.Volumes = append(facts.Volumes, path)
	}
	sort.Strings(facts.Volumes)
	facts.User = strings.TrimSpace(payload.Config.User)
	return facts, nil
}

func stringValueOf(value any) string {
	text, _ := value.(string)
	return text
}

func nestedMapValue(value map[string]any, keys ...string) any {
	current := any(value)
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = object[key]
	}
	return current
}
