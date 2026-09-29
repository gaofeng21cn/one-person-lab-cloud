package fabric

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// SecretBindInput is the Fabric-owned request to bind one Agent runtime's
// Gateway Secret into the resource set its deployment actually consumed. It
// carries only opaque identities: the runtime instance, the approved Secret
// delivery reference and its fingerprint. The raw credential never travels here;
// the provider reads it from the approved store and confirms the exact identity.
type SecretBindInput struct {
	AccountID         string
	WorkspaceID       string
	RuntimeInstanceID string
	SecretRef         string
	Purpose           string
	Fingerprint       string
	TargetSlot        string
}

// SecretBindResult is the provider-confirmed binding identity: the Secret
// reference, its exact version and fingerprint. It is a readback of the approved
// store, never a caller claim.
type SecretBindResult struct {
	SecretRef   string
	Version     string
	Fingerprint string
}

// workspaceApplicationSecretBinder is the optional port a provider implements to
// confirm an already-written approved-store Secret for a runtime. A provider that
// cannot read the store reports an error instead of a fabricated binding.
type workspaceApplicationSecretBinder interface {
	BindWorkspaceApplicationSecret(context.Context, SecretBindInput) (SecretBindResult, error)
}

// BindWorkspaceApplicationSecret confirms the exact approved-store Secret named by
// the input and returns its provider-observed version and fingerprint. It fails
// closed when the provider holds no such Secret, when the stored identity differs
// from the requested account/workspace/reference/fingerprint, or when no binder
// is configured for this provider. Nothing is written and no raw value is read.
func (s *Service) BindWorkspaceApplicationSecret(ctx context.Context, input SecretBindInput) (SecretBindResult, error) {
	input.WorkspaceID = strings.TrimSpace(input.WorkspaceID)
	input.RuntimeInstanceID = strings.TrimSpace(input.RuntimeInstanceID)
	input.SecretRef = strings.TrimSpace(input.SecretRef)
	input.Purpose = strings.TrimSpace(input.Purpose)
	input.Fingerprint = strings.TrimSpace(input.Fingerprint)
	input.AccountID = strings.TrimSpace(input.AccountID)
	if input.AccountID == "" || input.WorkspaceID == "" || input.RuntimeInstanceID == "" || input.SecretRef == "" || input.Purpose == "" || input.Fingerprint == "" {
		return SecretBindResult{}, fmt.Errorf("workspace_application_secret_binding_input_required")
	}
	binder := s.optionalProviders.applicationSecretBinder
	if binder == nil {
		return SecretBindResult{}, errors.New("workspace_application_secret_binder_unavailable")
	}
	return binder.BindWorkspaceApplicationSecret(ctx, input)
}
