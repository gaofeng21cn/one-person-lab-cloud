package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const (
	serviceEntitlementMaxBodyBytes   = 8192
	serviceEntitlementMaxReasonRunes = 200
)

var operatorServiceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{1,63}$`)

type operatorServiceEntitlementInput struct {
	ConfirmationAccountID *string        `json:"confirmationAccountId"`
	Enabled               *bool          `json:"enabled"`
	Role                  *string        `json:"role"`
	OriginOverride        *string        `json:"originOverride"`
	ClearOriginOverride   *bool          `json:"clearOriginOverride"`
	Limits                map[string]any `json:"limits"`
	ExpiresAt             *string        `json:"expiresAt"`
	ClearExpiresAt        *bool          `json:"clearExpiresAt"`
	Reason                *string        `json:"reason"`
}

func (s *Server) registerOperatorServiceEntitlementRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v2/operator/accounts/{actorId}/service-entitlements/{serviceId}", s.operatorServiceEntitlementHandler)
}

func (s *Server) operatorServiceEntitlementHandler(w http.ResponseWriter, r *http.Request) {
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
	serviceID := strings.TrimSpace(r.PathValue("serviceId"))
	if !operatorServiceIDPattern.MatchString(serviceID) || serviceID != r.PathValue("serviceId") {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "a valid service id is required")
		return
	}
	input, expiresAtPB, limitsPB, ok := decodeServiceEntitlementInput(w, r, actorID)
	if !ok {
		return
	}
	if s.tenant == nil {
		writePublisherError(w, r, http.StatusServiceUnavailable, "owner_unconfigured", "tenant owner is not configured")
		return
	}
	requestID := r.Header.Get(requestIDHeader)
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETSERVICEENTITLEMENT,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM, Id: proto.String(actorID)},
		requestID); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	body := &api.SetServiceEntitlementRequest{
		ConfirmationAccountId: actorID,
		Enabled:               *input.Enabled,
		ClearOriginOverride:   input.ClearOriginOverride != nil && *input.ClearOriginOverride,
		ClearExpiresAt:        input.ClearExpiresAt != nil && *input.ClearExpiresAt,
		Limits:                limitsPB,
		ExpiresAt:             expiresAtPB,
		Reason:                strings.TrimSpace(*input.Reason),
	}
	if input.Role != nil {
		trimmedRole := strings.TrimSpace(*input.Role)
		body.Role = &trimmedRole
	}
	if input.OriginOverride != nil {
		trimmedOrigin := strings.TrimSpace(*input.OriginOverride)
		body.OriginOverride = &trimmedOrigin
	}
	resp, err := s.tenant.SetServiceEntitlement(ctx, &api.SetServiceEntitlementRpcRequest{
		Context:       clients.CallContext(ctx),
		TargetActorId: actorID,
		ServiceId:     serviceID,
		Body:          body,
	})
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	payload := map[string]any{
		"operationId":      resp.GetOperationId(),
		"accountId":        resp.GetAccountId(),
		"serviceId":        resp.GetServiceId(),
		"status":           resp.GetStatus(),
		"enabled":          resp.GetEnabled(),
		"effectiveEnabled": resp.GetEffectiveEnabled(),
		"role":             resp.GetRole(),
	}
	if resp.GetOrigin() != "" {
		payload["origin"] = resp.GetOrigin()
	}
	if resp.OriginOverride != nil && resp.GetOriginOverride() != "" {
		payload["originOverride"] = resp.GetOriginOverride()
	}
	if resp.GetLimits() != nil {
		payload["limits"] = resp.GetLimits().AsMap()
	}
	if resp.GetExpiresAt() != nil {
		payload["expiresAt"] = resp.GetExpiresAt().AsTime().UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, payload)
}

func decodeServiceEntitlementInput(w http.ResponseWriter, r *http.Request, actorID string) (operatorServiceEntitlementInput, *timestamppb.Timestamp, *structpb.Struct, bool) {
	if r.URL.RawQuery != "" {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "query parameters are not accepted")
		return operatorServiceEntitlementInput{}, nil, nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, serviceEntitlementMaxBodyBytes+1))
	if err != nil || len(raw) == 0 || len(raw) > serviceEntitlementMaxBodyBytes {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "invalid request body")
		return operatorServiceEntitlementInput{}, nil, nil, false
	}
	var input operatorServiceEntitlementInput
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "invalid request body")
		return operatorServiceEntitlementInput{}, nil, nil, false
	}
	if input.ConfirmationAccountID == nil || strings.TrimSpace(*input.ConfirmationAccountID) != actorID || input.Enabled == nil || input.Reason == nil {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "confirmationAccountId, enabled and reason are required")
		return operatorServiceEntitlementInput{}, nil, nil, false
	}
	reason := strings.TrimSpace(*input.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > serviceEntitlementMaxReasonRunes {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "reason must be between 1 and 200 characters")
		return operatorServiceEntitlementInput{}, nil, nil, false
	}
	var expiresAtPB *timestamppb.Timestamp
	if input.ExpiresAt != nil {
		parsedTime, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(*input.ExpiresAt))
		if parseErr != nil {
			writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "expiresAt must be an RFC3339 timestamp")
			return operatorServiceEntitlementInput{}, nil, nil, false
		}
		expiresAtPB = timestamppb.New(parsedTime.UTC())
	}
	var limitsPB *structpb.Struct
	if input.Limits != nil {
		var structErr error
		limitsPB, structErr = structpb.NewStruct(input.Limits)
		if structErr != nil {
			writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "limits must be a valid JSON object")
			return operatorServiceEntitlementInput{}, nil, nil, false
		}
	}
	return input, expiresAtPB, limitsPB, true
}

func serializeServiceEntitlementReadback(entry *api.ServiceEntitlementReadback) map[string]any {
	out := map[string]any{
		"serviceId":           entry.GetServiceId(),
		"admissionMode":       entry.GetAdmissionMode(),
		"hasExplicitOverride": entry.GetHasExplicitOverride(),
		"enabled":             entry.GetEnabled(),
		"effectiveEnabled":    entry.GetEffectiveEnabled(),
		"role":                entry.GetRole(),
		"origin":              entry.GetOrigin(),
	}
	if entry.OriginOverride != nil && entry.GetOriginOverride() != "" {
		out["originOverride"] = entry.GetOriginOverride()
	}
	if entry.GetLimits() != nil {
		out["limits"] = entry.GetLimits().AsMap()
	}
	if entry.GetExpiresAt() != nil {
		out["expiresAt"] = entry.GetExpiresAt().AsTime().UTC().Format(time.RFC3339)
	}
	if entry.Reason != nil && entry.GetReason() != "" {
		out["reason"] = entry.GetReason()
	}
	if entry.UpdatedBy != nil && entry.GetUpdatedBy() != "" {
		out["updatedBy"] = entry.GetUpdatedBy()
	}
	if entry.GetUpdatedAt() != nil {
		out["updatedAt"] = entry.GetUpdatedAt().AsTime().UTC().Format(time.RFC3339)
	}
	return out
}

func (s *Server) projectOperatorServiceEntitlements(ctx context.Context, listCall *api.CallContext, actorIDs []string) (map[string][]map[string]any, error) {
	resp, err := s.tenant.ListServiceEntitlements(ctx, &api.ListServiceEntitlementsRpcRequest{
		Context:  listCall,
		ActorIds: actorIDs,
	})
	if err != nil {
		return nil, err
	}
	grouped := make(map[string][]map[string]any, len(actorIDs))
	for _, entry := range resp.GetItems() {
		grouped[entry.GetActorId()] = append(grouped[entry.GetActorId()], serializeServiceEntitlementReadback(entry))
	}
	return grouped, nil
}

// Keep context import used cleanly across package build tags.
var _ = context.Background
