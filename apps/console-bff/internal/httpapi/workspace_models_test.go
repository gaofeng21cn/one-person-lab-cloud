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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// workspaceModelsProbe answers the Workspace owner's model-configuration read
// and update. It records the exact request it received so a test can prove the
// BFF forwarded the caller's own request instead of reimplementing it.
type workspaceModelsProbe struct {
	api.WorkspaceProductServiceClient
	configuration *api.ModelConfiguration
	operation     *api.Operation
	err           error

	read   *api.GetWorkspaceModelsRpcRequest
	update *api.UpdateWorkspaceModelsRpcRequest
}

func (p *workspaceModelsProbe) GetWorkspaceModels(_ context.Context, request *api.GetWorkspaceModelsRpcRequest, _ ...grpc.CallOption) (*api.ModelConfiguration, error) {
	p.read = request
	return p.configuration, p.err
}

func (p *workspaceModelsProbe) UpdateWorkspaceModels(_ context.Context, request *api.UpdateWorkspaceModelsRpcRequest, _ ...grpc.CallOption) (*api.Operation, error) {
	p.update = request
	return p.operation, p.err
}

func modelsConfiguration() *api.ModelConfiguration {
	return &api.ModelConfiguration{
		WorkspaceId:    "ws-1",
		Version:        4,
		AppliedVersion: proto.Int64(4),
		Status:         api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_APPLIED,
		Selections:     []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}},
		OperationId:    proto.String("op-models-1"),
		UpdatedAt:      timestamppb.Now(),
	}
}

func modelsUpdateRequest(body string) *http.Request {
	request := sessionRequest(http.MethodPut, "/api/v2/workspaces/ws-1/models")
	if body != "" {
		request.Body = io.NopCloser(strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("X-CSRF-Token", "csrf-1")
	request.Header.Set("Idempotency-Key", "models-update-1")
	return request
}

// TestWorkspaceModelsReadForwardsTheCallersOwnRequest proves the read route asks
// the Workspace owner for exactly this workspace under the session's own
// identity, and publishes the owner's own configuration facts unchanged.
func TestWorkspaceModelsReadForwardsTheCallersOwnRequest(t *testing.T) {
	probe := &workspaceModelsProbe{configuration: modelsConfiguration()}
	identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEMODELS, "ws-1")
	response := httptest.NewRecorder()
	NewWorkspaceHandler(probe, identity).ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1/models"))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["workspaceId"] != "ws-1" || body["version"] != "4" || body["appliedVersion"] != "4" ||
		body["status"] != "applied" || body["operationId"] != "op-models-1" {
		t.Fatalf("owner facts rewritten: %v", body)
	}
	selections, ok := body["selections"].([]any)
	if !ok || len(selections) != 1 || selections[0].(map[string]any)["slot"] != "chat" || selections[0].(map[string]any)["modelId"] != "model-a" {
		t.Fatalf("selections rewritten: %v", body["selections"])
	}
	if probe.read == nil || probe.read.GetWorkspaceId() != "ws-1" {
		t.Fatalf("owner read request: %v", probe.read)
	}
	call := probe.read.GetContext()
	if call.GetActorId() != "actor-1" || call.GetScope().GetTenant().GetTenantId() != "tenant-1" ||
		call.GetSessionId() != owneridentity.SessionReference("session-1") || call.GetRequestId() == "" ||
		call.GetAuthorizationContextId() != "workspace-authorization" {
		t.Fatalf("caller not preserved: %v", call)
	}
	if len(identity.requests) != 1 {
		t.Fatalf("authorization requests = %d", len(identity.requests))
	}
	authorization := identity.requests[0]
	if authorization.GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEMODELS ||
		authorization.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE ||
		authorization.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE ||
		authorization.GetResource().GetId() != "ws-1" {
		t.Fatalf("authorization not bound to the owner action and resource: %v", authorization)
	}
}

// TestWorkspaceModelsUpdateForwardsTheOriginalOperation proves the update route
// carries the expected version, the selections and the caller's idempotency key
// to the Workspace owner and answers the contract's accepted status without
// pretending the reload finished.
func TestWorkspaceModelsUpdateForwardsTheOriginalOperation(t *testing.T) {
	probe := &workspaceModelsProbe{operation: &api.Operation{
		OperationId: "op-models-2", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE,
		Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_UPDATE_MODELS, ResourceId: "ws-1",
		Status:    api.OperationStatusEnum_OPERATION_STATUS_ENUM_ACCEPTED,
		Stage:     api.OperationStageEnum_OPERATION_STAGE_ENUM_ADMISSION,
		RequestId: "request-1", PollAfterSeconds: proto.Int32(2),
		CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now(),
	}}
	identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEMODELS, "ws-1")
	response := httptest.NewRecorder()
	NewWorkspaceHandler(probe, identity).ServeHTTP(response,
		modelsUpdateRequest(`{"expectedVersion":"4","selections":[{"slot":"chat","modelId":"model-b"}]}`))

	if response.Code != http.StatusAccepted || response.Header().Get("Retry-After") != "2" {
		t.Fatalf("status = %d retry-after = %q body = %s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["operationId"] != "op-models-2" || body["status"] != "accepted" || body["kind"] != "update_models" {
		t.Fatalf("operation rewritten: %v", body)
	}
	if probe.update == nil || probe.update.GetWorkspaceId() != "ws-1" ||
		probe.update.GetBody().GetExpectedVersion() != 4 ||
		len(probe.update.GetBody().GetSelections()) != 1 ||
		probe.update.GetBody().GetSelections()[0].GetModelId() != "model-b" {
		t.Fatalf("owner update request: %v", probe.update)
	}
	call := probe.update.GetContext()
	if call.GetIdempotencyKey() != "models-update-1" || call.GetActorId() != "actor-1" ||
		call.GetAuthorizationContextId() != "workspace-authorization" {
		t.Fatalf("update caller not preserved: %v", call)
	}
	if len(identity.requests) != 1 ||
		identity.requests[0].GetAction() != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEMODELS ||
		identity.requests[0].GetResource().GetId() != "ws-1" {
		t.Fatalf("update authorization: %v", identity.requests)
	}
}

// TestWorkspaceModelsWriteGuardsNeverReachOwner covers the canonical write guard
// for the update route, plus the cross-tenant case: a decision CloudIdentity
// issued for another workspace never becomes an update for this one.
func TestWorkspaceModelsWriteGuardsNeverReachOwner(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*http.Request, *fakeIdentity)
		want   int
	}{
		{"no session", func(r *http.Request, _ *fakeIdentity) { r.Header.Del("Cookie") }, 401},
		{"csrf", func(r *http.Request, _ *fakeIdentity) { r.Header.Del("X-CSRF-Token") }, 403},
		{"idempotency", func(r *http.Request, _ *fakeIdentity) { r.Header.Del("Idempotency-Key") }, 400},
		{"cross origin", func(r *http.Request, _ *fakeIdentity) { r.Header.Set("Origin", "https://foreign.test") }, 403},
		{"denied", func(_ *http.Request, identity *fakeIdentity) {
			identity.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
		}, 403},
		{"cross tenant decision", func(_ *http.Request, identity *fakeIdentity) {
			identity.decision.Resource.Id = proto.String("ws-other")
		}, 403},
		{"forged body", func(r *http.Request, _ *fakeIdentity) {
			r.Body = io.NopCloser(strings.NewReader(`{"expectedVersion":"4","selections":[],"tenantId":"foreign"}`))
		}, 400},
		{"missing body", func(r *http.Request, _ *fakeIdentity) {
			r.Body = http.NoBody
			r.Header.Del("Content-Type")
		}, 415},
	} {
		t.Run(test.name, func(t *testing.T) {
			probe := &workspaceModelsProbe{operation: &api.Operation{OperationId: "op-models-2"}}
			identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEMODELS, "ws-1")
			request := modelsUpdateRequest(`{"expectedVersion":"4","selections":[{"slot":"chat","modelId":"model-b"}]}`)
			test.mutate(request, identity)
			response := httptest.NewRecorder()
			NewWorkspaceHandler(probe, identity).ServeHTTP(response, request)
			if response.Code != test.want || probe.update != nil {
				t.Fatalf("status = %d, want %d, owner = %v, body = %s", response.Code, test.want, probe.update, response.Body.String())
			}
		})
	}
}

// TestWorkspaceModelsReadDenialDoesNotReachOwner proves a refused read is an
// authorization decision, not an empty configuration.
func TestWorkspaceModelsReadDenialDoesNotReachOwner(t *testing.T) {
	probe := &workspaceModelsProbe{configuration: modelsConfiguration()}
	identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEMODELS, "ws-other")
	response := httptest.NewRecorder()
	NewWorkspaceHandler(probe, identity).ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1/models"))

	if response.Code != http.StatusForbidden || probe.read != nil {
		t.Fatalf("status = %d owner = %v body = %s", response.Code, probe.read, response.Body.String())
	}
}

// TestWorkspaceModelsOwnerRefusalKeepsItsTypedCode proves the owner's own typed
// refusal (here: a model outside the frozen publisher contract) reaches the
// Console with the contract's status and code instead of a generic failure.
func TestWorkspaceModelsOwnerRefusalKeepsItsTypedCode(t *testing.T) {
	probe := &workspaceModelsProbe{err: owneridentity.WithErrorCode(
		status.Error(codes.InvalidArgument, "model outside the frozen contract"),
		api.ErrorCodeEnum_ERROR_CODE_ENUM_MODEL_NOT_ALLOWED)}
	identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEMODELS, "ws-1")
	response := httptest.NewRecorder()
	NewWorkspaceHandler(probe, identity).ServeHTTP(response,
		modelsUpdateRequest(`{"expectedVersion":"4","selections":[{"slot":"chat","modelId":"model-z"}]}`))

	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["code"] != "MODEL_NOT_ALLOWED" {
		t.Fatalf("typed code lost: %v", body)
	}
}

// TestWorkspaceModelsOperationRefreshReadsTheOriginalOperation proves a Console
// reload can recover a submitted model update from the original Workspace
// operation instead of submitting the command again.
func TestWorkspaceModelsOperationRefreshReadsTheOriginalOperation(t *testing.T) {
	reader := resolvedReader()
	reader.operation = &api.Operation{
		OperationId: "op-models-2", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE,
		Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_UPDATE_MODELS, ResourceId: "ws-1",
		Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_RUNNING,
		Stage:  api.OperationStageEnum_OPERATION_STAGE_ENUM_ADMISSION,
	}
	identity := allowedIdentity()
	identity.decision.Action = api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION
	identity.decision.AudienceOwner = api.OwnerEnum_OWNER_ENUM_WORKSPACE
	identity.decision.Resource = &api.AuthorizationResource{
		Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION, Id: proto.String("op-models-2")}
	response := httptest.NewRecorder()
	NewServer(reader, identity).Handler().ServeHTTP(response,
		sessionRequest(http.MethodGet, "/api/v2/operations/workspace/op-models-2"))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d body = %s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["operationId"] != "op-models-2" || body["status"] != "running" || body["kind"] != "update_models" {
		t.Fatalf("operation readback: %v", body)
	}
}
