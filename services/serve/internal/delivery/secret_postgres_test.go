package delivery_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/serve/internal/delivery"
)

// gatewayForServe is a GatewayCoordination fixture that mints one managed key per
// runtime and records how many times it was asked, so a replay can be told apart.
type gatewayForServe struct {
	api.GatewayCoordinationClient
	calls      int
	management *api.ManagedKeyBinding
}

func (g *gatewayForServe) CreateManagedKey(context.Context, *api.ManagedKeyCommand, ...grpc.CallOption) (*api.ManagedKeyBinding, error) {
	g.calls++
	return proto.Clone(g.management).(*api.ManagedKeyBinding), nil
}

// secretResources extends the Serve resource fixture with the Fabric BindSecret
// confirmation and records the exact binding it confirmed.
type secretResources struct {
	*resourcesForServe
	binds       int
	lastCommand *api.SecretBindingCommand
	conflict    bool
}

func (r *secretResources) BindSecret(_ context.Context, c *api.SecretBindingCommand, _ ...grpc.CallOption) (*api.SecretBindingReadback, error) {
	r.binds++
	c = proto.Clone(c).(*api.SecretBindingCommand)
	r.lastCommand = c
	if r.conflict {
		return &api.SecretBindingReadback{SecretBindingId: "sbx_" + c.GetRuntimeInstanceId(), RuntimeInstanceId: c.GetRuntimeInstanceId(), Fingerprint: "sha256:other", Version: "other", Outcome: api.Observation_OBSERVATION_CONFIRMED}, nil
	}
	return &api.SecretBindingReadback{SecretBindingId: "sbx_" + c.GetRuntimeInstanceId(), RuntimeInstanceId: c.GetRuntimeInstanceId(), Fingerprint: c.GetFingerprint(), Version: c.GetFingerprint()[7:23], Outcome: api.Observation_OBSERVATION_CONFIRMED}, nil
}

// managedKeyFixture reserves a delivery whose revision declares the platform
// Gateway credential, then binds the Gateway and Fabric Secret stubs.
func managedKeyFixture(t *testing.T) (*delivery.Service, *api.RuntimeReservationCommand, *api.RuntimeReservation, *gatewayForServe, *secretResources) {
	t.Helper()
	s, r, cap := reservationFixture(t)
	r.DeploymentDescriptor.ApplicationRevision.Credentials = []*api.WorkspaceApplicationCredential{{Name: "gateway", Kind: api.WorkspaceApplicationCredentialKindEnum_WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_GATEWAY_KEY, Target: "/run/secrets/opl_gateway_api_key"}}
	// The declared credential changes the descriptor, so its digest must be re-derived
	// from the canonical public bytes; Reserve authenticates the descriptor against it.
	rawDescriptor, err := publicjson.Marshal(r.GetDeploymentDescriptor())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(rawDescriptor)
	r.DeploymentDescriptorDigest = "sha256:" + hex.EncodeToString(sum[:])
	// Capability must confirm the very descriptor and digest the reservation carries.
	cap.version.DeploymentDescriptor = r.GetDeploymentDescriptor()
	cap.version.DeploymentDescriptorDigest = r.GetDeploymentDescriptorDigest()
	ctx := workspaceContext()
	out, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	const fingerprint = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	gateway := &gatewayForServe{management: &api.ManagedKeyBinding{KeyBindingId: "key-1", Fingerprint: fingerprint, SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(r.WorkspaceId), WorkspaceId: r.WorkspaceId, TargetRuntimeInstanceId: out.RuntimeInstanceId}}
	resources := &secretResources{resourcesForServe: &resourcesForServe{confirmed: true}}
	s.Gateway = gateway
	s.Resources = resources
	s.Runtime = &runtimeForServe{}
	return s, r, out, gateway, resources
}

// declareGatewayCredential makes one reservation's revision declare the
// installation Gateway credential, then re-binds the descriptor digest and the
// capability fixture to the exact bytes Reserve authenticates.
func declareGatewayCredential(t *testing.T, r *api.RuntimeReservationCommand, cap *capabilityForServe) {
	t.Helper()
	r.DeploymentDescriptor.ApplicationRevision.Credentials = []*api.WorkspaceApplicationCredential{{Name: "gateway", Kind: api.WorkspaceApplicationCredentialKindEnum_WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_GATEWAY_KEY, Target: "/run/secrets/opl_gateway_api_key"}}
	rawDescriptor, err := publicjson.Marshal(r.GetDeploymentDescriptor())
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(rawDescriptor)
	r.DeploymentDescriptorDigest = "sha256:" + hex.EncodeToString(sum[:])
	cap.version.DeploymentDescriptor = r.GetDeploymentDescriptor()
	cap.version.DeploymentDescriptorDigest = r.GetDeploymentDescriptorDigest()
}

// confirmedReloadBindingFixture is the opaque Gateway/Fabric readback a
// Workspace presents for one model-configuration version: Gateway's key binding
// identity delivered into this Workspace's installation Gateway Secret slot.
func confirmedReloadBindingFixture(workspaceID, keyBindingID, version string) *api.RuntimeManagedKeyBinding {
	return &api.RuntimeManagedKeyBinding{
		KeyBindingId: keyBindingID, Fingerprint: "sha256:" + strings.Repeat("ab", 32),
		SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(workspaceID),
		TargetSlot:              "gateway", SecretBindingId: "sbx_" + keyBindingID, SecretVersion: version,
	}
}

func TestServeDeployResolvesAndInjectsManagedKey(t *testing.T) {
	s, r, out, gateway, resources := managedKeyFixture(t)
	ctx := workspaceContext()
	command := deployReserved(r, out)
	command.ModelSelections = []*api.ModelSelection{{Slot: "default", ModelId: "model-alpha"}}
	readback, err := s.Deploy(ctx, command)
	if err != nil || readback.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
		t.Fatalf("deploy=%v %v", readback, err)
	}
	if gateway.calls != 1 || resources.binds != 1 {
		t.Fatalf("gateway=%d binds=%d", gateway.calls, resources.binds)
	}
	if resources.lastCommand.GetWorkspaceId() != r.WorkspaceId || resources.lastCommand.GetRuntimeInstanceId() != out.RuntimeInstanceId || resources.lastCommand.GetKeyBindingId() != "key-1" || resources.lastCommand.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(r.WorkspaceId) {
		t.Fatalf("bind command=%+v", resources.lastCommand)
	}
	// The readiness event reports injection only because that binding exists.
	var verified bool
	if err := s.DB.QueryRowContext(ctx, `SELECT (payload->>'credentialInjectionVerified')::boolean FROM serve.outbox_events WHERE event_type='serve.agent_readiness_observed.v1' AND aggregate_id=$1`, out.DeploymentId).Scan(&verified); err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("readiness event did not report verified credential injection")
	}
	// A second Deploy replays the same binding without a second Gateway mint.
	if _, err = s.Deploy(ctx, command); err != nil {
		t.Fatal(err)
	}
	if gateway.calls != 1 || resources.binds != 1 {
		t.Fatalf("replay gateway=%d binds=%d", gateway.calls, resources.binds)
	}
}

func TestServeDeployRefusesUnconfirmedManagedKeyBinding(t *testing.T) {
	s, r, out, _, resources := managedKeyFixture(t)
	resources.conflict = true
	command := deployReserved(r, out)
	command.ModelSelections = []*api.ModelSelection{{Slot: "default", ModelId: "model-alpha"}}
	if _, err := s.Deploy(workspaceContext(), command); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("conflicting binding err=%v", err)
	}
	// A refused binding must not record the deployment as active.
	var status string
	if err := s.DB.QueryRowContext(context.Background(), `SELECT status FROM serve.agent_deployments WHERE id=$1`, out.DeploymentId).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "active" {
		t.Fatal("a refused managed-key binding activated the deployment")
	}
}
