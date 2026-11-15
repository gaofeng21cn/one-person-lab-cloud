package server

import (
	"context"
	"net/http"
	"time"
)

func (app *controlPlaneServer) saveComputeFact(allocation any) error {
	if row, ok := allocation.(map[string]any); ok {
		if isTerminalResourceStatus(stringValue(row["status"])) {
			if err := app.suspendWorkspacesForCompute(stringValue(row["id"])); err != nil {
				return err
			}
		}
		return app.tables.SaveCompute(context.Background(), row)
	}
	return nil
}

func (app *controlPlaneServer) saveStorageFact(volume any) error {
	if row, ok := volume.(map[string]any); ok {
		if isTerminalResourceStatus(stringValue(row["status"])) {
			if err := app.markWorkspacesStorageDestroyed(stringValue(row["id"])); err != nil {
				return err
			}
		}
		return app.tables.SaveStorage(context.Background(), row)
	}
	return nil
}
func providerSyncFacts(row map[string]any, err error) map[string]any {
	out := cloneMap(row)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	out["lastProviderSyncAt"] = now
	if err != nil {
		out["providerStatus"] = "sync_failed"
		out["lastProviderSyncError"] = customerSafeProviderError(err)
		return out
	}
	status := firstNonEmpty(stringValue(out["providerStatus"]), stringValue(out["status"]))
	out["lastProviderSyncError"] = ""
	if isExternallyDeletedStatus(status) {
		out["providerStatus"] = "missing"
		out["externalDeletedAt"] = firstNonEmpty(stringValue(out["externalDeletedAt"]), now)
		return out
	}
	out["providerStatus"] = firstNonEmpty(status, "unknown")
	return out
}

func isExternallyDeletedStatus(status string) bool {
	return status == "external_deleted" || status == "deleted" || status == "missing"
}

func customerSafeProviderError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (app *controlPlaneServer) resourceBelongsToAccount(row map[string]any, accountID string) bool {
	if accountID == "" {
		return false
	}
	return firstNonEmpty(stringValue(row["accountId"]), stringValue(row["ownerAccountId"])) == accountID
}

func (app *controlPlaneServer) getCompute(id string) (map[string]any, bool) {
	for _, compute := range app.listComputes("") {
		if stringValue(compute["id"]) == id {
			return cloneMap(compute), true
		}
	}
	return nil, false
}

func (app *controlPlaneServer) getStorage(id string) (map[string]any, bool) {
	for _, storage := range app.listStorages("") {
		if stringValue(storage["id"]) == id {
			return cloneMap(storage), true
		}
	}
	return nil, false
}

func (app *controlPlaneServer) getAttachment(id string) (map[string]any, bool) {
	for _, attachment := range app.listAttachments("") {
		if stringValue(attachment["id"]) == id {
			return cloneMap(attachment), true
		}
	}
	return nil, false
}

func (app *controlPlaneServer) canAccessResource(r *http.Request, row map[string]any) bool {
	user, ok := app.sessionUserContext(r)
	if !ok {
		return false
	}
	return app.resourceBelongsToAccount(row, stringValue(user["accountId"]))
}
