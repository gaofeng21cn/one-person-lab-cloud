package ownerservice

import (
	"context"

	"opl-cloud/packages/contracts/go/owneridentity"
)

// ModuleEnv aliases owneridentity.ModuleEnv so both internal services and
// top-level entrypoints share one identical environment type.
type ModuleEnv = owneridentity.ModuleEnv

// OSModuleEnv wraps the host OS process environment for standalone CLI mode.
func OSModuleEnv() ModuleEnv {
	return owneridentity.OSModuleEnv()
}

// ServeContext serves server until ctx is canceled and stops it cleanly.
func ServeContext(ctx context.Context, server *Server) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			server.Stop()
		case <-done:
		}
	}()
	return server.Serve()
}
