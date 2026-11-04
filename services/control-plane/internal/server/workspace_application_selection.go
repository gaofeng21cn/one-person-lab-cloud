package server

import (
	"context"
	"errors"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/domain/application"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

// Reads are allowed during suspension/deletion: they identify owned resources,
// and never authorize a new provider mutation by themselves.
func workspaceApplicationOwnedResourcesMatch(workspace map[string]any, intent workspaceApplicationDeploymentIntent) bool {
	return stringValue(workspace["id"]) == intent.WorkspaceID &&
		firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])) == intent.AccountID &&
		firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])) == intent.ComputeID &&
		stringValue(workspace["storageId"]) == intent.StorageID &&
		firstNonEmpty(stringValue(workspace["currentAttachmentId"]), stringValue(workspace["attachmentId"])) == intent.AttachmentID
}

func workspaceApplicationEntitlementOpen(workspace map[string]any, now time.Time) bool {
	if firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"])) != "running" {
		return false
	}
	if workspace["resourceBillingEnabled"] == false {
		return true
	}
	paidThrough, err := time.Parse(time.RFC3339Nano, stringValue(workspace["paidThrough"]))
	return err == nil && now.Before(paidThrough)
}

func (app *controlPlaneServer) currentWorkspaceApplicationDeployment(ctx context.Context, workspace map[string]any) (workspaceApplicationDeploymentIntent, bool, error) {
	id := stringValue(workspace["currentApplicationDeploymentId"])
	if id == "" {
		binding := stringValue(workspace["applicationBinding"])
		if binding == "" || binding == "empty" || binding == provisioning.ApplicationBindingOPLApp {
			return workspaceApplicationDeploymentIntent{}, false, nil
		}
		return workspaceApplicationDeploymentIntent{}, false, errWorkspaceApplicationBindingUnknown
	}
	row, found, err := app.tables.GetRuntimeOperation(ctx, id)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, false, err
	}
	if !found {
		return workspaceApplicationDeploymentIntent{}, false, errWorkspaceApplicationBindingUnknown
	}
	intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
	if err != nil {
		return intent, false, err
	}
	if !workspaceApplicationOwnedResourcesMatch(workspace, intent) || intent.ApplicationID+"@"+intent.TargetRevision != stringValue(workspace["applicationBinding"]) || intent.ExpectedWorkspaceVersion+1 != int64(numberField(workspace, "applicationBindingVersion", 0)) || intent.ActivationAt == "" {
		return intent, false, errWorkspaceApplicationBindingUnknown
	}
	return intent, true, nil
}

func (app *controlPlaneServer) ownedWorkspaceApplicationDeployments(ctx context.Context, workspace map[string]any) ([]workspaceApplicationDeploymentIntent, error) {
	rows, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{WorkspaceID: stringValue(workspace["id"]), Action: workspaceApplicationDeploymentAction})
	if err != nil {
		return nil, err
	}
	intents := make([]workspaceApplicationDeploymentIntent, 0, len(rows))
	for _, row := range rows {
		intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
		if err != nil {
			return nil, err
		}
		if !workspaceApplicationOwnedResourcesMatch(workspace, intent) {
			return nil, errWorkspaceApplicationResourcesUnready
		}
		intents = append(intents, intent)
	}
	return intents, nil
}

func (app *controlPlaneServer) workspaceApplicationRuntimeInput(ctx context.Context, intent workspaceApplicationDeploymentIntent) (clients.WorkspaceApplicationRuntimeInput, error) {
	row, found, err := app.tables.AdmittedApplicationRevision(ctx, intent.ApplicationID, intent.TargetRevision)
	if err != nil {
		return clients.WorkspaceApplicationRuntimeInput{}, err
	}
	if !found || stringValue(row["digest"]) != intent.RevisionDigest {
		return clients.WorkspaceApplicationRuntimeInput{}, errWorkspaceApplicationRevisionGone
	}
	revision, ok := decodeApplicationRevisionPayload(stringValue(row["payload"]))
	if !ok {
		return clients.WorkspaceApplicationRuntimeInput{}, errApplicationRevisionPayloadInvalid
	}
	digest, err := application.RevisionDigest(revision)
	if err != nil || digest != intent.RevisionDigest {
		return clients.WorkspaceApplicationRuntimeInput{}, errApplicationRevisionPayloadInvalid
	}
	workspace, found, err := app.tables.GetWorkspace(ctx, intent.WorkspaceID)
	if err != nil {
		return clients.WorkspaceApplicationRuntimeInput{}, err
	}
	if !found || !workspaceApplicationOwnedResourcesMatch(workspace, intent) {
		return clients.WorkspaceApplicationRuntimeInput{}, errWorkspaceApplicationResourcesUnready
	}
	launch, found, err := app.canonicalWorkspaceLaunch(ctx, workspace, func(launch workspaceLaunchReconcileOperation, _ map[string]any) []string {
		if launch.stringFact("accountId") != intent.AccountID || launch.stringFact("workspaceId") != intent.WorkspaceID || launch.stringFact("computeAllocationId") != intent.ComputeID || launch.stringFact("storageId") != intent.StorageID || launch.stringFact("attachmentId") != intent.AttachmentID || launch.stringFact("attachmentBindingRef") == "" {
			return []string{"application_resource_binding"}
		}
		return nil
	}, nil)
	if err != nil {
		return clients.WorkspaceApplicationRuntimeInput{}, err
	}
	if !found {
		return clients.WorkspaceApplicationRuntimeInput{}, errWorkspaceApplicationResourcesUnready
	}
	input := clients.WorkspaceApplicationRuntimeInput{AccountID: intent.AccountID, WorkspaceID: intent.WorkspaceID, ComputeID: intent.ComputeID, VolumeID: intent.StorageID, AttachmentID: intent.AttachmentID, AttachmentOperationID: launch.stringFact("attachmentBindingRef"), RuntimeOperationID: intent.OperationID + ":runtime", Revision: revision, ConfigurationDigest: intent.ConfigurationDigest}
	if intent.Version == 2 {
		input.SchemaVersion, input.Configuration, input.SecretBindings, input.DataBindingID, input.DataLayout, input.DataSourceRuntimeOperationID = 2, intent.Configuration, intent.SecretBindings, intent.DataBindingID, intent.DataLayout, intent.DataSourceRuntimeOperationID
		actual, err := contracts.WorkspaceApplicationConfigurationDigest(input.Configuration, input.SecretBindings, input.DataBindingID)
		if err != nil || actual != input.ConfigurationDigest {
			return input, errors.New("workspace_application_configuration_mismatch")
		}
	}
	return input, nil
}
