package ownerstoretest

import (
	"context"
	"strings"
	"testing"

	"opl-cloud/services/internal/ownerstore"
)

// Owner identifies one Cloud data owner's isolated database shape. Each owner's own
// test supplies its real values; one shared suite then proves the same facts for all
// of them instead of each owner re-deriving them.
type Owner struct {
	// Name is the Cloud owner identity, which is also the schema name.
	Name string
	// Database is the exact database name the owner's DDL guards.
	Database string
	// SchemaOwnerRole owns the schema and runs the owner's migration.
	SchemaOwnerRole string
	// WriterRole is the writer role the owner's DDL grants its writes to.
	WriterRole string
	// RuntimeRole is the runtime login that holds only the writer grant.
	RuntimeRole string
	// Source is the owner's real embedded migration entrypoint.
	Source ownerstore.MigrationSource
	// Tables are tables this owner's DDL must create.
	Tables []string
	// OperationKind is one kind value this owner accepts in its own operations table.
	OperationKind string
}

// RunIsolationSuite proves, against a real PostgreSQL server, the facts that make an
// owner's migrations trustworthy and that reading SQL text cannot show:
//
//  1. the owner's real migration entrypoint installs its schema and tables;
//  2. the DDL refuses a database with a different name;
//  3. the runtime login writes its own schema; and
//  4. the runtime login cannot read another owner's schema or create outside its own.
func RunIsolationSuite(t *testing.T, getenv func(string) string, owner Owner) {
	t.Helper()
	if len(owner.Tables) == 0 {
		t.Fatal("isolation suite requires at least one expected table")
	}
	adminDSN := EnsureAdminDSNOrSkip(getenv, t.Skip)
	ctx := context.Background()

	harness, err := Setup(ctx, Config{
		AdminDSN:        adminDSN,
		Owner:           owner.Name,
		Database:        owner.Database,
		SchemaOwnerRole: owner.SchemaOwnerRole,
		WriterRole:      owner.WriterRole,
		RuntimeRole:     owner.RuntimeRole,
	})
	if err != nil {
		t.Fatalf("provision isolated %s database: %v", owner.Name, err)
	}
	t.Cleanup(func() { _ = harness.Close(context.Background()) })

	migrate := func(t *testing.T, database string) error {
		t.Helper()
		return harness.Install(ctx, harness.OwnerDSN, database, owner.Source)
	}

	t.Run("migrates its own database", func(t *testing.T) {
		if err := migrate(t, harness.DatabaseName()); err != nil {
			t.Fatalf("install %s migrations: %v", owner.Name, err)
		}
		db, err := harness.Open(ctx, harness.OwnerDSN, harness.DatabaseName())
		if err != nil {
			t.Fatalf("open %s database: %v", owner.Name, err)
		}
		defer db.Close()
		for _, table := range owner.Tables {
			var present bool
			if err := db.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, owner.Name+"."+table).Scan(&present); err != nil {
				t.Fatal(err)
			}
			if !present {
				t.Fatalf("%s.%s was not created by the real migration", owner.Name, table)
			}
		}
	})

	t.Run("refuses a differently named database", func(t *testing.T) {
		// The harness creates a second database the same owner role may connect to and
		// create in, under a name the owner's DDL does not guard. A refusal there is the
		// owner's own wrong-database guard rather than a missing privilege.
		err := migrate(t, harness.OtherDatabaseName())
		if err == nil {
			t.Fatalf("%s migration accepted the postgres database instead of refusing it", owner.Name)
		}
		if !strings.Contains(err.Error(), "Wrong database") || !strings.Contains(err.Error(), harness.OtherDatabaseName()) {
			t.Fatalf("%s wrong-database refusal does not name the actual database: %v", owner.Name, err)
		}
	})

	t.Run("runtime writes only its own schema", func(t *testing.T) {
		if err := migrate(t, harness.DatabaseName()); err != nil {
			t.Fatalf("install %s migrations: %v", owner.Name, err)
		}
		runtime, err := harness.Open(ctx, harness.RuntimeDSN, harness.DatabaseName())
		if err != nil {
			t.Fatalf("open %s runtime role: %v", owner.Name, err)
		}
		defer runtime.Close()

		var current string
		if err := runtime.QueryRowContext(ctx, `SELECT current_user`).Scan(&current); err != nil {
			t.Fatal(err)
		}
		if current != owner.RuntimeRole {
			t.Fatalf("runtime connection is %q, want %q", current, owner.RuntimeRole)
		}
		var count int
		if err := runtime.QueryRowContext(ctx, `SELECT count(*) FROM `+quote(owner.Name)+`.outbox_events`).Scan(&count); err != nil {
			t.Fatalf("%s runtime cannot read its own outbox: %v", owner.Name, err)
		}
		// The runtime performs this owner's own write, which the DDL granted.
		if _, err := runtime.ExecContext(ctx, `INSERT INTO `+quote(owner.Name)+`.operations
			(id, actor_id, kind, resource_id, stage, request_id, accepted_input)
			VALUES ('op-isolation-probe', 'actor-isolation-probe', $1, 'resource-isolation-probe', 'queued', 'request-isolation-probe', '{}'::jsonb)`, owner.OperationKind); err != nil {
			t.Fatalf("%s runtime cannot insert its own operation: %v", owner.Name, err)
		}
		// The runtime is an unprivileged login: it may not widen its own access.
		var superuser, createdb, createrole bool
		if err := runtime.QueryRowContext(ctx, `SELECT rolsuper, rolcreatedb, rolcreaterole FROM pg_roles WHERE rolname = current_user`).Scan(&superuser, &createdb, &createrole); err != nil {
			t.Fatal(err)
		}
		if superuser || createdb || createrole {
			t.Fatalf("%s runtime role has elevated attributes: super=%v createdb=%v createrole=%v", owner.Name, superuser, createdb, createrole)
		}
	})

	t.Run("runtime cannot reach another owner's schema", func(t *testing.T) {
		runtime, err := harness.Open(ctx, harness.RuntimeDSN, harness.DatabaseName())
		if err != nil {
			t.Fatalf("open %s runtime role: %v", owner.Name, err)
		}
		defer runtime.Close()

		// A neighbour schema in the same database stands in for another Cloud owner's
		// data. The server must refuse the read; the service must not be the only thing
		// keeping owners apart.
		var id string
		err = runtime.QueryRowContext(ctx, `SELECT id FROM `+quote(NeighborSchema())+`.owner_records LIMIT 1`).Scan(&id)
		if err == nil {
			t.Fatalf("%s runtime read another owner's record %q", owner.Name, id)
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("cross-owner read failed for an unexpected reason: %v", err)
		}
		// The runtime may not create outside its own schema either.
		if _, err := runtime.ExecContext(ctx, `CREATE TABLE `+quote(NeighborSchema())+`.intrusion (id integer)`); err == nil {
			t.Fatal("runtime was allowed to create a table in another owner's schema")
		} else if !strings.Contains(err.Error(), "permission denied") {
			t.Fatalf("cross-schema create failed for an unexpected reason: %v", err)
		}
	})
}
