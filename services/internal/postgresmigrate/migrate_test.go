package postgresmigrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestValidateTLSRequiresExplicitVerifyFull(t *testing.T) {
	for _, databaseURL := range []string{
		"postgresql://user:pass@db.example/opl",
		"postgresql://user:pass@db.example/opl?sslmode=disable",
		"postgresql://user:pass@db.example/opl?sslmode=allow",
		"postgresql://user:pass@db.example/opl?sslmode=prefer",
		"postgresql://user:pass@db.example/opl?sslmode=require",
		"postgresql://user:pass@db.example/opl?sslmode=verify-ca",
		"postgresql://user:pass@db.example/opl?sslmode=verify-full&sslmode=disable",
		"host=db.example dbname=opl user=opl",
		"host=db.example dbname=opl sslmode=disable",
		"host=db.example application_name='sslmode=verify-full'",
		"host=db.example sslmode=verify-full sslmode=disable",
	} {
		t.Run(databaseURL, func(t *testing.T) {
			if err := ValidateTLS(databaseURL); err == nil {
				t.Fatalf("ValidateTLS(%q) succeeded", databaseURL)
			}
		})
	}

	for _, databaseURL := range []string{
		"postgresql://user:pass@db.example/opl?application_name=opl&sslmode=verify-full",
		"host=db.example dbname=opl user=opl sslmode=verify-full application_name='opl cloud'",
	} {
		t.Run(databaseURL, func(t *testing.T) {
			if err := ValidateTLS(databaseURL); err != nil {
				t.Fatalf("ValidateTLS(%q): %v", databaseURL, err)
			}
		})
	}
}

func TestValidateTLSRequiresExplicitTCPHost(t *testing.T) {
	t.Setenv("PGHOST", "db.example")
	for _, databaseURL := range []string{
		"dbname=opl sslmode=verify-full",
		"host='' dbname=opl sslmode=verify-full",
		"host=/tmp/opl-postgres dbname=opl sslmode=verify-full",
		"host=@opl-postgres dbname=opl sslmode=verify-full",
		"host='db.example,/tmp/opl-postgres' dbname=opl sslmode=verify-full",
		"postgresql:///opl?sslmode=verify-full",
		"postgresql:///opl?host=%2Ftmp%2Fopl-postgres&sslmode=verify-full",
		"postgresql://db.example/opl?host=db.example%2C%2Ftmp%2Fopl-postgres&sslmode=verify-full",
	} {
		t.Run(databaseURL, func(t *testing.T) {
			if err := ValidateTLS(databaseURL); err == nil {
				t.Fatalf("ValidateTLS(%q) succeeded", databaseURL)
			}
		})
	}

	for _, databaseURL := range []string{
		"host=db.example dbname=opl sslmode=verify-full",
		"postgresql://user:pass@db.example/opl?sslmode=verify-full",
		"postgresql:///opl?host=db.example&sslmode=verify-full",
	} {
		t.Run(databaseURL, func(t *testing.T) {
			if err := ValidateTLS(databaseURL); err != nil {
				t.Fatalf("ValidateTLS(%q): %v", databaseURL, err)
			}
		})
	}
}

func TestValidateTLSAllowsExplicitPrivatePilotDisableOnly(t *testing.T) {
	t.Setenv("PGSSLMODE", "disable")
	for _, databaseURL := range []string{
		"postgresql://user:pass@10.66.0.21/opl",
		"postgresql://user:pass@10.66.0.21/opl?sslmode=disable",
		"host=172.16.0.1 dbname=opl sslmode=disable",
		"host=192.168.1.1 dbname=opl",
	} {
		t.Run("accept_"+databaseURL, func(t *testing.T) {
			if err := ValidateTLS(databaseURL); err != nil {
				t.Fatalf("ValidateTLS(%q): %v", databaseURL, err)
			}
		})
	}

	for _, databaseURL := range []string{
		"postgresql://user:pass@db.example/opl?sslmode=disable",
		"postgresql://user:pass@8.8.8.8/opl?sslmode=disable",
		"postgresql:///opl?sslmode=disable",
		"postgresql:///opl?host=%2Ftmp%2Fopl-postgres&sslmode=disable",
		"postgresql:///opl?host=10.66.0.21%2C10.66.0.22&sslmode=disable",
		"host=10.66.0.21,10.66.0.22 dbname=opl sslmode=disable",
		"host=10.66.0.21 dbname=opl sslmode=require",
	} {
		t.Run("reject_"+databaseURL, func(t *testing.T) {
			if err := ValidateTLS(databaseURL); err == nil {
				t.Fatalf("ValidateTLS(%q) succeeded", databaseURL)
			}
		})
	}

	t.Setenv("PGSSLMODE", "")
	if err := ValidateTLS("postgresql://user:pass@10.66.0.21/opl?sslmode=disable"); err == nil {
		t.Fatal("private sslmode=disable succeeded without explicit PGSSLMODE=disable")
	}
}

func TestNormalizePostgresDSNPreservesAuthAndAdmissionSemantics(t *testing.T) {
	t.Run("password with literal at and sslmode disable", func(t *testing.T) {
		t.Setenv("PGSSLMODE", "disable")
		raw := "postgresql://user:p@ss@10.66.0.21:5432/opl?sslmode=disable"
		if err := ValidateTLS(raw); err != nil {
			t.Fatalf("expected ValidateTLS to accept unescaped @ password, got: %v", err)
		}
		if err := AdmitDatabaseURL(func(string) string { return "" }, raw); err != nil {
			t.Fatalf("expected AdmitDatabaseURL to accept unescaped @ password, got: %v", err)
		}
		cfg, err := pgxpool.ParseConfig(normalizePostgresDSN(raw))
		if err != nil {
			t.Fatalf("pgxpool.ParseConfig failed: %v", err)
		}
		if cfg.ConnConfig.User != "user" || cfg.ConnConfig.Password != "p@ss" || cfg.ConnConfig.Host != "10.66.0.21" || cfg.ConnConfig.Database != "opl" {
			t.Fatalf("unexpected parsed config: user=%q password=%q host=%q db=%q", cfg.ConnConfig.User, cfg.ConnConfig.Password, cfg.ConnConfig.Host, cfg.ConnConfig.Database)
		}
	})

	t.Run("password with literal at without sslmode", func(t *testing.T) {
		raw := "postgresql://user:p@ss@10.66.0.21:5432/opl"
		t.Setenv("PGSSLMODE", "")
		if err := ValidateTLS(raw); err == nil {
			t.Fatal("expected ValidateTLS to reject missing sslmode without PGSSLMODE=disable")
		}
		t.Setenv("PGSSLMODE", "disable")
		if err := ValidateTLS(raw); err != nil {
			t.Fatalf("expected ValidateTLS with PGSSLMODE=disable to accept private IPv4 without sslmode query, got: %v", err)
		}
	})

	t.Run("password with literal at and verify-full", func(t *testing.T) {
		t.Setenv("PGSSLMODE", "")
		raw := "postgres://user:p@ss@db.example.com:5432/opl?sslmode=verify-full"
		if err := ValidateTLS(raw); err != nil {
			t.Fatalf("expected ValidateTLS to accept verify-full with unescaped @ password, got: %v", err)
		}
		cfg, err := pgxpool.ParseConfig(normalizePostgresDSN(raw))
		if err != nil {
			t.Fatalf("pgxpool.ParseConfig failed: %v", err)
		}
		if cfg.ConnConfig.Password != "p@ss" || cfg.ConnConfig.Host != "db.example.com" {
			t.Fatalf("unexpected parsed config: password=%q host=%q", cfg.ConnConfig.Password, cfg.ConnConfig.Host)
		}
	})

	t.Run("password with colon and at or already encoded percent40", func(t *testing.T) {
		t.Setenv("PGSSLMODE", "")
		rawColonAt := "postgresql://user:p@ss:123@10.66.0.21:5432/opl?sslmode=verify-full"
		if err := ValidateTLS(rawColonAt); err != nil {
			t.Fatalf("expected ValidateTLS to accept password with : and @, got: %v", err)
		}
		cfgColonAt, err := pgxpool.ParseConfig(normalizePostgresDSN(rawColonAt))
		if err != nil {
			t.Fatalf("pgxpool.ParseConfig failed: %v", err)
		}
		if cfgColonAt.ConnConfig.User != "user" || cfgColonAt.ConnConfig.Password != "p@ss:123" {
			t.Fatalf("unexpected colon+@ parse: user=%q password=%q", cfgColonAt.ConnConfig.User, cfgColonAt.ConnConfig.Password)
		}

		rawEncoded := "postgresql://user:p%40ss@10.66.0.21:5432/opl?sslmode=verify-full"
		if err := ValidateTLS(rawEncoded); err != nil {
			t.Fatalf("expected ValidateTLS to accept %%40 password, got: %v", err)
		}
		cfgEncoded, err := pgxpool.ParseConfig(normalizePostgresDSN(rawEncoded))
		if err != nil {
			t.Fatalf("pgxpool.ParseConfig failed: %v", err)
		}
		if cfgEncoded.ConnConfig.Password != "p@ss" {
			t.Fatalf("expected %%40 not to be double-encoded, got %q", cfgEncoded.ConnConfig.Password)
		}
	})

	t.Run("query unescaped equals, literal plus, uppercase scheme, empty pair", func(t *testing.T) {
		t.Setenv("PGSSLMODE", "disable")
		raw := "POSTGRES://user:p@ss@10.66.0.21:5432/opl?&options=-c%20search_path=public&application_name=opl+ledger&sslmode=disable"
		if err := ValidateTLS(raw); err != nil {
			t.Fatalf("expected ValidateTLS to accept canonicalized query/scheme, got: %v", err)
		}
		cfg, err := pgxpool.ParseConfig(normalizePostgresDSN(raw))
		if err != nil {
			t.Fatalf("pgxpool.ParseConfig failed: %v", err)
		}
		if got := cfg.ConnConfig.RuntimeParams["options"]; got != "-c search_path=public" {
			t.Fatalf("unexpected options param: %q", got)
		}
		if got := cfg.ConnConfig.RuntimeParams["application_name"]; got != "opl+ledger" {
			t.Fatalf("expected literal + preserved in application_name, got: %q", got)
		}
	})

	t.Run("single empty sslmode follows PGSSLMODE while duplicate sslmode remains rejected", func(t *testing.T) {
		rawSingleEmpty := "postgresql://user:pass@10.66.0.21:5432/opl?sslmode="
		t.Setenv("PGSSLMODE", "disable")
		if err := ValidateTLS(rawSingleEmpty); err != nil {
			t.Fatalf("expected single empty sslmode= with PGSSLMODE=disable on private IP to succeed, got: %v", err)
		}
		t.Setenv("PGSSLMODE", "")
		if err := ValidateTLS(rawSingleEmpty); err == nil {
			t.Fatal("expected single empty sslmode= without PGSSLMODE=disable to be rejected")
		}

		t.Setenv("PGSSLMODE", "disable")
		for _, dup := range []string{
			"postgresql://user:pass@10.66.0.21:5432/opl?sslmode=verify-full&sslmode=disable",
			"postgresql://user:pass@10.66.0.21:5432/opl?sslmode=disable&sslmode=verify-full",
			"postgresql://user:pass@10.66.0.21:5432/opl?sslmode=&sslmode=disable",
		} {
			if err := ValidateTLS(dup); err == nil {
				t.Fatalf("expected ValidateTLS to reject duplicate sslmode %q", dup)
			}
			if err := AdmitDatabaseURL(func(string) string { return "" }, dup); err == nil {
				t.Fatalf("expected AdmitDatabaseURL to reject duplicate sslmode %q", dup)
			}
		}
	})

	t.Run("keyword value DSN stays untouched aside from trimming", func(t *testing.T) {
		t.Setenv("PGSSLMODE", "")
		raw := "  host=db.example.com port=5432 user=opl password=p@ss dbname=opl sslmode=verify-full  "
		if got := normalizePostgresDSN(raw); got != "host=db.example.com port=5432 user=opl password=p@ss dbname=opl sslmode=verify-full" {
			t.Fatalf("unexpected keyword/value normalization: %q", got)
		}
		if err := ValidateTLS(raw); err != nil {
			t.Fatalf("expected ValidateTLS to accept keyword/value verify-full DSN, got: %v", err)
		}
	})
}

func TestOpenPoolTrimsWhitespaceAndNormalizesLegacyLibPQParams(t *testing.T) {
	t.Run("trailing_newline_plain_private_ip", func(t *testing.T) {
		t.Setenv("PGSSLMODE", "disable")
		dsn := "postgres://u:p@10.0.0.1:5432/opl_control_plane\n"
		if err := ValidateTLS(dsn); err != nil {
			t.Fatalf("ValidateTLS: %v", err)
		}
		var gotDB string
		var gotTLSNil bool
		pool, db, err := OpenPool(context.Background(), dsn, func(cfg *pgxpool.Config) {
			gotDB = cfg.ConnConfig.Database
			gotTLSNil = cfg.ConnConfig.TLSConfig == nil
		})
		if err != nil {
			t.Fatalf("OpenPool: %v", err)
		}
		_ = db.Close()
		pool.Close()
		if gotDB != "opl_control_plane" || !gotTLSNil {
			t.Fatalf("gotDB=%q gotTLSNil=%v", gotDB, gotTLSNil)
		}
	})

	t.Run("trailing_crlf_sslmode_disable", func(t *testing.T) {
		t.Setenv("PGSSLMODE", "disable")
		dsn := "postgres://u:p@10.0.0.1:5432/opl_control_plane?sslmode=disable\r\n"
		if err := ValidateTLS(dsn); err != nil {
			t.Fatalf("ValidateTLS: %v", err)
		}
		var gotDB string
		var gotTLSNil bool
		pool, db, err := OpenPool(context.Background(), dsn, func(cfg *pgxpool.Config) {
			gotDB = cfg.ConnConfig.Database
			gotTLSNil = cfg.ConnConfig.TLSConfig == nil
		})
		if err != nil {
			t.Fatalf("OpenPool: %v", err)
		}
		_ = db.Close()
		pool.Close()
		if gotDB != "opl_control_plane" || !gotTLSNil {
			t.Fatalf("gotDB=%q gotTLSNil=%v", gotDB, gotTLSNil)
		}
	})

	t.Run("surrounding_spaces_verify_full", func(t *testing.T) {
		dsn := "  postgres://u:p@db.example.com:5432/opl_control_plane?sslmode=verify-full  "
		if err := ValidateTLS(dsn); err != nil {
			t.Fatalf("ValidateTLS: %v", err)
		}
		var gotDB string
		var hasTLS bool
		pool, db, err := OpenPool(context.Background(), dsn, func(cfg *pgxpool.Config) {
			gotDB = cfg.ConnConfig.Database
			hasTLS = cfg.ConnConfig.TLSConfig != nil
		})
		if err != nil {
			t.Fatalf("OpenPool: %v", err)
		}
		_ = db.Close()
		pool.Close()
		if gotDB != "opl_control_plane" || !hasTLS {
			t.Fatalf("gotDB=%q hasTLS=%v", gotDB, hasTLS)
		}
	})

	t.Run("strips_binary_parameters_and_promotes_fallback_application_name", func(t *testing.T) {
		t.Setenv("PGSSLMODE", "disable")
		dsn := "postgres://u:p@10.0.0.1:5432/opl_control_plane?sslmode=disable&binary_parameters=yes&fallback_application_name=opl-ledger"
		if err := ValidateTLS(dsn); err != nil {
			t.Fatalf("ValidateTLS: %v", err)
		}
		var params map[string]string
		pool, db, err := OpenPool(context.Background(), dsn, func(cfg *pgxpool.Config) {
			params = cfg.ConnConfig.RuntimeParams
		})
		if err != nil {
			t.Fatalf("OpenPool: %v", err)
		}
		_ = db.Close()
		pool.Close()
		if _, ok := params["binary_parameters"]; ok {
			t.Fatalf("binary_parameters must be stripped: %#v", params)
		}
		if _, ok := params["fallback_application_name"]; ok {
			t.Fatalf("fallback_application_name must be stripped: %#v", params)
		}
		if params["application_name"] != "opl-ledger" {
			t.Fatalf("application_name = %q, want opl-ledger", params["application_name"])
		}
	})

	t.Run("keeps_explicit_application_name_over_fallback", func(t *testing.T) {
		t.Setenv("PGSSLMODE", "disable")
		dsn := "postgres://u:p@10.0.0.1:5432/opl_control_plane?sslmode=disable&application_name=explicit&fallback_application_name=fallback"
		if err := ValidateTLS(dsn); err != nil {
			t.Fatalf("ValidateTLS: %v", err)
		}
		var params map[string]string
		pool, db, err := OpenPool(context.Background(), dsn, func(cfg *pgxpool.Config) {
			params = cfg.ConnConfig.RuntimeParams
		})
		if err != nil {
			t.Fatalf("OpenPool: %v", err)
		}
		_ = db.Close()
		pool.Close()
		if _, ok := params["fallback_application_name"]; ok {
			t.Fatalf("fallback_application_name must be stripped: %#v", params)
		}
		if params["application_name"] != "explicit" {
			t.Fatalf("application_name = %q, want explicit", params["application_name"])
		}
	})
}

func TestApplyRunsMigrationOnlyOnce(t *testing.T) {
	db := openIsolatedPostgres(t)
	var calls atomic.Int32
	migration := Migration{
		Version: "001_create_probe",
		Run: func(ctx context.Context) error {
			calls.Add(1)
			_, err := db.ExecContext(ctx, `CREATE TABLE migration_probe (id integer PRIMARY KEY); INSERT INTO migration_probe VALUES (1)`)
			return err
		},
	}

	for range 2 {
		if err := Apply(context.Background(), db, "test-service", []Migration{migration}); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("migration calls = %d, want 1", got)
	}
	assertMigrationCount(t, db, "test-service", "001_create_probe", 1)
}

func TestApplySerializesConcurrentStartup(t *testing.T) {
	databaseURL := isolatedPostgresURL(t)
	first := openPostgres(t, databaseURL)
	second := openPostgres(t, databaseURL)
	var calls atomic.Int32
	start := make(chan struct{})
	run := func(db *sql.DB) error {
		<-start
		return Apply(context.Background(), db, "concurrent-service", []Migration{{
			Version: "001_concurrent",
			Run: func(ctx context.Context) error {
				calls.Add(1)
				time.Sleep(100 * time.Millisecond)
				_, err := db.ExecContext(ctx, `CREATE TABLE concurrent_probe (id integer PRIMARY KEY)`)
				return err
			},
		}})
	}

	errs := make(chan error, 2)
	var wait sync.WaitGroup
	for _, db := range []*sql.DB{first, second} {
		wait.Add(1)
		go func(db *sql.DB) {
			defer wait.Done()
			errs <- run(db)
		}(db)
	}
	close(start)
	wait.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("concurrent migration calls = %d, want 1", got)
	}
	assertMigrationCount(t, first, "concurrent-service", "001_concurrent", 1)
}

func TestApplyDoesNotRecordFailedMigration(t *testing.T) {
	db := openIsolatedPostgres(t)
	wantErr := errors.New("migration failed")
	err := Apply(context.Background(), db, "failure-service", []Migration{{
		Version: "001_failure",
		Run: func(context.Context) error {
			return wantErr
		},
	}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Apply error = %v, want %v", err, wantErr)
	}
	assertMigrationCount(t, db, "failure-service", "001_failure", 0)

	if err := Apply(context.Background(), db, "failure-service", []Migration{{
		Version: "001_failure",
		Run: func(context.Context) error {
			return nil
		},
	}}); err != nil {
		t.Fatalf("retry failed migration: %v", err)
	}
	assertMigrationCount(t, db, "failure-service", "001_failure", 1)
}

func TestApplyBackwardCompatibleWithExistingJournalAndFS(t *testing.T) {
	databaseURL := isolatedPostgresURL(t)
	pool, db, err := OpenPool(context.Background(), databaseURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		pool.Close()
	})

	// Simulate a live production database with a pre-existing opl_schema_migrations row
	// written by the legacy runner before pressly/goose/v3 took over.
	if _, err := db.Exec(`
		CREATE TABLE opl_schema_migrations (
			service TEXT NOT NULL,
			version TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (service, version)
		);
		CREATE TABLE legacy_items (id integer PRIMARY KEY);
		INSERT INTO legacy_items VALUES (42);
		INSERT INTO opl_schema_migrations (service, version) VALUES ('legacy-svc', '202607010001_initial');
	`); err != nil {
		t.Fatal(err)
	}

	fsys := fstest.MapFS{
		"migrations/202607010001_initial.sql": &fstest.MapFile{
			Data: []byte(`CREATE TABLE legacy_items (id integer PRIMARY KEY); -- would fail if re-run`),
		},
		"migrations/202607020001_add_note.sql": &fstest.MapFile{
			Data: []byte(`ALTER TABLE legacy_items ADD COLUMN note TEXT NOT NULL DEFAULT 'preserved';`),
		},
	}
	if err := ApplyFS(context.Background(), db, "", "legacy-svc", fsys, "migrations"); err != nil {
		t.Fatalf("ApplyFS over pre-existing journal: %v", err)
	}

	var note string
	if err := pool.QueryRow(context.Background(), `SELECT note FROM legacy_items WHERE id = 42`).Scan(&note); err != nil {
		t.Fatal(err)
	}
	if note != "preserved" {
		t.Fatalf("note = %q, want preserved", note)
	}
	assertMigrationCount(t, db, "legacy-svc", "202607010001_initial", 1)
	assertMigrationCount(t, db, "legacy-svc", "202607020001_add_note", 1)

	var totalRows int
	if err := db.QueryRow(`SELECT count(*) FROM opl_schema_migrations WHERE service = 'legacy-svc'`).Scan(&totalRows); err != nil {
		t.Fatal(err)
	}
	if totalRows != 2 {
		t.Fatalf("total opl_schema_migrations rows = %d, want 2 (no synthetic version 0)", totalRows)
	}
}

func assertMigrationCount(t *testing.T, db *sql.DB, service, version string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(`SELECT count(*) FROM opl_schema_migrations WHERE service = $1 AND version = $2`, service, version).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("migration count = %d, want %d", got, want)
	}
}

func openIsolatedPostgres(t *testing.T) *sql.DB {
	t.Helper()
	return openPostgres(t, isolatedPostgresURL(t))
}

func openPostgres(t *testing.T, databaseURL string) *sql.DB {
	t.Helper()
	db, err := OpenStdlib(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func isolatedPostgresURL(t *testing.T) string {
	t.Helper()
	databaseURL := os.Getenv("POSTGRES_MIGRATION_TEST_DATABASE_URL")
	if databaseURL == "" {
		if os.Getenv("OPL_POSTGRES_TESTS") == "1" {
			databaseURL = "connect_timeout=10"
		} else {
			t.Skip("PostgreSQL test gate is not configured: set POSTGRES_MIGRATION_TEST_DATABASE_URL or OPL_POSTGRES_TESTS=1 for an isolated fixture")
		}
	}
	admin, err := OpenStdlib(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.Ping(); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	schema := fmt.Sprintf("opl_migration_%d", time.Now().UnixNano())
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err := admin.Exec(`CREATE SCHEMA ` + quotedSchema); err != nil {
		_ = admin.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(`DROP SCHEMA ` + quotedSchema + ` CASCADE`)
		_ = admin.Close()
	})
	if parsed, err := url.Parse(databaseURL); err == nil && parsed.Scheme != "" {
		query := parsed.Query()
		query.Set("search_path", schema)
		parsed.RawQuery = query.Encode()
		return parsed.String()
	}
	return databaseURL + " search_path='" + strings.ReplaceAll(schema, "'", "''") + "'"
}
