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
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"
)

func validateTencentApplicationCapabilities(revision contracts.WorkspaceApplicationRevision) error {
	if len(revision.HealthChecks) > 1 {
		return errors.New("tencent_application_health_checks_unsupported")
	}
	mounts := append([]contracts.WorkspaceApplicationMount(nil), revision.ScratchMounts...)
	executions := []contracts.WorkspaceApplicationExecution{revision.Execution}
	for _, dependency := range revision.Dependencies {
		if len(dependency.HealthChecks) > 1 {
			return errors.New("tencent_application_health_checks_unsupported")
		}
		mounts = append(mounts, dependency.ScratchMounts...)
		executions = append(executions, dependency.Execution)
	}
	for _, execution := range executions {
		if execution.Init || execution.SeccompProfile != "" {
			return errors.New("tencent_application_execution_unsupported")
		}
	}
	for _, mount := range mounts {
		if mount.Mode != nil || mount.UserID != nil || mount.GroupID != nil || mount.Executable {
			return errors.New("tencent_application_scratch_options_unsupported")
		}
	}
	return nil
}

func workspaceApplicationSecurityContext(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState) map[string]any {
	security := map[string]any{"seccompProfile": map[string]any{"type": "RuntimeDefault"}}
	execution := input.Revision.Execution
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		security["runAsNonRoot"], security["runAsUser"], security["runAsGroup"] = true, 10001, 10001
		security["fsGroup"], security["fsGroupChangePolicy"] = 10001, tencentWorkspaceFSGroupPolicy
	} else if dependency, err := dependencySpecByName(input.Revision, component.Name); err == nil {
		execution = dependency.Execution
	}
	if execution.UserID != nil {
		security["runAsNonRoot"], security["runAsUser"] = true, *execution.UserID
	}
	if execution.GroupID != nil {
		security["runAsGroup"] = *execution.GroupID
	}
	return security
}

func verifyTencentApplicationExecution(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState, actual any) bool {
	security, ok := actual.(map[string]any)
	if !ok {
		return false
	}
	expected := workspaceApplicationSecurityContext(input, component)
	for _, field := range []string{"runAsNonRoot", "runAsUser", "runAsGroup", "fsGroup", "fsGroupChangePolicy", "seccompProfile"} {
		if !bytes.Equal(mustJSON(expected[field]), mustJSON(security[field])) {
			return false
		}
	}
	return true
}

func workspaceApplicationScratchVolume(mount contracts.WorkspaceApplicationMount) map[string]any {
	volume := map[string]any{"medium": "Memory"}
	if mount.SizeBytes > 0 {
		volume["sizeLimit"] = strconv.FormatInt(mount.SizeBytes, 10)
	}
	return volume
}

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
		if secret.Target != "" {
			targets[secret.Name] = secret.Target
		}
	}
	// A platform-issued credential lands where the application asked for it.
	for _, credential := range input.Revision.Credentials {
		if credential.Target != "" {
			targets[credential.Name] = credential.Target
		}
	}
	return targets
}

func workspaceApplicationComponentSecretTargets(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState) map[string]string {
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		return workspaceApplicationSecretTargets(input)
	}
	targets := map[string]string{}
	dependency, err := dependencySpecByName(input.Revision, component.Name)
	if err == nil {
		for _, secret := range dependency.SecretInputs {
			if secret.Target != "" {
				targets[secret.Name] = secret.Target
			}
		}
	}
	return targets
}

func workspaceApplicationComponentEnvironment(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState) []any {
	declarations := input.Revision.SecretInputs
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		dependency, err := dependencySpecByName(input.Revision, component.Name)
		if err != nil {
			return nil
		}
		input.Configuration.Environment = dependency.Command.Env
		declarations = dependency.SecretInputs
	}
	result := workspaceApplicationEnvironment(input)
	for _, secret := range declarations {
		if secret.Env != "" {
			result = append(result, map[string]any{"name": secret.Env, "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": workspaceApplicationComponentResourceName(input, "secrets"), "key": secret.Name}}})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return stringValue(result[i].(map[string]any)["name"]) < stringValue(result[j].(map[string]any)["name"])
	})
	return result
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
	required := len(input.Revision.SecretInputs) > 0 || contracts.WorkspaceApplicationRequiresPlatformCredentials(input.Revision)
	for _, dependency := range input.Revision.Dependencies {
		required = required || len(dependency.SecretInputs) > 0
	}
	if !required {
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
	if contracts.WorkspaceApplicationCredentialKind(input.Revision, contracts.WorkspaceApplicationCredentialGatewayKey) {
		if _, err := workspaceApplicationGatewayBinding(input); err != nil {
			return err
		}
	}
	admin, hasAdmin := contracts.WorkspaceApplicationDeclaredCredential(input.Revision, contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword)
	session, hasSession := contracts.WorkspaceApplicationDeclaredCredential(input.Revision, contracts.WorkspaceApplicationCredentialWorkspaceSessionSecret)
	if hasAdmin || hasSession {
		seed := strings.TrimSpace(os.Getenv("OPL_AIONUI_ADMIN_PASSWORD_SEED"))
		if seed == "" {
			return errors.New("workspace_application_credential_seed_required")
		}
		token, err := tencentApplicationCredentialToken(ctx, input)
		if err != nil {
			return err
		}
		if hasAdmin {
			data[admin.Name] = deriveWorkspaceAdminPassword(seed, input.WorkspaceID, token)
		}
		if hasSession {
			data[session.Name] = deriveWorkspaceSessionSecret(seed, input.WorkspaceID, token)
		}
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
	if input.SchemaVersion == 0 {
		return true
	}
	if !verifyTencentApplicationExecution(input, component, nested(deployment, "spec", "template", "spec", "securityContext")) {
		return false
	}
	persistentMounts, scratchMounts := input.Revision.PersistentMounts, input.Revision.ScratchMounts
	command, arguments := input.Revision.Entrypoint, []string(nil)
	scratchPrefix := "scratch-"
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		dependency, err := dependencySpecByName(input.Revision, component.Name)
		if err != nil {
			return false
		}
		persistentMounts, scratchMounts = dependency.PersistentMounts, dependency.ScratchMounts
		command, arguments = dependency.Command.Entrypoint, dependency.Command.Args
		scratchPrefix = "dependency-scratch-"
		ports := []any{}
		for _, port := range dependency.Ports {
			ports = append(ports, map[string]any{"name": port.Name, "containerPort": port.Port, "protocol": port.Protocol})
		}
		actualPorts := firstContainerField(deployment, "ports")
		if !(len(ports) == 0 && actualPorts == nil) && !bytes.Equal(mustJSON(ports), mustJSON(actualPorts)) {
			return false
		}
		if len(dependency.HealthChecks) > 1 {
			return false
		}
		probe := firstContainerField(deployment, "readinessProbe")
		if len(dependency.HealthChecks) == 1 {
			check := dependency.HealthChecks[0]
			actual, ok := probe.(map[string]any)
			if !ok || number(actual["initialDelaySeconds"]) != float64(check.InitialDelaySeconds) || number(actual["periodSeconds"]) != 10 || actual["grpc"] != nil {
				return false
			}
			if check.Type == "http" {
				if actual["exec"] != nil || actual["tcpSocket"] != nil || stringValue(nested(actual, "httpGet", "path")) != check.Path || number(nested(actual, "httpGet", "port")) != float64(check.Port) {
					return false
				}
			} else if check.Type == "exec" {
				if actual["httpGet"] != nil || actual["tcpSocket"] != nil || !bytes.Equal(mustJSON(check.Command), mustJSON(nested(actual, "exec", "command"))) {
					return false
				}
			} else {
				if actual["exec"] != nil || actual["httpGet"] != nil || number(nested(actual, "tcpSocket", "port")) != float64(check.Port) {
					return false
				}
			}
		} else if probe != nil {
			return false
		}
	}
	for field, expected := range map[string][]string{"command": command, "args": arguments} {
		actual := firstContainerField(deployment, field)
		if len(expected) == 0 && actual == nil {
			continue
		}
		if !bytes.Equal(mustJSON(expected), mustJSON(actual)) {
			return false
		}
	}
	expected := workspaceApplicationComponentEnvironment(input, component)
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
	for _, mount := range persistentMounts {
		if !consumeMount(mount.MountPath, "workspace-data", applicationPersistentSubPath(input, mount), mount.ReadOnly) {
			return false
		}
	}
	if len(persistentMounts) > 0 {
		volume := volumes["workspace-data"]
		if len(volume) != 2 || storagePVC == "" || stringValue(nested(volume, "persistentVolumeClaim", "claimName")) != storagePVC || nested(volume, "persistentVolumeClaim", "readOnly") == true {
			return false
		}
		delete(volumes, "workspace-data")
	}
	for i, mount := range scratchMounts {
		name := fmt.Sprintf("%s%d", scratchPrefix, i)
		if !consumeMount(mount.MountPath, name, "", false) || len(volumes[name]) != 2 || stringValue(nested(volumes[name], "emptyDir", "medium")) != "Memory" {
			return false
		}
		actualSize := stringValue(nested(volumes[name], "emptyDir", "sizeLimit"))
		if mount.SizeBytes > 0 {
			quantity, err := resource.ParseQuantity(actualSize)
			if err != nil || quantity.Cmp(*resource.NewQuantity(mount.SizeBytes, resource.DecimalSI)) != 0 {
				return false
			}
		} else if actualSize != "" {
			return false
		}
		delete(volumes, name)
	}
	targets := workspaceApplicationComponentSecretTargets(input, component)
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
	configInputs := workspaceApplicationComponentConfigInputs(input, component)
	for _, config := range configInputs {
		if !consumeMount(config.Target, "application-config", config.Name, true) {
			return false
		}
	}
	if len(configInputs) > 0 {
		volume := volumes["application-config"]
		if len(volume) != 2 || stringValue(nested(volume, "configMap", "name")) != workspaceApplicationComponentResourceName(input, "config") || number(nested(volume, "configMap", "defaultMode")) != 0444 {
			return false
		}
		delete(volumes, "application-config")
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
	admin, declared := contracts.WorkspaceApplicationDeclaredCredential(input.Revision, contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword)
	if !declared {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, errors.New("workspace_application_credentials_unavailable")
	}
	password, err := base64.StdEncoding.DecodeString(stringValue(nested(secret, "data", admin.Name)))
	if err != nil || len(password) == 0 {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, ErrLaunchStageBindingConflict
	}
	return contracts.WorkspaceApplicationRuntimeCredentials{RuntimeID: applicationRuntimeID(input), WorkspaceID: input.WorkspaceID, WebUIUsername: admin.Username, WebUIPassword: string(password)}, nil
}
