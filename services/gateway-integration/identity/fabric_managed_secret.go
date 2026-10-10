package identity

import (
	"context"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
)

// FabricManagedSecretStore writes a managed Gateway key into Fabric's approved
// Secret store through FabricCoordination.PutManagedSecret. This owner (transport
// principal `tenant`) is the only approved caller, and Fabric stores only the
// approved Secret identity: the raw value exists only for this call and never
// enters a Gateway or Fabric normal table.
type FabricManagedSecretStore struct {
	client api.FabricCoordinationClient
}

// NewFabricManagedSecretStore wires the approved-store client over an
// authenticated Fabric peer connection.
func NewFabricManagedSecretStore(client api.FabricCoordinationClient) *FabricManagedSecretStore {
	return &FabricManagedSecretStore{client: client}
}

// PutSecret writes the delivery's raw value through Fabric and returns the
// provider-confirmed opaque identity. The derived continuation context is the
// original command's own context with the caller's authorization context cleared:
// Fabric authorizes the accepted Workspace obligation and never a caller claim.
// The caller's session is not forwarded as an authorization context, so a retry
// reauthorizes from the accepted grant record.
func (s *FabricManagedSecretStore) PutSecret(ctx context.Context, delivery SecretDelivery) (SecretDelivery, error) {
	if s == nil || s.client == nil {
		return SecretDelivery{}, ErrSecretStoreUnavailable
	}
	if strings.TrimSpace(delivery.WorkspaceID) == "" || strings.TrimSpace(delivery.TenantID) == "" || strings.TrimSpace(delivery.Raw) == "" || strings.TrimSpace(delivery.Fingerprint) == "" {
		return SecretDelivery{}, status.Error(codes.InvalidArgument, "workspace, tenant, fingerprint and value are required for an approved Secret write")
	}
	externalKeyID, err := parseGatewaySubject(delivery.ExternalKeyID)
	if err != nil {
		return SecretDelivery{}, status.Error(codes.FailedPrecondition, "the issued key identity is not an external key id")
	}
	call := secretWriteCall(delivery)
	if call == nil {
		return SecretDelivery{}, status.Error(codes.FailedPrecondition, "the original command identity is required for an approved Secret write")
	}
	secretRef := contracts.WorkspaceGatewaySecretRef(delivery.WorkspaceID)
	readback, err := s.client.PutManagedSecret(ctx, &api.ManagedSecretCommand{
		Context: call, WorkspaceId: delivery.WorkspaceID, WorkspaceApiKeyId: externalKeyID,
		Fingerprint: delivery.Fingerprint, SecretDeliveryReference: secretRef, GatewayApiKey: delivery.Raw,
	})
	if err != nil {
		return SecretDelivery{}, err
	}
	if readback.GetSecretRef() != secretRef || readback.GetFingerprint() != delivery.Fingerprint || strings.TrimSpace(readback.GetVersion()) == "" {
		return SecretDelivery{}, status.Error(codes.FailedPrecondition, "Fabric returned a different Secret identity than the delivered key")
	}
	stored := delivery
	stored.Reference, stored.Raw = readback.GetSecretRef(), ""
	if stored.Fingerprint == "" {
		stored.Fingerprint = readback.GetFingerprint()
	}
	return stored, nil
}

// secretWriteCall derives the owner-to-owner continuation context for the Secret
// write. It keeps the original command's tenant scope, actor, session or accepted
// grant and request identity, and its idempotency key is the bounded write
// identity Fabric appends its own step suffix to. The authorization context is
// deliberately cleared: Fabric's peer identity is derived from the transport, not
// forwarded from the caller's decision.
func secretWriteCall(delivery SecretDelivery) *api.CallContext {
	if delivery.Call == nil {
		return nil
	}
	call := proto.Clone(delivery.Call).(*api.CallContext)
	if strings.TrimSpace(call.GetIdempotencyKey()) == "" || strings.TrimSpace(call.GetActorId()) == "" || strings.TrimSpace(call.GetRequestId()) == "" ||
		call.GetScope() == nil || (call.GetSessionId() == "" && call.GetAcceptedOperationGrantId() == "") {
		return nil
	}
	call.AuthorizationContextId = ""
	return call
}
