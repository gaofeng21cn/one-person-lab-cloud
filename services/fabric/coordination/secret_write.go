package coordination

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/internal/fabric"
	"opl-cloud/services/internal/ownerservice"
)

// managedSecretFingerprint is the one approved fingerprint shape: the SHA-256 of
// the stored key value in the same lowercase hex form Fabric's provider adapters
// write and read back (`sha256:<64 lowercase hex>`).
var managedSecretFingerprint = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// PutManagedSecret writes one managed Gateway key into Fabric's approved Secret
// store for one accepted Workspace obligation. The Gateway Integration deployment
// unit (transport principal `tenant`) is the only caller, and it presents the
// exact Workspace, external key id, delivery reference and fingerprint it issued;
// Fabric validates the fingerprint against the presented value, authorizes the
// Workspace with the same BINDMANAGEDSECRET action Serve's binding uses, and
// writes through the provider's existing idempotent Secret path. The raw value is
// never persisted in a Fabric normal table, log, Outbox or receipt: the returned
// readback is the opaque reference, version and fingerprint.
func (s *Service) PutManagedSecret(ctx context.Context, r *api.ManagedSecretCommand) (*api.ManagedSecretReadback, error) {
	if err := peer(ctx, owneridentity.Tenant.Service()); err != nil {
		return nil, err
	}
	if err := ownerservice.ValidateCallContext(ctx, r.GetContext()); err != nil {
		return nil, err
	}
	if s.Dispatcher == nil {
		return nil, status.Error(codes.FailedPrecondition, "no provider Secret delivery capability is configured")
	}
	workspace := strings.TrimSpace(r.GetWorkspaceId())
	if workspace == "" || r.GetWorkspaceApiKeyId() <= 0 || strings.TrimSpace(r.GetGatewayApiKey()) == "" {
		return nil, status.Error(codes.InvalidArgument, "workspace, external key id and key value are required")
	}
	if !managedSecretFingerprint.MatchString(r.GetFingerprint()) {
		return nil, status.Error(codes.InvalidArgument, "an exact sha256 fingerprint of the key value is required")
	}
	if r.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(workspace) {
		return nil, status.Error(codes.InvalidArgument, "the Secret delivery reference does not name this Workspace's Gateway Secret")
	}
	digest := sha256.Sum256([]byte(r.GetGatewayApiKey()))
	if r.GetFingerprint() != "sha256:"+hex.EncodeToString(digest[:]) {
		return nil, status.Error(codes.InvalidArgument, "the fingerprint does not match the presented key value")
	}
	idempotencyKey := strings.TrimSpace(r.GetContext().GetIdempotencyKey())
	if idempotencyKey == "" {
		return nil, status.Error(codes.InvalidArgument, "idempotency key is required")
	}
	tenant := r.GetContext().GetScope().GetTenant().GetTenantId()
	if err := s.authorizeWorkspace(ctx, r.GetContext(), workspace, tenant, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_BINDMANAGEDSECRET); err != nil {
		return nil, err
	}
	stored, err := s.Dispatcher.PutManagedSecret(ctx, PutManagedSecretIntent{
		TenantID: tenant, WorkspaceID: workspace, WorkspaceAPIKeyID: r.GetWorkspaceApiKeyId(),
		SecretRef: r.GetSecretDeliveryReference(), Fingerprint: r.GetFingerprint(), GatewayAPIKey: r.GetGatewayApiKey(),
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		return nil, managedSecretDispatchError(err)
	}
	if stored.SecretRef != r.GetSecretDeliveryReference() || stored.Fingerprint != r.GetFingerprint() || strings.TrimSpace(stored.Version) == "" {
		return nil, status.Error(codes.FailedPrecondition, "the approved Secret store returned a different identity")
	}
	return &api.ManagedSecretReadback{SecretRef: stored.SecretRef, Version: stored.Version, Fingerprint: stored.Fingerprint}, nil
}

// managedSecretDispatchError maps the provider's Secret write outcome to the
// caller's retry decision. Only outcomes that the reachable call path produces
// as typed sentinels are classified: Fabric's own operation claim raises
// fabric.ErrGatewaySecretIdempotencyConflict when one idempotency key is reused
// for a different Secret, the provider readback raises
// fabric.ErrLaunchStageBindingConflict when the stored Secret identity does not
// match the requested identity, and it raises
// fabric.ErrWorkspaceLaunchResourceAbsent while the write is not confirmed
// yet. The first two are definite refusals and must not be retried; an absent
// resource stays retryable. Every other failure — including a kubectl refusal
// that the production provider returns as a plain, unclassified error — stays
// unknown and retryable rather than being reported as success or as a definite
// refusal. The Workspace launch-chain sentinels are not produced by this path
// and must not be classified as refusals here.
func managedSecretDispatchError(err error) error {
	switch {
	case errors.Is(err, fabric.ErrGatewaySecretIdempotencyConflict):
		return status.Error(codes.AlreadyExists, "the Secret write identity was reused for a different Secret")
	case errors.Is(err, fabric.ErrLaunchStageBindingConflict):
		return status.Error(codes.FailedPrecondition, "the approved Secret store holds a conflicting Secret identity")
	case errors.Is(err, fabric.ErrWorkspaceLaunchResourceAbsent):
		return status.Error(codes.Unavailable, "the approved Secret store has not confirmed the write")
	default:
		return status.Error(codes.Unavailable, "the approved Secret store could not confirm the write")
	}
}
