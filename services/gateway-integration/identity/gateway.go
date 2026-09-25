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
}

func NewGateway(raw string) (*Gateway, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost"))) {
		return nil, fmt.Errorf("explicit HTTPS Gateway URL required; loopback HTTP allowed for isolated tests")
	}
	return &Gateway{strings.TrimRight(raw, "/"), &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
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
