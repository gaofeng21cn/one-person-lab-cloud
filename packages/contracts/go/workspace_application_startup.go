package contracts

import "errors"

// WorkspaceApplicationComponentDependencies is the declared readiness boundary.
// The primary application requires its supporting components; supporting
// components name only the services they actually need at startup.
func WorkspaceApplicationComponentDependencies(revision WorkspaceApplicationRevision, name string) []string {
	if name == WorkspaceApplicationComponentMain {
		names := make([]string, 0, len(revision.Dependencies))
		for _, component := range revision.Dependencies {
			names = append(names, component.Name)
		}
		return names
	}
	for _, component := range revision.Dependencies {
		if component.Name == name {
			return append([]string{}, component.DependsOn...)
		}
	}
	return nil
}

// WorkspaceApplicationStartupOrder performs a stable topological traversal.
// Cycles, unknown services and self edges fail admission rather than deadlock a
// provider. Stopping uses the reverse of the same dependency order.
func WorkspaceApplicationStartupOrder(revision WorkspaceApplicationRevision) ([]string, error) {
	nodes := map[string][]string{}
	for _, component := range revision.Dependencies {
		if _, exists := nodes[component.Name]; exists || component.Name == WorkspaceApplicationComponentMain {
			return nil, errors.New("workspace_application_dependency_duplicate")
		}
		nodes[component.Name] = component.DependsOn
	}
	nodes[WorkspaceApplicationComponentMain] = WorkspaceApplicationComponentDependencies(revision, WorkspaceApplicationComponentMain)
	state := map[string]uint8{}
	order := make([]string, 0, len(nodes))
	var visit func(string) error
	visit = func(name string) error {
		dependencies, exists := nodes[name]
		if !exists {
			return errors.New("workspace_application_dependency_unknown")
		}
		if state[name] == 1 {
			return errors.New("workspace_application_dependency_cycle")
		}
		if state[name] == 2 {
			return nil
		}
		state[name] = 1
		seen := map[string]bool{}
		for _, dependency := range dependencies {
			if dependency == name || seen[dependency] || dependency == WorkspaceApplicationComponentMain {
				return errors.New("workspace_application_dependency_edge_invalid")
			}
			seen[dependency] = true
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[name] = 2
		order = append(order, name)
		return nil
	}
	for _, component := range revision.Dependencies {
		if err := visit(component.Name); err != nil {
			return nil, err
		}
	}
	if err := visit(WorkspaceApplicationComponentMain); err != nil {
		return nil, err
	}
	return order, nil
}
