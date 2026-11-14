// Command opl-owner-probe is the installer-facing owner health readback.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/packages/contracts/go/owneridentity"
)

const controlPlaneOwner = "control_plane"

// The Console BFF is a deployment unit, not a data owner. owneridentity keeps
// it as a transport principal, so it has no owner identity to parse and no
// owner gRPC health service to call. Its health criterion is its own HTTP
// surface.
const consoleBFFDeploymentUnit = "console_bff"

type result struct {
	SchemaVersion      int                        `json:"schemaVersion"`
	Owner              string                     `json:"owner"`
	Status             string                     `json:"status"`
	ErrorCode          string                     `json:"errorCode,omitempty"`
	Health             *healthResult              `json:"health,omitempty"`
	ProductSourceSHA   string                     `json:"productSourceSha,omitempty"`
	FabricReadiness    *contracts.FabricReadiness `json:"fabricReadiness,omitempty"`
	TransportPrincipal string                     `json:"transportPrincipal,omitempty"`
}

type healthResult struct {
	Status string `json:"status"`
}

type config struct {
	Owner             owneridentity.Owner
	Address           string
	ExpectedSourceSHA string
	Timeout           time.Duration
}

func main() {
	result, err := probeFromArgs(os.Args[1:], os.Getenv)
	if err != nil {
		result.Status = "failed"
		result.ErrorCode = errorCode(err)
	}
	if encodeErr := json.NewEncoder(os.Stdout).Encode(result); encodeErr != nil {
		os.Exit(1)
	}
	if result.Status != "ok" {
		os.Exit(1)
	}
}

func probeFromArgs(args []string, getenv func(string) string) (result, error) {
	flags := flag.NewFlagSet("opl-owner-probe", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	ownerArg := flags.String("owner", "", "owner identity or deployment-unit identity")
	addressArg := flags.String("addr", "", "owner gRPC address, or a deployment unit's HTTP address")
	expectedArg := flags.String("expected-source-sha", "", "expected baked ProductSourceSHA")
	timeoutArg := flags.Duration("timeout", 5*time.Second, "probe timeout")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return resultFor(*ownerArg), errors.New("usage_invalid")
	}
	ownerText := strings.TrimSpace(*ownerArg)
	if strings.EqualFold(ownerText, controlPlaneOwner) {
		return probeControlPlane(config{Address: *addressArg, ExpectedSourceSHA: *expectedArg, Timeout: *timeoutArg}, getenv)
	}
	if strings.EqualFold(ownerText, consoleBFFDeploymentUnit) {
		return probeConsoleBFF(config{Address: *addressArg, Timeout: *timeoutArg}, getenv)
	}
	owner, ok := owneridentity.Parse(ownerText)
	if !ok {
		return resultFor(ownerText), errors.New("owner_invalid")
	}
	if *timeoutArg <= 0 {
		return resultFor(owner.String()), errors.New("timeout_invalid")
	}
	address := strings.TrimSpace(*addressArg)
	if address == "" {
		address = strings.TrimSpace(getenv("OPL_" + strings.ToUpper(owner.String()) + "_ADDR"))
	}
	if address == "" {
		return resultFor(owner.String()), errors.New("address_missing")
	}
	expected := strings.TrimSpace(*expectedArg)
	if expected == "" {
		expected = strings.TrimSpace(getenv("OPL_PROBE_EXPECTED_SOURCE_SHA"))
	}
	return probeGRPC(config{Owner: owner, Address: address, ExpectedSourceSHA: expected, Timeout: *timeoutArg}, getenv)
}

func resultFor(owner string) result {
	return result{SchemaVersion: 1, Owner: strings.TrimSpace(owner), Status: "failed"}
}

func probeGRPC(cfg config, getenv func(string) string) (result, error) {
	out := resultFor(cfg.Owner.String())
	transportTarget := owneridentity.DeploymentUnitTransportPrincipal(cfg.Owner)
	out.TransportPrincipal = transportTarget.String()
	transport := owneridentity.TLSFromEnv(getenv)
	options, err := transport.HealthDialOptions(transportTarget, transportTarget)
	if err != nil {
		return out, fmt.Errorf("tls_config: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Timeout)
	defer cancel()
	conn, err := grpc.NewClient(cfg.Address, options...)
	if err != nil {
		return out, fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	response, err := healthpb.NewHealthClient(conn).Check(ctx, &healthpb.HealthCheckRequest{})
	if err != nil {
		return out, fmt.Errorf("health_rpc: %w", err)
	}
	out.Health = &healthResult{Status: response.GetStatus().String()}
	if response.GetStatus() != healthpb.HealthCheckResponse_SERVING {
		return out, errors.New("health_not_serving")
	}
	out.Status = "ok"
	return out, nil
}

func probeControlPlane(cfg config, getenv func(string) string) (result, error) {
	out := resultFor(controlPlaneOwner)
	if cfg.Timeout <= 0 {
		return out, errors.New("timeout_invalid")
	}
	base := strings.TrimSpace(cfg.Address)
	if base == "" {
		base = strings.TrimSpace(getenv("CONTROL_PLANE_ADDR"))
	}
	if base == "" {
		base = "127.0.0.1:8787"
	}
	base = httpBaseURL(base)
	client := &http.Client{Timeout: cfg.Timeout}
	healthResponse, err := getJSON(client, base+"/api/healthz")
	if err != nil {
		return out, fmt.Errorf("control_plane_health: %w", err)
	}
	sha, ok := healthResponse["productSourceSha"].(string)
	if !ok || strings.TrimSpace(sha) == "" {
		return out, errors.New("product_source_sha_missing")
	}
	out.ProductSourceSHA = strings.TrimSpace(sha)
	expected := strings.TrimSpace(cfg.ExpectedSourceSHA)
	if expected == "" {
		expected = strings.TrimSpace(getenv("OPL_RELEASE_SHA"))
	}
	if expected != "" && out.ProductSourceSHA != expected {
		return out, errors.New("product_source_sha_mismatch")
	}
	readinessBody, err := getJSON(client, base+"/api/runtime/readiness")
	if err != nil {
		return out, fmt.Errorf("fabric_readiness: %w", err)
	}
	encoded, err := json.Marshal(readinessBody)
	if err != nil {
		return out, fmt.Errorf("fabric_readiness_decode: %w", err)
	}
	var readiness contracts.FabricReadiness
	if err := json.Unmarshal(encoded, &readiness); err != nil {
		return out, fmt.Errorf("fabric_readiness_decode: %w", err)
	}
	out.FabricReadiness = &readiness
	if !readiness.ServiceReady {
		return out, errors.New("fabric_not_service_ready")
	}
	out.Status = "ok"
	return out, nil
}

// The Console BFF is a deployment unit rather than a data owner, so the owner
// gRPC health service is not its criterion: it publishes no owner identity to
// parse and no owner health service to call. Its own HTTP surface answers
// whether the Console is served, and that is what the installer reads.
func probeConsoleBFF(cfg config, getenv func(string) string) (result, error) {
	out := resultFor(consoleBFFDeploymentUnit)
	if cfg.Timeout <= 0 {
		return out, errors.New("timeout_invalid")
	}
	base := strings.TrimSpace(cfg.Address)
	if base == "" {
		base = strings.TrimSpace(getenv("OPL_BFF_ADDR"))
	}
	if base == "" {
		base = "127.0.0.1:8190"
	}
	base = httpBaseURL(base)
	client := &http.Client{Timeout: cfg.Timeout}
	health, err := getJSON(client, base+"/healthz")
	if err != nil {
		return out, fmt.Errorf("console_bff_health: %w", err)
	}
	status, ok := health["status"].(string)
	if !ok || strings.TrimSpace(status) == "" {
		return out, errors.New("console_bff_health_status_missing")
	}
	out.Health = &healthResult{Status: strings.TrimSpace(status)}
	if strings.TrimSpace(status) != "ok" {
		return out, errors.New("console_bff_not_ready")
	}
	out.Status = "ok"
	return out, nil
}

func getJSON(client *http.Client, target string) (map[string]any, error) {
	request, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http_status_%d", response.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, err
	}
	return body, nil
}

func httpBaseURL(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, ":") {
		return "http://127.0.0.1" + value
	}
	if !strings.Contains(value, "://") {
		return "http://" + value
	}
	return strings.TrimRight(value, "/")
}

func errorCode(err error) string {
	if err == nil {
		return ""
	}
	message := err.Error()
	if index := strings.IndexByte(message, ':'); index >= 0 {
		message = message[:index]
	}
	return strings.ToUpper(strings.ReplaceAll(message, "-", "_"))
}
