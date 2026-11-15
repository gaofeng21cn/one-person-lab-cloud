package build

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// The Build owner is the producer of the workspace image trust source: it is
// the only owner that knows a repository was published by one of its own jobs.
// These tests exercise the producer against a real Control Plane route, so the
// request it publishes is the request the Control Plane actually reads.

func trustSourceEnvelope(t *testing.T, source contracts.WorkspaceImageTrustSource) string {
	t.Helper()
	encoded, err := json.Marshal(map[string]any{"source": "control-plane", "status": "available", "available": true, "data": source})
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func TestTrustSourceClientPublishesTheContractRequest(t *testing.T) {
	var method, path, authorization string
	var body contracts.WorkspaceImageReleaseActivationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path, authorization = r.Method, r.URL.Path, r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(trustSourceEnvelope(t, contracts.WorkspaceImageTrustSource{SchemaVersion: 1, Revision: 2, Repositories: []string{"registry.example.test/opl"}})))
	}))
	defer server.Close()

	client := newTrustSourceClient(func(name string) string {
		switch name {
		case "OPL_CONTROL_PLANE_URL":
			return server.URL
		case "OPL_FABRIC_CAPABILITY_KEY":
			return "shared-capability-key"
		}
		return ""
	})
	updated, err := client.RegisterRepository(context.Background(), contracts.WorkspaceImageReleaseActivationRequest{
		ReleaseVersion:    "build_job_1",
		ReleaseRepository: "registry.example.test/opl",
		ExpectedRevision:  1,
		Reason:            "build job build_job_1 published package version pv_1",
	})
	if err != nil {
		t.Fatalf("register repository: %v", err)
	}
	if method != http.MethodPut {
		t.Fatalf("method = %q, want PUT", method)
	}
	if path != contracts.WorkspaceImageTrustSourcePath {
		t.Fatalf("path = %q, want %q", path, contracts.WorkspaceImageTrustSourcePath)
	}
	if authorization != "Bearer shared-capability-key" {
		t.Fatalf("authorization = %q, want the shared bearer credential", authorization)
	}
	if body.ReleaseVersion != "build_job_1" || body.ReleaseRepository != "registry.example.test/opl" || body.ExpectedRevision != 1 {
		t.Fatalf("published request = %+v", body)
	}
	if body.Reason == "" {
		t.Fatal("the published request must carry an auditable reason")
	}
	if updated.Revision != 2 || !updated.TrustsRepository("registry.example.test/opl") {
		t.Fatalf("registered source = %+v", updated)
	}
}

func TestTrustSourceClientReadsTheRegisteredSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer shared-capability-key" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(trustSourceEnvelope(t, contracts.WorkspaceImageTrustSource{SchemaVersion: 1, Revision: 4, Repositories: []string{"registry.example.test/opl"}})))
	}))
	defer server.Close()

	client := newTrustSourceClient(func(name string) string {
		switch name {
		case "OPL_CONTROL_PLANE_URL":
			return server.URL
		case "OPL_FABRIC_CAPABILITY_KEY":
			return "shared-capability-key"
		}
		return ""
	})
	source, err := client.TrustSource(context.Background())
	if err != nil {
		t.Fatalf("read trust source: %v", err)
	}
	if source.Revision != 4 || !source.TrustsRepository("registry.example.test/opl") {
		t.Fatalf("source = %+v", source)
	}
}

func TestTrustSourceClientRefusesAnUntrustedResponse(t *testing.T) {
	for name, payload := range map[string]string{
		"unknown field": `{"data":{"schemaVersion":1,"revision":1,"repositories":[],"surprise":true}}`,
		"not a source":  `{"data":{"schemaVersion":2,"revision":1,"repositories":[]}}`,
		"no data":       `{"status":"available"}`,
		"null data":     `{"data":null}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(payload))
			}))
			defer server.Close()
			client := newTrustSourceClient(func(name string) string {
				switch name {
				case "OPL_CONTROL_PLANE_URL":
					return server.URL
				case "OPL_FABRIC_CAPABILITY_KEY":
					return "shared-capability-key"
				}
				return ""
			})
			if _, err := client.TrustSource(context.Background()); err == nil {
				t.Fatal("an untrusted response must not become a trust decision")
			} else if code := trustSourceErrorCode(err); code != trustSourceResponseInvalid {
				t.Fatalf("error code = %q, want %q", code, trustSourceResponseInvalid)
			}
		})
	}
}

func TestTrustSourceClientNamesTheRefusal(t *testing.T) {
	for name, testCase := range map[string]struct {
		status int
		want   string
	}{
		"refused credential": {http.StatusUnauthorized, trustSourceUnauthorized},
		"forbidden":          {http.StatusForbidden, trustSourceUnauthorized},
		"moved revision":     {http.StatusConflict, trustSourceConflict},
		"refused request":    {http.StatusBadRequest, trustSourceRequestRejected},
		"unavailable":        {http.StatusServiceUnavailable, trustSourceUnavailable},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = w.Write([]byte(`{"error":"refused"}`))
			}))
			defer server.Close()
			client := newTrustSourceClient(func(name string) string {
				switch name {
				case "OPL_CONTROL_PLANE_URL":
					return server.URL
				case "OPL_FABRIC_CAPABILITY_KEY":
					return "shared-capability-key"
				}
				return ""
			})
			_, err := client.TrustSource(context.Background())
			if err == nil {
				t.Fatal("a refused Control Plane must not read as an empty trust source")
			}
			if code := trustSourceErrorCode(err); code != testCase.want {
				t.Fatalf("error code = %q, want %q", code, testCase.want)
			}
		})
	}
}

func TestTrustSourceClientWithoutCredentialMakesNoCall(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	client := newTrustSourceClient(func(name string) string {
		if name == "OPL_CONTROL_PLANE_URL" {
			return server.URL
		}
		return ""
	})
	if _, err := client.TrustSource(context.Background()); err == nil {
		t.Fatal("a producer without the shared credential must not read a trust source")
	} else if code := trustSourceErrorCode(err); code != trustSourceCredentialMissing {
		t.Fatalf("error code = %q, want %q", code, trustSourceCredentialMissing)
	}
	if called {
		t.Fatal("a producer without the shared credential must not call the Control Plane")
	}
}

func TestTrustSourceClientFallsBackToTheDeploymentDefaultAddress(t *testing.T) {
	client := newTrustSourceClient(nil)
	if client.url != contracts.WorkspaceImageTrustSourceURLDefault {
		t.Fatalf("url = %q, want %q", client.url, contracts.WorkspaceImageTrustSourceURLDefault)
	}
	named := newTrustSourceClient(func(name string) string {
		if name == "OPL_CONTROL_PLANE_URL" {
			return "https://control-plane.example.test/"
		}
		return ""
	})
	if named.url != "https://control-plane.example.test" {
		t.Fatalf("url = %q, want the installation's own address without a trailing slash", named.url)
	}
}

func TestRegistrationReasonNamesThePublication(t *testing.T) {
	reason := registrationReason(registrationIntent{BuildJobID: "build_job_1", PackageVersionID: "pv_1"})
	if !strings.Contains(reason, "build_job_1") || !strings.Contains(reason, "pv_1") {
		t.Fatalf("reason = %q, want it to name the build job and the package version", reason)
	}
}

func TestTrustSourceRetryDelayGrows(t *testing.T) {
	previous := time.Duration(0)
	for _, attempts := range []int{1, 2, 3, 4, 5, 50} {
		delay := trustSourceRetryDelay(attempts)
		if delay < previous {
			t.Fatalf("attempt %d waits %s, shorter than the attempt before it", attempts, delay)
		}
		if delay > 30*time.Minute {
			t.Fatalf("attempt %d waits %s, longer than a producer should wait", attempts, delay)
		}
		previous = delay
	}
}
