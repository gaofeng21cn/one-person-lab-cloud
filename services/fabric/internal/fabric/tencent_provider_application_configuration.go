package fabric

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/fabric/internal/protectedresource"
	"os"
	"reflect"
	"sort"
	"strings"
)

func workspaceApplicationEnvironment(input WorkspaceApplicationRuntimeInput) []any {
	names := make([]string, 0, len(input.Configuration.Environment))
	for name := range input.Configuration.Environment {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]any, 0, len(names))
	for _, name := range names {
		result = append(result, map[string]any{"name": name, "value": input.Configuration.Environment[name]})
	}
	return result
}
func workspaceApplicationSecretTargets(input WorkspaceApplicationRuntimeInput) map[string]string {
	targets := map[string]string{}
	for _, secret := range input.Revision.SecretInputs {
		targets[secret.Name] = secret.Target
	}
	if input.Revision.RuntimeProfile == "opl_app" {
		targets["webui-password"] = "/run/secrets/opl_webui_password"
		targets["webui-session"] = "/run/secrets/webui_session_secret"
	}
	return targets
}

func (p *TencentProvider) readApplicationBoundSecret(ctx context.Context, input WorkspaceApplicationRuntimeInput, binding contracts.WorkspaceApplicationRuntimeSecretBinding) (string, error) {
	raw, err := p.callKubectl(ctx, []string{"get", "secret/" + binding.SecretRef, "--ignore-not-found", "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return "", err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return "", ErrWorkspaceLaunchResourceAbsent
	}
	var secret map[string]any
	if json.Unmarshal(raw, &secret) != nil {
		return "", ErrLaunchStageBindingConflict
	}
	if stringValue(nested(secret, "metadata", "annotations", "oplcloud.cn/account-id")) != input.AccountID || stringValue(nested(secret, "metadata", "annotations", "oplcloud.cn/workspace-id")) != input.WorkspaceID || stringValue(nested(secret, "metadata", "annotations", "oplcloud.cn/secret-version")) != binding.Version {
		return "", ErrLaunchStageBindingConflict
	}
	if binding.SecretRef == gatewaySecretName(input.WorkspaceID) {
		if _, err := workspaceApplicationGatewayBinding(input); err != nil {
			return "", err
		}
	}
	value, err := base64.StdEncoding.DecodeString(stringValue(nested(secret, "data", binding.Key)))
	if err != nil || len(value) == 0 {
		return "", ErrLaunchStageBindingConflict
	}
	return string(value), nil
}

func (p *TencentProvider) applicationSecretObject(ctx context.Context, input WorkspaceApplicationRuntimeInput) (map[string]any, error) {
	name := workspaceApplicationComponentResourceName(input, "secrets")
	raw, err := p.callKubectl(ctx, []string{"get", "secret/" + name, "--ignore-not-found", "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, ErrWorkspaceLaunchResourceAbsent
	}
	var secret map[string]any
	if json.Unmarshal(raw, &secret) != nil {
		return nil, ErrLaunchStageBindingConflict
	}
	if stringValue(nested(secret, "metadata", "labels", "oplcloud.cn/runtime-id")) != k8sCostLabelValue(applicationRuntimeID(input)) || stringValue(nested(secret, "metadata", "labels", "oplcloud.cn/account-id")) != k8sCostLabelValue(input.AccountID) || stringValue(nested(secret, "metadata", "annotations", "oplcloud.cn/configuration-digest")) != input.ConfigurationDigest || secret["immutable"] != true {
		return nil, ErrLaunchStageBindingConflict
	}
	return secret, nil
}

func (p *TencentProvider) prepareApplicationSecrets(ctx context.Context, input WorkspaceApplicationRuntimeInput) error {
	if len(workspaceApplicationSecretTargets(input)) == 0 {
		return nil
	}
	if _, err := p.applicationSecretObject(ctx, input); err == nil {
		return nil
	} else if !errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
		return err
	}
	data := map[string]string{}
	for _, binding := range input.SecretBindings {
		value, err := p.readApplicationBoundSecret(ctx, input, binding)
		if err != nil {
			return err
		}
		data[binding.Name] = value
	}
	if input.Revision.RuntimeProfile == "opl_app" {
		_, err := workspaceApplicationGatewayBinding(input)
		if err != nil {
			return err
		}
		seed := strings.TrimSpace(os.Getenv("OPL_AIONUI_ADMIN_PASSWORD_SEED"))
		if seed == "" {
			return errors.New("workspace_application_webui_credential_seed_required")
		}
		token, err := tencentApplicationCredentialToken(ctx, input)
		if err != nil {
			return err
		}
		data["webui-password"] = deriveAionUIAdminPassword(seed, input.WorkspaceID, token)
		data["webui-session"] = deriveWebUISessionSecret(seed, input.WorkspaceID, token)
	}
	manifest := mustJSON(map[string]any{"apiVersion": "v1", "kind": "Secret", "type": "Opaque", "immutable": true, "metadata": map[string]any{
		"name": workspaceApplicationComponentResourceName(input, "secrets"), "labels": map[string]string{"oplcloud.cn/runtime-id": k8sCostLabelValue(applicationRuntimeID(input)), "oplcloud.cn/account-id": k8sCostLabelValue(input.AccountID), "oplcloud.cn/workspace-id": k8sCostLabelValue(input.WorkspaceID)}, "annotations": map[string]string{"oplcloud.cn/configuration-digest": input.ConfigurationDigest}}, "stringData": data})
	_, err := p.callKubectl(ctx, []string{"create", "-f", "-"}, manifest, protectedresource.Target{})
	if err != nil {
		return err
	}
	readback, err := p.applicationSecretObject(ctx, input)
	if err != nil {
		return err
	}
	for key, value := range data {
		actual, err := base64.StdEncoding.DecodeString(stringValue(nested(readback, "data", key)))
		if err != nil || string(actual) != value {
			return ErrLaunchStageBindingConflict
		}
	}
	return nil
}

func tencentApplicationCredentialToken(ctx context.Context, input WorkspaceApplicationRuntimeInput) (string, error) {
	version := input.Configuration.CredentialVersion
	if version == "" {
		return "", errors.New("workspace_application_credential_version_required")
	}
	if source := input.Configuration.CredentialSourceRuntimeOperationID; source != "" {
		proven, ok := ctx.Value(applicationCredentialSourceContextKey{}).(applicationCredentialSource)
		if !ok || proven.RuntimeOperationID != source || proven.Version != version || proven.OperationKey == "" {
			return "", ErrLaunchStageBindingConflict
		}
		token := stableID(input.WorkspaceID, proven.OperationKey)[:24]
		if stableID("workspace-credential", input.WorkspaceID, token)[:16] != version {
			return "", ErrLaunchStageBindingConflict
		}
		return token, nil
	}
	return version, nil
}
func verifyTencentApplicationConfiguration(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState, deployment map[string]any, storagePVC string) bool {
	if input.SchemaVersion == 0 || component.Role != contracts.WorkspaceApplicationComponentMain {
		return true
	}
	expected := workspaceApplicationEnvironment(input)
	actual, _ := firstContainerField(deployment, "env").([]any)
	if len(expected) != len(actual) || len(expected) > 0 && !reflect.DeepEqual(expected, actual) {
		return false
	}
	if stringValue(nested(deployment, "metadata", "annotations", "oplcloud.cn/configuration-digest")) != input.ConfigurationDigest {
		return false
	}
	mounts := map[string]map[string]any{}
	actualMounts, _ := firstContainerField(deployment, "volumeMounts").([]any)
	for _, value := range actualMounts {
		mount, ok := value.(map[string]any)
		path := stringValue(mount["mountPath"])
		if !ok || path == "" || mounts[path] != nil || stringValue(mount["subPathExpr"]) != "" {
			return false
		}
		mounts[path] = mount
	}
	volumes := map[string]map[string]any{}
	actualVolumes, _ := nested(deployment, "spec", "template", "spec", "volumes").([]any)
	for _, value := range actualVolumes {
		volume, ok := value.(map[string]any)
		name := stringValue(volume["name"])
		if !ok || name == "" || volumes[name] != nil {
			return false
		}
		volumes[name] = volume
	}
	consumeMount := func(path, name, subPath string, readOnly bool) bool {
		mount := mounts[path]
		if mount == nil || mount["name"] != name || stringValue(mount["subPath"]) != subPath || (mount["readOnly"] == true) != readOnly {
			return false
		}
		delete(mounts, path)
		return true
	}
	for _, mount := range input.Revision.PersistentMounts {
		if !consumeMount(mount.MountPath, "workspace-data", applicationPersistentSubPath(input, mount), mount.ReadOnly) {
			return false
		}
	}
	if len(input.Revision.PersistentMounts) > 0 {
		volume := volumes["workspace-data"]
		if len(volume) != 2 || storagePVC == "" || stringValue(nested(volume, "persistentVolumeClaim", "claimName")) != storagePVC || nested(volume, "persistentVolumeClaim", "readOnly") == true {
			return false
		}
		delete(volumes, "workspace-data")
	}
	for i, mount := range input.Revision.ScratchMounts {
		name := fmt.Sprintf("scratch-%d", i)
		if !consumeMount(mount.MountPath, name, "", false) || len(volumes[name]) != 2 || stringValue(nested(volumes[name], "emptyDir", "medium")) != "Memory" {
			return false
		}
		delete(volumes, name)
	}
	targets := workspaceApplicationSecretTargets(input)
	for name, path := range targets {
		if !consumeMount(path, "application-secrets", name, true) {
			return false
		}
	}
	if len(targets) > 0 {
		volume := volumes["application-secrets"]
		if len(volume) != 2 || stringValue(nested(volume, "secret", "secretName")) != workspaceApplicationComponentResourceName(input, "secrets") || number(nested(volume, "secret", "defaultMode")) != 0440 {
			return false
		}
		delete(volumes, "application-secrets")
	}
	return len(mounts) == 0 && len(volumes) == 0
}
func (p *TencentProvider) ReadWorkspaceApplicationRuntimeCredentials(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	if _, err := workspaceApplicationGatewayBinding(input); err != nil {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, err
	}
	secret, err := p.applicationSecretObject(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, err
	}
	password, err := base64.StdEncoding.DecodeString(stringValue(nested(secret, "data", "webui-password")))
	if err != nil || len(password) == 0 {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, ErrLaunchStageBindingConflict
	}
	return contracts.WorkspaceApplicationRuntimeCredentials{RuntimeID: applicationRuntimeID(input), WorkspaceID: input.WorkspaceID, WebUIUsername: webuiUsername, WebUIPassword: string(password)}, nil
}
