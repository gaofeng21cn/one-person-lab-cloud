package delivery

// Expiry is not a Serve fact. Workspace owns Workspace identity, membership,
// entitlement, resource plan and target authorization (docs/opl-serve.md,
// "Ownership"; docs/architecture.md lists Workspace entitlement among the facts
// Serve does not own). Serve nevertheless runs the request that a closed term is
// supposed to stop, so the data plane needs the owner's answer at the moment it
// resolves a route. The answer has to arrive from the owner: recomputing a term
// inside Serve would create the second entitlement authority this repository
// already forbids, and a periodic copy of one would be a cross-service sync
// mechanism this package must not invent on its own.
//
// EntitlementGate is therefore the narrow shape of that answer: one verdict per
// Workspace and nothing else — no term, no deadline comparison, no stored copy.
// No owner-side implementation is declared here on purpose. Which owner fact
// backs the gate, how it reaches the data plane, and whether an unanswered
// verdict may still be served are owner decisions; this file only makes the
// question askable and the answer decidable.

import "context"

// EntitlementVerdict is the owner's answer to one question: may this
// Workspace's data plane serve a request right now.
type EntitlementVerdict string

const (
	// EntitlementUnverified means the owner has not answered: no term was ever
	// published, the answer is not reachable, or the Workspace predates the
	// answer existing. It is deliberately not the same value as
	// EntitlementClosed, so "we were not told" can never be silently read as
	// "not allowed" and "not allowed" can never be silently read as "we were
	// not told".
	EntitlementUnverified EntitlementVerdict = "unverified"
	// EntitlementPermitted means the owner confirmed the Workspace may be
	// served.
	EntitlementPermitted EntitlementVerdict = "permitted"
	// EntitlementClosed means the owner withdrew the entitlement — the paid term
	// ended and was not renewed. It is refused even when the binding is
	// confirmed and the runtime is ready: readiness describes the instance, not
	// the right to reach it.
	EntitlementClosed EntitlementVerdict = "closed"
)

// EntitlementGate is Serve's single entitlement input at the data plane. An
// implementation reads the owner's verdict; it does not own the term. Returning
// an error is a failed read of the owner's answer, which the entry refuses as
// unavailable rather than guessing either verdict.
type EntitlementGate interface {
	Verdict(ctx context.Context, workspaceID string) (EntitlementVerdict, error)
}
