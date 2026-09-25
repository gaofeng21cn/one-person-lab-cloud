package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"time"

	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

// LoginReader carries only typed CloudIdentity responses and private cookie
// metadata. Neither the BFF nor the browser chooses the authenticated actor.
type LoginReader interface {
	LoginContext(context.Context) (*api.LoginContext, string, error)
	Login(context.Context, string, *api.LoginRequest) (*api.Session, string, error)
	Logout(context.Context, string) error
}

func setIdentityCookie(w http.ResponseWriter, name, value string, age int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/api/v2/auth", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func sameOrigin(r *http.Request) bool {
	u, e := url.Parse(r.Header.Get("Origin"))
	scheme := "https"
	if r.TLS == nil {
		scheme = "http"
	}
	if configured := os.Getenv("OPL_PUBLIC_URL"); configured != "" {
		expected, err := url.Parse(configured)
		if err != nil || expected.Host == "" || (expected.Scheme != "https" && expected.Scheme != "http") {
			return false
		}
		return e == nil && u.User == nil && u.Host == expected.Host && u.Scheme == expected.Scheme && u.RawQuery == "" && u.Fragment == "" && (u.Path == "" || u.Path == "/") && r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	return e == nil && u.User == nil && u.Host == r.Host && u.Scheme == scheme && (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == "" && r.Header.Get("Sec-Fetch-Site") != "cross-site"
}
func (s *Server) registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v2/auth/context", func(w http.ResponseWriter, r *http.Request) {
		publisherRequestID(r)
		w.Header().Set("Cache-Control", "no-store")
		identity, ok := s.identity.(LoginReader)
		if !ok {
			writePublisherError(w, r, 503, "owner_unconfigured", "CloudIdentity login unavailable")
			return
		}
		out, cookie, e := identity.LoginContext(r.Context())
		if e != nil || cookie == "" || out == nil {
			writePublisherError(w, r, 503, "owner_unconfigured", "CloudIdentity login unavailable")
			return
		}
		setIdentityCookie(w, "opl_login_context", cookie, 300)
		// Double-submit CSRF value is bound to the HttpOnly challenge by the signed
		// authority value; an attacker cannot create a matching authority challenge.
		h := sha256.Sum256([]byte(out.CsrfToken))
		setIdentityCookie(w, "opl_login_csrf", hex.EncodeToString(h[:]), 300)
		authJSON(w, out)
	})
	mux.HandleFunc("POST /api/v2/auth/login", func(w http.ResponseWriter, r *http.Request) {
		publisherRequestID(r)
		w.Header().Set("Cache-Control", "no-store")
		identity, ok := s.identity.(LoginReader)
		if !ok {
			writePublisherError(w, r, 503, "owner_unconfigured", "CloudIdentity login unavailable")
			return
		}
		challenge, e := r.Cookie("opl_login_context")
		csrf, e2 := r.Cookie("opl_login_csrf")
		hash := sha256.Sum256([]byte(r.Header.Get("X-CSRF-Token")))
		if e != nil || e2 != nil || subtle.ConstantTimeCompare([]byte(csrf.Value), []byte(hex.EncodeToString(hash[:]))) != 1 || !sameOrigin(r) {
			writePublisherError(w, r, 403, "csrf_required", "same-origin login context required")
			return
		}
		content, _, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
		raw, readErr := io.ReadAll(http.MaxBytesReader(w, r.Body, 16<<10))
		input := &api.LoginRequest{}
		if e != nil || content != "application/json" || readErr != nil || publicjson.Unmarshal(raw, input) != nil || r.Header.Get("Idempotency-Key") == "" {
			writePublisherError(w, r, 400, "invalid_request", "valid login request required")
			return
		}
		out, cookie, e := identity.Login(r.Context(), challenge.Value, input)
		input.Password = ""
		if e != nil {
			writePublisherIdentityError(w, r, e)
			return
		}
		if cookie == "" || out == nil {
			writePublisherError(w, r, 503, "owner_unconfigured", "CloudIdentity login unavailable")
			return
		}
		http.SetCookie(w, &http.Cookie{Name: SessionCookieName, Value: cookie, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: int(time.Until(out.ExpiresAt.AsTime()).Seconds())})
		setIdentityCookie(w, "opl_login_context", "", -1)
		setIdentityCookie(w, "opl_login_csrf", "", -1)
		authJSON(w, out)
	})
	mux.HandleFunc("POST /api/v2/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		publisherRequestID(r)
		w.Header().Set("Cache-Control", "no-store")
		identity, ok := s.identity.(LoginReader)
		if !ok {
			writePublisherError(w, r, 503, "owner_unconfigured", "CloudIdentity login unavailable")
			return
		}
		caller, e := RequireSession(r.Context(), s.identity, r)
		if e != nil {
			writePublisherIdentityError(w, r, e)
			return
		}
		if !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(caller.Session.CsrfToken), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
			writePublisherError(w, r, 403, "csrf_required", "same-origin session CSRF required")
			return
		}
		if e = identity.Logout(r.Context(), caller.SessionID); e != nil {
			writePublisherIdentityError(w, r, e)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: SessionCookieName, Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
		w.WriteHeader(204)
	})
}
func authJSON(w http.ResponseWriter, m proto.Message) {
	raw, e := publicjson.Marshal(m)
	if e != nil {
		http.Error(w, "invalid identity response", 502)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(raw)
}
