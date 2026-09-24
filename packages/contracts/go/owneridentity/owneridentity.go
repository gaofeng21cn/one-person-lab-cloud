// Package owneridentity defines the wire convention one Cloud owner process uses
// to identify itself to another on an internal typed gRPC call.
//
// It is deliberately in the shared contracts module: the same convention is used
// by every domain owner, by the Console BFF backend, and by any future in-repo
// caller, so a single definition prevents a second spelling of the header names.
// It carries no business rule and no domain state.
package owneridentity

import "strings"

// TokenHeader carries the calling owner's bearer token.
const TokenHeader = "x-opl-owner-token"

// PeerHeader names the calling owner. It is advisory: it selects which configured
// token the caller must prove, so a caller cannot claim a peer identity it does
// not hold the token for.
const PeerHeader = "x-opl-owner"

// Owner is the canonical text identity of one Cloud owner process.
type Owner string

// The fixed Cloud owner names. They match the schema prefixes and the contract's
// OwnerEnum, and are the only accepted peer identities on the boundary.
const (
	Tenant          Owner = "tenant"
	Capability      Owner = "capability"
	Build           Owner = "build"
	Workspace       Owner = "workspace"
	RuntimeControl  Owner = "runtime_control"
	Serve           Owner = "serve"
	Fabric          Owner = "fabric"
	Gateway         Owner = "gateway"
	ResourceCatalog Owner = "resource_catalog"
	Ledger          Owner = "ledger"
)

// String returns the owner's canonical text form.
func (o Owner) String() string { return string(o) }

// Valid reports whether the owner name is one of the Cloud data owners.
func (o Owner) Valid() bool {
	switch o {
	case Tenant, Capability, Build, Workspace, RuntimeControl, Serve, Fabric, Gateway, ResourceCatalog, Ledger:
		return true
	default:
		return false
	}
}

// Parse resolves a text token to an owner, rejecting anything outside the set.
func Parse(value string) (Owner, bool) {
	owner := Owner(strings.ToLower(strings.TrimSpace(value)))
	if !owner.Valid() {
		return "", false
	}
	return owner, true
}

// MinimumTokenLength is the shortest accepted identity token. A shorter token is
// refused so a configured allowlist is never satisfied by a guessable value.
const MinimumTokenLength = 32
