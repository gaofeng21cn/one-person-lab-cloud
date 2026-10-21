// The Console BFF's operator activation composition surface.
//
// Activation is the one operator command the BFF composes across owners: the
// retained Control Plane activates the account and its Gateway user, the Tenant
// owner admits the Tenant whose owner is that Gateway subject, and the Gateway
// owner binds its billing. This file exposes the dialed composition and the
// authenticated handler for an isolated cross-owner qualification, which a caller
// outside apps/console-bff cannot reach directly because the handler and the
// transport live in internal packages. It mirrors the Gateway, Serve and Catalog
// pass-throughs for the same reason.
package bff

import (
	"context"
	"net/http"

	"opl-cloud/apps/console-bff/internal/clients"
	"opl-cloud/apps/console-bff/internal/httpapi"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// OperatorActivationClient is a dialed activation composition: the BFF's own
// delivery reads, the CloudIdentity session/authorization boundary, the Tenant
// owner, the Gateway owner's binding surface, and the retained Control Plane
// activation command. It is a bff-owned interface so a caller outside this module
// never names the internal transport type.
type OperatorActivationClient interface {
	httpapi.OwnerReader
	IdentityReader
	httpapi.LoginReader
	TenantClient() api.TenantProductServiceClient
	GatewayCoordinationClient() api.GatewayCoordinationClient
	OperatorProvisionAccount(context.Context, string, string, []byte) ([]byte, error)
	Close()
}

// NewOperatorActivationHandler exposes the BFF's authenticated routes over the
// supplied composition. It is the same handler the process serves, so a
// qualification drives the real activation route and its guards.
func NewOperatorActivationHandler(reader OperatorActivationClient) http.Handler {
	return httpapi.NewServer(reader, reader).Handler()
}

// DialOperatorActivationReader dials the Tenant and Gateway owners plus the
// CloudIdentity boundary for the activation composition, and points the retained
// activation command at the supplied Control Plane origin.
func DialOperatorActivationReader(controlPlaneURL, controlPlaneToken, address, ownerToken, cloudIdentityToken string, tls owneridentity.TLSConfig) (OperatorActivationClient, error) {
	return clients.Dial(clients.Config{
		ControlPlaneURL:    controlPlaneURL,
		ControlPlaneToken:  controlPlaneToken,
		Addresses:          map[owneridentity.Owner]string{owneridentity.Gateway: address},
		Tokens:             map[owneridentity.Owner]string{owneridentity.Gateway: ownerToken},
		CloudIdentityAddr:  address,
		CloudIdentityToken: cloudIdentityToken,
		TLS:                tls,
	})
}
