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
	"google.golang.org/protobuf/encoding/protojson"
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

// secretResources extends the Serve resource fixture with the Fabric Secret
// coordination a Serve-only caller reaches: it records every bind and replacement
// Serve requests and confirms exactly the identity the approved store was given,
// holding one active binding per runtime purpose like the real owner.
type secretResources struct {
	*resourcesForServe
	binds       int
	rebinds     int
	lastCommand *api.SecretBindingCommand
	lastRebind  *api.SecretBindingRebindCommand
	// active is the one binding the approved Secret store currently holds for the
	// fixture runtime purpose; a replacement CASes against it.
	active string
	// conflict makes the provider answer a different identity, so a caller that
	// trusts an unconfirmed readback can be told apart.
	conflict bool
}

func (r *secretResources) BindSecret(_ context.Context, c *api.SecretBindingCommand, _ ...grpc.CallOption) (*api.SecretBindingReadback, error) {
	r.binds++
	c = proto.Clone(c).(*api.SecretBindingCommand)
	r.lastCommand = c
	if r.conflict {
		return &api.SecretBindingReadback{SecretBindingId: "sbx_" + c.GetRuntimeInstanceId(), RuntimeInstanceId: c.GetRuntimeInstanceId(), Fingerprint: "sha256:other", Version: "other", Outcome: api.Observation_OBSERVATION_CONFIRMED}, nil
	}
	id := "sbx_" + c.GetRuntimeInstanceId()
	r.active = id
	return &api.SecretBindingReadback{SecretBindingId: id, RuntimeInstanceId: c.GetRuntimeInstanceId(), Fingerprint: c.GetFingerprint(), Version: c.GetFingerprint()[7:23], Outcome: api.Observation_OBSERVATION_CONFIRMED}, nil
}

func (r *secretResources) RebindSecret(_ context.Context, c *api.SecretBindingRebindCommand, _ ...grpc.CallOption) (*api.SecretBindingRebindReadback, error) {
	r.rebinds++
	c = proto.Clone(c).(*api.SecretBindingRebindCommand)
	r.lastRebind = c
	// The real owner compares the exact predecessor under its lock; a request that
	// names another binding is refused and the predecessor stays active.
	if c.GetExpectedCurrentSecretBindingId() != r.active {
		return nil, status.Error(codes.FailedPrecondition, "the active Secret binding is not the expected predecessor")
	}
	id := "sbx_" + c.GetRuntimeInstanceId() + "_" + c.GetKeyBindingId()
	r.active = id
	return &api.SecretBindingRebindReadback{PreviousSecretBindingId: c.GetExpectedCurrentSecretBindingId(), SecretBindingId: id, RuntimeInstanceId: c.GetRuntimeInstanceId(), Fingerprint: c.GetFingerprint(), Version: c.GetFingerprint()[7:23], Outcome: api.Observation_OBSERVATION_CONFIRMED, ReceiptId: id}, nil
}

// managedKeyFixture reserves a delivery whose revision declares the platform
// Gateway credential and wires the Fabric Secret coordination Serve now drives.
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
	gateway := &gatewayForServe{management: &api.ManagedKeyBinding{KeyBindingId: "key-1"}}
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

// launchManagedKeyHandover is the opaque input the Workspace coordinator hands
// Serve for a runtime whose revision declares the installation Gateway credential:
// Gateway's key binding, fingerprint, the approved Secret delivery reference and
// the publisher slot. The Fabric-confirmed binding identity is never part of it;
// Serve obtains that from Fabric alone.
func launchManagedKeyHandover(workspaceID, keyBindingID string) *api.RuntimeManagedKeyBinding {
	return &api.RuntimeManagedKeyBinding{
		KeyBindingId: keyBindingID, Fingerprint: "sha256:" + strings.Repeat("ab", 32),
		SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(workspaceID),
		TargetSlot:              "gateway",
	}
}

// confirmedReloadBindingReadback is the opaque handover a Workspace presents for
// one model-configuration version: the same four Gateway input fields. Serve
// performs the replacement itself.
func confirmedReloadBindingReadback(workspaceID, keyBindingID string) *api.RuntimeManagedKeyBinding {
	return &api.RuntimeManagedKeyBinding{
		KeyBindingId: keyBindingID, Fingerprint: "sha256:" + strings.Repeat("cd", 32),
		SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(workspaceID),
		TargetSlot:              "gateway",
	}
}

func TestServeDeployPerformsTheFabricSecretHandover(t *testing.T) {
	s, r, out, gateway, resources := managedKeyFixture(t)
	ctx := workspaceContext()
	// Each admitted command is a fresh message in production, so the test builds one
	// per call and never replays a command object Serve already completed.
	launchCommand := func() *api.RuntimeDeployCommand {
		c := deployReserved(r, out)
		c.ModelSelections = []*api.ModelSelection{{Slot: "default", ModelId: "model-alpha"}}
		c.ManagedKeyBinding = launchManagedKeyHandover(r.WorkspaceId, "key-1")
		return c
	}
	readback, err := s.Deploy(ctx, launchCommand())
	if err != nil || readback.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
		t.Fatalf("deploy=%v %v", readback, err)
	}
	// Serve alone requested the Fabric Secret binding. It minted no Gateway key.
	if gateway.calls != 0 {
		t.Fatalf("Serve minted %d Gateway keys, want none", gateway.calls)
	}
	if resources.binds != 1 || resources.rebinds != 0 {
		t.Fatalf("binds=%d rebinds=%d, want one initial bind", resources.binds, resources.rebinds)
	}
	// The exact handover input reached Fabric: this Workspace's delivery, the
	// Gateway key binding, the approved Secret reference and the publisher slot.
	last := resources.lastCommand
	if last.GetWorkspaceId() != r.WorkspaceId || last.GetRuntimeInstanceId() != out.RuntimeInstanceId ||
		last.GetKeyBindingId() != "key-1" || last.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef(r.WorkspaceId) ||
		last.GetTargetSlot() != "gateway" || last.GetContext().GetIdempotencyKey() == "" {
		t.Fatalf("handover command=%+v", last)
	}
	// The confirmed binding the provider returned is what Serve recorded with the
	// frozen start command: the caller's unconfirmed input is never persisted as a
	// confirmed fact.
	var snapshot []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE action='start' AND expected_deployment_id=$1`, out.DeploymentId).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var frozen api.RuntimeDeployCommand
	if err := protojson.Unmarshal(snapshot, &frozen); err != nil {
		t.Fatal(err)
	}
	binding := frozen.GetManagedKeyBinding()
	if binding.GetSecretBindingId() != resources.active || binding.GetSecretVersion() == "" {
		t.Fatalf("frozen binding=%+v active=%q", binding, resources.active)
	}
	// The readiness event reports injection only because a confirmed binding exists.
	var verified bool
	if err := s.DB.QueryRowContext(ctx, `SELECT (payload->>'credentialInjectionVerified')::boolean FROM serve.outbox_events WHERE event_type='serve.agent_readiness_observed.v1' AND aggregate_id=$1`, out.DeploymentId).Scan(&verified); err != nil {
		t.Fatal(err)
	}
	if !verified {
		t.Fatal("readiness event did not report verified credential injection")
	}
	// A lost response replays the same command. Fabric replays its own readback, so
	// the binding identity is unchanged and no second binding is recorded.
	firstID := binding.GetSecretBindingId()
	if _, err = s.Deploy(ctx, launchCommand()); err != nil {
		t.Fatal(err)
	}
	if resources.binds != 2 {
		t.Fatalf("replay binds=%d, want the idempotent replay request", resources.binds)
	}
	if resources.active != firstID {
		t.Fatalf("replay changed the active binding %q -> %q", firstID, resources.active)
	}
	if gateway.calls != 0 {
		t.Fatalf("replay minted %d Gateway keys", gateway.calls)
	}
}

func TestServeDeployRefusesAnUnconfirmedBindingFromTheCaller(t *testing.T) {
	s, r, out, _, resources := managedKeyFixture(t)
	// A caller may not present a Fabric-confirmed identity: Serve is the only owner
	// that may obtain it, so the retired Workspace-issued form is refused.
	confirmed := deployReserved(r, out)
	confirmed.ModelSelections = []*api.ModelSelection{{Slot: "default", ModelId: "model-alpha"}}
	confirmed.ManagedKeyBinding = &api.RuntimeManagedKeyBinding{KeyBindingId: "key-1", Fingerprint: "sha256:" + strings.Repeat("ab", 32), SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(r.WorkspaceId), TargetSlot: "gateway", SecretBindingId: "sbx_elsewhere", SecretVersion: "v-elsewhere"}
	if _, err := s.Deploy(workspaceContext(), confirmed); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("caller-confirmed binding err=%v want failed precondition", err)
	}
	if resources.binds != 0 || resources.rebinds != 0 {
		t.Fatalf("a refused handover still bound: binds=%d rebinds=%d", resources.binds, resources.rebinds)
	}
}

func TestServeDeployRefusesAMissingOrIncompleteHandover(t *testing.T) {
	s, r, out, gateway, resources := managedKeyFixture(t)
	command := deployReserved(r, out)
	command.ModelSelections = []*api.ModelSelection{{Slot: "default", ModelId: "model-alpha"}}
	if _, err := s.Deploy(workspaceContext(), command); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("absent handover err=%v want failed precondition", err)
	}
	incomplete := deployReserved(r, out)
	incomplete.ModelSelections = command.GetModelSelections()
	incomplete.ManagedKeyBinding = &api.RuntimeManagedKeyBinding{KeyBindingId: "key-1", Fingerprint: "sha256:" + strings.Repeat("ab", 32), SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(r.WorkspaceId), TargetSlot: "gateway", SecretVersion: "uninvited"}
	if _, err := s.Deploy(workspaceContext(), incomplete); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("caller version err=%v want failed precondition", err)
	}
	if gateway.calls != 0 || resources.binds != 0 || resources.rebinds != 0 {
		t.Fatalf("a refused launch bound: gateway=%d binds=%d rebinds=%d", gateway.calls, resources.binds, resources.rebinds)
	}
	var status string
	if err := s.DB.QueryRowContext(context.Background(), `SELECT status FROM serve.agent_deployments WHERE id=$1`, out.DeploymentId).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status == "active" {
		t.Fatal("a refused managed-key handover activated the deployment")
	}
}

// TestServeDeployRecoversAfterExecutionLostACK reuses the exact original
// identity after a lost response: the first Deploy already had Fabric confirm the
// Secret before the execution step failed, and the replay converges on that same
// binding and that same deployment instead of binding a second Secret or starting
// a second delivery.
func TestServeDeployRecoversAfterExecutionLostACK(t *testing.T) {
	s, r, out, _, resources := managedKeyFixture(t)
	ctx := workspaceContext()
	runtime := &runtimeForServe{observeErr: true}
	s.Runtime = runtime
	command := func() *api.RuntimeDeployCommand {
		c := deployReserved(r, out)
		c.ModelSelections = []*api.ModelSelection{{Slot: "default", ModelId: "model-alpha"}}
		c.ManagedKeyBinding = launchManagedKeyHandover(r.WorkspaceId, "key-1")
		return c
	}
	if _, err := s.Deploy(ctx, command()); err == nil {
		t.Fatal("the first Deploy must report the execution readback failure")
	}
	// The Secret was confirmed once, and the frozen start command recorded it even
	// though the execution step never completed.
	if resources.binds != 1 || resources.active == "" {
		t.Fatalf("first attempt binds=%d active=%q", resources.binds, resources.active)
	}
	// The frozen start command recorded the Fabric-confirmed binding even though the
	// execution step failed, so recovery has one original identity to reuse.
	var snapshot []byte
	if err := s.DB.QueryRowContext(ctx, `SELECT input_snapshot FROM serve.agent_runtime_actions WHERE action='start' AND expected_deployment_id=$1`, out.DeploymentId).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	var frozen api.RuntimeDeployCommand
	if err := protojson.Unmarshal(snapshot, &frozen); err != nil {
		t.Fatal(err)
	}
	confirmed := frozen.GetManagedKeyBinding().GetSecretBindingId()
	if confirmed == "" {
		t.Fatal("the lost-ACK delivery did not record the confirmed launch binding")
	}
	// The replay binds the same original identity; Fabric replays its readback, so
	// no second binding is created.
	runtime.observeErr = false
	readback, err := s.Deploy(ctx, command())
	if err != nil || readback.GetState() != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY {
		t.Fatalf("replay=%v %v", readback, err)
	}
	if resources.binds != 2 || resources.active != confirmed {
		t.Fatalf("replay binds=%d active=%q, want the same binding %q", resources.binds, resources.active, confirmed)
	}
	var starts int
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM serve.agent_runtime_actions WHERE action='start' AND expected_deployment_id=$1`, out.DeploymentId).Scan(&starts); err != nil {
		t.Fatal(err)
	}
	if starts != 1 {
		t.Fatalf("start actions=%d, want one original delivery", starts)
	}
}

// TestServeReloadRefusesAStalePredecessor proves Serve preserves the replacement
// CAS: when the predecessor Serve recorded is no longer the one active binding,
// Fabric refuses, Serve reports it, and neither the applied version nor the active
// binding moves. Serve never silently retries as a second initial bind.
func TestServeReloadRefusesAStalePredecessor(t *testing.T) {
	s, r, reservation, _, resources := managedKeyFixture(t)
	ctx := workspaceContext()
	runtime := &runtimeForServe{reloadVersion: 7}
	s.Runtime = runtime
	deploy := deployReserved(r, reservation)
	deploy.ModelSelections = []*api.ModelSelection{{Slot: "chat", ModelId: "model-original"}}
	deploy.ManagedKeyBinding = launchManagedKeyHandover(r.GetWorkspaceId(), "key-original")
	if _, err := s.Deploy(ctx, deploy); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	active := resources.active
	// Another writer replaced the runtime's active binding, so the predecessor the
	// first writer recorded is stale. Fabric must refuse the reintroduced
	// replacement rather than retire the newer binding.
	resources.active = "sbx_replaced_by_another_writer"
	reloadCall := call(r.GetContext().GetScope().GetTenant().GetTenantId(), false)
	reloadCall.IdempotencyKey = "model-update-stale"
	reloadCall.RequestId = "model-update-stale"
	_, err := s.ReloadModels(ctx, &api.RuntimeReloadCommand{Context: reloadCall, RuntimeInstanceId: reservation.RuntimeInstanceId, ExpectedAppliedVersion: 0, TargetVersion: 7, Selections: []*api.ModelSelection{{Slot: "chat", ModelId: "model-7"}}, ManagedKeyBinding: confirmedReloadBindingReadback(r.GetWorkspaceId(), "key-7")})
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale predecessor err=%v want a failed precondition", err)
	}
	if len(runtime.reloads) != 0 {
		t.Fatalf("a refused replacement reached the execution boundary: %v", runtime.reloads)
	}
	if resources.active != "sbx_replaced_by_another_writer" {
		t.Fatalf("a refused replacement changed the active binding to %q", resources.active)
	}
	if applied := appliedModelConfiguration(t, s, reservation.RuntimeInstanceId); applied != 0 {
		t.Fatalf("a refused replacement advanced the applied configuration to %d", applied)
	}
	_ = active
}
