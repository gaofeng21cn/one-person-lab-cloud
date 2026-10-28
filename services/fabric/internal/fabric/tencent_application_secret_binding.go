package fabric

import (
	"context"
	"fmt"
	"strings"

	"opl-cloud/services/internal/protectedresource"
)

// BindWorkspaceApplicationSecret confirms the exact approved-store Secret named by
// the binding input. The Gateway Secret is the only managed credential the platform
// injects, so the reference must be this Workspace's own Gateway Secret and its
// stored identity must match the requested workspace and fingerprint. The provider
// account is the confirmed resource-set identity. A legacy Secret written before
// that distinction was enforced may carry the old tenant value in its account
// annotation; after proving the exact Secret bytes, this path repairs only that
// annotation before returning the binding. It never changes the key, version,
// workspace or Gateway identity.
func (p *TencentProvider) BindWorkspaceApplicationSecret(ctx context.Context, input SecretBindInput) (SecretBindResult, error) {
	if input.SecretRef != gatewaySecretName(input.WorkspaceID) || strings.TrimSpace(input.Fingerprint) == "" || len(strings.TrimPrefix(input.Fingerprint, "sha256:")) != 64 {
		return SecretBindResult{}, fmt.Errorf("workspace_application_secret_binding_mismatch")
	}
	identity, err := p.workspaceGatewaySecretIdentity(ctx, input.WorkspaceID)
	if err != nil {
		return SecretBindResult{}, err
	}
	if identity.SecretRef != input.SecretRef || identity.Fingerprint != input.Fingerprint {
		return SecretBindResult{}, ErrLaunchStageBindingConflict
	}
	if identity.AccountID != input.AccountID {
		if _, err := p.callKubectl(ctx, []string{"annotate", "secret/" + input.SecretRef, "oplcloud.cn/account-id=" + input.AccountID, "--overwrite"}, nil, protectedresource.Target{}); err != nil {
			return SecretBindResult{}, err
		}
		identity, err = p.workspaceGatewaySecretIdentity(ctx, input.WorkspaceID)
		if err != nil || identity.AccountID != input.AccountID || identity.Fingerprint != input.Fingerprint {
			return SecretBindResult{}, ErrLaunchStageBindingConflict
		}
	}
	version := strings.TrimPrefix(input.Fingerprint, "sha256:")[:16]
	if strings.TrimSpace(version) == "" {
		return SecretBindResult{}, ErrLaunchStageBindingConflict
	}
	return SecretBindResult{SecretRef: identity.SecretRef, Version: version, Fingerprint: identity.Fingerprint}, nil
}
