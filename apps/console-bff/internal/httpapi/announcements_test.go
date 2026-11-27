package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	"opl-cloud/apps/console-bff/internal/clients"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// fakeAnnouncementReader is the announcement owner the BFF reaches. It records
// the call context the BFF forwarded, so a test can prove the browser's own
// assertions never select the reader.
type fakeAnnouncementReader struct {
	*fakeReader
	tenant        api.TenantProductServiceClient
	page          *api.AnnouncementPage
	read          *api.AnnouncementRead
	err           error
	calls         int
	readCalls     int
	operatorCalls int
	lastCall      *api.CallContext
}

func (f *fakeAnnouncementReader) TenantClient() api.TenantProductServiceClient { return f.tenant }

func (f *fakeAnnouncementReader) ListAnnouncements(_ context.Context, call *api.CallContext, page, pageSize int) (*api.AnnouncementPage, error) {
	f.calls++
	f.lastCall = call
	if f.err != nil {
		return nil, f.err
	}
	if f.page == nil {
		return &api.AnnouncementPage{Items: []*api.Announcement{}, Page: int32(page), PageSize: int32(pageSize)}, nil
	}
	return f.page, nil
}

func (f *fakeAnnouncementReader) MarkAnnouncementRead(_ context.Context, call *api.CallContext, announcementID string) (*api.AnnouncementRead, error) {
	f.readCalls++
	f.lastCall = call
	if f.err != nil {
		return nil, f.err
	}
	if f.read == nil {
		return &api.AnnouncementRead{AnnouncementId: announcementID, ReadAt: "2026-07-19T01:00:00Z"}, nil
	}
	return f.read, nil
}

func (f *fakeAnnouncementReader) ListOperatorAnnouncements(_ context.Context, call *api.CallContext, page, pageSize int) (*api.AnnouncementPage, error) {
	f.operatorCalls++
	f.lastCall = call
	if f.err != nil {
		return nil, f.err
	}
	if f.page == nil {
		return &api.AnnouncementPage{Items: []*api.Announcement{}, Page: int32(page), PageSize: int32(pageSize)}, nil
	}
	return f.page, nil
}

// fakeTenantOwner answers the two administrator announcement commands.
type fakeTenantOwner struct {
	api.TenantProductServiceClient
	published     *api.Announcement
	withdrawn     *api.Announcement
	publishCalls  int
	withdrawCalls int
	lastPublish   *api.PublishAnnouncementRpcRequest
	lastWithdraw  *api.WithdrawAnnouncementRpcRequest
}

func (c *fakeTenantOwner) PublishAnnouncement(_ context.Context, request *api.PublishAnnouncementRpcRequest, _ ...grpc.CallOption) (*api.Announcement, error) {
	c.publishCalls++
	c.lastPublish = request
	return c.published, nil
}

func (c *fakeTenantOwner) WithdrawAnnouncement(_ context.Context, request *api.WithdrawAnnouncementRpcRequest, _ ...grpc.CallOption) (*api.Announcement, error) {
	c.withdrawCalls++
	c.lastWithdraw = request
	return c.withdrawn, nil
}

// allowAnnouncement builds the allowed decision for one platform-scoped
// announcement action, so the BFF's own request shape and the decision match.
func allowAnnouncement(actor string, action api.AuthorizationActionEnum, resourceID string) *api.AuthorizationDecision {
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM}
	if resourceID != "" {
		resource.Id = ptr(resourceID)
	}
	return &api.AuthorizationDecision{
		ActorId: actor, SessionId: ptr(owneridentity.SessionReference("session-1")),
		Scope:             &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}},
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Minute)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
		Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
		Action: action, AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Resource: resource,
	}
}

func announcementReader() *fakeAnnouncementReader {
	return &fakeAnnouncementReader{fakeReader: &fakeReader{}}
}

func TestAnnouncementRoutesRequireCloudSessionBeforeOwnerRead(t *testing.T) {
	handler := NewServer(nil, allowedIdentity()).Handler()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := "/api/v2/announcements"
		if method == http.MethodPost {
			path += "/notice/read"
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(method, path, nil))
		if response.Code != http.StatusUnauthorized {
			t.Errorf("%s missing session: status=%d body=%s, want 401", method, response.Code, response.Body.String())
		}
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/announcements"))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing announcement owner: status=%d body=%s, want 503", response.Code, response.Body.String())
	}
}

// TestAnnouncementRoutesReadTheOwnerWithoutTheRetainedBridge proves the
// customer read and the read receipt reach the announcement owner over its own
// typed client, with the live session reference and no browser assertion.
func TestAnnouncementRoutesReadTheOwnerWithoutTheRetainedBridge(t *testing.T) {
	owner := announcementReader()
	owner.page = &api.AnnouncementPage{
		Items:    []*api.Announcement{{Id: "notice", Title: "Notice", Body: "Body", Status: "published", CreatedAt: "2026-07-19T00:00:00Z", UpdatedAt: "2026-07-19T00:00:00Z", Read: true}},
		Total:    4,
		Page:     2,
		PageSize: 3,
	}
	identity := allowedIdentity()
	identity.session.TenantId = nil
	handler := NewServer(owner, identity).Handler()

	response := httptest.NewRecorder()
	request := sessionRequest(http.MethodGet, "/api/v2/announcements?page=2&pageSize=3")
	request.Header.Set("X-Actor-Id", "other")
	request.Header.Set("X-Tenant-Id", "other")
	handler.ServeHTTP(response, request)
	var page contracts.AnnouncementSource
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Data == nil {
		t.Fatalf("page=%d %s", response.Code, response.Body.String())
	}
	if page.Source != announcementSourceTag || page.Status != "available" || !page.Available {
		t.Fatalf("the owner answer was not published: %+v", page)
	}
	if len(page.Data.Items) != 1 || page.Data.Items[0].ID != "notice" || !page.Data.Items[0].Read || page.Data.Page != 2 || page.Data.PageSize != 3 {
		t.Fatalf("the owner page was rewritten: %+v", page.Data)
	}
	if owner.lastCall.GetSessionId() != owneridentity.SessionReference("session-1") || owner.lastCall.GetActorId() != "actor-1" {
		t.Fatalf("the owner call context=%v", owner.lastCall)
	}
	if owner.lastCall.GetScope().GetTenant() != nil {
		t.Fatalf("a customer read carried a Tenant scope: %v", owner.lastCall.GetScope())
	}

	for i := 0; i < 2; i++ {
		request = sessionRequest(http.MethodPost, "/api/v2/announcements/notice/read")
		request.Header.Set("Origin", "http://example.com")
		request.Header.Set("X-CSRF-Token", "csrf-1")
		request.Header.Set("Idempotency-Key", "original-intent")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var receipt contracts.AnnouncementRead
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &receipt) != nil || receipt.ReadAt != "2026-07-19T01:00:00Z" || receipt.AnnouncementID != "notice" {
			t.Fatalf("receipt=%d %s", response.Code, response.Body.String())
		}
	}
	if owner.calls != 1 || owner.readCalls != 2 {
		t.Fatalf("owner calls=%d read calls=%d", owner.calls, owner.readCalls)
	}
}

// TestAnnouncementRoutesRefuseUndeclaredSelectorsAndGuards keeps browser input
// from selecting a reader, a Tenant or an announcement, and keeps the write
// guards in front of the owner.
func TestAnnouncementRoutesRefuseUndeclaredSelectorsAndGuards(t *testing.T) {
	owner := announcementReader()
	identity := allowedIdentity()
	identity.session.TenantId = nil
	handler := NewServer(owner, identity).Handler()

	for _, query := range []string{"?page=0", "?pageSize=51", "?page=1&page=2", "?page=oops", "?userId=other", "?tenantId=other"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/announcements"+query))
		if response.Code != http.StatusBadRequest {
			t.Errorf("query=%s status=%d body=%s", query, response.Code, response.Body.String())
		}
	}
	for _, tc := range []struct {
		origin, csrf, key string
		want              int
	}{
		{"http://other.example", "csrf-1", "key", http.StatusForbidden},
		{"http://example.com", "wrong", "key", http.StatusForbidden},
		{"http://example.com", "csrf-1", "", http.StatusBadRequest},
	} {
		request := sessionRequest(http.MethodPost, "/api/v2/announcements/notice/read")
		request.Header.Set("Origin", tc.origin)
		request.Header.Set("X-CSRF-Token", tc.csrf)
		request.Header.Set("Idempotency-Key", tc.key)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != tc.want {
			t.Errorf("guard=%d body=%s want=%d", response.Code, response.Body.String(), tc.want)
		}
	}
	if owner.calls != 0 || owner.readCalls != 0 {
		t.Fatalf("the owner was reached before the guards: %d/%d", owner.calls, owner.readCalls)
	}
}

// TestAnnouncementOwnerFailureNeverBecomesAnEmptyPage proves an unavailable
// owner is an upstream failure, never an empty page the browser would read as
// "nothing was published".
func TestAnnouncementOwnerFailureNeverBecomesAnEmptyPage(t *testing.T) {
	owner := announcementReader()
	owner.err = status.Error(codes.Unavailable, "announcement owner unavailable")
	identity := allowedIdentity()
	identity.session.TenantId = nil
	handler := NewServer(owner, identity).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/announcements"))
	if response.Code != http.StatusServiceUnavailable || strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("owner failure=%d %s", response.Code, response.Body.String())
	}
}

// TestAnnouncementOperatorListRequiresThePlatformAdministrator proves the
// administrative page is the owner's answer and that CloudIdentity decides the
// platform administrator before the owner is read.
func TestAnnouncementOperatorListRequiresThePlatformAdministrator(t *testing.T) {
	owner := announcementReader()
	owner.page = &api.AnnouncementPage{
		Items:    []*api.Announcement{{Id: "notice", Status: "published", CreatedAt: "2026-07-19T00:00:00Z", UpdatedAt: "2026-07-19T00:00:00Z"}},
		Total:    1,
		Page:     1,
		PageSize: 20,
	}
	identity := allowedIdentity()
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTADMINANNOUNCEMENTS] = allowAnnouncement("actor-1", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTADMINANNOUNCEMENTS, "")
	handler := NewServer(owner, identity).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/operator/announcements?page=1&pageSize=20"))
	var page contracts.AnnouncementSource
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Data == nil || len(page.Data.Items) != 1 {
		t.Fatalf("operator page=%d %s", response.Code, response.Body.String())
	}
	if owner.operatorCalls != 1 {
		t.Fatalf("the owner was read %d times", owner.operatorCalls)
	}
	if len(identity.requests) != 1 || identity.requests[0].GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTADMINANNOUNCEMENTS {
		t.Fatalf("the operator decision=%v", identity.requests)
	}
	if identity.requests[0].GetScope().GetPlatform() == nil {
		t.Fatalf("the operator read was not platform scoped: %v", identity.requests[0].GetScope())
	}

	// A refused platform administrator never reaches the owner.
	denied := allowedIdentity()
	decision := allowAnnouncement("actor-1", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTADMINANNOUNCEMENTS, "")
	decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	denied.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTADMINANNOUNCEMENTS] = decision
	refused := announcementReader()
	handler = NewServer(refused, denied).Handler()
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/operator/announcements"))
	if response.Code != http.StatusForbidden || refused.operatorCalls != 0 {
		t.Fatalf("the refused operator read=%d owner calls=%d body=%s", response.Code, refused.operatorCalls, response.Body.String())
	}
}

// TestAnnouncementOperatorRouteNeverTouchesCookiesOnAnUnknownSession pins the
// 401 boundary for the administrative announcement read: a missing or unknown
// CloudIdentity session is refused without clearing the console session, and
// the owner is never read.
func TestAnnouncementOperatorRouteNeverTouchesCookiesOnAnUnknownSession(t *testing.T) {
	owner := announcementReader()
	identity := &fakeIdentity{err: status.Error(codes.Unauthenticated, "expired or unknown session")}
	handler := NewServer(owner, identity).Handler()

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/operator/announcements"))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("the unknown session mutated a cookie: %v", cookies)
	}
	if owner.operatorCalls != 0 {
		t.Fatalf("the unknown session reached the owner: %d", owner.operatorCalls)
	}
}

// TestAnnouncementAdminCommandsAreTheOwnersCommands proves the administrator
// publication and withdrawal reach the owner as its own commands, with the
// caller's idempotency key and the contract's success status.
func TestAnnouncementAdminCommandsAreTheOwnersCommands(t *testing.T) {
	tenant := &fakeTenantOwner{
		published: &api.Announcement{Id: "notice", Title: "Notice", Body: "Body", Status: "published", CreatedAt: "2026-07-19T00:00:00Z", UpdatedAt: "2026-07-19T00:00:00Z"},
		withdrawn: &api.Announcement{Id: "notice", Title: "Notice", Body: "Body", Status: "withdrawn", CreatedAt: "2026-07-19T00:00:00Z", UpdatedAt: "2026-07-19T00:00:00Z"},
	}
	owner := announcementReader()
	owner.tenant = tenant
	identity := allowedIdentity()
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PUBLISHANNOUNCEMENT] = allowAnnouncement("actor-1", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PUBLISHANNOUNCEMENT, "")
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_WITHDRAWANNOUNCEMENT] = allowAnnouncement("actor-1", api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_WITHDRAWANNOUNCEMENT, "notice")
	handler := NewServer(owner, identity).Handler()

	request := sessionRequest(http.MethodPost, "/api/v2/admin/announcements")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://example.com")
	request.Header.Set("X-CSRF-Token", "csrf-1")
	request.Header.Set("Idempotency-Key", "publish-intent")
	request.Body = http.NoBody
	request = httptest.NewRequest(http.MethodPost, "/api/v2/admin/announcements", strings.NewReader(`{"title":"Notice","body":"Body"}`))
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-1"})
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://example.com")
	request.Header.Set("X-CSRF-Token", "csrf-1")
	request.Header.Set("Idempotency-Key", "publish-intent")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("publish=%d %s", response.Code, response.Body.String())
	}
	if tenant.publishCalls != 1 || tenant.lastPublish.GetBody().GetTitle() != "Notice" || tenant.lastPublish.GetContext().GetIdempotencyKey() != "publish-intent" {
		t.Fatalf("the owner did not receive the command: %+v", tenant.lastPublish)
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v2/admin/announcements/notice/withdraw", nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-1"})
	request.Header.Set("Origin", "http://example.com")
	request.Header.Set("X-CSRF-Token", "csrf-1")
	request.Header.Set("Idempotency-Key", "withdraw-intent")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("withdraw=%d %s", response.Code, response.Body.String())
	}
	if tenant.withdrawCalls != 1 || tenant.lastWithdraw.GetAnnouncementId() != "notice" || tenant.lastWithdraw.GetContext().GetIdempotencyKey() != "withdraw-intent" {
		t.Fatalf("the owner did not receive the withdrawal: %+v", tenant.lastWithdraw)
	}
}

// TestAnnouncementClientsReachTheOwnerWithoutTheRetainedBridge proves the BFF
// client set exposes the announcement owner and no longer answers the retired
// Control Plane bridge.
func TestAnnouncementClientsReachTheOwnerWithoutTheRetainedBridge(t *testing.T) {
	readers, err := clients.Dial(clients.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	if _, err := readers.ListAnnouncements(t.Context(), &api.CallContext{ActorId: "actor-1"}, 1, 20); err == nil {
		t.Fatal("an unconfigured announcement owner answered")
	}
	if _, err := readers.MarkAnnouncementRead(t.Context(), &api.CallContext{ActorId: "actor-1"}, "notice"); err == nil {
		t.Fatal("an unconfigured announcement owner answered")
	}
	if _, err := readers.ListOperatorAnnouncements(t.Context(), &api.CallContext{ActorId: "actor-1"}, 1, 20); err == nil {
		t.Fatal("an unconfigured announcement owner answered")
	}
}
