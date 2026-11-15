package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
)

// uploadCompletionProbe is the Capability owner. Completing an upload
// publishes the package version and names it in the operation.
type uploadCompletionProbe struct {
	api.CapabilityProductServiceClient
	completed int
}

func (p *uploadCompletionProbe) CompleteUpload(_ context.Context, _ *api.CompleteUploadRpcRequest, _ ...grpc.CallOption) (*api.Operation, error) {
	p.completed++
	return &api.Operation{
		OperationId: "op-1",
		Owner:       api.OperationOwnerEnum_OPERATION_OWNER_ENUM_CAPABILITY,
		Kind:        api.OperationKindEnum_OPERATION_KIND_ENUM_COMPLETE_UPLOAD,
		ResourceId:  "pv-1",
		Status:      api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED,
		Stage:       api.OperationStageEnum_OPERATION_STAGE_ENUM_UPLOAD_VERIFICATION,
	}, nil
}

// buildPolicyProbe is the Runtime Control owner: the installation's effective
// build policy, the pair of runtime and WebUI versions a new build uses.
type buildPolicyProbe struct {
	api.RuntimeControlProductServiceClient
	policy *api.BuildRuntimePolicy
	err    error
}

func (p *buildPolicyProbe) GetBuildRuntimePolicy(_ context.Context, _ *api.GetBuildRuntimePolicyRpcRequest, _ ...grpc.CallOption) (*api.BuildRuntimePolicy, error) {
	if p.err != nil {
		return nil, p.err
	}
	return p.policy, nil
}

// buildOwnerProbe is the Build owner. It keeps one job per idempotency key,
// the way the owner itself resolves a replayed command.
type buildOwnerProbe struct {
	api.BuildProductServiceClient
	jobs     map[string]*api.BuildJob
	requests []*api.CreateBuildRpcRequest
	err      error
}

func (p *buildOwnerProbe) CreateBuild(_ context.Context, r *api.CreateBuildRpcRequest, _ ...grpc.CallOption) (*api.BuildJob, error) {
	p.requests = append(p.requests, r)
	if p.err != nil {
		return nil, p.err
	}
	key := r.GetContext().GetIdempotencyKey()
	if job, ok := p.jobs[key]; ok {
		return job, nil
	}
	job := &api.BuildJob{Id: "build-1", PackageVersionId: r.GetBody().GetPackageVersionId()}
	p.jobs[key] = job
	return job, nil
}

func autoBuildFixture(t *testing.T, policy *buildPolicyProbe, build *buildOwnerProbe) (http.Handler, *fakeIdentity) {
	t.Helper()
	identity := allowedIdentity()
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEUPLOAD] =
		allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_CAPABILITY, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_COMPLETEUPLOAD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_PACKAGE, "upload-1")
	identity.decisions[api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD] =
		allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_BUILD, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_VERSION, "pv-1")
	capability := &uploadCompletionProbe{}
	return NewPublisherHandler(capability, policy, build, identity), identity
}

func uploadCompletionRequest() *http.Request {
	request := sessionRequest("POST", "/api/v2/uploads/upload-1/complete")
	request.Body = io.NopCloser(strings.NewReader(`{"parts":[]}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", "csrf-1")
	request.Header.Set("Idempotency-Key", "stable-command")
	return request
}

func publicError(t *testing.T, response *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	body := map[string]any{}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("response body is not the public error shape: %v (%s)", err, response.Body.String())
	}
	code, _ := body["code"].(string)
	message, _ := body["message"].(string)
	return code, message
}

// The upload publishes a package version, and that package version is built
// with the installation's effective build policy. The caller never asks for a
// build: the upload is the trigger.
func TestUploadCompletionStartsTheAutomaticBuild(t *testing.T) {
	policy := &buildPolicyProbe{policy: &api.BuildRuntimePolicy{
		Id:                    "policy-1",
		RuntimeVersionId:      "runtime-1",
		DefaultWebuiVersionId: ptr("webui-1"),
		PolicyVersion:         "1",
	}}
	build := &buildOwnerProbe{jobs: map[string]*api.BuildJob{}}
	handler, identity := autoBuildFixture(t, policy, build)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, uploadCompletionRequest())
	if response.Code != 202 {
		t.Fatalf("upload completion=%d %s", response.Code, response.Body.String())
	}
	if len(build.requests) != 1 {
		t.Fatalf("build calls=%d, want 1", len(build.requests))
	}
	request := build.requests[0]
	if request.GetBody().GetPackageVersionId() != "pv-1" || request.GetBody().GetRuntimeVersionId() != "runtime-1" || request.GetBody().GetWebuiVersionId() != "webui-1" {
		t.Fatalf("the build was asked for %v", request.GetBody())
	}
	// The Build owner authorizes the actor itself, so the automatic build
	// carries a Build owner decision and not the upload's Capability one.
	authorized := false
	for _, asked := range identity.requests {
		if asked.GetAction() == api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEBUILD {
			authorized = true
			if asked.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_BUILD {
				t.Fatalf("the build was authorized for %v, want the Build owner", asked.GetAudienceOwner())
			}
			if asked.GetResource().GetId() != "pv-1" {
				t.Fatalf("the build was authorized for %v, want the uploaded package version", asked.GetResource().GetId())
			}
		}
	}
	if !authorized {
		t.Fatal("the automatic build was never authorized for the Build owner")
	}
}

// Without an effective build policy nothing can be built, and the caller has
// to learn that: the package version is uploaded, and a silent skip would
// leave it unbuilt with nothing to act on.
func TestUploadCompletionFailsWithoutAnEffectiveBuildPolicy(t *testing.T) {
	policy := &buildPolicyProbe{err: status.Error(codes.NotFound, "no build policy")}
	build := &buildOwnerProbe{jobs: map[string]*api.BuildJob{}}
	handler, _ := autoBuildFixture(t, policy, build)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, uploadCompletionRequest())
	if response.Code != 404 {
		t.Fatalf("upload completion=%d %s", response.Code, response.Body.String())
	}
	code, message := publicError(t, response)
	if code != "BUILD_POLICY_MISSING" || !strings.Contains(message, "build policy") {
		t.Fatalf("the caller was told %q %q", code, message)
	}
	if len(build.requests) != 0 || len(build.jobs) != 0 {
		t.Fatalf("a build was started without a policy: %d calls, %d jobs", len(build.requests), len(build.jobs))
	}
}

// A policy that names no runtime or no WebUI version cannot build either.
func TestUploadCompletionFailsOnAnIncompleteBuildPolicy(t *testing.T) {
	policy := &buildPolicyProbe{policy: &api.BuildRuntimePolicy{Id: "policy-1", RuntimeVersionId: "runtime-1"}}
	build := &buildOwnerProbe{jobs: map[string]*api.BuildJob{}}
	handler, _ := autoBuildFixture(t, policy, build)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, uploadCompletionRequest())
	if response.Code != 409 {
		t.Fatalf("upload completion=%d %s", response.Code, response.Body.String())
	}
	code, message := publicError(t, response)
	if code != "BUILD_POLICY_INCOMPLETE" || !strings.Contains(message, "WebUI") {
		t.Fatalf("the caller was told %q %q", code, message)
	}
	if len(build.jobs) != 0 {
		t.Fatalf("a build was started from an incomplete policy: %d jobs", len(build.jobs))
	}
}

// A build the owner refuses carries its own reason, such as a runtime version
// that is not approved. The caller sees that reason, and no job is created.
func TestUploadCompletionReportsWhyTheBuildDidNotStart(t *testing.T) {
	policy := &buildPolicyProbe{policy: &api.BuildRuntimePolicy{Id: "policy-1", RuntimeVersionId: "runtime-1", DefaultWebuiVersionId: ptr("webui-1")}}
	build := &buildOwnerProbe{jobs: map[string]*api.BuildJob{}, err: status.Error(codes.FailedPrecondition, "Runtime version is not approved")}
	handler, _ := autoBuildFixture(t, policy, build)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, uploadCompletionRequest())
	if response.Code != 422 {
		t.Fatalf("upload completion=%d %s", response.Code, response.Body.String())
	}
	code, message := publicError(t, response)
	if code != "BUILD_NOT_STARTED" || message != "Runtime version is not approved" {
		t.Fatalf("the caller was told %q %q", code, message)
	}
	if len(build.jobs) != 0 {
		t.Fatalf("a refused build created %d jobs", len(build.jobs))
	}
}

// The same package version is built once. The automatic build carries its own
// idempotency key, derived from the package version, so a replayed upload
// resolves the same job instead of allocating a second one.
func TestUploadCompletionBuildsOnePackageVersionOnce(t *testing.T) {
	policy := &buildPolicyProbe{policy: &api.BuildRuntimePolicy{Id: "policy-1", RuntimeVersionId: "runtime-1", DefaultWebuiVersionId: ptr("webui-1")}}
	build := &buildOwnerProbe{jobs: map[string]*api.BuildJob{}}
	handler, _ := autoBuildFixture(t, policy, build)

	for range 2 {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, uploadCompletionRequest())
		if response.Code != 202 {
			t.Fatalf("upload completion=%d %s", response.Code, response.Body.String())
		}
	}
	if len(build.jobs) != 1 {
		t.Fatalf("build jobs=%d, want 1", len(build.jobs))
	}
	if len(build.requests) != 2 {
		t.Fatalf("build calls=%d, want 2", len(build.requests))
	}
	key := build.requests[0].GetContext().GetIdempotencyKey()
	if key != "auto-build:pv-1" {
		t.Fatalf("the automatic build used the key %q", key)
	}
	if build.requests[1].GetContext().GetIdempotencyKey() != key {
		t.Fatal("the replayed upload used another idempotency key")
	}
	if key == "stable-command" {
		t.Fatal("the automatic build reused the caller's idempotency key")
	}
}
