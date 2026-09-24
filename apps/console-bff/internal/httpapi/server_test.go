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
	server := NewServer(resolvedReader())
	request := httptest.NewRequest(http.MethodGet, "/api/v2/delivery/ws-1", nil)
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
	server := NewServer(&fakeReader{err: errors.New("serve unavailable")})
	request := httptest.NewRequest(http.MethodGet, "/api/v2/delivery/ws-1", nil)
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
	server := NewServer(resolvedReader())
	request := httptest.NewRequest(http.MethodGet, "/api/v2/operations/not-an-owner/op-1", nil)
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
	server := NewServer(reader)
	request := httptest.NewRequest(http.MethodGet, "/api/v2/operations/capability/op-1", nil)
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func ptr[T any](value T) *T { return &value }
