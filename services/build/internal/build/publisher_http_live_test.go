//go:build livebuild

package build

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"opl-cloud/apps/console-bff"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

type publisherIdentity struct{}

func (publisherIdentity) Session(_ context.Context, id string) (*api.Session, error) {
	tenant := "tenant-live"
	if id == "other-publisher" {
		tenant = "another-tenant"
	} else if id != "isolated-publisher" {
		return nil, status.Error(codes.Unauthenticated, "unknown session")
	}
	return &api.Session{ActorId: "publisher", TenantId: proto.String(tenant), CsrfToken: "isolated-csrf", ExpiresAt: timestamppb.New(time.Now().Add(time.Hour))}, nil
}
func (publisherIdentity) Authorize(ctx context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	d, err := (fixtureIdentity{}).AuthorizeAction(ctx, r)
	if err == nil {
		d.AuthorizationContextId = proto.String("fixture-authorization")
	}
	return d, err
}
func newPublisherHTTP(t *testing.T, cap api.CapabilityProductServiceClient, build api.BuildProductServiceClient) string {
	t.Helper()
	server := httptest.NewServer(bff.NewPublisherHandler(cap, build, publisherIdentity{}))
	t.Cleanup(server.Close)
	for _, test := range []struct {
		cookie, csrf string
		want         int
	}{{"", "", 401}, {"isolated-publisher", "wrong", 403}} {
		req, _ := http.NewRequest("POST", server.URL+"/api/v2/namespaces", bytes.NewBufferString(`{"name":"must-not-create"}`))
		if test.cookie != "" {
			req.AddCookie(&http.Cookie{Name: "opl_session", Value: test.cookie})
		}
		req.Header.Set("x-opl-csrf", test.csrf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "must-not-create")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != test.want {
			t.Fatalf("BFF authorization rejection=%d want=%d", resp.StatusCode, test.want)
		}
	}
	return server.URL
}
func publisherRequest(ctx context.Context, base, method, path string, call *api.CallContext, body, result proto.Message) error {
	var raw []byte
	var err error
	if body != nil {
		raw, err = publicjson.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	cookie := "isolated-publisher"
	if call.GetScope().GetTenant().GetTenantId() == "another-tenant" {
		cookie = "other-publisher"
	}
	req.AddCookie(&http.Cookie{Name: "opl_session", Value: cookie})
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-opl-csrf", "isolated-csrf")
	req.Header.Set("Idempotency-Key", call.GetIdempotencyKey())
	req.Header.Set("x-opl-request-id", call.GetRequestId())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		code := codes.Internal
		switch resp.StatusCode {
		case 400:
			code = codes.InvalidArgument
		case 401:
			code = codes.Unauthenticated
		case 403:
			code = codes.PermissionDenied
		case 404:
			code = codes.NotFound
		case 409:
			code = codes.AlreadyExists
		case 422:
			code = codes.FailedPrecondition
		}
		return status.Errorf(code, "BFF status %d: %s", resp.StatusCode, data)
	}
	return publicjson.Unmarshal(data, result)
}

type publisherCapabilityClient struct {
	api.CapabilityProductServiceClient
	t    *testing.T
	base string
}
type publisherBuildClient struct {
	api.BuildProductServiceClient
	t    *testing.T
	base string
}

func (c *publisherCapabilityClient) CreateNamespace(ctx context.Context, r *api.CreateNamespaceRpcRequest, _ ...grpc.CallOption) (*api.Namespace, error) {
	out := &api.Namespace{}
	err := publisherRequest(ctx, c.base, "POST", "/api/v2/namespaces", r.Context, r.Body, out)
	return out, err
}

func (c *publisherCapabilityClient) CreatePackage(ctx context.Context, r *api.CreatePackageRpcRequest, _ ...grpc.CallOption) (*api.Package, error) {
	out := &api.Package{}
	err := publisherRequest(ctx, c.base, "POST", "/api/v2/packages", r.Context, r.Body, out)
	return out, err
}

func (c *publisherCapabilityClient) GetPackage(ctx context.Context, r *api.GetPackageRpcRequest, _ ...grpc.CallOption) (*api.Package, error) {
	out := &api.Package{}
	err := publisherRequest(ctx, c.base, "GET", "/api/v2/packages/"+r.PackageId, r.Context, nil, out)
	return out, err
}

func (c *publisherCapabilityClient) CreateUpload(ctx context.Context, r *api.CreateUploadRpcRequest, _ ...grpc.CallOption) (*api.UploadSession, error) {
	out := &api.UploadSession{}
	err := publisherRequest(ctx, c.base, "POST", "/api/v2/packages/"+r.PackageId+"/uploads", r.Context, r.Body, out)
	return out, err
}

func (c *publisherCapabilityClient) GetUpload(ctx context.Context, r *api.GetUploadRpcRequest, _ ...grpc.CallOption) (*api.UploadSession, error) {
	out := &api.UploadSession{}
	err := publisherRequest(ctx, c.base, "GET", "/api/v2/uploads/"+r.UploadId, r.Context, nil, out)
	return out, err
}

func (c *publisherCapabilityClient) CreateUploadPart(ctx context.Context, r *api.CreateUploadPartRpcRequest, _ ...grpc.CallOption) (*api.UploadPartAuthorization, error) {
	out := &api.UploadPartAuthorization{}
	err := publisherRequest(ctx, c.base, "POST", "/api/v2/uploads/"+r.UploadId+"/parts", r.Context, r.Body, out)
	return out, err
}

func (c *publisherCapabilityClient) CompleteUpload(ctx context.Context, r *api.CompleteUploadRpcRequest, _ ...grpc.CallOption) (*api.Operation, error) {
	out := &api.Operation{}
	err := publisherRequest(ctx, c.base, "POST", "/api/v2/uploads/"+r.UploadId+"/complete", r.Context, r.Body, out)
	return out, err
}

func (c *publisherBuildClient) CreateBuild(ctx context.Context, r *api.CreateBuildRpcRequest, _ ...grpc.CallOption) (*api.BuildJob, error) {
	out := &api.BuildJob{}
	err := publisherRequest(ctx, c.base, "POST", "/api/v2/builds", r.Context, r.Body, out)
	return out, err
}

func (c *publisherBuildClient) GetBuild(ctx context.Context, r *api.GetBuildRpcRequest, _ ...grpc.CallOption) (*api.BuildJob, error) {
	out := &api.BuildJob{}
	err := publisherRequest(ctx, c.base, "GET", "/api/v2/builds/"+r.BuildId, r.Context, nil, out)
	return out, err
}

func (c *publisherCapabilityClient) GetPackageVersion(ctx context.Context, r *api.GetPackageVersionRpcRequest, _ ...grpc.CallOption) (*api.PackageVersion, error) {
	out := &api.PackageVersion{}
	err := publisherRequest(ctx, c.base, "GET", "/api/v2/package-versions/"+r.PackageVersionId, r.Context, nil, out)
	return out, err
}
