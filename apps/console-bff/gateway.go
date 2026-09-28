// The Console BFF's Gateway read composition surface.
//
// The process reaches the Gateway owner through the shared client set, so this
// file carries no second dial path. It exposes only the authenticated handler and
// the dialer for an isolated cross-owner composition, which a caller outside
// apps/console-bff cannot reach directly because the handler and the transport
// live in internal packages. It mirrors the Serve and Resource Catalog
// pass-throughs for the same reason.
package bff

import (
	"net/http"

	"opl-cloud/apps/console-bff/internal/clients"
	"opl-cloud/apps/console-bff/internal/httpapi"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// GatewayReadClient is a dialed Gateway read surface plus the CloudIdentity
// session/authorization boundary, which Gateway Integration serves in the same
// deployment unit. It is a bff-owned interface so a caller outside this module
// never names the internal transport type.
type GatewayReadClient interface {
	IdentityReader
	httpapi.LoginReader
	GatewayClient() api.GatewayProductServiceClient
	Close()
}

// NewGatewayReadHandler exposes the authenticated Gateway product reads (wallet
// and model catalog) over the supplied owner client and identity authority.
func NewGatewayReadHandler(gateway api.GatewayProductServiceClient, identity IdentityReader) http.Handler {
	return httpapi.NewGatewayReadHandler(gateway, identity)
}

// DialGatewayReader dials Gateway Integration for the wallet/model read surface
// and the CloudIdentity boundary, reusing the same thin transport, identity and
// per-owner token the process uses, so a test drives the real client path.
func DialGatewayReader(address, ownerToken, cloudIdentityToken string, tls owneridentity.TLSConfig) (GatewayReadClient, error) {
	return clients.Dial(clients.Config{
		Addresses:          map[owneridentity.Owner]string{owneridentity.Gateway: address},
		Tokens:             map[owneridentity.Owner]string{owneridentity.Gateway: ownerToken},
		CloudIdentityAddr:  address,
		CloudIdentityToken: cloudIdentityToken,
		TLS:                tls,
	})
}
