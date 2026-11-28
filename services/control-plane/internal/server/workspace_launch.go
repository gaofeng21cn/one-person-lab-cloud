package server

import (
	"errors"
	"os"
	"regexp"
	"strconv"
	"strings"

	contracts "opl-cloud/packages/contracts/go"
)

var (
	errInvalidWorkspaceLaunchOperation = errors.New("invalid_workspace_launch_operation")
	errWorkspaceLaunchCASConflict      = errors.New("workspace_launch_cas_conflict")
)

const (
	workspaceLaunchAction = "workspace.launch.v2"

	workspaceKeyCodexGroupBound = "codex_group_bound"
)

var workspaceImageDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var workspaceProviderSpecDigestPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func workspaceLaunchOperationID(accountID, key string) string {
	return "workspace-launch-" + stableID(accountID, key)[:18]
}

func currentWorkspaceImageDigest() string {
	return currentWorkspaceImageDigestWith(os.Getenv)
}

func currentWorkspaceImageDigestWith(getenv func(string) string) string {
	if getenv == nil {
		getenv = os.Getenv
	}
	value := strings.TrimSpace(getenv("OPL_WORKSPACE_IMAGE"))
	repository, digest, ok := strings.Cut(value, "@")
	if ok && strings.TrimSpace(repository) != "" && !strings.Contains(repository, "@") && workspaceImageDigestPattern.MatchString(digest) {
		return value
	}
	return ""
}

func isWorkspaceLaunchAction(action string) bool {
	return action == workspaceLaunchAction || action == "workspace.launch"
}

func terminalWorkspaceLaunchStatus(status string) bool {
	switch contracts.LaunchStatus(status) {
	case contracts.StatusSucceeded, contracts.StatusFailed, contracts.StatusRefunded:
		return true
	default:
		return false
	}
}

func workspaceLaunchResponse(row map[string]any) (map[string]any, error) {
	operation, err := decodeWorkspaceLaunchReconcileOperation(row)
	if err != nil {
		return nil, err
	}
	return workspaceLaunchReconcileResponse(operation, row)
}

func workspaceLaunchReconcileResponse(operation workspaceLaunchReconcileOperation, row map[string]any) (map[string]any, error) {
	if operation.SchemaVersion != workspaceLaunchReconcileSchemaVersion || operation.raw == nil {
		return nil, errInvalidWorkspaceLaunchOperation
	}
	workspaceAPIKeyID := ""
	if value := operation.int64Fact("workspaceApiKeyId"); value > 0 {
		workspaceAPIKeyID = strconv.FormatInt(value, 10)
	}
	response := map[string]any{
		"operationId": operation.ID, "schemaVersion": operation.SchemaVersion, "version": operation.Version,
		"provisioningMode": string(operation.provisioningMode()),
		"status":           string(operation.Status), "stage": string(operation.Stage), "phase": string(operation.Stage),
		"accountId": operation.stringFact("accountId"), "workspaceId": operation.stringFact("workspaceId"),
		"name": operation.stringFact("name"), "packageId": operation.stringFact("packageId"), "sizeGb": operation.intFact("sizeGb"),
		"autoRenew": operation.boolFact("autoRenew"), "priceVersion": operation.stringFact("priceVersion"),
		"currency": pricingCurrency, "totalChargeUsdMicros": operation.int64Fact("totalChargeUsdMicros"),
		"computeAllocationId": operation.stringFact("computeAllocationId"), "storageId": operation.stringFact("storageId"),
		"attachmentId": operation.stringFact("attachmentId"), "workspaceApiKeyId": workspaceAPIKeyID,
		"workspaceKeyStatus": operation.stringFact("workspaceKeyStatus"), "workspaceKeyFingerprint": operation.stringFact("workspaceKeyFingerprint"),
		"runtimeServiceName": operation.stringFact("runtimeServiceName"), "url": operation.stringFact("url"), "receiptId": operation.stringFact("receiptId"),
		"continuationAttemptBudgets": operation.Attempts,
	}
	if operation.Closeout != nil {
		response["closeout"] = workspaceLaunchCloseoutResponse(operation)
	}
	if operation.Status == contracts.StatusManualReview {
		response["failureStage"] = string(operation.Stage)
		if diagnostic := operation.Observations[operation.Stage].Diagnostic; diagnostic != nil {
			response["blockReason"] = diagnostic.BlockReason
			checks := make([]map[string]any, 0, len(diagnostic.Checks))
			for _, check := range diagnostic.Checks {
				checks = append(checks, map[string]any{"name": check.Name, "ok": check.OK})
			}
			response["checks"] = checks
		}
	}
	if operation.ResumeAuthorization != nil {
		authorization := *operation.ResumeAuthorization
		authorization.AcceptanceBResumeExisting = nil
		response["resumeAuthorization"] = authorization
		response["resumeAuthorizationConsumedAt"] = operation.ResumeAuthorizationConsumedAt
	}
	if row != nil {
		response["createdAt"], response["updatedAt"] = row["createdAt"], row["updatedAt"]
	} else {
		response["createdAt"] = operation.CreatedAt
	}
	return response, nil
}

func workspaceLaunchReconcileRequestMatches(operation workspaceLaunchReconcileOperation, accountID, ownerUserID, name, packageID string, autoRenew bool, mode contracts.WorkspaceProvisioningMode) bool {
	if mode == "" {
		mode = contracts.WorkspaceProvisioningFull
	}
	return operation.stringFact("accountId") == accountID && operation.stringFact("ownerUserId") == ownerUserID &&
		operation.stringFact("name") == name && operation.stringFact("packageId") == packageID &&
		operation.boolFact("autoRenew") == autoRenew && operation.provisioningMode() == mode
}
