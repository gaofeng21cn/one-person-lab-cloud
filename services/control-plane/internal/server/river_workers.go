package server

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
	"opl-cloud/services/internal/ownerstore"
)

const (
	riverQueueWorkspaceLaunch = "control_plane_workspace_launch"
	riverQueueMaintenance     = "control_plane_maintenance"
)

// WorkspaceLaunchJobArgs executes one durable workspace launch or manual-review
// auto-recovery attempt via River's PostgreSQL-native job queue.
type WorkspaceLaunchJobArgs struct {
	OperationID string `json:"operationId"`
	AccountID   string `json:"accountId"`
	Manual      bool   `json:"manual,omitempty"`
}

func (WorkspaceLaunchJobArgs) Kind() string { return "control_plane.workspace_launch" }

func (WorkspaceLaunchJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       riverQueueWorkspaceLaunch,
		MaxAttempts: 25,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 30 * time.Second,
		},
	}
}

type workspaceLaunchJobWorker struct {
	river.WorkerDefaults[WorkspaceLaunchJobArgs]
	app     *controlPlaneServer
	service *controlplane.Service
}

func (w *workspaceLaunchJobWorker) Work(ctx context.Context, job *river.Job[WorkspaceLaunchJobArgs]) error {
	unlock, err := w.app.lockResourceContext(ctx, "workspace-launch", job.Args.AccountID)
	if err != nil {
		return err
	}
	defer unlock()
	if job.Args.Manual {
		_, err = w.app.workspaceLaunchReconciler(w.service, clients.SessionDelegatedCredential{}, 0).AutoRecoverManualReview(ctx, job.Args.OperationID)
		return err
	}
	return w.app.runWorkspaceLaunch(ctx, w.service, job.Args.OperationID)
}

// WorkspaceLaunchSweepJobArgs scans open workspace launch operations and
// enqueues per-operation durable River jobs inside one PostgreSQL transaction.
type WorkspaceLaunchSweepJobArgs struct{}

func (WorkspaceLaunchSweepJobArgs) Kind() string { return "control_plane.workspace_launch_sweep" }

func (WorkspaceLaunchSweepJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       riverQueueMaintenance,
		MaxAttempts: 3,
		UniqueOpts: river.UniqueOpts{
			ByArgs:   true,
			ByPeriod: 5 * time.Second,
		},
	}
}

type workspaceLaunchSweepWorker struct {
	river.WorkerDefaults[WorkspaceLaunchSweepJobArgs]
	app  *controlPlaneServer
	pool *pgxpool.Pool
}

func (w *workspaceLaunchSweepWorker) Work(ctx context.Context, _ *river.Job[WorkspaceLaunchSweepJobArgs]) error {
	query := runtimeOperationQuery{
		Action: workspaceLaunchAction,
		ExcludedStatuses: []string{
			string(contracts.StatusSucceeded), string(contracts.StatusRefunded), string(contracts.StatusFailed),
		},
		Limit: workspaceLaunchWorkerPageSize * 4,
	}
	page, err := w.app.tables.PageRuntimeOperations(ctx, query)
	if err != nil {
		return err
	}
	if len(page.Items) == 0 || w.app.riverClient == nil {
		return nil
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var errs []error
	for _, row := range page.Items {
		operation, decodeErr := decodeWorkspaceLaunchReconcileOperation(row)
		if decodeErr != nil {
			errs = append(errs, decodeErr)
			continue
		}
		if _, insertErr := ownerstore.EnqueueInTx(ctx, w.app.riverClient, tx, WorkspaceLaunchJobArgs{
			OperationID: operation.ID,
			AccountID:   operation.stringFact("accountId"),
			Manual:      operation.Status == contracts.StatusManualReview,
		}, nil); insertErr != nil {
			errs = append(errs, insertErr)
		}
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		return errors.Join(append(errs, commitErr)...)
	}
	return errors.Join(errs...)
}

type WorkspaceDeleteSweepJobArgs struct{}

func (WorkspaceDeleteSweepJobArgs) Kind() string { return "control_plane.workspace_delete_sweep" }

func (WorkspaceDeleteSweepJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: riverQueueMaintenance, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 5 * time.Second}}
}

type workspaceDeleteSweepWorker struct {
	river.WorkerDefaults[WorkspaceDeleteSweepJobArgs]
	app     *controlPlaneServer
	service *controlplane.Service
}

func (w *workspaceDeleteSweepWorker) Work(ctx context.Context, _ *river.Job[WorkspaceDeleteSweepJobArgs]) error {
	if err := w.app.runWorkspaceDeletesOnce(ctx, w.service); err != nil {
		log.Printf("workspace delete recovery failed: %v", err)
		return err
	}
	return nil
}

type WorkspaceAppDeploySweepJobArgs struct{}

func (WorkspaceAppDeploySweepJobArgs) Kind() string { return "control_plane.workspace_app_deploy_sweep" }

func (WorkspaceAppDeploySweepJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: riverQueueMaintenance, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 5 * time.Second}}
}

type workspaceAppDeploySweepWorker struct {
	river.WorkerDefaults[WorkspaceAppDeploySweepJobArgs]
	app     *controlPlaneServer
	service *controlplane.Service
}

func (w *workspaceAppDeploySweepWorker) Work(ctx context.Context, _ *river.Job[WorkspaceAppDeploySweepJobArgs]) error {
	if err := w.app.runWorkspaceApplicationDeploymentsOnce(ctx, w.service); err != nil {
		log.Printf("workspace application deployment recovery failed: %v", err)
		return err
	}
	return nil
}

type MonthlyBillingSweepJobArgs struct{}

func (MonthlyBillingSweepJobArgs) Kind() string { return "control_plane.monthly_billing_sweep" }

func (MonthlyBillingSweepJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: riverQueueMaintenance, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 15 * time.Second}}
}

type monthlyBillingSweepWorker struct {
	river.WorkerDefaults[MonthlyBillingSweepJobArgs]
	app     *controlPlaneServer
	service *controlplane.Service
}

func (w *monthlyBillingSweepWorker) Work(ctx context.Context, _ *river.Job[MonthlyBillingSweepJobArgs]) error {
	if _, err := w.app.runMonthlyBillingSweep(ctx, w.service, time.Now().UTC()); err != nil {
		log.Printf("monthly billing failed: %v", err)
		return err
	}
	return nil
}

type ProviderReconcileSweepJobArgs struct{}

func (ProviderReconcileSweepJobArgs) Kind() string { return "control_plane.provider_reconcile_sweep" }

func (ProviderReconcileSweepJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: riverQueueMaintenance, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: 30 * time.Second}}
}

type providerReconcileSweepWorker struct {
	river.WorkerDefaults[ProviderReconcileSweepJobArgs]
	app     *controlPlaneServer
	service *controlplane.Service
}

func (w *providerReconcileSweepWorker) Work(ctx context.Context, _ *river.Job[ProviderReconcileSweepJobArgs]) error {
	if err := w.app.runProviderReconcileOnce(ctx, w.service, time.Now().UTC()); err != nil {
		log.Printf("provider reconcile failed: %v", err)
		return err
	}
	return nil
}

type ArchiveRetentionSweepJobArgs struct{}

func (ArchiveRetentionSweepJobArgs) Kind() string { return "control_plane.archive_retention_sweep" }

func (ArchiveRetentionSweepJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: riverQueueMaintenance, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByArgs: true, ByPeriod: time.Minute}}
}

type archiveRetentionSweepWorker struct {
	river.WorkerDefaults[ArchiveRetentionSweepJobArgs]
	app *controlPlaneServer
}

func (w *archiveRetentionSweepWorker) Work(ctx context.Context, _ *river.Job[ArchiveRetentionSweepJobArgs]) error {
	if err := w.app.runRetentionOnce(ctx); err != nil {
		log.Printf("retention failed: %v", err)
		return err
	}
	return nil
}

// startRiverWorkers migrates River's PostgreSQL tables in the control-plane's
// schema and starts one shared river.Client[pgx.Tx] running all enabled
// background workers and periodic recovery sweeps on the existing pgxpool.
func (app *controlPlaneServer) startRiverWorkers(ctx context.Context, service *controlplane.Service, pool *pgxpool.Pool) error {
	workers := river.NewWorkers()
	river.AddWorker(workers, &workspaceLaunchJobWorker{app: app, service: service})
	river.AddWorker(workers, &workspaceLaunchSweepWorker{app: app, pool: pool})
	river.AddWorker(workers, &workspaceDeleteSweepWorker{app: app, service: service})
	river.AddWorker(workers, &workspaceAppDeploySweepWorker{app: app, service: service})
	river.AddWorker(workers, &monthlyBillingSweepWorker{app: app, service: service})
	river.AddWorker(workers, &providerReconcileSweepWorker{app: app, service: service})
	river.AddWorker(workers, &archiveRetentionSweepWorker{app: app})

	var periodic []*river.PeriodicJob
	if monthlyBillingWorkerEnabled() {
		periodic = append(periodic, river.NewPeriodicJob(
			river.PeriodicInterval(monthlyBillingWorkerInterval()),
			func() (river.JobArgs, *river.InsertOpts) { return MonthlyBillingSweepJobArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		))
	}
	if workspaceLaunchWorkerEnabled() {
		interval := workspaceLaunchWorkerInterval()
		periodic = append(periodic,
			river.NewPeriodicJob(
				river.PeriodicInterval(interval),
				func() (river.JobArgs, *river.InsertOpts) { return WorkspaceLaunchSweepJobArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
			river.NewPeriodicJob(
				river.PeriodicInterval(interval),
				func() (river.JobArgs, *river.InsertOpts) { return WorkspaceDeleteSweepJobArgs{}, nil },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		)
	}
	if workspaceApplicationDeploymentWorkerEnabled() {
		periodic = append(periodic, river.NewPeriodicJob(
			river.PeriodicInterval(workspaceApplicationDeploymentWorkerInterval),
			func() (river.JobArgs, *river.InsertOpts) { return WorkspaceAppDeploySweepJobArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		))
	}
	if providerReconcileWorkerEnabled() {
		periodic = append(periodic, river.NewPeriodicJob(
			river.PeriodicInterval(providerReconcileInterval()),
			func() (river.JobArgs, *river.InsertOpts) { return ProviderReconcileSweepJobArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		))
	}
	if retentionWorkerEnabled() {
		periodic = append(periodic, river.NewPeriodicJob(
			river.PeriodicInterval(retentionWorkerInterval()),
			func() (river.JobArgs, *river.InsertOpts) { return ArchiveRetentionSweepJobArgs{}, nil },
			&river.PeriodicJobOpts{RunOnStart: true},
		))
	}

	if err := ownerstore.MigrateRiverSchema(ctx, pool, ""); err != nil {
		return err
	}
	client, err := ownerstore.NewRiverClient(pool, ownerstore.RiverClientConfig{
		Workers:      workers,
		PeriodicJobs: periodic,
		Queues: map[string]river.QueueConfig{
			riverQueueWorkspaceLaunch: {MaxWorkers: workspaceLaunchWorkerConcurrency},
			riverQueueMaintenance:     {MaxWorkers: 2},
		},
	})
	if err != nil {
		return err
	}
	app.riverClient = client
	if len(periodic) == 0 {
		return nil
	}
	return client.Start(ctx)
}

func (app *controlPlaneServer) stopRiverWorkers(ctx context.Context) error {
	if app == nil || app.riverClient == nil {
		return nil
	}
	stopCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return app.riverClient.Stop(stopCtx)
}
