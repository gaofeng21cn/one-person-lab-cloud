package server

import (
	"context"
	"errors"
	"sort"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type workspaceApplicationSecretCleanup struct {
	Secrets               []workspaceApplicationSecretRetirement `json:"secrets"`
	RetainedGatewayKeyIDs []int64                                `json:"retainedGatewayKeyIds,omitempty"`
}

type workspaceApplicationSecretRetirement struct {
	SecretRef string `json:"secretRef"`
	Ownership string `json:"ownership"`
	State     string `json:"state"`
}

func (app *controlPlaneServer) workspaceApplicationSecretInventory(ctx context.Context, workspace map[string]any) (*workspaceApplicationSecretCleanup, error) {
	intents, err := app.ownedWorkspaceApplicationDeployments(ctx, workspace)
	if err != nil {
		return nil, err
	}
	requests, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{WorkspaceID: stringValue(workspace["id"]), Action: workspaceDefaultApplicationAction})
	if err != nil {
		return nil, err
	}
	secrets := map[string]workspaceApplicationSecretRetirement{}
	keys := map[int64]bool{}
	for _, intent := range intents {
		if intent.WorkspaceAPIKeyID > 0 {
			keys[intent.WorkspaceAPIKeyID] = true
		}
		for _, binding := range intent.SecretBindings {
			owned := binding.Name == "gateway" && binding.Key == "opl_gateway_api_key" && intent.WorkspaceAPIKeyID > 0
			if existing, ok := secrets[binding.SecretRef]; ok && existing.Ownership == "workspace_gateway" {
				continue
			}
			secret := workspaceApplicationSecretRetirement{SecretRef: binding.SecretRef, Ownership: "external", State: "retained"}
			if owned {
				secret.Ownership, secret.State = "workspace_gateway", "pending"
			}
			secrets[binding.SecretRef] = secret
		}
	}
	for _, row := range requests {
		request, err := decodeWorkspaceDefaultApplication(row)
		if err != nil || request.AccountID != firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])) || request.WorkspaceID != stringValue(workspace["id"]) {
			return nil, errors.New("workspace_application_secret_inventory_invalid")
		}
		if request.WorkspaceAPIKeyID > 0 {
			keys[request.WorkspaceAPIKeyID] = true
		}
		ref := contracts.WorkspaceGatewaySecretRef(request.WorkspaceID)
		if request.GatewaySecret != nil {
			if request.GatewaySecret.SecretRef != ref {
				return nil, errors.New("workspace_application_secret_inventory_invalid")
			}
		}
		secrets[ref] = workspaceApplicationSecretRetirement{SecretRef: ref, Ownership: "workspace_gateway", State: "pending"}
	}
	if len(intents) > 0 || len(requests) > 0 {
		if current, ok := positiveIntegerField(workspace, "workspaceApiKeyId"); ok {
			keys[current] = true
		}
	}
	result := &workspaceApplicationSecretCleanup{Secrets: make([]workspaceApplicationSecretRetirement, 0, len(secrets))}
	for _, secret := range secrets {
		result.Secrets = append(result.Secrets, secret)
	}
	for key := range keys {
		result.RetainedGatewayKeyIDs = append(result.RetainedGatewayKeyIDs, key)
	}
	sort.Slice(result.Secrets, func(i, j int) bool { return result.Secrets[i].SecretRef < result.Secrets[j].SecretRef })
	sort.Slice(result.RetainedGatewayKeyIDs, func(i, j int) bool { return result.RetainedGatewayKeyIDs[i] < result.RetainedGatewayKeyIDs[j] })
	return result, nil
}

func validWorkspaceApplicationSecretCleanup(cleanup *workspaceApplicationSecretCleanup) bool {
	if cleanup == nil {
		return true
	}
	previous := ""
	for _, secret := range cleanup.Secrets {
		if secret.SecretRef <= previous {
			return false
		}
		if secret.Ownership == "workspace_gateway" {
			if secret.State != "pending" && secret.State != "absent" {
				return false
			}
		} else if secret.Ownership != "external" || secret.State != "retained" {
			return false
		}
		previous = secret.SecretRef
	}
	var previousKey int64
	for _, key := range cleanup.RetainedGatewayKeyIDs {
		if key <= previousKey {
			return false
		}
		previousKey = key
	}
	return true
}

func workspaceApplicationSecretTargetsMatch(current, desired *workspaceApplicationSecretCleanup) bool {
	if current == nil {
		return true
	}
	if desired == nil || len(current.Secrets) != len(desired.Secrets) || len(current.RetainedGatewayKeyIDs) != len(desired.RetainedGatewayKeyIDs) {
		return false
	}
	for i, secret := range current.Secrets {
		if secret.SecretRef != desired.Secrets[i].SecretRef || secret.Ownership != desired.Secrets[i].Ownership || secret.State == "absent" && desired.Secrets[i].State != "absent" {
			return false
		}
	}
	for i, key := range current.RetainedGatewayKeyIDs {
		if key != desired.RetainedGatewayKeyIDs[i] {
			return false
		}
	}
	return true
}

func workspaceApplicationSecretCleanupComplete(cleanup *workspaceApplicationSecretCleanup) bool {
	if cleanup == nil {
		return false
	}
	for _, secret := range cleanup.Secrets {
		if secret.Ownership == "workspace_gateway" && secret.State != "absent" {
			return false
		}
	}
	return true
}

func (app *controlPlaneServer) convergeWorkspaceApplicationSecretCleanup(ctx context.Context, service *controlplane.Service, operation workspaceDeleteOperation, persist func() error) error {
	var failures []error
	for i, secret := range operation.ApplicationSecrets.Secrets {
		if secret.Ownership != "workspace_gateway" || secret.State == "absent" {
			continue
		}
		input := clients.WorkspaceApplicationGatewaySecretCleanupInput{AccountID: operation.AccountID, WorkspaceID: operation.WorkspaceID, SecretRef: secret.SecretRef}
		if err := service.RemoveWorkspaceApplicationGatewaySecret(ctx, input, operation.OperationID+":application-secret:"+secret.SecretRef); err != nil {
			failures = append(failures, err)
			continue
		}
		operation.ApplicationSecrets.Secrets[i].State = "absent"
		if err := persist(); err != nil {
			return err
		}
	}
	return errors.Join(failures...)
}
