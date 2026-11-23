package server

import (
	"context"
	"encoding/json"
	"errors"

	contracts "opl-cloud/packages/contracts/go"
)

var errApplicationRevisionPayloadInvalid = errors.New("workspace_application_revision_payload_invalid")

var applicationRevisionEntFields = []sqlRecordField{
	sqlTextField("application_id", "applicationId"),
	sqlTextField("version", "version"),
	sqlTextField("digest", "digest"),
	sqlTextField("payload", "payload"),
	sqlTextField("admitted_by_user_id", "admittedByUserId"),
}

func (s *postgresEntStateStore) AdmittedApplicationRevision(ctx context.Context, applicationID, version string) (map[string]any, bool, error) {
	rows, err := s.listSQLRecords(ctx, s.db, "control_plane_application_revisions", applicationRevisionEntFields, "WHERE application_id = ? AND version = ?", applicationID, version)
	if err != nil {
		return nil, false, err
	}
	if len(rows) == 0 {
		return nil, false, nil
	}
	if len(rows) > 1 {
		return nil, false, errIdempotencyConflict
	}
	return rows[0], true, nil
}

func decodeApplicationRevisionPayload(payload string) (contracts.WorkspaceApplicationRevision, bool) {
	var revision contracts.WorkspaceApplicationRevision
	if json.Unmarshal([]byte(payload), &revision) != nil {
		return contracts.WorkspaceApplicationRevision{}, false
	}
	return revision, true
}
