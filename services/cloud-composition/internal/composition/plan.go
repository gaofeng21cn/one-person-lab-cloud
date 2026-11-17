package composition

import (
	"fmt"
	"strings"
)

// Plan is the executable order the owner topology contract declares, with the
// dependency each process waits for before it starts.
type Plan struct {
	Topology Topology
	Steps    []Step
}

// Step is one process in the composition.
type Step struct {
	Process   Process
	Criterion Criterion

	// Dependencies names the processes this one waits for before it starts.
	// They are the active communication edges whose target the contract's
	// declared order already placed before this process.
	Dependencies []Step
}

// Service names the step's process.
func (s Step) Service() string { return s.Process.Service }

// NewPlan derives the composition order, the dependencies and the effective
// addresses from the contract. The contract's declared process order is the
// order: it is the contract's own statement of the sequence, and the
// communication edges contain cycles, so the declared order is what breaks
// them.
func NewPlan(topology Topology, getenv func(string) string) (Plan, error) {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	plan := Plan{Topology: topology}
	built := map[string]Step{}
	criteria := map[string]Criterion{}
	for _, criterion := range topology.Readiness.Criteria {
		criteria[criterion.Process] = criterion
	}
	order := map[string]int{}
	for index, process := range topology.Processes {
		order[process.Service] = index
	}
	for _, process := range topology.Processes {
		criterion, ok := criteria[process.Service]
		if !ok {
			return Plan{}, fmt.Errorf("owner %s declares no readiness criterion", process.Service)
		}
		dependencyProcesses, err := dependenciesBefore(topology, process, order, getenv)
		if err != nil {
			return Plan{}, err
		}
		dependencies := []Step{}
		for _, dependency := range dependencyProcesses {
			built, ok := built[dependency.Service]
			if !ok {
				return Plan{}, fmt.Errorf("owner %s depends on %s, which the contract does not order before it", process.Service, dependency.Service)
			}
			dependencies = append(dependencies, built)
		}
		step := Step{
			Process:      process,
			Criterion:    criterion,
			Dependencies: dependencies,
		}
		plan.Steps = append(plan.Steps, step)
		built[process.Service] = step
	}
	if err := plan.checkDistinctAddresses(getenv); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

// dependenciesBefore returns the processes the given process waits for: the
// active communication edges that name it as the caller and whose target the
// contract's declared order already placed before it.
//
// A dependency the declared order places after the process is not waited for.
// The contract's edges form cycles (capability and build require each other,
// workspace and ledger require each other), so a process cannot wait for every
// target it names. The declared order is the contract's own resolution of
// that, and this entrypoint keeps it.
func dependenciesBefore(topology Topology, process Process, order map[string]int, getenv func(string) string) ([]Process, error) {
	dependencies := []Process{}
	seen := map[string]bool{}
	for _, edge := range topology.Communication.Edges {
		if edge.Caller != process.TransportPrincipal {
			continue
		}
		active, err := edgeActive(topology, edge, process, getenv)
		if err != nil {
			return nil, err
		}
		if !active {
			continue
		}
		target, ok := processByPrincipal(topology, edge.Target)
		if !ok {
			return nil, fmt.Errorf("owner %s declares a transport to %q, which no declared process serves", process.Service, edge.Target)
		}
		if target.Service == process.Service {
			continue
		}
		targetIndex, ok := order[target.Service]
		if !ok {
			return nil, fmt.Errorf("owner %s declares a transport to %q, which the contract does not order", process.Service, edge.Target)
		}
		if targetIndex >= order[process.Service] {
			continue
		}
		if seen[target.Service] {
			continue
		}
		seen[target.Service] = true
		dependencies = append(dependencies, target)
	}
	return dependencies, nil
}

// processByPrincipal returns the process that serves the given transport
// principal.
func processByPrincipal(topology Topology, principal string) (Process, bool) {
	for _, process := range topology.Processes {
		if process.TransportPrincipal == principal {
			return process, true
		}
	}
	return Process{}, false
}

// edgeActive reports whether one communication edge applies in this
// installation, following the contract's activation semantics.
func edgeActive(topology Topology, edge Edge, caller Process, getenv func(string) string) (bool, error) {
	if enabled, err := rpcSurfaceEnabled(caller, getenv); err != nil {
		return false, err
	} else if !enabled {
		return false, nil
	}
	switch edge.Activation {
	case "required":
		return true, nil
	case "configured":
		addressed, err := edgeAddressConfigured(edge, getenv)
		if err != nil {
			return false, err
		}
		if addressed {
			return true, nil
		}
		if strings.TrimSpace(getenv(edge.TokenEnv)) != "" {
			return true, nil
		}
		for _, name := range edge.ActivationEnvs {
			if strings.TrimSpace(getenv(name)) != "" {
				return true, nil
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("owner %s declares a transport to %q with the unknown activation %q", caller.Service, edge.Target, edge.Activation)
	}
}

// edgeAddressConfigured reports whether the edge's address environment carries
// a value, following the edge's declared address semantics.
func edgeAddressConfigured(edge Edge, getenv func(string) string) (bool, error) {
	if edge.AddressEnv == "" {
		return false, nil
	}
	value := getenv(edge.AddressEnv)
	switch edge.ConfiguredAddressSemantics {
	case "raw-nonempty":
		return value != "", nil
	case "trimmed-nonempty":
		return strings.TrimSpace(value) != "", nil
	case "":
		return false, fmt.Errorf("transport to %q declares no address semantics", edge.Target)
	default:
		return false, fmt.Errorf("transport to %q declares the unknown address semantics %q", edge.Target, edge.ConfiguredAddressSemantics)
	}
}

// rpcSurfaceEnabled reports whether the process's optional typed owner
// listener is started in this installation. A process that declares no such
// environment always serves it.
func rpcSurfaceEnabled(process Process, getenv func(string) string) (bool, error) {
	if process.RPCEnabledByEnv == "" {
		return true, nil
	}
	value := getenv(process.RPCEnabledByEnv)
	switch process.RPCEnabledByEnvSemantics {
	case "raw-nonempty":
		return value != "", nil
	case "trimmed-nonempty":
		return strings.TrimSpace(value) != "", nil
	case "":
		return false, fmt.Errorf("owner %s declares an RPC activation environment without its semantics", process.Service)
	default:
		return false, fmt.Errorf("owner %s declares the unknown RPC activation semantics %q", process.Service, process.RPCEnabledByEnvSemantics)
	}
}

// ResolveAddress returns the address one of the process's listeners listens
// on in this installation. It resolves the same environment the process itself
// reads and falls back to the contract's declared default the same way, so the
// entrypoint probes the address the process actually bound.
func ResolveAddress(process Process, addressEnv string, getenv func(string) string) (string, error) {
	declared, ok := process.defaultAddressFor(addressEnv)
	if !ok {
		return "", fmt.Errorf("owner %s declares no default address for %s", process.Service, addressEnv)
	}
	value := getenv(addressEnv)
	if process.Kind == "ownerservice" {
		value = strings.TrimSpace(value)
	}
	if value == "" {
		if port := process.AddressPortEnv; port != "" && addressEnv == process.AddressEnv {
			if portValue := getenv(port); strings.TrimSpace(portValue) != "" {
				return ":" + portValue, nil
			}
		}
		return declared, nil
	}
	return value, nil
}

// listenerAddress returns the address one of the process's listeners binds in
// this installation. A listener the contract declares without a default
// address and the installation does not set binds nothing here.
func listenerAddress(process Process, addressEnv string, getenv func(string) string) (string, error) {
	value := getenv(addressEnv)
	if process.Kind == "ownerservice" {
		value = strings.TrimSpace(value)
	}
	if value != "" {
		return value, nil
	}
	declared, ok := process.defaultAddressFor(addressEnv)
	if !ok {
		return "", nil
	}
	if port := process.AddressPortEnv; port != "" && addressEnv == process.AddressEnv {
		if portValue := getenv(port); strings.TrimSpace(portValue) != "" {
			return ":" + portValue, nil
		}
	}
	return declared, nil
}

// checkDistinctAddresses refuses a composition in which two owners would bind
// the same address inside one network namespace. The contract requires the
// declared listeners to be distinct in that scope, and a shared port is a
// reachable bind failure that would otherwise surface as an owner that never
// becomes ready.
func (p *Plan) checkDistinctAddresses(getenv func(string) string) error {
	if !p.Topology.DistinctAddressesRequired {
		return nil
	}
	type binding struct {
		owner string
		env   string
	}
	bindings := map[string]binding{}
	for _, step := range p.Steps {
		process := step.Process
		envs := []string{}
		if grpcEnv := process.grpcAddressEnv(); grpcEnv != "" {
			enabled, err := rpcSurfaceEnabled(process, getenv)
			if err != nil {
				return err
			}
			if enabled {
				envs = append(envs, grpcEnv)
			}
		}
		envs = append(envs, process.httpAddressEnvs()...)
		for _, env := range envs {
			address, err := listenerAddress(process, env, getenv)
			if err != nil {
				return err
			}
			if address == "" {
				continue
			}
			if other, taken := bindings[address]; taken {
				return fmt.Errorf("owner %s and owner %s would both listen on %s in one %s", other.owner, process.Service, address, p.Topology.DistinctAddressesScope)
			}
			bindings[address] = binding{owner: process.Service, env: env}
		}
	}
	return nil
}
