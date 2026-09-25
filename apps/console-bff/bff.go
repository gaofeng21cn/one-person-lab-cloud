// Package bff exposes the authenticated publisher handler used by the Console
// process and isolated cross-owner qualification.
package bff

import (
	"net/http"
	"opl-cloud/apps/console-bff/internal/clients"
	"opl-cloud/apps/console-bff/internal/httpapi"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
)

type IdentityReader = httpapi.IdentityReader

func NewPublisherHandler(capability api.CapabilityProductServiceClient, build api.BuildProductServiceClient, identity IdentityReader) http.Handler {
	return httpapi.NewPublisherHandler(capability, build, identity)
}

// IdentityClient is the same thin transport used by the BFF process.
type IdentityClient interface {
	IdentityReader
	httpapi.LoginReader
	Close()
}

func DialIdentity(address, token string, tls owneridentity.TLSConfig) (IdentityClient, error) {
	return clients.Dial(clients.Config{CloudIdentityAddr: address, CloudIdentityToken: token, TLS: tls})
}
