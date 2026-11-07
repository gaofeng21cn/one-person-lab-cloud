package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
)

type operatorOverviewReader struct {
	OperatorReader
	raw   []byte
	err   error
	calls int
}

func (r *operatorOverviewReader) OperatorOverview(context.Context, string) ([]byte, error) {
	r.calls++
	return r.raw, r.err
}

type operatorOverviewWorkspaceClient struct {
	api.WorkspaceProductServiceClient
	page     *api.OperatorWorkspacePage
	err      error
	requests []*api.ListOperatorWorkspacesRpcRequest
}

func (c *operatorOverviewWorkspaceClient) ListOperatorWorkspaces(_ context.Context, request *api.ListOperatorWorkspacesRpcRequest, _ ...grpc.CallOption) (*api.OperatorWorkspacePage, error) {
	c.requests = append(c.requests, proto.Clone(request).(*api.ListOperatorWorkspacesRpcRequest))
	return c.page, c.err
}

func operatorOverviewCallerContext(t *testing.T, identity *operatorIdentity) (context.Context, Caller) {
	t.Helper()
	caller := Caller{Session: &api.Session{ActorId: "operator-1"}, SessionID: "session-1"}
	return WithCaller(context.Background(), caller, "overview-request"), caller
}

func operatorOverviewResponse(t *testing.T, server *Server, identity *operatorIdentity) *httptest.ResponseRecorder {
	t.Helper()
	ctx, caller := operatorOverviewCallerContext(t, identity)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v2/operator/overview", nil)
	server.readOperatorOverview(response, request, ctx, caller)
	return response
}

func operatorOverviewCPEnvelope(data string) []byte {
	return []byte(`{"source":"control-plane","status":"available","available":true,"fetchedAt":"2026-11-07T00:00:00Z","sourceUpdatedAt":"2026-11-06T00:00:00Z","data":` + data + `}`)
}

func TestOperatorOverviewUsesWorkspaceAuthoritativeCountAndMarksResourcesUnavailable(t *testing.T) {
	identity := &operatorIdentity{}
	workspace := &operatorOverviewWorkspaceClient{page: &api.OperatorWorkspacePage{Page: 1, PageSize: 1, Total: 37}}
	server := &Server{
		operator:  &operatorOverviewReader{raw: operatorOverviewCPEnvelope(`{"accounts":{"total":9},"workspaces":{"total":999},"resources":{"total":88},"health":{"status":"ok"}}`)},
		identity:  identity,
		workspace: workspace,
	}

	response := operatorOverviewResponse(t, server, identity)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope["source"] != "control-plane+workspace" {
		t.Fatalf("source=%v", envelope["source"])
	}
	data := envelope["data"].(map[string]any)
	if got := data["accounts"].(map[string]any)["total"]; got != float64(9) {
		t.Fatalf("accounts provenance/data changed: %v", got)
	}
	workspaces := data["workspaces"].(map[string]any)
	if workspaces["source"] != "workspace" || workspaces["available"] != true || workspaces["data"].(map[string]any)["total"] != float64(37) {
		t.Fatalf("workspace count=%v", workspaces)
	}
	resources := data["resources"].(map[string]any)
	if resources["available"] != false || resources["reasonCode"] != "owner_aggregate_not_available" || resources["data"] != nil {
		t.Fatalf("resources=%v", resources)
	}
	if len(workspace.requests) != 1 || workspace.requests[0].Page != 1 || workspace.requests[0].PageSize != 1 {
		t.Fatalf("workspace requests=%v", workspace.requests)
	}
	if len(identity.calls) != 1 || identity.calls[0].Action != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORWORKSPACES || identity.calls[0].AudienceOwner != api.OwnerEnum_OWNER_ENUM_WORKSPACE {
		t.Fatalf("workspace authorization=%v", identity.calls)
	}
	if identity.calls[0].GetScope().GetPlatform() == nil || identity.calls[0].GetScope().GetTenant() != nil {
		t.Fatalf("workspace authorization scope=%v", identity.calls[0].GetScope())
	}
}

func TestOperatorOverviewNeverFallsBackToControlPlaneCountWhenWorkspaceUnavailable(t *testing.T) {
	for name, client := range map[string]*operatorOverviewWorkspaceClient{
		"unconfigured": nil,
		"unavailable":  {err: status.Error(codes.Unavailable, "workspace unavailable")},
		"deadline":     {err: status.Error(codes.DeadlineExceeded, "workspace deadline")},
	} {
		t.Run(name, func(t *testing.T) {
			identity := &operatorIdentity{}
			server := &Server{operator: &operatorOverviewReader{raw: operatorOverviewCPEnvelope(`{"workspaces":{"total":999}}`)}, identity: identity}
			if client != nil {
				server.workspace = client
			}
			response := operatorOverviewResponse(t, server, identity)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var envelope map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			workspace := envelope["data"].(map[string]any)["workspaces"].(map[string]any)
			if workspace["available"] != false || workspace["data"] != nil {
				t.Fatalf("workspace=%v", workspace)
			}
			if workspace["reasonCode"] == "" {
				t.Fatalf("workspace reason missing: %v", workspace)
			}
		})
	}
}

func TestOperatorOverviewPreservesControlPlaneUnavailableAndFailureSemantics(t *testing.T) {
	t.Run("valid unavailable is forwarded without workspace read", func(t *testing.T) {
		identity := &operatorIdentity{}
		workspace := &operatorOverviewWorkspaceClient{page: &api.OperatorWorkspacePage{Page: 1, PageSize: 1, Total: 10}}
		raw := []byte(`{"source":"control-plane","status":"unavailable","available":false,"reasonCode":"owner_unavailable","fetchedAt":"2026-11-07T00:00:00Z"}`)
		server := &Server{operator: &operatorOverviewReader{raw: raw}, identity: identity, workspace: workspace}
		response := operatorOverviewResponse(t, server, identity)
		if response.Code != http.StatusOK || response.Body.String() != string(raw) {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		if len(workspace.requests) != 0 {
			t.Fatalf("workspace was read: %v", workspace.requests)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		identity := &operatorIdentity{}
		reader := &operatorOverviewReader{err: status.Error(codes.Unavailable, "control plane unavailable")}
		server := &Server{operator: reader, identity: identity, workspace: &operatorOverviewWorkspaceClient{}}
		response := operatorOverviewResponse(t, server, identity)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("invalid envelope", func(t *testing.T) {
		identity := &operatorIdentity{}
		workspace := &operatorOverviewWorkspaceClient{}
		server := &Server{operator: &operatorOverviewReader{raw: []byte(`{"source":"wrong","status":"available","available":true,"fetchedAt":"2026-11-07T00:00:00Z","data":{}}`)}, identity: identity, workspace: workspace}
		response := operatorOverviewResponse(t, server, identity)
		if response.Code != http.StatusBadGateway {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
		if len(workspace.requests) != 0 {
			t.Fatalf("workspace was read: %v", workspace.requests)
		}
	})
}

func TestOperatorOverviewDoesNotHideWorkspaceAuthorizationFailure(t *testing.T) {
	identity := &operatorIdentity{denied: true}
	server := &Server{
		operator:  &operatorOverviewReader{raw: operatorOverviewCPEnvelope(`{"workspaces":{"total":999}}`)},
		identity:  identity,
		workspace: &operatorOverviewWorkspaceClient{page: &api.OperatorWorkspacePage{Page: 1, PageSize: 1, Total: 7}},
	}
	response := operatorOverviewResponse(t, server, identity)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOperatorOverviewRejectsInvalidWorkspacePageWithoutUsingCount(t *testing.T) {
	for name, page := range map[string]*api.OperatorWorkspacePage{
		"wrong page":         {Page: 2, PageSize: 1, Total: 1},
		"wrong size":         {Page: 1, PageSize: 2, Total: 1},
		"negative total":     {Page: 1, PageSize: 1, Total: -1},
		"unsafe total":       {Page: 1, PageSize: 1, Total: 9007199254740992},
		"item exceeds total": {Page: 1, PageSize: 1, Total: 0, Items: []*api.OperatorWorkspace{{Workspace: &api.Workspace{Id: "ws"}}}},
	} {
		t.Run(name, func(t *testing.T) {
			identity := &operatorIdentity{}
			server := &Server{operator: &operatorOverviewReader{raw: operatorOverviewCPEnvelope(`{"workspaces":{"total":999}}`)}, identity: identity, workspace: &operatorOverviewWorkspaceClient{page: page}}
			response := operatorOverviewResponse(t, server, identity)
			if response.Code != http.StatusBadGateway {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), `"total":999`) {
				t.Fatalf("invalid Workspace response leaked the retired count: %s", response.Body.String())
			}
		})
	}
}

func TestOperatorOverviewWorkspaceAuthorizationIsIndependentFromControlPlaneGate(t *testing.T) {
	identity := &operatorIdentity{}
	workspace := &operatorOverviewWorkspaceClient{page: &api.OperatorWorkspacePage{Page: 1, PageSize: 1, Total: 1}}
	server := &Server{operator: &operatorOverviewReader{raw: operatorOverviewCPEnvelope(`{"accounts":{"total":1}}`)}, identity: identity, workspace: workspace}
	ctx, caller := operatorOverviewCallerContext(t, identity)
	response := httptest.NewRecorder()
	server.readOperatorOverview(response, httptest.NewRequest(http.MethodGet, "/api/v2/operator/overview", nil), ctx, caller)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(identity.calls) != 1 {
		t.Fatalf("authorization calls=%d", len(identity.calls))
	}
	call := identity.calls[0]
	if call.Action != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORWORKSPACES || call.GetAuthorizationContextId() != "" {
		t.Fatalf("independent authorization request=%v", call)
	}
	if got := clients.CallContext(ctx).GetAuthorizationContextId(); got != "" {
		t.Fatalf("control-plane authorization context was mutated: %q", got)
	}
}

func TestOperatorOverviewReaderErrorIsNotSilentlyConverted(t *testing.T) {
	identity := &operatorIdentity{}
	reader := &operatorOverviewReader{err: errors.New("reader failed")}
	server := &Server{operator: reader, identity: identity}
	response := operatorOverviewResponse(t, server, identity)
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestOperatorOverviewHTTPRouteUsesIndependentOwnerCount(t *testing.T) {
	identity := &operatorIdentity{}
	workspace := &operatorOverviewWorkspaceClient{page: &api.OperatorWorkspacePage{Page: 1, PageSize: 1, Total: 37}}
	server := &Server{
		operator: &operatorOverviewReader{raw: operatorOverviewCPEnvelope(`{"workspaces":{"total":999},"resources":{"total":88}}`)},
		identity: identity, workspace: workspace,
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, sessionRequest(http.MethodGet, "/api/v2/operator/overview"))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	count := envelope["data"].(map[string]any)["workspaces"].(map[string]any)
	if count["source"] != "workspace" || count["data"].(map[string]any)["total"] != float64(37) {
		t.Fatalf("count=%v", count)
	}
	if len(identity.calls) != 2 || identity.calls[0].Action != operatorPrivilegeAction || identity.calls[1].Action != api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTOPERATORWORKSPACES {
		t.Fatalf("independent owner decisions=%v", identity.calls)
	}
}
