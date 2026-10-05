// Package httpapi composes the Workspace owner's own readback with the
// Serve-owned application facts the canonical Workspace read model derives.
//
// The canonical `Workspace` schema names `console-bff` as its owner and marks
// `applicationAvailability` and `accessUrl` as derived from the Serve-owned
// deployment, runtime instance and access readbacks. The Workspace owner stores
// no Agent deployment selection and therefore cannot answer those two fields on
// its own; the BFF composes them here, under the caller's own Serve decisions.
package httpapi

import (
	"context"
	"net/url"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"opl-cloud/apps/console-bff/internal/clients"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// applicationEntryReader is the Serve read surface the composed Workspace read
// needs: the deployment history that names the current application and the
// current application's own access facts. It is satisfied by the BFF's typed
// Serve client set and by a test double.
type applicationEntryReader interface {
	Deployments(ctx context.Context, workspaceID, cursor string, limit int32) (*api.DeploymentPage, error)
	WorkspaceAccess(ctx context.Context, workspaceID string) (*api.WorkspaceAccess, error)
}

// workspaceApplication returns the composed Workspace read: the Workspace owner's
// own facts unchanged, plus the application availability and access entry Serve
// confirms. A layer the BFF could not confirm is reported as the contract's
// `unknown` rather than replaced by an invented value, and the Workspace's own
// facts are never dropped because a second owner is unavailable.
func (s *Server) workspaceApplication(ctx context.Context, caller Caller, workspaceID string) (*api.Workspace, error) {
	workspace, err := s.workspace.GetWorkspace(ctx, &api.GetWorkspaceRpcRequest{Context: clients.CallContext(ctx), WorkspaceId: workspaceID})
	if err != nil {
		return nil, err
	}
	if workspace == nil || workspace.GetId() != workspaceID {
		return nil, errOwnerUnconfigured
	}
	serve, ok := s.reader.(applicationEntryReader)
	if !ok {
		// A deployment without the Serve read surface cannot compose the derived
		// fields at all; the Workspace owner's own readback stands unchanged rather
		// than being replaced by a guess.
		return workspace, nil
	}
	// The derived facts come from Serve's own records, so each read is authorized
	// as its own Serve action for this exact Workspace. A denied action is a real
	// authorization boundary and surfaces as such; it is never answered with an
	// invented availability.
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Serve,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_LISTDEPLOYMENTS,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspaceID)},
		clients.CallContext(ctx).GetRequestId()); err != nil {
		return nil, err
	}
	page, err := serve.Deployments(ctx, workspaceID, "", 0)
	if err != nil {
		// Serve's readback is not available, so its layer is genuinely unknown for
		// this response. The contract has no per-source reason field on Workspace,
		// and /api/v2/delivery/{workspaceId} reports the owner failure itself.
		workspace.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNKNOWN
		return workspace, nil
	}
	current := currentDeployment(page)
	if current == nil {
		// No Serve-owned deployment exists for this Workspace: the rule's legacy
		// bare-resource case, which is unavailable rather than pending.
		workspace.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNAVAILABLE
		return workspace, nil
	}
	switch current.GetStatus() {
	case api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ACTIVE:
		// Only the current active deployment can confirm readiness and admission.
	case api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_FAILED,
		api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_NEEDS_ATTENTION,
		api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_SUPERSEDED,
		api.DeploymentStatusEnum_DEPLOYMENT_STATUS_ENUM_ROLLED_BACK:
		workspace.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNAVAILABLE
		return workspace, nil
	default:
		// queued, deploying, verifying, rolling_back: the application is still
		// being delivered, so it is pending instead of failed or openable.
		workspace.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_PENDING
		return workspace, nil
	}
	if err := RequireAuthorizedAction(ctx, s.identity, caller, owneridentity.Serve,
		api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEACCESS,
		&api.AuthorizationResource{Kind: api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE, Id: proto.String(workspaceID)},
		clients.CallContext(ctx).GetRequestId()); err != nil {
		return nil, err
	}
	entry, err := serve.WorkspaceAccess(ctx, workspaceID)
	switch {
	case err != nil && applicationAccessUnavailable(err):
		// Serve answered that this application has no confirmed supported access
		// entry: the deployment is current, but it cannot be opened, so the
		// application is not available and no URL is reported.
		workspace.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNAVAILABLE
	case err != nil:
		// The access read is unresolved rather than refused, so this layer stays
		// unknown and the URL stays absent.
		workspace.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNKNOWN
	case entry == nil:
		workspace.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNKNOWN
	case validApplicationEntry(entry.GetUrl()):
		// Serve confirms both readiness and the supported access entry, so the URL
		// is reported and the application is openable.
		workspace.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_AVAILABLE
		workspace.AccessUrl = proto.String(strings.TrimSpace(entry.GetUrl()))
	default:
		// An entry without a usable absolute http(s) origin is not an openable
		// application entry, so nothing is reported as available.
		workspace.ApplicationAvailability = api.WorkspaceApplicationAvailabilityEnum_WORKSPACE_APPLICATION_AVAILABILITY_ENUM_UNKNOWN
	}
	return workspace, nil
}

// applicationAccessUnavailable reports Serve's own typed refusal that the current
// application has no confirmed supported access entry. Other failures are
// unresolved reads, which stay unknown instead of being presented as a decision.
func applicationAccessUnavailable(err error) bool {
	if code, ok := owneridentity.ErrorCode(err); ok && code == api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE {
		return true
	}
	return status.Code(err) == codes.FailedPrecondition
}

// validApplicationEntry mirrors Serve's own entry contract: an absolute http(s)
// origin with a hostname and no embedded credentials. Anything else is not an
// openable application entry.
func validApplicationEntry(raw string) bool {
	entry, err := url.Parse(strings.TrimSpace(raw))
	return err == nil && entry.Hostname() != "" && (entry.Scheme == "http" || entry.Scheme == "https") && entry.User == nil
}
