package delivery_test

// Serve owns the Workspace's current application, so a version switch and a
// rollback are Serve's own replacement of it rather than a second delivery
// source. These tests exercise the real owner store, the real route state
// machine and the real replacement orchestration through the product API the
// Console BFF calls.

import (
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	api "opl-cloud/packages/contracts/go/api"
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
	var queued int
	var queuedStatus string
	if err := s.DB.QueryRow(`SELECT count(*),COALESCE(max(status),'') FROM serve.agent_deployments WHERE workspace_id='ws-first' AND execution_epoch=4`).Scan(&queued, &queuedStatus); err != nil {
		t.Fatal(err)
	}
	if queued != 1 || queuedStatus != "queued" {
		t.Fatalf("failed switch left %d epoch-4 deliveries with status %q", queued, queuedStatus)
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
