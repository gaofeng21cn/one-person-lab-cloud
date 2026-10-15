package httpapi

import (
	"encoding/json"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"net/http"
	"net/http/httptest"
	"opl-cloud/apps/console-bff/internal/clients"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
	"strings"
	"testing"
)

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
		t.Fatalf("missing original owner: status=%d body=%s, want 503", response.Code, response.Body.String())
	}
}

func TestAnnouncementCloudReadAndAckUseOriginalOwnerWithoutTenant(t *testing.T) {
	const token = "announcement-reader-test-token-000001"
	var requests int
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get(owneridentity.PeerHeader) != "console_bff" || r.Header.Get(owneridentity.TokenHeader) != token || r.Header.Get(contracts.CloudSessionHeader) != owneridentity.SessionReference("session-1") {
			t.Errorf("owner identity/session headers=%v", r.Header)
		}
		if r.Header.Get("Cookie") != "" || r.Header.Get("X-Actor-Id") != "" || r.Header.Get("X-Tenant-Id") != "" {
			t.Errorf("browser assertion reached owner: %v", r.Header)
		}
		if r.Method == http.MethodGet {
			if r.URL.Path != "/internal/cloud/announcements" || r.URL.Query().Get("page") != "2" || r.URL.Query().Get("pageSize") != "3" {
				t.Errorf("owner query=%s", r.URL)
			}
			_ = json.NewEncoder(w).Encode(contracts.AnnouncementSource{Source: "control-plane", Status: "available", Available: true, FetchedAt: "2026-10-15T00:00:00Z", Data: &contracts.AnnouncementPage{Items: []contracts.Announcement{{ID: "notice", Read: true}}, Total: 4, Page: 2, PageSize: 3}})
		} else {
			if r.URL.Path != "/internal/cloud/announcements/notice/read" || r.Header.Get("Idempotency-Key") != "original-intent" {
				t.Errorf("owner ACK=%s %v", r.URL, r.Header)
			}
			_ = json.NewEncoder(w).Encode(contracts.AnnouncementRead{AnnouncementID: "notice", ReadAt: "2026-07-19T01:00:00Z"})
		}
	}))
	defer owner.Close()
	readers, err := clients.Dial(clients.Config{ControlPlaneURL: owner.URL, ControlPlaneToken: token})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	identity := allowedIdentity()
	identity.session.TenantId = nil
	handler := NewServer(readers, identity).Handler()
	response := httptest.NewRecorder()
	request := sessionRequest(http.MethodGet, "/api/v2/announcements?page=2&pageSize=3")
	request.Header.Set("X-Actor-Id", "other")
	request.Header.Set("X-Tenant-Id", "other")
	handler.ServeHTTP(response, request)
	var page contracts.AnnouncementSource
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &page) != nil || page.Data == nil || !page.Data.Items[0].Read {
		t.Fatalf("page=%d %s", response.Code, response.Body.String())
	}
	for i := 0; i < 2; i++ {
		request = sessionRequest(http.MethodPost, "/api/v2/announcements/notice/read")
		request.Header.Set("Origin", "http://example.com")
		request.Header.Set("X-CSRF-Token", "csrf-1")
		request.Header.Set("Idempotency-Key", "original-intent")
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		var receipt contracts.AnnouncementRead
		if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &receipt) != nil || receipt.ReadAt != "2026-07-19T01:00:00Z" {
			t.Fatalf("ACK=%d %s", response.Code, response.Body.String())
		}
	}
	if requests != 3 {
		t.Fatalf("owner requests=%d", requests)
	}
}

func TestAnnouncementCloudGuardsAndUnavailableOwner(t *testing.T) {
	var calls int
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "unavailable", 503) }))
	defer owner.Close()
	readers, err := clients.Dial(clients.Config{ControlPlaneURL: owner.URL, ControlPlaneToken: strings.Repeat("a", 32)})
	if err != nil {
		t.Fatal(err)
	}
	defer readers.Close()
	identity := allowedIdentity()
	identity.session.TenantId = nil
	handler := NewServer(readers, identity).Handler()
	for _, tc := range []struct {
		origin, csrf, key string
		want              int
	}{
		{"http://other.example", "csrf-1", "key", 403},
		{"http://example.com", "wrong", "key", 403},
		{"http://example.com", "csrf-1", "", 400},
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
	for _, query := range []string{"?page=0", "?pageSize=101", "?page=1&page=2", "?page=oops"} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/announcements"+query))
		if response.Code != 400 {
			t.Errorf("query=%s status=%d", query, response.Code)
		}
	}
	if calls != 0 {
		t.Fatalf("owner reached before guards: %d", calls)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/announcements"))
	if response.Code != 503 || strings.Contains(response.Body.String(), `"items":[]`) {
		t.Fatalf("owner failure=%d %s", response.Code, response.Body.String())
	}
	identity.err = status.Error(codes.Unauthenticated, "expired")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/announcements"))
	if response.Code != 401 || calls != 1 {
		t.Fatalf("expired=%d owner calls=%d", response.Code, calls)
	}
}
