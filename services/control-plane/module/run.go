// Package module runs the Control Plane service inside an owner module environment.
package module

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/services/control-plane/internal/clients"
	"opl-cloud/services/control-plane/internal/controlplane"
	controlserver "opl-cloud/services/control-plane/internal/server"
)

// InternalServiceTokens holds Control Plane's outbound Fabric and Ledger bearer tokens.
type InternalServiceTokens struct {
	Fabric string
	Ledger string
}

// Run starts the Control Plane HTTP server and blocks until ctx is canceled or serving fails.
func Run(ctx context.Context, env owneridentity.ModuleEnv) error {
	if ctx == nil {
		ctx = context.Background()
	}
	getenv := env.Env
	addr := ControlPlaneAddr(getenv)
	ledgerURL := getenv("LEDGER_URL")
	fabricURL := getenv("FABRIC_URL")
	outboundTokens, err := InternalServiceTokensFromEnv(getenv)
	if err != nil {
		return err
	}
	fabricCapabilityKey, err := FabricCapabilityKeyFromEnv(getenv, outboundTokens.Fabric)
	if err != nil {
		return err
	}
	ledgerCapabilityKey, err := LedgerCapabilityKeyFromEnv(getenv, outboundTokens.Ledger)
	if err != nil {
		return err
	}
	sub2APIConfig, err := Sub2APIConfigFromEnv(getenv)
	if err != nil {
		return err
	}
	var sub2API clients.Sub2APIClient
	if sub2APIConfig.BaseURL != "" {
		// Create HTTP client with proxy support from environment (HTTP_PROXY, HTTPS_PROXY, NO_PROXY)
		httpClient := &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				DialContext: (&net.Dialer{
					Timeout:   30 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
				MaxIdleConns:          100,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 1 * time.Second,
			},
		}
		sub2API, err = clients.NewSub2APIHTTPClient(sub2APIConfig, httpClient)
		if err != nil {
			return err
		}
	}

	service := controlplane.NewService(
		clients.NewLedgerHTTPClientWithCapability(ledgerURL, outboundTokens.Ledger, ledgerCapabilityKey, nil),
		clients.NewFabricHTTPClientWithCapability(fabricURL, outboundTokens.Fabric, fabricCapabilityKey, nil),
		sub2API,
	)
	store, err := controlserver.StateStoreFromEnvWith(getenv)
	if err != nil {
		return err
	}
	handler, err := controlserver.NewPersistentServerWithEnv(service, store, getenv, env.InProc)
	if err != nil {
		return err
	}
	if closer, ok := handler.(io.Closer); ok {
		defer closer.Close()
	}

	server := NewHTTPServer(addr, handler)
	done := make(chan struct{})
	shutdownDone := make(chan struct{})
	defer func() { close(done); <-shutdownDone }()
	go func() {
		defer close(shutdownDone)
		select {
		case <-ctx.Done():
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := server.Shutdown(shutdownCtx); err != nil {
				_ = server.Close()
			}
		case <-done:
		}
	}()
	log.Printf("control-plane listening on %s", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// LedgerCapabilityKeyFromEnv validates OPL_LEDGER_CAPABILITY_KEY against transport credentials.
func LedgerCapabilityKeyFromEnv(getenv func(string) string, ledgerTransportToken string) (string, error) {
	key := strings.TrimSpace(getenv("OPL_LEDGER_CAPABILITY_KEY"))
	if (getenv("NODE_ENV") == "production" || ledgerTransportToken != "" || key != "") && len(key) < 32 {
		return "", errors.New("OPL_LEDGER_CAPABILITY_KEY must contain at least 32 characters when Ledger transport is configured")
	}
	if key != "" && key == ledgerTransportToken {
		return "", errors.New("Ledger capability key must be distinct from its transport token")
	}
	return key, nil
}

// NewHTTPServer builds the configured HTTP server with Control Plane's connection timeouts.
func NewHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr: addr, Handler: handler,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: time.Hour, IdleTimeout: 2 * time.Minute,
	}
}

// Sub2APIConfigFromEnv parses and validates the external Sub2API client configuration.
func Sub2APIConfigFromEnv(getenv func(string) string) (clients.Sub2APIConfig, error) {
	mode := strings.TrimSpace(getenv("OPL_DEPLOYMENT_MODE"))
	switch mode {
	case "platform_owned", "managed_tke", "customer_owned":
	default:
		return clients.Sub2APIConfig{}, errors.New("OPL_DEPLOYMENT_MODE must be platform_owned, managed_tke, or customer_owned")
	}
	required := []string{
		"OPL_SUB2API_BASE_URL",
	}
	if mode == "customer_owned" {
		required = append(required, "OPL_SUB2API_USER_EMAIL", "OPL_SUB2API_USER_PASSWORD")
	} else {
		required = append(required, "OPL_SUB2API_ADMIN_EMAIL", "OPL_SUB2API_ADMIN_PASSWORD")
	}
	missing := make([]string, 0, len(required))
	configured := 0
	for _, key := range required {
		if strings.TrimSpace(getenv(key)) == "" {
			missing = append(missing, key)
		} else {
			configured++
		}
	}
	if len(missing) > 0 {
		if getenv("NODE_ENV") == "production" || configured > 0 {
			return clients.Sub2APIConfig{}, fmt.Errorf("missing required Sub2API configuration: %s", strings.Join(missing, ", "))
		}
		return clients.Sub2APIConfig{}, nil
	}
	timeoutMS := 5000
	if raw := strings.TrimSpace(getenv("OPL_SUB2API_REQUEST_TIMEOUT_MS")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 30_000 {
			return clients.Sub2APIConfig{}, errors.New("OPL_SUB2API_REQUEST_TIMEOUT_MS must be between 1 and 30000")
		}
		timeoutMS = parsed
	}
	return clients.Sub2APIConfig{
		BaseURL:       strings.TrimSpace(getenv("OPL_SUB2API_BASE_URL")),
		AdminEmail:    strings.TrimSpace(getenv("OPL_SUB2API_ADMIN_EMAIL")),
		AdminPassword: getenv("OPL_SUB2API_ADMIN_PASSWORD"),
		UserEmail:     strings.TrimSpace(getenv("OPL_SUB2API_USER_EMAIL")),
		UserPassword:  getenv("OPL_SUB2API_USER_PASSWORD"),
		Timeout:       time.Duration(timeoutMS) * time.Millisecond,
	}, nil
}

// InternalServiceTokensFromEnv reads and validates distinct Fabric and Ledger transport tokens.
func InternalServiceTokensFromEnv(getenv func(string) string) (InternalServiceTokens, error) {
	tokens := InternalServiceTokens{
		Fabric: strings.TrimSpace(getenv("OPL_FABRIC_SERVICE_TOKEN")),
		Ledger: strings.TrimSpace(getenv("OPL_LEDGER_SERVICE_TOKEN")),
	}
	configured := 0
	if tokens.Fabric != "" {
		configured++
	}
	if tokens.Ledger != "" {
		configured++
	}
	if getenv("NODE_ENV") == "production" || configured > 0 {
		missing := make([]string, 0, 2)
		if tokens.Fabric == "" {
			missing = append(missing, "OPL_FABRIC_SERVICE_TOKEN")
		}
		if tokens.Ledger == "" {
			missing = append(missing, "OPL_LEDGER_SERVICE_TOKEN")
		}
		if len(missing) > 0 {
			return InternalServiceTokens{}, fmt.Errorf("missing required outbound service token: %s", strings.Join(missing, ", "))
		}
	}
	if tokens.Fabric != "" && tokens.Fabric == tokens.Ledger {
		return InternalServiceTokens{}, errors.New("Fabric and Ledger outbound service tokens must be distinct")
	}
	return tokens, nil
}

// FabricCapabilityKeyFromEnv validates OPL_FABRIC_CAPABILITY_KEY against transport credentials.
func FabricCapabilityKeyFromEnv(getenv func(string) string, fabricTransportToken string) (string, error) {
	key := strings.TrimSpace(getenv("OPL_FABRIC_CAPABILITY_KEY"))
	if (getenv("NODE_ENV") == "production" || fabricTransportToken != "" || key != "") && len(key) < 32 {
		return "", errors.New("OPL_FABRIC_CAPABILITY_KEY must contain at least 32 characters when Fabric transport is configured")
	}
	if key != "" && key == fabricTransportToken {
		return "", errors.New("Fabric capability key must be distinct from its transport token")
	}
	return key, nil
}

// ControlPlaneAddr resolves Control Plane's HTTP bind address from getenv.
func ControlPlaneAddr(getenv func(string) string) string {
	var addr, port string
	if getenv != nil {
		addr = getenv("CONTROL_PLANE_ADDR")
		port = getenv("PORT")
	}
	if addr == "" && port != "" {
		addr = ":" + port
	}
	if addr == "" {
		addr = ":8787"
	}
	return addr
}
