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

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/requestcontext"
	"opl-cloud/packages/contracts/go/transporttls"
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

// Config holds the owner addresses, this process's service token, and
// the CloudIdentity session/authorization boundary.
type Config struct {
	Addresses          map[owneridentity.Owner]string
	CloudIdentityAddr  string
	CloudIdentityToken string
	ServiceToken       string
	TLS                transporttls.Config
}

// ConfigFromEnv resolves owner addresses and BFF service tokens from the
// environment.
func ConfigFromEnv(getenv func(string) string) Config {
	return Config{
		Addresses: map[owneridentity.Owner]string{
			owneridentity.Capability: strings.TrimSpace(getenv("OPL_CAPABILITY_URL")),
			owneridentity.Build:      strings.TrimSpace(getenv("OPL_BUILD_URL")),
			owneridentity.Workspace:  strings.TrimSpace(getenv("OPL_WORKSPACE_URL")),
			owneridentity.Serve:      strings.TrimSpace(getenv("OPL_SERVE_URL")),
		},
		CloudIdentityAddr:  strings.TrimSpace(getenv(CloudIdentityAddressEnv)),
		CloudIdentityToken: strings.TrimSpace(getenv(CloudIdentityTokenEnv)),
		ServiceToken:       strings.TrimSpace(getenv("OPL_BFF_TOKEN")),
		TLS:                transporttls.FromEnv(getenv, "OPL_BFF_MTLS"),
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
	if err := config.TLS.Validate(); err != nil {
		return nil, fmt.Errorf("BFF mTLS: %w", err)
	}
	if config.TLS.Empty() {
		return nil, errors.New("BFF mTLS is required for domain-owner connections")
	}
	if strings.TrimSpace(config.ServiceToken) == "" {
		return nil, errors.New("OPL_BFF_TOKEN is required for domain-owner connections")
	}
	if strings.TrimSpace(config.CloudIdentityAddr) != "" && strings.TrimSpace(config.CloudIdentityToken) == "" {
		return nil, errors.New("OPL_CLOUD_IDENTITY_TOKEN is required for CloudIdentity")
	}
	for _, owner := range ReachableOwners() {
		addr := strings.TrimSpace(config.Addresses[owner])
		if addr == "" {
			continue
		}
		credentials, err := config.TLS.ClientCredentials()
		if err != nil {
			clients.Close()
			return nil, fmt.Errorf("mTLS for %s: %w", owner, err)
		}
		options := []grpc.DialOption{grpc.WithTransportCredentials(credentials)}
		options = append(options, grpc.WithChainUnaryInterceptor(owneridentity.OutboundServiceInterceptor(owneridentity.ConsoleBFF, config.ServiceToken)))
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
		credentials, err := config.TLS.ClientCredentials()
		if err != nil {
			clients.Close()
			return nil, fmt.Errorf("mTLS for CloudIdentity: %w", err)
		}
		options := []grpc.DialOption{grpc.WithTransportCredentials(credentials)}
		options = append(options, grpc.WithChainUnaryInterceptor(owneridentity.OutboundServiceInterceptor(owneridentity.ConsoleBFF, config.CloudIdentityToken)))
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
	call, err := requiredCallContext(ctx)
	if err != nil {
		return nil, err
	}
	return c.workspace.GetWorkspace(ctx, &api.GetWorkspaceRpcRequest{Context: call, WorkspaceId: workspaceID})
}

// Deployments lists the Serve-owned deployment attempts for one Workspace.
func (c *Clients) Deployments(ctx context.Context, workspaceID string) (*api.DeploymentPage, error) {
	if c.serve == nil {
		return nil, fmt.Errorf("serve: %w", ErrUpstreamUnconfigured)
	}
	call, err := requiredCallContext(ctx)
	if err != nil {
		return nil, err
	}
	return c.serve.ListDeployments(ctx, &api.ListDeploymentsRpcRequest{Context: call, WorkspaceId: workspaceID})
}

// WorkspaceAccess reads the Serve-owned access facts for one Workspace.
func (c *Clients) WorkspaceAccess(ctx context.Context, workspaceID string) (*api.WorkspaceAccess, error) {
	if c.serve == nil {
		return nil, fmt.Errorf("serve: %w", ErrUpstreamUnconfigured)
	}
	call, err := requiredCallContext(ctx)
	if err != nil {
		return nil, err
	}
	return c.serve.GetWorkspaceAccess(ctx, &api.GetWorkspaceAccessRpcRequest{Context: call, WorkspaceId: workspaceID})
}

// Build reads one Build job from the Build owner.
func (c *Clients) Build(ctx context.Context, buildID string) (*api.BuildJob, error) {
	if c.build == nil {
		return nil, fmt.Errorf("build: %w", ErrUpstreamUnconfigured)
	}
	call, err := requiredCallContext(ctx)
	if err != nil {
		return nil, err
	}
	return c.build.GetBuild(ctx, &api.GetBuildRpcRequest{Context: call, BuildId: buildID})
}

// CapabilityVersion reads one capability version from the Capability owner.
func (c *Clients) CapabilityVersion(ctx context.Context, capabilityVersionID string) (*api.CapabilityVersion, error) {
	if c.capability == nil {
		return nil, fmt.Errorf("capability: %w", ErrUpstreamUnconfigured)
	}
	call, err := requiredCallContext(ctx)
	if err != nil {
		return nil, err
	}
	return c.capability.GetCapabilityVersion(ctx, &api.GetCapabilityVersionRpcRequest{Context: call, CapabilityVersionId: capabilityVersionID})
}

// Operation reads one operation from the single owner named by the request. The
// caller resolves the finite owner enum before this call, so no scan is performed.
func (c *Clients) Operation(ctx context.Context, owner owneridentity.Owner, operationID string) (*api.Operation, error) {
	client, ok := c.owner[owner]
	if !ok || client == nil {
		return nil, fmt.Errorf("%s: %w", owner, ErrUpstreamUnconfigured)
	}
	call, err := requiredCallContext(ctx)
	if err != nil {
		return nil, err
	}
	return client.Read(ctx, &api.OwnerOperationRequest{Context: call, OperationId: operationID})
}

func requiredCallContext(ctx context.Context) (*api.CallContext, error) {
	call := requestcontext.CallContext(ctx)
	if call == nil || strings.TrimSpace(call.GetRequestId()) == "" || strings.TrimSpace(call.GetActorId()) == "" || call.GetScope() == nil {
		return nil, errors.New("authorized CallContext is required for owner reads")
	}
	return call, nil
}
