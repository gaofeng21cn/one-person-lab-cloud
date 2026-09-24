// Package ownerstoretest provisions one owner's isolated PostgreSQL database so a
// test can prove the real migration entrypoint instead of reading SQL text.
//
// It is test support: nothing in a running service imports it. It exists once
// because every owner repeats the same prerequisite — the migrating role, the
// writer role the owner's DDL grants its writes to, and a runtime login that
// inherits only that writer role — and duplicating that bootstrap five times would
// let each copy drift.
package ownerstoretest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"net/url"
	"strings"

	_ "github.com/lib/pq"

	"opl-cloud/services/internal/ownerstore"
)

// AdminDSNEnv names the superuser DSN the harness may use. When it is absent the
// caller must skip: creating roles and databases is a deployment-owner action that
// an unprivileged test connection cannot perform.
const AdminDSNEnv = "OPL_OWNER_MIGRATION_TEST_ADMIN_DSN"

// WriterPassword is the password the harness gives the roles it creates. It is a
// fixed local test credential, never a production value.
const WriterPassword = "opl-owner-isolation-test-password"

// AdminDSN returns the administrative DSN and whether one is configured.
func AdminDSN(getenv func(string) string) (string, bool) {
	dsn := strings.TrimSpace(getenv(AdminDSNEnv))
	return dsn, dsn != ""
}

// EnsureAdminDSNOrSkip reports the administrative DSN, skipping the test when the
// environment does not provide one.
func EnsureAdminDSNOrSkip(getenv func(string) string, skip func(...any)) string {
	dsn, ok := AdminDSN(getenv)
	if !ok {
		skip("set " + AdminDSNEnv + " to an isolated PostgreSQL administrator DSN to run owner migration isolation tests")
	}
	return dsn
}

// Config names one owner's isolated database shape, matching the contract's
// deployment-owner provisioning: the database is owned by the migrating role, the
// owner's DDL grants its writes to a dedicated writer role, and the runtime logs in
// through a separate role that holds only that writer grant.
type Config struct {
	// AdminDSN is a superuser connection to the same server.
	AdminDSN string
	// Owner is the Cloud owner identity, which is also the schema the migration
	// creates.
	Owner string
	// Database is the exact database name the owner's migration guards.
	Database string
	// SchemaOwnerRole owns the schema and runs the owner's migration.
	SchemaOwnerRole string
	// WriterRole is the non-login privilege container the owner's DDL grants its
	// writes to.
	WriterRole string
	// RuntimeRole logs in and inherits only WriterRole, so the running service cannot
	// exceed the write boundary the owner's DDL declared.
	RuntimeRole string
}

// Harness is one provisioned isolated owner database.
type Harness struct {
	Config Config
	// OwnerDSN connects as the migrating schema owner.
	OwnerDSN string
	// RuntimeDSN connects as the runtime login that holds only the writer grant.
	RuntimeDSN string
	admin      *sql.Conn
}

// neighborSchema is a second schema in the same database, owned by a role this
// owner's runtime is never granted. It is how the isolation proof shows the server
// refuses cross-owner reads rather than trusting the service to avoid them.
const neighborSchema = "opl_isolation_neighbor"

// Setup takes a per-owner advisory lock, removes any previous isolation database,
// creates the three roles and the guarded database, and plants the neighbour schema.
func Setup(ctx context.Context, config Config) (*Harness, error) {
	if strings.TrimSpace(config.AdminDSN) == "" {
		return nil, errors.New("admin DSN is required to provision an isolated owner database")
	}
	if strings.TrimSpace(config.Owner) == "" || strings.TrimSpace(config.Database) == "" ||
		strings.TrimSpace(config.SchemaOwnerRole) == "" || strings.TrimSpace(config.WriterRole) == "" ||
		strings.TrimSpace(config.RuntimeRole) == "" {
		return nil, errors.New("owner, database, schema owner role, writer role, and runtime role are required")
	}
	db, err := sql.Open("postgres", config.AdminDSN)
	if err != nil {
		return nil, err
	}
	connection, err := db.Conn(ctx)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	// The advisory lock serializes same-owner runs across processes so two suites
	// cannot delete each other's database. Different owners hash to different keys.
	if _, err := connection.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, advisoryKey(config.Database)); err != nil {
		_ = connection.Close()
		_ = db.Close()
		return nil, fmt.Errorf("lock isolated %s database: %w", config.Owner, err)
	}
	harness := &Harness{Config: config, admin: connection}
	if err := harness.provision(ctx); err != nil {
		_ = harness.Close(context.Background())
		return nil, err
	}
	return harness, nil
}

func (h *Harness) provision(ctx context.Context) error {
	if err := h.dropDatabase(ctx); err != nil {
		return err
	}
	// Roles persist on the server between runs, and an earlier run may have left a
	// membership or an owned object behind. Dropping and recreating them makes this
	// run's privilege shape deterministic instead of inheriting stale grants.
	roles := []struct {
		name  string
		login bool
	}{
		{h.Config.SchemaOwnerRole, true},
		{h.Config.WriterRole, false},
		{h.Config.RuntimeRole, true},
		{neighborRole(h.Config), false},
	}
	for _, role := range roles {
		_, _ = h.admin.ExecContext(ctx, `DROP OWNED BY `+quote(role.name))
		_, _ = h.admin.ExecContext(ctx, `DROP ROLE IF EXISTS `+quote(role.name))
	}
	for _, role := range roles {
		login := "NOLOGIN"
		if role.login {
			login = "LOGIN"
		}
		if _, err := h.admin.ExecContext(ctx, `CREATE ROLE `+quote(role.name)+` `+login+` NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION PASSWORD '`+WriterPassword+`'`); err != nil {
			return fmt.Errorf("create role %s: %w", role.name, err)
		}
	}
	// The schema owner runs the owner's DDL, which grants its writes to the writer
	// role; it therefore needs administrative authority over that one role. The runtime
	// login holds only that writer grant, so it cannot exceed the DDL's own boundary.
	if _, err := h.admin.ExecContext(ctx, `GRANT `+quote(h.Config.WriterRole)+` TO `+quote(h.Config.SchemaOwnerRole)+` WITH ADMIN OPTION`); err != nil {
		return fmt.Errorf("grant writer to schema owner: %w", err)
	}
	if _, err := h.admin.ExecContext(ctx, `GRANT `+quote(h.Config.WriterRole)+` TO `+quote(h.Config.RuntimeRole)); err != nil {
		return fmt.Errorf("grant writer to runtime: %w", err)
	}
	if _, err := h.admin.ExecContext(ctx, `CREATE DATABASE `+quote(h.Config.Database)+` OWNER `+quote(h.Config.SchemaOwnerRole)); err != nil {
		return fmt.Errorf("create database %s: %w", h.Config.Database, err)
	}

	// A second database owned by this same owner role exists so the wrong-database
	// proof has a connectable target that is not the name the DDL guards. The owner
	// role may create in it, so a refusal there is the DDL's own guard rather than a
	// missing privilege.
	_, _ = h.admin.ExecContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, otherDatabase(h.Config.Database))
	if _, err := h.admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+quote(otherDatabase(h.Config.Database))); err != nil {
		return fmt.Errorf("drop previous other database: %w", err)
	}
	if _, err := h.admin.ExecContext(ctx, `CREATE DATABASE `+quote(otherDatabase(h.Config.Database))+` OWNER `+quote(h.Config.SchemaOwnerRole)); err != nil {
		return fmt.Errorf("create other database: %w", err)
	}
	if _, err := h.admin.ExecContext(ctx, `CREATE ROLE `+quote(neighborRole(h.Config))); err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("create neighbor role: %w", err)
	}
	ownerDSN, err := withDatabaseAndUser(h.Config.AdminDSN, h.Config.Database, h.Config.SchemaOwnerRole)
	if err != nil {
		return err
	}
	h.OwnerDSN = ownerDSN
	runtimeDSN, err := withDatabaseAndUser(h.Config.AdminDSN, h.Config.Database, h.Config.RuntimeRole)
	if err != nil {
		return err
	}
	h.RuntimeDSN = runtimeDSN
	return h.plantNeighbor(ctx)
}

// plantNeighbor creates one table in a schema this owner's runtime is never granted.
func (h *Harness) plantNeighbor(ctx context.Context) error {
	dsn, err := withDatabase(h.Config.AdminDSN, h.Config.Database)
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS `+quote(neighborSchema)+` AUTHORIZATION `+quote(neighborRole(h.Config))+`;
		SET ROLE `+quote(neighborRole(h.Config))+`;
		CREATE TABLE IF NOT EXISTS `+quote(neighborSchema)+`.owner_records (id text PRIMARY KEY);
		INSERT INTO `+quote(neighborSchema)+`.owner_records (id) VALUES ('neighbor-record') ON CONFLICT DO NOTHING;
		RESET ROLE;`); err != nil {
		return fmt.Errorf("plant neighbour schema: %w", err)
	}
	return nil
}

func (h *Harness) dropDatabase(ctx context.Context) error {
	_, _ = h.admin.ExecContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, h.Config.Database)
	if _, err := h.admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+quote(h.Config.Database)); err != nil {
		return fmt.Errorf("drop previous %s database: %w", h.Config.Database, err)
	}
	return nil
}

// Close drops the isolated database and releases the lock. The roles stay because
// they are shared with any concurrent run of the same owner suite.
func (h *Harness) Close(ctx context.Context) error {
	if h == nil || h.admin == nil {
		return nil
	}
	dropErr := h.dropDatabase(ctx)
	if _, err := h.admin.ExecContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, otherDatabase(h.Config.Database)); err == nil {
		_, _ = h.admin.ExecContext(ctx, `DROP DATABASE IF EXISTS `+quote(otherDatabase(h.Config.Database)))
	}
	if _, err := h.admin.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, advisoryKey(h.Config.Database)); err != nil && dropErr == nil {
		dropErr = err
	}
	if err := h.admin.Close(); err != nil && dropErr == nil {
		dropErr = err
	}
	return dropErr
}

// Install runs the owner's real migration entrypoint through the schema owner role,
// which is the role the owner's DDL grants its writes to the writer role from.
func (h *Harness) Install(ctx context.Context, dsn, database string, source ownerstore.MigrationSource) error {
	db, err := h.open(ctx, dsn, database)
	if err != nil {
		return err
	}
	defer db.Close()
	store, err := ownerstore.New(db, h.Config.Owner)
	if err != nil {
		return err
	}
	return store.Install(ctx, source)
}

// Open opens the isolated owner database under an explicit DSN and database name.
func (h *Harness) Open(ctx context.Context, dsn, database string) (*sql.DB, error) {
	return h.open(ctx, dsn, database)
}

func (h *Harness) open(ctx context.Context, dsn, database string) (*sql.DB, error) {
	scoped, err := withDatabase(dsn, database)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("postgres", scoped)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// DatabaseName is the isolated database the harness created.
func (h *Harness) DatabaseName() string { return h.Config.Database }

// NeighborSchema is the other owner's schema planted in the same database.
func NeighborSchema() string { return neighborSchema }

// OtherDatabaseName is a second database owned by the same owner role but named
// differently from the one the owner's DDL guards.
func (h *Harness) OtherDatabaseName() string { return otherDatabase(h.Config.Database) }

func otherDatabase(database string) string { return database + "_other" }

func neighborRole(config Config) string { return config.SchemaOwnerRole + "_neighbor" }

func advisoryKey(database string) int64 {
	hash := fnv.New64a()
	_, _ = hash.Write([]byte("opl-owner-isolation\x00" + database))
	return int64(hash.Sum64())
}

func quote(identifier string) string {
	return `"` + strings.ReplaceAll(identifier, `"`, `""`) + `"`
}

func withDatabase(dsn, database string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", errors.New("admin DSN must be a postgres URL")
	}
	parsed.Path = "/" + database
	return parsed.String(), nil
}

func withDatabaseAndUser(dsn, database, user string) (string, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", errors.New("admin DSN must be a postgres URL")
	}
	parsed.Path = "/" + database
	host := parsed.Hostname()
	port := parsed.Port()
	if port == "" {
		port = "5432"
	}
	parsed.Host = host + ":" + port
	parsed.User = url.UserPassword(user, WriterPassword)
	return parsed.String(), nil
}
