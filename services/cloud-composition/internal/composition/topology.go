// Package composition is the logic behind the Cloud backend composition
// entrypoint.
//
// The Cloud container is one process space in which the owner processes
// collaborate, not eleven microservices. This package brings them up: it reads
// the owner topology contract at run time, starts each declared owner binary in
// the order the contract declares, holds an owner back until the owners it
// depends on are ready, and verifies every owner against the readiness
// criterion the contract declares for it.
//
// The contract is the only source of the process set, the order, the addresses
// and the criteria. Nothing here restates them. An owner the contract does not
// declare, a criterion this package cannot execute, an address it cannot
// resolve or a listener two owners would share is refused with a precise
// error, never with a fallback or a guess.
package composition

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// supportedSchemaVersion is the owner topology contract schema this entrypoint
// reads. A contract with another schema version is refused rather than read
// with this version's assumptions.
const supportedSchemaVersion = 1

// Topology is the owner topology contract, reduced to the facts the composition
// entrypoint reads. Every field is the contract's own fact: this type carries
// no owner list, no address and no criterion of its own.
type Topology struct {
	SchemaVersion int    `json:"schemaVersion"`
	Owner         string `json:"owner"`
	// SharedEnv contains environment names the installation deliberately
	// exposes to every owner child. Values are lists in the contract (apart
	// from the legacy database entry), so the composition derives names from
	// the declaration instead of copying the parent process environment.
	SharedEnv     map[string]json.RawMessage `json:"sharedEnv"`
	Image         Image                      `json:"image"`
	Processes     []Process                  `json:"processes"`
	Readiness     ReadinessCriteria          `json:"readinessCriteria"`
	Communication Communication              `json:"communication"`
	PeerAddresses PeerAddresses              `json:"peerAddresses"`

	// DistinctAddressesRequired and DistinctAddressesScope are the contract's
	// own statement that the declared listeners must not collide inside one
	// network namespace.
	DistinctAddressesRequired bool   `json:"distinctAddressesRequired"`
	DistinctAddressesScope    string `json:"distinctAddressesScope"`
}

// Image is the contract's declaration of the product image that carries every
// owner executable.
type Image struct {
	Env             string   `json:"env"`
	Fact            string   `json:"fact"`
	BinaryDirectory string   `json:"binaryDirectory"`
	DefaultCommand  []string `json:"defaultCommand"`
}

// Process is one declared owner process.
type Process struct {
	Service       string `json:"service"`
	Kind          string `json:"kind"`
	OwnerName     string `json:"ownerName"`
	OwnerConstant string `json:"ownerConstant"`
	Binary        string `json:"binary"`
	// Env, OptionalEnv and ExternalEnv are the contract's per-process
	// environment allowlist. They are deliberately carried into the executable
	// plan so a child never inherits an unrelated owner's secrets.
	Env            []string `json:"env"`
	OptionalEnv    []string `json:"optionalEnv"`
	ExternalEnv    []string `json:"externalEnv"`
	DatabaseEnv    string   `json:"databaseEnv"`
	PeerTokensEnv  string   `json:"peerTokensEnv"`
	RPCDatabaseEnv string   `json:"rpcDatabaseEnv"`
	// AllowedEnv is derived from sharedEnv and the process allowlist at plan
	// construction. It is runtime state, not a contract field.
	AllowedEnv []string `json:"-"`
	// PeerEnvironment binds configured caller origins to the target listeners
	// in this shared network namespace. Names still pass through AllowedEnv.
	PeerEnvironment map[string]string `json:"-"`
	// ListenerEnvironment binds the owner's own listen environments to the
	// listener the contract declares for them. The shared environment names
	// those variables with the published per-container address of another
	// owner's service, which does not resolve inside one container, so the
	// owner binds the declared listener instead.
	ListenerEnvironment map[string]string `json:"-"`

	// AddressEnv is the process's own listen address. For an ownerservice
	// process it is the gRPC address; for an http or bff process it is the
	// HTTP address.
	AddressEnv string `json:"addressEnv"`
	// RPCAddressEnv is the gRPC address of a process that serves an HTTP
	// surface and a typed owner surface on separate listeners.
	RPCAddressEnv     string `json:"rpcAddressEnv"`
	DefaultAddress    string `json:"defaultAddress"`
	DefaultRPCAddress string `json:"defaultRPCAddress"`
	// AddressPortEnv names the environment a process reads its listen port
	// from when its own address environment is absent.
	AddressPortEnv string `json:"addressPortEnv"`

	// TransportPrincipal is the identity the process presents on the owner
	// transport, and the identity the communication edges name it by.
	TransportPrincipal string `json:"transportPrincipal"`

	// RPCEnabledByEnv names the environment that enables a process's optional
	// typed owner listener. An absent or empty value means that listener is
	// not started, so it neither listens nor collides.
	RPCEnabledByEnv          string `json:"rpcEnabledByEnv"`
	RPCEnabledByEnvSemantics string `json:"rpcEnabledByEnvSemantics"`

	// AdditionalListeners are the extra HTTP listeners a process serves.
	AdditionalListeners []Listener `json:"additionalListeners"`

	// DaemonArgs are the arguments the contract declares for the process.
	DaemonArgs []string `json:"daemonArgs"`
}

// Listener is one declared additional listener of a process.
type Listener struct {
	AddressEnv     string `json:"addressEnv"`
	DefaultAddress string `json:"defaultAddress"`
	ValueSemantics string `json:"valueSemantics"`
}

// ReadinessCriteria is the contract's declaration of every process's
// readiness criterion.
type ReadinessCriteria struct {
	Rule     string      `json:"rule"`
	Criteria []Criterion `json:"criteria"`
}

// Criterion is one process's readiness criterion: the checks an installation
// runs to decide whether the process is ready to serve.
type Criterion struct {
	Process  string  `json:"process"`
	Kind     string  `json:"kind"`
	Checks   []Check `json:"checks"`
	Evidence string  `json:"evidence"`
}

// Check is one concrete check inside a criterion.
type Check struct {
	Transport  string `json:"transport"`
	Role       string `json:"role"`
	AddressEnv string `json:"addressEnv"`
	Service    string `json:"service"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	Projection string `json:"projection"`
}

// Communication is the contract's declaration of the owner transports.
type Communication struct {
	SchemaVersion int    `json:"schemaVersion"`
	Scope         string `json:"scope"`
	Edges         []Edge `json:"edges"`
}

type PeerAddresses struct {
	SchemaVersion int           `json:"schemaVersion"`
	OriginSchemes []string      `json:"originSchemes"`
	Entries       []PeerAddress `json:"entries"`
}

type PeerAddress struct {
	Caller                     string `json:"caller"`
	Target                     string `json:"target"`
	AddressEnv                 string `json:"addressEnv"`
	TokenEnv                   string `json:"tokenEnv"`
	TargetAddressEnv           string `json:"targetAddressEnv"`
	OriginScheme               string `json:"originScheme"`
	Activation                 string `json:"activation"`
	ConfiguredAddressSemantics string `json:"configuredAddressSemantics"`
}

// Edge is one caller-to-target owner transport.
type Edge struct {
	Caller                     string   `json:"caller"`
	Target                     string   `json:"target"`
	AddressEnv                 string   `json:"addressEnv"`
	TokenEnv                   string   `json:"tokenEnv"`
	Activation                 string   `json:"activation"`
	ActivationEnvs             []string `json:"activationEnvs"`
	ConfiguredAddressSemantics string   `json:"configuredAddressSemantics"`
}

// Load reads the owner topology contract from disk and refuses a contract this
// entrypoint cannot execute.
func Load(path string) (Topology, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Topology{}, fmt.Errorf("read the owner topology contract: %w", err)
	}
	var topology Topology
	if err := json.Unmarshal(raw, &topology); err != nil {
		return Topology{}, fmt.Errorf("decode the owner topology contract: %w", err)
	}
	if err := topology.validate(); err != nil {
		return Topology{}, err
	}
	return topology, nil
}

// validate refuses a contract whose facts the composition entrypoint cannot
// honour. Every refusal names the contract fact that is missing or unusable,
// so an installation sees what it has to fix rather than a later probe
// failure.
func (t *Topology) validate() error {
	if t.SchemaVersion != supportedSchemaVersion {
		return fmt.Errorf("owner topology contract schema version %d is not supported", t.SchemaVersion)
	}
	if strings.TrimSpace(t.Owner) == "" {
		return fmt.Errorf("owner topology contract names no owner")
	}
	if len(t.Processes) == 0 {
		return fmt.Errorf("owner topology contract declares no process")
	}
	seen := map[string]bool{}
	for _, process := range t.Processes {
		if strings.TrimSpace(process.Service) == "" {
			return fmt.Errorf("owner topology contract declares a process without a service name")
		}
		if seen[process.Service] {
			return fmt.Errorf("owner topology contract declares %q twice", process.Service)
		}
		seen[process.Service] = true
		if strings.TrimSpace(process.Binary) == "" {
			return fmt.Errorf("owner %s declares no binary", process.Service)
		}
		if strings.TrimSpace(process.Kind) == "" {
			return fmt.Errorf("owner %s declares no kind", process.Service)
		}
		if strings.TrimSpace(process.TransportPrincipal) == "" {
			return fmt.Errorf("owner %s declares no transport principal", process.Service)
		}
		if strings.TrimSpace(process.AddressEnv) == "" && strings.TrimSpace(process.RPCAddressEnv) == "" {
			return fmt.Errorf("owner %s declares no listen address environment", process.Service)
		}
	}
	criteria := map[string]Criterion{}
	for _, criterion := range t.Readiness.Criteria {
		if strings.TrimSpace(criterion.Process) == "" {
			return fmt.Errorf("owner topology contract declares a readiness criterion without a process")
		}
		if _, duplicate := criteria[criterion.Process]; duplicate {
			return fmt.Errorf("owner %s declares two readiness criteria", criterion.Process)
		}
		if len(criterion.Checks) == 0 {
			return fmt.Errorf("owner %s declares a readiness criterion without a check", criterion.Process)
		}
		criteria[criterion.Process] = criterion
	}
	for _, process := range t.Processes {
		criterion, ok := criteria[process.Service]
		if !ok {
			return fmt.Errorf("owner %s declares no readiness criterion", process.Service)
		}
		for _, check := range criterion.Checks {
			if check.Transport != "grpc" && check.Transport != "http" {
				return fmt.Errorf("owner %s declares a readiness check on the unknown transport %q", process.Service, check.Transport)
			}
			if check.Role != "readiness" && check.Role != "liveness" {
				return fmt.Errorf("owner %s declares a readiness check with the unknown role %q", process.Service, check.Role)
			}
			if !process.declaresAddressEnv(check.AddressEnv) {
				return fmt.Errorf("owner %s declares a readiness check on %s, which it does not declare as a listen address", process.Service, check.AddressEnv)
			}
		}
	}
	return nil
}

// declaresAddressEnv reports whether the process declares the given address
// environment name as one of its own listeners.
func (p Process) declaresAddressEnv(name string) bool {
	if name == "" {
		return false
	}
	if p.AddressEnv == name || p.RPCAddressEnv == name {
		return true
	}
	for _, listener := range p.AdditionalListeners {
		if listener.AddressEnv == name {
			return true
		}
	}
	return false
}

// defaultAddressFor returns the contract's declared default listen address for
// one of the process's own address environment names.
func (p Process) defaultAddressFor(name string) (string, bool) {
	if p.AddressEnv == name && p.DefaultAddress != "" {
		return p.DefaultAddress, true
	}
	if p.RPCAddressEnv == name && p.DefaultRPCAddress != "" {
		return p.DefaultRPCAddress, true
	}
	for _, listener := range p.AdditionalListeners {
		if listener.AddressEnv == name && listener.DefaultAddress != "" {
			return listener.DefaultAddress, true
		}
	}
	return "", false
}

// listenerAddressEnvs names the environments that carry the process's own
// listeners: its listen address, its typed owner address and every additional
// listener it serves.
func (p Process) listenerAddressEnvs() []string {
	names := []string{}
	for _, name := range []string{p.AddressEnv, p.RPCAddressEnv} {
		if name != "" {
			names = append(names, name)
		}
	}
	for _, listener := range p.AdditionalListeners {
		if listener.AddressEnv != "" {
			names = append(names, listener.AddressEnv)
		}
	}
	return names
}

// peerListenerEnv names the listener the process serves a peer address
// environment on. A contract names the target's listener either by the same
// environment name or by the name the caller's own surface uses for it.
func (p Process) peerListenerEnv(addressEnv string) string {
	if p.declaresAddressEnv(addressEnv) {
		return addressEnv
	}
	return p.grpcAddressEnv()
}

// grpcAddressEnv names the environment that carries the process's typed owner
// address. An ownerservice process carries it in its own address environment;
// a process that serves a separate typed owner listener declares it.
func (p Process) grpcAddressEnv() string {
	if p.RPCAddressEnv != "" {
		return p.RPCAddressEnv
	}
	if p.Kind == "ownerservice" {
		return p.AddressEnv
	}
	return ""
}

// httpAddressEnvs names the environments that carry the process's HTTP
// listeners.
func (p Process) httpAddressEnvs() []string {
	names := []string{}
	if p.Kind != "ownerservice" && p.AddressEnv != "" {
		names = append(names, p.AddressEnv)
	}
	for _, listener := range p.AdditionalListeners {
		if listener.AddressEnv != "" {
			names = append(names, listener.AddressEnv)
		}
	}
	return names
}
