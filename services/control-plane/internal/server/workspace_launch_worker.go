package server

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"time"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
)

const (
	defaultWorkspaceLaunchInterval   = 10 * time.Second
	workspaceLaunchWorkerConcurrency = 4
	workspaceLaunchWorkerPageSize    = workspaceLaunchWorkerConcurrency
)

type workspaceLaunchWorkerResult struct {
	accountID string
	err       error
}

// One scheduler owns its cursor and in-flight accounts. Execution completes
// independently so an unrelated account can advance on the next tick.
type workspaceLaunchScheduler struct {
	app            *controlPlaneServer
	service        *controlplane.Service
	afterCreatedAt time.Time
	afterID        string
	roundComplete  bool
	inFlight       map[string]struct{}
	completed      chan workspaceLaunchWorkerResult
}

func newWorkspaceLaunchScheduler(app *controlPlaneServer, service *controlplane.Service) *workspaceLaunchScheduler {
	return &workspaceLaunchScheduler{
		app: app, service: service, inFlight: make(map[string]struct{}),
		completed: make(chan workspaceLaunchWorkerResult, workspaceLaunchWorkerConcurrency),
	}
}

func (scheduler *workspaceLaunchScheduler) collect(wait bool) error {
	var errs []error
	for len(scheduler.inFlight) > 0 {
		var result workspaceLaunchWorkerResult
		if wait {
			result = <-scheduler.completed
		} else {
			select {
			case result = <-scheduler.completed:
			default:
				return errors.Join(errs...)
			}
		}
		delete(scheduler.inFlight, result.accountID)
		errs = append(errs, result.err)
	}
	return errors.Join(errs...)
}

func (scheduler *workspaceLaunchScheduler) beginRound(ctx context.Context) error {
	if scheduler.roundComplete {
		scheduler.afterCreatedAt, scheduler.afterID = time.Time{}, ""
		scheduler.roundComplete = false
	}
	return scheduler.dispatch(ctx)
}

func (scheduler *workspaceLaunchScheduler) dispatch(ctx context.Context) error {
	errs := []error{scheduler.collect(false)}
	if err := ctx.Err(); err != nil {
		return errors.Join(append(errs, err)...)
	}
	if scheduler.roundComplete || len(scheduler.inFlight) >= workspaceLaunchWorkerConcurrency {
		return errors.Join(errs...)
	}
	query := runtimeOperationQuery{
		Action: workspaceLaunchAction, ExcludedStatuses: []string{
			string(contracts.StatusSucceeded), string(contracts.StatusRefunded), string(contracts.StatusFailed),
		},
		AfterCreatedAt: scheduler.afterCreatedAt, AfterID: scheduler.afterID,
		Limit: workspaceLaunchWorkerPageSize,
	}
	page, err := scheduler.app.tables.PageRuntimeOperations(ctx, query)
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	for _, row := range page.Items {
		if len(scheduler.inFlight) >= workspaceLaunchWorkerConcurrency {
			return errors.Join(errs...)
		}
		createdAt, valid := parseTimeString(stringValue(row["createdAt"]))
		if !valid || stringValue(row["id"]) == "" {
			return errors.Join(append(errs, errInvalidWorkspaceLaunchOperation)...)
		}
		scheduler.afterCreatedAt, scheduler.afterID = createdAt, stringValue(row["id"])
		operation, err := decodeWorkspaceLaunchReconcileOperation(row)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		accountID := operation.stringFact("accountId")
		if _, running := scheduler.inFlight[accountID]; running {
			continue
		}
		scheduler.inFlight[accountID] = struct{}{}
		go func() {
			unlock, err := scheduler.app.lockResourceContext(ctx, "workspace-launch", accountID)
			if err == nil {
				if operation.Status == contracts.StatusManualReview {
					_, err = scheduler.app.workspaceLaunchReconciler(scheduler.service, clients.SessionDelegatedCredential{}, 0).AutoRecoverManualReview(ctx, operation.ID)
				} else {
					err = scheduler.app.runWorkspaceLaunch(ctx, scheduler.service, operation.ID)
				}
				unlock()
			}
			scheduler.completed <- workspaceLaunchWorkerResult{accountID: accountID, err: err}
		}()
	}
	if page.Total <= len(page.Items) {
		scheduler.roundComplete = true
	}
	return errors.Join(errs...)
}

func workspaceLaunchWorkerEnabled() bool {
	value := strings.TrimSpace(os.Getenv("OPL_WORKSPACE_LAUNCH_WORKER_ENABLED"))
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func workspaceLaunchWorkerInterval() time.Duration {
	return durationFromEnv("OPL_WORKSPACE_LAUNCH_INTERVAL_MS", defaultWorkspaceLaunchInterval)
}

func (app *controlPlaneServer) startWorkspaceLaunchWorker(ctx context.Context, service *controlplane.Service, interval time.Duration) {
	if interval <= 0 {
		interval = defaultWorkspaceLaunchInterval
	}
	go func() {
		scheduler := newWorkspaceLaunchScheduler(app, service)
		if err := scheduler.dispatch(ctx); err != nil {
			log.Printf("workspace launch failed: %v", err)
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case result := <-scheduler.completed:
				delete(scheduler.inFlight, result.accountID)
				if result.err != nil {
					log.Printf("workspace launch failed: %v", result.err)
				}
				if err := scheduler.dispatch(ctx); err != nil {
					log.Printf("workspace launch failed: %v", err)
				}
			case <-ticker.C:
				if err := scheduler.beginRound(ctx); err != nil {
					log.Printf("workspace launch failed: %v", err)
				}
			}
		}
	}()
}
