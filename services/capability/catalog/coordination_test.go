package catalog

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
)

type runtimeCatalogClient struct {
	api.RuntimeControlProductServiceClient
	pages []*api.RuntimeVersionPage
	calls int
}

func (c *runtimeCatalogClient) ListRuntimeVersions(context.Context, *api.ListRuntimeVersionsRpcRequest, ...grpc.CallOption) (*api.RuntimeVersionPage, error) {
	page := c.pages[c.calls]
	c.calls++
	return page, nil
}

func TestRuntimeVersionRequiresExplicitVersionToBeApproved(t *testing.T) {
	approved := &api.RuntimeVersion{Id: "runtime-approved", Status: api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED}
	revoked := &api.RuntimeVersion{Id: "runtime-revoked", Status: api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_REVOKED}
	for _, test := range []struct {
		name     string
		id       string
		rows     []*api.RuntimeVersion
		wantID   string
		wantCode codes.Code
	}{
		{name: "exact approved release", id: approved.Id, rows: []*api.RuntimeVersion{approved}, wantID: approved.Id},
		{name: "exact revoked release rejected", id: revoked.Id, rows: []*api.RuntimeVersion{revoked}, wantCode: codes.FailedPrecondition},
		{name: "default picks only approved release", rows: []*api.RuntimeVersion{revoked, {Id: "runtime-default", Status: api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED, DefaultForNewBuilds: true}}, wantID: "runtime-default"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := &runtimeCatalogClient{pages: []*api.RuntimeVersionPage{{Items: test.rows}}}
			service := &Service{Runtime: client}
			got, err := service.runtimeVersion(context.Background(), nil, test.id)
			if status.Code(err) != test.wantCode {
				t.Fatalf("error code=%s, want %s (err=%v)", status.Code(err), test.wantCode, err)
			}
			if err == nil && got.GetId() != test.wantID {
				t.Fatalf("runtime id=%q, want %q", got.GetId(), test.wantID)
			}
		})
	}
}
