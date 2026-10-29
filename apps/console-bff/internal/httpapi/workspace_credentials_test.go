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
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type workspaceCredentialProbe struct {
	api.WorkspaceProductServiceClient
	requests []*api.RevealWorkspaceApplicationCredentialsRpcRequest
	response *api.WorkspaceApplicationCredentials
	err      error
}

func (p *workspaceCredentialProbe) RevealWorkspaceApplicationCredentials(_ context.Context, r *api.RevealWorkspaceApplicationCredentialsRpcRequest, _ ...grpc.CallOption) (*api.WorkspaceApplicationCredentials, error) {
	p.requests = append(p.requests, r)
	return p.response, p.err
}
func credentialRevealRequest() *http.Request {
	r := sessionRequest(http.MethodPost, "/api/v2/workspaces/ws-1/application-credentials/reveal")
	r.Header.Set("X-CSRF-Token", "csrf-1")
	r.Header.Set("Idempotency-Key", "credential-reveal-1")
	return r
}
func TestWorkspaceCredentialsTypedRevealAndPrivateResponse(t *testing.T) {
	p := &workspaceCredentialProbe{response: &api.WorkspaceApplicationCredentials{WorkspaceId: "ws-1", RuntimeInstanceId: "runtime-1", Username: "admin", Password: "fixture-password"}}
	identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVEALWORKSPACEAPPLICATIONCREDENTIALS, "ws-1")
	handler := NewWorkspaceHandler(p, identity)
	for n := 0; n < 2; n++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, credentialRevealRequest())
		if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("status=%d cache=%s", w.Code, w.Header().Get("Cache-Control"))
		}
		var body map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if len(body) != 4 || body["username"] != "admin" || body["password"] != p.response.Password || body["runtimeInstanceId"] != "runtime-1" {
			t.Fatal("response is not the declared credential shape")
		}
		p.response.Password = "fresh-runtime-value"
	}
	if len(p.requests) != 2 || len(identity.requests) != 2 {
		t.Fatal("repeated reveal was cached")
	}
	c := p.requests[0].GetContext()
	if p.requests[0].GetWorkspaceId() != "ws-1" || c.GetActorId() != "actor-1" || c.GetSessionId() != owneridentity.SessionReference("session-1") || c.GetAuthorizationContextId() != "workspace-authorization" {
		t.Fatal("live caller not forwarded")
	}
}
func TestWorkspaceCredentialsGuardsAndFailuresNeverCallOrCacheSecrets(t *testing.T) {
	for _, name := range []string{"no session", "csrf", "origin", "idempotency", "denied", "owner unavailable", "wrong workspace", "missing password"} {
		t.Run(name, func(t *testing.T) {
			p := &workspaceCredentialProbe{response: &api.WorkspaceApplicationCredentials{WorkspaceId: "ws-1", RuntimeInstanceId: "runtime-1", Username: "admin", Password: "fixture-password"}}
			identity := workspaceIdentity(api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_REVEALWORKSPACEAPPLICATIONCREDENTIALS, "ws-1")
			r := credentialRevealRequest()
			want := 403
			wantCalls := 0
			switch name {
			case "no session":
				r.Header.Del("Cookie")
				want = 401
			case "csrf":
				r.Header.Del("X-CSRF-Token")
			case "origin":
				r.Header.Set("Origin", "https://foreign.test")
			case "idempotency":
				r.Header.Del("Idempotency-Key")
				want = 400
			case "denied":
				identity.decision.Result = api.AuthorizationResult_AUTHORIZATION_RESULT_DENIED
			case "owner unavailable":
				p.err = status.Error(codes.Unavailable, "owner unavailable")
				want = 503
				wantCalls = 1
			case "wrong workspace":
				p.response.WorkspaceId = "foreign"
				want = 503
				wantCalls = 1
			case "missing password":
				p.response.Password = ""
				want = 503
				wantCalls = 1
			}
			w := httptest.NewRecorder()
			NewWorkspaceHandler(p, identity).ServeHTTP(w, r)
			if w.Code != want || w.Header().Get("Cache-Control") != "private, no-store" || len(p.requests) != wantCalls {
				t.Fatalf("status=%d want=%d owner calls=%d", w.Code, want, len(p.requests))
			}
		})
	}
}
