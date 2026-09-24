package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// fakeReader is a typed owner reader double. Each owner read either returns the
// configured fact or a configured error, so a missing owner is a real failure.
type fakeReader struct {
	workspace   *api.Workspace
	deployments *api.DeploymentPage
	access      *api.WorkspaceAccess
	build       *api.BuildJob
	version     *api.CapabilityVersion
	operation   *api.Operation
	err         error
}

func (f *fakeReader) Workspace(context.Context, string) (*api.Workspace, error) {
	return f.workspace, f.err
}

func (f *fakeReader) Deployments(context.Context, string) (*api.DeploymentPage, error) {
	return f.deployments, f.err
}

func (f *fakeReader) WorkspaceAccess(context.Context, string) (*api.WorkspaceAccess, error) {
	return f.access, f.err
}

func (f *fakeReader) Build(context.Context, string) (*api.BuildJob, error) {
	return f.build, f.err
}

func (f *fakeReader) CapabilityVersion(context.Context, string) (*api.CapabilityVersion, error) {
	return f.version, f.err
}

func (f *fakeReader) Operation(_ context.Context, _ owneridentity.Owner, _ string) (*api.Operation, error) {
	return f.operation, f.err
}

// fakeIdentity is a typed CloudIdentity double. The session and the authorization
// decision are configured separately so a test can show that a valid session alone
// does not authorize an action.
type fakeIdentity struct {
	session  *api.Session
	decision *api.AuthorizationDecision
	err      error
	requests []*api.AuthorizationRequest
}

func (f *fakeIdentity) Session(context.Context, string) (*api.Session, error) {
	return f.session, f.err
}

func (f *fakeIdentity) Authorize(_ context.Context, request *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	f.requests = append(f.requests, request)
	return f.decision, f.err
}

func allowedIdentity() *fakeIdentity {
	return &fakeIdentity{
		session: &api.Session{ActorId: "actor-1", TenantId: ptr("tenant-1"), CsrfToken: "csrf-1"},
		decision: &api.AuthorizationDecision{
			Result:        api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
			Issuer:        api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
			Action:        api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE,
			AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE,
			Resource:      &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: ptr("ws-1")},
		},
	}
}

// sessionRequest is the BFF request shape: a browser session cookie and the same
// origin path the browser calls.
func sessionRequest(method, path string) *http.Request {
	request := httptest.NewRequest(method, path, nil)
	request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "session-1"})
	return request
}

func resolvedReader() *fakeReader {
	return &fakeReader{
		workspace: &api.Workspace{
			Id:                  "ws-1",
			ComputePlanId:       "compute-plan-a",
			StoragePlanId:       "storage-plan-a",
			CapabilityVersionId: ptr("cv-1"),
			Status:              api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_ACTIVE,
		},
		deployments: &api.DeploymentPage{
			Items: []*api.Deployment{{
				Id:                  "dep-1",
				WorkspaceId:         "ws-1",
				CapabilityVersionId: "cv-1",
				Status:              api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE,
			}},
		},
		access: &api.WorkspaceAccess{WorkspaceId: "ws-1", Url: "https://agent.example.test"},
		build:  &api.BuildJob{Id: "build-1", Stage: "succeeded", Status: api.BuildJobStatusEnum_BUILD_JOB_STATUS_ENUM_SUCCEEDED},
		version: &api.CapabilityVersion{
			Id:             "cv-1",
			BuildJobId:     ptr("build-1"),
			ArtifactDigest: "sha256:" + strings.Repeat("a", 64),
			Status:         api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY,
			Artifact:       &api.ArtifactReference{Repository: "registry.example.test/opl/agent", Digest: "sha256:" + strings.Repeat("a", 64)},
		},
	}
}

func TestDeliveryViewAttributesEachLayerToItsOwner(t *testing.T) {
	server := NewServer(resolvedReader(), allowedIdentity())
	request := sessionRequest(http.MethodGet, "/api/v2/delivery/ws-1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var view DeliveryView
	if err := json.Unmarshal(response.Body.Bytes(), &view); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, layer := range []OwnerFact{view.Workspace, view.Serve, view.Capability, view.Build} {
		if layer.Owner == "" || layer.State == "" {
			t.Fatalf("layer without owner attribution: %+v", layer)
		}
	}
	if view.Serve.Details["accessUrl"] != "https://agent.example.test" {
		t.Fatalf("serve access url not composed: %+v", view.Serve.Details)
	}
	if view.Capability.Details["artifactDigest"] == "" {
		t.Fatalf("capability digest not composed: %+v", view.Capability.Details)
	}
}

func TestDeliveryViewReportsMissingOwnerAsFailure(t *testing.T) {
	server := NewServer(&fakeReader{err: errors.New("serve unavailable")}, allowedIdentity())
	request := sessionRequest(http.MethodGet, "/api/v2/delivery/ws-1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", response.Code)
	}
	if !strings.Contains(response.Body.String(), "owner_read_failed") {
		t.Fatalf("missing owner did not surface as an owner read failure: %s", response.Body.String())
	}
}

func TestOperationRoutingRejectsUnknownOwner(t *testing.T) {
	server := NewServer(resolvedReader(), allowedIdentity())
	request := sessionRequest(http.MethodGet, "/api/v2/operations/not-an-owner/op-1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", response.Code)
	}
	if !strings.Contains(response.Body.String(), "unknown_owner") {
		t.Fatalf("unknown owner was not rejected: %s", response.Body.String())
	}
}

func TestOperationRoutingUsesNamedOwner(t *testing.T) {
	reader := resolvedReader()
	reader.operation = &api.Operation{OperationId: "op-1", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_CAPABILITY}
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_CAPABILITY
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION, Id: ptr("op-1")}
	server := NewServer(reader, identity)
	request := sessionRequest(http.MethodGet, "/api/v2/operations/capability/op-1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func ptr[T any](value T) *T { return &value }

func TestDeliveryRequiresABrowserSession(t *testing.T) {
	server := NewServer(resolvedReader(), allowedIdentity())
	request := httptest.NewRequest(http.MethodGet, "/api/v2/delivery/ws-1", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "session_required") {
		t.Fatalf("a missing session was not reported: %s", response.Body.String())
	}
}

func TestDeliveryRequiresCloudIdentityAuthorization(t *testing.T) {
	identity := allowedIdentity()
	identity.decision = &api.AuthorizationDecision{
		// A valid session with no explicit CloudIdentity allow decision must not read
		// another owner's facts: service identity is not user authorization.
		Result: api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED,
		Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY,
	}
	server := NewServer(resolvedReader(), identity)
	request := sessionRequest(http.MethodGet, "/api/v2/delivery/ws-1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "authorization_required") {
		t.Fatalf("a denied action was not reported as an authorization failure: %s", response.Body.String())
	}
}

func TestDeliveryRejectsAuthorizationFromAnotherIssuer(t *testing.T) {
	identity := allowedIdentity()
	identity.decision.Issuer = api.AuthorizationIssuer_AUTHORIZATION_ISSUER_UNSPECIFIED
	server := NewServer(resolvedReader(), identity)
	request := sessionRequest(http.MethodGet, "/api/v2/delivery/ws-1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", response.Code, response.Body.String())
	}
}

func TestOperationRoutingAcceptsTheServeOwner(t *testing.T) {
	reader := resolvedReader()
	reader.operation = &api.Operation{OperationId: "op-1", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE}
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_SERVE
	identity.decision.Resource = &api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION, Id: ptr("op-1")}
	server := NewServer(reader, identity)
	request := sessionRequest(http.MethodGet, "/api/v2/operations/serve/op-1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestAuthorizationRequestCarriesTheSessionScope(t *testing.T) {
	identity := allowedIdentity()
	server := NewServer(resolvedReader(), identity)
	request := sessionRequest(http.MethodGet, "/api/v2/delivery/ws-1")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if len(identity.requests) != 1 {
		t.Fatalf("authorization requests = %d, want 1", len(identity.requests))
	}
	sent := identity.requests[0]
	if sent.GetActorId() != "actor-1" {
		t.Fatalf("authorization actor = %q, want the session actor", sent.GetActorId())
	}
	if sent.GetScope().GetTenant().GetTenantId() != "tenant-1" {
		t.Fatalf("authorization scope = %+v, want the session tenant", sent.GetScope())
	}
	if sent.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE {
		t.Fatalf("authorization audience = %v, want workspace", sent.GetAudienceOwner())
	}
}
