package server

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	contracts "opl-cloud/packages/contracts/go"
)

const (
	workspaceApplicationDeploymentAction        = "workspace.application.deploy"
	workspaceApplicationDeploymentSchemaVersion = 1
	workspaceApplicationDeploymentIntentPhase   = "intent"
)

var (
	errWorkspaceApplicationResourcesUnready = errors.New("workspace_application_resources_unready")
	errWorkspaceApplicationBindingUnknown   = errors.New("workspace_application_binding_unknown")
	errWorkspaceApplicationWorkspaceGone    = errors.New("workspace_application_workspace_not_found")
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
