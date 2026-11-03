package server

import (
	"context"
	"errors"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

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
