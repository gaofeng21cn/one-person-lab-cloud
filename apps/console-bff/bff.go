// Package bff exposes the authenticated publisher handler used by the Console
// process and isolated cross-owner qualification.
package bff

import (
	"net/http"
	"opl-cloud/apps/console-bff/internal/httpapi"
	api "opl-cloud/packages/contracts/go/api"
)

type IdentityReader = httpapi.IdentityReader

func NewPublisherHandler(capability api.CapabilityProductServiceClient, build api.BuildProductServiceClient, identity IdentityReader) http.Handler {
	return httpapi.NewPublisherHandler(capability, build, identity)
}
