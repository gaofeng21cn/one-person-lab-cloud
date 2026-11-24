package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestApplyInvitedAccountIdentityNormalizesAndFailsClosed(t *testing.T) {
	driver := &recordingDriver{}
	if err := ApplyInvitedAccountIdentity(context.Background(), driver); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"btrim(email) = ''",
		"GROUP BY lower(btrim(email))",
		"duplicate normalized user emails",
		"UPDATE control_plane_users",
		"SET email = lower(btrim(email))",
		"CREATE UNIQUE INDEX IF NOT EXISTS control_plane_users_email_normalized_unique",
		"(lower(btrim(email)))",
	} {
		if !strings.Contains(driver.query, required) {
			t.Fatalf("embedded identity migration missing %q", required)
		}
	}
}

func TestInvitedAccountIdentityMigrationPostgres(t *testing.T) {
	databaseURL := requiredIdentityTestDatabaseURL(t)
	admin, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.Ping(); err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("control_plane_identity_%d", time.Now().UnixNano())
	if _, err := admin.Exec(`CREATE SCHEMA ` + pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(`DROP SCHEMA ` + pgx.Identifier{schema}.Sanitize() + ` CASCADE`) })

	db, err := sql.Open("pgx", postgresURLWithSearchPath(databaseURL, schema))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
		CREATE TABLE control_plane_users (id text PRIMARY KEY, email text NOT NULL);
		INSERT INTO control_plane_users VALUES ('one', ' Owner@Example.com '), ('two', 'owner@example.com');
	`); err != nil {
		t.Fatal(err)
	}
	driver := OpenDriver(db)
	if err := ApplyInvitedAccountIdentity(context.Background(), driver); err == nil {
		t.Fatal("identity migration accepted normalized collision")
	}
	var first string
	if err := db.QueryRow(`SELECT email FROM control_plane_users WHERE id = 'one'`).Scan(&first); err != nil || first != " Owner@Example.com " {
		t.Fatalf("failed migration mutated legacy email=%q err=%v", first, err)
	}
	if _, err := db.Exec(`DELETE FROM control_plane_users WHERE id = 'two'`); err != nil {
		t.Fatal(err)
	}
	if err := ApplyInvitedAccountIdentity(context.Background(), driver); err != nil {
		t.Fatal(err)
	}
	if err := ApplyInvitedAccountIdentity(context.Background(), driver); err != nil {
		t.Fatalf("repeat identity migration: %v", err)
	}
	if err := db.QueryRow(`SELECT email FROM control_plane_users WHERE id = 'one'`).Scan(&first); err != nil || first != "owner@example.com" {
		t.Fatalf("normalized email=%q err=%v", first, err)
	}
	if _, err := db.Exec(`INSERT INTO control_plane_users VALUES ('two', ' OWNER@example.com ')`); err == nil {
		t.Fatal("normalized unique index accepted duplicate")
	}
}

func postgresURLWithSearchPath(databaseURL, schema string) string {
	if parsed, err := url.Parse(databaseURL); err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return databaseURL + " search_path=" + ("'" + strings.ReplaceAll(schema, "'", "''") + "'")
}
