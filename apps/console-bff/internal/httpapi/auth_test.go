package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// fakeLoginIdentity is the typed CloudIdentity double for the auth routes. It
// implements the login reader and the session/authorization reader so one value
// can serve both the login route and the authorization path that consumes a
// session.
type fakeLoginIdentity struct {
	session  *api.Session
	cookie   string
	decision *api.AuthorizationDecision
	err      error
	requests []*api.AuthorizationRequest
}

func (f *fakeLoginIdentity) LoginContext(context.Context) (*api.LoginContext, string, error) {
	return &api.LoginContext{CsrfToken: "challenge-csrf", ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}, "context-cookie", f.err
}

func (f *fakeLoginIdentity) Login(context.Context, string, *api.LoginRequest) (*api.Session, string, error) {
	return f.session, f.cookie, f.err
}

func (f *fakeLoginIdentity) Logout(context.Context, string) error {
	return f.err
}

func (f *fakeLoginIdentity) Session(context.Context, string) (*api.Session, error) {
	return f.session, f.err
}

func (f *fakeLoginIdentity) Authorize(_ context.Context, request *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	f.requests = append(f.requests, request)
	return f.decision, f.err
}

// loginRequest builds the same-origin login request the browser sends: a login
// context challenge, the double-submit CSRF value, JSON credentials and an
// idempotency key.
func loginRequest(csrf string) *http.Request {
	request := httptest.NewRequest(http.MethodPost, "/api/v2/auth/login", strings.NewReader(`{"username":"owner-988","password":"correct horse battery staple"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://"+request.Host)
	request.Header.Set("X-CSRF-Token", csrf)
	request.Header.Set("Idempotency-Key", "login-988")
	hash := sha256.Sum256([]byte(csrf))
	request.AddCookie(&http.Cookie{Name: "opl_login_context", Value: "challenge-988"})
	request.AddCookie(&http.Cookie{Name: "opl_login_csrf", Value: hex.EncodeToString(hash[:])})
	return request
}

// sessionCookie returns the named Set-Cookie the response carries, or nil.
func sessionCookie(response *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&buffer)
	t.Cleanup(func() { log.SetOutput(previous) })
	return &buffer
}

func sessionWithUnnameablePermission() *api.Session {
	return &api.Session{
		ActorId:     "actor-988",
		DisplayName: "Actor 988",
		TenantId:    ptr("tenant-988"),
		Permissions: []api.AuthorizationActionEnum{
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWALLET,
			api.AuthorizationActionEnum(999),
		},
		CsrfToken: "csrf-988",
		ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)),
	}
}

// TestLoginProjectsOnlyNamedPermissions pins the lenient direction from #988: a
// permission the contract cannot name must not block login, must be absent from
// the body, and must be named in the pod log with the account so an operator can
// repair the stored value.
func TestLoginProjectsOnlyNamedPermissions(t *testing.T) {
	logs := captureLog(t)
	identity := &fakeLoginIdentity{session: sessionWithUnnameablePermission(), cookie: "session-988"}
	response := httptest.NewRecorder()
	NewServer(nil, identity).Handler().ServeHTTP(response, loginRequest("csrf-988"))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		ActorId     string   `json:"actorId"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("session body is not public JSON: %v", err)
	}
	if body.ActorId != "actor-988" {
		t.Fatalf("actorId = %q, want the CloudIdentity actor", body.ActorId)
	}
	if want := []string{"getWallet"}; !reflect.DeepEqual(body.Permissions, want) {
		t.Fatalf("permissions = %v, want %v: the unnamed value must not be projected", body.Permissions, want)
	}
	cookie := sessionCookie(response, SessionCookieName)
	if cookie == nil || cookie.Value != "session-988" || cookie.MaxAge <= 0 {
		t.Fatalf("%s cookie = %+v, want the CloudIdentity session with a future expiry", SessionCookieName, cookie)
	}
	for _, want := range []string{"permissions", "999", "actor-988"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log does not name %q: %s", want, logs.String())
		}
	}
	if strings.Contains(logs.String(), "correct horse battery staple") || strings.Contains(logs.String(), "session-988") {
		t.Fatalf("log leaked the password or session token: %s", logs.String())
	}
}

// TestProjectionFailureLeavesNoSessionCookie is the fail-closed half: when the
// identity body cannot be produced for any other reason, the login is refused
// with a typed error and no session cookie is left behind.
func TestProjectionFailureLeavesNoSessionCookie(t *testing.T) {
	logs := captureLog(t)
	session := sessionWithUnnameablePermission()
	session.DisplayName = string([]byte{0xff, 0xfe})
	identity := &fakeLoginIdentity{session: session, cookie: "session-that-must-not-survive"}
	response := httptest.NewRecorder()
	NewServer(nil, identity).Handler().ServeHTTP(response, loginRequest("csrf-988"))

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if cookie := sessionCookie(response, SessionCookieName); cookie != nil {
		t.Fatalf("refused login set %s=%q", SessionCookieName, cookie.Value)
	}
	var body struct {
		Code      string `json:"code"`
		RequestId string `json:"requestId"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("refused login body is not typed JSON: %v", err)
	}
	if body.Code == "" || body.RequestId == "" {
		t.Fatalf("refused login body = %s, want a typed code and request id", response.Body.String())
	}
	for _, want := range []string{"display_name", "actor-988"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("log does not name %q: %s", want, logs.String())
		}
	}
}

// TestDroppedPermissionConfersNoAuthorization shows the dropped value is not an
// authorization input. The BFF forwards only the session identity and scope to
// CloudIdentity, and an explicit denial stays a denial even though the session
// carries the unnameable value.
// A dropped permission must not become a grant, and a named permission must not
// become one either: CloudIdentity decides every action, and the session's
// permission list is never consulted for authorization. Asserting only the first
// half would also pass if the BFF authorized on any non-empty list.
func TestDroppedPermissionConfersNoAuthorization(t *testing.T) {
	cases := []struct {
		name        string
		permissions []api.AuthorizationActionEnum
		result      api.AuthorizationResult
		// forbidden is the whole claim: CloudIdentity's decision alone decides,
		// so an allowed decision must pass authorization whatever the session's
		// permission list holds, and a denied one must stop the request whatever
		// it holds. The terminal status past authorization is the owner reader's
		// business and is not asserted here.
		forbidden bool
	}{
		{
			name:        "dropped permission, denied decision",
			permissions: []api.AuthorizationActionEnum{999},
			result:      api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED,
			forbidden:   true,
		},
		{
			name:        "dropped permission, allowed decision",
			permissions: []api.AuthorizationActionEnum{999},
			result:      api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
			forbidden:   false,
		},
		{
			name:        "named permission, denied decision",
			permissions: []api.AuthorizationActionEnum{api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETTENANT},
			result:      api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED,
			forbidden:   true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			identity := &fakeLoginIdentity{
				session: &api.Session{
					ActorId:     "actor-988",
					TenantId:    ptr("tenant-988"),
					Permissions: tc.permissions,
					CsrfToken:   "csrf-988",
				},
				decision: &api.AuthorizationDecision{
					ActorId:           "actor-988",
					SessionId:         ptr(owneridentity.SessionReference("session-1")),
					Scope:             &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-988"}}},
					PermissionVersion: 1,
					IssuedAt:          timestamppb.New(time.Now().Add(-time.Minute)),
					ExpiresAt:         timestamppb.New(time.Now().Add(time.Minute)),
					Result:            tc.result,
					Issuer:            api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
					Action:            api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETTENANT,
					AudienceOwner:     api.OwnerEnum_OWNER_ENUM_TENANT,
					Resource:          &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: ptr("tenant-988")},
				},
			}
			response := httptest.NewRecorder()
			NewServer(resolvedReader(), identity).Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/tenant"))

			if got := response.Code == http.StatusForbidden; got != tc.forbidden {
				t.Fatalf("forbidden = %v, want %v (status = %d, body = %s)", got, tc.forbidden, response.Code, response.Body.String())
			}
			if len(identity.requests) != 1 {
				t.Fatalf("authorization requests = %d, want exactly one CloudIdentity decision", len(identity.requests))
			}
			request := identity.requests[0]
			if request.GetActorId() != "actor-988" || request.GetScope().GetTenant().GetTenantId() != "tenant-988" {
				t.Fatalf("authorization request = %+v, want the session identity and scope", request)
			}
		})
	}
}

// loginSession is the CloudIdentity session the double issues for the cookie
// authority tests below. Its CSRF token is the one the browser would receive, so
// the logout route's same-origin CSRF check can be exercised for real.
func loginSession() *api.Session {
	return &api.Session{
		ActorId:   "actor-987",
		TenantId:  ptr("tenant-987"),
		CsrfToken: "csrf-987",
		ExpiresAt: timestamppb.New(time.Now().Add(time.Hour)),
	}
}

// TestLoginSetsOnlyTheBFFSessionCookie is the core acceptance for #987: the BFF
// writes the CloudIdentity session under its own cookie name and never under
// Control Plane's opl_session. Two authorities that share one cookie name destroy
// each other's sessions, so the login must not emit the legacy name at all.
func TestLoginSetsOnlyTheBFFSessionCookie(t *testing.T) {
	identity := &fakeLoginIdentity{session: loginSession(), cookie: "session-987"}
	response := httptest.NewRecorder()
	NewServer(nil, identity).Handler().ServeHTTP(response, loginRequest("csrf-987"))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cookie := sessionCookie(response, "opl_console_session")
	if cookie == nil || cookie.Value != "session-987" {
		t.Fatalf("login console session cookie = %+v, want the CloudIdentity session under opl_console_session", cookie)
	}
	if cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.MaxAge <= 0 {
		t.Fatalf("console session cookie attributes changed: %+v", cookie)
	}
	if legacy := sessionCookie(response, "opl_session"); legacy != nil {
		t.Fatalf("login also wrote Control Plane's opl_session: %+v", legacy)
	}
}

// TestBFFDoesNotAuthenticateFromControlPlaneSessionCookie pins one authority per
// cookie name. The value is a real session this BFF's own login just issued, but
// presenting it under Control Plane's opl_session must not authenticate: reading
// both names is the forbidden compatibility path that keeps the collision alive.
func TestBFFDoesNotAuthenticateFromControlPlaneSessionCookie(t *testing.T) {
	identity := &fakeLoginIdentity{session: loginSession(), cookie: "session-987"}
	server := NewServer(nil, identity).Handler()
	login := httptest.NewRecorder()
	server.ServeHTTP(login, loginRequest("csrf-987"))
	if login.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", login.Code, login.Body.String())
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v2/auth/session", nil)
	request.AddCookie(&http.Cookie{Name: "opl_session", Value: "session-987"})
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status with a session under opl_session = %d, want 401; body = %s", response.Code, response.Body.String())
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("unauthenticated request wrote cookies: %v", cookies)
	}
}

// TestBFFRejectsUnknownSessionCookieWithoutClearing reproduces the production
// deletion on the BFF side: an unrecognised value in the BFF's own cookie is
// answered as unauthenticated and nothing deletes the browser's cookie. The BFF
// never issued that value, so it has no basis for invalidating it, and a failed
// read must not destroy a credential.
func TestBFFRejectsUnknownSessionCookieWithoutClearing(t *testing.T) {
	identity := &fakeLoginIdentity{err: status.Error(codes.Unauthenticated, "unknown session")}
	server := NewServer(nil, identity).Handler()
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v2/auth/session"},
		{http.MethodPost, "/api/v2/auth/logout"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			request := httptest.NewRequest(tc.method, tc.path, nil)
			request.AddCookie(&http.Cookie{Name: "opl_console_session", Value: "this-is-a-foreign-session-token"})
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body = %s", response.Code, response.Body.String())
			}
			if cookie := sessionCookie(response, "opl_console_session"); cookie != nil {
				t.Fatalf("unknown session value was cleared or replaced: %+v", cookie)
			}
			if cookies := response.Result().Cookies(); len(cookies) != 0 {
				t.Fatalf("unknown session value produced Set-Cookie headers: %v", cookies)
			}
		})
	}
}

// TestLogoutClearsOnlyTheBFFSessionCookie shows the single place the BFF may
// delete the browser session: its own logout, after it verified the session and
// revoked it through CloudIdentity.
func TestLogoutClearsOnlyTheBFFSessionCookie(t *testing.T) {
	identity := &fakeLoginIdentity{session: loginSession(), cookie: "session-987"}
	server := NewServer(nil, identity).Handler()

	request := httptest.NewRequest(http.MethodPost, "/api/v2/auth/logout", nil)
	request.AddCookie(&http.Cookie{Name: "opl_console_session", Value: "session-987"})
	request.Header.Set("Origin", "http://"+request.Host)
	request.Header.Set("X-CSRF-Token", "csrf-987")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	cookie := sessionCookie(response, "opl_console_session")
	if cookie == nil || cookie.Value != "" || cookie.MaxAge >= 0 {
		t.Fatalf("logout session cookie = %+v, want an expired opl_console_session", cookie)
	}
	if cookie.Path != "/" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("logout changed console session cookie attributes: %+v", cookie)
	}
	if legacy := sessionCookie(response, "opl_session"); legacy != nil {
		t.Fatalf("logout touched Control Plane's opl_session: %+v", legacy)
	}
}
