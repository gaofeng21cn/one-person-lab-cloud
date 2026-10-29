package httpapi

import (
	"context"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io"
	"net/http"
	"net/http/httptest"
	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"strings"
	"testing"
)

type updateReader struct {
	ServeDeliveryReader
	call      *api.CallContext
	body      *api.UpdateWorkspaceVersionRequest
	workspace string
	calls     int
}

func (u *updateReader) UpdateWorkspaceVersion(ctx context.Context, workspace string, body *api.UpdateWorkspaceVersionRequest) (*api.Operation, error) {
	u.calls++
	u.call = clients.CallContext(ctx)
	u.workspace = workspace
	u.body = body
	return &api.Operation{OperationId: "op-update", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_UPDATE_WORKSPACE, ResourceId: "dep-next", Status: api.OperationStatusEnum_OPERATION_STATUS_ENUM_RUNNING, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_RUNTIME, RequestId: "request-1", CreatedAt: timestamppb.Now(), UpdatedAt: timestamppb.Now(), PollAfterSeconds: proto.Int32(2)}, nil
}
func TestNativeWorkspaceUpdateRoute(t *testing.T) {
	identity := allowedIdentity()
	action := api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION
	identity.decisions[action] = allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_SERVE, action, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws_1042")
	identity.decisions[action].AuthorizationContextId = proto.String("serve-update-authority")
	u := &updateReader{}
	handler := NewServeDeliveryHandler(u, identity)
	body := `{"applicationSelection":{"kind":"opl_app","runtimeVersionId":"runtime-next"},"expectedCurrentDeploymentId":"dep-original"}`
	request := func(raw string) *http.Request {
		r := sessionRequest(http.MethodPost, "/api/v2/workspaces/ws_1042/update")
		r.Body = io.NopCloser(strings.NewReader(raw))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", "csrf-1")
		r.Header.Set("Idempotency-Key", "same-upgrade")
		return r
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request(body))
	if response.Code != http.StatusAccepted || response.Header().Get("Retry-After") != "2" {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if u.calls != 1 || u.workspace != "ws_1042" || u.body.GetApplicationSelection().GetRuntimeVersionId() != "runtime-next" || u.body.GetExpectedCurrentAgentDeploymentId() != "dep-original" || u.call.GetIdempotencyKey() != "same-upgrade" || u.call.GetActorId() != "actor-1" || u.call.GetAuthorizationContextId() != "serve-update-authority" {
		t.Fatalf("command=%v context=%v calls=%d", u.body, u.call, u.calls)
	}
	for _, raw := range []string{
		`{"applicationSelection":{"kind":"opl_app","runtimeVersionId":"runtime-next"},"capabilityVersionId":"","expectedCurrentDeploymentId":"dep-original"}`,
		`{"applicationSelection":{"kind":"opl_app","runtimeVersionId":"runtime-next","capabilityVersionId":"cv"},"expectedCurrentDeploymentId":"dep-original"}`,
		`{"expectedCurrentDeploymentId":"dep-original"}`,
		`{"applicationSelection":{"kind":"opl_app","runtimeVersionId":"runtime-next"},"expectedCurrentDeploymentId":"dep-original","actorId":"forged"}`,
	} {
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request(raw))
		if response.Code != http.StatusBadRequest || u.calls != 1 {
			t.Fatalf("invalid raw=%s status=%d calls=%d body=%s", raw, response.Code, u.calls, response.Body.String())
		}
	}
	for _, header := range []string{"X-CSRF-Token", "Idempotency-Key"} {
		r := request(body)
		r.Header.Del(header)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, r)
		if response.Code < 400 || u.calls != 1 {
			t.Fatalf("missing %s reached owner", header)
		}
	}
	r := request(body)
	r.Header.Set("Origin", "https://other.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, r)
	if response.Code != http.StatusForbidden || u.calls != 1 {
		t.Fatal("cross-origin update reached owner")
	}
	identity.decisions[action].Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request(body))
	if response.Code != http.StatusForbidden || u.calls != 1 {
		t.Fatal("denied update reached owner")
	}
}

func TestWorkspaceUpdatePreservesLegacyAndExplicitAgentSelection(t *testing.T) {
	for _, source := range []string{`"capabilityVersionId":"cv-ready"`, `"applicationSelection":{"kind":"agent","capabilityVersionId":"cv-ready"}`} {
		t.Run(source, func(t *testing.T) {
			identity := allowedIdentity()
			action := api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEVERSION
			identity.decisions[action] = allowFor("actor-1", "tenant-1", api.OwnerEnum_OWNER_ENUM_SERVE, action, api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, "ws_1042")
			u := &updateReader{}
			r := sessionRequest(http.MethodPost, "/api/v2/workspaces/ws_1042/update")
			r.Body = io.NopCloser(strings.NewReader(`{` + source + `,"expectedCurrentDeploymentId":"dep-original"}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-CSRF-Token", "csrf-1")
			r.Header.Set("Idempotency-Key", "original-agent-update")
			response := httptest.NewRecorder()
			NewServeDeliveryHandler(u, identity).ServeHTTP(response, r)
			if response.Code != http.StatusAccepted || u.calls != 1 {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if u.body.GetApplicationSelection() != nil {
				if u.body.CapabilityVersionId != nil || u.body.GetApplicationSelection().GetCapabilityVersionId() != "cv-ready" {
					t.Fatalf("explicit source changed: %v", u.body)
				}
			} else if u.body.GetCapabilityVersionId() != "cv-ready" {
				t.Fatalf("legacy source changed: %v", u.body)
			}
		})
	}
}
