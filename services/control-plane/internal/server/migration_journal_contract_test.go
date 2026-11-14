package server

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The product publishes its own migration journal declaration, and the Control
// Plane's part of that declaration is the hand-written Go list
// `controlPlaneMigrations` returns: these versions exist nowhere on disk as
// `*.sql` files (`services/control-plane/migrations/migrations.go` embeds 19
// other files that are named by the list, not by their own stems). So the only
// thing that can prove the declaration still matches the code is the code
// itself.
//
// This test lives in this package because `controlPlaneMigrations` is
// unexported. It compares the declared versions with the list the product
// installs, in the product's own order.

const migrationJournalContractPath = "../../../../packages/contracts/opl-cloud-migration-journal-contract.json"

type migrationJournalSource struct {
	Kind   string `json:"kind"`
	File   string `json:"file"`
	Symbol string `json:"symbol"`
}

type migrationJournalEntry struct {
	Service  string                 `json:"service"`
	Source   migrationJournalSource `json:"source"`
	Versions []string               `json:"versions"`
}

type migrationJournalContract struct {
	SchemaVersion int                     `json:"schemaVersion"`
	Journals      []migrationJournalEntry `json:"journals"`
}

func readMigrationJournalContract(t *testing.T) migrationJournalContract {
	t.Helper()
	raw, err := os.ReadFile(migrationJournalContractPath)
	if err != nil {
		t.Fatalf("read migration journal contract: %v", err)
	}
	var contract migrationJournalContract
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatalf("parse migration journal contract: %v", err)
	}
	if contract.SchemaVersion != 1 {
		t.Fatalf("migration journal contract schemaVersion = %d, want 1", contract.SchemaVersion)
	}
	return contract
}

func TestControlPlaneMigrationJournalContractMatchesTheInstalledVersions(t *testing.T) {
	contract := readMigrationJournalContract(t)

	var declared *migrationJournalEntry
	for index := range contract.Journals {
		if contract.Journals[index].Service == "control-plane" {
			if declared != nil {
				t.Fatal("the contract declares the control-plane journal more than once")
			}
			declared = &contract.Journals[index]
		}
	}
	if declared == nil {
		t.Fatal("the contract does not declare the control-plane journal")
	}
	if declared.Source.Kind != "goMigrationList" {
		t.Fatalf("control-plane source kind = %q, want goMigrationList", declared.Source.Kind)
	}
	if declared.Source.Symbol != "controlPlaneMigrations" {
		t.Fatalf("control-plane source symbol = %q, want controlPlaneMigrations", declared.Source.Symbol)
	}

	// The declared source has to be the file this package installs from, so a
	// moved or renamed declaration cannot keep pointing at a stale path.
	source := "../../../../" + declared.Source.File
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read the declared source %s: %v", declared.Source.File, err)
	}
	if !strings.Contains(string(raw), "func "+declared.Source.Symbol+"(") {
		t.Fatalf("%s does not define %s", declared.Source.File, declared.Source.Symbol)
	}

	// The client and driver are only used inside the runners, and a runner is
	// never called here: the declaration is about the version sequence.
	installed := make([]string, 0, len(declared.Versions))
	for _, migration := range controlPlaneMigrations(nil, nil) {
		installed = append(installed, migration.Version)
	}

	if len(installed) != len(declared.Versions) {
		t.Fatalf("the contract declares %d control-plane versions, the product installs %d: %v",
			len(declared.Versions), len(installed), installed)
	}
	for index, version := range declared.Versions {
		if installed[index] != version {
			t.Fatalf("control-plane version %d = %q in the contract, %q in the product's migration list",
				index, version, installed[index])
		}
	}
}
