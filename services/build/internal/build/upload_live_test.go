//go:build livebuild

package build

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/capability/catalog"
	capabilitymigrations "opl-cloud/services/capability/migrations"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore/ownerstoretest"
)

// CloudIdentity and Capability run their real handlers and restricted stores.
// Only external Sub2API identity and existing test membership are fixtures.
func uploadLivePackage(t *testing.T, ctx context.Context, dsn string, data []byte) (*api.SourceObjectReference, string, string, string, string, *catalog.Service, string, *liveIdentity) {
	t.Helper()
	identity := newLiveIdentity(t, ctx, dsn)
	h, err := ownerstoretest.Setup(ctx, ownerstoretest.Config{AdminDSN: dsn, Owner: "capability", Database: "opl_capability", SchemaOwnerRole: "opl_capability_owner", WriterRole: "opl_capability_writer", RuntimeRole: "opl_capability_runtime"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { h.Close(context.Background()) })
	source, err := capabilitymigrations.Source()
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Install(ctx, h.OwnerDSN, h.DatabaseName(), source); err != nil {
		t.Fatal(err)
	}
	db, err := h.Open(ctx, h.RuntimeDSN, h.DatabaseName())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	root := t.TempDir()
	schema := []byte(`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}},"additionalProperties":false}`)
	schemaPath := filepath.Join(root, "schema.json")
	os.WriteFile(schemaPath, schema, 0600)
	httpServer := httptest.NewUnstartedServer(nil)
	httpURL := "http://" + httpServer.Listener.Addr().String()
	objects, err := catalog.NewObjects(root, httpURL, bytes.Repeat([]byte("x"), 32), catalog.UploadPolicy{MaxBytes: 1 << 20, PartBytes: 128, MaxExpandedBytes: 2 << 20, MaxFiles: 20, TTL: time.Minute, ManifestPath: "manifest.json", SchemaPath: schemaPath, SchemaDigest: digest(schema)})
	if err != nil {
		t.Fatal(err)
	}
	service, err := catalog.New(db, ownerservice.NewAuthorizer(owneridentity.Capability, identity.auth(t, owneridentity.Capability)).Authorize, objects)
	if err != nil {
		t.Fatal(err)
	}
	token := "isolated-build-object-token-0000000000"
	handler, err := service.DataHandler(token)
	if err != nil {
		t.Fatal(err)
	}
	httpServer.Config.Handler = handler
	httpServer.Start()
	t.Cleanup(httpServer.Close)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer(grpc.UnaryInterceptor(func(c context.Context, r any, _ *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		peer := owneridentity.ConsoleBFF
		if values := metadata.ValueFromIncomingContext(c, "test-peer"); len(values) > 0 {
			peer = owneridentity.Service(values[0])
		}
		return next(ownerservice.WithPeerOwner(c, peer), r)
	}))
	api.RegisterCapabilityProductServiceServer(server, service)
	api.RegisterCapabilityCoordinationServer(server, service)
	api.RegisterDomainInboxServer(server, service)
	go server.Serve(listener)
	t.Cleanup(server.Stop)
	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	client := &publisherCapabilityClient{t: t, base: newPublisherHTTP(t, api.NewCapabilityProductServiceClient(conn), nil, identity), identity: identity}
	call := func(key string) *api.CallContext { return identity.call(key, "tenant-live") }

	ns, err := client.CreateNamespace(ctx, &api.CreateNamespaceRpcRequest{Context: call("namespace"), Body: &api.NamespaceWriteRequest{Name: "live-package"}})
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := client.CreatePackage(ctx, &api.CreatePackageRpcRequest{Context: call("package"), Body: &api.CreatePackageRequest{NamespaceId: ns.Id, Name: "live-package"}})
	if err != nil {
		t.Fatal(err)
	}
	foreign := call("foreign")
	foreign.Scope.GetTenant().TenantId = "another-tenant"
	if _, err = client.GetPackage(ctx, &api.GetPackageRpcRequest{Context: foreign, PackageId: pkg.Id}); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("cross-tenant read accepted: %v", err)
	}
	upload, err := client.CreateUpload(ctx, &api.CreateUploadRpcRequest{Context: call("upload"), PackageId: pkg.Id, Body: &api.CreateUploadRequest{VersionLabel: "v1", FileName: "package.zip", SizeBytes: int64(len(data)), Sha256: digest(data)}})
	if err != nil {
		t.Fatal(err)
	}
	var parts []*api.UploadPart
	for start, number := 0, int32(1); start < len(data); number++ {
		end := start + int(upload.PartSizeBytes)
		if end > len(data) {
			end = len(data)
		}
		chunk := data[start:end]
		part, err := client.CreateUploadPart(ctx, &api.CreateUploadPartRpcRequest{Context: call(fmt.Sprint("part-", number)), UploadId: upload.Id, Body: &api.CreateUploadPartRequest{PartNumber: number, SizeBytes: int64(len(chunk)), Sha256: digest(chunk)}})
		if err != nil {
			t.Fatal(err)
		}
		put := func(body []byte) int {
			q, _ := http.NewRequestWithContext(ctx, "PUT", part.Url, bytes.NewReader(body))
			q.Header.Set(part.RequiredChecksumHeaderName, part.RequiredChecksumHeaderValue)
			resp, e := http.DefaultClient.Do(q)
			if e != nil {
				t.Fatal(e)
			}
			defer resp.Body.Close()
			return resp.StatusCode
		}
		if number == 1 && put(bytes.Repeat([]byte("x"), len(chunk))) != 400 {
			t.Fatal("corrupt upload accepted")
		}
		if put(chunk) != 204 || put(chunk) != 204 {
			t.Fatal("part upload/retry failed")
		}
		parts = append(parts, &api.UploadPart{PartNumber: number, Etag: strings.TrimPrefix(digest(chunk), "sha256:"), SizeBytes: int64(len(chunk)), Sha256: digest(chunk)})
		start = end
	}
	req := &api.CompleteUploadRpcRequest{Context: call("complete"), UploadId: upload.Id, Body: &api.CompleteUploadRequest{Parts: parts}}
	operation, err := client.CompleteUpload(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if operation.Status != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("upload failed: %v", operation)
	}
	replay, err := client.CompleteUpload(ctx, req)
	if err != nil || replay.OperationId != operation.OperationId {
		t.Fatalf("complete replay: %v", err)
	}
	req.Context = call("complete-new-key")
	replay, err = client.CompleteUpload(ctx, req)
	if err != nil || replay.OperationId != operation.OperationId {
		t.Fatalf("completion with fresh key: %v", err)
	}
	req.Context = call("complete-different-parts")
	changed := proto.Clone(req.Body).(*api.CompleteUploadRequest)
	changed.Parts[0].Sha256 = digest([]byte("different"))
	req.Body = changed
	if _, err = client.CompleteUpload(ctx, req); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("conflicting completion accepted: %v", err)
	}
	version, err := client.GetPackageVersion(ctx, &api.GetPackageVersionRpcRequest{Context: call("read"), PackageVersionId: upload.PackageVersionId})
	if err != nil {
		t.Fatal(err)
	}
	if version.Status != api.PackageVersionStatusEnum_PACKAGE_VERSION_STATUS_ENUM_UPLOADED {
		t.Fatal("not uploaded")
	}
	objectURL := httpURL + "/objects/" + strings.TrimPrefix(version.Sha256, "sha256:")
	response, err := http.Get(objectURL)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("object read did not require service credential")
	}
	q, _ := http.NewRequestWithContext(ctx, "GET", objectURL, nil)
	q.Header.Set("Authorization", "Bearer "+token)
	response, err = http.DefaultClient.Do(q)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != 200 || !bytes.Equal(actual, data) {
		t.Fatal("confirmed object bytes differ")
	}
	t.Logf("Capability wire upload complete: %d replayed parts, idempotent completion, corrupt bytes and cross-tenant reads rejected", len(parts))
	return &api.SourceObjectReference{StorageObjectId: version.Sha256, VersionId: version.Sha256, Sha256: version.Sha256, SizeBytes: version.SizeBytes}, httpURL, token, pkg.Id, version.Id, service, listener.Addr().String(), identity
}
