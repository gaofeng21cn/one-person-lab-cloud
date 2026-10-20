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
