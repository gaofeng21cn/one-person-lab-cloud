package owneridentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

// SessionReference is a non-bearer identifier for typed owner coordination.
// Only TenantProductService accepts the original browser credential; owner
// persistence and authorization contexts carry this reference, never the cookie.
func SessionReference(cookie string) string {
	h := sha256.Sum256([]byte(cookie))
	return "session_" + hex.EncodeToString(h[:])
}

// SessionCookieHeader is private gRPC response metadata between CloudIdentity
// and the BFF. The BFF delivers it only as an HttpOnly cookie, never in JSON.
const SessionCookieHeader = "x-opl-session-cookie"

// OutboundInterceptor presents the calling owner's identity and token on every
// outbound call. The target verifies the token against its own allowlist.
func OutboundInterceptor(owner interface{ String() string }, token string) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, request, reply any, conn *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		md, _ := metadata.FromOutgoingContext(ctx)
		md = md.Copy()
		md.Set(PeerHeader, owner.String())
		md.Set(TokenHeader, token)
		outgoing := metadata.NewOutgoingContext(ctx, md)
		return invoker(outgoing, method, request, reply, conn, opts...)
	}
}

// ValidateDecision binds an uncached CloudIdentity response to the exact typed
// authorization request. An ALLOWED flag alone is never authority.
func ValidateDecision(request *api.AuthorizationRequest, decision *api.AuthorizationDecision, now time.Time) error {
	if request == nil || decision == nil || decision.GetIssuer() != api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY || decision.GetResult() != api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED {
		return fmt.Errorf("CloudIdentity did not allow the request")
	}
	if strings.TrimSpace(request.GetActorId()) == "" || request.GetActorId() != decision.GetActorId() || !proto.Equal(request.GetScope(), decision.GetScope()) || request.GetAudienceOwner() != decision.GetAudienceOwner() || request.GetAction() != decision.GetAction() || !proto.Equal(request.GetResource(), decision.GetResource()) || request.GetSessionId() != decision.GetSessionId() || request.GetAcceptedOperationGrantId() != decision.GetAcceptedOperationGrantId() {
		return fmt.Errorf("CloudIdentity decision does not match actor, scope, session, grant, audience, action and resource")
	}
	if decision.GetPermissionVersion() <= 0 || decision.GetIssuedAt() == nil || decision.GetExpiresAt() == nil || decision.GetIssuedAt().CheckValid() != nil || decision.GetExpiresAt().CheckValid() != nil || decision.GetIssuedAt().AsTime().After(now) || !decision.GetExpiresAt().AsTime().After(now) || !decision.GetExpiresAt().AsTime().After(decision.GetIssuedAt().AsTime()) {
		return fmt.Errorf("CloudIdentity decision has invalid permission version or lifetime")
	}
	return nil
}
