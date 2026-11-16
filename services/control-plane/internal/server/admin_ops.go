package server

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type auditActorContextKey struct{}

type auditActor struct {
	UserID    string
	Role      string
	AccountID string
}

const (
	maxAuditIPAddressBytes = 64
	maxAuditUserAgentBytes = 512
)

func findRecord(rows []map[string]any, id string) map[string]any {
	for _, row := range rows {
		if stringValue(row["id"]) == id {
			return row
		}
	}
	return nil
}

func (app *controlPlaneServer) managementState(includeDeleted bool, computePools []any) map[string]any {
	app.mu.Lock()
	defer app.mu.Unlock()
	return map[string]any{
		"users":                  sanitizedUserValues(app.userRecordSet(includeDeleted), includeDeleted),
		"accounts":               app.accountsLocked(""),
		"packages":               packageList(computePools),
		"computePools":           computePools,
		"workspaces":             rowsAsAnyFromMaps(app.listWorkspaces("")),
		"computeAllocations":     rowsAsAnyFromMaps(app.listComputes("")),
		"storageVolumes":         rowsAsAnyFromMaps(app.listStorages("")),
		"storageAttachments":     rowsAsAnyFromMaps(app.listAttachments("")),
		"resourceLedgerEvidence": app.resourceLedgerEvidenceLocked(),
		"runtimeOperations":      rowsAsAnyFromMaps(app.runtimeOperationRows(runtimeOperationQuery{})),
		"auditEvents":            rowsAsAnyFromMaps(app.listAuditEvents("")),
		"billingReconciliation":  app.reconciliationProjectionLocked(),
		"archive":                app.archiveStateLocked(),
		"retentionPolicy":        currentRetentionPolicy().dto(),
	}
}

func (app *controlPlaneServer) appendAuditEvent(r *http.Request, action string, resourceKind string, resourceID string, targetAccountID string, before any, after any, result string) error {
	return app.tables.SaveAuditEvent(r.Context(), app.auditEvent(r, action, resourceKind, resourceID, targetAccountID, before, after, result))
}

func (app *controlPlaneServer) appendBillingReviewResolutionAudit(r *http.Request, key string, result map[string]any) error {
	action, resourceType, resourceID := "billing.review.resolve", "workspace", stringValue(result["resourceId"])
	event := app.auditEvent(r, action, resourceType, resourceID, stringValue(result["accountId"]), nil, result, "succeeded")
	event["id"] = "audit-" + stableID(action, resourceType, resourceID, stringValue(result["billingOperationId"]), key)[:12]
	event["createdAt"] = result["resolvedAt"]
	return app.tables.SaveAuditEvent(r.Context(), event)
}

func (app *controlPlaneServer) auditEvent(r *http.Request, action string, resourceKind string, resourceID string, targetAccountID string, before any, after any, result string) map[string]any {
	user, _ := app.sessionUserContext(r)
	actor := auditActor{UserID: stringValue(user["id"]), Role: stringValue(user["role"]), AccountID: stringValue(user["accountId"])}
	if systemActor, ok := r.Context().Value(auditActorContextKey{}).(auditActor); ok {
		actor = systemActor
	}
	now := time.Now().UTC().Format(time.RFC3339)
	event := map[string]any{
		"id":              "audit-" + stableID(action, resourceKind, resourceID, now)[:12],
		"actorUserId":     actor.UserID,
		"actorRole":       actor.Role,
		"actorAccountId":  actor.AccountID,
		"targetAccountId": targetAccountID,
		"action":          action,
		"resourceKind":    resourceKind,
		"resourceId":      resourceID,
		"ipAddress":       boundedAuditText(requestIP(r), maxAuditIPAddressBytes),
		"userAgent":       boundedAuditText(r.UserAgent(), maxAuditUserAgentBytes),
		"before":          before,
		"after":           after,
		"result":          result,
		"createdAt":       now,
	}
	return event
}

func boundedAuditText(value string, maxBytes int) string {
	value = strings.ToValidUTF8(value, "")
	if len(value) <= maxBytes {
		return value
	}
	for maxBytes > 0 && !utf8.ValidString(value[:maxBytes]) {
		maxBytes--
	}
	return value[:maxBytes]
}
func (app *controlPlaneServer) accountsLocked(accountID string) []any {
	accounts, err := app.tables.ListAccounts(context.Background(), accountID)
	if err != nil {
		return []any{}
	}
	sort.Slice(accounts, func(i, j int) bool { return stringValue(accounts[i]["id"]) < stringValue(accounts[j]["id"]) })
	return rowsAsAnyFromMaps(accounts)
}

type retentionStore interface {
	ApplyRetention(ctx context.Context, policy retentionPolicy) (map[string]any, error)
}

func (app *controlPlaneServer) archiveStateLocked() map[string]any {
	return map[string]any{
		"adminAuditEvents": []any{},
		"productionE2E":    productionE2ESummary(nil),
		"retentionPolicy":  currentRetentionPolicy().dto(),
	}
}

func (app *controlPlaneServer) applyRetention(ctx context.Context) (map[string]any, error) {
	if store, ok := app.store.(retentionStore); ok {
		return store.ApplyRetention(ctx, currentRetentionPolicy())
	}
	return map[string]any{"retentionPolicy": currentRetentionPolicy().dto()}, nil
}
