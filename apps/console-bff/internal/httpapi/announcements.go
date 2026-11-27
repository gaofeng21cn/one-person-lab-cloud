package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	"opl-cloud/apps/console-bff/internal/clients"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// AnnouncementReader reaches the announcement owner. A valid Cloud User is the
// audience; no Tenant membership is required by this surface.
type AnnouncementReader interface {
	ListAnnouncements(context.Context, *api.CallContext, int, int) (*api.AnnouncementPage, error)
	MarkAnnouncementRead(context.Context, *api.CallContext, string) (*api.AnnouncementRead, error)
	ListOperatorAnnouncements(context.Context, *api.CallContext, int, int) (*api.AnnouncementPage, error)
}

// announcementSourceTag names the owner that answered the announcement read.
// The Console envelope keeps the answering owner visible to the browser.
const announcementSourceTag = "cloud-identity"

// registerAnnouncementRoutes publishes the announcement surfaces. The customer
// read and the read receipt are session-bound reads of the owner's own facts;
// the administrative surfaces are the owner's platform administrator commands.
func (s *Server) registerAnnouncementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v2/announcements", s.listAnnouncements)
	mux.HandleFunc("POST /api/v2/announcements/{announcementId}/read", s.markAnnouncementRead)
	mux.HandleFunc("GET /api/v2/operator/announcements", s.listOperatorAnnouncements)
	s.publisherRoute(mux, "POST /api/v2/admin/announcements", owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PUBLISHANNOUNCEMENT,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, "",
		func() proto.Message { return &api.PublishAnnouncementRequest{} },
		func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
			return s.tenant.PublishAnnouncement(r.Context(), &api.PublishAnnouncementRpcRequest{Context: c, Body: body.(*api.PublishAnnouncementRequest)})
		})
	s.publisherRoute(mux, "POST /api/v2/admin/announcements/{announcementId}/withdraw", owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_WITHDRAWANNOUNCEMENT,
		api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, "announcementId", nil,
		func(r *http.Request, c *api.CallContext, body proto.Message) (proto.Message, error) {
			return s.tenant.WithdrawAnnouncement(r.Context(), &api.WithdrawAnnouncementRpcRequest{Context: c, AnnouncementId: r.PathValue("announcementId")})
		})
}

func (s *Server) listAnnouncements(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	page, pageSize, ok := announcementPagination(w, r)
	if !ok {
		return
	}
	if s.announcements == nil {
		writePublisherError(w, r, 503, "owner_unconfigured", "announcement owner is not configured")
		return
	}
	ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
	out, err := s.announcements.ListAnnouncements(ctx, clients.CallContext(ctx), page, pageSize)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	if out == nil || out.GetPage() != int32(page) || out.GetPageSize() != int32(pageSize) {
		writePublisherError(w, r, 502, "invalid_owner_response", "invalid announcement owner readback")
		return
	}
	writeJSON(w, http.StatusOK, announcementSource(out))
}

func (s *Server) markAnnouncementRead(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
	call := clients.CallContext(ctx)
	if !requirePublisherWriteGuards(w, r, caller, call) {
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
	out, err := s.announcements.MarkAnnouncementRead(ctx, call, id)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	if out == nil || out.GetAnnouncementId() != id || out.GetReadAt() == "" {
		writePublisherError(w, r, 502, "invalid_owner_response", "invalid acknowledgement owner readback")
		return
	}
	writeJSON(w, http.StatusOK, contracts.AnnouncementRead{AnnouncementID: out.GetAnnouncementId(), ReadAt: out.GetReadAt()})
}

// listOperatorAnnouncements is the administrative view of the same owner. The
// operator privilege is CloudIdentity's decision, and the owner re-checks it.
func (s *Server) listOperatorAnnouncements(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	page, pageSize, ok := announcementPagination(w, r)
	if !ok {
		return
	}
	if s.announcements == nil {
		writePublisherError(w, r, 503, "owner_unconfigured", "announcement owner is not configured")
		return
	}
	// Operator surfaces are platform-wide. The scope is the session's own, with
	// the Tenant removed exactly as the admin publisher routes do.
	caller.Session = proto.Clone(caller.Session).(*api.Session)
	caller.Session.TenantId = nil
	ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTADMINANNOUNCEMENTS,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM},
		clients.CallContext(ctx).GetRequestId()); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	out, err := s.announcements.ListOperatorAnnouncements(ctx, clients.CallContext(ctx), page, pageSize)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	if out == nil || out.GetPage() != int32(page) || out.GetPageSize() != int32(pageSize) {
		writePublisherError(w, r, 502, "invalid_owner_response", "invalid announcement owner readback")
		return
	}
	writeJSON(w, http.StatusOK, announcementSource(out))
}

// announcementSource publishes the owner's own page in the Console envelope.
// The BFF neither derives an announcement nor a receipt of its own.
func announcementSource(page *api.AnnouncementPage) contracts.AnnouncementSource {
	items := make([]contracts.Announcement, 0, len(page.GetItems()))
	for _, item := range page.GetItems() {
		items = append(items, contracts.Announcement{
			ID: item.GetId(), Title: item.GetTitle(), Body: item.GetBody(), Status: item.GetStatus(),
			StartsAt: item.GetStartsAt(), EndsAt: item.GetEndsAt(), PublishedAt: item.GetPublishedAt(),
			CreatedAt: item.GetCreatedAt(), UpdatedAt: item.GetUpdatedAt(), Read: item.GetRead(),
		})
	}
	status := "available"
	if len(items) == 0 {
		status = "empty"
	}
	return contracts.AnnouncementSource{
		Source: announcementSourceTag, Status: status, Available: true,
		Data: &contracts.AnnouncementPage{
			Items: items, Total: int(page.GetTotal()), Page: int(page.GetPage()), PageSize: int(page.GetPageSize()),
		},
		FetchedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// announcementPagination accepts the one page and pageSize the owner reads.
// Any other selector is refused, so a browser cannot name a reader, a Tenant
// or an announcement through the query.
func announcementPagination(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	page, pageSize := 1, 20
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		writePublisherError(w, r, 400, "invalid_pagination", "one page and pageSize are required")
		return 0, 0, false
	}
	for key, values := range query {
		if (key != "page" && key != "pageSize") || len(values) != 1 {
			writePublisherError(w, r, 400, "invalid_pagination", "one page and pageSize are required")
			return 0, 0, false
		}
		value, convErr := strconv.Atoi(values[0])
		if convErr != nil || value < 1 || (key == "pageSize" && value > contracts.AnnouncementMaxPageSize) {
			writePublisherError(w, r, 400, "invalid_pagination", "positive page and pageSize up to 50 are required")
			return 0, 0, false
		}
		if key == "page" {
			page = value
		} else {
			pageSize = value
		}
	}
	return page, pageSize, true
}
