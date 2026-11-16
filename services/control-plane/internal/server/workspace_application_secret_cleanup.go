package server

import ()

type workspaceApplicationSecretCleanup struct {
	Secrets               []workspaceApplicationSecretRetirement `json:"secrets"`
	RetainedGatewayKeyIDs []int64                                `json:"retainedGatewayKeyIds,omitempty"`
}

type workspaceApplicationSecretRetirement struct {
	SecretRef string `json:"secretRef"`
	Ownership string `json:"ownership"`
	State     string `json:"state"`
}

func validWorkspaceApplicationSecretCleanup(cleanup *workspaceApplicationSecretCleanup) bool {
	if cleanup == nil {
		return true
	}
	previous := ""
	for _, secret := range cleanup.Secrets {
		if secret.SecretRef <= previous {
			return false
		}
		if secret.Ownership == "workspace_gateway" {
			if secret.State != "pending" && secret.State != "absent" {
				return false
			}
		} else if secret.Ownership != "external" || secret.State != "retained" {
			return false
		}
		previous = secret.SecretRef
	}
	var previousKey int64
	for _, key := range cleanup.RetainedGatewayKeyIDs {
		if key <= previousKey {
			return false
		}
		previousKey = key
	}
	return true
}

func workspaceApplicationSecretTargetsMatch(current, desired *workspaceApplicationSecretCleanup) bool {
	if current == nil {
		return true
	}
	if desired == nil || len(current.Secrets) != len(desired.Secrets) || len(current.RetainedGatewayKeyIDs) != len(desired.RetainedGatewayKeyIDs) {
		return false
	}
	for i, secret := range current.Secrets {
		if secret.SecretRef != desired.Secrets[i].SecretRef || secret.Ownership != desired.Secrets[i].Ownership || secret.State == "absent" && desired.Secrets[i].State != "absent" {
			return false
		}
	}
	for i, key := range current.RetainedGatewayKeyIDs {
		if key != desired.RetainedGatewayKeyIDs[i] {
			return false
		}
	}
	return true
}

func workspaceApplicationSecretCleanupComplete(cleanup *workspaceApplicationSecretCleanup) bool {
	if cleanup == nil {
		return false
	}
	for _, secret := range cleanup.Secrets {
		if secret.Ownership == "workspace_gateway" && secret.State != "absent" {
			return false
		}
	}
	return true
}
