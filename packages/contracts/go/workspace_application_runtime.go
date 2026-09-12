package contracts

import "errors"

// WorkspaceApplicationRuntimeComponentState is the authoritative per-component
// observation of one deployed application runtime. State values mirror the
// runtime observation vocabulary: absent, pending, ready, failed.
type WorkspaceApplicationRuntimeComponentState struct {
	Name       string   `json:"name"`
	Role       string   `json:"role"`
	Image      string   `json:"image"`
	State      string   `json:"state"`
	Ports      []int    `json:"ports,omitempty"`
	LastError  string   `json:"lastError,omitempty"`
	ReadyCheck string   `json:"readyCheck,omitempty"`
	CheckNames []string `json:"checkNames,omitempty"`
}

// WorkspaceApplicationRuntimeObservation is the whole-runtime readback: every
// declared component of the admitted revision, observed where it actually
// runs. A component missing from the observation never proves absence; the
// observer must report it with State "absent".
type WorkspaceApplicationRuntimeObservation struct {
	SchemaVersion int                                        `json:"schemaVersion"`
	WorkspaceID   string                                     `json:"workspaceId"`
	RuntimeID     string                                     `json:"runtimeId"`
	Status        string                                     `json:"status"`
	Components    []WorkspaceApplicationRuntimeComponentState `json:"components"`
}

const (
	// WorkspaceApplicationComponentMain is the revision's primary component.
	WorkspaceApplicationComponentMain = "main"
	// WorkspaceApplicationComponentDependency marks a supporting service
	// declared by the revision's Dependencies.
	WorkspaceApplicationComponentDependency = "dependency"
)

// WorkspaceApplicationRuntimeComponents derives the declared component list of
// one revision: the main component plus one component per dependency. Names
// are unique by construction; the revision validation owns the image rules.
func WorkspaceApplicationRuntimeComponents(revision WorkspaceApplicationRevision) []WorkspaceApplicationRuntimeComponentState {
	components := []WorkspaceApplicationRuntimeComponentState{{
		Name: WorkspaceApplicationComponentMain, Role: WorkspaceApplicationComponentMain,
		Image: revision.Image, State: "absent",
	}}
	for _, dependency := range revision.Dependencies {
		components = append(components, WorkspaceApplicationRuntimeComponentState{
			Name: dependency.Name, Role: WorkspaceApplicationComponentDependency,
			Image: dependency.Image, State: "absent",
		})
	}
	return components
}

// ValidateWorkspaceApplicationRuntimeObservation checks one observation
// against the revision it claims to observe: every declared component appears
// exactly once with the declared image, and states use the runtime vocabulary.
func ValidateWorkspaceApplicationRuntimeObservation(revision WorkspaceApplicationRevision, observation WorkspaceApplicationRuntimeObservation) error {
	if observation.SchemaVersion != 1 || observation.WorkspaceID == "" {
		return errors.New("workspace_application_runtime_observation_invalid")
	}
	switch observation.Status {
	case "absent", "pending", "ready", "failed":
	default:
		return errors.New("workspace_application_runtime_status_invalid")
	}
	declared := WorkspaceApplicationRuntimeComponents(revision)
	seen := map[string]struct{}{}
	byName := map[string]WorkspaceApplicationRuntimeComponentState{}
	for _, component := range observation.Components {
		if _, duplicate := seen[component.Name]; duplicate {
			return errors.New("workspace_application_runtime_component_duplicate")
		}
		seen[component.Name] = struct{}{}
		byName[component.Name] = component
		switch component.State {
		case "absent", "pending", "ready", "failed":
		default:
			return errors.New("workspace_application_runtime_component_state_invalid")
		}
	}
	for _, expected := range declared {
		observed, found := byName[expected.Name]
		if !found {
			return errors.New("workspace_application_runtime_component_missing")
		}
		if observed.Image != expected.Image || observed.Role != expected.Role {
			return errors.New("workspace_application_runtime_component_identity_mismatch")
		}
	}
	if len(observation.Components) != len(declared) {
		return errors.New("workspace_application_runtime_component_unexpected")
	}
	return nil
}
