package coordination_test

import (
	"context"
	"database/sql"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/internal/ownerservice"
)

func operatorReadCall(scope *api.AuthorizationScope) *api.CallContext {
	return &api.CallContext{
		ActorId:        "operator",
		RequestId:      "operator-read-request",
		IdempotencyKey: "operator-read-key",
		SessionId:      proto.String("interactive-session"),
		Scope:          scope,
	}
}

func platformScope() *api.AuthorizationScope {
	return &api.AuthorizationScope{Scope: &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}}
}

type operatorCatalogClient struct {
	api.CatalogCoordinationClient
}

func (*operatorCatalogClient) ReadQuoteResourcePlan(context.Context, *api.QuoteResourcePlanRequest, ...grpc.CallOption) (*api.QuoteAcceptance, error) {
	return nil, status.Error(codes.Unavailable, "operator read test does not use Catalog")
}

func seedOperatorReadSet(t *testing.T, ctx context.Context, db *sql.DB, setID, workspace, tenant string) {
	t.Helper()
	_, err := db.ExecContext(ctx, `INSERT INTO fabric.resource_sets (id,tenant_id,workspace_id,provider,provider_profile_ref,region,compute_plan_id,storage_plan_id,accepted_quote_id,approved_specification,observation_result,observed_at) VALUES ($1,$2,$3,'local-docker','profile','local','cp','sp','quote','{"billingMode":"LOCAL_NO_CHARGE"}'::jsonb,'unknown',NULL)`, setID, tenant, workspace)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO fabric.resources (id,resource_set_id,kind,provider_purchase_key,billing_mode,requested_specification,observation_result) VALUES ($1,$2,'compute','purchase:'||$1,'LOCAL_NO_CHARGE','{}'::jsonb,'unknown')`, "res_"+setID, setID)
	if err != nil {
		t.Fatal(err)
	}
}

func TestOperatorReadUsesFabricOwnershipAndIndependentPlatformAuthorization(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	const setID, workspace, tenant = "rset_operator_read", "workspace_operator_read", "tenant_operator_read"
	seedOperatorReadSet(t, ctx, db, setID, workspace, tenant)

	var gotAction api.AuthorizationActionEnum
	var gotResource *api.AuthorizationResource
	var gotScope ownerservice.ResourceScope
	authority := &identity{}
	ownerAuthorization := ownerservice.NewAuthorizer(owneridentity.Fabric, authority)
	authorize := func(ctx context.Context, call *api.CallContext, action api.AuthorizationActionEnum, resource *api.AuthorizationResource, scope ownerservice.ResourceScope) error {
		gotAction = action
		gotResource = proto.Clone(resource).(*api.AuthorizationResource)
		gotScope = scope
		if call.GetScope().GetPlatform() == nil {
			t.Fatalf("operator authorization was not platform-scoped: %v", call.GetScope())
		}
		return ownerAuthorization.Authorize(ctx, call, action, resource, scope)
	}
	service, err := coordination.New(db, authorize, &operatorCatalogClient{})
	if err != nil {
		t.Fatal(err)
	}

	request := &api.ResourceReadbackRequest{Context: operatorReadCall(platformScope()), WorkspaceId: workspace, ResourceSetId: setID}
	read, err := service.ReadResources(ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF), request)
	if err != nil {
		t.Fatal(err)
	}
	if read.GetResourceSetId() != setID || read.GetWorkspaceId() != workspace || read.GetOutcome() != api.Observation_OBSERVATION_UNKNOWN || len(read.GetResources()) != 1 || read.GetResources()[0].GetState() != "not_dispatched" {
		t.Fatalf("operator readback was not Fabric's persisted fact: %v", read)
	}
	if read.GetObservedAt() != nil || read.GetExecutionResources() != nil || read.GetAbsenceConfirmed() || read.GetErrorCode() != "DEPENDENCY_UNAVAILABLE" || read.GetResourceVersion() == "" {
		t.Fatalf("operator read fabricated provider confirmation: %v", read)
	}
	if gotAction != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_READOPERATORRESOURCES || gotResource.GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE || gotResource.GetId() != workspace || gotScope != (ownerservice.ResourceScope{}) {
		t.Fatalf("wrong operator authorization request: action=%v resource=%v scope=%+v", gotAction, gotResource, gotScope)
	}
	authority.mu.Lock()
	decisionRequest := proto.Clone(authority.last).(*api.AuthorizationRequest)
	authority.mu.Unlock()
	if decisionRequest.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_FABRIC || decisionRequest.GetAction() != gotAction || decisionRequest.GetScope().GetPlatform() == nil || decisionRequest.GetSessionId() != request.GetContext().GetSessionId() || decisionRequest.GetResource().GetId() != workspace {
		t.Fatalf("independent CloudIdentity authorization request changed: %v", decisionRequest)
	}

	// Both readers use the same persisted Fabric facts, even though their
	// authorization scopes differ. Reading must not create a new observation.
	tenantCall := proto.Clone(request.Context).(*api.CallContext)
	tenantCall.Scope = &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}
	service.Authorize = ownerAuthorization.Authorize
	tenantRead, err := service.ReadResources(ownerservice.WithPeerOwner(ctx, owneridentity.Workspace.Service()), &api.ResourceReadbackRequest{Context: tenantCall, ResourceSetId: setID})
	if err != nil || !proto.Equal(read, tenantRead) {
		t.Fatalf("operator and tenant resource readbacks diverged: operator=%v tenant=%v err=%v", read, tenantRead, err)
	}
}

func TestOperatorReadRejectsMismatchedWorkspaceBeforeAuthorization(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	const setID, storedWorkspace, tenant = "rset_operator_mismatch", "workspace_stored", "tenant_operator_mismatch"
	seedOperatorReadSet(t, ctx, db, setID, storedWorkspace, tenant)

	authorizeCalls := 0
	authorize := func(context.Context, *api.CallContext, api.AuthorizationActionEnum, *api.AuthorizationResource, ownerservice.ResourceScope) error {
		authorizeCalls++
		return nil
	}
	service, err := coordination.New(db, authorize, &operatorCatalogClient{})
	if err != nil {
		t.Fatal(err)
	}
	request := &api.ResourceReadbackRequest{Context: operatorReadCall(platformScope()), WorkspaceId: "workspace_claimed", ResourceSetId: setID}
	if _, err = service.ReadResources(ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF), request); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("mismatched Workspace was accepted: %v", err)
	}
	if authorizeCalls != 0 {
		t.Fatalf("authorization ran before Fabric ownership validation: %d calls", authorizeCalls)
	}
}

func TestOperatorReadPropagatesPlatformAuthorizationDenial(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	const setID, workspace, tenant = "rset_operator_denied", "workspace_operator_denied", "tenant_operator_denied"
	seedOperatorReadSet(t, ctx, db, setID, workspace, tenant)

	authority := &identity{deny: true}
	ownerAuthorization := ownerservice.NewAuthorizer(owneridentity.Fabric, authority)
	authorize := func(ctx context.Context, call *api.CallContext, action api.AuthorizationActionEnum, resource *api.AuthorizationResource, scope ownerservice.ResourceScope) error {
		if call.GetScope().GetPlatform() == nil || action != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_READOPERATORRESOURCES || resource.GetId() != workspace || scope != (ownerservice.ResourceScope{}) {
			t.Fatalf("wrong operator authorization request: scope=%v action=%v resource=%v owner-scope=%+v", call.GetScope(), action, resource, scope)
		}
		return ownerAuthorization.Authorize(ctx, call, action, resource, scope)
	}
	service, err := coordination.New(db, authorize, &operatorCatalogClient{})
	if err != nil {
		t.Fatal(err)
	}
	request := &api.ResourceReadbackRequest{Context: operatorReadCall(platformScope()), WorkspaceId: workspace, ResourceSetId: setID}
	if read, err := service.ReadResources(ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF), request); status.Code(err) != codes.PermissionDenied || read != nil {
		t.Fatalf("operator authorization refusal was not propagated: read=%v err=%v", read, err)
	}
}

func TestWorkspaceReadKeepsTenantAuthorizationPath(t *testing.T) {
	db := database(t)
	ctx := context.Background()
	const setID, workspace, tenant = "rset_workspace_read", "workspace_customer_read", "tenant_customer_read"
	seedOperatorReadSet(t, ctx, db, setID, workspace, tenant)

	var gotAction api.AuthorizationActionEnum
	authorize := func(_ context.Context, call *api.CallContext, action api.AuthorizationActionEnum, _ *api.AuthorizationResource, scope ownerservice.ResourceScope) error {
		gotAction = action
		if call.GetScope().GetTenant().GetTenantId() != tenant || scope.TenantID != tenant {
			t.Fatalf("tenant read authorization changed: call=%v scope=%+v", call.GetScope(), scope)
		}
		return nil
	}
	service, err := coordination.New(db, authorize, &operatorCatalogClient{})
	if err != nil {
		t.Fatal(err)
	}
	call := &api.CallContext{ActorId: "customer", RequestId: "customer-read-request", IdempotencyKey: "customer-read-key", SessionId: proto.String("customer-session"), Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
	for _, peer := range []owneridentity.Service{owneridentity.Workspace.Service(), owneridentity.Serve.Service()} {
		t.Run(peer.String(), func(t *testing.T) {
			peerCtx := ownerservice.WithPeerOwner(ctx, peer)
			if _, err = service.ReadResources(peerCtx, &api.ResourceReadbackRequest{Context: call, ResourceSetId: setID}); err != nil {
				t.Fatal(err)
			}
			if gotAction != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES {
				t.Fatalf("tenant read changed authorization action: %v", gotAction)
			}
			foreign := proto.Clone(call).(*api.CallContext)
			foreign.Scope.GetTenant().TenantId = "another-tenant"
			if _, err := service.ReadResources(peerCtx, &api.ResourceReadbackRequest{Context: foreign, ResourceSetId: setID}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("foreign tenant resource read allowed: %v", err)
			}
			if _, err := service.ReadResources(peerCtx, &api.ResourceReadbackRequest{Context: operatorReadCall(platformScope()), ResourceSetId: setID, WorkspaceId: workspace}); status.Code(err) != codes.PermissionDenied {
				t.Fatalf("tenant peer gained platform resource read: %v", err)
			}
		})
	}
}
