//go:build livebuild

package build

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/build/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// Only input resolution is simulated here. Admission, identity readback and
// persistence run through their real owners and databases without an exporter.
type bindingBuildInput struct {
	api.CapabilityCoordinationClient
	input *api.BuildInputSnapshot
}

func (c bindingBuildInput) ResolveBuildInput(context.Context, *api.BuildInputRequest, ...grpc.CallOption) (*api.BuildInputSnapshot, error) {
	return proto.Clone(c.input).(*api.BuildInputSnapshot), nil
}

type bindingGatewayRead struct {
	api.GatewayCoordinationClient
	request      *api.UserExternalBindingReadRequest
	foreignActor bool
}

func (c *bindingGatewayRead) ReadUserExternalIdentity(ctx context.Context, req *api.UserExternalBindingReadRequest, opts ...grpc.CallOption) (*api.UserExternalBindingReadback, error) {
	c.request = proto.Clone(req).(*api.UserExternalBindingReadRequest)
	binding, err := c.GatewayCoordinationClient.ReadUserExternalIdentity(ctx, req, opts...)
	if err == nil && c.foreignActor {
		binding.ActorId = "another-user"
	}
	return binding, err
}

func TestLiveBuildUsesAuthenticatedUserDestination(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	t.Setenv("OPL_BUILD_TEST_REGISTRY_HOST", "127.0.0.1:5000")
	t.Setenv("OPL_BUILD_TEST_REGISTRY_NAMESPACE", "result")
	dsn := startLivePostgres(t, ctx)
	identity := newLiveIdentity(t, ctx, dsn)
	// Both Users share one Tenant, but each keeps its own destination. A new
	// login observes the current membership through the real session owner.
	if _, err := identity.db.ExecContext(ctx, `UPDATE tenant.tenant_members SET tenant_id='tenant-live' WHERE actor_id='102'`); err != nil {
		t.Fatal(err)
	}
	_, challenge, err := identity.client.LoginContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, cookie, err := identity.client.Login(ctx, challenge, &api.LoginRequest{Username: "other@example.test", Password: "isolated-password"})
	if err != nil {
		t.Fatal(err)
	}
	identity.cookies["another-tenant"] = cookie

	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	db, _ := liveOwnerDB(t, ctx, dsn, "build", source)
	store, err := ownerstore.New(db, "build")
	if err != nil {
		t.Fatal(err)
	}
	conn := identityConn(t, identity.address, owneridentity.Build.Service())
	authorization := api.NewCloudIdentityAuthorizationClient(conn)
	gateway := &bindingGatewayRead{GatewayCoordinationClient: api.NewGatewayCoordinationClient(conn)}
	runtime, webui := gapContracts(t)
	input := gapSnapshot(t, runtime, webui)
	runner := gapRunner()
	runner.RegistryPrefix = "127.0.0.1:5000/result"
	service, err := New(store, ownerservice.NewAuthorizer(owneridentity.Build, authorization), bindingBuildInput{input: input}, api.NewCapabilityProductServiceClient(conn), nil, authorization, gateway, runner)
	if err != nil {
		t.Fatal(err)
	}
	ctx = ownerservice.WithPeerOwner(ctx, owneridentity.ConsoleBFF)
	body := &api.CreateBuildRequest{PackageVersionId: input.PackageVersionId, RuntimeVersionId: input.RuntimeVersionId, WebuiVersionId: input.WebuiVersionId}
	var otherJob *api.BuildJob
	for _, user := range []struct{ tenant, repository string }{{"tenant-live", "publisher"}, {"another-tenant", "other"}} {
		call := identity.call("create-"+user.repository, user.tenant)
		call.Scope.GetTenant().TenantId = "tenant-live"
		job, err := service.CreateBuild(ctx, &api.CreateBuildRpcRequest{Context: call, Body: body})
		if err != nil {
			t.Fatal(err)
		}
		record, err := service.read(ctx, job.Id)
		if err != nil {
			t.Fatal(err)
		}
		want := runner.RegistryPrefix + "/" + user.repository + ":" + job.Id
		if record.Executor != want || record.Actor != call.ActorId || gateway.request.GetActorId() != call.ActorId || !proto.Equal(gateway.request.GetContext(), call) {
			t.Fatalf("wrong User destination or context: executor=%q want=%q actor=%q request=%v", record.Executor, want, record.Actor, gateway.request)
		}
		otherJob = job
	}
	// A retry is a new Build by the current actor, even when another member
	// created the failed job. The old executor remains frozen for recovery.
	otherRecord, err := service.read(ctx, otherJob.Id)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.stage(ctx, otherRecord, "failed", "failed", ""); err != nil {
		t.Fatal(err)
	}
	retried, err := service.RetryBuild(ctx, &api.RetryBuildRpcRequest{Context: identity.call("retry-as-current-user", "tenant-live"), BuildId: otherJob.Id})
	if err != nil {
		t.Fatal(err)
	}
	retriedRecord, err := service.read(ctx, retried.Id)
	if err != nil || retriedRecord.Executor != runner.RegistryPrefix+"/publisher:"+retried.Id {
		t.Fatalf("retry did not use current actor: record=%v err=%v", retriedRecord, err)
	}
	original, err := service.read(ctx, otherJob.Id)
	if err != nil || original.Executor != otherRecord.Executor || !strings.Contains(original.Executor, "/other:") {
		t.Fatalf("historical executor changed: record=%v err=%v", original, err)
	}
	var before int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM build.build_jobs`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	gateway.foreignActor = true
	if _, err := service.CreateBuild(ctx, &api.CreateBuildRpcRequest{Context: identity.call("foreign-response", "tenant-live"), Body: body}); err == nil {
		t.Fatal("Build accepted another actor's binding")
	}
	gateway.foreignActor = false
	foreign := identity.call("foreign-tenant", "tenant-live")
	foreign.Scope.GetTenant().TenantId = "another-tenant"
	if _, err := service.CreateBuild(ctx, &api.CreateBuildRpcRequest{Context: foreign, Body: body}); err == nil {
		t.Fatal("Build bypassed Tenant authorization")
	}
	for _, tc := range []struct{ name, update, restore string }{
		{"unverified-destination", "destination_verified_at=NULL", "destination_verified_at=now()"},
		{"revoked-identity", "revoked_at=now()", "revoked_at=NULL"},
		{"unverified-identity", "verified_at=NULL", "verified_at=now()"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gatewayDB := identity.service.GatewayStore.DB()
			if _, err := gatewayDB.ExecContext(ctx, "UPDATE gateway.identity_mappings SET "+tc.update+" WHERE actor_id='101'"); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := gatewayDB.ExecContext(ctx, "UPDATE gateway.identity_mappings SET "+tc.restore+" WHERE actor_id='101'"); err != nil {
					t.Error(err)
				}
			}()
			if _, err := service.CreateBuild(ctx, &api.CreateBuildRpcRequest{Context: identity.call(tc.name, "tenant-live"), Body: body}); err == nil {
				t.Fatal("Build admitted unusable User binding")
			}
		})
	}
	if _, err := identity.service.GatewayStore.DB().ExecContext(ctx, `DELETE FROM gateway.identity_mappings WHERE actor_id='101'`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.CreateBuild(ctx, &api.CreateBuildRpcRequest{Context: identity.call("missing-binding", "tenant-live"), Body: body}); err == nil {
		t.Fatal("Build admitted a missing User binding")
	}
	var after int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM build.build_jobs`).Scan(&after); err != nil || after != before {
		t.Fatalf("rejected admission persisted jobs: before=%d after=%d err=%v", before, after, err)
	}
}
