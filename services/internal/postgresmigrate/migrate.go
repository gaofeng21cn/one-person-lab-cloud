package postgresmigrate

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"os"
	"strings"
)

// One database-wide key keeps the three services from migrating in parallel.
const advisoryLockKey int64 = 0x4f504c4d49475241

const createJournalSQL = `
CREATE TABLE opl_schema_migrations (
	service TEXT NOT NULL,
	version TEXT NOT NULL,
	applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (service, version)
)`

type Migration struct {
	Version string
	Run     func(context.Context) error
}

func ValidateTLS(databaseURL string) error {
	mode, host, err := postgresTLSSettings(strings.TrimSpace(databaseURL))
	if err == nil && postgresTCPHost(host) {
		if mode == "verify-full" {
			return nil
		}
		if os.Getenv("PGSSLMODE") == "disable" && (mode == "" || mode == "disable") && postgresPrivateIPv4(host) {
			return nil
		}
	}
	return errors.New("PostgreSQL DATABASE_URL must set sslmode=verify-full and an explicit TCP host, or use PGSSLMODE=disable with one RFC1918 IPv4 host")
}

func postgresTLSSettings(databaseURL string) (string, string, error) {
	if databaseURL == "" {
		return "", "", errors.New("empty PostgreSQL DSN")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return "", "", err
	}
	if parsed.Scheme != "" {
		if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
			return "", "", errors.New("invalid PostgreSQL URL scheme")
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil || len(query["sslmode"]) > 1 {
			return "", "", errors.New("PostgreSQL URL allows one sslmode")
		}
		host := parsed.Hostname()
		if queryHosts, ok := query["host"]; ok {
			if len(queryHosts) != 1 {
				return "", "", errors.New("PostgreSQL URL allows one host setting")
			}
			host = queryHosts[0]
		}
		return first(query["sslmode"]), host, nil
	}
	values, err := parseKeywordDSN(databaseURL)
	if err != nil || len(values["sslmode"]) > 1 {
		return "", "", errors.New("PostgreSQL DSN allows one sslmode")
	}
	if len(values["host"]) != 1 {
		return "", "", errors.New("PostgreSQL DSN requires one host setting")
	}
	return first(values["sslmode"]), values["host"][0], nil
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

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
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

func parseKeywordDSN(databaseURL string) (map[string][]string, error) {
	values := map[string][]string{}
	for index := 0; index < len(databaseURL); {
		for index < len(databaseURL) && dsnSpace(databaseURL[index]) {
			index++
		}
		if index == len(databaseURL) {
			break
		}
		start := index
		for index < len(databaseURL) && dsnKeyByte(databaseURL[index]) {
			index++
		}
		if start == index {
			return nil, errors.New("invalid PostgreSQL DSN key")
		}
		key := databaseURL[start:index]
		for index < len(databaseURL) && dsnSpace(databaseURL[index]) {
			index++
		}
		if index == len(databaseURL) || databaseURL[index] != '=' {
			return nil, errors.New("invalid PostgreSQL DSN assignment")
		}
		index++
		for index < len(databaseURL) && dsnSpace(databaseURL[index]) {
			index++
		}
		var value strings.Builder
		if index < len(databaseURL) && databaseURL[index] == '\'' {
			index++
			closed := false
			for index < len(databaseURL) {
				if databaseURL[index] == '\\' {
					index++
					if index == len(databaseURL) {
						return nil, errors.New("invalid PostgreSQL DSN escape")
					}
					value.WriteByte(databaseURL[index])
					index++
					continue
				}
				if databaseURL[index] == '\'' {
					index++
					closed = true
					break
				}
				value.WriteByte(databaseURL[index])
				index++
			}
			if !closed || (index < len(databaseURL) && !dsnSpace(databaseURL[index])) {
				return nil, errors.New("invalid PostgreSQL DSN quoted value")
			}
		} else {
			for index < len(databaseURL) && !dsnSpace(databaseURL[index]) {
				if databaseURL[index] == '\\' {
					index++
					if index == len(databaseURL) {
						return nil, errors.New("invalid PostgreSQL DSN escape")
					}
				}
				value.WriteByte(databaseURL[index])
				index++
			}
		}
		values[key] = append(values[key], value.String())
	}
	return values, nil
}

func dsnSpace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func dsnKeyByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9' || value == '_'
}

// Apply records migration history in the connection's default schema. It is the
// shared entry point for owners whose migrating role also owns the database's
// default schema, which is the current Control Plane, Fabric and Ledger shape.
func Apply(ctx context.Context, db *sql.DB, service string, migrations []Migration) error {
	return ApplyInSchema(ctx, db, "", service, migrations)
}

// ApplyInSchema records migration history in one explicit schema. An owner whose
// migrating role may only write its own schema needs the journal inside that
// schema, so the journal belongs to the owner rather than to a shared default
// schema the owner cannot create in.
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
		createJournal = strings.Replace(createJournalSQL, "CREATE TABLE ", "CREATE TABLE "+schema+".", 1)
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", advisoryLockKey); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", advisoryLockKey)
	}()

	var journalExists bool
	if err := conn.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", qualifiedJournal).Scan(&journalExists); err != nil {
		return fmt.Errorf("inspect migration journal: %w", err)
	}
	if !journalExists {
		if _, err := conn.ExecContext(ctx, createJournal); err != nil {
			return fmt.Errorf("create migration journal: %w", err)
		}
	}

	for _, migration := range migrations {
		version := strings.TrimSpace(migration.Version)
		if version == "" || migration.Run == nil {
			return errors.New("migration version and runner are required")
		}
		var applied bool
		if err := conn.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM `+qualifiedJournal+` WHERE service = $1 AND version = $2
		)`, service, version).Scan(&applied); err != nil {
			return fmt.Errorf("inspect migration %s/%s: %w", service, version, err)
		}
		if applied {
			continue
		}
		if err := migration.Run(ctx); err != nil {
			return fmt.Errorf("apply migration %s/%s: %w", service, version, err)
		}
		if _, err := conn.ExecContext(ctx, `
			INSERT INTO `+qualifiedJournal+` (service, version) VALUES ($1, $2)
		`, service, version); err != nil {
			return fmt.Errorf("record migration %s/%s: %w", service, version, err)
		}
	}
	return nil
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
