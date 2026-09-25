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
)

type workspaceProbe struct {
	api.WorkspaceProductServiceClient
	created *api.CreateWorkspaceRpcRequest
	listed  *api.ListWorkspacesRpcRequest
	read    *api.GetWorkspaceRpcRequest
	err     error
}

func (p *workspaceProbe) CreateWorkspace(_ context.Context, r *api.CreateWorkspaceRpcRequest, _ ...grpc.CallOption) (*api.Operation, error) {
	p.created = r
	return &api.Operation{OperationId: "op-1", ResourceId: "ws-1", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_CREATE_WORKSPACE, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_ADMISSION, RequestId: r.Context.RequestId, CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now(), Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_ACCEPTED, PollAfterSeconds: proto.Int32(3)}, p.err
}
func (p *workspaceProbe) ListWorkspaces(_ context.Context, r *api.ListWorkspacesRpcRequest, _ ...grpc.CallOption) (*api.WorkspacePage, error) {
	p.listed = r
	return &api.WorkspacePage{Items: []*api.Workspace{}}, p.err
}
func (p *workspaceProbe) GetWorkspace(_ context.Context, r *api.GetWorkspaceRpcRequest, _ ...grpc.CallOption) (*api.Workspace, error) {
	p.read = r
	return &api.Workspace{Id: r.WorkspaceId, Status: api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_PROVISIONING}, p.err
}

type workspaceReader struct {
	*fakeReader
	client api.WorkspaceProductServiceClient
}

func (r workspaceReader) WorkspaceClient() api.WorkspaceProductServiceClient { return r.client }
func workspaceIdentity(action api.AuthorizationActionEnum, id string) *fakeIdentity {
	i := allowedIdentity()
	i.decision.Action = action
	i.decision.Resource.Id = nil
	if id != "" {
		i.decision.Resource.Id = proto.String(id)
	}
	i.decision.AuthorizationContextId = proto.String("workspace-authorization")
	return i
}
func workspaceWrite() *http.Request {
	r := sessionRequest(http.MethodPost, "/api/v2/workspaces")
	r.Body = io.NopCloser(strings.NewReader(`{"name":"Research","quoteId":"quote-1","renewalMode":"manual"}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", "csrf-1")
	r.Header.Set("Idempotency-Key", "create-workspace-1")
	return r
}
func TestWorkspaceProcessRoutePreservesAcceptedOwnerResponse(t *testing.T) {
	p := &workspaceProbe{}
	i := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE, "")
	h := NewServer(workspaceReader{fakeReader: resolvedReader(), client: p}, i).Handler()
	r := workspaceWrite()
	r.Header.Set("X-OPL-Actor", "forged")
	r.Header.Set("X-OPL-Tenant", "foreign")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 || w.Header().Get("Retry-After") != "3" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	c := p.created.GetContext()
	if c.GetActorId() != "actor-1" || c.GetScope().GetTenant().GetTenantId() != "tenant-1" || c.GetIdempotencyKey() != "create-workspace-1" || c.GetAuthorizationContextId() != "workspace-authorization" || p.created.GetBody().GetQuoteId() != "quote-1" {
		t.Fatalf("caller not preserved: %v", p.created)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "accepted" || body["resourceId"] != "ws-1" {
		t.Fatalf("owner status rewritten: %v", body)
	}
}
func TestWorkspaceWriteGuardsNeverReachOwner(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request, *fakeIdentity)
		want   int
	}{
		{"no session", func(r *http.Request, _ *fakeIdentity) { r.Header.Del("Cookie") }, 401},
		{"csrf", func(r *http.Request, _ *fakeIdentity) { r.Header.Del("X-CSRF-Token") }, 403},
		{"idempotency", func(r *http.Request, _ *fakeIdentity) { r.Header.Del("Idempotency-Key") }, 400},
		{"cross origin", func(r *http.Request, _ *fakeIdentity) { r.Header.Set("Origin", "https://foreign.test") }, 403},
		{"denied", func(_ *http.Request, i *fakeIdentity) {
			i.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
		}, 403},
		{"forged body", func(r *http.Request, _ *fakeIdentity) {
			r.Body = io.NopCloser(strings.NewReader(`{"name":"X","quoteId":"q","renewalMode":"manual","tenantId":"foreign"}`))
		}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &workspaceProbe{}
			i := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_CREATEWORKSPACE, "")
			r := workspaceWrite()
			tc.mutate(r, i)
			w := httptest.NewRecorder()
			NewWorkspaceHandler(p, i).ServeHTTP(w, r)
			if w.Code != tc.want || p.created != nil {
				t.Fatalf("status=%d owner=%v body=%s", w.Code, p.created, w.Body.String())
			}
		})
	}
}
func TestWorkspaceReadPaginationAndFailure(t *testing.T) {
	for _, limit := range []string{"", "1", "100", "0", "101", "bad", "1.5"} {
		t.Run("limit="+limit, func(t *testing.T) {
			p := &workspaceProbe{}
			i := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTWORKSPACES, "")
			path := "/api/v2/workspaces?cursor=next"
			if limit != "" {
				path += "&limit=" + limit
			}
			w := httptest.NewRecorder()
			NewWorkspaceHandler(p, i).ServeHTTP(w, sessionRequest("GET", path))
			invalid := limit == "0" || limit == "101" || limit == "bad" || limit == "1.5"
			if invalid {
				if w.Code != 400 || p.listed != nil {
					t.Fatalf("invalid limit reached owner: %d %v", w.Code, p.listed)
				}
				return
			}
			if w.Code != 200 || p.listed.GetQueryCursor() != "next" || p.listed.GetQueryLimit() < 1 {
				t.Fatalf("pagination lost: %d %v", w.Code, p.listed)
			}
			if limit == "" && p.listed.GetQueryLimit() != 25 {
				t.Fatal("default limit not25")
			}
		})
	}
	p := &workspaceProbe{err: status.Error(codes.NotFound, "absent")}
	i := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE, "ws-1")
	w := httptest.NewRecorder()
	NewWorkspaceHandler(p, i).ServeHTTP(w, sessionRequest("GET", "/api/v2/workspaces/ws-1"))
	if w.Code != 404 || p.read.GetWorkspaceId() != "ws-1" {
		t.Fatalf("readback status=%d request=%v", w.Code, p.read)
	}
}
