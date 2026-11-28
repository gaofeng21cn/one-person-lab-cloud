package composition

import (
	"fmt"
	"net"
	"strconv"
)

// ownerListenerEnvironment resolves each owner's effective bind inputs. A
// single-container installation must supply addresses that it can really bind;
// service DNS names from a multi-container installation are not rewritten.
func ownerListenerEnvironment(process Process, getenv func(string) string) (map[string]string, error) {
	enabled, err := rpcSurfaceEnabled(process, getenv)
	if err != nil {
		return nil, err
	}
	resolved := map[string]string{}
	for _, name := range process.listenerAddressEnvs() {
		if name == process.RPCEnabledByEnv && !enabled {
			continue
		}
		address, err := ResolveAddress(process, name, getenv)
		if err != nil {
			return nil, err
		}
		resolved[name] = address
	}
	return resolved, nil
}

// configuredOwnerPeerEnvironment resolves active gRPC peers from the same
// effective target listener that the owner binds and readiness probes use.
func configuredOwnerPeerEnvironment(topology Topology, caller Process, getenv func(string) string) (map[string]string, error) {
	resolved := map[string]string{}
	for _, edge := range topology.Communication.Edges {
		if edge.Caller != caller.TransportPrincipal {
			continue
		}
		active, err := edgeActive(topology, edge, caller, getenv)
		if err != nil {
			return nil, err
		}
		if !active {
			continue
		}
		target, found := processByPrincipal(topology, edge.Target)
		if !found {
			return nil, fmt.Errorf("owner %s: peer %s names unknown target %q", caller.Service, edge.AddressEnv, edge.Target)
		}
		name := target.peerListenerEnv(edge.AddressEnv)
		if name == "" {
			return nil, fmt.Errorf("owner %s: peer %s names target %q, which declares no listener for it", caller.Service, edge.AddressEnv, edge.Target)
		}
		address, err := ResolveAddress(target, name, getenv)
		if err != nil {
			return nil, err
		}
		peer, err := loopbackPeerAddress(address)
		if err != nil {
			return nil, fmt.Errorf("owner %s: peer %s target listener is invalid: %w", caller.Service, edge.AddressEnv, err)
		}
		if existing, declared := resolved[edge.AddressEnv]; declared && existing != peer {
			return nil, fmt.Errorf("owner %s: peer address %s names two different listeners", caller.Service, edge.AddressEnv)
		}
		resolved[edge.AddressEnv] = peer
	}
	return resolved, nil
}

// loopbackPeerAddress turns a declared listener into the address a caller in
// the same shared container reaches it at. A listener that binds every
// interface of the container is reached through its loopback interface.
func loopbackPeerAddress(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil || portErr != nil || portNumber < 1 || portNumber > 65535 {
		return "", fmt.Errorf("invalid listener address %q", address)
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return net.JoinHostPort(host, port), nil
}
