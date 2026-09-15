package contracts

import (
	"reflect"
	"testing"
)

func TestApplicationStartupOrderIsDependencyDriven(t *testing.T) {
	revision := validWorkspaceApplicationRevision()
	revision.Dependencies = []WorkspaceApplicationDependency{{Name: "service", Image: revision.Image, DependsOn: []string{"database", "model"}}, {Name: "model", Image: revision.Image}, {Name: "database", Image: revision.Image}}
	order, err := WorkspaceApplicationStartupOrder(revision)
	if err != nil || !reflect.DeepEqual(order, []string{"database", "model", "service", "main"}) {
		t.Fatalf("order=%v err=%v", order, err)
	}
	if err := ValidateWorkspaceApplicationRevision(revision); err != nil {
		t.Fatal(err)
	}
	for _, edges := range [][]string{{"missing"}, {"service"}, {"main"}, {"model", "model"}} {
		revision.Dependencies[0].DependsOn = edges
		if _, err := WorkspaceApplicationStartupOrder(revision); err == nil {
			t.Fatalf("invalid edges accepted: %v", edges)
		}
	}
	revision.Dependencies[0].DependsOn = []string{"model"}
	revision.Dependencies[1].DependsOn = []string{"service"}
	if _, err := WorkspaceApplicationStartupOrder(revision); err == nil {
		t.Fatal("cycle accepted")
	}
}

func TestPartiallyCreatedApplicationSuspensionRetainsAbsentFacts(t *testing.T) {
	components := []WorkspaceApplicationRuntimeComponentState{{Name: "main", State: "absent"}, {Name: "database", State: "suspended"}}
	if status := WorkspaceApplicationRuntimeOverallStatus(components); status != "suspended" {
		t.Fatalf("status=%s", status)
	}
	if components[0].State != "absent" {
		t.Fatal("missing component was fabricated")
	}
	components[1].State = "absent"
	if status := WorkspaceApplicationRuntimeOverallStatus(components); status != "absent" {
		t.Fatal(status)
	}
	components[1].State = "pending"
	if status := WorkspaceApplicationRuntimeOverallStatus(components); status != "pending" {
		t.Fatal(status)
	}
}
