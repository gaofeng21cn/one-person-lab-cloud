package ownerstoretest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// Inspect only the backend tagged by this fixture; the shared server can also
// be serving other owner suites. PostgreSQL session exit is observed directly.
func awaitHarnessBackendExit(t *testing.T, observer *sql.DB, application string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	lastCount := 0
	observed := false
	for {
		var count int
		if err := observer.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE application_name=$1`, application).Scan(&count); err != nil {
			t.Fatalf("backend exit observation failed; last PostgreSQL readback=%d backend(s) for %s: %v", lastCount, application, err)
		}
		lastCount = count
		if !observed {
			t.Logf("PostgreSQL readback after Close: %d backend(s) for %s", count, application)
			observed = true
		}
		if count == 0 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("Harness.Close left %d PostgreSQL backend(s) alive for %s", count, application)
		case <-tick.C:
		}
	}
}

func harnessLifecycleFixture(t *testing.T) (Config, *sql.DB, string) {
	t.Helper()
	dsn := EnsureAdminDSNOrSkip(os.Getenv, t.Skip)
	observer, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = observer.Close() })
	name := fmt.Sprintf("opl_h871_%d", time.Now().UnixNano())
	application := name + "_admin"
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("application_name", application)
	parsed.RawQuery = query.Encode()
	config := Config{AdminDSN: parsed.String(), Owner: name, Database: name, SchemaOwnerRole: name + "_owner", WriterRole: name + "_writer", RuntimeRole: name + "_runtime"}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		// On RED, terminate only this fixture backend after recording its leak.
		_, _ = observer.ExecContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE application_name=$1`, application)
		for _, database := range []string{config.Database, otherDatabase(config.Database)} {
			_, _ = observer.ExecContext(ctx, `DROP DATABASE IF EXISTS `+quote(database))
		}
		for _, role := range []string{config.RuntimeRole, config.SchemaOwnerRole, config.WriterRole, neighborRole(config)} {
			_, _ = observer.ExecContext(ctx, `DROP ROLE IF EXISTS `+quote(role))
		}
	})
	return config, observer, application
}

func TestHarnessReleasesAdminBackend(t *testing.T) {
	t.Run("successful and repeated Close", func(t *testing.T) {
		config, observer, application := harnessLifecycleFixture(t)
		harness, err := Setup(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = harness.Close(context.Background()) })
		var pid int
		if err := harness.admin.QueryRowContext(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			t.Fatal(err)
		}
		t.Logf("fixture admin backend before Close: pid=%d application=%s", pid, application)
		if err := harness.Close(t.Context()); err != nil {
			t.Fatal(err)
		}
		awaitHarnessBackendExit(t, observer, application)
		t.Logf("fixture admin backend after Close: pid=%d no longer present", pid)
		var calls sync.WaitGroup
		for i := 0; i < 8; i++ {
			calls.Add(1)
			go func() {
				defer calls.Done()
				if err := harness.Close(t.Context()); err != nil {
					t.Errorf("repeated Close must be safe: %v", err)
				}
			}()
		}
		calls.Wait()
	})
	t.Run("failed cleanup still releases pool", func(t *testing.T) {
		config, observer, application := harnessLifecycleFixture(t)
		harness, err := Setup(t.Context(), config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = harness.Close(context.Background()) })
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		err = harness.Close(ctx)
		if err == nil || ctx.Err() != context.Canceled {
			t.Fatalf("canceled cleanup error=%v", err)
		}
		t.Logf("actual PostgreSQL cleanup rejection: %v", err)
		awaitHarnessBackendExit(t, observer, application)
		if repeated := harness.Close(context.Background()); repeated != err {
			t.Fatalf("repeated Close changed result: first=%v repeated=%v", err, repeated)
		}
	})
	t.Run("failed provision releases pool", func(t *testing.T) {
		config, observer, application := harnessLifecycleFixture(t)
		// Duplicate role names fail after the admin connection and lock exist.
		config.RuntimeRole = config.WriterRole
		harness, err := Setup(t.Context(), config)
		if err == nil || harness != nil || !strings.Contains(err.Error(), "create role") {
			t.Fatalf("expected provision failure: harness=%v err=%v", harness, err)
		}
		t.Logf("actual PostgreSQL provisioning rejection: %v", err)
		awaitHarnessBackendExit(t, observer, application)
	})
	t.Run("failed advisory lock releases pool", func(t *testing.T) {
		config, observer, application := harnessLifecycleFixture(t)
		holder, err := observer.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		defer holder.Close()
		if _, err := holder.ExecContext(t.Context(), `SELECT pg_advisory_lock($1)`, advisoryKey(config.Database)); err != nil {
			t.Fatal(err)
		}
		defer holder.ExecContext(context.Background(), `SELECT pg_advisory_unlock($1)`, advisoryKey(config.Database))
		ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		defer cancel()
		harness, err := Setup(ctx, config)
		if err == nil || harness != nil || !strings.Contains(err.Error(), "lock isolated") {
			t.Fatalf("expected lock failure: harness=%v err=%v", harness, err)
		}
		t.Logf("actual PostgreSQL lock rejection: %v", err)
		awaitHarnessBackendExit(t, observer, application)
	})
}
