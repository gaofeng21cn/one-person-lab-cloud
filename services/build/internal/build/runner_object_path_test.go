package build

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	api "opl-cloud/packages/contracts/go/api"
)

func TestRunnerRequiresObjectNamespaceBase(t *testing.T) {
	for _, tc := range []struct {
		suffix string
		valid  bool
	}{
		{"/objects", true}, {"/objects/", true}, {"/objects///", true},
		{"", false}, {"/", false}, {"/parts", false}, {"/other", false},
		{"/objects/objects", false}, {"/other/../objects", false},
		{"/%6fbjects", false}, {"/objects%2F", false},
		{"/objects?", false}, {"/objects?x=1", false},
		{"/objects#", false}, {"/objects#fragment", false},
	} {
		t.Run(tc.suffix, func(t *testing.T) {
			runner := &Runner{Builder: "isolated", RegistryPrefix: "registry.example.test/opl", StorageURL: "https://objects.example.test" + tc.suffix, WorkDir: t.TempDir(), DockerConfig: t.TempDir(), MaxPackageBytes: 1 << 20, MaxExpandedBytes: 1 << 20, MaxFiles: 1, Timeout: time.Second}
			if err := runner.Validate(); (err == nil) != tc.valid {
				t.Fatalf("Validate()=%v, valid=%v", err, tc.valid)
			}
		})
	}
}

// Validate the configured base before reading the bytes, including the allowed
// trailing slash, so a startup-accepted URL is exercised by its real consumer.
func TestPackageBytesReadsTheObjectNamespaceBase(t *testing.T) {
	for _, suffix := range []string{"/objects", "/objects/", "/objects///"} {
		t.Run(suffix, func(t *testing.T) {
			payload := []byte("package-bytes")
			hex := strings.TrimPrefix(digest(payload), "sha256:")
			var requested string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requested = r.URL.Path
				w.Write(payload)
			}))
			defer server.Close()
			runner := &Runner{Builder: "isolated", RegistryPrefix: "registry.example.test/opl", StorageURL: server.URL + suffix, StorageToken: "token", WorkDir: t.TempDir(), DockerConfig: t.TempDir(), MaxPackageBytes: 1 << 20, MaxExpandedBytes: 1 << 20, MaxFiles: 1, Timeout: time.Second, AllowHTTP: true}
			if err := runner.Validate(); err != nil {
				t.Fatal(err)
			}
			object := &api.SourceObjectReference{StorageObjectId: "sha256:" + hex, VersionId: "sha256:" + hex, Sha256: "sha256:" + hex, SizeBytes: int64(len(payload))}
			read, err := runner.packageBytes(context.Background(), object)
			if err != nil {
				t.Fatal(err)
			}
			if string(read) != string(payload) {
				t.Fatalf("read %q, want %q", read, payload)
			}
			if requested != "/objects/"+hex {
				t.Fatalf("the read asked for %q; the data plane serves /objects/{digest}", requested)
			}
		})
	}
}
