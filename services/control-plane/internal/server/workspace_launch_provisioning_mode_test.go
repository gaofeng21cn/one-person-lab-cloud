package server

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
)

func workspaceLaunchResourceOnlyUnitCommand() workspaceLaunchReconcileCreate {
	command := workspaceLaunchUnitCommand()
	command.Mode = contracts.WorkspaceProvisioningResourceOnly
	command.WorkspaceImageDigest = ""
	command.WorkspaceKeyGroupID = 0
	return command
}

func TestWorkspaceLaunchResourceOnlyReconcilerCompletesWithoutApplicationFacts(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchResourceOnlyUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	if operation.Stage != contracts.StageDebit {
		t.Fatalf("initial stage = %q, want %q", operation.Stage, contracts.StageDebit)
	}
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	store := &workspaceLaunchUnitStore{row: row}
	adapter := &workspaceLaunchUnitAdapter{readyStages: map[string]bool{}}
	reconciler := NewWorkspaceLaunchReconciler(store, adapter)
	resourceOnlyPlan, err := contracts.WorkspaceProvisioningStages(contracts.WorkspaceProvisioningResourceOnly)
	if err != nil {
		t.Fatal(err)
	}
	got := operation
	for range len(resourceOnlyPlan) {
		got, err = reconciler.Reconcile(context.Background(), operation.ID)
		if err != nil {
			t.Fatalf("Reconcile() unexpected error: %v", err)
		}
		if got.Status == contracts.StatusSucceeded {
			break
		}
	}
	if got.Status != contracts.StatusSucceeded || got.Stage != contracts.StageSucceeded {
		t.Fatalf("resource-only operation ended at %s", workspaceLaunchReconcileResultSummary(got))
	}
	for _, stage := range []contracts.Stage{contracts.StageKey, contracts.StageSecret, contracts.StageRuntime} {
		if adapter.mutationsByStage[string(stage)] != 0 {
			t.Fatalf("resource-only operation mutated application stage %q", stage)
		}
	}
	for _, stage := range []contracts.Stage{contracts.StageDebit, contracts.StageCompute, contracts.StageStorage, contracts.StageAttachment, contracts.StageActivation, contracts.StageReceipt} {
		if adapter.mutationsByStage[string(stage)] != 1 {
			t.Fatalf("stage %q mutations = %d, want 1", stage, adapter.mutationsByStage[string(stage)])
		}
	}
}

func TestWorkspaceLaunchResourceOnlyDecodeRejectsApplicationFacts(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchResourceOnlyUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.raw["workspaceImageDigest"] = json.RawMessage(`"repo.example/workspace@sha256:` + strings.Repeat("b", 64) + `"`)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if _, decodeErr := decodeWorkspaceLaunchReconcileOperation(row); workspaceLaunchDecodeFailureCategory(decodeErr) != "invalid_provisioning_mode" {
		t.Fatalf("decode category = %q, want invalid_provisioning_mode", workspaceLaunchDecodeFailureCategory(decodeErr))
	}
	operation.raw["workspaceKeyGroupId"] = json.RawMessage(`7`)
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if _, decodeErr := decodeWorkspaceLaunchReconcileOperation(row); workspaceLaunchDecodeFailureCategory(decodeErr) != "invalid_provisioning_mode" {
		t.Fatalf("decode category = %q, want invalid_provisioning_mode", workspaceLaunchDecodeFailureCategory(decodeErr))
	}
}

func TestWorkspaceLaunchDecodeRejectsInvalidProvisioningModeFact(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchResourceOnlyUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	operation.raw[workspaceLaunchProvisioningModeFact] = json.RawMessage(`"application_only"`)
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if _, decodeErr := decodeWorkspaceLaunchReconcileOperation(row); workspaceLaunchDecodeFailureCategory(decodeErr) != "invalid_provisioning_mode" {
		t.Fatalf("decode category = %q, want invalid_provisioning_mode", workspaceLaunchDecodeFailureCategory(decodeErr))
	}
}

func TestWorkspaceLaunchFullModeStaysApplicationCoupledByDefault(t *testing.T) {
	command := workspaceLaunchUnitCommand()
	command.WorkspaceImageDigest = ""
	if _, err := newWorkspaceLaunchReconcileOperation(command); !errors.Is(err, errInvalidWorkspaceLaunchOperation) {
		t.Fatalf("full-mode command without an image digest error = %v, want %v", err, errInvalidWorkspaceLaunchOperation)
	}
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	if operation.provisioningMode() != contracts.WorkspaceProvisioningFull {
		t.Fatalf("default mode = %q, want full", operation.provisioningMode())
	}
	if _, exists := operation.raw[workspaceLaunchProvisioningModeFact]; exists {
		t.Fatal("full-mode operation must not persist a provisioning mode fact")
	}
	if operation.Stage != contracts.StageKey {
		t.Fatalf("full-mode initial stage = %q, want %q", operation.Stage, contracts.StageKey)
	}
}

func TestWorkspaceLaunchActivationRowResourceOnlyKeepsApplicationBindingEmpty(t *testing.T) {
	operation, err := newWorkspaceLaunchReconcileOperation(workspaceLaunchResourceOnlyUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"computeAllocationId": "ca-unit", "storageId": "vol-unit", "attachmentId": "att-unit",
		"paidThrough": "2026-10-12T00:00:00Z", "periodStart": "2026-09-12T00:00:00Z",
	} {
		operation.raw[key] = json.RawMessage(`"` + value + `"`)
	}
	operation.raw["billingAnchorDay"] = json.RawMessage(`12`)
	row, err := workspaceLaunchActivationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	if row["state"] != "running" || row["status"] != "running" {
		t.Fatalf("resource-only activation state/status = %v/%v, want running", row["state"], row["status"])
	}
	if row["runtimeId"] != "" {
		t.Fatalf("resource-only activation runtimeId = %q, want empty", row["runtimeId"])
	}
	if ready, ok := nested(row, "runtime", "ready").(bool); !ok || ready {
		t.Fatalf("resource-only activation runtime ready = %v, want false", nested(row, "runtime", "ready"))
	}
	if _, exists := row["workspaceApiKeyId"]; exists {
		t.Fatal("resource-only activation must not carry a workspace API key")
	}
	operation.raw["runtimeReady"] = json.RawMessage(`true`)
	if _, err := workspaceLaunchActivationRow(operation); err == nil || !strings.Contains(err.Error(), "runtime_fact_forbidden") {
		t.Fatalf("resource-only activation with runtime facts error = %v, want runtime_fact_forbidden", err)
	}
}
