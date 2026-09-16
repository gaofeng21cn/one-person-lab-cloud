package server

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/controlplane"
)

type workspaceApplicationCapabilities struct {
	Credentials bool `json:"credentials"`
	Gateway     bool `json:"gateway"`
}

type workspaceCurrentApplication struct {
	OperationID   string                           `json:"operationId"`
	ApplicationID string                           `json:"applicationId"`
	Revision      string                           `json:"revision"`
	Status        string                           `json:"status"`
	EntryURL      string                           `json:"entryUrl"`
	Capabilities  workspaceApplicationCapabilities `json:"capabilities"`
}

type workspaceApplicationInstallationProjection struct {
	OperationID   string `json:"operationId"`
	ApplicationID string `json:"applicationId"`
	Revision      string `json:"revision"`
	Status        string `json:"status"`
	CanResume     bool   `json:"canResume"`
	CanRetry      bool   `json:"canRetry"`
}

func (app *controlPlaneServer) projectWorkspaceApplicationInstallation(ctx context.Context, workspace, projection map[string]any) error {
	if id := stringValue(workspace["reservedApplicationDeploymentId"]); id != "" {
		row, found, err := app.tables.GetRuntimeOperation(ctx, id)
		if err != nil || !found {
			return errors.New("workspace_application_installation_unavailable")
		}
		intent, err := decodeWorkspaceApplicationDeploymentIntent(row)
		if err != nil || !workspaceApplicationOwnedResourcesMatch(workspace, intent) {
			return errors.New("workspace_application_installation_unavailable")
		}
		if intent.Phase == workspaceApplicationDeploymentActivePhase {
			return nil
		}
		status := stringValue(row["status"])
		if status != "pending" && status != "running" && status != "manual_review" {
			return errors.New("workspace_application_installation_unavailable")
		}
		canResume := false
		if status == "manual_review" && workspaceApplicationEntitlementOpen(workspace, time.Now()) {
			requests, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{WorkspaceID: intent.WorkspaceID, Action: workspaceDefaultApplicationAction})
			if err != nil {
				return err
			}
			if len(requests) == 1 {
				request, err := decodeWorkspaceDefaultApplication(requests[0])
				canResume = err == nil && request.AccountID == intent.AccountID && request.WorkspaceID == intent.WorkspaceID && request.DeploymentID == intent.OperationID
			}
		}
		projection["applicationInstallation"] = workspaceApplicationInstallationProjection{OperationID: intent.OperationID, ApplicationID: intent.ApplicationID, Revision: intent.TargetRevision, Status: status, CanResume: canResume, CanRetry: status == "manual_review" && intent.FailurePhase != "" && workspaceApplicationEntitlementOpen(workspace, time.Now())}
		return nil
	}
	rows, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{WorkspaceID: stringValue(workspace["id"]), Action: workspaceDefaultApplicationAction})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	if len(rows) != 1 {
		return errors.New("workspace_default_application_ambiguous")
	}
	request, err := decodeWorkspaceDefaultApplication(rows[0])
	if err != nil || request.WorkspaceID != stringValue(workspace["id"]) || request.AccountID != firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])) {
		return errors.New("workspace_application_installation_unavailable")
	}
	if request.Phase == "deployed" || request.DeploymentID != "" && request.DeploymentID == stringValue(workspace["currentApplicationDeploymentId"]) {
		return nil
	}
	status := "pending"
	if request.Phase == "failed" || request.Phase == "credentials_required" {
		status = "manual_review"
	}
	projection["applicationInstallation"] = workspaceApplicationInstallationProjection{OperationID: request.OperationID, ApplicationID: request.Revision.ApplicationID, Revision: request.Revision.Version, Status: status, CanResume: (status == "manual_review" || request.Phase == "preparing_credentials") && workspaceApplicationEntitlementOpen(workspace, time.Now())}
	return nil
}

func (app *controlPlaneServer) readWorkspaceCurrentApplication(ctx context.Context, service *controlplane.Service, workspace map[string]any) (*workspaceCurrentApplication, contracts.WorkspaceApplicationRuntimeObservation, error) {
	intent, found, err := app.currentWorkspaceApplicationDeployment(ctx, workspace)
	if err != nil || !found {
		return nil, contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	current := &workspaceCurrentApplication{OperationID: intent.OperationID, ApplicationID: intent.ApplicationID, Revision: intent.TargetRevision, Status: "unavailable"}
	input, err := app.workspaceApplicationRuntimeInput(ctx, intent)
	if err != nil {
		return current, contracts.WorkspaceApplicationRuntimeObservation{}, err
	}
	current.Capabilities = workspaceApplicationCapabilities{Credentials: input.Revision.RuntimeProfile == "opl_app", Gateway: input.Revision.RuntimeProfile == "opl_app"}
	observation, err := service.ReadWorkspaceApplicationRuntime(ctx, input)
	if err != nil {
		return current, observation, err
	}
	expectedRuntimeID := contracts.WorkspaceApplicationRuntimeID(input.RuntimeOperationID)
	if intent.Version == 1 {
		expectedRuntimeID = contracts.WorkspaceApplicationHistoricalRuntimeID(intent.WorkspaceID)
	}
	if contracts.ValidateWorkspaceApplicationRuntimeObservation(input.Revision, observation) != nil || observation.WorkspaceID != intent.WorkspaceID ||
		observation.RuntimeID != expectedRuntimeID {
		return current, observation, errors.New("workspace_application_runtime_observation_mismatch")
	}
	// A provider that publishes the endpoint itself states its URL; an entry
	// published through the installation gateway is addressed by the workspace
	// route this server owns.
	if entry := observation.Entry; entry != nil && entry.URL != "" {
		entryURL, err := url.Parse(entry.URL)
		if err != nil || entryURL.Host == "" || (entryURL.Scheme != "http" && entryURL.Scheme != "https") || entryURL.User != nil {
			return current, observation, errors.New("workspace_application_entry_invalid")
		}
		current.EntryURL = entry.URL
	} else if entry != nil {
		current.EntryURL = workspaceGatewayEntryURL(intent.WorkspaceID)
	}
	current.Status = observation.Status
	return current, observation, nil
}

func projectWorkspaceCurrentApplication(workspace map[string]any, projection map[string]any, current *workspaceCurrentApplication) {
	projection["currentApplication"] = current
	delete(projection, "url")
	delete(projection, "runtimeId")
	delete(projection, "access")
	delete(projection, "workspaceApiKeyId")
	projection["openable"] = false
	if current == nil {
		return
	}
	_, _, billingReason := workspaceBillingAccessFacts(workspace, time.Now().UTC())
	active := stringValue(workspace["state"]) == "running" && (billingReason == "" || providerAcceptanceWorkspaceBillingExempt(workspace))
	if current.Status == "ready" && current.EntryURL != "" && active {
		projection["url"], projection["openable"] = current.EntryURL, true
	}
}

func workspaceCurrentApplicationRuntimeResponse(current *workspaceCurrentApplication, observation contracts.WorkspaceApplicationRuntimeObservation) map[string]any {
	checks := make([]any, 0, len(observation.Components))
	for _, component := range observation.Components {
		checks = append(checks, map[string]any{"name": component.Name, "ok": component.State == "ready"})
	}
	status := "unready"
	if current.Status == "ready" {
		status = "running"
	} else if current.Status == "absent" {
		status = "not_found"
	}
	return map[string]any{
		"workspaceId": observation.WorkspaceID, "runtimeId": observation.RuntimeID,
		"status": status, "ready": current.Status == "ready", "url": current.EntryURL,
		"checks": checks, "currentApplication": current,
	}
}

func (app *controlPlaneServer) workspaceCurrentApplicationCredentials(w http.ResponseWriter, r *http.Request, service *controlplane.Service, workspace map[string]any) bool {
	if stringValue(workspace["currentApplicationDeploymentId"]) == "" {
		return false
	}
	current, observation, err := app.readWorkspaceCurrentApplication(r.Context(), service, workspace)
	if err != nil || current == nil || current.Status != "ready" || !current.Capabilities.Credentials {
		writeError(w, http.StatusConflict, "workspace_credentials_unavailable")
		return true
	}
	credentials, err := service.ReadWorkspaceApplicationRuntimeCredentials(r.Context(), contracts.WorkspaceApplicationRuntimeLifecycleInput{
		AccountID:   firstNonEmpty(stringValue(workspace["accountId"]), stringValue(workspace["ownerAccountId"])),
		WorkspaceID: stringValue(workspace["id"]), RuntimeID: observation.RuntimeID, RuntimeOperationID: current.OperationID + ":runtime",
	})
	if err != nil || credentials.RuntimeID != observation.RuntimeID || credentials.WorkspaceID != observation.WorkspaceID || credentials.WebUIPassword == "" {
		writeError(w, http.StatusConflict, "workspace_credentials_unavailable")
		return true
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusOK, map[string]any{"workspaceId": observation.WorkspaceID, "access": map[string]any{
		"account": credentials.WebUIUsername, "username": credentials.WebUIUsername, "password": credentials.WebUIPassword,
	}})
	return true
}

func (app *controlPlaneServer) rotateWorkspaceCurrentApplicationCredentials(w http.ResponseWriter, r *http.Request, service *controlplane.Service, workspace map[string]any, key string) bool {
	if stringValue(workspace["currentApplicationDeploymentId"]) == "" {
		return false
	}
	current, found, err := app.currentWorkspaceApplicationDeployment(r.Context(), workspace)
	if err != nil || !found {
		writeError(w, http.StatusConflict, "workspace_runtime_truth_unavailable")
		return true
	}
	input, err := app.workspaceApplicationRuntimeInput(r.Context(), current)
	if err != nil || input.Revision.RuntimeProfile != "opl_app" {
		writeError(w, http.StatusConflict, "workspace_credentials_unavailable")
		return true
	}
	configuration := current.Configuration
	configuration.CredentialVersion = stableID("workspace-application-credential-rotation", current.WorkspaceID, key)
	configuration.CredentialSourceRuntimeOperationID = ""
	next, err := app.createWorkspaceApplicationDeploymentIntent(r.Context(), current.WorkspaceID, "credential-rotation:"+key,
		current.ApplicationID, current.TargetRevision, configuration, current.SecretBindings, current.WorkspaceAPIKeyID, "", "")
	if err != nil {
		writeError(w, http.StatusConflict, "workspace_credential_rotation_conflict")
		return true
	}
	if workspaceApplicationDeploymentWorkerEnabled() {
		_ = app.runWorkspaceApplicationDeployment(r.Context(), service, next.OperationID)
	}
	row, exists, readErr := app.tables.GetRuntimeOperation(r.Context(), next.OperationID)
	if readErr != nil || !exists {
		writeError(w, http.StatusInternalServerError, "workspace_credential_rotation_readback_unavailable")
		return true
	}
	observed, readErr := decodeWorkspaceApplicationDeploymentIntent(row)
	if readErr != nil || observed.Phase == workspaceApplicationDeploymentManualReviewPhase {
		writeError(w, http.StatusConflict, "workspace_credential_rotation_failed")
		return true
	}
	workspace, exists, readErr = app.tables.GetWorkspace(r.Context(), current.WorkspaceID)
	if readErr == nil && exists && stringValue(workspace["currentApplicationDeploymentId"]) == next.OperationID && observed.Phase == workspaceApplicationDeploymentActivePhase {
		return app.workspaceCurrentApplicationCredentials(w, r, service, workspace)
	}
	w.Header().Set("Cache-Control", "private, no-store")
	writeJSON(w, http.StatusAccepted, map[string]any{"workspaceId": current.WorkspaceID, "operationId": next.OperationID, "status": "pending"})
	return true
}

func (app *controlPlaneServer) requireWorkspaceGatewayApplication(ctx context.Context, workspace map[string]any) error {
	if stringValue(workspace["currentApplicationDeploymentId"]) == "" {
		return nil
	}
	current, found, err := app.currentWorkspaceApplicationDeployment(ctx, workspace)
	if err != nil || !found {
		return errors.New("workspace_gateway_application_unavailable")
	}
	input, err := app.workspaceApplicationRuntimeInput(ctx, current)
	if err != nil || input.Revision.RuntimeProfile != "opl_app" || current.WorkspaceAPIKeyID <= 0 {
		return errors.New("workspace_gateway_not_declared")
	}
	return nil
}

func (app *controlPlaneServer) bindWorkspaceCurrentApplicationGateway(ctx context.Context, service *controlplane.Service, workspaceID, operationID string, operation *workspaceKeyRotationOperation) (bool, error) {
	workspace, found, err := app.tables.GetWorkspace(ctx, workspaceID)
	if err != nil || !found {
		return true, errWorkspaceKeyRotationState
	}
	if stringValue(workspace["currentApplicationDeploymentId"]) == "" && operation.ApplicationDeploymentID == "" {
		return false, nil
	}
	if operation.ApplicationDeploymentID == "" {
		current, found, err := app.currentWorkspaceApplicationDeployment(ctx, workspace)
		if err != nil || !found || operation.SecretVersion == "" {
			return true, errWorkspaceKeyRotationState
		}
		input, err := app.workspaceApplicationRuntimeInput(ctx, current)
		if err != nil || input.Revision.RuntimeProfile != "opl_app" {
			return true, errWorkspaceKeyRotationConflict
		}
		bindings := append([]contracts.WorkspaceApplicationRuntimeSecretBinding(nil), current.SecretBindings...)
		bound := false
		for index := range bindings {
			if bindings[index].Name == "gateway" && bindings[index].Key == "opl_gateway_api_key" {
				bindings[index].SecretRef, bindings[index].Version = operation.SecretRef, operation.SecretVersion
				bound = true
			}
		}
		if !bound {
			return true, errWorkspaceKeyRotationConflict
		}
		configuration := current.Configuration
		configuration.CredentialVersion = stableID("workspace-application-gateway-rotation", workspaceID, operationID)
		configuration.CredentialSourceRuntimeOperationID = ""
		next, err := app.createWorkspaceApplicationDeploymentIntent(ctx, workspaceID, "gateway-rotation:"+operationID,
			current.ApplicationID, current.TargetRevision, configuration, bindings, operation.NewKeyID, operationID, "")
		if err != nil {
			return true, err
		}
		operation.ApplicationDeploymentID = next.OperationID
		if err := app.persistWorkspaceKeyRotation(ctx, operationID, current.AccountID, workspaceID, "started", *operation); err != nil {
			return true, err
		}
	}
	if workspaceApplicationDeploymentWorkerEnabled() {
		if err := app.runWorkspaceApplicationDeployment(ctx, service, operation.ApplicationDeploymentID); err != nil {
			return true, err
		}
	}
	if !app.workspaceApplicationGatewayBindingConverged(ctx, service, workspaceID, *operation) {
		return true, errWorkspaceKeyRotationInProgress
	}
	operation.RuntimeID = contracts.WorkspaceApplicationRuntimeID(operation.ApplicationDeploymentID + ":runtime")
	return true, nil
}

func (app *controlPlaneServer) workspaceApplicationGatewayBindingConverged(ctx context.Context, service *controlplane.Service, workspaceID string, operation workspaceKeyRotationOperation) bool {
	workspace, found, err := app.tables.GetWorkspace(ctx, workspaceID)
	if err != nil || !found || stringValue(workspace["currentApplicationDeploymentId"]) != operation.ApplicationDeploymentID {
		return false
	}
	current, found, err := app.currentWorkspaceApplicationDeployment(ctx, workspace)
	if err != nil || !found || current.WorkspaceAPIKeyID != operation.NewKeyID || current.Phase != workspaceApplicationDeploymentActivePhase {
		return false
	}
	bound := false
	for _, binding := range current.SecretBindings {
		if binding.Name == "gateway" && binding.Key == "opl_gateway_api_key" && binding.SecretRef == operation.SecretRef && binding.Version == operation.SecretVersion {
			bound = true
		}
	}
	projection, _, err := app.readWorkspaceCurrentApplication(ctx, service, workspace)
	return bound && err == nil && projection != nil && projection.Status == "ready" && projection.Capabilities.Gateway
}
