package identity

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerstore"
)

type auditEvent struct {
	tenantID     string
	actorID      string
	action       string
	resourceType string
	resourceID   string
	requestID    string
	details      map[string]any
	outcome      string
}

// tenantOf returns the tenant id a CallContext is scoped to. A platform-scoped
// call has none; member governance is always tenant-scoped.
func tenantOf(c *api.CallContext) string {
	return c.GetScope().GetTenant().GetTenantId()
}

// authorize rechecks one member-governance action against this owner's own live
// authority. The caller is the Console BFF; the actor, session, scope, action,
// resource and current permission version are validated by the same
// CloudIdentityAuthorizeAction used externally, so a stale role or a revoked
// membership is refused here rather than trusted from the BFF.
func (s *Service) authorize(ctx context.Context, c *api.CallContext, action api.AuthorizationActionEnum, resourceID string) error {
	if e := peer(ctx, owneridentity.ConsoleBFF); e != nil {
		return e
	}
	if c == nil || strings.TrimSpace(c.GetActorId()) == "" || strings.TrimSpace(c.GetRequestId()) == "" || c.GetScope() == nil || c.GetSessionId() == "" {
		return status.Error(codes.Unauthenticated, "authenticated member context is required")
	}
	resource := &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT}
	if resourceID != "" {
		resource.Id = &resourceID
	}
	request := &api.AuthorizationRequest{Scope: c.GetScope(), ActorId: c.GetActorId(), SessionId: c.SessionId, AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT, Action: action, Resource: resource, RequestId: c.GetRequestId()}
	if c.GetAuthorizationContextId() != "" {
		id := c.GetAuthorizationContextId()
		request.AuthorizationContextId = &id
	}
	_, e := s.AuthorizeAction(ctx, request)
	return e
}

// authorizeInvitee authorizes acceptInvitation, whose contract permission is
// `invitee` rather than a Tenant role: the caller is not yet a member of the
// inviting Tenant. It uses the same AuthorizeAction path as every other member
// command rather than a second session check, so the audience, the inviter's own
// scope, the exact invitation resource and the original authorization context are
// all re-verified together and none of them can be widened by the caller. The
// accepted transaction then re-reads the invitation and requires it to name
// exactly this actor.
func (s *Service) authorizeInvitee(ctx context.Context, c *api.CallContext, invitationID string) error {
	if e := peer(ctx, owneridentity.ConsoleBFF); e != nil {
		return e
	}
	if c == nil || strings.TrimSpace(c.GetActorId()) == "" || strings.TrimSpace(c.GetRequestId()) == "" || c.GetScope() == nil || c.GetSessionId() == "" {
		return status.Error(codes.Unauthenticated, "authenticated invitee session is required")
	}
	request := &api.AuthorizationRequest{
		Scope:         c.GetScope(),
		ActorId:       c.GetActorId(),
		SessionId:     c.SessionId,
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_TENANT,
		Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACCEPTINVITATION,
		Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT, Id: &invitationID},
		RequestId:     c.GetRequestId(),
	}
	if c.GetAuthorizationContextId() != "" {
		id := c.GetAuthorizationContextId()
		request.AuthorizationContextId = &id
	}
	_, e := s.AuthorizeAction(ctx, request)
	return e
}

// recordAudit appends one immutable permission-audit row in the caller's
// transaction. It carries no token, Key or password.
func (s *Service) recordAudit(ctx context.Context, tx *sql.Tx, e auditEvent) error {
	details, err := json.Marshal(e.details)
	if err != nil {
		return persistence(err)
	}
	if details == nil {
		details = []byte("{}")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tenant.audit_events(id,tenant_id,actor_id,action,resource_type,resource_id,request_id,safe_details,outcome) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		"audit_"+randomID(), null(e.tenantID), e.actorID, e.action, e.resourceType, e.resourceID, e.requestID, details, e.outcome); err != nil {
		return persistence(err)
	}
	return nil
}

// rejection reports an owner decision that refuses the command but must keep its
// audit evidence. The audit row was written in the same transaction, so command
// commits it and returns the canonical failure rather than rolling the record back.
type rejection struct{ err error }

func (r rejection) Error() string { return r.err.Error() }
func (r rejection) Unwrap() error { return r.err }

func rejected(err error) error { return rejection{err: err} }

// command runs one idempotent member-management write. The stored response is a
// de-identified identity, so a replay returns the original object instead of a
// second mutation.
func (s *Service) command(ctx context.Context, c *api.CallContext, name string, request, response proto.Message, run func(*sql.Tx) error) error {
	if c.GetIdempotencyKey() == "" {
		return status.Error(codes.InvalidArgument, "idempotency key is required")
	}
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return persistence(e)
	}
	defer tx.Rollback()
	scope := tenantOf(c)
	if scope == "" {
		scope = "platform"
	}
	key, _ := json.Marshal([]string{scope, c.ActorId, name, c.IdempotencyKey})
	if _, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, string(key)); e != nil {
		return persistence(e)
	}
	raw, _ := protojson.Marshal(request)
	sum := hash(string(raw))
	input := ownerstore.IdempotencyInput{ID: "idem_" + randomID(), TenantScope: scope, ActorScope: c.ActorId, OperationName: name, IdempotencyKey: c.IdempotencyKey, RequestSHA256: sum}
	previous, found, e := s.store.LookupIdempotency(ctx, tx, input)
	if errors.Is(e, ownerstore.ErrIdempotencyConflict) {
		return owneridentity.WithErrorCode(status.Error(codes.AlreadyExists, "idempotency key reused with a different request"), api.ErrorCodeEnum_ERROR_CODE_ENUM_IDEMPOTENCY_CONFLICT)
	}
	if e != nil {
		return persistence(e)
	}
	if found {
		return protojson.Unmarshal(previous.ResponseBody, response)
	}
	if e = run(tx); e != nil {
		var refuse rejection
		if errors.As(e, &refuse) {
			// A refusal already wrote its immutable audit row; commit that evidence,
			// then report the refusal.
			if e = tx.Commit(); e != nil {
				return persistence(e)
			}
			return refuse.err
		}
		return e
	}
	body, _ := protojson.Marshal(response)
	input.ResourceID = "command"
	input.ResponseStatus = 200
	input.ResponseBody = body
	if e = s.store.RecordIdempotency(ctx, tx, input); e != nil {
		return persistence(e)
	}
	if e = tx.Commit(); e != nil {
		return persistence(e)
	}
	return nil
}

func roleEnum(role string) api.TenantRoleEnum {
	return api.TenantRoleEnum(api.TenantRoleEnum_value["TENANT_ROLE_ENUM_"+strings.ToUpper(role)])
}
func invitationRoleEnum(role string) api.InvitationRoleEnum {
	return api.InvitationRoleEnum(api.InvitationRoleEnum_value["INVITATION_ROLE_ENUM_"+strings.ToUpper(role)])
}
func roleRank(role string) int {
	switch role {
	case "owner":
		return 3
	case "admin":
		return 2
	case "member":
		return 1
	default:
		return 0
	}
}

// memberRow is the persisted membership shape this owner reads and writes.
type memberRow struct {
	id        string
	actorID   string
	role      string
	revokedAt sql.NullTime
	createdAt time.Time
}

func (m memberRow) message() *api.Member {
	out := &api.Member{Id: m.id, ActorId: m.actorID, Role: roleEnum(m.role), CreatedAt: timestamppb.New(m.createdAt)}
	// DisplayName is filled from the authorized Gateway directory read when this
	// deployment configures one and the subject resolves; otherwise the field is
	// left empty rather than fabricated.

	if m.revokedAt.Valid {
		out.Status = api.MemberStatusEnum_MEMBER_STATUS_ENUM_REVOKED
	} else {
		out.Status = api.MemberStatusEnum_MEMBER_STATUS_ENUM_ACTIVE
	}
	return out
}

// invitationRow is the persisted invitation shape. token_hash is never read
// back into a response; only its presence at insert matters.
type invitationRow struct {
	id        string
	invitee   string
	role      string
	expiresAt time.Time
	accepted  sql.NullTime
	revoked   sql.NullTime
	createdAt time.Time
}

func (i invitationRow) message(now time.Time) *api.Invitation {
	out := &api.Invitation{Id: i.id, InviteeGatewaySubjectId: i.invitee, Role: invitationRoleEnum(i.role), ExpiresAt: timestamppb.New(i.expiresAt), CreatedAt: timestamppb.New(i.createdAt)}
	switch {
	case i.accepted.Valid:
		out.Status = api.InvitationStatusEnum_INVITATION_STATUS_ENUM_ACCEPTED
	case i.revoked.Valid:
		out.Status = api.InvitationStatusEnum_INVITATION_STATUS_ENUM_REVOKED
	case !i.expiresAt.After(now):
		out.Status = api.InvitationStatusEnum_INVITATION_STATUS_ENUM_EXPIRED
	default:
		out.Status = api.InvitationStatusEnum_INVITATION_STATUS_ENUM_PENDING
	}
	return out
}

func cursorOf(created time.Time, id string) string {
	return strconv.FormatInt(created.UTC().UnixNano(), 10) + "|" + id
}
func parseCursor(raw string) (time.Time, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, "", true
	}
	ns, id, ok := strings.Cut(raw, "|")
	if !ok || id == "" {
		return time.Time{}, "", false
	}
	value, e := strconv.ParseInt(ns, 10, 64)
	if e != nil {
		return time.Time{}, "", false
	}
	return time.Unix(0, value).UTC(), id, true
}
func pageLimit(n int32) int {
	if n <= 0 {
		return 50
	}
	if n > 100 {
		return 100
	}
	return int(n)
}

// GetTenant returns the caller's active Tenant. The session already proved the
// membership; the row is re-read here so a concurrently suspended Tenant is
// reported by the owner rather than a cached session value.
func (s *Service) GetTenant(ctx context.Context, r *api.GetTenantRpcRequest) (*api.Tenant, error) {
	c := r.GetContext()
	tid := tenantOf(c)
	if e := s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETTENANT, tid); e != nil {
		return nil, e
	}
	out := &api.Tenant{AssetCustodyStatus: api.TenantAssetCustodyStatusEnum_TENANT_ASSET_CUSTODY_STATUS_ENUM_TENANT_OWNED}
	var restore sql.NullTime
	var created, updated time.Time
	var st string
	e := s.DB.QueryRowContext(ctx, `SELECT id,name,status,restore_until,created_at,updated_at FROM tenant.tenants WHERE id=$1`, tid).Scan(&out.Id, &out.Name, &st, &restore, &created, &updated)
	if e != nil {
		return nil, persistence(e)
	}
	out.Status = api.TenantStatusEnum(api.TenantStatusEnum_value["TENANT_STATUS_ENUM_"+strings.ToUpper(st)])
	if restore.Valid {
		out.RestoreUntil = timestamppb.New(restore.Time)
	}
	out.CreatedAt = timestamppb.New(created)
	out.UpdatedAt = timestamppb.New(updated)
	return out, nil
}

// ListMembers returns the Tenant's memberships, newest first. Revoked rows are
// included with Member.status=revoked so removal stays auditable rather than
// silently disappearing.
func (s *Service) ListMembers(ctx context.Context, r *api.ListMembersRpcRequest) (*api.MemberPage, error) {
	c := r.GetContext()
	tid := tenantOf(c)
	if e := s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTMEMBERS, tid); e != nil {
		return nil, e
	}
	after, afterID, ok := parseCursor(r.GetQueryCursor())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid cursor")
	}
	n := pageLimit(r.GetQueryLimit())
	rows, e := s.DB.QueryContext(ctx, `SELECT id,actor_id,role,revoked_at,created_at FROM tenant.tenant_members WHERE tenant_id=$1 AND ($2::timestamptz IS NULL OR (created_at,id)<($2,$3)) ORDER BY created_at DESC,id DESC LIMIT $4`, tid, sql.NullTime{Time: after, Valid: afterID != ""}, afterID, n+1)
	if e != nil {
		return nil, persistence(e)
	}
	defer rows.Close()
	out := &api.MemberPage{}
	for rows.Next() {
		var m memberRow
		if e = rows.Scan(&m.id, &m.actorID, &m.role, &m.revokedAt, &m.createdAt); e != nil {
			return nil, persistence(e)
		}
		out.Items = append(out.Items, m.message())
	}
	if e = rows.Err(); e != nil {
		return nil, persistence(e)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		last := out.Items[n-1]
		out.NextCursor = proto.String(cursorOf(last.CreatedAt.AsTime(), last.Id))
	}
	if err := s.resolveDisplayNames(ctx, out.Items); err != nil {
		return nil, err
	}
	return out, nil
}

// resolveDisplayNames fills Member.displayName from the authorized Gateway
// directory read. It resolves nothing when the deployment has no directory
// identity, and an individual subject that does not resolve stays empty: a name
// is never guessed from the actor id, and no copy of the directory is kept.
func (s *Service) resolveDisplayNames(ctx context.Context, members []*api.Member) error {
	if !s.Gateway.DirectoryConfigured() {
		return nil
	}
	for _, member := range members {
		name, err := s.Gateway.displayName(ctx, member.GetActorId())
		if err != nil {
			// A directory read that fails is not a reason to serve a wrong name, and
			// not a reason to fail the whole list: the row keeps its identity facts
			// and the name stays unresolved.
			continue
		}
		member.DisplayName = name
	}
	return nil
}

// ListInvitations returns the Tenant's invitations, newest first.
func (s *Service) ListInvitations(ctx context.Context, r *api.ListInvitationsRpcRequest) (*api.InvitationPage, error) {
	c := r.GetContext()
	tid := tenantOf(c)
	if e := s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTINVITATIONS, tid); e != nil {
		return nil, e
	}
	after, afterID, ok := parseCursor(r.GetQueryCursor())
	if !ok {
		return nil, status.Error(codes.InvalidArgument, "invalid cursor")
	}
	n := pageLimit(r.GetQueryLimit())
	rows, e := s.DB.QueryContext(ctx, `SELECT id,invitee_gateway_subject_id,role,expires_at,accepted_at,revoked_at,created_at FROM tenant.invitations WHERE tenant_id=$1 AND ($2::timestamptz IS NULL OR (created_at,id)<($2,$3)) ORDER BY created_at DESC,id DESC LIMIT $4`, tid, sql.NullTime{Time: after, Valid: afterID != ""}, afterID, n+1)
	if e != nil {
		return nil, persistence(e)
	}
	defer rows.Close()
	now := time.Now()
	out := &api.InvitationPage{}
	for rows.Next() {
		var i invitationRow
		if e = rows.Scan(&i.id, &i.invitee, &i.role, &i.expiresAt, &i.accepted, &i.revoked, &i.createdAt); e != nil {
			return nil, persistence(e)
		}
		out.Items = append(out.Items, i.message(now))
	}
	if e = rows.Err(); e != nil {
		return nil, persistence(e)
	}
	if len(out.Items) > n {
		out.Items = out.Items[:n]
		last := out.Items[n-1]
		out.NextCursor = proto.String(cursorOf(last.CreatedAt.AsTime(), last.Id))
	}
	return out, nil
}

// InviteMember creates a pending invitation addressed to one Gateway subject.
// It never creates that subject or a wallet; the invitee must already exist in
// Gateway and accept with the matching identity.
func (s *Service) InviteMember(ctx context.Context, r *api.InviteMemberRpcRequest) (*api.Invitation, error) {
	c := r.GetContext()
	tid := tenantOf(c)
	if e := s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_INVITEMEMBER, tid); e != nil {
		return nil, e
	}
	b := r.GetBody()
	invitee := strings.TrimSpace(b.GetInviteeGatewaySubjectId())
	role := strings.ToLower(strings.TrimPrefix(b.GetRole().String(), "INVITE_MEMBER_REQUEST_ROLE_ENUM_"))
	if _, e := strconv.ParseInt(invitee, 10, 64); e != nil || role != "admin" && role != "member" {
		return nil, status.Error(codes.InvalidArgument, "a Gateway subject id and admin/member role are required")
	}
	out := &api.Invitation{}
	request := &api.InviteMemberRpcRequest{Body: b}
	e := s.command(ctx, c, "InviteMember", request, out, func(tx *sql.Tx) error {
		// The inviter's live role is re-read from the locked Tenant so an admin
		// cannot grant above their own rank even if the session was cached.
		var callerRole string
		if e := tx.QueryRowContext(ctx, `SELECT m.role FROM tenant.tenants t JOIN tenant.tenant_members m ON m.tenant_id=t.id WHERE t.id=$1 AND t.status='active' AND m.actor_id=$2 AND m.revoked_at IS NULL FOR UPDATE OF t`, tid, c.ActorId).Scan(&callerRole); e != nil {
			// The caller's authority was re-checked before this transaction; losing
			// the row here means the membership or the Tenant changed in between, so
			// refuse the command rather than report an expired session.
			if errors.Is(e, sql.ErrNoRows) {
				return owneridentity.WithErrorCode(status.Error(codes.PermissionDenied, "the caller no longer manages this tenant"), api.ErrorCodeEnum_ERROR_CODE_ENUM_FORBIDDEN)
			}
			return persistence(e)
		}
		if roleRank(role) > roleRank(callerRole) {
			return owneridentity.WithErrorCode(status.Error(codes.PermissionDenied, "role above the inviter's own grant"), api.ErrorCodeEnum_ERROR_CODE_ENUM_FORBIDDEN)
		}
		var active bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant.tenant_members WHERE actor_id=$1 AND revoked_at IS NULL)`, invitee).Scan(&active); e != nil {
			return persistence(e)
		}
		if active {
			return owneridentity.WithErrorCode(status.Error(codes.AlreadyExists, "invitee already belongs to an active tenant"), api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID)
		}
		// Entry validation: when this deployment can read the Gateway directory,
		// only a subject the authority actually knows may be invited. The check uses
		// the service's own directory identity, never the inviter's, and it never
		// writes anything in the Gateway authority.
		if s.Gateway.DirectoryConfigured() {
			identity, e := s.Gateway.directoryIdentity(ctx, invitee)
			if e != nil || identity.Status != "active" {
				return owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "invitee is not an active Gateway subject"), api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID)
			}
		}
		expires := time.Now().Add(s.InvitationTTL)
		invitationID := "invite_" + randomID()
		token := "token_" + randomID() + randomID()
		if _, e := tx.ExecContext(ctx, `INSERT INTO tenant.invitations(id,tenant_id,invitee_gateway_subject_id,role,token_hash,invited_by,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7)`, invitationID, tid, invitee, role, hash(token), c.ActorId, expires); e != nil {
			return persistence(e)
		}
		row := invitationRow{id: invitationID, invitee: invitee, role: role, expiresAt: expires, createdAt: time.Now()}
		proto.Merge(out, row.message(time.Now()))
		return s.recordAudit(ctx, tx, auditEvent{tenantID: tid, actorID: c.ActorId, action: "inviteMember", resourceType: "tenant.invitation", resourceID: invitationID, requestID: c.RequestId, details: map[string]any{"invitee": invitee, "role": role}, outcome: "confirmed"})
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// AcceptInvitation binds the authenticated invitee to the inviting Tenant. It
// locks the invitation and enforces that the invitee has no other active
// membership, then bumps the Tenant permission version so any earlier
// authorization context is refused.
func (s *Service) AcceptInvitation(ctx context.Context, r *api.AcceptInvitationRpcRequest) (*api.Member, error) {
	c := r.GetContext()
	invitationID := strings.TrimSpace(r.GetInvitationId())
	if invitationID == "" {
		return nil, status.Error(codes.InvalidArgument, "invitation id is required")
	}
	if e := s.authorizeInvitee(ctx, c, invitationID); e != nil {
		return nil, e
	}
	out := &api.Member{}
	request := &api.AcceptInvitationRpcRequest{InvitationId: invitationID}
	e := s.command(ctx, c, "AcceptInvitation", request, out, func(tx *sql.Tx) error {
		var tid, invitee, role string
		var expires time.Time
		var accepted, revoked sql.NullTime
		if e := tx.QueryRowContext(ctx, `SELECT tenant_id,invitee_gateway_subject_id,role,expires_at,accepted_at,revoked_at FROM tenant.invitations WHERE id=$1 FOR UPDATE`, invitationID).Scan(&tid, &invitee, &role, &expires, &accepted, &revoked); e != nil {
			return owneridentity.WithErrorCode(status.Error(codes.NotFound, "invitation is not available"), api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID)
		}
		if invitee != c.ActorId || accepted.Valid || revoked.Valid || !expires.After(time.Now()) {
			return owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "invitation is no longer valid for this subject"), api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID)
		}
		var status0 string
		if e := tx.QueryRowContext(ctx, `SELECT status FROM tenant.tenants WHERE id=$1 FOR UPDATE`, tid).Scan(&status0); e != nil || status0 != "active" {
			return owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "tenant is not active"), api.ErrorCodeEnum_ERROR_CODE_ENUM_TENANT_INACTIVE)
		}
		var other bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant.tenant_members WHERE actor_id=$1 AND revoked_at IS NULL)`, c.ActorId).Scan(&other); e != nil {
			return persistence(e)
		}
		if other {
			return owneridentity.WithErrorCode(status.Error(codes.AlreadyExists, "subject already belongs to an active tenant"), api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID)
		}
		memberID := "member_" + randomID()
		if _, e := tx.ExecContext(ctx, `INSERT INTO tenant.tenant_members(id,tenant_id,actor_id,role) VALUES($1,$2,$3,$4)`, memberID, tid, c.ActorId, role); e != nil {
			return persistence(e)
		}
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.invitations SET accepted_by=$1,accepted_at=now(),updated_at=now() WHERE id=$2`, c.ActorId, invitationID); e != nil {
			return persistence(e)
		}
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.tenants SET permission_version=permission_version+1,updated_at=now() WHERE id=$1`, tid); e != nil {
			return persistence(e)
		}
		// The invitee's live sessions now belong to the Tenant so the next
		// session read reflects the accepted membership without a re-login.
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.sessions SET tenant_id=$1,updated_at=now() WHERE actor_id=$2 AND revoked_at IS NULL`, tid, c.ActorId); e != nil {
			return persistence(e)
		}
		created := time.Now()
		out.Id, out.ActorId, out.Role, out.Status, out.CreatedAt = memberID, c.ActorId, roleEnum(role), api.MemberStatusEnum_MEMBER_STATUS_ENUM_ACTIVE, timestamppb.New(created)
		return s.recordAudit(ctx, tx, auditEvent{tenantID: tid, actorID: c.ActorId, action: "acceptInvitation", resourceType: "tenant.member", resourceID: memberID, requestID: c.RequestId, details: map[string]any{"invitation": invitationID, "role": role}, outcome: "confirmed"})
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// RevokeInvitation invalidates a pending invitation. An already accepted or
// revoked invitation is refused with INVITATION_INVALID rather than rewritten.
func (s *Service) RevokeInvitation(ctx context.Context, r *api.RevokeInvitationRpcRequest) (*api.Invitation, error) {
	c := r.GetContext()
	tid := tenantOf(c)
	if e := s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVOKEINVITATION, tid); e != nil {
		return nil, e
	}
	invitationID := strings.TrimSpace(r.GetInvitationId())
	if invitationID == "" {
		return nil, status.Error(codes.InvalidArgument, "invitation id is required")
	}
	out := &api.Invitation{}
	request := &api.RevokeInvitationRpcRequest{InvitationId: invitationID}
	e := s.command(ctx, c, "RevokeInvitation", request, out, func(tx *sql.Tx) error {
		var i invitationRow
		var invTenant string
		if e := tx.QueryRowContext(ctx, `SELECT tenant_id,id,invitee_gateway_subject_id,role,expires_at,accepted_at,revoked_at,created_at FROM tenant.invitations WHERE id=$1 FOR UPDATE`, invitationID).Scan(&invTenant, &i.id, &i.invitee, &i.role, &i.expiresAt, &i.accepted, &i.revoked, &i.createdAt); e != nil {
			return owneridentity.WithErrorCode(status.Error(codes.NotFound, "invitation is not available"), api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID)
		}
		if invTenant != tid {
			// A cross-Tenant object is reported as absent, never as forbidden, so
			// another Tenant's invitation ids are not confirmed to exist.
			return owneridentity.WithErrorCode(status.Error(codes.NotFound, "invitation is not available"), api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID)
		}
		if i.accepted.Valid || i.revoked.Valid {
			if e := s.recordAudit(ctx, tx, auditEvent{tenantID: tid, actorID: c.ActorId, action: "revokeInvitation", resourceType: "tenant.invitation", resourceID: invitationID, requestID: c.RequestId, details: map[string]any{"reason": "invitation_invalid"}, outcome: "rejected"}); e != nil {
				return e
			}
			return rejected(owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "invitation is no longer pending"), api.ErrorCodeEnum_ERROR_CODE_ENUM_INVITATION_INVALID))
		}
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.invitations SET revoked_at=now(),updated_at=now() WHERE id=$1`, invitationID); e != nil {
			return persistence(e)
		}
		i.revoked = sql.NullTime{Time: time.Now(), Valid: true}
		proto.Merge(out, i.message(time.Now()))
		return s.recordAudit(ctx, tx, auditEvent{tenantID: tid, actorID: c.ActorId, action: "revokeInvitation", resourceType: "tenant.invitation", resourceID: invitationID, requestID: c.RequestId, details: map[string]any{"invitee": i.invitee}, outcome: "confirmed"})
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// UpdateMemberRole changes one active membership's role. It locks the Tenant row
// and refuses to remove the last owner, then bumps the Tenant permission version
// so the changed member's existing authorization contexts go stale.
func (s *Service) UpdateMemberRole(ctx context.Context, r *api.UpdateMemberRoleRpcRequest) (*api.Member, error) {
	c := r.GetContext()
	tid := tenantOf(c)
	if e := s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEMEMBERROLE, tid); e != nil {
		return nil, e
	}
	memberID := strings.TrimSpace(r.GetMemberId())
	newRole := strings.ToLower(strings.TrimPrefix(r.GetBody().GetRole().String(), "TENANT_ROLE_ENUM_"))
	if memberID == "" {
		return nil, status.Error(codes.InvalidArgument, "member id is required")
	}
	if newRole != "owner" && newRole != "admin" && newRole != "member" {
		return nil, status.Error(codes.InvalidArgument, "a tenant role is required")
	}
	out := &api.Member{}
	request := &api.UpdateMemberRoleRpcRequest{MemberId: memberID, Body: r.GetBody()}
	e := s.command(ctx, c, "UpdateMemberRole", request, out, func(tx *sql.Tx) error {
		if e := lockTenant(ctx, tx, tid); e != nil {
			return e
		}
		var m memberRow
		var memberTenant string
		if e := tx.QueryRowContext(ctx, `SELECT tenant_id,id,actor_id,role,revoked_at,created_at FROM tenant.tenant_members WHERE id=$1 FOR UPDATE`, memberID).Scan(&memberTenant, &m.id, &m.actorID, &m.role, &m.revokedAt, &m.createdAt); e != nil || memberTenant != tid || m.revokedAt.Valid {
			return status.Error(codes.NotFound, "member is not available")
		}
		if m.role == "owner" && newRole != "owner" {
			remaining, e := activeOwnersExcept(ctx, tx, tid, m.id)
			if e != nil {
				return e
			}
			if remaining == 0 {
				if e := s.recordAudit(ctx, tx, auditEvent{tenantID: tid, actorID: c.ActorId, action: "updateMemberRole", resourceType: "tenant.member", resourceID: m.id, requestID: c.RequestId, details: map[string]any{"reason": "last_owner"}, outcome: "rejected"}); e != nil {
					return e
				}
				return rejected(owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "the last owner cannot be demoted"), api.ErrorCodeEnum_ERROR_CODE_ENUM_LAST_OWNER))
			}
		}
		if m.role == newRole {
			proto.Merge(out, m.message())
			return nil
		}
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.tenant_members SET role=$1,updated_at=now() WHERE id=$2`, newRole, m.id); e != nil {
			return persistence(e)
		}
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.tenants SET permission_version=permission_version+1,updated_at=now() WHERE id=$1`, tid); e != nil {
			return persistence(e)
		}
		m.role = newRole
		proto.Merge(out, m.message())
		return s.recordAudit(ctx, tx, auditEvent{tenantID: tid, actorID: c.ActorId, action: "updateMemberRole", resourceType: "tenant.member", resourceID: m.id, requestID: c.RequestId, details: map[string]any{"role": newRole, "targetActor": m.actorID}, outcome: "confirmed"})
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// RemoveMember revokes one active membership and its live sessions. Like a role
// change it locks the Tenant row, refuses to remove the last owner, and bumps the
// permission version so the removed member loses all further access immediately.
func (s *Service) RemoveMember(ctx context.Context, r *api.RemoveMemberRpcRequest) (*emptypb.Empty, error) {
	c := r.GetContext()
	tid := tenantOf(c)
	if e := s.authorize(ctx, c, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REMOVEMEMBER, tid); e != nil {
		return nil, e
	}
	memberID := strings.TrimSpace(r.GetMemberId())
	if memberID == "" {
		return nil, status.Error(codes.InvalidArgument, "member id is required")
	}
	out := &emptypb.Empty{}
	request := &api.RemoveMemberRpcRequest{MemberId: memberID}
	e := s.command(ctx, c, "RemoveMember", request, out, func(tx *sql.Tx) error {
		if e := lockTenant(ctx, tx, tid); e != nil {
			return e
		}
		var m memberRow
		var memberTenant string
		if e := tx.QueryRowContext(ctx, `SELECT tenant_id,id,actor_id,role,revoked_at,created_at FROM tenant.tenant_members WHERE id=$1 FOR UPDATE`, memberID).Scan(&memberTenant, &m.id, &m.actorID, &m.role, &m.revokedAt, &m.createdAt); e != nil || memberTenant != tid || m.revokedAt.Valid {
			return status.Error(codes.NotFound, "member is not available")
		}
		if m.role == "owner" {
			remaining, e := activeOwnersExcept(ctx, tx, tid, m.id)
			if e != nil {
				return e
			}
			if remaining == 0 {
				if e := s.recordAudit(ctx, tx, auditEvent{tenantID: tid, actorID: c.ActorId, action: "removeMember", resourceType: "tenant.member", resourceID: m.id, requestID: c.RequestId, details: map[string]any{"reason": "last_owner"}, outcome: "rejected"}); e != nil {
					return e
				}
				return rejected(owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "the last owner cannot be removed"), api.ErrorCodeEnum_ERROR_CODE_ENUM_LAST_OWNER))
			}
		}
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.tenant_members SET revoked_at=now(),updated_at=now() WHERE id=$1`, m.id); e != nil {
			return persistence(e)
		}
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.sessions SET revoked_at=now(),updated_at=now() WHERE actor_id=$1 AND tenant_id=$2 AND revoked_at IS NULL`, m.actorID, tid); e != nil {
			return persistence(e)
		}
		if _, e := tx.ExecContext(ctx, `UPDATE tenant.tenants SET permission_version=permission_version+1,updated_at=now() WHERE id=$1`, tid); e != nil {
			return persistence(e)
		}
		return s.recordAudit(ctx, tx, auditEvent{tenantID: tid, actorID: c.ActorId, action: "removeMember", resourceType: "tenant.member", resourceID: m.id, requestID: c.RequestId, details: map[string]any{"targetActor": m.actorID}, outcome: "confirmed"})
	})
	if e != nil {
		return nil, e
	}
	return out, nil
}

// lockTenant serializes membership changes on the Tenant row, so two concurrent
// owner removals cannot each observe the other and both leave zero owners.
func lockTenant(ctx context.Context, tx *sql.Tx, tenantID string) error {
	var id string
	if e := tx.QueryRowContext(ctx, `SELECT id FROM tenant.tenants WHERE id=$1 AND status='active' FOR UPDATE`, tenantID).Scan(&id); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "tenant is not active"), api.ErrorCodeEnum_ERROR_CODE_ENUM_TENANT_INACTIVE)
		}
		return persistence(e)
	}
	return nil
}

func activeOwnersExcept(ctx context.Context, tx *sql.Tx, tenantID, exceptMemberID string) (int, error) {
	var n int
	if e := tx.QueryRowContext(ctx, `SELECT count(*) FROM tenant.tenant_members WHERE tenant_id=$1 AND role='owner' AND revoked_at IS NULL AND id<>$2`, tenantID, exceptMemberID).Scan(&n); e != nil {
		return 0, persistence(e)
	}
	return n, nil
}
