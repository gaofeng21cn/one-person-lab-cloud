package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"opl-cloud/packages/contracts/go/owneridentity"
)

// TestCloudOperatorReadsServeTheConsoleSurfaces proves the Console BFF can
// read every operator surface the console renders through the established
// peer/token/session proof, without presenting a Control Plane admin session and
// without receiving any cookie mutation. The retained reads are the same functions
// the retained /api/operator/* routes call.
func TestCloudOperatorReadsServeTheConsoleSurfaces(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	operator := f.gateway.bind("operator-session", "operator-actor", "910")

	overview := f.request(t, http.MethodGet, "/internal/cloud/operator/overview", operator, "")
	assertCloudOperatorEnvelope(t, overview, "control-plane", "available")
	assertNoCookieMutation(t, overview)

	accounts := f.request(t, http.MethodGet, "/internal/cloud/operator/accounts?page=1&pageSize=20", operator, "")
	assertCloudOperatorEnvelope(t, accounts, "control-plane+sub2api", "available", "empty")
	assertNoCookieMutation(t, accounts)

	workspaces := f.request(t, http.MethodGet, "/internal/cloud/operator/workspaces?page=1&pageSize=20", operator, "")
	assertCloudOperatorEnvelope(t, workspaces, "control-plane+fabric+sub2api", "available", "empty")
	assertNoCookieMutation(t, workspaces)

	// The shared detail writer answers not-found, so an unknown id cannot become
	// an invented empty Workspace.
	detail := f.request(t, http.MethodGet, "/internal/cloud/operator/workspaces/ws-unknown", operator, "")
	if detail.code != http.StatusNotFound || !strings.Contains(string(detail.body), "workspace_not_found") {
		t.Fatalf("unknown workspace detail=%d %s", detail.code, detail.body)
	}
	assertNoCookieMutation(t, detail)
}

// TestCloudOperatorAnnouncementsShareTheRetainedWriter proves the operator
// announcement list the console still sent to the retained /api/operator/*
// route is reachable through the same BFF proof and answers with the same shared
// writer, page shape and control-plane source name.
func TestCloudOperatorAnnouncementsShareTheRetainedWriter(t *testing.T) {
	store := newMemoryTableStore()
	f := newCloudAnnouncementHTTPFixture(t, store)
	now := time.Now().UTC()
	seedCloudAnnouncement(t, store, "notice-active", "published", now.Add(-time.Hour), now.Add(time.Hour))
	operator := f.gateway.bind("operator-session", "operator-actor", "910")

	announcements := f.requestRaw(t, http.MethodGet, "/internal/cloud/operator/announcements?page=1&pageSize=20", operator, "")
	if announcements.code != http.StatusOK || !strings.HasPrefix(announcements.header.Get("Content-Type"), "application/json") {
		t.Fatalf("announcements route status=%d content-type=%q body=%s", announcements.code, announcements.header.Get("Content-Type"), announcements.body)
	}
	assertCloudOperatorEnvelope(t, announcements, "control-plane", "available")
	if announcements.header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("announcements cache policy=%v", announcements.header)
	}
	if !strings.Contains(string(announcements.body), `"id":"notice-active"`) || !strings.Contains(string(announcements.body), `"total":1`) {
		t.Fatalf("retained announcement page=%s", announcements.body)
	}
	assertNoCookieMutation(t, announcements)
}

// Rejected service proofs and selectors leave every browser cookie untouched.
func TestCloudOperatorReadsRequireTheServiceProofAndNeverTouchCookies(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	operator := f.gateway.bind("operator-session", "operator-actor", "910")
	unknown := owneridentity.SessionReference(strings.Repeat("a", 64))

	for _, tc := range []struct {
		name   string
		path   string
		ref    string
		code   int
		mutate func(http.Header)
		body   string
	}{
		{name: "unknown session", path: "/internal/cloud/operator/overview", ref: unknown, code: http.StatusUnauthorized},
		{name: "wrong token", path: "/internal/cloud/operator/overview", ref: operator, code: http.StatusUnauthorized, mutate: func(h http.Header) { h.Set(owneridentity.TokenHeader, "wrong") }},
		{name: "wrong peer", path: "/internal/cloud/operator/overview", ref: operator, code: http.StatusUnauthorized, mutate: func(h http.Header) { h.Set(owneridentity.PeerHeader, "capability") }},
		{name: "browser actor header", path: "/internal/cloud/operator/overview", ref: operator, code: http.StatusBadRequest, mutate: func(h http.Header) { h.Set("X-Opl-Actor-Id", "other") }},
		{name: "body", path: "/internal/cloud/operator/overview", ref: operator, code: http.StatusBadRequest, body: `{}`},
		{name: "overview query", path: "/internal/cloud/operator/overview?tenantId=tenant-a", ref: operator, code: http.StatusBadRequest},
		{name: "accounts selector", path: "/internal/cloud/operator/accounts?accountId=acct-a", ref: operator, code: http.StatusBadRequest},
		{name: "accounts duplicate page", path: "/internal/cloud/operator/accounts?page=1&page=2", ref: operator, code: http.StatusBadRequest},
		{name: "accounts page out of range", path: "/internal/cloud/operator/accounts?page=0", ref: operator, code: http.StatusBadRequest},
		{name: "announcements duplicate page", path: "/internal/cloud/operator/announcements?page=1&page=2", ref: operator, code: http.StatusBadRequest},
		{name: "announcements selector", path: "/internal/cloud/operator/announcements?accountId=acct-a", ref: operator, code: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mutate []func(http.Header)
			if tc.mutate != nil {
				mutate = append(mutate, tc.mutate)
			}
			response := f.request(t, http.MethodGet, tc.path, tc.ref, tc.body, mutate...)
			if response.code != tc.code {
				t.Fatalf("%s=%d %s want %d", tc.path, response.code, response.body, tc.code)
			}
			assertNoCookieMutation(t, response)
		})
	}
}

func assertCloudOperatorEnvelope(t *testing.T, response cloudAnnouncementHTTPResponse, source string, wantStatus ...string) {
	t.Helper()
	var envelope struct {
		Source     string          `json:"source"`
		Status     string          `json:"status"`
		Available  bool            `json:"available"`
		FetchedAt  string          `json:"fetchedAt"`
		ReasonCode string          `json:"reasonCode"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil {
		t.Fatalf("operator envelope: %v body=%s", err, response.body)
	}
	statusOK := false
	for _, status := range wantStatus {
		statusOK = statusOK || envelope.Status == status
	}
	if response.code != http.StatusOK || envelope.Source != source || !statusOK || !envelope.Available || envelope.FetchedAt == "" || envelope.ReasonCode != "" || len(envelope.Data) == 0 {
		t.Fatalf("operator envelope status=%d body=%s", response.code, response.body)
	}
}

func assertNoCookieMutation(t *testing.T, response cloudAnnouncementHTTPResponse) {
	t.Helper()
	// request() only returns the body; re-read the headers on a direct request is
	// avoided by asserting the helper captured them.
	if cookies := response.cookies(); len(cookies) != 0 {
		t.Fatalf("operator read mutated cookies: %v", cookies)
	}
}

func TestCloudOperatorSupplementalReadsKeepOwnerAndCookieBoundaries(t *testing.T) {
	f := newCloudAnnouncementHTTPFixture(t, newMemoryTableStore())
	operator := f.gateway.bind("operator-session", "operator-actor", "910")
	for _, tc := range []struct {
		path string
		code int
	}{
		{"health", http.StatusOK},
		{"runtime-observations", http.StatusBadGateway},
		{"reconciliation", http.StatusOK},
		{"workspace-launches/unknown/recovery", http.StatusNotFound},
		{"wallet-adjustments/unknown", http.StatusNotFound},
		{"application-deployments/unknown", http.StatusNotFound},
		{"registry/repositories", http.StatusServiceUnavailable},
		{"registry/tags/namespace/repository", http.StatusServiceUnavailable},
	} {
		path := "/internal/cloud/operator/" + tc.path
		response := f.request(t, http.MethodGet, path, operator, "")
		if response.code != tc.code {
			t.Fatalf("%s code=%d body=%s", path, response.code, response.body)
		}
		assertNoCookieMutation(t, response)
		response = f.request(t, http.MethodGet, path+"?tenantId=other", operator, "")
		if response.code != http.StatusBadRequest {
			t.Fatalf("%s accepted identity selector: %d", path, response.code)
		}
		assertNoCookieMutation(t, response)
		response = f.request(t, http.MethodGet, path, owneridentity.SessionReference(strings.Repeat("a", 64)), "")
		if response.code != http.StatusUnauthorized {
			t.Fatalf("%s accepted unknown session: %d", path, response.code)
		}
		assertNoCookieMutation(t, response)
	}
}
