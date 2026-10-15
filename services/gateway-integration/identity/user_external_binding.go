package identity

import (
	"context"
	"database/sql"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

var registryHostPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9.-]*[a-z0-9])?(:[0-9]{1,5})?$`)

// UserExternalBinding is the single Gateway-owned mapping. IDs are opaque here;
// only the Sub2API adapter interprets its upstream subject representation.
type UserExternalBinding struct {
	ActorID, Sub2APIUserID, ExternalIdentityRef  string
	RegistryHost, RegistryNamespace, Repository  string
	VerifiedAt, DestinationVerifiedAt, RevokedAt sql.NullTime
	CreatedAt, UpdatedAt                         time.Time
}

const userExternalBindingSelect = `SELECT actor_id,sub2api_user_id,external_identity_ref,
 COALESCE(registry_host,''),COALESCE(registry_namespace,''),COALESCE(repository,''),
 verified_at,destination_verified_at,revoked_at,created_at,updated_at
 FROM gateway.identity_mappings`

func scanUserExternalBinding(row interface{ Scan(...any) error }) (UserExternalBinding, error) {
	var b UserExternalBinding
	err := row.Scan(&b.ActorID, &b.Sub2APIUserID, &b.ExternalIdentityRef, &b.RegistryHost, &b.RegistryNamespace, &b.Repository,
		&b.VerifiedAt, &b.DestinationVerifiedAt, &b.RevokedAt, &b.CreatedAt, &b.UpdatedAt)
	return b, bindingPersistence(err)
}

func bindingPersistence(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "User external binding not found")
	}
	var constraint *pq.Error
	if errors.As(err, &constraint) && constraint.Code == "23505" {
		return status.Error(codes.AlreadyExists, "User external binding conflicts with an existing identity or destination")
	}
	return status.Error(codes.Unavailable, "Gateway User binding persistence unavailable")
}

func (b UserExternalBinding) usableIdentity() error {
	if b.RevokedAt.Valid {
		return status.Error(codes.FailedPrecondition, "User external binding is revoked")
	}
	if !b.VerifiedAt.Valid || b.ActorID == "" || b.Sub2APIUserID == "" || b.ExternalIdentityRef == "" {
		return status.Error(codes.FailedPrecondition, "User external identity is unverified")
	}
	return nil
}

func validateUserDestination(host, namespace, repository string) error {
	if !registryHostPattern.MatchString(host) || strings.Contains(host, "..") {
		return status.Error(codes.InvalidArgument, "registry host is invalid")
	}
	u, err := url.Parse("https://" + host)
	if err != nil || u.Host != host || u.Hostname() == "" {
		return status.Error(codes.InvalidArgument, "registry host is invalid")
	}
	for _, segment := range []string{namespace, repository} {
		if !repositoryNamePattern.MatchString(segment) || strings.Contains(segment, "..") {
			return status.Error(codes.InvalidArgument, "registry namespace and repository must each be one safe path segment")
		}
	}
	return nil
}

// ReadUserExternalBinding reads durable state on every call. It never derives an
// identity from an actor id, a Tenant, an email, or an installation default.
func (s *GatewayStore) ReadUserExternalBinding(ctx context.Context, actorID string) (UserExternalBinding, error) {
	b, err := scanUserExternalBinding(s.db.QueryRowContext(ctx, userExternalBindingSelect+` WHERE actor_id=$1`, actorID))
	if err != nil {
		return b, err
	}
	return b, b.usableIdentity()
}

// bindAuthenticatedUser persists the authenticated upstream identity before a
// Cloud session can be issued. Reauthentication cannot change a binding or undo
// a revocation. An installation may assign the OCI destination on first login;
// an existing destination is retained even when the User's email changes.
func (s *GatewayStore) bindAuthenticatedUser(ctx context.Context, actorID, subjectID, host, namespace, repository string) (UserExternalBinding, error) {
	if strings.TrimSpace(actorID) == "" || actorID != strings.TrimSpace(actorID) || strings.TrimSpace(subjectID) == "" || subjectID != strings.TrimSpace(subjectID) {
		return UserExternalBinding{}, status.Error(codes.InvalidArgument, "actor and Sub2API User identities are required")
	}
	if host != "" || namespace != "" || repository != "" {
		if err := validateUserDestination(host, namespace, repository); err != nil {
			return UserExternalBinding{}, err
		}
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserExternalBinding{}, bindingPersistence(err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "gateway.user_binding:"+actorID); err != nil {
		return UserExternalBinding{}, bindingPersistence(err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO gateway.identity_mappings(id,actor_id,sub2api_user_id,external_identity_ref,verified_at)
 VALUES($1,$2,$3,$4,now()) ON CONFLICT(actor_id) DO NOTHING`, "identity_"+randomID()[:24], actorID, subjectID, "sub2api:user:"+subjectID)
	if err != nil {
		return UserExternalBinding{}, bindingPersistence(err)
	}
	b, err := scanUserExternalBinding(tx.QueryRowContext(ctx, userExternalBindingSelect+` WHERE actor_id=$1 FOR UPDATE`, actorID))
	if err != nil {
		return b, err
	}
	if b.Sub2APIUserID != subjectID {
		return b, status.Error(codes.AlreadyExists, "authenticated User conflicts with the existing binding")
	}
	if b.RevokedAt.Valid {
		return b, status.Error(codes.FailedPrecondition, "User external binding is revoked")
	}
	if !b.VerifiedAt.Valid {
		if _, err = tx.ExecContext(ctx, `UPDATE gateway.identity_mappings SET verified_at=now(),external_identity_ref=$2,updated_at=now() WHERE actor_id=$1`, actorID, "sub2api:user:"+subjectID); err != nil {
			return b, bindingPersistence(err)
		}
	}
	if b.RegistryHost == "" && host != "" {
		if _, err = tx.ExecContext(ctx, `UPDATE gateway.identity_mappings SET registry_host=$2,registry_namespace=$3,repository=$4,destination_verified_at=now(),updated_at=now() WHERE actor_id=$1`, actorID, host, namespace, repository); err != nil {
			return b, bindingPersistence(err)
		}
	}
	b, err = scanUserExternalBinding(tx.QueryRowContext(ctx, userExternalBindingSelect+` WHERE actor_id=$1`, actorID))
	if err != nil {
		return b, err
	}
	if err = tx.Commit(); err != nil {
		return b, bindingPersistence(err)
	}
	return b, nil
}

// BindUserExternalDestination assigns a destination to an already authenticated
// identity. It never fabricates an identity mapping or replaces a destination.
func (s *GatewayStore) BindUserExternalDestination(ctx context.Context, actorID, subjectID, host, namespace, repository string) (UserExternalBinding, error) {
	if err := validateUserDestination(host, namespace, repository); err != nil {
		return UserExternalBinding{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return UserExternalBinding{}, bindingPersistence(err)
	}
	defer tx.Rollback()
	b, err := scanUserExternalBinding(tx.QueryRowContext(ctx, userExternalBindingSelect+` WHERE actor_id=$1 FOR UPDATE`, actorID))
	if err != nil {
		return b, err
	}
	if err = b.usableIdentity(); err != nil {
		return b, err
	}
	if b.Sub2APIUserID != subjectID {
		return b, status.Error(codes.AlreadyExists, "Sub2API User conflicts with the authenticated mapping")
	}
	if b.RegistryHost != "" && (b.RegistryHost != host || b.RegistryNamespace != namespace || b.Repository != repository) {
		return b, status.Error(codes.AlreadyExists, "User already has a different OCI destination")
	}
	if _, err = tx.ExecContext(ctx, `UPDATE gateway.identity_mappings SET registry_host=$2,registry_namespace=$3,repository=$4,destination_verified_at=COALESCE(destination_verified_at,now()),updated_at=now() WHERE actor_id=$1`, actorID, host, namespace, repository); err != nil {
		return b, bindingPersistence(err)
	}
	b, err = scanUserExternalBinding(tx.QueryRowContext(ctx, userExternalBindingSelect+` WHERE actor_id=$1`, actorID))
	if err != nil {
		return b, err
	}
	if err = tx.Commit(); err != nil {
		return b, bindingPersistence(err)
	}
	return b, nil
}

func (s *Service) userBinding(ctx context.Context, actorID string) (UserExternalBinding, error) {
	if s.GatewayStore == nil {
		return UserExternalBinding{}, status.Error(codes.Unavailable, "Gateway User binding persistence is not configured")
	}
	return s.GatewayStore.ReadUserExternalBinding(ctx, actorID)
}

func (s *Service) bindingReadback(b UserExternalBinding) (*api.UserExternalBindingReadback, error) {
	if err := b.usableIdentity(); err != nil {
		return nil, err
	}
	if !b.DestinationVerifiedAt.Valid {
		return nil, status.Error(codes.FailedPrecondition, "User OCI destination is unverified")
	}
	if err := validateUserDestination(b.RegistryHost, b.RegistryNamespace, b.Repository); err != nil {
		return nil, status.Error(codes.FailedPrecondition, "User OCI destination is invalid")
	}
	if b.RegistryHost != s.registryHost || b.RegistryNamespace != s.registryNamespace {
		return nil, status.Error(codes.FailedPrecondition, "User OCI destination conflicts with installation registry")
	}
	return &api.UserExternalBindingReadback{ActorId: b.ActorID, Sub2ApiUserId: b.Sub2APIUserID, RegistryHost: b.RegistryHost,
		RegistryNamespace: b.RegistryNamespace, Repository: b.Repository, Status: api.UserExternalBindingReadbackStatusEnum_USER_EXTERNAL_BINDING_READBACK_STATUS_ENUM_ACTIVE, Verification: api.UserExternalBindingReadbackVerificationEnum_USER_EXTERNAL_BINDING_READBACK_VERIFICATION_ENUM_CONFIRMED,
		VerificationEvidenceRef: b.ExternalIdentityRef, VerifiedAt: timestamppb.New(b.VerifiedAt.Time), DestinationVerifiedAt: timestamppb.New(b.DestinationVerifiedAt.Time),
		CreatedAt: timestamppb.New(b.CreatedAt), UpdatedAt: timestamppb.New(b.UpdatedAt)}, nil
}

// GetSelfUserExternalBinding resolves only the User behind the live BFF session.
func (s *Service) GetSelfUserExternalBinding(ctx context.Context, r *api.GetSelfUserExternalBindingRequest) (*api.UserExternalBindingReadback, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	session, _, _, err := s.session(ctx, r.GetContext().GetSessionId())
	if err != nil {
		return nil, err
	}
	if r.GetContext().GetActorId() != "" && r.GetContext().GetActorId() != session.ActorId {
		return nil, denied()
	}
	b, err := s.userBinding(ctx, session.ActorId)
	if err != nil {
		return nil, err
	}
	return s.bindingReadback(b)
}

// ReadUserExternalIdentity is the Build owner's read. The target must match the
// live session User; service identity alone cannot select another User's output.
func (s *Service) ReadUserExternalIdentity(ctx context.Context, r *api.UserExternalBindingReadRequest) (*api.UserExternalBindingReadback, error) {
	if err := peer(ctx, owneridentity.Build.Service()); err != nil {
		return nil, err
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	session, _, _, err := s.session(ctx, r.GetContext().GetSessionId())
	if err != nil {
		return nil, err
	}
	if !scopeMatchesSession(session, r.GetContext().GetScope()) || r.GetActorId() == "" || r.GetActorId() != session.ActorId || r.GetContext().GetActorId() != session.ActorId {
		return nil, denied()
	}
	b, err := s.userBinding(ctx, session.ActorId)
	if err != nil {
		return nil, err
	}
	return s.bindingReadback(b)
}

// BindUserExternalIdentity is a platform-admin owner command. It can assign a
// destination only to the mapping already established by successful login.
func (s *Service) BindUserExternalIdentity(ctx context.Context, r *api.UserExternalBindingCommand) (*api.UserExternalBindingReadback, error) {
	if err := peer(ctx, owneridentity.ConsoleBFF); err != nil {
		return nil, err
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	session, _, admin, err := s.session(ctx, r.GetContext().GetSessionId())
	if err != nil {
		return nil, err
	}
	if !admin || r.GetContext().GetActorId() != session.ActorId || r.GetContext().GetScope().GetPlatform() == nil {
		return nil, denied()
	}
	if r.GetActorId() == "" || r.GetSub2ApiUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "actor and Sub2API User identities are required")
	}
	if r.GetRegistryHost() != s.registryHost || r.GetRegistryNamespace() != s.registryNamespace {
		return nil, status.Error(codes.InvalidArgument, "destination must use the installation registry host and namespace")
	}
	if _, err := s.userBinding(ctx, r.GetActorId()); err != nil {
		return nil, err
	}
	if err := s.checkHistoricalDestination(ctx, r.GetActorId(), r.GetRegistryHost(), r.GetRegistryNamespace(), r.GetRepository()); err != nil {
		return nil, err
	}
	b, err := s.GatewayStore.BindUserExternalDestination(ctx, r.GetActorId(), r.GetSub2ApiUserId(), r.GetRegistryHost(), r.GetRegistryNamespace(), r.GetRepository())
	if err != nil {
		return nil, err
	}
	return s.bindingReadback(b)
}
