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
	errWorkspaceApplicationIntentConflict   = errors.New("workspace_application_deployment_intent_conflict")
	errWorkspaceApplicationResourcesUnready = errors.New("workspace_application_resources_unready")
	errWorkspaceApplicationBindingUnknown   = errors.New("workspace_application_binding_unknown")
	errWorkspaceApplicationWorkspaceGone    = errors.New("workspace_application_workspace_not_found")
)

// workspaceApplicationDeploymentIntent is the durable, immutable intent that
// binds one admitted revision to one Workspace at one binding version. The
// Fabric runtime creation and the atomic activation consume it in later
// slices; neither can bypass or rewrite it.
type workspaceApplicationDeploymentIntent struct {
	SchemaVersion            int                                               `json:"schemaVersion"`
	Version                  int                                               `json:"version"`
	RequestHash              string                                            `json:"requestHash"`
	OperationID              string                                            `json:"operationId"`
	AccountID                string                                            `json:"accountId"`
	WorkspaceID              string                                            `json:"workspaceId"`
	ComputeID                string                                            `json:"computeId"`
	StorageID                string                                            `json:"storageId"`
	AttachmentID             string                                            `json:"attachmentId"`
	ApplicationID            string                                            `json:"applicationId"`
	TargetRevision           string                                            `json:"targetRevision"`
	RevisionDigest           string                                            `json:"revisionDigest"`
	ConfigurationDigest      string                                            `json:"configurationDigest"`
	SecretBindingVersions    []string                                          `json:"secretBindingVersions,omitempty"`
	DataBindingIDs           []string                                          `json:"dataBindingIds,omitempty"`
	ExpectedWorkspaceVersion int64                                             `json:"expectedWorkspaceVersion"`
	CurrentBinding           string                                            `json:"currentBinding"`
	Phase                    string                                            `json:"phase"`
	CreatedAt                string                                            `json:"createdAt"`
	RuntimeObservation       *contracts.WorkspaceApplicationRuntimeObservation `json:"runtimeObservation,omitempty"`
	ActivationAt             string                                            `json:"activationAt,omitempty"`
	ReceiptID                string                                            `json:"receiptId,omitempty"`
	LastError                string                                            `json:"lastError,omitempty"`
}

func workspaceApplicationDeploymentOperationID(workspaceID, key string) string {
	return "workspace-application-deploy-" + stableID(workspaceID, key)
}

func workspaceApplicationDeploymentRequestHash(intent workspaceApplicationDeploymentIntent) string {
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
		intent.SchemaVersion != workspaceApplicationDeploymentSchemaVersion || intent.Version != 1 ||
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
	workspaceID, mutationKey, applicationID, targetRevision, configurationDigest string,
	secretBindingVersions, dataBindingIDs []string,
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
	deployment := contracts.WorkspaceApplicationDeployment{
		SchemaVersion: 1, OperationID: operationID, WorkspaceID: workspaceID,
		ApplicationID: applicationID, TargetRevision: targetRevision,
		ConfigurationDigest:   configurationDigest,
		SecretBindingVersions: secretBindingVersions, DataBindingIDs: dataBindingIDs,
		ExpectedWorkspaceVersion: bindingVersion,
		IdempotencyKey:           operationID + ":1",
	}
	if err := application.ValidateDeploymentTransition(deployment, revision, currentBinding); err != nil {
		return workspaceApplicationDeploymentIntent{}, err
	}
	now := time.Now().UTC()
	intent := workspaceApplicationDeploymentIntent{
		SchemaVersion: workspaceApplicationDeploymentSchemaVersion, Version: 1,
		OperationID:   operationID,
		AccountID:     firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])),
		WorkspaceID:   workspaceID,
		ComputeID:     firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])),
		StorageID:     stringValue(workspace["storageId"]),
		AttachmentID:  firstNonEmpty(stringValue(workspace["currentAttachmentId"]), stringValue(workspace["attachmentId"])),
		ApplicationID: applicationID, TargetRevision: targetRevision,
		RevisionDigest: revisionDigest, ConfigurationDigest: configurationDigest,
		SecretBindingVersions: secretBindingVersions, DataBindingIDs: dataBindingIDs,
		ExpectedWorkspaceVersion: bindingVersion, CurrentBinding: currentBinding,
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
		if !ok {
			return
		}
		workspaceID, _ := input["workspaceId"].(string)
		applicationID, _ := input["applicationId"].(string)
		targetRevision, _ := input["targetRevision"].(string)
		configurationDigest, _ := input["configurationDigest"].(string)
		if workspaceID == "" || applicationID == "" || targetRevision == "" || configurationDigest == "" {
			writeError(w, http.StatusBadRequest, "invalid_application_deployment")
			return
		}
		secretBindings := decodeStringList(input["secretBindingVersions"])
		dataBindings := decodeStringList(input["dataBindingIds"])
		intent, err := app.createWorkspaceApplicationDeploymentIntent(
			r.Context(), workspaceID, key, applicationID, targetRevision, configurationDigest,
			secretBindings, dataBindings,
		)
		if err != nil {
			switch {
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
