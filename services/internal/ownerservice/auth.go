package ownerservice

import (
	"context"
	"crypto/subtle"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
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

// Authenticate resolves the calling owner from the inbound metadata and verifies
// its token against the configured allowlist. An unknown peer, a missing token, or
// a mismatched token is refused here, before any owner store is touched.
func Authenticate(ctx context.Context, config Config) (Service, error) {
	if len(config.Peers) == 0 {
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
	owner := Service(name)
	expected, allowed := config.Peers[owner]
	if !allowed {
		return "", status.Errorf(codes.PermissionDenied, "owner %s may not call %s", owner, config.Owner)
	}
	presented := firstHeader(md, peerTokenHeader)
	if presented == "" {
		return "", status.Error(codes.Unauthenticated, "calling owner token is required")
	}
	if subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) != 1 {
		return "", status.Error(codes.Unauthenticated, "calling owner token is invalid")
	}
	if !config.TLS.AllowInsecureLocal {
		remote, ok := peer.FromContext(ctx)
		if !ok {
			return "", status.Error(codes.Unauthenticated, "mTLS peer is required")
		}
		tlsInfo, ok := remote.AuthInfo.(credentials.TLSInfo)
		if !ok || len(tlsInfo.State.VerifiedChains) == 0 || len(tlsInfo.State.PeerCertificates) == 0 {
			return "", status.Error(codes.Unauthenticated, "verified mTLS peer is required")
		}
		certificateOwner, err := owneridentity.CertificateService(tlsInfo.State.PeerCertificates[0])
		if err != nil || certificateOwner != owner {
			return "", status.Error(codes.Unauthenticated, "calling service differs from its mTLS certificate")
		}
	}
	return owner, nil
}

func firstHeader(md metadata.MD, key string) string {
	values := md.Get(key)
	if len(values) != 1 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

// PeerContextKey carries the authenticated peer owner inside a request context.
type peerContextKey struct{}

// WithPeerOwner records the authenticated peer owner on the context so a handler
// can bind the caller into its owner-local commit evidence.
func WithPeerOwner(ctx context.Context, owner Service) context.Context {
	return context.WithValue(ctx, peerContextKey{}, owner)
}

// PeerOwner returns the authenticated peer owner recorded on the context.
func PeerOwner(ctx context.Context) (Service, bool) {
	owner, ok := ctx.Value(peerContextKey{}).(Service)
	return owner, ok
}
