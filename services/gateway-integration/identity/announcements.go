package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// CloudIdentity owns the platform announcements and the read receipts every
// authenticated Cloud User records for them.
//
// An announcement is platform content, not a Tenant fact: the row carries no
// Tenant column and the audience is every live User. The receipt is bound to
// the reader alone, so the same announcement and reader pair is one row and a
// repeated mark keeps its original timestamp.
//
// The owner resolves the reader from the live session itself. The browser never
// names a reader, and the Console BFF never derives one.

const (
	maxAnnouncementTitleRunes = 160
	maxAnnouncementBodyRunes  = 20_000
	maxAnnouncementPageSize   = 50
)

var errAnnouncementNotActive = status.Error(codes.NotFound, "the announcement is not readable")

// announcementRow is one stored announcement. The read receipt is joined in
// for a reader's own view and is absent from the administrative view.
type announcementRow struct {
	id          string
	title       string
	body        string
	status      string
	startsAt    sql.NullTime
	endsAt      sql.NullTime
	publishedAt sql.NullTime
	createdAt   time.Time
	updatedAt   time.Time
	read        bool
}

func (row announcementRow) announcement() *api.Announcement {
	return &api.Announcement{
		Id:          row.id,
		Title:       row.title,
		Body:        row.body,
		Status:      row.status,
		StartsAt:    announcementTime(row.startsAt),
		EndsAt:      announcementTime(row.endsAt),
		PublishedAt: announcementTime(row.publishedAt),
		CreatedAt:   row.createdAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   row.updatedAt.UTC().Format(time.RFC3339Nano),
		Read:        row.read,
	}
}

func announcementTime(value sql.NullTime) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format(time.RFC3339Nano)
}

// announcementReader resolves the live reader behind one call. The session is
// the only identity selector: a browser-supplied actor or Tenant never names
// the reader.
func (s *Service) announcementReader(ctx context.Context, call *api.CallContext) (string, error) {
	if call == nil || strings.TrimSpace(call.GetSessionId()) == "" {
		return "", status.Error(codes.Unauthenticated, "an active Cloud session is required")
	}
	session, _, _, err := s.session(ctx, call.GetSessionId())
	if err != nil {
		return "", err
	}
	actor := strings.TrimSpace(session.GetActorId())
	if actor == "" || (call.GetActorId() != "" && call.GetActorId() != actor) {
		return "", denied()
	}
	return actor, nil
}

// ListAnnouncements publishes the active announcements every authenticated User
// may read, with this reader's own receipt state. A withdrawn announcement and
// an announcement outside its published window are not published.
func (s *Service) ListAnnouncements(ctx context.Context, r *api.ListAnnouncementsRpcRequest) (*api.AnnouncementPage, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	reader, err := s.announcementReader(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	page, pageSize, err := announcementPaging(r.GetPage(), r.GetPageSize())
	if err != nil {
		return nil, err
	}
	return s.announcementPage(ctx, reader, page, pageSize, true)
}

// ListOperatorAnnouncements is the administrative view: every announcement the
// owner holds, including a withdrawn one and one outside its published window.
// Only a platform administrator may read it.
func (s *Service) ListOperatorAnnouncements(ctx context.Context, r *api.ListAnnouncementsRpcRequest) (*api.AnnouncementPage, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if err := s.authorizeAnnouncementAction(ctx, r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTADMINANNOUNCEMENTS, ""); err != nil {
		return nil, err
	}
	page, pageSize, err := announcementPaging(r.GetPage(), r.GetPageSize())
	if err != nil {
		return nil, err
	}
	return s.announcementPage(ctx, "", page, pageSize, false)
}

// announcementPage reads one page of announcements. Only the reader's own
// receipts decide the read flag, so the administrative view never borrows a
// receipt that belongs to somebody else.
func (s *Service) announcementPage(ctx context.Context, reader string, page, pageSize int32, activeOnly bool) (*api.AnnouncementPage, error) {
	where := ""
	if activeOnly {
		where = " WHERE a.status='published' AND (a.starts_at IS NULL OR a.starts_at<=now()) AND (a.ends_at IS NULL OR a.ends_at>now())"
	}
	var total int64
	if err := s.DB.QueryRowContext(ctx, "SELECT count(*) FROM tenant.announcements a"+where).Scan(&total); err != nil {
		return nil, persistence(err)
	}
	query := `SELECT a.id,a.title,a.body,a.status,a.starts_at,a.ends_at,a.published_at,a.created_at,a.updated_at,
		(r.announcement_id IS NOT NULL) AS read
		FROM tenant.announcements a
		LEFT JOIN tenant.announcement_reads r ON r.announcement_id=a.id AND r.actor_id=$1`
	args := []any{reader}
	query += where + " ORDER BY COALESCE(a.published_at,a.created_at) DESC, a.id DESC LIMIT $2 OFFSET $3"
	// The offset is computed in 64 bits: an untrusted page number must not
	// wrap around into a negative offset.
	args = append(args, pageSize, int64(page-1)*int64(pageSize))
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, persistence(err)
	}
	defer rows.Close()
	items := make([]*api.Announcement, 0, pageSize)
	for rows.Next() {
		row, scanErr := scanAnnouncement(rows)
		if scanErr != nil {
			return nil, persistence(scanErr)
		}
		items = append(items, row.announcement())
	}
	if err := rows.Err(); err != nil {
		return nil, persistence(err)
	}
	return &api.AnnouncementPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func scanAnnouncement(rows *sql.Rows) (announcementRow, error) {
	var row announcementRow
	err := rows.Scan(&row.id, &row.title, &row.body, &row.status, &row.startsAt, &row.endsAt, &row.publishedAt, &row.createdAt, &row.updatedAt, &row.read)
	return row, err
}

// MarkAnnouncementRead records one read receipt for the live reader and the
// named announcement. The receipt is unique per pair, so a repeated mark
// returns the original timestamp instead of writing a second row.
func (s *Service) MarkAnnouncementRead(ctx context.Context, r *api.MarkAnnouncementReadRpcRequest) (*api.AnnouncementRead, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	reader, err := s.announcementReader(ctx, r.GetContext())
	if err != nil {
		return nil, err
	}
	announcementID := strings.TrimSpace(r.GetAnnouncementId())
	if announcementID == "" || announcementID != r.GetAnnouncementId() {
		return nil, status.Error(codes.InvalidArgument, "an announcement id is required")
	}
	var active bool
	if err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant.announcements WHERE id=$1 AND status='published' AND (starts_at IS NULL OR starts_at<=now()) AND (ends_at IS NULL OR ends_at>now()))`, announcementID).Scan(&active); err != nil {
		return nil, persistence(err)
	}
	if !active {
		return nil, errAnnouncementNotActive
	}
	if _, err := s.DB.ExecContext(ctx, `
		INSERT INTO tenant.announcement_reads(announcement_id,actor_id,read_at) VALUES($1,$2,now())
		ON CONFLICT (announcement_id,actor_id) DO NOTHING`, announcementID, reader); err != nil {
		return nil, persistence(err)
	}
	var readAt time.Time
	if err := s.DB.QueryRowContext(ctx, `SELECT read_at FROM tenant.announcement_reads WHERE announcement_id=$1 AND actor_id=$2`, announcementID, reader).Scan(&readAt); err != nil {
		return nil, persistence(err)
	}
	return &api.AnnouncementRead{AnnouncementId: announcementID, ReadAt: readAt.UTC().Format(time.RFC3339Nano)}, nil
}

// PublishAnnouncement publishes one platform announcement for every
// authenticated User. The caller's idempotency key makes a repeated
// publication return the first announcement instead of creating a second one.
func (s *Service) PublishAnnouncement(ctx context.Context, r *api.PublishAnnouncementRpcRequest) (*api.Announcement, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	call := r.GetContext()
	if err := s.authorizeAnnouncementAction(ctx, call, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_PUBLISHANNOUNCEMENT, ""); err != nil {
		return nil, err
	}
	body := r.GetBody()
	if body == nil {
		return nil, status.Error(codes.InvalidArgument, "an announcement title and body are required")
	}
	title := strings.TrimSpace(body.GetTitle())
	text := strings.TrimSpace(body.GetBody())
	if title == "" || text == "" || utf8.RuneCountInString(title) > maxAnnouncementTitleRunes ||
		utf8.RuneCountInString(text) > maxAnnouncementBodyRunes || strings.ContainsRune(title, 0) || strings.ContainsRune(text, 0) {
		return nil, status.Error(codes.InvalidArgument, "an announcement title and body are required")
	}
	startsAt, startsTime, ok := announcementSchedule(body.GetStartsAt())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "the announcement start time is invalid")
	}
	endsAt, endsTime, ok := announcementSchedule(body.GetEndsAt())
	if !ok || (!endsTime.IsZero() && !startsTime.IsZero() && !endsTime.After(startsTime)) {
		return nil, status.Error(codes.InvalidArgument, "the announcement end time is invalid")
	}
	canonical := &api.PublishAnnouncementRpcRequest{
		Body: &api.PublishAnnouncementRequest{Title: title, Body: text, StartsAt: startsAt, EndsAt: endsAt},
	}
	id := "announcement-" + hash(call.GetActorId()+":"+call.GetIdempotencyKey())[:18]
	out := &api.Announcement{}
	err := s.command(ctx, call, "platform", "PublishAnnouncement", canonical, out, func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(ctx, `
			INSERT INTO tenant.announcements(id,title,body,status,starts_at,ends_at,published_at,published_by,created_at,updated_at)
			VALUES($1,$2,$3,'published',$4,$5,now(),$6,now(),now())
			ON CONFLICT (id) DO NOTHING`, id, title, text, announcementTimeArg(startsTime), announcementTimeArg(endsTime), call.GetActorId()); execErr != nil {
			return persistence(execErr)
		}
		row, readErr := readAnnouncement(ctx, tx, id)
		if readErr != nil {
			return readErr
		}
		if auditErr := s.recordAudit(ctx, tx, auditEvent{
			actorID:      call.GetActorId(),
			action:       "announcement.publish",
			resourceType: "announcement",
			resourceID:   id,
			requestID:    call.GetRequestId(),
			details:      map[string]any{"title": title, "startsAt": startsAt, "endsAt": endsAt},
			outcome:      "succeeded",
		}); auditErr != nil {
			return auditErr
		}
		proto.Merge(out, row.announcement())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// WithdrawAnnouncement withdraws one published announcement. A reader no
// longer sees it and can no longer record a receipt for it, while the receipts
// the earlier readers recorded are preserved.
func (s *Service) WithdrawAnnouncement(ctx context.Context, r *api.WithdrawAnnouncementRpcRequest) (*api.Announcement, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	call := r.GetContext()
	announcementID := strings.TrimSpace(r.GetAnnouncementId())
	if announcementID == "" || announcementID != r.GetAnnouncementId() {
		return nil, status.Error(codes.InvalidArgument, "an announcement id is required")
	}
	if err := s.authorizeAnnouncementAction(ctx, call, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_WITHDRAWANNOUNCEMENT, announcementID); err != nil {
		return nil, err
	}
	canonical := &api.WithdrawAnnouncementRpcRequest{AnnouncementId: announcementID}
	out := &api.Announcement{}
	err := s.command(ctx, call, "platform", "WithdrawAnnouncement", canonical, out, func(tx *sql.Tx) error {
		var state string
		scanErr := tx.QueryRowContext(ctx, `SELECT status FROM tenant.announcements WHERE id=$1 FOR UPDATE`, announcementID).Scan(&state)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return status.Error(codes.NotFound, "the announcement does not exist")
		}
		if scanErr != nil {
			return persistence(scanErr)
		}
		if state != "withdrawn" {
			if _, execErr := tx.ExecContext(ctx, `UPDATE tenant.announcements SET status='withdrawn',withdrawn_at=now(),updated_at=now() WHERE id=$1`, announcementID); execErr != nil {
				return persistence(execErr)
			}
			if auditErr := s.recordAudit(ctx, tx, auditEvent{
				actorID:      call.GetActorId(),
				action:       "announcement.withdraw",
				resourceType: "announcement",
				resourceID:   announcementID,
				requestID:    call.GetRequestId(),
				details:      map[string]any{"status": state},
				outcome:      "succeeded",
			}); auditErr != nil {
				return auditErr
			}
		}
		row, readErr := readAnnouncement(ctx, tx, announcementID)
		if readErr != nil {
			return readErr
		}
		proto.Merge(out, row.announcement())
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// authorizeAnnouncementAction decides the administrative announcement command
// at CloudIdentity. The decision is the owner's own, and the presented
// authorization context is verified against the live session before it is
// allowed.
func (s *Service) authorizeAnnouncementAction(ctx context.Context, call *api.CallContext, action api.AuthorizationActionEnum, resourceID string) error {
	if call == nil || call.GetScope().GetPlatform() == nil || strings.TrimSpace(call.GetActorId()) == "" ||
		call.GetSessionId() == "" || strings.TrimSpace(call.GetRequestId()) == "" {
		return denied()
	}
	request := &api.AuthorizationRequest{
		Scope:         call.GetScope(),
		ActorId:       call.GetActorId(),
		SessionId:     call.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Action:        action,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM},
		RequestId:     call.GetRequestId(),
	}
	if resourceID != "" {
		request.Resource.Id = proto.String(resourceID)
	}
	if call.GetAuthorizationContextId() != "" {
		request.AuthorizationContextId = proto.String(call.GetAuthorizationContextId())
	}
	decision, err := s.AuthorizeAction(ctx, request)
	if err != nil {
		return err
	}
	if decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return status.Error(codes.PermissionDenied, "platform administrator authorization required")
	}
	return nil
}

func readAnnouncement(ctx context.Context, tx *sql.Tx, id string) (announcementRow, error) {
	row := announcementRow{}
	err := tx.QueryRowContext(ctx, `SELECT id,title,body,status,starts_at,ends_at,published_at,created_at,updated_at,false FROM tenant.announcements WHERE id=$1`, id).
		Scan(&row.id, &row.title, &row.body, &row.status, &row.startsAt, &row.endsAt, &row.publishedAt, &row.createdAt, &row.updatedAt, &row.read)
	if errors.Is(err, sql.ErrNoRows) {
		return row, status.Error(codes.NotFound, "the announcement does not exist")
	}
	if err != nil {
		return row, persistence(err)
	}
	return row, nil
}

func announcementPaging(page, pageSize int32) (int32, int32, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > maxAnnouncementPageSize {
		return 0, 0, status.Error(codes.InvalidArgument, fmt.Sprintf("a page holds at most %d announcements", maxAnnouncementPageSize))
	}
	return page, pageSize, nil
}

// announcementSchedule reads one optional published-window bound. An empty
// value leaves that bound open.
func announcementSchedule(value string) (string, time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", time.Time{}, true
	}
	if value != strings.TrimSpace(value) {
		return "", time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", time.Time{}, false
	}
	return parsed.UTC().Format(time.RFC3339Nano), parsed.UTC(), true
}

func announcementTimeArg(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
