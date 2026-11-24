package server

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"time"
)

var (
	errWorkspaceRenewalCASConflict  = errors.New("workspace_renewal_cas_conflict")
	errInvalidWorkspaceRenewalAudit = errors.New("invalid_workspace_renewal_audit")
)

// monthlyRenewalLead is the window before the paid period ends in which a
// renewal becomes due. It is the shared renewal schedule fact of the retained
// renewal records and the auto-renew intent.
const monthlyRenewalLead = 24 * time.Hour

// The auto-renew command records the customer's renewal intent. It is a
// compare-and-set on the Workspace row and never charges, renews or expires
// anything by itself; the renewal execution engine that used to read this
// intent has been withdrawn.

type workspaceRenewalIntentCAS struct {
	WorkspaceID               string
	AccountID                 string
	OwnerUserID               string
	ExpectedPaidThrough       string
	ExpectedAutoRenew         bool
	ExpectedOperationsVersion string
	WorkspacePatch            workspaceRenewalIntentPatch
	CommandOperation          map[string]any
	AuditEvent                map[string]any
}

type workspaceRenewalIntentPatch struct {
	AutoRenew    bool
	AuthorizedBy string
	AuthorizedAt string
}

type workspaceAutoRenewCommandResult struct {
	RequestHash string         `json:"requestHash"`
	Response    map[string]any `json:"response"`
}

func workspaceAutoRenewCommandID(workspaceID, key string) string {
	return "workspace-renewal-intent-" + stableID(workspaceID, key)[:18]
}

func workspaceAutoRenewAuditID(commandID string) string {
	return "audit-" + stableID("workspace.auto_renew", commandID)[:12]
}

func workspaceRenewalIntentState(autoRenew bool, authorizedBy, authorizedAt string) map[string]any {
	return map[string]any{"autoRenew": autoRenew, "authorizedBy": authorizedBy, "authorizedAt": authorizedAt}
}

func bindWorkspaceAutoRenewAudit(command, event map[string]any) map[string]any {
	event = cloneMap(event)
	event["id"] = workspaceAutoRenewAuditID(stringValue(command["id"]))
	event["createdAt"] = command["createdAt"]
	return event
}

func validateWorkspaceRenewalIntentAudit(update workspaceRenewalIntentCAS, current map[string]any) error {
	event, command := update.AuditEvent, update.CommandOperation
	before := workspaceRenewalIntentState(current["autoRenew"] == true, stringValue(current["authorizedBy"]), stringValue(current["authorizedAt"]))
	after := workspaceRenewalIntentState(update.WorkspacePatch.AutoRenew, update.WorkspacePatch.AuthorizedBy, update.WorkspacePatch.AuthorizedAt)
	if stringValue(event["id"]) != workspaceAutoRenewAuditID(stringValue(command["id"])) ||
		stringValue(event["createdAt"]) == "" || stringValue(event["createdAt"]) != stringValue(command["createdAt"]) ||
		stringValue(event["actorUserId"]) != update.OwnerUserID || !validRole(stringValue(event["actorRole"])) ||
		stringValue(event["actorAccountId"]) != update.AccountID || stringValue(event["targetAccountId"]) != update.AccountID ||
		stringValue(event["action"]) != "workspace.auto_renew" || stringValue(event["resourceKind"]) != "workspace" ||
		stringValue(event["resourceId"]) != update.WorkspaceID || stringValue(event["result"]) != "succeeded" ||
		string(mustJSON(event["before"])) != string(mustJSON(before)) || string(mustJSON(event["after"])) != string(mustJSON(after)) {
		return errInvalidWorkspaceRenewalAudit
	}
	return nil
}

func workspaceRenewalIntentAuditIdentityMatches(current, desired map[string]any) bool {
	identity := func(row map[string]any) map[string]any {
		return map[string]any{
			"id": stringValue(row["id"]), "actorUserId": stringValue(row["actorUserId"]), "actorRole": stringValue(row["actorRole"]),
			"actorAccountId": stringValue(row["actorAccountId"]), "targetAccountId": stringValue(row["targetAccountId"]),
			"action": stringValue(row["action"]), "resourceKind": stringValue(row["resourceKind"]), "resourceId": stringValue(row["resourceId"]),
			"ipAddress": stringValue(row["ipAddress"]), "userAgent": stringValue(row["userAgent"]), "result": stringValue(row["result"]),
			"createdAt": stringValue(row["createdAt"]), "before": row["before"], "after": row["after"],
		}
	}
	return string(mustJSON(identity(current))) == string(mustJSON(identity(desired)))
}

func workspaceAutoRenewRequestHash(workspaceID string, autoRenew bool) string {
	return stableID("workspace-auto-renew-v1", workspaceID, strconv.FormatBool(autoRenew))
}

func workspaceAutoRenewCommandRow(workspace, user map[string]any, key, requestHash string, response map[string]any, now time.Time) map[string]any {
	result, _ := json.Marshal(workspaceAutoRenewCommandResult{RequestHash: requestHash, Response: response})
	id := workspaceAutoRenewCommandID(stringValue(workspace["id"]), key)
	return map[string]any{
		"id": id, "operationId": id, "accountId": stringValue(workspace["accountId"]), "workspaceId": stringValue(workspace["id"]),
		"resourceId": stringValue(workspace["id"]), "resourceKind": "workspace_renewal_intent", "action": "workspace.auto_renew",
		"status": "succeeded", "result": string(result), "createdAt": now.UTC().Format(time.RFC3339Nano),
	}
}

func decodeWorkspaceAutoRenewCommand(row map[string]any) (workspaceAutoRenewCommandResult, error) {
	var result workspaceAutoRenewCommandResult
	if stringValue(row["action"]) != "workspace.auto_renew" || json.Unmarshal([]byte(stringValue(row["result"])), &result) != nil || result.RequestHash == "" || len(result.Response) != 5 {
		return workspaceAutoRenewCommandResult{}, errors.New("invalid_workspace_auto_renew_operation")
	}
	return result, nil
}

func runtimeOperationsVersion(rows []map[string]any, workspaceID string) string {
	values := make([]string, 0)
	for _, row := range rows {
		if stringValue(row["workspaceId"]) != workspaceID {
			continue
		}
		values = append(values, string(mustJSON(map[string]any{
			"id": row["id"], "action": row["action"], "status": row["status"], "result": row["result"],
		})))
	}
	sort.Strings(values)
	return stableID(values...)
}

func currentWorkspaceRenewalOperation(rows []map[string]any, workspaceID, paidThrough string) map[string]any {
	for _, row := range rows {
		if stringValue(row["action"]) != "workspace.renewal" || stringValue(row["workspaceId"]) != workspaceID {
			continue
		}
		var result struct {
			PaidThrough string `json:"paidThrough"`
		}
		if json.Unmarshal([]byte(stringValue(row["result"])), &result) != nil {
			continue
		}
		parsed, err := time.Parse(time.RFC3339, result.PaidThrough)
		if err != nil || parsed.UTC().Format(time.RFC3339Nano) != paidThrough {
			continue
		}
		switch stringValue(row["status"]) {
		case "active", "cancelled", "expired_unpaid", "refunded":
			continue
		default:
			return row
		}
	}
	return nil
}

func workspaceAutoRenewResponse(workspace map[string]any, operations []map[string]any, autoRenew bool, now time.Time) (map[string]any, error) {
	paidThrough, err := time.Parse(time.RFC3339, stringValue(workspace["paidThrough"]))
	if err != nil {
		return nil, errInvalidWorkspaceBillingState
	}
	paidThrough = paidThrough.UTC()
	nextRenewal, err := time.Parse(time.RFC3339, stringValue(workspace["nextRenewalAt"]))
	if err != nil {
		return nil, errInvalidWorkspaceBillingState
	}
	renewalStatus := "scheduled"
	effectiveAfter := nextRenewal.UTC()
	operation := currentWorkspaceRenewalOperation(operations, stringValue(workspace["id"]), paidThrough.Format(time.RFC3339Nano))
	if autoRenew {
		if !now.UTC().Before(paidThrough) {
			effectiveAfter, renewalStatus = now.UTC(), "pending"
		} else if operation != nil {
			renewalStatus = stringValue(operation["status"])
		}
	} else {
		effectiveAfter, renewalStatus = paidThrough, "cancelled"
		if operation != nil {
			anchor := int(numberField(workspace, "billingAnchorDay", float64(paidThrough.Day())))
			effectiveAfter, renewalStatus = nextBillingMonth(paidThrough, anchor), stringValue(operation["status"])
		}
	}
	return map[string]any{
		"autoRenew": autoRenew, "effectiveAfter": effectiveAfter.Format(time.RFC3339Nano),
		"nextRenewalAt": nextRenewal.UTC().Format(time.RFC3339Nano), "paidThrough": paidThrough.Format(time.RFC3339Nano), "renewalStatus": renewalStatus,
	}, nil
}

func planWorkspaceRenewalIntent(workspace, user map[string]any, operations []map[string]any, autoRenew bool, key string, now time.Time) (workspaceRenewalIntentCAS, map[string]any, error) {
	response, err := workspaceAutoRenewResponse(workspace, operations, autoRenew, now)
	if err != nil {
		return workspaceRenewalIntentCAS{}, nil, err
	}
	patch := workspaceRenewalIntentPatch{AutoRenew: autoRenew}
	if autoRenew {
		patch.AuthorizedBy, patch.AuthorizedAt = stringValue(user["id"]), now.UTC().Format(time.RFC3339Nano)
	}
	requestHash := workspaceAutoRenewRequestHash(stringValue(workspace["id"]), autoRenew)
	return workspaceRenewalIntentCAS{
		WorkspaceID: stringValue(workspace["id"]), AccountID: stringValue(workspace["accountId"]), OwnerUserID: stringValue(user["id"]),
		ExpectedPaidThrough: stringValue(workspace["paidThrough"]), ExpectedAutoRenew: workspace["autoRenew"] == true,
		ExpectedOperationsVersion: runtimeOperationsVersion(operations, stringValue(workspace["id"])), WorkspacePatch: patch,
		CommandOperation: workspaceAutoRenewCommandRow(workspace, user, key, requestHash, response, now),
	}, response, nil
}
