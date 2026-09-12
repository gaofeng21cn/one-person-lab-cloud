package server

import (
	"context"
	"encoding/json"

	contracts "opl-cloud/packages/contracts/go"
	controlplaneent "opl-cloud/services/control-plane/ent"
	"opl-cloud/services/control-plane/ent/applicationrevision"
	"opl-cloud/services/control-plane/internal/domain/application"
)

// applicationRevisionMutation is one idempotent revision admission. The row
// identity is derived from the application identity; the payload is the
// canonical revision JSON whose digest was computed by the domain layer.
type applicationRevisionMutation struct {
	ApplicationID    string
	Version          string
	Digest           string
	Payload          string
	AdmittedByUserID string
}

func applicationRevisionRowID(applicationID, version string) string {
	return "application-revision-" + stableID(applicationID, version)
}

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

func (s *postgresEntStateStore) ApplyApplicationRevisionAdmission(ctx context.Context, mutation applicationRevisionMutation) (map[string]any, error) {
	existing, found, err := s.AdmittedApplicationRevision(ctx, mutation.ApplicationID, mutation.Version)
	if err != nil {
		return nil, err
	}
	if found {
		if stringValue(existing["digest"]) != mutation.Digest {
			return nil, application.ErrRevisionConflict
		}
		return existing, nil
	}
	row := map[string]any{
		"id":               applicationRevisionRowID(mutation.ApplicationID, mutation.Version),
		"applicationId":    mutation.ApplicationID,
		"version":          mutation.Version,
		"digest":           mutation.Digest,
		"payload":          mutation.Payload,
		"admittedByUserId": mutation.AdmittedByUserID,
	}
	if err := saveRecord(ctx, stringValue(row["id"]), row, s.client.ApplicationRevision.Create(), applicationRevisionEntFields); controlplaneent.IsConstraintError(err) {
		// A concurrent admission of the same identity won the unique index.
		raced, raceFound, raceErr := s.AdmittedApplicationRevision(ctx, mutation.ApplicationID, mutation.Version)
		if raceErr != nil {
			return nil, err
		}
		if !raceFound || stringValue(raced["digest"]) != mutation.Digest {
			return nil, application.ErrRevisionConflict
		}
		return raced, nil
	} else if err != nil {
		return nil, err
	}
	created, _, err := s.AdmittedApplicationRevision(ctx, mutation.ApplicationID, mutation.Version)
	return created, err
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
func (s *postgresEntStateStore) ClaimWorkspaceApplicationDeploymentIntent(ctx context.Context, row map[string]any) error {
	if err := saveRecord(ctx, stringValue(row["id"]), row, s.client.RuntimeOperation.Create(), runtimeOpEntFields); controlplaneent.IsConstraintError(err) {
		return errWorkspaceApplicationIntentConflict
	} else if err != nil {
		return err
	}
	return nil
}
