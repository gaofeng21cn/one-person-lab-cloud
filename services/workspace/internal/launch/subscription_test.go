package launch

import (
	"context"
	"database/sql"
	"encoding/json"
	"net"
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
	bff "opl-cloud/apps/console-bff"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// subscriptionIdentity supplies role decisions at the actual authorizer seam.
// Both BFF and owner still validate the complete decision, action and resource.
type subscriptionIdentity struct {
	api.CloudIdentityAuthorizationClient
	role           api.TenantRoleEnum
	tenant         string
	requests       []*api.AuthorizationRequest
	afterAuthorize func()
	failure        error
}

func (i *subscriptionIdentity) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	i.requests = append(i.requests, proto.Clone(r).(*api.AuthorizationRequest))
	if i.afterAuthorize != nil {
		i.afterAuthorize()
	}
	if i.failure != nil {
		return nil, i.failure
	}
	result := api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	if (i.role == api.TenantRoleEnum_TENANT_ROLE_ENUM_OWNER || i.role == api.TenantRoleEnum_TENANT_ROLE_ENUM_ADMIN) &&
		r.GetAudienceOwner() == api.OwnerEnum_OWNER_ENUM_WORKSPACE && r.GetAction() == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION &&
		r.GetResource().GetKind() == api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE && r.GetResource().GetId() == "workspace-subscription" {
		result = api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED
	}
	authorization := r.GetAuthorizationContextId()
	if authorization == "" {
		authorization = "subscription-authorization"
	}
	return &api.AuthorizationDecision{
		Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: result,
		ActorId: r.GetActorId(), Scope: r.GetScope(), SessionId: r.SessionId,
		AudienceOwner: r.GetAudienceOwner(), Action: r.GetAction(), Resource: r.GetResource(),
		AuthorizationContextId: proto.String(authorization), PermissionVersion: 1,
		IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}, nil
}

func (i *subscriptionIdentity) Session(context.Context, string) (*api.Session, error) {
	return &api.Session{ActorId: "subscription-actor", TenantId: proto.String(i.tenant), Role: i.role.Enum()}, nil
}
func (i *subscriptionIdentity) Authorize(ctx context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	return i.AuthorizeAction(ctx, r)
}

func subscriptionCall() (*api.CallContext, context.Context) {
	return &api.CallContext{ActorId: "subscription-actor", SessionId: proto.String("subscription-session"), RequestId: "subscription-request",
			Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "subscription-tenant"}}}},
		ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
}

func subscriptionFixture(t *testing.T, db *sql.DB, provenance string) (*Service, *subscriptionIdentity, *api.QuoteAcceptance) {
	t.Helper()
	start := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	end := start.AddDate(0, 1, 0)
	quoted := &api.QuoteAcceptance{WorkspaceId: "workspace-subscription", Quote: &api.Quote{
		Id: "quote-original", PricePolicyVersionId: "price-original", TotalUsdMicros: 720000000,
		RefundPolicyVersionId: "refund-original", RetentionPolicyVersionId: "retention-original",
		RefundTerms: "Original refund terms", RetentionTerms: "Original retention terms",
		PeriodStart: timestamppb.New(start), PeriodEnd: timestamppb.New(end), PeriodMonths: 1,
	}}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO workspace.workspaces
        (id,tenant_id,name,status,compute_plan_id,storage_plan_id,created_by,version)
        VALUES('workspace-subscription','subscription-tenant','Subscription workspace','active','compute-original','storage-original','subscription-actor',1)`); err != nil {
		t.Fatal(err)
	}
	var quoteID, quotedRaw, legacyID, legacyRaw, price, monthly any
	if provenance == "quoted" {
		quoteID, quotedRaw, price, monthly = "quote-original", wire(quoted), "price-current-accepted", int64(9007199254740993)
	} else {
		legacyID, legacyRaw = "legacy-purchase-original", []byte(`{"evidenceGap":"original policies unavailable"}`)
	}
	if _, err := db.ExecContext(t.Context(), `INSERT INTO workspace.subscriptions
        (id,workspace_id,billing_subject_ref,current_period_start,current_period_end,period_months,created_at,
         provenance,renewal_mode,version,renewal_settings_version,accepted_quote_id,accepted_quote_snapshot,
         legacy_purchase_id,legacy_obligation_snapshot,current_price_policy_version_id,current_monthly_usd_micros,billing_anchor_day)
        VALUES('subscription-original','workspace-subscription','tenant:subscription-tenant',$1,$2,1,$1,$3,'manual',7,3,$4,$5,$6,$7,$8,$9,1)`,
		start, end, provenance, quoteID, quotedRaw, legacyID, legacyRaw, price, monthly); err != nil {
		t.Fatal(err)
	}
	store, err := ownerstore.New(db, "workspace")
	if err != nil {
		t.Fatal(err)
	}
	identity := &subscriptionIdentity{role: api.TenantRoleEnum_TENANT_ROLE_ENUM_OWNER, tenant: "subscription-tenant"}
	service := &Service{Store: store, Auth: ownerservice.NewAuthorizer(owneridentity.Workspace, identity),
		accessNow: func() time.Time { return start.Add(time.Hour) }}
	return service, identity, quoted
}

func readSubscription(t *testing.T, service *Service) *api.Subscription {
	t.Helper()
	call, ctx := subscriptionCall()
	out, err := service.GetSubscription(ctx, &api.GetSubscriptionRpcRequest{Context: call, WorkspaceId: "workspace-subscription"})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGetSubscriptionPreservesAcceptedFinancialFacts(t *testing.T) {
	db := runtimeDatabase(t)
	service, identity, original := subscriptionFixture(t, db, "quoted")
	// A scheduled change may own the active operation without changing current
	// accepted pricing, policies or the period. Automatic consent also survives.
	if _, err := db.ExecContext(t.Context(), `INSERT INTO workspace.operations
        (id,tenant_id,actor_id,kind,resource_id,stage,request_id,accepted_input)
        VALUES('renewal-original','subscription-tenant','subscription-actor','renew_workspace','workspace-subscription','quote_binding','renewal-request','{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `UPDATE workspace.subscriptions SET active_change_operation_id='renewal-original',
        renewal_mode='automatic',renewal_consent_id='consent-original',renewal_consent_snapshot='{}'`); err != nil {
		t.Fatal(err)
	}
	out := readSubscription(t, service)
	if out.GetId() != "subscription-original" || out.GetWorkspaceId() != "workspace-subscription" ||
		out.GetCurrentPricePolicyVersionId() != "price-current-accepted" || out.GetCurrentMonthlyUsdMicros() != 9007199254740993 ||
		out.GetAcceptedQuoteId() != "quote-original" || out.GetProvenance() != api.SubscriptionProvenanceEnum_SUBSCRIPTION_PROVENANCE_ENUM_QUOTED ||
		out.GetRefundPolicyVersionId() != original.GetQuote().GetRefundPolicyVersionId() || out.GetRetentionPolicyVersionId() != "retention-original" ||
		out.GetRefundTerms() != "Original refund terms" || out.GetRetentionTerms() != "Original retention terms" ||
		out.GetVersion() != 7 || out.GetRenewalSettingsVersion() != 3 || out.GetPeriodMonths() != 1 ||
		out.GetRenewalMode() != api.SubscriptionRenewalModeEnum_SUBSCRIPTION_RENEWAL_MODE_ENUM_AUTOMATIC || out.GetRenewalConsentId() != "consent-original" ||
		out.GetRenewalOperationId() != "renewal-original" || out.LegacyPurchaseId != nil ||
		!proto.Equal(out.GetCurrentPeriodStart(), original.GetQuote().GetPeriodStart()) || !proto.Equal(out.GetCurrentPeriodEnd(), original.GetQuote().GetPeriodEnd()) ||
		!proto.Equal(out.GetCreatedAt(), original.GetQuote().GetPeriodStart()) {
		t.Fatalf("accepted facts changed: %v", out)
	}
	if len(identity.requests) != 1 || identity.requests[0].GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETSUBSCRIPTION ||
		identity.requests[0].GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE {
		t.Fatalf("authorization: %v", identity.requests)
	}
	// A database-enforced read-only transaction mode proves this read creates no
	// operation, audit, policy or other persistence effect.
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(t.Context(), `SET default_transaction_read_only=on`); err != nil {
		t.Fatal(err)
	}
	again := readSubscription(t, service)
	if !proto.Equal(out, again) {
		t.Fatalf("read-only replay changed: %v", again)
	}
}

func TestGetSubscriptionLegacyEvidenceRemainsOptional(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, _ := subscriptionFixture(t, db, "legacy_import")
	for _, tc := range []struct {
		name, snapshot string
		price, monthly any
		complete       bool
	}{
		{"missing original evidence", `{"evidenceGap":"no original policy"}`, nil, nil, false},
		{"only price policy", `{}`, "legacy-price", nil, false},
		{"only price amount", `{}`, nil, int64(0), false},
		{"original evidence and explicit zero", `{"refundPolicyVersionId":"legacy-refund","retentionPolicyVersionId":"legacy-retention","refundTerms":"Legacy refund","retentionTerms":"Legacy retention"}`, "legacy-price", int64(0), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.ExecContext(t.Context(), `UPDATE workspace.subscriptions SET legacy_obligation_snapshot=$1,current_price_policy_version_id=$2,current_monthly_usd_micros=$3`, []byte(tc.snapshot), tc.price, tc.monthly); err != nil {
				t.Fatal(err)
			}
			out := readSubscription(t, service)
			if out.GetProvenance() != api.SubscriptionProvenanceEnum_SUBSCRIPTION_PROVENANCE_ENUM_LEGACY_IMPORT || out.GetLegacyPurchaseId() != "legacy-purchase-original" || out.AcceptedQuoteId != nil {
				t.Fatalf("legacy identity: %v", out)
			}
			if tc.complete {
				if out.CurrentMonthlyUsdMicros == nil || *out.CurrentMonthlyUsdMicros != 0 || out.GetCurrentPricePolicyVersionId() != "legacy-price" ||
					out.GetRefundPolicyVersionId() != "legacy-refund" || out.GetRetentionPolicyVersionId() != "legacy-retention" ||
					out.GetRefundTerms() != "Legacy refund" || out.GetRetentionTerms() != "Legacy retention" {
					t.Fatalf("legacy evidence lost: %v", out)
				}
			} else if out.CurrentMonthlyUsdMicros != nil || out.CurrentPricePolicyVersionId != nil || out.RefundPolicyVersionId != nil || out.RetentionPolicyVersionId != nil || out.RefundTerms != nil || out.RetentionTerms != nil {
				t.Fatalf("missing legacy evidence invented: %v", out)
			}
		})
	}
}

func TestGetSubscriptionRejectsInvalidStoredSnapshots(t *testing.T) {
	for _, provenance := range []string{"quoted", "legacy_import"} {
		t.Run(provenance, func(t *testing.T) {
			db := runtimeDatabase(t)
			service, _, quoted := subscriptionFixture(t, db, provenance)
			snapshots := map[string][]byte{"null": []byte(`null`), "array": []byte(`[]`), "wrong policy type": []byte(`{"refundTerms":42}`)}
			column := "legacy_obligation_snapshot"
			if provenance == "quoted" {
				column = "accepted_quote_snapshot"
				missing := proto.Clone(quoted).(*api.QuoteAcceptance)
				missing.Quote.RetentionTerms = ""
				foreignQuote := proto.Clone(quoted).(*api.QuoteAcceptance)
				foreignQuote.Quote.Id = "quote-foreign"
				foreignWorkspace := proto.Clone(quoted).(*api.QuoteAcceptance)
				foreignWorkspace.WorkspaceId = "workspace-foreign"
				snapshots["missing required policy"] = wire(missing)
				snapshots["foreign quote"] = wire(foreignQuote)
				snapshots["foreign workspace"] = wire(foreignWorkspace)
			} else {
				snapshots["empty policy id"] = []byte(`{"refundPolicyVersionId":""}`)
			}
			for name, raw := range snapshots {
				t.Run(name, func(t *testing.T) {
					if _, err := db.ExecContext(t.Context(), `UPDATE workspace.subscriptions SET `+column+`=$1`, raw); err != nil {
						t.Fatal(err)
					}
					call, ctx := subscriptionCall()
					out, err := service.GetSubscription(ctx, &api.GetSubscriptionRpcRequest{Context: call, WorkspaceId: "workspace-subscription"})
					if status.Code(err) != codes.DataLoss || out != nil {
						t.Fatalf("invalid evidence returned %v, %v", out, err)
					}
				})
			}
		})
	}
}

func TestGetSubscriptionDerivesLifecycleAtPeriodBoundary(t *testing.T) {
	db := runtimeDatabase(t)
	service, _, original := subscriptionFixture(t, db, "quoted")
	end := original.GetQuote().GetPeriodEnd().AsTime()
	for _, tc := range []struct {
		name, state string
		now         time.Time
		want        api.SubscriptionStatusEnum
	}{
		{"before expiry", "active", end.Add(-time.Nanosecond), api.SubscriptionStatusEnum_SUBSCRIPTION_STATUS_ENUM_ACTIVE},
		{"expiry boundary", "active", end, api.SubscriptionStatusEnum_SUBSCRIPTION_STATUS_ENUM_EXPIRED},
		{"suspended paid period", "suspended", end.Add(-time.Hour), api.SubscriptionStatusEnum_SUBSCRIPTION_STATUS_ENUM_ACTIVE},
		{"deletion before expiry", "deleted", end.Add(-time.Hour), api.SubscriptionStatusEnum_SUBSCRIPTION_STATUS_ENUM_TERMINATED},
		{"deletion after expiry", "deleted", end.Add(time.Hour), api.SubscriptionStatusEnum_SUBSCRIPTION_STATUS_ENUM_TERMINATED},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := db.ExecContext(t.Context(), `UPDATE workspace.workspaces SET status=$1,deleted_at=CASE WHEN $1='deleted' THEN now() ELSE NULL END`, tc.state); err != nil {
				t.Fatal(err)
			}
			service.accessNow = func() time.Time { return tc.now }
			if out := readSubscription(t, service); out.GetStatus() != tc.want {
				t.Fatalf("status: %v", out)
			}
		})
	}
}

func TestGetSubscriptionAuthorizationAndMissingRows(t *testing.T) {
	db := runtimeDatabase(t)
	service, identity, _ := subscriptionFixture(t, db, "quoted")
	for _, role := range []api.TenantRoleEnum{api.TenantRoleEnum_TENANT_ROLE_ENUM_OWNER, api.TenantRoleEnum_TENANT_ROLE_ENUM_ADMIN} {
		identity.role = role
		readSubscription(t, service)
	}
	call, ctx := subscriptionCall()
	request := &api.GetSubscriptionRpcRequest{Context: call, WorkspaceId: "workspace-subscription"}
	identity.role = api.TenantRoleEnum_TENANT_ROLE_ENUM_MEMBER
	if out, err := service.GetSubscription(ctx, request); status.Code(err) != codes.PermissionDenied || out != nil {
		t.Fatalf("member received %v, %v", out, err)
	}
	identity.role = api.TenantRoleEnum_TENANT_ROLE_ENUM_OWNER
	identity.requests = nil
	call.Scope.GetTenant().TenantId = "foreign-tenant"
	if out, err := service.GetSubscription(ctx, request); status.Code(err) != codes.PermissionDenied || out != nil || len(identity.requests) != 0 {
		t.Fatalf("cross tenant: %v, %v", out, err)
	}
	call.Scope.GetTenant().TenantId = "subscription-tenant"
	request.WorkspaceId = " "
	if _, err := service.GetSubscription(ctx, request); status.Code(err) != codes.InvalidArgument {
		t.Fatal(err)
	}
	request.WorkspaceId = "workspace-missing"
	if _, err := service.GetSubscription(ctx, request); status.Code(err) != codes.NotFound {
		t.Fatal(err)
	}
	request.WorkspaceId = "workspace-subscription"
	if _, err := service.GetSubscription(context.Background(), request); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	if _, err := service.GetSubscription(ctx, nil); status.Code(err) != codes.Unauthenticated {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(t.Context(), `DELETE FROM workspace.subscriptions`); err != nil {
		t.Fatal(err)
	}
	identity.role = api.TenantRoleEnum_TENANT_ROLE_ENUM_MEMBER
	if _, err := service.GetSubscription(ctx, request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("member learned subscription absence: %v", err)
	}
	identity.role = api.TenantRoleEnum_TENANT_ROLE_ENUM_ADMIN
	if out, err := service.GetSubscription(ctx, request); status.Code(err) != codes.NotFound || out != nil {
		t.Fatalf("no subscription: %v, %v", out, err)
	}
	identity.failure = status.Error(codes.Unavailable, "identity unavailable")
	if _, err := service.GetSubscription(ctx, request); status.Code(err) != codes.Unavailable {
		t.Fatal(err)
	}
}

func TestGetSubscriptionDatabaseUnknownIsNotMissing(t *testing.T) {
	for _, afterAuthorization := range []bool{false, true} {
		t.Run(map[bool]string{false: "workspace lookup", true: "subscription lookup"}[afterAuthorization], func(t *testing.T) {
			db := runtimeDatabase(t)
			service, identity, _ := subscriptionFixture(t, db, "quoted")
			closeDB := func() {
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if afterAuthorization {
				identity.afterAuthorize = closeDB
			} else {
				closeDB()
			}
			call, ctx := subscriptionCall()
			out, err := service.GetSubscription(ctx, &api.GetSubscriptionRpcRequest{Context: call, WorkspaceId: "workspace-subscription"})
			if status.Code(err) != codes.Unavailable || out != nil {
				t.Fatalf("unknown persistence presented as missing: %v, %v", out, err)
			}
		})
	}
}

// This is the real HTTP -> protobuf RPC -> Workspace -> isolated PostgreSQL
// chain. Only CloudIdentity's decision seam is a probe; no owner DTO is mocked.
func TestGetSubscriptionBFFThroughWorkspaceGRPCAndPostgres(t *testing.T) {
	db := runtimeDatabase(t)
	service, identity, _ := subscriptionFixture(t, db, "quoted")
	const token = "isolated-subscription-bff-peer-token"
	server, err := ownerservice.NewServer(ownerservice.Config{Owner: owneridentity.Workspace,
		TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{owneridentity.ConsoleBFF: token}})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(server); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.ServeOn(listener)
	t.Cleanup(server.Stop)
	options, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(owneridentity.ConsoleBFF, owneridentity.Workspace.Service(), token)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(listener.Addr().String(), options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := api.NewWorkspaceProductServiceClient(conn)
	handler := bff.NewWorkspaceHandler(client, identity)
	for _, tc := range []struct {
		name   string
		role   api.TenantRoleEnum
		tenant string
		want   int
	}{
		{"owner", api.TenantRoleEnum_TENANT_ROLE_ENUM_OWNER, "subscription-tenant", 200},
		{"admin", api.TenantRoleEnum_TENANT_ROLE_ENUM_ADMIN, "subscription-tenant", 200},
		{"member", api.TenantRoleEnum_TENANT_ROLE_ENUM_MEMBER, "subscription-tenant", 403},
		{"cross tenant", api.TenantRoleEnum_TENANT_ROLE_ENUM_OWNER, "foreign-tenant", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity.role, identity.tenant = tc.role, tc.tenant
			request := httptest.NewRequest(http.MethodGet, "/api/v2/workspaces/workspace-subscription/subscription", nil)
			request.AddCookie(&http.Cookie{Name: "opl_console_session", Value: "subscription-session"})
			request.Header.Set("X-OPL-Actor", "forged")
			request.Header.Set("X-OPL-Tenant", "subscription-tenant")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tc.want || response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("HTTP %d: %s", response.Code, response.Body.String())
			}
			if tc.want == 200 {
				var body map[string]any
				if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body["currentMonthlyUSDMicros"] != "9007199254740993" || body["currentPricePolicyVersionId"] != "price-current-accepted" || body["refundTerms"] != "Original refund terms" {
					t.Fatalf("wire facts: %v", body)
				}
			} else if strings.Contains(response.Body.String(), "price-current-accepted") {
				t.Fatal("financial data leaked in denial")
			}
		})
	}
	// Direct RPC must reject the member even without the BFF's first check.
	identity.role = api.TenantRoleEnum_TENANT_ROLE_ENUM_MEMBER
	call, _ := subscriptionCall()
	if out, err := client.GetSubscription(t.Context(), &api.GetSubscriptionRpcRequest{Context: call, WorkspaceId: "workspace-subscription"}); status.Code(err) != codes.PermissionDenied || out != nil {
		t.Fatalf("member direct RPC: %v, %v", out, err)
	}
}
