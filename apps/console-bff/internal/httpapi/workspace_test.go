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
	created      *api.CreateWorkspaceRpcRequest
	listed       *api.ListWorkspacesRpcRequest
	read         *api.GetWorkspaceRpcRequest
	deleted      *api.DeleteWorkspaceRpcRequest
	readDeletion *api.GetWorkspaceDeletionRpcRequest
	deletion     *api.WorkspaceDeletion
	deletionErr  error
	readErr      error
	err          error
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
	err := p.readErr
	if err == nil {
		err = p.err
	}
	return &api.Workspace{Id: r.WorkspaceId, Status: api.WorkspaceStatusEnum_WORKSPACE_STATUS_ENUM_PROVISIONING}, err
}
func (p *workspaceProbe) DeleteWorkspace(_ context.Context, r *api.DeleteWorkspaceRpcRequest, _ ...grpc.CallOption) (*api.Operation, error) {
	p.deleted = r
	if p.err != nil {
		return nil, p.err
	}
	return &api.Operation{OperationId: "op-delete-1", ResourceId: "ws-1", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_DELETE_WORKSPACE, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_RETIREMENT, Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_ACCEPTED, RequestId: r.GetContext().GetRequestId(), CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now(), PollAfterSeconds: proto.Int32(3)}, nil
}
func (p *workspaceProbe) GetWorkspaceDeletion(_ context.Context, r *api.GetWorkspaceDeletionRpcRequest, _ ...grpc.CallOption) (*api.WorkspaceDeletion, error) {
	p.readDeletion = r
	err := p.deletionErr
	if err == nil {
		err = p.err
	}
	return p.deletion, err
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
	r.Body = io.NopCloser(strings.NewReader(`{"name":"Research","quoteId":"quote-1","renewalMode":"manual","automaticRenewalConsent":false}`))
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
	if c.GetActorId() != "actor-1" || c.GetScope().GetTenant().GetTenantId() != "tenant-1" || c.GetIdempotencyKey() != "create-workspace-1" || c.GetAuthorizationContextId() != "workspace-authorization" || p.created.GetBody().GetQuoteId() != "quote-1" || p.created.GetBody().GetRenewalMode() != api.CreateWorkspaceRequestRenewalModeEnum_CREATE_WORKSPACE_REQUEST_RENEWAL_MODE_ENUM_MANUAL || p.created.GetBody().GetAutomaticRenewalConsent() {
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

// TestWorkspaceDeletionCommandReachesTheWorkspaceOwner proves the browser DELETE
// route forwards the caller's exact confirmation and destruction
// acknowledgement to the Workspace owner and publishes the owner's own accepted
// operation instead of reporting the deletion as finished.
func TestWorkspaceDeletionCommandReachesTheWorkspaceOwner(t *testing.T) {
	p := &workspaceProbe{}
	i := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE, "ws-1")
	r := sessionRequest(http.MethodDelete, "/api/v2/workspaces/ws-1")
	r.Body = io.NopCloser(strings.NewReader(`{"confirmationName":"Research","acknowledgeDataDestruction":true}`))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", "csrf-1")
	r.Header.Set("Idempotency-Key", "delete-workspace-1")
	w := httptest.NewRecorder()
	NewWorkspaceHandler(p, i).ServeHTTP(w, r)

	if w.Code != http.StatusAccepted || w.Header().Get("Retry-After") != "3" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if p.deleted == nil || p.deleted.GetWorkspaceId() != "ws-1" {
		t.Fatalf("owner command: %v", p.deleted)
	}
	body := p.deleted.GetBody()
	if body.GetConfirmationName() != "Research" || !body.GetAcknowledgeDataDestruction() {
		t.Fatalf("owner command body rewritten: %v", body)
	}
	call := p.deleted.GetContext()
	if call.GetActorId() != "actor-1" || call.GetScope().GetTenant().GetTenantId() != "tenant-1" ||
		call.GetIdempotencyKey() != "delete-workspace-1" || call.GetRequestId() == "" {
		t.Fatalf("caller not preserved: %v", call)
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["operationId"] != "op-delete-1" || response["status"] != "accepted" || response["kind"] != "delete_workspace" {
		t.Fatalf("operation rewritten: %v", response)
	}
}

// TestWorkspaceDeletionCommandRequiresTheWriteGuard proves a deletion with no
// live CSRF token or idempotency key never reaches the Workspace owner.
func TestWorkspaceDeletionCommandRequiresTheWriteGuard(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request)
		want   int
	}{
		{"csrf", func(r *http.Request) { r.Header.Del("X-CSRF-Token") }, 403},
		{"idempotency", func(r *http.Request) { r.Header.Del("Idempotency-Key") }, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &workspaceProbe{}
			i := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_DELETEWORKSPACE, "ws-1")
			r := sessionRequest(http.MethodDelete, "/api/v2/workspaces/ws-1")
			r.Body = io.NopCloser(strings.NewReader(`{"confirmationName":"Research","acknowledgeDataDestruction":true}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", "csrf-1")
			r.Header.Set("Idempotency-Key", "delete-workspace-1")
			tc.mutate(r)
			w := httptest.NewRecorder()
			NewWorkspaceHandler(p, i).ServeHTTP(w, r)
			if w.Code != tc.want || p.deleted != nil {
				t.Fatalf("status=%d owner=%v body=%s", w.Code, p.deleted, w.Body.String())
			}
		})
	}
}

// TestWorkspaceDeletionReadbackPreservesTheOwnerFacts proves the GET route
// publishes the canonical WorkspaceDeletion unchanged, so Console projects the
// owner's resource, data and refund facts instead of a BFF-invented state.
func TestWorkspaceDeletionReadbackPreservesTheOwnerFacts(t *testing.T) {
	p := &workspaceProbe{deletion: &api.WorkspaceDeletion{
		WorkspaceId:            "ws-1",
		OperationId:            "op-delete-1",
		ResourceDeletionStatus: api.WorkspaceDeletionResourceDeletionStatusEnum_WORKSPACE_DELETION_RESOURCE_DELETION_STATUS_ENUM_CONFIRMED,
		DataDeletionStatus:     api.WorkspaceDeletionDataDeletionStatusEnum_WORKSPACE_DELETION_DATA_DELETION_STATUS_ENUM_CONFIRMED,
		RefundOperationId:      proto.String("refund-1"),
		RefundStatus:           api.WorkspaceDeletionRefundStatusEnum_WORKSPACE_DELETION_REFUND_STATUS_ENUM_CONFIRMED,
		UpdatedAt:              timestamppb.Now(),
	}}
	i := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEDELETION, "ws-1")
	w := httptest.NewRecorder()
	NewWorkspaceHandler(p, i).ServeHTTP(w, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1/deletion"))

	if w.Code != http.StatusOK || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if p.readDeletion == nil || p.readDeletion.GetWorkspaceId() != "ws-1" {
		t.Fatalf("owner read: %v", p.readDeletion)
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["workspaceId"] != "ws-1" || response["operationId"] != "op-delete-1" ||
		response["resourceDeletionStatus"] != "confirmed" || response["dataDeletionStatus"] != "confirmed" ||
		response["refundStatus"] != "confirmed" || response["refundOperationId"] != "refund-1" {
		t.Fatalf("owner facts rewritten: %v", response)
	}
}

// TestWorkspaceDeletionReadbackReportsNoAcceptedDeletionAsNull proves the BFF
// preserves the owner's distinction between an existing Workspace with no
// accepted deletion and a missing Workspace.
func TestWorkspaceDeletionReadbackReportsNoAcceptedDeletionAsNull(t *testing.T) {
	p := &workspaceProbe{deletionErr: status.Error(codes.NotFound, "the Workspace has no accepted deletion")}
	i := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEDELETION, "ws-1")
	i.decisions = map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEDELETION: allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEDELETION, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws-1"),
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE:         allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws-1"),
	}
	w := httptest.NewRecorder()
	NewWorkspaceHandler(p, i).ServeHTTP(w, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1/deletion"))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != "null" || p.readDeletion == nil || p.read == nil {
		t.Fatalf("status=%d deletion=%v workspace=%v body=%s", w.Code, p.readDeletion, p.read, w.Body.String())
	}
}

func TestWorkspaceDeletionReadbackReportsMissingWorkspace(t *testing.T) {
	p := &workspaceProbe{
		deletionErr: status.Error(codes.NotFound, "the Workspace has no accepted deletion"),
		readErr:     status.Error(codes.NotFound, "Workspace order not found"),
	}
	i := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEDELETION, "ws-1")
	i.decisions = map[api.AuthorizationActionEnum]*api.AuthorizationDecision{
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEDELETION: allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEDELETION, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws-1"),
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE:         allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_WORKSPACE, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACE, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws-1"),
	}
	w := httptest.NewRecorder()
	NewWorkspaceHandler(p, i).ServeHTTP(w, sessionRequest(http.MethodGet, "/api/v2/workspaces/ws-1/deletion"))
	if w.Code != http.StatusNotFound || p.readDeletion == nil || p.read == nil {
		t.Fatalf("status=%d deletion=%v workspace=%v body=%s", w.Code, p.readDeletion, p.read, w.Body.String())
	}
}
