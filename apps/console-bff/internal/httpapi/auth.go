package httpapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"io"
	"log"
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
		authJSON(w, r, out)
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
		projected, e := identityProjection(r, out)
		if e != nil {
			writeIdentityProjectionError(w, r)
			return
		}
		// The session cookie is written only after the response body exists, so a
		// login the browser cannot be told about leaves no session behind.
		http.SetCookie(w, &http.Cookie{Name: SessionCookieName, Value: cookie, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: int(time.Until(out.ExpiresAt.AsTime()).Seconds())})
		setIdentityCookie(w, "opl_login_context", "", -1)
		setIdentityCookie(w, "opl_login_csrf", "", -1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(projected)
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

// identityProjection projects a CloudIdentity message onto the public contract
// and records what the contract could not name. A dropped value is an observed
// contract gap, never a grant: it is absent from the body and CloudIdentity still
// decides every action. The actor id is the only identity fact logged; the
// password and the session token are never logged.
func identityProjection(r *http.Request, m proto.Message) ([]byte, error) {
	raw, omissions, e := publicjson.MarshalWithOmissions(m)
	actor := ""
	if session, ok := m.(*api.Session); ok {
		actor = session.GetActorId()
	}
	for _, omission := range omissions {
		log.Printf("identity projection omitted unnamed enum value message=%s field=%s value=%d actor=%q request=%s", omission.Message, omission.Field, omission.Value, actor, r.Header.Get(requestIDHeader))
	}
	if e != nil {
		log.Printf("identity projection failed message=%s actor=%q request=%s error=%v", m.ProtoReflect().Descriptor().FullName(), actor, r.Header.Get(requestIDHeader), e)
		return nil, e
	}
	return raw, nil
}

// writeIdentityProjectionError answers a projection failure with the contract's
// typed error body. The failing field and offending value stay in the pod log.
func writeIdentityProjectionError(w http.ResponseWriter, r *http.Request) {
	writePublisherError(w, r, 502, "invalid_identity_projection", "CloudIdentity identity response could not be projected")
}

func authJSON(w http.ResponseWriter, r *http.Request, m proto.Message) {
	raw, e := identityProjection(r, m)
	if e != nil {
		writeIdentityProjectionError(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}
