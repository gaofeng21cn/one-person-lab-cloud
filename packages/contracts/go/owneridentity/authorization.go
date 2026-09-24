package owneridentity

import (
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
)

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
