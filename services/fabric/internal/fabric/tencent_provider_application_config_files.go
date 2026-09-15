package fabric

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	contracts "opl-cloud/packages/contracts/go"
	"opl-cloud/services/fabric/internal/protectedresource"
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

func verifyApplicationConfigObject(input WorkspaceApplicationRuntimeInput, config map[string]any) bool {
	if config["kind"] != "ConfigMap" || config["immutable"] != true ||
		stringValue(nested(config, "metadata", "name")) != workspaceApplicationComponentResourceName(input, "config") ||
		stringValue(nested(config, "metadata", "labels", "oplcloud.cn/account-id")) != k8sCostLabelValue(input.AccountID) ||
		stringValue(nested(config, "metadata", "labels", "oplcloud.cn/workspace-id")) != k8sCostLabelValue(input.WorkspaceID) ||
		stringValue(nested(config, "metadata", "labels", "oplcloud.cn/runtime-id")) != k8sCostLabelValue(applicationRuntimeID(input)) ||
		stringValue(nested(config, "metadata", "annotations", "oplcloud.cn/configuration-digest")) != input.ConfigurationDigest ||
		config["binaryData"] != nil {
		return false
	}
	return bytes.Equal(mustJSON(config["data"]), mustJSON(input.Configuration.Files))
}

func (p *TencentProvider) readApplicationConfigObject(ctx context.Context, input WorkspaceApplicationRuntimeInput) (map[string]any, error) {
	name := workspaceApplicationComponentResourceName(input, "config")
	raw, err := p.callKubectl(ctx, []string{"get", "configmap/" + name, "--ignore-not-found", "-o", "json"}, nil, protectedresource.Target{})
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, ErrWorkspaceLaunchResourceAbsent
	}
	var config map[string]any
	if json.Unmarshal(raw, &config) != nil || !verifyApplicationConfigObject(input, config) {
		return nil, ErrLaunchStageBindingConflict
	}
	return config, nil
}

func (p *TencentProvider) prepareApplicationConfigFiles(ctx context.Context, input WorkspaceApplicationRuntimeInput) error {
	if len(input.Configuration.Files) == 0 {
		return nil
	}
	if _, err := p.readApplicationConfigObject(ctx, input); err == nil {
		return nil
	} else if !errors.Is(err, ErrWorkspaceLaunchResourceAbsent) {
		return err
	}
	config := map[string]any{"apiVersion": "v1", "kind": "ConfigMap", "immutable": true,
		"metadata": map[string]any{"name": workspaceApplicationComponentResourceName(input, "config"),
			"labels":      map[string]string{"oplcloud.cn/account-id": k8sCostLabelValue(input.AccountID), "oplcloud.cn/workspace-id": k8sCostLabelValue(input.WorkspaceID), "oplcloud.cn/runtime-id": k8sCostLabelValue(applicationRuntimeID(input))},
			"annotations": map[string]string{"oplcloud.cn/configuration-digest": input.ConfigurationDigest}},
		"data": input.Configuration.Files}
	if _, err := p.callKubectl(ctx, []string{"create", "-f", "-"}, mustJSON(config), protectedresource.Target{}); err != nil {
		return err
	}
	_, err := p.readApplicationConfigObject(ctx, input)
	return err
}
