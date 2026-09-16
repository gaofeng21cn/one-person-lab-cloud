package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
	"opl-cloud/services/control-plane/internal/domain/application"
)

const workspaceDefaultApplicationAction = "workspace.application.default-install"

// This installation request is committed beside the resource Launch, rather
// than becoming a purchase stage. Its failure cannot alter a completed debit.
type workspaceDefaultApplicationRequest struct {
	SchemaVersion       int                                    `json:"schemaVersion"`
	OperationID         string                                 `json:"operationId"`
	LaunchOperationID   string                                 `json:"launchOperationId"`
	AccountID           string                                 `json:"accountId"`
	WorkspaceID         string                                 `json:"workspaceId"`
	OwnerUserID         string                                 `json:"ownerUserId"`
	Sub2APIUserID       int64                                  `json:"sub2apiUserId"`
	WorkspaceKeyGroupID int64                                  `json:"workspaceKeyGroupId"`
	Revision            contracts.WorkspaceApplicationRevision `json:"revision"`
	Phase               string                                 `json:"phase"`
	GatewaySecret       *clients.GatewaySecretWriteResult      `json:"gatewaySecret,omitempty"`
	WorkspaceAPIKeyID   int64                                  `json:"workspaceApiKeyId,omitempty"`
	DeploymentID        string                                 `json:"deploymentId,omitempty"`
	LastError           string                                 `json:"lastError,omitempty"`
}

func workspaceDefaultApplicationOperationID(launchID string) string {
	return launchID + ":default-application"
}

func defaultOPLApplicationRevision(image string) contracts.WorkspaceApplicationRevision {
	revision := contracts.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationID: "opl-app", Platform: "linux/amd64", Image: image,
		Ports: []contracts.WorkspaceApplicationPort{{Name: "webui", Port: 3000, Protocol: "TCP"}}, EntryPort: "webui",
		HealthChecks:     []contracts.WorkspaceApplicationHealthCheck{{Port: 3000, Path: "/", InitialDelaySeconds: 10}},
		PersistentMounts: []contracts.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}, {Name: "projects", MountPath: "/projects"}},
		ScratchMounts:    []contracts.WorkspaceApplicationMount{{Name: "recovery", MountPath: "/recovery"}},
		Credentials: []contracts.WorkspaceApplicationCredential{
			{Name: "admin-password", Kind: contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword, Target: "/run/secrets/opl_webui_password", Username: "opl"},
			{Name: "session-secret", Kind: contracts.WorkspaceApplicationCredentialWorkspaceSessionSecret, Target: "/run/secrets/webui_session_secret"},
			{Name: "gateway", Kind: contracts.WorkspaceApplicationCredentialGatewayKey, Target: "/run/secrets/opl_gateway_api_key"},
		}, ExposurePolicy: "application"}
	payload, _ := json.Marshal(revision)
	revision.Version = fmt.Sprintf("%x", sha256.Sum256(payload))
	return revision
}

func workspaceDefaultApplicationRow(request workspaceDefaultApplicationRequest) (map[string]any, error) {
	if request.SchemaVersion != 1 || request.OperationID != workspaceDefaultApplicationOperationID(request.LaunchOperationID) || request.AccountID == "" || request.WorkspaceID == "" || request.OwnerUserID == "" || request.Sub2APIUserID <= 0 || request.WorkspaceKeyGroupID <= 0 || !contracts.WorkspaceApplicationRequiresPlatformCredentials(request.Revision) {
		return nil, errors.New("workspace_default_application_invalid")
	}
	if err := contracts.ValidateWorkspaceApplicationRevision(request.Revision); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	status := "pending"
	if request.Phase == "deployed" {
		status = "succeeded"
	}
	if request.Phase == "failed" {
		status = "failed"
	}
	return map[string]any{"id": request.OperationID, "operationId": request.OperationID, "accountId": request.AccountID, "workspaceId": request.WorkspaceID, "resourceId": request.WorkspaceID, "resourceKind": "workspace", "action": workspaceDefaultApplicationAction, "status": status, "result": string(encoded), "createdAt": time.Now().UTC().Format(time.RFC3339Nano)}, nil
}

func decodeWorkspaceDefaultApplication(row map[string]any) (workspaceDefaultApplicationRequest, error) {
	var request workspaceDefaultApplicationRequest
	if stringValue(row["action"]) != workspaceDefaultApplicationAction || json.Unmarshal([]byte(stringValue(row["result"])), &request) != nil || request.OperationID != stringValue(row["id"]) || request.AccountID != stringValue(row["accountId"]) || request.WorkspaceID != stringValue(row["workspaceId"]) {
		return request, errors.New("workspace_default_application_invalid")
	}
	_, err := workspaceDefaultApplicationRow(request)
	return request, err
}

func (app *controlPlaneServer) persistWorkspaceDefaultApplication(ctx context.Context, row map[string]any, request workspaceDefaultApplicationRequest) error {
	next, err := workspaceDefaultApplicationRow(request)
	if err != nil {
		return err
	}
	next["createdAt"] = row["createdAt"]
	if err := app.tables.PersistWorkspaceApplicationOperation(ctx, stringValue(row["result"]), next); err != nil {
		return err
	}
	row["result"], row["status"] = next["result"], next["status"]
	return nil
}

func (app *controlPlaneServer) prepareDefaultWorkspaceApplication(ctx context.Context, service *controlplane.Service, launchID string, credential clients.SessionDelegatedCredential, userID int64) error {
	unlock := app.lockResource("workspace-default-application", launchID)
	defer unlock()
	row, found, err := app.tables.GetRuntimeOperation(ctx, workspaceDefaultApplicationOperationID(launchID))
	if err != nil || !found {
		return err
	}
	request, err := decodeWorkspaceDefaultApplication(row)
	if err != nil {
		return err
	}
	if request.GatewaySecret != nil || request.Phase == "failed" {
		return nil
	}
	if request.Sub2APIUserID != userID || credential.Bearer == "" || !credential.ExpiresAt.After(time.Now()) {
		return errors.New("workspace_default_application_credentials_required")
	}
	request.Phase = "preparing_credentials"
	if err := app.persistWorkspaceDefaultApplication(ctx, row, request); err != nil {
		return err
	}
	// Use the existing Sub2API idempotent Key owner and transient Secret write.
	key, err := service.CreateGatewayUserKey(ctx, credential, userID, clients.Sub2APICreateKeyInput{Name: workspaceReservedKeyName(request.WorkspaceID), GroupID: request.WorkspaceKeyGroupID}, request.OperationID+":workspace-key")
	if err == nil && (key.ID <= 0 || request.WorkspaceAPIKeyID > 0 && request.WorkspaceAPIKeyID != key.ID || key.UserID != userID || key.Status != "active" || key.GroupID == nil || *key.GroupID != request.WorkspaceKeyGroupID || key.Key == "") {
		err = errors.New("workspace_default_application_key_invalid")
	}
	if err == nil {
		request.WorkspaceAPIKeyID = key.ID
		if err := app.persistWorkspaceDefaultApplication(ctx, row, request); err != nil {
			return err
		}
		var secret clients.GatewaySecretWriteResult
		secret, err = service.SyncWorkspaceGatewaySecretWithKey(ctx, request.AccountID, request.WorkspaceID, userID, key, request.OperationID+":workspace-key")
		if err == nil {
			request.GatewaySecret = &secret
			request.WorkspaceAPIKeyID = key.ID
			request.Phase = "waiting_resources"
			request.LastError = ""
		}
	}
	if err != nil {
		request.Phase = "credentials_required"
		request.LastError = "workspace_default_application_credentials_unconfirmed"
	}
	persistErr := app.persistWorkspaceDefaultApplication(ctx, row, request)
	return errors.Join(err, persistErr)
}

func (app *controlPlaneServer) runWorkspaceDefaultApplicationsOnce(ctx context.Context, service *controlplane.Service) error {
	rows, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{Action: workspaceDefaultApplicationAction, Statuses: []string{"pending", "failed"}})
	if err != nil {
		return err
	}
	var errs []error
	for _, row := range rows {
		if err := app.runWorkspaceDefaultApplication(ctx, service, stringValue(row["id"])); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (app *controlPlaneServer) runWorkspaceDefaultApplication(ctx context.Context, service *controlplane.Service, id string) error {
	row, found, err := app.tables.GetRuntimeOperation(ctx, id)
	if err != nil || !found {
		return err
	}
	request, err := decodeWorkspaceDefaultApplication(row)
	if err != nil {
		return err
	}
	unlock := app.lockResource("workspace-default-application", request.LaunchOperationID)
	defer unlock()
	row, found, err = app.tables.GetRuntimeOperation(ctx, id)
	if err != nil || !found {
		return err
	}
	request, err = decodeWorkspaceDefaultApplication(row)
	if err != nil {
		return err
	}
	if request.Phase == "deployed" {
		return nil
	}
	if request.Phase == "failed" {
		if request.DeploymentID == "" {
			return nil
		}
		deployment, found, err := app.tables.GetRuntimeOperation(ctx, request.DeploymentID)
		if err != nil || !found {
			return err
		}
		intent, err := decodeWorkspaceApplicationDeploymentIntent(deployment)
		if err != nil {
			return err
		}
		if intent.Phase == workspaceApplicationDeploymentManualReviewPhase {
			return nil
		}
		request.Phase, request.LastError = "installing", ""
		if err := app.persistWorkspaceDefaultApplication(ctx, row, request); err != nil {
			return err
		}
	}
	launchRow, found, err := app.tables.GetRuntimeOperation(ctx, request.LaunchOperationID)
	if err != nil || !found {
		return err
	}
	launch, err := decodeWorkspaceLaunchReconcileOperation(launchRow)
	if err != nil {
		return err
	}
	if launch.stringFact("accountId") != request.AccountID || launch.stringFact("workspaceId") != request.WorkspaceID || launch.provisioningMode() != contracts.WorkspaceProvisioningResourceOnly {
		return errors.New("workspace_default_application_purchase_mismatch")
	}
	if launch.Status == contracts.StatusFailed || launch.Status == contracts.StatusRefunded {
		request.Phase = "failed"
		request.LastError = "workspace_resources_not_delivered"
		return app.persistWorkspaceDefaultApplication(ctx, row, request)
	}
	if launch.Status != contracts.StatusSucceeded || request.GatewaySecret == nil {
		return nil
	}
	if request.DeploymentID == "" {
		digest, err := application.RevisionDigest(request.Revision)
		if err != nil {
			return err
		}
		payload, _ := json.Marshal(request.Revision)
		if _, err := app.tables.ApplyApplicationRevisionAdmission(ctx, applicationRevisionMutation{ApplicationID: request.Revision.ApplicationID, Version: request.Revision.Version, Digest: digest, Payload: string(payload), AdmittedByUserID: "control-plane-default-application-policy"}); err != nil {
			request.Phase, request.LastError = "failed", "workspace_default_application_revision_unconfirmed"
			return errors.Join(err, app.persistWorkspaceDefaultApplication(ctx, row, request))
		}
		configuration, bindings, keyID, err := app.workspaceOPLApplicationConfiguration(ctx, service, request.WorkspaceID, request.Revision.ApplicationID)
		if err != nil {
			request.Phase, request.LastError = "failed", "workspace_default_application_configuration_unconfirmed"
			return errors.Join(err, app.persistWorkspaceDefaultApplication(ctx, row, request))
		}
		intent, err := app.createWorkspaceApplicationDeploymentIntent(ctx, request.WorkspaceID, request.OperationID, request.Revision.ApplicationID, request.Revision.Version, configuration, bindings, keyID, "", "")
		if err != nil {
			request.Phase, request.LastError = "failed", "workspace_default_application_deployment_unconfirmed"
			return errors.Join(err, app.persistWorkspaceDefaultApplication(ctx, row, request))
		}
		request.DeploymentID, request.Phase = intent.OperationID, "installing"
		if err := app.persistWorkspaceDefaultApplication(ctx, row, request); err != nil {
			return err
		}
	}
	if err := app.runWorkspaceApplicationDeployment(ctx, service, request.DeploymentID); err != nil {
		return err
	}
	deployment, found, err := app.tables.GetRuntimeOperation(ctx, request.DeploymentID)
	if err != nil || !found {
		return err
	}
	intent, err := decodeWorkspaceApplicationDeploymentIntent(deployment)
	if err != nil {
		return err
	}
	if intent.Phase == workspaceApplicationDeploymentActivePhase {
		request.Phase = "deployed"
		return app.persistWorkspaceDefaultApplication(ctx, row, request)
	}
	if intent.Phase == workspaceApplicationDeploymentManualReviewPhase {
		request.Phase = "failed"
		request.LastError = intent.LastError
		return app.persistWorkspaceDefaultApplication(ctx, row, request)
	}
	return nil
}

func (app *controlPlaneServer) workspaceOPLApplicationConfiguration(ctx context.Context, service *controlplane.Service, workspaceID, applicationID string) (contracts.WorkspaceApplicationRuntimeConfiguration, []contracts.WorkspaceApplicationRuntimeSecretBinding, int64, error) {
	var configuration contracts.WorkspaceApplicationRuntimeConfiguration
	workspace, found, err := app.tables.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return configuration, nil, 0, err
	}
	if !found {
		return configuration, nil, 0, errWorkspaceApplicationWorkspaceGone
	}
	// These exact keys form the CP-owned OPL runtime ABI. Each new deployment
	// supplies a complete set of user environment values over this base; a prior
	// user's settings never become reserved keys through application selection.
	configuration.Environment = map[string]string{
		"OPL_WEBUI_DEPLOYMENT_MODE": "cloud", "OPL_WEBUI_AUTH_MODE": "password", "OPL_WEBUI_USERNAME": "opl",
		"OPL_GATEWAY_API_KEY_FILE": "/run/secrets/opl_gateway_api_key",
		"OPL_WEBUI_PASSWORD_FILE":  "/run/secrets/opl_webui_password", "OPL_WEBUI_SESSION_SECRET_FILE": "/run/secrets/webui_session_secret",
		"OPL_WORKSPACE_ID": workspaceID, "OPL_COMPUTE_ALLOCATION_ID": firstNonEmpty(stringValue(workspace["currentComputeAllocationId"]), stringValue(workspace["computeAllocationId"])), "OPL_OWNER_ACCOUNT_ID": stringValue(workspace["accountId"]),
		"DATA_DIR": "/data", "AIONUI_DATA_DIR": "/data", "OPL_PROJECTS_DIR": "/projects", "OPL_WORKSPACE_ROOT": "/projects", "OPL_WEBUI_RECOVERY_DIR": "/recovery", "AIONUI_ALLOW_REMOTE": "true", "ALLOW_REMOTE": "true", "HOME": "/data", "CODEX_HOME": "/data/.codex",
	}
	var secret clients.GatewaySecretWriteResult
	var keyID int64
	if current, selected, err := app.workspaceApplicationSelectedAncestor(ctx, workspace, applicationID); err != nil {
		return configuration, nil, 0, err
	} else if selected {
		input, err := app.workspaceApplicationRuntimeInput(ctx, current)
		if err != nil {
			return configuration, nil, 0, err
		}
		if current.Version == 2 && contracts.WorkspaceApplicationRequiresPlatformCredentials(input.Revision) {
			configuration.CredentialVersion = current.Configuration.CredentialVersion
			configuration.CredentialSourceRuntimeOperationID = current.Configuration.CredentialSourceRuntimeOperationID
			return configuration, current.SecretBindings, current.WorkspaceAPIKeyID, nil
		}
	}
	rows, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{WorkspaceID: workspaceID, Action: workspaceDefaultApplicationAction})
	if err != nil {
		return configuration, nil, 0, err
	}
	if len(rows) == 1 {
		request, err := decodeWorkspaceDefaultApplication(rows[0])
		if err != nil {
			return configuration, nil, 0, err
		}
		if request.GatewaySecret != nil {
			secret = *request.GatewaySecret
			keyID = request.WorkspaceAPIKeyID
		}
	} else if len(rows) > 1 {
		return configuration, nil, 0, errors.New("workspace_default_application_ambiguous")
	}
	if secret.SecretRef == "" {
		keyID = int64(numberField(workspace, "workspaceApiKeyId", 0))
		userID, err := app.sub2APIUserID(ctx, stringValue(workspace["accountId"]))
		if err != nil {
			return configuration, nil, 0, err
		}
		secret, err = service.SyncWorkspaceGatewaySecretByID(ctx, stringValue(workspace["accountId"]), workspaceID, userID, keyID, workspaceReservedKeyName(workspaceID), "application-secret-"+stableID(workspaceID, stringValue(workspace["applicationBinding"])))
		if err != nil {
			return configuration, nil, 0, err
		}
	}
	if secret.SecretRef == "" || secret.Version == "" || keyID <= 0 {
		return configuration, nil, 0, errors.New("workspace_application_gateway_binding_missing")
	}
	return configuration, []contracts.WorkspaceApplicationRuntimeSecretBinding{{Name: "gateway", SecretRef: secret.SecretRef, Version: secret.Version, Key: "opl_gateway_api_key"}}, keyID, nil
}
