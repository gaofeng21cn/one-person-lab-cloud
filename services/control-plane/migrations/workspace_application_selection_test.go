package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func newWorkspaceApplicationSelectionMigrationDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db := newIdentityMigrationDatabase(t, requiredIdentityTestDatabaseURL(t))
	if _, err := db.Exec(`CREATE TABLE control_plane_workspaces (
        id TEXT PRIMARY KEY, application_binding TEXT NOT NULL, application_binding_version BIGINT NOT NULL
    ); CREATE TABLE control_plane_runtime_operations (
        id TEXT PRIMARY KEY, workspace_id TEXT NOT NULL, action TEXT NOT NULL, result TEXT NOT NULL
    ); INSERT INTO control_plane_workspaces VALUES ('ws-alpha', 'old-app@1', 1)`); err != nil {
		t.Fatal(err)
	}
	return db
}

// These are retained persistence payloads consumed directly by SQL. Their
// complete original bytes must survive the selection/reservation migration.
func insertRetainedApplicationOperation(t *testing.T, db *sql.DB, id, applicationID, phase, binding string, expectedVersion int64) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"schemaVersion": 1, "version": 1, "operationId": id, "accountId": "acct-alpha", "workspaceId": "ws-alpha",
		"applicationId": applicationID, "targetRevision": "1", "currentBinding": binding,
		"expectedWorkspaceVersion": expectedVersion, "phase": phase, "configurationDigest": "retained-configuration",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO control_plane_runtime_operations VALUES ($1, 'ws-alpha', 'workspace.application.deploy', $2)`, id, string(payload)); err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestWorkspaceApplicationSelectionMigrationPostgresPreservesReservations(t *testing.T) {
	for _, phase := range []string{"active", "receipt", "intent", "runtime", "activating", "manual_review"} {
		t.Run(phase, func(t *testing.T) {
			db := newWorkspaceApplicationSelectionMigrationDatabase(t)
			selectedPhase := "active"
			if phase == "receipt" {
				selectedPhase = phase
			}
			selectedPayload := insertRetainedApplicationOperation(t, db, "selected", "old-app", selectedPhase, "empty", 0)
			wantReservation := "selected"
			var pendingPayload string
			if phase != "active" && phase != "receipt" {
				pendingPayload = insertRetainedApplicationOperation(t, db, "pending", "new-app", phase, "old-app@1", 1)
				wantReservation = "pending"
			}
			insertRetainedApplicationOperation(t, db, "stale-version", "stale-app", "runtime", "old-app@1", 0)
			insertRetainedApplicationOperation(t, db, "stale-binding", "stale-app", "manual_review", "different-app@1", 1)
			for range 2 {
				if err := ApplyWorkspaceApplicationSelection(context.Background(), entsql.OpenDB(dialect.Postgres, db)); err != nil {
					t.Fatal(err)
				}
			}
			var selected, reserved string
			if err := db.QueryRow(`SELECT current_application_deployment_id, reserved_application_deployment_id FROM control_plane_workspaces WHERE id='ws-alpha'`).Scan(&selected, &reserved); err != nil {
				t.Fatal(err)
			}
			if selected != "selected" || reserved != wantReservation {
				t.Fatalf("selected=%q reserved=%q want=%q", selected, reserved, wantReservation)
			}
			for id, want := range map[string]string{"selected": selectedPayload, "pending": pendingPayload} {
				if want == "" {
					continue
				}
				var retained string
				if err := db.QueryRow(`SELECT result FROM control_plane_runtime_operations WHERE id=$1`, id).Scan(&retained); err != nil || retained != want {
					t.Fatalf("retained %s changed: err=%v", id, err)
				}
			}
		})
	}
}

func TestWorkspaceApplicationSelectionMigrationPostgresReservesEmptyWorkspace(t *testing.T) {
	db := newWorkspaceApplicationSelectionMigrationDatabase(t)
	if _, err := db.Exec(`UPDATE control_plane_workspaces SET application_binding='empty', application_binding_version=0`); err != nil {
		t.Fatal(err)
	}
	insertRetainedApplicationOperation(t, db, "pending", "new-app", "runtime", "empty", 0)
	if err := ApplyWorkspaceApplicationSelection(context.Background(), entsql.OpenDB(dialect.Postgres, db)); err != nil {
		t.Fatal(err)
	}
	var current, reserved string
	if err := db.QueryRow(`SELECT current_application_deployment_id, reserved_application_deployment_id FROM control_plane_workspaces`).Scan(&current, &reserved); err != nil {
		t.Fatal(err)
	}
	if current != "" || reserved != "pending" {
		t.Fatalf("empty Workspace selected=%q reserved=%q", current, reserved)
	}
}

func TestWorkspaceApplicationSelectionMigrationPostgresRejectsAmbiguousReservation(t *testing.T) {
	db := newWorkspaceApplicationSelectionMigrationDatabase(t)
	insertRetainedApplicationOperation(t, db, "selected", "old-app", "active", "empty", 0)
	insertRetainedApplicationOperation(t, db, "pending-a", "next-app", "runtime", "old-app@1", 1)
	insertRetainedApplicationOperation(t, db, "pending-b", "other-app", "manual_review", "old-app@1", 1)
	err := ApplyWorkspaceApplicationSelection(context.Background(), entsql.OpenDB(dialect.Postgres, db))
	if err == nil || !strings.Contains(err.Error(), "workspace_application_reservation_unresolvable") {
		t.Fatalf("ambiguous reservation migration err=%v", err)
	}
	var binding string
	if err := db.QueryRow(`SELECT application_binding FROM control_plane_workspaces`).Scan(&binding); err != nil || binding != "old-app@1" {
		t.Fatalf("failed migration rewrote binding: %q %v", binding, err)
	}
}

func TestWorkspaceApplicationSelectionMigrationPostgresKeepsNewOwnerReservationOnReplay(t *testing.T) {
	db := newWorkspaceApplicationSelectionMigrationDatabase(t)
	insertRetainedApplicationOperation(t, db, "selected", "old-app", "active", "empty", 0)
	if err := ApplyWorkspaceApplicationSelection(context.Background(), entsql.OpenDB(dialect.Postgres, db)); err != nil {
		t.Fatal(err)
	}
	insertRetainedApplicationOperation(t, db, "new-v2-owner", "new-app", "intent", "old-app@1", 1)
	if _, err := db.Exec(`UPDATE control_plane_runtime_operations SET result=jsonb_set(result::jsonb, '{version}', '2')::text WHERE id='new-v2-owner'; UPDATE control_plane_workspaces SET reserved_application_deployment_id='new-v2-owner'`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyWorkspaceApplicationSelection(context.Background(), entsql.OpenDB(dialect.Postgres, db)); err != nil {
		t.Fatal(err)
	}
	var reserved string
	if err := db.QueryRow(`SELECT reserved_application_deployment_id FROM control_plane_workspaces`).Scan(&reserved); err != nil || reserved != "new-v2-owner" {
		t.Fatalf("new reservation overwritten: %q %v", reserved, err)
	}
}
