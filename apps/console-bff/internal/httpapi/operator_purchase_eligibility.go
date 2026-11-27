package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const (
	operatorMutationMaxBodyBytes   = 4096
	operatorMutationMaxReasonRunes = 200
)

type operatorToggleInput struct {
	ConfirmationAccountID *string `json:"confirmationAccountId"`
	Enabled               *bool   `json:"enabled"`
	Reason                *string `json:"reason"`
}

func (s *Server) registerOperatorPurchaseEligibilityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v2/operator/accounts/{actorId}/workspace-purchase-eligibility", s.operatorPurchaseEligibilityHandler)
}

func (s *Server) registerOperatorPlatformAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v2/operator/accounts/{actorId}/platform-admin", s.operatorPlatformAdminHandler)
}

// operatorPurchaseEligibilityHandler publishes the canonical CloudIdentity
// workspace-purchase-eligibility command for platform operators.
func (s *Server) operatorPurchaseEligibilityHandler(w http.ResponseWriter, r *http.Request) {
	s.handleOperatorActorToggle(
		w,
		r,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETWORKSPACEPURCHASEELIGIBILITY,
		func(ctx context.Context, call *api.CallContext, actorID string, enabled bool, reason string) (any, error) {
			resp, err := s.tenant.SetWorkspacePurchaseEligibility(ctx, &api.SetWorkspacePurchaseEligibilityRpcRequest{
				Context:       call,
				TargetActorId: actorID,
				Body: &api.SetWorkspacePurchaseEligibilityRequest{
					ConfirmationAccountId: actorID,
					Enabled:               enabled,
					Reason:                reason,
				},
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"operationId":              resp.GetOperationId(),
				"accountId":                resp.GetAccountId(),
				"status":                   resp.GetStatus(),
				"workspacePurchaseEnabled": resp.GetWorkspacePurchaseEnabled(),
			}, nil
		},
	)
}

// operatorPlatformAdminHandler publishes the canonical CloudIdentity delegated
// platform-admin toggle command for platform operators.
func (s *Server) operatorPlatformAdminHandler(w http.ResponseWriter, r *http.Request) {
	s.handleOperatorActorToggle(
		w,
		r,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETPLATFORMADMIN,
		func(ctx context.Context, call *api.CallContext, actorID string, enabled bool, reason string) (any, error) {
			resp, err := s.tenant.SetPlatformAdmin(ctx, &api.SetPlatformAdminRpcRequest{
				Context:       call,
				TargetActorId: actorID,
				Body: &api.SetPlatformAdminRequest{
					ConfirmationAccountId: actorID,
					Enabled:               enabled,
					Reason:                reason,
				},
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"operationId":          resp.GetOperationId(),
				"accountId":            resp.GetAccountId(),
				"status":               resp.GetStatus(),
				"platformAdminEnabled": resp.GetPlatformAdminEnabled(),
				"isBootstrapAdmin":     resp.GetIsBootstrapAdmin(),
			}, nil
		},
	)
}

func (s *Server) handleOperatorActorToggle(
	w http.ResponseWriter,
	r *http.Request,
	action api.AuthorizationActionEnum,
	execute func(context.Context, *api.CallContext, string, bool, string) (any, error),
) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	caller, err := RequireSession(r.Context(), s.identity, r)
	if err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	caller.Session = proto.Clone(caller.Session).(*api.Session)
	caller.Session.TenantId = nil
	ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
	if !requirePublisherWriteGuards(w, r, caller, clients.CallContext(ctx)) {
		return
	}
	actorID := strings.TrimSpace(r.PathValue("actorId"))
	parsedID, parseErr := strconv.ParseInt(actorID, 10, 64)
	if parseErr != nil || parsedID <= 0 || actorID != r.PathValue("actorId") || strconv.FormatInt(parsedID, 10) != actorID {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "a valid target actor id is required")
		return
	}
	input, ok := decodeOperatorToggleInput(w, r, actorID)
	if !ok {
		return
	}
	if s.tenant == nil {
		writePublisherError(w, r, http.StatusServiceUnavailable, "owner_unconfigured", "tenant owner is not configured")
		return
	}
	requestID := r.Header.Get(requestIDHeader)
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Tenant,
		action,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, Id: proto.String(actorID)},
		requestID); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	call := clients.CallContext(ctx)
	payload, err := execute(ctx, call, actorID, *input.Enabled, strings.TrimSpace(*input.Reason))
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, payload)
}

func decodeOperatorToggleInput(w http.ResponseWriter, r *http.Request, actorID string) (operatorToggleInput, bool) {
	if r.URL.RawQuery != "" {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "query parameters are not accepted")
		return operatorToggleInput{}, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, operatorMutationMaxBodyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > operatorMutationMaxBodyBytes {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "invalid request body")
		return operatorToggleInput{}, false
	}
	var input operatorToggleInput
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "invalid request body")
		return operatorToggleInput{}, false
	}
	if input.ConfirmationAccountID == nil || strings.TrimSpace(*input.ConfirmationAccountID) != actorID || input.Enabled == nil || input.Reason == nil {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "confirmationAccountId, enabled and reason are required")
		return operatorToggleInput{}, false
	}
	reason := strings.TrimSpace(*input.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > operatorMutationMaxReasonRunes {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "reason must be between 1 and 200 characters")
		return operatorToggleInput{}, false
	}
	return input, true
}
