// Command opl-cloud is the unified Cloud backend executable and composition entrypoint.
//
// In its default composition mode (`opl-cloud` or `opl-cloud-composition`), it
// reads the owner topology contract at runtime, starts all declared domain
// modules in-process as concurrent goroutines within one PID / one Go runtime,
// wires owneridentity.InProcTransport for internal owner RPCs while exposing
// external TCP+mTLS/HTTP listeners, holds each module back until its upstream
// dependencies satisfy their declared readiness criteria, and fails fast with
// the canonical opl-cloud-composition failure code naming any failing owner.
//
// When invoked via BusyBox-style symlinks (`opl-<service>`) or `opl-cloud run
// <service>`, it dispatches that individual domain module standalone.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	consolebffmodule "opl-cloud/apps/console-bff/module"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
	buildmodule "opl-cloud/services/build/module"
	capabilitymodule "opl-cloud/services/capability/module"
	"opl-cloud/services/cloud-composition/composition"
	controlplanemodule "opl-cloud/services/control-plane/module"
	fabricmodule "opl-cloud/services/fabric/module"
	gatewayintegrationmodule "opl-cloud/services/gateway-integration/module"
	ledgermodule "opl-cloud/services/ledger/module"
	resourcecatalogmodule "opl-cloud/services/resource-catalog/module"
	runtimecontrolmodule "opl-cloud/services/runtime-control/module"
	servemodule "opl-cloud/services/serve/module"
	workspacemodule "opl-cloud/services/workspace/module"
)

const topologyEnv = "OPL_CLOUD_OWNER_TOPOLOGY"

var allModules = map[string]composition.ModuleRunner{
	"gateway-integration": gatewayintegrationmodule.Run,
	"capability":          capabilitymodule.Run,
	"build":               buildmodule.Run,
	"runtime-control":     runtimecontrolmodule.Run,
	"workspace":           workspacemodule.Run,
	"resource-catalog":    resourcecatalogmodule.Run,
	"serve":               servemodule.Run,
	"fabric":              fabricmodule.Run,
	"ledger":              ledgermodule.Run,
	"console-bff":         consolebffmodule.Run,
	"control-plane":       controlplanemodule.Run,
}

var binaryDispatch = map[string]composition.ModuleRunner{
	"opl-gateway-integration": gatewayintegrationmodule.Run,
	"opl-capability":          capabilitymodule.Run,
	"opl-build":               buildmodule.Run,
	"opl-runtime-control":     runtimecontrolmodule.Run,
	"opl-workspace":           workspacemodule.Run,
	"opl-resource-catalog":    resourcecatalogmodule.Run,
	"opl-serve":               servemodule.Run,
	"opl-fabric":              fabricmodule.Run,
	"opl-ledger":              ledgermodule.Run,
	"opl-console-bff":         consolebffmodule.Run,
	"opl-control-plane":       controlplanemodule.Run,
}

func main() {
	os.Exit(runBinary(filepath.Base(os.Args[0]), os.Args[1:]))
}

func runBinary(name string, args []string) int {
	if name == "opl-cloud" && len(args) > 0 && args[0] == "run" {
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "usage: opl-cloud run <service> [args...]")
			return 2
		}
		target := "opl-" + strings.TrimPrefix(args[1], "opl-")
		return runBinary(target, args[2:])
	}

	switch name {
	case "opl-workspace":
		if len(args) > 0 && args[0] == "retry-initial-runtime" {
			return workspacemodule.RunInitialRuntimeRetryCLI(args[1:], os.Stdin, os.Stdout)
		}
	case "opl-serve":
		if len(args) > 0 && args[0] == "probe-workspace-access" {
			return servemodule.RunWorkspaceAccessProbe(args[1:])
		}
	}

	if runner, ok := binaryDispatch[name]; ok {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()
		if err := runner(ctx, owneridentity.OSModuleEnv()); err != nil {
			log.Printf("%s: %v", name, err)
			return 1
		}
		return 0
	}

	if name == "opl-cloud" || name == "opl-cloud-composition" {
		return runCompositionCLI(args)
	}

	fmt.Fprintf(os.Stderr, "unknown opl-cloud applet %q\n", name)
	return 2
}

func runCompositionCLI(args []string) int {
	flags := flag.NewFlagSet("opl-cloud-composition", flag.ContinueOnError)
	topologyPath := flags.String("topology", os.Getenv(topologyEnv), "path of the owner topology contract to compose")
	readinessTimeout := flags.Duration("readiness-timeout", 2*time.Minute, "how long one owner may take to become ready")
	readinessInterval := flags.Duration("readiness-interval", 500*time.Millisecond, "how often a not-ready owner is probed again")
	shutdownTimeout := flags.Duration("shutdown-timeout", 30*time.Second, "how long an owner may take to exit after a stop signal")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	logger := log.New(os.Stderr, "opl-cloud-composition: ", log.LstdFlags|log.LUTC)
	if err := runComposition(*topologyPath, *readinessTimeout, *readinessInterval, *shutdownTimeout, logger); err != nil {
		logger.Printf("composition failed: %v", err)
		var failure *composition.Failure
		if errors.As(err, &failure) {
			fmt.Fprintln(os.Stderr, contracts.FormatCompositionFailure(failure.Code, failure.Owner))
		}
		return 1
	}
	return 0
}

func runComposition(topologyPath string, readinessTimeout, readinessInterval, shutdownTimeout time.Duration, logger *log.Logger) error {
	if strings.TrimSpace(topologyPath) == "" {
		return composition.NewFailure(contracts.CompositionFailureOwnerTopologyRequired, composition.CompositionSubject, fmt.Errorf("the owner topology contract is required: pass --topology or set %s", topologyEnv))
	}
	topology, err := composition.Load(topologyPath)
	if err != nil {
		return composition.NewFailure(contracts.CompositionFailureOwnerTopologyInvalid, composition.CompositionSubject, err)
	}
	plan, err := composition.NewPlan(topology, os.Getenv)
	if err != nil {
		return composition.NewFailure(contracts.CompositionFailureOwnerPlanInvalid, composition.CompositionSubject, err)
	}
	logger.Printf("composing %d owner processes from %s (in-process)", len(plan.Steps), topologyPath)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	composer := composition.Composer{
		Plan:    plan,
		Checker: composition.NewChecker(os.Getenv),
		Starter: &composition.InProcStarter{
			Modules: allModules,
			InProc:  owneridentity.NewInProcTransport(),
		},
		Timeout:         readinessTimeout,
		Interval:        readinessInterval,
		ShutdownTimeout: shutdownTimeout,
		Logf: func(format string, args ...any) {
			logger.Printf(format, args...)
		},
	}
	return composer.Run(ctx)
}
