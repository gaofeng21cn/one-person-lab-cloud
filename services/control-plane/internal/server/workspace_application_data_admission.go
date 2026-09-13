package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/domain/application"
)

var errApplicationDataMaterialPayloadInvalid = errors.New("workspace_application_data_material_payload_invalid")

// admitApplicationDataMaterial admits one data material idempotently. The
// first admission stores the canonical payload; an exact replay returns the
// stored row; different content under an admitted identity is a conflict and
// never rewrites the stored material.
func (app *controlPlaneServer) admitApplicationDataMaterial(ctx context.Context, material contracts.WorkspaceApplicationDataMaterial, admittedByUserID string) (map[string]any, application.AdmissionDecision, error) {
	payload, err := json.Marshal(material)
	if err != nil {
		return nil, "", err
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(payload))
	existing, found, err := app.tables.AdmittedApplicationDataMaterial(ctx, material.ApplicationID, material.Version)
	if err != nil {
		return nil, "", err
	}
	if found {
		admitted, ok := decodeApplicationDataMaterialPayload(stringValue(existing["payload"]))
		if !ok {
			return nil, "", errApplicationDataMaterialPayloadInvalid
		}
		decision, err := application.DecideDataMaterialAdmission(&admitted, material)
		if err != nil {
			return nil, "", err
		}
		return existing, decision, nil
	}
	row, err := app.tables.ApplyApplicationDataMaterialAdmission(ctx, applicationDataMaterialMutation{
		ApplicationID: material.ApplicationID, Version: material.Version,
		Digest: digest, Payload: string(payload), AdmittedByUserID: admittedByUserID,
	})
	if err != nil {
		return nil, "", err
	}
	return row, application.AdmissionNew, nil
}

func registerApplicationDataMaterialRoutes(mux *http.ServeMux, app *controlPlaneServer) {
	mux.HandleFunc("POST /api/operator/application-data-materials", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
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
			writeError(w, http.StatusBadRequest, "invalid_application_data_material")
			return
		}
		var material contracts.WorkspaceApplicationDataMaterial
		if json.Unmarshal(encoded, &material) != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_data_material")
			return
		}
		if err := contracts.ValidateWorkspaceApplicationDataMaterial(material); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_application_data_material")
			return
		}
		row, decision, err := app.admitApplicationDataMaterial(r.Context(), material, stringValue(user["id"]))
		if err != nil {
			if errors.Is(err, application.ErrDataMaterialConflict) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "state_persist_failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"decision": string(decision), "dataMaterial": row})
	}))

	mux.HandleFunc("GET /api/operator/application-data-materials/{applicationId}/{version}", app.protected(true, func(w http.ResponseWriter, r *http.Request) {
		row, found, err := app.tables.AdmittedApplicationDataMaterial(r.Context(), r.PathValue("applicationId"), r.PathValue("version"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "state_read_failed")
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "workspace_application_data_material_not_found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"decision": string(application.AdmissionIdentical), "dataMaterial": row})
	}))
}
