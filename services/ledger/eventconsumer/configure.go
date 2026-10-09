package eventconsumer

import (
	"strings"

	"google.golang.org/grpc"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/internal/ownerservice"
)

// ConfigureCoordination binds optional typed owner readbacks on the existing
// explicitly enabled Ledger listener. Missing dependencies fail the new RPCs
// closed; legacy HTTP and Build/Capability event receipt paths stay unchanged.
func (s *Server) ConfigureCoordination(config ownerservice.Config, getenv func(string) string) (func(), error) {
	var conns []*grpc.ClientConn
	close := func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}
	authorizer, identityConn, err := ownerservice.AuthorizerFromConfig(config)
	if err != nil {
		return close, err
	}
	if identityConn != nil {
		conns = append(conns, identityConn)
	}
	s.Authorizer = authorizer
	for _, owner := range []owneridentity.Owner{owneridentity.ResourceCatalog, owneridentity.Workspace, owneridentity.Gateway} {
		prefix := "OPL_" + strings.ToUpper(owner.String())
		address := strings.TrimSpace(getenv(prefix + "_ADDR"))
		if address == "" {
			continue
		}
		options, err := config.TLS.DialOptions(config.Owner.Service(), owneridentity.DeploymentUnitTransportPrincipal(owner), getenv(prefix+"_TOKEN"))
		if err != nil {
			close()
			return close, err
		}
		conn, err := grpc.NewClient(address, options...)
		if err != nil {
			close()
			return close, err
		}
		conns = append(conns, conn)
		switch owner {
		case owneridentity.ResourceCatalog:
			s.Catalog = api.NewCatalogCoordinationClient(conn)
		case owneridentity.Workspace:
			s.Workspace = api.NewOwnerCommitReadbackClient(conn)
		case owneridentity.Gateway:
			s.Gateway = api.NewGatewayCoordinationClient(conn)
		}
	}
	return close, nil
}
