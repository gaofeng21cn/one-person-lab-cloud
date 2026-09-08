package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"opl-cloud/services/control-plane/internal/controlplane"
)

func TestWorkspaceLaunchRecoveryRouteChecksOriginalResultWithoutMutation(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(fmt.Sprintf("ready=%t", ready), func(t *testing.T) {
			store := newMemoryTableStore()
			fabric := &workspaceLaunchStorageResumeFabric{ready: ready}
			server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
			if err != nil {
				t.Fatal(err)
			}
			operator := reservedOperatorSessionForTest(t, server)
			row := workspaceLaunchUnknownStorageManualReviewRow(t)
			original, err := decodeWorkspaceLaunchReconcileOperation(row)
			if err != nil {
				t.Fatal(err)
			}
			mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
			base := "/api/operator/workspace-launches/" + original.ID
			preview := requestWithSession(t, server, operator, http.MethodGet, base+"/recovery", "")
			var dto workspaceLaunchRecoveryDTO
			if preview.Code != http.StatusOK || json.Unmarshal(preview.Body.Bytes(), &dto) != nil ||
				dto.OperationID != original.ID || dto.LaunchVersion != original.Version || dto.Stage != original.Stage ||
				!reflect.DeepEqual(dto.AllowedActions, []string{"check_result"}) || fabric.reads != 0 || fabric.ensures != 0 {
				t.Fatalf("recovery preview status=%d body=%s reads=%d ensures=%d", preview.Code, preview.Body.String(), fabric.reads, fabric.ensures)
			}
			body := fmt.Sprintf(`{"action":"check_result","launchVersion":%d,"reason":"check original purchase"}`, dto.LaunchVersion)
			response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, base+"/recover", body, "check-original-result")
			if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &dto) != nil || fabric.reads != 1 || fabric.ensures != 0 {
				t.Fatalf("check status=%d body=%s reads=%d ensures=%d", response.Code, response.Body.String(), fabric.reads, fabric.ensures)
			}
			persisted, _, _ := store.GetRuntimeOperation(context.Background(), original.ID)
			result, err := decodeWorkspaceLaunchReconcileOperation(persisted)
			check, consumed, found := result.resultCheckByID("check-original-result")
			if err != nil || result.ID != original.ID || !found || !consumed || check.MutationBudget != 0 || check.IdempotentReplayBudget != 0 {
				t.Fatalf("check lost original read-only authorization: operation=%s err=%v", workspaceLaunchReconcileResultSummary(result), err)
			}
			if ready {
				if dto.Status != "pending" || dto.Stage != "attachment" || len(dto.AllowedActions) != 0 || result.Attempts[original.Stage].Confirmed != 1 {
					t.Fatalf("ready did not converge original stage: %#v", dto)
				}
			} else if dto.Status != "manual_review" || dto.Stage != original.Stage || !reflect.DeepEqual(result.Attempts, original.Attempts) {
				t.Fatalf("unconfirmed check changed business outcome: %#v", dto)
			}
			replayed := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, base+"/recover", body, "check-original-result")
			after, _, _ := store.GetRuntimeOperation(context.Background(), original.ID)
			if replayed.Code != http.StatusOK || replayed.Body.String() != response.Body.String() || fabric.reads != 1 || fabric.ensures != 0 || after["result"] != persisted["result"] {
				t.Fatalf("exact replay performed work: status=%d body=%s reads=%d ensures=%d", replayed.Code, replayed.Body.String(), fabric.reads, fabric.ensures)
			}
			conflict := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, base+"/recover", body, "check-stale-version")
			if conflict.Code != http.StatusConflict || fabric.reads != 1 {
				t.Fatalf("stale version escaped: status=%d reads=%d", conflict.Code, fabric.reads)
			}
			changedBody := fmt.Sprintf(`{"action":"check_result","launchVersion":%d,"reason":"different reason"}`, original.Version)
			conflict = requestWithMutationKeyForTest(t, server, operator, http.MethodPost, base+"/recover", changedBody, "check-original-result")
			if conflict.Code != http.StatusConflict || fabric.reads != 1 {
				t.Fatalf("changed retry escaped: status=%d reads=%d", conflict.Code, fabric.reads)
			}
		})
	}
}

func TestWorkspaceLaunchRecoveryRouteRequiresOperatorAndRejectsBudgetInjection(t *testing.T) {
	store := newMemoryTableStore()
	fabric := &workspaceLaunchStorageResumeFabric{}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	customer := tenantOwnerSessionForTest(t, server)
	row := workspaceLaunchUnknownStorageManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	base := "/api/operator/workspace-launches/" + operation.ID
	body := fmt.Sprintf(`{"action":"check_result","launchVersion":%d,"reason":"check original purchase"}`, operation.Version)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		path := base + "/recovery"
		if method == http.MethodPost {
			path = base + "/recover"
		}
		response := requestWithMutationKeyForTest(t, server, customer, method, path, body, "customer-check")
		if response.Code != http.StatusForbidden {
			t.Fatalf("customer recovery accepted: method=%s status=%d body=%s", method, response.Code, response.Body.String())
		}
	}
	for _, field := range []string{"mutationBudget", "idempotentReplayBudget", "authoritativeReadBudget", "authorizedStage", "replacementWorkspaceImageDigest"} {
		input := map[string]any{"action": "check_result", "launchVersion": operation.Version, "reason": "check original purchase", field: 1}
		encoded, _ := json.Marshal(input)
		response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, base+"/recover", string(encoded), "injected-check")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("injected %s accepted: status=%d body=%s", field, response.Code, response.Body.String())
		}
	}
	if fabric.reads != 0 || fabric.ensures != 0 {
		t.Fatalf("rejected command reached provider: reads=%d ensures=%d", fabric.reads, fabric.ensures)
	}
	authorization := workspaceLaunchUnknownStorageReplayAuthorization(t, row, "existing-recovery")
	operation.ResumeAuthorization = &authorization
	operation.Version++
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	preview := requestWithSession(t, server, operator, http.MethodGet, base+"/recovery", "")
	var dto workspaceLaunchRecoveryDTO
	if preview.Code != http.StatusOK || json.Unmarshal(preview.Body.Bytes(), &dto) != nil || len(dto.AllowedActions) != 0 {
		t.Fatalf("active recovery offered another action: status=%d body=%s", preview.Code, preview.Body.String())
	}
	conflict := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, base+"/recover", body, "overlapping-check")
	if conflict.Code != http.StatusConflict || fabric.reads != 0 || fabric.ensures != 0 {
		t.Fatalf("active recovery accepted overlap: status=%d body=%s reads=%d ensures=%d", conflict.Code, conflict.Body.String(), fabric.reads, fabric.ensures)
	}
}
