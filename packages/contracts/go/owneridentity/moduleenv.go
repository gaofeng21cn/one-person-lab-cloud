package owneridentity

import "os"

// ModuleEnv provides an isolated per-module environment view and optional
// in-process gRPC transport for converged single-binary execution.
type ModuleEnv struct {
	Getenv    func(string) string
	LookupEnv func(string) (string, bool)
	InProc    *InProcTransport
}

// OSModuleEnv wraps the host OS process environment for standalone CLI mode.
func OSModuleEnv() ModuleEnv {
	return ModuleEnv{Getenv: os.Getenv, LookupEnv: os.LookupEnv}
}

// Env reads key from the scoped module environment.
func (e ModuleEnv) Env(key string) string {
	if e.Getenv != nil {
		return e.Getenv(key)
	}
	return os.Getenv(key)
}

// Lookup reads key and reports presence from the scoped module environment.
func (e ModuleEnv) Lookup(key string) (string, bool) {
	if e.LookupEnv != nil {
		return e.LookupEnv(key)
	}
	if e.Getenv != nil {
		v := e.Getenv(key)
		return v, v != ""
	}
	return os.LookupEnv(key)
}
