package tkeapply

import (
	"context"
	"encoding/json"
	"errors"
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/internal/protectedresource"
)

func workspaceApplicationComponentConfigInputs(input WorkspaceApplicationRuntimeInput, component contracts.WorkspaceApplicationRuntimeComponentState) []contracts.WorkspaceApplicationConfigInput {
	if component.Role == contracts.WorkspaceApplicationComponentMain {
		return input.Revision.ConfigInputs
	}
	dependency, err := dependencySpecByName(input.Revision, component.Name)
	if err != nil {
		return nil
	}
	return dependency.ConfigInputs
}

func verifyTypedConfigMap(input WorkspaceApplicationRuntimeInput, config *corev1.ConfigMap) bool {
	if config == nil ||
		(config.Kind != "" && config.Kind != "ConfigMap") ||
		config.Immutable == nil || !*config.Immutable ||
		config.Name != ComponentResourceName(input, "config") ||
		config.Labels["oplcloud.cn/account-id"] != k8sCostLabelValue(input.AccountID) ||
		config.Labels["oplcloud.cn/workspace-id"] != k8sCostLabelValue(input.WorkspaceID) ||
		config.Labels["oplcloud.cn/runtime-id"] != k8sCostLabelValue(applicationRuntimeID(input)) ||
		config.Annotations["oplcloud.cn/configuration-digest"] != input.ConfigurationDigest ||
		config.BinaryData != nil {
		return false
	}
	if len(input.Configuration.Files) == 0 {
		return len(config.Data) == 0
	}
	return maps.Equal(config.Data, input.Configuration.Files)
}

// VerifyConfigObject verifies an untyped ConfigMap readback against the
// declared application configuration files.
func VerifyConfigObject(input WorkspaceApplicationRuntimeInput, config map[string]any) bool {
	if config == nil || config["kind"] != "ConfigMap" {
		return false
	}
	var typed corev1.ConfigMap
	if json.Unmarshal(mustJSON(config), &typed) != nil {
		return false
	}
	return verifyTypedConfigMap(input, &typed)
}

func (p *Executor) readApplicationConfigObject(ctx context.Context, input WorkspaceApplicationRuntimeInput) (*corev1.ConfigMap, error) {
	name := ComponentResourceName(input, "config")
	config, err := p.getConfigMap(ctx, name)
	if err != nil {
		return nil, err
	}
	if config == nil {
		return nil, ErrResourceAbsent
	}
	if !verifyTypedConfigMap(input, config) {
		return nil, ErrLaunchStageBindingConflict
	}
	return config, nil
}

func (p *Executor) prepareApplicationConfigFiles(ctx context.Context, input WorkspaceApplicationRuntimeInput) error {
	if len(input.Configuration.Files) == 0 {
		return nil
	}
	if _, err := p.readApplicationConfigObject(ctx, input); err == nil {
		return nil
	} else if !errors.Is(err, ErrResourceAbsent) {
		return err
	}
	immutable := true
	config := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "ConfigMap"},
		ObjectMeta: metav1.ObjectMeta{
			Name: ComponentResourceName(input, "config"),
			Labels: map[string]string{
				"oplcloud.cn/account-id":   k8sCostLabelValue(input.AccountID),
				"oplcloud.cn/workspace-id": k8sCostLabelValue(input.WorkspaceID),
				"oplcloud.cn/runtime-id":   k8sCostLabelValue(applicationRuntimeID(input)),
			},
			Annotations: map[string]string{
				"oplcloud.cn/configuration-digest": input.ConfigurationDigest,
			},
		},
		Immutable: &immutable,
		Data:      maps.Clone(input.Configuration.Files),
	}
	if err := p.createConfigMap(ctx, config, protectedresource.Target{}); err != nil {
		return err
	}
	_, err := p.readApplicationConfigObject(ctx, input)
	return err
}
