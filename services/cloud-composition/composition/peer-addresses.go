package composition

import (
	"fmt"
	"net/url"
)

// configuredPeerEnvironment resolves the retained HTTP peers through the same
// target listener facts the plan probes. It changes only configured caller
// origins; credentials and unconfigured optional peers retain their own state.
func configuredPeerEnvironment(topology Topology, caller Process, getenv func(string) string) (map[string]string, error) {
	resolved := map[string]string{}
	for _, peer := range topology.PeerAddresses.Entries {
		if peer.Caller != caller.TransportPrincipal {
			continue
		}
		if topology.PeerAddresses.SchemaVersion != 1 {
			return nil, fmt.Errorf("owner %s: unsupported peer address schema", caller.Service)
		}
		if peer.Activation != "configured" {
			return nil, fmt.Errorf("owner %s: unsupported peer activation for %s", caller.Service, peer.AddressEnv)
		}
		active, err := edgeAddressConfigured(Edge{AddressEnv: peer.AddressEnv,
			ConfiguredAddressSemantics: peer.ConfiguredAddressSemantics}, getenv)
		if err != nil {
			return nil, err
		}
		if !active {
			continue
		}
		target, found := processByPrincipal(topology, peer.Target)
		if !found {
			return nil, fmt.Errorf("owner %s: peer %s names unknown target %q", caller.Service, peer.AddressEnv, peer.Target)
		}
		schemeDeclared := false
		for _, scheme := range topology.PeerAddresses.OriginSchemes {
			schemeDeclared = schemeDeclared || scheme == peer.OriginScheme
		}
		if !schemeDeclared || (peer.OriginScheme != "http" && peer.OriginScheme != "https") {
			return nil, fmt.Errorf("owner %s: peer %s has no supported declared HTTP scheme", caller.Service, peer.AddressEnv)
		}
		address, err := ResolveAddress(target, peer.TargetAddressEnv, getenv)
		if err != nil {
			return nil, err
		}
		peerAddress, err := loopbackPeerAddress(address)
		if err != nil {
			return nil, fmt.Errorf("owner %s: peer %s target listener is invalid: %w", caller.Service, peer.AddressEnv, err)
		}
		if _, duplicate := resolved[peer.AddressEnv]; duplicate {
			return nil, fmt.Errorf("owner %s: peer address %s is declared twice", caller.Service, peer.AddressEnv)
		}
		resolved[peer.AddressEnv] = (&url.URL{Scheme: peer.OriginScheme,
			Host: peerAddress}).String()
	}
	return resolved, nil
}
