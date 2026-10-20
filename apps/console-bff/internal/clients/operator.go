package clients

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
)

// operatorReadMaximumBytes bounds one operator console readback. The retained
// owner's page DTOs are small; the limit keeps one owner answer from exhausting
// the BFF while still allowing a full account page with provider sub-facts.
const operatorReadMaximumBytes = 4 << 20

// The operator console reads are served by the retained Control Plane owner over
// the same service proof and CloudIdentity session reference the customer reads
// use. The BFF forwards the owner's source envelope unchanged; it does not
// project, recompute or cache the operator facts.
func (c *Clients) OperatorOverview(ctx context.Context, session string) ([]byte, error) {
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, "/internal/cloud/operator/overview", "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorAccounts(ctx context.Context, session string, page, pageSize int) ([]byte, error) {
	path := "/internal/cloud/operator/accounts?" + url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}.Encode()
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorWorkspaces(ctx context.Context, session string, page, pageSize int) ([]byte, error) {
	path := "/internal/cloud/operator/workspaces?" + url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}.Encode()
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorWorkspace(ctx context.Context, session, workspaceID string) ([]byte, error) {
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, "/internal/cloud/operator/workspaces/"+url.PathEscape(workspaceID), "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorWorkspaceRuntimeImagePolicy(ctx context.Context, session string) ([]byte, error) {
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, "/internal/cloud/operator/workspace-runtime-image-policy", "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorAnnouncements(ctx context.Context, session string, page, pageSize int) ([]byte, error) {
	path := "/internal/cloud/operator/announcements?" + url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}.Encode()
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorWorkspaceRuntimeImageReplacementPreview(ctx context.Context, session, workspaceID, replacementImageDigest string) ([]byte, error) {
	path := "/internal/cloud/operator/workspaces/" + url.PathEscape(workspaceID) + "/runtime-image-replacements/preview"
	if replacementImageDigest != "" {
		path += "?" + url.Values{"replacementImageDigest": {replacementImageDigest}}.Encode()
	}
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorHealth(ctx context.Context, session string) ([]byte, error) {
	path := "/internal/cloud/operator/health"
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorRuntimeObservations(ctx context.Context, session string) ([]byte, error) {
	path := "/internal/cloud/operator/runtime-observations"
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorReconciliation(ctx context.Context, session string, page, pageSize int) ([]byte, error) {
	path := "/internal/cloud/operator/reconciliation?" + url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}.Encode()
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorWorkspaceLaunchRecovery(ctx context.Context, session string, operationId string) ([]byte, error) {
	path := "/internal/cloud/operator/workspace-launches/" + url.PathEscape(operationId) + "/recovery"
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorWalletAdjustment(ctx context.Context, session string, operationId string) ([]byte, error) {
	path := "/internal/cloud/operator/wallet-adjustments/" + url.PathEscape(operationId)
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorWorkspaceRuntimeImageReplacement(ctx context.Context, session string, workspaceId, operationId string) ([]byte, error) {
	path := "/internal/cloud/operator/workspaces/" + url.PathEscape(workspaceId) + "/runtime-image-replacements/" + url.PathEscape(operationId)
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorApplicationDeployment(ctx context.Context, session string, operationId string) ([]byte, error) {
	path := "/internal/cloud/operator/application-deployments/" + url.PathEscape(operationId)
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorRegistryRepositories(ctx context.Context, session string, namespace string) ([]byte, error) {
	path := "/internal/cloud/operator/registry/repositories?" + url.Values{"namespace": {namespace}}.Encode()
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}

func (c *Clients) OperatorRegistryTags(ctx context.Context, session string, namespace, repository string) ([]byte, error) {
	path := "/internal/cloud/operator/registry/tags/" + url.PathEscape(namespace) + "/" + url.PathEscape(repository)
	return c.controlPlane.requestRaw(ctx, session, http.MethodGet, path, "", operatorReadMaximumBytes)
}
