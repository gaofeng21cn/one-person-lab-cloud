package server

import (
	"context"
	"encoding/json"
	"errors"

	contracts "opl-cloud/packages/contracts/go"
	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/applicationrevision"
)

var errApplicationRevisionPayloadInvalid = errors.New("workspace_application_revision_payload_invalid")

var applicationRevisionEntFields = []entRecordField{
	textField("ApplicationID", "SetApplicationID", "applicationId"),
	textField("Version", "SetVersion", "version"),
	textField("Digest", "SetDigest", "digest"),
	textField("Payload", "SetPayload", "payload"),
	textField("AdmittedByUserID", "SetAdmittedByUserID", "admittedByUserId"),
}

func applicationRevisionRecordFromEnt(entity *controlplaneent.ApplicationRevision) map[string]any {
	if entity == nil {
		return nil
	}
	return recordFromEnt(entity, applicationRevisionEntFields)
}

func (s *postgresEntStateStore) AdmittedApplicationRevision(ctx context.Context, applicationID, version string) (map[string]any, bool, error) {
	entity, err := s.client.ApplicationRevision.Query().Where(
		applicationrevision.ApplicationIDEQ(applicationID),
		applicationrevision.VersionEQ(version),
	).Only(ctx)
	if controlplaneent.IsNotFound(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return applicationRevisionRecordFromEnt(entity), true, nil
}

func decodeApplicationRevisionPayload(payload string) (contracts.WorkspaceApplicationRevision, bool) {
	var revision contracts.WorkspaceApplicationRevision
	if json.Unmarshal([]byte(payload), &revision) != nil {
		return contracts.WorkspaceApplicationRevision{}, false
	}
	return revision, true
}

// ClaimWorkspaceApplicationDeploymentIntent creates the deployment intent's
// runtime-operation row if the identity is free. An existing row means a
// concurrent claim won; the caller compares the persisted request hash for the
// idempotent replay decision.
