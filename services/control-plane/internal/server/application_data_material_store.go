package server

import (
	"context"
	"encoding/json"

	contracts "opl-cloud/packages/contracts/go"
	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/applicationdatamaterial"
	"opl-cloud/services/control-plane/internal/domain/application"
)

// applicationDataMaterialMutation is one idempotent data material admission.
// The payload is the canonical material JSON whose digest was computed by the
// admission flow; the restore flow later consumes exactly this payload.
type applicationDataMaterialMutation struct {
	ApplicationID    string
	Version          string
	Digest           string
	Payload          string
	AdmittedByUserID string
}

func applicationDataMaterialRowID(applicationID, version string) string {
	return "application-data-material-" + stableID(applicationID, version)
}

func applicationDataMaterialRecordFromEnt(entity *controlplaneent.ApplicationDataMaterial) map[string]any {
	if entity == nil {
		return nil
	}
	return recordFromEnt(entity, applicationDataMaterialEntFields)
}

func decodeApplicationDataMaterialPayload(payload string) (contracts.WorkspaceApplicationDataMaterial, bool) {
	var material contracts.WorkspaceApplicationDataMaterial
	if json.Unmarshal([]byte(payload), &material) != nil {
		return contracts.WorkspaceApplicationDataMaterial{}, false
	}
	return material, true
}

func (s *postgresEntStateStore) AdmittedApplicationDataMaterial(ctx context.Context, applicationID, version string) (map[string]any, bool, error) {
	entity, err := s.client.ApplicationDataMaterial.Query().Where(
		applicationdatamaterial.ApplicationIDEQ(applicationID),
		applicationdatamaterial.VersionEQ(version),
	).Only(ctx)
	if controlplaneent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return applicationDataMaterialRecordFromEnt(entity), true, nil
}

func (s *postgresEntStateStore) ApplyApplicationDataMaterialAdmission(ctx context.Context, mutation applicationDataMaterialMutation) (map[string]any, error) {
	existing, found, err := s.AdmittedApplicationDataMaterial(ctx, mutation.ApplicationID, mutation.Version)
	if err != nil {
		return nil, err
	}
	if found {
		if stringValue(existing["digest"]) != mutation.Digest {
			return nil, application.ErrDataMaterialConflict
		}
		return existing, nil
	}
	row := map[string]any{
		"id":               applicationDataMaterialRowID(mutation.ApplicationID, mutation.Version),
		"applicationId":    mutation.ApplicationID,
		"version":          mutation.Version,
		"digest":           mutation.Digest,
		"payload":          mutation.Payload,
		"admittedByUserId": mutation.AdmittedByUserID,
	}
	if err := saveRecord(ctx, stringValue(row["id"]), row, s.client.ApplicationDataMaterial.Create(), applicationDataMaterialEntFields); controlplaneent.IsConstraintError(err) {
		raced, racedFound, raceErr := s.AdmittedApplicationDataMaterial(ctx, mutation.ApplicationID, mutation.Version)
		if raceErr != nil {
			return nil, err
		}
		if !racedFound || stringValue(raced["digest"]) != mutation.Digest {
			return nil, application.ErrDataMaterialConflict
		}
		return raced, nil
	} else if err != nil {
		return nil, err
	}
	created, _, err := s.AdmittedApplicationDataMaterial(ctx, mutation.ApplicationID, mutation.Version)
	return created, err
}
