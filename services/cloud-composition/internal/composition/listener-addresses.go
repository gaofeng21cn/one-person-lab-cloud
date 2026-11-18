package composition

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// The composition resolves every address the contract declares through the
// contract.
//
// The shared container is one network namespace. The published installation
// expresses that with a per-container service name: an owner binds a bare port
// and its callers reach it at "<service>:<port>". Those service names do not
// exist inside one container, so the address the shared environment carries
// for a listener is the address of another owner's service, not the address
// the owner is reached at. The contract declares the listener every owner
// serves and the transport every owner calls, so the composition resolves both
// from the contract.

// ownerListenerEnvironment resolves the listener every address environment the
// owner declares carries inside the shared container.
//
// The contract declares the listener. The shared environment names the same
// variable with the published per-container address of another owner's
// service, which does not resolve inside one container, so the owner's own
// listener comes from the contract's declaration. The owner's own port
// environment still names the port that listener binds, so the two agree.
//
// A listener the installation did not enable is left alone: the environment
// that enables it stays empty, so the owner does not start a listener the
// installation did not ask for.
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
		address, err := ResolveAddress(process, name, declaredListener)
		if err != nil {
			return nil, err
		}
		resolved[name] = address
	}
	if port := process.AddressPortEnv; port != "" && process.AddressEnv != "" {
		address, err := ResolveAddress(process, process.AddressEnv, declaredListener)
		if err != nil {
			return nil, err
		}
		if _, number, err := net.SplitHostPort(address); err == nil && number != "" {
			resolved[port] = number
		}
	}
	return resolved, nil
}

// declaredListener reads no installation value. The contract declares the
// listener an owner binds inside the shared container, and the shared
// environment names the same variable with the published per-container address
// of another owner's service, which does not resolve there.
func declaredListener(string) string { return "" }

// configuredOwnerPeerEnvironment resolves the address every caller reaches one
// of its declared targets at.
//
// The contract's communication edges name the target's listener: the edge's
// address environment carries the listener the target declares, reached
// through the loopback interface of the one shared container. A target that
// declares no listener for the edge is refused: the composition never falls
// back to the installation's value and never invents a port.
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
		address, err := ResolveAddress(target, name, declaredListener)
		if err != nil {
			return nil, err
		}
		peer, err := loopbackPeerAddress(address)
		if err != nil {
			return nil, fmt.Errorf("owner %s: peer %s target listener is invalid", caller.Service, edge.AddressEnv)
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
	return net.JoinHostPort(strings.TrimSpace(host), port), nil
}
