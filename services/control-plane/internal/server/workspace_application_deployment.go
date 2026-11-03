package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/domain/application"
	"opl-cloud/services/control-plane/internal/domain/provisioning"
)

const (
	workspaceApplicationDeploymentAction        = "workspace.application.deploy"
	workspaceApplicationDeploymentSchemaVersion = 1
	workspaceApplicationDeploymentIntentPhase   = "intent"
)

var (
	errWorkspaceApplicationConfigurationInvalid = errors.New("workspace_application_configuration_invalid")
	errWorkspaceApplicationIntentConflict       = errors.New("workspace_application_deployment_intent_conflict")
	errWorkspaceApplicationResourcesUnready     = errors.New("workspace_application_resources_unready")
	errWorkspaceApplicationBindingUnknown       = errors.New("workspace_application_binding_unknown")
	errWorkspaceApplicationWorkspaceGone        = errors.New("workspace_application_workspace_not_found")
)

// workspaceApplicationDeploymentIntent is the durable, immutable intent that
// binds one admitted revision to one Workspace at one binding version. The
// Fabric runtime creation and the atomic activation consume it in later
// slices; neither can bypass or rewrite it.
type workspaceApplicationDeploymentIntent struct {
	SchemaVersion                int                                                   `json:"schemaVersion"`
	Version                      int                                                   `json:"version"`
	RequestHash                  string                                                `json:"requestHash"`
	OperationID                  string                                                `json:"operationId"`
	AccountID                    string                                                `json:"accountId"`
	WorkspaceID                  string                                                `json:"workspaceId"`
	ComputeID                    string                                                `json:"computeId"`
	StorageID                    string                                                `json:"storageId"`
	AttachmentID                 string                                                `json:"attachmentId"`
	ApplicationID                string                                                `json:"applicationId"`
	TargetRevision               string                                                `json:"targetRevision"`
	RevisionDigest               string                                                `json:"revisionDigest"`
	ConfigurationDigest          string                                                `json:"configurationDigest"`
	ClientConfigurationDigest    string                                                `json:"clientConfigurationDigest,omitempty"`
	SecretBindingVersions        []string                                              `json:"secretBindingVersions,omitempty"`
	DataBindingIDs               []string                                              `json:"dataBindingIds,omitempty"`
	ExpectedWorkspaceVersion     int64                                                 `json:"expectedWorkspaceVersion"`
	CurrentBinding               string                                                `json:"currentBinding"`
	Configuration                contracts.WorkspaceApplicationRuntimeConfiguration    `json:"configuration,omitempty"`
	SecretBindings               []contracts.WorkspaceApplicationRuntimeSecretBinding  `json:"secretBindings,omitempty"`
	DataSourceRuntimeOperationID string                                                `json:"dataSourceRuntimeOperationId,omitempty"`
	DataLayout                   string                                                `json:"dataLayout,omitempty"`
	DataBindingID                string                                                `json:"dataBindingId,omitempty"`
	PreviousDeploymentID         string                                                `json:"previousDeploymentId,omitempty"`
	LegacyPredecessor            *contracts.WorkspaceRuntimePowerInput                 `json:"legacyPredecessor,omitempty"`
	PredecessorObservation       *contracts.WorkspaceApplicationRuntimeLifecycleResult `json:"predecessorObservation,omitempty"`
	WorkspaceAPIKeyID            int64                                                 `json:"workspaceApiKeyId,omitempty"`
	OriginOperationID            string                                                `json:"originOperationId,omitempty"`

	Phase              string                                            `json:"phase"`
	FailurePhase       string                                            `json:"failurePhase,omitempty"`
	CreatedAt          string                                            `json:"createdAt"`
	RuntimeObservation *contracts.WorkspaceApplicationRuntimeObservation `json:"runtimeObservation,omitempty"`
	ActivationAt       string                                            `json:"activationAt,omitempty"`
	ReceiptID          string                                            `json:"receiptId,omitempty"`
	LastError          string                                            `json:"lastError,omitempty"`
}

func workspaceApplicationDeploymentOperationID(workspaceID, key string) string {
	return "workspace-application-deploy-" + stableID(workspaceID, key)
}

func workspaceApplicationDeploymentRequestHash(intent workspaceApplicationDeploymentIntent) string {
	if intent.Version == 2 {
		// Progress and observations are mutable; the reserved command is not.
		intent.RequestHash, intent.Phase, intent.CreatedAt, intent.ActivationAt, intent.ReceiptID, intent.LastError = "", "", "", "", "", ""
		intent.RuntimeObservation, intent.PredecessorObservation = nil, nil
		intent.FailurePhase = ""
		payload, err := json.Marshal(intent)
		if err != nil {
			return ""
		}
		return fmt.Sprintf("%x", sha256.Sum256(payload))
	}

	payload, err := json.Marshal(struct {
		WorkspaceID              string   `json:"workspaceId"`
		ComputeID                string   `json:"computeId"`
		StorageID                string   `json:"storageId"`
		AttachmentID             string   `json:"attachmentId"`
		ApplicationID            string   `json:"applicationId"`
		TargetRevision           string   `json:"targetRevision"`
		RevisionDigest           string   `json:"revisionDigest"`
		ConfigurationDigest      string   `json:"configurationDigest"`
		SecretBindingVersions    []string `json:"secretBindingVersions,omitempty"`
		DataBindingIDs           []string `json:"dataBindingIds,omitempty"`
		ExpectedWorkspaceVersion int64    `json:"expectedWorkspaceVersion"`
		CurrentBinding           string   `json:"currentBinding"`
	}{
		WorkspaceID: intent.WorkspaceID, ComputeID: intent.ComputeID, StorageID: intent.StorageID,
		AttachmentID: intent.AttachmentID, ApplicationID: intent.ApplicationID, TargetRevision: intent.TargetRevision,
		RevisionDigest: intent.RevisionDigest, ConfigurationDigest: intent.ConfigurationDigest,
		SecretBindingVersions: intent.SecretBindingVersions, DataBindingIDs: intent.DataBindingIDs,
		ExpectedWorkspaceVersion: intent.ExpectedWorkspaceVersion, CurrentBinding: intent.CurrentBinding,
	})
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%x", sha256.Sum256(payload))
}

func decodeWorkspaceApplicationDeploymentIntent(row map[string]any) (workspaceApplicationDeploymentIntent, error) {
	var intent workspaceApplicationDeploymentIntent
	if stringValue(row["action"]) != workspaceApplicationDeploymentAction ||
		json.Unmarshal([]byte(stringValue(row["result"])), &intent) != nil ||
		intent.SchemaVersion != workspaceApplicationDeploymentSchemaVersion || (intent.Version != 1 && intent.Version != 2) ||
		intent.OperationID == "" || intent.OperationID != stringValue(row["id"]) ||
		intent.OperationID != stringValue(row["operationId"]) || intent.AccountID == "" ||
		intent.WorkspaceID == "" || intent.WorkspaceID != stringValue(row["workspaceId"]) ||
		intent.WorkspaceID != stringValue(row["resourceId"]) || intent.ApplicationID == "" ||
		intent.TargetRevision == "" || intent.RevisionDigest == "" || intent.ConfigurationDigest == "" ||
		intent.ExpectedWorkspaceVersion < 0 || intent.Phase == "" || intent.RequestHash == "" ||
		intent.RequestHash != workspaceApplicationDeploymentRequestHash(intent) {
		return workspaceApplicationDeploymentIntent{}, errors.New("workspace_application_deployment_intent_invalid")
	}
	return intent, nil
}

// createWorkspaceApplicationDeploymentIntent runs the Control Plane-owned
// prechecks and persists the immutable deployment intent. The intent captures
// the Workspace's current binding and binding version; the later activation
// must commit against exactly this reserved version.
func (app *controlPlaneServer) createWorkspaceApplicationDeploymentIntent(
	ctx context.Context,
	workspaceID, mutationKey, applicationID, targetRevision string,
	configuration contracts.WorkspaceApplicationRuntimeConfiguration, secretBindings []contracts.WorkspaceApplicationRuntimeSecretBinding, workspaceAPIKeyID int64, originOperationID, clientConfigurationDigest string,
) (workspaceApplicationDeploymentIntent, error) {
	workspace, found, err := app.tables.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	if !found {
		return workspaceApplicationDeploymentIntent{}, errWorkspaceApplicationWorkspaceGone
	}
	if firstNonEmpty(stringValue(workspace["state"]), stringValue(workspace["status"])) != "running" ||
		stringValue(workspace["computeAllocationId"]) == "" && stringValue(workspace["currentComputeAllocationId"]) == "" ||
		stringValue(workspace["storageId"]) == "" ||
		stringValue(workspace["attachmentId"]) == "" && stringValue(workspace["currentAttachmentId"]) == "" {
		return workspaceApplicationDeploymentIntent{}, errWorkspaceApplicationResourcesUnready
	}
	currentBinding := stringValue(workspace["applicationBinding"])
	if currentBinding == "" {
		return workspaceApplicationDeploymentIntent{}, errWorkspaceApplicationBindingUnknown
	}
	bindingVersion := int64(numberField(workspace, "applicationBindingVersion", 0))
	existing, admitted, err := app.tables.AdmittedApplicationRevision(ctx, applicationID, targetRevision)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	if !admitted {
		return workspaceApplicationDeploymentIntent{}, application.ErrRevisionNotAdmitted
	}
	revision, ok := decodeApplicationRevisionPayload(stringValue(existing["payload"]))
	if !ok {
		return workspaceApplicationDeploymentIntent{}, errApplicationRevisionPayloadInvalid
	}
	revisionDigest := stringValue(existing["digest"])

	operationID := workspaceApplicationDeploymentOperationID(workspaceID, mutationKey)
	// A replay compares the original command, including its original data
	// binding; the selected application may already have changed meanwhile.
	if row, found, err := app.tables.GetRuntimeOperation(ctx, operationID); err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	} else if found {
		prior, err := decodeWorkspaceApplicationDeploymentIntent(row)
		if err != nil || prior.Version != 2 || prior.ApplicationID != applicationID || prior.TargetRevision != targetRevision || prior.OriginOperationID != originOperationID || prior.ClientConfigurationDigest != clientConfigurationDigest {
			return workspaceApplicationDeploymentIntent{}, errWorkspaceApplicationIntentConflict
		}
		if clientConfigurationDigest == "" {
			if contracts.WorkspaceApplicationRequiresPlatformCredentials(revision) && configuration.CredentialVersion == "" && configuration.CredentialSourceRuntimeOperationID == "" {
				configuration.CredentialVersion = prior.Configuration.CredentialVersion
				configuration.CredentialSourceRuntimeOperationID = prior.Configuration.CredentialSourceRuntimeOperationID
			}
			configurationDigest, digestErr := contracts.WorkspaceApplicationConfigurationDigest(configuration, secretBindings, prior.DataBindingID)
			if digestErr != nil || prior.ConfigurationDigest != configurationDigest || prior.WorkspaceAPIKeyID != workspaceAPIKeyID {
				return workspaceApplicationDeploymentIntent{}, errWorkspaceApplicationIntentConflict
			}
		}
		return prior, nil
	}
	previous, selected, err := app.currentWorkspaceApplicationDeployment(ctx, workspace)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	dataBindingID, dataLayout, dataSource := "application-data-"+stableID(workspaceID, applicationID), "", ""
	previousID := ""
	if selected {
		previousID = previous.OperationID
		if previous.ApplicationID == applicationID {
			if previous.Version == 2 {
				dataBindingID, dataLayout, dataSource = previous.DataBindingID, previous.DataLayout, previous.DataSourceRuntimeOperationID
			} else {
				dataLayout, dataSource = "legacy_application", previous.OperationID+":runtime"
			}
		}
	}
	// Reinstalling a previously selected application uses the same durable
	// data namespace, even if a different application is selected now.
	owned, err := app.ownedWorkspaceApplicationDeployments(ctx, workspace)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	bound := false
	for _, prior := range owned {
		if prior.Version != 2 || prior.ApplicationID != applicationID {
			continue
		}
		if bound && (dataBindingID != prior.DataBindingID || dataLayout != prior.DataLayout || dataSource != prior.DataSourceRuntimeOperationID) {
			return workspaceApplicationDeploymentIntent{}, errors.New("workspace_application_data_binding_conflict")
		}
		dataBindingID, dataLayout, dataSource = prior.DataBindingID, prior.DataLayout, prior.DataSourceRuntimeOperationID
		bound = true
	}
	if !bound {
		historicalLayout, historicalSource, err := app.workspaceApplicationHistoricalDataBinding(ctx, workspace, revision, previous, selected, owned)
		if err != nil {
			return workspaceApplicationDeploymentIntent{}, err
		}
		if historicalLayout != "" {
			dataLayout, dataSource = historicalLayout, historicalSource
		}
	}
	if currentBinding == provisioning.ApplicationBindingOPLApp && contracts.WorkspaceApplicationCredentialKind(revision, contracts.WorkspaceApplicationCredentialGatewayKey) {
		dataLayout = "legacy_opl"
	}
	if contracts.WorkspaceApplicationRequiresPlatformCredentials(revision) {
		configuration, err = app.workspaceApplicationCredentialConfiguration(ctx, workspace, revision, operationID, configuration)
		if err != nil {
			return workspaceApplicationDeploymentIntent{}, err
		}
	}
	configurationDigest, err := contracts.WorkspaceApplicationConfigurationDigest(configuration, secretBindings, dataBindingID)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, fmt.Errorf("%w: %v", errWorkspaceApplicationConfigurationInvalid, err)
	}
	var secretBindingVersions []string
	for _, binding := range secretBindings {
		secretBindingVersions = append(secretBindingVersions, binding.Name+":"+binding.SecretRef+":"+binding.Version+":"+binding.Key)
	}
	dataBindingIDs := []string{dataBindingID}
	var legacy *contracts.WorkspaceRuntimePowerInput
	if currentBinding == provisioning.ApplicationBindingOPLApp {
		launch, found, err := app.canonicalWorkspaceLaunch(ctx, workspace, workspaceLaunchResourceProjectionMismatchFields, nil)
		if err != nil || !found || launch.stringFact("runtimeId") == "" || launch.stringFact("runtimeBindingRef") == "" {
			return workspaceApplicationDeploymentIntent{}, errWorkspaceApplicationBindingUnknown
		}
		if dataLayout == "legacy_opl" {
			dataSource = launch.stringFact("runtimeBindingRef")
		}
		legacy = &contracts.WorkspaceRuntimePowerInput{SchemaVersion: 1, AccountID: launch.stringFact("accountId"), WorkspaceID: workspaceID, RuntimeID: launch.stringFact("runtimeId"), RuntimeOperationID: launch.stringFact("runtimeBindingRef"), PaidThrough: stringValue(workspace["paidThrough"])}
	}

	if err := contracts.ValidateWorkspaceApplicationRuntimeConfiguration(contracts.WorkspaceApplicationRuntimeInput{SchemaVersion: 2, Revision: revision, Configuration: configuration, SecretBindings: secretBindings, DataBindingID: dataBindingID, DataLayout: dataLayout, DataSourceRuntimeOperationID: dataSource, ConfigurationDigest: configurationDigest}); err != nil {
		return workspaceApplicationDeploymentIntent{}, fmt.Errorf("%w: %v", errWorkspaceApplicationConfigurationInvalid, err)
	}

	deployment := contracts.WorkspaceApplicationDeployment{
		SchemaVersion: 1, OperationID: operationID, WorkspaceID: workspaceID,
		ApplicationID: applicationID, TargetRevision: targetRevision,
		ConfigurationDigest:   configurationDigest,
		SecretBindingVersions: secretBindingVersions, DataBindingIDs: dataBindingIDs,
		ExpectedWorkspaceVersion: bindingVersion,
		IdempotencyKey:           operationID + ":1",
	}
	if selected {
		deployment.PreviousApplicationID, deployment.PreviousRevision = previous.ApplicationID, previous.TargetRevision
	}
	if err := application.ValidateDeploymentTransition(deployment, revision, currentBinding); err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	now := time.Now().UTC()
	intent := workspaceApplicationDeploymentIntent{
		SchemaVersion: workspaceApplicationDeploymentSchemaVersion, Version: 2,
		OperationID:   operationID,
		AccountID:     firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])),
		WorkspaceID:   workspaceID,
		ComputeID:     firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])),
		StorageID:     stringValue(workspace["storageId"]),
		AttachmentID:  firstNonEmpty(stringValue(workspace["currentAttachmentId"]), stringValue(workspace["attachmentId"])),
		ApplicationID: applicationID, TargetRevision: targetRevision,
		RevisionDigest: revisionDigest, ConfigurationDigest: configurationDigest,
		ClientConfigurationDigest: clientConfigurationDigest,
		SecretBindingVersions:     secretBindingVersions, DataBindingIDs: dataBindingIDs,
		ExpectedWorkspaceVersion: bindingVersion, CurrentBinding: currentBinding,
		Configuration: configuration, SecretBindings: secretBindings, DataBindingID: dataBindingID, DataLayout: dataLayout, DataSourceRuntimeOperationID: dataSource, PreviousDeploymentID: previousID, LegacyPredecessor: legacy, WorkspaceAPIKeyID: workspaceAPIKeyID, OriginOperationID: originOperationID,
		Phase: workspaceApplicationDeploymentIntentPhase, CreatedAt: now.Format(time.RFC3339Nano),
	}
	intent.RequestHash = workspaceApplicationDeploymentRequestHash(intent)
	encoded, err := json.Marshal(intent)
	if err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	row := map[string]any{
		"id": operationID, "operationId": operationID, "accountId": intent.AccountID,
		"workspaceId": workspaceID, "resourceId": workspaceID, "resourceKind": "workspace",
		"action": workspaceApplicationDeploymentAction, "status": "pending",
		"result": string(encoded), "createdAt": now.Format(time.RFC3339Nano),
	}
	if err := app.tables.ClaimWorkspaceApplicationDeploymentIntent(ctx, row); err != nil {
		if !errors.Is(err, errWorkspaceApplicationIntentConflict) {
			return workspaceApplicationDeploymentIntent{}, err
		}
		existing, found, readErr := app.tables.GetRuntimeOperation(ctx, operationID)
		if readErr != nil || !found {
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
