package contracts_test

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
)

func TestRuntimeConfigurationPublicJSONRoundTripContainsOnlyOpaqueFacts(t *testing.T) {
	message := &api.WorkspaceApplicationRuntimeConfiguration{SecretBindings: []*api.RuntimeSecretBindingReference{{InputName: "gateway", Target: "/run/gateway", Env: "OPL_GATEWAY_KEY", SecretBindingId: "binding-1", Fingerprint: "sha256:" + strings.Repeat("a", 64), Handle: &api.RuntimeInjectionHandle{Kind: api.RuntimeInjectionHandle_SECRET, HandleId: "handle-1", DeliveryReference: "fabric://opaque/1", WorkspaceId: "ws-1", RuntimeInstanceId: "runtime-1", DeploymentDescriptorDigest: "sha256:" + strings.Repeat("b", 64), TargetSlot: "gateway", ExecutionEpoch: 2, Fingerprint: "sha256:" + strings.Repeat("a", 64)}}}}
	raw, err := publicjson.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	var decoded api.WorkspaceApplicationRuntimeConfiguration
	if err := publicjson.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(message, &decoded) {
		t.Fatalf("round trip changed message: %s", raw)
	}
	if strings.Contains(string(raw), "value") || strings.Contains(string(raw), "secretMaterial") {
		t.Fatalf("raw value leaked into public JSON: %s", raw)
	}
}

func TestRuntimeConfigurationPublicJSONRejectsRawValue(t *testing.T) {
	var decoded api.WorkspaceApplicationRuntimeConfiguration
	if err := publicjson.Unmarshal([]byte(`{"secretBindings":[{"inputName":"gateway","target":"/run/gateway","env":"OPL_GATEWAY_KEY","secretBindingId":"binding-1","value":"secret"}]}`), &decoded); err == nil {
		t.Fatal("raw runtime value was accepted at the typed contract boundary")
	}
}
