// Package identity owns Cloud sessions and authorization within Gateway Integration.
package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GatewayIdentity struct {
	ID     int64  `json:"id"`
	Email  string `json:"email"`
	Status string `json:"status"`
}
type Gateway struct {
	base   string
	client *http.Client
	// directory is this service's own configured Gateway identity, used only for
	// the read-only administrative directory lookup that resolves a subject's
	// display name or confirms a subject exists. It never acts as a member: the
	// service presents its own credential to the directory, and no member request
	// is ever made under it.
	directory *GatewayDirectory

	mu    sync.Mutex
	token string
	until time.Time
}

// GatewayDirectory is the bounded administrative identity CloudIdentity uses to
// read the Gateway directory. Both values are required together or the directory
// capability is absent.
type GatewayDirectory struct {
	Email    string
	Password string
}

func NewGateway(raw string) (*Gateway, error) {
	return NewGatewayWithDirectory(raw, nil)
}

// NewGatewayWithDirectory builds the adapter and, when a directory identity is
// supplied, enables the administrative directory read. Supplying half an identity
// is a misconfiguration and is refused.
func NewGatewayWithDirectory(raw string, directory *GatewayDirectory) (*Gateway, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return nil, fmt.Errorf("explicit HTTPS Gateway URL required; loopback HTTP allowed for isolated tests")
	}
	var configured *GatewayDirectory
	if directory != nil {
		email := strings.ToLower(strings.TrimSpace(directory.Email))
		if email == "" || directory.Password == "" {
			return nil, fmt.Errorf("the Gateway directory identity requires both an email and a password")
		}
		configured = &GatewayDirectory{Email: email, Password: directory.Password}
	}
	return &Gateway{base: strings.TrimRight(raw, "/"), client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, directory: configured}, nil
}

// DirectoryConfigured reports whether this service may read the Gateway
// directory. A caller that needs a display name or a subject check asks first, so
// an unconfigured deployment reports the fact as unresolved instead of inventing
// it.
func (g *Gateway) DirectoryConfigured() bool { return g != nil && g.directory != nil }

// displayName resolves one subject's display name through the Gateway directory.
// It returns an empty name, never a fabricated one, when the subject has no
// resolvable name.
func (g *Gateway) displayName(ctx context.Context, subject string) (string, error) {
	identity, err := g.directoryIdentity(ctx, subject)
	if err != nil {
		return "", err
	}
	return identity.Email, nil
}

// directoryIdentity reads one subject from the administrative directory. The
// service authenticates as its own configured identity; it never presents a
// member credential and never mutates anything.
func (g *Gateway) directoryIdentity(ctx context.Context, subject string) (GatewayIdentity, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(subject), 10, 64)
	if err != nil || id <= 0 {
		return GatewayIdentity{}, status.Error(codes.InvalidArgument, "a Gateway subject id is required")
	}
	if !g.DirectoryConfigured() {
		return GatewayIdentity{}, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return GatewayIdentity{}, err
	}
	var out GatewayIdentity
	if err = g.request(ctx, "/api/v1/admin/users/"+strconv.FormatInt(id, 10), token, nil, &out); err != nil {
		if status.Code(err) == codes.Unauthenticated {
			// The cached administrative credential is no longer accepted; drop it so
			// the next call re-authenticates instead of failing permanently.
			g.mu.Lock()
			g.token, g.until = "", time.Time{}
			g.mu.Unlock()
			return GatewayIdentity{}, status.Error(codes.Unauthenticated, "Gateway directory credential rejected")
		}
		return GatewayIdentity{}, err
	}
	if out.ID != id || out.ID <= 0 || (out.Status != "active" && out.Status != "disabled") {
		return GatewayIdentity{}, status.Error(codes.FailedPrecondition, "Gateway directory returned a different subject")
	}
	return out, nil
}

// directoryToken returns a cached administrative access token, authenticating as
// this service's own directory identity when none is live.
func (g *Gateway) directoryToken(ctx context.Context) (string, error) {
	g.mu.Lock()
	if g.token != "" && g.until.After(time.Now().Add(time.Minute)) {
		token := g.token
		g.mu.Unlock()
		return token, nil
	}
	g.mu.Unlock()
	var out struct {
		AccessToken string          `json:"access_token"`
		User        GatewayIdentity `json:"user"`
	}
	if err := g.request(ctx, "/api/v1/auth/login", "", map[string]string{"email": g.directory.Email, "password": g.directory.Password, "turnstile_token": ""}, &out); err != nil {
		return "", err
	}
	if out.AccessToken == "" || out.User.ID <= 0 || strings.ToLower(out.User.Email) != g.directory.Email || out.User.Status != "active" {
		return "", status.Error(codes.Unauthenticated, "Gateway directory identity rejected")
	}
	g.mu.Lock()
	g.token, g.until = out.AccessToken, time.Now().Add(30*time.Minute)
	g.mu.Unlock()
	return out.AccessToken, nil
}
func (g *Gateway) request(ctx context.Context, path, token string, body any, out any) error {
	var raw []byte
	var err error
	method := "GET"
	if body != nil {
		raw, err = json.Marshal(body)
		method = "POST"
		if err != nil {
			return err
		}
	}
	r, err := http.NewRequestWithContext(ctx, method, g.base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := g.client.Do(r)
	if err != nil {
		return status.Error(codes.Unavailable, "Gateway authentication unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return status.Error(codes.Unauthenticated, "Gateway identity rejected")
	}
	if response.StatusCode == 429 {
		return status.Error(codes.ResourceExhausted, "Gateway login rate limited")
	}
	if response.StatusCode != 200 {
		return status.Error(codes.Unavailable, "Gateway authentication unavailable")
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || json.Unmarshal(data, &envelope) != nil || envelope.Code != 0 || json.Unmarshal(envelope.Data, out) != nil {
		return status.Error(codes.Unavailable, "invalid Gateway identity response")
	}
	return nil
}
func (g *Gateway) Login(ctx context.Context, user, password string) (GatewayIdentity, string, error) {
	var out struct {
		AccessToken string          `json:"access_token"`
		User        GatewayIdentity `json:"user"`
	}
	user = strings.ToLower(strings.TrimSpace(user))
	if user == "" || password == "" {
		return out.User, "", status.Error(codes.InvalidArgument, "credentials required")
	}
	err := g.request(ctx, "/api/v1/auth/login", "", map[string]string{"email": user, "password": password, "turnstile_token": ""}, &out)
	if err != nil {
		return out.User, "", err
	}
	if out.AccessToken == "" || out.User.ID <= 0 || strings.ToLower(out.User.Email) != user || out.User.Status != "active" {
		return out.User, "", status.Error(codes.Unauthenticated, "Gateway identity rejected")
	}
	return out.User, out.AccessToken, nil
}
func (g *Gateway) Read(ctx context.Context, token, actor string) (GatewayIdentity, error) {
	var out GatewayIdentity
	err := g.request(ctx, "/api/v1/auth/me", token, nil, &out)
	if err != nil {
		return out, err
	}
	if strconv.FormatInt(out.ID, 10) != actor || out.ID <= 0 || out.Status != "active" {
		return out, status.Error(codes.Unauthenticated, "Gateway identity is no longer active")
	}
	return out, nil
}
