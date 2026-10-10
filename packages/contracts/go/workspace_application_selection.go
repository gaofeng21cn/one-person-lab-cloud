package contracts

import (
	"errors"

	api "opl-cloud/packages/contracts/go/api"
)

// The default OPL App and a built Agent are the only two application sources. A
// WorkspaceApplicationSelection carries a required kind discriminator plus the
// one id that kind owns. Decoding follows the discriminator the same way the
// OpenAPI schema does: a mixed selection (both members present), a missing
// kind, an unknown id for the named kind, or an id set for the wrong kind is
// rejected. There is no implicit default and no fallback to a current
// deployment pointer.
func ValidateWorkspaceApplicationSelection(selection *api.WorkspaceApplicationSelection) error {
	if selection == nil {
		return errors.New("workspace_application_selection_required")
	}
	runtimeVersionID := selection.GetRuntimeVersionId()
	capabilityVersionID := selection.GetCapabilityVersionId()
	// Presence, not value emptiness, discriminates the branches: the proto
	// fields are optional (absent != present-empty) and each OpenAPI branch
	// declares additionalProperties:false, so a branch carrying the other
	// branch's member at all is a mixed selection.
	runtimePresent := selection.RuntimeVersionId != nil
	capabilityPresent := selection.CapabilityVersionId != nil
	switch selection.GetKind() {
	case api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP:
		if !runtimePresent || runtimeVersionID == "" || capabilityPresent {
			return errors.New("workspace_application_selection_opl_app_invalid")
		}
	case api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT:
		if !capabilityPresent || capabilityVersionID == "" || runtimePresent {
			return errors.New("workspace_application_selection_agent_invalid")
		}
	default:
		return errors.New("workspace_application_selection_kind_required")
	}
	return nil
}

// IsDefaultOPLAppSelection reports whether the selection is the default OPL App
// branch: an approved Runtime Release with no Package, Build or CapabilityVersion.
func IsDefaultOPLAppSelection(selection *api.WorkspaceApplicationSelection) bool {
	return selection != nil && selection.GetKind() == api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP
}
