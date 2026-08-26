package controlplane

import (
	"context"
	"errors"
	"strings"

	"opl-cloud/services/control-plane/internal/clients"
)

// ReplaceWorkspaceRuntimeImage delegates the narrowly scoped provider
// capability. Authorization, owner-chain resolution, and the protected image
// policy remain in the HTTP/control-plane layer.
func (s *Service) ReplaceWorkspaceRuntimeImage(ctx context.Context, input clients.WorkspaceRuntimeImageReplacementInput, idempotencyKey string) (clients.WorkspaceRuntimeImageReplacementResult, error) {
	if s == nil || strings.TrimSpace(idempotencyKey) == "" || input.LaunchOperationID == "" || input.AccountID == "" || input.WorkspaceID == "" ||
		input.ComputeID == "" || input.StorageID == "" || input.AttachmentID == "" || input.RuntimeID == "" ||
		input.RuntimeOperationID == "" || input.RuntimeServiceName == "" || input.PreviousImageDigest == "" || input.ReplacementImageDigest == "" {
		return clients.WorkspaceRuntimeImageReplacementResult{}, errors.New("workspace_runtime_image_replacement_input_required")
	}
	client, ok := s.fabric.(clients.FabricWorkspaceRuntimeImageReplacementClient)
	if !ok {
		return clients.WorkspaceRuntimeImageReplacementResult{}, errors.New("workspace_runtime_image_replacement_unavailable")
	}
	return client.ReplaceWorkspaceRuntimeImage(ctx, input, idempotencyKey)
}
