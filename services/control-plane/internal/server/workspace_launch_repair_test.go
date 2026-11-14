package server

import (
	"context"
	"testing"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

const workspaceLaunchRepairImage = "registry.example/opl/workspace@sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"

type workspaceLaunchRepairFabric struct {
	fakeFabricClient
	repairs         int
	inputs          []clients.WorkspaceRuntimeInput
	idempotencyKeys []string
	runtimeURL      string
	mutateRuntime   func(*clients.WorkspaceRuntime)
}

func (f *workspaceLaunchRepairFabric) RepairWorkspaceRuntime(_ context.Context, input clients.WorkspaceRuntimeInput, idempotencyKey string) (clients.WorkspaceRuntime, error) {
	f.repairs++
	f.inputs = append(f.inputs, input)
	f.idempotencyKeys = append(f.idempotencyKeys, idempotencyKey)
	runtime := clients.WorkspaceRuntime{
		ID: "runtime-repaired", OperationID: input.RuntimeOperationID, WorkspaceID: input.WorkspaceID,
		URL: f.runtimeURL, Status: "running", ServiceName: "runtime-repaired",
		ImageID: input.ImageID, Ready: true,
		Access: clients.WorkspaceRuntimeAccess{
			Username: "opl", CredentialStatus: "configured", CredentialVersion: "v2", SecretRef: "runtime-repaired-env",
		},
	}
	if f.mutateRuntime != nil {
		f.mutateRuntime(&runtime)
	}
	return runtime, nil
}

type workspaceLaunchRepairLedger struct {
	fakeLedgerClient
	records  int
	receipts map[string]clients.Receipt
}

func (l *workspaceLaunchRepairLedger) RecordReceipt(_ context.Context, input clients.ReceiptInput, key string) (clients.Receipt, error) {
	l.records++
	if receipt, ok := l.receipts[key]; ok {
		return receipt, nil
	}
	receipt := clients.Receipt{ReceiptInput: input, ReceiptID: "receipt-runtime-repair"}
	l.receipts[key] = receipt
	return receipt, nil
}

func (l *workspaceLaunchRepairLedger) ListReceipts(_ context.Context, query clients.ReceiptQuery) (clients.ReceiptPage, error) {
	page := clients.ReceiptPage{}
	for _, receipt := range l.receipts {
		if receipt.AccountID == query.AccountID {
			page.Receipts = append(page.Receipts, receipt)
		}
	}
	return page, nil
}

func workspaceLaunchRuntimeRepairFixture(t *testing.T) workspaceLaunchReconcileOperation {
	t.Helper()
	operation, err := newWorkspaceLaunchFixtureOperation(workspaceLaunchUnitCommand())
	if err != nil {
		t.Fatal(err)
	}
	for _, stage := range []contracts.Stage{contracts.StageKey, contracts.StageDebit, contracts.StageCompute, contracts.StageStorage, contracts.StageAttachment, contracts.StageSecret} {
		operation.Stage = stage
		facts := workspaceLaunchReadyFacts(stage)
		if stage == "debit" {
			facts["periodStart"] = "2026-08-12T00:00:00Z"
			facts["paidThrough"] = "2026-09-12T00:00:00Z"
			facts["billingAnchorDay"] = int64(12)
		}
		observation, reduceErr := reduceWorkspaceLaunchStageObservation(&operation, workspaceLaunchStageObservation{
			State: workspaceLaunchStageReady, Facts: facts,
		})
		if reduceErr != nil {
			t.Fatalf("prepare %s facts: %v", stage, reduceErr)
		}
		attempt := operation.Attempts[stage]
		attempt.Attempted, attempt.Confirmed, attempt.Unknown, attempt.Status = 1, 1, 0, "confirmed"
		attempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
		operation.Attempts[stage], operation.Observations[stage] = attempt, observation
	}
	operation.Stage, operation.Status = "runtime", "manual_review"
	runtimeAttempt := operation.Attempts["runtime"]
	runtimeAttempt.Attempted, runtimeAttempt.Confirmed, runtimeAttempt.Unknown, runtimeAttempt.Status = 1, 0, 1, "unknown"
	runtimeAttempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(operation, 1)
	operation.Attempts["runtime"] = runtimeAttempt
	operation.Observations["runtime"] = workspaceLaunchStageObservation{State: workspaceLaunchStageUnknown}
	return operation
}

func cloneWorkspaceLaunchRuntimeRepairFixture(t *testing.T, operation workspaceLaunchReconcileOperation) workspaceLaunchReconcileOperation {
	t.Helper()
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	cloned, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	return cloned
}
