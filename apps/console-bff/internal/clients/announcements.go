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
	if c == nil {
		return status.Error(codes.Unavailable, "announcement owner is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, nil)
	if err != nil {
		return status.Error(codes.InvalidArgument, "invalid owner request")
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
		return status.Error(codes.Unavailable, "announcement owner is unavailable")
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return status.Error(codes.Unauthenticated, "Cloud session or owner identity rejected")
	case http.StatusForbidden:
		return status.Error(codes.PermissionDenied, "announcement access denied")
	case http.StatusNotFound:
		return status.Error(codes.NotFound, "announcement is not active")
	case http.StatusBadRequest:
		return status.Error(codes.InvalidArgument, "invalid announcement request")
	case http.StatusConflict:
		return status.Error(codes.FailedPrecondition, "original announcement identity is unresolved")
	default:
		return status.Error(codes.Unavailable, "announcement owner is unavailable")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, out) != nil {
		return status.Error(codes.DataLoss, "invalid announcement owner response")
	}
	return nil
}

func (c *Clients) ListAnnouncements(ctx context.Context, session string, page, pageSize int) (*contracts.AnnouncementSource, error) {
	out := &contracts.AnnouncementSource{}
	path := "/internal/cloud/announcements?" + url.Values{"page": {strconv.Itoa(page)}, "pageSize": {strconv.Itoa(pageSize)}}.Encode()
	if err := c.controlPlane.request(ctx, session, http.MethodGet, path, "", out); err != nil {
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
