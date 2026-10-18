package launch

// This file owns the model-configuration update tests. The Workspace must keep
// the exact Gateway-managed key it already holds: it reads that binding and its
// approved model scope from the Gateway owner, refuses a selection outside that
// scope, records the new configuration under the same binding, and lets Serve
// perform the runtime Secret handover. A model update must never mint, rebind or
// revoke a key of its own.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	contracts "opl-cloud/packages/contracts/go"
	api "opl-cloud/packages/contracts/go/api"
	"opl-cloud/packages/contracts/go/owneridentity"
	"opl-cloud/packages/contracts/go/publicjson"
	"opl-cloud/services/internal/ownerservice"
	"opl-cloud/services/internal/ownerstore"
)

// modelUpdateGateway is the GatewayCoordination fixture. ReadManagedKey serves the
// exact binding and approved model scope the Workspace already holds. The issuance
// and revocation entries exist so a model update that reaches for them is counted
// and fails the test instead of silently minting a second key.
type modelUpdateGateway struct {
	api.GatewayCoordinationClient
	reads, mints, revokes int
	lastRead              *api.ManagedKeyReadCommand
	lastMint              *api.ManagedKeyCommand
	lastRevoke            *api.ManagedKeyRevoke
	readErr               error
	mintErr               error
	binding               *api.ManagedKeyBinding
	createBinding         *api.ManagedKeyBinding
}

func (g *modelUpdateGateway) ReadManagedKey(_ context.Context, r *api.ManagedKeyReadCommand, _ ...grpc.CallOption) (*api.ManagedKeyBinding, error) {
	g.reads++
	g.lastRead = proto.Clone(r).(*api.ManagedKeyReadCommand)
	if g.readErr != nil {
		return nil, g.readErr
	}
	if g.binding == nil {
		return nil, status.Error(codes.NotFound, "fixture has no managed key binding")
	}
	return proto.Clone(g.binding).(*api.ManagedKeyBinding), nil
}

func (g *modelUpdateGateway) CreateManagedKey(_ context.Context, r *api.ManagedKeyCommand, _ ...grpc.CallOption) (*api.ManagedKeyBinding, error) {
	g.mints++
	g.lastMint = proto.Clone(r).(*api.ManagedKeyCommand)
	if g.mintErr != nil {
		return nil, g.mintErr
	}
	if g.createBinding != nil {
		return proto.Clone(g.createBinding).(*api.ManagedKeyBinding), nil
	}
	return &api.ManagedKeyBinding{KeyBindingId: "gateway-key-" + strings.Join(r.GetModelIds(), "_"), Fingerprint: "sha256:" + strings.Repeat("cd", 32), SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(r.GetWorkspaceId()), WorkspaceId: r.GetWorkspaceId(), TargetRuntimeInstanceId: r.GetTargetRuntimeInstanceId(), ModelIds: r.GetModelIds()}, nil
}

func (g *modelUpdateGateway) RevokeManagedKey(_ context.Context, r *api.ManagedKeyRevoke, _ ...grpc.CallOption) (*api.Operation, error) {
	g.revokes++
	g.lastRevoke = proto.Clone(r).(*api.ManagedKeyRevoke)
	return nil, status.Error(codes.Internal, "an ordinary model update must not revoke a managed key")
}

// modelUpdateFabric is the FabricCoordination fixture. A model-configuration
// update must not bind or rebind a Secret itself: Serve owns that handover.
type modelUpdateFabric struct {
	api.FabricCoordinationClient
	binds, rebinds int
}

func (f *modelUpdateFabric) BindSecret(_ context.Context, _ *api.SecretBindingCommand, _ ...grpc.CallOption) (*api.SecretBindingReadback, error) {
	f.binds++
	return nil, status.Error(codes.Internal, "a model update must not bind a Secret directly")
}

func (f *modelUpdateFabric) RebindSecret(_ context.Context, _ *api.SecretBindingRebindCommand, _ ...grpc.CallOption) (*api.SecretBindingRebindReadback, error) {
	f.rebinds++
	return nil, status.Error(codes.Internal, "a model update must not rebind a Secret directly")
}

// modelUpdateServe is the ServeAgentCoordination fixture. It records the handover
// Serve receives and refuses, in the test, any Fabric-confirmed binding a caller
// is not allowed to supply.
type modelUpdateServe struct {
	api.ServeAgentCoordinationClient
	reloads            int
	lastReload         *api.RuntimeReloadCommand
	reloadErr          error
	serveStatus        api.OperationStatusEnum
	operationNil       bool
	handoverViolations []string
	beforeReloadAnswer func(context.Context, *api.RuntimeReloadCommand) error
}

func (s *modelUpdateServe) ReloadModels(ctx context.Context, r *api.RuntimeReloadCommand, _ ...grpc.CallOption) (*api.Operation, error) {
	s.reloads++
	s.lastReload = proto.Clone(r).(*api.RuntimeReloadCommand)
	if binding := r.GetManagedKeyBinding(); binding.GetSecretBindingId() != "" || binding.GetSecretVersion() != "" {
		s.handoverViolations = append(s.handoverViolations, "the caller supplied a Fabric-confirmed Secret binding")
		return nil, status.Error(codes.FailedPrecondition, "the caller may not supply a Fabric-confirmed Secret binding; Serve obtains it from Fabric")
	}
	if s.reloadErr != nil {
		return nil, s.reloadErr
	}
	if s.beforeReloadAnswer != nil {
		if err := s.beforeReloadAnswer(ctx, r); err != nil {
			return nil, err
		}
	}
	operationStatus := s.serveStatus
	if operationStatus == api.OperationStatusEnum_OPERATION_STATUS_ENUM_UNSPECIFIED {
		operationStatus = api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED
	}
	if s.operationNil {
		return &api.Operation{}, nil
	}
	return &api.Operation{OperationId: "serve-reload-operation", Owner: api.OperationOwnerEnum_OPERATION_OWNER_ENUM_SERVE, Kind: api.OperationKindEnum_OPERATION_KIND_ENUM_RUNTIME_RELOAD, ResourceId: r.GetRuntimeInstanceId(), Status: operationStatus, Stage: api.OperationStageEnum_OPERATION_STAGE_ENUM_SUCCEEDED}, nil
}

// modelUpdateRevision is the frozen revision the update path continues: it declares
// the installation Gateway credential, which is exactly the capability a model
// configuration update requires.
func modelUpdateRevision(t *testing.T, declareGateway bool) *api.WorkspaceApplicationRevision {
	t.Helper()
	revision := &api.WorkspaceApplicationRevision{
		SchemaVersion: 1, ApplicationId: "knowledge-app", Version: "1", Platform: "linux/amd64",
		Image:          "registry.test/app@sha256:1111111111111111111111111111111111111111111111111111111111111111",
		ExposurePolicy: api.WorkspaceApplicationRevisionExposurePolicyEnum_WORKSPACE_APPLICATION_REVISION_EXPOSURE_POLICY_ENUM_APPLICATION,
		Ports:          []*api.WorkspaceApplicationPort{{Name: "http", Port: 8080, Protocol: api.WorkspaceApplicationPortProtocolEnum_WORKSPACE_APPLICATION_PORT_PROTOCOL_ENUM_TCP}},
		EntryPort:      proto.String("http"),
		HealthChecks:   []*api.WorkspaceApplicationHealthCheck{{Path: "/healthz", Port: 8080}},
	}
	if declareGateway {
		revision.Credentials = []*api.WorkspaceApplicationCredential{{Name: "gateway", Kind: api.WorkspaceApplicationCredentialKindEnum_WORKSPACE_APPLICATION_CREDENTIAL_KIND_ENUM_GATEWAY_KEY, Target: "/run/secrets/opl_gateway_api_key"}}
	}
	return revision
}

// modelUpdateWorkspace seeds a delivered Workspace whose accepted launch froze a
// resolved application source, a runtime command and the managed-key handover the
// launch established. The Gateway fixture serves that exact binding with the
// launch-approved model scope, so the update path can preserve it.
func modelUpdateWorkspace(t *testing.T, db *sql.DB, declareGateway bool) (*Service, *modelUpdateGateway, *modelUpdateFabric, *modelUpdateServe) {
	t.Helper()
	service, op, _, _ := seedRuntimeOrder(t, db)
	revision := modelUpdateRevision(t, declareGateway)
	artifact := &api.ArtifactReference{Repository: "registry.test/app", Digest: "sha256:1111111111111111111111111111111111111111111111111111111111111111"}
	descriptor := &api.DeploymentDescriptor{SchemaVersion: api.DeploymentDescriptorSchemaVersionEnum_DEPLOYMENT_DESCRIPTOR_SCHEMA_VERSION_ENUM_OPL_DEPLOYMENT_DESCRIPTOR_V1, Artifact: artifact, Provenance: api.DeploymentDescriptorProvenanceEnum_DEPLOYMENT_DESCRIPTOR_PROVENANCE_ENUM_BUILD, ApplicationRevision: revision}
	descriptorRaw, err := publicjson.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(descriptorRaw)
	artifactRaw, err := protojson.Marshal(artifact)
	if err != nil {
		t.Fatal(err)
	}
	sourceRaw, err := json.Marshal(sourceRecord{Kind: "agent", CapabilityVersionID: "capability-original", Artifact: artifactRaw, DeploymentDescriptor: descriptorRaw, DescriptorDigest: "sha256:" + hex.EncodeToString(sum[:]), DescriptorObjectRef: "descriptor-original"})
	if err != nil {
		t.Fatal(err)
	}
	command := &api.RuntimeDeployCommand{Context: continuation(op, "grant-original", "deploy_runtime"), WorkspaceId: op.ResourceID, DeploymentId: "deployment-original", RuntimeInstanceId: "runtime-original", ExecutionEpoch: 1}
	result, err := json.Marshal(orderResult{GrantID: "grant-original", ApplicationSource: sourceRaw, RuntimeCommand: wire(command), ManagedKeyBinding: wire(&api.RuntimeManagedKeyBinding{KeyBindingId: "gateway-key-original", Fingerprint: "sha256:" + strings.Repeat("ab", 32), SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(op.ResourceID), TargetSlot: "gateway"})})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.ExecContext(t.Context(), `UPDATE workspace.operations SET result=$2 WHERE id=$1`, op.ID, result); err != nil {
		t.Fatal(err)
	}
	gateway := &modelUpdateGateway{binding: &api.ManagedKeyBinding{KeyBindingId: "gateway-key-original", Fingerprint: "sha256:" + strings.Repeat("ab", 32), SecretDeliveryReference: contracts.WorkspaceGatewaySecretRef(op.ResourceID), WorkspaceId: op.ResourceID, TargetRuntimeInstanceId: "runtime-original", ModelIds: []string{"model-a", "model-b", "model-new"}}}
	fabric, serve := &modelUpdateFabric{}, &modelUpdateServe{}
	service.Auth = ownerservice.NewAuthorizer(ownerservice.OwnerWorkspace, &modelReadIdentity{})
	service.Gateway, service.Fabric, service.Serve = gateway, fabric, serve
	return service, gateway, fabric, serve
}

// approveModels narrows the Gateway-owned scope the fixture reports, so a test can
// ask for a selection outside the key's approved allowlist.
func approveModels(gateway *modelUpdateGateway, models ...string) {
	gateway.binding.ModelIds = append([]string(nil), models...)
}

// modelUpdateContext is the Console BFF's session-scoped update of one Workspace.
func modelUpdateContext(tenant, idempotencyKey string) (*api.CallContext, context.Context) {
	session := "session-models"
	call := &api.CallContext{ActorId: "actor-original", SessionId: &session, RequestId: "request-" + idempotencyKey, IdempotencyKey: idempotencyKey, Scope: &api.AuthorizationScope{Scope: &api.AuthorizationScope_Tenant{Tenant: &api.TenantScope{TenantId: tenant}}}}
	return call, ownerservice.WithPeerOwner(context.Background(), owneridentity.ConsoleBFF)
}

func modelUpdateRequest(call *api.CallContext, workspaceID string, expected int64, selections []*api.ModelSelection) *api.UpdateWorkspaceModelsRpcRequest {
	return &api.UpdateWorkspaceModelsRpcRequest{Context: call, WorkspaceId: workspaceID, Body: &api.UpdateWorkspaceModelsRequest{ExpectedVersion: expected, Selections: selections}}
}

func appliedWorkspaceModelVersion(t *testing.T, db *sql.DB) int64 {
	t.Helper()
	var applied int64
	if err := db.QueryRowContext(t.Context(), `SELECT model_configuration_version FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&applied); err != nil {
		t.Fatal(err)
	}
	return applied
}

// configurationKeyBindings lists the Gateway key binding each recorded model
// configuration was applied under, oldest first. Every ordinary update must keep
// naming the same binding.
func configurationKeyBindings(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), `SELECT gateway_key_binding_id FROM workspace.model_configurations WHERE workspace_id='workspace-original' ORDER BY version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var bindings []string
	for rows.Next() {
		var binding string
		if err := rows.Scan(&binding); err != nil {
			t.Fatal(err)
		}
		bindings = append(bindings, binding)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return bindings
}

// TestUpdateWorkspaceModelsAppliesOneOwnerSeparatedConfiguration proves the whole
// accepted sequence: Workspace reads back the managed key it already holds and the
// model scope that key is approved for, records a new configuration under that same
// binding, lets Serve perform the Secret handover and apply the publisher contract,
// and advances its applied version only from Serve's confirmed readback.
func TestUpdateWorkspaceModelsAppliesOneOwnerSeparatedConfiguration(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "update-models-first")
	operation, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}}))
	if err != nil {
		t.Fatal(err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("first update=%v", operation)
	}
	// The Workspace read the binding it already held; it neither minted nor revoked.
	if gateway.reads != 1 || gateway.mints != 0 || gateway.revokes != 0 {
		t.Fatalf("key dispatch: reads=%d mints=%d revokes=%d", gateway.reads, gateway.mints, gateway.revokes)
	}
	if gateway.lastRead.GetKeyBindingId() != "gateway-key-original" || gateway.lastRead.GetWorkspaceId() != "workspace-original" {
		t.Fatalf("key read=%+v", gateway.lastRead)
	}
	if fabric.binds != 0 || fabric.rebinds != 0 {
		t.Fatalf("the update coordinated Fabric itself: binds=%d rebinds=%d", fabric.binds, fabric.rebinds)
	}
	// Serve received only the Workspace handover: the exact same key, its
	// fingerprint, delivery reference and publisher slot, never a Fabric binding.
	binding := serve.lastReload.GetManagedKeyBinding()
	if serve.reloads != 1 || len(serve.handoverViolations) != 0 || binding.GetKeyBindingId() != "gateway-key-original" ||
		binding.GetFingerprint() != "sha256:"+strings.Repeat("ab", 32) || binding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef("workspace-original") || binding.GetTargetSlot() != "gateway" {
		t.Fatalf("serve reloads=%d violations=%v handover=%+v", serve.reloads, serve.handoverViolations, binding)
	}
	if serve.lastReload.GetExpectedAppliedVersion() != 0 || serve.lastReload.GetTargetVersion() != 1 {
		t.Fatalf("serve version window=%d..%d", serve.lastReload.GetExpectedAppliedVersion(), serve.lastReload.GetTargetVersion())
	}
	if bindings := configurationKeyBindings(t, db); len(bindings) != 1 || bindings[0] != "gateway-key-original" {
		t.Fatalf("recorded key bindings=%v", bindings)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 1 {
		t.Fatalf("applied version=%d want 1", applied)
	}
	configuration, err := service.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
	if err != nil {
		t.Fatal(err)
	}
	if configuration.GetVersion() != 1 || configuration.GetAppliedVersion() != 1 || configuration.GetStatus() != api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_APPLIED {
		t.Fatalf("read after update=%v", configuration)
	}
}

// TestUpdateWorkspaceModelsPreservesTheExactManagedKeyAcrossConfigurations proves a
// later model configuration keeps the same external key. The second update reads the
// same binding, records it under the same identity, hands the same handover to Serve
// and never mints a replacement or revokes the key still serving the runtime.
func TestUpdateWorkspaceModelsPreservesTheExactManagedKeyAcrossConfigurations(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "preserve-first")
	first, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}}))
	if err != nil || first.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("first update=%v %v", first, err)
	}
	second := &api.CallContext{RequestId: call.GetRequestId() + "-2", IdempotencyKey: call.GetIdempotencyKey() + "-2", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	operation, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(second, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}, {Slot: "embedding", ModelId: "model-new"}}))
	if err != nil || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED {
		t.Fatalf("second update=%v %v", operation, err)
	}
	if gateway.reads != 2 || gateway.mints != 0 || gateway.revokes != 0 || fabric.binds != 0 || fabric.rebinds != 0 {
		t.Fatalf("second update dispatched: reads=%d mints=%d revokes=%d binds=%d rebinds=%d", gateway.reads, gateway.mints, gateway.revokes, fabric.binds, fabric.rebinds)
	}
	if serve.reloads != 2 || len(serve.handoverViolations) != 0 || serve.lastReload.GetManagedKeyBinding().GetKeyBindingId() != "gateway-key-original" {
		t.Fatalf("second reload did not carry the original key: reloads=%d violations=%v handover=%+v", serve.reloads, serve.handoverViolations, serve.lastReload.GetManagedKeyBinding())
	}
	if serve.lastReload.GetExpectedAppliedVersion() != 1 || serve.lastReload.GetTargetVersion() != 2 {
		t.Fatalf("second reload version window=%d..%d", serve.lastReload.GetExpectedAppliedVersion(), serve.lastReload.GetTargetVersion())
	}
	if bindings := configurationKeyBindings(t, db); len(bindings) != 2 || bindings[0] != "gateway-key-original" || bindings[1] != "gateway-key-original" {
		t.Fatalf("recorded key bindings=%v, want the same key twice", bindings)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 2 {
		t.Fatalf("applied version=%d want 2", applied)
	}
}

// TestUpdateWorkspaceModelsRefusesAKeyScopeExpansion proves a selection the current
// key is not approved for is refused before the configuration is recorded, before
// Serve is asked to apply it, and without minting a wider key. Widening a live key
// is a separately authorized owner change, so the Workspace fails closed.
func TestUpdateWorkspaceModelsRefusesAKeyScopeExpansion(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	approveModels(gateway, "model-a")
	call, ctx := modelUpdateContext("tenant-original", "scope-expansion")
	operation, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}}))
	if err != nil {
		t.Fatalf("scope expansion returned a transport error: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION ||
		operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED ||
		operation.GetStage() != api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY || operation.ErrorCode == nil ||
		operation.GetErrorCode() != api.ErrorCodeEnum_ERROR_CODE_ENUM_MODEL_NOT_ALLOWED {
		t.Fatalf("scope expansion was not refused at the key step: %v", operation)
	}
	if gateway.mints != 0 || gateway.revokes != 0 || serve.reloads != 0 || fabric.binds != 0 || fabric.rebinds != 0 {
		t.Fatalf("a refused expansion created effects: mints=%d revokes=%d reloads=%d binds=%d rebinds=%d", gateway.mints, gateway.revokes, serve.reloads, fabric.binds, fabric.rebinds)
	}
	if bindings := configurationKeyBindings(t, db); len(bindings) != 0 {
		t.Fatalf("a refused expansion recorded configurations=%v", bindings)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 0 {
		t.Fatalf("a refused expansion advanced the applied version to %d", applied)
	}
}

// TestUpdateWorkspaceModelsRejectsABindingThatNamesAnotherWorkspace proves the
// Workspace never applies a configuration under a key identity it did not ask for:
// a Gateway answer naming another workspace's binding is a data-loss failure before
// any configuration or Serve call.
func TestUpdateWorkspaceModelsRejectsABindingThatNamesAnotherWorkspace(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, _, serve := modelUpdateWorkspace(t, db, true)
	gateway.binding.WorkspaceId = "workspace-other"
	call, ctx := modelUpdateContext("tenant-original", "foreign-binding")
	operation, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}}))
	if err != nil {
		t.Fatalf("foreign binding returned a transport error: %v", err)
	}
	if operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION ||
		operation.GetErrorCode() != api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR || serve.reloads != 0 {
		t.Fatalf("foreign binding was not refused: %v reloads=%d", operation, serve.reloads)
	}
	if bindings := configurationKeyBindings(t, db); len(bindings) != 0 {
		t.Fatalf("a foreign binding recorded configurations=%v", bindings)
	}
}

// TestUpdateWorkspaceModelsRequiresTheExpectedVersion proves an update whose
// expected version is not the Workspace's current configuration is refused before
// any Gateway or Serve side effect.
func TestUpdateWorkspaceModelsRequiresTheExpectedVersion(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "update-models-stale")
	_, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 4, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}}))
	if status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("stale expectation err=%v want a failed precondition", err)
	}
	if gateway.reads != 0 || gateway.mints != 0 || fabric.binds != 0 || serve.reloads != 0 {
		t.Fatalf("stale update reached owners: reads=%d mints=%d fabric=%d serve=%d", gateway.reads, gateway.mints, fabric.binds, serve.reloads)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 0 {
		t.Fatalf("stale update advanced the applied version to %d", applied)
	}
}

// TestUpdateWorkspaceModelsReplaysOneOperationForOneIdempotencyKey proves a lost
// response replays the original operation instead of reading the key again, binding a
// Secret or applying a second configuration version.
func TestUpdateWorkspaceModelsReplaysOneOperationForOneIdempotencyKey(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, _, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "update-models-replay")
	selections := []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}}
	first, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, selections))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, selections))
	if err != nil {
		t.Fatal(err)
	}
	if replayed.GetOperationId() != first.GetOperationId() {
		t.Fatalf("replay allocated a second operation: %v / %v", first, replayed)
	}
	if gateway.reads != 1 || serve.reloads != 1 || gateway.mints != 0 || gateway.revokes != 0 {
		t.Fatalf("replay repeated side effects: reads=%d serve=%d mints=%d revokes=%d", gateway.reads, serve.reloads, gateway.mints, gateway.revokes)
	}
	// A different body under the same key is a conflict, never a second write.
	conflict := &api.CallContext{RequestId: call.GetRequestId(), IdempotencyKey: call.GetIdempotencyKey(), ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	if _, err = service.UpdateWorkspaceModels(ctx, modelUpdateRequest(conflict, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}})); status.Code(err) != codes.AlreadyExists {
		t.Fatalf("changed input under the same key err=%v want already exists", err)
	}
}

// TestUpdateWorkspaceModelsLeavesTheAppliedVersionUnconfirmedWhenAnOwnerDoesNotAnswer
// proves each unconfirmed step: an unanswered or refused Gateway/Serve call leaves the
// Workspace applied version at its last confirmed value and records the configuration
// as not confirmed.
func TestUpdateWorkspaceModelsLeavesTheAppliedVersionUnconfirmedWhenAnOwnerDoesNotAnswer(t *testing.T) {
	cases := map[string]struct {
		arrange  func(*modelUpdateGateway, *modelUpdateServe)
		stage    api.OperationStageEnum
		observed api.OperationObservationResultEnum
		code     api.ErrorCodeEnum
	}{
		"gateway unavailable": {
			arrange: func(g *modelUpdateGateway, _ *modelUpdateServe) {
				g.readErr = status.Error(codes.Unavailable, "gateway lost the response")
			},
			stage:    api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY,
			observed: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN,
			code:     api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE,
		},
		"gateway refused": {
			arrange: func(g *modelUpdateGateway, _ *modelUpdateServe) {
				g.readErr = status.Error(codes.FailedPrecondition, "the key binding is revoked")
			},
			stage:    api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY,
			observed: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED,
			code:     api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED,
		},
		"serve unavailable": {
			arrange: func(_ *modelUpdateGateway, s *modelUpdateServe) {
				s.reloadErr = status.Error(codes.Unavailable, "serve lost the response")
			},
			stage:    api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD,
			observed: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN,
			code:     api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE,
		},
		"serve refused": {
			arrange: func(_ *modelUpdateGateway, s *modelUpdateServe) {
				s.reloadErr = status.Error(codes.FailedPrecondition, "the runtime refused the configuration")
			},
			stage:    api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD,
			observed: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED,
			code:     api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED,
		},
		"serve not succeeded": {
			arrange: func(_ *modelUpdateGateway, s *modelUpdateServe) {
				s.serveStatus = api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION
			},
			stage:    api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD,
			observed: api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED,
			code:     api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			db := runtimeDatabase(t)
			service, gateway, _, serve := modelUpdateWorkspace(t, db, true)
			call, ctx := modelUpdateContext("tenant-original", "update-models-first")
			if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); err != nil {
				t.Fatalf("first update: %v", err)
			}
			if applied := appliedWorkspaceModelVersion(t, db); applied != 1 {
				t.Fatalf("first update applied=%d want 1", applied)
			}
			tc.arrange(gateway, serve)
			next := &api.CallContext{RequestId: call.GetRequestId() + "-2", IdempotencyKey: call.GetIdempotencyKey() + "-2", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
			failed, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(next, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}}))
			if err != nil {
				t.Fatalf("failing update returned a transport error: %v", err)
			}
			if failed.GetStatus() == api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || failed.GetStage() != tc.stage || failed.GetObservationResult() != tc.observed || failed.GetErrorCode() != tc.code {
				t.Fatalf("failed update=%v; want stage=%s observed=%s code=%s", failed, tc.stage, tc.observed, tc.code)
			}
			if applied := appliedWorkspaceModelVersion(t, db); applied != 1 {
				t.Fatalf("an unconfirmed reload advanced the applied version to %d", applied)
			}
			var observation string
			rowErr := db.QueryRowContext(t.Context(), `SELECT runtime_reload_observation FROM workspace.model_configurations WHERE workspace_id='workspace-original' AND version=2`).Scan(&observation)
			if tc.stage == api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY {
				if rowErr == nil {
					t.Fatalf("a key-step failure recorded configuration observation %q", observation)
				}
			} else if rowErr != nil || observation == "confirmed" || observation != strings.ToLower(strings.TrimPrefix(tc.observed.String(), "OPERATION_OBSERVATION_RESULT_ENUM_")) {
				t.Fatalf("the unconfirmed configuration row=%q err=%v", observation, rowErr)
			}
			if gateway.mints != 0 || gateway.revokes != 0 {
				t.Fatalf("a failed update minted=%d or revoked=%d keys", gateway.mints, gateway.revokes)
			}
		})
	}
}

// TestUpdateWorkspaceModelsBlocksAfterAnUnknownReload proves a lost Serve answer is
// not treated as a refusal: the key and applied version stay as they were, and a
// later update stops at the configuration step without reaching any owner.
func TestUpdateWorkspaceModelsBlocksAfterAnUnknownReload(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, _, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "unknown-first")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); err != nil {
		t.Fatal(err)
	}
	serve.reloadErr = status.Error(codes.Unavailable, "serve lost the response")
	second := &api.CallContext{RequestId: call.GetRequestId() + "-2", IdempotencyKey: call.GetIdempotencyKey() + "-2", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(second, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}})); err != nil {
		t.Fatal(err)
	}
	serve.reloadErr = nil
	reads, reloads := gateway.reads, serve.reloads
	third := &api.CallContext{RequestId: call.GetRequestId() + "-3", IdempotencyKey: call.GetIdempotencyKey() + "-3", ActorId: call.GetActorId(), SessionId: call.SessionId, Scope: call.GetScope()}
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(third, "workspace-original", 1, []*api.ModelSelection{{Slot: "chat", ModelId: "model-new"}})); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("an unresolved reload did not block the next accepted update: %v", err)
	}
	if gateway.reads != reads || serve.reloads != reloads || gateway.mints != 0 || gateway.revokes != 0 {
		t.Fatalf("a blocked update reached owners: reads=%d/%d reloads=%d/%d mints=%d revokes=%d", gateway.reads, reads, serve.reloads, reloads, gateway.mints, gateway.revokes)
	}
	if applied := appliedWorkspaceModelVersion(t, db); applied != 1 {
		t.Fatalf("a blocked update advanced the applied version to %d", applied)
	}
}

// TestUpdateWorkspaceModelsRefusesARuntimeWithoutTheGatewayCredential proves the
// update path is not fabricated for a runtime that declares no model-configuration
// capability: no key is read or minted and the applied version does not advance.
func TestUpdateWorkspaceModelsRefusesARuntimeWithoutTheGatewayCredential(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, false)
	call, ctx := modelUpdateContext("tenant-original", "update-models-no-gateway")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("runtime without the Gateway credential err=%v want a failed precondition", err)
	}
	if gateway.reads != 0 || gateway.mints != 0 || fabric.binds != 0 || serve.reloads != 0 {
		t.Fatalf("a runtime without the capability reached owners: reads=%d mints=%d fabric=%d serve=%d", gateway.reads, gateway.mints, fabric.binds, serve.reloads)
	}
}

// TestUpdateWorkspaceModelsAdmitsOnlyTheOwningTenant proves an update is refused for
// another tenant and for a denied policy decision, before any side effect.
func TestUpdateWorkspaceModelsAdmitsOnlyTheOwningTenant(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, _, _ := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-other", "update-models-other-tenant")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("another tenant err=%v want permission denied", err)
	}
	service.Auth = ownerservice.NewAuthorizer(ownerservice.OwnerWorkspace, &modelReadIdentity{denial: status.Error(codes.PermissionDenied, "admin role required")})
	call, ctx = modelUpdateContext("tenant-original", "update-models-denied")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); status.Code(err) != codes.PermissionDenied {
		t.Fatalf("denied policy err=%v want permission denied", err)
	}
	if gateway.reads != 0 || gateway.mints != 0 {
		t.Fatalf("a refused update reached Gateway: reads=%d mints=%d", gateway.reads, gateway.mints)
	}
}

// TestUpdateWorkspaceModelsRefusesASecondAcceptedUpdateWhileOneIsRunning proves
// different idempotency keys cannot reserve the same configuration version and race
// into cross-owner side effects.
func TestUpdateWorkspaceModelsRefusesASecondAcceptedUpdateWhileOneIsRunning(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, _, serve := modelUpdateWorkspace(t, db, true)
	if _, err := db.ExecContext(t.Context(), `INSERT INTO workspace.operations(id,tenant_id,actor_id,kind,resource_id,status,stage,request_id,accepted_input,observation_result)
		VALUES('op-models-pending','tenant-original','actor-original','update_models','workspace-original','running','key','request-pending','{}'::jsonb,'unknown')`); err != nil {
		t.Fatal(err)
	}
	call, ctx := modelUpdateContext("tenant-original", "update-models-concurrent")
	if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}})); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("concurrent accepted update err=%v want failed precondition", err)
	}
	if gateway.reads != 0 || serve.reloads != 0 {
		t.Fatalf("a concurrently refused update reached owners: reads=%d serve=%d", gateway.reads, serve.reloads)
	}
}

// TestUpdateWorkspaceModelsValidatesTheSelections proves the accepted selection set
// is restated and checked before any owner is called.
func TestUpdateWorkspaceModelsValidatesTheSelections(t *testing.T) {
	db := runtimeDatabase(t)
	service, gateway, _, serve := modelUpdateWorkspace(t, db, true)
	call, ctx := modelUpdateContext("tenant-original", "update-models-validation")
	for name, selections := range map[string][]*api.ModelSelection{
		"empty":       nil,
		"blank slot":  {{Slot: " ", ModelId: "model-a"}},
		"blank model": {{Slot: "chat", ModelId: " "}},
		"duplicate":   {{Slot: "chat", ModelId: "model-a"}, {Slot: "chat", ModelId: "model-b"}},
	} {
		if _, err := service.UpdateWorkspaceModels(ctx, modelUpdateRequest(call, "workspace-original", 0, selections)); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("%s err=%v want invalid argument", name, err)
		}
	}
	if gateway.reads != 0 || serve.reloads != 0 {
		t.Fatalf("an invalid selection reached owners: reads=%d serve=%d", gateway.reads, serve.reloads)
	}
}

// TestEnsureRuntimeGatewayBindingIssuesTheLaunchHandover proves the F08 owner path
// for one launch: for a runtime whose frozen revision declares the installation
// Gateway credential, the Workspace issues the opaque key binding through Gateway
// and freezes the handover - key binding, fingerprint, Secret delivery reference and
// publisher slot - on the command without ever binding into Fabric itself, because
// Serve alone asks Fabric to bind the Secret. A restart reuses the recorded handover
// instead of minting a second key, and a revision that declares no credential mints
// nothing.
func TestEnsureRuntimeGatewayBindingIssuesTheLaunchHandover(t *testing.T) {
	revision := modelUpdateRevision(t, true)
	command := &api.RuntimeDeployCommand{
		WorkspaceId: "workspace-original", RuntimeInstanceId: "runtime-original",
		DeploymentDescriptor: &api.DeploymentDescriptor{ApplicationRevision: revision},
		ModelSelections:      []*api.ModelSelection{{Slot: "chat", ModelId: "model-a"}},
	}
	op := ownerstore.Operation{ID: "op-launch", RequestID: "request-launch", TenantID: "tenant-original", ActorID: "actor-original", ResourceID: "workspace-original"}
	result := &orderResult{GrantID: "grant-launch"}
	gateway, fabric := &modelUpdateGateway{}, &modelUpdateFabric{}
	service := &Service{Gateway: gateway, Fabric: fabric}

	if err := service.ensureRuntimeGatewayBinding(context.Background(), op, result, command); err != nil {
		t.Fatalf("ensure launch handover: %v", err)
	}
	if gateway.mints != 1 || fabric.binds != 0 {
		t.Fatalf("launch handover mints=%d fabric binds=%d, want the Gateway key alone", gateway.mints, fabric.binds)
	}
	binding := command.GetManagedKeyBinding()
	if binding.GetKeyBindingId() != "gateway-key-model-a" || binding.GetFingerprint() == "" || binding.GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef("workspace-original") || binding.GetTargetSlot() != "gateway" {
		t.Fatalf("launch command handover=%+v", binding)
	}
	// The Fabric-confirmed binding identity is Serve's alone: the Workspace handover
	// must never carry one.
	if binding.GetSecretBindingId() != "" || binding.GetSecretVersion() != "" {
		t.Fatalf("launch handover supplied a Fabric-confirmed binding: %+v", binding)
	}
	if len(result.ManagedKeyBinding) == 0 {
		t.Fatal("launch handover was not recorded on the order result")
	}
	// A restart reuses the recorded handover instead of minting a second key.
	replayed := &api.RuntimeDeployCommand{WorkspaceId: command.WorkspaceId, RuntimeInstanceId: command.RuntimeInstanceId, DeploymentDescriptor: command.DeploymentDescriptor, ModelSelections: command.ModelSelections}
	if err := service.ensureRuntimeGatewayBinding(context.Background(), op, result, replayed); err != nil {
		t.Fatalf("replay launch handover: %v", err)
	}
	if gateway.mints != 1 || fabric.binds != 0 || !proto.Equal(replayed.GetManagedKeyBinding(), binding) {
		t.Fatalf("restart reissued the launch key: mints=%d binds=%d", gateway.mints, fabric.binds)
	}
	// A stored handover that already carries a Fabric-confirmed binding is refused
	// instead of being presented again as an unconfirmed handover.
	stale := wire(&api.RuntimeManagedKeyBinding{KeyBindingId: binding.GetKeyBindingId(), SecretDeliveryReference: binding.GetSecretDeliveryReference(), Fingerprint: binding.GetFingerprint(), TargetSlot: "gateway", SecretBindingId: "sbx_stale", SecretVersion: "v1"})
	fresh := &api.RuntimeDeployCommand{WorkspaceId: command.WorkspaceId, RuntimeInstanceId: command.RuntimeInstanceId, DeploymentDescriptor: command.DeploymentDescriptor, ModelSelections: command.ModelSelections}
	if err := service.ensureRuntimeGatewayBinding(context.Background(), op, &orderResult{GrantID: "grant-launch", ManagedKeyBinding: stale}, fresh); status.Code(err) != codes.DataLoss || fresh.GetManagedKeyBinding() != nil {
		t.Fatalf("stored confirmed binding err=%v handover=%+v want data loss and no handover", err, fresh.GetManagedKeyBinding())
	}
	// A revision that declares no credential mints nothing and carries no binding.
	plain := &api.RuntimeDeployCommand{WorkspaceId: command.WorkspaceId, RuntimeInstanceId: command.RuntimeInstanceId, DeploymentDescriptor: &api.DeploymentDescriptor{ApplicationRevision: modelUpdateRevision(t, false)}, ModelSelections: command.ModelSelections}
	if err := service.ensureRuntimeGatewayBinding(context.Background(), op, &orderResult{GrantID: "grant-launch"}, plain); err != nil {
		t.Fatalf("no-credential launch: %v", err)
	}
	if gateway.mints != 1 || plain.GetManagedKeyBinding() != nil {
		t.Fatalf("a runtime without a declared credential minted a key")
	}
	// The Gateway owner is the only coordination this handover needs: an order that
	// reaches it before Gateway is configured waits instead of deploying keyless.
	waiting := &Service{}
	if err := waiting.ensureRuntimeGatewayBinding(context.Background(), op, &orderResult{GrantID: "grant-launch"}, fresh); !errors.Is(err, errRuntimeAwaitingKeyOwner) {
		t.Fatalf("unconfigured Gateway err=%v want the awaiting-owner wait", err)
	}
	// A default OPL App declares no model selection at all. The Workspace must not
	// refuse locally: it forwards the declared (empty) selection and the Gateway
	// owner resolves the approved concrete scope, returning the opaque handover the
	// Workspace records on the command.
	defaultApp := &api.RuntimeDeployCommand{WorkspaceId: command.WorkspaceId, RuntimeInstanceId: command.RuntimeInstanceId, DeploymentDescriptor: command.DeploymentDescriptor}
	defaultResult := &orderResult{GrantID: "grant-launch"}
	if err := service.ensureRuntimeGatewayBinding(context.Background(), op, defaultResult, defaultApp); err != nil {
		t.Fatalf("default App launch handover: %v", err)
	}
	if gateway.mints != 2 || len(gateway.lastMint.GetModelIds()) != 0 ||
		defaultApp.GetManagedKeyBinding().GetKeyBindingId() == "" || defaultApp.GetManagedKeyBinding().GetSecretDeliveryReference() != contracts.WorkspaceGatewaySecretRef("workspace-original") {
		t.Fatalf("default App launch mint=%d models=%v handover=%+v", gateway.mints, gateway.lastMint.GetModelIds(), defaultApp.GetManagedKeyBinding())
	}
	if len(defaultResult.ManagedKeyBinding) == 0 {
		t.Fatal("the default App handover was not recorded on the order result")
	}
}

// modelUpdateOperationIdentity binds the authorization fixture to the verified
// Workspace service and the exact Console actor/session/resource, rather than
// accepting an arbitrary request or echoing an unverified actor grant.
type modelUpdateOperationIdentity struct {
	api.UnimplementedCloudIdentityAuthorizationServer
	mu          sync.RWMutex
	operationID string
}

func (i *modelUpdateOperationIdentity) AuthorizeAction(ctx context.Context, r *api.AuthorizationRequest) (*api.AuthorizationDecision, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	peer, verified := ownerservice.PeerOwner(ctx)
	if !verified || peer != owneridentity.Workspace.Service() || r.GetActorId() != "actor-original" || r.GetSessionId() != "session-models" || r.GetAcceptedOperationGrantId() != "" || r.GetScope().GetTenant().GetTenantId() != "tenant-original" || r.GetAudienceOwner() != api.OwnerEnum_OWNER_ENUM_WORKSPACE {
		return nil, status.Error(codes.PermissionDenied, "fixture authorization identity mismatch")
	}
	switch r.GetAction() {
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_UPDATEWORKSPACEMODELS, api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETWORKSPACEMODELS:
		if r.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_WORKSPACE || r.GetResource().GetId() != "workspace-original" {
			return nil, status.Error(codes.PermissionDenied, "fixture update resource mismatch")
		}
	case api.AuthorizationActionEnum_AUTHORIZATION_ACTION_ENUM_GETOPERATION:
		if i.operationID == "" || r.GetResource().GetKind() != api.AuthorizationResourceKind_AUTHORIZATION_RESOURCE_KIND_OPERATION || r.GetResource().GetId() != i.operationID {
			return nil, status.Error(codes.PermissionDenied, "fixture operation resource mismatch")
		}
	default:
		return nil, status.Error(codes.PermissionDenied, "fixture action mismatch")
	}
	return &api.AuthorizationDecision{
		Issuer: api.AuthorizationIssuer_AUTHORIZATION_ISSUER_CLOUD_IDENTITY, Result: api.AuthorizationResult_AUTHORIZATION_RESULT_ALLOWED,
		ActorId: "actor-original", Scope: r.GetScope(), SessionId: proto.String("session-models"),
		AudienceOwner: api.OwnerEnum_OWNER_ENUM_WORKSPACE, Action: r.GetAction(), Resource: r.GetResource(),
		PermissionVersion: 1, IssuedAt: timestamppb.New(time.Now().Add(-time.Second)), ExpiresAt: timestamppb.New(time.Now().Add(time.Minute)),
	}, nil
}

func modelUpdateNativeServer(t *testing.T, owner owneridentity.Owner, caller owneridentity.Service, register func(*ownerservice.Server) error) string {
	t.Helper()
	server, err := ownerservice.NewServer(ownerservice.Config{Owner: owner, TLS: owneridentity.TLSConfig{AllowInsecureLocal: true}, Peers: map[owneridentity.Service]string{caller: modelUpdateNativeToken}})
	if err != nil {
		t.Fatal(err)
	}
	if err = register(server); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- server.ServeOn(listener) }()
	t.Cleanup(func() {
		server.Stop()
		select {
		case err := <-served:
			if err != nil && !errors.Is(err, grpc.ErrServerStopped) {
				t.Errorf("native owner server: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("native owner server did not stop")
		}
	})
	return listener.Addr().String()
}

const modelUpdateNativeToken = "synthetic-model-update-owner-token-0001"

func modelUpdateNativeDial(t *testing.T, address string, caller, target owneridentity.Service) *grpc.ClientConn {
	t.Helper()
	options, err := (owneridentity.TLSConfig{AllowInsecureLocal: true}).DialOptions(caller, target, modelUpdateNativeToken)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := grpc.NewClient(address, options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// modelUpdateNativeWorkspace wires the registered Workspace producer, the registered
// operation reader and the CloudIdentity decision fixture over authenticated loopback
// gRPC, and returns the clients plus the service and the fixture identity the caller
// must point at the accepted operation before reading it.
func modelUpdateNativeWorkspace(t *testing.T, db *sql.DB) (*Service, *modelUpdateGateway, *modelUpdateFabric, *modelUpdateServe, *modelUpdateOperationIdentity, api.WorkspaceProductServiceClient, api.OwnerOperationsClient) {
	t.Helper()
	service, gateway, fabric, serve := modelUpdateWorkspace(t, db, true)
	identity := &modelUpdateOperationIdentity{}
	identityAddress := modelUpdateNativeServer(t, owneridentity.Tenant, owneridentity.Workspace.Service(), func(s *ownerservice.Server) error {
		return s.Register(func(g *grpc.Server) { api.RegisterCloudIdentityAuthorizationServer(g, identity) })
	})
	identityConn := modelUpdateNativeDial(t, identityAddress, owneridentity.Workspace.Service(), owneridentity.Tenant.Service())
	service.Auth = ownerservice.NewAuthorizer(ownerservice.OwnerWorkspace, api.NewCloudIdentityAuthorizationClient(identityConn))
	operations, err := ownerservice.NewOperations(ownerservice.OwnerWorkspace, service.Store, service.Auth)
	if err != nil {
		t.Fatal(err)
	}
	workspaceAddress := modelUpdateNativeServer(t, owneridentity.Workspace, owneridentity.ConsoleBFF, func(s *ownerservice.Server) error {
		if err := service.Register(s); err != nil {
			return err
		}
		return operations.Register(s)
	})
	conn := modelUpdateNativeDial(t, workspaceAddress, owneridentity.ConsoleBFF, owneridentity.Workspace.Service())
	return service, gateway, fabric, serve, identity, api.NewWorkspaceProductServiceClient(conn), api.NewOwnerOperationsClient(conn)
}

// TestUpdateWorkspaceModelsNativeFailureReadback drives the registered Workspace
// producer, PostgreSQL row and registered OwnerOperations.Read over authenticated
// loopback gRPC. Only the Gateway coordination read, Serve coordination and
// CloudIdentity decisions are fixtures; neither the persisted operation nor its
// public reader is stubbed. Each case names the stage, observation and canonical
// error code the customer read must report, and proves a same-intent replay cannot
// repeat the effect or change the original identity.
func TestUpdateWorkspaceModelsNativeFailureReadback(t *testing.T) {
	cases := []struct {
		name        string
		arrange     func(*modelUpdateGateway, *modelUpdateServe)
		stage       api.OperationStageEnum
		observation api.OperationObservationResultEnum
		code        api.ErrorCodeEnum
	}{
		{"key read unavailable", func(g *modelUpdateGateway, _ *modelUpdateServe) {
			g.readErr = status.Error(codes.Unavailable, "synthetic Gateway read lost")
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE},
		{"key read precondition refused", func(g *modelUpdateGateway, _ *modelUpdateServe) {
			g.readErr = status.Error(codes.FailedPrecondition, "the managed key binding is revoked")
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED},
		{"key read data loss", func(g *modelUpdateGateway, _ *modelUpdateServe) {
			g.readErr = status.Error(codes.DataLoss, "stored managed key binding is invalid")
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR},
		{"key typed scope refusal", func(g *modelUpdateGateway, _ *modelUpdateServe) {
			g.readErr = owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "synthetic key scope refusal"), api.ErrorCodeEnum_ERROR_CODE_ENUM_MODEL_NOT_ALLOWED)
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_MODEL_NOT_ALLOWED},
		{"scope expansion refused", func(g *modelUpdateGateway, _ *modelUpdateServe) {
			approveModels(g, "model-a")
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_MODEL_NOT_ALLOWED},
		{"key typed synchronous unauthenticated", func(g *modelUpdateGateway, _ *modelUpdateServe) {
			g.readErr = owneridentity.WithErrorCode(status.Error(codes.Unauthenticated, "synthetic downstream identity failure"), api.ErrorCodeEnum_ERROR_CODE_ENUM_UNAUTHENTICATED)
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_INTERNAL_ERROR},
		{"reload precondition refused", func(_ *modelUpdateGateway, s *modelUpdateServe) {
			s.reloadErr = status.Error(codes.FailedPrecondition, "synthetic reload admission refused")
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED},
		{"reload unavailable", func(_ *modelUpdateGateway, s *modelUpdateServe) {
			s.reloadErr = status.Error(codes.Unavailable, "synthetic Serve response lost")
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_DEPENDENCY_UNAVAILABLE},
		{"reload typed access refusal", func(_ *modelUpdateGateway, s *modelUpdateServe) {
			s.reloadErr = owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "synthetic unsupported entry"), api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE)
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_APP_ACCESS_UNAVAILABLE},
		{"reload typed unknown", func(_ *modelUpdateGateway, s *modelUpdateServe) {
			s.reloadErr = owneridentity.WithErrorCode(status.Error(codes.FailedPrecondition, "synthetic runtime outcome unconfirmed"), api.ErrorCodeEnum_ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN)
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN, api.ErrorCodeEnum_ERROR_CODE_ENUM_EXTERNAL_OUTCOME_UNKNOWN},
		{"reload not confirmed", func(_ *modelUpdateGateway, s *modelUpdateServe) {
			s.serveStatus = api.OperationStatusEnum_OPERATION_STATUS_ENUM_AWAITING_CONFIRMATION
		}, api.OperationStageEnum_OPERATION_STAGE_ENUM_RELOAD, api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_REJECTED, api.ErrorCodeEnum_ERROR_CODE_ENUM_VALIDATION_FAILED},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := runtimeDatabase(t)
			service, gateway, _, serve, identity, writer, reader := modelUpdateNativeWorkspace(t, db)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			tc.arrange(gateway, serve)
			call, _ := modelUpdateContext("tenant-original", "native-failure")
			request := modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}})
			put, err := writer.UpdateWorkspaceModels(ctx, request)
			operationID := put.GetOperationId()
			if err != nil || operationID == "" {
				t.Errorf("registered update returned no readable operation: %v %v", put, err)
				if err := db.QueryRowContext(ctx, `SELECT id FROM workspace.operations WHERE request_id=$1 AND kind='update_models'`, call.GetRequestId()).Scan(&operationID); err != nil {
					t.Fatalf("read accepted operation identity: %v", err)
				}
			}
			identity.mu.Lock()
			identity.operationID = operationID
			identity.mu.Unlock()
			row, err := service.Store.ReadOperation(ctx, operationID)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("persisted operation=%s stage=%s state=%s observation=%s errorCode=%s applied=%d", row.ID, row.Stage, row.Status, row.Observation, row.ErrorCode, appliedWorkspaceModelVersion(t, db))
			if row.ErrorCode != errorCodeText(tc.code) {
				t.Errorf("stored error code=%q want %s", row.ErrorCode, tc.code)
			}
			get, readErr := reader.Read(ctx, &api.OwnerOperationRequest{Context: call, OperationId: operationID})
			if readErr != nil {
				t.Errorf("registered OwnerOperations.Read rejected the producer row: %v", readErr)
			}
			for name, operation := range map[string]*api.Operation{"PUT": put, "GET": get} {
				if operation == nil {
					t.Errorf("%s omitted the accepted operation", name)
					continue
				}
				if operation.GetOperationId() != row.ID || operation.GetOwner() != api.OperationOwnerEnum_OPERATION_OWNER_ENUM_WORKSPACE || operation.GetResourceId() != "workspace-original" || operation.GetKind() != api.OperationKindEnum_OPERATION_KIND_ENUM_UPDATE_MODELS || operation.GetStage() != tc.stage || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION || operation.GetObservationResult() != tc.observation || operation.GetErrorCode() != tc.code || operation.GetPollAfterSeconds() != 5 {
					t.Errorf("%s does not report the persisted failure: %v; want stage=%s observation=%s code=%s", name, operation, tc.stage, tc.observation, tc.code)
				}
				if _, err := publicjson.Marshal(operation); err != nil {
					t.Errorf("%s public JSON: %v", name, err)
				}
			}
			if appliedWorkspaceModelVersion(t, db) != 0 {
				t.Error("a failed update advanced the applied version")
			}
			if tc.stage != api.OperationStageEnum_OPERATION_STAGE_ENUM_KEY {
				var persistedObservation string
				if err := db.QueryRowContext(ctx, `SELECT runtime_reload_observation FROM workspace.model_configurations WHERE operation_id=$1`, row.ID).Scan(&persistedObservation); err != nil {
					t.Fatal(err)
				}
				if persistedObservation != row.Observation {
					t.Errorf("configuration observation=%s want %s", persistedObservation, row.Observation)
				}
				configuration, err := writer.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
				if err != nil || configuration.GetOperationId() != row.ID || configuration.GetAppliedVersion() != 0 || configuration.GetStatus() != api.ModelConfigurationStatusEnum_MODEL_CONFIGURATION_STATUS_ENUM_NEEDS_ATTENTION {
					t.Errorf("registered configuration read lost the original operation/outcome: %v %v", configuration, err)
				}
			}
			reads, reloads, mints, revokes := gateway.reads, serve.reloads, gateway.mints, gateway.revokes
			replay, err := writer.UpdateWorkspaceModels(ctx, request)
			if err != nil || !proto.Equal(put, replay) {
				t.Errorf("same-intent replay: %v %v, original %v", replay, err, put)
			}
			if reads != gateway.reads || reloads != serve.reloads || mints != gateway.mints || revokes != gateway.revokes {
				t.Error("replay repeated a downstream effect")
			}
			// The strict shared reader must still refuse transport spellings stored
			// outside the fixed producer; no decoder expansion is part of this repair.
			if _, err := db.ExecContext(ctx, `UPDATE workspace.operations SET error_code='Unavailable' WHERE id=$1`, row.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := reader.Read(ctx, &api.OwnerOperationRequest{Context: call, OperationId: row.ID}); status.Code(err) != codes.Internal {
				t.Errorf("strict reader accepted invalid stored code: %v", err)
			}
			if _, err := writer.UpdateWorkspaceModels(ctx, request); status.Code(err) != codes.Internal {
				t.Errorf("PUT replay accepted invalid stored code: %v", err)
			}
			for name, change := range map[string]func(*api.CallContext){
				"actor":   func(c *api.CallContext) { c.ActorId = "another-actor" },
				"tenant":  func(c *api.CallContext) { c.Scope.GetTenant().TenantId = "another-tenant" },
				"session": func(c *api.CallContext) { c.SessionId = proto.String("another-session") },
				"grant":   func(c *api.CallContext) { c.AcceptedOperationGrantId = proto.String("unbound-grant") },
			} {
				other := proto.Clone(call).(*api.CallContext)
				change(other)
				if _, err := reader.Read(ctx, &api.OwnerOperationRequest{Context: other, OperationId: row.ID}); status.Code(err) != codes.PermissionDenied {
					t.Errorf("reader did not bind %s: %v", name, err)
				}
			}
		})
	}
}

// TestUpdateWorkspaceModelsNativeAppliedVersionCAS holds the accepted intent and the
// reload response constant, changing only the owner's applied version before
// confirmation. It exercises the registered producer, real transaction and registered
// operation readback, and proves the same binding is preserved on success and that a
// stale confirmation rolls back without minting or revoking anything.
func TestUpdateWorkspaceModelsNativeAppliedVersionCAS(t *testing.T) {
	for _, tc := range []struct {
		name              string
		concurrentApplied int64
	}{
		{"stale higher applied", 2},
		{"stale already target applied", 1},
		{"adjacent expected applied", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := runtimeDatabase(t)
			service, gateway, fabric, serve, identity, writer, reader := modelUpdateNativeWorkspace(t, db)
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			serve.beforeReloadAnswer = func(ctx context.Context, r *api.RuntimeReloadCommand) error {
				if r.GetExpectedAppliedVersion() != 0 || r.GetTargetVersion() != 1 {
					return status.Error(codes.Internal, "fixture did not receive captured expected/target versions")
				}
				if tc.concurrentApplied == 0 {
					return nil
				}
				_, err := db.ExecContext(ctx, `UPDATE workspace.workspaces SET model_configuration_version=$1,version=version+1 WHERE id='workspace-original'`, tc.concurrentApplied)
				return err
			}
			call, _ := modelUpdateContext("tenant-original", "native-applied-cas")
			request := modelUpdateRequest(call, "workspace-original", 0, []*api.ModelSelection{{Slot: "chat", ModelId: "model-b"}})
			put, callErr := writer.UpdateWorkspaceModels(ctx, request)
			if callErr != nil || put.GetOperationId() == "" {
				t.Errorf("accepted CAS update returned no operation: %v %v", put, callErr)
			}
			var operationID string
			if err := db.QueryRowContext(ctx, `SELECT id FROM workspace.operations WHERE request_id=$1 AND kind='update_models'`, call.GetRequestId()).Scan(&operationID); err != nil {
				t.Fatal(err)
			}
			identity.mu.Lock()
			identity.operationID = operationID
			identity.mu.Unlock()
			get, err := reader.Read(ctx, &api.OwnerOperationRequest{Context: call, OperationId: operationID})
			if err != nil {
				t.Fatal(err)
			}
			row, err := service.Store.ReadOperation(ctx, operationID)
			if err != nil {
				t.Fatal(err)
			}
			var applied, workspaceVersion int64
			var configurationID, observation string
			var evidence sql.NullString
			if err := db.QueryRowContext(ctx, `SELECT model_configuration_version,version FROM workspace.workspaces WHERE id='workspace-original'`).Scan(&applied, &workspaceVersion); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRowContext(ctx, `SELECT id,runtime_reload_observation,verification_evidence_ref FROM workspace.model_configurations WHERE operation_id=$1`, operationID).Scan(&configurationID, &observation, &evidence); err != nil {
				t.Fatal(err)
			}
			t.Logf("CAS captured=0 target=1 concurrent=%d applied=%d workspaceVersion=%d operation=%s state=%s observation=%s errorCode=%s configurationObservation=%s evidencePresent=%t mints=%d", tc.concurrentApplied, applied, workspaceVersion, operationID, row.Status, row.Observation, row.ErrorCode, observation, evidence.Valid, gateway.mints)
			if tc.concurrentApplied != 0 {
				if applied != tc.concurrentApplied || workspaceVersion != 2 {
					t.Errorf("stale callback changed newer owner facts: applied=%d version=%d want %d/2", applied, workspaceVersion, tc.concurrentApplied)
				}
				if observation != "unknown" || evidence.Valid {
					t.Errorf("failed confirmation transaction did not roll back: observation=%s evidence=%v", observation, evidence)
				}
				if row.Status != "needs_attention" || row.Observation != "unknown" || row.ErrorCode != errorCodeText(api.ErrorCodeEnum_ERROR_CODE_ENUM_VERSION_CONFLICT) {
					t.Errorf("CAS rejection was not durably classified on original operation: %+v", row)
				}
				for name, operation := range map[string]*api.Operation{"PUT": put, "GET": get} {
					if operation == nil || operation.GetOperationId() != operationID || operation.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_NEEDS_ATTENTION || operation.GetObservationResult() != api.OperationObservationResultEnum_OPERATION_OBSERVATION_RESULT_ENUM_UNKNOWN || operation.ErrorCode == nil || operation.GetErrorCode() != api.ErrorCodeEnum_ERROR_CODE_ENUM_VERSION_CONFLICT {
						t.Errorf("%s does not report original CAS conflict: %v", name, operation)
					}
				}
			} else {
				if applied != 1 || workspaceVersion != 2 || observation != "confirmed" || !evidence.Valid || evidence.String != "workspace-model-configuration://"+configurationID+"/gateway-key-original" {
					t.Errorf("adjacent happy confirmation did not commit atomically: applied=%d version=%d observation=%s evidence=%v", applied, workspaceVersion, observation, evidence)
				}
				if put.GetStatus() != api.OperationStatusEnum_OPERATION_STATUS_ENUM_SUCCEEDED || !proto.Equal(put, get) {
					t.Errorf("happy PUT/GET mismatch: %v / %v", put, get)
				}
			}
			if gateway.mints != 0 || gateway.revokes != 0 || fabric.binds != 0 || fabric.rebinds != 0 || serve.reloads != 1 || gateway.reads != 1 {
				t.Errorf("unexpected dispatch counts mints=%d revokes=%d binds=%d rebinds=%d reloads=%d reads=%d", gateway.mints, gateway.revokes, fabric.binds, fabric.rebinds, serve.reloads, gateway.reads)
			}
			configuration, err := writer.GetWorkspaceModels(ctx, &api.GetWorkspaceModelsRpcRequest{Context: call, WorkspaceId: "workspace-original"})
			if err != nil || configuration.GetOperationId() != operationID || configuration.GetAppliedVersion() != applied {
				t.Errorf("registered configuration read does not retain owner facts: %v %v", configuration, err)
			}
			replay, err := writer.UpdateWorkspaceModels(ctx, request)
			if err != nil || !proto.Equal(replay, get) || gateway.mints != 0 || serve.reloads != 1 || gateway.reads != 1 {
				t.Errorf("CAS replay repeated effects or changed original identity: %v %v", replay, err)
			}
		})
	}
}
