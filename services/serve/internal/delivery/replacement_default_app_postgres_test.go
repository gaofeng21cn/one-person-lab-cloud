package delivery_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/services/serve/internal/delivery"
)

type upgradeRuntimeCatalog struct {
	api.RuntimeControlProductServiceClient
	versions map[string]*api.RuntimeVersion
	calls    int
}

func (c *upgradeRuntimeCatalog) ListRuntimeVersions(_ context.Context, r *api.ListRuntimeVersionsRpcRequest, _ ...grpc.CallOption) (*api.RuntimeVersionPage, error) {
	c.calls++
	if r.GetQueryCursor() == "" {
		return &api.RuntimeVersionPage{Items: []*api.RuntimeVersion{proto.Clone(c.versions["rv-original"]).(*api.RuntimeVersion)}, NextCursor: proto.String("second")}, nil
	}
	return &api.RuntimeVersionPage{Items: []*api.RuntimeVersion{proto.Clone(c.versions["rv-next"]).(*api.RuntimeVersion)}}, nil
}

type upgradeRuntime struct {
	runtimeForServe
	commands    []*api.RuntimeDeployCommand
	credentials []string
	events      []string
	stopFailure bool
}

func (r *upgradeRuntime) Start(ctx context.Context, c *api.RuntimeDeployCommand, target delivery.ExecutionTarget) (delivery.RuntimeObservation, error) {
	r.commands = append(r.commands, proto.Clone(c).(*api.RuntimeDeployCommand))
	r.credentials = append(r.credentials, target.CredentialVersion)
	r.events = append(r.events, "start")
	return r.runtimeForServe.Start(ctx, c, target)
}
func (r *upgradeRuntime) Lifecycle(ctx context.Context, c *api.RuntimeDeployCommand, target delivery.ExecutionTarget, desired string) error {
	r.events = append(r.events, desired)
	if desired == "suspended" && r.stopFailure {
		return errors.New("stop readback unknown")
	}
	return r.runtimeForServe.Lifecycle(ctx, c, target, desired)
}

func nativeUpgradeRelease(t *testing.T, id, version, hex string) *api.RuntimeVersion {
	t.Helper()
	contract := publisherContractExample(t)
	contract.Image = &api.ArtifactReference{Repository: "registry.test/opl-app", Digest: "sha256:" + strings.Repeat(hex, 64), Platform: &api.ImagePlatform{Os: api.ImagePlatformOsEnum_IMAGE_PLATFORM_OS_ENUM_LINUX, Architecture: api.ImagePlatformArchitectureEnum_IMAGE_PLATFORM_ARCHITECTURE_ENUM_AMD64}}
	contract.BuildRecipe = nil
	contract.PackageFormatContracts = nil
	contract.PackageFormatVersions = nil
	contract.ModelConfiguration = nil
	contract.Data = &api.DataContract{SchemaVersion: "opl-app-data/v1", Upgrade: &api.DataUpgradeContract{Mode: api.DataUpgradeContractModeEnum_DATA_UPGRADE_CONTRACT_MODE_ENUM_COMPATIBLE, CompatibleFromSchemaVersions: []string{"opl-app-data/v1"}}, Rollback: &api.DataRollbackContract{Safe: true, CompatibleSchemaVersions: []string{"opl-app-data/v1"}}, MountPolicies: []*api.DataMountPolicy{{MountName: "data", ConcurrentWritersSupported: false}}}
	contract.ApplicationRevisionTemplate = &api.WorkspaceApplicationRevision{SchemaVersion: 1, ApplicationId: "opl-app", Version: version, Platform: "linux/amd64", Image: contract.Image.Repository + "@" + contract.Image.Digest,
		Ports: []*api.WorkspaceApplicationPort{{Name: "app", Port: 3000, Protocol: api.WorkspaceApplicationPortProtocolEnum_WORKSPACE_APPLICATION_PORT_PROTOCOL_ENUM_TCP}}, EntryPort: proto.String("app"),
		PersistentMounts: []*api.WorkspaceApplicationMount{{Name: "data", MountPath: "/data"}},
		Credentials: []*api.WorkspaceApplicationCredential{
			{Name: "gateway", Kind: api.WorkspaceApplicationCredentialKindEnum_WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_GATEWAY_KEY, Target: "/run/secrets/opl_gateway_api_key"},
			{Name: "admin-password", Kind: api.WorkspaceApplicationCredentialKindEnum_WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_WORKSPACE_ADMIN_PASSWORD, Target: "/run/secrets/opl_webui_password", Username: proto.String("opl")},
			{Name: "session-secret", Kind: api.WorkspaceApplicationCredentialKindEnum_WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_WORKSPACE_SESSION_SECRET, Target: "/run/secrets/webui_session_secret"}},
		ExposurePolicy: api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION}
	return &api.RuntimeVersion{Id: id, Name: "one-person-lab-app", VersionLabel: version, Status: api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED, PublisherContract: contract}
}

func nativeUpgradeFixture(t *testing.T) (*delivery.Service, *api.RuntimeReservation, *upgradeRuntimeCatalog, *upgradeRuntime, *secretResources, *gatewayForServe) {
	t.Helper()
	s, base, _ := reservationFixture(t)
	original, next := nativeUpgradeRelease(t, "rv-original", "1", "1"), nativeUpgradeRelease(t, "rv-next", "2", "2")
	next.PublisherContract.ApplicationRevisionTemplate.Entrypoint = []string{"/usr/bin/env", "OPL_WEBUI_DEPLOYMENT_MODE=cloud", "OPL_WEBUI_AUTH_MODE=password", "OPL_WEBUI_USERNAME=opl", "OPL_WEBUI_PASSWORD_FILE=/run/secrets/opl_webui_password", "OPL_WEBUI_SESSION_SECRET_FILE=/run/secrets/webui_session_secret", "/usr/local/bin/opl-webui-entrypoint"}
	catalog := &upgradeRuntimeCatalog{versions: map[string]*api.RuntimeVersion{"rv-original": original, "rv-next": next}}
	s.RuntimeReleases = catalog
	selection := &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP, RuntimeVersionId: proto.String(original.Id)}
	// Original native Launch omitted the publisher data snapshot from its descriptor.
	descriptor := &api.DeploymentDescriptor{SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Artifact: original.PublisherContract.Image, ApplicationRevision: original.PublisherContract.ApplicationRevisionTemplate, ApplicationSelection: selection, Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_RUNTIME_RELEASE}
	base.WorkspaceId = "ws_1042"
	base.CapabilityVersionId = ""
	base.ApplicationSelection = selection
	base.Artifact = descriptor.Artifact
	base.DeploymentDescriptor = descriptor
	base.DeploymentDescriptorDigest = descriptorDigestOf(t, descriptor)
	base.DeploymentDescriptorObjectRef = "runtime-release:rv-original@" + base.DeploymentDescriptorDigest
	base.Context.IdempotencyKey = "native-original"
	resources := &secretResources{resourcesForServe: &resourcesForServe{confirmed: true, workspace: base.WorkspaceId}}
	s.Resources = resources
	runtime := &upgradeRuntime{}
	s.Runtime = runtime
	gateway := &gatewayForServe{}
	s.Gateway = gateway
	reserved, err := s.Reserve(workspaceContext(), base)
	if err != nil {
		t.Fatal(err)
	}
	command := &api.RuntimeDeployCommand{Context: base.Context, WorkspaceId: base.WorkspaceId, DeploymentId: reserved.DeploymentId, RuntimeInstanceId: reserved.RuntimeInstanceId, ExecutionEpoch: reserved.ExecutionEpoch, ResourceSetId: base.ResourceSetId, DataAttachmentId: base.DataAttachmentId, DeploymentDescriptor: descriptor, DeploymentDescriptorDigest: base.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: base.DeploymentDescriptorObjectRef, ApplicationSelection: selection, ManagedKeyBinding: launchManagedKeyHandover(base.WorkspaceId, "key-original")}
	if _, err = s.Deploy(workspaceContext(), command); err != nil {
		t.Fatal(err)
	}
	runtime.events = nil
	return s, reserved, catalog, runtime, resources, gateway
}
func nativeUpgradeRequest(current, key string) *api.UpdateWorkspaceVersionRpcRequest {
	c := call("tenant-alpha", false)
	c.IdempotencyKey = key
	return &api.UpdateWorkspaceVersionRpcRequest{Context: c, WorkspaceId: "ws_1042", Body: &api.UpdateWorkspaceVersionRequest{ExpectedCurrentAgentDeploymentId: current, ApplicationSelection: &api.WorkspaceApplicationSelection{Kind: api.WorkspaceApplicationSelectionKindEnum_WORKSPACE_APPLICATION_SELECTION_KIND_ENUM_OPL_APP, RuntimeVersionId: proto.String("rv-next")}}}
}
func frozenUpgradeCommand(t *testing.T, s *delivery.Service, deployment string) (*api.RuntimeDeployCommand, string) {
	t.Helper()
	var raw []byte
	if err := s.DB.QueryRow(`SELECT input_snapshot FROM serve.agent_runtime_actions WHERE expected_deployment_id=$1 AND action='start'`, deployment).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var v struct {
		Command           json.RawMessage
		CredentialVersion string
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	command := &api.RuntimeDeployCommand{}
	if err := protojson.Unmarshal(v.Command, command); err != nil {
		t.Fatal(err)
	}
	return command, v.CredentialVersion
}
func TestNativeDefaultAppUpgradeRetainsOriginalResourcesAndCredentials(t *testing.T) {
	s, original, catalog, runtime, resources, gateway := nativeUpgradeFixture(t)
	first, credential := frozenUpgradeCommand(t, s, original.DeploymentId)
	// Deprecating the running source does not erase its exact historical schema.
	catalog.versions["rv-original"].Status = api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_DEPRECATED
	request := nativeUpgradeRequest(original.DeploymentId, "native-upgrade")
	result, err := s.UpdateWorkspaceVersion(serveContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("operation=%v", result)
	}
	id, state, epoch, previous := currentDeploymentFacts(t, s, request.WorkspaceId)
	if id == original.DeploymentId || state != "active" || epoch != 2 || previous != original.DeploymentId {
		t.Fatalf("current %s %s %d %s", id, state, epoch, previous)
	}
	next, nextCredential := frozenUpgradeCommand(t, s, id)
	if next.GetResourceSetId() != first.GetResourceSetId() || next.GetDataAttachmentId() != first.GetDataAttachmentId() || !proto.Equal(next.GetManagedKeyBinding(), first.GetManagedKeyBinding()) || nextCredential != credential || credential == "" {
		t.Fatalf("replacement changed frozen resources/credentials: %v", next)
	}
	if next.GetApplicationSelection().GetRuntimeVersionId() != "rv-next" || next.GetCapabilityVersionId() != "" || next.GetDeploymentDescriptor().GetArtifact().GetDigest() != catalog.versions["rv-next"].GetPublisherContract().GetImage().GetDigest() {
		t.Fatal("replacement did not execute exact approved App source")
	}
	if !proto.Equal(next.GetDeploymentDescriptor().GetApplicationRevision(), catalog.versions["rv-next"].GetPublisherContract().GetApplicationRevisionTemplate()) {
		t.Fatal("replacement changed the declared cloud/password entrypoint or credential files")
	}
	if len(runtime.events) < 2 || runtime.events[0] != "suspended" || runtime.events[1] != "start" || resources.binds != 1 || resources.rebinds != 0 || gateway.calls != 0 {
		t.Fatalf("events=%v binds=%d rebinds=%d keys=%d", runtime.events, resources.binds, resources.rebinds, gateway.calls)
	}
	if runtime.credentials[0] != runtime.credentials[1] {
		t.Fatal("password/session derivation identity changed")
	}
	before := runtime.starts
	catalog.versions["rv-next"].Status = api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_REVOKED
	beforeReads := catalog.calls
	replay, err := s.UpdateWorkspaceVersion(serveContext(), request)
	if err != nil || replay.GetOperationId() != result.GetOperationId() || runtime.starts != before || catalog.calls != beforeReads {
		t.Fatalf("replay=%v err=%v starts=%d", replay, err, runtime.starts)
	}
	changed := proto.Clone(request).(*api.UpdateWorkspaceVersionRpcRequest)
	changed.Body.ApplicationSelection.RuntimeVersionId = proto.String("rv-original")
	if _, err = s.UpdateWorkspaceVersion(serveContext(), changed); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("changed replay=%v", err)
	}
	changed = proto.Clone(request).(*api.UpdateWorkspaceVersionRpcRequest)
	changed.Body.ExpectedCurrentAgentDeploymentId = id
	if _, err = s.UpdateWorkspaceVersion(serveContext(), changed); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("changed predecessor replay=%v", err)
	}
	var count int
	if err = s.DB.QueryRow(`SELECT count(*) FROM serve.agent_deployments WHERE workspace_id=$1`, request.WorkspaceId).Scan(&count); err != nil || count != 2 {
		t.Fatalf("deliveries=%d err=%v", count, err)
	}
}
func TestNativeDefaultAppUpgradeRequiresStoppedWriterAndReadySuccessor(t *testing.T) {
	s, original, _, runtime, _, _ := nativeUpgradeFixture(t)
	request := nativeUpgradeRequest(original.DeploymentId, "stop-retry")
	runtime.stopFailure = true
	if _, err := s.UpdateWorkspaceVersion(serveContext(), request); err == nil {
		t.Fatal("unknown stop was accepted")
	}
	if runtime.starts != 1 {
		t.Fatal("started successor before predecessor stop confirmed")
	}
	if _, err := s.UpdateWorkspaceVersion(serveContext(), nativeUpgradeRequest(original.DeploymentId, "another-command")); status.Code(err) != codes.FailedPrecondition || runtime.starts != 1 {
		t.Fatalf("another command admitted a concurrent successor: %v", err)
	}
	runtime.stopFailure = false
	runtime.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_PENDING
	result, err := s.UpdateWorkspaceVersion(serveContext(), request)
	if err != nil {
		t.Fatal(err)
	}
	if result.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatal("pending runtime reported success")
	}
	if result.PollAfterSeconds == nil || result.GetPollAfterSeconds() != 5 {
		t.Fatalf("pending upgrade must provide the owner's polling cadence: %v", result)
	}
	_, target := routeBinding(t, s, request.WorkspaceId)
	if target != original.DeploymentId {
		t.Fatal("route moved before successor readiness")
	}
	starts := runtime.starts
	if _, err := s.UpdateWorkspaceVersion(serveContext(), nativeUpgradeRequest(original.DeploymentId, "another-pending-command")); status.Code(err) != codes.FailedPrecondition || runtime.starts != starts {
		t.Fatalf("pending replacement allowed another writer: %v", err)
	}
	runtime.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_READY
	result, err = s.UpdateWorkspaceVersion(serveContext(), request)
	if err != nil || result.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("resumed=%v %v", result, err)
	}
	if result.PollAfterSeconds != nil {
		t.Fatalf("completed upgrade must stop polling: %v", result)
	}
}

func TestNativeDefaultAppUpgradeUsesConfirmedPredecessorReload(t *testing.T) {
	for _, changedModels := range []bool{false, true} {
		name := "same models retain latest confirmed binding"
		if changedModels {
			name = "changed models refuse rather than revert"
		}
		t.Run(name, func(t *testing.T) {
			s, original, _, runtime, resources, gateway := nativeUpgradeFixture(t)
			_, credential := frozenUpgradeCommand(t, s, original.DeploymentId)
			runtime.reloadVersion = 7
			var selections []*api.ModelSelection
			if changedModels {
				selections = []*api.ModelSelection{{Slot: "chat", ModelId: "customer-later-model"}}
			}
			binding := confirmedReloadBindingReadback("ws_1042", "customer-confirmed-key")
			reloadCall := call("tenant-alpha", false)
			reloadCall.RequestId, reloadCall.IdempotencyKey = "original-model-command", "original-model-command"
			op, err := s.ReloadModels(workspaceContext(), &api.RuntimeReloadCommand{Context: reloadCall, RuntimeInstanceId: original.RuntimeInstanceId, ExpectedAppliedVersion: 0, TargetVersion: 7, Selections: selections, ManagedKeyBinding: binding})
			if err != nil || op.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
				t.Fatalf("reload=%v err=%v", op, err)
			}
			active, rebinds := resources.active, resources.rebinds
			result, err := s.UpdateWorkspaceVersion(serveContext(), nativeUpgradeRequest(original.DeploymentId, "after-reload"))
			if changedModels {
				if status.Code(err) != codes.FailedPrecondition || runtime.starts != 1 || len(runtime.events) != 0 {
					t.Fatalf("unrecoverable model selections triggered replacement: result=%v err=%v events=%v", result, err, runtime.events)
				}
			} else {
				if err != nil || result.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
					t.Fatalf("replacement=%v err=%v", result, err)
				}
				id, _, _, _ := currentDeploymentFacts(t, s, "ws_1042")
				command, nextCredential := frozenUpgradeCommand(t, s, id)
				if command.GetManagedKeyBinding().GetSecretBindingId() != active || command.GetManagedKeyBinding().GetKeyBindingId() != binding.GetKeyBindingId() || command.GetModelConfigurationVersion() != 7 || nextCredential != credential {
					t.Fatal("upgrade changed the confirmed predecessor binding, model version or password/session identity")
				}
			}
			if resources.active != active || resources.rebinds != rebinds || gateway.calls != 0 {
				t.Fatal("upgrade created or rebound a Gateway credential")
			}
		})
	}
}

func TestNativeUpgradeWaitsForUnconfirmedPredecessorReload(t *testing.T) {
	s, original, _, runtime, resources, _ := nativeUpgradeFixture(t)
	runtime.reloadErr = errors.New("reload response lost")
	runtime.reloadVersion = 7
	c := call("tenant-alpha", false)
	c.IdempotencyKey = "predecessor-reload"
	reload := &api.RuntimeReloadCommand{Context: c, RuntimeInstanceId: original.RuntimeInstanceId, TargetVersion: 7, ManagedKeyBinding: confirmedReloadBindingReadback("ws_1042", "same-models-new-binding")}
	if _, err := s.ReloadModels(workspaceContext(), reload); err == nil {
		t.Fatal("unknown reload reported success")
	}
	active, rebinds := resources.active, resources.rebinds
	request := nativeUpgradeRequest(original.DeploymentId, "upgrade-after-unknown-reload")
	if _, err := s.UpdateWorkspaceVersion(serveContext(), request); status.Code(err) != codes.FailedPrecondition || runtime.starts != 1 {
		t.Fatalf("unknown predecessor reload admitted replacement: %v", err)
	}
	runtime.reloadErr = nil
	if _, err := s.ReloadModels(workspaceContext(), reload); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateWorkspaceVersion(serveContext(), request); err != nil {
		t.Fatal(err)
	}
	id, _, _, _ := currentDeploymentFacts(t, s, "ws_1042")
	command, _ := frozenUpgradeCommand(t, s, id)
	if command.GetModelConfigurationVersion() != 7 || command.GetManagedKeyBinding().GetSecretBindingId() != active || resources.rebinds != rebinds {
		t.Fatal("replacement changed the resumed reload's confirmed state")
	}
}

func TestNativeDefaultAppUpgradeRejectsInvalidSourcesBeforeMutation(t *testing.T) {
	s, original, catalog, runtime, _, _ := nativeUpgradeFixture(t)
	for _, test := range []struct {
		name   string
		change func(*api.UpdateWorkspaceVersionRpcRequest)
		code   codes.Code
	}{
		{"mixed legacy", func(r *api.UpdateWorkspaceVersionRpcRequest) { r.Body.CapabilityVersionId = proto.String("") }, codes.InvalidArgument},
		{"mixed selection", func(r *api.UpdateWorkspaceVersionRpcRequest) {
			r.Body.ApplicationSelection.CapabilityVersionId = proto.String("")
		}, codes.InvalidArgument},
		{"stale predecessor", func(r *api.UpdateWorkspaceVersionRpcRequest) { r.Body.ExpectedCurrentAgentDeploymentId = "wrong" }, codes.FailedPrecondition},
		{"foreign tenant", func(r *api.UpdateWorkspaceVersionRpcRequest) { r.Context = call("foreign", false) }, codes.PermissionDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := nativeUpgradeRequest(original.DeploymentId, test.name)
			test.change(r)
			if _, err := s.UpdateWorkspaceVersion(serveContext(), r); status.Code(err) != test.code {
				t.Fatalf("err=%v", err)
			}
		})
	}
	catalog.versions["rv-next"].Status = api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_REVOKED
	if _, err := s.UpdateWorkspaceVersion(serveContext(), nativeUpgradeRequest(original.DeploymentId, "revoked")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("revoked=%v", err)
	}
	catalog.versions["rv-next"].Status = api.RuntimeVersionStatusEnum_RUNTIME_VERSION_STATUS_ENUM_APPROVED
	for _, mutate := range []func(*api.WorkspaceApplicationCredential){
		func(c *api.WorkspaceApplicationCredential) { c.Name = "new-password" },
		func(c *api.WorkspaceApplicationCredential) { c.Target = "/run/secrets/new-password" },
		func(c *api.WorkspaceApplicationCredential) { c.Username = proto.String("another-user") },
	} {
		credentials := catalog.versions["rv-next"].PublisherContract.ApplicationRevisionTemplate.Credentials
		originalCredential := proto.Clone(credentials[1]).(*api.WorkspaceApplicationCredential)
		mutate(credentials[1])
		if _, err := s.UpdateWorkspaceVersion(serveContext(), nativeUpgradeRequest(original.DeploymentId, "changed-credential")); status.Code(err) != codes.FailedPrecondition {
			t.Fatalf("changed credential declaration=%v", err)
		}
		credentials[1] = originalCredential
	}
	catalog.versions["rv-next"].PublisherContract.Data.Upgrade.CompatibleFromSchemaVersions = []string{"different"}
	catalog.versions["rv-next"].PublisherContract.Data.SchemaVersion = "incompatible/v2"
	if _, err := s.UpdateWorkspaceVersion(serveContext(), nativeUpgradeRequest(original.DeploymentId, "incompatible")); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("incompatible=%v", err)
	}
	if runtime.starts != 1 || len(runtime.events) != 0 {
		t.Fatalf("refused replacement mutated runtime: %v", runtime.events)
	}
	var count int
	if err := s.DB.QueryRow(`SELECT count(*) FROM serve.agent_deployments WHERE workspace_id='ws_1042'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("deliveries=%d %v", count, err)
	}
}
