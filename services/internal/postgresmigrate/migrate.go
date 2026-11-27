package postgresmigrate

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"
)

// One database-wide key keeps the three services from migrating in parallel.
const advisoryLockKey int64 = 0x4f504c4d49475241

const createJournalSQL = `
CREATE TABLE IF NOT EXISTS opl_schema_migrations (
	service TEXT NOT NULL,
	version TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (service, version)
)`

const errTLSAdmissionMessage = "PostgreSQL DATABASE_URL must set sslmode=verify-full and an explicit TCP host, or use PGSSLMODE=disable with one RFC1918 IPv4 host"

type Migration struct {
	Version string
	Run     func(context.Context) error
}

// OpenPool parses databaseURL with pgxpool, opens one shared native pgxpool.Pool,
// and exposes a database/sql *sql.DB backed by that same pool. Callers must close
// both the *sql.DB and the *pgxpool.Pool on shutdown.
func OpenPool(ctx context.Context, databaseURL string, configure func(*pgxpool.Config)) (*pgxpool.Pool, *sql.DB, error) {
	databaseURL = strings.TrimSpace(databaseURL)
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, nil, err
	}
	normalizeLegacyLibPQRuntimeParams(cfg)
	if configure != nil {
		configure(cfg)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, nil, err
	}
	return pool, stdlib.OpenDBFromPool(pool), nil
}

// normalizeLegacyLibPQRuntimeParams strips lib/pq client-only query parameters
// that pgx/v5 forwards as PostgreSQL startup RuntimeParams (which cause servers
// to reject startup with SQLSTATE 42704).
func normalizeLegacyLibPQRuntimeParams(cfg *pgxpool.Config) {
	if cfg == nil || cfg.ConnConfig == nil || cfg.ConnConfig.RuntimeParams == nil {
		return
	}
	params := cfg.ConnConfig.RuntimeParams
	delete(params, "binary_parameters")
	if fallback, ok := params["fallback_application_name"]; ok {
		if strings.TrimSpace(params["application_name"]) == "" && strings.TrimSpace(fallback) != "" {
			params["application_name"] = fallback
		}
		delete(params, "fallback_application_name")
	}
}

// OpenStdlib opens a database/sql *sql.DB using the pgx/v5 stdlib driver.
func OpenStdlib(databaseURL string) (*sql.DB, error) {
	return sql.Open("pgx", strings.TrimSpace(databaseURL))
}

// StringArrayScanner returns a database/sql.Scanner for PostgreSQL text[] columns
// backed by pgx/v5's pgtype map, keeping Go 1.26 and Go 1.27 database/sql scanning
// consistent without lib/pq.
func StringArrayScanner(dest *[]string) sql.Scanner {
	return pgtype.NewMap().SQLScanner(dest)
}

// ValidateTLS validates a PostgreSQL DSN using pgx/v5's official libpq parser
// (pgconn.ParseConfig), requiring either explicit sslmode=verify-full on explicit
// TCP host(s) or explicit PGSSLMODE=disable on a single RFC1918 IPv4 host.
func ValidateTLS(databaseURL string) error {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return errors.New(errTLSAdmissionMessage)
	}
	cfg, err := pgconn.ParseConfig(databaseURL)
	if err != nil {
		return errors.New(errTLSAdmissionMessage)
	}
	if !hasExplicitTCPHost(databaseURL, cfg) {
		return errors.New(errTLSAdmissionMessage)
	}
	if allEndpointsVerifyFull(databaseURL, cfg) {
		return nil
	}
	if os.Getenv("PGSSLMODE") == "disable" &&
		len(cfg.Fallbacks) == 0 &&
		cfg.TLSConfig == nil &&
		postgresPrivateIPv4(cfg.Host) &&
		explicitSSLModeAllowsDisable(databaseURL) {
		return nil
	}
	return errors.New(errTLSAdmissionMessage)
}

func isPostgresURI(databaseURL string) bool {
	return strings.HasPrefix(databaseURL, "postgres://") || strings.HasPrefix(databaseURL, "postgresql://")
}

func hasExplicitTCPHost(databaseURL string, cfg *pgconn.Config) bool {
	if isPostgresURI(databaseURL) {
		parsed, err := url.Parse(databaseURL)
		if err != nil {
			return false
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil || len(query["sslmode"]) > 1 {
			return false
		}
		if queryHosts, ok := query["host"]; ok {
			if len(queryHosts) != 1 || !postgresTCPHost(queryHosts[0]) {
				return false
			}
		} else if !postgresTCPHost(parsed.Hostname()) {
			return false
		}
	} else {
		// Prepend two distinct sentinel hosts before parsing with pgconn.ParseConfig:
		// because libpq applies later keys last, both probes resolve to cfg.Host if
		// and only if databaseURL itself explicitly specifies host=... rather than
		// inheriting PGHOST or libpq's default socket host.
		probeA, errA := pgconn.ParseConfig("host=probe-a.invalid " + databaseURL)
		probeB, errB := pgconn.ParseConfig("host=probe-b.invalid " + databaseURL)
		if errA != nil || errB != nil || probeA.Host != probeB.Host || probeA.Host != cfg.Host {
			return false
		}
	}
	return allEndpointsTCP(cfg)
}

func allEndpointsTCP(cfg *pgconn.Config) bool {
	network, _ := pgconn.NetworkAddress(cfg.Host, cfg.Port)
	if network != "tcp" || !postgresTCPHost(cfg.Host) {
		return false
	}
	for _, fallback := range cfg.Fallbacks {
		network, _ := pgconn.NetworkAddress(fallback.Host, fallback.Port)
		if network != "tcp" || !postgresTCPHost(fallback.Host) {
			return false
		}
	}
	return true
}

func isVerifyFullTLS(tlsCfg *tls.Config) bool {
	// In pgconn's libpq TLS builder, verify-full is the only sslmode that keeps
	// TLSConfig non-nil with InsecureSkipVerify == false (require/prefer/allow/verify-ca
	// all set InsecureSkipVerify = true, and disable sets TLSConfig = nil).
	return tlsCfg != nil && !tlsCfg.InsecureSkipVerify
}

func allEndpointsVerifyFull(databaseURL string, cfg *pgconn.Config) bool {
	if !isVerifyFullTLS(cfg.TLSConfig) {
		return false
	}
	for _, fallback := range cfg.Fallbacks {
		if !isVerifyFullTLS(fallback.TLSConfig) {
			return false
		}
	}
	if isPostgresURI(databaseURL) {
		parsed, err := url.Parse(databaseURL)
		if err != nil {
			return false
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil || len(query["sslmode"]) != 1 || query["sslmode"][0] != "verify-full" {
			return false
		}
		return true
	}
	// Prepending sslmode=disable overrides any PGSSLMODE environment variable,
	// proving that sslmode=verify-full was explicitly declared in databaseURL.
	probe, err := pgconn.ParseConfig("sslmode=disable " + databaseURL)
	if err != nil || !isVerifyFullTLS(probe.TLSConfig) {
		return false
	}
	for _, fallback := range probe.Fallbacks {
		if !isVerifyFullTLS(fallback.TLSConfig) {
			return false
		}
	}
	return true
}

func explicitSSLModeAllowsDisable(databaseURL string) bool {
	if isPostgresURI(databaseURL) {
		parsed, err := url.Parse(databaseURL)
		if err != nil {
			return false
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil || len(query["sslmode"]) > 1 {
			return false
		}
		return len(query["sslmode"]) == 0 || query["sslmode"][0] == "disable"
	}
	return true
}

// validSchemaName accepts one unqualified lower-case identifier, so a caller cannot
// inject SQL through the journal's schema prefix.
func validSchemaName(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for index, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r == '_':
		case r >= '0' && r <= '9' && index > 0:
		default:
			return false
		}
	}
	return true
}

func postgresTCPHost(value string) bool {
	for _, host := range strings.Split(value, ",") {
		host = strings.TrimSpace(host)
		if host == "" || strings.ContainsAny(host, "/@") {
			return false
		}
	}
	return true
}

func postgresPrivateIPv4(host string) bool {
	address, err := netip.ParseAddr(host)
	return err == nil && address.Is4() && address.IsPrivate()
}

// Apply records migration history in the connection's default schema. It is the
// shared entry point for owners whose migrating role also owns the database's
// default schema, which is the current Control Plane, Fabric and Ledger shape.
func Apply(ctx context.Context, db *sql.DB, service string, migrations []Migration) error {
	return ApplyInSchema(ctx, db, "", service, migrations)
}

// ApplyFS reads every *.sql file in dir from an embedded or filesystem fs.FS in
// lexicographical version order and applies them through pressly/goose/v3.
func ApplyFS(ctx context.Context, db *sql.DB, schema, service string, fsys fs.FS, dir string) error {
	if fsys == nil {
		return errors.New("migration filesystem is required")
	}
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	type sqlMigration struct {
		version string
		query   string
	}
	ordered := make([]sqlMigration, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		filePath := entry.Name()
		if dir != "." {
			filePath = path.Join(dir, entry.Name())
		}
		data, err := fs.ReadFile(fsys, filePath)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}
		ordered = append(ordered, sqlMigration{
			version: strings.TrimSuffix(entry.Name(), ".sql"),
			query:   string(data),
		})
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].version < ordered[j].version })
	migrations := make([]Migration, 0, len(ordered))
	for _, item := range ordered {
		query := item.query
		migrations = append(migrations, Migration{
			Version: item.version,
			Run: func(ctx context.Context) error {
				_, err := db.ExecContext(ctx, query)
				return err
			},
		})
	}
	return ApplyInSchema(ctx, db, schema, service, migrations)
}

// ApplyInSchema records migration history in one explicit schema using
// pressly/goose/v3 with a PostgreSQL session advisory lock and a store adapter
// backed by the canonical opl_schema_migrations table so existing production
// databases migrate seamlessly with zero data loss.
func ApplyInSchema(ctx context.Context, db *sql.DB, schema, service string, migrations []Migration) error {
	if db == nil {
		return errors.New("migration database is required")
	}
	service = strings.TrimSpace(service)
	if service == "" {
		return errors.New("migration service is required")
	}
	schema = strings.TrimSpace(schema)
	qualifiedJournal := "opl_schema_migrations"
	createJournal := createJournalSQL
	if schema != "" {
		if !validSchemaName(schema) {
			return fmt.Errorf("migration schema %q is not a plain identifier", schema)
		}
		qualifiedJournal = schema + ".opl_schema_migrations"
		createJournal = strings.Replace(createJournalSQL, "CREATE TABLE IF NOT EXISTS ", "CREATE TABLE IF NOT EXISTS "+schema+".", 1)
	}

	store := &oplJournalStore{
		service:        service,
		qualifiedTable: qualifiedJournal,
		createSQL:      createJournal,
		idToVersion:    make(map[int64]string, len(migrations)),
		versionToID:    make(map[string]int64, len(migrations)),
	}
	gooseMigrations := make([]*goose.Migration, 0, len(migrations))
	for idx, migration := range migrations {
		version := strings.TrimSpace(migration.Version)
		if version == "" || migration.Run == nil {
			return errors.New("migration version and runner are required")
		}
		if _, duplicate := store.versionToID[version]; duplicate {
			return fmt.Errorf("duplicate migration version %s/%s", service, version)
		}
		ordinal := int64(idx + 1)
		store.idToVersion[ordinal] = version
		store.versionToID[version] = ordinal
		run := migration.Run
		gooseMigrations = append(gooseMigrations, goose.NewGoMigration(
			ordinal,
			&goose.GoFunc{
				RunDB: func(ctx context.Context, _ *sql.DB) error {
					if err := run(ctx); err != nil {
						return fmt.Errorf("apply migration %s/%s: %w", service, version, err)
					}
					return nil
				},
			},
			nil,
		))
	}

	locker, err := lock.NewPostgresSessionLocker(
		lock.WithLockID(advisoryLockKey),
		lock.WithLockTimeout(1, 300),
		lock.WithUnlockTimeout(1, 60),
	)
	if err != nil {
		return fmt.Errorf("configure migration locker: %w", err)
	}
	provider, err := goose.NewProvider(
		database.DialectCustom,
		db,
		nil,
		goose.WithStore(store),
		goose.WithSessionLocker(locker),
		goose.WithGoMigrations(gooseMigrations...),
		goose.WithAllowOutofOrder(true),
	)
	if err != nil {
		return fmt.Errorf("configure migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return err
	}
	return nil
}

// oplJournalStore adapts pressly/goose/v3's database.StoreExtender onto the
// canonical opl_schema_migrations (service, version, applied_at) table so live
// production databases retain full version history across services and schemas.
type oplJournalStore struct {
	service        string
	qualifiedTable string
	createSQL      string
	idToVersion    map[int64]string
	versionToID    map[string]int64
}

var _ database.StoreExtender = (*oplJournalStore)(nil)

func (s *oplJournalStore) Tablename() string {
	return s.qualifiedTable
}

func (s *oplJournalStore) TableExists(ctx context.Context, db database.DBTxConn) (bool, error) {
	var exists bool
	if err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", s.qualifiedTable).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect migration journal: %w", err)
	}
	return exists, nil
}

func (s *oplJournalStore) CreateVersionTable(ctx context.Context, db database.DBTxConn) error {
	if _, err := db.ExecContext(ctx, s.createSQL); err != nil {
		return fmt.Errorf("create migration journal: %w", err)
	}
	return nil
}

func (s *oplJournalStore) Insert(ctx context.Context, db database.DBTxConn, req database.InsertRequest) error {
	// goose records synthetic version 0 immediately after creating the version
	// table. Keep opl_schema_migrations strictly scoped to real service versions.
	if req.Version == 0 {
		return nil
	}
	version, ok := s.idToVersion[req.Version]
	if !ok {
		return fmt.Errorf("unknown migration ordinal %d for service %s", req.Version, s.service)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO `+s.qualifiedTable+` (service, version) VALUES ($1, $2)`,
		s.service, version,
	); err != nil {
		return fmt.Errorf("record migration %s/%s: %w", s.service, version, err)
	}
	return nil
}

func (s *oplJournalStore) Delete(ctx context.Context, db database.DBTxConn, version int64) error {
	if version == 0 {
		return nil
	}
	name, ok := s.idToVersion[version]
	if !ok {
		return database.ErrVersionNotFound
	}
	if _, err := db.ExecContext(ctx,
		`DELETE FROM `+s.qualifiedTable+` WHERE service = $1 AND version = $2`,
		s.service, name,
	); err != nil {
		return fmt.Errorf("delete migration %s/%s: %w", s.service, name, err)
	}
	return nil
}

func (s *oplJournalStore) GetMigration(ctx context.Context, db database.DBTxConn, version int64) (*database.GetMigrationResult, error) {
	if version == 0 {
		return &database.GetMigrationResult{IsApplied: true}, nil
	}
	name, ok := s.idToVersion[version]
	if !ok {
		return nil, database.ErrVersionNotFound
	}
	var appliedAt sql.NullTime
	err := db.QueryRowContext(ctx,
		`SELECT applied_at FROM `+s.qualifiedTable+` WHERE service = $1 AND version = $2`,
		s.service, name,
	).Scan(&appliedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrVersionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("inspect migration %s/%s: %w", s.service, name, err)
	}
	res := &database.GetMigrationResult{IsApplied: true}
	if appliedAt.Valid {
		res.Timestamp = appliedAt.Time
	}
	return res, nil
}

func (s *oplJournalStore) GetLatestVersion(ctx context.Context, db database.DBTxConn) (int64, error) {
	migrations, err := s.ListMigrations(ctx, db)
	if err != nil {
		return 0, err
	}
	if len(migrations) == 0 {
		return 0, database.ErrVersionNotFound
	}
	return migrations[0].Version, nil
}

func (s *oplJournalStore) ListMigrations(ctx context.Context, db database.DBTxConn) ([]*database.ListMigrationsResult, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT version FROM `+s.qualifiedTable+` WHERE service = $1`,
		s.service,
	)
	if err != nil {
		return nil, fmt.Errorf("list migrations for %s: %w", s.service, err)
	}
	defer rows.Close()
	results := make([]*database.ListMigrationsResult, 0, len(s.idToVersion)+1)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan migration version for %s: %w", s.service, err)
		}
		if ordinal, ok := s.versionToID[version]; ok {
			results = append(results, &database.ListMigrationsResult{
				Version:   ordinal,
				IsApplied: true,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate migrations for %s: %w", s.service, err)
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Version > results[j].Version
	})
	results = append(results, &database.ListMigrationsResult{
		Version:   0,
		IsApplied: true,
	})
	return results, nil
}

// AdmitDatabaseURL is the single accepted admission rule for an owner's
// PostgreSQL DSN. Production requires sslmode=verify-full on an explicit TCP
// host. The one local exception is an explicit OPL_POSTGRES_TESTS=1 with
// sslmode=disable against a loopback host, which only an isolated test database
// may use. Every Cloud owner applies this same rule, so no service invents its own
// weaker or stronger variant.
func AdmitDatabaseURL(getenv func(string) string, databaseURL string) error {
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" {
		return errors.New("PostgreSQL DATABASE_URL is required")
	}
	if err := ValidateTLS(databaseURL); err == nil {
		return nil
	}
	if getenv("NODE_ENV") == "production" || getenv("OPL_POSTGRES_TESTS") != "1" {
		return ValidateTLS(databaseURL)
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" || parsed.Host == "" || parsed.Fragment != "" {
		return ValidateTLS(databaseURL)
	}
	query := parsed.Query()
	if len(query["sslmode"]) != 1 || query.Get("sslmode") != "disable" || query.Has("host") {
		return ValidateTLS(databaseURL)
	}
	if address := net.ParseIP(parsed.Hostname()); address == nil || !address.IsLoopback() {
		return ValidateTLS(databaseURL)
	}
	return nil
}
