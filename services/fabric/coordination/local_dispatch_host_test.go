//go:build !linux

package coordination_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/fabric/coordination"
	"opl-cloud/services/fabric/internal/fabric"
)

// dockerFixtureCallsLogPlaceholder is substituted with the quoted absolute
// calls-log path when the scratch Docker fixture is generated.
const dockerFixtureCallsLogPlaceholder = "__DOCKER_FIXTURE_CALLS_LOG__"

// dockerInfoFixtureScript is a scratch stand-in for the Docker binary. It
// answers only the provider's exact version readback, refuses every other argv
// with exit status 1, and first appends the full argv of every invocation to
// the calls log.
const dockerInfoFixtureScript = `#!/bin/sh
log="__DOCKER_FIXTURE_CALLS_LOG__"
{
	sep=""
	for arg in "$@"; do
		printf '%s%s' "$sep" "$arg"
		sep=" "
	done
	printf '\n'
} >> "$log"
if [ "$#" -ne 3 ] || [ "$1" != "info" ] || [ "$2" != "--format" ] || [ "$3" != "{{.ServerVersion}}" ]; then
	printf 'fixture_refused_unexpected_argv\n' >&2
	exit 1
fi
printf '29.0.0-test-fixture\n'
exit 0
`

// The real provider must reject an unsupported host before creating a network;
// Docker Desktop's Linux daemon is not the Fabric host's project-quota backend.
func TestLocalDispatcherRejectsUnsupportedHostQuota(t *testing.T) {
	t.Setenv("OPL_FABRIC_LOCAL_DOCKER_PROVIDER_PROFILE_JSON", `{"schemaVersion":1,"profileId":"profile","capabilityVersion":"v1","region":"local","accountBindings":[{"tenantId":"tenant","accountId":"account"}],"packages":[{"id":"package","name":"Package","available":true,"compute":{"id":"compute-sku","server":"2c4g","cpu":2,"memoryGb":4,"instanceType":"local-2c4g"},"storage":{"id":"storage-sku","sizeGb":10,"quotaPolicy":"linux-project"}}]}`)

	scratch := t.TempDir()
	script, callsLog := writeDockerInfoFixture(t, scratch)
	// The provider must run this scratch fixture instead of any Docker binary.
	t.Setenv("OPL_FABRIC_DOCKER_BINARY", script)

	// The unmeasured control phase proves the fixture fails closed on argv it
	// was not built for; the script records that invocation too, so the calls
	// log is reset before the one measured provider phase.
	assertDockerFixtureRefusesUnexpectedArgv(t, script, callsLog)
	if err := os.Truncate(callsLog, 0); err != nil {
		t.Fatalf("reset docker fixture calls log: %v", err)
	}

	provider := fabric.NewLocalDockerProvider()
	service := fabric.NewService(provider)
	dispatcher := coordination.NewLocalDispatcher(service, provider)
	_, err := dispatcher.EnsureResources(context.Background(), coordination.ResourceIntent{TenantID: "tenant", WorkspaceID: "quota-guard-never-created", OperationID: "quota-guard", ComputeID: "quota-compute-never-created", StorageID: "quota-storage-never-created", Plan: &api.ResourcePlanSnapshot{Provider: "local-docker", ProviderProfileId: "profile", ProviderCapabilityVersion: "v1", Region: "local", BillingMode: "LOCAL_NO_CHARGE", ProviderComputeSkuId: "compute-sku", ProviderStorageSkuId: "storage-sku", Vcpus: 2, MemoryMib: 4096, CapacityGib: 10}})
	if err == nil || !strings.Contains(err.Error(), "local_docker_storage_quota_unavailable") {
		t.Fatalf("host quota guard=%v", err)
	}
	if _, found := service.GetComputeAllocation(context.Background(), "quota-compute-never-created"); found {
		t.Fatal("compute was dispatched before quota admission")
	}
	if call := readSingleDockerFixtureCall(t, callsLog); call != "info --format {{.ServerVersion}}" {
		t.Fatalf("docker fixture argv=%q", call)
	}
}

// writeDockerInfoFixture writes the scratch fixture script and returns its
// absolute path with the absolute calls log it appends to.
func writeDockerInfoFixture(t *testing.T, scratch string) (script, callsLog string) {
	t.Helper()
	script = filepath.Join(scratch, "docker-fixture")
	callsLog = filepath.Join(scratch, "docker-fixture-calls.log")
	body := strings.ReplaceAll(dockerInfoFixtureScript, dockerFixtureCallsLogPlaceholder, dockerFixtureQuote(callsLog))
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write docker fixture script: %v", err)
	}
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatalf("make docker fixture script executable: %v", err)
	}
	return script, callsLog
}

// dockerFixtureQuote escapes a path for the double-quoted POSIX sh assignment
// in the generated script so an absolute path with spaces still resolves.
func dockerFixtureQuote(path string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "`", "\\`", `$`, `\$`).Replace(path)
}

// assertDockerFixtureRefusesUnexpectedArgv runs the fixture directly, without
// Docker, and proves an argv it was not built for fails closed and is recorded.
func assertDockerFixtureRefusesUnexpectedArgv(t *testing.T, script, callsLog string) {
	t.Helper()
	var stderr bytes.Buffer
	command := exec.Command(script, "ps")
	command.Stdout = io.Discard
	command.Stderr = &stderr
	if err := command.Run(); err == nil {
		t.Fatal(`docker fixture accepted unexpected argv "ps"`)
	}
	if !strings.Contains(stderr.String(), "fixture_refused_unexpected_argv") {
		t.Fatalf("docker fixture refusal stderr=%q", stderr.String())
	}
	if call := readSingleDockerFixtureCall(t, callsLog); call != "ps" {
		t.Fatalf("refused docker fixture argv=%q", call)
	}
}

// readSingleDockerFixtureCall returns the one argv line the fixture recorded;
// a missing, empty or multi-line log fails the fixture itself.
func readSingleDockerFixtureCall(t *testing.T, callsLog string) string {
	t.Helper()
	raw, err := os.ReadFile(callsLog)
	if err != nil {
		t.Fatalf("read docker fixture calls: %v", err)
	}
	lines := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	if len(lines) != 1 || strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("docker fixture calls=%q, want exactly one argv line", raw)
	}
	return lines[0]
}
