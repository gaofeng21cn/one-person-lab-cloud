// Package ownerservice provides the process mechanics every Cloud domain owner
// repeats: environment configuration, the owner's PostgreSQL connection, and the
// inbound service identity allowlist on its gRPC boundary.
//
// It carries no domain rule. Each owner supplies its own store and service
// registrations; this package only makes the boundary uniform so a request from
// an unexpected peer is refused before any owner store is touched. The wire
// identity convention itself lives in the shared contracts module.
package ownerservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"opl-cloud/packages/contracts/go/owneridentity"
)

// Owner names one Cloud data owner. A process serves exactly one owner identity
// even when it carries more than one API group.
type Owner = owneridentity.Owner

// The fixed Cloud owner names, aliased from the shared wire convention.
const (
	OwnerTenant          = owneridentity.Tenant
	OwnerCapability      = owneridentity.Capability
	OwnerBuild           = owneridentity.Build
	OwnerWorkspace       = owneridentity.Workspace
	OwnerRuntimeControl  = owneridentity.RuntimeControl
	OwnerServe           = owneridentity.Serve
	OwnerFabric          = owneridentity.Fabric
	OwnerGateway         = owneridentity.Gateway
	OwnerResourceCatalog = owneridentity.ResourceCatalog
	OwnerLedger          = owneridentity.Ledger
)

// Config is one owner process's resolved configuration.
type Config struct {
	// Owner is this process's own identity.
	Owner Owner
	// Addr is the gRPC listen address.
	Addr string
	// DatabaseURL points at this owner's own database.
	DatabaseURL string
	// Peers maps an accepted inbound owner identity to the bearer token that
	// owner must present. A peer absent from this map can never call this owner.
	Peers map[Owner]string
}

// LoadConfig resolves the process configuration for one owner from the
// environment. In production every field is mandatory; in development the
// database may be empty so an owner can run without persistence, but a configured
// peer allowlist is still validated the same way.
func LoadConfig(getenv func(string) string, owner Owner, defaultAddr string) (Config, error) {
	if !owner.Valid() {
		return Config{}, fmt.Errorf("%q is not a Cloud owner", owner)
	}
	prefix := "OPL_" + strings.ToUpper(owner.String())
	config := Config{
		Owner:       owner,
		Addr:        strings.TrimSpace(getenv(prefix + "_ADDR")),
		DatabaseURL: strings.TrimSpace(getenv("DATABASE_URL")),
	}
	if config.Addr == "" {
		config.Addr = defaultAddr
	}
	production := getenv("NODE_ENV") == "production"
	if production && config.DatabaseURL == "" {
		return Config{}, fmt.Errorf("%s: DATABASE_URL is required in production", owner)
	}

	peers, err := parsePeerTokens(getenv(prefix + "_PEER_TOKENS"))
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", owner, err)
	}
	config.Peers = peers
	if production && len(peers) == 0 {
		return Config{}, fmt.Errorf("%s: %s_PEER_TOKENS is required in production", owner, prefix)
	}
	return config, nil
}

// parsePeerTokens reads the `{"<owner>":"<token>"}` allowlist. An unknown owner
// name or a short token is rejected instead of silently widening the boundary.
func parsePeerTokens(raw string) (map[Owner]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("peer tokens must be a JSON object of owner to token: %w", err)
	}
	peers := make(map[Owner]string, len(decoded))
	for name, token := range decoded {
		owner := Owner(strings.TrimSpace(name))
		if !owner.Valid() {
			return nil, fmt.Errorf("%q is not a Cloud owner", name)
		}
		token = strings.TrimSpace(token)
		if len(token) < 32 {
			return nil, fmt.Errorf("token for peer %s must contain at least 32 characters", owner)
		}
		peers[owner] = token
	}
	return peers, nil
}

// Listen opens the owner's gRPC listener.
func Listen(addr string) (net.Listener, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return nil, errors.New("listen address is required")
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", addr, err)
	}
	return listener, nil
}

// Getenv is the environment accessor the process uses, so tests can supply a map
// instead of mutating the real environment.
func Getenv(key string) string { return os.Getenv(key) }
