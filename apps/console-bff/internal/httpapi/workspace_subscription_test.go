package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type subscriptionProbe struct {
	api.WorkspaceProductServiceClient
	read         *api.GetSubscriptionRpcRequest
	subscription *api.Subscription
	err          error
}

func (p *subscriptionProbe) GetSubscription(_ context.Context, request *api.GetSubscriptionRpcRequest, _ ...grpc.CallOption) (*api.Subscription, error) {
	p.read = request
	return p.subscription, p.err
}

func subscriptionResponse() *api.Subscription {
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	return &api.Subscription{
		Id: "subscription-1", WorkspaceId: "ws-1", CurrentPeriodStart: timestamppb.New(start), CurrentPeriodEnd: timestamppb.New(start.AddDate(0, 1, 0)),
		PeriodMonths: 1, Status: api.SubscriptionStatusEnum_SUBSCRIPTION_STATUS_ENUM_ACTIVE, CreatedAt: timestamppb.New(start),
		Provenance: api.SubscriptionProvenanceEnum_SUBSCRIPTION_PROVENANCE_ENUM_QUOTED, AcceptedQuoteId: proto.String("original-quote"),
		RefundPolicyVersionId: proto.String("original-refund"), RetentionPolicyVersionId: proto.String("original-retention"),
		RefundTerms: proto.String("Original refund terms"), RetentionTerms: proto.String("Original retention terms"),
		RenewalMode: api.SubscriptionRenewalModeEnum_SUBSCRIPTION_RENEWAL_MODE_ENUM_AUTOMATIC, RenewalConsentId: proto.String("original-consent"),
		RenewalOperationId: proto.String("original-renewal"), Version: 7, RenewalSettingsVersion: 3,
		CurrentPricePolicyVersionId: proto.String("accepted-price"), CurrentMonthlyUsdMicros: proto.Int64(9007199254740993),
	}
}

func TestWorkspaceSubscriptionRouteForwardsOnlyAuthenticatedContext(t *testing.T) {
	probe := &subscriptionProbe{subscription: subscriptionResponse()}
	identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION, "ws-1")
	// Use the process constructor to prove the route is actually registered.
	handler := NewServer(workspaceReader{fakeReader: resolvedReader(), client: probe}, identity).Handler()
	request := sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1/subscription")
	request.Header.Set("X-OPL-Actor", "forged-actor")
	request.Header.Set("X-OPL-Tenant", "foreign-tenant")
	request.Header.Set("X-OPL-Request-ID", "subscription-wire-request")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %s", response.Code, response.Body.String())
	}
	if probe.read.GetWorkspaceId() != "ws-1" {
		t.Fatalf("owner request: %v", probe.read)
	}
	call := probe.read.GetContext()
	if call.GetActorId() != "actor-1" || call.GetScope().GetTenant().GetTenantId() != "tenant-1" || call.GetSessionId() != owneridentity.SessionReference("session-1") ||
		call.GetRequestId() != "subscription-wire-request" || call.GetAuthorizationContextId() != "workspace-authorization" || call.GetIdempotencyKey() != "" || call.GetDeadlineAt() == nil {
		t.Fatalf("authenticated context changed: %v", call)
	}
	if len(identity.requests) != 1 || identity.requests[0].GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION ||
		identity.requests[0].GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE || identity.requests[0].GetResource().GetId() != "ws-1" {
		t.Fatalf("authorization: %v", identity.requests)
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	for key, expected := range map[string]any{
		"id": "subscription-1", "workspaceId": "ws-1", "currentMonthlyUSDMicros": "9007199254740993", "currentPricePolicyVersionId": "accepted-price",
		"provenance": "quoted", "acceptedQuoteId": "original-quote", "refundPolicyVersionId": "original-refund", "retentionPolicyVersionId": "original-retention",
		"refundTerms": "Original refund terms", "retentionTerms": "Original retention terms", "renewalMode": "automatic", "renewalConsentId": "original-consent",
		"renewalOperationId": "original-renewal", "version": "7", "renewalSettingsVersion": "3", "currentPeriodStart": "2026-10-01T00:00:00Z", "currentPeriodEnd": "2026-11-01T00:00:00Z",
	} {
		if body[key] != expected {
			t.Fatalf("%s = %v, want %v", key, body[key], expected)
		}
	}
}

func TestWorkspaceSubscriptionRoutePreservesLegacyAbsenceAndExplicitZero(t *testing.T) {
	for _, explicitZero := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown price", true: "accepted zero"}[explicitZero], func(t *testing.T) {
			subscription := subscriptionResponse()
			subscription.Provenance = api.SubscriptionProvenanceEnum_SUBSCRIPTION_PROVENANCE_ENUM_LEGACY_IMPORT
			subscription.LegacyPurchaseId = proto.String("legacy-purchase")
			subscription.AcceptedQuoteId, subscription.RefundPolicyVersionId, subscription.RetentionPolicyVersionId = nil, nil, nil
			subscription.RefundTerms, subscription.RetentionTerms = nil, nil
			subscription.CurrentMonthlyUsdMicros, subscription.CurrentPricePolicyVersionId = nil, nil
			if explicitZero {
				subscription.CurrentMonthlyUsdMicros, subscription.CurrentPricePolicyVersionId = proto.Int64(0), proto.String("legacy-accepted-price")
			}
			probe := &subscriptionProbe{subscription: subscription}
			identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION, "ws-1")
			response := httptest.NewRecorder()
			NewWorkspaceHandler(probe, identity).ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1/subscription"))
			if response.Code != http.StatusOK {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body["provenance"] != "legacy_import" || body["legacyPurchaseId"] != "legacy-purchase" {
				t.Fatalf("legacy identity: %v", body)
			}
			for _, key := range []string{"acceptedQuoteId", "refundPolicyVersionId", "retentionPolicyVersionId", "refundTerms", "retentionTerms"} {
				if _, present := body[key]; present {
					t.Fatalf("invented %s: %v", key, body)
				}
			}
			if explicitZero {
				if body["currentMonthlyUSDMicros"] != "0" || body["currentPricePolicyVersionId"] != "legacy-accepted-price" {
					t.Fatalf("accepted zero lost: %v", body)
				}
			} else {
				if _, present := body["currentMonthlyUSDMicros"]; present {
					t.Fatalf("missing price changed to zero: %v", body)
				}
				if _, present := body["currentPricePolicyVersionId"]; present {
					t.Fatalf("missing policy invented: %v", body)
				}
			}
		})
	}
}

func TestWorkspaceSubscriptionRouteRejectsUnauthorizedBeforeOwner(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request, *fakeIdentity)
		want   int
	}{
		{"no session", func(r *http.Request, _ *fakeIdentity) { r.Header.Del("Cookie") }, 401},
		{"member denied", func(_ *http.Request, i *fakeIdentity) {
			i.session.Role = api.TenantRoleEnum_TENANT_ROLE_ENUM_MEMBER.Enum()
			i.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
		}, 403},
		{"foreign decision tenant", func(_ *http.Request, i *fakeIdentity) { i.decision.Scope.GetTenant().TenantId = "foreign" }, 403},
		{"foreign decision resource", func(_ *http.Request, i *fakeIdentity) { i.decision.Resource.Id = proto.String("foreign") }, 403},
		{"workspace read cannot authorize subscription", func(_ *http.Request, i *fakeIdentity) {
			i.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE
		}, 403},
		{"foreign decision audience", func(_ *http.Request, i *fakeIdentity) {
			i.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_RESOURCE_CATALOG
		}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &subscriptionProbe{subscription: subscriptionResponse()}
			identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION, "ws-1")
			request := sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1/subscription")
			tc.mutate(request, identity)
			response := httptest.NewRecorder()
			NewWorkspaceHandler(probe, identity).ServeHTTP(response, request)
			if response.Code != tc.want || probe.read != nil || strings.Contains(response.Body.String(), "accepted-price") {
				t.Fatalf("%d owner=%v %s", response.Code, probe.read, response.Body.String())
			}
		})
	}
}

func TestWorkspaceSubscriptionRouteDistinguishesMissingAndUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name string
		code codes.Code
		want int
	}{
		{"subscription absent", codes.NotFound, 404}, {"owner denies member", codes.PermissionDenied, 403},
		{"database unavailable", codes.Unavailable, 503}, {"deadline", codes.DeadlineExceeded, 503}, {"invalid stored evidence", codes.DataLoss, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &subscriptionProbe{err: status.Error(tc.code, tc.name)}
			identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION, "ws-1")
			response := httptest.NewRecorder()
			NewWorkspaceHandler(probe, identity).ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1/subscription"))
			if response.Code != tc.want || probe.read == nil || strings.TrimSpace(response.Body.String()) == "null" {
				t.Fatalf("%d %s", response.Code, response.Body.String())
			}
		})
	}
	identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION, "ws-1")
	response := httptest.NewRecorder()
	NewWorkspaceHandler(nil, identity).ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1/subscription"))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconfigured owner: %d %s", response.Code, response.Body.String())
	}
}
