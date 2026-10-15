package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	contracts "opl-cloud/packages/contracts/go"
)

func TestAnnouncementOwnerConfigurationIsExplicit(t *testing.T) {
	for _, config := range []Config{
		{ControlPlaneURL: "http://127.0.0.1:8080"},
		{ControlPlaneToken: strings.Repeat("a", 32)},
		{ControlPlaneURL: "https://user:secret@example.test", ControlPlaneToken: strings.Repeat("a", 32)},
		{ControlPlaneURL: "https://example.test?token=a", ControlPlaneToken: strings.Repeat("a", 32)},
	} {
		if c, err := Dial(config); err == nil {
			c.Close()
			t.Fatalf("partial/credential URL configuration accepted")
		}
	}
}

func TestAnnouncementOwnerFailuresNeverBecomeEmptySuccessOrReplay(t *testing.T) {
	for _, tc := range []struct {
		name       string
		httpStatus int
		body       string
		code       codes.Code
	}{
		{"unauthenticated", 401, `{}`, codes.Unauthenticated},
		{"denied", 403, `{}`, codes.PermissionDenied},
		{"inactive", 404, `{}`, codes.NotFound},
		{"unproven lineage", 409, `{}`, codes.FailedPrecondition},
		{"unavailable", 503, `{}`, codes.Unavailable},
		{"invalid JSON", 200, `{`, codes.DataLoss},
		{"too large", 200, strings.Repeat(" ", 1<<20) + `{}`, codes.DataLoss},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.WriteHeader(tc.httpStatus)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer owner.Close()
			reader, err := newControlPlaneReader(owner.URL, strings.Repeat("a", 32))
			if err != nil {
				t.Fatal(err)
			}
			c := &Clients{controlPlane: reader}
			out, err := c.MarkAnnouncementRead(context.Background(), "reference", "notice", "original-intent")
			if status.Code(err) != tc.code || out != nil || calls != 1 {
				t.Fatalf("readback=%v code=%s calls=%d", out, status.Code(err), calls)
			}
		})
	}
}

func TestAnnouncementOwnerRedirectDoesNotForwardCredentials(t *testing.T) {
	var reached bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		_ = json.NewEncoder(w).Encode(contracts.AnnouncementRead{})
	}))
	defer other.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()
	reader, err := newControlPlaneReader(origin.URL, strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	c := &Clients{controlPlane: reader}
	if _, err = c.MarkAnnouncementRead(context.Background(), "reference", "notice", "original-intent"); status.Code(err) != codes.Unavailable || reached {
		t.Fatalf("redirect=%v reached=%v", err, reached)
	}
}

func TestAnnouncementReaderAcceptsLargestValidPage(t *testing.T) {
	items := make([]contracts.Announcement, contracts.AnnouncementMaxPageSize)
	for i := range items {
		items[i] = contracts.Announcement{ID: "notice", Title: strings.Repeat("界", contracts.AnnouncementMaxTitleRunes), Body: strings.Repeat("界", contracts.AnnouncementMaxBodyRunes)}
	}
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(contracts.AnnouncementSource{Source: "control-plane", Status: "available", Available: true, Data: &contracts.AnnouncementPage{Items: items, Total: len(items), Page: 1, PageSize: len(items)}})
	}))
	defer owner.Close()
	reader, err := newControlPlaneReader(owner.URL, strings.Repeat("a", 32))
	if err != nil {
		t.Fatal(err)
	}
	c := &Clients{controlPlane: reader}
	out, err := c.ListAnnouncements(context.Background(), "reference", 1, len(items))
	if err != nil || out == nil || out.Data == nil || len(out.Data.Items) != len(items) || out.Data.Items[0].Body != items[0].Body {
		t.Fatalf("valid full page rejected: %v", err)
	}
}
