package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
	"opl-cloud/services/control-plane/internal/domain/application"
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
			if revision.RuntimeProfile == "opl_app" && configuration.CredentialVersion == "" && configuration.CredentialSourceRuntimeOperationID == "" {
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
		historicalLayout, historicalSource, err := app.workspaceApplicationHistoricalDataBinding(ctx, workspace, applicationID, revision.RuntimeProfile, previous, selected, owned)
		if err != nil {
			return workspaceApplicationDeploymentIntent{}, err
		}
		if historicalLayout != "" {
			dataLayout, dataSource = historicalLayout, historicalSource
		}
	}
	if currentBinding == "opl_app" && revision.RuntimeProfile == "opl_app" && applicationID == "opl-app" {
		dataLayout = "legacy_opl"
	}
	if revision.RuntimeProfile == "opl_app" {
		configuration, err = app.workspaceApplicationCredentialConfiguration(ctx, workspace, applicationID, operationID, configuration)
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
	if currentBinding == "opl_app" {
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

func decodeStringList(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	list := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
			list = append(list, text)
		}
	}
	return list
}

func registerApplicationDeploymentRoutes(mux *http.ServeMux, app *controlPlaneServer, service *controlplane.Service) {
	mux.HandleFunc("POST /api/operator/application-deployments", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		input := decodeJSON(r)
		key, ok := requiredMutationKey(w, r)
		if !ok {
			return
		}
		workspaceID, _ := input["workspaceId"].(string)
		applicationID, _ := input["applicationId"].(string)
		targetRevision, _ := input["targetRevision"].(string)
		var configuration contracts.WorkspaceApplicationRuntimeConfiguration
		rawConfiguration, configErr := json.Marshal(input["configuration"])
		if configErr != nil || json.Unmarshal(rawConfiguration, &configuration) != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_configuration")
			return
		}
		if _, supplied := input["configurationDigest"]; supplied {
			writeError(w, http.StatusBadRequest, "client_configuration_digest_forbidden")
			return
		}
		if _, supplied := input["secretBindingVersions"]; supplied {
			writeError(w, http.StatusBadRequest, "client_secret_binding_forbidden")
			return
		}
		if _, supplied := input["dataBindingIds"]; supplied {
			writeError(w, http.StatusBadRequest, "client_data_binding_forbidden")
			return
		}
		if workspaceID == "" || applicationID == "" || targetRevision == "" {
			writeError(w, http.StatusBadRequest, "invalid_application_deployment")
			return
		}
		clientConfiguration, err := json.Marshal(configuration)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_configuration")
			return
		}
		clientConfigurationDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(clientConfiguration))
		// Replay the accepted client command before resolving credentials or
		// configuration from the currently selected application.
		priorRow, replayed, err := app.tables.GetRuntimeOperation(r.Context(), workspaceApplicationDeploymentOperationID(workspaceID, key))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if replayed {
			prior, err := decodeWorkspaceApplicationDeploymentIntent(priorRow)
			if err != nil || prior.Version != 2 || prior.ApplicationID != applicationID || prior.TargetRevision != targetRevision || prior.ClientConfigurationDigest != clientConfigurationDigest || prior.OriginOperationID != "" {
				writeError(w, http.StatusConflict, errWorkspaceApplicationIntentConflict.Error())
				return
			}
			writeJSON(w, http.StatusAccepted, map[string]any{"intent": prior})
			return
		}
		var secretBindings []contracts.WorkspaceApplicationRuntimeSecretBinding
		var workspaceAPIKeyID int64
		// OPL credentials are resolved by the CP owner, never accepted as arbitrary application input.
		revisionRow, admitted, revisionErr := app.tables.AdmittedApplicationRevision(r.Context(), applicationID, targetRevision)
		if revisionErr != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if admitted {
			revision, valid := decodeApplicationRevisionPayload(stringValue(revisionRow["payload"]))
			if !valid {
				writeError(w, http.StatusConflict, "workspace_application_revision_invalid")
				return
			}
			if revision.RuntimeProfile == "opl_app" {
				requestedEnvironment := configuration.Environment
				var prepErr error
				configuration, secretBindings, workspaceAPIKeyID, prepErr = app.workspaceOPLApplicationConfiguration(r.Context(), service, workspaceID, applicationID)
				if prepErr != nil {
					writeError(w, http.StatusConflict, prepErr.Error())
					return
				}
				// The owner returns only ABI environment keys and the retained
				// credential identity. Omitted user keys disappear in this command.
				for name, value := range requestedEnvironment {
					if owned, exists := configuration.Environment[name]; exists && owned != value {
						writeError(w, http.StatusBadRequest, "workspace_application_owned_configuration_conflict")
						return
					}
					configuration.Environment[name] = value
				}
			}
		}
		intent, err := app.createWorkspaceApplicationDeploymentIntent(
			r.Context(), workspaceID, key, applicationID, targetRevision, configuration, secretBindings, workspaceAPIKeyID, "", clientConfigurationDigest,
		)
		if err != nil {
			switch {
			case errors.Is(err, errWorkspaceApplicationConfigurationInvalid):
				writeError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, errWorkspaceApplicationWorkspaceGone):
				writeError(w, http.StatusNotFound, "workspace_not_found")
			case errors.Is(err, application.ErrRevisionNotAdmitted):
				writeError(w, http.StatusNotFound, "workspace_application_revision_not_found")
			case errors.Is(err, errWorkspaceApplicationResourcesUnready), errors.Is(err, errWorkspaceApplicationBindingUnknown),
				errors.Is(err, application.ErrDeploymentTransitionInvalid):
				writeError(w, http.StatusConflict, err.Error())
			case errors.Is(err, errWorkspaceApplicationIntentConflict):
				writeError(w, http.StatusConflict, errWorkspaceApplicationIntentConflict.Error())
			default:
				writeError(w, http.StatusInternalServerError, "state_persist_failed")
			}
			return
		}
		if workspaceApplicationDeploymentWorkerEnabled() && intent.Phase == workspaceApplicationDeploymentIntentPhase {
			go func() {
				_ = app.runWorkspaceApplicationDeployment(context.Background(), service, intent.OperationID)
			}()
		}
		writeJSON(w, http.StatusAccepted, map[string]any{"intent": intent})
	}))

	mux.HandleFunc("GET /api/operator/application-deployments/{operationId}", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		row, found, err := app.tables.GetRuntimeOperation(r.Context(), r.PathValue("operationId"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if !found || stringValue(row["action"]) != workspaceApplicationDeploymentAction {
			writeError(w, http.StatusNotFound, "workspace_application_deployment_not_found")
			return
		}
		intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "workspace_application_deployment_intent_invalid")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": stringValue(row["status"]), "intent": intent})
	}))
}
