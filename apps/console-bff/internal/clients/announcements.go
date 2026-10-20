package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// controlPlaneReader is a typed HTTP client to the retained owner. It forwards
// only the live session reference; the owner resolves the User through Gateway.
type controlPlaneReader struct {
	base  string
	token string
	http  *http.Client
}

func newControlPlaneReader(raw, token string) (*controlPlaneReader, error) {
	if raw == "" && token == "" {
		return nil, nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(token) < owneridentity.MinimumTokenLength {
		return nil, fmt.Errorf("Control Plane reads require an absolute HTTP(S) origin and independent token of at least %d characters", owneridentity.MinimumTokenLength)
	}
	return &controlPlaneReader{base: strings.TrimRight(raw, "/"), token: token, http: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (c *controlPlaneReader) request(ctx context.Context, session, method, path, key string, out any) error {
	return c.requestWithLimit(ctx, session, method, path, key, out, 1<<20)
}

func (c *controlPlaneReader) requestWithLimit(ctx context.Context, session, method, path, key string, out any, maxBytes int64) error {
	raw, err := c.requestRaw(ctx, session, method, path, key, maxBytes)
	if err != nil {
		return err
	}
	if json.Unmarshal(raw, out) != nil {
		return status.Error(codes.DataLoss, "invalid retained owner response")
	}
	return nil
}

// requestRaw returns the owner's response body unchanged. The caller validates
// the retained envelope it forwards; the BFF never rewrites an owner's facts.
func (c *controlPlaneReader) requestRaw(ctx context.Context, session, method, path, key string, maxBytes int64) ([]byte, error) {
	if c == nil {
		return nil, status.Error(codes.Unavailable, "retained owner is not configured")
	}
	if maxBytes <= 0 {
		return nil, status.Error(codes.InvalidArgument, "invalid owner read limit")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, nil)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid owner request")
	}
	req.Header.Set(owneridentity.PeerHeader, owneridentity.ConsoleBFF.String())
	req.Header.Set(owneridentity.TokenHeader, c.token)
	req.Header.Set(contracts.CloudSessionHeader, session)
	req.Header.Set("Accept", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := c.http.Do(req)
	if err != nil {
		return nil, status.Error(codes.Unavailable, "retained owner is unavailable")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, status.Error(codes.Unauthenticated, "Cloud session or owner identity rejected")
	case http.StatusForbidden:
		return nil, status.Error(codes.PermissionDenied, "original owner access denied")
	case http.StatusNotFound:
		return nil, status.Error(codes.NotFound, "original owner record not found")
	case http.StatusBadRequest:
		return nil, status.Error(codes.InvalidArgument, "invalid original owner request")
	case http.StatusConflict:
		return nil, status.Error(codes.FailedPrecondition, "original owner identity is unresolved")
	default:
		return nil, status.Error(codes.Unavailable, "retained owner is unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || int64(len(raw)) > maxBytes {
		return nil, status.Error(codes.DataLoss, "invalid retained owner response")
	}
	return raw, nil
}

func (c *Clients) ListAnnouncements(ctx context.Context, session string, page, pageSize int) (*contracts.AnnouncementSource, error) {
	out := &contracts.AnnouncementSource{}
	path := "/internal/cloud/announcements?" + url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}.Encode()
	if err := c.controlPlane.requestWithLimit(ctx, session, http.MethodGet, path, "", out, contracts.AnnouncementMaxResponseBytes); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Clients) MarkAnnouncementRead(ctx context.Context, session, id, key string) (*contracts.AnnouncementRead, error) {
	out := &contracts.AnnouncementRead{}
	if err := c.controlPlane.request(ctx, session, http.MethodPost, "/internal/cloud/announcements/"+url.PathEscape(id)+"/read", key, out); err != nil {
		return nil, err
	}
	return out, nil
}
