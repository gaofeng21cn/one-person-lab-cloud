package delivery_test

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
	"opl-cloud/services/serve/internal/delivery"
	"opl-cloud/services/serve/migrations"
)

// fakeIdentity is a controllable CloudIdentity client. Tests wrap it in the real
// ownerservice.Authorizer, so Serve's declared resource scope, audience and action
// are exercised exactly as production would.
type fakeIdentity struct {
	deny  bool
	last  *api.AuthorizationRequest
	calls int
}

func (f *fakeIdentity) AuthorizeAction(_ context.Context, r *api.AuthorizationRequest, _ ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	f.calls++
	f.last = r
	if f.deny {
		return &api.AuthorizationDecision{Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED}, nil
	}
	return &api.AuthorizationDecision{
		Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		ActorId: r.GetActorId(), Scope: r.GetScope(), SessionId: r.SessionId, AcceptedOperationGrantId: r.AcceptedOperationGrantId,
		AudienceOwner: r.GetAudienceOwner(), Action: r.GetAction(), Resource: r.GetResource(),
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}, nil
}

func (*fakeIdentity) GetAuthorizationContext(context.Context, *api.GetAuthorizationContextRequest, ...grpc.CallOption) (*api.AuthorizationDecision, error) {
	return nil, status.Error(codes.Unimplemented, "not used")
}

func (*fakeIdentity) IssueAcceptedOperationGrant(context.Context, *api.AcceptedOperationGrantRequest, ...grpc.CallOption) (*api.AcceptedOperationGrant, error) {
	return nil, status.Error(codes.Unimplemented, "not used")
}

// ownerAuthorizer is the real Serve authorizer bound to the fake CloudIdentity.
func ownerAuthorizer(identity *fakeIdentity) delivery.AuthorizeFunc {
	return ownerservice.NewAuthorizer(ownerservice.OwnerServe, identity).Authorize
}

// fixture provisions Serve's real isolated database and installs its real
// migration entrypoint, then inserts the owner-local rows a delivery would have
// written. It never seeds another owner's facts.
func fixture(t *testing.T) (*sql.DB, string, string) {
	t.Helper()
	ctx := context.Background()
	dsn := ownerstoretest.EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{
		AdminDSN: dsn, Owner: "serve", Database: "opl_serve",
		SchemaOwnerRole: "opl_serve_owner", WriterRole: "opl_serve_writer", RuntimeRole: "opl_serve_runtime",
	})
	if err != nil {
		t.Fatalf("provision isolated serve database: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	source, err := migrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatalf("install serve migrations: %v", err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatalf("open serve database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, "tenant-alpha", h.RuntimeDSN
}

const artifactDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

func descriptor(t *testing.T, exposure api.WorkspaceApplicationRevisionExposurePolicyEnum) []byte {
	t.Helper()
	d := &api.DeploymentDescriptor{
		SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1,
		Artifact:      &api.ArtifactReference{Repository: "registry.test/app", Digest: artifactDigest},
		Provenance:    api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD,
		ApplicationRevision: &api.WorkspaceApplicationRevision{
			SchemaVersion: 1, ApplicationId: "knowledge-app", Version: "1", Platform: "linux/amd64",
			Image: "registry.test/app@" + artifactDigest, ExposurePolicy: exposure,
		},
	}
	raw, err := publicjson.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func compatibility(t *testing.T) []byte {
	t.Helper()
	raw, err := publicjson.Marshal(&api.DataCompatibility{DataSchemaVersion: "1"})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// seedDeployment writes one Serve-owned deployment plus its runtime instance and
// the Operation that carries the owning tenant.
func seedDeployment(t *testing.T, db *sql.DB, workspace, tenant, deploymentID, deploymentStatus, runtimeStatus string, accessURL string, exposure api.WorkspaceApplicationRevisionExposurePolicyEnum) {
	t.Helper()
	ctx := context.Background()
	operationID := "op_" + deploymentID
	if _, err := db.ExecContext(ctx, `INSERT INTO serve.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,observation_result,request_id,accepted_input,result,completed_at)
		VALUES($1,$2,'actor-1','runtime_deploy',$3,'succeeded','activate','confirmed','req-1','{}','{}',now())`, operationID, tenant, workspace); err != nil {
		t.Fatalf("insert operation: %v", err)
	}
	var runtimeID, activatedAt, verification any
	if deploymentStatus == "active" {
		runtimeID = "rt_" + deploymentID
		activatedAt = time.Now().UTC()
		verification = "build-artifact://" + deploymentID + "/verification"
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO serve.agent_deployments(id,workspace_id,capability_version_id,artifact_digest,reference_claim_id,runtime_instance_id,operation_id,status,data_compatibility,verification_evidence_ref,activated_at,execution_epoch)
		VALUES($1,$2,'cv_1',$3,'claim_1',$4,$5,$6,$7,$8,$9,1)`,
		deploymentID, workspace, artifactDigest, runtimeID, operationID, deploymentStatus, compatibility(t), verification, activatedAt); err != nil {
		t.Fatalf("insert deployment: %v", err)
	}
	if runtimeStatus == "" {
		return
	}
	var url, readiness, observed any
	if runtimeStatus == "ready" {
		url = accessURL
		readiness = "readiness://" + deploymentID
		observed = time.Now().UTC()
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO serve.agent_runtime_instances(id,workspace_id,deployment_id,artifact_digest,fabric_resource_set_id,status,access_url,data_attachment_contract,readiness_evidence_ref,observed_at,execution_epoch,deployment_descriptor,deployment_descriptor_digest,deployment_descriptor_object_ref)
		VALUES($1,$2,$3,$4,'rs_1',$5,$6,'{}',$7,$8,1,$9,'sha256:2222222222222222222222222222222222222222222222222222222222222222','ref')`,
		"rt_"+deploymentID, workspace, deploymentID, artifactDigest, runtimeStatus, url, readiness, observed, descriptor(t, exposure)); err != nil {
		t.Fatalf("insert runtime instance: %v", err)
	}
}

func call(tenant string, platform bool) *api.CallContext {
	scope := &api.AuthorizationScope{}
	if platform {
		scope.Scope = &api.AuthorizationScope_Platform{Platform: &api.PlatformScope{}}
	} else {
		scope.Scope = &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}
	}
	return &api.CallContext{RequestId: "req-1", ActorId: "actor-1", SessionId: proto.String("session-1"), Scope: scope}
}

func serveContext() context.Context {
	return ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
}

// TestServeReadSurfaceIsOwnerTruth proves the Serve read surface reports only
// Serve's own delivery facts and never substitutes a resource-ready fact for a
// ready application.
func TestServeReadSurfaceIsOwnerTruth(t *testing.T) {
	db, tenant, _ := fixture(t)
	identity := &fakeIdentity{}
	service, err := delivery.New(db, ownerAuthorizer(identity))
	if err != nil {
		t.Fatal(err)
	}
	ctx := serveContext()

	// An application exposure policy with a ready runtime: access is available.
	seedDeployment(t, db, "ws-ready", tenant, "dep-ready", "active", "ready", "https://ws-ready.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	access, err := service.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-ready"})
	if err != nil {
		t.Fatalf("ready access: %v", err)
	}
	if access.GetAuthenticationMode() != api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_APPLICATION_LOGIN || !access.GetApplicationCredentialsAvailable() || access.GetUrl() != "https://ws-ready.example/app" || access.GetExpiresAt() != nil {
		t.Fatalf("ready workspace reported %+v", access)
	}
	// Serve asked its own audience for the exact action and resource.
	if identity.last.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_SERVE ||
		identity.last.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS ||
		identity.last.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE ||
		identity.last.GetResource().GetId() != "ws-ready" ||
		identity.last.GetScope().GetTenant().GetTenantId() != tenant {
		t.Fatalf("serve asked the wrong authority: %+v", identity.last)
	}

	// A pending runtime: the resource exists but the application is not ready, so
	// no credentials are claimed and no URL is published.
	seedDeployment(t, db, "ws-pending", tenant, "dep-pending", "active", "starting", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	pending, err := service.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-pending"})
	assertAccessUnavailable(t, pending, err)

	// An anonymous exposure policy: no application credentials even when ready.
	seedDeployment(t, db, "ws-anon", tenant, "dep-anon", "active", "ready", "https://ws-anon.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_ANONYMOUS)
	anon, err := service.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-anon"})
	if err != nil {
		t.Fatal(err)
	}
	if anon.GetAuthenticationMode() != api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_ANONYMOUS || anon.GetApplicationCredentialsAvailable() {
		t.Fatalf("anonymous workspace reported %+v", anon)
	}

	// A Workspace Serve has never delivered has no entry and no fabricated mode.
	empty, err := service.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-unknown"})
	assertAccessUnavailable(t, empty, err)
}

func assertAccessUnavailable(t *testing.T, access *api.WorkspaceAccess, err error) {
	t.Helper()
	code, present := owneridentity.ErrorCode(err)
	if access != nil || status.Code(err) != codes.FailedPrecondition || !present || code != api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE {
		t.Fatalf("unavailable access = %v, error %v (code %v), want typed APP_ACCESS_UNAVAILABLE", access, err, code)
	}
}

func TestServeAccessRejectsUnconfirmedEntries(t *testing.T) {
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, entry string
		exposure    api.WorkspaceApplicationRevisionExposurePolicyEnum
	}{
		{"empty", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION},
		{"relative", "/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION},
		{"invalid", "https://%zz/", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION},
		{"script", "javascript:alert(1)", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION},
		{"credentials", "https://user:password@app.example/", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION},
		{"private", "https://private.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_CLOUD_PRIVATE},
		{"unknown", "https://unknown.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := "ws-" + test.name
			seedDeployment(t, db, workspace, tenant, "dep-"+test.name, "active", "ready", test.entry, test.exposure)
			if test.name == "unknown" {
				// Missing exposure in a persisted descriptor cannot manufacture a mode.
				if _, err := db.ExecContext(context.Background(), `UPDATE serve.agent_runtime_instances SET deployment_descriptor=deployment_descriptor #- '{applicationRevision,exposurePolicy}' WHERE workspace_id=$1`, workspace); err != nil {
					t.Fatal(err)
				}
			}
			access, err := service.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, false), WorkspaceId: workspace})
			assertAccessUnavailable(t, access, err)
		})
	}
}

// TestServeReadSurfaceRejectsCrossTenant proves a tenant-scoped caller cannot
// read a Workspace Serve's own records assign to another tenant, even when the
// live authorizer would allow it.
func TestServeReadSurfaceRejectsCrossTenant(t *testing.T) {
	db, tenant, _ := fixture(t)
	identity := &fakeIdentity{}
	service, err := delivery.New(db, ownerAuthorizer(identity))
	if err != nil {
		t.Fatal(err)
	}
	ctx := serveContext()
	seedDeployment(t, db, "ws-alpha", tenant, "dep-alpha", "active", "ready", "https://ws-alpha.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)

	if _, err := service.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call("tenant-other", false), WorkspaceId: "ws-alpha"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-tenant read was not denied: %v", err)
	}
	// A platform-scoped caller is authorized as a platform resource, not forced
	// into the Workspace's tenant scope.
	platformIdentity := &fakeIdentity{}
	platformService, err := delivery.New(db, ownerAuthorizer(platformIdentity))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := platformService.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call("tenant-other", true), WorkspaceId: "ws-alpha"}); err != nil {
		t.Fatalf("platform read of an existing workspace failed: %v", err)
	}
	if platformIdentity.last.GetScope().GetPlatform() == nil {
		t.Fatalf("platform caller was scoped as a tenant: %+v", platformIdentity.last.GetScope())
	}

	// The live CloudIdentity denial is honored: Serve invents no allow.
	denied, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{deny: true}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := denied.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-alpha"}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("CloudIdentity denial was not honored: %v", err)
	}
}

// TestServeDeploymentHistoryAndAccessMode proves the delivery history and single
// current deployment are read from Serve's own rows with the contract vocabulary.
func TestServeDeploymentHistoryAndAccessMode(t *testing.T) {
	db, tenant, _ := fixture(t)
	service, err := delivery.New(db, ownerAuthorizer(&fakeIdentity{}))
	if err != nil {
		t.Fatal(err)
	}
	ctx := serveContext()
	seedDeployment(t, db, "ws-history", tenant, "dep-old", "superseded", "stopped", "", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	seedDeployment(t, db, "ws-history", tenant, "dep-new", "active", "ready", "https://ws-history.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)

	page, err := service.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-history"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.GetItems()) != 2 {
		t.Fatalf("history length = %d, want 2", len(page.GetItems()))
	}
	// The page order is the schema's list index (created_at DESC, id DESC), so the
	// newest delivery leads. Ordering by id alone would let the delivery history
	// report a different "newest" than the current-Agent selection uses.
	if page.GetItems()[0].GetId() != "dep-new" || page.GetItems()[1].GetId() != "dep-old" {
		t.Fatalf("history order = %q,%q, want dep-new,dep-old (created_at DESC)", page.GetItems()[0].GetId(), page.GetItems()[1].GetId())
	}
	// The cursor is a keyset over the same pair, so the second page continues
	// strictly after the first page's last row.
	first, err := service.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-history", QueryLimit: proto.Int32(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.GetItems()) != 1 || first.GetItems()[0].GetId() != "dep-new" || first.GetNextCursor() != "dep-new" {
		t.Fatalf("first page = %+v cursor=%q", first.GetItems(), first.GetNextCursor())
	}
	second, err := service.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-history", QueryLimit: proto.Int32(1), QueryCursor: proto.String(first.GetNextCursor())})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.GetItems()) != 1 || second.GetItems()[0].GetId() != "dep-old" {
		t.Fatalf("second page = %+v", second.GetItems())
	}
	for _, item := range page.GetItems() {
		if item.GetWorkspaceId() != "ws-history" || item.GetCapabilityVersionId() != "cv_1" || item.GetStatus() == api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_UNSPECIFIED || item.GetDataCompatibility().GetDataSchemaVersion() != "1" || item.GetCreatedAt() == nil {
			t.Fatalf("deployment row not projected: %+v", item)
		}
	}
	deployment, err := service.GetDeployment(ctx, &api.GetDeploymentRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-history", DeploymentId: "dep-new"})
	if err != nil {
		t.Fatal(err)
	}
	if deployment.GetStatus() != api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE || deployment.GetRuntimeInstanceId() != "rt_dep-new" {
		t.Fatalf("current deployment = %+v", deployment)
	}
	if _, err := service.GetDeployment(ctx, &api.GetDeploymentRpcRequest{Context: call(tenant, false), WorkspaceId: "ws-history", DeploymentId: "dep-missing"}); status.Code(err) != codes.NotFound {
		t.Fatalf("missing deployment was not NotFound: %v", err)
	}

	// Exactly one active deployment is the current-Agent authority.
	var active int
	if err := db.QueryRowContext(context.Background(), `SELECT count(*) FROM serve.agent_deployments WHERE workspace_id='ws-history' AND status='active'`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("active deployments = %d, want 1", active)
	}
}
