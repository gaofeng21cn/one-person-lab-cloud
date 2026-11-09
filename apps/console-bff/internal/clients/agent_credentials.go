package clients

import (
	"context"
	"fmt"

	api "opl-cloud/packages/contracts/go/api"
)

func (c *Clients) IssueAgentCredential(ctx context.Context, request *api.IssueAgentCredentialRpcRequest) (*api.IssuedAgentCredential, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	return c.tenant.IssueAgentCredential(ctx, request)
}

func (c *Clients) ResolveAgentCredential(ctx context.Context, request *api.ResolveAgentCredentialRequest) (*api.ResolvedAgentCredential, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	return c.tenant.ResolveAgentCredential(ctx, request)
}

func (c *Clients) ListAgentCredentials(ctx context.Context, request *api.ListAgentCredentialsRpcRequest) (*api.AgentCredentialPage, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	return c.tenant.ListAgentCredentials(ctx, request)
}

func (c *Clients) RevokeAgentCredential(ctx context.Context, request *api.RevokeAgentCredentialRpcRequest) error {
	if c.tenant == nil {
		return fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	_, err := c.tenant.RevokeAgentCredential(ctx, request)
	return err
}
