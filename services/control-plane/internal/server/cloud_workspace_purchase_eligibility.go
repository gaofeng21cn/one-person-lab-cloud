package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
)

// Workspace consumes the original Account fact and owns new-purchase admission.
// The session is the only identity selector; this endpoint never creates an Account.
func (app *controlPlaneServer) cloudWorkspacePurchaseEligibility(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private, no-store")
	if app.cloudAnnouncements == nil || app.cloudAnnouncements.workspaceToken == "" {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
		writeError(w, http.StatusBadRequest, "invalid_workspace_purchase_eligibility_request")
		return
	}
	proof, ok := app.cloudSessionProofForCaller(w, r, owneridentity.Workspace.Service())
	if !ok {
		return
	}
	identity, err := app.cloudUserLineage(ctx, proof)
	if err != nil {
		switch {
		case errors.Is(err, errCloudUserInactive):
			writeError(w, http.StatusForbidden, "workspace_purchase_account_inactive")
		case errors.Is(err, errCloudUserLineage):
			writeError(w, http.StatusConflict, "workspace_purchase_identity_lineage_invalid")
		default:
			writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		}
		return
	}
	if identity.legacyAccountID == "" {
		writeError(w, http.StatusForbidden, "workspace_purchase_account_not_found")
		return
	}
	// Missing, null, numeric or textual values are unknown, never permission.
	enabled, known := identity.legacyAccount["workspacePurchaseEnabled"].(bool)
	if !known {
		writeSourceEnvelope(w, http.StatusServiceUnavailable, "control-plane", "unavailable", nil)
		return
	}
	writeJSON(w, http.StatusOK, contracts.WorkspacePurchaseEligibility{
		ActorID: proof.GetActorId(), AccountID: identity.legacyAccountID, WorkspacePurchaseEnabled: &enabled,
	})
}
