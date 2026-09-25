package clients

import (
	"context"
	"fmt"

	api "opl-cloud/packages/contracts/go/api"
)

// TenantClient is the CloudIdentity TenantProductService surface the BFF reaches.
// The BFF owns none of these facts; every value below is read from, or written
// by, the CloudIdentity owner.
func (c *Clients) TenantClient() api.TenantProductServiceClient {
	return c.tenant
}

func (c *Clients) Tenant(ctx context.Context, call *api.CallContext) (*api.Tenant, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	return c.tenant.GetTenant(ctx, &api.GetTenantRpcRequest{Context: call})
}

func (c *Clients) Members(ctx context.Context, call *api.CallContext, cursor string) (*api.MemberPage, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	request := &api.ListMembersRpcRequest{Context: call}
	if cursor != "" {
		request.QueryCursor = &cursor
	}
	return c.tenant.ListMembers(ctx, request)
}

func (c *Clients) Invitations(ctx context.Context, call *api.CallContext, cursor string) (*api.InvitationPage, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	request := &api.ListInvitationsRpcRequest{Context: call}
	if cursor != "" {
		request.QueryCursor = &cursor
	}
	return c.tenant.ListInvitations(ctx, request)
}

func (c *Clients) InviteMember(ctx context.Context, call *api.CallContext, body *api.InviteMemberRequest) (*api.Invitation, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	return c.tenant.InviteMember(ctx, &api.InviteMemberRpcRequest{Context: call, Body: body})
}

func (c *Clients) AcceptInvitation(ctx context.Context, call *api.CallContext, invitationID string) (*api.Member, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	return c.tenant.AcceptInvitation(ctx, &api.AcceptInvitationRpcRequest{Context: call, InvitationId: invitationID})
}

func (c *Clients) RevokeInvitation(ctx context.Context, call *api.CallContext, invitationID string) (*api.Invitation, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	return c.tenant.RevokeInvitation(ctx, &api.RevokeInvitationRpcRequest{Context: call, InvitationId: invitationID})
}

func (c *Clients) UpdateMemberRole(ctx context.Context, call *api.CallContext, memberID string, body *api.UpdateMemberRoleRequest) (*api.Member, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	return c.tenant.UpdateMemberRole(ctx, &api.UpdateMemberRoleRpcRequest{Context: call, MemberId: memberID, Body: body})
}

func (c *Clients) RemoveMember(ctx context.Context, call *api.CallContext, memberID string) error {
	if c.tenant == nil {
		return fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	_, err := c.tenant.RemoveMember(ctx, &api.RemoveMemberRpcRequest{Context: call, MemberId: memberID})
	return err
}
