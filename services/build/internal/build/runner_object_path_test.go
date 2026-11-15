package build

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	api "opl-cloud/packages/contracts/go/api"
)

// The object namespace base already carries the /objects prefix the Capability
// data plane serves, so a read appends only the digest. Appending the
// namespace again asks for /objects/objects/{digest}, which no route serves:
// the data plane reads an object at /objects/{digest}.
func TestPackageBytesReadsTheObjectNamespaceBase(t *testing.T) {
	payload := []byte("package-bytes")
	hex := strings.TrimPrefix(digest(payload), "sha256:")
	var requested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.Path
		w.Write(payload)
	}))
	defer server.Close()

	runner := &Runner{StorageURL: server.URL + "/objects", StorageToken: "token", MaxPackageBytes: 1 << 20}
	object := &api.SourceObjectReference{
		StorageObjectId: "sha256:" + hex,
		VersionId:       "sha256:" + hex,
		Sha256:          "sha256:" + hex,
		SizeBytes:       int64(len(payload)),
	}
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
}
