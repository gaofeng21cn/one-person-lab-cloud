package server

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"opl-cloud/services/control-plane/internal/controlplane"
)

const (
	defaultMonthlyBillingInterval = time.Minute
	monthlyRenewalLead            = 24 * time.Hour
	monthlyBillingWorkspacePage   = 50
)

func monthlyBillingWorkerEnabled() bool {
	value := strings.TrimSpace(os.Getenv("OPL_MONTHLY_BILLING_WORKER_ENABLED"))
	return value == "1" || strings.EqualFold(value, "true") || strings.EqualFold(value, "yes")
}

func monthlyBillingWorkerInterval() time.Duration {
	return durationFromEnv("OPL_MONTHLY_BILLING_INTERVAL_MS", defaultMonthlyBillingInterval)
}

func (app *controlPlaneServer) startMonthlyBillingWorker(ctx context.Context, service *controlplane.Service, interval time.Duration) {
	if interval <= 0 {
		interval = defaultMonthlyBillingInterval
	}
	go func() {
		for {
			now := time.Now().UTC()
			next, err := app.runMonthlyBillingSweep(ctx, service, now)
			if err != nil {
				log.Printf("monthly billing failed: %v", err)
			}
			delay := interval
			if !next.IsZero() {
				until := time.Until(next)
				if until < 0 {
					until = 0
				}
				if until < delay {
					delay = until
				}
			}
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func (app *controlPlaneServer) runMonthlyBillingOnce(ctx context.Context, service *controlplane.Service, now time.Time) error {
	_, err := app.runMonthlyBillingSweep(ctx, service, now)
	return err
}

func (app *controlPlaneServer) runMonthlyBillingSweep(ctx context.Context, service *controlplane.Service, now time.Time) (time.Time, error) {
	var next time.Time
	recoveryOperations, err := queryRuntimeOperations(ctx, app.tables, runtimeOperationQuery{
		Action: "workspace.renewal", Statuses: []string{"verifying"},
	})
	if err != nil {
		return next, err
	}
	recoveryWorkspaces := make(map[string]struct{}, len(recoveryOperations))
	for _, operation := range recoveryOperations {
		if workspaceID := stringValue(operation["workspaceId"]); workspaceID != "" {
			recoveryWorkspaces[workspaceID] = struct{}{}
		}
	}

	var errs []error
	for offset := 0; ; {
		page, err := app.tables.PageWorkspaces(ctx, "", tablePageQuery{Offset: offset, Limit: monthlyBillingWorkspacePage})
		if err != nil {
			return next, errors.Join(append(errs, err)...)
		}
		for _, workspace := range page.Items {
			state, present, stateErr := normalizeWorkspaceBillingStateForWorkspace(workspace, workspace)
			if stateErr != nil {
				errs = append(errs, fmt.Errorf("workspace %s: %w", stringValue(workspace["id"]), stateErr))
				continue
			}
			if !present {
				continue
			}
			paidThrough, _ := time.Parse(time.RFC3339, state.PaidThrough)
			deadlines := []time.Time{paidThrough}
			if state.AutoRenew {
				deadlines = append(deadlines, paidThrough.Add(-monthlyRenewalLead))
			}
			for _, deadline := range deadlines {
				if deadline.After(now) && (next.IsZero() || deadline.Before(next)) {
					next = deadline
				}
			}
			workspaceID := stringValue(workspace["id"])
			_, recovering := recoveryWorkspaces[workspaceID]
			if !recovering && !workspaceRenewalDue(state, now) {
				continue
			}
			if err := app.processWorkspaceRenewal(ctx, service, workspaceID, now.UTC()); err != nil && !monthlyBusinessOutcome(err) {
				errs = append(errs, fmt.Errorf("workspace %s: %w", stringValue(workspace["id"]), err))
			}
		}
		offset += len(page.Items)
		if offset >= page.Total || len(page.Items) == 0 {
			break
		}
	}
	return next, errors.Join(errs...)
}

func workspaceRenewalDue(state workspaceBillingState, now time.Time) bool {
	if state.ResourceBillingEnabled != nil && !*state.ResourceBillingEnabled {
		return false
	}
	paidThrough, err := time.Parse(time.RFC3339, state.PaidThrough)
	if err != nil {
		return false
	}
	return !now.UTC().Before(paidThrough.UTC()) || state.AutoRenew && !now.UTC().Before(paidThrough.UTC().Add(-monthlyRenewalLead))
}

func monthlyBusinessOutcome(err error) bool {
	return errors.Is(err, errMonthlyInsufficientBalance) || errors.Is(err, errMonthlyAccountUnmapped)
}
