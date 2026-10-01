package tkeapply

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
	contracts "opl-cloud/packages/contracts/go"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}

func number(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func nested(root map[string]any, keys ...string) any {
	var current any = root
	for _, key := range keys {
		asMap, ok := current.(map[string]any)
		if !ok {
			if raw, ok := current.(map[string]string); ok {
				return raw[key]
			}
			return nil
		}
		current = asMap[key]
	}
	return current
}

func mustJSON(value any) []byte {
	body, _ := json.Marshal(value)
	return body
}

func k8sName(id string) string {
	name := compactID(id)
	if len(name) > 54 {
		name = name[:54]
	}
	return "opl-" + strings.Trim(name, "-")
}

func k8sCostLabels(tags map[string]string) map[string]string {
	return map[string]string{
		"oplcloud.cn/account-id":   k8sCostLabelValue(tags["opl_account_id"]),
		"oplcloud.cn/workspace-id": k8sCostLabelValue(tags["opl_workspace_id"]),
		"oplcloud.cn/resource-id":  k8sCostLabelValue(tags["opl_resource_id"]),
		"oplcloud.cn/operation-id": k8sCostLabelValue(tags["opl_operation_id"]),
	}
}

func k8sCostLabelValue(value string) string {
	if len(validation.IsValidLabelValue(value)) == 0 {
		return value
	}
	digest := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s-%x", compactID(value), digest[:7])
}

func mergeStringMaps(values ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, value := range values {
		for key, item := range value {
			if strings.TrimSpace(item) != "" {
				merged[key] = item
			}
		}
	}
	return merged
}

func oplCostTags(accountID string, workspaceID string, resourceID string, operationID string) map[string]string {
	return map[string]string{
		"opl_account_id":   accountID,
		"opl_workspace_id": workspaceID,
		"opl_resource_id":  resourceID,
		"opl_operation_id": operationID,
	}
}

func stableID(parts ...string) string {
	hash := sha1.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func gatewaySecretName(workspaceID string) string {
	return contracts.WorkspaceGatewaySecretRef(workspaceID)
}

func workspaceNodeTolerations(packageID string) []any {
	return []any{
		map[string]any{"key": "oplcloud.cn/package-id", "operator": "Equal", "value": packageID, "effect": "NoSchedule"},
	}
}

func workspaceEgressRules() []any {
	return []any{
		map[string]any{
			"to": []any{map[string]any{
				"namespaceSelector": map[string]any{"matchLabels": map[string]any{"kubernetes.io/metadata.name": "kube-system"}},
				"podSelector":       map[string]any{"matchLabels": map[string]any{"k8s-app": "kube-dns"}},
			}},
			"ports": []any{map[string]any{"protocol": "UDP", "port": 53}, map[string]any{"protocol": "TCP", "port": 53}},
		},
		map[string]any{
			"to": []any{map[string]any{"ipBlock": map[string]any{
				"cidr": "0.0.0.0/0", "except": []any{"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16"},
			}}},
			"ports": []any{map[string]any{"protocol": "TCP", "port": 443}},
		},
		map[string]any{
			"to": []any{map[string]any{"ipBlock": map[string]any{
				"cidr": "::/0", "except": []any{"::1/128", "fc00::/7", "fe80::/10"},
			}}},
			"ports": []any{map[string]any{"protocol": "TCP", "port": 443}},
		},
	}
}

func firstContainerField(deployment map[string]any, key string) any {
	containers, _ := nested(deployment, "spec", "template", "spec", "containers").([]any)
	if len(containers) == 0 {
		return nil
	}
	container, _ := containers[0].(map[string]any)
	return container[key]
}

func strictKubectlItems(raw []byte) ([]any, error) {
	var list map[string]any
	if err := json.Unmarshal(raw, &list); err != nil || len(list) == 0 {
		return nil, fmt.Errorf("kubernetes_response_invalid")
	}
	if stringValue(list["kind"]) == "List" {
		items, ok := list["items"].([]any)
		if !ok {
			return nil, fmt.Errorf("kubernetes_response_invalid")
		}
		return items, nil
	}
	if stringValue(list["kind"]) == "" {
		return nil, fmt.Errorf("kubernetes_response_invalid")
	}
	return []any{list}, nil
}

func resourceName(value string) string {
	parts := strings.Split(value, "/")
	return parts[len(parts)-1]
}

func conditionStatuses(value any) map[string]string {
	statuses := map[string]string{}
	conditions, _ := value.([]any)
	for _, condition := range conditions {
		asMap, _ := condition.(map[string]any)
		statuses[stringValue(asMap["type"])] = stringValue(asMap["status"])
	}
	return statuses
}

func controllerUID(resource map[string]any, kind string) string {
	owners, _ := nested(resource, "metadata", "ownerReferences").([]any)
	uid := ""
	for _, value := range owners {
		owner, _ := value.(map[string]any)
		if owner["controller"] != true {
			continue
		}
		if uid != "" || stringValue(owner["kind"]) != kind || stringValue(owner["uid"]) == "" {
			return ""
		}
		uid = stringValue(owner["uid"])
	}
	return uid
}

func runtimeTemplateMatches(deployment, replicaSet map[string]any) bool {
	clone := func(resource map[string]any) map[string]any {
		template, _ := nested(resource, "spec", "template").(map[string]any)
		result := map[string]any{}
		for key, value := range template {
			result[key] = value
		}
		metadata, _ := result["metadata"].(map[string]any)
		copied := map[string]any{}
		for key, value := range metadata {
			copied[key] = value
		}
		labels, _ := copied["labels"].(map[string]any)
		labelCopy := map[string]any{}
		for key, value := range labels {
			if key != "pod-template-hash" {
				labelCopy[key] = value
			}
		}
		copied["labels"] = labelCopy
		result["metadata"] = copied
		return result
	}
	return nested(deployment, "spec", "template") != nil && reflect.DeepEqual(clone(deployment), clone(replicaSet))
}

func podImageIDsMatch(pods []any, labelKey, labelValue, containerName, expected string) bool {
	expectedDigest, ok := immutableImageDigest(expected)
	if !ok {
		return false
	}
	found := false
	for _, item := range pods {
		pod, _ := item.(map[string]any)
		labels, _ := nested(pod, "metadata", "labels").(map[string]any)
		label := stringValue(labels[labelKey])
		if label == "" || (labelValue != "" && label != labelValue) || stringValue(nested(pod, "status", "phase")) != "Running" || conditionStatuses(nested(pod, "status", "conditions"))["Ready"] != "True" {
			continue
		}
		containerFound := false
		statuses, _ := nested(pod, "status", "containerStatuses").([]any)
		for _, item := range statuses {
			status, _ := item.(map[string]any)
			if stringValue(status["name"]) != containerName {
				continue
			}
			containerFound = true
			found = true
			actualDigest, ok := runtimeImageDigest(stringValue(status["imageID"]))
			if status["ready"] != true || !ok || actualDigest != expectedDigest {
				return false
			}
		}
		if !containerFound {
			return false
		}
	}
	return found
}

func stableSuffix(values ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(values, ":")))
	return hex.EncodeToString(sum[:])
}

func applicationRuntimeID(input WorkspaceApplicationRuntimeInput) string {
	if input.SchemaVersion == 0 {
		return contracts.WorkspaceApplicationHistoricalRuntimeID(input.WorkspaceID)
	}
	return workspaceApplicationRuntimeID(input.RuntimeOperationID)
}

func applicationPersistentSubPath(input WorkspaceApplicationRuntimeInput, mount contracts.WorkspaceApplicationMount) string {
	if input.SchemaVersion == 0 || input.DataLayout == "legacy_application" {
		return "main/" + mount.Name
	}
	if input.DataLayout == "legacy_opl" {
		return mount.Name
	}
	return contracts.WorkspaceApplicationDataDirectory(input.DataBindingID) + "/" + mount.Name
}

func dependencySpecByName(revision contracts.WorkspaceApplicationRevision, name string) (contracts.WorkspaceApplicationDependency, error) {
	for _, dependency := range revision.Dependencies {
		if dependency.Name == name {
			return dependency, nil
		}
	}
	return contracts.WorkspaceApplicationDependency{}, fmt.Errorf("local_docker_application_dependency_spec_missing")
}

func deriveWorkspaceAdminPassword(seed string, workspaceID string, token string) string {
	secret := strings.TrimSpace(seed)
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(workspaceID + ":" + token))
	digest := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(digest) > 24 {
		digest = digest[:24]
	}
	return "opl_" + digest + "Aa1!"
}

func deriveWorkspaceSessionSecret(seed string, workspaceID string, token string) string {
	secret := strings.TrimSpace(seed)
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("webui-session:" + workspaceID + ":" + token))
	return hex.EncodeToString(mac.Sum(nil))
}

func compactID(value string) string {
	cleaned := strings.Builder{}
	lastDash := false
	for _, r := range strings.ToLower(value) {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if ok {
			cleaned.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			cleaned.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(cleaned.String(), "-")
	if len(result) > 48 {
		result = strings.Trim(result[:48], "-")
	}
	if result == "" {
		return "resource"
	}
	return result
}

func immutableImageDigest(value string) (string, bool) {
	repository, digest, ok := strings.Cut(strings.TrimSpace(value), "@")
	if !ok || repository == "" || repository != strings.TrimSpace(repository) || strings.ContainsAny(repository, " \t\r\n<>\"") || strings.Contains(digest, "@") || !strings.HasPrefix(digest, "sha256:") || len(digest) != len("sha256:")+64 {
		return "", false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(digest, "sha256:"))
	return digest, err == nil
}

func runtimeImageDigest(value string) (string, bool) {
	value = strings.TrimSpace(value)
	for _, prefix := range []string{"docker-pullable://", "containerd://"} {
		if strings.HasPrefix(value, prefix) {
			value = strings.TrimPrefix(value, prefix)
			break
		}
	}
	if strings.Contains(value, "://") {
		return "", false
	}
	if strings.Contains(value, "@") {
		return immutableImageDigest(value)
	}
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+64 {
		return "", false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return value, err == nil
}

func workspaceApplicationRuntimeID(runtimeOperationID string) string {
	return contracts.WorkspaceApplicationRuntimeID(runtimeOperationID)
}

func applicationLifecycleResult(observation contracts.WorkspaceApplicationRuntimeObservation) WorkspaceApplicationRuntimeLifecycleResult {
	state := observation.Status
	if state == "ready" {
		state = "running"
	}
	if state == "failed" {
		state = "pending"
	}
	return WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: observation.RuntimeID, WorkspaceID: observation.WorkspaceID, State: state, Observation: observation}
}
