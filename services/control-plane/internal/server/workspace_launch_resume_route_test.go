package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type workspaceLaunchResumeRouteSub2API struct {
	*testSub2APIClient
	keys              []clients.Sub2APIWorkspaceKey
	credentials       []clients.SessionDelegatedCredential
	expectedCreateKey string
	convergenceReads  int
	createCalls       int
}

type workspaceLaunchComputeResumeFabric struct {
	fakeFabricClient
	reads int
}

type workspaceLaunchStorageResumeFabric struct {
	fakeFabricClient
	reads   int
	ensures int
	ready   bool
}

type workspaceLaunchRuntimeImageRevisionResumeFabric struct {
	fakeFabricClient
	reads   []clients.WorkspaceLaunchStageInput
	ensures []clients.WorkspaceLaunchStageInput
}

func (*workspaceLaunchRuntimeImageRevisionResumeFabric) PreflightWorkspaceLaunch(context.Context, clients.WorkspaceLaunchPreflightInput) (clients.WorkspaceLaunchPreflight, error) {
	return clients.WorkspaceLaunchPreflight{}, errors.New("unexpected runtime revision preflight")
}

func (f *workspaceLaunchRuntimeImageRevisionResumeFabric) ReadWorkspaceLaunchStage(_ context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.reads = append(f.reads, input)
	if len(f.ensures) == 0 {
		return clients.WorkspaceLaunchStageResult{
			SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: workspaceLaunchStagePending, Reason: "runtime_image_revision_required",
			Binding: input.Binding, Resources: input.Resources,
		}, nil
	}
	resources := input.Resources
	resources.RuntimeID = "rt-original"
	resources.RuntimeServiceName = "runtime-original"
	resources.RuntimeUsername = "opl"
	resources.RuntimeURL = "https://workspace.example/original"
	resources.RuntimeCredentialStatus = "configured"
	resources.RuntimeCredentialVersion = "v1"
	resources.RuntimeCredentialSecretRef = "opl-gateway-ws-unit"
	resources.RuntimeBindingRef = input.Binding.FabricOperationID
	return clients.WorkspaceLaunchStageResult{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: workspaceLaunchStageReady, Reason: "none",
		Binding: input.Binding, Resources: resources,
	}, nil
}

func (f *workspaceLaunchRuntimeImageRevisionResumeFabric) EnsureWorkspaceLaunchStage(_ context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.ensures = append(f.ensures, input)
	return clients.WorkspaceLaunchStageResult{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: workspaceLaunchStagePending, Reason: "provider_provisioning",
		Binding: input.Binding, Resources: input.Resources,
	}, nil
}

func (*workspaceLaunchStorageResumeFabric) PreflightWorkspaceLaunch(context.Context, clients.WorkspaceLaunchPreflightInput) (clients.WorkspaceLaunchPreflight, error) {
	return clients.WorkspaceLaunchPreflight{}, errors.New("unexpected storage preflight")
}

func (f *workspaceLaunchStorageResumeFabric) ReadWorkspaceLaunchStage(_ context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.reads++
	if !f.ready {
		return clients.WorkspaceLaunchStageResult{
			SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: workspaceLaunchStageAbsent, Reason: "failed_no_resource",
			Binding: input.Binding, Resources: input.Resources,
		}, nil
	}
	resources := input.Resources
	resources.StorageID, resources.StorageBindingRef = "storage-route-ready", input.Binding.FabricOperationID
	return clients.WorkspaceLaunchStageResult{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: workspaceLaunchStageReady, Reason: "none",
		Binding: input.Binding, Resources: resources,
	}, nil
}

func (f *workspaceLaunchStorageResumeFabric) EnsureWorkspaceLaunchStage(_ context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.ensures++
	f.ready = true
	resources := input.Resources
	resources.StorageID, resources.StorageBindingRef = "storage-route-ready", input.Binding.FabricOperationID
	return clients.WorkspaceLaunchStageResult{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: workspaceLaunchStageReady, Reason: "none",
		Binding: input.Binding, Resources: resources,
	}, nil
}

func (*workspaceLaunchComputeResumeFabric) PreflightWorkspaceLaunch(context.Context, clients.WorkspaceLaunchPreflightInput) (clients.WorkspaceLaunchPreflight, error) {
	return clients.WorkspaceLaunchPreflight{}, errors.New("unexpected compute preflight")
}

func (f *workspaceLaunchComputeResumeFabric) ReadWorkspaceLaunchStage(_ context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.reads++
	return clients.WorkspaceLaunchStageResult{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: workspaceLaunchStagePending, Reason: "provider_provisioning",
		Binding: input.Binding, Resources: input.Resources,
	}, nil
}

func (*workspaceLaunchComputeResumeFabric) EnsureWorkspaceLaunchStage(context.Context, clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	return clients.WorkspaceLaunchStageResult{}, errors.New("unexpected compute ensure")
}

func (c *workspaceLaunchResumeRouteSub2API) WorkspaceKeysForConvergence(_ context.Context, userID int64, name string) ([]clients.Sub2APIWorkspaceKey, error) {
	if userID != 41 || name == "" {
		return nil, errors.New("wrong workspace key identity")
	}
	c.convergenceReads++
	result := make([]clients.Sub2APIWorkspaceKey, 0, len(c.keys))
	for _, key := range c.keys {
		if key.UserID == userID && key.Name == name {
			result = append(result, key)
		}
	}
	return result, nil
}

func (c *workspaceLaunchResumeRouteSub2API) WorkspaceUserKeysForConvergence(ctx context.Context, credential clients.SessionDelegatedCredential, userID int64, name string) ([]clients.Sub2APIWorkspaceKey, error) {
	if credential.Bearer != "test-user-delegated-token" {
		return nil, errors.New("wrong delegated key read")
	}
	return c.WorkspaceKeysForConvergence(ctx, userID, name)
}

func (c *workspaceLaunchResumeRouteSub2API) CreateUserKey(_ context.Context, credential clients.SessionDelegatedCredential, userID int64, input clients.Sub2APICreateKeyInput, idempotencyKey string) (clients.Sub2APIWorkspaceKey, error) {
	c.credentials = append(c.credentials, credential)
	if credential.Bearer != "test-user-delegated-token" || userID != 41 || input.GroupID != 7 || idempotencyKey != c.expectedCreateKey {
		return clients.Sub2APIWorkspaceKey{}, errors.New("wrong delegated key mutation")
	}
	c.createCalls++
	groupID := input.GroupID
	key := clients.Sub2APIWorkspaceKey{ID: 19, UserID: userID, Name: input.Name, Key: "route-created-key-secret", GroupID: &groupID, Status: "active"}
	c.keys = append(c.keys, key)
	return key, nil
}

func (*workspaceLaunchResumeRouteSub2API) UpdateUserKey(context.Context, clients.SessionDelegatedCredential, int64, int64, clients.Sub2APIUpdateKeyInput) (clients.Sub2APIWorkspaceKey, error) {
	return clients.Sub2APIWorkspaceKey{}, errors.New("unexpected key update")
}

func (*workspaceLaunchResumeRouteSub2API) DeleteUserKey(context.Context, clients.SessionDelegatedCredential, int64, int64) error {
	return errors.New("unexpected key delete")
}

func TestWorkspaceLaunchResumeRouteWaitsForOriginalCallerCredential(t *testing.T) {
	t.Setenv(productionAcceptanceBResumeExistingApprovalEnv, `{"configured":true}`)
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	client := &workspaceLaunchResumeRouteSub2API{testSub2APIClient: &testSub2APIClient{
		balance: 100_000_000, charges: map[string]int64{},
		identities: map[string]clients.Sub2APIIdentity{"alpha@example.com": {ID: 41, Email: "alpha@example.com", Status: "active"}},
		passwords:  map[string]string{"alpha@example.com": "CorrectHorseBatteryStaple!"},
	}}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	operatorUserID := sessionUserIDForTest(t, server, operator)
	customer := loginForTest(t, server, "alpha@example.com", "CorrectHorseBatteryStaple!")
	launchKey := "launch-route-key"
	command := workspaceLaunchUnitCommand()
	command.OperationID = workspaceLaunchOperationID("acct-alpha", launchKey)
	command.AccountID = "acct-alpha"
	command.OwnerUserID = "usr-alpha"
	command.Sub2APIUserID = 41
	command.WorkspaceID = "ws-route-resume"
	command.Name = "Route Resume"
	client.expectedCreateKey = command.OperationID + ":workspace-key"
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	operation.Status = "manual_review"
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))

	resumeBody := `{"launchVersion":1,"authorizedStage":"key","reason":"bounded operator retry","mutationBudget":1}`
	resume := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", resumeBody, "resume-route-key")
	if resume.Code != http.StatusOK {
		t.Fatalf("operator resume status=%d body=%s", resume.Code, resume.Body.String())
	}
	waitingRow, found, err := store.GetRuntimeOperation(context.Background(), operation.ID)
	if err != nil || !found {
		t.Fatalf("read waiting launch found=%v err=%v", found, err)
	}
	waiting, err := decodeWorkspaceLaunchReconcileOperation(waitingRow)
	if err != nil || waiting.Status != "pending" || waiting.Stage != "key" || waiting.Attempts["key"].Attempted != 0 ||
		waiting.ResumeAuthorization == nil || waiting.ResumeAuthorization.AuthorizationID != "resume-route-key" || waiting.ResumeAuthorizationConsumedAt != "" || client.createCalls != 0 {
		t.Fatalf("operator consumed credential-bound authorization: operation=%s creates=%d err=%v", workspaceLaunchReconcileResultSummary(waiting), client.createCalls, err)
	}

	launchBody := `{"name":"Route Resume","packageId":"basic","autoRenew":false}`
	continuedResponse := requestWithMutationKeyForTest(t, server, customer, http.MethodPost, "/api/workspace-launches", launchBody, launchKey)
	if continuedResponse.Code != http.StatusAccepted {
		t.Fatalf("customer continuation status=%d body=%s", continuedResponse.Code, continuedResponse.Body.String())
	}
	continuedRow, found, err := store.GetRuntimeOperation(context.Background(), operation.ID)
	if err != nil || !found {
		t.Fatalf("read continued launch found=%v err=%v", found, err)
	}
	continued, err := decodeWorkspaceLaunchReconcileOperation(continuedRow)
	if err != nil || continued.Status != "pending" || continued.Stage != "debit" || continued.Attempts["key"].Attempted != 1 ||
		continued.ResumeAuthorization == nil || continued.ResumeAuthorization.AuthorizationID != "resume-route-key" || continued.ResumeAuthorizationConsumedAt == "" ||
		client.createCalls != 1 || len(client.credentials) != 1 || client.credentials[0].Bearer != "test-user-delegated-token" {
		t.Fatalf("customer continuation operation=%s creates=%d credentials=%#v err=%v", workspaceLaunchReconcileResultSummary(continued), client.createCalls, client.credentials, err)
	}
	if strings.Contains(stringValue(continuedRow["result"]), "test-user-delegated-token") {
		t.Fatal("persisted launch result contains delegated bearer")
	}
	readbackResponse := requestWithSession(t, server, operator, http.MethodGet,
		"/api/operator/workspace-launches/"+operation.ID+"/resume-authorizations/resume-route-key", "")
	var readback map[string]any
	if readbackResponse.Code != http.StatusOK || json.Unmarshal(readbackResponse.Body.Bytes(), &readback) != nil ||
		readback["status"] != "consumed" || readback["authorizedBy"] != operatorUserID || readback["consumedAt"] == "" || readback["singleUse"] != true ||
		int(numberField(readback, "authorizationVersion", 0)) != 1 || int(numberField(readback, "operationVersion", 0)) != continued.Version {
		t.Fatalf("resume authorization readback status=%d body=%s", readbackResponse.Code, readbackResponse.Body.String())
	}
	attemptReadback, _ := readback["attempt"].(map[string]any)
	if int(numberField(attemptReadback, "attempted", 0)) != 1 || int(numberField(attemptReadback, "confirmed", 0)) != 1 ||
		int(numberField(attemptReadback, "unknown", 0)) != 0 || attemptReadback["status"] != "confirmed" {
		t.Fatalf("resume attempt readback=%#v", attemptReadback)
	}

	readsBefore := client.convergenceReads
	exactResume := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", resumeBody, "resume-route-key")
	exactLaunch := requestWithMutationKeyForTest(t, server, customer, http.MethodPost, "/api/workspace-launches", launchBody, launchKey)
	if exactResume.Code != http.StatusOK || exactLaunch.Code != http.StatusAccepted || client.createCalls != 1 || client.convergenceReads != readsBefore {
		t.Fatalf("exact retries caused work: resume=%d launch=%d creates=%d reads=%d/%d", exactResume.Code, exactLaunch.Code, client.createCalls, client.convergenceReads, readsBefore)
	}

	replayLaunchKey := "launch-route-replay-key"
	replayDescriptor, err := newWorkspaceLaunchDescriptor(command.AccountID, command.OwnerUserID, command.Name, command.PackageID, command.StorageGB, command.AutoRenew, command.PriceVersion, replayLaunchKey)
	if err != nil {
		t.Fatal(err)
	}
	replayCommand := command
	replayCommand.OperationID = replayDescriptor.OperationID
	replayCommand.RequestHash = replayDescriptor.RequestHash
	replayCommand.WorkspaceID = replayDescriptor.WorkspaceID
	replayCommand.WorkspaceImageDigest = replayDescriptor.WorkspaceImageDigest
	replayOperation, err := newWorkspaceLaunchReconcileOperation(replayCommand)
	if err != nil {
		t.Fatal(err)
	}
	replayOperation.Status = "manual_review"
	replayAttempt := replayOperation.Attempts["key"]
	replayAttempt.Attempted, replayAttempt.Status = 1, "reserved"
	replayAttempt.IdempotencyKey = workspaceLaunchStageIdempotencyKey(replayOperation, 1)
	replayOperation.Attempts["key"] = replayAttempt
	replayRow, err := workspaceLaunchReconcileOperationRow(replayOperation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), replayRow))
	replayBody := `{"launchVersion":1,"authorizedStage":"key","reason":"owner read proved exact key absent","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":3}`
	replay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+replayOperation.ID+"/resume", replayBody, "resume-route-replay-key")
	if replay.Code != http.StatusOK {
		t.Fatalf("operator replay status=%d body=%s", replay.Code, replay.Body.String())
	}
	persistedReplayRow, found, err := store.GetRuntimeOperation(context.Background(), replayOperation.ID)
	if err != nil || !found {
		t.Fatalf("read replay launch found=%v err=%v", found, err)
	}
	persistedReplay, err := decodeWorkspaceLaunchReconcileOperation(persistedReplayRow)
	if err != nil || persistedReplay.ResumeAuthorization == nil || persistedReplay.ResumeAuthorization.IdempotentReplayBudget != 1 ||
		persistedReplay.ResumeAuthorization.AuthoritativeReadBudget != workspaceLaunchAuthoritativeReadBudget || persistedReplay.ResumeAuthorizationConsumedAt != "" ||
		persistedReplay.Attempts["key"].Attempted != 1 || client.createCalls != 1 {
		t.Fatalf("operator replay authorization not durable: operation=%s creates=%d err=%v", workspaceLaunchReconcileResultSummary(persistedReplay), client.createCalls, err)
	}

	invalidReplayBody := `{"launchVersion":1,"authorizedStage":"key","reason":"missing read budget","mutationBudget":0,"idempotentReplayBudget":1}`
	invalidReplay := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+replayOperation.ID+"/resume", invalidReplayBody, "resume-route-invalid-replay")
	if invalidReplay.Code != http.StatusBadRequest {
		t.Fatalf("incomplete replay authorization status=%d body=%s", invalidReplay.Code, invalidReplay.Body.String())
	}

	client.expectedCreateKey = replayAttempt.IdempotencyKey
	replayedResponse := requestWithMutationKeyForTest(t, server, customer, http.MethodPost, "/api/workspace-launches", launchBody, replayLaunchKey)
	if replayedResponse.Code != http.StatusAccepted {
		t.Fatalf("customer replay continuation status=%d body=%s", replayedResponse.Code, replayedResponse.Body.String())
	}
	replayedRow, found, err := store.GetRuntimeOperation(context.Background(), replayOperation.ID)
	if err != nil || !found {
		t.Fatalf("read customer replay launch found=%v err=%v", found, err)
	}
	replayedOperation, err := decodeWorkspaceLaunchReconcileOperation(replayedRow)
	replayedAttempt := replayedOperation.Attempts["key"]
	if err != nil || replayedOperation.Status != "pending" || replayedOperation.Stage != "debit" || replayedAttempt.Attempted != 1 || replayedAttempt.Max != 1 || replayedAttempt.Confirmed != 1 ||
		replayedAttempt.IdempotencyKey != replayAttempt.IdempotencyKey || replayedOperation.IdempotentReplayClaims["key"].Status != "succeeded" ||
		replayedOperation.ResumeAuthorizationConsumedAt == "" || client.createCalls != 2 || len(client.credentials) != 2 || client.credentials[1].Bearer != "test-user-delegated-token" {
		t.Fatalf("customer replay did not use original credential and identity: operation=%s attempt=%#v creates=%d credentials=%#v err=%v", workspaceLaunchReconcileResultSummary(replayedOperation), replayedAttempt, client.createCalls, client.credentials, err)
	}
	if strings.Contains(stringValue(replayedRow["result"]), "test-user-delegated-token") || strings.Contains(stringValue(replayedRow["result"]), "route-created-key-secret") {
		t.Fatal("persisted replay result contains delegated bearer or raw key")
	}

	readsAfterReplay, createsAfterReplay := client.convergenceReads, client.createCalls
	exactReplay := requestWithMutationKeyForTest(t, server, customer, http.MethodPost, "/api/workspace-launches", launchBody, replayLaunchKey)
	if exactReplay.Code != http.StatusAccepted || client.createCalls != createsAfterReplay || client.convergenceReads != readsAfterReplay {
		t.Fatalf("exact customer replay caused key work: status=%d creates=%d/%d reads=%d/%d", exactReplay.Code, client.createCalls, createsAfterReplay, client.convergenceReads, readsAfterReplay)
	}
}

func TestWorkspaceLaunchResumeRouteAcceptsComputeWindowAndRejectsItForOtherStages(t *testing.T) {
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	fabric := &workspaceLaunchComputeResumeFabric{}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	row := workspaceLaunchUnknownStageManualReviewRow(t, "ensure_compute_allocation")
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))

	invalidBody := `{"launchVersion":5,"authorizedStage":"runtime","reason":"non compute cannot widen read window","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":60}`
	invalid := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", invalidBody, "resume-route-non-compute-window")
	if invalid.Code != http.StatusBadRequest || fabric.reads != 0 {
		t.Fatalf("non-compute widened read window status=%d body=%s reads=%d", invalid.Code, invalid.Body.String(), fabric.reads)
	}

	body := `{"launchVersion":5,"authorizedStage":"ensure_compute_allocation","reason":"provider read proves the original compute allocation can continue","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":60}`
	response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", body, "resume-route-compute-window")
	if response.Code != http.StatusOK || fabric.reads != 2 {
		t.Fatalf("compute resume window status=%d body=%s reads=%d", response.Code, response.Body.String(), fabric.reads)
	}
	persisted, found, err := store.GetRuntimeOperation(context.Background(), operation.ID)
	got, decodeErr := decodeWorkspaceLaunchReconcileOperation(persisted)
	if err != nil || !found || decodeErr != nil || got.Status != "pending" || got.Stage != "ensure_compute_allocation" ||
		got.Attempts[got.Stage].Attempted != 1 || got.Attempts[got.Stage].Unknown != 0 || got.ResumeAuthorization == nil ||
		got.ResumeAuthorization.IdempotentReplayBudget != 1 || got.ResumeAuthorization.AuthoritativeReadBudget != workspaceLaunchComputeFreshContinuationAdditionalReadBudget {
		t.Fatalf("compute resume route did not preserve the original operation: found=%v operation=%s errors=%v/%v", found, workspaceLaunchReconcileResultSummary(got), err, decodeErr)
	}
}

func TestWorkspaceLaunchResumeRouteRevisesOriginalTencentRuntimeImage(t *testing.T) {
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	fabric := &workspaceLaunchRuntimeImageRevisionResumeFabric{}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	row := workspaceLaunchRuntimeImageRevisionManualReviewRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	originalResult := stringValue(row["result"])
	originalKey := operation.Attempts["runtime"].IdempotencyKey
	originalBinding := operation.ID + ":runtime"
	replacementImage := "registry.example/workspace@sha256:" + strings.Repeat("c", 64)
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))

	t.Setenv("OPL_WORKSPACE_IMAGE", "registry.example/workspace@sha256:"+strings.Repeat("d", 64))
	body := fmt.Sprintf(`{"launchVersion":%d,"authorizedStage":"runtime","reason":"approved replacement on the original Tencent runtime stage","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":3,"replacementWorkspaceImageDigest":%q}`, operation.Version, replacementImage)
	invalidBudgetBody := strings.Replace(body, `"mutationBudget":0`, `"mutationBudget":1`, 1)
	invalidBudget := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", invalidBudgetBody, "resume-runtime-image-invalid-budget")
	if invalidBudget.Code != http.StatusBadRequest || len(fabric.reads) != 0 || len(fabric.ensures) != 0 {
		t.Fatalf("invalid Runtime image revision shape reached Fabric: status=%d body=%s reads=%d ensures=%d", invalidBudget.Code, invalidBudget.Body.String(), len(fabric.reads), len(fabric.ensures))
	}
	rejected := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", body, "resume-runtime-image-mismatch")
	afterRejected, found, readErr := store.GetRuntimeOperation(context.Background(), operation.ID)
	if rejected.Code != http.StatusConflict || readErr != nil || !found || stringValue(afterRejected["result"]) != originalResult || len(fabric.reads) != 0 || len(fabric.ensures) != 0 {
		t.Fatalf("deployed image mismatch changed Launch: status=%d body=%s found=%v readErr=%v reads=%d ensures=%d", rejected.Code, rejected.Body.String(), found, readErr, len(fabric.reads), len(fabric.ensures))
	}

	t.Setenv("OPL_WORKSPACE_IMAGE", replacementImage)
	authorizationID := "resume-runtime-image-original-launch"
	response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", body, authorizationID)
	persisted, found, readErr := store.GetRuntimeOperation(context.Background(), operation.ID)
	got, decodeErr := decodeWorkspaceLaunchReconcileOperation(persisted)
	if response.Code != http.StatusOK || readErr != nil || !found || decodeErr != nil || got.Status != "pending" || got.Stage != "activation" ||
		got.Attempts["runtime"].Confirmed != 1 || got.Attempts["runtime"].Unknown != 0 || got.Attempts["runtime"].MaxPendingReadbacks != 2*workspaceLaunchAuthoritativeReadBudget ||
		got.Attempts["runtime"].IdempotencyKey != originalKey || got.ResumeAuthorization == nil || got.ResumeAuthorization.AuthorizationID != authorizationID ||
		got.ResumeAuthorization.ReplacementWorkspaceImageDigest != replacementImage || got.ResumeAuthorizationConsumedAt == "" || got.RuntimeRepair != nil ||
		len(fabric.reads) != 4 || len(fabric.ensures) != 1 {
		t.Fatalf("runtime revision route did not continue original Launch: status=%d body=%s found=%v operation=%s reads=%d ensures=%d errors=%v/%v",
			response.Code, response.Body.String(), found, workspaceLaunchReconcileResultSummary(got), len(fabric.reads), len(fabric.ensures), readErr, decodeErr)
	}
	expectedDigest := workspaceLaunchResumeAuthorizationDigest(*got.ResumeAuthorization)
	for index, input := range append(append([]clients.WorkspaceLaunchStageInput(nil), fabric.reads...), fabric.ensures...) {
		proof := input.RuntimeImageRevision
		if input.Binding.LaunchOperationID != operation.ID || input.Binding.WorkspaceID != operation.stringFact("workspaceId") ||
			input.Binding.FabricOperationID != originalBinding || input.Binding.IdempotencyKey != originalKey ||
			input.WorkspaceImageDigest != operation.stringFact("workspaceImageDigest") || proof == nil ||
			proof.LaunchOperationID != operation.ID || proof.WorkspaceID != operation.stringFact("workspaceId") || proof.RuntimeOperationID != originalBinding ||
			proof.AuthorizationDigest != expectedDigest || proof.PreviousImageDigest != operation.stringFact("workspaceImageDigest") || proof.ReplacementImageDigest != replacementImage {
			t.Fatalf("Fabric call %d changed original identity or typed proof: input=%#v", index, input)
		}
	}
}

func TestWorkspaceLaunchResumeRouteConvergesFailedStorageReplayReadyReadOnly(t *testing.T) {
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	fabric := &workspaceLaunchStorageResumeFabric{ready: true}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	row := workspaceLaunchUnknownStorageAfterFailedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	claimBefore := operation.IdempotentReplayClaims["storage"]
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))

	body := fmt.Sprintf(`{"launchVersion":%d,"authorizedStage":"storage","reason":"authoritative read may confirm or replay the original storage attempt","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":3}`, operation.Version)
	authorizationID := "resume-route-failed-storage-ready"
	response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", body, authorizationID)
	if response.Code != http.StatusOK || fabric.reads != 1 || fabric.ensures != 0 {
		t.Fatalf("failed storage replay ready-read status=%d body=%s reads=%d ensures=%d", response.Code, response.Body.String(), fabric.reads, fabric.ensures)
	}
	persisted, found, err := store.GetRuntimeOperation(context.Background(), operation.ID)
	got, decodeErr := decodeWorkspaceLaunchReconcileOperation(persisted)
	if err != nil || !found || decodeErr != nil || got.Stage != "attachment" || got.Status != "pending" ||
		got.Attempts["storage"].Confirmed != 1 || got.Attempts["storage"].Unknown != 0 || got.IdempotentReplayClaims["storage"] != claimBefore ||
		got.ResumeAuthorization == nil || got.ResumeAuthorization.AuthorizationID != authorizationID || got.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("failed storage replay route did not persist ready transition: found=%v operation=%s errors=%v/%v", found, workspaceLaunchReconcileResultSummary(got), err, decodeErr)
	}

	readsBefore, persistedBefore := fabric.reads, stringValue(persisted["result"])
	replayed := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", body, authorizationID)
	after, _, _ := store.GetRuntimeOperation(context.Background(), operation.ID)
	if replayed.Code != http.StatusOK || fabric.reads != readsBefore || fabric.ensures != 0 || stringValue(after["result"]) != persistedBefore {
		t.Fatalf("exact route retry repeated work: status=%d body=%s reads=%d/%d ensures=%d", replayed.Code, replayed.Body.String(), fabric.reads, readsBefore, fabric.ensures)
	}
}

func TestWorkspaceLaunchResumeRouteReplaysFailedStorageAfterAuthoritativeAbsence(t *testing.T) {
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	fabric := &workspaceLaunchStorageResumeFabric{}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	row := workspaceLaunchUnknownStorageAfterFailedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))

	body := fmt.Sprintf(`{"launchVersion":%d,"authorizedStage":"storage","reason":"authoritative absence permits one original-key storage replay","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":3}`, operation.Version)
	authorizationID := "resume-route-failed-storage-absent"
	response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", body, authorizationID)
	if response.Code != http.StatusOK || fabric.ensures != 1 {
		t.Fatalf("failed storage replay absent status=%d body=%s reads=%d ensures=%d", response.Code, response.Body.String(), fabric.reads, fabric.ensures)
	}
	persisted, found, err := store.GetRuntimeOperation(context.Background(), operation.ID)
	got, decodeErr := decodeWorkspaceLaunchReconcileOperation(persisted)
	if err != nil || !found || decodeErr != nil || got.Stage != "attachment" || got.Status != "pending" ||
		got.Attempts["storage"].Confirmed != 1 || got.Attempts["storage"].Unknown != 0 ||
		got.IdempotentReplayClaims["storage"].AuthorizationID != authorizationID || got.IdempotentReplayClaims["storage"].Status != "succeeded" ||
		got.ResumeAuthorization == nil || got.ResumeAuthorization.AuthorizationID != authorizationID || got.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("failed storage replay route did not preserve the original launch: found=%v operation=%s errors=%v/%v", found, workspaceLaunchReconcileResultSummary(got), err, decodeErr)
	}

	readsBefore, persistedBefore := fabric.reads, stringValue(persisted["result"])
	replayed := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", body, authorizationID)
	after, _, _ := store.GetRuntimeOperation(context.Background(), operation.ID)
	if replayed.Code != http.StatusOK || fabric.reads != readsBefore || fabric.ensures != 1 || stringValue(after["result"]) != persistedBefore {
		t.Fatalf("exact route retry repeated storage: status=%d body=%s reads=%d/%d ensures=%d", replayed.Code, replayed.Body.String(), fabric.reads, readsBefore, fabric.ensures)
	}
}

func TestWorkspaceLaunchResumeRouteConvergesExhaustedStorageReadyReadOnly(t *testing.T) {
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	fabric := &workspaceLaunchStorageResumeFabric{ready: true}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	row := workspaceLaunchStorageAfterExhaustedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	claimBefore := operation.IdempotentReplayClaims["storage"]
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))

	body := fmt.Sprintf(`{"launchVersion":%d,"authorizedStage":"storage","reason":"authoritative ready read may continue the original exhausted storage stage","mutationBudget":0,"idempotentReplayBudget":0,"authoritativeReadBudget":3}`, operation.Version)
	authorizationID := "resume-route-exhausted-storage-ready"
	response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", body, authorizationID)
	if response.Code != http.StatusOK || fabric.reads != 1 || fabric.ensures != 0 {
		t.Fatalf("exhausted storage ready-read status=%d body=%s reads=%d ensures=%d", response.Code, response.Body.String(), fabric.reads, fabric.ensures)
	}
	persisted, found, err := store.GetRuntimeOperation(context.Background(), operation.ID)
	got, decodeErr := decodeWorkspaceLaunchReconcileOperation(persisted)
	if err != nil || !found || decodeErr != nil || got.Stage != "attachment" || got.Status != "pending" ||
		got.Attempts["storage"].Confirmed != 1 || got.Attempts["storage"].Unknown != 0 || got.IdempotentReplayClaims["storage"] != claimBefore ||
		got.ResumeAuthorization == nil || got.ResumeAuthorization.AuthorizationID != authorizationID || got.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("exhausted storage route did not persist ready transition: found=%v operation=%s errors=%v/%v", found, workspaceLaunchReconcileResultSummary(got), err, decodeErr)
	}
}

func TestWorkspaceLaunchResumeRouteContinuesExhaustedStorageBindingAfterAuthoritativeAbsence(t *testing.T) {
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	fabric := &workspaceLaunchStorageResumeFabric{}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	row := workspaceLaunchStorageAfterExhaustedReplayRow(t)
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	originalKey := operation.Attempts["storage"].IdempotencyKey
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))

	body := fmt.Sprintf(`{"launchVersion":%d,"authorizedStage":"storage","reason":"authoritative absence permits the original storage binding to continue","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":3}`, operation.Version)
	authorizationID := "resume-route-exhausted-storage-binding"
	response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", body, authorizationID)
	if response.Code != http.StatusOK || fabric.reads != 4 || fabric.ensures != 1 {
		t.Fatalf("exhausted storage binding status=%d body=%s reads=%d ensures=%d", response.Code, response.Body.String(), fabric.reads, fabric.ensures)
	}
	persisted, found, err := store.GetRuntimeOperation(context.Background(), operation.ID)
	got, decodeErr := decodeWorkspaceLaunchReconcileOperation(persisted)
	claim := got.IdempotentReplayClaims["storage"]
	if err != nil || !found || decodeErr != nil || got.Stage != "attachment" || got.Status != "pending" ||
		got.Attempts["storage"].Confirmed != 1 || got.Attempts["storage"].Unknown != 0 ||
		got.Attempts["storage"].IdempotencyKey != originalKey || claim.AuthorizationID != authorizationID || claim.Status != "succeeded" ||
		got.idempotentReplayAuthorizationCount("storage") != 3 || got.ResumeAuthorization == nil ||
		got.ResumeAuthorization.AuthorizationID != authorizationID || got.ResumeAuthorizationConsumedAt == "" {
		t.Fatalf("exhausted storage binding route did not preserve the original launch: found=%v operation=%s errors=%v/%v", found, workspaceLaunchReconcileResultSummary(got), err, decodeErr)
	}
}

func TestWorkspaceLaunchResumeRouteRequiresExactRemainingComputeWindow(t *testing.T) {
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	fabric := &workspaceLaunchComputeResumeFabric{}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, fabric, &testSub2APIClient{}), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	row := workspaceLaunchUnknownStageManualReviewRow(t, "ensure_compute_allocation")
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		t.Fatal(err)
	}
	attempt := operation.Attempts[operation.Stage]
	attempt.PendingReadbacks, attempt.MaxPendingReadbacks = 46, workspaceLaunchComputeFreshContinuationAdditionalReadBudget
	operation.Attempts[operation.Stage] = attempt
	row, err = workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))

	tooLargeBody := `{"launchVersion":5,"authorizedStage":"ensure_compute_allocation","reason":"remaining compute window must stay bounded","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":18}`
	tooLarge := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", tooLargeBody, "resume-route-compute-window-too-large")
	if tooLarge.Code != http.StatusConflict || fabric.reads != 0 {
		t.Fatalf("oversized remaining compute window status=%d body=%s reads=%d", tooLarge.Code, tooLarge.Body.String(), fabric.reads)
	}

	body := `{"launchVersion":5,"authorizedStage":"ensure_compute_allocation","reason":"continue within the exact remaining compute window","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":17}`
	response := requestWithMutationKeyForTest(t, server, operator, http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", body, "resume-route-compute-window-remaining")
	if response.Code != http.StatusOK || fabric.reads != 2 {
		t.Fatalf("remaining compute window status=%d body=%s reads=%d", response.Code, response.Body.String(), fabric.reads)
	}
	persisted, found, err := store.GetRuntimeOperation(context.Background(), operation.ID)
	got, decodeErr := decodeWorkspaceLaunchReconcileOperation(persisted)
	gotAttempt := got.Attempts[got.Stage]
	if err != nil || !found || decodeErr != nil || got.Status != "pending" || got.Stage != "ensure_compute_allocation" ||
		gotAttempt.PendingReadbacks != 47 || gotAttempt.MaxPendingReadbacks != workspaceLaunchMaximumPersistedReadbacks(got.Stage) ||
		got.ResumeAuthorization == nil || got.ResumeAuthorization.ReadbacksAtAuthorization != 46 || got.ResumeAuthorization.AuthoritativeReadBudget != 17 {
		t.Fatalf("remaining compute window was not persisted exactly: found=%v operation=%s attempt=%#v errors=%v/%v",
			found, workspaceLaunchReconcileResultSummary(got), gotAttempt, err, decodeErr)
	}
}

func TestAcceptanceBResumeExistingRoutePersistsApprovalBindingAndConvergesReady(t *testing.T) {
	configureProductionAcceptanceBEnvironment(t)
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	groupID := int64(7)
	command := workspaceLaunchUnitCommand()
	command.AccountID, command.OwnerUserID, command.Sub2APIUserID = "acct-alpha", "usr-alpha", 41
	command.WorkspaceKeyGroupID, command.WorkspaceID = groupID, "ws-acceptance-b-resume"
	client := &workspaceLaunchResumeRouteSub2API{
		testSub2APIClient: &testSub2APIClient{balance: 100_000_000, charges: map[string]int64{}},
		keys: []clients.Sub2APIWorkspaceKey{{
			ID: 19, UserID: 41, Name: workspaceReservedKeyName(command.WorkspaceID), Key: "acceptance-b-existing-key", GroupID: &groupID, Status: "active",
		}},
	}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	operation.Status = "manual_review"
	attempt := operation.Attempts["key"]
	attempt.Attempted, attempt.Status, attempt.IdempotencyKey = 1, "reserved", workspaceLaunchStageIdempotencyKey(operation, 1)
	operation.Attempts["key"] = attempt
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	authorization := workspaceLaunchResumeAuthorization{
		AuthorizationID: "acceptance-b-resume-route", LaunchVersion: operation.Version, AuthorizedStage: "key",
		AuthorizedBy: "server-owned", AuthorizedAt: "2026-08-16T00:00:00Z", Reason: "owner ready convergence",
		MutationBudget: 0, IdempotentReplayBudget: 1, AuthoritativeReadBudget: workspaceLaunchAuthoritativeReadBudget,
	}
	parseProductionAcceptanceBResumeExistingApprovalFixture(t,
		canonicalProductionAcceptanceBResumeExistingApproval(operation, authorization, workspaceLaunchStageReady))
	persistedBefore := stringValue(row["result"])
	for name, configureHeaders := range map[string]func(http.Header){
		"approval only": func(header http.Header) {
			header.Set(productionAcceptanceBApprovalID, "acceptance-b-resume-existing-approval")
		},
		"capability only": func(header http.Header) {
			header.Set(productionAcceptanceBCapability, "acceptance-b-capability")
		},
		"duplicate approval": func(header http.Header) {
			header.Add(productionAcceptanceBApprovalID, "acceptance-b-resume-existing-approval")
			header.Add(productionAcceptanceBApprovalID, "acceptance-b-resume-existing-approval")
			header.Set(productionAcceptanceBCapability, "acceptance-b-capability")
		},
		"duplicate capability": func(header http.Header) {
			header.Set(productionAcceptanceBApprovalID, "acceptance-b-resume-existing-approval")
			header.Add(productionAcceptanceBCapability, "acceptance-b-capability")
			header.Add(productionAcceptanceBCapability, "acceptance-b-capability")
		},
		"duplicate authorization ID": func(header http.Header) {
			header.Add("Idempotency-Key", authorization.AuthorizationID)
			header.Add("Idempotency-Key", authorization.AuthorizationID)
			header.Set(productionAcceptanceBApprovalID, "acceptance-b-resume-existing-approval")
			header.Set(productionAcceptanceBCapability, "acceptance-b-capability")
		},
		"invalid authorization ID": func(header http.Header) {
			header.Set("Idempotency-Key", "x")
			header.Set(productionAcceptanceBApprovalID, "acceptance-b-resume-existing-approval")
			header.Set(productionAcceptanceBCapability, "acceptance-b-capability")
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", strings.NewReader(
				`{"launchVersion":1,"authorizedStage":"key","reason":"owner ready convergence","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":3}`))
			addAuth(request, operator)
			request.Header.Set("Content-Type", "application/json")
			configureHeaders(request.Header)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			persisted, found, readErr := store.GetRuntimeOperation(context.Background(), operation.ID)
			if response.Code != http.StatusBadRequest || readErr != nil || !found || stringValue(persisted["result"]) != persistedBefore ||
				client.convergenceReads != 0 || client.createCalls != 0 {
				t.Fatalf("invalid headers changed owner state: status=%d body=%s reads=%d creates=%d found=%v err=%v", response.Code, response.Body.String(), client.convergenceReads, client.createCalls, found, readErr)
			}
		})
	}

	legacyBody := `{"launchVersion":1,"authorizedStage":"key","reason":"owner ready convergence","mutationBudget":0}`
	legacyRequest := httptest.NewRequest(http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", strings.NewReader(legacyBody))
	addAuth(legacyRequest, operator)
	legacyRequest.Header.Set("Content-Type", "application/json")
	legacyRequest.Header.Set("Idempotency-Key", authorization.AuthorizationID)
	for name, values := range productionAcceptanceBResumeHeaders() {
		for _, value := range values {
			legacyRequest.Header.Add(name, value)
		}
	}
	legacyResponse := httptest.NewRecorder()
	server.ServeHTTP(legacyResponse, legacyRequest)
	if legacyResponse.Code != http.StatusBadRequest || client.createCalls != 0 {
		t.Fatalf("legacy Acceptance B resume status=%d body=%s creates=%d", legacyResponse.Code, legacyResponse.Body.String(), client.createCalls)
	}

	body := `{"launchVersion":1,"authorizedStage":"key","reason":"owner ready convergence","mutationBudget":0,"idempotentReplayBudget":1,"authoritativeReadBudget":3}`
	req := httptest.NewRequest(http.MethodPost, "/api/operator/workspace-launches/"+operation.ID+"/resume", strings.NewReader(body))
	addAuth(req, operator)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", authorization.AuthorizationID)
	for name, values := range productionAcceptanceBResumeHeaders() {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	if response.Code != http.StatusOK || client.createCalls != 0 {
		t.Fatalf("Acceptance B resume status=%d body=%s creates=%d", response.Code, response.Body.String(), client.createCalls)
	}
	var payload map[string]any
	if json.Unmarshal(response.Body.Bytes(), &payload) != nil {
		t.Fatalf("invalid response body=%s", response.Body.String())
	}
	readback, _ := payload["resumeAuthorizationReadback"].(map[string]any)
	binding, _ := readback["acceptanceBResumeExisting"].(map[string]any)
	publicAuthorization, _ := payload["resumeAuthorization"].(map[string]any)
	if readback["status"] != "consumed" || binding["approvalId"] != "acceptance-b-resume-existing-approval" ||
		binding["canonicalCloudTree"] != strings.Repeat("d", 40) || binding["authoritativeState"] != workspaceLaunchStageReady ||
		publicAuthorization["acceptanceBResumeExisting"] != nil {
		t.Fatalf("Acceptance B readback=%#v", readback)
	}
}

func TestAcceptanceBResumePrepareRouteRequiresOperatorAndCapability(t *testing.T) {
	configureProductionAcceptanceBEnvironment(t)
	store := newMemoryTableStore()
	seedTenantMember(t, store, "acct-alpha", "org-alpha", "usr-alpha", "alpha@example.com")
	command := workspaceLaunchUnitCommand()
	command.AccountID, command.OwnerUserID, command.Sub2APIUserID = "acct-alpha", "usr-alpha", 41
	command.WorkspaceID = "ws-acceptance-b-prepare-route"
	operation, err := newWorkspaceLaunchReconcileOperation(command)
	if err != nil {
		t.Fatal(err)
	}
	operation.Status, operation.Stage = "manual_review", "key"
	attempt := operation.Attempts[operation.Stage]
	attempt.Attempted, attempt.Status, attempt.IdempotencyKey = 1, "reserved", workspaceLaunchStageIdempotencyKey(operation, 1)
	operation.Attempts[operation.Stage] = attempt
	row, err := workspaceLaunchReconcileOperationRow(operation)
	if err != nil {
		t.Fatal(err)
	}
	mustStore(t, store.SaveRuntimeOperation(context.Background(), row))
	groupID := int64(7)
	client := &workspaceLaunchResumeRouteSub2API{
		testSub2APIClient: &testSub2APIClient{balance: 100_000_000, charges: map[string]int64{}},
		keys: []clients.Sub2APIWorkspaceKey{{
			ID: 19, UserID: 41, Name: workspaceReservedKeyName(command.WorkspaceID), Key: "prepare-route-key-secret", GroupID: &groupID, Status: "active",
		}},
	}
	server, err := NewPersistentServer(controlplane.NewService(fakeLedgerClient{}, &fakeFabricClient{}, client), store)
	if err != nil {
		t.Fatal(err)
	}
	operator := reservedOperatorSessionForTest(t, server)
	request := canonicalResumePrepareRequest(operation, workspaceLaunchStageReady)
	path := "/api/operator/workspace-launches/" + operation.ID + "/resume-approval-candidates"
	for name, configure := range map[string]func(*http.Request){
		"anonymous":          func(_ *http.Request) {},
		"missing capability": func(value *http.Request) { addAuth(value, operator); addResumePrepareHeaders(value, request) },
		"wrong capability": func(value *http.Request) {
			addAuth(value, operator)
			addResumePrepareHeaders(value, request)
			value.Header.Set(productionAcceptanceBCapability, "wrong")
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			configure(req)
			response := httptest.NewRecorder()
			server.ServeHTTP(response, req)
			if response.Code == http.StatusOK {
				t.Fatal("unauthorized prepare succeeded")
			}
		})
	}
	persistedBefore := stringValue(row["result"])
	req := httptest.NewRequest(http.MethodGet, path, nil)
	addAuth(req, operator)
	addResumePrepareHeaders(req, request)
	req.Header.Set(productionAcceptanceBCapability, "acceptance-b-capability")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, req)
	var approval productionAcceptanceBResumeExistingApproval
	persisted, found, readErr := store.GetRuntimeOperation(context.Background(), operation.ID)
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &approval) != nil || approval.OperationMode != "acceptance_b_resume_existing" ||
		approval.Authorization.OperationID != operation.ID || approval.Reconciliation.AuthoritativeStageState != workspaceLaunchStageReady ||
		readErr != nil || !found || stringValue(persisted["result"]) != persistedBefore || client.convergenceReads != 1 || client.createCalls != 0 {
		t.Fatalf("prepare route status=%d body=%s reads=%d creates=%d found=%v err=%v", response.Code, response.Body.String(), client.convergenceReads, client.createCalls, found, readErr)
	}
	if strings.Contains(response.Body.String(), "prepare-route-key-secret") || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("prepare route leaked owner fact or omitted no-store: headers=%v", response.Header())
	}
}

func addResumePrepareHeaders(request *http.Request, candidate productionAcceptanceBResumeExistingPrepareRequest) {
	request.Header.Set(productionAcceptanceBApprovalID, candidate.ApprovalID)
	request.Header.Set(productionAcceptanceBResumeAuthorizationID, candidate.AuthorizationID)
	request.Header.Set(productionAcceptanceBResumeReasonSHA256, candidate.ReasonSHA256)
	request.Header.Set(productionAcceptanceBResumeReleaseSHA, candidate.Release.CanonicalCloudSHA)
	request.Header.Set(productionAcceptanceBResumeReleaseTree, candidate.Release.CanonicalCloudTree)
	request.Header.Set(productionAcceptanceBResumeImageDigest, candidate.Release.DeployedCloudImageDigest)
}
