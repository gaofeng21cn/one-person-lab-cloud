package fabric

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"opl-cloud/services/internal/ownerstore"
)

const riverQueueFabricReconcile = "fabric_reconcile"

// ComputePoolReconcileJobArgs schedules PostgreSQL-native durable compute-pool
// head reconciliation without an external queue or hand-rolled ticker loop.
type ComputePoolReconcileJobArgs struct {
	PoolKey     string `json:"poolKey"`
	OperationID string `json:"operationId"`
}

func (ComputePoolReconcileJobArgs) Kind() string { return "fabric.compute_pool_reconcile" }

func (ComputePoolReconcileJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       riverQueueFabricReconcile,
		MaxAttempts: 15,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 30 * time.Second,
		},
	}
}

// OperationReconcileJobArgs schedules durable reconciliation for an in-flight
// Fabric operation record.
type OperationReconcileJobArgs struct {
	OperationID string `json:"operationId"`
}

func (OperationReconcileJobArgs) Kind() string { return "fabric.operation_reconcile" }

func (OperationReconcileJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       riverQueueFabricReconcile,
		MaxAttempts: 15,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 30 * time.Second,
		},
	}
}

// RiverReconcileHandlers supplies the domain reconciliation callbacks executed
// by River workers over Fabric's existing PostgreSQL database.
type RiverReconcileHandlers struct {
	ReconcilePoolHead  func(ctx context.Context, poolKey, operationID string) error
	ReconcileOperation func(ctx context.Context, operationID string) error
}

type computePoolReconcileWorker struct {
	river.WorkerDefaults[ComputePoolReconcileJobArgs]
	fn func(ctx context.Context, poolKey, operationID string) error
}

func (w *computePoolReconcileWorker) Work(ctx context.Context, job *river.Job[ComputePoolReconcileJobArgs]) error {
	if w.fn == nil {
		return nil
	}
	return w.fn(ctx, job.Args.PoolKey, job.Args.OperationID)
}

type operationReconcileWorker struct {
	river.WorkerDefaults[OperationReconcileJobArgs]
	fn func(ctx context.Context, operationID string) error
}

func (w *operationReconcileWorker) Work(ctx context.Context, job *river.Job[OperationReconcileJobArgs]) error {
	if w.fn == nil {
		return nil
	}
	return w.fn(ctx, job.Args.OperationID)
}

// RiverDispatcher coordinates durable Fabric background jobs natively on
// PostgresOperationStore's existing pgxpool.Pool via riverqueue/river.
type RiverDispatcher struct {
	store  *PostgresOperationStore
	client *river.Client[pgx.Tx]
}

// NewRiverDispatcher migrates River tables in Fabric's schema and creates a
// PostgreSQL-native River worker client sharing store.Pool().
func NewRiverDispatcher(ctx context.Context, store *PostgresOperationStore, handlers RiverReconcileHandlers) (*RiverDispatcher, error) {
	if store == nil || store.Pool() == nil {
		return nil, errors.New("fabric: PostgresOperationStore with pgxpool is required for River dispatcher")
	}
	if err := ownerstore.MigrateRiverSchema(ctx, store.Pool(), ""); err != nil {
		return nil, err
	}
	workers := river.NewWorkers()
	river.AddWorker(workers, &computePoolReconcileWorker{fn: handlers.ReconcilePoolHead})
	river.AddWorker(workers, &operationReconcileWorker{fn: handlers.ReconcileOperation})

	client, err := ownerstore.NewRiverClient(store.Pool(), ownerstore.RiverClientConfig{
		Workers: workers,
		Queues: map[string]river.QueueConfig{
			riverQueueFabricReconcile: {MaxWorkers: 4},
		},
	})
	if err != nil {
		return nil, err
	}
	return &RiverDispatcher{store: store, client: client}, nil
}

func (d *RiverDispatcher) Start(ctx context.Context) error {
	return d.client.Start(ctx)
}

func (d *RiverDispatcher) Stop(ctx context.Context) error {
	if d == nil || d.client == nil {
		return nil
	}
	return d.client.Stop(ctx)
}

// EnqueuePoolReconcileTx atomically enqueues a compute-pool reconciliation job
// in the caller's PostgreSQL transaction alongside Fabric state updates.
func (d *RiverDispatcher) EnqueuePoolReconcileTx(ctx context.Context, tx pgx.Tx, poolKey, operationID string) (*rivertype.JobInsertResult, error) {
	return ownerstore.EnqueueInTx(ctx, d.client, tx, ComputePoolReconcileJobArgs{
		PoolKey:     poolKey,
		OperationID: operationID,
	}, nil)
}

// EnqueueOperationReconcileTx atomically enqueues an operation reconciliation
// job in the caller's PostgreSQL transaction.
func (d *RiverDispatcher) EnqueueOperationReconcileTx(ctx context.Context, tx pgx.Tx, operationID string) (*rivertype.JobInsertResult, error) {
	return ownerstore.EnqueueInTx(ctx, d.client, tx, OperationReconcileJobArgs{
		OperationID: operationID,
	}, nil)
}
