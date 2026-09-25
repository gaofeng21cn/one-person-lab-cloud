package bff

import (
	"net/http"

	"opl-cloud/apps/console-bff/internal/clients"
	"opl-cloud/apps/console-bff/internal/httpapi"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// ServeDeliveryReader is the BFF's typed Serve read surface (deployment history,
// one deployment, the current Agent's access state).
type ServeDeliveryReader = httpapi.ServeDeliveryReader

// ServeReadClient is a dialed Serve read surface the process and a test can both
// use. It is a bff-owned interface so a caller outside this module never names
// the internal transport type.
type ServeReadClient interface {
	ServeDeliveryReader
	Close()
}

// NewServeDeliveryHandler exposes the authenticated Serve delivery read routes
// over the supplied reader and identity authority. The returned handler is what
// the shared server registers; the mux wiring itself is the identity
// integrator's.
func NewServeDeliveryHandler(reader ServeDeliveryReader, identity IdentityReader) http.Handler {
	return httpapi.NewServeDeliveryHandler(reader, identity)
}

// DialServeReader dials the Serve owner for the Serve read surface, reusing the
// same thin transport, identity and per-owner token the process uses, so a test
// drives the real client path.
func DialServeReader(address, token string, tls owneridentity.TLSConfig) (ServeReadClient, error) {
	return clients.Dial(clients.Config{
		Addresses: map[owneridentity.Owner]string{owneridentity.Serve: address},
		Tokens:    map[owneridentity.Owner]string{owneridentity.Serve: token},
		TLS:       tls,
	})
}
