package ownerservice

import (
	"context"
	"crypto/subtle"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"opl-cloud/packages/contracts/go/owneridentity"
)

// The inbound identity header names come from the shared wire convention.
const (
	peerTokenHeader = owneridentity.TokenHeader
	peerOwnerHeader = owneridentity.PeerHeader
)

// Authenticate resolves the calling service identity from inbound metadata and
// verifies its token against the configured allowlist. An unknown peer, a missing
// token, or a mismatched token is refused before any owner store is touched.
func Authenticate(ctx context.Context, config Config) (string, error) {
	if len(config.Peers) == 0 && len(config.Services) == 0 {
		return "", status.Error(codes.Unauthenticated, "this owner accepts no inbound peers in this configuration")
	}
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "calling owner identity is required")
	}
	name := firstHeader(md, peerOwnerHeader)
	if name == "" {
		return "", status.Error(codes.Unauthenticated, "calling owner identity is required")
	}
	identity := strings.ToLower(strings.TrimSpace(name))
	owner, ownerOK := owneridentity.Parse(identity)
	service, serviceOK := owneridentity.ParseService(identity)
	expected, allowed := "", false
	if ownerOK {
		expected, allowed = config.Peers[Owner(owner)]
	}
	if serviceOK {
		expected, allowed = config.Services[service]
	}
	if !allowed {
		return "", status.Errorf(codes.PermissionDenied, "service identity %s may not call %s", identity, config.Owner)
	}
	presented := firstHeader(md, peerTokenHeader)
	if presented == "" {
		return "", status.Error(codes.Unauthenticated, "calling owner token is required")
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) != 1 {
		return "", status.Error(codes.Unauthenticated, "calling owner token is invalid")
	}
	return identity, nil
}

func firstHeader(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

// PeerContextKey carries the authenticated peer service identity inside a request context.
type peerContextKey struct{}

// WithPeerIdentity records the authenticated peer identity on the context so a
// handler can bind the caller into its owner-local commit evidence.
func WithPeerIdentity(ctx context.Context, identity string) context.Context {
	return context.WithValue(ctx, peerContextKey{}, identity)
}

// PeerIdentity returns the authenticated peer service identity recorded on the context.
func PeerIdentity(ctx context.Context) (string, bool) {
	identity, ok := ctx.Value(peerContextKey{}).(string)
	return identity, ok
}
