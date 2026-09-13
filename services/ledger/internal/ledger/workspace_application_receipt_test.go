package ledger

import (
	"context"
	"errors"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func resourceOnlyReceiptExecution(input ReceiptInput) map[string]any {
	return (contracts.WorkspaceResourceReceiptExecution{OperationID: input.RequestID, ResourceType: "workspace", ResourceID: input.WorkspaceID, ComputeAllocationID: "compute-alpha", StorageID: "storage-alpha", AttachmentID: "attachment-alpha", ProvisioningMode: contracts.WorkspaceProvisioningResourceOnly}).Fields()
}

func TestWorkspaceResourceOnlyReceiptBoundary(t *testing.T) {
	for _, kind := range []string{"billing.workspace_purchased.v1", "workspace.created", "workspace.deleted.v1"} {
		t.Run(kind, func(t *testing.T) {
			input := validWorkspaceLaunchReceiptInput(kind)
			if kind == "workspace.deleted.v1" {
				input = validWorkspaceDeletionReceiptInput()
			}
			input.Execution = resourceOnlyReceiptExecution(input)
			if _, err := NewMemoryStore().RecordReceipt(context.Background(), input); err != nil {
				t.Fatal(err)
			}
			for _, mutate := range []func(*ReceiptInput){
				func(r *ReceiptInput) { r.Execution["provisioningMode"] = "full" },
				func(r *ReceiptInput) { r.Execution["runtimeId"] = "runtime-alpha" },
				func(r *ReceiptInput) { delete(r.Execution, "storageId") },
				func(r *ReceiptInput) { r.Execution["resourceId"] = "another-workspace" },
			} {
				bad := input
				bad.Execution = resourceOnlyReceiptExecution(input)
				mutate(&bad)
				if _, err := NewMemoryStore().RecordReceipt(context.Background(), bad); !errors.Is(err, ErrInvalidReceiptInput) {
					t.Fatalf("bad execution accepted: %+v %v", bad.Execution, err)
				}
			}
		})
	}
}

func TestWorkspaceApplicationRetirementRequiresConfirmedAbsence(t *testing.T) {
	for _, mode := range []contracts.WorkspaceProvisioningMode{contracts.WorkspaceProvisioningFull, contracts.WorkspaceProvisioningResourceOnly} {
		input := validWorkspaceDeletionReceiptInput()
		if mode == contracts.WorkspaceProvisioningResourceOnly {
			input.Execution = resourceOnlyReceiptExecution(input)
		}
		evidence := contracts.WorkspaceApplicationRetirementReceipt{
			CurrentDeploymentID:   "deploy-alpha",
			Runtimes:              []contracts.WorkspaceApplicationRuntimeRetirementReceipt{{RuntimeID: "runtime-alpha", RuntimeOperationID: "deploy-alpha:runtime", State: "absent"}},
			Secrets:               []contracts.WorkspaceApplicationSecretRetirementReceipt{{SecretRef: "gateway-alpha", Ownership: "workspace_gateway", State: "absent"}, {SecretRef: "external-alpha", Ownership: "external", State: "retained"}},
			RetainedGatewayKeyIDs: []int64{9},
		}
		input.Execution["applicationRetirement"] = evidence
		input.OutputRefs["applicationGatewayKeysStatus"] = "retained"
		if _, err := NewMemoryStore().RecordReceipt(context.Background(), input); err != nil {
			t.Fatal(err)
		}
		evidence.Runtimes[0].State = "pending"
		input.Execution["applicationRetirement"] = evidence
		if _, err := NewMemoryStore().RecordReceipt(context.Background(), input); !errors.Is(err, ErrInvalidReceiptInput) {
			t.Fatalf("pending retirement accepted: %v", err)
		}
		evidence.Runtimes[0].State = "absent"
		evidence.Secrets[0].State = "retained"
		input.Execution["applicationRetirement"] = evidence
		if _, err := NewMemoryStore().RecordReceipt(context.Background(), input); !errors.Is(err, ErrInvalidReceiptInput) {
			t.Fatalf("uncleaned owned Secret accepted: %v", err)
		}
	}
}
