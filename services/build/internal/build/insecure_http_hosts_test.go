package build

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The plain-HTTP registry list is read from the environment, so the value the
// installation declares is the whole contract: an empty value declares none.
func TestParseInsecureHTTPHosts(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		expected []string
	}{
		{name: "an absent value declares no host", raw: "", expected: nil},
		{name: "one host", raw: "127.0.0.1:5000", expected: []string{"127.0.0.1:5000"}},
		{name: "several hosts with spaces", raw: " 127.0.0.1:5000 , registry.local-v2.test:5000 ", expected: []string{"127.0.0.1:5000", "registry.local-v2.test:5000"}},
		{name: "an empty entry is not a host", raw: "127.0.0.1:5000,,", expected: []string{"127.0.0.1:5000"}},
		{name: "a repeated host is one host", raw: "127.0.0.1:5000,127.0.0.1:5000", expected: []string{"127.0.0.1:5000"}},
	}
	for _, tc := range cases {
		got := ParseInsecureHTTPHosts(tc.raw)
		if len(got) != len(tc.expected) {
			t.Fatalf("%s: hosts %v, want %v", tc.name, got, tc.expected)
		}
		for i, host := range got {
			if host != tc.expected[i] {
				t.Fatalf("%s: hosts %v, want %v", tc.name, got, tc.expected)
			}
		}
	}
}

// A plain-HTTP registry serves the recipe blob the Build worker reads. With
// the host declared, the worker reaches it; without a declaration the worker
// keeps its HTTPS-only behaviour.
func TestReadBlobReachesADeclaredPlainHTTPRegistry(t *testing.T) {
	payload := []byte("recipe-blob")
	objectDigest := "sha256:" + strings.TrimPrefix(digest(payload), "sha256:")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/oplcloud/recipe/blobs/"+objectDigest {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write(payload)
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")

	declared := &Runner{RegistryToken: "token", InsecureHTTPHosts: []string{host}, MaxPackageBytes: 1 << 20}
	read, err := declared.readBlob(context.Background(), host+"/oplcloud/recipe", objectDigest, 1<<20)
	if err != nil {
		t.Fatalf("a declared plain-HTTP registry was not reachable: %v", err)
	}
	if string(read) != string(payload) {
		t.Fatalf("read %q, want %q", read, payload)
	}

	// No declaration: the legacy switch still decides, and it is off in
	// production, so the plain-HTTP registry stays unreachable.
	undecided := &Runner{RegistryToken: "token", MaxPackageBytes: 1 << 20}
	if _, err := undecided.readBlob(context.Background(), host+"/oplcloud/recipe", objectDigest, 1<<20); err == nil {
		t.Fatal("an undeclared plain-HTTP registry was reachable")
	}

	// A declaration is per host: another registry keeps HTTPS.
	other := &Runner{RegistryToken: "token", InsecureHTTPHosts: []string{"registry.local-v2.test:5000"}, MaxPackageBytes: 1 << 20}
	if _, err := other.readBlob(context.Background(), host+"/oplcloud/recipe", objectDigest, 1<<20); err == nil {
		t.Fatal("a registry that was not declared reached over plain HTTP")
	}
}

// The declaration is authoritative for the host it names, including for a
// redirect target on that host: a registry that redirects its blob read over
// plain HTTP has to be reachable.
func TestReadBlobFollowsARedirectOnADeclaredHost(t *testing.T) {
	payload := []byte("redirected-blob")
	objectDigest := "sha256:" + strings.TrimPrefix(digest(payload), "sha256:")
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(payload)
	}))
	defer storage.Close()
	storageHost := strings.TrimPrefix(storage.URL, "http://")
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, storage.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer registry.Close()
	registryHost := strings.TrimPrefix(registry.URL, "http://")

	declared := &Runner{RegistryToken: "token", InsecureHTTPHosts: []string{registryHost, storageHost}, MaxPackageBytes: 1 << 20}
	if _, err := declared.readBlob(context.Background(), registryHost+"/oplcloud/recipe", objectDigest, 1<<20); err != nil {
		t.Fatalf("a redirect on a declared host was refused: %v", err)
	}

	undecided := &Runner{RegistryToken: "token", MaxPackageBytes: 1 << 20}
	if _, err := undecided.readBlob(context.Background(), registryHost+"/oplcloud/recipe", objectDigest, 1<<20); err == nil {
		t.Fatal("a plain-HTTP redirect was followed without a declaration")
	}
}

// A declared host has to be an exact host name: a value the worker cannot
// match would leave that host silently unreachable.
func TestValidateRefusesAMalformedInsecureHost(t *testing.T) {
	runner := &Runner{
		Builder:         "builder",
		RegistryPrefix:  "registry.local-v2.test/opl",
		StorageURL:      "https://cloud-local-v2:8443/objects",
		WorkDir:         "/var/lib/opl/build",
		DockerConfig:    "/var/lib/opl/build/docker",
		MaxPackageBytes: 1 << 20, MaxExpandedBytes: 1 << 20, MaxFiles: 1,
		Timeout:           1,
		InsecureHTTPHosts: []string{"not a host"},
	}
	if err := runner.Validate(); err == nil {
		t.Fatal("a malformed insecure registry host was accepted")
	}
}
