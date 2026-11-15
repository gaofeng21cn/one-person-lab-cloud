package fabric

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// The workspace image trust source is the admission decision for images that
// may start a workspace. The Control Plane owns it, so a publisher registers
// a repository once and every provider sees it — no environment file and no
// allowlist file is edited to make a customer's build output startable.
//
// The published release catalog stays a separate fact: it is the version
// directory, and its exact image match is unchanged.

const (
	// The Control Plane is a deployment unit of the product. Its address is
	// fixed by the topology, and the environment may override it.
	defaultControlPlaneURL = "http://control-plane:8787"
	// A trust source older than this is refreshed before the next admission
	// decision, so a registration takes effect without a restart.
	workspaceImageTrustSourceTTL = 30 * time.Second
)

// workspaceImageTrustSourceClient keeps the registered trust source current.
type workspaceImageTrustSourceClient struct {
	url    string
	token  string
	client *http.Client

	mu        sync.RWMutex
	source    contracts.WorkspaceImageTrustSource
	refreshed time.Time
}

func newWorkspaceImageTrustSourceClient(getenv func(string) string) *workspaceImageTrustSourceClient {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	url := strings.TrimSpace(getenv("OPL_CONTROL_PLANE_URL"))
	if url == "" {
		url = defaultControlPlaneURL
	}
	return &workspaceImageTrustSourceClient{
		url:    strings.TrimSuffix(url, "/"),
		token:  strings.TrimSpace(getenv("OPL_INTERNAL_SERVICE_TOKEN")),
		client: &http.Client{Timeout: 3 * time.Second},
	}
}

// Refresh reads the registered trust source from the Control Plane. A failed
// read keeps the last known source: an unreachable Control Plane must not
// widen the admission decision, and it must not silently empty it either.
func (c *workspaceImageTrustSourceClient) Refresh(ctx context.Context) error {
	if c == nil {
		return nil
	}
	if c.token == "" {
		return fmt.Errorf("workspace_image_trust_source_token_missing")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url+contracts.WorkspaceImageTrustSourcePath, nil)
	if err != nil {
		return err
	}
	request.Header.Set(contracts.WorkspaceImageTrustSourceTokenHeader, c.token)
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("workspace_image_trust_source_read_status_%d", response.StatusCode)
	}
	var envelope struct {
		Data contracts.WorkspaceImageTrustSource `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return err
	}
	source, err := contracts.DecodeWorkspaceImageTrustSource(mustEncodeJSON(envelope.Data))
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.source = source
	c.refreshed = time.Now()
	c.mu.Unlock()
	return nil
}

// Source returns the last known trust source.
func (c *workspaceImageTrustSourceClient) Source() contracts.WorkspaceImageTrustSource {
	if c == nil {
		return contracts.WorkspaceImageTrustSource{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.source
}

// stale reports whether the cached source is older than the refresh window.
func (c *workspaceImageTrustSourceClient) stale() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return time.Since(c.refreshed) >= workspaceImageTrustSourceTTL
}

// admit refreshes the source when it is stale and reports whether the image
// reference is trusted by the registered source.
func (c *workspaceImageTrustSourceClient) admit(ctx context.Context, reference string) bool {
	if c == nil {
		return false
	}
	if c.stale() {
		_ = c.Refresh(ctx)
	}
	return c.Source().TrustsImage(reference)
}

func mustEncodeJSON(value contracts.WorkspaceImageTrustSource) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(encoded)
}
