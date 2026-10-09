package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

// workspaceDeletionReceiptEchoOperation is the deletion operation the Local
// qualification reaches: an application Workspace whose runtime, attachment,
// storage and compute were retired, with confirmed stage evidence and a retained
// Gateway Key. Its receipt carries typed contract values inside the Execution
// map, which is what the Ledger JSON round trip has to preserve.
func workspaceDeletionReceiptEchoOperation(t *testing.T) workspaceDeleteOperation {
	t.Helper()
	observed := func(offset time.Duration) string {
		return time.Date(2026, 10, 5, 23, 47, 20, 0, time.UTC).Add(offset).Format(time.RFC3339Nano)
	}
	evidence := []contracts.WorkspaceDeleteStageEvidence{
		{Stage: contracts.WorkspaceDeleteStageRuntimeAbsent, Result: contracts.WorkspaceDeleteEvidenceAbsent, EvidenceKind: contracts.WorkspaceDeleteEvidenceProviderReadback, ResourceID: "rt_app_e6079c7eb388aa848fa0cd621ad9d15e", ReadbackID: "readback-runtime", ObservedAt: observed(0), ReadAttempts: 1},
		{Stage: contracts.WorkspaceDeleteStageAttachmentAbsent, Result: contracts.WorkspaceDeleteEvidenceReleased, EvidenceKind: contracts.WorkspaceDeleteEvidenceLocalTransition, ResourceID: "att_dfbe5bcc22e8c8f032", ObservedAt: observed(5 * time.Millisecond), ReadAttempts: 1, MutationAttempts: 1},
		{Stage: contracts.WorkspaceDeleteStageStorageAbsent, Result: contracts.WorkspaceDeleteEvidenceAbsent, EvidenceKind: contracts.WorkspaceDeleteEvidenceProviderReadback, ResourceID: "vol_1b904a0ba620426f", ProviderResourceID: "directory/opl-workspace-alpha", ReadbackID: "readback-storage", ObservedAt: observed(20 * time.Millisecond), ReadAttempts: 1},
		{Stage: contracts.WorkspaceDeleteStageComputeAbsent, Result: contracts.WorkspaceDeleteEvidenceAbsent, EvidenceKind: contracts.WorkspaceDeleteEvidenceProviderReadback, ResourceID: "ca_6d9b7f245719d3302d", ReadbackID: "readback-compute", ObservedAt: observed(3 * time.Second), ReadAttempts: 2, MutationAttempts: 1},
		{Stage: contracts.WorkspaceDeleteStageWorkspaceAbsent, Result: contracts.WorkspaceDeleteEvidenceRemoved, EvidenceKind: contracts.WorkspaceDeleteEvidenceLocalTransition, ResourceID: "ws-87f86e098f4275e6be", ObservedAt: observed(3*time.Second + 3*time.Millisecond), ReadAttempts: 1},
	}
	if !contracts.WorkspaceDeleteReceiptEvidenceComplete(evidence) {
		t.Fatal("fixture stage evidence is not the frozen deletion sequence")
	}
	return workspaceDeleteOperation{
		SchemaVersion: 2, OperationID: "workspace-delete-098644d6490ffd48ac", AccountID: "acct-admin", OwnerUserID: "usr-admin", Sub2APIUserID: 41,
		WorkspaceID: "ws-87f86e098f4275e6be", ResourceType: "workspace", ResourceID: "ws-87f86e098f4275e6be",
		LaunchOperationID: "workspace-launch-3af2e049d5cc53628c", LaunchReceiptID: "receipt_1791243956432638099",
		RuntimeID: "rt_app_e6079c7eb388aa848fa0cd621ad9d15e", RuntimeServiceName: "opl-workspace-alpha",
		ComputeID: "ca_6d9b7f245719d3302d", StorageID: "vol_1b904a0ba620426f", AttachmentID: "att_dfbe5bcc22e8c8f032",
		WorkspaceAPIKeyID: 19, GatewaySecretRef: "opl-gateway-6ab14695c769321d", GatewayFingerprint: "sha256:" + "a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f60718293a4b5c6d7e8f90",
		ProvisioningMode: string(contracts.WorkspaceProvisioningResourceOnly), CurrentApplicationDeploymentID: "workspace-application-deploy-305c8e25d67bd92936583e9a2afe16f9cae9b6f2",
		ApplicationCleanup: &workspaceApplicationLifecycleOperation{Runtimes: []workspaceApplicationLifecycleRuntime{{
			Input: contracts.WorkspaceApplicationRuntimeLifecycleInput{RuntimeID: "rt_app_e6079c7eb388aa848fa0cd621ad9d15e", RuntimeOperationID: "workspace-application-deploy-305c8e25d67bd92936583e9a2afe16f9cae9b6f2:runtime"},
			Result: contracts.WorkspaceApplicationRuntimeLifecycleResult{
				RuntimeID: "rt_app_e6079c7eb388aa848fa0cd621ad9d15e", WorkspaceID: "ws-87f86e098f4275e6be", State: "absent",
				ImageRetirement: []contracts.WorkspaceApplicationRuntimeImageRetirement{{Image: "127.0.0.1:5005/opl-qualification-workspace@sha256:fdfa8fce2b9276d1a14004a6b280d6d61b0dd9ecec5d086cc521590002137c96", State: "retained_reference"}},
			},
		}}},
		ApplicationSecrets: &workspaceApplicationSecretCleanup{
			Secrets:               []workspaceApplicationSecretRetirement{{SecretRef: "opl-gateway-6ab14695c769321d", Ownership: "workspace_gateway", State: "absent"}},
			RetainedGatewayKeyIDs: []int64{701},
		},
		StageEvidence: evidence, Phase: "workspace_absent", Status: "running", CreatedAt: observed(0),
	}
}

// newLedgerDeletionReceiptEchoServer models the Ledger receipt endpoint the way
// the qualification reached it: the submitted receipt is stored as JSON and the
// stored record is returned, so the echo carries the same values with JSON
// object members ordered by the decoder instead of the Go struct field order
// Control Plane submitted.
func newLedgerDeletionReceiptEchoServer(t *testing.T, mutate func(*clients.ReceiptInput)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ledger/receipts" {
			t.Errorf("unexpected Ledger request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var stored clients.ReceiptInput
		if err := json.NewDecoder(r.Body).Decode(&stored); err != nil {
			t.Errorf("decode submitted receipt: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if mutate != nil {
			mutate(&stored)
		}
		payload, err := json.Marshal(clients.Receipt{ReceiptInput: stored, ReceiptID: "receipt_1791244041670052429", CreatedAt: "2026-10-05T23:47:21.670052429Z"})
		if err != nil {
			t.Errorf("encode stored receipt: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}))
}

func TestWorkspaceDeletionReceiptAcceptsExactLedgerJSONEcho(t *testing.T) {
	operation := workspaceDeletionReceiptEchoOperation(t)
	input := workspaceDeletionReceiptInput(operation)
	for _, key := range []string{"applicationRetirement", "stageEvidence"} {
		if _, present := input.Execution[key]; !present {
			t.Fatalf("fixture does not carry execution.%s, so the test proves nothing", key)
		}
	}
	echo := newLedgerDeletionReceiptEchoServer(t, nil)
	defer echo.Close()

	client := clients.NewLedgerHTTPClient(echo.URL, "internal-secret", echo.Client())
	receipt, err := client.RecordReceipt(context.Background(), input, operation.OperationID+":deletion-receipt")
	if err != nil {
		t.Fatalf("Ledger stored and echoed the submitted deletion receipt, and Control Plane refused it: %v", err)
	}
	if !clients.ReceiptInputEqual(receipt.ReceiptInput, input) {
		t.Fatal("returned deletion receipt was not recognized as the submitted input")
	}
}

func TestWorkspaceDeletionReceiptRejectsAlteredLedgerEcho(t *testing.T) {
	operation := workspaceDeletionReceiptEchoOperation(t)
	input := workspaceDeletionReceiptInput(operation)
	tests := []struct {
		name   string
		mutate func(*clients.ReceiptInput)
	}{
		{name: "retained Gateway Key", mutate: func(stored *clients.ReceiptInput) {
			retirement := stored.Execution["applicationRetirement"].(map[string]any)
			retirement["retainedGatewayKeyIds"] = []any{json.Number("702")}
		}},
		{name: "stage evidence observation", mutate: func(stored *clients.ReceiptInput) {
			digests := stored.Execution["stageEvidence"].([]any)
			digests[3].(map[string]any)["observedAt"] = "2026-10-05T23:47:22.000000000Z"
		}},
		{name: "unexpected execution member", mutate: func(stored *clients.ReceiptInput) {
			stored.Execution["unexpected"] = "value"
		}},
		{name: "output status value", mutate: func(stored *clients.ReceiptInput) {
			stored.OutputRefs["storageStatus"] = "present"
		}},
		{name: "removed launch receipt reference", mutate: func(stored *clients.ReceiptInput) {
			delete(stored.InputRefs, "launchReceiptId")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			echo := newLedgerDeletionReceiptEchoServer(t, test.mutate)
			defer echo.Close()

			client := clients.NewLedgerHTTPClient(echo.URL, "internal-secret", echo.Client())
			if _, err := client.RecordReceipt(context.Background(), input, operation.OperationID+":deletion-receipt"); err == nil {
				t.Fatal("altered Ledger echo was accepted as the submitted deletion receipt")
			}
		})
	}
}
