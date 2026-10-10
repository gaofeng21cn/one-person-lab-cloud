package contracts

import (
	"testing"

	"google.golang.org/protobuf/proto"

	api "opl-cloud/packages/contracts/go/api"
)

// The wire contract declares one branch per kind with additionalProperties:
// false, and both id fields are optional in the proto. Presence — not value
// emptiness — is therefore the discriminator: a branch that carries the other
// branch's member at all, even as an empty string, is a mixed selection and
// must be refused.
func TestApplicationSelectionRejectsPresentForeignMember(t *testing.T) {
	opl := api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP
	agent := api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_AGENT
	unspecified := api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_UNSPECIFIED

	rejected := []struct {
		name      string
		selection *api.WorkspaceApplicationSelection
	}{
		{"opl_app with an empty capability member present", &api.WorkspaceApplicationSelection{Kind: opl, RuntimeVersionId: proto.String("runtime-1"), CapabilityVersionId: proto.String("")}},
		{"agent with an empty runtime member present", &api.WorkspaceApplicationSelection{Kind: agent, RuntimeVersionId: proto.String(""), CapabilityVersionId: proto.String("capability-1")}},
		{"opl_app with both members present", &api.WorkspaceApplicationSelection{Kind: opl, RuntimeVersionId: proto.String("runtime-1"), CapabilityVersionId: proto.String("capability-1")}},
		{"agent with both members present", &api.WorkspaceApplicationSelection{Kind: agent, RuntimeVersionId: proto.String("runtime-1"), CapabilityVersionId: proto.String("capability-1")}},
		{"opl_app without its runtime member", &api.WorkspaceApplicationSelection{Kind: opl}},
		{"agent without its capability member", &api.WorkspaceApplicationSelection{Kind: agent}},
		{"opl_app with an empty runtime member", &api.WorkspaceApplicationSelection{Kind: opl, RuntimeVersionId: proto.String("")}},
		{"agent with an empty capability member", &api.WorkspaceApplicationSelection{Kind: agent, CapabilityVersionId: proto.String("")}},
		{"unspecified kind with a runtime member", &api.WorkspaceApplicationSelection{Kind: unspecified, RuntimeVersionId: proto.String("runtime-1")}},
	}
	for _, c := range rejected {
		if err := ValidateWorkspaceApplicationSelection(c.selection); err == nil {
			t.Errorf("%s was accepted", c.name)
		}
	}

	accepted := []struct {
		name      string
		selection *api.WorkspaceApplicationSelection
	}{
		{"opl_app with exactly its runtime member", &api.WorkspaceApplicationSelection{Kind: opl, RuntimeVersionId: proto.String("runtime-1")}},
		{"agent with exactly its capability member", &api.WorkspaceApplicationSelection{Kind: agent, CapabilityVersionId: proto.String("capability-1")}},
	}
	for _, c := range accepted {
		if err := ValidateWorkspaceApplicationSelection(c.selection); err != nil {
			t.Errorf("%s was refused: %v", c.name, err)
		}
	}
}
