package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http"
	"opl-cloud/apps/console-bff/internal/clients"
	"strings"
	"time"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// SessionCookieName is the browser session cookie the BFF reads. It is the
// CloudIdentity session: the BFF never mints a session, validates a password, or
// stores an identity of its own.
const SessionCookieName = "opl_session"

// Caller is the identified browser caller of one BFF request: the CloudIdentity
// session that authenticated it and the raw session id CloudIdentity can re-check.
type Caller struct {
	Session   *api.Session
	SessionID string
}

// ErrSessionRequired reports that a browser request carried no CloudIdentity
// session. The BFF refuses rather than serving owner facts to an unidentified
// caller: a valid service token only proves this process may call the owner.
var ErrSessionRequired = errors.New("browser session is required")

// ErrAuthorizationRequired reports that CloudIdentity did not allow the requested
// action. Service identity is not user authorization: a configured owner token
// lets the BFF call the owner, and the owner still authorizes the actor.
var ErrAuthorizationRequired = errors.New("CloudIdentity did not authorize this action")

// IdentityReader reads the caller's own CloudIdentity session and authorization
// decision. It is satisfied by the tenant/auth client set and by a test double.
type IdentityReader interface {
	Session(ctx context.Context, sessionID string) (*api.Session, error)
	Authorize(ctx context.Context, request *api.AuthorizationRequest) (*api.AuthorizationDecision, error)
}

// RequireSession reads the browser session cookie and resolves it through
// CloudIdentity. A missing cookie is a client error; an unknown or expired session
// is reported by CloudIdentity and surfaces unchanged.
func RequireSession(ctx context.Context, identity IdentityReader, request *http.Request) (Caller, error) {
	if identity == nil {
		return Caller{}, fmt.Errorf("CloudIdentity is not configured: %w", ErrSessionRequired)
	}
	cookie, err := request.Cookie(SessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return Caller{}, ErrSessionRequired
	}
	sessionID := strings.TrimSpace(cookie.Value)
	session, err := identity.Session(ctx, sessionID)
	if err != nil {
		return Caller{}, fmt.Errorf("read CloudIdentity session: %w", err)
	}
	if strings.TrimSpace(session.GetActorId()) == "" {
		return Caller{}, fmt.Errorf("CloudIdentity session carries no actor: %w", ErrSessionRequired)
	}
	return Caller{Session: session, SessionID: sessionID}, nil
}

// RequireAuthorizedAction asks CloudIdentity whether this caller may perform the
// action on the resource. Only an explicit ALLOWED decision from CloudIdentity
// passes: a missing issuer, an unspecified result, or a denial is refused, and no
// cached or session-derived permission is substituted.
func RequireAuthorizedAction(ctx context.Context, identity IdentityReader, caller Caller, audience owneridentity.Owner, action api.AuthorizationActionEnum, resource *api.AuthorizationResource, requestID string) error {
	if identity == nil {
		return fmt.Errorf("CloudIdentity is not configured: %w", ErrAuthorizationRequired)
	}
	if caller.Session == nil || strings.TrimSpace(caller.Session.GetActorId()) == "" {
		return ErrSessionRequired
	}
	audienceOwner := ownerEnum(audience)
	if audienceOwner == api.OwnerEnum_OWNER_ENUM_UNSPECIFIED {
		return fmt.Errorf("%q is not a Cloud owner: %w", audience, ErrAuthorizationRequired)
	}
	request := &api.AuthorizationRequest{
		Scope:         sessionScope(caller.Session),
		ActorId:       caller.Session.GetActorId(),
		AudienceOwner: audienceOwner,
		Action:        action,
		Resource:      resource,
		RequestId:     requestID,
	}
	if caller.SessionID != "" {
		sessionID := caller.SessionID
		request.SessionId = &sessionID
	}
	decision, err := identity.Authorize(ctx, request)
	if err != nil {
		return fmt.Errorf("read CloudIdentity authorization: %w", err)
	}
	if err := owneridentity.ValidateDecision(request, decision, time.Now()); err != nil {
		return fmt.Errorf("%v: %w", err, ErrAuthorizationRequired)
	}
	return nil
}

// sessionScope is the authorization scope the session itself carries. A session
// with no tenant is platform-scoped; the BFF never invents a tenant id.
func sessionScope(session *api.Session) *api.AuthorizationScope {
	if tenantID := strings.TrimSpace(session.GetTenantId()); tenantID != "" {
		return &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenantID}}}
	}
	return &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
}

func ownerEnum(owner owneridentity.Owner) api.OwnerEnum {
	value, ok := api.OwnerEnum_value["OWNER_ENUM_"+strings.ToUpper(owner.String())]
	if !ok {
		return api.OwnerEnum_OWNER_ENUM_UNSPECIFIED
	}
	return api.OwnerEnum(value)
}

// WithCaller forwards only the session identity resolved by CloudIdentity. Browser
// actor and tenant headers never enter this context.
func WithCaller(ctx context.Context, caller Caller, requestID string) context.Context {
	if strings.TrimSpace(requestID) == "" {
		var bytes [16]byte
		_, _ = rand.Read(bytes[:])
		requestID = hex.EncodeToString(bytes[:])
	}
	deadline := time.Now().Add(30 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	sessionID := caller.SessionID
	return clients.WithCallContext(ctx, &api.CallContext{ActorId: caller.Session.GetActorId(), SessionId: &sessionID, Scope: sessionScope(caller.Session), RequestId: requestID, DeadlineAt: timestamppb.New(deadline)})
}
