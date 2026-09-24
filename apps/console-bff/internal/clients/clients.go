// Package clients holds the BFF's typed gRPC clients to the Cloud domain owners.
//
// The BFF owns no business data. Every value it serves comes from the owner that
// wrote it, so a missing or unavailable owner is reported as an upstream failure
// rather than replaced by a default.
package clients

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// ErrUpstreamUnconfigured reports that a required domain owner address is not
// configured. The BFF refuses to answer rather than inventing that owner's facts.
var ErrUpstreamUnconfigured = errors.New("domain owner address is not configured")

// CloudIdentityAddressEnv and CloudIdentityTokenEnv configure the CloudIdentity
// module. It serves CloudIdentity's own two data owners (tenant and gateway) from
// one deployment unit, so the BFF dials it once for the browser session and the
// authorization decision rather than as a fifth data owner.
const (
	CloudIdentityAddressEnv = "OPL_CLOUD_IDENTITY_URL"
	CloudIdentityTokenEnv   = "OPL_CLOUD_IDENTITY_TOKEN"
)

// Config holds the owner addresses, this process's identity token per owner, and
// the CloudIdentity session/authorization boundary.
type Config struct {
	Addresses          map[owneridentity.Owner]string
	Tokens             map[owneridentity.Owner]string
	CloudIdentityAddr  string
	CloudIdentityToken string
}

// ConfigFromEnv resolves the owner addresses and per-owner tokens from the
// environment.
func ConfigFromEnv(getenv func(string) string) Config {
	return Config{
		Addresses: map[owneridentity.Owner]string{
			owneridentity.Capability: strings.TrimSpace(getenv("OPL_CAPABILITY_URL")),
			owneridentity.Build:      strings.TrimSpace(getenv("OPL_BUILD_URL")),
			owneridentity.Workspace:  strings.TrimSpace(getenv("OPL_WORKSPACE_URL")),
			owneridentity.Serve:      strings.TrimSpace(getenv("OPL_SERVE_URL")),
		},
		Tokens: map[owneridentity.Owner]string{
			owneridentity.Capability: strings.TrimSpace(getenv("OPL_CAPABILITY_TOKEN")),
			owneridentity.Build:      strings.TrimSpace(getenv("OPL_BUILD_TOKEN")),
			owneridentity.Workspace:  strings.TrimSpace(getenv("OPL_WORKSPACE_TOKEN")),
			owneridentity.Serve:      strings.TrimSpace(getenv("OPL_SERVE_TOKEN")),
		},
		CloudIdentityAddr:  strings.TrimSpace(getenv(CloudIdentityAddressEnv)),
		CloudIdentityToken: strings.TrimSpace(getenv(CloudIdentityTokenEnv)),
	}
}

// ReachableOwners is the finite owner set the BFF can call. It bounds operation
// routing as the accepted contract requires; there is no global registry or
// cross-service scan.
func ReachableOwners() []owneridentity.Owner {
	return []owneridentity.Owner{
		owneridentity.Capability,
		owneridentity.Build,
		owneridentity.Workspace,
		owneridentity.Serve,
	}
}

// Clients holds one typed client per owner the BFF reaches, plus the
// CloudIdentity session and authorization boundary.
type Clients struct {
	capability    api.CapabilityProductServiceClient
	build         api.BuildProductServiceClient
	workspace     api.WorkspaceProductServiceClient
	serve         api.ServeProductServiceClient
	tenant        api.TenantProductServiceClient
	authorization api.CloudIdentityAuthorizationClient
	owner         map[owneridentity.Owner]api.OwnerOperationsClient
	conns         []*grpc.ClientConn
}

// Dial opens one connection per configured owner. An owner that is not configured
// is simply absent; a call to it returns ErrUpstreamUnconfigured.
func Dial(config Config) (*Clients, error) {
	clients := &Clients{owner: make(map[owneridentity.Owner]api.OwnerOperationsClient)}
	for _, owner := range ReachableOwners() {
		addr := strings.TrimSpace(config.Addresses[owner])
		if addr == "" {
			continue
		}
		options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
		if token := strings.TrimSpace(config.Tokens[owner]); token != "" {
			options = append(options, grpc.WithChainUnaryInterceptor(owneridentity.OutboundInterceptor(owner, token)))
		}
		conn, err := grpc.NewClient(addr, options...)
		if err != nil {
			clients.Close()
			return nil, fmt.Errorf("dial %s at %s: %w", owner, addr, err)
		}
		clients.conns = append(clients.conns, conn)
		clients.owner[owner] = api.NewOwnerOperationsClient(conn)
		switch owner {
		case owneridentity.Capability:
			clients.capability = api.NewCapabilityProductServiceClient(conn)
		case owneridentity.Build:
			clients.build = api.NewBuildProductServiceClient(conn)
		case owneridentity.Workspace:
			clients.workspace = api.NewWorkspaceProductServiceClient(conn)
		case owneridentity.Serve:
			clients.serve = api.NewServeProductServiceClient(conn)
		}
	}
	if addr := strings.TrimSpace(config.CloudIdentityAddr); addr != "" {
		options := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
		if token := strings.TrimSpace(config.CloudIdentityToken); token != "" {
			// CloudIdentity presents its own identity on the boundary; the BFF calls
			// it as an in-repo peer with the configured token.
			options = append(options, grpc.WithChainUnaryInterceptor(owneridentity.OutboundInterceptor(owneridentity.Tenant, token)))
		}
		conn, err := grpc.NewClient(addr, options...)
		if err != nil {
			clients.Close()
			return nil, fmt.Errorf("dial CloudIdentity at %s: %w", addr, err)
		}
		clients.conns = append(clients.conns, conn)
		clients.tenant = api.NewTenantProductServiceClient(conn)
		clients.authorization = api.NewCloudIdentityAuthorizationClient(conn)
	}
	return clients, nil
}

// Session reads the caller's own CloudIdentity session. The browser session id is
// carried in CallContext, so CloudIdentity validates the live session itself rather
// than trusting an identity the BFF asserts.
func (c *Clients) Session(ctx context.Context, sessionID string) (*api.Session, error) {
	if c.tenant == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	callContext := &api.CallContext{SessionId: &sessionID}
	return c.tenant.GetSession(ctx, &api.GetSessionRpcRequest{Context: callContext})
}

// Authorize asks CloudIdentity to decide one action for one actor and resource.
func (c *Clients) Authorize(ctx context.Context, request *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	if c.authorization == nil {
		return nil, fmt.Errorf("CloudIdentity: %w", ErrUpstreamUnconfigured)
	}
	return c.authorization.AuthorizeAction(ctx, request)
}

// Close releases the owner connections.
func (c *Clients) Close() {
	for _, conn := range c.conns {
		_ = conn.Close()
	}
	c.conns = nil
}

// Workspace reads one Workspace from the Workspace owner.
func (c *Clients) Workspace(ctx context.Context, workspaceID string) (*api.Workspace, error) {
	if c.workspace == nil {
		return nil, fmt.Errorf("workspace: %w", ErrUpstreamUnconfigured)
	}
	return c.workspace.GetWorkspace(ctx, &api.GetWorkspaceRpcRequest{WorkspaceId: workspaceID})
}

// Deployments lists the Serve-owned deployment attempts for one Workspace.
func (c *Clients) Deployments(ctx context.Context, workspaceID string) (*api.DeploymentPage, error) {
	if c.serve == nil {
		return nil, fmt.Errorf("serve: %w", ErrUpstreamUnconfigured)
	}
	return c.serve.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{WorkspaceId: workspaceID})
}

// WorkspaceAccess reads the Serve-owned access facts for one Workspace.
func (c *Clients) WorkspaceAccess(ctx context.Context, workspaceID string) (*api.WorkspaceAccess, error) {
	if c.serve == nil {
		return nil, fmt.Errorf("serve: %w", ErrUpstreamUnconfigured)
	}
	return c.serve.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{WorkspaceId: workspaceID})
}

// Build reads one Build job from the Build owner.
func (c *Clients) Build(ctx context.Context, buildID string) (*api.BuildJob, error) {
	if c.build == nil {
		return nil, fmt.Errorf("build: %w", ErrUpstreamUnconfigured)
	}
	return c.build.GetBuild(ctx, &api.GetBuildRpcRequest{BuildId: buildID})
}

// CapabilityVersion reads one capability version from the Capability owner.
func (c *Clients) CapabilityVersion(ctx context.Context, capabilityVersionID string) (*api.CapabilityVersion, error) {
	if c.capability == nil {
		return nil, fmt.Errorf("capability: %w", ErrUpstreamUnconfigured)
	}
	return c.capability.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{CapabilityVersionId: capabilityVersionID})
}

// Operation reads one operation from the single owner named by the request. The
// caller resolves the finite owner enum before this call, so no scan is performed.
func (c *Clients) Operation(ctx context.Context, owner owneridentity.Owner, operationID string) (*api.Operation, error) {
	client, ok := c.owner[owner]
	if !ok || client == nil {
		return nil, fmt.Errorf("%s: %w", owner, ErrUpstreamUnconfigured)
	}
	return client.Read(ctx, &api.OwnerOperationRequest{OperationId: operationID})
}
