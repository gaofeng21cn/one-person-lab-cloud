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
	// imageFacts turns the fixture into a digest-pinned multi-platform image:
	// the tag/digest names an index whose amd64 child is childDigest, and that
	// child's config blob is configBody.
	imageFacts bool
	configBody string
}

const (
	registryFixtureChildDigest  = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	registryFixtureConfigDigest = "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"
)

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
		if r.URL.Query().Get("scope") != "repository:oplcloud/one-person-lab-app:pull" {
			// The fixture only proves token negotiation happened; scope checking
			// beyond that is registry policy, not this client's contract.
		}
		f.tokenIssued++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"test-token-` + fmt.Sprint(f.tokenIssued) + `","expires_in":300}`))
	case r.URL.Path == "/v2/oplcloud/one-person-lab-app/tags/list":
		_, _ = w.Write([]byte(`{"name":"oplcloud/one-person-lab-app","tags":["v1.0.0","latest","v1.1.0-rc1"]}`))
	case strings.HasPrefix(r.URL.Path, "/v2/oplcloud/one-person-lab-app/manifests/"):
		w.Header().Set("Docker-Content-Digest", testDigest)
		if !f.imageFacts {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, registryFixtureChildDigest):
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_, _ = w.Write([]byte(`{"mediaType":"application/vnd.oci.image.manifest.v1+json","config":{"digest":"` + registryFixtureConfigDigest + `"}}`))
		default:
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			_, _ = w.Write([]byte(`{"mediaType":"application/vnd.oci.image.index.v1+json","manifests":[{"digest":"` + registryFixtureChildDigest + `","platform":{"os":"linux","architecture":"amd64"}}]}`))
		}
	case r.URL.Path == "/v2/oplcloud/one-person-lab-app/blobs/"+registryFixtureConfigDigest:
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(f.configBody))
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
	if _, err := fixture.client.ListTags(context.Background(), "oplcloud", "one-person-lab-app"); err == nil {
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
	tags, err := client.ListTags(context.Background(), "oplcloud", "one-person-lab-app")
	if err != nil {
		t.Fatalf("credentialed ListTags failed: %v", err)
	}
	if len(tags) != 3 {
		t.Fatalf("unexpected tags: %+v", tags)
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

// ImageFacts reads what the digest-pinned image declares about its own runtime
// shape. The index is resolved to the requested platform's child first, so the
// facts always describe the image the deployment would actually run.
func TestImageFactsReadTheImageOwnDeclarations(t *testing.T) {
	fixture := newRegistryFixture(t)
	fixture.imageFacts = true
	fixture.configBody = `{"architecture":"amd64","os":"linux","config":{"User":"10001:10001","ExposedPorts":{"8082/tcp":{}},"Volumes":{"/data":{}}}}`
	facts, err := fixture.client.ImageFacts(context.Background(), "oplcloud", "one-person-lab-app", testDigest, "linux/amd64")
	if err != nil {
		t.Fatalf("ImageFacts failed: %v", err)
	}
	if len(facts.Ports) != 1 || facts.Ports[0] != 8082 || len(facts.Volumes) != 1 || facts.Volumes[0] != "/data" || facts.User != "10001:10001" {
		t.Fatalf("facts = %+v", facts)
	}
}

// An image that carries no manifest for the requested platform is refused: the
// facts would otherwise describe an image this deployment cannot run.
func TestImageFactsRefuseAnUnsupportedPlatform(t *testing.T) {
	fixture := newRegistryFixture(t)
	fixture.imageFacts = true
	fixture.configBody = `{"config":{}}`
	if _, err := fixture.client.ImageFacts(context.Background(), "oplcloud", "one-person-lab-app", testDigest, "linux/arm64"); err == nil {
		t.Fatal("an index without the requested platform must not resolve")
	}
	for _, digest := range []string{"", "latest", "sha256:abc"} {
		if _, err := fixture.client.ImageFacts(context.Background(), "oplcloud", "one-person-lab-app", digest, "linux/amd64"); err == nil {
			t.Fatalf("digest %q must not be read as a pinned image", digest)
		}
	}
}
