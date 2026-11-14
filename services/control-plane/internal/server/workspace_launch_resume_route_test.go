package server

import (
	"context"
	"errors"

	"opl-cloud/services/control-plane/internal/clients"
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
	reads    int
	observes int
	ensures  int
	ready    bool
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
			SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: string(workspaceLaunchStagePending), Reason: "runtime_image_revision_required",
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
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: string(workspaceLaunchStageReady), Reason: "none",
		Binding: input.Binding, Resources: resources,
	}, nil
}

func (f *workspaceLaunchRuntimeImageRevisionResumeFabric) EnsureWorkspaceLaunchStage(_ context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.ensures = append(f.ensures, input)
	return clients.WorkspaceLaunchStageResult{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: string(workspaceLaunchStagePending), Reason: "provider_provisioning",
		Binding: input.Binding, Resources: input.Resources,
	}, nil
}

func (*workspaceLaunchStorageResumeFabric) PreflightWorkspaceLaunch(context.Context, clients.WorkspaceLaunchPreflightInput) (clients.WorkspaceLaunchPreflight, error) {
	return clients.WorkspaceLaunchPreflight{}, errors.New("unexpected storage preflight")
}

func (f *workspaceLaunchStorageResumeFabric) ReadWorkspaceLaunchStage(_ context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.reads++
	return f.workspaceLaunchStageResult(input)
}

func (f *workspaceLaunchStorageResumeFabric) ObserveWorkspaceLaunchStage(_ context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.observes++
	return f.workspaceLaunchStageResult(input)
}

func (f *workspaceLaunchStorageResumeFabric) workspaceLaunchStageResult(input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	if !f.ready {
		return clients.WorkspaceLaunchStageResult{
			SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: string(workspaceLaunchStageAbsent), Reason: "failed_no_resource",
			Binding: input.Binding, Resources: input.Resources,
		}, nil
	}
	resources := input.Resources
	resources.StorageID, resources.StorageBindingRef = "storage-route-ready", input.Binding.FabricOperationID
	return clients.WorkspaceLaunchStageResult{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: string(workspaceLaunchStageReady), Reason: "none",
		Binding: input.Binding, Resources: resources,
	}, nil
}

func (f *workspaceLaunchStorageResumeFabric) EnsureWorkspaceLaunchStage(_ context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.ensures++
	f.ready = true
	resources := input.Resources
	resources.StorageID, resources.StorageBindingRef = "storage-route-ready", input.Binding.FabricOperationID
	return clients.WorkspaceLaunchStageResult{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: string(workspaceLaunchStageReady), Reason: "none",
		Binding: input.Binding, Resources: resources,
	}, nil
}

func (*workspaceLaunchComputeResumeFabric) PreflightWorkspaceLaunch(context.Context, clients.WorkspaceLaunchPreflightInput) (clients.WorkspaceLaunchPreflight, error) {
	return clients.WorkspaceLaunchPreflight{}, errors.New("unexpected compute preflight")
}

func (f *workspaceLaunchComputeResumeFabric) ReadWorkspaceLaunchStage(_ context.Context, input clients.WorkspaceLaunchStageInput) (clients.WorkspaceLaunchStageResult, error) {
	f.reads++
	return clients.WorkspaceLaunchStageResult{
		SchemaVersion: clients.WorkspaceLaunchFabricSchemaVersion, State: string(workspaceLaunchStagePending), Reason: "provider_provisioning",
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
