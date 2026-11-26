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
	"opl-cloud/packages/contracts/go/publicjson"
)

// renewalWorkspaceProbe records the one owner command the renewal route may
// publish. It embeds the owner client, so any second owner call the route
// would make is impossible: a route that priced, charged or read a period
// itself has no way to reach a client this probe does not implement.
type renewalWorkspaceProbe struct {
	api.WorkspaceProductServiceClient
	renewed   *api.RenewWorkspaceRpcRequest
	operation *api.Operation
	err       error
}

func (p *renewalWorkspaceProbe) RenewWorkspace(_ context.Context, r *api.RenewWorkspaceRpcRequest, _ ...grpc.CallOption) (*api.Operation, error) {
	p.renewed = r
	if p.err != nil {
		return nil, p.err
	}
	if p.operation != nil {
		return p.operation, nil
	}
	return &api.Operation{
		OperationId:      "op-renew-1",
		ResourceId:       r.GetWorkspaceId(),
		Owner:            api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE,
		Kind:             api.OperationKindEnum_OPERATION_KIND_ENUM_RENEW_WORKSPACE,
		Stage:            api.OperationStageEnum_OPERATION_STAGE_ENUM_QUOTE_BINDING,
		Status:           api.OperationStatusEnum_OPERATION_STATUS_ENUM_ACCEPTED,
		RequestId:        r.GetContext().GetRequestId(),
		CreatedAt:        timestamppb.Now(),
		UpdatedAt:        timestamppb.Now(),
		PollAfterSeconds: proto.Int32(3),
	}, nil
}

func renewalIdentity() *fakeIdentity {
	i := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_RENEWWORKSPACE, "ws-1")
	return i
}

func renewalWrite(body string) *http.Request {
	r := sessionRequest(http.MethodPost, "/api/v2/workspaces/ws-1/renew")
	r.Body = io.NopCloser(strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", "csrf-1")
	r.Header.Set("Idempotency-Key", "renew-workspace-1")
	return r
}

// TestWorkspaceRenewalCommandReachesTheWorkspaceOwner proves the browser renewal
// route forwards the caller's own renewal quote identifier and Workspace id to
// the Workspace owner and publishes the owner's accepted operation. The owner
// prices, charges and extends the period; the BFF publishes what the owner
// accepted and claims no renewal of its own.
func TestWorkspaceRenewalCommandReachesTheWorkspaceOwner(t *testing.T) {
	p := &renewalWorkspaceProbe{}
	w := httptest.NewRecorder()
	NewWorkspaceHandler(p, renewalIdentity()).ServeHTTP(w, renewalWrite(`{"quoteId":"quote-renew-1"}`))

	if w.Code != http.StatusAccepted || w.Header().Get("Retry-After") != "3" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("response: %d %v %s", w.Code, w.Header(), w.Body.String())
	}
	if p.renewed == nil {
		t.Fatal("the renewal command never reached the Workspace owner")
	}
	if p.renewed.GetWorkspaceId() != "ws-1" || p.renewed.GetBody().GetQuoteId() != "quote-renew-1" {
		t.Fatalf("owner command rewritten: %v", p.renewed)
	}
	call := p.renewed.GetContext()
	if call.GetActorId() != "actor-1" || call.GetScope().GetTenant().GetTenantId() != "tenant-1" ||
		call.GetIdempotencyKey() != "renew-workspace-1" || call.GetRequestId() == "" {
		t.Fatalf("caller not preserved: %v", call)
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["operationId"] != "op-renew-1" || response["status"] != "accepted" || response["kind"] != "renew_workspace" {
		t.Fatalf("owner operation rewritten: %v", response)
	}
}

// TestWorkspaceRenewalCommandPublishesOnlyTheOwnerOperation proves the BFF adds
// no renewal fact of its own: the published body is the owner's own operation
// projection, byte for byte, so no price, period or charge can appear in the
// response that the Workspace owner did not accept.
func TestWorkspaceRenewalCommandPublishesOnlyTheOwnerOperation(t *testing.T) {
	owner := &api.Operation{
		OperationId:      "op-renew-2",
		ResourceId:       "ws-1",
		Owner:            api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE,
		Kind:             api.OperationKindEnum_OPERATION_KIND_ENUM_RENEW_WORKSPACE,
		Stage:            api.OperationStageEnum_OPERATION_STAGE_ENUM_QUOTE_BINDING,
		Status:           api.OperationStatusEnum_OPERATION_STATUS_ENUM_ACCEPTED,
		RequestId:        "req-renew-2",
		CreatedAt:        timestamppb.Now(),
		UpdatedAt:        timestamppb.Now(),
		PollAfterSeconds: proto.Int32(5),
	}
	expected, err := publicjson.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	p := &renewalWorkspaceProbe{operation: owner}
	w := httptest.NewRecorder()
	NewWorkspaceHandler(p, renewalIdentity()).ServeHTTP(w, renewalWrite(`{"quoteId":"quote-renew-2"}`))

	if w.Code != http.StatusAccepted || w.Body.String() != string(expected) {
		t.Fatalf("published body is not the owner's own operation:\n got %s\nwant %s", w.Body.String(), string(expected))
	}
}

// TestWorkspaceRenewalCommandRequiresTheWriteGuard proves a renewal command
// without a live CSRF token, without an idempotency key, from a foreign origin
// or without an authorization decision never reaches the Workspace owner. A
// charge is never attempted by a command the boundary has not admitted.
func TestWorkspaceRenewalCommandRequiresTheWriteGuard(t *testing.T) {
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
			r.Body = io.NopCloser(strings.NewReader(`{"quoteId":"q","tenantId":"foreign"}`))
		}, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &renewalWorkspaceProbe{}
			i := renewalIdentity()
			r := renewalWrite(`{"quoteId":"quote-renew-1"}`)
			tc.mutate(r, i)
			w := httptest.NewRecorder()
			NewWorkspaceHandler(p, i).ServeHTTP(w, r)
			if w.Code != tc.want || p.renewed != nil {
				t.Fatalf("status=%d owner=%v body=%s", w.Code, p.renewed, w.Body.String())
			}
		})
	}
}

// TestWorkspaceRenewalCommandKeepsTheOwnerRefusal proves a refusal the Workspace
// owner states about the renewal keeps its own status and public error code,
// and that the owner's diagnostic detail is not leaked to the caller.
func TestWorkspaceRenewalCommandKeepsTheOwnerRefusal(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"elapsed period", status.Error(codes.FailedPrecondition, "the paid period already ended"), 422},
		{"no such workspace", status.Error(codes.NotFound, "Workspace order not found"), 404},
		{"invalid quote", status.Error(codes.InvalidArgument, "the quote is not a renewal quote"), 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := &renewalWorkspaceProbe{err: tc.err}
			w := httptest.NewRecorder()
			NewWorkspaceHandler(p, renewalIdentity()).ServeHTTP(w, renewalWrite(`{"quoteId":"quote-renew-1"}`))
			if w.Code != tc.want {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "paid period") || strings.Contains(w.Body.String(), "not a renewal quote") {
				t.Fatalf("owner diagnostic leaked: %s", w.Body.String())
			}
		})
	}
}
