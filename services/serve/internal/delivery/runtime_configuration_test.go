package delivery

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	api "opl-cloud/packages/contracts/go/api"
)

func runtimeConfigurationCommand() *api.RuntimeDeployCommand {
	digest := "sha256:" + strings.Repeat("a", 64)
	return &api.RuntimeDeployCommand{
		WorkspaceId: "ws-1", RuntimeInstanceId: "runtime-1", DeploymentDescriptorDigest: digest, ExecutionEpoch: 3,
		DeploymentDescriptor: &api.DeploymentDescriptor{ApplicationRevision: &api.WorkspaceApplicationRevision{
			SecretInputs:     []*api.WorkspaceApplicationSecretInput{{Name: "gateway", Target: proto.String("/run/gateway"), Env: proto.String("OPL_GATEWAY_KEY")}},
			ConfigInputs:     []*api.WorkspaceApplicationConfigInput{{Name: "config", Target: "/etc/agent/config.json"}},
			PersistentMounts: []*api.WorkspaceApplicationMount{{Name: "data", MountPath: "/var/lib/agent"}},
		}},
	}
}

func runtimeHandle(kind api.RuntimeInjectionHandle_Kind, slot string) *api.RuntimeInjectionHandle {
	return &api.RuntimeInjectionHandle{Kind: kind, HandleId: "handle-" + slot, DeliveryReference: "fabric://opaque/" + slot, WorkspaceId: "ws-1", RuntimeInstanceId: "runtime-1", DeploymentDescriptorDigest: "sha256:" + strings.Repeat("a", 64), TargetSlot: slot, ExecutionEpoch: 3, Fingerprint: "sha256:" + strings.Repeat("b", 64), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute))}
}

func TestRuntimeConfigurationAcceptsExactDeclaredOpaqueBindings(t *testing.T) {
	command := runtimeConfigurationCommand()
	secret := runtimeHandle(api.RuntimeInjectionHandle_SECRET, "gateway")
	config := runtimeHandle(api.RuntimeInjectionHandle_CONFIG, "config")
	mount := runtimeHandle(api.RuntimeInjectionHandle_DATA_MOUNT, "data")
	command.RuntimeConfiguration = &api.WorkspaceApplicationRuntimeConfiguration{
		ConfigBindings: []*api.RuntimeConfigBinding{{InputName: "config", Target: "/etc/agent/config.json", Handle: config}},
		SecretBindings: []*api.RuntimeSecretBindingReference{{InputName: "gateway", Target: "/run/gateway", Env: "OPL_GATEWAY_KEY", SecretBindingId: "secret-binding-1", Handle: secret, Fingerprint: secret.Fingerprint}},
		MountBindings:  []*api.RuntimeMountBinding{{MountName: "data", Target: "/var/lib/agent", AccessMode: "read_write", Handle: mount}},
	}
	if err := validateRuntimeConfiguration(command); err != nil {
		t.Fatalf("exact configuration rejected: %v", err)
	}
}

func TestRuntimeConfigurationRejectsScopeDriftMissingSlotAndHostPath(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*api.RuntimeDeployCommand)
	}{
		{"foreign workspace", func(c *api.RuntimeDeployCommand) {
			h := runtimeHandle(api.RuntimeInjectionHandle_SECRET, "gateway")
			h.WorkspaceId = "other"
			c.RuntimeConfiguration = &api.WorkspaceApplicationRuntimeConfiguration{SecretBindings: []*api.RuntimeSecretBindingReference{{InputName: "gateway", Target: "/run/gateway", Env: "OPL_GATEWAY_KEY", SecretBindingId: "s", Handle: h, Fingerprint: h.Fingerprint}}}
		}},
		{"missing declared slot", func(c *api.RuntimeDeployCommand) {
			c.RuntimeConfiguration = &api.WorkspaceApplicationRuntimeConfiguration{}
		}},
		{"host path", func(c *api.RuntimeDeployCommand) {
			h := runtimeHandle(api.RuntimeInjectionHandle_DATA_MOUNT, "data")
			h.DeliveryReference = "/var/lib/host"
			c.RuntimeConfiguration = &api.WorkspaceApplicationRuntimeConfiguration{MountBindings: []*api.RuntimeMountBinding{{MountName: "data", Target: "/var/lib/agent", AccessMode: "read_write", Handle: h}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := runtimeConfigurationCommand()
			tc.mutate(c)
			if err := validateRuntimeConfiguration(c); err == nil {
				t.Fatal("invalid runtime configuration was accepted")
			}
		})
	}
}
