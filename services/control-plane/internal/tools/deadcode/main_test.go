package main

import (
	"reflect"
	"testing"
)

func TestParseProductionDeadcodeFiltersTestsGeneratedAndOtherPackages(t *testing.T) {
	raw := []byte(`[
		{
			"Name": "server",
			"Path": "opl-cloud/services/control-plane/internal/server",
			"Funcs": [
				{"Name": "deadProdFunc", "Position": {"File": "internal/server/routes.go", "Line": 42, "Col": 6}},
				{"Name": "controlPlaneServer.deadMethod", "Position": {"File": "internal/server/server.go", "Line": 100, "Col": 28}},
				{"Name": "unusedTestHelper", "Position": {"File": "internal/server/server_test.go", "Line": 12, "Col": 6}},
				{"Name": "generatedFunc", "Position": {"File": "internal/server/gen.go", "Line": 5, "Col": 6}, "Generated": true},
				{"Name": "markerMethod", "Position": {"File": "internal/server/iface.go", "Line": 9, "Col": 6}, "Marker": true}
			]
		},
		{
			"Name": "other",
			"Path": "opl-cloud/services/control-plane/internal/other",
			"Funcs": [
				{"Name": "ignoredOtherPkg", "Position": {"File": "internal/other/file.go", "Line": 1, "Col": 6}}
			]
		}
	]`)
	got, err := parseProductionDeadcode(raw, defaultServerImportPath)
	if err != nil {
		t.Fatalf("parseProductionDeadcode returned error: %v", err)
	}
	want := map[string]string{
		"deadProdFunc":                  "internal/server/routes.go:42",
		"controlPlaneServer.deadMethod": "internal/server/server.go:100",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseProductionDeadcode() = %#v, want %#v", got, want)
	}
}

func TestDiffDeadcodeReportsUnexpectedAndStaleEntries(t *testing.T) {
	dead := map[string]string{
		"keptAllowed": "a.go:1",
		"newDead":     "b.go:2",
	}
	allow := map[string]bool{
		"keptAllowed": true,
		"staleEntry":  true,
	}
	unexpected, stale := diffDeadcode(dead, allow)
	if !reflect.DeepEqual(unexpected, []string{"newDead"}) {
		t.Fatalf("unexpected = %v, want [newDead]", unexpected)
	}
	if !reflect.DeepEqual(stale, []string{"staleEntry"}) {
		t.Fatalf("stale = %v, want [staleEntry]", stale)
	}
}
