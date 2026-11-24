package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestApplyCustomerIdentityHardCutContainsFailClosedOneToOneContract(t *testing.T) {
	driver := &recordingDriver{}
	if err := ApplyCustomerIdentityHardCut(context.Background(), driver); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"legacy account/user cardinality is not one-to-one",
		"legacy account owner is cross-account or missing",
		"legacy customer organization cardinality is not one-to-one",
		"legacy customer membership is not one-to-one",
		"LOCK TABLE control_plane_accounts, control_plane_users, control_plane_organizations, control_plane_memberships, control_plane_sessions IN ACCESS EXCLUSIVE MODE",
		"SET password_hash = ''",
		"DELETE FROM control_plane_sessions",
		"control_plane_users_account_id_unique",
		"control_plane_accounts_owner_user_id_unique",
		"control_plane_organizations_billing_account_id_unique",
		"control_plane_memberships_account_id_unique",
		"control_plane_memberships_user_id_unique",
		"control_plane_memberships_organization_id_unique",
		"CHECK (sub2api_user_id > 0)",
		"control_plane_users_email_canonical",
		"control_plane_users_password_hash_empty",
		"control_plane_memberships_owner_role",
		"control_plane_sessions_sub2api_id",
		"FOREIGN KEY (owner_user_id, id) REFERENCES control_plane_users(id, account_id)",
		"FOREIGN KEY (user_id, account_id) REFERENCES control_plane_users(id, account_id)",
		"FOREIGN KEY (organization_id, account_id) REFERENCES control_plane_organizations(id, billing_account_id)",
		"DEFERRABLE INITIALLY DEFERRED",
	} {
		if !strings.Contains(driver.query, required) {
			t.Fatalf("embedded customer identity migration missing %q", required)
		}
	}
}

func TestCustomerIdentityHardCutPostgresFailsClosedWithoutMutation(t *testing.T) {
	databaseURL := requiredIdentityTestDatabaseURL(t)
	for _, tc := range []struct {
		name   string
		mutate string
	}{
		{name: "second user", mutate: `INSERT INTO control_plane_users VALUES ('usr-two','acct-one','two@example.com','owner','active','second-hash')`},
		{name: "cross account owner", mutate: `UPDATE control_plane_accounts SET owner_user_id = 'usr-missing' WHERE id = 'acct-one'`},
		{name: "nonpositive remote mapping", mutate: `UPDATE control_plane_accounts SET sub2api_user_id = 0 WHERE id = 'acct-one'`},
		{name: "second organization", mutate: `INSERT INTO control_plane_organizations VALUES ('org-two','acct-one','active')`},
		{name: "second membership", mutate: `INSERT INTO control_plane_memberships VALUES ('mem-two','acct-one','org-one','usr-one','owner','active')`},
		{name: "membership mismatch", mutate: `UPDATE control_plane_memberships SET user_id = 'usr-missing' WHERE id = 'mem-one'`},
		{name: "nonowner customer", mutate: `UPDATE control_plane_users SET role = 'member' WHERE id = 'usr-one'`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newIdentityMigrationDatabase(t, databaseURL)
			seedIdentityMigrationTables(t, db, "usr-one")
			if _, err := db.Exec(tc.mutate); err != nil {
				t.Fatal(err)
			}
			var beforeOwner, beforeHash string
			var beforeSessions int
			if err := db.QueryRow(`SELECT owner_user_id FROM control_plane_accounts WHERE id='acct-one'`).Scan(&beforeOwner); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT password_hash FROM control_plane_users WHERE id='usr-one'`).Scan(&beforeHash); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT count(*) FROM control_plane_sessions`).Scan(&beforeSessions); err != nil {
				t.Fatal(err)
			}
			if err := ApplyCustomerIdentityHardCut(context.Background(), OpenDriver(db)); err == nil {
				t.Fatal("invalid legacy identity was accepted")
			}
			var afterOwner, afterHash string
			var afterSessions int
			_ = db.QueryRow(`SELECT owner_user_id FROM control_plane_accounts WHERE id='acct-one'`).Scan(&afterOwner)
			_ = db.QueryRow(`SELECT password_hash FROM control_plane_users WHERE id='usr-one'`).Scan(&afterHash)
			_ = db.QueryRow(`SELECT count(*) FROM control_plane_sessions`).Scan(&afterSessions)
			if beforeOwner != afterOwner || beforeHash != afterHash || beforeSessions != afterSessions {
				t.Fatalf("failed migration mutated owner/hash/sessions: %q/%q/%d -> %q/%q/%d", beforeOwner, beforeHash, beforeSessions, afterOwner, afterHash, afterSessions)
			}
		})
	}
}

func TestCustomerIdentityHardCutPostgresBackfillsClearsSecretsAndEnforcesConstraints(t *testing.T) {
	db := newIdentityMigrationDatabase(t, requiredIdentityTestDatabaseURL(t))
	seedIdentityMigrationTables(t, db, "")
	driver := OpenDriver(db)
	for range 2 {
		if err := ApplyCustomerIdentityHardCut(context.Background(), driver); err != nil {
			t.Fatal(err)
		}
	}
	var owner, passwordHash string
	var sessions int
	if err := db.QueryRow(`SELECT owner_user_id FROM control_plane_accounts WHERE id='acct-one'`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT password_hash FROM control_plane_users WHERE id='usr-one'`).Scan(&passwordHash); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM control_plane_sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if owner != "usr-one" || passwordHash != "" || sessions != 0 {
		t.Fatalf("owner=%q passwordHash=%q sessions=%d", owner, passwordHash, sessions)
	}

	for name, statement := range map[string]string{
		"user account unique":         `INSERT INTO control_plane_users VALUES ('usr-two','acct-one','two@example.com','owner','active','')`,
		"account owner unique":        `INSERT INTO control_plane_accounts VALUES ('acct-two','usr-one',42,'active')`,
		"remote mapping unique":       `INSERT INTO control_plane_accounts VALUES ('acct-two','usr-two',41,'active')`,
		"remote mapping positive":     `UPDATE control_plane_accounts SET sub2api_user_id=0 WHERE id='acct-one'`,
		"organization account unique": `INSERT INTO control_plane_organizations VALUES ('org-two','acct-one','active')`,
		"membership account unique":   `INSERT INTO control_plane_memberships VALUES ('mem-two','acct-one','org-two','usr-two','owner','active')`,
		"customer owner role":         `UPDATE control_plane_users SET role='member' WHERE id='usr-one'`,
		"canonical email":             `UPDATE control_plane_users SET email=' Owner@Example.com ' WHERE id='usr-one'`,
		"local password truth":        `UPDATE control_plane_users SET password_hash='new-secret-hash' WHERE id='usr-one'`,
		"membership owner role":       `UPDATE control_plane_memberships SET role='member' WHERE id='mem-one'`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := db.Exec(statement); err == nil {
				t.Fatal("database constraint accepted conflicting identity")
			}
		})
	}
}

func TestCustomerIdentityHardCutPostgresRejectsOldSessionInsertAcrossMigrationWindow(t *testing.T) {
	db := newIdentityMigrationDatabase(t, requiredIdentityTestDatabaseURL(t))
	seedIdentityMigrationTables(t, db, "")
	const lockKey int64 = 180001
	if _, err := db.Exec(fmt.Sprintf(`
		CREATE FUNCTION block_identity_hard_cut_after_session_delete() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_advisory_xact_lock(%d);
			RETURN NULL;
		END
		$$;
		CREATE TRIGGER block_identity_hard_cut_after_session_delete
		AFTER DELETE ON control_plane_sessions
		FOR EACH STATEMENT EXECUTE FUNCTION block_identity_hard_cut_after_session_delete();
	`, lockKey)); err != nil {
		t.Fatal(err)
	}
	blocker, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = blocker.Rollback() }()
	if _, err := blocker.Exec(`SELECT pg_advisory_xact_lock($1)`, lockKey); err != nil {
		t.Fatal(err)
	}
	migrationDone := make(chan error, 1)
	go func() {
		migrationDone <- ApplyCustomerIdentityHardCut(context.Background(), OpenDriver(db))
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM pg_locks WHERE locktype='advisory' AND classid=0 AND objid::bigint=$1 AND NOT granted)`, lockKey).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("migration did not reach blocked post-delete window")
		}
		time.Sleep(10 * time.Millisecond)
	}
	insertDone := make(chan error, 1)
	go func() {
		_, err := db.Exec(`INSERT INTO control_plane_sessions VALUES ('sha256:old-binary-late-session','usr-one')`)
		insertDone <- err
	}()
	var insertErr error
	insertReturned := false
	select {
	case insertErr = <-insertDone:
		insertReturned = true
	case <-time.After(150 * time.Millisecond):
	}
	if err := blocker.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := <-migrationDone; err != nil {
		t.Fatal(err)
	}
	if !insertReturned {
		select {
		case insertErr = <-insertDone:
		case <-time.After(5 * time.Second):
			t.Fatal("old session insert did not finish after migration")
		}
	}
	if insertErr == nil {
		t.Fatal("old binary session insert survived hard-cut migration")
	}
	var sessions int
	if err := db.QueryRow(`SELECT count(*) FROM control_plane_sessions`).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if sessions != 0 {
		t.Fatalf("old sessions survived hard cut: %d", sessions)
	}
}

func requiredIdentityTestDatabaseURL(t *testing.T) string {
	t.Helper()
	databaseURL := identityTestDatabaseURL()
	if databaseURL == "" {
		t.Fatal("CONTROL_PLANE_TEST_DATABASE_URL or OPL_POSTGRES_TESTS=1 with PGHOST, PGPORT, PGUSER, PGDATABASE, and PGSSLMODE is required for PostgreSQL tests")
	}
	return databaseURL
}

func identityTestDatabaseURL() string {
	if databaseURL := strings.TrimSpace(os.Getenv("CONTROL_PLANE_TEST_DATABASE_URL")); databaseURL != "" {
		return databaseURL
	}
	if os.Getenv("OPL_POSTGRES_TESTS") != "1" {
		return ""
	}
	for _, key := range []string{"PGHOST", "PGPORT", "PGUSER", "PGDATABASE", "PGSSLMODE"} {
		if strings.TrimSpace(os.Getenv(key)) == "" {
			return ""
		}
	}
	return "connect_timeout=10"
}

func TestIdentityTestDatabaseURLRequiresExplicitOrIsolatedPostgresEnvironment(t *testing.T) {
	for _, key := range []string{"CONTROL_PLANE_TEST_DATABASE_URL", "OPL_POSTGRES_TESTS", "PGHOST", "PGPORT", "PGUSER", "PGDATABASE", "PGSSLMODE"} {
		t.Setenv(key, "")
	}
	for key, value := range map[string]string{
		"PGHOST": "/tmp/isolated-postgres", "PGPORT": "55432", "PGUSER": "postgres", "PGDATABASE": "postgres", "PGSSLMODE": "disable",
	} {
		t.Setenv(key, value)
	}
	if got := identityTestDatabaseURL(); got != "" {
		t.Fatalf("PG environment bypassed OPL_POSTGRES_TESTS gate: %q", got)
	}
	t.Setenv("OPL_POSTGRES_TESTS", "1")
	if got := identityTestDatabaseURL(); got != "connect_timeout=10" {
		t.Fatalf("isolated PG environment URL = %q", got)
	}
	t.Setenv("PGPORT", "")
	if got := identityTestDatabaseURL(); got != "" {
		t.Fatalf("incomplete PG environment accepted: %q", got)
	}
	t.Setenv("CONTROL_PLANE_TEST_DATABASE_URL", "  host=/tmp/explicit dbname=postgres sslmode=disable  ")
	t.Setenv("OPL_POSTGRES_TESTS", "")
	if got := identityTestDatabaseURL(); got != "host=/tmp/explicit dbname=postgres sslmode=disable" {
		t.Fatalf("explicit PostgreSQL test URL = %q", got)
	}
}

func newIdentityMigrationDatabase(t *testing.T, databaseURL string) *sql.DB {
	t.Helper()
	admin, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.Ping(); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("control_plane_customer_identity_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + pgx.Identifier{schema}.Sanitize() + ` CASCADE`) })
	db, err := sql.Open("pgx", postgresURLWithSearchPath(databaseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`
		CREATE TABLE control_plane_accounts (id text PRIMARY KEY, owner_user_id text NOT NULL DEFAULT '', sub2api_user_id bigint NOT NULL DEFAULT 0, status text NOT NULL);
		CREATE TABLE control_plane_users (id text PRIMARY KEY, account_id text NOT NULL, email text NOT NULL, role text NOT NULL, status text NOT NULL, password_hash text NOT NULL DEFAULT '');
		CREATE TABLE control_plane_organizations (id text PRIMARY KEY, billing_account_id text NOT NULL, status text NOT NULL);
		CREATE TABLE control_plane_memberships (id text PRIMARY KEY, account_id text NOT NULL, organization_id text NOT NULL, user_id text NOT NULL, role text NOT NULL, status text NOT NULL);
		CREATE TABLE control_plane_sessions (id text PRIMARY KEY, user_id text NOT NULL);
	`); err != nil {
		t.Fatal(err)
	}
	return db
}

func seedIdentityMigrationTables(t *testing.T, db *sql.DB, ownerUserID string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO control_plane_accounts VALUES ('acct-one',$1,41,'active')`, ownerUserID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`
		INSERT INTO control_plane_users VALUES ('usr-one','acct-one','owner@example.com','owner','active','legacy-secret-hash');
		INSERT INTO control_plane_organizations VALUES ('org-one','acct-one','active');
		INSERT INTO control_plane_memberships VALUES ('mem-one','acct-one','org-one','usr-one','owner','active');
		INSERT INTO control_plane_sessions VALUES ('session-one','usr-one');
	`); err != nil {
		t.Fatal(err)
	}
}
