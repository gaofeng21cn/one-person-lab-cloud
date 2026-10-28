package fabric

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestTencentApplicationSecretBindingRepairsLegacyAccountAnnotation(t *testing.T) {
	const workspace, oldAccount, newAccount = "ws-alpha", "tenant-alpha", "acct-alpha"
	const rawKey = "raw-gateway-key"
	digest := sha256.Sum256([]byte(rawKey))
	fingerprint := "sha256:" + hex.EncodeToString(digest[:])
	secret := map[string]any{
		"apiVersion": "v1", "kind": "Secret", "type": "Opaque",
		"metadata": map[string]any{
			"name":   gatewaySecretName(workspace),
			"labels": map[string]any{"app.kubernetes.io/name": "opl-gateway-secret"},
			"annotations": map[string]any{
				"oplcloud.cn/account-id":           oldAccount,
				"oplcloud.cn/workspace-id":         workspace,
				"oplcloud.cn/workspace-api-key-id": "19",
				"oplcloud.cn/secret-version":       hex.EncodeToString(digest[:])[:16],
				"oplcloud.cn/secret-fingerprint":   fingerprint,
			},
		},
		"data": map[string]any{"opl_gateway_api_key": base64.StdEncoding.EncodeToString([]byte(rawKey))},
	}
	provider := NewTencentProvider()
	var calls [][]string
	provider.kubectl = func(_ context.Context, args []string, _ []byte) ([]byte, error) {
		calls = append(calls, slices.Clone(args))
		switch {
		case slices.Equal(args, []string{"get", "secret/" + gatewaySecretName(workspace), "--ignore-not-found", "-o", "json"}):
			return json.Marshal(secret)
		case len(args) == 4 && args[0] == "annotate" && args[1] == "secret/"+gatewaySecretName(workspace) && args[3] == "--overwrite":
			key, value, ok := strings.Cut(args[2], "=")
			if !ok || key != "oplcloud.cn/account-id" {
				t.Fatalf("unexpected annotation=%#v", args)
			}
			secret["metadata"].(map[string]any)["annotations"].(map[string]any)[key] = value
			return nil, nil
		default:
			t.Fatalf("unexpected kubectl args=%#v", args)
			return nil, nil
		}
	}

	binding, err := provider.BindWorkspaceApplicationSecret(context.Background(), SecretBindInput{
		AccountID: newAccount, WorkspaceID: workspace, RuntimeInstanceID: "runtime-alpha",
		SecretRef: gatewaySecretName(workspace), Fingerprint: fingerprint, TargetSlot: "gateway",
	})
	if err != nil {
		t.Fatal(err)
	}
	if binding.SecretRef != gatewaySecretName(workspace) || binding.Version != hex.EncodeToString(digest[:])[:16] || binding.Fingerprint != fingerprint {
		t.Fatalf("binding=%#v", binding)
	}
	if len(calls) != 3 || !slices.Equal(calls[1], []string{"annotate", "secret/" + gatewaySecretName(workspace), "oplcloud.cn/account-id=" + newAccount, "--overwrite"}) {
		t.Fatalf("calls=%#v", calls)
	}
}
