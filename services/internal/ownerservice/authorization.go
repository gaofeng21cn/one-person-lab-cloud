package ownerservice

import (
	"context"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// ResourceScope comes from the receiving owner's persisted resource, never a
// caller's claim. Empty TenantID denotes a platform resource; ActorID, when set,
// restricts access to the recorded actor as well.
type ResourceScope struct{ TenantID, ActorID string }

// Authorizer is a thin adapter to the existing CloudIdentity typed authority.
// It stores no decisions or policy and fails closed without that dependency.
type Authorizer struct {
	owner  Owner
	client api.CloudIdentityAuthorizationClient
}

func NewAuthorizer(owner Owner, client api.CloudIdentityAuthorizationClient) *Authorizer {
	return &Authorizer{owner: owner, client: client}
}
func AuthorizerFromConfig(config Config) (*Authorizer, *grpc.ClientConn, error) {
	if config.CloudIdentityAddr == "" {
		return NewAuthorizer(config.Owner, nil), nil, nil
	}
	options, err := config.TLS.DialOptions(config.Owner.Service(), owneridentity.Service(owneridentity.Tenant), config.CloudIdentityToken)
	if err != nil {
		return nil, nil, err
	}
	conn, err := grpc.NewClient(config.CloudIdentityAddr, options...)
	if err != nil {
		return nil, nil, err
	}
	return NewAuthorizer(config.Owner, api.NewCloudIdentityAuthorizationClient(conn)), conn, nil
}

func (a *Authorizer) Authorize(ctx context.Context, call *api.CallContext, action api.AuthorizationActionEnum, resource *api.AuthorizationResource, scope ResourceScope) error {
	if err := ValidateCallContext(ctx, call); err != nil {
		return err
	}
	if scope.TenantID != "" {
		if call.GetScope().GetTenant().GetTenantId() != scope.TenantID {
			return status.Error(codes.PermissionDenied, "resource belongs to another tenant")
		}
	} else if call.GetScope().GetPlatform() == nil {
		return status.Error(codes.PermissionDenied, "resource requires platform scope")
	}
	if scope.ActorID != "" && scope.ActorID != call.GetActorId() {
		return status.Error(codes.PermissionDenied, "resource belongs to another actor")
	}
	if a == nil || a.client == nil || !a.owner.Valid() {
		return status.Error(codes.Unavailable, "CloudIdentity authorization is not configured")
	}
	audience := api.OwnerEnum(api.OwnerEnum_value["OWNER_ENUM_"+strings.ToUpper(a.owner.String())])
	if action == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UNSPECIFIED || resource == nil || resource.GetKind() == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_UNSPECIFIED {
		return status.Error(codes.InvalidArgument, "authorization action and resource are required")
	}
	request := &api.AuthorizationRequest{Scope: call.GetScope(), ActorId: call.GetActorId(), SessionId: call.SessionId, AcceptedOperationGrantId: call.AcceptedOperationGrantId, AudienceOwner: audience, Action: action, Resource: resource, RequestId: call.GetRequestId()}
	if call.GetAuthorizationContextId() != "" {
		value := call.GetAuthorizationContextId()
		request.AuthorizationContextId = &value
	}
	decision, err := a.client.AuthorizeAction(ctx, request)
	if err != nil {
		return authorizationError(err)
	}
	if err := owneridentity.ValidateDecision(request, decision, time.Now()); err != nil {
		return status.Error(codes.PermissionDenied, err.Error())
	}
	return nil
}

// authorizationError preserves the distinction between a policy refusal and an
// unavailable authority. CloudIdentity denies with PermissionDenied,
// Unauthenticated or FailedPrecondition; those are decisions and must reach the
// caller as themselves, or an owner cannot tell a legitimate refusal from an
// outage. A transport or availability failure is reported as unavailable, and an
// unrecognised status is never turned into an allow: an owner boundary that cannot
// explain a failure refuses the call.
func authorizationError(err error) error {
	switch status.Code(err) {
	case codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition:
		return err
	case codes.InvalidArgument, codes.NotFound, codes.AlreadyExists, codes.Aborted, codes.OutOfRange, codes.ResourceExhausted:
		// A malformed or refused authorization request is a caller error, not an
		// authority outage; passing it through keeps the cause visible.
		return err
	default:
		return status.Error(codes.Unavailable, "CloudIdentity authorization is unavailable")
	}
}

// ValidateCallContext rejects missing caller context before a resource is read.
func ValidateCallContext(ctx context.Context, call *api.CallContext) error {
	if _, ok := PeerOwner(ctx); !ok {
		return status.Error(codes.Unauthenticated, "verified calling service is required")
	}
	if call == nil || strings.TrimSpace(call.GetActorId()) == "" || strings.TrimSpace(call.GetRequestId()) == "" || call.GetScope() == nil || (call.GetSessionId() == "" && call.GetAcceptedOperationGrantId() == "") {
		return status.Error(codes.Unauthenticated, "actor, request, scope and session or accepted grant are required")
	}
	if call.GetDeadlineAt() != nil && (call.GetDeadlineAt().CheckValid() != nil || !call.GetDeadlineAt().AsTime().After(time.Now())) {
		return status.Error(codes.DeadlineExceeded, "call context expired")
	}
	return nil
}
