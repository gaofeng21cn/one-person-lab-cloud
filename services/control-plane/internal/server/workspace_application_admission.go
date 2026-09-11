package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/domain/application"
)

var errApplicationRevisionPayloadInvalid = errors.New("workspace_application_revision_payload_invalid")

// admitWorkspaceApplicationRevision admits one application revision
// idempotently. The first admission stores the canonical payload; an exact
// replay returns the stored row; different content under an admitted identity
// is a conflict and never rewrites the stored revision.
func (app *controlPlaneServer) admitWorkspaceApplicationRevision(ctx context.Context, revision contracts.WorkspaceApplicationRevision, admittedByUserID string) (map[string]any, application.AdmissionDecision, error) {
	digest, err := application.RevisionDigest(revision)
	if err != nil {
		return nil, "", err
	}
	existing, found, err := app.tables.AdmittedApplicationRevision(ctx, revision.ApplicationID, revision.Version)
	if err != nil {
		return nil, "", err
	}
	if found {
		admitted, ok := decodeApplicationRevisionPayload(stringValue(existing["payload"]))
		if !ok {
			return nil, "", errApplicationRevisionPayloadInvalid
		}
		decision, err := application.DecideRevisionAdmission(&admitted, revision)
		if err != nil {
			return nil, "", err
		}
		return existing, decision, nil
	}
	payload, err := json.Marshal(revision)
	if err != nil {
		return nil, "", err
	}
	row, err := app.tables.ApplyApplicationRevisionAdmission(ctx, applicationRevisionMutation{
		ApplicationID: revision.ApplicationID, Version: revision.Version,
		Digest: digest, Payload: string(payload), AdmittedByUserID: admittedByUserID,
	})
	if err != nil {
		return nil, "", err
	}
	return row, application.AdmissionNew, nil
}

func registerApplicationRevisionRoutes(mux *http.ServeMux, app *controlPlaneServer) {
	mux.HandleFunc("POST /api/operator/application-revisions", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		input := decodeJSON(r)
		if _, ok := requiredMutationKey(w, r); !ok {
			return
		}
		user, ok := app.sessionUserContext(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "not_authenticated")
			return
		}
		encoded, err := json.Marshal(input)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_revision")
			return
		}
		var revision contracts.WorkspaceApplicationRevision
		if json.Unmarshal(encoded, &revision) != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_revision")
			return
		}
		if err := contracts.ValidateWorkspaceApplicationRevision(revision); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_revision")
			return
		}
		row, decision, err := app.admitWorkspaceApplicationRevision(r.Context(), revision, stringValue(user["id"]))
		if err != nil {
			if errors.Is(err, application.ErrRevisionConflict) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"decision": string(decision), "revision": row})
	}))

	mux.HandleFunc("GET /api/operator/application-revisions/{applicationId}/{version}", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		row, found, err := app.tables.AdmittedApplicationRevision(r.Context(), r.PathValue("applicationId"), r.PathValue("version"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "workspace_application_revision_not_found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"decision": string(application.AdmissionIdentical), "revision": row})
	}))
}
