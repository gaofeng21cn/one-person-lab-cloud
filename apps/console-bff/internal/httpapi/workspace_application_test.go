package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"time"
)

// workspaceReadProbe answers the Workspace owner's product read with one
// configured row, so a test exercises the BFF's composition instead of the
// owner's own storage.
type workspaceReadProbe struct {
	api.WorkspaceProductServiceClient
	workspace *api.Workspace
	err       error
	reads     int
}

func (p *workspaceReadProbe) GetWorkspace(_ context.Context, _ *api.GetWorkspaceRpcRequest, _ ...grpc.CallOption) (*api.Workspace, error) {
	p.reads++
	return p.workspace, p.err
}

// serveApplicationStub is the Serve read surface the composed Workspace view
// reads: the deployment history and the current application's access. It counts
// its own reads so a test can prove which branches were actually taken.
type serveApplicationStub struct {
	deployments *api.DeploymentPage
	entry       *api.WorkspaceAccess
	deployErr   error
	accessErr   error

	deploymentReads int
	accessReads     int
}

func (s *serveApplicationStub) Deployments(context.Context, string, string, int32) (*api.DeploymentPage, error) {
	s.deploymentReads++
	return s.deployments, s.deployErr
}

func (s *serveApplicationStub) WorkspaceAccess(context.Context, string) (*api.WorkspaceAccess, error) {
	s.accessReads++
	return s.entry, s.accessErr
}

// servedWorkspace is the Workspace owner's own readback: identity, plan,
// readiness and period are its facts, while the application availability stays
// unconfirmed until Serve's own readback is composed in.
func servedWorkspace(state api.WorkspaceStatusEnum) *api.Workspace {
	return &api.Workspace{
		Id: "ws-1", Name: "Research", ComputePlanId: "compute-plan-a", StoragePlanId: "storage-plan-a",
		CapabilityVersionId:     ptr("cv-1"),
		Status:                  state,
		DeliveryModel:           api.WorkspaceDeliveryModelEnum_WORKSPACE_DELIVERY_MODEL_ENUM_AGENT_SAAS,
		ResourceReadiness:       api.WorkspaceResourceReadinessEnum_WORKSPACE_RESOURCE_READINESS_ENUM_READY,
		ApplicationAvailability: api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNKNOWN,
		CreatedAt:               timestamppb.Now(), UpdatedAt: timestamppb.Now(),
	}
}

// composedWorkspaceReader is the BFF read surface of the running process: the
// Workspace owner read plus the Serve application readbacks the composed
// Workspace detail route needs. The remaining owner reads stay unconfigured so
// a test cannot silently pass through an unrelated surface.
type composedWorkspaceReader struct {
	*fakeReader
	probe *workspaceReadProbe
	serve *serveApplicationStub
}

func (r *composedWorkspaceReader) Workspace(context.Context, string) (*api.Workspace, error) {
	return r.probe.workspace, r.probe.err
}

func (r *composedWorkspaceReader) Deployments(ctx context.Context, id, cursor string, limit int32) (*api.DeploymentPage, error) {
	return r.serve.Deployments(ctx, id, cursor, limit)
}

func (r *composedWorkspaceReader) WorkspaceAccess(ctx context.Context, id string) (*api.WorkspaceAccess, error) {
	return r.serve.WorkspaceAccess(ctx, id)
}

func (r *composedWorkspaceReader) WorkspaceClient() api.WorkspaceProductServiceClient {
	return r.probe
}

type composedFixture struct {
	probe  *workspaceReadProbe
	serve  *serveApplicationStub
	reader *composedWorkspaceReader
	server *Server
}

func newComposedFixture(workspace *api.Workspace, serve *serveApplicationStub) composedFixture {
	probe := &workspaceReadProbe{workspace: workspace}
	reader := &composedWorkspaceReader{fakeReader: resolvedReader(), probe: probe, serve: serve}
	return composedFixture{probe: probe, serve: serve, reader: reader, server: NewServer(reader, allowedIdentity())}
}

func (f composedFixture) read(t *testing.T) (int, map[string]any) {
	t.Helper()
	response := httptest.NewRecorder()
	f.server.Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1"))
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q: %v", response.Body.String(), err)
	}
	return response.Code, body
}

func activeDeployment() *api.DeploymentPage {
	return &api.DeploymentPage{Items: []*api.Deployment{{
		Id: "dep-1", WorkspaceId: "ws-1", CapabilityVersionId: "cv-1",
		Status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE,
	}}}
}

func applicationEntry() *api.WorkspaceAccess {
	return &api.WorkspaceAccess{
		WorkspaceId: "ws-1", Url: "https://agent.example.test",
		AuthenticationMode: api.WorkspaceAccessAuthenticationModeEnum_WORKSPACE_ACCESS_AUTHENTICATION_MODE_ENUM_APPLICATION_LOGIN,
	}
}

// TestWorkspaceReadComposesTheServeApplicationEntry is the narrow reproduction of
// the missing composition: the Workspace owner answers `unknown` for a Workspace
// it knows is active, and only Serve's own active deployment plus confirmed
// access entry can make the response say `available` with the entry URL.
func TestWorkspaceReadComposesTheServeApplicationEntry(t *testing.T) {
	fixture := newComposedFixture(servedWorkspace(api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE),
		&serveApplicationStub{deployments: activeDeployment(), entry: applicationEntry()})
	code, body := fixture.read(t)
	if code != http.StatusOK {
		t.Fatalf("status = %d, body = %v", code, body)
	}
	if body["applicationAvailability"] != "available" || body["accessUrl"] != "https://agent.example.test" {
		t.Fatalf("Workspace read did not compose Serve's application entry: %v", body)
	}
	// The Workspace owner's own facts survive the composition unchanged.
	if body["id"] != "ws-1" || body["name"] != "Research" || body["status"] != "active" ||
		body["resourceReadiness"] != "ready" || body["capabilityVersionId"] != "cv-1" ||
		body["computePlanId"] != "compute-plan-a" || body["storagePlanId"] != "storage-plan-a" {
		t.Fatalf("composition changed the Workspace owner's own facts: %v", body)
	}
	if fixture.serve.deploymentReads != 1 || fixture.serve.accessReads != 1 || fixture.probe.reads != 1 {
		t.Fatalf("composition read counts: %+v %+v", fixture.serve, fixture.probe)
	}
}

// TestWorkspaceReadDerivesAvailabilityFromTheCurrentDeployment covers every
// deployment state against the same owner readback: only an active deployment
// whose access Serve confirms may be advertised as available, a delivery still
// in progress is pending, and a failed or replaced application is unavailable.
func TestWorkspaceReadDerivesAvailabilityFromTheCurrentDeployment(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    api.DeploymentStatusEnum
		entry     *api.WorkspaceAccess
		accessErr error
		want      string
		wantURL   bool
		wantReads int
	}{
		{name: "active", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE, entry: applicationEntry(), want: "available", wantURL: true, wantReads: 1},
		{name: "queued", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_QUEUED, want: "pending"},
		{name: "deploying", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_DEPLOYING, want: "pending"},
		{name: "verifying", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_VERIFYING, want: "pending"},
		{name: "rolling_back", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ROLLING_BACK, want: "pending"},
		{name: "failed", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_FAILED, want: "unavailable"},
		{name: "needs_attention", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_NEEDS_ATTENTION, want: "unavailable"},
		{name: "superseded", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_SUPERSEDED, want: "unavailable"},
		{name: "rolled_back", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ROLLED_BACK, want: "unavailable"},
		{
			name: "active without confirmed entry", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE,
			accessErr: owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "no entry"), api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE),
			want:      "unavailable", wantReads: 1,
		},
		{
			name: "active entry read unavailable", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE,
			accessErr: status.Error(codes.Unavailable, "serve down"),
			want:      "unknown", wantReads: 1,
		},
		{
			name: "active entry without a usable origin", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE,
			entry: &api.WorkspaceAccess{WorkspaceId: "ws-1", Url: "not-a-url"},
			want:  "unknown", wantReads: 1,
		},
		{
			name: "active entry missing from readback", status: api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE,
			want: "unknown", wantReads: 1,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := &api.DeploymentPage{Items: []*api.Deployment{{Id: "dep-1", WorkspaceId: "ws-1", CapabilityVersionId: "cv-1", Status: test.status}}}
			serve := &serveApplicationStub{deployments: page, entry: test.entry, accessErr: test.accessErr}
			fixture := newComposedFixture(servedWorkspace(api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE), serve)
			code, body := fixture.read(t)
			if code != http.StatusOK || body["applicationAvailability"] != test.want {
				t.Fatalf("status = %d, availability = %v, want %s", code, body["applicationAvailability"], test.want)
			}
			_, hasURL := body["accessUrl"]
			if hasURL != test.wantURL {
				t.Fatalf("accessUrl presence = %v, want %v (%v)", hasURL, test.wantURL, body)
			}
			if serve.accessReads != test.wantReads {
				t.Fatalf("access reads = %d, want %d", serve.accessReads, test.wantReads)
			}
		})
	}
}

// TestWorkspaceReadReportsUnavailableWithoutAServeDeployment proves the rule's
// legacy bare-resource case: the Workspace exists but Serve has delivered no
// application, so it must be unavailable rather than pending or openable.
func TestWorkspaceReadReportsUnavailableWithoutAServeDeployment(t *testing.T) {
	fixture := newComposedFixture(servedWorkspace(api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_PROVISIONING), &serveApplicationStub{})
	code, body := fixture.read(t)
	if code != http.StatusOK || body["applicationAvailability"] != "unavailable" {
		t.Fatalf("status = %d, body = %v", code, body)
	}
	if _, hasURL := body["accessUrl"]; hasURL {
		t.Fatalf("a Workspace without a Serve deployment reported an access URL: %v", body)
	}
	if fixture.serve.accessReads != 0 {
		t.Fatal("a Workspace without a current deployment had its access read")
	}
}

// TestWorkspaceReadKeepsOwnerFactsWhenServeIsUnavailable proves an unresolved
// Serve layer degrades to the contract's `unknown` instead of failing the whole
// read or inventing Serve's facts, and that the Workspace owner's own facts are
// still served.
func TestWorkspaceReadKeepsOwnerFactsWhenServeIsUnavailable(t *testing.T) {
	serve := &serveApplicationStub{deployErr: status.Error(codes.Unavailable, "serve down")}
	fixture := newComposedFixture(servedWorkspace(api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE), serve)
	code, body := fixture.read(t)
	if code != http.StatusOK || body["applicationAvailability"] != "unknown" {
		t.Fatalf("status = %d, body = %v", code, body)
	}
	if body["id"] != "ws-1" || body["status"] != "active" || body["resourceReadiness"] != "ready" {
		t.Fatalf("an unresolved Serve layer dropped the Workspace owner's own facts: %v", body)
	}
	if serve.accessReads != 0 {
		t.Fatal("an unreadable deployment history still read the access entry")
	}
}

// TestWorkspaceReadNeverServesAServeFactWithoutAuthorization proves the derived
// layer keeps its own authorization: a caller allowed to read the Workspace but
// denied the Serve deployment read must not receive a composed Serve fact.
func TestWorkspaceReadNeverServesAServeFactWithoutAuthorization(t *testing.T) {
	fixture := newComposedFixture(servedWorkspace(api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE),
		&serveApplicationStub{deployments: activeDeployment(), entry: applicationEntry()})
	identity := allowedIdentity()
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS] = &api.AuthorizationDecision{
		Result: api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED, Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
	}
	fixture.server = NewServer(fixture.reader, identity)
	response := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1"))
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if fixture.serve.deploymentReads != 0 {
		t.Fatal("a denied Serve decision still reached the Serve reader")
	}
}

// TestWorkspaceReadRefusesAnOwnerReadbackForAnotherWorkspace proves a mismatched
// owner readback is refused rather than composed under the requested id.
func TestWorkspaceReadRefusesAnOwnerReadbackForAnotherWorkspace(t *testing.T) {
	foreign := servedWorkspace(api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE)
	foreign.Id = "ws-other"
	fixture := newComposedFixture(foreign, &serveApplicationStub{deployments: activeDeployment(), entry: applicationEntry()})
	response := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1"))
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

// TestWorkspaceReadMapsTheOwnerFailure proves the composed route reports the
// Workspace owner's own typed failure with the contract's public status instead
// of masking it behind the composition.
func TestWorkspaceReadMapsTheOwnerFailure(t *testing.T) {
	fixture := newComposedFixture(servedWorkspace(api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE), &serveApplicationStub{})
	fixture.probe.err = status.Error(codes.NotFound, "absent")
	response := httptest.NewRecorder()
	fixture.server.Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1"))
	if response.Code != http.StatusNotFound || fixture.serve.deploymentReads != 0 {
		t.Fatalf("status = %d, deployments = %d, body = %s", response.Code, fixture.serve.deploymentReads, response.Body.String())
	}
}

// TestWorkspaceListStaysTheOwnerRead proves the composition is scoped to the
// detail route: the list still answers with the Workspace owner's own paged
// readback and does not fan out a Serve call per row.
func TestWorkspaceListStaysTheOwnerRead(t *testing.T) {
	page := &api.WorkspacePage{Items: []*api.Workspace{servedWorkspace(api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE)}}
	serve := &serveApplicationStub{deployments: activeDeployment(), entry: applicationEntry()}
	fixture := newComposedFixture(servedWorkspace(api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE), serve)
	fixture.reader.fakeReader.workspace = page.Items[0]
	identity := allowedIdentity()
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES] = &api.AuthorizationDecision{
		ActorId: "actor-1", SessionId: ptr(owneridentity.SessionReference("session-1")),
		Scope:             &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: "tenant-1"}}},
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Minute)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
		Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED, Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
		Action: api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES, AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE,
		Resource: &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE},
	}
	server := NewServer(&listWorkspaceReader{composedWorkspaceReader: fixture.reader, page: page}, identity)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces"))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if serve.deploymentReads != 0 {
		t.Fatal("the Workspace list fanned out to Serve")
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("list body: %v", body)
	}
	row, _ := items[0].(map[string]any)
	if row["applicationAvailability"] != "unknown" {
		t.Fatalf("the list did not preserve the owner's own readback: %v", row)
	}
}

type listWorkspaceReader struct {
	*composedWorkspaceReader
	page *api.WorkspacePage
}

func (r *listWorkspaceReader) WorkspaceClient() api.WorkspaceProductServiceClient {
	return &listWorkspaceClient{composedReader: r}
}

type listWorkspaceClient struct {
	api.WorkspaceProductServiceClient
	composedReader *listWorkspaceReader
}

func (c *listWorkspaceClient) GetWorkspace(context.Context, *api.GetWorkspaceRpcRequest, ...grpc.CallOption) (*api.Workspace, error) {
	return c.composedReader.workspace, nil
}

func (c *listWorkspaceClient) ListWorkspaces(context.Context, *api.ListWorkspacesRpcRequest, ...grpc.CallOption) (*api.WorkspacePage, error) {
	return c.composedReader.page, nil
}
