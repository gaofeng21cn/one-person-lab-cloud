package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTargetPackageCoversOwnerProductionPackages(t *testing.T) {
	cases := []struct {
		importPath string
		want       bool
	}{
		{"opl-cloud/apps/console-bff", true},
		{"opl-cloud/apps/console-bff/internal/clients", true},
		{"opl-cloud/apps/console-bff/internal/httpapi", true},
		{"opl-cloud/apps/console-bff/cmd/server", false},
		{"opl-cloud/services/fabric/internal/fabric", true},
		{"opl-cloud/services/fabric/internal/fabric/sqlcgen", true},
		{"opl-cloud/services/fabric/cmd/fabric", false},
		{"opl-cloud/services/fabric/cmd/opl-tencent-provisioner", false},
		{"opl-cloud/services/internal/ownerservice", true},
		{"opl-cloud/services/internal/ownerstore/ownerstoretest", true},
		{"opl-cloud/services/workspace/internal/launch", true},
		{"opl-cloud/services/control-plane/internal/server", true},
		{"opl-cloud/packages/contracts/go", false},
		{"opl-cloud/packages/contracts/go/api", false},
		{"opl-cloud/packages/contracts/go/cmd/opl-owner-probe", false},
		{"opl-cloud/tools/go/deadcode", false},
	}
	for _, testCase := range cases {
		if got := targetPackage(testCase.importPath); got != testCase.want {
			t.Errorf("targetPackage(%q) = %t, want %t", testCase.importPath, got, testCase.want)
		}
	}
}

func TestParseProductionDeadcodeUsesPackageQualifiedKeys(t *testing.T) {
	raw := []byte(`[
		{
			"Name": "server",
			"Path": "opl-cloud/services/control-plane/internal/server",
			"Funcs": [
				{"Name": "deadProdFunc", "Position": {"File": "services/control-plane/internal/server/routes.go", "Line": 42, "Col": 6}},
				{"Name": "controlPlaneServer.deadMethod", "Position": {"File": "services/control-plane/internal/server/server.go", "Line": 100, "Col": 28}},
				{"Name": "unusedTestHelper", "Position": {"File": "services/control-plane/internal/server/server_test.go", "Line": 12, "Col": 6}},
				{"Name": "generatedFunc", "Position": {"File": "services/control-plane/internal/server/gen.go", "Line": 5, "Col": 6}, "Generated": true},
				{"Name": "markerMethod", "Position": {"File": "services/control-plane/internal/server/iface.go", "Line": 9, "Col": 6}, "Marker": true}
			]
		},
		{
			"Name": "launch",
			"Path": "opl-cloud/services/workspace/internal/launch",
			"Funcs": [
				{"Name": "Service.deadMethod", "Position": {"File": "services/workspace/internal/launch/funding_proof.go", "Line": 58, "Col": 28}}
			]
		},
		{
			"Name": "other",
			"Path": "opl-cloud/services/other/internal/other",
			"Funcs": [
				{"Name": "ignoredOtherPkg", "Position": {"File": "services/other/internal/other/file.go", "Line": 1, "Col": 6}}
			]
		}
	]`)
	targets := []string{
		"opl-cloud/services/control-plane/internal/server",
		"opl-cloud/services/workspace/internal/launch",
	}
	got, err := parseProductionDeadcode(raw, targets)
	if err != nil {
		t.Fatalf("parseProductionDeadcode returned error: %v", err)
	}
	want := map[string]string{
		"opl-cloud/services/control-plane/internal/server.deadProdFunc":                  "services/control-plane/internal/server/routes.go:42",
		"opl-cloud/services/control-plane/internal/server.controlPlaneServer.deadMethod": "services/control-plane/internal/server/server.go:100",
		"opl-cloud/services/workspace/internal/launch.Service.deadMethod":                "services/workspace/internal/launch/funding_proof.go:58",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseProductionDeadcode() = %#v, want %#v", got, want)
	}
}

func TestDiffDeadcodeReportsUnexpectedAndStaleEntries(t *testing.T) {
	dead := map[string]string{
		"opl-cloud/services/workspace/internal/launch.Service.keptAllowed": "a.go:1",
		"opl-cloud/services/workspace/internal/launch.Service.newDead":     "b.go:2",
	}
	allow := map[string]bool{
		"opl-cloud/services/workspace/internal/launch.Service.keptAllowed": true,
		"opl-cloud/services/workspace/internal/launch.Service.staleEntry":  true,
	}
	unexpected, stale := diffDeadcode(dead, allow)
	if !reflect.DeepEqual(unexpected, []string{"opl-cloud/services/workspace/internal/launch.Service.newDead"}) {
		t.Fatalf("unexpected = %v, want the package-qualified new dead symbol", unexpected)
	}
	if !reflect.DeepEqual(stale, []string{"opl-cloud/services/workspace/internal/launch.Service.staleEntry"}) {
		t.Fatalf("stale = %v, want the package-qualified stale entry", stale)
	}
}

func TestReadAllowlistKeepsPackageQualifiedKeysAndRequiresAReason(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "allowlist.txt")
	content := "# A comment line.\n" +
		"opl-cloud/services/fabric/internal/fabric.workspaceDomain # superseded duplicate of the inline environment read\n" +
		"opl-cloud/services/workspace/internal/launch.Service.fundingProof\tsuperseded duplicate of fundingProofFor\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write allowlist: %v", err)
	}
	allow, err := readAllowlist(path)
	if err != nil {
		t.Fatalf("readAllowlist returned error: %v", err)
	}
	want := map[string]bool{
		"opl-cloud/services/fabric/internal/fabric.workspaceDomain":         true,
		"opl-cloud/services/workspace/internal/launch.Service.fundingProof": true,
	}
	if !reflect.DeepEqual(allow, want) {
		t.Fatalf("readAllowlist() = %#v, want %#v", allow, want)
	}

	withoutReason := filepath.Join(directory, "without-reason.txt")
	if err := os.WriteFile(withoutReason, []byte("opl-cloud/services/fabric/internal/fabric.workspaceDomain\n"), 0o600); err != nil {
		t.Fatalf("write allowlist without reason: %v", err)
	}
	if _, err := readAllowlist(withoutReason); err == nil {
		t.Fatal("readAllowlist must refuse an entry that carries no reason")
	}
}

func TestReadTargetsParsesDeclaredPackageList(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "targets.txt")
	content := "# A comment line.\n" +
		"opl-cloud/services/fabric/internal/fabric\n" +
		"opl-cloud/apps/console-bff # the Console BFF root package\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write targets: %v", err)
	}
	targets, err := readTargets(path)
	if err != nil {
		t.Fatalf("readTargets returned error: %v", err)
	}
	want := []string{
		"opl-cloud/apps/console-bff",
		"opl-cloud/services/fabric/internal/fabric",
	}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("readTargets() = %v, want %v", targets, want)
	}
}

func TestCompareTargetsReportsACoveredPackageThatIsNotDeclared(t *testing.T) {
	declared := []string{"opl-cloud/services/fabric/internal/fabric"}
	derived := []string{
		"opl-cloud/services/fabric/coordination",
		"opl-cloud/services/fabric/internal/fabric",
		"opl-cloud/services/fabric/internal/http",
	}
	err := compareTargets(declared, derived)
	if err == nil {
		t.Fatal("compareTargets must fail when a covered package is not declared")
	}
	for _, want := range []string{
		"opl-cloud/services/fabric/coordination",
		"opl-cloud/services/fabric/internal/http",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("compareTargets error %q must name %q", err, want)
		}
	}
	if err := compareTargets(derived, derived); err != nil {
		t.Fatalf("compareTargets must accept an equal list: %v", err)
	}
}
