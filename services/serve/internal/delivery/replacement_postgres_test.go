package delivery_test

// Serve owns the Workspace's current application, so a version switch and a
// rollback are Serve's own replacement of it rather than a second delivery
// source. These tests exercise the real owner store, the real route state
// machine and the real replacement orchestration through the product API the
// Console BFF calls.

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/serve/internal/delivery"
)

// replacementVersion builds a second admitted CapabilityVersion whose artifact
// and descriptor differ from the first delivery in every fact Serve checks, so a
// switch cannot pass by replaying the original identity.
func replacementVersion(t *testing.T, id, digestHex string) *api.CapabilityVersion {
	t.Helper()
	descriptor := &api.DeploymentDescriptor{
		SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1,
		Artifact:      &api.ArtifactReference{Repository: "registry.test/app", Digest: "sha256:" + digestHex},
		Provenance:    api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD,
		ApplicationRevision: &api.WorkspaceApplicationRevision{
			SchemaVersion: 1, ApplicationId: "knowledge-app", Version: "2", Platform: "linux/amd64",
			Image:          "registry.test/app@sha256:" + digestHex,
			ExposurePolicy: api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION,
		},
	}
	return &api.CapabilityVersion{
		Id: id, Status: api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY,
		Artifact: descriptor.GetArtifact(), DeploymentDescriptor: descriptor,
		DeploymentDescriptorDigest: descriptorDigestOf(t, descriptor), DeploymentDescriptorObjectRef: "serve-descriptor://" + id,
		DataCompatibility: &api.DataCompatibility{DataSchemaVersion: "1"},
	}
}

// currentDeploymentFacts reads the one delivery that is the Workspace's current
// application, which is the row every replacement assertion is about.
func currentDeploymentFacts(t *testing.T, s *delivery.Service, workspace string) (id, status string, epoch int64, previous string) {
	t.Helper()
	var prior *string
	if err := s.DB.QueryRow(`SELECT id,status,execution_epoch,previous_deployment_id FROM serve.agent_deployments WHERE workspace_id=$1 AND status='active'`, workspace).Scan(&id, &status, &epoch, &prior); err != nil {
		t.Fatalf("read current deployment: %v", err)
	}
	if prior != nil {
		previous = *prior
	}
	return id, status, epoch, previous
}

func deploymentFacts(t *testing.T, s *delivery.Service, id string) (status string, epoch int64, previous string) {
	t.Helper()
	var prior *string
	if err := s.DB.QueryRow(`SELECT status,execution_epoch,previous_deployment_id FROM serve.agent_deployments WHERE id=$1`, id).Scan(&status, &epoch, &prior); err != nil {
		t.Fatalf("read deployment %s: %v", id, err)
	}
	if prior != nil {
		previous = *prior
	}
	return status, epoch, previous
}

func activeDeployments(t *testing.T, s *delivery.Service, workspace string) int {
	t.Helper()
	var n int
	if err := s.DB.QueryRow(`SELECT count(*) FROM serve.agent_deployments WHERE workspace_id=$1 AND status='active'`, workspace).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func routeBinding(t *testing.T, s *delivery.Service, workspace string) (generation int64, target string) {
	t.Helper()
	var deployment *string
	if err := s.DB.QueryRow(`SELECT route_generation,target_deployment_id FROM serve.access_bindings WHERE workspace_id=$1`, workspace).Scan(&generation, &deployment); err != nil {
		t.Fatalf("read access binding: %v", err)
	}
	if deployment != nil {
		target = *deployment
	}
	return generation, target
}

func TestServeVersionSwitchReplacesTheCurrentApplication(t *testing.T) {
	s, r, cap := reservationFixture(t)
	ctx := workspaceContext()
	second := replacementVersion(t, "cv_2", strings.Repeat("2", 64))
	cap.versions = map[string]*api.CapabilityVersion{second.GetId(): second}
	resourceSet, attachment := "resource-set-original", "attachment-original"
	s.Resources = &resourcesForServe{confirmed: true, workspace: "ws-first", dataAttachment: attachment}
	runtime := &runtimeForServe{}
	s.Runtime = runtime
	first, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Deploy(ctx, deployReserved(r, first)); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	if id, state, epoch, _ := currentDeploymentFacts(t, s, "ws-first"); id != first.DeploymentId || state != "active" || epoch != 1 {
		t.Fatalf("first delivery facts id=%s status=%s epoch=%d", id, state, epoch)
	}
	if generation, target := routeBinding(t, s, "ws-first"); generation != 1 || target != first.DeploymentId {
		t.Fatalf("first route generation=%d target=%s", generation, target)
	}

	// A caller whose expected current delivery is not the Workspace's current one
	// would race another writer, so the switch is refused before anything runs.
	conflict := call("tenant-alpha", false)
	conflict.IdempotencyKey = "switch-conflict"
	before := runtime.starts
	_, err = s.UpdateWorkspaceVersion(serveContext(), &api.UpdateWorkspaceVersionRpcRequest{
		Context: conflict, WorkspaceId: "ws-first",
		Body: &api.UpdateWorkspaceVersionRequest{CapabilityVersionId: second.GetId(), ExpectedCurrentAgentDeploymentId: "dep-not-current"},
	})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(status.Convert(err).Message(), delivery.ReasonCurrentDeploymentConflict) {
		t.Fatalf("stale expected current=%v", err)
	}
	if runtime.starts != before || activeDeployments(t, s, "ws-first") != 1 {
		t.Fatalf("refused switch reached the runtime: starts=%d active=%d", runtime.starts, activeDeployments(t, s, "ws-first"))
	}

	// A Workspace Serve has never delivered has no current application to replace.
	empty := call("tenant-alpha", false)
	empty.IdempotencyKey = "switch-empty"
	_, err = s.UpdateWorkspaceVersion(serveContext(), &api.UpdateWorkspaceVersionRpcRequest{
		Context: empty, WorkspaceId: "ws-empty",
		Body: &api.UpdateWorkspaceVersionRequest{CapabilityVersionId: second.GetId(), ExpectedCurrentAgentDeploymentId: first.DeploymentId},
	})
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(status.Convert(err).Message(), delivery.ReasonReplacementUnavailable) {
		t.Fatalf("no current application=%v", err)
	}

	// A rollback target that is not a delivery of this Workspace is refused rather
	// than restored, even though it is a real delivery of another Workspace.
	seedDeployment(t, s.DB, "ws-other", "tenant-alpha", "dep-other", "active", "ready", "https://other.example/app", api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION)
	foreign := call("tenant-alpha", false)
	foreign.IdempotencyKey = "rollback-foreign"
	_, err = s.RollbackWorkspace(serveContext(), &api.RollbackWorkspaceRpcRequest{
		Context: foreign, WorkspaceId: "ws-first",
		Body: &api.RollbackWorkspaceRequest{TargetDeploymentId: "dep-other", ExpectedCurrentAgentDeploymentId: first.DeploymentId},
	})
	if status.Code(err) != codes.NotFound || !strings.Contains(status.Convert(err).Message(), delivery.ReasonReplacementUnavailable) {
		t.Fatalf("foreign rollback target=%v", err)
	}

	// The switch admits one new delivery at the next execution epoch, reuses the
	// confirmed infrastructure, and becomes the current application only because
	// its own runtime instance was proven ready.
	switchCall := call("tenant-alpha", false)
	switchCall.IdempotencyKey = "switch-version-2"
	operation, err := s.UpdateWorkspaceVersion(serveContext(), &api.UpdateWorkspaceVersionRpcRequest{
		Context: switchCall, WorkspaceId: "ws-first",
		Body: &api.UpdateWorkspaceVersionRequest{CapabilityVersionId: second.GetId(), ExpectedCurrentAgentDeploymentId: first.DeploymentId},
	})
	if err != nil {
		t.Fatalf("version switch: %v", err)
	}
	if operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_UPDATE_WORKSPACE || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || operation.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_VERIFICATION {
		t.Fatalf("switch operation=%v", operation)
	}
	secondID, state, epoch, previous := currentDeploymentFacts(t, s, "ws-first")
	if state != "active" || epoch != 2 || previous != first.DeploymentId || secondID == first.DeploymentId {
		t.Fatalf("switch facts id=%s status=%s epoch=%d previous=%s", secondID, state, epoch, previous)
	}
	if status, _, _ := deploymentFacts(t, s, first.DeploymentId); status != "superseded" {
		t.Fatalf("replaced deployment status=%s", status)
	}
	if activeDeployments(t, s, "ws-first") != 1 {
		t.Fatalf("switch left %d current applications", activeDeployments(t, s, "ws-first"))
	}
	if generation, target := routeBinding(t, s, "ws-first"); generation != 2 || target != secondID {
		t.Fatalf("switched route generation=%d target=%s", generation, target)
	}
	var runtimeResourceSet, runtimeAttachment, runtimeStatus string
	if err := s.DB.QueryRow(`SELECT fabric_resource_set_id,COALESCE(data_attachment_contract->>'attachmentId',''),status FROM serve.agent_runtime_instances WHERE deployment_id=$1`, secondID).Scan(&runtimeResourceSet, &runtimeAttachment, &runtimeStatus); err != nil {
		t.Fatal(err)
	}
	if runtimeResourceSet != resourceSet || runtimeAttachment != attachment || runtimeStatus != "ready" {
		t.Fatalf("replacement runtime set=%s attachment=%s status=%s", runtimeResourceSet, runtimeAttachment, runtimeStatus)
	}
	access, err := s.GetWorkspaceAccess(serveContext(), &api.GetWorkspaceAccessRpcRequest{Context: switchCall, WorkspaceId: "ws-first"})
	if err != nil || access.GetUrl() != "https://ws.example/app" {
		t.Fatalf("switched access=%v err=%v", access, err)
	}

	// A rollback re-executes the target delivery's own stored descriptor as a new
	// delivery that replaces the current one.
	rollbackCall := call("tenant-alpha", false)
	rollbackCall.IdempotencyKey = "rollback-to-first"
	restored, err := s.RollbackWorkspace(serveContext(), &api.RollbackWorkspaceRpcRequest{
		Context: rollbackCall, WorkspaceId: "ws-first",
		Body: &api.RollbackWorkspaceRequest{TargetDeploymentId: first.DeploymentId, ExpectedCurrentAgentDeploymentId: secondID},
	})
	if err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if restored.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_ROLLBACK_WORKSPACE || restored.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("rollback operation=%v", restored)
	}
	rolledID, state, epoch, previous := currentDeploymentFacts(t, s, "ws-first")
	if state != "active" || epoch != 3 || previous != secondID || rolledID == secondID {
		t.Fatalf("rollback facts id=%s status=%s epoch=%d previous=%s", rolledID, state, epoch, previous)
	}
	if status, _, _ := deploymentFacts(t, s, secondID); status != "superseded" {
		t.Fatalf("replaced deployment status=%s", status)
	}
	if generation, target := routeBinding(t, s, "ws-first"); generation != 3 || target != rolledID {
		t.Fatalf("rolled back route generation=%d target=%s", generation, target)
	}

	// A replacement whose execution never proves readiness leaves the current
	// application and its route untouched: the failed switch is visible only as
	// its own queued delivery, never as a lost application.
	runtime.observeErr = true
	failing := call("tenant-alpha", false)
	failing.IdempotencyKey = "switch-fails"
	if _, err = s.UpdateWorkspaceVersion(serveContext(), &api.UpdateWorkspaceVersionRpcRequest{
		Context: failing, WorkspaceId: "ws-first",
		Body: &api.UpdateWorkspaceVersionRequest{CapabilityVersionId: second.GetId(), ExpectedCurrentAgentDeploymentId: rolledID},
	}); err == nil {
		t.Fatal("unobserved replacement reported success")
	}
	runtime.observeErr = false
	if id, state, _, _ := currentDeploymentFacts(t, s, "ws-first"); id != rolledID || state != "active" {
		t.Fatalf("failed switch changed the current application: %s %s", id, state)
	}
	if generation, target := routeBinding(t, s, "ws-first"); generation != 3 || target != rolledID {
		t.Fatalf("failed switch changed the route: generation=%d target=%s", generation, target)
	}
	var attempted int
	var attemptedStatus string
	if err := s.DB.QueryRow(`SELECT count(*),COALESCE(max(status),'') FROM serve.agent_deployments WHERE workspace_id='ws-first' AND execution_epoch=4`).Scan(&attempted, &attemptedStatus); err != nil {
		t.Fatal(err)
	}
	// The replacement whose outcome is unknown is recorded as executing, because
	// its frozen start command was persisted before the provider call. Nothing was
	// retired against an unknown result.
	if attempted != 1 || attemptedStatus != "deploying" {
		t.Fatalf("failed switch left %d epoch-4 deliveries with status %q", attempted, attemptedStatus)
	}
}

// TestServeReplacementRefusesUnprovenDataCompatibility proves a replacement runs
// only against a data contract the target revision declared it can carry, and
// that an unproven or irreversible change is refused instead of executed.
func TestServeReplacementRefusesUnprovenDataCompatibility(t *testing.T) {
	s, r, cap := reservationFixture(t)
	ctx := workspaceContext()
	incompatible := replacementVersion(t, "cv_3", strings.Repeat("3", 64))
	incompatible.DataCompatibility = &api.DataCompatibility{DataSchemaVersion: "2", CompatibleFromVersions: []string{"9"}, RollbackSafe: true}
	migration := replacementVersion(t, "cv_4", strings.Repeat("4", 64))
	migration.DataCompatibility = &api.DataCompatibility{DataSchemaVersion: "2", CompatibleFromVersions: []string{"1"}, RollbackSafe: false, MigrationRequired: true}
	upgrade := replacementVersion(t, "cv_6", strings.Repeat("6", 64))
	upgrade.DataCompatibility = &api.DataCompatibility{DataSchemaVersion: "2", CompatibleFromVersions: []string{"1"}, RollbackSafe: false}
	reversible := replacementVersion(t, "cv_7", strings.Repeat("7", 64))
	reversible.DataCompatibility = &api.DataCompatibility{DataSchemaVersion: "2", CompatibleFromVersions: []string{"2"}, RollbackSafe: true, MigrationRequired: true}
	cap.versions = map[string]*api.CapabilityVersion{}
	for _, version := range []*api.CapabilityVersion{incompatible, migration, upgrade, reversible} {
		cap.versions[version.GetId()] = version
	}
	s.Resources = &resourcesForServe{confirmed: true, workspace: "ws-first", dataAttachment: "attachment-original"}
	runtime := &runtimeForServe{}
	s.Runtime = runtime
	first, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Deploy(ctx, deployReserved(r, first)); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	switchTo := func(key, versionID, expectedCurrent string) (*api.Operation, error) {
		call := call("tenant-alpha", false)
		call.IdempotencyKey = key
		return s.UpdateWorkspaceVersion(serveContext(), &api.UpdateWorkspaceVersionRpcRequest{
			Context: call, WorkspaceId: "ws-first",
			Body: &api.UpdateWorkspaceVersionRequest{CapabilityVersionId: versionID, ExpectedCurrentAgentDeploymentId: expectedCurrent},
		})
	}
	// The target does not declare compatibility with the schema the Workspace
	// already has.
	if _, err = switchTo("switch-incompatible", incompatible.GetId(), first.DeploymentId); status.Code(err) != codes.FailedPrecondition || !strings.Contains(status.Convert(err).Message(), delivery.ReasonDataCompatibilityUnproven) {
		t.Fatalf("incompatible switch=%v", err)
	}
	// The target declares the current schema but requires a migration it does not
	// declare rollback-safe.
	if _, err = switchTo("switch-migration", migration.GetId(), first.DeploymentId); status.Code(err) != codes.FailedPrecondition || !strings.Contains(status.Convert(err).Message(), delivery.ReasonDataRollbackUnsafe) {
		t.Fatalf("unsafe migration switch=%v", err)
	}
	if id, state, epoch, _ := currentDeploymentFacts(t, s, "ws-first"); id != first.DeploymentId || state != "active" || epoch != 1 {
		t.Fatalf("refused switches changed the current application: %s %s %d", id, state, epoch)
	}
	if generation, target := routeBinding(t, s, "ws-first"); generation != 1 || target != first.DeploymentId {
		t.Fatalf("refused switches changed the route: generation=%d target=%s", generation, target)
	}
	if starts := runtime.starts; starts != 1 {
		t.Fatalf("refused switches reached the runtime: starts=%d", starts)
	}

	// A declared compatible upgrade is admitted and records its own contract.
	operation, err := switchTo("switch-declared-compatible", upgrade.GetId(), first.DeploymentId)
	if err != nil {
		t.Fatalf("declared-compatible switch: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("declared-compatible switch operation=%v", operation)
	}
	upgraded, _, epoch, _ := currentDeploymentFacts(t, s, "ws-first")
	if epoch != 2 {
		t.Fatalf("declared-compatible switch epoch=%d", epoch)
	}
	var recordedSchema string
	if err := s.DB.QueryRow(`SELECT data_compatibility->>'dataSchemaVersion' FROM serve.agent_deployments WHERE id=$1`, upgraded).Scan(&recordedSchema); err != nil {
		t.Fatal(err)
	}
	if recordedSchema != "2" {
		t.Fatalf("recorded schema=%q", recordedSchema)
	}

	// Rolling back out of a version that does not declare its own data
	// rollback-safe cannot be proven safe and is refused.
	rollbackCall := call("tenant-alpha", false)
	rollbackCall.IdempotencyKey = "rollback-unproven"
	if _, err = s.RollbackWorkspace(serveContext(), &api.RollbackWorkspaceRpcRequest{
		Context: rollbackCall, WorkspaceId: "ws-first",
		Body: &api.RollbackWorkspaceRequest{TargetDeploymentId: first.DeploymentId, ExpectedCurrentAgentDeploymentId: upgraded},
	}); status.Code(err) != codes.FailedPrecondition || !strings.Contains(status.Convert(err).Message(), delivery.ReasonDataRollbackUnsafe) {
		t.Fatalf("unproven rollback=%v", err)
	}
	if id, _, epoch, _ := currentDeploymentFacts(t, s, "ws-first"); id != upgraded || epoch != 2 {
		t.Fatalf("refused rollback changed the current application: %s %d", id, epoch)
	}

	// A migration the publisher declares rollback-safe is admitted.
	second, err := switchTo("switch-reversible-migration", reversible.GetId(), upgraded)
	if err != nil {
		t.Fatalf("rollback-safe migration switch: %v", err)
	}
	if second.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("rollback-safe migration operation=%v", second)
	}
	final, _, epoch, previous := currentDeploymentFacts(t, s, "ws-first")
	if epoch != 3 || previous != upgraded {
		t.Fatalf("rollback-safe migration facts id=%s epoch=%d previous=%s", final, epoch, previous)
	}
	var migratedSchema string
	var migrated bool
	if err := s.DB.QueryRow(`SELECT data_compatibility->>'dataSchemaVersion',(data_compatibility->>'migrationRequired')::boolean FROM serve.agent_deployments WHERE id=$1`, final).Scan(&migratedSchema, &migrated); err != nil {
		t.Fatal(err)
	}
	if migratedSchema != "2" || !migrated {
		t.Fatalf("recorded migration contract schema=%q migrationRequired=%v", migratedSchema, migrated)
	}
}

// publisherContractExample loads the runtime publisher contract the SSOT schema
// publishes as its own example, so a test descriptor carries a complete contract
// instead of a hand-built partial one.
func publisherContractExample(t *testing.T) *api.RuntimePublisherContract {
	t.Helper()
	raw, err := os.ReadFile("../../../../docs/spec/target/contracts/publisher-contract.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Examples []json.RawMessage
	}
	if err = json.Unmarshal(raw, &document); err != nil || len(document.Examples) == 0 {
		t.Fatalf("publisher contract schema has no example: %v", err)
	}
	contract := &api.RuntimePublisherContract{}
	if err = publicjson.Unmarshal(document.Examples[0], contract); err != nil {
		t.Fatal(err)
	}
	return contract
}

// withPersistentMount gives one admitted application revision a read-write
// persistent mount, and its publisher contract the matching mount policy, so the
// replacement's single-writer rule has the facts it reads.
func withPersistentMount(t *testing.T, descriptor *api.DeploymentDescriptor, shareable bool) *api.DeploymentDescriptor {
	t.Helper()
	clone := proto.Clone(descriptor).(*api.DeploymentDescriptor)
	clone.ApplicationRevision.PersistentMounts = []*api.WorkspaceApplicationMount{{Name: "workspace-data", MountPath: "/var/lib/opl"}}
	contract := publisherContractExample(t)
	contract.Data.MountPolicies = []*api.DataMountPolicy{{MountName: "workspace-data", ConcurrentWritersSupported: shareable}}
	clone.RuntimeContract = contract
	return clone
}

// exclusiveSwitchFixture delivers a first application whose declared mount
// cannot be shared, and returns the Workspace's service plus the release that
// replaces it.
func exclusiveSwitchFixture(t *testing.T) (*delivery.Service, *api.RuntimeReservationCommand, *api.RuntimeReservation, *api.CapabilityVersion, *runtimeForServe) {
	t.Helper()
	s, base, cap := reservationFixture(t)
	first := withPersistentMount(t, base.DeploymentDescriptor, false)
	request := proto.Clone(base).(*api.RuntimeReservationCommand)
	request.DeploymentDescriptor = first
	request.Artifact = first.GetArtifact()
	request.DeploymentDescriptorDigest = descriptorDigestOf(t, first)
	request.Context.IdempotencyKey = "exclusive-first"
	cap.version = &api.CapabilityVersion{
		Id: "cv_1", Status: api.CapabilityVersionStatusEnum_CAPABILITY_VERSION_STATUS_ENUM_READY,
		Artifact: request.Artifact, DeploymentDescriptor: request.DeploymentDescriptor,
		DeploymentDescriptorDigest: request.DeploymentDescriptorDigest, DeploymentDescriptorObjectRef: request.DeploymentDescriptorObjectRef,
		DataCompatibility: &api.DataCompatibility{DataSchemaVersion: "1"},
	}
	second := replacementVersion(t, "cv_2", strings.Repeat("2", 64))
	second.DeploymentDescriptor = withPersistentMount(t, second.DeploymentDescriptor, false)
	second.Artifact = second.DeploymentDescriptor.GetArtifact()
	second.DeploymentDescriptorDigest = descriptorDigestOf(t, second.DeploymentDescriptor)
	cap.versions = map[string]*api.CapabilityVersion{second.GetId(): second}
	s.Resources = &resourcesForServe{confirmed: true, workspace: "ws-first", dataAttachment: "attachment-original"}
	runtime := &runtimeForServe{}
	s.Runtime = runtime
	ctx := workspaceContext()
	reservation, err := s.Reserve(ctx, request)
	if err != nil {
		t.Fatalf("exclusive first reservation: %v", err)
	}
	if _, err = s.Deploy(ctx, deployReserved(request, reservation)); err != nil {
		t.Fatalf("exclusive first delivery: %v", err)
	}
	runtime.lifecycle = nil
	return s, request, reservation, second, runtime
}

func switchVersion(t *testing.T, s *delivery.Service, key, versionID, expectedCurrent string) (*api.Operation, error) {
	t.Helper()
	call := call("tenant-alpha", false)
	call.IdempotencyKey = key
	return s.UpdateWorkspaceVersion(serveContext(), &api.UpdateWorkspaceVersionRpcRequest{
		Context: call, WorkspaceId: "ws-first",
		Body: &api.UpdateWorkspaceVersionRequest{CapabilityVersionId: versionID, ExpectedCurrentAgentDeploymentId: expectedCurrent},
	})
}

// TestServeReplacementQuiescesAnExclusiveDataWriter proves a replacement that
// cannot share the Workspace's retained data stops the running writer first, so
// the two revisions never hold the same mount at once.
func TestServeReplacementQuiescesAnExclusiveDataWriter(t *testing.T) {
	s, _, reservation, second, runtime := exclusiveSwitchFixture(t)
	operation, err := switchVersion(t, s, "switch-exclusive", second.GetId(), reservation.DeploymentId)
	if err != nil {
		t.Fatalf("exclusive switch: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("exclusive switch operation=%v", operation)
	}
	if !reflect.DeepEqual(runtime.lifecycle, []string{"suspended"}) {
		t.Fatalf("exclusive switch lifecycle=%v, want the previous writer stopped", runtime.lifecycle)
	}
	current, state, epoch, previous := currentDeploymentFacts(t, s, "ws-first")
	if state != "active" || epoch != 2 || previous != reservation.DeploymentId || current == reservation.DeploymentId {
		t.Fatalf("exclusive switch facts id=%s status=%s epoch=%d previous=%s", current, state, epoch, previous)
	}
	if generation, target := routeBinding(t, s, "ws-first"); generation != 2 || target != current {
		t.Fatalf("exclusive switch route generation=%d target=%s", generation, target)
	}
	var stopped string
	if err := s.DB.QueryRow(`SELECT status FROM serve.agent_runtime_instances WHERE deployment_id=$1`, reservation.DeploymentId).Scan(&stopped); err != nil {
		t.Fatal(err)
	}
	if stopped != "stopped" {
		t.Fatalf("replaced writer status=%q, want stopped", stopped)
	}
}

// TestServeReplacementRestoresTheStoppedWriterAfterDefiniteFailure proves a
// replacement that definitely failed is retired and the writer the switch
// stopped is restored, with the route never having moved.
func TestServeReplacementRestoresTheStoppedWriterAfterDefiniteFailure(t *testing.T) {
	s, _, reservation, second, runtime := exclusiveSwitchFixture(t)
	runtime.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED
	operation, err := switchVersion(t, s, "switch-exclusive-fails", second.GetId(), reservation.DeploymentId)
	if err != nil {
		t.Fatalf("definite failure returned a transport error: %v", err)
	}
	if operation.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("definitely failed replacement reported success: %v", operation)
	}
	if !reflect.DeepEqual(runtime.lifecycle, []string{"suspended", "absent", "running"}) {
		t.Fatalf("failure recovery lifecycle=%v, want stop then retire then restore", runtime.lifecycle)
	}
	id, state, epoch, _ := currentDeploymentFacts(t, s, "ws-first")
	if id != reservation.DeploymentId || state != "active" || epoch != 1 {
		t.Fatalf("failed replacement changed the current application: %s %s %d", id, state, epoch)
	}
	if generation, target := routeBinding(t, s, "ws-first"); generation != 1 || target != reservation.DeploymentId {
		t.Fatalf("failed replacement changed the route: generation=%d target=%s", generation, target)
	}
	var restored string
	if err := s.DB.QueryRow(`SELECT status FROM serve.agent_runtime_instances WHERE deployment_id=$1`, reservation.DeploymentId).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored != "ready" {
		t.Fatalf("restored writer status=%q, want ready", restored)
	}
	var failedStatus, failedRuntime string
	if err := s.DB.QueryRow(`SELECT d.status,r.status FROM serve.agent_deployments d JOIN serve.agent_runtime_instances r ON r.deployment_id=d.id WHERE d.execution_epoch=2 AND d.workspace_id='ws-first'`).Scan(&failedStatus, &failedRuntime); err != nil {
		t.Fatal(err)
	}
	if failedStatus != "verifying" || failedRuntime != "terminated" {
		t.Fatalf("retired replacement status=%q runtime=%q", failedStatus, failedRuntime)
	}
}

// TestServeVersionSwitchResumesItsOwnDeliveryOnReplay proves a switch whose
// response was lost converges on its original delivery instead of allocating a
// second one, and that every delivery keeps the frozen command a later stop,
// reload or replacement resumes from.
func TestServeVersionSwitchResumesItsOwnDeliveryOnReplay(t *testing.T) {
	s, _, reservation, second, _ := exclusiveSwitchFixture(t)
	first, err := switchVersion(t, s, "switch-replayed", second.GetId(), reservation.DeploymentId)
	if err != nil {
		t.Fatalf("switch: %v", err)
	}
	replayed, err := switchVersion(t, s, "switch-replayed", second.GetId(), reservation.DeploymentId)
	if err != nil {
		t.Fatalf("replayed switch: %v", err)
	}
	if replayed.GetOperationId() != first.GetOperationId() || replayed.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("replay=%v first=%v", replayed, first)
	}
	var deliveries int
	if err := s.DB.QueryRow(`SELECT count(*) FROM serve.agent_deployments WHERE workspace_id='ws-first'`).Scan(&deliveries); err != nil {
		t.Fatal(err)
	}
	if deliveries != 2 {
		t.Fatalf("replay allocated %d deliveries, want the original two", deliveries)
	}
	var starts int
	if err := s.DB.QueryRow(`SELECT count(*) FROM serve.agent_runtime_actions WHERE action='start'`).Scan(&starts); err != nil {
		t.Fatal(err)
	}
	if starts != 2 {
		t.Fatalf("recorded %d start commands, want one per delivery", starts)
	}
}

// TestServeReplayedSwitchRestoresTheWriterItStopped proves a switch that took the
// Workspace's only writer away and then lost its result still restores that writer
// when the caller replays it and the replacement turns out to have failed. The
// stop is recovered from the record the switch itself committed, not re-derived
// from whichever application the Workspace happens to serve.
func TestServeReplayedSwitchRestoresTheWriterItStopped(t *testing.T) {
	s, _, reservation, second, runtime := exclusiveSwitchFixture(t)
	runtime.observeErr = true
	if _, err := switchVersion(t, s, "switch-replayed-failure", second.GetId(), reservation.DeploymentId); err == nil {
		t.Fatal("an unknown replacement outcome was reported as a result")
	}
	runtime.observeErr = false
	runtime.state = api.AgentRuntimeObservationState_RUNTIME_INSTANCE_STATE_FAILED
	operation, err := switchVersion(t, s, "switch-replayed-failure", second.GetId(), reservation.DeploymentId)
	if err != nil {
		t.Fatalf("replayed switch: %v", err)
	}
	if operation.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("replayed failed switch reported success: %v", operation)
	}
	if !reflect.DeepEqual(runtime.lifecycle, []string{"suspended", "absent", "running"}) {
		t.Fatalf("replayed failure recovery lifecycle=%v, want stop then retire then restore", runtime.lifecycle)
	}
	id, state, epoch, _ := currentDeploymentFacts(t, s, "ws-first")
	if id != reservation.DeploymentId || state != "active" || epoch != 1 {
		t.Fatalf("replayed failed switch changed the current application: %s %s %d", id, state, epoch)
	}
	var restored string
	if err := s.DB.QueryRow(`SELECT status FROM serve.agent_runtime_instances WHERE deployment_id=$1`, reservation.DeploymentId).Scan(&restored); err != nil {
		t.Fatal(err)
	}
	if restored != "ready" {
		t.Fatalf("replayed switch left the restored writer %q, want ready", restored)
	}
}

// servePeerContext addresses Serve's own execution adapter surface, which only
// Serve's own process is admitted to call.
// appliedModelConfiguration reads the runtime's own stored applied model
// configuration version.
func appliedModelConfiguration(t *testing.T, s *delivery.Service, runtimeInstanceID string) int64 {
	t.Helper()
	var applied int64
	if err := s.DB.QueryRow(`SELECT applied_model_configuration_version FROM serve.agent_runtime_instances WHERE id=$1`, runtimeInstanceID).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	return applied
}

func servePeerContext() context.Context {
	return ownerservice.WithPeerOwner(context.Background(), owneridentity.Serve.Service())
}

// TestServeRuntimeLifecycleActsOnThePersistedCommand proves Stop, Reload and the
// credential read resolve the exact original runtime of a delivered application
// and report only states Serve actually committed. Before this change the frozen
// command carried no call context, so every one of these three actions panicked
// on the peer readback.
func TestServeRuntimeLifecycleActsOnThePersistedCommand(t *testing.T) {
	s, r, _ := reservationFixture(t)
	ctx := workspaceContext()
	s.Resources = &resourcesForServe{confirmed: true, workspace: "ws-first", dataAttachment: "attachment-original"}
	runtime := &runtimeForServe{}
	s.Runtime = runtime
	reservation, err := s.Reserve(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Deploy(ctx, deployReserved(r, reservation)); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	peer := servePeerContext()
	// Stop applies the suspended state and completes only because the provider
	// confirmed it; the operation is never reported as succeeded on a guess.
	stop, err := s.StopRuntime(peer, &api.RuntimeStopCommand{RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId})
	if err != nil {
		t.Fatalf("stop: %v", err)
	}
	if stop.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || stop.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_RUNTIME || stop.GetOperationId() == "" || stop.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE {
		t.Fatalf("stop operation=%v", stop)
	}
	resumed, err := s.StopRuntime(peer, &api.RuntimeStopCommand{RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId})
	if err != nil {
		t.Fatalf("resumed stop: %v", err)
	}
	if resumed.GetOperationId() != stop.GetOperationId() {
		t.Fatalf("resumed stop allocated a second operation: %v", resumed)
	}
	// A reload applies the requested configuration through the frozen publisher
	// interface and advances the applied version only to the version the application
	// itself read back.
	runtime.reloadVersion = 2
	reload, err := s.ReloadRuntime(peer, &api.RuntimeReloadCommand{RuntimeInstanceId: reservation.RuntimeInstanceId, ExpectedAppliedVersion: 0, TargetVersion: 2, Selections: []*api.ModelSelection{{Slot: "chat", ModelId: "model-2"}}})
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reload.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || reload.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_RUNTIME {
		t.Fatalf("reload operation=%v", reload)
	}
	applied := appliedModelConfiguration(t, s, reservation.RuntimeInstanceId)
	if applied != 2 {
		t.Fatalf("applied model configuration=%d, want the version the application read back", applied)
	}
	// A later configuration version is its own durable operation and its own applied
	// fact, so repeated model configuration updates are not refused as a replayed
	// first one.
	runtime.reloadVersion = 3
	second, err := s.ReloadRuntime(peer, &api.RuntimeReloadCommand{RuntimeInstanceId: reservation.RuntimeInstanceId, ExpectedAppliedVersion: 2, TargetVersion: 3, Selections: []*api.ModelSelection{{Slot: "chat", ModelId: "model-3"}}})
	if err != nil {
		t.Fatalf("second reload: %v", err)
	}
	if second.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || second.GetOperationId() == reload.GetOperationId() {
		t.Fatalf("second reload operation=%v first=%v", second, reload)
	}
	if applied = appliedModelConfiguration(t, s, reservation.RuntimeInstanceId); applied != 3 {
		t.Fatalf("applied model configuration=%d, want 3", applied)
	}
	// A readback the application did not answer with the requested version leaves
	// the last confirmed configuration exactly as it was.
	stale := &runtimeForServe{reloadVersion: 4}
	s.Runtime = stale
	if _, err = s.ReloadRuntime(peer, &api.RuntimeReloadCommand{RuntimeInstanceId: reservation.RuntimeInstanceId, ExpectedAppliedVersion: 3, TargetVersion: 5, Selections: []*api.ModelSelection{{Slot: "chat", ModelId: "model-5"}}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("unconfirmed reload err=%v want a failed precondition", err)
	}
	if applied = appliedModelConfiguration(t, s, reservation.RuntimeInstanceId); applied != 3 {
		t.Fatalf("unconfirmed reload advanced the applied configuration to %d", applied)
	}
	// The expected version is a precondition, not a hint: a caller that expects a
	// configuration the runtime does not hold is refused before any provider call.
	stale.reloads = nil
	if _, err = s.ReloadRuntime(peer, &api.RuntimeReloadCommand{RuntimeInstanceId: reservation.RuntimeInstanceId, ExpectedAppliedVersion: 1, TargetVersion: 6, Selections: []*api.ModelSelection{{Slot: "chat", ModelId: "model-6"}}}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale expected version err=%v", err)
	}
	if len(stale.reloads) != 0 {
		t.Fatalf("a stale expected version still called the execution boundary: %v", stale.reloads)
	}
	s.Runtime = runtime
	credentials, err := s.ReadApplicationCredentials(peer, &api.ReadApplicationCredentialsRequest{WorkspaceId: "ws-first", RuntimeInstanceId: reservation.RuntimeInstanceId, DeploymentId: reservation.DeploymentId})
	if err != nil {
		t.Fatalf("credentials: %v", err)
	}
	if credentials.GetUsername() != "admin" || credentials.GetPassword() != "issued-once" || credentials.GetWorkspaceId() != "ws-first" {
		t.Fatalf("credentials=%v", credentials)
	}
	if !reflect.DeepEqual(runtime.lifecycle, []string{"suspended"}) {
		t.Fatalf("lifecycle=%v, want the single stop this test issued", runtime.lifecycle)
	}
}

// TestServeReloadModelsAdmitsOnlyTheWorkspaceOwner proves the coordination entry
// point the Workspace owner calls reaches the same owner-local reload as the
// execution adapter: the admitted peer applies the requested configuration and the
// stored applied version advances only to the version the application read back,
// while another owner's call is refused before anything is applied.
func TestServeReloadModelsAdmitsOnlyTheWorkspaceOwner(t *testing.T) {
	s, r, reservation, _, _ := managedKeyFixture(t)
	ctx := workspaceContext()
	runtime := &runtimeForServe{reloadVersion: 7}
	s.Runtime = runtime
	deploy := deployReserved(r, reservation)
	deploy.ModelSelections = []*api.ModelSelection{{Slot: "chat", ModelId: "model-original"}}
	deploy.ManagedKeyBinding = launchManagedKeyBinding(r.GetWorkspaceId(), reservation.RuntimeInstanceId, "key-original")
	if _, err := s.Deploy(ctx, deploy); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	binding := confirmedReloadBindingFixture(r.GetWorkspaceId(), "key-7", "v7")
	command := &api.RuntimeReloadCommand{RuntimeInstanceId: reservation.RuntimeInstanceId, ExpectedAppliedVersion: 0, TargetVersion: 7, Selections: []*api.ModelSelection{{Slot: "chat", ModelId: "model-7"}}, ManagedKeyBinding: binding}
	// A peer that is not the Workspace owner never reaches the reload path.
	foreign := ownerservice.WithPeerOwner(context.Background(), owneridentity.Capability.Service())
	if _, err := s.ReloadModels(foreign, command); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("foreign peer err=%v want permission denied", err)
	}
	if len(runtime.reloads) != 0 {
		t.Fatalf("a refused caller still reached the execution boundary: %v", runtime.reloads)
	}
	operation, err := s.ReloadModels(ctx, command)
	if err != nil {
		t.Fatalf("reload models: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || operation.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE || operation.GetOperationId() == "" {
		t.Fatalf("operation=%v", operation)
	}
	if applied := appliedModelConfiguration(t, s, reservation.RuntimeInstanceId); applied != 7 {
		t.Fatalf("applied model configuration=%d, want the version the application read back", applied)
	}
	// A caller that still expects the pre-reload version is refused: the stored
	// applied version is a precondition, so a lost response is resolved by reading
	// the applied version, never by resending a stale expectation.
	if _, err = s.ReloadModels(ctx, command); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale expectation err=%v want a failed precondition", err)
	}
	// The command is idempotent by its target version: resuming it with the version
	// the runtime now holds replays the same durable operation instead of allocating
	// a second applied fact.
	resumed, err := s.ReloadModels(ctx, &api.RuntimeReloadCommand{RuntimeInstanceId: reservation.RuntimeInstanceId, ExpectedAppliedVersion: 7, TargetVersion: 7, Selections: []*api.ModelSelection{{Slot: "chat", ModelId: "model-7"}}, ManagedKeyBinding: binding})
	if err != nil {
		t.Fatalf("resumed reload: %v", err)
	}
	if resumed.GetOperationId() != operation.GetOperationId() {
		t.Fatalf("resumed reload allocated a second operation: %v", resumed)
	}
	if applied := appliedModelConfiguration(t, s, reservation.RuntimeInstanceId); applied != 7 {
		t.Fatalf("resumed reload advanced the applied configuration to %d", applied)
	}
}

// TestServeReloadModelsRefusesAnUnconfirmedManagedKeyBinding proves Serve applies
// only the credential generation its owners confirmed: a reload with no binding, an
// incomplete one, or one that names another Workspace's Secret delivery is refused
// before the execution boundary is reached and never advances the applied version.
// Serve issues no Gateway key of its own on the reload path.
func TestServeReloadModelsRefusesAnUnconfirmedManagedKeyBinding(t *testing.T) {
	for name, binding := range map[string]*api.RuntimeManagedKeyBinding{
		"absent":          nil,
		"incomplete":      {KeyBindingId: "key-7", Fingerprint: "sha256:" + strings.Repeat("ab", 32), SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef("ws-first"), TargetSlot: "gateway"},
		"foreign deliver": {KeyBindingId: "key-7", Fingerprint: "sha256:" + strings.Repeat("ab", 32), SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef("ws-other"), TargetSlot: "gateway", SecretBindingId: "sbx_key-7", SecretVersion: "v7"},
		"foreign slot":    {KeyBindingId: "key-7", Fingerprint: "sha256:" + strings.Repeat("ab", 32), SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef("ws-first"), TargetSlot: "other", SecretBindingId: "sbx_key-7", SecretVersion: "v7"},
	} {
		t.Run(name, func(t *testing.T) {
			s, r, reservation, gateway, _ := managedKeyFixture(t)
			ctx := workspaceContext()
			runtime := &runtimeForServe{reloadVersion: 7}
			s.Runtime = runtime
			deploy := deployReserved(r, reservation)
			deploy.ModelSelections = []*api.ModelSelection{{Slot: "chat", ModelId: "model-original"}}
			deploy.ManagedKeyBinding = launchManagedKeyBinding(r.GetWorkspaceId(), reservation.RuntimeInstanceId, "key-original")
			if _, err := s.Deploy(ctx, deploy); err != nil {
				t.Fatalf("first delivery: %v", err)
			}
			if gateway.calls != 0 {
				t.Fatalf("Serve minted %d keys on the launch path, want none", gateway.calls)
			}
			_, err := s.ReloadModels(ctx, &api.RuntimeReloadCommand{RuntimeInstanceId: reservation.RuntimeInstanceId, ExpectedAppliedVersion: 0, TargetVersion: 7, Selections: []*api.ModelSelection{{Slot: "chat", ModelId: "model-7"}}, ManagedKeyBinding: binding})
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("err=%v want a failed precondition", err)
			}
			if len(runtime.reloads) != 0 {
				t.Fatalf("a refused reload reached the execution boundary: %v", runtime.reloads)
			}
			if gateway.calls != 0 {
				t.Fatalf("the reload minted %d Gateway keys, want none", gateway.calls)
			}
			if applied := appliedModelConfiguration(t, s, reservation.RuntimeInstanceId); applied != 0 {
				t.Fatalf("a refused reload advanced the applied configuration to %d", applied)
			}
		})
	}
}
