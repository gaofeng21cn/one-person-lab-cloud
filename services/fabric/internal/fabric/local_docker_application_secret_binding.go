package fabric

import (
	"context"
	"fmt"
	"strings"
)

// BindWorkspaceApplicationSecret confirms the exact approved-store Secret for a
// runtime from the local provider's own Gateway Secret store. Its stored metadata
// must name the same account, workspace and reference, and its own digest must
// equal the requested fingerprint; the raw key is read only to verify the digest
// and is never returned.
func (p *LocalDockerProvider) BindWorkspaceApplicationSecret(ctx context.Context, input SecretBindInput) (SecretBindResult, error) {
	if input.SecretRef != gatewaySecretName(input.WorkspaceID) || strings.TrimSpace(input.Fingerprint) == "" {
		return SecretBindResult{}, fmt.Errorf("workspace_application_secret_binding_mismatch")
	}
	metadata, err := p.gatewayMetadata(ctx, input.SecretRef)
	if err != nil {
		return SecretBindResult{}, err
	}
	if metadata.AccountID != input.AccountID || metadata.WorkspaceID != input.WorkspaceID || metadata.SecretRef != input.SecretRef || metadata.Fingerprint != input.Fingerprint || metadata.Version == "" {
		return SecretBindResult{}, ErrLaunchStageBindingConflict
	}
	return SecretBindResult{SecretRef: metadata.SecretRef, Version: metadata.Version, Fingerprint: metadata.Fingerprint}, nil
}
