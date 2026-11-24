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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/validation"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	applymetav1 "k8s.io/client-go/applyconfigurations/meta/v1"
	applynetworkingv1 "k8s.io/client-go/applyconfigurations/networking/v1"

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

func workspaceNodeTolerations(packageID string) []*applycorev1.TolerationApplyConfiguration {
	return []*applycorev1.TolerationApplyConfiguration{
		applycorev1.Toleration().
			WithKey("oplcloud.cn/package-id").
			WithOperator(corev1.TolerationOpEqual).
			WithValue(packageID).
			WithEffect(corev1.TaintEffectNoSchedule),
	}
}

func workspaceEgressRules() []*applynetworkingv1.NetworkPolicyEgressRuleApplyConfiguration {
	return []*applynetworkingv1.NetworkPolicyEgressRuleApplyConfiguration{
		applynetworkingv1.NetworkPolicyEgressRule().
			WithTo(
				applynetworkingv1.NetworkPolicyPeer().
					WithNamespaceSelector(applymetav1.LabelSelector().WithMatchLabels(map[string]string{"kubernetes.io/metadata.name": "kube-system"})).
					WithPodSelector(applymetav1.LabelSelector().WithMatchLabels(map[string]string{"k8s-app": "kube-dns"})),
			).
			WithPorts(
				applynetworkingv1.NetworkPolicyPort().WithProtocol(corev1.ProtocolUDP).WithPort(intstr.FromInt32(53)),
				applynetworkingv1.NetworkPolicyPort().WithProtocol(corev1.ProtocolTCP).WithPort(intstr.FromInt32(53)),
			),
		applynetworkingv1.NetworkPolicyEgressRule().
			WithTo(
				applynetworkingv1.NetworkPolicyPeer().
					WithIPBlock(applynetworkingv1.IPBlock().
						WithCIDR("0.0.0.0/0").
						WithExcept("0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12", "192.168.0.0/16")),
			).
			WithPorts(
				applynetworkingv1.NetworkPolicyPort().WithProtocol(corev1.ProtocolTCP).WithPort(intstr.FromInt32(443)),
			),
		applynetworkingv1.NetworkPolicyEgressRule().
			WithTo(
				applynetworkingv1.NetworkPolicyPeer().
					WithIPBlock(applynetworkingv1.IPBlock().
						WithCIDR("::/0").
						WithExcept("::1/128", "fc00::/7", "fe80::/10")),
			).
			WithPorts(
				applynetworkingv1.NetworkPolicyPort().WithProtocol(corev1.ProtocolTCP).WithPort(intstr.FromInt32(443)),
			),
	}
}

func typedControllerUID(owners []metav1.OwnerReference, kind string) types.UID {
	var uid types.UID
	for _, owner := range owners {
		if owner.Controller == nil || !*owner.Controller {
			continue
		}
		if uid != "" || owner.Kind != kind || owner.UID == "" {
			return ""
		}
		uid = owner.UID
	}
	return uid
}

func typedPodReady(pod *corev1.Pod) bool {
	if pod == nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func typedRuntimeTemplateMatches(deployment *appsv1.Deployment, replicaSet *appsv1.ReplicaSet) bool {
	if deployment == nil || replicaSet == nil {
		return false
	}
	left := deployment.Spec.Template.DeepCopy()
	right := replicaSet.Spec.Template.DeepCopy()
	delete(left.Labels, "pod-template-hash")
	delete(right.Labels, "pod-template-hash")
	if len(left.Labels) == 0 {
		left.Labels = nil
	}
	if len(right.Labels) == 0 {
		right.Labels = nil
	}
	return reflect.DeepEqual(left, right)
}

func typedPodImageIDsMatch(pods []*corev1.Pod, labelKey, labelValue, containerName, expected string) bool {
	expectedDigest, ok := immutableImageDigest(expected)
	if !ok {
		return false
	}
	found := false
	for _, pod := range pods {
		if pod == nil {
			continue
		}
		label := pod.Labels[labelKey]
		if label == "" || (labelValue != "" && label != labelValue) || !typedPodReady(pod) {
			continue
		}
		containerFound := false
		for _, status := range pod.Status.ContainerStatuses {
			if status.Name != containerName {
				continue
			}
			containerFound = true
			found = true
			actualDigest, ok := runtimeImageDigest(status.ImageID)
			if !status.Ready || !ok || actualDigest != expectedDigest {
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
