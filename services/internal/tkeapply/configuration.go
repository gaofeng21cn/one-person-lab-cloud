package tkeapply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sort"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/protectedresource"
)

func ValidateCapabilities(revision contracts.WorkspaceApplicationRevision) error {
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

// BuildSecurityContext constructs the typed PodSecurityContext for one
// Workspace application component.
func BuildSecurityContext(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState) *corev1.PodSecurityContext {
	security := &corev1.PodSecurityContext{
		SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault},
	}
	execution := input.Revision.Execution
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		runAsNonRoot := true
		uid := int64(10001)
		gid := int64(10001)
		fsGroup := int64(10001)
		policy := corev1.PodFSGroupChangePolicy(tencentWorkspaceFSGroupPolicy)
		security.RunAsNonRoot = &runAsNonRoot
		security.RunAsUser = &uid
		security.RunAsGroup = &gid
		security.FSGroup = &fsGroup
		security.FSGroupChangePolicy = &policy
	} else if dependency, err := dependencySpecByName(input.Revision, component.Name); err == nil {
		execution = dependency.Execution
	}
	if execution.UserID != nil {
		runAsNonRoot := true
		uid := *execution.UserID
		security.RunAsNonRoot = &runAsNonRoot
		security.RunAsUser = &uid
	}
	if execution.GroupID != nil {
		gid := *execution.GroupID
		security.RunAsGroup = &gid
	}
	return security
}

func buildSecurityContextApply(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState) *applycorev1.PodSecurityContextApplyConfiguration {
	typed := BuildSecurityContext(input, component)
	cfg := applycorev1.PodSecurityContext().
		WithSeccompProfile(applycorev1.SeccompProfile().WithType(typed.SeccompProfile.Type))
	if typed.RunAsNonRoot != nil {
		cfg.WithRunAsNonRoot(*typed.RunAsNonRoot)
	}
	if typed.RunAsUser != nil {
		cfg.WithRunAsUser(*typed.RunAsUser)
	}
	if typed.RunAsGroup != nil {
		cfg.WithRunAsGroup(*typed.RunAsGroup)
	}
	if typed.FSGroup != nil {
		cfg.WithFSGroup(*typed.FSGroup)
	}
	if typed.FSGroupChangePolicy != nil {
		cfg.WithFSGroupChangePolicy(*typed.FSGroupChangePolicy)
	}
	return cfg
}

// SecurityContext renders the PodSecurityContext as a JSON-compatible map for
// callers inspecting serialized manifests.
func SecurityContext(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState) map[string]any {
	typed := BuildSecurityContext(input, component)
	out := map[string]any{
		"seccompProfile": map[string]any{"type": string(typed.SeccompProfile.Type)},
	}
	if typed.RunAsNonRoot != nil {
		out["runAsNonRoot"] = *typed.RunAsNonRoot
	}
	if typed.RunAsUser != nil {
		out["runAsUser"] = *typed.RunAsUser
	}
	if typed.RunAsGroup != nil {
		out["runAsGroup"] = *typed.RunAsGroup
	}
	if typed.FSGroup != nil {
		out["fsGroup"] = *typed.FSGroup
	}
	if typed.FSGroupChangePolicy != nil {
		out["fsGroupChangePolicy"] = string(*typed.FSGroupChangePolicy)
	}
	return out
}

func verifyTypedApplicationExecution(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState, actual *corev1.PodSecurityContext) bool {
	if actual == nil {
		return false
	}
	expected := BuildSecurityContext(input, component)
	return reflect.DeepEqual(expected.RunAsNonRoot, actual.RunAsNonRoot) &&
		reflect.DeepEqual(expected.RunAsUser, actual.RunAsUser) &&
		reflect.DeepEqual(expected.RunAsGroup, actual.RunAsGroup) &&
		reflect.DeepEqual(expected.FSGroup, actual.FSGroup) &&
		reflect.DeepEqual(expected.FSGroupChangePolicy, actual.FSGroupChangePolicy) &&
		reflect.DeepEqual(expected.SeccompProfile, actual.SeccompProfile)
}

func workspaceApplicationScratchVolumeApply(mount contracts.WorkspaceApplicationMount) *applycorev1.EmptyDirVolumeSourceApplyConfiguration {
	volume := applycorev1.EmptyDirVolumeSource().WithMedium(corev1.StorageMediumMemory)
	if mount.SizeBytes > 0 {
		volume.WithSizeLimit(resource.MustParse(strconv.FormatInt(mount.SizeBytes, 10)))
	}
	return volume
}

func workspaceApplicationEnvironment(input WorkspaceApplicationRuntimeInput) []corev1.EnvVar {
	names := make([]string, 0, len(input.Configuration.Environment))
	for name := range input.Configuration.Environment {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]corev1.EnvVar, 0, len(names))
	for _, name := range names {
		result = append(result, corev1.EnvVar{Name: name, Value: input.Configuration.Environment[name]})
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

func workspaceApplicationComponentEnvironment(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState) []corev1.EnvVar {
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
	secretName := ComponentResourceName(input, "secrets")
	for _, secret := range declarations {
		if secret.Env != "" {
			result = append(result, corev1.EnvVar{
				Name: secret.Env,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
						Key:                  secret.Name,
					},
				},
			})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

func envVarsToApply(envs []corev1.EnvVar) []*applycorev1.EnvVarApplyConfiguration {
	out := make([]*applycorev1.EnvVarApplyConfiguration, 0, len(envs))
	for _, env := range envs {
		item := applycorev1.EnvVar().WithName(env.Name)
		if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
			item.WithValueFrom(
				applycorev1.EnvVarSource().WithSecretKeyRef(
					applycorev1.SecretKeySelector().
						WithName(env.ValueFrom.SecretKeyRef.Name).
						WithKey(env.ValueFrom.SecretKeyRef.Key),
				),
			)
		} else {
			item.WithValue(env.Value)
		}
		out = append(out, item)
	}
	return out
}

func (p *Executor) readApplicationBoundSecret(ctx context.Context, input WorkspaceApplicationRuntimeInput, binding contracts.WorkspaceApplicationRuntimeSecretBinding) (string, error) {
	secret, err := p.getSecret(ctx, binding.SecretRef)
	if err != nil {
		return "", err
	}
	if secret == nil {
		return "", ErrResourceAbsent
	}
	if secret.Annotations["oplcloud.cn/account-id"] != input.AccountID ||
		secret.Annotations["oplcloud.cn/workspace-id"] != input.WorkspaceID ||
		secret.Annotations["oplcloud.cn/secret-version"] != binding.Version {
		return "", ErrLaunchStageBindingConflict
	}
	if binding.SecretRef == gatewaySecretName(input.WorkspaceID) {
		if _, err := workspaceApplicationGatewayBinding(input); err != nil {
			return "", err
		}
	}
	value := secret.Data[binding.Key]
	if len(value) == 0 {
		return "", ErrLaunchStageBindingConflict
	}
	return string(value), nil
}

func (p *Executor) applicationSecretObject(ctx context.Context, input WorkspaceApplicationRuntimeInput) (*corev1.Secret, error) {
	name := ComponentResourceName(input, "secrets")
	secret, err := p.getSecret(ctx, name)
	if err != nil {
		return nil, err
	}
	if secret == nil {
		return nil, ErrResourceAbsent
	}
	if secret.Labels["oplcloud.cn/runtime-id"] != k8sCostLabelValue(applicationRuntimeID(input)) ||
		secret.Labels["oplcloud.cn/account-id"] != k8sCostLabelValue(input.AccountID) ||
		secret.Annotations["oplcloud.cn/configuration-digest"] != input.ConfigurationDigest ||
		secret.Immutable == nil || !*secret.Immutable {
		return nil, ErrLaunchStageBindingConflict
	}
	return secret, nil
}

func (p *Executor) prepareApplicationSecrets(ctx context.Context, input WorkspaceApplicationRuntimeInput) error {
	required := len(input.Revision.SecretInputs) > 0 || contracts.WorkspaceApplicationRequiresPlatformCredentials(input.Revision)
	for _, dependency := range input.Revision.Dependencies {
		required = required || len(dependency.SecretInputs) > 0
	}
	if !required {
		return nil
	}
	if _, err := p.applicationSecretObject(ctx, input); err == nil {
		return nil
	} else if !errors.Is(err, ErrResourceAbsent) {
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
		seed := strings.TrimSpace(p.Installation.AdminPasswordSeed)
		if seed == "" {
			return errors.New("workspace_application_credential_seed_required")
		}
		resolveToken := p.CredentialToken
		if resolveToken == nil {
			resolveToken = tencentApplicationCredentialToken
		}
		token, err := resolveToken(ctx, input)
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
	immutable := true
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Secret"},
		ObjectMeta: metav1.ObjectMeta{
			Name: ComponentResourceName(input, "secrets"),
			Labels: map[string]string{
				"oplcloud.cn/runtime-id":   k8sCostLabelValue(applicationRuntimeID(input)),
				"oplcloud.cn/account-id":   k8sCostLabelValue(input.AccountID),
				"oplcloud.cn/workspace-id": k8sCostLabelValue(input.WorkspaceID),
			},
			Annotations: map[string]string{
				"oplcloud.cn/configuration-digest": input.ConfigurationDigest,
			},
		},
		Immutable:  &immutable,
		Type:       corev1.SecretTypeOpaque,
		StringData: maps.Clone(data),
	}
	if err := p.createSecret(ctx, secret, protectedresource.Target{}); err != nil {
		return err
	}
	readback, err := p.applicationSecretObject(ctx, input)
	if err != nil {
		return err
	}
	for key, value := range data {
		actual, ok := readback.Data[key]
		if !ok || string(actual) != value {
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

func verifyTypedConfiguration(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState, deployment *appsv1.Deployment, storagePVC string, imageArgs ...[]string) bool {
	if input.SchemaVersion == 0 {
		return true
	}
	if deployment == nil || len(deployment.Spec.Template.Spec.Containers) == 0 {
		return false
	}
	podSpec := &deployment.Spec.Template.Spec
	container := &podSpec.Containers[0]
	if !verifyTypedApplicationExecution(input, component, podSpec.SecurityContext) {
		return false
	}
	persistentMounts, scratchMounts := input.Revision.PersistentMounts, input.Revision.ScratchMounts
	command, arguments := input.Revision.Entrypoint, []string(nil)
	if len(command) > 0 && len(imageArgs) > 0 {
		arguments = imageArgs[0]
	}
	scratchPrefix := "scratch-"
	if component.Role == contracts.WorkspaceApplicationComponentDependency {
		dependency, err := dependencySpecByName(input.Revision, component.Name)
		if err != nil {
			return false
		}
		persistentMounts, scratchMounts = dependency.PersistentMounts, dependency.ScratchMounts
		command, arguments = dependency.Command.Entrypoint, dependency.Command.Args
		scratchPrefix = "dependency-scratch-"
		expectedPorts := make([]corev1.ContainerPort, 0, len(dependency.Ports))
		for _, port := range dependency.Ports {
			expectedPorts = append(expectedPorts, corev1.ContainerPort{
				Name:          port.Name,
				ContainerPort: int32(port.Port),
				Protocol:      corev1.Protocol(port.Protocol),
			})
		}
		if len(expectedPorts) == 0 {
			if len(container.Ports) != 0 {
				return false
			}
		} else if !reflect.DeepEqual(expectedPorts, container.Ports) {
			return false
		}
		if len(dependency.HealthChecks) > 1 {
			return false
		}
		probe := container.ReadinessProbe
		if len(dependency.HealthChecks) == 1 {
			check := dependency.HealthChecks[0]
			if probe == nil || probe.InitialDelaySeconds != int32(check.InitialDelaySeconds) || probe.PeriodSeconds != 10 || probe.GRPC != nil {
				return false
			}
			switch check.Type {
			case "http":
				if probe.Exec != nil || probe.TCPSocket != nil || probe.HTTPGet == nil ||
					probe.HTTPGet.Path != check.Path || probe.HTTPGet.Port.IntValue() != check.Port {
					return false
				}
			case "exec":
				if probe.HTTPGet != nil || probe.TCPSocket != nil || probe.Exec == nil ||
					!reflect.DeepEqual(check.Command, probe.Exec.Command) {
					return false
				}
			default:
				if probe.Exec != nil || probe.HTTPGet != nil || probe.TCPSocket == nil ||
					probe.TCPSocket.Port.IntValue() != check.Port {
					return false
				}
			}
		} else if probe != nil {
			return false
		}
	}
	if !equalStringSlices(command, container.Command) || !equalStringSlices(arguments, container.Args) {
		return false
	}
	expectedEnv := workspaceApplicationComponentEnvironment(input, component)
	if len(expectedEnv) != len(container.Env) || (len(expectedEnv) > 0 && !reflect.DeepEqual(expectedEnv, container.Env)) {
		return false
	}
	if deployment.Annotations["oplcloud.cn/configuration-digest"] != input.ConfigurationDigest {
		return false
	}
	mounts := map[string]corev1.VolumeMount{}
	for _, mount := range container.VolumeMounts {
		if mount.MountPath == "" || mount.SubPathExpr != "" {
			return false
		}
		if _, dup := mounts[mount.MountPath]; dup {
			return false
		}
		mounts[mount.MountPath] = mount
	}
	volumes := map[string]corev1.Volume{}
	for _, volume := range podSpec.Volumes {
		if volume.Name == "" {
			return false
		}
		if _, dup := volumes[volume.Name]; dup {
			return false
		}
		volumes[volume.Name] = volume
	}
	consumeMount := func(path, name, subPath string, readOnly bool) bool {
		mount, ok := mounts[path]
		if !ok || mount.Name != name || mount.SubPath != subPath || mount.ReadOnly != readOnly {
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
		volume, ok := volumes["workspace-data"]
		expectedSource := corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: storagePVC,
				ReadOnly:  false,
			},
		}
		if !ok || storagePVC == "" || !reflect.DeepEqual(volume.VolumeSource, expectedSource) {
			return false
		}
		delete(volumes, "workspace-data")
	}
	for i, mount := range scratchMounts {
		name := fmt.Sprintf("%s%d", scratchPrefix, i)
		volume, ok := volumes[name]
		if !consumeMount(mount.MountPath, name, "", false) || !ok || volume.EmptyDir == nil || volume.EmptyDir.Medium != corev1.StorageMediumMemory {
			return false
		}
		if !reflect.DeepEqual(volume.VolumeSource, corev1.VolumeSource{EmptyDir: volume.EmptyDir}) {
			return false
		}
		if mount.SizeBytes > 0 {
			if volume.EmptyDir.SizeLimit == nil || volume.EmptyDir.SizeLimit.Cmp(*resource.NewQuantity(mount.SizeBytes, resource.DecimalSI)) != 0 {
				return false
			}
		} else if volume.EmptyDir.SizeLimit != nil && !volume.EmptyDir.SizeLimit.IsZero() {
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
		volume, ok := volumes["application-secrets"]
		mode := int32(0440)
		expectedSource := corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName:  ComponentResourceName(input, "secrets"),
				DefaultMode: &mode,
			},
		}
		if !ok || !reflect.DeepEqual(volume.VolumeSource, expectedSource) {
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
		volume, ok := volumes["application-config"]
		mode := int32(0444)
		expectedSource := corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: ComponentResourceName(input, "config")},
				DefaultMode:          &mode,
			},
		}
		if !ok || !reflect.DeepEqual(volume.VolumeSource, expectedSource) {
			return false
		}
		delete(volumes, "application-config")
	}
	return len(mounts) == 0 && len(volumes) == 0
}

func equalStringSlices(expected, actual []string) bool {
	if len(expected) == 0 {
		return len(actual) == 0
	}
	return reflect.DeepEqual(expected, actual)
}

// VerifyConfiguration validates an untyped Deployment readback map against the
// declared Workspace application configuration.
func VerifyConfiguration(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState, deployment map[string]any, storagePVC string, imageArgs ...[]string) bool {
	if input.SchemaVersion == 0 {
		return true
	}
	var typed appsv1.Deployment
	if json.Unmarshal(mustJSON(deployment), &typed) != nil {
		return false
	}
	return verifyTypedConfiguration(input, component, &typed, storagePVC, imageArgs...)
}

func (p *Executor) ReadWorkspaceApplicationRuntimeCredentials(ctx context.Context, input WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	if contracts.WorkspaceApplicationCredentialKind(input.Revision, contracts.WorkspaceApplicationCredentialGatewayKey) {
		if _, err := workspaceApplicationGatewayBinding(input); err != nil {
			return contracts.WorkspaceApplicationRuntimeCredentials{}, err
		}
	}
	secret, err := p.applicationSecretObject(ctx, input)
	if err != nil {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, err
	}
	admin, declared := contracts.WorkspaceApplicationDeclaredCredential(input.Revision, contracts.WorkspaceApplicationCredentialWorkspaceAdminPassword)
	if !declared {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, errors.New("workspace_application_credentials_unavailable")
	}
	password := secret.Data[admin.Name]
	if len(password) == 0 {
		return contracts.WorkspaceApplicationRuntimeCredentials{}, ErrLaunchStageBindingConflict
	}
	return contracts.WorkspaceApplicationRuntimeCredentials{
		RuntimeID:     applicationRuntimeID(input),
		WorkspaceID:   input.WorkspaceID,
		WebUIUsername: admin.Username,
		WebUIPassword: string(password),
	}, nil
}
