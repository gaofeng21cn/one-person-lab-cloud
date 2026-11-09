package identity

import (
	"context"
	"database/sql"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const agentAudience = "opl-cloud-mcp"
const agentCredentialPrefix = "opl_agent_"

func isAgentSession(ref string) bool { return strings.HasPrefix(ref, "agent:") }

// Only explicit action authorization may resolve an agent reference. Other
// session-only RPCs (for example wallet reads and credential management) retain
// their browser-session boundary rather than silently inheriting this authority.
func (s *Service) authorizationSession(ctx context.Context, ref string) (*api.Session, int64, bool, error) {
	if isAgentSession(ref) {
		return s.agentSession(ctx, ref)
	}
	return s.session(ctx, ref)
}

// agentReadAction is the exact owner call closure of the six read-only MCP
// tools. Workspace detail also composes two Serve-owned reads. This is an upper
// bound; the normal role, tenant and receiving-owner object checks still apply.
func agentReadAction(r *api.AuthorizationRequest) bool {
	resource := r.GetResource()
	switch r.GetAction() {
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTCOMPUTEPLANS,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTSTORAGEPLANS,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE:
		return r.GetAudienceOwner() == api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG && resource.GetKind() == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG && resource.GetId() == ""
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES:
		return r.GetAudienceOwner() == api.OwnerEnum_OWNER_ENUM_WORKSPACE && resource.GetKind() == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE && resource.GetId() == ""
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE:
		return r.GetAudienceOwner() == api.OwnerEnum_OWNER_ENUM_WORKSPACE && resource.GetKind() == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE && strings.TrimSpace(resource.GetId()) != ""
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION:
		return r.GetAudienceOwner() == api.OwnerEnum_OWNER_ENUM_WORKSPACE && resource.GetKind() == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION && strings.TrimSpace(resource.GetId()) != ""
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS:
		return r.GetAudienceOwner() == api.OwnerEnum_OWNER_ENUM_SERVE && resource.GetKind() == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE && strings.TrimSpace(resource.GetId()) != ""
	default:
		return false
	}
}

// The subordinate session never outlives or widens the browser session that
// issued it. Its fixed tenant is deliberately not retargeted by invitations.
func (s *Service) agentSession(ctx context.Context, ref string) (*api.Session, int64, bool, error) {
	var parentExpires time.Time
	err := s.DB.QueryRowContext(ctx, `SELECT p.expires_at FROM tenant.agent_credentials a
JOIN tenant.sessions c ON c.id=a.session_id
JOIN tenant.sessions p ON p.id=a.parent_session_id
WHERE a.session_id=$1 AND a.session_id='agent:'||a.id AND p.id NOT LIKE 'agent:%'
AND a.audience=$2 AND a.revoked_at IS NULL AND a.expires_at>now()
AND c.revoked_at IS NULL AND c.expires_at=a.expires_at
AND p.revoked_at IS NULL AND p.expires_at>now()
AND c.actor_id=a.actor_id AND p.actor_id=a.actor_id
AND c.tenant_id IS NOT DISTINCT FROM a.tenant_id AND p.tenant_id IS NOT DISTINCT FROM a.tenant_id`, ref, agentAudience).Scan(&parentExpires)
	if err != nil {
		return nil, 0, false, persistence(err)
	}
	// This is the existing live membership, permission-version and Gateway
	// active-subject read, applied to the subordinate session's frozen scope.
	session, version, admin, err := s.storedSession(ctx, ref)
	if err != nil {
		return nil, 0, false, err
	}
	if parentExpires.Before(session.ExpiresAt.AsTime()) {
		session.ExpiresAt = timestamppb.New(parentExpires)
	}
	permissions := session.Permissions[:0]
	for _, action := range session.Permissions {
		switch action {
		case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTCOMPUTEPLANS,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTSTORAGEPLANS,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETQUOTE,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS,
			api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS:
			permissions = append(permissions, action)
		}
	}
	session.Permissions = permissions
	return session, version, admin, nil
}

func (s *Service) agentCredentialManager(ctx context.Context, call *api.CallContext) (*api.Session, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if call.GetSessionId() == "" || isAgentSession(call.GetSessionId()) || call.GetAcceptedOperationGrantId() != "" || strings.TrimSpace(call.GetRequestId()) == "" {
		return nil, denied()
	}
	session, _, _, err := s.session(ctx, call.GetSessionId())
	if err != nil {
		return nil, err
	}
	if session.ActorId != call.GetActorId() || !scopeMatchesSession(session, call.GetScope()) {
		return nil, denied()
	}
	return session, nil
}

func (s *Service) IssueAgentCredential(ctx context.Context, request *api.IssueAgentCredentialRpcRequest) (*api.IssuedAgentCredential, error) {
	session, err := s.agentCredentialManager(ctx, request.GetContext())
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(request.GetLabel())
	if label == "" || !utf8.ValidString(label) || utf8.RuneCountInString(label) > 128 || strings.ContainsFunc(label, unicode.IsControl) {
		return nil, status.Error(codes.InvalidArgument, "credential label must contain 1 to 128 printable characters")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistence(err)
	}
	defer tx.Rollback()
	var parentExpiry time.Time
	if err = tx.QueryRowContext(ctx, `SELECT expires_at FROM tenant.sessions WHERE id=$1 AND actor_id=$2 AND tenant_id IS NOT DISTINCT FROM $3::text AND revoked_at IS NULL AND expires_at>now() FOR SHARE`, request.GetContext().GetSessionId(), session.ActorId, null(session.GetTenantId())).Scan(&parentExpiry); err != nil {
		return nil, persistence(err)
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	expires := now.Add(time.Hour)
	if parentExpiry.Before(expires) {
		expires = parentExpiry
	}
	if !expires.After(now) {
		return nil, denied()
	}
	id := randomID()
	ref := "agent:" + id
	credential := agentCredentialPrefix + randomID()
	// The child has no valid browser-cookie spelling: its id is not a
	// SessionReference, and its CSRF digest is independent random material.
	if _, err = tx.ExecContext(ctx, `INSERT INTO tenant.sessions(id,session_hash,actor_id,tenant_id,gateway_session_ref,csrf_hash,expires_at,display_name) VALUES($1,$2,$3,$4,$1,$5,$6,$7)`, ref, hash(credential), session.ActorId, null(session.GetTenantId()), hash(randomID()), expires, session.DisplayName); err != nil {
		return nil, persistence(err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO tenant.agent_credentials(id,credential_hash,label,parent_session_id,session_id,actor_id,tenant_id,audience,issued_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, id, hash(credential), label, request.GetContext().GetSessionId(), ref, session.ActorId, null(session.GetTenantId()), agentAudience, now, expires); err != nil {
		return nil, persistence(err)
	}
	if err = s.recordAudit(ctx, tx, auditEvent{tenantID: session.GetTenantId(), actorID: session.ActorId, action: "issueAgentCredential", resourceType: "tenant.agent_credential", resourceID: id, requestID: request.GetContext().GetRequestId(), details: map[string]any{"audience": agentAudience}, outcome: "confirmed"}); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, persistence(err)
	}
	metadata := &api.AgentCredential{Id: id, Label: label, Audience: agentAudience, IssuedAt: timestamppb.New(now), ExpiresAt: timestamppb.New(expires)}
	if session.GetTenantId() != "" {
		tid := session.GetTenantId()
		metadata.TenantId = &tid
	}
	return &api.IssuedAgentCredential{Metadata: metadata, Credential: credential}, nil
}

type agentCredentialScanner interface{ Scan(...any) error }

func scanAgentCredential(row agentCredentialScanner) (*api.AgentCredential, error) {
	out := &api.AgentCredential{}
	var tenant sql.NullString
	var issued, expires time.Time
	var revoked sql.NullTime
	if err := row.Scan(&out.Id, &out.Label, &tenant, &out.Audience, &issued, &expires, &revoked); err != nil {
		return nil, persistence(err)
	}
	if tenant.Valid {
		out.TenantId = &tenant.String
	}
	out.IssuedAt, out.ExpiresAt = timestamppb.New(issued), timestamppb.New(expires)
	if revoked.Valid {
		out.RevokedAt = timestamppb.New(revoked.Time)
	}
	return out, nil
}

func (s *Service) ResolveAgentCredential(ctx context.Context, request *api.ResolveAgentCredentialRequest) (*api.ResolvedAgentCredential, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	credential := request.GetCredential()
	if request.GetAudience() != agentAudience || !strings.HasPrefix(credential, agentCredentialPrefix) || len(credential) != len(agentCredentialPrefix)+64 {
		return nil, denied()
	}
	metadata, err := scanAgentCredential(s.DB.QueryRowContext(ctx, `SELECT id,label,tenant_id,audience,issued_at,expires_at,revoked_at FROM tenant.agent_credentials WHERE credential_hash=$1 AND audience=$2 AND revoked_at IS NULL AND expires_at>now()`, hash(credential), agentAudience))
	if err != nil {
		return nil, err
	}
	ref := "agent:" + metadata.Id
	session, _, _, err := s.agentSession(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &api.ResolvedAgentCredential{SessionReference: ref, Session: session, Metadata: metadata}, nil
}

func (s *Service) ListAgentCredentials(ctx context.Context, request *api.ListAgentCredentialsRpcRequest) (*api.AgentCredentialPage, error) {
	session, err := s.agentCredentialManager(ctx, request.GetContext())
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id,label,tenant_id,audience,issued_at,expires_at,revoked_at FROM tenant.agent_credentials WHERE actor_id=$1 ORDER BY issued_at DESC,id DESC`, session.ActorId)
	if err != nil {
		return nil, persistence(err)
	}
	defer rows.Close()
	out := &api.AgentCredentialPage{}
	for rows.Next() {
		credential, err := scanAgentCredential(rows)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, credential)
	}
	if err = rows.Err(); err != nil {
		return nil, persistence(err)
	}
	return out, nil
}

func (s *Service) RevokeAgentCredential(ctx context.Context, request *api.RevokeAgentCredentialRpcRequest) (*emptypb.Empty, error) {
	session, err := s.agentCredentialManager(ctx, request.GetContext())
	if err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, persistence(err)
	}
	defer tx.Rollback()
	var ref string
	var revoked sql.NullTime
	if err = tx.QueryRowContext(ctx, `SELECT session_id,revoked_at FROM tenant.agent_credentials WHERE id=$1 AND actor_id=$2 FOR UPDATE`, request.GetCredentialId(), session.ActorId).Scan(&ref, &revoked); err == sql.ErrNoRows {
		return nil, status.Error(codes.NotFound, "agent credential is not available")
	} else if err != nil {
		return nil, persistence(err)
	}
	if !revoked.Valid {
		if _, err = tx.ExecContext(ctx, `UPDATE tenant.agent_credentials SET revoked_at=now() WHERE id=$1`, request.GetCredentialId()); err != nil {
			return nil, persistence(err)
		}
		if _, err = tx.ExecContext(ctx, `UPDATE tenant.sessions SET revoked_at=now(),updated_at=now() WHERE id=$1`, ref); err != nil {
			return nil, persistence(err)
		}
		if err = s.recordAudit(ctx, tx, auditEvent{tenantID: session.GetTenantId(), actorID: session.ActorId, action: "revokeAgentCredential", resourceType: "tenant.agent_credential", resourceID: request.GetCredentialId(), requestID: request.GetContext().GetRequestId(), outcome: "confirmed"}); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, persistence(err)
	}
	return &emptypb.Empty{}, nil
}
