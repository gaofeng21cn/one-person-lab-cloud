package fabric

import (
	"context"
	"errors"
	"os"
	"strings"
)

type workspaceApplicationPreflightProvider interface {
	PreflightWorkspaceApplicationRuntime(context.Context, WorkspaceApplicationRuntimeInput, ComputeAllocation, StorageVolume) error
}

func (s *Service) PreflightWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput) error {
	input.IdempotencyKey = input.RuntimeOperationID
	s.mu.Lock()
	compute := s.computes[input.ComputeID]
	volume := s.volumes[input.VolumeID]
	attachment := s.attachments[input.AttachmentID]
	s.mu.Unlock()
	if err := s.validateWorkspaceApplicationRuntimeInput(input, compute, volume, attachment); err != nil {
		return errors.Join(ErrWorkspaceApplicationRuntimeInputInvalid, err)
	}
	if err := s.validateApplicationDataLayout(ctx, input); err != nil {
		return err
	}
	provider, ok := s.runtimeProvider.(workspaceApplicationPreflightProvider)
	if !ok {
		return ErrWorkspaceApplicationRuntimeProviderUnsupported
	}
	ctx, err := s.applicationCredentialContext(ctx, input)
	if err != nil {
		return err
	}
	return provider.PreflightWorkspaceApplicationRuntime(ctx, input, compute, volume)
}
func (p *LocalDockerProvider) PreflightWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume) error {
	if err := p.validateLocalDockerApplicationRuntimeIdentity(input, compute); err != nil {
		return err
	}
	if err := p.validateLocalDockerApplicationProbe(input.Revision); err != nil {
		return err
	}
	if _, err := p.readStorageDirectories(volume); err != nil {
		return err
	}
	if err := validateWorkspaceApplicationConfiguration(input); err != nil {
		return err
	}
	if err := p.validateApplicationExecutions(input); err != nil {
		return err
	}
	for _, dependency := range input.Revision.Dependencies {
		if _, _, err := p.applicationDeclaredSecrets(input, dependency.SecretInputs); err != nil {
			return err
		}
	}
	if _, _, err := p.applicationSecretFiles(input); err != nil {
		return err
	}
	if input.Revision.RuntimeProfile == "opl_app" {
		if _, err := applicationWebUICredentials(input); err != nil {
			return err
		}
	}
	return nil
}
func (p *TencentProvider) PreflightWorkspaceApplicationRuntime(ctx context.Context, input WorkspaceApplicationRuntimeInput, compute ComputeAllocation, volume StorageVolume) error {
	if err := p.validateInstallationConfig(); err != nil {
		return err
	}
	if err := validateTencentApplicationCapabilities(input.Revision); err != nil {
		return err
	}
	for _, binding := range input.SecretBindings {
		if _, err := p.readApplicationBoundSecret(ctx, input, binding); err != nil {
			return err
		}
	}
	if input.Revision.RuntimeProfile == "opl_app" {
		if _, err := workspaceApplicationGatewayBinding(input); err != nil {
			return err
		}
		if strings.TrimSpace(os.Getenv("OPL_AIONUI_ADMIN_PASSWORD_SEED")) == "" {
			return errors.New("workspace_application_webui_credential_seed_required")
		}
		if _, err := tencentApplicationCredentialToken(ctx, input); err != nil {
			return err
		}
	}
	return nil
}
