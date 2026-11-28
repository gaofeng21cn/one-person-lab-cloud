package httpapi

import (
	"encoding/json"
	"io"
	"mime"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
)

const (
	serviceGrantMaxBodyBytes = 8192
	serviceGrantWindow       = time.Minute
	serviceGrantMaxPerIP     = 30
	serviceGrantMaxPerEmail  = 12
)

var serviceGrantNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

type serviceGrantInput struct {
	Email    *string `json:"email"`
	Password *string `json:"password"`
	TotpCode *string `json:"totp_code"`
	Service  *string `json:"service"`
	Scope    *string `json:"scope"`
}

type fixedWindowLimiter struct {
	mu      sync.Mutex
	buckets map[string]rateBucket
}

type rateBucket struct {
	count int
	reset time.Time
}

func newFixedWindowLimiter() *fixedWindowLimiter {
	return &fixedWindowLimiter{buckets: make(map[string]rateBucket)}
}

func (l *fixedWindowLimiter) allow(key string, limit int, now time.Time) (bool, time.Duration) {
	if key == "" || limit <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) > 4096 {
		for k, b := range l.buckets {
			if !now.Before(b.reset) {
				delete(l.buckets, k)
			}
		}
	}
	bucket, ok := l.buckets[key]
	if !ok || !now.Before(bucket.reset) {
		l.buckets[key] = rateBucket{count: 1, reset: now.Add(serviceGrantWindow)}
		return true, 0
	}
	if bucket.count >= limit {
		return false, time.Until(bucket.reset)
	}
	bucket.count++
	l.buckets[key] = bucket
	return true, 0
}

func clientRateLimitIP(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if parsed := net.ParseIP(strings.TrimSpace(first)); parsed != nil {
			return parsed.String()
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (s *Server) registerClientServiceGrantRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v2/client/service-grant", s.clientServiceGrantHandler)
	mux.HandleFunc("GET /.well-known/opl-grant-jwks.json", s.clientServiceGrantJWKSHandler)
}

func (s *Server) serviceGrantLimiterInstance() *fixedWindowLimiter {
	s.grantLimiterOnce.Do(func() {
		if s.grantLimiter == nil {
			s.grantLimiter = newFixedWindowLimiter()
		}
	})
	return s.grantLimiter
}

// clientServiceGrantJWKSHandler exposes active Ed25519 verification keys so
// downstream companion services verify grant JWTs offline without Cloud DB access.
func (s *Server) clientServiceGrantJWKSHandler(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "query parameters are not accepted")
		return
	}
	if s.tenant == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "CloudIdentity owner is not configured")
		return
	}
	resp, err := s.tenant.GetServiceGrantJWKS(r.Context(), &api.GetServiceGrantJWKSRpcRequest{})
	if err != nil {
		writeServiceGrantOwnerError(w, err)
		return
	}
	keys := make([]map[string]string, 0, len(resp.GetKeys()))
	for _, key := range resp.GetKeys() {
		keys = append(keys, map[string]string{
			"kty": key.GetKty(),
			"crv": key.GetCrv(),
			"alg": key.GetAlg(),
			"use": key.GetUse(),
			"kid": key.GetKid(),
			"x":   key.GetX(),
		})
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{"keys": keys})
}

// clientServiceGrantHandler issues a short-lived Ed25519 service grant and
// dispatches the authoritative service origin for non-browser devices and
// companion clients. Browser session/CSRF cookies are intentionally not used.
func (s *Server) clientServiceGrantHandler(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.RawQuery != "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "query parameters are not accepted")
		return
	}
	if _, err := r.Cookie(SessionCookieName); err == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized", "client bootstrap does not accept browser session cookies")
		return
	}
	contentType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		writeError(w, http.StatusBadRequest, "invalid_request", "application/json content type required")
		return
	}

	raw, readErr := io.ReadAll(io.LimitReader(r.Body, serviceGrantMaxBodyBytes+1))
	if readErr != nil || len(raw) == 0 || len(raw) > serviceGrantMaxBodyBytes {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	var input serviceGrantInput
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON payload")
		return
	}

	serviceID := ""
	if input.Service != nil {
		serviceID = strings.TrimSpace(*input.Service)
	}
	if !serviceGrantNamePattern.MatchString(serviceID) {
		writeError(w, http.StatusBadRequest, "invalid_request", "valid service id is required")
		return
	}
	scope := ""
	if input.Scope != nil {
		scope = strings.TrimSpace(*input.Scope)
		if scope != "device:enroll" && scope != "console:login" {
			writeError(w, http.StatusBadRequest, "invalid_request", "unsupported scope")
			return
		}
	}

	authHeader := r.Header.Values("Authorization")
	var bearerToken string
	if len(authHeader) > 1 {
		writeError(w, http.StatusUnauthorized, "unauthorized", "duplicate Authorization header")
		return
	}
	if len(authHeader) == 1 {
		token, ok := strings.CutPrefix(authHeader[0], "Bearer ")
		if !ok || strings.TrimSpace(token) == "" || strings.ContainsAny(token, " \t\r\n") {
			writeError(w, http.StatusUnauthorized, "unauthorized", "valid Bearer token required")
			return
		}
		bearerToken = strings.TrimSpace(token)
	}

	var email, password, totpCode string
	if input.Email != nil {
		email = strings.ToLower(strings.TrimSpace(*input.Email))
	}
	if input.Password != nil {
		password = *input.Password
		*input.Password = ""
	}
	if input.TotpCode != nil {
		totpCode = strings.TrimSpace(*input.TotpCode)
	}
	hasPasswordCreds := email != "" || password != "" || totpCode != ""
	if bearerToken != "" && hasPasswordCreds {
		writeError(w, http.StatusBadRequest, "invalid_request", "choose either Bearer authorization or email/password credentials")
		return
	}
	if bearerToken == "" && (email == "" || len(email) > 254 || password == "" || len(password) > 512) {
		writeError(w, http.StatusBadRequest, "invalid_request", "email and password (or Bearer token) are required")
		return
	}

	now := time.Now()
	limiter := s.serviceGrantLimiterInstance()
	if allowed, retryAfter := limiter.allow("ip:"+clientRateLimitIP(r), serviceGrantMaxPerIP, now); !allowed {
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Seconds()+0.999))))
		writeError(w, http.StatusTooManyRequests, "rate_limited", "too many service grant attempts")
		return
	}
	if email != "" {
		if allowed, retryAfter := limiter.allow("email:"+email, serviceGrantMaxPerEmail, now); !allowed {
			w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Seconds()+0.999))))
			writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts for account")
			return
		}
	}

	if s.tenant == nil {
		password = ""
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "CloudIdentity owner is not configured")
		return
	}

	rpcReq := &api.IssueServiceGrantRpcRequest{
		BearerToken: bearerToken,
		Body: &api.ServiceGrantRequest{
			Email:    email,
			Password: password,
			Service:  serviceID,
		},
	}
	password = ""
	if totpCode != "" {
		rpcReq.Body.TotpCode = &totpCode
	}
	if scope != "" {
		rpcReq.Body.Scope = &scope
	}
	resp, err := s.tenant.IssueServiceGrant(r.Context(), rpcReq)
	rpcReq.Body.Password = ""
	if err != nil {
		writeServiceGrantOwnerError(w, err)
		return
	}
	if resp.GetRequires_2Fa() {
		writeJSON(w, http.StatusOK, map[string]any{"requires_2fa": true})
		return
	}
	out := map[string]any{
		"actor_id":    resp.GetActorId(),
		"email":       resp.GetEmail(),
		"service":     resp.GetService(),
		"enabled":     resp.GetEnabled(),
		"origin":      resp.GetOrigin(),
		"grant_token": resp.GetGrantToken(),
		"expires_at":  resp.GetExpiresAt().AsTime().UTC().Format(time.RFC3339),
		"role":        resp.GetRole(),
		"limits":      map[string]any{},
	}
	if resp.GetLimits() != nil {
		out["limits"] = resp.GetLimits().AsMap()
	}
	writeJSON(w, http.StatusOK, out)
}

func writeServiceGrantOwnerError(w http.ResponseWriter, err error) {
	st, ok := status.FromError(err)
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "CloudIdentity owner unavailable")
		return
	}
	switch st.Code() {
	case codes.PermissionDenied:
		code := "service_not_entitled"
		if st.Message() != "service_not_entitled" && st.Message() != "" {
			code = st.Message()
		}
		if code == "service_not_entitled" {
			writeError(w, http.StatusForbidden, "service_not_entitled", "account is not entitled to use this service")
			return
		}
		writeError(w, http.StatusForbidden, "forbidden", st.Message())
	case codes.Unauthenticated:
		writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or inactive OPL Gateway credentials")
	case codes.InvalidArgument:
		writeError(w, http.StatusBadRequest, "invalid_request", st.Message())
	case codes.NotFound:
		writeError(w, http.StatusNotFound, "unknown_service", st.Message())
	case codes.ResourceExhausted:
		writeError(w, http.StatusTooManyRequests, "rate_limited", st.Message())
	default:
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", st.Message())
	}
}
