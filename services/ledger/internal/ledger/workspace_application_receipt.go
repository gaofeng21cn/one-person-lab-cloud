package ledger

import (
	"bytes"
	"encoding/json"

	contracts "opl-cloud/packages/contracts/go"
)

func validWorkspaceDeletionOutputShape(input ReceiptInput) bool {
	expected := 6
	if key, exists := input.OutputRefs["workspaceKeyStatus"]; exists {
		if key != "absent" {
			return false
		}
		expected++
	}
	if keys, exists := input.OutputRefs["applicationGatewayKeysStatus"]; exists {
		if keys != "retained" {
			return false
		}
		if _, exists := input.Execution["applicationRetirement"]; !exists {
			return false
		}
		expected++
	}
	return len(input.OutputRefs) == expected
}

func validWorkspaceApplicationRetirementReceipt(input ReceiptInput) bool {
	payload, err := json.Marshal(input.Execution["applicationRetirement"])
	if err != nil {
		return false
	}
	var retirement contracts.WorkspaceApplicationRetirementReceipt
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&retirement) != nil || len(retirement.Runtimes)+len(retirement.Secrets)+len(retirement.RetainedGatewayKeyIDs) == 0 {
		return false
	}
	if retirement.CurrentDeploymentID != "" && !isOpaqueReference(retirement.CurrentDeploymentID) {
		return false
	}
	seen := map[string]bool{}
	for _, runtime := range retirement.Runtimes {
		if !isOpaqueReference(runtime.RuntimeID) || !isOpaqueReference(runtime.RuntimeOperationID) || seen[runtime.RuntimeOperationID] || runtime.State != "absent" {
			return false
		}
		seen[runtime.RuntimeOperationID] = true
		images := map[string]bool{}
		for _, image := range runtime.ImageRetirement {
			if !contracts.ValidWorkspaceImageReference(image.Image) || images[image.Image] {
				return false
			}
			switch image.State {
			case "removed", "retained_reference", "absent", "instance_required":
			default:
				return false
			}
			images[image.Image] = true
		}
	}
	seen = map[string]bool{}
	for _, secret := range retirement.Secrets {
		if !isOpaqueReference(secret.SecretRef) || seen[secret.SecretRef] {
			return false
		}
		if !(secret.Ownership == "workspace_gateway" && secret.State == "absent" || secret.Ownership == "external" && secret.State == "retained") {
			return false
		}
		seen[secret.SecretRef] = true
	}
	var previous int64
	for _, key := range retirement.RetainedGatewayKeyIDs {
		if key <= previous {
			return false
		}
		previous = key
	}
	if len(retirement.RetainedGatewayKeyIDs) > 0 {
		return input.OutputRefs["applicationGatewayKeysStatus"] == "retained"
	}
	_, exists := input.OutputRefs["applicationGatewayKeysStatus"]
	return !exists
}
