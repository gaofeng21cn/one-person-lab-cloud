package ownerservice

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/internal/ownerstore"
)

// TestOperationsRequireAnOwnerStore proves the Operation readback group is only
// constructible over an owner-local store: an owner cannot advertise the surface
// without the table that answers it.
func TestOperationsRequireAnOwnerStore(t *testing.T) {
	if _, err := NewOperations(OwnerServe, nil); err == nil {
		t.Fatal("OwnerOperations was constructed without an owner store")
	}
	if _, err := NewOperations(Owner("not-an-owner"), &ownerstore.Store{}); err == nil {
		t.Fatal("OwnerOperations accepted an unknown owner identity")
	}
}

// TestOperationReadRejectsAMissingID proves a read without an id is a client error
// rather than an empty success.
func TestOperationReadRejectsAMissingID(t *testing.T) {
	operations := &Operations{owner: OwnerServe}
	_, err := operations.Read(context.Background(), &api.OwnerOperationRequest{})
	if status.Code(err) != codes.InvalidArgument {
		t.Fatalf("code = %v, want InvalidArgument", status.Code(err))
	}
}

// TestOperationOwnerEnumCoversEveryCloudOwner proves every Cloud data owner can be
// named on the Operation readback surface. A missing entry would make that owner's
// Operations unreachable through the contract rather than a routing mistake.
func TestOperationOwnerEnumCoversEveryCloudOwner(t *testing.T) {
	for _, owner := range []Owner{
		OwnerTenant, OwnerCapability, OwnerBuild, OwnerWorkspace, OwnerRuntimeControl,
		OwnerServe, OwnerFabric, OwnerGateway, OwnerResourceCatalog,
	} {
		if operationOwner(owner) == api.OperationOwnerEnum_OPERATION_OWNER_ENUM_UNSPECIFIED {
			t.Fatalf("%s has no OperationOwner enum value, so its operations cannot be routed", owner)
		}
	}
}

// TestOperationReadbackRejectsAnUnknownStoredStage proves an owner that stored a
// stage outside the contract vocabulary fails the read instead of returning a zero
// enum the Console would render as an unspecified stage.
func TestOperationReadbackRejectsAnUnknownStoredStage(t *testing.T) {
	operations := &Operations{owner: OwnerServe}
	_, err := operations.toContract(ownerstore.Operation{
		ID:         "op-1",
		Kind:       "runtime_deploy",
		ResourceID: "resource-1",
		Status:     ownerstore.OperationRunning,
		Stage:      "not_a_contract_stage",
		RequestID:  "request-1",
	})
	if err == nil {
		t.Fatal("a stored stage outside the contract vocabulary was accepted")
	}
}

var _ = errors.Is
