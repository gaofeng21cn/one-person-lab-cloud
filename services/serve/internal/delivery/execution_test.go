package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/serve/internal/tkeapply"
)

const testArtifactDigest = "sha256:1111111111111111111111111111111111111111111111111111111111111111"

// fakeExecutor stands in for a provider executor and records the exact typed input
// Serve's own execution produced. It is a seam for the adapter's mapping and
// refusal rules; the executor's own cluster behaviour is proven in its own tests.
type fakeExecutor struct {
	input       contracts.WorkspaceApplicationRuntimeInput
	placement   tkeapply.Placement
	observation contracts.WorkspaceApplicationRuntimeObservation
	ensureErr   error
	lifecycle   contracts.WorkspaceApplicationRuntimeLifecycleResult
	lifecycleEr error
	credentials contracts.WorkspaceApplicationRuntimeCredentials
	calls       int
	desired     string
}

func (f *fakeExecutor) Configured() error { return nil }

func (f *fakeExecutor) EnsureWorkspaceApplicationRuntime(_ context.Context, input contracts.WorkspaceApplicationRuntimeInput, placement tkeapply.Placement) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	f.calls++
	f.input, f.placement = input, placement
	return f.observation, f.ensureErr
}

func (f *fakeExecutor) ReadWorkspaceApplicationRuntime(_ context.Context, input contracts.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeObservation, error) {
	f.calls++
	f.input = input
	return f.observation, nil
}

func (f *fakeExecutor) SetWorkspaceApplicationRuntimeLifecycle(_ context.Context, input contracts.WorkspaceApplicationRuntimeInput, desired string) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	f.calls++
	f.input, f.desired = input, desired
	return f.lifecycle, f.lifecycleEr
}

func (f *fakeExecutor) ReadWorkspaceApplicationRuntimeLifecycle(_ context.Context, input contracts.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeLifecycleResult, error) {
	f.calls++
	f.input = input
	return f.lifecycle, f.lifecycleEr
}

func (f *fakeExecutor) ReadWorkspaceApplicationRuntimeCredentials(_ context.Context, input contracts.WorkspaceApplicationRuntimeInput) (contracts.WorkspaceApplicationRuntimeCredentials, error) {
	f.calls++
	f.input = input
	return f.credentials, nil
}

// executionCommand builds the typed command Serve's own Deploy path receives.
func executionCommand(t *testing.T) *api.RuntimeDeployCommand {
	t.Helper()
	descriptor := &api.DeploymentDescriptor{
		SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1,
		Artifact:      &api.ArtifactReference{Repository: "registry.test/app", Digest: testArtifactDigest},
		Provenance:    api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD,
		ApplicationRevision: &api.WorkspaceApplicationRevision{
			SchemaVersion: 1, ApplicationId: "knowledge-app", Version: "1", Platform: "linux/amd64",
			Image:          "registry.test/app@" + testArtifactDigest,
			EntryPort:      proto.String("http"),
			Ports:          []*api.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: api.WorkspaceApplicationPortProtocolEnum_WORKSPACE_APPLICATION_PORT_PROTOCOL_ENUM_TCP}},
			HealthChecks:   []*api.WorkspaceApplicationHealthCheck{{Path: "/healthz", Port: 8080}},
			ExposurePolicy: api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION,
		},
	}
	raw, err := publicjson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	return &api.RuntimeDeployCommand{
		WorkspaceId: "workspace-execution", DeploymentId: "deployment-execution", RuntimeInstanceId: "rt_deployment-execution",
		ResourceSetId: "rs_deployment-execution", DataAttachmentId: "att_deployment-execution",
		DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: "sha256:" + hex.EncodeToString(sum[:]),
		DeploymentDescriptorObjectRef: "serve-descriptor://deployment-execution", ExecutionEpoch: 1,
	}
}

func executionTargetFixture(command *api.RuntimeDeployCommand) ExecutionTarget {
	return ExecutionTarget{
		Binding: &api.ResourceExecutionBinding{AccountId: "original-account", ComputeAllocationId: "original-compute", StorageVolumeId: "original-volume", DataAttachmentId: command.DataAttachmentId, DataAttachmentOperationId: "original-attachment-operation"},
		Placement: &api.ApplicationExecutionPlacement{
			ComputeNodeName: "node-1", ComputePackageId: "basic", ComputeNodePoolId: "np-basic",
			ComputeMachineName: "machine-1", ComputeInstanceId: "ins-1", StoragePvcName: "pvc-1",
		},
	}
}

func readyObservation(t *testing.T, command *api.RuntimeDeployCommand, entry *contracts.WorkspaceApplicationEntry) contracts.WorkspaceApplicationRuntimeObservation {
	t.Helper()
	raw, err := publicjson.Marshal(command.GetDeploymentDescriptor().GetApplicationRevision())
	if err != nil {
		t.Fatal(err)
	}
	var revision contracts.WorkspaceApplicationRevision
	if json.Unmarshal(raw, &revision) != nil {
		t.Fatal("fixture revision is not decodable")
	}
	components := contracts.WorkspaceApplicationRuntimeComponents(revision)
	for i := range components {
		components[i].State = "ready"
	}
	return contracts.WorkspaceApplicationRuntimeObservation{
		SchemaVersion: 1, WorkspaceID: command.GetWorkspaceId(),
		RuntimeID: contracts.WorkspaceApplicationRuntimeID(command.GetRuntimeInstanceId()),
		Status:    "ready", Entry: entry, Components: components,
	}
}

func executionAdapter(fake *fakeExecutor, origin RouteOrigin) *agentExecutionAdapter {
	return &agentExecutionAdapter{Executor: fake, Origin: origin}
}

// TestAgentExecutionResolvesTheGatewayEntryToTheDeclaredOrigin is the decisive
// behaviour of Serve's own execution: a provider reports one in-cluster Service and
// port, and Serve publishes the installation's declared origin for it instead of
// treating the Service name as a URL.
func TestAgentExecutionResolvesTheGatewayEntryToTheDeclaredOrigin(t *testing.T) {
	origin := RouteOrigin{Scheme: "https", WorkspaceDomain: "workspaces.example", ApplicationDomain: "apps.example"}
	command := executionCommand(t)
	entry := &contracts.WorkspaceApplicationEntry{ServiceName: "app-runtime-http-main", Port: 8080}
	fake := &fakeExecutor{observation: readyObservation(t, command, entry)}
	adapter := executionAdapter(fake, origin)

	ack, err := adapter.Start(context.Background(), command, executionTargetFixture(command))
	if err != nil || ack.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING || ack.ReadinessEvidenceRef != "" {
		t.Fatalf("start=%+v err=%v", ack, err)
	}
	observed, err := adapter.Observe(context.Background(), command, executionTargetFixture(command))
	if err != nil {
		t.Fatal(err)
	}
	host, ok := origin.ApplicationEntryHost(command.GetWorkspaceId(), command.GetDeploymentDescriptor().GetApplicationRevision().GetApplicationId())
	if !ok {
		t.Fatal("fixture origin must compose a hostname")
	}
	if observed.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY || observed.AccessURL != "https://"+host+"/" {
		t.Fatalf("observed=%+v want ready at https://%s/", observed, host)
	}
	if observed.AccessUpstreamService != "app-runtime-http-main" || observed.AccessUpstreamPort != 8080 {
		t.Fatalf("observed upstream=%q:%d", observed.AccessUpstreamService, observed.AccessUpstreamPort)
	}
	if !strings.HasPrefix(observed.ReadinessEvidenceRef, "tke-application-readback:"+command.GetRuntimeInstanceId()+":https://"+host+"/:sha256:") {
		t.Fatalf("readiness evidence=%q", observed.ReadinessEvidenceRef)
	}
	// The executor received the confirmed placement, not a caller claim.
	for name, got := range map[string]string{"node": fake.placement.ComputeNodeName, "package": fake.placement.ComputePackageID, "claim": fake.placement.StoragePVCName, "workspace": fake.placement.WorkspaceID} {
		want := map[string]string{"node": "node-1", "package": "basic", "claim": "pvc-1", "workspace": command.GetWorkspaceId()}[name]
		if got != want {
			t.Fatalf("executor placement %s=%q want %q", name, got, want)
		}
	}
}

// TestAgentExecutionRefusesToGuessAnOrigin proves the other half: with no declared
// origin Serve never publishes the provider's Service name as the customer address.
func TestAgentExecutionRefusesToGuessAnOrigin(t *testing.T) {
	command := executionCommand(t)
	fake := &fakeExecutor{observation: readyObservation(t, command, &contracts.WorkspaceApplicationEntry{ServiceName: "app-runtime-http-main", Port: 8080})}
	observed, err := executionAdapter(fake, RouteOrigin{}).Observe(context.Background(), command, executionTargetFixture(command))
	if err != nil {
		t.Fatal(err)
	}
	if observed.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING || observed.AccessURL != "" || observed.ReadinessEvidenceRef != "" {
		t.Fatalf("observed=%+v want a not-open runtime", observed)
	}
}

// TestAgentExecutionRequiresWholeRuntimeReadiness proves a Pod that is merely
// running, or one unready component, is never recorded ready.
func TestAgentExecutionRequiresWholeRuntimeReadiness(t *testing.T) {
	command := executionCommand(t)
	origin := RouteOrigin{Scheme: "https", ApplicationDomain: "apps.example"}
	observation := readyObservation(t, command, &contracts.WorkspaceApplicationEntry{ServiceName: "app-runtime-http-main", Port: 8080})
	observation.Components[0].State = "pending"
	observation.Status, observation.Entry = "pending", nil
	fake := &fakeExecutor{observation: observation}
	if observed, err := executionAdapter(fake, origin).Observe(context.Background(), command, executionTargetFixture(command)); err != nil || observed.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING {
		t.Fatalf("pending component observed=%+v err=%v", observed, err)
	}
	observation.Components[0].State = "failed"
	observation.Status, observation.Entry = "failed", nil
	fake.observation = observation
	if observed, err := executionAdapter(fake, origin).Observe(context.Background(), command, executionTargetFixture(command)); err != nil || observed.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED {
		t.Fatalf("failed runtime observed=%+v err=%v", observed, err)
	}
}

// TestAgentExecutionCarriesModelConfigurationAndInjectableSecrets proves the
// adapter does not silently drop the requested model configuration and injects a
// declared Secret only from a confirmed owner binding.
func TestAgentExecutionCarriesModelConfigurationAndInjectableSecrets(t *testing.T) {
	command := executionCommand(t)
	command.ModelSelections = []*api.ModelSelection{{Slot: "default", ModelId: "model-alpha"}}
	command.ModelConfigurationVersion = 2
	fake := &fakeExecutor{observation: readyObservation(t, command, nil)}
	if _, err := executionAdapter(fake, RouteOrigin{}).Start(context.Background(), command, executionTargetFixture(command)); err != nil {
		t.Fatalf("model configuration rejected: %v", err)
	}
	if fake.input.Revision.ApplicationID != "knowledge-app" || fake.input.RuntimeOperationID != command.GetRuntimeInstanceId() {
		t.Fatalf("executor input=%+v", fake.input)
	}

	declared := proto.Clone(command).(*api.RuntimeDeployCommand)
	declared.DeploymentDescriptor.ApplicationRevision.Credentials = []*api.WorkspaceApplicationCredential{{Name: "gateway", Kind: api.WorkspaceApplicationCredentialKindEnum_WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_GATEWAY_KEY, Target: "/run/secrets/opl_gateway_api_key"}}
	const version = "0123456789abcdef"
	declared.ManagedKeyBinding = &api.RuntimeManagedKeyBinding{KeyBindingId: "key-1", SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(declared.GetWorkspaceId()), Fingerprint: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef", TargetSlot: "gateway", SecretBindingId: "sbx_1", SecretVersion: version}
	fake = &fakeExecutor{observation: readyObservation(t, declared, nil)}
	if _, err := executionAdapter(fake, RouteOrigin{}).Start(context.Background(), declared, executionTargetFixture(declared)); err != nil {
		t.Fatalf("declared secret rejected: %v", err)
	}
	if len(fake.input.SecretBindings) != 1 || fake.input.SecretBindings[0].Name != "gateway" || fake.input.SecretBindings[0].SecretRef != contracts.WorkspaceGatewaySecretRef(declared.GetWorkspaceId()) || fake.input.SecretBindings[0].Version != version || fake.input.Configuration.CredentialVersion != version {
		t.Fatalf("declared secret not injected: %+v", fake.input)
	}

	unbound := proto.Clone(declared).(*api.RuntimeDeployCommand)
	unbound.ManagedKeyBinding = nil
	fake = &fakeExecutor{}
	if _, err := executionAdapter(fake, RouteOrigin{}).Start(context.Background(), unbound, executionTargetFixture(unbound)); status.Code(err) != codes.FailedPrecondition || fake.calls != 0 {
		t.Fatalf("unbound declared secret err=%v calls=%d", err, fake.calls)
	}
}

// TestAgentExecutionCarriesOnlyThePublishedPlacement proves the target Serve runs a
// delivery against carries exactly the placement the resources owner published: the
// published one verbatim, and none when that owner published none. Serve composes no
// placement of its own, and the executor that schedules the workload owns the
// refusal, so a provider whose resources are not scheduled onto a node, prepaid
// package and storage claim keeps its own execution boundary.
func TestAgentExecutionCarriesOnlyThePublishedPlacement(t *testing.T) {
	command := executionCommand(t)
	target := executionTargetFixture(command)
	resources := &api.ResourceReadback{ResourceSetId: command.GetResourceSetId(), WorkspaceId: command.GetWorkspaceId(), Outcome: api.Observation_OBSERVATION_CONFIRMED, ExecutionResources: target.Binding, ApplicationPlacement: target.Placement}
	confirmed, err := confirmedExecutionTarget(command, resources)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(confirmed.Placement, target.Placement) {
		t.Fatalf("target placement=%v want the published placement %v", confirmed.Placement, target.Placement)
	}
	resources.ApplicationPlacement = nil
	confirmed, err = confirmedExecutionTarget(command, resources)
	if err != nil {
		t.Fatal(err)
	}
	if confirmed.Placement != nil {
		t.Fatalf("Serve composed a placement the resources owner did not publish: %v", confirmed.Placement)
	}
	if !proto.Equal(confirmed.Binding, target.Binding) {
		t.Fatalf("target binding=%v want the confirmed binding", confirmed.Binding)
	}
}

// TestAgentExecutionReachesTheBoundaryOfAProviderWithoutAPlacement proves the
// delivery Serve runs for a provider that publishes no node, prepaid package and
// storage claim still reaches that provider's own execution boundary. That
// boundary is the one writer for such a provider, so Serve must not refuse the
// delivery for a placement that provider never publishes.
func TestAgentExecutionReachesTheBoundaryOfAProviderWithoutAPlacement(t *testing.T) {
	const token, key = "serve-execution-test-token-distinct-0003", "serve-execution-signature-0000000004"
	command := executionCommand(t)
	called := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case called <- struct{}{}:
		default:
		}
		_ = json.NewEncoder(w).Encode(readyObservation(t, command, nil))
	}))
	defer server.Close()
	target := executionTargetFixture(command)
	resources := &api.ResourceReadback{ResourceSetId: command.GetResourceSetId(), WorkspaceId: command.GetWorkspaceId(), Outcome: api.Observation_OBSERVATION_CONFIRMED, ExecutionResources: target.Binding}
	confirmed, err := confirmedExecutionTarget(command, resources)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &agentExecutionAdapter{Executor: &fabricApplicationBridge{BaseURL: server.URL, Token: token, CapabilityKey: key, Client: server.Client()}, Origin: RouteOrigin{}}
	if _, err := adapter.Start(context.Background(), command, confirmed); err != nil {
		t.Fatalf("a provider without a published placement was refused: %v", err)
	}
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("the provider's own execution boundary was not reached")
	}
}

// TestAgentExecutionRefusesALifecycleDrift proves an applied lifecycle is only
// reported when the executor observed that state.
func TestAgentExecutionRefusesALifecycleDrift(t *testing.T) {
	command := executionCommand(t)
	target := executionTargetFixture(command)
	fake := &fakeExecutor{lifecycle: contracts.WorkspaceApplicationRuntimeLifecycleResult{RuntimeID: contracts.WorkspaceApplicationRuntimeID(command.GetRuntimeInstanceId()), WorkspaceID: command.GetWorkspaceId(), State: "pending"}}
	adapter := executionAdapter(fake, RouteOrigin{})
	if err := adapter.Lifecycle(context.Background(), command, target, "suspended"); err == nil {
		t.Fatal("a lifecycle the executor did not apply was reported applied")
	}
	if fake.desired != "suspended" || fake.input.RuntimeOperationID != command.GetRuntimeInstanceId() {
		t.Fatalf("executor lifecycle input=%+v desired=%q", fake.input, fake.desired)
	}
	fake.lifecycle.State = "absent"
	if err := adapter.Lifecycle(context.Background(), command, target, "suspended"); err != nil {
		t.Fatalf("an entirely retired runtime is a stopped writer: %v", err)
	}
	fake.lifecycle.State = "running"
	if err := adapter.Lifecycle(context.Background(), command, target, "running"); err != nil {
		t.Fatal(err)
	}
}

// TestAgentExecutionKeepsAnUnpublishedApplicationNotOpen proves a ready workload
// that publishes no web entry is never recorded as an open application and never
// crashes the adapter into inventing an address.
func TestAgentExecutionKeepsAnUnpublishedApplicationNotOpen(t *testing.T) {
	command := executionCommand(t)
	fake := &fakeExecutor{observation: readyObservation(t, command, nil)}
	observed, err := executionAdapter(fake, RouteOrigin{Scheme: "https", ApplicationDomain: "apps.example"}).Observe(context.Background(), command, executionTargetFixture(command))
	if err != nil {
		t.Fatal(err)
	}
	if observed.State != api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_STARTING || observed.AccessURL != "" || observed.ReadinessEvidenceRef != "" {
		t.Fatalf("observed=%+v want a not-open runtime", observed)
	}
}

// TestAgentExecutionRefusesAForeignObservation proves an observation of another
// runtime is never recorded as this delivery's readiness.
func TestAgentExecutionRefusesAForeignObservation(t *testing.T) {
	command := executionCommand(t)
	observation := readyObservation(t, command, nil)
	observation.RuntimeID = "other-runtime"
	fake := &fakeExecutor{observation: observation}
	if _, err := executionAdapter(fake, RouteOrigin{}).Observe(context.Background(), command, executionTargetFixture(command)); err == nil {
		t.Fatal("foreign runtime accepted")
	}
}

// TestAgentExecutionRefusesAnUnconfiguredExecutor proves an installation that
// declares no execution boundary has no anonymous one.
func TestAgentExecutionRefusesAnUnconfiguredExecutor(t *testing.T) {
	command := executionCommand(t)
	adapter := &agentExecutionAdapter{Origin: RouteOrigin{}}
	if _, err := adapter.Start(context.Background(), command, executionTargetFixture(command)); status.Code(err) != codes.Unavailable {
		t.Fatalf("err=%v want unavailable", err)
	}
}

// TestServeBindsExactlyOneExecutionBoundary proves an installation cannot declare
// its own cluster facts and the migration-source boundary at once: that would be two
// writers for one workload.
func TestServeBindsExactlyOneExecutionBoundary(t *testing.T) {
	service := &Service{}
	t.Setenv("OPL_K8S_NAMESPACE", "opl-cloud")
	t.Setenv("OPL_FABRIC_APPLICATION_URL", "https://fabric.example")
	if err := configureAgentExecution(service, RouteOrigin{}); err == nil {
		t.Fatal("two execution boundaries were accepted")
	}
	t.Setenv("OPL_FABRIC_APPLICATION_URL", "")
	if err := configureAgentExecution(service, RouteOrigin{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.Runtime.(*agentExecutionAdapter); !ok {
		t.Fatalf("runtime=%T want Serve's own execution adapter", service.Runtime)
	}
	if _, ok := service.Runtime.(*agentExecutionAdapter).Executor.(*tkeapply.Executor); !ok {
		t.Fatalf("executor=%T want the in-process TKE executor", service.Runtime.(*agentExecutionAdapter).Executor)
	}
	t.Setenv("OPL_K8S_NAMESPACE", "")
	t.Setenv("OPL_FABRIC_APPLICATION_URL", "https://fabric.example")
	service = &Service{}
	if err := configureAgentExecution(service, RouteOrigin{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := service.Runtime.(*agentExecutionAdapter).Executor.(*fabricApplicationBridge); !ok {
		t.Fatalf("executor=%T want the declared migration-source boundary", service.Runtime.(*agentExecutionAdapter).Executor)
	}
}

// TestInstallationIngressPeersParseTheDeclaredLabelObject proves the installation's
// declared ingress peer is either a usable label set or a refusal.
func TestInstallationIngressPeersParseTheDeclaredLabelObject(t *testing.T) {
	peers, err := applicationIngressPeers(`{"app.kubernetes.io/name":"opl-cloud","app.kubernetes.io/component":"serve"}`)
	if err != nil || len(peers) != 1 || peers[0]["app.kubernetes.io/component"] != "serve" {
		t.Fatalf("peers=%v err=%v", peers, err)
	}
	for _, invalid := range []string{`[]`, `{}`, `{"":"v"}`, `{"app":" "}`, `not json`} {
		if _, err := applicationIngressPeers(invalid); err == nil {
			t.Fatalf("declaration %q accepted", invalid)
		}
	}
	if peers, err := applicationIngressPeers(""); err != nil || peers != nil {
		t.Fatalf("an installation that declares no peer must stay absent: %v %v", peers, err)
	}
}

// TestFabricApplicationBridgeCarriesTheScopedCapability proves the remaining
// migration-source boundary still signs the exact typed input it sends, so the
// provider that has not moved yet keeps working unchanged.
func TestFabricApplicationBridgeCarriesTheScopedCapability(t *testing.T) {
	const token, key = "serve-execution-test-token-distinct-0001", "serve-execution-signature-0000000002"
	command := executionCommand(t)
	received := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.Header.Get("Authorization") != "Bearer "+token {
			t.Error("transport token missing")
		}
		parts := strings.Split(r.Header.Get("X-OPL-Fabric-Capability"), ".")
		if len(parts) != 2 {
			t.Fatal("missing scoped capability")
		}
		var claims map[string]any
		decoded, err := base64.RawURLEncoding.DecodeString(parts[0])
		if err != nil || json.Unmarshal(decoded, &claims) != nil {
			t.Fatal("capability claims are not decodable")
		}
		sum := sha256.Sum256(body)
		if claims["caller"] != "serve" || claims["workspaceId"] != command.GetWorkspaceId() || claims["bodySha256"] != hex.EncodeToString(sum[:]) {
			t.Errorf("capability identity drift=%v", claims)
		}
		var input contracts.WorkspaceApplicationRuntimeInput
		if json.Unmarshal(body, &input) != nil {
			t.Error("typed input is not decodable")
		}
		received <- claims
		json.NewEncoder(w).Encode(readyObservation(t, command, nil))
	}))
	defer server.Close()
	bridge := &fabricApplicationBridge{BaseURL: server.URL, Token: token, CapabilityKey: key, Client: server.Client()}
	observation, err := bridge.EnsureWorkspaceApplicationRuntime(context.Background(), bridgeInput(t, command), tkeapply.Placement{})
	if err != nil {
		t.Fatal(err)
	}
	if observation.Status != "ready" {
		t.Fatalf("observation=%+v", observation)
	}
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("the boundary was not called")
	}
}

// bridgeInput restates the typed input the bridge posts; it is produced by the
// adapter, so this test uses the adapter's own conversion.
func bridgeInput(t *testing.T, command *api.RuntimeDeployCommand) contracts.WorkspaceApplicationRuntimeInput {
	t.Helper()
	adapter := &agentExecutionAdapter{Executor: &fakeExecutor{}, Origin: RouteOrigin{}}
	input, err := adapter.runtimeInput(command, executionTargetFixture(command))
	if err != nil {
		t.Fatal(err)
	}
	return input
}
