package server

import (
	"encoding/json"
	"errors"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
)

const workspaceDefaultApplicationAction = "workspace.application.default-install"

// Historical installation requests remain readable for original-order identity,
// application credential lineage and Secret cleanup. No installer writes them.
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

func validateWorkspaceDefaultApplicationRequest(request workspaceDefaultApplicationRequest) error {
	if request.SchemaVersion != 1 || request.OperationID != workspaceDefaultApplicationOperationID(request.LaunchOperationID) || request.AccountID == "" || request.WorkspaceID == "" || request.OwnerUserID == "" || request.Sub2APIUserID <= 0 || request.WorkspaceKeyGroupID <= 0 || !contracts.WorkspaceApplicationRequiresPlatformCredentials(request.Revision) {
		return errors.New("workspace_default_application_invalid")
	}
	return contracts.ValidateWorkspaceApplicationRevision(request.Revision)
}

func decodeWorkspaceDefaultApplication(row map[string]any) (workspaceDefaultApplicationRequest, error) {
	var request workspaceDefaultApplicationRequest
	if stringValue(row["action"]) != workspaceDefaultApplicationAction || json.Unmarshal([]byte(stringValue(row["result"])), &request) != nil || request.OperationID != stringValue(row["id"]) || request.AccountID != stringValue(row["accountId"]) || request.WorkspaceID != stringValue(row["workspaceId"]) {
		return request, errors.New("workspace_default_application_invalid")
	}
	err := validateWorkspaceDefaultApplicationRequest(request)
	return request, err
}
