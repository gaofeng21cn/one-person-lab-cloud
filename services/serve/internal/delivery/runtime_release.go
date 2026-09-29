package delivery

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
)

// runtimeRelease confirms the exact approved Runtime Release behind a default OPL
// App selection. The list is cursor-paged, so the whole catalog is read until the
// exact id is found; a release that is not approved is refused rather than
// substituted. No CapabilityVersion, Package or Build lineage is involved.
func (s *Service) runtimeRelease(ctx context.Context, call *api.CallContext, runtimeVersionID string) (*api.RuntimeVersion, error) {
	if s.RuntimeReleases == nil {
		return nil, status.Error(codes.Unavailable, "Runtime Control is not configured")
	}
	if runtimeVersionID == "" {
		return nil, status.Error(codes.InvalidArgument, "a default OPL App selection requires a runtime version")
	}
	cursor := ""
	for {
		page, err := s.RuntimeReleases.ListRuntimeVersions(ctx, &api.ListRuntimeVersionsRpcRequest{Context: call, QueryCursor: &cursor})
		if err != nil {
			return nil, err
		}
		for _, release := range page.GetItems() {
			if release.GetId() != runtimeVersionID {
				continue
			}
			if release.GetStatus() != api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED {
				return nil, status.Error(codes.FailedPrecondition, "selected Runtime Release is not approved")
			}
			if release.GetPublisherContract() == nil || release.GetPublisherContract().GetImage() == nil || release.GetPublisherContract().GetApplicationRevisionTemplate() == nil {
				return nil, status.Error(codes.FailedPrecondition, "Runtime Release has no immutable application contract")
			}
			return release, nil
		}
		if page.GetNextCursor() == "" {
			return nil, status.Error(codes.NotFound, "Runtime Release not found")
		}
		cursor = page.GetNextCursor()
	}
}
