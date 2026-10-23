package tkeapply

import (
	"context"
	contracts "opl-cloud/packages/contracts/go"
)

func (p *Executor) ReadBoundSecret(ctx context.Context, input WorkspaceApplicationRuntimeInput, binding contracts.WorkspaceApplicationRuntimeSecretBinding) (string, error) {
	return p.readApplicationBoundSecret(ctx, input, binding)
}
func (p *Executor) PrepareConfigFiles(ctx context.Context, input WorkspaceApplicationRuntimeInput) error {
	return p.prepareApplicationConfigFiles(ctx, input)
}
