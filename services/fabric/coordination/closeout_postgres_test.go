package coordination_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	contracts "opl-cloud/packages/contracts/go"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/internal/ownerservice"
)

type closeoutCatalog struct {
	api.CatalogCoordinationClient
	accepted *api.QuoteAcceptance
}

func (c *closeoutCatalog) ReadQuoteResourcePlan(context.Context, *api.QuoteResourcePlanRequest, ...grpc.CallOption) (*api.QuoteAcceptance, error) {
	return c.accepted, nil
}

func TestDeletionRejectsLateOriginalResourceProvision(t *testing.T) {
	db := database(t)
	request := command("late-original-provision")
	service, err := coordination.New(db, ownerservice.NewAuthorizer(owneridentity.Fabric, &identity{}).Authorize, &closeoutCatalog{accepted: request.QuoteAcceptance})
	if err != nil {
		t.Fatal(err)
	}
	ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service())
	original, err := service.EnsureResources(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	deletionCall := *request.Context
	deletionCall.IdempotencyKey = "close-original-provision"
	_, err = service.DeleteResources(ctx, &api.MutateResourcesCommand{Context: &deletionCall, WorkspaceId: request.WorkspaceId, ResourceSetId: original.ResourceId})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.EnsureResources(ctx, request)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("late original provision was admitted after deletion: %v", err)
	}
}

func TestDeletionRejectsLateManagedSecretDelivery(t *testing.T) {
	db := database(t)
	request := command("secret-before-close")
	service, err := coordination.New(db, ownerservice.NewAuthorizer(owneridentity.Fabric, &identity{}).Authorize, &closeoutCatalog{accepted: request.QuoteAcceptance})
	if err != nil {
		t.Fatal(err)
	}
	ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service())
	original, err := service.EnsureResources(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	call := *request.Context
	call.IdempotencyKey = "close-before-secret"
	if _, err = service.DeleteResources(ctx, &api.MutateResourcesCommand{Context: &call, WorkspaceId: request.WorkspaceId, ResourceSetId: original.ResourceId}); err != nil {
		t.Fatal(err)
	}
	service.Dispatcher = &localFixture{}
	call.IdempotencyKey = "original-managed-secret"
	value := "synthetic-test-key"
	digest := sha256.Sum256([]byte(value))
	_, err = service.PutManagedSecret(ownerservice.WithPeerOwner(t.Context(), owneridentity.Tenant.Service()), &api.ManagedSecretCommand{Context: &call, WorkspaceId: request.WorkspaceId, WorkspaceApiKeyId: 77, GatewayApiKey: value, Fingerprint: "sha256:" + hex.EncodeToString(digest[:]), SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(request.WorkspaceId)})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("a late Secret write crossed the closing resource owner: %v", err)
	}
}

func TestDeletionRejectsLateSecretBinding(t *testing.T) {
	db := database(t)
	request := command("binding-before-close")
	service, err := coordination.New(db, ownerservice.NewAuthorizer(owneridentity.Fabric, &identity{}).Authorize, &closeoutCatalog{accepted: request.QuoteAcceptance})
	if err != nil {
		t.Fatal(err)
	}
	ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service())
	original, err := service.EnsureResources(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE fabric.resource_sets SET observation_result='confirmed',observed_at=now() WHERE id=$1`, original.ResourceId); err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(ctx, `UPDATE fabric.operations SET status='succeeded',stage='actual_resource_evidence',observation_result='confirmed',result=$2,completed_at=now() WHERE id=$1`, original.OperationId, confirmedProvisionResult(t)); err != nil {
		t.Fatal(err)
	}
	service.Dispatcher = &secretDispatcher{}
	binding := &api.SecretBindingCommand{Context: request.Context, WorkspaceId: request.WorkspaceId, RuntimeInstanceId: "original-runtime", KeyBindingId: "original-key", Fingerprint: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(request.WorkspaceId)}
	if _, err = service.BindSecret(ctx, binding); err != nil {
		t.Fatalf("original binding must work before closeout: %v", err)
	}
	call := *request.Context
	call.IdempotencyKey = "close-before-binding"
	if _, err = service.DeleteResources(ctx, &api.MutateResourcesCommand{Context: &call, WorkspaceId: request.WorkspaceId, ResourceSetId: original.ResourceId}); err != nil {
		t.Fatal(err)
	}
	_, err = service.BindSecret(ctx, binding)
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("retired resources accepted a late Secret binding: %v", err)
	}
}

func TestDeletionFencesAnOriginalProvisionThatNeverReachedFabric(t *testing.T) {
	db := database(t)
	request := command("original-not-dispatched")
	service, err := coordination.New(db, ownerservice.NewAuthorizer(owneridentity.Fabric, &identity{}).Authorize, &closeoutCatalog{accepted: request.QuoteAcceptance})
	if err != nil {
		t.Fatal(err)
	}
	ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service())
	call := *request.Context
	call.IdempotencyKey = "close-original-before-dispatch"
	deleted, err := service.DeleteResources(ctx, &api.MutateResourcesCommand{Context: &call, WorkspaceId: request.WorkspaceId, OriginalProvisionIdempotencyKey: request.GetContext().GetIdempotencyKey()})
	if err != nil || deleted.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("owner could not close the original unstarted provision: %v %v", deleted, err)
	}
	read, err := service.ReadResources(ctx, &api.ResourceReadbackRequest{Context: &call, WorkspaceId: request.WorkspaceId, OriginalProvisionIdempotencyKey: request.GetContext().GetIdempotencyKey()})
	if err != nil || read.GetWorkspaceId() != request.WorkspaceId || !read.GetAbsenceConfirmed() || read.GetOutcome() != api.Observation_OBSERVATION_CONFIRMED {
		t.Fatalf("owner did not confirm its fenced absence: %v %v", read, err)
	}
	if _, err = service.EnsureResources(ctx, request); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("the late original provision crossed its absence fence: %v", err)
	}
}

func TestDeletionDoesNotTreatAnUnknownCommandAsWorkspaceResourceAbsence(t *testing.T) {
	db := database(t)
	request := command("real-original-provision")
	service, err := coordination.New(db, ownerservice.NewAuthorizer(owneridentity.Fabric, &identity{}).Authorize, &closeoutCatalog{accepted: request.QuoteAcceptance})
	if err != nil {
		t.Fatal(err)
	}
	ctx := ownerservice.WithPeerOwner(t.Context(), owneridentity.Workspace.Service())
	if _, err = service.EnsureResources(ctx, request); err != nil {
		t.Fatal(err)
	}
	call := *request.Context
	call.IdempotencyKey = "close-with-unresolved-original"
	_, err = service.DeleteResources(ctx, &api.MutateResourcesCommand{Context: &call, WorkspaceId: request.WorkspaceId, OriginalProvisionIdempotencyKey: "unrelated-original"})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("existing resources were relabelled absent under another original key: %v", err)
	}
}
