package server

import (
	"context"

	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/applicationdatamaterial"
)

func applicationDataMaterialRecordFromEnt(entity *controlplaneent.ApplicationDataMaterial) map[string]any {
	if entity == nil {
		return nil
	}
	return recordFromEnt(entity, applicationDataMaterialEntFields)
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
