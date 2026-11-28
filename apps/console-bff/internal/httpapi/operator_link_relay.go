package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const linkRelayMaxBearerLength = 4096

type linkRelaySeedInput struct {
	Reason *string `json:"reason"`
}

func (s *Server) registerOperatorLinkRelayRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v2/operator/accounts/{actorId}/link-relay-entitlement", s.operatorLinkRelayEntitlementHandler)
	mux.HandleFunc("POST /api/v2/operator/link-relay/seed-active-users", s.operatorLinkRelaySeedHandler)
	mux.HandleFunc("GET /api/v2/link-relay/entitlement", s.callerLinkRelayEntitlementHandler)
}

func (s *Server) operatorLinkRelayEntitlementHandler(w http.ResponseWriter, r *http.Request) {
	s.handleOperatorActorToggle(
		w,
		r,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SETLINKRELAYENTITLEMENT,
		func(ctx context.Context, call *api.CallContext, actorID string, enabled bool, reason string) (any, error) {
			resp, err := s.tenant.SetLinkRelayEntitlement(ctx, &api.SetLinkRelayEntitlementRpcRequest{
				Context:       call,
				TargetActorId: actorID,
				Body: &api.SetLinkRelayEntitlementRequest{
					ConfirmationAccountId: actorID,
					Enabled:               enabled,
					Reason:                reason,
				},
			})
			if err != nil {
				return nil, err
			}
			return map[string]any{
				"operationId":      resp.GetOperationId(),
				"accountId":        resp.GetAccountId(),
				"status":           resp.GetStatus(),
				"linkRelayEnabled": resp.GetLinkRelayEnabled(),
			}, nil
		},
	)
}

func (s *Server) operatorLinkRelaySeedHandler(w http.ResponseWriter, r *http.Request) {
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
	if r.URL.RawQuery != "" {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "query parameters are not accepted")
		return
	}
	raw, readErr := io.ReadAll(io.LimitReader(r.Body, operatorMutationMaxBodyBytes+1))
	if readErr != nil || len(raw) == 0 || len(raw) > operatorMutationMaxBodyBytes {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "invalid request body")
		return
	}
	var input linkRelaySeedInput
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Reason == nil {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "reason is required")
		return
	}
	reason := strings.TrimSpace(*input.Reason)
	if reason == "" || utf8.RuneCountInString(reason) > operatorMutationMaxReasonRunes {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "reason must be between 1 and 200 characters")
		return
	}
	if s.tenant == nil {
		writePublisherError(w, r, http.StatusServiceUnavailable, "owner_unconfigured", "tenant owner is not configured")
		return
	}
	requestID := r.Header.Get(requestIDHeader)
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Tenant,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_SEEDACTIVEGATEWAYLINKRELAYENTITLEMENTS,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PLATFORM},
		requestID); err != nil {
		writePublisherIdentityError(w, r, err)
		return
	}
	resp, err := s.tenant.SeedActiveGatewayLinkRelayEntitlements(ctx, &api.SeedActiveGatewayLinkRelayEntitlementsRpcRequest{
		Context: clients.CallContext(ctx),
		Body:    &api.SeedActiveGatewayLinkRelayEntitlementsRequest{Reason: reason},
	})
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"operationId":   resp.GetOperationId(),
		"status":        resp.GetStatus(),
		"scannedCount":  resp.GetScannedCount(),
		"seededCount":   resp.GetSeededCount(),
		"existingCount": resp.GetExistingCount(),
	})
}

func (s *Server) callerLinkRelayEntitlementHandler(w http.ResponseWriter, r *http.Request) {
	publisherRequestID(r)
	w.Header().Set("Cache-Control", "private, no-store")
	if r.URL.RawQuery != "" {
		writePublisherError(w, r, http.StatusBadRequest, "invalid_request", "query parameters are not accepted")
		return
	}
	hasAuthHeader := len(r.Header.Values("Authorization")) != 0
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" || (r.Header.Get("Origin") != "" && !s.sameOrigin(r)) {
		writePublisherError(w, r, http.StatusForbidden, "origin_rejected", "cross-site request rejected")
		return
	}
	if s.tenant == nil {
		writeOwnerCallFailure(w, r, status.Error(codes.Unavailable, "Tenant owner is not configured"))
		return
	}

	var rpcReq *api.ReadCallerLinkRelayEntitlementRpcRequest
	if hasAuthHeader {
		token, err := singleGatewayBearer(r)
		if err != nil {
			writePublisherIdentityError(w, r, err)
			return
		}
		rpcReq = &api.ReadCallerLinkRelayEntitlementRpcRequest{BearerToken: token}
	} else {
		caller, err := RequireSession(r.Context(), s.identity, r)
		if err != nil {
			writePublisherIdentityError(w, r, err)
			return
		}
		ctx := WithCaller(r.Context(), caller, r.Header.Get(requestIDHeader))
		rpcReq = &api.ReadCallerLinkRelayEntitlementRpcRequest{Context: clients.CallContext(ctx)}
	}

	resp, err := s.tenant.ReadCallerLinkRelayEntitlement(r.Context(), rpcReq)
	if err != nil {
		writeOwnerCallFailure(w, r, err)
		return
	}
	payload := map[string]any{
		"actorId":          resp.GetActorId(),
		"email":            resp.GetEmail(),
		"linkRelayEnabled": resp.GetLinkRelayEnabled(),
		"isBootstrapAdmin": resp.GetIsBootstrapAdmin(),
		"isPlatformAdmin":  resp.GetIsPlatformAdmin(),
		"reason":           resp.GetReason(),
	}
	if resp.GetUpdatedAt() != nil {
		payload["updatedAt"] = resp.GetUpdatedAt().AsTime().UTC().Format(time.RFC3339Nano)
	}
	writeJSON(w, http.StatusOK, payload)
}

func singleGatewayBearer(r *http.Request) (string, error) {
	values := r.Header.Values("Authorization")
	if len(values) != 1 || len(values[0]) > linkRelayMaxBearerLength || r.Header.Get("Cookie") != "" {
		return "", ErrSessionRequired
	}
	parts := strings.Split(values[0], " ")
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" || strings.ContainsAny(parts[1], "\t\r\n,") {
		return "", ErrSessionRequired
	}
	return parts[1], nil
}
