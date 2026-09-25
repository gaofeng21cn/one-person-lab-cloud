package identity

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/lib/pq"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

func actionName(a api.AuthorizationActionEnum) string {
	return strings.TrimPrefix(a.String(), "AUTHORIZATION_ACTION_ENUM_")
}
func ownerName(a api.OwnerEnum) string {
	return strings.ToLower(strings.TrimPrefix(a.String(), "OWNER_ENUM_"))
}
func resourceName(a api.AuthorizationResourceKind) string {
	return strings.ToLower(strings.TrimPrefix(a.String(), "AUTHORIZATION_RESOURCE_KIND_"))
}
func permitted(p actionPolicy, role string, admin bool) bool {
	for _, want := range p.roles {
		if want == "platform_admin" {
			if admin {
				return true
			}
			continue
		}
		if want == role || want == "member" && (role == "admin" || role == "owner") || want == "admin" && role == "owner" {
			return true
		}
	}
	return false
}
func null(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func (s *Service) AuthorizeAction(ctx context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	caller, ok := ownerservice.PeerOwner(ctx)
	if !ok || r.GetActorId() == "" || r.GetRequestId() == "" || r.GetScope() == nil || r.GetResource().GetKind() == 0 || ((r.GetSessionId() == "") == (r.GetAcceptedOperationGrantId() == "")) {
		return nil, denied()
	}
	// Only the BFF may originate an interactive authorization. Owners revalidate
	// their own action or a narrowly specified publisher continuation.
	audience := owneridentity.Service(ownerName(r.GetAudienceOwner()))
	if caller != owneridentity.ConsoleBFF && caller != audience {
		return nil, denied()
	}
	if r.GetAcceptedOperationGrantId() != "" {
		if caller == owneridentity.ConsoleBFF {
			return nil, denied()
		}
		return s.authorizeGrant(ctx, r)
	}
	session, version, admin, e := s.session(ctx, r.GetSessionId())
	if e != nil {
		return nil, e
	}
	if session.ActorId != r.ActorId || (r.ExpectedPermissionVersion != nil && r.GetExpectedPermissionVersion() != version) {
		return nil, denied()
	}
	if r.Action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACCEPTINVITATION {
		// Accepting an invitation is bound to the invitee's own live session rather
		// than to a Tenant role, because the invitee is not yet a member of the
		// inviting Tenant. The scope must still be the one the session itself
		// carries, and the accepting transaction re-checks the invitation subject.
		if r.AudienceOwner != api.OwnerEnum_OWNER_ENUM_TENANT || r.Resource.GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT || !scopeMatchesSession(session, r.Scope) {
			return nil, denied()
		}
		// A presented context is verified like any other action, so a decision
		// issued for a different action or invitation cannot be reused to accept.
		if r.AuthorizationContextId != nil {
			original, e := s.verifyOriginalContext(ctx, r, version)
			if e != nil {
				return nil, e
			}
			return original, nil
		}
		return s.saveDecision(ctx, r, version)
	}
	tid := r.Scope.GetTenant().GetTenantId()
	if tid != "" {
		if session.GetTenantId() != tid && !admin {
			return nil, denied()
		}
	} else if r.Scope.GetPlatform() == nil || !admin {
		return nil, denied()
	}
	p, exists := actions[r.Action]
	if !exists {
		// ResolveBuildInput is the pre-admission owner call made by CreateBuild.
		if r.Action != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESOLVEBUILDINPUT || caller != owneridentity.Capability.Service() {
			return nil, denied()
		}
		p = actionPolicy{api.OwnerEnum_OWNER_ENUM_CAPABILITY, []string{"admin", "owner"}}
	}
	if p.owner != r.AudienceOwner || !permitted(p, strings.ToLower(strings.TrimPrefix(session.GetRole().String(), "TENANT_ROLE_ENUM_")), admin) {
		return nil, denied()
	}
	if r.AuthorizationContextId != nil {
		original, e := s.context(ctx, r.GetAuthorizationContextId())
		if e != nil {
			return nil, e
		}
		if original.PermissionVersion != version || original.GetActorId() != r.ActorId || original.GetSessionId() != r.GetSessionId() || !proto.Equal(original.Scope, r.Scope) || !original.ExpiresAt.AsTime().After(time.Now()) {
			return nil, denied()
		}
		if !sameAction(original, r) && !publisherContinuation(original, r, caller) {
			return nil, denied()
		}
		if sameAction(original, r) {
			return s.verifyOriginalContext(ctx, r, version)
		}
	}
	return s.saveDecision(ctx, r, version)
}

// verifyOriginalContext revalidates a presented authorization context against the
// current live facts and returns the original decision only when it is still
// exactly this actor, session, scope, permission version and action/resource. It
// is the single place that decides whether an introspected context may be reused,
// so no action can bypass the binding by minting its own path.
func (s *Service) verifyOriginalContext(ctx context.Context, r *api.AuthorizationRequest, version int64) (*api.AuthorizationDecision, error) {
	original, e := s.context(ctx, r.GetAuthorizationContextId())
	if e != nil {
		return nil, e
	}
	if original.PermissionVersion != version || original.GetActorId() != r.ActorId || original.GetSessionId() != r.GetSessionId() || !proto.Equal(original.Scope, r.Scope) || !original.ExpiresAt.AsTime().After(time.Now()) || !sameAction(original, r) {
		return nil, denied()
	}
	original.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED
	original.Issuer = api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY
	return original, nil
}

// scopeMatchesSession reports whether the request scope is exactly the scope the
// live session itself carries: the session's Tenant when it has one, otherwise
// platform. A caller cannot widen or relocate its own scope.
func scopeMatchesSession(session *api.Session, scope *api.AuthorizationScope) bool {
	if session.GetTenantId() == "" {
		return scope.GetPlatform() != nil
	}
	return scope.GetTenant().GetTenantId() == session.GetTenantId()
}

func sameAction(d *api.AuthorizationDecision, r *api.AuthorizationRequest) bool {
	return d.Action == r.Action && d.AudienceOwner == r.AudienceOwner && proto.Equal(d.Resource, r.Resource)
}
func publisherContinuation(d *api.AuthorizationDecision, r *api.AuthorizationRequest, caller owneridentity.Service) bool {
	if d.Action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD {
		return (caller == owneridentity.Capability.Service() && r.Action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RESOLVEBUILDINPUT && proto.Equal(d.Resource, r.Resource)) || (caller == owneridentity.RuntimeControl.Service() && r.Action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS && r.Resource.GetId() == "")
	}
	return (d.Action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REGISTERRUNTIMEVERSION || d.Action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETBUILDRUNTIMEPOLICY) && caller == owneridentity.Capability.Service() && r.Action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTPUBLISHERNAMESPACES && r.Resource.GetId() == ""
}
func (s *Service) saveDecision(ctx context.Context, r *api.AuthorizationRequest, version int64) (*api.AuthorizationDecision, error) {
	now := time.Now()
	id := "auth_" + randomID()
	scope := "platform"
	tid := r.Scope.GetTenant().GetTenantId()
	if tid != "" {
		scope = "tenant"
	}
	_, e := s.DB.ExecContext(ctx, `INSERT INTO tenant.authorization_contexts(id,scope_type,tenant_id,actor_id,session_id,permission_version,audience_owner,action,resource_kind,resource_id,issuer,issued_at,expires_at,accepted_operation_grant_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,'cloud_identity',$11,$12,$13)`, id, scope, null(tid), r.ActorId, null(r.GetSessionId()), version, ownerName(r.AudienceOwner), actionName(r.Action), resourceName(r.Resource.Kind), null(r.Resource.GetId()), now, now.Add(30*time.Second), null(r.GetAcceptedOperationGrantId()))
	if e != nil {
		return nil, persistence(e)
	}
	return &api.AuthorizationDecision{Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, AuthorizationContextId: &id, Scope: r.Scope, ActorId: r.ActorId, SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId, AudienceOwner: r.AudienceOwner, Action: r.Action, Resource: r.Resource, PermissionVersion: version, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(now.Add(30 * time.Second))}, nil
}
func (s *Service) context(ctx context.Context, id string) (*api.AuthorizationDecision, error) {
	d := &api.AuthorizationDecision{Resource: &api.AuthorizationResource{}}
	var tid, session, grant, resource sql.NullString
	var scope, owner, action, kind string
	var issued, expires time.Time
	e := s.DB.QueryRowContext(ctx, `SELECT scope_type,tenant_id,actor_id,session_id,permission_version,audience_owner,action,resource_kind,resource_id,issued_at,expires_at,accepted_operation_grant_id FROM tenant.authorization_contexts WHERE id=$1 AND revoked_at IS NULL`, id).Scan(&scope, &tid, &d.ActorId, &session, &d.PermissionVersion, &owner, &action, &kind, &resource, &issued, &expires, &grant)
	if e != nil {
		return nil, persistence(e)
	}
	d.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
	if tid.Valid {
		d.Scope.Scope = &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tid.String}}
	}
	if session.Valid {
		d.SessionId = &session.String
	}
	if grant.Valid {
		d.AcceptedOperationGrantId = &grant.String
	}
	if resource.Valid {
		d.Resource.Id = &resource.String
	}
	d.AudienceOwner = api.OwnerEnum(api.OwnerEnum_value["OWNER_ENUM_"+strings.ToUpper(owner)])
	d.Action = api.AuthorizationActionEnum(api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_"+action])
	d.Resource.Kind = api.AuthorizationResourceKind(api.AuthorizationResourceKind_value["AUTHORIZATION_RESOURCE_KIND_"+strings.ToUpper(kind)])
	d.IssuedAt = timestamppb.New(issued)
	d.ExpiresAt = timestamppb.New(expires)
	d.AuthorizationContextId = &id
	return d, nil
}
func (s *Service) GetAuthorizationContext(ctx context.Context, r *api.GetAuthorizationContextRequest) (*api.AuthorizationDecision, error) {
	if e := peer(ctx, owneridentity.Service(ownerName(r.GetExpectedAudienceOwner()))); e != nil {
		return nil, e
	}
	d, e := s.context(ctx, r.GetAuthorizationContextId())
	if e != nil {
		return nil, e
	}
	if !d.ExpiresAt.AsTime().After(time.Now()) || d.AudienceOwner != r.ExpectedAudienceOwner || d.Action != r.ExpectedAction || !proto.Equal(d.Resource, r.ExpectedResource) {
		return nil, denied()
	}
	return s.AuthorizeAction(ctx, &api.AuthorizationRequest{Scope: d.Scope, ActorId: d.ActorId, SessionId: d.SessionId, AcceptedOperationGrantId: d.AcceptedOperationGrantId, AudienceOwner: d.AudienceOwner, Action: d.Action, Resource: d.Resource, RequestId: r.RequestId, AuthorizationContextId: d.AuthorizationContextId})
}

var buildActions = []api.AuthorizationActionEnum{api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_ACQUIREREFERENCE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDREFERENCE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION}

func (s *Service) IssueAcceptedOperationGrant(ctx context.Context, r *api.AcceptedOperationGrantRequest) (*api.AcceptedOperationGrant, error) {
	if e := peer(ctx, owneridentity.Build.Service()); e != nil {
		return nil, e
	}
	if s.BuildCommit == nil || r.GetOwnerCommitEvidence() == nil || r.GetRenewalConsentId() != "" || r.GetSubscriptionPeriodId() != "" {
		return nil, denied()
	}
	claimed := r.OwnerCommitEvidence
	actual, e := s.BuildCommit.ReadOwnerCommit(ctx, &api.ReadOwnerCommitRequest{Owner: claimed.Owner, OperationId: claimed.OperationId, ResourceId: claimed.ResourceId})
	if e != nil {
		return nil, e
	}
	if !proto.Equal(actual, claimed) || actual.Owner != api.OwnerEnum_OWNER_ENUM_BUILD || actual.GetAuthorizationContextId() != r.AuthorizationContextId {
		return nil, denied()
	}
	d, e := s.context(ctx, r.AuthorizationContextId)
	if e != nil {
		return nil, e
	}
	if d.Action != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD || d.AudienceOwner != api.OwnerEnum_OWNER_ENUM_BUILD || d.ActorId != actual.ActorId || !proto.Equal(d.Scope, actual.Scope) || !proto.Equal(d.Resource, actual.AuthorizationResource) || actual.AcceptedAction != d.Action || actual.AcceptedAt == nil || actual.AcceptedAt.AsTime().Before(d.IssuedAt.AsTime()) || actual.AcceptedAt.AsTime().After(d.ExpiresAt.AsTime()) || actual.AcceptedInputDigest == "" || actual.CommittedVersion < 1 {
		return nil, denied()
	}
	names := []string{}
	seen := map[api.AuthorizationActionEnum]bool{}
	for _, a := range r.AllowedActions {
		allowed := false
		for _, v := range buildActions {
			if a == v {
				allowed = true
			}
		}
		if !allowed || seen[a] {
			return nil, denied()
		}
		seen[a] = true
		names = append(names, actionName(a))
	}
	if len(names) == 0 {
		return nil, denied()
	}
	// Serialize duplicate issuance; recovery is bound to the immutable original
	// context and committed operation, and does not depend on a still-live cookie.
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return nil, persistence(e)
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "grant:"+actual.OperationId)
	if e != nil {
		return nil, persistence(e)
	}
	tid := d.Scope.GetTenant().GetTenantId()
	scope := "platform"
	if tid != "" {
		scope = "tenant"
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO tenant.accepted_operation_grants(id,scope_type,tenant_id,actor_id,accepted_operation_owner,accepted_operation_id,accepted_action,resource_id,accepted_permission_version,allowed_actions,issued_at,mode) VALUES($1,$2,$3,$4,'build',$5,$6,$7,$8,$9,now(),'continue_original') ON CONFLICT(accepted_operation_owner,accepted_operation_id) DO NOTHING`, "grant_"+randomID(), scope, null(tid), d.ActorId, actual.OperationId, actionName(d.Action), actual.ResourceId, d.PermissionVersion, pq.Array(names))
	if e != nil {
		return nil, persistence(e)
	}
	g, e := readGrant(ctx, tx, actual.OperationId, true)
	if e != nil {
		return nil, e
	}
	if g.ActorId != d.ActorId || g.ResourceId != actual.ResourceId || !proto.Equal(g.Scope, d.Scope) || len(g.AllowedActions) != len(r.AllowedActions) {
		return nil, denied()
	}
	for _, a := range g.AllowedActions {
		if !seen[a] {
			return nil, denied()
		}
	}
	if e = tx.Commit(); e != nil {
		return nil, persistence(e)
	}
	return g, nil
}

type queryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readGrant(ctx context.Context, db queryer, key string, operation bool) (*api.AcceptedOperationGrant, error) {
	column := "id"
	if operation {
		column = "accepted_operation_id"
	}
	g := &api.AcceptedOperationGrant{}
	var tid sql.NullString
	var actions []string
	var mode string
	var issued time.Time
	e := db.QueryRowContext(ctx, `SELECT id,tenant_id,actor_id,accepted_operation_id,resource_id,accepted_permission_version,allowed_actions,mode,issued_at FROM tenant.accepted_operation_grants WHERE `+column+`=$1 AND accepted_operation_owner='build' AND revoked_at IS NULL AND obligation_completed_at IS NULL AND (expires_at IS NULL OR expires_at>now())`, key).Scan(&g.Id, &tid, &g.ActorId, &g.AcceptedOperationId, &g.ResourceId, &g.AcceptedPermissionVersion, pq.Array(&actions), &mode, &issued)
	if e != nil {
		return nil, persistence(e)
	}
	g.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
	if tid.Valid {
		g.Scope.Scope = &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tid.String}}
	}
	for _, a := range actions {
		g.AllowedActions = append(g.AllowedActions, api.AuthorizationActionEnum(api.AuthorizationActionEnum_value["AUTHORIZATION_ACTION_ENUM_"+a]))
	}
	g.Mode = api.AcceptedGrantMode(api.AcceptedGrantMode_value["ACCEPTED_GRANT_MODE_"+strings.ToUpper(mode)])
	g.IssuedAt = timestamppb.New(issued)
	g.AcceptedOperationOwner = api.OwnerEnum_OWNER_ENUM_BUILD
	g.AcceptedAction = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD
	return g, nil
}
func (s *Service) authorizeGrant(ctx context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	g, e := readGrant(ctx, s.DB, r.GetAcceptedOperationGrantId(), false)
	if e != nil {
		return nil, e
	}
	if g.Mode == api.AcceptedGrantMode_ACCEPTED_GRANT_MODE_REVOKED || g.ActorId != r.ActorId || !proto.Equal(g.Scope, r.Scope) || s.BuildCommit == nil {
		return nil, denied()
	}
	allowed := false
	for _, a := range g.AllowedActions {
		if a == r.Action {
			allowed = true
		}
	}
	if !allowed {
		return nil, denied()
	}
	var active bool
	e = s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM tenant.tenants t JOIN tenant.tenant_members m ON m.tenant_id=t.id WHERE t.id=$1 AND m.actor_id=$2 AND t.status='active' AND m.revoked_at IS NULL AND m.role IN ('admin','owner'))`, g.Scope.GetTenant().GetTenantId(), g.ActorId).Scan(&active)
	if e != nil {
		return nil, persistence(e)
	}
	if !active && g.Mode == api.AcceptedGrantMode_ACCEPTED_GRANT_MODE_CONTINUE_ORIGINAL {
		if _, e = s.DB.ExecContext(ctx, `UPDATE tenant.accepted_operation_grants SET mode='closeout_only' WHERE id=$1 AND mode='continue_original'`, g.Id); e != nil {
			return nil, persistence(e)
		}
		g.Mode = api.AcceptedGrantMode_ACCEPTED_GRANT_MODE_CLOSEOUT_ONLY
	}
	if !active || g.Mode == api.AcceptedGrantMode_ACCEPTED_GRANT_MODE_CLOSEOUT_ONLY {
		if r.Action != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RELEASEREFERENCE && r.Action != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS && r.Action != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION {
			return nil, denied()
		}
	}
	evidence, e := s.BuildCommit.ReadOwnerCommit(ctx, &api.ReadOwnerCommitRequest{Owner: g.AcceptedOperationOwner, OperationId: g.AcceptedOperationId, ResourceId: g.ResourceId})
	if e != nil {
		return nil, e
	}
	matched := false
	if r.Action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS {
		matched = r.AudienceOwner == api.OwnerEnum_OWNER_ENUM_RUNTIME_CONTROL && r.Resource.Kind == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG && r.Resource.GetId() == ""
	} else if r.Action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETCAPABILITYVERSION {
		matched = r.AudienceOwner == api.OwnerEnum_OWNER_ENUM_CAPABILITY && r.Resource.Kind == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_BUILD && r.Resource.GetId() == g.ResourceId
	} else if r.AudienceOwner == api.OwnerEnum_OWNER_ENUM_CAPABILITY {
		for _, v := range evidence.ContinuationResources {
			if proto.Equal(v, r.Resource) {
				matched = true
			}
		}
	}
	if !matched {
		return nil, denied()
	}
	return s.saveDecision(ctx, r, g.AcceptedPermissionVersion)
}
