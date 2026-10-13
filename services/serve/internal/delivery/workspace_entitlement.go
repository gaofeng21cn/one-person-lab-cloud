package delivery

import (
	"context"
	"errors"
	"time"

	api "opl-cloud/packages/contracts/go/api"
)

// WorkspaceEntitlement reads the current owner's verdict; no deadline or cached
// paid-through copy enters Serve. A bounded read also applies to streaming entry
// requests, whose client context otherwise has no deadline.
type WorkspaceEntitlement struct {
	Client api.WorkspaceAccessReadbackClient
}

func (g WorkspaceEntitlement) Verdict(ctx context.Context, workspaceID string) (EntitlementVerdict, error) {
	if g.Client == nil {
		return "", errors.New("Workspace entitlement owner unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	answer, err := g.Client.ReadWorkspaceAccess(ctx, &api.ReadWorkspaceAccessRequest{WorkspaceId: workspaceID})
	if err != nil {
		return "", err
	}
	if answer.GetWorkspaceId() != workspaceID {
		return "", errors.New("Workspace entitlement identity mismatch")
	}
	switch answer.GetVerdict() {
	case api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_PERMITTED:
		return EntitlementPermitted, nil
	case api.WorkspaceAccessVerdict_WORKSPACE_ACCESS_VERDICT_CLOSED:
		return EntitlementClosed, nil
	default:
		return EntitlementUnverified, nil
	}
}
