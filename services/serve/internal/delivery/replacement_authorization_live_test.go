//go:build livebuild

package delivery_test

import (
	"context"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	capability "opl-cloud/services/capability/catalog"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/serve/internal/delivery"
)

type lostUpgradeGrant struct {
	api.CloudIdentityAuthorizationClient
	drop bool
}

func (c *lostUpgradeGrant) IssueAcceptedOperationGrant(ctx context.Context, r *api.AcceptedOperationGrantRequest, opts ...grpc.CallOption) (*api.AcceptedOperationGrant, error) {
	grant, err := c.CloudIdentityAuthorizationClient.IssueAcceptedOperationGrant(ctx, r, opts...)
	if err == nil && c.drop {
		c.drop = false
		return nil, status.Error(codes.Unavailable, "accepted grant response lost")
	}
	return grant, err
}

type lostUpgradeReference struct {
	api.CapabilityCoordinationClient
	acquire, bind bool
}

func (c *lostUpgradeReference) AcquireReference(ctx context.Context, r *api.ReferenceClaimRequest, opts ...grpc.CallOption) (*api.ReferenceClaim, error) {
	claim, err := c.CapabilityCoordinationClient.AcquireReference(ctx, r, opts...)
	if err == nil && c.acquire {
		c.acquire = false
		return nil, status.Error(codes.Unavailable, "acquired reference response lost")
	}
	return claim, err
}

func (c *lostUpgradeReference) BindReference(ctx context.Context, r *api.BindReferenceRequest, opts ...grpc.CallOption) (*api.ReferenceClaim, error) {
	claim, err := c.CapabilityCoordinationClient.BindReference(ctx, r, opts...)
	if err == nil && c.bind {
		c.bind = false
		return nil, status.Error(codes.Unavailable, "bound reference response lost")
	}
	return claim, err
}

// Resource and catalog facts are declared fixtures. Their actual CloudIdentity
// authorizers enforce the continuation audience/resource consumed by Serve.
type authorizedUpgradeCatalog struct {
	*upgradeRuntimeCatalog
	auth *ownerservice.Authorizer
}

func (c *authorizedUpgradeCatalog) ListRuntimeVersions(ctx context.Context, r *api.ListRuntimeVersionsRpcRequest, opts ...grpc.CallOption) (*api.RuntimeVersionPage, error) {
	if err := c.auth.Authorize(serveContext(), r.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTRUNTIMEVERSIONS, &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_CATALOG}, ownerservice.ResourceScope{TenantID: liveTenant}); err != nil {
		return nil, err
	}
	return c.upgradeRuntimeCatalog.ListRuntimeVersions(ctx, r, opts...)
}

type authorizedUpgradeResources struct {
	*secretResources
	auth *ownerservice.Authorizer
}

func (r *authorizedUpgradeResources) ReadResources(ctx context.Context, request *api.ResourceReadbackRequest, opts ...grpc.CallOption) (*api.ResourceReadback, error) {
	if err := r.auth.Authorize(serveContext(), request.GetContext(), api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_OBSERVERESOURCES, &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(r.workspace)}, ownerservice.ResourceScope{TenantID: liveTenant}); err != nil {
		return nil, err
	}
	return r.secretResources.ReadResources(ctx, request, opts...)
}

type authorizedUpgradeRuntime struct {
	*upgradeRuntime
	t *testing.T
}

func (r *authorizedUpgradeRuntime) Lifecycle(ctx context.Context, command *api.RuntimeDeployCommand, target delivery.ExecutionTarget, desired string) error {
	if command.GetContext().GetAcceptedOperationGrantId() == "" || command.GetContext().GetSessionId() != "" {
		r.t.Fatal("replacement lifecycle borrowed a browser session or lost its own grant")
	}
	return r.upgradeRuntime.Lifecycle(ctx, command, target, desired)
}

func TestLiveNativeUpgradeGrantRecoversCommittedStages(t *testing.T) {
	ctx := context.Background()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	identity := newLiveIdentityChain(t, ctx, dsn)
	s, original, catalog, runtime, resources, gateway := nativeUpgradeFixture(t)
	if _, err := s.DB.Exec(`UPDATE serve.operations SET tenant_id=$1`, liveTenant); err != nil {
		t.Fatal(err)
	}
	client := api.NewCloudIdentityAuthorizationClient(serveLiveConn(t, identity.address, owneridentity.Serve.Service(), owneridentity.Tenant.Service()))
	s.Authorize = ownerservice.NewAuthorizer(owneridentity.Serve, client).Authorize
	s.Identity = &lostUpgradeGrant{CloudIdentityAuthorizationClient: client, drop: true}
	s.Runtime = &authorizedUpgradeRuntime{upgradeRuntime: runtime, t: t}
	s.Resources = &authorizedUpgradeResources{secretResources: resources, auth: ownerservice.NewAuthorizer(owneridentity.Fabric, api.NewCloudIdentityAuthorizationClient(serveLiveConn(t, identity.address, owneridentity.Fabric.Service(), owneridentity.Tenant.Service())))}
	checkedCatalog := &authorizedUpgradeCatalog{upgradeRuntimeCatalog: catalog, auth: ownerservice.NewAuthorizer(owneridentity.RuntimeControl, api.NewCloudIdentityAuthorizationClient(serveLiveConn(t, identity.address, owneridentity.RuntimeControl.Service(), owneridentity.Tenant.Service())))}
	s.RuntimeReleases = checkedCatalog
	capDB := serveCapabilityDB(t, ctx, dsn)
	capService := &capability.Service{DB: capDB, Runtime: checkedCatalog, Authorize: ownerservice.NewAuthorizer(owneridentity.Capability, api.NewCloudIdentityAuthorizationClient(serveLiveConn(t, identity.address, owneridentity.Capability.Service(), owneridentity.Tenant.Service()))).Authorize}
	capAddress := serveLiveOwner(t, owneridentity.Capability, []owneridentity.Service{owneridentity.Serve.Service()}, capService.Register)
	s.References = &lostUpgradeReference{CapabilityCoordinationClient: api.NewCapabilityCoordinationClient(serveLiveConn(t, capAddress, owneridentity.Serve.Service(), owneridentity.Capability.Service())), acquire: true, bind: true}
	serveAddress := serveLiveOwner(t, owneridentity.Serve, []owneridentity.Service{owneridentity.Tenant.Service(), owneridentity.Capability.Service()}, s.Register)
	identity.service.ServeCommit = api.NewOwnerCommitReadbackClient(serveLiveConn(t, serveAddress, owneridentity.Tenant.Service(), owneridentity.Serve.Service()))
	capService.ServeCommit = api.NewOwnerCommitReadbackClient(serveLiveConn(t, serveAddress, owneridentity.Capability.Service(), owneridentity.Serve.Service()))
	request := nativeUpgradeRequest(original.DeploymentId, "native-live-grant")
	request.Context = bffReadCall(t, ctx, identity, "serve", "upgrade-acceptance")
	request.Context.IdempotencyKey = "native-live-grant"
	decision, err := client.AuthorizeAction(ctx, &api.AuthorizationRequest{ActorId: request.Context.ActorId, SessionId: request.Context.SessionId, Scope: request.Context.Scope, AudienceOwner: api.OwnerEnum_OWNER_ENUM_SERVE, Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION, Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(request.WorkspaceId)}, RequestId: request.Context.RequestId})
	if err != nil {
		t.Fatal(err)
	}
	request.Context.AuthorizationContextId = decision.GetAuthorizationContextId()
	var operation, deployment, accepted string
	var acceptedAt time.Time
	for stage := 0; stage < 3; stage++ {
		if _, err = s.UpdateWorkspaceVersion(serveContext(), request); status.Code(err) != codes.Unavailable {
			t.Fatalf("lost-response stage %d: %v", stage, err)
		}
		var op, dep, input string
		var at time.Time
		if err = s.DB.QueryRow(`SELECT id,resource_id,accepted_input::text,created_at FROM serve.operations WHERE kind='update_workspace'`).Scan(&op, &dep, &input, &at); err != nil {
			t.Fatal(err)
		}
		if stage == 0 {
			operation, deployment, accepted, acceptedAt = op, dep, input, at
			// A new intent cannot pass an already committed operation-only fence.
			other := proto.Clone(request).(*api.UpdateWorkspaceVersionRpcRequest)
			other.Context.IdempotencyKey = "another-upgrade"
			if _, err = s.UpdateWorkspaceVersion(serveContext(), other); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("operation-only admission admitted a competing writer: %v", err)
			}
			changed := proto.Clone(request).(*api.UpdateWorkspaceVersionRpcRequest)
			changed.Body.ApplicationSelection.RuntimeVersionId = proto.String("rv-original")
			if _, err = s.UpdateWorkspaceVersion(serveContext(), changed); status.Code(err) != codes.AlreadyExists {
				t.Fatalf("accepted upgrade changed its frozen source: %v", err)
			}
			if _, err = s.ReloadModels(workspaceContext(), &api.RuntimeReloadCommand{Context: request.Context, RuntimeInstanceId: original.RuntimeInstanceId, TargetVersion: 7, ManagedKeyBinding: confirmedReloadBindingReadback(request.WorkspaceId, "later-key")}); status.Code(err) != codes.FailedPrecondition || resources.rebinds != 0 {
				t.Fatalf("reload changed an accepted predecessor before its upgrade resumed: %v, rebinds=%d", err, resources.rebinds)
			}
			catalog.versions["rv-next"].Status = api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_REVOKED
			if _, err = s.UpdateWorkspaceVersion(serveContext(), request); status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("Serve grant bypassed Runtime retirement: %v", err)
			}
			var state string
			var replacements int
			if err = s.DB.QueryRow(`SELECT status,(SELECT count(*) FROM serve.agent_deployments WHERE operation_id=$1) FROM serve.operations WHERE id=$1`, operation).Scan(&state, &replacements); err != nil || state != "needs_attention" || replacements != 0 {
				t.Fatalf("retirement did not retain explicit blocked admission: status=%s deployments=%d err=%v", state, replacements, err)
			}
			// An explicit catalog-owner correction may resume this same intent;
			// Serve never silently substitutes another Runtime Release.
			catalog.versions["rv-next"].Status = api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED
		} else if op != operation || dep != deployment || input != accepted || !at.Equal(acceptedAt) {
			t.Fatal("lost-response replay changed original acceptance evidence")
		}
		if runtime.starts != 1 || len(runtime.events) != 0 {
			t.Fatalf("provider ran before binding completed: %v", runtime.events)
		}
	}
	// Reconstruct the owner process with its same store and typed clients. A new
	// interactive authorization cannot replace the original acceptance evidence.
	restarted, err := delivery.New(s.DB, s.Authorize)
	if err != nil {
		t.Fatal(err)
	}
	restarted.Identity, restarted.References, restarted.RuntimeReleases, restarted.Resources, restarted.Runtime = s.Identity, s.References, s.RuntimeReleases, s.Resources, s.Runtime
	catalogCalls := catalog.calls
	result, err := restarted.UpdateWorkspaceVersion(serveContext(), request)
	if err != nil || result.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || result.GetOperationId() != operation {
		t.Fatalf("resumed native upgrade: %v %v", result, err)
	}
	if _, err = restarted.UpdateWorkspaceVersion(serveContext(), request); err != nil {
		t.Fatal(err)
	}
	if runtime.starts != 2 || gateway.calls != 0 || resources.binds != 1 || resources.rebinds != 0 || catalog.calls != catalogCalls+2 {
		t.Fatalf("replay duplicated execution or consulted live admission: starts=%d gateway=%d binds=%d rebinds=%d catalog=%d", runtime.starts, gateway.calls, resources.binds, resources.rebinds, catalog.calls-catalogCalls)
	}
	var grants, claims, deployments int
	if err = identity.db.QueryRow(`SELECT count(*) FROM tenant.accepted_operation_grants WHERE accepted_operation_id=$1`, operation).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if err = capDB.QueryRow(`SELECT count(*) FROM capability.reference_claims WHERE claimant_resource_id=$1 AND bound_operation_id=$2`, deployment, operation).Scan(&claims); err != nil {
		t.Fatal(err)
	}
	if err = s.DB.QueryRow(`SELECT count(*) FROM serve.agent_deployments WHERE operation_id=$1`, operation).Scan(&deployments); err != nil || grants != 1 || claims != 1 || deployments != 1 {
		t.Fatalf("grant/claim/deployment=%d/%d/%d: %v", grants, claims, deployments, err)
	}
	var after string
	if err = s.DB.QueryRow(`SELECT accepted_input::text FROM serve.operations WHERE id=$1`, operation).Scan(&after); err != nil || after != accepted {
		t.Fatalf("continuation rewrote original acceptance: %v", err)
	}
	t.Log("real Identity and Capability owners, SQL acceptance/claim/bind, three lost ACKs and Serve reconstruction passed; provider/catalog prerequisites are fixtures, no real resource or monetary action executed")
}
