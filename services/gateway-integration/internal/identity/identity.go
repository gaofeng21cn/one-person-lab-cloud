// Package identity is the CloudIdentity module boundary inside this deployment
// unit. CloudIdentity (`tenant`) and Gateway Integration (`gateway`) share one
// Go module, one process and one deployment unit, but CloudIdentity owns the
// Tenant/actor mapping while the Gateway adapter owns authentication, so this
// package holds the seam between them and nothing else.
//
// It deliberately contains no password store, no session issuance and no generic
// permission framework: v2.26 keeps authentication in the Gateway and Tenant
// authorization in the `tenant` schema, and a second auth system here would be a
// second authority. The real behaviour depends on the Gateway identity contract
// that W03 owns, so this package only declares the typed seam W03 implements.
// Nothing here may fabricate an authorization decision.
package identity

import (
	"context"
	"errors"
)

// GatewaySubjectID is the opaque Gateway identity reference the Gateway contract
// attests for an authenticated member (the specification's
// `invitee_gateway_subject_id` / `external_identity_ref` subject). CloudIdentity
// never authenticates a password itself.
type GatewaySubjectID string

// ActorID is the Cloud actor id a Gateway subject maps to: the actor recorded in
// `tenant.tenant_members` and `tenant.sessions`.
type ActorID string

// ErrUnimplemented reports that the Gateway identity contract is not implemented
// yet. It is returned instead of guessing an actor, so an absent Gateway
// attestation can never be mistaken for an authenticated identity.
var ErrUnimplemented = errors.New("gateway subject resolution is implemented by W03 against the Gateway contract")

// SubjectResolver resolves an already-authenticated Gateway subject into the
// Cloud actor it authenticates as. W03 implements it against the Gateway
// contract; until then the seam must fail closed rather than return an actor.
type SubjectResolver interface {
	ResolveActor(ctx context.Context, subject GatewaySubjectID) (ActorID, error)
}
