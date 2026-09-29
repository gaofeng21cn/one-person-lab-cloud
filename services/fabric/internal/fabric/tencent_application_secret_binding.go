package fabric

import (
	"context"
	"fmt"
	"strings"
)

// BindWorkspaceApplicationSecret confirms the exact approved-store Secret named by
// the binding input. The Gateway Secret is the only managed credential the platform
// injects, so the reference must be this Workspace's own Gateway Secret and its
// stored identity must match the requested account, workspace and fingerprint. The
// readback is the provider's observed identity, never a caller claim, and the raw
// key is never returned.
func (p *TencentProvider) BindWorkspaceApplicationSecret(ctx context.Context, input SecretBindInput) (SecretBindResult, error) {
	if input.SecretRef != gatewaySecretName(input.WorkspaceID) || strings.TrimSpace(input.Fingerprint) == "" || len(strings.TrimPrefix(input.Fingerprint, "sha256:")) != 64 {
		return SecretBindResult{}, fmt.Errorf("workspace_application_secret_binding_mismatch")
	}
	identity, err := p.workspaceGatewaySecretIdentity(ctx, input.WorkspaceID)
	if err != nil {
		return SecretBindResult{}, err
	}
	if identity.AccountID != input.AccountID || identity.SecretRef != input.SecretRef || identity.Fingerprint != input.Fingerprint {
		return SecretBindResult{}, ErrLaunchStageBindingConflict
	}
	version := strings.TrimPrefix(input.Fingerprint, "sha256:")[:16]
	if strings.TrimSpace(version) == "" {
		return SecretBindResult{}, ErrLaunchStageBindingConflict
	}
	return SecretBindResult{SecretRef: identity.SecretRef, Version: version, Fingerprint: identity.Fingerprint}, nil
}
