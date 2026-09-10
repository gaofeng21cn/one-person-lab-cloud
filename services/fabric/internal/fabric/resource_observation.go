package fabric

import (
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

// observationFromFacts maps provider states at their Fabric boundary. It never
// changes the legacy validation facts or infers absence from a failed read.
func observationFromFacts(facts ProviderResourceFacts) *contracts.ResourceObservation {
	state := contracts.ResourceObservedUnknown
	switch strings.ToLower(strings.TrimSpace(facts.Status)) {
	case "ready", "available", "provider_ready":
		state = contracts.ResourceObservedReady
	case "running", "active":
		state = contracts.ResourceObservedRunning
	case "stopped", "exited":
		state = contracts.ResourceObservedStopped
	case "suspended":
		state = contracts.ResourceObservedSuspended
	case "attached":
		state = contracts.ResourceObservedAttached
	case "detached", "unattached", "retained", "released":
		state = contracts.ResourceObservedDetached
	case "pending", "unready", "starting", "stopping", "creating", "provisioning", "attaching", "detaching", "restarting", "deleting", "destroying":
		state = contracts.ResourceObservedPending
	case "external_deleted", "deleted", "destroyed", "missing", "not_found":
		state = contracts.ResourceObservedAbsent
	}
	observation := &contracts.ResourceObservation{State: state, Available: state != contracts.ResourceObservedUnknown}
	if !observation.Available {
		observation.ReasonCode = "provider_state_unrecognized"
		return observation
	}
	observation.ProviderID, observation.PackageOrSpec, observation.Zone = facts.ProviderID, facts.PackageOrSpec, facts.Zone
	observation.CreatedAt, observation.ExpiresAt = facts.CreatedAt, facts.ExpiresAt
	return observation
}
