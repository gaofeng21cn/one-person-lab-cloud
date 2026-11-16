package server

import (
	"context"
	"errors"

	"opl-cloud/services/control-plane/internal/controlplane"
)

// runWorkspaceLaunchRound drives the production launch scheduler the way the
// workspace launch worker drives one round, and waits for that round to
// finish.
//
// The worker's own loop keeps one scheduler and one cursor for the life of the
// process. A test that observes an independent round builds a fresh scheduler
// for it, so the round driver belongs with the tests: the server has exactly
// one launch entrypoint, the worker, and no second one-shot command beside it.
func runWorkspaceLaunchRound(ctx context.Context, app *controlPlaneServer, service *controlplane.Service) error {
	scheduler := newWorkspaceLaunchScheduler(app, service)
	err := scheduler.beginRound(ctx)
	return errors.Join(err, scheduler.collect(true))
}
