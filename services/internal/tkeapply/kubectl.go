package tkeapply

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// KubectlRunner is the installation's single cluster-access boundary for
// Workspace application workloads: one kubectl invocation against the
// installation's own namespace, using the installation's own kubeconfig Secret
// reference when it declares one. It never reads a provider credential from the
// caller and never re-derives the namespace from a request.
func KubectlRunner(kubeconfigRef, namespace string) func(ctx context.Context, args []string, stdin []byte) ([]byte, error) {
	kubeconfigRef, namespace = strings.TrimSpace(kubeconfigRef), strings.TrimSpace(namespace)
	return func(ctx context.Context, args []string, stdin []byte) ([]byte, error) {
		if namespace == "" {
			return nil, fmt.Errorf("OPL_K8S_NAMESPACE is required")
		}
		base := []string{}
		if kubeconfigRef != "" {
			base = append(base, "--kubeconfig", kubeconfigRef)
		}
		base = append(base, "--namespace", namespace)
		base = append(base, args...)
		cmd := exec.CommandContext(ctx, "kubectl", base...)
		if stdin != nil {
			cmd.Stdin = bytes.NewReader(stdin)
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		output, err := cmd.Output()
		if err != nil {
			message := strings.TrimSpace(stderr.String())
			if message == "" {
				message = strings.TrimSpace(string(output))
			}
			return output, fmt.Errorf("%s: %s", err, message)
		}
		return output, nil
	}
}
