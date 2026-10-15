package httpapi

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strconv"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// AnnouncementReader reaches the original publication and ACK owner. A valid
// Cloud User is the audience; no Tenant membership is required by this surface.
type AnnouncementReader interface {
	ListAnnouncements(context.Context, string, int, int) (*contracts.AnnouncementSource, error)
	MarkAnnouncementRead(context.Context, string, string, string) (*contracts.AnnouncementRead, error)
}

func (s *Server) registerAnnouncementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v2/announcements", s.listAnnouncements)
	mux.HandleFunc("POST /api/v2/announcements/{announcementId}/read", s.markAnnouncementRead)
}

func (s *Server) listAnnouncements(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	page, pageSize := 1, 20
	for key, target := range map[string]*int{"page": &page, "pageSize": &pageSize} {
		if values, ok := r.URL.Query()[key]; ok {
			if len(values) != 1 {
				writePublisherError(w, r, 400, "invalid_pagination", "one page and pageSize are required")
				return
			}
			value, e := strconv.Atoi(values[0])
			if e != nil || value < 1 || (key == "pageSize" && value > contracts.AnnouncementMaxPageSize) {
				writePublisherError(w, r, 400, "invalid_pagination", "positive page and pageSize up to 50 are required")
				return
			}
			*target = value
		}
	}
	if s.announcements == nil {
		writePublisherError(w, r, 503, "owner_unconfigured", "announcement owner is not configured")
		return
	}
	out, err := s.announcements.ListAnnouncements(r.Context(), caller.SessionID, page, pageSize)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	if out == nil || !out.Available || out.Data == nil || out.Source != "control-plane" || (out.Status != "available" && out.Status != "empty") || out.Data.Page != page || out.Data.PageSize != pageSize {
		writePublisherError(w, r, 502, "invalid_owner_response", "invalid announcement owner readback")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) markAnnouncementRead(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	expected := caller.Session.GetCsrfToken()
	if expected == "" || !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(expected), []byte(r.Header.Get("X-CSRF-Token"))) != 1 {
		writePublisherError(w, r, 403, "csrf_required", "same-origin session CSRF required")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 256 {
		writePublisherError(w, r, 400, "idempotency_key_required", "a bounded idempotency key is required")
		return
	}
	id := r.PathValue("announcementId")
	if strings.TrimSpace(id) == "" || id != strings.TrimSpace(id) {
		writePublisherError(w, r, 400, "invalid_announcement_id", "announcement id is required")
		return
	}
	if s.announcements == nil {
		writePublisherError(w, r, 503, "owner_unconfigured", "announcement owner is not configured")
		return
	}
	out, err := s.announcements.MarkAnnouncementRead(r.Context(), caller.SessionID, id, key)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	var timestampErr error
	if out != nil {
		_, timestampErr = time.Parse(time.RFC3339Nano, out.ReadAt)
	}
	if out == nil || out.AnnouncementID != id || timestampErr != nil || out.ReadAt == "" {
		writePublisherError(w, r, 502, "invalid_owner_response", "invalid acknowledgement owner readback")
		return
	}
	writeJSON(w, http.StatusOK, out)
}
