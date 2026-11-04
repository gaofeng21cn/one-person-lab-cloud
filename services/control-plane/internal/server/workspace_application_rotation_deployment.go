package server

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	contracts "opl-cloud/packages/contracts/go"
)

// Only an accepted Key rotation can still need a new child command. It keeps
// the selected application/revision/data and replaces that parent's Gateway
// binding; ordinary application installation and replacement are retired.
func (app *controlPlaneServer) createWorkspaceApplicationKeyRotationDeployment(ctx context.Context, workspaceID, parentID string, operation workspaceKeyRotationOperation) (workspaceApplicationDeploymentIntent, error) {
	workspace, found, err := app.tables.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	if !found {
		return workspaceApplicationDeploymentIntent{}, errWorkspaceApplicationWorkspaceGone
	}
	accountID := firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"]))
	parent, complete, err := app.workspaceKeyRotation(ctx, parentID, accountID, workspaceID, operation.RequestHash)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	if complete || parent.Phase != "runtime_bind" || parent.NewKeyID <= 0 || parent.NewKeyID != operation.NewKeyID ||
		parent.SecretRef == "" || parent.SecretRef != operation.SecretRef || parent.SecretVersion == "" || parent.SecretVersion != operation.SecretVersion {
		return workspaceApplicationDeploymentIntent{}, errWorkspaceKeyRotationState
	}
	operationID := workspaceApplicationDeploymentOperationID(workspaceID, "gateway-rotation:"+parentID)
	// A lost child-claim response may leave the original child persisted but
	// not yet attached to the parent. Return that exact frozen command.
	if row, found, err := app.tables.GetRuntimeOperation(ctx, operationID); err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	} else if found {
		prior, err := decodeWorkspaceApplicationDeploymentIntent(row)
		if err != nil || prior.Version != 2 || prior.ClientConfigurationDigest != "" || !workspaceApplicationRotationParentMatches(prior, workspaceKeyRotationRow(parentID, accountID, workspaceID, "started", parent)) {
			return workspaceApplicationDeploymentIntent{}, errWorkspaceApplicationIntentConflict
		}
		return prior, nil
	}
	current, found, err := app.currentWorkspaceApplicationDeployment(ctx, workspace)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	if !found {
		return workspaceApplicationDeploymentIntent{}, errWorkspaceKeyRotationState
	}
	input, err := app.workspaceApplicationRuntimeInput(ctx, current)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	if !contracts.WorkspaceApplicationCredentialKind(input.Revision, contracts.WorkspaceApplicationCredentialGatewayKey) {
		return workspaceApplicationDeploymentIntent{}, errWorkspaceKeyRotationConflict
	}
	bindings := append([]contracts.WorkspaceApplicationRuntimeSecretBinding(nil), current.SecretBindings...)
	bound := false
	for index := range bindings {
		if bindings[index].Name == "gateway" && bindings[index].Key == "opl_gateway_api_key" {
			bindings[index].SecretRef, bindings[index].Version = parent.SecretRef, parent.SecretVersion
			bound = true
		}
	}
	if !bound {
		return workspaceApplicationDeploymentIntent{}, errWorkspaceKeyRotationConflict
	}
	configuration := current.Configuration
	configuration.CredentialVersion = stableID("workspace-application-gateway-rotation", workspaceID, parentID)
	configuration.CredentialSourceRuntimeOperationID = ""
	dataBindingID, dataLayout, dataSource := current.DataBindingID, current.DataLayout, current.DataSourceRuntimeOperationID
	if current.Version == 1 {
		dataBindingID = "application-data-" + stableID(workspaceID, current.ApplicationID)
		dataLayout, dataSource = "legacy_application", current.OperationID+":runtime"
	}
	// Retained generations must agree on this application's durable data
	// namespace. A Key change cannot choose a new namespace or hide a conflict.
	owned, err := app.ownedWorkspaceApplicationDeployments(ctx, workspace)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	dataBound := false
	for _, prior := range owned {
		if prior.Version != 2 || prior.ApplicationID != current.ApplicationID {
			continue
		}
		if dataBound && (dataBindingID != prior.DataBindingID || dataLayout != prior.DataLayout || dataSource != prior.DataSourceRuntimeOperationID) {
			return workspaceApplicationDeploymentIntent{}, errors.New("workspace_application_data_binding_conflict")
		}
		dataBindingID, dataLayout, dataSource = prior.DataBindingID, prior.DataLayout, prior.DataSourceRuntimeOperationID
		dataBound = true
	}
	if !dataBound {
		launch, found, err := app.canonicalWorkspaceLaunch(ctx, workspace, workspaceLaunchResourceProjectionMismatchFields, nil)
		if err != nil {
			return workspaceApplicationDeploymentIntent{}, err
		}
		if found && launch.provisioningMode() == contracts.WorkspaceProvisioningFull {
			if launch.stringFact("runtimeId") == "" || launch.stringFact("runtimeBindingRef") == "" {
				return workspaceApplicationDeploymentIntent{}, errWorkspaceApplicationBindingUnknown
			}
			dataLayout, dataSource = "legacy_opl", launch.stringFact("runtimeBindingRef")
		}
	}
	digest, err := contracts.WorkspaceApplicationConfigurationDigest(configuration, bindings, dataBindingID)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	if err := contracts.ValidateWorkspaceApplicationRuntimeConfiguration(contracts.WorkspaceApplicationRuntimeInput{
		SchemaVersion: 2, Revision: input.Revision, Configuration: configuration, SecretBindings: bindings,
		DataBindingID: dataBindingID, DataLayout: dataLayout, DataSourceRuntimeOperationID: dataSource, ConfigurationDigest: digest,
	}); err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	intent := workspaceApplicationDeploymentIntent{
		SchemaVersion: workspaceApplicationDeploymentSchemaVersion, Version: 2,
		OperationID: operationID, AccountID: accountID, WorkspaceID: workspaceID,
		ComputeID: current.ComputeID, StorageID: current.StorageID, AttachmentID: current.AttachmentID,
		ApplicationID: current.ApplicationID, TargetRevision: current.TargetRevision, RevisionDigest: current.RevisionDigest,
		ConfigurationDigest: digest, DataBindingIDs: []string{dataBindingID},
		ExpectedWorkspaceVersion: int64(numberField(workspace, "applicationBindingVersion", 0)), CurrentBinding: stringValue(workspace["applicationBinding"]),
		Configuration: configuration, SecretBindings: bindings, DataBindingID: dataBindingID, DataLayout: dataLayout, DataSourceRuntimeOperationID: dataSource,
		PreviousDeploymentID: current.OperationID, WorkspaceAPIKeyID: parent.NewKeyID, OriginOperationID: parentID,
		Phase: workspaceApplicationDeploymentIntentPhase, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	for _, binding := range bindings {
		intent.SecretBindingVersions = append(intent.SecretBindingVersions, binding.Name+":"+binding.SecretRef+":"+binding.Version+":"+binding.Key)
	}
	intent.RequestHash = workspaceApplicationDeploymentRequestHash(intent)
	encoded, err := json.Marshal(intent)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	row := map[string]any{
		"id": operationID, "operationId": operationID, "accountId": accountID,
		"workspaceId": workspaceID, "resourceId": workspaceID, "resourceKind": "workspace",
		"action": workspaceApplicationDeploymentAction, "status": "pending", "result": string(encoded), "createdAt": intent.CreatedAt,
	}
	if err := app.tables.ClaimWorkspaceApplicationDeploymentIntent(ctx, row); err != nil {
		if !errors.Is(err, errWorkspaceApplicationIntentConflict) {
			return workspaceApplicationDeploymentIntent{}, err
		}
		existing, found, readErr := app.tables.GetRuntimeOperation(ctx, operationID)
		if readErr != nil {
			return workspaceApplicationDeploymentIntent{}, readErr
		}
		if !found {
			return workspaceApplicationDeploymentIntent{}, err
		}
		persisted, decodeErr := decodeWorkspaceApplicationDeploymentIntent(existing)
		if decodeErr != nil || persisted.RequestHash != intent.RequestHash {
			return workspaceApplicationDeploymentIntent{}, err
		}
		return persisted, nil
	}
	return intent, nil
}
