// Package identity owns Cloud sessions and authorization within Gateway Integration.
package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type GatewayIdentity struct {
	ID     int64  `json:"id"`
	Email  string `json:"email"`
	Status string `json:"status"`
}

type GatewayBalance struct {
	UserID    int64
	USDMicros int64
	Status    string
}

type GatewayUsageStats struct {
	TotalRequests            int64
	TotalInputTokens         int64
	TotalOutputTokens        int64
	TotalTokens              int64
	TotalActualCostUSDMicros int64
}

type GatewayDirectoryUser struct {
	ID                 int64
	Email              string
	Status             string
	BalanceUSDMicros   int64
	BalanceUnavailable bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type GatewayDirectoryUserPage struct {
	Items    []GatewayDirectoryUser
	Total    int64
	Page     int
	PageSize int
	Pages    int
}

type GatewayPlatformUsage struct {
	Platform                 string
	TodayActualCostUSDMicros int64
	TotalActualCostUSDMicros int64
}

type GatewayBatchUserUsage struct {
	UserID                   int64
	TodayActualCostUSDMicros int64
	TotalActualCostUSDMicros int64
	ByPlatform               []GatewayPlatformUsage
}

// ErrGatewayAccountUnknown reports that the Gateway directory holds no account
// for the requested email.
var ErrGatewayAccountUnknown = errors.New("gateway account not found")

// ErrGatewayAccountConflict reports that the Gateway directory holds more than
// one account for the requested email, so no single account can be adopted.
var ErrGatewayAccountConflict = errors.New("gateway account is ambiguous")

// gatewayUserPageSize is the page size the Gateway account list is read with.
const gatewayUserPageSize = 100
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
			g.invalidateDirectoryToken()
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
func (g *Gateway) invalidateDirectoryToken() {
	g.mu.Lock()
	g.token, g.until = "", time.Time{}
	g.mu.Unlock()
}

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

// UserByEmail resolves the Gateway account that carries this exact email.
//
// The directory search is the native readback an account-opening command
// converges on: Sub2API has no idempotency key for account creation, so the
// owner confirms an account it may already have created by reading it back
// instead of writing a second one.
func (g *Gateway) UserByEmail(ctx context.Context, email string) (GatewayIdentity, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return GatewayIdentity{}, status.Error(codes.InvalidArgument, "an email is required")
	}
	if !g.DirectoryConfigured() {
		return GatewayIdentity{}, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return GatewayIdentity{}, err
	}
	matches := make([]GatewayIdentity, 0, 1)
	for page := 1; ; page++ {
		values := url.Values{
			"page": {strconv.Itoa(page)}, "page_size": {strconv.Itoa(gatewayUserPageSize)},
			"search": {email}, "sort_by": {"id"}, "sort_order": {"asc"},
		}
		var data struct {
			Items    []GatewayIdentity `json:"items"`
			Total    int64             `json:"total"`
			Page     int               `json:"page"`
			PageSize int               `json:"page_size"`
			Pages    int               `json:"pages"`
		}
		if err = g.request(ctx, "/api/v1/admin/users?"+values.Encode(), token, nil, &data); err != nil {
			if status.Code(err) == codes.Unauthenticated {
				g.invalidateDirectoryToken()
			}
			return GatewayIdentity{}, err
		}
		if data.Page != page || data.PageSize != gatewayUserPageSize || data.Total < 0 || data.Pages < 1 {
			return GatewayIdentity{}, status.Error(codes.Unavailable, "invalid Gateway account page")
		}
		for _, item := range data.Items {
			if strings.ToLower(strings.TrimSpace(item.Email)) == email {
				matches = append(matches, item)
			}
		}
		if page >= data.Pages {
			break
		}
	}
	switch len(matches) {
	case 1:
		if matches[0].ID <= 0 || (matches[0].Status != "active" && matches[0].Status != "disabled") {
			return GatewayIdentity{}, status.Error(codes.FailedPrecondition, "the Gateway returned an invalid account")
		}
		return matches[0], nil
	case 0:
		return GatewayIdentity{}, ErrGatewayAccountUnknown
	default:
		return GatewayIdentity{}, ErrGatewayAccountConflict
	}
}

// CreateUser creates one Gateway account for a customer.
//
// The Gateway provider has no idempotency key for account creation, so this
// write is dispatched at most once per owner command: the caller confirms the
// outcome with UserByEmail instead of repeating it.
func (g *Gateway) CreateUser(ctx context.Context, email, password string) (GatewayIdentity, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" || password == "" {
		return GatewayIdentity{}, status.Error(codes.InvalidArgument, "an email and a password are required")
	}
	if !g.DirectoryConfigured() {
		return GatewayIdentity{}, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return GatewayIdentity{}, err
	}
	var out GatewayIdentity
	if err = g.request(ctx, "/api/v1/admin/users", token, map[string]string{"email": email, "password": password, "role": "user"}, &out); err != nil {
		return GatewayIdentity{}, err
	}
	return out, nil
}

func (g *Gateway) Login(ctx context.Context, user, password string) (GatewayIdentity, string, time.Time, error) {
	var out struct {
		AccessToken string          `json:"access_token"`
		ExpiresIn   json.RawMessage `json:"expires_in"`
		User        GatewayIdentity `json:"user"`
	}
	user = strings.ToLower(strings.TrimSpace(user))
	if user == "" || password == "" {
		return out.User, "", time.Time{}, status.Error(codes.InvalidArgument, "credentials required")
	}
	started := time.Now()
	err := g.request(ctx, "/api/v1/auth/login", "", map[string]string{"email": user, "password": password, "turnstile_token": ""}, &out)
	if err != nil {
		return out.User, "", time.Time{}, err
	}
	if out.AccessToken == "" || out.User.ID <= 0 || strings.ToLower(out.User.Email) != user || out.User.Status != "active" {
		return out.User, "", time.Time{}, status.Error(codes.Unauthenticated, "Gateway identity rejected")
	}
	return out.User, out.AccessToken, delegatedExpiry(out.AccessToken, out.ExpiresIn, started), nil
}

// LoginForServiceGrant verifies a Gateway user's password (plus optional TOTP
// code for 2FA-enabled accounts) without persisting passwords or upstream
// refresh tokens. If the upstream account requires 2FA and no totpCode was
// supplied, it returns requires2FA=true without an identity.
func (g *Gateway) LoginForServiceGrant(ctx context.Context, user, password, totpCode string) (GatewayIdentity, bool, error) {
	user = strings.ToLower(strings.TrimSpace(user))
	totpCode = strings.TrimSpace(totpCode)
	if user == "" || len(user) > 254 || password == "" || len(password) > 512 {
		return GatewayIdentity{}, false, status.Error(codes.InvalidArgument, "credentials required")
	}
	var out struct {
		Requires2FA  bool            `json:"requires_2fa"`
		TempToken    string          `json:"temp_token"`
		AccessToken  string          `json:"access_token"`
		RefreshToken string          `json:"refresh_token"`
		User         GatewayIdentity `json:"user"`
	}
	err := g.request(ctx, "/api/v1/auth/login", "", map[string]string{
		"email":           user,
		"password":        password,
		"turnstile_token": "",
	}, &out)
	if err != nil {
		return GatewayIdentity{}, false, err
	}
	if out.Requires2FA || strings.TrimSpace(out.TempToken) != "" {
		if totpCode == "" {
			return GatewayIdentity{}, true, nil
		}
		if strings.TrimSpace(out.TempToken) == "" {
			return GatewayIdentity{}, false, status.Error(codes.Unavailable, "Gateway 2FA challenge missing temporary token")
		}
		var out2fa struct {
			AccessToken  string          `json:"access_token"`
			RefreshToken string          `json:"refresh_token"`
			User         GatewayIdentity `json:"user"`
		}
		if err = g.request(ctx, "/api/v1/auth/login/2fa", "", map[string]string{
			"temp_token": out.TempToken,
			"totp_code":  totpCode,
		}, &out2fa); err != nil {
			return GatewayIdentity{}, false, err
		}
		out.AccessToken = out2fa.AccessToken
		out.RefreshToken = out2fa.RefreshToken
		out.User = out2fa.User
	}
	if out.RefreshToken != "" && out.AccessToken != "" {
		logoutCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		var ignored json.RawMessage
		_ = g.request(logoutCtx, "/api/v1/auth/logout", out.AccessToken, map[string]string{"refresh_token": out.RefreshToken}, &ignored)
		cancel()
	}
	out.User.Email = strings.ToLower(strings.TrimSpace(out.User.Email))
	if out.AccessToken == "" || out.User.ID <= 0 || out.User.Email != user || out.User.Status != "active" {
		return GatewayIdentity{}, false, status.Error(codes.Unauthenticated, "Gateway identity rejected")
	}
	return out.User, false, nil
}

// AuthenticateBearer validates an existing Gateway bearer access token via
// /api/v1/auth/me and ensures the subject is active.
func (g *Gateway) AuthenticateBearer(ctx context.Context, token string) (GatewayIdentity, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 4096 {
		return GatewayIdentity{}, status.Error(codes.Unauthenticated, "Gateway bearer token required")
	}
	var out GatewayIdentity
	if err := g.request(ctx, "/api/v1/auth/me", token, nil, &out); err != nil {
		return GatewayIdentity{}, err
	}
	out.Email = strings.ToLower(strings.TrimSpace(out.Email))
	if out.ID <= 0 || out.Email == "" || out.Status != "active" {
		return GatewayIdentity{}, status.Error(codes.Unauthenticated, "Gateway identity is not active")
	}
	return out, nil
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

// VerifyBearer authenticates a live OPL Gateway bearer token against /api/v1/auth/me
// and ensures the Gateway subject is currently active.
func (g *Gateway) VerifyBearer(ctx context.Context, token string) (GatewayIdentity, error) {
	var out GatewayIdentity
	if g == nil || strings.TrimSpace(g.base) == "" {
		return out, status.Error(codes.Unavailable, "Gateway owner is not configured")
	}
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return out, status.Error(codes.Unauthenticated, "Gateway bearer token is required")
	}
	if err := g.request(ctx, "/api/v1/auth/me", trimmed, nil, &out); err != nil {
		return out, err
	}
	email := strings.ToLower(strings.TrimSpace(out.Email))
	if out.ID <= 0 || email == "" || out.Status != "active" {
		return out, status.Error(codes.Unauthenticated, "Gateway identity is no longer active")
	}
	out.Email = email
	return out, nil
}

// Balance reads the spendable balance from the real Sub2API admin user
// readback. Decimal conversion floors to whole USD micros, matching the retained
// Control Plane adapter's money semantics.
func (g *Gateway) Balance(ctx context.Context, subject string) (GatewayBalance, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(subject), 10, 64)
	if err != nil || id <= 0 {
		return GatewayBalance{}, status.Error(codes.InvalidArgument, "a positive Gateway subject id is required")
	}
	if !g.DirectoryConfigured() {
		return GatewayBalance{}, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return GatewayBalance{}, err
	}
	var out struct {
		ID      int64       `json:"id"`
		Balance json.Number `json:"balance"`
		Status  string      `json:"status"`
	}
	if err := g.request(ctx, "/api/v1/admin/users/"+strconv.FormatInt(id, 10), token, nil, &out); err != nil {
		if status.Code(err) == codes.Unauthenticated {
			g.invalidateDirectoryToken()
		}
		return GatewayBalance{}, err
	}
	if out.ID != id || (out.Status != "active" && out.Status != "disabled") {
		return GatewayBalance{}, status.Error(codes.FailedPrecondition, "Gateway wallet readback identity mismatch")
	}
	micros, err := floorUSDDecimalToMicros(out.Balance)
	if err != nil {
		return GatewayBalance{}, status.Error(codes.Unavailable, "invalid Gateway wallet balance")
	}
	return GatewayBalance{UserID: id, USDMicros: micros, Status: out.Status}, nil
}

func floorUSDDecimalToMicros(value json.Number) (int64, error) {
	raw := strings.TrimSpace(value.String())
	if raw == "" {
		return 0, fmt.Errorf("empty balance")
	}
	r := new(big.Rat)
	if _, ok := r.SetString(raw); !ok || r.Sign() < 0 {
		return 0, fmt.Errorf("invalid balance")
	}
	micros := new(big.Int).Quo(new(big.Int).Mul(r.Num(), big.NewInt(1_000_000)), r.Denom())
	if !micros.IsInt64() {
		return 0, fmt.Errorf("balance overflow")
	}
	return micros.Int64(), nil
}

// ModelEntry is one Gateway model catalog entry read live from the provider. It
// carries no price: token pricing is not a Gateway fact this owner publishes.
type ModelEntry struct {
	ID           string
	Name         string
	Capabilities []string
	Available    bool
}

// UsageStats reads directory-scoped usage totals from the Gateway provider.
// The service authenticates with its own configured directory identity and
// rejects malformed or negative provider facts.
func (g *Gateway) UsageStats(ctx context.Context, subject, period string) (GatewayUsageStats, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(subject), 10, 64)
	if err != nil || id <= 0 {
		return GatewayUsageStats{}, status.Error(codes.InvalidArgument, "a positive Gateway subject id is required")
	}
	startDate, endDate, ok := gatewayUsageDateRange(period, time.Now())
	if !ok {
		return GatewayUsageStats{}, status.Error(codes.InvalidArgument, "a valid usage period is required")
	}
	if !g.DirectoryConfigured() {
		return GatewayUsageStats{}, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return GatewayUsageStats{}, err
	}
	values := url.Values{
		"end_date":   {endDate},
		"start_date": {startDate},
		"timezone":   {"Asia/Shanghai"},
		"user_id":    {strconv.FormatInt(id, 10)},
	}
	var data gatewayUsageStatsPayload
	if err := g.request(ctx, "/api/v1/admin/usage/stats?"+values.Encode(), token, nil, &data); err != nil {
		if status.Code(err) == codes.Unauthenticated {
			g.invalidateDirectoryToken()
		}
		return GatewayUsageStats{}, err
	}
	return data.stats()
}

type gatewayUsageStatsPayload struct {
	TotalRequests     *int64       `json:"total_requests"`
	TotalInputTokens  *int64       `json:"total_input_tokens"`
	TotalOutputTokens *int64       `json:"total_output_tokens"`
	TotalTokens       *int64       `json:"total_tokens"`
	TotalActualCost   *json.Number `json:"total_actual_cost"`
}

func (data gatewayUsageStatsPayload) stats() (GatewayUsageStats, error) {
	if data.TotalRequests == nil ||
		data.TotalInputTokens == nil ||
		data.TotalOutputTokens == nil ||
		data.TotalTokens == nil ||
		data.TotalActualCost == nil ||
		*data.TotalRequests < 0 ||
		*data.TotalInputTokens < 0 ||
		*data.TotalOutputTokens < 0 ||
		*data.TotalTokens < 0 {
		return GatewayUsageStats{}, status.Error(codes.Unavailable, "invalid Gateway usage stats")
	}
	cost, err := floorUSDDecimalToMicros(*data.TotalActualCost)
	if err != nil {
		return GatewayUsageStats{}, status.Error(codes.Unavailable, "invalid Gateway usage stats cost")
	}
	return GatewayUsageStats{
		TotalRequests: *data.TotalRequests, TotalInputTokens: *data.TotalInputTokens,
		TotalOutputTokens: *data.TotalOutputTokens, TotalTokens: *data.TotalTokens,
		TotalActualCostUSDMicros: cost,
	}, nil
}

func gatewayUsageDateRange(period string, now time.Time) (string, string, bool) {
	location := time.FixedZone("Asia/Shanghai", 8*60*60)
	today := now.In(location)
	start := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, location)
	switch strings.TrimSpace(period) {
	case "today":
	case "week":
		start = start.AddDate(0, 0, -(int(today.Weekday())+6)%7)
	case "month":
		start = time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, location)
	default:
		return "", "", false
	}
	return start.Format("2006-01-02"), today.Format("2006-01-02"), true
}

// Models reads the Codex model catalog from the Gateway provider using the
// caller's own active API key. A caller without an active key, or an
// unresolvable or malformed catalog, fails closed; no model or price is
// synthesized.
func (g *Gateway) Models(ctx context.Context, subject string) ([]ModelEntry, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(subject), 10, 64)
	if err != nil || id <= 0 {
		return nil, status.Error(codes.InvalidArgument, "a positive Gateway subject id is required")
	}
	if !g.DirectoryConfigured() {
		return nil, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	admin, err := g.directoryToken(ctx)
	if err != nil {
		return nil, err
	}
	var keys struct {
		Items []struct {
			Key    string `json:"key"`
			Status string `json:"status"`
		} `json:"items"`
	}
	if err := g.request(ctx, "/api/v1/admin/users/"+strconv.FormatInt(id, 10)+"/api-keys", admin, nil, &keys); err != nil {
		if status.Code(err) == codes.Unauthenticated {
			g.invalidateDirectoryToken()
		}
		return nil, err
	}
	key := ""
	for _, candidate := range keys.Items {
		if candidate.Status == "active" && strings.TrimSpace(candidate.Key) != "" {
			key = candidate.Key
			break
		}
	}
	if key == "" {
		return nil, status.Error(codes.FailedPrecondition, "the caller has no active Gateway API key")
	}
	var catalog struct {
		Data []struct {
			ID           string   `json:"id"`
			Name         string   `json:"name"`
			Hidden       bool     `json:"hidden"`
			Capabilities []string `json:"capabilities"`
		} `json:"data"`
	}
	if err := g.modelRequest(ctx, "/v1/models", key, &catalog); err != nil {
		return nil, err
	}
	out := make([]ModelEntry, 0, len(catalog.Data))
	for _, item := range catalog.Data {
		modelID := strings.TrimSpace(item.ID)
		if modelID == "" {
			return nil, status.Error(codes.Unavailable, "Gateway model catalog entry has no id")
		}
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = modelID
		}
		out = append(out, ModelEntry{ID: modelID, Name: name, Capabilities: item.Capabilities, Available: !item.Hidden})
	}
	return out, nil
}

// keyModelScope reads the model scope one issued Key actually has, by reading
// the catalog with that Key. The Gateway answers the catalog with the scope the
// Key's Group permits, so the result is the Key's own permission rather than a
// declared intention. A hidden model is not usable by the Key and is therefore
// not part of the scope.
//
// This is the only read that can confirm a Key's permission: the key creation
// response carries no model ids, and the user-facing Group route does not carry
// the Group's allowlist either.
func (g *Gateway) keyModelScope(ctx context.Context, raw string) ([]string, error) {
	key := strings.TrimSpace(raw)
	if key == "" {
		return nil, status.Error(codes.InvalidArgument, "an issued Gateway key is required")
	}
	var catalog struct {
		Data []struct {
			ID     string `json:"id"`
			Hidden bool   `json:"hidden"`
		} `json:"data"`
	}
	if err := g.modelRequest(ctx, "/v1/models", key, &catalog); err != nil || catalog.Data == nil {
		return nil, status.Error(codes.Unavailable, "issued Gateway key model scope could not be read")
	}
	out := make([]string, 0, len(catalog.Data))
	seen := map[string]bool{}
	for _, item := range catalog.Data {
		modelID := strings.TrimSpace(item.ID)
		if modelID == "" {
			return nil, status.Error(codes.Unavailable, "Gateway model catalog entry has no id")
		}
		if item.Hidden || seen[modelID] {
			continue
		}
		seen[modelID] = true
		out = append(out, modelID)
	}
	return out, nil
}

// modelRequest performs the OpenAI-compatible model catalog read, which is not
// wrapped in the administrator {code,data} envelope the other Gateway endpoints
// use.
func (g *Gateway) modelRequest(ctx context.Context, path, token string, out any) error {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, g.base+path, nil)
	if err != nil {
		return err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	response, err := g.client.Do(r)
	if err != nil {
		return status.Error(codes.Unavailable, "Gateway model catalog unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return status.Error(codes.Unauthenticated, "Gateway API key rejected")
	}
	if response.StatusCode != 200 {
		return status.Error(codes.Unavailable, "Gateway model catalog unavailable")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil || json.Unmarshal(data, out) != nil {
		return status.Error(codes.Unavailable, "invalid Gateway model catalog response")
	}
	return nil
}

// delegatedExpiry admits only lifetimes proven by the authenticated login
// response. When both expires_in and JWT exp exist, the earlier bound wins.
// An opaque access token without either proof cannot support delegated reads.
func delegatedExpiry(token string, raw json.RawMessage, started time.Time) time.Time {
	var until time.Time
	if len(raw) > 0 && string(raw) != "null" {
		var seconds int64
		if json.Unmarshal(raw, &seconds) != nil || seconds <= 0 || seconds > math.MaxInt64/int64(time.Second) {
			return time.Time{}
		}
		until = started.Add(time.Duration(seconds) * time.Second)
	}
	parts := strings.Split(token, ".")
	if len(parts) == 3 {
		payload, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err != nil {
			return time.Time{}
		}
		var claims map[string]json.RawMessage
		if json.Unmarshal(payload, &claims) != nil {
			return time.Time{}
		}
		if rawExp, ok := claims["exp"]; ok {
			var exp int64
			if json.Unmarshal(rawExp, &exp) != nil || exp <= 0 {
				return time.Time{}
			}
			absolute := time.Unix(exp, 0)
			if until.IsZero() || absolute.Before(until) {
				until = absolute
			}
		}
	}
	return until
}

// DirectoryUsers reads one paginated page of Sub2API users via the configured
// administrative directory credential. No local user cache is maintained.
func (g *Gateway) DirectoryUsers(ctx context.Context, page, pageSize int, search string) (GatewayDirectoryUserPage, error) {
	trimmedSearch := strings.TrimSpace(search)
	if page < 1 || pageSize < 1 || pageSize > 50 || utf8.RuneCountInString(trimmedSearch) > 100 {
		return GatewayDirectoryUserPage{}, status.Error(codes.InvalidArgument, "valid directory pagination and search bounds are required")
	}
	if !g.DirectoryConfigured() {
		return GatewayDirectoryUserPage{}, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return GatewayDirectoryUserPage{}, err
	}
	values := url.Values{
		"page":       {strconv.Itoa(page)},
		"page_size":  {strconv.Itoa(pageSize)},
		"sort_by":    {"id"},
		"sort_order": {"asc"},
	}
	if trimmedSearch != "" {
		values.Set("search", trimmedSearch)
	}
	var data struct {
		Items []struct {
			ID        int64           `json:"id"`
			Email     string          `json:"email"`
			Balance   json.RawMessage `json:"balance"`
			Status    string          `json:"status"`
			CreatedAt time.Time       `json:"created_at"`
			UpdatedAt time.Time       `json:"updated_at"`
		} `json:"items"`
		Total    int64 `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"page_size"`
		Pages    int   `json:"pages"`
	}
	if err := g.request(ctx, "/api/v1/admin/users?"+values.Encode(), token, nil, &data); err != nil {
		if status.Code(err) == codes.Unauthenticated {
			g.invalidateDirectoryToken()
		}
		return GatewayDirectoryUserPage{}, err
	}
	if data.Total < 0 || data.Page != page || data.PageSize != pageSize || len(data.Items) > pageSize {
		return GatewayDirectoryUserPage{}, status.Error(codes.Unavailable, "invalid Gateway user pagination")
	}
	expectedPages := int((data.Total + int64(pageSize) - 1) / int64(pageSize))
	if expectedPages < 1 {
		expectedPages = 1
	}
	if data.Total == 0 {
		if (data.Pages != 0 && data.Pages != 1) || len(data.Items) != 0 {
			return GatewayDirectoryUserPage{}, status.Error(codes.Unavailable, "invalid Gateway empty user pagination")
		}
		return GatewayDirectoryUserPage{Total: 0, Page: page, PageSize: pageSize, Pages: 1}, nil
	}
	if data.Pages != expectedPages || (page <= expectedPages && len(data.Items) == 0 && data.Total > 0) || (page > expectedPages && len(data.Items) != 0) {
		return GatewayDirectoryUserPage{}, status.Error(codes.Unavailable, "invalid Gateway user pagination metadata")
	}
	out := GatewayDirectoryUserPage{
		Items:    make([]GatewayDirectoryUser, 0, len(data.Items)),
		Total:    data.Total,
		Page:     data.Page,
		PageSize: data.PageSize,
		Pages:    data.Pages,
	}
	seen := make(map[int64]struct{}, len(data.Items))
	for _, item := range data.Items {
		email := strings.ToLower(strings.TrimSpace(item.Email))
		_, dup := seen[item.ID]
		if item.ID <= 0 || email == "" || (item.Status != "active" && item.Status != "disabled") || dup ||
			(!item.CreatedAt.IsZero() && !item.UpdatedAt.IsZero() && item.UpdatedAt.Before(item.CreatedAt)) {
			return GatewayDirectoryUserPage{}, status.Error(codes.Unavailable, "invalid Gateway user identity facts")
		}
		seen[item.ID] = struct{}{}
		micros, balErr := floorJSONUSDMicros(item.Balance)
		out.Items = append(out.Items, GatewayDirectoryUser{
			ID:                 item.ID,
			Email:              email,
			Status:             item.Status,
			BalanceUSDMicros:   micros,
			BalanceUnavailable: balErr != nil,
			CreatedAt:          item.CreatedAt.UTC(),
			UpdatedAt:          item.UpdatedAt.UTC(),
		})
	}
	return out, nil
}

// DirectoryBatchUsersUsage reads per-user aggregate and per-platform API spend
// for a bounded page of Gateway user IDs.
func (g *Gateway) DirectoryBatchUsersUsage(ctx context.Context, userIDs []int64) (map[int64]GatewayBatchUserUsage, error) {
	ids, err := normalizeDirectoryUserIDs(userIDs)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return map[int64]GatewayBatchUserUsage{}, nil
	}
	if !g.DirectoryConfigured() {
		return nil, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return nil, err
	}
	var data struct {
		Stats map[string]json.RawMessage `json:"stats"`
	}
	if err := g.request(ctx, "/api/v1/admin/dashboard/users-usage", token, map[string]any{"user_ids": ids}, &data); err != nil {
		if status.Code(err) == codes.Unauthenticated {
			g.invalidateDirectoryToken()
		}
		return nil, err
	}
	if data.Stats == nil {
		return nil, status.Error(codes.Unavailable, "invalid Gateway batch usage response")
	}
	requested := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		requested[id] = struct{}{}
	}
	for rawID := range data.Stats {
		parsed, parseErr := strconv.ParseInt(rawID, 10, 64)
		if parseErr != nil || rawID != strconv.FormatInt(parsed, 10) {
			return nil, status.Error(codes.Unavailable, "invalid Gateway batch usage key")
		}
		if _, ok := requested[parsed]; !ok {
			return nil, status.Error(codes.Unavailable, "unexpected Gateway batch usage subject")
		}
	}
	out := make(map[int64]GatewayBatchUserUsage, len(ids))
	for _, id := range ids {
		rawItem, ok := data.Stats[strconv.FormatInt(id, 10)]
		if !ok {
			continue
		}
		var item struct {
			UserID          int64       `json:"user_id"`
			TodayActualCost json.Number `json:"today_actual_cost"`
			TotalActualCost json.Number `json:"total_actual_cost"`
			ByPlatform      []struct {
				Platform        string      `json:"platform"`
				TodayActualCost json.Number `json:"today_actual_cost"`
				TotalActualCost json.Number `json:"total_actual_cost"`
			} `json:"by_platform"`
		}
		if json.Unmarshal(rawItem, &item) != nil || item.UserID != id {
			continue
		}
		todayMicros, todayErr := floorUSDDecimalToMicros(item.TodayActualCost)
		totalMicros, totalErr := floorUSDDecimalToMicros(item.TotalActualCost)
		if todayErr != nil || totalErr != nil {
			continue
		}
		entry := GatewayBatchUserUsage{
			UserID:                   id,
			TodayActualCostUSDMicros: todayMicros,
			TotalActualCostUSDMicros: totalMicros,
			ByPlatform:               make([]GatewayPlatformUsage, 0, len(item.ByPlatform)),
		}
		platforms := make(map[string]struct{}, len(item.ByPlatform))
		valid := true
		for _, p := range item.ByPlatform {
			name := strings.TrimSpace(p.Platform)
			pToday, e1 := floorUSDDecimalToMicros(p.TodayActualCost)
			pTotal, e2 := floorUSDDecimalToMicros(p.TotalActualCost)
			_, dup := platforms[name]
			if name == "" || e1 != nil || e2 != nil || dup {
				valid = false
				break
			}
			platforms[name] = struct{}{}
			entry.ByPlatform = append(entry.ByPlatform, GatewayPlatformUsage{
				Platform:                 name,
				TodayActualCostUSDMicros: pToday,
				TotalActualCostUSDMicros: pTotal,
			})
		}
		if valid {
			out[id] = entry
		}
	}
	return out, nil
}

// DirectoryUserKeyCount returns the total API key count held by one Gateway subject.
func (g *Gateway) DirectoryUserKeyCount(ctx context.Context, userID int64) (int32, error) {
	if userID <= 0 {
		return 0, status.Error(codes.InvalidArgument, "a positive Gateway subject id is required")
	}
	if !g.DirectoryConfigured() {
		return 0, status.Error(codes.FailedPrecondition, "the Gateway directory identity is not configured")
	}
	token, err := g.directoryToken(ctx)
	if err != nil {
		return 0, err
	}
	var data struct {
		Total int `json:"total"`
	}
	if err := g.request(ctx, "/api/v1/admin/users/"+strconv.FormatInt(userID, 10)+"/api-keys?page=1&page_size=1", token, nil, &data); err != nil {
		if status.Code(err) == codes.Unauthenticated {
			g.invalidateDirectoryToken()
		}
		return 0, err
	}
	if data.Total < 0 || data.Total > math.MaxInt32 {
		return 0, status.Error(codes.Unavailable, "invalid Gateway user key count")
	}
	return int32(data.Total), nil
}

func floorJSONUSDMicros(raw json.RawMessage) (int64, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return 0, fmt.Errorf("missing balance")
	}
	if strings.HasPrefix(trimmed, `"`) {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return 0, err
		}
		return floorUSDDecimalToMicros(json.Number(s))
	}
	var num json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&num); err != nil {
		return 0, err
	}
	return floorUSDDecimalToMicros(num)
}

func normalizeDirectoryUserIDs(input []int64) ([]int64, error) {
	if len(input) > 100 {
		return nil, status.Error(codes.InvalidArgument, "too many Gateway subjects requested")
	}
	seen := make(map[int64]struct{}, len(input))
	ids := make([]int64, 0, len(input))
	for _, id := range input {
		if id <= 0 {
			return nil, status.Error(codes.InvalidArgument, "Gateway user IDs must be positive")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}
