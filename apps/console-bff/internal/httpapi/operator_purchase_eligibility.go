package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
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
	purchaseEligibilityMaxBodyBytes   = 4096
	purchaseEligibilityMaxReasonRunes = 200
)

type purchaseEligibilityInput struct {
	ConfirmationAccountID *string `json:"confirmationAccountId"`
	Enabled               *bool   `json:"enabled"`
	Reason                *string `json:"reason"`
}

func (s *Server) registerOperatorPurchaseEligibilityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v2/operator/accounts/{actorId}/workspace-purchase-eligibility", s.operatorPurchaseEligibilityHandler)
}

// operatorPurchaseEligibilityHandler publishes the canonical CloudIdentity
// workspace-purchase-eligibility command for platform operators. The BFF owns
// no policy state; it checks the live session, CSRF and idempotency guards,
// authorizes the action at platform scope and forwards the validated command.
func (s *Server) operatorPurchaseEligibilityHandler(w http.ResponseWriter, r *http.Request) {
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
	input, ok := decodePurchaseEligibilityInput(w, r, actorID)
	if !ok {
		return
	}
	if s.tenant == nil {
		writePublisherError(w, r, http.StatusServiceUnavailable, "owner_unconfigured", "tenant owner is not configured")
		return
	}
	requestID := r.Header.Get(requestIDHeader)
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETWORKSPACEPURCHASEELIGIBILITY,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, Id: proto.String(actorID)},
		requestID); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	call := clients.CallContext(ctx)
	resp, err := s.tenant.SetWorkspacePurchaseEligibility(ctx, &api.SetWorkspacePurchaseEligibilityRpcRequest{
		Context:       call,
		TargetActorId: actorID,
		Body: &api.SetWorkspacePurchaseEligibilityRequest{
			ConfirmationAccountId: actorID,
			Enabled:               *input.Enabled,
			Reason:                strings.TrimSpace(*input.Reason),
		},
	})
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"operationId":              resp.GetOperationId(),
		"accountId":                resp.GetAccountId(),
		"status":                   resp.GetStatus(),
		"workspacePurchaseEnabled": resp.GetWorkspacePurchaseEnabled(),
	})
}

func decodePurchaseEligibilityInput(w http.ResponseWriter, r *http.Request, actorID string) (purchaseEligibilityInput, bool) {
	if r.URL.RawQuery != "" {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "query parameters are not accepted")
		return purchaseEligibilityInput{}, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, purchaseEligibilityMaxBodyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > purchaseEligibilityMaxBodyBytes {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "invalid request body")
		return purchaseEligibilityInput{}, false
	}
	var input purchaseEligibilityInput
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "invalid request body")
		return purchaseEligibilityInput{}, false
	}
	if input.ConfirmationAccountID == nil || strings.TrimSpace(*input.ConfirmationAccountID) != actorID || input.Enabled == nil || input.Reason == nil {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "confirmationAccountId, enabled and reason are required")
		return purchaseEligibilityInput{}, false
	}
	reason := strings.TrimSpace(*input.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > purchaseEligibilityMaxReasonRunes {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "reason must be between 1 and 200 characters")
		return purchaseEligibilityInput{}, false
	}
	return input, true
}

// projectOperatorAccountEligibility overlays canonical workspacePurchaseEnabled
// facts from CloudIdentity onto an operator accounts page so Console readback
// reflects the single authoritative policy owner.
func (s *Server) projectOperatorAccountEligibility(ctx context.Context, caller Caller, requestID string, raw []byte) ([]byte, error) {
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		return raw, nil
	}
	rawItems, ok := data["items"].([]any)
	if !ok || len(rawItems) == 0 {
		return raw, nil
	}
	actorIDs := make([]string, 0, len(rawItems))
	itemActorIDs := make([]string, len(rawItems))
	for i, entry := range rawItems {
		item, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		actorID := canonicalAccountActorID(item["sub2apiUserId"])
		if actorID == "" {
			continue
		}
		itemActorIDs[i] = actorID
		actorIDs = append(actorIDs, actorID)
	}
	if len(actorIDs) == 0 {
		return raw, nil
	}
	if s.tenant == nil {
		return nil, fmt.Errorf("tenant owner is not configured")
	}
	listCall := activationCall(caller, requestID, platformScope(), "")
	listCtx := clients.WithCallContext(ctx, listCall)
	if err := RequireAuthorizedAction(listCtx, s.identity, caller, owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTTENANTS,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_TENANT},
		requestID); err != nil {
		return nil, err
	}
	resp, err := s.tenant.ListWorkspacePurchaseEligibility(listCtx, &api.ListWorkspacePurchaseEligibilityRpcRequest{
		Context:  listCall,
		ActorIds: actorIDs,
	})
	if err != nil {
		return nil, err
	}
	serviceEntitlementsByActor, err := s.projectOperatorServiceEntitlements(listCtx, listCall, actorIDs)
	if err != nil {
		return nil, err
	}
	byActor := make(map[string]bool, len(resp.GetItems()))
	for _, entry := range resp.GetItems() {
		byActor[entry.GetActorId()] = entry.GetWorkspacePurchaseEnabled()
	}
	for i, actorID := range itemActorIDs {
		if actorID == "" {
			continue
		}
		enabled, found := byActor[actorID]
		if !found {
			return nil, fmt.Errorf("missing purchase eligibility projection for actor %s", actorID)
		}
		item := rawItems[i].(map[string]any)
		item["workspacePurchaseEnabled"] = enabled
		entries := serviceEntitlementsByActor[actorID]
		if entries == nil {
			entries = []map[string]any{}
		}
		item["serviceEntitlements"] = entries
	}
	return json.Marshal(envelope)
}

func canonicalAccountActorID(value any) string {
	switch v := value.(type) {
	case string:
		trimmed := strings.TrimSpace(v)
		if n, err := strconv.ParseInt(trimmed, 10, 64); err == nil && n > 0 {
			return strconv.FormatInt(n, 10)
		}
		return ""
	case float64:
		n := int64(v)
		if float64(n) == v && n > 0 {
			return strconv.FormatInt(n, 10)
		}
		return ""
	default:
		return ""
	}
}
