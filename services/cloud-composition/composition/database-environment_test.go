package composition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChildDatabaseUsesTheDeclaredCompositionSource(t *testing.T) {
	installation := environment{
		"OPL_GRPC_INSECURE_LOCAL":           "1",
		"DATABASE_URL":                      "postgres://legacy",
		"OPL_CAPABILITY_DATABASE_URL":       "postgres://capability",
		"OPL_BUILD_DATABASE_URL":            "postgres://build",
		"OPL_RUNTIME_CONTROL_DATABASE_URL":  "postgres://runtime-control",
		"OPL_WORKSPACE_DATABASE_URL":        "postgres://workspace",
		"OPL_SERVE_DATABASE_URL":            "postgres://serve",
		"OPL_RESOURCE_CATALOG_DATABASE_URL": "postgres://resource-catalog",
		"OPL_TENANT_DATABASE_URL":           "postgres://tenant",
		"OPL_GATEWAY_DATABASE_URL":          "postgres://gateway",
		"OPL_FABRIC_DATABASE_URL":           "postgres://coordination",
		"OPL_LEDGER_DATABASE_URL":           "postgres://must-not-replace-legacy",
		"OPL_CONTROL_PLANE_DATABASE_URL":    "postgres://must-not-replace-legacy",
	}
	for _, tc := range []struct{ service, want string }{
		{"capability", "postgres://capability"}, {"build", "postgres://build"},
		{"runtime-control", "postgres://runtime-control"}, {"workspace", "postgres://workspace"},
		{"serve", "postgres://serve"}, {"resource-catalog", "postgres://resource-catalog"},
		{"gateway-integration", "postgres://tenant"}, {"fabric", "postgres://legacy"},
		{"ledger", "postgres://legacy"}, {"control-plane", "postgres://legacy"}, {"console-bff", ""},
	} {
		t.Run(tc.service, func(t *testing.T) {
			child := projectedInstallation(t, installation, tc.service)
			if child["DATABASE_URL"] != tc.want {
				t.Fatalf("DATABASE_URL = %q, want %q", child["DATABASE_URL"], tc.want)
			}
			for name, value := range child {
				if name == "DATABASE_URL" || name == "OPL_FABRIC_DATABASE_URL" && tc.service == "fabric" || name == "OPL_GATEWAY_DATABASE_URL" && tc.service == "gateway-integration" {
					continue
				}
				if strings.Contains(name, "DATABASE_URL") {
					t.Fatalf("undeclared database credential %s=%s reached %s", name, value, tc.service)
				}
			}
			if tc.service == "fabric" && child["OPL_FABRIC_DATABASE_URL"] != "postgres://coordination" {
				t.Fatal("Fabric coordination database was replaced")
			}
			if tc.service == "gateway-integration" && child["OPL_GATEWAY_DATABASE_URL"] != "postgres://gateway" {
				t.Fatal("Gateway's separate database was replaced")
			}
		})
	}
}

func TestChildDatabaseProjectionUsesExplicitFieldsWithoutNameDerivation(t *testing.T) {
	process := Process{Service: "custom", TransportPrincipal: "custom", DatabaseEnv: "CHILD_DATABASE", CompositionDatabaseEnv: "INSTALLATION_DATABASE", AllowedEnv: []string{"CHILD_DATABASE"}}
	projected, err := ChildEnvironment(process, []string{"CHILD_DATABASE=wrong", "INSTALLATION_DATABASE=explicit"}, environment{"OPL_GRPC_INSECURE_LOCAL": "1"}.getenv)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(projected, "\n") != "CHILD_DATABASE=explicit" {
		t.Fatalf("unexpected projection: %v", projected)
	}
	projected, err = ChildEnvironment(process, []string{"CHILD_DATABASE=wrong"}, environment{"OPL_GRPC_INSECURE_LOCAL": "1"}.getenv)
	if err != nil {
		t.Fatal(err)
	}
	if len(projected) != 0 {
		t.Fatalf("missing composition input must not reuse a different database: %v", projected)
	}
}

func TestDatabaseConsumerWithoutCompositionDeclarationIsRejected(t *testing.T) {
	raw, err := os.ReadFile(contractPath(t))
	if err != nil {
		t.Fatal(err)
	}
	var contract map[string]any
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	processes := contract["processes"].([]any)
	delete(processes[0].(map[string]any), "compositionDatabaseEnv")
	mutated, err := json.Marshal(contract)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "topology.json")
	if err := os.WriteFile(path, mutated, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = Load(path)
	if err == nil || !strings.Contains(err.Error(), "capability") || !strings.Contains(err.Error(), "compositionDatabaseEnv") {
		t.Fatalf("missing declaration must name the owner and field: %v", err)
	}
	_, err = ChildEnvironment(Process{Service: "explicit", DatabaseEnv: "DATABASE_URL"}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "compositionDatabaseEnv") {
		t.Fatalf("direct process must require an explicit declaration: %v", err)
	}
}
