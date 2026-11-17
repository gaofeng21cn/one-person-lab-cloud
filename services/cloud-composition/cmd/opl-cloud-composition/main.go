// Command opl-cloud-composition is the Cloud backend composition entrypoint.
//
// The Cloud container is one process space in which the owner processes
// collaborate. This binary brings them up: it reads the owner topology
// contract at run time, starts each declared owner binary in the order the
// contract declares, holds an owner back until the owners it depends on are
// ready, and verifies every owner against the readiness criterion the
// contract declares for it. A required owner that never becomes ready stops
// the composition with an error that names the owner and the criterion.
//
// The contract is the only source of the process set, the order, the
// addresses and the criteria. The published image keeps its current default
// command, so an installation opts into this entrypoint explicitly.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"opl-cloud/services/cloud-composition/internal/composition"
)

// topologyEnv names the environment an installation uses to point the
// entrypoint at the owner topology contract.
const topologyEnv = "OPL_CLOUD_OWNER_TOPOLOGY"

func main() {
	topologyPath := flag.String("topology", topologyPathFromEnv(), "path of the owner topology contract to compose")
	readinessTimeout := flag.Duration("readiness-timeout", 2*time.Minute, "how long one owner may take to become ready")
	readinessInterval := flag.Duration("readiness-interval", 500*time.Millisecond, "how often a not-ready owner is probed again")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "how long an owner may take to exit after a stop signal")
	flag.Parse()

	logger := log.New(os.Stderr, "opl-cloud-composition: ", log.LstdFlags|log.LUTC)

	if err := run(*topologyPath, *readinessTimeout, *readinessInterval, *shutdownTimeout, logger); err != nil {
		logger.Printf("composition failed: %v", err)
		os.Exit(1)
	}
}

// run composes the declared owners and returns when the composition stops.
func run(topologyPath string, readinessTimeout, readinessInterval, shutdownTimeout time.Duration, logger *log.Logger) error {
	if strings.TrimSpace(topologyPath) == "" {
		return fmt.Errorf("the owner topology contract is required: pass --topology or set %s", topologyEnv)
	}
	topology, err := composition.Load(topologyPath)
	if err != nil {
		return err
	}
	plan, err := composition.NewPlan(topology, os.Getenv)
	if err != nil {
		return err
	}
	logger.Printf("composing %d owner processes from %s", len(plan.Steps), topologyPath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	composer := composition.Composer{
		Plan:            plan,
		Checker:         composition.NewChecker(os.Getenv),
		Starter:         &composition.ExecStarter{Stdout: os.Stdout, Stderr: os.Stderr},
		Timeout:         readinessTimeout,
		Interval:        readinessInterval,
		ShutdownTimeout: shutdownTimeout,
		Logf: func(format string, args ...any) {
			logger.Printf(format, args...)
		},
	}
	return composer.Run(ctx)
}

// topologyPathFromEnv returns the owner topology contract path the
// installation selected. The contract is a Candidate-only asset the
// installation carries, so the entrypoint reads the file the installation
// provides and never a copy of its own.
func topologyPathFromEnv() string {
	return os.Getenv(topologyEnv)
}
