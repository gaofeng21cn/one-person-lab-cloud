package ownerstore

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"
)

// riverMigrationLockKey serializes concurrent River schema migrations per database
// without colliding with postgresmigrate's advisoryLockKey.
const riverMigrationLockKey int64 = 0x4f504c5249564552

// RiverClientConfig configures a PostgreSQL-native River worker client sharing
// the caller's existing pgxpool.Pool (no external broker or second orchestrator).
type RiverClientConfig struct {
	Schema       string
	Queues       map[string]river.QueueConfig
	Workers      *river.Workers
	PeriodicJobs []*river.PeriodicJob
	Logger       *slog.Logger
}

// MigrateRiverSchema applies River's built-in PostgreSQL migrations inside
// schema (or the connection's default search_path when schema is empty),
// serialized by a PostgreSQL session advisory lock.
func MigrateRiverSchema(ctx context.Context, pool *pgxpool.Pool, schema string) error {
	if pool == nil {
		return errors.New("ownerstore: pgxpool is required for River migration")
	}
	schema = strings.TrimSpace(schema)
	if schema != "" {
		if _, ok := ownerSchemas[schema]; !ok && !isSafeRiverSchemaName(schema) {
			return fmt.Errorf("ownerstore: invalid River schema %q", schema)
		}
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("ownerstore: acquire River migration connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", riverMigrationLockKey); err != nil {
		return fmt.Errorf("ownerstore: lock River migration: %w", err)
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", riverMigrationLockKey)
	}()

	migrator, err := rivermigrate.New(riverpgxv5.New(pool), &rivermigrate.Config{
		Schema: schema,
	})
	if err != nil {
		return fmt.Errorf("ownerstore: construct River migrator: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("ownerstore: migrate River schema %q: %w", schema, err)
	}
	return nil
}

// IsPermissionDenied reports PostgreSQL SQLSTATE 42501 (insufficient_privilege),
// allowing least-privilege runtime roles to detect when DBA-owned River DDL was
// not pre-installed in their schema.
func IsPermissionDenied(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}

// NewRiverClient builds a PostgreSQL-native *river.Client[pgx.Tx] over pool.
func NewRiverClient(pool *pgxpool.Pool, cfg RiverClientConfig) (*river.Client[pgx.Tx], error) {
	if pool == nil {
		return nil, errors.New("ownerstore: pgxpool is required for River client")
	}
	queues := cfg.Queues
	if len(queues) == 0 {
		queues = map[string]river.QueueConfig{
			river.QueueDefault: {MaxWorkers: 4},
		}
	}
	riverCfg := &river.Config{
		Schema:                      strings.TrimSpace(cfg.Schema),
		Queues:                      queues,
		Workers:                     cfg.Workers,
		PeriodicJobs:                cfg.PeriodicJobs,
		FetchOnlyKnownKinds:         true,
		FetchCooldown:               25 * time.Millisecond,
		FetchPollInterval:           500 * time.Millisecond,
		CompletedJobRetentionPeriod: 24 * time.Hour,
		Logger:                      cfg.Logger,
	}
	client, err := river.NewClient(riverpgxv5.New(pool), riverCfg)
	if err != nil {
		return nil, fmt.Errorf("ownerstore: construct River client: %w", err)
	}
	return client, nil
}

// EnqueueInTx atomically enqueues a River job inside the caller's existing
// PostgreSQL pgx.Tx transaction so state writes and durable work commit or
// roll back together.
func EnqueueInTx[T river.JobArgs](
	ctx context.Context,
	client *river.Client[pgx.Tx],
	tx pgx.Tx,
	args T,
	opts *river.InsertOpts,
) (*rivertype.JobInsertResult, error) {
	if client == nil {
		return nil, errors.New("ownerstore: River client is required")
	}
	if tx == nil {
		return nil, errors.New("ownerstore: pgx.Tx is required for transactional enqueue")
	}
	return client.InsertTx(ctx, tx, args, opts)
}

// OutboxDispatchJobArgs triggers durable delivery of one owner's pending Outbox
// deliveries over PostgreSQL-native River workers instead of bare ticker loops.
type OutboxDispatchJobArgs struct {
	Owner    string `json:"owner"`
	Consumer string `json:"consumer,omitempty"`
}

func (OutboxDispatchJobArgs) Kind() string { return "opl.ownerstore.outbox_dispatch" }

func (OutboxDispatchJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		MaxAttempts: 10,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 5 * time.Second,
		},
	}
}

// OutboxDispatchWorker executes one owner's outbox delivery function inside River.
type OutboxDispatchWorker struct {
	river.WorkerDefaults[OutboxDispatchJobArgs]
	Deliver func(ctx context.Context, consumer string) error
}

func (w *OutboxDispatchWorker) Work(ctx context.Context, job *river.Job[OutboxDispatchJobArgs]) error {
	if w.Deliver == nil {
		return nil
	}
	return w.Deliver(ctx, job.Args.Consumer)
}

func isSafeRiverSchemaName(value string) bool {
	if value == "" || len(value) > 63 {
		return false
	}
	for i, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
