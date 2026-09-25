package clients

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

func (c *Clients) LoginContext(ctx context.Context) (*api.LoginContext, string, error) {
	if c.tenant == nil {
		return nil, "", fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	var header metadata.MD
	out, e := c.tenant.GetLoginContext(ctx, &api.GetLoginContextRpcRequest{}, grpc.Header(&header))
	return out, cookie(header), e
}
func (c *Clients) Login(ctx context.Context, challenge string, input *api.LoginRequest) (*api.Session, string, error) {
	if c.tenant == nil {
		return nil, "", fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	var header metadata.MD
	out, e := c.tenant.Login(ctx, &api.LoginRpcRequest{Context: &api.CallContext{SessionId: &challenge}, Body: input}, grpc.Header(&header))
	return out, cookie(header), e
}
func (c *Clients) Logout(ctx context.Context, ref string) error {
	if c.tenant == nil {
		return fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	_, e := c.tenant.Logout(ctx, &api.LogoutRpcRequest{Context: &api.CallContext{SessionId: &ref}})
	return e
}
func cookie(header metadata.MD) string {
	v := header.Get(owneridentity.SessionCookieHeader)
	if len(v) != 1 {
		return ""
	}
	return v[0]
}
