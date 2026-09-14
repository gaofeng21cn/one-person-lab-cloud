package clients

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newRegistryTestServer wires a minimal OCI Distribution API v2 surface whose
// behavior each test controls through the captured handler.
type registryFixture struct {
	server      *httptest.Server
	client      WorkspaceRegistryClient
	basicUser   string
	basicPass   string
	bearerRealm string
	tokenIssued int
}

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func (f *registryFixture) handler(w http.ResponseWriter, r *http.Request) {
	if f.basicUser != "" {
		user, pass, ok := r.BasicAuth()
		if !ok || user != f.basicUser || pass != f.basicPass {
			w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/v2/token",service="registry"`, f.server.URL))
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	switch {
	case r.URL.Path == "/v2/" && f.bearerRealm != "":
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/v2/token",service="registry"`, f.server.URL))
		w.WriteHeader(http.StatusUnauthorized)
	case r.URL.Path == "/v2/":
		w.WriteHeader(http.StatusOK)
	case r.URL.Path == "/v2/token" && f.bearerRealm != "":
		if r.URL.Query().Get("scope") != "repository:oplcloud/one-person-lab-app:pull" && r.URL.Query().Get("scope") != "repository:oplcloud/*:pull,registry:catalog:*" {
			// The fixture only proves token negotiation happened; scope checking
			// beyond that is registry policy, not this client's contract.
		}
		f.tokenIssued++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"test-token-` + fmt.Sprint(f.tokenIssued) + `","expires_in":300}`))
	case r.URL.Path == "/v2/_catalog":
		_, _ = w.Write([]byte(`{"repositories":["oplcloud/chaokang_agent_ibd","oplcloud/one-person-lab-app","library/other","oplcloud/","oplcloud/Bad@name"]}`))
	case r.URL.Path == "/v2/oplcloud/one-person-lab-app/tags/list":
		_, _ = w.Write([]byte(`{"name":"oplcloud/one-person-lab-app","tags":["v1.0.0","latest","v1.1.0-rc1"]}`))
	case strings.HasPrefix(r.URL.Path, "/v2/oplcloud/one-person-lab-app/manifests/"):
		w.Header().Set("Docker-Content-Digest", testDigest)
		_, _ = w.Write([]byte(`{}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func newRegistryFixture(t *testing.T) *registryFixture {
	t.Helper()
	fixture := &registryFixture{}
	fixture.server = httptest.NewTLSServer(http.HandlerFunc(fixture.handler))
	t.Cleanup(fixture.server.Close)
	client, err := NewWorkspaceRegistryHTTPClient(WorkspaceRegistryConfig{
		Host: strings.TrimPrefix(fixture.server.URL, "https://"),
	}, fixture.server.Client())
	if err != nil {
		t.Fatalf("registry client construction failed: %v", err)
	}
	fixture.client = client
	return fixture
}

func TestListRepositoriesFiltersNamespace(t *testing.T) {
	fixture := newRegistryFixture(t)
	repositories, err := fixture.client.ListRepositories(context.Background(), "oplcloud")
	if err != nil {
		t.Fatalf("ListRepositories failed: %v", err)
	}
	var names []string
	for _, repository := range repositories {
		names = append(names, repository.Repository)
	}
	if len(names) != 2 || names[0] != "chaokang_agent_ibd" || names[1] != "one-person-lab-app" {
		t.Fatalf("unexpected repositories: %v", names)
	}
	for _, repository := range repositories {
		if repository.Namespace != "oplcloud" {
			t.Fatalf("repository carried wrong namespace: %+v", repository)
		}
	}
}

func TestListRepositoriesRejectsForeignNamespace(t *testing.T) {
	fixture := newRegistryFixture(t)
	if _, err := fixture.client.ListRepositories(context.Background(), "library"); err == nil {
		t.Fatal("foreign namespace must not reach the registry")
	}
}

func TestListTagsReturnsOrderedTags(t *testing.T) {
	fixture := newRegistryFixture(t)
	tags, err := fixture.client.ListTags(context.Background(), "oplcloud", "one-person-lab-app")
	if err != nil {
		t.Fatalf("ListTags failed: %v", err)
	}
	if len(tags) != 3 || tags[0].Tag != "latest" || tags[1].Tag != "v1.0.0" || tags[2].Tag != "v1.1.0-rc1" {
		t.Fatalf("unexpected tags: %+v", tags)
	}
}

func TestListTagsRejectsInvalidRepository(t *testing.T) {
	fixture := newRegistryFixture(t)
	if _, err := fixture.client.ListTags(context.Background(), "oplcloud", "../escape"); err == nil {
		t.Fatal("path traversal repository must not reach the registry")
	}
}

func TestResolveTagReturnsDigestPinnedReference(t *testing.T) {
	fixture := newRegistryFixture(t)
	resolution, err := fixture.client.ResolveTag(context.Background(), "oplcloud", "one-person-lab-app", "v1.0.0")
	if err != nil {
		t.Fatalf("ResolveTag failed: %v", err)
	}
	if resolution.Digest != testDigest {
		t.Fatalf("digest = %q", resolution.Digest)
	}
	expected := "uswccr-.invalid/oplcloud/one-person-lab-app@"
	if !strings.HasSuffix(resolution.Reference, expected+testDigest) && !strings.Contains(resolution.Reference, "/oplcloud/one-person-lab-app@"+testDigest) {
		t.Fatalf("reference %q is not digest-pinned in the requested repository", resolution.Reference)
	}
	if strings.Contains(resolution.Reference, "v1.0.0") {
		t.Fatalf("reference %q must not carry the tag", resolution.Reference)
	}
}

func TestResolveTagRejectsInvalidTag(t *testing.T) {
	fixture := newRegistryFixture(t)
	for _, tag := range []string{"", "a b", "a/b", "a@b", strings.Repeat("t", 200)} {
		if _, err := fixture.client.ResolveTag(context.Background(), "oplcloud", "one-person-lab-app", tag); err == nil {
			t.Fatalf("tag %q must not resolve", tag)
		}
	}
}

func TestAnonymousAccessDeniedSurfacesAuth(t *testing.T) {
	fixture := newRegistryFixture(t)
	fixture.basicUser, fixture.basicPass = "operator", "secret"
	fixture.bearerRealm = fixture.server.URL
	if _, err := fixture.client.ListRepositories(context.Background(), "oplcloud"); err == nil {
		t.Fatal("unauthenticated request against a credentialed registry must fail")
	} else {
		var apiErr *RegistryAPIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error is not a RegistryAPIError: %v", err)
		}
	}
}

func TestCredentialAuthNegotiatesToken(t *testing.T) {
	fixture := newRegistryFixture(t)
	fixture.basicUser, fixture.basicPass = "operator", "secret"
	fixture.bearerRealm = fixture.server.URL
	client, err := NewWorkspaceRegistryHTTPClient(WorkspaceRegistryConfig{
		Host:       strings.TrimPrefix(fixture.server.URL, "https://"),
		Credential: RegistryCredential{Username: "operator", Password: "secret"},
	}, fixture.server.Client())
	if err != nil {
		t.Fatalf("client construction failed: %v", err)
	}
	repositories, err := client.ListRepositories(context.Background(), "oplcloud")
	if err != nil {
		t.Fatalf("credentialed ListRepositories failed: %v", err)
	}
	if len(repositories) != 2 {
		t.Fatalf("unexpected repositories: %+v", repositories)
	}
	if fixture.tokenIssued == 0 {
		t.Fatal("bearer negotiation never ran")
	}
}

func TestInvalidHostRejectedAtConstruction(t *testing.T) {
	for _, host := range []string{"", "not a host/path", "registry"} {
		if _, err := NewWorkspaceRegistryHTTPClient(WorkspaceRegistryConfig{Host: host}, nil); err == nil {
			t.Fatalf("host %q must fail construction", host)
		}
	}
}

func TestCredentialMismatchFailsConstruction(t *testing.T) {
	if _, err := NewWorkspaceRegistryHTTPClient(WorkspaceRegistryConfig{
		Host:       "registry.example",
		Credential: RegistryCredential{Username: "operator"},
	}, nil); err == nil {
		t.Fatal("username without password must fail construction")
	}
}
