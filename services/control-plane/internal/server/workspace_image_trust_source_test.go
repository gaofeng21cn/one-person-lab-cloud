package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

const (
	workspaceImageTrustServiceToken = "workspace-image-trust-source-service-token-0001"
	workspaceImageTrustRepository   = "127.0.0.1:5000/qualification/local-qualification"
	workspaceImageTrustRepository2  = "127.0.0.1:5000/qualification/other-qualification"
)

// workspaceImageTrustTestServer runs the real trust source routes over a real
// HTTP listener, so the assertions below are wire assertions, not handler
// assertions.
func workspaceImageTrustTestServer(t *testing.T) (*httptest.Server, *controlPlaneServer) {
	t.Helper()
	app := newControlPlaneAppEmpty()
	mux := http.NewServeMux()
	registerWorkspaceImageTrustSourceRoutes(mux, app)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server, app
}

func workspaceImageTrustRequest(t *testing.T, method, url, token, body string) (*http.Response, map[string]any) {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { response.Body.Close() })
	var decoded map[string]any
	if response.Header.Get("Content-Type") == "application/json" {
		if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
			t.Fatalf("response body was not json: %v", err)
		}
	}
	return response, decoded
}

func workspaceImageTrustData(t *testing.T, decoded map[string]any) map[string]any {
	t.Helper()
	data, _ := decoded["data"].(map[string]any)
	if data == nil {
		t.Fatalf("envelope carried no data: %#v", decoded)
	}
	return data
}

func workspaceImageTrustRepositories(t *testing.T, decoded map[string]any) []string {
	t.Helper()
	data := workspaceImageTrustData(t, decoded)
	raw, _ := data["repositories"].([]any)
	repositories := make([]string, 0, len(raw))
	for _, item := range raw {
		value, _ := item.(string)
		repositories = append(repositories, value)
	}
	return repositories
}

// The registration is a service call. An unauthenticated write must not be
// able to widen what a workspace is allowed to start.
func TestWorkspaceImageTrustSourceRejectsWritesWithoutTheServiceToken(t *testing.T) {
	t.Setenv("OPL_FABRIC_CAPABILITY_KEY", workspaceImageTrustServiceToken)
	server, _ := workspaceImageTrustTestServer(t)
	body := `{"releaseVersion":"customer-agent-2026.11.15","releaseRepository":"` + workspaceImageTrustRepository + `","expectedRevision":1,"reason":"customer agent build succeeded"}`
	for name, token := range map[string]string{"no token": "", "wrong token": "not-the-service-token"} {
		t.Run(name, func(t *testing.T) {
			response, decoded := workspaceImageTrustRequest(t, http.MethodPut, server.URL+"/internal/workspace-image-trust-source", token, body)
			if response.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status=%d body=%#v want 401", response.StatusCode, decoded)
			}
			if decoded["error"] != "service_token_invalid" {
				t.Fatalf("error=%#v want service_token_invalid", decoded["error"])
			}
		})
	}
}

func TestWorkspaceImageTrustSourceRejectsMalformedRegistrations(t *testing.T) {
	t.Setenv("OPL_FABRIC_CAPABILITY_KEY", workspaceImageTrustServiceToken)
	server, _ := workspaceImageTrustTestServer(t)
	for name, body := range map[string]string{
		"malformed json":    `{`,
		"unknown field":     `{"releaseVersion":"v1","releaseRepository":"` + workspaceImageTrustRepository + `","expectedRevision":1,"reason":"r","trusted":true}`,
		"missing repository": `{"releaseVersion":"v1","expectedRevision":1,"reason":"r"}`,
		"invalid repository": `{"releaseVersion":"v1","releaseRepository":"registry.example","expectedRevision":1,"reason":"r"}`,
		"missing version":    `{"releaseRepository":"` + workspaceImageTrustRepository + `","expectedRevision":1,"reason":"r"}`,
		"missing reason":     `{"releaseVersion":"v1","releaseRepository":"` + workspaceImageTrustRepository + `","expectedRevision":1}`,
	} {
		t.Run(name, func(t *testing.T) {
			response, decoded := workspaceImageTrustRequest(t, http.MethodPut, server.URL+"/internal/workspace-image-trust-source", workspaceImageTrustServiceToken, body)
			if response.StatusCode != http.StatusBadRequest {
				t.Fatalf("status=%d body=%#v want 400", response.StatusCode, decoded)
			}
			if decoded["error"] != "workspace_image_trust_source_request_invalid" {
				t.Fatalf("error=%#v want workspace_image_trust_source_request_invalid", decoded["error"])
			}
		})
	}
}

// Registering a repository moves the revision once and is audited. Registering
// the same repository again is the same fact and must not move it again, or
// every retry would look like a new decision.
func TestWorkspaceImageTrustSourceRegistrationIsRevisionedAuditedAndIdempotent(t *testing.T) {
	t.Setenv("OPL_FABRIC_CAPABILITY_KEY", workspaceImageTrustServiceToken)
	server, app := workspaceImageTrustTestServer(t)

	response, decoded := workspaceImageTrustRequest(t, http.MethodPut, server.URL+"/internal/workspace-image-trust-source", workspaceImageTrustServiceToken,
		`{"releaseVersion":"customer-agent-2026.11.15","releaseRepository":"`+workspaceImageTrustRepository+`","expectedRevision":1,"reason":"customer agent build succeeded"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%#v want 200", response.StatusCode, decoded)
	}
	data := workspaceImageTrustData(t, decoded)
	if data["revision"] != float64(2) {
		t.Fatalf("revision=%#v want 2", data["revision"])
	}
	repositories := workspaceImageTrustRepositories(t, decoded)
	if len(repositories) != 1 || repositories[0] != workspaceImageTrustRepository {
		t.Fatalf("repositories=%#v want the registered repository", repositories)
	}

	// The same registration again is the same fact.
	response, decoded = workspaceImageTrustRequest(t, http.MethodPut, server.URL+"/internal/workspace-image-trust-source", workspaceImageTrustServiceToken,
		`{"releaseVersion":"customer-agent-2026.11.15","releaseRepository":"`+workspaceImageTrustRepository+`","expectedRevision":2,"reason":"customer agent build succeeded"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("repeat status=%d body=%#v want 200", response.StatusCode, decoded)
	}
	if workspaceImageTrustData(t, decoded)["revision"] != float64(2) {
		t.Fatalf("a repeated registration moved the revision: %#v", decoded)
	}

	// A different repository is a different fact and does move it.
	response, decoded = workspaceImageTrustRequest(t, http.MethodPut, server.URL+"/internal/workspace-image-trust-source", workspaceImageTrustServiceToken,
		`{"releaseVersion":"customer-agent-2026.11.16","releaseRepository":"`+workspaceImageTrustRepository2+`","expectedRevision":2,"reason":"second customer agent build succeeded"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("second status=%d body=%#v want 200", response.StatusCode, decoded)
	}
	data = workspaceImageTrustData(t, decoded)
	if data["revision"] != float64(3) {
		t.Fatalf("revision=%#v want 3", data["revision"])
	}
	if len(workspaceImageTrustRepositories(t, decoded)) != 2 {
		t.Fatalf("repositories=%#v want both", workspaceImageTrustRepositories(t, decoded))
	}

	// A stale expected revision is a conflict, not a silent overwrite.
	response, decoded = workspaceImageTrustRequest(t, http.MethodPut, server.URL+"/internal/workspace-image-trust-source", workspaceImageTrustServiceToken,
		`{"releaseVersion":"customer-agent-2026.11.17","releaseRepository":"registry.example/opl/workspace","expectedRevision":1,"reason":"stale writer"}`)
	if response.StatusCode != http.StatusConflict {
		t.Fatalf("stale status=%d body=%#v want 409", response.StatusCode, decoded)
	}
	if decoded["error"] != "workspace_image_trust_source_revision_conflict" {
		t.Fatalf("error=%#v want workspace_image_trust_source_revision_conflict", decoded["error"])
	}

	events, err := app.tables.ListAuditEvents(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	registrations := 0
	for _, event := range events {
		if stringValue(event["action"]) != workspaceImageTrustSourceAction {
			continue
		}
		registrations++
		after, _ := event["after"].(map[string]any)
		if after["repository"] == nil || after["releaseVersion"] == nil || after["reason"] == nil {
			t.Fatalf("audit event lost the registration facts: %#v", event)
		}
	}
	if registrations != 2 {
		t.Fatalf("audit registrations=%d want 2 (the idempotent repeat must not be audited as a new decision)", registrations)
	}
}

func TestWorkspaceImageTrustSourceReadbackMatchesTheSharedContractType(t *testing.T) {
	t.Setenv("OPL_FABRIC_CAPABILITY_KEY", workspaceImageTrustServiceToken)
	server, _ := workspaceImageTrustTestServer(t)
	response, decoded := workspaceImageTrustRequest(t, http.MethodPut, server.URL+"/internal/workspace-image-trust-source", workspaceImageTrustServiceToken,
		`{"releaseVersion":"customer-agent-2026.11.15","releaseRepository":"`+workspaceImageTrustRepository+`","expectedRevision":1,"reason":"customer agent build succeeded"}`)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", response.StatusCode)
	}
	readback, decoded := workspaceImageTrustRequest(t, http.MethodGet, server.URL+"/internal/workspace-image-trust-source", workspaceImageTrustServiceToken, "")
	if readback.StatusCode != http.StatusOK {
		t.Fatalf("readback status=%d body=%#v want 200", readback.StatusCode, decoded)
	}
	data := workspaceImageTrustData(t, decoded)
	// The wire payload must decode into the same shared contract type the
	// providers read, so the two sides cannot drift apart.
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	source, err := contracts.DecodeWorkspaceImageTrustSource(string(encoded))
	if err != nil {
		t.Fatalf("readback does not decode into the shared contract type: %v", err)
	}
	if !source.TrustsRepository(workspaceImageTrustRepository) {
		t.Fatalf("readback lost the registered repository: %#v", source)
	}
	if source.Revision != 2 {
		t.Fatalf("readback revision=%d want 2", source.Revision)
	}
}

func TestWorkspaceImageTrustSourceInternalReadRequiresTheServiceToken(t *testing.T) {
	t.Setenv("OPL_FABRIC_CAPABILITY_KEY", workspaceImageTrustServiceToken)
	server, _ := workspaceImageTrustTestServer(t)
	response, decoded := workspaceImageTrustRequest(t, http.MethodGet, server.URL+"/internal/workspace-image-trust-source", "", "")
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%#v want 401", response.StatusCode, decoded)
	}
}

// The registration is a service call, so the route lives under the internal
// service surface and not under the operator surface. An operator route with
// no production client is a surface nobody can reach, which is exactly what
// the route ownership boundary refuses to let exist.
func TestWorkspaceImageTrustSourceHasNoOperatorRoute(t *testing.T) {
	t.Setenv("OPL_FABRIC_CAPABILITY_KEY", workspaceImageTrustServiceToken)
	app := newControlPlaneAppEmpty()
	mux := http.NewServeMux()
	registerWorkspaceImageTrustSourceRoutes(mux, app)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		request := httptest.NewRequest(method, "/api/operator/workspace-image-trust-source", nil)
		_, pattern := mux.Handler(request)
		if pattern != "" {
			t.Fatalf("the operator surface registered %q", pattern)
		}
	}
}

// The registered route patterns and the shared contract constant must be the
// same path. The provider reads the constant, so a drift between the two
// would leave the provider calling a route nobody serves.
func TestWorkspaceImageTrustSourceRouteIsTheSharedContractPath(t *testing.T) {
	app := newControlPlaneAppEmpty()
	mux := http.NewServeMux()
	registerWorkspaceImageTrustSourceRoutes(mux, app)
	for _, method := range []string{http.MethodGet, http.MethodPut} {
		request := httptest.NewRequest(method, contracts.WorkspaceImageTrustSourcePath, nil)
		_, pattern := mux.Handler(request)
		if pattern != method+" "+contracts.WorkspaceImageTrustSourcePath {
			t.Fatalf("registered pattern=%q want %q", pattern, method+" "+contracts.WorkspaceImageTrustSourcePath)
		}
	}
}

func TestWorkspaceImageTrustSourceRejectsAnUntrustedServiceTokenLength(t *testing.T) {
	t.Setenv("OPL_FABRIC_CAPABILITY_KEY", "short")
	server, _ := workspaceImageTrustTestServer(t)
	response, decoded := workspaceImageTrustRequest(t, http.MethodPut, server.URL+"/internal/workspace-image-trust-source", "short",
		`{"releaseVersion":"v1","releaseRepository":"`+workspaceImageTrustRepository+`","expectedRevision":1,"reason":"r"}`)
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%#v want 401", response.StatusCode, decoded)
	}
	if !strings.Contains(server.URL, "http://") {
		t.Fatalf("unexpected test server url: %q", server.URL)
	}
}
